package cloudauth

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// manualLogin ends with the code the user pastes (spec §6.3). The provider does
// not return the state to the app; PKCE makes an intercepted code useless.
type manualLogin struct {
	session

	closed atomic.Bool
}

// Wait is not part of a manual login.
func (*manualLogin) Wait(context.Context) (usecase.CloudToken, error) {
	return usecase.CloudToken{}, fmt.Errorf("wait for the browser: %w", domain.ErrNoPendingOperation)
}

// Exchange exchanges the pasted code; surrounding whitespace is dropped.
// A rejected code leaves the login ready for another one.
func (l *manualLogin) Exchange(ctx context.Context, code string) (usecase.CloudToken, error) {
	if l.closed.Load() {
		return usecase.CloudToken{}, context.Canceled
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return usecase.CloudToken{}, &domain.ValidationError{Field: "code", Reason: "required"}
	}

	return l.redeem(ctx, code)
}

// Close ends the login; it holds no resources. It is idempotent.
func (l *manualLogin) Close() error {
	l.closed.Store(true)
	return nil
}
