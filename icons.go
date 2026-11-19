// The rastrillo default icon set: Fiesta's Lucide icons, inlined, never
// a CDN. Each is an SVG string in the Lucide idiom (24x24 viewBox,
// currentColor, stroke, round caps and joins), safe to inline into any
// app's own html/template output via {{icon "slug"}} (register with
// template.FuncMap{"icon": rastrillo.Icon}).
//
// Inline by default, and this file is that default: no build step, no
// second origin a browser fetches from and the app can't vouch for or
// serve when offline. That is why the drawings are compiled in rather
// than behind a <link>.
//
// It is a default, not a guarantee. An app scaffolded with
// --icon-delivery=cdn or =js loads its icons from a second origin
// instead, by the operator's explicit choice; see internal/iconsets. The
// framework recommends inline and supports all three.
//
// The drawings are Fiesta's icons/, from the pinned release, so a
// changed or added icon is a Fiesta release and a pin bump, not an edit
// here. Lucide (https://lucide.dev) is used under the ISC licence, which
// Fiesta ships beside them as icons/LICENSE-lucide.txt.
//
// Emoji policy, inherited from Eleven's docs/icons.md via the console:
// emoji are CONTENT -- a mark a person or a model chose -- and never
// chrome. Interface furniture uses these icons.
//
// Every icon carries aria-hidden="true" because it is meant to sit
// beside its own visible text label -- the label is the accessible
// name, and a second one from the icon would only be read out twice.
// An app using an icon as a control's ONLY label must not use these
// constants bare: give that button or link an explicit aria-label.

package rastrillo

import (
	"html/template"
	"sort"

	"amadan.net/rastrillo/fiesta"
)

// icons maps each slug to Fiesta's markup for it. template.HTML is
// safe here because every string is a file embedded in a pinned
// release, never user input or anything derived from a request.
var icons = fiestaIcons()

func fiestaIcons() map[string]template.HTML {
	out := map[string]template.HTML{}
	for _, slug := range fiesta.IconSlugs() {
		svg, _ := fiesta.Icon(slug)
		out[slug] = template.HTML(svg)
	}
	return out
}

// Icon renders one icon by its slug, for use as an html/template
// FuncMap entry:
//
//	tmpl.Funcs(template.FuncMap{"icon": rastrillo.Icon})
//	// then, in the template: {{icon "check"}}
//
// An unknown slug renders nothing rather than panicking a page
// mid-response -- a typo must cost a missing icon, not a crash.
func Icon(slug string) template.HTML { return icons[slug] }

// IconSlugs lists every slug Icon answers, sorted.
//
// These names are Fiesta's vocabulary and rastrillo's, not any
// vendor's: five of the twelve differ from the names lucide.dev
// publishes. "kebab" is Lucide's ellipsis-vertical, and v1 renamed the
// other four (check-circle, alert-triangle, x-circle, help-circle).
// "menu" is NOT one of them — that is Lucide's own slug, and it is Font
// Awesome that calls the glyph "bars". internal/iconsets and
// docs/site/icons.md say the same five, and internal/iconsets.LucideName
// is where the mapping actually lives. An app scaffolded with a
// different set answers exactly this list too, which is what lets
// {{icon "search"}} mean the same thing everywhere and keeps the shipped
// ui/ partials set-agnostic.
//
// Exported so tooling can check the two stay in step —
// internal/iconsets asserts every scaffoldable set covers all of it.
func IconSlugs() []string {
	out := make([]string, 0, len(icons))
	for slug := range icons {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}
