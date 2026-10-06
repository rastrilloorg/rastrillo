package codeview

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

var highlightTag = regexp.MustCompile(`</?ds-[tavxs]>`)

// plain is what a reader sees and copies: the highlighted markup with
// its five elements removed and its entities decoded.
func plain(h string) string { return html.UnescapeString(highlightTag.ReplaceAllString(h, "")) }

func TestHighlightMarksTheFiveKinds(t *testing.T) {
	got := string(Highlight(`{{template "meter" dict "Percent" 140 "Items" .Locales}}`))
	want := `<ds-x>{{</ds-x><ds-x>template</ds-x> <ds-s>"meter"</ds-s> <ds-x>dict</ds-x> <ds-s>"Percent"</ds-s> 140 <ds-s>"Items"</ds-s> <ds-x>.Locales</ds-x><ds-x>}}</ds-x>`
	if got != want {
		t.Errorf("call:\n got %s\nwant %s", got, want)
	}
	got = string(Highlight(`<a rst-btn href="/x">Go</a><input disabled>`))
	want = `<ds-t>&lt;a</ds-t> <ds-a>rst-btn</ds-a> <ds-a>href</ds-a>=<ds-v>"/x"</ds-v>&gt;Go<ds-t>&lt;/a</ds-t>&gt;<ds-t>&lt;input</ds-t> <ds-a>disabled</ds-a>&gt;`
	if got != want {
		t.Errorf("markup:\n got %s\nwant %s", got, want)
	}
}

// The text on screen is the source, whatever is in it: entities, a
// '>' inside a quoted value, {{ inside an attribute, upper case.
func TestHighlightRoundTripsAwkwardMarkup(t *testing.T) {
	for _, src := range []string{
		`<a href="/x?a=1&b=2" data-tip="a > b">Tom &amp; Jerry &euro; < 3</a>`,
		`<DIV Title='it"s'>{{x}}</DIV><!-- a comment <b> -->`,
		`<div title="{{.Name}}">a {{ "}}" }} b</div>`,
		"{{template \"x\" dict\n    \"A\" \"a\\\"b\"\n    \"B\" `raw`}}",
		`a < b > c & d`,
		`<span rst-stat-num>&minus;3%</span>`,
	} {
		h := string(Highlight(src))
		if got := plain(h); got != src {
			t.Errorf("round trip lost bytes:\n src: %s\nplain: %s\n html: %s", src, got, h)
		}
		if strings.Contains(highlightTag.ReplaceAllString(h, ""), "<") {
			t.Errorf("an unescaped '<' reached the page: %s", h)
		}
	}
}
