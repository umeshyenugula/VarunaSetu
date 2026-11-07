package verifier

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestEvaluate_Decisions(t *testing.T) {
	tests := []struct {
		name       string
		radar      float64   // radar for the peers and the claimant
		peers      []float64 // depths already reported by distinct other devices
		claimMM    float64
		wantState  State
		wantReason Reason
		wantPeers  int
		wantZHigh  bool // ZScore must exceed the outlier threshold
	}{
		{
			name:  "enough peers and radar agree -> accept",
			radar: 5, peers: []float64{4, 4.25, 3.75, 4}, claimMM: 4,
			wantState: StateAccept, wantReason: ReasonRadarAndPeersAgree, wantPeers: 4,
		},
		{
			name:  "radar dry but coherent peers report rain -> escrow",
			radar: 0, peers: []float64{8, 8.25, 7.75, 8}, claimMM: 8,
			wantState: StateProvisionalEscrow, wantReason: ReasonRadarPeerConflictEscrow, wantPeers: 4,
		},
		{
			name:  "isolated positive outlier -> quarantine",
			radar: 3, peers: []float64{3, 3.25, 2.75, 3}, claimMM: 5,
			wantState: StateQuarantine, wantReason: ReasonSpatialPeerOutlier, wantPeers: 4, wantZHigh: true,
		},
		{
			name:  "positive outlier beats the radar/peer conflict",
			radar: 0, peers: []float64{8, 8.25, 7.75, 8}, claimMM: 20,
			wantState: StateQuarantine, wantReason: ReasonSpatialPeerOutlier, wantPeers: 4, wantZHigh: true,
		},
		{
			name:  "peers agree with claim but radar bound is exceeded (radar not dry)",
			radar: 3, peers: []float64{8, 8.25, 7.75, 8}, claimMM: 8,
			wantState: StateQuarantine, wantReason: ReasonPhysicalBoundExceeded, wantPeers: 4,
		},
		{
			name:  "negative deviation (under-claim) is not quarantined",
			radar: 5, peers: []float64{7, 7.25, 6.75, 7}, claimMM: 1,
			wantState: StateAccept, wantReason: ReasonRadarAndPeersAgree, wantPeers: 4, wantZHigh: true,
		},
		{
			name:  "dry radar, rainy but incoherent peers -> no escrow, bound exceeded",
			radar: 0, peers: []float64{3, 6, 9, 12}, claimMM: 7.5,
			wantState: StateQuarantine, wantReason: ReasonPhysicalBoundExceeded, wantPeers: 4,
		},
		{
			name:  "dry radar, dry peers, claim within bound -> accept",
			radar: 0, peers: []float64{0, 0.25, 0, 0.25}, claimMM: 1,
			wantState: StateAccept, wantReason: ReasonRadarAndPeersAgree, wantPeers: 4,
		},
		{
			name:  "wet radar and rainy peers agree -> accept",
			radar: 8, peers: []float64{8, 8.25, 7.75, 8}, claimMM: 8,
			wantState: StateAccept, wantReason: ReasonRadarAndPeersAgree, wantPeers: 4,
		},
		{
			name:  "peer median exactly at rain threshold counts as rain -> escrow",
			radar: 0, peers: []float64{2, 2, 2, 2}, claimMM: 2,
			wantState: StateProvisionalEscrow, wantReason: ReasonRadarPeerConflictEscrow, wantPeers: 4,
		},
		{
			name:  "peer median just below rain threshold -> accept",
			radar: 0, peers: []float64{1.75, 1.75, 1.75, 1.75}, claimMM: 1.75,
			wantState: StateAccept, wantReason: ReasonRadarAndPeersAgree, wantPeers: 4,
		},
		{
			name:  "coherence uses 25% of median when larger than 1 mm (spread 1.5 <= 2)",
			radar: 0, peers: []float64{7, 8, 8, 8.5}, claimMM: 8,
			wantState: StateProvisionalEscrow, wantReason: ReasonRadarPeerConflictEscrow, wantPeers: 4,
		},
		{
			name:  "coherence just exceeded (spread 2.75 > 2) -> no escrow",
			radar: 0, peers: []float64{6.5, 8, 8, 9.25}, claimMM: 8,
			wantState: StateQuarantine, wantReason: ReasonPhysicalBoundExceeded, wantPeers: 4,
		},
		{
			name:  "coherence floor of 1 mm applies for small medians (spread exactly 1)",
			radar: 0, peers: []float64{2, 2.5, 2.5, 3}, claimMM: 2.5,
			wantState: StateProvisionalEscrow, wantReason: ReasonRadarPeerConflictEscrow, wantPeers: 4,
		},
		{
			name:  "coherence floor just exceeded (spread 1.25 > 1) -> no escrow",
			radar: 0, peers: []float64{2, 2.5, 2.5, 3.25}, claimMM: 2.5,
			wantState: StateQuarantine, wantReason: ReasonPhysicalBoundExceeded, wantPeers: 4,
		},
		{
			name:  "too few peers, within physical bound -> radar physics fallback",
			radar: 5, peers: []float64{4, 4.25, 3.75}, claimMM: 4,
			wantState: StateAccept, wantReason: ReasonRadarPhysicsFallback, wantPeers: 3,
		},
		{
			name:  "no peers at all, within physical bound -> radar physics fallback",
			radar: 5, peers: nil, claimMM: 7.5,
			wantState: StateAccept, wantReason: ReasonRadarPhysicsFallback, wantPeers: 0,
		},
		{
			name:  "too few peers, above physical bound -> quarantine for review",
			radar: 0, peers: []float64{8, 8, 8}, claimMM: 3,
			wantState: StateQuarantine, wantReason: ReasonInsufficientPeersReview, wantPeers: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			seed(v, "A", "p", t0, tc.radar, tc.peers...)
			got := v.Evaluate(rd("me", "A", t0, tc.claimMM, tc.radar))
			if got.State != tc.wantState || got.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s (decision: %+v)", got.State, got.Reason, tc.wantState, tc.wantReason, got)
			}
			if got.PeerCount != tc.wantPeers {
				t.Errorf("PeerCount = %d, want %d", got.PeerCount, tc.wantPeers)
			}
			if tc.wantZHigh && got.ZScore <= 3.5 {
				t.Errorf("ZScore = %v, want > 3.5", got.ZScore)
			}
		})
	}
}

func TestEvaluate_PhysicalBoundBoundary(t *testing.T) {
	// With radar 0, R_max is exactly 2.0 mm (Beta), so equality is testable.
	tests := []struct {
		name       string
		peers      []float64
		claimMM    float64
		wantState  State
		wantReason Reason
	}{
		{"fallback: R_obs == R_max is accepted", nil, 2.0, StateAccept, ReasonRadarPhysicsFallback},
		{"fallback: R_obs just above R_max is quarantined", nil, 2.01, StateQuarantine, ReasonInsufficientPeersReview},
		{"peers: R_obs == R_max is accepted", []float64{1.75, 2, 1.5, 1.75}, 2.0, StateAccept, ReasonRadarAndPeersAgree},
		{"peers: R_obs above R_max is quarantined", []float64{1.75, 2, 1.5, 1.75}, 2.25, StateQuarantine, ReasonPhysicalBoundExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			seed(v, "A", "p", t0, 0, tc.peers...)
			got := v.Evaluate(rd("me", "A", t0, tc.claimMM, 0))
			if got.RMaxMM != 2.0 {
				t.Fatalf("RMaxMM = %v, want 2", got.RMaxMM)
			}
			if got.State != tc.wantState || got.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s (decision: %+v)", got.State, got.Reason, tc.wantState, tc.wantReason, got)
			}
		})
	}
}

func TestEvaluate_RadarBoundary(t *testing.T) {
	// Peers are coherent and report ~8 mm. Radar exactly 0.5 mm is still "dry"
	// (escrow); anything above is not, so the 8 mm claim breaks the bound.
	tests := []struct {
		name       string
		radar      float64
		wantState  State
		wantReason Reason
	}{
		{"radar exactly 0.5 mm is dry", 0.5, StateProvisionalEscrow, ReasonRadarPeerConflictEscrow},
		{"radar just above 0.5 mm is not dry", 0.5000001, StateQuarantine, ReasonPhysicalBoundExceeded},
		{"radar 0 is dry", 0, StateProvisionalEscrow, ReasonRadarPeerConflictEscrow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			seed(v, "A", "p", t0, tc.radar, 8, 8.25, 7.75, 8)
			got := v.Evaluate(rd("me", "A", t0, 8, tc.radar))
			if got.State != tc.wantState || got.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s", got.State, got.Reason, tc.wantState, tc.wantReason)
			}
		})
	}
}

func TestEvaluate_PeerEligibility(t *testing.T) {
	type peer struct {
		device string
		age    time.Duration // reading time minus peer time; negative = peer is newer
	}
	const radar, mm = 5.0, 4.0 // R_max = 7.75, so every claim here is within the bound
	tests := []struct {
		name       string
		peers      []peer
		wantPeers  int
		wantReason Reason
	}{
		{"exactly 3 peers is too few",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}}, 3, ReasonRadarPhysicsFallback},
		{"exactly 4 peers is enough",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"d", 0}}, 4, ReasonRadarAndPeersAgree},
		{"peer exactly 10 minutes old is eligible",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"d", 10 * time.Minute}}, 4, ReasonRadarAndPeersAgree},
		{"peer 10 minutes and 1ns old is not eligible",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"d", 10*time.Minute + 1}}, 3, ReasonRadarPhysicsFallback},
		{"peer 9m59s old is eligible",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"d", 10*time.Minute - time.Second}}, 4, ReasonRadarAndPeersAgree},
		{"stale peers do not count even when numerous",
			[]peer{{"a", 0}, {"b", 11 * time.Minute}, {"c", 12 * time.Minute}, {"d", time.Hour}, {"e", 24 * time.Hour}}, 1, ReasonRadarPhysicsFallback},
		{"one latest observation per device: repeats count once",
			[]peer{{"a", 3 * time.Minute}, {"a", 2 * time.Minute}, {"a", time.Minute}, {"a", 0}, {"b", 0}, {"c", 0}}, 3, ReasonRadarPhysicsFallback},
		{"the evaluated device's own earlier reading is not a peer",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"me", time.Minute}}, 3, ReasonRadarPhysicsFallback},
		{"a peer newer than the reading (negative age) is eligible (assumption A5)",
			[]peer{{"a", 0}, {"b", 0}, {"c", 0}, {"d", -time.Minute}}, 4, ReasonRadarAndPeersAgree},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			for _, p := range tc.peers {
				v.Evaluate(rd(p.device, "A", t0.Add(-p.age), mm, radar))
			}
			got := v.Evaluate(rd("me", "A", t0, mm, radar))
			if got.PeerCount != tc.wantPeers {
				t.Errorf("PeerCount = %d, want %d", got.PeerCount, tc.wantPeers)
			}
			if got.State != StateAccept || got.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want ACCEPT/%s", got.State, got.Reason, tc.wantReason)
			}
		})
	}
}

func TestEvaluate_PeersAreScopedToTheCell(t *testing.T) {
	v := newVerifier(t)
	seed(v, "B", "other", t0, 5, 4, 4, 4, 4, 4)
	got := v.Evaluate(rd("me", "A", t0, 4, 5))
	if got.PeerCount != 0 || got.Reason != ReasonRadarPhysicsFallback {
		t.Errorf("got %s/%s with %d peers, want fallback with 0 peers", got.State, got.Reason, got.PeerCount)
	}
}

func TestEvaluate_InvalidInput(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	tests := []struct {
		name   string
		mutate func(*Reading)
	}{
		{"empty device id", func(r *Reading) { r.DeviceID = "" }},
		{"empty cell id", func(r *Reading) { r.CellID = "" }},
		{"zero timestamp", func(r *Reading) { r.Timestamp = time.Time{} }},
		{"negative claimed litres", func(r *Reading) { r.ClaimedLitres = -1 }},
		{"NaN claimed litres", func(r *Reading) { r.ClaimedLitres = nan }},
		{"+Inf claimed litres", func(r *Reading) { r.ClaimedLitres = inf }},
		{"zero roof area", func(r *Reading) { r.RoofAreaM2 = 0 }},
		{"negative roof area", func(r *Reading) { r.RoofAreaM2 = -5 }},
		{"NaN roof area", func(r *Reading) { r.RoofAreaM2 = nan }},
		{"zero runoff coefficient", func(r *Reading) { r.RunoffCoeff = 0 }},
		{"negative runoff coefficient", func(r *Reading) { r.RunoffCoeff = -0.5 }},
		{"runoff coefficient above 1", func(r *Reading) { r.RunoffCoeff = 1.0000001 }},
		{"NaN runoff coefficient", func(r *Reading) { r.RunoffCoeff = nan }},
		{"negative stored litres", func(r *Reading) { r.StoredLitres = -1 }},
		{"NaN stored litres", func(r *Reading) { r.StoredLitres = nan }},
		{"negative capacity", func(r *Reading) { r.CapacityLitres = -1 }},
		{"+Inf capacity", func(r *Reading) { r.CapacityLitres = inf }},
		{"negative radar", func(r *Reading) { r.RadarMM = -0.1 }},
		{"NaN radar", func(r *Reading) { r.RadarMM = nan }},
		{"+Inf radar", func(r *Reading) { r.RadarMM = inf }},
		{"derived depth overflows to +Inf", func(r *Reading) {
			r.ClaimedLitres, r.RoofAreaM2, r.CapacityLitres = 1e300, 1e-300, 1e301
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			r := rd("d", "A", t0, 3, 5)
			tc.mutate(&r)
			got := v.Evaluate(r)
			if got.State != StateQuarantine || got.Reason != ReasonInvalidInput {
				t.Errorf("got %s/%s, want QUARANTINE/INVALID_INPUT", got.State, got.Reason)
			}
			if n := windowLen(v, "A"); n != 0 {
				t.Errorf("invalid reading was recorded as a peer observation (window=%d)", n)
			}
		})
	}

	t.Run("control: unmutated reading is valid", func(t *testing.T) {
		v := newVerifier(t)
		if got := v.Evaluate(rd("d", "A", t0, 3, 5)); got.State != StateAccept {
			t.Errorf("got %s/%s, want ACCEPT", got.State, got.Reason)
		}
	})
	t.Run("zero claim and zero capacity are valid inputs", func(t *testing.T) {
		v := newVerifier(t)
		r := rd("d", "A", t0, 0, 5)
		r.CapacityLitres = 0
		if got := v.Evaluate(r); got.State != StateAccept {
			t.Errorf("got %s/%s, want ACCEPT", got.State, got.Reason)
		}
	})
}

func TestEvaluate_Capacity(t *testing.T) {
	tests := []struct {
		name                      string
		stored, capacity, claimed float64
		wantState                 State
		wantReason                Reason
		wantRecorded              bool
	}{
		{"stored exceeds capacity", 600, 500, 0, StateQuarantine, ReasonCapacityExceeded, false},
		{"claim exceeds free space", 300, 500, 250, StateQuarantine, ReasonCapacityExceeded, false},
		{"claim just exceeds free space", 300, 500, 200.5, StateQuarantine, ReasonCapacityExceeded, false},
		{"claim exactly fills free space", 300, 500, 200, StateAccept, ReasonRadarPhysicsFallback, true},
		{"stored exactly at capacity, zero claim", 500, 500, 0, StateAccept, ReasonRadarPhysicsFallback, true},
		{"stored exactly at capacity, positive claim", 500, 500, 1, StateQuarantine, ReasonCapacityExceeded, false},
		{"claim larger than whole tank", 0, 500, 501, StateQuarantine, ReasonCapacityExceeded, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			r := rd("d", "A", t0, 0, 5)
			r.StoredLitres, r.CapacityLitres, r.ClaimedLitres = tc.stored, tc.capacity, tc.claimed
			got := v.Evaluate(r)
			if got.State != tc.wantState || got.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s", got.State, got.Reason, tc.wantState, tc.wantReason)
			}
			if rec := windowLen(v, "A") == 1; rec != tc.wantRecorded {
				t.Errorf("recorded as peer = %v, want %v", rec, tc.wantRecorded)
			}
		})
	}
}

func TestEvaluate_CapacityCheckPrecedesPeerRules(t *testing.T) {
	// Peers and radar would let this through, but the tank cannot hold it.
	v := newVerifier(t)
	seed(v, "A", "p", t0, 5, 4, 4, 4, 4)
	r := rd("me", "A", t0, 4, 5)
	r.StoredLitres, r.CapacityLitres = 450, 500 // 400 L claimed, 50 L free
	got := v.Evaluate(r)
	if got.State != StateQuarantine || got.Reason != ReasonCapacityExceeded {
		t.Errorf("got %s/%s, want QUARANTINE/STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED", got.State, got.Reason)
	}
}

func TestEvaluate_RobustScore(t *testing.T) {
	// Peers 1,2,3,4: M = 2.5, deviations 1.5,.5,.5,1.5 so MAD = 1.0.
	// R_obs = 5 -> Z = 0.6745*|5-2.5|/(1.0+0.15).
	v := newVerifier(t)
	seed(v, "A", "p", t0, 10, 1, 2, 3, 4)
	got := v.Evaluate(rd("me", "A", t0, 5, 10))
	wantZ := 0.6745 * 2.5 / 1.15
	if math.Abs(got.ZScore-wantZ) > 1e-9 || got.PeerMedianMM != 2.5 || got.PeerMADMM != 1.0 {
		t.Errorf("median=%v mad=%v z=%v, want 2.5, 1.0, %v", got.PeerMedianMM, got.PeerMADMM, got.ZScore, wantZ)
	}
	if got.RObsMM != 5 || got.RMaxMM != 1.15*10+2.0 {
		t.Errorf("RObsMM=%v RMaxMM=%v", got.RObsMM, got.RMaxMM)
	}
	if got.State != StateAccept || got.Reason != ReasonRadarAndPeersAgree {
		t.Errorf("got %s/%s, want ACCEPT/RADAR_AND_PEERS_AGREE", got.State, got.Reason)
	}
}

func TestEvaluate_OutlierThresholdIsStrict(t *testing.T) {
	// Peers 0,0,0,0: M=0, MAD=0, so Z = 0.6745*x/0.15. Z == 3.5 needs
	// x = 3.5*0.15/0.6745; test just below and just above that depth.
	edge := 3.5 * 0.15 / 0.6745
	for _, tc := range []struct {
		name   string
		mm     float64
		reason Reason
	}{
		{"just below threshold", edge - 0.001, ReasonRadarAndPeersAgree},
		{"just above threshold", edge + 0.001, ReasonSpatialPeerOutlier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			seed(v, "A", "p", t0, 5, 0, 0, 0, 0)
			got := v.Evaluate(rd("me", "A", t0, tc.mm, 5))
			if got.Reason != tc.reason {
				t.Errorf("Z=%v: got %s, want %s", got.ZScore, got.Reason, tc.reason)
			}
		})
	}
}

func TestEvaluate_OutlierThresholdExactEquality(t *testing.T) {
	// With ZScale=1 and MADFloorMM=1, peers at 0 give Z = |R_obs|/1 exactly, so a
	// claim of 3.5 mm lands on Z == 3.5, which is not an outlier (Z must exceed it).
	tests := []struct {
		name   string
		mm     float64
		reason Reason
	}{
		{"Z exactly 3.5 is not an outlier", 3.5, ReasonRadarAndPeersAgree},
		{"Z just above 3.5 is an outlier", 3.75, ReasonSpatialPeerOutlier},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t, func(c *Config) { c.ZScale, c.MADFloorMM = 1, 1 })
			seed(v, "A", "p", t0, 5, 0, 0, 0, 0)
			got := v.Evaluate(rd("me", "A", t0, tc.mm, 5))
			if got.Reason != tc.reason {
				t.Errorf("Z=%v: got %s, want %s", got.ZScore, got.Reason, tc.reason)
			}
		})
	}
}

func TestMedianSorted(t *testing.T) {
	tests := []struct {
		in   []float64
		want float64
	}{
		{[]float64{5}, 5},
		{[]float64{1, 3}, 2},
		{[]float64{1, 2, 9}, 2},
		{[]float64{1, 2, 4, 9}, 3},
		{[]float64{math.MaxFloat64, math.MaxFloat64}, math.MaxFloat64}, // no overflow
	}
	for i, tc := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := medianSorted(tc.in); got != tc.want {
				t.Errorf("medianSorted(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestWindow(t *testing.T) {
	t.Run("bounded: oldest observation is evicted", func(t *testing.T) {
		v := newVerifier(t, func(c *Config) { c.WindowMaxDevices = 5 })
		for i := 0; i < 8; i++ {
			v.Evaluate(rd(fmt.Sprintf("d%d", i), "A", t0.Add(time.Duration(i)*time.Minute), 4, 5))
		}
		if n := windowLen(v, "A"); n != 5 {
			t.Fatalf("window = %d, want 5", n)
		}
		for i := 0; i < 8; i++ {
			_, ok := windowObs(v, "A", fmt.Sprintf("d%d", i))
			if want := i >= 3; ok != want {
				t.Errorf("d%d present = %v, want %v", i, ok, want)
			}
		}
	})
	t.Run("a reading older than everything in a full window is not recorded", func(t *testing.T) {
		v := newVerifier(t, func(c *Config) { c.WindowMaxDevices = 5 })
		for i := 1; i <= 5; i++ {
			v.Evaluate(rd(fmt.Sprintf("d%d", i), "A", t0.Add(time.Duration(i)*time.Minute), 4, 5))
		}
		v.Evaluate(rd("old", "A", t0, 4, 5))
		if _, ok := windowObs(v, "A", "old"); ok || windowLen(v, "A") != 5 {
			t.Errorf("old reading displaced a newer observation (window=%d)", windowLen(v, "A"))
		}
	})
	t.Run("same device updates in place", func(t *testing.T) {
		v := newVerifier(t, func(c *Config) { c.WindowMaxDevices = 5 })
		for i := 0; i < 20; i++ {
			v.Evaluate(rd("d", "A", t0.Add(time.Duration(i)*time.Second), 4, 5))
		}
		if n := windowLen(v, "A"); n != 1 {
			t.Errorf("window = %d, want 1", n)
		}
	})
	t.Run("an older reading does not replace a newer observation of the same device", func(t *testing.T) {
		v := newVerifier(t)
		v.Evaluate(rd("d", "A", t0.Add(5*time.Minute), 6, 10))
		v.Evaluate(rd("d", "A", t0, 1, 10))
		o, ok := windowObs(v, "A", "d")
		if !ok || o.depthMM != 6 || !o.ts.Equal(t0.Add(5*time.Minute)) {
			t.Errorf("observation = %+v, want depth 6 at t0+5m", o)
		}
	})
}

func TestConfig(t *testing.T) {
	t.Run("defaults match the specification", func(t *testing.T) {
		c := DefaultConfig()
		want := Config{
			Alpha: 1.15, BetaMM: 2.0,
			MinPeers: 4, PeerMaxAge: 10 * time.Minute, WindowMaxDevices: 128,
			ZScale: 0.6745, MADFloorMM: 0.15, OutlierZ: 3.5,
			PeerRainMM: 2.0, CoherenceAbsMM: 1.0, CoherenceRelFrac: 0.25,
			RadarDryMM: 0.5, EscrowTTL: 15 * time.Minute, NeighborMinCells: 2, NeighborMinDevices: 3,
		}
		if c != want {
			t.Errorf("DefaultConfig() = %+v, want %+v", c, want)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("defaults do not validate: %v", err)
		}
	})
	t.Run("zero config is rejected", func(t *testing.T) {
		if _, err := New(Config{}); err == nil {
			t.Error("New(Config{}) succeeded")
		}
	})
	invalid := []struct {
		name   string
		mutate func(*Config)
	}{
		{"negative alpha", func(c *Config) { c.Alpha = -1 }},
		{"NaN beta", func(c *Config) { c.BetaMM = math.NaN() }},
		{"zero min peers", func(c *Config) { c.MinPeers = 0 }},
		{"zero peer max age", func(c *Config) { c.PeerMaxAge = 0 }},
		{"window smaller than min peers + 1", func(c *Config) { c.WindowMaxDevices = 4 }},
		{"zero z scale", func(c *Config) { c.ZScale = 0 }},
		{"zero MAD floor", func(c *Config) { c.MADFloorMM = 0 }},
		{"zero outlier z", func(c *Config) { c.OutlierZ = 0 }},
		{"negative rain threshold", func(c *Config) { c.PeerRainMM = -1 }},
		{"negative coherence abs", func(c *Config) { c.CoherenceAbsMM = -1 }},
		{"Inf coherence frac", func(c *Config) { c.CoherenceRelFrac = math.Inf(1) }},
		{"negative radar dry", func(c *Config) { c.RadarDryMM = -0.1 }},
		{"zero escrow ttl", func(c *Config) { c.EscrowTTL = 0 }},
		{"zero neighbor min cells", func(c *Config) { c.NeighborMinCells = 0 }},
		{"zero neighbor min devices", func(c *Config) { c.NeighborMinDevices = 0 }},
	}
	for _, tc := range invalid {
		t.Run("invalid: "+tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Error("New succeeded, want error")
			}
		})
	}
	t.Run("config values are honoured", func(t *testing.T) {
		// Raising MinPeers to 5 turns four peers into "too few".
		v := newVerifier(t, func(c *Config) { c.MinPeers = 5 })
		seed(v, "A", "p", t0, 5, 4, 4, 4, 4)
		got := v.Evaluate(rd("me", "A", t0, 4, 5))
		if got.Reason != ReasonRadarPhysicsFallback {
			t.Errorf("got %s, want fallback", got.Reason)
		}
		if v.Config().MinPeers != 5 {
			t.Errorf("Config().MinPeers = %d", v.Config().MinPeers)
		}
	})
}
