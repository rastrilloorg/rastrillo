package money

import "errors"

// ErrCurrencyMismatch is returned by any Amount operation that combines
// two Amounts of different currencies — GBP 100 + USD 100 is not GBP
// 200 or USD 200, it is a bug at the call site, so this refuses rather
// than picking one currency silently. The two currency strings are
// compared with ==; neither side is case-folded first, so a caller
// that mixes "GBP" and "gbp" gets this error rather than a silent
// combine. Normalise before constructing an Amount if that matters —
// MinorUnits's own case-insensitivity is a different kind of lookup (a
// decimal-places guess that must never fail) and is not a precedent
// for Amount equality.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// ErrOverflow is returned instead of wrapping when an arithmetic
// operation on an Amount would exceed what an int64 minor-unit count
// can hold. A ledger that wraps a large enough figure into a small or
// negative one is a bug, not an edge case, so overflow is always an
// error and never silent.
var ErrOverflow = errors.New("money: overflow")

// ErrNoWeights is returned by AllocateProRata when there is nothing to
// allocate against: no weights at all.
var ErrNoWeights = errors.New("money: allocateprorata: no weights")

// ErrNegativeWeight is returned by AllocateProRata for any negative
// weight — a negative share is not a smaller share, it is a different
// operation this function does not perform.
var ErrNegativeWeight = errors.New("money: allocateprorata: negative weight")

// ErrZeroWeightSum is returned by AllocateProRata when every weight is
// zero but the total being allocated is not: there is no basis on
// which to split a non-zero amount across all-zero shares.
var ErrZeroWeightSum = errors.New("money: allocateprorata: weights sum to zero but total does not")
