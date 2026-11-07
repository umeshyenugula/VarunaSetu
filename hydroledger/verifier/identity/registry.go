package identity

import (
	"crypto/ed25519"
	"fmt"
	"sync"
)

// PublicKeyStore resolves an enrolled device id to its Ed25519 public key.
type PublicKeyStore interface {
	PublicKey(deviceID string) (ed25519.PublicKey, error)
}

// RevocationList reports whether a device id has been revoked.
type RevocationList interface {
	IsRevoked(deviceID string) bool
}

// Enroller is the administrative enrollment interface. Who is allowed to call
// Enroll, and on what evidence, is outside this package.
type Enroller interface {
	Enroll(deviceID string, publicKey ed25519.PublicKey) error
}

// RevocationSet is a standalone, in-memory revocation list. It is safe for
// concurrent use. Revocation is permanent for the lifetime of the set; there is
// no un-revoke operation.
type RevocationSet struct {
	mu      sync.RWMutex
	revoked map[string]struct{}
}

// NewRevocationSet returns an empty revocation list.
func NewRevocationSet() *RevocationSet {
	return &RevocationSet{revoked: make(map[string]struct{})}
}

// Revoke adds deviceID to the list. It is idempotent. Revoking an id that has
// not been enrolled is allowed so that a key cannot be enrolled later under a
// revoked identity.
func (s *RevocationSet) Revoke(deviceID string) error {
	if err := validateDeviceID(deviceID); err != nil {
		return err
	}
	s.mu.Lock()
	s.revoked[deviceID] = struct{}{}
	s.mu.Unlock()
	return nil
}

// IsRevoked reports whether deviceID is in the list.
func (s *RevocationSet) IsRevoked(deviceID string) bool {
	s.mu.RLock()
	_, ok := s.revoked[deviceID]
	s.mu.RUnlock()
	return ok
}

// Registry is an in-memory enrollment store and revocation list. It is safe for
// concurrent use. Keys and revocations are lost on restart; persistence is a
// later phase.
type Registry struct {
	mu          sync.RWMutex
	keys        map[string]ed25519.PublicKey
	revocations *RevocationSet
}

// NewRegistry returns an empty enrollment registry.
func NewRegistry() *Registry {
	return &Registry{
		keys:        make(map[string]ed25519.PublicKey),
		revocations: NewRevocationSet(),
	}
}

// Enroll binds deviceID to publicKey. It does not accept a second key for an
// already-enrolled id (no silent key rotation) and refuses revoked ids.
func (r *Registry) Enroll(deviceID string, publicKey ed25519.PublicKey) error {
	if err := validateDeviceID(deviceID); err != nil {
		return err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidPublicKey, len(publicKey))
	}
	key := append(ed25519.PublicKey(nil), publicKey...)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revocations.IsRevoked(deviceID) {
		return fmt.Errorf("%w: %q", ErrDeviceRevoked, deviceID)
	}
	if _, ok := r.keys[deviceID]; ok {
		return fmt.Errorf("%w: %q", ErrDeviceAlreadyEnrolled, deviceID)
	}
	r.keys[deviceID] = key
	return nil
}

// PublicKey returns a copy of the enrolled public key for deviceID.
func (r *Registry) PublicKey(deviceID string) (ed25519.PublicKey, error) {
	r.mu.RLock()
	key, ok := r.keys[deviceID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDevice, deviceID)
	}
	return append(ed25519.PublicKey(nil), key...), nil
}

// Enrolled reports whether deviceID has a public key.
func (r *Registry) Enrolled(deviceID string) bool {
	r.mu.RLock()
	_, ok := r.keys[deviceID]
	r.mu.RUnlock()
	return ok
}

// Revoke adds deviceID to the registry's revocation list. It serialises with
// Enroll, so an enrollment cannot complete after the revocation check but
// before the revocation is recorded.
func (r *Registry) Revoke(deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revocations.Revoke(deviceID)
}

// IsRevoked reports whether deviceID is revoked.
func (r *Registry) IsRevoked(deviceID string) bool {
	return r.revocations.IsRevoked(deviceID)
}
