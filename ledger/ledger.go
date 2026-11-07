// Package ledger implements an append-only SHA-256 hash-chained audit ledger
// and Ed25519 signed checkpoints.
//
// Canonical serialization (normative): each entry is hashed as the UTF-8 byte
// sequence of these fields in order, separated by a single ASCII newline '\n'
// (0x0A), with no trailing newline after the final field:
//
//  1. schema        literal "hydroledger.ledger.entry.v1"
//  2. seq           base-10 ASCII, no sign, no leading zeros except "0"
//  3. event_ref     raw UTF-8 bytes of the event reference (must not contain '\n' or '\r')
//  4. prev_hash     lower-case hex, exactly 64 chars (32 bytes) or 64 zeros for genesis
//  5. timestamp     RFC3339Nano in UTC, e.g. 2026-01-02T15:04:05.123456789Z
//
// current_hash = lower-case hex(SHA-256(canonical bytes)). The stored timestamp
// is normalized to UTC for hashing and comparison.
//
// A hash chain detects post-hoc modification of committed entries given a
// trusted head/checkpoint; it cannot by itself prevent an administrator with
// write access to storage and signing keys from rewriting the entire chain from
// any point (see docs/ledger.md).
package ledger

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SchemaV1      = "hydroledger.ledger.entry.v1"
	CheckpointV1  = "hydroledger.ledger.checkpoint.v1"
	GenesisPrev   = "0000000000000000000000000000000000000000000000000000000000000000"
	HashHexLen    = 64
)

var (
	ErrEmptyEventRef      = errors.New("ledger: event reference must not be empty")
	ErrEventRefControl    = errors.New("ledger: event reference must not contain newline/control line breaks")
	ErrSequenceGap        = errors.New("ledger: sequence number gap or rewind")
	ErrPrevHashMismatch   = errors.New("ledger: previous hash mismatch")
	ErrHashMismatch       = errors.New("ledger: entry hash mismatch")
	ErrEmptyChain         = errors.New("ledger: chain is empty")
	ErrInvalidCheckpoint  = errors.New("ledger: invalid checkpoint")
	ErrCheckpointSig      = errors.New("ledger: checkpoint signature invalid")
	ErrCheckpointChain    = errors.New("ledger: checkpoint does not match chain head")
	ErrNilSigner          = errors.New("ledger: nil signer")
	ErrNilPublicKey       = errors.New("ledger: nil public key")
	ErrInterval           = errors.New("ledger: checkpoint interval must be > 0")
)

// Entry is one append-only ledger record.
type Entry struct {
	Seq       uint64    `json:"seq"`
	EventRef  string    `json:"event_ref"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
	Timestamp time.Time `json:"timestamp"`
}

// CanonicalBytes returns the normative serialization used for hashing.
func (e Entry) CanonicalBytes() []byte {
	ts := e.Timestamp.UTC().Format(time.RFC3339Nano)
	s := strings.Join([]string{
		SchemaV1,
		strconv.FormatUint(e.Seq, 10),
		e.EventRef,
		strings.ToLower(e.PrevHash),
		ts,
	}, "\n")
	return []byte(s)
}

// ComputeHash computes SHA-256 over CanonicalBytes and returns lower-case hex.
func (e Entry) ComputeHash() string {
	sum := sha256.Sum256(e.CanonicalBytes())
	return hex.EncodeToString(sum[:])
}

// Validate checks structural invariants (not chain linkage).
func (e Entry) Validate() error {
	if e.EventRef == "" {
		return ErrEmptyEventRef
	}
	if strings.ContainsAny(e.EventRef, "\n\r") {
		return ErrEventRefControl
	}
	if len(e.PrevHash) != HashHexLen {
		return fmt.Errorf("ledger: prev_hash must be %d hex chars", HashHexLen)
	}
	if _, err := hex.DecodeString(e.PrevHash); err != nil {
		return fmt.Errorf("ledger: prev_hash not hex: %w", err)
	}
	if len(e.Hash) != HashHexLen {
		return fmt.Errorf("ledger: hash must be %d hex chars", HashHexLen)
	}
	if _, err := hex.DecodeString(e.Hash); err != nil {
		return fmt.Errorf("ledger: hash not hex: %w", err)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("ledger: timestamp is zero")
	}
	return nil
}

// Ledger is an in-memory append-only hash chain. It is safe for concurrent use.
type Ledger struct {
	mu      sync.RWMutex
	entries []Entry
}

// New creates an empty ledger.
func New() *Ledger { return &Ledger{} }

// Append adds a new entry. The caller supplies eventRef and timestamp; seq,
// prev_hash and hash are assigned by the ledger.
func (l *Ledger) Append(eventRef string, ts time.Time) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var seq uint64
	prev := GenesisPrev
	if n := len(l.entries); n > 0 {
		last := l.entries[n-1]
		seq = last.Seq + 1
		prev = last.Hash
	}
	e := Entry{Seq: seq, EventRef: eventRef, PrevHash: prev, Timestamp: ts.UTC()}
	if err := e.Validate(); err != nil {
		// Validate checks prev/hash lengths; hash not set yet, so validate pre-hash fields.
		if err2 := validatePreHash(e); err2 != nil {
			return Entry{}, err2
		}
	} else if err := validatePreHash(e); err != nil {
		return Entry{}, err
	}
	e.Hash = e.ComputeHash()
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	l.entries = append(l.entries, e)
	return e, nil
}

func validatePreHash(e Entry) error {
	if e.EventRef == "" {
		return ErrEmptyEventRef
	}
	if strings.ContainsAny(e.EventRef, "\n\r") {
		return ErrEventRefControl
	}
	if len(e.PrevHash) != HashHexLen {
		return fmt.Errorf("ledger: prev_hash must be %d hex chars", HashHexLen)
	}
	if _, err := hex.DecodeString(e.PrevHash); err != nil {
		return fmt.Errorf("ledger: prev_hash not hex: %w", err)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("ledger: timestamp is zero")
	}
	return nil
}

// Entries returns a copy of all entries.
func (l *Ledger) Entries() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Head returns the last entry.
func (l *Ledger) Head() (Entry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.entries) == 0 {
		return Entry{}, false
	}
	return l.entries[len(l.entries)-1], true
}

// Len returns the number of entries.
func (l *Ledger) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Verify checks genesis linkage, sequence continuity, prev_hash linkage, and
// recomputed SHA-256 hashes for every entry.
func (l *Ledger) Verify() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return VerifyEntries(l.entries)
}

// VerifyEntries verifies a sequence of entries as a contiguous chain.
func VerifyEntries(entries []Entry) error {
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("ledger: entry %d: %w", i, err)
		}
		wantSeq := uint64(i)
		if i > 0 {
			wantSeq = entries[i-1].Seq + 1
		}
		if e.Seq != wantSeq {
			return fmt.Errorf("%w: index %d has seq %d want %d", ErrSequenceGap, i, e.Seq, wantSeq)
		}
		wantPrev := GenesisPrev
		if i > 0 {
			wantPrev = entries[i-1].Hash
		}
		if !strings.EqualFold(e.PrevHash, wantPrev) {
			return fmt.Errorf("%w: index %d", ErrPrevHashMismatch, i)
		}
		got := e.ComputeHash()
		if !strings.EqualFold(got, e.Hash) {
			return fmt.Errorf("%w: index %d computed %s stored %s", ErrHashMismatch, i, got, e.Hash)
		}
	}
	return nil
}

// ReplaceEntriesForTest replaces internal state; intended for tamper tests.
func (l *Ledger) ReplaceEntriesForTest(entries []Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append([]Entry(nil), entries...)
}

// Checkpoint is an Ed25519-signed attestation to a chain head.
type Checkpoint struct {
	Schema    string `json:"schema"`
	Seq       uint64 `json:"seq"`
	HeadHash  string `json:"head_hash"`
	Timestamp time.Time `json:"timestamp"`
	Signature []byte `json:"signature"`
	KeyID     string `json:"key_id,omitempty"`
}

// CheckpointMessage returns the canonical bytes signed/verified for a checkpoint.
func (c Checkpoint) CheckpointMessage() []byte {
	ts := c.Timestamp.UTC().Format(time.RFC3339Nano)
	s := strings.Join([]string{
		CheckpointV1,
		strconv.FormatUint(c.Seq, 10),
		strings.ToLower(c.HeadHash),
		ts,
		c.KeyID,
	}, "\n")
	return []byte(s)
}

// SignCheckpoint signs a checkpoint for the given head entry.
func SignCheckpoint(priv ed25519.PrivateKey, keyID string, head Entry, ts time.Time) (Checkpoint, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return Checkpoint{}, ErrNilSigner
	}
	c := Checkpoint{
		Schema:    CheckpointV1,
		Seq:       head.Seq,
		HeadHash:  strings.ToLower(head.Hash),
		Timestamp: ts.UTC(),
		KeyID:     keyID,
	}
	c.Signature = ed25519.Sign(priv, c.CheckpointMessage())
	return c, nil
}

// VerifyCheckpoint verifies signature and that the checkpoint matches head.
func VerifyCheckpoint(pub ed25519.PublicKey, c Checkpoint, head Entry) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrNilPublicKey
	}
	if c.Schema != CheckpointV1 || len(c.HeadHash) != HashHexLen {
		return ErrInvalidCheckpoint
	}
	if c.Seq != head.Seq || !strings.EqualFold(c.HeadHash, head.Hash) {
		return ErrCheckpointChain
	}
	if len(c.Signature) != ed25519.SignatureSize || !ed25519.Verify(pub, c.CheckpointMessage(), c.Signature) {
		return ErrCheckpointSig
	}
	return nil
}

// Witness accepts externally anchored checkpoints (interface for HSM/KMS/
// transparency log/external witness integration).
type Witness interface {
	Publish(ctxTimestamp time.Time, c Checkpoint) error
}

// Checkpointer creates checkpoints every N appends.
type Checkpointer struct {
	interval uint64
	priv     ed25519.PrivateKey
	keyID    string
	witness  Witness
}

// NewCheckpointer constructs a checkpointer. priv may be nil only if never used.
func NewCheckpointer(interval uint64, priv ed25519.PrivateKey, keyID string, witness Witness) (*Checkpointer, error) {
	if interval == 0 {
		return nil, ErrInterval
	}
	return &Checkpointer{interval: interval, priv: append(ed25519.PrivateKey(nil), priv...), keyID: keyID, witness: witness}, nil
}

// MaybeCheckpoint signs/publishes a checkpoint when head.Seq+1 is divisible by N.
func (c *Checkpointer) MaybeCheckpoint(head Entry, ts time.Time) (*Checkpoint, error) {
	if c == nil {
		return nil, nil
	}
	if (head.Seq+1)%c.interval != 0 {
		return nil, nil
	}
	cp, err := SignCheckpoint(c.priv, c.keyID, head, ts)
	if err != nil {
		return nil, err
	}
	if c.witness != nil {
		if err := c.witness.Publish(ts, cp); err != nil {
			return nil, err
		}
	}
	return &cp, nil
}
