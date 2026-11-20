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
	plans := map[string]string{
		"home": "Plan 4 of the multi-instance apps roadmap",
		"hub":  "Plan 6 of the multi-instance apps roadmap",
	}
	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := runNew([]string{"--scaffold", name, "x"})
			msg := "--scaffold " + name + " is not built yet: it is " +
				plan + ". Use bare, single or accounts for now"
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

// sameScaffold fails unless apps one and other hold the same files with
// the same contents once each one's name is substituted out.
func sameScaffold(t *testing.T, one, other string) {
	t.Helper()
	first, second := appFiles(t, one), appFiles(t, other)
	if len(first) != len(second) {
		t.Errorf("%s has %d files, %s %d", one, len(first), other,
			len(second))
	}
	for path, content := range first {
		otherContent, ok := second[path]
		switch {
		case !ok:
			t.Errorf("%s: in %s, not in %s", path, one, other)
		case otherContent != content:
			t.Errorf("%s: differs between %s and %s", path, one, other)
		}
	}
}

// appFiles is every file in app by its path, with the app's name in
// paths and contents replaced by APP or app.
func appFiles(t *testing.T, app string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(app, func(path string, entry fs.DirEntry,
		err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		rel, _ := filepath.Rel(app, path)
		content := strings.ReplaceAll(string(body), strings.ToUpper(app),
			"APP")
		got[strings.ReplaceAll(rel, app, "APP")] = strings.ReplaceAll(
			content, app, "app")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestUsageListsTheScaffoldFlag(t *testing.T) {
	err := runNew(nil)
	want := "--scaffold=bare|single|accounts"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("usage = %v, want it to list %s", err, want)
	}
}

// A bad choice is refused by runNew, not by each shape's Write: finding
// x after the refusal would mean a shape made its directory first.
func TestNewChecksChoicesBeforeAnyShapeWrites(t *testing.T) {
	for _, shape := range []string{"bare", "single", "accounts"} {
		t.Run(shape, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := runNew([]string{"--scaffold", shape, "--theme=nope",
				"x"})
			want := `unknown theme "nope"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want the unknown theme refusal", err)
			}
			if _, statErr := os.Stat("x"); statErr == nil {
				t.Fatal("x exists after a refused choice")
			}
		})
	}
}
