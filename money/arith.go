package money

import "math"

// Add returns a+b. It refuses to combine different currencies
// (ErrCurrencyMismatch) and refuses to wrap on overflow (ErrOverflow)
// rather than do either silently.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, ErrCurrencyMismatch
	}
	sum, ok := addInt64(a.minor, b.minor)
	if !ok {
		return Amount{}, ErrOverflow
	}
	return Amount{minor: sum, currency: a.currency}, nil
}

// Sub returns a-b, under the same currency and overflow rules as Add.
// It is its own overflow-checked subtraction rather than a.Add(b.Neg()),
// because Neg cannot correctly negate math.MinInt64 (see Neg's doc) and
// Sub should not inherit that limitation for an operation that does not
// need it.
func (a Amount) Sub(b Amount) (Amount, error) {
	if a.currency != b.currency {
		return Amount{}, ErrCurrencyMismatch
	}
	diff, ok := subInt64(a.minor, b.minor)
	if !ok {
		return Amount{}, ErrOverflow
	}
	return Amount{minor: diff, currency: a.currency}, nil
}

// Neg returns -a. The one value it cannot correctly negate is a minor
// unit count of exactly math.MinInt64, whose negation does not fit in
// an int64 and would silently wrap back to itself — a wrong sign
// returned with no error, which is worse than Neg having no error
// return at all. math.MinInt64 minor units (tens of quadrillions of a
// two-decimal currency's major unit) is reachable — MulInt(2) of
// -1<<62 returns it without overflowing — but rare enough that Neg's
// contract still has no error return for it; instead it panics, trading a silent wrong answer for a loud one,
// consistent with every other operation in this file never answering
// an overflow quietly. Sub and MulInt do not depend on Neg, so they
// are not exposed to this case.
func (a Amount) Neg() Amount {
	if a.minor == math.MinInt64 {
		panic("money: Neg of math.MinInt64 minor units has no representable result")
	}
	return Amount{minor: -a.minor, currency: a.currency}
}

// MulInt returns a*n, refusing to wrap on overflow.
func (a Amount) MulInt(n int64) (Amount, error) {
	p, ok := mulInt64(a.minor, n)
	if !ok {
		return Amount{}, ErrOverflow
	}
	return Amount{minor: p, currency: a.currency}, nil
}

// Cmp returns -1, 0 or 1 as a is less than, equal to, or greater than
// b, refusing to compare across currencies.
func (a Amount) Cmp(b Amount) (int, error) {
	if a.currency != b.currency {
		return 0, ErrCurrencyMismatch
	}
	switch {
	case a.minor < b.minor:
		return -1, nil
	case a.minor > b.minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// addInt64 adds two int64s, reporting ok=false instead of wrapping on
// overflow. The check is the standard one: after a+b, the sum must
// have moved in the direction b pointed.
func addInt64(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

// subInt64 subtracts two int64s, reporting ok=false instead of
// wrapping on overflow.
func subInt64(a, b int64) (int64, bool) {
	diff := a - b
	if (b < 0 && diff < a) || (b > 0 && diff > a) {
		return 0, false
	}
	return diff, true
}

// mulInt64 multiplies two int64s, reporting ok=false instead of
// wrapping on overflow. The one case the divide-back check below
// cannot see for itself is math.MinInt64 * -1: negating MinInt64
// overflows on its own, before the "does dividing back give us what we
// started with" check even runs, and on most platforms that overflow
// silently wraps to MinInt64 again — which would pass the check and
// hide the overflow. So that case is rejected explicitly.
func mulInt64(a, n int64) (int64, bool) {
	if a == 0 || n == 0 {
		return 0, true
	}
	if (a == math.MinInt64 && n == -1) || (n == math.MinInt64 && a == -1) {
		return 0, false
	}
	p := a * n
	if p/n != a {
		return 0, false
	}
	return p, true
}
