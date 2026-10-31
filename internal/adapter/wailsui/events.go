package wailsui

import "github.com/wailsapp/wails/v3/pkg/application"

// Events sent to the interface.
const (
	// EventSyncState carries the StateDTO after every change of the synchronization state.
	EventSyncState = "sync:state"
	// EventCloseRequested asks the interface to run the close scenario; it has no data.
	EventCloseRequested = "app:close-requested"
)

//nolint:gochecknoinits // Wails documents init as the place where the binding generator finds events
func init() {
	application.RegisterEvent[StateDTO](EventSyncState)
	application.RegisterEvent[application.Void](EventCloseRequested)
}
