//go:build !unix

package localecheck

import (
	"os"
	"os/exec"
)

// Without process groups, only the probe's own process can be killed.
func ownGroup(*exec.Cmd) {}

func killGroup(p *os.Process) { p.Kill() }
