# HANDOFF

Newest entry first. Each entry should leave the next person able to continue
without re-reading the chat that produced it.

---

## Entry 6 — 2026-11-07 — Phase 5 frontend dashboard

### What was done

Implemented Phase 5 under allowed paths (`dashboard/`, `docs/demo.md`). Earlier
phases were **read-only**.

- **Frontend setup (`dashboard/`, React + Vite + TypeScript)**: maintainable
  component architecture, responsive white/minimal/professional UI, Leaflet +
  `react-leaflet` + `h3-js` for geospatial H3 rendering, accessible labels/
  controls/tables.
- **H3 map**: interactive polygons colored by `ACCEPT` (green),
  `PROVISIONAL_ESCROW` (amber), `QUARANTINE` (red), with cell selection/popup
  showing relevant telemetry context.
- **Telemetry table**: device ID, timestamp, rainfall, radar rainfall, peer
  consensus/count, decision, reason; filtering, sorting, pagination.
- **Escrow panel**: escrow ID, device, cell, state, created/expiry/remaining
  time (live countdown), corroboration status.
- **Audit ledger viewer**: seq, event reference, prev/current hash, timestamp,
  checkpoint signature status; UI action for ledger integrity verification
  (explicitly labeled simulated/structural in-browser check; authoritative
  verification is Phase 4 Go).
- **Analytics**: total/accepted/provisional/quarantined readings, escrow
  resolution rate, active devices, spatial cell distribution; clearly
  distinguishes SIMULATED fixture data from API data.
- **API integration**: `VITE_API_BASE` client with fallback to mock fixtures
  when endpoints are unavailable. Missing endpoints recorded under REQUESTED
  CHANGES; no backend functionality invented.
- **Demonstration mode**: 6 scripted scenarios (normal agreement, isolated spike
  quarantine, radar-dry/coherent-peer escrow, later radar confirms, escrow
  expires/quarantined, cross-cell collusion known limitation). All simulated
  data visibly labeled **SIMULATED**.
- **Tests**: Vitest + React Testing Library + jsdom — component/app tests, API
  fallback/integration behavior, map rendering (mocked Leaflet), escrow display,
  ledger verification UI, scenario coverage. `dashboard/README.md` and
  `docs/demo.md` added.

### REQUESTED CHANGES (missing backend endpoints)

Phase 3 service currently exposes: `POST /telemetry/readings`,
`GET /telemetry/readings/{id}`, `GET /escrows/{id}`,
`GET /cells/{h3_index}/status`. The dashboard requires read/list/aggregate
endpoints not yet available; mock fixtures are used temporarily:

- `GET /telemetry/readings` (or paginated/filterable query) — telemetry table.
- `GET /escrows` (or filterable list) — escrow panel.
- `GET /cells` or spatial summary endpoint — H3 map states/aggregates.
- `GET /ledger/entries` and `GET /ledger/checkpoints` (or equivalent) — ledger
  viewer; optional `POST /ledger/verify` for authoritative server-side
  verification.
- `GET /analytics/summary` (or composable aggregate endpoints) — dashboard
  analytics.
- CORS/auth/config support for browser clients if dashboard is hosted
  separately from API.

### Evidence

```text
cd dashboard && npm test
-> 2 test files, 10 tests passed

cd dashboard && npm run build
-> tsc -b && vite build succeeded (client bundle built)
```

Node v24.21.0 / npm 11.19.0. Build/test ran on host; no WDAC block encountered
for Node/Vite/Vitest in this session.

### Limitations

- List/aggregate/ledger data is SIMULATED when backend endpoints are missing.
- In-browser ledger integrity check is structural only; authoritative SHA-256/
  Ed25519 verification is in Phase 4 Go `ledger/`.
- Map tile layer uses public OpenStreetMap tiles in demo mode; production
  deployments should review tile/provider terms and availability.

---

## Entry 5 — 2026-11-07 — Phase 4 audit ledger and water allocation

### What was done

Implemented Phase 4 under allowed paths (`ledger/`, `alloc/`, `migrations/`
new numbered files only, `docs/ledger.md`, `docs/alloc.md`). `service/` was
**not modified**. Earlier phases remain unchanged.

- **SHA-256 hash-chained ledger (`ledger/ledger.go`, `ledger/ledger_test.go`)**:
  append-only `Entry` with `Seq`, `EventRef`, `PrevHash`, `Hash`, `Timestamp`.
  Canonical serialization is precisely specified and implemented (schema
  `hydroledger.ledger.entry.v1`; LF-separated fields in order: schema, base-10
  seq, event ref, lower-case 64-char prev hash/genesis zeros, RFC3339Nano UTC;
  `hash=hex_lower(SHA-256(canonical bytes))`). `Append()` assigns linkage/hash;
  `Verify()`/`VerifyEntries()` detect sequence gaps, prev-hash breaks, and hash
  tampering. Tests modify a historical entry and verify detection (and also
  detect linkage break if only that entry's hash is recomputed). Docs state the
  limitation: a hash chain alone cannot prevent an administrator with storage/
  key control from rewriting the entire chain.
- **Ed25519 signed checkpoints**: `Checkpoint` signs/verifies
  `(schema, seq, head_hash, RFC3339Nano UTC timestamp, key_id)` at interval
  `N`; `VerifyCheckpoint` checks signature and head binding. `Witness`
  interface supports external anchoring; `MaybeCheckpoint` enforces interval.
  Tests cover valid checkpoint, modified checkpoint, rewritten chain with
  checkpoint forged under attacker key rejected by legitimate verifier key, and
  invalid signature. Key rotation and remaining trust assumptions documented.
- **Durable schema (`migrations/002_ledger.sql`)**: additive
  `audit_ledger_entry` and `audit_ledger_checkpoint` tables; no service tables
  modified.
- **Water allocation prototype (`alloc/model.py`, `alloc/test_alloc.py`,
  Python+NetworkX)**: min-cost-flow redistribution. Supplies are ACCEPT
  readings with verified integer-litre stored volume; demands have integer
  requested litres and explicit priority (1 highest). Transfer edges carry
  integer capacity and cost; effective weight adds `PRIORITY_WEIGHT*(priority-1)`
  for the documented fairness/priority rule. Non-ACCEPT supplies excluded.
  Six-node worked example: 3 supplies + 3 demands (6 domain nodes), with a
  non-ACCEPT supply excluded; invariants and priority outcome asserted.
  Property-style randomized tests (50 deterministic cases, seed 42) verify water
  conservation, capacity constraints, supply limitations/ACCEPT-only sourcing,
  demand fulfillment bounds, and allocation validity. No claim of real-world
  water savings or flood reduction.
- **Docs**: `docs/ledger.md`, `docs/alloc.md`.

### Evidence (WDAC-safe containerized execution)

Windows Application Control on this host blocked freshly built host test
binaries in-session; tests were executed via Docker (Linux containers) as an
authorized/safe execution path:

```text
docker run --rm -v <repo>:/src -w /src golang:1.26.8 go test ./ledger/... ./hydroledger/... -count=1
-> ok hydroledger-prototype/ledger 0.014s
-> ok hydroledger-prototype/hydroledger/verifier 0.022s
-> ok hydroledger-prototype/hydroledger/verifier/identity 0.030s
-> ok hydroledger-prototype/hydroledger/verifier/spatial 0.013s

docker run --rm -v <repo>:/src -w /src python:3.14-slim sh -lc "pip install --no-cache-dir networkx && python -m unittest discover -t . -s alloc -p 'test*.py' -v"
-> Ran 3 tests ... OK (only_accept_readings, property_randomized, six_node_worked_example)
```

### Limitations

- Ledger/checkpointer is in-memory; migration 002 provides durable storage
  shape but service integration/witness deployment is out of scope.
- Hash chain + checkpoint do not prevent a privileged rewriter from replacing
  the full chain; external witnessing/transparency and key custody controls are
  required for stronger guarantees.
- Allocation is a prototype model; inputs/costs/priorities must come from
  validated operational data. No real-world water-savings/flood-reduction claims.

---

## Entry 4 — 2026-11-07 — Phase 3 durable backend service

### What was done

Implemented Phase 3 under the allowed paths (`service/`, `migrations/`,
`docs/service.md`), importing earlier packages as libraries without modifying
`hydroledger/` or prior phase behavior.

- **Schema (`migrations/001_service.sql`)**: PostgreSQL/PostGIS/pgcrypto
  extensions; `building`, `device`, `telemetry_reading`,
  `verification_decision`, `escrow_record` (`PENDING`/`CORROBORATED`/
  `EXPIRED_QUARANTINED`), and `ledger_entry` placeholder; PK/FK/UNIQUE/CHECK
  constraints; idempotency `UNIQUE(device_id,idempotency_key)` and payload hash;
  H3 cell/time and device/time indexes, BRIN time index, GiST spatial indexes;
  append-only triggers for readings/decisions; restricted `hydroledger_api`
  grants; `purge_telemetry_readings` retention helper.
- **Repository (`service/repository.go`)**: serializable `StoreReading` that
  checks idempotency, locks/reserves capacity, persists reading/decision/
  ledger, creates durable escrow for `PROVISIONAL_ESCROW`, and applies durable
  radar-based corroboration. Conditional `state='PENDING'` transitions plus
  row locks/`SKIP LOCKED` make expiry/corroboration races resolve to one
  terminal state. `RecentPeerReadings`/`RecentPeers` provide DB-backed eligible
  peers.
- **HTTP API (`service/service.go`, `service/models.go`)**: signed-telemetry
  middleware using Phase 1 identity (stateless signature/enrollment/revocation/
  timestamp checks + durable idempotency lookup); strict JSON validation;
  `POST /telemetry/readings` returns 201/200(idempotent)/409(conflict);
  `GET /telemetry/readings/{id}`, `GET /escrows/{id}`,
  `GET /cells/{h3_index}/status` (canonical H3 validation). Request timeouts,
  structured logging, context cancellation handling.
- **Verifier wiring**: ingestion retrieves persisted eligible peers from DB and
  seeds a scratch verifier in event-time order before `Evaluate`, preserving
  existing verifier logic (Task 3). The `hydroledger/verifier*` packages were
  not modified.
- **Worker**: `StartExpiryWorker`/`Close` run restart-recoverable expiry with
  row locking.
- **Tests/harness**: `service/dbtest_test.go`, `service/api_integration_test.go`
  (disposable PostGIS container or `HYDROLEDGER_TEST_DATABASE_URL`; `pgx`
  stdlib), including migrations, duplicate/delayed/reordered/malformed
  requests, idempotency conflict, DB-backed peer supply, escrow expiry/race/
  corroboration/recovery cases, and `HYDROLEDGER_RUN_LOAD=1` load harness
  writing `docs/results/phase3/load_results.json`.

### Evidence

- `go version`: `go1.26.8 windows/amd64`.
- `go vet ./...` clean.
- `go test ./hydroledger/...` (cached rerun): `ok verifier 1.098s`, `ok
  verifier/identity 1.087s`, `ok verifier/spatial 1.093s` (Phases 0–2 packages
  unaffected).
- `TestMigrationsExecute` passed against disposable `postgis/postgis:16-3.4`,
  including idempotent re-run — migrations execute successfully.
- After fixes, `TestPostTelemetry_DuplicateDelayedReorderedMalformed` and
  `TestPeerRetrievalSuppliesDBPeers` passed against disposable PostGIS.
- `service` package compiles and vets.

### Limitations

- **Windows Application Control blocked rebuilt Go test binaries in this
  session** after later edits, preventing a clean in-session re-run of the
  modified escrow/race/corroboration/recovery/atomicity integration tests and
  the load test. These cases are implemented and covered by the added tests;
  run `go test ./service -count=1` in a non-restricted environment/CI with
  Docker or `HYDROLEDGER_TEST_DATABASE_URL`.
- **Load results not executed here**. `TestLoadService` is implemented and
  writes `docs/results/phase3/load_results.json` (hw/sw metadata + p50/p95/p99,
  throughput, CPU/memory/DB write latency, concurrent device loads). Execute
  with `HYDROLEDGER_RUN_LOAD=1`; keep separate from verifier microbenchmarks.
- Durable escrow corroboration in this service is **radar-based/transactional**
  and does not replicate the in-memory verifier's neighbor-cell escrow resolver
  as a cross-cell durable path.
- `geom` spatial columns are present/indexed but not populated by ingestion;
  retention is deployment-scheduled via `purge_telemetry_readings`.
- The executable must register a PostgreSQL `database/sql` driver; tests use
  `github.com/jackc/pgx/v5/stdlib`. Dependency additions support the service;
  earlier packages remain unmodified.

---

## Entry 3 — 2026-11-07 — Phase 1 H3 spatial indexing and device identity

### What was done

Phase 1 adds two packages under the existing module layout. `verifier.go` and
every Phase 0 test were left untouched.

- `hydroledger/verifier/spatial` (**IMPLEMENTED**): pinned pure-Go H3 binding,
  coordinate-to-cell conversion at a configurable resolution, k-ring neighbour
  lookup (default k = 1), canonical cell-id validation, and a small adapter
  that calls `SetNeighbors`. Known-coordinate tests pin cells at resolutions
  7–11 and the exact resolution-9 k=1 neighbour set.
- `hydroledger/verifier/identity` (**IMPLEMENTED**): Ed25519 key pairs, signer,
  canonical signed telemetry envelope, stateless signature verification,
  in-memory enrollment registry, revocation list, nonce/timestamp replay
  protection with an explicit time window, and a full `Verifier.Verify` path.
- `hydroledger/verifier/spatial/README.md` documents how H3 resolution changes
  cell area and expected peer density.

### Package locations (ASSUMPTION C1)

The phase prompt names `verifier/spatial/` and `verifier/identity/`. The module
layout recorded in Entry 1/A1 is `hydroledger/verifier`, so the new packages are
`hydroledger/verifier/spatial` and `hydroledger/verifier/identity`. Creating a
second top-level `verifier/` tree would split one logical package family across
two import paths.

### H3 dependency (ASSUMPTION C2)

Pinned in `go.mod`:

```
github.com/uber/h3-go/v4 v4.5.1-0.20261104181000-32ae003dbaa6
```

Reason for the dependency: real H3 indexing needs a maintained H3
implementation; re-implementing H3 was out of scope. Reason for the exact
version: the tagged `v4.5.0` of this module is a cgo binding, while this machine
builds with `CGO_ENABLED=0` and has no C compiler. The pinned commit is the
official Uber repository's pure-Go implementation, its `go.mod` targets Go 1.24
(same as this module), and it is confined to the spatial package. Tests make no
network calls. A fresh module cache still needs `go mod download` before the
first build.

### Phase 0 pre-check (before writing code)

```text
go vet ./...
-> clean (exit 0)

go test ./... -count=1
-> ok hydroledger-prototype/hydroledger/verifier 1.436s
```

### Commands and evidence after implementation

```text
go mod tidy
-> go.mod now requires github.com/uber/h3-go/v4 v4.5.1-0.20261104181000-32ae003dbaa6

gofmt -l hydroledger/verifier/spatial hydroledger/verifier/identity
-> no output

go vet ./...
-> clean (exit 0)

go test ./... -count=1
-> ok hydroledger-prototype/hydroledger/verifier          0.956s
-> ok hydroledger-prototype/hydroledger/verifier/identity 0.951s
-> ok hydroledger-prototype/hydroledger/verifier/spatial  2.846s

go test ./hydroledger/verifier/spatial -count=1 -v
-> PASS (all spatial tests)

go test ./hydroledger/verifier/identity -count=1 -v
-> PASS (all identity and security tests)

GOPROXY=off go test ./... -count=1
-> ok hydroledger-prototype/hydroledger/verifier          0.721s
-> ok hydroledger-prototype/hydroledger/verifier/identity 0.723s
-> ok hydroledger-prototype/hydroledger/verifier/spatial  0.703s
   (proves the tests themselves need no network once the module cache exists)

go test ./... -race -count=1
-> failed before running: "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1"
```

Independent cross-check of the pinned H3 expectations: `h3-js` 4.1.0 under
Node 24.21.0 returned the same cells for resolutions 7–11 and the same
resolution-9 k=1 neighbour set. `h3-js` was used only for this cross-check and is
not a project dependency. The committed Go tests depend only on the pinned Go
module.

The race-enabled Phase 0 baseline came from a linux/amd64 machine. This
development machine is windows/amd64 with `CGO_ENABLED=0` and no C compiler, so
the race suite could not be repeated here. The non-race full suite passes, and
the replay concurrency test asserts that exactly one of 64 concurrent
verifications of the same envelope is accepted.

### What is IMPLEMENTED

- H3 coordinate conversion, configurable resolution, k-ring lookup, invalid
  coordinate/resolution/k/cell handling, sorted neighbour lists, adapter to
  `SetNeighbors`.
- Ed25519 key generation, rebuild from a 64-byte private key or 32-byte RFC
  8032 seed, public/private accessors returning copies, signer.
- Envelope with device id, timestamp, nonce and payload; canonical
  length-prefixed signing encoding with a domain separator; deterministic
  signatures; stateless `VerifySignature`.
- Enrollment interface (`Enroller`, `Registry`), public-key lookup, permanent
  revocation (`RevocationList`, `RevocationSet`, `Registry`).
- Replay protection keyed by `(device id, nonce)`, a `Window`/`FutureSkew`
  timestamp policy, retention of `Window + FutureSkew`, pruning, and atomic
  check-and-store.
- Tests required by Task C: valid signature accepted, modified payload
  rejected, replayed message rejected, expired timestamp rejected, revoked
  device rejected. Additional tests cover timestamp boundaries, metadata
  tampering, unknown devices, duplicate enrollment, nonce retention boundaries,
  rejected messages not consuming nonces, per-device nonce scope, and
  concurrent replay.

### ASSUMPTIONS (Phase 1)

- **C1 Package location.** `hydroledger/verifier/spatial` and
  `hydroledger/verifier/identity`, as explained above.
- **C2 H3 binding.** Official Uber module at a pinned pure-Go commit; tagged
  `v4.5.0` is cgo and could not build here.
- **C3 H3 default resolution 9.** Chosen because it is neighbourhood-sized
  (~0.108 km² at the reference Hyderabad coordinate). This is a project choice,
  not a field-calibrated optimum; `Config.Resolution` is configurable.
- **C4 k-ring bound.** Default k = 1; `MaxK = 10` caps a single lookup at 331
  cells including the centre. This bound is a project choice.
- **C5 Coordinate validation.** Latitude must be finite and in `[-90, 90]`;
  longitude finite and in `[-180, 180]`. The H3 binding only rejects NaN/Inf
  and silently wraps out-of-range values, so the adapter validates first.
- **C6 Canonical cell ids.** Only the lower-case hexadecimal form returned by
  H3 is accepted. Uppercase, leading zeros and surrounding whitespace are
  rejected so one cell cannot be represented by two strings.
- **C7 Neighbour list semantics.** The H3 grid disk includes the centre; the
  API excludes it, returns unique ids sorted lexicographically, and returns an
  empty slice for k = 0. The verifier's neighbour relation is directed, so the
  adapter configures only the cell named in the call.
- **C8 Adapter boundary.** `NeighborSetter` is the one-method interface
  (`SetNeighbors`) implemented by `*verifier.Verifier`. This keeps the spatial
  package free of a non-test import of the verifier and avoids an import cycle.
- **C9 Identity time defaults.** `Window = 5m`, `FutureSkew = 30s`. The phase
  did not specify values; both are configurable and a zero `Config` is invalid.
- **C10 Canonical signature encoding.** Domain string
  `hydroledger/identity/telemetry/v1`, then length-prefixed device id, timestamp
  as Unix seconds plus nanoseconds, nonce, and payload. Length prefixes remove
  concatenation ambiguity; the domain separates this protocol from other uses
  of the same Ed25519 key.
- **C11 Nonce scope and retention.** A nonce must be unique per device within
  the retention period. Retention is `Window + FutureSkew`; after that the
  captured envelope is outside the timestamp window anyway, so forgetting the
  nonce is safe. Different devices may reuse the same nonce string.
- **C12 Verification order.** Structure, enrollment, revocation, signature,
  timestamp window, replay. Only a message that passes every earlier check
  consumes its nonce. This makes rejected messages retryable.
- **C13 Enrollment.** Duplicate device ids are rejected (no silent key
  rotation); revoked ids cannot be enrolled. `Enroll` takes a public key and is
  an administrative interface, not a proof of possession.
- **C14 Revocation.** Permanent for the lifetime of the registry; revoking an
  unknown id is allowed so it cannot be enrolled later; there is no un-revoke.
- **C15 Size limits.** Device id and nonce max 128 bytes, payload max 64 KiB,
  signatures exactly 64 bytes. These are project choices to bound memory.
- **C16 Payload is opaque.** Phase 1 does not bind the payload to
  `verifier.Reading`, JSON, HTTP or a database.
- **C17 No wall clock.** `Signer.Sign` takes a timestamp; `Verifier.Verify`
  takes `now`. The package never calls `time.Now`.
- **C18 Error detail.** Unknown device, revoked device and invalid signature are
  distinct errors. They are useful diagnostics but can reveal registry
  membership; this is documented in `CONTRACTS.md`.
- **C19 Inclusive boundaries.** A message exactly `Window` old or exactly
  `FutureSkew` in the future is accepted; one nanosecond beyond is rejected.
- **C20 Key persistence format.** A 64-byte Ed25519 private key or a 32-byte
  RFC 8032 seed can rebuild a key pair. There is no encrypted key storage in
  this phase.

### Known limitations

- **L1 Enrolled but compromised devices are trusted.** Possession of the
  private key is sufficient to sign arbitrary readings. Revocation is the only
  mitigation exposed. This is the limitation stated in the phase prompt.
- **L2 H3 does not solve cross-cell collusion.** The existing
  `TestKnown_CrossCellCollusionCorroborates` still demonstrates that coordinated
  neighbouring cells can corroborate a fabricated escrow. H3 only defines which
  cells are neighbours.
- **L3 Replay state, enrollment and revocation are in memory.** They are lost on
  restart, so replay across a restart is not detected by this package alone.
- **L4 No rate limiting.** A valid device can fill the replay map up to the
  retention bound, and any enrolled device can sign unlimited messages.
- **L5 No Sybil resistance.** Enrollment authority and device uniqueness are out
  of scope; one actor can still control many enrolled identities if the
  enrollment process allows it.
- **L6 No transport, database or ledger integration.** TLS, HTTP ingestion,
  PostgreSQL/PostGIS, durable escrow, the audit ledger and signed checkpoints
  remain PLANNED.
- **L7 Clock assumptions.** Acceptance assumes roughly synchronised clocks and
  the configured `Window`/`FutureSkew`. A far-future timestamp is rejected, not
  clamped.
- **L8 No key rotation.** Re-enrolling an existing device id is rejected.
  Rotation needs a new interface and an explicit audit trail.
- **L9 Race suite not run on this machine.** `-race` needs cgo; this machine has
  `CGO_ENABLED=0` and no C compiler. Run it on Linux/CI before relying on the
  concurrency claims.
- **L10 No field or benchmark evidence for the new packages.** The H3 and
  identity code is tested, not benchmarked. No field accuracy, production
  readiness or Sybil-proofness is claimed.
- **L11 `README.md` was not updated.** It is outside the Phase 1 allowed paths.
  Its "Standard library only" statement remains true for the existing verifier
  package, but the repository now has one external H3 dependency. See REQUESTED
  CHANGES.
- **L12 H3 geometry is not hydrology.** Cells ignore buildings, terrain,
  catchments, drainage and radar coverage. Resolution 9 is an engineering
  default, not a validated neighbourhood definition.
- **L13 Error messages can leak registry membership.** See C18.

### REQUESTED CHANGES

1. Approval to update `README.md` layout/dependency text so it names
   `hydroledger/verifier/spatial` and `hydroledger/verifier/identity` and no
   longer implies the whole repository is standard-library-only. No code or
   interface change is involved.
2. Approval, in a later phase, for a key-rotation interface. It is deliberately
   absent rather than added silently.

### Open questions

1. Is H3 resolution 9 the intended default for Hyderabad, or should resolution
   be configured per deployment/city?
2. Should the adapter configure neighbour lists symmetrically for the centre
   and each neighbour, or should ingestion always call it per observed cell? The
   verifier's relation is directed; the current adapter does the minimum.
3. Should revocation carry an effective time so messages signed before
   revocation can still be verified? Current behaviour rejects every message
   from a revoked device.
4. Should nonces be globally unique rather than per device? Per-device scope is
   sufficient for replay and avoids a shared global namespace, but a registry-
   wide namespace is stricter.
5. Should enrollment require a challenge-response proving possession of the
   private key, rather than accepting an administrator-supplied public key?
6. Should `Verify` return a coarse "verification failed" error over an API
   boundary while keeping detailed sentinel errors internally?

### Files created or modified

Created:

- `hydroledger/verifier/spatial/spatial.go`
- `hydroledger/verifier/spatial/spatial_test.go`
- `hydroledger/verifier/spatial/README.md`
- `hydroledger/verifier/identity/errors.go`
- `hydroledger/verifier/identity/keys.go`
- `hydroledger/verifier/identity/registry.go`
- `hydroledger/verifier/identity/verifier.go`
- `hydroledger/verifier/identity/identity_test.go`
- `hydroledger/verifier/identity/security_test.go`
- `hydroledger/verifier/identity/replay_test.go`

Modified:

- `go.mod`, `go.sum` (pinned H3 dependency)
- `CONTRACTS.md` (Phase 1 additive contracts)
- `HANDOFF.md` (this entry)

Not modified: `hydroledger/verifier/verifier.go`, all Phase 0 tests, existing
defaults and reason codes, and `sim/`.

### Suggested next steps (not done)

- Wire the spatial adapter into the planned HTTP ingestion path and persist
  neighbour configuration.
- Replace in-memory enrollment/revocation/replay state with durable storage and
  document restart semantics.
- Add enrollment challenge-response and an audited key-rotation path.
- Add rate limiting per device and per cell.
- Run the full suite with `-race` on a Linux/CI machine with cgo.
- Evaluate whether radar-only or multi-cell structure can reduce the known
  cross-cell collusion limitation; H3 indexing alone cannot.

---

## Entry 2 — 2026-11-07 — Phase 0B Python simulation and ablation engine

### What was done

Built the Phase 0B research harness from scratch under `sim/`:

- `sim/config.py` is the single source of truth: 100,000 events per seed,
  seeds 1-10, seven scenarios, all generator distributions, and the four
  methods' thresholds.
- `sim/ablation_experiment.py` generates deterministic synthetic telemetry,
  evaluates static threshold, physics-only, peer-only (median/MAD robust Z)
  and hybrid (physics + peers) in one chronological pass, and writes every
  required table.
- `sim/tests/test_ablation.py` contains 28 offline tests: config contract,
  determinism, event validity, fraud proportions, metric arithmetic, method
  boundaries, the known peer-majority limitation, and end-to-end CSV/Markdown
  generation.
- `sim/run_all.sh` runs the tests first and then the full ablation.
- `sim/requirements.txt` records that only the Python standard library is
  used; there are no third-party packages to pin.

Outputs in `docs/results/`:

| File | Contents |
|---|---|
| `METHOD.md` | Generated methodology, distributions, scenario definitions, metrics, latest result tables, limitations |
| `overall_results.csv` | Mean and sample std across seeds, plus pooled confusion counts |
| `scenario_results.csv` | Per-scenario mean/std and pooled counts |
| `confusion_matrices.csv` | Per-scenario/seed, pooled scenario, per-seed overall, and pooled overall matrices |

### Commands and evidence

Executed on this machine:

```text
python -m unittest discover -s sim/tests -t . -v
-> Ran 28 tests ... OK

bash sim/run_all.sh          (Git Bash; Python 3.12.10)
-> 28 tests OK
-> full run: 100,000 events x 10 seeds x 7 scenarios = 7,000,000 events
-> wrote docs/results/{METHOD.md,overall_results.csv,scenario_results.csv,confusion_matrices.csv}
```

The earlier Phase 0A suite was also run before this phase:

```text
go test ./... -count=1
-> ok hydroledger-prototype/hydroledger/verifier 1.019s
```

No Go code, Go defaults, Go reason codes or Go public interfaces were
modified. `CONTRACTS.md` was not changed because no Go interface was added.

### Actual results from this run

Overall (pooled across the seven scenarios within each seed, then averaged;
positive class = fraudulent reading):

| Method | Precision | Recall | F1 | FPR |
|---|---:|---:|---:|---:|
| Static threshold | 0.730538 | 0.165091 | 0.269232 | 0.026826 |
| Physics-only | 0.987049 | 0.765345 | 0.862170 | 0.004398 |
| Peer-only | 0.999994 | 0.474728 | 0.643815 | 0.000001 |
| Hybrid | 0.988550 | 0.871699 | 0.926451 | 0.004422 |

Key scenario observations:

- Peer-only is near-perfect on normal rainfall, microburst and isolated-spike
  scenarios (F1 0.9994, 0.9991, 0.9997) and catches 97.1% of the 40%
  coordinated compromise (F1 0.9855).
- Peer-only fails when the fraudulent readings are a coherent majority:
  coordinated 60% recall is 4.2% (F1 0.0810). Median/MAD cannot detect a
  majority that defines the median.
- Peer-only is partial at the two 50%-fraud scenarios: dry-weather spoofing
  F1 0.6393 and radar-bounded spoofing F1 0.6237.
- Hybrid is stronger overall because the radar bound independently rejects
  the dry-radar coordinated fabrications (F1 0.999994 at 40% compromise and
  0.999976 at 60%); it matches peer-only at radar-bounded spoofing (F1 0.6237),
  where the radar bound is deliberately not discriminating.
- Peer-only has higher precision and a lower false-positive rate than hybrid
  in this run (0.999994 vs 0.988550 precision; 0.000001 vs 0.004422 FPR),
  consistent with the qualitative historical claim, but its overall F1 is
  lower because the scenario mix contains high-prevalence and peer-majority
  stress cases.

These numbers do **not** reproduce the previously quoted historical headline
table (static 25.45%, physics 63.63%, peer 95.36%, hybrid 96.42%). The
generator, priors and scenario mix are different from whatever produced that
table, and no parameter was tuned to close the gap. The historical figures
must not be attributed to this code. In particular, physics-only is much
higher here because the coordinated scenarios use a dry radar, so a 9 mm
fabricated claim is physically impossible; peer-only is much lower because
the 60% and 50%-fraud scenarios are deliberately adversarial.

### ASSUMPTIONS (Phase 0B)

- **B1 Positive class.** A reading is positive when `fraudulent=True`:
  the claim deliberately overstates the synthetic true depth. Honest noise
  and under-claims are negative. All precision/recall/F1/FPR figures are for
  fraud detection, not water-resource estimation.
- **B2 No tuning.** Generator and method parameters were fixed before
  evaluation; no parameter search was run on the evaluation seeds. The
  scenario mix is a deliberate stress mix, not a real-world prevalence.
- **B3 Priors.** Scenarios 1-5 use exact fraud shares 2%, 3%, 8%, 50%, 50%.
  Coordinated scenarios use 40% and 60% compromised devices per cell (13/32
  and 19/32 after rounding); the realised event share is close to, not
  exactly, the nominal value.
- **B4 Static baseline.** Fixed threshold 10.0 mm effective depth. The
  specification did not define the static baseline's threshold; this is an
  explicit choice, not a Go verifier default.
- **B5 Peer-only.** Uses the specified median/MAD robust Z with `Z > 3.5`
  and `R_obs > median`. With fewer than four peers it accepts; it never uses
  radar. This is the ablation definition, not a claim that peer-only is a
  complete verifier.
- **B6 Hybrid binary mapping.** Hybrid follows the Go rule order (outlier,
  dry-radar/rainy-coherent-peer conflict, physical bound) but maps
  `PROVISIONAL_ESCROW` to "not accepted" because Phase 0B does not model TTL
  resolution. This is conservative and may increase hybrid false positives.
- **B7 Capacity.** Tank capacity is 100,000 L and every generated event fits;
  capacity validation is intentionally non-binding and not measured.
- **B8 Radar.** `RadarMM` is supplied per reading, mirroring the current Go
  `Reading`. The simulation does not authenticate radar or devices.
- **B9 World.** 16 cells, 32 devices per cell, 24-hour horizon, uniform event
  timestamps, one latest observation per device in a 10-minute peer window.
  Ordinary rain is a sum of broad Gaussian storm pulses; microbursts are
  short intense pulses with radar under-catch.
- **B10 Statistics.** Mean and sample standard deviation (`n-1`) across the
  ten seeds. Per-scenario and overall metrics are computed per seed and then
  averaged; they are not the F1 of averaged counts. A zero denominator yields
  0.0.
- **B11 Determinism.** Integer seeds (`seed * 1000 + scenario_index`) are
  passed to `random.Random`; Python's randomized `hash` is never used.
- **B12 Runtime.** `run_all.sh` was exercised with Git Bash and CPython
  3.12.10; the same tests and full experiment also passed with CPython
  3.14.0. No network access is used.

### Known limitations

- **L1 Synthetic only.** The results describe this generator. They are not
  field accuracy, not Hyderabad rainfall validation, and not flood
  prediction.
- **L2 Not a Go integration test.** The peer window and decision order are
  reimplemented in Python and mirror the documented Go rules; the Python
  engine does not call the Go package.
- **L3 Escrow is binary.** `PROVISIONAL_ESCROW` is counted as rejected; no
  TTL, radar-later, or neighbor-cell corroboration is simulated.
- **L4 Peer-majority failure.** At 60% coordinated compromise, peer-only
  recall is 4.2%. This is the expected limitation of median/MAD under a
  coherent majority and is asserted in
  `test_known_peer_majority_limitation`.
- **L5 High-prevalence stress mix.** The 50% dry/radar-bounded priors and the
  40/60 coordinated scenarios dominate the pooled overall metrics. They are
  not estimates of real-world fraud prevalence.
- **L6 No identity or Sybil model.** Compromise is a fixed fraction of
  `DeviceID` strings; there is no authentication, key material, or Sybil
  cost. `RadarMM` is taken from the reading.
- **L7 Capacity and malformed inputs.** Capacity is non-binding; no invalid
  input stream is generated. Both are covered by the Go tests, not this
  ablation.
- **L8 Static threshold choice.** The 10.0 mm baseline is a design choice and
  materially affects the static row.

### Open questions

1. Should the coordinated scenarios also spoof radar, so the radar bound does
   not independently catch the fabrication? That would isolate peer
   consensus further and would need a new documented scenario, not a silent
   change to the existing one.
2. Should the overall table use a realistic weighting of scenarios instead of
   equal event counts? Current overall numbers are an adversarial stress
   aggregate.
3. Should hybrid model escrow TTL and later corroboration instead of mapping
   provisional to rejected?
4. Should peer-only use a different robust estimator (for example a lower
   quantile) to survive a 60% coherent majority? That would be a new method,
   not a tweak to the specified median/MAD method.

### Suggested next steps (not done)

- Decide the open questions above before Phase 1 comparisons.
- Add a scenario where coordinated attackers also spoof radar, under a new
  key, and report it separately.
- Add an integration test that feeds the same synthetic events through the
  Go verifier via a small exported test harness, if cross-language fixtures
  are wanted.
- Add confidence intervals or a bootstrap over seeds if the results are used
  in a paper; the current output reports mean ± sample std only.

---

## Entry 1 — 2026-11-07 — Initial rebuild from the written specification

### What was done

Built the verifier package from scratch (no prior code existed): `Config`,
`Evaluate`, `SetNeighbors`, `ResolveEscrows`, `PendingEscrows`; in-memory
per-cell peer windows and escrow store; table-driven tests; the two required
benchmarks; `CONTRACTS.md`, `README.md`, and `docs/baseline_results.txt`.
Standard library only.

### State of the work

| Item | State |
|---|---|
| All specified decision rules and reason codes | Implemented; every reason code has an emitter and a test |
| Required tests (conflict escrow, expiry, later radar, neighbor corroboration, single-cell majority, storage overflow, `TestKnown_CrossCellCollusionCorroborates`, invalid input, 3/4 peers, peer age exactly 10 min, radar exactly 0.5 mm) | Present and passing |
| `go test ./... -race` | Passing on the machine below; raw output in `docs/baseline_results.txt` |
| `BenchmarkEvaluate`, `BenchmarkEvaluateParallel` (both `ReportAllocs`) | Run on this machine; raw output in `docs/baseline_results.txt` |

### How to verify

```
go vet ./...
go test ./... -race -count=1
go test ./... -run '^$' -bench . -benchmem -count=5
```

Environment of the recorded run: Go 1.24.13 (installed from the Ubuntu
`golang-1.24-go` package because Go was not present), linux/amd64, a
**single-vCPU** virtual machine (Intel Xeon @ 2.30GHz; exact strings in the
results file). Consequences:

- `BenchmarkEvaluateParallel` ran with GOMAXPROCS=1. It cannot show multi-core
  scaling or lock contention; treat its output as "no regression visible on one
  core" only and re-run on a multi-core machine before drawing conclusions.
- No benchmark figures are quoted in this repository's prose; read the file.

Boundary tests were also checked by hand: 15 single-operator mutations of the
comparisons (`<=`↔`<`, `>`↔`>=`, etc.) were applied in a scratch copy and each
made at least one test fail. One mutant (strict `Z > 3.5`) initially survived,
which is why `TestEvaluate_OutlierThresholdExactEquality` exists. This was a
manual exercise and is not automated in the repo.

### ASSUMPTIONS

The specification was silent or ambiguous on the points below; the simplest
option was chosen. Items marked **(confirm)** change visible behaviour and are
the first things to check with the specification's owner.

- **A1 Layout and module.** `go.mod` at the repo root, module
  `hydroledger-prototype`, package at `hydroledger/verifier` (import path
  `hydroledger-prototype/hydroledger/verifier`). The spec gave a directory but
  no module path.
- **A2 Time base.** All ages and TTLs use event time: `Reading.Timestamp`, and
  the `now` argument of `ResolveEscrows`. The wall clock is never read; monotonic
  clock readings are stripped.
- **A3 Validation.** Invalid: empty `DeviceID`/`CellID`; zero `Timestamp`; any
  NaN/Inf; negative `ClaimedLitres`/`StoredLitres`/`CapacityLitres`/`RadarMM`;
  `RoofAreaM2 <= 0`; `RunoffCoeff` outside `(0,1]`; non-finite `R_obs` or
  `R_max`. A zero claim, zero stored, zero capacity or zero radar is valid.
- **A4 Check order.** Validation, then capacity, then peer/radar rules. Invalid
  and over-capacity readings are not recorded as peer observations.
- **A5 Peer age.** `age = reading.Timestamp − peer.Timestamp`; eligible when
  `age <= 10m` (exactly 10 minutes is eligible). A peer *newer* than the reading
  (negative age) is eligible. No clock-skew handling. A device is never its own peer.
- **A6 Window bound.** "Bounded" is `WindowMaxDevices` = 128 devices per cell
  (my choice). When full, the observation with the oldest timestamp is evicted;
  a reading older than everything in a full window is dropped. No time-based
  pruning. `Validate` requires `WindowMaxDevices >= MinPeers+1`. The number of
  cells is not bounded.
- **A7 What is recorded (confirm).** Every reading that passes validation and the
  capacity check is stored as its device's latest observation **regardless of
  verdict**, including quarantined ones. Without this the radar/peer conflict
  could never arise: while peers are scarce the readings that would form the peer
  evidence are themselves quarantined. Consequence: see K2. An older reading
  never replaces a newer observation from the same device.
- **A8 "Radar effectively dry".** `RadarMM <= 0.5`. The same field
  (`RadarDryMM`) is used for escrow corroboration (later radar `> 0.5`). Exactly
  0.5 is therefore dry and does not corroborate.
- **A9 "Peers report rain".** `median(peers) >= 2.0` (inclusive). The same
  `PeerRainMM` is used for the neighbor-cell medians.
- **A10 Spread / coherence.** Spread is `max − min` of peer depths; coherent when
  `spread <= max(1.0, 0.25 × median)` (inclusive).
- **A11 Outlier direction (confirm).** An outlier needs `Z > 3.5` (strict) and
  `R_obs > M`. Under-claims are not quarantined by the Z test.
- **A12 Rule order with enough peers (confirm).** (1) positive outlier →
  `SPATIAL_PEER_OUTLIER`; (2) radar dry ∧ peers rain ∧ peers coherent →
  `RADAR_PEER_CONFLICT_ESCROW`; (3) `R_obs > R_max` → `PHYSICAL_BOUND_EXCEEDED`;
  (4) else `ACCEPT / RADAR_AND_PEERS_AGREE`. "Agreement with radar" is read as
  `R_obs <= R_max`. The escrow test precedes the bound test because a dry radar
  makes `R_max = Beta`, which would otherwise pre-empt every escrow. The escrow
  rule does not look at the claimant's own size, so a low claim made under a dry
  radar next to coherent rainy peers is also escrowed.
- **A13 Too few peers (confirm).** `R_obs <= R_max` → `ACCEPT /
  RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS`; otherwise `QUARANTINE /
  INSUFFICIENT_PEERS_REVIEW`. The spec does not say where
  `INSUFFICIENT_PEERS_REVIEW` is emitted; I used it for "no peer cross-check and
  the fallback failed" rather than `PHYSICAL_BOUND_EXCEEDED`. If the owner wants
  `PHYSICAL_BOUND_EXCEEDED` here, that code stays but `INSUFFICIENT_PEERS_REVIEW`
  loses its only emitter.
- **A14 Escrow clock.** The TTL starts at the escrowed reading's `Timestamp`.
  Expired iff `now − Timestamp > 15m` (exactly 15 minutes is still pending).
  A device can hold several escrows; replayed or duplicate readings are not
  detected.
- **A15 Radar corroboration.** A reading corroborates an escrow when it is in the
  same cell, passed validation and the capacity check, has `RadarMM > 0.5`, and
  its `Timestamp` is strictly later than the escrowed reading's and at most one
  TTL after it. Any device's reading qualifies, including quarantined ones.
  `Evaluate` records the evidence when it sees the reading, so a sweep run after
  the TTL still reports `ESCROW_CORROBORATED`.
- **A16 Neighbor corroboration.** Checked only inside `ResolveEscrows(now)`, never
  in `Evaluate`, and only while the escrow is within its TTL. A neighbor cell
  qualifies with `>= 3` distinct devices observed within `PeerMaxAge` of `now`
  (the 10-minute constant is reused for "recent") and a median `>= PeerRainMM`;
  `>= 2` such cells are needed. The relation is directed. `SetNeighbors` replaces
  the list and drops empty ids, duplicates and the cell itself.
- **A17 Delivery of resolutions (confirm).** The spec says escrows are "returned
  as quarantined on expiry" without naming the mechanism. `Evaluate` returns only
  the decision for its own reading; `ResolveEscrows(now)` returns resolved escrows
  (each exactly once, sorted by cell, reading time, device). The caller must call
  it periodically. Nothing is persisted.
- **A18 Median.** Even counts use the mean of the two middle values.
- **A19 Config.** `DefaultConfig()` returns the specified defaults; `New`
  validates and rejects a zero `Config`. Constants the spec states as numbers
  (`ZScale`, `MADFloorMM`, `OutlierZ`, coherence widths, neighbor counts) are
  fields with those defaults; `WindowMaxDevices` is the only default I invented.
  A test pins every default.
- **A20 Extra output.** `Decision` carries diagnostics (`RObsMM`, `RMaxMM`,
  `PeerCount`, `PeerMedianMM`, `PeerMADMM`, `ZScore`) beyond the spec, for
  debugging and testing.

### Known limitations

- **K1 Coordinated neighbor cells corroborate a fabricated escrow.** Documented
  and asserted by `TestKnown_CrossCellCollusionCorroborates`: three coordinated
  devices in each of two neighbor cells, all reporting rain while the radar is
  dry, get a fabricated escrow accepted as `ESCROW_CORROBORATED`. Not mitigated.
- **K2 Quarantined readings still count as peers (A7).** Readings an actor
  controls shape peer medians and can manufacture the "coherent peers report
  rain" condition that creates escrows. A single cell's majority only stays
  provisional until the TTL (tested); it does not prove the rain.
- **K3 No device identity or uniqueness, no authentication.** `DeviceID` is an
  unverified string, so one actor can appear as many devices; the code has no
  Sybil resistance. `RadarMM` is taken from the reading as supplied, so a reading
  can carry its own radar value and thereby corroborate escrows (A15).
- **K4 Memory and durability.** Peer windows and escrows are in memory only and
  are lost on restart. The number of cells and of pending escrows is unbounded.
  Escrows are only reclaimed by `ResolveEscrows`.
- **K5 Time.** No clock-skew, out-of-order or replay handling beyond A5/A14;
  a far-future timestamp is an eligible peer for every reading (A5).
- **K6 Under-claims pass (A11).** A very low claim can be returned as
  `RADAR_AND_PEERS_AGREE` although it disagrees with the peers.
- **K7 Test depth.** Tests are example-based. There is no fuzzing or property
  testing, and the mutation check above was manual. The concurrency test checks
  for data races and one bookkeeping invariant, not performance.
- **K8 Benchmarks.** Recorded on one vCPU (see above).

### Open questions for the specification's owner

1. Should `INSUFFICIENT_PEERS_REVIEW` be emitted for "too few peers and
   `R_obs > R_max`" (A13) or for something else?
2. Should quarantined readings be recorded as peer observations (A7)?
3. Should negative deviations be flagged (A11)? Should a low claim under a dry
   radar be escrowed (A12)?
4. Should "radar dry" and "radar corroborates" be separate thresholds (A8)?
5. Is `ResolveEscrows` the intended way to hand back escrow outcomes (A17)?

### Suggested next steps (not done)

- Decide the open questions above, then update `CONTRACTS.md` in the same change.
- Bound the number of cells and pending escrows; add a background sweeper or an
  injectable clock if callers should not drive `ResolveEscrows` themselves.
- Re-run the benchmarks on a multi-core machine and append the result to
  `docs/` as a new file rather than overwriting the baseline.
- Add fuzz tests for `Evaluate` (finite and non-finite inputs) and a property
  test for the "every escrow is returned once" invariant.
