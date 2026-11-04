package wailsui

import (
	"context"
	"log/slog"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// vaultStore opens with password only.
type vaultStore struct{ password string }

func (vaultStore) Exists(context.Context) (bool, error) { return true, nil }

func (vaultStore) Create(context.Context, string) error { return nil }

func (v vaultStore) Unlock(_ context.Context, password string) error {
	if password != v.password {
		return domain.ErrWrongPassword
	}

	return nil
}

func (vaultStore) ChangePassword(context.Context, string) error { return nil }

func newTestVault(t *testing.T, store usecase.VaultStore) *usecase.Vault {
	t.Helper()

	v, err := usecase.NewVault(store)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestVaultServiceOnOpen(t *testing.T) {
	const password = "correct horse battery"

	tests := []struct {
		name  string
		open  func(*VaultService) error
		wantN int
	}{
		{"unlock", func(s *VaultService) error { return s.Unlock(password) }, 1},
		{"wrong password", func(s *VaultService) error { return s.Unlock("wrong password!") }, 0},
		{"create", func(s *VaultService) error { return s.Create(password, password) }, 1},
		{"create with a different confirmation", func(s *VaultService) error { return s.Create(password, password+"!") }, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newTestVault(t, vaultStore{password: password})

			var opened int

			svc := NewVaultService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, v, func() { opened++ })

			err := tc.open(svc)
			if (err == nil) != (tc.wantN == 1) {
				t.Fatalf("err = %v", err)
			}

			if opened != tc.wantN {
				t.Errorf("onOpen calls = %d, want %d", opened, tc.wantN)
			}
		})
	}
}

func TestVaultServiceNilOnOpen(t *testing.T) {
	svc := NewVaultService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, newTestVault(t, vaultStore{password: "pw"}), nil)

	if err := svc.Unlock("pw"); err != nil {
		t.Fatalf("Unlock without onOpen: %v", err)
	}
}
