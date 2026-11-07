package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

// CloudToken is the sign-in token of a cloud storage.
type CloudToken struct {
	Provider, ClientID, Access, Refresh, Account string
	Expiry                                       time.Time
}

// String hides the secrets, so the token is safe to format or log.
func (CloudToken) String() string { return "CloudToken{redacted}" }

// GoString keeps %#v as redacted as String.
func (t CloudToken) GoString() string { return t.String() }

// LogValue keeps slog output as redacted as String.
func (t CloudToken) LogValue() slog.Value { return slog.StringValue(t.String()) }

// CloudTokenSource gives the token for one operation. The stored source reads
// it from local.db, the pending one from memory. A token is never refreshed
// (spec §14): an expired or refused one asks for a new sign-in.
type CloudTokenSource interface {
	// Token returns domain.ErrReauthRequired when there is no usable token.
	Token(ctx context.Context) (CloudToken, error)
}

// CloudTokenStore is the token saved in local.db.
type CloudTokenStore interface {
	LoadCloudToken(ctx context.Context) (CloudToken, bool, error)
	SaveCloudToken(ctx context.Context, t CloudToken) error
}

// TokenChange tells ConnectionStore.SaveConnection what to do with the saved token.
type TokenChange int

// Token changes.
const (
	TokenKeep TokenChange = iota
	TokenSet
	TokenDelete
)

// ConnectionStore saves the settings and the token in one transaction.
type ConnectionStore interface {
	SaveConnection(ctx context.Context, s domain.ConnectionSettings, change TokenChange, t CloudToken) error
}

// CloudAuthorizer starts a sign-in. Exactly one path ends a login session.
type CloudAuthorizer interface {
	// BeginLoopback starts a receiver on 127.0.0.1 and returns the login;
	// domain.ErrLoopbackUnavailable if the port is busy.
	BeginLoopback(ctx context.Context, clientID string) (CloudLogin, error)
	// BeginManual prepares a login with the verification_code redirect_uri.
	BeginManual(ctx context.Context, clientID string) (CloudLogin, error)
}

// CloudLogin is one sign-in session.
type CloudLogin interface {
	URL() string
	// Wait finishes a loopback login.
	Wait(ctx context.Context) (CloudToken, error)
	// Exchange finishes a manual login with the code the user pasted.
	Exchange(ctx context.Context, code string) (CloudToken, error)
	// Close is idempotent and stops the receiver.
	Close() error
}

// EffectiveClientID returns the client ID of s, or builtin when s has none.
func EffectiveClientID(s domain.ConnectionSettings, builtin string) string {
	if s.YandexClientID != "" {
		return s.YandexClientID
	}

	return builtin
}

type storedTokenSource struct {
	store    CloudTokenStore
	clientID string
}

// NewStoredTokenSource returns a source over the token in store. A missing
// token, or one issued for another client ID, is domain.ErrReauthRequired;
// store may be nil.
func NewStoredTokenSource(store CloudTokenStore, clientID string) CloudTokenSource {
	return &storedTokenSource{store: store, clientID: clientID}
}

func (s *storedTokenSource) Token(ctx context.Context) (CloudToken, error) {
	if s.store == nil {
		return CloudToken{}, domain.ErrReauthRequired
	}

	t, ok, err := s.store.LoadCloudToken(ctx)
	if err != nil {
		return CloudToken{}, fmt.Errorf("load cloud token: %w", LocalError{Err: err})
	}

	if !ok || t.ClientID != s.clientID {
		return CloudToken{}, domain.ErrReauthRequired
	}

	return t, nil
}

type memoryTokenSource struct {
	clientID string

	mu  sync.Mutex
	tok CloudToken
}

// NewMemoryTokenSource returns a source that keeps t in memory.
func NewMemoryTokenSource(t CloudToken, clientID string) CloudTokenSource {
	return &memoryTokenSource{tok: t, clientID: clientID}
}

func (m *memoryTokenSource) Token(context.Context) (CloudToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tok.ClientID != m.clientID {
		return CloudToken{}, domain.ErrReauthRequired
	}

	return m.tok, nil
}
