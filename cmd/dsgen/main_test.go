package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosframework/rastrillo/internal/designsystem"
)

// The output on disk has to be the render and the whole render. This is
// the only thing standing between designsystem.Render and a published
// site, and "wrote 369 files" in the summary line is not the same claim
// as "wrote the 369 files it rendered".
//
// Rendered at a mount that is not the default, because the mount is the
// argument most likely to be quietly ignored: a flag that is parsed,
// printed in the summary, and never reaches the renderer would pass a
// test that only ever asked for the default.
func TestWriteIsTheWholeRender(t *testing.T) {
	const mount = "/ui/gallery"
	want, err := designsystem.Render(mount)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	dir := t.TempDir()
	root, n, bytes, err := write(dir, mount)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(want) {
		t.Errorf("reported %d files, rendered %d", n, len(want))
	}

	var total int
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != string(body) {
			t.Fatalf("%s on disk differs from what Render produced", name)
		}
		total += len(body)
	}
	if bytes != total {
		t.Errorf("reported %d bytes, rendered %d", bytes, total)
	}

	var onDisk int
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			onDisk++
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if onDisk != len(want) {
		t.Errorf("%d files on disk, %d rendered — something was written that nothing renders", onDisk, len(want))
	}
}

// dsgen is run with a directory named on a command line, by people and
// by build scripts, and it deletes before it writes. The rule that makes
// that safe is that it only ever removes the top-level paths it is about
// to write: a stale page from a previous run goes, and anything else in
// the directory stays.
//
// The earlier, internal version of this command deleted its output root
// outright and wiped 152 unrelated files the one time it was pointed at
// the wrong directory. This is the gate on the replacement.
func TestWriteRemovesItsOwnStaleOutputAndNothingElse(t *testing.T) {
	dir := t.TempDir()

	// A neighbour: something the site keeps in the same directory.
	neighbour := filepath.Join(dir, "robots.txt")
	if err := os.WriteFile(neighbour, []byte("User-agent: *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "images")
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "logo.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A leftover from a render that no longer produces it, inside a
	// directory this run does write.
	stale := filepath.Join(dir, designsystem.RootTheme(), "en", "gone.html")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := write(dir, designsystem.DefaultMount); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("%s survived: a page that stopped being rendered has to stop existing (err=%v)", stale, err)
	}
	for _, keep := range []string{neighbour, filepath.Join(kept, "logo.svg")} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was removed: dsgen must not touch what it did not write (%v)", keep, err)
		}
	}
}

// The mount reaches dsgen from a command line, so it takes the shapes a
// person types. Each of these is either normalised or refused, and the
// one that is refused is refused because the tree writes tokens.css
// beside its theme directories: mounted at the site root it would land
// on whatever the site already keeps there.
func TestMountShapes(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"/design-system", "/design-system", true},
		{"/design-system/", "/design-system", true},
		{"design-system", "/design-system", true},
		{"/ui/gallery/", "/ui/gallery", true},
		{"/", "", false},
		{"", "", false},
	} {
		got, err := designsystem.CleanMount(tc.in)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("CleanMount(%q) = %q, %v; want %q, no error", tc.in, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("CleanMount(%q) = %q, want an error", tc.in, got)
		}
	}
}
