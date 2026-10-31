package wailsui

import (
	"sync"
	"testing"
)

func TestLifecycle(t *testing.T) {
	var requests, shutdowns int

	l := NewLifecycle(func() { requests++ }, func() { shutdowns++ })

	if l.OnQuitRequested() {
		t.Error("first OnQuitRequested = true, want false: the UI must ask first")
	}

	if requests != 1 {
		t.Errorf("requestClose calls = %d, want 1", requests)
	}

	l.ConfirmQuit()

	if shutdowns != 1 {
		t.Errorf("shutdown calls after ConfirmQuit = %d, want 1", shutdowns)
	}

	if !l.OnQuitRequested() {
		t.Error("OnQuitRequested after ConfirmQuit = false, want true")
	}

	if requests != 1 {
		t.Errorf("requestClose calls = %d, want 1: quitting is already allowed", requests)
	}
}

func TestLifecycleForceQuitOnce(t *testing.T) {
	var (
		mu        sync.Mutex
		shutdowns int
	)

	l := NewLifecycle(func() { t.Error("ForceQuit asked the UI") }, func() {
		mu.Lock()
		defer mu.Unlock()

		shutdowns++
	})

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(l.ForceQuit)
	}

	wg.Wait()

	if shutdowns != 1 {
		t.Errorf("shutdown calls = %d, want 1", shutdowns)
	}

	if !l.OnQuitRequested() {
		t.Error("OnQuitRequested after ForceQuit = false, want true")
	}
}

func TestLifecycleWindowsSessionEnd(t *testing.T) {
	const (
		wmQueryEndSession = 0x0011
		wmEndSession      = 0x0016
		wmClose           = 0x0010
	)

	tests := []struct {
		name    string
		msg     uint32
		wParam  uintptr
		quits   int
		allowed bool
	}{
		{name: "session ends", msg: wmEndSession, wParam: 1, quits: 1, allowed: true},
		{name: "session end canceled", msg: wmEndSession, wParam: 0},
		{name: "query only", msg: wmQueryEndSession, wParam: 0},
		{name: "other message", msg: wmClose, wParam: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests, quits int

			l := NewLifecycle(func() { requests++ }, func() { quits++ })

			code, handled := l.OnWindowsMessage(0, tc.msg, tc.wParam, 0)
			if code != 0 || handled {
				t.Errorf("OnWindowsMessage = %d, %v; want 0, false so that Wails handles the message too", code, handled)
			}

			if quits != tc.quits {
				t.Errorf("shutdown calls = %d, want %d", quits, tc.quits)
			}

			if requests != 0 {
				t.Errorf("requestClose calls = %d, want 0: the session end does not ask", requests)
			}

			if tc.allowed && !l.OnQuitRequested() {
				t.Error("OnQuitRequested after the session end = false, want true")
			}
		})
	}
}
