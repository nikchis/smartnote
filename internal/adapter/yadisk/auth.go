package yadisk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	authorizePath = "/authorize"
	callbackPath  = "/callback"
	tokenPath     = "/token"

	// manualRedirect is the page of Yandex that shows the code to copy.
	manualRedirect = "https://oauth.yandex.ru/verification_code"

	defaultAuthTimeout = 5 * time.Minute  // the wait for the browser (spec §6.2)
	exchangeTimeout    = 60 * time.Second // the code exchange
	shutdownTimeout    = 2 * time.Second  // a graceful stop of the receiver, then a hard one
	secretBytes        = 32               // of state and of code_verifier

	grantAuthorizationCode = "authorization_code"
	responseTypeCode       = "code" // the authorize page redirects with a code
	errorAccessDenied      = "access_denied"
)

// AuthOptions are the seams of an Authorizer; zero values take the defaults.
type AuthOptions struct {
	HTTP    *http.Client                                     // base client; redirects are never followed
	Listen  func(network, addr string) (net.Listener, error) // default net.Listen
	Rand    io.Reader                                        // default crypto/rand
	Timeout time.Duration                                    // the wait for the browser, default 5 minutes
}

// Authorizer signs in to Yandex ID with OAuth 2.0 Authorization Code and
// PKCE S256 (spec §6.2, §6.3). It implements usecase.CloudAuthorizer.
type Authorizer struct {
	authorize string    // the authorize URL without parameters
	token     string    // the token endpoint
	api       apiClient // its client exchanges codes
	listen    func(network, addr string) (net.Listener, error)
	rand      io.Reader
	timeout   time.Duration
}

var _ usecase.CloudAuthorizer = (*Authorizer)(nil)

// NewAuthorizer returns an Authorizer for the endpoints e.
func NewAuthorizer(e Endpoints, opts AuthOptions) *Authorizer {
	oauth := strings.TrimSuffix(e.OAuth, "/")

	a := &Authorizer{
		authorize: oauth + authorizePath,
		token:     oauth + tokenPath,
		api:       newAPIClient(opts.HTTP, e.API),
		listen:    opts.Listen,
		rand:      opts.Rand,
		timeout:   opts.Timeout,
	}

	if a.listen == nil {
		a.listen = net.Listen
	}

	if a.rand == nil {
		a.rand = rand.Reader
	}

	if a.timeout <= 0 {
		a.timeout = defaultAuthTimeout
	}

	return a
}

// BeginLoopback starts the receiver on LoopbackAddr and returns the login
// with the redirect URI of the receiver. A busy port, or a listener that is
// not on a loopback address, is domain.ErrLoopbackUnavailable.
func (a *Authorizer) BeginLoopback(_ context.Context, clientID string) (usecase.CloudLogin, error) {
	deadline := time.Now().Add(a.timeout)

	ln, err := a.listen("tcp", LoopbackAddr)
	if err != nil {
		return nil, fmt.Errorf("start the sign-in receiver: %w: %w", domain.ErrLoopbackUnavailable, err)
	}

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("start the sign-in receiver: %w", domain.ErrLoopbackUnavailable)
	}

	redirect := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)) + callbackPath

	s, err := a.newSession(clientID, redirect)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}

	return startLoopback(s, &onceCloseListener{Listener: ln}, deadline), nil
}

// BeginManual returns a login whose browser page shows a code to paste.
func (a *Authorizer) BeginManual(_ context.Context, clientID string) (usecase.CloudLogin, error) {
	s, err := a.newSession(clientID, manualRedirect)
	if err != nil {
		return nil, err
	}

	return &manualLogin{session: s}, nil
}

// session is what both kinds of login share: the PKCE verifier, the state
// and the authorize URL that carries them.
type session struct {
	a        *Authorizer
	clientID string
	verifier string
	state    string
	url      string
}

func (a *Authorizer) newSession(clientID, redirect string) (session, error) {
	state, err := a.secret()
	if err != nil {
		return session{}, fmt.Errorf("generate state: %w", err)
	}

	verifier, err := a.secret()
	if err != nil {
		return session{}, fmt.Errorf("generate code verifier: %w", err)
	}

	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {responseTypeCode},
		paramClientID:           {clientID},
		"redirect_uri":          {redirect},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}

	return session{a: a, clientID: clientID, verifier: verifier, state: state, url: a.authorize + "?" + q.Encode()}, nil
}

// secret returns secretBytes random bytes in base64url.
func (a *Authorizer) secret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := io.ReadFull(a.rand, b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// URL returns the authorize URL to open in the browser.
func (s *session) URL() string { return s.url }

// redeem exchanges code for the token within exchangeTimeout. When ctx ends
// first the error is ctx's, not a network one.
func (s *session) redeem(ctx context.Context, code string) (usecase.CloudToken, error) {
	opCtx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()

	form := url.Values{
		paramGrantType:  {grantAuthorizationCode},
		paramCode:       {code},
		paramClientID:   {s.clientID},
		"code_verifier": {s.verifier},
	}

	answer, err := postToken(opCtx, s.a.api.http, s.a.token, form)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return usecase.CloudToken{}, fmt.Errorf("exchange the code: %w", ctxErr)
		}

		return usecase.CloudToken{}, fmt.Errorf("exchange the code: %w", err)
	}

	return usecase.CloudToken{
		Provider: string(domain.KindYandexDisk),
		ClientID: s.clientID,
		Access:   answer.AccessToken,
		Expiry:   answer.expiry(time.Now()),
	}, nil
}

// manualLogin ends with the code the user pastes (spec §6.3). Yandex does
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

// callback is the outcome of the first callback with the right state.
type callback struct {
	code string
	err  error
}

// loopbackLogin ends with the browser's redirect to its receiver (spec §6.2).
// It owns the goroutine that serves the receiver: Close and the end of Wait
// stop the server and wait for that goroutine.
type loopbackLogin struct {
	session

	deadline time.Time // of the wait for the browser
	srv      *http.Server
	ln       net.Listener
	served   chan struct{} // closed when Serve returns
	result   chan callback // the first right callback; buffered, sent once

	// life is the lifetime of the login, not of a request: Close ends it,
	// which cancels a running Wait together with its exchange.
	life context.Context
	end  context.CancelFunc

	answered atomic.Bool // the right callback has been taken
	waited   atomic.Bool // Wait has been called
	stopOnce sync.Once
}

func startLoopback(s session, ln net.Listener, deadline time.Time) *loopbackLogin {
	l := &loopbackLogin{
		session:  s,
		deadline: deadline,
		ln:       ln,
		served:   make(chan struct{}),
		result:   make(chan callback, 1),
	}
	l.life, l.end = context.WithCancel(context.Background())
	l.srv = &http.Server{
		Handler:           l,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0), // its messages may quote requests
	}

	go func() {
		defer close(l.served)

		_ = l.srv.Serve(l.ln) // ends with stop, or when the handler closes the listener
	}()

	return l
}

// Wait waits for the callback, at most until the deadline counted from
// BeginLoopback, then exchanges the code. A refusal in the browser and a
// Close return context.Canceled. Only the first Wait waits; later ones
// return domain.ErrNoPendingOperation. The receiver is stopped when Wait
// returns.
func (l *loopbackLogin) Wait(ctx context.Context) (usecase.CloudToken, error) {
	if l.life.Err() != nil {
		return usecase.CloudToken{}, context.Canceled
	}

	if !l.waited.CompareAndSwap(false, true) {
		return usecase.CloudToken{}, fmt.Errorf("wait for the browser: %w", domain.ErrNoPendingOperation)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopAfter := context.AfterFunc(l.life, cancel)
	defer stopAfter()

	cb, err := l.receive(ctx)
	l.stop()

	switch {
	case err != nil:
		return usecase.CloudToken{}, err
	case cb.err != nil:
		return usecase.CloudToken{}, cb.err
	}

	tok, err := l.redeem(ctx, cb.code)
	if l.life.Err() != nil {
		// Closed during the exchange: even a finished token is dropped.
		return usecase.CloudToken{}, context.Canceled
	}

	return tok, err
}

// receive returns the right callback, or the error of ctx or of the deadline.
func (l *loopbackLogin) receive(ctx context.Context) (callback, error) {
	wait, cancel := context.WithDeadline(ctx, l.deadline)
	defer cancel()

	select {
	case cb := <-l.result:
		return cb, nil
	case <-wait.Done():
		if l.life.Err() != nil {
			return callback{}, context.Canceled
		}

		return callback{}, fmt.Errorf("wait for the browser: %w", wait.Err())
	}
}

// Exchange is not part of a loopback login.
func (*loopbackLogin) Exchange(context.Context, string) (usecase.CloudToken, error) {
	return usecase.CloudToken{}, fmt.Errorf("exchange the code: %w", domain.ErrNoPendingOperation)
}

// Close cancels a running Wait and stops the receiver. It is idempotent.
func (l *loopbackLogin) Close() error {
	l.end()
	l.stop()

	return nil
}

// stop shuts the server down and waits for its goroutine; later calls wait
// for the first one to finish.
func (l *loopbackLogin) stop() {
	l.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := l.srv.Shutdown(ctx); err != nil {
			_ = l.srv.Close() // connections still busy after shutdownTimeout
		}

		<-l.served
	})
}

// ServeHTTP answers GET /callback. The state is checked first, in constant
// time, for every callback: a wrong or missing one is 400 and the wait goes
// on, so another page cannot end the sign-in (spec §6.2, §10). The first
// right callback with a code or an error ends it and closes the listener.
func (l *loopbackLogin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != callbackPath {
		pageNotFound.write(w, false)
		return
	}

	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(l.state)) != 1 {
		pageExpired.write(w, false)
		return
	}

	cb, ok := callbackOf(q)
	if !ok || !l.answered.CompareAndSwap(false, true) {
		pageExpired.write(w, false)
		return
	}

	l.result <- cb

	switch {
	case cb.err == nil:
		pageSignedIn.write(w, true)
	case errors.Is(cb.err, context.Canceled):
		pageCancelled.write(w, true)
	default:
		pageFailed.write(w, true)
	}

	_ = l.ln.Close() // no more connections; stop ends the open ones
}

// callbackOf reads the outcome of a callback with the right state: a code,
// a refusal (context.Canceled) or another OAuth error by its known name. It
// reports false for a callback with neither.
func callbackOf(q url.Values) (callback, bool) {
	switch name := q.Get("error"); {
	case name == errorAccessDenied:
		return callback{err: context.Canceled}, true
	case name != "":
		return callback{err: fmt.Errorf("sign-in refused: %w", CloudError{Code: knownCode(name)})}, true
	case q.Get(paramCode) != "":
		return callback{code: q.Get(paramCode)}, true
	}

	return callback{}, false
}

// onceCloseListener lets the handler and the server both close the
// listener; only the first Close reaches it.
type onceCloseListener struct {
	net.Listener

	once sync.Once
	err  error
}

func (l *onceCloseListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	return l.err
}
