package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

var idT0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// Compile-time checks that the registry and revocation set satisfy the public
// interfaces used by NewVerifier.
var (
	_ PublicKeyStore = (*Registry)(nil)
	_ RevocationList = (*Registry)(nil)
	_ Enroller       = (*Registry)(nil)
	_ RevocationList = (*RevocationSet)(nil)
)

// newTestSetup creates one enrolled device and a verifier using cfg.
func newTestSetup(t *testing.T) (*Registry, *KeyPair, *Signer, *Verifier) {
	t.Helper()
	return newTestSetupWithConfig(t, DefaultConfig())
}

func newTestSetupWithConfig(t *testing.T, cfg Config) (*Registry, *KeyPair, *Signer, *Verifier) {
	t.Helper()
	reg := NewRegistry()
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := reg.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	v, err := NewVerifier(cfg, reg, reg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return reg, kp, kp.Signer(), v
}

func mustSign(t *testing.T, s *Signer, ts time.Time, nonce string, payload []byte) Envelope {
	t.Helper()
	env, err := s.Sign(ts, nonce, payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return env
}

func TestConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Window != 5*time.Minute {
		t.Errorf("Window = %v, want 5m", cfg.Window)
	}
	if cfg.FutureSkew != 30*time.Second {
		t.Errorf("FutureSkew = %v, want 30s", cfg.FutureSkew)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config invalid: %v", err)
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "zero window", cfg: Config{Window: 0, FutureSkew: 0}, wantErr: true},
		{name: "negative window", cfg: Config{Window: -time.Second, FutureSkew: 0}, wantErr: true},
		{name: "negative future skew", cfg: Config{Window: time.Minute, FutureSkew: -time.Second}, wantErr: true},
		{name: "zero future skew is allowed", cfg: Config{Window: time.Minute, FutureSkew: 0}},
		{name: "window plus skew overflows", cfg: Config{Window: maxDuration, FutureSkew: time.Second}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestNewVerifier_RejectsInvalidDependencies(t *testing.T) {
	reg := NewRegistry()
	if _, err := NewVerifier(Config{}, reg, reg); err == nil {
		t.Fatal("NewVerifier accepted a zero config")
	}
	if _, err := NewVerifier(DefaultConfig(), nil, reg); !errors.Is(err, ErrNilKeyStore) {
		t.Fatalf("nil key store error = %v, want %v", err, ErrNilKeyStore)
	}
	if _, err := NewVerifier(DefaultConfig(), reg, nil); !errors.Is(err, ErrNilRevocationList) {
		t.Fatalf("nil revocation error = %v, want %v", err, ErrNilRevocationList)
	}
}

func TestVerifier_ConfigReturnsCopy(t *testing.T) {
	_, _, _, v := newTestSetup(t)
	got := v.Config()
	if got != DefaultConfig() {
		t.Fatalf("Config() = %+v, want %+v", got, DefaultConfig())
	}
}

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if kp.DeviceID() != "device-1" {
		t.Errorf("DeviceID = %q, want device-1", kp.DeviceID())
	}
	if len(kp.PublicKey()) != ed25519.PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(kp.PublicKey()), ed25519.PublicKeySize)
	}
	if len(kp.PrivateKey()) != ed25519.PrivateKeySize {
		t.Errorf("private key length = %d, want %d", len(kp.PrivateKey()), ed25519.PrivateKeySize)
	}
	if len(kp.Seed()) != ed25519.SeedSize {
		t.Errorf("seed length = %d, want %d", len(kp.Seed()), ed25519.SeedSize)
	}
	if !bytes.Equal(kp.PrivateKey().Public().(ed25519.PublicKey), kp.PublicKey()) {
		t.Error("public key does not match the private key")
	}

	other, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("second GenerateKeyPair: %v", err)
	}
	if bytes.Equal(kp.PrivateKey(), other.PrivateKey()) {
		t.Error("two generated key pairs are identical")
	}
}

func TestKeyPair_RoundTrip(t *testing.T) {
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	fromPrivate, err := NewKeyPair(kp.DeviceID(), kp.PrivateKey())
	if err != nil {
		t.Fatalf("NewKeyPair: %v", err)
	}
	if !bytes.Equal(fromPrivate.PublicKey(), kp.PublicKey()) {
		t.Error("NewKeyPair produced a different public key")
	}

	fromSeed, err := NewKeyPairFromSeed(kp.DeviceID(), kp.Seed())
	if err != nil {
		t.Fatalf("NewKeyPairFromSeed: %v", err)
	}
	if !bytes.Equal(fromSeed.PublicKey(), kp.PublicKey()) {
		t.Error("NewKeyPairFromSeed produced a different public key")
	}

	env := mustSign(t, fromSeed.Signer(), idT0, "nonce-1", []byte("payload"))
	if err := VerifySignature(env, fromPrivate.PublicKey()); err != nil {
		t.Fatalf("signature from seed-derived pair did not verify: %v", err)
	}
}

func TestKeyPair_AccessorsReturnCopies(t *testing.T) {
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	public := kp.PublicKey()
	private := kp.PrivateKey()
	seed := kp.Seed()

	public[0] ^= 0xff
	private[0] ^= 0xff
	seed[0] ^= 0xff

	if bytes.Equal(public, kp.PublicKey()) {
		t.Error("mutating the returned public key changed the key pair")
	}
	if bytes.Equal(private, kp.PrivateKey()) {
		t.Error("mutating the returned private key changed the key pair")
	}
	if bytes.Equal(seed, kp.Seed()) {
		t.Error("mutating the returned seed changed the key pair")
	}
}

func TestInvalidKeyMaterial(t *testing.T) {
	for _, n := range []int{0, 1, 31, 32, 63, 65} {
		if _, err := NewKeyPair("device-1", make(ed25519.PrivateKey, n)); !errors.Is(err, ErrInvalidPrivateKey) {
			t.Errorf("NewKeyPair(len %d) error = %v, want %v", n, err, ErrInvalidPrivateKey)
		}
		if _, err := NewSigner("device-1", make(ed25519.PrivateKey, n)); !errors.Is(err, ErrInvalidPrivateKey) {
			t.Errorf("NewSigner(len %d) error = %v, want %v", n, err, ErrInvalidPrivateKey)
		}
	}
	for _, n := range []int{0, 1, 31, 33} {
		if _, err := NewKeyPairFromSeed("device-1", make([]byte, n)); !errors.Is(err, ErrInvalidSeed) {
			t.Errorf("NewKeyPairFromSeed(len %d) error = %v, want %v", n, err, ErrInvalidSeed)
		}
	}
	for _, id := range []string{"", strings.Repeat("x", MaxDeviceIDBytes+1)} {
		if _, err := GenerateKeyPair(id); !errors.Is(err, ErrInvalidDeviceID) {
			t.Errorf("GenerateKeyPair(len %d) error = %v, want %v", len(id), err, ErrInvalidDeviceID)
		}
	}
}

func TestSigner_DeviceIDAndPublicKey(t *testing.T) {
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	s := kp.Signer()
	if s.DeviceID() != kp.DeviceID() {
		t.Errorf("DeviceID = %q, want %q", s.DeviceID(), kp.DeviceID())
	}
	if !bytes.Equal(s.PublicKey(), kp.PublicKey()) {
		t.Error("signer public key differs from the key pair")
	}
	public := s.PublicKey()
	public[0] ^= 0xff
	if bytes.Equal(public, s.PublicKey()) {
		t.Error("mutating the returned signer public key changed the signer")
	}
}

func TestSigner_RejectsInvalidMessages(t *testing.T) {
	_, _, s, _ := newTestSetup(t)
	tests := []struct {
		name    string
		ts      time.Time
		nonce   string
		payload []byte
		wantErr error
	}{
		{name: "zero timestamp", ts: time.Time{}, nonce: "n", wantErr: ErrTimestampZero},
		{name: "empty nonce", ts: idT0, nonce: "", wantErr: ErrNonceInvalid},
		{name: "nonce too long", ts: idT0, nonce: strings.Repeat("n", MaxNonceBytes+1), wantErr: ErrNonceInvalid},
		{name: "payload too large", ts: idT0, nonce: "n", payload: make([]byte, MaxPayloadBytes+1), wantErr: ErrPayloadTooLarge},
		{name: "maximum nonce and payload accepted", ts: idT0, nonce: strings.Repeat("n", MaxNonceBytes), payload: make([]byte, MaxPayloadBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Sign(tt.ts, tt.nonce, tt.payload)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Sign() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Sign() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSigner_CopiesPayload(t *testing.T) {
	_, _, s, _ := newTestSetup(t)
	payload := []byte("original")
	env := mustSign(t, s, idT0, "nonce-1", payload)
	payload[0] = 'X'
	if string(env.Payload) != "original" {
		t.Fatalf("envelope payload = %q, want original", env.Payload)
	}
	if err := VerifySignature(env, s.PublicKey()); err != nil {
		t.Fatalf("verify after mutating the caller's payload: %v", err)
	}
}

func TestSigner_DeterministicEncoding(t *testing.T) {
	_, _, s, _ := newTestSetup(t)
	a := mustSign(t, s, idT0, "nonce-1", []byte("payload"))
	b := mustSign(t, s, idT0, "nonce-1", []byte("payload"))
	if !bytes.Equal(a.Signature, b.Signature) {
		t.Error("Ed25519 signatures for identical messages differ")
	}
	c := mustSign(t, s, idT0.Add(time.Nanosecond), "nonce-1", []byte("payload"))
	if bytes.Equal(a.Signature, c.Signature) {
		t.Error("signature did not change when the timestamp changed")
	}
}

func TestVerifySignature_RejectsInvalid(t *testing.T) {
	_, kp, s, _ := newTestSetup(t)
	valid := mustSign(t, s, idT0, "nonce-1", []byte("payload"))

	tests := []struct {
		name    string
		mutate  func(*Envelope)
		pub     ed25519.PublicKey
		wantErr error
	}{
		{name: "wrong public key size", pub: make(ed25519.PublicKey, 31), wantErr: ErrInvalidPublicKey},
		{
			name:    "different device key",
			pub:     func() ed25519.PublicKey { other, _ := GenerateKeyPair("device-2"); return other.PublicKey() }(),
			wantErr: ErrSignatureInvalid,
		},
		{name: "short signature", mutate: func(e *Envelope) { e.Signature = e.Signature[:63] }, pub: kp.PublicKey(), wantErr: ErrSignatureSize},
		{name: "flipped signature bit", mutate: func(e *Envelope) { e.Signature[0] ^= 0xff }, pub: kp.PublicKey(), wantErr: ErrSignatureInvalid},
		{name: "empty device id", mutate: func(e *Envelope) { e.DeviceID = "" }, pub: kp.PublicKey(), wantErr: ErrInvalidDeviceID},
		{name: "zero timestamp", mutate: func(e *Envelope) { e.Timestamp = time.Time{} }, pub: kp.PublicKey(), wantErr: ErrTimestampZero},
		{name: "empty nonce", mutate: func(e *Envelope) { e.Nonce = "" }, pub: kp.PublicKey(), wantErr: ErrNonceInvalid},
		{name: "payload too large", mutate: func(e *Envelope) { e.Payload = make([]byte, MaxPayloadBytes+1) }, pub: kp.PublicKey(), wantErr: ErrPayloadTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := valid
			env.Payload = append([]byte(nil), valid.Payload...)
			env.Signature = append([]byte(nil), valid.Signature...)
			if tt.mutate != nil {
				tt.mutate(&env)
			}
			if err := VerifySignature(env, tt.pub); !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifySignature() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewNonce(t *testing.T) {
	a, err := NewNonce()
	if err != nil {
		t.Fatalf("NewNonce: %v", err)
	}
	b, err := NewNonce()
	if err != nil {
		t.Fatalf("second NewNonce: %v", err)
	}
	if a == b {
		t.Error("two nonces are identical")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil {
		t.Fatalf("nonce is not unpadded base64url: %v", err)
	}
	if len(raw) != 16 {
		t.Errorf("nonce entropy = %d bytes, want 16", len(raw))
	}
}

func TestRegistry_EnrollAndPublicKey(t *testing.T) {
	reg := NewRegistry()
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := reg.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if !reg.Enrolled(kp.DeviceID()) {
		t.Error("Enrolled = false after enrollment")
	}
	got, err := reg.PublicKey(kp.DeviceID())
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if !bytes.Equal(got, kp.PublicKey()) {
		t.Error("PublicKey returned a different key")
	}
	got[0] ^= 0xff
	again, err := reg.PublicKey(kp.DeviceID())
	if err != nil {
		t.Fatalf("second PublicKey: %v", err)
	}
	if !bytes.Equal(again, kp.PublicKey()) {
		t.Error("mutating the returned key changed the registry")
	}
}

func TestRegistry_RejectsInvalidEnrollmentAndDuplicates(t *testing.T) {
	reg := NewRegistry()
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := reg.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if err := reg.Enroll(kp.DeviceID(), kp.PublicKey()); !errors.Is(err, ErrDeviceAlreadyEnrolled) {
		t.Errorf("duplicate Enroll error = %v, want %v", err, ErrDeviceAlreadyEnrolled)
	}
	if err := reg.Enroll("", kp.PublicKey()); !errors.Is(err, ErrInvalidDeviceID) {
		t.Errorf("empty id error = %v, want %v", err, ErrInvalidDeviceID)
	}
	if err := reg.Enroll(strings.Repeat("x", MaxDeviceIDBytes+1), kp.PublicKey()); !errors.Is(err, ErrInvalidDeviceID) {
		t.Errorf("long id error = %v, want %v", err, ErrInvalidDeviceID)
	}
	for _, n := range []int{0, 31, 33} {
		if err := reg.Enroll("device-2", make(ed25519.PublicKey, n)); !errors.Is(err, ErrInvalidPublicKey) {
			t.Errorf("public key len %d error = %v, want %v", n, err, ErrInvalidPublicKey)
		}
	}
}

func TestRegistry_UnknownDevice(t *testing.T) {
	reg := NewRegistry()
	if reg.Enrolled("missing") {
		t.Error("Enrolled = true for an unknown device")
	}
	if _, err := reg.PublicKey("missing"); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("PublicKey error = %v, want %v", err, ErrUnknownDevice)
	}
}

func TestRegistry_Revocation(t *testing.T) {
	reg := NewRegistry()
	kp, err := GenerateKeyPair("device-1")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := reg.Enroll(kp.DeviceID(), kp.PublicKey()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if err := reg.Revoke(kp.DeviceID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !reg.IsRevoked(kp.DeviceID()) {
		t.Error("IsRevoked = false after Revoke")
	}
	if err := reg.Revoke(kp.DeviceID()); err != nil {
		t.Errorf("second Revoke = %v, want nil (idempotent)", err)
	}
	if err := reg.Revoke(""); !errors.Is(err, ErrInvalidDeviceID) {
		t.Errorf("Revoke(empty) error = %v, want %v", err, ErrInvalidDeviceID)
	}

	other, err := GenerateKeyPair("device-2")
	if err != nil {
		t.Fatalf("second GenerateKeyPair: %v", err)
	}
	if err := reg.Revoke(other.DeviceID()); err != nil {
		t.Fatalf("Revoke unenrolled: %v", err)
	}
	if err := reg.Enroll(other.DeviceID(), other.PublicKey()); !errors.Is(err, ErrDeviceRevoked) {
		t.Errorf("Enroll revoked device error = %v, want %v", err, ErrDeviceRevoked)
	}
}

func TestRevocationSet(t *testing.T) {
	set := NewRevocationSet()
	if set.IsRevoked("device-1") {
		t.Error("fresh set reports a revoked device")
	}
	if err := set.Revoke("device-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !set.IsRevoked("device-1") {
		t.Error("IsRevoked = false after Revoke")
	}
	if err := set.Revoke(""); !errors.Is(err, ErrInvalidDeviceID) {
		t.Errorf("Revoke(empty) error = %v, want %v", err, ErrInvalidDeviceID)
	}
}
