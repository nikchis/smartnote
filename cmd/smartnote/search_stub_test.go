package main

import (
	"context"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/usecase"
)

func TestUnavailableEngine(t *testing.T) {
	e := newUnavailableEngine()
	ctx := context.Background()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case ev := <-e.Events():
		want := usecase.SearchStatus{Lexical: usecase.SearchUnavailable, Semantic: usecase.SearchUnavailable}
		if ev.Status == nil || *ev.Status != want {
			t.Errorf("event = %+v, want status %+v", ev, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no status after Start")
	}

	// A second Start does not block on the unread status.
	if err := e.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	hits, err := e.Search(ctx, "x", 10)
	if err != nil || len(hits) != 0 {
		t.Errorf("Search = %v, %v, want an empty result", hits, err)
	}

	for name, err := range map[string]error{
		"Reset":  e.Reset(ctx),
		"Upsert": e.Upsert(ctx, []usecase.IndexedNote{{Name: "n"}}),
		"Remove": e.Remove(ctx, []usecase.IndexedRemoval{{Rev: 1}}),
		"Close":  e.Close(),
	} {
		if err != nil {
			t.Errorf("%s = %v, want nil", name, err)
		}
	}
}
