package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/spatial"
)

// rainingCell inserts n devices' readings in cell at ts, each implying
// 20 L / (10 m2 * 0.8) = 2.5 mm, i.e. at or above PeerRainMM (2.0).
func rainingCell(t *testing.T, db *sql.DB, cell string, n int, ts time.Time, tag string) {
	t.Helper()
	for i := 0; i < n; i++ {
		tel := testTelemetry(cell, 3.0)
		tel.ClaimedLitres = 20
		insertReadingRow(t, db, fmt.Sprintf("%s-%d", tag, i), "bldg-1", ts, tel, fmt.Sprintf("k-%s-%d", tag, i))
	}
}

func escrowState(t *testing.T, repo *Repository, id string) string {
	t.Helper()
	e, err := repo.GetEscrow(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e.State
}

func TestNeighborCorroboration(t *testing.T) {
	ctx := context.Background()
	vcfg := verifier.DefaultConfig()
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	setup := func(t *testing.T) (*testEnv, string, []string, string) {
		_, env, _, cell := setupIntegration(t)
		nbs, err := spatial.NeighborsOf(cell, 1)
		if err != nil || len(nbs) != 6 {
			t.Fatalf("neighbours: %v %v", nbs, err)
		}
		rid := insertReadingRow(t, env.DB, "esc-dev", "bldg-1", base, testTelemetry(cell, 0), "k-esc")
		eid := insertEscrowRow(t, env.DB, rid, base.Add(vcfg.EscrowTTL))
		return env, cell, nbs, eid
	}

	t.Run("two raining neighbour cells with three devices each corroborate", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		rainingCell(t, env.DB, nbs[0], 3, base.Add(time.Minute), "nA")
		rainingCell(t, env.DB, nbs[1], 3, base.Add(time.Minute), "nB")
		if err := env.Server.Sweep(ctx, base.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "CORROBORATED" {
			t.Fatalf("escrow state = %s, want CORROBORATED", got)
		}
		assertCount(t, env.DB, `SELECT COUNT(*) FROM ledger_entry WHERE entry_type='COMMIT'`, 1)
		assertCount(t, env.DB, fmt.Sprintf(`SELECT COUNT(*) FROM audit_ledger_entry WHERE event_ref = 'escrow_corroborated_h3:%s'`, eid), 1)
	})

	t.Run("a single raining neighbour is not enough", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		rainingCell(t, env.DB, nbs[0], 5, base.Add(time.Minute), "nA")
		if err := env.Server.Sweep(ctx, base.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "PENDING" {
			t.Fatalf("escrow state = %s, want PENDING", got)
		}
	})

	t.Run("neighbour cells need three devices each", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		rainingCell(t, env.DB, nbs[0], 3, base.Add(time.Minute), "nA")
		rainingCell(t, env.DB, nbs[1], 2, base.Add(time.Minute), "nB")
		if err := env.Server.Sweep(ctx, base.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "PENDING" {
			t.Fatalf("escrow state = %s, want PENDING", got)
		}
	})

	t.Run("dry neighbours do not corroborate", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		for i, c := range nbs[:3] {
			for j := 0; j < 4; j++ {
				tel := testTelemetry(c, 0)
				tel.ClaimedLitres = 0.4 // 0.05 mm: dry
				insertReadingRow(t, env.DB, fmt.Sprintf("dry-%d-%d", i, j), "bldg-1", base.Add(time.Minute), tel, fmt.Sprintf("kd-%d-%d", i, j))
			}
		}
		if err := env.Server.Sweep(ctx, base.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "PENDING" {
			t.Fatalf("escrow state = %s, want PENDING", got)
		}
	})

	t.Run("neighbour evidence older than PeerMaxAge is ignored", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		rainingCell(t, env.DB, nbs[0], 3, base.Add(time.Minute), "nA")
		rainingCell(t, env.DB, nbs[1], 3, base.Add(time.Minute), "nB")
		// Sweep 11 minutes after the neighbour readings (still inside escrow TTL).
		if err := env.Server.Sweep(ctx, base.Add(12*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "PENDING" {
			t.Fatalf("escrow state = %s, want PENDING (stale evidence)", got)
		}
	})

	t.Run("expiry wins once the TTL has passed", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		late := base.Add(vcfg.EscrowTTL + time.Minute)
		rainingCell(t, env.DB, nbs[0], 3, late.Add(-time.Minute), "nA")
		rainingCell(t, env.DB, nbs[1], 3, late.Add(-time.Minute), "nB")
		if err := env.Server.Sweep(ctx, late); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "EXPIRED_QUARANTINED" {
			t.Fatalf("escrow state = %s, want EXPIRED_QUARANTINED", got)
		}
	})

	t.Run("neighbour corroboration can be disabled", func(t *testing.T) {
		env, _, nbs, eid := setup(t)
		env.Server.cfg.NeighborK = -1
		rainingCell(t, env.DB, nbs[0], 3, base.Add(time.Minute), "nA")
		rainingCell(t, env.DB, nbs[1], 3, base.Add(time.Minute), "nB")
		if err := env.Server.Sweep(ctx, base.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := escrowState(t, env.Repo, eid); got != "PENDING" {
			t.Fatalf("escrow state = %s, want PENDING (disabled)", got)
		}
	})
}

// Error-path contract: rejected telemetry must answer with a client error and
// the verifier's verdict, and a QUARANTINEd claim must not lock capacity.
func TestRejectedAndQuarantinedTelemetry(t *testing.T) {
	_, env, h, cell := setupIntegration(t)
	kp := mustKeyForEnrolled(t, env, "dev-r")
	now := env.Now()
	post := func(tel Telemetry, nonce string) (int, map[string]any) {
		resp, out := postTelemetry(t, h, signRequest(t, kp, now, nonce, tel))
		return resp.StatusCode, out
	}
	reserved := func() float64 {
		var v float64
		if err := env.DB.QueryRow(`SELECT reserved_litres FROM building WHERE external_id='bldg-1'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	bad := testTelemetry(cell, 0)
	bad.RoofAreaM2 = 0
	if code, body := post(bad, "r1"); code != 422 || body["decision"] == nil {
		t.Fatalf("invalid input: got %d %v, want 422 with decision", code, body)
	}

	over := testTelemetry(cell, 0)
	over.ClaimedLitres = 20000 // more than the 10000 L tank can take
	code, body := post(over, "r2")
	if code != 422 {
		t.Fatalf("tank overflow claim: got %d %v, want 422", code, body)
	}
	if d, _ := body["decision"].(map[string]any); d["Reason"] != "STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED" {
		t.Fatalf("tank overflow reason: %v", body)
	}

	// Quarantined (no peers, 50 mm claimed vs 2 mm radar bound) is recorded but
	// reserves nothing.
	q := testTelemetry(cell, 0)
	q.ClaimedLitres = 400
	if code, body := post(q, "r3"); code != 201 {
		t.Fatalf("quarantine: got %d %v", code, body)
	}
	if got := reserved(); got != 0 {
		t.Fatalf("quarantined claim reserved %v L, want 0", got)
	}

	// An accepted claim does reserve.
	ok := testTelemetry(cell, 5)
	ok.ClaimedLitres = 20
	if code, body := post(ok, "r4"); code != 201 {
		t.Fatalf("accept: got %d %v", code, body)
	}
	if got := reserved(); got != 20 {
		t.Fatalf("accepted claim reserved %v L, want 20", got)
	}

	// Unknown building is a 404, and a claim above the remaining reservable
	// capacity is a 409 (not a 500).
	ghost := testTelemetry(cell, 5)
	ghost.BuildingID = "no-such-building"
	if code, _ := post(ghost, "r5"); code != 404 {
		t.Fatalf("unknown building: got %d, want 404", code)
	}
	if _, err := env.DB.Exec(`UPDATE building SET capacity_litres=30 WHERE external_id='bldg-1'`); err != nil {
		t.Fatal(err)
	}
	big := testTelemetry(cell, 5)
	big.ClaimedLitres = 20 // 20 reserved + 20 > 30
	if code, _ := post(big, "r6"); code != 409 {
		t.Fatalf("over reservable capacity: got %d, want 409", code)
	}
}
