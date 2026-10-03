package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
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

func budgetTest(args []string, dir string, stdout, stderr io.Writer, getenv func(string) string) error {
	return errors.New("budget test: not yet implemented")
}
