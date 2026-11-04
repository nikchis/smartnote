package searchworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// fakePath is the fake worker built by TestMain.
var fakePath string

// fakeStderrForm logs the stderr lines of the fake worker as they are, so
// the tests can follow it.
var fakeStderrForm = stderrForm{re: regexp.MustCompile(`^fake: [a-z0-9 =-]+$`)}

// fakeAnswered is the one query the fake answers in mode ignore-search.
const fakeAnswered = "answered"

// hangGuard bounds every wait of a test that would otherwise hang.
const hangGuard = 20 * time.Second

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "searchworker-fake")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)

	fakePath = filepath.Join(dir, "fakeworker")
	if runtime.GOOS == "windows" {
		fakePath += ".exe"
	}

	build := exec.CommandContext(context.Background(), "go", "build", "-o", fakePath, "./testdata/fakeworker")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build the fake worker: %v\n%s", err, out)
		return 1
	}

	return m.Run()
}

// logRecorder keeps the records of the engine's logger.
type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
	added   chan struct{} // signalled after each record
}

func newLogRecorder() *logRecorder { return &logRecorder{added: make(chan struct{}, 1)} }

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	r.records = append(r.records, rec.Clone())
	r.mu.Unlock()

	select {
	case r.added <- struct{}{}:
	default:
	}

	return nil
}

// waitLine waits for a record that contains all of parts.
func (r *logRecorder) waitLine(t *testing.T, parts ...string) {
	t.Helper()

	guard := time.After(hangGuard)

	for r.countLines(parts...) == 0 {
		select {
		case <-r.added:
		case <-guard:
			t.Fatalf("no log record with %q within %v", parts, hangGuard)
		}
	}
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *logRecorder) WithGroup(string) slog.Handler { return r }

// lines returns the records as text, one per record.
func (r *logRecorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	lines := make([]string, 0, len(r.records))

	for _, rec := range r.records {
		var b strings.Builder

		b.WriteString(rec.Message)
		rec.Attrs(func(a slog.Attr) bool {
			fmt.Fprintf(&b, " %s=%s", a.Key, a.Value)
			return true
		})

		lines = append(lines, b.String())
	}

	return lines
}

// countLines counts the records that contain all of parts.
func (r *logRecorder) countLines(parts ...string) int {
	n := 0

	for _, line := range r.lines() {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}

		if all {
			n++
		}
	}

	return n
}

// afterCall is one pause the engine asked for.
type afterCall struct {
	d    time.Duration
	fire chan time.Time
}

// manualAfter records the pauses; the test ends each one.
type manualAfter struct {
	calls chan afterCall
}

func newManualAfter() *manualAfter { return &manualAfter{calls: make(chan afterCall, 64)} }

func (m *manualAfter) After(d time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	m.calls <- afterCall{d: d, fire: c}

	return c
}

// immediateAfter ends every pause at once.
func immediateAfter(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()

	return c
}

type testEngine struct {
	*Engine
	log *logRecorder
}

// newTestEngine returns an engine over the fake worker in mode; edit may
// change the configuration. The engine is closed at the end of the test.
func newTestEngine(t *testing.T, mode string, edit func(*Config), env ...string) testEngine {
	t.Helper()

	rec := newLogRecorder()
	cfg := Config{
		Path:     fakePath,
		OrtLib:   "/opt/ort/libonnxruntime.so",
		ModelDir: "/opt/model",
		Threads:  2,
		Log:      slog.New(rec),
		Timeouts: Timeouts{Hello: 10 * time.Second, Command: 10 * time.Second, Search: 5 * time.Second},
		After:    immediateAfter,
	}

	if edit != nil {
		edit(&cfg)
	}

	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	e.extraEnv = append([]string{"FAKE_MODE=" + mode}, env...)
	e.out = newWorkerOutput(cfg.Log, append(slices.Clone(workerStderrForms), fakeStderrForm))

	t.Cleanup(func() { _ = e.Close() })

	return testEngine{Engine: e, log: rec}
}

func (e testEngine) start(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), hangGuard)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// waitEvent reads events until match accepts one.
func (e testEngine) waitEvent(t *testing.T, what string, match func(usecase.SearchEvent) bool) usecase.SearchEvent {
	t.Helper()

	guard := time.After(hangGuard)

	for {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				t.Fatalf("events closed while waiting for %s", what)
			}

			if match(ev) {
				return ev
			}
		case <-guard:
			t.Fatalf("no %s within %v", what, hangGuard)
		}
	}
}

func isRestarted(ev usecase.SearchEvent) bool { return ev.Restarted }

func testNote(info string) usecase.IndexedNote {
	return usecase.IndexedNote{ID: uuid.New(), Rev: 1, Name: "note", Info: info}
}

func TestNewValidates(t *testing.T) {
	log := slog.New(newLogRecorder())

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no path", cfg: Config{Log: log}},
		{name: "relative path", cfg: Config{Path: "smartnote-search", Log: log}},
		{name: "no log", cfg: Config{Path: fakePath}},
		{name: "negative threads", cfg: Config{Path: fakePath, Log: log, Threads: -1}},
		{name: "threads over u16", cfg: Config{Path: fakePath, Log: log, Threads: 1 << 16}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.cfg); err == nil {
				t.Fatal("New accepted the configuration")
			}
		})
	}
}

func TestEngineHelloAndSearch(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil, "ORT_LOGGING_LEVEL=0", "LD_PRELOAD=/nonexistent/inject.so")
	e.start(t)

	info, ok := e.Ready()
	if !ok {
		t.Fatal("no ready after Start")
	}

	wantWorker := fmt.Sprintf("fake proto=1 threads=2 model=/opt/model ort=/opt/ort/libonnxruntime.so parent=true env=%s", "ORT_DISABLE_TELEMETRY=1")
	if !strings.HasPrefix(info.Worker, wantWorker) || strings.Contains(info.Worker, "ORT_LOGGING_LEVEL") || strings.Contains(info.Worker, "LD_PRELOAD") {
		t.Fatalf("worker %q, want prefix %q and no other ORT_ or LD_PRELOAD", info.Worker, wantWorker)
	}

	if info.Lexical != usecase.SearchReady || info.Semantic != usecase.SearchLoading || info.Sandbox != "none" {
		t.Fatalf("ready %+v", info)
	}

	e.waitEvent(t, "semantic ready status", func(ev usecase.SearchEvent) bool {
		if ev.Restarted {
			t.Fatal("Restarted after the first start")
		}

		return ev.Status != nil && ev.Status.Semantic == usecase.SearchReady
	})

	ctx := t.Context()
	note := testNote("if (a == b)")

	if err := e.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if err := e.Upsert(ctx, []usecase.IndexedNote{note}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	ev := e.waitEvent(t, "embedded", func(ev usecase.SearchEvent) bool { return ev.Embedded != nil })
	if ev.Embedded.ID != note.ID || ev.Embedded.Rev != 1 || string(ev.Embedded.Data) != "emb:"+note.ID.String() {
		t.Fatalf("embedded %+v", ev.Embedded)
	}

	hits, err := e.Search(ctx, "==", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := usecase.SearchHit{
		NoteID: note.ID, Line: 1, Start: 6, End: 8, Text: "==", Snippet: "if (a == b)",
		Marks: [][2]int{{6, 8}}, Sources: []string{"substring"}, Score: 0.5,
	}
	if len(hits) != 1 || fmt.Sprint(hits[0]) != fmt.Sprint(want) {
		t.Fatalf("hits %+v, want [%+v]", hits, want)
	}

	if err := e.Remove(ctx, []usecase.IndexedRemoval{{ID: note.ID, Rev: 2}}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if hits, err = e.Search(ctx, "==", 10); err != nil || len(hits) != 0 {
		t.Fatalf("Search after Remove: %v, %v", hits, err)
	}

	// stderr is logged by its own goroutine: wait for the line, then count.
	e.log.waitLine(t, "source=search-worker", "fake: started mode=ok")

	if n := e.log.countLines("source=search-worker", "fake: started mode=ok"); n != 1 {
		t.Fatalf("%d stderr records of the start, want 1; log:\n%s", n, strings.Join(e.log.lines(), "\n"))
	}
}

func TestEngineUpsertOverLimit(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil)
	e.start(t)

	notes := make([]usecase.IndexedNote, maxUpsert+1)
	for i := range notes {
		notes[i] = testNote("x")
	}

	if err := e.Upsert(t.Context(), notes); err == nil {
		t.Fatal("Upsert accepted 101 notes")
	}

	if err := e.Upsert(t.Context(), notes[:maxUpsert]); err != nil {
		t.Fatalf("Upsert of 100 notes: %v", err)
	}
}

// An upsert whose cached embeddings do not fit one frame is sent again
// without them, and the worker computes them, instead of losing the notes.
// The notes are those of the review of PR 6: 100 valid notes with records of
// 125 chunks, 21 MB in one upsert.
func TestEngineUpsertOverFrameDropsEmbeddings(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil)
	e.start(t)

	ctx := t.Context()
	name := strings.Repeat("я", 64)
	info := strings.TrimSuffix(strings.Repeat(strings.Repeat("я", 64)+"\n", 126), "\n")
	record := make([]byte, 194922)

	notes := make([]usecase.IndexedNote, maxUpsert)
	for i := range notes {
		notes[i] = usecase.IndexedNote{ID: uuid.New(), Rev: 1, Name: name, Info: info, Embedding: record}
	}

	if err := e.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if err := e.Upsert(ctx, notes); err != nil {
		t.Fatalf("Upsert of %d notes with cached embeddings: %v", len(notes), err)
	}

	// The worker got them without the records: it reports new ones.
	seen := map[uuid.UUID]bool{}

	e.waitEvent(t, "an embedded event for every note", func(ev usecase.SearchEvent) bool {
		if ev.Embedded != nil {
			seen[ev.Embedded.ID] = true
		}

		return len(seen) == len(notes)
	})

	hits, err := e.Search(ctx, "я", 100)
	if err != nil || len(hits) != len(notes) {
		t.Fatalf("Search: %d hits, %v; want %d", len(hits), err, len(notes))
	}
}

// A note with invalid UTF-8 (from a Restore of another client's data) is
// indexed with U+FFFD in place of the bad bytes: the worker rejects a
// message with invalid UTF-8 and exits, and the rebuild after its restart
// would send the note again.
func TestEngineUpsertInvalidUTF8(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil)
	e.start(t)

	ctx := t.Context()
	note := usecase.IndexedNote{ID: uuid.New(), Rev: 1, Name: "n\xff", Info: "a\xffb \xe2\x82x"}

	if err := e.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if err := e.Upsert(ctx, []usecase.IndexedNote{note}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	hits, err := e.Search(ctx, "a\uFFFDb", 10)
	if err != nil || len(hits) != 1 || hits[0].NoteID != note.ID {
		t.Fatalf("Search: %+v, %v; want the note", hits, err)
	}

	// The worker indexes the text as the interface gets it through JSON, one
	// U+FFFD for each bad byte, so the offsets of its hits fit the editor.
	shown, err := json.Marshal(note.Info)
	if err != nil {
		t.Fatal(err)
	}

	var want string
	if err := json.Unmarshal(shown, &want); err != nil {
		t.Fatal(err)
	}

	if hits[0].Snippet != want {
		t.Fatalf("indexed text %q, want %q as the interface shows it", hits[0].Snippet, want)
	}

	if gen := e.liveGen.Load(); gen != 1 {
		t.Fatalf("generation %d, want 1: the worker restarted", gen)
	}
}

func TestEngineFloodDoesNotBlockReplies(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "flood-events", func(c *Config) { c.Timeouts.Command = 5 * time.Second })
	e.start(t)

	// Nobody reads Events while the worker floods them.
	note := testNote("flood")
	if err := e.Upsert(t.Context(), []usecase.IndexedNote{note}); err != nil {
		t.Fatalf("Upsert during a flood of events: %v", err)
	}

	// The embedded events are all kept, in order.
	var next uint64

	e.waitEvent(t, "all flooded embedded events", func(ev usecase.SearchEvent) bool {
		if ev.Embedded == nil {
			return false
		}

		if ev.Embedded.Rev != next {
			t.Fatalf("embedded rev %d, want %d", ev.Embedded.Rev, next)
		}

		next++

		return next == 10000
	})

	if gen := e.liveGen.Load(); gen != 1 {
		t.Fatalf("generation %d, want 1", gen)
	}
}

func TestEngineCommandTimeoutRestarts(t *testing.T) {
	t.Parallel()

	const timeout = 300 * time.Millisecond

	e := newTestEngine(t, "hang-on-upsert", func(c *Config) { c.Timeouts.Command = timeout })
	e.start(t)

	began := time.Now()

	err := e.Upsert(t.Context(), []usecase.IndexedNote{testNote("x")})
	if !errors.Is(err, errTimeout) {
		t.Fatalf("Upsert: %v, want errTimeout", err)
	}

	if took := time.Since(began); took < timeout || took > hangGuard {
		t.Fatalf("Upsert returned after %v, want about %v", took, timeout)
	}

	e.waitEvent(t, "Restarted", isRestarted)

	if gen := e.liveGen.Load(); gen != 2 {
		t.Fatalf("generation %d, want 2", gen)
	}

	if _, ok := e.Ready(); !ok {
		t.Fatal("no ready worker after the restart")
	}

	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset on the new worker: %v", err)
	}
}

func TestEngineSearchTimeout(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ignore-search", func(c *Config) { c.Timeouts.Search = 200 * time.Millisecond })
	e.start(t)

	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	hits, err := e.Search(t.Context(), "query", 10)
	if !errors.Is(err, usecase.ErrSearchUnavailable) || len(hits) != 0 {
		t.Fatalf("Search: %v, %v; want no hits and ErrSearchUnavailable", hits, err)
	}

	// A timeout is reported to the caller, not logged as an error for each query.
	if got := e.log.levels("search timed out"); slices.Contains(got, slog.LevelError) {
		t.Fatalf("levels of the timeout record = %v, want none at ERROR", got)
	}

	// The worker is the same one and still answers.
	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if gen := e.liveGen.Load(); gen != 1 {
		t.Fatalf("generation %d, want 1", gen)
	}
}

// Three searches in a row that time out mean a hung worker: it is killed and
// restarted, as after a command timeout. An answered search starts the count
// again.
func TestEngineSearchTimeoutsRestart(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ignore-search", func(c *Config) { c.Timeouts.Search = 100 * time.Millisecond })
	e.start(t)

	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	search := func(query string) error {
		_, err := e.Search(t.Context(), query, 10)
		return err
	}

	for _, q := range []string{"lost", "lost", fakeAnswered, "lost", "lost"} {
		err := search(q)
		if q == fakeAnswered && err != nil {
			t.Fatalf("Search %q: %v", q, err)
		}

		if q != fakeAnswered && !errors.Is(err, usecase.ErrSearchUnavailable) {
			t.Fatalf("Search %q: %v, want ErrSearchUnavailable", q, err)
		}
	}

	if gen := e.liveGen.Load(); gen != 1 {
		t.Fatalf("generation %d after two timeouts in a row, want 1", gen)
	}

	if err := search("lost"); !errors.Is(err, usecase.ErrSearchUnavailable) {
		t.Fatalf("third timeout: %v, want ErrSearchUnavailable", err)
	}

	e.waitEvent(t, "Restarted", isRestarted)

	if gen := e.liveGen.Load(); gen != 2 {
		t.Fatalf("generation %d after three timeouts in a row, want 2", gen)
	}
}

// Between the loss of a worker and the ready of the next one the engine
// reports the search unavailable, and searches fail with
// ErrSearchUnavailable instead of finding nothing (spec §9.1, §11); so they
// do on the next worker until its reset.
func TestEngineNoWorkerReportsUnavailable(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "exit-after-n", func(c *Config) { c.After = after.After }, "FAKE_N=3")
	e.start(t)

	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset on the ready worker: %v", err)
	}

	if _, err := e.Search(t.Context(), "x", 10); err != nil {
		t.Fatalf("Search on the ready worker: %v", err)
	}

	if err := e.Reset(t.Context()); err == nil {
		t.Fatal("Reset succeeded")
	}

	e.waitEvent(t, "unavailable status", func(ev usecase.SearchEvent) bool {
		return ev.Status != nil && ev.Status.Lexical == usecase.SearchUnavailable
	})

	var pause afterCall

	select {
	case pause = <-after.calls:
	case <-time.After(hangGuard):
		t.Fatal("no pause after the failure")
	}

	if _, err := e.Search(t.Context(), "x", 10); !errors.Is(err, usecase.ErrSearchUnavailable) {
		t.Fatalf("Search without a worker: %v, want ErrSearchUnavailable", err)
	}

	pause.fire <- time.Now()

	e.waitEvent(t, "Restarted", isRestarted)
	e.waitEvent(t, "ready status", func(ev usecase.SearchEvent) bool {
		return ev.Status != nil && ev.Status.Lexical == usecase.SearchReady
	})

	if _, err := e.Search(t.Context(), "x", 10); !errors.Is(err, usecase.ErrSearchUnavailable) {
		t.Fatalf("Search on the new worker before its reset: %v, want ErrSearchUnavailable", err)
	}

	if err := e.Reset(t.Context()); err != nil {
		t.Fatalf("Reset on the new worker: %v", err)
	}

	if _, err := e.Search(t.Context(), "x", 10); err != nil {
		t.Fatalf("Search after the restart: %v", err)
	}
}

// Until the first worker is ready the search is unavailable too.
func TestEngineUnavailableBeforeFirstReady(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "crash-on-hello", func(c *Config) { c.After = after.After })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = e.Start(ctx) }()

	select {
	case <-after.calls:
	case <-time.After(hangGuard):
		t.Fatal("no pause after the failed start")
	}

	if _, err := e.Search(t.Context(), "x", 10); !errors.Is(err, usecase.ErrSearchUnavailable) {
		t.Fatalf("Search before the first ready: %v, want ErrSearchUnavailable", err)
	}

	ev := e.waitEvent(t, "status", func(ev usecase.SearchEvent) bool { return ev.Status != nil })
	if ev.Status.Lexical != usecase.SearchUnavailable {
		t.Fatalf("status %+v, want lexical unavailable", ev.Status)
	}
}

// Close is no failure: it reports no unavailable status.
func TestEngineCloseReportsNoUnavailable(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil)
	e.start(t)

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	for {
		item, ok := e.queue.pop()
		if !ok {
			break
		}

		if s := item.ev.Status; s != nil && s.Lexical == usecase.SearchUnavailable {
			t.Fatalf("status %+v of generation %d after Close", s, item.gen)
		}
	}
}

func TestEngineCrashBackoffAndGiveUp(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "crash-on-hello", func(c *Config) { c.After = after.After })

	started := make(chan error, 1)
	go func() { started <- e.Start(t.Context()) }()

	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second} {
		select {
		case call := <-after.calls:
			if call.d != want {
				t.Fatalf("pause %v, want %v", call.d, want)
			}

			call.fire <- time.Now()
		case <-time.After(hangGuard):
			t.Fatalf("no pause of %v", want)
		}
	}

	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(hangGuard):
		t.Fatal("Start did not return after the engine gave up")
	}

	ev := e.waitEvent(t, "status", func(ev usecase.SearchEvent) bool { return ev.Status != nil })
	if ev.Status.Lexical != usecase.SearchUnavailable || ev.Status.Semantic != usecase.SearchUnavailable {
		t.Fatalf("status %+v, want both unavailable", ev.Status)
	}

	select {
	case call := <-after.calls:
		t.Fatalf("pause of %v after giving up", call.d)
	default:
	}

	if n := e.log.countLines("fake: started"); n != 6 {
		t.Fatalf("%d starts, want 6 (the first and 5 restarts)", n)
	}

	// Off for good: commands do nothing, searches are unavailable.
	if err := e.Upsert(t.Context(), []usecase.IndexedNote{testNote("x")}); err != nil {
		t.Fatalf("Upsert after giving up: %v", err)
	}

	if hits, err := e.Search(t.Context(), "x", 10); !errors.Is(err, usecase.ErrSearchUnavailable) || len(hits) != 0 {
		t.Fatalf("Search after giving up: %v, %v; want ErrSearchUnavailable", hits, err)
	}

	if err := e.Start(t.Context()); err != nil {
		t.Fatalf("Start after giving up: %v", err)
	}
}

func TestEngineProtocolFailuresRestart(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"bad-frame", "exit-after-n"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, mode, nil)
			e.start(t)

			if err := e.Reset(t.Context()); err == nil {
				t.Fatal("Reset succeeded")
			}

			e.waitEvent(t, "Restarted", isRestarted)

			if gen := e.liveGen.Load(); gen != 2 {
				t.Fatalf("generation %d, want 2", gen)
			}

			if n := e.log.countLines("search worker failed", "gen=1"); n != 1 {
				t.Fatalf("%d failure records, want 1; log:\n%s", n, strings.Join(e.log.lines(), "\n"))
			}
		})
	}
}

func TestEngineOldGenerationDropped(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "exit-after-n", nil)
	e.start(t)

	if err := e.Reset(t.Context()); err == nil {
		t.Fatal("Reset succeeded")
	}

	e.waitEvent(t, "Restarted", isRestarted)

	// The reader of the first process delivers late, after the restart.
	oldID, newID := uuid.New(), uuid.New()
	e.queue.push(1, usecase.SearchEvent{Embedded: &usecase.EmbeddedEvent{ID: oldID, Rev: 1}})
	e.queue.push(2, usecase.SearchEvent{Embedded: &usecase.EmbeddedEvent{ID: newID, Rev: 1}})

	ev := e.waitEvent(t, "embedded", func(ev usecase.SearchEvent) bool { return ev.Embedded != nil })
	if ev.Embedded.ID != newID {
		t.Fatal("an event of the previous process reached Events")
	}
}

func TestEngineMissingBinary(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "ok", func(c *Config) {
		c.Path = filepath.Join(t.TempDir(), "smartnote-search")
		c.After = after.After
	})
	e.start(t)

	ev := e.waitEvent(t, "status", func(ev usecase.SearchEvent) bool { return ev.Status != nil })
	if ev.Status.Lexical != usecase.SearchUnavailable || ev.Status.Semantic != usecase.SearchUnavailable {
		t.Fatalf("status %+v, want both unavailable", ev.Status)
	}

	if _, ok := e.Ready(); ok {
		t.Fatal("ready without a worker")
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	for ev := range e.Events() {
		if ev.Status != nil {
			t.Fatalf("a second status %+v", ev.Status)
		}
	}

	if len(after.calls) != 0 {
		t.Fatal("the engine paused for a restart")
	}

	if lines := e.log.lines(); len(lines) > 5 {
		t.Fatalf("%d log records, want at most 5:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestEngineCloseKillsHungWorker(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "hang-on-upsert", func(c *Config) { c.Timeouts.Command = time.Minute })
	e.start(t)

	upserted := make(chan error, 1)
	go func() { upserted <- e.Upsert(context.Background(), []usecase.IndexedNote{testNote("x")}) }()

	// The upsert reached the worker, which hangs and ignores the end of stdin.
	e.log.waitLine(t, "source=search-worker", "fake: hanging")

	began := time.Now()

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("Close took %v, want at most 3s", took)
	}

	select {
	case err := <-upserted:
		if err == nil {
			t.Fatal("the hung Upsert succeeded")
		}
	case <-time.After(hangGuard):
		t.Fatal("the hung Upsert did not return after Close")
	}

	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if _, ok := <-e.Events(); ok {
		t.Fatal("events open after Close")
	}

	if err := e.Reset(t.Context()); !errors.Is(err, errClosed) {
		t.Fatalf("Reset after Close: %v, want errClosed", err)
	}
}

func TestEngineCallsHonourContext(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "hang-on-upsert", func(c *Config) { c.Timeouts.Command = time.Minute })
	e.start(t)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	began := time.Now()

	if err := e.Upsert(ctx, []usecase.IndexedNote{testNote("x")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Upsert: %v, want the context's error", err)
	}

	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("Upsert returned after %v", took)
	}

	canceled, cancelNow := context.WithCancel(t.Context())
	cancelNow()

	if err := e.Start(canceled); err != nil {
		t.Fatalf("Start of a started engine: %v", err)
	}
}

// A worker that does not answer hello within its timeout is killed and
// started again after a pause (spec §8.1, §12.5).
func TestEngineHelloTimeoutRestarts(t *testing.T) {
	t.Parallel()

	const timeout = 200 * time.Millisecond

	after := newManualAfter()
	e := newTestEngine(t, "hang-on-hello", func(c *Config) {
		c.Timeouts.Hello = timeout
		c.After = after.After
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	began := time.Now()

	go func() { _ = e.Start(ctx) }()

	select {
	case call := <-after.calls:
		if call.d != time.Second {
			t.Fatalf("pause %v, want 1s", call.d)
		}
	case <-time.After(hangGuard):
		t.Fatal("no pause after the hello timed out")
	}

	if took := time.Since(began); took < timeout {
		t.Fatalf("gave up on hello after %v, want at least %v", took, timeout)
	}

	if _, ok := e.Ready(); ok {
		t.Fatal("ready without an answer to hello")
	}

	if n := e.log.countLines("search worker failed", "gen=1", "no answer in time"); n != 1 {
		t.Fatalf("%d failure records of the hello timeout, want 1; log:\n%s", n, strings.Join(e.log.lines(), "\n"))
	}
}

func TestEngineStartHonoursContext(t *testing.T) {
	t.Parallel()

	after := newManualAfter()
	e := newTestEngine(t, "crash-on-hello", func(c *Config) { c.After = after.After })

	ctx, cancel := context.WithCancel(t.Context())

	started := make(chan error, 1)
	go func() { started <- e.Start(ctx) }()

	// The first start failed and the engine waits for the first pause.
	select {
	case <-after.calls:
	case <-time.After(hangGuard):
		t.Fatal("no pause after the failed start")
	}

	cancel()

	select {
	case err := <-started:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start: %v, want context.Canceled", err)
		}
	case <-time.After(hangGuard):
		t.Fatal("Start ignored its context")
	}
}

// levels returns the levels of the records that contain all of parts.
func (r *logRecorder) levels(parts ...string) []slog.Level {
	r.mu.Lock()
	records := slices.Clone(r.records)
	r.mu.Unlock()

	lines := r.lines()

	var out []slog.Level

	for i, line := range lines {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}

		if all {
			out = append(out, records[i].Level)
		}
	}

	return out
}

// The status of a new worker waits until its Restarted is queued, so the
// consumer rebuilds the index before it learns that the search is ready.
func TestEventQueueHoldsStatusOfUnannouncedGeneration(t *testing.T) {
	t.Parallel()

	q := newEventQueue()
	q.open(1)

	ready := usecase.SearchStatus{Lexical: usecase.SearchReady, Semantic: usecase.SearchReady}
	q.setStatus(2, ready)

	if item, ok := q.pop(); ok {
		t.Fatalf("popped %+v before generation 2 was announced", item)
	}

	q.push(2, usecase.SearchEvent{Restarted: true})
	q.open(2)

	if item, ok := q.pop(); !ok || !item.ev.Restarted {
		t.Fatalf("first %+v, %v; want Restarted", item, ok)
	}

	if item, ok := q.pop(); !ok || item.ev.Status == nil || *item.ev.Status != ready {
		t.Fatalf("second %+v, %v; want the ready status", item, ok)
	}

	// The engine's own status is never held.
	q.setStatus(0, unavailableStatus)

	if item, ok := q.pop(); !ok || item.gen != 0 {
		t.Fatalf("%+v, %v; want the status of generation 0", item, ok)
	}
}

// storedNotes is a usecase.NoteLister over fixed notes.
type storedNotes []domain.Note

func (n storedNotes) ListNotes(context.Context) ([]domain.Note, error) { return n, nil }

// heldEmbeddings is a usecase.EmbeddingStore without records whose first
// PutEmbedding waits for release: the event handler of usecase.Search is
// busy meanwhile.
type heldEmbeddings struct {
	entered, release chan struct{}
	once             sync.Once
}

func (e *heldEmbeddings) ListEmbeddings(context.Context) (map[uuid.UUID][]byte, []uuid.UUID, error) {
	return nil, nil, nil
}

func (e *heldEmbeddings) DeleteEmbeddings(context.Context, []uuid.UUID) error { return nil }

func (e *heldEmbeddings) PutEmbedding(context.Context, uuid.UUID, []byte) error {
	e.once.Do(func() {
		close(e.entered)
		<-e.release
	})

	return nil
}

// A new worker answers from an empty index until its first reset: before
// usecase.Search handles Restarted (its event handler may be busy), a search
// must be unavailable, not "No matches" (spec §8.3). Reproduction of the
// review of PR 6.
func TestEngineRestartUnavailableUntilReset(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, "ok", nil)
	em := &heldEmbeddings{entered: make(chan struct{}), release: make(chan struct{})}
	notes := storedNotes{{ID: uuid.New(), Name: "note", Info: "unique existing text"}}

	s, err := usecase.NewSearch(usecase.SearchDeps{
		Engine: e.Engine, Notes: notes, Embeddings: em, Changes: &usecase.ChangeTracker{},
		Warn: func(err error) { t.Log(err) },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- s.Run(ctx) }()

	defer func() {
		close(em.release)
		cancel()
		<-done
	}()

	select {
	case <-em.entered:
	case <-time.After(hangGuard):
		t.Fatal("no embedding stored")
	}

	if hits, err := s.Query(ctx, "unique", 10); err != nil || len(hits) != 1 {
		t.Fatalf("Query before the restart: %d hits, %v; want 1", len(hits), err)
	}

	old, _ := e.current()
	old.kill(errExited)

	deadline := time.Now().Add(hangGuard)

	for {
		if g, _ := e.current(); g != nil && g.n > old.n {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("no new worker")
		}

		time.Sleep(time.Millisecond)
	}

	hits, err := s.Query(ctx, "unique", 10)
	if !errors.Is(err, usecase.ErrSearchUnavailable) {
		t.Fatalf("Query on the new worker before its reset: %d hits, %v; want ErrSearchUnavailable", len(hits), err)
	}
}
