// Package currency formats, seeds and parses money: an integer count of
// a currency's minor unit plus its ISO 4217 code, written in a reader's
// locale from CLDR data.
package currency

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"

	"amadan.net/rastrillo/rastrillo/locales"
)

// Errors. The first four are operator errors; the last five carry the
// catalog key's suffix, which form maps to the message a person sees.
var (
	ErrMalformedCurrency   = errors.New("currency code is not three ASCII letters")
	ErrUnsupportedCurrency = errors.New("currency code is not in the storage set")
	ErrNotInteger          = errors.New("amount is not an integer count of minor units")
	ErrOutOfRange          = errors.New("amount does not fit in int64")

	ErrInvalid      = errors.New("money_invalid")
	ErrPrecision    = errors.New("money_precision")
	ErrDecimalComma = errors.New("money_decimal_comma")
	ErrDecimalPoint = errors.New("money_decimal_point")
	ErrCurrencySign = errors.New("money_currency")
)

// Normalize is the one validation every entry point uses: trim Unicode
// white space, require exactly three ASCII letters, upper-case, and
// require membership of the frozen storage set. A code outside the set
// is refused rather than given a guessed scale: KDW, a typo of KWD,
// would otherwise store fils as if they were cents.
func Normalize(code string) (string, error) {
	s := strings.TrimFunc(code, unicode.IsSpace)
	if len(s) != 3 || !isAlpha(s) {
		return "", ErrMalformedCurrency
	}
	s = strings.ToUpper(s)
	if _, ok := locales.Storage(s); !ok {
		return "", ErrUnsupportedCurrency
	}
	return s, nil
}

// EligibleDefault reports whether code may be the default for new
// records. Storage and eligibility differ: a withdrawn code (HRK) still
// reads and formats, but no new record should start in it.
func EligibleDefault(code string) bool {
	c, err := Normalize(code)
	if err != nil {
		return false
	}
	cur, _ := locales.Storage(c)
	return cur.Eligible
}

// Exponent is code's frozen number of minor-unit digits.
func Exponent(code string) (int, error) {
	c, err := Normalize(code)
	if err != nil {
		return 0, err
	}
	return exponentOf(c), nil
}

// exponentOf is Exponent for a code Normalize has already accepted.
func exponentOf(code string) int {
	cur, _ := locales.Storage(code)
	return cur.Exponent
}

// Resolve is locales.Resolve. It is here because the form contract
// validates an echoed parsing locale with Resolve(v).ID == v, and a
// caller handling money should not need a second import for that.
func Resolve(tag string) locales.Resolution { return locales.Resolve(tag) }

// AmountFromAny is the template function's argument check. It refuses
// floats and strings, and unsigned values above MaxInt64, which an
// unchecked conversion would turn into MinInt64. A nil pointer is an
// absent amount, not zero.
func AmountFromAny(v any) (minor int64, present bool, err error) {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0, false, nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() > math.MaxInt64 {
			return 0, false, ErrOutOfRange
		}
		return int64(rv.Uint()), true, nil
	}
	return 0, false, fmt.Errorf("%w: got %T", ErrNotInteger, v)
}

// Legacy is a manifest's [legacy] table: the currency and decimal scale
// that integers stored before money had a currency were written in.
// The upgrade and the event replay both convert with it, so they share
// one type rather than two that could drift.
type Legacy struct {
	Currency string
	Scale    int
}

// Appendix C.2's op.legacy.inexact and op.legacy.overflow, verbatim.
const (
	legacyInexactMsg  = "%d at scale %d is %s, which %s cannot hold"
	legacyOverflowMsg = "%d at scale %d is too large for %s"
)

// Rescale converts an integer at scale decimal places to code's frozen
// exponent, exactly or not at all. It never rounds: a legacy amount the
// currency cannot hold is refused so a person decides, since a silently
// rounded price is a wrong price nobody chose.
func Rescale(v int64, scale int, code string) (int64, error) {
	c, err := Normalize(code)
	if err != nil {
		return 0, err
	}
	exp := exponentOf(c)
	out := v
	for diff := exp - scale; diff > 0; diff-- {
		if out > math.MaxInt64/10 || out < math.MinInt64/10 {
			return 0, fmt.Errorf(legacyOverflowMsg, v, scale, c)
		}
		out *= 10
	}
	for diff := exp - scale; diff < 0; diff++ {
		if out%10 != 0 {
			return 0, fmt.Errorf(legacyInexactMsg, v, scale, decimalAt(v, scale), c)
		}
		out /= 10
	}
	return out, nil
}

// decimalAt writes v with a point scale places from the right, so the
// inexact message names the amount as the operator stored it (12.50),
// not as an integer whose scale they must work out.
func decimalAt(v int64, scale int) string {
	whole, frac := splitDigits(v, scale)
	s := whole
	if scale > 0 {
		s += "." + frac
	}
	if v < 0 {
		s = "-" + s
	}
	return s
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return false
		}
	}
	return s != ""
}
