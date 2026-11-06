package platform

import (
	"testing"

	"golang.org/x/sys/unix"
)

func dumpable(t *testing.T) int {
	t.Helper()

	d, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("PR_GET_DUMPABLE: %v", err)
	}

	return d
}

func setDumpable(t *testing.T, v int) {
	t.Helper()

	if err := unix.Prctl(unix.PR_SET_DUMPABLE, uintptr(v), 0, 0, 0); err != nil {
		t.Fatalf("PR_SET_DUMPABLE %d: %v", v, err)
	}
}

// The tests change the dumpable flag of the test process itself, so they do
// not run in parallel and put the flag back.
func keepDumpable(t *testing.T) {
	t.Helper()

	was := dumpable(t)
	t.Cleanup(func() { setDumpable(t, was) })
}

func open(t *testing.T) func() error {
	t.Helper()

	restore, err := OpenToPortal()
	if err != nil {
		t.Fatalf("OpenToPortal: %v", err)
	}

	return restore
}

func TestOpenToPortalLiftsHardeningUntilTheLastDialogCloses(t *testing.T) {
	keepDumpable(t)
	setDumpable(t, 0)

	first := open(t)
	if got := dumpable(t); got != 1 {
		t.Fatalf("dumpable while a dialog is open = %d, want 1", got)
	}

	second := open(t)

	if err := first(); err != nil {
		t.Fatal(err)
	}

	if got := dumpable(t); got != 1 {
		t.Fatalf("dumpable while the second dialog is open = %d, want 1", got)
	}

	if err := second(); err != nil {
		t.Fatal(err)
	}

	if got := dumpable(t); got != 0 {
		t.Fatalf("dumpable after the last dialog = %d, want 0", got)
	}
}

func TestOpenToPortalRestoreIsIdempotent(t *testing.T) {
	keepDumpable(t)
	setDumpable(t, 0)

	restore := open(t)
	other := open(t)

	if err := restore(); err != nil {
		t.Fatal(err)
	}

	if err := restore(); err != nil { // a second call must not count as the other dialog
		t.Fatal(err)
	}

	if got := dumpable(t); got != 1 {
		t.Fatalf("dumpable while the other dialog is open = %d, want 1", got)
	}

	if err := other(); err != nil {
		t.Fatal(err)
	}

	if got := dumpable(t); got != 0 {
		t.Fatalf("dumpable after both dialogs = %d, want 0", got)
	}
}

func TestOpenToPortalKeepsAProcessThatWasNotHardened(t *testing.T) {
	keepDumpable(t)
	setDumpable(t, 1)

	restore := open(t)
	if err := restore(); err != nil {
		t.Fatal(err)
	}

	if got := dumpable(t); got != 1 {
		t.Fatalf("dumpable = %d, want 1: the process was not hardened", got)
	}
}
