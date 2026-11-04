package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestRunUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"-worker", "w"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
}

func TestRunMissingWorker(t *testing.T) {
	var stdout, stderr bytes.Buffer

	worker := filepath.Join(t.TempDir(), "smartnote-search")

	code := run([]string{"-worker", worker, "-ort", "o", "-model", "m", "-timeout", "20s"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "searchsmoke: no worker became ready") {
		t.Fatalf("exit code %d, stderr:\n%s", code, stderr.String())
	}
}

// TestRunFakeWorker runs the check against the fake worker of package
// searchworker: semantic search loads, the note is found, and the sandbox is
// "none", which fails the check on Linux only.
func TestRunFakeWorker(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "fakeworker")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}

	build := exec.CommandContext(t.Context(), "go", "build", "-o", fake, "../../internal/adapter/searchworker/testdata/fakeworker")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the fake worker: %v\n%s", err, out)
	}

	t.Setenv("FAKE_MODE", "ok")

	var stdout, stderr bytes.Buffer

	code := run([]string{"-worker", fake, "-ort", "/opt/ort.so", "-model", "/opt/model", "-timeout", "20s"}, &stdout, &stderr)

	want := regexp.MustCompile(`^ready worker=fake [^\n]* lexical=ready semantic=ready sandbox=none\nhits=1\n$`)
	if !want.MatchString(stdout.String()) {
		t.Fatalf("stdout:\n%s", stdout.String())
	}

	wantCode := 0
	if runtime.GOOS == "linux" {
		wantCode = 1
	}

	if code != wantCode {
		t.Fatalf("exit code %d, want %d; stderr:\n%s", code, wantCode, stderr.String())
	}

	if wantCode == 1 && !strings.Contains(stderr.String(), "sandbox is none") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
}
