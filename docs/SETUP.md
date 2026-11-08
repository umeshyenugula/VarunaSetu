# Setup (Local / Dev)

## Prerequisites

- Docker + Docker Compose (recommended for integrated run)
- Go 1.25+ / 1.26.x (module uses Go; Docker build uses `golang:1.26.8`)
- Node.js 20+ / npm (dashboard)
- Python 3.11+ (simulation/allocation; NetworkX required for `alloc` tests)

## Quick start (Docker Compose)

```bash
cp .env.example .env   # optional; edit secrets for non-local use
docker compose up --build
```

- PostGIS: `localhost:${POSTGRES_PORT:-5432}`
- API: `http://localhost:${API_PORT:-8080}` (`/healthz`, `/telemetry/readings`, `/escrows`, `/cells`, `/ledger/entries`, `/analytics/summary`)
- Dashboard (optional profile): `docker compose --profile ui up --build` then open `http://localhost:5173`

Migrations in `./migrations` are mounted into PostGIS init dir (first-time volume init). For existing volumes, apply migrations manually in order (`001_service.sql`, `002_ledger.sql`).

### Supabase PostgreSQL

Run both migrations in the Supabase SQL Editor, then set `DATABASE_URL` in `.env`
to the Supabase pooler or direct connection string with `sslmode=require`.
The Compose API honors `DATABASE_URL` when it is set; otherwise it uses the
local PostGIS container URL. The local `db` service still starts for Compose
compatibility, but the API connects to Supabase.

## Dashboard (standalone)

```bash
cd dashboard
npm install
npm run dev
# optional live API: VITE_API_BASE=http://localhost:8080 npm run dev
```

## Go (local)

```bash
go test ./hydroledger/... ./ledger/...
go build ./cmd/server
# service integration tests require disposable PostGIS:
# HYDROLEDGER_TEST_DATABASE_URL=postgres://... go test ./service -count=1
```

## Python

```bash
python -m unittest discover -s sim/tests -t . -v
pip install networkx
python -m unittest discover -t . -s alloc -p 'test*.py' -v
```

## Windows/WDAC note

If host Application Control blocks freshly built Go test binaries, run Go tests in Docker:

```bash
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1
```
