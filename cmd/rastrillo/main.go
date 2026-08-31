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
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = runNew(os.Args[2:])
	case "generate":
		err = runGenerate(os.Args[2:])
	case "dev":
		err = runDev(os.Args[2:])
	case "migration":
		err = runMigration(os.Args[2:])
	case "vectors":
		err = runVectors(os.Args[2:])
	case "doctor":
		err = runDoctor(os.Args[2:])
	case "markup":
		err = runMarkup(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "rastrillo: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
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

func usage() {
	fmt.Fprint(os.Stderr, `rastrillo — the CARLOS web framework CLI

Usage:
  rastrillo new <name>                          scaffold a new app in ./<name>
  rastrillo generate [flags] [dir]              run the filesystem-routing generator (flags before dir; default dir: .)
       --check --default-locale <code>          verify only: route collisions, action build tags, i18n catalogs
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
                                                exits 0 clean, 3 drift, 4 the app is on a different rastrillo version.
                                                A convenience and an upgrade tool: the vendored_test.go the scaffold
                                                writes is what catches drift on every commit without being run.
  rastrillo markup [--fix] [dir]                rewrite the rst- class spelling as the rst- attribute spelling (default dir: .)
       --fix                                    write it; without the flag it reports and exits 3 if there is work
                                                the codemod for design doc §6-v3. Idempotent. Skips static/'s vendored
                                                files (those are doctor's), and reports rather than guesses at a class
                                                list it cannot take apart.
  rastrillo vectors [flags] [dir]               Go↔JS parity vectors: run cmd/genvectors, write test/vectors.json (default dir: .)
       -init                                     scaffold cmd/genvectors, the test/ parity suite, and the go-test belt (once)
       -check                                    pre-ship gate: regenerate + byte-compare, then node --test test/parity.test.mjs
`)
}
