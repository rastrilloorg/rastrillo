package opsgen

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEffects is the flag table of the MCP spec, section 4, one row
// each. A replaced body is hand-written, so the generator cannot know
// what it does: every write becomes destructive and not idempotent. A
// read stays a read whatever replaces it.
func TestEffects(t *testing.T) {
	cases := []struct {
		action                  string
		replaced                bool
		destructive, idempotent bool
	}{
		{"list", false, false, true},
		{"get", false, false, true},
		{"create", false, false, false},
		{"update", false, true, false},
		{"delete", false, true, true},
		{"list", true, false, true},
		{"get", true, false, true},
		{"create", true, true, false},
		{"update", true, true, false},
		{"delete", true, true, false},
	}
	for _, row := range cases {
		destructive, idempotent := Effects(row.action, row.replaced)
		if destructive != row.destructive || idempotent != row.idempotent {
			t.Errorf("Effects(%q, %t) = %t, %t; want %t, %t", row.action,
				row.replaced, destructive, idempotent, row.destructive,
				row.idempotent)
		}
	}
}

// flagsIn reads the two flags out of one descriptor's literal in
// types.go, so the test checks the text the app is built from.
func flagsIn(t *testing.T, types, operation string) (bool, bool) {
	t.Helper()
	start := regexp.MustCompile(`Operation:\s+` + operation + `,`)
	at := start.FindStringIndex(types)
	if at == nil {
		t.Fatalf("types.go has no descriptor for %s:\n%s", operation, types)
	}
	block := types[at[0]:]
	block = block[:strings.Index(block, "Invoke:")]
	found := func(name string) bool {
		match := regexp.MustCompile(name + `:\s+(true|false),`).
			FindStringSubmatch(block)
		if match == nil {
			t.Fatalf("%s has no %s:\n%s", operation, name, block)
		}
		return match[1] == "true"
	}
	return found("Destructive"), found("Idempotent")
}

// emitTypes emits the bookmarks resource into a scratch module that
// has a hand-written file for each action in replaced, and returns
// its types.go.
func emitTypes(t *testing.T, replaced ...string) string {
	t.Helper()
	root := t.TempDir()
	gomod := "module scratch\n\ngo 1.25.0\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod),
		0o644); err != nil {
		t.Fatal(err)
	}
	for _, action := range replaced {
		path := filepath.Join(root, "ops", "bookmarks", action+".go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		hand := "//go:build rastrillo_actions\n\npackage anything\n"
		if err := os.WriteFile(path, []byte(hand), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	genDir := filepath.Join(root, "gen")
	if _, _, err := Emit(root, genDir, bookmarks(t)); err != nil {
		t.Fatal(err)
	}
	return readFile(t, filepath.Join(genDir, "ops", "bookmarks", "types.go"))
}

// TestDescriptorsCarryFlags: types.go says what each operation does,
// and a hand-written create.go or delete.go changes what the
// descriptor says about that one operation, though types.go itself
// is never replaced.
func TestDescriptorsCarryFlags(t *testing.T) {
	cases := []struct {
		name     string
		replaced []string
		want     map[string][2]bool // operation: destructive, idempotent
	}{
		{"generated", nil, map[string][2]bool{
			"ListOp": {false, true}, "GetOp": {false, true},
			"CreateOp": {false, false}, "UpdateOp": {true, false},
			"DeleteOp": {true, true},
		}},
		{"create and delete replaced", []string{"create", "delete"},
			map[string][2]bool{
				"ListOp": {false, true}, "GetOp": {false, true},
				"CreateOp": {true, false}, "UpdateOp": {true, false},
				"DeleteOp": {true, false},
			}},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			types := emitTypes(t, row.replaced...)
			for operation, want := range row.want {
				destructive, idempotent := flagsIn(t, types, operation)
				if destructive != want[0] || idempotent != want[1] {
					t.Errorf("%s: Destructive %t, Idempotent %t; want %t, %t",
						operation, destructive, idempotent, want[0], want[1])
				}
			}
		})
	}
}
