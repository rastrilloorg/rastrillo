// Package form holds the plain, framework-independent helpers a
// generated form handler needs: money parsing/formatting and a field
// error map. These used to be stamped privately into every generated
// action file; they live here once now, serving generated and
// hand-written handlers alike.
package form

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatCents renders cents as a dollar string for a DISPLAY context.
//
// It writes a dollar sign and nothing else: this package has no notion
// of a currency, and neither does anything else in the framework, so an
// app whose money is not dollars formats its own. That is a real gap
// rather than a considered minimalism — see the spec — and the reason
// this doc says so out loud is that the function name does not.
// (show.html's Fields/Title, index.GET's Rows) — the only money
// formatting a generated template ever sees there; a template never
// does money math itself. The sign, if any, is written once up front
// against the absolute value: cents/100 and cents%100 both truncate
// toward zero in Go, so naively formatting a negative cents value
// directly (an earlier draft did) mangles it into something like
// "$-1.-50" instead of "-$1.50". ParseCents below never actually
// hands this function a negative value (v1 rejects negative money
// outright), but a stored value could in principle be negative from
// some other path, so the sign is still handled correctly here as
// defense in depth.
func FormatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

// FormatCentsPlain renders cents exactly like FormatCents but without
// the leading "$" — the formatter edit.GET (and the OTHER field
// group's current values on a validation-failure re-render) must use
// to seed a form field a browser might resubmit completely unchanged:
// the seed has to be exactly what ParseCents itself accepts back in,
// and ParseCents rejects a leading "$" (see its own doc). Using
// FormatCents there instead (an earlier draft did) meant resubmitting
// an untouched Money field always 400ed.
func FormatCentsPlain(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// ParseCents parses a decimal amount (e.g. "12.34") into cents,
// rejecting more than two decimal places. An empty string
// parses to zero cents, not an error — a Required Money field rejects
// blankness on the raw text before this runs. v1 also has no use for
// negative prices, so any sign character is rejected outright as a
// field error rather than accepted and applied: the whole and
// fractional parts must each be composed entirely of ASCII digits.
// This is stricter than handing each half to strconv.ParseInt
// directly (an earlier draft did),
// which happily accepts its own leading "+"/"-" in either half — so
// "12.-5" or "12.+5" would silently mis-parse into a different
// magnitude than the digits alone suggest, rather than being rejected
// as the not-an-amount that it is.
func ParseCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if hasFrac && len(frac) > 2 {
		return 0, &Error{Key: "rastrillo.ui.money_precision", Msg: moneyPrecisionEN}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if whole == "" {
		whole = "0"
	}
	if !isDigits(whole) || !isDigits(frac) {
		return 0, &Error{Key: "rastrillo.ui.money_invalid", Msg: moneyInvalidEN}
	}
	wholeN, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, &Error{Key: "rastrillo.ui.money_invalid", Msg: moneyInvalidEN}
	}
	fracN, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, &Error{Key: "rastrillo.ui.money_invalid", Msg: moneyInvalidEN}
	}
	return wholeN*100 + fracN, nil
}

// Error is a field error that names the catalog key for its message as
// well as carrying the message itself.
//
// This package is deliberately framework-independent — it imports
// nothing from rastrillo, and a generated action or a hand-written
// handler uses it without a Ctx in reach — so it cannot call T. A
// package-level translator hook would be worse than not translating at
// all: the locale is per request, and a global would hand one request's
// language to another's error.
//
// So the error carries the key and lets whoever has a translator use
// it. A caller with one renders T(err.Key); a caller without one prints
// err.Error() and gets the English. That is the same three-step
// fallback the rest of the framework walks, moved to the one place a
// package with no request context can put it.
type Error struct {
	// Key is a rastrillo.ui.* catalog key, translated in every shipped
	// locale.
	Key string
	// Msg is the English, and what Error returns. It is the fallback
	// for a caller that does not look at Key, which is most of them.
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// The English for the two money errors, held here rather than inline so
// the strings the catalog translates and the strings this package falls
// back to cannot drift apart.
//
// Neither says "dollar" any more, and that is the fix rather than a
// tidy-up: this package formats and parses a decimal amount, the
// framework stores no currency anywhere, and telling a reader in Tokyo
// to "enter a valid dollar amount" was wrong in every locale including
// English.
const (
	moneyInvalidEN   = "Enter a valid amount."
	moneyPrecisionEN = "Enter an amount with at most 2 decimal places."
)

// isDigits reports whether s is non-empty and every byte is an ASCII
// digit — ParseCents' guard against a sign character ("-"/"+")
// slipping through either half via strconv.ParseInt's own leniency.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
