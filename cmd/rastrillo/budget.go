package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/internal/budgetrun"
	"amadan.net/rastrillo/rastrillo/internal/budgetsize"
)

func runBudget(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rastrillo budget <size|test> ...")
	}
	switch args[0] {
	case "size":
		return budgetSize(args[1:], os.Stdout)
	case "test":
		return budgetTest(args[1:], ".", os.Stdout, os.Stderr, os.Getenv)
	default:
		return fmt.Errorf("unknown budget subcommand %q (size, test)", args[0])
	}
}

// budgetSize exits 1 for a violation and 2 for a malformed budgets.txt,
// so a runner's log distinguishes "the code grew" from "the file is
// wrong" without anyone reading the output.
func budgetSize(args []string, out io.Writer) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("budget size: %s has no go.mod; run it at a module root", root)
	}
	f, err := budgetfile.Load(root)
	if err != nil {
		return exitError{code: 2, msg: err.Error()}
	}
	counts, err := budgetsize.Measure(root)
	if err != nil {
		return err
	}
	msgs := budgetsize.Check(counts, f)
	for _, m := range msgs {
		fmt.Fprintln(out, m)
	}
	if len(msgs) > 0 {
		return exitError{code: 1, msg: fmt.Sprintf("budget size: %d problem(s)", len(msgs))}
	}
	fmt.Fprintf(out, "budget size: %d directories within budget\n", len(counts))
	return nil
}

// goCommand is the go binary budget test runs. A variable only so a test
// can stand in a go that dies mid-run.
var goCommand = "go"

// budgetTest runs go test -json itself rather than reading a pipe: the
// shell reports only a pipe's last status, so `go test -json | x` would
// read a go test killed after one passing package as a clean, short
// stream. Here go test's own exit status is the floor of the verdict.
func budgetTest(args []string, dir string, stdout, stderr io.Writer, getenv func(string) string) error {
	var o budgetrun.Options
flags:
	for len(args) > 0 {
		switch args[0] {
		case "-no-time":
			o.NoTime = true
			args = args[1:]
		case "-require":
			if len(args) < 2 {
				return errors.New("budget test: -require needs a comma-separated list of test names")
			}
			o.Require = strings.Split(args[1], ",")
			args = args[2:]
		default:
			break flags
		}
	}
	pkgs := packageArgs(args)
	if len(pkgs) == 0 {
		args = append(args, "./...")
		pkgs = []string{"./..."}
	}
	o.Enforce = budgetfile.Enforce(getenv)

	root, err := budgetfile.ModuleRoot(dir)
	if err != nil {
		return err
	}
	if o.File, err = budgetfile.Load(root); err != nil {
		return exitError{code: 2, msg: err.Error()}
	}
	if o.ModulePath, err = budgetfile.ModulePath(root); err != nil {
		return err
	}
	// Only ./... from the module root lists every package, so only then
	// can a time record naming no package be called stale.
	if abs, err := filepath.Abs(dir); err == nil {
		o.WholeModule = abs == root && len(pkgs) == 1 && pkgs[0] == "./..."
	}

	cmd := exec.Command(goCommand, append([]string{"test", "-json"}, args...)...)
	cmd.Dir = dir
	cmd.Stderr = stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return err
	}
	rep, readErr := budgetrun.Read(pipe, stdout)
	if readErr != nil {
		// Stop the producer and empty the pipe, or Wait could block on a
		// go test still writing to a reader nobody consumes.
		cmd.Process.Kill()
		io.Copy(io.Discard, pipe)
	}
	waitErr := cmd.Wait()
	wall := time.Since(start)

	v := budgetrun.Judge(rep, o)
	budgetrun.Print(stdout, rep, v, wall)
	switch {
	case readErr != nil:
		return exitError{code: 1, msg: "budget test: " + readErr.Error()}
	case waitErr != nil:
		code := 1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) && ee.ExitCode() > 0 {
			code = ee.ExitCode()
		}
		return exitError{code: code, msg: "budget test: go test failed"}
	case len(v.Problems) > 0:
		return exitError{code: 1, msg: fmt.Sprintf("budget test: %d problem(s)", len(v.Problems))}
	}
	return nil
}

// boolTestFlags are the go test and build flags that take no value, so
// the token after them is a package, not their argument.
var boolTestFlags = map[string]bool{
	"-v": true, "-short": true, "-race": true, "-cover": true, "-failfast": true,
	"-json": true, "-benchmem": true, "-msan": true, "-asan": true, "-trimpath": true,
	"-x": true, "-n": true, "-a": true, "-fullpath": true, "-work": true, "-linkshared": true,
}

// packageArgs picks the package patterns out of go test arguments, so
// "-count 1 ./..." is recognised as the whole module and "-tags perf"
// does not make "perf" a package.
func packageArgs(args []string) []string {
	var pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case !strings.HasPrefix(a, "-"):
			pkgs = append(pkgs, a)
		case strings.Contains(a, "="), boolTestFlags[strings.Replace(a, "--", "-", 1)]:
		default:
			i++ // a value flag: the next token is its value
		}
	}
	return pkgs
}
