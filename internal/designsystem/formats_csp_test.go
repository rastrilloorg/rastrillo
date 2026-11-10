package designsystem

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/cspcheck"
)

// The formats page's samples are markup agents copy, so they write
// nothing the default CSP drops.
func TestFormatSamplesWriteNothingTheCSPDrops(t *testing.T) {
	for _, doc := range formatDocs() {
		markup := doc.Markup
		if doc.MarkupFor != nil {
			markup = doc.MarkupFor("en")
		}
		for _, finding := range cspcheck.Inspect(doc.Key+".html",
			[]byte(markup)) {
			t.Errorf("the %s sample, line %d: %s", doc.Key, finding.Line,
				finding.Problem)
		}
	}
}

// A sample's grid card keeps its columns: its preview document carries
// the class rule an app would put in its own stylesheet.
func TestGridSamplePreviewsCarryTheirColumns(t *testing.T) {
	for _, sample := range sampleRules {
		document := previewDoc("", "day", "en", "sample",
			`<div rst-card `+sample.hook+`></div>`)
		if !strings.Contains(document, sample.rule) {
			t.Errorf("no %q in the preview:\n%s", sample.rule, document)
		}
	}
}

// A sample's HTML tab opens with its grid card's rule, so a copy of the
// markup carries what the preview's stylesheet gave it.
func TestGridSampleCodeCarriesItsColumns(t *testing.T) {
	for _, sample := range sampleRules {
		preview, _ := newPreview("", "day", "en", "page", "group", "title",
			"", `<div rst-card `+sample.hook+`></div>`, "id")
		if !strings.Contains(string(preview.Source), "--rst-cols") {
			t.Errorf("the HTML tab for %s has no rule:\n%s", sample.hook,
				preview.Source)
		}
	}
}
