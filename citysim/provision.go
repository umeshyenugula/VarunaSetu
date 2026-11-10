package citysim

import (
	"context"
	"database/sql"
	"fmt"

	"hydroledger-prototype/hydroledger/verifier/identity"
)

// Probe device ids. The two enrolled ones live in the first core building.
const (
	ProbeOKDevice      = "dev-probe-ok"
	ProbeRevokedDevice = "dev-probe-rev"
	ProbeGhostDevice   = "dev-probe-ghost" // never enrolled
)

func probeKey(id string) *identity.KeyPair {
	kp, err := identity.NewKeyPairFromSeed(id, seedFor(id))
	if err != nil {
		panic(err)
	}
	return kp
}

// Provision enrols the city in the database: one building row and one device
// row (with its public key) per device. Enrolment is an administrative action
// that the runtime API deliberately does not offer (docs/ARCHITECTURE.md), so
// this is the one place the simulator touches the database directly. It is
// idempotent: reserved capacity is reset and revoked devices are reinstated.
func Provision(ctx context.Context, db *sql.DB, w *World) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range w.Devices {
		b := d.Building
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO building (external_id, capacity_litres, reserved_litres, location)
			VALUES ($1,$2,0,ST_SetSRID(ST_MakePoint($3,$4),4326)::geography)
			ON CONFLICT (external_id) DO UPDATE
			SET capacity_litres=EXCLUDED.capacity_litres, reserved_litres=0, location=EXCLUDED.location, updated_at=now()`,
			b.ID, b.ReservableL, b.Lng, b.Lat); err != nil {
			return fmt.Errorf("building %s: %w", b.ID, err)
		}
		if err := upsertDevice(ctx, tx, d.ID, b.ID, d.Key.PublicKey()); err != nil {
			return err
		}
	}
	host := w.ByCell[w.CenterCell][0].Building
	for _, id := range []string{ProbeOKDevice, ProbeRevokedDevice} {
		if err := upsertDevice(ctx, tx, id, host.ID, probeKey(id).PublicKey()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertDevice(ctx context.Context, tx *sql.Tx, id, building string, pub []byte) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO device (id, building_id, public_key, revoked_at)
		SELECT $1, b.id, $3, NULL FROM building b WHERE b.external_id=$2
		ON CONFLICT (id) DO UPDATE SET public_key=EXCLUDED.public_key, revoked_at=NULL`, id, building, pub)
	if err != nil {
		return fmt.Errorf("device %s: %w", id, err)
	}
	return nil
}

// Revoke marks a device as revoked (administrative action).
func Revoke(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `UPDATE device SET revoked_at=now() WHERE id=$1`, id)
	return err
}
