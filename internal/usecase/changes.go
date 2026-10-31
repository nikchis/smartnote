package usecase

import (
	"sync"

	"github.com/nikchis/smartnote/internal/domain"
)

// ChangeTracker is the in-memory "unsaved changes" flag with a generation
// counter (spec §6.4). The counter grows on every local edit, so a Backup can
// clear the flag only if nothing was edited since its snapshot. Local edits
// and Restore run under one edit lock, so a Restore never overwrites an edit
// it has not seen. The zero value is ready to use and it is safe for
// concurrent use.
type ChangeTracker struct {
	edit sync.Mutex // held by Edit and ApplyIfUnchanged; taken before mu

	mu       sync.Mutex
	dirty    bool
	gen      uint64
	onChange func()
}

// setOnChange makes f run, outside the tracker's locks, after the flag or the
// generation changes. NewSync connects it to the state it publishes.
func (t *ChangeTracker) setOnChange(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.onChange = f
}

// Edit runs a local write with no other write or Restore in progress. When
// write reports a change without an error, the generation grows and the flag
// is set before the lock is released.
func (t *ChangeTracker) Edit(write func() (changed bool, err error)) error {
	changed, err := t.editLocked(write)
	if err == nil && changed {
		t.notify()
	}

	return err
}

// editLocked runs write under the edit lock and records a change before the
// lock is released. The deferred unlock also covers a panic in write.
func (t *ChangeTracker) editLocked(write func() (bool, error)) (bool, error) {
	t.edit.Lock()
	defer t.edit.Unlock()

	changed, err := write()
	if err == nil && changed {
		t.set(func() { t.gen++; t.dirty = true })
	}

	return changed, err
}

// ApplyIfUnchanged runs apply with no local write in progress, provided the
// generation is still gen, and then clears the flag. Otherwise it returns
// domain.ErrLocalChanged without running apply.
func (t *ChangeTracker) ApplyIfUnchanged(gen uint64, apply func() error) error {
	if err := t.applyLocked(gen, apply); err != nil {
		return err
	}

	t.notify()

	return nil
}

// applyLocked runs apply under the edit lock if the generation is still gen
// and clears the flag before the lock is released. The deferred unlock also
// covers a panic in apply.
func (t *ChangeTracker) applyLocked(gen uint64, apply func() error) error {
	t.edit.Lock()
	defer t.edit.Unlock()

	if t.Generation() != gen {
		return domain.ErrLocalChanged
	}

	if err := apply(); err != nil {
		return err
	}

	t.set(func() { t.dirty = false })

	return nil
}

// MarkChanged records a local edit made without Edit: the generation grows
// and the flag is set.
func (t *ChangeTracker) MarkChanged() {
	t.set(func() { t.gen++; t.dirty = true })
	t.notify()
}

// MarkDiverged sets the flag after a connect check found differences; the
// generation does not change, since nothing was edited.
func (t *ChangeTracker) MarkDiverged() {
	t.set(func() { t.dirty = true })
	t.notify()
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

	ok := t.gen == gen
	if ok {
		t.dirty = false
	}
	t.mu.Unlock()

	if ok {
		t.notify()
	}

	return ok
}

// Clear clears the flag unconditionally.
func (t *ChangeTracker) Clear() {
	t.set(func() { t.dirty = false })
	t.notify()
}

// Dirty reports whether there are unsaved changes.
func (t *ChangeTracker) Dirty() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.dirty
}

func (t *ChangeTracker) set(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	f()
}

func (t *ChangeTracker) notify() {
	t.mu.Lock()
	f := t.onChange
	t.mu.Unlock()

	if f != nil {
		f()
	}
}
