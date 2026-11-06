package platform

import (
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

// portal counts the open file dialogs and remembers whether Harden was in
// effect when the first of them opened.
var portal struct {
	sync.Mutex

	open     int
	hardened bool
}

// OpenToPortal lets xdg-desktop-portal see the process while a file dialog
// is open. The portal identifies its caller by /proc/<pid>/root, which a
// non-dumpable process (Harden) keeps from it: the portal then refuses the
// request and GTK waits for an answer that never comes. The process is made
// dumpable for the time of the dialog; restore, called when the dialog
// closes, hardens it again once no other dialog is open; it must be called
// on every path, and while the dialog stays open the process stays dumpable.
// If restore fails, the process stays dumpable. Calling restore more than
// once has no further effect.
func OpenToPortal() (restore func() error, err error) {
	portal.Lock()
	defer portal.Unlock()

	if portal.open == 0 {
		d, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("prctl PR_GET_DUMPABLE: %w", err)
		}

		portal.hardened = d == 0
		if portal.hardened {
			if err := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); err != nil {
				return nil, fmt.Errorf("prctl PR_SET_DUMPABLE 1: %w", err)
			}
		}
	}

	portal.open++

	var once sync.Once

	return func() error {
		var err error

		once.Do(func() { err = closePortal() })

		return err
	}, nil
}

// closePortal ends one open dialog and hardens the process again after the
// last one.
func closePortal() error {
	portal.Lock()
	defer portal.Unlock()

	portal.open--
	if portal.open > 0 || !portal.hardened {
		return nil
	}

	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl PR_SET_DUMPABLE 0: %w", err)
	}

	return nil
}
