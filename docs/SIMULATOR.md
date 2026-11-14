# Story simulator

Open the dashboard (`cd dashboard && npm run dev`, http://localhost:5173). A left
sidebar holds every screen: **Story** (simulation) and **Overview, Map, Readings,
Escrows, Ledger** (monitoring). All screens share one layout: page header, bordered
cards, the same tables, badges and empty/error states. Monitoring screens reload
from the backend each time they are opened.

A story runs six steps in order, and every step calls the real backend on **port 8080**:

1. Reach the backend (`GET /healthz`)
2. Set up the city (`POST /simulator/provision`: enrols simulated devices and keys)
3. Rain falls (radar value, local)
4. Roofs report (each device signs and sends `POST /telemetry/readings`)
5. Backend decides (ACCEPT / PROVISIONAL_ESCROW / QUARANTINE comes from the service, never the browser)
6. It is recorded (`GET /analytics/summary`, `GET /ledger/entries`)

The **Requests sent** table shows the exact URL of every call.

## Ports

| Port | What | Used by the dashboard |
|---|---|---|
| 8080 | `cmd/server` backend API | yes |
| 5173 | dashboard (Vite) | - |
| 8090 | standalone `citysim` demo (own embedded API) | no |

By default the browser calls same-origin `/api/*`, and Vite forwards it to
`VITE_PROXY_TARGET` (default `http://localhost:8080`). Set `VITE_API_BASE` only to
call a backend directly. The backend needs `SIMULATOR_ENABLED=true` (set in
`docker-compose.yml`) for the `/simulator/*` endpoints.

## City simulator (port 8090)

`go run ./cmd/citysim serve` (or `docker compose --profile demo up --build citysim`) serves the
virtual-Hyderabad simulator at http://localhost:8090. It uses the same shell as the dashboard
(sidebar, page header, bordered cards, white and square-cornered) with screens City, Live API,
Scoreboard, Attacks and All scenarios, and the same responsive behaviour: on narrow screens the
sidebar becomes a top bar and nothing scrolls sideways. The dashboard has an **Open simulator** button (sidebar and Story screen)
that opens it in a new tab; the simulator's **Dashboard** button returns to the dashboard.
Set `VITE_SIM_URL` to change where the dashboard button points, or open the simulator with
`/?dashboard=http://host:5173` to change where its back button points. These are links only:
the dashboard sends its API requests to 8080, never to 8090.
