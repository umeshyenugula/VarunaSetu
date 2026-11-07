package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/identity"
	"hydroledger-prototype/hydroledger/verifier/spatial"
)

const testImage = "postgis/postgis:16-3.4"

type testDB struct {
	DB        *sql.DB
	URL       string
	container string
}

func openTestDB(t *testing.T) *testDB {
	t.Helper()
	if url := os.Getenv("HYDROLEDGER_TEST_DATABASE_URL"); url != "" {
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatalf("open test database: %v", err)
		}
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(2)
		db.SetConnMaxIdleTime(30 * time.Second)
		t.Cleanup(func() { _ = db.Close() })
		waitForDB(t, db)
		return &testDB{DB: db, URL: url}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not available and HYDROLEDGER_TEST_DATABASE_URL not set: %v", err)
	}
	name := "hydroledger-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-e", "POSTGRES_PASSWORD=test", "-e", "POSTGRES_USER=test", "-e", "POSTGRES_DB=hydroledger",
		"-p", "127.0.0.1::5432", testImage).CombinedOutput()
	if err != nil {
		t.Skipf("start disposable postgres container: %v\n%s", err, out)
	}
	tdb := &testDB{container: name}
	t.Cleanup(func() {
		if tdb.DB != nil {
			_ = tdb.DB.Close()
		}
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})
	portOut, err := exec.Command("docker", "port", name, "5432/tcp").CombinedOutput()
	if err != nil {
		t.Fatalf("docker port: %v\n%s", err, portOut)
	}
	port := strings.TrimSpace(strings.Split(strings.TrimSpace(string(portOut)), ":")[len(strings.Split(strings.TrimSpace(string(portOut)), ":"))-1])
	tdb.URL = fmt.Sprintf("postgres://test:test@127.0.0.1:%s/hydroledger?sslmode=disable", port)
	db, err := sql.Open("pgx", tdb.URL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	tdb.DB = db
	waitForDB(t, db)
	return tdb
}

func waitForDB(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		last = db.PingContext(ctx)
		cancel()
		if last == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("database not ready: %v", last)
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	for _, name := range []string{"001_service.sql", "002_ledger.sql"} {
		path := filepath.Join(filepath.Dir(thisFile), "..", "migrations", name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration: %v", err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply migration: %v\nSQL file: %s", err, path)
		}
	}
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`TRUNCATE ledger_entry, escrow_record, verification_decision, telemetry_reading, device, building RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func seedBuilding(t *testing.T, db *sql.DB, externalID string, capacity float64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO building (external_id, capacity_litres) VALUES ($1,$2) ON CONFLICT (external_id) DO UPDATE SET capacity_litres=EXCLUDED.capacity_litres, reserved_litres=0`, externalID, capacity)
	if err != nil {
		t.Fatalf("seed building: %v", err)
	}
}

func seedDevice(t *testing.T, db *sql.DB, deviceID, buildingExternalID string, pub []byte) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO device (id, building_id, public_key)
		SELECT $1, b.id, $3 FROM building b WHERE b.external_id=$2
		ON CONFLICT (id) DO UPDATE SET public_key=EXCLUDED.public_key`,
		deviceID, buildingExternalID, pub)
	if err != nil {
		t.Fatalf("seed device: %v", err)
	}
}

type testEnv struct {
	DB       *sql.DB
	Repo     *Repository
	Server   *Server
	Registry *identity.Registry
	Now      func() time.Time
	setNow   func(time.Time)
}

func newTestEnv(t *testing.T, db *sql.DB) *testEnv {
	t.Helper()
	reg := identity.NewRegistry()
	idv, err := identity.NewVerifier(identity.DefaultConfig(), reg, reg)
	if err != nil {
		t.Fatalf("identity verifier: %v", err)
	}
	v, err := verifier.New(verifier.DefaultConfig())
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	env := &testEnv{DB: db, Repo: &Repository{DB: db}, Registry: reg, Now: func() time.Time { return now }}
	env.setNow = func(n time.Time) { now = n }
	srv, err := New(Config{
		Verifier:       v,
		Identity:       idv,
		KeyStore:       reg,
		Revoked:        reg,
		IdentityConfig: identity.DefaultConfig(),
		Repository:     env.Repo,
		Logger:         slog.New(slog.NewTextHandler(ioDiscard{}, nil)),
		RequestTimeout: 15 * time.Second,
		Now:            env.Now,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	env.Server = srv
	return env
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func mustKey(t *testing.T, deviceID string) *identity.KeyPair {
	t.Helper()
	k, err := identity.GenerateKeyPair(deviceID)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return k
}

func signRequest(t *testing.T, kp *identity.KeyPair, ts time.Time, nonce string, telemetry Telemetry) signedTelemetryRequest {
	t.Helper()
	payload, err := json.Marshal(telemetry)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	env, err := kp.Signer().Sign(ts, nonce, payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signedTelemetryRequest{
		DeviceID:  env.DeviceID,
		Timestamp: env.Timestamp,
		Nonce:     env.Nonce,
		Payload:   json.RawMessage(payload),
		Signature: base64.RawStdEncoding.EncodeToString(env.Signature),
	}
}

func postTelemetry(t *testing.T, h http.Handler, req signedTelemetryRequest) (*http.Response, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/telemetry/readings", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	resp := w.Result()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	return resp, out
}

func doJSON(t *testing.T, h http.Handler, method, path string) (*http.Response, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	resp := w.Result()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	return resp, out
}

// canonical test cell (Hyderabad area, res 9) for tests needing valid H3.
func testCell(t *testing.T) string {
	t.Helper()
	c, err := spatial.CellAt(17.3850, 78.4867, spatial.DefaultResolution)
	if err != nil {
		t.Fatalf("test cell: %v", err)
	}
	return c
}

func testTelemetry(cell string, radar float64) Telemetry {
	return Telemetry{
		BuildingID:     "bldg-1",
		ClaimedLitres:  5.0,
		RoofAreaM2:     10.0,
		RunoffCoeff:    0.8,
		StoredLitres:   0,
		CapacityLitres: 10000,
		RadarMM:        radar,
		CellID:         cell,
	}
}

func envInfo() map[string]string {
	return map[string]string{
		"go_version": runtime.Version(),
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"cpus":       strconv.Itoa(runtime.NumCPU()),
		"gomaxprocs": strconv.Itoa(runtime.GOMAXPROCS(0)),
		"pg_image":   testImage,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}
