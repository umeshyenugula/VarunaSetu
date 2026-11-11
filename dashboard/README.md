# HydroLedger Dashboard

Minimal, professional white-background React dashboard for HydroLedger: H3 geospatial verification states, persisted telemetry readings, escrow workflow, audit ledger viewer, and backend analytics.

## Requirements

- Node.js 20+ (tested with Node 24)
- npm

## Setup

```bash
cd dashboard
npm install
```

## Run (development)

```bash
npm run dev
```

Open the URL shown by Vite (default `http://localhost:5173`).

## Build

```bash
npm run build
npm run preview   # optional: preview production build
```

## Test

```bash
npm test
```

Includes component tests, API success/error-state integration tests, map rendering tests (mocked Leaflet), escrow display tests, and ledger viewer tests.

## API integration

Requests go to `/api/*`, which the dev server proxies to the backend on
`http://localhost:8080` (override with `VITE_PROXY_TARGET`). Set `VITE_API_BASE`
(e.g. `http://localhost:8080`) to bypass the proxy; the API then needs CORS enabled.
Copy `.env.example` to `.env` to change either. Port 8090 is the standalone citysim
demo and is never used by the dashboard.

The dashboard does not fall back to fixtures. If the API is unavailable it shows an
explicit error, and the header chip shows the target and `unreachable`.
