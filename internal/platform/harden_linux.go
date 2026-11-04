package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Harden keeps the memory of the process out of reach: no core dumps, and no
// ptrace or /proc/<pid>/mem access for other processes of the same user.
// Both measures are tried; the errors of failed ones are joined.
func Harden() error {
	var errs []error

	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		errs = append(errs, fmt.Errorf("prctl PR_SET_DUMPABLE: %w", err))
	}

	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		errs = append(errs, fmt.Errorf("setrlimit RLIMIT_CORE: %w", err))
	}

	return errors.Join(errs...)
}
