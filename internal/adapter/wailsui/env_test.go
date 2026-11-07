package wailsui

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

// wantDeadline checks that ctx ends about want from now.
func wantDeadline(ctx context.Context, t *testing.T, want time.Duration) {
	t.Helper()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("context has no deadline, want %v", want)
	}

	if left := time.Until(dl); left > want || left < want-5*time.Second {
		t.Errorf("deadline in %v, want about %v", left, want)
	}
}

func TestOperationTimeoutByKind(t *testing.T) {
	tests := []struct {
		kind domain.StorageKind
		want time.Duration
	}{
		{domain.KindYandexDisk, 600 * time.Second},
		{domain.KindSSHPostgres, 60 * time.Second},
		{domain.KindNone, 60 * time.Second},
	}

	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}

	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			ctx, cancel := env.remoteFor(tc.kind)
			defer cancel()

			wantDeadline(ctx, t, tc.want)
		})
	}
}

func TestRemoteSavedPicksSavedKind(t *testing.T) {
	errLoad := errors.New("load failed")
	tests := []struct {
		name string
		kind func(context.Context) (domain.StorageKind, error)
		want time.Duration
	}{
		{"yandex", func(context.Context) (domain.StorageKind, error) { return domain.KindYandexDisk, nil }, 600 * time.Second},
		{"ssh", func(context.Context) (domain.StorageKind, error) { return domain.KindSSHPostgres, nil }, 60 * time.Second},
		{"load error", func(context.Context) (domain.StorageKind, error) { return domain.KindYandexDisk, errLoad }, 60 * time.Second},
		{"not wired", nil, 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler), SavedKind: tc.kind}

			ctx, cancel := env.remoteSaved()
			defer cancel()

			wantDeadline(ctx, t, tc.want)
		})
	}
}

// TestRemoteKeepsDefault: an operation that is not with the storage (the
// OAuth code exchange) keeps 60 s even when Yandex Disk is saved.
func TestRemoteKeepsDefault(t *testing.T) {
	env := Env{
		Ctx: t.Context(), Log: slog.New(slog.DiscardHandler),
		SavedKind: func(context.Context) (domain.StorageKind, error) { return domain.KindYandexDisk, nil },
	}

	ctx, cancel := env.remote()
	defer cancel()

	wantDeadline(ctx, t, 60*time.Second)
}
