package searchworker

import (
	"slices"
	"testing"
	"time"
)

func TestRestartPolicyPausesThenGivesUp(t *testing.T) {
	p := newRestartPolicy(nil)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	var pauses []time.Duration

	for {
		pause, ok := p.failed(now)
		if !ok {
			break
		}

		pauses = append(pauses, pause)
		now = now.Add(pause + time.Second) // the next start fails a second later
	}

	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	if !slices.Equal(pauses, want) {
		t.Fatalf("pauses %v, want %v", pauses, want)
	}
}

func TestRestartPolicyForgetsOldFailures(t *testing.T) {
	p := newRestartPolicy([]time.Duration{time.Second, 2 * time.Second})
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	// One failure every 11 minutes: never more than one within 10 minutes.
	for i := range 20 {
		pause, ok := p.failed(now)
		if !ok || pause != time.Second {
			t.Fatalf("failure %d: pause %v, %t; want 1s, true", i, pause, ok)
		}

		now = now.Add(11 * time.Minute)
	}

	// Three within the window: more than the two pauses.
	p.failed(now)

	if _, ok := p.failed(now.Add(time.Minute)); !ok {
		t.Fatal("gave up after two failures within the window")
	}

	if _, ok := p.failed(now.Add(9 * time.Minute)); ok {
		t.Fatal("did not give up after three failures within the window")
	}
}
