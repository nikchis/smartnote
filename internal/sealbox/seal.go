package sealbox

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// FormatVersion is the first byte of every sealed field and part of its AAD.
const FormatVersion byte = 1

// AAD binds a sealed field to its place: table, column, row and, for rows
// owned by another row (note versions), the owner. A field moved elsewhere
// does not open. Table, Column and RowID are required and no field may contain
// "|", so that two places never share one encoding.
type AAD struct {
	Table, Column, RowID, OwnerID string
}

// errAmbiguousAAD reports an AAD that could equal the encoding of another place.
var errAmbiguousAAD = errors.New("aad needs table, column and row id without \"|\"")

// bytes returns "smartnote|<version>|<table>|<column>|<row>[|<owner>]", or
// errAmbiguousAAD when a breaks the rules of AAD.
func (a AAD) bytes() ([]byte, error) {
	if a.Table == "" || a.Column == "" || a.RowID == "" ||
		strings.Contains(a.Table+a.Column+a.RowID+a.OwnerID, "|") {
		return nil, errAmbiguousAAD
	}
	s := "smartnote|" + strconv.Itoa(int(FormatVersion)) + "|" + a.Table + "|" + a.Column + "|" + a.RowID
	if a.OwnerID != "" {
		s += "|" + a.OwnerID
	}
	return []byte(s), nil
}

// Seal encrypts plaintext with XChaCha20-Poly1305 under a random nonce and
// returns version (1 byte) ‖ nonce (24 bytes) ‖ ciphertext ‖ tag (16 bytes).
// A key of the wrong size or an AAD that breaks its rules returns ErrInvalidParams.
func Seal(key, plaintext []byte, aad AAD) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidParams, err)
	}
	ad, err := aad.bytes()
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
	return aead.Seal(out, nonce, plaintext, ad), nil
}

// Open reverses Seal. Every failure, including a wrong key, a changed byte, a
// different AAD, an AAD that breaks its rules or an unknown version, returns
// an error matching ErrOpen. An empty plaintext comes back as an empty,
// non-nil slice.
func Open(key, sealed []byte, aad AAD) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	ad, err := aad.bytes()
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
	plaintext, err := aead.Open([]byte{}, sealed[1:header], sealed[header:], ad)
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
