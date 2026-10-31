package tmpl_test

import (
	"encoding/json"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	texttemplate "text/template"
	"text/template/parse"
	"time"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// capture writes what this process has recorded and reads it back.
func capture(t *testing.T) tmpl.Capture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := tmpl.WriteCapture(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var captured tmpl.Capture
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	return captured
}

// startCapture turns capture on for one test with nothing recorded.
func startCapture(t *testing.T) {
	t.Helper()
	t.Setenv(tmpl.CaptureEnv, "on")
	tmpl.ResetCapture()
	t.Cleanup(tmpl.ResetCapture)
}

func findSet(
	t *testing.T,
	captured tmpl.Capture,
	name string,
) tmpl.CapturedSet {
	t.Helper()
	for _, set := range captured.Sets {
		if set.Name == name {
			return set
		}
	}
	t.Fatalf("no captured set named %q in %d sets", name, len(captured.Sets))
	return tmpl.CapturedSet{}
}

func reparse(t *testing.T, name, src string) string {
	t.Helper()
	tree := parse.New(name)
	tree.Mode = parse.SkipFuncCheck
	if _, err := tree.Parse(src, "", "", map[string]*parse.Tree{}); err != nil {
		t.Fatalf("captured source of %q does not parse: %v\n%s", name, err, src)
	}
	return tree.Root.String()
}

func TestCaptureRoundTrip(t *testing.T) {
	startCapture(t)
	page := tmpl.Must(tmpl.New("roundtrip").Funcs(tmpl.FuncMap{
		"up":     strings.ToUpper,
		"double": func(number int) int { return number * 2 },
		"join": func(parts ...string) string {
			return strings.Join(parts, ",")
		},
	}).Parse(`{{block "sidebar" .}}default sidebar{{end}}
{{- define "item"}}<li>{{up .}}</li>{{end -}}
<ul>{{range .Items}}{{template "item" .}}{{end}}</ul>
{{- /* gone */ -}}
{{if gt (double .N) 2}}big{{else if .N}}small{{else}}none{{end}}
{{join "a" "b"}}`))
	// A define override, as an app does to replace a partial.
	override := `{{define "item"}}<li class="x">{{.}}</li>{{end}}`
	if _, err := page.Parse(override); err != nil {
		t.Fatal(err)
	}

	set := findSet(t, capture(t), "roundtrip")
	for _, name := range []string{"roundtrip", "sidebar", "item"} {
		src, ok := set.Templates[name]
		if !ok {
			t.Fatalf(
				"template %q not captured; have %v",
				name,
				slices.Sorted(mapKeys(set.Templates)),
			)
		}
		if got := reparse(t, name, src); got != src {
			t.Errorf(
				"round trip of %q changed it:\n got %q\nwant %q",
				name,
				got,
				src,
			)
		}
		if set.Fingerprints[name] == "" {
			t.Errorf("no fingerprint for %q", name)
		}
	}
	if !strings.Contains(set.Templates["item"], `class="x"`) {
		t.Errorf("item = %q, want the override", set.Templates["item"])
	}
	if len(set.Uncompilable) != 0 {
		t.Errorf("Uncompilable = %v, want none", set.Uncompilable)
	}
	for _, name := range []string{"up", "double", "join"} {
		if set.Funcs[name].Expr == "" {
			t.Errorf("func %q not captured: %+v", name, set.Funcs)
		}
	}
	if got := set.Funcs["join"].Expr; got != "func(...string) string" {
		t.Errorf("join = %q", got)
	}
	if set.Owner != "amadan.net/rastrillo/rastrillo/tmpl_test" {
		t.Errorf("Owner = %q", set.Owner)
	}
	if set.Option != "default" {
		t.Errorf("Option = %q, want default", set.Option)
	}
}

func mapKeys[V any](entries map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range entries {
			if !yield(key) {
				return
			}
		}
	}
}

// A source that does not survive being parsed again with the default
// delimiters cannot be compiled from what capture holds. A set with
// Delims is the case that happens: its text can hold "{{".
func TestCaptureListsATemplateWhoseRoundTripDiffers(t *testing.T) {
	startCapture(t)
	tmpl.Must(tmpl.New("delimited").Delims("<<", ">>").Parse(`a {{ b << .X >>`))

	set := findSet(t, capture(t), "delimited")
	reason := set.Uncompilable["delimited"]
	if !strings.Contains(reason, "round trip") {
		t.Errorf("reason = %q, want it to name the round trip", reason)
	}
	if !strings.Contains(reason, "Delims") {
		t.Errorf("reason = %q, want it to name Delims too", reason)
	}
}

func TestCaptureListsEveryTemplateOfASetWithDelims(t *testing.T) {
	startCapture(t)
	tmpl.Must(
		tmpl.New("delims2").
			Delims("[[", "]]").
			Parse(`[[define "inner"]]x[[end]][[template "inner"]]`),
	)

	set := findSet(t, capture(t), "delims2")
	for _, name := range []string{"delims2", "inner"} {
		if !strings.Contains(set.Uncompilable[name], "Delims") {
			t.Errorf(
				"Uncompilable[%q] = %q, want Delims",
				name,
				set.Uncompilable[name],
			)
		}
	}
}

func TestCaptureListsASetThatDefinesADerivedName(t *testing.T) {
	startCapture(t)
	tmpl.Must(tmpl.New("derived").Parse(
		`{{define "p$htmltemplate_stateJS_elementScript"}}x{{end}}` +
			`<b>{{.}}</b>`),
	)

	set := findSet(t, capture(t), "derived")
	for _, name := range []string{
		"derived", "p$htmltemplate_stateJS_elementScript",
	} {
		if !strings.Contains(set.Uncompilable[name], "$htmltemplate_") {
			t.Errorf("Uncompilable[%q] = %q", name, set.Uncompilable[name])
		}
	}
}

// A FuncMap function called break or continue makes the parser read
// {{break}} and {{continue}} as calls of it. Generate reparses the
// captured sources without the functions, where they read as the
// control statements and print the same, so the set is not compiled.
func TestCaptureListsASetWithABreakOrContinueFunction(t *testing.T) {
	startCapture(t)
	for _, name := range []string{"break", "continue"} {
		set := "control-" + name
		tmpl.Must(
			tmpl.New(set).
				Funcs(tmpl.FuncMap{name: func() string { return "x" }}).
				Parse(`{{define "inner"}}x{{end}}` +
					`{{range .}}{{` + name + `}}{{end}}`),
		)
		got := findSet(t, capture(t), set)
		for _, entry := range []string{set, "inner"} {
			if !strings.Contains(got.Uncompilable[entry], `"`+name+`"`) {
				t.Errorf(
					"%s: Uncompilable[%q] = %q, want the function named",
					set,
					entry,
					got.Uncompilable[entry],
				)
			}
		}
	}
}

// Fingerprints are the ones the set computes for itself at runtime, and
// capturing does not escape the trees or use up the set's first run.
func TestCaptureFingerprintsMatchRuntimeAndDoNotExecute(t *testing.T) {
	startCapture(t)
	page := tmpl.Must(tmpl.New("fp").Option("missingkey=error").
		Parse(`<a href="{{.}}">x</a>{{template "part"}}` +
			`{{define "part"}}p{{end}}`),
	)

	set := findSet(t, capture(t), "fp")
	for name, fp := range set.Fingerprints {
		if want := tmpl.FingerprintOf(page, name); fp != want {
			t.Errorf("fingerprint of %q = %s, runtime says %s", name, fp, want)
		}
	}
	if set.Option != "error" {
		t.Errorf("Option = %q, want error", set.Option)
	}
	// Still unescaped, so it can still be fingerprinted and run.
	if again := capture(t); !reflect.DeepEqual(findSet(t, again, "fp"), set) {
		t.Error("capturing twice gave different answers")
	}
	var builder strings.Builder
	if err := page.Execute(&builder, "u"); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureOfAnExecutedSetListsItAsUncompilable(t *testing.T) {
	startCapture(t)
	page := tmpl.Must(tmpl.New("ran").Parse(`<i>{{.}}</i>`))
	before := tmpl.FingerprintOf(page, "ran")
	var builder strings.Builder
	if err := page.Execute(&builder, "x"); err != nil {
		t.Fatal(err)
	}

	set := findSet(t, capture(t), "ran")
	if set.Fingerprints["ran"] != before {
		t.Errorf(
			"fingerprint = %s, want the pre-execution %s",
			set.Fingerprints["ran"],
			before,
		)
	}
	if !strings.Contains(set.Uncompilable["ran"], "executed") {
		t.Errorf(
			"reason = %q, want it to say the set executed",
			set.Uncompilable["ran"],
		)
	}
}

func TestCaptureRecordsClonesAndOwner(t *testing.T) {
	startCapture(t)
	base := tmpl.Must(tmpl.New("base").Parse(`{{define "x"}}1{{end}}`))
	clone, err := base.Clone()
	if err != nil {
		t.Fatal(err)
	}
	tmpl.Must(clone.Parse(`{{define "x"}}2{{end}}`))
	tmpl.NewViaHelper("viahelper")

	captured := capture(t)
	var bases []tmpl.CapturedSet
	for _, set := range captured.Sets {
		if set.Name == "base" {
			bases = append(bases, set)
		}
	}
	if len(bases) != 2 {
		t.Fatalf(
			"got %d sets named base, want the set and its clone",
			len(bases),
		)
	}
	if bases[0].Templates["x"] == bases[1].Templates["x"] {
		t.Error("the clone's override was not captured separately")
	}
	// The set cloned from says so; the clone, never cloned, does not.
	if !bases[0].Cloned || bases[1].Cloned {
		t.Errorf(
			"Cloned = %v and %v, want the base only",
			bases[0].Cloned,
			bases[1].Cloned,
		)
	}
	// A framework package building on an app's behalf: the app owns it.
	got := findSet(t, captured, "viahelper").Owner
	if got != "amadan.net/rastrillo/rastrillo/tmpl_test" {
		t.Errorf("Owner = %q", got)
	}
}

func TestCaptureRecordsNothingWithoutTheVariable(t *testing.T) {
	t.Setenv(tmpl.CaptureEnv, "")
	tmpl.ResetCapture()
	tmpl.Must(tmpl.New("unrecorded").Parse(`x`))
	for _, set := range capture(t).Sets {
		if set.Name == "unrecorded" {
			t.Fatal("a set was recorded with capture off")
		}
	}
}

func TestCaptureRecordsTheMetaProbe(t *testing.T) {
	startCapture(t)
	tmpl.Must(tmpl.New("meta").Parse(`x`))
	set := findSet(t, capture(t), "meta")

	var builder strings.Builder
	probe := template.Must(
		template.New("").
			Parse(`<meta http-equiv="refresh" content="0;url={{.}}">`),
	)
	if err := probe.Execute(&builder, "javascript:x"); err != nil {
		t.Fatal(err)
	}
	want := !strings.Contains(builder.String(), "javascript:x")
	if set.MetaContentURLEscape != want {
		t.Errorf(
			"MetaContentURLEscape = %v, want %v",
			set.MetaContentURLEscape,
			want,
		)
	}
}

func TestWriteCaptureReportsAnUnwritablePath(t *testing.T) {
	err := tmpl.WriteCapture(
		filepath.Join(t.TempDir(), "missing", "dir", "c.json"),
	)
	if err == nil {
		t.Fatal("no error for an unwritable path")
	}
	var perr *os.PathError
	if !errors.As(err, &perr) {
		t.Errorf("err = %v, want a path error", err)
	}
}

type hidden struct{}

func TestTypeExpr(t *testing.T) {
	type visible struct{ N int }
	owner := reflect.TypeOf(hidden{}).PkgPath()
	// A function returning a type unexported in another package can only
	// be built with reflect.
	other := reflect.TypeOf(errors.New("x"))
	foreign := reflect.MakeFunc(
		reflect.FuncOf(nil, []reflect.Type{other}, false),
		func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.Zero(other)}
		}).Interface()

	cases := []struct {
		name     string
		fn       any
		owner    string
		expr     string
		imports  []string
		nameable bool
	}{
		{
			"plain",
			func(int) string { return "" },
			owner,
			"func(int) string",
			nil,
			true,
		},
		{
			"html",
			func(string) template.HTML { return "" },
			owner,
			"func(string) template.HTML",
			[]string{"html/template"},
			true,
		},
		{
			"variadic",
			func(...any) (map[string]any, error) { return nil, nil },
			owner,
			"func(...any) (map[string]any, error)",
			nil,
			true,
		},
		{"no results", func() {}, owner, "func()", nil, true},
		{
			"composite",
			func(
				*[]string, [2]int, map[string][]byte,
				chan int, <-chan int, chan<- int,
			) {
			},
			owner,
			"func(*[]string, [2]int, map[string][]uint8, " +
				"chan int, <-chan int, chan<- int)",
			nil,
			true,
		},
		{
			"func argument",
			func(func(int) bool) func() error { return nil },
			owner,
			"func(func(int) bool) func() error",
			nil,
			true,
		},
		{
			"interface",
			func(interface{ Len() int }) {},
			owner,
			"func(interface{ Len() int })",
			nil,
			true,
		},
		{
			"own unexported",
			func(hidden) *hidden { return nil },
			owner,
			"func(hidden) *hidden",
			nil,
			true,
		},
		{
			"own local",
			func(visible) {},
			owner,
			"func(visible)",
			nil,
			true,
		}, // provisional: see TestTypeExprRefs
		{"unexported elsewhere", foreign, owner, "", nil, false},
		{"unexported with no owner", func(hidden) {}, "", "", nil, false},
		{"exported elsewhere", func() *template.Template { return nil }, owner,
			"func() *template.Template", []string{"html/template"}, true},
		{"unexported, other owner", func(hidden) {}, "x/other", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := tmpl.TypeExpr(reflect.TypeOf(tc.fn), tc.owner)
			if sig.Nameable != tc.nameable {
				t.Fatalf(
					"Nameable = %v, want %v (%+v)",
					sig.Nameable,
					tc.nameable,
					sig,
				)
			}
			if !tc.nameable {
				return
			}
			if sig.Expr != tc.expr {
				t.Errorf("Expr = %q, want %q", sig.Expr, tc.expr)
			}
			if !slices.Equal(sig.Imports, tc.imports) {
				t.Errorf("Imports = %v, want %v", sig.Imports, tc.imports)
			}
			// Each import is named in Expr by its package's name.
			if len(sig.Qualifiers) != len(tc.imports) {
				t.Errorf(
					"Qualifiers = %v, want one for each of %v",
					sig.Qualifiers,
					tc.imports,
				)
			}
			for name, path := range sig.Qualifiers {
				if !slices.Contains(tc.imports, path) ||
					!strings.Contains(sig.Expr, name+".") {
					t.Errorf(
						"Qualifiers names %s as %s, which %q does not use",
						path,
						name,
						sig.Expr,
					)
				}
			}
		})
	}
}

// Two packages that spell the same name in one signature cannot both be
// imported under it.
func TestTypeExprRefusesTwoPackagesWithOneName(t *testing.T) {
	sig := tmpl.TypeExpr(
		reflect.TypeOf(func(*template.Template, *texttemplate.Template) {}),
		"x",
	)
	if sig.Nameable {
		t.Errorf(
			"Nameable with html/template and text/template together: %+v",
			sig,
		)
	}
}

// reflect cannot tell a type declared inside a function from a
// package-level one, so Nameable is provisional. Refs lets generate look
// each type up in the package's scope and drop the function if one is
// missing.
func TestTypeExprRefs(t *testing.T) {
	type local struct{ N int }
	owner := reflect.TypeOf(hidden{}).PkgPath()
	fn := func(
		local, hidden, template.HTML, []*template.Template, int,
	) error {
		return nil
	}

	sig := tmpl.TypeExpr(reflect.TypeOf(fn), owner)
	if !sig.Nameable {
		t.Fatalf("not nameable: %+v", sig)
	}
	want := []tmpl.TypeRef{
		{PkgPath: "html/template", Name: "HTML"},
		{PkgPath: "html/template", Name: "Template"},
		{PkgPath: owner, Name: "hidden"},
		{PkgPath: owner, Name: "local"},
	}
	// Sorted by path, then name.
	slices.SortFunc(want, func(left, right tmpl.TypeRef) int {
		return strings.Compare(
			left.PkgPath+"."+left.Name,
			right.PkgPath+"."+right.Name,
		)
	})
	if !slices.Equal(sig.Refs, want) {
		t.Errorf("Refs = %v, want %v", sig.Refs, want)
	}
	predeclared := reflect.TypeOf(func(int) string { return "" })
	if plain := tmpl.TypeExpr(predeclared, owner); len(plain.Refs) != 0 {
		t.Errorf("Refs = %v for predeclared types only", plain.Refs)
	}
}

type timeAlias = time.Time

// An embedded field is named for its type, so an alias for an embedded
// type is a field with another name, and the expression with the type's
// own name is not the same type.
func TestTypeExprRefusesAnEmbeddedAlias(t *testing.T) {
	owner := reflect.TypeOf(hidden{}).PkgPath()
	cases := []struct {
		name     string
		fn       any
		nameable bool
	}{
		{"alias", func(struct{ timeAlias }) {}, false},
		{"pointer to alias", func(struct{ *timeAlias }) {}, false},
		{"named", func(struct{ time.Time }) {}, true},
		{"pointer to named", func(struct{ *time.Time }) {}, true},
		{"named field of alias type", func(struct{ At timeAlias }) {}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := tmpl.TypeExpr(reflect.TypeOf(tc.fn), owner)
			if sig.Nameable != tc.nameable {
				t.Errorf(
					"Nameable = %v, want %v (%+v)",
					sig.Nameable,
					tc.nameable,
					sig,
				)
			}
		})
	}
}

// A copy is recorded the moment Clone adds it, and capture reads it
// under its own lock, which Clone does not hold while filling it in. So
// the copy has to be complete before it is added. Writing a capture from
// inside the recorder's add hook is the one deterministic way to look at
// the copy at the instant it is published: the window is nanoseconds
// wide, and a concurrent reader almost never lands in it (and Clone's
// own wrapping of the copy takes its lock afterwards, which orders
// everything a later reader sees).
func TestCaptureSeesAClonePublishedComplete(t *testing.T) {
	t.Setenv(tmpl.CaptureEnv, "")
	base := tmpl.Must(
		tmpl.New("published").Funcs(tmpl.FuncMap{"up": strings.ToUpper}).
			Option("missingkey=zero").Parse(`{{up .}}`),
	)
	startCapture(t)

	var seen []tmpl.CapturedSet
	tmpl.OnCaptureAdd(t, func() {
		for _, set := range capture(t).Sets {
			if set.Name == "published" {
				seen = append(seen, set)
			}
		}
	})
	if _, err := base.Clone(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("saw %d copies when the clone was added, want 1", len(seen))
	}
	if got := seen[0]; got.Option != "zero" || got.Funcs["up"].Expr == "" {
		t.Errorf("the copy was published before it was complete: %+v", got)
	}
}
