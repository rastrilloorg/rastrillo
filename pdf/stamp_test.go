package pdf

import (
	"bytes"
	"strings"
	"testing"
)

// An approval stamp is drawn on every page, in red, and a document
// rendered without one carries no red at all — so the stamp is the
// only thing that puts it there.
func TestAStampIsDrawnInRedOnEveryPage(t *testing.T) {
	plain, _, err := sample(140).Render()
	if err != nil {
		t.Fatal(err)
	}
	o := opts()
	o.Stamp = &Stamp{Head: "APPROVED", Line: "A Manager, 17 Sep 2026"}
	b := New(o)
	addSample(b, 140)
	stamped, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	pages := pageCount(stamped)
	if pages < 2 {
		t.Fatalf("the sample laid out on %d page, so every page was not exercised", pages)
	}
	if got := bytes.Count(stamped, []byte("(APPROVED) Tj")); got != pages {
		t.Errorf("APPROVED is drawn %d times on %d pages", got, pages)
	}
	if got := bytes.Count(stamped, []byte("(A Manager, 17 Sep 2026) Tj")); got != pages {
		t.Errorf("who and when are drawn %d times on %d pages", got, pages)
	}
	if !strings.Contains(string(stamped), " rg ") {
		t.Error("the stamp is not in colour")
	}
	if strings.Contains(string(plain), " rg ") || strings.Contains(string(plain), "APPROVED") {
		t.Error("a document with no stamp carries one")
	}
	if pageCount(plain) != pages {
		t.Errorf("stamping moved the layout: %d pages unstamped, %d stamped", pageCount(plain), pages)
	}
}
