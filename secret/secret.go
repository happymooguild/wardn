// Package secret encrypts provider credentials at rest.
//
// Provider API keys differ from wardn's own app keys: app keys are only ever
// compared, so they are stored as a SHA-256 hash. A provider key has to be
// *used*, so it must be recoverable - which means encryption, not hashing.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrNoKey means WARDN_SECRET_KEY is unset. Callers treat this as "cannot
// persist credentials" and fall back to env-supplied ones - failing closed
// rather than writing a provider key to Postgres in plaintext.
var ErrNoKey = errors.New("WARDN_SECRET_KEY is not set; cannot store credentials")

type Box struct{ aead cipher.AEAD }

// NewBox derives an AES-256-GCM key from the passphrase. Any length is
// accepted - it is hashed to 32 bytes - so operators are not forced to
// generate exactly 32 raw bytes.
func NewBox(passphrase string) (*Box, error) {
	if passphrase == "" {
		return nil, ErrNoKey
	}
	sum := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal returns nonce||ciphertext.
func (b *Box) Seal(plaintext string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Open reverses Seal. A decryption failure usually means WARDN_SECRET_KEY
// changed since the credential was written.
func (b *Box) Open(sealed []byte) (string, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed (has WARDN_SECRET_KEY changed?): %w", err)
	}
	return string(plaintext), nil
}

// Last4 is the non-sensitive fingerprint shown in the UI so an operator can
// tell which key is installed without the API ever returning it.
func Last4(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return key[len(key)-4:]
}
