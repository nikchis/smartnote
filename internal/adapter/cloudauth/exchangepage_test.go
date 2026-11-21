package cloudauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/i18n"
)

const (
	signedInTitle  = "Signed in to Yandex Disk"
	failedTitle    = "Sign-in failed"
	cancelledTitle = "Sign-in cancelled"
)

// pageTitle returns the h1 of a page of the receiver.
func pageTitle(body string) string {
	_, rest, _ := strings.Cut(body, "<h1>")
	title, _, _ := strings.Cut(rest, "</h1>")

	return title
}

type callbackAnswer struct {
	status int
	header http.Header
	body   string
}

// callbackAsync sends the callback q in a goroutine.
func callbackAsync(t *testing.T, l *loginURL, q url.Values) <-chan callbackAnswer {
	t.Helper()

	ch := make(chan callbackAnswer, 1)

	go func() {
		status, header, body, err := l.request(t, http.MethodGet, "", q)
		if err != nil {
			t.Errorf("callback: %v", err)
		}

		ch <- callbackAnswer{status: status, header: header, body: body}
	}()

	return ch
}

func gotAnswer(t *testing.T, ch <-chan callbackAnswer) callbackAnswer {
	t.Helper()

	select {
	case a := <-ch:
		return a
	case <-time.After(waitLimit):
		t.Fatal("the callback was not answered")
		return callbackAnswer{}
	}
}

// TestLoopbackPageWaitsForExchange: the browser gets the success page only
// after the code is exchanged for the token.
func TestLoopbackPageWaitsForExchange(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	started, release := make(chan struct{}), make(chan struct{})

	var once, releaseOnce sync.Once

	releaseHold := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHold()

	h.oauth.setHold(func() {
		once.Do(func() { close(started) })
		<-release
	})

	ch := waitAsync(t.Context(), l)
	answer := callbackAsync(t, l, l.withState("code", fakeAuthCode))

	select {
	case <-started:
	case <-time.After(waitLimit):
		t.Fatal("the token request did not start")
	}

	select {
	case a := <-answer:
		t.Fatalf("the callback was answered before the exchange ended: %q", pageTitle(a.body))
	case <-time.After(100 * time.Millisecond):
	}

	releaseHold()

	a := gotAnswer(t, answer)
	if a.status != http.StatusOK || pageTitle(a.body) != signedInTitle {
		t.Errorf("callback answer = %d %q, want 200 %q", a.status, pageTitle(a.body), signedInTitle)
	}

	if r := got(t, ch); r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}
}

// TestLoopbackFailedExchangePage: a refused exchange shows the failure page,
// not the success one.
func TestLoopbackFailedExchangePage(t *testing.T) {
	cases := []struct {
		name  string
		setup func(o *fakeOAuth)
	}{
		{name: "OAuth error", setup: func(o *fakeOAuth) {
			o.setReply(func(url.Values) (int, map[string]any) {
				return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": fakeDescription}
			})
		}},
		{name: "server error", setup: func(o *fakeOAuth) {
			o.setReply(func(url.Values) (int, map[string]any) { return http.StatusInternalServerError, map[string]any{} })
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAuthHarness(t)
			tc.setup(h.oauth)

			l := h.loopback()
			ch := waitAsync(t.Context(), l)

			status, header, body := l.callback(t, "", l.withState("code", fakeAuthCode))
			if status != http.StatusOK {
				t.Errorf("status = %d, want 200", status)
			}

			wantPage(t, header, body, failedTitle, "Yandex did not complete the sign-in. Return to Smartnote and try again.", markError)
			wantNoRequestData(t, body, l)

			if r := got(t, ch); r.err == nil {
				t.Error("Wait succeeded after a refused exchange")
			}
		})
	}
}

// TestLoopbackNoRefreshTokenPage: a provider asked for offline access that
// returns no refresh token fails the sign-in, and the browser says so.
func TestLoopbackNoRefreshTokenPage(t *testing.T) {
	o := newFakeOAuth(t)
	p := googleLike(o)
	p.Issuer = "Google"
	a := NewAuthorizer(p, Options{HTTP: o.srv.Client()})

	login, err := a.BeginLoopback(t.Context(), secretClient)
	if err != nil {
		t.Fatalf("BeginLoopback: %v", err)
	}

	defer login.Close()

	l := parseLogin(t, login)
	ch := waitAsync(t.Context(), l)

	_, header, body := l.callback(t, "", l.withState("code", fakeAuthCode))
	wantPage(t, header, body, failedTitle, "Google did not complete the sign-in. Return to Smartnote and try again.", markError)

	if r := got(t, ch); r.err == nil {
		t.Errorf("Wait without a refresh token succeeded: %s", describeToken(r.tok))
	}
}

// TestLoopbackPageWaitIsBounded: without an outcome of the exchange the
// browser gets the failure page after the bound, not a hanging request.
func TestLoopbackPageWaitIsBounded(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	login, ok := l.CloudLogin.(*loopbackLogin)
	if !ok {
		t.Fatalf("login is %T", l.CloudLogin)
	}

	login.exchangeWait = 50 * time.Millisecond // no Wait runs the exchange

	_, _, body := l.callback(t, "", l.withState("code", fakeAuthCode))
	if pageTitle(body) != failedTitle {
		t.Errorf("page = %q, want %q", pageTitle(body), failedTitle)
	}
}

// TestLoopbackCloseDuringExchangePage: a login closed in the app during the
// exchange shows the cancelled page.
func TestLoopbackCloseDuringExchangePage(t *testing.T) {
	h := newAuthHarness(t)
	l := h.loopback()

	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)

	var once sync.Once

	h.oauth.setHold(func() {
		once.Do(func() { close(started) })
		<-release
	})

	ch := waitAsync(t.Context(), l)
	answer := callbackAsync(t, l, l.withState("code", fakeAuthCode))

	select {
	case <-started:
	case <-time.After(waitLimit):
		t.Fatal("the token request did not start")
	}

	_ = l.Close()

	if a := gotAnswer(t, answer); pageTitle(a.body) != cancelledTitle {
		t.Errorf("page = %q, want %q", pageTitle(a.body), cancelledTitle)
	}

	if r := got(t, ch); !errors.Is(r.err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", r.err)
	}
}

// TestLoopbackFailedPageFollowsLanguage: the failure page after a refused
// exchange is in the language of the moment.
func TestLoopbackFailedPageFollowsLanguage(t *testing.T) {
	h := newAuthHarness(t)
	h.oauth.setReply(func(url.Values) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant"}
	})

	lang := i18n.NewCurrent(i18n.RU)
	opts := h.options()
	opts.Lang = lang.Get
	l := h.begin(h.authorizer(opts), false)
	ch := waitAsync(t.Context(), l)

	_, _, body := l.callback(t, "", l.withState("code", fakeAuthCode))
	if want := i18n.T(i18n.RU, i18n.AuthFailedTitle); pageTitle(body) != want {
		t.Errorf("page = %q, want %q", pageTitle(body), want)
	}

	got(t, ch)
}

// TestLoopbackSlowExchangeOutlivesWriteTimeout: an exchange longer than the
// receiver's write timeout still reaches the browser as its page.
func TestLoopbackSlowExchangeOutlivesWriteTimeout(t *testing.T) {
	old := receiverWriteTimeout
	receiverWriteTimeout = 100 * time.Millisecond

	t.Cleanup(func() { receiverWriteTimeout = old })

	h := newAuthHarness(t)
	l := h.loopback()

	var once sync.Once

	h.oauth.setHold(func() { once.Do(func() { time.Sleep(300 * time.Millisecond) }) })

	ch := waitAsync(t.Context(), l)

	_, _, body := l.callback(t, "", l.withState("code", fakeAuthCode))
	if pageTitle(body) != signedInTitle {
		t.Errorf("page = %q, want %q", pageTitle(body), signedInTitle)
	}

	if r := got(t, ch); r.err != nil {
		t.Fatalf("Wait: %v", r.err)
	}
}
