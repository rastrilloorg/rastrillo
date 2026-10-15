package tmplmigrate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Round nine, finding 3: a dry run exits non-zero while an automatic edit
// is pending, so a preflight that only runs the dry run fails on an
// unmigrated app, even one with nothing to review.
func TestDryRunFailsWhileEditsArePending(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.html")
	const src = `{{T "app.hello"}}`
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if code := Run([]string{dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "pending: 1 templates") {
		t.Fatalf("dry run: exit %d, want 1\n%s%s", code, out.String(), errOut.String())
	}
	if b, _ := os.ReadFile(file); string(b) != src {
		t.Fatalf("the dry run changed the file: %s", b)
	}
	out.Reset()
	if code := Run([]string{"-write", dir}, &out, &errOut); code != 0 {
		t.Fatalf("-write: exit %d, want 0\n%s", code, out.String())
	}
	if b, _ := os.ReadFile(file); string(b) != `{{$L := localizer $}}{{T $L "app.hello"}}` {
		t.Fatalf("-write wrote %s", b)
	}
	out.Reset()
	if code := Run([]string{dir}, &out, &errOut); code != 0 {
		t.Fatalf("dry run after -write: exit %d, want 0\n%s", code, out.String())
	}
}

// A Go finding alone also fails the preflight, and so does a key the
// type checker cannot resolve to a constant (a review item).
func TestGoFindingsFailThePreflight(t *testing.T) {
	for name, src := range map[string]string{
		`function map entry "T"`:       `var f = template.FuncMap{"T": nil}`,
		`key name() is not a constant`: `func name() string { return "x" }` + "\n\nvar f = template.FuncMap{name(): nil}",
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/t\n\ngo 1.25.0\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "app.html"), []byte(`<p>no locale calls</p>`), 0o644)
		os.WriteFile(filepath.Join(dir, "render.go"), []byte("package app\n\nimport \"html/template\"\n\n"+src+"\n"), 0o644)
		var out, errOut strings.Builder
		if code := Run([]string{dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), name) {
			t.Errorf("%s: exit %d, want 1\n%s%s", name, code, out.String(), errOut.String())
		}
	}
}

func TestRunSkipsGeneratedTemplatesButSaysSo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "gen", "templates", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "gen", "templates", "x", "list.html"), []byte(`{{T "k"}}`), 0o644)
	var out strings.Builder
	code := Run([]string{"-write", dir}, &out, io.Discard)
	src, _ := os.ReadFile(filepath.Join(dir, "gen", "templates", "x", "list.html"))
	if string(src) != `{{T "k"}}` {
		t.Errorf("Run rewrote a generated file: %q", src)
	}
	if code != 1 || !strings.Contains(out.String(), "rastrillo generate") {
		t.Errorf("exit %d, output %q; want exit 1 and the advice to regenerate", code, out.String())
	}
}

// static/ holds vendored copies doctor compares byte for byte, so Run
// never rewrites there. A file the rewrite cannot handle is reported
// and left alone, and the run still migrates the rest.
func TestRunSkipsStaticAndReportsABadFileWithoutStopping(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "static"), 0o755)
	os.WriteFile(filepath.Join(dir, "static", "v.html"), []byte(`{{T "k"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.html"), []byte(`{{T "k"`), 0o644)
	os.WriteFile(filepath.Join(dir, "b.html"), []byte(`{{T "k"}}`), 0o644)
	var out strings.Builder
	code := Run([]string{"-write", dir}, &out, io.Discard)
	if b, _ := os.ReadFile(filepath.Join(dir, "static", "v.html")); string(b) != `{{T "k"}}` {
		t.Errorf("Run rewrote static/: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.html")); !strings.Contains(string(b), "$L") {
		t.Errorf("a bad file stopped the run: b.html is %q", b)
	}
	if code != 1 || !strings.Contains(out.String(), "refused: ") || !strings.Contains(out.String(), "a.html") {
		t.Errorf("exit %d, output %q; want exit 1 and a.html refused", code, out.String())
	}
}

func TestRunNeedsAPath(t *testing.T) {
	if code := Run(nil, io.Discard, io.Discard); code != 2 {
		t.Errorf("Run with no PATH: exit %d, want 2", code)
	}
}
