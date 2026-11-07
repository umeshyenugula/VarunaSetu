package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
)

var ErrNotFound = errors.New("service: not found")
var ErrIdempotencyConflict = errors.New("service: idempotency key reused with different payload")

type Repository struct{ DB *sql.DB }

func (r *Repository) ensureDB() error {
	if r == nil || r.DB == nil {
		return errors.New("service: nil database")
	}
	return nil
}

// StoreReading atomically reserves capacity, records the reading, persists its
// decision, creates a durable escrow when required, and applies durable
// radar-based corroboration to eligible pending escrows in the same cell.
func (r *Repository) StoreReading(ctx context.Context, deviceID, key string, ts time.Time, t Telemetry, d verifier.Decision, expiry time.Time, payloadHash []byte, cfg verifier.Config) (ReadingResult, error) {
	if err := r.ensureDB(); err != nil {
		return ReadingResult{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ReadingResult{}, fmt.Errorf("begin reading transaction: %w", err)
	}
	defer tx.Rollback()

	var existingID string
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `SELECT id, payload_hash FROM telemetry_reading WHERE device_id=$1 AND idempotency_key=$2 FOR UPDATE`, deviceID, key).Scan(&existingID, &existingHash)
	if err == nil {
		if string(existingHash) != string(payloadHash) {
			return ReadingResult{}, ErrIdempotencyConflict
		}
		var result ReadingResult
		result.ID = existingID
		err = tx.QueryRowContext(ctx, `SELECT state, reason, r_obs_mm, r_max_mm, peer_count, peer_median_mm, peer_mad_mm, z_score, created_at FROM verification_decision WHERE reading_id=$1`, existingID).
			Scan(&result.Decision.State, &result.Decision.Reason, &result.Decision.RObsMM, &result.Decision.RMaxMM, &result.Decision.PeerCount, &result.Decision.PeerMedianMM, &result.Decision.PeerMADMM, &result.Decision.ZScore, &result.CreatedAt)
		if err != nil {
			return ReadingResult{}, fmt.Errorf("read idempotent decision: %w", err)
		}
		result.Existing = true
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ReadingResult{}, fmt.Errorf("check idempotency: %w", err)
	}

	var buildingID string
	var available float64
	err = tx.QueryRowContext(ctx, `SELECT id, capacity_litres-reserved_litres FROM building WHERE external_id=$1 FOR UPDATE`, t.BuildingID).Scan(&buildingID, &available)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReadingResult{}, ErrNotFound
		}
		return ReadingResult{}, fmt.Errorf("lock capacity: %w", err)
	}
	if t.StoredLitres > t.CapacityLitres || t.ClaimedLitres > t.CapacityLitres-t.StoredLitres || t.ClaimedLitres > available {
		return ReadingResult{}, fmt.Errorf("capacity reservation failed")
	}
	var id string
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO telemetry_reading (idempotency_key,device_id,building_id,event_time,claimed_litres,roof_area_m2,runoff_coeff,stored_litres,capacity_litres,radar_mm,h3_index,payload_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, received_at`, key, deviceID, buildingID, ts, t.ClaimedLitres, t.RoofAreaM2, t.RunoffCoeff, t.StoredLitres, t.CapacityLitres, t.RadarMM, t.CellID, payloadHash).Scan(&id, &createdAt)
	if err != nil {
		return ReadingResult{}, fmt.Errorf("insert reading: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE building SET reserved_litres=reserved_litres+$1, updated_at=now() WHERE id=$2`, t.ClaimedLitres, buildingID)
	if err != nil {
		return ReadingResult{}, fmt.Errorf("reserve capacity: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entry (reading_id,amount_litres,entry_type) VALUES ($1,$2,'RESERVE')`, id, t.ClaimedLitres); err != nil {
		return ReadingResult{}, fmt.Errorf("insert reservation ledger entry: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO verification_decision (reading_id,state,reason,r_obs_mm,r_max_mm,peer_count,peer_median_mm,peer_mad_mm,z_score) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, d.State, d.Reason, d.RObsMM, d.RMaxMM, d.PeerCount, d.PeerMedianMM, d.PeerMADMM, d.ZScore)
	if err != nil {
		return ReadingResult{}, fmt.Errorf("insert decision: %w", err)
	}
	if d.State == verifier.StateProvisionalEscrow {
		if _, err = tx.ExecContext(ctx, `INSERT INTO escrow_record (reading_id,expires_at) VALUES ($1,$2)`, id, expiry); err != nil {
			return ReadingResult{}, fmt.Errorf("insert escrow: %w", err)
		}
	}

	corroborated := 0
	if t.RadarMM > cfg.RadarDryMM {
		n, err := corroborateEligibleRadarTx(ctx, tx, t.CellID, ts, cfg.EscrowTTL)
		if err != nil {
			return ReadingResult{}, fmt.Errorf("corroborate eligible escrows: %w", err)
		}
		corroborated = n
	}

	if err = tx.Commit(); err != nil {
		return ReadingResult{}, fmt.Errorf("commit reading transaction: %w", err)
	}
	return ReadingResult{ID: id, Decision: d, CreatedAt: createdAt, Corroborated: corroborated}, nil
}

// corroborateEligibleRadarTx durably corroborates pending escrows in cell that
// are older than readingTime and within ttl, mirroring verifier radar
// corroboration eligibility. Conditional updates make races with expiry safe.
func corroborateEligibleRadarTx(ctx context.Context, tx *sql.Tx, cell string, readingTime time.Time, ttl time.Duration) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT er.id, er.reading_id, tr.claimed_litres
		FROM escrow_record er
		JOIN telemetry_reading tr ON tr.id = er.reading_id
		WHERE er.state = 'PENDING'
		  AND tr.h3_index = $1
		  AND tr.event_time < $2
		  AND $2 - tr.event_time <= $3::interval
		  AND er.expires_at >= $2
		FOR UPDATE OF er SKIP LOCKED`, cell, readingTime, ttl)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, readingID string
		var amount float64
		if err := rows.Scan(&id, &readingID, &amount); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE escrow_record SET state='CORROBORATED', corroborated_at=$1, resolved_at=$1 WHERE id=$2 AND state='PENDING'`, readingTime, id)
		if err != nil {
			return 0, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entry (reading_id,escrow_id,amount_litres,entry_type) VALUES ($1,$2,$3,'COMMIT')`, readingID, id, amount); err != nil {
			return 0, err
		}
		n++
	}
	return n, rows.Err()
}

// CorroborateEscrow conditionally transitions a single PENDING escrow.
func (r *Repository) CorroborateEscrow(ctx context.Context, id string, now time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var readingID string
	var amount float64
	err = tx.QueryRowContext(ctx, `SELECT er.reading_id,tr.claimed_litres FROM escrow_record er JOIN telemetry_reading tr ON tr.id=er.reading_id WHERE er.id=$1 AND er.state='PENDING' FOR UPDATE`, id).Scan(&readingID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE escrow_record SET state='CORROBORATED',corroborated_at=$1,resolved_at=$1 WHERE id=$2 AND state='PENDING'`, now, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entry (reading_id,escrow_id,amount_litres,entry_type) VALUES ($1,$2,$3,'COMMIT')`, readingID, id, amount); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) GetReading(ctx context.Context, id string) (ReadingResult, error) {
	if err := r.ensureDB(); err != nil {
		return ReadingResult{}, err
	}
	var out ReadingResult
	err := r.DB.QueryRowContext(ctx, `SELECT tr.id, vd.state, vd.reason, vd.r_obs_mm, vd.r_max_mm, vd.peer_count, vd.peer_median_mm, vd.peer_mad_mm, vd.z_score, vd.created_at FROM telemetry_reading tr JOIN verification_decision vd ON vd.reading_id=tr.id WHERE tr.id=$1`, id).
		Scan(&out.ID, &out.Decision.State, &out.Decision.Reason, &out.Decision.RObsMM, &out.Decision.RMaxMM, &out.Decision.PeerCount, &out.Decision.PeerMedianMM, &out.Decision.PeerMADMM, &out.Decision.ZScore, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadingResult{}, ErrNotFound
	}
	if err != nil {
		return ReadingResult{}, fmt.Errorf("get reading: %w", err)
	}
	return out, nil
}

func (r *Repository) GetReadingByIdempotency(ctx context.Context, deviceID, key string) (ReadingResult, []byte, error) {
	if err := r.ensureDB(); err != nil {
		return ReadingResult{}, nil, err
	}
	var out ReadingResult
	var hash []byte
	err := r.DB.QueryRowContext(ctx, `SELECT tr.id, tr.payload_hash, vd.state, vd.reason, vd.r_obs_mm, vd.r_max_mm, vd.peer_count, vd.peer_median_mm, vd.peer_mad_mm, vd.z_score, vd.created_at
		FROM telemetry_reading tr JOIN verification_decision vd ON vd.reading_id=tr.id
		WHERE tr.device_id=$1 AND tr.idempotency_key=$2`, deviceID, key).
		Scan(&out.ID, &hash, &out.Decision.State, &out.Decision.Reason, &out.Decision.RObsMM, &out.Decision.RMaxMM, &out.Decision.PeerCount, &out.Decision.PeerMedianMM, &out.Decision.PeerMADMM, &out.Decision.ZScore, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadingResult{}, nil, ErrNotFound
	}
	if err != nil {
		return ReadingResult{}, nil, fmt.Errorf("get reading by idempotency: %w", err)
	}
	out.Existing = true
	return out, hash, nil
}

func (r *Repository) GetEscrow(ctx context.Context, id string) (Escrow, error) {
	if err := r.ensureDB(); err != nil {
		return Escrow{}, err
	}
	var e Escrow
	err := r.DB.QueryRowContext(ctx, `SELECT id,reading_id,state,expires_at,corroborated_at,resolved_at FROM escrow_record WHERE id=$1`, id).Scan(&e.ID, &e.ReadingID, &e.State, &e.ExpiresAt, &e.CorroboratedAt, &e.ResolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Escrow{}, ErrNotFound
	}
	if err != nil {
		return Escrow{}, fmt.Errorf("get escrow: %w", err)
	}
	return e, nil
}

type Peer struct {
	DeviceID  string
	Timestamp time.Time
	DepthMM   float64
}

func (r *Repository) RecentPeers(ctx context.Context, cell string, before time.Time, maxAge time.Duration, exclude string, limit int) ([]Peer, error) {
	readings, err := r.RecentPeerReadings(ctx, cell, before, maxAge, exclude, limit)
	if err != nil {
		return nil, err
	}
	peers := make([]Peer, 0, len(readings))
	for _, rr := range readings {
		depth := 0.0
		if rr.Reading.RoofAreaM2*rr.Reading.RunoffCoeff > 0 {
			depth = rr.Reading.ClaimedLitres / (rr.Reading.RoofAreaM2 * rr.Reading.RunoffCoeff)
		}
		peers = append(peers, Peer{DeviceID: rr.Reading.DeviceID, Timestamp: rr.Reading.Timestamp, DepthMM: depth})
	}
	return peers, nil
}

// RecentPeerReadings returns the latest eligible persisted reading per device
// in a cell/time window, in ascending event-time order for deterministic
// verifier seeding. It supplies DB-backed peers to the existing verifier logic.
func (r *Repository) RecentPeerReadings(ctx context.Context, cell string, before time.Time, maxAge time.Duration, exclude string, limit int) ([]PeerReading, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 128
	}
	since := before.Add(-maxAge)
	rows, err := r.DB.QueryContext(ctx, `
		SELECT device_id, event_time, claimed_litres, roof_area_m2, runoff_coeff,
		       stored_litres, capacity_litres, radar_mm, h3_index
		FROM (
			SELECT DISTINCT ON (device_id) device_id, event_time, claimed_litres, roof_area_m2, runoff_coeff,
			       stored_litres, capacity_litres, radar_mm, h3_index
			FROM telemetry_reading
			WHERE h3_index=$1 AND event_time > $2 AND event_time <= $3 AND device_id<>$4
			ORDER BY device_id, event_time DESC
			LIMIT $5
		) latest
		ORDER BY event_time ASC`, cell, since, before, exclude, limit)
	if err != nil {
		return nil, fmt.Errorf("retrieve peers: %w", err)
	}
	defer rows.Close()
	var out []PeerReading
	for rows.Next() {
		var rd verifier.Reading
		if err := rows.Scan(&rd.DeviceID, &rd.Timestamp, &rd.ClaimedLitres, &rd.RoofAreaM2, &rd.RunoffCoeff, &rd.StoredLitres, &rd.CapacityLitres, &rd.RadarMM, &rd.CellID); err != nil {
			return nil, err
		}
		out = append(out, PeerReading{Reading: rd})
	}
	return out, rows.Err()
}

func (r *Repository) ExpireEscrows(ctx context.Context, now time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT er.id,er.reading_id,tr.building_id,tr.claimed_litres FROM escrow_record er JOIN telemetry_reading tr ON tr.id=er.reading_id WHERE er.state='PENDING' AND er.expires_at < $1 FOR UPDATE OF er SKIP LOCKED`, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, readingID, buildingID string
		var amount float64
		if err := rows.Scan(&id, &readingID, &buildingID, &amount); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE escrow_record SET state='EXPIRED_QUARANTINED',resolved_at=$1 WHERE id=$2 AND state='PENDING'`, now, id)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE building SET reserved_litres=GREATEST(0,reserved_litres-$1),updated_at=now() WHERE id=$2`, amount, buildingID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ledger_entry (reading_id,escrow_id,amount_litres,entry_type) VALUES ($1,$2,$3,'RELEASE')`, readingID, id, amount); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

// ListReadings returns dashboard-facing reading projections (additive read API).
func (r *Repository) ListReadings(ctx context.Context, limit int) ([]ReadingView, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT tr.id, tr.device_id, tr.event_time, tr.h3_index,
		       tr.claimed_litres / NULLIF(tr.roof_area_m2 * tr.runoff_coeff, 0),
		       tr.radar_mm, vd.peer_count, vd.peer_median_mm, vd.state, vd.reason
		FROM telemetry_reading tr
		JOIN verification_decision vd ON vd.reading_id = tr.id
		ORDER BY tr.event_time DESC, tr.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list readings: %w", err)
	}
	defer rows.Close()
	var out []ReadingView
	for rows.Next() {
		var v ReadingView
		var rain sql.NullFloat64
		var peerMedian sql.NullFloat64
		if err := rows.Scan(&v.ID, &v.DeviceID, &v.Timestamp, &v.H3Index, &rain, &v.RadarRainfallMM, &v.PeerCount, &peerMedian, &v.Decision, &v.Reason); err != nil {
			return nil, err
		}
		if rain.Valid {
			v.RainfallMM = rain.Float64
		}
		if peerMedian.Valid && v.PeerCount > 0 {
			pm := peerMedian.Float64
			v.PeerConsensusMM = &pm
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListEscrows returns dashboard-facing escrow projections (additive read API).
func (r *Repository) ListEscrows(ctx context.Context, limit int) ([]EscrowView, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT er.id, tr.device_id, tr.h3_index, er.state, er.created_at, er.expires_at, er.corroborated_at
		FROM escrow_record er
		JOIN telemetry_reading tr ON tr.id = er.reading_id
		ORDER BY er.created_at DESC, er.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list escrows: %w", err)
	}
	defer rows.Close()
	var out []EscrowView
	for rows.Next() {
		var v EscrowView
		if err := rows.Scan(&v.ID, &v.DeviceID, &v.H3Index, &v.State, &v.CreatedAt, &v.ExpiresAt, &v.CorroboratedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListCellSummaries returns recent per-cell decision summaries (additive read API).
func (r *Repository) ListCellSummaries(ctx context.Context, limit int) ([]CellSummary, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT h3_index, state, readings FROM (
			SELECT tr.h3_index, vd.state, COUNT(*)::int AS readings,
			       ROW_NUMBER() OVER (PARTITION BY tr.h3_index ORDER BY
			         CASE vd.state WHEN 'QUARANTINE' THEN 3 WHEN 'PROVISIONAL_ESCROW' THEN 2 WHEN 'ACCEPT' THEN 1 ELSE 0 END DESC,
			         COUNT(*) DESC) AS rn
			FROM telemetry_reading tr
			JOIN verification_decision vd ON vd.reading_id = tr.id
			GROUP BY tr.h3_index, vd.state
		) s WHERE rn = 1
		ORDER BY readings DESC, h3_index
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list cell summaries: %w", err)
	}
	defer rows.Close()
	var out []CellSummary
	for rows.Next() {
		var v CellSummary
		if err := rows.Scan(&v.H3Index, &v.State, &v.Readings); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Analytics computes aggregate metrics from persisted readings/escrows.
func (r *Repository) Analytics(ctx context.Context) (AnalyticsSummary, error) {
	if err := r.ensureDB(); err != nil {
		return AnalyticsSummary{}, err
	}
	var a AnalyticsSummary
	a.Source = "database"
	err := r.DB.QueryRowContext(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE vd.state='ACCEPT')::int,
			COUNT(*) FILTER (WHERE vd.state='PROVISIONAL_ESCROW')::int,
			COUNT(*) FILTER (WHERE vd.state='QUARANTINE')::int,
			COUNT(DISTINCT tr.device_id)::int
		FROM telemetry_reading tr
		JOIN verification_decision vd ON vd.reading_id = tr.id`).Scan(
		&a.TotalReadings, &a.Accepted, &a.Provisional, &a.Quarantined, &a.ActiveDevices)
	if err != nil {
		return AnalyticsSummary{}, fmt.Errorf("analytics readings: %w", err)
	}
	var totalEscrows, resolvedEscrows int
	err = r.DB.QueryRowContext(ctx, `SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE state <> 'PENDING')::int FROM escrow_record`).Scan(&totalEscrows, &resolvedEscrows)
	if err != nil {
		return AnalyticsSummary{}, fmt.Errorf("analytics escrows: %w", err)
	}
	if totalEscrows > 0 {
		rate := float64(resolvedEscrows) / float64(totalEscrows)
		a.EscrowResolutionRate = &rate
	}
	return a, nil
}

// ListLedgerEntries returns audit ledger projections when migration 002 tables exist.
func (r *Repository) ListLedgerEntries(ctx context.Context, limit int) ([]LedgerEntryView, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `
		SELECT e.seq, e.event_ref, e.prev_hash, e.entry_hash, e.event_time,
		       CASE WHEN c.id IS NOT NULL THEN 'present' ELSE 'none' END AS checkpoint_status,
		       c.seq
		FROM audit_ledger_entry e
		LEFT JOIN LATERAL (
			SELECT id, seq FROM audit_ledger_checkpoint c WHERE c.seq = e.seq ORDER BY c.created_at DESC LIMIT 1
		) c ON true
		ORDER BY e.seq DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()
	var out []LedgerEntryView
	for rows.Next() {
		var v LedgerEntryView
		var cpSeq sql.NullInt64
		if err := rows.Scan(&v.Seq, &v.EventRef, &v.PrevHash, &v.Hash, &v.Timestamp, &v.CheckpointSignatureStatus, &cpSeq); err != nil {
			return nil, err
		}
		if cpSeq.Valid {
			s := cpSeq.Int64
			v.CheckpointSeq = &s
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
