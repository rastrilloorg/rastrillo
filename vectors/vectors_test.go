package vectors

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// The file must read in the order the rules were written, and
// regenerate byte-identically — Add order is the order, always.
func TestWriteToKeepsAddOrder(t *testing.T) {
	s := New()
	s.Add("zebra", "added first, stays first", map[string]any{"n": 1})
	s.Add("aardvark", "added second, stays second", map[string]any{"n": 2})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Index(out, `"zebra"`) > strings.Index(out, `"aardvark"`) {
		t.Errorf("cases were reordered:\n%s", out)
	}
}

// The exact wire shape: a JSON array, 2-space indent, trailing
// newline, name and why leading each object, then the fields sorted.
func TestWriteToShape(t *testing.T) {
	s := New()
	s.Add("one", "shape", map[string]any{"n": 1})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\n    \"name\": \"one\",\n    \"why\": \"shape\",\n    \"n\": 1\n  }\n]\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteTo = %q, want %q", got, want)
	}
}

func TestWriteToEmptySet(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New().WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "[]\n" {
		t.Errorf("empty Set = %q, want %q", got, "[]\n")
	}
}

// The library's whole normalisation: nil slices and maps sitting
// DIRECTLY in fields become []/{}, never null — "no bests has to look
// the same on both sides".
func TestWriteToNormalisesTopLevelNils(t *testing.T) {
	var nilSlice []int
	var nilMap map[string]int
	s := New()
	s.Add("empty", "nil collections read as empty on both sides", map[string]any{
		"log":   nilSlice,
		"index": nilMap,
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"log": []`) {
		t.Errorf("nil slice should write as []:\n%s", out)
	}
	if !strings.Contains(out, `"index": {}`) {
		t.Errorf("nil map should write as {}:\n%s", out)
	}
	if strings.Contains(out, "null") {
		t.Errorf("a top-level nil leaked through as null:\n%s", out)
	}
}

// The boundary of that normalisation, pinned on purpose: inside an
// app-typed value the library never walks (json tags, omitempty,
// custom marshalers make a generic deep walk a lie), so an inner nil
// slice stays null — the app's own discipline, exactly as kass
// normalises its two output fields by hand.
func TestWriteToDoesNotWalkInnerShapes(t *testing.T) {
	type inner struct {
		Log []int `json:"log"`
	}
	s := New()
	s.Add("inner", "inner shapes are the app's discipline", map[string]any{
		"wrapped": inner{},
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"log": null`) {
		t.Errorf("inner nils must NOT be normalised — that is the documented boundary:\n%s", buf.String())
	}
}

// Two identical Sets write identical bytes: field keys sorted, inner
// map keys sorted (encoding/json's own ordering), envelope first.
func TestWriteToIsDeterministic(t *testing.T) {
	build := func() *Set {
		s := New()
		s.Add("m", "map keys must not wobble between runs", map[string]any{
			"zed":   1,
			"alpha": 2,
			"tally": map[string]int{"b": 2, "a": 1, "c": 3},
		})
		return s
	}
	var one, two bytes.Buffer
	if _, err := build().WriteTo(&one); err != nil {
		t.Fatal(err)
	}
	if _, err := build().WriteTo(&two); err != nil {
		t.Fatal(err)
	}
	if one.String() != two.String() {
		t.Error("two identical Sets wrote different bytes")
	}
	out := one.String()
	if strings.Index(out, `"alpha"`) > strings.Index(out, `"zed"`) {
		t.Errorf("field keys are not sorted:\n%s", out)
	}
	if strings.Index(out, `"name"`) > strings.Index(out, `"alpha"`) {
		t.Errorf("name/why must lead each object:\n%s", out)
	}
	if strings.Index(out, `"a": 1`) > strings.Index(out, `"b": 2`) {
		t.Errorf("inner map keys are not sorted:\n%s", out)
	}
}

// The time half of the treaty: a time.Time round-trips via RFC 3339,
// so the JS side can new Date(v.now) it.
func TestWriteToTimeIsRFC3339(t *testing.T) {
	s := New()
	s.Add("clock", "times ride as RFC 3339", map[string]any{
		"now": time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"now": "2026-01-02T15:04:05Z"`) {
		t.Errorf("time did not round-trip as RFC 3339:\n%s", buf.String())
	}
}

// name and why are the vector's envelope; a field by either name
// would duplicate a JSON key, which no parser reads back honestly.
func TestWriteToRejectsReservedFieldKeys(t *testing.T) {
	s := New()
	s.Add("x", "the envelope owns name and why", map[string]any{"why": "no"})
	if _, err := s.WriteTo(io.Discard); err == nil {
		t.Fatal("a field named why must be refused, not written as a duplicate key")
	}
}
