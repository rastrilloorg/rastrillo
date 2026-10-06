package codeview

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrUnwritable is a value a call cannot show truthfully. A struct is
// one on purpose: a struct caller has different missing-field semantics
// from a map caller, so writing it as a dict would show a call the
// reader did not write. A value like that is the app's own, and a
// placeholder bound to it says so.
var ErrUnwritable = errors.New("bind it to a placeholder or give it as a map")

// lineLimit is the width, in characters rather than UTF-8 bytes, under
// which a call stays on one line: counted in bytes, a call carrying
// Bengali or Japanese copy would break at a third of the width a reader
// sees.
const lineLimit = 80

// Call writes the {{template}} action that renders partial with data,
// and the dot that action must run against. A map's keys are written
// in order's order first, then the rest alphabetically, so the call is
// the same on every render. A key in bind is written as the placeholder
// .Field it names, and dot carries the value under that field.
//
// A call that fits in 80 columns is one line. A longer one breaks after
// dict, one key and value per line, four spaces in, and a list of dicts
// puts each dict on a line of its own four spaces further in. Go
// templates allow newlines inside an action, so every layout parses.
func Call(partial string, data any, order []string, bind map[string]string) (string, map[string]any, error) {
	w := &callWriter{bind: bind, dot: map[string]any{}}
	head := "{{template " + strconv.Quote(partial)
	v := reflect.ValueOf(data)
	if s, ok := scalar(v); ok {
		return head + " " + s + "}}", w.dot, nil
	}
	if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
		return "", nil, fmt.Errorf("the data is %s: %w", describe(v), ErrUnwritable)
	}
	keys := keysOf(v, order)
	one := []string{head + " dict"}
	long := []string{head + " dict"}
	for _, k := range keys {
		short, err := w.value(k, v.MapIndex(reflect.ValueOf(k)), "", true)
		if err != nil {
			return "", nil, err
		}
		wide, err := w.value(k, v.MapIndex(reflect.ValueOf(k)), "    ", false)
		if err != nil {
			return "", nil, err
		}
		one = append(one, strconv.Quote(k)+" "+short)
		long = append(long, "    "+strconv.Quote(k)+" "+wide)
	}
	if line := strings.Join(one, " ") + "}}"; utf8.RuneCountInString(line) <= lineLimit && !strings.Contains(line, "\n") {
		return line, w.dot, nil
	}
	return strings.Join(long, "\n") + "}}", w.dot, nil
}

type callWriter struct {
	bind map[string]string
	dot  map[string]any
}

// value writes one argument. path is the top-level key it came from,
// which is the only level a placeholder can bind; indent is the indent
// of the line it starts on; oneLine forbids the per-dict lines a long
// call gives a list of dicts.
func (w *callWriter) value(path string, v reflect.Value, indent string, oneLine bool) (string, error) {
	if field, ok := w.bind[path]; ok && indent != "nested" {
		w.dot[field] = v.Interface()
		return "." + field, nil
	}
	for v.IsValid() && v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if s, ok := scalar(v); ok {
		return s, nil
	}
	if !v.IsValid() {
		return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
	}
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
		}
		parts := []string{"(dict"}
		for _, k := range keysOf(v, nil) {
			s, err := w.value(path, v.MapIndex(reflect.ValueOf(k)), "nested", true)
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Quote(k), s)
		}
		return strings.Join(parts, " ") + ")", nil
	case reflect.Slice, reflect.Array:
		items := make([]string, 0, v.Len())
		dicts := false
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			for item.Kind() == reflect.Interface {
				item = item.Elem()
			}
			dicts = dicts || item.Kind() == reflect.Map
			s, err := w.value(path, item, "nested", true)
			if err != nil {
				return "", err
			}
			items = append(items, s)
		}
		if oneLine || !dicts {
			return strings.TrimSpace("(list "+strings.Join(items, " ")) + ")", nil
		}
		inner := indent + "    "
		return "(list\n" + inner + strings.Join(items, "\n"+inner) + ")", nil
	}
	return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
}

// scalar writes a value a template can spell as a literal: a string,
// any integer, a finite float64, a bool. Each must be of the predeclared
// type, not a named one: the call hands the partial the plain value, so
// template.HTML would arrive as a string and be escaped, and a named int
// with a String method would print as its number.
//
// A float is written so the template reads it back as float64: an
// integral one gets ".0", since "3" would arrive as the int 3, and 'g'
// formatting keeps a large one in exponent form, since a literal past
// 2^64 written out in full does not parse. NaN and the infinities have
// no literal at all.
func scalar(v reflect.Value) (string, bool) {
	if !v.IsValid() || v.Type().PkgPath() != "" {
		return "", false
	}
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", false
		}
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s, true
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	}
	return "", false
}

// keysOf is a string-keyed map's keys: those order names first, in its
// order, then the rest alphabetically.
func keysOf(v reflect.Value, order []string) []string {
	have := map[string]bool{}
	for _, k := range v.MapKeys() {
		have[k.String()] = true
	}
	out := make([]string, 0, len(have))
	for _, k := range order {
		if have[k] {
			out = append(out, k)
			delete(have, k)
		}
	}
	rest := make([]string, 0, len(have))
	for k := range have {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func describe(v reflect.Value) string {
	if !v.IsValid() {
		return "nil"
	}
	return "a " + v.Type().String()
}
