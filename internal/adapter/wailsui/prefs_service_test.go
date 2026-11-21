package wailsui

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/prefs"
	"github.com/nikchis/smartnote/internal/i18n"
)

func newTestPrefsService(t *testing.T, dir string) (*PrefsService, *i18n.Current) {
	t.Helper()

	lang := i18n.NewCurrent(i18n.EN)

	return NewPrefsService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler), Lang: lang}, dir), lang
}

func TestPrefsServiceSystemDefaultNotSaved(t *testing.T) {
	tests := []struct{ system, want string }{
		{"ru-RU", "ru"}, {"ru", "ru"}, {"RU-ru", "ru"}, {"en-US", "en"}, {"de-DE", "en"}, {"", "en"},
	}

	for _, tc := range tests {
		t.Run(tc.system, func(t *testing.T) {
			dir := t.TempDir()
			svc, lang := newTestPrefsService(t, dir)

			if got := svc.Language(tc.system); got != tc.want {
				t.Errorf("Language(%q) = %q, want %q", tc.system, got, tc.want)
			}

			if lang.Get() != i18n.Lang(tc.want) {
				t.Errorf("current = %q, want %q", lang.Get(), tc.want)
			}

			if _, found, _ := prefs.Load(dir); found {
				t.Error("the system language was saved")
			}
		})
	}
}

func TestSetLanguagePersistsAndSwitches(t *testing.T) {
	dir := t.TempDir()
	svc, lang := newTestPrefsService(t, dir)

	if got := svc.Language("en-US"); got != "en" {
		t.Fatalf("Language = %q, want en", got)
	}

	if err := svc.SetLanguage("ru"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	if lang.Get() != i18n.RU {
		t.Errorf("current = %q, want ru", lang.Get())
	}

	if p, found, err := prefs.Load(dir); err != nil || !found || p.Language != "ru" {
		t.Errorf("prefs = %+v %v %v, want ru saved", p, found, err)
	}

	// The next start takes the saved language over the system one.
	next, nextLang := newTestPrefsService(t, dir)
	if got := next.Language("en-US"); got != "ru" || nextLang.Get() != i18n.RU {
		t.Errorf("Language after restart = %q (current %q), want ru", got, nextLang.Get())
	}
}

func TestSetLanguageRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	svc, lang := newTestPrefsService(t, dir)

	for _, l := range []string{"de", "", "RU", "ru-RU"} {
		err := svc.SetLanguage(l)
		if code := codeOf(err); code != codeValidation {
			t.Errorf("SetLanguage(%q) = %v, want validation", l, err)
		}
	}

	if lang.Get() != i18n.EN {
		t.Errorf("current = %q, want en kept", lang.Get())
	}

	if _, found, _ := prefs.Load(dir); found {
		t.Error("an unknown language was saved")
	}
}

func TestLanguageIgnoresUnusableSavedValue(t *testing.T) {
	dir := t.TempDir()
	if err := prefs.Save(dir, prefs.Prefs{Language: "de"}); err != nil {
		t.Fatal(err)
	}

	svc, _ := newTestPrefsService(t, dir)
	if got := svc.Language("ru-RU"); got != "ru" {
		t.Errorf("Language = %q, want the system ru", got)
	}

	// A file that cannot be read is logged and the system language is used.
	broken := t.TempDir()
	if err := os.Mkdir(filepath.Join(broken, "prefs.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	lang := i18n.NewCurrent(i18n.EN)
	svc = NewPrefsService(Env{Ctx: t.Context(), Log: slog.New(slog.NewTextHandler(&buf, nil)), Lang: lang}, broken)

	if got := svc.Language("ru"); got != "ru" {
		t.Errorf("Language = %q, want the system ru", got)
	}

	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Errorf("log = %q, want the read error", buf.String())
	}
}

func TestSetLanguageSaveFailureKeepsLanguage(t *testing.T) {
	svc, lang := newTestPrefsService(t, filepath.Join(t.TempDir(), "missing"))

	if err := svc.SetLanguage("ru"); codeOf(err) != codeInternal {
		t.Errorf("SetLanguage = %v, want internal", err)
	}

	if lang.Get() != i18n.EN {
		t.Errorf("current = %q, want en kept", lang.Get())
	}
}

// Concurrent switches leave the current language equal to the saved one.
func TestSetLanguageConcurrentKeepsFileAndCurrentInStep(t *testing.T) {
	dir := t.TempDir()
	svc, lang := newTestPrefsService(t, dir)

	var wg sync.WaitGroup

	for i := range 16 {
		wg.Go(func() {
			l := "en"
			if i%2 == 0 {
				l = "ru"
			}

			if err := svc.SetLanguage(l); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	p, found, err := prefs.Load(dir)
	if err != nil || !found || i18n.Lang(p.Language) != lang.Get() {
		t.Errorf("saved %+v %v %v, current %q: want the same language", p, found, err, lang.Get())
	}
}
