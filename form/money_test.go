package form

import "testing"

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
