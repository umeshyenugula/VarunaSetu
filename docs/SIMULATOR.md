# Urban Water Simulator (Judge Demo)

The simulator is available at `http://localhost:5173/#simulator` and is designed to demonstrate HydroLedger/VarunaSetu as a **virtual city** over a real H3-indexed network — not a static mockup.

## What it demonstrates

- **H3 network working:** devices/buildings are mapped to canonical H3 cells (resolution 9). The UI renders H3 polygons and live cell states from `/cells`, showing how the same spatial index drives peer grouping and neighbourhood context.
- **Real system behavior:** when the gated simulator API is enabled, the dashboard provisions demo buildings/devices, signs telemetry in the browser using Ed25519 (same canonical envelope format as the Go identity package), and submits to the real `POST /telemetry/readings` endpoint.
- **Every major case (catalog):** honest consensus accept, spatial peer outlier/tamper, physical bound exceeded, radar–peer conflict → provisional escrow, escrow corroborated by later wet radar, escrow expiry (gated demo sweep), capacity/integrity constraints, signed identity/idempotency/replay contract, audit ledger, and an explained neighbor-corroboration capability note (durable HTTP path currently applies radar corroboration + expiry).
- **Gap vs existing solutions:** each case explains what static/manual/isolated systems miss and how HydroLedger uses H3 + physics + robust peer consensus + escrow + audit evidence.

## Running the full live demo

1. Apply migrations (`001_service.sql`, `002_ledger.sql`).
2. Start API with simulator endpoints enabled:
   - `ENABLE_SIMULATOR_API=true`
   - `DATABASE_URL=...`
   - `CORS_ALLOW_ORIGIN=http://localhost:5173` (or dashboard origin)
3. Start dashboard with `VITE_API_BASE` pointing to the API.
4. Open `/#simulator`, click **Provision virtual city**, then run cases. The UI auto-refreshes live `/telemetry/readings`, `/escrows`, `/cells`, `/ledger/entries`, and `/analytics/summary`.

## Gated simulator API (demo only)

Enabled only when `ENABLE_SIMULATOR_API=true`:

- `POST /simulator/provision` — creates/resets `sim-bld-*` / `sim-dev-*` demo records and returns Ed25519 seeds/metadata for browser signing.
- `POST /simulator/reset` — deletes scoped `sim-dev-*` telemetry/escrows/decisions/ledger-linked rows and resets sim building reservations.
- `POST /simulator/expire-escrows` — runs durable expiry sweep with an advanced clock for judge demos (event-time escrow TTL defaults to 15 minutes).
- `GET /simulator/cases` — structured case catalog.

Security/accuracy notes:

- Demo keys are returned to the browser only for the gated local/demo flow. Do not enable in production.
- Real verification decisions are computed by the backend verifier/repository; the UI does not fabricate decisions.
- All demo data is labeled **SIMULATED**. Signed payload timestamps use wall-clock time to satisfy the real timestamp window.
- Neighbor-cell corroboration is a verifier/H3 capability; the durable service currently wires radar corroboration and expiry. The UI presents this status accurately.
