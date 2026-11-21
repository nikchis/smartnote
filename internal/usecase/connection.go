package usecase

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/nikchis/smartnote/internal/domain"
)

// TestResult is what a connection test found; Empty means the server has no
// Smartnote storage yet.
type TestResult struct {
	Empty bool
}

// Connection loads, tests and saves the connection settings and the trusted
// server key, and signs in to the cloud storage. Test, Save, TrustHostKey and
// ResetHostKey enter the Gate shared with Sync, so no setting changes during an
// operation with the server; Load, Account and the sign-in methods do not.
//
// A sign-in gives a pending token that lives only in memory: Test uses it,
// and only Save writes it to local.db. The pending token and a Sign out are
// kept for each cloud provider apart. At most one sign-in is active; it runs
// in the caller's goroutine, and Connection starts none of its own.
type Connection struct {
	settings SettingsStore
	store    ConnectionStore
	tokens   CloudTokenStore
	remote   RemoteConnector
	auth     map[domain.StorageKind]CloudAuthorizer
	gate     *Gate
	offer    *HostKeyOffer
	changed  func()
	builtin  map[domain.StorageKind]CloudClient
	openURL  func(string) error

	// beginMu serializes ending the previous login and starting the next, so
	// the previous receiver is closed before the next one starts listening.
	beginMu sync.Mutex

	mu      sync.Mutex
	current *signIn // the active sign-in, or nil
	// signIns is the sign-in state of each provider since the settings form
	// opened; a missing entry has neither a pending token nor a Sign out.
	signIns map[domain.StorageKind]*signInState
	gen     uint64 // changes with every pending token and Sign out
}

// signInState is the sign-in state of one provider, guarded by Connection.mu.
type signInState struct {
	pending *CloudToken // replaced, never changed in place
	signOut bool        // the saved token is to be deleted by Save
}

// signIn is one active sign-in. Its fields are guarded by Connection.mu while
// it is current; once detached they are only read.
type signIn struct {
	client CloudClient
	manual bool
	login  CloudLogin // nil until Begin* returns
	cancel context.CancelFunc
	// exchanging is set while SubmitCode exchanges a code.
	exchanging bool
}

// stop cancels what s is waiting for and closes its login. s may be nil.
func (s *signIn) stop() {
	if s == nil {
		return
	}

	s.cancel()

	if s.login != nil {
		// Close is idempotent; a failure to stop an abandoned receiver
		// changes nothing for the caller.
		_ = s.login.Close()
	}
}

// ConnectionDeps are the dependencies of Connection. Auth, OpenURL and
// Builtin are optional: without the Auth of a provider signing in to it
// returns domain.ErrNotConnected, without OpenURL the browser is not opened,
// without the Builtin client of a provider an own client is required.
type ConnectionDeps struct {
	Settings SettingsStore
	Store    ConnectionStore
	Tokens   CloudTokenStore
	Remote   RemoteConnector
	Auth     map[domain.StorageKind]CloudAuthorizer
	// Gate and Offer are shared with Sync.
	Gate  *Gate
	Offer *HostKeyOffer
	// Changed runs after the settings or the trusted key are saved (main
	// connects it to Sync.ResetConnection).
	Changed func()
	Builtin map[domain.StorageKind]CloudClient
	OpenURL func(url string) error
}

// NewConnection returns a Connection over d.
func NewConnection(d ConnectionDeps) (*Connection, error) {
	switch {
	case d.Settings == nil:
		return nil, nilDependency("NewConnection", "settings")
	case d.Store == nil:
		return nil, nilDependency("NewConnection", "store")
	case d.Tokens == nil:
		return nil, nilDependency("NewConnection", "tokens")
	case d.Remote == nil:
		return nil, nilDependency("NewConnection", "remote")
	case d.Gate == nil:
		return nil, nilDependency("NewConnection", "gate")
	case d.Offer == nil:
		return nil, nilDependency("NewConnection", "offer")
	case d.Changed == nil:
		return nil, nilDependency("NewConnection", "changed")
	}

	return &Connection{
		settings: d.Settings, store: d.Store, tokens: d.Tokens, remote: d.Remote, auth: d.Auth,
		gate: d.Gate, offer: d.Offer, changed: d.Changed, builtin: d.Builtin, openURL: d.OpenURL,
	}, nil
}

// Load returns the saved settings.
func (c *Connection) Load(ctx context.Context) (domain.ConnectionSettings, error) {
	s, err := c.settings.LoadSettings(ctx)
	if err != nil {
		return domain.ConnectionSettings{}, fmt.Errorf("load settings: %w", err)
	}

	return s, nil
}

// Test opens a session with s in OpenCheck mode and closes it, without saving
// s or creating the schema. Empty settings return domain.ErrNotConnected. An
// unknown server key is remembered for TrustOfferedKey. A cloud test uses the
// pending token of its provider, or else the saved one; without either it is
// refused before Open (ValidationError for "yandex_account" or
// "google_account"). A refresh of the pending token during the test updates
// it for Save; a refresh of the saved one writes local.db.
func (c *Connection) Test(ctx context.Context, s domain.ConnectionSettings) (res TestResult, err error) {
	leave, err := c.gate.Enter()
	if err != nil {
		return TestResult{}, err
	}
	defer leave()

	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return TestResult{}, err
	}

	if s.IsEmpty() {
		return TestResult{}, fmt.Errorf("test connection: %w", domain.ErrNotConnected)
	}

	target := RemoteTarget{Settings: s}
	if domain.IsCloud(s.Kind()) {
		if target.Token, err = c.testTokenSource(ctx, s); err != nil {
			return TestResult{}, err
		}
	}

	sess, err := c.remote.Open(ctx, target, OpenCheck)
	if err != nil {
		c.offer.remember(err)
		return TestResult{}, fmt.Errorf("test connection: %w", err)
	}
	defer func() { err = joinCloseError(sess, err) }()

	return TestResult{Empty: sess.Empty()}, nil
}

// Save validates s and saves it with defaults, together with the token
// updates, in one transaction: the pending token of s's provider is saved,
// and a Sign out of any provider deletes its saved token, whatever the kind
// of s. A trusted key bound to another address than s's is dropped, so the
// user is asked again. Empty settings are allowed and switch the app to
// local-only work. Save ends an active sign-in. Cloud settings without a
// token for their client ID are refused with a ValidationError for
// "yandex_account" or "google_account".
func (c *Connection) Save(ctx context.Context, s domain.ConnectionSettings) error {
	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return err
	}

	c.CancelSignIn()

	return c.write(func() error {
		plan, err := c.tokenPlan(ctx, s)
		if err != nil {
			return err
		}

		if err := c.store.SaveConnection(ctx, s.WithoutStaleTrust(), plan.updates); err != nil {
			return fmt.Errorf("save connection: %w", err)
		}

		c.clearSignInState(plan.gen)

		return nil
	})
}

// TrustHostKey trusts, in the saved settings, the key with fingerprint that
// the server last presented as unknown. The key is bound to the address that
// presented it, which must still be the saved one.
func (c *Connection) TrustHostKey(ctx context.Context, fingerprint string) error {
	return c.write(func() error {
		s, err := c.settings.LoadSettings(ctx)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		offer, err := c.offer.take(fingerprint, s.SSHAddr())
		if err != nil {
			return err
		}

		s.TrustedHostKey, s.TrustedHostAddr = offer.AuthorizedKey, offer.Addr

		return c.save(ctx, s)
	})
}

// TrustOfferedKey returns s trusting the key with fingerprint that the server
// at s's address last presented as unknown, for the settings dialog, which
// saves it only on OK. Nothing is saved here.
func (c *Connection) TrustOfferedKey(fingerprint string, s domain.ConnectionSettings) (domain.ConnectionSettings, error) {
	// The dialog may leave the port empty; the key was offered for the default.
	offer, err := c.offer.take(fingerprint, s.WithDefaults().SSHAddr())
	if err != nil {
		return domain.ConnectionSettings{}, err
	}

	s.TrustedHostKey, s.TrustedHostAddr = offer.AuthorizedKey, offer.Addr

	return s, nil
}

// ResetHostKey clears the trusted server key in the saved settings.
func (c *Connection) ResetHostKey(ctx context.Context) error {
	return c.write(func() error {
		s, err := c.settings.LoadSettings(ctx)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		s.TrustedHostKey, s.TrustedHostAddr = "", ""

		return c.save(ctx, s)
	})
}

func (c *Connection) save(ctx context.Context, s domain.ConnectionSettings) error {
	if err := c.settings.SaveSettings(ctx, s); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}

	return nil
}

// write runs save inside the Gate and reports the change after a success.
func (c *Connection) write(save func() error) error {
	leave, err := c.gate.Enter()
	if err != nil {
		return err
	}
	defer leave()

	if err := save(); err != nil {
		return err
	}

	c.changed()

	return nil
}

// SignIn signs in through the browser with the loopback receiver and blocks
// until the token arrives, the sign-in is cancelled or ctx ends. The token
// becomes pending; nothing is saved. A previous sign-in is ended first. A
// sign-in ended by another one, CancelSignIn or Save returns context.Canceled.
func (c *Connection) SignIn(ctx context.Context, s domain.ConnectionSettings) (string, error) {
	sess, ctx, err := c.beginSignIn(ctx, s, false)
	if err != nil {
		return "", err
	}

	tok, err := sess.login.Wait(ctx)
	if err != nil {
		if !c.endSignIn(sess) {
			return "", context.Canceled
		}

		return "", err
	}

	return c.complete(sess, tok)
}

// SignInManual starts a sign-in with a code the user copies from the browser
// and returns; SubmitCode finishes it. A previous sign-in is ended first.
func (c *Connection) SignInManual(ctx context.Context, s domain.ConnectionSettings) error {
	_, _, err := c.beginSignIn(ctx, s, true)
	return err
}

// SubmitCode exchanges code for the token of the active SignInManual, which
// becomes pending. Without one it returns domain.ErrNoPendingOperation, and
// domain.ErrBusy while another SubmitCode exchanges a code. A rejected code
// keeps the sign-in for another try.
func (c *Connection) SubmitCode(ctx context.Context, code string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.mu.Lock()

	sess := c.current
	switch {
	case sess == nil || !sess.manual || sess.login == nil:
		c.mu.Unlock()
		return "", fmt.Errorf("submit code: %w", domain.ErrNoPendingOperation)
	case sess.exchanging:
		c.mu.Unlock()
		return "", fmt.Errorf("submit code: %w", domain.ErrBusy)
	}

	// CancelSignIn also cancels this exchange.
	prev := sess.cancel
	sess.cancel = func() { prev(); cancel() }
	sess.exchanging = true
	c.mu.Unlock()

	tok, err := sess.login.Exchange(ctx, code)
	if err != nil {
		if !c.endExchange(sess, prev) {
			return "", context.Canceled
		}

		return "", err
	}

	return c.complete(sess, tok)
}

// endExchange makes a still current sess ready for another code, with its
// cancel func restored to prev, and reports whether sess is still current.
func (c *Connection) endExchange(sess *signIn, prev context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current != sess {
		return false
	}

	sess.cancel, sess.exchanging = prev, false

	return true
}

// CancelSignIn ends the active sign-in, if any; its pending token, if one
// was already received, stays.
func (c *Connection) CancelSignIn() {
	c.mu.Lock()
	sess := c.current
	c.current = nil
	c.mu.Unlock()

	sess.stop()
}

// DiscardPending ends the active sign-in and forgets the pending tokens and
// the Sign outs of every provider: the saved tokens stay as they are (Cancel
// or closing Settings).
func (c *Connection) DiscardPending() {
	c.CancelSignIn()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.signIns = nil
	c.gen++
}

// SignOut ends the active sign-in to p, forgets the pending token of p and
// marks the saved token of p for deletion by Save. Other providers keep
// their state. p comes from the form: a kind that is not a cloud changes
// nothing.
func (c *Connection) SignOut(p domain.StorageKind) {
	if !domain.IsCloud(p) {
		return
	}

	c.mu.Lock()

	sess := c.current
	if sess != nil && sess.client.Provider == p {
		c.current = nil
	} else {
		sess = nil
	}

	st := c.stateLocked(p)
	st.pending, st.signOut = nil, true
	c.gen++
	c.mu.Unlock()

	sess.stop()
}

// Account returns the account of the token Save would keep for the kind and
// the effective client of the draft s: the pending one of that provider,
// else the saved one. After a Sign out of that provider, without a token for
// that client, or for a kind that is not a cloud, signedIn is false. The
// pending tokens and Sign outs stay as they are.
func (c *Connection) Account(ctx context.Context, s domain.ConnectionSettings) (account string, signedIn bool, err error) {
	client, ok := EffectiveClient(s, c.builtin)
	if !ok {
		return "", false, nil
	}

	c.mu.Lock()
	st := c.stateOf(client.Provider)
	c.mu.Unlock()

	switch {
	case st.signOut:
		return "", false, nil
	case st.pending != nil && st.pending.ClientID == client.ID:
		return st.pending.Account, true, nil
	}

	tok, ok, err := c.tokens.LoadCloudToken(ctx, client.Provider)
	if err != nil {
		return "", false, fmt.Errorf("load cloud token: %w", err)
	}

	if !ok || tok.ClientID != client.ID {
		return "", false, nil
	}

	return tok.Account, true, nil
}

// beginSignIn ends the previous sign-in, starts a new one with the
// authorizer of s's kind for the effective client of s and opens its URL. It
// returns the sign-in and its context, which CancelSignIn cancels.
func (c *Connection) beginSignIn(ctx context.Context, s domain.ConnectionSettings, manual bool) (*signIn, context.Context, error) {
	auth := c.auth[s.Kind()]
	if auth == nil {
		return nil, nil, fmt.Errorf("sign in: %w", domain.ErrNotConnected)
	}

	client, err := c.client(s)
	if err != nil {
		return nil, nil, err
	}

	sess, ctx, err := c.startLogin(ctx, auth, client, manual)
	if err != nil {
		return nil, nil, err
	}

	if !c.isCurrent(sess) {
		// Replaced or cancelled after the login started; it is stopped already.
		return nil, nil, context.Canceled
	}

	if c.openURL != nil {
		if err := c.openURL(sess.login.URL()); err != nil {
			c.endSignIn(sess)
			return nil, nil, fmt.Errorf("open browser: %w", err)
		}
	}

	return sess, ctx, nil
}

// startLogin makes a new sign-in current, ending the previous one, and starts
// its login. Errors of Begin* are returned as is.
func (c *Connection) startLogin(ctx context.Context, auth CloudAuthorizer, client CloudClient, manual bool) (*signIn, context.Context, error) {
	c.beginMu.Lock()
	defer c.beginMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	sess := &signIn{client: client, manual: manual, cancel: cancel}

	c.mu.Lock()
	prev := c.current
	c.current = sess
	c.mu.Unlock()

	prev.stop()

	begin := auth.BeginLoopback
	if manual {
		begin = auth.BeginManual
	}

	login, err := begin(ctx, client)
	if err != nil {
		c.endSignIn(sess)
		return nil, nil, err
	}

	c.mu.Lock()

	current := c.current == sess
	if current {
		sess.login = login
	}
	c.mu.Unlock()

	if !current {
		// Cancelled while starting: close the receiver before the next begins.
		_ = login.Close()

		cancel()

		return nil, nil, context.Canceled
	}

	return sess, ctx, nil
}

// complete makes tok, issued for sess's client, the pending token of its
// provider and ends sess. A sess that is no longer current returns
// context.Canceled.
func (c *Connection) complete(sess *signIn, tok CloudToken) (string, error) {
	tok.Provider, tok.ClientID = sess.client.Provider, sess.client.ID

	c.mu.Lock()
	if c.current != sess {
		c.mu.Unlock()
		return "", context.Canceled
	}

	c.current = nil
	st := c.stateLocked(tok.Provider)
	st.pending, st.signOut = &tok, false
	c.gen++
	c.mu.Unlock()

	sess.stop()

	return tok.Account, nil
}

// endSignIn ends sess and reports whether it was still current.
func (c *Connection) endSignIn(sess *signIn) bool {
	c.mu.Lock()

	current := c.current == sess
	if current {
		c.current = nil
	}
	c.mu.Unlock()

	sess.stop()

	return current
}

func (c *Connection) isCurrent(sess *signIn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.current == sess
}

// stateLocked returns the sign-in state of p for a change, creating it.
// c.mu must be held.
func (c *Connection) stateLocked(p domain.StorageKind) *signInState {
	if c.signIns == nil {
		c.signIns = map[domain.StorageKind]*signInState{}
	}

	st := c.signIns[p]
	if st == nil {
		st = &signInState{}
		c.signIns[p] = st
	}

	return st
}

// stateOf returns a copy of the sign-in state of p. c.mu must be held.
func (c *Connection) stateOf(p domain.StorageKind) signInState {
	if st := c.signIns[p]; st != nil {
		return *st
	}

	return signInState{}
}

// clearSignInState forgets the pending tokens and Sign outs after a Save,
// unless they changed since generation gen.
func (c *Connection) clearSignInState(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.gen == gen {
		c.signIns = nil
		c.gen++
	}
}

// refreshPending makes t, a refresh of a pending token during Test, the
// pending token of t.Provider. The refresh is dropped if the sign-in state
// changed since generation gen, which the source was made from, or the
// pending token is of another client: a newer sign-in or a Sign out wins.
func (c *Connection) refreshPending(t CloudToken, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.signIns[t.Provider]
	if c.gen != gen || st == nil || st.pending == nil || st.pending.ClientID != t.ClientID {
		return
	}

	st.pending = &t
}

// client checks the format of the own client of the cloud settings s and
// returns the effective client. Settings of another kind are a
// ValidationError for "kind".
func (c *Connection) client(s domain.ConnectionSettings) (CloudClient, error) {
	switch s.Kind() {
	case domain.KindYandexDisk:
		if err := domain.ValidateYandexClientID(s.YandexClientID); err != nil {
			return CloudClient{}, err
		}
	case domain.KindGoogleDrive:
		if err := domain.ValidateGoogleClient(s.GoogleClientID, s.GoogleClientSecret); err != nil {
			return CloudClient{}, err
		}
	default:
		return CloudClient{}, &domain.ValidationError{Field: "kind", Reason: "is not a cloud storage"}
	}

	cl, ok := EffectiveClient(s, c.builtin)
	if !ok {
		return CloudClient{}, errNoClient(s.Kind())
	}

	return cl, nil
}

// errSignInRequired refuses cloud settings of provider p without a token.
func errSignInRequired(p domain.StorageKind) error {
	field := "yandex_account"
	if p == domain.KindGoogleDrive {
		field = "google_account"
	}

	return &domain.ValidationError{Field: field, Reason: "sign in required"}
}

// dropOtherPendingLocked forgets a pending token of client's provider issued
// for another client. c.mu must be held.
func (c *Connection) dropOtherPendingLocked(client CloudClient) {
	if st := c.signIns[client.Provider]; st != nil && st.pending != nil && st.pending.ClientID != client.ID {
		st.pending = nil
		c.gen++
	}
}

// testTokenSource returns the token source Test uses for cloud settings s:
// the pending token of s's provider, or the saved token unless Sign out
// marked it for deletion. Without either for the effective client it returns
// a ValidationError for the account of the provider.
func (c *Connection) testTokenSource(ctx context.Context, s domain.ConnectionSettings) (CloudTokenSource, error) {
	client, err := c.client(s)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.dropOtherPendingLocked(client)
	st, gen := c.stateOf(client.Provider), c.gen
	c.mu.Unlock()

	switch {
	case st.pending != nil:
		return c.pendingSource(*st.pending, client, gen), nil
	case st.signOut:
		return nil, errSignInRequired(client.Provider)
	}

	if err := c.requireStoredToken(ctx, client); err != nil {
		return nil, err
	}

	return NewStoredTokenSource(c.tokens, client), nil
}

// requireStoredToken returns a ValidationError for the account of the
// provider unless local.db has a token of client.
func (c *Connection) requireStoredToken(ctx context.Context, client CloudClient) error {
	tok, ok, err := c.tokens.LoadCloudToken(ctx, client.Provider)
	if err != nil {
		return fmt.Errorf("load cloud token: %w", err)
	}

	if !ok || tok.ClientID != client.ID {
		return errSignInRequired(client.Provider)
	}

	return nil
}

// issuedFor reports whether tok was issued for the effective client of its
// provider in s, whatever the kind of s: switching the clouds keeps the
// client fields of the other one.
func (c *Connection) issuedFor(tok CloudToken, s domain.ConnectionSettings) bool {
	s.StorageKind = tok.Provider
	client, ok := EffectiveClient(s, c.builtin)

	return ok && client.ID == tok.ClientID
}

// pendingSource is the source of the pending token tok of client made at
// generation gen: Test uses it, and a refresh updates the pending token
// itself through refreshPending, never local.db.
func (c *Connection) pendingSource(tok CloudToken, client CloudClient, gen uint64) CloudTokenSource {
	return &memoryTokenSource{
		tok: tok, client: client,
		updated: func(t CloudToken) { c.refreshPending(t, gen) },
	}
}

// tokenPlan is what Save does with the saved tokens.
type tokenPlan struct {
	updates []TokenUpdate
	gen     uint64 // the sign-in state generation the plan was made from
}

// tokenPlan decides the token updates for saving s, ordered by provider:
// the pending token of any provider is set if it was issued for the
// effective client of that provider in s, and dropped otherwise; a Sign out
// of any provider deletes its token. Cloud settings with neither a pending
// token nor a Sign out need a saved token of their client.
func (c *Connection) tokenPlan(ctx context.Context, s domain.ConnectionSettings) (tokenPlan, error) {
	kind := s.Kind()
	cloud := domain.IsCloud(kind)

	var client CloudClient

	if cloud {
		var err error
		if client, err = c.client(s); err != nil {
			return tokenPlan{}, err
		}
	}

	c.mu.Lock()

	if cloud {
		c.dropOtherPendingLocked(client)
	}

	plan := tokenPlan{gen: c.gen}

	for _, p := range slices.Sorted(maps.Keys(c.signIns)) {
		switch st := c.signIns[p]; {
		case st.pending != nil && c.issuedFor(*st.pending, s):
			plan.updates = append(plan.updates, TokenUpdate{Provider: p, Change: TokenSet, Token: *st.pending})
		case st.signOut:
			// Sign out then OK: the token is deleted; the notes in the
			// storage stay.
			plan.updates = append(plan.updates, TokenUpdate{Provider: p, Change: TokenDelete})
		}
	}

	st := c.stateOf(kind)
	c.mu.Unlock()

	if !cloud || st.pending != nil || st.signOut {
		return plan, nil
	}

	if err := c.requireStoredToken(ctx, client); err != nil {
		return tokenPlan{}, err
	}

	return plan, nil
}
