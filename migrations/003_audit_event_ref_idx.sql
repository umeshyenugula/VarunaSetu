-- Additive: lets the escrow read API find an escrow's settling audit event by
-- event_ref without scanning the whole audit chain.
CREATE INDEX IF NOT EXISTS audit_ledger_entry_event_ref_idx ON audit_ledger_entry (event_ref);
