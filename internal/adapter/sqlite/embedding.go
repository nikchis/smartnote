package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/sealbox"
)

// ListEmbeddings returns the readable embedding records by note ID, and the
// IDs of the rows that do not open (damaged, or sealed for another row): one
// bad row is not an error, the caller deletes it and indexes the note again.
func (s *Store) ListEmbeddings(ctx context.Context) (map[uuid.UUID][]byte, []uuid.UUID, error) {
	var (
		records    map[uuid.UUID][]byte
		unreadable []uuid.UUID
	)

	err := s.unlocked(func(db *sql.DB, dek []byte) (err error) {
		rows, err := db.QueryContext(ctx, "SELECT note_uuid, data FROM note_embedding")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, rows.Close()) }()

		records = map[uuid.UUID][]byte{}

		for rows.Next() {
			var (
				rawID  string
				sealed []byte
			)
			if err := rows.Scan(&rawID, &sealed); err != nil {
				return err
			}

			id, perr := uuid.Parse(rawID)
			if perr != nil {
				// Not a note ID, so no note is indexed again for it; the row
				// cannot be named in DeleteEmbeddings either.
				continue
			}

			data, oerr := sealbox.Open(dek, sealed, aadEmbedding(rawID))
			if oerr != nil {
				unreadable = append(unreadable, id)
				continue
			}

			records[id] = data
		}

		return rows.Err()
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list embeddings: %w", err)
	}

	return records, unreadable, nil
}

// PutEmbedding stores or replaces the record of note id. The note must exist.
func (s *Store) PutEmbedding(ctx context.Context, id uuid.UUID, data []byte) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		sealed, err := sealbox.Seal(dek, data, aadEmbedding(id.String()))
		if err != nil {
			return err
		}

		_, err = db.ExecContext(ctx, `INSERT INTO note_embedding (note_uuid, data) VALUES (?, ?)
			ON CONFLICT (note_uuid) DO UPDATE SET data = excluded.data`, id.String(), sealed)

		return err
	})
	if err != nil {
		return fmt.Errorf("put embedding %s: %w", id, err)
	}

	return nil
}

// DeleteEmbeddings deletes the records of the given notes; an empty list does nothing.
func (s *Store) DeleteEmbeddings(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	err := s.unlocked(func(db *sql.DB, _ []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error {
			for _, id := range ids {
				if _, err := tx.ExecContext(ctx, "DELETE FROM note_embedding WHERE note_uuid = ?", id.String()); err != nil {
					return err
				}
			}

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}

	return nil
}
