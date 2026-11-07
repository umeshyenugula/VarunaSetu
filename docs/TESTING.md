# Testing

## Verified commands (evidence paths)

| Scope | Command | Notes |
|---|---|---|
| Go verifier/identity/spatial/ledger | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test ./hydroledger/... ./ledger/... -count=1` | PASS |
| Go race (core) | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1` | PASS |
| Go build (service+cmd) | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go build ./cmd/server ./service ./ledger ./hydroledger/...` | PASS |
| Python simulation | `python -m unittest discover -s sim/tests -t . -v` | PASS (70 tests) |
| Python allocation | Docker `python:3.14-slim` + `networkx`, `python -m unittest discover -t . -s alloc -p 'test*.py' -v` | PASS (3 tests) |
| Dashboard | `cd dashboard && npm test` | PASS (10 tests) |
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
| A. Normal rainfall accepted | **PARTIAL** | Verifier/service logic and dashboard demo cover path; live signed HTTP+DB e2e not executed in this pass. |
| B. Radar-peer conflict escrow | **PARTIAL** | Decision/escrow logic implemented; durable service e2e requires running DB+enrolled device. |
| C. Escrow corroboration | **PARTIAL** | Durable corroboration/expiry transitions implemented; live e2e blocked by harness setup in this pass. |
| D. Escrow expiry | **PARTIAL** | Expiry worker + repo transitions implemented/tested previously; full timed HTTP e2e not executed. |
| E. Invalid device signature | **PARTIAL** | Identity/middleware reject invalid signatures; live API negative test not executed in this pass. |
| F. Audit integrity | **PASS (component)** | `ledger` tamper/hash-chain/checkpoint tests pass. Service event wiring pending. |

## Regression rule

After any integration change, re-run: Go core+ledger tests (race where possible), `go build ./cmd/server ./service`, dashboard `npm test && npm run build`, and service DB tests when available.
