package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// touch creates an empty file, with its directories.
func touch(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindSearchLayouts(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		exe    string // executable relative to the root
		worker string // expected paths relative to the root
		ort    string
		model  string
	}{
		{
			name: "deb", goos: "linux", exe: "usr/local/bin/smartnote",
			worker: "usr/local/lib/smartnote/smartnote-search",
			ort:    "usr/local/lib/smartnote/libonnxruntime.so.1.31.0",
			model:  "usr/local/share/smartnote/model",
		},
		{
			name: "appimage", goos: "linux", exe: "app/usr/bin/smartnote",
			worker: "app/usr/lib/smartnote/smartnote-search",
			ort:    "app/usr/lib/smartnote/libonnxruntime.so.1.31.0",
			model:  "app/usr/share/smartnote/model",
		},
		{
			name: "macos", goos: "darwin", exe: "Smartnote.app/Contents/MacOS/smartnote",
			worker: "Smartnote.app/Contents/MacOS/smartnote-search",
			ort:    "Smartnote.app/Contents/MacOS/libonnxruntime.1.31.0.dylib",
			model:  "Smartnote.app/Contents/Resources/model",
		},
		{
			name: "windows", goos: "windows", exe: "Smartnote/smartnote.exe",
			worker: "Smartnote/smartnote-search.exe",
			ort:    "Smartnote/onnxruntime.dll",
			model:  "Smartnote/model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			touch(t, filepath.Join(root, tt.worker))

			got, err := findSearch(tt.goos, filepath.Join(root, tt.exe), "")
			if err != nil {
				t.Fatalf("findSearch: %v", err)
			}

			want := SearchLocation{
				Worker:   filepath.Join(root, tt.worker),
				OrtLib:   filepath.Join(root, tt.ort),
				ModelDir: filepath.Join(root, tt.model),
			}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestFindSearchOverride(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "smartnote-search"))

	got, err := findSearch("linux", filepath.Join(t.TempDir(), "bin", "smartnote"), dir)
	if err != nil {
		t.Fatalf("findSearch: %v", err)
	}

	want := SearchLocation{
		Worker:   filepath.Join(dir, "smartnote-search"),
		OrtLib:   filepath.Join(dir, "libonnxruntime.so.1.31.0"),
		ModelDir: filepath.Join(dir, "model"),
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFindSearchRelativeOverrideIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "smartnote-search"))
	t.Chdir(dir)

	got, err := findSearch("linux", "/x/bin/smartnote", ".")
	if err != nil {
		t.Fatalf("findSearch: %v", err)
	}

	if !filepath.IsAbs(got.Worker) || !filepath.IsAbs(got.OrtLib) || !filepath.IsAbs(got.ModelDir) {
		t.Errorf("paths are not absolute: %+v", got)
	}
}

func TestSearchLocationMissing(t *testing.T) {
	root := t.TempDir()

	_, err := findSearch("linux", filepath.Join(root, "bin", "smartnote"), "")
	if !errors.Is(err, ErrSearchMissing) {
		t.Errorf("err = %v, want ErrSearchMissing", err)
	}

	_, err = findSearch("linux", filepath.Join(root, "bin", "smartnote"), root)
	if !errors.Is(err, ErrSearchMissing) {
		t.Errorf("override: err = %v, want ErrSearchMissing", err)
	}
}

func TestFindSearchResolvesSymlink(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "real", "lib", "smartnote", "smartnote-search"))
	touch(t, filepath.Join(root, "real", "bin", "smartnote"))

	if err := os.MkdirAll(filepath.Join(root, "link"), 0o700); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link", "smartnote")
	if err := os.Symlink(filepath.Join(root, "real", "bin", "smartnote"), link); err != nil {
		t.Fatal(err)
	}

	if searchDirOverride() != "" {
		t.Skip("SMARTNOTE_SEARCH_DIR is set")
	}

	got, err := FindSearch(link)
	if err != nil {
		t.Fatalf("FindSearch: %v", err)
	}

	if want := filepath.Join(root, "real", "lib", "smartnote", "smartnote-search"); got.Worker != want {
		t.Errorf("Worker = %q, want %q", got.Worker, want)
	}
}
