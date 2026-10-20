//go:build !linux

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// Off Linux there is no subreaper and no /proc to find descendants in:
// process groups are the only cleanup, and a process that leaves its
// group can outlive its run.
const runMark = "SKILLEVAL_RUN="

func becomeSubreaper() {}
func reap(string)      {}
func killDescendants() {}

func runGroup(cmd *exec.Cmd) error {
	if err := startGroup(cmd); err != nil {
		return err
	}
	return waitGroup(cmd)
}

func startGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	return cmd.Start()
}

func waitGroup(cmd *exec.Cmd) error {
	err := cmd.Wait()
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return err
}
