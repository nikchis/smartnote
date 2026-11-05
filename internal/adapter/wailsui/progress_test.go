package wailsui

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// eventLog records the events sent to the interface.
type eventLog struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name string
	data []any
}

func (l *eventLog) emit(name string, data ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, recordedEvent{name: name, data: data})
}

func (l *eventLog) list() []recordedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]recordedEvent(nil), l.events...)
}

func TestProgressFeedPublish(t *testing.T) {
	var log eventLog

	feed := NewProgressFeed(log.emit)
	feed.Publish(usecase.TransferProgress{
		Storage: domain.KindYandexDisk, Direction: usecase.DirectionUpload, Unit: usecase.UnitBytes, Done: 2_400_000, Total: 5_000_000,
	})
	feed.End()

	want := []recordedEvent{
		{name: EventSyncProgress, data: []any{ProgressDTO{Storage: "yandex_disk", Direction: "upload", Unit: "bytes", Done: 2_400_000, Total: 5_000_000}}},
		{name: EventSyncProgress, data: []any{ProgressDTO{}}},
	}

	if got := log.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

func TestProgressDTOJSON(t *testing.T) {
	b, err := json.Marshal(ProgressDTO{Storage: "ssh_postgres", Direction: "download", Unit: "notes", Done: 12, Total: 340})
	if err != nil {
		t.Fatal(err)
	}

	want := `{"storage":"ssh_postgres","direction":"download","unit":"notes","done":12,"total":340}`
	if string(b) != want {
		t.Errorf("JSON = %s, want %s", b, want)
	}
}

// TestSyncServiceEndsProgress checks that every operation of SyncService
// ends with the event that resets the progress in the interface, whatever
// its result.
func TestSyncServiceEndsProgress(t *testing.T) {
	store := &memSettings{s: filledSettings}

	syncer, err := usecase.NewSync(usecase.SyncDeps{
		Local: emptyLocal{}, Settings: store, Remote: &deadlineRemote{}, Changes: &usecase.ChangeTracker{},
		IDs: uuid.New, Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{}, Warn: func(error) {},
		Tokens: store, BuiltinClientID: "builtin",
	})
	if err != nil {
		t.Fatal(err)
	}

	var log eventLog

	svc := NewSyncService(Env{Ctx: context.Background(), Log: slog.New(slog.DiscardHandler)}, syncer, NewProgressFeed(log.emit))

	ops := []struct {
		name string
		run  func() error
	}{
		{"Connect", func() error { _, err := svc.Connect(); return err }},
		{"PrepareBackup", func() error { _, err := svc.PrepareBackup(); return err }},
		{"CommitBackup", svc.CommitBackup},
		{"PrepareRestore", func() error { _, err := svc.PrepareRestore(); return err }},
		{"CommitRestore", svc.CommitRestore}, // nothing planned: fails, and still ends
	}

	for _, op := range ops {
		before := len(log.list())
		_ = op.run() // only the events matter

		got := log.list()[before:]
		if len(got) == 0 || got[len(got)-1].name != EventSyncProgress || !reflect.DeepEqual(got[len(got)-1].data, []any{ProgressDTO{}}) {
			t.Errorf("%s: events %+v, want the end of the progress last", op.name, got)
		}
	}
}
