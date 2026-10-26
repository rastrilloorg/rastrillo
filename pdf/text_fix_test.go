package pdf

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A long token is cut where it stops fitting, and the cut must fall
// between characters. Cutting by byte split an em dash or an accented
// letter in two, and each half drew as a question mark.
func TestAHardBreakFallsBetweenRunes(t *testing.T) {
	token := strings.Repeat("IN—RE—SMITH—É—", 12)
	for max := 20.0; max < 200; max += 7 {
		lines := wrap(token, faceRegular, sizeBody, max)
		for _, l := range lines {
			if !utf8.ValidString(l) {
				t.Fatalf("width %.0f: line %q is not valid UTF-8", max, l)
			}
			if strings.ContainsRune(l, utf8.RuneError) {
				t.Fatalf("width %.0f: line %q carries a replacement character", max, l)
			}
		}
		if strings.Join(lines, "") != token {
			t.Fatalf("width %.0f: the lines do not reassemble the token: %q", max, lines)
		}
	}
}

// The footer's left text is the title, an em dash and the reference, so
// the dash is always in the string that gets trimmed.
func TestAFooterTrimFallsBetweenRunes(t *testing.T) {
	s := "Progress report — " + strings.Repeat("É—", 80)
	for max := 30.0; max < 400; max += 11 {
		got := trim(s, faceRegular, sizeSmall, max)
		if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
			t.Fatalf("width %.0f: %q", max, got)
		}
		if measure(got, faceRegular, sizeSmall) > max {
			t.Fatalf("width %.0f: %q still does not fit", max, got)
		}
	}
}

// What the footer cannot draw is reported like anything else: the
// footer used to append its ops past f.text, so a character the
// typeface lacks appeared as a question mark on every page and in the
// dropped set nowhere.
func TestAnUndrawableCharacterInTheFooterIsReported(t *testing.T) {
	o := opts()
	o.Reference = "ΔCVL-1" // Greek capital delta: not in Windows-1252
	o.Title = "Progress report"
	b := New(o)
	addSample(b, 3)
	_, undrawable, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range undrawable {
		if r == 'Δ' {
			found = true
		}
	}
	if !found {
		t.Errorf("Δ in the reference is drawn as ? on every page and was not reported: %q", string(undrawable))
	}
}

// wrap's hard-break loop used to run forever once a token's remaining
// runes measured zero width against a non-positive max: "" is never
// wider than anything, including a negative number, so a width-only
// loop condition could never see it as narrow enough to stop. A column
// requested narrower than its own 8pt padding, or a label wider than
// the space left for it, reach exactly this with ordinary caller data
// (see TestLayoutFieldsOfAPathologicallyLongLabelTerminates and
// TestLayoutTableOfPathologicallyNarrowColumnsTerminates), not just a
// contrived call to Wrap directly.
func TestWrapTerminatesForNonPositiveWidth(t *testing.T) {
	done := make(chan []string, 1)
	go func() { done <- wrap("hello world", faceRegular, sizeBody, -5) }()
	select {
	case lines := <-done:
		if strings.Join(lines, "") != "helloworld" {
			t.Errorf("want every rune preserved, got %q", lines)
		}
		for _, l := range lines {
			if len([]rune(l)) > 1 {
				t.Errorf("a negative width should give one rune per line, got %q", l)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wrap did not return within 2s for a negative max — it is looping")
	}
}

// Exactly 0 is the boundary the fix's condition (len(rs) > 0) has to
// get right in the other direction: it must still wrap normally, one
// rune per line, rather than stopping early and dropping the rest of
// the token.
func TestWrapAtExactlyZeroWidthStillCoversEveryRune(t *testing.T) {
	lines := wrap("abc", faceRegular, sizeBody, 0)
	if strings.Join(lines, "") != "abc" {
		t.Errorf("want every rune preserved, got %q", lines)
	}
}

// A non-breaking space exists so two words stay on one line together.
// unicode.IsSpace says yes to it like any other space, so without an
// exception wrap split there anyway — the glyph survived (it draws as
// an ordinary space) but the one reason anyone typed it did not.
func TestWrapDoesNotBreakAtANonBreakingSpace(t *testing.T) {
	s := "Total £1,000,000"
	// Wide enough to hold the whole string on one line (it measures
	// well under 100pt at this size): if the non-breaking space were
	// still a break point, two ordinary words that both fit alone
	// would still end up on separate lines.
	lines := wrap(s, faceRegular, sizeBody, 150)
	if len(lines) != 1 {
		t.Errorf("want the non-breaking space to keep both halves on one line, got %q", lines)
	}
}
