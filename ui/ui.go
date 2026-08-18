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
// Styling comes from tokens.css, which rastrillo new writes once into a
// new app's static/ directory. rastrillo.Serve never serves it: from the
// moment it is scaffolded it is an ordinary app-owned static file the app
// is free to edit in place.
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
package ui

import (
	"embed"
	"io/fs"
)

//go:embed partials/*.html
var partialsFS embed.FS

//go:embed tokens.css
var tokensCSS []byte

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
func TokensCSS() []byte { return tokensCSS }
