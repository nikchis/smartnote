package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

// CloudToken is the sign-in token of a cloud storage, one per provider.
type CloudToken struct {
	Provider                           domain.StorageKind
	ClientID, Access, Refresh, Account string
	Expiry                             time.Time
}

// String hides the secrets, so the token is safe to format or log.
func (CloudToken) String() string { return "CloudToken{redacted}" }

// GoString keeps %#v as redacted as String.
func (t CloudToken) GoString() string { return t.String() }

// LogValue keeps slog output as redacted as String.
func (t CloudToken) LogValue() slog.Value { return slog.StringValue(t.String()) }

// CloudClient is the effective OAuth client of a provider: the own one of
// the settings or the built-in one. Secret is empty for Yandex.
type CloudClient struct {
	Provider domain.StorageKind
	ID       string
	Secret   string
}

// String hides the secret, so the client is safe to format or log.
func (c CloudClient) String() string { return "CloudClient{" + string(c.Provider) + " redacted}" }

// GoString keeps %#v as redacted as String.
func (c CloudClient) GoString() string { return c.String() }

// LogValue keeps slog output as redacted as String.
func (c CloudClient) LogValue() slog.Value { return slog.StringValue(c.String()) }

// CloudTokenSource gives the token for one operation and keeps the token a
// refresh gives. The stored source reads and writes local.db, the pending
// one keeps the token of a sign-in not yet saved in memory.
type CloudTokenSource interface {
	// Token returns a domain.ReauthRequiredError of the source's provider
	// when there is no usable token.
	Token(ctx context.Context) (CloudToken, error)
	// Update keeps t, a refreshed token, as the token of the source's
	// provider and client.
	Update(ctx context.Context, t CloudToken) error
}

// CloudTokenStore is the token of each provider saved in local.db.
type CloudTokenStore interface {
	LoadCloudToken(ctx context.Context, p domain.StorageKind) (CloudToken, bool, error)
	// SaveCloudToken replaces the token of t.Provider.
	SaveCloudToken(ctx context.Context, t CloudToken) error
}

// TokenChange is what ConnectionStore.SaveConnection does with the saved
// token of one provider. The zero value is no change and is invalid.
type TokenChange int

// Token changes.
const (
	TokenSet TokenChange = iota + 1
	TokenDelete
)

// TokenUpdate changes the saved token of Provider; providers without a
// change are not listed.
type TokenUpdate struct {
	Provider domain.StorageKind
	Change   TokenChange
	Token    CloudToken // for TokenSet
}

// ConnectionStore saves the settings and applies the token updates in one
// transaction: if any update fails, nothing is saved.
type ConnectionStore interface {
	SaveConnection(ctx context.Context, s domain.ConnectionSettings, updates []TokenUpdate) error
}

// CloudAuthorizer starts a sign-in with client c. Exactly one path ends a
// login session.
type CloudAuthorizer interface {
	// BeginLoopback starts a receiver on 127.0.0.1 and returns the login;
	// domain.ErrLoopbackUnavailable if the port is busy.
	BeginLoopback(ctx context.Context, c CloudClient) (CloudLogin, error)
	// BeginManual prepares a login with a code the user copies;
	// domain.ErrManualUnsupported for a provider without one.
	BeginManual(ctx context.Context, c CloudClient) (CloudLogin, error)
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

// noBuiltinGoogleReason refuses Google settings without an own client in a
// build without the built-in Google client (spec §1, ред. 8).
const noBuiltinGoogleReason = "this build has no built-in Google client: set your own client ID and secret in Advanced"

// errNoClient refuses cloud settings of kind without an effective client:
// the user sets an own client in Advanced. For Google this is a build
// without the built-in client.
func errNoClient(kind domain.StorageKind) error {
	if kind == domain.KindGoogleDrive {
		return &domain.ValidationError{Field: "google_client_id", Reason: noBuiltinGoogleReason}
	}

	return &domain.ValidationError{Field: "yandex_client_id", Reason: "set your own client ID in Advanced"}
}

// EffectiveClient returns the OAuth client of the kind of s: the own one of
// s, or else the built-in one of builtin. It is false for a kind that is not
// a cloud, without either client, and for an own Google client ID without
// its secret, which never works with the built-in secret.
func EffectiveClient(s domain.ConnectionSettings, builtin map[domain.StorageKind]CloudClient) (CloudClient, bool) {
	kind := s.Kind()

	var own CloudClient

	switch kind {
	case domain.KindYandexDisk:
		own = CloudClient{ID: s.YandexClientID}
	case domain.KindGoogleDrive:
		if (s.GoogleClientID == "") != (s.GoogleClientSecret == "") {
			return CloudClient{}, false
		}

		own = CloudClient{ID: s.GoogleClientID, Secret: s.GoogleClientSecret}
	default:
		return CloudClient{}, false
	}

	c := own
	if c.ID == "" {
		c = builtin[kind]
	}

	if c.ID == "" {
		return CloudClient{}, false
	}

	c.Provider = kind

	return c, true
}

type storedTokenSource struct {
	store  CloudTokenStore
	client CloudClient
}

// NewStoredTokenSource returns a source over the token of c.Provider in
// store. A missing token, or one issued for another client ID, asks for a
// new sign-in to c.Provider; store may be nil. Update writes local.db.
func NewStoredTokenSource(store CloudTokenStore, c CloudClient) CloudTokenSource {
	return &storedTokenSource{store: store, client: c}
}

func (s *storedTokenSource) Token(ctx context.Context) (CloudToken, error) {
	if s.store == nil || s.client.ID == "" {
		return CloudToken{}, domain.ReauthRequired(s.client.Provider)
	}

	t, ok, err := s.store.LoadCloudToken(ctx, s.client.Provider)
	if err != nil {
		return CloudToken{}, fmt.Errorf("load cloud token: %w", LocalError{Err: err})
	}

	if !ok || t.ClientID != s.client.ID {
		return CloudToken{}, domain.ReauthRequired(s.client.Provider)
	}

	return t, nil
}

// Update saves t as the token of the source's provider and client. The write
// does not depend on the operation's deadline: the refresh already happened.
func (s *storedTokenSource) Update(ctx context.Context, t CloudToken) error {
	if s.store == nil {
		return domain.ReauthRequired(s.client.Provider)
	}

	t.Provider, t.ClientID = s.client.Provider, s.client.ID

	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), localWriteTimeout)
	defer cancel()

	if err := s.store.SaveCloudToken(lctx, t); err != nil {
		return fmt.Errorf("save cloud token: %w", LocalError{Err: err})
	}

	return nil
}

type memoryTokenSource struct {
	client CloudClient
	// updated, when set, gets every token Update keeps.
	updated func(CloudToken)

	mu  sync.Mutex
	tok CloudToken
}

// NewMemoryTokenSource returns a source that keeps t, issued for c, in
// memory; Update replaces it there.
func NewMemoryTokenSource(t CloudToken, c CloudClient) CloudTokenSource {
	return &memoryTokenSource{tok: t, client: c}
}

func (m *memoryTokenSource) Token(context.Context) (CloudToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tok.ClientID != m.client.ID {
		return CloudToken{}, domain.ReauthRequired(m.client.Provider)
	}

	return m.tok, nil
}

// Update keeps t as the token of the source's provider and client and
// reports it to updated.
func (m *memoryTokenSource) Update(_ context.Context, t CloudToken) error {
	t.Provider, t.ClientID = m.client.Provider, m.client.ID

	m.mu.Lock()
	m.tok = t
	m.mu.Unlock()

	if m.updated != nil {
		m.updated(t)
	}

	return nil
}
