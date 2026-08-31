package main

import (
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/designsystem"
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

	// The stamp is the one file in the directory that is not the render.
	// Everything else on disk has to be something Render produced, or
	// dsgen has written something nobody asked for.
	if _, err := os.Stat(filepath.Join(root, stampName)); err != nil {
		t.Errorf("no %s stamp in the output: dsgen would refuse its own directory on the next run (%v)", stampName, err)
	}
	var onDisk int
	if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && p != filepath.Join(root, stampName) {
			onDisk++
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if onDisk != len(want) {
		t.Errorf("%d files on disk beside the stamp, %d rendered — something was written that nothing renders", onDisk, len(want))
	}
}

// A build directory that persists between runs is the case this has to
// get right, and the case the first version of this command got wrong.
//
// It removed only the top-level paths the CURRENT render produces, so a
// theme dropped or renamed between framework versions kept its whole
// directory and its stylesheet in the published output — this project
// renamed ink to day, so the shape is not hypothetical — and a site
// guard that checks which files are present never sees an extra one.
// That is the deleted freshness gate's failure mode one directory over:
// an output directory that outlives a render is a second copy, and a
// second copy can drift.
//
// So the rule is that the directory holds the render and nothing else,
// and the seeded residue below is the exact shape of the rename.
func TestWriteLeavesNoTraceOfAnEarlierRender(t *testing.T) {
	dir := t.TempDir()
	if _, _, _, err := write(dir, designsystem.DefaultMount); err != nil {
		t.Fatalf("first write: %v", err)
	}

	residue := []string{
		// A whole theme that no longer exists: ink was renamed to day.
		filepath.Join("ink", "en", "index.html"),
		filepath.Join("ink", "ar", "shells", "sidebar.html"),
		// Its stylesheet, a top-level name the current render does not
		// produce and so never used to be removed.
		"theme-ink.css",
		// And a page inside a directory this render does write, which is
		// the only case the old rule covered.
		filepath.Join(designsystem.RootTheme(), "en", "gone.html"),
	}
	for _, rel := range residue {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<!doctype html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := designsystem.Render(designsystem.DefaultMount)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, _, _, err := write(dir, designsystem.DefaultMount); err != nil {
		t.Fatalf("second write: %v", err)
	}

	for _, rel := range residue {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s survived the second run: the output directory has to be the render and nothing else (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ink")); !os.IsNotExist(err) {
		t.Errorf("the ink/ directory survived the second run")
	}
	// And the run that cleaned up still produced the whole gallery.
	for _, name := range []string{"index.html", "tokens.css", designsystem.RootTheme() + "/en/index.html"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s after the second run: %v", name, err)
		}
		if string(got) != string(want[name]) {
			t.Errorf("%s after the second run differs from the render", name)
		}
	}
}

// The other half: dsgen empties a directory, so it must only ever empty
// one it owns. A directory that is not empty and carries no stamp is one
// dsgen has not written, and the answer is to refuse and change nothing.
//
// This is what stands in for the guardRoot that could only protect one
// hardcoded path, and it is the check the 152-file incident asked for: a
// directory somebody meant to keep is almost never empty.
func TestWriteRefusesADirectoryItDoesNotOwn(t *testing.T) {
	dir := t.TempDir()
	theirs := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(theirs, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "photos")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := write(dir, designsystem.DefaultMount); err == nil {
		t.Fatal("dsgen emptied a directory it had never written: -out is not a directory it may take on faith")
	}
	for _, keep := range []string{theirs, sub} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was removed by a run that refused: a refusal must change nothing (%v)", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); !os.IsNotExist(err) {
		t.Error("a refused run wrote pages anyway")
	}

	// With the stamp there — which is what a directory dsgen wrote looks
	// like — the same directory is fair game.
	if err := os.WriteFile(filepath.Join(dir, stampName), []byte(stampBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := write(dir, designsystem.DefaultMount); err != nil {
		t.Fatalf("write into a stamped directory: %v", err)
	}
	if _, err := os.Stat(theirs); !os.IsNotExist(err) {
		t.Error("the stamped directory kept a file that is not part of the render")
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
