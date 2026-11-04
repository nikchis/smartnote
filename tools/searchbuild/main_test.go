package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkerArtifactFindsTheExecutable(t *testing.T) {
	messages := strings.Join([]string{
		`{"reason":"compiler-artifact","target":{"name":"serde","kind":["lib"]},"executable":null}`,
		`{"reason":"build-script-executed","package_id":"worker"}`,
		`{"reason":"compiler-artifact","target":{"name":"smartnote-search","kind":["bin"]},"executable":"/t/release/smartnote-search"}`,
		`{"reason":"build-finished","success":true}`,
	}, "\n")

	got, err := workerArtifact(strings.NewReader(messages))
	if err != nil {
		t.Fatal(err)
	}

	if got != "/t/release/smartnote-search" {
		t.Errorf("workerArtifact() = %q, want /t/release/smartnote-search", got)
	}
}

func TestWorkerArtifactFailsWithoutTheExecutable(t *testing.T) {
	messages := `{"reason":"compiler-artifact","target":{"name":"smartnote-search","kind":["bin"]},"executable":null}
{"reason":"build-finished","success":false}`

	_, err := workerArtifact(strings.NewReader(messages))
	if err == nil || !strings.Contains(err.Error(), "smartnote-search") {
		t.Fatalf("workerArtifact() error = %v, want a missing executable", err)
	}
}

func TestWorkerArtifactRejectsMalformedOutput(t *testing.T) {
	_, err := workerArtifact(strings.NewReader("not json\n"))
	if err == nil {
		t.Fatal("workerArtifact() error = nil, want a parse error")
	}
}

func TestCopyExecutableReplacesTheOldCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), "smartnote-search")
	if err := os.WriteFile(src, []byte("new worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(out, "smartnote-search"), []byte("stale worker"), 0o600); err != nil {
		t.Fatal(err)
	}

	dst, err := copyExecutable(src, out)
	if err != nil {
		t.Fatal(err)
	}

	if dst != filepath.Join(out, "smartnote-search") {
		t.Errorf("copyExecutable() = %q", dst)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "new worker" {
		t.Errorf("copy = %q, want %q", got, "new worker")
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("files in %s = %d, want only the worker", out, len(entries))
	}
}

func TestCopyExecutableCreatesTheDirectory(t *testing.T) {
	src := filepath.Join(t.TempDir(), "smartnote-search.exe")
	if err := os.WriteFile(src, []byte("worker"), 0o700); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "a", "bin")

	dst, err := copyExecutable(src, out)
	if err != nil {
		t.Fatal(err)
	}

	if dst != filepath.Join(out, "smartnote-search.exe") {
		t.Errorf("copyExecutable() = %q", dst)
	}

	// bin is mounted into the AppImage smoke container, where another user reads it.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm()&0o005 != 0o005 {
		t.Errorf("directory mode = %v, want it readable and searchable by others", info.Mode().Perm())
	}
}
