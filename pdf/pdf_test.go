package pdf

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A fixed instant, so the tests say what they mean about determinism
// rather than about the clock.
var produced = time.Date(2026, 8, 24, 11, 30, 0, 0, time.UTC)

func opts() Options {
	return Options{Produced: produced, Title: "Progress report", Reference: "CVL-2026-0001", Author: "Example LLP"}
}

// addSample adds a representative mix of blocks — one of everything
// Builder draws — to b. Split from sample so a test that needs its own
// Options (the stamp and undrawable-footer tests) can still build the
// same content.
func addSample(b *Builder, rows int) {
	b.Heading(1, "The appointment")
	b.Fields("", []Field{
		{Label: "Office-holder", Value: "A Practitioner"},
		{Label: "Appointed", Value: "14 March 2026"},
		{Label: "What remains to be done", Value: "Realise the book debts.", Source: "A Practitioner"},
	})
	b.Paragraph(strings.Repeat("This paragraph exists to run over more than one line so that wrapping is exercised. ", 6))

	body := make([][]Cell, 0, rows)
	for i := 0; i < rows; i++ {
		body = append(body, []Cell{
			{Text: fmt.Sprintf("Realisation %d", i+1)},
			{Text: "2026-03-14"},
			{Text: fmt.Sprintf("%d,%03d.00", i+1, i*7%1000)},
		})
	}
	b.Table("Receipts and payments",
		[]Column{
			{Title: "Item", Width: 5},
			{Title: "Date", Width: 2},
			{Title: "Amount", Align: "right", Width: 2},
		},
		body,
		[]Cell{{Text: "Total", Bold: true}, {Text: ""}, {Text: "12,345.00", Bold: true}})
	b.List("Outstanding", []string{"One", "Two"})
	b.Callout("Not computed", "The prescribed part has not been estimated on this case.", true)
}

func sample(rows int) *Builder {
	b := New(opts())
	addSample(b, rows)
	return b
}

func render(t *testing.T, b *Builder) []byte {
	t.Helper()
	bs, dropped, err := b.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("unexpected undrawable characters: %q", string(dropped))
	}
	return bs
}

// The determinism claim in the package doc, asserted rather than
// described. It is the whole argument for rendering in process: a
// document can be regenerated and compared byte for byte, which
// nothing that shells out to a browser could promise.
func TestRenderIsDeterministic(t *testing.T) {
	a := render(t, sample(40))
	b := render(t, sample(40))
	if !bytes.Equal(a, b) {
		t.Fatalf("two renders of one document differ: %d bytes vs %d", len(a), len(b))
	}
}

// The cross-reference table is where a hand-written PDF goes wrong, and
// it goes wrong silently: a reader that can repair the file opens it
// anyway, so a viewer opening the document proves nothing. This test
// computes the object offsets independently of the writer — by reading
// the table and looking at what is actually at each byte — which is the
// house rule that a parser must be checked against ground truth
// somebody else computed rather than against its author's own
// expectations.
func TestCrossReferenceOffsetsPointAtTheirObjects(t *testing.T) {
	b := render(t, sample(60))

	i := bytes.LastIndex(b, []byte("startxref\n"))
	if i < 0 {
		t.Fatal("no startxref")
	}
	var start int
	if _, err := fmt.Sscanf(string(b[i+len("startxref\n"):]), "%d", &start); err != nil {
		t.Fatalf("startxref unreadable: %v", err)
	}
	if start <= 0 || start >= len(b) {
		t.Fatalf("startxref %d is outside a %d byte file", start, len(b))
	}
	if !bytes.HasPrefix(b[start:], []byte("xref\n")) {
		t.Fatalf("startxref points at %q, not at the table", b[start:min(start+16, len(b))])
	}

	lines := strings.Split(string(b[start:]), "\n")
	var count int
	if _, err := fmt.Sscanf(lines[1], "0 %d", &count); err != nil {
		t.Fatalf("subsection header %q unreadable: %v", lines[1], err)
	}
	if count < 8 {
		t.Fatalf("only %d objects; the fixture should produce more", count)
	}
	for n := 1; n < count; n++ {
		// lines[0] is "xref", lines[1] the subsection header and lines[2]
		// the free entry for object 0, so object n is at 2+n.
		entry := lines[2+n]
		off, err := strconv.Atoi(strings.TrimSpace(entry[:10]))
		if err != nil {
			t.Fatalf("object %d entry %q unreadable: %v", n, entry, err)
		}
		want := fmt.Sprintf("%d 0 obj", n)
		if off >= len(b) || !bytes.HasPrefix(b[off:], []byte(want)) {
			got := ""
			if off < len(b) {
				got = string(b[off:min(off+24, len(b))])
			}
			t.Errorf("object %d: table says byte %d, which holds %q, not %q", n, off, got, want)
		}
	}
}

// A stream whose declared length does not match its bytes is the other
// way a hand-written PDF breaks, and a lenient reader hides it too.
func TestStreamLengthsAreDeclaredCorrectly(t *testing.T) {
	b := render(t, sample(30))
	re := regexp.MustCompile(`(?s)<< /Length (\d+) >>\nstream\n`)
	locs := re.FindAllSubmatchIndex(b, -1)
	if len(locs) == 0 {
		t.Fatal("no content streams")
	}
	for _, loc := range locs {
		declared, err := strconv.Atoi(string(b[loc[2]:loc[3]]))
		if err != nil {
			t.Fatalf("length unreadable: %v", err)
		}
		body := b[loc[1]:]
		end := bytes.Index(body, []byte("\nendstream"))
		if end < 0 {
			t.Fatal("stream never ends")
		}
		if end != declared {
			t.Errorf("stream declares %d bytes and holds %d", declared, end)
		}
	}
}

func pageCount(b []byte) int {
	re := regexp.MustCompile(`/Count (\d+) >>`)
	m := re.FindSubmatch(b)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

// A table longer than a page has to break, and the header has to come
// back at the top of each part. A continued table with no header is a
// column of numbers a reader cannot name.
func TestLongTableBreaksAndRepeatsItsHeader(t *testing.T) {
	b := render(t, sample(200))
	pages := pageCount(b)
	if pages < 4 {
		t.Fatalf("200 rows produced %d pages; expected the table to break", pages)
	}
	headers := bytes.Count(b, []byte("(Receipts and payments) Tj"))
	if headers != 1 {
		t.Errorf("the table's title was drawn %d times; it belongs once", headers)
	}
	// The column headings, unlike the title, repeat on every part.
	repeats := bytes.Count(b, []byte("(Item) Tj"))
	if repeats < 4 {
		t.Errorf("column headings drawn %d times across %d pages; expected one per part", repeats, pages)
	}
}

// Every page says which page it is and how many there are, because a
// reader holding a loose sheet has to be able to tell that something is
// missing.
func TestEveryPageIsNumbered(t *testing.T) {
	b := render(t, sample(120))
	pages := pageCount(b)
	if pages < 3 {
		t.Fatalf("expected several pages, got %d", pages)
	}
	for n := 1; n <= pages; n++ {
		want := fmt.Sprintf("(Page %d of %d) Tj", n, pages)
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("page %d of %d is not numbered", n, pages)
		}
	}
}

// A character no base-14 face can draw is reported rather than silently
// replaced, so a caller can tell somebody. A document that quietly
// altered a name would be the plausible-wrong-answer failure this
// package exists to avoid.
func TestUndrawableCharactersAreReported(t *testing.T) {
	b := sample(2)
	b.Paragraph("The creditor is 東京商事.")
	_, dropped, err := b.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(dropped) != 4 {
		t.Fatalf("expected the four undrawable characters, got %q", string(dropped))
	}
}

// Typographic characters real prose is full of are in Windows-1252 and
// must survive, or every curly apostrophe in a caller's own sentence
// becomes a question mark.
func TestTypographicCharactersSurvive(t *testing.T) {
	got, dropped := encode("“the creditors’ decision” — £1,250 … 50–60")
	if len(dropped) != 0 {
		t.Fatalf("dropped %q", string(dropped))
	}
	for _, b := range []byte{0x93, 0x92, 0x94, 0x97, 0xA3, 0x85, 0x96} {
		if !bytes.Contains(got, []byte{b}) {
			t.Errorf("byte %#x is missing from %v", b, got)
		}
	}
}

// Right-aligned money lines up, which is what the metric table is for.
// A column a reader cannot add up by eye is a column that defeats the
// point of a figures table.
func TestRightAlignedCellsShareARightEdge(t *testing.T) {
	b := render(t, sample(6))
	re := regexp.MustCompile(`BT /F\d [\d.]+ Tf ([\d.]+) [\d.]+ Td \((\d,\d{3}\.00)\) Tj`)
	ms := re.FindAllSubmatch(b, -1)
	if len(ms) < 3 {
		t.Fatalf("found %d money cells, expected the fixture's rows", len(ms))
	}
	for _, m := range ms {
		x, _ := strconv.ParseFloat(string(m[1]), 64)
		right := x + measure(string(m[2]), faceRegular, sizeBody)
		if want := pageW - marginR - 4; right < want-0.5 || right > want+0.5 {
			t.Errorf("%q ends at %.2f, not at the column's right edge %.2f", m[2], right, want)
		}
	}
}

// TestWriteSample is not an assertion: it writes the fixture to a file
// so a person can open it in a real reader and look at it.
//
// It exists because every check above is structural, and a structurally
// perfect PDF can still be ugly or wrong on the page. Nothing in CI can
// tell you a table's columns are too narrow. Run it with
// RASTRILLO_PDF_SAMPLE=<path> and open what it writes.
func TestWriteSample(t *testing.T) {
	path := os.Getenv("RASTRILLO_PDF_SAMPLE")
	if path == "" {
		t.Skip("set RASTRILLO_PDF_SAMPLE=<path> to write a sample document")
	}
	b, _, err := sample(45).Render()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d bytes to %s", len(b), path)
}

// A page break turns the page. Without it, where a section starts is
// decided by how long the section before it happened to be.
func TestAPageBreakStartsANewPage(t *testing.T) {
	a := New(opts())
	a.Paragraph("Front matter.")
	a.Heading(1, "Receipts and payments")
	if n := pageCount(render(t, a)); n != 1 {
		t.Fatalf("without a break the two blocks took %d pages; expected 1", n)
	}

	b := New(opts())
	b.Paragraph("Front matter.")
	b.PageBreak()
	b.Heading(1, "Receipts and payments")
	if n := pageCount(render(t, b)); n != 2 {
		t.Errorf("with a break the two blocks took %d pages; expected 2", n)
	}
}

// A break that arrives at the top of a fresh sheet draws nothing on it,
// so it must not leave one behind: a blank page in the middle of a
// document reads as a page that failed to print.
func TestARepeatedPageBreakLeavesNoBlankPage(t *testing.T) {
	b := New(opts())
	b.Paragraph("Front matter.")
	b.PageBreak()
	b.PageBreak()
	b.PageBreak()
	b.Heading(1, "Receipts and payments")
	if n := pageCount(render(t, b)); n != 2 {
		t.Errorf("three breaks in a row produced %d pages; expected 2", n)
	}
}
