package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Length limits in UTF-16 code units after NormalizeText.
const (
	MaxNameLen       = 64
	MaxInfoLen       = 8192
	MinPasswordLen   = 6
	MaxPasswordLen   = 32
	MinSyncPhraseLen = 16
)

// Note is a decrypted note.
type Note struct {
	ID      uuid.UUID
	Name    string
	Info    string
	Updated time.Time
}

// NoteVersion is a previous text of a note, kept when the text changes.
type NoteVersion struct {
	ID      uuid.UUID
	NoteID  uuid.UUID
	Created time.Time
	Info    string
}

// Validate checks the lengths of the name and the text.
func (n Note) Validate() error {
	if err := checkLen("name", n.Name, 1, MaxNameLen); err != nil {
		return err
	}
	return checkLen("info", n.Info, 1, MaxInfoLen)
}

// ValidatePassword checks the length of a local storage password.
func ValidatePassword(p string) error {
	return checkLen("password", p, MinPasswordLen, MaxPasswordLen)
}

// checkLen returns a *ValidationError for field unless textLen(s) is in [minLen, maxLen].
func checkLen(field, s string, minLen, maxLen int) error {
	n := textLen(s)
	switch {
	case n == 0:
		return &ValidationError{Field: field, Reason: "is required"}
	case n < minLen:
		return &ValidationError{Field: field, Reason: fmt.Sprintf("must be at least %d characters", minLen)}
	case n > maxLen:
		return &ValidationError{Field: field, Reason: fmt.Sprintf("must be at most %d characters", maxLen)}
	}
	return nil
}
