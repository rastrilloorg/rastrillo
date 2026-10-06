package codeview

import (
	"regexp"
	"strings"
	"testing"
)

// normalise removes what Format may add: whitespace-only text between
// two tags where either is block-level, outside <pre>, <textarea> and
// <svg>. It is written here, apart from Format's own walk, so a walk
// that dropped some other whitespace could not drop it from both sides
// of the comparison at once.
var (
	normaliseTag   = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)[^>]*>`)
	normaliseBlock = map[string]bool{
		"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
		"dialog": true, "dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
		"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
		"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
		"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
		"search": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
		"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
	}
)

func normalise(s string) string {
	var b strings.Builder
	at, prev, raw := 0, "", ""
	for _, m := range normaliseTag.FindAllStringSubmatchIndex(s, -1) {
		name, closing, gap := strings.ToLower(s[m[4]:m[5]]), m[3] > m[2], s[at:m[0]]
		if raw != "" || prev == "" || strings.TrimSpace(gap) != "" || !(normaliseBlock[prev] || normaliseBlock[name]) {
			b.WriteString(gap)
		}
		b.WriteString(s[m[0]:m[1]])
		switch {
		case raw != "" && closing && name == raw:
			raw = ""
		case raw == "" && !closing && (name == "pre" || name == "textarea" || name == "svg"):
			raw = name
		}
		prev, at = name, m[1]
	}
	return b.String() + s[at:]
}

var formatCases = []string{
	`<section rst-box><form rst-form method="post" action="#"><div rst-field><label rst-field-label for="t">Title</label><input rst-input id="t" name="t"></div></form></section>`,
	`<div rst-list><div rst-lrow><a class="rst-nm" href="/p/1">Release notes<small>2 August</small></a> <span rst-pill>Draft</span></div></div>`,
	`<span rst-meter><meter rst-meter-bar value="100" min="0" max="100" aria-hidden="true"></meter><span rst-meter-num>700/500</span></span>`,
	"<ul>\n  <li>One</li>\n\n<li>Two</li></ul><p>After</p>",
	`<details rst-dropdown name="rst-menus"><summary>Filter<span rst-caret aria-hidden="true"><svg class="icon" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg></span></summary><div rst-dropdown-menu><a href="/a">A</a></div></details>`,
}

func TestFormatMovesOnlyWhitespaceBetweenBlockTags(t *testing.T) {
	for _, in := range formatCases {
		out := Format(in)
		if normalise(out) != normalise(in) {
			t.Errorf("Format changed more than whitespace between block tags:\n in: %s\nout: %s", in, out)
		}
		if Format(out) != out {
			t.Errorf("Format is not idempotent on:\n%s", out)
		}
	}
	// The comparison's own control: a formatter that dropped the space
	// between two inline elements, a space a reader sees, must fail it.
	in := formatCases[1]
	broken := strings.Replace(Format(in), "</a> <span", "</a><span", 1)
	if broken == Format(in) {
		t.Fatal("the control fixture has no space between </a> and <span> to drop")
	}
	if normalise(broken) == normalise(in) {
		t.Error("normalise cannot see a dropped space between two inline elements; this test would pass a formatter that loses it")
	}
}

// The space between two inline elements, and between text and an
// inline element, is rendered, so Format keeps it exactly.
func TestFormatKeepsTheSpaceBetweenInlineElements(t *testing.T) {
	for in, want := range map[string]string{
		`<div><a href="/a">A</a> <span>B</span></div>`:        "<div>\n  <a href=\"/a\">A</a> <span>B</span>\n</div>",
		`<p>Hello <b>world</b> <i>again</i></p>`:              "<p>Hello <b>world</b> <i>again</i>\n</p>",
		`<span rst-a>x</span>  <span rst-b>y</span>`:          `<span rst-a>x</span>  <span rst-b>y</span>`,
		"<li><a href=\"/x\">X</a>\n<a href=\"/y\">Y</a></li>": "<li>\n  <a href=\"/x\">X</a>\n<a href=\"/y\">Y</a>\n</li>",
	} {
		if got := Format(in); got != want {
			t.Errorf("Format(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestFormatBreaksAtBlockTagsAndIndents(t *testing.T) {
	got := Format(`<section rst-box><form rst-form><div rst-field><label>Title</label><input name="t"></div></form></section>`)
	want := "<section rst-box>\n  <form rst-form>\n    <div rst-field>\n      <label>Title</label><input name=\"t\">\n    </div>\n  </form>\n</section>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// An inline run stays one line however long: breaking inside it
	// would change the space a reader sees between two words.
	meter := formatCases[2]
	if got := Format(meter); got != meter {
		t.Errorf("an inline run was broken:\n%s", got)
	}
}

// A <pre> or <textarea> whose content looks like tags,
// an svg, and an attribute value holding a '>' all pass through byte
// for byte.
func TestFormatLeavesRawZonesAndQuotedBrackets(t *testing.T) {
	for _, raw := range []string{
		"<pre>  <div>\n not a tag </div>\n</pre>",
		"<textarea name=\"n\"><p>typed</p>\n  </textarea>",
		`<svg viewBox="0 0 2 2"><g><path d="M0 0"/></g></svg>`,
	} {
		in := "<div>" + raw + "</div>"
		out := Format(in)
		if !strings.Contains(out, raw) {
			t.Errorf("a raw zone changed:\n in: %s\nout: %s", in, out)
		}
	}
	in := `<div data-tip="a > b"><p title='x <div> y'>t</p></div>`
	out := Format(in)
	for _, v := range []string{`data-tip="a > b"`, `title='x <div> y'`} {
		if !strings.Contains(out, v) {
			t.Errorf("an attribute value changed: %s", out)
		}
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("a '>' inside a quoted value was taken for the end of a tag:\n%s", out)
	}
}
