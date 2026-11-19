package designsystem

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// ── Redirects from an old mount ──────────────────────────────────────
//
// rastrillo.org published this tree at /design-system until it moved
// to /components, and every address it published there has been
// bookmarked, linked and indexed. Redirects writes the _redirects
// rules, in the format CARLOS's static edge reads, that answer each of
// those addresses with one 301 to where it went. One hop, because each
// hop is a round trip, and a chain of them is a search engine deciding
// the page is gone.
//
// Three facts about the edge shape the rules. The first rule to match
// wins, so the specific rules come before the catch-all. A file at the
// requested path wins over every rule, which is why the two mounts may
// not overlap. And the edge sends a request for .../index.html to its
// directory, so a rule bound for an Overview names the directory: one
// naming the file would be the first of two hops.

// leftForFiesta is each page under a theme and locale that is now
// Fiesta's, with the page of Fiesta's design system that says what it
// said. The primitives page took its preview frames with it, and a
// reader who opened one of those on its own is sent to the page it
// framed.
var leftForFiesta = []struct{ source, page string }{
	{"tokens.html", FiestaTokensPage},
	{"primitives.html", FiestaPrimitivesPage},
	{"primitives/*", FiestaPrimitivesPage},
}

// Redirects is the _redirects rules, one a line, that send every
// address a tree mounted at from published to the same file of the
// tree mounted at to, or to Fiesta for a page that left. It refuses
// mounts that overlap, since a rule could then match a file of the new
// tree, and mounts CleanMount refuses.
func Redirects(from, to string) ([]string, error) {
	from, err := CleanMount(from)
	if err != nil {
		return nil, err
	}
	to, err = CleanMount(to)
	if err != nil {
		return nil, err
	}
	if from == to || strings.HasPrefix(from, to+"/") ||
		strings.HasPrefix(to, from+"/") {
		return nil, fmt.Errorf("designsystem: redirecting %s to %s: "+
			"one mount is inside the other", from, to)
	}

	rules := &ruleWriter{from: from, to: to}
	// A pt/ stub's page sends the reader on to pt-PT/ itself, so a rule
	// that sent its address to the stub would be the first of two hops.
	retagged := rastrillo.Retagged()
	for _, oldLocale := range slices.Sorted(maps.Keys(retagged)) {
		for _, theme := range ui.ThemeNames() {
			rules.retagged(theme, oldLocale, retagged[oldLocale])
		}
	}
	rules.redirect(from+"/index.html", to+"/")
	rules.redirect(from, to+"/")
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			rules.overview(theme, locale, locale)
		}
	}
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			rules.toFiesta(theme, locale, locale)
		}
	}
	rules.redirect(from+"/*", to+"/:splat")
	return rules.lines, nil
}

// ruleWriter collects the rules from one mount to another, in order.
type ruleWriter struct {
	from, to string
	lines    []string
}

func (rules *ruleWriter) redirect(source, destination string) {
	rules.lines = append(rules.lines, source+" "+destination+" 301")
}

// overview sends an Overview, asked for as its directory or its
// index.html, to its directory under the new mount.
func (rules *ruleWriter) overview(theme, oldLocale, locale string) {
	old := rules.from + "/" + theme + "/" + oldLocale
	directory := rules.to + "/" + theme + "/" + locale + "/"
	rules.redirect(old+"/index.html", directory)
	rules.redirect(old, directory)
}

// toFiesta sends each page that left, and its preview frames, to the
// Fiesta page that says what it said.
func (rules *ruleWriter) toFiesta(theme, oldLocale, locale string) {
	old := rules.from + "/" + theme + "/" + oldLocale + "/"
	for _, left := range leftForFiesta {
		page := FiestaPage(theme, locale, left.page)
		rules.redirect(old+left.source, page)
	}
}

// retagged sends everything under a locale's old directory to its
// final address under the new code's.
func (rules *ruleWriter) retagged(theme, oldLocale, locale string) {
	rules.overview(theme, oldLocale, locale)
	rules.toFiesta(theme, oldLocale, locale)
	rules.redirect(rules.from+"/"+theme+"/"+oldLocale+"/*",
		rules.to+"/"+theme+"/"+locale+"/:splat")
}

// refuseRuleSyntax refuses a mount holding a character CARLOS's edge
// reads as syntax in a _redirects rule, where a mount starts a source
// and a destination: a space ends a field, "#" starts a comment, "?" is
// a query no source matches, "*" a splat, ":" a placeholder, "%" an
// escape the edge decodes in a source and refuses when malformed, and
// a backslash after the first slash names another host.
func refuseRuleSyntax(mount string) error {
	for _, character := range mount {
		if unicode.IsSpace(character) || unicode.IsControl(character) ||
			strings.ContainsRune(`#?*:%\`, character) {
			return fmt.Errorf("designsystem: mount path %q holds %q, "+
				"which a _redirects rule cannot carry", mount, character)
		}
	}
	return nil
}
