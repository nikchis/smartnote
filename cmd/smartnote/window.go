package main

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The main window opens larger than in 2.0.0 (1000×640): 1440×960, so that
// the dialogs fit inside it with a margin, but never larger than the work
// area of its screen.
const (
	windowWidth     = 1440
	windowHeight    = 960
	windowMinWidth  = 900
	windowMinHeight = 600
)

// The size of a window excludes its frame; this room is left for the frame
// and the title bar of the window manager.
const (
	frameWidth  = 16
	frameHeight = 48
)

// fitSize reduces width and height so that the window with its frame fits
// into the work area; changed reports whether anything was reduced. An empty
// work area (unknown screen) keeps the size.
func fitSize(width, height int, work application.Rect) (w, h int, changed bool) {
	w, h = width, height
	if work.Width > 0 && w > work.Width-frameWidth {
		w = work.Width - frameWidth
	}

	if work.Height > 0 && h > work.Height-frameHeight {
		h = work.Height - frameHeight
	}

	return w, h, w != width || h != height
}

// fitToScreen shrinks and centres the window once its page is ready if it
// does not fit into the work area of its screen.
func fitToScreen(window *application.WebviewWindow) {
	var once sync.Once

	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		once.Do(func() {
			screen, err := window.GetScreen()
			if err != nil || screen == nil {
				return
			}

			width, height := window.Size()
			if w, h, changed := fitSize(width, height, screen.WorkArea); changed {
				window.SetSize(w, h)
				window.Center()
			}
		})
	})
}
