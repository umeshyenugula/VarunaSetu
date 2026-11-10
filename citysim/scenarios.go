package citysim

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Check inspects a finished run and says whether the system behaved as the
// scenario promises.
type Check func(*Result) CheckResult

// Scenario is one scripted situation in the virtual city.
type Scenario struct {
	ID         string
	Title      string
	Tagline    string
	Situation  string // what is happening, in plain English
	Existing   string // how simple validators fare
	Varuna     string // what VarunaSetu does
	Limitation bool   // a documented limit: the attack is expected to succeed
	Duration   time.Duration
	Weather    *Weather
	Rules      []Rule
	Probes     []Probe
	Focus      []string // cells to highlight on the map
	Checks     []Check
}

// Meta is the scenario description sent to the UI.
func (s *Scenario) Meta() map[string]any {
	return map[string]any{
		"id": s.ID, "title": s.Title, "tagline": s.Tagline, "situation": s.Situation,
		"existing": s.Existing, "varuna": s.Varuna, "limitation": s.Limitation,
		"durationMin": s.Duration.Minutes(), "focus": s.Focus,
	}
}

// ---- helpers --------------------------------------------------------------

func min(n int) time.Duration { return time.Duration(n) * time.Minute }

func stormAt(name string, c *Cell, peak float64, from, to time.Duration, sigma float64) Storm {
	return Storm{Name: name, X0: c.X, Y0: c.Y, SigmaM: sigma, PeakMMh: peak, Start: from, End: to, RampDur: min(40)}
}

func cellSet(ids ...string) map[string]bool { return Set(ids...) }

func where(res *Result, f func(*Outcome) bool) (out []*Outcome) {
	for _, o := range res.Outcomes {
		if f(o) {
			out = append(out, o)
		}
	}
	return
}

func inCells(cells ...string) func(*Outcome) bool {
	m := Set(cells...)
	return func(o *Outcome) bool { return m[o.Cell] }
}

func fraudOnly(o *Outcome) bool { return o.Fraud }

// heavy selects readings where the true rain is large enough for an inflated
// claim to stand out from the verifier's tolerance (small absolute inflations
// in light rain are inside it and are also worth little).
func heavy(o *Outcome) bool      { return o.TruthMM >= 1.5 }
func honestOnly(o *Outcome) bool { return !o.Fraud }

func count(res *Result, f func(*Outcome) bool, g func(*Outcome) bool) (n, of int) {
	for _, o := range res.Outcomes {
		if f(o) {
			of++
			if g(o) {
				n++
			}
		}
	}
	return
}

func finalIs(s string) func(*Outcome) bool  { return func(o *Outcome) bool { return o.Final == s } }
func reasonIs(s string) func(*Outcome) bool { return func(o *Outcome) bool { return o.Reason == s } }

func and(fs ...func(*Outcome) bool) func(*Outcome) bool {
	return func(o *Outcome) bool {
		for _, f := range fs {
			if !f(o) {
				return false
			}
		}
		return true
	}
}

// atLeast builds a check "at least frac of the matching readings satisfy g".
func atLeast(name string, f, g func(*Outcome) bool, frac float64) Check {
	return func(res *Result) CheckResult {
		n, of := count(res, f, g)
		pass := of > 0 && float64(n) >= frac*float64(of)
		return CheckResult{Name: name, Pass: pass, Detail: fmt.Sprintf("%d of %d (need ≥ %.0f%%)", n, of, frac*100)}
	}
}

// atMost builds a check "at most frac of the matching readings satisfy g".
func atMost(name string, f, g func(*Outcome) bool, frac float64) Check {
	return func(res *Result) CheckResult {
		n, of := count(res, f, g)
		pass := of > 0 && float64(n) <= frac*float64(of)
		return CheckResult{Name: name, Pass: pass, Detail: fmt.Sprintf("%d of %d (allowed ≤ %.0f%%)", n, of, frac*100)}
	}
}

func scoreOf(res *Result, v string) Confusion {
	for _, c := range res.Scoreboard {
		if c.Validator == v {
			return c
		}
	}
	return Confusion{}
}

func devicesOf(w *World, cell string, from, to int) []string {
	ids := w.DeviceIDs(cell)
	if to > len(ids) {
		to = len(ids)
	}
	return ids[from:to]
}

func cellWith(w *World, ring, minDev int, skip map[string]bool) string {
	for _, id := range w.Ring(ring) {
		if !skip[id] && w.Cells[id].Devices >= minDev {
			return id
		}
	}
	return ""
}

// ---- catalogue -------------------------------------------------------------

// Scenarios returns the demo catalogue for a world, in presentation order.
func Scenarios(w *World) []*Scenario {
	return []*Scenario{
		sNormal(w), sInflate(w), sRadarBlind(w), sRadarRecovers(w), sSameEvidence(w), sSparse(w), sHardening(w),
		sLocalBurst(w), sCollusion(w), sRadarSpoof(w), sOnset(w), sCityDay(w),
	}
}

// Find returns the scenario with the given id.
func Find(w *World, id string) *Scenario {
	for _, s := range Scenarios(w) {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func sNormal(w *World) *Scenario {
	wx := &Weather{Storms: []Storm{{Name: "Monsoon cell", X0: -1000, Y0: 100, VX: 0.45, SigmaM: 450, PeakMMh: 48, Start: 0, End: min(95), RampDur: min(40)}}}
	return &Scenario{
		ID: "storm-crossing", Title: "A storm crosses the city", Tagline: "The control case: honest devices, working radar",
		Situation: "A slow monsoon cell drifts west to east across 37 H3 cells. Every roof has a metered tank and an enrolled, signed device. Nobody cheats and the radar works.",
		Existing:  "Static threshold, radar-only and peer-only all accept honest data, so this case cannot tell them apart. It is here to show the baseline is clean.",
		Varuna:    "Each reading is checked against the radar bound and the median/MAD of its own H3 cell. Honest readings are accepted with RADAR_AND_PEERS_AGREE, and no escrows are needed.",
		Duration:  min(105), Weather: wx,
		Checks: []Check{
			atLeast("honest readings accepted", honestOnly, finalIs("ACCEPT"), 0.95),
			func(res *Result) CheckResult {
				n := res.Finals["PENDING"] + res.Escrows["EXPIRED"]
				return CheckResult{Name: "no escrows left unresolved or expired", Pass: n == 0, Detail: fmt.Sprintf("%d", n)}
			},
		},
	}
}

func sInflate(w *World) *Scenario {
	core := w.CenterCell
	r1 := cellWith(w, 1, 7, nil)
	wx := &Weather{
		Storms: []Storm{stormAt("Storm over the core", w.Cells[core], 50, 0, min(70), 600)},
		Faults: []RadarFault{{Cells: cellSet(core), From: 0, To: min(120), Mode: RadarScale, Factor: 1.6}},
	}
	a, b := devicesOf(w, core, 2, 3)[0], devicesOf(w, r1, 1, 2)[0]
	return &Scenario{
		ID: "inflated-claims", Title: "Two neighbours inflate their harvest", Tagline: "The radar over-reads, so a radar bound alone lets cheating through",
		Situation: "Heavy rain over the core. The radar over-reads there (×1.6, a common convective bias). One household in the core and one next to it claim 2.2× the water they caught.",
		Existing:  "A fixed 10 mm cap accepts both. Radar-only accepts the core cheat because the inflated radar leaves room for it. Peer-only catches both, but only once ≥4 peers exist.",
		Varuna:    "The physical bound is combined with the robust z-score against same-cell peers. The core cheat is stopped as SPATIAL_PEER_OUTLIER despite the generous radar, and the other as PHYSICAL_BOUND_EXCEEDED or an outlier.",
		Duration:  min(100), Weather: wx,
		Rules: []Rule{
			{Devices: Set(a), From: min(40), To: min(100), Kind: Inflate, Factor: 2.2},
			{Devices: Set(b), From: min(40), To: min(100), Kind: Inflate, Factor: 2.2},
		},
		Focus: []string{core, r1},
		Checks: []Check{
			atLeast("inflated claims rejected while it rains ≥ 1.5 mm/window", and(fraudOnly, heavy), finalIs("REJECT"), 0.95),
			atLeast("honest readings still accepted", honestOnly, finalIs("ACCEPT"), 0.9),
			func(res *Result) CheckResult {
				v, s := scoreOf(res, "VARUNASETU"), scoreOf(res, "STATIC_THRESHOLD")
				return CheckResult{Name: "fewer false accepts than the static threshold", Pass: v.FalseAccepts < s.FalseAccepts,
					Detail: fmt.Sprintf("VarunaSetu %d vs static %d", v.FalseAccepts, s.FalseAccepts)}
			},
		},
	}
}

func sRadarBlind(w *World) *Scenario {
	core := w.CenterCell
	skip := cellSet(core)
	for _, n := range w.Cells[core].Neighbors {
		skip[n] = true
	}
	wx := &Weather{
		Storms: []Storm{stormAt("Storm over the core", w.Cells[core], 55, 0, min(75), 650)},
		Faults: []RadarFault{{Cells: skip, From: 0, To: min(55), Mode: RadarDry}},
	}
	return &Scenario{
		ID: "radar-blind-spot", Title: "The radar goes blind over the city centre", Tagline: "Real rain, dry radar: escrow instead of rejection, then neighbours confirm",
		Situation: "A cloudburst sits under a radar blind spot (beam blockage / outage) for 55 minutes. Honest households report heavy rain while the radar says dry.",
		Existing:  "Radar-only rejects every honest report. That is the failure that makes people stop trusting a purely radar-based system. Static and peer-only accept without ever noticing the conflict.",
		Varuna:    "Radar says dry but a coherent group of peers reports rain, so readings go to PROVISIONAL_ESCROW instead of being rejected. They are released when H3 neighbours also show rain (or when the radar recovers, see the next scenario).",
		Duration:  min(100), Weather: wx, Focus: append([]string{core}, w.Cells[core].Neighbors...),
		Checks: []Check{
			func(res *Result) CheckResult {
				n := res.Reasons["RADAR_PEER_CONFLICT_ESCROW"]
				return CheckResult{Name: "radar/peer conflict goes to escrow", Pass: n > 0, Detail: fmt.Sprintf("%d escrowed readings", n)}
			},
			atLeast("escrowed readings end up credited", func(o *Outcome) bool { return o.State == "PROVISIONAL_ESCROW" }, finalIs("ACCEPT"), 0.9),
			func(res *Result) CheckResult {
				v, r := scoreOf(res, "VARUNASETU"), scoreOf(res, "RADAR_ONLY")
				return CheckResult{Name: "fewer false rejects than radar-only", Pass: v.FalseRejects < r.FalseRejects,
					Detail: fmt.Sprintf("VarunaSetu %d vs radar-only %d", v.FalseRejects, r.FalseRejects)}
			},
		},
	}
}

func sRadarRecovers(w *World) *Scenario {
	cell := cellWith(w, 1, 7, nil)
	c := w.Cells[cell]
	// A tight storm: it rains hard over this one cell, but its neighbours stay
	// below the 2 mm "raining" threshold, so only the radar can confirm it.
	wx := &Weather{
		Storms: []Storm{{Name: "Tight convective cell", X0: c.X, Y0: c.Y, SigmaM: 190, PeakMMh: 60, Start: 0, End: min(90), RampDur: min(40)}},
		Faults: []RadarFault{{Cells: cellSet(cell), From: 0, To: min(36), Mode: RadarDry}},
	}
	return &Scenario{
		ID: "radar-recovers", Title: "The radar blinks, then comes back", Tagline: "Escrow released by the radar's own late confirmation",
		Situation: "A tight storm drenches one cell while the radar over it is down. Neighbouring cells see only light rain, so they cannot vouch for it. After 36 minutes the radar recovers, well inside the 15-minute escrow window of the first held readings.",
		Existing:  "Radar-only has already rejected the honest readings by the time the radar returns, and the credit is gone. Static and peer-only accept without checking anything the radar later reveals.",
		Varuna:    "Readings are held as PROVISIONAL_ESCROW. The first later reading with a live radar value in the same cell corroborates every older pending escrow there (ESCROW_CORROBORATED via RADAR). No neighbour was needed and nothing expired.",
		Duration:  min(100), Weather: wx, Focus: append([]string{cell}, c.Neighbors...),
		Checks: []Check{
			func(res *Result) CheckResult {
				n := res.Escrows["RADAR"]
				return CheckResult{Name: "escrows released by the recovering radar", Pass: n > 0, Detail: fmt.Sprintf("%d escrows", n)}
			},
			func(res *Result) CheckResult {
				n := res.Escrows["EXPIRED"]
				return CheckResult{Name: "no escrow expired", Pass: n == 0, Detail: fmt.Sprintf("%d expired", n)}
			},
			atLeast("escrowed readings end up credited", func(o *Outcome) bool { return o.State == "PROVISIONAL_ESCROW" }, finalIs("ACCEPT"), 0.95),
		},
	}
}

func sSameEvidence(w *World) *Scenario {
	// The real storm sits over the westernmost first-ring cell and the staged
	// claim is made in the easternmost outer cell, far beyond the storm's reach.
	west := w.Ring(1)[0]
	for _, id := range w.Ring(1) {
		if w.Cells[id].X < w.Cells[west].X {
			west = id
		}
	}
	// remote cell on the far side of the city from the storm
	fake := ""
	best := -1.0
	for _, id := range w.Ring(3) {
		c := w.Cells[id]
		if c.Devices >= 6 && c.X > best {
			fake, best = id, c.X
		}
	}
	wx := &Weather{
		Storms: []Storm{stormAt("Real storm over the west", w.Cells[west], 48, 0, min(90), 450)},
		Faults: []RadarFault{{Cells: nil, From: 0, To: min(120), Mode: RadarDry}}, // whole-city radar outage
	}
	return &Scenario{
		ID: "same-evidence", Title: "Same evidence, opposite truth", Tagline: "The H3 neighbourhood tells a real storm from a staged one",
		Situation: "The radar is down city-wide. In the west a real storm drenches the neighbourhood. Far away in cell " + fake[len(fake)-6:] + " every household reports the same heavy rain, but it is dry there (a coordinated false claim).",
		Existing:  "At device level the two look identical: dry radar, coherent peers reporting rain. Radar-only rejects both (wrong for the real storm). Peer-only and static accept both (wrong for the fake).",
		Varuna:    "Both go to escrow. Then H3 decides: the western cells' k-ring neighbours show rain, so its escrows are released as ESCROW_CORROBORATED via H3_NEIGHBORS. The remote cell has no raining neighbours, so its escrows expire as ESCROW_EXPIRED_UNCORROBORATED.",
		Duration:  min(100), Weather: wx,
		Rules: []Rule{{Devices: Set(w.DeviceIDs(fake)...), From: min(25), To: min(60), Kind: Collude, FakeMM: 4.5}},
		Focus: append([]string{west, fake}, w.Cells[west].Neighbors...),
		Checks: []Check{
			func(res *Result) CheckResult {
				n := res.Escrows["H3_NEIGHBORS"]
				return CheckResult{Name: "escrows released by H3 neighbours", Pass: n > 0, Detail: fmt.Sprintf("%d escrows", n)}
			},
			atLeast("fake cell is not credited", and(fraudOnly), finalIs("REJECT"), 0.95),
			atLeast("real storm is credited", honestOnly, finalIs("ACCEPT"), 0.85),
			func(res *Result) CheckResult {
				v := scoreOf(res, "VARUNASETU")
				ok := true
				detail := fmt.Sprintf("VarunaSetu %.0f%%", v.Balanced*100)
				for _, b := range BaselineNames {
					if scoreOf(res, b).Balanced >= v.Balanced {
						ok = false
					}
					detail += fmt.Sprintf(", %s %.0f%%", b, scoreOf(res, b).Balanced*100)
				}
				return CheckResult{Name: "best balanced accuracy (fraud caught + honest credited)", Pass: ok, Detail: detail}
			},
		},
	}
}

func sSparse(w *World) *Scenario {
	var sp string
	for _, id := range w.Ring(2) {
		if w.Cells[id].Devices <= 3 {
			sp = id
			break
		}
	}
	wx := &Weather{Storms: []Storm{stormAt("Storm over the outskirts", w.Cells[sp], 48, 0, min(90), 600)}}
	cheat := w.DeviceIDs(sp)[0]
	return &Scenario{
		ID: "sparse-cell", Title: "A cell with too few neighbours to compare", Tagline: "Graceful degradation where peer statistics are impossible",
		Situation: fmt.Sprintf("Cell %s has only %d metered roofs, fewer than the 4 peers the robust statistics need. One of them claims 3× the rain it caught.", sp[len(sp)-6:], w.Cells[sp].Devices),
		Existing:  "Peer-only has nothing to compare against and accepts everything. A fixed cap accepts the cheat as well.",
		Varuna:    "Without enough peers the verifier falls back to the radar physical bound: honest claims are accepted as RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS, and the claim above the bound is held back as INSUFFICIENT_PEERS_REVIEW.",
		Duration:  min(100), Weather: wx,
		Rules: []Rule{{Devices: Set(cheat), From: min(40), To: min(100), Kind: Inflate, Factor: 3}},
		Focus: []string{sp},
		Checks: []Check{
			atLeast("sparse-cell cheat rejected while it rains ≥ 1.5 mm/window", and(fraudOnly, heavy), finalIs("REJECT"), 0.95),
			func(res *Result) CheckResult {
				n := res.Reasons["RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS"]
				return CheckResult{Name: "radar-physics fallback used", Pass: n > 0, Detail: fmt.Sprintf("%d readings", n)}
			},
		},
	}
}

func sHardening(w *World) *Scenario {
	kinds := []string{"REPLAY", "NONCE_REUSE", "TAMPER", "UNKNOWN_DEVICE", "REVOKED", "STALE", "FUTURE",
		"BAD_PAYLOAD", "INVALID_H3", "UNKNOWN_BUILDING", "INVALID_FIELDS", "NEGATIVE_CLAIM", "TANK_OVERFLOW"}
	var probes []Probe
	for i, k := range kinds {
		probes = append(probes, Probe{At: min(1 + 2*i), Kind: k})
	}
	return &Scenario{
		ID: "api-hardening", Title: "Attacks on the door, not the data", Tagline: "Forged, replayed, stale, malformed and impossible messages",
		Situation: "No rain at all. Thirteen hostile or broken messages hit POST /telemetry/readings: replays, tampering, an unenrolled key, a revoked device, stale and future timestamps, malformed JSON and impossible numbers.",
		Existing:  "A system that only looks at the numbers cannot tell a forged but plausible reading from a real one, and would credit replays twice.",
		Varuna:    "Ed25519 signatures bind device, time, nonce and payload. Durable idempotency returns the stored result for a replay. Invalid or impossible readings get a precise 4xx and are never credited.",
		Duration:  min(30), Weather: &Weather{},
		Probes: probes,
		Checks: []Check{
			func(res *Result) CheckResult {
				ok := 0
				for _, p := range res.Probes {
					if p.Pass {
						ok++
					}
				}
				return CheckResult{Name: "every hostile message answered as documented", Pass: ok == len(res.Probes) && ok == len(kinds), Detail: fmt.Sprintf("%d of %d", ok, len(kinds))}
			},
		},
	}
}

func sLocalBurst(w *World) *Scenario {
	cell := cellWith(w, 3, 6, nil)
	wx := &Weather{
		Storms: []Storm{{Name: "Hyper-local cloudburst", X0: w.Cells[cell].X, Y0: w.Cells[cell].Y, SigmaM: 110, PeakMMh: 60, Start: 0, End: min(75), RampDur: min(40)}},
		Faults: []RadarFault{{Cells: cellSet(cell), From: 0, To: min(120), Mode: RadarDry}},
	}
	return &Scenario{
		ID: "local-burst", Title: "A real cloudburst nobody else can see", Tagline: "Known limit: a genuine one-cell storm missed by radar is not credited",
		Limitation: true,
		Situation:  "A genuinely hyper-local cloudburst soaks a single cell. The radar misses it and the neighbouring cells stay dry. Every household in the cell honestly reports heavy rain.",
		Existing:   "Radar-only rejects it. Static and peer-only accept it, but they would accept a staged copy of it too.",
		Varuna:     "By its data this is indistinguishable from the staged claim in 'Same evidence'. The system fails safe: readings are escrowed, nothing corroborates them, and they expire as ESCROW_EXPIRED_UNCORROBORATED. Honest users lose credit, and that is the price of not paying fraud.",
		Duration:   min(100), Weather: wx, Focus: []string{cell},
		Checks: []Check{
			func(res *Result) CheckResult {
				n := res.Escrows["EXPIRED"]
				return CheckResult{Name: "escrows expire uncorroborated (fail safe)", Pass: n > 0, Detail: fmt.Sprintf("%d expired", n)}
			},
		},
	}
}

func triangle(w *World) (a, b, c string) {
	for _, t := range w.Ring(1) {
		for _, n1 := range w.Cells[t].Neighbors {
			c1, ok := w.Cells[n1]
			if !ok || c1.Devices < 3 || c1.Ring == 0 {
				continue
			}
			for _, n2 := range w.Cells[t].Neighbors {
				c2, ok := w.Cells[n2]
				if !ok || n2 <= n1 || c2.Devices < 3 || c2.Ring == 0 {
					continue
				}
				for _, x := range w.Cells[n1].Neighbors {
					if x == n2 {
						return t, n1, n2
					}
				}
			}
		}
	}
	return "", "", ""
}

func sCollusion(w *World) *Scenario {
	a, b, c := triangle(w)
	ids := append(append(w.DeviceIDs(a), w.DeviceIDs(b)...), w.DeviceIDs(c)...)
	return &Scenario{
		ID: "cross-cell-collusion", Title: "Three neighbouring cells collude", Tagline: "Known limit: H3 corroboration alone cannot stop a coordinated cluster",
		Limitation: true,
		Situation:  fmt.Sprintf("No rain anywhere. Every device in three mutually adjacent cells (%d devices) fabricates the same heavy rainfall.", len(ids)),
		Existing:   "Static and peer-only accept it. Radar-only rejects it (dry radar).",
		Varuna:     "Honest limitation: each colluding cell has two colluding neighbours with ≥3 devices each, so the neighbour rule is satisfied and the fraud is credited. The verifier cannot tell a corroborating neighbour from a complicit one. H3 gives locality, not trust (HANDOFF.md K1/C2).",
		Duration:   min(70), Weather: &Weather{},
		Rules: []Rule{{Devices: Set(ids...), From: min(10), To: min(40), Kind: Collude, FakeMM: 5}},
		Focus: []string{a, b, c},
		Checks: []Check{
			func(res *Result) CheckResult {
				n := res.Escrows["H3_NEIGHBORS"]
				return CheckResult{Name: "limit reproduced: colluders corroborate each other", Pass: n > 0, Detail: fmt.Sprintf("%d escrows released by colluding neighbours", n)}
			},
		},
	}
}

func sRadarSpoof(w *World) *Scenario {
	var sp string
	for _, id := range w.Ring(2) {
		if w.Cells[id].Devices <= 3 {
			sp = id
		}
	}
	wx := &Weather{Storms: []Storm{stormAt("Storm over the outskirts", w.Cells[sp], 48, 0, min(90), 600)}}
	cheat := w.DeviceIDs(sp)[0]
	return &Scenario{
		ID: "radar-spoof", Title: "A cheat that also lies about the radar", Tagline: "Known limit: radar is device-supplied in this prototype",
		Limitation: true,
		Situation:  "Same sparse cell as 'A cell with too few neighbours', but this cheat also reports a radar value that makes its inflated claim fit the physical bound.",
		Existing:   "Every baseline accepts it, because each one trusts the numbers in the message.",
		Varuna:     "Honest limitation: radar_mm travels inside the signed payload, so a compromised enrolled device can set it. With too few peers there is nothing else to check against. The fix is a server-side radar feed keyed by H3 cell, which this prototype does not yet have.",
		Duration:   min(100), Weather: wx, Focus: []string{sp},
		Rules: []Rule{{Devices: Set(cheat), From: min(40), To: min(100), Kind: InflateSpoofRadar, Factor: 3}},
		Checks: []Check{
			func(res *Result) CheckResult {
				n, of := count(res, fraudOnly, finalIs("ACCEPT"))
				return CheckResult{Name: "limit reproduced: radar-spoofing cheat is credited", Pass: n > 0, Detail: fmt.Sprintf("%d of %d fraudulent readings accepted", n, of)}
			},
		},
	}
}

func sOnset(w *World) *Scenario {
	core := w.CenterCell
	wx := &Weather{Storms: []Storm{{Name: "Abrupt cloudburst", X0: w.Cells[core].X, Y0: w.Cells[core].Y, SigmaM: 700, PeakMMh: 85, Start: 0, End: min(40), RampDur: min(4)}}}
	return &Scenario{
		ID: "abrupt-onset", Title: "Rain starts faster than peers can follow", Tagline: "Known limit: honest readings at an abrupt onset can be flagged",
		Limitation: true,
		Situation:  "Rain goes from nothing to 85 mm/h in four minutes. Everyone is honest and the radar is right.",
		Existing:   "All baselines accept honest data here (as they do in the control case).",
		Varuna:     "Honest limitation: a device's peers are the latest readings of the last 10 minutes, which at onset still describe the dry window. The first reporters of the new window can look like positive outliers (SPATIAL_PEER_OUTLIER) until most peers have reported it.",
		Duration:   min(50), Weather: wx, Focus: []string{core},
		Checks: []Check{
			func(res *Result) CheckResult {
				n, of := count(res, honestOnly, finalIs("REJECT"))
				return CheckResult{Name: "limit measured: honest readings rejected at onset", Pass: true, Detail: fmt.Sprintf("%d of %d honest readings rejected", n, of)}
			},
		},
	}
}

func sCityDay(w *World) *Scenario {
	core := w.CenterCell
	r1 := w.Ring(1)
	// The staged claim is made far from the storm's west-east track (the
	// northernmost / southernmost outer cell), so no real rain borders it.
	fakeCell := ""
	best := -1.0
	for _, id := range w.Ring(3) {
		c := w.Cells[id]
		if y := math.Abs(c.Y); c.Devices >= 6 && y > best {
			fakeCell, best = id, y
		}
	}
	var sp string
	for _, id := range w.Ring(2) {
		if w.Cells[id].Devices <= 3 {
			sp = id
			break
		}
	}
	faultCells := cellSet(append([]string{core}, r1[:3]...)...)
	wx := &Weather{
		Storms: []Storm{
			{Name: "Monsoon cell", X0: -1000, Y0: 0, VX: 0.4, SigmaM: 550, PeakMMh: 48, Start: 0, End: min(115), RampDur: min(40)},
			{Name: "Cell over the outskirts", X0: w.Cells[sp].X, Y0: w.Cells[sp].Y, SigmaM: 500, PeakMMh: 45, Start: min(10), End: min(95), RampDur: min(40)},
		},
		Faults: []RadarFault{{Cells: faultCells, From: min(35), To: min(75), Mode: RadarDry}},
	}
	cheatCore := devicesOf(w, core, 1, 2)[0]
	cheatR1 := devicesOf(w, r1[4], 0, 1)[0]
	offline := devicesOf(w, r1[2], 0, 1)[0]
	var probes []Probe
	for i, k := range []string{"REPLAY", "TAMPER", "REVOKED", "STALE", "INVALID_FIELDS", "TANK_OVERFLOW"} {
		probes = append(probes, Probe{At: min(40 + 9*i), Kind: k})
	}
	return &Scenario{
		ID: "city-day", Title: "A day in the virtual city", Tagline: "Everything at once: storms, a radar outage, cheaters, a staged cell, attacks",
		Situation: "Two storms cross a 37-cell city for two hours. The radar goes blind over the core for 40 minutes. Two households inflate, a sparse cell has a cheat, a remote cell stages a storm, a meter goes offline, and the API is attacked throughout.",
		Existing:  "No single baseline survives all of it: each one fails in a different way. The scoreboard shows the totals.",
		Varuna:    "The same code path handles all of it: verifier decisions, escrows resolved by radar or H3 neighbours, expiry for the staged claim, identity rejections, and an audit ledger that re-verifies at the end.",
		Duration:  min(125), Weather: wx,
		Rules: []Rule{
			{Devices: Set(cheatCore), From: min(50), To: min(125), Kind: Inflate, Factor: 2.4},
			{Devices: Set(cheatR1), From: min(55), To: min(125), Kind: Inflate, Factor: 2.0},
			{Devices: Set(w.DeviceIDs(sp)[0]), From: min(50), To: min(125), Kind: Inflate, Factor: 3},
			{Devices: Set(w.DeviceIDs(fakeCell)...), From: min(30), To: min(70), Kind: Collude, FakeMM: 4.5},
			{Devices: Set(offline), From: min(40), To: min(125), Kind: Offline},
		},
		Probes: probes,
		Focus:  []string{core, fakeCell, sp},
		Checks: []Check{
			atLeast("honest readings credited", honestOnly, finalIs("ACCEPT"), 0.85),
			atLeast("manipulated readings rejected (staged cell + inflations in real rain)", and(fraudOnly, func(o *Outcome) bool { return o.Kind == Collude || heavy(o) }), finalIs("REJECT"), 0.9),
			func(res *Result) CheckResult {
				v := scoreOf(res, "VARUNASETU")
				best, who := 0.0, ""
				for _, b := range BaselineNames {
					if a := scoreOf(res, b).Balanced; a > best {
						best, who = a, b
					}
				}
				return CheckResult{Name: "higher balanced accuracy than every baseline", Pass: v.Balanced > best,
					Detail: fmt.Sprintf("VarunaSetu %.1f%% vs best baseline %.1f%% (%s)", v.Balanced*100, best*100, who)}
			},
		},
	}
}

// SortedReasons returns reason codes by descending count (for reports).
func SortedReasons(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] })
	return keys
}
