package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/dates"
)

const dateKindMsg = "date: %q is not a kind of date; write one of %s"

// dateCall is {{date $L .At "kind"}}: a time.Time, or a *time.Time
// that may be nil (an absent date, written as nothing), in its own
// zone, as the locale's CLDR pattern for kind writes it.
func dateCall(call localeCall, locale string) (any, error) {
	if err := call.arity(2, false); err != nil {
		return nil, err
	}
	kind, ok := call.rest[1].(string)
	if !ok {
		return nil, fmt.Errorf(argTypeMsg, "date", 2+call.offset,
			call.rest[1], "string")
	}
	if !slices.Contains(dates.Kinds, kind) {
		return nil, fmt.Errorf(dateKindMsg, kind,
			strings.Join(dates.Kinds, ", "))
	}
	var at time.Time
	switch value := call.rest[0].(type) {
	case time.Time:
		at = value
	case *time.Time:
		if value == nil {
			return "", nil
		}
		at = *value
	default:
		return nil, fmt.Errorf(argTypeMsg, "date", 1+call.offset,
			call.rest[0], "time.Time")
	}
	return dates.For(locale).Format(kind, at), nil
}
