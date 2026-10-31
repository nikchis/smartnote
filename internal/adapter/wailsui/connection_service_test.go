package wailsui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

type memSettings struct {
	s     domain.ConnectionSettings
	saves int
}

func (m *memSettings) LoadSettings(context.Context) (domain.ConnectionSettings, error) {
	return m.s, nil
}

func (m *memSettings) SaveSettings(_ context.Context, s domain.ConnectionSettings) error {
	m.s = s
	m.saves++

	return nil
}

type emptySession struct{ usecase.RemoteSession }

func (emptySession) Empty() bool  { return true }
func (emptySession) Close() error { return nil }

// keyServer is an SSH server stand-in: it presents key and lets in only a
// client that trusts that key for its address.
type keyServer struct {
	key  string
	fp   string
	seen domain.ConnectionSettings
}

func (k *keyServer) Open(_ context.Context, s domain.ConnectionSettings, _ usecase.OpenMode) (usecase.RemoteSession, error) {
	k.seen = s
	if s.TrustedKey() != k.key {
		return nil, &domain.HostKeyUnknownError{Fingerprint: k.fp, AuthorizedKey: k.key, Addr: s.SSHAddr()}
	}

	return emptySession{}, nil
}

func newHostKey(t *testing.T) (authorized, fp string) {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))), gossh.FingerprintSHA256(key)
}

func newTestConnectionService(t *testing.T, saved domain.ConnectionSettings) (*ConnectionService, *memSettings, *keyServer) {
	t.Helper()

	store := &memSettings{s: saved}
	server := &keyServer{}
	server.key, server.fp = newHostKey(t)

	conn, err := usecase.NewConnection(store, server, &usecase.Gate{}, &usecase.HostKeyOffer{}, func() {})
	if err != nil {
		t.Fatal(err)
	}

	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}

	return NewConnectionService(env, conn), store, server
}

var filledSettings = domain.ConnectionSettings{
	SSHHost: "server", SSHPort: 22, SSHUser: "u", DBHost: "127.0.0.1", DBPort: 5432,
	DBName: "db", DBUser: "dbu", SyncPhrase: "sixteen chars ok",
}

func uiCode(t *testing.T, err error) (code, field string) {
	t.Helper()

	var ui *UIError
	if !errors.As(err, &ui) {
		t.Fatalf("error %v is not a UIError", err)
	}

	return ui.Code, ui.Field
}

func TestSettingsDTOCarriesNoHostKey(t *testing.T) {
	b, err := json.Marshal(SettingsDTO{})
	if err != nil {
		t.Fatal(err)
	}

	if s := string(b); strings.Contains(s, "trustedHostKey") || strings.Contains(s, "trustedHostAddr") {
		t.Errorf("SettingsDTO JSON = %s, want no server key: only its fingerprint crosses to the interface", s)
	}
}

func TestTrustOfferedKeyIsSavedOnlyWithOK(t *testing.T) {
	svc, store, server := newTestConnectionService(t, domain.ConnectionSettings{})
	form := toSettingsDTO(filledSettings)

	if _, err := svc.Test(form); err == nil {
		t.Fatal("Test with an unknown server key succeeded")
	}

	form, err := svc.TrustOfferedKey(server.fp, form)
	if err != nil {
		t.Fatalf("TrustOfferedKey: %v", err)
	}

	if form.TrustedFingerprint != server.fp {
		t.Errorf("TrustedFingerprint = %q, want %q", form.TrustedFingerprint, server.fp)
	}

	if store.saves != 0 {
		t.Fatalf("TrustOfferedKey saved the settings")
	}

	empty, err := svc.Test(form)
	if err != nil || !empty {
		t.Fatalf("Test after trusting = %v, %v; want the trusted key used", empty, err)
	}

	if err := svc.Save(form); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.s.TrustedHostKey != server.key || store.s.TrustedHostAddr != "server:22" {
		t.Errorf("saved trust = %q for %q, want the offered key for server:22", store.s.TrustedHostKey, store.s.TrustedHostAddr)
	}
}

func TestSaveRejectsFingerprintNobodyOffered(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, domain.ConnectionSettings{})
	_, otherFP := newHostKey(t)

	form := toSettingsDTO(filledSettings)
	form.TrustedFingerprint = otherFP

	err := svc.Save(form)
	if code, field := uiCode(t, err); code != "validation" || field != "trusted_host_key" {
		t.Errorf("Save = %s/%s, want validation of trusted_host_key", code, field)
	}

	if store.saves != 0 {
		t.Errorf("settings saved with a key the server never offered")
	}
}

func TestSaveKeepsTheSavedKey(t *testing.T) {
	key, _ := newHostKey(t)
	saved := filledSettings
	saved.TrustedHostKey, saved.TrustedHostAddr = key, "server:22"
	svc, store, _ := newTestConnectionService(t, saved)

	form, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Save(form); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.s.TrustedHostKey != key || store.s.TrustedHostAddr != "server:22" {
		t.Errorf("saved trust = %q for %q, want the key kept", store.s.TrustedHostKey, store.s.TrustedHostAddr)
	}
}

func TestSaveKeepsAnUnreadableSavedKey(t *testing.T) {
	saved := filledSettings
	saved.TrustedHostKey, saved.TrustedHostAddr = "not a key", "server:22"
	svc, store, _ := newTestConnectionService(t, saved)

	form, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	if form.TrustedFingerprint == "" {
		t.Fatal("TrustedFingerprint is empty for an unreadable key, Save would drop it silently")
	}

	if err := svc.Save(form); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.s.TrustedHostKey != "not a key" {
		t.Errorf("TrustedHostKey = %q, want the unreadable key kept", store.s.TrustedHostKey)
	}
}

func TestSaveWithoutFingerprintResetsTrust(t *testing.T) {
	key, _ := newHostKey(t)
	saved := filledSettings
	saved.TrustedHostKey, saved.TrustedHostAddr = key, "server:22"
	svc, store, _ := newTestConnectionService(t, saved)

	form, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	form.TrustedFingerprint = ""
	if err := svc.Save(form); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.s.TrustedHostKey != "" || store.s.TrustedHostAddr != "" {
		t.Errorf("saved trust = %q for %q, want it reset", store.s.TrustedHostKey, store.s.TrustedHostAddr)
	}
}

func TestReopenedSettingsForgetAnUnsavedTrust(t *testing.T) {
	svc, store, server := newTestConnectionService(t, domain.ConnectionSettings{})
	form := toSettingsDTO(filledSettings)

	if _, err := svc.Test(form); err == nil {
		t.Fatal("Test with an unknown server key succeeded")
	}

	if _, err := svc.TrustOfferedKey(server.fp, form); err != nil {
		t.Fatal(err)
	}

	// Cancel, then the dialog opens again.
	if _, err := svc.Load(); err != nil {
		t.Fatal(err)
	}

	form.TrustedFingerprint = server.fp
	if code, _ := uiCode(t, svc.Save(form)); code != "validation" {
		t.Errorf("Save after reopening = %s, want validation: the trust was canceled", code)
	}

	if store.saves != 0 {
		t.Errorf("a canceled trust was saved")
	}
}

func TestSaveRejectsAnotherKeyInsteadOfTheSavedOne(t *testing.T) {
	key, _ := newHostKey(t)
	saved := filledSettings
	saved.TrustedHostKey, saved.TrustedHostAddr = key, "server:22"
	svc, store, _ := newTestConnectionService(t, saved)
	_, otherFP := newHostKey(t)

	form := toSettingsDTO(filledSettings)
	form.TrustedFingerprint = otherFP

	if code, _ := uiCode(t, svc.Save(form)); code != "validation" {
		t.Errorf("Save = %s, want validation", code)
	}

	if store.saves != 0 || store.s.TrustedHostKey != key {
		t.Errorf("saved trust changed to a key the server never offered")
	}
}
