package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Harden disables core dumps and denies debugger attachment to the process.
// Both measures are tried; the errors of failed ones are joined.
func Harden() error {
	var errs []error

	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		errs = append(errs, fmt.Errorf("setrlimit RLIMIT_CORE: %w", err))
	}

	if err := unix.PtraceDenyAttach(); err != nil {
		errs = append(errs, fmt.Errorf("ptrace PT_DENY_ATTACH: %w", err))
	}

	return errors.Join(errs...)
}
