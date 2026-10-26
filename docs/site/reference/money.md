# 🤖 money

`amadan.net/rastrillo/rastrillo/money`

Parse decimal amounts into signed `int64` minor units with `money`. The package uses only the Go standard library.

Install it with:

```sh
go get amadan.net/rastrillo/rastrillo/money@v0.2.0
```

The package is an independent nested module in the Rastrillo repository. Its release tags use the `money/` prefix, such as `money/v0.2.0`.

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

This package does not format for a locale. In a rastrillo app,
[`currency`](/docs/reference/currency) does. Keep price limits, tax,
exchange rates and payment-provider scaling in your application.

## Amount

`Amount` holds an `int64` minor-unit count and a currency string. There
is no constructor that takes a float: an `Amount` either came from an
integer, or from another `Amount`.

```go
a := money.New(1000, "GBP") // £10.00
z := money.Zero("GBP")
a.Minor()    // 1000
a.Currency() // "GBP"
a.IsZero()   // false
a.Negative() // false
a.String()   // "GBP 1000" — a debug form, not for a reader
```

`Add(b Amount) (Amount, error)` and `Sub(b Amount) (Amount, error)`
return `ErrCurrencyMismatch` for two different currencies and
`ErrOverflow` for a result outside `int64` — never a panic and never a
silent wrap. The two currency strings are compared with `==`; neither
side is folded to a common case first, so an `Amount` built from
`"GBP"` and one built from `"gbp"` mismatch. Normalise with
[`currency.Normalize`](/docs/reference/currency) before constructing an
`Amount` if that matters to you — `MinorUnits`'s own case-insensitivity
above is a different kind of lookup (a decimal-places guess that must
never fail) and is not a precedent for `Amount` equality.

`Neg() Amount` negates; its one unreachable edge is `math.MinInt64`
minor units, which cannot be negated in `int64` and is accepted as a
value no real caller's arithmetic produces. `MulInt(n int64) (Amount,
error)` multiplies by an integer, overflow-checked. `Cmp(b Amount)
(int, error)` compares, returning `-1`, `0` or `1`, and the same
`ErrCurrencyMismatch` as `Add` and `Sub` for two different currencies.

## AllocateProRata

`AllocateProRata(total Amount, weights []int64) ([]Amount, error)`
splits `total` across `len(weights)` parts in proportion to `weights`,
with no minor unit lost and none invented: the parts always sum to
exactly `total`, including when `total` is negative.

```go
parts, err := money.AllocateProRata(money.New(100, "GBP"), []int64{1, 1, 1})
// parts = [34, 33, 33] pence — 100/3 floors to 33 each, and the one
// leftover penny goes to the part whose floored split lost the most
// in rounding (the largest-remainder method), ties broken by the
// lowest index.
```

A negative weight is `ErrNegativeWeight` — a smaller share and a
negative one are different operations, and this is only the first. No
weights at all is `ErrNoWeights`. Weights that are all zero are only
valid when `total` is also zero (every part is then `Zero`); a
non-zero `total` with nothing to divide it by is `ErrZeroWeightSum`,
because there is no basis for the split.

## The storage set

`MinorUnits` answers for any code, guessing two places for one it does
not know. Rastrillo stores an amount only in a currency whose minor
units ISO 4217 and this package agree on. That is 174 codes. 153 of
them can be the currency of a new record; the other 21, such as HRK and
SLL, stay for records that already use them.

`currency.Normalize` checks a code against the set. A code outside it,
such as XAU, CLF or a typo like KDW, is refused rather than given a
guessed scale. The set only grows: removing a code or changing its
exponent fails the build.
