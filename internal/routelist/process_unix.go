//go:build unix

package routelist

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// promisesCleanup is true where a process group lets a cancel reach
// everything a child started.
const promisesCleanup = true

// child is a command run in a process group of its own. A Ctrl-C in
// the terminal goes to the foreground group, which the child is not
// in, so cancelling must kill it, and killing the group also reaches
// what it started: the compiler under go build, or a process the app
// started while it registered.
type child struct{ *exec.Cmd }

func inOwnGroup(ctx context.Context, name string, args ...string) child {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		killGroup(command)
		return nil
	}
	// A process the child started can hold its output open after the
	// child has exited; Wait gives up on it after this long.
	command.WaitDelay = 2 * time.Second
	return child{command}
}

// run waits for the child, then kills its group whatever the outcome,
// so nothing it started outlives the listing. ErrWaitDelay means the
// child succeeded and something it started still held its output; the
// kill ends that, so it is not a failure.
func (running child) run() error {
	if err := running.Start(); err != nil {
		return err
	}
	err := running.Wait()
	killGroup(running.Cmd)
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	return err
}

// killGroup ignores the error: ESRCH means the group is already gone,
// which is the outcome wanted.
func killGroup(command *exec.Cmd) {
	syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

// EndBy ends this process by the signal it received, once it has
// cleaned up, so the shell that ran it sees what it would have seen had
// the signal killed it. bash stops a loop on Ctrl-C only for a command
// that died of SIGINT; one that exits 130 lets the loop run on.
func EndBy(received os.Signal) {
	number := received.(syscall.Signal)
	signal.Reset(received)
	syscall.Kill(os.Getpid(), number)
	// The signal is delivered on the way back from the kernel; this is
	// reached only if something blocks it, and then the status says it.
	time.Sleep(time.Second)
	os.Exit(128 + int(number))
}
