package api

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/op"
)

type titled struct {
	Title string `json:"title"`
}

type optionalPrice struct {
	Price op.Nullable[op.Money] `json:"price"`
}

type requiredPrice struct {
	Price op.Opt[op.Money] `json:"price"`
}

type feeBase struct {
	Fee op.Nullable[op.Money] `json:"fee"`
}

// withEmbedded checks that null is allowed by the destination field's
// type, found the way json finds it, and not only for top-level fields
// declared in the struct itself.
type withEmbedded struct {
	feeBase
	Title string `json:"title"`
}

const (
	wantFields    = "Check the fields and try again."
	wantNotObject = "Send one JSON object as the body."
	wantUnknown   = "Unknown field. Check the spelling and case."
	wantNull      = "Send a value, or leave this field out."
	wantOnce      = "Send this field once."
	wantNested    = "Check the values inside this field."
)

// sized is a valid {"title":"xxx"} body exactly n bytes long, so a test
// can sit on either side of a cap without the JSON being the reason.
func sized(n int) string {
	const head, tail = `{"title":"`, `"}`
	return head + strings.Repeat("x", n-len(head)-len(tail)) + tail
}

func TestDecode(t *testing.T) {
	const capDefault = 1 << 20
	withMax := func(n int64) func(*http.Request) *http.Request {
		return func(r *http.Request) *http.Request { return WithMaxBody(r, n) }
	}
	newTitled := func() any { return new(titled) }
	cases := []struct {
		name    string
		ct      string // "" sends no Content-Type at all
		body    string
		req     func(*http.Request) *http.Request
		dst     func() any
		ok      bool
		status  int
		code    string
		message string
		fields  map[string]string
		check   func(t *testing.T, dst any)
	}{
		{name: "text/plain is not JSON", ct: "text/plain", body: `{}`, dst: newTitled,
			status: 415, code: "unsupported_media_type", message: "Send the body as JSON, with Content-Type: application/json."},
		{name: "charset parameter is fine", ct: "application/json; charset=utf-8", body: `{"title":"x"}`, dst: newTitled, ok: true,
			check: func(t *testing.T, dst any) {
				if got := dst.(*titled).Title; got != "x" {
					t.Errorf("Title = %q, want x", got)
				}
			}},
		{name: "no content type", body: `{}`, dst: newTitled,
			status: 415, code: "unsupported_media_type", message: "Send the body as JSON, with Content-Type: application/json."},
		{name: "default cap is 1 MiB, inclusive", ct: "application/json", body: sized(capDefault), dst: newTitled, ok: true},
		{name: "one byte over the default cap", ct: "application/json", body: sized(capDefault + 1), dst: newTitled,
			status: 413, code: "too_large", message: "The request body is too large."},
		{name: "WithMaxBody 0 keeps the default", ct: "application/json", body: sized(capDefault), req: withMax(0), dst: newTitled, ok: true},
		{name: "WithMaxBody sets the cap, inclusive", ct: "application/json", body: sized(16), req: withMax(16), dst: newTitled, ok: true},
		{name: "one byte over WithMaxBody", ct: "application/json", body: sized(17), req: withMax(16), dst: newTitled,
			status: 413, code: "too_large", message: "The request body is too large."},
		{name: "size wins over syntax", ct: "application/json", body: `{"title":"xxxxxxx`, req: withMax(16), dst: newTitled,
			status: 413, code: "too_large", message: "The request body is too large."},
		{name: "truncated", ct: "application/json", body: `{"title":`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "empty body", ct: "application/json", body: ``, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "array", ct: "application/json", body: `[]`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "null body", ct: "application/json", body: `null`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "string body", ct: "application/json", body: `"x"`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "unknown member", ct: "application/json", body: `{"title":"x","nope":1}`, dst: newTitled,
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"nope": wantUnknown}},
		{name: "duplicate member", ct: "application/json", body: `{"title":"x","title":"y"}`, dst: newTitled,
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"title": wantOnce}},
		{name: "trailing value", ct: "application/json", body: `{"title":"x"} {}`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "trailing whitespace is fine", ct: "application/json", body: "{\"title\":\"x\"}\r\n ", dst: newTitled, ok: true},
		{name: "names match case-sensitively", ct: "application/json", body: `{"Title":"x"}`, dst: newTitled,
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"Title": wantUnknown}},
		{name: "null into a string", ct: "application/json", body: `{"title":null}`, dst: newTitled,
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"title": wantNull}},
		{name: "empty object", ct: "application/json", body: `{}`, dst: newTitled, ok: true},
		// The whole body is checked as JSON before any field is decoded, so
		// a body broken further on is not reported as a wrong field type.
		{name: "syntax beats a wrong type earlier on", ct: "application/json", body: `{"title":5,`, dst: newTitled,
			status: 422, code: "invalid", message: wantNotObject},
		{name: "null clears optional money", ct: "application/json", body: `{"price":null}`,
			dst: func() any { return new(optionalPrice) }, ok: true,
			check: func(t *testing.T, dst any) {
				if p := dst.(*optionalPrice).Price; !p.Set || !p.Null {
					t.Errorf("Price = %+v, want Set and Null", p)
				}
			}},
		{name: "null into required money", ct: "application/json", body: `{"price":null}`,
			dst:    func() any { return new(requiredPrice) },
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"price": wantNull}},
		// Nullable lets null through for the field, not for what is inside
		// it: a null amount would otherwise decode as zero and set the
		// price to nothing without saying so.
		{name: "null inside optional money", ct: "application/json", body: `{"price":{"amount":null}}`,
			dst:    func() any { return new(optionalPrice) },
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"price": wantNested}},
		{name: "null clears an embedded optional money field", ct: "application/json", body: `{"fee":null,"title":"x"}`,
			dst: func() any { return new(withEmbedded) }, ok: true,
			check: func(t *testing.T, dst any) {
				if f := dst.(*withEmbedded).Fee; !f.Set || !f.Null {
					t.Errorf("Fee = %+v, want Set and Null", f)
				}
			}},
		{name: "null beside an embedded nullable", ct: "application/json", body: `{"fee":null,"title":null}`,
			dst:    func() any { return new(withEmbedded) },
			status: 422, code: "invalid", message: wantFields, fields: map[string]string{"title": wantNull}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(tc.body))
			if tc.ct != "" {
				r.Header.Set("Content-Type", tc.ct)
			}
			if tc.req != nil {
				r = tc.req(r)
			}
			rec := httptest.NewRecorder()
			dst := tc.dst()
			ok := Decode(rec, r, dst)
			if ok != tc.ok {
				t.Fatalf("Decode = %v, want %v (status %d, body %s)", ok, tc.ok, rec.Code, rec.Body)
			}
			if ok {
				// A handler that goes on to answer must find the response
				// untouched, or its own status would be a superfluous write.
				if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
					t.Errorf("Decode accepted but wrote %q", rec.Body)
				}
				if tc.check != nil {
					tc.check(t, dst)
				}
				return
			}
			assertRefusal(t, rec, tc.status, tc.code, tc.message, tc.fields)
		})
	}
}

func assertRefusal(t *testing.T, rec *httptest.ResponseRecorder, status int, code, message string, fields map[string]string) {
	t.Helper()
	assertHeaders(t, rec)
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	e := decodeError(t, rec)
	if e.Code != code {
		t.Errorf("code = %q, want %q", e.Code, code)
	}
	if e.Message != message {
		t.Errorf("message = %q, want %q", e.Message, message)
	}
	if !maps.Equal(e.Fields, fields) {
		t.Errorf("fields = %v, want %v", e.Fields, fields)
	}
}

// kinds has one field per message a wrong type can get, so each message
// is pinned to the Go type that earns it.
type kinds struct {
	S   string           `json:"s"`
	I   op.Opt[int64]    `json:"i"`
	F   float64          `json:"f"`
	B   bool             `json:"b"`
	M   op.Opt[op.Money] `json:"m"`
	L   []string         `json:"l"`
	T   time.Time        `json:"t"`
	Raw []byte           `json:"raw"`
}

func TestDecodeFieldMessages(t *testing.T) {
	cases := []struct {
		body, field, message string
	}{
		{`{"s":1}`, "s", "Send a string."},
		{`{"i":"1"}`, "i", "Send a whole number."},
		{`{"i":1.5}`, "i", "Send a whole number."},
		{`{"i":1e400}`, "i", "Send a whole number."},
		{`{"f":"1"}`, "f", "Send a number."},
		{`{"b":1}`, "b", "Send true or false."},
		{`{"m":5}`, "m", "Send an object."},
		{`{"l":{}}`, "l", "Send an array."},
		{`{"raw":5}`, "raw", "Send a string."},
		// time.Time is a struct, but on the wire it is a string; it decodes
		// itself, so Decode cannot say what shape it wanted.
		{`{"t":5}`, "t", "Check this value."},
		{`{"t":"yesterday"}`, "t", "Check this value."},
		{`{"m":{"amount":"1"}}`, "m", wantNested},
		{`{"m":{"cents":1}}`, "m", wantNested},
		{`{"m":{"amount":1,"amount":2}}`, "m", wantNested},
		{`{"l":["a",1]}`, "l", wantNested},
		{`{"a/b":1}`, "a/b", wantUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			if Decode(rec, r, new(kinds)) {
				t.Fatal("Decode accepted a wrong type")
			}
			assertRefusal(t, rec, 422, "invalid", wantFields, map[string]string{tc.field: tc.message})
		})
	}
}

func TestNoBody(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		ct      string
		unsized bool // chunked: no Content-Length, so the body must be read
		ok      bool
	}{
		{name: "empty", ok: true},
		{name: "empty with a content type", ct: "text/plain", ok: true},
		// The body is refused before the content type is looked at, so a
		// DELETE carrying anything gets the same answer whatever it says
		// the body is.
		{name: "body with the wrong content type", body: `{}`, ct: "text/plain"},
		{name: "body with the right content type", body: `{}`, ct: "application/json"},
		{name: "chunked body", body: `x`, unsized: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r *http.Request
			if tc.body == "" {
				r = httptest.NewRequest(http.MethodDelete, "/api/v1/things/1", nil)
			} else {
				r = httptest.NewRequest(http.MethodDelete, "/api/v1/things/1", strings.NewReader(tc.body))
			}
			if tc.ct != "" {
				r.Header.Set("Content-Type", tc.ct)
			}
			if tc.unsized {
				r.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			ok := NoBody(rec, r)
			if ok != tc.ok {
				t.Fatalf("NoBody = %v, want %v (status %d, body %s)", ok, tc.ok, rec.Code, rec.Body)
			}
			if ok {
				if rec.Body.Len() != 0 {
					t.Errorf("NoBody accepted but wrote %q", rec.Body)
				}
				return
			}
			assertRefusal(t, rec, 422, "invalid", "Send this request with no body.", nil)
		})
	}
}
