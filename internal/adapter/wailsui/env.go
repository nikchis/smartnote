package wailsui

import (
	"context"
	"log/slog"
	"time"
)

// operationTimeout bounds one operation with the server (spec §6.1).
const operationTimeout = 60 * time.Second

// Env is what every service needs. Ctx is the application's root context:
// it ends when the application quits, which cancels running operations.
type Env struct {
	Ctx context.Context //nolint:containedctx // the root context lives as long as the services
	Log *slog.Logger
}

// remote returns the context of one operation with the server.
func (e Env) remote() (context.Context, context.CancelFunc) {
	return context.WithTimeout(e.Ctx, operationTimeout)
}

// fail logs err when its code calls for it and converts it for the interface.
func (e Env) fail(op string, err error) error {
	LogError(e.Log, op, err)
	return ToUIError(err)
}
