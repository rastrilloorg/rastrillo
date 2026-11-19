package form

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/number"
)

// The catalog keys a Number field reports, read with no request
// translator in reach, as money's are.
const (
	numberInvalidKey   = "rastrillo.ui.number_invalid" // {example}
	numberAmbiguousKey = "rastrillo.ui.number_ambiguous"
	numberTooLongKey   = "rastrillo.ui.number_too_long"
	numberNegativeKey  = "rastrillo.ui.number_negative"
	numberReenterKey   = "rastrillo.ui.number_reenter"
)

// numberExample is the number number_invalid shows, written in the
// reader's locale: "1200.5", or "1200,5" in German.
const numberExample number.Decimal = "1200.5"

// NumberInput is what a number input renders and a failed POST echoes:
// the text and the locale it is written in. ui's field-number partial
// takes them as Value and Locale, and posts the locale back as the
// hidden <Name>.locale.
type NumberInput struct {
	Value, Locale string
}

// SeedNumber is the input for a stored number, or for none (value nil:
// a create form, or an empty field): the value with no group marks, in
// the locale renderLocale resolves to, which is the id the POST's
// locale check accepts.
func SeedNumber(renderLocale string, value *number.Decimal) (
	NumberInput, error) {
	in := NumberInput{Locale: currency.Resolve(renderLocale).ID}
	if value == nil {
		return in, nil
	}
	written, err := number.InputValue(in.Locale, *value)
	in.Value = written
	return in, err
}

// Number is a Number field's value, exact. present is false for a
// blank field and for a field with an error.
func (parsed *Parsed) Number(name string) (value number.Decimal, present bool) {
	value, present = parsed.numbers[name]
	return value, present
}

// NumberInput is the re-render of a submitted number field: the
// person's own text, in the locale it was read in. reseed is true when
// the locale echo was refused: the text's meaning is unknown, so the
// caller seeds the input from the server instead.
func (parsed *Parsed) NumberInput(name, renderLocale string) (
	in NumberInput, reseed bool) {
	locale, read := parsed.echoLocale[name]
	if !read || parsed.reseed[name] {
		return NumberInput{Locale: currency.Resolve(renderLocale).ID}, true
	}
	return NumberInput{Value: parsed.echo[name], Locale: locale}, false
}

// parseNumber is a Number field's POST path, in money's order: the
// locale echo, blank, the unchanged seed, the reading Strict selects,
// the negative policy.
func (parsed *Parsed) parseNumber(request *http.Request, field Field) {
	raw := request.PostFormValue(field.Name)
	parsed.echo[field.Name] = raw
	locale, ok := parsed.numberLocale(request, field)
	if !ok {
		return
	}
	if currency.Clean(raw) == "" {
		if field.Required {
			parsed.errs[field.Name] = fieldRequiredKey
		}
		return
	}
	if unchanged(field, locale, raw) {
		// The person left the drawn text alone, so it is not read: an
		// ambiguous seed such as German "1,234" saves as it was.
		parsed.numbers[field.Name] = *field.StoredNumber
		return
	}
	read := number.ParseNormalized
	if field.Strict {
		read = number.Parse
	}
	value, err := read(locale, raw)
	switch {
	case err != nil:
		key, args := numberError(err, locale)
		parsed.errs[field.Name] = key
		if args != nil {
			parsed.args[field.Name] = args
		}
	case strings.HasPrefix(string(value), "-") && !field.Negative:
		parsed.errs[field.Name] = numberNegativeKey
	default:
		parsed.numbers[field.Name] = value
	}
}

// numberLocale is the locale the text is read in: the echo's, checked,
// with LocaleEcho, and Field.Locale's otherwise.
func (parsed *Parsed) numberLocale(
	request *http.Request, field Field) (string, bool) {
	locale := currency.Resolve(field.Locale).ID
	if field.LocaleEcho {
		echoed, ok := oneValue(request, field.Name+".locale")
		if !ok || !validLocaleEcho(echoed) {
			parsed.errs[field.Name] = numberReenterKey
			parsed.reseed[field.Name] = true
			return "", false
		}
		locale = echoed
	}
	parsed.echoLocale[field.Name] = locale
	return locale, true
}

// unchanged reports text that is exactly the seed StoredNumber draws
// in locale. The seed is computed here, never posted, so it can only
// keep what the field was drawn from.
func unchanged(field Field, locale, raw string) bool {
	if field.StoredNumber == nil {
		return false
	}
	seed, err := number.InputValue(locale, *field.StoredNumber)
	if err != nil {
		// StoredNumber is the server's own value; a malformed one is a
		// programming error, and saving over it would hide that.
		panic(fmt.Sprintf("form: field %q: stored number: %v",
			field.Name, err))
	}
	return strings.TrimSpace(raw) == seed
}

// numberError is the key and arguments for a refused number.
func numberError(err error, locale string) (string, map[string]any) {
	var ambiguous *number.AmbiguousError
	switch {
	case errors.As(err, &ambiguous):
		smallest, largest := ambiguous.Choices(locale)
		return numberAmbiguousKey, map[string]any{"a": smallest,
			"b": largest}
	case errors.Is(err, number.ErrTooLong):
		return numberTooLongKey, nil
	}
	example, _ := number.InputValue(locale, numberExample)
	return numberInvalidKey, map[string]any{"example": example}
}
