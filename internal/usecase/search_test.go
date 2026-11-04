package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// searchFixture is a Search over fakes with Notes writing through it.
type searchFixture struct {
	search     *Search
	notes      *Notes
	store      *fakeNoteStore
	engine     *fakeEngine
	embeddings *fakeEmbeddingStore
	changes    *ChangeTracker

	mu       sync.Mutex
	warnings []error
	statuses []SearchStatus
	reported broadcast // notified after every warning and status
}

func newSearchFixture(t *testing.T, notes ...domain.Note) *searchFixture {
	t.Helper()

	f := &searchFixture{
		store:      newFakeNoteStore(notes...),
		engine:     newFakeEngine(),
		embeddings: &fakeEmbeddingStore{},
		changes:    &ChangeTracker{},
	}

	s, err := NewSearch(SearchDeps{
		Engine:     f.engine,
		Notes:      f.store,
		Embeddings: f.embeddings,
		Changes:    f.changes,
		OnStatus: func(st SearchStatus) {
			f.mu.Lock()
			f.statuses = append(f.statuses, st)
			f.mu.Unlock()
			f.reported.notify()
		},
		Warn: func(err error) {
			f.mu.Lock()
			f.warnings = append(f.warnings, err)
			f.mu.Unlock()
			f.reported.notify()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	f.search = s

	f.notes, err = NewNotes(f.store, fixedClock(t1), seqIDs(), f.changes, s)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

// run starts Run until the test ends and then checks that it returned nil.
func (f *searchFixture) run(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- f.search.Run(ctx) }()

	t.Cleanup(func() {
		cancel()

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run = %v, want nil after cancel", err)
			}
		case <-time.After(hangGuard):
			t.Error("Run did not return after cancel")
		}
	})
}

// matchesStore reports whether the engine index holds exactly the stored notes.
func (f *searchFixture) matchesStore() bool {
	index, _ := f.engine.snapshot()
	stored, _ := f.store.ListNotes(context.Background())

	if len(index) != len(stored) {
		return false
	}

	for _, n := range stored {
		got, ok := index[n.ID]
		if !ok || got.Name != n.Name || got.Info != n.Info {
			return false
		}
	}

	return true
}

func (f *searchFixture) waitMatchesStore(t *testing.T) {
	t.Helper()
	waitUntil(t, &f.engine.changed, "the index to match the store", f.matchesStore)
}

// waitCalls waits until the engine has received n commands and returns them.
func (f *searchFixture) waitCalls(t *testing.T, n int) []engineCall {
	t.Helper()

	var calls []engineCall

	waitUntil(t, &f.engine.changed, fmt.Sprintf("%d index commands", n), func() bool {
		_, calls = f.engine.snapshot()
		return len(calls) >= n
	})

	return calls
}

func (f *searchFixture) warningCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.warnings)
}

// send delivers ev to the event goroutine of Run.
func (f *searchFixture) send(t *testing.T, ev SearchEvent) {
	t.Helper()

	select {
	case f.engine.events <- ev:
	case <-time.After(hangGuard):
		t.Fatal("Run does not read events")
	}
}

// receive returns the value from ch or fails the test.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(hangGuard):
		t.Fatalf("timed out waiting for %s", what)
	}

	panic("unreachable")
}

func kinds(calls []engineCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Kind)
	}

	return out
}

func TestNewSearchNil(t *testing.T) {
	full := SearchDeps{Engine: newFakeEngine(), Notes: newFakeNoteStore(), Embeddings: &fakeEmbeddingStore{},
		Changes: &ChangeTracker{}, Warn: func(error) {}}

	tests := []struct {
		name   string
		modify func(*SearchDeps)
	}{
		{name: "engine", modify: func(d *SearchDeps) { d.Engine = nil }},
		{name: "notes", modify: func(d *SearchDeps) { d.Notes = nil }},
		{name: "embeddings", modify: func(d *SearchDeps) { d.Embeddings = nil }},
		{name: "changes", modify: func(d *SearchDeps) { d.Changes = nil }},
		{name: "warn", modify: func(d *SearchDeps) { d.Warn = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := full
			tt.modify(&d)

			if _, err := NewSearch(d); err == nil {
				t.Error("want an error for a nil dependency")
			}
		})
	}

	if _, err := NewSearch(full); err != nil {
		t.Errorf("NewSearch without OnStatus: %v", err)
	}
}

func TestSearchRebuildMatchesStore(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"), note(2, "b", "y"), note(3, "c", "z"))
	f.embeddings.records = map[uuid.UUID][]byte{testID(2): []byte("emb-2")}
	f.run(t)

	calls := f.waitCalls(t, 2)
	if got := kinds(calls); !slices.Equal(got, []string{"reset", "upsert"}) {
		t.Fatalf("commands = %v, want reset, upsert", got)
	}

	if !f.matchesStore() {
		t.Error("the index does not match the store")
	}

	if f.changes.Dirty() || f.changes.Generation() != 0 {
		t.Error("the rebuild counted as an edit for synchronization")
	}

	revs := map[uint64]bool{}

	for _, n := range calls[1].Notes {
		want := []byte(nil)
		if n.ID == testID(2) {
			want = []byte("emb-2")
		}

		if !bytes.Equal(n.Embedding, want) {
			t.Errorf("note %v embedding = %q, want %q", n.ID, n.Embedding, want)
		}

		if n.Rev == 0 || revs[n.Rev] {
			t.Errorf("note %v Rev = %d, want a new nonzero number", n.ID, n.Rev)
		}

		revs[n.Rev] = true
	}
}

func TestSearchRebuildBatches(t *testing.T) {
	var notes []domain.Note
	for i := range 250 {
		notes = append(notes, domain.Note{ID: uuid.New(), Name: fmt.Sprint("n", i), Info: "x"})
	}

	f := newSearchFixture(t, notes...)
	f.run(t)

	calls := f.waitCalls(t, 4)

	var sizes []int
	for _, c := range calls[1:] {
		sizes = append(sizes, len(c.Notes))
	}

	if calls[0].Kind != "reset" || !slices.Equal(sizes, []int{100, 100, 50}) {
		t.Errorf("commands = %v with batches %v, want reset and batches 100, 100, 50", kinds(calls), sizes)
	}

	f.waitMatchesStore(t)
}

func TestSearchOrderSaveDuringRebuild(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "v1"), note(2, "b", "y"), note(3, "c", "z"))
	gate, entered := make(chan struct{}), make(chan struct{})
	f.engine.gate, f.engine.entered = gate, entered
	f.run(t)

	receive(t, entered, "the rebuild's Upsert")

	ctx := context.Background()
	if _, err := f.notes.Save(ctx, testID(1), "a", "v2"); err != nil {
		t.Fatal(err)
	}

	if err := f.notes.Delete(ctx, testID(2)); err != nil {
		t.Fatal(err)
	}

	close(gate)

	calls := f.waitCalls(t, 4)
	if got := kinds(calls); !slices.Equal(got, []string{"reset", "upsert", "upsert", "remove"}) {
		t.Errorf("commands = %v, want reset, upsert, upsert, remove", got)
	}

	if !f.matchesStore() {
		index, _ := f.engine.snapshot()
		t.Errorf("index = %+v, want the store: a v2 and c", index)
	}
}

func TestSearchTwoSavesKeepLast(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"))
	f.run(t)
	f.waitMatchesStore(t)

	ctx := context.Background()
	for _, text := range []string{"x1", "x2"} {
		if _, err := f.notes.Save(ctx, testID(1), "a", text); err != nil {
			t.Fatal(err)
		}
	}

	calls := f.waitCalls(t, 4)

	index, _ := f.engine.snapshot()
	if got := index[testID(1)].Info; got != "x2" {
		t.Errorf("indexed text = %q, want the last save x2", got)
	}

	var revs []uint64
	for _, c := range calls[1:] {
		revs = append(revs, c.Notes[0].Rev)
	}

	for i := 1; i < len(revs); i++ {
		if revs[i] <= revs[i-1] {
			t.Errorf("revs = %v, want them to grow", revs)
		}
	}
}

func TestSearchRestoreRebuilds(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "local"), note(2, "b", "only local"))
	srv := &fakeServer{serverID: serverA, revision: 5,
		notes: []domain.Note{note(1, "a", "server"), note(4, "d", "new on server")}}

	sync, err := NewSync(SyncDeps{
		Local:    &fakeSyncState{snap: LocalSnapshot{Revision: 4, ServerID: serverA}, notes: f.store},
		Settings: &fakeSettings{s: completeSettings()}, Remote: srv, Changes: f.changes, IDs: seqIDs(),
		Gate: &Gate{}, Offer: &HostKeyOffer{}, Warn: func(error) {}, Index: f.search,
	})
	if err != nil {
		t.Fatal(err)
	}

	f.run(t)
	f.waitMatchesStore(t)

	ctx := context.Background()
	if _, err := sync.PrepareRestore(ctx); err != nil {
		t.Fatal(err)
	}

	if err := sync.CommitRestore(ctx); err != nil {
		t.Fatal(err)
	}

	calls := f.waitCalls(t, 4)
	if got := kinds(calls); !slices.Equal(got, []string{"reset", "upsert", "reset", "upsert"}) {
		t.Errorf("commands = %v, want a second reset and upsert after the Restore", got)
	}

	if !f.matchesStore() {
		index, _ := f.engine.snapshot()
		t.Errorf("index = %+v, want the restored notes", index)
	}

	if index, _ := f.engine.snapshot(); index[testID(1)].Info != "server" {
		t.Errorf("note 1 text = %q, want the server's", index[testID(1)].Info)
	}
}

func TestSearchRestartedRebuilds(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "v1"), note(2, "b", "y"))
	gate, entered := make(chan struct{}), make(chan struct{})
	f.engine.gate, f.engine.entered = gate, entered
	f.run(t)

	receive(t, entered, "the rebuild's Upsert")

	// This upsert is queued behind the held one; the restart must drop it.
	if _, err := f.notes.Save(context.Background(), testID(1), "a", "v2"); err != nil {
		t.Fatal(err)
	}

	f.send(t, SearchEvent{Restarted: true})
	waitUntil(t, &f.embeddings.changed, "the second rebuild", func() bool {
		f.embeddings.mu.Lock()
		defer f.embeddings.mu.Unlock()

		return f.embeddings.lists == 2
	})

	// The rebuild enqueues under the edit lock, so this edit waits for it.
	if err := f.changes.Edit(func() (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}

	close(gate)

	calls := f.waitCalls(t, 4)
	if got := kinds(calls[:4]); !slices.Equal(got, []string{"reset", "upsert", "reset", "upsert"}) {
		t.Errorf("commands = %v, want the queued upsert dropped before the new reset", got)
	}

	f.waitMatchesStore(t)
}

func TestSearchEmbeddedCurrentRevOnly(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "v1"), note(2, "b", "y"))
	f.run(t)
	f.waitMatchesStore(t)

	index, _ := f.engine.snapshot()
	oldRev := index[testID(1)].Rev

	ctx := context.Background()
	if _, err := f.notes.Save(ctx, testID(1), "a", "v2"); err != nil {
		t.Fatal(err)
	}

	if err := f.notes.Delete(ctx, testID(2)); err != nil {
		t.Fatal(err)
	}

	calls := f.waitCalls(t, 4)
	curRev, removedRev := calls[2].Notes[0].Rev, calls[3].Removals[0].Rev

	f.changes.Clear()
	gen := f.changes.Generation()

	f.send(t, SearchEvent{Embedded: &EmbeddedEvent{ID: testID(1), Rev: oldRev, Data: []byte("old")}})
	f.send(t, SearchEvent{Embedded: &EmbeddedEvent{ID: testID(2), Rev: removedRev, Data: []byte("deleted")}})
	f.send(t, SearchEvent{Embedded: &EmbeddedEvent{ID: testID(3), Rev: curRev, Data: []byte("unknown")}})
	f.send(t, SearchEvent{Embedded: &EmbeddedEvent{ID: testID(1), Rev: curRev, Data: []byte("current")}})

	waitUntil(t, &f.embeddings.changed, "PutEmbedding", func() bool { return len(f.embeddings.putCalls()) > 0 })

	// Events are handled in order, so the three before the current one are done.
	want := []embeddingPut{{ID: testID(1), Data: []byte("current")}}
	if got := f.embeddings.putCalls(); len(got) != 1 || got[0].ID != want[0].ID || !bytes.Equal(got[0].Data, want[0].Data) {
		t.Errorf("PutEmbedding calls = %+v, want only %+v", got, want)
	}

	if f.changes.Dirty() || f.changes.Generation() != gen {
		t.Errorf("Dirty %v, generation %d: storing an embedding must not count as an edit",
			f.changes.Dirty(), f.changes.Generation())
	}
}

func TestSearchUnreadableEmbeddingsDeleted(t *testing.T) {
	broken := testID(9)
	f := newSearchFixture(t, note(1, "a", "x"))
	f.embeddings.records = map[uuid.UUID][]byte{testID(1): []byte("emb-1")}
	f.embeddings.unreadable = []uuid.UUID{broken}
	f.run(t)

	waitUntil(t, &f.embeddings.changed, "DeleteEmbeddings", func() bool {
		f.embeddings.mu.Lock()
		defer f.embeddings.mu.Unlock()

		return len(f.embeddings.deleted) > 0
	})

	f.embeddings.mu.Lock()
	deleted := slices.Clone(f.embeddings.deleted)
	f.embeddings.mu.Unlock()

	if len(deleted) != 1 || !slices.Equal(deleted[0], []uuid.UUID{broken}) {
		t.Errorf("DeleteEmbeddings calls = %v, want [[%v]]", deleted, broken)
	}

	waitUntil(t, &f.reported, "a warning", func() bool { return f.warningCount() > 0 })

	calls := f.waitCalls(t, 2)
	if got := calls[1].Notes[0].Embedding; !bytes.Equal(got, []byte("emb-1")) {
		t.Errorf("embedding = %q, want the readable record", got)
	}
}

func TestSearchStatus(t *testing.T) {
	f := newSearchFixture(t)
	f.run(t)

	want := SearchStatus{Notes: 3, Queued: 1, Lexical: SearchReady, Semantic: SearchLoading}
	f.send(t, SearchEvent{Status: &want})

	waitUntil(t, &f.reported, "OnStatus", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()

		return len(f.statuses) > 0
	})

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.statuses[0] != want {
		t.Errorf("OnStatus got %+v, want %+v", f.statuses[0], want)
	}
}

func TestSearchQuery(t *testing.T) {
	f := newSearchFixture(t)
	f.engine.hits = []SearchHit{{NoteID: testID(1), Line: 2, Text: "x"}}

	got, err := f.search.Query(context.Background(), "x", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].NoteID != testID(1) || !slices.Equal(f.engine.queries, []string{"x"}) {
		t.Errorf("Query = %+v with engine queries %v, want the engine's hit for x", got, f.engine.queries)
	}
}

func TestNotesSaveNotBlockedByEngine(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"))
	f.engine.hang = true
	f.run(t)

	done := make(chan error, 1)

	go func() {
		ctx := context.Background()

		n, err := f.notes.Create(ctx, "b", "y")
		if err == nil {
			_, err = f.notes.Save(ctx, testID(1), "a", "x2")
		}

		if err == nil {
			err = f.notes.Delete(ctx, n.ID)
		}

		done <- err
	}()

	if err := receive(t, done, "Notes writes with a hung engine"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchRunStartError(t *testing.T) {
	f := newSearchFixture(t)
	errStart := errors.New("no worker")
	f.engine.startErr = errStart

	if err := f.search.Run(context.Background()); !errors.Is(err, errStart) {
		t.Errorf("Run = %v, want the start error", err)
	}
}

func TestSearchRunTwice(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"))
	f.run(t)
	f.waitMatchesStore(t)

	if err := f.search.Run(context.Background()); err == nil {
		t.Error("a second Run while the first runs returned nil")
	}
}

func TestSearchIdleKeepsNoCommands(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"))

	if _, err := f.notes.Save(context.Background(), testID(1), "a", "x2"); err != nil {
		t.Fatal(err)
	}

	if err := f.notes.Delete(context.Background(), testID(1)); err != nil {
		t.Fatal(err)
	}

	if n := f.search.queue.len(); n != 0 {
		t.Errorf("queue holds %d commands before Run, want none: Run rebuilds from the store", n)
	}
}

func TestSearchRunStopsWithHungEngine(t *testing.T) {
	f := newSearchFixture(t, note(1, "a", "x"))
	f.engine.hang = true

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- f.search.Run(ctx) }()

	// The rebuild has listed the embeddings and enqueues under the edit
	// lock, so once this edit runs the sender is on its way to the hung Reset.
	waitUntil(t, &f.embeddings.changed, "the rebuild", func() bool {
		f.embeddings.mu.Lock()
		defer f.embeddings.mu.Unlock()

		return f.embeddings.lists == 1
	})

	if err := f.changes.Edit(func() (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}

	cancel()

	if err := receive(t, done, "Run to return"); err != nil {
		t.Errorf("Run = %v, want nil after cancel", err)
	}

	if n := f.search.queue.len(); n != 0 {
		t.Errorf("queue holds %d commands after Run, want none", n)
	}
}

func TestIndexQueue(t *testing.T) {
	q := newIndexQueue()

	for i := range 1000 { // far more than any buffer: push never waits
		q.push(indexCommand{kind: cmdRemove, removals: []IndexedRemoval{{Rev: uint64(i + 1)}}})
	}

	receive(t, q.ready(), "the ready signal")

	for i := range 3 {
		c, ok := q.pop()
		if !ok || c.removals[0].Rev != uint64(i+1) {
			t.Fatalf("pop %d = %+v, %v, want Rev %d", i, c, ok, i+1)
		}
	}

	q.clear()

	if _, ok := q.pop(); ok || q.len() != 0 {
		t.Errorf("after clear: len %d, want an empty queue", q.len())
	}
}
