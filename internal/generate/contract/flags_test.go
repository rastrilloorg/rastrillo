package contract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/opsgen"
	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
)

// flags is the destructive and idempotent flags the contract records
// for each of bookmarks' operations.
func flags(t *testing.T, raw []byte) map[string][2]bool {
	t.Helper()
	document, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][2]bool{}
	for _, operation := range document.Operations {
		found[operation.ID] = [2]bool{
			operation.Destructive, operation.Idempotent,
		}
	}
	return found
}

// withFlag is raw with the flag on one operation set by hand, as an
// older generator or a hand edit would have left it. The flag lines
// follow the operation's id line, so the first match after it is its.
func withFlag(t *testing.T, raw []byte, id, flag string, to bool) []byte {
	t.Helper()
	text := string(raw)
	at := strings.Index(text, `"id": "`+id+`"`)
	if at < 0 {
		t.Fatalf("no operation %s in:\n%s", id, text)
	}
	was := fmt.Sprintf(`"%s": %t,`, flag, !to)
	now := fmt.Sprintf(`"%s": %t,`, flag, to)
	if !strings.Contains(text[at:], was) {
		t.Fatalf("%s of %s was not %t:\n%s", flag, id, !to, text)
	}
	return []byte(text[:at] + strings.Replace(text[at:], was, now, 1))
}

// commit replaces the app's api/v1.json with raw.
func commit(t *testing.T, app string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(app, Path), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantCautious fails unless the flags are a hand-written body's:
// destructive and not idempotent.
func wantCautious(t *testing.T, found map[string][2]bool, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if found[id] != [2]bool{true, false} {
			t.Errorf("%s: flags %v; want destructive, not idempotent",
				id, found[id])
		}
	}
}

// TestContractFlags: api/v1.json records what each operation does, a
// flag changing in either direction breaks no client, and a flag turning
// destructive is told to the operator rather than refused.
func TestContractFlags(t *testing.T) {
	now := contract(t, bookmarks(t))
	want := map[string][2]bool{
		"bookmarks.list": {false, true}, "bookmarks.get": {false, true},
		"bookmarks.create": {false, false},
		"bookmarks.update": {true, false}, "bookmarks.delete": {true, true},
	}
	got := flags(t, now)
	for id, wanted := range want {
		if got[id] != wanted {
			t.Errorf("%s: destructive, idempotent = %v; want %v",
				id, got[id], wanted)
		}
	}
	oneLineEach := "\"destructive\": true,\n      \"idempotent\": false,"
	if !strings.Contains(string(now), oneLineEach) {
		t.Errorf("update's flags are not each on a line of their own:\n%s", now)
	}

	for _, change := range []struct {
		name, id, flag string
		to             bool
	}{
		{"update stops being destructive",
			"bookmarks.update", "destructive", false},
		{"create becomes destructive",
			"bookmarks.create", "destructive", true},
		{"delete stops being idempotent",
			"bookmarks.delete", "idempotent", false},
		{"create becomes idempotent",
			"bookmarks.create", "idempotent", true},
	} {
		other := withFlag(t, now, change.id, change.flag, change.to)
		for _, pair := range [][2][]byte{{now, other}, {other, now}} {
			breaks, err := BreakingChanges(pair[0], pair[1])
			if err != nil || len(breaks) != 0 {
				t.Errorf("%s: BreakingChanges = %q, %v; want none",
					change.name, breaks, err)
			}
		}
	}
}

// TestWriteNotesADestructiveFlagTurningOn: the operator is told, in one
// line naming the operation, when a flag a client may already be using
// turns destructive, and told nothing when one turns off.
func TestWriteNotesADestructiveFlagTurningOn(t *testing.T) {
	app := t.TempDir()
	resources := []rastrillo.Resource{bookmarks(t)}
	if _, err := Write(app, resources, false); err != nil {
		t.Fatal(err)
	}
	committed := read(t, app)
	older := withFlag(t, committed, "bookmarks.update", "destructive", false)
	commit(t, app, older)
	notes, err := Write(app, resources, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "bookmarks.update: now marked destructive") {
		t.Errorf("notes = %q; want one naming bookmarks.update", notes)
	}
	if strings.Contains(joined, "bookmarks.create") ||
		strings.Count(joined, "destructive") != 1 {
		t.Errorf("notes = %q; want only the one operation named", notes)
	}

	quieter := withFlag(t, committed, "bookmarks.create", "destructive", true)
	commit(t, app, quieter)
	notes, err = Write(app, resources, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{Path + ": updated"}
	if strings.Join(notes, "|") != strings.Join(want, "|") {
		t.Errorf("notes for a flag turning off = %q; want %q", notes, want)
	}
}

// TestContractCheckWithReplacedBodies: a hand file under ops/ makes
// that write the cautious flags in api/v1.json, and Write and Check
// both read it from the app root, so --check agrees with generate.
func TestContractCheckWithReplacedBodies(t *testing.T) {
	app := t.TempDir()
	for _, action := range []string{"create", "delete"} {
		path := filepath.Join(app, "ops", "bookmarks", action+".go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		hand := []byte("package anything\n")
		if err := os.WriteFile(path, hand, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resources := []rastrillo.Resource{bookmarks(t)}
	if _, err := Write(app, resources, false); err != nil {
		t.Fatal(err)
	}
	if err := Check(app, resources); err != nil {
		t.Errorf("Check after Write with replaced bodies: %v", err)
	}
	got := flags(t, read(t, app))
	wantCautious(t, got, "bookmarks.create", "bookmarks.delete")
	if got["bookmarks.update"] != [2]bool{true, false} ||
		got["bookmarks.get"] != [2]bool{false, true} {
		t.Errorf("an operation nobody replaced changed its flags: %v", got)
	}

	if err := os.RemoveAll(filepath.Join(app, "ops")); err != nil {
		t.Fatal(err)
	}
	if err := Check(app, resources); err == nil {
		t.Error("Check passed after the replaced bodies were removed; " +
			"it must read them from the app root")
	}
}

// TestGenerateCheckWithReplacedBodies drives the real command over an
// app that replaces create and delete: generate, then generate --check,
// must agree, and the file shows the cautious flags for both.
func TestGenerateCheckWithReplacedBodies(t *testing.T) {
	app := scratch.App(t, map[string]string{
		"manifest/bookmarks.toml": bookmarksTOML,
	})
	for _, action := range []string{"create", "delete"} {
		scratch.Write(t, app, "ops/bookmarks/"+action+".go",
			"//go:build rastrillo_actions\n\npackage anything\n")
	}
	out, err := scratch.Run(app, scratch.CLI(t), "generate", app)
	if err != nil {
		t.Fatalf("generate with replaced bodies: %v\n%s", err, out)
	}
	if err := scratch.Check(t, app); err != nil {
		t.Errorf("--check after generate with replaced bodies: %v", err)
	}
	wantCautious(t, flags(t, read(t, app)),
		"bookmarks.create", "bookmarks.delete")
}

// descriptorFlags reads every operation's flags out of an emitted
// types.go, keyed by the operation's id.
func descriptorFlags(t *testing.T, types string) map[string][2]bool {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)Operation:\s+(\w+)Op,.*?` +
		`Destructive:\s+(true|false),\s+Idempotent:\s+(true|false),`)
	found := map[string][2]bool{}
	for _, match := range pattern.FindAllStringSubmatch(types, -1) {
		id := "bookmarks." + strings.ToLower(match[1])
		found[id] = [2]bool{match[2] == "true", match[3] == "true"}
	}
	return found
}

// writeFile writes content at path under app, making the directories.
func writeFile(t *testing.T, app, path, content string) {
	t.Helper()
	full := filepath.Join(app, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEmittedFlagsMatchTheContract: the descriptors in types.go and
// api/v1.json each decide which bodies are replaced, from the same
// ops/ tree, and must reach the same flags for every operation. A
// dangling symlink named create.go is the case that tells two ways of
// looking apart: a directory listing sees it and a stat does not.
func TestEmittedFlagsMatchTheContract(t *testing.T) {
	app := t.TempDir()
	writeFile(t, app, "go.mod", "module scratch\n\ngo 1.25.0\n")
	writeFile(t, app, "ops/bookmarks/delete.go",
		"//go:build rastrillo_actions\n\npackage anything\n")
	dangling := filepath.Join(app, "ops", "bookmarks", "create.go")
	if err := os.Symlink("nowhere.go", dangling); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	resources := []rastrillo.Resource{bookmarks(t)}
	genDir := filepath.Join(app, "gen")
	if _, _, err := opsgen.Emit(app, genDir, resources[0]); err != nil {
		t.Fatal(err)
	}
	types := filepath.Join(genDir, "ops", "bookmarks", "types.go")
	source, err := os.ReadFile(types)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(app, resources, false); err != nil {
		t.Fatal(err)
	}
	emitted := descriptorFlags(t, string(source))
	written := flags(t, read(t, app))
	if len(emitted) != 5 {
		t.Fatalf("read %d descriptors from types.go; want 5: %v",
			len(emitted), emitted)
	}
	for id, want := range written {
		if emitted[id] != want {
			t.Errorf("%s: types.go says %v, api/v1.json says %v",
				id, emitted[id], want)
		}
	}
}

// TestFlaglessContractNotesNothing: an api/v1.json from before the
// flags were recorded has no destructive key, so no client could have
// relied on one. Regenerating over it says nothing.
func TestFlaglessContractNotesNothing(t *testing.T) {
	app := t.TempDir()
	resources := []rastrillo.Resource{bookmarks(t)}
	if _, err := Write(app, resources, false); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(
		`\n      "(destructive|idempotent)": (true|false),`)
	flagless := pattern.ReplaceAll(read(t, app), nil)
	if bytes.Contains(flagless, []byte("destructive")) {
		t.Fatalf("the old file still has flags:\n%s", flagless)
	}
	commit(t, app, flagless)
	notes, err := Write(app, resources, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{Path + ": updated"}
	if strings.Join(notes, "|") != strings.Join(want, "|") {
		t.Errorf("notes over a flag-less file = %q; want %q", notes, want)
	}
}
