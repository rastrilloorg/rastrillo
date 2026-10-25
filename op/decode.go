package op

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// The English a refused member carries, in Invalid.Fields under the
// member's name as the client sent it. A refusal of the whole value
// has no member to name, so it carries no message here: each door says
// what to send instead, in its own terms.
const (
	msgUnknown  = "Unknown field. Check the spelling and case."
	msgNull     = "Send a value, or leave this field out."
	msgOnce     = "Send this field once."
	msgNested   = "Check the values inside this field."
	msgNoAmount = "Send an amount."
	msgString   = "Send a string."
	msgInteger  = "Send a whole number."
	msgNumber   = "Send a number."
	msgBool     = "Send true or false."
	msgObject   = "Send an object."
	msgArray    = "Send an array."
	msgValue    = "Check this value."
)

// DecodeInput decodes one JSON object of arguments into dst, a non-nil
// pointer to a struct, with the rules every door shares, for a caller
// that has no HTTP request: Descriptor.Invoke, and api.Decode once it
// has read the body. One implementation, so the API door and Invoke
// cannot drift on what a member may be.
//
// It refuses with an Invalid. One keyed by the member's name, as sent,
// for a member that is unknown (names match case-sensitively), sent
// twice, the wrong type, or null; a failure deeper inside a member is
// keyed by that member, because fields names the client's fields, not
// paths into them. One with no Fields when the value is not exactly
// one JSON object: bad syntax, another kind of value, or anything after
// it. A door answers that one with its own sentence about what to send.
//
// null is refused everywhere except for a member whose destination has
// Nullable's IsNullable marker. That is the one place null means
// something (clearing optional money); anywhere else json would quietly
// turn it into a zero, and a client that sent null for an amount would
// set it to 0 without being told.
func DecodeInput(data []byte, dst any) error {
	if v := reflect.ValueOf(dst); v.Kind() != reflect.Pointer || v.IsNil() {
		// A caller's bug, not a client error: a 422 would blame the client.
		panic(fmt.Sprintf("op: DecodeInput needs a non-nil pointer, got %T", dst))
	}
	// The whole value is checked as JSON before any of it is decoded
	// into dst. Decoding streams, so without this a body broken near
	// its end would be reported as a wrong type for whichever field
	// came first. Unmarshal into a Value also refuses duplicate names
	// and trailing data, which json v2 does by default.
	var whole jsontext.Value
	if err := json.Unmarshal(data, &whole); err != nil {
		return refuse(err)
	}
	if whole.Kind() != '{' {
		return Invalid{}
	}
	if err := json.Unmarshal(data, dst, json.RejectUnknownMembers(true), refuseNull); err != nil {
		return refuse(err)
	}
	return nil
}

// nullable is Nullable's marker. DecodeInput asks the type rather than
// listing Nullable instantiations, which it cannot name for every T.
type nullable interface{ IsNullable() bool }

// refuseNull runs before every value json decodes, at every depth,
// including inside a type's own UnmarshalJSONFrom (Opt, Nullable and
// Money pass the decoder on), so the destination type it sees is the
// one json itself resolved: tags, embedded structs and all. Checking
// the raw object's top-level members instead would need a copy of
// json's name rules, and would miss a null nested inside a value.
var refuseNull = json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v any) error {
	if dec.PeekKind() != 'n' {
		return json.SkipFunc
	}
	if n, ok := v.(nullable); ok && n.IsNullable() {
		return json.SkipFunc
	}
	return errNull
}))

// refuse turns a decode error into the Invalid it earns. A failure
// json can pin to a member is keyed by that member's name as the
// client sent it; one deeper down is keyed by the top-level member
// that holds it. Anything else is the value as a whole.
func refuse(err error) error {
	var (
		syn *jsontext.SyntacticError
		sem *json.SemanticError
	)
	switch {
	case errors.As(err, &syn):
		// Duplicates are the one syntax error about a member rather than
		// about the text, so they are the one that names it.
		if errors.Is(syn.Err, jsontext.ErrDuplicateName) {
			if name, nested, ok := member(syn.JSONPointer); ok {
				return fieldInvalid(name, nested, msgOnce)
			}
		}
		return Invalid{}
	case errors.As(err, &sem):
		name, nested, ok := member(sem.JSONPointer)
		if !ok {
			// The value is an object, so a failure at the root means dst is
			// not something an object decodes into. Nothing to point at.
			return Invalid{}
		}
		return fieldInvalid(name, nested, semanticMessage(sem))
	default:
		return Invalid{}
	}
}

func fieldInvalid(name string, nested bool, message string) error {
	if nested {
		message = msgNested
	}
	return Invalid{Fields: map[string]string{name: message}}
}

// member is the first token of p, unescaped, and whether p goes deeper.
func member(p jsontext.Pointer) (name string, nested, ok bool) {
	for tok := range p.Tokens() {
		if !ok {
			name, ok = tok, true
			continue
		}
		return name, true, true
	}
	return name, false, ok
}

var (
	unmarshalerFrom = reflect.TypeFor[json.UnmarshalerFrom]()
	unmarshaler     = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// semanticMessage says what to send instead, when the Go type makes
// that plain. A type that decodes itself (time.Time is a struct but
// reads a string) gets the generic message, because its Go kind says
// nothing about its JSON shape.
func semanticMessage(sem *json.SemanticError) string {
	switch {
	case errors.Is(sem.Err, errNull):
		return msgNull
	case errors.Is(sem.Err, errNoAmount):
		return msgNoAmount
	case errors.Is(sem.Err, json.ErrUnknownName):
		return msgUnknown
	}
	t := sem.GoType
	if t == nil {
		return msgValue
	}
	if p := reflect.PointerTo(t); p.Implements(unmarshalerFrom) || p.Implements(unmarshaler) {
		return msgValue
	}
	if reflect.PointerTo(t).Implements(textUnmarshaler) {
		return msgString
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		// A number that is fractional or out of range fails strconv, not
		// a kind check; it still wants a whole number.
		if sem.Err == nil || errors.Is(sem.Err, strconv.ErrSyntax) || errors.Is(sem.Err, strconv.ErrRange) {
			return msgInteger
		}
		return msgValue
	}
	// The rest are pure kind mismatches only when json reports no
	// underlying error; with one, the shape was right and the value was not.
	if sem.Err != nil {
		return msgValue
	}
	switch t.Kind() {
	case reflect.String:
		return msgString
	case reflect.Float32, reflect.Float64:
		return msgNumber
	case reflect.Bool:
		return msgBool
	case reflect.Struct, reflect.Map:
		return msgObject
	case reflect.Slice, reflect.Array:
		// json writes []byte as a base64 string, so that is what it reads.
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return msgString
		}
		return msgArray
	}
	return msgValue
}
