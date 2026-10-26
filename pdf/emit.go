package pdf

import (
	"bytes"
	"fmt"
	"strconv"
	"time"
	"unicode/utf16"
)

// Options are the facts about the act that produced the file, as
// opposed to the facts in it.
//
// Produced is the only clock this package sees. It is passed in rather
// than read here so that rendering the same calls again gives the same
// bytes as it did before — the determinism the package doc describes,
// and the thing that makes "render it again and compare" worth
// offering.
type Options struct {
	// Produced stamps the file's creation and modification dates.
	Produced time.Time
	// Title goes in the document information dictionary, which is what
	// a reader's window title and a document management system's
	// metadata both read, and labels the left side of the running
	// footer on every page.
	Title string
	// Reference, if set, is appended to Title in the running footer as
	// "Title — Reference". A caller with nothing case- or
	// record-specific to say leaves it empty.
	Reference string
	// Author is the organisation producing the file, not a person: a
	// metadata field is not where somebody's name should live.
	Author string
	// Stamp is words laid over every page once the document is laid
	// out, or nil for none — an approval stamp, drawn in the top
	// margin rather than across the body so it covers no figure. It is
	// words; the file carries no signature.
	Stamp *Stamp
	// NoRunningTitle leaves the title and reference off the foot of
	// every page; "Page n of N" is drawn regardless.
	NoRunningTitle bool
	// Landscape turns the page on its side. A table with more columns
	// than a portrait page's content width can hold at a readable size
	// is the reason to set it; most documents leave it false.
	Landscape bool
}

// Stamp is the two lines of an approval stamp: Head in large red
// capitals, Line beneath it in small type.
type Stamp struct {
	Head string
	Line string
}

// emit turns laid-out pages into a PDF file.
//
// The file is written by hand — object table, cross-reference table,
// trailer — because that is a couple of hundred lines and the
// alternative was a dependency whose own dependency set includes a PDF
// parser (see the package doc). Everything here is PDF 1.4 and
// deliberately dull: no compression, no object streams, no encryption.
// A file a person can open in a text editor and read is worth more to a
// long-lived record than one that is smaller.
func emit(pages [][]op, opt Options, g geom) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("pdf: nothing to render")
	}

	// Object numbering, fixed so the writer stays readable:
	//   1 catalog, 2 page tree, 3-5 fonts, 6 document information,
	//   then two objects per page — the page and its content stream.
	const (
		objCatalog = 1
		objPages   = 2
		objFontR   = 3
		objFontB   = 4
		objFontI   = 5
		objInfo    = 6
		objFirst   = 7
	)
	// Objects 1..objFirst-1 are fixed; every page adds two more, so the
	// last object number is objFirst-1+2*len(pages). Getting this wrong
	// leaves a cross-reference entry pointing at byte zero, which most
	// readers repair silently — which is why a test computes the
	// offsets back out of the finished file rather than trusting this
	// arithmetic.
	total := objFirst - 1 + 2*len(pages)

	offsets := make([]int, total)
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	// A comment of high bytes is the conventional way of telling a
	// transport that this file is binary. It costs four bytes and
	// prevents a well-meaning pipeline from mangling line endings.
	buf.Write([]byte{'%', 0xE2, 0xE3, 0xCF, 0xD3, '\n'})

	object := func(n int, body string) {
		offsets[n-1] = buf.Len()
		buf.WriteString(strconv.Itoa(n))
		buf.WriteString(" 0 obj\n")
		buf.WriteString(body)
		buf.WriteString("\nendobj\n")
	}

	kids := ""
	for i := range pages {
		if i > 0 {
			kids += " "
		}
		kids += strconv.Itoa(objFirst+2*i) + " 0 R"
	}

	object(objCatalog, "<< /Type /Catalog /Pages "+strconv.Itoa(objPages)+" 0 R >>")
	object(objPages, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(pages)))
	object(objFontR, font("Helvetica"))
	object(objFontB, font("Helvetica-Bold"))
	object(objFontI, font("Helvetica-Oblique"))
	object(objInfo, info(opt))

	for i, ops := range pages {
		pageObj := objFirst + 2*i
		contentObj := pageObj + 1
		object(pageObj, fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.2f %.2f] "+
				"/Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> "+
				"/Contents %d 0 R >>",
			objPages, g.pageW, g.pageH, objFontR, objFontB, objFontI, contentObj))

		stream := content(ops, g.pageH)
		object(contentObj, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}

	xref := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", total+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf(
		"trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		total+1, objCatalog, objInfo, xref))
	return buf.Bytes(), nil
}

// font is one base-14 font dictionary. WinAnsiEncoding is named
// explicitly because the default for a Type 1 font is the font's own
// built-in encoding, and the metrics table this package measures with
// is a Windows-1252 table. A file whose encoding and whose metrics
// disagree lays out correctly and prints the wrong characters, which is
// the worst of the two possible failures.
func font(base string) string {
	return "<< /Type /Font /Subtype /Type1 /BaseFont /" + base + " /Encoding /WinAnsiEncoding >>"
}

func info(opt Options) string {
	t := opt.Produced.UTC()
	stamp := fmt.Sprintf("D:%04d%02d%02d%02d%02d%02dZ",
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	return fmt.Sprintf(
		"<< /Title (%s) /Author (%s) /Producer (rastrillo/pdf) /CreationDate (%s) /ModDate (%s) >>",
		escape(pdfDocBytes(opt.Title)), escape(pdfDocBytes(opt.Author)), stamp, stamp)
}

// pdfDocBytes encodes a string for an Info dictionary text string —
// /Title, /Author — as UTF-16BE with the leading byte-order mark the
// PDF spec uses to tell a UTF-16 text string apart from one in
// PDFDocEncoding (ISO 32000-1 §7.9.2.2). Those two strings are not
// drawn on a page, so none of the reasons the page content stays in
// Windows-1252 — matching the fonts' declared /Encoding, staying
// seven-bit so escape() need only octal-escape it — apply to them, and
// Windows-1252 is the wrong encoding for them regardless: a reader
// decodes an Info string with no BOM as PDFDocEncoding, whose 0x80-0x9F
// range disagrees with Windows-1252's (confirmed against a real reader:
// an em dash and an ellipsis in a Title came back as "Š" and "–").
// UTF-16 also has no undrawable character for Render to report, unlike
// the page content: it can represent anything these two fields are
// likely to hold.
func pdfDocBytes(s string) []byte {
	out := []byte{0xFE, 0xFF}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// content turns one page's draw operations into a content stream. pageH
// is that page's height: operations measure y down from the top, and PDF
// measures it up from the bottom.
func content(ops []op, pageH float64) string {
	var b bytes.Buffer
	gray := -1.0
	setGray := func(g float64) {
		if g != gray {
			fmt.Fprintf(&b, "%.3f g\n", g)
			gray = g
		}
	}
	for _, o := range ops {
		switch o.kind {
		case 't':
			if o.text == "" {
				continue
			}
			setGray(o.gray)
			enc, _ := encode(o.text)
			fmt.Fprintf(&b, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n",
				fontName(o.face), o.size, o.x, pageH-o.y, escape(enc))
		case 'c':
			// Stamp text, in red. Wrapped in q/Q so the colour it sets
			// does not leak into the gray state tracked above.
			if o.text == "" {
				continue
			}
			enc, _ := encode(o.text)
			fmt.Fprintf(&b, "q 0.80 0.07 0.07 rg BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET Q\n",
				fontName(o.face), o.size, o.x, pageH-o.y, escape(enc))
		case 'l':
			// A rule is drawn as a thin filled rectangle rather than a
			// stroked line, so the content stream never has to carry
			// line-width state. One less piece of state is one less
			// thing to leave set wrong for the next operation.
			setGray(o.gray)
			fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f re f\n",
				o.x, pageH-o.y-0.6, o.x2-o.x, 0.6)
		case 'r':
			setGray(o.gray)
			fmt.Fprintf(&b, "%.2f %.2f %.2f %.2f re f\n",
				o.x, pageH-o.y2, o.x2-o.x, o.y2-o.y)
		}
	}
	return b.String()
}

func fontName(face int) string {
	switch face {
	case faceBold:
		return "F2"
	case faceItalic:
		return "F3"
	default:
		return "F1"
	}
}
