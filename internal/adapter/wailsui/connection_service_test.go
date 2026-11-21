package wailsui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

type memSettings struct {
	s        domain.ConnectionSettings
	saves    int
	token    usecase.CloudToken // the Yandex token
	hasToken bool
	google   *usecase.CloudToken // the Google token, if any
	tokenErr error               // of LoadCloudToken
}

func (m *memSettings) LoadSettings(context.Context) (domain.ConnectionSettings, error) {
	return m.s, nil
}

func (m *memSettings) SaveSettings(_ context.Context, s domain.ConnectionSettings) error {
	m.s = s
	m.saves++

	return nil
}

// SaveConnection saves the settings as SaveSettings does; the token is not
// kept, these tests never sign in.
func (m *memSettings) SaveConnection(ctx context.Context, s domain.ConnectionSettings, _ []usecase.TokenUpdate) error {
	return m.SaveSettings(ctx, s)
}

func (m *memSettings) LoadCloudToken(_ context.Context, p domain.StorageKind) (usecase.CloudToken, bool, error) {
	if p == domain.KindGoogleDrive && m.google != nil {
		return *m.google, true, m.tokenErr
	}

	return m.token, m.hasToken && p == domain.KindYandexDisk, m.tokenErr
}

func (m *memSettings) SaveCloudToken(context.Context, usecase.CloudToken) error { return nil }

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

func (k *keyServer) Open(_ context.Context, t usecase.RemoteTarget, _ usecase.OpenMode) (usecase.RemoteSession, error) {
	s := t.Settings
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

	conn, err := usecase.NewConnection(usecase.ConnectionDeps{
		Settings: store, Store: store, Tokens: store, Remote: server,
		Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Changed: func() {},
	})
	if err != nil {
		t.Fatal(err)
	}

	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}

	return NewConnectionService(env, conn, nil), store, server
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

func TestSettingsDTONeverHasTokens(t *testing.T) {
	typ := reflect.TypeFor[SettingsDTO]()
	for f := range typ.Fields() {
		name := strings.ToLower(f.Name)
		if strings.Contains(name, "token") || strings.Contains(name, "refresh") || strings.Contains(name, "access") {
			t.Errorf("SettingsDTO field %s may carry a token", f.Name)
		}
	}

	svc, store, _ := newTestConnectionService(t, domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "c"})
	store.token = usecase.CloudToken{ClientID: "c", Account: "ivan", Access: "ACCESS-SECRET", Refresh: "REFRESH-SECRET"}
	store.hasToken = true

	dto, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(dto)
	if s := string(b); strings.Contains(s, "SECRET") || !strings.Contains(s, `"yandexAccount":"ivan"`) || !strings.Contains(s, `"yandexSignedIn":true`) {
		t.Errorf("Load JSON = %s", s)
	}
}

// TestLoadShowsYandexAccountForOtherKind: until the form asks for the
// account of its draft, Load reports the Yandex account whatever kind is
// saved, so that switching the form to Yandex Disk shows it.
func TestLoadShowsYandexAccountForOtherKind(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, filledSettings)
	store.token = usecase.CloudToken{Provider: domain.KindYandexDisk, ClientID: "c", Account: "ivan"}
	store.hasToken = true
	store.s.YandexClientID = "c"

	dto, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	if !dto.YandexSignedIn || dto.YandexAccount != "ivan" {
		t.Errorf("Load = %q, %v; want the Yandex account", dto.YandexAccount, dto.YandexSignedIn)
	}
}

// TestLoadWithUnreadableToken: a token that cannot be read still opens
// Settings, as Not signed in, and only the error's code is logged.
func TestLoadWithUnreadableToken(t *testing.T) {
	saved := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "c", SyncPhrase: "sixteen chars ok"}
	svc, store, _ := newTestConnectionService(t, saved)
	store.tokenErr = errors.New("cipher: message authentication failed SECRET-DETAIL")

	var logged strings.Builder

	svc.env.Log = slog.New(slog.NewTextHandler(&logged, nil))

	dto, err := svc.Load()
	if err != nil {
		t.Fatalf("Load: %v, want the settings without the account", err)
	}

	if dto.Kind != string(domain.KindYandexDisk) || dto.YandexClientID != "c" || dto.YandexSignedIn || dto.YandexAccount != "" {
		t.Errorf("Load = %+v, want the saved settings, not signed in", dto)
	}

	if out := logged.String(); !strings.Contains(out, "code=") || strings.Contains(out, "SECRET-DETAIL") {
		t.Errorf("log = %q, want the code only", out)
	}
}

type fakeAuth struct {
	clientID string
	login    *fakeLogin
}

func (a *fakeAuth) BeginLoopback(_ context.Context, c usecase.CloudClient) (usecase.CloudLogin, error) {
	a.clientID = c.ID
	return a.login, nil
}

func (a *fakeAuth) BeginManual(_ context.Context, c usecase.CloudClient) (usecase.CloudLogin, error) {
	a.clientID = c.ID
	return a.login, nil
}

type fakeLogin struct{}

func (*fakeLogin) URL() string { return "https://oauth.example/authorize" }
func (*fakeLogin) Wait(context.Context) (usecase.CloudToken, error) {
	return usecase.CloudToken{ClientID: "form-id", Account: "ivan"}, nil
}
func (*fakeLogin) Exchange(context.Context, string) (usecase.CloudToken, error) {
	return usecase.CloudToken{ClientID: "form-id", Account: "ivan"}, nil
}
func (*fakeLogin) Close() error { return nil }

func TestSignInUsesFormClientID(t *testing.T) {
	svc, auth := newYandexService(t)

	account, err := svc.SignIn(SettingsDTO{Kind: "yandex_disk", YandexClientID: "form-id", SyncPhrase: "sixteen chars ok"})
	if err != nil {
		t.Fatal(err)
	}

	if account != "ivan" || auth.clientID != "form-id" {
		t.Errorf("account = %q, client ID = %q, want ivan and form-id", account, auth.clientID)
	}
}

func TestSignInManualAndSubmit(t *testing.T) {
	svc, auth := newYandexService(t)

	if err := svc.SignInManual(SettingsDTO{Kind: "yandex_disk", YandexClientID: "form-id"}); err != nil {
		t.Fatal(err)
	}

	account, err := svc.SubmitCode("code")
	if err != nil || account != "ivan" || auth.clientID != "form-id" {
		t.Errorf("SubmitCode = %q, %v; client ID %q", account, err, auth.clientID)
	}

	svc.SignOut(string(domain.KindYandexDisk))
	svc.CancelSignIn()
}

func TestSignInErrorsAreUIErrors(t *testing.T) {
	svc, _, _ := newTestConnectionService(t, domain.ConnectionSettings{})

	_, err := svc.SignIn(SettingsDTO{Kind: "yandex_disk"})
	if code, _ := uiCode(t, err); code != "not_connected" {
		t.Errorf("code = %q, want not_connected without an authorizer", code)
	}
}

func newYandexService(t *testing.T) (*ConnectionService, *fakeAuth) {
	t.Helper()

	store := &memSettings{}
	auth := &fakeAuth{login: &fakeLogin{}}

	conn, err := usecase.NewConnection(usecase.ConnectionDeps{
		Settings: store, Store: store, Tokens: store, Remote: &keyServer{},
		Auth: map[domain.StorageKind]usecase.CloudAuthorizer{domain.KindYandexDisk: auth},
		Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Changed: func() {},
		OpenURL: func(string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	return NewConnectionService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, conn, nil), auth
}

// Test may run a transfer with the storage: whatever its result, the
// interface is told that the progress is over, as after the sync operations.
func TestConnectionTestEndsProgress(t *testing.T) {
	svc, _, server := newTestConnectionService(t, domain.ConnectionSettings{})

	var log eventLog

	svc.progress = NewProgressFeed(log.emit)

	wantEnd := func(step string) {
		t.Helper()

		got := log.list()
		if len(got) == 0 || got[len(got)-1].name != EventSyncProgress || !reflect.DeepEqual(got[len(got)-1].data, []any{ProgressDTO{}}) {
			t.Errorf("%s: events %+v, want the end of the progress last", step, got)
		}
	}

	form := toSettingsDTO(filledSettings)

	if _, err := svc.Test(form); err == nil {
		t.Fatal("Test with an unknown server key succeeded")
	}

	wantEnd("failure")

	form, err := svc.TrustOfferedKey(server.fp, form)
	if err != nil {
		t.Fatalf("TrustOfferedKey: %v", err)
	}

	before := len(log.list())

	if _, err := svc.Test(form); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if len(log.list()) == before {
		t.Error("successful Test sent no event")
	}

	wantEnd("success")
}

// ownGoogle is a draft of the Google Drive settings with an own client.
var ownGoogle = SettingsDTO{
	Kind: string(domain.KindGoogleDrive), SyncPhrase: "sixteen chars ok",
	GoogleClientID: "1-abc.apps.googleusercontent.com", GoogleClientSecret: "GOCSPX-OWN-SECRET",
}

// TestSettingsDTONeverHasGoogleTokens: neither the settings nor the
// account of a Google draft carry a token.
func TestSettingsDTONeverHasGoogleTokens(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, ownGoogle.settings())
	store.google = &usecase.CloudToken{Provider: domain.KindGoogleDrive, ClientID: ownGoogle.GoogleClientID, Access: "ACCESS-SECRET", Refresh: "REFRESH-SECRET"}

	dto, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}

	acc, err := svc.Account(dto)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range []any{dto, acc} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "ACCESS-SECRET") || strings.Contains(string(b), "REFRESH-SECRET") {
			t.Errorf("JSON %s carries a token", b)
		}
	}

	if !acc.SignedIn {
		t.Errorf("Account = %+v, want signed in with the saved token", acc)
	}
}

// TestAccountByDraftKind: Account reports the sign-in of the kind and the
// client of the draft, not of the saved settings.
func TestAccountByDraftKind(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "c", SyncPhrase: "sixteen chars ok"})
	store.token = usecase.CloudToken{Provider: domain.KindYandexDisk, ClientID: "c", Account: "ivan"}
	store.hasToken = true

	got, err := svc.Account(ownGoogle)
	if err != nil || got != (AccountDTO{}) {
		t.Errorf("Account(google draft) = %+v, %v; want not signed in", got, err)
	}

	got, err = svc.Account(SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "c"})
	if err != nil || got != (AccountDTO{Account: "ivan", SignedIn: true}) {
		t.Errorf("Account(yandex draft) = %+v, %v; want ivan", got, err)
	}

	got, err = svc.Account(SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "other"})
	if err != nil || got.SignedIn {
		t.Errorf("Account(yandex draft, other client) = %+v, %v; want not signed in", got, err)
	}
}

// TestAccountKeepsPendingSignIn: unlike Load, Account keeps the token of a
// sign-in waiting for OK.
func TestAccountKeepsPendingSignIn(t *testing.T) {
	svc, _ := newYandexService(t)
	form := SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "form-id", SyncPhrase: "sixteen chars ok"}

	if _, err := svc.SignIn(form); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		got, err := svc.Account(form)
		if err != nil || got != (AccountDTO{Account: "ivan", SignedIn: true}) {
			t.Fatalf("Account after the sign-in = %+v, %v; want the pending ivan", got, err)
		}
	}

	if _, err := svc.Load(); err != nil {
		t.Fatal(err)
	}

	if got, err := svc.Account(form); err != nil || got.SignedIn {
		t.Errorf("Account after Load = %+v, %v; want the pending sign-in discarded", got, err)
	}
}

// TestAccountWithUnreadableToken: a token that cannot be read is Not
// signed in, and only the code of the error is logged.
func TestAccountWithUnreadableToken(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, domain.ConnectionSettings{})
	store.tokenErr = errors.New("cipher: message authentication failed SECRET-DETAIL")

	var logged strings.Builder

	svc.env.Log = slog.New(slog.NewTextHandler(&logged, nil))

	got, err := svc.Account(SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "c"})
	if err != nil || got.SignedIn {
		t.Errorf("Account = %+v, %v; want not signed in without an error", got, err)
	}

	if out := logged.String(); !strings.Contains(out, "code=") || strings.Contains(out, "SECRET-DETAIL") {
		t.Errorf("log = %q, want the code only", out)
	}
}

// TestSignInByDraftKind: the sign-in goes to the authorizer of the draft's
// kind with the own client of the form.
func TestSignInByDraftKind(t *testing.T) {
	store := &memSettings{}
	yandex, google := &fakeAuth{login: &fakeLogin{}}, &fakeAuth{login: &fakeLogin{}}

	conn, err := usecase.NewConnection(usecase.ConnectionDeps{
		Settings: store, Store: store, Tokens: store, Remote: &keyServer{},
		Auth: map[domain.StorageKind]usecase.CloudAuthorizer{domain.KindYandexDisk: yandex, domain.KindGoogleDrive: google},
		Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Changed: func() {},
		OpenURL: func(string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewConnectionService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, conn, nil)

	if _, err := svc.SignIn(ownGoogle); err != nil {
		t.Fatal(err)
	}

	if google.clientID != ownGoogle.GoogleClientID || yandex.clientID != "" {
		t.Errorf("client IDs: google %q, yandex %q; want the own Google client only", google.clientID, yandex.clientID)
	}
}

// TestSignOutOfDraftKind: SignOut forgets the token of the kind it names
// only; Save deletes it.
func TestSignOutOfDraftKind(t *testing.T) {
	svc, store, _ := newTestConnectionService(t, domain.ConnectionSettings{})
	store.token = usecase.CloudToken{Provider: domain.KindYandexDisk, ClientID: "c", Account: "ivan"}
	store.hasToken = true
	store.google = &usecase.CloudToken{Provider: domain.KindGoogleDrive, ClientID: ownGoogle.GoogleClientID}

	svc.SignOut(string(domain.KindGoogleDrive))

	if got, _ := svc.Account(ownGoogle); got.SignedIn {
		t.Error("Google is still signed in after its Sign out")
	}

	if got, _ := svc.Account(SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "c"}); !got.SignedIn {
		t.Error("the Sign out of Google signed Yandex out")
	}
}

// TestReauthMessageNamesErrorProvider: with Yandex Disk saved, Test of a
// Google draft whose token Google rejects asks to sign in to Google Drive.
func TestReauthMessageNamesErrorProvider(t *testing.T) {
	saved := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "c", SyncPhrase: "sixteen chars ok"}
	store := &memSettings{s: saved, token: usecase.CloudToken{Provider: domain.KindYandexDisk, ClientID: "c"}, hasToken: true}
	store.google = &usecase.CloudToken{Provider: domain.KindGoogleDrive, ClientID: ownGoogle.GoogleClientID, Refresh: "r"}

	conn, err := usecase.NewConnection(usecase.ConnectionDeps{
		Settings: store, Store: store, Tokens: store, Remote: rejectingCloud{},
		Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Changed: func() {},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewConnectionService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, conn, nil)

	_, err = svc.Test(ownGoogle)

	ui, ok := errors.AsType[*UIError](err)
	if !ok || ui.Code != codeReauthRequired || ui.Message != "Sign in to Google Drive again." {
		t.Errorf("Test = %v, want reauth_required \"Sign in to Google Drive again.\"", err)
	}
}

// rejectingCloud is a cloud whose provider rejects the token, as the store
// of the target's kind reports it.
type rejectingCloud struct{}

func (rejectingCloud) Open(_ context.Context, t usecase.RemoteTarget, _ usecase.OpenMode) (usecase.RemoteSession, error) {
	return nil, domain.ReauthRequired(t.Settings.Kind())
}
