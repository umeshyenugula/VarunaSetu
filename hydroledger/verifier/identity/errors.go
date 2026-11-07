package identity

import "errors"

var (
	// ErrInvalidDeviceID means the device id was empty or longer than
	// MaxDeviceIDBytes.
	ErrInvalidDeviceID = errors.New("identity: device id must be 1..128 bytes")
	// ErrInvalidPublicKey means the public key was not ed25519.PublicKeySize
	// bytes.
	ErrInvalidPublicKey = errors.New("identity: public key must be 32 bytes")
	// ErrInvalidPrivateKey means the private key was not
	// ed25519.PrivateKeySize bytes.
	ErrInvalidPrivateKey = errors.New("identity: private key must be 64 bytes")
	// ErrInvalidSeed means the seed was not ed25519.SeedSize bytes.
	ErrInvalidSeed = errors.New("identity: seed must be 32 bytes")

	// ErrSignatureSize means the signature was not ed25519.SignatureSize bytes.
	ErrSignatureSize = errors.New("identity: signature must be 64 bytes")
	// ErrSignatureInvalid means the signature did not verify for the message
	// and public key.
	ErrSignatureInvalid = errors.New("identity: signature verification failed")

	// ErrNonceInvalid means the nonce was empty or longer than MaxNonceBytes.
	ErrNonceInvalid = errors.New("identity: nonce must be 1..128 bytes")
	// ErrPayloadTooLarge means the payload exceeded MaxPayloadBytes.
	ErrPayloadTooLarge = errors.New("identity: payload exceeds 64 KiB")
	// ErrTimestampZero means the message timestamp was the zero time.
	ErrTimestampZero = errors.New("identity: timestamp is zero")
	// ErrZeroNow means Verify was called with a zero reference time.
	ErrZeroNow = errors.New("identity: verification time is zero")

	// ErrUnknownDevice means the device id has no enrolled public key.
	ErrUnknownDevice = errors.New("identity: device is not enrolled")
	// ErrDeviceRevoked means the device is on the revocation list.
	ErrDeviceRevoked = errors.New("identity: device is revoked")
	// ErrDeviceAlreadyEnrolled means the device id already has a public key.
	ErrDeviceAlreadyEnrolled = errors.New("identity: device is already enrolled")

	// ErrNonceReused means the (device, nonce) pair was already accepted inside
	// the replay retention window.
	ErrNonceReused = errors.New("identity: nonce has already been used")
	// ErrTimestampTooOld means now - timestamp exceeded Config.Window.
	ErrTimestampTooOld = errors.New("identity: timestamp is older than the replay window")
	// ErrTimestampTooFuture means timestamp - now exceeded Config.FutureSkew.
	ErrTimestampTooFuture = errors.New("identity: timestamp is too far in the future")

	// ErrNilKeyStore means a Verifier was constructed without a key store.
	ErrNilKeyStore = errors.New("identity: nil public key store")
	// ErrNilRevocationList means a Verifier was constructed without a
	// revocation list.
	ErrNilRevocationList = errors.New("identity: nil revocation list")
)
