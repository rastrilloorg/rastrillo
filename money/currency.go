// Package money converts exact decimal amounts and currency minor units.
// It has no payment-provider, locale, storage or application dependencies.
package money

import "strings"

var minorUnits = map[string]int{
	// Zero-decimal: the stored integer IS the whole currency unit.
	"bif": 0, "clp": 0, "djf": 0, "gnf": 0, "isk": 0, "jpy": 0, "kmf": 0,
	"krw": 0, "mga": 0, "pyg": 0, "rwf": 0, "ugx": 0, "vnd": 0, "vuv": 0,
	"xaf": 0, "xof": 0, "xpf": 0,
	// Three-decimal: the minor unit is a thousandth.
	"bhd": 3, "iqd": 3, "jod": 3, "kwd": 3, "lyd": 3, "omr": 3, "tnd": 3,
}

// MinorUnits reports the decimal places in the currency's minor unit: 0 for
// JPY and the rest of the zero-decimal set, 3 for the dinars, 2 for everything
// else — including an unrecognized code, which behaves like EUR rather than
// guessing.
func MinorUnits(currency string) int {
	if n, ok := minorUnits[strings.ToLower(currency)]; ok {
		return n
	}
	return 2
}

// CurrencyMinorUnits returns a copy of the non-two-decimal entries.
func CurrencyMinorUnits() map[string]int {
	out := make(map[string]int, len(minorUnits))
	for code, units := range minorUnits {
		out[code] = units
	}
	return out
}
