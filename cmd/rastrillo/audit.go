// rastrillo audit — what the audit log leaves out, in one place.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"amadan.net/rastrillo/rastrillo/audit"
)

const msgNoExemptions = "No exemptions declared."

const msgExemption = "%s  %s  %s  %s"

// exemptionsCall hands the app's composed boot set to the loader. It
// needs nothing else, so an app on plain SQL, with no Schema or
// Models, is found without -package.
var exemptionsCall = loaderCall{
	body:  "dump.Exemptions(app.BootSchema)",
	needs: []string{"BootSchema"},
}

func runAudit(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "exemptions" {
		return errors.New("usage: rastrillo audit exemptions " +
			"[-package name] [dir]")
	}
	flags := flag.NewFlagSet("exemptions", flag.ContinueOnError)
	positional, override, err := parseMigrationArgs(flags, args[1:])
	if err != nil {
		return err
	}
	var list []audit.Exemption
	err = runLoader(dirArg(positional), override, exemptionsCall, &list)
	if err != nil {
		return err
	}
	printExemptions(out, list)
	return nil
}

// printExemptions writes one line per exemption, in the order given.
func printExemptions(out io.Writer, list []audit.Exemption) {
	if len(list) == 0 {
		fmt.Fprintln(out, msgNoExemptions)
		return
	}
	for _, ex := range list {
		fmt.Fprintf(out, msgExemption+"\n",
			ex.Namespace, ex.Table, ex.What, ex.Reason)
	}
}
