package cloudauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// googleLike is a provider with a random loopback port, extra authorize
// parameters and no manual sign-in.
func googleLike(o *fakeOAuth) Provider {
	p := o.provider()
	p.Kind = domain.KindGoogleDrive
	p.Name = "Google Drive"
	p.LoopbackAddr = "127.0.0.1:0"
	p.ManualRedirect = ""
	p.Scope = "https://www.googleapis.com/auth/drive.appdata"
	p.Extra = url.Values{"access_type": {"offline"}, "prompt": {"consent"}}

	return p
}

var secretClient = usecase.CloudClient{Provider: domain.KindGoogleDrive, ID: fakeClientID, Secret: "s3cret-marker"}

func TestBeginManualUnsupported(t *testing.T) {
	o := newFakeOAuth(t)
	a := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()})

	l, err := a.BeginManual(t.Context(), secretClient)
	if err == nil {
		_ = l.Close()
	}

	if !errors.Is(err, domain.ErrManualUnsupported) {
		t.Errorf("BeginManual = %v, want ErrManualUnsupported", err)
	}
}

func TestRandomPortRedirect(t *testing.T) {
	o := newFakeOAuth(t)
	a := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()})

	l, err := a.BeginLoopback(t.Context(), secretClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	defer l.Close()

	p := parseLogin(t, l)

	u, err := url.Parse(p.query.Get("redirect_uri"))
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/callback" || u.Port() == "" || u.Port() == "0" {
		t.Errorf("redirect_uri = %q, want http://127.0.0.1:<real port>/callback", p.query.Get("redirect_uri"))
	}

	if p.query.Get("scope") != "https://www.googleapis.com/auth/drive.appdata" ||
		p.query.Get("access_type") != "offline" || p.query.Get("prompt") != "consent" {
		t.Errorf("query = %v, want the scope and the extra parameters", p.query)
	}

	ch := waitAsync(t.Context(), l)

	if status, _, _ := p.callback(t, "", p.withState("code", "x")); status != http.StatusOK {
		t.Errorf("receiver on the random port answered %d, want 200", status)
	}

	got(t, ch) // the fake server refuses the code
}

func TestExtraDoesNotOverrideProtocolParameters(t *testing.T) {
	o := newFakeOAuth(t)
	p := googleLike(o)
	p.Extra = url.Values{"state": {"fixed"}, "code_challenge": {"fixed"}, "client_id": {"fixed"}}

	l, err := NewAuthorizer(p, Options{HTTP: o.srv.Client()}).BeginLoopback(t.Context(), secretClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	defer l.Close()

	q := parseLogin(t, l).query
	for _, k := range []string{"state", "code_challenge", "client_id"} {
		if q.Get(k) == "fixed" || len(q[k]) != 1 {
			t.Errorf("%s = %v, want the protocol value only", k, q[k])
		}
	}
}

func TestNoScopeParameterWithoutScope(t *testing.T) {
	h := newAuthHarness(t)
	if l := h.loopback(); l.query.Has("scope") {
		t.Errorf("query = %v, want no scope for a provider without one", l.query)
	}
}

func TestExchangeSendsSecretWhenSet(t *testing.T) {
	o := newFakeOAuth(t)
	o.setReply(func(url.Values) (int, map[string]any) {
		return http.StatusOK, map[string]any{"access_token": "acc", "refresh_token": "ref", "expires_in": 60}
	})

	a := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()})

	l, err := a.BeginLoopback(t.Context(), secretClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	defer l.Close()

	p := parseLogin(t, l)
	ch := waitAsync(t.Context(), l)
	p.callback(t, "", p.withState("code", fakeAuthCode))

	r := got(t, ch)
	if r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}

	if r.tok.Provider != domain.KindGoogleDrive || r.tok.Access != "acc" || r.tok.Refresh != "ref" || r.tok.ClientID != fakeClientID {
		t.Errorf("token = %s", describeToken(r.tok))
	}

	f := o.forms()
	if len(f) != 1 || f[0].Get("client_secret") != "s3cret-marker" || f[0].Get("redirect_uri") != p.query.Get("redirect_uri") {
		t.Fatalf("form = %v, want client_secret and the redirect_uri of the login", f)
	}
}

func TestExchangeWithoutSecretHasNoSecretOrRedirect(t *testing.T) {
	h := newAuthHarness(t)
	l := h.begin(h.authorizer(h.options()), true)

	if _, err := l.Exchange(t.Context(), fakeAuthCode); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	f := h.oauth.forms()
	if len(f) != 1 || f[0].Has("client_secret") || f[0].Has("redirect_uri") {
		t.Errorf("form = %v, want neither client_secret nor redirect_uri", f)
	}
}

func TestExchangeRequiresRefreshTokenWhenOffline(t *testing.T) {
	o := newFakeOAuth(t)
	p := googleLike(o)
	p.ManualRedirect = verificationRedirect // to exchange without a receiver

	l, err := NewAuthorizer(p, Options{HTTP: o.srv.Client()}).BeginManual(t.Context(), secretClient)
	if err != nil {
		t.Fatalf("BeginManual: %v", err)
	}

	defer l.Close()

	tok, err := l.Exchange(t.Context(), fakeAuthCode)
	if err == nil {
		t.Fatalf("Exchange without a refresh token succeeded: %s", describeToken(tok))
	}

	if tok.Access != "" {
		t.Error("a failed exchange returned an access token")
	}

	if ce := cloudError(t, err); ce.Provider != domain.KindGoogleDrive {
		t.Errorf("error = %+v, want a Google Drive cloud error", ce)
	}
}

func TestRefreshInvalidGrantIsReauth(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_client", "unauthorized_client"} {
		t.Run(code, func(t *testing.T) {
			o := newFakeOAuth(t)
			o.setReply(func(url.Values) (int, map[string]any) {
				return http.StatusBadRequest, map[string]any{"error": code, "error_description": fakeDescription}
			})

			a := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()})

			_, err := a.Refresh(t.Context(), secretClient, fakeRefresh)

			re, ok := errors.AsType[*domain.ReauthRequiredError](err)
			if !ok || re.Kind != domain.KindGoogleDrive || !errors.Is(err, domain.ErrReauthRequired) {
				t.Fatalf("Refresh = %v, want ReauthRequiredError of google_drive", err)
			}

			wantNoText(t, err, fakeRefresh, fakeDescription, "s3cret-marker")
		})
	}
}

func TestRefreshOtherErrorIsNotReauth(t *testing.T) {
	o := newFakeOAuth(t)
	o.setReply(func(url.Values) (int, map[string]any) {
		return http.StatusServiceUnavailable, map[string]any{"error": "temporarily_unavailable"}
	})

	_, err := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()}).Refresh(t.Context(), secretClient, fakeRefresh)
	if errors.Is(err, domain.ErrReauthRequired) {
		t.Errorf("Refresh = %v, a temporary failure must not ask for a new sign-in", err)
	}

	if ce := cloudError(t, err); ce.Status != http.StatusServiceUnavailable || ce.Code != "temporarily_unavailable" {
		t.Errorf("error = %+v", ce)
	}
}

func TestRefreshKeepsOldRefreshTokenWhenAbsent(t *testing.T) {
	o := newFakeOAuth(t)
	a := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()})
	before := time.Now()

	tok, err := a.Refresh(t.Context(), secretClient, fakeRefresh)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if tok.Provider != domain.KindGoogleDrive || tok.ClientID != fakeClientID || tok.Access != "acc-3" || tok.Refresh != fakeRefresh {
		t.Errorf("token = %s, want acc-3 with the old refresh token", describeToken(tok))
	}

	if lo, hi := before.Add(30*time.Minute), time.Now().Add(30*time.Minute); tok.Expiry.Before(lo) || tok.Expiry.After(hi) {
		t.Errorf("expiry = %v, want between %v and %v", tok.Expiry, lo, hi)
	}

	f := o.forms()
	if len(f) != 1 || f[0].Get("grant_type") != "refresh_token" || f[0].Get("refresh_token") != fakeRefresh ||
		f[0].Get("client_id") != fakeClientID || f[0].Get("client_secret") != "s3cret-marker" {
		t.Errorf("form = %v, want a refresh_token grant with client_secret", f)
	}
}

func TestRefreshTakesNewRefreshToken(t *testing.T) {
	o := newFakeOAuth(t)
	o.setReply(func(url.Values) (int, map[string]any) {
		return http.StatusOK, map[string]any{"access_token": "acc-4", "refresh_token": "ref-2", "expires_in": 60}
	})

	tok, err := NewAuthorizer(googleLike(o), Options{HTTP: o.srv.Client()}).Refresh(t.Context(), secretClient, fakeRefresh)
	if err != nil || tok.Refresh != "ref-2" {
		t.Errorf("Refresh = %s, %v; want the new refresh token", describeToken(tok), err)
	}
}

func TestRefreshWithoutSecretSendsNone(t *testing.T) {
	o := newFakeOAuth(t)

	_, err := NewAuthorizer(o.provider(), Options{HTTP: o.srv.Client()}).Refresh(t.Context(), fakeClient, fakeRefresh)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if f := o.forms(); len(f) != 1 || f[0].Has("client_secret") {
		t.Errorf("form = %v, want no client_secret", f)
	}
}

func TestRefreshCancelledIsContextError(t *testing.T) {
	o := newFakeOAuth(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewAuthorizer(o.provider(), Options{HTTP: o.srv.Client()}).Refresh(ctx, fakeClient, fakeRefresh)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Refresh = %v, want context.Canceled", err)
	}

	if _, ok := errors.AsType[cloudhttp.CloudError](err); ok {
		t.Errorf("a cancelled refresh reads as a cloud error: %v", err)
	}
}

func wantNoText(t *testing.T, err error, secrets ...string) {
	t.Helper()

	for _, s := range secrets {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error %q quotes %q", err.Error(), s)
		}
	}
}
