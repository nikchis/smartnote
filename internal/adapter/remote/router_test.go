package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

type tagConnector struct{ tag string }

func (c tagConnector) Open(context.Context, usecase.RemoteTarget, usecase.OpenMode) (usecase.RemoteSession, error) {
	return nil, errors.New(c.tag)
}

func TestRouterDispatch(t *testing.T) {
	tests := []struct {
		name     string
		kind     domain.StorageKind
		wantTag  string
		wantSame error
	}{
		{name: "ssh", kind: domain.KindSSHPostgres, wantTag: "ssh"},
		{name: "yandex", kind: domain.KindYandexDisk, wantTag: "yandex"},
		{name: "none", kind: domain.KindNone, wantSame: domain.ErrNotConnected},
		{name: "unknown", kind: "future", wantSame: domain.ErrUnsupportedFormat},
	}

	r := NewRouter(tagConnector{"ssh"}, tagConnector{"yandex"})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Open(t.Context(), usecase.RemoteTarget{Settings: domain.ConnectionSettings{StorageKind: tc.kind}}, usecase.OpenCheck)
			if err == nil {
				t.Fatal("Open succeeded, want an error")
			}

			if tc.wantSame != nil {
				if !errors.Is(err, tc.wantSame) {
					t.Errorf("err = %v, want %v", err, tc.wantSame)
				}

				return
			}

			if err.Error() != tc.wantTag {
				t.Errorf("routed to %q, want %q", err, tc.wantTag)
			}
		})
	}
}
