package citysim

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"hydroledger-prototype/hydroledger/verifier/identity"
	"hydroledger-prototype/service"
)

// API is an HTTP client for a VarunaSetu service.
type API struct {
	Base string
	HTTP *http.Client
}

func NewAPI(base string) *API {
	return &API{Base: base, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Wire is the JSON request body of POST /telemetry/readings.
type Wire struct {
	DeviceID  string          `json:"device_id"`
	Timestamp time.Time       `json:"timestamp"`
	Nonce     string          `json:"nonce"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

// DecisionJSON mirrors verifier.Decision on the wire.
type DecisionJSON struct {
	State        string
	Reason       string
	RObsMM       float64
	RMaxMM       float64
	PeerCount    int
	PeerMedianMM float64
	PeerMADMM    float64
	ZScore       float64
}

// PostResult is everything a caller needs to know about one API call.
type PostResult struct {
	Method    string        `json:"method"`
	Path      string        `json:"path"`
	Status    int           `json:"status"`
	LatencyMs float64       `json:"latencyMs"`
	ID        string        `json:"id,omitempty"`
	Decision  *DecisionJSON `json:"decision,omitempty"`
	Error     string        `json:"error,omitempty"`
	Existing  bool          `json:"existing,omitempty"` // 200 = idempotent replay of a stored result
	Corrob    int           `json:"corroboratedEscrows,omitempty"`
}

// Sign builds a signed wire envelope for a telemetry payload.
func Sign(kp *identity.KeyPair, ts time.Time, nonce string, tel service.Telemetry) (Wire, error) {
	payload, err := json.Marshal(tel)
	if err != nil {
		return Wire{}, err
	}
	return SignRaw(kp, ts, nonce, payload)
}

// SignRaw signs an arbitrary payload (used for malformed-payload probes).
func SignRaw(kp *identity.KeyPair, ts time.Time, nonce string, payload []byte) (Wire, error) {
	env, err := kp.Signer().Sign(ts, nonce, payload)
	if err != nil {
		return Wire{}, err
	}
	return Wire{
		DeviceID:  env.DeviceID,
		Timestamp: env.Timestamp,
		Nonce:     env.Nonce,
		Payload:   json.RawMessage(payload),
		Signature: base64.RawStdEncoding.EncodeToString(env.Signature),
	}, nil
}

// Nonce returns a deterministic nonce for a device and sequence number.
func Nonce(deviceID string, seq int) string { return fmt.Sprintf("%s-%06d", deviceID, seq) }

// Post sends one signed telemetry envelope.
func (a *API) Post(ctx context.Context, w Wire) PostResult {
	body, _ := json.Marshal(w)
	res := PostResult{Method: "POST", Path: "/telemetry/readings"}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Base+res.Path, bytes.NewReader(body))
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := a.HTTP.Do(req)
	res.LatencyMs = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		ID       string        `json:"id"`
		Decision *DecisionJSON `json:"decision"`
		Error    string        `json:"error"`
		Corrob   int           `json:"corroborated_escrows"`
	}
	_ = json.Unmarshal(raw, &parsed)
	res.ID, res.Decision, res.Error, res.Corrob = parsed.ID, parsed.Decision, parsed.Error, parsed.Corrob
	res.Existing = resp.StatusCode == http.StatusOK
	return res
}

// Get fetches a JSON resource into out.
func (a *API) Get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Base+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
