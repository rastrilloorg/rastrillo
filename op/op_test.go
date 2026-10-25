package op

import (
	"context"
	stdjson "encoding/json"
	"errors"
	"testing"

	json "github.com/go-json-experiment/json"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

type patch struct {
	Title Opt[string] `json:"title"`
	Price Opt[int64]  `json:"price"`
}

func TestOptPresence(t *testing.T) {
	var p patch
	if err := json.Unmarshal([]byte(`{"title":"x"}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Title.Set || p.Title.Value != "x" {
		t.Fatalf("title = %+v, want set x", p.Title)
	}
	if p.Price.Set {
		t.Fatal("an absent member must leave Opt unset")
	}
}

func TestOptRefusesNull(t *testing.T) {
	var p patch
	if err := json.Unmarshal([]byte(`{"title":null}`), &p); err == nil {
		t.Fatal("null must be refused: the manifest has no nullable fields, so null is a client bug")
	}
}

func TestOptZeroIsPresent(t *testing.T) {
	var p patch
	if err := json.Unmarshal([]byte(`{"price":0}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Price.Set || p.Price.Value != 0 {
		t.Fatal("an explicit zero must be Set: a required money field may legitimately be 0")
	}
}

func TestSomeIsSet(t *testing.T) {
	o := Some("x")
	if !o.Set || o.Value != "x" {
		t.Fatalf("Some(x) = %+v, want Set with x", o)
	}
}

func TestOptMarshal(t *testing.T) {
	got, err := json.Marshal(patch{Title: Some("x")})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"title":"x","price":null}` {
		t.Fatalf("got %s: a set Opt writes its value and an unset one writes null", got)
	}
}

func TestRefusalsMatchWithErrorsAs(t *testing.T) {
	var err error = Invalid{Fields: map[string]string{"Title": "Title is required"}}
	var inv Invalid
	if !errors.As(err, &inv) || inv.Fields["Title"] == "" {
		t.Fatal("Invalid must match errors.As by value")
	}
	for _, e := range []error{NotFound{}, Forbidden{Reason: "x"}, Conflict{Message: "y"}} {
		if e.Error() == "" {
			t.Fatalf("%T has an empty message", e)
		}
	}
}

func TestRefusalMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{Invalid{Fields: map[string]string{"Title": "required", "Amount": "too small"}}, "invalid: Amount, Title"},
		{Invalid{}, "invalid"},
		{NotFound{}, "not found"},
		{Forbidden{Reason: "read-only token"}, "forbidden: read-only token"},
		{Conflict{Message: "currency changed"}, "conflict: currency changed"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%T.Error() = %q, want %q", c.err, got, c.want)
		}
	}
}

type priced struct {
	Price Opt[Money]      `json:"price"`
	Fee   Nullable[Money] `json:"fee"`
}

func TestNullablePresence(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"fee":{"amount":5,"currency":"EUR"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Fee.Set || p.Fee.Null || p.Fee.Value != (Money{Amount: 5, Currency: "EUR"}) {
		t.Fatalf("fee = %+v, want Set, not Null, 5 EUR", p.Fee)
	}
	if p.Price.Set {
		t.Fatal("an absent member must leave Nullable's neighbour unset")
	}

	p = priced{}
	if err := json.Unmarshal([]byte(`{}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Fee.Set || p.Fee.Null {
		t.Fatalf("fee = %+v: an absent member must leave Nullable unset and not Null", p.Fee)
	}
}

func TestNullableNull(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"fee":null}`), &p); err != nil {
		t.Fatalf("Nullable must accept null, the one way to clear an optional money field: %v", err)
	}
	if !p.Fee.Set || !p.Fee.Null {
		t.Fatalf("fee = %+v, want Set and Null", p.Fee)
	}
	if p.Fee.Value != (Money{}) {
		t.Fatalf("fee.Value = %+v, want zero after null", p.Fee.Value)
	}
}

func TestNullableMarshal(t *testing.T) {
	cases := []struct {
		in   Nullable[Money]
		want string
	}{
		{Nullable[Money]{}, `null`},
		{Nullable[Money]{Set: true, Null: true}, `null`},
		{Nullable[Money]{Set: true, Value: Money{Amount: 7, Currency: "GBP"}}, `{"amount":7,"currency":"GBP"}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("%+v marshals to %s, want %s", c.in, got, c.want)
		}
	}
}

func TestMoneyRoundTrip(t *testing.T) {
	in := priced{
		Price: Some(Money{Amount: 1490, Currency: "USD"}),
		Fee:   Nullable[Money]{Set: true, Value: Money{Amount: 200}},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"price":{"amount":1490,"currency":"USD"},"fee":{"amount":200,"currency":""}}`
	if string(raw) != want {
		t.Fatalf("marshal = %s, want %s", raw, want)
	}
	var out priced
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

func TestMoneyCurrencyMayBeOmitted(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"price":{"amount":1490}}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Price.Set || p.Price.Value != (Money{Amount: 1490}) {
		t.Fatalf("price = %+v: currency may be left out, the operation fills the default", p.Price)
	}
}

// TestOptNullNamesTheField pins that the refusal reaches the caller as a
// json.SemanticError carrying the member's JSON pointer, which is what
// api.Decode needs to put the message on the right field.
func TestOptNullNamesTheField(t *testing.T) {
	var p patch
	err := json.Unmarshal([]byte(`{"title":null}`), &p)
	var sem *json.SemanticError
	if !errors.As(err, &sem) {
		t.Fatalf("err = %v (%T), want a *json.SemanticError", err, err)
	}
	if sem.JSONPointer != "/title" {
		t.Fatalf("pointer = %q, want /title", sem.JSONPointer)
	}
}

// Descriptor is a plain struct; this only pins its shape, so a later
// generator or api.New cannot drift from the spec without a compile error.
func TestDescriptorShape(t *testing.T) {
	d := Descriptor{
		Operation: rastrillo.Operation{ID: "announcements.get", Resource: "announcements", Access: rastrillo.Read},
		Exposed:   true,
		Input:     []byte(`{"type":"object"}`),
		Result:    []byte(`{"type":"object"}`),
		Invoke: func(ctx context.Context, rc *rastrillo.Ctx, input stdjson.RawMessage) (any, error) {
			return nil, NotFound{}
		},
	}
	if d.ID != "announcements.get" || !d.Exposed {
		t.Fatalf("descriptor = %+v", d)
	}
	_, err := d.Invoke(context.Background(), nil, nil)
	var nf NotFound
	if !errors.As(err, &nf) {
		t.Fatalf("Invoke returned %v, want NotFound", err)
	}
}

// The next four tests decode twice into one destination. A door never
// does that, but a long-lived input value in a test or a tool might, and
// a decode has to describe only the JSON it just read: no flag and no
// field may survive from the previous one.

func TestNullableNullThenValue(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"fee":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"fee":{"amount":5,"currency":"EUR"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Fee.Set || p.Fee.Null || p.Fee.Value != (Money{Amount: 5, Currency: "EUR"}) {
		t.Fatalf("fee = %+v after null then a value, want Set, not Null, 5 EUR", p.Fee)
	}
	got, err := json.Marshal(p.Fee)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"amount":5,"currency":"EUR"}` {
		t.Fatalf("a value decoded after null marshals as %s: a stale Null turned it into a clear", got)
	}
}

func TestNullableValueThenNull(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"fee":{"amount":5,"currency":"EUR"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"fee":null}`), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Fee.Set || !p.Fee.Null || p.Fee.Value != (Money{}) {
		t.Fatalf("fee = %+v after a value then null, want Set, Null and a zero Value", p.Fee)
	}
}

func TestOptReuseDoesNotMerge(t *testing.T) {
	var p priced
	if err := json.Unmarshal([]byte(`{"price":{"amount":1,"currency":"EUR"}}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"price":{"amount":5}}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Price.Value != (Money{Amount: 5}) {
		t.Fatalf("price = %+v: the second decode left out currency, so it must be empty, not merged from the first", p.Price.Value)
	}
}

func TestOptFailedDecodeLeavesUnset(t *testing.T) {
	var p patch
	if err := json.Unmarshal([]byte(`{"title":"x"}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"title":5}`), &p); err == nil {
		t.Fatal("a number is not a string")
	}
	if p.Title.Set || p.Title.Value != "" {
		t.Fatalf("title = %+v after a failed decode, want unset and zero: a refused request must not look like a sent field", p.Title)
	}
}

// TestNullableIsTheOnlyNullable pins the marker api.Decode reads to let
// null through. If Nullable lost it, clearing optional money would be
// refused; if Opt gained it, a required field would accept null.
func TestNullableIsTheOnlyNullable(t *testing.T) {
	type nullable interface{ IsNullable() bool }
	for _, v := range []any{&Nullable[Money]{}, Nullable[int64]{}} {
		n, ok := v.(nullable)
		if !ok || !n.IsNullable() {
			t.Errorf("%T: IsNullable missing or false; api.Decode would refuse null for it", v)
		}
	}
	for _, v := range []any{&Opt[Money]{}, Opt[string]{}, &Money{}} {
		if n, ok := v.(nullable); ok && n.IsNullable() {
			t.Errorf("%T reports IsNullable; api.Decode would accept null for it", v)
		}
	}
}
