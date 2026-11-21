package cloudhttp

import (
	"io"
	"sync"
)

// Meter counts the bytes of one transfer and reports the count. After Stop
// it reports nothing: the transport may read a request body from its own
// goroutine even after the request is over. A nil Meter counts nothing.
type Meter struct {
	report func(done int64)

	mu      sync.Mutex
	done    int64
	gen     int // the body being sent; Restart starts the next one
	stopped bool
}

// StartMeter reports 0 to report and returns a Meter that reports to it.
func StartMeter(report func(done int64)) *Meter {
	report(0)
	return &Meter{report: report}
}

// add counts n bytes read by a reader of generation gen; bytes of a body
// that Restart has replaced are not counted.
func (m *Meter) add(gen, n int) {
	if m == nil || n <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped || gen != m.gen {
		return
	}

	m.done += int64(n)
	m.report(m.done)
}

// Restart counts from 0 again: the body is sent anew. A reader of the
// earlier body, which the transport may still read, adds nothing.
func (m *Meter) Restart() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.done = 0
	m.gen++
}

// Wrap returns r counted by the current generation of m; r itself when m is
// nil.
func (m *Meter) Wrap(r io.Reader) io.Reader {
	if m == nil {
		return r
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return meteredReader{r: r, m: m, gen: m.gen}
}

// Stop ends the reports.
func (m *Meter) Stop() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopped = true
}

// meteredReader counts what is read from r with m.
type meteredReader struct {
	r   io.Reader
	m   *Meter
	gen int
}

func (r meteredReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.m.add(r.gen, n)

	return n, err
}
