package usecase

import (
	"slices"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

func TestThrottleProgress(t *testing.T) {
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	at := start

	var got []int64

	report := ThrottleProgress(func(p TransferProgress) { got = append(got, p.Done) }, func() time.Time { return at })

	steps := []struct {
		after time.Duration
		done  int64
	}{
		{0, 0},                       // the first report passes
		{50 * time.Millisecond, 10},  // too soon
		{99 * time.Millisecond, 20},  // too soon
		{100 * time.Millisecond, 30}, // 100 ms after the last one passed
		{150 * time.Millisecond, 40}, // too soon
		{160 * time.Millisecond, 50}, // the final one always passes
	}

	for _, s := range steps {
		at = start.Add(s.after)
		report(TransferProgress{Storage: domain.KindYandexDisk, Direction: DirectionUpload, Unit: UnitBytes, Done: s.done, Total: 50})
	}

	if want := []int64{0, 30, 50}; !slices.Equal(got, want) {
		t.Errorf("reports = %v, want %v", got, want)
	}
}

func TestThrottleProgressNil(_ *testing.T) {
	report := ThrottleProgress(nil, time.Now)
	report(TransferProgress{Done: 1, Total: 1}) // must not panic
}
