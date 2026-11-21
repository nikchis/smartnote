package yadisk

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudauth"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// fakeClient is the Yandex client of the tests.
var fakeClient = usecase.CloudClient{Provider: domain.KindYandexDisk, ID: fakeClientID}

const verificationRedirect = "https://oauth.yandex.ru/verification_code"

// yandexAuthorizer is an Authorizer with the Yandex provider over the
// emulator. listen records the address of the receiver and listens on a free
// loopback port.
func yandexAuthorizer(t *testing.T, d *fakeDisk, addrs *[]string) *cloudauth.Authorizer {
	t.Helper()

	return cloudauth.NewAuthorizer(oauthProvider(d.endpoints()), cloudauth.Options{
		HTTP: d.api.Client(),
		Listen: func(network, addr string) (net.Listener, error) {
			*addrs = append(*addrs, network+" "+addr)

			var lc net.ListenConfig

			return lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		},
	})
}

func TestOAuthProvider(t *testing.T) {
	p := OAuthProvider()

	want := cloudauth.Provider{
		Kind: domain.KindYandexDisk, Name: "Yandex Disk", Issuer: "Yandex",
		AuthorizeURL: "https://oauth.yandex.ru/authorize", TokenURL: "https://oauth.yandex.ru/token",
		LoopbackAddr: "127.0.0.1:47123", CallbackPath: "/callback", ManualRedirect: verificationRedirect,
	}

	if p.Kind != want.Kind || p.Name != want.Name || p.Issuer != want.Issuer || p.AuthorizeURL != want.AuthorizeURL ||
		p.TokenURL != want.TokenURL || p.LoopbackAddr != want.LoopbackAddr || p.CallbackPath != want.CallbackPath ||
		p.ManualRedirect != want.ManualRedirect || p.Scope != "" || len(p.Extra) != 0 {
		t.Errorf("OAuthProvider() = %+v, want %+v", p, want)
	}
}

// TestYandexLoopbackLogin: the receiver listens on the fixed address, the
// redirect URI is the registered callback, the code is exchanged without a
// client secret and the token has no refresh token and no account.
func TestYandexLoopbackLogin(t *testing.T) {
	d := newFakeDisk(t, testNow)

	var addrs []string

	before := time.Now()

	login, err := yandexAuthorizer(t, d, &addrs).BeginLoopback(t.Context(), fakeClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	t.Cleanup(func() { _ = login.Close() })

	if len(addrs) != 1 || addrs[0] != "tcp "+LoopbackAddr {
		t.Errorf("Listen calls = %v, want tcp %s", addrs, LoopbackAddr)
	}

	u, err := url.Parse(login.URL())
	if err != nil {
		t.Fatalf("login URL: %v", err)
	}

	q := u.Query()
	if got, want := u.Scheme+"://"+u.Host+u.Path, d.oauth.URL+"/authorize"; got != want {
		t.Errorf("authorize URL = %s, want %s", got, want)
	}

	if q.Has("scope") || q.Get("client_id") != fakeClientID || q.Get("code_challenge_method") != "S256" {
		t.Errorf("query = %v", q)
	}

	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Hostname() != "127.0.0.1" || redirect.Path != "/callback" || redirect.Port() == "" {
		t.Fatalf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", q.Get("redirect_uri"))
	}

	type result struct {
		tok usecase.CloudToken
		err error
	}

	ch := make(chan result, 1)

	go func() {
		tok, err := login.Wait(t.Context())
		ch <- result{tok, err}
	}()

	cb := *redirect
	cb.RawQuery = url.Values{"state": {q.Get("state")}, "code": {fakeAuthCode}}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, cb.String(), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	_ = resp.Body.Close()

	r := <-ch
	if r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}

	tok := r.tok
	if tok.Provider != domain.KindYandexDisk || tok.ClientID != fakeClientID || tok.Access != "acc-2" || tok.Refresh != "" || tok.Account != "" {
		t.Errorf("token = %s, want acc-2 without a refresh token and account", describeToken(tok))
	}

	if lo, hi := before.Add(time.Hour), time.Now().Add(time.Hour); tok.Expiry.Before(lo) || tok.Expiry.After(hi) {
		t.Errorf("expiry = %v, want between %v and %v", tok.Expiry, lo, hi)
	}

	forms := d.forms()
	if len(forms) != 1 || forms[0].Has("client_secret") || forms[0].Has("redirect_uri") ||
		forms[0].Get("grant_type") != "authorization_code" || forms[0].Get("code") != fakeAuthCode {
		t.Errorf("token forms = %v, want one code grant without client_secret", forms)
	}

	// The sign-in does not read the account: the app-folder permission forbids it.
	if n := d.count("api", http.MethodGet, "/v1/disk"); n != 0 {
		t.Errorf("account requests = %d, want 0", n)
	}
}

func TestYandexManualLogin(t *testing.T) {
	d := newFakeDisk(t, testNow)

	var addrs []string

	login, err := yandexAuthorizer(t, d, &addrs).BeginManual(t.Context(), fakeClient)
	if err != nil {
		t.Fatalf("BeginManual: %v", err)
	}

	t.Cleanup(func() { _ = login.Close() })

	u, err := url.Parse(login.URL())
	if err != nil {
		t.Fatalf("login URL: %v", err)
	}

	if got := u.Query().Get("redirect_uri"); got != verificationRedirect {
		t.Errorf("redirect_uri = %q, want %q", got, verificationRedirect)
	}

	if len(addrs) != 0 {
		t.Errorf("manual login listened: %v", addrs)
	}

	_, err = login.Exchange(t.Context(), "wrong")
	if ce := cloudError(t, err); ce.Provider != domain.KindYandexDisk || ce.Status != http.StatusBadRequest || ce.Code != codeInvalidGrant {
		t.Errorf("rejected code = %+v, want yandex_disk 400 invalid_grant", ce)
	}

	tok, err := login.Exchange(t.Context(), "  "+fakeAuthCode+"\n")
	if err != nil || tok.Access != "acc-2" || tok.Provider != domain.KindYandexDisk {
		t.Errorf("Exchange = %s, %v; want acc-2", describeToken(tok), err)
	}
}

// TestYandexPortBusy: a busy fixed port is the signal for the manual sign-in.
func TestYandexPortBusy(t *testing.T) {
	d := newFakeDisk(t, testNow)

	var lc net.ListenConfig

	busy, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer busy.Close()

	a := cloudauth.NewAuthorizer(oauthProvider(d.endpoints()), cloudauth.Options{
		HTTP: d.api.Client(),
		Listen: func(string, string) (net.Listener, error) {
			var lc net.ListenConfig

			return lc.Listen(t.Context(), "tcp", busy.Addr().String())
		},
	})

	l, err := a.BeginLoopback(t.Context(), fakeClient)
	if err == nil {
		_ = l.Close()
	}

	if !errors.Is(err, domain.ErrLoopbackUnavailable) {
		t.Errorf("BeginLoopback = %v, want ErrLoopbackUnavailable", err)
	}
}
