package tmplgen

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// driftMain renders each of its templates through tmpl and through
// html/template, and prints whether tmpl ran compiled code and whether
// the two agree: the same bytes, or both an error, whose text and
// partial output are not a contract.
const driftMain = `package main

import (
	"fmt"
	"html/template"
	"os"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

const text = ` + "`" + `{{define "numbers"}}` + "`" + ` +
	` + "`" + `<p>{{.N}} {{.C}}</p>{{end}}` + "`" + ` +
	` + "`" + `{{define "label"}}{{.Label}}{{end}}` + "`" + `

var (
	set = tmpl.Must(tmpl.New("").Parse(text))
	std = template.Must(template.New("").Parse(text))
)

func main() {
	if path := os.Getenv(tmpl.CaptureEnv); path != "" {
		if err := tmpl.WriteCapture(path); err != nil {
			panic(err)
		}
		return
	}
	for _, name := range []string{"numbers", "label"} {
		var got, want strings.Builder
		gotErr := set.ExecuteTemplate(&got, name, sample())
		wantErr := std.ExecuteTemplate(&want, name, sample())
		bothRan := gotErr == nil && wantErr == nil
		same := bothRan && got.String() == want.String() ||
			gotErr != nil && wantErr != nil
		compiled := set.IsCompiled(name)
		fmt.Printf("%s: compiled=%t same=%t\n", name, compiled, same)
		if !same {
			fmt.Printf("  tmpl: %q %v\n  html/template: %q %v\n",
				got.String(), gotErr, want.String(), wantErr)
		}
	}
}
`

// driftBefore is the app's types when generate runs.
const driftBefore = `package main

type view struct {
	N int64
	C count
}

type count int

func (v view) Label() string { return "label" }

func sample() view { return view{N: -1, C: 3} }
`

// driftAfter changes them without generate running again, in ways the
// generated file still builds against: N becomes unsigned, count gains a
// String method, and Label a pointer receiver, which text/template does
// not find on a value. No name changes, so every fingerprint matches.
const driftAfter = `package main

import "math"

type view struct {
	N uint64
	C count
}

type count int

func (c count) String() string { return "three" }

func (v *view) Label() string { return "label" }

func sample() view { return view{N: math.MaxUint64, C: 3} }
`

const driftAgree = "numbers: compiled=true same=true\n" +
	"label: compiled=true same=true\n"

// TestTypedCodeFollowsChangedTypes: typed code is written for the types
// the app had when generate ran. Changed afterwards in ways the code
// still builds against, the app's data must not run that code: it is
// bound, but the data takes the code that reads by reflection, and the
// page renders what html/template renders.
func TestTypedCodeFollowsChangedTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scratch app three times")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offlineGo(t)
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"go.mod":           goMod("example.com/drift", root),
		"cmd/app/main.go":  driftMain,
		"cmd/app/types.go": driftBefore,
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
	if want := []string{"cmd/app/" + GenFile}; !slices.Equal(
		res.Written,
		want,
	) {
		t.Fatalf("Written = %v, want %v", res.Written, want)
	}
	gen, err := os.ReadFile(filepath.Join(app, "cmd", "app", GenFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gen), "case view:") ||
		!strings.Contains(string(gen), "tmplrt.WriteInt(b, int64(dot.N))") {
		t.Fatalf("no typed code for view:\n%s", gen)
	}

	bin := filepath.Join(t.TempDir(), "app")
	run(t, app, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/app")
	if out := run(t, app, bin); out != driftAgree {
		t.Fatalf("before the types change:\n%s", out)
	}

	writeFiles(t, app, map[string]string{"cmd/app/types.go": driftAfter})
	run(t, app, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/app")
	if out := run(t, app, bin); out != driftAgree {
		t.Errorf("after the types change:\n%s", out)
	}
}

// newerMain is an app whose rastrillo writes compiled code of another
// generator version than this checkout's: it captures as usual, then
// rewrites the version, as a newer rastrillo's capture would carry it.
const newerMain = `package main

import (
	"encoding/json"
	"os"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

var page = tmpl.Must(tmpl.New("page").Parse("<p>{{.}}</p>"))

func main() {
	path := os.Getenv(tmpl.CaptureEnv)
	if path == "" {
		return
	}
	if err := tmpl.WriteCapture(path); err != nil {
		panic(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil {
		panic(err)
	}
	c["GeneratorVersion"] = 99
	data, err = json.Marshal(c)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		panic(err)
	}
}
`

// TestRunRefusesAnotherGeneratorVersion: the app's own rastrillo decides
// the fingerprints compiled code registers under, which take in its
// generator version. Code another version writes would be registered
// under them and run, the behaviour the version bump was to retire, so
// Run refuses, naming the main and what to run instead, and writes
// nothing.
func TestRunRefusesAnotherGeneratorVersion(t *testing.T) {
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
		"go.mod":          goMod("example.com/newer", root),
		"cmd/app/main.go": newerMain,
	})
	run(t, app, "go", "mod", "tidy")
	for _, check := range []bool{false, true} {
		_, err := Run(app, Options{Check: check})
		if err == nil ||
			!strings.Contains(err.Error(), "example.com/newer/cmd/app") ||
			!strings.Contains(err.Error(), "99") ||
			strings.Contains(err.Error(), "\n") {
			t.Errorf("check %t: Run = %v, want one line naming the main "+
				"and its generator version", check, err)
		}
	}
	gen := filepath.Join(app, "cmd", "app", GenFile)
	if _, err := os.Stat(gen); !os.IsNotExist(err) {
		t.Errorf(
			"Run wrote compiled code for another generator (stat err %v)",
			err,
		)
	}
}

// htmlMain renders the set package template builds, through tmpl, and
// the same text through html/template, as driftMain does.
const htmlMain = `package main

import (
	"fmt"
	htmltemplate "html/template"
	"os"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"example.com/samename/template"
)

func main() {
	if path := os.Getenv(tmpl.CaptureEnv); path != "" {
		if err := tmpl.WriteCapture(path); err != nil {
			panic(err)
		}
		return
	}
	std := htmltemplate.Must(htmltemplate.New("body").Parse(template.Text))
	var got, want strings.Builder
	gotErr := template.Set.Execute(&got, template.Sample())
	wantErr := std.Execute(&want, template.Sample())
	same := gotErr == nil && wantErr == nil && got.String() == want.String()
	fmt.Printf("compiled=%t same=%t\n", template.Set.IsCompiled("body"), same)
	if got.String() != want.String() {
		fmt.Printf("  tmpl: %q %v\n  html/template: %q %v\n",
			got.String(), gotErr, want.String(), wantErr)
	}
}
`

// ownHTML is a package named template with an HTML type of its own,
// which html/template escapes like any string.
const ownHTML = `package template

import "amadan.net/rastrillo/rastrillo/tmpl"

const Text = "<p>{{.Body}}</p>"

var Set = tmpl.Must(tmpl.New("body").Parse(Text))

type HTML string

type view struct{ Body HTML }

func Sample() any { return view{Body: "<b>"} }
`

// stdHTML makes Body html/template's HTML, which it writes as it is.
// Both types print as template.HTML: only the import path tells them
// apart.
const stdHTML = `package template

import (
	htmltemplate "html/template"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

const Text = "<p>{{.Body}}</p>"

var Set = tmpl.Must(tmpl.New("body").Parse(Text))

type HTML string

type view struct{ Body htmltemplate.HTML }

func Sample() any { return view{Body: "<b>"} }
`

// TestTypedCodeTellsPackagesApart: a fact names a type by its import
// path, not its package name, so a field moved to another package's
// type of the same name and package name takes the code that reads by
// reflection, and renders what html/template renders.
func TestTypedCodeTellsPackagesApart(t *testing.T) {
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
		"go.mod":               goMod("example.com/samename", root),
		"cmd/app/main.go":      htmlMain,
		"template/template.go": ownHTML,
	})
	run(t, app, "go", "mod", "tidy")
	res, err := Run(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"template/" + GenFile}; !slices.Equal(
		res.Written,
		want,
	) ||
		len(res.Unsupported) > 0 {
		t.Fatalf(
			"Written = %v, want %v; unsupported %+v",
			res.Written,
			want,
			res.Unsupported,
		)
	}
	gen, err := os.ReadFile(filepath.Join(app, "template", GenFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gen), "case view:") ||
		!strings.Contains(
			string(gen),
			"WriteHTMLEscaped(b, string(dot.Body))",
		) {
		t.Fatalf("no typed code escaping Body:\n%s", gen)
	}

	bin := filepath.Join(t.TempDir(), "app")
	run(t, app, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/app")
	if out := run(t, app, bin); out != "compiled=true same=true\n" {
		t.Fatalf("before the type changes:\n%s", out)
	}
	writeFiles(t, app, map[string]string{"template/template.go": stdHTML})
	run(t, app, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/app")
	if out := run(t, app, bin); out != "compiled=true same=true\n" {
		t.Errorf("after Body becomes html/template.HTML:\n%s", out)
	}
}
