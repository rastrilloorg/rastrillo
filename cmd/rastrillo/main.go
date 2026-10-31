// Command rastrillo is the CARLOS web framework's CLI: rastrillo new
// scaffolds an app, rastrillo generate runs the filesystem-routing
// generator, rastrillo dev runs the watch/rebuild/restart loop.
// Subcommand dispatch only — everything real lives in this package's
// other files, one concern per file, per the family's own convention.
package main

import (
	"errors"
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/localecheck"
	"amadan.net/rastrillo/rastrillo/internal/tmplmigrate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	err := dispatch(os.Args[1], os.Args[2:])
	if err != nil {
		// A subcommand with findings rather than a failure — doctor —
		// returns its own exit code, and has already printed its
		// report. An empty message means exactly that: do not print a
		// second summary on stderr, just carry the status out.
		var ex exitError
		if errors.As(err, &ex) {
			if ex.msg != "" {
				fmt.Fprintf(os.Stderr, "rastrillo: %v\n", ex.msg)
			}
			os.Exit(ex.code)
		}
		fmt.Fprintf(os.Stderr, "rastrillo: %v\n", err)
		os.Exit(1)
	}
}

// dispatch runs the command named. An unknown one prints the usage and
// returns status 2 with no message, so main adds no second line.
func dispatch(command string, args []string) error {
	switch command {
	case "new":
		return runNew(args)
	case "generate":
		return runGenerate(args)
	case "dev":
		return runDev(args)
	case "migration":
		return runMigration(args)
	case "vectors":
		return runVectors(args)
	case "doctor":
		return runDoctor(args)
	case "migrate-templates":
		if code := tmplmigrate.Run(args, os.Stdout, os.Stderr); code != 0 {
			return exitError{code: code}
		}
		return nil
	case "markup":
		return runMarkup(args)
	case "budget":
		return runBudget(args)
	case "routes":
		return runRoutes(args, os.Stdout)
	case "session-key":
		return runSessionKey(args, os.Stdout)
	case "-h", "--help", "help":
		usage()
		return nil
	}
	fmt.Fprintf(os.Stderr, "rastrillo: unknown command %q\n\n", command)
	usage()
	return exitError{code: 2}
}

func usage() {
	fmt.Fprint(os.Stderr, usageText)
}

// usageText is a constant rather than usage's body so that the help can
// grow a line per command without making usage a function too long to
// read: it is a table, not code.
const usageText = `rastrillo — the CARLOS web framework CLI

Usage:
  rastrillo new [--locale=<code>] <name>        scaffold a new app in ./<name>, offering one language (default en)
  rastrillo generate [--check] [DIR]    write gen/ from manifest/; --check verifies without writing
  rastrillo dev [dir] [-- app args]             watch + regenerate + rebuild + restart (default dir: .)
  rastrillo migration <cmd> [dir]                schema changes (generate, new, status, check, baseline)
       generate [--allow-destructive]            diff models against migrations, write the delta
       check                                     CI gate: models and migrations agree (no database)
       new <name>                                write a numbered stub migration (for a hand-written change, e.g. a rename)
       status --db <path>                        what a real database's ledger has applied, plus pending drift
       baseline --db <path> [--through <id>]      stamp a ledger by hand after boot refuses to adopt (manual by design)
  rastrillo doctor [flags] [dir]                compare the app's vendored static/ files with this binary's (default dir: .)
       --fix [--force]                          re-copy what drifted (--force overrides its two refusals)
       --theme <name>                           which theme static/theme.css should be, for an app with no pin
       --probe                                  ` + localecheck.ProbeFlag + `
                                                exits 0 clean, 3 drift, 4 the app is on a different rastrillo version,
                                                5 it imports rastrillo's old module path.
                                                6  templates still use the old syntax: run rastrillo migrate-templates
                                                7  the app's locale configuration would stop it starting
                                                A convenience and an upgrade tool: the vendored_test.go the scaffold
                                                writes is what catches drift on every commit without being run.
  rastrillo migrate-templates [-write] PATH...    move templates to the localizer-first syntax; exits 1 while anything is left to do
  rastrillo markup [--fix] [dir]                rewrite the rst- class spelling as the rst- attribute spelling (default dir: .)
       --fix                                    write it; without the flag it reports and exits 3 if there is work
                                                the codemod for design doc §6-v3. Idempotent. Skips static/'s vendored
                                                files (those are doctor's), and reports rather than guesses at a class
                                                list it cannot take apart.
  rastrillo vectors [flags] [dir]               Go↔JS parity vectors: run cmd/genvectors, write test/vectors.json (default dir: .)
       -init                                     scaffold cmd/genvectors, the test/ parity suite, and the go-test belt (once)
       -check                                    pre-ship gate: regenerate + byte-compare, then node --test test/parity.test.mjs
  rastrillo budget size [dir]                   hold each directory to 5,000 source and 8,000 test lines (default dir: .)
                                                exits 1 over budget, 2 for a malformed .rastrillo/budgets.txt
  rastrillo budget code [--init | --lower] [dir]
                                                hold Go code to 80 columns,
                                                400-line files, 40-line
                                                functions, no single-letter
                                                names; exits 1 over a limit, 2
                                                for a malformed
                                                .rastrillo/code.txt or when
                                                --init finds one already there
  rastrillo budget test [-no-time] [-require T,...] [go test args]
                                                run go test -json, judge each package's time, print a receipt
  rastrillo routes [--package path] [--check] [dir]
                                                print each route and its
                                                guards (default dir: .);
                                                --check exits 1 for an
                                                action no route mounts
  rastrillo session-key <kid>                   print a new signing key for signed sessions, and its public key
`
