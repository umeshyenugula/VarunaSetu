// Package verifier classifies rainwater-harvest readings as ACCEPT,
// PROVISIONAL_ESCROW or QUARANTINE by comparing each claim with a radar-derived
// physical bound and with recent readings from other devices in the same cell.
//
// This is a prototype rebuilt from a written specification. It has not been
// reviewed for security and offers no protection against Sybil attacks; see
// HANDOFF.md for assumptions and known limitations.
//
// A Verifier is safe for concurrent use. All time arithmetic uses
// Reading.Timestamp (and the "now" passed to ResolveEscrows); the package never
// reads the wall clock.
package verifier

import (
	"math"
	"strings"
	"sync"
	"time"
)

// observation is the latest depth reported by one device in a cell.
type observation struct {
	device  string
	ts      time.Time
	depthMM float64
}

// escrow is a pending PROVISIONAL_ESCROW reading.
type escrow struct {
	device  string
	ts      time.Time // timestamp of the escrowed reading; TTL starts here
	radarOK bool      // a later radar reading > RadarDryMM arrived within the TTL
}

// cell is the per-cell state. All fields except id are guarded by mu.
type cell struct {
	id      string
	mu      sync.Mutex
	obs     []observation // at most Config.WindowMaxDevices, one per device
	escrows []escrow
}

// Verifier evaluates readings. Create it with New.
type Verifier struct {
	cfg Config

	cellsMu sync.RWMutex
	cells   map[string]*cell

	nbMu      sync.RWMutex
	neighbors map[string][]string // values are replaced, never mutated

	sweepMu sync.Mutex // serialises ResolveEscrows

	scratch sync.Pool
}

// New returns a Verifier using cfg, which must pass cfg.Validate.
func New(cfg Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	v := &Verifier{
		cfg:       cfg,
		cells:     make(map[string]*cell),
		neighbors: make(map[string][]string),
	}
	v.scratch.New = func() any { return new(scratch) }
	return v, nil
}

// Config returns a copy of the configuration in use.
func (v *Verifier) Config() Config { return v.cfg }

func (v *Verifier) lookupCell(id string) *cell {
	v.cellsMu.RLock()
	c := v.cells[id]
	v.cellsMu.RUnlock()
	return c
}

func (v *Verifier) cellFor(id string) *cell {
	if c := v.lookupCell(id); c != nil {
		return c
	}
	v.cellsMu.Lock()
	defer v.cellsMu.Unlock()
	c, ok := v.cells[id]
	if !ok {
		c = &cell{id: strings.Clone(id)}
		v.cells[c.id] = c
	}
	return c
}

func quarantine(reason Reason) Decision {
	return Decision{State: StateQuarantine, Reason: reason}
}

// validReading checks the fields that do not depend on the configuration.
func validReading(r *Reading) bool {
	switch {
	case r.DeviceID == "", r.CellID == "":
		return false
	case r.Timestamp.IsZero():
		return false
	case !finite(r.ClaimedLitres) || r.ClaimedLitres < 0:
		return false
	case !finite(r.RoofAreaM2) || r.RoofAreaM2 <= 0:
		return false
	case !(r.RunoffCoeff > 0 && r.RunoffCoeff <= 1): // also rejects NaN
		return false
	case !finite(r.StoredLitres) || r.StoredLitres < 0:
		return false
	case !finite(r.CapacityLitres) || r.CapacityLitres < 0:
		return false
	case !finite(r.RadarMM) || r.RadarMM < 0:
		return false
	}
	return true
}

// Evaluate classifies r.
//
// Order of checks: field validation (INVALID_INPUT), capacity
// (STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED), then the peer/radar rules. A reading
// that passes validation and the capacity check is recorded as its device's
// latest observation in its cell whatever the verdict, and a reading with
// RadarMM above Config.RadarDryMM corroborates pending escrows in the same cell
// that are older than it. Escrow resolutions are not returned here; see
// ResolveEscrows.
func (v *Verifier) Evaluate(r Reading) Decision {
	if !validReading(&r) {
		return quarantine(ReasonInvalidInput)
	}
	cfg := &v.cfg
	rObs := r.ClaimedLitres / (r.RoofAreaM2 * r.RunoffCoeff)
	rMax := cfg.Alpha*r.RadarMM + cfg.BetaMM
	if !finite(rObs) || !finite(rMax) {
		return quarantine(ReasonInvalidInput)
	}
	d := Decision{RObsMM: rObs, RMaxMM: rMax}

	if r.StoredLitres > r.CapacityLitres || r.ClaimedLitres > r.CapacityLitres-r.StoredLitres {
		d.State, d.Reason = StateQuarantine, ReasonCapacityExceeded
		return d
	}

	r.Timestamp = r.Timestamp.Round(0) // strip any monotonic reading

	c := v.cellFor(r.CellID)
	sc := v.scratch.Get().(*scratch)
	c.mu.Lock()
	d = v.classify(c, &r, d, sc)
	c.record(&r, rObs, cfg.WindowMaxDevices)
	if r.RadarMM > cfg.RadarDryMM && len(c.escrows) > 0 {
		c.corroborateByRadar(r.Timestamp, cfg.EscrowTTL)
	}
	if d.State == StateProvisionalEscrow {
		c.escrows = append(c.escrows, escrow{device: strings.Clone(r.DeviceID), ts: r.Timestamp})
	}
	c.mu.Unlock()
	v.scratch.Put(sc)
	return d
}

// classify applies the peer/radar rules. c.mu must be held.
func (v *Verifier) classify(c *cell, r *Reading, d Decision, sc *scratch) Decision {
	cfg := &v.cfg

	peers := sc.a[:0]
	for i := range c.obs {
		o := &c.obs[i]
		if o.device == r.DeviceID || r.Timestamp.Sub(o.ts) > cfg.PeerMaxAge {
			continue
		}
		peers = append(peers, o.depthMM)
	}
	sc.a = peers
	d.PeerCount = len(peers)

	if len(peers) < cfg.MinPeers {
		if d.RObsMM <= d.RMaxMM {
			d.State, d.Reason = StateAccept, ReasonRadarPhysicsFallback
		} else {
			d.State, d.Reason = StateQuarantine, ReasonInsufficientPeersReview
		}
		return d
	}

	st, dev := computeStats(peers, sc.b)
	sc.b = dev
	d.PeerMedianMM, d.PeerMADMM = st.median, st.mad
	d.ZScore = cfg.ZScale * math.Abs(d.RObsMM-st.median) / (st.mad + cfg.MADFloorMM)

	positiveOutlier := d.ZScore > cfg.OutlierZ && d.RObsMM > st.median
	radarDry := r.RadarMM <= cfg.RadarDryMM
	peersRain := st.median >= cfg.PeerRainMM
	peersCoherent := st.spread <= math.Max(cfg.CoherenceAbsMM, cfg.CoherenceRelFrac*st.median)

	switch {
	case positiveOutlier:
		d.State, d.Reason = StateQuarantine, ReasonSpatialPeerOutlier
	case radarDry && peersRain && peersCoherent:
		d.State, d.Reason = StateProvisionalEscrow, ReasonRadarPeerConflictEscrow
	case d.RObsMM > d.RMaxMM:
		d.State, d.Reason = StateQuarantine, ReasonPhysicalBoundExceeded
	default:
		d.State, d.Reason = StateAccept, ReasonRadarAndPeersAgree
	}
	return d
}

// record stores depth as r.DeviceID's latest observation. An older reading
// never replaces a newer observation of the same device; when the window is
// full the oldest observation is evicted, unless the new one is older still.
// c.mu must be held.
func (c *cell) record(r *Reading, depth float64, maxDevices int) {
	for i := range c.obs {
		if c.obs[i].device == r.DeviceID {
			if !r.Timestamp.Before(c.obs[i].ts) {
				c.obs[i].ts, c.obs[i].depthMM = r.Timestamp, depth
			}
			return
		}
	}
	o := observation{device: strings.Clone(r.DeviceID), ts: r.Timestamp, depthMM: depth}
	if len(c.obs) < maxDevices {
		c.obs = append(c.obs, o)
		return
	}
	oldest := 0
	for i := 1; i < len(c.obs); i++ {
		if c.obs[i].ts.Before(c.obs[oldest].ts) {
			oldest = i
		}
	}
	if r.Timestamp.Before(c.obs[oldest].ts) {
		return
	}
	c.obs[oldest] = o
}
