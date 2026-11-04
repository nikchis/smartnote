package searchworker

import "syscall"

// createNoWindow keeps a console program started by a GUI program without a
// console window.
const createNoWindow = 0x08000000

// sysProcAttr starts the worker, a console program, without a console window.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
