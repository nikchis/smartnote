package sqlite

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// Compile-time checks that Store implements the five ports.
var (
	_ usecase.VaultStore     = (*Store)(nil)
	_ usecase.NoteStore      = (*Store)(nil)
	_ usecase.SyncStateStore = (*Store)(nil)
	_ usecase.SettingsStore  = (*Store)(nil)
	_ usecase.KeyStore       = (*Store)(nil)
)

var serverA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

func TestSyncStateAndRestore(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	snap, err := s.LocalSnapshot(ctx)
	if err != nil {
		t.Fatalf("LocalSnapshot: %v", err)
	}

	if snap.ServerID != uuid.Nil || snap.Revision != 0 || len(snap.Notes) != 0 {
		t.Errorf("fresh snapshot = %+v, want empty", snap)
	}

	text := domain.Note{ID: testUUID(1), Name: "t", Info: "local", Updated: ms(10)}
	rename := domain.Note{ID: testUUID(2), Name: "old", Info: "same", Updated: ms(10)}

	gone := domain.Note{ID: testUUID(3), Name: "gone", Info: "x", Updated: ms(10)}
	for _, n := range []domain.Note{text, rename, gone} {
		if err := s.InsertNote(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.UpdateNoteWithVersion(ctx, gone, domain.NoteVersion{ID: testUUID(30), NoteID: gone.ID, Created: ms(5), Info: "older"}); err != nil {
		t.Fatal(err)
	}

	plan := usecase.RestorePlan{
		Insert: []domain.Note{{ID: testUUID(4), Name: "new", Info: "server only", Updated: ms(20)}},
		Text: []usecase.TextChange{{
			Note:     domain.Note{ID: text.ID, Name: "t2", Info: "server", Updated: ms(20)},
			Previous: domain.NoteVersion{ID: testUUID(11), NoteID: text.ID, Created: text.Updated, Info: text.Info},
		}},
		Rename: []domain.Note{{ID: rename.ID, Name: "new name", Info: "same", Updated: ms(20)}},
		Delete: []uuid.UUID{gone.ID},
	}
	if err := s.ApplyRestore(ctx, plan, 7, serverA); err != nil {
		t.Fatalf("ApplyRestore: %v", err)
	}

	snap, err = s.LocalSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if snap.Revision != 7 || snap.ServerID != serverA {
		t.Errorf("sync state = (%v, %d), want (A, 7)", snap.ServerID, snap.Revision)
	}

	got := map[uuid.UUID]domain.Note{}
	for _, n := range snap.Notes {
		got[n.ID] = n
	}

	want := map[uuid.UUID]domain.Note{
		testUUID(1): plan.Text[0].Note,
		testUUID(2): plan.Rename[0],
		testUUID(4): plan.Insert[0],
	}
	if len(got) != len(want) {
		t.Fatalf("notes = %+v, want %+v", got, want)
	}

	for id, w := range want {
		if got[id] != w {
			t.Errorf("note %v = %+v, want %+v", id, got[id], w)
		}
	}

	versions, err := s.ListVersions(ctx, text.ID)
	if err != nil || len(versions) != 1 || versions[0] != plan.Text[0].Previous {
		t.Errorf("versions = %+v, %v; want the local text", versions, err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM note_version WHERE note_uuid = ?", gone.ID.String()).Scan(&count); err != nil || count != 0 {
		t.Errorf("versions of the deleted note = %d, %v", count, err)
	}
}

func TestApplyRestoreIsAtomic(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	keep := domain.Note{ID: testUUID(1), Name: "keep", Info: "x", Updated: ms(1)}
	if err := s.InsertNote(ctx, keep); err != nil {
		t.Fatal(err)
	}

	if err := s.SetSyncState(ctx, serverA, 3); err != nil {
		t.Fatal(err)
	}

	plan := usecase.RestorePlan{
		Delete: []uuid.UUID{keep.ID},
		Insert: []domain.Note{
			{ID: testUUID(5), Name: "a", Info: "a", Updated: ms(2)},
			{ID: testUUID(5), Name: "dup", Info: "dup", Updated: ms(2)},
		},
	}
	if err := s.ApplyRestore(ctx, plan, 9, uuid.New()); err == nil {
		t.Fatal("ApplyRestore with a duplicate ID succeeded")
	}

	snap, err := s.LocalSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(snap.Notes) != 1 || snap.Notes[0] != keep || snap.Revision != 3 || snap.ServerID != serverA {
		t.Errorf("snapshot after a failed restore = %+v, want the old state", snap)
	}
}

func TestSetSyncState(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	settings := domain.ConnectionSettings{SSHHost: "h", SSHPort: 22}
	if err := s.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}

	if err := s.SetSyncState(ctx, serverA, 5); err != nil {
		t.Fatalf("SetSyncState: %v", err)
	}

	snap, err := s.LocalSnapshot(ctx)
	if err != nil || snap.Revision != 5 || snap.ServerID != serverA {
		t.Errorf("snapshot = %+v, %v; want (A, 5)", snap, err)
	}

	got, err := s.LoadSettings(ctx)
	if err != nil || got != settings {
		t.Errorf("settings after SetSyncState = %v, %v", got, err)
	}
}
