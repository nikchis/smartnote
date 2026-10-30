// Package domain holds Smartnote's entities and data rules. It imports no
// other package of the project.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors. Adapters wrap them with context; the UI layer maps them to codes.
var (
	ErrValidation            = errors.New("validation failed")
	ErrWrongPassword         = errors.New("wrong password")
	ErrCorruptStorage        = errors.New("local storage is corrupt")
	ErrUnsupportedFormat     = errors.New("data was created by a newer version")
	ErrWrongSyncPhrase       = errors.New("wrong sync phrase")
	ErrNotConnected          = errors.New("not connected")
	ErrRemoteChanged         = errors.New("remote data changed during the operation")
	ErrLocalChanged          = errors.New("local data changed during the operation")
	ErrBusy                  = errors.New("another server operation is in progress")
	ErrStorageExists         = errors.New("local storage already exists")
	ErrStorageMissing        = errors.New("local storage does not exist")
	ErrKeyPassphraseRequired = errors.New("key passphrase required")
	ErrKeyMissing            = errors.New("no ssh key")
	ErrNoPendingOperation    = errors.New("no pending operation")
)

// ValidationError reports an invalid field. It matches ErrValidation.
type ValidationError struct {
	Field  string // field name as the UI knows it, e.g. "ssh_port"
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// Is reports whether target is ErrValidation.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// HostKeyUnknownError is returned when no server host key is trusted yet.
type HostKeyUnknownError struct {
	Fingerprint   string // SHA256 fingerprint of the presented key
	AuthorizedKey string // the presented key in authorized_keys format
}

func (e *HostKeyUnknownError) Error() string {
	return fmt.Sprintf("unknown server host key %s", e.Fingerprint)
}

// HostKeyMismatchError is returned when the server presents a key other than the trusted one.
type HostKeyMismatchError struct {
	Expected string // SHA256 fingerprint of the trusted key
	Got      string // SHA256 fingerprint of the presented key
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("server host key mismatch: expected %s, got %s", e.Expected, e.Got)
}
