// Package replay holds the money rules generated event stores replay
// with: a currency if and only if an amount, and versions checked
// before kinds.
package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/eventlog"
)

// Version is the event version a currency-aware writer stamps as "V"
// on every event it appends, amount-free and deleted ones included, so
// a later binary can refuse events newer than it understands.
const Version = 2

// Appendix C.2's op.replay.* messages, verbatim.
const (
	msgVersion          = "event %d: version %d is not one this binary understands"
	msgCurrencyNoAmount = "event %d: %s has a currency but no amount"
	msgAmountNoCurrency = "event %d: %s has an amount but no currency"
	msgCurrencyChange   = "event %d: %s changes currency %s to %s: a currency change needs its own event"
	msgV1Currency       = "event %d: a version-1 event carries a currency for %s"
	msgV1NeedsLegacy    = "event %d: a version-1 amount for %s needs [legacy] in the manifest"
	msgAfterUpgrade     = "%s: an event from writer %s (seq %d) has no version but was written after the money upgrade, so a currency-unaware version wrote it"
	msgHalfClear        = "event %d: %s clears the amount or the currency but not both"
	msgFieldError       = "event %d: %s: %w"
	msgBadVersion       = "event %d: the version is not a whole number"
	msgV1Null           = "event %d: a version-1 event has a null amount or currency for %s"
)

// ErrCurrencyChange matches, with errors.Is, the refusal of an amount
// in a currency other than the one its field already holds. A generated
// action writes only the currency it read, so this refusal means the
// record changed after the action read it. The action matches it to
// show the person the form again, as a mismatched currency echo does,
// rather than a 500. Its text is the tail of op.replay.currency_change.
var ErrCurrencyChange = errors.New("a currency change needs its own event")

// currencyChange is op.replay.currency_change, worded exactly as
// approved, and matching ErrCurrencyChange.
type currencyChange struct{ msg string }

func (e *currencyChange) Error() string { return e.msg }
func (e *currencyChange) Unwrap() error { return ErrCurrencyChange }

// Event is one decoded payload. V is nil when the payload has no "V"
// (a version-1, legacy event); an explicit "V": 0 is not the same thing
// and is refused. A key present in Amounts or Currencies is present in
// the payload; absence means "unchanged". Cleared marks a field whose
// amount and currency keys were both JSON null: an explicit clear,
// which absence cannot express without changing what every event
// already written means.
type Event struct {
	V          *int
	Kind       string // "created", "updated" or "deleted"
	Amounts    map[string]int64
	Currencies map[string]string
	Cleared    map[string]bool
}

// Field is one money field of the derived record. Both nil: empty, no
// amount yet. Both set: an amount in a currency. Never one without the
// other.
type Field struct {
	Minor    *int64
	Currency *string
}

// Money names one money field and the two payload keys that carry it,
// for example Price, "price" and "price_currency".
type Money struct {
	Field, AmountKey, CurrencyKey string
}

// Decode reads an event's payload into an Event. Each money key is
// absent (unchanged), JSON null, or a value; both keys null is a clear,
// and one null without the other is refused, since a half-cleared field
// would hold an amount without a currency or a currency without an
// amount.
//
// Decode alone numbers the event 0 in its errors; Apply numbers it by
// its place in the fold.
func Decode(ev eventlog.Event, fields []Money) (Event, error) {
	return decode(0, ev, fields)
}

func decode(i int, ev eventlog.Event, fields []Money) (Event, error) {
	out := Event{Kind: ev.Kind}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return out, err
	}
	if raw, ok := payload["V"]; ok {
		// Atoi accepts only an integer literal: a quoted "2", null or
		// 2.5 is refused rather than read as some version.
		v, err := strconv.Atoi(string(bytes.TrimSpace(raw)))
		if err != nil {
			return out, fmt.Errorf(msgBadVersion, i)
		}
		out.V = &v
		if v != Version {
			// Step refuses the version. Nothing else is read first: a
			// newer writer's payload may use shapes this binary would
			// misreport as a broken event of its own version.
			return out, nil
		}
	}
	for _, m := range fields {
		rawAmount, hasAmount := payload[m.AmountKey]
		rawCode, hasCode := payload[m.CurrencyKey]
		nullAmount := hasAmount && isNull(rawAmount)
		nullCode := hasCode && isNull(rawCode)
		if nullAmount || nullCode {
			// Legacy writers omitted an empty value and never wrote
			// null, so a null in an unversioned event is not theirs.
			if out.V == nil {
				return out, fmt.Errorf(msgV1Null, i, m.Field)
			}
			if !nullAmount || !nullCode {
				return out, fmt.Errorf(msgHalfClear, i, m.Field)
			}
			if out.Cleared == nil {
				out.Cleared = map[string]bool{}
			}
			out.Cleared[m.Field] = true
			continue
		}
		if hasAmount {
			n, err := minor(rawAmount)
			if err != nil {
				return out, fmt.Errorf(msgFieldError, i, m.Field, err)
			}
			if out.Amounts == nil {
				out.Amounts = map[string]int64{}
			}
			out.Amounts[m.Field] = n
		}
		if hasCode {
			var code string
			if err := json.Unmarshal(rawCode, &code); err != nil {
				return out, fmt.Errorf(msgFieldError, i, m.Field, currency.ErrMalformedCurrency)
			}
			if out.Currencies == nil {
				out.Currencies = map[string]string{}
			}
			out.Currencies[m.Field] = code
		}
	}
	return out, nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// minor reads an amount as a JSON integer literal. Not json.Unmarshal:
// it cannot tell an amount too large for int64 from one that is not an
// integer, and the operator fixes the two differently. A float such as
// 12.5 or 1e3 is refused rather than truncated or rounded, since
// rounding a stored amount is a wrong price nobody chose.
func minor(raw json.RawMessage) (int64, error) {
	n, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		return 0, currency.ErrOutOfRange
	}
	if err != nil {
		return 0, currency.ErrNotInteger
	}
	return n, nil
}

// Step applies one event to rec, which it mutates; i is the event's
// place in the fold, for messages. On an error rec may be partly
// updated: the caller fails the whole fold, never serves it.
func Step(rec map[string]Field, i int, ev Event, fields []string, legacy *currency.Legacy) error {
	// The version is checked before anything reads the kind: a newer
	// writer's tombstone is as unreadable as its update.
	legacyEvent := ev.V == nil
	if !legacyEvent && *ev.V != Version {
		return fmt.Errorf(msgVersion, i, *ev.V)
	}
	if ev.Kind == "deleted" {
		return nil
	}
	for _, f := range fields {
		amount, hasAmount := ev.Amounts[f]
		code, hasCurrency := ev.Currencies[f]
		cur := rec[f]
		if !legacyEvent {
			switch {
			case ev.Cleared[f] && (hasAmount || hasCurrency):
				return fmt.Errorf(msgHalfClear, i, f)
			case ev.Cleared[f]:
				// A clear is allowed whether or not the field had an
				// amount, and leaves it as if it never had one, as an
				// SQL row's NULL/NULL does.
				delete(rec, f)
				continue
			case hasCurrency && !hasAmount:
				return fmt.Errorf(msgCurrencyNoAmount, i, f)
			case hasAmount && !hasCurrency:
				return fmt.Errorf(msgAmountNoCurrency, i, f)
			case !hasAmount:
				continue // unchanged; a field that never had an amount stays empty
			}
			c, err := currency.Normalize(code)
			if err != nil {
				return fmt.Errorf(msgFieldError, i, f, err)
			}
			// "Unchanged" means equal after Normalize (trimmed, upper
			// case) to the currency replay has derived for this field
			// so far, in the merged order.
			if cur.Currency != nil && *cur.Currency != c {
				return &currencyChange{fmt.Sprintf(msgCurrencyChange, i, f, *cur.Currency, c)}
			}
			a := amount
			rec[f] = Field{Minor: &a, Currency: &c}
			continue
		}
		// Version 1: legacy writers had no currency at all, and never
		// wrote null.
		if ev.Cleared[f] {
			return fmt.Errorf(msgV1Null, i, f)
		}
		if hasCurrency {
			return fmt.Errorf(msgV1Currency, i, f)
		}
		if !hasAmount {
			continue
		}
		if legacy == nil {
			return fmt.Errorf(msgV1NeedsLegacy, i, f)
		}
		target := legacy.Currency
		if cur.Currency != nil {
			target = *cur.Currency
		}
		v, err := currency.Rescale(amount, legacy.Scale, target)
		if err != nil {
			return fmt.Errorf(msgFieldError, i, f, err)
		}
		// Rescale accepted target, so this cannot fail; it stores the
		// normalised code, as a version-2 amount does, should [legacy]
		// have been written in lower case.
		c, _ := currency.Normalize(target)
		rec[f] = Field{Minor: &v, Currency: &c}
	}
	return nil
}

// Replay folds one stream's events, in merged order, for the given
// money fields.
func Replay(events []Event, fields []string, legacy *currency.Legacy) (map[string]Field, error) {
	rec := map[string]Field{}
	for i, ev := range events {
		if err := Step(rec, i, ev, fields, legacy); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// CheckUnversioned judges an event already known to carry no "V". Only
// a currency-unaware binary writes such an event, so one is legitimate
// only if the money upgrade's snapshot shows it was written before the
// upgrade: its (stream, writer) pair has a row there and its seq is not
// above it. The pair is the unit because the log has no global
// position; a pair with no row had written nothing before the upgrade.
func CheckUnversioned(boundary map[eventlog.StreamWriter]int64, ev eventlog.Event) error {
	last, ok := boundary[eventlog.StreamWriter{Stream: ev.Stream, Writer: ev.Writer}]
	if !ok || ev.Seq > last {
		return fmt.Errorf(msgAfterUpgrade, ev.Stream, ev.Writer, ev.Seq)
	}
	return nil
}

// Rules is everything one store's fold needs. HasBoundary is false
// where the money upgrade's snapshot does not exist, and then no
// unversioned event is judged against it.
type Rules struct {
	Fields      []Money
	Legacy      *currency.Legacy
	Boundary    map[eventlog.StreamWriter]int64
	HasBoundary bool
}

// Apply decodes ev, checks an unversioned one against the upgrade
// boundary, and steps rec with it. Every fold a store does goes through
// Apply, reads as well as the boot check, so an unversioned event
// ingested after the app started is refused on the next read, not only
// at the next boot.
func (r Rules) Apply(rec map[string]Field, i int, ev eventlog.Event) error {
	e, err := decode(i, ev, r.Fields)
	if err != nil {
		return err
	}
	if e.V == nil && r.HasBoundary {
		if err := CheckUnversioned(r.Boundary, ev); err != nil {
			return err
		}
	}
	names := make([]string, len(r.Fields))
	for k, m := range r.Fields {
		names[k] = m.Field
	}
	return Step(rec, i, e, names, r.Legacy)
}
