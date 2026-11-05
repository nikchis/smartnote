package searchworker

import (
	"slices"
	"testing"
)

func TestWorkerEnv(t *testing.T) {
	parent := []string{
		"HOME=/home/u",
		"PATH=/usr/bin",
		"ORT_LOGGING_LEVEL=0",
		"ORT_DISABLE_TELEMETRY=0",
		"Ort_Telemetry=1", // Windows names ignore case
		"LD_PRELOAD=/tmp/inject.so",
		"LD_AUDIT=/tmp/audit.so",
		"DYLD_INSERT_LIBRARIES=/tmp/inject.dylib",
		"LD_LIBRARY_PATH=/opt/app/usr/lib",
		"SMARTNOTE=1",
	}

	got := workerEnv(parent)

	want := []string{
		"HOME=/home/u",
		"PATH=/usr/bin",
		"LD_LIBRARY_PATH=/opt/app/usr/lib",
		"SMARTNOTE=1",
		"ORT_DISABLE_TELEMETRY=1",
		"GLIBC_TUNABLES=glibc.pthread.stack_cache_size=0",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("workerEnv =\n%q\nwant\n%q", got, want)
	}
}

// The stacks of finished threads must not be kept for reuse: they hold note
// text that the zeroing allocator never sees (search spec §14). Tunables the
// parent sets stay, the stack cache is turned off after them.
func TestWorkerEnvKeepsTunables(t *testing.T) {
	got := workerEnv([]string{"GLIBC_TUNABLES=glibc.malloc.check=3", "HOME=/home/u"})

	want := []string{
		"HOME=/home/u",
		"ORT_DISABLE_TELEMETRY=1",
		"GLIBC_TUNABLES=glibc.malloc.check=3:glibc.pthread.stack_cache_size=0",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("workerEnv =\n%q\nwant\n%q", got, want)
	}
}
