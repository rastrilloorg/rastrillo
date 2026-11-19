package pdf

import (
	"bytes"
	"testing"
	"unicode/utf16"
)

// A PageBreak called last leaves the flow's current page empty: before
// the fix, Render appended it anyway, producing a trailing sheet that
// carries only the running footer — which looks like a page that
// failed to print, the exact failure newPage's own empty-page guard
// exists to stop everywhere else.
func TestRenderDropsATrailingEmptyPageFromAFinalPageBreak(t *testing.T) {
	b := New(opts())
	b.Paragraph("Body.")
	b.PageBreak()
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Errorf("want the trailing break to cost nothing, got %d pages", pageCount(bs))
	}
}

// The specific case Produced exists to rule out: with a trailing
// PageBreak, the blank page that used to survive is exactly where
// Produced's line would land — alone, on a page holding nothing else
// of the document.
func TestProducedDoesNotLandOnATrailingEmptyPage(t *testing.T) {
	b := New(opts())
	b.Paragraph("Body.")
	b.PageBreak()
	b.Produced("a member", "today", "")
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Errorf("want 1 page, got %d", pageCount(bs))
	}
	if n := bytes.Count(bs, []byte("(Produced by a member on today.) Tj")); n != 1 {
		t.Errorf("Produced line drawn %d times, want 1", n)
	}
}

// A document that is genuinely empty (nothing drawn at all) must still
// render one page — the first-page case the fix above must not break,
// since there is no "previous real page" for an all-blank document to
// be a trailing, discardable copy of.
func TestRenderOfAnEmptyDocumentStillProducesOnePage(t *testing.T) {
	b := New(opts())
	bs, _, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if pageCount(bs) != 1 {
		t.Errorf("want 1 page for an empty document, got %d", pageCount(bs))
	}
}

// Title and Author are metadata, not page content, and a PDF reader
// decodes an Info dictionary string with no byte-order mark as
// PDFDocEncoding — not Windows-1252, which is what the page content
// stream (correctly) uses. The two disagree in the 0x80-0x9F range:
// writing Title the same way the page content is written put an em
// dash and an ellipsis in a real reader's window title as "Š" and "…"
// read back wrong. UTF-16BE with its BOM is unambiguous either way,
// and happens to have no undrawable character for these two fields at
// all.
func TestTitleAndAuthorSurviveAsUTF16(t *testing.T) {
	title := "Receipts — the year's ‘total’…"
	author := "Example € Ltd"
	b := New(Options{Produced: opts().Produced, Title: title, Author: author})
	b.Paragraph("x")
	bs, dropped, err := b.Render()
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 0 {
		t.Errorf("want nothing reported as undrawable in Title/Author, got %q", string(dropped))
	}

	want := append([]byte{0xFE, 0xFF}, utf16BE(title)...)
	i := bytes.Index(bs, []byte("/Title ("))
	if i < 0 {
		t.Fatal("/Title not found")
	}
	got := decodeLiteral(t, bs[i+len("/Title ("):])
	if !bytes.Equal(got, want) {
		t.Errorf("/Title bytes = %x, want %x (UTF-16BE with BOM)", got, want)
	}

	wantAuthor := append([]byte{0xFE, 0xFF}, utf16BE(author)...)
	j := bytes.Index(bs, []byte("/Author ("))
	if j < 0 {
		t.Fatal("/Author not found")
	}
	gotAuthor := decodeLiteral(t, bs[j+len("/Author ("):])
	if !bytes.Equal(gotAuthor, wantAuthor) {
		t.Errorf("/Author bytes = %x, want %x (UTF-16BE with BOM)", gotAuthor, wantAuthor)
	}
}

func utf16BE(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// decodeLiteral reverses escape(): it reads a PDF literal string's
// octal and backslash escapes back to raw bytes, stopping at the
// closing unescaped ')'.
func decodeLiteral(t *testing.T, b []byte) []byte {
	t.Helper()
	var out []byte
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == ')':
			return out
		case b[i] == '\\' && i+3 < len(b) && b[i+1] >= '0' && b[i+1] <= '7':
			v := (int(b[i+1]-'0') << 6) | (int(b[i+2]-'0') << 3) | int(b[i+3]-'0')
			out = append(out, byte(v))
			i += 3
		case b[i] == '\\':
			out = append(out, b[i+1])
			i++
		default:
			out = append(out, b[i])
		}
	}
	t.Fatal("literal never closed")
	return nil
}
