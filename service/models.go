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
	ID             string            `json:"id"`
	Decision       verifier.Decision `json:"decision"`
	CreatedAt      time.Time         `json:"created_at"`
	Existing       bool              `json:"-"`
	Corroborated   int               `json:"corroborated_escrows,omitempty"`
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
