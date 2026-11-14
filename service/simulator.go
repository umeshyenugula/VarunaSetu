package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"hydroledger-prototype/hydroledger/verifier/spatial"
)

// SimulatorConfig enables gated demo endpoints. Off by default.
type SimulatorConfig struct {
	Enabled bool
}

// SimulatorCase describes a judge-facing verification scenario.
type SimulatorCase struct {
	ID                string         `json:"id"`
	Title             string         `json:"title"`
	Category          string         `json:"category"`
	Scenario          string         `json:"scenario"`
	Expected          string         `json:"expected"`
	ExistingGap       string         `json:"existingGap"`
	HowHydroLedger    string         `json:"howHydroLedger"`
	H3Role            string         `json:"h3Role"`
	APICalls          []string       `json:"apiCalls"`
	Notes             string         `json:"notes,omitempty"`
	AutoRunnable      bool           `json:"autoRunnable"`
	RequiresTimeLapse bool           `json:"requiresTimeLapse,omitempty"`
	Spec              map[string]any `json:"spec,omitempty"`
}

type provisionDevice struct {
	DeviceID          string  `json:"deviceId"`
	BuildingExternal  string  `json:"buildingId"`
	H3Index           string  `json:"h3Index"`
	PublicKeyBase64   string  `json:"publicKeyBase64"`
	PrivateSeedBase64 string  `json:"privateSeedBase64"`
	RoofAreaM2        float64 `json:"roofAreaM2"`
	RunoffCoeff       float64 `json:"runoffCoeff"`
	CapacityLitres    float64 `json:"capacityLitres"`
}

type provisionResponse struct {
	Simulated     bool              `json:"simulated"`
	CityName      string            `json:"cityName"`
	H3Resolution  int               `json:"h3Resolution"`
	Cells         []string          `json:"cells"`
	Buildings     []map[string]any `json:"buildings"`
	Devices       []provisionDevice `json:"devices"`
	Cases         []SimulatorCase   `json:"cases"`
	SignerContract struct {
		Domain        string   `json:"domain"`
		Encoding      string   `json:"encoding"`
		Fields        []string `json:"fields"`
		SignatureB64  string   `json:"signatureB64"`
		NonceFormat   string   `json:"nonceFormat"`
		TimestampNote string   `json:"timestampNote"`
	} `json:"signerContract"`
}

func (s *Server) registerSimulatorRoutes(mux *http.ServeMux) {
	if !s.cfg.Simulator.Enabled {
		return
	}
	mux.HandleFunc("GET /simulator/config", s.simulatorConfig)
	mux.HandleFunc("GET /simulator/cases", s.simulatorCases)
	mux.HandleFunc("POST /simulator/provision", s.simulatorProvision)
	mux.HandleFunc("POST /simulator/reset", s.simulatorReset)
	mux.HandleFunc("POST /simulator/expire-escrows", s.simulatorExpireEscrows)
}

func (s *Server) simulatorEnabled(w http.ResponseWriter) bool {
	if !s.cfg.Simulator.Enabled {
		writeError(w, http.StatusNotFound, "simulator api disabled")
		return false
	}
	return true
}

func (s *Server) simulatorConfig(w http.ResponseWriter, r *http.Request) {
	if !s.simulatorEnabled(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":      true,
		"simulated":    true,
		"h3Resolution": spatial.DefaultResolution,
		"timestampWindowSeconds": 300,
		"escrowTTLSeconds":       int(s.cfg.Verifier.Config().EscrowTTL.Seconds()),
		"notes":                  "Demo endpoints provision sim-* data and use the real verification engine. Real telemetry endpoint requires valid Ed25519 signatures and recent timestamps.",
	})
}

func (s *Server) simulatorCases(w http.ResponseWriter, r *http.Request) {
	if !s.simulatorEnabled(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"simulated": true, "cases": defaultSimulatorCases()})
}

func defaultSimulatorCases() []SimulatorCase {
	return []SimulatorCase{
		{ID: "h3-network", Category: "H3 Network", Title: "H3 cell indexing and neighbourhood context", Scenario: "Buildings and devices are mapped to H3 resolution 9 cells; the same event is evaluated in cell context.", Expected: "Each reading carries h3_index; dashboard renders H3 polygons and cell states from /cells.", ExistingGap: "Static meter registries and flat databases usually store addresses/IDs without a uniform spatial index for peer-neighbour reasoning.", HowHydroLedger: "Uses canonical H3 cell IDs for peer grouping, cell summaries, and spatial visualization.", H3Role: "H3 is the spatial key for peer windows and cell status.", APICalls: []string{"GET /cells", "GET /cells/{h3_index}/status", "GET /telemetry/readings"}, AutoRunnable: true},
		{ID: "accept-consensus", Category: "Verification", Title: "Honest rainfall: radar and peers agree", Scenario: "Multiple devices in the same H3 cell report physically plausible runoff consistent with radar and peer median.", Expected: "ACCEPT / RADAR_AND_PEERS_AGREE (or physics fallback path when peers are sparse).", ExistingGap: "Traditional AMI/manual systems often trust isolated meter totals and lack cross-sensor spatial consensus.", HowHydroLedger: "Computes R_obs vs radar bound and robust peer median/MAD consensus before accepting.", H3Role: "Peers are selected from the same H3 cell and recent time window.", APICalls: []string{"POST /telemetry/readings", "GET /telemetry/readings"}, AutoRunnable: true},
		{ID: "spatial-outlier", Category: "Tamper/Fault", Title: "Spatial peer outlier", Scenario: "One device claims an abnormally high volume compared with coherent neighbours under similar radar.", Expected: "QUARANTINE / SPATIAL_PEER_OUTLIER.", ExistingGap: "Single-device validation misses local anomalies that are obvious relative to neighbouring sensors.", HowHydroLedger: "Uses robust z-score over same-cell peer depths; positive outliers are quarantined.", H3Role: "Same H3 cell provides the peer comparison set.", APICalls: []string{"POST /telemetry/readings"}, AutoRunnable: true},
		{ID: "physical-bound", Category: "Physics", Title: "Physical/radar bound exceeded", Scenario: "A claim exceeds the radar-informed physical upper bound for the roof/runoff parameters.", Expected: "QUARANTINE / PHYSICAL_BOUND_EXCEEDED (when enough peers exist and outlier rule does not preempt).", ExistingGap: "Billing systems may accept impossible volumes until manual audit.", HowHydroLedger: "Checks R_obs against R_max = Alpha*RadarMM + BetaMM.", H3Role: "H3 groups peers; physics bound applies per reading.", APICalls: []string{"POST /telemetry/readings"}, AutoRunnable: true},
		{ID: "radar-peer-conflict", Category: "Escrow", Title: "Radar dry, coherent peers report rain", Scenario: "Local peers coherently indicate rainfall while radar is dry/low.", Expected: "PROVISIONAL_ESCROW / RADAR_PEER_CONFLICT_ESCROW.", ExistingGap: "Binary accept/reject systems force a premature decision when evidence conflicts.", HowHydroLedger: "Creates a durable escrow record instead of immediate accept/quarantine.", H3Role: "Conflict is detected within the same H3 cell peer group.", APICalls: []string{"POST /telemetry/readings", "GET /escrows"}, AutoRunnable: true},
		{ID: "escrow-corroborated", Category: "Escrow", Title: "Escrow corroborated by later wet radar", Scenario: "After a provisional escrow, a later same-cell reading with wet radar arrives within TTL.", Expected: "Pending escrow transitions to CORROBORATED; ledger/audit events recorded.", ExistingGap: "Systems without evidence workflows cannot safely resolve ambiguous measurements.", HowHydroLedger: "Durable repository corroborates eligible pending escrows in the same H3 cell when corroborating radar evidence arrives.", H3Role: "Corroboration is scoped to the same h3_index.", APICalls: []string{"POST /telemetry/readings", "GET /escrows", "GET /ledger/entries"}, AutoRunnable: true},
		{ID: "escrow-expired", Category: "Escrow", Title: "Escrow expires without corroboration", Scenario: "A provisional escrow reaches expiry with no corroborating evidence.", Expected: "EXPIRED_QUARANTINED and reserved capacity released (durable expiry worker/sweep).", ExistingGap: "Manual holds often lack deterministic expiry and auditability.", HowHydroLedger: "Repository ExpireEscrows marks pending escrows expired and writes RELEASE ledger/audit entries.", H3Role: "Escrow is tied to a reading/cell; expiry is durable.", APICalls: []string{"GET /escrows", "POST /simulator/expire-escrows"}, AutoRunnable: true, RequiresTimeLapse: true, Notes: "Event-time escrow TTL is 15 minutes in defaults. The gated demo endpoint can run an expiry sweep with an advanced clock for judge demos."},
		{ID: "capacity-exceeded", Category: "Integrity", Title: "Storage/catchment/capacity exceeded", Scenario: "Claimed or stored volume exceeds building/tank capacity constraints.", Expected: "QUARANTINE / STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED or repository capacity reservation failure.", ExistingGap: "Many systems do not bind telemetry claims to physical asset capacity.", HowHydroLedger: "Verifier and durable repository enforce capacity constraints.", H3Role: "Orthogonal to H3; combined with spatial verification in the same pipeline.", APICalls: []string{"POST /telemetry/readings"}, AutoRunnable: true},
		{ID: "identity-replay", Category: "Security", Title: "Signed identity, idempotency, and replay handling", Scenario: "Telemetry is Ed25519-signed by enrolled devices; duplicate nonce+payload is idempotent; nonce reuse with different payload conflicts.", Expected: "Unauthorized for bad/missing auth; 200 for identical idempotent replay; 409 for conflicting nonce reuse.", ExistingGap: "Unsigned/manual ingestion can be spoofed or duplicated without cryptographic provenance.", HowHydroLedger: "Canonical signed envelope (domain-separated, length-prefixed), enrolled key lookup, timestamp window, durable idempotency.", H3Role: "Identity protects the reading whose h3_index drives spatial logic.", APICalls: []string{"POST /telemetry/readings"}, AutoRunnable: true},
		{ID: "audit-ledger", Category: "Audit", Title: "Hash-chained audit ledger", Scenario: "Stored readings and escrow transitions append audit events.", Expected: "GET /ledger/entries shows chained hashes/event refs when migration 002 is applied.", ExistingGap: "Mutable logs make post-incident tampering hard to detect.", HowHydroLedger: "Append-only hash-linked audit entries provide tamper evidence (not a substitute for trusted checkpoints/admin controls).", H3Role: "Audit records include events originating from cell-scoped telemetry/escrow processing.", APICalls: []string{"GET /ledger/entries"}, AutoRunnable: true},
		{ID: "neighbor-corroboration", Category: "H3 Network", Title: "Neighbor-cell corroboration (engine capability)", Scenario: "In-memory verifier can corroborate escrows from configured H3 neighbor cells with sufficient rain evidence.", Expected: "Verifier supports SetNeighbors/neighbor corroboration; durable service currently applies radar corroboration and expiry, not wired H3 neighbor geometry.", ExistingGap: "Non-spatial systems cannot use nearby-cell evidence when a single cell is ambiguous.", HowHydroLedger: "Spatial package computes k-ring neighbors; verifier resolves escrow with neighbor evidence when configured.", H3Role: "H3 k-ring provides neighbour cells. Demo UI explains current durable-service status accurately.", APICalls: []string{"GET /cells"}, AutoRunnable: false, Notes: "Displayed as an explained capability/roadmap case because durable HTTP service does not wire neighbor SetNeighbors in the ingestion path."},
	}
}

func (s *Server) simulatorProvision(w http.ResponseWriter, r *http.Request) {
	if !s.simulatorEnabled(w) {
		return
	}
	ctx := r.Context()
	resp, err := s.provisionVirtualCity(ctx)
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		s.cfg.Logger.Error("simulator provision failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not provision simulator: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) provisionVirtualCity(ctx context.Context) (*provisionResponse, error) {
	if err := s.cfg.Repository.ensureDB(); err != nil {
		return nil, err
	}
	db := s.cfg.Repository.DB
	// Demo H3 cells around a real coordinate (Hyderabad) for a convincing virtual city.
	lat, lng := 17.385044, 78.486671
	center, err := spatial.CellAt(lat, lng, spatial.DefaultResolution)
	if err != nil {
		return nil, err
	}
	cells := []string{center}
	neighbors, err := spatial.NeighborsOf(center, 1)
	if err == nil && len(neighbors) > 0 {
		if len(neighbors) > 3 {
			neighbors = neighbors[:3]
		}
		cells = append(cells, neighbors...)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Verification decisions and audit entries are immutable. Each provision
	// therefore gets a fresh namespace instead of deleting prior simulator data.
	runIDBytes := make([]byte, 8)
	if _, err := rand.Read(runIDBytes); err != nil {
		return nil, fmt.Errorf("generate simulator run id: %w", err)
	}
	runID := base64.RawURLEncoding.EncodeToString(runIDBytes)

	type bspec struct {
		ext, name, cell string
		roof, runoff    float64
		cap             float64
	}
	buildings := []bspec{
		{"sim-bld-001", "Residency A", cells[0], 100, 0.8, 100000},
		{"sim-bld-002", "Residency B", cells[0], 120, 0.8, 100000},
		{"sim-bld-003", "Residency C", cells[0], 90, 0.75, 100000},
		{"sim-bld-004", "Residency D", cells[0], 110, 0.8, 100000},
		{"sim-bld-005", "Residency E", cells[0], 95, 0.78, 100000},
	}
	if len(cells) > 1 {
		buildings = append(buildings,
			bspec{"sim-bld-006", "Market Block", cells[1], 260, 0.75, 150000},
			bspec{"sim-bld-007", "School Roof", cells[min(2, len(cells)-1)], 300, 0.7, 150000},
		)
	}
	resp := &provisionResponse{Simulated: true, CityName: "VarunaSetu Virtual City", H3Resolution: spatial.DefaultResolution, Cells: cells}
	resp.SignerContract.Domain = "hydroledger/identity/telemetry/v1"
	resp.SignerContract.Encoding = "length-prefixed big-endian: domain+0x00, uint32 device_id, int64 unix_sec + uint32 nsec, uint32 nonce, uint32 payload"
	resp.SignerContract.Fields = []string{"device_id", "timestamp", "nonce", "payload"}
	resp.SignerContract.SignatureB64 = "base64 RawStdEncoding (standard base64, no URL alphabet requirement in tests; service decodes RawStdEncoding)"
	resp.SignerContract.NonceFormat = "unique per device; 16 random bytes base64url-no-padding recommended"
	resp.SignerContract.TimestampNote = "Must be within service timestamp window (default 5m past, 30s future). Use wall-clock time for live API submissions."

	for i, b := range buildings {
		buildingExternal := fmt.Sprintf("%s-%s", b.ext, runID)
		if _, err := tx.ExecContext(ctx, `INSERT INTO building (external_id, capacity_litres, reserved_litres) VALUES ($1,$2,0)`, buildingExternal, b.cap); err != nil {
			return nil, err
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		seed := priv.Seed()
		devID := fmt.Sprintf("sim-dev-%s-%03d", runID, i+1)
		var buildingID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM building WHERE external_id=$1`, buildingExternal).Scan(&buildingID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device (id, building_id, public_key, revoked_at) VALUES ($1,$2,$3,NULL)`, devID, buildingID, []byte(pub)); err != nil {
			return nil, err
		}
		resp.Buildings = append(resp.Buildings, map[string]any{"buildingId": buildingExternal, "name": b.name, "h3Index": b.cell, "roofAreaM2": b.roof, "runoffCoeff": b.runoff, "capacityLitres": b.cap})
		resp.Devices = append(resp.Devices, provisionDevice{DeviceID: devID, BuildingExternal: buildingExternal, H3Index: b.cell, PublicKeyBase64: base64.StdEncoding.EncodeToString(pub), PrivateSeedBase64: base64.StdEncoding.EncodeToString(seed), RoofAreaM2: b.roof, RunoffCoeff: b.runoff, CapacityLitres: b.cap})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	resp.Cases = defaultSimulatorCases()
	return resp, nil
}

func (s *Server) simulatorReset(w http.ResponseWriter, r *http.Request) {
	if !s.simulatorEnabled(w) {
		return
	}
	ctx := r.Context()
	db := s.cfg.Repository.DB
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reset failed")
		return
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM device d WHERE d.id LIKE 'sim-dev-%' AND NOT EXISTS (SELECT 1 FROM telemetry_reading tr WHERE tr.device_id=d.id)`,
		`DELETE FROM building b WHERE b.external_id LIKE 'sim-bld-%' AND NOT EXISTS (SELECT 1 FROM device d WHERE d.building_id=b.id)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			s.cfg.Logger.Error("simulator reset step failed", "error", err, "query", q)
			writeError(w, http.StatusInternalServerError, "reset failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "reset failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"simulated": true, "reset": true})
}

func (s *Server) simulatorExpireEscrows(w http.ResponseWriter, r *http.Request) {
	if !s.simulatorEnabled(w) {
		return
	}
	// Gated demo helper: run expiry sweep as-of an advanced time so judges can observe expiry.
	type req struct {
		AdvanceMinutes int `json:"advanceMinutes"`
	}
	var body req
	_ = decodeStrict(r, &body) // optional
	advance := time.Duration(body.AdvanceMinutes) * time.Minute
	if advance <= 0 {
		advance = 20 * time.Minute
	}
	now := s.now().Add(advance)
	if err := s.cfg.Repository.ExpireEscrows(r.Context(), now); err != nil {
		writeError(w, http.StatusInternalServerError, "expire failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"simulated": true, "expiredAsOf": now, "advanceMinutes": int(advance.Minutes())})
}

