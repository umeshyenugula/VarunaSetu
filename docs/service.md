# Durable backend service

The Phase 3 service is in [`service/`](../service/). It uses `database/sql`
with PostgreSQL-compatible `$n` queries. The schema is
[`migrations/001_service.sql`](../migrations/001_service.sql).

## Schema

Migrations create PostGIS/pgcrypto extensions and tables for `building`,
`device`, `telemetry_reading`, `verification_decision`, `escrow_record` (states
`PENDING`, `CORROBORATED`, `EXPIRED_QUARANTINED`), and `ledger_entry`
placeholder. They include primary/foreign keys, idempotency uniqueness
`(device_id, idempotency_key)`, H3 cell/time indexes, BRIN time index, GiST
spatial indexes, append-only triggers for readings/decisions, a database role
with restricted grants, and `purge_telemetry_readings(retention interval)` for
scheduled retention.

## API

`POST /telemetry/readings` is protected by signed-telemetry middleware using
the Phase 1 identity package. The middleware performs stateless signature,
enrollment, revocation and timestamp-window checks, then resolves durable
idempotency against PostgreSQL so retries do not depend on in-memory replay
state. The signed payload is the exact JSON bytes signed by the device and
contains `building_id`, `claimed_litres`, `roof_area_m2`, `runoff_coeff`,
`stored_litres`, `capacity_litres`, `radar_mm`, and canonical `h3_index`.

Reusing a device/nonce returns the persisted result with `200`; reusing it for
different payload bytes returns `409`. Validation is strict (unknown fields and
trailing data rejected). Other endpoints: `GET /telemetry/readings/{id}`,
`GET /escrows/{id}`, and `GET /cells/{h3_index}/status` (canonical H3 index
validated).

## Verification and durability

Before evaluating, the service retrieves recent eligible persisted peer
readings from PostgreSQL for the reading's H3 cell/time window and supplies
them to the existing verifier implementation via a scratch verifier seeded in
event-time order. The existing verifier package is not modified.

`StoreReading` runs a serializable transaction that checks idempotency, locks
capacity, reserves capacity, inserts the reading, inserts the decision, inserts
a durable escrow for `PROVISIONAL_ESCROW`, and writes ledger reservation
entries. A wet-radar reading also applies durable radar-based escrow
corroboration for eligible pending escrows in the same cell inside the same
transactional flow; terminal escrow transitions use conditional
`state='PENDING'` updates so corroboration and expiry races resolve to exactly
one terminal state.

`StartExpiryWorker` runs a background sweep and can be started once per
process; `Close` stops it. Expiry uses row locking/`SKIP LOCKED` and is safe to
run after restart because it resolves pending escrows whose `expires_at` has
passed.

## Testing and load

Integration tests use `github.com/jackc/pgx/v5/stdlib`, a disposable PostgreSQL
+ PostGIS container (or `HYDROLEDGER_TEST_DATABASE_URL`), and the Phase 1
identity package for signed requests. Load testing is implemented as
`TestLoadService` (guarded by `HYDROLEDGER_RUN_LOAD=1`) and writes
`docs/results/phase3/load_results.json`; keep service latency measurements
separate from verifier microbenchmarks and record Go, PostgreSQL, PostGIS, OS,
CPU, and memory metadata.

Readings and decisions are append-only. Retention deletion/archival is a
scheduled deployment operation (`purge_telemetry_readings`), not an
application request path.
