package locales_test

import (
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// Every shipped locale must rebuild from its data parent inside its
// own pack: a pack that needed another to rebuild would format
// wrongly, not fail, when only it is linked (spec §3, "Two parent maps").
func TestEveryLinkedLocaleRebuilds(t *testing.T) {
	ids := locales.LinkedIDs()
	if len(ids) != 429 {
		t.Fatalf("modern links %d locales, want 429", len(ids))
	}
	for _, id := range ids {
		l, ok := locales.Data(id)
		if !ok {
			t.Errorf("%s: no data", id)
			continue
		}
		for name, v := range map[string]string{"CurrencyDecimal": l.CurrencyDecimal, "CurrencyGroup": l.CurrencyGroup,
			"Decimal": l.Decimal, "Minus": l.Minus, "Digits": l.Digits, "MinGrouping": l.MinGrouping,
			"Standard": l.Standard, "Alpha": l.Alpha, "Spacing": l.Spacing} {
			if v == "" {
				t.Errorf("%s: %s is empty after merging", id, name)
			}
		}
	}
	if got := len(locales.LinkedPacks()); got != 101 {
		t.Errorf("modern links %d packs, want 101", got)
	}
	if p, ok := locales.PackOf("de-CH"); !ok || p != "de" {
		t.Errorf("PackOf(de-CH) = %q, %v", p, ok)
	}
	if got := locales.Parent("hi-Latn"); got != "en-IN" {
		t.Errorf("Parent(hi-Latn) = %q, want en-IN (a fallback parent that crosses packs)", got)
	}
}

// The data parent (Locale.Parent) must be in the locale's own pack, and
// only the fallback parent (Parent) may cross packs. The test above
// cannot see a data parent in another pack: with modern linked every
// parent is present, so Data rebuilds whatever the pack boundary is.
// This asserts the boundary itself, through PackOf, so a generator that
// wrote hi-Latn's record relative to en-IN fails here instead of
// formatting wrongly in an app that links hi without en.
func TestDataParentsStayInsideTheirPack(t *testing.T) {
	for _, id := range locales.LinkedIDs() {
		pack, ok := locales.PackOf(id)
		if !ok {
			t.Errorf("%s: linked but not in the pack index", id)
			continue
		}
		l, _ := locales.Data(id)
		if l.Parent != "root" {
			if parentPack, ok := locales.PackOf(l.Parent); !ok || parentPack != pack {
				t.Errorf("%s (pack %s): data parent %s is in pack %q, want the same pack or root", id, pack, l.Parent, parentPack)
			}
		}
		if fb := locales.Parent(id); fb != "root" {
			if _, ok := locales.PackOf(fb); !ok {
				t.Errorf("%s: fallback parent %s is not a shipped locale", id, fb)
			}
		}
	}
	// The one cross-pack fallback the resolver's tests rely on, pinned
	// beside the rule it is the exception to.
	if p, ok := locales.PackOf("hi-Latn"); !ok || p != "hi" {
		t.Errorf("PackOf(hi-Latn) = %q, %v, want hi", p, ok)
	}
	if got := locales.Parent("hi-Latn"); got != "en-IN" {
		t.Errorf("Parent(hi-Latn) = %q, want en-IN", got)
	}
}

// Data's maps are the caller's own. Root's are the maps every locale
// inherits through Merge, so handing them out would let any package
// an app imports rewrite, say, the USD symbol for every locale at init.
func TestDataReturnsCopies(t *testing.T) {
	ids := append([]string{"root"}, locales.LinkedIDs()...)
	before := map[string]locales.Locale{}
	for _, id := range ids {
		before[id], _ = locales.Data(id)
	}
	for _, id := range []string{"root", "en", "de-CH"} {
		l, _ := locales.Data(id)
		// A nil map cannot be written, so it cannot leak; skip it.
		for _, m := range []map[string]string{l.Symbols, l.Narrow} {
			for k := range m {
				m[k] = "X"
			}
			if m != nil {
				m["ZZZ"] = "X"
			}
		}
		for k := range l.Overrides {
			l.Overrides[k] = locales.Override{Pattern: "X"}
		}
		if l.Overrides != nil {
			l.Overrides["ZZZ"] = locales.Override{Pattern: "X"}
		}
	}
	// before["root"] would share root's maps if Data leaked them, so
	// root gets a check against a known value as well.
	if r, _ := locales.Data("root"); r.Symbols["USD"] != "US$" || r.Narrow["USD"] != "$" {
		t.Errorf("root's USD symbols are now %q and %q, want US$ and $", r.Symbols["USD"], r.Narrow["USD"])
	}
	for _, id := range ids {
		if after, _ := locales.Data(id); !reflect.DeepEqual(after, before[id]) {
			t.Errorf("%s: changing a Locale that Data returned changed what Data returns next", id)
		}
	}
}
