package chart

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo/number"
)

// decimalText is number.Decimal's own shape: an optional "-", the
// whole part with no leading zeros, and an optional fraction.
var decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

var decimalType = reflect.TypeFor[number.Decimal]()

// exact is a Decimal as an exact rational, for the arithmetic the
// scale needs: nothing is rounded until a coordinate is written.
func exact(value number.Decimal) *big.Rat {
	rat, _ := new(big.Rat).SetString(string(value))
	return rat
}

// decimalOf reads a value an app passes as a figure: nil, "" or a nil
// pointer is no value (missing), an integer is taken digit for digit,
// a float as its shortest exact decimal, and a number.Decimal as
// written. A string is refused: "4.1k" or "1,200" would be drawn as
// something it is not.
func decimalOf(value any) (number.Decimal, bool, error) {
	held := reflect.ValueOf(value)
	for held.IsValid() && (held.Kind() == reflect.Pointer ||
		held.Kind() == reflect.Interface) {
		held = held.Elem()
	}
	if !held.IsValid() {
		return "", false, nil
	}
	if held.Type() == decimalType {
		return checkedDecimal(number.Decimal(held.String()))
	}
	switch held.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64:
		return number.Decimal(strconv.FormatInt(held.Int(), 10)), true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		return number.Decimal(strconv.FormatUint(held.Uint(), 10)), true,
			nil
	case reflect.Float32, reflect.Float64:
		return floatDecimal(held.Float(), held.Type().Bits())
	}
	return "", false, fmt.Errorf(notANumberMsg, value, value)
}

const (
	notANumberMsg = "%v (%T) is not a number: pass an integer, a " +
		"float or a number.Decimal, or nil for no value"
	notDecimalMsg = "%q is not a number.Decimal: write digits, with " +
		"an optional - and . as in \"-1200.5\""
	notFiniteMsg = "%v is not a finite number: pass nil, or a " +
		"Missing point, for no value"
)

// checkedDecimal is value if it is a decimal, and no value if it is "".
func checkedDecimal(value number.Decimal) (number.Decimal, bool, error) {
	if value == "" {
		return "", false, nil
	}
	if !decimalText.MatchString(string(value)) {
		return "", false, fmt.Errorf(notDecimalMsg, value)
	}
	return value, true, nil
}

func floatDecimal(value float64, bits int) (number.Decimal, bool, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", false, fmt.Errorf(notFiniteMsg, value)
	}
	text := strconv.FormatFloat(value, 'f', -1, bits)
	if text == "-0" {
		text = "0"
	}
	return number.Decimal(text), true, nil
}

// rounded is value rounded half to even to places digits after the
// point, with exactly that many written.
func rounded(value number.Decimal, places int) number.Decimal {
	scaled := exact(value)
	scaled.Mul(scaled, new(big.Rat).SetInt(pow10(places)))
	quotient, remainder := new(big.Int).QuoRem(
		scaled.Num(), scaled.Denom(), new(big.Int))
	twice := new(big.Int).Mul(remainder.Abs(remainder), big.NewInt(2))
	switch twice.Cmp(scaled.Denom()) {
	case 1:
		quotient.Add(quotient, big.NewInt(int64(scaled.Sign())))
	case 0:
		if quotient.Bit(0) == 1 {
			quotient.Add(quotient, big.NewInt(int64(scaled.Sign())))
		}
	}
	return decimalFrom(quotient, places)
}

// decimalFrom writes whole/10^places as a Decimal with places digits
// after the point.
func decimalFrom(whole *big.Int, places int) number.Decimal {
	text := new(big.Rat).SetFrac(whole, pow10(places)).FloatString(places)
	if strings.Trim(text, "-0.") == "" {
		text = strings.TrimPrefix(text, "-")
	}
	return number.Decimal(text)
}

func pow10(places int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
}

// coordinate writes a position in a viewBox to a tenth, as the
// examples Fiesta's checks measure do.
func coordinate(position *big.Rat) string {
	value, _ := position.Float64()
	text := strconv.FormatFloat(math.Round(value*10)/10, 'f', -1, 64)
	if text == "-0" {
		return "0"
	}
	return text
}
