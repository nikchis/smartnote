//go:build !linux || !cgo || gtk3

package main

// useWindowIcon does nothing here: Windows takes the logo from the
// executable's resources, GTK 3 builds from application.Options.Icon, and
// macOS windows have no title bar icon.
func useWindowIcon() error { return nil }
