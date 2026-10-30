package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// testKDF is the lowest allowed cost, to keep tests fast.
var testKDF = sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

func openTest(t *testing.T, dir string) *Store {
	t.Helper()

	s, err := OpenWithKDF(dir, testKDF)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s
}

// createTest returns an unlocked store created in a new directory.
func createTest(t *testing.T) (s *Store, dir string) {
	t.Helper()
	dir = t.TempDir()

	s = openTest(t, dir)
	if err := s.Create(context.Background(), "secret1"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	return s, dir
}

// rawExec runs SQL on the database file directly, as an attacker or a broken
// tool would.
func rawExec(t *testing.T, dir, query string, args ...any) {
	t.Helper()

	db, err := sql.Open("sqlite", dsn(filepath.Join(dir, dbFile)))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()

	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestOpenCreatesNoFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s := openTest(t, dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Open created %v", entries)
	}

	ok, err := s.Exists(context.Background())
	if err != nil || ok {
		t.Errorf("Exists = %v, %v; want false, nil", ok, err)
	}

	if _, err := os.Stat(filepath.Join(dir, dbFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Exists created the database file: %v", err)
	}
}

func TestCreateAndUnlock(t *testing.T) {
	ctx := context.Background()

	s, dir := createTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)

	ok, err := s2.Exists(ctx)
	if err != nil || !ok {
		t.Fatalf("Exists = %v, %v; want true, nil", ok, err)
	}

	if err := s2.Unlock(ctx, "wrong12"); !errors.Is(err, domain.ErrWrongPassword) {
		t.Errorf("Unlock(wrong) = %v, want ErrWrongPassword", err)
	}

	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if err := s2.Create(ctx, "other12"); !errors.Is(err, domain.ErrStorageExists) {
		t.Errorf("Create over an existing storage = %v, want ErrStorageExists", err)
	}
}

func TestLockedStoreRefusesData(t *testing.T) {
	ctx := context.Background()

	s, dir := createTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)

	if _, err := s2.ListNotes(ctx); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("ListNotes before Unlock = %v, want ErrStorageMissing", err)
	}

	if _, err := s2.LoadSettings(ctx); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("LoadSettings before Unlock = %v, want ErrStorageMissing", err)
	}

	if err := s2.ChangePassword(ctx, "newpass1"); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("ChangePassword before Unlock = %v, want ErrStorageMissing", err)
	}

	if err := s2.Unlock(ctx, "wrong12"); err == nil {
		t.Fatal("Unlock with a wrong password succeeded")
	}

	if _, err := s2.ListNotes(ctx); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("ListNotes after a failed Unlock = %v, want ErrStorageMissing", err)
	}
}

func TestUnlockMissing(t *testing.T) {
	s := openTest(t, t.TempDir())
	if err := s.Unlock(context.Background(), "secret1"); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("Unlock without a file = %v, want ErrStorageMissing", err)
	}
}

func TestCreateIsAtomic(t *testing.T) {
	dir := t.TempDir()

	tmp := filepath.Join(dir, tmpFile)
	if err := os.WriteFile(tmp, []byte("half-written storage"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := openTest(t, dir)
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("Open removed the leftover: %v", err)
	}

	if err := s.Create(context.Background(), "secret1"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s left after Create: %v", tmpFile, err)
	}

	if _, err := os.Stat(filepath.Join(dir, dbFile)); err != nil {
		t.Errorf("no %s after Create: %v", dbFile, err)
	}
}

func TestExistsFalseWithoutDEK(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{name: "zero-length file", setup: func(t *testing.T, path string) {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "database without meta", setup: func(t *testing.T, path string) {
			rawExec(t, filepath.Dir(path), "CREATE TABLE other (x INTEGER)")
		}},
		{name: "meta without wrapped_dek", setup: func(t *testing.T, path string) {
			rawExec(t, filepath.Dir(path), "CREATE TABLE meta (key TEXT PRIMARY KEY, value BLOB)")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			path := filepath.Join(dir, dbFile)
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, path)
			s := openTest(t, dir)

			ok, err := s.Exists(ctx)
			if err != nil || ok {
				t.Fatalf("Exists = %v, %v; want false, nil", ok, err)
			}

			if err := s.Create(ctx, "secret1"); err != nil {
				t.Fatalf("Create over a storage without a key: %v", err)
			}

			if ok, err := s.Exists(ctx); err != nil || !ok {
				t.Errorf("Exists after Create = %v, %v", ok, err)
			}
		})
	}
}

func TestCorruptFile(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		data func(t *testing.T) []byte
	}{
		{name: "garbage", data: func(*testing.T) []byte { return bytes.Repeat([]byte("not a database "), 512) }},
		{name: "truncated", data: func(t *testing.T) []byte {
			_, dir := createTest(t)

			b, err := os.ReadFile(filepath.Join(dir, dbFile))
			if err != nil {
				t.Fatal(err)
			}

			return b[:len(b)/2]
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, dbFile)

			data := tt.data(t)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			s := openTest(t, dir)

			if err := s.Unlock(ctx, "secret1"); !errors.Is(err, domain.ErrCorruptStorage) {
				t.Errorf("Unlock = %v, want ErrCorruptStorage", err)
			}

			if _, err := s.Exists(ctx); !errors.Is(err, domain.ErrCorruptStorage) {
				t.Errorf("Exists = %v, want ErrCorruptStorage", err)
			}

			if err := s.Create(ctx, "secret1"); !errors.Is(err, domain.ErrCorruptStorage) {
				t.Errorf("Create = %v, want ErrCorruptStorage: the file must not be replaced", err)
			}

			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got, data) {
				t.Error("the corrupt file was changed")
			}
		})
	}
}

func TestKDFParamsOutOfRange(t *testing.T) {
	_, dir := createTest(t)
	rawExec(t, dir, "UPDATE meta SET value = ? WHERE key = 'kdf_params'", []byte(`{"t":3,"m":4194304,"p":4}`))
	s := openTest(t, dir)

	start := time.Now()

	err := s.Unlock(context.Background(), "secret1")
	if !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("Unlock = %v, want ErrCorruptStorage", err)
	}

	if d := time.Since(start); d > time.Second {
		t.Errorf("Unlock took %v: Argon2id ran with forged parameters", d)
	}
}

func TestUnknownFormatVersion(t *testing.T) {
	_, dir := createTest(t)
	rawExec(t, dir, "UPDATE meta SET value = ? WHERE key = 'format_version'", []byte("2"))

	s := openTest(t, dir)
	if err := s.Unlock(context.Background(), "secret1"); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("Unlock = %v, want ErrUnsupportedFormat", err)
	}
}

func TestBrokenMeta(t *testing.T) {
	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{name: "format version not a number", query: "UPDATE meta SET value = ? WHERE key = 'format_version'", args: []any{[]byte("one")}},
		{name: "salt of 15 bytes", query: "UPDATE meta SET value = ? WHERE key = 'kdf_salt'", args: []any{make([]byte, 15)}},
		{name: "kdf params not json", query: "UPDATE meta SET value = ? WHERE key = 'kdf_params'", args: []any{[]byte("{")}},
		{name: "no salt", query: "DELETE FROM meta WHERE key = 'kdf_salt'"},
		{name: "no notes table", query: "DROP TABLE note_versions; DROP TABLE notes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dir := createTest(t)
			rawExec(t, dir, tt.query, tt.args...)

			s := openTest(t, dir)
			if err := s.Unlock(context.Background(), "secret1"); !errors.Is(err, domain.ErrCorruptStorage) {
				t.Errorf("Unlock = %v, want ErrCorruptStorage", err)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	n := domain.Note{ID: testUUID(1), Name: "n", Info: "i", Updated: time.UnixMilli(1000).UTC()}
	if err := s.InsertNote(ctx, n); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangePassword(ctx, "newpass1"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); !errors.Is(err, domain.ErrWrongPassword) {
		t.Errorf("Unlock(old) = %v, want ErrWrongPassword", err)
	}

	if err := s2.Unlock(ctx, "newpass1"); err != nil {
		t.Fatalf("Unlock(new): %v", err)
	}

	notes, err := s2.ListNotes(ctx)
	if err != nil || len(notes) != 1 || notes[0] != n {
		t.Errorf("ListNotes = %+v, %v; want [%+v]", notes, err, n)
	}
}

func TestUnicodePasswordNFC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	s := openTest(t, dir)
	if err := s.Create(ctx, "passéword"); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "passéword"); err != nil {
		t.Errorf("Unlock with the NFD form: %v", err)
	}
}

func TestPathWithSpecialCharacters(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "a b#c?d%20e")

	s := openTest(t, dir)
	if err := s.Create(ctx, "secret1"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, dbFile)); err != nil {
		t.Errorf("database not at the exact path: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("files outside the data dir: %v", entries)
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}

	dir := filepath.Join(t.TempDir(), "data")

	s := openTest(t, dir)
	if err := s.Create(context.Background(), "secret1"); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveSettings(context.Background(), domain.ConnectionSettings{SSHHost: "h"}); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, dbFile): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	s, _ := createTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	if _, err := s.ListNotes(context.Background()); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("ListNotes after Close = %v, want ErrStorageMissing", err)
	}
}
