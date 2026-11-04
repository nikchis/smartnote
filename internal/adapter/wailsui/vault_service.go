package wailsui

import "github.com/nikchis/smartnote/internal/usecase"

// VaultService creates, unlocks and re-keys the local storage.
type VaultService struct {
	env    Env
	vault  *usecase.Vault
	onOpen func()
}

// NewVaultService returns a VaultService over v. onOpen is called after every
// successful Create and Unlock, once the storage is open; nil calls nothing.
func NewVaultService(env Env, v *usecase.Vault, onOpen func()) *VaultService {
	if onOpen == nil {
		onOpen = func() {}
	}

	return &VaultService{env: env, vault: v, onOpen: onOpen}
}

// Exists reports whether the local storage exists.
func (s *VaultService) Exists() (bool, error) {
	ok, err := s.vault.Exists(s.env.Ctx)
	if err != nil {
		return false, s.env.fail("vault exists", err)
	}

	return ok, nil
}

// Create creates the local storage with password, which confirm repeats.
func (s *VaultService) Create(password, confirm string) error {
	if err := s.vault.Create(s.env.Ctx, password, confirm); err != nil {
		return s.env.fail("vault create", err)
	}

	s.onOpen()

	return nil
}

// Unlock opens the local storage with password.
func (s *VaultService) Unlock(password string) error {
	if err := s.vault.Unlock(s.env.Ctx, password); err != nil {
		return s.env.fail("vault unlock", err)
	}

	s.onOpen()

	return nil
}

// ChangePassword re-encrypts the storage key with password, which confirm repeats.
func (s *VaultService) ChangePassword(password, confirm string) error {
	return s.env.fail("vault change password", s.vault.ChangePassword(s.env.Ctx, password, confirm))
}
