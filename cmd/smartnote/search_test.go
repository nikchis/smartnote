package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/searchworker"
	"github.com/nikchis/smartnote/internal/platform"
	"github.com/nikchis/smartnote/internal/usecase"
)

// blockingRun counts its runs and returns when ctx ends.
type blockingRun struct {
	runs     atomic.Int32
	started  chan struct{}
	returned atomic.Bool
}

func (b *blockingRun) Run(ctx context.Context) error {
	b.runs.Add(1)
	close(b.started)
	<-ctx.Done()
	b.returned.Store(true)

	return nil
}

func TestSearchRunnerStartsOnceAndWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := newSearchRunner(ctx, slog.New(slog.DiscardHandler))
	b := &blockingRun{started: make(chan struct{})}

	r.start(b)
	r.start(b)

	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start")
	}

	cancel()
	r.wait()

	if !b.returned.Load() {
		t.Error("wait returned before Run")
	}

	if n := b.runs.Load(); n != 1 {
		t.Errorf("Run calls = %d, want 1", n)
	}
}

type countingRun struct{ runs atomic.Int32 }

func (c *countingRun) Run(context.Context) error {
	c.runs.Add(1)
	return nil
}

func TestSearchRunnerAfterCancelStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := newSearchRunner(ctx, slog.New(slog.DiscardHandler))
	c := &countingRun{}

	cancel()
	r.wait()
	r.start(c)
	r.wait()

	if n := c.runs.Load(); n != 0 {
		t.Errorf("Run calls after the end = %d, want 0", n)
	}
}

type failingRun struct{}

func (failingRun) Run(context.Context) error { return errors.New("search: start engine: boom") }

func TestSearchRunnerLogsStartError(t *testing.T) {
	var buf bytes.Buffer

	r := newSearchRunner(t.Context(), slog.New(slog.NewTextHandler(&buf, nil)))

	r.start(failingRun{})
	r.wait()

	if got := buf.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "boom") {
		t.Errorf("log = %q, want the error of Run", got)
	}
}

func TestNewSearchEngine(t *testing.T) {
	unavailable := usecase.SearchStatus{Lexical: usecase.SearchUnavailable, Semantic: usecase.SearchUnavailable}
	loading := usecase.SearchStatus{Lexical: usecase.SearchLoading, Semantic: usecase.SearchLoading}
	found := platform.SearchLocation{Worker: "/opt/smartnote/smartnote-search", OrtLib: "/opt/ort.so", ModelDir: "/opt/model"}

	tests := []struct {
		name       string
		loc        platform.SearchLocation
		err        error
		wantWorker bool
		wantStatus usecase.SearchStatus
		wantLog    string
	}{
		{"missing worker", platform.SearchLocation{}, fmt.Errorf("%w: /usr/lib/smartnote/smartnote-search", platform.ErrSearchMissing), false, unavailable, "search worker not found"},
		{"unreadable layout", platform.SearchLocation{}, errors.New("search worker: permission denied"), false, unavailable, "permission denied"},
		{"relative worker path", platform.SearchLocation{Worker: "smartnote-search"}, nil, false, unavailable, "not absolute"},
		{"worker found", found, nil, true, loading, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

			engine, status := newSearchEngine(log, func() (platform.SearchLocation, error) { return tc.loc, tc.err })

			t.Cleanup(func() { _ = engine.Close() })

			if _, isWorker := engine.(*searchworker.Engine); isWorker != tc.wantWorker {
				t.Errorf("engine = %T, want the worker: %v", engine, tc.wantWorker)
			}

			if status != tc.wantStatus {
				t.Errorf("status = %+v, want %+v", status, tc.wantStatus)
			}

			lines := strings.Count(buf.String(), "\n")

			switch {
			case tc.wantLog == "" && lines != 0:
				t.Errorf("log = %q, want nothing", buf.String())
			case tc.wantLog != "" && (lines != 1 || !strings.Contains(buf.String(), tc.wantLog) || !strings.Contains(buf.String(), "kind=search-warning")):
				t.Errorf("log = %q, want one search-warning record with %q", buf.String(), tc.wantLog)
			}
		})
	}
}

func TestSearchThreads(t *testing.T) {
	for cpus, want := range map[int]int{1: 1, 2: 1, 4: 2, 8: 4, 32: 4} {
		if got := searchThreads(cpus); got != want {
			t.Errorf("searchThreads(%d) = %d, want %d", cpus, got, want)
		}
	}
}
