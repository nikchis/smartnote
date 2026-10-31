package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDataDirXDG(t *testing.T) {
	testDirXDG(t, DataDir, "XDG_DATA_HOME", filepath.Join(".local", "share"))
}

func TestStateDirXDG(t *testing.T) {
	testDirXDG(t, StateDir, "XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// testDirXDG checks dir on Linux: the XDG variable when it is absolute,
// otherwise fallback under the home directory.
func testDirXDG(t *testing.T, dir func() (string, error), env, fallback string) {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("XDG directories are used on Linux only")
	}

	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "absolute", value: xdg, want: filepath.Join(xdg, "smartnote")},
		{name: "unset", value: "", want: filepath.Join(home, fallback, "smartnote")},
		// The XDG Base Directory specification says to ignore a relative path.
		{name: "relative", value: "relative/dir", want: filepath.Join(home, fallback, "smartnote")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(env, tc.value)

			got, err := dir()
			if err != nil {
				t.Fatalf("dir: %v", err)
			}

			if got != tc.want {
				t.Errorf("dir = %q, want %q", got, tc.want)
			}
		})
	}
}
