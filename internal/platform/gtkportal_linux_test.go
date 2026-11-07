package platform

import (
	"os"
	"testing"
)

func TestWithNoPortals(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "no-portals"},
		{"misc", "misc,no-portals"},
		{"misc,no-portals", "misc,no-portals"},
		{"no-portals", "no-portals"},
	}

	for _, tc := range tests {
		if got := withNoPortals(tc.in); got != tc.want {
			t.Errorf("withNoPortals(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKeepGTKOffPortalsRestoresTheVariable(t *testing.T) {
	t.Setenv("GDK_DEBUG", "misc")

	restore := KeepGTKOffPortals()

	if got := os.Getenv("GDK_DEBUG"); got != "misc,no-portals" {
		t.Fatalf("GDK_DEBUG = %q, want misc,no-portals", got)
	}

	restore()
	restore() // a second call changes nothing

	if got := os.Getenv("GDK_DEBUG"); got != "misc" {
		t.Errorf("GDK_DEBUG after restore = %q, want misc", got)
	}
}

func TestKeepGTKOffPortalsUnsetsAnAbsentVariable(t *testing.T) {
	t.Setenv("GDK_DEBUG", "") // restores the variable after the test

	if err := os.Unsetenv("GDK_DEBUG"); err != nil {
		t.Fatal(err)
	}

	restore := KeepGTKOffPortals()

	if got := os.Getenv("GDK_DEBUG"); got != "no-portals" {
		t.Fatalf("GDK_DEBUG = %q, want no-portals", got)
	}

	restore()

	if v, ok := os.LookupEnv("GDK_DEBUG"); ok {
		t.Errorf("GDK_DEBUG after restore = %q, want unset", v)
	}
}
