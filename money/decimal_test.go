package money_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"

	"amadan.net/rastrillo/rastrillo/money"
)

func TestDecimalCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/decimal.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input, Currency string
		Minor                 *string
		Decimal               string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := money.ParseDecimal(c.Input, c.Currency)
			if c.Minor == nil {
				if err == nil {
					t.Fatalf("ParseDecimal(%q, %q) = %d; want refusal", c.Input, c.Currency, got)
				}
				return
			}
			want, parseErr := strconv.ParseInt(*c.Minor, 10, 64)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if err != nil || got != want {
				t.Fatalf("parse = %d, %v; want %d", got, err, want)
			}
			if got := money.Decimal(want, c.Currency); got != c.Decimal {
				t.Errorf("format = %q; want %q", got, c.Decimal)
			}
		})
	}
}

func TestDecimalRoundTrip(t *testing.T) {
	for _, currency := range []string{"jpy", "eur", "bhd", "unknown"} {
		for _, value := range []int64{math.MinInt64, math.MinInt64 + 1, -9999999999, -1299, -100, -1, 0, 1, 100, 1299, 9999999999, math.MaxInt64 - 1, math.MaxInt64} {
			text := money.Decimal(value, currency)
			got, err := money.ParseDecimal(text, currency)
			if err != nil || got != value {
				t.Errorf("%s %d through %q = %d, %v", currency, value, text, got, err)
			}
		}
	}
}

func TestCurrencyTableCopy(t *testing.T) {
	table := money.CurrencyMinorUnits()
	table["jpy"] = 2
	delete(table, "bhd")
	if money.MinorUnits("JPY") != 0 || money.MinorUnits("bhd") != 3 || money.MinorUnits("unknown") != 2 {
		t.Fatal("caller changed currency precision")
	}
	if money.CurrencyMinorUnits()["jpy"] != 0 {
		t.Fatal("table export shares mutable state")
	}
}

func ExampleParseDecimal() {
	minor, err := money.ParseDecimal("1.299", "bhd")
	fmt.Println(minor, err)
	fmt.Println(money.Decimal(minor, "bhd"))
	// Output:
	// 1299 <nil>
	// 1.299
}
