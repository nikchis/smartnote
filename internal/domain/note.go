package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Length limits in UTF-16 code units: of the name and text after NormalizeText,
// of the password and sync phrase after NormalizeSecret.
const (
	MaxNameLen       = 64
	MaxInfoLen       = 8192
	MinPasswordLen   = 6
	MaxPasswordLen   = 64
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
//
// The name must have a character other than white space and no control
// characters; the text may hold no control characters but line feeds and tabs.
func (n Note) Validate() error {
	if err := checkLen(fieldName, UTF16Len(n.Name), 1, MaxNameLen); err != nil {
		return err
	}

	if strings.TrimFunc(n.Name, unicode.IsSpace) == "" {
		return &ValidationError{Field: fieldName, Reason: reasonRequired}
	}

	if strings.ContainsFunc(n.Name, unicode.IsControl) {
		return &ValidationError{Field: fieldName, Reason: reasonControl}
	}

	if err := checkLen(fieldInfo, UTF16Len(n.Info), 1, MaxInfoLen); err != nil {
		return err
	}

	if strings.ContainsFunc(n.Info, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return &ValidationError{Field: fieldInfo, Reason: reasonControl}
	}

	return nil
}

// ValidatePassword checks the length of a local storage password in its NFC
// form, the form the key is derived from.
func ValidatePassword(p string) error {
	return checkLen("password", secretLen(p), MinPasswordLen, MaxPasswordLen)
}

// Fields of a note as the UI names them.
const (
	fieldName = "name"
	fieldInfo = "info"
)

// Reasons of validation errors.
const (
	reasonRequired = "is required"
	reasonControl  = "must not contain control characters"
)

// checkLen returns a *ValidationError for field unless the length n is in [minLen, maxLen].
func checkLen(field string, n, minLen, maxLen int) error {
	switch {
	case n == 0:
		return &ValidationError{Field: field, Reason: reasonRequired}
	case n < minLen:
		return &ValidationError{Field: field, Reason: fmt.Sprintf("must be at least %d characters", minLen)}
	case n > maxLen:
		return &ValidationError{Field: field, Reason: fmt.Sprintf("must be at most %d characters", maxLen)}
	}

	return nil
}
