package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidationError(t *testing.T) {
	err := fmt.Errorf("save settings: %w", &ValidationError{Field: "ssh_port", Reason: "must be between 1 and 65535"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false")
	}

	if errors.Is(err, ErrWrongPassword) {
		t.Errorf("errors.Is(err, ErrWrongPassword) = true")
	}

	if msg := err.Error(); !strings.Contains(msg, "ssh_port") || !strings.Contains(msg, "between 1 and 65535") {
		t.Errorf("Error() = %q, want field and reason", msg)
	}
}

func TestHostKeyErrors(t *testing.T) {
	unknown := fmt.Errorf("connect: %w", &HostKeyUnknownError{Fingerprint: "SHA256:abc", AuthorizedKey: "ssh-ed25519 AAAA"})

	var u *HostKeyUnknownError
	if !errors.As(unknown, &u) || u.Fingerprint != "SHA256:abc" {
		t.Fatalf("errors.As unknown = %v, %+v", errors.As(unknown, &u), u)
	}

	if !strings.Contains(unknown.Error(), "SHA256:abc") {
		t.Errorf("unknown Error() = %q, want fingerprint", unknown.Error())
	}

	mismatch := fmt.Errorf("connect: %w", &HostKeyMismatchError{Expected: "SHA256:old", Got: "SHA256:new"})

	var m *HostKeyMismatchError
	if !errors.As(mismatch, &m) || m.Expected != "SHA256:old" || m.Got != "SHA256:new" {
		t.Fatalf("errors.As mismatch = %+v", m)
	}

	if msg := mismatch.Error(); !strings.Contains(msg, "SHA256:old") || !strings.Contains(msg, "SHA256:new") {
		t.Errorf("mismatch Error() = %q, want both fingerprints", msg)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{
		ErrValidation, ErrWrongPassword, ErrCorruptStorage, ErrUnsupportedFormat, ErrWrongSyncPhrase,
		ErrNotConnected, ErrRemoteChanged, ErrLocalChanged, ErrBusy, ErrStorageExists, ErrStorageMissing,
		ErrKeyPassphraseRequired, ErrKeyMissing, ErrNoPendingOperation,
	}
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("%v matches %v", a, b)
			}
		}
	}
}
