package main

import (
	"fmt"
	"io"

	"amadan.net/rastrillo/rastrillo/sessions"
)

const sessionKeyUsage = "usage: rastrillo session-key <kid>"

// runSessionKey mints one signing key and prints its two halves on
// separate lines, "secret ..." and "public ...", so a script can take
// either with awk. A bad kid is a usage error (exit 2), not exit 1,
// and prints nothing on stdout: a script that captures stdout must not
// end up with a half-written key.
func runSessionKey(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return exitError{code: 2, msg: sessionKeyUsage}
	}
	k, err := sessions.NewSigningKey(args[0])
	if err != nil {
		return exitError{code: 2, msg: fmt.Sprintf("%s (%v)", sessionKeyUsage, err)}
	}
	_, err = fmt.Fprintf(stdout, "secret %s\npublic %s\n", k.Secret(), k.Public())
	return err
}
