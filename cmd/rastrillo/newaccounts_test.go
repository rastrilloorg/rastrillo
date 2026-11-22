package main

import (
	"strings"
	"testing"
)

// accounts is single with --auth=magic under the menu's name, not a
// second sign-in scaffold: anything one writes and the other does not
// is a fork someone has to keep in step by hand.
func TestAccountsIsSingleWithMagicAuth(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runNew([]string{"--scaffold", "accounts", "plumbus"})
	if err != nil {
		t.Fatal(err)
	}
	err = runNew([]string{"--scaffold", "single", "--auth=magic",
		"frobnitz"})
	if err != nil {
		t.Fatal(err)
	}
	sameScaffold(t, "plumbus", "frobnitz")
}

// The first go test signs someone in by the emailed link, and the
// browser and perf files, which only build under their tags, vet clean.
func TestAccountsPassesItsOwnTests(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "club", "--scaffold", "accounts")
	out, err := goTestIn(dir, "-v", "./...")
	if err != nil {
		t.Fatalf("go test ./...:\n%s", out)
	}
	// Sign-in adds an owner column: the migration check sees it.
	for _, test := range []string{"TestSignInByEmailedLink",
		"TestModelsMatchMigrations"} {
		if !strings.Contains(out, "--- PASS: "+test) {
			t.Errorf("%s did not run and pass:\n%s", test, out)
		}
	}
	vet, err := goIn(dir, "vet", "-tags", "browser,perf", "./...")
	if err != nil {
		t.Fatalf("go vet -tags browser,perf ./...:\n%s", vet)
	}
}

// Typed out, --auth=none asks accounts for the app it is not; left out,
// --auth means magic there (TestAccountsIsSingleWithMagicAuth).
func TestAccountsRefusesAuthNone(t *testing.T) {
	refusedBeforeWriting(t, "--auth=none does nothing with --scaffold "+
		"accounts, which is the app with sign-in: use --scaffold single "+
		"for an app without it",
		"--scaffold", "accounts", "--auth=none")
}

// Sign-in keeps its sessions and links in the database bare does not have.
func TestBareRefusesAuth(t *testing.T) {
	refusedBeforeWriting(t, "--auth=magic needs a database: use "+
		"--scaffold single or accounts",
		"--scaffold", "bare", "--auth=magic")
}

// signin is the name the help teaches and magic the name older scripts
// and docs use, so the two must keep writing one app.
func TestAuthSigninWritesWhatAuthMagicWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, run := range [][]string{
		{"--auth=signin", "plumbus"}, {"--auth=magic", "frobnitz"},
	} {
		if err := runNew(run); err != nil {
			t.Fatalf("rastrillo new %v: %v", run, err)
		}
	}
	sameScaffold(t, "plumbus", "frobnitz")
}

// A mistyped kind names signin, the one to use, first after none.
func TestUnknownAuthNamesSignin(t *testing.T) {
	refusedBeforeWriting(t, `unknown --auth "sigin": known kinds are `+
		"none, signin, magic", "--auth=sigin")
}
