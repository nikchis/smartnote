// Package sealbox derives keys and seals individual fields. It holds no state
// and imports no other package of the project.
package sealbox

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Sizes of the KDF salt and of every key, in bytes.
const (
	SaltSize = 16
	KeySize  = 32
)

// Bounds checked before running Argon2id, so forged parameters cannot exhaust memory.
const (
	minTime      = 1
	maxTime      = 10
	minMemoryKiB = 19456  // 19 MiB
	maxMemoryKiB = 262144 // 256 MiB
	minThreads   = 1
	maxThreads   = 8
)

// KDFParams are the Argon2id cost parameters, stored next to the salt.
type KDFParams struct {
	Time      uint32 `json:"t"`
	MemoryKiB uint32 `json:"m"`
	Threads   uint8  `json:"p"`
}

// DefaultKDF returns the parameters for new storages and new servers. It is a
// function so that no package can lower the cost for everyone.
func DefaultKDF() KDFParams { return KDFParams{Time: 3, MemoryKiB: 65536, Threads: 4} }

var (
	// ErrInvalidParams reports KDF parameters, a salt, a key, a secret or an AAD
	// that the caller passed outside the allowed bounds.
	ErrInvalidParams = errors.New("sealbox: invalid parameters")
	// ErrOpen reports a sealed field that cannot be opened with the given key and AAD.
	ErrOpen = errors.New("sealbox: cannot open sealed data")
)

// Validate checks that p is within the allowed bounds.
func (p KDFParams) Validate() error {
	switch {
	case p.Time < minTime || p.Time > maxTime:
		return fmt.Errorf("%w: time %d not in [%d, %d]", ErrInvalidParams, p.Time, minTime, maxTime)
	case p.MemoryKiB < minMemoryKiB || p.MemoryKiB > maxMemoryKiB:
		return fmt.Errorf("%w: memory %d KiB not in [%d, %d]", ErrInvalidParams, p.MemoryKiB, minMemoryKiB, maxMemoryKiB)
	case p.Threads < minThreads || p.Threads > maxThreads:
		return fmt.Errorf("%w: threads %d not in [%d, %d]", ErrInvalidParams, p.Threads, minThreads, maxThreads)
	}

	return nil
}

// NewSalt returns SaltSize random bytes.
func NewSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("sealbox: read random salt: %w", err)
	}

	return salt, nil
}

// DeriveKey derives a KeySize key from secret with Argon2id after checking p,
// the salt length and that secret is not empty.
func DeriveKey(secret, salt []byte, p KDFParams) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: empty secret", ErrInvalidParams)
	}

	if len(salt) != SaltSize {
		return nil, fmt.Errorf("%w: salt of %d bytes, want %d", ErrInvalidParams, len(salt), SaltSize)
	}

	return argon2.IDKey(secret, salt, p.Time, p.MemoryKiB, p.Threads, KeySize), nil
}

// SubKey derives a KeySize key for the purpose named by info with HKDF-SHA256
// and no salt. master must be at least KeySize bytes.
func SubKey(master []byte, info string) ([]byte, error) {
	if len(master) < KeySize {
		return nil, fmt.Errorf("%w: master key of %d bytes, want at least %d", ErrInvalidParams, len(master), KeySize)
	}

	return subKeyLen(master, info, KeySize)
}

func subKeyLen(master []byte, info string, n int) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, master, nil, info, n)
	if err != nil {
		return nil, fmt.Errorf("sealbox: hkdf: %w", err)
	}

	return key, nil
}
