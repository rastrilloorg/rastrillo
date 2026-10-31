package tmpl

import (
	"bytes"
	htmltemplate "html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// fingerprintsOf parses sources into one set, in order, and returns its
// fingerprints.
func fingerprintsOf(
	t *testing.T,
	funcs FuncMap,
	option string,
	sources ...string,
) map[string]string {
	t.Helper()
	set := New("page")
	if funcs != nil {
		set.Funcs(funcs)
	}
	if option != "" {
		set.Option(option)
	}
	for _, source := range sources {
		Must(set.Parse(source))
	}
	return set.fingerprints()
}

func TestFingerprint(t *testing.T) {
	const (
		page   = `<main>{{template "header" .}}{{up .Title}}</main>`
		header = `{{define "header"}}<h1>{{.Title}}</h1>` +
			`{{template "logo"}}{{end}}`
		logo   = `{{define "logo"}}<img src="/logo.svg">{{end}}`
		footer = `{{define "footer"}}<footer>{{.}}</footer>{{end}}`
	)
	funcs := FuncMap{"up": strings.ToUpper}
	base := fingerprintsOf(t, funcs, "", page, header, logo, footer)
	if base["page"] == "" || base["header"] == "" || base["footer"] == "" {
		t.Fatalf("missing fingerprints: %v", base)
	}
	again := fingerprintsOf(t, funcs, "", page, header, logo, footer)
	if again["page"] != base["page"] {
		t.Error("two identical sets have different fingerprints")
	}

	changes := map[string]map[string]string{
		"the template's text": fingerprintsOf(
			t,
			funcs,
			"",
			`<main>{{template "header" .}}{{up .Title}}!</main>`,
			header,
			footer,
		),
		"a called template": fingerprintsOf(
			t,
			funcs,
			"",
			page,
			`{{define "header"}}<h2>{{.Title}}</h2>{{template "logo"}}{{end}}`,
			logo,
			footer,
		),
		"a template called through another": fingerprintsOf(
			t,
			funcs,
			"",
			page,
			header,
			`{{define "logo"}}<img src="/logo.png">{{end}}`,
			footer,
		),
		"an app re-defining a partial": fingerprintsOf(
			t,
			funcs,
			"",
			page,
			header,
			logo,
			footer,
			`{{define "header"}}<h1 class=x>{{.Title}}</h1>{{end}}`,
		),
		"a used function's type": fingerprintsOf(
			t,
			FuncMap{"up": func(any) string { return "" }},
			"",
			page,
			header,
			logo,
			footer,
		),
		"Option": fingerprintsOf(
			t,
			funcs,
			"missingkey=error",
			page,
			header,
			logo,
			footer,
		),
	}
	for what, fps := range changes {
		if fps["page"] == base["page"] {
			t.Errorf("changing %s did not change the fingerprint", what)
		}
	}

	uncalled := fingerprintsOf(
		t,
		funcs,
		"",
		page,
		header,
		logo,
		`{{define "footer"}}<footer>changed</footer>{{end}}`,
	)
	if uncalled["page"] != base["page"] {
		t.Error(
			"changing a template the entry never calls changed its fingerprint",
		)
	}
	if uncalled["footer"] == base["footer"] {
		t.Error("changing footer did not change footer's own fingerprint")
	}
	unusedFunc := fingerprintsOf(
		t,
		FuncMap{"up": strings.ToUpper, "other": strings.TrimSpace},
		"",
		page,
		header,
		logo,
		footer,
	)
	if unusedFunc["page"] != base["page"] {
		t.Error(
			"adding a function the entry never calls changed its fingerprint",
		)
	}
}

func TestFingerprintEnvironment(t *testing.T) {
	set := Must(New("page").Parse(`<p>{{.}}</p>`))
	trees := set.set.trees()
	env := currentEnv("default")
	base := fingerprint("page", trees, nil, env)

	other := env
	other.goVersion = "go1.99.0"
	if fingerprint("page", trees, nil, other) == base {
		t.Error("the Go version is not part of the fingerprint")
	}
	if env.goVersion != tmplescape.GoVersion {
		t.Errorf(
			"fingerprint uses Go version %q, want tmplescape.GoVersion %q",
			env.goVersion,
			tmplescape.GoVersion,
		)
	}
	other = env
	other.generatorVersion = env.generatorVersion + 1
	if fingerprint("page", trees, nil, other) == base {
		t.Error("the generator version is not part of the fingerprint")
	}
	if env.generatorVersion != tmplrt.GeneratorVersion {
		t.Errorf(
			"fingerprint uses generator version %d, "+
				"want tmplrt.GeneratorVersion %d",
			env.generatorVersion,
			tmplrt.GeneratorVersion,
		)
	}
	other = env
	other.metaContentURLEscape = !env.metaContentURLEscape
	if fingerprint("page", trees, nil, other) == base {
		t.Error("meta-refresh URL escaping is not part of the fingerprint")
	}
}

func TestMetaContentURLEscapeProbe(t *testing.T) {
	if !probeMetaContentURLEscape() {
		t.Error(
			"probe says html/template does not filter a meta-refresh URL; " +
				"that is its default",
		)
	}
	t.Setenv("GODEBUG", "htmlmetacontenturlescape=0")
	if probeMetaContentURLEscape() {
		t.Error("probe missed GODEBUG htmlmetacontenturlescape=0")
	}
}

// TestFingerprintBeforeEscaping holds prepare to fingerprinting the trees
// as parsed. html/template escapes a set's trees in place on its first
// Execute, so a fingerprint read from them afterwards would match
// nothing that rastrillo generate registered.
func TestFingerprintBeforeEscaping(t *testing.T) {
	const source = `{{define "item"}}<a href="{{.}}">{{.}}</a>{{end}}` +
		`<p>{{template "item" .}}</p>`
	fresh := Must(New("page").Parse(source)).fingerprints()

	set := Must(New("page").Parse(source))
	if err := set.Execute(&bytes.Buffer{}, "/x"); err != nil {
		t.Fatal(err)
	}
	if got := set.fingerprints(); got["page"] != fresh["page"] ||
		got["item"] != fresh["item"] {
		t.Errorf("after Execute: %v, want the fresh set's %v", got, fresh)
	}

	std := htmltemplate.Must(htmltemplate.New("page").Parse(source))
	item := std.Lookup("item").Tree
	before := item.Root.String()
	if err := std.Execute(&bytes.Buffer{}, "/x"); err != nil {
		t.Fatal(err)
	}
	if item.Root.String() == before {
		t.Fatal(
			"html/template no longer edits trees in place; " +
				"this test proves nothing",
		)
	}
}
