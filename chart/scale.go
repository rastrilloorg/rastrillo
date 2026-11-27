package chart

import (
	"errors"
	"math"
	"math/big"

	"amadan.net/rastrillo/rastrillo/number"
)

// scale is a value axis: its ticks, exact multiples of a 1, 2 or 5
// step, from the lowest to the highest, and the step's places.
type scale struct {
	low, high *big.Rat
	ticks     []number.Decimal // highest first, as the axis lists them
	places    int
}

// errTooWide is a domain whose span no float can hold, so no position
// on the plot could be worked out from it.
var errTooWide = errors.New("the values span too wide a range to " +
	"draw: check for one far larger or smaller than the rest")

// niceScale covers low to high in steps of 1, 2 or 5 times a power of
// ten: the smallest such step that needs no more than six of them, so
// the labels stay apart on a short plot. A domain of one value is
// widened so the line sits on the plot rather than on its edge.
func niceScale(low, high *big.Rat) (scale, error) {
	if low.Cmp(high) == 0 {
		high = widened(low)
	}
	span := new(big.Rat).Sub(high, low)
	if size, _ := span.Float64(); math.IsInf(size, 0) {
		return scale{}, errTooWide
	}
	exponent := floorLog10(span.Quo(span, big.NewRat(5, 1)))
	for _, mantissa := range []int64{1, 2, 5, 10, 20, 50} {
		step := new(big.Rat).SetInt64(mantissa)
		step.Mul(step, ratPow10(exponent))
		bottom, top := floorDiv(low, step), ceilDiv(high, step)
		steps := new(big.Int).Sub(top, bottom)
		if steps.Cmp(big.NewInt(6)) <= 0 {
			return stepped(bottom, top, step, exponent, mantissa), nil
		}
	}
	return scale{}, errTooWide
}

// stepped lists the ticks from top down to bottom, each step apart.
func stepped(bottom, top *big.Int, step *big.Rat, exponent int,
	mantissa int64) scale {
	if mantissa >= 10 {
		exponent++
	}
	built := scale{places: max(0, -exponent)}
	for index := new(big.Int).Set(top); index.Cmp(bottom) >= 0; index.Sub(
		index, big.NewInt(1)) {
		tick := new(big.Rat).Mul(new(big.Rat).SetInt(index), step)
		built.ticks = append(built.ticks,
			number.Decimal(tick.FloatString(built.places)))
	}
	built.high = new(big.Rat).Mul(new(big.Rat).SetInt(top), step)
	built.low = new(big.Rat).Mul(new(big.Rat).SetInt(bottom), step)
	return built
}

// floorLog10 is the power of ten at or below a positive value, worked
// out exactly: a float's logarithm of a span far below its range is of
// zero, and would collapse every mark onto the axis.
func floorLog10(value *big.Rat) int {
	exponent := len(value.Num().String()) - len(value.Denom().String())
	for ratPow10(exponent).Cmp(value) > 0 {
		exponent--
	}
	for ratPow10(exponent+1).Cmp(value) <= 0 {
		exponent++
	}
	return exponent
}

// widened is the top of a one-value domain: the value's own size above
// it, or one above zero.
func widened(value *big.Rat) *big.Rat {
	size := new(big.Rat).Abs(value)
	if size.Sign() == 0 {
		size.SetInt64(1)
	}
	return size.Add(size, value)
}

func ratPow10(exponent int) *big.Rat {
	if exponent >= 0 {
		return new(big.Rat).SetInt(pow10(exponent))
	}
	return new(big.Rat).SetFrac(big.NewInt(1), pow10(-exponent))
}

// floorDiv is value/step rounded down: big.Int's Div is Euclidean, and
// a Rat's denominator is positive.
func floorDiv(value, step *big.Rat) *big.Int {
	ratio := new(big.Rat).Quo(value, step)
	return new(big.Int).Div(ratio.Num(), ratio.Denom())
}

func ceilDiv(value, step *big.Rat) *big.Int {
	ratio := new(big.Rat).Quo(value, step)
	quotient, modulus := new(big.Int).DivMod(
		ratio.Num(), ratio.Denom(), new(big.Int))
	if modulus.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

// along is where value falls between low and high, from 0 to size.
func along(value, low, high *big.Rat, size int64) *big.Rat {
	position := new(big.Rat).Sub(value, low)
	position.Quo(position, new(big.Rat).Sub(high, low))
	return position.Mul(position, big.NewRat(size, 1))
}
