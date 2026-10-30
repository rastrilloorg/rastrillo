package money

import (
	"math/big"
	"sort"
)

// AllocateProRata splits total across len(weights) parts in
// proportion to weights, with no minor unit lost and no minor unit
// invented: the parts always sum to exactly total. Weights must be
// non-negative; AllocateProRata is not a way to subtract a share.
//
// The base split is total*weight[i]/sum(weights), floored. Flooring
// every part necessarily leaves some minor units undistributed — the
// classic largest-remainder method hands them out one minor unit at a
// time to the parts whose floored split lost the most in rounding,
// ties broken by ascending index, until nothing is left over. That is
// the one distribution rule that is both exact and doesn't quietly
// favour whichever index happens to come first.
//
// The intermediate total*weight[i] is computed in math/big rather
// than int64: weights here are share counts (creditors, unit
// classes), not currency, and an int64 total times an int64 weight
// can overflow int64 long before either operand looks unreasonable.
// Every part is bounded in magnitude by total itself once divided
// back, so converting the final parts to int64 cannot overflow.
func AllocateProRata(total Amount, weights []int64) ([]Amount, error) {
	if len(weights) == 0 {
		return nil, ErrNoWeights
	}
	sumW := new(big.Int)
	for _, w := range weights {
		if w < 0 {
			return nil, ErrNegativeWeight
		}
		sumW.Add(sumW, big.NewInt(w))
	}

	parts := make([]Amount, len(weights))
	if sumW.Sign() == 0 {
		if !total.IsZero() {
			return nil, ErrZeroWeightSum
		}
		for i := range parts {
			parts[i] = Zero(total.Currency())
		}
		return parts, nil
	}

	tot := big.NewInt(total.Minor())
	bases := make([]int64, len(weights))
	remainders := make([]*big.Int, len(weights))
	sumBases := new(big.Int)
	for i, w := range weights {
		prod := new(big.Int).Mul(tot, big.NewInt(w))
		// DivMod is Euclidean division: the remainder is always in
		// [0, sumW), whichever sign prod (i.e. total) has, which is
		// exactly the "floor" split the largest-remainder method
		// needs — Go's native / and % truncate toward zero instead
		// and would put a negative total's remainders in the wrong
		// range for the comparison below.
		q, r := new(big.Int), new(big.Int)
		q.DivMod(prod, sumW, r)
		bases[i] = q.Int64()
		remainders[i] = r
		sumBases.Add(sumBases, q)
	}

	// leftover = total - sum(bases) is, by construction, the exact
	// count of parts still owed one more minor unit; see the package
	// test for the derivation this relies on.
	leftover := new(big.Int).Sub(tot, sumBases).Int64()

	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if c := remainders[a].Cmp(remainders[b]); c != 0 {
			return c > 0 // larger remainder first
		}
		return a < b // ties: lower index first
	})

	for k := int64(0); k < leftover; k++ {
		bases[order[k]]++
	}

	for i, b := range bases {
		parts[i] = New(b, total.Currency())
	}
	return parts, nil
}
