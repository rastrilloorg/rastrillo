package designsystem

import (
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

// ── The page model ───────────────────────────────────────────────────
//
// Every URL on a page is an absolute path under mount: an asset is
// /design-system/tokens.css, a shell demo is
// /design-system/day/en/shells/topbar.html, wherever the page holding
// the link sits in the tree. There used to be a pair of "../../" depth
// prefixes here instead; designsystem.go's mount comment has the
// edge behaviour that made them wrong.
//
// The one thing absolute paths cost: a page can no longer link to
// "itself" as a bare filename, so the theme and locale switchers point
// their current entry at that page's canonical address. From
// index.html that address is day/en/index.html — a different file
// holding the same bytes, which is what the tree root is.

type navLink struct {
	Label   string
	Href    string
	Current bool
}

type localeLink struct {
	Code    string
	Name    string
	Dir     string
	Href    string
	Current bool
}

type stateView struct {
	State   string
	Note    string
	Preview previewView
}

type partialView struct {
	Name   string
	ID     string
	Marker template.HTML
	Blurb  string
	Wrap   wrapper
	States []stateView
}

type familyView struct {
	Title    string
	ID       string
	Blurb    string
	Partials []partialView
}

type idiomView struct {
	Name    string
	ID      string
	Marker  template.HTML
	Rule    template.HTML // the #98 callout this idiom carries, if any
	Blurb   string
	Preview previewView

	// DemoLabel and DemoHref are the three idioms that also exist as a
	// page of this tree: the modal route and the two shells. Their
	// preview frames the sample like every other one does, and the link
	// goes to the same idiom at a URL of its own, in a new tab — which
	// is the whole demonstration in the modal's case. See demoIdioms.
	DemoLabel string // the link's own words
	DemoHref  string
}

type shellView struct {
	Name    string
	ID      string
	Href    string
	Blurb   string
	Preview previewView
}

// schemeButton is one position of the colour-scheme toggle. Pressed is
// the server-rendered aria-pressed: System is the state a page with no
// JavaScript is actually in, so it is the one that starts pressed, and
// gallery.js repaints all three from localStorage before the reader
// sees them.
type schemeButton struct {
	Value   string
	Label   string
	Pressed string
}

type pageView struct {
	Theme      string
	Locale     string
	LocaleName string
	Dir        string

	// Kind is which of the five pages this is ("overview", "tokens",
	// "components", "primitives", "shells"), and Title is that page's
	// own name in this locale. Both come off one row of pageKinds().
	Kind  string
	Title string

	// Body is the section's own markup: the ds-body-<kind> template,
	// executed against this same view and handed back to the frame.
	// template.HTML because it is this package's own output — see
	// renderBody, which is the only thing that ever sets it.
	Body template.HTML

	// Mount is mount, so the template can write
	// href="{{.Mount}}/tokens.css" without knowing it.
	Mount string

	// Sub is the page's opening sentence, built in Go rather than in
	// the template because it interleaves prose with two names that
	// have to carry markup of their own: the theme in <strong>, and the
	// language in a <strong lang=…> so a screen reader says the autonym
	// in its own voice. See proseMarkup.
	Sub template.HTML

	// Pages is the section tab strip over the page: one entry per page
	// kind, the current one marked. It is the rail's job done again in
	// a row, and it earns its place below 800px, where the shell folds
	// the rail away behind a disclosure.
	Pages []navLink

	Themes    []navLink
	Schemes   []schemeButton
	Locales   []localeLink
	Colours   []tokenGroup
	Structure []tokenGroup
	Families  []familyView
	Idioms    []idiomView
	Shells    []shellView

	// Prev and Next are the pair of links at the foot of the page, in
	// pageKinds() order. Either is nil at the ends of the sequence:
	// the Overview has no previous and the last page has no next.
	Prev *navLink
	Next *navLink

	// Routes is the Overview's way into the rest of the gallery: every
	// other page kind, named, with one sentence saying what is on it.
	// Built for every page, rendered by the Overview. See pageRoutes.
	Routes []routeView

	// Demo is the demo application, framed, and DemoHref is the same
	// page at its own address. The Overview renders both; see demoView.
	Demo     previewView
	DemoHref string

	// Nav is the sidebar: derived from the five fields above it, once
	// they are built, so it cannot list anything the page does not
	// render and cannot miss anything it does. See galleryNav.
	Nav []navSection
}

// ── The sidebar ──────────────────────────────────────────────────────
//
// The rail is a second view of the page, not a second copy of it. Every
// entry in it is read off the same slices the body renders — the token
// groups, the families and their partials, the idioms, the shells — so
// a partial added to ui, a family added to samples.go or a token added
// to a theme appears in the sidebar the same day it appears in the
// page, with no list to remember to update.
//
// That is a gate as much as a design. TestTheSidebarLinksEverythingOnThePageExactlyOnce
// holds the two sides to one sequence — the fragments the rail links,
// in rail order, are the anchored elements on the page, in page order —
// so an entry here pointing at nothing, and a section rendered with no
// way to reach it, are both build failures.

// navItem is one line in the rail. Code is set for the entries whose
// label is an identifier — a partial's template name, an idiom's class
// family, a shell's name — which the rail draws in the mono face for
// the same reason the headings on the page do.
//
// Group marks a family heading inside the Partials section: a link like
// any other, drawn as the rail's own group label, and hidden by the
// filter when everything under it has gone.
// Blank marks the entries that leave this document — the demo pages,
// the only off-page links the rail has. They open in a new tab for the
// reason every other demo link on this page does: a reader is in the
// middle of a long page and a filter they typed, and a demo is a
// detour, not a destination.
type navItem struct {
	Label string
	Href  string
	Code  bool
	Group bool
	Blank bool

	// Aria is the accessible name, where it has to differ from the
	// visible label. Exactly one entry uses it: the section overview
	// link, whose visible word is "Overview" on every section and whose
	// accessible name carries the section — "Tokens overview" — so a
	// screen reader does not hear the same word four times in one
	// navigation landmark. It CONTAINS the visible label, which is what
	// WCAG 2.2 SC 2.5.3 Label in Name asks for; disambiguating by
	// changing the visible word would fail a different reader instead.
	Aria string
}

// navSection is one group in the rail: a page of this directory, or —
// for Demos, the only one that is not — the run of links that leave
// the gallery altogether.
//
// Href is the section's own page, and Current says whether that page is
// the one being rendered. A section with items renders as a <details>,
// open and aria-current when it is the current one; a section with none
// yet renders as a plain link, because a disclosure over an empty box
// is a control that does nothing.
type navSection struct {
	Title   string
	Href    string
	Current bool
	Items   []navItem
}

// ── The page kinds ───────────────────────────────────────────────────
//
// One page per section, each at its own URL under <theme>/<locale>/.
// The table below is the whole of that seam: a row here is a page in
// the tree, an entry in the rail, a tab in the strip over the page and
// a target for both switchers, with nothing else to remember.
//
// ADDING A PAGE KIND — the four things, and there are only four:
//
//  1. a row in pageKinds(), with the file it renders to, its English
//     title and its English blurb (both prose keys, so both need their
//     eleven translations in prose.go) and the function that reads its
//     rail entries off the finished view — nil where the page anchors
//     nothing yet, which draws the section as a plain link to it;
//  2. a `{{define "ds-body-<kind>"}}` constant beside the others in
//     this file, named exactly "ds-body-" + the row's Kind, because
//     renderBody looks it up by that name;
//  3. that constant in bodyTemplates(), which is what parses it into
//     the tree and what TestNoUnregisteredEnglishInThePageTemplates
//     sweeps for English;
//  4. the page's own data on pageView, if the body needs any the five
//     existing fields do not already carry.
//
// Nothing else moves. TestTreeShapeIsComplete counts the tree off this
// same table, so the new file is expected the moment the row lands, and
// the coverage, nav, chrome and link gates all walk it too.
type pageKind struct {
	Kind  string
	File  string
	Title string // English, and therefore a prose.go key
	// Blurb is the one sentence the Overview says about this page when
	// it routes a reader to it. English, and therefore a prose.go key
	// too. Empty on the Overview itself, which is the one page that
	// never appears in its own route list — see pageRoutes.
	Blurb string
	// Nav reads this page's rail entries off the finished view. nil is
	// a page with nothing anchored on it yet.
	Nav func(mount, theme, locale string, view pageView) []navItem
}

func pageKinds() []pageKind {
	return []pageKind{
		{Kind: "overview", File: "index.html", Title: "Overview"},
		{Kind: "tokens", File: "tokens.html", Title: "Tokens", Nav: tokenNav,
			Blurb: "Every custom property the system is built out of: the theme's colour and type, and the scales for size, spacing and radius."},
		{Kind: "components", File: "components.html", Title: "Components", Nav: componentNav,
			Blurb: "The framework's template partials, each one rendered in every state it ships with, with the markup beside it."},
		{Kind: "primitives", File: "primitives.html", Title: "UI primitives", Nav: primitiveNav,
			Blurb: "The shapes a component cannot be, because they wrap a body only the caller knows: cards, data grids, menus and the shells' own chrome."},
		{Kind: "shells", File: "shells.html", Title: "Shells", Nav: shellNav,
			Blurb: "The three page frames rastrillo new can scaffold, each of them openable as a whole page at full width."},
	}
}

// fileOf is one page kind's filename. It panics on a kind that is not
// in the table because every caller passes a Kind it read out of the
// table in the first place: a miss is a programming error in this file,
// not a condition to handle.
func fileOf(kind string) string {
	for _, pk := range pageKinds() {
		if pk.Kind == kind {
			return pk.File
		}
	}
	panic("designsystem: no page kind " + kind)
}

// anchorHrefIn is a link to one anchored element on one page of this
// directory: an absolute page address with the fragment on the end.
// Used for the current page's own entries too — see pageTemplate's
// comment for why every entry in the rail has one shape.
func anchorHrefIn(mount, theme, locale, file, id string) string {
	return pageHref(mount, theme, locale, file) + "#" + id
}

func tokenNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("tokens")
	var items []navItem
	for _, g := range view.Colours {
		items = append(items, navItem{Label: g.Title, Href: anchorHrefIn(mount, theme, locale, file, g.ID)})
	}
	for _, g := range view.Structure {
		items = append(items, navItem{Label: g.Title, Href: anchorHrefIn(mount, theme, locale, file, g.ID)})
	}
	return items
}

func componentNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("components")
	var items []navItem
	for _, fam := range view.Families {
		items = append(items, navItem{Label: fam.Title, Href: anchorHrefIn(mount, theme, locale, file, fam.ID), Group: true})
		for _, p := range fam.Partials {
			items = append(items, navItem{Label: p.Name, Href: anchorHrefIn(mount, theme, locale, file, p.ID), Code: true})
		}
	}
	return items
}

func primitiveNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("primitives")
	var items []navItem
	for _, idiom := range view.Idioms {
		items = append(items, navItem{Label: idiom.Name, Href: anchorHrefIn(mount, theme, locale, file, idiom.ID), Code: true})
	}
	return items
}

func shellNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("shells")
	var items []navItem
	for _, sh := range view.Shells {
		items = append(items, navItem{Label: sh.Name, Href: anchorHrefIn(mount, theme, locale, file, sh.ID), Code: true})
	}
	return items
}

// galleryNav builds the rail from a finished view: one section per page
// kind, in the order the pages themselves are in, and last the demo
// pages — the only entries in the rail that leave this tree's gallery
// altogether, and the reason the modal demo is not reachable only from
// a sentence halfway down the primitives.
//
// The rail is identical on all five pages apart from which section
// carries `open aria-current="page"`, which is what makes it a rail
// rather than five tables of contents. TestTheRailIsTheSameOnEveryPage
// holds that literally.
func galleryNav(mount, theme, locale, kind string, view pageView) []navSection {
	out := make([]navSection, 0, len(pageKinds())+1)
	for _, pk := range pageKinds() {
		section := navSection{
			Title:   proseIn(locale, pk.Title),
			Href:    pageHref(mount, theme, locale, pk.File),
			Current: pk.Kind == kind,
		}
		if pk.Nav != nil {
			// The section overview link, first, before the anchors.
			// A section that lists anything draws its title as a
			// <summary>, which discloses rather than navigates — so
			// without this, expanding TOKENS showed nine fragments of
			// tokens.html and no way to tokens.html itself. A section
			// with nothing to list is already a plain link to its own
			// page, so it does not need a second one under it.
			section.Items = append([]navItem{sectionOverview(locale, section)}, pk.Nav(mount, theme, locale, view)...)
		}
		out = append(out, section)
	}

	demos := navSection{Title: proseIn(locale, "Demos")}
	for _, sh := range view.Shells {
		demos.Items = append(demos.Items, navItem{Label: proseIn(locale, "The {shell} shell", "shell", sh.Name), Href: sh.Href, Blank: true})
	}
	demos.Items = append(demos.Items, navItem{Label: proseIn(locale, "The modal route"), Href: modalHref(mount, theme, locale), Blank: true})
	demos.Items = append(demos.Items, navItem{Label: proseIn(locale, "The demo application"), Href: demoHref(mount, theme, locale), Blank: true})
	return append(out, demos)
}

// sectionOverview is one section's route to the top of its own page:
// the first item under it, labelled with Paul's word — Overview, which
// is also the word the rail's filter matches on — and named for a
// screen reader by the section it belongs to.
//
// The visible label is deliberately the same on every section and the
// accessible name deliberately is not. Four links reading "Overview" in
// one navigation landmark are unambiguous nested under their headings
// and ambiguous read out in a list, so the accessible name carries the
// section: "Tokens overview". It contains the visible label, so WCAG
// 2.2 SC 2.5.3 Label in Name is satisfied without changing the word a
// sighted reader sees or types into the filter.
func sectionOverview(locale string, section navSection) navItem {
	return navItem{
		Label: proseIn(locale, "Overview"),
		Href:  section.Href,
		Aria:  proseIn(locale, "{section} overview", "section", section.Title),
	}
}

// pageTabs is the strip of section tabs over the page: the same five
// pages the rail names, in the same order, with the current one marked.
func pageTabs(mount, theme, locale, kind string) []navLink {
	out := make([]navLink, 0, len(pageKinds()))
	for _, pk := range pageKinds() {
		out = append(out, navLink{
			Label:   proseIn(locale, pk.Title),
			Href:    pageHref(mount, theme, locale, pk.File),
			Current: pk.Kind == kind,
		})
	}
	return out
}

// pageSteps is the pair of links at the foot of one page: the page
// before it and the page after it, in pageKinds() order. Either can be
// nil — the first page has no previous and the last has no next — and
// the template leaves the missing side's space rather than pulling the
// other one across it.
//
// Derived from the table like everything else on this seam, so a sixth
// page kind joins the sequence with no edit here: it becomes the last
// page's next and the new last page.
func pageSteps(mount, theme, locale, kind string) (prev, next *navLink) {
	kinds := pageKinds()
	at := -1
	for i, pk := range kinds {
		if pk.Kind == kind {
			at = i
			break
		}
	}
	if at < 0 {
		panic("designsystem: no page kind " + kind)
	}
	step := func(i int, format string) *navLink {
		if i < 0 || i >= len(kinds) {
			return nil
		}
		pk := kinds[i]
		title := proseIn(locale, pk.Title)
		return &navLink{
			// The name is the label, not an arrow beside one: "Next"
			// alone makes a reader click to find out where they are
			// going, which is the whole of what this pair is for.
			Label: proseIn(locale, format, "page", title),
			Href:  pageHref(mount, theme, locale, pk.File),
		}
	}
	return step(at-1, "Previous: {page}"), step(at+1, "Next: {page}")
}

// routeView is one entry in the Overview's route list: another page of
// this gallery, its name, and the one sentence saying what is on it.
type routeView struct {
	Label string
	Blurb string
	Href  string
}

// pageRoutes is the Overview's routes into the rest of the gallery: one
// per page kind that is not the page being rendered, in table order,
// each carrying that row's own Blurb. Read off pageKinds() rather than
// written out, so a sixth page kind appears here the day its row lands
// — and its Blurb is the only new thing anybody has to write.
//
// It is built for every page and rendered only by the Overview, which
// is why the Overview's own row carries no Blurb: a page never routes
// to itself.
func pageRoutes(mount, theme, locale, kind string) []routeView {
	out := make([]routeView, 0, len(pageKinds())-1)
	for _, pk := range pageKinds() {
		if pk.Kind == kind {
			continue
		}
		out = append(out, routeView{
			Label: proseIn(locale, pk.Title),
			Blurb: proseIn(locale, pk.Blurb),
			Href:  pageHref(mount, theme, locale, pk.File),
		})
	}
	return out
}

// ── Anchors ──────────────────────────────────────────────────────────

// anchorID is the id one anchorable thing on the page wears, and the
// fragment the rail links it by. Two rules make it worth having its own
// function rather than being spelled out at four call sites:
//
//   - it is built from the ENGLISH name, so the twelve translations of
//     a page carry the same sixty fragments and a link into the
//     gallery survives a change of language;
//   - it is prefixed by kind, so a partial called "badge" and an idiom
//     called "badge" are two ids and not one. A duplicate id is the
//     failure a rail full of fragments would hide — both entries scroll
//     to the first element — and the same gate counts them.
func anchorID(kind, name string) string { return kind + "-" + slug(name) }

// slug lowercases and hyphenates. ASCII letters and digits survive;
// everything else becomes a single hyphen, and leading and trailing
// hyphens go. The inputs are English headings and identifiers already
// written in this alphabet — "Surfaces and lines", "field-select" — so
// there is no transliteration question to answer here.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// ── Building one page ────────────────────────────────────────────────

// renderGallery builds all five pages of one theme × locale directory,
// keyed by filename. One call rather than five because everything
// expensive about a page — parsing ui's whole partial set, rendering a
// hundred and ten samples into their preview documents, parsing the
// theme's stylesheet — is shared by all five, and doing it per page
// would be five times the work for the same bytes.
//
// Where a page ends up in the tree does not change a byte of it: every
// link it carries is absolute. Theme, locale and kind are the whole of
// a page's identity.
func renderGallery(mount, theme, locale string) (map[string][]byte, error) {
	tmpl, err := partialTree(locale)
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(pageTemplate); err != nil {
		return nil, fmt.Errorf("parsing the page frame: %w", err)
	}
	for _, body := range bodyTemplates() {
		if _, err := tmpl.Parse(body.src); err != nil {
			return nil, fmt.Errorf("parsing the %s body: %w", body.kind, err)
		}
	}
	if _, err := tmpl.Parse(viewTemplate); err != nil {
		return nil, fmt.Errorf("parsing the preview widget: %w", err)
	}
	// Every hand-written sample is parsed into the tree before anything
	// executes: html/template refuses to Clone or Parse into a tree that
	// has already run, so there is no lazily-add-one-later option here.
	if err := parseRawSamples(tmpl); err != nil {
		return nil, err
	}

	colours, err := themePalette(theme)
	if err != nil {
		return nil, err
	}
	structure, err := structuralGroups()
	if err != nil {
		return nil, err
	}
	families, err := buildFamilies(mount, tmpl, theme, locale)
	if err != nil {
		return nil, err
	}
	idioms, err := buildIdioms(mount, tmpl, theme, locale)
	if err != nil {
		return nil, err
	}

	localeName := rastrillo.BaseCatalogs()[locale]["rastrillo.ui.locale_name"]
	base := pageView{
		Theme: theme, Locale: locale, Dir: rastrillo.Dir(locale),
		LocaleName: localeName,
		Mount:      mount,
		Sub:        subhead(locale, theme, localeName),
		Schemes:    schemeButtons(locale),
		Colours:    localiseGroups(locale, colours),
		Structure:  localiseGroups(locale, structure),
		Families:   families,
		Idioms:     idioms,
		Shells:     shellViews(mount, theme, locale),
	}

	out := make(map[string][]byte, len(pageKinds()))
	for _, pk := range pageKinds() {
		view := base
		view.Kind = pk.Kind
		view.Title = proseIn(locale, pk.Title)
		view.Pages = pageTabs(mount, theme, locale, pk.Kind)
		view.Themes = themeLinks(mount, theme, locale, pk.File)
		view.Locales = localeLinks(mount, theme, locale, pk.File)
		view.Prev, view.Next = pageSteps(mount, theme, locale, pk.Kind)
		view.Routes = pageRoutes(mount, theme, locale, pk.Kind)
		view.Demo, view.DemoHref = demoView(mount, theme, locale), demoHref(mount, theme, locale)
		// Last, and off the finished view: the rail is a reading of the
		// whole gallery, so it is built once everything it reads exists.
		view.Nav = galleryNav(mount, theme, locale, pk.Kind, view)

		body, err := renderBody(tmpl, pk.Kind, view)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pk.File, err)
		}
		view.Body = body

		var buf strings.Builder
		if err := tmpl.ExecuteTemplate(&buf, "ds-page", view); err != nil {
			return nil, fmt.Errorf("%s: %w", pk.File, err)
		}
		out[pk.File] = []byte(buf.String())
	}
	return out, nil
}

// bodyTemplates is the section bodies, in page order: the constant that
// defines each one, beside the kind it belongs to. The kind is carried
// so the parse error names the page rather than a line number, and so
// TestNoUnregisteredEnglishInThePageTemplates can sweep this list
// rather than a second one written beside it.
func bodyTemplates() []struct{ kind, src string } {
	return []struct{ kind, src string }{
		{"overview", overviewBody},
		{"tokens", tokensBody},
		{"components", componentsBody},
		{"primitives", primitivesBody},
		{"shells", shellsBody},
	}
}

// renderBody executes one page's section body against the finished
// view. The result is this package's own template output, escaped by
// html/template on the way out, so handing it back to the frame as
// template.HTML re-inserts exactly what was just written — the one
// thing the alternative, a {{if eq .Kind …}} chain in the frame, would
// have bought is a template name html/template can resolve statically,
// and it would have cost a line in the frame per page kind forever.
func renderBody(tmpl *template.Template, kind string, view pageView) (template.HTML, error) {
	name := "ds-body-" + kind
	if tmpl.Lookup(name) == nil {
		return "", fmt.Errorf("no %s template", name)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, name, view); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// themeLinks is the theme switcher. It keeps the reader on the page
// they are reading: choosing another theme from components.html lands
// on that theme's components.html, not back at the overview.
func themeLinks(mount, theme, locale, file string) []navLink {
	out := make([]navLink, 0, len(ui.ThemeNames()))
	for _, name := range ui.ThemeNames() {
		out = append(out, navLink{
			Label:   name,
			Href:    pageHref(mount, name, locale, file),
			Current: name == theme,
		})
	}
	return out
}

// schemeButtons is the colour-scheme toggle: three positions, System
// first, because System is where every reader starts and is the only
// state the page can be in with scripts off.
//
// The values are the ones the themes already understand — data-theme
// "light" and "dark" on <html>, and no attribute at all for System,
// which is color-scheme: light dark doing the deciding. gallery.js
// writes the attribute; nothing here does.
func schemeButtons(locale string) []schemeButton {
	labels := map[string]string{"system": "System", "light": "Light", "dark": "Dark"}
	out := make([]schemeButton, 0, 3)
	for _, value := range []string{"system", "light", "dark"} {
		pressed := "false"
		if value == "system" {
			pressed = "true"
		}
		out = append(out, schemeButton{Value: value, Label: proseIn(locale, labels[value]), Pressed: pressed})
	}
	return out
}

// localiseGroups translates the token headings. The groups are built by
// designsystem.go, which reads a stylesheet and knows nothing about
// what language anybody is reading in; translating the titles on the
// way into the view keeps the parser and the page's words apart.
func localiseGroups(locale string, groups []tokenGroup) []tokenGroup {
	out := make([]tokenGroup, len(groups))
	for i, g := range groups {
		g.Title = proseIn(locale, g.Title)
		out[i] = g
	}
	return out
}

// subhead is the page's opening sentence: one prose string with two
// placeholders, and two names substituted into it as markup rather than
// as text. See proseMarkup for why the escaping runs in that order.
func subhead(locale, theme, localeName string) template.HTML {
	return proseMarkup(locale,
		"An overview of everything the design system provides. Theme: {theme}. Language: {language}.",
		"theme", template.HTML("<strong>"+template.HTMLEscapeString(theme)+"</strong>"),
		"language", template.HTML(`<strong lang="`+template.HTMLEscapeString(locale)+`">`+template.HTMLEscapeString(localeName)+"</strong>"),
	)
}

// indexHref is one theme × locale page's canonical address. Every
// switcher entry uses it, the current one included: the tree root is a
// copy of day/en, so "the page you are on" and "this page's address"
// are the same document even when they are two files.
func indexHref(mount, theme, locale string) string {
	return pageHref(mount, theme, locale, "index.html")
}

// pageHref is the address of one page of one theme × locale directory.
// Every link this renderer emits into the tree is built here or by one
// of the two functions beside it (shellHref, modalHref), so the mount
// path is spelled once.
func pageHref(mount, theme, locale, file string) string {
	return mount + "/" + theme + "/" + locale + "/" + file
}

// localeLinks is the language switcher: the twelve shipped locales, each
// labelled with its own autonym out of its own catalog, each carrying
// its own lang and dir so a screen reader switches voice to say
// "Gaeilge" in Irish.
//
// Links rather than locale-menu's POST forms, because a static site has
// no /_locale route to post to. The partial itself is on the page, in
// the route family, with the markup a real app uses.
//
// An entry points at the same page in another language: from
// tokens.html to that locale's tokens.html, so choosing a language does
// not also lose your place. A shell demo has no counterpart of its own
// in the gallery, so its switcher sends you back to the overview in the
// language you picked, which is where the switcher is a component worth
// looking at anyway.
func localeLinks(mount, theme, locale, file string) []localeLink {
	catalogs := rastrillo.BaseCatalogs()
	out := make([]localeLink, 0, len(rastrillo.BaseLocales()))
	for _, code := range rastrillo.BaseLocales() {
		href := pageHref(mount, theme, code, file)
		out = append(out, localeLink{
			Code:    code,
			Name:    catalogs[code]["rastrillo.ui.locale_name"],
			Dir:     rastrillo.Dir(code),
			Href:    href,
			Current: code == locale,
		})
	}
	return out
}

func shellViews(mount, theme, locale string) []shellView {
	blurbs := map[string]string{
		"column":  "The plain centred page every scaffolded app starts on: a skip link, a title, and the content column.",
		"topbar":  "Brand, navigation and an account menu across the top, with a footer under the page.",
		"sidebar": "A navigation rail beside the page, collapsing below 800px into a details disclosure. No JavaScript.",
	}
	out := make([]shellView, 0, len(ui.LayoutNames()))
	for _, name := range ui.LayoutNames() {
		id := anchorID("shell", name)
		href := shellHref(mount, theme, locale, name)
		out = append(out, shellView{
			Name:  name,
			ID:    id,
			Href:  href,
			Blurb: proseIn(locale, blurbs[name]),
			// The one preview that frames a page of this tree rather
			// than a document written for it: the shell demos already
			// exist, at their own URLs, and framing the real file is
			// both smaller and more honest than copying it. No Code
			// tab either — a shell's source is a Go template with
			// {{block}} in it, not markup to copy, and the two shell
			// chrome idioms above show the markup it produces.
			Preview: previewView{
				Group: id + "-0",
				Style: previewStyle(heightOf(id)),
				Src:   href,
				Title: proseIn(locale, "The {shell} shell, rendered at full page", "shell", name),
			},
		})
	}
	return out
}

// shellHref is one full-page shell demo's address.
func shellHref(mount, theme, locale, shell string) string {
	return mount + "/" + theme + "/" + locale + "/shells/" + shell + ".html"
}

// modalHref is the modal demo's address — which is the whole point of
// the demo. A modal is its own URL, so the sample that shows one open
// has to be a page you navigate to, not a fragment of a gallery.
func modalHref(mount, theme, locale string) string {
	return mount + "/" + theme + "/" + locale + "/modal.html"
}

// ── Partial samples ──────────────────────────────────────────────────

// buildFamilies renders every sample in samples.go, then sweeps up any
// partial ui defines that no family claims. A partial with no sample is
// a gap in the documentation, not a reason to drop it off the page: it
// gets its own section, its marker comment (so the coverage gate still
// sees it), and a visible note saying it has no sample data yet.
func buildFamilies(mount string, tmpl *template.Template, theme, locale string) ([]familyView, error) {
	claimed := map[string]bool{}
	out := make([]familyView, 0, len(families())+1)
	for _, fam := range families() {
		view := familyView{Title: proseIn(locale, fam.Title), ID: anchorID("family", fam.Title), Blurb: proseIn(locale, fam.Blurb)}
		for _, doc := range fam.Partials {
			if tmpl.Lookup(doc.Name) == nil {
				return nil, fmt.Errorf("samples.go documents %q, which ui does not define", doc.Name)
			}
			claimed[doc.Name] = true
			pv := partialView{Name: doc.Name, ID: anchorID("partial", doc.Name), Marker: marker("partial", doc.Name), Blurb: proseIn(locale, doc.Blurb), Wrap: doc.Wrap}
			for i, s := range doc.States {
				html, err := renderSample(tmpl, doc.Name, i, s, locale)
				if err != nil {
					return nil, fmt.Errorf("%s (%s): %w", doc.Name, s.State, err)
				}
				pv.States = append(pv.States, stateView{
					State: proseIn(locale, s.State),
					Note:  proseIn(locale, s.Note),
					Preview: newPreview(mount, theme, locale,
						fmt.Sprintf("%s-%d", pv.ID, i),
						previewTitle(locale, doc.Name, s.State),
						wrap(doc.Wrap, string(html)), heightOf(pv.ID)),
				})
			}
			view.Partials = append(view.Partials, pv)
		}
		out = append(out, view)
	}

	defined, err := partialNames()
	if err != nil {
		return nil, err
	}
	var orphans []string
	for _, name := range defined {
		if !claimed[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		view := familyView{
			Title: proseIn(locale, "Ungrouped"),
			ID:    anchorID("family", "Ungrouped"),
			Blurb: proseIn(locale, "Partials ui defines that samples.go has not been taught to render yet. They are listed rather than dropped, because a component nobody documented is still a component apps can call."),
		}
		for _, name := range orphans {
			pv := partialView{Name: name, ID: anchorID("partial", name), Marker: marker("partial", name), Blurb: proseIn(locale, "No sample data yet — add one in internal/designsystem/samples.go.")}
			// Many partials guard every optional field, so an empty
			// dict renders something honest. One that does not simply
			// shows its heading and the note above.
			if html, err := renderSample(tmpl, name, 0, sample{Data: map[string]any{}}, locale); err == nil {
				pv.States = append(pv.States, stateView{
					State: proseIn(locale, "Rendered from an empty data value"),
					Preview: newPreview(mount, theme, locale, pv.ID+"-0",
						previewTitle(locale, name, "Rendered from an empty data value"),
						string(html), heightOf(pv.ID)),
				})
			}
			view.Partials = append(view.Partials, pv)
		}
		out = append(out, view)
	}
	return out, nil
}

// marker is the coverage comment a section carries. It is built in Go
// and injected as template.HTML on purpose: html/template strips HTML
// comments out of template source during escaping, so a marker written
// literally in indexTemplate would never reach the page — and the two
// coverage gates would pass on an empty page forever.
func marker(kind, name string) template.HTML {
	return template.HTML("<!-- " + kind + ": " + name + " -->")
}

// rawName is the tree name a hand-written sample is parsed under. It is
// derived from the partial it sits beside and its position in that
// partial's state list, so parseRawSamples and buildFamilies agree
// without carrying a shared index around.
func rawName(partial string, state int) string {
	return fmt.Sprintf("ds-raw-%s-%d", partial, state)
}

// parseRawSamples parses every hand-written sample into the tree, ahead
// of any execution. Raw markup goes through the template engine rather
// than straight onto the page so it can call T — the optgroup'd select
// carries the same three catalog strings the partial emits, in whatever
// language the page is in.
func parseRawSamples(tmpl *template.Template) error {
	for _, fam := range families() {
		for _, doc := range fam.Partials {
			for i, s := range doc.States {
				if s.Raw == "" {
					continue
				}
				if _, err := tmpl.New(rawName(doc.Name, i)).Parse(s.Raw); err != nil {
					return fmt.Errorf("parsing the hand-written sample for %s: %w", doc.Name, err)
				}
			}
		}
	}
	return nil
}

// renderSample executes one state — the partial itself, or, for a
// hand-written sample, the template parseRawSamples put in the tree.
func renderSample(tmpl *template.Template, name string, state int, s sample, locale string) (template.HTML, error) {
	var buf strings.Builder
	which, data := name, s.Data
	if s.Raw != "" {
		which = rawName(name, state)
	}
	if s.Build != nil {
		data = s.Build(locale)
	}
	if err := tmpl.ExecuteTemplate(&buf, which, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// ── Preview widgets ──────────────────────────────────────────────────
//
// Every example on this page is shown three ways behind one control:
// framed at a desktop width, framed at a phone's, and as source. The
// frame is the whole of it — the sample is not on this page at all, it
// is a document of its own inside an <iframe>, which is what a
// component gallery has always wanted and what several of this page's
// scars were about:
//
//   - the modal sample's overlay is position: fixed, so rendered in the
//     gallery it covered the gallery. In its own document it is fixed
//     to its own viewport, which is where the idiom is honest.
//   - the two shell samples are whole page frames with their own <main>
//     landmark, and this page has one. Two documents, two mains.
//   - .rst-form-foot is position: sticky; bottom: 0, so the form save
//     bar stuck to the bottom of the GALLERY and floated over the
//     sample below it. It now sticks to the bottom of its own frame,
//     which is what it does in an app.
//   - every id a sample carries is scoped to its own document, so a
//     field id repeated across two states is no longer a duplicate id
//     on this page. (The outer page stays clean either way; html/template
//     writes a srcdoc's quotes as &#34;, so nothing inside one is markup
//     here.)
//
// The three tabs are radio inputs and their labels, and the panels
// follow from :checked through :has(). No script: the page picks
// Desktop with the checked attribute, and a reader with JavaScript off
// switches tabs exactly like one who has it.

// previewView is one example's widget. Doc and Src are alternatives:
// Doc is a whole document written here and handed to the frame as
// srcdoc, Src is a page of this tree the frame loads instead (the shell
// demos, which are already documents). Source empty means no Code tab —
// only the shell demos, whose source is a Go template and not markup to
// copy.
type previewView struct {
	Group  string       // the radio group's name, unique on the page
	Style  template.CSS // --ds-h and --ds-hm: the frame's virtual height
	Doc    string
	Src    string
	Source string
	Title  string
}

// previewStyle writes one example's two virtual heights. The frame is a
// window, not a fit: a sample taller than its box scrolls inside it,
// and the box carries resize: vertical so a reader can drag it open —
// the frame reads its height back off the box, so dragging really does
// show more of the document rather than more of the box.
//
// Mobile is a quarter taller than desktop, one factor for every
// example rather than a second table of numbers. The same measuring
// drive that fixed the numbers below fixed this: at 390px the tallest
// any sample grows is 1.17× its desktop height — a page header, whose
// title and action stack — and most grow not at all, because a
// component this small has one column either way.
func previewStyle(h int) template.CSS {
	return template.CSS(fmt.Sprintf("--ds-h: %dpx; --ds-hm: %dpx", h, h*5/4))
}

// previewHeights is how tall each example's own document is, in CSS
// pixels at the 1200px desktop width. Measured, not guessed:
// TestPreviewFrameHeightsFitTheirContent (a browser drive) reads every
// frame's content height off a real engine and fails on any box its
// content does not fit in, so these numbers cannot rot quietly. They
// were taken on the English page in the default theme and rounded up
// with a little slack, because twelve languages and three type
// families do not lay out to the same pixel.
//
// A partial with several states takes the tallest of them: one number
// per section keeps the boxes the same size down a column, which reads
// better than each one being exactly its own content and none of them
// lining up.
//
// Some entries are deliberately taller than anything that is on screen
// at rest — the fields whose script opens a panel, the menus, the
// modal. What a reader does with those examples is open them, and a
// box that fits the closed state is a box they cannot use.
//
// A menu's box has a second job, and it is easy to get wrong: dvh
// inside a frame is THE FRAME, so tokens.css's menu cap
// (min(20rem, max(100dvh - 6rem, 8rem))) is computed against these
// numbers and not against the reader's window. The 8rem floor means no
// frame here can collapse a menu to nothing any more — ui's
// TestAMenuOpenedInsideAShortFrameIsStillUsable holds the framework to
// that — but a frame under about 140px still clips an open three-item
// menu, and no frame can show more than 20rem of one. Size a menu's box
// for the state a reader opens, not for the closed one.
//
// The key is the anchor id, not the name, because "dropdown" is both a
// partial and a class idiom and they are not the same height. Being
// wrong here costs a scrollbar or some white space, never a broken
// page.
var previewHeights = map[string]int{
	// The list screen.
	"partial-page-header":        140,
	"partial-list-bar":           190, // room for the filter menu it opens
	"partial-list-bar-search":    110,
	"partial-list-search-submit": 120,
	"partial-list-row-action":    120,
	"partial-seg-tabs":           80,
	"partial-dropdown":           200, // room for the open menu
	"partial-bulk-bar":           190, // room for the actions menu it opens
	"partial-pagination":         110,
	"partial-empty-state":        240,
	// Display.
	"partial-status-pill": 70,
	"partial-badge":       70,
	"partial-meter":       70,
	"partial-person":      95,
	"partial-callout":     160,
	"partial-detail-list": 230,
	"partial-notice":      110,
	"partial-form-error":  90,
	"partial-job-status":  70,
	// Form. The three enhanced fields carry the panel their script
	// opens under them, which is the whole reason to look at them.
	"partial-field":           210,
	"partial-field-text":      210,
	"partial-field-textarea":  260,
	"partial-field-select":    300,
	"partial-field-check":     140,
	"partial-choice-field":    280,
	"partial-field-date":      330,
	"partial-field-time":      330,
	"partial-field-datetime":  330,
	"partial-field-daterange": 360,
	"partial-form-foot":       170,
	"partial-confirm-form":    160,
	// Route.
	"partial-error-page":  390,
	"partial-back-nav":    80,
	"partial-locale-menu": 420, // the open menu at its full 20rem cap; twelve languages scroll inside it
	// The class idioms.
	"idiom-box":           220,
	"idiom-list-grid":     280,
	"idiom-dropdown":      220,
	"idiom-form-layout":   390,
	"idiom-tblock":        270,
	"idiom-modal":         620, // a modal wants a window to be modal over
	"idiom-help":          100,
	"idiom-selbox":        70,
	"idiom-shell-topbar":  250,
	"idiom-shell-sidebar": 400,
	// The three shell demos, which are whole pages.
	// One height for the three, because they sit under one another and
	// the sidebar's rail is the tallest of them.
	// The demo application, framed at the top of the Overview. Taller
	// than the shells because it is a screen with content in it rather
	// than a frame with a sentence in it.
	"demo-app":      780,
	"shell-column":  780,
	"shell-topbar":  780,
	"shell-sidebar": 780,
}

// previewHeight is what an example gets when the table has nothing to
// say about it.
const previewHeight = 220

func heightOf(id string) int {
	if h, ok := previewHeights[id]; ok {
		return h
	}
	return previewHeight
}

// srcdocScripts names the framework scripts one sample needs, by the
// attribute — or, for the menus, the class — each of them boots on. A
// sample that needs none — most of them — gets a document with no
// script in it at all, which is both smaller and the truth about that
// component.
//
// The menu classes are here because light dismiss is the one thing a
// <details> menu cannot do by itself, and a preview that cannot do it
// teaches the opposite of what the shim exists for: a reader who clicks
// away from an open menu in the frame and watches it stay open has been
// told, by the page, that menus do not close. It costs one script tag
// in each frame that holds a menu: 20,202 bytes across the whole tree
// when it was added, 0.13% of it, against a ceiling still 5.3 MiB away.
var srcdocScripts = []struct {
	asset string
	hooks []string
}{
	{"rastrillo.js", []string{"data-poll", "rst-dropdown", "rst-row-menu"}},
	{"select.js", []string{"data-rst-select"}},
	{"datetime.js", []string{"data-rst-date", "data-rst-time", "data-rst-range"}},
}

// srcdoc builds the document one example is previewed in: the tree's own
// stylesheets by absolute path (so the browser has them cached from the
// gallery), the example, and nothing else. No script unless the example
// needs one.
//
// Nothing in here follows the reader's colour scheme, and it cannot.
// color-scheme is an inherited property and a browser does propagate
// the embedder's through an <iframe> — but only into a document that
// has not declared one of its own, and this one links a theme, and
// every theme declares color-scheme: light dark. The declaration wins,
// the propagated value is ignored, and the frame resolves against the
// reader's OS instead of against the gallery. So the gallery paints
// it: gallery.js writes data-theme on each frame's own <html>, on load
// and on every toggle. See its "the previews" section, and the drive
// leg that measured the two apart.
func srcdoc(mount, theme, locale, title, body string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString(`<html lang="` + locale + `" dir="` + rastrillo.Dir(locale) + `">` + "\n")
	b.WriteString("<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	// The same words the frame's title attribute carries. A frame is a
	// document, and a document with no title fails WCAG 2.4.2 — which
	// the a11y gate says out loud, because it scans these documents in
	// the frame rather than only the page around them. It is also what
	// a screen reader announces on entering the frame, so the two names
	// agreeing is the point rather than a coincidence.
	b.WriteString("<title>" + template.HTMLEscapeString(title) + "</title>\n")
	b.WriteString(`<link rel="stylesheet" href="` + mount + `/tokens.css">` + "\n")
	b.WriteString(`<link rel="stylesheet" href="` + mount + `/theme-` + theme + `.css">` + "\n")
	// A component sample gets breathing room; a whole-page sample —
	// a shell frame, the modal's backdrop — fills the frame, because
	// insetting a page inside a page is not what any of them look
	// like. The shells' rail is block-size: 100dvh, so padding under
	// one is also a scrollbar that can never be got rid of.
	b.WriteString("<style>body { padding: 1rem; }\n")
	b.WriteString("body:has(> .rst-shell-topbar, > .rst-shell-sidebar, > .rst-backdrop) { padding: 0; }</style>\n")
	for _, s := range srcdocScripts {
		for _, hook := range s.hooks {
			if strings.Contains(body, hook) {
				b.WriteString(`<script defer src="` + mount + "/" + s.asset + `"></script>` + "\n")
				break
			}
		}
	}
	b.WriteString("</head>\n<body>\n")
	b.WriteString(body)
	// The sink. Every form in a preview is aimed at it (see deaden), so
	// a reader who clicks Save gets the submission a real app would make
	// and a preview that is still on the screen afterwards — instead of
	// a frame navigated to a route this static site does not serve.
	// rastrillo.js's busy rule skips a form whose target is not _self
	// for the same reason, so nothing spins pointlessly either.
	if strings.Contains(body, "<form") {
		b.WriteString("\n<iframe name=\"ds-void\" hidden></iframe>")
	}
	b.WriteString("\n</body>\n</html>\n")
	return b.String()
}

var (
	sampleHref = regexp.MustCompile(`href="([^"]*)"`)
	sampleForm = regexp.MustCompile(`<form\b`)
)

// deaden makes one sample's markup safe to click. The samples are
// written to read like a real application — /posts/1/edit, and a form
// that posts to /posts/1/delete — and this site serves none of those,
// so following one landed on a missing page. Every link that is not
// already a fragment or a page of this tree becomes href="#", which
// goes nowhere and looks like the link it is; every form is aimed at
// the sink iframe srcdoc appends.
//
// A form's ACTION is deliberately left alone — nineteen of them still
// name a real route — because the target is what decides where the
// answer goes, and the target is the sink. The request is made and
// lands nowhere the reader can see, which is closer to what the sample
// says it does than a form posting to "#" would be. It also keeps the
// action a reader reads in the preview the same as the one in the Code
// tab beside it.
//
// Only the LIVE rendering is treated. The Code tab beside it shows the
// sample as it was written, routes and all, because those are the
// hrefs somebody copying this markup wants — a gallery that had quietly
// replaced them with # would be teaching the wrong thing.
func deaden(mount, html string) string {
	out := sampleHref.ReplaceAllStringFunc(html, func(m string) string {
		v := m[len(`href="`) : len(m)-1]
		if v == "" || strings.HasPrefix(v, "#") || strings.HasPrefix(v, mount+"/") {
			return m
		}
		return `href="#"`
	})
	return sampleForm.ReplaceAllString(out, `<form target="ds-void"`)
}

// previewTitle names one preview frame, and the name has to be unique
// on the page. A frame is a document, and a screen reader announces its
// title on the way in; a page of a hundred and ten frames with
// forty-six names between them is a page whose frame list is useless.
// That is WCAG 2.0 A 4.1.2, and axe says so — as an "incomplete" rather
// than a violation only because the gallery is scanned with the frames
// left to their own pass, so the engine can see the duplicates but not
// prove it.
//
// Uniqueness comes from what already distinguishes one preview from
// another: which state of the partial this is, and — for the one name
// that is both a partial and an idiom, dropdown — which section it is
// in. Both halves are existing prose keys, translated in all twelve
// locales like everything else the page says. No new key was invented
// for this, deliberately: a frame title is not the place to spend
// eleven translations.
//
// TestEveryFrameTitleIsUniqueOnThePage holds the result.
func previewTitle(locale, name, qualifier string) string {
	t := proseIn(locale, "{name} sample standalone preview", "name", name)
	if qualifier == "" {
		return t
	}
	return t + " — " + proseIn(locale, qualifier)
}

// newPreview is one example's widget: the source as written, and a
// document holding the same markup with its links deadened.
func newPreview(mount, theme, locale, group, title, source string, height int) previewView {
	return previewView{
		Group:  group,
		Style:  previewStyle(height),
		Doc:    srcdoc(mount, theme, locale, title, deaden(mount, source)),
		Source: source,
		Title:  title,
	}
}

// wrap puts a sample in the container its partial assumes, per the two
// rules under Class idioms: rows go in a list, a form's fields go in a
// form inside a padded box. It runs in Go rather than in the template
// because the wrapper is part of the sample now — it is inside the
// frame, and it is in the source the Code tab shows, which is where a
// reader learns that a field partial does not bring its own <form>.
func wrap(w wrapper, html string) string {
	switch w {
	case wrapList:
		return `<div class="rst-list">` + html + `</div>`
	case wrapForm:
		return `<section class="rst-box"><form class="rst-form" method="post" action="#">` + html + `</form></section>`
	case wrapBox:
		return `<section class="rst-box">` + html + `</section>`
	}
	return html
}

// ── Class idioms ─────────────────────────────────────────────────────

// idiomBlurbs is one English sentence per class idiom, in the page's own
// voice. A missing entry renders no blurb rather than an empty one.
//
// English here is the source AND the prose.go key, exactly as in
// samples.go: adding an idiom means adding its eleven translations, and
// the parity gate says so.
var idiomBlurbs = map[string]string{
	"box":           "The padded section card, and the heading that sits outside it.",
	"list-grid":     "The real data-table vocabulary: the card sets its columns once, rows only choose cells.",
	"dropdown":      "The details/summary menu behind header overflow menus and a list bar's filter, plus an applied filter as a removable chip.",
	"form-layout":   "The classes that give a form its rhythm and its save bar. No partial emits these — they wrap a caller-composed run of fields.",
	"tblock":        "A bordered card whose body reveals only while its switch is on, via :has(). The switch is authoritative; the reveal is a display convenience.",
	"modal":         "A modal is its own URL, not client state: the page underneath, marked inert, with the panel over it and a plain link to close.",
	"help":          "A bordered question mark linking to a help article. Its CSS tooltip is decoration; the link carries its own full-sentence label.",
	"selbox":        "The selection checkbox a list row wears in select mode. Its label restates the row's identity.",
	"shell-topbar":  "The topbar shell's own chrome, as markup. Layout ships it as a whole template.",
	"shell-sidebar": "The sidebar shell's chrome, collapsing below 800px into a details disclosure.",
}

// idiomRules attaches the two rules from docs/site/templates.md to the
// idioms they govern, quoted rather than paraphrased: they are the rules
// people get wrong, and the wording is the part that has been argued
// over.
var idiomRules = map[string]struct{ Title, Body string }{
	"box": {
		"Which card is which",
		"rst-list and rst-card have no padding by design: they hold a run of rows, and each row pads itself. " +
			"Put a form, a paragraph, a strip of links or anything else that is not a row straight into one and it renders flush against the border — the text touching the edge is the tell. " +
			"The padded card for arbitrary content is rst-box, with its heading as a sibling rst-box-head before it.",
	},
	"list-grid": {
		"Screens stack vertically",
		"A screen is a column: page-header, then section-header + card, then the next section-header + card, in reading order. " +
			"Horizontal arrangement is reserved for the idioms that ship it: rst-box-head, rst-field-row, rst-lbar, rst-lrow cells, rst-seg-tabs.",
	},
}

// demoIdiom is one idiom that is also a page of this tree: the words
// its link wears, and the address it goes to.
type demoIdiom struct {
	Label string
	Href  func(mount, theme, locale string) string
}

// demoIdioms are the three idioms whose preview is not the last word on
// them. The two shells are whole page frames, and a frame is better
// seen at the size of a window than at the size of a paragraph; the
// modal's whole claim is that it is a URL, so the demonstration is not
// complete until you have been to it. Each link opens in a new tab —
// the reader is in the middle of a page, and losing their place in it
// is a poor trade for a look at a demo.
var demoIdioms = map[string]demoIdiom{
	"shell-topbar": {
		Label: "Open the topbar shell demo, a dedicated preview.",
		Href:  func(mount, theme, locale string) string { return shellHref(mount, theme, locale, "topbar") },
	},
	"shell-sidebar": {
		Label: "Open the sidebar shell demo, a standalone preview.",
		Href:  func(mount, theme, locale string) string { return shellHref(mount, theme, locale, "sidebar") },
	},
	"modal": {
		Label: "See it live at the URL it belongs to",
		Href:  modalHref,
	},
}

// buildIdioms renders ui.Styleguide in sorted order. The samples are
// complete HTML with no template actions, so they go onto the page as
// they are — the point is that the page shows the same bytes the ui
// tests hold against tokens.css.
//
// An idiom's own heading is an h3, not the h4 a partial gets, and the
// difference is the outline rather than the size. A partial sits inside
// a family, so its heading is one level under the family's h3; an idiom
// sits directly under the "UI primitives" h2 with nothing between, and
// an h4 there skipped a level. It read the same and described a
// structure that was not there — which is the whole of WCAG 1.3.1, and
// what the accessibility gate found the first time it ran.
func buildIdioms(mount string, tmpl *template.Template, theme, locale string) ([]idiomView, error) {
	samples := ui.Styleguide()
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]idiomView, 0, len(names))
	for _, name := range names {
		view := idiomView{
			Name:   name,
			ID:     anchorID("idiom", name),
			Marker: marker("idiom", name),
			Blurb:  proseIn(locale, idiomBlurbs[name]),
		}
		view.Preview = newPreview(mount, theme, locale, view.ID+"-0",
			previewTitle(locale, name, "UI primitives"),
			samples[name], heightOf(view.ID))
		if demo, ok := demoIdioms[name]; ok {
			view.DemoLabel = proseIn(locale, demo.Label)
			view.DemoHref = demo.Href(mount, theme, locale)
		}
		if rule, ok := idiomRules[name]; ok {
			var buf strings.Builder
			err := tmpl.ExecuteTemplate(&buf, "callout", map[string]any{
				"Tone": "info", "Title": proseIn(locale, rule.Title), "Body": proseIn(locale, rule.Body),
			})
			if err != nil {
				return nil, fmt.Errorf("idiom %s rule: %w", name, err)
			}
			view.Rule = template.HTML(buf.String())
		}
		out = append(out, view)
	}
	return out, nil
}

// ── Shell demos ──────────────────────────────────────────────────────

// shellData is what a shell demo page executes against. The three shells
// take no data of their own — every piece of chrome is a block with a
// working default — so this struct exists only for the blocks this page
// overrides.
type shellData struct {
	Locale  string
	Dir     string
	Name    string
	Title   string
	Mount   string
	Index   string
	Locales []localeLink
	Account template.HTML
}

// accountMarkup is the one block whose shape differs between the two
// chrome shells: topbar owns the details/summary and an override
// supplies only the menu body, while sidebar's block is a bare slot in
// the rail. Moving markup between the two needs an edit, which is
// exactly what ui/layouts documents.
var accountMarkup = map[string]template.HTML{
	"topbar": `<a href="#">Profile</a><a href="#">Billing</a><hr><a href="#">Sign out</a>`,
	"sidebar": `<div class="rst-shell__account"><a class="rst-person" href="#">` +
		`<span class="rst-person__av" aria-hidden="true">G</span>` +
		`<span class="rst-person__meta"><span class="rst-person__name">Grace Hopper</span>` +
		`<span class="rst-person__email">grace@example.com</span></span></a></div>`,
}

// renderShell builds one full-page shell demo: ui.Layout's own template,
// its chrome blocks filled with sample links so the frame is visible,
// and a small representative screen in the content hole.
func renderShell(mount, theme, locale, shell string) ([]byte, error) {
	src, ok := ui.Layout(shell)
	if !ok {
		return nil, fmt.Errorf("no shell %q", shell)
	}
	funcs := galleryFuncs(locale)
	funcs["asset"] = func(p string) string {
		name := strings.TrimPrefix(p, "static/")
		if name == "theme.css" {
			name = "theme-" + theme + ".css"
		}
		return mount + "/" + name
	}
	tmpl, err := template.New("designsystem").Funcs(funcs).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(string(src)); err != nil {
		return nil, fmt.Errorf("parsing the %s shell: %w", shell, err)
	}
	if _, err := tmpl.Parse(shellTemplate); err != nil {
		return nil, fmt.Errorf("parsing the shell overrides: %w", err)
	}
	var buf strings.Builder
	err = tmpl.ExecuteTemplate(&buf, "layout", shellData{
		Locale:  locale,
		Dir:     rastrillo.Dir(locale),
		Name:    shell,
		Title:   proseIn(locale, "The {shell} shell", "shell", shell) + " — " + proseIn(locale, "rastrillo design system"),
		Mount:   mount,
		Index:   indexHref(mount, theme, locale),
		Locales: localeLinks(mount, theme, locale, "index.html"),
		Account: accountMarkup[shell],
	})
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ── The modal demo ───────────────────────────────────────────────────

// modalData is what the modal demo page executes against. Self is the
// page's own address, so the panel's nav rail can switch tabs without
// leaving the modal; Index is the gallery, which is where Close goes.
type modalData struct {
	Theme  string
	Locale string
	Dir    string
	Mount  string
	Index  string
	Self   string
}

// renderModal builds one theme × locale modal demo: the idiom at the
// URL the idiom insists on. The markup is ui.Styleguide()["modal"]'s
// own structure — inert backdrop holding the page, overlay and panel
// over it — with the sample's hrefs replaced by addresses this tree
// actually serves, so every link on it resolves.
//
// No scripts: closing is a link, the backdrop is an HTML attribute, and
// there is nothing here to enhance. That is the demonstration — and it
// is why this is the one page in the tree that does not load
// gallery.js. The cost is real and small: a reader who chose Dark in
// the gallery gets the system scheme for as long as this demo is open,
// because the page that would restore it is the page whose whole claim
// is that it runs nothing. The claim is worth more than the
// consistency.
func renderModal(mount, theme, locale string) ([]byte, error) {
	tmpl, err := partialTree(locale)
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(modalTemplate); err != nil {
		return nil, fmt.Errorf("parsing the modal demo: %w", err)
	}
	var buf strings.Builder
	err = tmpl.ExecuteTemplate(&buf, "ds-modal", modalData{
		Theme:  theme,
		Locale: locale,
		Dir:    rastrillo.Dir(locale),
		Mount:  mount,
		Index:  indexHref(mount, theme, locale),
		Self:   modalHref(mount, theme, locale),
	})
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ── The demo application ─────────────────────────────────────────────
//
// The question a first-time reader arrives with is "what does an app
// built with this look like?", and every other page in this tree
// answers a smaller one: what does a token look like, what does a
// partial look like, what does a page frame look like. So the Overview
// frames an application — a dashboard, a list of requests and one
// request open — before it says a word about any of that.
//
// One page with three views inside it rather than three pages, which
// is Paul's choice and the honest constraint of a static tree. The
// views are :target: each has an address of its own, the browser's back
// button walks them, and a reader can copy a link to the detail view
// out of the frame. That is the framework's URL-per-view idiom rendered
// in the only currency a single static file has, and the demo says so
// on the page rather than implying a route it does not have.
//
// The framework's own three scripts load on it, because a real app gets
// them and this is meant to read as a real app — and not one of them
// does any of the switching. TestTheDemoApplicationSwitchesViewsWithNoScript
// drives the whole journey with script execution disabled in the
// engine, which is the strongest form of that claim. gallery.js loads
// too, restoring the colour scheme a reader chose in the gallery,
// exactly as the shell demos do.

// demoShell is the page frame the demo application is built in. Named,
// not derived: a demo has to pick one shell, and the sidebar is the
// richest of the three — a rail of real navigation, a person at its
// foot and a main column — which is what makes it read as an
// application rather than as a page with a border round it. Renaming
// the shell in ui/layouts fails Render() rather than quietly falling
// back to another one; see renderDemo.
const demoShell = "sidebar"

// demoHref is the demo application's address.
func demoHref(mount, theme, locale string) string {
	return mount + "/" + theme + "/" + locale + "/demo.html"
}

// demoData is what the demo page executes against. Self is the page's
// own address, which the language switcher uses so choosing a language
// keeps you in the app; Index is the gallery it came from.
type demoData struct {
	Locale  string
	Dir     string
	Mount   string
	Title   string
	Index   string
	Self    string
	Locales []localeLink
}

// renderDemo builds one theme × locale copy of the demo application.
func renderDemo(mount, theme, locale string) ([]byte, error) {
	src, ok := ui.Layout(demoShell)
	if !ok {
		return nil, fmt.Errorf("no shell %q to build the demo application in", demoShell)
	}
	funcs := galleryFuncs(locale)
	funcs["asset"] = func(p string) string {
		name := strings.TrimPrefix(p, "static/")
		if name == "theme.css" {
			name = "theme-" + theme + ".css"
		}
		return mount + "/" + name
	}
	tmpl, err := template.New("designsystem").Funcs(funcs).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(string(src)); err != nil {
		return nil, fmt.Errorf("parsing the %s shell: %w", demoShell, err)
	}
	if _, err := tmpl.Parse(demoTemplate); err != nil {
		return nil, fmt.Errorf("parsing the demo application: %w", err)
	}
	var buf strings.Builder
	err = tmpl.ExecuteTemplate(&buf, "layout", demoData{
		Locale:  locale,
		Dir:     rastrillo.Dir(locale),
		Mount:   mount,
		Title:   proseIn(locale, "The demo application") + " — " + proseIn(locale, "rastrillo design system"),
		Index:   indexHref(mount, theme, locale),
		Self:    demoHref(mount, theme, locale),
		Locales: localeLinks(mount, theme, locale, "demo.html"),
	})
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// demoView is the widget the Overview frames the demo application in:
// the same preview widget every example on this tree uses, loading the
// real page rather than a copy of it, and with no Code tab — an
// application is not a snippet to paste.
func demoView(mount, theme, locale string) previewView {
	return previewView{
		Group: "demo-app-0",
		Style: previewStyle(heightOf("demo-app")),
		Src:   demoHref(mount, theme, locale),
		Title: proseIn(locale, "The demo application"),
	}
}

// demoCSS is the whole of the demo's own stylesheet, and it is worth
// reading for how little of it there is: everything visible on the page
// is tokens.css, and this is the view switching plus a three-up grid
// for the dashboard's numbers.
//
// The switching, in four rules. The list and the detail view are hidden
// until they are the :target; the dashboard is shown until one of them
// is, which is what makes the address with no fragment land somewhere.
// Written that way round on purpose: where :has() is missing, the only
// rule that drops is the one hiding the dashboard, so the address with
// no fragment still shows the dashboard alone and a targeted view
// stacks under it — two screens at worst, never three, and never a
// blank one.
//
// The rail's current item is the same trick. A real rastrillo app
// renders each view at its own route and puts aria-current on the link
// server-side; a single document cannot know which view is showing
// without asking CSS, so the marker here is visual — background, colour
// and weight, the three signals tokens.css uses for aria-current — and
// the screen's own <h1> is what actually tells a reader where they are.
const demoCSS = `
#view-requests, #view-request { display: none; }
#view-requests:target, #view-request:target { display: block; }
body:has(#view-requests:target) #view-dashboard,
body:has(#view-request:target) #view-dashboard { display: none; }
body:not(:has(#view-requests:target, #view-request:target)) .rst-shell__nav a[href="#view-dashboard"],
body:has(#view-requests:target) .rst-shell__nav a[href="#view-requests"],
body:has(#view-request:target) .rst-shell__nav a[href="#view-requests"] { background: var(--rst-accent-soft); color: var(--rst-accent); font-weight: 600; }
.app-stats { display: grid; gap: var(--rst-sp-3); grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr)); margin-block-end: var(--rst-sp-5); }
.app-stats > .rst-box { margin: 0; }
.app-stat__n { font-size: 1.9rem; font-weight: 650; line-height: 1.1; margin: 0; }
.app-stat__l { color: var(--rst-text-muted); font-size: var(--rst-fs-sm); margin: 0.2rem 0 0; }
`

// demoTemplate fills every block demoShell leaves open, and then the
// content hole with the three views.
//
// The line between what is translated and what is not runs through the
// grid, not around it. A ROW is data — a person's name, a subject, a
// date, a queue, the app's own brand — and stays English on every copy,
// the same rule the shell demos follow: translating a person's name
// would be a stranger thing to do than leaving it. The HEADER over that
// row is not data; it is the application naming its own columns, so
// Subject, Status and Updated are prose keys like every screen title,
// control and status word on the page. They were fixtures for one
// review round, which put an English table header in the middle of the
// fully localised frame that is a Japanese reader's first sight of this
// system.
const demoTemplate = `
{{define "head"}}<script src="{{.Mount}}/gallery.js"></script>
<style>` + demoCSS + `</style>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
{{define "brand"}}<a class="rst-shell__brand" href="#view-dashboard">Harbour</a>{{end}}
{{define "nav"}}<a href="#view-dashboard">{{P "Dashboard"}}</a><a href="#view-requests">{{P "Requests"}}</a>{{end}}
{{define "locale"}}<details class="rst-dropdown rst-locale" name="rst-menus"><summary>{{T "rastrillo.ui.shell_language"}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span></summary><div class="rst-dropdown__menu">{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>{{end}}
{{define "account"}}<div class="rst-shell__account"><a class="rst-person" href="#view-dashboard"><span class="rst-person__av" aria-hidden="true">A</span><span class="rst-person__meta"><span class="rst-person__name">Ada Lovelace</span><span class="rst-person__email">ada@example.com</span></span></a></div>{{end}}
{{define "content"}}
<section class="app-view" id="view-dashboard">
{{template "page-header" dict "Title" (P "Dashboard") "Sub" (P "Everything the team has waiting this morning.")}}
<div class="app-stats">
<section class="rst-box"><p class="app-stat__n">24</p><p class="app-stat__l">{{P "Open requests"}}</p></section>
<section class="rst-box"><p class="app-stat__n">6</p><p class="app-stat__l">{{P "Waiting on us"}}</p></section>
<section class="rst-box"><p class="app-stat__n">41</p><p class="app-stat__l">{{P "Resolved this week"}}</p></section>
</div>
<div class="rst-box-head"><h2>{{P "Mailbox storage"}}</h2></div>
<section class="rst-box">{{template "meter" dict "Percent" 82 "Text" "412 / 500"}}</section>
<div class="rst-box-head"><h2>{{P "Latest activity"}}</h2><a class="rst-btn" href="#view-requests">{{P "Requests"}}</a></div>
<div class="rst-card" style="--rst-cols: minmax(0, 1fr) 120px">
<div class="rst-lrow rst-lrow--head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Invoice #4471 never arrived<small>Fiona Reid · 09:12</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Card declined on renewal<small>Otto Neurath · 08:40</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Seat count is wrong on the invoice<small>Hedy Lamarr · 11 August</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span></div>
</div>
</section>

<section class="app-view" id="view-requests">
{{template "page-header" dict "Title" (P "Requests") "Sub" (P "Every request in the queue, newest first.") "ActionHref" "#view-requests" "ActionLabel" (P "New request") "ActionIcon" "plus"}}
{{template "seg-tabs" dict "Label" (P "Requests") "Items" (list (dict "Label" (P "All") "Href" "#view-requests" "Current" true) (dict "Label" (P "Open") "Href" "#view-requests") (dict "Label" (P "Resolved") "Href" "#view-requests"))}}
<div class="rst-card" style="--rst-cols: minmax(0, 1fr) 120px 120px">
{{template "list-bar" dict "SearchAction" "#view-requests" "Placeholder" (P "Search requests")}}
<div class="rst-lrow rst-lrow--head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span><span class="rst-m-hide">{{P "Updated"}}</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Invoice #4471 never arrived<small>Fiona Reid · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span><span class="rst-cell-mut rst-m-hide">09:12</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Card declined on renewal<small>Otto Neurath · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">08:40</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Export takes twenty minutes<small>Mary Sherman · Data</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">12 August</span></div>
<div class="rst-lrow"><a class="rst-nm" href="#view-request">Seat count is wrong on the invoice<small>Hedy Lamarr · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span><span class="rst-cell-mut rst-m-hide">11 August</span></div>
</div>
<p class="rst-count-line">{{P "{shown} of {total} requests" "shown" "1–4" "total" "24"}}</p>
{{template "pagination" dict "Items" (list (dict "Label" "1" "Current" true) (dict "Label" "2" "Href" "#view-requests") (dict "Label" "3" "Href" "#view-requests"))}}
</section>

<section class="app-view" id="view-request">
{{template "back-nav" dict "Href" "#view-requests" "Label" (P "Requests")}}
{{template "page-header" dict "Title" "Invoice #4471 never arrived" "Sub" (P "Reported by {person}, and still waiting on us." "person" "Fiona Reid")}}
<p>{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}} {{template "badge" dict "Label" "Billing"}}</p>
<div class="rst-box-head"><h2>{{P "Details"}}</h2></div>
<section class="rst-box">{{template "detail-list" dict "Items" (list (dict "Label" (P "Reference") "Value" "REQ-4471" "Mono" true) (dict "Label" (P "Reported by") "Value" "fiona@example.com") (dict "Label" (P "Queue") "Value" "Billing") (dict "Label" (P "Opened") "Value" "14 August, 09:12"))}}</section>
<div class="rst-box-head"><h2>{{P "Reply"}}</h2></div>
<section class="rst-box"><form class="rst-form" method="post" action="#view-request">
{{template "field-textarea" dict "Name" "reply" "Label" (P "Your reply") "Rows" 4 "Hint" (P "The person who reported this gets it by email.")}}
{{template "form-foot" dict "Submit" (P "Send reply") "CancelHref" "#view-requests" "CancelLabel" (P "Cancel")}}
</form></section>
{{template "callout" dict "Tone" "info" "Title" (P "Three screens, three addresses") "Body" (P "Each view here has an address of its own, the way a rastrillo app gives every screen a URL. Turn JavaScript off and this page behaves exactly the same: the switching is CSS reading the address bar.")}}
</section>
{{end}}
`

// ── The page itself ──────────────────────────────────────────────────

// dsCSS is the page's own chrome: the swatch grid, the sample frames and
// the section rhythm. It is deliberately small and deliberately not in
// tokens.css — none of it is vocabulary an app should reach for, and
// tokens.css shipping a class only one page uses is how a stylesheet
// starts to rot. Every value here is a token, so the page's own
// furniture changes theme with everything else.
const dsCSS = `
.ds-head { border-top: 1px solid var(--rst-line); margin: var(--rst-sp-6) 0 var(--rst-sp-3); padding-top: var(--rst-sp-5); }
.ds-head h2 { font-size: 1.35rem; letter-spacing: -0.01em; margin: 0; }
.ds-lead { color: var(--rst-text-muted); margin: 0 0 var(--rst-sp-4); max-width: 62ch; }
.ds-sub { font-size: var(--rst-fs-base); margin: var(--rst-sp-5) 0 var(--rst-sp-2); }
.ds-switch { align-items: center; display: flex; flex-wrap: wrap; gap: var(--rst-sp-3); margin: var(--rst-sp-4) 0; }
.ds-chrome { align-items: center; border-block-end: 1px solid var(--rst-line); display: flex; flex-wrap: wrap; gap: var(--rst-sp-3); justify-content: flex-end; padding: var(--rst-sp-2) var(--rst-sp-4); }
.ds-chrome .rst-dropdown { position: relative; }
.ds-chrome .rst-dropdown > summary { padding-block: 0.25rem; }
/* The toggle is hidden until gallery.js says it is there. With scripts
   off it never appears, and the page stays on the theme's own
   color-scheme: light dark — which is the System position anyway, so
   nothing is lost but a control that could not have worked. The
   attribute is set while the head is still parsing, so this is not a
   reveal the reader sees happen. */
.ds-scheme { display: none; }
:root[data-rst-js] .ds-scheme { align-items: stretch; border: 1px solid var(--rst-line); border-radius: var(--rst-radius-sm); display: inline-flex; overflow: hidden; }
.ds-scheme button { background: none; border: 0; box-sizing: border-box; color: var(--rst-text-muted); cursor: pointer; font: inherit; font-size: var(--rst-fs-sm); padding: 0.25rem 0.6rem; }
.ds-scheme button + button { border-inline-start: 1px solid var(--rst-line); }
.ds-scheme button:hover { background: var(--rst-accent-soft); color: var(--rst-text); }
.ds-scheme button[aria-pressed="true"] { background: var(--rst-accent-soft); color: var(--rst-accent); font-weight: 600; }
.ds-scheme button:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: -2px; }
.ds-family { margin: var(--rst-sp-6) 0 0; }
.ds-family > h3 { border-block-end: 1px solid var(--rst-line); font-size: 1.05rem; margin: 0 0 var(--rst-sp-2); padding-block-end: var(--rst-sp-2); }
.ds-partial { margin: var(--rst-sp-5) 0; }
.ds-partial > :is(h3, h4) { font-size: var(--rst-fs-base); margin: 0 0 var(--rst-sp-1); }
.ds-sample { background: var(--rst-surface-2); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); margin: var(--rst-sp-3) 0; padding: var(--rst-sp-4); }
.ds-state { color: var(--rst-text-faint); font-size: var(--rst-fs-xs); font-weight: 650; letter-spacing: 0.06em; margin: 0 0 var(--rst-sp-3); text-transform: uppercase; }
.ds-note { border-inline-start: 2px solid var(--rst-line-strong); color: var(--rst-text-muted); font-size: var(--rst-fs-sm); margin: var(--rst-sp-3) 0 0; max-width: 62ch; padding-inline-start: var(--rst-sp-3); }
.ds-toks { display: grid; gap: var(--rst-sp-3); grid-template-columns: repeat(auto-fill, minmax(13rem, 1fr)); list-style: none; margin: 0 0 var(--rst-sp-5); padding: 0; }
.ds-tok { align-items: center; display: flex; gap: var(--rst-sp-3); min-inline-size: 0; }
.ds-tok__text { min-inline-size: 0; }
.ds-tok__name { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ds-tok__value { color: var(--rst-text-muted); display: block; }
.ds-chip { background: var(--rst-surface); border: 1px solid var(--rst-line-strong); border-radius: var(--rst-radius-sm); block-size: 2.25rem; flex: none; inline-size: 2.25rem; }
.ds-chip--fill { background: var(--rst-accent); border-color: var(--rst-accent); }
.ds-bar { background: var(--rst-accent); block-size: 1.25rem; border-radius: 2px; flex: none; }
.ds-type { display: block; flex: none; inline-size: 3.25rem; line-height: 1.15; text-align: center; }
.ds-swatch-note { color: var(--rst-text-muted); font-size: var(--rst-fs-sm); margin: 0 0 var(--rst-sp-4); max-width: 62ch; }
/* The Overview's opening paragraph and the routes under it. The
   paragraph is the one piece of copy on this site somebody wrote by
   hand rather than derived from the code, so it is set a step larger
   than body text and given the same 62ch measure everything else here
   reads at. */
.ds-intro { font-size: 1.05rem; margin: 0 0 var(--rst-sp-5); max-width: 62ch; }
.ds-routes { display: grid; gap: var(--rst-sp-4); list-style: none; margin: 0 0 var(--rst-sp-5); padding: 0; }
.ds-routes > li { border-inline-start: 2px solid var(--rst-line-strong); padding-inline-start: var(--rst-sp-3); }
.ds-routes a { font-weight: 600; }
.ds-routes span { color: var(--rst-text-muted); display: block; font-size: var(--rst-fs-sm); max-width: 62ch; }
/* Prev/next at the foot. Two grid columns rather than a flex row with
   space-between, because the ends of the sequence are missing a link
   and not missing a column: the Overview's Next has to stay on the
   inline end rather than sliding across to where Previous would have
   been. */
.ds-updown { border-block-start: 1px solid var(--rst-line); display: grid; gap: var(--rst-sp-3); grid-template-columns: 1fr 1fr; margin-block-start: var(--rst-sp-6); padding-block-start: var(--rst-sp-4); }
.ds-updown__prev { grid-column: 1; justify-self: start; }
.ds-updown__next { grid-column: 2; justify-self: end; text-align: end; }
.ds-shell { margin: var(--rst-sp-5) 0; }
.ds-shell h3 { font-size: 1.05rem; margin: 0 0 var(--rst-sp-1); }
.ds-src { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); margin: var(--rst-sp-3) 0; overflow-x: auto; padding: var(--rst-sp-4); }
.ds-src code { white-space: pre; }

/* The preview widget: [Desktop] [Mobile] [Code] over one frame.

   No script anywhere in it. The tabs are three radio inputs sharing a
   name, hidden but focusable inside their labels, and :has() reads
   which one is checked from the wrapper — so the panels switch on the
   browser's own form behaviour and a reader with JavaScript off has
   the same three views as everyone else.

   The scale is the other half. The frame is laid out at a virtual
   1200px (or 390px, on Mobile) and scaled to fit whatever the reader's
   column actually is, so the desktop rendering is the desktop
   rendering on a phone too. CSS can divide two lengths into a plain
   number exactly one way — tan(atan2(a, b)) — and 100cqw is the
   container's own width, so --ds-k is that fraction and min() stops it
   scaling anything UP. Where the trig functions are missing the
   @supports block never applies, --ds-k stays 1, and the frame is a
   1200px page clipped to the column: smaller, not broken.

   --ds-h is the frame's virtual height, written per example by
   previewStyle. It sizes the BOX; the frame takes its height back off
   the box, 100% / --ds-k, which is the same number until a reader
   drags the resize grip and then is whatever they dragged it to. Doing
   it the other way round — a fixed height on the frame — gave the grip
   nothing to move: the box grew and the document inside it stayed the
   size it was, leaving 300px of empty box under a 255px rendering.

   The box is a window on a document, not a fit to it: a taller sample
   scrolls inside its frame, and the grip is there for the ones a
   reader wants more of. */
.ds-view { --ds-w: 1200px; --ds-h: 220px; --ds-hm: 330px; margin: var(--rst-sp-3) 0; }
.ds-view__tabs { border: 0; display: flex; margin: 0 0 var(--rst-sp-2); padding: 0; }
.ds-view__tab { align-items: center; border: 1px solid var(--rst-line); color: var(--rst-text-muted); cursor: pointer; display: inline-flex; font-size: var(--rst-fs-sm); padding: 0.2rem 0.7rem; }
.ds-view__tab + .ds-view__tab { border-inline-start: 0; }
.ds-view__tab:first-of-type { border-end-start-radius: var(--rst-radius-sm); border-start-start-radius: var(--rst-radius-sm); }
.ds-view__tab:last-of-type { border-end-end-radius: var(--rst-radius-sm); border-start-end-radius: var(--rst-radius-sm); }
.ds-view__tab input { block-size: 1px; clip-path: inset(50%); inline-size: 1px; margin: 0; overflow: hidden; position: absolute; }
.ds-view__tab:hover { background: var(--rst-accent-soft); color: var(--rst-text); }
.ds-view__tab:has(input:checked) { background: var(--rst-accent-soft); color: var(--rst-accent); font-weight: 600; }
.ds-view__tab:has(input:focus-visible) { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
.ds-view__stage { container-type: inline-size; }
.ds-view__box { --ds-k: 1; background: var(--rst-bg); block-size: calc(var(--ds-h) * var(--ds-k)); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); inline-size: calc(var(--ds-w) * var(--ds-k)); margin-inline: auto; max-inline-size: 100%; overflow: hidden; position: relative; resize: vertical; }
.ds-view__frame { block-size: calc(100% / var(--ds-k)); border: 0; inline-size: var(--ds-w); left: 0; position: absolute; top: 0; transform: scale(var(--ds-k)); transform-origin: top left; }
@supports (inline-size: calc(1px * tan(atan2(1px, 2px)))) {
  .ds-view__box { --ds-k: min(1, tan(atan2(100cqw, var(--ds-w)))); }
}
.ds-view:has(.ds-view__tab--m input:checked) .ds-view__box { --ds-h: var(--ds-hm); --ds-w: 390px; }
.ds-view__code { display: none; margin-block-start: 0; }
.ds-view:has(.ds-view__tab--c input:checked) .ds-view__stage { display: none; }
.ds-view:has(.ds-view__tab--c input:checked) .ds-view__code { display: block; }

/* The sidebar. The rail itself is the framework's own sidebar shell —
   rst-shell-sidebar, rst-shell__rail, rst-shell__nav, and the details
   chrome strip that collapses the rail below 800px — so the page that
   documents the vocabulary is laid out in it. What is left here is the
   two things a rail of links has no class for yet: a collapsible
   section per group, and the filter over them. */
.ds-rail { gap: var(--rst-sp-2); }
.ds-nav > details > summary { align-items: center; color: var(--rst-text-faint); cursor: pointer; display: flex; font-size: var(--rst-fs-xs); font-weight: 650; gap: 0.35rem; letter-spacing: 0.06em; list-style: none; padding-block: 0.35rem; text-transform: uppercase; }
.ds-nav > details > summary::-webkit-details-marker { display: none; }
/* The disclosure glyph is the framework's own chevron: the vendored
   Lucide chevron-down inside a .rst-caret, which tokens.css already
   flips 180 degrees on [open] and already stills under
   prefers-reduced-motion. It replaces content: "\25b8"/"\25be", two
   geometric-shape characters that were the only arrow in the whole
   vocabulary not drawn by the icon set — and that render as an emoji
   triangle on some platforms and as nothing at all where the font has
   no glyph for them. It is aria-hidden because it sits beside the
   section's own name. */
.ds-nav > details > summary > .rst-caret { flex: none; }
.ds-nav > details[aria-current] > summary { color: var(--rst-accent); }
/* A section with nothing to list yet is a link to its page rather than
   a disclosure over an empty box. It wears the summary's typography so
   the rail still reads as one list. */
.ds-nav__page { color: var(--rst-text-faint); font-size: var(--rst-fs-xs); font-weight: 650; letter-spacing: 0.06em; padding-block: 0.35rem; text-transform: uppercase; }
.ds-nav__page[aria-current] { color: var(--rst-accent); }
.ds-nav__page:hover { color: var(--rst-text); }
.ds-nav > details > summary:hover { color: var(--rst-text); }
.ds-nav > details > summary:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
.ds-nav .ds-nav__group { color: var(--rst-text-faint); font-size: var(--rst-fs-xs); font-weight: 550; letter-spacing: 0.05em; margin-block-start: var(--rst-sp-2); text-transform: uppercase; }
/* The filter hides a link by setting [hidden] on it, and the shell
   paints every rail link display: block — which the browser's own
   [hidden] rule loses to. This selector outranks it rather than
   shouting !important at it. */
.rst-shell-sidebar .ds-nav a[hidden], .ds-nav details[hidden] { display: none; }
/* The same scriptless story as the scheme toggle above: no script, no
   filter, and no box sitting there looking like one. The nav under it
   is a complete list of every anchor on the page either way. */
.ds-search { display: none; }
:root[data-rst-js] .ds-search { display: block; }
.ds-search input { background: var(--rst-bg); border: 1px solid var(--rst-line-strong); border-radius: var(--rst-radius-sm); box-sizing: border-box; color: var(--rst-text); font: inherit; font-size: var(--rst-fs-sm); inline-size: 100%; padding: 0.35rem 0.5rem; }
.ds-search input:focus-visible { border-color: var(--rst-accent); outline: 2px solid var(--rst-accent); outline-offset: -1px; }
.ds-nav__empty { color: var(--rst-text-muted); font-size: var(--rst-fs-sm); margin: var(--rst-sp-3) 0 0; }
`

// pageTemplate is the frame every page of a <theme>/<locale> directory
// is drawn in: the head, the chrome, the rail, the page header, the
// section tabs — and a hole where the section's own body goes. The
// bodies are the constants under it, one per page kind, and the whole
// of the difference between the five pages is which one is executed
// into .Body and which nav section is current.
//
// Four things in here are load-bearing beyond layout:
//
//   - The marker comments. Every partial section emits
//     <!-- partial: NAME --> and every idiom <!-- idiom: NAME -->, and
//     designsystem_test.go's coverage gates grep for exactly those. A
//     gate that grepped for a class or a rendered string instead would
//     fail every time a partial's markup was tidied, which is the
//     opposite of what it is for. Since the split they land on two
//     different pages, so the gates assert the UNION over a directory
//     rather than the contents of one page: a partial that lands on no
//     page at all is the failure a per-page gate could not see.
//   - Every href and src the page itself owns is absolute under
//     .Mount, because the edge serves this tree's directory indexes
//     without a trailing slash and a relative path resolves against the
//     wrong base there. TestEveryPageIsAWholeLocalisedDocument holds it,
//     and holds every such link to a path the tree actually renders.
//     The hrefs inside the samples are the exception: they are content,
//     sample data written to read like a real app, and they point at
//     routes no static site has.
//   - id="…" data-ds-anchor. Every element the sidebar can link wears
//     the pair, and nothing else does. The attribute is the marker —
//     the same trick as the comments above, for the same reason — so
//     an element that grew an id for its own purposes is not a nav
//     target and a section that lost one is not either. The ids
//     themselves come out of anchorID and are built from English, so
//     the twelve translations of a page carry the same fragments.
//   - The rail is the same on all five pages, byte for byte apart from
//     the current section's `open aria-current="page"`. Every entry in
//     it is an absolute page address with a fragment on the end, the
//     current page's included — a fragment link to the document you are
//     already on is a same-document navigation, so nothing reloads, and
//     one shape for every entry is one shape for the gate to hold.
//
// The frame is the sidebar shell, class for class: rst-shell-sidebar
// holding the details chrome strip, the rail and the main column. That
// is dogfooding with a point to it — the shell is one of the three
// things this page documents, and a gallery built out of something else
// while recommending this would be advertising.
// viewTemplate is the preview widget, once, used by every example on
// the page. Desktop is the checked radio, so a page with no JavaScript
// and no interaction at all still opens on the rendering a reader most
// wants — and the Code tab only exists where there is source worth
// copying, which is everywhere but the shell demos.
const viewTemplate = `{{define "ds-view"}}<div class="ds-view" style="{{.Style}}">
<fieldset class="ds-view__tabs"><legend class="rst-sr-only">{{P "Preview"}}</legend>
<label class="ds-view__tab"><input type="radio" name="{{.Group}}" checked>{{P "Desktop"}}</label>
<label class="ds-view__tab ds-view__tab--m"><input type="radio" name="{{.Group}}">{{P "Mobile"}}</label>
{{if .Source}}<label class="ds-view__tab ds-view__tab--c"><input type="radio" name="{{.Group}}">{{P "Code"}}</label>{{end}}
</fieldset>
<div class="ds-view__stage"><div class="ds-view__box"><iframe class="ds-view__frame" title="{{.Title}}" loading="lazy"{{if .Src}} src="{{.Src}}"{{else}} srcdoc="{{.Doc}}"{{end}}></iframe></div></div>
{{if .Source}}<pre class="ds-src ds-view__code rst-mono"><code>{{.Source}}</code></pre>{{end}}
</div>{{end}}`

const pageTemplate = `{{define "ds-page"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — {{P "rastrillo design system"}} — {{.Theme}}</title>
<link rel="stylesheet" href="{{.Mount}}/tokens.css">
<link rel="stylesheet" href="{{.Mount}}/theme-{{.Theme}}.css">
<style>` + dsCSS + `</style>
<script src="{{.Mount}}/gallery.js"></script>
<script defer src="{{.Mount}}/rastrillo.js"></script>
<script defer src="{{.Mount}}/select.js"></script>
<script defer src="{{.Mount}}/datetime.js"></script>
</head>
<body>
<div class="rst-shell-sidebar">
<a class="rst-skip" href="#main">{{T "rastrillo.ui.shell_skip"}}</a>
<details class="rst-shell__chrome"><summary>{{icon "menu"}}{{T "rastrillo.ui.shell_menu"}}</summary></details>

<aside class="rst-shell__rail ds-rail">
  <search class="ds-search">
    <label class="rst-sr-only" for="ds-filter">{{P "Filter"}}</label>
    <input id="ds-filter" type="search" placeholder="{{P "Filter"}}" autocomplete="off" aria-controls="ds-nav" data-ds-filter>
  </search>
  <p class="ds-nav__empty" data-ds-filter-empty role="status" hidden>{{P "No matches"}}</p>
  <nav class="rst-shell__nav ds-nav" id="ds-nav" aria-label="{{P "Sections and demos"}}">
{{range .Nav}}{{if .Items}}    <details{{if .Current}} open aria-current="page"{{end}}><summary><span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span>{{.Title}}</summary>{{range .Items}}<a href="{{.Href}}"{{if .Aria}} aria-label="{{.Aria}}"{{end}}{{if .Group}} class="ds-nav__group"{{else if .Code}} class="rst-mono"{{end}}{{if .Blank}} target="_blank" rel="noopener"{{end}}>{{.Label}}</a>{{end}}</details>
{{else}}    <a class="ds-nav__page" href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Title}}</a>
{{end}}{{end}}  </nav>
</aside>

<main class="rst-shell__main" id="main">

<header class="ds-chrome">
  <nav class="rst-seg-tabs" aria-label="{{P "Theme"}}">{{range .Themes}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
  <div class="ds-scheme" role="group" aria-label="{{P "Colour scheme"}}">{{range .Schemes}}<button type="button" data-ds-scheme="{{.Value}}" aria-pressed="{{.Pressed}}">{{.Label}}</button>{{end}}</div>
  <details class="rst-dropdown rst-locale" name="rst-menus">
    <summary>{{T "rastrillo.ui.shell_language"}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span><span class="rst-sr-only">{{P ", currently {language}" "language" .LocaleName}}</span></summary>
    <div class="rst-dropdown__menu">{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div>
  </details>
</header>

<div class="rst-page">

<header class="rst-page-header">
  <div class="rst-page-header__titles">
    <h1>{{P "rastrillo design system"}}</h1>
    <p class="rst-page-header__sub">{{.Sub}}</p>
  </div>
</header>

<div class="ds-switch">
  <nav class="rst-seg-tabs" aria-label="{{P "Sections"}}">{{range .Pages}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
</div>

{{.Body}}

<nav class="ds-updown" aria-label="{{P "Previous and next"}}">{{with .Prev}}<a class="ds-updown__prev" href="{{.Href}}">{{.Label}}</a>{{end}}{{with .Next}}<a class="ds-updown__next" href="{{.Href}}">{{.Label}}</a>{{end}}</nav>

</div>
</main>
</div>
</body>
</html>
{{end}}`

// deadLinkCallout is the note every page carrying samples opens with:
// the links inside a preview go nowhere on purpose. It is a constant
// rather than three copies because the three bodies that need it want
// the same sentence, and a fourth page kind that frames anything will
// want it too.
const deadLinkCallout = `{{template "callout" dict "Tone" "info" "Title" (P "Links here are inactive") "Body" (P "Links inactive, sample source provided.")}}`

// overviewBody is the Overview page: the address every visitor lands on
// first, and the reason this file's history has a note in the spec about
// shipping a heading with nothing under it.
//
// Two things on it. Paul's paragraph, which is the whole of what this
// system claims to be, applied word for word — it is his copy, and the
// eleven translations in prose.go are of that sentence and not of an
// improvement on it. Then a route into each of the other pages, read off
// pageKinds() with each row's own Blurb, so a sixth page kind appears
// here the day its row lands.
//
// A page kind with nothing anchored on it yet renders no rail entries,
// which is why galleryNav draws a section with no items as a plain link
// to its page — and why the Overview is the one section of the rail that
// does not carry an overview link of its own: it already is one.
const overviewBody = `{{define "ds-body-overview"}}
<div class="ds-head"><h2 id="overview">{{P "Overview"}}</h2></div>
<p class="ds-intro">{{P "The Rastrillo design system aims to be a starter framework for any app to get a consistent, polished, accessible UI with no or minimal JavaScript dependence, available in multiple languages, and using clean, modern HTML and CSS. It's designed to be delightful to use with or without LLM assistance, and easily remixable."}}</p>
<section class="ds-demo" id="demo-app">
<h3 class="ds-sub">{{P "The demo application"}}</h3>
<p class="ds-lead">{{P "A working application built out of nothing but this system: a dashboard, a list and one record open, each at an address of its own, and nothing on it that needs JavaScript to work."}}</p>
{{template "ds-view" .Demo}}
<p class="ds-note"><a href="{{.DemoHref}}" target="_blank" rel="noopener">{{P "Open the demo application"}}<span class="rst-sr-only"> ({{P "opens in a new tab"}})</span></a>.</p>
</section>
<h3 class="ds-sub">{{P "Where to go next"}}</h3>
<ul class="ds-routes">{{range .Routes}}
<li><a href="{{.Href}}">{{.Label}}</a><span>{{.Blurb}}</span></li>{{end}}
</ul>
{{end}}`

const tokensBody = `{{define "ds-body-tokens"}}
<div class="ds-head"><h2 id="tokens">{{P "Tokens"}}</h2></div>
<p class="ds-lead">{{P "Shared custom properties for all components. Colour and the type are part of the theme (themes/{theme}.css). Type scale and the spacing steps are structure and come from tokens.css." "theme" .Theme}}</p>
<p class="ds-swatch-note">{{P "Light mode shown here, light-dark for all values. WCAG 2.2 AA floors: 4.5:1 for text, 3:1 for control borders. All colours in chips etc pull from the variables."}}</p>
{{range .Colours}}
<h3 class="ds-sub" id="{{.ID}}" data-ds-anchor>{{.Title}}</h3>
<ul class="ds-toks">{{range .Rows}}<li class="ds-tok">{{if .Preview}}<span class="ds-chip" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}
{{range .Structure}}
<h3 class="ds-sub" id="{{.ID}}" data-ds-anchor>{{.Title}}</h3>
<ul class="ds-toks">{{$kind := .Kind}}{{range .Rows}}<li class="ds-tok">{{if eq $kind "type"}}<span class="ds-type" style="{{.Preview}}">Ag</span>{{else if eq $kind "space"}}<span class="ds-bar" style="{{.Preview}}"></span>{{else}}<span class="ds-chip ds-chip--fill" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}
{{end}}`

const componentsBody = `{{define "ds-body-components"}}
<div class="ds-head"><h2 id="components">{{P "Components"}}</h2></div>
<p class="ds-lead">{{P "Components give you pre-built, consistent UI elements, rendered server-side."}}</p>
<p class="ds-note">{{P "The framework's own vocabulary calls these partials: ui.Templates() returns partials, and docs/site/templates.md documents them under that name. The word on this page changed; the code's did not."}}</p>
` + deadLinkCallout + `
<p class="ds-note">{{P "Each sample below in its own frame."}}</p>
<p class="ds-note">{{P "Sample content in English. Sample shells translated."}}</p>
{{range .Families}}
<section class="ds-family" id="{{.ID}}" data-ds-anchor>
<h3>{{.Title}}</h3>
<p class="ds-lead">{{.Blurb}}</p>
{{range .Partials}}
{{.Marker}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
<h4 class="rst-mono">{{.Name}}</h4>
<p class="ds-lead">{{.Blurb}}</p>
{{range .States}}
<div class="ds-sample">
{{if .State}}<p class="ds-state">{{.State}}</p>{{end}}
{{template "ds-view" .Preview}}
{{if .Note}}<p class="ds-note">{{.Note}}</p>{{end}}
</div>
{{end}}
</article>
{{end}}
</section>
{{end}}
{{end}}`

const primitivesBody = `{{define "ds-body-primitives"}}
<div class="ds-head"><h2 id="primitives">{{P "UI primitives"}}</h2></div>
<p class="ds-lead">{{P "The shapes a component cannot be, because they wrap a body only the caller knows: the section card, the data grid, the disclosure menu, the shells' own chrome. tokens.css ships the classes and an app writes the markup. Everything below is the exact sample ui.Styleguide returns, which is the sample ui tests hold against tokens.css."}}</p>
` + deadLinkCallout + `
{{range .Idioms}}
{{.Marker}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
<h3 class="rst-mono">{{.Name}}</h3>
{{if .Blurb}}<p class="ds-lead">{{.Blurb}}</p>{{end}}
{{.Rule}}
<div class="ds-sample">{{template "ds-view" .Preview}}
{{if .DemoHref}}<p class="ds-note"><a href="{{.DemoHref}}" target="_blank" rel="noopener">{{.DemoLabel}}</a><span class="rst-sr-only"> ({{P "opens in a new tab"}})</span>.</p>{{end}}</div>
</article>
{{end}}
{{end}}`

const shellsBody = `{{define "ds-body-shells"}}
<div class="ds-head"><h2 id="shells">{{P "Shells"}}</h2></div>
<p class="ds-lead">{{P "The page frame itself. rastrillo new writes one of these as templates/layout.html; a page fills its content hole and overrides whichever chrome blocks it needs. Each demo below is a whole page at its own URL — open one to see it at full width, where the sidebar's rail and the topbar's footer actually have room."}}</p>
` + deadLinkCallout + `
{{range .Shells}}
<section class="ds-shell" id="{{.ID}}" data-ds-anchor>
<h3>{{.Name}}</h3>
<p class="ds-lead">{{.Blurb}}</p>
{{template "ds-view" .Preview}}
<p><a class="rst-btn" href="{{.Href}}" target="_blank" rel="noopener">{{P "Open the {shell} shell" "shell" .Name}}<span class="rst-sr-only"> ({{P "opens in a new tab"}})</span></a></p>
</section>
{{end}}
{{end}}`

// shellTemplate fills every block the three shells leave open. The
// blocks a given shell does not declare are simply never executed, so
// one override set covers all three.
//
// head is the newest of them and the reason it exists: this demo is a
// real page a reader can open in a tab of its own, and a reader who
// chose Dark in the gallery should still be in Dark when they get
// here. gallery.js reads that choice out of localStorage and applies
// it before the first paint — the same script, on the same origin,
// doing the same job it does on the gallery. It is also the honest
// answer to "what is the head block FOR": an app's favicon, an app's
// stylesheet, an app's one script that has to run early.
const shellTemplate = `
{{define "head"}}<script src="{{.Mount}}/gallery.js"></script>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
{{define "brand"}}<a class="rst-shell__brand" href="{{.Index}}">rastrillo</a>{{end}}
{{define "nav"}}<a href="#" aria-current="page">Posts</a><a href="#">Comments</a><a href="#">Settings</a>{{end}}
{{define "account"}}{{.Account}}{{end}}
{{define "locale"}}<details class="rst-dropdown rst-locale" name="rst-menus"><summary>{{T "rastrillo.ui.shell_language"}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span></summary><div class="rst-dropdown__menu">{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>{{end}}
{{define "foot"}}<a href="{{.Index}}">{{P "Back to the design system"}}</a>{{end}}
{{define "content"}}
{{template "page-header" dict "Title" "Posts" "Sub" (P "A representative screen, so the chrome around it has something to frame.") "ActionHref" "#" "ActionLabel" (P "Write a post") "ActionIcon" "plus"}}
<div class="rst-box-head"><h2>{{P "This page"}}</h2><a class="rst-btn" href="{{.Index}}">{{P "Back to the design system"}}</a></div>
<section class="rst-box"><p>{{P "This is the {shell} shell, one of the three ui.Layout ships. A screen is a column: a page header, then a section heading and its card, then the next one. Everything you see here is the shell, tokens.css and two partials." "shell" .Name}}</p></section>
<div class="rst-box-head"><h2>Recent</h2></div>
<div class="rst-card" style="--rst-cols: 2fr 110px 32px">
<div class="rst-lrow rst-lrow--head"><span>Post</span><span class="rst-m-hide">Status</span><span></span></div>
<div class="rst-lrow"><a class="rst-nm" href="#">Release notes, August<small>Published 2 August</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Published")}}</span><span></span></div>
<div class="rst-lrow"><a class="rst-nm" href="#">Why we moved off the old runner<small>{{P "Draft"}}</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Draft")}}</span><span></span></div>
</div>
<p class="rst-count-line">Displaying <strong>1–2</strong> of <strong>412</strong></p>
{{end}}
`

// modalTemplate is the modal demo page: the sample's structure with
// real addresses. It is a hand-written document rather than one of the
// three shells because the idiom is body-level — the backdrop wraps the
// whole page, and no shell has a block outside its own main.
//
// The three deviations from ui.Styleguide()["modal"], all of them the
// difference between a sample and a page that exists:
//
//   - the backdrop holds a real screen (page-header and a box) instead
//     of a bare <h1>, so there is something visible behind the panel;
//   - the panel's nav rail links this page, so switching tabs keeps the
//     modal open rather than landing on /settings/billing, a 404;
//   - Close returns to the gallery rather than to the backdrop's own
//     screen, which does not have a URL of its own here. The panel says
//     so, rather than leaving a reader to wonder.
//
// The panel is <dialog open>, exactly as the sample is: rendered open,
// never showModal()'d, so it never enters the top layer, ::backdrop
// never paints, and .rst-modal-overlay stays the scrim. Its
// aria-labelledby points at the panel's own <h2>, the same way the
// sample's does — a dialog role with no name is an axe failure, and the
// heading is already the text that names this panel.
const modalTemplate = `{{define "ds-modal"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{P "The modal route"}} — {{P "rastrillo design system"}}</title>
<link rel="stylesheet" href="{{.Mount}}/tokens.css">
<link rel="stylesheet" href="{{.Mount}}/theme-{{.Theme}}.css">
</head>
<body>
<div class="rst-backdrop" inert>
<main class="rst-page" id="main">
{{template "page-header" dict "Title" "Settings" "Sub" (P "The page the modal opened over. It is marked inert, so nothing in here takes focus or reaches a screen reader while the panel is up.")}}
<div class="rst-box-head"><h2>Account</h2></div>
<section class="rst-box"><p>{{P "Modals get their own URL."}}</p></section>
</main>
</div>
<div class="rst-modal-overlay">
  <dialog class="rst-modal-panel" open aria-labelledby="modal-title">
    <nav>
      <a href="{{.Self}}" aria-current="page">Profile</a>
      <a href="{{.Self}}">Billing</a>
      <a href="{{.Self}}">Notifications</a>
    </nav>
    <section>
      <a class="rst-modal-close" href="{{.Index}}" aria-label="{{P "Close settings"}}">✕</a>
      <h2 id="modal-title">Profile</h2>
      <p>{{P "Update the name and photo shown across the account."}}</p>
      <p>{{P "Close link designed to work without JS."}}</p>
      <p>{{P "In an application the ✕ would return you to the screen in the backdrop."}}</p>
      <p><a class="rst-btn" href="{{.Index}}">{{P "Back to the design system"}}</a></p>
    </section>
  </dialog>
</div>
</body>
</html>
{{end}}`
