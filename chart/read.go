package chart

import (
	"fmt"
	"reflect"
)

// A reader takes a partial's data apart, key by key, keeping the first
// problem: a dict built in a template, or an app's own struct with
// fields of the same names.
type reader struct {
	data any
	err  error
}

// field is key's value in data: a map's entry or a struct's exported
// field, through any pointers.
func field(data any, key string) (any, bool) {
	held := reflect.ValueOf(data)
	for held.IsValid() && (held.Kind() == reflect.Pointer ||
		held.Kind() == reflect.Interface) {
		held = held.Elem()
	}
	switch {
	case !held.IsValid():
		return nil, false
	case held.Kind() == reflect.Map &&
		held.Type().Key().Kind() == reflect.String:
		value := held.MapIndex(reflect.ValueOf(key).Convert(
			held.Type().Key()))
		if !value.IsValid() {
			return nil, false
		}
		return value.Interface(), true
	case held.Kind() == reflect.Struct:
		value := held.FieldByName(key)
		if !value.IsValid() || !value.CanInterface() {
			return nil, false
		}
		return value.Interface(), true
	}
	return nil, false
}

// typed is the Chart a partial was given, when it is a T or a *T.
func typed[T any](data any) (T, bool) {
	value, _ := field(data, "Chart")
	switch chart := value.(type) {
	case T:
		return chart, true
	case *T:
		if chart != nil {
			return *chart, true
		}
	}
	var zero T
	return zero, false
}

func (reading *reader) fail(format string, args ...any) {
	if reading.err == nil {
		reading.err = fmt.Errorf(format, args...)
	}
}

const wrongTypeMsg = "%s is %T; want %s"

func (reading *reader) text(key string) string {
	value, _ := field(reading.data, key)
	switch text := value.(type) {
	case nil:
		return ""
	case string:
		return text
	case fmt.Stringer:
		return text.String()
	}
	reading.fail(wrongTypeMsg, key, value, "a string")
	return ""
}

func (reading *reader) flag(key string) bool {
	value, _ := field(reading.data, key)
	switch flag := value.(type) {
	case nil:
		return false
	case bool:
		return flag
	}
	reading.fail(wrongTypeMsg, key, value, "true or false")
	return false
}

// bound reads an optional figure: absent or nil is no bound, and any
// number, 0 included, is one.
func (reading *reader) bound(key string) Bound {
	value, _ := field(reading.data, key)
	if bound, ok := value.(Bound); ok {
		return bound
	}
	figure, set, err := decimalOf(value)
	if err != nil {
		reading.fail("%s: %v", key, err)
	}
	return Bound{Value: figure, Set: set}
}

// rounding reads Places, the digits to round to: absent is no rounding.
func (reading *reader) rounding() Rounding {
	value, present := field(reading.data, "Places")
	if rounding, ok := value.(Rounding); ok {
		return rounding
	}
	held := reflect.ValueOf(value)
	switch {
	case !present || value == nil:
		return Rounding{}
	case held.CanInt():
		return Rounding{Places: int(held.Int()), Set: true}
	case held.CanUint():
		return Rounding{Places: int(min(held.Uint(), 99)), Set: true}
	}
	reading.fail(wrongTypeMsg, "Places", value, "a count of digits")
	return Rounding{}
}

// points reads Points: []Point, or a list of items with Label, Value
// and Missing, as dict builds them or an app's own structs carry them.
func (reading *reader) points() []Point {
	value, _ := field(reading.data, "Points")
	if points, ok := value.([]Point); ok {
		return points
	}
	list := reflect.ValueOf(value)
	if value == nil {
		return nil
	}
	if list.Kind() != reflect.Slice && list.Kind() != reflect.Array {
		reading.fail(wrongTypeMsg, "Points", value, "a list of points")
		return nil
	}
	var points []Point
	for index := range list.Len() {
		points = append(points, reading.point(index, list.Index(index)))
	}
	return points
}

func (reading *reader) point(index int, item reflect.Value) Point {
	if point, ok := item.Interface().(Point); ok {
		return point
	}
	inner := reader{data: item.Interface()}
	point := Point{Label: inner.label(), Missing: inner.flag("Missing")}
	value, _ := field(inner.data, "Value")
	figure, set, err := decimalOf(value)
	point.Value, point.Missing = figure, point.Missing || !set
	if inner.err == nil {
		inner.err = err
	}
	if inner.err != nil {
		reading.fail("point %d: %v", index+1, inner.err)
	}
	return point
}

// label reads a point's Label: words, or a number written as it is,
// as a week or a year often is.
func (reading *reader) label() string {
	value, _ := field(reading.data, "Label")
	held := reflect.ValueOf(value)
	if held.IsValid() && (held.CanInt() || held.CanUint()) {
		return fmt.Sprint(value)
	}
	return reading.text("Label")
}
