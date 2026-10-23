package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"

	"amadan.net/rastrillo/rastrillo/op"
)

// defaultMaxBody is the cap when nothing set one: Config.MaxBody's
// default, and what a hand-written handler outside the tree gets.
const defaultMaxBody int64 = 1 << 20

// The English a 422 from Decode or NoBody carries when the body as a
// whole is refused. A refusal tied to one member puts op.DecodeInput's
// message in fields under that member's name, and the top-level message
// stays the table's; a refusal of the body as a whole has no member to
// name, so the top-level message says what to send instead.
const (
	msgNoBody    = "Send this request with no body."
	msgNotObject = "Send one JSON object as the body."
)

type maxBodyCtxKey struct{}

// WithMaxBody sets the most bytes Decode will read from r's body. The
// tree sets it from Config.MaxBody on every request; n <= 0 keeps the
// 1 MiB default, so a zero Config field cannot mean "no body at all".
func WithMaxBody(r *http.Request, n int64) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), maxBodyCtxKey{}, n))
}

func maxBodyFrom(r *http.Request) int64 {
	if n, ok := r.Context().Value(maxBodyCtxKey{}).(int64); ok && n > 0 {
		return n
	}
	return defaultMaxBody
}

// Decode reads r's body into dst, a non-nil pointer, or answers the
// refusal itself and returns false. The handler returns at once on
// false and must not read dst, which may be partly filled.
//
// It refuses, in this order:
//
//   - 415 when Content-Type is not application/json. A browser form
//     cannot send that type, so this is also the cross-site defence.
//   - 413 when the body is over the cap (WithMaxBody, default 1 MiB).
//     The whole bounded body is read before any parsing, so an oversized
//     body is 413 even when it is also broken JSON: size always wins.
//   - 422 when the body is not exactly one JSON object: bad syntax,
//     another kind of value, or anything after it.
//   - 422 with fields for a member that is refused: unknown (names match
//     case-sensitively), sent twice, the wrong type, or null.
//
// The last two are op.DecodeInput's rules, which Descriptor.Invoke
// applies to the same bytes, so what this door accepts and what Invoke
// accepts cannot drift. null is refused everywhere except for a member
// whose destination field has op.Nullable's IsNullable marker.
func Decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if v := reflect.ValueOf(dst); v.Kind() != reflect.Pointer || v.IsNil() {
		// A handler bug, not a client error: the tree's recoverer turns
		// it into a 500 with a ref rather than a 422 that blames the client.
		panic(fmt.Sprintf("api: Decode needs a non-nil pointer, got %T", dst))
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		answer(w, "unsupported_media_type")
		return false
	}
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, body, maxBodyFrom(r)))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			answer(w, "too_large")
			return false
		}
		// The client stopped sending mid-body. What arrived is not a whole
		// object, and the client is most likely gone; a 500 would log an
		// error for every dropped connection.
		invalid(w, msgNotObject, nil)
		return false
	}
	if err := op.DecodeInput(data, dst); err != nil {
		var inv op.Invalid
		if !errors.As(err, &inv) {
			panic(fmt.Errorf("api: op.DecodeInput returned %T, not op.Invalid", err))
		}
		// No field means the body as a whole was refused. The fields are
		// written as DecodeInput keyed them, which is the name the client
		// sent, so a wrong-case name is reported in its own spelling.
		if len(inv.Fields) == 0 {
			invalid(w, msgNotObject, nil)
			return false
		}
		invalid(w, codes["invalid"].message, inv.Fields)
		return false
	}
	return true
}

// NoBody is what a handler with no input calls first, as a DELETE does.
// Any body at all is 422, whatever its Content-Type: a client that sent
// one meant something by it, and silently dropping it would hide that
// the request did not do what they thought.
func NoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength > 0 {
		invalid(w, msgNoBody, nil)
		return false
	}
	if r.Body == nil {
		return true
	}
	// A chunked body has no length up front; one byte is enough to know.
	var b [1]byte
	if n, err := io.ReadFull(r.Body, b[:]); n > 0 || (err != nil && err != io.EOF) {
		invalid(w, msgNoBody, nil)
		return false
	}
	return true
}

func invalid(w http.ResponseWriter, message string, fields map[string]string) {
	WriteError(w, codes["invalid"].status, Error{Code: "invalid", Message: message, Fields: fields})
}
