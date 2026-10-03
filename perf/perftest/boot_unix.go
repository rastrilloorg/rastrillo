//go:build unix

package perftest

import (
	"os/exec"
	"syscall"
)

// isolate puts the child in a process group of its own, so the timeout
// kills it and everything it started: a grandchild holding stdout would
// otherwise survive the kill and keep the pipe open.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
