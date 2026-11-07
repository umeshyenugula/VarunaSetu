package service

import (
	"time"

	"hydroledger-prototype/hydroledger/verifier"
)

// Telemetry is the signed payload carried by a device reading.
type Telemetry struct {
	BuildingID     string  `json:"building_id"`
	ClaimedLitres  float64 `json:"claimed_litres"`
	RoofAreaM2     float64 `json:"roof_area_m2"`
	RunoffCoeff    float64 `json:"runoff_coeff"`
	StoredLitres   float64 `json:"stored_litres"`
	CapacityLitres float64 `json:"capacity_litres"`
	RadarMM        float64 `json:"radar_mm"`
	CellID         string  `json:"h3_index"`
}

func (t Telemetry) Reading(deviceID string, timestamp time.Time) verifier.Reading {
	return verifier.Reading{
		DeviceID:       deviceID,
		Timestamp:      timestamp,
		ClaimedLitres:  t.ClaimedLitres,
		RoofAreaM2:     t.RoofAreaM2,
		RunoffCoeff:    t.RunoffCoeff,
		StoredLitres:   t.StoredLitres,
		CapacityLitres: t.CapacityLitres,
		RadarMM:        t.RadarMM,
		CellID:         t.CellID,
	}
}

type ReadingResult struct {
	ID           string            `json:"id"`
	Decision     verifier.Decision `json:"decision"`
	CreatedAt    time.Time         `json:"created_at"`
	Existing     bool              `json:"-"`
	Corroborated int               `json:"corroborated_escrows,omitempty"`
}

type Escrow struct {
	ID             string     `json:"id"`
	ReadingID      string     `json:"reading_id"`
	State          string     `json:"state"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CorroboratedAt *time.Time `json:"corroborated_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// PeerReading is a persisted reading reconstructed for verifier peer evaluation.
type PeerReading struct {
	Reading verifier.Reading
}

// CellStatus summarizes recent peer activity for a cell.
type CellStatus struct {
	H3Index       string `json:"h3_index"`
	RecentDevices int    `json:"recent_devices"`
	Status        string `json:"status"`
}

// ReadingView is a dashboard-friendly reading projection (additive read API).
type ReadingView struct {
	ID              string    `json:"id"`
	DeviceID        string    `json:"deviceId"`
	Timestamp       time.Time `json:"timestamp"`
	H3Index         string    `json:"h3Index"`
	RainfallMM      float64   `json:"rainfallMm"`
	RadarRainfallMM float64   `json:"radarRainfallMm"`
	PeerConsensusMM *float64  `json:"peerConsensusMm"`
	PeerCount       int       `json:"peerCount"`
	Decision        string    `json:"decision"`
	Reason          string    `json:"reason"`
	Simulated       bool      `json:"simulated"`
}

// EscrowView is a dashboard-friendly escrow projection (additive read API).
type EscrowView struct {
	ID             string     `json:"id"`
	DeviceID       string     `json:"deviceId"`
	H3Index        string     `json:"h3Index"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	CorroboratedAt *time.Time `json:"corroboratedAt,omitempty"`
	Simulated      bool       `json:"simulated"`
}

// CellSummary is a dashboard-friendly cell state projection.
type CellSummary struct {
	H3Index   string `json:"h3Index"`
	State     string `json:"state"`
	Readings  int    `json:"readings"`
	Simulated bool   `json:"simulated"`
}

// LedgerEntryView is a dashboard-friendly audit ledger projection.
type LedgerEntryView struct {
	Seq                       int64     `json:"seq"`
	EventRef                  string    `json:"eventRef"`
	PrevHash                  string    `json:"prevHash"`
	Hash                      string    `json:"hash"`
	Timestamp                 time.Time `json:"timestamp"`
	CheckpointSignatureStatus string    `json:"checkpointSignatureStatus"`
	CheckpointSeq             *int64    `json:"checkpointSeq,omitempty"`
	Simulated                 bool      `json:"simulated"`
}

// AnalyticsSummary is an aggregate dashboard projection computed from persisted data.
type AnalyticsSummary struct {
	TotalReadings        int      `json:"totalReadings"`
	Accepted             int      `json:"accepted"`
	Provisional          int      `json:"provisional"`
	Quarantined          int      `json:"quarantined"`
	EscrowResolutionRate *float64 `json:"escrowResolutionRate"`
	ActiveDevices        int      `json:"activeDevices"`
	SimulatedDataPresent bool     `json:"simulatedDataPresent"`
	Source               string   `json:"source"`
}
