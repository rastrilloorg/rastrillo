// Command rastrillo is the CARLOS web framework's CLI: rastrillo new
// scaffolds an app, rastrillo generate runs the filesystem-routing
// generator, rastrillo dev runs the watch/rebuild/restart loop.
// Subcommand dispatch only — everything real lives in this package's
// other files, one concern per file, per the family's own convention.
package main

import (
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
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "rastrillo: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
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
  rastrillo vectors [flags] [dir]               Go↔JS parity vectors: run cmd/genvectors, write test/vectors.json (default dir: .)
       -init                                     scaffold cmd/genvectors, the test/ parity suite, and the go-test belt (once)
       -check                                    pre-ship gate: regenerate + byte-compare, then node --test test/parity.test.mjs
`)
}
