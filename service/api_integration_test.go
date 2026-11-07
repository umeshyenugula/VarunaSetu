package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/identity"
)

func setupIntegration(t *testing.T) (*testDB, *testEnv, http.Handler, string) {
	t.Helper()
	if testing.Short() && os.Getenv("HYDROLEDGER_TEST_DATABASE_URL") == "" {
		// allow short only when explicitly using external DB? still try docker; skip handled in openTestDB
	}
	tdb := openTestDB(t)
	applyMigrations(t, tdb.DB)
	resetSchema(t, tdb.DB)
	cell := testCell(t)
	env := newTestEnv(t, tdb.DB)
	seedBuilding(t, tdb.DB, "bldg-1", 100000)
	kp := mustKey(t, "dev-1")
	if err := env.Registry.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, tdb.DB, kp.DeviceID(), "bldg-1", kp.PublicKey())
	return tdb, env, env.Server.Handler(), cell
}

func TestMigrationsExecute(t *testing.T) {
	tdb := openTestDB(t)
	applyMigrations(t, tdb.DB)
	// idempotent re-run
	applyMigrations(t, tdb.DB)
	var n int
	if err := tdb.DB.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('building','device','telemetry_reading','verification_decision','escrow_record','ledger_entry')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("expected 6 service tables, got %d", n)
	}
}

func TestPostTelemetry_DuplicateDelayedReorderedMalformed(t *testing.T) {
	_, env, h, cell := setupIntegration(t)
	kp := mustKey(t, "dev-dup")
	if err := env.Registry.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, env.DB, kp.DeviceID(), "bldg-1", kp.PublicKey())

	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.setNow(base.Add(30 * time.Second))

	// malformed JSON
	r := httptest.NewRequest(http.MethodPost, "/telemetry/readings", bytes.NewBufferString("{"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed json: got %d %s", w.Code, w.Body.String())
	}

	// malformed: unknown field (strict)
	nonce, _ := identity.NewNonce()
	req := signRequest(t, kp, base, nonce, testTelemetry(cell, 1))
	body, _ := json.Marshal(req)
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	raw["extra"] = true
	body2, _ := json.Marshal(raw)
	r = httptest.NewRequest(http.MethodPost, "/telemetry/readings", bytes.NewReader(body2))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: got %d %s", w.Code, w.Body.String())
	}

	// valid request
	resp, out := postTelemetry(t, h, req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d %+v", resp.StatusCode, out)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("missing id: %+v", out)
	}

	// duplicate same payload => 200 idempotent
	resp2, out2 := postTelemetry(t, h, req)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("duplicate: got %d %+v", resp2.StatusCode, out2)
	}
	if out2["id"] != id {
		t.Fatalf("duplicate id mismatch: %v vs %v", out2["id"], id)
	}

	// same nonce different payload => 409
	req2 := req
	t2 := testTelemetry(cell, 2)
	p2, _ := json.Marshal(t2)
	env2, err := kp.Signer().Sign(base, nonce, p2)
	if err != nil {
		t.Fatal(err)
	}
	req2.Payload = json.RawMessage(p2)
	req2.Signature = base64.RawStdEncoding.EncodeToString(env2.Signature)
	resp3, _ := postTelemetry(t, h, req2)
	if resp3.StatusCode != http.StatusConflict {
		t.Fatalf("idempotency conflict: got %d", resp3.StatusCode)
	}

	// delayed/reordered: post earlier event time after later? Use different devices/nonces.
	// Reordered arrival: submit t2 before t1 to service; both should persist if signed within window.
	kp1 := mustKey(t, "dev-r1")
	kp2 := mustKey(t, "dev-r2")
	for _, k := range []*identity.KeyPair{kp1, kp2} {
		if err := env.Registry.Enroll(k.DeviceID(), k.PublicKey()); err != nil {
			t.Fatal(err)
		}
		seedDevice(t, env.DB, k.DeviceID(), "bldg-1", k.PublicKey())
	}
	t1 := base.Add(2 * time.Minute)
	t0 := base.Add(1 * time.Minute)
	env.setNow(base.Add(3 * time.Minute))
	n2, _ := identity.NewNonce()
	n1, _ := identity.NewNonce()
	respA, _ := postTelemetry(t, h, signRequest(t, kp2, t1, n2, testTelemetry(cell, 1)))
	respB, _ := postTelemetry(t, h, signRequest(t, kp1, t0, n1, testTelemetry(cell, 1)))
	if respA.StatusCode != http.StatusCreated || respB.StatusCode != http.StatusCreated {
		t.Fatalf("reordered: %d %d", respA.StatusCode, respB.StatusCode)
	}

	// invalid H3
	bad := testTelemetry("not-an-h3", 1)
	nb, _ := identity.NewNonce()
	respBad, _ := postTelemetry(t, h, signRequest(t, mustKeyForEnrolled(t, env, "dev-bad"), base, nb, bad))
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid h3: got %d", respBad.StatusCode)
	}

	// GET reading
	respG, outG := doJSON(t, h, http.MethodGet, "/telemetry/readings/"+id)
	if respG.StatusCode != http.StatusOK || outG["id"] != id {
		t.Fatalf("get reading: %d %+v", respG.StatusCode, outG)
	}
	resp404, _ := doJSON(t, h, http.MethodGet, "/telemetry/readings/00000000-0000-0000-0000-000000000000")
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing: %d", resp404.StatusCode)
	}

	// GET cell status
	respC, outC := doJSON(t, h, http.MethodGet, "/cells/"+cell+"/status")
	if respC.StatusCode != http.StatusOK {
		t.Fatalf("cell status: %d %+v", respC.StatusCode, outC)
	}
	if _, ok := outC["recent_devices"]; !ok {
		t.Fatalf("cell status missing recent_devices: %+v", outC)
	}
	respC2, _ := doJSON(t, h, http.MethodGet, "/cells/not-h3/status")
	if respC2.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad cell status: %d", respC2.StatusCode)
	}
}

func mustKeyForEnrolled(t *testing.T, env *testEnv, id string) *identity.KeyPair {
	t.Helper()
	kp := mustKey(t, id)
	if err := env.Registry.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, env.DB, kp.DeviceID(), "bldg-1", kp.PublicKey())
	return kp
}

func TestPeerRetrievalSuppliesDBPeers(t *testing.T) {
	_, env, _, cell := setupIntegration(t)
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	vcfg := verifier.DefaultConfig()

	// Insert 4 peer readings directly so DB-backed seeding sees them.
	for i := 0; i < 4; i++ {
		id := "dev-p" + string(rune('a'+i))
		kp := mustKey(t, id)
		if err := env.Registry.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
			t.Fatal(err)
		}
		seedDevice(t, env.DB, kp.DeviceID(), "bldg-1", kp.PublicKey())
		tel := testTelemetry(cell, 5.0)
		tel.ClaimedLitres = 40.0 // 5mm effective with roof 10, runoff 0.8
		payload, _ := json.Marshal(tel)
		hash := sha256Test(payload)
		_, err := env.Repo.StoreReading(context.Background(), id, "n-"+id, base.Add(time.Duration(i)*time.Second), tel,
			verifier.Decision{State: verifier.StateAccept, Reason: verifier.ReasonRadarAndPeersAgree}, base.Add(time.Hour), hash, vcfg)
		if err != nil {
			t.Fatalf("seed peer %d: %v", i, err)
		}
	}

	targetID := "dev-target"
	kpT := mustKey(t, targetID)
	if err := env.Registry.Enroll(kpT.DeviceID(), kpT.PublicKey()); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, env.DB, kpT.DeviceID(), "bldg-1", kpT.PublicKey())
	target := testTelemetry(cell, 5.0)
	target.ClaimedLitres = 40.0
	reading := target.Reading(targetID, base.Add(10*time.Second))
	d, err := env.Server.evaluateWithPeers(context.Background(), vcfg, reading)
	if err != nil {
		t.Fatal(err)
	}
	if d.PeerCount < 4 {
		t.Fatalf("expected DB-backed peers >=4, got %+v", d)
	}
	if d.State != verifier.StateAccept {
		t.Fatalf("expected accept with agreeing peers, got %+v", d)
	}
}

func sha256Test(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

var _ = errors.New

func TestEscrowExpiryAndRace(t *testing.T) {
	_, env, _, cell := setupIntegration(t)
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Create reading + escrow rows directly.
	readingID := insertReadingRow(t, env.DB, "dev-e1", "bldg-1", base, testTelemetry(cell, 0), "k-e1")
	escrowID := insertEscrowRow(t, env.DB, readingID, base.Add(-time.Minute)) // already expired

	if err := env.Repo.ExpireEscrows(ctx, base); err != nil {
		t.Fatal(err)
	}
	e, err := env.Repo.GetEscrow(ctx, escrowID)
	if err != nil {
		t.Fatal(err)
	}
	if e.State != "EXPIRED_QUARANTINED" || e.ResolvedAt == nil {
		t.Fatalf("expected expired: %+v", e)
	}

	// idempotent/re-run
	if err := env.Repo.ExpireEscrows(ctx, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	e2, _ := env.Repo.GetEscrow(ctx, escrowID)
	if e2.State != "EXPIRED_QUARANTINED" {
		t.Fatalf("state changed on rerun: %+v", e2)
	}

	// Race: expiry vs corroboration on a fresh pending escrow.
	readingID2 := insertReadingRow(t, env.DB, "dev-e2", "bldg-1", base, testTelemetry(cell, 0), "k-e2")
	expAt := base.Add(50 * time.Millisecond)
	escrowID2 := insertEscrowRow(t, env.DB, readingID2, expAt)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		time.Sleep(55 * time.Millisecond)
		errs[0] = env.Repo.ExpireEscrows(context.Background(), base.Add(100*time.Millisecond))
	}()
	go func() {
		defer wg.Done()
		<-start
		errs[1] = env.Repo.CorroborateEscrow(context.Background(), escrowID2, base.Add(40*time.Millisecond))
	}()
	close(start)
	wg.Wait()
	for i, e := range errs {
		if e != nil && !errors.Is(e, ErrNotFound) {
			t.Fatalf("race err %d: %v", i, e)
		}
	}
	final, err := env.Repo.GetEscrow(ctx, escrowID2)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "CORROBORATED" && final.State != "EXPIRED_QUARANTINED" {
		t.Fatalf("invalid terminal state: %+v", final)
	}
	if final.ResolvedAt == nil {
		t.Fatalf("missing resolved_at: %+v", final)
	}
}

func TestCrashAndRecovery(t *testing.T) {
	_, env, _, cell := setupIntegration(t)
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Transaction atomicity: failing StoreReading (bad building) leaves no partial rows.
	vcfg := verifier.DefaultConfig()
	tel := testTelemetry(cell, 1)
	tel.BuildingID = "missing-building"
	hash := []byte("12345678901234567890123456789012")
	_, err := env.Repo.StoreReading(ctx, "dev-1", "crash-k", base, tel, verifier.Decision{State: verifier.StateAccept}, base.Add(time.Hour), hash, vcfg)
	if err == nil {
		t.Fatal("expected error")
	}
	assertCount(t, env.DB, `SELECT count(*) FROM telemetry_reading WHERE idempotency_key='crash-k'`, 0)

	// Recovery: pending expired escrow remains resolvable; a fresh repository
	// view (simulating restart) must transition it deterministically.
	readingID := insertReadingRow(t, env.DB, "dev-rec", "bldg-1", base, testTelemetry(cell, 0), "k-rec")
	escrowID := insertEscrowRow(t, env.DB, readingID, base.Add(-time.Minute))
	repo2 := &Repository{DB: env.DB}
	if err := repo2.ExpireEscrows(ctx, base); err != nil {
		t.Fatalf("recovery expire: %v", err)
	}
	e, err := repo2.GetEscrow(ctx, escrowID)
	if err != nil {
		t.Fatalf("recovery get: %v", err)
	}
	if e.State != "EXPIRED_QUARANTINED" {
		t.Fatalf("recovery state: %+v", e)
	}
}

func TestDurableRadarCorroboration(t *testing.T) {
	_, env, _, cell := setupIntegration(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	vcfg := verifier.DefaultConfig()

	// pending escrow from t0
	readingID := insertReadingRow(t, env.DB, "dev-c1", "bldg-1", base, testTelemetry(cell, 0), "k-c1")
	escrowID := insertEscrowRow(t, env.DB, readingID, base.Add(vcfg.EscrowTTL))

	// corroborating reading arrives later with wet radar; StoreReading applies durable corroboration.
	kp := mustKeyForEnrolled(t, env, "dev-c2")
	later := base.Add(2 * time.Minute)
	tel := testTelemetry(cell, 5.0)
	d := verifier.Decision{State: verifier.StateAccept, Reason: verifier.ReasonRadarAndPeersAgree}
	payload, _ := json.Marshal(tel)
	hash := sha256Test(payload)
	_, err := env.Repo.StoreReading(ctx, kp.DeviceID(), "k-c2", later, tel, d, later.Add(vcfg.EscrowTTL), hash, vcfg)
	if err != nil {
		t.Fatal(err)
	}
	e, err := env.Repo.GetEscrow(ctx, escrowID)
	if err != nil {
		t.Fatal(err)
	}
	if e.State != "CORROBORATED" {
		t.Fatalf("expected corroborated escrow, got %+v", e)
	}
}

func insertReadingRow(t *testing.T, db *sql.DB, deviceID, buildingExt string, ts time.Time, tel Telemetry, key string) string {
	t.Helper()
	// ensure device/building exist
	kp := mustKey(t, deviceID)
	var pub []byte = kp.PublicKey()
	_, _ = db.Exec(`INSERT INTO device (id,building_id,public_key) SELECT $1,b.id,$3 FROM building b WHERE b.external_id=$2 ON CONFLICT (id) DO NOTHING`, deviceID, buildingExt, pub)
	payload, _ := json.Marshal(tel)
	var id string
	err := db.QueryRow(`INSERT INTO telemetry_reading (idempotency_key,device_id,building_id,event_time,claimed_litres,roof_area_m2,runoff_coeff,stored_litres,capacity_litres,radar_mm,h3_index,payload_hash)
		SELECT $1,$2,b.id,$3,$4,$5,$6,$7,$8,$9,$10,$11 FROM building b WHERE b.external_id=$12 RETURNING id`,
		key, deviceID, ts, tel.ClaimedLitres, tel.RoofAreaM2, tel.RunoffCoeff, tel.StoredLitres, tel.CapacityLitres, tel.RadarMM, tel.CellID, payload, buildingExt).Scan(&id)
	if err != nil {
		t.Fatalf("insert reading row: %v", err)
	}
	_, err = db.Exec(`INSERT INTO verification_decision (reading_id,state,reason) VALUES ($1,'PROVISIONAL_ESCROW','RADAR_PEER_CONFLICT_ESCROW')`, id)
	if err != nil {
		t.Fatalf("insert decision row: %v", err)
	}
	return id
}

func insertEscrowRow(t *testing.T, db *sql.DB, readingID string, expiresAt time.Time) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO escrow_record (reading_id,expires_at) VALUES ($1,$2) RETURNING id`, readingID, expiresAt).Scan(&id); err != nil {
		t.Fatalf("insert escrow: %v", err)
	}
	return id
}

func assertCount(t *testing.T, db *sql.DB, q string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(q).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s: got %d want %d", q, got, want)
	}
}

type loadResult struct {
	Devices    int     `json:"concurrent_devices"`
	Requests   int     `json:"requests"`
	P50ms      float64 `json:"p50_ms"`
	P95ms      float64 `json:"p95_ms"`
	P99ms      float64 `json:"p99_ms"`
	Throughput float64 `json:"throughput_rps"`
	DBWriteP95 float64 `json:"db_write_p95_ms"`
	CPUGOMAX   int     `json:"gomaxprocs"`
	AllocMB    float64 `json:"alloc_mb"`
}

// --- Load test (Go benchmark-style harness writing results) ---

func TestLoadService(t *testing.T) {
	if os.Getenv("HYDROLEDGER_RUN_LOAD") == "" {
		t.Skip("set HYDROLEDGER_RUN_LOAD=1 to run service load test")
	}
	tdb := openTestDB(t)
	applyMigrations(t, tdb.DB)
	resetSchema(t, tdb.DB)
	cell := testCell(t)
	seedBuilding(t, tdb.DB, "bldg-load", 1e12)
	env := newTestEnv(t, tdb.DB)
	h := env.Server.Handler()

	results := []loadResult{}
	for _, devices := range []int{1, 4, 16} {
		// fresh schema per run to avoid capacity/id collisions
		resetSchema(t, tdb.DB)
		seedBuilding(t, tdb.DB, "bldg-load", 1e12)
		env = newTestEnv(t, tdb.DB)
		h = env.Server.Handler()
		r := runLoad(t, env, h, cell, devices, 40)
		results = append(results, r)
	}
	info := envInfo()
	pgVer := ""
	_ = tdb.DB.QueryRow(`SELECT version()`).Scan(&pgVer)
	postgisVer := ""
	_ = tdb.DB.QueryRow(`SELECT PostGIS_Version()`).Scan(&postgisVer)
	out := map[string]any{
		"environment": info,
		"postgres":    pgVer,
		"postgis":     postgisVer,
		"note":        "End-to-end service latency (signed HTTP -> DB-backed peer eval -> transactional persist). Separate from verifier microbenchmarks.",
		"results":     results,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	path := filepathJoinRepo("docs", "results", "phase3", "load_results.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Logf("write load results: %v\n%s", err, string(b))
	} else {
		t.Logf("wrote %s", path)
	}
}

func runLoad(t *testing.T, env *testEnv, h http.Handler, cell string, devices, perDevice int) loadResult {
	t.Helper()
	base := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.setNow(base.Add(30 * time.Minute))
	var keys []*identity.KeyPair
	for i := 0; i < devices; i++ {
		id := fmtSprintf("dev-load-%d-%d", devices, i)
		kp := mustKeyForEnrolled(t, env, id)
		keys = append(keys, kp)
	}
	latencies := make([]time.Duration, 0, devices*perDevice)
	dbWrites := make([]time.Duration, 0, devices*perDevice)
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()
	for i, kp := range keys {
		wg.Add(1)
		go func(i int, kp *identity.KeyPair) {
			defer wg.Done()
			for j := 0; j < perDevice; j++ {
				ts := base.Add(time.Duration(i*perDevice+j) * time.Second)
				env.setNow(ts.Add(5 * time.Second))
				nonce, _ := identity.NewNonce()
				req := signRequest(t, kp, ts, nonce, Telemetry{BuildingID: "bldg-load", ClaimedLitres: 1, RoofAreaM2: 10, RunoffCoeff: 0.8, StoredLitres: 0, CapacityLitres: 1e12, RadarMM: 1, CellID: cell})
				t0 := time.Now()
				resp, _ := postTelemetry(t, h, req)
				dur := time.Since(t0)
				if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
					// contention/retry noise acceptable in load harness
				}
				mu.Lock()
				latencies = append(latencies, dur)
				dbWrites = append(dbWrites, dur)
				mu.Unlock()
			}
		}(i, kp)
	}
	wg.Wait()
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sort.Slice(dbWrites, func(i, j int) bool { return dbWrites[i] < dbWrites[j] })
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return loadResult{
		Devices: devices, Requests: len(latencies), CPUGOMAX: runtime.GOMAXPROCS(0),
		P50ms: pct(latencies, 0.50), P95ms: pct(latencies, 0.95), P99ms: pct(latencies, 0.99),
		Throughput: float64(len(latencies)) / elapsed.Seconds(),
		DBWriteP95: pct(dbWrites, 0.95), AllocMB: float64(ms.Alloc) / (1024 * 1024),
	}
}

func pct(d []time.Duration, p float64) float64 {
	if len(d) == 0 {
		return 0
	}
	i := int(float64(len(d)-1) * p)
	if i < 0 {
		i = 0
	}
	if i >= len(d) {
		i = len(d) - 1
	}
	return float64(d[i].Microseconds()) / 1000.0
}

func fmtSprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
func filepathJoinRepo(elem ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(thisFile), "..")
	return filepath.Join(append([]string{base}, elem...)...)
}
