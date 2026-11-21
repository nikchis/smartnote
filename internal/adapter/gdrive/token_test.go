package gdrive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// recordingSource is a token source that keeps the token the store gives to
// Update, as the stored source does, and can fail the update.
type recordingSource struct {
	mu        sync.Mutex
	tok       usecase.CloudToken
	updates   []usecase.CloudToken
	updateErr error
}

func (r *recordingSource) Token(context.Context) (usecase.CloudToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.tok, nil
}

func (r *recordingSource) Update(_ context.Context, t usecase.CloudToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.updateErr != nil {
		return r.updateErr
	}

	r.tok = t
	r.updates = append(r.updates, t)

	return nil
}

func (r *recordingSource) saved() []usecase.CloudToken {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]usecase.CloudToken(nil), r.updates...)
}

// signedIn is a device signed in as "user@example.org" with access, valid
// for left from now.
func signedIn(access string, left time.Duration) *recordingSource {
	return &recordingSource{tok: usecase.CloudToken{
		Provider: domain.KindGoogleDrive, ClientID: fakeClientID, Access: access, Refresh: fakeRefresh,
		Account: "user@example.org", Expiry: time.Now().Add(left),
	}}
}

// tokenStore is a store over d whose clock is the real one, as the refresh
// computes the expiry of the new token with it.
func tokenStore(t *testing.T, d *fakeDrive, src usecase.CloudTokenSource) *Store {
	t.Helper()

	opts := testOptions(d)
	opts.now = time.Now

	return newStore(d.endpoints(), src, fakeClient, testRefresher(t, d), &memUploads{}, opts)
}

func apiRequests(d *fakeDrive) int {
	n := 0

	for _, r := range d.reqs() {
		if r.server == "api" {
			n++
		}
	}

	return n
}

// assertFullToken checks the token the store saved after one refresh: the
// new access token and expiry, and everything the answer lacks carried over.
func assertFullToken(t *testing.T, src *recordingSource) {
	t.Helper()

	saved := src.saved()
	if len(saved) != 1 {
		t.Fatalf("Update calls = %d, want 1", len(saved))
	}

	got := saved[0]
	if got.Access != "acc-2" || got.Refresh != fakeRefresh || got.Account != "user@example.org" ||
		got.ClientID != fakeClientID || got.Provider != domain.KindGoogleDrive {
		t.Errorf("saved token lost a field of the previous one: %+v", got)
	}

	if got.Expiry.Before(time.Now().Add(59 * time.Minute)) {
		t.Errorf("saved token expires at %v, want about an hour", got.Expiry)
	}
}

func TestStoreRefreshesBeforeExpiry(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)

	// The old access token is still accepted by the API: only the expiry
	// makes the store refresh.
	src := signedIn(fakeAccess, 30*time.Second)

	data, _, found, err := tokenStore(t, d, src).Read(t.Context(), container)
	if err != nil || !found || string(data) != "data" {
		t.Fatalf("Read = %q, %v, %v", data, found, err)
	}

	assertFullToken(t, src)

	if n := len(d.forms()); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}

	for _, r := range d.reqs() {
		if r.server == "api" && r.auth != "Bearer acc-2" {
			t.Errorf("request %s %s carried %q, want the new token", r.method, r.path, r.auth)
		}
	}
}

func TestStoreKeepsFreshToken(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)

	src := signedIn(fakeAccess, 2*time.Minute)

	if _, _, _, err := tokenStore(t, d, src).Read(t.Context(), container); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if n := len(d.forms()); n != 0 || len(src.saved()) != 0 {
		t.Errorf("token requests = %d, updates = %d, want none", n, len(src.saved()))
	}
}

func TestStoreRefreshesOnce401(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)

	src := signedIn("stale", time.Hour) // not expired, but the API rejects it

	data, _, found, err := tokenStore(t, d, src).Read(t.Context(), container)
	if err != nil || !found || string(data) != "data" {
		t.Fatalf("Read = %q, %v, %v", data, found, err)
	}

	assertFullToken(t, src)

	if n := len(d.forms()); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}

func TestStoreSecond401IsReauth(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)
	d.set(func(d *fakeDrive) { d.denyAll = true })

	err := readErr(t, tokenStore(t, d, signedIn("stale", time.Hour)))

	if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
		t.Fatalf("err = %v, want ReauthRequiredError{google_drive}", err)
	}

	if !errors.Is(err, domain.ErrReauthRequired) {
		t.Errorf("err = %v, want ErrReauthRequired", err)
	}

	if n := len(d.forms()); n != 1 {
		t.Errorf("token requests = %d, want exactly 1 refresh", n)
	}

	if n := apiRequests(d); n != 2 {
		t.Errorf("API requests = %d, want 2 (first and one retry)", n)
	}
}

func TestStoreInvalidGrantIsReauth(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)
	d.set(func(d *fakeDrive) { d.refresh = "another-refresh" }) // the grant was revoked

	for name, src := range map[string]*recordingSource{
		"expired":  signedIn(fakeAccess, 10*time.Second),
		"rejected": signedIn("stale", time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			err := readErr(t, tokenStore(t, d, src))

			if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
				t.Fatalf("err = %v, want ReauthRequiredError{google_drive}", err)
			}

			if len(src.saved()) != 0 {
				t.Error("a token was saved after a refused grant")
			}
		})
	}
}

func TestStoreUpdateErrorIsLocal(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)

	src := signedIn(fakeAccess, 10*time.Second)
	src.updateErr = errors.New("disk full")

	err := readErr(t, tokenStore(t, d, src))

	if _, ok := errors.AsType[usecase.LocalError](err); !ok {
		t.Errorf("err = %v, want usecase.LocalError", err)
	}

	if n := apiRequests(d); n != 0 {
		t.Errorf("API requests = %d after a failed save, want 0", n)
	}
}

// TestStoreRefreshFailureOtherThanGrantIsNotReauth: a network failure of
// the refresh does not ask for a new sign-in.
func TestStoreRefreshNetworkErrorIsNotReauth(t *testing.T) {
	d := newFakeDrive(t)
	src := signedIn(fakeAccess, 10*time.Second)
	s := tokenStore(t, d, src)

	d.oauth.Close()

	err := readErr(t, s)
	if err == nil || errors.Is(err, domain.ErrReauthRequired) {
		t.Errorf("err = %v, want a network error", err)
	}
}

func TestStoreNoRefreshTokenIsReauth(t *testing.T) {
	d := newFakeDrive(t)
	src := signedIn("stale", time.Hour)
	src.tok.Refresh = ""

	err := readErr(t, tokenStore(t, d, src))
	if !errors.Is(err, domain.ErrReauthRequired) {
		t.Errorf("err = %v, want ErrReauthRequired", err)
	}
}

// TestStoreConcurrentReadsRefreshOnce: requests that share an expired token
// refresh it once, and the race detector sees no unguarded access.
func TestStoreConcurrentReadsRefreshOnce(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, []byte("data"), testNow)

	src := signedIn(fakeAccess, 10*time.Second)
	s := tokenStore(t, d, src)

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			if err := readErr(t, s); err != nil {
				t.Errorf("Read: %v", err)
			}
		})
	}

	wg.Wait()

	if n := len(d.forms()); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}
