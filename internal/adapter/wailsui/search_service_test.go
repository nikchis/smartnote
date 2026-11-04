package wailsui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// hitsEngine answers every search with hits or err and records the limit;
// Search uses no other method of the engine.
type hitsEngine struct {
	usecase.SearchEngine

	hits  []usecase.SearchHit
	err   error
	limit int
}

func (e *hitsEngine) Search(_ context.Context, _ string, limit int) ([]usecase.SearchHit, error) {
	e.limit = limit
	return e.hits, e.err
}

type noNotes struct{}

func (noNotes) ListNotes(context.Context) ([]domain.Note, error) { return nil, nil }

type noEmbeddings struct{}

func (noEmbeddings) ListEmbeddings(context.Context) (map[uuid.UUID][]byte, []uuid.UUID, error) {
	return nil, nil, nil
}

func (noEmbeddings) PutEmbedding(context.Context, uuid.UUID, []byte) error { return nil }

func (noEmbeddings) DeleteEmbeddings(context.Context, []uuid.UUID) error { return nil }

func newTestSearch(t *testing.T, engine usecase.SearchEngine) *usecase.Search {
	t.Helper()

	s, err := usecase.NewSearch(usecase.SearchDeps{
		Engine: engine, Notes: noNotes{}, Embeddings: noEmbeddings{}, Changes: &usecase.ChangeTracker{},
		Warn: func(error) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// emitted is one event the feed sent.
type emitted struct {
	name string
	data []any
}

type emitRecorder struct {
	mu     sync.Mutex
	events []emitted
}

func (r *emitRecorder) emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, emitted{name: name, data: data})
}

func TestSearchServiceMapsHits(t *testing.T) {
	id := uuid.New()
	engine := &hitsEngine{hits: []usecase.SearchHit{
		{
			NoteID: id, Line: 2, Start: 5, End: 9, Text: "word", Snippet: "a word here",
			Marks: [][2]int{{2, 6}}, Sources: []string{"words", "semantic"}, Score: 0.7,
		},
		{NoteID: id}, // the engine may leave Marks and Sources nil
	}}
	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}
	svc := NewSearchService(env, newTestSearch(t, engine), NewSearchStatusFeed(usecase.SearchStatus{}, nil))

	hits, err := svc.Search("word")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if engine.limit != 10 {
		t.Errorf("limit = %d, want 10", engine.limit)
	}

	want := []SearchHitDTO{
		{
			NoteID: id.String(), Line: 2, Start: 5, End: 9, Text: "word", Snippet: "a word here",
			Marks: [][2]int{{2, 6}}, Sources: []string{"words", "semantic"},
		},
		{NoteID: id.String(), Marks: [][2]int{}, Sources: []string{}},
	}
	if !reflect.DeepEqual(hits, want) {
		t.Fatalf("hits = %+v, want %+v", hits, want)
	}

	b, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}

	wantJSON := `[{"noteId":"` + id.String() + `","line":2,"start":5,"end":9,"text":"word","snippet":"a word here",` +
		`"marks":[[2,6]],"sources":["words","semantic"]},` +
		`{"noteId":"` + id.String() + `","line":0,"start":0,"end":0,"text":"","snippet":"","marks":[],"sources":[]}]`
	if string(b) != wantJSON {
		t.Errorf("JSON = %s\nwant   %s", b, wantJSON)
	}
}

func TestSearchServiceFindsNothingAsEmptyList(t *testing.T) {
	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}
	svc := NewSearchService(env, newTestSearch(t, &hitsEngine{}), NewSearchStatusFeed(usecase.SearchStatus{}, nil))

	hits, err := svc.Search("nothing")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if b, _ := json.Marshal(hits); string(b) != "[]" {
		t.Errorf("JSON = %s, want []", b)
	}
}

func TestSearchServiceError(t *testing.T) {
	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}
	engine := &hitsEngine{err: errors.New("search worker: bad_request: query")}
	svc := NewSearchService(env, newTestSearch(t, engine), NewSearchStatusFeed(usecase.SearchStatus{}, nil))

	hits, err := svc.Search("x")
	if hits != nil {
		t.Errorf("hits = %v, want nil with an error", hits)
	}

	if code, _ := uiCode(t, err); code != codeInternal {
		t.Errorf("code = %q, want %q", code, codeInternal)
	}
}

// A search the engine cannot run is "Search unavailable" for the interface,
// not an empty list, and is not logged for every query.
func TestSearchServiceUnavailable(t *testing.T) {
	var buf bytes.Buffer

	env := Env{Ctx: t.Context(), Log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	engine := &hitsEngine{err: fmt.Errorf("search worker: search timed out: %w", usecase.ErrSearchUnavailable)}
	svc := NewSearchService(env, newTestSearch(t, engine), NewSearchStatusFeed(usecase.SearchStatus{}, nil))

	hits, err := svc.Search("x")
	if hits != nil {
		t.Errorf("hits = %v, want nil with an error", hits)
	}

	var ui *UIError
	if !errors.As(err, &ui) || ui.Code != codeSearchUnavailable || ui.Message != "Search unavailable" {
		t.Fatalf("error = %v, want code %q with message %q", err, codeSearchUnavailable, "Search unavailable")
	}

	if buf.Len() != 0 {
		t.Errorf("log = %q, want nothing", buf.String())
	}
}

func TestSearchServiceStatusEvent(t *testing.T) {
	var rec emitRecorder

	initial := usecase.SearchStatus{Lexical: usecase.SearchLoading, Semantic: usecase.SearchLoading}
	feed := NewSearchStatusFeed(initial, rec.emit)
	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}
	svc := NewSearchService(env, newTestSearch(t, &hitsEngine{}), feed)

	if got, want := svc.Status(), (SearchStatusDTO{Lexical: "loading", Semantic: "loading"}); got != want {
		t.Errorf("initial Status = %+v, want %+v", got, want)
	}

	feed.Publish(usecase.SearchStatus{Notes: 12, Queued: 3, Lexical: usecase.SearchReady, Semantic: usecase.SearchUnavailable})

	want := SearchStatusDTO{Notes: 12, Queued: 3, Lexical: "ready", Semantic: "unavailable"}
	if got := svc.Status(); got != want {
		t.Errorf("Status = %+v, want %+v", got, want)
	}

	if len(rec.events) != 1 || rec.events[0].name != EventSearchStatus || EventSearchStatus != "search:status" ||
		len(rec.events[0].data) != 1 || rec.events[0].data[0] != want {
		t.Fatalf("events = %+v, want one %s with %+v", rec.events, EventSearchStatus, want)
	}

	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	if s := string(b); s != `{"notes":12,"queued":3,"lexical":"ready","semantic":"unavailable"}` {
		t.Errorf("JSON = %s", s)
	}
}

func TestLogSearchWarning(t *testing.T) {
	var buf bytes.Buffer

	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	LogSearchWarning(log, "search", errors.New("search: rebuild: embeddings: 2 unreadable records deleted"))

	got := buf.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "kind=search-warning") ||
		!strings.Contains(got, "2 unreadable records deleted") || strings.Contains(got, "code=internal") {
		t.Errorf("log = %q, want one search-warning record that the error log keeps, not an internal error", got)
	}
}
