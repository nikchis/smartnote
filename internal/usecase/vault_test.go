package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

func TestNewVaultNil(t *testing.T) {
	if _, err := NewVault(nil); err == nil {
		t.Error("NewVault(nil) = nil error")
	}
}

func TestVaultCreateValidates(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		confirm   string
		wantField string
	}{
		{name: "5 characters", password: "12345", confirm: "12345", wantField: "password"},
		{name: "65 characters", password: strings.Repeat("a", 65), confirm: strings.Repeat("a", 65), wantField: "password"},
		{name: "confirmation differs", password: "123456", confirm: "123457", wantField: "confirm"},
		{name: "valid", password: "123456", confirm: "123456"},
		{name: "same password in nfc and nfd", password: "passéword", confirm: "passéword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeVaultStore{}

			v, err := NewVault(store)
			if err != nil {
				t.Fatal(err)
			}

			err = v.Create(context.Background(), tt.password, tt.confirm)
			assertField(t, err, tt.wantField)

			wantCalls := 1
			if tt.wantField != "" {
				wantCalls = 0
			}

			if len(store.created) != wantCalls {
				t.Errorf("store.Create called %d times, want %d", len(store.created), wantCalls)
			}
		})
	}
}

func TestVaultChangePasswordValidates(t *testing.T) {
	store := &fakeVaultStore{}

	v, err := NewVault(store)
	if err != nil {
		t.Fatal(err)
	}

	assertField(t, v.ChangePassword(context.Background(), "123456", "654321"), "confirm")
	assertField(t, v.ChangePassword(context.Background(), "123", "123"), "password")

	if len(store.changedPassword) != 0 {
		t.Fatalf("store.ChangePassword called with invalid input")
	}

	if err := v.ChangePassword(context.Background(), "new password", "new password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if len(store.changedPassword) != 1 || store.changedPassword[0] != "new password" {
		t.Errorf("store got %q, want [\"new password\"]", store.changedPassword)
	}
}

func TestVaultUnlockAndExists(t *testing.T) {
	store := &fakeVaultStore{exists: true}

	v, err := NewVault(store)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := v.Exists(context.Background())
	if err != nil || !ok {
		t.Fatalf("Exists() = %v, %v, want true, nil", ok, err)
	}

	if err := v.Unlock(context.Background(), "secret"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if len(store.unlocked) != 1 || store.unlocked[0] != "secret" {
		t.Errorf("store got %q", store.unlocked)
	}
}

// assertField checks that err is nil when wantField is empty, and otherwise a
// *domain.ValidationError for wantField.
func assertField(t *testing.T, err error, wantField string) {
	t.Helper()

	if wantField == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return
	}

	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a *domain.ValidationError", err)
	}

	if ve.Field != wantField {
		t.Errorf("Field = %q, want %q", ve.Field, wantField)
	}
}
