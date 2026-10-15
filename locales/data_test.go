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
