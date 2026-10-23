package op

import (
	"errors"
	"testing"
)

type decodeInput struct {
	Title   Opt[string]     `json:"title"`
	Price   Opt[Money]      `json:"price"`
	Deposit Nullable[Money] `json:"deposit"`
}

type decodeArgs struct {
	ID          int64 `json:"id"`
	decodeInput `json:",inline"`
}

// TestDecodeInput pins the rules both doors share: what a member may
// be, which one may be null, and that a refusal names the member as
// the client sent it. A refusal of the whole value has no field, so a
// resource with a field called body is never blamed for a bad body.
func TestDecodeInput(t *testing.T) {
	for _, c := range []struct {
		in, field, msg string // field "" and msg "" = accepted; field "" with a msg = refused with no field
	}{
		{`{"id":3,"title":"x","price":{"amount":1490},"deposit":null}`, "", ""},
		{`{"id":3,"deposit":{"amount":5,"currency":"eur"}}`, "", ""},
		{`{"id":null}`, "id", "Send a value, or leave this field out."},
		{`{"title":null}`, "title", "Send a value, or leave this field out."},
		{`{"price":null}`, "price", "Send a value, or leave this field out."},
		{`{"price":{"currency":"USD"}}`, "price", "Send an amount."},
		{`{"price":{"amount":14.9}}`, "price", "Check the values inside this field."},
		{`{"price":{"amount":1,"nope":2}}`, "price", "Check the values inside this field."},
		{`{"deposit":{"amount":null}}`, "deposit", "Check the values inside this field."},
		{`{"nope":1}`, "nope", "Unknown field. Check the spelling and case."},
		{`{"Title":"x"}`, "Title", "Unknown field. Check the spelling and case."},
		{`{"id":"3"}`, "id", "Send a whole number."},
		{`{"id":1.5}`, "id", "Send a whole number."},
		{`{"title":5}`, "title", "Send a string."},
		{`{"price":5}`, "price", "Send an object."},
		{`{"id":1,"id":2}`, "id", "Send this field once."},
		{`[]`, "", "invalid"},
		{`null`, "", "invalid"},
		{``, "", "invalid"},
		{`{"id":1} {}`, "", "invalid"},
		{`{"id":`, "", "invalid"},
	} {
		var a decodeArgs
		err := DecodeInput([]byte(c.in), &a)
		var inv Invalid
		switch {
		case c.msg == "" && err != nil:
			t.Errorf("%s: %v, want accepted", c.in, err)
		case c.msg != "" && c.field == "" && (!errors.As(err, &inv) || len(inv.Fields) != 0):
			t.Errorf("%s: %v, want Invalid with no fields", c.in, err)
		case c.field != "" && (!errors.As(err, &inv) || inv.Fields[c.field] != c.msg || len(inv.Fields) != 1):
			t.Errorf("%s: %v, want Invalid with only %s = %q", c.in, err, c.field, c.msg)
		}
	}
	var a decodeArgs
	if err := DecodeInput([]byte(`{"id":3,"deposit":null,"price":{"amount":0}}`), &a); err != nil ||
		a.ID != 3 || !a.Deposit.Set || !a.Deposit.Null || !a.Price.Set || a.Price.Value != (Money{}) {
		t.Fatalf("decoded %+v, %v; want id 3, deposit cleared, a zero price that was sent", a, err)
	}
	if err := DecodeInput([]byte(`{"deposit":{"amount":5,"currency":" eur "}}`), &a); err != nil ||
		a.Deposit.Value != (Money{Amount: 5, Currency: " eur "}) {
		t.Fatalf("decoded %+v, %v; want the currency as sent: the operation normalises it", a, err)
	}
}
