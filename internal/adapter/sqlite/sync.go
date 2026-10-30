package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/usecase"
)

// LocalSnapshot returns all notes with the server and revision of the last
// synchronization, read in one transaction.
func (s *Store) LocalSnapshot(ctx context.Context) (usecase.LocalSnapshot, error) {
	var snap usecase.LocalSnapshot
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error {
			notes, err := listNotes(ctx, tx, dek)
			if err != nil {
				return err
			}
			serverID, revision, err := readSyncState(ctx, tx)
			if err != nil {
				return err
			}
			snap = usecase.LocalSnapshot{Notes: notes, Revision: revision, ServerID: serverID}
			return nil
		})
	})
	if err != nil {
		return usecase.LocalSnapshot{}, fmt.Errorf("local snapshot: %w", err)
	}
	return snap, nil
}

func readSyncState(ctx context.Context, q querier) (uuid.UUID, int64, error) {
	var serverID sql.NullString
	var revision int64
	err := q.QueryRowContext(ctx, "SELECT server_id, last_sync_revision FROM connection WHERE id = 1").Scan(&serverID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, 0, nil
	}
	if err != nil {
		return uuid.Nil, 0, err
	}
	if !serverID.Valid || serverID.String == "" {
		return uuid.Nil, revision, nil
	}
	id, err := uuid.Parse(serverID.String)
	if err != nil {
		return uuid.Nil, 0, corrupt("server id %q", serverID.String)
	}
	return id, revision, nil
}

// ApplyRestore applies p and stores revision and serverID in one transaction:
// any failure leaves the storage as it was. A renamed note takes the name and
// Updated of the server.
func (s *Store) ApplyRestore(ctx context.Context, p usecase.RestorePlan, revision int64, serverID uuid.UUID) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error {
			for _, n := range p.Insert {
				if err := insertNote(ctx, tx, dek, n); err != nil {
					return fmt.Errorf("insert %s: %w", n.ID, err)
				}
			}
			for _, c := range p.Text {
				if err := updateNoteWithVersion(ctx, tx, dek, c.Note, c.Previous); err != nil {
					return fmt.Errorf("update %s: %w", c.Note.ID, err)
				}
			}
			for _, n := range p.Rename {
				if err := updateNoteName(ctx, tx, dek, n.ID, n.Name, n.Updated); err != nil {
					return fmt.Errorf("rename %s: %w", n.ID, err)
				}
			}
			for _, id := range p.Delete {
				if err := deleteNote(ctx, tx, id); err != nil {
					return fmt.Errorf("delete %s: %w", id, err)
				}
			}
			return writeSyncState(ctx, tx, serverID, revision)
		})
	})
	if err != nil {
		return fmt.Errorf("apply restore: %w", err)
	}
	return nil
}

// SetSyncState stores the server and revision of the last synchronization.
func (s *Store) SetSyncState(ctx context.Context, serverID uuid.UUID, revision int64) error {
	err := s.unlocked(func(db *sql.DB, _ []byte) error { return writeSyncState(ctx, db, serverID, revision) })
	if err != nil {
		return fmt.Errorf("set sync state: %w", err)
	}
	return nil
}

func writeSyncState(ctx context.Context, q querier, serverID uuid.UUID, revision int64) error {
	_, err := q.ExecContext(ctx, `INSERT INTO connection (id, server_id, last_sync_revision) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET server_id = excluded.server_id, last_sync_revision = excluded.last_sync_revision`,
		serverID.String(), revision)
	return err
}
