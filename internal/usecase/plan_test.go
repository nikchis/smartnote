package usecase

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

func note(id byte, name, info string) domain.Note {
	return domain.Note{ID: testID(id), Name: name, Info: info, Updated: t0}
}

func TestDiff(t *testing.T) {
	base := []domain.Note{note(1, "a", "x"), note(2, "b", "y")}

	tests := []struct {
		name   string
		local  []domain.Note
		remote []domain.Note
		want   bool
	}{
		{name: "same sets", local: base, remote: []domain.Note{note(2, "b", "y"), note(1, "a", "x")}, want: false},
		{name: "both empty", local: nil, remote: nil, want: false},
		{name: "only updated differs", local: base, remote: []domain.Note{note(1, "a", "x"), {ID: testID(2), Name: "b", Info: "y", Updated: t1}}, want: false},
		{name: "other name", local: base, remote: []domain.Note{note(1, "a2", "x"), note(2, "b", "y")}, want: true},
		{name: "other text", local: base, remote: []domain.Note{note(1, "a", "x2"), note(2, "b", "y")}, want: true},
		{name: "extra local", local: base, remote: base[:1], want: true},
		{name: "extra remote", local: base[:1], remote: base, want: true},
		{name: "same count other ids", local: base[:1], remote: []domain.Note{note(3, "a", "x")}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Diff(tt.local, tt.remote); got != tt.want {
				t.Errorf("Diff = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanBackup(t *testing.T) {
	serverID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	local := LocalSnapshot{Notes: []domain.Note{note(1, "same", "x"), note(2, "renamed", "y"), note(3, "new", "z"), note(5, "text", "new")}, Revision: 2}
	remote := RemoteSnapshot{Notes: []domain.Note{note(1, "same", "x"), note(2, "old", "y"), note(4, "gone", "w"), note(5, "text", "old")}, Revision: 7, ServerID: serverID}

	p := PlanBackup(local, remote, 42)
	if want := (Counts{Added: 1, Updated: 2, Deleted: 1}); p.Counts != want {
		t.Errorf("Counts = %+v, want %+v", p.Counts, want)
	}

	if len(p.Notes) != len(local.Notes) {
		t.Errorf("Notes = %d, want all %d local notes", len(p.Notes), len(local.Notes))
	}

	if p.ExpectedRevision != 7 || p.ServerID != serverID || p.Generation != 42 {
		t.Errorf("plan = {rev %d, server %v, gen %d}, want {7, %v, 42}", p.ExpectedRevision, p.ServerID, p.Generation, serverID)
	}
}

func TestPlanBackupRemoteNewer(t *testing.T) {
	local := []domain.Note{note(1, "a", "local")}
	remote := []domain.Note{note(1, "a", "server")}

	tests := []struct {
		name   string
		local  LocalSnapshot
		remote RemoteSnapshot
		want   bool
	}{
		{name: "newer revision, other data", local: LocalSnapshot{Notes: local, Revision: 3, ServerID: serverA}, remote: RemoteSnapshot{Notes: remote, Revision: 5, ServerID: serverA}, want: true},
		{name: "same revision", local: LocalSnapshot{Notes: local, Revision: 5, ServerID: serverA}, remote: RemoteSnapshot{Notes: remote, Revision: 5, ServerID: serverA}},
		{name: "newer revision, same data", local: LocalSnapshot{Notes: remote, Revision: 3, ServerID: serverA}, remote: RemoteSnapshot{Notes: remote, Revision: 5, ServerID: serverA}},
		{name: "other server with data", local: LocalSnapshot{Notes: local, Revision: 7, ServerID: serverA}, remote: RemoteSnapshot{Notes: remote, Revision: 1, ServerID: serverB}, want: true},
		{name: "other server, empty", local: LocalSnapshot{Notes: local, Revision: 7, ServerID: serverA}, remote: RemoteSnapshot{Revision: 0, ServerID: serverB}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlanBackup(tt.local, tt.remote, 0).RemoteNewer; got != tt.want {
				t.Errorf("RemoteNewer = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanBackupNoChanges(t *testing.T) {
	notes := []domain.Note{note(1, "a", "x")}

	p := PlanBackup(LocalSnapshot{Notes: notes}, RemoteSnapshot{Notes: notes}, 0)
	if p.Counts != (Counts{}) {
		t.Errorf("Counts = %+v, want zero", p.Counts)
	}
}

func TestPlanRestore(t *testing.T) {
	serverID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	localText := domain.Note{ID: testID(1), Name: "n", Info: "local text", Updated: t0}
	local := LocalSnapshot{Notes: []domain.Note{
		localText,
		note(2, "old name", "same"),
		note(3, "extra", "local only"),
		note(6, "same", "same"),
	}, Revision: 3}
	remoteText := domain.Note{ID: testID(1), Name: "n2", Info: "server text", Updated: t1}
	remote := RemoteSnapshot{Notes: []domain.Note{
		remoteText,
		{ID: testID(2), Name: "new name", Info: "same", Updated: t1},
		note(4, "missing", "server only"),
		note(6, "same", "same"),
	}, Revision: 9, ServerID: serverID}

	p := PlanRestore(local, remote, seqIDs(), 5)

	if want := (Counts{Added: 1, Updated: 2, Deleted: 1}); p.Counts != want {
		t.Errorf("Counts = %+v, want %+v", p.Counts, want)
	}

	if len(p.Text) != 1 {
		t.Fatalf("Text = %+v, want one change", p.Text)
	}

	tc := p.Text[0]
	if tc.Note != remoteText {
		t.Errorf("Text[0].Note = %+v, want the server note %+v", tc.Note, remoteText)
	}

	if tc.Previous.NoteID != testID(1) || tc.Previous.Info != "local text" || !tc.Previous.Created.Equal(t0) {
		t.Errorf("Previous = %+v, want local text created at the local Updated %v", tc.Previous, t0)
	}

	if tc.Previous.ID == uuid.Nil {
		t.Error("Previous has no ID")
	}

	if len(p.Rename) != 1 || p.Rename[0].ID != testID(2) || p.Rename[0].Name != "new name" {
		t.Errorf("Rename = %+v, want note 2 named \"new name\"", p.Rename)
	}

	if len(p.Insert) != 1 || p.Insert[0].ID != testID(4) {
		t.Errorf("Insert = %+v, want note 4", p.Insert)
	}

	if len(p.Delete) != 1 || p.Delete[0] != testID(3) {
		t.Errorf("Delete = %v, want note 3", p.Delete)
	}

	if p.Revision != 9 || p.ServerID != serverID || p.Generation != 5 {
		t.Errorf("plan = {rev %d, server %v, gen %d}, want {9, %v, 5}", p.Revision, p.ServerID, p.Generation, serverID)
	}
}
