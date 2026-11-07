# HANDOFF

Newest entry first. Each entry should leave the next person able to continue
without re-reading the chat that produced it.

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
