package gdrive

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudauth"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const appDataScope = "https://www.googleapis.com/auth/drive.appdata"

// googleAuthorizer is an Authorizer of p whose receiver listens on a free
// loopback port; listen records the asked address.
func googleAuthorizer(t *testing.T, p cloudauth.Provider, hc *http.Client, addrs *[]string) *cloudauth.Authorizer {
	t.Helper()

	return cloudauth.NewAuthorizer(p, cloudauth.Options{
		HTTP: hc,
		Listen: func(network, addr string) (net.Listener, error) {
			*addrs = append(*addrs, network+" "+addr)

			var lc net.ListenConfig

			return lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		},
	})
}

func TestOAuthProvider(t *testing.T) {
	p := OAuthProvider()

	if p.Kind != domain.KindGoogleDrive || p.Name != "Google Drive" || p.Issuer != "Google" ||
		p.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" || p.TokenURL != "https://oauth2.googleapis.com/token" ||
		p.Scope != appDataScope || p.LoopbackAddr != "127.0.0.1:0" || p.CallbackPath != "/callback" || p.ManualRedirect != "" {
		t.Errorf("OAuthProvider() = %+v", p)
	}

	if len(p.Extra) != 1 || p.Extra.Get("access_type") != "offline" {
		t.Errorf("Extra = %v, want only access_type=offline (no prompt=consent, П2)", p.Extra)
	}
}

// TestOAuthProviderScopeIsAppDataOnly: the authorize URL asks for exactly
// the drive.appdata scope (§1.2, criterion 4), offline access and PKCE S256,
// with the receiver on a random loopback port and /callback.
func TestOAuthProviderScopeIsAppDataOnly(t *testing.T) {
	var addrs []string

	login, err := googleAuthorizer(t, OAuthProvider(), nil, &addrs).BeginLoopback(t.Context(), fakeClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	t.Cleanup(func() { _ = login.Close() })

	if len(addrs) != 1 || addrs[0] != "tcp 127.0.0.1:0" {
		t.Errorf("Listen calls = %v, want tcp 127.0.0.1:0", addrs)
	}

	u, err := url.Parse(login.URL())
	if err != nil {
		t.Fatalf("login URL: %v", err)
	}

	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("authorize URL = %s", got)
	}

	q := u.Query()
	if s := q["scope"]; len(s) != 1 || s[0] != appDataScope {
		t.Errorf("scope = %q, want exactly %s", s, appDataScope)
	}

	if !strings.Contains(u.RawQuery, "scope="+url.QueryEscape(appDataScope)+"&") && !strings.HasSuffix(u.RawQuery, "scope="+url.QueryEscape(appDataScope)) {
		t.Errorf("raw query %q lacks the exact scope", u.RawQuery)
	}

	if q.Get("access_type") != "offline" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("response_type") != "code" || q.Get("client_id") != fakeClientID || q.Has("prompt") || q.Has("client_secret") {
		t.Errorf("query = %v", q)
	}

	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Path != "/callback" {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", q.Get("redirect_uri"))
	}
}

// TestGoogleLoopbackLogin: the code comes back to /callback and is exchanged
// with the client secret and the redirect URI; the token has the refresh
// token (П2).
func TestGoogleLoopbackLogin(t *testing.T) {
	d := newFakeDrive(t)

	var addrs []string

	login, err := googleAuthorizer(t, oauthProvider(d.endpoints()), d.oauth.Client(), &addrs).BeginLoopback(t.Context(), fakeClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	t.Cleanup(func() { _ = login.Close() })

	u, err := url.Parse(login.URL())
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()

	type result struct {
		tok usecase.CloudToken
		err error
	}

	ch := make(chan result, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	go func() {
		tok, err := login.Wait(ctx)
		ch <- result{tok, err}
	}()

	cb, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}

	cb.RawQuery = url.Values{
		"state": {q.Get("state")}, "code": {fakeAuthCode}, "scope": {appDataScope}, "iss": {"https://accounts.google.com"},
	}.Encode()

	callback(t, cb.String())

	r := <-ch
	if r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}

	if r.tok.Provider != domain.KindGoogleDrive || r.tok.Refresh != fakeRefresh || r.tok.Access != "acc-2" || r.tok.ClientID != fakeClientID {
		t.Errorf("token = %+v", r.tok)
	}

	forms := d.forms()
	if len(forms) != 1 {
		t.Fatalf("token requests = %d, want 1", len(forms))
	}

	if f := forms[0]; f.Get("client_secret") != fakeClientSecret || f.Get("redirect_uri") != q.Get("redirect_uri") || f.Get("code_verifier") == "" {
		t.Errorf("exchange form lacks the secret, the redirect URI or the verifier: keys %v", keys(f))
	}
}

// TestGoogleRefresh: the refresh sends the client secret and keeps the
// refresh token, which Google does not renew (П2); without the secret
// Google refuses it.
func TestGoogleRefresh(t *testing.T) {
	d := newFakeDrive(t)

	var addrs []string

	a := googleAuthorizer(t, oauthProvider(d.endpoints()), d.oauth.Client(), &addrs)

	tok, err := a.Refresh(t.Context(), fakeClient, fakeRefresh)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if tok.Access != "acc-2" || tok.Refresh != fakeRefresh || tok.Expiry.Before(time.Now().Add(59*time.Minute)) {
		t.Errorf("token = %+v", tok)
	}

	if f := d.forms()[0]; f.Get("client_secret") != fakeClientSecret || f.Get("grant_type") != "refresh_token" {
		t.Errorf("refresh form keys = %v", keys(f))
	}

	noSecret := fakeClient
	noSecret.Secret = ""

	if _, err := a.Refresh(t.Context(), noSecret, fakeRefresh); err == nil || errors.Is(err, domain.ErrReauthRequired) {
		t.Errorf("Refresh without the secret: err = %v, want a refused request (invalid_request)", err)
	}
}

// callback sends the browser's GET to the receiver.
func callback(t *testing.T, link string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, link, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	_ = resp.Body.Close()
}

func keys(v url.Values) []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}

	return out
}
