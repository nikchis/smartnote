package i18n

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestTSubstitutes(t *testing.T) {
	got := T(EN, AuthSignedInTitle, "provider", "Google Drive")
	if want := "Signed in to Google Drive"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}

	if got := T(EN, AuthSignedInTitle); got != "Signed in to {provider}" {
		t.Errorf("T without args = %q, want the placeholder kept", got)
	}

	if got := T(EN, AuthSignedInTitle, "provider"); got != "Signed in to {provider}" {
		t.Errorf("T with an odd arg = %q, want the placeholder kept", got)
	}
}

func TestUnknownLangIsEnglish(t *testing.T) {
	want := T(EN, AuthExpiredTitle)
	if got := T(Lang("de"), AuthExpiredTitle); got != want {
		t.Errorf("T(de) = %q, want the English %q", got, want)
	}

	if got := T(EN, Key("no.such.key")); got != "no.such.key" {
		t.Errorf("T of an unknown key = %q, want the key", got)
	}
}

func TestCurrentSetGet(t *testing.T) {
	c := NewCurrent(EN)
	if c.Get() != EN {
		t.Fatalf("Get = %q, want en", c.Get())
	}

	c.Set(RU)

	if c.Get() != RU {
		t.Fatalf("Get after Set = %q, want ru", c.Get())
	}

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			if i%2 == 0 {
				c.Set(EN)
			} else {
				c.Set(RU)
			}
		}()

		go func() {
			defer wg.Done()

			if l := c.Get(); l != EN && l != RU {
				t.Errorf("Get = %q, want en or ru", l)
			}
		}()
	}

	wg.Wait()
}

func TestCatalogsComplete(t *testing.T) {
	for _, l := range []Lang{EN, RU} {
		cat := catalogs[l]
		if len(cat) == 0 {
			t.Fatalf("no catalog for %q", l)
		}

		for k := range en {
			if cat[k] == "" {
				t.Errorf("%s: key %q has no text", l, k)
			}
		}

		for k := range cat {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: key %q is not in the English catalog", l, k)
			}
		}
	}
}

func TestCatalogSourcesNoDashes(t *testing.T) {
	for _, name := range []string{"en.go", "ru.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		for i, line := range strings.Split(string(b), "\n") {
			if strings.ContainsAny(line, "\u2013\u2014") {
				t.Errorf("%s:%d has an en or em dash: %s", name, i+1, line)
			}
		}
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{"en": EN, "ru": RU} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %q %v, want %q", in, got, ok, want)
		}
	}

	for _, in := range []string{"", "de", "RU", "ru-RU"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) accepted", in)
		}
	}
}

func TestLangsHaveCatalogs(t *testing.T) {
	langs := Langs()
	if len(langs) != len(catalogs) || langs[0] != EN {
		t.Fatalf("Langs = %q, want English first and one per catalog", langs)
	}

	for _, l := range langs {
		if _, ok := catalogs[l]; !ok {
			t.Errorf("no catalog for %q", l)
		}
	}
}

func TestRussianPageText(t *testing.T) {
	if got := T(RU, AuthSignedInTitle, "provider", "Google Drive"); got != "Вход в Google Drive выполнен" {
		t.Errorf("T(ru) = %q", got)
	}
}
