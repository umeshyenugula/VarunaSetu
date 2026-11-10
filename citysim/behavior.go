package citysim

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"

	"hydroledger-prototype/service"
)

// Kind is what a device is doing. Anything but HONEST/OFFLINE is a
// manipulated report and is labelled as fraud/bad data in the ground truth.
type Kind string

const (
	Honest            Kind = "HONEST"
	Inflate           Kind = "INFLATE"             // claims Factor x the rain it really caught; radar value is honest
	InflateSpoofRadar Kind = "INFLATE+RADAR_SPOOF" // inflates and also reports a radar value that makes the claim fit
	Collude           Kind = "COLLUDE_FAKE_STORM"  // reports a fabricated, mutually consistent depth with no rain
	Offline           Kind = "OFFLINE"
)

// Rule applies a Kind to a set of devices during a time range.
type Rule struct {
	Devices map[string]bool
	From    time.Duration
	To      time.Duration
	Kind    Kind
	Factor  float64 // Inflate: multiplier on the true depth
	FakeMM  float64 // Collude: fabricated depth per report
}

// Set builds a device-id set.
func Set(ids ...string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// Window is the reporting interval. Devices report the depth harvested in the
// window that just ended, shortly after the window boundary.
const Window = 5 * time.Minute

// MinReportMM is the smallest depth a device bothers to report: rainwater
// harvesting telemetry is event driven, a device with nothing harvested stays
// quiet.
const MinReportMM = 0.2

// hashGauss is a deterministic standard normal draw for (key, k), so results
// do not depend on call order.
func hashGauss(key string, k int) float64 {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", key, k)))
	u1 := (float64(binary.BigEndian.Uint64(h[0:8])>>11) + 0.5) / (1 << 53)
	u2 := (float64(binary.BigEndian.Uint64(h[8:16])>>11) + 0.5) / (1 << 53)
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

// Observation is one device report the simulator is about to send.
type Observation struct {
	Report  bool
	Tel     service.Telemetry
	Kind    Kind
	Fraud   bool
	TruthMM float64 // rain that really fell on the roof
	ClaimMM float64 // effective depth implied by the claim (R_obs)
	RadarMM float64
}

func (r *Runner) ruleFor(d *Device, t time.Duration) (Rule, bool) {
	for _, ru := range r.Scn.Rules {
		if ru.Devices[d.ID] && t >= ru.From && t < ru.To {
			return ru, true
		}
	}
	return Rule{}, false
}

// observe builds the report device d sends for the window ending at boundary.
func (r *Runner) observe(d *Device, boundary time.Duration) Observation {
	wx, b := r.Scn.Weather, d.Building
	k := int(boundary / Window)
	cell := r.World.Cells[b.CellID]
	truth := wx.Depth(cell.X, cell.Y, boundary, Window) // rain is uniform inside an H3 cell
	radar := wx.Radar(cell, boundary, Window)
	sensor := truth * (1 + 0.03*hashGauss(d.ID, k))

	obs := Observation{Kind: Honest, TruthMM: truth, RadarMM: radar}
	claim := sensor
	rule, has := r.ruleFor(d, boundary)
	if has {
		obs.Kind = rule.Kind
	}
	switch obs.Kind {
	case Offline:
		return obs
	case Inflate:
		claim = sensor * rule.Factor
		obs.Fraud = true
	case InflateSpoofRadar:
		claim = sensor * rule.Factor
		radar = claim / 1.15 // report a radar value that makes the bound pass
		obs.Fraud = true
	case Collude:
		claim = rule.FakeMM * (1 + 0.02*hashGauss(d.ID, k))
		obs.Fraud = true
	}
	if obs.Kind != Collude && claim < MinReportMM {
		return obs // nothing harvested: honest devices stay quiet
	}
	litres := math.Round(claim*b.RoofM2*b.Runoff*100) / 100
	obs.Report = true
	obs.ClaimMM = claim
	obs.RadarMM = radar
	obs.Tel = service.Telemetry{
		BuildingID:     b.ID,
		ClaimedLitres:  litres,
		RoofAreaM2:     b.RoofM2,
		RunoffCoeff:    b.Runoff,
		StoredLitres:   math.Round(b.storedL*100) / 100,
		CapacityLitres: b.TankL,
		RadarMM:        math.Round(radar*10000) / 10000,
		CellID:         b.CellID,
	}
	if !obs.Fraud {
		b.storedL += litres
	}
	return obs
}

// ---- reference baselines ---------------------------------------------------
//
// These are simple validators implemented here for comparison; they are
// stand-ins for the approaches the project's own ablation study calls Static,
// Physics-only and Peer-only (sim/ablation_experiment.py). They are not
// products of any vendor. Each sees the same readings as VarunaSetu.

// StaticThresholdMM is the fixed per-reading cap used by the Static baseline
// (sim/config.py: STATIC_THRESHOLD_MM).
const StaticThresholdMM = 10.0

type baselineObs struct {
	ts    time.Time
	depth float64
}

// Baselines holds the per-cell peer memory the peer-only baseline needs.
type Baselines struct {
	peers map[string]map[string]baselineObs // cell -> device -> latest
}

func NewBaselines() *Baselines { return &Baselines{peers: map[string]map[string]baselineObs{}} }

// Names of the baseline validators, in display order.
var BaselineNames = []string{"STATIC_THRESHOLD", "RADAR_ONLY", "PEER_ONLY"}

// Judge returns whether each baseline accepts the reading and then records it.
func (bl *Baselines) Judge(device, cell string, ts time.Time, rObs, radar float64) map[string]bool {
	out := map[string]bool{
		"STATIC_THRESHOLD": rObs <= StaticThresholdMM,
		"RADAR_ONLY":       rObs <= 1.15*radar+2.0,
	}
	// Peer-only: robust z-score against same-cell peers, if there are enough.
	cp := bl.peers[cell]
	var vals []float64
	for dev, o := range cp {
		if dev != device && ts.Sub(o.ts) <= 10*time.Minute {
			vals = append(vals, o.depth)
		}
	}
	accept := true
	if len(vals) >= 4 {
		slices.Sort(vals)
		m := median(vals)
		dev := make([]float64, len(vals))
		for i, v := range vals {
			dev[i] = math.Abs(v - m)
		}
		slices.Sort(dev)
		z := 0.6745 * math.Abs(rObs-m) / (median(dev) + 0.15)
		if z > 3.5 && rObs > m {
			accept = false
		}
	}
	out["PEER_ONLY"] = accept
	if cp == nil {
		cp = map[string]baselineObs{}
		bl.peers[cell] = cp
	}
	cp[device] = baselineObs{ts: ts, depth: rObs}
	return out
}

func median(s []float64) float64 {
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return s[n/2-1]/2 + s[n/2]/2
}
