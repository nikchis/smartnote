// Package remotetest holds the behavioural contract every usecase.RemoteSession
// implementation must satisfy. It is used by tests only.
package remotetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// Opener opens a session over one storage.
type Opener func(phrase string, mode usecase.OpenMode) (usecase.RemoteSession, error)

const (
	phrase      = "sixteen chars ok"
	otherPhrase = "another phrase ok!"
)

// RunSessionContract runs the contract. fresh returns an Opener over a new
// empty storage; one subtest may open several sessions over it.
func RunSessionContract(t *testing.T, fresh func(t *testing.T) Opener) {
	t.Helper()

	cases := []struct {
		name string
		run  func(t *testing.T, open Opener)
	}{
		{"create then check", createThenCheck},
		{"check empty", checkEmpty},
		{"wrong phrase", wrongPhrase},
		{"backup snapshot round trip", roundTrip},
		{"unicode round trip", unicodeRoundTrip},
		{"stale revision", staleRevision},
		{"other server id", otherServerID},
		{"empty set backup", emptySetBackup},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, fresh(t)) })
	}
}

func mustOpen(t *testing.T, open Opener, p string, mode usecase.OpenMode) usecase.RemoteSession {
	t.Helper()

	s, err := open(p, mode)
	if err != nil {
		t.Fatalf("open (mode %d): %v", mode, err)
	}

	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return s
}

func snapshot(t *testing.T, s usecase.RemoteSession) usecase.RemoteSnapshot {
	t.Helper()

	snap, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	return snap
}

func note(name, info string) domain.Note {
	return domain.Note{ID: uuid.New(), Name: name, Info: info, Updated: time.UnixMilli(1_700_000_000_123).UTC()}
}

func sameNotes(t *testing.T, got, want []domain.Note) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%d notes, want %d", len(got), len(want))
	}

	byID := map[uuid.UUID]domain.Note{}
	for _, n := range got {
		byID[n.ID] = n
	}

	for _, w := range want {
		g, ok := byID[w.ID]
		if !ok || g.Name != w.Name || g.Info != w.Info || !g.Updated.Equal(w.Updated) {
			t.Errorf("note %v = %+v (found %v), want %+v", w.ID, g, ok, w)
		}
	}
}

func createThenCheck(t *testing.T, open Opener) {
	created := mustOpen(t, open, phrase, usecase.OpenCreate)
	if !created.Created() || created.Empty() {
		t.Fatalf("Created = %v, Empty = %v; want true, false", created.Created(), created.Empty())
	}

	first := snapshot(t, created)

	checked := mustOpen(t, open, phrase, usecase.OpenCheck)
	if checked.Empty() || checked.Created() {
		t.Fatalf("check: Empty = %v, Created = %v; want false, false", checked.Empty(), checked.Created())
	}

	if again := snapshot(t, checked); again.ServerID != first.ServerID {
		t.Errorf("server id %v, want %v", again.ServerID, first.ServerID)
	}

	again := mustOpen(t, open, phrase, usecase.OpenCreate)
	if again.Created() {
		t.Error("second OpenCreate reports Created")
	}
}

func checkEmpty(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCheck)
	if !s.Empty() || s.Created() {
		t.Fatalf("Empty = %v, Created = %v; want true, false", s.Empty(), s.Created())
	}

	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Error("Snapshot of an empty storage succeeded")
	}

	if _, err := s.Backup(context.Background(), nil, uuid.New(), 0); err == nil {
		t.Error("Backup of an empty storage succeeded")
	}
}

func wrongPhrase(t *testing.T, open Opener) {
	mustOpen(t, open, phrase, usecase.OpenCreate)

	for _, mode := range []usecase.OpenMode{usecase.OpenCheck, usecase.OpenCreate} {
		s, err := open(otherPhrase, mode)
		if !errors.Is(err, domain.ErrWrongSyncPhrase) {
			t.Errorf("mode %d: err = %v, want ErrWrongSyncPhrase", mode, err)
		}

		if err == nil {
			_ = s.Close()
		}
	}
}

func roundTrip(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCreate)
	snap := snapshot(t, s)
	notes := []domain.Note{note("one", "text 1"), note("two", "text 2")}

	rev, err := s.Backup(context.Background(), notes, snap.ServerID, snap.Revision)
	if err != nil || rev != snap.Revision+1 {
		t.Fatalf("Backup = %d, %v; want %d, nil", rev, err, snap.Revision+1)
	}

	got := snapshot(t, mustOpen(t, open, phrase, usecase.OpenCheck))
	if got.Revision != rev || got.ServerID != snap.ServerID {
		t.Errorf("snapshot revision %d server %v; want %d, %v", got.Revision, got.ServerID, rev, snap.ServerID)
	}

	sameNotes(t, got.Notes, notes)

	// The same session sees its own write.
	sameNotes(t, snapshot(t, s).Notes, notes)
}

func unicodeRoundTrip(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCreate)
	snap := snapshot(t, s)
	notes := []domain.Note{note("Заметка ✓ 😀", "строка\tс табом\nвторая строка 😀")}

	if _, err := s.Backup(context.Background(), notes, snap.ServerID, snap.Revision); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	sameNotes(t, snapshot(t, mustOpen(t, open, phrase, usecase.OpenCheck)).Notes, notes)
}

func staleRevision(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCreate)
	snap := snapshot(t, s)
	kept := []domain.Note{note("kept", "kept")}

	rev, err := s.Backup(context.Background(), kept, snap.ServerID, snap.Revision)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	late := mustOpen(t, open, phrase, usecase.OpenCheck)

	if _, err := late.Backup(context.Background(), []domain.Note{note("late", "late")}, snap.ServerID, snap.Revision); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("stale Backup err = %v, want ErrRemoteChanged", err)
	}

	got := snapshot(t, mustOpen(t, open, phrase, usecase.OpenCheck))
	if got.Revision != rev {
		t.Errorf("revision %d, want %d", got.Revision, rev)
	}

	sameNotes(t, got.Notes, kept)
}

func otherServerID(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCreate)
	snap := snapshot(t, s)

	if _, err := s.Backup(context.Background(), []domain.Note{note("x", "x")}, uuid.New(), snap.Revision); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("Backup err = %v, want ErrRemoteChanged", err)
	}

	got := snapshot(t, mustOpen(t, open, phrase, usecase.OpenCheck))
	if got.Revision != snap.Revision || len(got.Notes) != 0 {
		t.Errorf("storage changed: revision %d, %d notes", got.Revision, len(got.Notes))
	}
}

func emptySetBackup(t *testing.T, open Opener) {
	s := mustOpen(t, open, phrase, usecase.OpenCreate)
	snap := snapshot(t, s)

	rev, err := s.Backup(context.Background(), []domain.Note{note("a", "a")}, snap.ServerID, snap.Revision)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	rev2, err := s.Backup(context.Background(), nil, snap.ServerID, rev)
	if err != nil || rev2 != rev+1 {
		t.Fatalf("empty Backup = %d, %v; want %d, nil", rev2, err, rev+1)
	}

	got := snapshot(t, mustOpen(t, open, phrase, usecase.OpenCheck))
	if got.Revision != rev2 || len(got.Notes) != 0 {
		t.Errorf("snapshot revision %d, %d notes; want %d, 0", got.Revision, len(got.Notes), rev2)
	}
}
