package designsystem

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// cardDocs are the documents whose shell has an account card: every
// demo of a shell with an index, and the demo application.
var cardDocs = []string{
	"shells/topbar.html", "shells/topbar-menu.html",
	"shells/sidebar.html", "shells/sidebar-page.html",
	"shells/console.html", "shells/console-page.html",
	"demo.html", "demo-dashboard.html", "demo-requests.html",
	"demo-request.html",
}

// Every demo's account card holds what the contract says a card
// holds, in its order, less the Appearance row: the person, the
// Language row (button, fallback, sheet) and the account's links, which
// the shell demos fill and the demo application, whose account block is
// its own, leaves empty. The tree is static, with no /_theme for a
// scheme form to post to, so the demos carry no scheme control
// (TestTheStaticDemosCarryNoSchemeMenu) and the Shells page does not
// promise one.
func TestEveryDemoCardHoldsLanguage(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			tr := translator(locale)
			language := `<div rst-menu-row><span rst-menu-row-label>` +
				tr("rastrillo.ui.shell_language") + `</span>` +
				`<button type="button" popovertarget="rst-language">`
			for _, doc := range cardDocs {
				name := theme + "/" + locale + "/" + doc
				page := string(files[name])
				_, card, ok := strings.Cut(page, `<div rst-shell-who>`)
				if !ok {
					t.Errorf("%s: no account card", name)
					continue
				}
				card, _, _ = strings.Cut(card, "</details>\n")
				at := []int{strings.Index(card, language),
					strings.Index(card, `<a href="#">`)}
				if !strings.HasPrefix(doc, "shells/") {
					at[1] = len(card)
				}
				if at[0] < 0 || at[1] < at[0] {
					t.Errorf("%s: the card's Language row and account "+
						"links are at %v; want both, in that order",
						name, at)
				}
			}
		}
	}
}

// The Shells page's words for the shells with an account card say
// what their demos show: appearance comes up only as what a served
// app's card adds.
func TestTheShellBlurbsPromiseNoAppearanceRow(t *testing.T) {
	for shell, blurb := range shellBlurbs {
		for _, sentence := range strings.SplitAfter(blurb, ". ") {
			if strings.Contains(strings.ToLower(sentence), "appearance") &&
				!strings.HasPrefix(sentence, "In a served app,") {
				t.Errorf("%s: %q promises appearance, which the static "+
					"demo does not show", shell, sentence)
			}
		}
	}
}
