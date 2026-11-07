package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/identity"
	"hydroledger-prototype/hydroledger/verifier/spatial"
)

type ctxKey string

const verifiedMessageKey ctxKey = "verified-telemetry"

type Config struct {
	Verifier            *verifier.Verifier
	Identity            *identity.Verifier
	KeyStore            identity.PublicKeyStore
	Revoked             identity.RevocationList
	IdentityConfig      identity.Config
	Repository          *Repository
	Logger              *slog.Logger
	RequestTimeout      time.Duration
	EscrowSweepInterval time.Duration
	Now                 func() time.Time
	// CORSAllowOrigin, if set, enables simple CORS for browser dashboard reads (dev/integration).
	CORSAllowOrigin string
}

type Server struct {
	cfg       Config
	stop      chan struct{}
	closeOnce sync.Once
}

func New(cfg Config) (*Server, error) {
	if cfg.Verifier == nil || cfg.Repository == nil || cfg.Repository.DB == nil {
		return nil, errors.New("service: verifier and repository are required")
	}
	if cfg.Identity == nil && (cfg.KeyStore == nil || cfg.Revoked == nil) {
		return nil, errors.New("service: identity verifier or key store+revocation list required")
	}
	if cfg.KeyStore == nil && cfg.Identity != nil {
		// best effort: cannot extract; require explicit stores for durable idempotent auth
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.EscrowSweepInterval <= 0 {
		cfg.EscrowSweepInterval = time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.IdentityConfig.Window == 0 && cfg.IdentityConfig.FutureSkew == 0 {
		if cfg.Identity != nil {
			cfg.IdentityConfig = cfg.Identity.Config()
		} else {
			cfg.IdentityConfig = identity.DefaultConfig()
		}
	}
	return &Server{cfg: cfg, stop: make(chan struct{})}, nil
}

func (s *Server) now() time.Time { return s.cfg.Now() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /telemetry/readings", s.signedTelemetryMiddleware(http.HandlerFunc(s.postReading)))
	mux.HandleFunc("GET /telemetry/readings", s.listReadings)
	mux.HandleFunc("GET /telemetry/readings/{id}", s.getReading)
	mux.HandleFunc("GET /escrows", s.listEscrows)
	mux.HandleFunc("GET /escrows/{id}", s.getEscrow)
	mux.HandleFunc("GET /cells", s.listCells)
	mux.HandleFunc("GET /cells/{h3_index}/status", s.getCellStatus)
	mux.HandleFunc("GET /ledger/entries", s.listLedgerEntries)
	mux.HandleFunc("GET /analytics/summary", s.getAnalytics)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("OPTIONS /", s.optionsOK)
	var h http.Handler = mux
	if s.cfg.CORSAllowOrigin != "" {
		h = corsMiddleware(s.cfg.CORSAllowOrigin, h)
	}
	return requestMiddleware(s.cfg.RequestTimeout, s.cfg.Logger, h)
}

func (s *Server) StartExpiryWorker(ctx context.Context) {
	go func() {
		t := time.NewTicker(s.cfg.EscrowSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case now := <-t.C:
				if err := s.cfg.Repository.ExpireEscrows(ctx, now); err != nil {
					s.cfg.Logger.Error("escrow expiry failed", "error", err)
				}
			}
		}
	}()
}

func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.stop) })
}

type signedTelemetryRequest struct {
	DeviceID  string          `json:"device_id"`
	Timestamp time.Time       `json:"timestamp"`
	Nonce     string          `json:"nonce"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

// signedTelemetryMiddleware verifies device signatures using the Phase 1
// identity package and enforces durable idempotency. Stateless signature/
// enrollment/revocation/timestamp checks are performed; replay/idempotency is
// resolved against durable storage so retries return the persisted result and
// nonce reuse cannot bypass persistence.
func (s *Server) signedTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req signedTelemetryRequest
		if err := decodeStrict(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "malformed request")
			return
		}
		sig, err := base64.RawStdEncoding.DecodeString(req.Signature)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid signature encoding")
			return
		}
		msg := identity.Message{DeviceID: req.DeviceID, Timestamp: req.Timestamp, Nonce: req.Nonce, Payload: append([]byte(nil), req.Payload...)}
		env := identity.Envelope{Message: msg, Signature: sig}

		if s.cfg.KeyStore != nil && s.cfg.Revoked != nil {
			if err := s.verifyStateless(env); err != nil {
				writeError(w, http.StatusUnauthorized, "telemetry authentication failed")
				return
			}
			hash := sha256.Sum256(msg.Payload)
			existing, existingHash, err := s.cfg.Repository.GetReadingByIdempotency(r.Context(), msg.DeviceID, msg.Nonce)
			if err == nil {
				if string(existingHash) != string(hash[:]) {
					writeError(w, http.StatusConflict, "idempotency key reused with different payload")
					return
				}
				writeJSON(w, http.StatusOK, existing)
				return
			}
			if !errors.Is(err, ErrNotFound) {
				if errors.Is(err, context.DeadlineExceeded) {
					writeError(w, http.StatusGatewayTimeout, "request timed out")
					return
				}
				if errors.Is(err, context.Canceled) {
					return
				}
				s.cfg.Logger.Error("idempotency lookup failed", "error", err)
				writeError(w, http.StatusInternalServerError, "could not persist telemetry")
				return
			}
			msg.Timestamp = msg.Timestamp.Round(0)
			ctx := context.WithValue(r.Context(), verifiedMessageKey, msg)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fallback: Phase 1 verifier (in-memory replay). Suitable for
		// non-durable/single-process use where durable idempotency is not required.
		vmsg, err := s.cfg.Identity.Verify(env, s.now())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "telemetry authentication failed")
			return
		}
		ctx := context.WithValue(r.Context(), verifiedMessageKey, vmsg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) verifyStateless(env identity.Envelope) error {
	if err := validateIdentityMessage(env.Message); err != nil {
		return err
	}
	pub, err := s.cfg.KeyStore.PublicKey(env.DeviceID)
	if err != nil {
		return err
	}
	if s.cfg.Revoked.IsRevoked(env.DeviceID) {
		return identity.ErrDeviceRevoked
	}
	if err := identity.VerifySignature(env, pub); err != nil {
		return err
	}
	now := s.now().Round(0)
	ts := env.Timestamp.Round(0)
	age := now.Sub(ts)
	if age > s.cfg.IdentityConfig.Window {
		return identity.ErrTimestampTooOld
	}
	if age < -s.cfg.IdentityConfig.FutureSkew {
		return identity.ErrTimestampTooFuture
	}
	return nil
}

func validateIdentityMessage(m identity.Message) error {
	if m.DeviceID == "" || len(m.DeviceID) > identity.MaxDeviceIDBytes {
		return identity.ErrInvalidDeviceID
	}
	if m.Timestamp.IsZero() {
		return identity.ErrTimestampZero
	}
	if m.Nonce == "" || len(m.Nonce) > identity.MaxNonceBytes {
		return identity.ErrNonceInvalid
	}
	if len(m.Payload) > identity.MaxPayloadBytes {
		return identity.ErrPayloadTooLarge
	}
	return nil
}

func verifiedMessage(r *http.Request) (identity.Message, bool) {
	msg, ok := r.Context().Value(verifiedMessageKey).(identity.Message)
	return msg, ok
}

func (s *Server) postReading(w http.ResponseWriter, r *http.Request) {
	msg, ok := verifiedMessage(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "telemetry authentication failed")
		return
	}
	var telemetry Telemetry
	if err := strictJSON(msg.Payload, &telemetry); err != nil {
		writeError(w, http.StatusBadRequest, "invalid telemetry payload")
		return
	}
	if telemetry.CellID == "" || telemetry.BuildingID == "" {
		writeError(w, http.StatusBadRequest, "building_id and h3_index are required")
		return
	}
	if err := spatial.ValidateCell(telemetry.CellID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid h3_index")
		return
	}

	vcfg := s.cfg.Verifier.Config()
	reading := telemetry.Reading(msg.DeviceID, msg.Timestamp)
	d, err := s.evaluateWithPeers(r.Context(), vcfg, reading)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "request timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		s.cfg.Logger.Error("peer retrieval or evaluation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not evaluate telemetry")
		return
	}

	hash := sha256.Sum256(msg.Payload)
	result, err := s.cfg.Repository.StoreReading(r.Context(), msg.DeviceID, msg.Nonce, msg.Timestamp, telemetry, d, msg.Timestamp.Add(vcfg.EscrowTTL), hash[:], vcfg)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "request timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		s.cfg.Logger.Error("store telemetry failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not persist telemetry")
		return
	}
	status := http.StatusCreated
	if result.Existing {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

// evaluateWithPeers supplies recent eligible peers from the database to the
// existing verifier implementation without modifying verifier logic. A scratch
// verifier is seeded in event-time order so durable DB peers drive the same
// Evaluate rules.
func (s *Server) evaluateWithPeers(ctx context.Context, vcfg verifier.Config, reading verifier.Reading) (verifier.Decision, error) {
	peers, err := s.cfg.Repository.RecentPeerReadings(ctx, reading.CellID, reading.Timestamp, vcfg.PeerMaxAge, reading.DeviceID, vcfg.WindowMaxDevices)
	if err != nil {
		return verifier.Decision{}, err
	}
	scratch, err := verifier.New(vcfg)
	if err != nil {
		return verifier.Decision{}, err
	}
	for _, p := range peers {
		scratch.Evaluate(p.Reading)
	}
	return scratch.Evaluate(reading), nil
}

func (s *Server) getReading(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.GetReading(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "reading not found")
		return
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "request timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not retrieve reading")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getEscrow(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.GetEscrow(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "escrow not found")
		return
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "request timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not retrieve escrow")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getCellStatus(w http.ResponseWriter, r *http.Request) {
	cell := r.PathValue("h3_index")
	if err := spatial.ValidateCell(cell); err != nil {
		writeError(w, http.StatusBadRequest, "invalid h3_index")
		return
	}
	vcfg := s.cfg.Verifier.Config()
	peers, err := s.cfg.Repository.RecentPeers(r.Context(), cell, s.now(), vcfg.PeerMaxAge, "", vcfg.WindowMaxDevices)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "request timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		writeError(w, http.StatusInternalServerError, "could not retrieve cell status")
		return
	}
	writeJSON(w, http.StatusOK, CellStatus{H3Index: cell, RecentDevices: len(peers), Status: "AVAILABLE"})
}

func requestMiddleware(timeout time.Duration, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func decodeStrict(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength > 1<<20 {
		return errors.New("invalid body")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("trailing data")
	}
	return nil
}

func strictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func queryLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func (s *Server) listReadings(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.ListReadings(r.Context(), queryLimit(r))
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		s.cfg.Logger.Error("list readings failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list readings")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listEscrows(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.ListEscrows(r.Context(), queryLimit(r))
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		s.cfg.Logger.Error("list escrows failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list escrows")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listCells(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.ListCellSummaries(r.Context(), queryLimit(r))
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		s.cfg.Logger.Error("list cells failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list cells")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listLedgerEntries(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.ListLedgerEntries(r.Context(), queryLimit(r))
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		// migration 002 may not be applied; dashboard falls back to fixtures
		writeError(w, http.StatusServiceUnavailable, "ledger entries unavailable")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAnalytics(w http.ResponseWriter, r *http.Request) {
	out, err := s.cfg.Repository.Analytics(r.Context())
	if err != nil {
		if isContextErr(w, err) {
			return
		}
		s.cfg.Logger.Error("analytics failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not compute analytics")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Repository.DB.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) optionsOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func isContextErr(w http.ResponseWriter, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "request timed out")
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return false
}

func corsMiddleware(allowOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
