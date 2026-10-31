package tmplgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// offlineGo makes the module cache the proxy, so that a scratch module
// resolves this checkout's dependencies without a network, as
// localecheck's tests do.
func offlineGo(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv(
		"GOPROXY",
		"file://"+strings.TrimSpace(string(out))+"/cache/download",
	)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// goMod is a scratch module's go.mod, which requires this checkout of
// rastrillo, at root, through a replace.
func goMod(module, root string) string {
	return "module " + module + "\n\ngo 1.25.0\n\n" +
		"require amadan.net/rastrillo/rastrillo v0.0.0\n\n" +
		"replace amadan.net/rastrillo/rastrillo => " + root + "\n"
}

// libGen is a generated file left in package lib.
const libGen = "//go:build !rastrillo_capture\n\npackage lib\n"

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

const appMain = `package main

import (
	"fmt"
	"os"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"example.com/app/lib"
)

var home = tmpl.Must(tmpl.New("home").Parse("<p>{{.}}</p>"))

// label is declared in package main, which capture records as "main".
type label string

func shout(l label) string { return strings.ToUpper(string(l)) }

var tagged = tmpl.Must(tmpl.New("tagged").
	Funcs(tmpl.FuncMap{"shout": shout}).Parse("{{shout .}}"))

func main() {
	if path := os.Getenv(tmpl.CaptureEnv); path != "" {
		if err := tmpl.WriteCapture(path); err != nil {
			panic(err)
		}
		return
	}
	var b strings.Builder
	if err := home.Execute(&b, lib.Name); err != nil {
		panic(err)
	}
	fmt.Println(home.IsCompiled("home"), b.String())
	b.Reset()
	if err := tagged.Execute(&b, label(lib.Name)); err != nil {
		panic(err)
	}
	fmt.Println(tagged.IsCompiled("tagged"), b.String())
}
`

// TestRunCompilesAnApp runs generate on a scratch app with the default
// mains. Its sets are built in package main, which tmpl records as owner
// "main", and one calls a function whose parameter type is main's own
// unexported one, which the generated code names to call it directly. lib,
// which the app imports, and unused, which nothing imports, have
// generated files left from sets they no longer build. The app built
// normally afterwards renders through the code.
func TestRunCompilesAnApp(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scratch app twice")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offlineGo(t)
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"go.mod":           goMod("example.com/app", root),
		"cmd/app/main.go":  appMain,
		"lib/lib.go":       "package lib\n\nconst Name = \"<lib>\"\n",
		"lib/" + GenFile:   libGen,
		"unused/unused.go": "package unused\n",
	})
	orphan := map[string]string{
		"unused/" + GenFile: "//go:build !rastrillo_capture\n\n" +
			"package unused\n",
	}
	writeFiles(t, app, orphan)
	run(t, app, "go", "mod", "tidy")

	res, err := Run(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CaptureFailures) > 0 {
		t.Fatalf("capture failed: %+v", res.CaptureFailures)
	}
	wantWritten := []string{"cmd/app/" + GenFile}
	if !slices.Equal(res.Written, wantWritten) {
		t.Errorf("Written = %v, want %v", res.Written, wantWritten)
	}
	if want := []string{"lib/" + GenFile, "unused/" + GenFile}; !slices.Equal(
		res.Removed,
		want,
	) {
		t.Errorf("Removed = %v, want %v", res.Removed, want)
	}
	gen, err := os.ReadFile(filepath.Join(app, "cmd", "app", GenFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gen), "package main\n") ||
		!strings.Contains(string(gen), `tmplrt.RegisterOwner("main", nil)`) ||
		!strings.Contains(string(gen), "func(label) string") {
		t.Errorf("generated file is not main's:\n%s", gen)
	}

	check, err := Run(app, Options{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Stale) > 0 || len(check.Written) > 0 {
		t.Errorf("a check right after generating found %+v", check)
	}

	// An orphan outside the mains' imports is stale by default, and none
	// of an explicit list's business.
	writeFiles(t, app, orphan)
	check, err = Run(app, Options{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"unused/" + GenFile}; !slices.Equal(check.Stale, want) {
		t.Errorf("default check: Stale = %v, want %v", check.Stale, want)
	}
	check, err = Run(app, Options{Mains: []string{"./cmd/app"}, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Stale) > 0 {
		t.Errorf(
			"check with explicit mains: Stale = %v, want none",
			check.Stale,
		)
	}

	bin := filepath.Join(t.TempDir(), "app")
	run(t, app, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/app")
	want := "true <p>&lt;lib&gt;</p>\ntrue &lt;LIB&gt;\n"
	if got := run(t, app, bin); got != want {
		t.Errorf("the app printed %q, want %q", got, want)
	}
}

// vetMain has templates whose generated code once failed to build or
// vet: a $ of their own, which must not leave the dot's $ declared and
// unused; range bodies that always end in a break or continue, which
// must not be followed by an unreachable return; and commands that fail
// as written, with the wrong number of arguments for a function called
// directly or for a builtin, or as a variable given an argument, whose
// piped value must be evaluated and then not left unused. typed reads
// main's own page type, which gets typed code: Go for loops broken and
// continued, a {{break}} in a range's {{else}}, range variables that
// shadow what is ranged over, a struct that is always true, a template
// specialised for what it is given, also given a value whose type is
// an alias another package declares, and a $ assigned another type.
const vetMain = `package main

import (
	"os"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"example.com/vetapp/lib"
)

var shadow = tmpl.Must(tmpl.New("shadow").
	Parse(` + "`" + `{{$ := "x"}}{{$}}` + "`" + `))

var assigned = tmpl.Must(tmpl.New("assigned").
	Parse(` + "`" + `{{$ = "x"}}` + "`" + `))

var ranges = tmpl.Must(tmpl.New("ranges").
	Parse(` + "`" + `{{range .}}{{break}}{{end}}` +
	`{{range .}}{{continue}}{{end}}` +
	`{{range .}}{{if .}}{{break}}{{else}}{{continue}}{{end}}` +
	`never{{end}}` + "`" + `))

func f(s string) string { return s }

var arity = tmpl.Must(tmpl.New("arity").Funcs(tmpl.FuncMap{"f": f}).
	Parse(` + "`" + `{{"x" | f "y"}}` +
	`{{f "a" | len "extra"}}` +
	`{{f "a" | $}}` +
	`{{"x" | f "y" | f}}` + "`" + `))

type item struct {
	Name string
	Done bool
}

func (i *item) Mark() string { return "marked " + i.Name }

type page struct {
	Title string
	Items []item
	Tags  map[string]int
	Grid  [2]item
	N     uint8
	Owner *item
	One   item
	Label lib.Text
}

var typed = tmpl.Must(tmpl.New("typed").
	Parse(` + "`" + `{{define "sub"}}{{.Name}}{{end}}` +
	`{{.Title}}{{range $i, $e := .Items}}{{if $e.Done}}{{break}}{{end}}` +
	`{{if not $e.Name}}{{continue}}{{end}}{{$i}}{{$e.Mark}}{{end}}` +
	`{{range .Items}}{{range 0}}{{else}}{{break}}{{end}}` +
	`{{template "sub" .}}{{end}}` +
	`{{range $k, $v := .Tags}}{{$k}}{{$v}}{{else}}none{{end}}` +
	`{{range .Grid}}{{.Name}}{{end}}{{range .N}}{{.}}{{else}}zero{{end}}` +
	`{{with .One}}{{.Name}}{{end}}{{if .One}}one{{end}}` +
	`{{template "sub" .Owner}}{{$ = .Title}}{{$}}` +
	`{{define "label"}}<b>{{.}}</b>{{end}}` +
	`{{template "label" .Label}}` + "`" + `))

func main() {
	if path := os.Getenv(tmpl.CaptureEnv); path != "" {
		if err := tmpl.WriteCapture(path); err != nil {
			panic(err)
		}
	}
}
`

// TestGeneratedCodeVets generates code for templates that once produced
// a file that did not build or vet, and runs go vet on it.
func TestGeneratedCodeVets(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scratch app")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offlineGo(t)
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"go.mod":          goMod("example.com/vetapp", root),
		"cmd/app/main.go": vetMain,
		"lib/lib.go": "package lib\n\n" +
			"// Text is an alias, which a specialised template's " +
			"parameter must\n// not spell as its own name.\n" +
			"type Text = string\n",
	})
	run(t, app, "go", "mod", "tidy")
	res, err := Run(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CaptureFailures) > 0 || len(res.Unsupported) > 0 {
		t.Fatalf(
			"capture failures %+v, unsupported %+v",
			res.CaptureFailures,
			res.Unsupported,
		)
	}
	wantWritten := []string{"cmd/app/" + GenFile}
	if !slices.Equal(res.Written, wantWritten) {
		t.Fatalf("Written = %v, want %v", res.Written, wantWritten)
	}
	gen, err := os.ReadFile(filepath.Join(app, "cmd", "app", GenFile))
	if err != nil {
		t.Fatal(err)
	}
	// f is called directly, so its wrong argument count is known when the
	// code is generated, the case that left a variable unused.
	if !strings.Contains(string(gen), "func(string) string") {
		t.Errorf("f is not called directly:\n%s", gen)
	}
	for _, want := range []string{
		"case page:",
		"case *page:",
		`renders "label" for a dot of type string.`,
	} {
		if !strings.Contains(string(gen), want) {
			t.Errorf("typed has no %q:\n%s", want, gen)
		}
	}
	run(t, app, "go", "vet", "-buildvcs=false", "./...")
}

// A main that does not import tmpl cannot build a set, so Run does not
// start it: a tool such as cmd/genvectors would otherwise run its real
// work in capture mode and be reported as failing to capture. It still
// speaks for the packages it imports, so a file left in one of them is
// removed even when the mains are listed.
func TestRunSkipsAMainWithoutTemplates(t *testing.T) {
	if testing.Short() {
		t.Skip("lists a scratch module")
	}
	offlineGo(t)
	app := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	writeFiles(t, app, map[string]string{
		"go.mod": "module example.com/tool\n\ngo 1.25.0\n",
		"cmd/tool/main.go": "package main\n\n" +
			"import (\n\t\"os\"\n\n\t\"example.com/tool/lib\"\n)\n\n" +
			"func main() { os.WriteFile(" + strconv.Quote(marker) +
			", []byte(lib.Name), 0o644) }\n",
		"lib/lib.go":     "package lib\n\nconst Name = \"lib\"\n",
		"lib/" + GenFile: libGen,
	})
	res, err := Run(app, Options{Mains: []string{"./cmd/tool"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CaptureFailures) > 0 {
		t.Errorf("CaptureFailures = %+v, want none", res.CaptureFailures)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf(
			"Run started a main that cannot build a set (stat err %v)",
			err,
		)
	}
	if want := []string{"lib/" + GenFile}; !slices.Equal(res.Removed, want) {
		t.Errorf("Removed = %v, want %v", res.Removed, want)
	}
}

// A main whose imports did not all load may import tmpl through the
// one that is missing, so Run does not take it for a main without
// templates: it reports the main, and leaves the generated files of the
// packages it imports alone rather than pruning them as orphans.
func TestRunCapturesAMainWithAMissingImport(t *testing.T) {
	if testing.Short() {
		t.Skip("lists a scratch module")
	}
	offlineGo(t)
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"go.mod": "module example.com/broken\n\ngo 1.25.0\n\n" +
			"require example.com/dep v0.0.0\n\n" +
			"replace example.com/dep => ./nowhere\n",
		"cmd/app/main.go": "package main\n\n" +
			"import (\n\t_ \"example.com/broken/lib\"\n" +
			"\t_ \"example.com/dep/views\"\n)\n\nfunc main() {}\n",
		"lib/lib.go":     "package lib\n",
		"lib/" + GenFile: libGen,
	})
	res, err := Run(app, Options{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stale) > 0 {
		t.Errorf(
			"Stale = %v, want none: the main's sets are unknown",
			res.Stale,
		)
	}
	if len(res.CaptureFailures) != 1 {
		t.Errorf(
			"CaptureFailures = %+v, want the main reported",
			res.CaptureFailures,
		)
	}
}
