package form

import (
	"net/http"
	"strings"
	"time"
)

// Kind is how a Field's submitted text is read. The zero value is
// Text, so a plain Field{Name: "Title"} does the common thing.
type Kind string

const (
	// Text is trimmed on the way in — the stored value AND the
	// re-render echo are both the trimmed text.
	Text Kind = ""
	// Textarea is preserved exactly as typed: the value and the echo
	// keep the raw submission (leading/trailing whitespace included),
	// and only the required check trims.
	Textarea Kind = "textarea"
	// Money parses through ParseCents (integer cents; see its doc for
	// what it refuses). The echo keeps the raw text so a rejected
	// "12.345" re-renders as typed, and an empty optional Money is 0.
	Money Kind = "money"
	// Date parses the wire format "2006-01-02" (time.DateOnly) via
	// time.ParseInLocation in Field.Location (nil = time.UTC). The
	// echo keeps the raw text; an empty optional Date is the zero
	// time.Time, never an error.
	Date Kind = "date"
	// Time parses the wire format "15:04" — a bare clock reading, no
	// location involved. The echo keeps the raw text; an empty
	// optional Time reports ok=false from Parsed.Time, never an error.
	Time Kind = "time"
	// DateTime parses the wire format "2006-01-02T15:04" via
	// time.ParseInLocation in Field.Location (nil = time.UTC), same as
	// Date. The echo keeps the raw text; an empty optional DateTime is
	// the zero time.Time, never an error.
	DateTime Kind = "datetime"
)

// Field declares one form field for Parse.
type Field struct {
	Name     string
	Kind     Kind
	Required bool
	// Location is the zone Date and DateTime parse their wire value
	// in; nil means time.UTC. Time is clock-only and ignores it.
	Location *time.Location
}

// Parsed is Parse's result: the per-field values, the echo map for a
// validation re-render, and the errors. Zero allocations of ceremony —
// the 422/400 re-render convention (seed the raw input back, name each
// field's problem) falls out of Echo and Errors directly.
type Parsed struct {
	strings map[string]string
	cents   map[string]int64
	// dates holds both Date and DateTime results, keyed by field
	// name — a field is only ever one Kind, so the two accessors
	// (Date, DateTime) never collide reading the same map.
	dates map[string]time.Time
	times map[string][2]int // Time results: [hour, minute]
	echo  map[string]string
	errs  Errors
}

// Parse reads and validates r's submitted fields in one pass — the
// same rules for hand-written handlers and generated actions alike:
//
//   - Text is trimmed; a Required Text that trims to "" gets
//     "<Humanized name> is required".
//   - Textarea keeps the raw submission; Required is checked against
//     the trimmed text but never alters the value.
//   - Money goes through ParseCents. Required is checked against the
//     RAW text — "" is required-blank, while "0" is a present, valid
//     zero — and a present-but-unparseable value reports the parse
//     error, never the required message.
//   - Date, Time and DateTime each parse their exact wire format via
//     time.ParseInLocation (Date/DateTime honour Field.Location, nil
//     = UTC; Time is a bare clock reading with no location). Required
//     is checked against the RAW text, same as Money. A present but
//     unparseable value reports "rastrillo.ui.date_invalid" — a
//     catalog key, not English, resolved by the renderer via T — and
//     never the required message. An empty optional value parses to
//     the zero time.Time (or ok=false for Time), never an error.
//   - The Required message for Date, Time and DateTime is the catalog
//     key "rastrillo.ui.field_required", unlike Text/Textarea/Money's
//     humanized English above — a deliberate, scoped-to-these-three
//     behaviour change (see form/datetime.go).
//
// Parse reads via r.PostFormValue (which parses the form on first
// use); callers that want a body-size cap or a 400 on a malformed
// body wrap r.Body and call r.ParseForm first, as the generated
// actions do.
func Parse(r *http.Request, fields ...Field) *Parsed {
	p := &Parsed{
		strings: map[string]string{},
		cents:   map[string]int64{},
		dates:   map[string]time.Time{},
		times:   map[string][2]int{},
		echo:    map[string]string{},
		errs:    Errors{},
	}
	for _, f := range fields {
		raw := r.PostFormValue(f.Name)
		switch f.Kind {
		case Money:
			p.echo[f.Name] = raw
			cents, err := ParseCents(raw)
			p.cents[f.Name] = cents
			if f.Required && raw == "" {
				p.errs[f.Name] = Humanize(f.Name) + " is required"
			} else if err != nil {
				p.errs[f.Name] = err.Error()
			}
		case Date:
			p.echo[f.Name] = raw
			if raw == "" {
				if f.Required {
					p.errs[f.Name] = fieldRequiredKey
				}
				continue
			}
			if t, err := time.ParseInLocation(dateLayout, raw, location(f)); err != nil {
				p.errs[f.Name] = dateInvalidKey
			} else {
				p.dates[f.Name] = t
			}
		case Time:
			p.echo[f.Name] = raw
			if raw == "" {
				if f.Required {
					p.errs[f.Name] = fieldRequiredKey
				}
				continue
			}
			if t, err := time.Parse(timeLayout, raw); err != nil {
				p.errs[f.Name] = dateInvalidKey
			} else {
				p.times[f.Name] = [2]int{t.Hour(), t.Minute()}
			}
		case DateTime:
			p.echo[f.Name] = raw
			if raw == "" {
				if f.Required {
					p.errs[f.Name] = fieldRequiredKey
				}
				continue
			}
			if t, err := time.ParseInLocation(dateTimeLayout, raw, location(f)); err != nil {
				p.errs[f.Name] = dateInvalidKey
			} else {
				p.dates[f.Name] = t
			}
		case Textarea:
			p.strings[f.Name] = raw
			p.echo[f.Name] = raw
			if f.Required && strings.TrimSpace(raw) == "" {
				p.errs[f.Name] = Humanize(f.Name) + " is required"
			}
		default: // Text
			v := strings.TrimSpace(raw)
			p.strings[f.Name] = v
			p.echo[f.Name] = v
			if f.Required && v == "" {
				p.errs[f.Name] = Humanize(f.Name) + " is required"
			}
		}
	}
	return p
}

// OK reports whether every field validated.
func (p *Parsed) OK() bool { return len(p.errs) == 0 }

// Errors is the per-field problem map — empty (never nil) when OK.
func (p *Parsed) Errors() Errors { return p.errs }

// String returns a Text or Textarea field's value (trimmed for Text,
// raw for Textarea). Unknown names return "".
func (p *Parsed) String(name string) string { return p.strings[name] }

// Cents returns a Money field's parsed value — 0 for an empty
// optional field, and 0 alongside an error for an unparseable one
// (check OK before storing). Unknown names return 0.
func (p *Parsed) Cents(name string) int64 { return p.cents[name] }

// Echo is the map a validation re-render seeds the form with: what
// was typed, per field — trimmed for Text, raw for Textarea and
// Money — so nobody retypes a whole form over one bad field.
func (p *Parsed) Echo() map[string]string { return p.echo }

// Humanize turns a Go-ish field name into the label its default
// messages use: split on underscores and interior capitals, lowercase
// the tail, capitalize the front. "MaxPerOrder" -> "Max per order";
// "ticket_types" -> "Ticket types"; "Title" -> "Title".
func Humanize(name string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			flush()
			continue
		}
		isUpper := c >= 'A' && c <= 'Z'
		prevUpper := i > 0 && name[i-1] >= 'A' && name[i-1] <= 'Z'
		if isUpper && i > 0 && !prevUpper {
			flush()
		}
		cur.WriteByte(c)
	}
	flush()
	if len(words) == 0 {
		return name
	}
	out := strings.ToLower(strings.Join(words, " "))
	return strings.ToUpper(out[:1]) + out[1:]
}
