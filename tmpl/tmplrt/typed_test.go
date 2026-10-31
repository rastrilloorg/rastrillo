package tmplrt_test

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// A named integer and a named string with no methods print as their
// underlying values; fancy has a String method, which they do not.
type (
	id    int16
	bare  string
	fancy int
)

func (fancy) String() string { return "fancy" }

// integers is every integer type at its edges, as an any.
func integers() []any {
	return []any{
		0, 1, -1, 42, math.MaxInt, math.MinInt,
		int8(math.MinInt8), int8(math.MaxInt8), int16(math.MinInt16),
		int32(math.MinInt32), int64(math.MinInt64), int64(math.MaxInt64),
		uint(0), uint(math.MaxUint), uint8(255), uint16(math.MaxUint16),
		uint32(math.MaxUint32), uint64(math.MaxUint64), uintptr(7),
		id(-12), id(math.MaxInt16),
	}
}

// decimal is what WriteInt or WriteUint writes for integer.
func decimal(t *testing.T, integer any) string {
	t.Helper()
	var buffer bytes.Buffer
	buffer.WriteString("pre:")
	value := reflect.ValueOf(integer)
	if value.CanInt() {
		tmplrt.WriteInt(&buffer, value.Int())
		got, want := tmplrt.IntString(value.Int()), buffer.String()[4:]
		if got != want {
			t.Errorf("IntString(%v) = %q, WriteInt wrote %q",
				integer, got, want)
		}
	} else {
		tmplrt.WriteUint(&buffer, value.Uint())
		got, want := tmplrt.UintString(value.Uint()), buffer.String()[4:]
		if got != want {
			t.Errorf("UintString(%v) = %q, WriteUint wrote %q",
				integer, got, want)
		}
	}
	return buffer.String()[4:]
}

// TestDecimalEscapers holds each escaper DecimalEscapers names to leaving
// an integer's digits as they are, which is what lets the compiler write
// them directly.
func TestDecimalEscapers(t *testing.T) {
	for _, name := range tmplrt.DecimalEscapers {
		fn, ok := tmplrt.Escapers[name].(func(...any) string)
		if !ok {
			t.Fatalf("%s is not an escaper", name)
		}
		for _, integer := range integers() {
			if got, want := fn(integer), decimal(t, integer); got != want {
				t.Errorf("%s(%T %v) = %q, want %q",
					name, integer, integer, got, want)
			}
		}
		// The compiler leaves a type with a String method to the escaper.
		if got := fn(fancy(3)); got == "3" {
			t.Errorf("%s(fancy(3)) = %q, the digits: "+
				"the test type does not test that", name, got)
		}
	}
}

func FuzzDecimalEscapers(f *testing.F) {
	f.Add(int64(0), uint64(0))
	f.Add(int64(math.MinInt64), uint64(math.MaxUint64))
	f.Fuzz(func(t *testing.T, signed int64, unsigned uint64) {
		for _, name := range tmplrt.DecimalEscapers {
			fn := tmplrt.Escapers[name].(func(...any) string)
			got, want := fn(signed), strconv.FormatInt(signed, 10)
			if got != want {
				t.Fatalf("%s(%d) = %q", name, signed, got)
			}
			got, want = fn(unsigned), strconv.FormatUint(unsigned, 10)
			if got != want {
				t.Fatalf("%s(%d) = %q", name, unsigned, got)
			}
		}
	})
}

// TestNamedStringsEscapeAsStrings: a named string type that is not one of
// html/template's content types and has no String, Error or Format
// method is escaped as its string is, by every escaper, so the compiler
// converts it and takes the string's fast path.
func TestNamedStringsEscapeAsStrings(t *testing.T) {
	for name, fn := range tmplrt.Escapers {
		fn := fn.(func(...any) string)
		for _, text := range fastCorpus {
			if got, want := fn(bare(text)), fn(text); got != want {
				t.Errorf("%s(bare(%q)) = %q, %s(%q) = %q",
					name, text, got, name, text, want)
			}
		}
	}
}

// TestSortedMapMatchesRange holds SortedMap to the order Range visits a
// map in, for string and integer keys of their own types.
func TestSortedMapMatchesRange(t *testing.T) {
	check := func(mapping any, sorted any) {
		t.Helper()
		var want []string
		visit := func(key, value reflect.Value) error {
			want = append(want,
				fmt.Sprint(key.Interface(), "=", value.Interface()))
			return nil
		}
		err := tmplrt.Range(reflect.ValueOf(mapping), visit)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		entries := reflect.ValueOf(sorted)
		for index := range entries.Len() {
			entry := entries.Index(index)
			got = append(got, fmt.Sprint(entry.Field(0).Interface(), "=",
				entry.Field(1).Interface()))
		}
		if !slices.Equal(got, want) {
			t.Errorf("SortedMap(%v) = %v, Range visits %v", mapping, got, want)
		}
	}
	strs := map[bare]int{"b": 2, "a": 1, "": 0, "B": 3, "ä": 4}
	check(strs, tmplrt.SortedMap(strs))
	ints := map[id]string{3: "c", -1: "m", 0: "z", math.MinInt16: "min"}
	check(ints, tmplrt.SortedMap(ints))
	uints := map[uint8]bool{255: true, 0: false, 7: true}
	check(uints, tmplrt.SortedMap(uints))
	var empty map[string]int
	check(empty, tmplrt.SortedMap(empty))
}

func FuzzNamedStringsEscapeAsStrings(f *testing.F) {
	for _, text := range fastCorpus {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for name, fn := range tmplrt.Escapers {
			fn := fn.(func(...any) string)
			if got, want := fn(bare(text)), fn(text); got != want {
				t.Fatalf("%s(bare(%q)) = %q, %s(%q) = %q",
					name, text, got, name, text, want)
			}
		}
	})
}
