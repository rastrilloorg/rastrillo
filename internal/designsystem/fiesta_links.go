package designsystem

import (
	"slices"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// ── Fiesta's pages ───────────────────────────────────────────────────
//
// Fiesta owns the look, and its design system documents it. What this
// gallery used to say about the look (every token, the markup with no
// Go call behind it, and each stylesheet and script) is now a link to
// the page of Fiesta's design system that says it, in the reader's
// theme and language. testdata/fiesta-pages.txt pins those pages for
// the Fiesta version in go.mod, and TestFiestaLinksNameAPublishedPage
// holds every Fiesta link the render writes to that list.

// fiestaDesignSystem is where Fiesta publishes its design system.
const fiestaDesignSystem = "https://fiesta.rastrillo.org/design-system/"

// The Fiesta pages that replace what left this gallery, read off
// Fiesta v1.3.0's gallery: its Tokens page, its "primitives" family's
// page, and its Getting started page, whose rows describe and weigh
// each stylesheet and script.
const (
	FiestaTokensPage     = "tokens.html"
	FiestaPrimitivesPage = "primitives.html"
	FiestaFilesPage      = "getting-started.html"
)

// fiestaPages is every Fiesta page this gallery links.
var fiestaPages = []string{
	FiestaFilesPage, FiestaTokensPage, FiestaPrimitivesPage,
}

// FiestaPage is one page of Fiesta's design system in one theme and
// locale. It panics on a theme, locale or page outside the manifest,
// because every caller passes values read from this package's own
// tables: a miss is a programming error, not a condition to handle.
// The manifest is those same tables, which
// TestFiestaLinksNameAPublishedPage holds it to.
func FiestaPage(theme, locale, page string) string {
	known := slices.Contains(ui.ThemeNames(), theme) &&
		slices.Contains(rastrillo.BaseLocales(), locale) &&
		slices.Contains(fiestaPages, page)
	if !known {
		panic("designsystem: no Fiesta page " +
			theme + "/" + locale + "/" + page)
	}
	return fiestaDesignSystem + theme + "/" + locale + "/" + page
}

// offsite is an entry in the rail and the Overview's routes that leaves
// for a page of Fiesta's: its English title and blurb, both prose keys,
// and the Fiesta page it opens.
type offsite struct{ Title, Blurb, Page string }

// fiestaEntries are the pages that left, each keyed by the page kind it
// now stands before, so the rail and the Overview keep the order they
// had: Tokens after Getting started, UI primitives after the families.
var fiestaEntries = map[string]offsite{
	"icons": {
		Title: "Tokens on Fiesta",
		Blurb: "Every custom property in Fiesta's design system: the " +
			"theme's colour and type, and the scales for size, " +
			"spacing and radius.",
		Page: FiestaTokensPage,
	},
	"formats": {
		Title: "UI primitives on Fiesta",
		Blurb: "Fiesta's UI primitives, the markup you write by hand " +
			"in a template: buttons, cards, data grids, the modal and " +
			"the shells' own chrome.",
		Page: FiestaPrimitivesPage,
	},
}

// fiestaSection is one Fiesta page as a section of the rail: a plain
// link, since there is nothing of it on this side to list.
func fiestaSection(theme, locale string, away offsite) navSection {
	return navSection{
		Title:    proseIn(locale, away.Title),
		Href:     FiestaPage(theme, locale, away.Page),
		External: true,
	}
}

// fiestaRoute is one Fiesta page as a route off the Overview.
func fiestaRoute(theme, locale string, away offsite) routeView {
	return routeView{
		Label:    proseIn(locale, away.Title),
		Blurb:    proseIn(locale, away.Blurb),
		Href:     FiestaPage(theme, locale, away.Page),
		External: true,
	}
}
