package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAcquireLockTwice(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}

	if _, err := AcquireLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second AcquireLock: err = %v, want ErrLocked", err)
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	again, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock after release: %v", err)
	}

	if err := again(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestAcquireLockCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "smartnote")

	release, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	t.Cleanup(func() { _ = release() })

	info, err := os.Stat(filepath.Join(dir, "smartnote.lock"))
	if err != nil {
		t.Fatalf("lock file: %v", err)
	}

	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("lock file mode = %o, want 600", perm)
		}
	}
}
