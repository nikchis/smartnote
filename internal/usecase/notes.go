package usecase

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// NoteStore keeps the decrypted notes and their previous texts.
type NoteStore interface {
	ListNotes(ctx context.Context) ([]domain.Note, error)
	GetNote(ctx context.Context, id uuid.UUID) (domain.Note, error)
	InsertNote(ctx context.Context, n domain.Note) error
	// UpdateNoteWithVersion writes n and keeps prev in one transaction.
	UpdateNoteWithVersion(ctx context.Context, n domain.Note, prev domain.NoteVersion) error
	UpdateNoteName(ctx context.Context, id uuid.UUID, name string, updated time.Time) error
	// DeleteNote removes the note with its versions.
	DeleteNote(ctx context.Context, id uuid.UUID) error
	ListVersions(ctx context.Context, noteID uuid.UUID) ([]domain.NoteVersion, error)
}

// Notes lists, creates, saves and deletes notes. Every write runs under the
// ChangeTracker edit lock and, when it succeeds, marks a local change.
type Notes struct {
	store   NoteStore
	clock   Clock
	ids     IDGenerator
	changes *ChangeTracker
}

// NewNotes returns Notes over s.
func NewNotes(s NoteStore, clock Clock, ids IDGenerator, changes *ChangeTracker) (*Notes, error) {
	switch {
	case s == nil:
		return nil, nilDependency("NewNotes", "store")
	case clock == nil:
		return nil, nilDependency("NewNotes", "clock")
	case ids == nil:
		return nil, nilDependency("NewNotes", "ids")
	case changes == nil:
		return nil, nilDependency("NewNotes", "changes")
	}

	return &Notes{store: s, clock: clock, ids: ids, changes: changes}, nil
}

// List returns all notes ordered by name byte by byte, as ORDER BY in v1, and
// by ID among equal names.
func (n *Notes) List(ctx context.Context) ([]domain.Note, error) {
	notes, err := n.store.ListNotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}

	slices.SortFunc(notes, func(a, b domain.Note) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), bytes.Compare(a.ID[:], b.ID[:]))
	})

	return notes, nil
}

// Create normalizes and validates a new note and stores it.
func (n *Notes) Create(ctx context.Context, name, info string) (domain.Note, error) {
	note := domain.Note{ID: n.ids(), Name: name, Info: info, Updated: n.clock()}.Normalized()
	if err := note.Validate(); err != nil {
		return domain.Note{}, err
	}

	err := n.changes.Edit(func() (bool, error) {
		return true, n.store.InsertNote(ctx, note)
	})
	if err != nil {
		return domain.Note{}, fmt.Errorf("create note: %w", err)
	}

	return note, nil
}

// Save writes a new name and text of note id. A changed text keeps the
// previous one as a version created at the previous Updated; a changed name
// alone keeps no version, as in v1. Without changes nothing is written.
func (n *Notes) Save(ctx context.Context, id uuid.UUID, name, info string) (domain.Note, error) {
	note := domain.Note{ID: id, Name: name, Info: info}.Normalized()
	if err := note.Validate(); err != nil {
		return domain.Note{}, err
	}

	var saved domain.Note

	err := n.changes.Edit(func() (bool, error) {
		cur, err := n.store.GetNote(ctx, id)
		if err != nil {
			return false, err
		}

		if note.Name == cur.Name && note.Info == cur.Info {
			saved = cur
			return false, nil
		}

		note.Updated = n.clock()
		if note.Info != cur.Info {
			prev := domain.NoteVersion{ID: n.ids(), NoteID: id, Created: cur.Updated, Info: cur.Info}
			err = n.store.UpdateNoteWithVersion(ctx, note, prev)
		} else {
			err = n.store.UpdateNoteName(ctx, id, note.Name, note.Updated)
		}

		saved = note

		return true, err
	})
	if err != nil {
		return domain.Note{}, fmt.Errorf("save note: %w", err)
	}

	return saved, nil
}

// Delete removes note id with its versions.
func (n *Notes) Delete(ctx context.Context, id uuid.UUID) error {
	err := n.changes.Edit(func() (bool, error) {
		return true, n.store.DeleteNote(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}

	return nil
}

// Versions returns the previous texts of note id, newest first.
func (n *Notes) Versions(ctx context.Context, id uuid.UUID) ([]domain.NoteVersion, error) {
	versions, err := n.store.ListVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}

	slices.SortFunc(versions, func(a, b domain.NoteVersion) int {
		return cmp.Or(b.Created.Compare(a.Created), bytes.Compare(a.ID[:], b.ID[:]))
	})

	return versions, nil
}
