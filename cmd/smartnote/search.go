package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/nikchis/smartnote/internal/adapter/searchworker"
	"github.com/nikchis/smartnote/internal/adapter/wailsui"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/platform"
	"github.com/nikchis/smartnote/internal/usecase"
)

// maxSearchThreads bounds the model threads of the search worker: the
// embeddings are computed in the background beside the interface.
const maxSearchThreads = 4

// searchThreads is the number of model threads for cpus logical CPUs: half
// of them, at least 1 and at most maxSearchThreads.
func searchThreads(cpus int) int { return min(maxSearchThreads, max(1, cpus/2)) }

// findSearch locates the search worker next to the running executable.
func findSearch() (platform.SearchLocation, error) {
	// Without the path FindSearch reports the layout error, which is logged;
	// in development SMARTNOTE_SEARCH_DIR still works.
	exe, _ := os.Executable()

	return platform.FindSearch(exe)
}

// newSearchEngine returns the engine over the worker that find locates and
// the status of the search until the worker reports its own: loading. Without
// a usable worker it returns the engine that reports search unavailable and
// that status, and logs why once.
func newSearchEngine(log *slog.Logger, find func() (platform.SearchLocation, error)) (usecase.SearchEngine, usecase.SearchStatus) {
	unavailable := usecase.UnavailableStatus()

	loc, err := find()
	if err != nil {
		wailsui.LogSearchWarning(log, "search unavailable", err)
		return newUnavailableEngine(), unavailable
	}

	engine, err := searchworker.New(searchworker.Config{
		Path: loc.Worker, OrtLib: loc.OrtLib, ModelDir: loc.ModelDir,
		Threads: searchThreads(runtime.NumCPU()),
		Log:     log,
	})
	if err != nil {
		wailsui.LogSearchWarning(log, "search unavailable", err)
		return newUnavailableEngine(), unavailable
	}

	return engine, usecase.SearchStatus{Lexical: usecase.SearchLoading, Semantic: usecase.SearchLoading}
}

// runner is usecase.Search as searchRunner runs it.
type runner interface {
	Run(ctx context.Context) error
}

// searchRunner runs the search in the background once the storage is open
// and lets the shutdown wait for it. The worker is not started before: its
// events would have no reader.
type searchRunner struct {
	ctx context.Context //nolint:containedctx // the root context: the run lasts until the application quits
	log *slog.Logger

	mu      sync.Mutex
	started bool
	wg      sync.WaitGroup
}

// newSearchRunner returns a runner whose run lasts until ctx ends.
func newSearchRunner(ctx context.Context, log *slog.Logger) *searchRunner {
	return &searchRunner{ctx: ctx, log: log}
}

// start runs s.Run in a goroutine on the first call before ctx ends; later
// calls do nothing. The error of Run, which only a failed start returns, is
// logged unless ctx has ended.
func (r *searchRunner) start(s runner) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Checked under mu: once wait has taken mu after ctx ended, no run is added.
	if r.started || r.ctx.Err() != nil {
		return
	}

	r.started = true

	r.wg.Go(func() {
		if err := s.Run(r.ctx); err != nil && r.ctx.Err() == nil {
			r.log.Error("search", applog.Err(err))
		}
	})
}

// wait waits for the run to return; call it after ctx has ended. Taking mu
// first orders it after any start in progress, so wg.Wait sees that run.
func (r *searchRunner) wait() {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()

	if started {
		r.wg.Wait()
	}
}
