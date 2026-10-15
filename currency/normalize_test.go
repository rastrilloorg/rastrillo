package currency_test

import (
	"errors"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// ambiguous expects an AmbiguousError with these two choices.
type ambiguous struct{ a, b string }

// The spec's examples (§5.1), each expected value written by hand from
// the decision table, not derived from the parser. Locales en, de, fr,
// hi, ar and ja; currencies with 0 (JPY), 2 and 3 (KWD) decimals.
func TestNormalizeExpected(t *testing.T) {
	cases := []struct {
		tag, in, code string
		want          int64
		err           any // nil, a sentinel error, or ambiguous
	}{
		// en: the field's own currency in every CLDR spelling, either side.
		{"en", "US$ 5", "USD", 500, nil}, // root's symbol
		{"en", "$5", "USD", 500, nil},
		{"en", "5$", "USD", 500, nil},
		{"en", "5 usd", "USD", 500, nil},
		{"en", "USD5", "USD", 500, nil},
		{"en", "\u200e$5\u00a0", "USD", 500, nil},
		{"en", "JP¥1500", "JPY", 1500, nil},
		{"en", "¥1500", "JPY", 1500, nil},
		// en: separators with one reading.
		{"en", "1,284.50", "USD", 128450, nil},
		{"en", "1 284.50", "USD", 128450, nil},
		{"en", "1'284.50", "USD", 128450, nil},
		{"en", "1.284,50", "USD", 128450, nil},
		{"en", "1.234.567", "USD", 123456700, nil},
		{"en", "12,50", "USD", 1250, nil},        // as a group, "50" is too short
		{"en", "12.5000", "USD", 1250, nil},      // zeros past the exponent round nothing
		{"en", "1,23,456", "USD", 12345600, nil}, // Indian grouping, read anywhere
		{"en", ".50", "USD", 50, nil},
		{"en", "5.", "USD", 500, nil},
		{"en", "1,500.250", "KWD", 1500250, nil},
		{"en", "1.5", "KWD", 1500, nil},
		{"en", "1500", "KWD", 1500000, nil},
		// en: competing readings, an over-precise one included.
		{"en", "1,500", "USD", 150000, nil}, // en groups with ","
		{"en", "1.500", "USD", 0, ambiguous{"1.5 USD", "1500 USD"}},
		{"en", "1,000", "USD", 100000, nil},
		{"en", "1,234", "USD", 123400, nil},
		{"en", "1.234", "USD", 0, ambiguous{"1.2340 USD", "1234 USD"}}, // en decimal mark, and a group reading exists
		{"en", "5.999", "USD", 0, ambiguous{"5.9990 USD", "5999 USD"}},
		{"en", "1,500", "KWD", 1500000, nil}, // the accepted cost: German 1,500 meaning 1.5 reads 1500 here
		{"en", "1.500", "KWD", 0, ambiguous{"1.5 KWD", "1500 KWD"}},
		{"en", "1,234", "KWD", 1234000, nil},
		{"en", "1.234", "KWD", 0, ambiguous{"1.2340 KWD", "1234 KWD"}},
		{"en", "1,500", "JPY", 1500, nil},
		{"en", "1.500", "JPY", 0, ambiguous{"1.5 JPY", "1500 JPY"}},
		// en: only over-precise readings.
		{"en", "12.3456", "USD", 0, currency.ErrPrecision},
		{"en", "1.2345", "KWD", 0, currency.ErrPrecision},
		{"en", "1.5", "JPY", 0, currency.ErrPrecision},
		// en: malformed separators.
		{"en", "12,.50", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1,234,.50", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1,,234", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1,2,3", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1.2.3", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1,234.", "USD", 123400, nil}, // the comma reading would need a second decimal mark
		{"en", "12.50.", "USD", 0, currency.ErrDecimalPoint},
		{"en", ",50", "USD", 50, nil}, // a leading comma is a leading decimal in the comma reading
		{"en", ".", "USD", 0, currency.ErrDecimalPoint},
		{"en", "1 ,5", "USD", 0, currency.ErrDecimalPoint},
		// en: signs and parentheses.
		{"en", "(5.00)", "USD", -500, nil},
		{"en", "( $5 )", "USD", -500, nil},
		{"en", "($5)", "USD", -500, nil},
		{"en", "+5", "USD", 500, nil},
		{"en", "-$5", "USD", -500, nil},
		{"en", "$-5", "USD", -500, nil},
		{"en", "5-", "USD", -500, nil},
		{"en", "5 USD-", "USD", -500, nil},
		{"en", "\u22125", "USD", -500, nil},
		{"en", "--5", "USD", 0, currency.ErrInvalid},
		{"en", "+-5", "USD", 0, currency.ErrInvalid},
		{"en", "-5-", "USD", 0, currency.ErrInvalid},
		{"en", "-+5", "USD", 0, currency.ErrInvalid},
		{"en", "(-5)", "USD", 0, currency.ErrInvalid},
		{"en", "-(5)", "USD", 0, currency.ErrInvalid},
		{"en", "(5)-", "USD", 0, currency.ErrInvalid},
		{"en", "(5", "USD", 0, currency.ErrInvalid},
		{"en", "5)", "USD", 0, currency.ErrInvalid},
		// Review finding M1: an amount too large to store, and a sign with
		// no amount, are invalid, as the strict parser says, not a
		// decimal-mark problem the person cannot fix by changing marks.
		{"en", "()", "USD", 0, currency.ErrInvalid},
		{"en", "99999999999999999999", "USD", 0, currency.ErrInvalid},
		{"en", "-99999999999999999999", "USD", 0, currency.ErrInvalid},
		{"de", "99999999999999999999", "EUR", 0, currency.ErrInvalid},
		{"en", "92,233,720,368,547,758.08", "USD", 0, currency.ErrInvalid},
		// en: refused.
		{"en", "€5", "USD", 0, currency.ErrWrongCurrency},
		{"en", "5 EUR", "USD", 0, currency.ErrWrongCurrency},
		{"en", "5 eur", "USD", 0, currency.ErrWrongCurrency},
		{"en", "CA$5", "USD", 0, currency.ErrWrongCurrency},
		{"en", "A$5", "USD", 0, currency.ErrWrongCurrency},
		{"en", "5A$", "USD", 0, currency.ErrWrongCurrency},
		{"en", "5 dollars", "USD", 0, currency.ErrInvalid},
		{"en", "5abc", "USD", 0, currency.ErrInvalid},
		{"en", "$5 USD", "USD", 0, currency.ErrInvalid},
		{"en", "$5$", "USD", 0, currency.ErrInvalid},
		{"en", "5$5", "USD", 0, currency.ErrInvalid},
		{"en", "\u00a0\u202f", "USD", 0, nil}, // blank; Required runs on Clean

		// de: comma decimals, point groups.
		{"de", "12.50", "EUR", 1250, nil}, // as a group, "50" is too short
		{"de", "12,50 €", "EUR", 1250, nil},
		{"de", "EUR 5", "EUR", 500, nil},
		{"de", "1.284,50", "EUR", 128450, nil},
		{"de", "1,284.50", "EUR", 128450, nil},
		{"de", "1.234", "EUR", 123400, nil}, // de groups with "."
		{"de", "1,234", "EUR", 0, ambiguous{"1,2340 EUR", "1234 EUR"}},
		{"de", "1.500", "EUR", 150000, nil},
		{"de", "1.500", "KWD", 1500000, nil},
		{"de", "1,500", "KWD", 0, ambiguous{"1,5 KWD", "1500 KWD"}},
		{"de", "-1.250", "KWD", -1250000, nil},
		{"de", "-1,250", "KWD", 0, ambiguous{"-1250 KWD", "-1,25 KWD"}},
		{"de", "1,5", "KWD", 1500, nil},
		{"de", "1.500,250", "KWD", 1500250, nil},
		{"de", "1.500", "JPY", 1500, nil},
		{"de", "1,500", "JPY", 0, ambiguous{"1,5 JPY", "1500 JPY"}},
		{"de", "1500", "JPY", 1500, nil},
		{"de", "1,5", "JPY", 0, currency.ErrPrecision},
		{"de", "12.,50", "EUR", 0, currency.ErrDecimalComma},
		{"de", "5 $", "EUR", 0, currency.ErrWrongCurrency},

		// de-CH: apostrophe groups, point decimals.
		{"de-CH", "1'284.50", "CHF", 128450, nil},
		{"de-CH", "12'.50", "CHF", 0, currency.ErrDecimalPoint},

		// fr: narrow no-break space groups.
		{"fr", "1\u202f284,50\u00a0€", "EUR", 128450, nil},
		{"fr", "1 284,50", "EUR", 128450, nil},
		{"fr", "1284.50", "EUR", 128450, nil},
		{"fr", "1.284,50", "EUR", 128450, nil},
		{"fr", "1.500", "EUR", 0, ambiguous{"1,5 EUR", "1500 EUR"}},
		{"fr", "1 500", "KWD", 1500000, nil},
		{"fr", "1 500", "JPY", 1500, nil},

		// hi: lakh grouping and thousands grouping.
		{"hi", "₹1,23,456.50", "INR", 12345650, nil},
		{"hi", "1,23,456.50", "INR", 12345650, nil},
		{"hi", "123,456.50", "INR", 12345650, nil},
		{"hi", "5 INR", "INR", 500, nil},
		{"hi", "१२३.५०", "INR", 12350, nil},
		{"hi", "12,34,5", "INR", 0, currency.ErrDecimalPoint},
		{"hi", "1,23,456", "KWD", 123456000, nil},
		{"hi", "1,500", "KWD", 1500000, nil},
		{"hi", "1.500", "KWD", 0, ambiguous{"1.5 KWD", "1500 KWD"}},

		// ar: Arabic digits and marks; U+066B is only ever a decimal.
		{"ar", "١٢\u066b٥٠", "SAR", 1250, nil},
		{"ar", "١\u066c٢٨٤\u066b٥٠ ر.س.", "SAR", 128450, nil},
		{"ar", "\u200f1,284.50\u00a0ر.س.\u200f", "SAR", 128450, nil},
		{"ar", "SAR 12.50", "SAR", 1250, nil},
		{"ar", "١\u066b٥٠٠", "KWD", 1500, nil},
		{"ar", "1,500", "KWD", 1500000, nil},
		{"ar", "1.500", "KWD", 0, ambiguous{"1.5 KWD", "1500 KWD"}},
		{"ar", "١٢\u066b٥", "JPY", 0, currency.ErrPrecision},
		{"ar", "$5", "SAR", 0, currency.ErrWrongCurrency},

		// ja: no decimals for JPY.
		{"ja", "￥1285", "JPY", 1285, nil},
		{"ja", "¥1285", "JPY", 1285, nil},
		{"ja", "￥1,285", "JPY", 1285, nil}, // how ja displays it
		{"ja", "1.500", "JPY", 0, ambiguous{"1.5 JPY", "1500 JPY"}},
		{"ja", "1285円", "JPY", 0, currency.ErrInvalid}, // CLDR has no 円 symbol
		{"ja", "CN¥5", "JPY", 0, currency.ErrWrongCurrency},
		{"ja", "$12.50", "USD", 1250, nil},
	}
	for _, c := range cases {
		got, err := currency.ParseNormalized(c.tag, c.in, c.code)
		var amb *currency.AmbiguousError
		switch want := c.err.(type) {
		case nil:
			if err != nil || got != c.want {
				t.Errorf("ParseNormalized(%q, %q, %q) = %d, %v; want %d", c.tag, c.in, c.code, got, err, c.want)
			}
		case ambiguous:
			if !errors.As(err, &amb) {
				t.Errorf("ParseNormalized(%q, %q, %q) = %d, %v; want ambiguous", c.tag, c.in, c.code, got, err)
				continue
			}
			if a, b := amb.Choices(c.tag, c.code); a != want.a || b != want.b {
				t.Errorf("ParseNormalized(%q, %q, %q): choices %q, %q; want %q, %q", c.tag, c.in, c.code, a, b, want.a, want.b)
			}
		case error:
			if !errors.Is(err, want) || errors.As(err, &amb) {
				t.Errorf("ParseNormalized(%q, %q, %q) = %d, %v; want %v", c.tag, c.in, c.code, got, err, want)
			}
		}
	}
}

// Shared symbols (§5.1, step 2): a spelling the field's own currency
// uses in the page's locale or in root is that currency, even where
// others share it; a spelling only another currency uses, or another
// code, is refused. One family per row group.
func TestSharedSymbols(t *testing.T) {
	cases := []struct {
		tag, in, code string
		want          int64
		err           error
	}{
		// dollar
		{"en", "$5", "CAD", 500, nil},
		{"en", "CA$5", "CAD", 500, nil},
		{"en", "5 cad", "CAD", 500, nil},
		{"en", "US$5", "CAD", 0, currency.ErrWrongCurrency},
		{"en", "5US$", "CAD", 0, currency.ErrWrongCurrency},
		{"en", "USD 5", "CAD", 0, currency.ErrWrongCurrency},
		{"en", "A$5", "CAD", 0, currency.ErrWrongCurrency},
		{"en", "$5", "AUD", 500, nil},
		{"en", "A$5", "AUD", 500, nil},
		{"en", "US$5", "AUD", 0, currency.ErrWrongCurrency},
		{"en", "CA$5", "AUD", 0, currency.ErrWrongCurrency},
		{"en-AU", "$5", "AUD", 500, nil},
		{"en-AU", "US$5", "AUD", 0, currency.ErrWrongCurrency},
		{"en-CA", "$5", "CAD", 500, nil},
		{"en-CA", "US$5", "CAD", 0, currency.ErrWrongCurrency},
		{"en-CA", "US$5", "USD", 500, nil},
		// peso
		{"es-MX", "$5", "MXN", 500, nil},
		{"en", "MX$5", "MXN", 500, nil},
		{"en", "$5", "MXN", 500, nil},
		{"es-MX", "US$5", "MXN", 0, currency.ErrWrongCurrency},
		{"es-MX", "5 USD", "MXN", 0, currency.ErrWrongCurrency},
		// yen and yuan
		{"zh", "¥5", "CNY", 500, nil},
		{"zh", "CN¥5", "CNY", 500, nil},
		{"zh", "JP¥5", "CNY", 0, currency.ErrWrongCurrency},
		{"ja", "¥5", "JPY", 5, nil},
		{"ja", "CN¥5", "JPY", 0, currency.ErrWrongCurrency},
		{"en", "¥5", "CNY", 500, nil},
		{"en", "5 JPY", "CNY", 0, currency.ErrWrongCurrency},
		// pound
		{"en", "£5", "GBP", 500, nil},
		{"en", "£5", "EGP", 0, currency.ErrWrongCurrency},
		{"en", "E£5", "EGP", 500, nil},
		{"en", "E£5", "GBP", 0, currency.ErrWrongCurrency},
		{"en", "£5", "GIP", 500, nil},
		// krona and krone
		{"sv", "5 kr", "SEK", 500, nil},
		{"en", "kr 5", "NOK", 500, nil},
		{"en", "kr 5", "DKK", 500, nil},
		{"en", "SEK 5", "NOK", 0, currency.ErrWrongCurrency},
		{"en", "kr 5", "EUR", 0, currency.ErrWrongCurrency},
		// rupee
		{"hi", "₹5", "INR", 500, nil},
		{"hi", "Rs 5", "INR", 0, currency.ErrWrongCurrency}, // CLDR gives INR no "Rs"
		{"en", "Rs 5", "PKR", 500, nil},
		{"en", "Rs 5", "LKR", 500, nil},
		{"en", "₹5", "PKR", 0, currency.ErrWrongCurrency},
		{"en", "PKR 5", "LKR", 0, currency.ErrWrongCurrency},
	}
	for _, c := range cases {
		got, err := currency.ParseNormalized(c.tag, c.in, c.code)
		if got != c.want || !errors.Is(err, c.err) || err != nil && c.err == nil {
			t.Errorf("ParseNormalized(%q, %q, %q) = %d, %v; want %d, %v", c.tag, c.in, c.code, got, err, c.want, c.err)
		}
	}
}
