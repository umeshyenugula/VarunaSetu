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

The dashboard attempts to use `VITE_API_BASE` when configured:

```bash
# example
VITE_API_BASE=http://localhost:8080 npm run dev
```

The dashboard does not fall back to fixtures or fabricate values. If the API is
unavailable, it shows an explicit error and empty operational state.

The backend currently exposes persisted readings, escrows, cell summaries,
analytics, and a ledger projection. Cryptographic ledger verification requires
an authoritative verification endpoint; the dashboard does not claim to verify
hashes in the browser.
