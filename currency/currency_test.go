package currency_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// These tests link every language pack, like an app that imports the
// all-modern convenience pack. localestest/gallery links only the default 12.

// Findings 3-6: one normalisation for every entry point, the tender
// filter, and storage separate from eligibility.
func TestNormalizeCurrency(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		err      error
		eligible bool
	}{
		{" eur\t", "EUR", nil, true},
		{"KDW", "", currency.ErrUnsupportedCurrency, false}, // a typo of KWD is refused, never given 2 decimals
		{"EU", "", currency.ErrMalformedCurrency, false},
		{"E1R", "", currency.ErrMalformedCurrency, false},
		{"CLF", "", currency.ErrUnsupportedCurrency, false}, // _tender false
		{"UYW", "", currency.ErrUnsupportedCurrency, false}, // _tender false
		{"SLL", "SLL", nil, false},                          // withdrawn 2023; ISO 2 = money 2: stored
		{"HRK", "HRK", nil, false},                          // withdrawn 2023: stored, not a new default
		{"MGA", "MGA", nil, true},                           // reviewed deviation: money 0, ISO 2
		{"CHE", "CHE", nil, false},                          // ISO fund code: stored, never a default
		{"BYR", "", currency.ErrUnsupportedCurrency, false}, // ISO 0, money 2: excluded
		{"ITL", "", currency.ErrUnsupportedCurrency, false}, // withdrawn before the earliest pinned edition
		{"SLE", "SLE", nil, true},
		{"XCG", "XCG", nil, true},
	}
	for _, c := range cases {
		got, err := currency.Normalize(c.in)
		if got != c.want || !errors.Is(err, c.err) || err == nil && c.err != nil {
			t.Errorf("Normalize(%q) = %q, %v; want %q, %v", c.in, got, err, c.want, c.err)
		}
		if currency.EligibleDefault(c.in) != c.eligible {
			t.Errorf("EligibleDefault(%q) = %v", c.in, !c.eligible)
		}
	}
	if _, err := currency.Format("en", 1500, "KDW"); !errors.Is(err, currency.ErrUnsupportedCurrency) {
		t.Errorf("Format with an unsupported code: err = %v", err)
	}
}

func TestAmountFromAny(t *testing.T) {
	var nilPtr *int64
	big := uint64(1 << 63)
	cases := []struct {
		in      any
		want    int64
		present bool
		err     error
	}{
		{int64(math.MinInt64), math.MinInt64, true, nil},
		{uint64(math.MaxInt64), math.MaxInt64, true, nil},
		{big, 0, false, currency.ErrOutOfRange},
		{&big, 0, false, currency.ErrOutOfRange},
		{nilPtr, 0, false, nil},
		{1.5, 0, false, currency.ErrNotInteger},
		{"128450", 0, false, currency.ErrNotInteger},
	}
	for _, c := range cases {
		got, present, err := currency.AmountFromAny(c.in)
		if got != c.want || present != c.present || !errors.Is(err, c.err) || err == nil && c.err != nil {
			t.Errorf("AmountFromAny(%v) = %d, %v, %v", c.in, got, present, err)
		}
	}
}

func TestExponent(t *testing.T) {
	for code, want := range map[string]int{"usd": 2, " JPY ": 0, "KWD": 3, "MGA": 0, "SLL": 2} {
		if got, err := currency.Exponent(code); err != nil || got != want {
			t.Errorf("Exponent(%q) = %d, %v; want %d", code, got, err, want)
		}
	}
	if _, err := currency.Exponent("KDW"); !errors.Is(err, currency.ErrUnsupportedCurrency) {
		t.Errorf("Exponent(KDW) = %v", err)
	}
}

func TestRescale(t *testing.T) {
	for _, c := range []struct {
		v     int64
		scale int
		code  string
		want  int64
		err   string
	}{
		{1250, 2, "USD", 1250, ""},
		{1200, 2, "JPY", 12, ""},
		{1250, 2, "JPY", 0, "1250 at scale 2 is 12.50, which JPY cannot hold"},
		{1250, 2, "KWD", 12500, ""},
		{1250, 2, "SLL", 1250, ""},
		{math.MaxInt64 / 5, 2, "KWD", 0, "is too large for KWD"},
		{-1250, 2, "KWD", -12500, ""},
		{0, 18, "JPY", 0, ""},
	} {
		got, err := currency.Rescale(c.v, c.scale, c.code)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("Rescale(%d, %d, %s) = %d, %v; want error %q", c.v, c.scale, c.code, got, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Rescale(%d, %d, %s) = %d, %v; want %d", c.v, c.scale, c.code, got, err, c.want)
		}
	}
}
