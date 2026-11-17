package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"amadan.net/rastrillo/rastrillo/number"
)

// WithLocale keeps displayed quantities in the same locale as the page.
// Bind it on a per-request clone, alongside WithT and WithIcons.
// An empty or invalid locale falls back to the app's default locale.
func WithLocale(locale string) Option {
	return func(c *config) { c.locale = locale }
}

var decimalType = reflect.TypeOf(number.Decimal(""))

// formatNumber writes a quantity from the locale's CLDR pack, exactly:
// an integer or a number.Decimal digit for digit, a float as its
// shortest exact decimal. Anything else comes back unchanged, and a
// string only when integerStrings asks for integer strings, so an
// identifier or a version passed as a string is never regrouped.
func formatNumber(
	value any, locale string, integerStrings bool, options number.Options,
) (any, error) {
	if absentDecimal(value) {
		return "", nil
	}
	held := reflect.ValueOf(value)
	for held.IsValid() && (held.Kind() == reflect.Pointer ||
		held.Kind() == reflect.Interface) {
		held = held.Elem()
	}
	if !held.IsValid() {
		return value, nil
	}
	if held.Type() == decimalType {
		return number.Format(locale, number.Decimal(held.String()), options)
	}
	switch held.Kind() {
	case reflect.Float32:
		return number.FormatFloat(locale, held.Float(), 32, options)
	case reflect.Float64:
		return number.FormatFloat(locale, held.Float(), 64, options)
	}
	text, ok := decimalOf(held, integerStrings)
	if !ok {
		return value, nil
	}
	return number.Format(locale, number.Decimal(text), options)
}

// absentDecimal is a number.Decimal that holds no number: "", which
// Parsed.Number returns for a blank field, or a nil pointer to one. It
// writes nothing, as an absent amount does in {{money}}, rather than
// stop the page.
func absentDecimal(value any) bool {
	switch held := value.(type) {
	case number.Decimal:
		return held == ""
	case *number.Decimal:
		return held == nil || *held == ""
	}
	return false
}

// decimalOf is a value's exact decimal. Converting integers to float64
// would change counts above 2^53, so they are written as they are.
func decimalOf(held reflect.Value, integerStrings bool) (string, bool) {
	switch held.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64:
		return strconv.FormatInt(held.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		return strconv.FormatUint(held.Uint(), 10), true
	case reflect.String:
		return integerString(held.String(), integerStrings)
	}
	return "", false
}

// integerString accepts only an integer already written canonically,
// so "001234", "4.1k" and "1,234.50" are shown exactly as given.
func integerString(text string, integerStrings bool) (string, bool) {
	if !integerStrings {
		return "", false
	}
	if whole, err := strconv.ParseInt(text, 10, 64); err == nil &&
		strconv.FormatInt(whole, 10) == text {
		return text, true
	}
	if whole, err := strconv.ParseUint(text, 10, 64); err == nil &&
		strconv.FormatUint(whole, 10) == text {
		return text, true
	}
	return "", false
}

// numberOptions reads {{number}}'s words after the value: "plain",
// "grouped", "percent", and "places" with a count, in any order.
func numberOptions(words []any) (number.Options, error) {
	var options number.Options
	for index := 0; index < len(words); index++ {
		word, _ := words[index].(string)
		switch word {
		case "plain":
			options.Plain = true
		case "grouped":
			options.Plain = false
		case "percent":
			options.Percent = true
		case "places":
			index++
			places, ok := placesOf(words, index)
			if !ok {
				return options, errNumberPlaces
			}
			options.Places = places
		default:
			return options, fmt.Errorf(numberOptionMsg, words[index])
		}
	}
	return options, nil
}

// placesOf is the count after "places": 0 to 20, as any integer type,
// so a field's int64 setting passes as a literal 2 does.
func placesOf(words []any, index int) (int, bool) {
	if index >= len(words) {
		return 0, false
	}
	held := reflect.ValueOf(words[index])
	var places int64
	switch held.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64:
		places = held.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		places = int64(min(held.Uint(), 21))
	default:
		return 0, false
	}
	return int(places), places >= 0 && places <= 20
}

const numberOptionMsg = `number: %v is not an option; write "plain", ` +
	`"grouped", "percent" or "places" and a count`

var errNumberPlaces = errors.New(
	`number: "places" needs a count from 0 to 20 after it, like "places" 2`)
