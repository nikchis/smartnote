package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageDesktopFileIsNamedAfterAppID(t *testing.T) {
	nfpm, err := os.ReadFile(filepath.Join("..", "..", "build", "linux", "nfpm", "nfpm.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	want := `dst: "/usr/share/applications/` + linuxAppID + `.desktop"`
	if !strings.Contains(string(nfpm), want) {
		t.Errorf("nfpm.yaml does not install the .desktop file as %s.desktop; Wayland would not match the window with it", linuxAppID)
	}
}

func TestLinuxAppIDDiffersFromInstanceID(t *testing.T) {
	if linuxAppID == instanceID {
		t.Error("linuxAppID equals instanceID; GTK and the single instance check would claim the same D-Bus name")
	}
}
