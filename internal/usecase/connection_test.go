package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
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

	f := newConnFixture(t, saved, srv)

	return f.c, f.settings, f.gate, f.resets
}

// connFixture is a Connection over fakes; the Yandex built-in client ID is empty.
type connFixture struct {
	c        *Connection
	settings *fakeSettings
	tokens   *fakeTokenStore
	store    *fakeConnectionStore
	auth     *fakeAuthorizer
	browser  *fakeBrowser
	srv      *fakeServer
	gate     *Gate
	resets   *int
}

// fakeBrowser records the URLs opened; err, when set, is returned instead.
type fakeBrowser struct {
	mu   sync.Mutex
	urls []string
	err  error
}

func (b *fakeBrowser) open(url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.err != nil {
		return b.err
	}

	b.urls = append(b.urls, url)

	return nil
}

func (b *fakeBrowser) opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.urls)
}

func newConnFixture(t *testing.T, saved domain.ConnectionSettings, srv *fakeServer) *connFixture {
	t.Helper()

	f := &connFixture{
		settings: &fakeSettings{s: saved},
		tokens:   &fakeTokenStore{},
		auth:     newFakeAuthorizer(),
		browser:  &fakeBrowser{},
		srv:      srv,
		gate:     &Gate{},
		resets:   new(int),
	}
	f.store = &fakeConnectionStore{settings: f.settings, tokens: f.tokens}

	c, err := NewConnection(f.deps())
	if err != nil {
		t.Fatal(err)
	}

	f.c = c

	return f
}

func (f *connFixture) deps() ConnectionDeps {
	return ConnectionDeps{
		Settings: f.settings, Store: f.store, Tokens: f.tokens, Remote: f.srv, Auth: f.auth,
		Gate: f.gate, Offer: &HostKeyOffer{}, Changed: func() { *f.resets++ }, OpenURL: f.browser.open,
	}
}

func TestNewConnectionNil(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	tests := []struct {
		name  string
		clear func(*ConnectionDeps)
	}{
		{name: "settings", clear: func(d *ConnectionDeps) { d.Settings = nil }},
		{name: "store", clear: func(d *ConnectionDeps) { d.Store = nil }},
		{name: "tokens", clear: func(d *ConnectionDeps) { d.Tokens = nil }},
		{name: "remote", clear: func(d *ConnectionDeps) { d.Remote = nil }},
		{name: "gate", clear: func(d *ConnectionDeps) { d.Gate = nil }},
		{name: "offer", clear: func(d *ConnectionDeps) { d.Offer = nil }},
		{name: "changed", clear: func(d *ConnectionDeps) { d.Changed = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := f.deps()
			tt.clear(&d)

			if _, err := NewConnection(d); err == nil {
				t.Error("want an error for a nil dependency")
			}
		})
	}

	d := f.deps()
	d.Auth, d.OpenURL, d.BuiltinClientID = nil, nil, ""

	if _, err := NewConnection(d); err != nil {
		t.Errorf("NewConnection without Auth, OpenURL and a built-in client ID: %v", err)
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

func TestConnectionTestAppliesDefaults(t *testing.T) {
	srv := &fakeServer{}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)
	s := completeSettings()
	s.DBHost, s.SSHPort, s.DBPort = "", 0, 0

	if _, err := c.Test(context.Background(), s); err != nil {
		t.Fatalf("Test with empty ports and db host: %v", err)
	}

	got := srv.settings[0]
	if got.DBHost != domain.DefaultDBHost || got.SSHPort != domain.DefaultSSHPort || got.DBPort != domain.DefaultDBPort {
		t.Errorf("Open got DBHost %q, SSHPort %d, DBPort %d; want %q, %d, %d",
			got.DBHost, got.SSHPort, got.DBPort, domain.DefaultDBHost, domain.DefaultSSHPort, domain.DefaultDBPort)
	}
}

func TestConnectionTestValidates(t *testing.T) {
	srv := &fakeServer{}
	c, _, _ := newTestConnection(t, domain.ConnectionSettings{}, srv)
	s := completeSettings()
	s.SSHPort = -1 // the form's value for a port that is not 1-65535

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
	s.DBPort = -1

	assertField(t, c.Save(context.Background(), s), "db_port")

	if len(settings.saved) != 0 {
		t.Error("invalid settings were saved")
	}
}

func TestSaveAppliesDefaults(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{})
	s := completeSettings()
	s.DBHost, s.SSHPort, s.DBPort = "", 0, 0

	if err := c.Save(context.Background(), s); err != nil {
		t.Fatalf("Save with empty ports and db host: %v", err)
	}

	got := settings.s
	if got.DBHost != domain.DefaultDBHost || got.SSHPort != domain.DefaultSSHPort || got.DBPort != domain.DefaultDBPort {
		t.Errorf("saved DBHost %q, SSHPort %d, DBPort %d; want %q, %d, %d",
			got.DBHost, got.SSHPort, got.DBPort, domain.DefaultDBHost, domain.DefaultSSHPort, domain.DefaultDBPort)
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

// TestTrustOfferedKeyEmptyPort: the dialog may leave the SSH port empty; the
// key offered for the default port must be trusted for it and survive OK.
func TestTrustOfferedKeyEmptyPort(t *testing.T) {
	c, settings, _ := newTestConnection(t, domain.ConnectionSettings{}, &fakeServer{openErr: offered()})
	form := completeSettings()
	form.SSHPort = 0

	if _, err := c.Test(context.Background(), form); err == nil {
		t.Fatal("Test: nil error")
	}

	got, err := c.TrustOfferedKey("SHA256:abc", form)
	if err != nil {
		t.Fatalf("TrustOfferedKey with an empty port: %v", err)
	}

	if err := c.Save(context.Background(), got); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if key := settings.s.TrustedKey(); key != "ssh-ed25519 AAAA" {
		t.Errorf("saved trusted key = %q, want the offered key for %s", key, settings.s.SSHAddr())
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
	s.SSHPort = -1
	_ = c.Save(ctx, s)

	if *resets != 3 {
		t.Errorf("settings-changed calls = %d after Test and a rejected Save, want 3", *resets)
	}
}

// yandexSettings are valid yandex_disk settings with the client ID clientID.
func yandexSettings(clientID string) domain.ConnectionSettings {
	return domain.ConnectionSettings{
		StorageKind: domain.KindYandexDisk, YandexClientID: clientID,
		SyncPhrase: strings.Repeat("p", domain.MinSyncPhraseLen),
	}
}

// storedToken is the token saved in local.db for client ID a.
var storedToken = CloudToken{Provider: "yandex_disk", ClientID: "a", Access: "old", Refresh: "old-r", Account: "stored"}

// issued is what the fake authorizer returns; Provider and ClientID are
// deliberately wrong, Connection must set them.
var issued = CloudToken{Provider: "other", ClientID: "other", Access: "new", Refresh: "new-r", Account: "me"}

type signInResult struct {
	account string
	err     error
}

// startSignIn runs SignIn in a goroutine and returns its result channel and
// the login it began.
func (f *connFixture) startSignIn(t *testing.T, s domain.ConnectionSettings) (<-chan signInResult, *fakeLogin) {
	t.Helper()

	done := make(chan signInResult, 1)

	go func() {
		account, err := f.c.SignIn(context.Background(), s)
		done <- signInResult{account: account, err: err}
	}()

	return done, recv(t, f.auth.begun, "BeginLoopback")
}

// signIn signs in with s and the token tok.
func (f *connFixture) signIn(t *testing.T, s domain.ConnectionSettings, tok CloudToken) {
	t.Helper()

	done, login := f.startSignIn(t, s)
	send(t, login.result, loginResult{tok: tok}, "the token")

	if r := recv(t, done, "SignIn"); r.err != nil || r.account != tok.Account {
		t.Fatalf("SignIn = %q, %v; want %q", r.account, r.err, tok.Account)
	}
}

// withStored puts storedToken into local.db.
func (f *connFixture) withStored() *connFixture {
	f.tokens.tok, f.tokens.has = storedToken, true
	return f
}

// assertNothingSaved fails if the settings or the token reached local.db.
func (f *connFixture) assertNothingSaved(t *testing.T) {
	t.Helper()

	if calls := f.store.saveCalls(); len(calls) != 0 {
		t.Errorf("SaveConnection calls = %d, want none", len(calls))
	}

	if len(f.tokens.saves) != 0 {
		t.Errorf("SaveCloudToken calls = %d, want none", len(f.tokens.saves))
	}
}

// assertSaved fails unless exactly one SaveConnection happened with change and tok.
func (f *connFixture) assertSaved(t *testing.T, change TokenChange, tok CloudToken) {
	t.Helper()

	calls := f.store.saveCalls()
	if len(calls) != 1 {
		t.Fatalf("SaveConnection calls = %d, want 1", len(calls))
	}

	if calls[0].Change != change || calls[0].Token != tok {
		t.Errorf("SaveConnection change %d, token %+v; want %d, %+v",
			calls[0].Change, calls[0].Token.Access, change, tok.Access)
	}
}

func pendingOf(tok CloudToken, clientID string) CloudToken {
	tok.Provider, tok.ClientID = string(domain.KindYandexDisk), clientID
	return tok
}

func TestSignInPendingNotSaved(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	f.signIn(t, yandexSettings("a"), issued)

	f.assertNothingSaved(t)

	if len(f.settings.saved) != 0 || *f.resets != 0 {
		t.Errorf("settings saved %d times, changed %d times; want none", len(f.settings.saved), *f.resets)
	}

	if got := f.browser.opened(); len(got) != 1 || got[0] != f.auth.logins[0].URL() {
		t.Errorf("opened URLs = %d, want the login URL once", len(got))
	}

	if calls := f.auth.beginCalls(); len(calls) != 1 || calls[0].clientID != "a" || calls[0].manual {
		t.Errorf("Begin calls = %+v, want one loopback for client a", calls)
	}

	account, ok, err := f.c.Account(context.Background(), yandexSettings("a"))
	if err != nil || !ok || account != "me" {
		t.Errorf("Account = %q, %v, %v; want the pending account", account, ok, err)
	}
}

func TestSaveWithPendingToken(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.signIn(t, yandexSettings("a"), issued)

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenSet, pendingOf(issued, "a"))

	if got := f.store.saveCalls()[0].Settings; got != yandexSettings("a").WithDefaults() {
		t.Errorf("saved settings %+v", got)
	}

	if *f.resets != 1 {
		t.Errorf("settings-changed calls = %d, want 1", *f.resets)
	}

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if calls := f.store.saveCalls(); calls[1].Change != TokenKeep {
		t.Errorf("second Save change = %d, want TokenKeep: the pending token is used up", calls[1].Change)
	}
}

func TestDiscardPendingOnCancel(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.signIn(t, yandexSettings("a"), issued)
	f.c.DiscardPending()

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenKeep, CloudToken{})

	if f.tokens.tok != storedToken {
		t.Errorf("stored token changed to %q", f.tokens.tok.Access)
	}
}

func TestSaveYandexWithoutSignIn(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")
	f.assertNothingSaved(t)

	if *f.resets != 0 {
		t.Errorf("settings-changed calls = %d, want none", *f.resets)
	}
}

func TestSaveYandexWithoutClientID(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	assertField(t, f.c.Save(context.Background(), yandexSettings("")), "yandex_client_id")
	f.assertNothingSaved(t)
}

// TestSaveReplacesTokenOfOtherClientID: the token of client a is replaced
// by the one signed in for client b; without a sign-in for b Save is refused
// and the token of a stays.
func TestSaveReplacesTokenOfOtherClientID(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	assertField(t, f.c.Save(context.Background(), yandexSettings("b")), "yandex_account")
	f.assertNothingSaved(t)

	f.signIn(t, yandexSettings("b"), issued)

	if err := f.c.Save(context.Background(), yandexSettings("b")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenSet, pendingOf(issued, "b"))

	if f.tokens.tok.ClientID != "b" {
		t.Errorf("stored token is for client %q, want b", f.tokens.tok.ClientID)
	}
}

func TestSignOutThenSave(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.c.SignOut()

	if account, ok, err := f.c.Account(context.Background(), yandexSettings("a")); err != nil || ok || account != "" {
		t.Errorf("Account after SignOut = %q, %v, %v; want none", account, ok, err)
	}

	assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")
	f.assertNothingSaved(t)

	if err := f.c.Save(context.Background(), completeSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenDelete, CloudToken{})

	if f.tokens.has {
		t.Error("the token is still stored")
	}
}

func TestSignOutThenDiscard(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.c.SignOut()
	f.c.DiscardPending()

	account, ok, err := f.c.Account(context.Background(), yandexSettings("a"))
	if err != nil || !ok || account != "stored" {
		t.Errorf("Account = %q, %v, %v; want the stored account", account, ok, err)
	}

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenKeep, CloudToken{})

	if f.tokens.tok != storedToken {
		t.Error("the stored token changed")
	}
}

func TestSignInThenSignOut(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.signIn(t, yandexSettings("a"), issued)
	f.c.SignOut()

	assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")

	if _, err := f.c.Test(context.Background(), yandexSettings("a")); err == nil {
		t.Error("Test after SignOut: nil error, want sign in required")
	} else {
		assertField(t, err, "yandex_account")
	}

	if len(f.srv.opens) != 0 {
		t.Errorf("Open called %d times", len(f.srv.opens))
	}
}

// TestTestRefreshPendingCancel: a refresh during Test stays in memory, and
// a discarded pending token never reaches local.db.
func TestTestRefreshPendingCancel(t *testing.T) {
	srv := &fakeServer{}
	f := newConnFixture(t, domain.ConnectionSettings{}, srv)
	f.withStored()
	f.signIn(t, yandexSettings("a"), issued)

	refreshed := pendingOf(issued, "a")
	refreshed.Access = "refreshed"

	var seen []CloudToken

	srv.onOpen = func(ctx context.Context, rt RemoteTarget) {
		tok, err := rt.Token.Token(ctx)
		if err != nil {
			t.Errorf("Token: %v", err)
		}

		seen = append(seen, tok)

		if err := rt.Token.Update(ctx, refreshed); err != nil {
			t.Errorf("Update: %v", err)
		}
	}

	for range 2 {
		if _, err := f.c.Test(context.Background(), yandexSettings("a")); err != nil {
			t.Fatalf("Test: %v", err)
		}
	}

	if len(seen) != 2 || seen[0] != pendingOf(issued, "a") || seen[1] != refreshed {
		t.Fatalf("tokens seen by Open = %d, want the pending one, then its refresh", len(seen))
	}

	f.c.DiscardPending()

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenKeep, CloudToken{})

	if len(f.tokens.saves) != 0 || f.tokens.tok != storedToken {
		t.Errorf("SaveCloudToken calls = %d; stored token changed: %v", len(f.tokens.saves), f.tokens.tok != storedToken)
	}
}

func TestTestUsesPendingSource(t *testing.T) {
	srv := &fakeServer{}
	f := newConnFixture(t, domain.ConnectionSettings{}, srv)
	f.withStored()
	f.signIn(t, yandexSettings("a"), issued)

	if _, err := f.c.Test(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Test: %v", err)
	}

	src := srv.targets[0].Token
	if src == nil {
		t.Fatal("Test passed no token source")
	}

	if tok, err := src.Token(context.Background()); err != nil || tok != pendingOf(issued, "a") {
		t.Errorf("Token = %q, %v; want the pending token", tok.Access, err)
	}

	f.assertNothingSaved(t)
}

func TestTestUsesStoredSource(t *testing.T) {
	srv := &fakeServer{}
	f := newConnFixture(t, domain.ConnectionSettings{}, srv)
	f.withStored()

	if _, err := f.c.Test(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Test: %v", err)
	}

	src := srv.targets[0].Token
	if src == nil {
		t.Fatal("Test passed no token source")
	}

	if tok, err := src.Token(context.Background()); err != nil || tok != storedToken {
		t.Errorf("Token = %q, %v; want the stored token", tok.Access, err)
	}

	if err := src.Update(context.Background(), issued); err != nil || len(f.tokens.saves) != 1 {
		t.Errorf("Update = %v, saves = %d; the stored source writes local.db", err, len(f.tokens.saves))
	}
}

func TestTestPostgresHasNoToken(t *testing.T) {
	srv := &fakeServer{}
	f := newConnFixture(t, domain.ConnectionSettings{}, srv)
	f.withStored()

	if _, err := f.c.Test(context.Background(), completeSettings()); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if srv.targets[0].Token != nil {
		t.Error("ssh_postgres target got a token source")
	}
}

func TestSignInTwiceReplacesFirst(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	first, login1 := f.startSignIn(t, yandexSettings("a"))
	second, login2 := f.startSignIn(t, yandexSettings("a"))

	if calls := f.auth.beginCalls(); len(calls) != 2 || !calls[1].prevClosed {
		t.Errorf("Begin calls = %+v, want the first login closed before the second begins", calls)
	}

	if !login1.isClosed() {
		t.Error("the first login is not closed")
	}

	if r := recv(t, first, "first SignIn"); !errors.Is(r.err, context.Canceled) {
		t.Errorf("first SignIn = %q, %v; want context.Canceled", r.account, r.err)
	}

	send(t, login2.result, loginResult{tok: issued}, "the token")

	if r := recv(t, second, "second SignIn"); r.err != nil || r.account != "me" {
		t.Errorf("second SignIn = %q, %v", r.account, r.err)
	}

	if !login2.isClosed() {
		t.Error("a finished login is not closed")
	}
}

func TestCancelSignInEndsWait(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	done, login := f.startSignIn(t, yandexSettings("a"))
	f.c.CancelSignIn()

	if r := recv(t, done, "SignIn"); !errors.Is(r.err, context.Canceled) {
		t.Errorf("SignIn = %v, want context.Canceled", r.err)
	}

	if !login.isClosed() {
		t.Error("the login is not closed")
	}

	if account, _, _ := f.c.Account(context.Background(), yandexSettings("a")); account != "stored" {
		t.Errorf("Account = %q, want the stored one", account)
	}
}

func TestSignInErrorReturned(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	denied := errors.New("denied")

	done, login := f.startSignIn(t, yandexSettings("a"))
	send(t, login.result, loginResult{err: denied}, "the error")

	if r := recv(t, done, "SignIn"); !errors.Is(r.err, denied) {
		t.Errorf("SignIn = %v, want %v", r.err, denied)
	}

	if !login.isClosed() {
		t.Error("a failed login is not closed")
	}

	if _, ok, _ := f.c.Account(context.Background(), yandexSettings("a")); ok {
		t.Error("signed in after a failed login")
	}
}

func TestSubmitCodeWithoutManual(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	if _, err := f.c.SubmitCode(context.Background(), "code"); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("SubmitCode without a login = %v, want ErrNoPendingOperation", err)
	}

	done, login := f.startSignIn(t, yandexSettings("a"))

	if _, err := f.c.SubmitCode(context.Background(), "code"); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("SubmitCode during a loopback login = %v, want ErrNoPendingOperation", err)
	}

	f.c.CancelSignIn()
	recv(t, done, "SignIn")

	if len(login.submitted()) != 0 {
		t.Errorf("Exchange called on a loopback login")
	}
}

func TestSignInManualSubmitCode(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	if err := f.c.SignInManual(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("SignInManual: %v", err)
	}

	login := recv(t, f.auth.begun, "BeginManual")

	if calls := f.auth.beginCalls(); len(calls) != 1 || !calls[0].manual || calls[0].clientID != "a" {
		t.Errorf("Begin calls = %+v, want one manual for client a", calls)
	}

	if got := f.browser.opened(); len(got) != 1 || got[0] != login.URL() {
		t.Errorf("opened URLs = %d, want the login URL once", len(got))
	}

	done := make(chan signInResult, 1)

	go func() {
		account, err := f.c.SubmitCode(context.Background(), "123456")
		done <- signInResult{account: account, err: err}
	}()

	send(t, login.result, loginResult{tok: issued}, "the token")

	if r := recv(t, done, "SubmitCode"); r.err != nil || r.account != "me" {
		t.Fatalf("SubmitCode = %q, %v", r.account, r.err)
	}

	if codes := login.submitted(); login.waitCalls() != 0 || len(codes) != 1 || codes[0] != "123456" {
		t.Errorf("Wait calls = %d, codes = %d; want Exchange with the code only", login.waitCalls(), len(codes))
	}

	if !login.isClosed() {
		t.Error("a finished manual login is not closed")
	}

	if _, err := f.c.SubmitCode(context.Background(), "again"); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("SubmitCode after the login ended = %v, want ErrNoPendingOperation", err)
	}

	if err := f.c.Save(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenSet, pendingOf(issued, "a"))
}

// TestSubmitCodeRetry: a rejected code keeps the manual login for another try.
func TestSubmitCodeRetry(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	if err := f.c.SignInManual(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("SignInManual: %v", err)
	}

	login := recv(t, f.auth.begun, "BeginManual")
	bad := errors.New("bad code")

	for _, res := range []loginResult{{err: bad}, {tok: issued}} {
		done := make(chan error, 1)

		go func() {
			_, err := f.c.SubmitCode(context.Background(), "code")
			done <- err
		}()

		send(t, login.result, res, "the exchange result")

		if err := recv(t, done, "SubmitCode"); !errors.Is(err, res.err) {
			t.Fatalf("SubmitCode = %v, want %v", err, res.err)
		}
	}

	if account, ok, _ := f.c.Account(context.Background(), yandexSettings("a")); !ok || account != "me" {
		t.Errorf("Account = %q, %v; want the pending account", account, ok)
	}
}

func TestCancelSignInEndsExchange(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	if err := f.c.SignInManual(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("SignInManual: %v", err)
	}

	login := recv(t, f.auth.begun, "BeginManual")
	done := make(chan error, 1)

	go func() {
		_, err := f.c.SubmitCode(context.Background(), "code")
		done <- err
	}()

	recv(t, login.entered, "Exchange")
	f.c.CancelSignIn()

	if err := recv(t, done, "SubmitCode"); !errors.Is(err, context.Canceled) {
		t.Errorf("SubmitCode = %v, want context.Canceled", err)
	}

	if !login.isClosed() {
		t.Error("the login is not closed")
	}
}

func TestCancelWithoutSignIn(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	f.c.CancelSignIn()
	f.c.DiscardPending()

	if account, ok, err := f.c.Account(context.Background(), yandexSettings("a")); err != nil || !ok || account != "stored" {
		t.Errorf("Account = %q, %v, %v", account, ok, err)
	}
}

func TestSignInNotInGate(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	done, login := f.startSignIn(t, yandexSettings("a"))

	leave, err := f.gate.Enter()
	if err != nil {
		t.Fatalf("Gate.Enter during SignIn: %v", err)
	}

	// The sign-in finishes while the Gate is held by someone else.
	send(t, login.result, loginResult{tok: issued}, "the token")

	if r := recv(t, done, "SignIn"); r.err != nil {
		t.Errorf("SignIn with the Gate held: %v", r.err)
	}

	leave()
}

func TestSignInLoopbackUnavailable(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.auth.beginErr = domain.ErrLoopbackUnavailable

	if _, err := f.c.SignIn(context.Background(), yandexSettings("a")); !errors.Is(err, domain.ErrLoopbackUnavailable) {
		t.Errorf("SignIn = %v, want ErrLoopbackUnavailable", err)
	}

	if len(f.browser.opened()) != 0 {
		t.Error("the browser opened without a login")
	}

	if _, err := f.c.SubmitCode(context.Background(), "code"); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("SubmitCode = %v, want ErrNoPendingOperation", err)
	}
}

func TestSignInOpenURLFails(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	noBrowser := errors.New("no browser")
	f.browser.err = noBrowser

	if _, err := f.c.SignIn(context.Background(), yandexSettings("a")); !errors.Is(err, noBrowser) {
		t.Errorf("SignIn = %v, want %v", err, noBrowser)
	}

	if err := f.c.SignInManual(context.Background(), yandexSettings("a")); !errors.Is(err, noBrowser) {
		t.Errorf("SignInManual = %v, want %v", err, noBrowser)
	}

	for i, l := range f.auth.logins {
		if !l.isClosed() || l.waitCalls() != 0 {
			t.Errorf("login %d: closed %v, Wait calls %d; want closed without Wait", i, l.isClosed(), l.waitCalls())
		}
	}

	if _, err := f.c.SubmitCode(context.Background(), "code"); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("SubmitCode = %v, want ErrNoPendingOperation", err)
	}
}

func TestSignInPreconditions(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	assertSignInErr := func(t *testing.T, c *Connection, s domain.ConnectionSettings, check func(error)) {
		t.Helper()

		_, err := c.SignIn(context.Background(), s)
		check(err)
		check(c.SignInManual(context.Background(), s))
	}

	t.Run("no authorizer", func(t *testing.T) {
		d := f.deps()
		d.Auth = nil

		c, err := NewConnection(d)
		if err != nil {
			t.Fatal(err)
		}

		assertSignInErr(t, c, yandexSettings("a"), func(err error) {
			if !errors.Is(err, domain.ErrNotConnected) {
				t.Errorf("err = %v, want ErrNotConnected", err)
			}
		})
	})

	t.Run("no client ID", func(t *testing.T) {
		assertSignInErr(t, f.c, yandexSettings(""), func(err error) { assertField(t, err, "yandex_client_id") })
	})

	t.Run("built-in client ID", func(t *testing.T) {
		d := f.deps()
		d.BuiltinClientID = "builtin"

		c, err := NewConnection(d)
		if err != nil {
			t.Fatal(err)
		}

		if err := c.SignInManual(context.Background(), yandexSettings("")); err != nil {
			t.Fatalf("SignInManual: %v", err)
		}

		recv(t, f.auth.begun, "BeginManual")

		if calls := f.auth.beginCalls(); len(calls) != 1 || calls[0].clientID != "builtin" {
			t.Errorf("Begin calls = %+v, want the built-in client ID", calls)
		}

		c.CancelSignIn()
	})
}

func TestSavePostgresUnchanged(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	if err := f.c.Save(context.Background(), completeSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f.assertSaved(t, TokenKeep, CloudToken{})

	if got := f.store.saveCalls()[0].Settings; got != completeSettings() {
		t.Errorf("saved settings %+v", got)
	}

	if f.tokens.tok != storedToken {
		t.Error("the stored token changed")
	}
}

func TestSaveCancelsSignIn(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})

	done, login := f.startSignIn(t, yandexSettings("a"))

	if err := f.c.Save(context.Background(), completeSettings()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !login.isClosed() {
		t.Error("Save left the login open")
	}

	if r := recv(t, done, "SignIn"); !errors.Is(r.err, context.Canceled) {
		t.Errorf("SignIn = %v, want context.Canceled", r.err)
	}
}

func TestPendingDroppedOnClientIDChange(t *testing.T) {
	t.Run("Test", func(t *testing.T) {
		srv := &fakeServer{}
		f := newConnFixture(t, domain.ConnectionSettings{}, srv)
		f.signIn(t, yandexSettings("a"), issued)

		if _, err := f.c.Test(context.Background(), yandexSettings("b")); err != nil {
			t.Fatalf("Test: %v", err)
		}

		if _, err := srv.targets[0].Token.Token(context.Background()); !errors.Is(err, domain.ErrReauthRequired) {
			t.Errorf("Token for client b = %v, want ErrReauthRequired: the pending token is of client a", err)
		}

		assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")
		f.assertNothingSaved(t)
	})

	t.Run("Save", func(t *testing.T) {
		f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
		f.signIn(t, yandexSettings("a"), issued)

		assertField(t, f.c.Save(context.Background(), yandexSettings("b")), "yandex_account")
		assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")
		f.assertNothingSaved(t)
	})
}

func TestAccount(t *testing.T) {
	f := newConnFixture(t, domain.ConnectionSettings{}, &fakeServer{})
	f.withStored()

	tests := []struct {
		name     string
		s        domain.ConnectionSettings
		account  string
		signedIn bool
	}{
		{name: "stored", s: yandexSettings("a"), account: "stored", signedIn: true},
		{name: "other client ID", s: yandexSettings("b")},
		{name: "no client ID", s: yandexSettings("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, ok, err := f.c.Account(context.Background(), tt.s)
			if err != nil || ok != tt.signedIn || account != tt.account {
				t.Errorf("Account = %q, %v, %v; want %q, %v", account, ok, err, tt.account, tt.signedIn)
			}
		})
	}

	f.tokens.loadErr = errStore
	if _, _, err := f.c.Account(context.Background(), yandexSettings("a")); !errors.Is(err, errStore) {
		t.Errorf("Account with a failing store = %v, want %v", err, errStore)
	}
}

// TestRefreshAfterDiscardIgnored: a refresh that arrives after Cancel does
// not bring the discarded token back.
func TestRefreshAfterDiscardIgnored(t *testing.T) {
	srv := &fakeServer{}
	f := newConnFixture(t, domain.ConnectionSettings{}, srv)
	f.signIn(t, yandexSettings("a"), issued)

	srv.onOpen = func(ctx context.Context, rt RemoteTarget) {
		f.c.DiscardPending()

		if err := rt.Token.Update(ctx, pendingOf(issued, "a")); err != nil {
			t.Errorf("Update: %v", err)
		}
	}

	if _, err := f.c.Test(context.Background(), yandexSettings("a")); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if _, ok, _ := f.c.Account(context.Background(), yandexSettings("a")); ok {
		t.Error("the discarded token came back with its refresh")
	}

	assertField(t, f.c.Save(context.Background(), yandexSettings("a")), "yandex_account")
}
