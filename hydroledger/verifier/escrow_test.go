package verifier

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEscrow_Resolution(t *testing.T) {
	const ttl = 15 * time.Minute
	// later submits a reading whose only purpose is to carry a radar value. Its
	// 30 mm claim is a positive outlier against the 8 mm peers, so it is
	// quarantined rather than creating a second escrow when the radar is dry.
	later := func(cell string, at time.Duration, radar float64) func(*Verifier) {
		return func(v *Verifier) { v.Evaluate(rd("later", cell, t0.Add(at), 30, radar)) }
	}
	tests := []struct {
		name       string
		act        func(*Verifier) // runs after the escrow exists, before the sweep
		sweepAt    time.Time
		wantState  State // empty: nothing may be resolved and the escrow stays pending
		wantReason Reason
	}{
		// --- expiry ---
		{name: "pending shortly before the TTL", sweepAt: t0.Add(ttl - time.Second)},
		{name: "still pending at exactly the TTL", sweepAt: t0.Add(ttl)},
		{name: "expires uncorroborated just after the TTL", sweepAt: t0.Add(ttl + 1),
			wantState: StateQuarantine, wantReason: ReasonEscrowExpired},
		{name: "expires uncorroborated long after the TTL", sweepAt: t0.Add(24 * time.Hour),
			wantState: StateQuarantine, wantReason: ReasonEscrowExpired},

		// --- later radar reading above 0.5 mm ---
		{name: "later radar 3 mm confirms", act: later("A", 5*time.Minute, 3.0), sweepAt: t0.Add(5 * time.Minute),
			wantState: StateAccept, wantReason: ReasonEscrowCorroborated},
		{name: "later radar just above 0.5 mm confirms", act: later("A", 5*time.Minute, 0.5000001), sweepAt: t0.Add(5 * time.Minute),
			wantState: StateAccept, wantReason: ReasonEscrowCorroborated},
		{name: "later radar exactly 0.5 mm does not confirm", act: later("A", 5*time.Minute, 0.5), sweepAt: t0.Add(5 * time.Minute)},
		{name: "later radar 0 does not confirm", act: later("A", 5*time.Minute, 0), sweepAt: t0.Add(5 * time.Minute)},
		{name: "radar reading with the same timestamp is not later", act: later("A", 0, 3.0), sweepAt: t0.Add(time.Minute)},
		{name: "radar reading before the escrowed one is not later", act: later("A", -time.Minute, 3.0), sweepAt: t0.Add(time.Minute)},
		{name: "radar reading in another cell does not confirm", act: later("B", 5*time.Minute, 3.0), sweepAt: t0.Add(5 * time.Minute)},
		{name: "radar reading exactly at the TTL still confirms", act: later("A", ttl, 3.0), sweepAt: t0.Add(ttl),
			wantState: StateAccept, wantReason: ReasonEscrowCorroborated},
		{name: "radar reading just after the TTL is too late", act: later("A", ttl+1, 3.0), sweepAt: t0.Add(ttl + 1),
			wantState: StateQuarantine, wantReason: ReasonEscrowExpired},
		{name: "confirmation seen in time survives a late sweep", act: later("A", 5*time.Minute, 3.0), sweepAt: t0.Add(time.Hour),
			wantState: StateAccept, wantReason: ReasonEscrowCorroborated},
		{name: "invalid later reading cannot confirm",
			act: func(v *Verifier) {
				r := rd("later", "A", t0.Add(5*time.Minute), 30, 3.0)
				r.ClaimedLitres = math.NaN()
				v.Evaluate(r)
			},
			sweepAt: t0.Add(5 * time.Minute)},
		{name: "over-capacity later reading cannot confirm",
			act: func(v *Verifier) {
				r := rd("later", "A", t0.Add(5*time.Minute), 30, 3.0)
				r.StoredLitres = 2 * testCapacity
				v.Evaluate(r)
			},
			sweepAt: t0.Add(5 * time.Minute)},
		{name: "a sweep earlier than the escrowed reading leaves it alone", sweepAt: t0.Add(-time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := escrowScenario(t)
			if tc.act != nil {
				tc.act(v)
			}
			got := v.ResolveEscrows(tc.sweepAt)

			if tc.wantState == "" {
				if len(got) != 0 {
					t.Fatalf("resolved %+v, want nothing", got)
				}
				if n := v.PendingEscrows(); n != 1 {
					t.Errorf("PendingEscrows = %d, want 1", n)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("resolved %d escrows, want 1: %+v", len(got), got)
			}
			r := got[0]
			if r.State != tc.wantState || r.Reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s", r.State, r.Reason, tc.wantState, tc.wantReason)
			}
			if r.DeviceID != "claimant" || r.CellID != "A" || !r.ReadingTimestamp.Equal(t0) || !r.ResolvedAt.Equal(tc.sweepAt) {
				t.Errorf("resolution identity wrong: %+v", r)
			}
			if n := v.PendingEscrows(); n != 0 {
				t.Errorf("PendingEscrows = %d, want 0", n)
			}
			if again := v.ResolveEscrows(tc.sweepAt); len(again) != 0 {
				t.Errorf("second sweep returned %+v; each escrow must be returned once", again)
			}
		})
	}
}

func TestEscrow_RadarOnlyConfirmsOlderEscrows(t *testing.T) {
	v := escrowScenario(t) // escrow #1 at t0
	// A radar reading at t0+5m (quarantined itself, but its radar value counts).
	v.Evaluate(rd("later", "A", t0.Add(5*time.Minute), 8, 3.0))
	// Escrow #2 is created after that radar reading, so the reading is not "later" than it.
	if got := v.Evaluate(rd("claimant2", "A", t0.Add(6*time.Minute), 8, 0)); got.State != StateProvisionalEscrow {
		t.Fatalf("setup: second claim got %s/%s, want escrow", got.State, got.Reason)
	}
	got := v.ResolveEscrows(t0.Add(6 * time.Minute))
	if len(got) != 1 || got[0].DeviceID != "claimant" || got[0].Reason != ReasonEscrowCorroborated {
		t.Fatalf("resolved %+v, want only claimant corroborated", got)
	}
	if n := v.PendingEscrows(); n != 1 {
		t.Errorf("PendingEscrows = %d, want 1 (claimant2)", n)
	}
}

func TestEscrow_NeighborCorroboration(t *testing.T) {
	type nb struct {
		id     string
		depths []float64
		age    time.Duration // observation time is t0-age; the sweep is at t0
	}
	tests := []struct {
		name       string
		mutate     func(*Config)
		configured []string // SetNeighbors("A", ...); nil means never called
		cells      []nb
		want       bool // corroborated?
	}{
		{name: "two neighbors, 3 devices each, raining",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8, 8}, 0}}, want: true},
		{name: "medians exactly at 2.0 mm count",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{1.5, 2, 2.5}, 0}, {"C", []float64{2, 2, 2}, 0}}, want: true},
		{name: "one neighbor median just below 2.0 mm",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{1.75, 1.75, 1.75}, 0}}},
		{name: "only one neighbor configured",
			configured: []string{"B"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}}},
		{name: "one neighbor has only 2 devices",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8}, 0}}},
		{name: "exactly 3 devices is enough",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8, 8}, 0}}, want: true},
		{name: "three configured, two qualify",
			configured: []string{"B", "C", "D"},
			cells:      []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{0, 0, 0}, 0}, {"D", []float64{8, 8, 8}, 0}}, want: true},
		{name: "configured neighbor without any data",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}}},
		{name: "neighbor observations exactly 10 minutes old are recent",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 10 * time.Minute}, {"C", []float64{8, 8, 8}, 10 * time.Minute}}, want: true},
		{name: "neighbor observations 10 minutes and 1ns old are not recent",
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 10*time.Minute + 1}, {"C", []float64{8, 8, 8}, 10 * time.Minute}}},
		{name: "duplicate neighbor ids count once",
			configured: []string{"B", "B"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}}},
		{name: "the escrow's own cell listed as neighbor does not count",
			configured: []string{"A", "B"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}}},
		{name: "neighbors never configured",
			cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8, 8}, 0}}},
		{name: "neighbor relation is directed",
			cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8, 8}, 0}}}, // B and C list A; A lists no one
		{name: "NeighborMinCells = 1 is honoured",
			mutate:     func(c *Config) { c.NeighborMinCells = 1 },
			configured: []string{"B"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}}, want: true},
		{name: "NeighborMinDevices = 4 is honoured",
			mutate:     func(c *Config) { c.NeighborMinDevices = 4 },
			configured: []string{"B", "C"}, cells: []nb{{"B", []float64{8, 8, 8}, 0}, {"C", []float64{8, 8, 8}, 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mut []func(*Config)
			if tc.mutate != nil {
				mut = append(mut, tc.mutate)
			}
			v := escrowScenario(t, mut...)
			for _, c := range tc.cells {
				seed(v, c.id, c.id, t0.Add(-c.age), 0, c.depths...)
			}
			if tc.configured != nil {
				v.SetNeighbors("A", tc.configured)
			}
			if tc.name == "neighbor relation is directed" {
				v.SetNeighbors("B", []string{"A", "C"})
				v.SetNeighbors("C", []string{"A", "B"})
			}
			got := v.ResolveEscrows(t0)
			if !tc.want {
				if len(got) != 0 || v.PendingEscrows() != 1 {
					t.Fatalf("resolved %+v (pending %d), want escrow to stay pending", got, v.PendingEscrows())
				}
				return
			}
			if len(got) != 1 || got[0].State != StateAccept || got[0].Reason != ReasonEscrowCorroborated {
				t.Fatalf("resolved %+v, want one ACCEPT/ESCROW_CORROBORATED", got)
			}
		})
	}
}

func TestEscrow_NeighborEvidenceMustBeRecentAtSweepTime(t *testing.T) {
	v := escrowScenario(t)
	seed(v, "B", "B", t0, 0, 8, 8, 8)
	seed(v, "C", "C", t0, 0, 8, 8, 8)
	v.SetNeighbors("A", []string{"B", "C"})
	// 11 minutes later the neighbors' observations have aged out, and the
	// escrow (age 11m < TTL) is still pending.
	if got := v.ResolveEscrows(t0.Add(11 * time.Minute)); len(got) != 0 {
		t.Fatalf("resolved %+v, want nothing", got)
	}
	// Fresh neighbor observations then corroborate it before the TTL ends.
	seed(v, "B", "B2", t0.Add(12*time.Minute), 0, 8, 8, 8)
	seed(v, "C", "C2", t0.Add(12*time.Minute), 0, 8, 8, 8)
	got := v.ResolveEscrows(t0.Add(13 * time.Minute))
	if len(got) != 1 || got[0].Reason != ReasonEscrowCorroborated {
		t.Fatalf("resolved %+v, want corroborated", got)
	}
}

func TestEscrow_SingleCellMajorityStaysProvisional(t *testing.T) {
	// Six devices in cell A all report heavy rain under a dry radar. Devices 0-3
	// have too few peers; devices 4 and 5 are escrowed. A majority inside one
	// cell is not independent evidence, so nothing resolves until the TTL.
	tests := []struct {
		name  string
		setup func(v *Verifier, at time.Time) // runs with neighbor observations stamped at `at`
	}{
		{"neighbors never configured", func(v *Verifier, at time.Time) {}},
		{"neighbors configured but silent", func(v *Verifier, at time.Time) {
			v.SetNeighbors("A", []string{"B", "C"})
		}},
		{"neighbors configured and dry", func(v *Verifier, at time.Time) {
			v.SetNeighbors("A", []string{"B", "C"})
			seed(v, "B", "B", at, 0, 0, 0, 0)
			seed(v, "C", "C", at, 0, 0, 0, 0)
		}},
		{"only one neighbor is raining", func(v *Verifier, at time.Time) {
			v.SetNeighbors("A", []string{"B", "C"})
			seed(v, "B", "B", at, 0, 8, 8, 8)
			seed(v, "C", "C", at, 0, 0, 0, 0)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			for i := 0; i < 6; i++ {
				got := v.Evaluate(rd(fmt.Sprintf("a%d", i), "A", t0, 8, 0))
				wantState, wantReason := StateQuarantine, ReasonInsufficientPeersReview
				if i >= 4 {
					wantState, wantReason = StateProvisionalEscrow, ReasonRadarPeerConflictEscrow
				}
				if got.State != wantState || got.Reason != wantReason {
					t.Fatalf("device a%d: got %s/%s, want %s/%s", i, got.State, got.Reason, wantState, wantReason)
				}
			}
			late := t0.Add(14 * time.Minute)
			tc.setup(v, late)

			if got := v.ResolveEscrows(late.Add(59 * time.Second)); len(got) != 0 {
				t.Fatalf("resolved %+v before the TTL, want nothing", got)
			}
			if n := v.PendingEscrows(); n != 2 {
				t.Fatalf("PendingEscrows = %d, want 2", n)
			}
			expired := v.ResolveEscrows(t0.Add(15*time.Minute + 1))
			if len(expired) != 2 {
				t.Fatalf("expired %d escrows, want 2: %+v", len(expired), expired)
			}
			for _, r := range expired {
				if r.State != StateQuarantine || r.Reason != ReasonEscrowExpired {
					t.Errorf("got %s/%s, want QUARANTINE/ESCROW_EXPIRED_UNCORROBORATED", r.State, r.Reason)
				}
			}
		})
	}
}

// TestKnown_CrossCellCollusionCorroborates documents a KNOWN LIMITATION; it
// asserts the behaviour we do NOT want in a hardened system.
//
// Neighbor-cell corroboration counts any recent observation, whatever verdict
// it received and however it was produced. Devices in two neighboring cells
// that coordinate their reports (here: 3 devices per cell claiming 8 mm while
// the radar is dry in every cell) therefore satisfy the "at least two neighbor
// cells, 3+ devices, median >= 2.0 mm" rule, and a fabricated escrow is
// ACCEPTED as ESCROW_CORROBORATED with no radar evidence at all.
//
// The verifier has no device identity, trust weighting or cross-cell
// consistency check, so this is not mitigated; see HANDOFF.md (known
// limitations). If this test starts failing because the escrow is no longer
// corroborated, the limitation has been addressed: update this test and the
// docs rather than deleting the documentation of the risk.
func TestKnown_CrossCellCollusionCorroborates(t *testing.T) {
	tests := []struct {
		name          string
		colludingIn   []string // neighbor cells whose devices fabricate 8 mm under dry radar
		wantCorrobor  bool
		wantStillHeld bool
	}{
		{name: "two coordinated neighbor cells corroborate a fabricated escrow (KNOWN LIMITATION)",
			colludingIn: []string{"B", "C"}, wantCorrobor: true},
		{name: "control: coordination in only one neighbor cell is not enough",
			colludingIn: []string{"B"}, wantStillHeld: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := escrowScenario(t) // radar is dry (0 mm) in cell A
			for _, id := range tc.colludingIn {
				seed(v, id, "sybil-"+id, t0, 0 /* radar dry here too */, 8, 8, 8)
			}
			v.SetNeighbors("A", []string{"B", "C"})

			got := v.ResolveEscrows(t0)
			switch {
			case tc.wantCorrobor:
				if len(got) != 1 || got[0].State != StateAccept || got[0].Reason != ReasonEscrowCorroborated {
					t.Fatalf("resolved %+v; the known limitation (collusion corroborates) no longer reproduces", got)
				}
			case tc.wantStillHeld:
				if len(got) != 0 || v.PendingEscrows() != 1 {
					t.Fatalf("resolved %+v (pending %d), want escrow held", got, v.PendingEscrows())
				}
			}
		})
	}
}

func TestResolveEscrows_OrderIsDeterministic(t *testing.T) {
	v := newVerifier(t)
	for _, cell := range []string{"Z", "M", "A"} {
		seed(v, cell, "p", t0, 0, 8, 8.25, 7.75, 8)
		for _, dev := range []string{"y", "x"} {
			if got := v.Evaluate(rd(dev, cell, t0, 8, 0)); got.State != StateProvisionalEscrow {
				t.Fatalf("setup %s/%s: %s/%s", cell, dev, got.State, got.Reason)
			}
		}
	}
	got := v.ResolveEscrows(t0.Add(time.Hour))
	var order []string
	for _, r := range got {
		order = append(order, r.CellID+"/"+r.DeviceID)
	}
	want := []string{"A/x", "A/y", "M/x", "M/y", "Z/x", "Z/y"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestSetNeighbors_Normalisation(t *testing.T) {
	v := newVerifier(t)
	v.SetNeighbors("A", []string{"B", "", "A", "B", "C"})
	v.nbMu.RLock()
	got := fmt.Sprint(v.neighbors["A"])
	v.nbMu.RUnlock()
	if got != "[B C]" {
		t.Errorf("neighbors = %s, want [B C]", got)
	}
	v.SetNeighbors("A", nil)
	v.nbMu.RLock()
	_, ok := v.neighbors["A"]
	v.nbMu.RUnlock()
	if ok {
		t.Error("empty list should clear the entry")
	}
	v.SetNeighbors("", []string{"B"}) // ignored, must not panic
}

// TestConcurrentUse is meant to run under -race. It also checks a bookkeeping
// invariant: every escrow Evaluate creates is eventually returned by
// ResolveEscrows exactly once.
func TestConcurrentUse(t *testing.T) {
	v := newVerifier(t)
	const writers, perWriter, cells = 8, 300, 4

	var created, resolved atomic.Int64
	stop := make(chan struct{})
	var sweepers sync.WaitGroup
	for s := 0; s < 2; s++ {
		sweepers.Add(1)
		go func() {
			defer sweepers.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				resolved.Add(int64(len(v.ResolveEscrows(t0.Add(time.Duration(i%30) * time.Minute)))))
				v.PendingEscrows()
				v.SetNeighbors("c0", []string{"c1", "c2"})
				runtime.Gosched()
			}
		}()
	}

	var writersWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(w int) {
			defer writersWG.Done()
			cell := fmt.Sprintf("c%d", w%cells)
			for i := 0; i < perWriter; i++ {
				ts := t0.Add(time.Duration(i%60) * time.Second)
				if v.Evaluate(rd(fmt.Sprintf("w%d-d%d", w, i%16), cell, ts, 8, 0)).State == StateProvisionalEscrow {
					created.Add(1)
				}
			}
		}(w)
	}
	writersWG.Wait()
	close(stop)
	sweepers.Wait()

	resolved.Add(int64(len(v.ResolveEscrows(t0.Add(24 * time.Hour)))))
	if n := v.PendingEscrows(); n != 0 {
		t.Errorf("PendingEscrows = %d after a final sweep, want 0", n)
	}
	if created.Load() == 0 {
		t.Fatal("test did not exercise escrow creation")
	}
	if created.Load() != resolved.Load() {
		t.Errorf("escrows created = %d, resolutions returned = %d; want equal", created.Load(), resolved.Load())
	}
}
