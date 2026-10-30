package usecase

import (
	"context"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
)

// VaultStore is the encrypted local storage as a whole. The store brings the
// password to NFC before deriving a key.
type VaultStore interface {
	Exists(ctx context.Context) (bool, error)
	Create(ctx context.Context, password string) error
	Unlock(ctx context.Context, password string) error
	ChangePassword(ctx context.Context, newPassword string) error
}

// Vault creates, unlocks and re-keys the local storage.
type Vault struct {
	store VaultStore
}

// NewVault returns a Vault over s.
func NewVault(s VaultStore) (*Vault, error) {
	if s == nil {
		return nil, nilDependency("NewVault", "store")
	}

	return &Vault{store: s}, nil
}

// Exists reports whether the local storage has been created.
func (v *Vault) Exists(ctx context.Context) (bool, error) {
	ok, err := v.store.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("check storage: %w", err)
	}

	return ok, nil
}

// Create validates the password and its confirmation and creates the storage.
func (v *Vault) Create(ctx context.Context, password, confirm string) error {
	if err := validateNewPassword(password, confirm); err != nil {
		return err
	}

	if err := v.store.Create(ctx, password); err != nil {
		return fmt.Errorf("create storage: %w", err)
	}

	return nil
}

// Unlock opens the storage with password.
func (v *Vault) Unlock(ctx context.Context, password string) error {
	if err := v.store.Unlock(ctx, password); err != nil {
		return fmt.Errorf("unlock storage: %w", err)
	}

	return nil
}

// ChangePassword validates the new password and its confirmation and re-keys
// the unlocked storage.
func (v *Vault) ChangePassword(ctx context.Context, password, confirm string) error {
	if err := validateNewPassword(password, confirm); err != nil {
		return err
	}

	if err := v.store.ChangePassword(ctx, password); err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	return nil
}

// validateNewPassword checks the length of password and that confirm is the
// same secret; both are compared in NFC, the form the key is derived from.
func validateNewPassword(password, confirm string) error {
	if err := domain.ValidatePassword(password); err != nil {
		return err
	}

	if domain.NormalizeSecret(password) != domain.NormalizeSecret(confirm) {
		return &domain.ValidationError{Field: "confirm", Reason: "does not match the password"}
	}

	return nil
}
