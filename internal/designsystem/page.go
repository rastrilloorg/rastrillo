package designsystem

import (
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/internal/iconsets"
	"amadan.net/rastrillo/rastrillo/ui"
)

// ── The page model ───────────────────────────────────────────────────
//
// Every URL on a page is an absolute path under mount: an asset is
// /design-system/tokens.<hash>.css (hashed.go), a shell demo is
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

	// Notes are a grouped partial's notes in state order, under its one
	// widget; an ungrouped partial's notes sit under each state's widget
	// instead.
	Notes []groupNote
}

// groupNote is one note under a grouped widget and the state it is
// about. The two are separate elements rather than one string joined
// with ": ", because that joiner is copy no review saw, and an ASCII
// colon is wrong punctuation in ja, zh-Hans and yue.
type groupNote struct {
	State, Note string
}

// familyView is one family of partials — and, since the split, one
// PAGE of the gallery. Key is the family's row in samples.go, which is
// what pageKinds() names the page after and what familyNav matches on;
// Title and Blurb are the page's own heading and its opening sentence,
// already localised.
type familyView struct {
	Key      string
	Title    string
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

	// DocTitle is the <title>, and TitleID the id the page's <h1>
	// carries: the anchor its old body heading had (#tokens,
	// #overview, …), kept so a link to it still lands on the title.
	// The family pages had none and have none.
	DocTitle string
	TitleID  string

	// Body is the section's own markup: the ds-body-<kind> template,
	// executed against this same view and handed back to the frame.
	// template.HTML because it is this package's own output — see
	// renderBody, which is the only thing that ever sets it.
	Body template.HTML

	// Mount is mount, so the template can write an absolute address
	// without knowing it. Assets are the exception: they are linked
	// through the asset func, which knows their hashed names.
	Mount string

	// Home is the Overview's address, which the bar's brand links, and
	// Up a content page's way back: the Overview with this page's own
	// row as the fragment, so focus returns to it even with scripts off.
	// The Overview has no Up; its way up would be itself.
	Home string
	Up   string

	// Rows and Demos are the phone index, the Overview's alone. Below
	// 800px the Overview is the index and every other page a content
	// page, the way the shipped sidebar layout works, and on any other
	// page these would show at no width.
	Rows  []indexRow
	Demos navSection

	// Bar is the theme, scheme and language controls. Every page writes
	// them twice, in the pinned bar for a wide screen and in the display
	// settings menu for a phone, because on a phone the bar's cell is
	// gone and the menu has to sit in the index's header row or the back
	// strip; one copy is ever shown. Both are this one value, so the two
	// cannot list different things.
	Bar controls

	Colours   []tokenGroup
	Structure []tokenGroup
	Families  []familyView
	Idioms    []idiomView
	Shells    []shellView

	// Family is the one family this page IS, where the page is a
	// component family, and nil on every other page. Families beside
	// it stays the whole list on every page, because the rail is the
	// same on all of them and lists all of them; this is the narrowing
	// the body renders. See renderGallery, which is the only thing that
	// sets it.
	Family *familyView

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

	// Icons is the Icons page's whole content and Assets is the Getting
	// started page's. Both are read off the framework rather than
	// written out — IconSlugs() and the embedded asset bytes — so
	// neither page has a list in it to go stale. See iconsView and
	// assetsView.
	Icons  iconsView
	Assets assetsView

	// Screens is the Screens page: whole compositions rather than
	// components, and ScreenPartials the partials it documents. See
	// buildScreens and screenPartials.
	Screens        []screenView
	ScreenPartials []screenPartialView

	// Formats is the Dates, numbers and names page. See buildFormats.
	Formats []formatView

	// Nav is the sidebar: derived from the five fields above it, once
	// they are built, so it cannot list anything the page does not
	// render and cannot miss anything it does. See galleryNav.
	Nav []navSection

	// CopyJoin is copyJoin, handed to gallery.js on the live region so
	// the script writes no punctuation of its own.
	CopyJoin string
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
// Blank marks the entries that leave this document — the demo pages,
// the only off-page links the rail has. They open in a new tab for the
// reason every other demo link on this page does: a reader is in the
// middle of a long page and a filter they typed, and a demo is a
// detour, not a destination.
type navItem struct {
	Label string
	Href  string
	Code  bool
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

	// Terms are the synonyms the filter matches beside the label, from
	// searchTerms; empty for most entries.
	Terms string
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
	Kind    string // the page kind; "" for Demos
	Current bool
	Items   []navItem
}

// ── The page kinds ───────────────────────────────────────────────────
//
// One page per section, each at its own URL under <theme>/<locale>/.
// The table below is the whole of that seam: a row here is a page in
// the tree, an entry in the rail, a row on the phone index and a
// target for both switchers, with nothing else to remember.
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
	kinds := []pageKind{
		{Kind: "overview", File: "index.html", Title: "Overview"},
		{Kind: "getting-started", File: "getting-started.html", Title: "Getting started", Nav: assetNav,
			Blurb: "Details about the stylesheets and scripts that ship with the Rastrillo design system."},
		{Kind: "tokens", File: "tokens.html", Title: "Tokens", Nav: tokenNav,
			Blurb: "Every custom property the system is built out of: the theme's colour and type, and the scales for size, spacing and radius."},
		{Kind: "icons", File: "icons.html", Title: "Icons", Nav: iconNav,
			Blurb: "Every icon slug, at the size components draw it, with the call to copy and its lucide.dev name."},
	}
	// The component families, where the one components page used to be.
	kinds = append(kinds, componentPages()...)
	return append(kinds,
		pageKind{Kind: "primitives", File: "primitives.html", Title: "UI primitives", Nav: primitiveNav,
			Blurb: "The shapes a component cannot be, because they wrap a body only the caller knows: cards, data grids, menus and the shells' own chrome."},
		pageKind{Kind: "formats", File: "formats.html", Title: "Dates, numbers and names", Nav: formatNav,
			Blurb: "How to show dates, numbers, money, names and addresses, and which ones people mix up."},
		pageKind{Kind: "screens", File: "screens.html", Title: "Screens", Nav: screenNav,
			Blurb: "Examples of screens using the design system. You can use these as starter templates for your own apps."},
		pageKind{Kind: "shells", File: "shells.html", Title: "Shells", Nav: shellNav,
			Blurb: "The page frames rastrillo new can scaffold. Each opens full width."},
	)
}

// componentPages is one page per family in samples.go: the split that
// took components.html from 98 preview frames to five pages of roughly
// twenty (16 + 30 + 30 + 14 + 8, counted off the render).
//
// Read off families() rather than written out here, which is what makes
// a family a page and not a page a family. A row added to samples.go is
// a page in the tree, an entry in the rail, a row on the phone index,
// a step in the prev/next sequence and a route off the Overview, with
// nothing to remember on this side — and its Title and Blurb, which samples.go
// already writes as prose keys, are the page's name and the sentence
// the Overview routes to it with. That is why the split cost no new
// English beyond the one family it added.
//
// Kind is the family key, so the file is form.html and the rail entry
// is Form. The keys are not the other kinds' names and cannot become
// them by accident: TestNoTwoPageKindsShareAName holds that.
func componentPages() []pageKind {
	fams := families()
	out := make([]pageKind, 0, len(fams))
	for _, fam := range fams {
		out = append(out, pageKind{
			Kind:  fam.Key,
			File:  fam.Key + ".html",
			Title: fam.Title,
			Blurb: fam.Blurb,
			Nav:   familyNav(fam.Key),
		})
	}
	return out
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

// familyNav is one family page's rail entries: its own partials, by
// name, on its own page.
//
// It closes over the family key rather than reading the page being
// rendered, because the rail is the same on every page — every family's
// section lists that family's partials wherever the reader is standing.
// A key with no family renders an empty section rather than panicking:
// pageKinds() builds both sides off families(), so that cannot happen
// without the table having changed underneath.
func familyNav(key string) func(mount, theme, locale string, view pageView) []navItem {
	return func(mount, theme, locale string, view pageView) []navItem {
		file := fileOf(key)
		var items []navItem
		for _, fam := range view.Families {
			if fam.Key != key {
				continue
			}
			for _, p := range fam.Partials {
				items = append(items, navItem{Label: p.Name, Href: anchorHrefIn(mount, theme, locale, file, p.ID), Code: true})
			}
		}
		return items
	}
}

func primitiveNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("primitives")
	var items []navItem
	for _, idiom := range view.Idioms {
		items = append(items, navItem{Label: idiom.Name, Href: anchorHrefIn(mount, theme, locale, file, idiom.ID), Code: true})
	}
	return items
}

// iconNav is one rail entry per slug, in IconSlugs() order — which is
// the order the page draws them in, because both read the same call.
// Code because a slug is an identifier, the same as a partial's name.
func iconNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("icons")
	var items []navItem
	for _, ic := range view.Icons.List {
		items = append(items, navItem{Label: ic.Slug, Href: anchorHrefIn(mount, theme, locale, file, ic.ID), Code: true})
	}
	return items
}

// assetNav is one rail entry per shipped file, in the order the page
// lists them. Derived from assetViews for the same reason iconNav is
// derived from IconSlugs(): a fourth script joins both lists on the day
// it ships, or neither.
func assetNav(mount, theme, locale string, view pageView) []navItem {
	file := fileOf("getting-started")
	var items []navItem
	for _, a := range view.Assets.List {
		items = append(items, navItem{Label: a.Name, Href: anchorHrefIn(mount, theme, locale, file, a.ID), Code: true})
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
			Kind:    pk.Kind,
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
			for i := range section.Items {
				if _, id, ok := strings.Cut(section.Items[i].Href, "#"); ok {
					section.Items[i].Terms = searchTerms[id]
				}
			}
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
// keyed by path under the directory: the pages, and the preview files
// their frames load. One call rather than five because everything
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
	tmpl.Funcs(template.FuncMap{"asset": assetFunc(mount, theme)})
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
	if _, err := tmpl.Parse(screenFrame); err != nil {
		return nil, fmt.Errorf("parsing the screen frame: %w", err)
	}

	colours, err := themePalette(theme)
	if err != nil {
		return nil, err
	}
	structure, err := structuralGroups()
	if err != nil {
		return nil, err
	}
	files := previewSet{}
	families, err := buildFamilies(mount, tmpl, theme, locale, files)
	if err != nil {
		return nil, err
	}
	idioms, err := buildIdioms(mount, tmpl, theme, locale, files)
	if err != nil {
		return nil, err
	}
	screens, err := buildScreens(mount, theme, locale, tmpl, files)
	if err != nil {
		return nil, err
	}
	formats, err := buildFormats(mount, theme, locale, files)
	if err != nil {
		return nil, err
	}

	localeName := rastrillo.BaseCatalogs()[locale]["rastrillo.ui.locale_name"]
	base := pageView{
		Theme: theme, Locale: locale, Dir: rastrillo.Dir(locale),
		LocaleName:     localeName,
		Mount:          mount,
		Home:           indexHref(mount, theme, locale),
		Colours:        localiseGroups(locale, colours),
		Structure:      localiseGroups(locale, structure),
		Families:       families,
		Idioms:         idioms,
		Formats:        formats,
		Screens:        screens,
		ScreenPartials: screenPartialViews(),
		Shells:         shellViews(mount, theme, locale),
		Icons:          buildIcons(locale),
		Assets:         buildAssets(mount, theme, locale),
		CopyJoin:       copyJoin,
	}

	out := make(map[string][]byte, len(pageKinds())+len(files))
	for _, pk := range pageKinds() {
		view := base
		view.Kind = pk.Kind
		view.Title = proseIn(locale, pk.Title)
		view.Family = familyOf(families, pk.Kind)
		view.DocTitle = docTitle(view.Title, proseIn(locale, "Rastrillo Design System"), theme)
		if view.Family == nil {
			view.TitleID = pk.Kind
		}
		view.Bar = newControls(mount, theme, locale, pk.File, localeName)
		if pk.Kind != "overview" {
			view.Up = view.Home + "#nav-" + pk.Kind
		}
		view.Prev, view.Next = pageSteps(mount, theme, locale, pk.Kind)
		view.Routes = pageRoutes(mount, theme, locale, pk.Kind)
		view.Demo, view.DemoHref = demoView(mount, theme, locale), demoHref(mount, theme, locale)
		// Last, and off the finished view: the rail is a reading of the
		// whole gallery, so it is built once everything it reads exists.
		view.Nav = galleryNav(mount, theme, locale, pk.Kind, view)
		if pk.Kind == "overview" {
			view.Rows, view.Demos = indexRows(view.Nav)
		}

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
	// The preview files share the directory with the pages. A preview
	// whose path a page also takes would replace that page silently.
	for path, doc := range files {
		if _, clash := out[path]; clash {
			return nil, fmt.Errorf("preview %s collides with a page", path)
		}
		out[path] = []byte(doc)
	}
	return out, nil
}

// bodyTemplates is the section bodies, in page order: the constant that
// defines each one, beside the kind it belongs to. The kind is carried
// so the parse error names the page rather than a line number, and so
// TestNoUnregisteredEnglishInThePageTemplates can sweep this list
// rather than a second one written beside it.
func bodyTemplates() []struct{ kind, src string } {
	out := []struct{ kind, src string }{
		{"overview", overviewBody},
		{"getting-started", gettingStartedBody},
		{"tokens", tokensBody},
		{"icons", iconsBody},
		{"primitives", primitivesBody},
		{"formats", formatsBody},
		{"screens", screensBody},
		{"shells", shellsBody},
		// The one family body, under a name no page kind has, so
		// renderBody never reaches it directly.
		{"family", familyBody},
	}
	// Every family page renders that same body against its own
	// pageView.Family. renderBody looks a body up as "ds-body-"+kind,
	// so each family needs a name of its own; the alternative — five
	// copies of a 20-line template, or a {{if eq .Kind}} chain inside
	// one — is five things to keep in step for the same output.
	for _, pk := range componentPages() {
		out = append(out, struct{ kind, src string }{
			pk.Kind,
			fmt.Sprintf(`{{define "ds-body-%s"}}{{template "ds-family" .}}{{end}}`, pk.Kind),
		})
	}
	return out
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
// they are reading: choosing another theme from the form family's page
// lands on that theme's form page, not back at the overview.
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

// controls is one copy of the switchers.
type controls struct {
	Themes     []navLink
	Schemes    []schemeButton
	Locales    []localeLink
	LocaleName string
}

// newControls builds the switchers for one page. Their links carry no
// fragment: gallery.js adds the reader's place at the moment of the
// click, in the bar and in the phone's menu alike. With scripts off a
// switch lands at the top of the page, where the phone's menu is.
func newControls(mount, theme, locale, file, localeName string) controls {
	return controls{
		Themes:     themeLinks(mount, theme, locale, file),
		Schemes:    schemeButtons(locale),
		Locales:    localeLinks(mount, theme, locale, file),
		LocaleName: localeName,
	}
}

// indexRow is one row of the phone index: a section's page, by title,
// carrying the id its pages' back controls name.
type indexRow struct {
	ID, Title, Href string
}

// indexRows reads the phone index off the rail, so the rows and the
// tree cannot list different sections: one row per page kind but the
// Overview, which is the index itself, and the Demos section as is.
// Rows are plain links, not the tree's disclosures, because shell.js
// returns focus to the first nav link whose path is the page just
// left, and in the tree that link is inside a closed <details> that
// cannot take focus. anchorID never writes a nav- id, and the kinds are
// unique, so the ids cannot collide with anything else on the page.
func indexRows(nav []navSection) ([]indexRow, navSection) {
	rows := make([]indexRow, 0, len(nav))
	for _, s := range nav[:len(nav)-1] {
		if s.Kind == "overview" {
			continue
		}
		rows = append(rows, indexRow{ID: "nav-" + s.Kind, Title: s.Title, Href: s.Href})
	}
	return rows, nav[len(nav)-1]
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
		"sidebar": "A navigation rail beside the page. On a phone, the rail is an index page and every other page has a back control. No JavaScript needed.",
		"console": "A bar across the top and a navigation rail down the side at once, the shape most admin consoles are. On a phone, the bar's menu opens as a card and the rail is an index page. No JavaScript needed.",
		"stage":   "One card in the middle of a full-page backdrop, for a screen that stands alone. The sign-in screen is what it is for.",
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
			// both smaller and more honest than copying it. No HTML or
			// Template tab either — a shell's source is a Go template with
			// {{block}} in it, not markup to copy, and the two shell
			// chrome idioms above show the markup it produces.
			Preview: previewView{
				Group: id + "-0",
				Style: previewStyle(id, heightOf(id)),
				Class: previewClass(id),
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

// shellPageHref is the content page of a shell demo that has an index
// (sidebar and console): shells/<shell>.html is the index, the address
// the gallery has always linked, and this is the page its nav opens.
func shellPageHref(mount, theme, locale, shell string) string {
	return mount + "/" + theme + "/" + locale + "/shells/" + shell + "-page.html"
}

// shellHasIndex is the shells whose layout has a view block: their demo
// is two documents, the index and a content page, because a phone shows
// one or the other and one document cannot be both.
func shellHasIndex(shell string) bool { return shell == "sidebar" || shell == "console" }

// modalHref is the modal demo's address — which is the whole point of
// the demo. A modal is its own URL, so the sample that shows one open
// has to be a page you navigate to, not a fragment of a gallery.
func modalHref(mount, theme, locale string) string {
	return mount + "/" + theme + "/" + locale + "/modal.html"
}

// ── Partial samples ──────────────────────────────────────────────────

// familyOf is the family a page kind is, or nil where the page is not a
// family page. The kind IS the family key — see componentPages — so
// this is a lookup and not a second table.
func familyOf(fams []familyView, kind string) *familyView {
	for i := range fams {
		if fams[i].Key == kind {
			return &fams[i]
		}
	}
	return nil
}

// buildFamilies renders every sample in samples.go and holds the table
// to ui: a partial samples.go documents that ui does not define, and a
// partial ui defines that no page claims — no family, and not the
// Screens page's screenPartials — are both errors here.
func buildFamilies(mount string, tmpl *template.Template, theme, locale string, files previewSet) ([]familyView, error) {
	claimed := map[string]bool{}
	out := make([]familyView, 0, len(families()))
	for _, fam := range families() {
		view := familyView{Key: fam.Key, Title: proseIn(locale, fam.Title), Blurb: proseIn(locale, fam.Blurb)}
		for _, doc := range fam.Partials {
			if tmpl.Lookup(doc.Name) == nil {
				return nil, fmt.Errorf("samples.go documents %q, which ui does not define", doc.Name)
			}
			claimed[doc.Name] = true
			pv := partialView{Name: doc.Name, ID: anchorID("partial", doc.Name), Marker: marker("partial", doc.Name), Blurb: proseIn(locale, doc.Blurb), Wrap: doc.Wrap}
			if doc.Row {
				preview, file, err := groupedPreview(mount, theme, locale, fam.Key, tmpl, doc, pv.ID)
				if err != nil {
					return nil, err
				}
				if err := files.add(file); err != nil {
					return nil, err
				}
				pv.States = []stateView{{Preview: preview}}
				for _, s := range doc.States {
					if s.Note != "" {
						pv.Notes = append(pv.Notes, groupNote{State: proseIn(locale, s.State), Note: proseIn(locale, s.Note)})
					}
				}
				view.Partials = append(view.Partials, pv)
				continue
			}
			for i, s := range doc.States {
				html, err := renderSample(tmpl, doc.Name, i, s, locale)
				if err != nil {
					return nil, fmt.Errorf("%s (%s): %w", doc.Name, s.State, err)
				}
				preview, file := newPreview(mount, theme, locale, fam.Key,
					fmt.Sprintf("%s-%d", pv.ID, i),
					previewTitle(locale, doc.Name, s.State),
					wrap(doc.Wrap, string(html)), string(html), pv.ID)
				preview.NoCopy = s.Illustration
				if s.Raw == "" {
					c, err := callFor(doc.Name, s, locale)
					if err != nil {
						return nil, err
					}
					preview.Call = codeview.Highlight(c.Source)
				}
				if w := wrapperMarkup(doc.Wrap); w != "" {
					preview.Wrapper = proseMarkup(locale, "Put this inside {wrapper}.",
						"wrapper", template.HTML("<code>"+template.HTMLEscapeString(w)+"</code>"))
				}
				if err := files.add(file); err != nil {
					return nil, err
				}
				pv.States = append(pv.States, stateView{
					State:   proseIn(locale, s.State),
					Note:    proseIn(locale, s.Note),
					Preview: preview,
				})
			}
			view.Partials = append(view.Partials, pv)
		}
		out = append(out, view)
	}

	// The Screens page is a page too, and it documents the sign-in
	// partials; see screenPartials.
	for _, name := range screenPartials {
		claimed[name] = true
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
	// A partial no family claims used to get an "Ungrouped" section at
	// the foot of the one components page: listed rather than dropped,
	// because a component nobody documented is still a component apps
	// can call.
	//
	// There is no such page any more. A family IS a page, so a partial
	// no page claims — no family, and not screenPartials, the Screens
	// page's own claim — has nowhere to be, and the choices were a page
	// that exists only on the days something is broken or a failure
	// that says so. This is the failure. It is stricter than what it
	// replaces — the old sweep let a partial reach the gallery with no
	// sample and no thought — and it fails at build rather than in a
	// coverage gate, so the message can name the file to edit.
	if len(orphans) > 0 {
		return nil, fmt.Errorf("ui defines %d partial(s) no page of this gallery claims: %s — every partial belongs to a page; add a component to a family in families() in internal/designsystem/samples.go, or a whole screen to screenPartials in internal/designsystem/screens.go",
			len(orphans), strings.Join(orphans, ", "))
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
//   - [rst-form-bar] is position: sticky; bottom: 0, so the form save
//     bar stuck to the bottom of the GALLERY and floated over the
//     sample below it. It now sticks to the bottom of its own frame,
//     which is what it does in an app.
//   - every id a sample carries is scoped to its own document, so a
//     field id repeated across two states is no longer a duplicate id
//     on this page.
//
// The three tabs are radio inputs and their labels, and the panels
// follow from :checked through :has(). No script: the page picks
// Desktop with the checked attribute, and a reader with JavaScript off
// switches tabs exactly like one who has it.

// previewView is one example's widget. Src is the document the frame
// loads: a preview file written for it (newPreview), or a page of this
// tree that already exists (the shell demos, the demo application).
// No Source, no Call and no Rows means neither source tab: only those
// framed pages, whose source is a Go template or a whole application
// rather than markup to copy.
type previewView struct {
	Group string       // the radio group's name, unique on the page
	Style template.CSS // --ds-h and --ds-hm: the frame's virtual height
	// Class is " ds-view--page" on the examples whose document is a
	// whole page, and empty on the rest; see pageFrame. It carries its
	// own leading space so the template can write class="ds-view{{.Class}}"
	// without a conditional.
	Class string
	Src   string
	// Call is the template call the Template tab shows, highlighted;
	// empty where the markup is not a partial's (a hand-written sample,
	// an idiom, a format). Wrapper is the one line above the code, in
	// both tabs, naming the container a wrapped sample needs: the frame
	// wears that container, the code does not, so the reader is told
	// rather than handed a placeholder form to delete.
	Call    template.HTML
	Wrapper template.HTML
	// Source is the markup the HTML tab shows, formatted and
	// highlighted.
	Source template.HTML
	Title  string
	NoCopy bool // the sample is an Illustration: no copy button

	// Rows is a grouped partial's two panels: each state's label, call
	// and rendering. Source is empty there, since no one rendering is
	// the widget's.
	Rows []rowCode
}

// HasHTML says whether the widget draws an HTML tab: it has markup to
// show, as one block or as a grouped partial's rows.
func (v previewView) HasHTML() bool { return v.Source != "" || len(v.Rows) > 0 }

// HasTemplate says whether the widget draws a Template tab: it has a
// call to show, as one block or one per grouped row.
func (v previewView) HasTemplate() bool { return v.Call != "" || len(v.Rows) > 0 }

// previewFile is one example's document as a file of the tree: its path
// under the theme × locale directory, and the document.
type previewFile struct {
	Path string
	Doc  string
}

// rowCode is one state of a grouped partial in its HTML and Template
// panels.
type rowCode struct {
	State        string
	Call, Source template.HTML
}

// rowsStyle lays a grouped preview out as a row that wraps into rows on
// a phone. It is in the document's own <style> because a ds- class
// would need gallery.css inside every preview.
const rowsStyle = "body > ul { display: flex; flex-wrap: wrap; gap: 1rem 1.5rem; list-style: none; margin: 0; padding: 0; }\n" +
	"body > ul > li > p { color: var(--rst-text-muted); font-size: var(--rst-fs-xs); margin: 0 0 0.35rem; }\n"

// groupedPreview is one grouped partial's widget and the file its frame
// loads: every state, labelled, in one frame. The frame title is the
// partial's own, still unique on the page.
//
// It wraps the frame's samples in doc.Wrap but writes no "Put this
// inside" note to either panel, so a grouped partial that needed a
// container would be copied without one. Every grouped partial is an
// inline piece that needs none, and TestAGroupedPartialNeedsNoWrapper
// holds samples.go to that.
func groupedPreview(mount, theme, locale, page string, tmpl *template.Template, doc partialDoc, id string) (previewView, previewFile, error) {
	view := previewView{
		Group: id + "-0",
		Style: previewStyle(id, heightOf(id)),
		Class: previewClass(id) + " ds-view--rows",
		Title: previewTitle(locale, doc.Name, ""),
	}
	var body strings.Builder
	body.WriteString("<ul>")
	for i, s := range doc.States {
		html, err := renderSample(tmpl, doc.Name, i, s, locale)
		if err != nil {
			return previewView{}, previewFile{}, fmt.Errorf("%s (%s): %w", doc.Name, s.State, err)
		}
		c, err := callFor(doc.Name, s, locale)
		if err != nil {
			return previewView{}, previewFile{}, err
		}
		label := proseIn(locale, s.State)
		body.WriteString("<li><p>" + template.HTMLEscapeString(label) + "</p>" + deaden(mount, wrap(doc.Wrap, string(html))) + "</li>")
		view.Rows = append(view.Rows, rowCode{State: label, Call: codeview.Highlight(c.Source), Source: codeview.Highlight(codeview.Format(string(html)))})
	}
	body.WriteString("</ul>")
	file := page + "/" + view.Group + ".html"
	view.Src = pageHref(mount, theme, locale, file)
	return view, previewFile{Path: file, Doc: previewDocStyled(mount, theme, locale, view.Title, rowsStyle, body.String())}, nil
}

// previewSet collects one directory's preview files. A path produced
// twice is a build error rather than an overwrite: two examples sharing
// a radio-group name would show one document in both frames, and every
// other gate would still pass.
type previewSet map[string]string

func (s previewSet) add(f previewFile) error {
	if _, dup := s[f.Path]; dup {
		return fmt.Errorf("preview %s is produced twice", f.Path)
	}
	s[f.Path] = f.Doc
	return nil
}

// pageFrame reports whether one example's document is a whole PAGE
// rather than a component inside one. It is what picks the preview
// widget's width class: the page examples lay out at a virtual 1200px,
// everything else at 900px. gallery.css carries the argument for the
// two numbers; this is only the sorting.
//
// Matched on the anchor id's prefix rather than listed, so the set
// tracks ui rather than being a copy of it. A fifth shell in
// ui.LayoutNames() arrives as shell-<name> and a matching idiom as
// idiom-shell-<name>, and both land here with no edit. The two names
// written out are the two that no rule could derive: the demo
// application, which is this package's own page, and the modal, whose
// whole claim is that it is a page — its backdrop is position: fixed,
// so it is fixed to the frame's viewport and wants a window-sized one.
//
// Getting this wrong costs a rendering, never a broken page: a page
// example sorted as a component is a shell squeezed into 900px, and a
// component sorted as a page is the four-fifths scaling this class
// split exists to remove.
func pageFrame(id string) bool {
	return id == "demo-app" || id == "idiom-modal" ||
		strings.HasPrefix(id, "shell-") || strings.HasPrefix(id, "idiom-shell-")
}

// previewClass is pageFrame as the template writes it.
func previewClass(id string) string {
	if pageFrame(id) {
		return " ds-view--page"
	}
	return ""
}

// previewStyle writes one example's two virtual heights. The frame is a
// window, not a fit: a sample taller than its box scrolls inside it,
// and the box carries resize: vertical so a reader can drag it open —
// the frame reads its height back off the box, so dragging really does
// show more of the document rather than more of the box.
//
// Mobile is a quarter taller than desktop for almost every example,
// one factor rather than a second table of numbers. The same measuring
// drive that fixed the numbers below fixed this: at 390px the tallest
// any sample grows is 1.17× its desktop height — a page header, whose
// title and action stack — and most grow not at all, because a
// component this small has one column either way.
//
// previewMobileHeights is the exception, and the stat band is what
// earned it. The factor holds for components that have one column at
// both widths; it cannot hold for one whose whole shape is a ROW that
// becomes a COLUMN. A four-cell band is 170px of strip on a desktop
// and 439px of stack on a phone — 2.6×, not 1.25 — and there is no
// single factor that fits both that and a status pill. Raising the
// desktop number until the derived mobile one fitted would have put
// 180px of empty box under every desktop rendering of the band, which
// is paying for the phone with the page most readers are on.
//
// Add an entry here only for an example whose layout genuinely changes
// axis. An entry is not a place to absorb a desktop height that was
// measured wrong.
func previewStyle(id string, h int) template.CSS {
	m, ok := previewMobileHeights[id]
	if !ok {
		m = h * 5 / 4
	}
	return template.CSS(fmt.Sprintf("--ds-h: %dpx; --ds-hm: %dpx", h, m))
}

// previewMobileHeights overrides the 1.25× factor for the examples
// whose Mobile layout is taller than the factor allows: one that
// reflows onto a different axis at 390px, and, since Mobile lays out at
// the stage's own width below 390px, one whose text wraps taller at
// the narrowest phone frame, a 320px viewport. Measured by the two
// height drives and held by them: a number too small fails, and the
// slack a number too large leaves is logged.
var previewMobileHeights = map[string]int{
	// Reflows onto a different axis: a row at 900px becomes a column,
	// or several, below 390px.
	"idiom-stat-band":     450, // a four-cell strip becomes three stacked rows
	"partial-status-pill": 170, // the row of four wraps into rows

	// Both axis-reflow AND the worst of the twelve locales wraps taller
	// still at 320px than the factor derived from the 390px reading.
	"partial-badge": 252,
	"partial-meter": 366,

	// Wraps taller at the narrowest phone frame (320px), measured by
	// TestPreviewFrameHeightsFitAtThePhonesNarrowest across all twelve
	// locales.
	"idiom-button":                    399,
	"partial-callout":                 236,
	"partial-error-page":              543,
	"partial-job-status":              113,
	"partial-list-row-action":         299,
	"partial-page-header":             216,
	"partial-pagination":              176,
	"screen-signin-ask":               649,
	"screen-signin-continue":          560,
	"screen-signin-problem-keymail":   693,
	"screen-signin-returning-keymail": 580,
	"screen-signin-returning-link":    544,
	"screen-signin-returning-passkey": 721,
	"screen-signin-sent":              620,
	"screen-signin-sent-instead":      696,
	"screen-signin-sent-unbound":      608,
	"shell-column":                    1010,
	"shell-topbar":                    1135,
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
// partial and a markup idiom and they are not the same height. Being
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
	"partial-status-pill": 100, // grouped: four labelled pills in one row
	"partial-badge":       100, // grouped: five labelled badges in one row
	"partial-meter":       100, // grouped: five labelled meters in one row
	"partial-stat":        150, // a lead cell is label, number, delta and note stacked

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
	"partial-field-url":       210,
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
	// The markup idioms.
	"idiom-button":        240, // four rows: the variants, the sizes and a link, full width, disabled
	"idiom-box":           220,
	"idiom-list-grid":     280,
	"idiom-stat-band":     170,
	"idiom-dropdown":      220,
	"idiom-form-layout":   390,
	"idiom-tblock":        270,
	"idiom-modal":         620, // a modal wants a window to be modal over
	"idiom-help":          100,
	"idiom-selbox":        70,
	"idiom-shell-topbar":  250,
	"idiom-shell-sidebar": 400,
	// The shell demos, which are whole pages.
	// One height for them all, because they sit under one another and
	// the sidebar's rail is the tallest of them.
	// The demo application, framed at the top of the Overview. Taller
	// than the shells because it is a screen with content in it rather
	// than a frame with a sentence in it.
	// The data-format samples. Two are taller than the rest: the ratios
	// section shows a meter and a progress bar in a box each, and the
	// output section is a form.
	"format-ratios": 320,
	"format-output": 260,

	// The sign-in screens. A stage frame is at least as tall as the
	// window it is drawn in (100dvh), so each height here is the card
	// plus the stage's margin around it; the browser gate
	// TestPreviewFrameHeightsFitTheirContent is what holds them to it.
	"screen-signin-ask":               460,
	"screen-signin-returning-keymail": 420,
	"screen-signin-returning-link":    400,
	"screen-signin-returning-passkey": 520,
	"screen-signin-sent":              400,
	"screen-signin-sent-unbound":      400,
	"screen-signin-sent-instead":      440,
	"screen-signin-continue":          400,
	"screen-signin-problem-address":   480,
	"screen-signin-problem-keymail":   520,
	"screen-signin-social":            290,
	"screen-signin-password":          420,

	"demo-app":      700, // demo.html is the index: 680px of dashboard beside the rail at 1200, the phone index well inside the Mobile tab's 875
	"shell-column":  780,
	"shell-topbar":  780,
	"shell-sidebar": 780,
	"shell-console": 780,
	"shell-stage":   780,
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

// previewScripts names the framework scripts one sample needs, by the
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
var previewScripts = []struct {
	asset string
	hooks []string
}{
	{"rastrillo.js", []string{"data-poll", "rst-dropdown", "rst-row-menu"}},
	{"busy.js", []string{"data-busy"}},
	{"select.js", []string{"data-rst-select"}},
	// calendar.js comes FIRST, and the order is load-bearing here in a
	// way it is not on an ordinary page. datetime.js scans the moment it
	// runs, and loaded before calendar.js it finds no factory: every
	// field in the frame enhances without a calendar, and the button
	// falls back to the browser's own picker, the control this overlay
	// exists to replace. Same hooks, because calendar.js draws
	// nothing on its own and a frame needs the pair or neither.
	{"calendar.js", []string{"data-rst-date", "data-rst-time", "data-rst-range"}},
	{"datetime.js", []string{"data-rst-date", "data-rst-time", "data-rst-range"}},
}

// previewDocStyled builds the document one example is previewed in,
// which is written to a file of its own: the tree's own stylesheets by
// absolute path (so the browser has them cached from the gallery), the
// example, and nothing else. No script unless the example needs one.
// style is an extra rule or two the example's own layout needs — a
// grouped preview's row — written into the document's one <style>
// rather than gallery.css, which no preview document loads.
//
// Nothing in here follows the reader's colour scheme, and it cannot.
// color-scheme is an inherited property and a browser does propagate
// the embedder's through an <iframe> — but only into a document that
// has not declared one of its own, and this one links a theme, and
// every theme declares color-scheme: light dark. The declaration wins,
// the propagated value is ignored, and the frame resolves against the
// reader's OS instead of against the gallery. So the gallery paints
// it: gallery.js writes data-theme on each frame's own <html>, once
// each document in the frame is parsed, and on every toggle. See its
// "the previews" section, and the drive leg that measured the two
// apart. A frame can paint before gallery.js reaches it, and shows
// its blank document and its unstyled one before that, so gallery.css
// hides a frame until gallery.js has painted it, in every scheme.
// Without that a reader who chose Dark on a light OS saw each frame
// flash white (TestAFrameNeverShowsTheOtherSchemeWhileItLoads), and so
// did a reader on System in Safari on a dark phone
// (TestAFrameShowsNothingUntilItsDocumentIsStyled).
func previewDocStyled(mount, theme, locale, title, style, body string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString(`<html lang="` + locale + `" dir="` + rastrillo.Dir(locale) + `">` + "\n")
	b.WriteString("<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width">` + "\n")
	// A preview is a fragment, not a page: thousands of them sit under
	// the gallery's directories, and a search engine listing them would
	// send a reader to a lone field with nothing around it.
	b.WriteString(`<meta name="robots" content="noindex">` + "\n")
	// The same words the frame's title attribute carries. A frame is a
	// document, and a document with no title fails WCAG 2.4.2 — which
	// the a11y gate says out loud, because it scans these documents in
	// the frame rather than only the page around them. It is also what
	// a screen reader announces on entering the frame, so the two names
	// agreeing is the point rather than a coincidence.
	b.WriteString("<title>" + template.HTMLEscapeString(title) + "</title>\n")
	b.WriteString(`<link rel="stylesheet" href="` + assetURL(mount, "tokens.css") + `">` + "\n")
	b.WriteString(`<link rel="stylesheet" href="` + assetURL(mount, "theme-"+theme+".css") + `">` + "\n")
	// A component sample gets breathing room; a whole-page sample —
	// a shell frame, the modal's backdrop, a stage frame — fills the
	// frame, because insetting a page inside a page is not what any of
	// them look like. The shells' rail and the stage are 100dvh tall,
	// so padding under one is also a scrollbar that can never be got
	// rid of.
	b.WriteString("<style>body { padding: 1rem; }\n")
	b.WriteString("body:has(> [rst-shell-topbar], > [rst-shell-sidebar], > [rst-backdrop], > [rst-stage]) { padding: 0; }\n")
	b.WriteString(style)
	b.WriteString("</style>\n")
	for _, s := range previewScripts {
		for _, hook := range s.hooks {
			if strings.Contains(body, hook) {
				b.WriteString(`<script defer src="` + assetURL(mount, s.asset) + `"></script>` + "\n")
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
	// busy.js's busy rule skips a form whose target is not _self
	// for the same reason, so nothing spins pointlessly either.
	if strings.Contains(body, "<form") {
		b.WriteString("\n<iframe name=\"ds-void\" hidden></iframe>")
	}
	b.WriteString("\n</body>\n</html>\n")
	return b.String()
}

// previewDoc is previewDocStyled with no style of the example's own.
func previewDoc(mount, theme, locale, title, body string) string {
	return previewDocStyled(mount, theme, locale, title, "", body)
}

var (
	sampleHref = regexp.MustCompile(`href="([^"]*)"`)
	sampleForm = regexp.MustCompile(`<form\b`)
	// A start tag, quoted values whole so a ">" or an attribute's name
	// inside one is not mistaken for the tag's own, and one attribute in
	// it, with the space before it.
	startTag = regexp.MustCompile(`<[a-zA-Z](?:[^>"']|"[^"]*"|'[^']*')*>`)
	tagAttr  = regexp.MustCompile(`\s+([^\s=/>"']+)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>"']+))?`)
)

// unfocus takes autofocus off every element of a sample, for deaden. A
// preview is only ever shown in a frame, and the page around a
// same-origin frame grants its autofocus: focus left the gallery for
// the frame and the gallery scrolled to it, so a reader scrolling the
// screens page was pulled down to a sign-in sample as its lazy frame
// loaded. The HTML tab keeps the attribute, because it belongs to the
// markup being taught. The shell demos are framed whole and opened on
// their own as well, so they keep theirs and gallery.js refuses it in
// a frame.
func unfocus(html string) string {
	return startTag.ReplaceAllStringFunc(html, func(tag string) string {
		return tagAttr.ReplaceAllStringFunc(tag, func(attr string) string {
			if strings.EqualFold(tagAttr.FindStringSubmatch(attr)[1], "autofocus") {
				return ""
			}
			return attr
		})
	})
}

// deaden makes one sample's markup safe to click. The samples are
// written to read like a real application — /posts/1/edit, and a form
// that posts to /posts/1/delete — and this site serves none of those,
// so following one landed on a missing page. Every link that is not
// already a fragment or a page of this tree becomes href="#", which
// goes nowhere and looks like the link it is; every form is aimed at
// the sink iframe previewDoc appends; and no element asks for focus
// (unfocus).
//
// A form's ACTION is deliberately left alone — nineteen of them still
// name a real route — because the target is what decides where the
// answer goes, and the target is the sink. The request is made and
// lands nowhere the reader can see, which is closer to what the sample
// says it does than a form posting to "#" would be. It also keeps the
// action a reader reads in the preview the same as the one in the HTML
// tab beside it.
//
// Only the LIVE rendering is treated. The HTML tab beside it shows the
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
	return unfocus(sampleForm.ReplaceAllString(out, `<form target="ds-void"`))
}

// copyJoin joins the parts of a copy button's accessible name, the
// section and then the state: "Copy field-text, Required". It is a
// separator rather than words, so it is one constant and not a prose
// key: every locale joins the two parts the same way.
const copyJoin = ", "

// titleSep and frameSep are the separators copy review approved for
// titles: " · " between a page, the site and the theme ("Form ·
// Rastrillo Design System · day"), and ", " before a preview frame's
// state ("field-text sample standalone preview, Required"). They
// replaced an em dash, which a screen reader announces or swallows
// depending on its settings. Separators, not words, so not translated.
const (
	titleSep = " · "
	frameSep = ", "
)

// docTitle is a gallery document's <title>: the page first, because a
// tab strip truncates from the end, then the site, then the theme when
// the document has one. The modal, shell and demo documents have none.
func docTitle(page, site, theme string) string {
	t := page + titleSep + site
	if theme != "" {
		t += titleSep + theme
	}
	return t
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
	return t + frameSep + proseIn(locale, qualifier)
}

// newPreview is one example's widget and the file its frame loads. body
// is what the frame shows, and its links are deadened there; code is
// what the HTML tab shows, and is laid out and highlighted here. The
// two differ for a partial sample, whose frame wears the container the
// partial assumes while its code does not. code empty means no HTML
// tab. page is the page's file stem, so the file sits in a directory
// named after the page that frames it.
//
// Only the HTML tab's copy is formatted: the frame keeps the bytes as
// rendered, so a measured frame height cannot move because the source
// beside it was laid out for reading.
//
// id is the example's anchor id. Both the height and the width class
// are read off it, because the two tables that size a preview are keyed
// the same way.
func newPreview(mount, theme, locale, page, group, title, body, code, id string) (previewView, previewFile) {
	file := page + "/" + group + ".html"
	view := previewView{
		Group: group,
		Style: previewStyle(id, heightOf(id)),
		Class: previewClass(id),
		Src:   pageHref(mount, theme, locale, file),
		Title: title,
	}
	if code != "" {
		view.Source = codeview.Highlight(codeview.Format(code))
	}
	return view, previewFile{Path: file, Doc: previewDoc(mount, theme, locale, title, deaden(mount, body))}
}

// wrapperMarkup is the opening markup of the container a wrapper puts
// a sample in, as the HTML and Template tabs name it. method and action are left
// out: they belong to the reader's form, and the gallery's action="#"
// is a placeholder nobody should copy.
func wrapperMarkup(w wrapper) string {
	switch w {
	case wrapList:
		return `<div rst-list>`
	case wrapForm:
		return `<section rst-box><form rst-form>`
	case wrapBox:
		return `<section rst-box>`
	case wrapStats:
		return `<div rst-stats>`
	}
	return ""
}

// wrap puts a sample in the container its partial assumes, per the two
// rules under Class idioms: rows go in a list, a form's fields go in a
// form inside a padded box. It runs in Go rather than in the template
// because the wrapper is part of the sample now — it is inside the
// frame, and it is in the source the HTML tab shows, which is where a
// reader learns that a field partial does not bring its own <form>.
func wrap(w wrapper, html string) string {
	switch w {
	case wrapList:
		return `<div rst-list>` + html + `</div>`
	case wrapForm:
		return `<section rst-box><form rst-form method="post" action="#">` + html + `</form></section>`
	case wrapBox:
		return `<section rst-box>` + html + `</section>`
	case wrapStats:
		return `<div rst-stats>` + html + `</div>`
	}
	return html
}

// ── Class idioms ─────────────────────────────────────────────────────

// idiomBlurbs is one English sentence per markup idiom, in the page's own
// voice. A missing entry renders no blurb rather than an empty one.
//
// English here is the source AND the prose.go key, exactly as in
// samples.go: adding an idiom means adding its eleven translations, and
// the parity gate says so.
var idiomBlurbs = map[string]string{
	"button":        "Buttons come in four variants and three sizes, and a link can look like one. For the spinner a form shows while it submits, see form-foot.",
	"box":           "The padded section card, and the heading that sits outside it.",
	"list-grid":     "The real data-table vocabulary: the card sets its columns once, rows only choose cells.",
	"stat-band":     "A row of stats across the top of a dashboard.",
	"dropdown":      "The details/summary menu behind header overflow menus and a list bar's filter, plus an applied filter as a removable chip.",
	"form-layout":   "The attributes that give a form its rhythm and its save bar. No partial emits these; they wrap a run of fields you compose yourself.",
	"tblock":        "A bordered card whose body reveals only while its switch is on, via :has(). The switch is authoritative; the reveal is a display convenience.",
	"modal":         "A modal is its own URL, not client state: the page underneath, marked inert, with the panel over it and a plain link to close.",
	"help":          "A bordered question mark linking to a help article. Its CSS tooltip is decoration; the link carries its own full-sentence label.",
	"selbox":        "The selection checkbox a list row wears in select mode. Its label restates the row's identity.",
	"shell-topbar":  "The topbar shell's own chrome, as markup. Layout ships it as a whole template.",
	"shell-sidebar": "The sidebar shell's chrome on a content page: on a phone, a back control to the index.",
}

// idiomRules attaches the two rules from docs/site/templates.md to the
// idioms they govern, quoted rather than paraphrased: they are the rules
// people get wrong, and the wording is the part that has been argued
// over.
var idiomRules = map[string]struct{ Title, Body string }{
	"box": {
		"Which card is which",
		"rst-list and rst-card have no padding by design: they hold a run of rows, and each row pads itself. " +
			"Put a form, a paragraph, a strip of links or anything else that is not a row straight into one and it renders flush against the border; the text touching the edge is the tell. " +
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
// An idiom's heading is an <h2> under the page's <h1>, with nothing
// between: a deeper level there would skip one, which is WCAG 1.3.1
// and what the accessibility gate found the first time it ran.
func buildIdioms(mount string, tmpl *template.Template, theme, locale string, files previewSet) ([]idiomView, error) {
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
		preview, file := newPreview(mount, theme, locale, "primitives", view.ID+"-0",
			previewTitle(locale, name, "UI primitives"),
			samples[name], samples[name], view.ID)
		if err := files.add(file); err != nil {
			return nil, err
		}
		view.Preview = preview
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

// shellData is what a shell demo page executes against. The shells
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
	Profile template.HTML
	// Signin and Brand are the stage demo's card: the plain Ask state,
	// with no problem, nothing remembered and no passkey door, because
	// the demo is about the shell and the card is only what it frames;
	// and the Screens page's own sample brand, so the two show one app.
	Signin auth.SigninState
	Brand  map[string]any
	// View, Up and PageHref are the index/page pair of the shells that
	// have one: which view this document is, where its back control
	// goes, and the content page its nav opens. "#" for the others.
	View     string
	Up       string
	PageHref string
}

// accountMarkup is the account block of every chrome shell: the menu
// body, the links. topbar and console wrap it in their Account menu,
// and the sidebar puts it in its profile menu's card under the person,
// so one literal serves all three, which is what lets a screen's
// account block move between them unedited. The person is the profile
// block, profileMarkup.
const accountMarkup template.HTML = `<a href="#">Profile</a><a href="#">Billing</a><hr><a href="#">Sign out</a>`

// profileMarkup is the person the profile menu shows: the avatar that
// opens it (the sidebar's rail foot, the console's Menu on its phone
// index) and the name and email heading its card. Sample data, so it
// stays English in every locale, as a person's name would.
const profileMarkup template.HTML = `<span rst-person-av aria-hidden="true">G</span>` +
	`<span rst-person-name>Grace Hopper</span><span rst-person-email>grace@example.com</span>`

// renderShell builds one shell's full-page demo: ui.Layout's own
// template, its chrome blocks filled with sample links so the frame is
// visible, and a small representative screen in the content hole. The
// result is keyed by file name under shells/: <shell>.html, and for a
// shell with an index also <shell>-page.html, the content page its nav
// opens, whose back control returns to <shell>.html#nav-posts.
func renderShell(mount, theme, locale, shell string) (map[string][]byte, error) {
	src, ok := ui.Layout(shell)
	if !ok {
		return nil, fmt.Errorf("no shell %q", shell)
	}
	funcs := galleryFuncs(locale)
	funcs["asset"] = assetFunc(mount, theme)
	tmpl, err := template.New("designsystem").Funcs(funcs).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(string(src)); err != nil {
		return nil, fmt.Errorf("parsing the %s shell: %w", shell, err)
	}
	overrides := shellTemplate
	if shell == "stage" {
		// stage has no chrome to fill and one card to show; the chrome
		// shells' content would put a Posts list in the middle of a
		// sign-in frame.
		overrides = stageShellTemplate
	}
	if _, err := tmpl.Parse(shellCommon + overrides); err != nil {
		return nil, fmt.Errorf("parsing the shell overrides: %w", err)
	}
	data := shellData{
		Locale:   locale,
		Dir:      rastrillo.Dir(locale),
		Name:     shell,
		Title:    docTitle(proseIn(locale, "The {shell} shell", "shell", shell), proseIn(locale, "Rastrillo Design System"), ""),
		Mount:    mount,
		Index:    indexHref(mount, theme, locale),
		Locales:  localeLinks(mount, theme, locale, "index.html"),
		Account:  accountMarkup,
		Profile:  profileMarkup,
		Signin:   auth.SigninState{Step: auth.StepAsk, BeginPath: "/signin", ForgetPath: "/signin/forget"},
		Brand:    galleryBrand,
		PageHref: "#",
	}
	type doc struct {
		file string
		data shellData
	}
	docs := []doc{{shell + ".html", data}}
	if shellHasIndex(shell) {
		index, page := data, data
		index.View, index.Up = "index", shellHref(mount, theme, locale, shell)
		page.View, page.Up = "page", shellHref(mount, theme, locale, shell)+"#nav-posts"
		index.PageHref = shellPageHref(mount, theme, locale, shell)
		page.PageHref = index.PageHref
		docs = []doc{{shell + ".html", index}, {shell + "-page.html", page}}
	}
	out := make(map[string][]byte, len(docs))
	for _, d := range docs {
		// A clone per document: html/template refuses to clone a tree
		// that has executed, and each document is its own execution.
		run, err := tmpl.Clone()
		if err != nil {
			return nil, err
		}
		var buf strings.Builder
		if err := run.ExecuteTemplate(&buf, "layout", d.data); err != nil {
			return nil, fmt.Errorf("%s: %w", d.file, err)
		}
		out[d.file] = []byte(buf.String())
	}
	return out, nil
}

// ── The modal demo ───────────────────────────────────────────────────

// modalData is what the modal demo page executes against. Self is the
// page's own address, so the panel's nav rail can switch tabs without
// leaving the modal; Index is the gallery, which is where Close goes.
type modalData struct {
	Title  string
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
	tmpl.Funcs(template.FuncMap{"asset": assetFunc(mount, theme)})
	if _, err := tmpl.Parse(modalTemplate); err != nil {
		return nil, fmt.Errorf("parsing the modal demo: %w", err)
	}
	var buf strings.Builder
	err = tmpl.ExecuteTemplate(&buf, "ds-modal", modalData{
		Title:  docTitle(proseIn(locale, "The modal route"), proseIn(locale, "Rastrillo Design System"), ""),
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
// Four documents, one per view, each a real page, the way the app it
// stands for would serve them: demo.html is the index (on a phone the
// rail is the whole page; on a desktop the dashboard sits beside it),
// and demo-dashboard.html, demo-requests.html and demo-request.html are
// content pages whose back control returns to demo.html#nav-…. It used
// to be one document switching three views with :target, which cannot
// show the two server-rendered views of one URL a phone needs.
//
// The framework's own scripts load on it, because a real app gets them
// and this is meant to read as a real app — and none of them is needed
// to move around it. TestTheDemoApplicationWorksWithNoScript drives the
// whole journey with script execution disabled in the engine, which is
// the strongest form of that claim. gallery.js loads too, restoring the
// colour scheme a reader chose in the gallery, exactly as the shell
// demos do.

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

// demoPageHref is one content page of the demo application. The demo is
// four documents, like the app it stands for: demo.html is the index
// (on a desktop it shows the dashboard beside the rail), and the
// dashboard, the request list and one request are pages whose back
// control returns to demo.html#nav-…. One document switching views with
// :target could not express two server-rendered views of one URL.
func demoPageHref(mount, theme, locale, view string) string {
	return mount + "/" + theme + "/" + locale + "/demo-" + view + ".html"
}

// demoViews are the demo's four documents, the index first.
var demoViews = []string{"index", "dashboard", "requests", "request"}

// demoData is what a demo page executes against. Self is the page's own
// address; Index is the gallery it came from. View is which of the four
// documents this is, Up its back control's address, Home the index, and
// the last three the content pages the rail and the rows link to.
type demoData struct {
	Locale    string
	Dir       string
	Mount     string
	Title     string
	Index     string
	Self      string
	Locales   []localeLink
	View      string
	Up        string
	Home      string
	Dashboard string
	Requests  string
	Request   string
}

// renderDemo builds one theme × locale copy of the demo application:
// its four documents, keyed by file name.
func renderDemo(mount, theme, locale string) (map[string][]byte, error) {
	src, ok := ui.Layout(demoShell)
	if !ok {
		return nil, fmt.Errorf("no shell %q to build the demo application in", demoShell)
	}
	funcs := galleryFuncs(locale)
	funcs["asset"] = assetFunc(mount, theme)
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
	home := demoHref(mount, theme, locale)
	out := make(map[string][]byte, len(demoViews))
	for _, view := range demoViews {
		file, self, up := "demo-"+view+".html", demoPageHref(mount, theme, locale, view), home+"#nav-requests"
		switch view {
		case "index":
			file, self, up = "demo.html", home, home
		case "dashboard":
			up = home + "#nav-dashboard"
		}
		// A clone per document: html/template refuses to clone a tree
		// that has executed, and each document is its own execution.
		run, err := tmpl.Clone()
		if err != nil {
			return nil, err
		}
		var buf strings.Builder
		err = run.ExecuteTemplate(&buf, "layout", demoData{
			Locale:    locale,
			Dir:       rastrillo.Dir(locale),
			Mount:     mount,
			Title:     docTitle(proseIn(locale, "The demo application"), proseIn(locale, "Rastrillo Design System"), ""),
			Index:     indexHref(mount, theme, locale),
			Self:      self,
			Locales:   localeLinks(mount, theme, locale, file),
			View:      view,
			Up:        up,
			Home:      home,
			Dashboard: demoPageHref(mount, theme, locale, "dashboard"),
			Requests:  demoPageHref(mount, theme, locale, "requests"),
			Request:   demoPageHref(mount, theme, locale, "request"),
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		out[file] = []byte(buf.String())
	}
	return out, nil
}

// demoView is the widget the Overview frames the demo application in:
// the same preview widget every example on this tree uses, loading the
// real page rather than a copy of it, and with no HTML or Template tab — an
// application is not a snippet to paste.
func demoView(mount, theme, locale string) previewView {
	return previewView{
		Group: "demo-app-0",
		Style: previewStyle("demo-app", heightOf("demo-app")),
		Class: previewClass("demo-app"),
		Src:   demoHref(mount, theme, locale),
		Title: proseIn(locale, "The demo application"),
	}
}

// demoCSS is the whole of the demo's own stylesheet: one margin. The
// view switching it used to carry went when the demo became four
// documents, and the rail's current item is aria-current written by the
// server now, as in a real app.
const demoCSS = `
[rst-stats] { margin-block-end: var(--rst-sp-5); }
`

// demoTemplate fills every block demoShell leaves open, and then the
// content hole with whichever view this document is (the index shows
// the dashboard, which is what a desktop sees beside the rail).
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
{{define "head"}}<script src="{{asset "gallery.js"}}"></script>
<style>` + demoCSS + `</style>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
{{define "view"}}{{if eq .View "index"}}index{{else}}page{{end}}{{end}}
{{define "up"}}{{.Up}}{{end}}
{{define "brand"}}<a rst-shell-brand href="{{.Home}}">Harbour</a>{{end}}
{{define "nav"}}<a id="nav-dashboard" href="{{.Dashboard}}"{{if or (eq .View "index") (eq .View "dashboard")}} aria-current="page"{{end}}>{{P "Dashboard"}}</a><a id="nav-requests" href="{{.Requests}}"{{if or (eq .View "requests") (eq .View "request")}} aria-current="page"{{end}}>{{P "Requests"}}</a>{{end}}
{{define "locale"}}<details rst-dropdown rst-locale name="rst-menus"><summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span></summary><div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>{{end}}
{{define "profile"}}<span rst-person-av aria-hidden="true">A</span><span rst-person-name>Ada Lovelace</span><span rst-person-email>ada@example.com</span>{{end}}
{{define "content"}}
{{if or (eq .View "index") (eq .View "dashboard")}}
<section class="app-view" id="view-dashboard">
{{template "page-header" dict "Title" (P "Dashboard") "Sub" (P "Everything the team has waiting this morning.")}}
<div rst-stats>
{{template "stat" dict "Label" (P "Open requests") "Value" "24" "Lead" true "Delta" "−6" "Tone" "positive" "Note" (P "since Monday")}}
{{template "stat" dict "Label" (P "Waiting on us") "Value" "6"}}
{{template "stat" dict "Label" (P "Resolved this week") "Value" "41" "Delta" "+12" "Tone" "positive" "Note" (P "since Monday")}}
</div>
<div rst-box-head><h2>{{P "Mailbox storage"}}</h2></div>
<section rst-box>{{template "meter" dict "Percent" 82 "Text" "412 / 500"}}</section>
<div rst-box-head><h2>{{P "Latest activity"}}</h2><a rst-btn href="{{.Requests}}">{{P "Requests"}}</a></div>
<div rst-card style="--rst-cols: minmax(0, 1fr) 120px">
<div rst-lrow="head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Invoice #4471 never arrived<small><bdi>Fiona Reid</bdi> · 09:12</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Card declined on renewal<small><bdi>Otto Neurath</bdi> · 08:40</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Seat count is wrong on the invoice<small><bdi>Hedy Lamarr</bdi> · 11 August</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span></div>
</div>
</section>
{{else if eq .View "requests"}}
<section class="app-view" id="view-requests">
{{template "page-header" dict "Title" (P "Requests") "Sub" (P "Every request in the queue, newest first.") "ActionHref" .Requests "ActionLabel" (P "New request") "ActionIcon" "plus"}}
{{template "seg-tabs" dict "Label" (P "Requests") "Items" (list (dict "Label" (P "All") "Href" .Requests "Current" true) (dict "Label" (P "Open") "Href" .Requests) (dict "Label" (P "Resolved") "Href" .Requests))}}
<div rst-card style="--rst-cols: minmax(0, 1fr) 120px 120px var(--rst-col-menu)">
{{template "list-bar" dict "SearchAction" .Requests "Placeholder" (P "Search requests")}}
<div rst-lrow="head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span><span class="rst-m-hide">{{P "Updated"}}</span><span></span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Invoice #4471 never arrived<small><bdi>Fiona Reid</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span><span class="rst-cell-mut rst-m-hide">09:12</span>{{template "row-menu" dict "Name" "Invoice #4471 never arrived" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Card declined on renewal<small><bdi>Otto Neurath</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">08:40</span>{{template "row-menu" dict "Name" "Card declined on renewal" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Export takes twenty minutes<small><bdi>Mary Sherman</bdi> · Data</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">12 August</span>{{template "row-menu" dict "Name" "Export takes twenty minutes" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Seat count is wrong on the invoice<small><bdi>Hedy Lamarr</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span><span class="rst-cell-mut rst-m-hide">11 August</span>{{template "row-menu" dict "Name" "Seat count is wrong on the invoice" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
</div>
<p rst-count-line>{{P "{shown} of {total} requests" "shown" "1–4" "total" "24"}}</p>
{{template "pagination" dict "Items" (list (dict "Label" "1" "Current" true) (dict "Label" "2" "Href" .Requests) (dict "Label" "3" "Href" .Requests))}}
</section>
{{else}}
<section class="app-view" id="view-request">
{{template "back-nav" dict "Href" .Requests "Label" (P "Requests")}}
{{template "page-header" dict "Title" "Invoice #4471 never arrived" "Sub" (P "Reported by {person}, and still waiting on us." "person" "Fiona Reid")}}
<p>{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}} {{template "badge" dict "Label" "Billing"}}</p>
<div rst-box-head><h2>{{P "Details"}}</h2></div>
<section rst-box>{{template "detail-list" dict "Items" (list (dict "Label" (P "Reference") "Value" "REQ-4471" "Mono" true) (dict "Label" (P "Reported by") "Value" "fiona@example.com") (dict "Label" (P "Queue") "Value" "Billing") (dict "Label" (P "Opened") "Value" "14 August, 09:12" "DateTime" "2026-08-14T09:12"))}}</section>
<div rst-box-head><h2>{{P "Reply"}}</h2></div>
<section rst-box><form rst-form method="post" action="{{.Request}}">
{{template "field-textarea" dict "Name" "reply" "Label" (P "Your reply") "Rows" 4 "Hint" (P "The person who reported this gets it by email.")}}
{{template "form-foot" dict "Submit" (P "Send reply") "CancelHref" .Requests "CancelLabel" (P "Cancel")}}
</form></section>
{{template "callout" dict "Tone" "info" "Title" (P "Every screen has its own address") "Body" (P "Each screen is a page of its own, like any rastrillo screen, and works the same with JavaScript off. On a phone, the sections are a list and each screen has a way back.")}}
</section>
{{end}}
{{end}}
`

// ── Icons ────────────────────────────────────────────────────────────
//
// The whole page is a reading of rastrillo.IconSlugs(). Nothing here
// names an icon and nothing counts them: the twelfth slug (menu) landed
// while this page was being written and cost no edit, and the
// thirteenth will cost none either. TestTheIconsPageIsAReadingOfIconSlugs
// is what holds that — it fails on a slug the page does not draw and on
// a drawing the slug set does not have.

// lucideHome is the one address in this tree that is not in this tree.
// It is where the glyphs come from and where a reader goes to see the
// rest of the set; see outboundLinks in designsystem_test.go, which is
// the gate's side of the same decision.
const lucideHome = "https://lucide.dev"

// iconWiring is how an app puts its own icon set in front of the
// framework's. Held here as source rather than written into the
// template because it is code a reader copies, not prose the page says
// — the same footing ui.Styleguide's samples are on.
const iconWiring = `tmpl := template.Must(template.New("").
	Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
	ParseFS(ui.Templates(), "*.html"))`

// iconRebind is the call the trap is about: BOTH seams, every time.
// ui.FuncsWith(t) alone is the shape that silently reverts.
const iconRebind = `ui.Funcs(ui.WithT(t), ui.WithIcons(icons.Icon, icons.Assets))`

// iconView is one slug: the markup Icon answers, the call that renders
// it, and where the glyph came from.
//
// Provenance is an address rather than a sentence — lucide.dev/icons/…
// — so it needs no translation and a reader can type it. Renamed is
// set where rastrillo's name for the glyph is not Lucide's, which is
// the difference a reader can see between the two lines.
type iconView struct {
	ID         string
	Slug       string
	Call       string
	Provenance string
	Renamed    bool
	Markup     template.HTML
}

// iconsView is the Icons page's data. Total and Renamed are counted
// here rather than written into a sentence, so the sentence cannot be
// wrong about the set it is describing.
type iconsView struct {
	List       []iconView
	Total      int
	Renamed    int
	Wiring     string
	Rebind     string
	LucideLine template.HTML
}

func buildIcons(locale string) iconsView {
	slugs := rastrillo.IconSlugs()
	out := iconsView{
		List:   make([]iconView, 0, len(slugs)),
		Total:  len(slugs),
		Wiring: iconWiring,
		Rebind: iconRebind,
	}
	for _, slug := range slugs {
		name := iconsets.LucideName(slug)
		renamed := name != "" && name != slug
		if renamed {
			out.Renamed++
		}
		out.List = append(out.List, iconView{
			ID:         anchorID("icon", slug),
			Slug:       slug,
			Call:       `{{icon "` + slug + `"}}`,
			Provenance: "lucide.dev/icons/" + name,
			Renamed:    renamed,
			Markup:     rastrillo.Icon(slug),
		})
	}
	out.LucideLine = proseMarkup(locale, "The whole set is at {link}.", "link", template.HTML(
		`<a href="`+lucideHome+`" target="_blank" rel="noopener">lucide.dev<span class="rst-sr-only"> (`+
			template.HTMLEscapeString(proseIn(locale, "opens in a new tab"))+`)</span></a>`))
	return out
}

// ── Getting started ──────────────────────────────────────────────────
//
// Every weight on that page is len() of the bytes the page was rendered
// from. Not one of them is written down, here or in prose.go, which is
// the whole point: a number in a sentence is wrong the first time
// somebody edits the file it describes, and this project has corrected
// five of those in three days.

// assetView is one shipped file: what it is called, where this tree
// serves it, what it weighs and what it is for.
type assetView struct {
	ID    string
	Name  string
	Href  string
	Bytes int
	Blurb string
}

// assetsView is the Getting started page's data. AppBytes is what a
// scaffolded app is actually handed — tokens.css, ONE theme and the
// three scripts — which is not the sum of the list, because the list
// carries every theme and an app receives one.
//
// What the list does NOT carry is this gallery's own furniture:
// gallery.js and gallery.css are the page's plumbing, no scaffold
// writes them, no app is ever handed them, and a reader of a page
// about what an app ships has no use for either. They were on the list
// once, each with a sentence saying it did not count — which is a row
// whose whole content is that it should not be there.
// TestTheGettingStartedPageWeighsTheRealAssets holds the absence.
type assetsView struct {
	List     []assetView
	AppBytes int
	Scaffold string
	Pin      string
	// Links is the two <link> tags a page outside the framework needs,
	// highlighted: the order is the point, tokens.css then the theme,
	// because the theme's values fill the references tokens.css makes.
	Links template.HTML
}

// buildAssets reads the shipped assets out of ui, in the order the page
// lists them. The theme rows come off ui.ThemeNames(), so a fourth
// theme appears here and in the rail with no edit, and the getters are
// the same ones Render writes the tree's copies from — the file a
// reader downloads and the number beside it cannot disagree.
func buildAssets(mount, theme, locale string) assetsView {
	// The vendored set, from the one place it is defined:
	// ui.VendoredAssets, shared with rastrillo new's scaffold, the pin
	// test it generates into every app, and rastrillo doctor. This page
	// is the fourth reader of that list, and the only one whose numbers
	// nobody would notice going stale — so it reads the list rather
	// than repeating it, and a sixth vendored file lands in the total
	// here on the same commit it reaches an app.
	vendored, ok := ui.VendoredAssets(theme)
	if !ok {
		// Render has already failed on an unknown theme by the time
		// anything calls this; an empty row would be a silent lie.
		panic("designsystem: no theme " + theme)
	}
	appBytes := 0
	for _, body := range vendored {
		appBytes += len(body)
	}
	add := func(out *assetsView, name string, body []byte, blurb string, args ...any) {
		out.List = append(out.List, assetView{
			ID:    anchorID("asset", name),
			Name:  name,
			Href:  assetURL(mount, name),
			Bytes: len(body),
			Blurb: proseIn(locale, blurb, args...),
		})
	}
	out := assetsView{
		AppBytes: appBytes,
		Scaffold: "rastrillo new --theme=" + theme + " myapp",
		Pin:      `const vendoredTheme = "` + theme + `"`,
		Links:    codeview.Highlight(`<link rel="stylesheet" href="tokens.css">` + "\n" + `<link rel="stylesheet" href="theme-` + theme + `.css">`),
	}
	add(&out, "tokens.css", ui.TokensCSS(),
		"Structure: every component class, every scale, and no colour literal anywhere. The same file under every theme.")
	for _, name := range ui.ThemeNames() {
		css, ok := ui.ThemeCSS(name)
		if !ok {
			continue
		}
		add(&out, "theme-"+name+".css", css,
			"Colour, type family and shape for the {theme} theme: one :root block where every colour is declared once as a light-dark() pair.", "theme", name)
	}
	add(&out, "shell.css", ui.ShellCSS(),
		"The slide between pages on a phone, for the sidebar and console shells. Deletable with shell.js.")
	add(&out, "rastrillo.js", ui.ShimJS(),
		"The progressive-enhancement shim: polling fragments and light dismiss. Every scaffolded app gets it.")
	add(&out, "busy.js", ui.BusyJS(),
		"The busy rule: while a form sends, its button shows a spinner for at least 650ms and refuses a second submit, and Back hands it back. Every scaffolded app gets it.")
	add(&out, "shell.js", ui.ShellJS(),
		"Phone navigation for the sidebar and console shells: the slide between pages, Back that reuses history, and focus returned to the section you left. Deletable on its own.")
	add(&out, "select.js", ui.SelectJS(),
		"field-select's searchable combobox. Inert until a select opts in with data-rst-select, and deletable on its own.")
	add(&out, "datetime.js", ui.DatetimeJS(),
		"The date fields' natural-language input. Inert until a field opts in with data-rst-date or data-rst-time, and deletable on its own.")
	add(&out, "calendar.js", ui.CalendarJS(),
		"The month grid that field's calendar button opens. Inert until datetime.js asks it for a panel, and deletable on its own: without it the button falls back to the browser's own picker.")
	return out
}

// ── The page itself ──────────────────────────────────────────────────

// pageTemplate is the frame every page of a <theme>/<locale> directory
// is drawn in: the head, the rail, the bar, the page header, and a
// hole where the section's own body goes. The
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
// The frame is the sidebar layout an app is scaffolded with
// (ui/layouts/sidebar.html), attribute for attribute: below 800px the
// Overview is the index and every other page a content page with the
// shell's back control, and shell.js and shell.css give the slide, Back
// through history and the focus return with nothing written here. Two
// departures, each for a reason: the index's pieces are on the Overview
// only, and the controls live in a bar pinned over main on a wide
// screen and, on a phone, in a display settings menu at the top of
// every page (the index's header row, a content page's back strip), so
// every page writes them twice. That is dogfooding with a point: the
// shell is one of the things this page documents.
// viewTemplate is the preview widget, once, used by every example on
// the page. The tabs run Desktop, Mobile, HTML, Template: from the
// picture to the markup it is, to the call that writes it. A source tab
// exists only where it has something to show. The shell demos have
// neither, a sample with no call has no Template, and a sign-in screen,
// whose markup is the module's to produce, has no HTML; an empty tab
// would be a click that shows a blank panel.
//
// No radio starts checked, and that is not an oversight. It used to be
// Desktop, so that a page with no JavaScript and no interaction at all
// opened on the rendering a reader most wants — true on a laptop, and
// exactly wrong on a phone, where a 1200px page scaled into a 309px
// column is an 18px sliver nobody can read. CSS cannot tell an
// explicit choice from a shipped default, so as long as one radio
// arrives checked the opening view can never follow the reader's width
// without taking the other view away from them. With none of them
// checked, gallery.css picks the opening rendering from the width and
// lights the tab that matches it, and a click on either tab still
// overrides the width at any size. The Desktop label carries a
// modifier class for the same reason the Mobile one does: the
// stylesheet has to be able to say which of the two a reader chose.
//
// The two renderings and the two sources are each a pair of their own
// in one radio group, so that where the four do not fit on one line,
// on a phone or in a language with long words, the source pair moves
// under the renderings whole rather than one tab overflowing the page.
const viewTemplate = `{{define "ds-view"}}<div class="ds-view{{.Class}}" style="{{.Style}}">
<fieldset class="ds-view__tabs"><legend class="rst-sr-only">{{P "Preview"}}</legend>
<span class="ds-view__pair"><label class="ds-view__tab ds-view__tab--d"><input type="radio" name="{{.Group}}">{{P "Desktop"}}</label>
<label class="ds-view__tab ds-view__tab--m"><input type="radio" name="{{.Group}}">{{P "Mobile"}}</label></span>
{{if or .HasHTML .HasTemplate}}<span class="ds-view__pair">{{if .HasHTML}}<label class="ds-view__tab ds-view__tab--h"><input type="radio" name="{{.Group}}">{{P "HTML"}}</label>
{{end}}{{if .HasTemplate}}<label class="ds-view__tab ds-view__tab--t"><input type="radio" name="{{.Group}}">{{P "Template"}}</label>
{{end}}</span>
{{end}}</fieldset>
<div class="ds-view__stage"><div class="ds-view__box"><iframe class="ds-view__frame" title="{{.Title}}" src="{{.Src}}" loading="lazy"></iframe></div></div>
{{if .Rows}}<div class="ds-view__code ds-view__code--h">
{{range .Rows}}<p class="ds-state">{{.State}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Source}}</code></pre>
{{end}}</div>
<div class="ds-view__code ds-view__code--t">
{{range .Rows}}<p class="ds-state">{{.State}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Call}}</code></pre>
{{end}}</div>
{{else}}{{if .Source}}<div class="ds-view__code ds-view__code--h">
{{if .Wrapper}}<p class="ds-wrap">{{.Wrapper}}</p>
{{end}}<pre class="ds-src rst-mono" tabindex="0"{{if .NoCopy}} data-ds-nocopy{{end}}><code>{{.Source}}</code></pre>
</div>
{{end}}{{if .Call}}<div class="ds-view__code ds-view__code--t">
{{if .Wrapper}}<p class="ds-wrap">{{.Wrapper}}</p>
{{end}}<pre class="ds-src rst-mono" tabindex="0"><code>{{.Call}}</code></pre>
</div>
{{end}}{{end}}</div>{{end}}`

// cogIcon is the display settings button's glyph, Lucide's settings
// (ISC, as icons.go; lucide-static 1.31.0), drawn the way the icon set
// draws its own. The framework's set has no settings glyph, and this
// one button is the gallery's furniture, not something an app is
// offered, so it is not worth a slug every scaffoldable set would then
// have to carry. aria-hidden: the button's name is its aria-label.
const cogIcon = `<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
	`<path d="M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915"/><circle cx="12" cy="12" r="3"/></svg>`

// pageTemplate is every gallery page. The Overview's phone heading
// leaves the shell's brand span empty and puts the gallery's name in
// the title span after it. The shell hides a brand span with no element
// in it, as a blank brand, and shows the title instead; the name written
// as bare text in the brand span was hidden that way, and the phone
// index had no visible heading. A brand link would add a tab stop that
// goes to the page it is on.
const pageTemplate = `{{define "ds-controls"}}<nav rst-seg-tabs aria-label="{{P "Theme"}}">{{range .Themes}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
<div class="ds-scheme" role="group" aria-label="{{P "Colour scheme"}}">{{range .Schemes}}<button type="button" data-ds-scheme="{{.Value}}" aria-pressed="{{.Pressed}}">{{.Label}}</button>{{end}}</div>
<details rst-dropdown rst-locale name="rst-menus">
<summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span><span class="rst-sr-only">{{P ", currently {language}" "language" .LocaleName}}</span></summary>
<div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div>
</details>{{end}}
{{define "ds-prefs"}}<details rst-dropdown class="ds-prefs" id="ds-prefs" name="ds-prefs"><summary aria-label="{{P "Display settings"}}">` + cogIcon + `</summary><div rst-dropdown-menu>{{template "ds-controls" .}}</div></details>{{end}}
{{define "ds-page"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>{{.DocTitle}}</title>
<link rel="stylesheet" href="{{asset "tokens.css"}}">
<link rel="stylesheet" href="{{asset "theme.css"}}">
<link rel="stylesheet" href="{{asset "shell.css"}}">
<link rel="stylesheet" href="{{asset "gallery.css"}}">
<script src="{{asset "gallery.js"}}"></script>
<script defer src="{{asset "rastrillo.js"}}"></script>
<script defer blocking="render" src="{{asset "shell.js"}}"></script>
</head>
<body>
<div rst-shell-sidebar="{{if .Rows}}index{{else}}page{{end}}">
<a rst-skip href="#main">{{T "rastrillo.ui.shell_skip"}}</a>
{{with .Up}}<div rst-shell-back><a href="{{.}}" rel="up" aria-label="{{Tf "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a>{{template "ds-prefs" $.Bar}}</div>
{{end}}<aside class="ds-rail" rst-shell-rail>
{{if .Rows}}<h1 rst-shell-title><span></span><span>{{P "Rastrillo Design System"}}</span></h1>
{{template "ds-prefs" .Bar}}
<p class="ds-index-lead">{{P "The Rastrillo design system aims to be a starter framework for any app to get a consistent, polished, accessible UI with no or minimal JavaScript dependence, available in multiple languages, and using clean, modern HTML and CSS. It's designed to be delightful to use with or without LLM assistance, and easily remixable."}}</p>
{{end}}  <search class="ds-search">
    <label class="rst-sr-only" for="ds-filter">{{P "Filter"}}</label>
    {{icon "search"}}<input id="ds-filter" type="search" placeholder="{{P "Filter"}}" autocomplete="off" aria-controls="ds-nav" data-ds-filter>
  </search>
  <p class="ds-nav__empty" data-ds-filter-empty role="status" hidden>{{P "No matches"}}</p>
{{if .Rows}}  <nav class="ds-index" rst-shell-nav aria-label="{{T "rastrillo.ui.shell_up_label"}}">{{range .Rows}}<a id="{{.ID}}" href="{{.Href}}">{{.Title}}</a>{{end}}<p rst-shell-group>{{.Demos.Title}}</p>{{range .Demos.Items}}<a href="{{.Href}}" target="_blank" rel="noopener">{{.Label}}<span class="rst-sr-only"> ({{P "opens in a new tab"}})</span></a>{{end}}</nav>
{{end}}  <nav class="ds-nav" rst-shell-nav id="ds-nav" aria-label="{{P "Sections and demos"}}">
{{range .Nav}}{{if .Items}}    <details{{if .Current}} open aria-current="page"{{end}}><summary><span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span>{{.Title}}</summary>{{range .Items}}<a href="{{.Href}}"{{if .Aria}} aria-label="{{.Aria}}"{{end}}{{if .Code}} class="rst-mono"{{end}}{{if .Blank}} target="_blank" rel="noopener"{{end}}{{if .Terms}} data-ds-terms="{{.Terms}}"{{end}}>{{.Label}}</a>{{end}}</details>
{{else}}    <a class="ds-nav__page" href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Title}}</a>
{{end}}{{end}}  </nav>
</aside>
<header class="ds-top">
<a class="ds-top__brand" href="{{.Home}}">{{P "Rastrillo Design System"}}</a>
<div class="ds-top__controls">{{template "ds-controls" .Bar}}</div>
</header>
<main rst-shell-main id="main">
<div rst-page>

<header rst-page-header>
  <div rst-page-header-titles>
    <h1{{with .TitleID}} id="{{.}}"{{end}}>{{.Title}}</h1>
  </div>
</header>

{{.Body}}

<nav class="ds-updown" aria-label="{{P "Previous and next"}}">{{with .Prev}}<a class="ds-updown__prev" href="{{.Href}}">{{.Label}}</a>{{end}}{{with .Next}}<a class="ds-updown__next" href="{{.Href}}">{{.Label}}</a>{{end}}</nav>
<p class="rst-sr-only" role="status" data-ds-copy-status data-copy="{{P "Copy"}}" data-copied="{{P "Copied"}}" data-failed="{{P "Copy failed. Select the code and copy it yourself."}}" data-join="{{.CopyJoin}}"></p>

</div>
</main>
</div>
</body>
</html>
{{end}}`

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
<p class="ds-intro">{{P "The Rastrillo design system aims to be a starter framework for any app to get a consistent, polished, accessible UI with no or minimal JavaScript dependence, available in multiple languages, and using clean, modern HTML and CSS. It's designed to be delightful to use with or without LLM assistance, and easily remixable."}}</p>
<section class="ds-demo" id="demo-app">
<h2 class="ds-sub">{{P "The demo application"}}</h2>
<p class="ds-lead">{{P "A working app built only from this system: a dashboard, a list, one record open. Each has its own address. No JavaScript."}}</p>
{{template "ds-view" .Demo}}
<p class="ds-note"><a href="{{.DemoHref}}" target="_blank" rel="noopener">{{P "Open the demo application"}}<span class="rst-sr-only"> ({{P "opens in a new tab"}})</span></a>.</p>
</section>
<h2 class="ds-sub">{{P "Where to go next"}}</h2>
<ul class="ds-routes">{{range .Routes}}
<li><a href="{{.Href}}">{{.Label}}</a><span>{{.Blurb}}</span></li>{{end}}
</ul>
{{end}}`

// gettingStartedBody is the page a reader meets before any of the
// vocabulary: what the framework actually ships as CSS and JavaScript,
// and what it costs.
//
// Every weight on it is len() of the embedded bytes, taken while the
// page renders. There is no number in prose.go and none in this
// constant, which is the only way a page about file sizes stays true
// after somebody edits one of the files.
const gettingStartedBody = `{{define "ds-body-getting-started"}}
<p class="ds-lead">{{P "Rastrillo apps get all of this day one, but anyone can use the design system. Progressively enhanced with JS, but no JS required."}}</p>

<h2 class="ds-sub">{{P "The stylesheets"}}</h2>
<p class="ds-lead">{{P "tokens.css is structure: the component classes, the layout, and the scales for type, spacing and radius. Values are references, set elsewhere. themes/<name>.css is colour, type family and shape: one :root block where every colour is declared once as a light-dark() pair."}}</p>

<h2 class="ds-sub">{{P "The scripts"}}</h2>
<p class="ds-lead">{{P "rastrillo.js is the progressive-enhancement shim: polling fragments, light dismiss. busy.js is the busy rule. select.js and datetime.js are enhancements, each inert until a control opts in and each deletable on its own."}}</p>

<h2 class="ds-sub">{{P "What each file weighs"}}</h2>
<p class="ds-note">{{P "Filesizes for the various components."}}</p>
<ul class="ds-files">{{range .Assets.List}}
<li id="{{.ID}}" data-ds-anchor><a class="rst-mono" href="{{.Href}}" download="{{.Name}}">{{.Name}}</a><span>{{.Blurb}}</span><span class="rst-mono">{{P "{bytes} bytes" "bytes" .Bytes}}</span></li>{{end}}
</ul>
<p class="ds-lead">{{P "A new app gets {bytes} bytes of CSS and JavaScript in total: tokens.css, one theme, and the scripts." "bytes" .Assets.AppBytes}}</p>

<h2 class="ds-sub">{{P "In a new rastrillo app"}}</h2>
<p class="ds-lead">{{P "rastrillo new writes these into the app's static directory and links them from the layout, tokens.css first. They're yours from then on: edit them, or delete what you don't use."}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Assets.Scaffold}}</code></pre>
<p class="ds-lead">{{P "The theme is pinned twice. --theme decides which shipped theme is copied to static/theme.css, and logged at app generation time."}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Assets.Pin}}</code></pre>

<h2 class="ds-sub">{{P "Using it without the framework"}}</h2>
<p class="ds-lead">{{P "The names above are links. Take tokens.css and one theme and you have the whole visual system: attributes on ordinary HTML, no build step."}}</p>
<p class="ds-lead">{{P "Link them in this order, tokens.css first:"}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Assets.Links}}</code></pre>

<h2 class="ds-sub">{{P "Upgrading"}}</h2>
<p class="ds-lead">{{P "The trap worth knowing before it costs you an afternoon: tokens.css is copied into the app's static directory at scaffold time and frozen there, while the partials it styles keep upgrading with the module. An app can quietly run new markup against old CSS. Re-copy tokens.css when you upgrade, and the theme and the scripts with it."}}</p>
<p class="ds-note">{{P "rastrillo doctor will compare an app's frozen files against the module's and offer to re-copy."}}</p>
{{end}}`

// iconsBody is every slug rastrillo.IconSlugs() answers, drawn at 1em —
// the size tokens.css gives .icon, which is the size a component draws
// it at. The grid is the token page's own, because an icon in this
// system is a token-shaped thing: a name, a value and a call.
//
// Nothing in this constant names an icon or counts them. Both numbers
// in the sentence under the grid are interpolated from the set.
const iconsBody = `{{define "ds-body-icons"}}
<p class="ds-lead">{{P "{total} slugs, vendored as inline SVG and compiled into the binary: no build step, no second origin, and they work with no network at all. Each one is sized from the text beside it by tokens.css's own .icon rule, which is the size you are looking at here. A slug nothing answers renders nothing, so a typo costs a missing icon rather than a page that died mid-response." "total" .Icons.Total}}</p>
<ul class="ds-toks ds-icons">{{range .Icons.List}}
<li class="ds-tok" id="{{.ID}}" data-ds-anchor><span class="ds-chip ds-chip--icon">{{.Markup}}</span><span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Slug}}</span><span class="ds-tok__value">{{.Call}}</span><span class="ds-tok__value">{{.Provenance}}</span></span></li>{{end}}
</ul>

<h2 class="ds-sub">{{P "The names are rastrillo's own"}}</h2>
<p class="ds-lead">{{P "{renamed} of the {total} differ from the name lucide.dev publishes, so even the Lucide set carries a translation of its own. Where the last line under an icon does not repeat its name, that is one of them. The payoff is that one call means the same glyph whichever set an app scaffolds, and the shipped partials never change when the set does." "renamed" .Icons.Renamed "total" .Icons.Total}}</p>
<p class="ds-note">{{P "kebab and menu are the pair worth keeping straight: kebab is the three dots that mean more actions on this row, menu the three lines that mean navigation. The shells use menu when they collapse."}}</p>

<h2 class="ds-sub">{{P "Where the glyphs come from"}}</h2>
<p class="ds-lead">{{P "Lucide, vendored under the ISC licence and pinned in the source rather than fetched at run time. rastrillo new --icons=font-awesome scaffolds Font Awesome Free instead: a source an app can draw from, not a dependency this framework has. Nothing here fetches it, and Pro is a paid product rastrillo cannot vendor on your behalf."}}</p>
<p class="ds-note">{{.Icons.LucideLine}}</p>

<h2 class="ds-sub">{{P "An icon the framework does not ship"}}</h2>
<p class="ds-lead">{{P "The icons package rastrillo new writes is app-owned source: add the glyph there and call it like any other. ui.WithIcons is the seam that puts your set in front of the framework's own."}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Icons.Wiring}}</code></pre>
<p class="ds-lead">{{P "The trap, and it is a silent one: ui.FuncsWith rebinds icon and iconAssets back to the built-in set. An app that scaffolded its own icons has to pass both seams on every call, or its icons revert to Lucide on every request while still rendering something perfectly plausible."}}</p>
<pre class="ds-src rst-mono" tabindex="0"><code>{{.Icons.Rebind}}</code></pre>
{{end}}`

const tokensBody = `{{define "ds-body-tokens"}}
<p class="ds-lead">{{P "Shared custom properties for all components. Colour and the type are part of the theme (themes/{theme}.css). Type scale and the spacing steps are structure and come from tokens.css." "theme" .Theme}}</p>
<p class="ds-swatch-note">{{P "Light mode shown here, light-dark for all values. WCAG 2.2 AA floors: 4.5:1 for text, 3:1 for control borders. All colours in chips etc pull from the variables."}}</p>
{{range .Colours}}
<h2 class="ds-sub" id="{{.ID}}" data-ds-anchor>{{.Title}}</h2>
<ul class="ds-toks">{{range .Rows}}<li class="ds-tok">{{if .Preview}}<span class="ds-chip" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}
{{range .Structure}}
<h2 class="ds-sub" id="{{.ID}}" data-ds-anchor>{{.Title}}</h2>
<ul class="ds-toks">{{$kind := .Kind}}{{range .Rows}}<li class="ds-tok">{{if eq $kind "type"}}<span class="ds-type" style="{{.Preview}}">Ag</span>{{else if eq $kind "space"}}<span class="ds-bar" style="{{.Preview}}"></span>{{else}}<span class="ds-chip ds-chip--fill" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}
{{end}}`

// familyBody is every component page: the family whose page this is,
// its partials, and the two sentences true of all of them.
//
// One template for five pages rather than five: the pages differ in
// which family they are and in nothing else, and the two sentences
// belong on each of them — a reader who lands on Form from a search has
// not read the Display page's preamble.
//
// The page's own title is its <h1>, so a partial is an <h2>.
const familyBody = `{{define "ds-family"}}{{with .Family}}
<p class="ds-lead">{{.Blurb}}</p>
{{end}}
<p class="ds-lead">{{P "Each example is live, but its links go nowhere."}} {{P "HTML shows the markup to copy."}}</p>
{{range .Family.Partials}}
{{.Marker}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
<h2 class="ds-partial__name rst-mono">{{.Name}}</h2>
{{if .Blurb}}<p class="ds-lead">{{.Blurb}}</p>{{end}}
{{range .States}}
<div class="ds-sample">
{{if .State}}<p class="ds-state">{{.State}}</p>{{end}}
{{template "ds-view" .Preview}}
{{if .Note}}<p class="ds-note">{{.Note}}</p>{{end}}
</div>
{{end}}
{{range .Notes}}<p class="ds-note"><b class="ds-note__state">{{.State}}</b>
{{.Note}}</p>{{end}}
</article>
{{end}}
{{end}}`

const primitivesBody = `{{define "ds-body-primitives"}}
<p class="ds-lead">{{P "The shapes a component cannot be, because they wrap a body only the caller knows: the section card, the data grid, the disclosure menu, the shells' own chrome. tokens.css ships the classes and an app writes the markup. Everything below is the exact sample ui.Styleguide returns, which is the sample ui tests hold against tokens.css."}}</p>
<p class="ds-lead">{{P "Each example is live, but its links go nowhere."}}</p>
{{range .Idioms}}
{{.Marker}}
<article class="ds-partial" id="{{.ID}}" data-ds-anchor>
<h2 class="ds-partial__name rst-mono">{{.Name}}</h2>
{{if .Blurb}}<p class="ds-lead">{{.Blurb}}</p>{{end}}
{{.Rule}}
<div class="ds-sample">{{template "ds-view" .Preview}}
{{if .DemoHref}}<p class="ds-note"><a href="{{.DemoHref}}" target="_blank" rel="noopener">{{.DemoLabel}}</a><span class="rst-sr-only"> ({{P "opens in a new tab"}})</span>.</p>{{end}}</div>
</article>
{{end}}
{{end}}`

const shellsBody = `{{define "ds-body-shells"}}
<p class="ds-lead">{{P "The page frame itself. rastrillo new writes one of these as templates/layout.html; a page fills its content hole and overrides whichever chrome blocks it needs. Each demo below is a whole page at its own URL: open one to see it at full width, where the sidebar's rail and the topbar's footer actually have room."}}</p>
{{range .Shells}}
<section class="ds-shell" id="{{.ID}}" data-ds-anchor>
<h2 class="ds-shell__name">{{.Name}}</h2>
<p class="ds-lead">{{.Blurb}}</p>
{{template "ds-view" .Preview}}
<p><a rst-btn href="{{.Href}}" target="_blank" rel="noopener">{{P "Open the {shell} shell" "shell" .Name}}<span class="rst-sr-only"> ({{P "opens in a new tab"}})</span></a></p>
</section>
{{end}}
{{end}}`

// shellCommon fills the blocks every shell declares, the stage
// included: the document's language, direction and title, and head.
// It is prepended to whichever override set a shell gets, so the two
// sets cannot drift on the blocks they share.
//
// head is the newest of them and the reason it exists: this demo is a
// real page a reader can open in a tab of its own, and a reader who
// chose Dark in the gallery should still be in Dark when they get
// here. gallery.js reads that choice out of localStorage and applies
// it before the first paint — the same script, on the same origin,
// doing the same job it does on the gallery. It is also the honest
// answer to "what is the head block FOR": an app's favicon, an app's
// stylesheet, an app's one script that has to run early.
//
// gallery.css stays out (backLinkStyle says why).
const shellCommon = `
{{define "head"}}<script src="{{asset "gallery.js"}}"></script>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
`

// backLinkStyle holds the way back in a shell demo's foot, the one
// gallery control on the demo, to the target floor: as a line of the
// foot's 12px text it was 15 to 19px tall, and the foot is the
// framework's and has no floor of its own. It is written on the link
// rather than in gallery.css, which the demos do not load: that file
// carries the gallery's own page layout, and its grid for the pinned
// bar took hold of the sidebar demo, dropping main below an empty
// strip the demo has no bar for. A demo shows the shell exactly as an
// app gets it.
const backLinkStyle = `align-items: center; display: inline-flex; min-block-size: var(--rst-target);`

// shellTemplate fills the rest of the blocks the chrome shells leave
// open. The blocks a given shell does not declare are simply never
// executed, so one override set covers all four of them; stage has its
// own, below.
const shellTemplate = `
{{define "view"}}{{if eq .View "index"}}index{{else}}page{{end}}{{end}}
{{define "up"}}{{.Up}}{{end}}
{{define "brand"}}<a rst-shell-brand href="{{.Index}}">rastrillo</a>{{end}}
{{define "nav"}}<a id="nav-posts" href="{{.PageHref}}" aria-current="page">Posts</a><a id="nav-comments" href="{{.PageHref}}">Comments</a><a id="nav-settings" href="{{.PageHref}}">Settings</a>{{end}}
{{define "account"}}{{.Account}}{{end}}
{{define "profile"}}{{.Profile}}{{end}}
{{define "locale"}}<details rst-dropdown rst-locale name="rst-menus"><summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span></summary><div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>{{end}}
{{define "foot"}}<a style="` + backLinkStyle + `" href="{{.Index}}">{{P "Back to the design system"}}</a>{{end}}
{{define "content"}}
{{template "page-header" dict "Title" "Posts" "Sub" (P "A representative screen, so the chrome around it has something to frame.") "ActionHref" "#" "ActionLabel" (P "Write a post") "ActionIcon" "plus"}}
<div rst-box-head><h2>{{P "This page"}}</h2><a rst-btn href="{{.Index}}">{{P "Back to the design system"}}</a></div>
<section rst-box><p>{{P "This is the {shell} shell, one of the shells ui.Layout ships. A screen is a column: a page header, then a section heading and its card, then the next one. Everything you see here is the shell, tokens.css and two partials." "shell" .Name}}</p></section>
<div rst-box-head><h2>Recent</h2></div>
<div rst-card style="--rst-cols: 2fr 110px 32px">
<div rst-lrow="head"><span>Post</span><span class="rst-m-hide">Status</span><span></span></div>
<div rst-lrow><a class="rst-nm" href="#">Release notes, August<small>Published 2 August</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Published")}}</span><span></span></div>
<div rst-lrow><a class="rst-nm" href="#">Why we moved off the old runner<small>{{P "Draft"}}</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Draft")}}</span><span></span></div>
</div>
<p rst-count-line>Displaying <strong>1–2</strong> of <strong>412</strong></p>
{{end}}
`

// stageShellTemplate fills the stage shell's blocks for its demo: the
// sign-in card in its Ask state, and a foot with the way back, in the
// <footer rst-stage-foot> the shell's foot block documents — a bare
// link there would sit outside every landmark and miss the foot's
// styling. The backdrop block is left alone, so the demo shows the
// default art.
const stageShellTemplate = `
{{define "foot"}}<footer rst-stage-foot><a style="` + backLinkStyle + `" href="{{.Index}}">{{P "Back to the design system"}}</a></footer>{{end}}
{{define "content"}}{{template "signin" (dict "State" .Signin "Brand" .Brand "Preview" true)}}{{end}}
`

// modalTemplate is the modal demo page: the sample's structure with
// real addresses. It is a hand-written document rather than one of the
// shells because the idiom is body-level — the backdrop wraps the
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
// never paints, and [rst-modal-overlay] stays the scrim. Its
// aria-labelledby points at the panel's own <h2>, the same way the
// sample's does — a dialog role with no name is an axe failure, and the
// heading is already the text that names this panel.
const modalTemplate = `{{define "ds-modal"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>{{.Title}}</title>
<link rel="stylesheet" href="{{asset "tokens.css"}}">
<link rel="stylesheet" href="{{asset "theme.css"}}">
</head>
<body>
<div rst-backdrop inert>
<main rst-page id="main">
{{template "page-header" dict "Title" "Settings" "Sub" (P "The page the modal opened over. It is marked inert, so nothing in here takes focus or reaches a screen reader while the panel is up.")}}
<div rst-box-head><h2>Account</h2></div>
<section rst-box><p>{{P "Modals get their own URL."}}</p></section>
</main>
</div>
<div rst-modal-overlay>
  <dialog rst-modal-panel open aria-labelledby="modal-title">
    <nav>
      <a href="{{.Self}}" aria-current="page">Profile</a>
      <a href="{{.Self}}">Billing</a>
      <a href="{{.Self}}">Notifications</a>
    </nav>
    <section>
      <a rst-modal-close href="{{.Index}}" aria-label="{{P "Close settings"}}">✕</a>
      <h2 id="modal-title">Profile</h2>
      <p>{{P "Update the name and photo shown across the account."}}</p>
      <p>{{P "Close link designed to work without JS."}}</p>
      <p>{{P "In an application the ✕ would return you to the screen in the backdrop."}}</p>
      <p><a rst-btn href="{{.Index}}">{{P "Back to the design system"}}</a></p>
    </section>
  </dialog>
</div>
</body>
</html>
{{end}}`
