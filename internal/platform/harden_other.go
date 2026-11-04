//go:build !linux && !darwin && !windows

package platform

// Harden does nothing on systems without a hardening recipe.
func Harden() error {
	return nil
}
