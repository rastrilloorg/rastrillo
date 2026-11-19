package form_test

import (
	"fmt"
	"net/url"
	"testing"

	"amadan.net/rastrillo/rastrillo/form"
	"amadan.net/rastrillo/rastrillo/number"
)

var seats = form.Field{Name: "Seats", Kind: form.Number, LocaleEcho: true,
	Required: true}

func postSeats(text, locale string) url.Values {
	return url.Values{"Seats": {text}, "Seats.locale": {locale}}
}

// The text is read in the locale the page was drawn in, which the form
// echoes, as money's is: "1.500" is 1500 from a German page and two
// readings from an English one.
func TestNumberReadsInTheEchoedLocale(t *testing.T) {
	for _, each := range []struct {
		text, locale string
		want         number.Decimal
	}{
		{"1.500", "de", "1500"},
		{"1,500", "en", "1500"},
		{"1.200,5", "de", "1200.5"},
		{"1200,5", "en", "1200.5"},
		{"-12", "en", ""},
	} {
		parsed := form.Parse(post(postSeats(each.text, each.locale)), seats)
		got, present := parsed.Number("Seats")
		if got != each.want || present != (each.want != "") {
			t.Errorf("%s %q: %q, %v, %v", each.locale, each.text, got,
				present, parsed.Errors())
		}
	}
}

func TestNumberRefusalsAreKeysWithArguments(t *testing.T) {
	for _, each := range []struct {
		text, locale, key, args string
	}{
		{"1.500", "en", "rastrillo.ui.number_ambiguous", "map[a:1.5 b:1500]"},
		{"1,500", "de", "rastrillo.ui.number_ambiguous", "map[a:1,5 b:1500]"},
		{"1e3", "de", "rastrillo.ui.number_invalid", "map[example:1200,5]"},
		{"twelve", "en", "rastrillo.ui.number_invalid",
			"map[example:1200.5]"},
		{"1234567890123456789012345678901", "en",
			"rastrillo.ui.number_too_long", "map[]"},
		{"-5", "en", "rastrillo.ui.number_negative", "map[]"},
		{"", "en", "rastrillo.ui.field_required", "map[]"},
	} {
		parsed := form.Parse(post(postSeats(each.text, each.locale)), seats)
		args := fmt.Sprint(parsed.ErrorArgs()["Seats"])
		if parsed.Errors()["Seats"] != each.key || args != each.args {
			t.Errorf("%s %q: %v %s, want %s %s", each.locale, each.text,
				parsed.Errors(), args, each.key, each.args)
		}
		if _, present := parsed.Number("Seats"); present {
			t.Errorf("%s %q: a refused number is present", each.locale,
				each.text)
		}
	}
}

// A locale echo that names no locale this binary draws, or is missing
// or repeated, asks for the number again rather than guess.
func TestNumberRefusesABadEchoAndReseeds(t *testing.T) {
	for name, values := range map[string]url.Values{
		"missing":  {"Seats": {"12"}},
		"repeated": {"Seats": {"12"}, "Seats.locale": {"en", "de"}},
		"unknown":  postSeats("12", "xx-Nope"),
		"unshaped": postSeats("12", "de_DE"),
	} {
		parsed := form.Parse(post(values), seats)
		in, reseed := parsed.NumberInput("Seats", "de")
		if parsed.Errors()["Seats"] != "rastrillo.ui.number_reenter" ||
			!reseed || in.Value != "" || in.Locale != "de" {
			t.Errorf("%s: %v, %+v, %v", name, parsed.Errors(), in, reseed)
		}
	}
	parsed := form.Parse(post(postSeats("1.500", "en")), seats)
	in, reseed := parsed.NumberInput("Seats", "de")
	if reseed || in.Value != "1.500" || in.Locale != "en" {
		t.Errorf("a refused reading echoes the text: %+v, %v", in, reseed)
	}
}

// An ambiguous seed the person left alone saves as it was: German
// "1,234" is 1.234 drawn, and two readings typed fresh.
func TestAnUnchangedNumberSeedKeepsTheValue(t *testing.T) {
	stored := number.Decimal("1.234")
	edit := seats
	edit.StoredNumber = &stored
	seed, err := form.SeedNumber("de-DE", &stored)
	if err != nil || seed.Value != "1,234" || seed.Locale != "de" {
		t.Fatalf("SeedNumber = %+v, %v", seed, err)
	}
	parsed := form.Parse(post(postSeats(" 1,234 ", "de")), edit)
	if got, present := parsed.Number("Seats"); got != stored || !present {
		t.Errorf("unchanged seed: %q, %v, %v", got, present, parsed.Errors())
	}
	parsed = form.Parse(post(postSeats("1,234", "de")), seats)
	if parsed.Errors()["Seats"] != "rastrillo.ui.number_ambiguous" {
		t.Errorf("the same text with no stored value: %v", parsed.Errors())
	}
	parsed = form.Parse(post(postSeats("1,2345", "de")), edit)
	if got, _ := parsed.Number("Seats"); got != "1.2345" {
		t.Errorf("an edited seed is read: %q, %v", got, parsed.Errors())
	}
}

func TestStrictNumberReadsOnlyTheLocalesFormat(t *testing.T) {
	strict := seats
	strict.Strict, strict.Negative = true, true
	parsed := form.Parse(post(postSeats("1200,5", "en")), strict)
	if parsed.Errors()["Seats"] != "rastrillo.ui.number_invalid" {
		t.Errorf("strict en 1200,5: %v", parsed.Errors())
	}
	parsed = form.Parse(post(postSeats("-1.500", "en")), strict)
	if got, _ := parsed.Number("Seats"); got != "-1.5" {
		t.Errorf("strict en -1.500: %q, %v", got, parsed.Errors())
	}
}

// Without the echo, Field.Locale names the locale: an API client or a
// hand-written handler owns that choice.
func TestNumberWithoutTheEchoReadsFieldLocale(t *testing.T) {
	field := form.Field{Name: "Seats", Kind: form.Number, Locale: "de"}
	parsed := form.Parse(post(url.Values{"Seats": {"1.500"}}), field)
	if got, _ := parsed.Number("Seats"); got != "1500" {
		t.Errorf("de 1.500: %q, %v", got, parsed.Errors())
	}
	parsed = form.Parse(post(url.Values{"Seats": {""}}), field)
	if _, present := parsed.Number("Seats"); present || !parsed.OK() {
		t.Errorf("an optional blank number: %v, %v", present, parsed.Errors())
	}
}
