// Package cryptutil seals and opens the one thing this service custodies that
// nothing else in it does: a live bearer credential at rest, for the
// delegated-Teams broker token pass-through (ADR 0037). Nothing else in this
// codebase encrypts a column, so it is a package of its own rather than a
// helper buried in internal/store.
package cryptutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the only key size this package accepts. AES-256 rather than
// AES-128: the key is the one thing standing between a stolen database and a
// live Keycloak session, so there is no reason to take the smaller margin.
const KeySize = 32

// ErrInvalidKeySize means a key was not exactly KeySize bytes -- the operator
// most likely gave 16 or 24 random bytes, or pasted the wrong config value.
var ErrInvalidKeySize = errors.New("cryptutil: key must be 32 bytes")

// A Sealer seals and opens ciphertext with one AES-256-GCM key. It is safe for
// concurrent use: cipher.AEAD is, and Seal generates a fresh nonce per call.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 32-byte key, decoded once at startup from
// configuration (see config.BrokerConfig.TokenEncryptionKey) rather than
// re-derived on every call.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cryptutil: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cryptutil: new gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext and authenticates it together with aad, which is
// not itself encrypted. The broker token store uses the session id as aad, so
// a sealed row cannot be copied onto a different session and still open.
//
// The nonce is prepended to the returned ciphertext rather than kept
// alongside it, so a single opaque value is what the store has to persist.
func (s *Sealer) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cryptutil: read nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. It fails if aad does not match what Seal was given, if
// the key is wrong, or if ciphertext was altered after sealing -- GCM
// authenticates the whole thing, not just decrypts it.
func (s *Sealer) Open(ciphertext, aad []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("cryptutil: ciphertext shorter than a nonce")
	}

	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("cryptutil: open: %w", err)
	}
	return plaintext, nil
}
