//go:build !linux && !windows

package searchworker

import "syscall"

// sysProcAttr adds nothing: without PR_SET_PDEATHSIG the worker ends when it
// reads EOF on stdin, which it does when Smartnote exits.
func sysProcAttr() *syscall.SysProcAttr { return nil }
