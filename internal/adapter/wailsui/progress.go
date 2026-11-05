package wailsui

import "github.com/nikchis/smartnote/internal/usecase"

// ProgressDTO is the progress of a transfer with the remote storage, sent
// as EventSyncProgress. Direction "" ends the operation: the interface hides
// the progress. Unit is "bytes" or "notes".
type ProgressDTO struct {
	Storage   string `json:"storage"`
	Direction string `json:"direction"`
	Unit      string `json:"unit"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
}

// ProgressFeed sends the progress of the remote storages to the interface.
// Publish is the usecase.ProgressFunc of the adapters; SyncService calls End
// after each operation. It is a type of its own, not a method of a service,
// because Wails would let the interface call every exported method of a
// service.
type ProgressFeed struct {
	emit func(name string, data ...any)
}

// NewProgressFeed returns a feed that sends events with emit; nil sends nothing.
func NewProgressFeed(emit func(name string, data ...any)) *ProgressFeed {
	if emit == nil {
		emit = func(string, ...any) {}
	}

	return &ProgressFeed{emit: emit}
}

// Publish sends p to the interface.
func (f *ProgressFeed) Publish(p usecase.TransferProgress) {
	f.emit(EventSyncProgress, ProgressDTO{
		Storage: string(p.Storage), Direction: p.Direction, Unit: p.Unit, Done: p.Done, Total: p.Total,
	})
}

// End tells the interface that the operation is over.
func (f *ProgressFeed) End() {
	f.emit(EventSyncProgress, ProgressDTO{Direction: usecase.DirectionNone})
}
