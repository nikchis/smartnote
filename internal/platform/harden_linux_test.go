package platform

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHardenHelper is the child of TestHardenChild: it hardens itself when
// asked to and waits until the parent kills it.
func TestHardenHelper(t *testing.T) {
	if os.Getenv("HARDEN_HELPER") != "1" {
		t.Skip("helper of TestHardenChild")
	}

	if os.Getenv("HARDEN") == "1" {
		if err := Harden(); err != nil {
			t.Fatalf("Harden: %v", err)
		}
	}

	if err := os.WriteFile(os.Getenv("HARDEN_READY"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Minute)
}

// startHelper runs the helper and returns its pid once it is ready.
func startHelper(t *testing.T, harden bool) int {
	t.Helper()

	ready := t.TempDir() + "/ready"
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHardenHelper$")

	cmd.Env = append(os.Environ(), "HARDEN_HELPER=1", "HARDEN_READY="+ready)

	if harden {
		cmd.Env = append(cmd.Env, "HARDEN=1")
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)

	for {
		if _, err := os.Stat(ready); err == nil {
			return cmd.Process.Pid
		}

		if time.Now().After(deadline) {
			t.Fatal("helper did not start")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func procPath(pid int, name string) string {
	return "/proc/" + strconv.Itoa(pid) + "/" + name
}

func TestHardenChild(t *testing.T) {
	t.Run("control", func(t *testing.T) {
		pid := startHelper(t, false)

		f, err := os.Open(procPath(pid, "mem"))
		if err != nil {
			t.Skipf("control: cannot open child memory (ptrace policy): %v", err)
		}

		_ = f.Close()
	})

	t.Run("hardened", func(t *testing.T) {
		pid := startHelper(t, true)

		f, err := os.Open(procPath(pid, "mem"))
		if err == nil {
			_ = f.Close()

			t.Fatal("opened /proc/<pid>/mem of a hardened process")
		}

		if !errors.Is(err, syscall.EACCES) {
			t.Errorf("open mem: %v, want EACCES", err)
		}

		limits, err := os.ReadFile(procPath(pid, "limits"))
		if err != nil {
			t.Fatal(err)
		}

		for _, line := range strings.Split(string(limits), "\n") {
			if strings.HasPrefix(line, "Max core file size") {
				if f := strings.Fields(line); f[4] != "0" || f[5] != "0" {
					t.Errorf("core limit: %q, want 0 0", line)
				}

				return
			}
		}

		t.Error("no core limit line")
	})
}
