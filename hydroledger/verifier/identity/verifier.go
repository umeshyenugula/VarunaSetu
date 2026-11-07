package identity

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultWindow is the default maximum age of a telemetry timestamp.
	DefaultWindow = 5 * time.Minute
	// DefaultFutureSkew is the default allowance for a timestamp ahead of the
	// verifier's clock.
	DefaultFutureSkew = 30 * time.Second

	maxDuration = time.Duration(1<<63 - 1)
)

// Config controls the timestamp window enforced by a Verifier. The replay
// retention period is Window + FutureSkew: an envelope whose timestamp is
// outside Window/FutureSkew is rejected on time, so remembering its nonce for
// longer adds no protection.
type Config struct {
	Window     time.Duration // default DefaultWindow (5m); must be > 0
	FutureSkew time.Duration // default DefaultFutureSkew (30s); must be >= 0
}

// DefaultConfig returns the default timestamp window.
func DefaultConfig() Config {
	return Config{Window: DefaultWindow, FutureSkew: DefaultFutureSkew}
}

// Validate reports the first problem in c, or nil.
func (c Config) Validate() error {
	if c.Window <= 0 {
		return fmt.Errorf("identity: config Window must be > 0, got %v", c.Window)
	}
	if c.FutureSkew < 0 {
		return fmt.Errorf("identity: config FutureSkew must be >= 0, got %v", c.FutureSkew)
	}
	if c.Window > maxDuration-c.FutureSkew {
		return errors.New("identity: config Window + FutureSkew overflows time.Duration")
	}
	return nil
}

type nonceKey struct {
	deviceID string
	nonce    string
}

type nonceEntry struct {
	key  nonceKey
	seen time.Time
}

// Verifier performs the full acceptance check for a signed envelope:
// enrollment, revocation, Ed25519 signature, timestamp window and replay.
//
// A Verifier is safe for concurrent use. Replay state is in memory only and is
// lost on restart, so a message replayed across a process restart is not
// detected by this package alone.
type Verifier struct {
	cfg       Config
	keys      PublicKeyStore
	revoked   RevocationList
	retention time.Duration

	mu    sync.Mutex
	seen  map[nonceKey]time.Time
	queue []nonceEntry
	head  int
}

// NewVerifier returns a Verifier. keys and revoked must be non-nil; pass a
// Registry and/or RevocationSet when no external store exists.
func NewVerifier(cfg Config, keys PublicKeyStore, revoked RevocationList) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if keys == nil {
		return nil, ErrNilKeyStore
	}
	if revoked == nil {
		return nil, ErrNilRevocationList
	}
	return &Verifier{
		cfg:       cfg,
		keys:      keys,
		revoked:   revoked,
		retention: cfg.Window + cfg.FutureSkew,
		seen:      make(map[nonceKey]time.Time),
	}, nil
}

// Config returns a copy of the verifier configuration.
func (v *Verifier) Config() Config { return v.cfg }

// Verify checks env as of now and returns a deep copy of its unsigned Message.
// Checks run in this order: structure, enrollment, revocation, signature,
// timestamp window, replay. A message is consumed (its nonce recorded) only
// after all earlier checks pass.
//
// now is an explicit reference time on the same clock as Message.Timestamp; the
// package never reads the wall clock.
func (v *Verifier) Verify(env Envelope, now time.Time) (Message, error) {
	if now.IsZero() {
		return Message{}, ErrZeroNow
	}
	now = now.Round(0)

	if err := validateMessage(env.Message); err != nil {
		return Message{}, err
	}

	publicKey, err := v.keys.PublicKey(env.DeviceID)
	if err != nil {
		return Message{}, err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return Message{}, fmt.Errorf("%w: got %d bytes", ErrInvalidPublicKey, len(publicKey))
	}
	if v.revoked.IsRevoked(env.DeviceID) {
		return Message{}, fmt.Errorf("%w: %q", ErrDeviceRevoked, env.DeviceID)
	}
	if err := VerifySignature(env, publicKey); err != nil {
		return Message{}, err
	}

	timestamp := env.Timestamp.Round(0)
	age := now.Sub(timestamp)
	if age > v.cfg.Window {
		return Message{}, fmt.Errorf("%w: age %v exceeds %v", ErrTimestampTooOld, age, v.cfg.Window)
	}
	if age < -v.cfg.FutureSkew {
		return Message{}, fmt.Errorf("%w: timestamp is %v ahead of now (max %v)", ErrTimestampTooFuture, -age, v.cfg.FutureSkew)
	}

	if err := v.checkAndStoreNonce(env.DeviceID, env.Nonce, now); err != nil {
		return Message{}, err
	}

	msg := cloneMessage(env.Message)
	msg.Timestamp = timestamp
	return msg, nil
}

// checkAndStoreNonce records (deviceID, nonce) as used at now, after pruning
// entries older than the retention period. It is atomic, so concurrent
// verification of the same envelope accepts at most one.
func (v *Verifier) checkAndStoreNonce(deviceID, nonce string, now time.Time) error {
	key := nonceKey{deviceID: deviceID, nonce: nonce}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.pruneLocked(now)
	if _, ok := v.seen[key]; ok {
		return fmt.Errorf("%w: device %q", ErrNonceReused, deviceID)
	}
	v.seen[key] = now
	v.queue = append(v.queue, nonceEntry{key: key, seen: now})
	return nil
}

// pruneLocked drops entries older than retention. The queue is ordered by
// insertion, which normally matches now; if now moves backwards the queue may
// retain some expired entries, which is safe (only memory is affected).
func (v *Verifier) pruneLocked(now time.Time) {
	for v.head < len(v.queue) {
		e := v.queue[v.head]
		if now.Sub(e.seen) <= v.retention {
			break
		}
		if seen, ok := v.seen[e.key]; ok && seen.Equal(e.seen) {
			delete(v.seen, e.key)
		}
		v.head++
	}
	if v.head > 0 && v.head*2 >= len(v.queue) {
		v.queue = append([]nonceEntry(nil), v.queue[v.head:]...)
		v.head = 0
	}
}
