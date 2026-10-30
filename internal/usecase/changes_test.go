package usecase

import (
	"sync"
	"testing"
)

func TestChangeTrackerClearIf(t *testing.T) {
	var c ChangeTracker

	gen := c.Generation()
	c.MarkChanged()

	if c.ClearIf(gen) {
		t.Error("ClearIf with a stale generation = true")
	}

	if !c.Dirty() {
		t.Error("Dirty() = false after a stale ClearIf")
	}

	if !c.ClearIf(c.Generation()) {
		t.Error("ClearIf with the current generation = false")
	}

	if c.Dirty() {
		t.Error("Dirty() = true after ClearIf")
	}
}

func TestChangeTrackerMarkDivergedKeepsGeneration(t *testing.T) {
	var c ChangeTracker

	gen := c.Generation()
	c.MarkDiverged()

	if !c.Dirty() {
		t.Error("Dirty() = false after MarkDiverged")
	}

	if c.Generation() != gen {
		t.Errorf("Generation() = %d after MarkDiverged, want %d", c.Generation(), gen)
	}

	c.Clear()

	if c.Dirty() {
		t.Error("Dirty() = true after Clear")
	}
}

func TestChangeTrackerConcurrent(t *testing.T) {
	var (
		c  ChangeTracker
		wg sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			for range 100 {
				c.MarkChanged()
				_ = c.Dirty()
				c.ClearIf(c.Generation())
			}
		})
	}

	wg.Wait()

	if got := c.Generation(); got != 800 {
		t.Errorf("Generation() = %d, want 800", got)
	}
}
