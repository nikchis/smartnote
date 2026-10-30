package usecase

import "sync"

// ChangeTracker is the in-memory "unsaved changes" flag with a generation
// counter (spec §6.4). The counter grows on every local edit, so a Backup can
// clear the flag only if nothing was edited since its snapshot. The zero value
// is ready to use and it is safe for concurrent use.
type ChangeTracker struct {
	mu    sync.Mutex
	dirty bool
	gen   uint64
}

// MarkChanged records a local edit: the generation grows and the flag is set.
func (t *ChangeTracker) MarkChanged() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gen++
	t.dirty = true
}

// MarkDiverged sets the flag after a connect check found differences; the
// generation does not change, since nothing was edited.
func (t *ChangeTracker) MarkDiverged() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dirty = true
}

// Generation returns the number of local edits so far.
func (t *ChangeTracker) Generation() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gen
}

// ClearIf clears the flag and returns true if the generation is still gen.
func (t *ChangeTracker) ClearIf(gen uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gen != gen {
		return false
	}
	t.dirty = false
	return true
}

// Clear clears the flag unconditionally.
func (t *ChangeTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dirty = false
}

// Dirty reports whether there are unsaved changes.
func (t *ChangeTracker) Dirty() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dirty
}
