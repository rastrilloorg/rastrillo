package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/internal/cspcheck"
)

const cspUsage = "usage: rastrillo csp [dir]"

// runCSP fails markup the default Content-Security-Policy drops. The
// browser drops it without an error the page sees, so only a browser
// test that visits that page would notice, and only on its lane.
func runCSP(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("csp", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // the usage line says it once
	if err := flags.Parse(args); err != nil || flags.NArg() > 1 {
		return exitError{code: 2, msg: cspUsage}
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("csp: %s has no go.mod; run it at a module "+
			"root", root)
	}
	findings, err := cspcheck.Find(root)
	if err != nil {
		return fmt.Errorf("csp: %w", err)
	}
	for _, finding := range findings {
		fmt.Fprintln(out, finding)
	}
	if len(findings) > 0 {
		return exitError{code: 1, msg: fmt.Sprintf("csp: %d problem(s): "+
			"the browser drops these without an error", len(findings))}
	}
	fmt.Fprintln(out, "csp: nothing the CSP drops")
	return nil
}
