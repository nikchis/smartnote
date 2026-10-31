package wailsui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/domain"
)

// UIError is an error as the interface sees it: a code from spec §3.5, the
// text to show, and the details some codes carry. The server key itself
// never goes to the interface, only its fingerprint and address.
type UIError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Field       string `json:"field,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Expected    string `json:"expected,omitempty"`
	Got         string `json:"got,omitempty"`
	Address     string `json:"address,omitempty"` // host:port that offered the key
}

// Error returns e as a JSON object. Wails rejects the call's promise with an
// Error whose message is this text, so the interface parses the code from it
// whatever else Wails adds (docs/dev-notes.md, check 1).
func (e *UIError) Error() string {
	b, err := json.Marshal(e)
	if err != nil { // a struct of strings always marshals
		return `{"code":"internal","message":"unencodable error"}`
	}

	return string(b)
}

// Error codes (spec §3.5).
const (
	codeWrongPassword         = "wrong_password"
	codeCorruptStorage        = "corrupt_storage"
	codeUnsupportedFormat     = "unsupported_format"
	codeWrongSyncPhrase       = "wrong_sync_phrase"
	codeHostKeyUnknown        = "host_key_unknown"
	codeHostKeyMismatch       = "host_key_mismatch"
	codeValidation            = "validation"
	codeNotConnected          = "not_connected"
	codeRemoteChanged         = "remote_changed"
	codeLocalChanged          = "local_changed"
	codeKeyMissing            = "key_missing"
	codeKeyPassphraseRequired = "key_passphrase_required"
	codeBusy                  = "busy"
	codeTimeout               = "timeout"
	codeCanceled              = "canceled"
	codeSSH                   = "ssh"
	codeDatabase              = "database"
	codeInternal              = "internal"
)

// Messages of the codes whose text the spec does not fix.
const (
	msgCorruptStorage        = "Local storage is damaged and cannot be opened."
	msgWrongSyncPhrase       = "The sync phrase does not match the remote storage."
	msgNotConnected          = "Remote storage is not configured."
	msgKeyPassphraseRequired = "The key is protected with a passphrase." //nolint:gosec // a message, not a credential
	msgBusy                  = "Another operation with the remote storage is in progress."
	msgTimeout               = "The operation timed out."
	msgCanceled              = "The operation was canceled."
)

// sentinels maps domain errors to their code and text, in the order they are checked.
var sentinels = []struct {
	err           error
	code, message string
}{
	{domain.ErrWrongPassword, codeWrongPassword, "Access to local storage denied."},
	{domain.ErrCorruptStorage, codeCorruptStorage, msgCorruptStorage},
	{domain.ErrUnsupportedFormat, codeUnsupportedFormat, "Data was created by a newer version of Smartnote."},
	{domain.ErrWrongSyncPhrase, codeWrongSyncPhrase, msgWrongSyncPhrase},
	{domain.ErrNotConnected, codeNotConnected, msgNotConnected},
	{domain.ErrRemoteChanged, codeRemoteChanged, "Remote data changed during the operation. Please try again."},
	{domain.ErrLocalChanged, codeLocalChanged, "Local data changed during the operation. Please try again."},
	{domain.ErrKeyMissing, codeKeyMissing, "There is no SSH key. Generate or import a key first."},
	{domain.ErrKeyPassphraseRequired, codeKeyPassphraseRequired, msgKeyPassphraseRequired},
	{domain.ErrBusy, codeBusy, msgBusy},
}

// ToUIError converts err into a *UIError by the table of spec §3.5; nil stays
// nil. Wrapped errors are recognized. A deadline or a cancellation wins over
// the error it caused, then come the domain errors, SSH, PostgreSQL, and
// everything else is internal.
func ToUIError(err error) error {
	if err == nil {
		return nil
	}

	return classify(err)
}

func classify(err error) *UIError {
	if ui, ok := errors.AsType[*UIError](err); ok {
		return ui
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &UIError{Code: codeTimeout, Message: msgTimeout}
	case errors.Is(err, context.Canceled):
		return &UIError{Code: codeCanceled, Message: msgCanceled}
	}

	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		return &UIError{Code: codeValidation, Message: ve.Error(), Field: ve.Field}
	}

	if hk, ok := errors.AsType[*domain.HostKeyUnknownError](err); ok {
		return &UIError{Code: codeHostKeyUnknown, Message: hk.Error(), Fingerprint: hk.Fingerprint, Address: hk.Addr}
	}

	if hk, ok := errors.AsType[*domain.HostKeyMismatchError](err); ok {
		return &UIError{Code: codeHostKeyMismatch, Message: hk.Error(), Expected: hk.Expected, Got: hk.Got}
	}

	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return &UIError{Code: s.code, Message: s.message}
		}
	}

	switch {
	case isSSH(err):
		return &UIError{Code: codeSSH, Message: err.Error()}
	case isDatabase(err):
		return &UIError{Code: codeDatabase, Message: err.Error()}
	default:
		return &UIError{Code: codeInternal, Message: err.Error()}
	}
}

func isSSH(err error) bool {
	_, ok := errors.AsType[*ssh.TransportError](err)
	return ok
}

func isDatabase(err error) bool {
	if _, ok := errors.AsType[*pgconn.PgError](err); ok {
		return true
	}

	_, ok := errors.AsType[*pgconn.ConnectError](err)

	return ok
}

// LogError writes err to log when its code is worth a record: internal with
// its text; ssh, database and timeout without it, since those texts name the
// user, the database and the host (spec §3.5). Codes the interface explains
// to the user are not logged.
func LogError(log *slog.Logger, msg string, err error) {
	if err == nil {
		return
	}

	ui := classify(err)

	switch ui.Code {
	case codeInternal:
		log.Error(msg, "code", ui.Code, applog.Err(err))
	case codeSSH, codeDatabase, codeTimeout:
		log.Error(msg, "code", ui.Code, applog.Redacted(err))
	}
}
