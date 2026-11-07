package wailsui

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// deadlineRemote records how long each Open may take and opens an empty
// session whose Backup succeeds.
type deadlineRemote struct {
	left []time.Duration
}

func (r *deadlineRemote) Open(ctx context.Context, _ usecase.RemoteTarget, _ usecase.OpenMode) (usecase.RemoteSession, error) {
	if dl, ok := ctx.Deadline(); ok {
		r.left = append(r.left, time.Until(dl))
	} else {
		r.left = append(r.left, -1)
	}

	return deadlineSession{}, nil
}

type deadlineSession struct{}

func (deadlineSession) Created() bool { return false }
func (deadlineSession) Empty() bool   { return false }
func (deadlineSession) Close() error  { return nil }
func (deadlineSession) Snapshot(context.Context) (usecase.RemoteSnapshot, error) {
	return usecase.RemoteSnapshot{Revision: 1, ServerID: uuid.New()}, nil
}

func (deadlineSession) Backup(context.Context, []domain.Note, uuid.UUID, int64) (int64, error) {
	return 2, nil
}

type emptyLocal struct{}

func (emptyLocal) LocalSnapshot(context.Context) (usecase.LocalSnapshot, error) {
	return usecase.LocalSnapshot{}, nil
}

func (emptyLocal) ApplyRestore(context.Context, usecase.RestorePlan, int64, uuid.UUID) error {
	return nil
}

func (emptyLocal) SetSyncState(context.Context, uuid.UUID, int64) error { return nil }

// wantLeft checks that every recorded Open had about want to run.
func wantLeft(t *testing.T, left []time.Duration, wantCalls int, want time.Duration) {
	t.Helper()

	if len(left) != wantCalls {
		t.Fatalf("Open calls = %d, want %d", len(left), wantCalls)
	}

	for i, l := range left {
		if l > want || l < want-5*time.Second {
			t.Errorf("Open %d had %v, want about %v", i, l, want)
		}
	}
}

var yandexSaved = domain.ConnectionSettings{
	StorageKind: domain.KindYandexDisk, YandexClientID: "own", SyncPhrase: strings.Repeat("p", domain.MinSyncPhraseLen),
}

func TestSyncServiceTimeoutByKind(t *testing.T) {
	tests := []struct {
		name  string
		saved domain.ConnectionSettings
		want  time.Duration
	}{
		{"yandex disk", yandexSaved, 600 * time.Second},
		{"ssh postgres", filledSettings, 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memSettings{s: tc.saved, token: usecase.CloudToken{ClientID: "own"}, hasToken: true}
			remote := &deadlineRemote{}

			syncer, err := usecase.NewSync(usecase.SyncDeps{
				Local: emptyLocal{}, Settings: store, Remote: remote, Changes: &usecase.ChangeTracker{},
				IDs: uuid.New, Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Warn: func(error) {},
				Tokens: store, BuiltinClientID: "builtin",
			})
			if err != nil {
				t.Fatal(err)
			}

			env := Env{
				Ctx: t.Context(), Log: slog.New(slog.DiscardHandler),
				SavedKind: func(ctx context.Context) (domain.StorageKind, error) {
					s, err := store.LoadSettings(ctx)
					return s.Kind(), err
				},
			}
			svc := NewSyncService(env, syncer, nil)

			if _, err := svc.Connect(); err != nil {
				t.Fatalf("Connect: %v", err)
			}

			if _, err := svc.PrepareBackup(); err != nil {
				t.Fatalf("PrepareBackup: %v", err)
			}

			if err := svc.CommitBackup(); err != nil {
				t.Fatalf("CommitBackup: %v", err)
			}

			wantLeft(t, remote.left, 3, tc.want)
		})
	}
}

func TestConnectionServiceTestTimeoutByKind(t *testing.T) {
	tests := []struct {
		name string
		dto  SettingsDTO
		want time.Duration
	}{
		{"yandex disk", SettingsDTO{Kind: string(domain.KindYandexDisk), YandexClientID: "own", SyncPhrase: yandexSaved.SyncPhrase}, 600 * time.Second},
		{"ssh postgres", SettingsDTO{
			Kind: string(domain.KindSSHPostgres), SSHHost: "server", SSHPort: 22, SSHUser: "u", DBHost: "127.0.0.1",
			DBPort: 5432, DBName: "db", DBUser: "dbu", SyncPhrase: "sixteen chars ok",
		}, 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memSettings{token: usecase.CloudToken{ClientID: "own"}, hasToken: true}
			remote := &deadlineRemote{}

			conn, err := usecase.NewConnection(usecase.ConnectionDeps{
				Settings: store, Store: store, Tokens: store, Remote: remote,
				Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Changed: func() {},
			})
			if err != nil {
				t.Fatal(err)
			}

			// The saved kind is none: Test must use the kind of the form.
			svc := NewConnectionService(Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}, conn, nil)

			if _, err := svc.Test(tc.dto); err != nil {
				t.Fatalf("Test: %v", err)
			}

			wantLeft(t, remote.left, 1, tc.want)
		})
	}
}
