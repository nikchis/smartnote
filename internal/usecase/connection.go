package usecase

import (
	"context"
	"fmt"
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
// and only Save writes it to local.db. At most one sign-in is active; it runs
// in the caller's goroutine, and Connection starts none of its own.
type Connection struct {
	settings SettingsStore
	store    ConnectionStore
	tokens   CloudTokenStore
	remote   RemoteConnector
	auth     CloudAuthorizer
	gate     *Gate
	offer    *HostKeyOffer
	changed  func()
	builtin  string
	openURL  func(string) error

	// beginMu serializes ending the previous login and starting the next, so
	// the previous receiver is closed before the next one starts listening.
	beginMu sync.Mutex

	mu      sync.Mutex
	current *signIn     // the active sign-in, or nil
	pending *CloudToken // replaced, never changed in place
	signOut bool        // the saved token is to be deleted by Save
	gen     uint64      // changes with pending and signOut, not with a refresh
}

// signIn is one active sign-in. Its fields are guarded by Connection.mu while
// it is current; once detached they are only read.
type signIn struct {
	clientID string
	manual   bool
	login    CloudLogin // nil until Begin* returns
	cancel   context.CancelFunc
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
// BuiltinClientID are optional: without Auth signing in returns
// domain.ErrNotConnected, without OpenURL the browser is not opened.
type ConnectionDeps struct {
	Settings SettingsStore
	Store    ConnectionStore
	Tokens   CloudTokenStore
	Remote   RemoteConnector
	Auth     CloudAuthorizer
	// Gate and Offer are shared with Sync.
	Gate  *Gate
	Offer *HostKeyOffer
	// Changed runs after the settings or the trusted key are saved (main
	// connects it to Sync.ResetConnection).
	Changed         func()
	BuiltinClientID string
	OpenURL         func(url string) error
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
		gate: d.Gate, offer: d.Offer, changed: d.Changed, builtin: d.BuiltinClientID, openURL: d.OpenURL,
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
// unknown server key is remembered for TrustOfferedKey. A Yandex Disk test
// uses the pending token, whose refresh stays in memory, or else the saved
// one; without either it is refused before Open (ValidationError for
// "yandex_account").
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
	if s.Kind() == domain.KindYandexDisk {
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
// change: a pending token is saved, Sign out deletes the saved one. A trusted
// key bound to another address than s's is dropped, so the user is asked
// again. Empty settings are allowed and switch the app to local-only work.
// Save ends an active sign-in. Yandex Disk settings without a token for their
// client ID are refused with a ValidationError for "yandex_account".
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

		if err := c.store.SaveConnection(ctx, s.WithoutStaleTrust(), plan.change, plan.tok); err != nil {
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

// DiscardPending ends the active sign-in and forgets the pending token and a
// Sign out: the saved token stays as it is (Cancel or closing Settings).
func (c *Connection) DiscardPending() {
	c.CancelSignIn()
	c.setSignInState(nil, false)
}

// SignOut ends the active sign-in, forgets the pending token and marks the
// saved token for deletion by Save.
func (c *Connection) SignOut() {
	c.CancelSignIn()
	c.setSignInState(nil, true)
}

// Account returns the account of the token Save would keep for the
// effective client ID of s: the pending one, else the saved one. After
// SignOut, or without a token for that client ID, signedIn is false.
func (c *Connection) Account(ctx context.Context, s domain.ConnectionSettings) (account string, signedIn bool, err error) {
	clientID := EffectiveClientID(s, c.builtin)

	c.mu.Lock()
	pending, signOut := c.pending, c.signOut
	c.mu.Unlock()

	switch {
	case signOut || clientID == "":
		return "", false, nil
	case pending != nil && pending.ClientID == clientID:
		return pending.Account, true, nil
	}

	tok, ok, err := c.tokens.LoadCloudToken(ctx)
	if err != nil {
		return "", false, fmt.Errorf("load cloud token: %w", err)
	}

	if !ok || tok.ClientID != clientID {
		return "", false, nil
	}

	return tok.Account, true, nil
}

// beginSignIn ends the previous sign-in, starts a new one for the effective
// client ID of s and opens its URL. It returns the sign-in and its context,
// which CancelSignIn cancels.
func (c *Connection) beginSignIn(ctx context.Context, s domain.ConnectionSettings, manual bool) (*signIn, context.Context, error) {
	if c.auth == nil {
		return nil, nil, fmt.Errorf("sign in: %w", domain.ErrNotConnected)
	}

	clientID, err := c.clientID(s)
	if err != nil {
		return nil, nil, err
	}

	sess, ctx, err := c.startLogin(ctx, clientID, manual)
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
func (c *Connection) startLogin(ctx context.Context, clientID string, manual bool) (*signIn, context.Context, error) {
	c.beginMu.Lock()
	defer c.beginMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	sess := &signIn{clientID: clientID, manual: manual, cancel: cancel}

	c.mu.Lock()
	prev := c.current
	c.current = sess
	c.mu.Unlock()

	prev.stop()

	begin := c.auth.BeginLoopback
	if manual {
		begin = c.auth.BeginManual
	}

	login, err := begin(ctx, clientID)
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

// complete makes tok, issued for sess's client ID, the pending token and
// ends sess. A sess that is no longer current returns context.Canceled.
func (c *Connection) complete(sess *signIn, tok CloudToken) (string, error) {
	tok.Provider, tok.ClientID = string(domain.KindYandexDisk), sess.clientID

	c.mu.Lock()
	if c.current != sess {
		c.mu.Unlock()
		return "", context.Canceled
	}

	c.current = nil
	c.pending, c.signOut = &tok, false
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

func (c *Connection) setSignInState(pending *CloudToken, signOut bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pending, c.signOut = pending, signOut
	c.gen++
}

// clearSignInState forgets the pending token and Sign out after a Save,
// unless they changed since generation gen.
func (c *Connection) clearSignInState(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.gen == gen {
		c.pending, c.signOut = nil, false
		c.gen++
	}
}

// clientID returns the effective client ID of s.
func (c *Connection) clientID(s domain.ConnectionSettings) (string, error) {
	id := EffectiveClientID(s, c.builtin)
	if id == "" {
		return "", &domain.ValidationError{Field: "yandex_client_id", Reason: "set your own client ID in Advanced"}
	}

	return id, nil
}

func errSignInRequired() error {
	return &domain.ValidationError{Field: "yandex_account", Reason: "sign in required"}
}

// dropOtherPendingLocked forgets a pending token of another client ID than
// clientID. c.mu must be held.
func (c *Connection) dropOtherPendingLocked(clientID string) {
	if c.pending != nil && c.pending.ClientID != clientID {
		c.pending = nil
		c.gen++
	}
}

// testTokenSource returns the token source Test uses for Yandex Disk
// settings s: the pending token, whose refresh replaces it in memory only,
// or the saved token unless Sign out marked it for deletion. Without either
// for the effective client ID it returns a ValidationError for "yandex_account".
func (c *Connection) testTokenSource(ctx context.Context, s domain.ConnectionSettings) (CloudTokenSource, error) {
	clientID, err := c.clientID(s)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.dropOtherPendingLocked(clientID)
	pending, signOut, gen := c.pending, c.signOut, c.gen
	c.mu.Unlock()

	switch {
	case pending != nil:
		return NewMemoryTokenSource(*pending, clientID, c.refreshPending(gen, *pending)), nil
	case signOut:
		return nil, errSignInRequired()
	}

	if err := c.requireStoredToken(ctx, clientID); err != nil {
		return nil, err
	}

	return NewStoredTokenSource(c.tokens, clientID), nil
}

// requireStoredToken returns a ValidationError for "yandex_account" unless
// local.db has a token for clientID.
func (c *Connection) requireStoredToken(ctx context.Context, clientID string) error {
	tok, ok, err := c.tokens.LoadCloudToken(ctx)
	if err != nil {
		return fmt.Errorf("load cloud token: %w", err)
	}

	if !ok || tok.ClientID != clientID {
		return errSignInRequired()
	}

	return nil
}

// refreshPending returns the callback that replaces the pending token of was
// with its refresh, unless the pending token changed since generation gen.
// The refresh keeps the Provider and ClientID of was.
func (c *Connection) refreshPending(gen uint64, was CloudToken) func(CloudToken) {
	return func(t CloudToken) {
		t.Provider, t.ClientID = was.Provider, was.ClientID

		c.mu.Lock()
		defer c.mu.Unlock()

		if c.gen == gen {
			c.pending = &t
		}
	}
}

// tokenPlan is what Save does with the saved token.
type tokenPlan struct {
	change TokenChange
	tok    CloudToken
	gen    uint64 // the sign-in state generation the plan was made from
}

// tokenPlan decides the token change for saving s. Other kinds than
// yandex_disk keep the saved token unless Sign out deletes it.
func (c *Connection) tokenPlan(ctx context.Context, s domain.ConnectionSettings) (tokenPlan, error) {
	if s.Kind() != domain.KindYandexDisk {
		c.mu.Lock()
		defer c.mu.Unlock()

		if c.signOut {
			return tokenPlan{change: TokenDelete, gen: c.gen}, nil
		}

		return tokenPlan{change: TokenKeep, gen: c.gen}, nil
	}

	clientID, err := c.clientID(s)
	if err != nil {
		return tokenPlan{}, err
	}

	c.mu.Lock()
	c.dropOtherPendingLocked(clientID)
	pending, signOut, gen := c.pending, c.signOut, c.gen
	c.mu.Unlock()

	switch {
	case pending != nil:
		return tokenPlan{change: TokenSet, tok: *pending, gen: gen}, nil
	case signOut:
		// Yandex Disk settings cannot be saved without an account.
		return tokenPlan{}, errSignInRequired()
	}

	if err := c.requireStoredToken(ctx, clientID); err != nil {
		return tokenPlan{}, err
	}

	return tokenPlan{change: TokenKeep, gen: gen}, nil
}
