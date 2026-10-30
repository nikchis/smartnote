package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

func newTestConnection(t *testing.T, saved domain.ConnectionSettings, srv *fakeServer) (*Connection, *fakeSettings, *Gate) {
	c, settings, gate, _ := newTestConnectionResets(t, saved, srv)
	return c, settings, gate
}

// newTestConnectionResets also returns a counter of settings-changed calls.
func newTestConnectionResets(t *testing.T, saved domain.ConnectionSettings, srv *fakeServer) (*Connection, *fakeSettings, *Gate, *int) {
	t.Helper()

	settings := &fakeSettings{s: saved}
	gate := &Gate{}
	resets := new(int)

	c, err := NewConnection(settings, srv, gate, &HostKeyOffer{}, func() { *resets++ })
	if err != nil {
		t.Fatal(err)
	}

	return c, settings, gate, resets
}

func TestNewConnectionNil(t *testing.T) {
	noop, offer := func() {}, &HostKeyOffer{}

	tests := []struct {
		name string
		call func() (*Connection, error)
	}{
		{name: "settings", call: func() (*Connection, error) { return NewConnection(nil, &fakeServer{}, &Gate{}, offer, noop) }},
		{name: "remote", call: func() (*Connection, error) { return NewConnection(&fakeSettings{}, nil, &Gate{}, offer, noop) }},
		{name: "gate", call: func() (*Connection, error) { return NewConnection(&fakeSettings{}, &fakeServer{}, nil, offer, noop) }},
		{name: "offer", call: func() (*Connection, error) { return NewConnection(&fakeSettings{}, &fakeServer{}, &Gate{}, nil, noop) }},
		{name: "changed", call: func() (*Connection, error) { return NewConnection(&fakeSettings{}, &fakeServer{}, &Gate{}, offer, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.call(); err == nil {
				t.Error("want an error for a nil dependency")
			}
		})
	}
}

func TestConnectionLoad(t *testing.T) {
	c, _, _ := newTestConnection(t, completeSettings(), &fakeServer{})

	got, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got != completeSettings() {
		t.Error("Load returned other settings")
	}
}

func TestConnectionTestUsesCheckMode(t *testing.T) {
	srv := &fakeServer{serverID: serverA}
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)

	res, err := c.Test(context.Background(), completeSettings())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}

	if res.Empty {
		t.Error("Empty = true for a server with a schema")
	}

	if len(srv.opens) != 1 || srv.opens[0] != OpenCheck {
		t.Errorf("opens = %v, want [OpenCheck]", srv.opens)
	}

	if srv.closed != 1 {
		t.Errorf("sessions closed = %d, want 1", srv.closed)
	}

	if len(settings.saved) != 0 {
		t.Errorf("Test saved settings %d times", len(settings.saved))
	}
}

func TestConnectionTestEmpty(t *testing.T) {
	srv := &fakeServer{noSchema: true}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)

	res, err := c.Test(context.Background(), completeSettings())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}

	if !res.Empty {
		t.Error("Empty = false for a server without meta")
	}

	if !srv.noSchema {
		t.Error("Test created the schema")
	}
}

func TestConnectionTestDefaultsDBHost(t *testing.T) {
	srv := &fakeServer{}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)
	s := completeSettings()
	s.DBHost = ""

	if _, err := c.Test(context.Background(), s); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if got := srv.settings[0].DBHost; got != domain.DefaultDBHost {
		t.Errorf("Open got DBHost %q, want %q", got, domain.DefaultDBHost)
	}
}

func TestConnectionTestValidates(t *testing.T) {
	srv := &fakeServer{}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)
	s := completeSettings()
	s.SSHPort = 0

	_, err := c.Test(context.Background(), s)
	assertField(t, err, "ssh_port")

	if _, err := c.Test(context.Background(), domain.ConnectionSettings{}); !errors.Is(err, domain.ErrNotConnected) {
		t.Errorf("Test of empty settings = %v, want ErrNotConnected", err)
	}

	if len(srv.opens) != 0 {
		t.Errorf("Open called %d times", len(srv.opens))
	}
}

func TestConnectionTestPassesHostKeyUnknown(t *testing.T) {
	srv := &fakeServer{openErr: &domain.HostKeyUnknownError{Fingerprint: "SHA256:abc", AuthorizedKey: "ssh-ed25519 AAAA"}}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)

	_, err := c.Test(context.Background(), completeSettings())

	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		t.Fatalf("Test = %v, want a *HostKeyUnknownError", err)
	}

	if hk.AuthorizedKey != "ssh-ed25519 AAAA" {
		t.Errorf("AuthorizedKey = %q", hk.AuthorizedKey)
	}
}

func TestConnectionTestBusy(t *testing.T) {
	srv := &fakeServer{}
	c, _, gate := newTestConnection(t, domain.ConnectionSettings{}, srv)

	leave, err := gate.Enter()
	if err != nil {
		t.Fatal(err)
	}
	defer leave()

	if _, err := c.Test(context.Background(), completeSettings()); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("Test = %v, want ErrBusy", err)
	}

	if len(srv.opens) != 0 {
		t.Errorf("Open called %d times", len(srv.opens))
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{})
	s := completeSettings()
	s.DBPort = 0

	assertField(t, c.Save(context.Background(), s), "db_port")

	if len(settings.saved) != 0 {
		t.Error("invalid settings were saved")
	}
}

func TestSaveDefaultsDBHost(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{})
	s := completeSettings()
	s.DBHost = ""

	if err := c.Save(context.Background(), s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := settings.s.DBHost; got != domain.DefaultDBHost {
		t.Errorf("saved DBHost %q, want %q", got, domain.DefaultDBHost)
	}
}

func TestSaveEmptyAllowed(t *testing.T) {
	c, settings, _ := newTestConnection(t, completeSettings(), &fakeServer{})

	if err := c.Save(context.Background(), domain.ConnectionSettings{}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !settings.s.IsEmpty() || settings.s.DBHost != "" {
		t.Errorf("saved %v, want empty settings without a default host", settings.s)
	}
}

// offered is the unknown key the fake server presents at the address of completeSettings.
func offered() *domain.HostKeyUnknownError {
	return &domain.HostKeyUnknownError{Fingerprint: "SHA256:abc", AuthorizedKey: "ssh-ed25519 AAAA", Addr: completeSettings().SSHAddr()}
}

func trusted() domain.ConnectionSettings {
	s := completeSettings()
	s.TrustedHostKey, s.TrustedHostAddr = "ssh-ed25519 AAAA", s.SSHAddr()

	return s
}

func TestTrustAndReset(t *testing.T) {
	c, settings, _ := newTestConnection(t, completeSettings(), &fakeServer{openErr: offered()})
	ctx := context.Background()

	if _, err := c.Test(ctx, completeSettings()); err == nil {
		t.Fatal("Test against an unknown key: nil error")
	}

	assertField(t, c.TrustHostKey(ctx, "SHA256:other"), "trusted_host_key")

	if err := c.TrustHostKey(ctx, "SHA256:abc"); err != nil {
		t.Fatalf("TrustHostKey: %v", err)
	}

	if settings.s != trusted() {
		t.Errorf("saved %+v, want the key bound to the server address", settings.s.TrustedHostAddr)
	}

	assertField(t, c.TrustHostKey(ctx, "SHA256:abc"), "trusted_host_key") // the offer is used up

	if err := c.ResetHostKey(ctx); err != nil {
		t.Fatalf("ResetHostKey: %v", err)
	}

	if settings.s != completeSettings() {
		t.Error("ResetHostKey did not clear only the key and its address")
	}
}

func TestTrustHostKeyWithoutOffer(t *testing.T) {
	c, settings, _ := newTestConnection(t, completeSettings(), &fakeServer{})
	assertField(t, c.TrustHostKey(context.Background(), "SHA256:abc"), "trusted_host_key")

	if len(settings.saved) != 0 {
		t.Error("a key nobody offered was saved")
	}
}

func TestTrustHostKeyOfferedByAnotherServer(t *testing.T) {
	offer := offered()
	offer.Addr = "other.example.org:22" // the settings changed after the offer
	c, settings, _ := newTestConnection(t, completeSettings(), &fakeServer{openErr: offer})
	other := completeSettings()

	other.SSHHost = "other.example.org"
	if _, err := c.Test(context.Background(), other); err == nil {
		t.Fatal("Test: nil error")
	}

	assertField(t, c.TrustHostKey(context.Background(), "SHA256:abc"), "trusted_host_key")

	if len(settings.saved) != 0 {
		t.Error("a key offered by another server was saved")
	}
}

func TestTrustOfferedKeyForDialog(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{openErr: offered()})
	if _, err := c.Test(context.Background(), completeSettings()); err == nil {
		t.Fatal("Test: nil error")
	}

	moved := completeSettings()

	moved.SSHPort = 2222
	if _, err := c.TrustOfferedKey("SHA256:abc", moved); err == nil {
		t.Error("TrustOfferedKey for settings with another port: nil error")
	}

	got, err := c.TrustOfferedKey("SHA256:abc", completeSettings())
	if err != nil {
		t.Fatalf("TrustOfferedKey: %v", err)
	}

	if got != trusted() {
		t.Errorf("TrustOfferedKey = key %q for %q, want the offered key for the dialog address", got.TrustedHostKey, got.TrustedHostAddr)
	}

	if len(settings.saved) != 0 {
		t.Error("TrustOfferedKey saved the settings: only OK saves")
	}
}

func TestSaveDropsStaleTrust(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{})
	s := trusted()
	s.SSHHost = "new.example.org" // trusted for the old server

	if err := c.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if settings.s.TrustedHostKey != "" || settings.s.TrustedHostAddr != "" {
		t.Errorf("saved key %q for %q, want none", settings.s.TrustedHostKey, settings.s.TrustedHostAddr)
	}

	if err := c.Save(context.Background(), trusted()); err != nil {
		t.Fatal(err)
	}

	if settings.s != trusted() {
		t.Error("a key bound to the current address was dropped")
	}
}

func TestTrustHostKeyEmpty(t *testing.T) {
	c, settings, _ := newTestConnection(t, completeSettings(), &fakeServer{})
	assertField(t, c.TrustHostKey(context.Background(), ""), "trusted_host_key")

	if len(settings.saved) != 0 {
		t.Error("an empty key was saved")
	}
}

func TestSettingsWritesBusy(t *testing.T) {
	c, settings, gate, resets := newTestConnectionResets(t, completeSettings(), &fakeServer{})

	leave, err := gate.Enter()
	if err != nil {
		t.Fatal(err)
	}
	defer leave()

	ctx := context.Background()

	writes := map[string]func() error{
		"Save":         func() error { return c.Save(ctx, completeSettings()) },
		"TrustHostKey": func() error { return c.TrustHostKey(ctx, "SHA256:abc") },
		"ResetHostKey": func() error { return c.ResetHostKey(ctx) },
	}
	for name, write := range writes {
		if err := write(); !errors.Is(err, domain.ErrBusy) {
			t.Errorf("%s = %v, want ErrBusy", name, err)
		}
	}

	if len(settings.saved) != 0 || *resets != 0 {
		t.Errorf("saved %d times, reset %d times; want none", len(settings.saved), *resets)
	}
}

func TestSettingsWritesResetConnection(t *testing.T) {
	c, _, _, resets := newTestConnectionResets(t, completeSettings(), &fakeServer{})
	ctx := context.Background()

	if err := c.Save(ctx, completeSettings()); err != nil {
		t.Fatal(err)
	}

	c.offer.remember(offered())

	if err := c.TrustHostKey(ctx, "SHA256:abc"); err != nil {
		t.Fatal(err)
	}

	if err := c.ResetHostKey(ctx); err != nil {
		t.Fatal(err)
	}

	if *resets != 3 {
		t.Errorf("settings-changed calls = %d, want 3", *resets)
	}

	if _, err := c.Test(ctx, completeSettings()); err != nil {
		t.Fatal(err)
	}

	s := completeSettings()
	s.SSHPort = 0
	_ = c.Save(ctx, s)

	if *resets != 3 {
		t.Errorf("settings-changed calls = %d after Test and a rejected Save, want 3", *resets)
	}
}
