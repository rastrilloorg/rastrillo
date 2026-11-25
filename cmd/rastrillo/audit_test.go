package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/audit"
)

// TestAuditExemptionsOnTickets runs the real subcommand against the
// tickets example, whose boot set carries core's eventlog declaration.
// No -package: tickets has no Schema or Models, and exemptions needs
// only BootSchema, so detection must find it on its own.
func TestAuditExemptionsOnTickets(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the tickets example")
	}
	setSandboxGoEnv(t)
	// A copy, not the example: the loader writes into the module it
	// reads and -mod=mod may rewrite go.mod and go.sum, while other
	// shards build the tracked tree.
	root := repoRoot(t)
	tickets := filepath.Join(t.TempDir(), "tickets")
	examples := os.DirFS(filepath.Join(root, "examples", "tickets"))
	if err := os.CopyFS(tickets, examples); err != nil {
		t.Fatal(err)
	}
	edit := exec.Command("go", "mod", "edit", "-replace",
		"amadan.net/rastrillo/rastrillo="+root)
	edit.Dir = tickets
	if out, err := edit.CombinedOutput(); err != nil {
		t.Fatalf("pointing the copy at this checkout: %v\n%s", err, out)
	}
	var out bytes.Buffer
	err := runAudit([]string{"exemptions", tickets}, &out)
	if err != nil {
		t.Fatal(err)
	}
	want := "eventlog  eventlog  not audited  eventlog records actor, " +
		"kind and time for every append itself\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("output lacks the eventlog line:\n%s", out.String())
	}
}

// None declared is all an empty list shows: a shared table has nothing
// to erase, and an undeclared one has no exemption to list.
func TestPrintExemptionsSaysWhenThereAreNone(t *testing.T) {
	var out bytes.Buffer
	printExemptions(&out, nil)
	want := "No exemptions declared.\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestPrintExemptionsOneLineEach(t *testing.T) {
	var out bytes.Buffer
	printExemptions(&out, []audit.Exemption{
		{Table: "t", Namespace: "ns", What: "kept on erasure", Reason: "why"},
	})
	if got, want := out.String(), "ns  t  kept on erasure  why\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
