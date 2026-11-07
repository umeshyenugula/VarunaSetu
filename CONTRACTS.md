# CONTRACTS

Public contract of `hydroledger-prototype/hydroledger/verifier`. This is a
prototype rebuilt from a written specification. It has not been reviewed for
security and offers no protection against Sybil or collusion attacks (see
`HANDOFF.md`, "Known limitations"). Wherever the specification was silent the
behaviour below follows an assumption `A<n>` recorded in `HANDOFF.md`.

## Public API

```go
package verifier

func DefaultConfig() Config
func (c Config) Validate() error
func New(cfg Config) (*Verifier, error)

func (v *Verifier) Evaluate(r Reading) Decision
func (v *Verifier) SetNeighbors(cellID string, neighbors []string)
func (v *Verifier) ResolveEscrows(now time.Time) []Resolution
func (v *Verifier) PendingEscrows() int
func (v *Verifier) Config() Config
```

| Type | Fields |
|---|---|
| `Reading` | `DeviceID string`, `Timestamp time.Time`, `ClaimedLitres`, `RoofAreaM2`, `RunoffCoeff`, `StoredLitres`, `CapacityLitres`, `RadarMM float64`, `CellID string` |
| `Decision` | `State`, `Reason`, `RObsMM`, `RMaxMM float64`, `PeerCount int`, `PeerMedianMM`, `PeerMADMM`, `ZScore float64` |
| `Resolution` | `DeviceID`, `CellID string`, `ReadingTimestamp`, `ResolvedAt time.Time`, `State`, `Reason` |

`Decision` diagnostics: `RObsMM`/`RMaxMM` are set once field validation passed;
`PeerCount` once the capacity check passed; `PeerMedianMM`, `PeerMADMM` and
`ZScore` only when `PeerCount >= Config.MinPeers` (zero otherwise).

## Definitions

- Effective depth: `R_obs = ClaimedLitres / (RoofAreaM2 * RunoffCoeff)` mm.
- Physical bound: `R_max = Alpha * RadarMM + BetaMM`.
- Peers of a reading: observations in the same cell from *other* devices, one
  (the latest) per device, with `reading.Timestamp - peer.Timestamp <= PeerMaxAge`.
- Robust score: `M = median(peers)`, `MAD = median(|x - M|)`,
  `Z = ZScale * |R_obs - M| / (MAD + MADFloorMM)`.
- Positive outlier: `Z > OutlierZ` and `R_obs > M`.
- Radar dry: `RadarMM <= RadarDryMM`. Peers rain: `M >= PeerRainMM`. Peers
  coherent: `max - min <= max(CoherenceAbsMM, CoherenceRelFrac * M)`.

## Input validation (`INVALID_INPUT`)

A reading is invalid if any of: empty `DeviceID` or `CellID`; zero `Timestamp`;
any NaN/Inf field; negative `ClaimedLitres`, `StoredLitres`, `CapacityLitres` or
`RadarMM`; `RoofAreaM2 <= 0`; `RunoffCoeff` outside `(0, 1]`; or `R_obs`/`R_max`
not finite. Invalid readings are not recorded.

## States

| State | Meaning |
|---|---|
| `ACCEPT` | Claim accepted. |
| `PROVISIONAL_ESCROW` | Held in memory pending corroboration; resolved later by `ResolveEscrows`. |
| `QUARANTINE` | Claim rejected / held for review. |

## Reason codes

| Reason | State | Returned by | When |
|---|---|---|---|
| `INVALID_INPUT` | QUARANTINE | Evaluate | Input validation failed. |
| `STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED` | QUARANTINE | Evaluate | `StoredLitres > CapacityLitres` or `ClaimedLitres > CapacityLitres - StoredLitres`. |
| `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` | ACCEPT | Evaluate | Fewer than `MinPeers` peers and `R_obs <= R_max`. |
| `INSUFFICIENT_PEERS_REVIEW` | QUARANTINE | Evaluate | Fewer than `MinPeers` peers and `R_obs > R_max` (A13). |
| `SPATIAL_PEER_OUTLIER` | QUARANTINE | Evaluate | Enough peers and a positive outlier. |
| `RADAR_PEER_CONFLICT_ESCROW` | PROVISIONAL_ESCROW | Evaluate | Enough peers, not a positive outlier, radar dry, peers rain and are coherent. |
| `PHYSICAL_BOUND_EXCEEDED` | QUARANTINE | Evaluate | Enough peers, none of the above, `R_obs > R_max`. |
| `RADAR_AND_PEERS_AGREE` | ACCEPT | Evaluate | Enough peers, none of the above, `R_obs <= R_max`. |
| `ESCROW_CORROBORATED` | ACCEPT | ResolveEscrows | Escrow confirmed by radar or by neighbor cells. |
| `ESCROW_EXPIRED_UNCORROBORATED` | QUARANTINE | ResolveEscrows | Escrow older than `EscrowTTL` without corroboration. |

## Evaluate: order of checks

1. Field validation, then capacity. Failures return immediately and are not recorded.
2. Collect peers. If `len(peers) < MinPeers`: fallback accept or `INSUFFICIENT_PEERS_REVIEW`.
3. Otherwise, first match wins: positive outlier, then radar/peer conflict
   (escrow), then `R_obs > R_max`, then accept.
4. Any reading that passed step 1 is stored as its device's latest observation
   **whatever the verdict** (A7). A reading with `RadarMM > RadarDryMM` also marks
   older pending escrows in its cell as radar-corroborated (A15).

## Escrow lifecycle

- Created by `Evaluate` with `RADAR_PEER_CONFLICT_ESCROW`; in memory only; the TTL starts at the escrowed reading's `Timestamp`.
- Resolved only inside `ResolveEscrows(now)`, which checks each escrow whose reading is not later than `now`, in this order:
  1. radar-corroborated (a later reading in the same cell with `RadarMM > RadarDryMM`, strictly later than the escrowed reading and no more than `EscrowTTL` after it, was seen by `Evaluate`) → `ACCEPT / ESCROW_CORROBORATED`;
  2. `now - Timestamp > EscrowTTL` → `QUARANTINE / ESCROW_EXPIRED_UNCORROBORATED` (exactly `EscrowTTL` is still pending);
  3. at least `NeighborMinCells` cells configured via `SetNeighbors` each have at least `NeighborMinDevices` devices observed within `PeerMaxAge` of `now` and a median `>= PeerRainMM` → `ACCEPT / ESCROW_CORROBORATED`;
  4. otherwise it stays pending.
- Each escrow is returned exactly once, ordered by cell, reading time, device. Nothing else resolves or expires escrows: callers must call `ResolveEscrows` periodically, with `now` on the same time base as `Reading.Timestamp`.
- `SetNeighbors(cell, list)` replaces the list; it is directed; empty ids, duplicates and `cell` itself are dropped. No geometry (H3 or otherwise) is involved.

## Config and defaults

`New` rejects an invalid `Config` (a zero `Config` is invalid); use `DefaultConfig()`.

| Field | Default | Use |
|---|---|---|
| `Alpha` | 1.15 | `R_max` slope |
| `BetaMM` | 2.0 | `R_max` offset (mm) |
| `MinPeers` | 4 | minimum eligible peers |
| `PeerMaxAge` | 10m | peer eligibility; also "recent" for neighbor devices |
| `WindowMaxDevices` | 128 | per-cell bound (A6; not given by the spec) |
| `ZScale` | 0.6745 | Z numerator factor |
| `MADFloorMM` | 0.15 | added to MAD in the Z divisor |
| `OutlierZ` | 3.5 | outlier if `Z >` this |
| `PeerRainMM` | 2.0 | "peers report rain"; also neighbor median threshold |
| `CoherenceAbsMM` | 1.0 | coherence floor (mm) |
| `CoherenceRelFrac` | 0.25 | coherence relative width |
| `RadarDryMM` | 0.5 | radar dry iff `<=`; later radar corroborates iff `>` |
| `EscrowTTL` | 15m | escrow lifetime |
| `NeighborMinCells` | 2 | neighbor cells needed |
| `NeighborMinDevices` | 3 | devices needed per neighbor cell |

## Concurrency

A `*Verifier` is safe for concurrent use by multiple goroutines; do not copy it.
Each cell has its own lock, so evaluations in different cells do not wait on each
other's cell lock (they share only a read lock on the cell index); evaluations in
the same cell serialise. Locks are never nested across cells.
`ResolveEscrows` calls are serialised with each other.

## Not guaranteed

Persistence, replay/duplicate detection, device authentication, clock-skew
handling, resistance to coordinated or Sybil reporting, and bounds on the number
of cells or pending escrows. See `HANDOFF.md`.
