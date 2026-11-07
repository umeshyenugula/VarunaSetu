package identity

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// The five checks required by Phase 1 Task C are the TestSecurity_* functions
// below. The additional tests pin order and boundary behaviour.

func TestSecurity_ValidSignatureAccepted(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-valid", []byte("depth=8.0mm"))

	msg, err := v.Verify(env, idT0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if msg.DeviceID != env.DeviceID || msg.Nonce != env.Nonce {
		t.Fatalf("message metadata = %q/%q, want %q/%q", msg.DeviceID, msg.Nonce, env.DeviceID, env.Nonce)
	}
	if !msg.Timestamp.Equal(env.Timestamp) {
		t.Fatalf("message timestamp = %v, want %v", msg.Timestamp, env.Timestamp)
	}
	if !bytes.Equal(msg.Payload, env.Payload) {
		t.Fatalf("message payload = %q, want %q", msg.Payload, env.Payload)
	}

	// Verify returns a copy of the payload.
	env.Payload[0] = 'X'
	if msg.Payload[0] == 'X' {
		t.Fatal("mutating the envelope payload after Verify changed the returned message")
	}
}

func TestSecurity_ModifiedPayloadRejected(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", []byte("depth=8.0mm"))

	env.Payload[0] ^= 0xff
	if _, err := v.Verify(env, idT0); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify modified payload error = %v, want %v", err, ErrSignatureInvalid)
	}
}

func TestSecurity_ReplayedMessageRejected(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-replay", []byte("depth=8.0mm"))

	if _, err := v.Verify(env, idT0); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	if _, err := v.Verify(env, idT0.Add(time.Second)); !errors.Is(err, ErrNonceReused) {
		t.Fatalf("replayed Verify error = %v, want %v", err, ErrNonceReused)
	}
}

func TestSecurity_ExpiredTimestampRejected(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-old", []byte("depth=8.0mm"))

	now := idT0.Add(DefaultConfig().Window + time.Nanosecond)
	if _, err := v.Verify(env, now); !errors.Is(err, ErrTimestampTooOld) {
		t.Fatalf("Verify expired error = %v, want %v", err, ErrTimestampTooOld)
	}
}

func TestSecurity_RevokedDeviceRejected(t *testing.T) {
	reg, kp, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-revoked", []byte("depth=8.0mm"))

	if err := reg.Revoke(kp.DeviceID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := v.Verify(env, idT0); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("Verify revoked error = %v, want %v", err, ErrDeviceRevoked)
	}
}

func TestSecurity_ModifiedMetadataRejected(t *testing.T) {
	reg, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", []byte("depth=8.0mm"))

	tests := []struct {
		name   string
		mutate func(*Envelope)
	}{
		{name: "timestamp", mutate: func(e *Envelope) { e.Timestamp = e.Timestamp.Add(time.Nanosecond) }},
		{name: "nonce", mutate: func(e *Envelope) { e.Nonce = "nonce-2" }},
		{name: "signature", mutate: func(e *Envelope) { e.Signature[0] ^= 0xff }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := env
			tampered.Payload = append([]byte(nil), env.Payload...)
			tampered.Signature = append([]byte(nil), env.Signature...)
			tt.mutate(&tampered)
			if _, err := v.Verify(tampered, idT0); !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("Verify tampered %s error = %v, want %v", tt.name, err, ErrSignatureInvalid)
			}
		})
	}

	// Changing the device id makes the verifier use a different enrolled key,
	// so the original signature cannot verify.
	other, err := GenerateKeyPair("device-2")
	if err != nil {
		t.Fatalf("GenerateKeyPair device-2: %v", err)
	}
	if err := reg.Enroll(other.DeviceID(), other.PublicKey()); err != nil {
		t.Fatalf("Enroll device-2: %v", err)
	}
	swapped := env
	swapped.DeviceID = other.DeviceID()
	if _, err := v.Verify(swapped, idT0); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify swapped device id error = %v, want %v", err, ErrSignatureInvalid)
	}
}

func TestSecurity_TimestampBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		name    string
		ts      time.Time
		now     time.Time
		wantErr error
	}{
		{name: "exactly at the old boundary", ts: idT0.Add(-cfg.Window), now: idT0},
		{name: "just past the old boundary", ts: idT0.Add(-cfg.Window - time.Nanosecond), now: idT0, wantErr: ErrTimestampTooOld},
		{name: "exactly at the future boundary", ts: idT0.Add(cfg.FutureSkew), now: idT0},
		{name: "just past the future boundary", ts: idT0.Add(cfg.FutureSkew + time.Nanosecond), now: idT0, wantErr: ErrTimestampTooFuture},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, signer, v := newTestSetup(t)
			env := mustSign(t, signer, tt.ts, "nonce-boundary", nil)
			_, err := v.Verify(env, tt.now)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Verify() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurity_ZeroNowRejected(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", nil)
	if _, err := v.Verify(env, time.Time{}); !errors.Is(err, ErrZeroNow) {
		t.Fatalf("Verify(zero now) error = %v, want %v", err, ErrZeroNow)
	}
}

func TestSecurity_UnknownDeviceRejected(t *testing.T) {
	_, _, _, v := newTestSetup(t)
	stranger, err := GenerateKeyPair("device-stranger")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	env := mustSign(t, stranger.Signer(), idT0, "nonce-1", nil)
	if _, err := v.Verify(env, idT0); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("Verify unknown device error = %v, want %v", err, ErrUnknownDevice)
	}
}

func TestSecurity_RevokedCheckPrecedesSignature(t *testing.T) {
	reg, kp, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", []byte("payload"))
	env.Payload[0] ^= 0xff // invalidate the signature
	if err := reg.Revoke(kp.DeviceID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := v.Verify(env, idT0); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("Verify revoked/tampered error = %v, want %v", err, ErrDeviceRevoked)
	}
}

func TestSecurity_VerifySignatureIsStateless(t *testing.T) {
	_, kp, signer, _ := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", []byte("payload"))
	for i := 0; i < 2; i++ {
		if err := VerifySignature(env, kp.PublicKey()); err != nil {
			t.Fatalf("VerifySignature call %d: %v", i+1, err)
		}
	}
}
