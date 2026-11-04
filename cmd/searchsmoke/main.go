// Command searchsmoke checks an installed search worker: it starts the worker
// through the adapter of Smartnote, waits for semantic search to load,
// indexes one note and searches it. The package smoke tests run it.
//
//	searchsmoke -worker PATH -ort PATH -model DIR [-timeout 60s]
//
// It prints
//
//	ready worker=… lexical=… semantic=… sandbox=…
//	hits=N
//
// where semantic is the state after loading, and exits with code 1 when no
// worker became ready, semantic search is not ready, the search found nothing
// or, on Linux, the sandbox is not full; code 2 is a usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/searchworker"
	"github.com/nikchis/smartnote/internal/usecase"
)

// smokeQuery finds smokeText by the substring search, with or without the
// model.
const (
	smokeText  = "if (a == b)"
	smokeQuery = "=="
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args, runs the check and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("searchsmoke", flag.ContinueOnError)
	flags.SetOutput(stderr)

	worker := flags.String("worker", "", "path of the smartnote-search executable")
	ort := flags.String("ort", "", "path of the ONNX Runtime library")
	model := flags.String("model", "", "model directory")
	timeout := flags.Duration("timeout", 60*time.Second, "time limit of the whole check")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *worker == "" || *ort == "" || *model == "" || flags.NArg() > 0 {
		flags.Usage()
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	path, err := filepath.Abs(*worker)
	if err != nil {
		fmt.Fprintf(stderr, "searchsmoke: %v\n", err)
		return 2
	}

	cfg := searchworker.Config{
		Path:     path,
		OrtLib:   *ort,
		ModelDir: *model,
		Log:      slog.New(slog.NewTextHandler(stderr, nil)),
	}

	if err := smoke(ctx, cfg, stdout); err != nil {
		fmt.Fprintf(stderr, "searchsmoke: %v\n", err)
		return 1
	}

	return 0
}

// smoke runs the check and returns what failed.
func smoke(ctx context.Context, cfg searchworker.Config, out io.Writer) (err error) {
	engine, err := searchworker.New(cfg)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, engine.Close()) }()

	if err := engine.Start(ctx); err != nil {
		return err
	}

	info, ok := engine.Ready()
	if !ok {
		return errors.New("no worker became ready")
	}

	semantic, waitErr := waitSemantic(ctx, engine.Events(), info.Semantic)
	fmt.Fprintf(out, "ready worker=%s lexical=%s semantic=%s sandbox=%s\n", info.Worker, info.Lexical, semantic, info.Sandbox)

	hits, searchErr := searchNote(ctx, engine)
	fmt.Fprintf(out, "hits=%d\n", hits)

	var problems []error

	problems = append(problems, waitErr, searchErr)

	if semantic != usecase.SearchReady {
		problems = append(problems, fmt.Errorf("semantic search is %s", semantic))
	}

	if hits == 0 {
		problems = append(problems, errors.New("the search found nothing"))
	}

	if runtime.GOOS == "linux" && info.Sandbox != "full" {
		problems = append(problems, fmt.Errorf("sandbox is %s", info.Sandbox))
	}

	return errors.Join(problems...)
}

// waitSemantic waits while semantic search loads and returns its state then;
// the error tells that ctx ended first.
func waitSemantic(ctx context.Context, events <-chan usecase.SearchEvent, state usecase.SearchState) (usecase.SearchState, error) {
	for state == usecase.SearchLoading {
		select {
		case ev, ok := <-events:
			if !ok {
				return state, errors.New("events closed")
			}

			if ev.Status != nil {
				state = ev.Status.Semantic
			}
		case <-ctx.Done():
			return state, fmt.Errorf("semantic search still loading: %w", ctx.Err())
		}
	}

	return state, nil
}

// searchNote indexes one note and returns the number of its hits. It resets
// the index first, as every rebuild of Smartnote does: a worker answers
// searches only after its first reset.
func searchNote(ctx context.Context, engine *searchworker.Engine) (int, error) {
	if err := engine.Reset(ctx); err != nil {
		return 0, fmt.Errorf("reset: %w", err)
	}

	note := usecase.IndexedNote{ID: uuid.New(), Rev: 1, Name: "smoke", Info: smokeText}
	if err := engine.Upsert(ctx, []usecase.IndexedNote{note}); err != nil {
		return 0, fmt.Errorf("upsert: %w", err)
	}

	hits, err := engine.Search(ctx, smokeQuery, 10)
	if err != nil {
		return 0, fmt.Errorf("search: %w", err)
	}

	return len(hits), nil
}
