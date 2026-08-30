// The rastrillo default icon set — vendored, never a CDN. Mirrors
// carlosframework/platform's internal/console/icons.go exactly: static,
// in-code SVG strings in the Lucide idiom (24x24 viewBox, currentColor,
// stroke, round caps and joins), safe to inline into any app's own
// html/template output via {{icon "slug"}} (register with
// template.FuncMap{"icon": rastrillo.Icon}).
//
// Vendored by default, and this file is that default: no build step, no
// second origin a browser fetches from and the app can't vouch for or
// serve when offline. That is why the paths live here rather than
// behind a <link>.
//
// It is a default, not a guarantee. An app scaffolded with
// --icon-delivery=cdn or =js loads its icons from a second origin
// instead, by the operator's explicit choice; see internal/iconsets. The
// framework recommends inline and supports all three.
//
// Icons are vendored from Lucide -- https://lucide.dev -- used under the
// ISC licence:
//
//	Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as
//	part of Feather (MIT). All other copyright (c) for Lucide are held by
//	Lucide Contributors 2022.
//
//	Permission to use, copy, modify, and/or distribute this software for any
//	purpose with or without fee is hereby granted, provided that the above
//	copyright notice and this permission notice appear in all copies. THE
//	SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
//	REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
//	MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
//	ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
//	WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
//	ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
//	OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// Emoji policy, inherited from Eleven's docs/icons.md via the console:
// emoji are CONTENT -- a mark a person or a model chose -- and never
// chrome. Interface furniture uses these icons. Adding one = paste the
// Lucide SVG's paths in below, keep the (Lucide: <slug>) comment,
// register it in the icons map, use it as {{icon "<slug>"}}.
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
)

// template.HTML is used HERE AND ONLY HERE in this package, for the same
// reason it's safe in the console: every string in this file is a
// compile-time constant of our own markup, never user input, a bucket
// object, or anything derived from a request.
const (
	// iconOpen is the shared opening tag: no width or height, so the icon
	// sizes from the consuming app's own CSS (e.g. a .icon { } rule) and
	// tracks the text beside it.
	iconOpen = `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" ` +
		`stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">`

	// A disclosure caret -- dropdowns, expandable sections (Lucide: chevron-down).
	iconChevronDown template.HTML = iconOpen + `<path d="m6 9 6 6 6-6"/></svg>`

	// Confirmation, success states (Lucide: check).
	iconCheck template.HTML = iconOpen + `<path d="M20 6 9 17l-5-5"/></svg>`

	// "New" / "create" actions (Lucide: plus).
	iconPlus template.HTML = iconOpen + `<path d="M5 12h14"/><path d="M12 5v14"/></svg>`

	// Search fields and filter bars (Lucide: search).
	iconSearch template.HTML = iconOpen + `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>`

	// Overflow menus, row-level "more actions" (Lucide: kebab, three
	// stacked dots -- Lucide's own slug for this glyph is "more-vertical",
	// renamed here to the common UI term).
	iconKebab template.HTML = iconOpen + `<circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/></svg>`

	// Dismiss, close, remove (Lucide: x).
	iconX template.HTML = iconOpen + `<path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`

	// The neutral callout tone (Lucide: info).
	iconInfo template.HTML = iconOpen + `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`

	// The positive callout tone (Lucide: check-circle).
	iconCheckCircle template.HTML = iconOpen + `<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/></svg>`

	// The warning callout tone (Lucide: alert-triangle).
	iconAlertTriangle template.HTML = iconOpen + `<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`

	// The negative callout tone (Lucide: x-circle).
	iconXCircle template.HTML = iconOpen + `<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/></svg>`

	// Help text, tooltips (Lucide: help-circle).
	iconHelpCircle template.HTML = iconOpen + `<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>`

	// Navigation behind a disclosure -- the shells' collapsed rail and
	// collapsed topbar (Lucide: menu, the three-line hamburger).
	//
	// NOT kebab. Kebab means "more actions on this row" everywhere else
	// in this vocabulary, and spending it on navigation would blur a
	// distinction the set currently keeps; the set had no hamburger
	// until the two shells needed one, so this is a twelfth icon rather
	// than a second job for the eleventh.
	iconMenu template.HTML = iconOpen + `<path d="M4 12h16"/><path d="M4 6h16"/><path d="M4 18h16"/></svg>`
)

// icons maps each Lucide slug to its vendored markup. Slugs are the
// Lucide names so the mapping back to lucide.dev stays obvious.
var icons = map[string]template.HTML{
	"chevron-down":   iconChevronDown,
	"check":          iconCheck,
	"plus":           iconPlus,
	"search":         iconSearch,
	"kebab":          iconKebab,
	"x":              iconX,
	"info":           iconInfo,
	"check-circle":   iconCheckCircle,
	"alert-triangle": iconAlertTriangle,
	"x-circle":       iconXCircle,
	"help-circle":    iconHelpCircle,
	"menu":           iconMenu,
}

// Icon renders one vendored icon by its Lucide slug for use as an
// html/template FuncMap entry:
//
//	tmpl.Funcs(template.FuncMap{"icon": rastrillo.Icon})
//	// then, in the template: {{icon "check"}}
//
// An unknown slug renders nothing rather than panicking a page
// mid-response -- a typo must cost a missing icon, not a crash.
func Icon(slug string) template.HTML { return icons[slug] }

// IconSlugs lists every slug Icon answers, sorted.
//
// These names are rastrillo's own vocabulary, not any vendor's: most
// match Lucide, but "kebab" is Lucide's ellipsis-vertical and Lucide v1
// renamed several others. An app scaffolded with a different set answers
// exactly this list too, which is what lets {{icon "search"}} mean the
// same thing everywhere and keeps the shipped ui/ partials set-agnostic.
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
