package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestAppendVerifyAndCanonicalHash(t *testing.T) {
	l := New()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 600000000, time.FixedZone("X", 3600))
	e1, err := l.Append("reading:abc", ts)
	if err != nil {
		t.Fatal(err)
	}
	if e1.Seq != 0 || e1.PrevHash != GenesisPrev || len(e1.Hash) != HashHexLen {
		t.Fatalf("unexpected genesis entry: %+v", e1)
	}
	if e1.ComputeHash() != e1.Hash {
		t.Fatal("hash mismatch")
	}
	// same logical instant in UTC must hash the same as UTC-normalized append
	e2, err := l.Append("reading:def", ts.UTC())
	if err != nil {
		t.Fatal(err)
	}
	if e2.Seq != 1 || e2.PrevHash != e1.Hash {
		t.Fatalf("unexpected linkage: %+v", e2)
	}
	if err := l.Verify(); err != nil {
		t.Fatal(err)
	}
	// canonical bytes are deterministic and include schema/fields in order
	b := e1.CanonicalBytes()
	s := string(b)
	if !strings.HasPrefix(s, SchemaV1+"\n0\nreading:abc\n"+GenesisPrev+"\n") {
		t.Fatalf("unexpected canonical form: %q", s)
	}
}

func TestTamperDetection_ModifiedHistoricalEntry(t *testing.T) {
	l := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if _, err := l.Append("evt:"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Verify(); err != nil {
		t.Fatal(err)
	}
	entries := l.Entries()
	entries[1].EventRef = "evt:TAMPERED" // historical entry modified, hash not updated
	l.ReplaceEntriesForTest(entries)
	if err := l.Verify(); err == nil {
		t.Fatal("expected verification failure for tampered historical entry")
	} else if !strings.Contains(err.Error(), "hash mismatch") && !strings.Contains(err.Error(), "previous hash mismatch") {
		// hash mismatch on modified entry is the direct detection
		t.Fatalf("unexpected error: %v", err)
	}

	// If attacker also recomputes that entry's hash, linkage breaks at next entry.
	entries = l.Entries()
	entries[1].EventRef = "evt:TAMPERED"
	entries[1].Hash = entries[1].ComputeHash()
	l.ReplaceEntriesForTest(entries)
	if err := l.Verify(); err == nil {
		t.Fatal("expected verification failure when only tampered entry hash is recomputed")
	}
}

func TestCheckpointValidAndInvalid(t *testing.T) {
	l := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_, _ = l.Append("evt", base.Add(time.Duration(i)*time.Second))
	}
	head, _ := l.Head()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cp, err := SignCheckpoint(priv, "k1", head, base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCheckpoint(pub, cp, head); err != nil {
		t.Fatal(err)
	}

	// Modified checkpoint
	bad := cp
	bad.HeadHash = strings.Repeat("0", 64)
	if err := VerifyCheckpoint(pub, bad, head); err == nil {
		t.Fatal("expected modified checkpoint to fail")
	}

	// Invalid signature
	bad2 := cp
	bad2.Signature[0] ^= 0xff
	if err := VerifyCheckpoint(pub, bad2, head); err == nil {
		t.Fatal("expected invalid signature")
	}

	// Wrong key
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyCheckpoint(pub2, cp, head); err == nil {
		t.Fatal("expected wrong key to fail")
	}
}

func TestRewrittenChainWithForgedCheckpoint(t *testing.T) {
	// Demonstrates the documented limitation boundary: a verifier who trusts the
	// correct public key detects a checkpoint forged by a different key.
	l := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_, _ = l.Append("evt", base.Add(time.Duration(i)*time.Second))
	}
	legitPub, legitPriv, _ := ed25519.GenerateKey(rand.Reader)
	head, _ := l.Head()
	_, err := SignCheckpoint(legitPriv, "legit", head, base)
	if err != nil {
		t.Fatal(err)
	}

	// Administrator rewrites entire chain and forges a checkpoint with attacker key.
	attackerPub, attackerPriv, _ := ed25519.GenerateKey(rand.Reader)
	rewritten := New()
	for i := 0; i < 3; i++ {
		_, _ = rewritten.Append("forged", base.Add(time.Duration(i)*time.Second))
	}
	forgedHead, _ := rewritten.Head()
	forged, err := SignCheckpoint(attackerPriv, "attacker", forgedHead, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCheckpoint(legitPub, forged, forgedHead); err == nil {
		t.Fatal("expected verifier with legitimate key to reject forged checkpoint")
	}
	// The rewritten chain is internally consistent but represents a different history.
	if err := rewritten.Verify(); err != nil {
		t.Fatal(err)
	}
	_ = attackerPub
}

type memWitness struct{ cps []Checkpoint }

func (m *memWitness) Publish(_ time.Time, c Checkpoint) error { m.cps = append(m.cps, c); return nil }

func TestCheckpointerInterval(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	w := &memWitness{}
	cp, err := NewCheckpointer(2, priv, "k", w)
	if err != nil {
		t.Fatal(err)
	}
	l := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		e, _ := l.Append("evt", base.Add(time.Duration(i)*time.Second))
		_, _ = cp.MaybeCheckpoint(e, base)
	}
	if len(w.cps) != 2 { // seq 1 (2nd entry) and seq 3 (4th)
		t.Fatalf("expected 2 checkpoints, got %d", len(w.cps))
	}
}
