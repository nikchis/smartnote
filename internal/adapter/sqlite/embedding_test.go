package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

func insertTestNote(t *testing.T, s *Store, id uuid.UUID) {
	t.Helper()

	n := domain.Note{ID: id, Name: "name", Info: "text", Updated: time.UnixMilli(1000).UTC()}
	if err := s.InsertNote(context.Background(), n); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)
	id := testUUID(1)
	insertTestNote(t, s, id)

	record := bytes.Repeat([]byte("embedding-record"), 8) // recognisable plaintext

	if err := s.PutEmbedding(ctx, id, record); err != nil {
		t.Fatal(err)
	}

	// Replacing the record is an upsert.
	record2 := append([]byte("second-version--"), bytes.Repeat([]byte("x"), 64)...)

	if err := s.PutEmbedding(ctx, id, record2); err != nil {
		t.Fatal(err)
	}

	got, bad, err := s.ListEmbeddings(ctx)
	if err != nil || len(bad) != 0 {
		t.Fatalf("ListEmbeddings = %v, %v, %v", got, bad, err)
	}

	if len(got) != 1 || !bytes.Equal(got[id], record2) {
		t.Errorf("records = %v, want the second record of %s", got, id)
	}

	// Only ciphertext is on disk.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range [][]byte{record, record2} {
		if bytes.Contains(raw, r[:32]) {
			t.Errorf("local.db holds the plaintext %q", r[:32])
		}
	}
}

func TestEmbeddingUnreadable(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		query string
	}{
		{name: "garbage", query: "UPDATE note_embedding SET data = x'00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00' WHERE note_uuid = ?"},
		{name: "sealed for another row", query: "UPDATE note_embedding SET data = (SELECT data FROM note_embedding WHERE note_uuid = '" + testUUID(2).String() + "') WHERE note_uuid = ?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, dir := createTest(t)
			good, broken := testUUID(1), testUUID(3)

			for _, id := range []uuid.UUID{good, testUUID(2), broken} {
				insertTestNote(t, s, id)

				if err := s.PutEmbedding(ctx, id, []byte("record "+id.String())); err != nil {
					t.Fatal(err)
				}
			}

			rawExec(t, dir, tt.query, broken.String())

			got, bad, err := s.ListEmbeddings(ctx)
			if err != nil {
				t.Fatalf("ListEmbeddings: %v", err)
			}

			if len(bad) != 1 || bad[0] != broken {
				t.Errorf("unreadable = %v, want [%s]", bad, broken)
			}

			if len(got) != 2 || got[good] == nil {
				t.Errorf("records = %v, want the two good ones", got)
			}

			if err := s.DeleteEmbeddings(ctx, bad); err != nil {
				t.Fatal(err)
			}

			got, bad, err = s.ListEmbeddings(ctx)
			if err != nil || len(bad) != 0 || len(got) != 2 {
				t.Errorf("after delete: %v, %v, %v", got, bad, err)
			}

			if err := s.DeleteEmbeddings(ctx, nil); err != nil {
				t.Errorf("DeleteEmbeddings(nil) = %v", err)
			}
		})
	}
}

func TestEmbeddingCascade(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)
	id := testUUID(1)
	insertTestNote(t, s, id)

	if err := s.PutEmbedding(ctx, id, []byte("record")); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteNote(ctx, id); err != nil {
		t.Fatal(err)
	}

	if got, bad, err := s.ListEmbeddings(ctx); err != nil || len(got) != 0 || len(bad) != 0 {
		t.Errorf("ListEmbeddings = %v, %v, %v; want none", got, bad, err)
	}
}

func TestEmbeddingPutMissingNote(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	if err := s.PutEmbedding(ctx, testUUID(9), []byte("record")); err == nil {
		t.Fatal("PutEmbedding of a missing note succeeded")
	}

	if got, bad, err := s.ListEmbeddings(ctx); err != nil || len(got) != 0 || len(bad) != 0 {
		t.Errorf("ListEmbeddings = %v, %v, %v; want none", got, bad, err)
	}
}

func TestEmbeddingLocked(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())

	if _, _, err := s.ListEmbeddings(ctx); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("ListEmbeddings = %v, want ErrStorageMissing", err)
	}
}

// downgradeToV1 turns a created storage into a format 1 one.
func downgradeToV1(t *testing.T, dir string) {
	t.Helper()
	rawExec(t, dir, "DROP TABLE note_embedding")
	rawExec(t, dir, "UPDATE meta SET value = ? WHERE key = 'format_version'", []byte("1"))
}

func formatVersionOf(t *testing.T, dir string) (version string, hasTable bool) {
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

	var v []byte
	if err := db.QueryRowContext(context.Background(), "SELECT value FROM meta WHERE key = 'format_version'").Scan(&v); err != nil {
		t.Fatal(err)
	}

	tables, err := tableNames(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}

	return string(v), tables[tableNoteEmbedding]
}

func TestUnlockMigratesV1(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)
	insertTestNote(t, s, testUUID(1))

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	downgradeToV1(t, dir)

	if v, has := formatVersionOf(t, dir); v != "1" || has {
		t.Fatalf("setup: version %s, table %v", v, has)
	}

	s = openTest(t, dir)
	if err := s.Unlock(ctx, "secret1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if v, has := formatVersionOf(t, dir); v != "2" || !has {
		t.Errorf("after Unlock: version %s, table %v; want 2, true", v, has)
	}

	if err := s.PutEmbedding(ctx, testUUID(1), []byte("record")); err != nil {
		t.Errorf("PutEmbedding after migration: %v", err)
	}

	if notes, err := s.ListNotes(ctx); err != nil || len(notes) != 1 {
		t.Errorf("ListNotes = %v, %v; the note must survive", notes, err)
	}
}

func TestUnlockWrongPasswordNoMigration(t *testing.T) {
	_, dir := createTest(t)
	downgradeToV1(t, dir)

	s := openTest(t, dir)
	if err := s.Unlock(context.Background(), "wrong-password"); !errors.Is(err, domain.ErrWrongPassword) {
		t.Fatalf("Unlock = %v, want ErrWrongPassword", err)
	}

	if v, has := formatVersionOf(t, dir); v != "1" || has {
		t.Errorf("version %s, table %v; want 1, false: the migration ran", v, has)
	}
}

// A failed migration rolls back both of its steps.
func TestMigrationIsAtomic(t *testing.T) {
	ctx := context.Background()
	_, dir := createTest(t)
	downgradeToV1(t, dir)
	rawExec(t, dir, "DELETE FROM meta WHERE key = 'format_version'") // the UPDATE changes no row

	db, err := openDB(filepath.Join(dir, dbFile))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = db.Close() }()

	if err := migrateV1(ctx, db); err == nil {
		t.Fatal("migrateV1 succeeded without a format_version row")
	}

	tables, err := tableNames(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if tables[tableNoteEmbedding] {
		t.Error("note_embedding stayed after the failed migration")
	}
}

func TestUnlockV2MissingTable(t *testing.T) {
	_, dir := createTest(t)
	rawExec(t, dir, "DROP TABLE note_embedding")

	s := openTest(t, dir)
	if err := s.Unlock(context.Background(), "secret1"); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("Unlock = %v, want ErrCorruptStorage", err)
	}
}

func TestUnlockRejectsV3(t *testing.T) {
	_, dir := createTest(t)
	rawExec(t, dir, "UPDATE meta SET value = ? WHERE key = 'format_version'", []byte("3"))

	s := openTest(t, dir)
	if err := s.Unlock(context.Background(), "secret1"); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("Unlock = %v, want ErrUnsupportedFormat", err)
	}
}
