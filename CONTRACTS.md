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

---

# Phase 1 additive contracts

The packages below were added in Phase 1. They do not change any existing
`verifier` type, function, reason code or default.

## Package `hydroledger-prototype/hydroledger/verifier/spatial`

H3 coordinate indexing and the adapter that feeds `Verifier.SetNeighbors`.

```go
package spatial

const (
    MinResolution    = 0
    MaxResolution    = 15
    DefaultResolution = 9
    DefaultK          = 1
    MaxK              = 10
)

var (
    ErrInvalidLatitude
    ErrInvalidLongitude
    ErrInvalidResolution
    ErrInvalidK
    ErrInvalidCell
    ErrNilIndexer
    ErrNilNeighborSetter
)

func DefaultConfig() Config
func (c Config) Validate() error

type Config struct {
    Resolution int // default 9, valid 0..15
    K          int // default 1, valid 0..MaxK
}

func New(cfg Config) (*Indexer, error)
func (ix *Indexer) Resolution() int
func (ix *Indexer) K() int

func CellAt(lat, lng float64, resolution int) (string, error)
func (ix *Indexer) Cell(lat, lng float64) (string, error)
func (ix *Indexer) Neighbors(lat, lng float64) (cell string, neighbors []string, err error)
func NeighborsOf(cellID string, k int) ([]string, error)
func (ix *Indexer) NeighborsOf(cellID string) ([]string, error)
func ValidateCell(cellID string) error

type NeighborSetter interface {
    SetNeighbors(cellID string, neighbors []string)
}

func NewAdapter(ix *Indexer) (*Adapter, error)
func (a *Adapter) SetNeighborsForCoordinate(setter NeighborSetter, lat, lng float64) (string, error)
func (a *Adapter) SetNeighborsForCell(setter NeighborSetter, cellID string) error
```

### Behaviour

- `lat`/`lng` are degrees. Latitude must be finite and in `[-90, 90]`;
  longitude must be finite and in `[-180, 180]`. The adapter rejects
  out-of-range values because the H3 binding itself only rejects NaN/Inf and
  wraps them.
- `resolution` must be in `[0, 15]`; `k` must be in `[0, 10]`.
- Cell ids are canonical lower-case hexadecimal H3 indexes. Empty, malformed,
  uppercase, leading-zero and whitespace-padded ids are rejected.
- `NeighborsOf`/`Neighbors` exclude the centre cell, return unique ids sorted
  lexicographically, and return an empty slice for `k = 0`. Pentagons return
  fewer than six k=1 neighbours.
- `Adapter` calls `SetNeighbors` exactly once and returns the centre cell id.
  A nil `Indexer` or nil `NeighborSetter` is rejected. `*verifier.Verifier`
  satisfies `NeighborSetter`; the adapter has no non-test dependency on the
  verifier package.
- The verifier's neighbour relation is directed. The adapter configures only
  the cell named in the call; callers must configure every cell whose escrows
  need corroboration.
- No state is persisted. H3 geometry does not solve cross-cell collusion.

### H3 dependency

`github.com/uber/h3-go/v4 v4.5.1-0.20261104181000-32ae003dbaa6` (pure-Go
commit; the tagged `v4.5.0` is cgo). The dependency is pinned in `go.mod` and
confined to this package. See `hydroledger/verifier/spatial/README.md` for the
resolution/peer-density discussion.

## Package `hydroledger-prototype/hydroledger/verifier/identity`

Ed25519 device identity, signed telemetry, replay protection and an in-memory
enrollment/revocation registry. Cryptography is from the Go standard library.

```go
package identity

const (
    MaxDeviceIDBytes = 128
    MaxNonceBytes    = 128
    MaxPayloadBytes  = 64 * 1024

    DefaultWindow     = 5 * time.Minute
    DefaultFutureSkew = 30 * time.Second
)

type Message struct {
    DeviceID  string
    Timestamp time.Time
    Nonce     string
    Payload   []byte
}

type Envelope struct {
    Message
    Signature []byte
}

func GenerateKeyPair(deviceID string) (*KeyPair, error)
func NewKeyPair(deviceID string, privateKey ed25519.PrivateKey) (*KeyPair, error)
func NewKeyPairFromSeed(deviceID string, seed []byte) (*KeyPair, error)
func (k *KeyPair) DeviceID() string
func (k *KeyPair) PublicKey() ed25519.PublicKey
func (k *KeyPair) PrivateKey() ed25519.PrivateKey
func (k *KeyPair) Seed() []byte
func (k *KeyPair) Signer() *Signer

func NewSigner(deviceID string, privateKey ed25519.PrivateKey) (*Signer, error)
func (s *Signer) DeviceID() string
func (s *Signer) PublicKey() ed25519.PublicKey
func (s *Signer) Sign(timestamp time.Time, nonce string, payload []byte) (Envelope, error)
func NewNonce() (string, error)

func VerifySignature(env Envelope, publicKey ed25519.PublicKey) error

type PublicKeyStore interface {
    PublicKey(deviceID string) (ed25519.PublicKey, error)
}
type RevocationList interface {
    IsRevoked(deviceID string) bool
}
type Enroller interface {
    Enroll(deviceID string, publicKey ed25519.PublicKey) error
}

func NewRegistry() *Registry
func (r *Registry) Enroll(deviceID string, publicKey ed25519.PublicKey) error
func (r *Registry) PublicKey(deviceID string) (ed25519.PublicKey, error)
func (r *Registry) Enrolled(deviceID string) bool
func (r *Registry) Revoke(deviceID string) error
func (r *Registry) IsRevoked(deviceID string) bool

func NewRevocationSet() *RevocationSet
func (s *RevocationSet) Revoke(deviceID string) error
func (s *RevocationSet) IsRevoked(deviceID string) bool

func DefaultConfig() Config
func (c Config) Validate() error

type Config struct {
    Window     time.Duration // default 5m; must be > 0
    FutureSkew time.Duration // default 30s; must be >= 0
}

func NewVerifier(cfg Config, keys PublicKeyStore, revoked RevocationList) (*Verifier, error)
func (v *Verifier) Config() Config
func (v *Verifier) Verify(env Envelope, now time.Time) (Message, error)
```

### Signed message

`Sign` sets `DeviceID` from the signer, rounds `Timestamp` to remove the
monotonic reading, copies `Payload`, and signs the following canonical
length-prefixed encoding:

```
"hydroledger/identity/telemetry/v1" 0x00
uint32(len(DeviceID)) || DeviceID
int64(Timestamp.Unix()) || uint32(Timestamp.Nanosecond())
uint32(len(Nonce)) || Nonce
uint32(len(Payload)) || Payload
```

The domain string gives cross-protocol separation. `Signature` itself is not
covered. Ed25519 signatures are deterministic, so identical messages produce
identical signatures. `VerifySignature` validates lengths and the signature but
does **not** check enrollment, revocation, time or replay; use
`Verifier.Verify` for the full check.

`NewNonce` returns 16 random bytes from `crypto/rand` encoded as unpadded
base64url (22 characters). A nonce is required and must be unique per device
within the replay retention window.

### Verification

`Verifier.Verify(env, now)` checks, in order:

1. `now` is non-zero and the message structure is valid (device id 1..128
   bytes, non-zero timestamp, nonce 1..128 bytes, payload <= 64 KiB).
2. The device id is enrolled in the `PublicKeyStore`.
3. The device is not in the `RevocationList`.
4. The Ed25519 signature verifies with that device's public key.
5. `now - Timestamp <= Window` and `Timestamp - now <= FutureSkew`. Both
   boundaries are inclusive.
6. The `(DeviceID, Nonce)` pair has not been accepted inside the retention
   period.

Only a message that passes all earlier checks consumes its nonce. Replay
retention is `Window + FutureSkew`; entries older than that are pruned. The
replay key is the device id plus nonce, so different devices may use the same
nonce. Replay state is in memory and is lost on restart. `Verify` returns a deep
copy of the payload.

`Verifier` and `Signer` are safe for concurrent use. Concurrent verification of
the same envelope accepts at most one call.

### Enrollment and revocation

- `Registry.Enroll` rejects a duplicate device id (no silent key rotation) and
  a revoked device id. `PublicKey` returns `ErrUnknownDevice` for an unknown id.
- `Revoke` is idempotent and permanently rejects the id for the lifetime of the
  registry. Revoking an id that was never enrolled is allowed, so a later
  enrollment is refused. There is no un-revoke.
- Both `Registry` and `RevocationSet` are in memory only. Persistence and the
  authority that authorises enrollment are later phases. Construct them with
  `NewRegistry` / `NewRevocationSet`; their zero values are not initialised.

### Errors

| Error | Meaning |
|---|---|
| `ErrInvalidDeviceID` | Device id empty or longer than 128 bytes. |
| `ErrInvalidPublicKey` | Public key not 32 bytes. |
| `ErrInvalidPrivateKey` | Private key not 64 bytes. |
| `ErrInvalidSeed` | Seed not 32 bytes. |
| `ErrSignatureSize` | Signature not 64 bytes. |
| `ErrSignatureInvalid` | Signature does not verify. |
| `ErrNonceInvalid` | Nonce empty or longer than 128 bytes. |
| `ErrPayloadTooLarge` | Payload larger than 64 KiB. |
| `ErrTimestampZero` | Message timestamp is the zero time. |
| `ErrZeroNow` | `Verify` called with a zero reference time. |
| `ErrUnknownDevice` | Device id not enrolled. |
| `ErrDeviceRevoked` | Device id is revoked. |
| `ErrDeviceAlreadyEnrolled` | Device id already has a key. |
| `ErrNonceReused` | `(device, nonce)` already accepted inside retention. |
| `ErrTimestampTooOld` | Message older than `Window`. |
| `ErrTimestampTooFuture` | Message further ahead than `FutureSkew`. |
| `ErrNilKeyStore` | `NewVerifier` called with a nil key store. |
| `ErrNilRevocationList` | `NewVerifier` called with a nil revocation list. |

### Security properties and limitations

- A compromised but enrolled device remains trusted: possession of its private
  key is sufficient to sign arbitrary payloads. Revocation is the only
  mitigation exposed here.
- Enrollment authority, identity proofing and Sybil cost are out of scope.
  `Enroll` is an administrative interface, not a proof of device uniqueness.
- Keys, enrollment and replay state are in memory and are lost on restart.
  Replay across a restart is not detected by this package.
- There is no rate limiting. A valid device can fill the nonce map up to the
  retention bound.
- There is no transport security, payload schema, database or ledger. The
  payload is opaque bytes; binding it to `verifier.Reading` is a later phase.
- Timestamp acceptance assumes roughly synchronised clocks and the configured
  skew. A far-future timestamp is rejected, not clamped.
- Error distinctions (unknown vs revoked vs bad signature) are diagnostic and
  may reveal registry membership.
- This package does not make HydroLedger tamper-proof, production-ready or
  Sybil-proof.
