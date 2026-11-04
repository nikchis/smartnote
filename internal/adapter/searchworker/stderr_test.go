package searchworker

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nikchis/smartnote/internal/usecase"
)

// marker stands for note text that a compromised worker writes out.
const marker = "MARKER-7f3a"

func TestStderrFilterKnownLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line, want string
		level      slog.Level
	}{
		{"model loaded in 641 ms", "model loaded in 641 ms", slog.LevelInfo},
		{"sandbox partial: landlock_net threads", "sandbox partial: landlock_net threads", slog.LevelError},
		{"sandbox none: unsupported_os", "sandbox none: unsupported_os", slog.LevelError},
		{"sandbox: 2 descriptors besides 0, 1, 2 are open, 1 writable", "sandbox: 2 descriptors besides 0, 1, 2 are open, 1 writable", slog.LevelError},
		{"sandbox: open descriptors cannot be listed", "sandbox: open descriptors cannot be listed", slog.LevelError},
		{"panic at src/worker.rs:10:5", "panic at src/worker.rs:10:5", slog.LevelError},
		{"panic at <unknown>", "panic at <unknown>", slog.LevelError},
		{"semantic search unavailable: the CPU has no AVX2", "semantic search unavailable: the CPU has no AVX2", slog.LevelError},
		{
			"semantic search unavailable: model files: SHA-256 of model.onnx differs from the pinned digest",
			"semantic search unavailable: model files: SHA-256 of model.onnx differs from the pinned digest",
			slog.LevelError,
		},
		{"semantic search unavailable: onnxruntime: reading a model file failed", "semantic search unavailable: onnxruntime: reading a model file failed", slog.LevelError},
		{"semantic search unavailable: loading the ONNX Runtime library failed", "semantic search unavailable: loading the ONNX Runtime library failed", slog.LevelError},
		{"semantic search unavailable: model: ONNX Runtime failed", "semantic search unavailable: model: ONNX Runtime failed", slog.LevelError},
		{"note embedding failed: tokenizer failed", "note embedding failed: tokenizer failed", slog.LevelError},
		{"query embedding failed: unexpected model inputs or outputs", "query embedding failed: unexpected model inputs or outputs", slog.LevelError},
		{"query vector timed out after 1s", "query vector timed out after 1s", slog.LevelError},
		{"onnxruntime error at inference_session.cc:1234 onnxruntime::InferenceSession::Initialize", "onnxruntime error at inference_session.cc:1234", slog.LevelError},
		{"onnxruntime fatal at env.cc:7", "onnxruntime fatal at env.cc:7", slog.LevelError},
		{"harden failed: Operation not permitted (os error 1)", "harden failed: os error 1", slog.LevelError},
		{"harden failed: parent process has changed", "harden failed: parent process has changed", slog.LevelError},
		{"closing inherited descriptors failed: Bad file descriptor (os error 9)", "closing inherited descriptors failed: os error 9", slog.LevelError},
		{"unsupported protocol version 2, expected 1", "unsupported protocol version 2, expected 1", slog.LevelError},
		{"bad request: malformed frame", "bad request: malformed frame", slog.LevelError},
		{"bad request: unexpected hello", "bad request: unexpected hello", slog.LevelError},
		{"stdin read failed", "stdin read failed", slog.LevelError},
		{"stdout write failed", "stdout write failed", slog.LevelError},
		{"starting the embed thread failed", "starting the embed thread failed", slog.LevelError},
		{"memory allocation of 1048576 bytes failed", "memory allocation of 1048576 bytes failed", slog.LevelError},
	}

	for _, tc := range tests {
		f := newStderrFilter(workerStderrForms)

		text, level, ok := f.filter(tc.line)
		if !ok || text != tc.want || level != tc.level {
			t.Errorf("filter(%q) = %q, %v, %v; want %q, %v", tc.line, text, level, ok, tc.want, tc.level)
		}
	}
}

func TestStderrFilterDropsUnknownText(t *testing.T) {
	t.Parallel()

	lines := []string{
		marker,
		"model loaded in " + marker,
		"panic at " + marker + " src/worker.rs:1:1",
		"panic at src/worker.rs:1:1 " + marker,
		"semantic search unavailable: " + marker,
		"semantic search unavailable: model files: " + marker,
		"note embedding failed: " + marker,
		"sandbox partial: " + marker,
		"bad request: " + marker,
		"starting the " + marker + " thread failed",
		"onnxruntime error at " + marker,
		"",
	}

	for _, line := range lines {
		f := newStderrFilter(workerStderrForms)
		if text, _, ok := f.filter(line); ok {
			t.Errorf("filter(%q) kept %q", line, text)
		}
	}

	// A free text inside a known form is not logged either.
	f := newStderrFilter(workerStderrForms)
	for _, line := range []string{
		"harden failed: " + marker + " (os error 1)",
		"closing inherited descriptors failed: " + marker + " (os error 9)",
		"onnxruntime error at session.cc:12 " + marker,
	} {
		if text, _, ok := f.filter(line); !ok || strings.Contains(text, marker) {
			t.Errorf("filter(%q) = %q, %v; want the form without the free text", line, text, ok)
		}
	}
}

// A panic aborts the worker, so one process writes at most one panic line;
// more are not the panic hook's.
func TestStderrFilterOnePanicPerProcess(t *testing.T) {
	t.Parallel()

	f := newStderrFilter(workerStderrForms)

	if _, _, ok := f.filter("panic at src/a.rs:1:1"); !ok {
		t.Fatal("the first panic line was dropped")
	}

	if text, _, ok := f.filter("panic at src/b.rs:2:2"); ok {
		t.Fatalf("a second panic line was kept: %q", text)
	}
}

// eprintlnFormat finds the format string of every eprintln! call.
var eprintlnFormat = regexp.MustCompile(`eprintln!\(\s*"((?:[^"\\]|\\.)*)"`)

// Every line the worker writes to stderr has a form in workerStderrForms:
// a new eprintln! in the worker fails this test until its form is added.
func TestStderrFormsCoverWorker(t *testing.T) {
	t.Parallel()

	src := filepath.Join("..", "..", "..", "search", "worker", "src")

	var found []string

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// The probes are test programs, not the worker.
		if d.IsDir() && d.Name() == "bin" {
			return filepath.SkipDir
		}

		if d.IsDir() || filepath.Ext(path) != ".rs" {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range eprintlnFormat.FindAllStringSubmatch(string(b), -1) {
			found = append(found, m[1])
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(found) < 20 {
		t.Fatalf("found %d eprintln! formats in %s, want the worker's (20 or more)", len(found), src)
	}

	var covered []string
	for _, f := range workerStderrForms {
		covered = append(covered, f.sources...)
	}

	for _, format := range found {
		if !slices.Contains(covered, format) {
			t.Errorf("eprintln!(%s) of the worker has no form in workerStderrForms", strconv.Quote(format))
		}
	}
}

// Text a compromised worker writes to stderr or into error replies does not
// reach the log; its own lines still do (spec §10.2).
func TestEngineLogsNoUnknownWorkerText(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "leak", nil, "FAKE_MARKER="+marker)
	e.start(t)

	e.log.waitLine(t, "source=search-worker", "sandbox partial: landlock_net")
	e.log.waitLine(t, "search worker stderr: unrecognized line", "len="+strconv.Itoa(len("leaked: "+marker)))

	err := e.Reset(t.Context())
	if err == nil || strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "internal") {
		t.Fatalf("Reset: %v; want the code without the text", err)
	}

	if err := e.Upsert(t.Context(), []usecase.IndexedNote{testNote("x")}); err == nil {
		t.Fatal("Upsert succeeded")
	} else if strings.Contains(err.Error(), marker) {
		t.Fatalf("Upsert: %v", err)
	}

	e.waitEvent(t, "Restarted", isRestarted)
	e.log.waitLine(t, "search worker failed", "gen=1")

	for _, line := range e.log.lines() {
		if strings.Contains(line, marker) {
			t.Fatalf("the log has the marker: %s", line)
		}
	}
}
