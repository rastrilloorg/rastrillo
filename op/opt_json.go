package op

import (
	"errors"

	json "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// errNull is what a client sees for null in a field that cannot take it.
// json wraps it in a SemanticError that carries the member's pointer, so
// the API door can put the message on the right field.
var errNull = errors.New("null is not allowed")

// Opt is a field a request may or may not carry. Set is true when the
// member was present, whatever its value, so an explicit zero is Set
// and an absent member is not.
type Opt[T any] struct {
	Value T
	Set   bool
}

// Some is a present Opt.
func Some[T any](v T) Opt[T] { return Opt[T]{Value: v, Set: true} }

// UnmarshalJSONFrom decodes a present member. null is refused: no
// manifest field is nullable, so a null is a client bug and saying so
// beats silently treating it as absent or as zero. Optional money, the
// one place null means something, uses Nullable.
//
// Each decode starts from zero and replaces the whole Opt only on
// success. Decoding into the existing Value would merge: json keeps the
// members of a struct the new document leaves out, so a reused
// destination could report a currency the client never sent. And a
// failed decode must not leave Set from an earlier one, or a refused
// request looks like a sent field.
func (o *Opt[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	*o = Opt[T]{}
	if dec.PeekKind() == 'n' {
		return errNull
	}
	var v T
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err
	}
	*o = Opt[T]{Value: v, Set: true}
	return nil
}

// MarshalJSONTo writes the value, or null when unset. Doors never emit
// an unset Opt, because result types do not use Opt; this exists so a
// test or a log can round-trip an input.
func (o Opt[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !o.Set {
		return enc.WriteToken(jsontext.Null)
	}
	return json.MarshalEncode(enc, o.Value)
}

// Nullable is Opt plus a Null flag. A JSON null sets both Set and
// Null: the member was sent, and it asks to clear the field. Only
// optional money fields use it, because clearing is a legitimate change
// there and nowhere else; Opt still refuses null.
type Nullable[T any] struct {
	Value T
	Set   bool
	Null  bool
}

// UnmarshalJSONFrom decodes a present member, marking null as Null
// rather than refusing it.
//
// It resets first for the same reason Opt does, and one more: a null
// after a value must drop the value, and a value after a null must drop
// Null, or a stale flag turns a supplied amount into a clear.
func (n *Nullable[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	*n = Nullable[T]{}
	if dec.PeekKind() == 'n' {
		// The token has to be consumed: json checks that exactly one
		// value was read, and refuses the decode otherwise.
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		*n = Nullable[T]{Set: true, Null: true}
		return nil
	}
	var v T
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err
	}
	*n = Nullable[T]{Value: v, Set: true}
	return nil
}

// IsNullable is the marker api.Decode reads: it refuses null for every
// member except one whose destination has this method and says true.
// Decode cannot name Nullable[T] for every T it might meet, and a list
// of instantiations would refuse the next money field someone adds, so
// it asks the type instead.
func (Nullable[T]) IsNullable() bool { return true }

// MarshalJSONTo writes the value, or null when Null or unset.
func (n Nullable[T]) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !n.Set || n.Null {
		return enc.WriteToken(jsontext.Null)
	}
	return json.MarshalEncode(enc, n.Value)
}

var _ json.UnmarshalerFrom = (*Opt[int])(nil)
var _ json.MarshalerTo = Opt[int]{}
var _ json.UnmarshalerFrom = (*Nullable[int])(nil)
var _ json.MarshalerTo = Nullable[int]{}
