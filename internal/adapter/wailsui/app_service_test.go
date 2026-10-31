package wailsui

import "testing"

func TestAppServiceReady(t *testing.T) {
	var requests int

	l := NewLifecycle(func() { requests++ }, func() {})
	s := NewAppService(l)

	s.Ready()

	if l.OnQuitRequested() {
		t.Error("OnQuitRequested after AppService.Ready = true, want false: the UI must ask first")
	}

	if requests != 1 {
		t.Errorf("requestClose calls = %d, want 1", requests)
	}
}
