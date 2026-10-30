// Package sqlite is the encrypted local storage (spec §5.1). Every name, text,
// setting and private key is sealed with a data key (DEK) that is itself
// sealed with a key derived from the password.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// Store is the local storage in one directory. It implements
// usecase.VaultStore, NoteStore, SyncStateStore, SettingsStore and KeyStore;
// data methods return domain.ErrStorageMissing until Create or Unlock. It is
// safe for concurrent use.
type Store struct {
	dir string
	kdf sealbox.KDFParams // for new keys; parameters read from the file only open it

	mu  sync.RWMutex
	db  *sql.DB // nil before the first use and after Close
	dek []byte  // nil while locked
}

// Open returns the store in dir, creating the directory with mode 0700. It
// creates and removes no files; the caller holds the data directory lock.
func Open(dir string) (*Store, error) { return OpenWithKDF(dir, sealbox.DefaultKDF()) }

// OpenWithKDF is Open with the KDF parameters for new keys; tests pass cheap ones.
func OpenWithKDF(dir string, p sealbox.KDFParams) (*Store, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("open local storage: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("open local storage: %w", err)
	}
	// A directory needs the x bit to be entered; 0700 still keeps out other users.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("open local storage: %w", err)
	}

	return &Store{dir: dir, kdf: p}, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, dbFile) }

// Exists reports whether the storage has a sealed data key. A file that is
// not a database returns domain.ErrCorruptStorage, so it is never mistaken
// for a missing storage and replaced.
func (s *Store) Exists(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.existsLocked(ctx)
}

func (s *Store) existsLocked(ctx context.Context) (bool, error) {
	info, err := os.Stat(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check local storage: %w", err)
	}

	if info.Size() == 0 {
		return false, nil
	}

	if err := s.ensureDBLocked(); err != nil {
		return false, err
	}

	tables, err := tableNames(ctx, s.db)
	if err != nil {
		return false, err
	}

	if !tables[tableMeta] {
		return false, nil
	}

	var n int

	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM meta WHERE key = ?", metaWrappedDEK).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check local storage: %w", mapOpenError(err))
	}

	return n > 0, nil
}

// Create builds a new storage in local.db.tmp and renames it over local.db,
// so a crash leaves either no storage or a complete one. It refuses to
// replace an existing or corrupt storage.
func (s *Store) Create(ctx context.Context, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ok, err := s.existsLocked(ctx)
	if err != nil {
		return fmt.Errorf("create local storage: %w", err)
	}

	if ok {
		return fmt.Errorf("create local storage: %w", domain.ErrStorageExists)
	}

	if err := s.closeDBLocked(); err != nil {
		return fmt.Errorf("create local storage: %w", err)
	}

	tmp := filepath.Join(s.dir, tmpFile)

	dek, err := s.buildStorage(ctx, tmp, password)
	if err != nil {
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			err = errors.Join(err, rerr)
		}

		return fmt.Errorf("create local storage: %w", err)
	}

	if err := s.install(tmp); err != nil {
		sealbox.Wipe(dek)
		return fmt.Errorf("create local storage: %w", err)
	}

	db, err := openDB(s.path())
	if err != nil {
		sealbox.Wipe(dek)
		return fmt.Errorf("create local storage: %w", err)
	}

	s.db, s.dek = db, dek

	return nil
}

// buildStorage writes the schema and the keys to a fresh file at tmp and
// returns the new data key.
func (s *Store) buildStorage(ctx context.Context, tmp, password string) (dek []byte, err error) {
	for _, p := range []string{tmp, tmp + "-journal"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	// SQLite would create the file with the umask's mode; create it 0600 first.
	if err := s.withRoot(func(root *os.Root) error {
		f, err := root.OpenFile(filepath.Base(tmp), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}

		return f.Close()
	}); err != nil {
		return nil, err
	}

	db, err := openDB(tmp)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	dek = make([]byte, sealbox.KeySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("new data key: %w", err)
	}

	meta, err := s.sealDEK(password, dek)
	if err != nil {
		sealbox.Wipe(dek)
		return nil, err
	}

	meta[metaFormatVersion] = []byte(strconv.Itoa(formatVersion))
	for k, v := range meta {
		if _, err := db.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?)", k, v); err != nil {
			sealbox.Wipe(dek)
			return nil, fmt.Errorf("write meta: %w", err)
		}
	}

	return dek, nil
}

// install makes tmp durable and renames it to local.db.
func (s *Store) install(tmp string) error {
	if err := s.withRoot(func(root *os.Root) error { return syncFile(root, filepath.Base(tmp), os.O_RDWR) }); err != nil {
		return err
	}
	// A journal left by the replaced file would be rolled back into the new one.
	if err := os.Remove(s.path() + "-journal"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(tmp, s.path()); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		return nil // no directory sync on Windows
	}

	return s.withRoot(func(root *os.Root) error { return syncFile(root, ".", os.O_RDONLY) })
}

// withRoot runs f with the data directory opened as an os.Root, so file
// operations cannot leave it.
func (s *Store) withRoot(f func(root *os.Root) error) (err error) {
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := root.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	return f(root)
}

// syncFile flushes the file or directory name in root to disk.
func syncFile(root *os.Root, name string, flag int) (err error) {
	f, err := root.OpenFile(name, flag, 0)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	return f.Sync()
}

// sealDEK derives a key from password with fresh salt and s.kdf and seals dek
// with it; it returns the meta rows to store.
func (s *Store) sealDEK(password string, dek []byte) (map[string][]byte, error) {
	salt, err := sealbox.NewSalt()
	if err != nil {
		return nil, err
	}

	kek, err := deriveKEK(password, salt, s.kdf)
	if err != nil {
		return nil, err
	}
	defer sealbox.Wipe(kek)

	wrapped, err := sealbox.Seal(kek, dek, aadWrappedDEK())
	if err != nil {
		return nil, err
	}

	params, err := json.Marshal(s.kdf)
	if err != nil {
		return nil, err
	}

	return map[string][]byte{metaKDFParams: params, metaKDFSalt: salt, metaWrappedDEK: wrapped}, nil
}

func deriveKEK(password string, salt []byte, p sealbox.KDFParams) ([]byte, error) {
	secret := []byte(domain.NormalizeSecret(password))
	defer sealbox.Wipe(secret)

	return sealbox.DeriveKey(secret, salt, p)
}

// Unlock checks the format and the KDF parameters before running Argon2id,
// then opens the data key. A wrong password returns domain.ErrWrongPassword.
func (s *Store) Unlock(ctx context.Context, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDBLocked(); err != nil {
		return fmt.Errorf("unlock local storage: %w", err)
	}

	dek, err := s.unlock(ctx, password)
	if err != nil {
		return fmt.Errorf("unlock local storage: %w", err)
	}

	sealbox.Wipe(s.dek)
	s.dek = dek

	return nil
}

func (s *Store) unlock(ctx context.Context, password string) ([]byte, error) {
	tables, err := tableNames(ctx, s.db)
	if err != nil {
		return nil, err
	}

	if !tables[tableMeta] {
		return nil, domain.ErrStorageMissing
	}

	meta, err := readMeta(ctx, s.db)
	if err != nil {
		return nil, err
	}

	wrapped, ok := meta[metaWrappedDEK]
	if !ok {
		return nil, domain.ErrStorageMissing
	}

	for _, t := range requiredTables {
		if !tables[t] {
			return nil, corrupt("table %s is missing", t)
		}
	}

	version, err := strconv.Atoi(string(meta[metaFormatVersion]))
	switch {
	case err != nil || version < 1:
		return nil, corrupt("format version %q", meta[metaFormatVersion])
	case version > formatVersion:
		return nil, fmt.Errorf("%w: format version %d", domain.ErrUnsupportedFormat, version)
	}

	var p sealbox.KDFParams
	if err := json.Unmarshal(meta[metaKDFParams], &p); err != nil {
		return nil, corrupt("kdf parameters: %v", err)
	}

	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrCorruptStorage, err)
	}

	salt := meta[metaKDFSalt]
	if len(salt) != sealbox.SaltSize {
		return nil, corrupt("salt of %d bytes", len(salt))
	}

	kek, err := deriveKEK(password, salt, p)
	if err != nil {
		return nil, err
	}
	defer sealbox.Wipe(kek)

	dek, err := sealbox.Open(kek, wrapped, aadWrappedDEK())
	if err != nil {
		return nil, domain.ErrWrongPassword
	}

	if len(dek) != sealbox.KeySize {
		sealbox.Wipe(dek)
		return nil, corrupt("data key of %d bytes", len(dek))
	}

	return dek, nil
}

// ChangePassword seals the data key with a key from newPassword, new salt and
// the default parameters, in one transaction.
func (s *Store) ChangePassword(ctx context.Context, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.dek == nil {
		return fmt.Errorf("change password: %w", errLocked)
	}

	meta, err := s.sealDEK(newPassword, s.dek)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	err = inTx(ctx, s.db, func(tx *sql.Tx) error {
		for k, v := range meta {
			if err := execOne(ctx, tx, "UPDATE meta SET value = ? WHERE key = ?", v, k); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	return nil
}

// Close wipes the data key and closes the database. It can be called again.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sealbox.Wipe(s.dek)

	s.dek = nil
	if err := s.closeDBLocked(); err != nil {
		return fmt.Errorf("close local storage: %w", err)
	}

	return nil
}

func (s *Store) closeDBLocked() error {
	if s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil

	return err
}

// ensureDBLocked opens local.db if it exists; it never creates the file.
func (s *Store) ensureDBLocked() error {
	if s.db != nil {
		return nil
	}

	if _, err := os.Stat(s.path()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ErrStorageMissing
		}

		return err
	}

	db, err := openDB(s.path())
	if err != nil {
		return err
	}

	s.db = db

	return nil
}

// errLocked is returned by data methods before Create or Unlock.
var errLocked = fmt.Errorf("local storage is locked: %w", domain.ErrStorageMissing)

// unlocked runs f with the database and the data key under the read lock.
func (s *Store) unlocked(f func(db *sql.DB, dek []byte) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil || s.dek == nil {
		return errLocked
	}

	return f(s.db, s.dek)
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	return db, nil
}

func tableNames(ctx context.Context, db *sql.DB) (_ map[string]bool, err error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", mapOpenError(err))
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	names := map[string]bool{}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read schema: %w", mapOpenError(err))
		}

		names[name] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema: %w", mapOpenError(err))
	}

	return names, nil
}

func readMeta(ctx context.Context, db *sql.DB) (_ map[string][]byte, err error) {
	rows, err := db.QueryContext(ctx, "SELECT key, value FROM meta")
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", mapOpenError(err))
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	meta := map[string][]byte{}

	for rows.Next() {
		var (
			k string
			v []byte
		)
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("read meta: %w", mapOpenError(err))
		}

		meta[k] = v
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read meta: %w", mapOpenError(err))
	}

	return meta, nil
}
