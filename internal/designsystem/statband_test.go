package designsystem

import (
	"fmt"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/ui"
)

// statBandState is the stat state that is the band: the one sized by
// its own row in the height tables.
func statBandState(t *testing.T) (int, sample) {
	t.Helper()
	for at, state := range statStates() {
		if state.Frame == "stat-band" {
			return at, state
		}
	}
	t.Fatal("statStates has no stat-band state")
	return 0, sample{}
}

// The stat band stayed when the UI primitives page left, because stat
// draws it: on Display, as four stat calls. Its frame is a file of the
// tree holding the four cells inside rst-stats, its HTML tab is exactly
// those cells, and its Template tab is the four calls, in every theme
// and locale.
func TestTheStatBandIsFourStatCallsOnDisplay(t *testing.T) {
	files := render(t)
	at, state := statBandState(t)
	if len(state.Calls) != 4 {
		t.Fatalf("the band makes %d stat calls, want 4", len(state.Calls))
	}
	group := fmt.Sprintf("%s-%d", anchorID("partial", "stat"), at)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			where := theme + "/" + locale
			page := galleryPage(t, files, theme, locale, "display")
			var widget string
			for _, each := range widgetsOf(page) {
				if strings.Contains(each, `name="`+group+`"`) {
					widget = each
				}
			}
			if widget == "" {
				t.Errorf("%s: Display has no widget %s", where, group)
				continue
			}
			frames := frameDocs(t, files, widget)
			if len(frames) != 1 {
				t.Errorf("%s: the band has %d frames, want 1",
					where, len(frames))
				continue
			}
			tmpl := sampleTree(t, locale)
			cells, err := renderSample(tmpl, "stat", at, state, locale)
			if err != nil {
				t.Fatal(err)
			}
			body := previewBody(frames[0].Doc)
			if want := wrap(wrapStats, string(cells)); body != want {
				t.Errorf("%s: the band's frame shows\n%s\nwant\n%s",
					where, body, want)
			}
			if got := strings.Count(body, "<div rst-stat"); got != 5 {
				t.Errorf("%s: %d rst-stat elements in the frame, want "+
					"the band and its four cells", where, got)
			}
			html := sourceTexts(panelOf(widget, "h"))
			want := codeview.Format(withColumns(string(cells)))
			if len(html) != 1 || html[0] != want {
				t.Errorf("%s: the HTML tab is %q, want the frame's cells %q",
					where, html, want)
			}
			calls := strings.Join(sourceTexts(panelOf(widget, "t")), "\n")
			if found := strings.Count(calls, `{{template "stat"`); found != 4 {
				t.Errorf("%s: the Template tab has %d stat calls, want 4:\n%s",
					where, found, calls)
			}
		}
	}
}
