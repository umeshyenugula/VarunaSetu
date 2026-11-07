# Phase 3 handoff

## Implemented

- PostgreSQL/PostGIS migration `migrations/001_service.sql`: extensions,
  `building/device/telemetry_reading/verification_decision/escrow_record/ledger_entry`,
  PK/FK/UNIQUE/CHECK constraints, H3 `(h3_index,event_time)` and device/time
  indexes, BRIN time index, GiST spatial indexes, idempotency key
  `(device_id,idempotency_key)`, escrow state enum/terminal-state checks,
  append-only triggers for readings/decisions, restricted DB role/grants, and
  retention helper `purge_telemetry_readings`.
- HTTP API: `POST /telemetry/readings`, `GET /telemetry/readings/{id}`,
  `GET /escrows/{id}`, `GET /cells/{h3_index}/status`. Strict request
  validation, signed-telemetry middleware using Phase 1 identity package
  (stateless signature/enrollment/revocation/timestamp checks plus durable
  idempotency lookup), structured logging, request timeouts, context
  cancellation/deadline handling, and appropriate status codes.
- Repository layer: DB-backed `RecentPeerReadings`/`RecentPeers` retrieval;
  ingestion seeds a scratch verifier with persisted eligible peers (event-time
  order) before calling existing `verifier.Evaluate`, preserving existing
  verification logic.
- Durable escrow: `PENDING` -> `CORROBORATED`/`EXPIRED_QUARANTINED` with
  conditional `state='PENDING'` transitions, row locks/`SKIP LOCKED`, serializable
  transactions for capacity reservation + decision + escrow/ledger persistence,
  durable radar-based corroboration, and restart-recoverable expiry worker.
- Tests/harness: disposable PostgreSQL+PostGIS integration tests and
  `HYDROLEDGER_RUN_LOAD=1` end-to-end load harness writing
  `docs/results/phase3/load_results.json` (separate from verifier microbench).

## Evidence (this session)

- `go vet ./...` clean (repo).
- `go test ./hydroledger/...` (verifier/identity/spatial) passed; previous
  phases not modified.
- `TestMigrationsExecute` passed against disposable `postgis/postgis:16-3.4`
  (Docker), confirming migrations execute successfully and are idempotent.
- After fixes, `TestPostTelemetry_DuplicateDelayedReorderedMalformed` and
  `TestPeerRetrievalSuppliesDBPeers` passed against the same disposable
  PostGIS container (strict/malformed handling, duplicate 200, conflict 409,
  delayed/reordered handling, H3 validation, GET endpoints, DB-backed peer
  supply).
- `service` package compiles and vets.

## Limitations / environment notes

- Windows Application Control blocked execution of rebuilt Go test binaries in
  this session after later edits, preventing a clean re-run of the modified
  escrow/race/corroboration/recovery/atomicity integration tests and the load
  run in-session. The escrow terminal-transition logic is implemented with
  conditional `state='PENDING'` updates, row locks, `SKIP LOCKED`, and
  serializable transactions; run `go test ./service` in a non-restricted
  environment/CI with Docker or `HYDROLEDGER_TEST_DATABASE_URL`.
- `TestLoadService` is implemented but was not executed here; see
  `docs/results/phase3/load_results.json` for required metrics and environment
  metadata to capture.
- Neighbor-cell corroboration from the in-memory verifier's escrow resolver is
  not replicated as a durable cross-cell service path; durable corroboration in
  this phase is radar-based/transactional. The verifier package remains the
  source of evaluation logic and is not modified.
- Retention deletion/archival is deployment-scheduled via the provided DB
  function, not performed by the HTTP service.
- The executable must register a PostgreSQL driver with `database/sql`
  (tests use `pgx` stdlib). Earlier packages are unmodified; dependency
  additions are limited to supporting the service/tests.
