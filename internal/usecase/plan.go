package usecase

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// Diff reports whether local and remote differ (spec §6.2): a note is on one
// side only, or the same note ID has another name or text. Updated is not compared.
func Diff(local, remote []domain.Note) bool {
	localByID, remoteByID := index(local), index(remote)
	if len(localByID) != len(remoteByID) {
		return true
	}

	for id, l := range localByID {
		r, ok := remoteByID[id]
		if !ok || !sameContent(l, r) {
			return true
		}
	}

	return false
}

// Counts are the notes a plan adds, updates and deletes on its target side.
type Counts struct {
	Added, Updated, Deleted int
}

// BackupPlan replaces the server set with Notes, provided the server still has
// ServerID at ExpectedRevision. Generation is the local change generation read
// before the snapshot.
type BackupPlan struct {
	Counts
	Notes            []domain.Note
	ExpectedRevision int64
	ServerID         uuid.UUID
	Generation       uint64
}

// TextChange replaces a local note with the server's, keeping Previous as a version.
type TextChange struct {
	Note     domain.Note
	Previous domain.NoteVersion
}

// RestorePlan turns the local set into the server snapshot taken at Revision
// of ServerID. Rename holds server notes that differ by name only.
type RestorePlan struct {
	Counts
	Insert     []domain.Note
	Text       []TextChange
	Rename     []domain.Note
	Delete     []uuid.UUID
	Revision   int64
	ServerID   uuid.UUID
	Generation uint64
}

// PlanBackup plans sending every local note to the server.
func PlanBackup(local LocalSnapshot, remote RemoteSnapshot, gen uint64) BackupPlan {
	p := BackupPlan{
		Notes:            local.Notes,
		ExpectedRevision: remote.Revision,
		ServerID:         remote.ServerID,
		Generation:       gen,
	}

	remoteByID := index(remote.Notes)
	for _, l := range local.Notes {
		r, ok := remoteByID[l.ID]
		switch {
		case !ok:
			p.Added++
		case !sameContent(l, r):
			p.Updated++
		}
	}

	localByID := index(local.Notes)
	for _, r := range remote.Notes {
		if _, ok := localByID[r.ID]; !ok {
			p.Deleted++
		}
	}

	return p
}

// PlanRestore plans turning the local set into the server's. A changed text
// keeps the local text as a version created at the local Updated. Notes are
// visited in ID order, so ids is called in a stable order.
func PlanRestore(local LocalSnapshot, remote RemoteSnapshot, ids IDGenerator, gen uint64) RestorePlan {
	p := RestorePlan{Revision: remote.Revision, ServerID: remote.ServerID, Generation: gen}

	localByID := index(local.Notes)
	for _, r := range sortedByID(remote.Notes) {
		l, ok := localByID[r.ID]
		switch {
		case !ok:
			p.Insert = append(p.Insert, r)
		case l.Info != r.Info:
			prev := domain.NoteVersion{ID: ids(), NoteID: l.ID, Created: l.Updated, Info: l.Info}
			p.Text = append(p.Text, TextChange{Note: r, Previous: prev})
		case l.Name != r.Name:
			p.Rename = append(p.Rename, r)
		}
	}

	remoteByID := index(remote.Notes)
	for _, l := range sortedByID(local.Notes) {
		if _, ok := remoteByID[l.ID]; !ok {
			p.Delete = append(p.Delete, l.ID)
		}
	}

	p.Counts = Counts{Added: len(p.Insert), Updated: len(p.Text) + len(p.Rename), Deleted: len(p.Delete)}

	return p
}

func sameContent(a, b domain.Note) bool { return a.Name == b.Name && a.Info == b.Info }

func index(notes []domain.Note) map[uuid.UUID]domain.Note {
	m := make(map[uuid.UUID]domain.Note, len(notes))
	for _, n := range notes {
		m[n.ID] = n
	}

	return m
}

func sortedByID(notes []domain.Note) []domain.Note {
	out := slices.Clone(notes)
	slices.SortFunc(out, func(a, b domain.Note) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	return out
}

// checkRemoteNotes returns domain.ErrUnsupportedFormat unless every server note
// has a unique non-nil ID and is a normalized, valid note: a newer or broken
// client could have written notes this version cannot keep. The error names
// the position and the rule, never the content.
func checkRemoteNotes(notes []domain.Note) error {
	seen := make(map[uuid.UUID]struct{}, len(notes))
	for i, n := range notes {
		if n.ID == uuid.Nil {
			return fmt.Errorf("%w: note %d has no id", domain.ErrUnsupportedFormat, i)
		}

		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("%w: note %d repeats an id", domain.ErrUnsupportedFormat, i)
		}

		seen[n.ID] = struct{}{}
		if n.Normalized() != n {
			return fmt.Errorf("%w: note %d is not normalized", domain.ErrUnsupportedFormat, i)
		}

		if err := n.Validate(); err != nil {
			return fmt.Errorf("%w: note %d: %w", domain.ErrUnsupportedFormat, i, err)
		}
	}

	return nil
}
