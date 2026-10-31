package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// ErrLocked means another Smartnote instance holds the lock.
var ErrLocked = errors.New("another instance of Smartnote is running")

// lockFile is the name of the lock file in the data directory.
const lockFile = "smartnote.lock"

// AcquireLock takes the exclusive lock file smartnote.lock in dir, creating
// dir when needed, without waiting. If another process holds it, it returns
// ErrLocked. release frees the lock; the file stays, since removing it would
// let a third instance lock a new file while the second still holds the old one.
func AcquireLock(dir string) (release func() error, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	lock := flock.New(filepath.Join(dir, lockFile), flock.SetPermissions(0o600))

	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", lockFile, err)
	}

	if !locked {
		return nil, ErrLocked
	}

	return func() error {
		if err := lock.Unlock(); err != nil {
			return fmt.Errorf("unlock %s: %w", lockFile, err)
		}

		return nil
	}, nil
}
