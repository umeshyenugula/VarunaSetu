package citysim

import (
	"context"
	"os"
	"testing"
	"time"

	h3 "github.com/uber/h3-go/v4"
)

func TestWorldIsDeterministicAndConsistent(t *testing.T) {
	a, err := NewWorld(WorldConfig{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewWorld(WorldConfig{})
	if len(a.Cells) != 37 {
		t.Fatalf("cells = %d, want 37 (k=3 disk)", len(a.Cells))
	}
	if len(a.Devices) != len(b.Devices) {
		t.Fatal("device count not deterministic")
	}
	seen := map[string]bool{}
	for i, d := range a.Devices {
		if d.ID != b.Devices[i].ID || d.Building.Lat != b.Devices[i].Building.Lat {
			t.Fatalf("device %d differs between builds", i)
		}
		if seen[d.ID] {
			t.Fatalf("duplicate device id %s", d.ID)
		}
		seen[d.ID] = true
		c, err := h3.LatLngToCell(h3.NewLatLng(d.Building.Lat, d.Building.Lng), a.Resolution)
		if err != nil || c.String() != d.Building.CellID {
			t.Fatalf("building %s is not inside its H3 cell", d.Building.ID)
		}
	}
	for id, c := range a.Cells {
		if n := len(a.ByCell[id]); n != c.Devices {
			t.Fatalf("cell %s has %d devices, expected %d", id, n, c.Devices)
		}
		if len(c.Neighbors) != 6 {
			t.Fatalf("cell %s has %d neighbours, want 6", id, len(c.Neighbors))
		}
	}
	if len(a.Ring(1)) != 6 || len(a.Ring(2)) != 12 || len(a.Ring(3)) != 18 {
		t.Fatal("ring sizes wrong")
	}
	if len(a.SparseCells()) == 0 {
		t.Fatal("expected sparse cells for the too-few-peers cases")
	}
}

func TestStormEnvelopeAndRadarFaults(t *testing.T) {
	w, _ := NewWorld(WorldConfig{})
	c := w.Cells[w.CenterCell]
	wx := &Weather{
		Storms: []Storm{stormAt("s", c, 48, min(10), min(100), 600)}, // 40-min ramps, plateau 50-60 min
		Faults: []RadarFault{{Cells: cellSet(c.ID), From: 0, To: min(30), Mode: RadarDry}, {Cells: cellSet(c.ID), From: min(30), To: min(60), Mode: RadarScale, Factor: 2}},
	}
	if wx.Rate(c.X, c.Y, min(5)) != 0 || wx.Rate(c.X, c.Y, min(110)) != 0 {
		t.Fatal("rain outside the storm window")
	}
	prev := 0.0
	for m := 10; m <= 55; m += 5 { // raised-cosine ramp must be non-decreasing up to the plateau
		r := wx.Rate(c.X, c.Y, min(m))
		if r < prev-1e-9 {
			t.Fatalf("rate fell during ramp at %d min", m)
		}
		prev = r
	}
	if prev < 47 {
		t.Fatalf("plateau rate %.1f, want ~48", prev)
	}
	if got := wx.Radar(c, min(25), Window); got != 0 {
		t.Fatalf("dry fault: radar = %v", got)
	}
	clean := (&Weather{Storms: wx.Storms}).RadarClean(c, min(55), Window)
	if got := wx.RadarClean(c, min(55), Window); got < 1.9*clean || got > 2.1*clean {
		t.Fatalf("scale fault x2: got %v vs clean %v", got, clean)
	}
	if hashGauss("x", 3) != hashGauss("x", 3) || hashGauss("x", 3) == hashGauss("x", 4) {
		t.Fatal("hashGauss must be deterministic per key and vary with k")
	}
}

func TestBaselines(t *testing.T) {
	bl := NewBaselines()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ { // six honest peers around 4 mm
		bl.Judge("p"+string(rune('a'+i)), "cell", ts, 4+0.05*float64(i), 4)
	}
	v := bl.Judge("cheat", "cell", ts.Add(time.Minute), 9, 8) // radar over-reads: bound 11.2
	if !v["STATIC_THRESHOLD"] || !v["RADAR_ONLY"] || v["PEER_ONLY"] {
		t.Fatalf("expected static+radar accept and peer reject, got %v", v)
	}
	v = bl.Judge("honest", "cell", ts.Add(time.Minute), 4.1, 4)
	if !v["STATIC_THRESHOLD"] || !v["RADAR_ONLY"] || !v["PEER_ONLY"] {
		t.Fatalf("honest reading rejected: %v", v)
	}
	v = bl.Judge("far", "other", ts, 3, 0) // dry radar
	if v["RADAR_ONLY"] || !v["PEER_ONLY"] {
		t.Fatalf("radar-only must reject, peer-only has no peers: %v", v)
	}
}

// TestScenariosAgainstService runs the demo catalogue against a real database
// (HYDROLEDGER_TEST_DATABASE_URL) and requires every scenario's self-checks to
// pass. Runs are non-destructive: each starts at a later virtual time.
func TestScenariosAgainstService(t *testing.T) {
	url := os.Getenv("HYDROLEDGER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set HYDROLEDGER_TEST_DATABASE_URL to run the end-to-end scenarios")
	}
	ctx := context.Background()
	st, err := StartStack(ctx, url, "127.0.0.1:0", WorldConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, scn := range Scenarios(st.World) {
		if testing.Short() && scn.ID != "api-hardening" && scn.ID != "same-evidence" {
			continue
		}
		t.Run(scn.ID, func(t *testing.T) {
			res, err := st.Runner.Run(ctx, scn)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range res.Checks {
				if !c.Pass {
					t.Errorf("check failed: %s (%s)", c.Name, c.Detail)
				}
			}
			for _, p := range res.Probes {
				if !p.Pass {
					t.Errorf("probe %s: HTTP %d %s, want %d %s", p.Kind, p.Status, p.Reason, p.ExpectStatus, p.ExpectReason)
				}
			}
			if !res.Ledger.Verified {
				t.Errorf("ledger not verified: %s", res.Ledger.Error)
			}
			if res.Finals["PENDING"] != 0 {
				t.Errorf("%d readings still pending after the escrow TTL", res.Finals["PENDING"])
			}
		})
	}
}
