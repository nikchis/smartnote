package usecase

import (
	"context"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
)

// KeyStore keeps the client SSH key in the local storage.
type KeyStore interface {
	// LoadKey returns the key and true, or false when there is none.
	LoadKey(ctx context.Context) (domain.KeyPair, bool, error)
	SaveKey(ctx context.Context, k domain.KeyPair) error
}

// KeyTool creates and parses SSH keys.
type KeyTool interface {
	Generate() (domain.KeyPair, error)
	// Import parses an OpenSSH private key; a protected key without the right
	// passphrase returns domain.ErrKeyPassphraseRequired.
	Import(data []byte, passphrase string) (domain.KeyPair, error)
}

// Keys generates, imports and exports the client SSH key.
type Keys struct {
	store KeyStore
	tool  KeyTool
}

// NewKeys returns Keys over s and t.
func NewKeys(s KeyStore, t KeyTool) (*Keys, error) {
	switch {
	case s == nil:
		return nil, nilDependency("NewKeys", "store")
	case t == nil:
		return nil, nilDependency("NewKeys", "tool")
	}
	return &Keys{store: s, tool: t}, nil
}

// HasKey reports whether a key is stored.
func (k *Keys) HasKey(ctx context.Context) (bool, error) {
	_, ok, err := k.store.LoadKey(ctx)
	if err != nil {
		return false, fmt.Errorf("load ssh key: %w", err)
	}
	return ok, nil
}

// Generate creates a new key and stores it in place of the old one.
func (k *Keys) Generate(ctx context.Context) error {
	pair, err := k.tool.Generate()
	if err != nil {
		return fmt.Errorf("generate ssh key: %w", err)
	}
	return k.save(ctx, pair)
}

// Import parses data and stores the key in place of the old one; nothing is
// stored when parsing fails.
func (k *Keys) Import(ctx context.Context, data []byte, passphrase string) error {
	pair, err := k.tool.Import(data, passphrase)
	if err != nil {
		return fmt.Errorf("import ssh key: %w", err)
	}
	return k.save(ctx, pair)
}

// PublicKey returns the stored public key in authorized_keys format, or
// domain.ErrKeyMissing when there is no key.
func (k *Keys) PublicKey(ctx context.Context) (string, error) {
	pair, ok, err := k.store.LoadKey(ctx)
	if err != nil {
		return "", fmt.Errorf("load ssh key: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("public key: %w", domain.ErrKeyMissing)
	}
	return pair.PublicAuthorized, nil
}

func (k *Keys) save(ctx context.Context, pair domain.KeyPair) error {
	if err := k.store.SaveKey(ctx, pair); err != nil {
		return fmt.Errorf("save ssh key: %w", err)
	}
	return nil
}
