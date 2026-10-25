package shape

import (
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// TestSearchBind pins that search binds only when there is a column to
// search: a list of money columns with search on has no LIKE clause,
// so a caller that passed a search would not compile against the store.
func TestSearchBind(t *testing.T) {
	text := rastrillo.Resource{List: rastrillo.List{Search: true, Columns: []rastrillo.Column{{Field: "Title", Kind: rastrillo.Text}}}}
	money := rastrillo.Resource{List: rastrillo.List{Search: true, Columns: []rastrillo.Column{{Field: "Price", Kind: rastrillo.Money}}}}
	off := rastrillo.Resource{List: rastrillo.List{Columns: []rastrillo.Column{{Field: "Title", Kind: rastrillo.Text}}}}
	if !SearchBind(text) || SearchBind(money) || SearchBind(off) {
		t.Fatalf("SearchBind = %v, %v, %v; want true, false, false", SearchBind(text), SearchBind(money), SearchBind(off))
	}
	if n := CountBinds(rastrillo.Resource{Scope: rastrillo.UserScoped, List: text.List}); n != 2 {
		t.Fatalf("CountBinds(scoped, searched) = %d, want 2", n)
	}
}

// TestWrapComment pins the width rule at both edges: a word that fits
// exactly stays, one over goes to the next line.
func TestWrapComment(t *testing.T) {
	if got, want := WrapComment("aaaa bbbb cccc", 9), "// aaaa bbbb\n// cccc\n"; got != want {
		t.Fatalf("WrapComment at 9 = %q, want %q", got, want)
	}
	if got, want := WrapComment("aaaa bbbb cccc", 8), "// aaaa\n// bbbb\n// cccc\n"; got != want {
		t.Fatalf("WrapComment at 8 = %q, want %q", got, want)
	}
}
