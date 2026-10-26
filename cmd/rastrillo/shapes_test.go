package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reserved shapes are named in the menu so nobody invents them, and
// refused before anything is written: a half-made directory would make
// the next, correct, attempt fail with "already exists".
func TestNewRefusesReservedShapes(t *testing.T) {
	want := map[string]string{
		"home": "--scaffold home is not built yet: it is Plan 6 of the user-models roadmap. Use bare, single or accounts for now",
		"hub":  "--scaffold hub is not built yet: it is Plan 7 of the user-models roadmap. Use bare, single or accounts for now",
	}
	for name, msg := range want {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := runNew([]string{"--scaffold", name, "x"})
			if err == nil || err.Error() != msg {
				t.Fatalf("error = %v, want %q", err, msg)
			}
			if _, statErr := os.Stat("x"); statErr == nil {
				t.Fatal("x exists after a refused shape")
			}
		})
	}
}

func TestNewRefusesUnknownShape(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runNew([]string{"--scaffold=tiny", "x"})
	want := `unknown --scaffold "tiny": choose bare, single or accounts`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if _, statErr := os.Stat("x"); statErr == nil {
		t.Fatal("x exists after an unknown shape")
	}
}

// No --scaffold means single. Both runs go through writeSingle, so this
// proves only that the default is single; the existing scaffold tests are
// what pin single's output as unchanged.
func TestDefaultShapeIsSingle(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"plumbus"}); err != nil {
		t.Fatal(err)
	}
	if err := runNew([]string{"--scaffold", "single", "frobnitz"}); err != nil {
		t.Fatal(err)
	}
	sameScaffold(t, "plumbus", "frobnitz")
}

// sameScaffold fails unless apps a and b hold the same files with the
// same contents once each one's name is substituted out.
func sameScaffold(t *testing.T, a, b string) {
	t.Helper()
	read := func(app string) map[string]string {
		got := map[string]string{}
		err := filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			rel, _ := filepath.Rel(app, p)
			got[strings.ReplaceAll(rel, app, "APP")] = strings.ReplaceAll(strings.ReplaceAll(string(b), strings.ToUpper(app), "APP"), app, "app")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first, second := read(a), read(b)
	if len(first) != len(second) {
		t.Errorf("%s has %d files, %s %d", a, len(first), b, len(second))
	}
	for path, content := range first {
		other, ok := second[path]
		switch {
		case !ok:
			t.Errorf("%s: in %s, not in %s", path, a, b)
		case other != content:
			t.Errorf("%s: differs between %s and %s", path, a, b)
		}
	}
}

func TestUsageListsTheScaffoldFlag(t *testing.T) {
	err := runNew(nil)
	if err == nil || !strings.Contains(err.Error(), "--scaffold=bare|single|accounts") {
		t.Fatalf("usage = %v, want it to list --scaffold=bare|single|accounts", err)
	}
}

// A bad choice is refused by runNew, not by each shape's Write: finding
// x after the refusal would mean a shape made its directory first.
func TestNewChecksChoicesBeforeAnyShapeWrites(t *testing.T) {
	for _, shape := range []string{"bare", "single", "accounts"} {
		t.Run(shape, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := runNew([]string{"--scaffold", shape, "--theme=nope", "x"})
			if err == nil || !strings.Contains(err.Error(), `unknown theme "nope"`) {
				t.Fatalf("error = %v, want the unknown theme refusal", err)
			}
			if _, statErr := os.Stat("x"); statErr == nil {
				t.Fatal("x exists after a refused choice")
			}
		})
	}
}
