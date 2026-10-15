package replay

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/eventlog"
)

func ver(n int) *int { return &n }

func v2(kind string, amounts map[string]int64, currencies map[string]string) Event {
	return Event{V: ver(2), Kind: kind, Amounts: amounts, Currencies: currencies}
}

func amt(f Field) string {
	if f.Minor == nil && f.Currency == nil {
		return "empty"
	}
	if f.Minor == nil || f.Currency == nil {
		return "BROKEN"
	}
	return strings.TrimSpace(*f.Currency) + " " + itoa(*f.Minor)
}

func itoa(v int64) string {
	var b []byte
	neg := v < 0
	if neg {
		v = -v
	}
	for {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
		if v == 0 {
			break
		}
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Confirmation round, gap 3: version-2 history from before the money
// field existed replays, the field is empty, and a later event that
// sets an amount (with its currency) fills it.
func TestFieldAddedAfterVersion2History(t *testing.T) {
	history := []Event{
		v2("created", nil, nil), // created before Price existed
		v2("updated", nil, nil), // a notes-only edit, still before Price
		v2("updated", nil, nil),
	}
	rec, err := Replay(history, []string{"Price"}, nil) // no [legacy]: none needed
	if err != nil || amt(rec["Price"]) != "empty" {
		t.Fatalf("pre-addition history: %v, %s", err, amt(rec["Price"]))
	}
	later := append(history,
		v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": "EUR"}),
		v2("updated", nil, nil), // amount-free edit after: unchanged
		v2("updated", map[string]int64{"Price": 1300}, map[string]string{"Price": "EUR"}),
	)
	rec, err = Replay(later, []string{"Price"}, nil)
	if err != nil || amt(rec["Price"]) != "EUR 1300" {
		t.Errorf("after the amount is set: %v, %s", err, amt(rec["Price"]))
	}
}

func TestCurrencyIfAndOnlyIfAmount(t *testing.T) {
	for name, c := range map[string]struct {
		events []Event
		legacy *currency.Legacy
		want   string // "EUR 1250", "empty", or an error substring
	}{
		"amount without currency":          {[]Event{v2("created", map[string]int64{"Price": 1}, nil)}, nil, "event 0: Price has an amount but no currency"},
		"currency without amount":          {[]Event{v2("updated", nil, map[string]string{"Price": "EUR"})}, nil, "event 0: Price has a currency but no amount"},
		"currency change":                  {[]Event{v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "EUR"}), v2("updated", map[string]int64{"Price": 2}, map[string]string{"Price": "USD"})}, nil, "event 1: Price changes currency EUR to USD: a currency change needs its own event"},
		"same currency repeated":           {[]Event{v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "EUR"}), v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": "eur"})}, nil, "EUR 1250"},
		"unsupported currency":             {[]Event{v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "KDW"})}, nil, "event 0: Price: currency code is not in the storage set"},
		"unknown version":                  {[]Event{{V: ver(3), Kind: "created"}}, nil, "event 0: version 3 is not one this binary understands"},
		"unknown version deleted":          {[]Event{v2("created", nil, nil), {V: ver(3), Kind: "deleted"}}, nil, "event 1: version 3 is not one this binary understands"},
		"explicit version 0":               {[]Event{{V: ver(0), Kind: "deleted"}}, nil, "event 0: version 0 is not one this binary understands"},
		"absent version is legacy":         {[]Event{{Kind: "created", Amounts: map[string]int64{"Price": 1250}}}, &currency.Legacy{Currency: "USD", Scale: 2}, "USD 1250"},
		"v2 deleted is fine":               {[]Event{v2("created", map[string]int64{"Price": 1250}, map[string]string{"Price": "EUR"}), v2("deleted", nil, nil)}, nil, "EUR 1250"},
		"historical create, neither field": {[]Event{v2("created", nil, nil)}, nil, "empty"},
		"amount with unchanged currency":   {[]Event{v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "EUR"}), v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": " EUR"})}, nil, "EUR 1250"},
		"legacy needs [legacy]":            {[]Event{{Kind: "created", Amounts: map[string]int64{"Price": 1250}}}, nil, "event 0: a version-1 amount for Price needs [legacy] in the manifest"},
		"legacy amount-free":               {[]Event{{Kind: "created"}, {Kind: "updated"}}, nil, "empty"},
		"legacy then new EUR":              {[]Event{v2("created", map[string]int64{"Price": 100}, map[string]string{"Price": "EUR"}), {Kind: "updated", Amounts: map[string]int64{"Price": 1250}}}, &currency.Legacy{Currency: "USD", Scale: 2}, "EUR 1250"},
		"legacy into JPY inexact":          {[]Event{v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "JPY"}), {Kind: "updated", Amounts: map[string]int64{"Price": 1250}}}, &currency.Legacy{Currency: "USD", Scale: 2}, "event 1: Price: 1250 at scale 2 is 12.50, which JPY cannot hold"},
		"legacy into JPY exact":            {[]Event{{Kind: "created", Amounts: map[string]int64{"Price": 1200}}}, &currency.Legacy{Currency: "JPY", Scale: 2}, "JPY 12"},
		"legacy into KWD":                  {[]Event{{Kind: "created", Amounts: map[string]int64{"Price": 1250}}}, &currency.Legacy{Currency: "KWD", Scale: 2}, "KWD 12500"},
		"legacy with a currency":           {[]Event{{Kind: "created", Amounts: map[string]int64{"Price": 1}, Currencies: map[string]string{"Price": "EUR"}}}, &currency.Legacy{Currency: "USD", Scale: 2}, "event 0: a version-1 event carries a currency for Price"},
	} {
		rec, err := Replay(c.events, []string{"Price"}, c.legacy)
		got := ""
		if err != nil {
			got = err.Error()
		} else {
			got = amt(rec["Price"])
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

// A currency change, and only a currency change, matches
// ErrCurrencyChange, through the wrapping a generated store adds, with
// its approved text unchanged.
func TestOnlyACurrencyChangeMatchesErrCurrencyChange(t *testing.T) {
	eur := v2("created", map[string]int64{"Price": 1}, map[string]string{"Price": "EUR"})
	_, err := Replay([]Event{eur, v2("updated", map[string]int64{"Price": 2}, map[string]string{"Price": "USD"})}, []string{"Price"}, nil)
	wrapped := fmt.Errorf("%s: %w", "prices/1", err)
	if !errors.Is(wrapped, ErrCurrencyChange) || wrapped.Error() != "prices/1: event 1: Price changes currency EUR to USD: a currency change needs its own event" {
		t.Errorf("a currency change: %v (matches: %v)", wrapped, errors.Is(wrapped, ErrCurrencyChange))
	}
	_, err = Replay([]Event{eur, v2("updated", map[string]int64{"Price": 2}, nil)}, []string{"Price"}, nil)
	if err == nil || errors.Is(err, ErrCurrencyChange) {
		t.Errorf("an amount without its currency: %v; want a refusal that is not a currency change", err)
	}
}

var priceMoney = []Money{{Field: "Price", AmountKey: "price", CurrencyKey: "price_currency"}}

func ev(stream, writer string, seq int64, payload string) eventlog.Event {
	return eventlog.Event{Stream: stream, Writer: writer, Seq: seq, Kind: "updated", Payload: json.RawMessage(payload)}
}

func TestClearAndHalfClear(t *testing.T) {
	r := Rules{Fields: priceMoney}
	rec := map[string]Field{}
	for i, e := range []eventlog.Event{
		ev("t/1", "w", 1, `{"V":2,"price":1250,"price_currency":"EUR"}`),
		ev("t/1", "w", 2, `{"V":2,"title":"x"}`),                        // absent: unchanged
		ev("t/1", "w", 3, `{"V":2,"price":null,"price_currency":null}`), // explicit clear
	} {
		if err := r.Apply(rec, i, e); err != nil {
			t.Fatal(err)
		}
		if i == 1 && amt(rec["Price"]) != "EUR 1250" {
			t.Errorf("an absent key changed the field: %s", amt(rec["Price"]))
		}
	}
	if amt(rec["Price"]) != "empty" {
		t.Errorf("after a clear: %s", amt(rec["Price"]))
	}
	for _, bad := range []string{
		`{"V":2,"price":null}`,
		`{"V":2,"price":null,"price_currency":"EUR"}`,
		`{"V":2,"price":5,"price_currency":null}`,
		`{"price":null}`,      // version 1 never wrote null
		`{"V":"2","price":5}`, // V must be an integer
	} {
		if err := r.Apply(map[string]Field{}, 0, ev("t/1", "w", 1, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// Review finding 4: the boundary applies to every fold, so an
// unversioned event ingested after the app started is refused by the
// next Get or List, not only by the next boot.
func TestRulesApplyEnforcesTheBoundary(t *testing.T) {
	r := Rules{Fields: priceMoney, Legacy: &currency.Legacy{Currency: "USD", Scale: 2},
		Boundary: map[eventlog.StreamWriter]int64{{Stream: "t/1", Writer: "w"}: 1}, HasBoundary: true}
	if err := r.Apply(map[string]Field{}, 0, ev("t/1", "w", 1, `{"price":1250}`)); err != nil {
		t.Errorf("a pre-upgrade legacy event: %v", err)
	}
	if err := r.Apply(map[string]Field{}, 1, ev("t/1", "w", 2, `{"price":1250}`)); err == nil {
		t.Error("an unversioned event after the boundary was accepted")
	}
}

func TestStepMatchesReplay(t *testing.T) {
	events := []Event{
		v2("created", nil, nil),
		v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": "EUR"}),
		v2("updated", nil, nil),
		v2("updated", map[string]int64{"Price": 1300}, map[string]string{"Price": "eur"}),
	}
	rec := map[string]Field{}
	for i, ev := range events {
		if err := Step(rec, i, ev, []string{"Price"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	all, err := Replay(events, []string{"Price"}, nil)
	if err != nil || amt(rec["Price"]) != "EUR 1300" || amt(all["Price"]) != "EUR 1300" {
		t.Errorf("Step %s, Replay %s, %v", amt(rec["Price"]), amt(all["Price"]), err)
	}
}

// Spec §12 item 6, last case: an unversioned event after the upgrade
// marker is a replay error, judged per stream and writer because the
// log has no global position.
func TestCheckUnversioned(t *testing.T) {
	boundary := map[eventlog.StreamWriter]int64{{Stream: "ticket/1", Writer: "w1"}: 3}
	for _, c := range []struct {
		stream, writer string
		seq            int64
		ok             bool
	}{
		{"ticket/1", "w1", 3, true},  // written before the upgrade
		{"ticket/1", "w1", 4, false}, // the same writer, after it
		{"ticket/1", "w2", 1, false}, // a writer that had written nothing before
		{"ticket/2", "w1", 1, false}, // a stream that did not exist before
	} {
		err := CheckUnversioned(boundary, eventlog.Event{Stream: c.stream, Writer: c.writer, Seq: c.seq})
		if (err == nil) != c.ok {
			t.Errorf("%s/%s/%d: %v, want ok=%v", c.stream, c.writer, c.seq, err, c.ok)
		}
	}
}

// Each refusal names its event by its place in the fold, the index
// Apply was given, and uses Appendix C.2's approved wording. A newer
// version is refused before its payload's shape is read: a version-3
// writer may clear fields in a way version 2 cannot parse.
func TestApplyMessages(t *testing.T) {
	r := Rules{Fields: priceMoney}
	for payload, want := range map[string]string{
		`{"V":2,"price":null}`:                                        "event 2: Price clears the amount or the currency but not both",
		`{"V":2,"price":null,"price_currency":"EUR"}`:                 "event 2: Price clears the amount or the currency but not both",
		`{"V":2,"price":5,"price_currency":null}`:                     "event 2: Price clears the amount or the currency but not both",
		`{"price":null}`:                                              "event 2: a version-1 event has a null amount or currency for Price",
		`{"price":5,"price_currency":null}`:                           "event 2: a version-1 event has a null amount or currency for Price",
		`{"V":"2","price":5}`:                                         "event 2: the version is not a whole number",
		`{"V":null}`:                                                  "event 2: the version is not a whole number",
		`{"V":2.5}`:                                                   "event 2: the version is not a whole number",
		`{"V":3,"price":null}`:                                        "event 2: version 3 is not one this binary understands",
		`{"V":2,"price":12.5,"price_currency":"EUR"}`:                 "event 2: Price: amount is not an integer count of minor units",
		`{"V":2,"price":"5","price_currency":"EUR"}`:                  "event 2: Price: amount is not an integer count of minor units",
		`{"V":2,"price":1e30,"price_currency":"EUR"}`:                 "event 2: Price: amount is not an integer count of minor units",
		`{"V":2,"price":99999999999999999999,"price_currency":"EUR"}`: "event 2: Price: amount does not fit in int64",
		`{"V":2,"price":5,"price_currency":5}`:                        "event 2: Price: currency code is not three ASCII letters",
		`{"V":2,"price":5,"price_currency":"EURO"}`:                   "event 2: Price: currency code is not three ASCII letters",
	} {
		err := r.Apply(map[string]Field{}, 2, ev("t/1", "w", 1, payload))
		if err == nil || err.Error() != want {
			t.Errorf("%s: got %v, want %q", payload, err, want)
		}
	}

	r = Rules{Fields: priceMoney, Boundary: map[eventlog.StreamWriter]int64{}, HasBoundary: true}
	err := r.Apply(map[string]Field{}, 0, ev("t/1", "w", 2, `{}`))
	const after = "t/1: an event from writer w (seq 2) has no version but was written after the money upgrade, so a currency-unaware version wrote it"
	if err == nil || err.Error() != after {
		t.Errorf("after the upgrade: got %v, want %q", err, after)
	}
	// Without a boundary (HasBoundary false) the same event is legacy.
	if err := (Rules{Fields: priceMoney}).Apply(map[string]Field{}, 0, ev("t/1", "w", 2, `{}`)); err != nil {
		t.Errorf("no boundary: %v", err)
	}
}

// A clear empties the field whether or not it had an amount, and the
// field is then like one that never had an amount: the next amount
// sets it with its own currency, as an SQL row's NULL/NULL would.
func TestClearThenSet(t *testing.T) {
	events := []Event{
		{V: ver(2), Kind: "updated", Cleared: map[string]bool{"Price": true}}, // never had an amount
		v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": "EUR"}),
		{V: ver(2), Kind: "updated", Cleared: map[string]bool{"Price": true}},
		v2("updated", map[string]int64{"Price": 900}, map[string]string{"Price": "JPY"}),
	}
	rec, err := Replay(events, []string{"Price"}, nil)
	if err != nil || amt(rec["Price"]) != "JPY 900" {
		t.Errorf("got %s, %v", amt(rec["Price"]), err)
	}
	// A clear built by hand alongside an amount, or in a version-1
	// event, is refused like the payloads Decode refuses.
	for name, c := range map[string]struct {
		ev   Event
		want string
	}{
		"clear with an amount": {Event{V: ver(2), Kind: "updated", Amounts: map[string]int64{"Price": 1}, Cleared: map[string]bool{"Price": true}}, "event 0: Price clears the amount or the currency but not both"},
		"version-1 clear":      {Event{Kind: "updated", Cleared: map[string]bool{"Price": true}}, "event 0: a version-1 event has a null amount or currency for Price"},
	} {
		_, err := Replay([]Event{c.ev}, []string{"Price"}, nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
}

func TestDecode(t *testing.T) {
	e, err := Decode(ev("t/1", "w", 1, `{"V":2,"price":-1250,"price_currency":" eur","title":"x"}`), priceMoney)
	if err != nil || e.V == nil || *e.V != Version || e.Kind != "updated" ||
		e.Amounts["Price"] != -1250 || e.Currencies["Price"] != " eur" || e.Cleared["Price"] {
		t.Errorf("got %+v, %v", e, err)
	}
	e, err = Decode(ev("t/1", "w", 1, `{"price":null,"price_currency":null,"V":2}`), priceMoney)
	if err != nil || !e.Cleared["Price"] || len(e.Amounts) != 0 || len(e.Currencies) != 0 {
		t.Errorf("clear: got %+v, %v", e, err)
	}
	e, err = Decode(ev("t/1", "w", 1, `{"price":1250}`), priceMoney)
	if err != nil || e.V != nil || e.Amounts["Price"] != 1250 {
		t.Errorf("legacy: got %+v, %v", e, err)
	}
}
