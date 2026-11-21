package cloudhttp_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
)

func TestMeterIgnoresReplacedBody(t *testing.T) {
	var reports []int64

	m := cloudhttp.StartMeter(func(done int64) { reports = append(reports, done) })
	old := m.Wrap(bytes.NewReader(make([]byte, 100)))

	if _, err := io.CopyN(io.Discard, old, 60); err != nil {
		t.Fatal(err)
	}

	m.Restart()

	fresh := m.Wrap(bytes.NewReader(make([]byte, 100)))

	if _, err := io.CopyN(io.Discard, old, 40); err != nil { // the transport still reads the old body
		t.Fatal(err)
	}

	if _, err := io.Copy(io.Discard, fresh); err != nil {
		t.Fatal(err)
	}

	if last := reports[len(reports)-1]; last != 100 {
		t.Errorf("last report = %d, want 100 (reports %v)", last, reports)
	}

	for _, d := range reports {
		if d > 100 {
			t.Errorf("report %d exceeds the body length", d)
		}
	}
}

func TestMeterStopAndNil(t *testing.T) {
	var reports []int64

	m := cloudhttp.StartMeter(func(done int64) { reports = append(reports, done) })
	r := m.Wrap(bytes.NewReader(make([]byte, 10)))
	m.Stop()

	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}

	if len(reports) != 1 || reports[0] != 0 {
		t.Errorf("reports = %v, want only the initial 0", reports)
	}

	var nilMeter *cloudhttp.Meter

	nilMeter.Restart()
	nilMeter.Stop()

	if _, err := io.Copy(io.Discard, nilMeter.Wrap(bytes.NewReader(make([]byte, 3)))); err != nil {
		t.Fatal(err)
	}
}
