package currency_test

import (
	"math"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// Expected values are written out, not derived from the formatter, so a
// shared mistake in Format and Parse cannot hide behind a round trip.
// Every string here that ICU also produces was confirmed by cmd/check;
// the ones where ICU differs say so.

func TestFormatExpected(t *testing.T) {
	cases := []struct {
		tag   string
		minor int64
		code  string
		want  string
	}{
		// The gallery locales (spec Appendix A.1).
		{"en", 128450, "USD", "$1,284.50"},
		{"en", -128450, "USD", "-$1,284.50"},
		{"en", 0, "USD", "$0.00"},
		{"de", 128450, "EUR", "1.284,50\u00a0€"},
		{"es", 128450, "EUR", "1284,50\u00a0€"}, // minimumGroupingDigits 2
		{"es", 1234567850, "EUR", "12.345.678,50\u00a0€"},
		{"hi", 1234567850, "INR", "₹1,23,45,678.50"},
		{"bn", 1234567850, "BDT", "১,২৩,৪৫,৬৭৮.৫০৳"},
		{"ja", 1285, "JPY", "￥1,285"},
		{"zh-Hans", 128450, "CNY", "¥1,284.50"},
		{"yue", 128450, "HKD", "HK$1,284.50"},
		{"ar", 128450, "SAR", "\u200f1,284.50\u00a0ر.س.\u200f"},
		{"ar", -128450, "SAR", "\u200f\u200e-1,284.50\u00a0ر.س.\u200f"},
		{"en", 1284500, "KWD", "KWD\u00a01,284.500"},
		{"vi", 1285, "VND", "1.285\u00a0₫"},
		{"ga", 128450, "EUR", "€1,284.50"},
		{"pt", 128450, "EUR", "€\u00a01.284,50"},
		{"pt-PT", 128450, "EUR", "1284,50\u00a0€"},
		// Finding 8: currency-specific marks. fr-CH money uses "." where
		// its plain numbers use ","; ICU differs (it groups with U+0027).
		{"fr-CH", 123450, "EUR", "1\u202f234.50\u00a0€"},
		{"fr", 123450, "EUR", "1\u202f234,50\u00a0€"},
		// Per-currency overrides: en-150's EUR pattern, the escudo's "$".
		{"en-150", 123450, "EUR", "€1,234.50"},
		{"pt-CV", 128450, "CVE", "1284$50\u00a0\u200b"},
		// Finding 9: resolution before inheritance.
		{"zh-TW", 128450, "TWD", "$1,284.50"},
		{"zh", 128450, "TWD", "NT$1,284.50"},
		{"ZH-hant-tw", 128450, "TWD", "$1,284.50"},
		{"en-IE", 128450, "USD", "US$1,284.50"},
		{"es-MX", 128450, "MXN", "$1,284.50"},
		{"de-DE-u-nu-arab", 128450, "EUR", "1.284,50\u00a0€"}, // extensions ignored
		{"xx", 128450, "EUR", "€\u00a01,284.50"},              // unknown: CLDR root
		{"de--CH", 128450, "EUR", "€\u00a01,284.50"},          // malformed: CLDR root
		{"swc", 1500000, "KWD", "KWD\u00a01.500,000"},         // sw-CD marks, not sw's
		// Finding 7: both int64 boundaries at every exponent.
		{"en", math.MaxInt64, "USD", "$92,233,720,368,547,758.07"},
		{"en", math.MinInt64, "USD", "-$92,233,720,368,547,758.08"},
		{"ja", math.MinInt64, "JPY", "-￥9,223,372,036,854,775,808"},
		{"en", math.MaxInt64, "KWD", "KWD\u00a09,223,372,036,854,775.807"},
		{"en", math.MinInt64, "KWD", "-KWD\u00a09,223,372,036,854,775.808"},
		// Finding 5: a historical code keeps its scale and still formats.
		{"hr", 128450, "HRK", "1.284,50\u00a0kn"},
	}
	for _, c := range cases {
		got, err := currency.Format(c.tag, c.minor, c.code)
		if err != nil || got != c.want {
			t.Errorf("Format(%q, %d, %q) = %q, %v; want %q", c.tag, c.minor, c.code, got, err, c.want)
		}
	}
}

func TestInputValueExpected(t *testing.T) {
	for _, c := range []struct {
		tag   string
		minor int64
		code  string
		want  string
	}{
		{"de", 128450, "EUR", "1284,50"},
		{"en", -128450, "USD", "-1284.50"},
		{"ja", 1285, "JPY", "1285"},
		{"bn", 128450, "BDT", "১২৮৪.৫০"},
		{"fr-CH", 123450, "CHF", "1234.50"},
		{"en", math.MinInt64, "KWD", "-9223372036854775.808"},
	} {
		got, err := currency.InputValue(c.tag, c.minor, c.code)
		if err != nil || got != c.want {
			t.Errorf("InputValue(%q, %d, %q) = %q, %v; want %q", c.tag, c.minor, c.code, got, err, c.want)
		}
	}
}

// Format's output goes straight into templates. It must never contain
// markup characters, whatever the locale's symbols are, so the only
// escaping it needs is html/template's ordinary text escaping.
func TestFormatHasNoMarkup(t *testing.T) {
	for _, id := range locales.LinkedIDs() {
		for _, code := range []string{"USD", "EUR", "JPY", "KWD", "SAR", "INR"} {
			s, err := currency.Format(id, -1234567850, code)
			if err != nil || strings.ContainsAny(s, "<>&\"") {
				t.Errorf("Format(%s, %s) = %q, %v", id, code, s, err)
			}
		}
	}
}
