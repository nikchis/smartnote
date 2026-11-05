package yadisk

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	verificationRedirect = "https://oauth.yandex.ru/verification_code"
	waitLimit            = 10 * time.Second // a Wait that takes longer fails the test
)

// authHarness is an Authorizer over the emulator with a real receiver on a
// free loopback port.
type authHarness struct {
	t    *testing.T
	disk *fakeDisk

	mu     sync.Mutex
	listen []string // network and address of each Listen call
}

func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()

	return &authHarness{t: t, disk: newFakeDisk(t, testNow)}
}

func (h *authHarness) options() AuthOptions {
	return AuthOptions{HTTP: h.disk.api.Client(), Listen: h.listenFree}
}

func (h *authHarness) listenFree(network, addr string) (net.Listener, error) {
	h.mu.Lock()
	h.listen = append(h.listen, network+" "+addr)
	h.mu.Unlock()

	return listenLocal(h.t, "127.0.0.1:0")
}

// listenLocal listens on addr for a test.
func listenLocal(t *testing.T, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(t.Context(), "tcp", addr)
}

func (h *authHarness) authorizer(opts AuthOptions) *Authorizer {
	return NewAuthorizer(h.disk.endpoints(), opts)
}

// loopback begins a loopback login with the harness options.
func (h *authHarness) loopback() *loginURL {
	h.t.Helper()

	return h.begin(h.authorizer(h.options()), false)
}

// begin starts a login with a and closes it at the end of the test.
func (h *authHarness) begin(a *Authorizer, manual bool) *loginURL {
	h.t.Helper()

	begin := a.BeginLoopback
	if manual {
		begin = a.BeginManual
	}

	l, err := begin(h.t.Context(), fakeClientID)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}

	h.t.Cleanup(func() { _ = l.Close() })

	return parseLogin(h.t, l)
}

// loginURL is a login with the parameters of its authorize URL.
type loginURL struct {
	usecase.CloudLogin

	base  *url.URL // the URL without its query
	query url.Values
}

func parseLogin(t *testing.T, l usecase.CloudLogin) *loginURL {
	t.Helper()

	u, err := url.Parse(l.URL())
	if err != nil {
		t.Fatalf("login URL does not parse: %v", err)
	}

	q := u.Query()
	u.RawQuery = ""

	return &loginURL{CloudLogin: l, base: u, query: q}
}

// callback sends a GET to the receiver at the login's redirect_uri with
// query and returns the status, the headers and the body.
func (l *loginURL) callback(t *testing.T, path string, query url.Values) (status int, header http.Header, body string) {
	t.Helper()

	status, header, body, err := l.request(t, http.MethodGet, path, query)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	return status, header, body
}

func (l *loginURL) request(t *testing.T, method, path string, query url.Values) (status int, header http.Header, body string, err error) {
	t.Helper()

	u, err := url.Parse(l.query.Get("redirect_uri"))
	if err != nil {
		t.Fatalf("redirect_uri does not parse: %v", err)
	}

	if path != "" {
		u.Path = path
	}

	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(t.Context(), method, u.String(), http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	c := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: waitLimit}

	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}

	return resp.StatusCode, resp.Header, string(raw), nil
}

// withState returns the callback query with the login's state and kv.
func (l *loginURL) withState(kv ...string) url.Values {
	q := url.Values{"state": {l.query.Get("state")}}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}

	return q
}

// wantClosed checks that the receiver accepts no connections.
func (l *loginURL) wantClosed(t *testing.T) {
	t.Helper()

	if status, _, _, err := l.request(t, http.MethodGet, "", l.withState("code", fakeAuthCode)); err == nil {
		t.Errorf("receiver answered %d after it should be closed", status)
	}
}

type waitResult struct {
	tok usecase.CloudToken
	err error
}

// waitAsync runs Wait in a goroutine; receive the result with got.
func waitAsync(ctx context.Context, l usecase.CloudLogin) <-chan waitResult {
	ch := make(chan waitResult, 1)

	go func() {
		tok, err := l.Wait(ctx)
		ch <- waitResult{tok: tok, err: err}
	}()

	return ch
}

func got(t *testing.T, ch <-chan waitResult) waitResult {
	t.Helper()

	select {
	case r := <-ch:
		return r
	case <-time.After(waitLimit):
		t.Fatal("Wait did not return")
		return waitResult{}
	}
}

// stillWaiting fails the test if Wait has returned.
func stillWaiting(t *testing.T, ch <-chan waitResult) {
	t.Helper()

	select {
	case r := <-ch:
		t.Fatalf("Wait returned %v, %v; want it still waiting", r.tok, r.err)
	default:
	}
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// wantCodeForm checks the one token request: the authorization code grant
// with the verifier of challenge and no client secret.
func wantCodeForm(t *testing.T, d *fakeDisk, challenge string) {
	t.Helper()

	forms := d.forms()
	if len(forms) != 1 {
		t.Fatalf("token requests = %d, want 1", len(forms))
	}

	f := forms[0]
	if f.Get("grant_type") != "authorization_code" || f.Get("code") != fakeAuthCode || f.Get("client_id") != fakeClientID {
		t.Errorf("token form = %v", f)
	}

	if f.Has("client_secret") {
		t.Error("token form has client_secret")
	}

	if v := f.Get("code_verifier"); v == "" || s256(v) != challenge {
		t.Errorf("code_verifier %q does not match code_challenge %q", v, challenge)
	}
}

func wantToken(t *testing.T, tok usecase.CloudToken, before time.Time) {
	t.Helper()

	if tok.Provider != fakeProvider || tok.ClientID != fakeClientID || tok.Access != "acc-2" || tok.Refresh != "" || tok.Account != "" {
		t.Errorf("token = %s, want acc-2 without a refresh token and account", describeToken(tok))
	}

	if lo, hi := before.Add(time.Hour), time.Now().Add(time.Hour); tok.Expiry.Before(lo) || tok.Expiry.After(hi) {
		t.Errorf("expiry = %v, want between %v and %v", tok.Expiry, lo, hi)
	}
}

// wantNoSecrets checks that err does not quote the login's secrets or the
// description of the answer.
func wantNoSecrets(t *testing.T, err error, l *loginURL) {
	t.Helper()

	text := err.Error()
	for _, s := range []string{l.query.Get("state"), l.query.Get("code_challenge"), fakeAuthCode, fakeDescription, "acc-", "ref-"} {
		if strings.Contains(text, s) {
			t.Errorf("error %q quotes %q", text, s)
		}
	}
}

func TestLoopbackURL(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	if got, want := l.base.String(), h.disk.oauth.URL+"/authorize"; got != want {
		t.Errorf("authorize URL = %s, want %s", got, want)
	}

	h.mu.Lock()
	calls := h.listen
	h.mu.Unlock()

	if len(calls) != 1 || calls[0] != "tcp "+LoopbackAddr {
		t.Errorf("Listen calls = %v, want tcp %s", calls, LoopbackAddr)
	}

	q := l.query
	for k, want := range map[string]string{"response_type": "code", "client_id": fakeClientID, "code_challenge_method": "S256"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}

	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Path != "/callback" {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", q.Get("redirect_uri"))
	}

	for _, k := range []string{"state", "code_challenge"} {
		raw, err := base64.RawURLEncoding.DecodeString(q.Get(k))
		if err != nil || len(raw) != 32 {
			t.Errorf("%s = %q, want 32 bytes in base64url", k, q.Get(k))
		}
	}

	if other := h.begin(h.authorizer(h.options()), false); other.query.Get("state") == q.Get("state") {
		t.Error("two logins have one state")
	}

	t.Run("client ID is escaped", func(t *testing.T) {
		a := h.authorizer(h.options())

		l, err := a.BeginManual(t.Context(), "a&b=c d")
		if err != nil {
			t.Fatalf("BeginManual: %v", err)
		}
		defer l.Close()

		p := parseLogin(t, l)
		if p.query.Get("client_id") != "a&b=c d" || p.query.Has("b") {
			t.Errorf("query = %v, want client_id %q only", p.query, "a&b=c d")
		}
	})
}

func TestLoopbackRandFailure(t *testing.T) {
	h := newAuthHarness(t)
	opts := h.options()
	opts.Rand = strings.NewReader("short")

	for _, manual := range []bool{false, true} {
		a := h.authorizer(opts)

		begin := a.BeginLoopback
		if manual {
			begin = a.BeginManual
		}

		if l, err := begin(t.Context(), fakeClientID); err == nil {
			_ = l.Close()

			t.Errorf("manual %v: begin with a failing Rand succeeded", manual)
		}
	}
}

func TestLoopbackSuccess(t *testing.T) {
	h := newAuthHarness(t)
	before := time.Now()
	l := h.loopback()
	ch := waitAsync(t.Context(), l)

	status, header, body := l.callback(t, "", l.withState("code", fakeAuthCode))
	if status != http.StatusOK || !strings.Contains(body, "You can close this tab") || strings.Contains(strings.ToLower(body), "<script") {
		t.Errorf("callback answer = %d %q", status, body)
	}

	for k, want := range map[string]string{
		"Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
	} {
		if header.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, header.Get(k), want)
		}
	}

	r := got(t, ch)
	if r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}

	wantToken(t, r.tok, before)
	wantCodeForm(t, h.disk, l.query.Get("code_challenge"))
	l.wantClosed(t)
}

func TestLoopbackWrongState(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()
	ch := waitAsync(t.Context(), l)

	if status, _, _ := l.callback(t, "", url.Values{"state": {"foreign"}, "code": {"stolen"}}); status != http.StatusBadRequest {
		t.Errorf("wrong state answered %d, want 400", status)
	}

	stillWaiting(t, ch)

	if status, _, _ := l.callback(t, "", l.withState("code", fakeAuthCode)); status != http.StatusOK {
		t.Errorf("right callback answered %d, want 200", status)
	}

	if r := got(t, ch); r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}

	wantCodeForm(t, h.disk, l.query.Get("code_challenge"))
}

func TestLoopbackMissingState(t *testing.T) {
	cases := []struct {
		name  string
		query url.Values
	}{
		{name: "code without state", query: url.Values{"code": {fakeAuthCode}}},
		{name: "empty state", query: url.Values{"state": {""}, "code": {fakeAuthCode}}},
		{name: "error without state", query: url.Values{"error": {"server_error"}}},
		{name: "state without code or error", query: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAuthHarness(t)
			l := h.loopback()
			ch := waitAsync(t.Context(), l)

			q := tc.query
			if q == nil {
				q = l.withState()
			}

			if status, _, _ := l.callback(t, "", q); status != http.StatusBadRequest {
				t.Errorf("answered %d, want 400", status)
			}

			stillWaiting(t, ch)

			if err := l.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}

			if r := got(t, ch); !errors.Is(r.err, context.Canceled) {
				t.Errorf("Wait after Close = %v, want context.Canceled", r.err)
			}

			if n := len(h.disk.forms()); n != 0 {
				t.Errorf("token requests = %d, want none", n)
			}
		})
	}
}

func TestAccessDeniedForeignState(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()
	ch := waitAsync(t.Context(), l)

	if status, _, _ := l.callback(t, "", url.Values{"state": {"foreign"}, "error": {"access_denied"}}); status != http.StatusBadRequest {
		t.Errorf("foreign access_denied answered %d, want 400", status)
	}

	stillWaiting(t, ch)
	l.callback(t, "", l.withState("code", fakeAuthCode))

	if r := got(t, ch); r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}
}

func TestAccessDeniedOwnState(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()
	ch := waitAsync(t.Context(), l)

	if status, _, body := l.callback(t, "", l.withState("error", "access_denied", "error_description", fakeDescription)); status != http.StatusOK ||
		strings.Contains(body, fakeDescription) {
		t.Errorf("access_denied answered %d %q", status, body)
	}

	r := got(t, ch)
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", r.err)
	}

	if n := len(h.disk.forms()); n != 0 {
		t.Errorf("token requests = %d, want none", n)
	}

	l.wantClosed(t)
}

func TestOtherOAuthError(t *testing.T) {
	cases := []struct{ name, error, wantCode string }{
		{name: "known name", error: "server_error", wantCode: "server_error"},
		{name: "invalid scope", error: "invalid_scope", wantCode: "invalid_scope"},
		{name: "unknown name", error: "something " + fakeDescription, wantCode: codeOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAuthHarness(t)
			l := h.loopback()
			ch := waitAsync(t.Context(), l)

			l.callback(t, "", l.withState("error", tc.error, "error_description", fakeDescription))

			r := got(t, ch)
			if ce := cloudError(t, r.err); ce.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", ce.Code, tc.wantCode)
			}

			if errors.Is(r.err, context.Canceled) {
				t.Error("another error reads as a cancelled sign-in")
			}

			wantNoSecrets(t, r.err, l)
			l.wantClosed(t)
		})
	}
}

func TestLoopbackOnlyCallbackPath(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()
	ch := waitAsync(t.Context(), l)

	cases := []struct{ name, method, path string }{
		{name: "root", method: http.MethodGet, path: "/"},
		{name: "favicon", method: http.MethodGet, path: "/favicon.ico"},
		{name: "below callback", method: http.MethodGet, path: "/callback/x"},
		{name: "POST callback", method: http.MethodPost, path: "/callback"},
	}

	for _, tc := range cases {
		status, _, _, err := l.request(t, tc.method, tc.path, l.withState("code", fakeAuthCode))
		if err != nil || status != http.StatusNotFound {
			t.Errorf("%s answered %d, %v; want 404", tc.name, status, err)
		}
	}

	stillWaiting(t, ch)

	if n := len(h.disk.forms()); n != 0 {
		t.Errorf("token requests = %d, want none", n)
	}

	_ = l.Close()

	if r := got(t, ch); !errors.Is(r.err, context.Canceled) {
		t.Errorf("Wait after Close = %v, want context.Canceled", r.err)
	}
}

func TestLoopbackTimeout(t *testing.T) {
	h := newAuthHarness(t)
	opts := h.options()
	opts.Timeout = 50 * time.Millisecond
	l := h.begin(h.authorizer(opts), false)

	r := got(t, waitAsync(t.Context(), l))
	if !errors.Is(r.err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want context.DeadlineExceeded", r.err)
	}

	l.wantClosed(t)

	if _, err := l.Wait(t.Context()); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("second Wait = %v, want ErrNoPendingOperation", err)
	}
}

func TestLoopbackCancel(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	ctx, cancel := context.WithCancel(t.Context())
	ch := waitAsync(ctx, l)

	stillWaiting(t, ch)
	cancel()

	if r := got(t, ch); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", r.err)
	}

	l.wantClosed(t)
}

func TestLoopbackExchange(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	if _, err := l.Exchange(t.Context(), fakeAuthCode); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("Exchange on a loopback login = %v, want ErrNoPendingOperation", err)
	}

	if n := len(h.disk.forms()); n != 0 {
		t.Errorf("token requests = %d, want none", n)
	}
}

func TestLoopbackPortBusy(t *testing.T) {
	h := newAuthHarness(t)

	busy, err := listenLocal(t, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer busy.Close()

	opts := h.options()
	opts.Listen = func(string, string) (net.Listener, error) { return listenLocal(t, busy.Addr().String()) }

	l, err := h.authorizer(opts).BeginLoopback(t.Context(), fakeClientID)
	if err == nil {
		_ = l.Close()
	}

	if !errors.Is(err, domain.ErrLoopbackUnavailable) {
		t.Errorf("BeginLoopback = %v, want ErrLoopbackUnavailable", err)
	}
}

func TestLoopbackNotLoopback(t *testing.T) {
	h := newAuthHarness(t)
	opts := h.options()
	opts.Listen = func(string, string) (net.Listener, error) { return listenLocal(t, "[::]:0") }

	l, err := h.authorizer(opts).BeginLoopback(t.Context(), fakeClientID)
	if err == nil {
		_ = l.Close()
	}

	if !errors.Is(err, domain.ErrLoopbackUnavailable) {
		t.Errorf("BeginLoopback on all addresses = %v, want ErrLoopbackUnavailable", err)
	}
}

func TestManualExchange(t *testing.T) {
	h := newAuthHarness(t)
	before := time.Now()
	l := h.begin(h.authorizer(h.options()), true)

	if got := l.query.Get("redirect_uri"); got != verificationRedirect {
		t.Errorf("redirect_uri = %q, want %q", got, verificationRedirect)
	}

	if l.query.Get("code_challenge_method") != "S256" || l.query.Get("code_challenge") == "" || l.query.Get("response_type") != "code" {
		t.Errorf("query = %v, want the PKCE parameters", l.query)
	}

	h.mu.Lock()
	calls := len(h.listen)
	h.mu.Unlock()

	if calls != 0 {
		t.Errorf("manual login listened %d times", calls)
	}

	if _, err := l.Wait(t.Context()); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("Wait on a manual login = %v, want ErrNoPendingOperation", err)
	}

	for _, code := range []string{"", " \t\n"} {
		_, err := l.Exchange(t.Context(), code)
		if ve, ok := errors.AsType[*domain.ValidationError](err); !ok || ve.Field != "code" {
			t.Errorf("Exchange(%q) = %v, want ValidationError of code", code, err)
		}
	}

	_, err := l.Exchange(t.Context(), "wrong")
	if ce := cloudError(t, err); ce.Status != http.StatusBadRequest || ce.Code != codeInvalidGrant {
		t.Errorf("rejected code = %+v, want 400 invalid_grant", ce)
	}

	wantNoSecrets(t, err, l)
	h.disk.set(func(d *fakeDisk) { d.tokenForms = nil })

	tok, err := l.Exchange(t.Context(), "  "+fakeAuthCode+"\n")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	wantToken(t, tok, before)
	wantCodeForm(t, h.disk, l.query.Get("code_challenge"))
}

// TestNoAccountRequest: the sign-in does not read the account (the
// app-folder permission forbids it) and keeps no refresh token.
func TestNoAccountRequest(t *testing.T) {
	h := newAuthHarness(t)
	l := h.begin(h.authorizer(h.options()), true)

	tok, err := l.Exchange(t.Context(), fakeAuthCode)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if tok.Account != "" || tok.Refresh != "" || tok.Access != "acc-2" {
		t.Errorf("token = %s, want acc-2 without account and refresh token", describeToken(tok))
	}

	if n := h.disk.count("api", http.MethodGet, "/v1/disk"); n != 0 {
		t.Errorf("account requests = %d, want 0", n)
	}
}

func TestCloseIdempotent(t *testing.T) {
	for _, manual := range []bool{false, true} {
		h := newAuthHarness(t)
		l := h.begin(h.authorizer(h.options()), manual)

		for i := range 2 {
			if err := l.Close(); err != nil {
				t.Errorf("manual %v: Close #%d = %v", manual, i+1, err)
			}
		}

		if manual {
			if _, err := l.Exchange(t.Context(), fakeAuthCode); !errors.Is(err, context.Canceled) {
				t.Errorf("Exchange after Close = %v, want context.Canceled", err)
			}

			continue
		}

		if _, err := l.Wait(t.Context()); !errors.Is(err, context.Canceled) {
			t.Errorf("Wait after Close = %v, want context.Canceled", err)
		}

		l.wantClosed(t)
	}
}

func TestCloseDuringExchange(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	// The token request is held until the login is closed.
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)

	var once sync.Once

	h.disk.set(func(d *fakeDisk) {
		d.hold = func(got string) {
			if got == epToken {
				once.Do(func() { close(started) })
				<-release
			}
		}
	})

	ch := waitAsync(t.Context(), l)
	l.callback(t, "", l.withState("code", fakeAuthCode))

	select {
	case <-started:
	case <-time.After(waitLimit):
		t.Fatal("the token request did not start")
	}

	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	r := got(t, ch)
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Wait = %s, %v; want context.Canceled", describeToken(r.tok), r.err)
	}

	if _, ok := errors.AsType[CloudError](r.err); ok {
		t.Errorf("a cancelled exchange reads as a Disk error: %v", r.err)
	}
}
