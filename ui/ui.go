// Package ui is rastrillo's server-shape component library: a small
// starter set of List-screen html/template partials, a design-token
// stylesheet, and the template helpers they need — vendored the same way
// icons.go vendors Lucide, so an app pulls in a working component with an
// import and a ParseFS call, not a hand-copy.
//
// It is a component library, not a screen generator. Nothing here
// generates a screen, decides a route, or owns rendering. An app builds
// its own template tree and calls these partials by name:
//
//	tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
//	        ParseFS(ui.Templates(), "*.html"))
//
// An app that scaffolded its own icon set points both icon seams at it:
//
//	tmpl := template.Must(template.New("").
//	        Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
//	        ParseFS(ui.Templates(), "*.html"))
//
// {{iconAssets}} goes in the layout's <head>. It renders empty for the
// vendored-inline default, so it is safe to call unconditionally.
//
//	tmpl = template.Must(tmpl.ParseFS(appTemplateFS, "templates/*.html"))
//
// The partial set spans list screens plus the display, form and route
// families — see ui_test.go's TestAllPartialsAreDefined for the current,
// authoritative name list and count; this comment intentionally does not
// repeat a number that would go stale the next time one lands. Each
// partial takes exactly one data value; build it inline with dict (see
// Funcs). Each partial's own file carries its data contract in a
// template comment above the {{define}}.
//
// Three container elements the partials assume but do not emit, because
// they belong to the app's own page markup:
//
//	<div class="rst-page">   — the centred content column every screen sits in
//	<div class="rst-list">   — the card wrapping a list-bar and a run of rows
//	<form class="rst-form">  — the column a run of fields and a form-foot sit in
//
// rst-list and rst-card have no padding by design: they hold rows, and
// each row pads itself. Anything that is not a row — a form, prose, a
// strip of links — goes in rst-box, the padded section card, never
// straight into a list card (the tell is text flush against the border).
// rst-form draws nothing on its own; it sits inside a rst-box or on the
// bare page. Screens stack vertically: page-header, then section-header +
// card, repeated. Do not put a heading, a paragraph and a button side by
// side in a flex row; a notice with a call to action is a callout whose
// body ends in a link, or rst-box-head (h2 + one compact button) over a
// rst-box. The horizontal idioms are the ones tokens.css ships:
// rst-box-head, rst-field-row, rst-lbar, rst-lrow cells, rst-seg-tabs.
//
// Styling comes from two stylesheets rastrillo new writes once into a
// new app's static/ directory: tokens.css, which is structure — layout,
// spacing, radius, the type scale and the component classes — and a
// theme (see ThemeCSS), which is the colour and the type family those
// classes paint themselves with. rastrillo.Serve never serves either:
// from the moment they are scaffolded they are ordinary app-owned
// static files the app is free to edit in place. rastrillo.js, the fragment shim behind
// data-poll and data-busy, ships the same way, landing beside it. It
// never replaces a native idiom — every "no JavaScript" idiom above
// still works with scripts disabled; the shim exists only for the one
// kind of work a native idiom cannot do, work that finishes after the
// response has already been sent, such as a background job's progress.
//
// Errors follow ordinary html/template semantics (nothing is
// special-cased). With dict-built map data a key the caller forgot to set
// does not fail at Execute; the partials guard every optional field, so a
// missing key renders nothing. A caller who wants missing-field detection
// gets it by passing a Go struct instead of a dict-built map.
//
// # Class idioms
//
// Not every reusable shape can be a partial: a section box or a grid row
// wraps a caller-chosen, arbitrary body, and a Go template partial can
// only wrap data it already knows the shape of. For these, tokens.css
// ships the class vocabulary and this doc shows the markup; the app
// writes the HTML itself. The canonical, exercised versions of every
// sample below live in ui_test.go's styleguideSamples, rendered by
// TestStyleguideSamplesRender — copy from there, not from here, if the
// two ever disagree.
//
// box — the section card. Its heading is a sibling before the card, not
// inside it:
//
//	<div class="rst-box-head"><h2>Payout</h2><a class="rst-btn" href="/payout/edit">Edit</a></div>
//	<section class="rst-box"><p>…</p><div class="rst-box-foot">Last updated 2 hours ago</div></section>
//
// list grid — the real data-table vocabulary. The card sets its columns
// once with the --rst-cols custom property (trailing 32px reserved for a
// kebab); rows only choose cells. A head row carries rst-lrow--head; a
// data row's identity cell is rst-nm, a column hidden below 800px is
// rst-m-hide, and the per-row overflow menu is a native
// <details class="rst-row-menu"> — no JavaScript:
//
//	<div class="rst-card" style="--rst-cols: 2fr 110px 32px">
//	  <div class="rst-lrow rst-lrow--head"><span>Order</span><span class="rst-m-hide">Status</span><span></span></div>
//	  <div class="rst-lrow">
//	    <a class="rst-nm" href="/orders/AB3PX">Grace Hopper<small>AB3PX · grace@example.com</small></a>
//	    <span class="rst-m-hide rst-cell-mut">Paid</span>
//	    <details class="rst-row-menu"><summary aria-label="Actions for order AB3PX">{{icon "kebab"}}</summary>
//	      <div class="rst-row-menu__panel"><a href="/orders/AB3PX">View</a><hr><button type="submit" class="rst-danger">Refund order…</button></div>
//	    </details>
//	  </div>
//	</div>
//
// dropdown — the details/summary menu vocabulary behind header overflow
// menus and a list-bar's Filter/Sort controls. A dropdown's exclusivity
// with its siblings (only one open at a time) is the native <details
// name> attribute, never JavaScript; rst-menu-group nests a submenu the
// same way. rst-caret is the disclosure arrow that flips on [open]:
//
//	<details class="rst-dropdown" name="list-controls">
//	  <summary>Filter<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span><span class="rst-sr-only">Filter orders: Paid</span></summary>
//	  <div class="rst-dropdown__menu">
//	    <a aria-current="true" href="/orders?status=paid">Paid</a>
//	    <details class="rst-menu-group" open><summary>Price</summary><div><a href="/orders?price=free">Free</a></div></details>
//	  </div>
//	</details>
//
// ftok — an applied filter as a removable chip. The × is a plain link to
// the unfiltered URL, so removing a filter is ordinary navigation, no
// JavaScript:
//
//	<span class="rst-ftok"><span class="rst-ftok__k">Paid</span><a href="/orders" aria-label="Remove filter Paid">✕</a></span>
//
// toggle-block — a bordered card whose head is a switch and whose body
// reveals only while that switch is on, via :has() — zero JavaScript.
// The switch is authoritative: a submit handler treats an unchecked
// switch as off no matter what the (still-POSTed, still-visible-in-the-
// DOM) revealed fields carry. The reveal is a display convenience, never
// a second source of truth:
//
//	<div class="rst-tblock">
//	  <label class="rst-tblock__head"><input type="checkbox" name="notify" checked>
//	    <span class="rst-switch__track" aria-hidden="true"></span>
//	    <span><span class="rst-tblock__title">Email notifications</span><span class="rst-tblock__desc">Sent for every reply.</span></span>
//	  </label>
//	  <div class="rst-tblock__body">…</div>
//	</div>
//
// modal route — a modal is its own URL, not client state. The response
// renders the page a Close click returns to, wrapped in an inert
// backdrop (rst-backdrop, marked inert so a keyboard or screen-reader
// user cannot reach it while the panel is open), then the overlay and
// panel on top. Closing is a plain link back to that same URL — never
// JavaScript, so there is nothing to wire up and nothing that can get
// out of sync with the page underneath:
//
//	<div class="rst-backdrop" inert>…the page a Close click returns to…</div>
//	<div class="rst-modal-overlay">
//	  <div class="rst-modal-panel">
//	    <nav><a href="/settings/profile" aria-current="page">Profile</a><a href="/settings/billing">Billing</a></nav>
//	    <section><a class="rst-modal-close" href="/settings" aria-label="Close settings">✕</a>…</section>
//	  </div>
//	</div>
//
// help — a bordered "?" icon-link to a help article, opening in a new
// tab. Its CSS tooltip (rst-tip, driven by the data-tip attribute) is
// decoration a sighted pointer user sees on hover or focus; it is never
// the accessible name, so the link carries its own full-sentence
// aria-label regardless:
//
//	<a class="rst-help rst-tip" href="/help/orders" target="_blank" rel="noopener" aria-label="Help: orders" data-tip="About orders">{{icon "help-circle"}}</a>
//
// selbox — the selection checkbox a list row wears in select mode. Its
// label restates the row's own identity rather than a bare "checkbox 3
// of 12", the same disambiguation list-row-action's ActionAria and
// row-menu's per-row aria-label already use:
//
//	<label class="rst-selbox"><input type="checkbox" aria-label="Select order AB3PX"></label>
//
// shell — the page frame's own chrome. rst-shell-topbar wraps a
// rst-shell__bar holding rst-shell__brand, rst-shell__nav,
// rst-shell__account and, below the page, rst-shell__foot;
// rst-shell-sidebar wraps a rst-shell__rail of rst-shell__group-labelled
// nav beside rst-shell__main, collapsing below 800px into a
// <details class="rst-shell__chrome"> — no JavaScript. Both carry
// rst-skip, the skip link. The canonical markup is styleguideSamples'
// "shell-topbar" and "shell-sidebar", and an app does not usually write
// any of it by hand: Layout ships the three shells as whole templates
// and rastrillo new writes the chosen one as templates/layout.html.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed partials/*.html
var partialsFS embed.FS

//go:embed tokens.css
var tokensCSS []byte

//go:embed themes/*.css
var themesFS embed.FS

//go:embed layouts/*.html
var layoutsFS embed.FS

//go:embed rastrillo.js
var shimJS []byte

//go:embed select.js
var selectJS []byte

// Templates returns the embedded partials rooted at partials/, so every
// caller parses "*.html" regardless of this package's own source-tree
// layout:
//
//	tmpl := template.Must(template.New("").Funcs(ui.Funcs()).
//	        ParseFS(ui.Templates(), "*.html"))
func Templates() fs.FS {
	sub, err := fs.Sub(partialsFS, "partials")
	if err != nil {
		panic(err) // partials/ is embedded above; this cannot fail
	}
	return sub
}

// TokensCSS returns tokens.css's raw bytes, for rastrillo new's scaffold
// step to write into a new app's static directory. The stylesheet is
// delivered once, at scaffold time, and is app-owned from then on.
//
// It is structure only. Colour and the type family live in a theme file
// beside it — see ThemeCSS.
func TokensCSS() []byte { return tokensCSS }

// themeNames lists the shipped themes, ink first: it is the reference
// theme, the one every other theme's token set is checked against
// (ui_test.go, TestThemesDeclareIdenticalTokenSets). The slice matches
// the files in themes/ exactly — adding a theme means adding both.
var themeNames = []string{"ink", "teal", "warm"}

// ThemeNames returns the shipped theme names, ink first. The returned
// slice is a copy, so a caller sorting or truncating it cannot reorder
// the library's own list.
func ThemeNames() []string { return append([]string(nil), themeNames...) }

// ThemeCSS returns one theme's raw bytes — the colour tokens and the
// type family tokens.css paints its component classes with — reporting
// false for a name that is not shipped. rastrillo new writes the chosen
// theme once as static/theme.css, beside tokens.css and on exactly the
// same terms: app-owned from then on, and swappable for a hand-written
// one without touching the structural stylesheet.
func ThemeCSS(name string) ([]byte, bool) {
	b, err := fs.ReadFile(themesFS, "themes/"+name+".css")
	return b, err == nil
}

// ShimJS returns rastrillo.js's raw bytes — the fragment shim — for
// rastrillo new's scaffold step to write into a new app's static
// directory beside tokens.css. Like the stylesheet, it is delivered
// once and app-owned from then on. The file's own header comment is
// its contract; TestShimContract holds the two honest.
func ShimJS() []byte { return shimJS }

// SelectJS returns select.js — field-select's searchable enhancement,
// on exactly the same terms as ShimJS: delivered once by rastrillo new,
// app-owned from then on, inert until a <select> opts in with
// data-rst-select.
//
// A sibling file rather than more of rastrillo.js so both stay small
// enough to read in one sitting. An app that never renders a select past
// ten options can delete it and the script tag; nothing else changes.
func SelectJS() []byte { return selectJS }

// layoutNames lists the shipped shells, column first: it is the plain
// centred page every scaffolded app starts on, and the two chrome
// shells are the ones an app opts into. The slice matches the files in
// layouts/ exactly — adding a shell means adding both.
var layoutNames = []string{"column", "topbar", "sidebar"}

// LayoutNames returns the shipped shell names, column first. The
// returned slice is a copy, so a caller sorting or truncating it cannot
// reorder the library's own list.
func LayoutNames() []string { return append([]string(nil), layoutNames...) }

// Layout returns one shell's raw template text — a complete
// layout.html defining "layout" — reporting false for a name that is
// not shipped. rastrillo new writes the chosen shell once as
// templates/layout.html, on the same terms as tokens.css and the theme:
// app-owned from the moment it lands.
//
// A shell is a page frame with holes in it. It executes
// {{template "content" .}} for the page's own body, and every piece of
// chrome around that is a block with a working default a page overrides
// by redefining it: title, lang and dir in all three, plus brand, nav,
// account and locale in the two chrome shells, and foot in topbar. No
// block reads a field off the data, so a shell renders the same whether
// a handler passes a struct, a dict-built map, or nil.
func Layout(name string) ([]byte, bool) {
	b, err := fs.ReadFile(layoutsFS, "layouts/"+name+".html")
	return b, err == nil
}
