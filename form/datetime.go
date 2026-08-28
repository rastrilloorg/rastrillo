package form

import "time"

// Wire layouts for Date, Time and DateTime — the exact formats Parse
// accepts and every scaffolded date/time input submits. These are not
// display formats; formatting for a human belongs to the renderer.
const (
	dateLayout     = "2006-01-02"
	timeLayout     = "15:04"
	dateTimeLayout = "2006-01-02T15:04"
)

// Catalog keys used as Errors values for Date, Time and DateTime.
// These are plain strings, not a new error type — the form package
// doesn't import the root package, so the keys are literals here,
// resolved to human text at render time via T (see ui/funcs.go).
const (
	// dateInvalidKey is reported when a present value fails to parse
	// against its Kind's wire layout.
	dateInvalidKey = "rastrillo.ui.date_invalid"
	// dateEndBeforeStartKey is what Range reports on the end field
	// when it precedes start.
	dateEndBeforeStartKey = "rastrillo.ui.date_end_before_start"
	// fieldRequiredKey is the Required message for Date, Time and
	// DateTime only — Text/Textarea/Money keep their humanized
	// English (see Parse's doc) until a later sweep routes all
	// rendering through T.
	fieldRequiredKey = "rastrillo.ui.field_required"
)

// location returns f.Location, defaulting to time.UTC — the nil
// convention Date and DateTime parse against.
func location(f Field) *time.Location {
	if f.Location == nil {
		return time.UTC
	}
	return f.Location
}

// Date returns a Date field's parsed value. Unknown names, an empty
// optional value, and an unparseable value all return the zero
// time.Time — check OK (or Errors) to tell a real midnight-UTC
// zero-date submission apart from "nothing parsed".
func (p *Parsed) Date(name string) time.Time { return p.dates[name] }

// Time returns a Time field's parsed clock reading. ok is false for
// an unknown name, an empty optional value, or an unparseable value —
// h and m are both 0 in that case, never a partial reading.
func (p *Parsed) Time(name string) (h, m int, ok bool) {
	hm, ok := p.times[name]
	if !ok {
		return 0, 0, false
	}
	return hm[0], hm[1], true
}

// DateTime returns a DateTime field's parsed value, same zero-value
// convention as Date.
func (p *Parsed) DateTime(name string) time.Time { return p.dates[name] }

// Range checks that the end field's instant doesn't precede start's,
// adding dateEndBeforeStartKey on end when it does. Both start and
// end must already have been parsed by Parse as Date or DateTime
// fields (Range reads them via the shared dates map, so either Kind
// works interchangeably here). If either side is empty or failed to
// parse, Range adds nothing — Parse already recorded that field's own
// error (required or date_invalid), and Range never overwrites or
// duplicates it. Equal instants are not an error. Range compares only
// fields that actually parsed (a comma-ok read of the dates map, not
// an IsZero check) — a genuinely-submitted zero date participates
// like any other instant, rather than being mistaken for unset.
func Range(p *Parsed, start, end string) {
	s, sok := p.dates[start]
	e, eok := p.dates[end]
	if !sok || !eok {
		return
	}
	if e.Before(s) {
		p.errs[end] = dateEndBeforeStartKey
	}
}
