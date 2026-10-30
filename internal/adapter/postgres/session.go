package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

// Options configure NewSession. KDF is used only when the storage is
// created. Abort tears down the transport (the SSH client) when a context
// ends, since an SSH channel has no deadlines; nil means nothing to tear down.
type Options struct {
	Mode   usecase.OpenMode
	Phrase string
	KDF    sealbox.KDFParams
	Abort  func()
}

// closeTimeout bounds the goodbye message to the server on Close.
const closeTimeout = 2 * time.Second

// Session is one operation's connection with the remote storage. It
// implements usecase.RemoteSession. It is not safe for concurrent use.
type Session struct {
	conn    *pgx.Conn
	abort   func()
	key     []byte
	created bool
	empty   bool

	closeOnce sync.Once
	closeErr  error

	// Test hooks: snapshotHook runs inside Snapshot between reading the
	// revision and the notes, backupHook inside Backup after the upsert.
	snapshotHook func()
	backupHook   func()
}

// NewSession checks the remote storage on conn and returns a ready session.
// OpenCreate creates the schema and the meta row when missing; OpenCheck
// creates nothing and reports a storage without meta as Empty, without
// checking the phrase. NewSession owns conn: on error it closes it and calls
// Abort.
func NewSession(ctx context.Context, conn *pgx.Conn, o Options) (*Session, error) {
	abort := o.Abort
	if abort == nil {
		abort = func() {}
	}

	s := &Session{conn: conn, abort: abort}

	stop := context.AfterFunc(ctx, abort)
	defer stop()

	if err := s.open(ctx, o); err != nil {
		return nil, errors.Join(err, s.Close())
	}

	return s, nil
}

func (s *Session) open(ctx context.Context, o Options) error {
	if o.Mode == usecase.OpenCheck {
		m, found, err := s.readMetaIfAny(ctx)
		if err != nil {
			return err
		}

		if !found {
			s.empty = true
			return nil
		}

		s.key, err = m.unlock(o.Phrase)

		return err
	}

	m, key, created, err := s.ensureStorage(ctx, o)
	if err != nil {
		return err
	}

	if created {
		s.key, s.created = key, true
		return nil
	}

	s.key, err = m.unlock(o.Phrase)

	return err
}

// ensureStorage creates the schema, the tables and the meta row where they
// are missing, under the advisory lock, so that two devices connecting at
// once share one meta row. When it creates meta it also returns the key.
func (s *Session) ensureStorage(ctx context.Context, o Options) (m meta, key []byte, created bool, err error) {
	err = inTx(ctx, s.conn, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", schemaLockKey); err != nil {
			return fmt.Errorf("lock schema: %w", err)
		}

		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'smartnote')").Scan(&exists); err != nil {
			return fmt.Errorf("check schema: %w", err)
		}

		if !exists {
			if _, err := tx.Exec(ctx, "CREATE SCHEMA smartnote"); err != nil {
				return fmt.Errorf("create schema: %w", err)
			}
		}
		// CREATE TABLE IF NOT EXISTS needs CREATE on the schema even when the
		// table exists, so the DDL runs only when something is missing.
		var missing bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('smartnote.meta') IS NULL
			OR to_regclass('smartnote.notes') IS NULL OR to_regclass('smartnote.sync_state') IS NULL`).Scan(&missing); err != nil {
			return fmt.Errorf("check tables: %w", err)
		}

		if missing {
			if _, err := tx.Exec(ctx, ddl); err != nil {
				return fmt.Errorf("create tables: %w", err)
			}
		}

		var found bool

		m, found, err = readMeta(ctx, tx)
		if err != nil || found {
			return err
		}

		m, key, err = newMeta(o.Phrase, o.KDF)
		if err != nil {
			return err
		}

		kdf, err := jsonKDF(m.KDF)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `INSERT INTO smartnote.meta (id, server_id, format_version, kdf_params, kdf_salt, key_check)
			VALUES (1, $1, $2, $3, $4, $5)`, m.ServerID.String(), m.FormatVersion, kdf, m.Salt, m.KeyCheck); err != nil {
			return fmt.Errorf("write meta: %w", err)
		}

		created = true

		return nil
	})
	if err != nil {
		sealbox.Wipe(key)
		return meta{}, nil, false, err
	}

	return m, key, created, nil
}

// readMetaIfAny reads meta without creating anything; a missing schema,
// table or row is not an error.
func (s *Session) readMetaIfAny(ctx context.Context) (meta, bool, error) {
	var exists bool
	if err := s.conn.QueryRow(ctx, "SELECT to_regclass('smartnote.meta') IS NOT NULL").Scan(&exists); err != nil {
		return meta{}, false, fmt.Errorf("check tables: %w", err)
	}

	if !exists {
		return meta{}, false, nil
	}

	return readMeta(ctx, s.conn)
}

// queryRower is *pgx.Conn or pgx.Tx.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readMeta(ctx context.Context, q queryRower) (meta, bool, error) {
	var (
		m        meta
		serverID string
		kdf      []byte
	)

	err := q.QueryRow(ctx, `SELECT server_id::text, format_version, kdf_params::text, kdf_salt, key_check
		FROM smartnote.meta WHERE id = 1`).Scan(&serverID, &m.FormatVersion, &kdf, &m.Salt, &m.KeyCheck)
	if errors.Is(err, pgx.ErrNoRows) {
		return meta{}, false, nil
	}

	if err != nil {
		return meta{}, false, fmt.Errorf("read meta: %w", err)
	}

	if m.ServerID, err = uuid.Parse(serverID); err != nil {
		return meta{}, false, fmt.Errorf("%w: server id %q", domain.ErrUnsupportedFormat, serverID)
	}

	if m.KDF, err = parseKDF(kdf); err != nil {
		return meta{}, false, err
	}

	return m, true, nil
}

// Created reports whether this session created the meta row.
func (s *Session) Created() bool { return s.created }

// Empty reports, for OpenCheck, that the server has no meta row.
func (s *Session) Empty() bool { return s.empty }

// Snapshot reads the revision, the server ID and all notes in one REPEATABLE
// READ transaction and decrypts every note; any row that does not open fails
// the whole snapshot.
func (s *Session) Snapshot(ctx context.Context) (usecase.RemoteSnapshot, error) {
	if s.key == nil {
		return usecase.RemoteSnapshot{}, errNoStorage
	}

	stop := context.AfterFunc(ctx, s.abort)
	defer stop()

	var snap usecase.RemoteSnapshot

	err := inTx(ctx, s.conn, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var serverID string

		if err := tx.QueryRow(ctx, "SELECT revision FROM smartnote.sync_state WHERE id = 1").Scan(&snap.Revision); err != nil {
			return fmt.Errorf("read revision: %w", err)
		}

		if err := tx.QueryRow(ctx, "SELECT server_id::text FROM smartnote.meta WHERE id = 1").Scan(&serverID); err != nil {
			return fmt.Errorf("read server id: %w", err)
		}

		id, err := uuid.Parse(serverID)
		if err != nil {
			return fmt.Errorf("%w: server id %q", domain.ErrUnsupportedFormat, serverID)
		}

		snap.ServerID = id

		if s.snapshotHook != nil {
			s.snapshotHook()
		}

		snap.Notes, err = s.readNotes(ctx, tx)

		return err
	})
	if err != nil {
		return usecase.RemoteSnapshot{}, fmt.Errorf("remote snapshot: %w", err)
	}

	return snap, nil
}

func (s *Session) readNotes(ctx context.Context, tx pgx.Tx) ([]domain.Note, error) {
	rows, err := tx.Query(ctx, "SELECT note_id::text, updated, name, info FROM smartnote.notes")
	if err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	defer rows.Close()

	var notes []domain.Note

	for rows.Next() {
		var (
			id         string
			updated    time.Time
			name, info []byte
		)
		if err := rows.Scan(&id, &updated, &name, &info); err != nil {
			return nil, fmt.Errorf("read notes: %w", err)
		}

		n, err := openNote(s.key, id, updated, name, info)
		if err != nil {
			return nil, err
		}

		notes = append(notes, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}

	return notes, nil
}

// Backup replaces the server set with notes in one transaction, provided the
// server still has expectedServerID at expectedRevision (checked under the
// sync_state row lock); otherwise it returns domain.ErrRemoteChanged before
// changing anything. It returns the new revision.
func (s *Session) Backup(ctx context.Context, notes []domain.Note, expectedServerID uuid.UUID, expectedRevision int64) (int64, error) {
	if s.key == nil {
		return 0, errNoStorage
	}

	stop := context.AfterFunc(ctx, s.abort)
	defer stop()

	var revision int64

	err := inTx(ctx, s.conn, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var (
			current  int64
			serverID string
		)

		if err := tx.QueryRow(ctx, "SELECT revision FROM smartnote.sync_state WHERE id = 1 FOR UPDATE").Scan(&current); err != nil {
			return fmt.Errorf("lock revision: %w", err)
		}

		if err := tx.QueryRow(ctx, "SELECT server_id::text FROM smartnote.meta WHERE id = 1").Scan(&serverID); err != nil {
			return fmt.Errorf("read server id: %w", err)
		}

		if serverID != expectedServerID.String() || current != expectedRevision {
			return domain.ErrRemoteChanged
		}

		if err := s.upsert(ctx, tx, notes); err != nil {
			return err
		}

		if s.backupHook != nil {
			s.backupHook()
		}

		ids := make([]string, len(notes))
		for i, n := range notes {
			ids[i] = n.ID.String()
		}

		if _, err := tx.Exec(ctx, "DELETE FROM smartnote.notes WHERE note_id <> ALL($1::uuid[])", ids); err != nil {
			return fmt.Errorf("delete notes: %w", err)
		}

		if err := tx.QueryRow(ctx, `UPDATE smartnote.sync_state SET revision = revision + 1, updated_at = now()
			WHERE id = 1 RETURNING revision`).Scan(&revision); err != nil {
			return fmt.Errorf("bump revision: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}

	return revision, nil
}

// upsert writes notes in one batch, one round trip through the tunnel.
func (s *Session) upsert(ctx context.Context, tx pgx.Tx, notes []domain.Note) (err error) {
	if len(notes) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	for _, n := range notes {
		name, info, err := sealNote(s.key, n)
		if err != nil {
			return err
		}

		batch.Queue(`INSERT INTO smartnote.notes (note_id, updated, name, info) VALUES ($1::uuid, $2, $3, $4)
			ON CONFLICT (note_id) DO UPDATE SET updated = excluded.updated, name = excluded.name, info = excluded.info`,
			n.ID.String(), n.Updated, name, info)
	}

	br := tx.SendBatch(ctx, batch)
	defer func() {
		if cerr := br.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	for range notes {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("write notes: %w", err)
		}
	}

	return nil
}

// Close wipes the key, says goodbye to the server and calls Abort. Later
// calls return the first result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		sealbox.Wipe(s.key)

		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()

		if err := s.conn.Close(ctx); err != nil {
			s.closeErr = fmt.Errorf("close database connection: %w", err)
		}

		s.abort()
	})

	return s.closeErr
}

// errNoStorage is returned by Snapshot and Backup of an OpenCheck session
// that found no storage.
var errNoStorage = errors.New("remote storage is empty")

// inTx runs f in a transaction and commits it, or rolls it back on error.
func inTx(ctx context.Context, conn *pgx.Conn, opts pgx.TxOptions, f func(tx pgx.Tx) error) (err error) {
	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err == nil {
			return
		}
		// The rollback runs without ctx: a canceled operation must still end its transaction.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()

		if rerr := tx.Rollback(rctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rerr))
		}
	}()

	if err := f(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// jsonKDF encodes p for the jsonb column kdf_params.
func jsonKDF(p sealbox.KDFParams) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
