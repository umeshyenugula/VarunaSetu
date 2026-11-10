package citysim

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

// UI is the live, judge-facing control surface mounted next to the real API.
type UI struct {
	st *Stack

	mu       sync.Mutex
	hist     [][]byte
	subs     map[chan []byte]struct{}
	running  bool
	cancel   context.CancelFunc
	scenario string
	last     *Result
	matrix   []map[string]any
	tamper   *tamperInfo
}

type tamperInfo struct {
	Seq      int64  `json:"seq"`
	Original string `json:"original"`
}

// MountUI registers the /sim endpoints and the page on the stack's mux.
func (st *Stack) MountUI() *UI {
	u := &UI{st: st, subs: map[chan []byte]struct{}{}}
	st.Runner.Emit = u.emit
	m := st.Mux
	m.HandleFunc("GET /{$}", u.page)
	m.HandleFunc("GET /sim/world", u.world)
	m.HandleFunc("GET /sim/scenarios", u.scenarios)
	m.HandleFunc("GET /sim/state", u.state)
	m.HandleFunc("GET /sim/stream", u.stream)
	m.HandleFunc("POST /sim/run", u.run)
	m.HandleFunc("POST /sim/run-all", u.runAll)
	m.HandleFunc("POST /sim/control", u.control)
	m.HandleFunc("GET /sim/cell/{id}", u.cell)
	m.HandleFunc("GET /sim/last", u.lastResult)
	m.HandleFunc("GET /sim/ledger/verify", u.ledgerVerify)
	m.HandleFunc("POST /sim/ledger/tamper", u.ledgerTamper)
	m.HandleFunc("POST /sim/ledger/restore", u.ledgerRestore)
	return u
}

func (u *UI) emit(e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	u.mu.Lock()
	if e.Type == "run_start" {
		u.hist = u.hist[:0]
	}
	u.hist = append(u.hist, b)
	for ch := range u.subs {
		select {
		case ch <- b:
		default: // slow client: drop it rather than stall the simulation
			delete(u.subs, ch)
			close(ch)
		}
	}
	u.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (u *UI) page(w http.ResponseWriter, r *http.Request) {
	b, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (u *UI) world(w http.ResponseWriter, r *http.Request) {
	wd := u.st.World
	type dev struct {
		ID     string  `json:"id"`
		Cell   string  `json:"cell"`
		Lat    float64 `json:"lat"`
		Lng    float64 `json:"lng"`
		Kind   string  `json:"kind"`
		RoofM2 float64 `json:"roofM2"`
	}
	var devs []dev
	for _, d := range wd.Devices {
		devs = append(devs, dev{d.ID, d.Building.CellID, d.Building.Lat, d.Building.Lng, d.Building.Kind, d.Building.RoofM2})
	}
	cells := make([]*Cell, 0, len(wd.CellOrder))
	for _, id := range wd.CellOrder {
		cells = append(cells, wd.Cells[id])
	}
	cfg := u.st.VCfg
	writeJSON(w, 200, map[string]any{
		"lat": wd.Lat, "lng": wd.Lng, "resolution": wd.Resolution, "center": wd.CenterCell,
		"cells": cells, "devices": devs, "baselines": BaselineNames,
		"rules": map[string]any{
			"alpha": cfg.Alpha, "betaMm": cfg.BetaMM, "minPeers": cfg.MinPeers, "peerMaxAgeMin": cfg.PeerMaxAge.Minutes(),
			"outlierZ": cfg.OutlierZ, "madFloorMm": cfg.MADFloorMM, "peerRainMm": cfg.PeerRainMM, "radarDryMm": cfg.RadarDryMM,
			"escrowTtlMin": cfg.EscrowTTL.Minutes(), "neighborMinCells": cfg.NeighborMinCells, "neighborMinDevices": cfg.NeighborMinDevices,
			"windowMin": Window.Minutes(),
		},
	})
}

func (u *UI) scenarios(w http.ResponseWriter, r *http.Request) {
	var out []map[string]any
	for _, s := range Scenarios(u.st.World) {
		out = append(out, s.Meta())
	}
	writeJSON(w, 200, out)
}

func (u *UI) state(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"running": u.running, "paused": u.st.Runner.Paused(), "speed": u.st.Runner.Speed(),
		"scenario": u.scenario, "matrix": u.matrix, "tampered": u.tamper,
	})
}

func (u *UI) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := make(chan []byte, 8192)
	u.mu.Lock()
	replay := make([][]byte, len(u.hist))
	copy(replay, u.hist)
	u.subs[ch] = struct{}{}
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		if _, ok := u.subs[ch]; ok {
			delete(u.subs, ch)
			close(ch)
		}
		u.mu.Unlock()
	}()
	for _, b := range replay {
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

type runReq struct {
	Scenario string  `json:"scenario"`
	Speed    float64 `json:"speed"`
}

func (u *UI) begin(name string, speed float64) (context.Context, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.running {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.running, u.cancel, u.scenario = true, cancel, name
	u.st.Runner.SetSpeed(speed)
	u.st.Runner.SetPaused(false)
	return ctx, true
}

func (u *UI) finish() {
	u.mu.Lock()
	u.running, u.cancel = false, nil
	u.mu.Unlock()
}

func (u *UI) run(w http.ResponseWriter, r *http.Request) {
	var q runReq
	_ = json.NewDecoder(r.Body).Decode(&q)
	scn := Find(u.st.World, q.Scenario)
	if scn == nil {
		writeJSON(w, 404, map[string]string{"error": "unknown scenario"})
		return
	}
	ctx, ok := u.begin(scn.ID, q.Speed)
	if !ok {
		writeJSON(w, 409, map[string]string{"error": "a run is already in progress"})
		return
	}
	go func() {
		defer u.finish()
		res, err := u.st.Runner.Run(ctx, scn)
		if err != nil {
			u.emit(Event{"run_error", map[string]string{"error": err.Error()}})
			return
		}
		u.mu.Lock()
		u.last = res
		u.mu.Unlock()
	}()
	writeJSON(w, 202, map[string]string{"status": "started", "scenario": scn.ID})
}

func (u *UI) runAll(w http.ResponseWriter, r *http.Request) {
	var q runReq
	_ = json.NewDecoder(r.Body).Decode(&q)
	ctx, ok := u.begin("all", q.Speed)
	if !ok {
		writeJSON(w, 409, map[string]string{"error": "a run is already in progress"})
		return
	}
	u.mu.Lock()
	u.matrix = nil
	u.mu.Unlock()
	go func() {
		defer u.finish()
		list := Scenarios(u.st.World)
		for i, scn := range list {
			u.mu.Lock()
			u.scenario = scn.ID
			u.mu.Unlock()
			res, err := u.st.Runner.Run(ctx, scn)
			if err != nil {
				u.emit(Event{"run_error", map[string]string{"error": err.Error()}})
				return
			}
			row := res.Summary()
			delete(row, "probes")
			row["index"], row["total"] = i+1, len(list)
			u.mu.Lock()
			u.last = res
			u.matrix = append(u.matrix, row)
			u.mu.Unlock()
			u.emit(Event{"batch", row})
		}
		u.emit(Event{"batch_done", map[string]int{"scenarios": len(list)}})
	}()
	writeJSON(w, 202, map[string]string{"status": "started"})
}

func (u *UI) control(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Action string  `json:"action"`
		Speed  float64 `json:"speed"`
	}
	_ = json.NewDecoder(r.Body).Decode(&q)
	switch q.Action {
	case "pause":
		u.st.Runner.SetPaused(true)
	case "resume":
		u.st.Runner.SetPaused(false)
	case "speed":
		u.st.Runner.SetSpeed(q.Speed)
	case "stop":
		u.mu.Lock()
		if u.cancel != nil {
			u.cancel()
		}
		u.mu.Unlock()
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown action"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (u *UI) cell(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	now := u.st.Clock.Now()
	cfg := u.st.VCfg
	self, err := u.st.Repo.CellRainAt(r.Context(), u.st.DB, id, now, cfg)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	nbs, met, err := u.st.Repo.NeighborEvidence(r.Context(), id, now, cfg, u.st.Server.NeighborK())
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"cell": self, "neighbors": nbs, "ruleMet": met, "asOf": now,
		"needCells": cfg.NeighborMinCells, "needDevices": cfg.NeighborMinDevices, "rainMm": cfg.PeerRainMM,
	})
}

func (u *UI) lastResult(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	res := u.last
	u.mu.Unlock()
	if res == nil {
		writeJSON(w, 404, map[string]string{"error": "no run yet"})
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=varunasetu-"+res.Scenario+".json")
	writeJSON(w, 200, res)
}

func (u *UI) ledgerVerify(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, u.st.Runner.verifyLedger(r.Context()))
}

// ledgerTamper rewrites one audit entry directly in the database, the way a
// privileged insider could, so the demo can show the hash chain catching it.
func (u *UI) ledgerTamper(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tamper != nil {
		writeJSON(w, 409, map[string]string{"error": "already tampered; restore first"})
		return
	}
	var seq int64
	var orig string
	err := u.st.DB.QueryRowContext(r.Context(), `SELECT seq, event_ref FROM audit_ledger_entry ORDER BY seq DESC OFFSET 3 LIMIT 1`).Scan(&seq, &orig)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "not enough ledger entries yet; run a scenario first"})
		return
	}
	if _, err := u.st.DB.ExecContext(r.Context(), `UPDATE audit_ledger_entry SET event_ref='TAMPERED:'||event_ref WHERE seq=$1`, seq); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	u.tamper = &tamperInfo{Seq: seq, Original: orig}
	writeJSON(w, 200, u.tamper)
}

func (u *UI) ledgerRestore(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tamper == nil {
		writeJSON(w, 409, map[string]string{"error": "nothing to restore"})
		return
	}
	if _, err := u.st.DB.ExecContext(r.Context(), `UPDATE audit_ledger_entry SET event_ref=$2 WHERE seq=$1`, u.tamper.Seq, u.tamper.Original); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	u.tamper = nil
	writeJSON(w, 200, map[string]string{"status": "restored"})
}
