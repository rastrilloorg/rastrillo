package markup

import (
	"regexp"
	"strings"
	"testing"
)

// TestTheGrammar pins the translation itself: kind, part, variant,
// part-with-variant, tone, and the things that must NOT move — a
// utility class, an app's own class, and the data-* attributes that
// carry runtime state rather than authored vocabulary.
func TestTheGrammar(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// Kinds, parts, variants.
		{`<div class="rst-box">`, `<div rst-box>`},
		{`<div class="rst-callout__body">`, `<div rst-callout-body>`},
		{`<a class="rst-btn rst-btn--primary" href="/x">`, `<a rst-btn="primary" href="/x">`},
		{`<span class="rst-badge rst-badge--positive">`, `<span rst-badge="positive">`},
		{`<div class="rst-dtp__row rst-dtp__row--set">`, `<div rst-dtp-row="set">`},
		{`<div class="rst-lrow rst-lrow--head">`, `<div rst-lrow="head">`},
		// Two kinds on one element become two attributes.
		{`<details class="rst-dropdown rst-locale">`, `<details rst-dropdown rst-locale>`},
		// A kind, a variant and a utility: an attribute AND a class.
		{`<span class="rst-m-hide rst-cell-mut">`, `<span class="rst-m-hide rst-cell-mut">`},
		{`<a class="rst-nm rst-person rst-person--lg">`, `<a class="rst-nm" rst-person="lg">`},
		// Tone.
		{`<span class="rst-status" data-tone="warning">`, `<span rst-status rst-tone="warning">`},
		{`<div class="rst-callout" data-tone="positive">`, `<div rst-callout rst-tone="positive">`},
		// Runtime state stays data-*.
		{`<form class="rst-form" data-busy="false">`, `<form rst-form data-busy="false">`},
		{`<div class="rst-job" data-poll="/x">`, `<div rst-job data-poll="/x">`},
		// Not ours.
		{`<svg class="icon">`, `<svg class="icon">`},
		{`<p class="flash flash-ok">`, `<p class="flash flash-ok">`},
		// The renames.
		{`<div class="rst-form__foot">`, `<div rst-form-foot>`},
		{`<div class="rst-form-foot"><span class="rst-form-foot__note">x</span></div>`,
			`<div rst-form-bar><span rst-form-bar-note>x</span></div>`},
		// The deletion, including the space it took with it.
		{`<summary class="rst-btn rst-dropdown__summary">`, `<summary rst-btn>`},
		{`<summary class="rst-dropdown__summary">Filter</summary>`, `<summary>Filter</summary>`},
		// A Go interpreted string literal keeps its own quoting.
		{`b.WriteString("<a class=\"rst-btn rst-btn--primary\">")`,
			`b.WriteString("<a rst-btn=\"primary\">")`},
		{`b.WriteString("<div class=\"rst-dropdown__menu\">")`,
			`b.WriteString("<div rst-dropdown-menu>")`},
		// Templated class lists: the conditional moves into the value.
		{`<span class="rst-badge{{with .Tone}} rst-badge--{{.}}{{end}}">`,
			`<span rst-badge{{with .Tone}}="{{.}}"{{end}}>`},
		{`<input class="rst-input{{if .Short}} rst-input--short{{end}}">`,
			`<input rst-input{{if .Short}}="short"{{end}}>`},
		{`<button class="rst-btn{{if .Danger}} rst-btn--danger{{else}} rst-btn--primary{{end}}">`,
			`<button rst-btn="{{if .Danger}}danger{{else}}primary{{end}}">`},
		{`<span class="rst-person__av{{if not .Initial}} rst-person__av--empty{{end}}">`,
			`<span rst-person-av{{if not .Initial}}="empty"{{end}}>`},
		// A whole class attribute inside a conditional: the value has no
		// action in it, so it is an ordinary literal list.
		{`<legend{{if .Hidden}} class="rst-sr-only"{{end}}>`, `<legend{{if .Hidden}} class="rst-sr-only"{{end}}>`},
		{`<dd{{if .Mono}} class="rst-mono"{{end}}>`, `<dd{{if .Mono}} class="rst-mono"{{end}}>`},
		// A CSS selector is not markup: neither of these moves.
		{`.rst-box { color: red }`, `.rst-box { color: red }`},
		{`.rst-status[data-tone="positive"] { color: red }`, `.rst-status[data-tone="positive"] { color: red }`},
		// A class attribute that opens a Go literal has no whitespace
		// in front of it, and is still an attribute.
		{"wantContains(t, body, `class=\"rst-field\"`)", "wantContains(t, body, `rst-field`)"},
		{"strings.Contains(got, `class=\"rst-status\" data-tone=\"positive\"`)",
			"strings.Contains(got, `rst-status rst-tone=\"positive\"`)"},
		// data-class= and superclass= are not the class attribute.
		{`<div data-class="rst-box">`, `<div data-class="rst-box">`},
		// A <details name> group is a value, not a class.
		{`<details class="rst-row-menu" name="rst-menus">`, `<details rst-row-menu name="rst-menus">`},
	} {
		got, notes := Rewrite([]byte(c.in))
		if string(got) != c.want {
			t.Errorf("Rewrite(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
		if len(notes) > 0 {
			t.Errorf("Rewrite(%q) reported %v, but the shape is one it handles", c.in, notes)
		}
	}
}

// TestRewritingIsIdempotent is the property the release notes lean on:
// an app can run the tool twice, or run it over a tree half of which
// has already been done, and the second pass changes nothing.
func TestRewritingIsIdempotent(t *testing.T) {
	src := `<div class="rst-card" style="--rst-cols: 2fr 32px">
  <div class="rst-lrow rst-lrow--head"><span class="rst-m-hide">Status</span></div>
  <span class="rst-status" data-tone="positive">Paid</span>
  <span class="rst-badge{{with .Tone}} rst-badge--{{.}}{{end}}">x</span>
  <summary class="rst-btn rst-dropdown__summary">Filter</summary>
  <div class="rst-form-foot"><span class="rst-form-foot__note">n</span></div>
</div>`
	once, _ := Rewrite([]byte(src))
	twice, notes := Rewrite(once)
	if string(once) != string(twice) {
		t.Errorf("a second pass changed the file:\n%s\n---\n%s", once, twice)
	}
	if len(notes) > 0 {
		t.Errorf("a second pass reported %v", notes)
	}
	// The only class= left is the utilities', which is the grammar.
	for _, attr := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(string(once), -1) {
		for _, tok := range strings.Fields(attr[1]) {
			if _, ok := Utilities[tok]; !ok && strings.HasPrefix(tok, "rst-") {
				t.Errorf("a migrating class survived: %q in\n%s", tok, once)
			}
		}
	}
}

// TestAnUnreadableShapeIsReportedNotGuessed. A codemod that guesses is
// worse than one that stops: the wrong guess renders unstyled and looks
// like markup somebody wrote.
func TestAnUnreadableShapeIsReportedNotGuessed(t *testing.T) {
	for _, in := range []string{
		`<div class="rst-box{{if .X}} rst-card{{end}}">`,                            // a second KIND, not a modifier
		`<div class="{{if .X}}rst-btn--primary{{else}}rst-badge--positive{{end}}">`, // two kinds
		`<div class="rst-box{{if .X}} something rst-box--wide{{end}}">`,             // literal text beside it
	} {
		got, notes := Rewrite([]byte(in))
		if string(got) != in {
			t.Errorf("Rewrite(%q) = %q: an unreadable shape must be left exactly as it was", in, got)
		}
		if len(notes) != 1 {
			t.Errorf("Rewrite(%q) reported %d notes, want 1", in, len(notes))
		}
	}
}

// TestSelectorTranslationIsTheSameGrammar. tokens.css's stage-1 twins
// and the markup flip have to mean the same thing by construction, so
// they are one package and this is the selector half of the table.
func TestSelectorTranslationIsTheSameGrammar(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{".rst-box", "[rst-box]"},
		{".rst-box-head + .rst-box", "[rst-box-head] + [rst-box]"},
		{".rst-btn--primary:hover", `[rst-btn~="primary"]:hover`},
		{".rst-callout__body > p", "[rst-callout-body] > p"},
		{".rst-dtp__row--set .rst-dtp__label", `[rst-dtp-row~="set"] [rst-dtp-label]`},
		{".rst-person__av--empty", `[rst-person-av~="empty"]`},
		{`.rst-status[data-tone="positive"]`, `[rst-status][rst-tone~="positive"]`},
		{`.rst-btn[aria-busy="true"]`, `[rst-btn][aria-busy="true"]`},
		{".rst-mono", ".rst-mono"},
		{".rst-row-menu__panel .rst-danger:hover", "[rst-row-menu-panel] .rst-danger:hover"},
		{".rst-search .icon", "[rst-search] .icon"},
		{`.rst-row__lead[data-lead="positive"]`, `[rst-row-lead][data-lead="positive"]`},
		// Selector translation is the pure grammar: the markup renames
		// have already happened in tokens.css itself, so a selector is
		// never renamed under a rewrite's feet.
		{".rst-form-foot", "[rst-form-foot]"},
		{".rst-form-bar .rst-form-bar__note", "[rst-form-bar] [rst-form-bar-note]"},
	} {
		if got := Selector(c.in); got != c.want {
			t.Errorf("Selector(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRespellIsTheGrammarWithoutTheMigration. Rewrite translates markup
// written before the flip, where rst-form-foot meant the sticky save
// bar. Respell translates markup written in today's class vocabulary,
// where it means the action row the form-foot partial emits. Running
// the wrong one over the wrong markup moves an element to a different
// rule and changes what renders, so the difference is pinned here.
func TestRespellIsTheGrammarWithoutTheMigration(t *testing.T) {
	const in = `<div class="rst-form-foot"><span class="rst-form-foot__note">n</span></div>`
	migrated, _ := Rewrite([]byte(in))
	if want := `<div rst-form-bar><span rst-form-bar-note>n</span></div>`; string(migrated) != want {
		t.Errorf("Rewrite(%q) = %q, want %q", in, migrated, want)
	}
	respelled, _ := Respell([]byte(in))
	if want := `<div rst-form-foot><span rst-form-foot-note>n</span></div>`; string(respelled) != want {
		t.Errorf("Respell(%q) = %q, want %q", in, respelled, want)
	}
	// Everything that is not a rename is the same translation in both.
	const plain = `<a class="rst-btn rst-btn--primary">x</a>`
	a, _ := Rewrite([]byte(plain))
	b, _ := Respell([]byte(plain))
	if string(a) != string(b) {
		t.Errorf("Rewrite and Respell disagree on markup with no rename in it: %q vs %q", a, b)
	}
}

// TestTheExemptionsAreDisjoint. A class cannot be both a utility that
// keeps its spelling and a name the flip renames or deletes; the three
// lists are the whole of what the grammar treats specially, so an
// overlap would make the translation depend on lookup order.
func TestTheExemptionsAreDisjoint(t *testing.T) {
	for class := range Utilities {
		if _, ok := Renamed[class]; ok {
			t.Errorf("%q is both a utility and a rename", class)
		}
		if _, ok := Dropped[class]; ok {
			t.Errorf("%q is both a utility and a deletion", class)
		}
	}
	for class := range Renamed {
		if _, ok := Dropped[class]; ok {
			t.Errorf("%q is both a rename and a deletion", class)
		}
	}
}
