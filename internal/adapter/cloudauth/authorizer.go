// Package cloudauth signs the user in to a cloud with OAuth 2.0 Authorization
// Code and PKCE S256, and refreshes the token (spec §5.6, §6). One Authorizer
// serves one provider; the providers differ only in the parameters of
// Provider, so there is no interface between them. The package imports no
// provider adapter.
package cloudauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	defaultAuthTimeout = 5 * time.Minute  // the wait for the browser (spec §6.2)
	exchangeTimeout    = 60 * time.Second // the code exchange and the refresh
	shutdownTimeout    = 2 * time.Second  // a graceful stop of the receiver, then a hard one
	secretBytes        = 32               // of state and of code_verifier

	grantAuthorizationCode = "authorization_code"
	grantRefreshToken      = "refresh_token"
	responseTypeCode       = "code" // the authorize page redirects with a code
	errorAccessDenied      = "access_denied"

	// Parameters of the OAuth requests.
	paramGrantType    = "grant_type"
	paramClientID     = "client_id"
	paramClientSecret = "client_secret"
	paramCode         = "code"
	paramRedirectURI  = "redirect_uri"
	paramRefreshToken = "refresh_token"
)

// Provider is what differs between the clouds: the endpoints and the way the
// browser comes back.
type Provider struct {
	Kind domain.StorageKind // the cloud; it marks tokens and errors
	Name string             // of the cloud in the texts of the browser pages, e.g. "Yandex Disk"

	// Issuer is the name of the service that signs the user in, for the text
	// of a failed sign-in; Name when empty.
	Issuer string

	AuthorizeURL string // without parameters
	TokenURL     string // https only
	Scope        string // sent as "scope" when not empty

	LoopbackAddr string // of the receiver, "127.0.0.1:47123"; port 0 takes a free one
	CallbackPath string // of the receiver, "/callback"

	// ManualRedirect is the page of the provider that shows the code to
	// copy; empty when the provider has no sign-in with a code.
	ManualRedirect string

	// Extra are more parameters of the authorize URL, e.g. access_type. The
	// parameters of the protocol are never replaced by them. When it holds
	// access_type=offline the exchange must return a refresh token.
	Extra url.Values
}

// Options are the seams of an Authorizer; zero values take the defaults.
type Options struct {
	HTTP    *http.Client                                     // base client; redirects are never followed
	Listen  func(network, addr string) (net.Listener, error) // default net.Listen
	Rand    io.Reader                                        // default crypto/rand
	Timeout time.Duration                                    // the wait for the browser, default 5 minutes

	// Lang gives the language of the browser pages at the moment they are
	// shown; default English.
	Lang func() i18n.Lang
}

// Authorizer signs in to one provider. It implements usecase.CloudAuthorizer
// and refreshes tokens. Of the client it uses the ID, and the secret when it
// is not empty.
type Authorizer struct {
	p       Provider
	http    *http.Client
	listen  func(network, addr string) (net.Listener, error)
	rand    io.Reader
	timeout time.Duration
	lang    func() i18n.Lang
}

var _ usecase.CloudAuthorizer = (*Authorizer)(nil)

// NewAuthorizer returns an Authorizer for p.
func NewAuthorizer(p Provider, o Options) *Authorizer {
	a := &Authorizer{
		p:       p,
		http:    cloudhttp.WithRedirects(o.HTTP, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }),
		listen:  o.Listen,
		rand:    o.Rand,
		timeout: o.Timeout,
		lang:    o.Lang,
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

	if a.lang == nil {
		a.lang = func() i18n.Lang { return i18n.EN }
	}

	return a
}

// loopbackUnavailable is the error of a receiver that did not start; it says
// whether the provider has a sign-in with a code to fall back to.
func (a *Authorizer) loopbackUnavailable(cause error) error {
	return &domain.LoopbackUnavailableError{Kind: a.p.Kind, ManualCode: a.p.ManualRedirect != "", Err: cause}
}

// BeginLoopback starts the receiver on LoopbackAddr and returns the login
// with the redirect URI of the receiver, with its real port when the address
// asks for any. A busy port, or a listener that is not on a loopback address,
// is domain.ErrLoopbackUnavailable.
func (a *Authorizer) BeginLoopback(_ context.Context, c usecase.CloudClient) (usecase.CloudLogin, error) {
	deadline := time.Now().Add(a.timeout)

	ln, err := a.listen("tcp", a.p.LoopbackAddr)
	if err != nil {
		return nil, fmt.Errorf("start the sign-in receiver: %w", a.loopbackUnavailable(err))
	}

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		_ = ln.Close()
		return nil, fmt.Errorf("start the sign-in receiver: %w", a.loopbackUnavailable(nil))
	}

	redirect := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)) + a.p.CallbackPath

	s, err := a.newSession(c, redirect)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}

	return startLoopback(s, &onceCloseListener{Listener: ln}, deadline), nil
}

// BeginManual returns a login whose browser page shows a code to paste. A
// provider without ManualRedirect gives domain.ErrManualUnsupported.
func (a *Authorizer) BeginManual(_ context.Context, c usecase.CloudClient) (usecase.CloudLogin, error) {
	if a.p.ManualRedirect == "" {
		return nil, fmt.Errorf("begin the sign-in with a code: %w", domain.ErrManualUnsupported)
	}

	s, err := a.newSession(c, a.p.ManualRedirect)
	if err != nil {
		return nil, err
	}

	return &manualLogin{session: s}, nil
}

// Refresh exchanges refreshToken for a new access token. The result is a
// full token, as CloudTokenSource.Update stores it as given: Refresh is the
// token the provider returned, or refreshToken itself when the answer has
// none. Account is empty, because the answer has no account and the call has
// no old token to take it from; the caller keeps the account it has. A
// refused grant or client (invalid_grant, invalid_client,
// unauthorized_client) is domain.ReauthRequired of the provider; other
// failures are cloudhttp.CloudError or the error of ctx.
func (a *Authorizer) Refresh(ctx context.Context, c usecase.CloudClient, refreshToken string) (usecase.CloudToken, error) {
	form := url.Values{
		paramGrantType:    {grantRefreshToken},
		paramRefreshToken: {refreshToken},
		paramClientID:     {c.ID},
	}

	if c.Secret != "" {
		form.Set(paramClientSecret, c.Secret)
	}

	answer, err := a.postToken(ctx, form, "refresh the token")
	if err != nil {
		if ce, ok := errors.AsType[cloudhttp.CloudError](err); ok && refused(ce.Code) {
			return usecase.CloudToken{}, fmt.Errorf("refresh the token: %w", domain.ReauthRequired(a.p.Kind))
		}

		return usecase.CloudToken{}, err
	}

	tok := usecase.CloudToken{
		Provider: a.p.Kind,
		ClientID: c.ID,
		Access:   answer.AccessToken,
		Refresh:  answer.RefreshToken,
		Expiry:   answer.expiry(time.Now()),
	}

	if tok.Refresh == "" {
		tok.Refresh = refreshToken
	}

	return tok, nil
}

// refused reports whether the OAuth error says the grant or the client is
// no longer accepted, so only a new sign-in helps.
func refused(code string) bool {
	return code == codeInvalidGrant || code == codeInvalidClient || code == codeUnauthorizedClient
}

// session is what both kinds of login share: the PKCE verifier, the state
// and the authorize URL that carries them.
type session struct {
	a        *Authorizer
	client   usecase.CloudClient
	redirect string
	verifier string
	state    string
	url      string
}

func (a *Authorizer) newSession(c usecase.CloudClient, redirect string) (session, error) {
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
		paramClientID:           {c.ID},
		paramRedirectURI:        {redirect},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}

	if a.p.Scope != "" {
		q.Set("scope", a.p.Scope)
	}

	for k, v := range a.p.Extra {
		if !q.Has(k) {
			q[k] = append([]string(nil), v...)
		}
	}

	return session{a: a, client: c, redirect: redirect, verifier: verifier, state: state, url: a.p.AuthorizeURL + "?" + q.Encode()}, nil
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

// redeem exchanges code for the token. When ctx ends first the error is
// ctx's, not a network one. A provider that is asked for offline access must
// return a refresh token, or the token would die within the hour.
func (s *session) redeem(ctx context.Context, code string) (usecase.CloudToken, error) {
	form := url.Values{
		paramGrantType:  {grantAuthorizationCode},
		paramCode:       {code},
		paramClientID:   {s.client.ID},
		"code_verifier": {s.verifier},
	}

	if s.client.Secret != "" {
		form.Set(paramClientSecret, s.client.Secret)
		form.Set(paramRedirectURI, s.redirect)
	}

	answer, err := s.a.postToken(ctx, form, "exchange the code")
	if err != nil {
		return usecase.CloudToken{}, err
	}

	if answer.RefreshToken == "" && s.a.p.Extra.Get("access_type") == "offline" {
		return usecase.CloudToken{}, fmt.Errorf("exchange the code: %w", s.a.answerError(http.StatusOK, errNoRefresh))
	}

	return usecase.CloudToken{
		Provider: s.a.p.Kind,
		ClientID: s.client.ID,
		Access:   answer.AccessToken,
		Refresh:  answer.RefreshToken,
		Expiry:   answer.expiry(time.Now()),
	}, nil
}
