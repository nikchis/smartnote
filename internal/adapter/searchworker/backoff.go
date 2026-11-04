package searchworker

import (
	"slices"
	"time"
)

// defaultBackoff are the pauses before the restarts after failures (spec §8.1).
var defaultBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}

// failureWindow is how long a failure counts against the restarts.
const failureWindow = 10 * time.Minute

// restartPolicy decides what follows a failure of the worker: the n-th
// failure within failureWindow is followed by pause n, and a failure past the
// last pause ends the restarts. With the default pauses the first failure
// starts the restarts and the engine gives up after 5 failed restarts within
// 10 minutes, at the sixth failure (spec §8.1, §11).
type restartPolicy struct {
	pauses   []time.Duration
	failures []time.Time // within the window, oldest first
}

// newRestartPolicy returns a policy with pauses, or the default ones when
// pauses is nil.
func newRestartPolicy(pauses []time.Duration) *restartPolicy {
	if pauses == nil {
		pauses = defaultBackoff
	}

	return &restartPolicy{pauses: slices.Clone(pauses)}
}

// failed records a failure at now. It returns the pause before the next start,
// or false when the engine must give up.
func (p *restartPolicy) failed(now time.Time) (time.Duration, bool) {
	cutoff := now.Add(-failureWindow)
	p.failures = slices.DeleteFunc(p.failures, func(t time.Time) bool { return !t.After(cutoff) })
	p.failures = append(p.failures, now)

	if len(p.failures) > len(p.pauses) {
		return 0, false
	}

	return p.pauses[len(p.failures)-1], true
}
