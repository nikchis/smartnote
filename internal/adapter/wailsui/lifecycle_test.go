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
