package ui

import (
	"sort"
	"strings"
	"testing"
)

// sizingExtras is what no partial and no sample renders: text controls
// in small parents — a bulk bar and a menu panel, whose 14px (desktop)
// is where 1em is under the floor and the floor has something to do —
// a bare input with no rst- attribute (the zero-weight floor's case),
// and the primary field (the 1em trap's case). The menu that holds a
// field is opened by the drives that measure it; everything else is
// read closed, because a font size is computed whether or not it shows.
const sizingExtras = `<div rst-page data-extra="small-parents">
<div rst-bulkbar id="sizing-bulk"><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-bulk-search" aria-label="Find in the selection"></form><input rst-input type="text" id="sizing-bulk-input" aria-label="Rename the selection"><textarea rst-textarea id="sizing-bulk-note" aria-label="Note on the selection"></textarea></div>
<details rst-dropdown name="rst-sizing-extra" id="sizing-menu"><summary>Find</summary><div rst-dropdown-menu><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-menu-search" aria-label="Find a filter"></form><input rst-input type="text" id="sizing-menu-input" aria-label="Name the filter"><textarea rst-textarea id="sizing-menu-note" aria-label="Note on the filter"></textarea></div></details>
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
