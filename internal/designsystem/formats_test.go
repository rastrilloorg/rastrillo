package designsystem

import (
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// The Money sample writes three currencies the way each page's
// language does; the currency comes from the data, so the euro stays a
// euro on the Japanese page.
func TestMoneySampleIsFormattedPerLocale(t *testing.T) {
	for locale, wants := range map[string][]string{
		"en":    {"€1,284.50", "¥1,285", "KWD 1,284.500"}, // English writes the yen with U+00A5
		"ja":    {"￥1,285"},                               // Japanese with the fullwidth U+FFE5 (Appendix A.1)
		"es":    {"1284,50 €"},
		"pt-PT": {"1284,50\u00a0€"}, // Appendix A.1: European Portuguese, not CLDR pt's Brazilian € 1.284,50
		// currency.Format("ar", 128450, "EUR") has no trailing RLM
		// after the symbol; Appendix A.1 only tables ar SAR, not ar
		// EUR, so this is the formatter's actual shape, not the
		// table's.
		"ar": {"‏1,284.50 €"},
	} {
		// moneySampleMarkup formats an unknown code as itself, so a
		// locale the gallery has no page for would pass here and prove
		// nothing about what a reader sees.
		if !slices.Contains(rastrillo.BaseLocales(), locale) {
			t.Errorf("%s: the gallery has no page in this locale", locale)
		}
		got := moneySampleMarkup(locale)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", locale, got, w)
			}
		}
	}
}
