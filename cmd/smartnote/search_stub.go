package main

import (
	"context"

	"github.com/nikchis/smartnote/internal/usecase"
)

// unavailableEngine is the search engine when there is no worker: it reports
// both searches unavailable, fails every search with
// usecase.ErrSearchUnavailable and accepts every index command.
type unavailableEngine struct {
	events chan usecase.SearchEvent
}

func newUnavailableEngine() *unavailableEngine {
	return &unavailableEngine{events: make(chan usecase.SearchEvent, 1)}
}

// Start reports the unavailable status; it never waits for a reader.
func (e *unavailableEngine) Start(context.Context) error {
	status := usecase.UnavailableStatus()

	select {
	case e.events <- usecase.SearchEvent{Status: &status}:
	default: // the same status is still unread
	}

	return nil
}

func (e *unavailableEngine) Reset(context.Context) error { return nil }

func (e *unavailableEngine) Upsert(context.Context, []usecase.IndexedNote) error { return nil }

func (e *unavailableEngine) Remove(context.Context, []usecase.IndexedRemoval) error { return nil }

func (e *unavailableEngine) Search(context.Context, string, int) ([]usecase.SearchHit, error) {
	return nil, usecase.ErrSearchUnavailable
}

func (e *unavailableEngine) Events() <-chan usecase.SearchEvent { return e.events }

func (e *unavailableEngine) Close() error { return nil }
