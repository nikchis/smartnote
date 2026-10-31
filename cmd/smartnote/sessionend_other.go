//go:build !darwin

package main

// sessionEnding reports a quit for the end of a macOS session; elsewhere the
// end of the session reaches the app as a signal or a Windows message.
func sessionEnding() bool { return false }
