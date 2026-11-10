# VarunaSetu virtual-city results

Generated 2026-11-10 04:28 UTC by `citysim verify`. The virtual city has **37 H3 cells** (resolution 9) around Hyderabad (17.4435, 78.3772) and **210 enrolled devices**. Every reading below is an Ed25519-signed `POST /telemetry/readings` to the real service (verifier, identity, PostgreSQL/PostGIS persistence, hash-chained ledger). Only the weather and the clock are simulated.

Validators compared: **Static** (R_obs ≤ 10 mm), **Radar-only** (R_obs ≤ 1.15·radar + 2 mm), **Peer-only** (robust z-score vs. same-cell peers), and **VarunaSetu** (the live service, including escrow resolution by radar and H3 neighbours). The first three are reference baselines implemented in `citysim/behavior.go`, not vendor products. *Balanced* = mean of (fraud caught) and (honest readings credited). In scenarios with no fraud, a validator that accepts everything scores 100% by construction, so those rows measure only the false-reject cost.

## Summary

| Scenario | Readings | Static | Radar-only | Peer-only | **VarunaSetu** | Checks |
|---|---:|---:|---:|---:|---:|:---|
| A storm crosses the city | 1955 | 100.0% | 100.0% | 99.8% | **99.8%** | ✅ 2/2 |
| Two neighbours inflate their harvest | 2231 | 50.0% | 66.7% | 87.5% | **87.5%** | ✅ 3/3 |
| The radar goes blind over the city centre | 2550 | 100.0% | 87.3% | 99.8% | **98.1%** | ✅ 3/3 |
| The radar blinks, then comes back | 652 | 100.0% | 95.9% | 99.5% | **97.4%** | ✅ 3/3 |
| Same evidence, opposite truth | 2311 | 50.0% | 91.2% | 50.0% | **98.3%** | ✅ 4/4 |
| A cell with too few neighbours to compare | 2357 | 75.0% | 90.0% | 50.0% | **90.0%** | ✅ 2/2 |
| Attacks on the door, not the data | 0 | — | — | — | **—** | ✅ 1/1 |
| A real cloudburst nobody else can see *(documented limit)* | 78 | 100.0% | 37.2% | 94.9% | **32.1%** | ✅ 1/1 |
| Three neighbouring cells collude *(documented limit)* | 102 | 0.0% | 100.0% | 0.0% | **25.5%** | ✅ 1/1 |
| A cheat that also lies about the radar *(documented limit)* | 2580 | 75.0% | 50.0% | 50.0% | **50.0%** | ✅ 1/1 |
| Rain starts faster than peers can follow *(documented limit)* | 1680 | 100.0% | 100.0% | 95.6% | **95.6%** | ✅ 1/1 |
| A day in the virtual city | 3313 | 53.9% | 88.3% | 60.8% | **94.1%** | ✅ 3/3 |

## A storm crosses the city

*The control case: honest devices, working radar*

**Situation.** A slow monsoon cell drifts west to east across 37 H3 cells. Every roof has a metered tank and an enrolled, signed device. Nobody cheats and the radar works.

**Simple validators.** Static threshold, radar-only and peer-only all accept honest data, so this case cannot tell them apart. It is here to show the baseline is clean.

**VarunaSetu.** Each reading is checked against the radar bound and the median/MAD of its own H3 cell. Honest readings are accepted with RADAR_AND_PEERS_AGREE, and no escrows are needed.

1955 readings, 1955 HTTP requests, 2.7 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 1955/1955 | 100.0% |
| RADAR_ONLY | 0/0 | 1955/1955 | 100.0% |
| PEER_ONLY | 0/0 | 1952/1955 | 99.8% |
| VARUNASETU | 0/0 | 1952/1955 | 99.8% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 1688, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 264, `SPATIAL_PEER_OUTLIER` 3

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ honest readings accepted — 1952 of 1955 (need ≥ 95%)
- ✅ no escrows left unresolved or expired — 0

## Two neighbours inflate their harvest

*The radar over-reads, so a radar bound alone lets cheating through*

**Situation.** Heavy rain over the core. The radar over-reads there (×1.6, a common convective bias). One household in the core and one next to it claim 2.2× the water they caught.

**Simple validators.** A fixed 10 mm cap accepts both. Radar-only accepts the core cheat because the inflated radar leaves room for it. Peer-only catches both, but only once ≥4 peers exist.

**VarunaSetu.** The physical bound is combined with the robust z-score against same-cell peers. The core cheat is stopped as SPATIAL_PEER_OUTLIER despite the generous radar, and the other as PHYSICAL_BOUND_EXCEEDED or an outlier.

2231 readings, 2231 HTTP requests, 3.3 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/12 | 2219/2219 | 50.0% |
| RADAR_ONLY | 4/12 | 2219/2219 | 66.7% |
| PEER_ONLY | 9/12 | 2218/2219 | 87.5% |
| VARUNASETU | 9/12 | 2218/2219 | 87.5% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 1877, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 344, `SPATIAL_PEER_OUTLIER` 10

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ inflated claims rejected while it rains ≥ 1.5 mm/window — 7 of 7 (need ≥ 95%)
- ✅ honest readings still accepted — 2218 of 2219 (need ≥ 90%)
- ✅ fewer false accepts than the static threshold — VarunaSetu 3 vs static 12

## The radar goes blind over the city centre

*Real rain, dry radar: escrow instead of rejection, then neighbours confirm*

**Situation.** A cloudburst sits under a radar blind spot (beam blockage / outage) for 55 minutes. Honest households report heavy rain while the radar says dry.

**Simple validators.** Radar-only rejects every honest report. That is the failure that makes people stop trusting a purely radar-based system. Static and peer-only accept without ever noticing the conflict.

**VarunaSetu.** Radar says dry but a coherent group of peers reports rain, so readings go to PROVISIONAL_ESCROW instead of being rejected. They are released when H3 neighbours also show rain (or when the radar recovers, see the next scenario).

2550 readings, 2550 HTTP requests, 4.1 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 2550/2550 | 100.0% |
| RADAR_ONLY | 0/0 | 2226/2550 | 87.3% |
| PEER_ONLY | 0/0 | 2544/2550 | 99.8% |
| VARUNASETU | 0/0 | 2502/2550 | 98.1% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 1857, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 367, `RADAR_PEER_CONFLICT_ESCROW` 278, `PHYSICAL_BOUND_EXCEEDED` 42, `SPATIAL_PEER_OUTLIER` 6

**Escrows settled:** radar 0, H3 neighbours 278, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ radar/peer conflict goes to escrow — 278 escrowed readings
- ✅ escrowed readings end up credited — 278 of 278 (need ≥ 90%)
- ✅ fewer false rejects than radar-only — VarunaSetu 48 vs radar-only 324

## The radar blinks, then comes back

*Escrow released by the radar's own late confirmation*

**Situation.** A tight storm drenches one cell while the radar over it is down. Neighbouring cells see only light rain, so they cannot vouch for it. After 36 minutes the radar recovers, well inside the 15-minute escrow window of the first held readings.

**Simple validators.** Radar-only has already rejected the honest readings by the time the radar returns, and the credit is gone. Static and peer-only accept without checking anything the radar later reveals.

**VarunaSetu.** Readings are held as PROVISIONAL_ESCROW. The first later reading with a live radar value in the same cell corroborates every older pending escrow there (ESCROW_CORROBORATED via RADAR). No neighbour was needed and nothing expired.

652 readings, 652 HTTP requests, 1.2 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 652/652 | 100.0% |
| RADAR_ONLY | 0/0 | 625/652 | 95.9% |
| PEER_ONLY | 0/0 | 649/652 | 99.5% |
| VARUNASETU | 0/0 | 635/652 | 97.4% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 563, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 62, `PHYSICAL_BOUND_EXCEEDED` 14, `RADAR_PEER_CONFLICT_ESCROW` 10, `SPATIAL_PEER_OUTLIER` 3

**Escrows settled:** radar 10, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ escrows released by the recovering radar — 10 escrows
- ✅ no escrow expired — 0 expired
- ✅ escrowed readings end up credited — 10 of 10 (need ≥ 95%)

## Same evidence, opposite truth

*The H3 neighbourhood tells a real storm from a staged one*

**Situation.** The radar is down city-wide. In the west a real storm drenches the neighbourhood. Far away in cell 87ffff every household reports the same heavy rain, but it is dry there (a coordinated false claim).

**Simple validators.** At device level the two look identical: dry radar, coherent peers reporting rain. Radar-only rejects both (wrong for the real storm). Peer-only and static accept both (wrong for the fake).

**VarunaSetu.** Both go to escrow. Then H3 decides: the western cells' k-ring neighbours show rain, so its escrows are released as ESCROW_CORROBORATED via H3_NEIGHBORS. The remote cell has no raining neighbours, so its escrows expire as ESCROW_EXPIRED_UNCORROBORATED.

2311 readings, 2311 HTTP requests, 4.5 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/42 | 2269/2269 | 50.0% |
| RADAR_ONLY | 42/42 | 1869/2269 | 91.2% |
| PEER_ONLY | 0/42 | 2269/2269 | 50.0% |
| VARUNASETU | 42/42 | 2190/2269 | 98.3% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 1561, `RADAR_PEER_CONFLICT_ESCROW` 415, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 284, `INSUFFICIENT_PEERS_REVIEW` 28, `PHYSICAL_BOUND_EXCEEDED` 23

**Escrows settled:** radar 0, H3 neighbours 345, expired 70. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ escrows released by H3 neighbours — 345 escrows
- ✅ fake cell is not credited — 42 of 42 (need ≥ 95%)
- ✅ real storm is credited — 2190 of 2269 (need ≥ 85%)
- ✅ best balanced accuracy (fraud caught + honest credited) — VarunaSetu 98%, STATIC_THRESHOLD 50%, RADAR_ONLY 91%, PEER_ONLY 50%

## A cell with too few neighbours to compare

*Graceful degradation where peer statistics are impossible*

**Situation.** Cell d7ffff has only 3 metered roofs, fewer than the 4 peers the robust statistics need. One of them claims 3× the rain it caught.

**Simple validators.** Peer-only has nothing to compare against and accepts everything. A fixed cap accepts the cheat as well.

**VarunaSetu.** Without enough peers the verifier falls back to the radar physical bound: honest claims are accepted as RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS, and the claim above the bound is held back as INSUFFICIENT_PEERS_REVIEW.

2357 readings, 2357 HTTP requests, 2.9 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 5/10 | 2347/2347 | 75.0% |
| RADAR_ONLY | 8/10 | 2347/2347 | 90.0% |
| PEER_ONLY | 0/10 | 2347/2347 | 50.0% |
| VARUNASETU | 8/10 | 2347/2347 | 90.0% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 2022, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 327, `INSUFFICIENT_PEERS_REVIEW` 8

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ sparse-cell cheat rejected while it rains ≥ 1.5 mm/window — 8 of 8 (need ≥ 95%)
- ✅ radar-physics fallback used — 327 readings

## Attacks on the door, not the data

*Forged, replayed, stale, malformed and impossible messages*

**Situation.** No rain at all. Thirteen hostile or broken messages hit POST /telemetry/readings: replays, tampering, an unenrolled key, a revoked device, stale and future timestamps, malformed JSON and impossible numbers.

**Simple validators.** A system that only looks at the numbers cannot tell a forged but plausible reading from a real one, and would credit replays twice.

**VarunaSetu.** Ed25519 signatures bind device, time, nonce and payload. Durable idempotency returns the stored result for a replay. Invalid or impossible readings get a precise 4xx and are never credited.

0 readings, 15 HTTP requests, 0.1 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 0/0 | — |
| RADAR_ONLY | 0/0 | 0/0 | — |
| PEER_ONLY | 0/0 | 0/0 | — |
| VARUNASETU | 0/0 | 0/0 | — |

**Reason codes returned by the API:** 

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ every hostile message answered as documented — 13 of 13

| Hostile request | Expected | Got | Numbers-only system |
|---|---|---|---|
| ✅ Replayed message | 200 | 200 RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS | would accept |
| ✅ Same nonce, different numbers | 409 | 409 | would accept |
| ✅ Tampered in transit | 401 | 401 | would accept |
| ✅ Device that was never enrolled | 401 | 401 | would accept |
| ✅ Revoked device | 401 | 401 | would accept |
| ✅ Stale message (30 min old) | 401 | 401 | would accept |
| ✅ Clock in the future (+10 min) | 401 | 401 | would accept |
| ✅ Unknown field in payload | 400 | 400 | — |
| ✅ Invalid H3 index | 400 | 400 | — |
| ✅ Unknown building | 404 | 404 | — |
| ✅ Impossible roof (area 0) | 422 INVALID_INPUT | 422 INVALID_INPUT | — |
| ✅ Negative litres | 422 INVALID_INPUT | 422 INVALID_INPUT | — |
| ✅ Claim larger than the tank can hold | 422 STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED | 422 STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED | — |

## A real cloudburst nobody else can see

*Known limit: a genuine one-cell storm missed by radar is not credited*

> **Documented limit.** This scenario shows something VarunaSetu does *not* solve; it is included on purpose.

**Situation.** A genuinely hyper-local cloudburst soaks a single cell. The radar misses it and the neighbouring cells stay dry. Every household in the cell honestly reports heavy rain.

**Simple validators.** Radar-only rejects it. Static and peer-only accept it, but they would accept a staged copy of it too.

**VarunaSetu.** By its data this is indistinguishable from the staged claim in 'Same evidence'. The system fails safe: readings are escrowed, nothing corroborates them, and they expire as ESCROW_EXPIRED_UNCORROBORATED. Honest users lose credit, and that is the price of not paying fraud.

78 readings, 78 HTTP requests, 1.6 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 78/78 | 100.0% |
| RADAR_ONLY | 0/0 | 29/78 | 37.2% |
| PEER_ONLY | 0/0 | 74/78 | 94.9% |
| VARUNASETU | 0/0 | 25/78 | 32.1% |

**Reason codes returned by the API:** `RADAR_PEER_CONFLICT_ESCROW` 38, `RADAR_AND_PEERS_AGREE` 21, `PHYSICAL_BOUND_EXCEEDED` 11, `SPATIAL_PEER_OUTLIER` 4, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 4

**Escrows settled:** radar 0, H3 neighbours 0, expired 38. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ escrows expire uncorroborated (fail safe) — 38 expired

## Three neighbouring cells collude

*Known limit: H3 corroboration alone cannot stop a coordinated cluster*

> **Documented limit.** This scenario shows something VarunaSetu does *not* solve; it is included on purpose.

**Situation.** No rain anywhere. Every device in three mutually adjacent cells (17 devices) fabricates the same heavy rainfall.

**Simple validators.** Static and peer-only accept it. Radar-only rejects it (dry radar).

**VarunaSetu.** Honest limitation: each colluding cell has two colluding neighbours with ≥3 devices each, so the neighbour rule is satisfied and the fraud is credited. The verifier cannot tell a corroborating neighbour from a complicit one. H3 gives locality, not trust (HANDOFF.md K1/C2).

102 readings, 102 HTTP requests, 0.4 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/102 | 0/0 | 0.0% |
| RADAR_ONLY | 102/102 | 0/0 | 100.0% |
| PEER_ONLY | 0/102 | 0/0 | 0.0% |
| VARUNASETU | 26/102 | 0/0 | 25.5% |

**Reason codes returned by the API:** `RADAR_PEER_CONFLICT_ESCROW` 76, `INSUFFICIENT_PEERS_REVIEW` 26

**Escrows settled:** radar 0, H3 neighbours 76, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ limit reproduced: colluders corroborate each other — 76 escrows released by colluding neighbours

## A cheat that also lies about the radar

*Known limit: radar is device-supplied in this prototype*

> **Documented limit.** This scenario shows something VarunaSetu does *not* solve; it is included on purpose.

**Situation.** Same sparse cell as 'A cell with too few neighbours', but this cheat also reports a radar value that makes its inflated claim fit the physical bound.

**Simple validators.** Every baseline accepts it, because each one trusts the numbers in the message.

**VarunaSetu.** Honest limitation: radar_mm travels inside the signed payload, so a compromised enrolled device can set it. With too few peers there is nothing else to check against. The fix is a server-side radar feed keyed by H3 cell, which this prototype does not yet have.

2580 readings, 2580 HTTP requests, 3.8 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 5/10 | 2570/2570 | 75.0% |
| RADAR_ONLY | 0/10 | 2570/2570 | 50.0% |
| PEER_ONLY | 0/10 | 2570/2570 | 50.0% |
| VARUNASETU | 0/10 | 2570/2570 | 50.0% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 2245, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 335

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ limit reproduced: radar-spoofing cheat is credited — 10 of 10 fraudulent readings accepted

## Rain starts faster than peers can follow

*Known limit: honest readings at an abrupt onset can be flagged*

> **Documented limit.** This scenario shows something VarunaSetu does *not* solve; it is included on purpose.

**Situation.** Rain goes from nothing to 85 mm/h in four minutes. Everyone is honest and the radar is right.

**Simple validators.** All baselines accept honest data here (as they do in the control case).

**VarunaSetu.** Honest limitation: a device's peers are the latest readings of the last 10 minutes, which at onset still describe the dry window. The first reporters of the new window can look like positive outliers (SPATIAL_PEER_OUTLIER) until most peers have reported it.

1680 readings, 1680 HTTP requests, 2.5 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 0/0 | 1680/1680 | 100.0% |
| RADAR_ONLY | 0/0 | 1680/1680 | 100.0% |
| PEER_ONLY | 0/0 | 1606/1680 | 95.6% |
| VARUNASETU | 0/0 | 1606/1680 | 95.6% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 1318, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 288, `SPATIAL_PEER_OUTLIER` 74

**Escrows settled:** radar 0, H3 neighbours 0, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ limit measured: honest readings rejected at onset — 74 of 1680 honest readings rejected

## A day in the virtual city

*Everything at once: storms, a radar outage, cheaters, a staged cell, attacks*

**Situation.** Two storms cross a 37-cell city for two hours. The radar goes blind over the core for 40 minutes. Two households inflate, a sparse cell has a cheat, a remote cell stages a storm, a meter goes offline, and the API is attacked throughout.

**Simple validators.** No single baseline survives all of it: each one fails in a different way. The scoreboard shows the totals.

**VarunaSetu.** The same code path handles all of it: verifier decisions, escrows resolved by radar or H3 neighbours, expiry for the staged claim, identity rejections, and an audit ledger that re-verifies at the end.

3313 readings, 3320 HTTP requests, 5.3 s wall time.

| Validator | Fraud caught | Honest credited | Balanced |
|---|---:|---:|---:|
| STATIC_THRESHOLD | 6/76 | 3237/3237 | 53.9% |
| RADAR_ONLY | 64/76 | 2993/3237 | 88.3% |
| PEER_ONLY | 17/76 | 3213/3237 | 60.8% |
| VARUNASETU | 69/76 | 3152/3237 | 94.1% |

**Reason codes returned by the API:** `RADAR_AND_PEERS_AGREE` 2535, `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` 441, `RADAR_PEER_CONFLICT_ESCROW` 183, `PHYSICAL_BOUND_EXCEEDED` 106, `SPATIAL_PEER_OUTLIER` 41, `INSUFFICIENT_PEERS_REVIEW` 7

**Escrows settled:** radar 0, H3 neighbours 183, expired 0. **Ledger:** 500 entries re-verified: true.

**Self-checks**

- ✅ honest readings credited — 3152 of 3237 (need ≥ 85%)
- ✅ manipulated readings rejected (staged cell + inflations in real rain) — 66 of 66 (need ≥ 90%)
- ✅ higher balanced accuracy than every baseline — VarunaSetu 94.1% vs best baseline 88.3% (RADAR_ONLY)

| Hostile request | Expected | Got | Numbers-only system |
|---|---|---|---|
| ✅ Replayed message | 200 | 200 RADAR_PEER_CONFLICT_ESCROW | would accept |
| ✅ Tampered in transit | 401 | 401 | would accept |
| ✅ Revoked device | 401 | 401 | would accept |
| ✅ Stale message (30 min old) | 401 | 401 | would accept |
| ✅ Impossible roof (area 0) | 422 INVALID_INPUT | 422 INVALID_INPUT | — |
| ✅ Claim larger than the tank can hold | 422 STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED | 422 STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED | — |
