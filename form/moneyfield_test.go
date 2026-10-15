package form_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/locales"
	// en and de are the two locales round two's KWD scenario needs; ar
	// is here because the Arabic decimal mark is the one input a strict
	// field refuses and a normalising one reads, and without its pack
	// "ar" fails the locale check and proves nothing about either.
	_ "amadan.net/rastrillo/rastrillo/locales/ar"
	_ "amadan.net/rastrillo/rastrillo/locales/de"
	_ "amadan.net/rastrillo/rastrillo/locales/en"
)

func post(values url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

var price = form.Field{Name: "Price", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Required: true}

// Spec §12 item 5: round two's KWD scenario, on a Strict field, where
// the locale echo alone decides: an English form shows 1.500 (one and
// a half dinars); parsed in the locale that rendered it, it is 1500
// fils, and in German it is 1,500,000.
func TestLocaleEchoParsesInTheRenderingLocale(t *testing.T) {
	strict := price
	strict.Strict = true
	p := form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}}), strict)
	if m, ok := p.Minor("Price"); !p.OK() || !ok || m != 1500 {
		t.Fatalf("en echo: %d, %v, %v", m, ok, p.Errors())
	}
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"de"}, "Price.currency": {"KWD"}}), strict)
	if m, _ := p.Minor("Price"); m != 1500000 {
		t.Errorf("de echo: %d, want 1500000 (1.500 is fifteen hundred in German)", m)
	}
	// Normalising (the default): the same English text typed fresh has
	// two readings and is refused with both; German text is read the
	// page's way (decision 23).
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}}), price)
	if p.Errors()["Price"] != "rastrillo.ui.money_ambiguous" || fmt.Sprint(p.ErrorArgs()["Price"]) != "map[a:1.5 KWD b:1500 KWD]" {
		t.Errorf("normalising en 1.500 KWD: %v %v", p.Errors(), p.ErrorArgs())
	}
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"de"}, "Price.currency": {"KWD"}}), price)
	if m, _ := p.Minor("Price"); m != 1500000 {
		t.Errorf("normalising de 1.500 KWD: %d, want 1500000", m)
	}
}

func TestLocaleEchoRejectedValuesReseed(t *testing.T) {
	for name, locs := range map[string][]string{
		"missing":       nil,
		"duplicated":    {"en", "en"},
		"malformed":     {"de--CH"},
		"non-canonical": {"EN"},
		"too long":      {strings.Repeat("a", 36)},
		"bad bytes":     {"en_US"},
	} {
		v := url.Values{"Price": {"12.500"}, "Price.currency": {"KWD"}}
		if locs != nil {
			v["Price.locale"] = locs
		}
		p := form.Parse(post(v), price)
		if p.Errors()["Price"] != "rastrillo.ui.money_reenter" {
			t.Errorf("%s: errors %v, want money_reenter", name, p.Errors())
		}
		if _, reseed := p.MoneyInput("Price", "en"); !reseed {
			t.Errorf("%s: a rejected locale must reseed from the server, not echo text of unknown meaning", name)
		}
	}
}

// Spec §12 item 4 and Review Focus 1. The currency echo is a
// consistency check only: equal changes nothing, different is
// rejected with the text kept, and the value never decides the
// currency.
func TestCurrencyEchoMismatchKeepsText(t *testing.T) {
	p := form.Parse(post(url.Values{"Price": {"12.50"}, "Price.locale": {"en"}, "Price.currency": {"USD"}}),
		form.Field{Name: "Price", Kind: form.Money, Currency: "JPY", LocaleEcho: true})
	if p.Errors()["Price"] != "rastrillo.ui.money_currency_changed" {
		t.Fatalf("errors %v", p.Errors())
	}
	if _, ok := p.Minor("Price"); ok {
		t.Error("a mismatched currency produced an amount")
	}
	in, reseed := p.MoneyInput("Price", "en")
	if reseed || in != (form.MoneyInput{Value: "12.50", Locale: "en", Currency: "JPY"}) {
		t.Errorf("MoneyInput = %+v, %v; want the typed text, its locale and the server's currency", in, reseed)
	}
	// Spec §12 item 4: a forged currency equal to the server's changes
	// nothing (after Normalize: trimmed, upper case); any other is
	// rejected. The server's currency is what Minor's amount is in.
	for forged, accepted := range map[string]bool{"EUR": true, " eur ": true, "USD": false, "usd": false, "XXX": false, "": false} {
		p := form.Parse(post(url.Values{"Price": {"12.50"}, "Price.locale": {"en"}, "Price.currency": {forged}}),
			form.Field{Name: "Price", Kind: form.Money, Currency: "EUR", LocaleEcho: true})
		m, ok := p.Minor("Price")
		if ok != accepted || accepted && m != 1250 {
			t.Errorf("forged %q: %d, accepted=%v; want accepted=%v (1250 when accepted)", forged, m, ok, accepted)
		}
		if !accepted && p.Errors()["Price"] != "rastrillo.ui.money_currency_changed" {
			t.Errorf("forged %q: error %q", forged, p.Errors()["Price"])
		}
	}
	// Blank text needs no currency echo: an empty optional field has neither.
	p = form.Parse(post(url.Values{"Price": {"  "}, "Price.locale": {"en"}}),
		form.Field{Name: "Price", Kind: form.Money, Currency: "EUR", LocaleEcho: true})
	if _, ok := p.Minor("Price"); ok || !p.OK() {
		t.Errorf("blank optional: present=%v errors=%v", ok, p.Errors())
	}
}

func TestMoneyErrorsAreKeys(t *testing.T) {
	for _, c := range []struct {
		code, locale, text string
		strict, negative   bool
		want               string
	}{
		// Strict: today's errors.
		{"JPY", "en", "12.5", true, false, "rastrillo.ui.money_precision_0"},
		{"USD", "en", "12.345", true, false, "rastrillo.ui.money_precision"},
		{"KWD", "en", "1.2345", true, false, "rastrillo.ui.money_precision_3"},
		{"EUR", "de", "1.2,50", true, false, "rastrillo.ui.money_decimal_comma"},
		{"USD", "en", "1,2.50", true, false, "rastrillo.ui.money_decimal_point"},
		{"USD", "en", "US$ 5 £", true, false, "rastrillo.ui.money_currency"},
		{"USD", "en", "five", true, false, "rastrillo.ui.money_currency"},
		{"USD", "en", "-5", true, false, "rastrillo.ui.money_negative"},
		{"USD", "en", "-5", true, true, ""},
		// Normalising (the default): spec §5.1's examples.
		{"USD", "en", "€5", false, false, "rastrillo.ui.money_wrong_currency"},
		{"CAD", "en", "US$5", false, false, "rastrillo.ui.money_wrong_currency"},
		{"USD", "en", "1.500", false, false, "rastrillo.ui.money_ambiguous"},
		{"USD", "en", "1.234", false, false, "rastrillo.ui.money_ambiguous_precision"},
		{"JPY", "en", "1.500", false, false, "rastrillo.ui.money_ambiguous_precision"},
		{"USD", "en", "-1.234", false, true, "rastrillo.ui.money_ambiguous_precision"},
		{"USD", "en", "12.3456", false, false, "rastrillo.ui.money_precision"},
		{"USD", "en", "12,.50", false, false, "rastrillo.ui.money_decimal_point"},
		{"USD", "en", "(5)", false, false, "rastrillo.ui.money_negative"},
		{"USD", "en", "-(5)", false, false, "rastrillo.ui.money_invalid"},
		{"USD", "en", "5 dollars", false, false, "rastrillo.ui.money_invalid"},
		{"USD", "en", "US$ 1,284.5", false, false, ""},
		{"USD", "en", "\u200f \u00a0", false, false, "rastrillo.ui.field_required"}, // blank after Clean
	} {
		f := form.Field{Name: "A", Kind: form.Money, Currency: c.code, Locale: c.locale, Strict: c.strict, Negative: c.negative, Required: true}
		p := form.Parse(post(url.Values{"A": {c.text}}), f)
		if got := p.Errors()["A"]; got != c.want {
			t.Errorf("strict=%v %s %s %q: error %q, want %q", c.strict, c.code, c.locale, c.text, got, c.want)
		}
	}
	p := form.Parse(post(url.Values{"A": {"€5"}}), form.Field{Name: "A", Kind: form.Money, Currency: "USD", Locale: "en"})
	if fmt.Sprint(p.ErrorArgs()["A"]) != "map[currency:USD]" {
		t.Errorf("money_wrong_currency args %v; the message names the field's ISO code", p.ErrorArgs()["A"])
	}
	// money_ambiguous_precision names the reading that cannot be stored
	// as {a}, the other as {b}, and the currency whose exponent rules
	// {a} out (Appendix C.1).
	p = form.Parse(post(url.Values{"A": {"1.234"}}), form.Field{Name: "A", Kind: form.Money, Currency: "USD", Locale: "en"})
	if got := fmt.Sprint(p.ErrorArgs()["A"]); got != "map[a:1.2340 USD b:1234 USD currency:USD]" {
		t.Errorf("money_ambiguous_precision args %s", got)
	}
	// Negative, the over-precise reading is the largest, and it is still {a}.
	p = form.Parse(post(url.Values{"A": {"-1.234"}}), form.Field{Name: "A", Kind: form.Money, Currency: "USD", Locale: "en", Negative: true})
	if got := fmt.Sprint(p.ErrorArgs()["A"]); got != "map[a:-1.2340 USD b:-1234 USD currency:USD]" {
		t.Errorf("negative money_ambiguous_precision args %s", got)
	}
	p = form.Parse(post(url.Values{"A": {"5"}}), form.Field{Name: "A", Kind: form.Money, Currency: "USD", Locale: "en"})
	if _, ok := p.ErrorArgs()["A"]; ok || !p.OK() {
		t.Errorf("an accepted field carries arguments %v", p.ErrorArgs())
	}
}

// Spec §12 item 15 and the prototype's TestFieldStrictDispatch, through
// the field's whole POST path: the same posts with Strict set and unset.
func TestStrictAndNormalisingOnTheSamePosts(t *testing.T) {
	for _, c := range []struct {
		locale, text, code string
		strict             bool
		want               int64
		err                string // "" means accepted
	}{
		{"en", "US$ 5", "USD", true, 0, "rastrillo.ui.money_currency"},
		{"en", "US$ 5", "USD", false, 500, ""},
		{"en", "+5", "USD", true, 0, "rastrillo.ui.money_invalid"},
		{"en", "+5", "USD", false, 500, ""},
		{"en", "1.284,50", "USD", true, 0, "rastrillo.ui.money_decimal_point"},
		{"en", "1.284,50", "USD", false, 128450, ""},
		{"en", "€5", "USD", true, 0, "rastrillo.ui.money_currency"},
		{"en", "€5", "USD", false, 0, "rastrillo.ui.money_wrong_currency"},
		{"en", "1,500", "USD", true, 150000, ""},
		{"en", "12.5000", "USD", true, 0, "rastrillo.ui.money_precision"},
		{"en", "12.5000", "USD", false, 1250, ""},
		{"de", "1.500", "KWD", true, 1500000, ""},
		{"ar", "١٢\u066b٥٠", "SAR", true, 0, "rastrillo.ui.money_invalid"},
		{"ar", "١٢\u066b٥٠", "SAR", false, 1250, ""},
		{"en", "(5)", "USD", true, 0, "rastrillo.ui.money_invalid"},
		{"en", "-5", "USD", true, 0, "rastrillo.ui.money_negative"},
		{"en", "(5)", "USD", false, 0, "rastrillo.ui.money_negative"},
		{"en", "1.500", "KWD", false, 0, "rastrillo.ui.money_ambiguous"},
		{"de", "1.500", "KWD", false, 1500000, ""}, // decision 23
	} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: c.code, LocaleEcho: true, Strict: c.strict}
		p := form.Parse(post(url.Values{"P": {c.text}, "P.locale": {c.locale}, "P.currency": {c.code}}), f)
		m, _ := p.Minor("P")
		if got := p.Errors()["P"]; got != c.err || c.err == "" && m != c.want {
			t.Errorf("Strict=%v %s %q %s: %d, %q; want %d, %q", c.strict, c.locale, c.text, c.code, m, got, c.want, c.err)
		}
	}
	// A posted seed changes nothing on a Strict field: it renders none
	// and reads the text exactly.
	stored := int64(1500)
	f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Strict: true, Stored: &stored}
	p := form.Parse(post(url.Values{"P": {"9.999"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"9.999"}}), f)
	if m, ok := p.Minor("P"); !ok || m != 9999 {
		t.Errorf("strict field with a seed: %d, %v; want 9999", m, p.Errors())
	}
	if in, _ := form.SeedMoney("en", &stored, "KWD", true); in.Seed != "" {
		t.Errorf("a Strict field rendered a seed %q", in.Seed)
	}
}

// Spec §5.3: the unchanged-seed check. The seed is never a value: a
// match keeps the record's stored amount only if the record still
// renders to exactly that seed.
func TestUnchangedSeed(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	in, err := form.SeedMoney("en", i(1500), "KWD", false)
	if err != nil || in.Value != "1.500" || in.Seed != "1.500" {
		t.Fatalf("en seed %+v, %v", in, err)
	}
	if in, _ := form.SeedMoney("de", i(1500), "KWD", false); in.Seed != "1,500" {
		t.Fatalf("de seed %+v", in)
	}
	for _, c := range []struct {
		name                         string
		text, locale, currency, seed string
		stored                       *int64
		want                         int64
		err                          string
	}{
		{"unchanged, en", "1.500", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"unchanged, de", "1,500", "de", "KWD", "1,500", i(1500), 1500, ""},
		{"unchanged, trimmed", " 1.500\t", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"no seed: the rule applies", "1.500", "en", "KWD", "", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"edited to the same digits differently", "1.5", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"edited, ambiguous", "2.500", "en", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"a bidi mark is not trimming", "1.500\u200f", "en", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"tampered: seed forged to the text", "9.999", "en", "KWD", "9.999", i(1500), 0, "rastrillo.ui.money_reenter"},
		{"tampered: seed in another locale", "1,500", "en", "KWD", "1,500", i(1500), 0, "rastrillo.ui.money_reenter"},
		{"record changed since the form was drawn", "1.500", "en", "KWD", "1.500", i(2000), 0, "rastrillo.ui.money_reenter"},
		{"a seed on create was not ours", "1.500", "en", "KWD", "1.500", nil, 0, "rastrillo.ui.money_reenter"},
		{"any seed on create", "2", "en", "KWD", "1.500", nil, 0, "rastrillo.ui.money_reenter"},
		{"no seed on create: the rule applies", "2", "en", "KWD", "", nil, 2000, ""},
		{"currency echo still checked first", "1.500", "en", "USD", "1.500", i(1500), 0, "rastrillo.ui.money_currency_changed"},
		{"locale echo still checked first", "1.500", "EN", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_reenter"},
	} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Stored: c.stored}
		p := form.Parse(post(url.Values{"P": {c.text}, "P.locale": {c.locale}, "P.currency": {c.currency}, "P.seed": {c.seed}}), f)
		m, _ := p.Minor("P")
		if got := p.Errors()["P"]; got != c.err || c.err == "" && m != c.want {
			t.Errorf("%s: %d, %q; want %d, %q", c.name, m, got, c.want, c.err)
		}
	}
	// Paul's ruling on review finding M5: a stored refund saves
	// unchanged whatever the field's negative policy, because the
	// verified seed is the record's own amount, not new input. A legacy
	// refund in a generated resource, whose fields never allow
	// negatives, could otherwise not be edited at all until the amount
	// changed. A changed amount is input, and the policy applies to it.
	for _, c := range []struct {
		name     string
		text     string
		negative bool
		want     string
		minor    int64
	}{
		{"unchanged, Negative false", "-1.500", false, "", -1500},
		{"unchanged, Negative true", "-1.500", true, "", -1500},
		{"changed, Negative false", "-2.5", false, "rastrillo.ui.money_negative", 0},
		{"changed, Negative true", "-2.5", true, "", -2500},
	} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Negative: c.negative, Stored: i(-1500)}
		p := form.Parse(post(url.Values{"P": {c.text}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"-1.500"}}), f)
		m, _ := p.Minor("P")
		if got := p.Errors()["P"]; got != c.want || c.want == "" && m != c.minor {
			t.Errorf("negative seed, %s: %d, %q; want %d, %q", c.name, m, got, c.minor, c.want)
		}
	}
}

// Spec §5.3 (c62d25e6) and the prototype's TestFieldSeedSurvivesEcho:
// an error on another field must not turn an unchanged ambiguous value
// into money_ambiguous on the second submission, and every submission
// re-verifies the seed against the current record.
func TestSeedSurvivesAFailureElsewhere(t *testing.T) {
	stored := int64(1500) // 1.500 KWD
	fields := func(st *int64) []form.Field {
		return []form.Field{{Name: "Name", Required: true},
			{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Stored: st}}
	}
	first := url.Values{"Name": {""}, "P": {"1.500"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"1.500"}}
	p := form.Parse(post(first), fields(&stored)...)
	if p.Errors()["Name"] == "" || p.Errors()["P"] != "" {
		t.Fatalf("first submission: %v", p.Errors())
	}
	in, reseed := p.MoneyInput("P", "en")
	if reseed || in != (form.MoneyInput{Value: "1.500", Locale: "en", Currency: "KWD", Seed: "1.500"}) {
		t.Fatalf("echo %+v, %v; want the text, its locale and the verified seed", in, reseed)
	}
	second := url.Values{"Name": {"Fee"}, "P": {in.Value}, "P.locale": {in.Locale}, "P.currency": {in.Currency}, "P.seed": {in.Seed}}
	if m, ok := form.Parse(post(second), fields(&stored)...).Minor("P"); !ok || m != 1500 {
		t.Errorf("second submission: %d, %v; want 1500", m, ok)
	}
	changed := int64(2000)
	if got := form.Parse(post(second), fields(&changed)...).Errors()["P"]; got != "rastrillo.ui.money_reenter" {
		t.Errorf("second submission after the record changed: %q; want money_reenter", got)
	}
	// Edited text and a forged seed are echoed with no seed.
	for _, v := range []url.Values{
		{"Name": {""}, "P": {"2.500"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"1.500"}},
		{"Name": {""}, "P": {"9.999"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"9.999"}},
	} {
		if in, _ := form.Parse(post(v), fields(&stored)...).MoneyInput("P", "en"); in.Seed != "" {
			t.Errorf("text %q echoed seed %q", v.Get("P"), in.Seed)
		}
	}
}

func TestSeedMoney(t *testing.T) {
	m := int64(-128450)
	in, err := form.SeedMoney("de", &m, "EUR", false)
	if err != nil || in != (form.MoneyInput{Value: "-1284,50", Locale: "de", Currency: "EUR", Seed: "-1284,50"}) {
		t.Errorf("SeedMoney = %+v, %v", in, err)
	}
	if in, _ := form.SeedMoney("en", nil, "EUR", false); in.Value != "" || in.Seed != "" || in.Currency != "EUR" {
		t.Errorf("nil seed = %+v; an empty field shows the currency it will be saved in", in)
	}
	// A render locale that is not itself a shipped id (de-DE resolves to
	// de) must not reach the hidden input as typed: the locale check
	// would refuse it, and every save of an untouched form would ask
	// for the amount again.
	m = 1500
	in, err = form.SeedMoney("de-DE", &m, "KWD", false)
	if err != nil {
		t.Fatal(err)
	}
	p := form.Parse(post(url.Values{"P": {in.Value}, "P.locale": {in.Locale}, "P.currency": {in.Currency}, "P.seed": {in.Seed}}),
		form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Stored: &m})
	if got, ok := p.Minor("P"); !ok || got != 1500 {
		t.Errorf("seeded in de-DE as %+v, saved %d, %v", in, got, p.Errors())
	}
	if _, err := form.SeedMoney("en", &m, "XX", false); err == nil {
		t.Error("a malformed currency seeded an input")
	}
}

// moneyErrorKey has precision keys for exponents 0, 2 and 3 only; any
// other would fall back to "no more than 2 decimal places" and tell a
// person the wrong number.
func TestPrecisionKeysCoverEveryExponent(t *testing.T) {
	for _, code := range locales.StorageCodes() {
		if exp, err := currency.Exponent(code); err != nil || exp != 0 && exp != 2 && exp != 3 {
			t.Errorf("%s has exponent %d (%v): its precision error needs an approved key of its own", code, exp, err)
		}
	}
}

// Decision 4: the cents API is untouched.
func TestCentsPathUnchanged(t *testing.T) {
	p := form.Parse(post(url.Values{"P": {"12.34"}}), form.Field{Name: "P", Kind: form.Money})
	if p.Cents("P") != 1234 || !p.OK() {
		t.Errorf("legacy Money: %d %v", p.Cents("P"), p.Errors())
	}
}
