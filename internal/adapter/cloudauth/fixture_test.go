package cloudauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const testLoopbackAddr = "127.0.0.1:47123"

// fakeOAuth is an OAuth server of the tests: an https token endpoint that
// records the forms it gets.
type fakeOAuth struct {
	srv *httptest.Server

	mu         sync.Mutex
	tokenForms []url.Values
	reqs       []string
	hold       func()                                   // called before a token request is served
	reply      func(f url.Values) (int, map[string]any) // replaces the default answer when set
}

func newFakeOAuth(t *testing.T) *fakeOAuth {
	t.Helper()

	o := &fakeOAuth{}
	o.srv = httptest.NewTLSServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.srv.Close)

	return o
}

// provider is the Yandex-like provider over the fake server.
func (o *fakeOAuth) provider() Provider {
	return Provider{
		Kind: domain.KindYandexDisk, Name: "Yandex Disk", Issuer: "Yandex",
		AuthorizeURL: o.srv.URL + "/authorize", TokenURL: o.srv.URL + "/token",
		LoopbackAddr: testLoopbackAddr, CallbackPath: "/callback",
		ManualRedirect: verificationRedirect,
	}
}

func (o *fakeOAuth) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	hold := o.hold
	o.mu.Unlock()

	if hold != nil {
		hold()
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	o.reqs = append(o.reqs, r.Method+" "+r.URL.Path)

	if r.Method != http.MethodPost || r.URL.Path != "/token" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f := r.PostForm
	o.tokenForms = append(o.tokenForms, f)

	status, body := o.answer(f)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (o *fakeOAuth) answer(f url.Values) (status int, body map[string]any) {
	if o.reply != nil {
		return o.reply(f)
	}

	denied := map[string]any{"error": "invalid_grant", "error_description": fakeDescription}

	switch f.Get("grant_type") {
	case "authorization_code":
		if f.Get("code") != fakeAuthCode || f.Get("code_verifier") == "" || f.Get("client_id") != fakeClientID {
			return http.StatusBadRequest, denied
		}

		return http.StatusOK, map[string]any{"access_token": "acc-2", "expires_in": 3600, "token_type": "bearer"}
	case "refresh_token":
		if f.Get("refresh_token") != fakeRefresh || f.Get("client_id") != fakeClientID {
			return http.StatusBadRequest, denied
		}

		return http.StatusOK, map[string]any{"access_token": "acc-3", "expires_in": 1800, "token_type": "bearer"}
	}

	return http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"}
}

func (o *fakeOAuth) forms() []url.Values {
	o.mu.Lock()
	defer o.mu.Unlock()

	return slices.Clone(o.tokenForms)
}

func (o *fakeOAuth) requests() []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	return slices.Clone(o.reqs)
}

func (o *fakeOAuth) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.tokenForms, o.reqs = nil, nil
}

func (o *fakeOAuth) setHold(f func()) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.hold = f
}

func (o *fakeOAuth) setReply(f func(url.Values) (int, map[string]any)) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.reply = f
}

func testProvider() Provider {
	return (&fakeOAuth{srv: &httptest.Server{URL: "https://oauth.test"}}).provider()
}

func cloudError(t *testing.T, err error) (ce cloudhttp.CloudError) {
	t.Helper()

	ce, ok := errors.AsType[cloudhttp.CloudError](err)
	if !ok {
		t.Fatalf("err = %v, want cloudhttp.CloudError", err)
	}

	return ce
}

func describeToken(t usecase.CloudToken) string {
	return fmt.Sprintf("{%s %s %s %s %s %v}", t.Provider, t.ClientID, t.Access, t.Refresh, t.Account, t.Expiry)
}
