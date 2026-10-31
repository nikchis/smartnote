// Package platform finds Smartnote's directories and guards them against a
// second running instance.
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Values of runtime.GOOS with their own directories; any other system uses XDG.
const (
	goosWindows = "windows"
	goosDarwin  = "darwin"
)

// appDir is the name of Smartnote's directory inside the per-user base directories.
const appDir = "smartnote"

// DataDir returns the directory of the local storage:
// Linux $XDG_DATA_HOME/smartnote (default ~/.local/share/smartnote),
// Windows %APPDATA%\smartnote, macOS ~/Library/Application Support/smartnote.
// The directory may not exist yet.
func DataDir() (string, error) {
	switch runtime.GOOS {
	case goosWindows:
		return envDir("APPDATA")
	case goosDarwin:
		return homeDir("Library", "Application Support")
	default:
		return xdgDir("XDG_DATA_HOME", ".local", "share")
	}
}

// StateDir returns the directory of the error log:
// Linux $XDG_STATE_HOME/smartnote (default ~/.local/state/smartnote),
// Windows %LOCALAPPDATA%\smartnote, macOS ~/Library/Logs/smartnote.
// The directory may not exist yet.
func StateDir() (string, error) {
	switch runtime.GOOS {
	case goosWindows:
		return envDir("LOCALAPPDATA")
	case goosDarwin:
		return homeDir("Library", "Logs")
	default:
		return xdgDir("XDG_STATE_HOME", ".local", "state")
	}
}

// xdgDir follows the XDG Base Directory specification: the variable counts
// only when it holds an absolute path, otherwise the default under home is used.
func xdgDir(env string, fallback ...string) (string, error) {
	if base := os.Getenv(env); filepath.IsAbs(base) {
		return filepath.Join(base, appDir), nil
	}

	return homeDir(fallback...)
}

func envDir(env string) (string, error) {
	base := os.Getenv(env)
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("%%%s%% is not an absolute path", env)
	}

	return filepath.Join(base, appDir), nil
}

func homeDir(elem ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}

	if !filepath.IsAbs(home) {
		return "", errors.New("home directory is not an absolute path")
	}

	return filepath.Join(append(append([]string{home}, elem...), appDir)...), nil
}
