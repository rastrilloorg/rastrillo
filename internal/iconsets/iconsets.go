// Package iconsets holds the vendored data for every icon set and
// delivery mode rastrillo can scaffold, and renders the app-owned
// internal/icons/icons.go from it.
//
// It is internal on purpose: a set an app did not choose must never
// compile into that app's binary.
//
// Every map here is keyed by LUCIDE slug regardless of set. Lucide's
// names are rastrillo's icon vocabulary (see the design doc); a set with
// different native names carries the translation in its own glyph data,
// so the shipped partials never change when the set does.
package iconsets

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

// Delivery is how an app's icons reach the browser.
type Delivery string

const (
	// Inline vendors the SVG into the binary: no build step, no second
	// origin, works offline. The default and the recommendation.
	Inline Delivery = "inline"
	// CDN loads the set's stylesheet from a second origin and renders
	// glyph elements that the stylesheet resolves.
	CDN Delivery = "cdn"
	// JS loads the set's script, which renders the glyphs client-side.
	JS Delivery = "js"
)

// Rendered is everything the scaffold needs to write for one choice.
type Rendered struct {
	Source      []byte // internal/icons/icons.go
	AttribName  string // "" when the set's licence needs no file
	Attribution []byte
	Notice      string // printed once by the scaffold; "" for inline
}

// glyph is one icon in one set. For Inline, Body is the SVG's inner
// markup and ViewBox its coordinate system. For CDN and JS, Element is
// the whole element the remote asset resolves.
type glyph struct {
	ViewBox string
	Body    string
	Element string
}

type set struct {
	// credit is emitted as a comment above the generated glyph map. Font
	// Awesome's licence asks that the attribution comments in their
	// distributed files not be removed; transcribing path data into Go
	// drops them as a side effect, so this puts one back where the data
	// actually lives. Empty for a set that asks nothing.
	credit       string
	openTag      string // inline <svg> open tag, minus the viewBox
	glyphs       map[string]glyph
	cdnHref      string
	cdnIntegrity string
	jsSrc        string
	jsIntegrity  string
	jsInit       string
	attribName   string
	attribution  string
}

// Slugs is rastrillo's icon vocabulary: the names the shipped partials
// call, matching the framework's own set in icons.go. Every set answers
// all of them.
//
// These are rastrillo's names, not any vendor's. Five of the twelve
// differ from Lucide's canonical names — "kebab" is Lucide's
// ellipsis-vertical, and v1 renamed four others (check-circle,
// alert-triangle, x-circle, help-circle). "menu" is NOT one of them:
// it is Lucide's own slug, and it is Font Awesome that calls that glyph
// "bars", which is a fact about the Font Awesome set below rather than
// a divergence from Lucide. docs/site/icons.md says the same five. Each
// set therefore carries its own translation in its glyph data, Lucide
// included, which is exactly why the shipped partials never change when
// the set does.
//
// Keep this list in step with icons.go's map: a slug the framework
// answers but a set does not is an icon that vanishes on switching sets,
// and TestEverySetCoversEverySlug fails loudly rather than let that ship.
func Slugs() []string {
	return []string{
		"alert-triangle", "check", "check-circle", "chevron-down",
		"help-circle", "info", "kebab", "menu", "plus", "search", "x",
		"x-circle",
	}
}

// LucideName is the name lucide.dev publishes one rastrillo slug under.
//
// Read off the vendored Lucide set's own CDN element rather than
// written out in a second table beside it: the class lucide-static's
// webfont binds to IS that icon's Lucide name, so the two cannot drift,
// and a slug the framework grows without a Lucide glyph answers ""
// here instead of a stale guess. TestEverySetCoversEverySlug already
// fails on such a slug, so an empty answer means the build is broken
// somewhere louder than this.
//
// It is what lets the design system show each icon's provenance without
// keeping a list of twelve names anywhere: internal/designsystem calls
// this once per rastrillo.IconSlugs() entry.
func LucideName(slug string) string {
	const marker = `class="icon icon-`
	g, ok := sets["lucide"].glyphs[slug]
	if !ok {
		return ""
	}
	i := strings.Index(g.Element, marker)
	if i < 0 {
		return ""
	}
	rest := g.Element[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// Names lists the sets an app can choose, sorted.
func Names() []string {
	out := make([]string, 0, len(sets))
	for name := range sets {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Deliveries lists the delivery modes an app can choose, sorted.
func Deliveries() []string { return []string{"cdn", "inline", "js"} }

// Versions are pinned exactly, and every cdnIntegrity/jsIntegrity below
// is a real sha384 computed against that exact published file. Bump a
// version and its hash together or SRI will reject the asset at load
// time, which fails silently as an unstyled page rather than loudly as
// an error. Recompute with:
//
//	curl -sfL <url> | openssl dgst -sha384 -binary | openssl base64 -A
//
// Nothing re-pins these automatically. `go test -tags pins
// ./internal/iconsets/` checks both halves against the network: that
// every pinned URL still hashes to the integrity value beside it, and
// whether a newer release exists. Run it at release time; it is
// build-tagged so the ordinary suite never depends on someone else's
// CDN being up.
//
// Font Awesome here is Font Awesome FREE. Pro is a paid product that
// cannot be vendored or linked on a user's behalf, so Pro-only icons do
// not resolve — an app with a Pro licence wires its own kit through the
// same ui.WithIcons seam instead.
var sets = map[string]set{
	"lucide": {
		// Lucide is a stroked set on a 24x24 grid. No width or height, so
		// the icon sizes from the app's own .icon rule and tracks its text.
		openTag: `<svg class="icon" fill="none" stroke="currentColor" stroke-width="2" ` +
			`stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"`,
		glyphs: map[string]glyph{
			"alert-triangle": {ViewBox: "0 0 24 24", Body: `<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><path d="M12 9v4"/><path d="M12 17h.01"/>`, Element: `<i class="icon icon-triangle-alert" aria-hidden="true"></i>`},
			"check":          {ViewBox: "0 0 24 24", Body: `<path d="M20 6 9 17l-5-5"/>`, Element: `<i class="icon icon-check" aria-hidden="true"></i>`},
			"check-circle":   {ViewBox: "0 0 24 24", Body: `<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>`, Element: `<i class="icon icon-circle-check" aria-hidden="true"></i>`},
			"chevron-down":   {ViewBox: "0 0 24 24", Body: `<path d="m6 9 6 6 6-6"/>`, Element: `<i class="icon icon-chevron-down" aria-hidden="true"></i>`},
			"help-circle":    {ViewBox: "0 0 24 24", Body: `<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>`, Element: `<i class="icon icon-circle-help" aria-hidden="true"></i>`},
			"info":           {ViewBox: "0 0 24 24", Body: `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`, Element: `<i class="icon icon-info" aria-hidden="true"></i>`},
			"kebab":          {ViewBox: "0 0 24 24", Body: `<circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/>`, Element: `<i class="icon icon-ellipsis-vertical" aria-hidden="true"></i>`},
			"menu":           {ViewBox: "0 0 24 24", Body: `<path d="M4 12h16"/><path d="M4 6h16"/><path d="M4 18h16"/>`, Element: `<i class="icon icon-menu" aria-hidden="true"></i>`},
			"plus":           {ViewBox: "0 0 24 24", Body: `<path d="M5 12h14"/><path d="M12 5v14"/>`, Element: `<i class="icon icon-plus" aria-hidden="true"></i>`},
			"search":         {ViewBox: "0 0 24 24", Body: `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`, Element: `<i class="icon icon-search" aria-hidden="true"></i>`},
			"x":              {ViewBox: "0 0 24 24", Body: `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`, Element: `<i class="icon icon-x" aria-hidden="true"></i>`},
			"x-circle":       {ViewBox: "0 0 24 24", Body: `<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>`, Element: `<i class="icon icon-circle-x" aria-hidden="true"></i>`},
		},
		// lucide-static's webfont binds via [class^="icon-"], [class*=" icon-"],
		// so "icon icon-search" matches on the second selector and still picks
		// up the app's own .icon sizing rule.
		cdnHref:      "https://cdn.jsdelivr.net/npm/lucide-static@1.33.0/font/lucide.css",
		cdnIntegrity: "sha384-GrjIauatmEPiuUAR2b7VVWFeyg6dBwqgSS0BWaICbpP9P7PisL3b/qpB+nfTbP5I",
		jsSrc:        "https://cdn.jsdelivr.net/npm/lucide@1.33.0/dist/umd/lucide.min.js",
		jsIntegrity:  "sha384-tCQ4YYVYKuOFYH+pnSIPOS3PaT3+5AmX2okMauzDJlLLxkQfJfSwmgpZPceZWs1F",
		jsInit:       "lucide.createIcons();",
	},
	"font-awesome": {
		// Font Awesome is a filled set; its solid icons sit on 512-high
		// viewBoxes that vary in width, so ViewBox is per glyph.
		credit: "Font Awesome Free 7.3.1 by @fontawesome - https://fontawesome.com\n" +
			"License - https://fontawesome.com/license/free\n" +
			"(Icons: CC BY 4.0, Fonts: SIL OFL 1.1, Code: MIT)\n" +
			"Copyright 2026 Fonticons, Inc. Please keep this notice.",
		openTag: `<svg class="icon" fill="currentColor" aria-hidden="true"`,
		glyphs: map[string]glyph{
			"alert-triangle": {ViewBox: "0 0 512 512", Body: `<path fill="currentColor" d="M256 0c14.7 0 28.2 8.1 35.2 21l216 400c6.7 12.4 6.4 27.4-.8 39.5S486.1 480 472 480L40 480c-14.1 0-27.2-7.4-34.4-19.5s-7.5-27.1-.8-39.5l216-400c7-12.9 20.5-21 35.2-21zm0 352a32 32 0 1 0 0 64 32 32 0 1 0 0-64zm0-192c-18.2 0-32.7 15.5-31.4 33.7l7.4 104c.9 12.5 11.4 22.3 23.9 22.3 12.6 0 23-9.7 23.9-22.3l7.4-104c1.3-18.2-13.1-33.7-31.4-33.7z"/>`, Element: `<i class="icon fa-solid fa-triangle-exclamation" aria-hidden="true"></i>`},
			"check":          {ViewBox: "0 0 448 512", Body: `<path d="M434.8 70.1c14.3 10.4 17.5 30.4 7.1 44.7l-256 352c-5.5 7.6-14 12.3-23.4 13.1s-18.5-2.7-25.1-9.3l-128-128c-12.5-12.5-12.5-32.8 0-45.3s32.8-12.5 45.3 0l101.5 101.5 234-321.7c10.4-14.3 30.4-17.5 44.7-7.1z"/>`, Element: `<i class="icon fa-solid fa-check" aria-hidden="true"></i>`},
			"check-circle":   {ViewBox: "0 0 512 512", Body: `<path fill="currentColor" d="M256 512a256 256 0 1 1 0-512 256 256 0 1 1 0 512zM374 145.7c-10.7-7.8-25.7-5.4-33.5 5.3L221.1 315.2 169 263.1c-9.4-9.4-24.6-9.4-33.9 0s-9.4 24.6 0 33.9l72 72c5 5 11.8 7.5 18.8 7s13.4-4.1 17.5-9.8L379.3 179.2c7.8-10.7 5.4-25.7-5.3-33.5z"/>`, Element: `<i class="icon fa-solid fa-circle-check" aria-hidden="true"></i>`},
			"chevron-down":   {ViewBox: "0 0 448 512", Body: `<path d="M201.4 406.6c12.5 12.5 32.8 12.5 45.3 0l192-192c12.5-12.5 12.5-32.8 0-45.3s-32.8-12.5-45.3 0L224 338.7 54.6 169.4c-12.5-12.5-32.8-12.5-45.3 0s-12.5 32.8 0 45.3l192 192z"/>`, Element: `<i class="icon fa-solid fa-chevron-down" aria-hidden="true"></i>`},
			"help-circle":    {ViewBox: "0 0 512 512", Body: `<path fill="currentColor" d="M256 512a256 256 0 1 0 0-512 256 256 0 1 0 0 512zm0-336c-17.7 0-32 14.3-32 32 0 13.3-10.7 24-24 24s-24-10.7-24-24c0-44.2 35.8-80 80-80s80 35.8 80 80c0 47.2-36 67.2-56 74.5l0 3.8c0 13.3-10.7 24-24 24s-24-10.7-24-24l0-8.1c0-20.5 14.8-35.2 30.1-40.2 6.4-2.1 13.2-5.5 18.2-10.3 4.3-4.2 7.7-10 7.7-19.6 0-17.7-14.3-32-32-32zM224 368a32 32 0 1 1 64 0 32 32 0 1 1 -64 0z"/>`, Element: `<i class="icon fa-solid fa-circle-question" aria-hidden="true"></i>`},
			"info":           {ViewBox: "0 0 512 512", Body: `<path fill="currentColor" d="M256 512a256 256 0 1 0 0-512 256 256 0 1 0 0 512zM224 160a32 32 0 1 1 64 0 32 32 0 1 1 -64 0zm-8 64l48 0c13.3 0 24 10.7 24 24l0 88 8 0c13.3 0 24 10.7 24 24s-10.7 24-24 24l-80 0c-13.3 0-24-10.7-24-24s10.7-24 24-24l24 0 0-64-24 0c-13.3 0-24-10.7-24-24s10.7-24 24-24z"/>`, Element: `<i class="icon fa-solid fa-circle-info" aria-hidden="true"></i>`},
			"kebab":          {ViewBox: "0 0 128 512", Body: `<path fill="currentColor" d="M64 144a56 56 0 1 1 0-112 56 56 0 1 1 0 112zm0 224c30.9 0 56 25.1 56 56s-25.1 56-56 56-56-25.1-56-56 25.1-56 56-56zm56-112c0 30.9-25.1 56-56 56s-56-25.1-56-56 25.1-56 56-56 56 25.1 56 56z"/>`, Element: `<i class="icon fa-solid fa-ellipsis-vertical" aria-hidden="true"></i>`},
			"menu":           {ViewBox: "0 0 448 512", Body: `<path fill="currentColor" d="M0 96C0 78.3 14.3 64 32 64l384 0c17.7 0 32 14.3 32 32s-14.3 32-32 32L32 128C14.3 128 0 113.7 0 96zM0 256c0-17.7 14.3-32 32-32l384 0c17.7 0 32 14.3 32 32s-14.3 32-32 32L32 288c-17.7 0-32-14.3-32-32zM448 416c0 17.7-14.3 32-32 32L32 448c-17.7 0-32-14.3-32-32s14.3-32 32-32l384 0c17.7 0 32 14.3 32 32z"/>`, Element: `<i class="icon fa-solid fa-bars" aria-hidden="true"></i>`},
			"plus":           {ViewBox: "0 0 448 512", Body: `<path d="M256 64c0-17.7-14.3-32-32-32s-32 14.3-32 32l0 160-160 0c-17.7 0-32 14.3-32 32s14.3 32 32 32l160 0 0 160c0 17.7 14.3 32 32 32s32-14.3 32-32l0-160 160 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-160 0 0-160z"/>`, Element: `<i class="icon fa-solid fa-plus" aria-hidden="true"></i>`},
			"search":         {ViewBox: "0 0 512 512", Body: `<path d="M416 208c0 45.9-14.9 88.3-40 122.7L502.6 457.4c12.5 12.5 12.5 32.8 0 45.3s-32.8 12.5-45.3 0L330.7 376C296.3 401.1 253.9 416 208 416 93.1 416 0 322.9 0 208S93.1 0 208 0 416 93.1 416 208zM208 352a144 144 0 1 0 0-288 144 144 0 1 0 0 288z"/>`, Element: `<i class="icon fa-solid fa-magnifying-glass" aria-hidden="true"></i>`},
			"x":              {ViewBox: "0 0 384 512", Body: `<path fill="currentColor" d="M55.1 73.4c-12.5-12.5-32.8-12.5-45.3 0s-12.5 32.8 0 45.3L147.2 256 9.9 393.4c-12.5 12.5-12.5 32.8 0 45.3s32.8 12.5 45.3 0L192.5 301.3 329.9 438.6c12.5 12.5 32.8 12.5 45.3 0s12.5-32.8 0-45.3L237.8 256 375.1 118.6c12.5-12.5 12.5-32.8 0-45.3s-32.8-12.5-45.3 0L192.5 210.7 55.1 73.4z"/>`, Element: `<i class="icon fa-solid fa-xmark" aria-hidden="true"></i>`},
			"x-circle":       {ViewBox: "0 0 512 512", Body: `<path fill="currentColor" d="M256 512a256 256 0 1 0 0-512 256 256 0 1 0 0 512zM167 167c9.4-9.4 24.6-9.4 33.9 0l55 55 55-55c9.4-9.4 24.6-9.4 33.9 0s9.4 24.6 0 33.9l-55 55 55 55c9.4 9.4 9.4 24.6 0 33.9s-24.6 9.4-33.9 0l-55-55-55 55c-9.4 9.4-24.6 9.4-33.9 0s-9.4-24.6 0-33.9l55-55-55-55c-9.4-9.4-9.4-24.6 0-33.9z"/>`, Element: `<i class="icon fa-solid fa-circle-xmark" aria-hidden="true"></i>`},
		},
		cdnHref:      "https://cdn.jsdelivr.net/npm/@fortawesome/fontawesome-free@7.3.1/css/all.min.css",
		cdnIntegrity: "sha384-qrALq7+6jBOZIQsNnT6xGkMDru64qD6uTlDra39xrt2SoXl4pO3FX6Roz/RpR/BS",
		jsSrc:        "https://cdn.jsdelivr.net/npm/@fortawesome/fontawesome-free@7.3.1/js/all.min.js",
		jsIntegrity:  "sha384-nUoGK97t7uWSCbMZY3w/oH8v2WJFAsPdz0qtcXgHwyCeTlyvTClTftOOsjZHfdhj",
		attribName:   "ICONS-LICENCE.md",
		attribution: `# Icon licence — Font Awesome Free 7.3.1

The icons in this app's icons package are Font Awesome Free by
Fonticons, Inc. — https://fontawesome.com — used under the Font Awesome
Free License: https://fontawesome.com/license/free

Which licence covers what, per that file:

- Icons (SVG and JS file types): CC BY 4.0
  https://creativecommons.org/licenses/by/4.0/
- Fonts (web and desktop font files): SIL OFL 1.1
- Code: MIT

Which of those you are relying on depends on the delivery mode chosen at
scaffold time: inline vendors SVG path data (icons, CC BY 4.0); cdn loads
the stylesheet and its webfonts (fonts, OFL, plus MIT code); js loads
their script (icons-as-JS and MIT code).

## Why this file exists

Font Awesome's licence says their distributed files carry embedded
attribution comments that are sufficient on their own, and asks that you
not work to remove them. rastrillo does not ship their files: it
transcribes path data into an app-owned Go source file, which drops those
comments as a side effect. This file, and the comment above the glyph map
in the icons package, are that attribution restored — keep both.

Nothing here obliges you to credit Font Awesome in the running app's UI.
Doing so is welcome but is not what the licence asks for.
`,
	},
}

const noticeCDN = `Icons load from a CDN. The app now depends on a second origin it
cannot serve itself: icons will not render offline, the origin must be
allowed by any Content-Security-Policy, and the pinned SRI must be
updated whenever the version is.`

const noticeJS = `Icons render client-side via JavaScript. They will not appear with
JavaScript disabled, or before the script runs. The app also depends on
a second origin it cannot serve itself. tokens.css reserves the icon box
so the page does not shift as they resolve.`

// Render produces the app-owned internal/icons package for one choice.
func Render(name string, d Delivery) (Rendered, error) {
	s, ok := sets[name]
	if !ok {
		return Rendered{}, fmt.Errorf("unknown icon set %q (have %v)", name, Names())
	}
	if d != Inline && d != CDN && d != JS {
		return Rendered{}, fmt.Errorf("unknown icon delivery %q (have %v)", d, Deliveries())
	}

	data := struct {
		Set           string
		Delivery      Delivery
		Inline        bool
		OpenTag       string
		Slugs         []string
		Glyphs        map[string]glyph
		Href          string
		HrefIntegrity string
		Src           string
		SrcIntegrity  string
		Init          string
		Notice        string
		Credit        string
	}{
		Set: name, Delivery: d, Inline: d == Inline,
		OpenTag: s.openTag, Slugs: Slugs(), Glyphs: s.glyphs,
		Credit: s.credit,
	}
	switch d {
	case CDN:
		data.Href, data.HrefIntegrity, data.Notice = s.cdnHref, s.cdnIntegrity, noticeCDN
	case JS:
		data.Src, data.SrcIntegrity, data.Init, data.Notice = s.jsSrc, s.jsIntegrity, s.jsInit, noticeJS
	}

	var buf bytes.Buffer
	if err := sourceTmpl.Execute(&buf, data); err != nil {
		return Rendered{}, err
	}
	// gofmt the result: this is source written into someone's repository,
	// so it should arrive formatted the way they would have written it.
	// format.Source also parses, which turns a malformed template into a
	// loud error here rather than a broken package in a scaffolded app.
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return Rendered{}, fmt.Errorf("rendered icons.go for %s/%s does not parse: %w", name, d, err)
	}
	out := Rendered{Source: src, Notice: data.Notice}
	if s.attribName != "" {
		out.AttribName, out.Attribution = s.attribName, []byte(s.attribution)
	}
	return out, nil
}

// sourceTmpl renders Go source, so it is text/template: html/template
// would escape the very markup it is trying to emit.
//
// The helpers are registered before Parse because text/template resolves
// function names at parse time — a FuncMap added afterwards is too late.
// bt emits a backtick, which a Go raw string literal cannot contain.
var sourceTmpl = template.Must(template.New("icons.go").Funcs(template.FuncMap{
	// comment turns the delivery notice into a Go comment block, so the
	// tradeoff is recorded in the generated source rather than only
	// printed once at scaffold time and forgotten.
	"comment": func(s string) string {
		var b strings.Builder
		for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			b.WriteString("// " + line + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	},
	"bt": func() string { return "`" },
}).Parse(`// Package icons is this app's icon set, scaffolded once by
// rastrillo new --icons={{.Set}} --icon-delivery={{.Delivery}} and owned
// by this app from then on. Edit it, add to it, or replace it.
//
// Keys are Lucide slugs whatever the set: that is rastrillo's icon
// vocabulary, so {{"{{icon \"search\"}}"}} means the same thing in every
// app and the shipped ui/ partials never have to change.
//
// Wire both seams onto your template tree:
//
//	tmpl := template.Must(template.New("").
//	        Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
//	        ParseFS(ui.Templates(), "*.html"))
//
// Every icon is aria-hidden: it sits beside its own visible text label,
// which is the accessible name. A control that uses an icon as its ONLY
// label needs an explicit aria-label.
{{- if not .Inline}}
//
// TRADEOFF, chosen at scaffold time:
{{comment .Notice}}
{{- end}}
package icons

import "html/template"

{{if .Credit}}{{comment .Credit}}
{{end}}var icons = map[string]template.HTML{
{{- range .Slugs}}{{$g := index $.Glyphs .}}
	{{printf "%q" .}}: {{bt}}{{if $.Inline}}{{$.OpenTag}} viewBox="{{$g.ViewBox}}">{{$g.Body}}</svg>{{else}}{{$g.Element}}{{end}}{{bt}},
{{- end}}
}

// Icon renders one icon by its Lucide slug. An unknown slug renders
// nothing rather than panicking a page mid-response — a typo costs a
// missing icon, not a crash. rastrillo generate --check catches it
// before ship.
func Icon(slug string) template.HTML { return icons[slug] }

// Assets is the markup this delivery mode needs in the document <head>.
{{if .Inline -}}
// Inline delivery vendors every icon into the binary, so there is
// nothing to load and this returns empty. Call it anyway: switching
// --icon-delivery later then needs no template change.
func Assets() template.HTML { return "" }
{{- else if .Href -}}
func Assets() template.HTML {
	return {{bt}}<link rel="stylesheet" href="{{.Href}}" integrity="{{.HrefIntegrity}}" crossorigin="anonymous" referrerpolicy="no-referrer">{{bt}}
}
{{- else -}}
func Assets() template.HTML {
	return {{bt}}<script src="{{.Src}}" integrity="{{.SrcIntegrity}}" crossorigin="anonymous" referrerpolicy="no-referrer" defer></script>{{if .Init}}<script>window.addEventListener("DOMContentLoaded", function () { {{.Init}} });</script>{{end}}{{bt}}
}
{{- end}}
`))
