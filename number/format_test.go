package number_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/de"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/es"
	_ "amadan.net/rastrillo/rastrillo/locales/fr"
	_ "amadan.net/rastrillo/rastrillo/locales/hi"
	_ "amadan.net/rastrillo/rastrillo/locales/sv"
	"amadan.net/rastrillo/rastrillo/number"
)

// Each case is CLDR 48 as the packs carry it, which is what ICU 78
// writes too; the x/text figures rastrillo wrote before are noted
// where they differ.
func TestFormatWritesThePacksConventions(t *testing.T) {
	plain := number.Options{Plain: true}
	for _, each := range []struct {
		locale  string
		value   number.Decimal
		options number.Options
		want    string
	}{
		{"en-IE", "1234", number.Options{}, "1,234"},
		{"de", "1234", number.Options{}, "1.234"},
		{"es", "1234", number.Options{}, "1234"}, // x/text: 1.234
		{"es", "12345", number.Options{}, "12.345"},
		{"fr", "1234", number.Options{}, "1\u202f234"}, // x/text: U+00A0
		{"de-AT", "1234", number.Options{}, "1\u00a0234"},
		{"de-CH", "1234567.5", number.Options{}, "1'234'567.5"},
		{"hi", "1234567", number.Options{}, "12,34,567"},
		{"sv", "-1234.5", number.Options{}, "\u22121\u00a0234,5"},
		{"ar", "-1234.5", number.Options{}, "\u200e-1,234.5"},
		{"de", "-0.00", number.Options{}, "0,00"},
		{"de", "1234567.5", plain, "1234567,5"},
		{"en", "1200.5", number.Options{Places: 2}, "1,200.50"},
		{"en", "1200.125", number.Options{Places: 2}, "1,200.125"},
		{"de", "50", number.Options{Percent: true}, "50\u00a0%"},
		{"en", "12.5", number.Options{Percent: true}, "12.5%"},
		{"fr-CH", "1234", number.Options{Percent: true}, "1\u202f234%"},
		{"en", "123456789012345678901234567890.123456789", plain,
			"123456789012345678901234567890.123456789"},
	} {
		got, err := number.Format(each.locale, each.value, each.options)
		if err != nil || got != each.want {
			t.Errorf("Format(%s, %s, %+v) = %q, %v; want %q", each.locale,
				each.value, each.options, got, err, each.want)
		}
	}
}

func TestFormatRefusesWhatIsNotADecimal(t *testing.T) {
	for _, bad := range []number.Decimal{"", "1,5", "01", "+1", "1.",
		".5", "1e3", "NaN", "--1", " 1", "١"} {
		got, err := number.Format("en", bad, number.Options{})
		if err == nil || !strings.Contains(err.Error(), "-1200.5") {
			t.Errorf("Format(%q) = %q, %v; want a refusal naming the form",
				bad, got, err)
		}
	}
}

// A seed with three fraction digits and at most three whole ones reads
// two ways when read fresh, which is why form.Number keeps an unchanged
// seed unread; the strict reading takes it back exactly.
func TestASeedLikeGerman1234IsAmbiguousReadFresh(t *testing.T) {
	seed, _ := number.InputValue("de", "1.234")
	if seed != "1,234" {
		t.Fatalf("seed %q", seed)
	}
	if got, err := number.Parse("de", seed); got != "1.234" || err != nil {
		t.Errorf("Parse(de, %q) = %q, %v", seed, got, err)
	}
	var ambiguous *number.AmbiguousError
	if _, err := number.ParseNormalized("de", seed); !errors.As(err,
		&ambiguous) {
		t.Errorf("ParseNormalized(de, %q): %v, want ambiguous", seed, err)
	}
}

// A seed is read back by the parser, so it carries no group mark: in
// another reading a group mark makes the same digits a thousand times
// more.
func TestInputValueHasNoGroupMarks(t *testing.T) {
	for _, each := range []struct {
		locale string
		value  number.Decimal
		want   string
	}{
		{"de", "1234567.5", "1234567,5"},
		{"en", "-1200.5", "-1200.5"},
		{"fr", "0.25", "0,25"},
	} {
		got, err := number.InputValue(each.locale, each.value)
		if err != nil || got != each.want {
			t.Errorf("InputValue(%s, %s) = %q, %v; want %q", each.locale,
				each.value, got, err, each.want)
		}
	}
}

// NaN and the infinities are written in the locale's own symbols and
// pattern, as ICU 78 writes them (Intl.NumberFormat), not in English.
func TestFormatFloatWritesNonFiniteValuesInTheLocale(t *testing.T) {
	percent := number.Options{Percent: true}
	for _, each := range []struct {
		locale  string
		value   float64
		options number.Options
		want    string
	}{
		{"sv", math.Inf(-1), number.Options{}, "\u2212∞"},
		{"fi", math.NaN(), number.Options{}, "epäluku"},
		{"ar", math.NaN(), number.Options{}, "ليس\u00a0رقمًا"},
		{"en", math.Inf(1), number.Options{}, "∞"},
		{"de", math.NaN(), percent, "NaN\u00a0%"},
		{"sv", math.Inf(-1), percent, "\u2212∞\u00a0%"},
		{"de", 1234.5, number.Options{}, "1.234,5"},
	} {
		got, err := number.FormatFloat(each.locale, each.value, 64,
			each.options)
		if err != nil || got != each.want {
			t.Errorf("FormatFloat(%s, %v, %+v) = %q, %v; want %q",
				each.locale, each.value, each.options, got, err, each.want)
		}
	}
}
