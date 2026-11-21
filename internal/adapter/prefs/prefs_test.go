package prefs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, Prefs{Language: "ru"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, found, err := Load(dir)
	if err != nil || !found || got.Language != "ru" {
		t.Fatalf("Load = %+v %v %v, want ru found", got, found, err)
	}

	if err := Save(dir, Prefs{Language: "en"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if got, _, _ := Load(dir); got.Language != "en" {
		t.Errorf("Load after the second Save = %+v, want en", got)
	}

	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("the directory holds %d files, want only %s", len(entries), fileName)
	}
}

func TestPrefsMissingFile(t *testing.T) {
	got, found, err := Load(t.TempDir())
	if err != nil || found || got != (Prefs{}) {
		t.Errorf("Load = %+v %v %v, want nothing found and no error", got, found, err)
	}
}

func TestPrefsCorruptIsDefault(t *testing.T) {
	for name, content := range map[string]string{"broken": `{"language":`, "not an object": `[1]`, "empty": ``} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, fileName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			got, found, err := Load(dir)
			if err != nil || found || got != (Prefs{}) {
				t.Errorf("Load = %+v %v %v, want nothing found and no error", got, found, err)
			}
		})
	}
}

func TestPrefsSaveFailsWithoutDirectory(t *testing.T) {
	if err := Save(filepath.Join(t.TempDir(), "missing"), Prefs{Language: "en"}); err == nil {
		t.Error("Save into a missing directory succeeded")
	}
}
