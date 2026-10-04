//go:build !unix

package perftest

import "os/exec"

// Without process groups only the child itself is killed; WaitDelay
// still bounds the wait. The make gate does not run on these platforms.
func isolate(*exec.Cmd)   {}
func killGroup(*exec.Cmd) {}
