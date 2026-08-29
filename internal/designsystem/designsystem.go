// Package designsystem renders rastrillo.org/design-system: one static
// page per theme × locale showing every partial, every class idiom and
// every design token the framework ships, plus a full-page demo of each
// of the three shells.
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
// Determinism is a contract, not a nicety: 144 pages regenerated on
// every partial change are only reviewable if the diff is the change.
// Every map in here — Styleguide's samples, BaseCatalogs, the parsed
// token blocks — is sorted before anything reaches output.
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

// Render builds the whole design-system tree in memory: path relative to
// docs/design-system → file content.
//
//	index.html                            ink, en, assets at the tree root
//	<theme>/<locale>/index.html           the same page, 36 times
//	<theme>/<locale>/shells/<shell>.html  108 full-page shell demos
//	tokens.css theme-<theme>.css          the stylesheets, once each
//	rastrillo.js select.js datetime.js    the three scripts, once each
//
// The assets are shared by every page rather than copied per theme, so
// the tree's size is 144 documents plus one copy of the library.
func Render() (map[string][]byte, error) {
	out := map[string][]byte{
		"tokens.css":   ui.TokensCSS(),
		"rastrillo.js": ui.ShimJS(),
		"select.js":    ui.SelectJS(),
		"datetime.js":  ui.DatetimeJS(),
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
			page, err := renderIndex(theme, locale, "../../", "")
			if err != nil {
				return nil, fmt.Errorf("designsystem: %s: %w", dir+"index.html", err)
			}
			out[dir+"index.html"] = page
			for _, shell := range ui.LayoutNames() {
				demo, err := renderShell(theme, locale, shell, "../../../")
				if err != nil {
					return nil, fmt.Errorf("designsystem: %s: %w", dir+"shells/"+shell+".html", err)
				}
				out[dir+"shells/"+shell+".html"] = demo
			}
		}
	}

	// The tree root is ink/en again, at a different depth: same page,
	// every relative path rewritten. Generated rather than copied, so
	// the two cannot drift — the only difference between this call and
	// the one above is the two prefixes.
	root, err := renderIndex("ink", "en", "", "ink/en/")
	if err != nil {
		return nil, fmt.Errorf("designsystem: index.html: %w", err)
	}
	out["index.html"] = root
	return out, nil
}

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
// runs 36 times at generate time, so the Clone discipline ui.FuncsWith
// documents buys nothing here.
func partialTree(locale string) (*template.Template, error) {
	return template.New("designsystem").
		Funcs(ui.Funcs(ui.WithT(translator(locale)))).
		ParseFS(ui.Templates(), "*.html")
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
// "colour" (the default), "type", "space" or "radius".
type tokenGroup struct {
	Title string
	Kind  string
	Rows  []tokenRow
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
		group := tokenGroup{Title: g.Title, Kind: "colour"}
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
	other := tokenGroup{Title: "Other", Kind: "colour"}
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
	return []tokenGroup{
		{Title: "Type scale", Kind: "type", Rows: pick("--rst-fs-", "font-size")},
		{Title: "Spacing", Kind: "space", Rows: pick("--rst-sp-", "inline-size")},
		{Title: "Radius", Kind: "radius", Rows: pick("--rst-radius", "border-radius")},
	}, nil
}

// themePalette reads one theme's light block plus the type family it
// declares above it. Dark values are not on the page: they live in the
// same file, and the page says so rather than showing a second set of
// chips nobody can compare side by side anyway.
func themePalette(theme string) (colours []tokenGroup, font tokenRow, err error) {
	raw, ok := ui.ThemeCSS(theme)
	if !ok {
		return nil, tokenRow{}, fmt.Errorf("no theme %q", theme)
	}
	css := string(raw)
	light, err := blockBody(css, `:root[data-theme="light"] {`)
	if err != nil {
		return nil, tokenRow{}, fmt.Errorf("themes/%s.css: %w", theme, err)
	}
	family, err := blockBody(css, ":root {")
	if err != nil {
		return nil, tokenRow{}, fmt.Errorf("themes/%s.css: %w", theme, err)
	}
	for _, row := range parseTokens(family) {
		if row.Name == "--rst-font" {
			font = row
		}
	}
	return colourGroups(parseTokens(light)), font, nil
}
