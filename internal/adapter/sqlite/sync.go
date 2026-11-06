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

			synced, err := readSynced(ctx, tx)
			if err != nil {
				return err
			}

			// The current pair wins: 2.1.0 writes only that one.
			if serverID != uuid.Nil {
				synced[serverID] = revision
			}

			snap = usecase.LocalSnapshot{Notes: notes, Revision: revision, ServerID: serverID, Synced: synced}

			return nil
		})
	})
	if err != nil {
		return usecase.LocalSnapshot{}, fmt.Errorf("local snapshot: %w", err)
	}

	return snap, nil
}

func readSyncState(ctx context.Context, q querier) (uuid.UUID, int64, error) {
	var (
		serverID sql.NullString
		revision int64
	)

	err := q.QueryRowContext(ctx, "SELECT server_uuid, last_sync_revision FROM connection WHERE connection_id = 1").Scan(&serverID, &revision)
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

// readSynced returns the revision of every server in sync_server. The table
// only helps the comparison, so a damaged row is skipped: that server counts
// as one never synchronized with.
func readSynced(ctx context.Context, q querier) (_ map[uuid.UUID]int64, err error) {
	rows, err := q.QueryContext(ctx, "SELECT sync_server_uuid, revision FROM sync_server")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	synced := map[uuid.UUID]int64{}

	for rows.Next() {
		var (
			text     sql.NullString
			revision int64
		)

		if err := rows.Scan(&text, &revision); err != nil {
			return nil, err
		}

		id, err := uuid.Parse(text.String)
		if err != nil || id.String() != text.String {
			continue
		}

		synced[id] = revision
	}

	return synced, rows.Err()
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
	err := s.unlocked(func(db *sql.DB, _ []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error { return writeSyncState(ctx, tx, serverID, revision) })
	})
	if err != nil {
		return fmt.Errorf("set sync state: %w", err)
	}

	return nil
}

// writeSyncState stores the current pair and the server's revision in
// sync_server; tx keeps the rows in agreement. The pair it replaces is kept
// in sync_server first: a database of 2.1.0 or of an earlier build has it
// only in connection.
func writeSyncState(ctx context.Context, tx *sql.Tx, serverID uuid.UUID, revision int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_server (sync_server_uuid, revision)
		SELECT server_uuid, last_sync_revision FROM connection
		WHERE connection_id = 1 AND server_uuid IS NOT NULL AND server_uuid <> ''
		ON CONFLICT (sync_server_uuid) DO UPDATE SET revision = excluded.revision`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO connection (connection_id, server_uuid, last_sync_revision) VALUES (1, ?, ?)
		ON CONFLICT (connection_id) DO UPDATE SET server_uuid = excluded.server_uuid, last_sync_revision = excluded.last_sync_revision`,
		serverID.String(), revision)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO sync_server (sync_server_uuid, revision) VALUES (?, ?)
		ON CONFLICT (sync_server_uuid) DO UPDATE SET revision = excluded.revision`,
		serverID.String(), revision)

	return err
}
