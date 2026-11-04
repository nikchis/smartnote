package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var testFiles = map[string]string{
	"model.onnx":    "onnx bytes",
	"tokenizer.kit": "kit bytes",
}

func sum(content string) string {
	digest := sha256.Sum256([]byte(content))

	return hex.EncodeToString(digest[:])
}

// writeLock writes a lock file for testFiles with the given base URL and returns its path.
func writeLock(t *testing.T, baseURL string) string {
	t.Helper()

	files := map[string]string{}
	for name, content := range testFiles {
		files[name] = sum(content)
	}

	data, err := json.Marshal(lockFile{Release: "test", BaseURL: baseURL, Files: files})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "model.lock.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// serve serves the given files and counts the requests.
func serve(t *testing.T, files map[string]string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		content, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

func assertModelFiles(t *testing.T, dir string) {
	t.Helper()

	for name, content := range testFiles {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != content {
			t.Errorf("%s = %q, want %q", name, got, content)
		}
	}
}

func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files in %s = %v, want %v", dir, got, want)
	}
}

func TestRunDownloadsVerifiedFiles(t *testing.T) {
	server, _ := serve(t, testFiles)
	out := filepath.Join(t.TempDir(), "model")

	if err := run(context.Background(), writeLock(t, server.URL+"/"), out, ""); err != nil {
		t.Fatal(err)
	}

	assertModelFiles(t, out)
	assertOnlyFiles(t, out, "model.onnx", "tokenizer.kit")
}

func TestRunRejectsCorruptedDownload(t *testing.T) {
	server, _ := serve(t, map[string]string{"model.onnx": "onnx bytes", "tokenizer.kit": "tampered"})
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, server.URL+"/"), out, "")
	if err == nil || !strings.Contains(err.Error(), "tokenizer.kit") || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("run() error = %v, want a SHA-256 mismatch of tokenizer.kit", err)
	}

	if _, statErr := os.Stat(filepath.Join(out, "tokenizer.kit")); !os.IsNotExist(statErr) {
		t.Errorf("corrupted tokenizer.kit left in place (stat error %v)", statErr)
	}

	for _, name := range []string{"tokenizer.kit.part", "model.onnx.part"} {
		if _, statErr := os.Stat(filepath.Join(out, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s left in place", name)
		}
	}
}

func TestRunKeepsValidFilesAndReplacesCorruptedOnes(t *testing.T) {
	server, requests := serve(t, testFiles)
	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, "model.onnx"), []byte("onnx bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(out, "tokenizer.kit"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run(context.Background(), writeLock(t, server.URL+"/"), out, ""); err != nil {
		t.Fatal(err)
	}

	assertModelFiles(t, out)

	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (only the corrupted file)", got)
	}
}

func TestRunUsesBaseURLOverrideWithFileScheme(t *testing.T) {
	source := t.TempDir()
	for name, content := range testFiles {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// file:///tmp/x/ on Unix, file:///C:/x/ on Windows.
	override := "file:///" + strings.TrimPrefix(filepath.ToSlash(source), "/") + "/"
	out := t.TempDir()

	if err := run(context.Background(), writeLock(t, "http://127.0.0.1:1/unused/"), out, override); err != nil {
		t.Fatal(err)
	}

	assertModelFiles(t, out)
}

func TestRunRejectsFileNamesWithPaths(t *testing.T) {
	data := `{"release":"test","base_url":"http://127.0.0.1:1/","files":{"../escape":"00"}}`

	lock := filepath.Join(t.TempDir(), "model.lock.json")
	if err := os.WriteFile(lock, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run(context.Background(), lock, t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "../escape") {
		t.Fatalf("run() error = %v, want a rejected file name", err)
	}
}
