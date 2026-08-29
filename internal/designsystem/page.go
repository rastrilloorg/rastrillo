package designsystem

import (
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

// ── The page model ───────────────────────────────────────────────────
//
// Every URL on a page is an absolute path under mountPath: an asset is
// /design-system/tokens.css, a shell demo is
// /design-system/ink/en/shells/topbar.html, wherever the page holding
// the link sits in the tree. There used to be a pair of "../../" depth
// prefixes here instead; designsystem.go's mountPath comment has the
// edge behaviour that made them wrong.
//
// The one thing absolute paths cost: a page can no longer link to
// "itself" as a bare filename, so the theme and locale switchers point
// their current entry at that page's canonical address. From
// index.html that address is ink/en/index.html — a different file
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
	State string
	Note  string
	HTML  template.HTML
}

type partialView struct {
	Name   string
	Marker template.HTML
	Blurb  string
	Wrap   wrapper
	States []stateView
}

// IsList, IsForm and IsBox keep the wrapper decision out of the
// template, which cannot compare an unexported constant.
func (p partialView) IsList() bool { return p.Wrap == wrapList }
func (p partialView) IsForm() bool { return p.Wrap == wrapForm }
func (p partialView) IsBox() bool  { return p.Wrap == wrapBox }

type familyView struct {
	Title    string
	Blurb    string
	Partials []partialView
}

type idiomView struct {
	Name   string
	Marker template.HTML
	Rule   template.HTML // the #98 callout this idiom carries, if any
	HTML   template.HTML
	Blurb  string

	// Source, Why, DemoLabel and DemoHref are the escape route for the
	// samples that cannot be rendered inline on a gallery page. A shell
	// sample is a whole page frame, <main id="main"> and all, and this
	// page already has one: rendering it inline nests a main inside a
	// main (invalid HTML) and puts three main landmarks on the page.
	// The modal sample is worse — its overlay is position: fixed, so it
	// rendered as an open modal covering the whole gallery the moment
	// the page loaded. Those samples are shown as escaped source —
	// which is what a reader wants from them anyway, since they are
	// markup to copy rather than a component to look at — beside a link
	// to the demo page where the same markup is real. See sourceIdioms.
	Source    string
	Why       string // why it is source here and not markup
	DemoLabel string // the link's own words
	DemoHref  string
}

type shellView struct {
	Name  string
	Href  string
	Blurb string
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

	// Mount is mountPath, so the template can write
	// href="{{.Mount}}/tokens.css" without knowing it.
	Mount string

	// Sub is the page's opening sentence, built in Go rather than in
	// the template because it interleaves prose with two names that
	// have to carry markup of their own: the theme in <strong>, and the
	// language in a <strong lang=…> so a screen reader says the autonym
	// in its own voice. See proseMarkup.
	Sub template.HTML

	Themes    []navLink
	Schemes   []schemeButton
	Locales   []localeLink
	Font      tokenRow
	Colours   []tokenGroup
	Structure []tokenGroup
	Families  []familyView
	Idioms    []idiomView
	Shells    []shellView
}

// ── Building one page ────────────────────────────────────────────────

// renderIndex builds one theme × locale index page. Where the page ends
// up in the tree no longer changes a byte of it — every link it carries
// is absolute — so theme and locale are the whole of its identity.
func renderIndex(theme, locale string) ([]byte, error) {
	tmpl, err := partialTree(locale)
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	if _, err := tmpl.Parse(indexTemplate); err != nil {
		return nil, fmt.Errorf("parsing the page: %w", err)
	}
	// Every hand-written sample is parsed into the tree before anything
	// executes: html/template refuses to Clone or Parse into a tree that
	// has already run, so there is no lazily-add-one-later option here.
	if err := parseRawSamples(tmpl); err != nil {
		return nil, err
	}

	colours, font, err := themePalette(theme)
	if err != nil {
		return nil, err
	}
	structure, err := structuralGroups()
	if err != nil {
		return nil, err
	}
	families, err := buildFamilies(tmpl, locale)
	if err != nil {
		return nil, err
	}
	idioms, err := buildIdioms(tmpl, theme, locale)
	if err != nil {
		return nil, err
	}

	localeName := rastrillo.BaseCatalogs()[locale]["rastrillo.ui.locale_name"]
	view := pageView{
		Theme: theme, Locale: locale, Dir: rastrillo.Dir(locale),
		LocaleName: localeName,
		Mount:      mountPath,
		Sub:        subhead(locale, theme, localeName),
		Themes:     themeLinks(theme, locale),
		Schemes:    schemeButtons(locale),
		Locales:    localeLinks(theme, locale),
		Font:       font,
		Colours:    localiseGroups(locale, colours),
		Structure:  localiseGroups(locale, structure),
		Families:   families,
		Idioms:     idioms,
		Shells:     shellViews(theme, locale),
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "ds-index", view); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func themeLinks(theme, locale string) []navLink {
	out := make([]navLink, 0, len(ui.ThemeNames()))
	for _, name := range ui.ThemeNames() {
		out = append(out, navLink{
			Label:   name,
			Href:    indexHref(name, locale),
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
		"Every partial, class idiom and design token the framework ships, on one page. The theme is {theme} and the components speak {language}.",
		"theme", template.HTML("<strong>"+template.HTMLEscapeString(theme)+"</strong>"),
		"language", template.HTML(`<strong lang="`+template.HTMLEscapeString(locale)+`">`+template.HTMLEscapeString(localeName)+"</strong>"),
	)
}

// indexHref is one theme × locale page's canonical address. Every
// switcher entry uses it, the current one included: the tree root is a
// copy of ink/en, so "the page you are on" and "this page's address"
// are the same document even when they are two files.
func indexHref(theme, locale string) string {
	return mountPath + "/" + theme + "/" + locale + "/index.html"
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
// Every entry, on an index page and on a shell demo alike, points at
// that locale's index page for the current theme — a shell demo's
// switcher sends you back to the gallery in the language you picked,
// which is where the language switcher is a component worth looking at.
func localeLinks(theme, locale string) []localeLink {
	catalogs := rastrillo.BaseCatalogs()
	out := make([]localeLink, 0, len(rastrillo.BaseLocales()))
	for _, code := range rastrillo.BaseLocales() {
		href := indexHref(theme, code)
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

func shellViews(theme, locale string) []shellView {
	blurbs := map[string]string{
		"column":  "The plain centred page every scaffolded app starts on: a skip link, a title, and the content column.",
		"topbar":  "Brand, navigation and an account menu across the top, with a footer under the page.",
		"sidebar": "A navigation rail beside the page, collapsing below 800px into a details disclosure. No JavaScript.",
	}
	out := make([]shellView, 0, len(ui.LayoutNames()))
	for _, name := range ui.LayoutNames() {
		out = append(out, shellView{
			Name:  name,
			Href:  shellHref(theme, locale, name),
			Blurb: proseIn(locale, blurbs[name]),
		})
	}
	return out
}

// shellHref is one full-page shell demo's address.
func shellHref(theme, locale, shell string) string {
	return mountPath + "/" + theme + "/" + locale + "/shells/" + shell + ".html"
}

// modalHref is the modal demo's address — which is the whole point of
// the demo. A modal is its own URL, so the sample that shows one open
// has to be a page you navigate to, not a fragment of a gallery.
func modalHref(theme, locale string) string {
	return mountPath + "/" + theme + "/" + locale + "/modal.html"
}

// ── Partial samples ──────────────────────────────────────────────────

// buildFamilies renders every sample in samples.go, then sweeps up any
// partial ui defines that no family claims. A partial with no sample is
// a gap in the documentation, not a reason to drop it off the page: it
// gets its own section, its marker comment (so the coverage gate still
// sees it), and a visible note saying it has no sample data yet.
func buildFamilies(tmpl *template.Template, locale string) ([]familyView, error) {
	claimed := map[string]bool{}
	out := make([]familyView, 0, len(families())+1)
	for _, fam := range families() {
		view := familyView{Title: proseIn(locale, fam.Title), Blurb: proseIn(locale, fam.Blurb)}
		for _, doc := range fam.Partials {
			if tmpl.Lookup(doc.Name) == nil {
				return nil, fmt.Errorf("samples.go documents %q, which ui does not define", doc.Name)
			}
			claimed[doc.Name] = true
			pv := partialView{Name: doc.Name, Marker: marker("partial", doc.Name), Blurb: proseIn(locale, doc.Blurb), Wrap: doc.Wrap}
			for i, s := range doc.States {
				html, err := renderSample(tmpl, doc.Name, i, s, locale)
				if err != nil {
					return nil, fmt.Errorf("%s (%s): %w", doc.Name, s.State, err)
				}
				pv.States = append(pv.States, stateView{
					State: proseIn(locale, s.State),
					Note:  proseIn(locale, s.Note),
					HTML:  html,
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
			Blurb: proseIn(locale, "Partials ui defines that samples.go has not been taught to render yet. They are listed rather than dropped, because a component nobody documented is still a component apps can call."),
		}
		for _, name := range orphans {
			pv := partialView{Name: name, Marker: marker("partial", name), Blurb: proseIn(locale, "No sample data yet — add one in internal/designsystem/samples.go.")}
			// Many partials guard every optional field, so an empty
			// dict renders something honest. One that does not simply
			// shows its heading and the note above.
			if html, err := renderSample(tmpl, name, 0, sample{Data: map[string]any{}}, locale); err == nil {
				pv.States = append(pv.States, stateView{State: proseIn(locale, "Rendered from an empty data value"), HTML: html})
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
	"shell-topbar":  "The topbar shell's own chrome, as markup. Layout ships it as a whole template — this is what is inside.",
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
			"Do not compose a heading, a paragraph and a button side by side in a flex row. " +
			"Horizontal arrangement is reserved for the idioms that ship it: rst-box-head, rst-field-row, rst-lbar, rst-lrow cells, rst-seg-tabs.",
	},
}

// sourceIdiom is one sample the gallery shows as escaped source: why it
// is not rendered inline, the words its link wears, and the page where
// the same markup is real.
type sourceIdiom struct {
	Why   string
	Label string
	Href  func(theme, locale string) string
}

// sourceIdioms are the three samples that cannot be rendered inline on
// a gallery page. See idiomView.Source for the shape of the problem;
// each Why below is the particular one.
var sourceIdioms = map[string]sourceIdiom{
	"shell-topbar": {
		Why:   "Shown as source, not rendered: this sample is a whole page frame with its own main landmark, and it is sitting inside one.",
		Label: "Open the topbar shell demo, where the same markup is a real page",
		Href:  func(theme, locale string) string { return shellHref(theme, locale, "topbar") },
	},
	"shell-sidebar": {
		Why:   "Shown as source, not rendered: this sample is a whole page frame with its own main landmark, and it is sitting inside one.",
		Label: "Open the sidebar shell demo, where the same markup is a real page",
		Href:  func(theme, locale string) string { return shellHref(theme, locale, "sidebar") },
	},
	"modal": {
		Why: "Shown as source, not rendered: the overlay is fixed to the viewport, so rendering it here opened a modal over the whole gallery the moment this page loaded. " +
			"That is the idiom telling you what it wants — a modal is its own URL, and this is not it.",
		Label: "See it live at the URL it belongs to",
		Href:  modalHref,
	},
}

// buildIdioms renders ui.Styleguide in sorted order. The samples are
// complete HTML with no template actions, so they go onto the page as
// they are — the point is that the page shows the same bytes the ui
// tests hold against tokens.css.
func buildIdioms(tmpl *template.Template, theme, locale string) ([]idiomView, error) {
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
			Marker: marker("idiom", name),
			Blurb:  proseIn(locale, idiomBlurbs[name]),
		}
		if src, ok := sourceIdioms[name]; ok {
			view.Source = samples[name]
			view.Why = proseIn(locale, src.Why)
			view.DemoLabel = proseIn(locale, src.Label)
			view.DemoHref = src.Href(theme, locale)
		} else {
			view.HTML = template.HTML(samples[name])
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
func renderShell(theme, locale, shell string) ([]byte, error) {
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
		return mountPath + "/" + name
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
		Index:   indexHref(theme, locale),
		Locales: localeLinks(theme, locale),
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
func renderModal(theme, locale string) ([]byte, error) {
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
		Mount:  mountPath,
		Index:  indexHref(theme, locale),
		Self:   modalHref(theme, locale),
	})
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

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
.ds-partial > h4 { font-size: var(--rst-fs-base); margin: 0 0 var(--rst-sp-1); }
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
.ds-frame { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); block-size: 26rem; display: block; inline-size: 100%; margin: var(--rst-sp-3) 0; }
.ds-shell { margin: var(--rst-sp-5) 0; }
.ds-shell h3 { font-size: 1.05rem; margin: 0 0 var(--rst-sp-1); }
.ds-src { background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: var(--rst-radius); margin: var(--rst-sp-3) 0; overflow-x: auto; padding: var(--rst-sp-4); }
.ds-src code { white-space: pre; }
`

// indexTemplate is the whole design-system page. Two things in it are
// load-bearing beyond layout:
//
//   - The marker comments. Every partial section emits
//     <!-- partial: NAME --> and every idiom <!-- idiom: NAME -->, and
//     designsystem_test.go's coverage gates grep for exactly those. A
//     gate that grepped for a class or a rendered string instead would
//     fail every time a partial's markup was tidied, which is the
//     opposite of what it is for.
//   - Every href and src the page itself owns is absolute under
//     .Mount, because the edge serves this tree's directory indexes
//     without a trailing slash and a relative path resolves against the
//     wrong base there. TestEveryPageIsAWholeLocalisedDocument holds it,
//     and holds every such link to a path the tree actually renders.
//     The hrefs inside the samples are the exception: they are content,
//     sample data written to read like a real app, and they point at
//     routes no static site has.
const indexTemplate = `{{define "ds-index"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{P "rastrillo design system"}} — {{.Theme}}</title>
<link rel="stylesheet" href="{{.Mount}}/tokens.css">
<link rel="stylesheet" href="{{.Mount}}/theme-{{.Theme}}.css">
<style>` + dsCSS + `</style>
<script src="{{.Mount}}/gallery.js"></script>
<script defer src="{{.Mount}}/rastrillo.js"></script>
<script defer src="{{.Mount}}/select.js"></script>
<script defer src="{{.Mount}}/datetime.js"></script>
</head>
<body>
<a class="rst-skip" href="#main">{{T "rastrillo.ui.shell_skip"}}</a>

<header class="ds-chrome">
  <nav class="rst-seg-tabs" aria-label="{{P "Theme"}}">{{range .Themes}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
  <div class="ds-scheme" role="group" aria-label="{{P "Colour scheme"}}">{{range .Schemes}}<button type="button" data-ds-scheme="{{.Value}}" aria-pressed="{{.Pressed}}">{{.Label}}</button>{{end}}</div>
  <details class="rst-dropdown rst-locale" name="rst-menus">
    <summary>{{T "rastrillo.ui.shell_language"}}<span class="rst-caret" aria-hidden="true">{{icon "chevron-down"}}</span><span class="rst-sr-only">{{P ", currently {language}" "language" .LocaleName}}</span></summary>
    <div class="rst-dropdown__menu">{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div>
  </details>
</header>

<main class="rst-page" id="main">

<header class="rst-page-header">
  <div class="rst-page-header__titles">
    <h1>{{P "rastrillo design system"}}</h1>
    <p class="rst-page-header__sub">{{.Sub}}</p>
  </div>
</header>

<div class="ds-switch">
  <nav class="rst-seg-tabs" aria-label="{{P "Sections"}}"><a href="#tokens">{{P "Tokens"}}</a><a href="#partials">{{P "Partials"}}</a><a href="#idioms">{{P "Class idioms"}}</a><a href="#shells">{{P "Shells"}}</a></nav>
</div>

{{template "callout" dict "Tone" "info" "Title" (P "The links in these samples go nowhere") "Body" (P "This is a static page. Every href below is sample data chosen to read like a real application, so the markup is the markup an app would ship — but nothing here is served by anything, and following one lands on a missing page.")}}

<div class="ds-head"><h2 id="tokens">{{P "Tokens"}}</h2></div>
<p class="ds-lead">{{P "The custom properties every component paints itself with. Colour and the type family come from the theme (themes/{theme}.css); the type scale and the spacing steps are structure and come from tokens.css, the same on every theme." "theme" .Theme}}</p>
<p class="ds-swatch-note">{{P "The values printed here are the light ones. The dark set is authored by hand in the same file — never inverted — and ui/contrast_test.go holds every documented pair in both sets to the WCAG 2.2 AA floors: 4.5:1 for text, 3:1 for control borders. The chips themselves are painted with var(), so they follow whichever scheme you are reading in, and they will not match the printed values in dark mode."}}</p>
{{range .Colours}}
<h3 class="ds-sub">{{.Title}}</h3>
<ul class="ds-toks">{{range .Rows}}<li class="ds-tok">{{if .Preview}}<span class="ds-chip" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}
<h3 class="ds-sub">{{P "Type family"}}</h3>
<ul class="ds-toks"><li class="ds-tok"><span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Font.Name}}</span><span class="ds-tok__value">{{.Font.Value}}</span></span></li></ul>
{{range .Structure}}
<h3 class="ds-sub">{{.Title}}</h3>
<ul class="ds-toks">{{$kind := .Kind}}{{range .Rows}}<li class="ds-tok">{{if eq $kind "type"}}<span class="ds-type" style="{{.Preview}}">Ag</span>{{else if eq $kind "space"}}<span class="ds-bar" style="{{.Preview}}"></span>{{else}}<span class="ds-chip ds-chip--fill" style="{{.Preview}}"></span>{{end}}<span class="ds-tok__text rst-mono"><span class="ds-tok__name">{{.Name}}</span><span class="ds-tok__value">{{.Value}}</span></span></li>{{end}}</ul>
{{end}}

<div class="ds-head"><h2 id="partials">{{P "Partials"}}</h2></div>
<p class="ds-lead">{{P "Every template partial ui ships, in the states a real screen puts it in. Each one takes exactly one data value, built inline with dict. Forms sit in a padded rst-box and rows sit in a rst-list, because that is what these partials assume — see the two rules under Class idioms below. One thing a gallery cannot help: page-header and error-page each own an h1, so this page carries several, where a real screen has exactly one."}}</p>
<p class="ds-note">{{P "Sample content stays English on every page: the names, the routes and the labels in these samples are stand-ins, and translating them would suggest the framework ships those words. The shell and modal demos are the other way round — they impersonate a real application, so their chrome speaks the language you chose."}}</p>
{{range .Families}}
<section class="ds-family">
<h3>{{.Title}}</h3>
<p class="ds-lead">{{.Blurb}}</p>
{{range .Partials}}
{{.Marker}}
<article class="ds-partial">
<h4 class="rst-mono">{{.Name}}</h4>
<p class="ds-lead">{{.Blurb}}</p>
{{$wrapList := .IsList}}{{$wrapForm := .IsForm}}{{$wrapBox := .IsBox}}
{{range .States}}
<div class="ds-sample">
{{if .State}}<p class="ds-state">{{.State}}</p>{{end}}
{{if $wrapList}}<div class="rst-list">{{.HTML}}</div>{{else if $wrapForm}}<section class="rst-box"><form class="rst-form" method="post" action="#">{{.HTML}}</form></section>{{else if $wrapBox}}<section class="rst-box">{{.HTML}}</section>{{else}}{{.HTML}}{{end}}
{{if .Note}}<p class="ds-note">{{.Note}}</p>{{end}}
</div>
{{end}}
</article>
{{end}}
</section>
{{end}}

<div class="ds-head"><h2 id="idioms">{{P "Class idioms"}}</h2></div>
<p class="ds-lead">{{P "The shapes a partial cannot be, because they wrap a body only the caller knows: the section card, the data grid, the disclosure menu, the shells' own chrome. tokens.css ships the classes and an app writes the markup. Everything below is the exact sample ui.Styleguide returns, which is the sample ui tests hold against tokens.css — copy from here."}}</p>
{{range .Idioms}}
{{.Marker}}
<article class="ds-partial">
<h4 class="rst-mono">{{.Name}}</h4>
{{if .Blurb}}<p class="ds-lead">{{.Blurb}}</p>{{end}}
{{.Rule}}
{{if .Source}}<pre class="ds-src rst-mono"><code>{{.Source}}</code></pre>
<p class="ds-note">{{.Why}} <a href="{{.DemoHref}}">{{.DemoLabel}}</a>.</p>
{{else}}<div class="ds-sample">{{.HTML}}</div>{{end}}
</article>
{{end}}

<div class="ds-head"><h2 id="shells">{{P "Shells"}}</h2></div>
<p class="ds-lead">{{P "The page frame itself. rastrillo new writes one of these as templates/layout.html; a page fills its content hole and overrides whichever chrome blocks it needs. Each demo below is a whole page at its own URL — open one to see it at full width, where the sidebar's rail and the topbar's footer actually have room."}}</p>
{{range .Shells}}
<section class="ds-shell">
<h3>{{.Name}}</h3>
<p class="ds-lead">{{.Blurb}}</p>
<iframe class="ds-frame" src="{{.Href}}" title="{{P "The {shell} shell, rendered at full page" "shell" .Name}}" loading="lazy"></iframe>
<p><a class="rst-btn" href="{{.Href}}">{{P "Open the {shell} shell" "shell" .Name}}</a></p>
</section>
{{end}}

</main>
</body>
</html>
{{end}}`

// shellTemplate fills every block the three shells leave open. The
// blocks a given shell does not declare are simply never executed, so
// one override set covers all three.
const shellTemplate = `
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
<section class="rst-box"><p>{{P "One response rendered this screen and the panel over it. That is the whole idiom: the modal is a URL, the page underneath is what a Close click returns to, and neither of them is client state."}}</p></section>
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
      <p>{{P "There is no JavaScript on this page. Closing is the ✕ above, a plain link; the tabs on the left are plain links too, which is why they stay on this URL instead of pretending to load a section."}}</p>
      <p>{{P "In an application the ✕ would return you to the screen in the backdrop. Here it returns you to the gallery you opened this demo from, because that is the page that exists."}}</p>
      <p><a class="rst-btn" href="{{.Index}}">{{P "Back to the design system"}}</a></p>
    </section>
  </dialog>
</div>
</body>
</html>
{{end}}`
