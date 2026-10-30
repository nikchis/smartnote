package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// errNoteNotFound is returned for a note ID that is not stored.
var errNoteNotFound = errors.New("note not found")

// ListNotes returns all notes, decrypted, in no particular order.
func (s *Store) ListNotes(ctx context.Context) ([]domain.Note, error) {
	var notes []domain.Note

	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		var err error

		notes, err = listNotes(ctx, db, dek)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}

	return notes, nil
}

// querier is *sql.DB or *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func listNotes(ctx context.Context, q querier, dek []byte) (_ []domain.Note, err error) {
	rows, err := q.QueryContext(ctx, "SELECT note_id, updated, name, info FROM notes")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	var notes []domain.Note

	for rows.Next() {
		n, err := scanNote(rows, dek)
		if err != nil {
			return nil, err
		}

		notes = append(notes, n)
	}

	return notes, rows.Err()
}

// GetNote returns note id, decrypted.
func (s *Store) GetNote(ctx context.Context, id uuid.UUID) (domain.Note, error) {
	var n domain.Note

	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		row := db.QueryRowContext(ctx, "SELECT note_id, updated, name, info FROM notes WHERE note_id = ?", id.String())

		var err error

		n, err = scanNote(row, dek)
		if errors.Is(err, sql.ErrNoRows) {
			return errNoteNotFound
		}

		return err
	})
	if err != nil {
		return domain.Note{}, fmt.Errorf("get note %s: %w", id, err)
	}

	return n, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanNote(sc scanner, dek []byte) (domain.Note, error) {
	var (
		id         string
		updated    int64
		name, info []byte
	)
	if err := sc.Scan(&id, &updated, &name, &info); err != nil {
		return domain.Note{}, err
	}

	noteID, err := uuid.Parse(id)
	if err != nil {
		return domain.Note{}, corrupt("note id %q", id)
	}

	n := domain.Note{ID: noteID, Updated: fromMillis(updated)}
	if n.Name, err = openString(dek, name, aadNote("name", id)); err != nil {
		return domain.Note{}, err
	}

	if n.Info, err = openString(dek, info, aadNote("info", id)); err != nil {
		return domain.Note{}, err
	}

	return n, nil
}

// InsertNote stores a new note.
func (s *Store) InsertNote(ctx context.Context, n domain.Note) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error { return insertNote(ctx, db, dek, n) })
	if err != nil {
		return fmt.Errorf("insert note %s: %w", n.ID, err)
	}

	return nil
}

func insertNote(ctx context.Context, q querier, dek []byte, n domain.Note) error {
	id := n.ID.String()

	name, info, err := sealNote(dek, n)
	if err != nil {
		return err
	}

	_, err = q.ExecContext(ctx, "INSERT INTO notes (note_id, updated, name, info) VALUES (?, ?, ?, ?)",
		id, toMillis(n.Updated), name, info)

	return err
}

// UpdateNoteWithVersion writes n and keeps prev as a version in one transaction.
func (s *Store) UpdateNoteWithVersion(ctx context.Context, n domain.Note, prev domain.NoteVersion) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error { return updateNoteWithVersion(ctx, tx, dek, n, prev) })
	})
	if err != nil {
		return fmt.Errorf("update note %s: %w", n.ID, err)
	}

	return nil
}

func updateNoteWithVersion(ctx context.Context, q querier, dek []byte, n domain.Note, prev domain.NoteVersion) error {
	name, info, err := sealNote(dek, n)
	if err != nil {
		return err
	}

	if err := execOne(ctx, q, "UPDATE notes SET updated = ?, name = ?, info = ? WHERE note_id = ?",
		toMillis(n.Updated), name, info, n.ID.String()); err != nil {
		return err
	}

	return insertVersion(ctx, q, dek, prev)
}

func insertVersion(ctx context.Context, q querier, dek []byte, v domain.NoteVersion) error {
	vid, nid := v.ID.String(), v.NoteID.String()

	info, err := sealbox.Seal(dek, []byte(v.Info), aadVersion(vid, nid))
	if err != nil {
		return err
	}

	_, err = q.ExecContext(ctx, "INSERT INTO note_versions (version_id, note_id, created, info) VALUES (?, ?, ?, ?)",
		vid, nid, toMillis(v.Created), info)

	return err
}

// UpdateNoteName writes a new name and Updated of note id.
func (s *Store) UpdateNoteName(ctx context.Context, id uuid.UUID, name string, updated time.Time) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error { return updateNoteName(ctx, db, dek, id, name, updated) })
	if err != nil {
		return fmt.Errorf("rename note %s: %w", id, err)
	}

	return nil
}

func updateNoteName(ctx context.Context, q querier, dek []byte, id uuid.UUID, name string, updated time.Time) error {
	sealed, err := sealbox.Seal(dek, []byte(name), aadNote("name", id.String()))
	if err != nil {
		return err
	}

	return execOne(ctx, q, "UPDATE notes SET updated = ?, name = ? WHERE note_id = ?", toMillis(updated), sealed, id.String())
}

// DeleteNote removes note id; its versions go with it (ON DELETE CASCADE).
func (s *Store) DeleteNote(ctx context.Context, id uuid.UUID) error {
	err := s.unlocked(func(db *sql.DB, _ []byte) error { return deleteNote(ctx, db, id) })
	if err != nil {
		return fmt.Errorf("delete note %s: %w", id, err)
	}

	return nil
}

func deleteNote(ctx context.Context, q querier, id uuid.UUID) error {
	return execOne(ctx, q, "DELETE FROM notes WHERE note_id = ?", id.String())
}

// ListVersions returns the previous texts of note noteID. A version moved to
// another note does not open: its AAD names the note it was sealed for.
func (s *Store) ListVersions(ctx context.Context, noteID uuid.UUID) ([]domain.NoteVersion, error) {
	var versions []domain.NoteVersion

	err := s.unlocked(func(db *sql.DB, dek []byte) (err error) {
		rows, err := db.QueryContext(ctx,
			"SELECT version_id, note_id, created, info FROM note_versions WHERE note_id = ?", noteID.String())
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, rows.Close()) }()

		for rows.Next() {
			var (
				vid, nid string
				created  int64
				info     []byte
			)
			if err := rows.Scan(&vid, &nid, &created, &info); err != nil {
				return err
			}

			versionID, err := uuid.Parse(vid)
			if err != nil {
				return corrupt("version id %q", vid)
			}

			text, err := openString(dek, info, aadVersion(vid, nid))
			if err != nil {
				return err
			}

			versions = append(versions, domain.NoteVersion{ID: versionID, NoteID: noteID, Created: fromMillis(created), Info: text})
		}

		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list versions of %s: %w", noteID, err)
	}

	return versions, nil
}

func sealNote(dek []byte, n domain.Note) (name, info []byte, err error) {
	id := n.ID.String()
	if name, err = sealbox.Seal(dek, []byte(n.Name), aadNote("name", id)); err != nil {
		return nil, nil, err
	}

	if info, err = sealbox.Seal(dek, []byte(n.Info), aadNote("info", id)); err != nil {
		return nil, nil, err
	}

	return name, info, nil
}

// openString opens a sealed field; a field that does not open is a sign of
// damaged or tampered data.
func openString(dek, sealed []byte, aad sealbox.AAD) (string, error) {
	b, err := sealbox.Open(dek, sealed, aad)
	if err != nil {
		return "", fmt.Errorf("%w: %s.%s %s: %w", domain.ErrCorruptStorage, aad.Table, aad.Column, aad.RowID, err)
	}

	return string(b), nil
}

// execOne runs a statement that must change exactly one row.
func execOne(ctx context.Context, q querier, query string, args ...any) error {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n != 1 {
		return fmt.Errorf("%w: %d rows changed", errNoteNotFound, n)
	}

	return nil
}

// inTx runs f in a transaction and commits it, or rolls it back on error.
func inTx(ctx context.Context, db *sql.DB, f func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rerr))
			}
		}
	}()

	if err := f(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func toMillis(t time.Time) int64    { return t.UnixMilli() }
func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
