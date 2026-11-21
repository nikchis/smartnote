package wailsui

import (
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/prefs"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
)

// PrefsService keeps the language of the interface (spec §16.4). Go is the
// source of truth about it: the interface asks for it at start and reports
// each switch, and Go titles the file dialogs and the sign-in pages in it.
type PrefsService struct {
	env Env
	dir string
}

// NewPrefsService returns a PrefsService that keeps prefs.json in dir and
// sets env.Lang, which must not be nil.
func NewPrefsService(env Env, dir string) *PrefsService {
	return &PrefsService{env: env, dir: dir}
}

// errUnknownLanguage refuses a language without a catalog.
var errUnknownLanguage = &domain.ValidationError{Field: "language", Reason: "must be en or ru"}

// Language returns the saved language, or, while none is saved, the one of
// system (the language of the browser engine: "ru" and "ru-…" are Russian,
// anything else English), and makes it current. The language from system is
// not saved: it follows the system until the user switches it.
func (s *PrefsService) Language(system string) string {
	l := s.saved()
	if l == "" {
		l = fromSystem(system)
	}

	s.env.Lang.Set(l)

	return string(l)
}

// SetLanguage saves lang ("en" or "ru") and makes it current. When the save
// fails the current language stays as it was.
func (s *PrefsService) SetLanguage(lang string) error {
	l, ok := i18n.Parse(lang)
	if !ok {
		return s.env.fail("set language", errUnknownLanguage)
	}

	if err := prefs.Save(s.dir, prefs.Prefs{Language: string(l)}); err != nil {
		return s.env.fail("set language", err)
	}

	s.env.Lang.Set(l)

	return nil
}

// saved returns the saved language; "" when none is saved, the file cannot
// be read (logged), or it names no known language.
func (s *PrefsService) saved() i18n.Lang {
	p, found, err := prefs.Load(s.dir)
	if err != nil {
		LogError(s.env.Log, "load preferences", err)
		return ""
	}

	if !found {
		return ""
	}

	l, _ := i18n.Parse(p.Language)

	return l
}

// fromSystem is the language of the system language tag tag.
func fromSystem(tag string) i18n.Lang {
	if strings.HasPrefix(strings.ToLower(tag), string(i18n.RU)) {
		return i18n.RU
	}

	return i18n.EN
}
