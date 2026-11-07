// Package identity implements Ed25519 device identity, signed telemetry
// envelopes, replay protection and an in-memory enrollment/revocation
// registry.
//
// It uses only the Go standard library for cryptography. It does not make an
// enrolled-but-compromised device trustworthy, and it does not provide Sybil
// resistance: enrollment is an administrative decision outside this package.
// See HANDOFF.md for assumptions and known limitations.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	// MaxDeviceIDBytes is the maximum accepted device id length in bytes.
	MaxDeviceIDBytes = 128
	// MaxNonceBytes is the maximum accepted nonce length in bytes.
	MaxNonceBytes = 128
	// MaxPayloadBytes is the maximum accepted telemetry payload size.
	MaxPayloadBytes = 64 * 1024

	// signingDomain separates these signatures from any other use of the same
	// key material. It is covered by the signature but is not transmitted.
	signingDomain = "hydroledger/identity/telemetry/v1"
)

// Message is the unsigned part of a telemetry envelope. DeviceID, Timestamp,
// Nonce and Payload are all covered by the signature.
type Message struct {
	DeviceID  string
	Timestamp time.Time
	Nonce     string
	Payload   []byte
}

// Envelope is a signed Message.
type Envelope struct {
	Message
	Signature []byte
}

// KeyPair is an Ed25519 device identity. The key material is unexported;
// accessors return copies so callers cannot mutate the pair in place.
type KeyPair struct {
	deviceID   string
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

// GenerateKeyPair creates a new Ed25519 key pair for deviceID.
func GenerateKeyPair(deviceID string) (*KeyPair, error) {
	if err := validateDeviceID(deviceID); err != nil {
		return nil, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate key: %w", err)
	}
	return &KeyPair{
		deviceID:   deviceID,
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

// NewKeyPair rebuilds a KeyPair from an existing 64-byte Ed25519 private key.
// Use it when a private key was loaded from storage.
func NewKeyPair(deviceID string, privateKey ed25519.PrivateKey) (*KeyPair, error) {
	if err := validateDeviceID(deviceID); err != nil {
		return nil, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidPrivateKey, len(privateKey))
	}
	priv := append(ed25519.PrivateKey(nil), privateKey...)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, ErrInvalidPrivateKey
	}
	return &KeyPair{
		deviceID:   deviceID,
		publicKey:  append(ed25519.PublicKey(nil), pub...),
		privateKey: priv,
	}, nil
}

// NewKeyPairFromSeed rebuilds a KeyPair from the 32-byte RFC 8032 seed.
func NewKeyPairFromSeed(deviceID string, seed []byte) (*KeyPair, error) {
	if err := validateDeviceID(deviceID); err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidSeed, len(seed))
	}
	return NewKeyPair(deviceID, ed25519.NewKeyFromSeed(seed))
}

// DeviceID returns the device id bound to this key pair.
func (k *KeyPair) DeviceID() string { return k.deviceID }

// PublicKey returns a copy of the 32-byte public key.
func (k *KeyPair) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k.publicKey...)
}

// PrivateKey returns a copy of the 64-byte private key. Handle it as a secret
// and do not log it.
func (k *KeyPair) PrivateKey() ed25519.PrivateKey {
	return append(ed25519.PrivateKey(nil), k.privateKey...)
}

// Seed returns a copy of the 32-byte RFC 8032 seed for storage.
func (k *KeyPair) Seed() []byte { return k.privateKey.Seed() }

// Signer returns a Signer bound to this key pair.
func (k *KeyPair) Signer() *Signer {
	return &Signer{
		deviceID:   k.deviceID,
		publicKey:  append(ed25519.PublicKey(nil), k.publicKey...),
		privateKey: append(ed25519.PrivateKey(nil), k.privateKey...),
	}
}

// Signer signs telemetry with one device's Ed25519 private key. It is safe for
// concurrent use; the key material is never mutated after construction.
type Signer struct {
	deviceID   string
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

// NewSigner returns a Signer for deviceID from a 64-byte Ed25519 private key.
func NewSigner(deviceID string, privateKey ed25519.PrivateKey) (*Signer, error) {
	if err := validateDeviceID(deviceID); err != nil {
		return nil, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidPrivateKey, len(privateKey))
	}
	priv := append(ed25519.PrivateKey(nil), privateKey...)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, ErrInvalidPrivateKey
	}
	return &Signer{
		deviceID:   deviceID,
		publicKey:  append(ed25519.PublicKey(nil), pub...),
		privateKey: priv,
	}, nil
}

// DeviceID returns the signer's device id.
func (s *Signer) DeviceID() string { return s.deviceID }

// PublicKey returns a copy of the signer's public key.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), s.publicKey...)
}

// Sign creates a signed envelope. The timestamp is rounded to remove any
// monotonic clock reading so the wire representation is unambiguous. The
// payload is copied. The caller must supply a nonce; use NewNonce for a random
// one. A nonce is required to be unique per device inside the verifier's replay
// retention window.
func (s *Signer) Sign(timestamp time.Time, nonce string, payload []byte) (Envelope, error) {
	msg := Message{
		DeviceID:  s.deviceID,
		Timestamp: timestamp.Round(0),
		Nonce:     nonce,
		Payload:   payload,
	}
	if err := validateMessage(msg); err != nil {
		return Envelope{}, err
	}
	msg.Payload = append([]byte(nil), payload...)
	signature := ed25519.Sign(s.privateKey, signingBytes(msg))
	return Envelope{Message: msg, Signature: signature}, nil
}

// NewNonce returns a 128-bit random nonce as unpadded base64url (22
// characters). It reads crypto/rand; it never uses the network.
func NewNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("identity: read random nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// VerifySignature checks env.Signature against publicKey. It validates the
// message structure and signature length first. It is stateless: it does not
// check enrollment, revocation, timestamps or replay. Use Verifier.Verify for
// the full check.
func VerifySignature(env Envelope, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidPublicKey, len(publicKey))
	}
	if err := validateMessage(env.Message); err != nil {
		return err
	}
	if len(env.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: got %d bytes", ErrSignatureSize, len(env.Signature))
	}
	if !ed25519.Verify(publicKey, signingBytes(env.Message), env.Signature) {
		return ErrSignatureInvalid
	}
	return nil
}

// signingBytes is the canonical, length-prefixed encoding covered by the
// signature. Length prefixes make the encoding unambiguous for arbitrary byte
// strings and payloads. The domain string provides cross-protocol separation.
func signingBytes(m Message) []byte {
	buf := make([]byte, 0, len(signingDomain)+1+4+len(m.DeviceID)+8+4+4+len(m.Nonce)+4+len(m.Payload))
	buf = append(buf, signingDomain...)
	buf = append(buf, 0)
	buf = appendString(buf, m.DeviceID)
	buf = appendInt64(buf, m.Timestamp.Unix())
	buf = appendUint32(buf, uint32(m.Timestamp.Nanosecond()))
	buf = appendString(buf, m.Nonce)
	buf = appendBytes(buf, m.Payload)
	return buf
}

func appendString(dst []byte, s string) []byte {
	dst = appendUint32(dst, uint32(len(s)))
	return append(dst, s...)
}

func appendBytes(dst []byte, b []byte) []byte {
	dst = appendUint32(dst, uint32(len(b)))
	return append(dst, b...)
}

func appendUint32(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}

func appendInt64(dst []byte, v int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	return append(dst, b[:]...)
}

func validateDeviceID(deviceID string) error {
	if len(deviceID) == 0 || len(deviceID) > MaxDeviceIDBytes {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidDeviceID, len(deviceID))
	}
	return nil
}

func validateMessage(m Message) error {
	if err := validateDeviceID(m.DeviceID); err != nil {
		return err
	}
	if m.Timestamp.IsZero() {
		return ErrTimestampZero
	}
	if len(m.Nonce) == 0 || len(m.Nonce) > MaxNonceBytes {
		return fmt.Errorf("%w: got %d bytes", ErrNonceInvalid, len(m.Nonce))
	}
	if len(m.Payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: got %d bytes", ErrPayloadTooLarge, len(m.Payload))
	}
	return nil
}

func cloneMessage(m Message) Message {
	m.Payload = append([]byte(nil), m.Payload...)
	return m
}
