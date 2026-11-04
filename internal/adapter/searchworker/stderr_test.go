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
	"time"

	"github.com/nikchis/smartnote/internal/usecase"
)

// marker stands for note text that a compromised worker writes out.
const marker = "MARKER-7f3a"

func TestStderrRenderKnownLines(t *testing.T) {
	t.Parallel()

	same := func(line string) [2]string { return [2]string{line, line} }

	tests := []struct {
		io    [2]string // the line and the text logged
		level slog.Level
	}{
		{same("model loaded in 641 ms"), slog.LevelInfo},
		{same("sandbox partial: landlock_net threads"), slog.LevelError},
		{same("sandbox none: unsupported_os"), slog.LevelError},
		{same("sandbox: 2 descriptors besides 0, 1, 2 are open, 1 writable"), slog.LevelError},
		{same("sandbox: open descriptors cannot be listed"), slog.LevelError},
		{[2]string{"panic at src/worker.rs:10:5", "panic at worker/src/worker.rs:10:5"}, slog.LevelError},
		{same("panic at worker/src/embed/model.rs:3:9"), slog.LevelError},
		{same("panic at <unknown>"), slog.LevelError},
		{same("semantic search unavailable: the CPU has no AVX2"), slog.LevelError},
		{same("semantic search unavailable: model files: SHA-256 of model.onnx differs from the pinned digest"), slog.LevelError},
		{same("semantic search unavailable: onnxruntime: reading a model file failed"), slog.LevelError},
		{same("semantic search unavailable: loading the ONNX Runtime library failed"), slog.LevelError},
		{same("semantic search unavailable: model: ONNX Runtime failed"), slog.LevelError},
		{same("note embedding failed: tokenizer failed"), slog.LevelError},
		{same("query embedding failed: unexpected model inputs or outputs"), slog.LevelError},
		{same("query vector timed out after 1s"), slog.LevelError},
		{[2]string{"onnxruntime error at inference_session.cc:1234 onnxruntime::InferenceSession::Initialize", "onnxruntime error"}, slog.LevelError},
		{[2]string{"onnxruntime fatal at env.cc:7", "onnxruntime fatal"}, slog.LevelError},
		{[2]string{"harden failed: Operation not permitted (os error 1)", "harden failed: os error 1"}, slog.LevelError},
		{same("harden failed: parent process has changed"), slog.LevelError},
		{[2]string{"closing inherited descriptors failed: Bad file descriptor (os error 9)", "closing inherited descriptors failed: os error 9"}, slog.LevelError},
		{same("unsupported protocol version 2, expected 1"), slog.LevelError},
		{same("bad request: malformed frame"), slog.LevelError},
		{same("bad request: unexpected hello"), slog.LevelError},
		{same("stdin read failed"), slog.LevelError},
		{same("stdout write failed"), slog.LevelError},
		{same("starting the embed thread failed"), slog.LevelError},
		{same("memory allocation of 1048576 bytes failed"), slog.LevelError},
	}

	for _, tc := range tests {
		_, text, level, ok := renderStderr(workerStderrForms, tc.io[0])
		if !ok || text != tc.io[1] || level != tc.level {
			t.Errorf("renderStderr(%q) = %q, %v, %v; want %q, %v", tc.io[0], text, level, ok, tc.io[1], tc.level)
		}
	}
}

// The place of a panic names a file only when it is one of the worker's own
// sources, also as Windows builds write it.
func TestStderrPanicPlace(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("Attacker_chosen_note_text_", 8)

	for line, want := range map[string]string{
		`panic at C:\a\_work\smartnote\search\worker\src\embed\model.rs:12:3`:      "panic at worker/src/embed/model.rs:12:3",
		`panic at worker\src\worker.rs:1:2`:                                        "panic at worker/src/worker.rs:1:2",
		"panic at /home/u/.cargo/registry/src/index/tantivy-0.27.0/src/lib.rs:5:6": "panic at <other>:5:6",
		"panic at " + long + ".rs:1:1":                                             "panic at <other>:1:1",
		"panic at worker/src/" + long + ".rs:1:1":                                  "panic at <other>:1:1",
		"panic at " + marker + "/worker/src/rank.rs:7:8":                           "panic at worker/src/rank.rs:7:8",
	} {
		if _, text, _, ok := renderStderr(workerStderrForms, line); !ok || text != want {
			t.Errorf("renderStderr(%q) = %q, %v; want %q", line, text, ok, want)
		}
	}
}

// No text the worker chooses reaches the log: lines with free text match no
// form, or are logged without it.
func TestStderrRenderDropsFreeText(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		marker,
		"model loaded in " + marker,
		"semantic search unavailable: " + marker,
		"semantic search unavailable: model files: " + marker,
		"note embedding failed: " + marker,
		"sandbox partial: " + marker,
		"bad request: " + marker,
		"starting the " + marker + " thread failed",
		"",
	} {
		if _, text, _, ok := renderStderr(workerStderrForms, line); ok {
			t.Errorf("renderStderr(%q) kept %q", line, text)
		}
	}

	for _, line := range []string{
		"harden failed: " + marker + " (os error 1)",
		"closing inherited descriptors failed: " + marker + " (os error 9)",
		"onnxruntime error at session.cc:12 " + marker,
		"onnxruntime error at " + marker + ":1",
		"panic at " + marker + ".rs:1:1",
	} {
		if _, text, _, ok := renderStderr(workerStderrForms, line); !ok || strings.Contains(text, marker) {
			t.Errorf("renderStderr(%q) = %q, %v; want the form without the free text", line, text, ok)
		}
	}
}

// The budget covers a run of the application: every record about the
// worker's stderr and every error text of the worker uses it, whatever the
// process; after it one line says so and the rest is only counted.
func TestWorkerOutputBudget(t *testing.T) {
	t.Parallel()

	rec := newLogRecorder()
	out := newWorkerOutput(slog.New(rec), workerStderrForms)

	for i := range stderrBudget + 6 {
		out.line(uint64(1+i%3), "sandbox: open descriptors cannot be listed", false)
	}

	if n := rec.countLines("source=search-worker"); n != stderrBudget {
		t.Fatalf("%d stderr records, want %d", n, stderrBudget)
	}

	if n := rec.countLines("further worker output dropped"); n != 1 {
		t.Fatalf("%d notices of dropped output, want 1", n)
	}

	e := &errorMsg{Code: "bad_request", Msg: "unexpected hello"}
	for i := range errorTextBudget {
		if got := out.errorText(e); got != "bad_request: unexpected hello" {
			t.Fatalf("error text %d = %q, want the code with the text", i, got)
		}
	}

	if got := out.errorText(e); got != "bad_request" {
		t.Fatalf("error text after the budget = %q, want the code only", got)
	}

	out.close()

	if n := rec.countLines("worker output dropped", "lines=6", "texts=1"); n != 1 {
		t.Fatalf("no final count of the dropped output; log:\n%s", strings.Join(rec.lines(), "\n"))
	}
}

// One process writes at most one panic line; in a run the log keeps a few.
func TestWorkerOutputPanicsPerRun(t *testing.T) {
	t.Parallel()

	rec := newLogRecorder()
	out := newWorkerOutput(slog.New(rec), workerStderrForms)

	for gen := range uint64(12) {
		out.line(gen+1, "panic at worker/src/worker.rs:1:1", false)
	}

	if n := rec.countLines("panic at"); n != panicsPerRun {
		t.Fatalf("%d panic records, want %d", n, panicsPerRun)
	}
}

// eprintlnFormat finds the format string of every eprintln! call.
var eprintlnFormat = regexp.MustCompile(`eprintln!\(\s*"((?:[^"\\]|\\.)*)"`)

// Every line the worker writes to stderr has a form in workerStderrForms:
// a new eprintln! in the worker fails this test until its form is added. And
// workerSourceFiles, the files a panic place may name, are exactly the
// worker's sources.
func TestStderrFormsCoverWorker(t *testing.T) {
	t.Parallel()

	src := filepath.Join("..", "..", "..", "search", "worker", "src")

	var found, files []string

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

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(rel))

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

	slices.Sort(files)

	if !slices.Equal(files, slices.Sorted(slices.Values(workerSourceFiles))) {
		t.Errorf("workerSourceFiles = %v, want the sources of the worker %v", workerSourceFiles, files)
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

// Restarts do not renew the budget: three processes that each write more
// lines than half of it still get only the budget of one run.
func TestEngineOutputBudgetSpansRestarts(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "chatty-crash", func(c *Config) { c.After = after.After })

	go func() { _ = e.Start(t.Context()) }()

	for range 2 {
		select {
		case call := <-after.calls:
			call.fire <- time.Now()
		case <-time.After(hangGuard):
			t.Fatal("no pause after a failed start")
		}
	}

	e.log.waitLine(t, "search worker failed", "gen=3")

	if n := e.log.countLines("source=search-worker"); n != stderrBudget {
		t.Fatalf("%d stderr records over 3 processes, want the budget of one run, %d", n, stderrBudget)
	}

	if n := e.log.countLines("further worker output dropped"); n != 1 {
		t.Fatalf("%d notices of dropped output, want 1", n)
	}
}
