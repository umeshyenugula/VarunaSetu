-- Phase 4: durable audit ledger/checkpoint storage (additive; does not modify service tables).
CREATE TABLE IF NOT EXISTS audit_ledger_entry (
    seq bigint PRIMARY KEY CHECK (seq >= 0),
    event_ref text NOT NULL,
    prev_hash char(64) NOT NULL,
    entry_hash char(64) NOT NULL,
    event_time timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (prev_hash ~ '^[0-9a-f]{64}$'),
    CHECK (entry_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS audit_ledger_entry_hash_idx ON audit_ledger_entry (entry_hash);
CREATE INDEX IF NOT EXISTS audit_ledger_entry_event_time_idx ON audit_ledger_entry (event_time DESC);

CREATE TABLE IF NOT EXISTS audit_ledger_checkpoint (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    schema text NOT NULL,
    seq bigint NOT NULL REFERENCES audit_ledger_entry(seq),
    head_hash char(64) NOT NULL,
    checkpoint_time timestamptz NOT NULL,
    key_id text,
    signature bytea NOT NULL,
    witness_ref text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (head_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS audit_ledger_checkpoint_seq_idx ON audit_ledger_checkpoint (seq DESC);

COMMENT ON TABLE audit_ledger_entry IS 'Append-only hash-chained audit entries. Hash chain detects tampering given a trusted head/checkpoint; it does not by itself prevent an administrator from rewriting the entire chain.';
COMMENT ON TABLE audit_ledger_checkpoint IS 'Ed25519-signed checkpoints and optional external witness/anchoring references. Key management and witness trust are deployment assumptions.';
