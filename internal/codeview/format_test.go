package codeview

import (
	"strings"
	"testing"
	"unicode"
)

// squash is markup with every whitespace character taken out. Format
// may add, move and drop whitespace and must touch nothing else, so the
// two sides of a formatting agree once squashed. Whether the whitespace
// it moved could show is the browser's question, not this one's:
// TestTheHTMLTabRendersExactlyLikeThePreview in internal/designsystem
// renders every gallery sample both ways and compares every box.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

var formatCases = []string{
	`<section rst-box><form rst-form method="post" action="#"><div rst-field><label rst-field-label for="t">Title</label><input rst-input id="t" name="t"></div></form></section>`,
	`<div rst-list><div rst-lrow><a class="rst-nm" href="/p/1">Release notes<small>2 August</small></a> <span rst-pill>Draft</span></div></div>`,
	`<span rst-meter><meter rst-meter-bar value="100" min="0" max="100" aria-hidden="true"></meter><span rst-meter-num>700/500</span></span>`,
	"<ul>\n  <li>One</li>\n\n<li>Two</li></ul><p>After</p>",
	`<details rst-dropdown name="rst-menus"><summary>Filter<span rst-caret aria-hidden="true"><svg class="icon" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg></span></summary><div rst-dropdown-menu><a href="/a">A</a></div></details>`,
	`<div rst-form-foot><button rst-btn="primary lg" type="submit" aria-busy="true" disabled><span rst-spin aria-hidden="true"></span>Publish</button><a rst-btn="lg" href="/posts">Back</a></div>`,
	`<form rst-form-actions method="post"><input type="hidden" name="csrf" value="t"><input type="hidden" name="id" value="1"><a rst-btn href="/p">Cancel</a><button rst-btn="danger">Delete</button></form>`,
}

func TestFormatChangesOnlyWhitespaceAndIsIdempotent(t *testing.T) {
	for _, in := range formatCases {
		out := Format(in)
		if squash(out) != squash(in) {
			t.Errorf("Format changed more than whitespace:\n in: %s\nout: %s", in, out)
		}
		if Format(out) != out {
			t.Errorf("Format is not idempotent on:\n%s\nsecond pass:\n%s", out, Format(out))
		}
	}
}

// The space between two inline elements, and between text and an
// inline element, is rendered wherever the container is not one the
// formatter knows lays its children out as flex or grid, so Format
// keeps it exactly. An unknown container stays inline.
func TestFormatKeepsTheSpaceBetweenInlineElements(t *testing.T) {
	for in, want := range map[string]string{
		`<div><a href="/a">A</a> <span>B</span></div>`:                                                     "<div>\n  <a href=\"/a\">A</a> <span>B</span>\n</div>",
		`<p>Hello <b>world</b> <i>again</i></p>`:                                                           "<p>Hello <b>world</b> <i>again</i></p>",
		`<span rst-a>x</span>  <span rst-b>y</span>`:                                                       `<span rst-a>x</span>  <span rst-b>y</span>`,
		"<li><a href=\"/x\">X</a>\n<a href=\"/y\">Y</a></li>":                                              "<li>\n  <a href=\"/x\">X</a>\n<a href=\"/y\">Y</a>\n</li>",
		`<p>Total <output name="t">$1</output> due</p>`:                                                    `<p>Total <output name="t">$1</output> due</p>`,
		`<label rst-field-label for="a">Name <span rst-field-required aria-hidden="true">*</span></label>`: `<label rst-field-label for="a">Name <span rst-field-required aria-hidden="true">*</span></label>`,
		`<div rst-unknown><span>a</span><span>b</span></div>`:                                              "<div rst-unknown>\n  <span>a</span><span>b</span>\n</div>",
	} {
		if got := Format(in); got != want {
			t.Errorf("Format(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestFormatBreaksAtBlockTagsAndIndents(t *testing.T) {
	got := Format(`<section rst-box><form><div rst-field><label>Title</label><input name="t"></div></form></section>`)
	want := "<section rst-box>\n  <form>\n    <div rst-field>\n      <label>Title</label><input name=\"t\">\n    </div>\n  </form>\n</section>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Inside a flex or grid container whitespace between children renders
// as nothing, so every child gets a line: the busy button's spinner and
// its label, and the two buttons beside each other. A button holding
// only its label keeps it inline.
func TestFormatPutsEachChildOfAFlexContainerOnALine(t *testing.T) {
	got := Format(formatCases[5])
	want := "<div rst-form-foot>\n" +
		"  <button rst-btn=\"primary lg\" type=\"submit\" aria-busy=\"true\" disabled>\n" +
		"    <span rst-spin aria-hidden=\"true\"></span>\n" +
		"    Publish\n" +
		"  </button>\n" +
		"  <a rst-btn=\"lg\" href=\"/posts\">Back</a>\n" +
		"</div>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got = Format(formatCases[2])
	want = "<span rst-meter>\n  <meter rst-meter-bar value=\"100\" min=\"0\" max=\"100\" aria-hidden=\"true\"></meter>\n  <span rst-meter-num>700/500</span>\n</span>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A hidden input renders nothing, so the space beside it shows only if
// something visible shares its line. At the head of a form, before the
// first thing drawn, each one gets a line.
func TestFormatPutsSilentElementsOnLinesOfTheirOwn(t *testing.T) {
	got := Format(formatCases[6])
	want := "<form rst-form-actions method=\"post\">\n" +
		"  <input type=\"hidden\" name=\"csrf\" value=\"t\">\n" +
		"  <input type=\"hidden\" name=\"id\" value=\"1\">\n" +
		"  <a rst-btn href=\"/p\">Cancel</a>\n" +
		"  <button rst-btn=\"danger\">Delete</button>\n" +
		"</form>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// In a sentence, a hidden input between two words keeps the line it
	// is on: a break there would be a space between them.
	in := `<p>one<input type="hidden" name="a">two</p>`
	if got := Format(in); got != in {
		t.Errorf("a hidden input inside running text was broken out:\n%s", got)
	}
}

// An element with nothing in it stays with nothing in it: :empty is a
// selector tokens.css uses, and a line break would stop it matching.
func TestFormatLeavesAnEmptyElementEmpty(t *testing.T) {
	for _, in := range []string{
		`<div rst-form-foot><span rst-spin aria-hidden="true"></span></div>`,
		`<footer rst-shell-foot></footer>`,
		`<div rst-lrow="head"><span>Order</span><span></span></div>`,
	} {
		out := Format(in)
		for _, empty := range []string{`<span rst-spin aria-hidden="true"></span>`, `<footer rst-shell-foot></footer>`, `<span></span>`} {
			if strings.Contains(in, empty) && !strings.Contains(out, empty) {
				t.Errorf("an empty element got content:\n%s", out)
			}
		}
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

// A select sits in its line like a word: the space after it shows, so
// the formatter may lay out its option list, where nothing between two
// options renders, but must not break around the select itself.
func TestFormatKeepsASelectInItsLine(t *testing.T) {
	in := `<p>Pick <select><option>A</option></select><span>B</span></p>`
	if got := Format(in); !strings.Contains(got, "Pick <select>") || !strings.Contains(got, "</select><span>B</span>") {
		t.Errorf("a select in running text was broken out:\n%s", got)
	}
	got := Format(`<div rst-field><select name="s"><optgroup label="E"><option>A</option><option>B</option></optgroup></select></div>`)
	want := "<div rst-field>\n  <select name=\"s\">\n    <optgroup label=\"E\">\n      <option>A</option>\n      <option>B</option>\n    </optgroup>\n  </select>\n</div>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Under white-space: pre and its kin a line break is a line break and
// an indent is spaces, whatever the container. An element that says so
// in its style attribute is left byte for byte, its children included,
// as a <pre> is.
func TestFormatLeavesWhitespacePreservingStylesAlone(t *testing.T) {
	for _, style := range []string{"white-space:pre", "white-space: pre-wrap", "white-space:pre-line", "white-space: break-spaces", "white-space-collapse: preserve"} {
		button := `<button rst-btn style="` + style + `">Pay <em>now</em></button>`
		got := Format(`<div rst-form-foot>` + button + `<a rst-btn href="/">Back</a></div>`)
		if !strings.Contains(got, button) {
			t.Errorf("%s: the button's content was reformatted:\n%s", style, got)
		}
	}
	// nowrap collapses whitespace like normal, so it changes nothing.
	got := Format(`<div rst-form-foot><button rst-btn style="white-space: nowrap"><span rst-spin></span>Pay</button></div>`)
	if !strings.Contains(got, "\n    Pay\n") {
		t.Errorf("white-space: nowrap stopped the formatter:\n%s", got)
	}
}

// HTML allows whitespace either side of an attribute's =, and the
// formatter's reading of a tag has to see the attribute the browser
// sees: style = "white-space:pre" preserves whitespace exactly as
// style="white-space:pre" does.
func TestFormatReadsAnAttributeWithSpacesAroundItsEquals(t *testing.T) {
	button := "<button rst-btn style = \"white-space:pre\">Pay <em>now</em></button>"
	got := Format(`<div rst-form-foot>` + button + `<a rst-btn href="/">Back</a></div>`)
	if !strings.Contains(got, button) {
		t.Errorf("a style written with spaces around = was not seen:\n%s", got)
	}
	attrs := attrsOf("<input type =\n'hidden' name\t=  n value=v checked>")
	for name, want := range map[string]string{"type": "hidden", "name": "n", "value": "v", "checked": ""} {
		if got, ok := attrs[name]; !ok || got != want {
			t.Errorf("attrsOf: %s = %q (present %v), want %q", name, got, ok, want)
		}
	}
	if len(attrs) != 4 {
		t.Errorf("attrsOf read %d attributes, want 4: %v", len(attrs), attrs)
	}
}
