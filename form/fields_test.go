package form

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func parseForm(t *testing.T, values url.Values, fields ...Field) *Parsed {
	t.Helper()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return Parse(r, fields...)
}

func TestParseTextTrimsAndRequires(t *testing.T) {
	p := parseForm(t, url.Values{"Title": {"  padded  "}, "Status": {"   "}},
		Field{Name: "Title", Required: true},
		Field{Name: "Status", Required: true},
		Field{Name: "Note"},
	)
	if p.String("Title") != "padded" {
		t.Errorf("Title = %q, want trimmed", p.String("Title"))
	}
	if p.Echo()["Title"] != "padded" {
		t.Errorf("Text echo = %q, want the trimmed value", p.Echo()["Title"])
	}
	if p.OK() {
		t.Fatal("whitespace-only required Text passed")
	}
	if got := p.Errors()["Status"]; got != "Status is required" {
		t.Errorf("Status error = %q", got)
	}
	if _, ok := p.Errors()["Note"]; ok {
		t.Error("optional blank field errored")
	}
}

func TestParseTextareaPreservesRaw(t *testing.T) {
	p := parseForm(t, url.Values{"Body": {"  keep me  "}},
		Field{Name: "Body", Kind: Textarea, Required: true})
	if !p.OK() {
		t.Fatal("non-blank textarea refused")
	}
	if p.String("Body") != "  keep me  " || p.Echo()["Body"] != "  keep me  " {
		t.Errorf("textarea trimmed: value %q echo %q", p.String("Body"), p.Echo()["Body"])
	}

	// Whitespace-only still trips Required — checked trimmed, stored raw.
	p = parseForm(t, url.Values{"Body": {"   "}},
		Field{Name: "Body", Kind: Textarea, Required: true})
	if p.OK() {
		t.Fatal("whitespace-only required textarea passed")
	}
	if p.Echo()["Body"] != "   " {
		t.Errorf("echo = %q, want the raw whitespace back", p.Echo()["Body"])
	}
}

func TestParseMoney(t *testing.T) {
	// "0" is a present, valid required value.
	p := parseForm(t, url.Values{"Price": {"0"}},
		Field{Name: "Price", Kind: Money, Required: true})
	if !p.OK() || p.Cents("Price") != 0 {
		t.Fatalf("required Price=0: OK=%v cents=%d errs=%v", p.OK(), p.Cents("Price"), p.Errors())
	}

	// Empty required reports the required message, not a parse error.
	p = parseForm(t, url.Values{},
		Field{Name: "Price", Kind: Money, Required: true})
	if got := p.Errors()["Price"]; got != "Price is required" {
		t.Errorf("empty required Money error = %q", got)
	}

	// Present-but-invalid reports ParseCents' own error and echoes raw.
	p = parseForm(t, url.Values{"Price": {"12.345"}},
		Field{Name: "Price", Kind: Money, Required: true})
	if p.OK() {
		t.Fatal("three decimal places passed")
	}
	if got := p.Errors()["Price"]; strings.Contains(got, "required") {
		t.Errorf("invalid Money reported the required message: %q", got)
	}
	if p.Echo()["Price"] != "12.345" {
		t.Errorf("Money echo = %q, want the raw text", p.Echo()["Price"])
	}

	// Empty optional Money is 0, no error.
	p = parseForm(t, url.Values{}, Field{Name: "Price", Kind: Money})
	if !p.OK() || p.Cents("Price") != 0 {
		t.Errorf("optional empty Money: OK=%v cents=%d", p.OK(), p.Cents("Price"))
	}

	p = parseForm(t, url.Values{"Price": {"12.34"}}, Field{Name: "Price", Kind: Money})
	if p.Cents("Price") != 1234 {
		t.Errorf("Cents = %d, want 1234", p.Cents("Price"))
	}
}

// Humanize must match the generator's own titleCase — the required
// messages in generated actions and in hand-written Parse calls have
// to read identically.
func TestHumanize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Title", "Title"},
		{"MaxPerOrder", "Max per order"},
		{"ticket_types", "Ticket types"},
		{"notes", "Notes"},
	}
	for _, c := range cases {
		if got := Humanize(c.in); got != c.want {
			t.Errorf("Humanize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
