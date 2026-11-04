// Package searchworker runs the search worker smartnote-search and talks to
// it over its stdin and stdout (spec §7, §8.1): it implements
// usecase.SearchEngine.
package searchworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/usecase"
)

// Default timeouts (spec §8.1).
const (
	defaultHelloTimeout   = 30 * time.Second
	defaultCommandTimeout = 10 * time.Second
	defaultSearchTimeout  = 2 * time.Second
)

const (
	// closeGrace is how long Close waits for the worker to exit after closing
	// its stdin (spec §3.3 rule 9).
	closeGrace = 2 * time.Second
	// maxUpsert is the most notes one upsert carries (spec §7.2).
	maxUpsert = 100
	// maxSearchTimeouts is how many searches in a row may time out before
	// the worker counts as hung and is restarted, as after a command
	// timeout. The reader of a worker whose index thread hangs keeps reading
	// stdin, so only the searches notice it.
	maxSearchTimeouts = 3
)

var (
	errClosed = errors.New("search worker: engine closed")
	// errNoWorker means no worker is ready: the first one is starting, or the
	// last one failed and the next one has not started yet. Index commands
	// meanwhile succeed without being sent: the next worker reports Restarted
	// and gets the whole index again. (usecase.Search sends none before the
	// first worker is ready: its Run starts with Engine.Start, which waits for
	// that.) Searches meanwhile are unavailable.
	errNoWorker = errors.New("search worker: restarting")
	// errTimeout means the worker did not answer in time.
	errTimeout = errors.New("search worker: no answer in time")
)

// Timeouts bound the requests to the worker, writing included; a zero field
// takes the default: Hello 30 s, Command (reset, upsert, remove) 10 s, Search
// 2 s.
type Timeouts struct {
	Hello, Command, Search time.Duration
}

// Config configures an Engine.
type Config struct {
	// Path is the absolute path of the worker; OrtLib and ModelDir go to it in
	// hello. Threads is the number of model threads; the worker takes 0 as 1.
	// The caller chooses it (cmd/smartnote: half the CPUs, 1 to 4).
	Path, OrtLib, ModelDir string
	Threads                int
	// Log receives the failures of the worker at level Error, so the error log
	// keeps them, and its stderr lines (with source=search-worker) that match
	// the worker's own forms, at level Info for the timing of the model load
	// and Error for the rest (panics, sandbox and model problems); other
	// lines only with their length (stderr.go).
	Log      *slog.Logger
	Timeouts Timeouts
	// Backoff are the pauses before restarts; nil takes 1, 2, 4, 8, 16 s.
	Backoff []time.Duration
	// Now and After are the clock of the restart policy, for the tests; nil
	// takes time.Now and time.After.
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}

// ReadyInfo is the ready message of a worker.
type ReadyInfo struct {
	Worker            string
	Lexical, Semantic usecase.SearchState
	Sandbox           string
}

// Engine runs the worker and passes requests and events (spec §8.1). One
// supervisor goroutine starts the worker, restarts it with pauses after a
// failure and gives up after 5 failed restarts within 10 minutes (the sixth
// failure); a forwarder goroutine passes the events to Events. A failure is a
// failed start, an exit, a protocol violation, a command that timed out or
// the third search in a row that timed out. Each process is a generation:
// replies and events of earlier ones are dropped.
//
// While no worker is ready after a failure, the engine reports both searches
// unavailable (a status of generation 0, which the ready worker's status
// replaces), and searches fail with usecase.ErrSearchUnavailable, as does a
// search that times out. After giving up, the engine is off until the
// application restarts: commands do nothing and searches are unavailable.
type Engine struct {
	cfg      Config
	extraEnv []string // added to the parent's environment: a seam for the tests
	// out logs what the worker processes write, within the budget of a run
	// (stderr.go); the tests replace it to add the fake worker's lines.
	out *workerOutput

	events chan usecase.SearchEvent
	queue  *eventQueue
	req    atomic.Uint64 // the last request number
	// liveGen is the number of the newest process; events of others are
	// dropped.
	liveGen atomic.Uint64

	life   context.Context // ends at Close
	cancel context.CancelFunc
	wg     sync.WaitGroup // the supervisor and the forwarder

	settled    chan struct{} // closed when the first worker is ready or the engine gave up
	settleOnce sync.Once
	closeOnce  sync.Once

	mu      sync.Mutex
	started bool
	closed  bool
	gaveUp  bool
	cur     *generation // the ready worker, nil while there is none
	ready   ReadyInfo   // the ready message of cur
}

var _ usecase.SearchEngine = (*Engine)(nil)

// New returns an engine for cfg; Start starts the worker.
func New(cfg Config) (*Engine, error) {
	switch {
	case !filepath.IsAbs(cfg.Path):
		// A bare name would be looked up in PATH.
		return nil, fmt.Errorf("searchworker: worker path %q is not absolute", cfg.Path)
	case cfg.Log == nil:
		return nil, errors.New("searchworker: nil log")
	case cfg.Threads < 0 || cfg.Threads > math.MaxUint16:
		return nil, fmt.Errorf("searchworker: %d threads out of range", cfg.Threads)
	}

	cfg.Timeouts.Hello = orDefault(cfg.Timeouts.Hello, defaultHelloTimeout)
	cfg.Timeouts.Command = orDefault(cfg.Timeouts.Command, defaultCommandTimeout)
	cfg.Timeouts.Search = orDefault(cfg.Timeouts.Search, defaultSearchTimeout)

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	if cfg.After == nil {
		cfg.After = time.After
	}

	life, cancel := context.WithCancel(context.Background())

	return &Engine{
		cfg:     cfg,
		out:     newWorkerOutput(cfg.Log, workerStderrForms),
		events:  make(chan usecase.SearchEvent),
		queue:   newEventQueue(),
		life:    life,
		cancel:  cancel,
		settled: make(chan struct{}),
	}, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}

	return d
}

// Start starts the worker once and waits until the first worker is ready or
// the engine gave up; both return nil, as the status events tell the state.
// It returns early with the error of ctx; the worker keeps starting. Later
// calls wait the same way and start nothing.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()

	if e.closed {
		e.mu.Unlock()
		return errClosed
	}

	if !e.started {
		e.started = true

		e.wg.Go(e.supervise)
		e.wg.Go(e.forward)
	}

	e.mu.Unlock()

	select {
	case <-e.settled:
		return e.settledErr()
	default:
	}

	select {
	case <-e.settled:
		return e.settledErr()
	case <-ctx.Done():
		return fmt.Errorf("search worker: start: %w", ctx.Err())
	}
}

// settledErr is the result of Start once settled is closed: the supervisor
// also closes it when it stops at Close.
func (e *Engine) settledErr() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return errClosed
	}

	return nil
}

// Reset drops the index. Its ok makes the worker answer searches: every
// rebuild starts with a reset, and before the first one a new worker's index
// is empty, not the notes.
func (e *Engine) Reset(ctx context.Context) error {
	g, err := e.command(ctx, func(req uint64) any { return &resetMsg{T: tReset, Req: req} })
	if g != nil {
		g.wasReset.Store(true)
	}

	return err
}

// Upsert adds or replaces up to 100 notes; more are an error, as the worker
// takes at most 100 per message (spec §7.2) and usecase.Search sends batches
// of 100. Notes whose cached embeddings make the message larger than a frame
// are sent again without them: the worker computes them anew.
//
// Invalid UTF-8 in a name or text (a Restore takes the server's notes as they
// are) goes as U+FFFD (asShown): the worker rejects a message with invalid
// UTF-8 and exits, and every rebuild would send the note again.
func (e *Engine) Upsert(ctx context.Context, notes []usecase.IndexedNote) error {
	if len(notes) > maxUpsert {
		return fmt.Errorf("search worker: upsert of %d notes, at most %d", len(notes), maxUpsert)
	}

	in := make([]noteIn, len(notes))
	cached := false

	for i, n := range notes {
		in[i] = noteIn{
			ID: n.ID.String(), Rev: n.Rev,
			Name: asShown(n.Name), Info: asShown(n.Info),
			Emb: n.Embedding,
		}
		cached = cached || n.Embedding != nil
	}

	upsert := func(req uint64) any { return &upsertMsg{T: tUpsert, Req: req, Notes: in} }

	_, err := e.command(ctx, upsert)
	if !cached || !errors.Is(err, errFrameTooLarge) || workerLost(err) {
		return err // a frame too large from the worker is its failure
	}

	e.cfg.Log.Warn("search worker: upsert over the frame limit, sent without cached embeddings", slog.Int("notes", len(in)))

	for i := range in {
		in[i].Emb = nil
	}

	_, err = e.command(ctx, upsert)

	return err
}

// asShown is s with every byte of invalid UTF-8 replaced by U+FFFD, as
// encoding/json replaces them when the note goes to the interface, so the
// UTF-16 offsets of the worker's hits fit the text in the editor. The
// interface still checks the Text of a hit against the note before it
// selects the place (spec §9.2). strings.ToValidUTF8 would replace a run of
// bad bytes with one U+FFFD and shift the offsets after it.
func asShown(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	return string([]rune(s)) // a bad byte decodes as utf8.RuneError of width 1
}

// Remove removes notes.
func (e *Engine) Remove(ctx context.Context, ids []usecase.IndexedRemoval) error {
	items := make([]removalIn, len(ids))
	for i, r := range ids {
		items[i] = removalIn{ID: r.ID.String(), Rev: r.Rev}
	}

	_, err := e.command(ctx, func(req uint64) any { return &removeMsg{T: tRemove, Req: req, Items: items} })

	return err
}

// command sends an index command and waits for its ok; it returns the worker
// that answered ok. A timeout kills the worker, which the supervisor then
// restarts. Without a worker, while restarting or after giving up, it does
// nothing and returns nil.
func (e *Engine) command(ctx context.Context, msg func(req uint64) any) (*generation, error) {
	g, err := e.current()
	if g == nil {
		if errors.Is(err, errNoWorker) {
			err = nil // the next worker gets the whole index
		}

		return nil, err // nil after giving up too: the engine is off
	}

	req := e.req.Add(1)

	reply, err := g.call(ctx, req, msg(req), e.cfg.Timeouts.Command)
	if err != nil {
		if errors.Is(err, errTimeout) {
			g.kill(err)
		}

		return nil, e.callErr(err)
	}

	switch r := reply.(type) {
	case *okMsg:
		return g, nil
	case *errorMsg:
		return nil, fmt.Errorf("search worker: %s", e.out.errorText(r))
	default:
		err := fmt.Errorf("%w: %T answers a command", errProtocol, reply)
		g.kill(err)

		return nil, fmt.Errorf("search worker: %w", err)
	}
}

// Search searches the index for at most limit hits. Without a ready worker,
// before the worker's first reset, when the worker does not answer within the
// search timeout or is lost meanwhile (workerLost), it fails with
// usecase.ErrSearchUnavailable: the search could not run, which is not an
// empty result. The third timeout in a row restarts the worker. After Close
// it fails with errClosed.
func (e *Engine) Search(ctx context.Context, query string, limit int) ([]usecase.SearchHit, error) {
	g, err := e.current()
	if g == nil {
		if err == nil || errors.Is(err, errNoWorker) {
			err = usecase.ErrSearchUnavailable // restarting or given up
		}

		return nil, fmt.Errorf("search worker: %w", err)
	}

	// A new worker's index is empty until the rebuild that starts with a
	// reset: usecase.Search may not have handled Restarted yet.
	if !g.wasReset.Load() {
		return nil, fmt.Errorf("search worker: index not rebuilt yet: %w", usecase.ErrSearchUnavailable)
	}

	if limit <= 0 {
		return nil, nil
	}

	req := e.req.Add(1)
	msg := &searchMsg{T: tSearch, Req: req, Query: query, Limit: uint16(min(limit, math.MaxUint16))}

	reply, err := g.call(ctx, req, msg, e.cfg.Timeouts.Search)
	if errors.Is(err, errTimeout) {
		e.searchTimedOut(g)
		return nil, fmt.Errorf("search worker: search timed out: %w", usecase.ErrSearchUnavailable)
	}

	if err != nil {
		return nil, e.searchErr(err)
	}

	g.searchTimeouts.Store(0)

	switch r := reply.(type) {
	case *resultMsg:
		hits, err := searchHits(r.Hits)
		if err != nil {
			g.kill(err)
			return nil, e.searchErr(err)
		}

		return hits, nil
	case *errorMsg:
		return nil, fmt.Errorf("search worker: %s", e.out.errorText(r))
	default:
		err := fmt.Errorf("%w: %T answers a search", errProtocol, reply)
		g.kill(err)

		return nil, e.searchErr(err)
	}
}

// searchErr wraps the error of a search: a lost worker makes the search
// unavailable; the supervisor logs why, once.
func (e *Engine) searchErr(err error) error {
	if workerLost(err) && e.life.Err() == nil {
		return fmt.Errorf("search worker: %w: %w", usecase.ErrSearchUnavailable, err)
	}

	return e.callErr(err)
}

// searchTimedOut counts a search of g that timed out; the third in a row
// kills g, which the supervisor then restarts. A single timeout is logged
// below Error: the interface shows it and the error log keeps failures only.
func (e *Engine) searchTimedOut(g *generation) {
	n := g.searchTimeouts.Add(1)
	if n < maxSearchTimeouts {
		e.cfg.Log.Warn("search worker: search timed out", slog.Uint64("gen", g.n), slog.Int64("in_a_row", int64(n)))
		return
	}

	g.kill(fmt.Errorf("%d searches in a row: %w", n, errTimeout))
}

func searchHits(in []hitMsg) ([]usecase.SearchHit, error) {
	hits := make([]usecase.SearchHit, len(in))

	for i, h := range in {
		id, err := uuid.Parse(h.Note)
		if err != nil {
			return nil, fmt.Errorf("%w: hit note id", errProtocol)
		}

		marks := make([][2]int, len(h.Marks))
		for j, m := range h.Marks {
			marks[j] = [2]int{int(m[0]), int(m[1])}
		}

		hits[i] = usecase.SearchHit{
			NoteID: id, Line: int(h.Line), Start: int(h.Start), End: int(h.End),
			Text: h.Text, Snippet: h.Snippet, Marks: marks, Sources: h.Sources, Score: h.Score,
		}
	}

	return hits, nil
}

// callErr wraps the error of a request; after Close it is errClosed.
func (e *Engine) callErr(err error) error {
	if e.life.Err() != nil {
		return errClosed
	}

	return fmt.Errorf("search worker: %w", err)
}

// current returns the ready worker, or nil and errClosed, errNoWorker, or nil
// after giving up.
func (e *Engine) current() (*generation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch {
	case e.closed:
		return nil, errClosed
	case e.cur != nil:
		return e.cur, nil
	case e.gaveUp:
		return nil, nil
	default:
		return nil, errNoWorker
	}
}

// Ready returns the ready message of the current worker; false while no
// worker is ready.
func (e *Engine) Ready() (ReadyInfo, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.ready, e.cur != nil
}

// Events returns the events: Embedded, Status and Restarted. Status is the
// latest one: statuses not read meanwhile are dropped. The channel is closed
// by Close.
func (e *Engine) Events() <-chan usecase.SearchEvent { return e.events }

// Close stops the worker: it closes its stdin, waits 2 s for it to exit and
// then kills it. It waits for the goroutines of the engine and closes Events.
// Calls in progress fail; Close may be called more than once.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.mu.Unlock()

		e.cancel()
		e.wg.Wait()
		e.out.close()
		close(e.events)
	})

	return nil
}

// supervise starts the worker and restarts it after failures until Close or
// until it gives up.
func (e *Engine) supervise() {
	defer e.settle()

	policy := newRestartPolicy(e.cfg.Backoff)
	wasReady := false

	for {
		g, err := e.launch()
		if err == nil {
			e.serve(g, wasReady)
			wasReady = true

			if e.life.Err() != nil {
				return
			}

			err = g.failure()
			g.logFailure()
		} else if e.life.Err() == nil {
			e.cfg.Log.Error("search worker failed", slog.Uint64("gen", e.liveGen.Load()), applog.Err(err))
		}

		if e.life.Err() != nil {
			return
		}

		pause, ok := policy.failed(e.cfg.Now())
		if !ok || permanent(err) {
			e.giveUp()
			return
		}

		// No worker is ready until the next one is: generation 0, as every
		// goroutine of the lost one has stopped and cannot overwrite it; the
		// ready status of the next worker replaces it.
		e.queue.setStatus(0, unavailableStatus)

		select {
		case <-e.cfg.After(pause):
		case <-e.life.Done():
			return
		}
	}
}

// launch starts a new generation and greets it; on failure the generation is
// gone.
func (e *Engine) launch() (*generation, error) {
	n := e.liveGen.Add(1)

	env := workerEnv(append(os.Environ(), e.extraEnv...))

	g, err := startGeneration(n, e.cfg.Path, env, &e.cfg, e.queue, e.out)
	if err != nil {
		return nil, err
	}

	ready, err := g.hello(e.life, &helloMsg{
		T:         tHello,
		Proto:     protoVersion,
		ModelDir:  e.cfg.ModelDir,
		OrtLib:    e.cfg.OrtLib,
		Threads:   uint16(e.cfg.Threads), //nolint:gosec // checked by New
		ParentPID: uint32(os.Getpid()),   //nolint:gosec // process ids fit in 32 bits
	}, e.cfg.Timeouts.Hello)
	if err != nil {
		g.kill(err)
		g.teardown()

		return nil, err
	}

	e.mu.Lock()
	e.cur = g
	e.ready = ReadyInfo{
		Worker:   ready.Worker,
		Lexical:  usecase.SearchState(ready.Lexical),
		Semantic: usecase.SearchState(ready.Semantic),
		Sandbox:  ready.Sandbox,
	}
	e.mu.Unlock()

	return g, nil
}

// serve reports the ready worker g and waits until it fails or the engine
// closes; then g is gone. After an earlier ready worker the index is empty
// again: Restarted comes first, before any status of g, so the consumer
// rebuilds the index before it learns that the search is ready.
func (e *Engine) serve(g *generation, restarted bool) {
	if restarted {
		e.queue.push(g.n, usecase.SearchEvent{Restarted: true})
	}

	e.queue.open(g.n)

	e.settle()

	select {
	case <-g.dead:
	case <-e.life.Done():
	}

	e.mu.Lock()
	e.cur = nil
	e.ready = ReadyInfo{}
	e.mu.Unlock()

	if e.life.Err() != nil {
		g.shutdown(closeGrace)
		return
	}

	g.teardown()
}

// giveUp turns the engine off and reports both searches unavailable.
func (e *Engine) giveUp() {
	e.mu.Lock()
	e.gaveUp = true
	e.mu.Unlock()

	e.cfg.Log.Error("search worker: given up, search is unavailable until the next start of the application")
	// Generation 0: the status of the engine itself, never dropped.
	e.queue.setStatus(0, unavailableStatus)
	e.settle()
}

// unavailableStatus is the status of the engine while no worker is ready.
var unavailableStatus = usecase.SearchStatus{Lexical: usecase.SearchUnavailable, Semantic: usecase.SearchUnavailable}

func (e *Engine) settle() { e.settleOnce.Do(func() { close(e.settled) }) }

// forward passes the queued events to Events until Close; events of earlier
// generations are dropped.
func (e *Engine) forward() {
	for {
		item, ok := e.queue.next(e.life.Done())
		if !ok {
			return
		}

		if item.gen != 0 && item.gen != e.liveGen.Load() {
			continue
		}

		select {
		case e.events <- item.ev:
		case <-e.life.Done():
			return
		}
	}
}

// queued is an event of generation gen (0 for the engine itself).
type queued struct {
	gen uint64
	ev  usecase.SearchEvent
}

// eventQueue holds the events between the reader and the forwarder: the
// other events in order, without a limit, and the latest status. Adding never
// waits. A status of a generation above the open one is held until that
// generation opens.
type eventQueue struct {
	mu     sync.Mutex
	items  []queued
	status *queued
	opened uint64        // the newest generation whose statuses may pass
	wake   chan struct{} // capacity 1
}

func newEventQueue() *eventQueue { return &eventQueue{wake: make(chan struct{}, 1)} }

// push adds an event at the end.
func (q *eventQueue) push(gen uint64, ev usecase.SearchEvent) {
	q.mu.Lock()
	q.items = append(q.items, queued{gen: gen, ev: ev})
	q.mu.Unlock()
	q.signal()
}

// setStatus replaces the latest status.
func (q *eventQueue) setStatus(gen uint64, s usecase.SearchStatus) {
	q.mu.Lock()
	q.status = &queued{gen: gen, ev: usecase.SearchEvent{Status: &s}}
	q.mu.Unlock()
	q.signal()
}

// open lets the statuses of generation gen and earlier pass.
func (q *eventQueue) open(gen uint64) {
	q.mu.Lock()
	q.opened = max(q.opened, gen)
	q.mu.Unlock()
	q.signal()
}

func (q *eventQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default: // a wake-up is pending
	}
}

// next returns the first event, then the status unless it is held, waiting
// for one until done closes.
func (q *eventQueue) next(done <-chan struct{}) (queued, bool) {
	for {
		if item, ok := q.pop(); ok {
			return item, true
		}

		select {
		case <-q.wake:
		case <-done:
			return queued{}, false
		}
	}
}

func (q *eventQueue) pop() (queued, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) > 0 {
		item := q.items[0]
		q.items[0] = queued{}
		q.items = q.items[1:]

		if len(q.items) == 0 {
			q.items = nil // release the backing array
		}

		return item, true
	}

	if q.status != nil && q.status.gen <= q.opened {
		item := *q.status
		q.status = nil

		return item, true
	}

	return queued{}, false
}
