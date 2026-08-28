package form

import (
	"net/url"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	// Valid date, no Location given: parses in UTC.
	p := parseForm(t, url.Values{"When": {"2026-08-28"}},
		Field{Name: "When", Kind: Date})
	if !p.OK() {
		t.Fatalf("valid date refused: %v", p.Errors())
	}
	got := p.Date("When")
	want := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location().String() != "UTC" {
		t.Errorf("Date(When) = %v, want %v in UTC", got, want)
	}

	// Empty optional: zero value, no error.
	p = parseForm(t, url.Values{}, Field{Name: "When", Kind: Date})
	if !p.OK() {
		t.Fatalf("empty optional date errored: %v", p.Errors())
	}
	if !p.Date("When").IsZero() {
		t.Errorf("empty optional Date = %v, want zero", p.Date("When"))
	}

	// Empty required: field_required key.
	p = parseForm(t, url.Values{}, Field{Name: "When", Kind: Date, Required: true})
	if got := p.Errors()["When"]; got != "rastrillo.ui.field_required" {
		t.Errorf("empty required Date error = %q", got)
	}

	// Garbage: date_invalid key, echoed raw, zero value.
	p = parseForm(t, url.Values{"When": {"not-a-date"}},
		Field{Name: "When", Kind: Date})
	if got := p.Errors()["When"]; got != "rastrillo.ui.date_invalid" {
		t.Errorf("garbage Date error = %q", got)
	}
	if p.Echo()["When"] != "not-a-date" {
		t.Errorf("garbage Date echo = %q, want raw", p.Echo()["When"])
	}
	if !p.Date("When").IsZero() {
		t.Errorf("garbage Date value = %v, want zero", p.Date("When"))
	}
}

func TestParseDateLocation(t *testing.T) {
	loc := time.FixedZone("Test/Minus0500", -5*3600)
	p := parseForm(t, url.Values{"When": {"2026-08-28"}},
		Field{Name: "When", Kind: Date, Location: loc})
	if !p.OK() {
		t.Fatalf("valid date refused: %v", p.Errors())
	}
	got := p.Date("When")
	if got.Location().String() != loc.String() {
		t.Errorf("Date(When) zone = %v, want %v", got.Location(), loc)
	}
	want := time.Date(2026, 8, 28, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("Date(When) = %v, want %v", got, want)
	}
}

func TestParseTime(t *testing.T) {
	p := parseForm(t, url.Values{"Start": {"15:04"}},
		Field{Name: "Start", Kind: Time})
	if !p.OK() {
		t.Fatalf("valid time refused: %v", p.Errors())
	}
	h, m, ok := p.Time("Start")
	if !ok || h != 15 || m != 4 {
		t.Errorf("Time(Start) = %d:%d ok=%v, want 15:04 ok=true", h, m, ok)
	}

	// Empty optional: ok=false, no error.
	p = parseForm(t, url.Values{}, Field{Name: "Start", Kind: Time})
	if !p.OK() {
		t.Fatalf("empty optional time errored: %v", p.Errors())
	}
	if _, _, ok := p.Time("Start"); ok {
		t.Error("empty optional Time reported ok=true")
	}

	// Empty required: field_required key.
	p = parseForm(t, url.Values{}, Field{Name: "Start", Kind: Time, Required: true})
	if got := p.Errors()["Start"]; got != "rastrillo.ui.field_required" {
		t.Errorf("empty required Time error = %q", got)
	}

	// Garbage: date_invalid key, echoed raw, ok=false.
	p = parseForm(t, url.Values{"Start": {"nope"}},
		Field{Name: "Start", Kind: Time})
	if got := p.Errors()["Start"]; got != "rastrillo.ui.date_invalid" {
		t.Errorf("garbage Time error = %q", got)
	}
	if p.Echo()["Start"] != "nope" {
		t.Errorf("garbage Time echo = %q, want raw", p.Echo()["Start"])
	}
	if _, _, ok := p.Time("Start"); ok {
		t.Error("garbage Time reported ok=true")
	}
}

func TestParseDateTime(t *testing.T) {
	p := parseForm(t, url.Values{"Starts": {"2026-08-28T15:04"}},
		Field{Name: "Starts", Kind: DateTime})
	if !p.OK() {
		t.Fatalf("valid datetime refused: %v", p.Errors())
	}
	got := p.DateTime("Starts")
	want := time.Date(2026, 8, 28, 15, 4, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location().String() != "UTC" {
		t.Errorf("DateTime(Starts) = %v, want %v in UTC", got, want)
	}

	// Empty optional: zero value, no error.
	p = parseForm(t, url.Values{}, Field{Name: "Starts", Kind: DateTime})
	if !p.OK() {
		t.Fatalf("empty optional datetime errored: %v", p.Errors())
	}
	if !p.DateTime("Starts").IsZero() {
		t.Errorf("empty optional DateTime = %v, want zero", p.DateTime("Starts"))
	}

	// Empty required: field_required key.
	p = parseForm(t, url.Values{}, Field{Name: "Starts", Kind: DateTime, Required: true})
	if got := p.Errors()["Starts"]; got != "rastrillo.ui.field_required" {
		t.Errorf("empty required DateTime error = %q", got)
	}

	// Garbage: date_invalid key, echoed raw, zero value.
	p = parseForm(t, url.Values{"Starts": {"garbage"}},
		Field{Name: "Starts", Kind: DateTime})
	if got := p.Errors()["Starts"]; got != "rastrillo.ui.date_invalid" {
		t.Errorf("garbage DateTime error = %q", got)
	}
	if p.Echo()["Starts"] != "garbage" {
		t.Errorf("garbage DateTime echo = %q, want raw", p.Echo()["Starts"])
	}
	if !p.DateTime("Starts").IsZero() {
		t.Errorf("garbage DateTime value = %v, want zero", p.DateTime("Starts"))
	}
}

func TestRange(t *testing.T) {
	// Ok: end after start.
	p := parseForm(t, url.Values{"Start": {"2026-08-28"}, "End": {"2026-08-29"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if !p.OK() {
		t.Errorf("valid range flagged: %v", p.Errors())
	}

	// End before start: error on End.
	p = parseForm(t, url.Values{"Start": {"2026-08-29"}, "End": {"2026-08-28"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if got := p.Errors()["End"]; got != "rastrillo.ui.date_end_before_start" {
		t.Errorf("end-before-start error = %q", got)
	}
	if _, ok := p.Errors()["Start"]; ok {
		t.Error("Range errored the Start field, want End only")
	}

	// Equal instants: ok.
	p = parseForm(t, url.Values{"Start": {"2026-08-28"}, "End": {"2026-08-28"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if !p.OK() {
		t.Errorf("equal-instant range flagged: %v", p.Errors())
	}

	// One side empty: no additional error (both optional here).
	p = parseForm(t, url.Values{"End": {"2026-08-28"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if !p.OK() {
		t.Errorf("empty start range flagged: %v", p.Errors())
	}

	// One side invalid: the existing parse error stands, Range adds nothing.
	p = parseForm(t, url.Values{"Start": {"garbage"}, "End": {"2026-08-28"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if got := p.Errors()["Start"]; got != "rastrillo.ui.date_invalid" {
		t.Errorf("invalid Start error = %q, want date_invalid preserved", got)
	}
	if _, ok := p.Errors()["End"]; ok {
		t.Error("Range added an End error over an invalid Start")
	}

	// Works for DateTime fields too.
	p = parseForm(t, url.Values{"Start": {"2026-08-28T15:04"}, "End": {"2026-08-28T14:00"}},
		Field{Name: "Start", Kind: DateTime}, Field{Name: "End", Kind: DateTime})
	Range(p, "Start", "End")
	if got := p.Errors()["End"]; got != "rastrillo.ui.date_end_before_start" {
		t.Errorf("DateTime end-before-start error = %q", got)
	}

	// A genuinely-submitted zero date ("0001-01-01") is a real parsed
	// instant, not "unset" — it must participate in the comparison
	// like any other date, proving Range reads parse-presence
	// (comma-ok) rather than IsZero.
	p = parseForm(t, url.Values{"Start": {"0001-01-01"}, "End": {"2026-08-28"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if !p.OK() {
		t.Errorf("zero-date start before End flagged: %v", p.Errors())
	}

	// Same zero date as Start, but now after End: must error, which
	// an IsZero-based check would have missed entirely.
	p = parseForm(t, url.Values{"Start": {"0001-01-01"}, "End": {"0000-12-31"}},
		Field{Name: "Start", Kind: Date}, Field{Name: "End", Kind: Date})
	Range(p, "Start", "End")
	if got := p.Errors()["End"]; got != "rastrillo.ui.date_end_before_start" {
		t.Errorf("zero-date-start-after-End error = %q, want date_end_before_start", got)
	}
}

func TestDatetimeAccessorsUnknownNames(t *testing.T) {
	p := parseForm(t, url.Values{}, Field{Name: "Known", Kind: Date})
	if !p.Date("Unknown").IsZero() {
		t.Errorf("Date(Unknown) = %v, want zero", p.Date("Unknown"))
	}
	if !p.DateTime("Unknown").IsZero() {
		t.Errorf("DateTime(Unknown) = %v, want zero", p.DateTime("Unknown"))
	}
	if h, m, ok := p.Time("Unknown"); ok || h != 0 || m != 0 {
		t.Errorf("Time(Unknown) = %d:%d ok=%v, want zero/false", h, m, ok)
	}
}
