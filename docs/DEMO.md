# Live demo: the virtual city

This is the judge-facing demonstration of VarunaSetu. It builds a **virtual
Hyderabad** out of real H3 cells and real devices, and drives it against the
**actual service**. Nothing on screen is a mock of the backend: every reading is
an Ed25519-signed `POST /telemetry/readings` over HTTP, and every verdict,
escrow, reason code and ledger entry comes from the production verifier,
identity layer, PostgreSQL/PostGIS persistence and audit ledger.

> Numbers measured by this harness are in [`DEMO_RESULTS.md`](DEMO_RESULTS.md)
> (regenerate with the command at the end of this page). They come from
> synthetic weather, not from real rainfall, and prove nothing about real-world
> water savings.

## 1. Run it

You need PostgreSQL with PostGIS and Go 1.25+.

```bash
# Option A: Docker (database + demo, one command)
docker compose --profile demo up --build citysim db
# open http://localhost:8090/

# Option B: your own PostGIS
export DATABASE_URL="postgres://USER:PASS@localhost:5432/DB?sslmode=disable"
go run ./cmd/citysim serve            # open http://127.0.0.1:8090/

# Option C: no browser, check every scenario and write the results report
go run ./cmd/citysim verify -report docs/DEMO_RESULTS.md
```

`citysim` applies the migrations itself and never deletes data: each run starts
at a later virtual time than anything already stored, so earlier runs cannot act
as "peers" of later ones. Use a dedicated demo database anyway.

The page works without internet access (no tiles, no CDN): the map is drawn
from the H3 cell boundaries.

## 2. What is real and what is simulated

| Real (the system under test) | Simulated (the world around it) |
|---|---|
| HTTP API, JSON contract, status codes | Rainfall: slow Gaussian storms, uniform inside a cell |
| Ed25519 signatures, nonce replay protection, idempotency | Radar product, including injected faults (dry, lagged, biased) |
| Device enrolment lookup and revocation in PostgreSQL | The 210 buildings and devices (synthetic, but located in real H3 cells) |
| The Go verifier: radar bound, median/MAD peer test, escrow | Cheating behaviour (inflation, collusion, radar spoofing) |
| H3 spatial indexing and k-ring neighbours (`h3-go`) | The clock: virtual, so 100 minutes of storm replay in seconds |
| Escrow state machine, TTL, durable neighbour corroboration | |
| Capacity reservation and the SHA-256 audit ledger | |

Honest notes on the harness itself:

- The embedded service uses the virtual clock (`service.Config.Now`) and the
  harness calls `Server.Sweep` once per simulated minute, where production uses a
  wall-clock ticker. All thresholds (10-minute peer window, 15-minute escrow TTL,
  5-minute signature window) keep their real values.
- Device enrolment has no HTTP endpoint (by design, see `HANDOFF.md`), so the
  harness writes buildings and device keys straight into the database. It is the
  only place it touches the database apart from the optional "tamper" button.
- Rain is modelled per H3 cell, like a radar product at cell resolution, and
  storms ramp up slowly. A verifier that compares a reading to peers' *latest*
  readings cannot follow an abrupt onset; that is a documented limit with its
  own scenario, not something the calibration hides.
- The three comparison validators (Static, Radar-only, Peer-only) are small
  reference implementations in `citysim/behavior.go`, matching the baselines of
  the project's ablation study. They are **not** vendor products, and nobody
  should say they are.

## 3. How the H3 network works

H3 divides the city into hexagonal cells. At resolution 9 a cell is about
0.1 km², roughly a street block. Each device reports under the cell of its
building, and each cell has exactly six neighbours (the *k=1 ring*).

```
                reading from one roof
                        │  signed (Ed25519), timestamped, nonce
                        ▼
   1 identity   known, un-revoked device? fresh? not replayed?      → 401 / 409
   2 H3 cell    building → cell id (res 9)
   3 radar      R_obs = claimed litres / (roof m² × runoff)
                R_max = 1.15 × radar_mm + 2.0 mm   (physical bound)
   4 peers      latest reading of every other device in the SAME cell
                within 10 min; ≥ 4 peers → robust z-score (median/MAD)
                z > 3.5 and above the median → SPATIAL_PEER_OUTLIER
   5 conflict   radar dry (< 0.5 mm) but a coherent group of peers reports
                rain (median ≥ 2 mm) → PROVISIONAL_ESCROW, not a rejection
   6 resolution an escrow is released when
                  a) a later reading in the cell has live radar, or
                  b) ≥ 2 cells of the k=1 ring each have ≥ 3 devices with
                     median ≥ 2 mm  (the "H3 network" rule)
                otherwise it expires after 15 min → QUARANTINE
   7 ledger     every decision and resolution is a hash-chained audit entry
```

The two ideas that existing validators do not combine:

- **Locality as evidence.** A claim is judged against the devices on the same
  few blocks, not against a citywide threshold.
- **Neighbourhood as a tie-breaker.** When radar and peers disagree, VarunaSetu
  does not guess; it holds the reading and asks the surrounding H3 cells.

Click any cell on the map to see the server evaluate step 6 live for that cell.

## 4. The scenarios

All run from the dropdown in the header (the first group shows what the system
handles, the second shows documented limits on purpose). "Run all" executes the
whole catalogue and fills a comparison matrix.

| # | Scenario | What it shows | Settled by |
|---|---|---|---|
| 1 | A storm crosses the city | Control: honest data, working radar, nothing flagged beyond ramp noise | verifier |
| 2 | Two neighbours inflate their harvest | Radar over-reads, so a radar bound alone lets a cheat through; peers catch it | peer test |
| 3 | The radar goes blind over the city centre | Real rain with dry radar becomes escrow, not rejection | H3 neighbours |
| 4 | The radar blinks, then comes back | A tight storm that neighbours cannot vouch for | radar recovery |
| 5 | **Same evidence, opposite truth** | A real storm and a staged claim look identical at device level; H3 separates them | H3 neighbours vs expiry |
| 6 | A cell with too few neighbours | Fewer than 4 peers: falls back to the radar bound | radar bound |
| 7 | Attacks on the door, not the data | Replay, tamper, unknown/revoked key, stale/future time, malformed and impossible readings | identity, validation |
| 8 | A real cloudburst nobody else can see | *Limit:* one-cell storm missed by radar is not credited | expiry |
| 9 | Three neighbouring cells collude | *Limit:* a coordinated cluster corroborates itself | H3 neighbours (wrongly) |
| 10 | A cheat that also lies about the radar | *Limit:* radar value is device-supplied in this prototype | radar bound (fooled) |
| 11 | Rain starts faster than peers can follow | *Limit:* abrupt onset flags honest first reporters | peer test |
| 12 | A day in the virtual city | Everything at once for two hours, with attacks | all of it |

Scenario 5 is the one to dwell on. A real storm over the west of the city and a
staged claim in the far east produce the same device-level signal: dry radar and
a coherent group reporting heavy rain. Radar-only rejects both. Peer-only and the
static cap accept both. VarunaSetu holds both, then credits the real storm
because its k=1 neighbours also show rain, and lets the staged claim expire
because its neighbours are dry.

## 5. A ten-minute talk track

1. **(1 min) What you are looking at.** "This is a virtual Hyderabad: 37 real H3
   cells, 210 enrolled devices. The map is the city; the right-hand log is real
   API traffic. The weather and the clock are simulated; everything that
   decides is the production service." Point at the green/amber/red dots.
2. **(1.5 min) Scenario 1.** Run it. Show the *Live API* tab and click a reading:
   the five-step card shows signature → cell → peers → radar bound → verdict.
3. **(2 min) Scenario 2.** Toggle *Radar* layer: the radar over-reads in the
   core. Show the *Scoreboard*: static and radar-only miss the cheats, peers
   catch them. "A radar-only system would have paid them."
4. **(3 min) Scenarios 3 and 5.** Run 5 on the *Radar* layer: the red dashed
   cells are rain the radar cannot see. Watch ⏳ badges appear, then blue links
   flow from neighbouring cells into the western cells as escrows are released,
   while the eastern cell's escrows expire in red. Click the eastern cell and
   read the inspector: no raining neighbours.
5. **(1 min) Scenario 7.** Thirteen hostile requests, each answered as
   documented; last column shows a numbers-only system would accept them.
6. **(1 min) Limits.** Run 9 (colluding cluster) and say plainly that H3
   corroboration gives locality, not trust. Mention scenarios 8, 10 and 11.
7. **(0.5 min) Close.** *Run all*, then *Scoreboard → Tamper → Verify chain*: the
   audit ledger detects the edited row.

## 6. Likely questions, answered straight

**Is this just a simulation?** The environment is. The decision-maker is not:
the same binary code path that serves `POST /telemetry/readings` produced every
verdict, over a real socket, backed by a real database. You can `curl` it while
the demo runs (links at the bottom of the page).

**Why not just use radar?** Radar misses and lags. In scenario 3, radar-only
rejects 324 honest readings that VarunaSetu escrows and, in the end, mostly
credits. See `DEMO_RESULTS.md` for the measured rates.

**Why not just trust the neighbours?** Because they can be wrong or complicit.
Scenario 9 shows exactly that; it is a limit, not a hidden one.

**Does VarunaSetu always beat the baselines?** No, and the harness says so.
With no fraud in a scenario, "accept everything" scores 100% by construction.
In scenario 2 it ties peer-only; in scenario 6 it ties radar-only. It wins where
the evidence conflicts (scenarios 3, 5 and 12).

**How is scoring decided?** The simulator labels every reading honest or
manipulated before sending it, so each validator is scored against ground
truth. A reading VarunaSetu holds in escrow counts only after it is resolved.

## 7. Measured limits (all reproduced by the harness)

| Limit | Evidence | Where |
|---|---|---|
| Rising rain: first reporters of a new window compare against peers still describing the previous window | 3 of 1,955 honest readings in a gentle storm; 74 of 1,680 in an abrupt onset | scenarios 1, 11 |
| Dry-radar transition band: radar dry, a reading above 2 mm, peers' median still below 2 mm → physical-bound rejection of honest data | 42 of 2,550 honest readings in scenario 3 | scenarios 3, 4, 5, 12 |
| Hyper-local real storm missed by radar looks the same as a staged claim, so it fails safe and is not credited | 38 of 38 escrows expire | scenario 8 |
| Coordinated cluster of ≥3 adjacent cells corroborates itself; neighbour evidence ignores other devices' verdicts (as the verifier does) | 76 of 102 fraudulent readings credited | scenario 9 |
| `radar_mm` is part of the device-signed payload; a compromised enrolled device can set it. With <4 peers there is nothing to cross-check | 10 of 10 spoofed readings accepted | scenario 10 |
| A staged claim next to *real* rain is credited, because neighbours are raining | observed during calibration, not a dedicated scenario | n/a |
| Small absolute inflation in light rain stays inside the tolerance | 3 of 12 inflated readings in scenario 2, all under 1.5 mm of real rain | scenarios 2, 12 |

Suggested follow-ups, none done here: a server-side radar feed keyed by H3 cell;
normalising peer comparison by window; counting only non-quarantined devices as
neighbour evidence; device reputation across events.

## 8. Verify it yourself

While a run is in progress (or after):

```bash
curl -s localhost:8090/analytics/summary | jq
curl -s 'localhost:8090/escrows?limit=5' | jq     # readingId, resolution: RADAR | H3_NEIGHBORS | EXPIRED
curl -s 'localhost:8090/ledger/entries?limit=5' | jq
curl -s localhost:8090/sim/ledger/verify | jq     # re-computes the hash chain
```

Reproduce the numbers and the report:

```bash
go run ./cmd/citysim verify -report docs/DEMO_RESULTS.md -out /tmp/results.json
HYDROLEDGER_TEST_DATABASE_URL=... go test ./citysim ./service -count=1
```

## 9. What changed in the service to make this honest

Building the harness exposed defects in the service path; all are fixed with
tests (`service/neighbors_test.go`) and recorded in `docs/ERROR_LOG.md`:

- **H3 neighbour corroboration was not wired into the durable service.** The
  library implemented it, but the service evaluated each reading in a scratch
  verifier with no neighbours, so escrows could only be released by radar or
  expired. `Server.Sweep` now releases escrows via the k=1 ring
  (`CorroborateEscrowsByNeighbors`), mirroring the verifier's rule.
- **Invalid or over-capacity readings returned an opaque 500.** They now return
  `422` with the verifier's verdict body; unknown building is `404`; a claim
  above the building's reservable capacity is `409`.
- **A quarantined claim still reserved capacity**, so one rejected (or hostile)
  claim could lock an honest building out. Quarantined claims no longer reserve.
- The expiry worker now uses the injectable clock; `GET /escrows` exposes
  `readingId` and `resolution`; migration `003` indexes the audit `event_ref`.
