package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

// fakeTokenStore keeps one token per provider, as local.db does.
type fakeTokenStore struct {
	mu      sync.Mutex
	toks    map[domain.StorageKind]CloudToken
	loadErr error
	saveErr error
	saves   []CloudToken
	// saveCtxErr is the context error SaveCloudToken last saw.
	saveCtxErr error
}

// tokensOf returns a store holding toks under their providers.
func tokensOf(toks ...CloudToken) *fakeTokenStore {
	f := &fakeTokenStore{}
	for _, t := range toks {
		f.putLocked(t)
	}

	return f
}

func (f *fakeTokenStore) putLocked(t CloudToken) {
	if f.toks == nil {
		f.toks = map[domain.StorageKind]CloudToken{}
	}

	f.toks[t.Provider] = t
}

// token returns the stored token of p.
func (f *fakeTokenStore) token(p domain.StorageKind) (CloudToken, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	t, ok := f.toks[p]

	return t, ok
}

func (f *fakeTokenStore) saveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.saves)
}

func (f *fakeTokenStore) LoadCloudToken(_ context.Context, p domain.StorageKind) (CloudToken, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.loadErr != nil {
		return CloudToken{}, false, f.loadErr
	}

	t, ok := f.toks[p]

	return t, ok, nil
}

func (f *fakeTokenStore) SaveCloudToken(ctx context.Context, t CloudToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.saveCtxErr = ctx.Err()

	if f.saveErr != nil {
		return f.saveErr
	}

	f.saves = append(f.saves, t)
	f.putLocked(t)

	return nil
}

// Clients of the tests.
var (
	yandexA = CloudClient{Provider: domain.KindYandexDisk, ID: "a"}
	googleA = CloudClient{Provider: domain.KindGoogleDrive, ID: "a", Secret: "sec"}
)

func TestStoredSourceToken(t *testing.T) {
	stored := CloudToken{Provider: domain.KindYandexDisk, ClientID: "a", Access: "x", Expiry: time.Unix(10, 0)}
	loadErr := errors.New("disk")

	tests := []struct {
		name    string
		store   CloudTokenStore
		client  CloudClient
		want    CloudToken
		wantErr error
		local   bool
	}{
		{name: "nil store", store: nil, client: yandexA, wantErr: domain.ErrReauthRequired},
		{name: "no token", store: tokensOf(), client: yandexA, wantErr: domain.ErrReauthRequired},
		{name: "other client id", store: tokensOf(stored), client: CloudClient{Provider: domain.KindYandexDisk, ID: "b"},
			wantErr: domain.ErrReauthRequired},
		{name: "token of another provider", store: tokensOf(stored), client: googleA, wantErr: domain.ErrReauthRequired},
		{name: "load error", store: &fakeTokenStore{loadErr: loadErr}, client: yandexA, wantErr: loadErr, local: true},
		{name: "ok", store: tokensOf(stored), client: yandexA, want: stored},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewStoredTokenSource(tc.store, tc.client).Token(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if _, ok := errors.AsType[LocalError](err); ok != tc.local {
				t.Fatalf("LocalError = %v, want %v", ok, tc.local)
			}

			if got != tc.want {
				t.Fatalf("token = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestStoredSourceMissingTokenIsReauthOfKind: the path of Test, Backup and
// Restore without a token asks to sign in to the provider of the source.
func TestStoredSourceMissingTokenIsReauthOfKind(t *testing.T) {
	for _, c := range []CloudClient{googleA, yandexA} {
		t.Run(string(c.Provider), func(t *testing.T) {
			for _, store := range []CloudTokenStore{nil, tokensOf()} {
				_, err := NewStoredTokenSource(store, c).Token(context.Background())

				re, ok := errors.AsType[*domain.ReauthRequiredError](err)
				if !ok || re.Kind != c.Provider {
					t.Errorf("Token = %v, want ReauthRequiredError{%s}", err, c.Provider)
				}
			}
		})
	}
}

func TestStoredSourceUpdateWritesLocalDB(t *testing.T) {
	store := tokensOf(CloudToken{Provider: domain.KindGoogleDrive, ClientID: "a", Access: "old", Refresh: "r1"})
	src := NewStoredTokenSource(store, googleA)

	// The operation's context may already be over: the write still happens.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Provider and ClientID come from the source, whatever the token says.
	if err := src.Update(ctx, CloudToken{Provider: "other", ClientID: "other", Access: "new", Refresh: "r2"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	want := CloudToken{Provider: domain.KindGoogleDrive, ClientID: "a", Access: "new", Refresh: "r2"}
	if len(store.saves) != 1 || store.saves[0] != want || store.saveCtxErr != nil {
		t.Fatalf("saves = %+v (context error %v), want %+v under a live context", store.saves, store.saveCtxErr, want)
	}

	if got, err := src.Token(context.Background()); err != nil || got != want {
		t.Errorf("Token after Update = %+v, %v; want %+v", got, err, want)
	}

	store.saveErr = errors.New("disk full")

	err := src.Update(context.Background(), want)
	if _, ok := errors.AsType[LocalError](err); !ok || !errors.Is(err, store.saveErr) {
		t.Errorf("Update with a failing store = %v, want LocalError", err)
	}
}

func TestMemorySourceClientMismatch(t *testing.T) {
	src := NewMemoryTokenSource(CloudToken{ClientID: "a"}, CloudClient{Provider: domain.KindGoogleDrive, ID: "b"})

	_, err := src.Token(context.Background())
	if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
		t.Fatalf("err = %v, want ReauthRequiredError{google_drive}", err)
	}
}

func TestMemorySourceUpdate(t *testing.T) {
	src := NewMemoryTokenSource(CloudToken{Provider: domain.KindYandexDisk, ClientID: "a", Access: "old"}, yandexA)

	if err := src.Update(context.Background(), CloudToken{Access: "new"}); err != nil {
		t.Fatal(err)
	}

	want := CloudToken{Provider: domain.KindYandexDisk, ClientID: "a", Access: "new"}
	if got, err := src.Token(context.Background()); err != nil || got != want {
		t.Errorf("Token after Update = %+v, %v; want %+v", got, err, want)
	}
}

func TestEffectiveClient(t *testing.T) {
	builtin := map[domain.StorageKind]CloudClient{
		domain.KindYandexDisk:  {ID: "y-builtin"},
		domain.KindGoogleDrive: {ID: "g-builtin", Secret: "g-secret"},
	}
	yandex := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk}
	google := domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive}

	withYandexID := yandex
	withYandexID.YandexClientID = "own"

	withGooglePair := google
	withGooglePair.GoogleClientID, withGooglePair.GoogleClientSecret = "own-g", "own-secret"

	// The Yandex ID of google settings is not a Google client.
	googleWithYandexID := google
	googleWithYandexID.YandexClientID = "own"

	halfPair := google
	halfPair.GoogleClientID = "own-g"

	tests := []struct {
		name    string
		s       domain.ConnectionSettings
		builtin map[domain.StorageKind]CloudClient
		want    CloudClient
		ok      bool
	}{
		{name: "yandex own", s: withYandexID, builtin: builtin,
			want: CloudClient{Provider: domain.KindYandexDisk, ID: "own"}, ok: true},
		{name: "yandex built-in", s: yandex, builtin: builtin,
			want: CloudClient{Provider: domain.KindYandexDisk, ID: "y-builtin"}, ok: true},
		{name: "yandex without any", s: yandex},
		{name: "google built-in", s: googleWithYandexID, builtin: builtin,
			want: CloudClient{Provider: domain.KindGoogleDrive, ID: "g-builtin", Secret: "g-secret"}, ok: true},
		{name: "google own pair", s: withGooglePair, builtin: builtin,
			want: CloudClient{Provider: domain.KindGoogleDrive, ID: "own-g", Secret: "own-secret"}, ok: true},
		{name: "google own id without secret", s: halfPair, builtin: builtin},
		{name: "ssh_postgres", s: completeSettings(), builtin: builtin},
		{name: "none", s: domain.ConnectionSettings{StorageKind: domain.KindNone}, builtin: builtin},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := EffectiveClient(tc.s, tc.builtin)
			if ok != tc.ok || got != tc.want {
				t.Errorf("EffectiveClient = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCloudClientRedacted(t *testing.T) {
	c := CloudClient{Provider: domain.KindGoogleDrive, ID: "id", Secret: "CLIENT-SECRET"}

	var buf bytes.Buffer

	slog.New(slog.NewJSONHandler(&buf, nil)).Info("client", "c", c)

	for name, out := range map[string]string{
		"%v": c.String(), "%+v": fmt.Sprintf("%+v", c), "%#v": fmt.Sprintf("%#v", c), "slog": buf.String(),
	} {
		if strings.Contains(out, "CLIENT-SECRET") {
			t.Errorf("%s output %q contains the secret", name, out)
		}
	}
}

func TestCloudTokenRedacted(t *testing.T) {
	tok := CloudToken{Provider: domain.KindYandexDisk, ClientID: "cid", Access: "ACCESS-SECRET", Refresh: "REFRESH-SECRET", Account: "me"}

	var buf bytes.Buffer

	slog.New(slog.NewJSONHandler(&buf, nil)).Info("token", "t", tok)

	outputs := map[string]string{
		"%v":   tok.String(),
		"%+v":  fmt.Sprintf("%+v", tok),
		"%#v":  fmt.Sprintf("%#v", tok),
		"slog": buf.String(),
	}
	for name, out := range outputs {
		for _, secret := range []string{tok.Access, tok.Refresh} {
			if strings.Contains(out, secret) {
				t.Errorf("%s output %q contains %q", name, out, secret)
			}
		}

		if !strings.Contains(out, "CloudToken{redacted}") {
			t.Errorf("%s output %q is not redacted", name, out)
		}
	}
}
