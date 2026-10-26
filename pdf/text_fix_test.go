package pdf

import (
	"strings"
	"testing"
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
