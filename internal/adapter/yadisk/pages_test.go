package yadisk

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

const closeHint = "You can close this tab."

func TestPageCSPHashesTheInlineStyle(t *testing.T) {
	body := pageSignedIn.body()

	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(body)
	if m == nil || strings.Count(body, "<style") != 1 {
		t.Fatal("the page has no single <style>")
	}

	sum := sha256.Sum256([]byte(m[1]))
	want := "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; img-src 'none'; frame-ancestors 'none'"

	if pageCSP != want {
		t.Errorf("pageCSP = %q, want %q", pageCSP, want)
	}
}

func TestPageColorsAreLiteralsInSVG(t *testing.T) {
	for _, svg := range []string{logoSVG, markOK, markMuted, markError} {
		if strings.Contains(svg, "var(") || strings.Contains(svg, "currentColor") {
			t.Errorf("svg takes a color from outside: %.60s", svg)
		}
	}
}

func TestPagesOfTheReceiver(t *testing.T) {
	tests := []struct {
		name   string
		status int
		title  string
		text   string
		mark   string
		act    func(t *testing.T, l *loginURL) (int, http.Header, string)
	}{
		{
			name: "success", status: http.StatusOK, title: "Signed in to Yandex Disk", mark: markOK,
			text: "Return to Smartnote and press OK in Settings to save the sign-in.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "", l.withState("code", fakeAuthCode))
			},
		},
		{
			name: "cancelled", status: http.StatusOK, title: "Sign-in cancelled", mark: markMuted,
			text: "Smartnote did not get access to Yandex Disk. You can sign in again from Settings.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "", l.withState("error", "access_denied", "error_description", fakeDescription))
			},
		},
		{
			name: "other OAuth error", status: http.StatusOK, title: "Sign-in failed", mark: markError,
			text: "Yandex did not complete the sign-in. Return to Smartnote and try again.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "", l.withState("error", "server_error", "error_description", fakeDescription))
			},
		},
		{
			name: "wrong state", status: http.StatusBadRequest, title: "Sign-in link expired", mark: markMuted,
			text: "This sign-in link has expired or was already used. Start the sign-in again from Smartnote Settings.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "", url.Values{"state": {"foreign"}, "code": {fakeAuthCode}})
			},
		},
		{
			name: "missing state", status: http.StatusBadRequest, title: "Sign-in link expired", mark: markMuted,
			text: "This sign-in link has expired or was already used. Start the sign-in again from Smartnote Settings.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "", url.Values{"code": {fakeAuthCode}})
			},
		},
		{
			name: "second callback", status: http.StatusBadRequest, title: "Sign-in link expired", mark: markMuted,
			text: "This sign-in link has expired or was already used. Start the sign-in again from Smartnote Settings.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				if status, _, _ := l.callback(t, "", l.withState("code", fakeAuthCode)); status != http.StatusOK {
					t.Fatalf("first callback answered %d", status)
				}

				// The listener is closed after the first one; a connection that
				// is already open could still be served, so the handler is asked directly.
				return serveCallback(t, l, l.withState("code", "second"))
			},
		},
		{
			name: "other path", status: http.StatusNotFound, title: "Page not found", mark: markMuted,
			text: "This address is not part of Smartnote.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				return l.callback(t, "/favicon.ico", l.withState("code", fakeAuthCode))
			},
		},
		{
			name: "other method", status: http.StatusNotFound, title: "Page not found", mark: markMuted,
			text: "This address is not part of Smartnote.",
			act: func(t *testing.T, l *loginURL) (int, http.Header, string) {
				status, header, body, err := l.request(t, http.MethodPost, "", l.withState("code", fakeAuthCode))
				if err != nil {
					t.Fatalf("POST: %v", err)
				}

				return status, header, body
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newAuthHarness(t)
			l := h.loopback()

			status, header, body := tc.act(t, l)

			if status != tc.status {
				t.Errorf("status = %d, want %d", status, tc.status)
			}

			wantPage(t, header, body, tc.title, tc.text, tc.mark)
			wantNoRequestData(t, body, l)
		})
	}
}

// serveCallback runs the receiver's handler on a request, without a socket.
func serveCallback(t *testing.T, l *loginURL, q url.Values) (status int, header http.Header, body string) {
	t.Helper()

	login, ok := l.CloudLogin.(*loopbackLogin)
	if !ok {
		t.Fatalf("login is %T", l.CloudLogin)
	}

	rec := &recorder{header: http.Header{}, status: http.StatusOK}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callbackPath+"?"+q.Encode(), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	login.ServeHTTP(rec, req)

	return rec.status, rec.header, rec.body.String()
}

// recorder is a minimal http.ResponseWriter.
type recorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func wantPage(t *testing.T, header http.Header, body, title, text, mark string) {
	t.Helper()

	css := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(body)
	if css == nil {
		t.Fatal("no <style> in the page")
	}

	sum := sha256.Sum256([]byte(css[1]))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"

	for k, want := range map[string]string{
		"Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; style-src " + hash + "; img-src 'none'; frame-ancestors 'none'",
	} {
		if header.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, header.Get(k), want)
		}
	}

	if got := regexp.MustCompile(`<h1>(.*?)</h1>`).FindAllStringSubmatch(body, -1); len(got) != 1 || got[0][1] != title {
		t.Errorf("h1 = %v, want only %q", got, title)
	}

	for _, want := range []string{"<p>" + text + "</p>", closeHint, "<title>Smartnote</title>", mark} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	if strings.Count(body, "<main") != 1 || strings.Count(body, "<svg") != 2 {
		t.Errorf("page is not one card with a logo and a mark")
	}

	lower := strings.ToLower(body)
	for _, bad := range []string{"<script", "http", "<a ", "href", "<link", "<img", "<iframe", "@import", "url(http", "var(--", "onload", "onclick"} {
		if strings.Contains(lower, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
}

// wantNoRequestData checks that nothing of the callback is in the page.
func wantNoRequestData(t *testing.T, body string, l *loginURL) {
	t.Helper()

	for _, secret := range []string{fakeAuthCode, fakeDescription, l.query.Get("state"), "foreign", "second", "server_error", "access_denied", "favicon", callbackPath} {
		if strings.Contains(body, secret) {
			t.Errorf("page contains request data %q", secret)
		}
	}
}
