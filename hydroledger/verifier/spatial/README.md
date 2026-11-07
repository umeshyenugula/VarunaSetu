# spatial — H3 indexing for the verifier

`hydroledger/verifier/spatial` maps a geographic coordinate to a canonical H3
cell and derives the k-ring that is passed to `verifier.Verifier.SetNeighbors`.
It is a thin adapter; it makes no water-claim decision.

Status: **IMPLEMENTED** for Phase 1. It is not field-validated and it does not
make HydroLedger Sybil-proof or collusion-proof.

## Defaults and configuration

| Field | Default | Meaning |
|---|---:|---|
| `Resolution` | 9 | H3 resolution 0–15 |
| `K` | 1 | k-ring radius 0–`MaxK` (10) |

`New(Config)` validates both fields. `MaxK = 10` is a project bound that caps a
single lookup at `3*k*(k+1)+1` cells. A `k` of 0 disables neighbour lookup.

Coordinates are validated before conversion: latitude must be finite and in
`[-90, 90]`; longitude must be finite and in `[-180, 180]`. This check is
required because the H3 binding only rejects NaN/Inf and silently wraps
out-of-range coordinates.

Cell ids are canonical lower-case hexadecimal H3 indexes. Non-canonical
spellings (upper case, leading zeros, surrounding whitespace) are rejected so
one cell cannot appear under two strings in verifier state or persisted data.

`NeighborsOf` / `Indexer.Neighbors` exclude the centre cell and return a sorted,
duplicate-free list. The verifier's neighbour relation is **directed**; the
adapter configures only the cell named in the call. Call it for every cell whose
escrows should be corroborated.

## Resolution and peer density

H3 resolution changes the cell area and therefore the number of devices that can
form a peer set. Each step to a finer resolution shrinks a cell by roughly a
factor of seven. Cell areas below were measured with the pinned H3 binding for
the reference coordinate `17.5449, 78.3864` (Hyderabad); they are illustrative
for that location, not a global constant.

| Resolution | Cell area at the reference point | k=1 neighbourhood (approx.) |
|---:|---:|---:|
| 7 | 5.28 km² | 37.0 km² |
| 8 | 0.754 km² | 5.28 km² |
| 9 | 0.108 km² | 0.754 km² |
| 10 | 0.0154 km² | 0.108 km² |
| 11 | 0.00220 km² | 0.0154 km² |

If devices are spread uniformly at density `d` devices/km², the expected number
of devices in a cell is about `d × area`, and in the six-cell k=1 ring about
`6 × d × area`. For an illustrative `d = 100 devices/km²`:

| Resolution | Expected devices in the cell | Expected in the k=1 ring |
|---:|---:|---:|
| 8 | ~75 | ~450 |
| 9 | ~11 | ~65 |
| 10 | ~1.5 | ~9 |
| 11 | ~0.2 | ~1.3 |

Consequences:

- Coarser cells (lower resolution) give more peers, so the configured
  `MinPeers = 4` is easier to satisfy, but they mix distinct microclimates and
  widen the pool an attacker can coordinate within.
- Finer cells (higher resolution) make "same cell" a stronger spatial claim but
  can starve the peer quorum. At the illustrative density, resolution 11 would
  usually have fewer than four peers.
- Resolution 9 is the project default because it is neighbourhood-sized
  (~0.1 km²) and still plausibly yields a peer quorum in dense urban rooftop
  deployments. This is an assumption, not a measured Hyderabad result.

The same trade-off is why H3 alone does not solve collusion: a larger cell means
more devices are allowed to corroborate each other, and a smaller cell does not
stop devices in adjacent cells from coordinating. `TestKnown_CrossCellCollusion
Corroborates` in the verifier package documents that limitation.

## API

```go
ix, err := spatial.New(spatial.DefaultConfig())       // res 9, k 1
cell, err := ix.Cell(17.5449, 78.3864)                // "8960b194b87ffff"
cell, nbrs, err := ix.Neighbors(17.5449, 78.3864)     // centre + 6 cells

a, err := spatial.NewAdapter(ix)
cell, err = a.SetNeighborsForCoordinate(v, 17.5449, 78.3864)
err = a.SetNeighborsForCell(v, cell)                  // v is any NeighborSetter
```

`NeighborSetter` is the one-method subset of `verifier.Verifier`; the adapter
therefore has no non-test dependency on the verifier package.

## Dependency

Pinned in `go.mod`:

```
github.com/uber/h3-go/v4 v4.5.1-0.20261104181000-32ae003dbaa6
```

Reason: real H3 indexing requires a maintained H3 implementation. The tagged
`v4.5.0` of this module is a cgo binding; this machine builds with
`CGO_ENABLED=0` and has no C compiler, so the pinned untagged pure-Go commit is
used. It is the official Uber repository and targets Go 1.24, matching the
module directive. The dependency is confined to this package.

Tests do not use the network. A fresh module cache needs `go mod download`
before the first build; after that the suite runs offline.

## Tests

```
go test ./hydroledger/verifier/spatial -count=1 -v
```

The known-coordinate test pins the H3 cell at resolutions 7–11 and the exact
k=1 neighbour set at resolution 9. Invalid latitude/longitude, resolution, k and
non-canonical cell ids are covered in `spatial_test.go`.

## Limitations

- H3 does not independently solve cross-cell collusion; it only defines which
  cells may corroborate.
- The k-ring is geometric. It does not model buildings, terrain, drainage
  catchments, radar coverage or device density.
- Pentagons have five neighbours, not six; the API returns whatever H3 returns.
- The adapter is stateless and keeps no registry of configured cells. It does
  not persist neighbour lists.
- Resolution 9 and `MaxK = 10` are project choices, not field-calibrated values.
