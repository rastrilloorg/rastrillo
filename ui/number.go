package ui

import (
	"reflect"
	"strconv"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// WithLocale keeps displayed quantities in the same locale as the page.
// Bind it on a per-request clone, alongside WithT and WithIcons.
// An empty or invalid locale falls back to English.
func WithLocale(locale string) Option {
	return func(c *config) { c.locale = locale }
}

func formatNumber(value any, locale string, integerStrings bool) any {
	v := reflect.ValueOf(value)
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		v = v.Elem()
	}
	if !v.IsValid() {
		return value
	}
	var n any
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n = v.Uint()
	case reflect.Float32:
		n = float32(v.Float())
	case reflect.Float64:
		n = v.Float()
	case reflect.String:
		if !integerStrings {
			return value
		}
		s := v.String()
		if i, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(i, 10) == s {
			n = i
		} else if u, err := strconv.ParseUint(s, 10, 64); err == nil && strconv.FormatUint(u, 10) == s {
			n = u
		} else {
			return value
		}
	default:
		return value
	}
	tag, err := language.Parse(locale)
	if locale == "" || err != nil {
		tag = language.English
	}
	// Converting integers to float64 would change counts above 2^53.
	// Exact precision also avoids rounding fractional quantities for display.
	return message.NewPrinter(tag).Sprintf("%v", number.Decimal(n, number.Precision(-1), number.MaxFractionDigits(32767)))
}
