package platform

import (
	"os"
	"slices"
	"strings"
	"sync"
)

// gdkDebug is the variable GTK 4 reads its debug flags from when it opens
// the display; the flag no-portals makes it draw the file dialogs itself.
const (
	gdkDebug  = "GDK_DEBUG"
	noPortals = "no-portals"
)

// KeepGTKOffPortals makes GTK draw its file dialogs in the process instead
// of asking xdg-desktop-portal. The portal identifies its caller through
// /proc/<pid>/root, which the hardening (Harden) closes: the portal refuses,
// and GTK 4 then waits for an answer that never comes. Opening the process
// to the portal for the time of a dialog would also open its memory to the
// processes of the user, and a descriptor of /proc/<pid>/mem taken meanwhile
// stays readable after the hardening returns (audit of 2f51241, item 1).
//
// Call it before GTK starts. restore puts GDK_DEBUG back as it was, so that
// the programs Smartnote starts, such as the browser, do not inherit the
// flag; call it once GTK has opened the display. Calling restore more than
// once has no further effect.
func KeepGTKOffPortals() (restore func()) {
	old, had := os.LookupEnv(gdkDebug)
	_ = os.Setenv(gdkDebug, withNoPortals(old)) // cannot fail for this name and value

	var once sync.Once

	return func() {
		once.Do(func() {
			if had {
				_ = os.Setenv(gdkDebug, old)
			} else {
				_ = os.Unsetenv(gdkDebug)
			}
		})
	}
}

// withNoPortals adds no-portals to the comma-separated flags of GDK_DEBUG.
func withNoPortals(flags string) string {
	if flags == "" {
		return noPortals
	}

	if slices.Contains(strings.Split(flags, ","), noPortals) {
		return flags
	}

	return flags + "," + noPortals
}
