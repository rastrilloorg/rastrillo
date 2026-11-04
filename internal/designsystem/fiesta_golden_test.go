package designsystem

import (
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/fiesta"
	"amadan.net/rastrillo/fiesta/example"
	"amadan.net/rastrillo/rastrillo/ui"
)

// Fiesta's examples were seeded from this package's samples (Fiesta's
// hack/seed/_rastrillo/designsystem_fiestaseed_test.go), so the
// reference markup for every partial is Fiesta's. These tests render
// each seeded state the way the seed did and hold it to its example.

// inARow says how to read a component out of the row Fiesta 1.1 put
// around it, keyed by example path.
var inARow = map[string]string{
	"dropdown/a-filter-menu-with-one-choice-applied":   "rst-dropdown",
	"locale-menu/twelve-languages-current-highlighted": "rst-locale",
	"scheme-menu/three-choices-dark-current":           "rst-scheme",
	"scheme-menu/standalone":                           "rst-scheme",
}

// menuIdiom is the one idiom held to an example: the dropdown idiom
// became the dropdown example's menu-idiom state, in a list's toolbar.
const menuIdiom = "dropdown/menu-idiom"

// standalone is the scheme menu's first state with its forms' action
// removed, on a page that remembers the choice itself.
const standalone = "scheme-menu/standalone"

// differsFromFiesta names the example states a partial renders
// differently on purpose, each with its reason. An entry that no
// longer differs fails, so the map cannot outlive its reasons.
var differsFromFiesta = map[string]string{}

func TestEveryPartialMatchesItsFiestaExample(t *testing.T) {
	seed := newSeedRender(t)
	sources := fiesta.Sources()
	compared := map[string]bool{}
	states := 0
	for _, path := range slices.Sorted(maps.Keys(sources)) {
		partial, ours, ok := seed.state(path, sources[path])
		if !ok {
			continue
		}
		compared[partial] = true
		states++
		holdToFiesta(t, path, partial, ours)
	}
	// Over a hundred states, as Fiesta's own coverage gates ask, so an
	// empty or misread record cannot pass.
	if states < 100 {
		t.Fatalf("compared %d example states; Fiesta seeded over 130", states)
	}
	seeded := seededPartials(sources)
	for file, defined := range partialFiles(t) {
		for _, name := range defined {
			if seeded[name] && !compared[name] {
				t.Errorf("%s has a Fiesta example, but %s went uncompared",
					file, name)
			}
		}
	}
}

// holdToFiesta fails when a state differs from its example and has no
// exception, or has one and no longer differs.
func holdToFiesta(t *testing.T, path, partial, ours string) {
	t.Helper()
	reason, excepted := differsFromFiesta[path]
	difference := firstDifference(ours, fiestaMarkup(t, path))
	switch {
	case difference != "" && !excepted:
		t.Errorf("%s (Fiesta's %s) differs from its example:\n%s",
			partial, path, difference)
	case difference == "" && excepted:
		t.Errorf("%s matches its example now; drop its exception (%s)",
			path, reason)
	}
}

// seedRender renders the gallery's states as Fiesta's seed did.
type seedRender struct {
	t    *testing.T
	tree *template.Template
	docs map[string]partialDoc
}

func newSeedRender(t *testing.T) seedRender {
	tree, err := partialTree("en")
	if err != nil {
		t.Fatal(err)
	}
	if err := parseRawSamples(tree); err != nil {
		t.Fatal(err)
	}
	docs := map[string]partialDoc{}
	for _, family := range families() {
		for _, doc := range family.Partials {
			docs[doc.Name] = doc
		}
	}
	return seedRender{t: t, tree: tree, docs: docs}
}

// state is the markup one example was seeded from and the partial that
// renders it, or not ok for an example of something other than a
// partial: a shell, a format or an idiom no partial emits.
func (seed seedRender) state(
	path string, source fiesta.Source,
) (partial, markup string, ok bool) {
	switch {
	case source.Kind == "partial":
		markup = seed.partial(source.Source, source.State)
		if path == standalone {
			markup = strings.ReplaceAll(markup, ` action="/_theme"`, "")
		}
		return source.Source, markup, true
	case source.Kind == "screen":
		return "signin", seed.screen(source.Source), true
	case path == menuIdiom:
		markup = localMarkup("en", ui.Styleguide()[source.Source])
		return "the dropdown idiom", markup, true
	}
	return "", "", false
}

// partial is one gallery state as the seed rendered it: the HTML tab's
// markup, in the container the partial assumes, with the sample
// pictures pointing at Fiesta's assets.
func (seed seedRender) partial(name string, state int) string {
	seed.t.Helper()
	doc, ok := seed.docs[name]
	if !ok || state >= len(doc.States) {
		seed.t.Fatalf("the gallery has no %s state %d", name, state)
	}
	markup, err := renderSample(
		seed.tree, name, state, doc.States[state], "en")
	if err != nil {
		seed.t.Fatalf("%s state %d: %v", name, state, err)
	}
	return pictures(wrap(doc.Wrap, string(markup)))
}

// screen is one sign-in state: the partial with the data the stage
// frame hands it, or the hand-written markup of a screen that has none.
func (seed seedRender) screen(key string) string {
	seed.t.Helper()
	for _, doc := range screenDocs() {
		if doc.Key != key {
			continue
		}
		if doc.Signin == nil {
			return localMarkup("en", doc.Markup)
		}
		var markup strings.Builder
		err := seed.tree.ExecuteTemplate(&markup, "signin", map[string]any{
			"State":   *doc.Signin,
			"Brand":   localData("en", galleryBrand),
			"Preview": true,
		})
		if err != nil {
			seed.t.Fatalf("screen %s: %v", key, err)
		}
		return markup.String()
	}
	seed.t.Fatalf("no screen is called %s", key)
	return ""
}

// fiestaMarkup is an example state's body, read down to the component
// where Fiesta put the component in a row.
func fiestaMarkup(t *testing.T, path string) string {
	t.Helper()
	name, state, _ := strings.Cut(path, "/")
	markup, err := example.Markup(fiesta.FS(), name, state)
	if err != nil {
		t.Fatal(err)
	}
	if attribute, ok := inARow[path]; ok {
		markup, err = example.Element(markup, attribute)
	}
	if path == menuIdiom {
		markup, err = example.Contents(markup, "rst-lbar")
		// The toolbar's own tags sit on lines of their own around the
		// idiom, the way example.Markup trims the body's.
		markup = strings.TrimPrefix(markup, "\n")
		markup = strings.TrimSuffix(markup, "\n")
	}
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return markup
}

// pictures points a sample's picture routes at Fiesta's example
// assets, as the seed did.
func pictures(markup string) string {
	return sampleSrc.ReplaceAllStringFunc(markup, func(match string) string {
		route := match[len(`src="`) : len(match)-1]
		if asset, ok := sampleImages[route]; ok {
			return `src="../assets/` + asset + `"`
		}
		return match
	})
}

// firstDifference is empty when the two are equal, and otherwise the
// first line where they part, with the bytes around where they do.
func firstDifference(ours, theirs string) string {
	if ours == theirs {
		return ""
	}
	oursLines := strings.Split(ours, "\n")
	theirsLines := strings.Split(theirs, "\n")
	for number := range max(len(oursLines), len(theirsLines)) {
		our := lineAt(oursLines, number)
		their := lineAt(theirsLines, number)
		if our == their {
			continue
		}
		at := 0
		for at < len(our) && at < len(their) && our[at] == their[at] {
			at++
		}
		return fmt.Sprintf(
			"line %d, byte %d:\n  partial: %s\n  example: %s",
			number+1, at+1, around(our, at), around(their, at))
	}
	return "the same lines, joined differently"
}

func lineAt(lines []string, number int) string {
	if number < len(lines) {
		return lines[number]
	}
	return "(no line)"
}

// around is up to sixty bytes either side of at.
func around(line string, at int) string {
	start, end := max(at-60, 0), min(at+60, len(line))
	return fmt.Sprintf("%q", line[start:end])
}

var defineName = regexp.MustCompile(`{{-?\s*define\s+"([^"]+)"`)

// partialFiles is every ui/partials file with the templates it defines.
func partialFiles(t *testing.T) map[string][]string {
	t.Helper()
	files, err := fs.Glob(ui.Templates(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, file := range files {
		source, err := fs.ReadFile(ui.Templates(), file)
		if err != nil {
			t.Fatal(err)
		}
		defines := defineName.FindAllStringSubmatch(string(source), -1)
		for _, match := range defines {
			out[file] = append(out[file], match[1])
		}
	}
	if len(out) < 30 {
		t.Fatalf("read %d partial files, too few to be ui's", len(out))
	}
	return out
}

// seededPartials is every partial an example was seeded from. The
// sign-in screens are keyed signin-ask and so on, and all of them
// render the signin partial.
func seededPartials(sources map[string]fiesta.Source) map[string]bool {
	out := map[string]bool{}
	for _, source := range sources {
		switch source.Kind {
		case "partial":
			out[source.Source] = true
		case "screen":
			out["signin"] = true
		}
	}
	return out
}
