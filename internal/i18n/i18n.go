// Package i18n is the catalog of the texts that Go forms for the user, and
// the current language (spec §16.4). It is a leaf: it imports nothing of the
// project, and only the interface, the sign-in adapter and the composition
// root import it.
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Lang is a language of the interface.
type Lang string

// The languages of the interface.
const (
	EN Lang = "en"
	RU Lang = "ru"
)

// Langs returns the languages of the interface, English first.
func Langs() []Lang { return []Lang{EN, RU} }

// Parse returns the language named s, exactly "en" or "ru".
func Parse(s string) (Lang, bool) {
	switch l := Lang(s); l {
	case EN, RU:
		return l, true
	default:
		return "", false
	}
}

// Key names a text of a catalog.
type Key string

// catalogs holds the text of each key per language. A language without a
// catalog, or a key without a translation, falls back to English.
var catalogs = map[Lang]map[Key]string{EN: en, RU: ru}

// T returns the text of k in l. Each pair of args (a name and a value)
// replaces {name} in the text. A language without a catalog or a key without
// a translation gives the English text; a key that is not in English either
// gives itself.
func T(l Lang, k Key, args ...any) string {
	text, ok := catalogs[l][k]
	if !ok {
		text, ok = en[k]
	}

	if !ok {
		return string(k)
	}

	if len(args) < 2 {
		return text
	}

	pairs := make([]string, 0, len(args))

	for i := 0; i+1 < len(args); i += 2 {
		pairs = append(pairs, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
	}

	return strings.NewReplacer(pairs...).Replace(text)
}

// Current is the language the app shows now. It is safe for concurrent use.
type Current struct {
	lang atomic.Value // Lang
}

// NewCurrent returns a Current set to l.
func NewCurrent(l Lang) *Current {
	c := &Current{}
	c.lang.Store(l)

	return c
}

// Get returns the current language.
func (c *Current) Get() Lang {
	l, _ := c.lang.Load().(Lang)
	return l
}

// Set makes l the current language.
func (c *Current) Set(l Lang) { c.lang.Store(l) }
