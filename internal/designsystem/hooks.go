package designsystem

import "amadan.net/rastrillo/rastrillo/ui"

// The few things the browser sweeps in internal/designsystem/sweep read
// from the renderer. The sweeps are a package of their own to keep this
// package's browser run inside its share of the CI time bound, and a
// test in another package can only reach what is exported. Each hook is
// the unexported function the gallery itself uses, so a sweep cannot
// build a URL or name a page differently from the gallery.

// PageKinds is every page kind, in the order the rail lists them.
func PageKinds() []string {
	out := make([]string, 0, len(pageKinds()))
	for _, pk := range pageKinds() {
		out = append(out, pk.Kind)
	}
	return out
}

// PageFile is one page kind's file name.
func PageFile(kind string) string { return fileOf(kind) }

// PageHref is one page's absolute address under mount.
func PageHref(mount, theme, locale, file string) string { return pageHref(mount, theme, locale, file) }

// ExampleCounts is, per page kind that frames examples, the least number
// of its sections with an example to measure: every partial of a
// family, every Styleguide sample, every shell, screen and format, and
// the Overview's demo application. The height drives read it, so a
// component documented with nothing to look at fails there rather than
// only on a reader's screen.
func ExampleCounts() map[string]int {
	out := map[string]int{
		"overview":   1,
		"primitives": len(ui.Styleguide()),
		"shells":     len(ui.LayoutNames()),
		"screens":    len(screenDocs()),
		"formats":    len(formatDocs()),
	}
	for _, fam := range families() {
		out[fam.Key] = len(fam.Partials)
	}
	return out
}
