# VarunaSetu Integration Audit

Date: 2026-11-07  
Scope: repository-wide baseline audit (Phases 0–5 present in tree). Evidence gathered via Dockerized Go/Python runs (host WDAC blocks some freshly built host test binaries) and local Node dashboard checks.

## Baseline evidence

| Area | Command (evidence path) | Result |
|---|---|---|
| Go race (core) | `docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1` | PASS (`verifier`, `identity`, `spatial`, `ledger`) |
| Go tests (core+ledger) | Docker `go test ./hydroledger/... ./ledger/... -count=1` | PASS |
| Python sim | `python -m unittest discover -s sim/tests -t . -v` (host) | PASS (70 tests) |
| Python alloc | Docker `python:3.14-slim` + `networkx`, `unittest discover -t . -s alloc` | PASS (3 tests) |
| Dashboard | `cd dashboard && npm test && npm run build` (host) | PASS (10 tests, production build) |
| Go fmt/vet (selected) | Docker `gofmt -l` / `go vet ./hydroledger/... ./ledger/... ./service/...` | See notes: `gofmt` reports formatting diffs in several files; `go vet` clean for selected packages. |
| Service integration tests | `go test ./service/...` requires disposable PostgreSQL/PostGIS or `HYDROLEDGER_TEST_DATABASE_URL`; not executed in this baseline pass inside plain container (no DB). | **NOT RUN in baseline** (known harness exists). |
| Docker Compose / env | Repo scan | **MISSING** `docker-compose*`, `Dockerfile*`, `.env.example`. |

## Component audit

| Component | Current Status | Problems | Severity | Required Action |
|---|---|---|---:|---|
| Go Verifier | Implemented (Phase 0+) | No new functional defect found in baseline; formatting nits in tree | LOW | Keep thresholds/rules stable; run full service+e2e integration |
| H3 Spatial Engine | Implemented (Phase 1) | Not wired as a full service read/list API surface | MEDIUM | Add/verify service list endpoints or mock boundary (dashboard uses fixtures) |
| Device Identity | Implemented (Phase 1) | Telemetry ingest path is signed; dashboard lacks admin/device workflows | LOW | Preserve signature verification; document API gaps |
| Python Simulation | Implemented (Phase 0B/2 research) | Host/CI must have Python deps as configured; uses stdlib per config | LOW | Keep deterministic seeds/repro checks |
| Rainfall Replay | Implemented under `sim/` | Not wired to runtime service; research workflow | MEDIUM | Document; optional integration harness |
| HTTP Service | Implemented (Phase 3) | Only ID/cell-status read endpoints; missing list/aggregate/ledger/dashboard endpoints; CORS/auth/browser concerns | HIGH | Add thin read endpoints matching dashboard contracts; preserve signed ingest/transactions; add CORS config |
| PostgreSQL/PostGIS | Migrations 001/002 present | No compose/env bootstrap in repo; migration 002 adds ledger tables not wired to service | MEDIUM | Add `docker-compose` + `.env.example`; keep migrations ordered |
| Durable Escrow | Implemented in service | Neighbor-cell durable corroboration not fully replicated (documented) | MEDIUM | Keep transactional invariants; document limitation |
| Audit Ledger | Go package + migration 002 | Not connected to HTTP service/events | HIGH | Define integration interface; avoid competing truth; do not claim admin-rewrite prevention |
| Water Allocation | Implemented (Phase 4, NetworkX) | Not exposed via service/dashboard live API | MEDIUM | Keep ACCEPT-only/integer constraints; add API/UI wiring only if stable |
| React Dashboard | Implemented (Phase 5) | Uses live backend read endpoints; reports API failures explicitly; chunk size warning remains | LOW | Keep API contracts and error states covered by tests |
| Docker | Not present | No Dockerfiles/Compose | HIGH | Add minimal compose for PostGIS + service (+ optional dashboard) with health checks |
| Deployment/Env | Not present | No `.env.example` | HIGH | Add `.env.example`; never store secrets |

## Formatting notes (`gofmt -l` sample)

`gofmt` reported diffs in: `ledger/ledger.go`, `migrations` not applicable, `service/api_integration_test.go`, `service/dbtest_test.go`, `service/repository.go`, `service/service.go`, `sim/gocli/main.go`. These are CRLF/LF or formatting style issues; fix with `gofmt -w` after review (does not change behavior).

## Security / reliability quick notes

- Signed telemetry verification exists in service middleware (stateless checks + durable idempotency). Preserve; do not bypass.
- Service has request size/strict JSON/timeout patterns in existing code; review CORS and auth for browser dashboard before production exposure.
- Ledger hash chain detects tampering given trusted checkpoint; does not prevent privileged full rewrite (documented).
- Allocation prototype is not field-validated.

## Requested next actions (prioritized)

1. **CRITICAL/HIGH integration**: add missing dashboard-facing read endpoints (list readings/escrows/cells summary, analytics, ledger entries) as thin handlers/repo queries; keep existing endpoints stable.
2. **Docker/env**: add `docker-compose.yml` and `.env.example` with PostGIS + service build/run and correct health checks.
3. **Frontend wiring**: keep the API client connected to real endpoints and do not add fabricated fallback data.
4. **Service tests**: run with disposable PostGIS (Docker network or host DB URL) and fix regressions.
5. **Docs**: update `README`, `docs/SETUP.md`, `docs/TESTING.md`, `docs/ERROR_LOG.md`, `docs/ARCHITECTURE.md` to match actual state.

## Classification

CRITICAL: none confirmed by baseline test failures in core packages.  
HIGH: missing dashboard-facing APIs, missing Docker/env, ledger not service-integrated, dashboard relies on fixtures for unavailable APIs.  
MEDIUM: formatting, migration 002 not service-wired, allocation/replay not runtime-wired, durable neighbor corroboration limitation.  
LOW: documentation alignment, chunk size warning.
