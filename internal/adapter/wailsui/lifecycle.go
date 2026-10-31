package wailsui

import (
	"sync"
	"sync/atomic"
)

// Lifecycle decides whether the app may quit (spec §7.3). Interactive quits
// (closing the window, Cmd+Q) first ask the interface, which runs the close
// scenario and calls ConfirmQuit; signals and the end of the session quit
// without asking.
type Lifecycle struct {
	allowQuit    atomic.Bool
	requestClose func()
	shutdown     func()
	once         sync.Once
}

// NewLifecycle returns a Lifecycle. requestClose asks the interface to run
// the close scenario; shutdown quits the application, which then runs the
// one shutdown function of every path.
func NewLifecycle(requestClose, shutdown func()) *Lifecycle {
	return &Lifecycle{requestClose: requestClose, shutdown: shutdown}
}

// OnQuitRequested is the window close hook and ShouldQuit: it returns true
// once quitting is allowed; otherwise it asks the interface and returns false.
func (l *Lifecycle) OnQuitRequested() bool {
	if l.allowQuit.Load() {
		return true
	}

	l.requestClose()

	return false
}

// ConfirmQuit allows quitting and quits; the interface calls it at the end of
// the close scenario.
func (l *Lifecycle) ConfirmQuit() { l.quit() }

// ForceQuit quits without asking, for SIGTERM, SIGINT and the end of the session.
func (l *Lifecycle) ForceQuit() { l.quit() }

func (l *Lifecycle) quit() {
	l.allowQuit.Store(true)
	l.once.Do(l.shutdown)
}
