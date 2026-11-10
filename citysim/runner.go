package citysim

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"hydroledger-prototype/ledger"
	"hydroledger-prototype/service"
)

// Clock is the virtual wall clock shared by the simulator and the embedded
// service (service.Config.Now), so a 40-minute storm can be replayed in
// seconds while every timestamp, peer window, escrow TTL and signature window
// in the real code keeps its real meaning.
type Clock struct {
	mu sync.RWMutex
	t  time.Time
}

func NewClock(t time.Time) *Clock { return &Clock{t: t} }
func (c *Clock) Now() time.Time   { c.mu.RLock(); defer c.mu.RUnlock(); return c.t }
func (c *Clock) Set(t time.Time)  { c.mu.Lock(); c.t = t; c.mu.Unlock() }

// Event is one message on the live stream.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Outcome is the full story of one reading.
type Outcome struct {
	Seq         int             `json:"seq"`
	SimMin      float64         `json:"simMin"`
	At          time.Time       `json:"at"`
	Device      string          `json:"device"`
	Cell        string          `json:"cell"`
	Kind        Kind            `json:"kind"`
	Fraud       bool            `json:"fraud"`
	TruthMM     float64         `json:"truthMm"`
	RadarMM     float64         `json:"radarMm"`
	Claimed     float64         `json:"claimedLitres"`
	API         PostResult      `json:"api"`
	State       string          `json:"state"`  // verdict returned by the API
	Reason      string          `json:"reason"` // reason code returned by the API
	ReadingID   string          `json:"readingId"`
	Final       string          `json:"final"` // ACCEPT | REJECT | PENDING after escrow resolution
	FinalReason string          `json:"finalReason"`
	Resolution  string          `json:"resolution,omitempty"`
	Baseline    map[string]bool `json:"baseline"`
}

func (o *Outcome) Accepted() bool { return o.Final == "ACCEPT" }

// ProbeResult is the result of one API-level attack/robustness probe.
type ProbeResult struct {
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	What          string  `json:"what"`
	SimMin        float64 `json:"simMin"`
	ExpectStatus  int     `json:"expectStatus"`
	ExpectReason  string  `json:"expectReason,omitempty"`
	Status        int     `json:"status"`
	Reason        string  `json:"reason,omitempty"`
	Message       string  `json:"message,omitempty"`
	LatencyMs     float64 `json:"latencyMs"`
	Pass          bool    `json:"pass"`
	ContentOnlyOK *bool   `json:"contentOnlyWouldAccept,omitempty"` // a system that only judged the numbers
	Extra         string  `json:"extra,omitempty"`
}

// Confusion is a validator's score against ground truth.
type Confusion struct {
	Validator    string  `json:"validator"`
	Caught       int     `json:"caught"`       // fraud rejected
	Credited     int     `json:"credited"`     // honest accepted
	FalseAccepts int     `json:"falseAccepts"` // fraud accepted
	FalseRejects int     `json:"falseRejects"` // honest rejected
	Accuracy     float64 `json:"accuracy"`     // plain share of correct decisions
	CatchRate    float64 `json:"catchRate"`    // share of fraud rejected (-1 if no fraud)
	CreditRate   float64 `json:"creditRate"`   // share of honest readings credited (-1 if none)
	Balanced     float64 `json:"balanced"`     // mean of the two rates, so 2000 honest readings cannot hide 40 frauds
}

// CheckResult is a self-verifying expectation of a scenario.
type CheckResult struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// Result is the full record of a run.
type Result struct {
	Scenario   string         `json:"scenario"`
	Title      string         `json:"title"`
	Start      time.Time      `json:"start"`
	Outcomes   []*Outcome     `json:"outcomes"`
	Probes     []ProbeResult  `json:"probes"`
	Scoreboard []Confusion    `json:"scoreboard"`
	Checks     []CheckResult  `json:"checks"`
	Reasons    map[string]int `json:"reasons"` // verdict reason -> count (as returned at ingest)
	Finals     map[string]int `json:"finals"`  // ACCEPT/REJECT/PENDING after escrow resolution
	Escrows    map[string]int `json:"escrows"` // resolution path -> count
	Ledger     LedgerReport   `json:"ledger"`
	Requests   int            `json:"requests"`
	WallMs     int64          `json:"wallMs"`
	Pass       bool           `json:"pass"`
	Limitation bool           `json:"limitation"`
}

// LedgerReport is the outcome of re-verifying the audit hash chain.
type LedgerReport struct {
	Entries  int    `json:"entries"`
	Verified bool   `json:"verified"`
	HeadSeq  int64  `json:"headSeq"`
	HeadHash string `json:"headHash"`
	Error    string `json:"error,omitempty"`
}

// Runner executes scenarios against a service.
type Runner struct {
	World *World
	API   *API
	DB    *sql.DB
	Clock *Clock
	Sweep func(ctx context.Context, now time.Time) error
	Emit  func(Event)

	Scn *Scenario
	T0  time.Time

	mu      sync.Mutex
	paused  bool
	speed   float64 // sim seconds per wall second; 0 = unthrottled
	seq     map[string]int
	tag     string
	reqs    int
	pending map[string]*Outcome // readingID -> outcome awaiting escrow resolution
	bl      *Baselines
}

func NewRunner(w *World, api *API, db *sql.DB, clock *Clock, sweep func(context.Context, time.Time) error) *Runner {
	return &Runner{World: w, API: api, DB: db, Clock: clock, Sweep: sweep, Emit: func(Event) {}, speed: 0}
}

func (r *Runner) SetSpeed(s float64) { r.mu.Lock(); r.speed = s; r.mu.Unlock() }
func (r *Runner) Speed() float64     { r.mu.Lock(); defer r.mu.Unlock(); return r.speed }
func (r *Runner) SetPaused(p bool)   { r.mu.Lock(); r.paused = p; r.mu.Unlock() }
func (r *Runner) Paused() bool       { r.mu.Lock(); defer r.mu.Unlock(); return r.paused }

// pace waits for the real time a simulated step should take at the current
// speed, and blocks while paused.
func (r *Runner) pace(ctx context.Context, simStep time.Duration) error {
	if simStep < 0 {
		simStep = 0
	}
	remaining := simStep
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		sp, paused := r.Speed(), r.Paused()
		if paused {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if sp <= 0 || remaining <= 0 {
			return nil
		}
		slice := 40 * time.Millisecond
		wall := time.Duration(float64(remaining) / sp)
		if wall <= slice {
			time.Sleep(wall)
			return nil
		}
		time.Sleep(slice)
		remaining -= time.Duration(float64(slice) * sp)
	}
}

type item struct {
	at    time.Duration
	order int
	kind  string // report | probe | minute
	dev   *Device
	probe *Probe
	k     int
}

// Run executes a scenario and returns its full result.
func (r *Runner) Run(ctx context.Context, scn *Scenario) (*Result, error) {
	wallStart := time.Now()
	r.Scn = scn
	r.seq = map[string]int{}
	r.reqs = 0
	r.pending = map[string]*Outcome{}
	r.bl = NewBaselines()
	for _, d := range r.World.Devices {
		d.Building.storedL = 0
	}
	if err := Provision(ctx, r.DB, r.World); err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}
	t0, err := r.nextStart(ctx)
	if err != nil {
		return nil, err
	}
	r.T0 = t0
	r.tag = strconv.FormatInt(t0.Unix(), 36)
	r.Clock.Set(t0)

	res := &Result{Scenario: scn.ID, Title: scn.Title, Start: t0, Limitation: scn.Limitation,
		Reasons: map[string]int{}, Finals: map[string]int{}, Escrows: map[string]int{}}
	r.Emit(Event{"run_start", map[string]any{
		"scenario": scn.Meta(), "start": t0, "durationMin": scn.Duration.Minutes(),
		"clock": t0, "ttlMin": 15.0,
	}})

	// Timeline: device reports, probes and per-minute sweeps.
	var tl []item
	n := int(scn.Duration / Window)
	for k := 1; k <= n; k++ {
		for i, d := range r.World.Devices {
			tl = append(tl, item{at: time.Duration(k)*Window + time.Duration(d.phase*float64(time.Second)).Round(time.Millisecond), order: i, kind: "report", dev: d, k: k})
		}
	}
	for i := range scn.Probes {
		p := scn.Probes[i]
		tl = append(tl, item{at: p.At, order: 1 << 20, kind: "probe", probe: &p})
	}
	end := scn.Duration + 16*time.Minute // drain: escrow TTL is 15 minutes
	for m := 1; time.Duration(m)*time.Minute <= end; m++ {
		tl = append(tl, item{at: time.Duration(m) * time.Minute, order: 1 << 21, kind: "minute", k: m})
	}
	sort.SliceStable(tl, func(i, j int) bool {
		if tl[i].at != tl[j].at {
			return tl[i].at < tl[j].at
		}
		return tl[i].order < tl[j].order
	})

	var now time.Duration
	for _, it := range tl {
		if err := r.pace(ctx, it.at-now); err != nil {
			return nil, err
		}
		now = it.at
		r.Clock.Set(t0.Add(now))
		switch it.kind {
		case "report":
			obs := r.observe(it.dev, time.Duration(it.k)*Window)
			if !obs.Report {
				continue
			}
			oc := r.send(ctx, it.dev, obs, now)
			res.Outcomes = append(res.Outcomes, oc)
			if oc.API.Corrob > 0 {
				r.refreshEscrows(ctx, now)
			}
		case "probe":
			pr := r.runProbe(ctx, *it.probe, now)
			res.Probes = append(res.Probes, pr)
			r.Emit(Event{"probe", pr})
		case "minute":
			if err := r.Sweep(ctx, r.Clock.Now()); err != nil {
				return nil, fmt.Errorf("sweep: %w", err)
			}
			if len(r.pending) > 0 {
				r.refreshEscrows(ctx, now)
			}
			r.emitTick(now)
		}
	}

	r.finalise(ctx, res)
	res.Requests = r.reqs
	res.WallMs = time.Since(wallStart).Milliseconds()
	r.Emit(Event{"run_end", res.Summary()})
	return res, nil
}

// nextStart picks a virtual start strictly after every stored event, so runs
// never see each other's readings as peers and nothing has to be deleted.
func (r *Runner) nextStart(ctx context.Context) (time.Time, error) {
	var last sql.NullTime
	if err := r.DB.QueryRowContext(ctx, `SELECT MAX(event_time) FROM telemetry_reading`).Scan(&last); err != nil {
		return time.Time{}, err
	}
	t := time.Now().UTC().Truncate(5 * time.Minute)
	if last.Valid && !last.Time.Add(time.Hour).Before(t) {
		t = last.Time.UTC().Add(time.Hour).Truncate(5 * time.Minute).Add(5 * time.Minute)
	}
	return t, nil
}

func (r *Runner) send(ctx context.Context, d *Device, obs Observation, at time.Duration) *Outcome {
	r.seq[d.ID]++
	ts := r.Clock.Now()
	wire, err := Sign(d.Key, ts, fmt.Sprintf("%s-%s-%05d", d.ID, r.tag, r.seq[d.ID]), obs.Tel)
	oc := &Outcome{Seq: r.reqs + 1, SimMin: at.Minutes(), At: ts, Device: d.ID, Cell: d.Building.CellID, Kind: obs.Kind,
		Fraud: obs.Fraud, TruthMM: obs.TruthMM, RadarMM: obs.RadarMM, Claimed: obs.Tel.ClaimedLitres}
	if err != nil {
		oc.API.Error = err.Error()
		oc.Final, oc.FinalReason = "REJECT", "SIGN_ERROR"
		return oc
	}
	rObs := obs.ClaimMM
	oc.Baseline = r.bl.Judge(d.ID, d.Building.CellID, ts, rObs, obs.RadarMM)
	oc.API = r.API.Post(ctx, wire)
	r.reqs++
	switch {
	case oc.API.Decision != nil:
		oc.State, oc.Reason, oc.ReadingID = oc.API.Decision.State, oc.API.Decision.Reason, oc.API.ID
	default:
		oc.State, oc.Reason = "ERROR", fmt.Sprintf("HTTP_%d", oc.API.Status)
	}
	switch oc.State {
	case "ACCEPT":
		oc.Final, oc.FinalReason = "ACCEPT", oc.Reason
	case "QUARANTINE":
		oc.Final, oc.FinalReason = "REJECT", oc.Reason
	case "PROVISIONAL_ESCROW":
		oc.Final, oc.FinalReason = "PENDING", "ESCROW_PENDING"
		r.pending[oc.ReadingID] = oc
	default:
		oc.Final, oc.FinalReason = "REJECT", oc.Reason
	}
	r.Emit(Event{"reading", oc})
	return oc
}

type escrowRow struct {
	ID         string `json:"id"`
	ReadingID  string `json:"readingId"`
	DeviceID   string `json:"deviceId"`
	H3Index    string `json:"h3Index"`
	State      string `json:"state"`
	Resolution string `json:"resolution"`
}

// refreshEscrows polls GET /escrows and updates outcomes whose escrow settled.
func (r *Runner) refreshEscrows(ctx context.Context, at time.Duration) {
	var rows []escrowRow
	if _, err := r.API.Get(ctx, "/escrows?limit=500", &rows); err != nil {
		return
	}
	for _, e := range rows {
		oc, ok := r.pending[e.ReadingID]
		if !ok || e.State == "PENDING" {
			continue
		}
		oc.Resolution = e.Resolution
		if e.State == "CORROBORATED" {
			oc.Final, oc.FinalReason = "ACCEPT", "ESCROW_CORROBORATED"
		} else {
			oc.Final, oc.FinalReason = "REJECT", "ESCROW_EXPIRED_UNCORROBORATED"
		}
		delete(r.pending, e.ReadingID)
		r.Emit(Event{"escrow", map[string]any{
			"readingId": e.ReadingID, "device": e.DeviceID, "cell": e.H3Index, "state": e.State,
			"resolution": e.Resolution, "final": oc.Final, "reason": oc.FinalReason, "simMin": at.Minutes(),
		}})
	}
}

// TickCell is the per-cell weather picture on the live stream.
type TickCell struct {
	ID    string  `json:"id"`
	Truth float64 `json:"truth"` // true rain rate at the cell centre, mm/h
	Radar float64 `json:"radar"` // what the radar reports, mm/h
}

func (r *Runner) emitTick(at time.Duration) {
	wx := r.Scn.Weather
	cells := make([]TickCell, 0, len(r.World.CellOrder))
	for _, id := range r.World.CellOrder {
		c := r.World.Cells[id]
		cells = append(cells, TickCell{ID: id, Truth: wx.Rate(c.X, c.Y, at), Radar: wx.RadarClean(c, at, Window) / Window.Hours()})
	}
	r.Emit(Event{"tick", map[string]any{
		"simMin": at.Minutes(), "clock": r.Clock.Now(), "storms": wx.StormCentres(at), "cells": cells, "pending": len(r.pending),
	}})
}

// finalise polls the escrows one last time, scores every validator, re-verifies
// the ledger chain and evaluates the scenario's own expectations.
func (r *Runner) finalise(ctx context.Context, res *Result) {
	r.refreshEscrows(ctx, r.Scn.Duration+16*time.Minute)
	names := append([]string{}, BaselineNames...)
	names = append(names, "VARUNASETU")
	score := map[string]*Confusion{}
	for _, n := range names {
		score[n] = &Confusion{Validator: n}
	}
	for _, o := range res.Outcomes {
		res.Reasons[o.Reason]++
		res.Finals[o.Final]++
		if o.Resolution != "" {
			res.Escrows[o.Resolution]++
		}
		verdicts := map[string]bool{"VARUNASETU": o.Accepted()}
		for k, v := range o.Baseline {
			verdicts[k] = v
		}
		for n, acc := range verdicts {
			c := score[n]
			switch {
			case o.Fraud && !acc:
				c.Caught++
			case o.Fraud && acc:
				c.FalseAccepts++
			case !o.Fraud && acc:
				c.Credited++
			default:
				c.FalseRejects++
			}
		}
	}
	for _, n := range names {
		c := score[n]
		if tot := c.Caught + c.Credited + c.FalseAccepts + c.FalseRejects; tot > 0 {
			c.Accuracy = float64(c.Caught+c.Credited) / float64(tot)
		}
		c.CatchRate, c.CreditRate, c.Balanced = -1, -1, -1
		if f := c.Caught + c.FalseAccepts; f > 0 {
			c.CatchRate = float64(c.Caught) / float64(f)
		}
		if h := c.Credited + c.FalseRejects; h > 0 {
			c.CreditRate = float64(c.Credited) / float64(h)
		}
		switch {
		case c.CatchRate >= 0 && c.CreditRate >= 0:
			c.Balanced = (c.CatchRate + c.CreditRate) / 2
		case c.CatchRate >= 0:
			c.Balanced = c.CatchRate
		case c.CreditRate >= 0:
			c.Balanced = c.CreditRate
		}
		res.Scoreboard = append(res.Scoreboard, *c)
	}
	res.Ledger = r.verifyLedger(ctx)
	res.Pass = true
	for _, chk := range r.Scn.Checks {
		cr := chk(res)
		res.Checks = append(res.Checks, cr)
		if !cr.Pass {
			res.Pass = false
		}
	}
	for _, p := range res.Probes {
		if !p.Pass {
			res.Pass = false
		}
	}
	if !res.Ledger.Verified {
		res.Pass = false
	}
}

// verifyLedger re-computes the SHA-256 chain of the newest audit entries.
func (r *Runner) verifyLedger(ctx context.Context) LedgerReport {
	var rows []service.LedgerEntryView
	if code, err := r.API.Get(ctx, "/ledger/entries?limit=500", &rows); err != nil || code != 200 {
		return LedgerReport{Error: fmt.Sprintf("GET /ledger/entries: status %d err %v", code, err)}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	rep := LedgerReport{Entries: len(rows), Verified: true}
	for i, e := range rows {
		ent := ledger.Entry{Seq: uint64(e.Seq), EventRef: e.EventRef, PrevHash: e.PrevHash, Timestamp: e.Timestamp.UTC()}
		if ent.ComputeHash() != e.Hash {
			rep.Verified, rep.Error = false, fmt.Sprintf("entry %d hash mismatch", e.Seq)
			break
		}
		if i > 0 && e.PrevHash != rows[i-1].Hash {
			rep.Verified, rep.Error = false, fmt.Sprintf("entry %d does not link to %d", e.Seq, rows[i-1].Seq)
			break
		}
	}
	if n := len(rows); n > 0 {
		rep.HeadSeq, rep.HeadHash = rows[n-1].Seq, rows[n-1].Hash
	}
	return rep
}

// Summary is the compact end-of-run payload for the stream (no per-reading data).
func (res *Result) Summary() map[string]any {
	return map[string]any{
		"scenario": res.Scenario, "title": res.Title, "scoreboard": res.Scoreboard, "checks": res.Checks,
		"reasons": res.Reasons, "finals": res.Finals, "escrows": res.Escrows, "ledger": res.Ledger,
		"probes": res.Probes, "requests": res.Requests, "wallMs": res.WallMs, "pass": res.Pass,
		"limitation": res.Limitation, "readings": len(res.Outcomes),
	}
}
