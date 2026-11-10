package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/spatial"
)

// CellRain summarises recent rain evidence in one H3 cell as the verifier
// sees it: the latest reading per device inside PeerMaxAge, reduced to a
// device count and the median effective depth (mm).
type CellRain struct {
	H3Index  string  `json:"h3Index"`
	Devices  int     `json:"devices"`
	MedianMM float64 `json:"medianMm"`
	Rains    bool    `json:"rains"` // enough devices AND median >= PeerRainMM
}

// CellRainAt computes CellRain for cell as of now. It mirrors the verifier's
// neighbour test (Verifier.neighborsCorroborate): a device counts when its
// latest observation is at most PeerMaxAge old, and the cell "rains" when at
// least NeighborMinDevices devices are present with a median >= PeerRainMM.
func (r *Repository) CellRainAt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cell string, now time.Time, cfg verifier.Config) (CellRain, error) {
	out := CellRain{H3Index: cell}
	var median sql.NullFloat64
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)::int, percentile_cont(0.5) WITHIN GROUP (ORDER BY depth)
		FROM (
			SELECT DISTINCT ON (device_id)
			       claimed_litres / (roof_area_m2 * runoff_coeff) AS depth
			FROM telemetry_reading
			WHERE h3_index = $1 AND event_time >= $2 AND event_time <= $3
			ORDER BY device_id, event_time DESC
		) latest`, cell, now.Add(-cfg.PeerMaxAge), now).Scan(&out.Devices, &median)
	if err != nil {
		return CellRain{}, fmt.Errorf("cell rain %s: %w", cell, err)
	}
	if median.Valid {
		out.MedianMM = median.Float64
	}
	out.Rains = out.Devices >= cfg.NeighborMinDevices && out.MedianMM >= cfg.PeerRainMM
	return out, nil
}

// NeighborEvidence returns, for an escrowed cell, the k-ring neighbours and
// their rain evidence at now, plus whether the corroboration rule is met
// (at least NeighborMinCells neighbours that rain).
func (r *Repository) NeighborEvidence(ctx context.Context, cell string, now time.Time, cfg verifier.Config, k int) ([]CellRain, bool, error) {
	if err := r.ensureDB(); err != nil {
		return nil, false, err
	}
	ns, err := spatial.NeighborsOf(cell, k)
	if err != nil {
		return nil, false, err
	}
	out := make([]CellRain, 0, len(ns))
	qualifying := 0
	for _, n := range ns {
		cr, err := r.CellRainAt(ctx, r.DB, n, now, cfg)
		if err != nil {
			return nil, false, err
		}
		if cr.Rains {
			qualifying++
		}
		out = append(out, cr)
	}
	return out, len(ns) >= cfg.NeighborMinCells && qualifying >= cfg.NeighborMinCells, nil
}

// CorroborateEscrowsByNeighbors durably resolves PENDING escrows that are
// still inside their TTL when at least cfg.NeighborMinCells cells of the
// escrowed cell's H3 k-ring each show rain (see CellRainAt). It is the
// durable counterpart of the neighbour rule in Verifier.ResolveEscrows, which
// the service path did not previously exercise.
//
// Escrows past their TTL are left for ExpireEscrows, matching the verifier's
// precedence (expiry before neighbour corroboration). Neighbour evidence does
// not look at other devices' verdicts, exactly like the verifier; that is the
// documented cross-cell collusion limitation, not a new one.
func (r *Repository) CorroborateEscrowsByNeighbors(ctx context.Context, now time.Time, cfg verifier.Config, k int) (int, error) {
	if err := r.ensureDB(); err != nil {
		return 0, err
	}
	if k < 1 {
		return 0, nil
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT er.id, er.reading_id, tr.h3_index, tr.claimed_litres
		FROM escrow_record er
		JOIN telemetry_reading tr ON tr.id = er.reading_id
		WHERE er.state = 'PENDING' AND er.expires_at >= $1 AND tr.event_time <= $1
		ORDER BY tr.event_time, er.id
		FOR UPDATE OF er SKIP LOCKED`, now)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id, readingID, cell string
		amount              float64
	}
	var all []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.readingID, &p.cell, &p.amount); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, p)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	verdict := map[string]bool{} // escrowed cell -> neighbour rule met
	rain := map[string]CellRain{}
	n := 0
	for _, p := range all {
		ok, seen := verdict[p.cell]
		if !seen {
			ns, err := spatial.NeighborsOf(p.cell, k)
			if err != nil {
				return 0, err
			}
			q := 0
			for _, nb := range ns {
				cr, cached := rain[nb]
				if !cached {
					if cr, err = r.CellRainAt(ctx, tx, nb, now, cfg); err != nil {
						return 0, err
					}
					rain[nb] = cr
				}
				if cr.Rains {
					q++
				}
			}
			ok = len(ns) >= cfg.NeighborMinCells && q >= cfg.NeighborMinCells
			verdict[p.cell] = ok
		}
		if !ok {
			continue
		}
		res, err := tx.ExecContext(ctx, `UPDATE escrow_record SET state='CORROBORATED', corroborated_at=$1, resolved_at=$1 WHERE id=$2 AND state='PENDING'`, now, p.id)
		if err != nil {
			return 0, err
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entry (reading_id,escrow_id,amount_litres,entry_type) VALUES ($1,$2,$3,'COMMIT')`, p.readingID, p.id, p.amount); err != nil {
			return 0, err
		}
		if err := appendAuditEntryTx(ctx, tx, "escrow_corroborated_h3:"+p.id, now); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
