# HydroLedger Dashboard (Phase 5)

Minimal, professional white-background React dashboard for HydroLedger: H3 geospatial verification states, telemetry readings, escrow workflow, audit ledger viewer, analytics, and scripted demonstration scenarios.

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

Includes component tests, API fallback/integration behavior tests, map rendering tests (mocked Leaflet), escrow display tests, and ledger verification UI tests.

## API integration

The dashboard attempts to use `VITE_API_BASE` when configured:

```bash
# example
VITE_API_BASE=http://localhost:8080 npm run dev
```

Many list/aggregate/ledger endpoints required by the dashboard are not yet available in the Phase 3 service (see `HANDOFF.md` REQUESTED CHANGES). When endpoints are unavailable, the UI uses **clearly labeled SIMULATED mock fixtures** and never invents backend functionality.

## Simulated data

All demonstration fixtures are marked `simulated: true` and rendered with visible **SIMULATED** labels/banners. Do not treat fixture data as production telemetry.

## Demonstration scenarios

See [`../docs/demo.md`](../docs/demo.md) for the six scripted scenarios, including the known cross-cell collusion limitation.
