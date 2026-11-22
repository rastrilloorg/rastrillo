package op

import (
	"encoding"
	encodingjson "encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// direction is which side of the codec a schema describes. The sides
// differ: DecodeInput refuses null where the encoder writes it for a nil
// pointer, and an unset Opt, which DecodeInput fills, encodes as null.
type direction int

const (
	input direction = iota
	output
)

// schemaFor is the JSON Schema of goType as the codec reads it in one
// direction: DecodeInput for input, api.Encode for output. It reads the
// json tags the codec reads, and refuses a type the two would disagree
// on, so an agent is never told a shape the doors refuse or never write.
func schemaFor(
	goType reflect.Type, way direction,
) (encodingjson.RawMessage, error) {
	walk := walk{way: way, path: map[reflect.Type]bool{}}
	described, err := walk.describe(goType,
		place{owner: typeName(goType), declared: goType})
	if err != nil {
		return nil, err
	}
	return json.Marshal(described, json.Deterministic(true))
}

// schema is one JSON Schema object with its members in the order they
// are written, so a client sees a type's fields in the order Go declares
// them.
type schema []entry

type entry struct {
	name  string
	value any
}

func (described schema) MarshalJSONTo(encoder *jsontext.Encoder) error {
	if err := encoder.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, each := range described {
		err := encoder.WriteToken(jsontext.String(each.name))
		if err != nil {
			return err
		}
		if err := json.MarshalEncode(encoder, each.value); err != nil {
			return err
		}
	}
	return encoder.WriteToken(jsontext.EndObject)
}

func typed(name string) schema { return schema{{"type", name}} }

// fixed holds the types the codec reads by their own rules rather than
// by their Go kind.
var fixed = map[reflect.Type]schema{
	reflect.TypeFor[time.Time](): {
		{"type", "string"}, {"format", "date-time"}},
	// Money's decoder requires amount and lets currency be left out.
	reflect.TypeFor[Money](): {
		{"type", "object"},
		{"properties", schema{
			{"amount", typed("integer")}, {"currency", typed("string")}}},
		{"required", []string{"amount"}},
		{"additionalProperties", false}},
	reflect.TypeFor[None]():                    typed("null"),
	reflect.TypeFor[encodingjson.RawMessage](): {},
	reflect.TypeFor[jsontext.Value]():          {},
}

var (
	durationType = reflect.TypeFor[time.Duration]()
	numberType   = reflect.TypeFor[encodingjson.Number]()
	optionalType = reflect.TypeFor[interface{ optional() }]()
	nullableType = reflect.TypeFor[nullable]()
)

// ownJSON and ownText are the methods by which a type reads or writes
// itself, per direction. Its Go kind then says nothing about its JSON,
// so a type with its own JSON methods is described as anything, and one
// with text methods as the string json makes of them.
var (
	ownJSON = [...][]reflect.Type{
		input: {
			reflect.TypeFor[json.UnmarshalerFrom](),
			reflect.TypeFor[json.Unmarshaler]()},
		output: {
			reflect.TypeFor[json.MarshalerTo](),
			reflect.TypeFor[json.Marshaler]()},
	}
	ownText = [...][]reflect.Type{
		input: {reflect.TypeFor[encoding.TextUnmarshaler]()},
		output: {
			reflect.TypeFor[encoding.TextAppender](),
			reflect.TypeFor[encoding.TextMarshaler]()},
	}
)

type walk struct {
	way direction
	// path holds the types being described, outermost first, so a type
	// met again inside itself is a cycle rather than endless recursion.
	path map[reflect.Type]bool
}

// place is the field a refusal names.
type place struct {
	owner, field string
	declared     reflect.Type
}

func (at place) unsupported() error {
	return fmt.Errorf(msgUnsupported, at.owner, at.field, at.declared)
}

func (walk *walk) describe(goType reflect.Type, at place) (schema, error) {
	if known, ok := fixed[goType]; ok {
		return known, nil
	}
	switch {
	case goType == numberType && walk.way == output:
		// api.Encode writes a Number as the number it holds.
		return typed("number"), nil
	case goType == durationType:
		// json v2 has no default for a Duration, and refuses one.
		return nil, at.unsupported()
	case goType.Kind() == reflect.Pointer:
		return walk.pointer(goType, at)
	case goType.Kind() == reflect.Interface:
		if goType.NumMethod() > 0 {
			return nil, at.unsupported()
		}
		return schema{}, nil
	case presence(goType):
		return walk.present(goType, at)
	case implementsAny(goType, ownJSON[walk.way]):
		return schema{}, nil
	case implementsAny(goType, ownText[walk.way]):
		return typed("string"), nil
	}
	if walk.path[goType] {
		return nil, fmt.Errorf(msgCycle, goType)
	}
	walk.path[goType] = true
	defer delete(walk.path, goType)
	return walk.byKind(goType, at)
}

func (walk *walk) byKind(goType reflect.Type, at place) (schema, error) {
	switch goType.Kind() {
	case reflect.String:
		return typed("string"), nil
	case reflect.Bool:
		return typed("boolean"), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return typed("integer"), nil
	case reflect.Float32, reflect.Float64:
		return typed("number"), nil
	case reflect.Slice, reflect.Array:
		return walk.list(goType, at)
	case reflect.Map:
		return walk.dictionary(goType, at)
	case reflect.Struct:
		return walk.object(goType)
	}
	return nil, at.unsupported()
}

// pointer is refused in an input because DecodeInput refuses null for
// it, so the only reason to use one there is a mistake.
func (walk *walk) pointer(goType reflect.Type, at place) (schema, error) {
	if walk.way == input {
		return nil, fmt.Errorf(msgPointerInput, at.owner, at.field)
	}
	described, err := walk.describe(goType.Elem(), at)
	if err != nil {
		return nil, err
	}
	return withNull(described), nil
}

// present describes an Opt or a Nullable. They are refused in a result
// because an unset one encodes as null, which its schema would not say.
func (walk *walk) present(goType reflect.Type, at place) (schema, error) {
	if walk.way == output {
		return nil, fmt.Errorf(msgOptResult, at.owner, at.field)
	}
	value, _ := goType.FieldByName("Value")
	described, err := walk.describe(value.Type, at)
	if err != nil || !goType.Implements(nullableType) {
		return described, err
	}
	return withNull(described), nil
}

// presence reports an Opt or a Nullable by their markers, as
// DecodeInput does, because neither can be named for every T.
func presence(goType reflect.Type) bool {
	if goType.Kind() != reflect.Struct {
		return false
	}
	if _, ok := goType.FieldByName("Value"); !ok {
		return false
	}
	return goType.Implements(optionalType) ||
		goType.Implements(nullableType)
}

// list is base64 for plain bytes, which json writes as one string. A
// slice of a named byte type is an array of numbers, as json writes it.
func (walk *walk) list(goType reflect.Type, at place) (schema, error) {
	element := goType.Elem()
	if element.Kind() == reflect.Uint8 && element.PkgPath() == "" {
		return schema{
			{"type", "string"}, {"contentEncoding", "base64"}}, nil
	}
	items, err := walk.describe(element, at)
	if err != nil {
		return nil, err
	}
	return schema{{"type", "array"}, {"items", items}}, nil
}

func (walk *walk) dictionary(goType reflect.Type, at place) (schema, error) {
	if goType.Key().Kind() != reflect.String {
		return nil, at.unsupported()
	}
	values, err := walk.describe(goType.Elem(), at)
	if err != nil {
		return nil, err
	}
	return schema{
		{"type", "object"}, {"additionalProperties", values}}, nil
}

// withNull widens a schema's type to admit null. A schema with no type
// admits null already.
func withNull(described schema) schema {
	index := slices.IndexFunc(described, func(each entry) bool {
		return each.name == "type"
	})
	if index < 0 {
		return described
	}
	widened := slices.Clone(described)
	switch kind := described[index].value.(type) {
	case string:
		if kind != "null" {
			widened[index].value = []string{kind, "null"}
		}
	case []string:
		if !slices.Contains(kind, "null") {
			widened[index].value = append(slices.Clone(kind), "null")
		}
	}
	return widened
}

func implementsAny(goType reflect.Type, interfaces []reflect.Type) bool {
	pointer := reflect.PointerTo(goType)
	for _, each := range interfaces {
		if goType.Implements(each) || pointer.Implements(each) {
			return true
		}
	}
	return false
}

// typeName is how a refusal names a struct: by its Go name, or by its
// literal when it has none.
func typeName(goType reflect.Type) string {
	if goType.Name() != "" {
		return goType.Name()
	}
	return goType.String()
}
