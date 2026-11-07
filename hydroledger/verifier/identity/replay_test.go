package identity

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplay_NonceIsScopedPerDevice(t *testing.T) {
	reg, _, signerA, v := newTestSetup(t)
	kpB, err := GenerateKeyPair("device-2")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := reg.Enroll(kpB.DeviceID(), kpB.PublicKey()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	envA := mustSign(t, signerA, idT0, "shared-nonce", []byte("a"))
	envB := mustSign(t, kpB.Signer(), idT0, "shared-nonce", []byte("b"))

	if _, err := v.Verify(envA, idT0); err != nil {
		t.Fatalf("Verify device A: %v", err)
	}
	if _, err := v.Verify(envB, idT0); err != nil {
		t.Fatalf("Verify device B with the same nonce: %v", err)
	}
}

func TestReplay_RetentionBoundary(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	retention := DefaultConfig().Window + DefaultConfig().FutureSkew

	first := mustSign(t, signer, idT0, "reused-nonce", []byte("first"))
	if _, err := v.Verify(first, idT0); err != nil {
		t.Fatalf("first Verify: %v", err)
	}

	// At exactly the retention boundary the nonce is still remembered.
	atBoundary := idT0.Add(retention)
	second := mustSign(t, signer, atBoundary, "reused-nonce", []byte("second"))
	if _, err := v.Verify(second, atBoundary); !errors.Is(err, ErrNonceReused) {
		t.Fatalf("Verify at retention boundary error = %v, want %v", err, ErrNonceReused)
	}

	// One nanosecond later the old entry is pruned. The old envelope is now
	// outside the timestamp window, so allowing the nonce again is safe.
	afterBoundary := atBoundary.Add(time.Nanosecond)
	third := mustSign(t, signer, afterBoundary, "reused-nonce", []byte("third"))
	if _, err := v.Verify(third, afterBoundary); err != nil {
		t.Fatalf("Verify after retention boundary: %v", err)
	}
}

func TestReplay_RejectedSignatureDoesNotConsumeNonce(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", []byte("payload"))

	tampered := env
	tampered.Payload = append([]byte(nil), env.Payload...)
	tampered.Payload[0] ^= 0xff
	if _, err := v.Verify(tampered, idT0); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify tampered error = %v, want %v", err, ErrSignatureInvalid)
	}
	if _, err := v.Verify(env, idT0); err != nil {
		t.Fatalf("Verify original after a rejected signature: %v", err)
	}
}

func TestReplay_ExpiredMessageDoesNotConsumeNonce(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-1", nil)

	late := idT0.Add(DefaultConfig().Window + time.Nanosecond)
	if _, err := v.Verify(env, late); !errors.Is(err, ErrTimestampTooOld) {
		t.Fatalf("Verify late error = %v, want %v", err, ErrTimestampTooOld)
	}
	if _, err := v.Verify(env, idT0); err != nil {
		t.Fatalf("Verify in-window after a timestamp rejection: %v", err)
	}
}

func TestReplay_FutureMessageDoesNotConsumeNonce(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	ts := idT0.Add(DefaultConfig().FutureSkew + time.Nanosecond)
	env := mustSign(t, signer, ts, "nonce-1", nil)

	if _, err := v.Verify(env, idT0); !errors.Is(err, ErrTimestampTooFuture) {
		t.Fatalf("Verify future error = %v, want %v", err, ErrTimestampTooFuture)
	}
	if _, err := v.Verify(env, ts); err != nil {
		t.Fatalf("Verify at the message timestamp after a future rejection: %v", err)
	}
}

func TestReplay_ConcurrentSameEnvelopeOnlyOneAccepted(t *testing.T) {
	_, _, signer, v := newTestSetup(t)
	env := mustSign(t, signer, idT0, "nonce-concurrent", []byte("payload"))

	const workers = 64
	var (
		wg       sync.WaitGroup
		accepted atomic.Int32
		errs     = make(chan error, workers)
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := v.Verify(env, idT0); err != nil {
				errs <- err
				return
			}
			accepted.Add(1)
		}()
	}
	wg.Wait()
	close(errs)

	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted = %d, want exactly 1", got)
	}
	for err := range errs {
		if !errors.Is(err, ErrNonceReused) {
			t.Errorf("concurrent Verify error = %v, want %v", err, ErrNonceReused)
		}
	}
}
