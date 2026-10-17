package crosspack

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/fr"
	_ "amadan.net/rastrillo/rastrillo/locales/no"
)

func TestCrossPackFallback(t *testing.T) {
	for _, c := range []struct{ tag, id, missing string }{
		{"hi-Latn", "en-IN", "hi"},
		{"ht", "fr-HT", "ht"},
		{"nb", "no", "nb"},
		{"nn", "no", "nn"},
	} {
		r := locales.Resolve(c.tag)
		if r.ID != c.id || r.Wanted != c.tag || r.MissingPack != c.missing || r.UsedDefault {
			t.Errorf("Resolve(%q) = %+v, want ID %q MissingPack %q", c.tag, r, c.id, c.missing)
		}
		// The fallback formats exactly as the parent does. Both errors
		// are checked: two failures would otherwise agree on "".
		got, err := currency.Format(c.tag, -128450, "EUR")
		if err != nil {
			t.Errorf("Format(%q) = %v", c.tag, err)
			continue
		}
		want, err := currency.Format(c.id, -128450, "EUR")
		if err != nil {
			t.Errorf("Format(%q) = %v", c.id, err)
			continue
		}
		if got != want {
			t.Errorf("Format(%q) = %q, want %s's %q", c.tag, got, c.id, want)
		}
	}
	// The data parents stay inside packs: hi-Latn's record, had the hi
	// pack been linked, is relative to root, not en-IN.
	pack, _ := locales.PackOf("hi-Latn")
	if locales.Parent("hi-Latn") != "en-IN" || pack != "hi" {
		t.Errorf("fallback parent %q, pack %q", locales.Parent("hi-Latn"), pack)
	}
}
