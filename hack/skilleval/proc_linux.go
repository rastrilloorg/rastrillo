package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// started holds the pids exec.Cmd is waiting on. They are the
// harness's children too, and a blind wait for orphans would steal
// their exit status from a concurrent run.
var started sync.Map

// runMark is set in the environment of everything a run starts. A
// process that daemonises and leaves the run directory still inherits
// it, so reap can find it by run as well as by working directory.
const runMark = "SKILLEVAL_RUN="

// prSetChildSubreaper is PR_SET_CHILD_SUBREAPER from linux/prctl.h.
const prSetChildSubreaper = 36

// becomeSubreaper makes processes that daemonise out of a run (setsid,
// a double fork) reparent to the harness instead of to init, so they
// stay findable as its descendants and die with the run.
func becomeSubreaper() {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		log.Printf("cannot become a subreaper (%v): a process that daemonises may outlive its run", errno)
	}
}

// runGroup runs the command in its own process group and kills the
// whole group when it ends, timed out or not.
func runGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	started.Store(cmd.Process.Pid, true)
	err := cmd.Wait()
	started.Delete(cmd.Process.Pid)
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return err
}

type proc struct {
	cwd, run string
	ppid     int
}

// descendants lists the harness's descendant pids with each one's
// working directory. Only descendants are ever killed: a shell the
// operator opened inside a run directory to look at it is not one.
func descendants() map[int]proc {
	self := os.Getpid()
	parent := map[int]int{}
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// The command name is parenthesised and may hold spaces, so the
		// fields worth reading start after its last paren.
		s := string(stat)
		fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
		if len(fields) < 2 {
			continue
		}
		if ppid, err := strconv.Atoi(fields[1]); err == nil {
			parent[pid] = ppid
		}
	}
	out := map[int]proc{}
	for pid := range parent {
		for p := parent[pid]; p > 1; p = parent[p] {
			if p == self {
				cwd, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
				out[pid] = proc{cwd: cwd, run: runOf(pid), ppid: parent[pid]}
				break
			}
		}
	}
	return out
}

// runOf reads the run a process belongs to from its environment.
func runOf(pid int) string {
	env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return ""
	}
	for _, kv := range strings.Split(string(env), "\x00") {
		if v, ok := strings.CutPrefix(kv, runMark); ok {
			return v
		}
	}
	return ""
}

// reap kills the harness's descendants belonging to the run in dir, by
// its mark or by working there: what one run left behind, without
// touching the runs beside it.
func reap(dir string) {
	var killed []int
	for pid, p := range descendants() {
		if _, ok := under(p.cwd, dir); ok || p.run == dir {
			syscall.Kill(pid, syscall.SIGKILL)
			if p.ppid == os.Getpid() {
				killed = append(killed, pid)
			}
		}
	}
	waitOrphans(killed)
}

// killDescendants is the last word: on exit, interrupt or failure,
// nothing the harness started survives it.
func killDescendants() {
	var killed []int
	for pid, p := range descendants() {
		syscall.Kill(pid, syscall.SIGKILL)
		if p.ppid == os.Getpid() {
			killed = append(killed, pid)
		}
	}
	waitOrphans(killed)
}

// waitOrphans collects killed orphans, which as a subreaper are ours to
// wait for, and leaves alone the children exec.Cmd is waiting on.
func waitOrphans(pids []int) {
	for _, pid := range pids {
		if _, ours := started.Load(pid); ours {
			continue
		}
		var ws syscall.WaitStatus
		syscall.Wait4(pid, &ws, 0, nil)
	}
}
