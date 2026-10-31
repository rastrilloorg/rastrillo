//go:build unix

package tmplgen

import (
	"os"
	"os/exec"
	"syscall"
)

// ownGroup puts the capture run in a process group of its own, so
// killGroup reaches every process it started and nothing else.
func ownGroup(
	cmd *exec.Cmd,
) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(
	process *os.Process,
) {
	syscall.Kill(-process.Pid, syscall.SIGKILL)
}
