package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

func testUUID(b byte) uuid.UUID {
	var id uuid.UUID

	id[0], id[15] = 0x5a, b

	return id
}

func ms(v int64) time.Time { return time.UnixMilli(v).UTC() }

func TestNotesCRUDAndCascade(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	n := domain.Note{ID: testUUID(1), Name: "Привет 😀", Info: "line1\nline2", Updated: ms(1000)}
	if err := s.InsertNote(ctx, n); err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	got, err := s.GetNote(ctx, n.ID)
	if err != nil || got != n {
		t.Fatalf("GetNote = %+v, %v; want %+v", got, err, n)
	}

	n2 := domain.Note{ID: n.ID, Name: "renamed", Info: "new text", Updated: ms(2000)}

	prev := domain.NoteVersion{ID: testUUID(10), NoteID: n.ID, Created: ms(1000), Info: n.Info}
	if err := s.UpdateNoteWithVersion(ctx, n2, prev); err != nil {
		t.Fatalf("UpdateNoteWithVersion: %v", err)
	}

	if err := s.UpdateNoteName(ctx, n.ID, "renamed again", ms(3000)); err != nil {
		t.Fatalf("UpdateNoteName: %v", err)
	}

	got, err = s.GetNote(ctx, n.ID)

	want := domain.Note{ID: n.ID, Name: "renamed again", Info: "new text", Updated: ms(3000)}
	if err != nil || got != want {
		t.Fatalf("GetNote = %+v, %v; want %+v", got, err, want)
	}

	versions, err := s.ListVersions(ctx, n.ID)
	if err != nil || len(versions) != 1 || versions[0] != prev {
		t.Fatalf("ListVersions = %+v, %v; want [%+v]", versions, err, prev)
	}

	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	if notes, err := s.ListNotes(ctx); err != nil || len(notes) != 0 {
		t.Errorf("ListNotes after delete = %v, %v", notes, err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM note_version").Scan(&count); err != nil || count != 0 {
		t.Errorf("versions after delete = %d, %v; want 0", count, err)
	}
}

func TestNotesMissingID(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	id := testUUID(9)
	if _, err := s.GetNote(ctx, id); err == nil {
		t.Error("GetNote of a missing note: nil error")
	}

	if err := s.UpdateNoteName(ctx, id, "n", ms(1)); err == nil {
		t.Error("UpdateNoteName of a missing note: nil error")
	}

	n := domain.Note{ID: id, Name: "n", Info: "i", Updated: ms(1)}
	if err := s.UpdateNoteWithVersion(ctx, n, domain.NoteVersion{ID: testUUID(10), NoteID: id, Info: "x"}); err == nil {
		t.Error("UpdateNoteWithVersion of a missing note: nil error")
	}

	if err := s.DeleteNote(ctx, id); err == nil {
		t.Error("DeleteNote of a missing note: nil error")
	}

	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM note_version").Scan(&count); err != nil || count != 0 {
		t.Errorf("versions = %d, %v; the failed update kept a version", count, err)
	}
}

func TestVersionBoundToNote(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)
	a := domain.Note{ID: testUUID(1), Name: "a", Info: "a2", Updated: ms(2)}

	b := domain.Note{ID: testUUID(2), Name: "b", Info: "b", Updated: ms(2)}
	for _, n := range []domain.Note{a, b} {
		if err := s.InsertNote(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.UpdateNoteWithVersion(ctx, a, domain.NoteVersion{ID: testUUID(10), NoteID: a.ID, Created: ms(1), Info: "a1"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE note_version SET note_uuid = ?", b.ID.String()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ListVersions(ctx, b.ID); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("ListVersions of the other note = %v, want ErrCorruptStorage", err)
	}
}

func TestFieldMovedToOtherNote(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)
	a := domain.Note{ID: testUUID(1), Name: "a", Info: "secret a", Updated: ms(1)}

	b := domain.Note{ID: testUUID(2), Name: "b", Info: "b", Updated: ms(1)}
	for _, n := range []domain.Note{a, b} {
		if err := s.InsertNote(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE note SET info = (SELECT info FROM note WHERE note_uuid = ?) WHERE note_uuid = ?",
		a.ID.String(), b.ID.String()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ListNotes(ctx); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("ListNotes = %v, want ErrCorruptStorage", err)
	}

	if _, err := s.GetNote(ctx, b.ID); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("GetNote = %v, want ErrCorruptStorage", err)
	}
}

// TestInTxFailedCommit: a failed Commit already ends the transaction, so the
// error must be the Commit's alone, without a "transaction has already been
// committed or rolled back" from the rollback that follows it.
func TestInTxFailedCommit(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(1)

	for _, q := range []string{
		"PRAGMA foreign_keys = ON",
		"CREATE TABLE parent (id INTEGER PRIMARY KEY)",
		// A deferred key is checked at COMMIT, which then fails.
		"CREATE TABLE child (parent_id INTEGER REFERENCES parent (id) DEFERRABLE INITIALLY DEFERRED)",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	err = inTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO child (parent_id) VALUES (1)")
		return err
	})
	if err == nil {
		t.Fatal("inTx = nil, want the failed Commit's error")
	}

	if errors.Is(err, sql.ErrTxDone) || strings.Contains(err.Error(), "rollback") {
		t.Errorf("inTx error = %q, want the Commit error without a rollback error", err)
	}

	if !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Errorf("inTx error = %q, want the FOREIGN KEY error of Commit", err)
	}
}
