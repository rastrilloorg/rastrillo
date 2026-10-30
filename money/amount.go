package money

import "fmt"

// Amount is an integer number of minor units (pence, cents, ...) in
// one currency. There is deliberately no constructor that takes a
// float: every Amount in a system that uses this type either came
// from an integer input or was computed from other Amounts, so a
// float can never be the value a person is held to. The zero value's
// currency is "", which is usable as a placeholder but combines with
// nothing that holds a real code; use Zero(c) or New(0, c) for a
// usable zero in a given currency.
type Amount struct {
	minor    int64
	currency string
}

// New builds an Amount directly from minor units. It does not validate
// or normalise currency — Normalize, in the currency package, is
// there for callers who need that — because a great many callers
// already hold a currency known good from a prior validation and
// should not pay for it twice.
func New(minor int64, currency string) Amount {
	return Amount{minor: minor, currency: currency}
}

// Zero is the additive identity for currency.
func Zero(currency string) Amount {
	return Amount{minor: 0, currency: currency}
}

// Minor returns the amount as an integer count of minor units.
func (a Amount) Minor() int64 { return a.minor }

// Currency returns the amount's currency code, exactly as given to
// New or Zero — not normalised or case-folded.
func (a Amount) Currency() string { return a.currency }

// IsZero reports whether the amount is exactly zero.
func (a Amount) IsZero() bool { return a.minor == 0 }

// Negative reports whether the amount is less than zero.
func (a Amount) Negative() bool { return a.minor < 0 }

// String is a debug form — "GBP 1234567" — unambiguous about both the
// currency and the fact that the number is minor units, not a decimal
// amount. It is not for showing to a reader: format minor units with
// Decimal, or with the currency package's locale-aware formatting.
func (a Amount) String() string {
	return fmt.Sprintf("%s %d", a.currency, a.minor)
}
