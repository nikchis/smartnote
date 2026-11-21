package gdrive

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
)

// TestGoogleReasons: the store reads the reason of a Drive error answer
// from the closed list (§5.7) and gives the known causes of §9; the message
// of the answer never gets into the error.
func TestGoogleReasons(t *testing.T) {
	tests := []struct {
		name   string
		fail   fakeFailure
		code   string
		reason cloudhttp.Reason
	}{
		{name: "quota", fail: fakeFailure{http.StatusForbidden, "storageQuotaExceeded"}, code: "storageQuotaExceeded", reason: cloudhttp.ReasonNoSpace},
		{name: "user rate", fail: fakeFailure{http.StatusForbidden, "userRateLimitExceeded"}, code: "userRateLimitExceeded", reason: cloudhttp.ReasonBusy},
		{name: "rate", fail: fakeFailure{http.StatusForbidden, "rateLimitExceeded"}, code: "rateLimitExceeded", reason: cloudhttp.ReasonBusy},
		{name: "too many requests", fail: fakeFailure{http.StatusTooManyRequests, "rateLimitExceeded"}, code: "rateLimitExceeded", reason: cloudhttp.ReasonBusy},
		{name: "too many requests without a reason", fail: fakeFailure{http.StatusTooManyRequests, ""}, code: "other", reason: cloudhttp.ReasonBusy},
		{name: "permissions", fail: fakeFailure{http.StatusForbidden, "insufficientPermissions"}, code: "insufficientPermissions"},
		{name: "quota reason on another status", fail: fakeFailure{http.StatusBadRequest, "storageQuotaExceeded"}, code: "storageQuotaExceeded"},
		{name: "backend", fail: fakeFailure{http.StatusInternalServerError, "backendError"}, code: "backendError"},
		{name: "unknown reason", fail: fakeFailure{http.StatusBadRequest, "somethingNew"}, code: "other"},
		{name: "not JSON", fail: fakeFailure{http.StatusBadGateway, ""}, code: "other"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDrive(t)
			d.set(func(d *fakeDrive) { d.fail[epList] = tc.fail })

			err := readErr(t, newTestStore(t, d))

			ce := cloudError(t, err)
			if ce.Provider != domain.KindGoogleDrive || ce.Status != tc.fail.status || ce.Code != tc.code ||
				ce.Reason != tc.reason || ce.Step != stepList {
				t.Errorf("CloudError = %+v, want google_drive %d %s reason %q at list", ce, tc.fail.status, tc.code, tc.reason)
			}

			if strings.Contains(err.Error(), fakeMessage) {
				t.Errorf("the error carries the message of the answer: %v", err)
			}
		})
	}
}

func TestBearerAllowed(t *testing.T) {
	tests := []struct {
		link string
		ok   bool
	}{
		{link: "https://www.googleapis.com/drive/v3/files", ok: true},
		{link: "https://www.googleapis.com:443/upload/drive/v3/files", ok: true},
		{link: "https://oauth2.googleapis.com/token", ok: true},
		{link: "https://WWW.GOOGLEAPIS.COM/drive/v3/files", ok: true},
		{link: "http://www.googleapis.com/drive/v3/files"},
		{link: "https://googleapis.com/drive/v3/files"},
		{link: "https://storage.googleapis.com/x"},
		{link: "https://www.googleapis.com.evil.example/x"},
		{link: "https://evilwww.googleapis.com/x"},
		{link: "https://www.googleapis.com@evil.example/x"},
		{link: "https://127.0.0.1/x"},
	}

	for _, tc := range tests {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Fatal(err)
		}

		if got := bearerAllowed(u, googleHost); got != tc.ok {
			t.Errorf("bearerAllowed(%s) = %v, want %v", tc.link, got, tc.ok)
		}
	}
}

// TestAuthorizationOnlyToGoogleHosts: the store sends no request with the
// token to a host other than the Google API hosts, and does not follow a
// redirect elsewhere.
func TestAuthorizationOnlyToGoogleHosts(t *testing.T) {
	t.Run("emulator host is refused by default", func(t *testing.T) {
		d := newFakeDrive(t)
		d.put(container, []byte("x"), testNow)

		opts := testOptions(d)
		opts.allowHost = nil

		err := readErr(t, newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), opts))
		if !errors.Is(err, errForeignHost) {
			t.Errorf("Read: err = %v, want errForeignHost", err)
		}

		if n := len(d.reqs()); n != 0 {
			t.Errorf("the emulator got %d requests, want 0", n)
		}
	})

	t.Run("http endpoint is refused", func(t *testing.T) {
		d := newFakeDrive(t)
		e := d.endpoints()
		e.API = strings.Replace(e.API, "https://", "http://", 1)

		if err := readErr(t, newStore(e, testTokens(), fakeClient, testRefresher(t, d), testOptions(d))); !errors.Is(err, errForeignHost) {
			t.Errorf("Read: err = %v, want errForeignHost", err)
		}
	})

	t.Run("redirect is not followed", func(t *testing.T) {
		var hits atomic.Int32

		other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
		t.Cleanup(other.Close)

		d := newFakeDrive(t)
		d.set(func(d *fakeDrive) { d.redirectTo = other.URL + "/elsewhere" })

		err := readErr(t, newTestStore(t, d))
		if ce := cloudError(t, err); ce.Status != http.StatusFound {
			t.Errorf("CloudError = %+v, want the redirect status", ce)
		}

		if n := hits.Load(); n != 0 {
			t.Errorf("the redirect target got %d requests, want 0", n)
		}
	})
}
