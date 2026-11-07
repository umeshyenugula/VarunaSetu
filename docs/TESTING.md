# Testing

## Verified commands (evidence paths)

| Scope | Command | Notes |
|---|---|---|
| Go verifier/identity/spatial/ledger | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test ./hydroledger/... ./ledger/... -count=1` | PASS |
| Go race (core) | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1` | PASS |
| Go build (service+cmd) | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go build ./cmd/server ./service ./ledger ./hydroledger/...` | PASS |
| Python simulation | `python -m unittest discover -s sim/tests -t . -v` | PASS (70 tests) |
| Python allocation | Docker `python:3.14-slim` + `networkx`, `python -m unittest discover -t . -s alloc -p 'test*.py' -v` | PASS (3 tests) |
| Dashboard | `cd dashboard && npm test -- --run` | PASS (5 tests) |
| Dashboard build | `cd dashboard && npm run build` | PASS |
| Compose config | `docker compose config --quiet` | PASS |

## Service integration tests (DB required)

```bash
# Docker or external PostGIS
export HYDROLEDGER_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/hydroledger?sslmode=disable'
go test ./service -count=1
```

Covers migrations/idempotency/malformed requests, escrow behavior, etc. (see `service/*_test.go`). Re-run after service API changes.

## End-to-end scenarios (status)

| Scenario | Status | Evidence / gap |
|---|---|---|
| A. Normal rainfall accepted | **PASS (service integration)** | Signed HTTP ingest, verification, persistence, and idempotency are covered by disposable PostGIS tests. |
| B. Radar-peer conflict escrow | **PASS (service integration)** | Durable peer retrieval, capacity reservation, and escrow creation are covered by service tests. |
| C. Escrow corroboration | **PASS (service integration)** | Corroboration and expiry race handling are covered with database-backed transitions. |
| D. Escrow expiry | **PASS (service integration)** | Expiry worker/recovery transitions and accounting are covered by service tests. |
| E. Invalid device signature | **PASS (service integration)** | Signed request validation and malformed/reordered/replay cases are covered by service tests. |
| F. Audit integrity | **PASS** | Library tamper/checkpoint tests pass and service events append transactionally to the persisted chain. |

## Regression rule

After any integration change, re-run: Go core+ledger tests (race where possible), `go build ./cmd/server ./service`, dashboard `npm test && npm run build`, and service DB tests when available.
