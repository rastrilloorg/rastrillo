package ui

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// sizingExtras is what no partial and no sample renders: text controls
// in a parent that is really small, a bare input with no rst- attribute
// (the zero-weight floor's case), and the primary field (the 1em trap's
// case). A bulk bar and a menu panel set no font-size of their own, so
// on touch they are 16px and `font: inherit` alone would pass every
// floor test with the floors deleted. The .sizing-small wrapper is an
// app's own small parent, xs (13px on touch): there 1em is under the
// floor and each component floor is the only thing lifting its field.
// The menu that holds a field is opened by the drives that measure it;
// everything else is read closed, because a font size is computed
// whether or not it shows.
const sizingExtras = `<style>.sizing-small { font-size: var(--rst-fs-xs); }</style><div rst-page data-extra="small-parents">
<div class="sizing-small">
<div rst-bulkbar id="sizing-bulk"><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-bulk-search" aria-label="Find in the selection"></form><input rst-input type="text" id="sizing-bulk-input" aria-label="Rename the selection"><textarea rst-textarea id="sizing-bulk-note" aria-label="Note on the selection"></textarea></div>
<details rst-dropdown name="rst-sizing-extra" id="sizing-menu"><summary>Find</summary><div rst-dropdown-menu><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-menu-search" aria-label="Find a filter"></form><input rst-input type="text" id="sizing-menu-input" aria-label="Name the filter"><textarea rst-textarea id="sizing-menu-note" aria-label="Note on the filter"></textarea></div></details>
</div>
<div rst-field><label rst-field-label for="sizing-bare">Bare</label><input type="text" id="sizing-bare" name="sizing_bare" data-sizing-not-an-idiom></div>
<div rst-field><label rst-field-label for="sizing-primary">Title</label><input rst-input="primary" type="text" id="sizing-primary" name="sizing_primary"></div>
<p data-extra="buttons"><button rst-btn="sm" type="button" id="sizing-btn-sm">Small</button> <button rst-btn type="button" id="sizing-btn">Default</button> <button rst-btn="lg" type="button" id="sizing-btn-lg">Large</button></p>
<div rst-shell-sidebar data-extra="legacy-drawer"><details rst-shell-chrome><summary>Menu</summary></details><aside rst-shell-rail><nav rst-shell-nav><a href="#legacy-one">Legacy one</a><a href="#legacy-two">Legacy two</a></nav></aside><main rst-shell-main><p>The pre-H sidebar, which tokens.css keeps working for old layouts.</p></main></div>
</div>`

// sizingFixture is one page body with every partial (each with its own
// test data, the set TestRenderEverythingSmoke renders), every
// styleguide sample except the modal, and the extras above. The modal
// is its own page: its overlay is fixed over the whole viewport and
// would occlude every hit test on this one (spec §10.1: overlays are
// measured in their own states, one at a time).
func sizingFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(sizingExtras)
	// An enhanced select: select.js arms a field past ten options, and
	// the combobox options it draws are a row of §1.4's table.
	var options []any
	for i := 1; i <= 12; i++ {
		options = append(options, map[string]any{"Value": fmt.Sprint(i), "Label": fmt.Sprintf("Option %d", i)})
	}
	b.WriteString(`<div rst-page data-extra="combobox">` + render(t, "field-select", map[string]any{
		"ID": "sizing-combo", "Name": "sizing_combo", "Label": "Country", "Options": options,
	}) + `</div>`)
	// The language menu: allPartials has no locale-menu fixture and no
	// sample carries one, and its buttons only render with Items.
	b.WriteString(`<div rst-page data-extra="locale">` + render(t, "locale-menu", map[string]any{
		"Return": "/orders",
		"Items": []rastrillo.LocaleItem{
			{Code: "en", Name: "English", Href: "/en/orders", Current: true},
			{Code: "ga", Name: "Gaeilge", Href: "/ga/orders"},
			{Code: "ja", Name: "日本語", Href: "/ja/orders"},
		},
	}) + `</div>`)
	// Controls narrower than a tap by their content or their container,
	// which only a min-inline-size lifts: an escalate link whose label is
	// one short word, and a person link in a column narrower than a tap
	// (its base rule's min-width: 0 lets the track shrink it).
	b.WriteString(`<style>.sizing-narrow { display: grid; grid-template-columns: 36px; }</style><div rst-page data-extra="short-labels">` +
		render(t, "bulk-bar", map[string]any{
			"DoneHref": "/orders", "DoneLabel": "Done selecting", "Count": "3 selected",
			"EscalateHref": "/orders?select=all", "EscalateLabel": "All", "MenuLabel": "Actions", "MenuGroup": "rst-sizing-short",
		}) +
		`<div class="sizing-narrow">` + render(t, "person", map[string]any{"Href": "/people/al", "Name": "Al", "Initial": "A"}) + `</div></div>`)
	samples := Styleguide()
	names := make([]string, 0, len(samples))
	for name := range samples {
		if name != "modal" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString(`<div rst-page data-sample="` + name + `">` + samples[name] + `</div>`)
	}
	for _, p := range allPartials() {
		b.WriteString(`<div rst-page data-partial="` + p.Name + `">` + render(t, p.Name, p.Data) + `</div>`)
	}
	return b.String()
}
