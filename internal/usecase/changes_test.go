package usecase

import (
	"sync"
	"testing"
	"time"
)

// TestChangeTrackerReleasesEditLockOnPanic: Wails recovers a panic in a
// service call, so the panicking callback must not leave the edit lock held,
// or every later edit and Restore would hang.
func TestChangeTrackerReleasesEditLockOnPanic(t *testing.T) {
	tests := []struct {
		name  string
		panic func(*ChangeTracker)
	}{
		{name: "Edit", panic: func(c *ChangeTracker) {
			_ = c.Edit(func() (bool, error) { panic("write failed") })
		}},
		{name: "ApplyIfUnchanged", panic: func(c *ChangeTracker) {
			_ = c.ApplyIfUnchanged(c.Generation(), func() error { panic("apply failed") })
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c ChangeTracker

			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("callback panic did not reach the caller")
					}
				}()

				tc.panic(&c)
			}()

			done := make(chan error, 1)
			go func() { done <- c.Edit(func() (bool, error) { return true, nil }) }()

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Edit after panic = %v, want nil", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Edit after a panic did not finish: the edit lock is still held")
			}
		})
	}
}

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
