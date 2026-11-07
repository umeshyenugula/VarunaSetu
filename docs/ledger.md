# Audit Ledger and Signed Checkpoints (Phase 4)

This document specifies the `ledger/` prototype: an append-only SHA-256
hash-chained audit ledger and Ed25519 signed checkpoints. It is a
prototype; it does not modify `service/` and makes no production security or
availability claims.

## Architecture

- **Append-only chain**: each entry links to the previous entry hash. The
  ledger assigns `seq`, `prev_hash`, and `hash`; callers provide `event_ref`
  and `timestamp`.
- **Verification**: `Verify`/`VerifyEntries` recompute every hash and check
  genesis linkage, contiguous sequence numbers, and `prev_hash` linkage.
- **Checkpoints**: Ed25519 signatures attest `(seq, head_hash, timestamp,
  key_id)` at interval *N* (configurable). A `Witness` interface allows
  external anchoring (HSM/KMS/transparency log/witness service).
- **Durable schema (additive)**: `migrations/002_ledger.sql` defines
  `audit_ledger_entry` and `audit_ledger_checkpoint`; it does not alter
  service tables.

## Canonical serialization (normative)

Each entry is hashed as UTF-8 bytes of the following fields in order, joined by
ASCII LF (`\n`, 0x0A), with **no trailing newline**:

| Order | Field | Format |
|---:|---|---|
| 1 | `schema` | literal `hydroledger.ledger.entry.v1` |
| 2 | `seq` | base-10 ASCII unsigned integer; no sign, no leading zeros except `0` |
| 3 | `event_ref` | UTF-8 text; MUST NOT contain `\n` or `\r`; MUST NOT be empty |
| 4 | `prev_hash` | lower-case hexadecimal, exactly 64 characters (32 bytes); genesis uses 64 `0` chars |
| 5 | `timestamp` | `time.RFC3339Nano` formatted in UTC (Go: `2006-01-02T15:04:05.999999999Z07:00` after `UTC()`), e.g. `2026-01-02T15:04:05.123456789Z` |

`current_hash = hex_lower(SHA-256(canonical_bytes))`. `prev_hash` input is
normalized to lower-case hex. Timestamps are normalized to UTC before
serialization.

## Checkpoint mechanism

Checkpoint signed message (UTF-8, LF-separated, no trailing newline):

1. `schema` = `hydroledger.ledger.checkpoint.v1`
2. `seq` = base-10 head sequence
3. `head_hash` = lower-case 64-char hex head hash
4. `timestamp` = RFC3339Nano UTC
5. `key_id` = key identifier (may be empty)

Signature: Ed25519 over the message bytes. Verification checks key size,
schema, head match, signature size, and `ed25519.Verify`.

`Checkpointer` signs/publishes when `(head.Seq+1) % N == 0`.

## Key management assumptions

- Private keys must be generated, stored, and used under deployment controls
  (HSM/KMS recommended); this prototype accepts `ed25519.PrivateKey` bytes.
- **Key rotation**: publish new checkpoints with a new `key_id` and
  corresponding public key; retain old public keys for historical verification;
  rotate before compromise suspicion windows and after policy intervals.
  Overlapping validity/audit records should bind `key_id` to the verification
  trust store. Rotation does not retroactively protect entries signed after a
  compromise unless externally witnessed.
- **External witness/anchoring**: the `Witness` interface is an integration
  point; production deployments should anchor checkpoint hashes/IDs to an
  independent system outside the ledger administrator's sole control.

## Trust assumptions and limitations

- **Hash chain limitation (documented/tested)**: SHA-256 chaining detects
  post-hoc modification of committed entries when verifiers compare against a
  trusted head/checkpoint. It **cannot** prevent an administrator with write
  access to storage (and/or signing keys) from **rewriting the entire chain**
  and, if they control keys, issuing new checkpoints. Tests demonstrate
  historical tamper detection and rejection of checkpoints forged under an
  untrusted key.
- Signed checkpoints bind a head to a key; they do not prove global
  uniqueness without external witnessing/transparency.
- In-memory ledger state is not durable by itself; migration 002 provides a
  storage shape but service integration is out of scope.
- Event references and timestamps are caller-supplied; the ledger does not
  authenticate event producers.

## Tests (Task 1/2)

`ledger/ledger_test.go` covers append/verify/canonical hashing, historical
entry tamper detection, valid checkpoint, modified checkpoint, rewritten chain
with forged checkpoint rejected by legitimate key, invalid signature, and
checkpoint interval/witness hook. Run (WDAC-safe): `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test ./ledger/... -count=1`.
