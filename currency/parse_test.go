package currency_test

import (
	"errors"
	"math"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

func TestParseExpected(t *testing.T) {
	cases := []struct {
		tag, in, code string
		want          int64
		err           error
	}{
		{"en", "1,284.50", "USD", 128450, nil},
		{"en", "$1,284.50", "USD", 128450, nil},
		{"en", "usd 12", "USD", 1200, nil},
		{"en", "\u200f1,284.50\u00a0", "USD", 128450, nil},
		{"en", "1.284", "USD", 0, currency.ErrPrecision},
		{"en", "12,50", "USD", 0, currency.ErrDecimalPoint},
		{"en", "$12", "EUR", 0, currency.ErrCurrencySign},
		{"en", "+12", "USD", 0, currency.ErrInvalid},
		{"de", "1.284", "EUR", 128400, nil},
		{"de", "1.284,50 €", "EUR", 128450, nil},
		{"de", "1284.50", "EUR", 0, currency.ErrDecimalComma},
		{"fr", "1\u202f284,50", "EUR", 128450, nil},
		{"fr", "1 284,50", "EUR", 128450, nil},
		{"fr", "1284.50", "EUR", 128450, nil},
		{"fr-CH", "1234.50", "CHF", 123450, nil},
		{"fr-CH", "1234,50", "CHF", 123450, nil},
		{"hi", "1,23,456.00", "INR", 12345600, nil},
		{"hi", "12,34,5", "INR", 0, currency.ErrDecimalPoint},
		{"ar", "١٢٣", "SAR", 12300, nil},
		{"bn", "১২৮৪.৫০", "BDT", 128450, nil},
		{"pt-CV", "1284$50", "CVE", 128450, nil},
		{"ja", "12.5", "JPY", 0, currency.ErrPrecision},
		// Finding 10: the parser reads signs; field policy decides.
		{"en", "-12", "USD", -1200, nil},
		{"en", "-$12", "USD", -1200, nil},
		{"en", "AED 1,284.50-", "AED", -128450, nil}, // trailing sign, as fy (full set) writes it
		// Finding 7: boundaries and overflow.
		{"en", "92233720368547758.07", "USD", math.MaxInt64, nil},
		{"en", "-92233720368547758.08", "USD", math.MinInt64, nil},
		{"en", "92233720368547758.08", "USD", 0, currency.ErrInvalid},
		// Finding 2: the same text means 1000 times more under de. This
		// is why the parsing locale travels with the echoed value.
		{"en", "1.500", "KWD", 1500, nil},
		{"de", "1.500", "KWD", 1500000, nil},
		{"swc", "1.500", "KWD", 1500000, nil}, // round two, finding 3
		// Finding 12: visually empty is blank, and blank is zero here;
		// the required check runs on Clean.
		{"en", "\u200f \u00a0", "USD", 0, nil},
	}
	for _, c := range cases {
		got, err := currency.Parse(c.tag, c.in, c.code)
		if got != c.want || !errors.Is(err, c.err) || err == nil && c.err != nil {
			t.Errorf("Parse(%q, %q, %q) = %d, %v; want %d, %v", c.tag, c.in, c.code, got, err, c.want, c.err)
		}
	}
	if currency.Clean("\u200f \u00a0\u202f") != "" {
		t.Error("Clean left a visually empty value non-empty")
	}
}
