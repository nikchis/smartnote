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

type fakeTokenStore struct {
	mu      sync.Mutex
	tok     CloudToken
	has     bool
	loadErr error
	saveErr error
	saves   []CloudToken
}

func (f *fakeTokenStore) LoadCloudToken(context.Context) (CloudToken, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tok, f.has, f.loadErr
}

func (f *fakeTokenStore) SaveCloudToken(_ context.Context, t CloudToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.saveErr != nil {
		return f.saveErr
	}

	f.saves = append(f.saves, t)

	return nil
}

func TestStoredSourceToken(t *testing.T) {
	stored := CloudToken{Provider: "yandex", ClientID: "a", Access: "x", Expiry: time.Unix(10, 0)}
	loadErr := errors.New("disk")

	tests := []struct {
		name     string
		store    CloudTokenStore
		clientID string
		want     CloudToken
		wantErr  error
		local    bool
	}{
		{name: "nil store", store: nil, clientID: "a", wantErr: domain.ErrReauthRequired},
		{name: "no token", store: &fakeTokenStore{}, clientID: "a", wantErr: domain.ErrReauthRequired},
		{name: "other client id", store: &fakeTokenStore{tok: stored, has: true}, clientID: "b",
			wantErr: domain.ErrReauthRequired},
		{name: "load error", store: &fakeTokenStore{loadErr: loadErr}, clientID: "a", wantErr: loadErr, local: true},
		{name: "ok", store: &fakeTokenStore{tok: stored, has: true}, clientID: "a", want: stored},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewStoredTokenSource(tc.store, tc.clientID).Token(context.Background())
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

func TestStoredSourceNoToken(t *testing.T) {
	_, err := NewStoredTokenSource(&fakeTokenStore{}, "a").Token(context.Background())
	if !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestStoredSourceOtherClientID(t *testing.T) {
	store := &fakeTokenStore{tok: CloudToken{ClientID: "a"}, has: true}

	_, err := NewStoredTokenSource(store, "b").Token(context.Background())
	if !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemorySourceClientMismatch(t *testing.T) {
	src := NewMemoryTokenSource(CloudToken{ClientID: "a"}, "b")

	if _, err := src.Token(context.Background()); !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestEffectiveClientID(t *testing.T) {
	if got := EffectiveClientID(domain.ConnectionSettings{YandexClientID: "own"}, "builtin"); got != "own" {
		t.Fatalf("got %q", got)
	}

	if got := EffectiveClientID(domain.ConnectionSettings{}, "builtin"); got != "builtin" {
		t.Fatalf("got %q", got)
	}
}

func TestCloudTokenRedacted(t *testing.T) {
	tok := CloudToken{Provider: "yandex", ClientID: "cid", Access: "ACCESS-SECRET", Refresh: "REFRESH-SECRET", Account: "me"}

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
