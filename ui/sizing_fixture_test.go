package ui

import (
	"sort"
	"strings"
	"testing"
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
<div rst-field><label rst-field-label for="sizing-bare">Bare</label><input type="text" id="sizing-bare" name="sizing_bare"></div>
<div rst-field><label rst-field-label for="sizing-primary">Title</label><input rst-input="primary" type="text" id="sizing-primary" name="sizing_primary"></div>
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
