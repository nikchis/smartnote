//go:build !linux

package platform

// KeepGTKOffPortals does nothing outside Linux: there is no GTK and no
// xdg-desktop-portal there.
func KeepGTKOffPortals() (restore func()) {
	return func() {}
}
