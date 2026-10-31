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

// OnAppQuit is the Wails ShouldQuit hook. sessionEnding reports that macOS
// asks to quit for logout, restart or shutdown: that quit is allowed at once,
// since refusing it cancels the logout. Any other quit is OnQuitRequested.
func (l *Lifecycle) OnAppQuit(sessionEnding bool) bool {
	if sessionEnding {
		l.allowQuit.Store(true)
		return true
	}

	return l.OnQuitRequested()
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

// wmEndSession is the Windows message sent when the session ends (winuser.h);
// a non-zero wParam means it really ends.
const wmEndSession = 0x0016

// OnWindowsMessage is the Wails WndProcInterceptor: at the end of the session
// it quits without asking, since Windows ends the process right after. It
// never consumes the message, so Wails handles it as well.
func (l *Lifecycle) OnWindowsMessage(_ uintptr, msg uint32, wParam, _ uintptr) (returnCode uintptr, shouldReturn bool) {
	if msg == wmEndSession && wParam != 0 {
		l.ForceQuit()
	}

	return 0, false
}
