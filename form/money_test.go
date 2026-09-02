package form

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCents(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"12.34", 1234, false},
		{"12", 1200, false},
		{"12.3", 1230, false},
		{".5", 50, false},
		{"", 0, false},      // blank = zero; Required rejects earlier on raw text
		{"12.345", 0, true}, // >2 decimal places
		{"-1.50", 0, true},  // sign refused outright (v1: no negative money)
		{"12.-5", 0, true},  // the strconv leniency hole, closed
		{"12.+5", 0, true},
		{"$12.34", 0, true}, // ParseCents rejects "$": FormatCentsPlain seeds forms
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := ParseCents(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseCents(%q) = %d, <nil>; want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCents(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseCents(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFormatCentsNegative(t *testing.T) {
	// FormatCents(-150) == "-$1.50" (not "$-1.-50" — the truncation
	// mangling the stamped comment documents).
	if got, want := FormatCents(-150), "-$1.50"; got != want {
		t.Errorf("FormatCents(-150) = %q, want %q", got, want)
	}
	if got, want := FormatCents(150), "$1.50"; got != want {
		t.Errorf("FormatCents(150) = %q, want %q", got, want)
	}
}

func TestFormatCentsPlainRoundTrip(t *testing.T) {
	if got, want := FormatCentsPlain(150), "1.50"; got != want {
		t.Errorf("FormatCentsPlain(150) = %q, want %q", got, want)
	}
	// ParseCents(FormatCentsPlain(150)) round-trips to 150 — the resubmit rule.
	got, err := ParseCents(FormatCentsPlain(150))
	if err != nil {
		t.Fatalf("ParseCents(FormatCentsPlain(150)) unexpected error: %v", err)
	}
	if got != 150 {
		t.Errorf("ParseCents(FormatCentsPlain(150)) = %d, want 150", got)
	}
}

// Every error ParseCents can return names a catalog key, and no message
// it can produce says "dollar".
//
// The key is what makes the message translatable from a package that
// cannot translate it itself: this package imports nothing from
// rastrillo, so it hands the caller a key and an English fallback and
// lets whoever holds a translator decide. A caller that ignores the key
// still gets a sentence, which is why the fallback is asserted too.
//
// The word check is not pedantry. These strings reach a person filling
// in a form, in a framework that ships twelve languages, and telling
// someone in Tokyo to enter a dollar amount was wrong in all of them.
// The framework stores no currency anywhere, so this package is in no
// position to name one.
func TestParseCentsErrorsAreTranslatableAndCurrencyFree(t *testing.T) {
	for _, in := range []string{"1.234", "abc", "1.-5", "-1.00", "12.+5"} {
		_, err := ParseCents(in)
		if err == nil {
			t.Errorf("ParseCents(%q) returned no error; this test is asserting on the errors and one of its inputs stopped producing one", in)
			continue
		}
		var fe *Error
		if !errors.As(err, &fe) {
			t.Errorf("ParseCents(%q) returned %T, which carries no catalog key, so nothing can translate it", in, err)
			continue
		}
		if !strings.HasPrefix(fe.Key, "rastrillo.ui.") {
			t.Errorf("ParseCents(%q) names the key %q; a catalog key has to be under rastrillo.ui.", in, fe.Key)
		}
		if fe.Msg == "" || fe.Error() != fe.Msg {
			t.Errorf("ParseCents(%q) has Msg %q and Error() %q; a caller that ignores the key must still get a sentence", in, fe.Msg, fe.Error())
		}
		if strings.Contains(strings.ToLower(fe.Error()), "dollar") {
			t.Errorf("ParseCents(%q) says %q. This package has no currency to name and neither does the framework", in, fe.Error())
		}
	}
}

// The two keys are the ones the catalogs carry. Written out rather than
// read off the errors, so renaming a key here fails until the catalogs
// are renamed with it.
func TestMoneyErrorKeysAreTheOnesTheCatalogCarries(t *testing.T) {
	_, err := ParseCents("1.234")
	var fe *Error
	if !errors.As(err, &fe) || fe.Key != "rastrillo.ui.money_precision" {
		t.Errorf("too many decimal places names %v, want rastrillo.ui.money_precision", err)
	}
	_, err = ParseCents("abc")
	if !errors.As(err, &fe) || fe.Key != "rastrillo.ui.money_invalid" {
		t.Errorf("an unparseable amount names %v, want rastrillo.ui.money_invalid", err)
	}
}
