package searchworker

import "syscall"

// sysProcAttr kills the worker when the thread that started it ends (spec
// §8.1); the engine starts it from a locked thread that lives as long as the
// process.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
