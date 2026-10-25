package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/sessions"
)

func TestSessionKeyPrintsAPairThatParses(t *testing.T) {
	var out bytes.Buffer
	if err := runSessionKey([]string{"k2"}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "secret k2.") || !strings.HasPrefix(lines[1], "public k2.") {
		t.Fatalf("output: %q", out.String())
	}
	k, err := sessions.ParseSigningKey(strings.TrimPrefix(lines[0], "secret "))
	if err != nil || k.Public() != strings.TrimPrefix(lines[1], "public ") {
		t.Fatalf("the two lines are not one key pair: %v", err)
	}
	// A tenant reads the public line with ParsePublicKeys; a line that
	// only the signing side can parse would ship a key no one can verify.
	if _, err := sessions.ParsePublicKeys(strings.TrimPrefix(lines[1], "public ")); err != nil {
		t.Fatalf("the public line is not a tenant's key list: %v", err)
	}
}

func TestSessionKeyRefusesABadKid(t *testing.T) {
	for _, args := range [][]string{nil, {"K 2"}, {"a", "b"}} {
		var out bytes.Buffer
		err := runSessionKey(args, &out)
		var ex exitError
		if !errors.As(err, &ex) || ex.code != 2 || ex.msg == "" || out.Len() != 0 {
			t.Errorf("%q: %v, stdout %q; want exit 2 with usage and nothing on stdout", args, err, out.String())
		}
	}
}
