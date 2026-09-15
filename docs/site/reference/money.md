# 🤖 money

`amadan.net/rastrillo/rastrillo/money`

Parse decimal amounts into signed `int64` minor units with `money`. The package uses only the Go standard library.

Install it with:

```sh
go get amadan.net/rastrillo/rastrillo/money@v0.1.0
```

The package is an independent nested module in the Rastrillo repository. Its release tags use the `money/` prefix, such as `money/v0.1.0`.

```go
package main

import (
	"fmt"

	"amadan.net/rastrillo/rastrillo/money"
)

func main() {
	minor, err := money.ParseDecimal("1.299", "BHD")
	fmt.Println(minor, err)
	fmt.Println(money.Decimal(1299, "bhd"))
}
```

This prints:

```text
1299 <nil>
1.299
```

`MinorUnits(currency string) int` returns the number of decimal places: `0`, `2`, or `3`. The lookup ignores case. Unknown and empty currencies use two places. This lookup does not validate currency codes.

`CurrencyMinorUnits() map[string]int` returns a new, mutable map containing the entries that do not use two places. Changes to that map do not affect later lookups.

`ParseDecimal(s, currency string) (int64, error)` trims surrounding whitespace and accepts a leading sign and a decimal dot. Blank input returns zero. It rejects exponents, grouping marks, commas, invalid syntax, and values outside signed `int64`. Extra fractional digits are allowed only when they are all zero. It uses integer arithmetic and never rounds.

`Decimal(minor int64, currency string) string` formats every signed `int64`, including `math.MinInt64`, without grouping.

Keep locale formatting, price limits, tax, exchange rates and payment-provider scaling in your application. The `form` package keeps its two-decimal input rules.
