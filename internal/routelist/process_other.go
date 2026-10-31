//go:build !unix

package routelist

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// promisesCleanup is false here: without Unix process groups only the
// child itself is killed, so the compiler under go build, or a process
// the app started, could outlive the command and hold its scratch
// directory open. goSettings refuses before Go runs; child stays so
// the package builds.
const promisesCleanup = false

// child is a command killed when the context is cancelled. Without
// Unix process groups, only the child itself is killed.
type child struct{ *exec.Cmd }

func inOwnGroup(ctx context.Context, name string, args ...string) child {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 2 * time.Second
	return child{command}
}

func (running child) run() error { return running.Run() }

// EndBy exits with the status a Unix shell gives a command killed by
// the signal, 128 and its number. There is no signal to die of here.
func EndBy(received os.Signal) {
	number, ok := received.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	os.Exit(128 + int(number))
}
