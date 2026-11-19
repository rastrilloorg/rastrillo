//go:build !unix

package tmplgen

import (
	"os"
	"os/exec"
)

// Without process groups, only the capture run's own process can be
// killed.
func ownGroup(*exec.Cmd) {}

func killGroup(process *os.Process) { process.Kill() }
