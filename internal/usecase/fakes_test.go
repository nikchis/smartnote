package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

var errStore = errors.New("store failure")

// fakeNoteStore keeps notes in a map and versions in a slice. fail, when set,
// is returned by every write.
type fakeNoteStore struct {
	mu       sync.Mutex
	notes    map[uuid.UUID]domain.Note
	versions []domain.NoteVersion
	writes   int
	fail     error
}

func newFakeNoteStore(notes ...domain.Note) *fakeNoteStore {
	s := &fakeNoteStore{notes: map[uuid.UUID]domain.Note{}}
	for _, n := range notes {
		s.notes[n.ID] = n
	}
	return s
}

func (s *fakeNoteStore) ListNotes(context.Context) ([]domain.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n)
	}
	return out, nil
}

func (s *fakeNoteStore) GetNote(_ context.Context, id uuid.UUID) (domain.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	if !ok {
		return domain.Note{}, fmt.Errorf("note %s: not found", id)
	}
	return n, nil
}

func (s *fakeNoteStore) write() error {
	s.writes++
	return s.fail
}

func (s *fakeNoteStore) InsertNote(_ context.Context, n domain.Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(); err != nil {
		return err
	}
	s.notes[n.ID] = n
	return nil
}

func (s *fakeNoteStore) UpdateNoteWithVersion(_ context.Context, n domain.Note, prev domain.NoteVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(); err != nil {
		return err
	}
	s.notes[n.ID] = n
	s.versions = append(s.versions, prev)
	return nil
}

func (s *fakeNoteStore) UpdateNoteName(_ context.Context, id uuid.UUID, name string, updated time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(); err != nil {
		return err
	}
	n := s.notes[id]
	n.Name, n.Updated = name, updated
	s.notes[id] = n
	return nil
}

func (s *fakeNoteStore) DeleteNote(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(); err != nil {
		return err
	}
	delete(s.notes, id)
	s.versions = slices.DeleteFunc(s.versions, func(v domain.NoteVersion) bool { return v.NoteID == id })
	return nil
}

func (s *fakeNoteStore) ListVersions(_ context.Context, noteID uuid.UUID) ([]domain.NoteVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.NoteVersion
	for _, v := range s.versions {
		if v.NoteID == noteID {
			out = append(out, v)
		}
	}
	return out, nil
}

// fakeVaultStore records the passwords it receives.
type fakeVaultStore struct {
	exists          bool
	created         []string
	unlocked        []string
	changedPassword []string
}

func (s *fakeVaultStore) Exists(context.Context) (bool, error) { return s.exists, nil }

func (s *fakeVaultStore) Create(_ context.Context, password string) error {
	s.created = append(s.created, password)
	return nil
}

func (s *fakeVaultStore) Unlock(_ context.Context, password string) error {
	s.unlocked = append(s.unlocked, password)
	return nil
}

func (s *fakeVaultStore) ChangePassword(_ context.Context, newPassword string) error {
	s.changedPassword = append(s.changedPassword, newPassword)
	return nil
}

// fixedClock returns t on every call.
func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

// seqIDs returns UUIDs 00000000-0000-0000-0000-000000000001, …02 and so on.
func seqIDs() IDGenerator {
	var mu sync.Mutex
	var n byte
	return func() uuid.UUID {
		mu.Lock()
		defer mu.Unlock()
		n++
		var id uuid.UUID
		id[15] = n
		return id
	}
}

// testID returns a UUID ending in b, outside the range seqIDs reaches in a test.
func testID(b byte) uuid.UUID {
	var id uuid.UUID
	id[0], id[15] = 0xff, b
	return id
}

var (
	t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)
