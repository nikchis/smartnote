package gdrive

import (
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

// TestBuiltinClientOfBuild: the values -ldflags -X set are both empty (a
// build without the built-in client) or a valid Google Desktop client. Run
// with the same -ldflags to check a release build; the values are never
// printed.
func TestBuiltinClientOfBuild(t *testing.T) {
	if BuiltinClientID == "" && BuiltinClientSecret == "" {
		t.Skip("this build has no built-in Google client")
	}

	if BuiltinClientID == "" || BuiltinClientSecret == "" {
		t.Fatal("the build set only one of the built-in Google client ID and secret")
	}

	if err := domain.ValidateGoogleClient(BuiltinClientID, BuiltinClientSecret); err != nil {
		field := ""
		if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
			field = ve.Field
		}

		t.Fatalf("the built-in Google client is not valid: %s", field)
	}
}
