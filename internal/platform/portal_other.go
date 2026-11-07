//go:build !linux

package platform

// OpenToPortal does nothing outside Linux: there is no xdg-desktop-portal
// that the hardening keeps out.
func OpenToPortal() (restore func() error, err error) {
	return func() error { return nil }, nil
}
