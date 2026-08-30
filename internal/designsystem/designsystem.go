// Package designsystem renders rastrillo.org/design-system: one static
// page per theme × locale showing every partial, every class idiom and
// every design token the framework ships, plus a full-page demo of each
// of the three shells and one of the modal route.
//
// It exists because a component library nobody can look at is a
// specification, not a library. The page is the only place the whole
// vocabulary is on screen at once — every tone beside every other tone,
// every field in its error state, the RTL and CJK renderings of the same
// markup — and it is the only place the two enhanced controls
// (select.js's filterable combobox, datetime.js's natural-language date
// field) actually run.
//
// Render returns the whole tree in memory, keyed by path relative to
// docs/design-system. `go generate ./...` writes it; the
// TestDesignSystemIsCurrent gate holds the committed tree to what this
// package renders, so a partial that changes shows up as a diff in every
// page that renders it. That is the point of committing it rather than
// building it on the website.
//
// Determinism is a contract, not a nicety: 180 pages regenerated on
// every partial change are only reviewable if the diff is the change.
// Every map in here — Styleguide's samples, BaseCatalogs, the parsed
// token blocks — is sorted before anything reaches output.
package designsystem

import (
	_ "embed"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strings"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

// galleryJS is the gallery's own script, embedded from the file beside
// this one so it is edited as JavaScript — with a syntax highlighter
// and a linter — rather than as a Go string constant.
//
// It lives here and not in ui/ because it is not part of the framework:
// no scaffold writes it, no app receives it, and the only page that
// loads it is the one this package renders. Its header comment is its
// contract; TestGalleryScriptStaysInertAndFirstParty holds the two
// honest.
//
//go:embed gallery.js
var galleryJS []byte

// GalleryJS returns gallery.js's raw bytes. Exported for the same
// reason ui.ShimJS is: the gates read it, and a caller embedding this
// tree somewhere else needs the asset as well as the pages.
func GalleryJS() []byte { return append([]byte(nil), galleryJS...) }

// mountPath is where this tree is served: rastrillo.org/design-system.
// Every URL the renderer emits — stylesheet, script, iframe, switcher,
// shell demo, back-link — is an absolute path under it.
//
// Absolute rather than relative because the CARLOS static edge serves a
// directory index at its slash-less URL as a 200 with no redirect:
// /design-system and /design-system/ both return the same document, but
// a relative href in it resolves against a different base on each, so
// the slash-less visit loaded no stylesheet and every link pointed one
// directory too high. That was the live bug. The cost is that the tree
// only works at this one mount path, which is the path the site serves
// it from; the constant is the whole of that binding.
const mountPath = "/design-system"

// Render builds the whole design-system tree in memory: path relative to
// docs/design-system → file content.
//
//	index.html                            day, en, assets at the tree root
//	<theme>/<locale>/index.html           the Overview, 36 times
//	<theme>/<locale>/tokens.html          and one page per section beside
//	<theme>/<locale>/components.html      it: see pageKinds() in page.go,
//	<theme>/<locale>/primitives.html      which is the whole of that list
//	<theme>/<locale>/shells.html
//	<theme>/<locale>/modal.html           36 modal demos, one per gallery
//	<theme>/<locale>/shells/<shell>.html  108 full-page shell demos
//	tokens.css theme-<theme>.css          the stylesheets, once each
//	rastrillo.js select.js datetime.js    the framework's three scripts
//	gallery.js                            the gallery's own script, once
//
// The assets are shared by every page rather than copied per theme, so
// the tree's size is the documents plus one copy of the library.
func Render() (map[string][]byte, error) {
	out := map[string][]byte{
		"tokens.css":   ui.TokensCSS(),
		"rastrillo.js": ui.ShimJS(),
		"select.js":    ui.SelectJS(),
		"datetime.js":  ui.DatetimeJS(),
		"gallery.js":   GalleryJS(),
	}
	for _, theme := range ui.ThemeNames() {
		css, ok := ui.ThemeCSS(theme)
		if !ok {
			return nil, fmt.Errorf("designsystem: no theme %q", theme)
		}
		out["theme-"+theme+".css"] = css
	}

	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			dir := theme + "/" + locale + "/"
			pages, err := renderGallery(theme, locale)
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir, err)
			}
			for file, page := range pages {
				out[dir+file] = page
			}
			modal, err := renderModal(theme, locale)
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir+"modal.html", err)
			}
			out[dir+"modal.html"] = modal
			for _, shell := range ui.LayoutNames() {
				demo, err := renderShell(theme, locale, shell)
				if err != nil {
					return nil, fmt.Errorf("designsystem: %s: %w", dir+"shells/"+shell+".html", err)
				}
				out[dir+"shells/"+shell+".html"] = demo
			}
		}
	}

	// The tree root is the default theme in English again — day/en since
	// v2, and named through ui.ThemeNames() rather than spelled out, so
	// renaming the default theme moves the root with it. It used to be a
	// second render at a different depth, because every path on it needed
	// rewriting for the shallower directory; with absolute paths there is
	// nothing left to rewrite, so the two files are the same bytes and
	// saying so is more honest than a second call that could only ever
	// return them. Copied, not aliased: callers get one slice per path.
	rootPage := RootTheme() + "/en/index.html"
	nested, ok := out[rootPage]
	if !ok {
		return nil, fmt.Errorf("designsystem: index.html: no %s page to serve as the tree root", rootPage)
	}
	out["index.html"] = append([]byte(nil), nested...)
	return out, nil
}

// RootTheme is the theme the tree root serves: the first name
// ui.ThemeNames() reports, which is the same theme rastrillo new
// scaffolds by default. Exported so the gates can say "the root index is
// this theme's English page" without repeating the name.
func RootTheme() string { return ui.ThemeNames()[0] }

// translator binds T (and Tf, which ui.WithT sets to the same function)
// to one locale's framework catalog: the locale's own value, English if
// the locale is missing the key, and the key itself if English is too —
// the same three-step fallback rastrillo.Locales.T walks, minus the app
// layers a static page has none of.
func translator(locale string) func(string, ...any) string {
	catalogs := rastrillo.BaseCatalogs()
	own, en := catalogs[locale], catalogs["en"]
	return func(key string, args ...any) string {
		s, ok := own[key]
		if !ok {
			if s, ok = en[key]; !ok {
				return key
			}
		}
		return interpolate(s, args)
	}
}

// interpolate substitutes {name} placeholders from alternating
// name/value arguments. Reimplemented here rather than reached for in
// the root package, where the equivalent walk is unexported and takes a
// Locales receiver this package has no reason to build.
//
// A placeholder with no matching argument is left verbatim, for the
// reason locale.go gives: a translator's typo should show up in the page
// rather than silently delete half a sentence.
func interpolate(s string, args []any) string {
	if len(args) == 0 || !strings.Contains(s, "{") {
		return s
	}
	values := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok {
			values[name] = fmt.Sprint(args[i+1])
		}
	}
	var b strings.Builder
	for {
		open := strings.Index(s, "{")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		shut := strings.Index(s[open:], "}")
		if shut < 0 {
			b.WriteString(s)
			return b.String()
		}
		shut += open
		if v, ok := values[s[open+1:shut]]; ok {
			b.WriteString(s[:open])
			b.WriteString(v)
		} else {
			b.WriteString(s[:shut+1])
		}
		s = s[shut+1:]
	}
}

// partialTree parses ui's whole partial set with T bound to one locale.
// A fresh tree per locale rather than one tree cloned per request: this
// runs 72 times at generate time — an index page and a modal demo each
// — so the Clone discipline ui.FuncsWith documents buys nothing here.
func partialTree(locale string) (*template.Template, error) {
	return template.New("designsystem").
		Funcs(galleryFuncs(locale)).
		ParseFS(ui.Templates(), "*.html")
}

// galleryFuncs is ui's own func map plus P, the gallery's own
// translator. T resolves the framework's catalog — the strings a
// component emits — and P resolves prose.go, the strings this page says
// in its own voice. Two functions rather than one table because the two
// sets have different owners: an app can override a rastrillo.ui.* key,
// and nothing outside this package has any business in prose.go.
func galleryFuncs(locale string) template.FuncMap {
	funcs := ui.Funcs(ui.WithT(translator(locale)))
	funcs["P"] = func(key string, args ...any) string { return proseIn(locale, key, args...) }
	return funcs
}

// partialNames lists the partials ui defines, sorted: the templates that
// carry a {{define}}, not the per-file templates ParseFS also creates.
// It is how the page discovers a partial nobody wrote a sample for — see
// buildFamilies' orphan sweep.
//
// Deliberately read off a tree holding nothing but ui's partials, not
// off the page's own tree. The page adds templates of its own (the page
// body, one per hand-written sample), and an orphan sweep over that tree
// would treat the page as a partial with no sample data and render it
// inside itself. It did, once.
func partialNames() ([]string, error) {
	tmpl, err := template.New("partials").Funcs(ui.Funcs()).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing partials: %w", err)
	}
	var names []string
	for _, t := range tmpl.Templates() {
		name := t.Name()
		if name == "" || name == "partials" || strings.HasSuffix(name, ".html") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ── Token parsing ────────────────────────────────────────────────────
//
// The same regex family ui/contrast_test.go uses, copied rather than
// imported: test code is not importable, and a stylesheet parser small
// enough to read twice is cheaper than the seam it would take to share
// one. If the two ever disagree, contrast_test.go is the authority — it
// is the one holding a WCAG floor.

var declPattern = regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:\s*([^;]+);`)

// colourValue matches a value that is a colour on its own, so it can be
// shown as a chip. A shadow (0 8px 24px rgba(…)) is deliberately not
// one: it contains a colour but is not one, and it gets its own preview.
var colourValue = regexp.MustCompile(`^(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{6}|rgba?\([^()]*\)|hsla?\([^()]*\))$`)

// blockBody returns the brace-matched contents following header (which
// must end in "{"). Theme files nest at most one level and no value in
// them contains a brace, so depth counting is enough.
func blockBody(css, header string) (string, error) {
	i := strings.Index(css, header)
	if i < 0 {
		return "", fmt.Errorf("no %q block", header)
	}
	start := i + len(header)
	depth := 1
	for j := start; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start:j], nil
			}
		}
	}
	return "", fmt.Errorf("unterminated %q block", header)
}

// tokenRow is one custom property as the page shows it. Preview is the
// inline style that makes the token visible — a chip painted with it, a
// bar as wide as it, a line of text set at it.
//
// It is template.CSS because it has to survive html/template's style
// attribute sanitiser, which would otherwise reject var(--rst-…) as an
// unrecognised value and write ZgotmplZ into the page. That is safe
// here and only here: the declaration is built from a token name this
// package just matched against --rst-[a-z0-9-]+ in the framework's own
// stylesheet. Nothing a caller supplies reaches it.
type tokenRow struct {
	Name    string
	Value   string
	Colour  bool // the value is a colour on its own
	Shadow  bool // the value is a shadow: preview it on a card
	Preview template.CSS
}

// parseTokens extracts every --rst-* declaration in body, sorted by
// name. Last declaration of a name wins, matching CSS's own rule.
func parseTokens(body string) []tokenRow {
	seen := map[string]string{}
	for _, m := range declPattern.FindAllStringSubmatch(body, -1) {
		seen[m[1]] = strings.TrimSpace(m[2])
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]tokenRow, 0, len(names))
	for _, name := range names {
		v := seen[name]
		row := tokenRow{
			Name:   name,
			Value:  v,
			Colour: colourValue.MatchString(v),
			Shadow: strings.HasPrefix(name, "--rst-shadow"),
		}
		// Only a value that is a colour gets painted into a chip, and
		// only a shadow gets worn by one. Anything else — a future
		// token in the light block that is neither — shows its name and
		// value with no preview, rather than a chip painted with
		// something that is not a colour.
		switch {
		case row.Shadow:
			row.Preview = preview("box-shadow", name)
		case row.Colour:
			row.Preview = preview("background", name)
		}
		rows = append(rows, row)
	}
	return rows
}

// preview builds one CSS declaration naming a token. See tokenRow's
// comment for why the result is marked safe.
func preview(property, token string) template.CSS {
	return template.CSS(property + ": var(" + token + ")")
}

// tokenGroup is a run of related custom properties under one heading.
// Kind picks the shape of the preview the page draws beside each row:
// "colour" (the default), "type", "space", "radius" or "font".
//
// ID is the group's anchor on the page, and the reason newGroup exists:
// it is derived from the ENGLISH title, before localiseGroups touches
// it, so /ja/index.html and /en/index.html carry the same sixty
// fragments and a link to one is a link to the same thing on all of
// them.
type tokenGroup struct {
	Title string
	Kind  string
	ID    string
	Rows  []tokenRow
}

// newGroup starts a group with its anchor already on it. Every
// tokenGroup on the page comes through here; a literal built anywhere
// else would render a heading the sidebar cannot link to, and
// TestTheSidebarLinksEverythingOnThePageExactlyOnce says so.
func newGroup(title, kind string) tokenGroup {
	return tokenGroup{Title: title, Kind: kind, ID: anchorID("tokens", title)}
}

// colourGroups splits a theme's light-block tokens into the five groups
// the palette is actually authored in, with anything unrecognised
// landing in "Other" rather than disappearing — a new token shows up on
// the page the day it is added, in the wrong group at worst.
func colourGroups(rows []tokenRow) []tokenGroup {
	groups := []struct {
		Title    string
		Prefixes []string
	}{
		{"Surfaces and lines", []string{"--rst-bg", "--rst-surface", "--rst-line"}},
		{"Text", []string{"--rst-text"}},
		{"Accent", []string{"--rst-accent", "--rst-on-accent"}},
		{"Status tones", []string{"--rst-tone-"}},
		{"Depth", []string{"--rst-shadow", "--rst-overlay"}},
	}
	out := make([]tokenGroup, 0, len(groups)+1)
	claimed := map[string]bool{}
	for _, g := range groups {
		group := newGroup(g.Title, "colour")
		for _, row := range rows {
			for _, prefix := range g.Prefixes {
				if strings.HasPrefix(row.Name, prefix) && !claimed[row.Name] {
					claimed[row.Name] = true
					group.Rows = append(group.Rows, row)
					break
				}
			}
		}
		if len(group.Rows) > 0 {
			out = append(out, group)
		}
	}
	other := newGroup("Other", "colour")
	for _, row := range rows {
		if !claimed[row.Name] {
			other.Rows = append(other.Rows, row)
		}
	}
	if len(other.Rows) > 0 {
		out = append(out, other)
	}
	return out
}

// structuralGroups pulls the type scale, the spacing steps and the radii
// out of tokens.css. They are structure, not colour, so they are the
// same on every page — but they belong beside the palette, because a
// reader looking up "how big is small" does not care which file the
// answer lives in.
func structuralGroups() ([]tokenGroup, error) {
	body, err := blockBody(string(ui.TokensCSS()), ":root {")
	if err != nil {
		return nil, fmt.Errorf("tokens.css: %w", err)
	}
	rows := parseTokens(body)
	pick := func(prefix, property string) []tokenRow {
		var out []tokenRow
		for _, r := range rows {
			if strings.HasPrefix(r.Name, prefix) {
				r.Preview = preview(property, r.Name)
				out = append(out, r)
			}
		}
		return out
	}
	// No Radius group: the radii moved into the themes in v2, so they are
	// part of the palette below, not of the structure every page shares.
	scale := newGroup("Type scale", "type")
	scale.Rows = pick("--rst-fs-", "font-size")
	space := newGroup("Spacing", "space")
	space.Rows = pick("--rst-sp-", "inline-size")
	return []tokenGroup{scale, space}, nil
}

// lightHalf resolves a v2 theme value for the light scheme: the first
// argument of a whole-value light-dark(<light>, <dark>) call, or the
// value unchanged when it is not one. The split is paren-aware, because
// both halves are usually rgba() calls carrying commas of their own.
//
// A value that merely contains a light-dark() somewhere inside it — a
// shadow, "0 8px 24px light-dark(a, b)" — is left whole on purpose: the
// page shows shadows as a shadow worn by a card, not as a colour chip,
// so the whole declaration is what a reader needs to see.
func lightHalf(v string) string {
	const prefix = "light-dark("
	if !strings.HasPrefix(v, prefix) || !strings.HasSuffix(v, ")") {
		return v
	}
	inner := v[len(prefix) : len(v)-1]
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return v
			}
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[:i])
			}
		}
	}
	return v
}

// themePalette reads one theme's single :root block: the colours
// resolved to their light halves, the shape tokens as their own group,
// and the type family. Dark values are not on the page: they live in the
// same declaration, and the page says so rather than showing a second
// set of chips nobody can compare side by side anyway.
//
// The type family comes back as a one-row group rather than as a
// tokenRow of its own. It used to be the latter, and the page had a
// hand-written heading and list for it — which meant the sidebar had a
// heading to link and no group to derive it from. A group with one row
// renders the same bytes (a font stack has no preview to draw) and is
// one fewer special case in two files.
func themePalette(theme string) ([]tokenGroup, error) {
	raw, ok := ui.ThemeCSS(theme)
	if !ok {
		return nil, fmt.Errorf("no theme %q", theme)
	}
	css := string(raw)
	// ":root {" cannot match the two toggle rules at the foot of the
	// file, whose selectors are ":root[data-theme=…] {".
	body, err := blockBody(css, ":root {")
	if err != nil {
		return nil, fmt.Errorf("themes/%s.css: %w", theme, err)
	}

	var palette []tokenRow
	var radii []tokenRow
	var font tokenRow
	for _, row := range parseTokens(body) {
		switch {
		case row.Name == "--rst-font":
			font = row
		case strings.HasPrefix(row.Name, "--rst-radius"):
			row.Preview = preview("border-radius", row.Name)
			radii = append(radii, row)
		default:
			palette = append(palette, resolveLight(row))
		}
	}
	groups := colourGroups(palette)
	if len(radii) > 0 {
		radius := newGroup("Radius", "radius")
		radius.Rows = radii
		groups = append(groups, radius)
	}
	if font.Name != "" {
		family := newGroup("Type family", "font")
		family.Rows = []tokenRow{font}
		groups = append(groups, family)
	}
	return groups, nil
}

// resolveLight rewrites one row to the light scheme: the displayed value
// loses its light-dark() wrapper, and a value that only became a colour
// once resolved (every palette token, since v2) gets its chip back.
func resolveLight(row tokenRow) tokenRow {
	row.Value = lightHalf(row.Value)
	row.Colour = colourValue.MatchString(row.Value)
	switch {
	case row.Shadow:
		row.Preview = preview("box-shadow", row.Name)
	case row.Colour:
		row.Preview = preview("background", row.Name)
	default:
		row.Preview = ""
	}
	return row
}
