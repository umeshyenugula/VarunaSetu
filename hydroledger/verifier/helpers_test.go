package verifier

import (
	"fmt"
	"testing"
	"time"
)

// t0 is the reference instant for all tests.
var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// With a 100 m2 roof and coefficient 1.0, one litre is 0.01 mm, so depths that
// are multiples of 1/8 mm survive the litres<->mm round trip exactly.
const (
	testRoofM2   = 100.0
	testCoeff    = 1.0
	testCapacity = 1e6
)

// rd builds a valid reading that implies a depth of mm.
func rd(device, cell string, ts time.Time, mm, radar float64) Reading {
	return Reading{
		DeviceID:       device,
		Timestamp:      ts,
		ClaimedLitres:  mm * testRoofM2 * testCoeff,
		RoofAreaM2:     testRoofM2,
		RunoffCoeff:    testCoeff,
		StoredLitres:   0,
		CapacityLitres: testCapacity,
		RadarMM:        radar,
		CellID:         cell,
	}
}

func newVerifier(t testing.TB, mutate ...func(*Config)) *Verifier {
	t.Helper()
	cfg := DefaultConfig()
	for _, m := range mutate {
		m(&cfg)
	}
	v, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

// seed submits one reading per depth from distinct devices "<prefix>-<i>" and
// discards the decisions. It is how tests build up a peer window.
func seed(v *Verifier, cell, prefix string, ts time.Time, radar float64, depths ...float64) {
	for i, mm := range depths {
		v.Evaluate(rd(fmt.Sprintf("%s-%d", prefix, i), cell, ts, mm, radar))
	}
}

// windowLen returns the number of observations held for cell.
func windowLen(v *Verifier, cell string) int {
	c := v.lookupCell(cell)
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.obs)
}

// windowObs returns the observation of device in cell, if any.
func windowObs(v *Verifier, cell, device string) (observation, bool) {
	c := v.lookupCell(cell)
	if c == nil {
		return observation{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.obs {
		if o.device == device {
			return o, true
		}
	}
	return observation{}, false
}

// escrowScenario creates the canonical radar/peer conflict in cell "A" at t0:
// radar is dry, four peers report ~8 mm, and device "claimant" claims 8 mm.
// It fails the test unless the claimant ends up in PROVISIONAL_ESCROW.
func escrowScenario(t *testing.T, mutate ...func(*Config)) *Verifier {
	t.Helper()
	v := newVerifier(t, mutate...)
	seed(v, "A", "peer", t0, 0, 8, 8.25, 7.75, 8)
	got := v.Evaluate(rd("claimant", "A", t0, 8, 0))
	if got.State != StateProvisionalEscrow || got.Reason != ReasonRadarPeerConflictEscrow {
		t.Fatalf("scenario setup: got %s/%s, want PROVISIONAL_ESCROW/RADAR_PEER_CONFLICT_ESCROW", got.State, got.Reason)
	}
	if n := v.PendingEscrows(); n != 1 {
		t.Fatalf("scenario setup: PendingEscrows = %d, want 1", n)
	}
	return v
}
