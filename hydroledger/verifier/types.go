package verifier

import "time"

// Reading is one device report. All fields are required; see CONTRACTS.md for
// the validation rules that decide whether a Reading is INVALID_INPUT.
type Reading struct {
	DeviceID       string
	Timestamp      time.Time // event time; used for all age and TTL arithmetic
	ClaimedLitres  float64
	RoofAreaM2     float64
	RunoffCoeff    float64 // 0 < C <= 1
	StoredLitres   float64
	CapacityLitres float64
	RadarMM        float64
	CellID         string
}

// State is the outcome class of a decision.
type State string

const (
	StateAccept            State = "ACCEPT"
	StateProvisionalEscrow State = "PROVISIONAL_ESCROW"
	StateQuarantine        State = "QUARANTINE"
)

// Reason is the machine-readable reason code attached to a decision.
type Reason string

const (
	ReasonRadarAndPeersAgree      Reason = "RADAR_AND_PEERS_AGREE"
	ReasonRadarPhysicsFallback    Reason = "RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS"
	ReasonInsufficientPeersReview Reason = "INSUFFICIENT_PEERS_REVIEW"
	ReasonRadarPeerConflictEscrow Reason = "RADAR_PEER_CONFLICT_ESCROW"
	ReasonSpatialPeerOutlier      Reason = "SPATIAL_PEER_OUTLIER"
	ReasonPhysicalBoundExceeded   Reason = "PHYSICAL_BOUND_EXCEEDED"
	ReasonInvalidInput            Reason = "INVALID_INPUT"
	ReasonEscrowCorroborated      Reason = "ESCROW_CORROBORATED"
	ReasonEscrowExpired           Reason = "ESCROW_EXPIRED_UNCORROBORATED"
	ReasonCapacityExceeded        Reason = "STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED"
)

// Decision is the result of Evaluate for a single reading.
//
// The diagnostic fields are filled in as far as the evaluation got:
//   - RObsMM and RMaxMM are set once the input passed field validation
//     (they are zero for INVALID_INPUT).
//   - PeerCount is set once the capacity check passed.
//   - PeerMedianMM, PeerMADMM and ZScore are only meaningful when
//     PeerCount >= Config.MinPeers; otherwise they are zero.
type Decision struct {
	State  State
	Reason Reason

	RObsMM       float64 // effective depth implied by the claim
	RMaxMM       float64 // Alpha*RadarMM + Beta
	PeerCount    int     // eligible peers found in the cell window
	PeerMedianMM float64
	PeerMADMM    float64
	ZScore       float64
}

// Resolution reports the final outcome of an escrowed reading. It is returned
// by Verifier.ResolveEscrows, exactly once per escrow.
type Resolution struct {
	DeviceID         string
	CellID           string
	ReadingTimestamp time.Time // Timestamp of the escrowed reading
	ResolvedAt       time.Time // the "now" passed to ResolveEscrows
	State            State     // StateAccept or StateQuarantine
	Reason           Reason    // ReasonEscrowCorroborated or ReasonEscrowExpired
}
