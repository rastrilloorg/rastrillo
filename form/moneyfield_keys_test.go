package form

import (
	"os"
	"strings"
	"testing"
)

// form cannot import the catalogs (the root package imports form), so
// a key named here and missing from en.toml would render as the raw
// key on every page. Read the file instead.
func TestMoneyFieldKeysAreInTheCatalog(t *testing.T) {
	data, err := os.ReadFile("../locales/en.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		moneyInvalidKey, moneyPrecisionKey, moneyPrecision0Key, moneyPrecision3Key,
		moneyDecimalCommaKey, moneyDecimalPointKey, moneyCurrencyKey, moneyNegativeKey,
		moneyReenterKey, moneyCurrencyChangedKey, moneyWrongCurrencyKey, moneyAmbiguousKey,
		moneyAmbiguousPrecisionKey, fieldRequiredKey,
	} {
		if !strings.Contains(string(data), "\n"+key+" = ") {
			t.Errorf("en.toml has no %s", key)
		}
	}
}
