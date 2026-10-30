package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Length limits in UTF-16 code units: of the name and text after NormalizeText,
// of the password and sync phrase after NormalizeSecret.
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

// Normalized returns n with the name and the text passed through NormalizeText.
// Callers validate and store the normalized note.
func (n Note) Normalized() Note {
	n.Name = NormalizeText(n.Name)
	n.Info = NormalizeText(n.Info)
	return n
}

// Validate checks the lengths of the name and the text as given, so a note that
// passes is within the limits when stored. Call it on Normalized(): a CRLF
// still counts as two units here.
func (n Note) Validate() error {
	if err := checkLen("name", UTF16Len(n.Name), 1, MaxNameLen); err != nil {
		return err
	}
	return checkLen("info", UTF16Len(n.Info), 1, MaxInfoLen)
}

// ValidatePassword checks the length of a local storage password in its NFC
// form, the form the key is derived from.
func ValidatePassword(p string) error {
	return checkLen("password", secretLen(p), MinPasswordLen, MaxPasswordLen)
}

// checkLen returns a *ValidationError for field unless the length n is in [minLen, maxLen].
func checkLen(field string, n, minLen, maxLen int) error {
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
