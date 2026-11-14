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

## Brand, simulator and docs

- **Logo-oriented design.** The wordmark (`src/brand/Wordmark.tsx`, traced to vector from the project logo; originals in `public/brand/`) leads the sidebar of the dashboard, the city simulator and the docs. The logo's headline bar recurs as the heavy rule over every page header and card. Tokens are at the top of `src/index.css`; the city simulator (`citysim/web/index.html`) carries the same tokens and wordmark.
- **Docs are a separate page**, not a dashboard screen: `/docs/` (entry `docs/index.html`, code in `src/docs/`). It has its own masthead and routes, makes no backend calls, and is linked from both the dashboard and the city simulator. Content lives in `src/docs/content.ts`; edit it there. The build emits two pages (`vite.config.ts`), and the docs text is only in the docs bundle.
- **URL parameters.** Routes are hash based and carry state, so views can be bookmarked and shared:

| URL | Effect |
|---|---|
| `/docs/#/:section/:page` | e.g. `/docs/#/documentation/verification` |
| `/docs/#/constitution/:article` | highlights an article, e.g. `iii` |
| `/docs/#/...?at=:heading` · `/docs/#/?q=:text` | scroll to a heading · search all pages |
| `#/story?scenario=` | `honest`, `inflated` or `conflict` |
| `#/readings?q&decision&sort&dir&page` | filtered, sorted, paged table |
| `#/readings/:id` · `#/map/:cell` | detail for one reading · one H3 cell |

Old `#/docs/...` dashboard links redirect to `/docs/`.
