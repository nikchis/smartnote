package sealbox

import (
	"crypto/rand"
	"fmt"
	"strconv"

	"golang.org/x/crypto/chacha20poly1305"
)

// FormatVersion is the first byte of every sealed field and part of its AAD.
const FormatVersion byte = 1

// AAD binds a sealed field to its place: table, column, row and, for rows
// owned by another row (note versions), the owner. A field moved elsewhere
// does not open.
type AAD struct {
	Table, Column, RowID, OwnerID string
}

// bytes returns "smartnote|<version>|<table>|<column>|<row>[|<owner>]".
func (a AAD) bytes() []byte {
	s := "smartnote|" + strconv.Itoa(int(FormatVersion)) + "|" + a.Table + "|" + a.Column + "|" + a.RowID
	if a.OwnerID != "" {
		s += "|" + a.OwnerID
	}
	return []byte(s)
}

// Seal encrypts plaintext with XChaCha20-Poly1305 under a random nonce and
// returns version (1 byte) ‖ nonce (24 bytes) ‖ ciphertext ‖ tag (16 bytes).
func Seal(key, plaintext []byte, aad AAD) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidParams, err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("sealbox: read random nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, FormatVersion)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad.bytes()), nil
}

// Open reverses Seal. Every failure, including a wrong key, a changed byte, a
// different AAD or an unknown version, returns an error matching ErrOpen.
func Open(key, sealed []byte, aad AAD) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	header := 1 + aead.NonceSize()
	if len(sealed) < header+aead.Overhead() {
		return nil, fmt.Errorf("%w: %d bytes is too short", ErrOpen, len(sealed))
	}
	if sealed[0] != FormatVersion {
		return nil, fmt.Errorf("%w: unknown version %d", ErrOpen, sealed[0])
	}
	plaintext, err := aead.Open(nil, sealed[1:header], sealed[header:], aad.bytes())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	return plaintext, nil
}

// Wipe overwrites b with zeros. The garbage collector may still hold copies,
// so this is best effort.
func Wipe(b []byte) {
	clear(b)
}
