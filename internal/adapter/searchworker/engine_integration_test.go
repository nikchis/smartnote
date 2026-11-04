//go:build integration

package searchworker

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/usecase"
)

// TestEngineRealWorker runs the built smartnote-search named by
// SMARTNOTE_SEARCH_BIN without a model: words and substrings work, semantic
// search is unavailable.
func TestEngineRealWorker(t *testing.T) {
	bin := os.Getenv("SMARTNOTE_SEARCH_BIN")
	if bin == "" {
		t.Skip("SMARTNOTE_SEARCH_BIN is not set")
	}

	rec := newLogRecorder()
	dir := t.TempDir()

	e, err := New(Config{
		Path:     bin,
		OrtLib:   filepath.Join(dir, "libonnxruntime.so"),
		ModelDir: filepath.Join(dir, "model"),
		Threads:  1,
		Log:      slog.New(rec),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = e.Close() })

	te := testEngine{Engine: e, log: rec}

	te.start(t)

	info, ok := e.Ready()
	if !ok {
		t.Fatal("no ready after Start")
	}

	t.Logf("ready %+v", info)

	if info.Lexical != usecase.SearchReady || info.Semantic != usecase.SearchUnavailable {
		t.Fatalf("ready %+v, want lexical ready and semantic unavailable", info)
	}

	ctx := t.Context()
	note := testNote("if (a == b)")

	if err := e.Upsert(ctx, []usecase.IndexedNote{note}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	hits, err := e.Search(ctx, "==", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(hits) == 0 || hits[0].NoteID != note.ID || hits[0].Text != "==" {
		t.Fatalf("hits %+v, want the note at ==", hits)
	}

	t.Logf("hit %+v", hits[0])

	// Note and query text never reach the log (spec §12.5).
	const marker = "MARKERq7z3"

	secret := testNote("retry " + marker + " backoff")
	if err := e.Upsert(ctx, []usecase.IndexedNote{secret}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if hits, err = e.Search(ctx, marker, 10); err != nil || len(hits) == 0 {
		t.Fatalf("Search of the marker: %v, %v", hits, err)
	}

	began := time.Now()

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	if took := time.Since(began); took > closeGrace {
		t.Fatalf("Close took %v: the worker did not exit at the end of stdin", took)
	}

	for _, line := range rec.lines() {
		t.Log(line)

		if strings.Contains(strings.ToLower(line), strings.ToLower(marker)) {
			t.Fatalf("the log quotes note text: %s", line)
		}
	}
}
