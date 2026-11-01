package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAppIconMatchesBuild(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "build", "appicon.png"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(appIcon, src) {
		t.Error("cmd/smartnote/appicon.png differs from build/appicon.png; copy it again")
	}
}
