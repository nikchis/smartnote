package wailsui

import "github.com/nikchis/smartnote/internal/usecase"

// NotesService lists and edits notes.
type NotesService struct {
	env   Env
	notes *usecase.Notes
}

// NewNotesService returns a NotesService over n.
func NewNotesService(env Env, n *usecase.Notes) *NotesService {
	return &NotesService{env: env, notes: n}
}

// List returns the notes ordered by name.
func (s *NotesService) List() ([]NoteDTO, error) {
	notes, err := s.notes.List(s.env.Ctx)
	if err != nil {
		return nil, s.env.fail("list notes", err)
	}

	out := make([]NoteDTO, 0, len(notes))
	for _, n := range notes {
		out = append(out, toNoteDTO(n))
	}

	return out, nil
}

// Create adds a note.
func (s *NotesService) Create(name, info string) (NoteDTO, error) {
	n, err := s.notes.Create(s.env.Ctx, name, info)
	if err != nil {
		return NoteDTO{}, s.env.fail("create note", err)
	}

	return toNoteDTO(n), nil
}

// Save updates the note id; a changed text keeps the previous one as a version.
func (s *NotesService) Save(id, name, info string) (NoteDTO, error) {
	noteID, err := parseID(id)
	if err != nil {
		return NoteDTO{}, s.env.fail("save note", err)
	}

	n, err := s.notes.Save(s.env.Ctx, noteID, name, info)
	if err != nil {
		return NoteDTO{}, s.env.fail("save note", err)
	}

	return toNoteDTO(n), nil
}

// Delete removes the note id with its versions.
func (s *NotesService) Delete(id string) error {
	noteID, err := parseID(id)
	if err != nil {
		return s.env.fail("delete note", err)
	}

	return s.env.fail("delete note", s.notes.Delete(s.env.Ctx, noteID))
}

// Versions returns the previous texts of the note id, newest first.
func (s *NotesService) Versions(id string) ([]VersionDTO, error) {
	noteID, err := parseID(id)
	if err != nil {
		return nil, s.env.fail("note versions", err)
	}

	versions, err := s.notes.Versions(s.env.Ctx, noteID)
	if err != nil {
		return nil, s.env.fail("note versions", err)
	}

	out := make([]VersionDTO, 0, len(versions))
	for _, v := range versions {
		out = append(out, toVersionDTO(v))
	}

	return out, nil
}
