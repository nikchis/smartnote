package usecase

import (
	"sync"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

// Directions and units of TransferProgress.
const (
	DirectionUpload   = "upload"
	DirectionDownload = "download"
	// DirectionNone is the end of the operation: nothing is transferred.
	DirectionNone = ""

	UnitBytes = "bytes"
	UnitNotes = "notes"
)

// progressInterval is the shortest time between two reports of a transfer
// (at most 10 a second).
const progressInterval = 100 * time.Millisecond

// TransferProgress is how far a transfer with the remote storage has got:
// Done of Total units sent or received.
type TransferProgress struct {
	Storage     domain.StorageKind
	Direction   string // DirectionUpload, DirectionDownload or DirectionNone
	Unit        string // UnitBytes or UnitNotes
	Done, Total int64
}

// ProgressFunc receives the progress of transfers. The adapters that report
// it are the remote storages; main passes it on to the interface.
type ProgressFunc func(TransferProgress)

// ThrottleProgress returns a ProgressFunc that passes the reports of one
// transfer to f at most once per 100 ms of now: the first report and a final
// one (Done >= Total) always pass. A nil f gives a ProgressFunc that does
// nothing. The result is safe for concurrent use.
func ThrottleProgress(f ProgressFunc, now func() time.Time) ProgressFunc {
	if f == nil {
		return func(TransferProgress) {}
	}

	var (
		mu   sync.Mutex
		last time.Time
		sent bool
	)

	return func(p TransferProgress) {
		mu.Lock()

		t := now()
		pass := !sent || p.Done >= p.Total || t.Sub(last) >= progressInterval

		if pass {
			last, sent = t, true
		}

		mu.Unlock()

		if pass {
			f(p)
		}
	}
}
