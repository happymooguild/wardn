package secret

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := NewBox("a-passphrase-of-any-length")
	if err != nil {
		t.Fatal(err)
	}
	const key = "sk-ant-api03-secret-value"

	sealed, err := box.Seal(key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(key)) {
		t.Fatal("plaintext key must not appear in the ciphertext")
	}

	got, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != key {
		t.Errorf("round trip changed the value: %q", got)
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	box, _ := NewBox("passphrase")
	a, _ := box.Seal("same-key")
	b, _ := box.Seal("same-key")

	// A fresh nonce per seal: identical plaintext must not produce identical
	// ciphertext, or the DB leaks which apps share a key.
	if bytes.Equal(a, b) {
		t.Error("two seals of the same value produced identical ciphertext")
	}
}

func TestOpenWithWrongPassphraseFails(t *testing.T) {
	original, _ := NewBox("original-passphrase")
	sealed, _ := original.Seal("sk-secret")

	rotated, _ := NewBox("different-passphrase")
	if _, err := rotated.Open(sealed); err == nil {
		t.Error("decrypting with the wrong key should fail, not return garbage")
	}
}

func TestOpenRejectsTruncatedCiphertext(t *testing.T) {
	box, _ := NewBox("passphrase")
	if _, err := box.Open([]byte{1, 2, 3}); err == nil {
		t.Error("a too-short blob should error rather than panic")
	}
}

func TestNewBoxRequiresPassphrase(t *testing.T) {
	if _, err := NewBox(""); !errors.Is(err, ErrNoKey) {
		t.Errorf("want ErrNoKey for an empty passphrase, got %v", err)
	}
}

func TestLast4(t *testing.T) {
	if got := Last4("sk-ant-api03-abcd1234"); got != "1234" {
		t.Errorf("Last4 = %q", got)
	}
	// Never expose most of a short key.
	if got := Last4("abc"); got != "****" {
		t.Errorf("short keys should be fully masked, got %q", got)
	}
}
