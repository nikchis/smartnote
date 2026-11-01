package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestFitSize(t *testing.T) {
	tests := []struct {
		name         string
		work         application.Rect
		wantW, wantH int
		wantChanged  bool
	}{
		{"opens at 1440×960", application.Rect{Width: 1920, Height: 1040}, 1440, 960, false},
		{"fits with the frame", application.Rect{Width: 1456, Height: 1008}, windowWidth, windowHeight, false},
		// The frame needs room too: 1366 - 16 for the width, 768 - 48 for the height.
		{"small screen", application.Rect{Width: 1366, Height: 768}, 1350, 720, true},
		{"narrow screen", application.Rect{Width: 1280, Height: 1000}, 1264, 952, true},
		{"unknown screen", application.Rect{}, windowWidth, windowHeight, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h, changed := fitSize(windowWidth, windowHeight, tc.work)

			if w != tc.wantW || h != tc.wantH || changed != tc.wantChanged {
				t.Errorf("fitSize = %d×%d, %v; want %d×%d, %v", w, h, changed, tc.wantW, tc.wantH, tc.wantChanged)
			}
		})
	}
}
