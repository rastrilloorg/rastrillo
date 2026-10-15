//go:build unix

package localecheck

import (
	"os"
	"os/exec"
	"syscall"
)

// ownGroup puts the probe in a process group of its own, so killGroup
// reaches every process it started and nothing else.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func killGroup(p *os.Process) { syscall.Kill(-p.Pid, syscall.SIGKILL) }
