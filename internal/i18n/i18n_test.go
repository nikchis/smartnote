package i18n

import (
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
