package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/gdrive"
	"github.com/nikchis/smartnote/internal/adapter/sqlite"
	"github.com/nikchis/smartnote/internal/adapter/wailsui"
	"github.com/nikchis/smartnote/internal/adapter/yadisk"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
	"github.com/nikchis/smartnote/internal/usecase"
)

func TestLogStartError(t *testing.T) {
	var buf bytes.Buffer

	logStartError(slog.New(slog.NewTextHandler(&buf, nil)), fmt.Errorf("create data directory: %w", syscall.ENOTDIR))

	if got := buf.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "not a directory") {
		t.Errorf("log = %q, want the start error", got)
	}
}

func TestLogStartErrorNil(t *testing.T) {
	var buf bytes.Buffer

	logStartError(slog.New(slog.NewTextHandler(&buf, nil)), nil)

	if buf.Len() != 0 {
		t.Errorf("log = %q, want nothing after a clean exit", buf.String())
	}
}

func TestWithoutURLHidesTheURL(t *testing.T) {
	const url = "https://oauth.yandex.ru/authorize?state=SECRET"

	err := withoutURL(func(u string) error { return fmt.Errorf("exec xdg-open %s: failed", u) })(url)
	if !errors.Is(err, errOpenBrowser) || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v, want the fixed error without the URL", err)
	}

	if err := withoutURL(func(string) error { return nil })(url); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestNewServicesWires(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.DiscardHandler)
	engine, status := newSearchEngine(logger, findSearch)

	services, err := newServices(wailsui.Env{Ctx: t.Context(), Log: logger, Lang: i18n.NewCurrent(i18n.EN)}, t.TempDir(), store, wailsui.NewLifecycle(func() {}, func() {}), searchParts{
		engine: engine, status: status, run: newSearchRunner(t.Context(), logger),
	})
	if err != nil || len(services) == 0 {
		t.Fatalf("newServices = %d services, %v", len(services), err)
	}

	t.Cleanup(func() { _ = engine.Close() })
}

// PrefsService sets the language the other services read.
func TestNewServicesSharesLanguage(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.DiscardHandler)
	engine, status := newSearchEngine(logger, findSearch)

	t.Cleanup(func() { _ = engine.Close() })

	lang := i18n.NewCurrent(i18n.EN)

	services, err := newServices(wailsui.Env{Ctx: t.Context(), Log: logger, Lang: lang}, t.TempDir(), store, wailsui.NewLifecycle(func() {}, func() {}), searchParts{
		engine: engine, status: status, run: newSearchRunner(t.Context(), logger),
	})
	if err != nil {
		t.Fatal(err)
	}

	var prefs *wailsui.PrefsService

	for _, s := range services {
		if p, ok := s.Instance().(*wailsui.PrefsService); ok {
			prefs = p
		}
	}

	if prefs == nil {
		t.Fatal("PrefsService is not registered")
	}

	if err := prefs.SetLanguage("ru"); err != nil {
		t.Fatal(err)
	}

	if lang.Get() != i18n.RU {
		t.Errorf("shared language = %q, want ru", lang.Get())
	}
}

// setBuiltinGoogle sets the built-in Google client as -ldflags -X does for
// the test and restores the values of the build after it.
func setBuiltinGoogle(t *testing.T, id, secret string) {
	t.Helper()

	oldID, oldSecret := gdrive.BuiltinClientID, gdrive.BuiltinClientSecret
	gdrive.BuiltinClientID, gdrive.BuiltinClientSecret = id, secret

	t.Cleanup(func() { gdrive.BuiltinClientID, gdrive.BuiltinClientSecret = oldID, oldSecret })
}

// TestMainWithoutBuiltinGoogle: a build without the built-in Google client
// wires no Google client, and Google Drive still asks to sign in; Yandex
// keeps its client.
func TestMainWithoutBuiltinGoogle(t *testing.T) {
	setBuiltinGoogle(t, "", "")

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	env := wailsui.Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler), Lang: i18n.NewCurrent(i18n.EN)}

	r, err := newRemote(env, store, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := r.builtin[domain.KindGoogleDrive]; ok {
		t.Error("a build without the Google client has a built-in Google client")
	}

	if r.builtin[domain.KindYandexDisk].ID != yadisk.BuiltinClientID {
		t.Error("built-in Yandex client is missing")
	}

	_, err = r.connector.Open(t.Context(), usecase.RemoteTarget{Settings: domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive}}, usecase.OpenCheck)
	if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
		t.Errorf("Open(google_drive) without a client = %v, want a sign-in to google_drive", err)
	}
}

// TestMainWiresThreeConnectors: each storage kind reaches its own
// connector, and both clouds have their authorizer and built-in client.
// Without a token each cloud asks to sign in to itself; the SSH connector
// never does.
func TestMainWiresThreeConnectors(t *testing.T) {
	setBuiltinGoogle(t, "1-test.apps.googleusercontent.com", "builtin-test-secret")

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	env := wailsui.Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler), Lang: i18n.NewCurrent(i18n.EN)}

	r, err := newRemote(env, store, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, kind := range []domain.StorageKind{domain.KindYandexDisk, domain.KindGoogleDrive} {
		_, err := r.connector.Open(t.Context(), usecase.RemoteTarget{Settings: domain.ConnectionSettings{StorageKind: kind}}, usecase.OpenCheck)
		if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != kind {
			t.Errorf("Open(%s) without a token = %v, want a sign-in to %s", kind, err, kind)
		}

		if r.auth[kind] == nil {
			t.Errorf("no authorizer of %s", kind)
		}

		if c := r.builtin[kind]; c.Provider != kind || c.ID == "" {
			t.Errorf("built-in client of %s = provider %q, ID %q", kind, c.Provider, c.ID)
		}
	}

	_, err = r.connector.Open(t.Context(), usecase.RemoteTarget{Settings: domain.ConnectionSettings{StorageKind: domain.KindSSHPostgres}}, usecase.OpenCheck)
	if err == nil || errors.Is(err, domain.ErrReauthRequired) || errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("Open(ssh_postgres) = %v, want an error of the SSH connector", err)
	}

	if r.builtin[domain.KindYandexDisk].ID != yadisk.BuiltinClientID || r.builtin[domain.KindYandexDisk].Secret != "" {
		t.Error("built-in Yandex client is not the client of yadisk")
	}

	// Compared, never printed.
	if g := r.builtin[domain.KindGoogleDrive]; g.ID != gdrive.BuiltinClientID || g.Secret != gdrive.BuiltinClientSecret {
		t.Error("built-in Google client is not the client of gdrive")
	}
}

// TestBuiltinSecretNeverInDTO: the built-in Google client secret reaches
// neither the settings nor the account the form gets (spec §1.2, criterion
// 3), even for Google Drive settings with the built-in client.
func TestBuiltinSecretNeverInDTO(t *testing.T) {
	setBuiltinGoogle(t, "1-test.apps.googleusercontent.com", "builtin-test-secret")

	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	logger := slog.New(slog.DiscardHandler)
	engine, status := newSearchEngine(logger, findSearch)

	t.Cleanup(func() { _ = engine.Close() })

	services, err := newServices(wailsui.Env{Ctx: t.Context(), Log: logger, Lang: i18n.NewCurrent(i18n.EN)}, t.TempDir(), store, wailsui.NewLifecycle(func() {}, func() {}), searchParts{
		engine: engine, status: status, run: newSearchRunner(t.Context(), logger),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Create(t.Context(), "a long local password"); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveSettings(t.Context(), domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive, SyncPhrase: "sixteen chars ok"}); err != nil {
		t.Fatal(err)
	}

	var conn *wailsui.ConnectionService

	for _, s := range services {
		if c, ok := s.Instance().(*wailsui.ConnectionService); ok {
			conn = c
		}
	}

	if conn == nil {
		t.Fatal("ConnectionService is not registered")
	}

	dto, err := conn.Load()
	if err != nil || dto.Kind != string(domain.KindGoogleDrive) {
		t.Fatalf("Load = kind %q, %v; want the saved Google Drive settings", dto.Kind, err)
	}

	acc, err := conn.Account(dto)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range []any{dto, acc} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}

		// Checked, never printed.
		if strings.Contains(string(b), gdrive.BuiltinClientSecret) {
			t.Errorf("%T carries the built-in client secret", v)
		}
	}
}

// TestBuiltinClients: the Google client is built in only when the build set
// both its ID and secret (spec §1, ред. 8); Yandex always has its client.
func TestBuiltinClients(t *testing.T) {
	tests := []struct {
		name, id, secret string
		google           bool
	}{
		{name: "both set", id: "1-x.apps.googleusercontent.com", secret: "s", google: true},
		{name: "none", id: "", secret: ""},
		{name: "no secret", id: "1-x.apps.googleusercontent.com", secret: ""},
		{name: "no ID", id: "", secret: "s"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := builtinClients(tc.id, tc.secret)

			if y := got[domain.KindYandexDisk]; y.Provider != domain.KindYandexDisk || y.ID != yadisk.BuiltinClientID || y.Secret != "" {
				t.Error("built-in Yandex client is not the client of yadisk")
			}

			g, ok := got[domain.KindGoogleDrive]
			if ok != tc.google {
				t.Fatalf("Google built in = %v, want %v", ok, tc.google)
			}

			// Compared, never printed.
			if ok && (g.Provider != domain.KindGoogleDrive || g.ID != tc.id || g.Secret != tc.secret) {
				t.Error("built-in Google client is not the client of the build")
			}
		})
	}
}
