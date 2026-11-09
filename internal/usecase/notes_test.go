package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// newTestNotes returns Notes over store with the clock fixed at now.
func newTestNotes(t *testing.T, store NoteStore, now Clock) (*Notes, *ChangeTracker) {
	t.Helper()

	changes := &ChangeTracker{}

	n, err := NewNotes(store, now, seqIDs(), changes, nopIndex{})
	if err != nil {
		t.Fatal(err)
	}

	return n, changes
}

func TestNewNotesNil(t *testing.T) {
	store, clock, ids, changes, index := newFakeNoteStore(), fixedClock(t0), seqIDs(), &ChangeTracker{}, nopIndex{}

	tests := []struct {
		name string
		call func() (*Notes, error)
	}{
		{name: "store", call: func() (*Notes, error) { return NewNotes(nil, clock, ids, changes, index) }},
		{name: "clock", call: func() (*Notes, error) { return NewNotes(store, nil, ids, changes, index) }},
		{name: "ids", call: func() (*Notes, error) { return NewNotes(store, clock, nil, changes, index) }},
		{name: "changes", call: func() (*Notes, error) { return NewNotes(store, clock, ids, nil, index) }},
		{name: "index", call: func() (*Notes, error) { return NewNotes(store, clock, ids, changes, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.call(); err == nil {
				t.Error("want an error for a nil dependency")
			}
		})
	}
}

func TestNotesSaveTextCreatesVersion(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	got, err := notes.Save(context.Background(), id, "n", "a2")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got.Info != "a2" || !got.Updated.Equal(t1) {
		t.Errorf("Save = %+v, want Info a2 and Updated %v", got, t1)
	}

	if len(store.versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(store.versions))
	}

	v := store.versions[0]
	if v.NoteID != id || !v.Created.Equal(t0) || v.Info != "a" {
		t.Errorf("version = %+v, want {NoteID: %v, Created: %v, Info: a}", v, id, t0)
	}

	if v.ID == (domain.NoteVersion{}).ID {
		t.Error("version has no ID")
	}

	if stored := store.notes[id]; stored != got {
		t.Errorf("stored %+v, returned %+v", stored, got)
	}
}

func TestNotesSaveNameAndTextCreatesVersion(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	if _, err := notes.Save(context.Background(), id, "n2", "a2"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := store.notes[id]; got.Name != "n2" || got.Info != "a2" {
		t.Errorf("stored %+v, want name n2 and info a2", got)
	}

	if len(store.versions) != 1 {
		t.Errorf("versions = %d, want 1", len(store.versions))
	}
}

func TestNotesSaveNameOnlyNoVersion(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	got, err := notes.Save(context.Background(), id, "n2", "a")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if len(store.versions) != 0 {
		t.Errorf("versions = %d, want 0", len(store.versions))
	}

	if got.Name != "n2" || !got.Updated.Equal(t1) {
		t.Errorf("Save = %+v, want Name n2 and Updated %v", got, t1)
	}

	if stored := store.notes[id]; stored != got {
		t.Errorf("stored %+v, returned %+v", stored, got)
	}
}

func TestNotesSaveNoChange(t *testing.T) {
	id := testID(1)
	note := domain.Note{ID: id, Name: "n", Info: "a\nb", Updated: t0}
	store := newFakeNoteStore(note)
	notes, changes := newTestNotes(t, store, fixedClock(t1))
	gen := changes.Generation()

	got, err := notes.Save(context.Background(), id, "n", "a\r\nb")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.writes != 0 {
		t.Errorf("store writes = %d, want 0", store.writes)
	}

	if got != note {
		t.Errorf("Save = %+v, want %+v", got, note)
	}

	if changes.Generation() != gen || changes.Dirty() {
		t.Error("an unchanged save marked a change")
	}
}

func TestNotesSaveValidates(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	_, err := notes.Save(context.Background(), id, "", "a")
	assertField(t, err, "name")
	_, err = notes.Save(context.Background(), id, "n", strings.Repeat("x", domain.MaxInfoLen+1))
	assertField(t, err, "info")

	if store.writes != 0 {
		t.Errorf("store writes = %d, want 0", store.writes)
	}
}

func TestNotesCreateNormalizes(t *testing.T) {
	store := newFakeNoteStore()
	notes, _ := newTestNotes(t, store, fixedClock(t0))

	got, err := notes.Create(context.Background(), "name", "a\r\nb")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got.Info != "a\nb" {
		t.Errorf("Create returned Info %q, want %q", got.Info, "a\nb")
	}

	stored, ok := store.notes[got.ID]
	if !ok {
		t.Fatalf("note %v not stored", got.ID)
	}

	if stored.Info != "a\nb" || !stored.Updated.Equal(t0) || stored.Name != "name" {
		t.Errorf("stored %+v, want Info %q, Updated %v", stored, "a\nb", t0)
	}
}

func TestNotesCreateCountsNormalizedLength(t *testing.T) {
	store := newFakeNoteStore()
	notes, _ := newTestNotes(t, store, fixedClock(t0))

	if _, err := notes.Create(context.Background(), "n", strings.Repeat("\r\n", domain.MaxInfoLen)); err != nil {
		t.Fatalf("Create with %d CRLF: %v", domain.MaxInfoLen, err)
	}

	_, err := notes.Create(context.Background(), "n", "")
	assertField(t, err, "info")
}

func TestNotesMarkChanged(t *testing.T) {
	store := newFakeNoteStore()
	notes, changes := newTestNotes(t, store, fixedClock(t0))
	ctx := context.Background()

	gen := changes.Generation()

	n, err := notes.Create(ctx, "n", "a")
	if err != nil {
		t.Fatal(err)
	}

	if changes.Generation() == gen || !changes.Dirty() {
		t.Error("Create did not mark a change")
	}

	changes.Clear()
	gen = changes.Generation()

	if _, err := notes.Save(ctx, n.ID, "n", "b"); err != nil {
		t.Fatal(err)
	}

	if changes.Generation() == gen || !changes.Dirty() {
		t.Error("Save did not mark a change")
	}

	changes.Clear()
	gen = changes.Generation()

	if err := notes.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}

	if changes.Generation() == gen || !changes.Dirty() {
		t.Error("Delete did not mark a change")
	}
}

func TestNotesStoreErrorMarksNoChange(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	store.fail = errStore
	notes, changes := newTestNotes(t, store, fixedClock(t1))
	ctx := context.Background()

	_, errCreate := notes.Create(ctx, "new", "a")
	_, errText := notes.Save(ctx, id, "n", "b")
	_, errName := notes.Save(ctx, id, "n2", "a")

	errDelete := notes.Delete(ctx, id)
	for name, err := range map[string]error{"Create": errCreate, "Save text": errText, "Save name": errName, "Delete": errDelete} {
		if !errors.Is(err, errStore) {
			t.Errorf("%s = %v, want the store error", name, err)
		}
	}

	if changes.Generation() != 0 || changes.Dirty() {
		t.Errorf("failed writes marked a change: generation %d", changes.Generation())
	}
}

func TestNotesListSorted(t *testing.T) {
	store := newFakeNoteStore(
		domain.Note{ID: testID(1), Name: "b", Info: "x"},
		domain.Note{ID: testID(2), Name: "B", Info: "x"},
		domain.Note{ID: testID(3), Name: "а", Info: "x"}, // Cyrillic
		domain.Note{ID: testID(4), Name: "a", Info: "x"},
		domain.Note{ID: testID(5), Name: "a", Info: "y"},
	)
	notes, _ := newTestNotes(t, store, fixedClock(t0))

	got, err := notes.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var names []string
	for _, n := range got {
		names = append(names, n.Name)
	}

	if want := []string{"B", "a", "a", "b", "а"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("names = %q, want %q", names, want)
	}

	if got[1].ID != testID(4) || got[2].ID != testID(5) {
		t.Errorf("equal names are not ordered by ID: %v, %v", got[1].ID, got[2].ID)
	}
}

func TestNotesVersionsNewestFirst(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "c", Updated: t1})
	store.versions = []domain.NoteVersion{
		{ID: testID(10), NoteID: id, Created: t0, Info: "a"},
		{ID: testID(11), NoteID: id, Created: t0.Add(30 * 60e9), Info: "b"},
		{ID: testID(12), NoteID: testID(2), Created: t1, Info: "other"},
	}
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	got, err := notes.Versions(context.Background(), id)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}

	if len(got) != 2 || got[0].Info != "b" || got[1].Info != "a" {
		t.Errorf("Versions = %+v, want b then a", got)
	}
}

// newIndexedNotes returns Notes over store whose writes go to a recordingIndex.
func newIndexedNotes(t *testing.T, store NoteStore) (*Notes, *recordingIndex) {
	t.Helper()

	index := &recordingIndex{changes: &ChangeTracker{}}

	n, err := NewNotes(store, fixedClock(t1), seqIDs(), index.changes, index)
	if err != nil {
		t.Fatal(err)
	}

	return n, index
}

func TestNotesHooks(t *testing.T) {
	store := newFakeNoteStore()
	notes, index := newIndexedNotes(t, store)
	ctx := context.Background()

	created, err := notes.Create(ctx, "n", "a")
	if err != nil {
		t.Fatal(err)
	}

	saved, err := notes.Save(ctx, created.ID, "n2", "a")
	if err != nil {
		t.Fatal(err)
	}

	if err := notes.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if want := []domain.Note{created, saved}; !slices.Equal(index.saved, want) {
		t.Errorf("NoteSaved calls = %+v, want %+v", index.saved, want)
	}

	if want := []uuid.UUID{created.ID}; !slices.Equal(index.deleted, want) {
		t.Errorf("NoteDeleted calls = %v, want %v", index.deleted, want)
	}

	if index.unlocked != 0 {
		t.Errorf("%d index calls ran outside the edit lock", index.unlocked)
	}
}

func TestNotesHookOnlyOnChange(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	notes, index := newIndexedNotes(t, store)

	if _, err := notes.Save(context.Background(), id, "n", "a"); err != nil {
		t.Fatal(err)
	}

	if len(index.saved) != 0 {
		t.Errorf("an unchanged Save called NoteSaved: %+v", index.saved)
	}
}

func TestNotesHookNotOnStoreError(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "n", Info: "a", Updated: t0})
	store.fail = errStore
	notes, index := newIndexedNotes(t, store)
	ctx := context.Background()

	_, _ = notes.Create(ctx, "n", "a")   // the store error is checked in TestNotesStoreErrorMarksNoChange
	_, _ = notes.Save(ctx, id, "n", "b") // as above
	_ = notes.Delete(ctx, id)            // as above

	if len(index.saved) != 0 || len(index.deleted) != 0 {
		t.Errorf("failed writes reached the index: saved %+v, deleted %v", index.saved, index.deleted)
	}
}

func TestNotesCreateDuplicateName(t *testing.T) {
	store := newFakeNoteStore(domain.Note{ID: testID(1), Name: "Заметка", Info: "a", Updated: t0})
	notes, changes := newTestNotes(t, store, fixedClock(t1))

	_, err := notes.Create(context.Background(), "заМЕТКА", "b")
	assertField(t, err, "name")

	if store.writes != 0 || len(store.notes) != 1 {
		t.Errorf("store writes = %d, notes = %d, want 0 and 1", store.writes, len(store.notes))
	}

	if changes.Dirty() {
		t.Error("a refused create marked a change")
	}
}

func TestNotesSaveRenameToDuplicate(t *testing.T) {
	a, b := testID(1), testID(2)
	store := newFakeNoteStore(
		domain.Note{ID: a, Name: "a", Info: "x", Updated: t0},
		domain.Note{ID: b, Name: "b", Info: "y", Updated: t0},
	)
	notes, changes := newTestNotes(t, store, fixedClock(t1))

	_, err := notes.Save(context.Background(), b, "A", "y2")
	assertField(t, err, "name")

	if got := store.notes[b]; got.Name != "b" || got.Info != "y" || store.writes != 0 {
		t.Errorf("note b = %+v after %d writes, want it unchanged", got, store.writes)
	}

	if changes.Dirty() {
		t.Error("a refused save marked a change")
	}
}

func TestNotesSaveRenameOwnCase(t *testing.T) {
	id := testID(1)
	store := newFakeNoteStore(domain.Note{ID: id, Name: "note", Info: "x", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	got, err := notes.Save(context.Background(), id, "NOTE", "x")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got.Name != "NOTE" || store.notes[id].Name != "NOTE" {
		t.Errorf("Save = %+v, stored %+v, want the name NOTE", got, store.notes[id])
	}
}

// Notes that share a name from before the check stay editable while the name is kept.
func TestNotesSaveKeepsExistingDuplicate(t *testing.T) {
	a, b := testID(1), testID(2)
	store := newFakeNoteStore(
		domain.Note{ID: a, Name: "x", Info: "1", Updated: t0},
		domain.Note{ID: b, Name: "X", Info: "2", Updated: t0},
	)
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	if _, err := notes.Save(context.Background(), a, "x", "1 changed"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if store.notes[a].Info != "1 changed" {
		t.Errorf("stored %+v, want the new text", store.notes[a])
	}
}

func TestNotesCreateTrimsName(t *testing.T) {
	store := newFakeNoteStore()
	notes, _ := newTestNotes(t, store, fixedClock(t0))

	got, err := notes.Create(context.Background(), "  name\t", "a")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got.Name != "name" || store.notes[got.ID].Name != "name" {
		t.Errorf("Create = %+v, stored %+v, want the name without surrounding spaces", got, store.notes[got.ID])
	}
}

func TestNotesCreateDuplicateNameWithSpaces(t *testing.T) {
	store := newFakeNoteStore(domain.Note{ID: testID(1), Name: "note", Info: "a", Updated: t0})
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	_, err := notes.Create(context.Background(), " Note ", "b")
	assertField(t, err, "name")
}

// A stored name with surrounding spaces is trimmed on save, which is no new
// name: a note that already shares it stays editable.
func TestNotesSaveTrimsExistingDuplicate(t *testing.T) {
	a, b := testID(1), testID(2)
	store := newFakeNoteStore(
		domain.Note{ID: a, Name: "x ", Info: "1", Updated: t0},
		domain.Note{ID: b, Name: "X", Info: "2", Updated: t0},
	)
	notes, _ := newTestNotes(t, store, fixedClock(t1))

	if _, err := notes.Save(context.Background(), a, "x ", "1 changed"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := store.notes[a]; got.Name != "x" || got.Info != "1 changed" {
		t.Errorf("stored %+v, want the trimmed name and the new text", got)
	}
}
