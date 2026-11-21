package wailsui

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
)

const (
	// operationTimeout bounds one operation with the server (spec §6.1).
	operationTimeout = 60 * time.Second
	// cloudOperationTimeout bounds one operation with a cloud storage: uploads
	// can run at about 120 KB/s (spec §14).
	cloudOperationTimeout = 600 * time.Second
)

// Env is what every service needs. Ctx is the application's root context:
// it ends when the application quits, which cancels running operations.
type Env struct {
	Ctx context.Context //nolint:containedctx // the root context lives as long as the services
	Log *slog.Logger

	// SavedKind returns the kind of the saved connection settings; nil means
	// the default timeout for every operation.
	SavedKind func(context.Context) (domain.StorageKind, error)

	// Lang is the current language of the interface, which titles the file
	// dialogs and which PrefsService sets; nil means English.
	Lang *i18n.Current
}

// text is the text of k in the current language.
func (e Env) text(k i18n.Key) string {
	l := i18n.EN
	if e.Lang != nil {
		l = e.Lang.Get()
	}

	return i18n.T(l, k)
}

// timeoutFor returns the limit of one operation with the storage of kind.
func timeoutFor(kind domain.StorageKind) time.Duration {
	if domain.IsCloud(kind) {
		return cloudOperationTimeout
	}

	return operationTimeout
}

// remote returns the context of one operation that is not with the storage
// itself (the OAuth code exchange): the default limit.
func (e Env) remote() (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.Ctx, operationTimeout)
}

// remoteFor returns the context of one operation with the storage of kind.
func (e Env) remoteFor(kind domain.StorageKind) (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.Ctx, timeoutFor(kind))
}

// remoteSaved returns the context of one operation with the saved storage.
// When the saved kind cannot be read the default limit applies and the
// operation itself reports the failure.
func (e Env) remoteSaved() (context.Context, context.CancelFunc) {
	if e.SavedKind == nil {
		return e.remote()
	}

	kind, err := e.SavedKind(e.Ctx)
	if err != nil {
		return e.remote()
	}

	return e.remoteFor(kind)
}

// fail logs err when its code calls for it and converts it for the interface.
func (e Env) fail(op string, err error) error {
	LogError(e.Log, op, err)
	return ToUIError(err)
}
