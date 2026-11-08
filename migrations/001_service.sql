-- Supabase-compatible service schema.
-- Run this in the Supabase SQL Editor or with a privileged database connection.
-- The application should connect using the Supabase pooler/direct database role;
-- Supabase-managed roles are intentionally not created or revoked here.

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
DO $$ BEGIN
    CREATE TYPE escrow_state AS ENUM ('PENDING', 'CORROBORATED', 'EXPIRED_QUARANTINED');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE TYPE decision_state AS ENUM ('ACCEPT', 'PROVISIONAL_ESCROW', 'QUARANTINE');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS building (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id text NOT NULL UNIQUE,
    capacity_litres double precision NOT NULL CHECK (capacity_litres >= 0),
    reserved_litres double precision NOT NULL DEFAULT 0 CHECK (reserved_litres >= 0),
    location geography(Point, 4326),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS device (
    id text PRIMARY KEY,
    building_id uuid NOT NULL REFERENCES building(id),
    public_key bytea NOT NULL CHECK (octet_length(public_key) = 32),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telemetry_reading (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key text NOT NULL,
    device_id text NOT NULL REFERENCES device(id),
    building_id uuid NOT NULL REFERENCES building(id),
    event_time timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    claimed_litres double precision NOT NULL CHECK (claimed_litres >= 0),
    roof_area_m2 double precision NOT NULL CHECK (roof_area_m2 > 0),
    runoff_coeff double precision NOT NULL CHECK (runoff_coeff > 0 AND runoff_coeff <= 1),
    stored_litres double precision NOT NULL CHECK (stored_litres >= 0),
    capacity_litres double precision NOT NULL CHECK (capacity_litres >= 0),
    radar_mm double precision NOT NULL CHECK (radar_mm >= 0),
    h3_index text NOT NULL,
    geom geography(Point, 4326),
    payload_hash bytea NOT NULL,
    UNIQUE (device_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS telemetry_reading_cell_time_idx
    ON telemetry_reading (h3_index, event_time DESC);
CREATE INDEX IF NOT EXISTS telemetry_reading_device_time_idx
    ON telemetry_reading (device_id, event_time DESC);
CREATE INDEX IF NOT EXISTS telemetry_reading_time_brin_idx
    ON telemetry_reading USING brin (event_time);
CREATE INDEX IF NOT EXISTS telemetry_reading_geom_gist_idx
    ON telemetry_reading USING gist (geom);

CREATE TABLE IF NOT EXISTS verification_decision (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reading_id uuid NOT NULL UNIQUE REFERENCES telemetry_reading(id) ON DELETE CASCADE,
    state decision_state NOT NULL,
    reason text NOT NULL,
    r_obs_mm double precision NOT NULL DEFAULT 0,
    r_max_mm double precision NOT NULL DEFAULT 0,
    peer_count integer NOT NULL DEFAULT 0 CHECK (peer_count >= 0),
    peer_median_mm double precision NOT NULL DEFAULT 0,
    peer_mad_mm double precision NOT NULL DEFAULT 0,
    z_score double precision NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS escrow_record (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reading_id uuid NOT NULL UNIQUE REFERENCES telemetry_reading(id) ON DELETE CASCADE,
    state escrow_state NOT NULL DEFAULT 'PENDING',
    expires_at timestamptz NOT NULL,
    corroborated_at timestamptz,
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((state = 'PENDING' AND resolved_at IS NULL) OR
           (state <> 'PENDING' AND resolved_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS escrow_pending_expiry_idx
    ON escrow_record (expires_at) WHERE state = 'PENDING';

CREATE TABLE IF NOT EXISTS ledger_entry (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reading_id uuid REFERENCES telemetry_reading(id) ON DELETE SET NULL,
    escrow_id uuid REFERENCES escrow_record(id),
    amount_litres double precision NOT NULL CHECK (amount_litres >= 0),
    entry_type text NOT NULL CHECK (entry_type IN ('RESERVE', 'COMMIT', 'RELEASE')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION prevent_telemetry_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    RAISE EXCEPTION 'telemetry readings are append-only';
END $$;
CREATE OR REPLACE FUNCTION prevent_decision_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    RAISE EXCEPTION 'verification decisions are immutable';
END $$;
DROP TRIGGER IF EXISTS telemetry_reading_immutable ON telemetry_reading;
CREATE TRIGGER telemetry_reading_immutable BEFORE UPDATE ON telemetry_reading
FOR EACH ROW EXECUTE FUNCTION prevent_telemetry_mutation();
DROP TRIGGER IF EXISTS verification_decision_immutable ON verification_decision;
CREATE TRIGGER verification_decision_immutable BEFORE UPDATE OR DELETE ON verification_decision
FOR EACH ROW EXECUTE FUNCTION prevent_decision_mutation();

ALTER TABLE telemetry_reading SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE verification_decision SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE escrow_record SET (autovacuum_vacuum_scale_factor = 0.02);
COMMENT ON TABLE telemetry_reading IS 'Retention policy: archive/delete rows older than the operational retention window via scheduled maintenance.';

CREATE OR REPLACE FUNCTION purge_telemetry_readings(retention interval)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE removed bigint;
BEGIN
    IF retention <= interval '0 seconds' THEN
        RAISE EXCEPTION 'retention must be positive';
    END IF;
    DELETE FROM telemetry_reading WHERE received_at < now() - retention;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END $$;
