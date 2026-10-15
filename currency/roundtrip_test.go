package currency_test

import (
	"math"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// Every shipped locale, every storage code, the boundaries: the seed
// and the display both read back. cmd/check runs the same property on
// a wider grid; this is the offline copy.
func TestRoundTripEveryLocale(t *testing.T) {
	if testing.Short() {
		t.Skip("about 300,000 formats and parses")
	}
	amounts := []int64{math.MinInt64, -1, 0, 1, 123456789, math.MaxInt64}
	for _, id := range append(locales.LinkedIDs(), "root") {
		for _, code := range locales.StorageCodes() {
			for _, m := range amounts {
				shown, _ := currency.Format(id, m, code)
				seed, _ := currency.InputValue(id, m, code)
				a, errA := currency.Parse(id, shown, code)
				b, errB := currency.Parse(id, seed, code)
				if a != m || b != m || errA != nil || errB != nil {
					t.Fatalf("%s %s %d: Parse(%q) = %d, %v; Parse(%q) = %d, %v", id, code, m, shown, a, errA, seed, b, errB)
				}
			}
		}
	}
}
