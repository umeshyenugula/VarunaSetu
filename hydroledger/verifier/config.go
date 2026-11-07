package verifier

import (
	"fmt"
	"math"
	"time"
)

// Config holds every tunable of the verifier. Use DefaultConfig for the
// specified defaults; a zero Config is not valid.
type Config struct {
	// Physical bound: R_max = Alpha*RadarMM + BetaMM.
	Alpha  float64 // default 1.15
	BetaMM float64 // default 2.0

	// Peer window.
	MinPeers         int           // default 4
	PeerMaxAge       time.Duration // default 10m; a peer is eligible when age <= PeerMaxAge
	WindowMaxDevices int           // default 128 (assumption: spec only says "bounded")

	// Robust score: Z = ZScale*|R_obs-M| / (MAD+MADFloorMM); outlier if Z > OutlierZ.
	ZScale     float64 // default 0.6745
	MADFloorMM float64 // default 0.15
	OutlierZ   float64 // default 3.5

	// Peer rain / coherence.
	PeerRainMM       float64 // default 2.0; median >= this means "peers report rain"
	CoherenceAbsMM   float64 // default 1.0
	CoherenceRelFrac float64 // default 0.25; coherent if spread <= max(CoherenceAbsMM, CoherenceRelFrac*median)

	// Escrow.
	RadarDryMM         float64       // default 0.5; radar is "dry" when RadarMM <= this; a later radar > this corroborates
	EscrowTTL          time.Duration // default 15m
	NeighborMinCells   int           // default 2
	NeighborMinDevices int           // default 3
}

// DefaultConfig returns the specified defaults.
func DefaultConfig() Config {
	return Config{
		Alpha:  1.15,
		BetaMM: 2.0,

		MinPeers:         4,
		PeerMaxAge:       10 * time.Minute,
		WindowMaxDevices: 128,

		ZScale:     0.6745,
		MADFloorMM: 0.15,
		OutlierZ:   3.5,

		PeerRainMM:       2.0,
		CoherenceAbsMM:   1.0,
		CoherenceRelFrac: 0.25,

		RadarDryMM:         0.5,
		EscrowTTL:          15 * time.Minute,
		NeighborMinCells:   2,
		NeighborMinDevices: 3,
	}
}

// Validate reports the first problem found in c, or nil.
func (c Config) Validate() error {
	nonNeg := func(name string, v float64) error {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("verifier: config %s must be finite and >= 0, got %v", name, v)
		}
		return nil
	}
	pos := func(name string, v float64) error {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return fmt.Errorf("verifier: config %s must be finite and > 0, got %v", name, v)
		}
		return nil
	}
	for _, err := range []error{
		nonNeg("Alpha", c.Alpha),
		nonNeg("BetaMM", c.BetaMM),
		pos("ZScale", c.ZScale),
		pos("MADFloorMM", c.MADFloorMM), // must be > 0: it is the divisor floor
		pos("OutlierZ", c.OutlierZ),
		nonNeg("PeerRainMM", c.PeerRainMM),
		nonNeg("CoherenceAbsMM", c.CoherenceAbsMM),
		nonNeg("CoherenceRelFrac", c.CoherenceRelFrac),
		nonNeg("RadarDryMM", c.RadarDryMM),
	} {
		if err != nil {
			return err
		}
	}
	switch {
	case c.MinPeers < 1:
		return fmt.Errorf("verifier: config MinPeers must be >= 1, got %d", c.MinPeers)
	case c.PeerMaxAge <= 0:
		return fmt.Errorf("verifier: config PeerMaxAge must be > 0, got %v", c.PeerMaxAge)
	case c.WindowMaxDevices < c.MinPeers+1:
		return fmt.Errorf("verifier: config WindowMaxDevices (%d) must be >= MinPeers+1 (%d), or the minimum can never be reached",
			c.WindowMaxDevices, c.MinPeers+1)
	case c.EscrowTTL <= 0:
		return fmt.Errorf("verifier: config EscrowTTL must be > 0, got %v", c.EscrowTTL)
	case c.NeighborMinCells < 1:
		return fmt.Errorf("verifier: config NeighborMinCells must be >= 1, got %d", c.NeighborMinCells)
	case c.NeighborMinDevices < 1:
		return fmt.Errorf("verifier: config NeighborMinDevices must be >= 1, got %d", c.NeighborMinDevices)
	}
	return nil
}
