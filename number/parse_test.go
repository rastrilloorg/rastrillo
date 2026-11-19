package number_test

import (
	"errors"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
	"amadan.net/rastrillo/rastrillo/number"
)

// The Fichas level-up spec rev 4 (§6.8) states these readings; each is
// money's §5.1 with no currency.
func TestParseNormalizedReadsWhatHasOneReading(t *testing.T) {
	for _, each := range []struct {
		locale, text string
		want         number.Decimal
	}{
		{"en", "1,200.5", "1200.5"},
		{"en", "1200,5", "1200.5"},
		{"de", "1.200,5", "1200.5"},
		{"de", "1200,5", "1200.5"},
		{"fr", "1\u202f200,5", "1200.5"},
		{"fr", "1 200,5", "1200.5"},
		{"de-CH", "1\u2019200.5", "1200.5"},
		{"de-CH", "1'200.5", "1200.5"},
		{"en", "1,20,000", "120000"},
		{"de", "1,20,000", "120000"},
		{"en", "1,500", "1500"},
		{"de", "1.500", "1500"},
		{"en", "1,5", "1.5"},
		{"en", "1.200,50", "1200.5"},
		{"en", "+5", "5"},
		{"en", "-5", "-5"},
		{"en", "5-", "-5"},
		{"en", "\u22125", "-5"},
		{"en", "(1,200.5)", "-1200.5"},
		{"en", "-0", "0"},
		{"en", "007.50", "7.5"},
		{"en", ".5", "0.5"},
		{"ar", "\u200e-١٢٣", "-123"},
		{"en", "123456789012345678901234567890",
			"123456789012345678901234567890"},
	} {
		got, err := number.ParseNormalized(each.locale, each.text)
		if err != nil || got != each.want {
			t.Errorf("ParseNormalized(%s, %q) = %q, %v; want %q",
				each.locale, each.text, got, err, each.want)
		}
	}
}

func TestParseNormalizedRefusesTwoReadingsAndNamesBoth(t *testing.T) {
	for _, each := range []struct {
		locale, text, smallest, largest string
	}{
		{"en", "1.500", "1.5", "1500"},
		{"de", "1,500", "1,5", "1500"},
		{"fr", "1,500", "1,5", "1500"},
		{"de", "1,234", "1,2340", "1234"},
		{"en", "-1.500", "-1500", "-1.5"},
	} {
		_, err := number.ParseNormalized(each.locale, each.text)
		var ambiguous *number.AmbiguousError
		if !errors.As(err, &ambiguous) {
			t.Errorf("%s %q: %v, want an AmbiguousError", each.locale,
				each.text, err)
			continue
		}
		smallest, largest := ambiguous.Choices(each.locale)
		if smallest != each.smallest || largest != each.largest {
			t.Errorf("%s %q: choices %q and %q, want %q and %q",
				each.locale, each.text, smallest, largest, each.smallest,
				each.largest)
		}
	}
}

func TestParseNormalizedRefusesWhatIsNotANumber(t *testing.T) {
	for text, want := range map[string]error{
		"1e3": number.ErrInvalid, "NaN": number.ErrInvalid,
		"Inf": number.ErrInvalid, "0x10": number.ErrInvalid,
		"twelve": number.ErrInvalid, "1.2.3": number.ErrInvalid,
		"1,,2": number.ErrInvalid, "12,.5": number.ErrInvalid,
		"--5": number.ErrInvalid, "-(5)": number.ErrInvalid,
		"()": number.ErrInvalid, "-": number.ErrInvalid,
		"$5": number.ErrInvalid, "\U0001d7cf": number.ErrInvalid,
		"1234567890123456789012345678901":   number.ErrTooLong,
		"0.0000000000000000000000000000001": number.ErrTooLong,
	} {
		got, err := number.ParseNormalized("en", text)
		if !errors.Is(err, want) {
			t.Errorf("ParseNormalized(en, %q) = %q, %v; want %v", text, got,
				err, want)
		}
	}
	// Zeros count as typed: they are not trimmed into a short number.
	for _, text := range []string{strings.Repeat("0", 100000) + "1",
		"1." + strings.Repeat("0", 100000),
		strings.Repeat("0", 30) + "1", "1." + strings.Repeat("0", 30)} {
		for name, read := range map[string]func(string, string) (
			number.Decimal, error){
			"ParseNormalized": number.ParseNormalized, "Parse": number.Parse,
		} {
			got, err := read("en", text)
			if !errors.Is(err, number.ErrTooLong) {
				t.Errorf("%s of %d characters = %q, %v; want ErrTooLong",
					name, len(text), got, err)
			}
		}
	}
	if got, err := number.ParseNormalized("en", " \u200f "); got != "" ||
		err != nil {
		t.Errorf("blank = %q, %v; want \"\" and no error", got, err)
	}
}

func TestParseReadsOnlyTheLocalesOwnFormat(t *testing.T) {
	for _, each := range []struct {
		locale, text string
		want         number.Decimal
		err          error
	}{
		{"en", "1,200.5", "1200.5", nil},
		{"en", "1200.5", "1200.5", nil},
		{"en", "1200,5", "", number.ErrInvalid},
		{"en", "1 200", "", number.ErrInvalid},
		{"en", "1.500", "1.5", nil},
		{"de", "1.200,5", "1200.5", nil},
		{"de", "1.5", "", number.ErrInvalid},
		{"de", "1,500", "1.5", nil},
		{"fr", "1\u202f200,5", "1200.5", nil},
		{"de-CH", "1\u2019200.5", "1200.5", nil},
		{"hi", "12,34,567", "1234567", nil},
		{"de-CH", "1'234\u2019567", "1234567", nil},
		{"de-CH", "1\u2019234'567.5", "1234567.5", nil},
		{"hi", "1,234,567", "", number.ErrInvalid},
		{"en", "+5", "", number.ErrInvalid},
		{"en", "(5)", "", number.ErrInvalid},
		{"en", "5-", "-5", nil},
	} {
		got, err := number.Parse(each.locale, each.text)
		if got != each.want || !errors.Is(err, each.err) {
			t.Errorf("Parse(%s, %q) = %q, %v; want %q, %v", each.locale,
				each.text, got, err, each.want, each.err)
		}
	}
}

// One table must never read "1.500" two ways: a number reads every
// text exactly as a money field in a three-decimal currency does, in
// locales whose money and plain marks agree.
func TestNumbersReadAsMoneyDoes(t *testing.T) {
	texts := []string{"1.500", "1,500", "1.234,5", "1 234,5", "1,20,000",
		"(12.5)", "12-", "1.2.3", "1,,2", "0,125", "1'000.5", "abc"}
	for _, locale := range []string{"en", "de", "es", "hi", "ar", "sv"} {
		for _, text := range texts {
			ours, err := number.ParseNormalized(locale, text)
			minor, moneyErr := currency.ParseNormalized(locale, text, "KWD")
			var numberAmbiguous *number.AmbiguousError
			var moneyAmbiguous *currency.AmbiguousError
			ambiguous := errors.As(err, &numberAmbiguous)
			moneyAmbiguousToo := errors.As(moneyErr, &moneyAmbiguous)
			switch {
			case ambiguous || moneyAmbiguousToo:
				if ambiguous != moneyAmbiguousToo {
					t.Errorf("%s %q: number %v, money %v", locale, text,
						err, moneyErr)
				}
			case (err == nil) != (moneyErr == nil):
				t.Errorf("%s %q: number %q, %v; money %d, %v", locale,
					text, ours, err, minor, moneyErr)
			case err == nil && ours != thousandths(minor):
				t.Errorf("%s %q: number %q, money %d fils", locale, text,
					ours, minor)
			}
		}
	}
}

func thousandths(minor int64) number.Decimal {
	written, _ := currency.InputValue("en", minor, "KWD")
	read, _ := number.Parse("en", written)
	return read
}

// What a page draws reads back as the same value, in every shipped
// locale: grouped, plain, padded, and as an input's seed.
func TestEveryLocaleReadsBackWhatItWrites(t *testing.T) {
	values := []number.Decimal{"0", "7", "-1234.5", "1234567.891",
		"12345678901234567890.5", "0.25"}
	styles := []number.Options{{}, {Plain: true}, {Places: 2}}
	for _, locale := range locales.LinkedIDs() {
		for _, value := range values {
			for _, options := range styles {
				written, _ := number.Format(locale, value, options)
				readBack(t, locale, written, value)
			}
			seed, _ := number.InputValue(locale, value)
			readBack(t, locale, seed, value)
		}
	}
}

func readBack(t *testing.T, locale, written string, want number.Decimal) {
	t.Helper()
	for name, read := range map[string]func(string, string) (
		number.Decimal, error){
		"ParseNormalized": number.ParseNormalized, "Parse": number.Parse,
	} {
		got, err := read(locale, written)
		if err != nil || got != want {
			t.Errorf("%s(%s, %q) = %q, %v; want %q", name, locale, written,
				got, err, want)
		}
	}
}
