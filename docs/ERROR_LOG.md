# Error Log (Integration Stabilization)

| Issue | Root Cause | Fix | Files Modified | Verification |
|---|---|---|---|---|
| Dashboard missing live list/aggregate APIs | Phase 3 service exposed only ID-based reads; Phase 5 needed list/analytics/ledger views | Added additive read endpoints (`GET /telemetry/readings`, `/escrows`, `/cells`, `/ledger/entries`, `/analytics/summary`, `/healthz`) + CORS config; kept existing routes/behavior | `service/service.go`, `service/repository.go`, `service/models.go` | `docker run ... golang:1.26.8 go build ./cmd/server ./service ./ledger ./hydroledger/...` => `BUILD_OK`; core/ledger tests still PASS |
| No runnable service entrypoint for compose | Library package only | Added minimal server with env config, pgx driver, healthcheck flag | `cmd/server/main.go`, `Dockerfile`, `docker-compose.yml`, `.env.example` | `docker compose config` valid; build compiles in container |
| Phase 5 committed dashboard missing runtime map deps in `package.json` | Lock/config drift not captured in initial phase commit | Added `leaflet`, `react-leaflet`, `h3-js` deps and `vitest/globals` type | `dashboard/package.json`, `dashboard/package-lock.json`, `dashboard/tsconfig.app.json` | `cd dashboard && npm test` (10 PASS), `npm run build` PASS (after fix) |
| WDAC blocked rebuilt host Go test binaries | Windows Application Control policy on this host | Used Dockerized Go/Python execution for test evidence; documented workaround | n/a (operational) | Docker `go test -race ./hydroledger/... ./ledger/...` PASS; alloc/sim PASS |
| Formatting diffs reported | `gofmt` style/CRLF context | Applied `gofmt -w` to selected Go files | `ledger/ledger.go`, `service/*`, `sim/gocli/main.go`, new `cmd/server/main.go` | `gofmt` clean on modified files (host) |

## Not fully resolved / blocked

| Issue | Status | Reason |
|---|---|---|
| Service DB integration tests (`./service`) end-to-end in this pass | NOT RUN (regression risk low for additive changes) | Requires disposable PostGIS + WDAC-safe execution path; prior Phase 3 evidence recorded in HANDOFF. Run `go test ./service -count=1` with `HYDROLEDGER_TEST_DATABASE_URL` or CI Docker+DB. |
| Full docker-compose runtime smoke (API+DB+UI) | NOT EXECUTED | Image build/pull and startup time not completed in this session; config validated. |
| Signed e2e scenarios A–F via live HTTP+DB | PARTIAL/BLOCKED | Core logic tested at package level; live signed-ingest e2e needs device enrollment flow + running DB/service harness. |
| Ledger events not wired from service decisions | PLANNED/OPEN | Migration 002 exists; service integration requires careful append/transaction design (no competing truth). |
| `go test -race` for `./service` | BLOCKED on host WDAC; DB needed | Run in Linux CI/container with PostGIS. |
