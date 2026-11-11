# VarunaSetu (HydroLedger prototype)

VarunaSetu is a physics-informed, spatially aware urban water intelligence prototype. It evaluates rooftop rainwater harvesting telemetry using physical/radar constraints, spatial peer consensus (H3 context), escrow for conflicts, cryptographic identity primitives, durable backend persistence, audit-ledger primitives, allocation modeling, and a React dashboard.

> **Status:** integrated prototype/research codebase. Not production-ready. No Sybil-proof or real-world water-savings/flood-reduction claims.

## Repository map

| Area | Path | Status |
|---|---|---|
| Go verifier (core) | `hydroledger/verifier/` | Implemented, tested |
| Spatial / identity | `hydroledger/verifier/spatial/`, `hydroledger/verifier/identity/` | Implemented, tested |
| Durable service + API | `service/`, `cmd/server/`, `migrations/` | Implemented core ingest/escrow/read APIs; DB integration tests require PostGIS |
| Audit ledger | `ledger/`, `migrations/002_ledger.sql` | Implemented/tested library and persisted service event chain |
| Allocation (research) | `alloc/` | Implemented/tested (NetworkX) |
| Simulation/replay (research) | `sim/` | Implemented/tested Python harness |
| Dashboard | `dashboard/` | Implemented/tested; live backend data only, with explicit API-unavailable errors |
| Virtual city + live demo | `citysim/`, `cmd/citysim/` | Real signed traffic into the real service, 12 scenarios with self-checks, judge UI |
| Ops | `Dockerfile`, `docker-compose.yml`, `.env.example` | Config validated; runtime smoke recommended |

## Quick start

```bash
cp .env.example .env
docker compose up --build
# optional dashboard: docker compose --profile ui up --build
```

### Live demo for judges

```bash
export DATABASE_URL="postgres://USER:PASS@localhost:5432/DB?sslmode=disable"   # PostGIS
go run ./cmd/citysim serve        # then open http://127.0.0.1:8090/
go run ./cmd/citysim verify       # headless: run every scenario and check it
```

A virtual Hyderabad of 37 real H3 cells and 210 enrolled devices drives the
**actual service** with signed telemetry; only weather and clock are simulated.
It shows how H3 neighbourhoods separate a real storm from a staged claim, which
cases simple validators get wrong, and which limits remain. See
[`docs/DEMO.md`](docs/DEMO.md) (guide and talk track) and
[`docs/DEMO_RESULTS.md`](docs/DEMO_RESULTS.md) (measured results).

See [`docs/SETUP.md`](docs/SETUP.md), [`docs/TESTING.md`](docs/TESTING.md), [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/INTEGRATION_AUDIT.md`](docs/INTEGRATION_AUDIT.md), [`docs/ERROR_LOG.md`](docs/ERROR_LOG.md), [`HANDOFF.md`](HANDOFF.md), and [`CONTRACTS.md`](CONTRACTS.md).

## Core API (service)

- `POST /telemetry/readings` — signed telemetry ingest (identity + durable idempotency)
- `GET /telemetry/readings`, `GET /telemetry/readings/{id}`
- `GET /escrows`, `GET /escrows/{id}`
- `GET /cells`, `GET /cells/{h3_index}/status`
- `GET /ledger/entries` (requires migration 002 data/tables)
- `GET /analytics/summary`, `GET /healthz`

`cmd/server` resolves enrolled device keys and revocation state from the database.

## Verification

```bash
# Go core/ledger (Docker, WDAC-safe)
docker run --rm -v "$PWD:/src" -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1
# Dashboard
cd dashboard && npm test && npm run build
# Simulation / allocation
python -m unittest discover -s sim/tests -t . -v
docker run --rm -v "$PWD:/src" -w /src python:3.14-slim sh -lc "pip install -q networkx && python -m unittest discover -t . -s alloc -p 'test*.py' -v"
```

Service DB tests: `HYDROLEDGER_TEST_DATABASE_URL=... go test ./service -count=1` (disposable PostGIS).
