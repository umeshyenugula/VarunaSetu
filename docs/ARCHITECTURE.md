# VarunaSetu Architecture (Actual, Integrated View)

## Core principle

A water telemetry reading must not be trusted by fixed threshold alone. Validity is evaluated using physical constraints (`R_obs`, radar-informed bound), robust spatial peer statistics (median/MAD, Z), H3 spatial context, escrow for conflicts, and (where enabled) cryptographic device identity and auditability.

## Implemented components

| Layer | Package/Path | Responsibility |
|---|---|---|
| Verification | `hydroledger/verifier` | Physics/radar bound, peer consensus, escrow decisions, reason codes (thresholds preserved) |
| Spatial | `hydroledger/verifier/spatial` | H3 indexing/neighbor helpers (library) |
| Identity | `hydroledger/verifier/identity` | Ed25519 keys/signatures, stateless verification primitives; in-memory registry in dev server |
| Durable service | `service/` | Signed telemetry ingest middleware (identity + durable idempotency), DB-backed peer retrieval into scratch verifier, transactional persistence, escrow lifecycle operations, read/aggregate APIs for dashboard |
| Storage | `migrations/001_service.sql`, `002_ledger.sql` | PostGIS-capable schema for telemetry/decisions/escrows/ledger tables; additive ledger tables |
| Audit ledger | `ledger/` | SHA-256 hash chain, Ed25519 signed checkpoints, verification/witness interface (Go library + tests); service DB integration pending |
| Allocation | `alloc/` | Python+NetworkX min-cost-flow prototype, ACCEPT-only, integer litres, explicit priority/fairness rule |
| Simulation/research | `sim/` | Deterministic scenarios, replay/ablation/consistency tooling; not runtime service path |
| Dashboard | `dashboard/` | React/Vite UI: map, telemetry, escrow, ledger viewer, analytics, demo scenarios; uses live API when available, labeled SIMULATED fixtures otherwise |
| Ops | `Dockerfile`, `docker-compose.yml`, `.env.example`, `cmd/server` | Containerized PostGIS + API + optional UI dev server |

## Integration flow (intended/implemented where present)

1. Device signs telemetry envelope (identity package contract).
2. `POST /telemetry/readings` verifies signature/enrollment/revocation/timestamp and durable idempotency.
3. Service loads recent DB-backed peers for H3/time window and evaluates with a scratch verifier (preserves verifier logic).
4. Capacity reservation, reading, decision, escrow (if provisional), and ledger-reserve-style entries persist transactionally.
5. Escrows can be corroborated/expired via durable transitions (radar-based corroboration path + expiry worker).
6. Dashboard reads list/aggregate endpoints (`/telemetry/readings`, `/escrows`, `/cells`, `/ledger/entries`, `/analytics/summary`) or falls back to labeled fixtures.

## Trust boundaries / limitations

- No Sybil-proof claim. Compromised enrolled device keys can sign bad data until revoked.
- Hash chain detects tampering given trusted checkpoint; privileged full rewrite remains possible without external witnessing.
- `cmd/server` uses in-memory identity registry for dev/demo; production needs durable enrollment/key management.
- Neighbor-cell durable corroboration and full ledger-event wiring are not fully integrated into service.
- Dashboard simulated data must remain clearly labeled when live APIs are unavailable.

## Key files

- `docs/INTEGRATION_AUDIT.md`, `docs/ERROR_LOG.md`, `docs/SETUP.md`, `docs/TESTING.md`
- `CONTRACTS.md`, `HANDOFF.md`, `README.md`
- `service/service.go`, `service/repository.go`, `migrations/*.sql`
- `ledger/ledger.go`, `alloc/model.py`, `dashboard/src/api/client.ts`
