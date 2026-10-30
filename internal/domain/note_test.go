package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNote_Validate(t *testing.T) {
	tests := []struct {
		name      string
		note      Note
		wantField string // empty means valid
	}{
		{name: "valid", note: Note{Name: "n", Info: "i"}},
		{name: "empty name", note: Note{Name: "", Info: "i"}, wantField: "name"},
		{name: "name of 64 units", note: Note{Name: strings.Repeat("a", MaxNameLen), Info: "i"}},
		{name: "name of 65 units", note: Note{Name: strings.Repeat("a", MaxNameLen+1), Info: "i"}, wantField: "name"},
		{name: "name of 32 emoji", note: Note{Name: strings.Repeat("😀", 32), Info: "i"}},
		{name: "name of 33 emoji", note: Note{Name: strings.Repeat("😀", 33), Info: "i"}, wantField: "name"},
		{name: "empty info", note: Note{Name: "n", Info: ""}, wantField: "info"},
		{name: "info of 8192 units", note: Note{Name: "n", Info: strings.Repeat("a", MaxInfoLen)}},
		{name: "info of 8193 units", note: Note{Name: "n", Info: strings.Repeat("a", MaxInfoLen+1)}, wantField: "info"},
		{name: "info of 8192 normalized line breaks", note: Note{Name: "n", Info: strings.Repeat("\r\n", MaxInfoLen)}.Normalized()},
		{name: "info of 8192 crlf is 16384 units before normalization", note: Note{Name: "n", Info: strings.Repeat("\r\n", MaxInfoLen)}, wantField: "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.note.Validate()
			assertValidationField(t, err, tt.wantField)
		})
	}
}

func TestNote_Normalized(t *testing.T) {
	n := Note{Name: "a\r\nb", Info: "c\r\nd\r\n"}

	got := n.Normalized()
	if got.Name != "a\nb" || got.Info != "c\nd\n" {
		t.Errorf("Normalized() = {Name: %q, Info: %q}, want {Name: \"a\\nb\", Info: \"c\\nd\\n\"}", got.Name, got.Info)
	}

	if n.Info != "c\r\nd\r\n" {
		t.Errorf("Normalized changed the receiver: %q", n.Info)
	}
}

// nfdE is "é" decomposed: e and a combining acute accent, 2 units before NFC and 1 after.
const nfdE = "e\u0301"

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantField string
	}{
		{name: "5 units", password: strings.Repeat("a", MinPasswordLen-1), wantField: "password"},
		{name: "6 units", password: strings.Repeat("a", MinPasswordLen)},
		{name: "32 units", password: strings.Repeat("a", MaxPasswordLen)},
		{name: "33 units", password: strings.Repeat("a", MaxPasswordLen+1), wantField: "password"},
		{name: "3 emoji are 6 units", password: strings.Repeat("😀", 3)},
		{name: "empty", password: "", wantField: "password"},
		{name: "3 nfd letters are 6 units but 3 after nfc", password: strings.Repeat(nfdE, 3), wantField: "password"},
		{name: "6 nfd letters are 6 after nfc", password: strings.Repeat(nfdE, 6)},
		{name: "20 nfd letters are 40 units but 20 after nfc", password: strings.Repeat(nfdE, 20)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValidationField(t, ValidatePassword(tt.password), tt.wantField)
		})
	}
}

// assertValidationField checks that err is nil when wantField is empty, and
// otherwise that it is a *ValidationError for wantField matching ErrValidation.
func assertValidationField(t *testing.T, err error, wantField string) {
	t.Helper()

	if wantField == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return
	}

	if err == nil {
		t.Fatalf("want validation error for %q, got nil", wantField)
	}

	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(%v, ErrValidation) = false", err)
	}

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}

	if ve.Field != wantField {
		t.Errorf("Field = %q, want %q (error: %v)", ve.Field, wantField, err)
	}
}
