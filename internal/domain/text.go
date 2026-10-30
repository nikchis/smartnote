package domain

import (
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// NormalizeText replaces CRLF with LF, as a browser text field does.
func NormalizeText(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// UTF16Len returns the length of s in UTF-16 code units, the unit of the
// browser's maxlength and of the limits in this package.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// NormalizeSecret returns a password or sync phrase in Unicode NFC, so the same
// secret typed on different systems derives the same key. Spaces are kept.
func NormalizeSecret(s string) string {
	return norm.NFC.String(s)
}

// textLen is the length of user text as the limits count it.
func textLen(s string) int { return UTF16Len(NormalizeText(s)) }
