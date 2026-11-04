package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// SearchEngine is the search index outside Go: the smartnote-search worker
// (spec §3.2). Index commands come from one goroutine in order; Search may
// run beside them.
type SearchEngine interface {
	// Start starts (or restarts) the worker and greets it.
	Start(ctx context.Context) error
	Reset(ctx context.Context) error
	// Upsert takes up to 100 notes.
	Upsert(ctx context.Context, notes []IndexedNote) error
	Remove(ctx context.Context, ids []IndexedRemoval) error
	Search(ctx context.Context, query string, limit int) ([]SearchHit, error)
	// Events carries Embedded, Status and Restarted.
	Events() <-chan SearchEvent
	Close() error
}

// IndexedNote is a note as the index receives it.
type IndexedNote struct {
	ID uuid.UUID
	// Rev is the global monotonic change number, kept in memory only.
	Rev        uint64
	Name, Info string
	// Embedding is the decrypted note_embedding record or nil.
	Embedding []byte
}

// IndexedRemoval removes note ID from the index as change Rev.
type IndexedRemoval struct {
	ID  uuid.UUID
	Rev uint64
}

// SearchHit is one found place (spec §3.2): Line 0 is the name, 1…n the lines
// of the text; Start and End are UTF-16 offsets within the name or the whole
// text, and Text is the indexed text between them. Marks are UTF-16 ranges
// within Snippet; Sources name the searches that found it.
type SearchHit struct {
	NoteID           uuid.UUID
	Line, Start, End int
	Text, Snippet    string
	Marks            [][2]int
	Sources          []string
	Score            float32
}

// SearchState is the state of one kind of search.
type SearchState string

// Search states.
const (
	SearchLoading     SearchState = "loading"
	SearchReady       SearchState = "ready"
	SearchUnavailable SearchState = "unavailable"
)

// SearchStatus is what the worker reports about its index.
type SearchStatus struct {
	Notes, Queued     int
	Lexical, Semantic SearchState
}

// SearchEvent is one event of the engine; exactly one field is set.
type SearchEvent struct {
	Embedded *EmbeddedEvent
	Status   *SearchStatus
	// Restarted reports a new worker process with an empty index.
	Restarted bool
}

// EmbeddedEvent carries the embedding record the worker computed for note ID
// at change Rev.
type EmbeddedEvent struct {
	ID   uuid.UUID
	Rev  uint64
	Data []byte
}

// EmbeddingStore keeps the encrypted embedding records of notes.
type EmbeddingStore interface {
	// ListEmbeddings returns the readable records and the IDs of those that
	// do not open.
	ListEmbeddings(ctx context.Context) (map[uuid.UUID][]byte, []uuid.UUID, error)
	PutEmbedding(ctx context.Context, id uuid.UUID, data []byte) error
	DeleteEmbeddings(ctx context.Context, ids []uuid.UUID) error
}

// NoteLister lists the stored notes.
type NoteLister interface {
	ListNotes(ctx context.Context) ([]domain.Note, error)
}

// NoteIndex learns about local note writes. Notes calls it inside the
// ChangeTracker.Edit callback after a successful write, so its calls follow
// the order of the writes; the calls only enqueue and never block.
type NoteIndex interface {
	NoteSaved(n domain.Note)
	NoteDeleted(id uuid.UUID)
}

// RestoreIndex learns that a Restore replaced the notes. Sync calls it inside
// ChangeTracker.ApplyIfUnchanged after the Restore is applied.
type RestoreIndex interface {
	NotesReplaced(ctx context.Context)
}

// SearchDeps are the dependencies of Search. OnStatus may be nil; it is called
// from one goroutine. Warn receives the errors of the index, which never fail
// a note operation; it must not log note text, and errors here carry none.
type SearchDeps struct {
	Engine     SearchEngine
	Notes      NoteLister
	Embeddings EmbeddingStore
	Changes    *ChangeTracker
	OnStatus   func(SearchStatus)
	Warn       func(error)
}

// ErrSearchUnavailable means the engine cannot answer a search now: no
// worker is ready (it is starting, restarting or was given up) or the worker
// did not answer in time. It is not an empty result: the interface shows
// "Search unavailable", not "No matches" (spec §9.1, §11).
var ErrSearchUnavailable = errors.New("search unavailable")

// upsertBatch is the most notes one Upsert carries (spec §3.2, §7.2).
const upsertBatch = 100

// errSearchRunning is returned by a Run while another one runs.
var errSearchRunning = errors.New("search: already running")

// Search keeps the search index in step with the local notes (spec §3.3) and
// answers queries.
//
// Index commands are enqueued only under the ChangeTracker edit lock: by
// Notes through NoteIndex, by Sync through RestoreIndex and by the rebuilds
// Search takes the lock for. So the queue holds them in the order of the
// writes to the store, and one sender goroutine passes them to the engine in
// that order. Enqueueing never waits for the engine.
//
// Commands are kept only while Run runs: Run starts with a rebuild from the
// store, which covers every write made before it.
type Search struct {
	d       SearchDeps
	queue   *indexQueue
	running atomic.Bool

	mu     sync.Mutex
	active bool                 // Run has started its first rebuild and not returned
	rev    uint64               // the last change number given out; never reset
	revs   map[uuid.UUID]uint64 // the last change number of each indexed note

	// statusMu orders the calls of OnStatus. While a rebuild is queued and
	// not yet applied, statuses are held (the latest one in held) and passed
	// on when the cmdRebuilt of the latest rebuild is reached: the interface
	// searches again when the search turns ready, and the index must be
	// filled by then. While the rebuild after a restart is pending, Query
	// reports the search unavailable: the new worker's index is incomplete.
	statusMu       sync.Mutex
	rebuilding     bool
	held           *SearchStatus
	rebuildSeq     uint64 // the number of the latest rebuild
	restartPending bool   // a rebuild after Restarted is not applied yet
}

// rebuildCause tells why the index is rebuilt.
type rebuildCause int

const (
	rebuildAtStart      rebuildCause = iota // the first rebuild of Run
	rebuildAfterRestart                     // a new worker with an empty index
	rebuildAfterRestore                     // a Restore replaced the notes
)

// NewSearch returns a Search over d.
func NewSearch(d SearchDeps) (*Search, error) {
	switch {
	case d.Engine == nil:
		return nil, nilDependency("NewSearch", "engine")
	case d.Notes == nil:
		return nil, nilDependency("NewSearch", "notes")
	case d.Embeddings == nil:
		return nil, nilDependency("NewSearch", "embeddings")
	case d.Changes == nil:
		return nil, nilDependency("NewSearch", "changes")
	case d.Warn == nil:
		return nil, nilDependency("NewSearch", "warn")
	}

	return &Search{d: d, queue: newIndexQueue(), revs: map[uuid.UUID]uint64{}}, nil
}

// Run starts the engine, rebuilds the index from the store and then, until
// ctx ends, passes the queued commands to the engine and handles its events.
// It returns nil once ctx ends, after its goroutines have stopped, or the
// error of Start. A failed command or rebuild goes to Warn; restarting the
// worker is the engine's job. Run does not close the engine: its owner does,
// after Run returns. Only one Run may run at a time.
func (s *Search) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errSearchRunning
	}
	defer s.running.Store(false)

	if err := s.d.Engine.Start(ctx); err != nil {
		return fmt.Errorf("search: start engine: %w", err)
	}

	defer s.stop()

	if err := s.rebuild(ctx, rebuildAtStart); err != nil {
		s.warn(ctx, err)
	}

	var wg sync.WaitGroup

	wg.Go(func() { s.send(ctx) })
	wg.Go(func() { s.handleEvents(ctx) })
	wg.Wait()

	return nil
}

// stop makes Search keep no commands until the next Run. It takes the edit
// lock, as every enqueue runs under it, so no rebuild is halfway through.
func (s *Search) stop() {
	_ = s.d.Changes.Edit(func() (bool, error) { // the callback returns no error
		s.mu.Lock()
		defer s.mu.Unlock()

		s.active = false
		s.queue.clear()
		clear(s.revs)

		return false, nil
	})

	s.statusMu.Lock()
	s.rebuilding, s.held, s.restartPending = false, nil, false
	s.statusMu.Unlock()
}

// Query searches the index, bypassing the queue of index commands (spec
// §3.3 rule 7). While the rebuild after a restart of the worker is not
// applied, it fails with ErrSearchUnavailable: the answer of an incomplete
// index would look like "No matches". During the rebuild at the start of Run
// it answers from the part already indexed (spec §11).
func (s *Search) Query(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	s.statusMu.Lock()
	pending := s.restartPending
	s.statusMu.Unlock()

	if pending {
		return nil, fmt.Errorf("search: index rebuilt after a restart: %w", ErrSearchUnavailable)
	}

	hits, err := s.d.Engine.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	return hits, nil
}

// NoteSaved enqueues n as a new change; its embedding is computed again.
// Notes calls it under the edit lock.
func (s *Search) NoteSaved(n domain.Note) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	rev := s.nextRevLocked()
	s.revs[n.ID] = rev
	s.queue.push(indexCommand{kind: cmdUpsert, notes: []IndexedNote{{ID: n.ID, Rev: rev, Name: n.Name, Info: n.Info}}})
}

// NoteDeleted enqueues the removal of note id as a new change. Notes calls it
// under the edit lock.
func (s *Search) NoteDeleted(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	rev := s.nextRevLocked()
	delete(s.revs, id)
	s.queue.push(indexCommand{kind: cmdRemove, removals: []IndexedRemoval{{ID: id, Rev: rev}}})
}

// NotesReplaced rebuilds the index after a Restore. Sync calls it under the
// edit lock, so it does not take the lock. The Restore is already applied, so
// the rebuild does not stop when ctx is canceled.
func (s *Search) NotesReplaced(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	if err := s.rebuildLocked(ctx, rebuildAfterRestore); err != nil {
		s.warn(ctx, err)
	}
}

// rebuild takes the edit lock and rebuilds the index (spec §3.3 rule 4). The
// rebuild is not an edit, so the change flag of synchronization stays as it
// is. The first rebuild of Run (rebuildAtStart) makes Search keep commands
// from now on.
func (s *Search) rebuild(ctx context.Context, cause rebuildCause) error {
	return s.d.Changes.Edit(func() (bool, error) {
		if cause == rebuildAtStart {
			s.mu.Lock()
			s.active = true
			s.mu.Unlock()
		}

		return false, s.rebuildLocked(ctx, cause)
	})
}

// rebuildLocked replaces the queued commands with a reset and upserts of all
// stored notes, with their readable embeddings; unreadable ones are deleted.
// The caller holds the edit lock, so later writes enqueue after the snapshot.
// If the notes cannot be listed, only the reset is enqueued: the index then
// holds only the notes saved after it, a partial index, until the next
// rebuild (the next unlock, Restore or worker restart).
func (s *Search) rebuildLocked(ctx context.Context, cause rebuildCause) error {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	if !active {
		return nil // Run rebuilds when it starts
	}

	s.queue.clear()

	notes, err := s.d.Notes.ListNotes(ctx)
	if err != nil {
		s.enqueueRebuild(nil, nil, cause)
		return fmt.Errorf("search: rebuild: %w", err)
	}

	s.enqueueRebuild(notes, s.readableEmbeddings(ctx), cause)

	return nil
}

// readableEmbeddings returns the embedding records the store can open and
// deletes the others, so their notes get new ones. Errors go to Warn: without
// a record the worker only computes the embedding again.
func (s *Search) readableEmbeddings(ctx context.Context) map[uuid.UUID][]byte {
	records, unreadable, err := s.d.Embeddings.ListEmbeddings(ctx)
	if err != nil {
		s.warn(ctx, fmt.Errorf("search: rebuild: %w", err))
		return nil
	}

	if len(unreadable) > 0 {
		err := s.d.Embeddings.DeleteEmbeddings(ctx, unreadable)
		if err == nil {
			err = fmt.Errorf("%d unreadable records deleted", len(unreadable))
		}

		s.warn(ctx, fmt.Errorf("search: rebuild: embeddings: %w", err))
	}

	return records
}

// enqueueRebuild enqueues a reset and upserts of notes in batches, each note
// with a new change number, and cmdRebuilt with the number of this rebuild
// after them, which it returns; statuses are held until the sender reaches
// it, and Query is unavailable meanwhile when cause is a restart.
func (s *Search) enqueueRebuild(notes []domain.Note, embeddings map[uuid.UUID][]byte, cause rebuildCause) uint64 {
	s.statusMu.Lock()
	s.rebuildSeq++
	seq := s.rebuildSeq
	s.rebuilding = true
	s.restartPending = s.restartPending || cause == rebuildAfterRestart
	s.statusMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	clear(s.revs)

	cmds := []indexCommand{{kind: cmdReset}}

	for batch := range slices.Chunk(notes, upsertBatch) {
		indexed := make([]IndexedNote, 0, len(batch))

		for _, n := range batch {
			rev := s.nextRevLocked()
			s.revs[n.ID] = rev
			indexed = append(indexed, IndexedNote{ID: n.ID, Rev: rev, Name: n.Name, Info: n.Info, Embedding: embeddings[n.ID]})
		}

		cmds = append(cmds, indexCommand{kind: cmdUpsert, notes: indexed})
	}

	s.queue.push(append(cmds, indexCommand{kind: cmdRebuilt, rebuild: seq})...)

	return seq
}

func (s *Search) nextRevLocked() uint64 {
	s.rev++
	return s.rev
}

// send passes the queued commands to the engine in order until ctx ends.
func (s *Search) send(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.queue.ready():
		}

		for ctx.Err() == nil {
			cmd, ok := s.queue.pop()
			if !ok {
				break
			}

			if err := s.apply(ctx, cmd); err != nil {
				s.warn(ctx, fmt.Errorf("search: %s: %w", cmd.kind, err))
			}
		}
	}
}

func (s *Search) apply(ctx context.Context, cmd indexCommand) error {
	switch cmd.kind {
	case cmdReset:
		return s.d.Engine.Reset(ctx)
	case cmdUpsert:
		return s.d.Engine.Upsert(ctx, cmd.notes)
	case cmdRemove:
		return s.d.Engine.Remove(ctx, cmd.removals)
	case cmdRebuilt:
		s.rebuilt(cmd.rebuild)
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd.kind)
	}
}

// handleEvents handles the engine's events until ctx ends or the engine
// closes its event channel.
func (s *Search) handleEvents(ctx context.Context) {
	events := s.d.Engine.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			s.handleEvent(ctx, ev)
		}
	}
}

func (s *Search) handleEvent(ctx context.Context, ev SearchEvent) {
	switch {
	case ev.Embedded != nil:
		if err := s.storeEmbedding(ctx, *ev.Embedded); err != nil {
			s.warn(ctx, err)
		}
	case ev.Status != nil:
		s.publish(*ev.Status)
	case ev.Restarted:
		// The new worker's index is empty and the old one's commands are gone.
		if err := s.rebuild(ctx, rebuildAfterRestart); err != nil {
			s.warn(ctx, err)
		}
	}
}

// publish passes st to OnStatus, or holds it while a rebuild is queued.
func (s *Search) publish(st SearchStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if s.rebuilding {
		s.held = &st
		return
	}

	if s.d.OnStatus != nil {
		s.d.OnStatus(st)
	}
}

// rebuilt ends the hold of the statuses and the unavailability after a
// restart once the commands of rebuild number seq are applied, and passes on
// the latest status held. Only the latest rebuild ends them: the marker of an
// older one that the sender took before a newer rebuild replaced the queue
// does nothing.
func (s *Search) rebuilt(seq uint64) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if seq != s.rebuildSeq {
		return
	}

	held := s.held
	s.rebuilding, s.held, s.restartPending = false, nil, false

	if held != nil && s.d.OnStatus != nil {
		s.d.OnStatus(*held)
	}
}

// storeEmbedding stores the record of e if it is for the last change of a
// note that is still there (spec §3.3 rule 6); otherwise the record is
// dropped. The check and the write run under the edit lock, so no write to
// the note comes between them; the store's foreign key cannot fail here.
func (s *Search) storeEmbedding(ctx context.Context, e EmbeddedEvent) error {
	return s.d.Changes.Edit(func() (bool, error) {
		s.mu.Lock()
		rev, ok := s.revs[e.ID]
		s.mu.Unlock()

		if !ok || rev != e.Rev {
			return false, nil
		}

		if err := s.d.Embeddings.PutEmbedding(ctx, e.ID, e.Data); err != nil {
			return false, fmt.Errorf("search: store embedding: %w", err)
		}

		return false, nil
	})
}

// warn sends err to Warn unless ctx has ended: then err only reports that.
func (s *Search) warn(ctx context.Context, err error) {
	if ctx.Err() == nil {
		s.d.Warn(err)
	}
}
