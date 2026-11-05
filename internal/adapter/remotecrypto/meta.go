// Package remotecrypto is the key handling shared by the remote storages:
// the sync-phrase key derivation and the key check (spec §5.3).
package remotecrypto

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// FormatVersion is the remote format this version reads and writes.
const FormatVersion = 1

// Key derivation and the key check.
const (
	encKeyInfo    = "smartnote/v2/enc"
	keyCheckPlain = "smartnote-key-check"
)

// KeyCheckAAD binds the key check to its place: smartnote|1|meta|key_check|1.
func KeyCheckAAD() sealbox.AAD { return sealbox.AAD{Table: "meta", Column: "key_check", RowID: "1"} }

// Meta is the key material of a remote storage.
type Meta struct {
	ServerID      uuid.UUID
	FormatVersion int
	KDF           sealbox.KDFParams
	Salt          []byte
	KeyCheck      []byte
}

// NewMeta returns the meta of a new remote storage and its encryption key.
func NewMeta(phrase string, p sealbox.KDFParams) (Meta, []byte, error) {
	salt, err := sealbox.NewSalt()
	if err != nil {
		return Meta{}, nil, err
	}

	key, err := DeriveEncKey(phrase, salt, p)
	if err != nil {
		return Meta{}, nil, err
	}

	check, err := sealbox.Seal(key, []byte(keyCheckPlain), KeyCheckAAD())
	if err != nil {
		sealbox.Wipe(key)
		return Meta{}, nil, err
	}

	return Meta{ServerID: uuid.New(), FormatVersion: FormatVersion, KDF: p, Salt: salt, KeyCheck: check}, key, nil
}

// Unlock checks the format and the KDF parameters before running Argon2id,
// then derives the key from phrase and opens the key check. A newer format or
// parameters out of bounds return domain.ErrUnsupportedFormat, a wrong phrase
// domain.ErrWrongSyncPhrase.
func (m Meta) Unlock(phrase string) ([]byte, error) {
	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("%w: remote format version %d", domain.ErrUnsupportedFormat, m.FormatVersion)
	}

	if err := m.KDF.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnsupportedFormat, err)
	}

	if len(m.Salt) != sealbox.SaltSize {
		return nil, fmt.Errorf("%w: salt of %d bytes", domain.ErrUnsupportedFormat, len(m.Salt))
	}

	key, err := DeriveEncKey(phrase, m.Salt, m.KDF)
	if err != nil {
		return nil, err
	}

	check, err := sealbox.Open(key, m.KeyCheck, KeyCheckAAD())
	if err != nil || string(check) != keyCheckPlain {
		sealbox.Wipe(key)
		return nil, domain.ErrWrongSyncPhrase
	}

	return key, nil
}

// DeriveEncKey is HKDF(Argon2id(NFC(phrase))); the master key is wiped.
func DeriveEncKey(phrase string, salt []byte, p sealbox.KDFParams) ([]byte, error) {
	secret := []byte(domain.NormalizeSecret(phrase))
	defer sealbox.Wipe(secret)

	master, err := sealbox.DeriveKey(secret, salt, p)
	if err != nil {
		return nil, err
	}
	defer sealbox.Wipe(master)

	return sealbox.SubKey(master, encKeyInfo)
}
