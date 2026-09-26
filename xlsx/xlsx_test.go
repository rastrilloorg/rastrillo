package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWriteReadRoundTrip(t *testing.T) {
	rows := [][]string{
		{"identifier", "name", "email"},
		{"a1", "José Müller", "jose@example.com"},
		{"a2", `quotes "and" <tags> & commas,`, ""},
		{"a3", "", "empty-middle@example.com"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "Attendees", rows); err != nil {
		t.Fatal(err)
	}
	if !IsZip(buf.Bytes()) {
		t.Fatal("written workbook is not a zip")
	}
	got, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	// Trailing empty cells trim on read — a reader pads short
	// rows, so this is lossless for a table of strings.
	want := [][]string{
		rows[0], rows[1],
		{"a2", `quotes "and" <tags> & commas,`},
		rows[3],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %q\nwant %q", got, want)
	}
}

func TestSniffers(t *testing.T) {
	if IsZip([]byte("identifier,name\na,b")) || IsZip([]byte{0x50}) {
		t.Fatal("IsZip false positives")
	}
	if !IsZip([]byte{0x50, 0x4b, 0x03, 0x04, 0x00}) {
		t.Fatal("IsZip misses PK signature")
	}
	legacy := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1, 0x00, 0x00}
	if !IsLegacyXLS(legacy) {
		t.Fatal("IsLegacyXLS misses OLE signature")
	}
	if IsLegacyXLS([]byte("plain text")) || IsLegacyXLS(legacy[:4]) {
		t.Fatal("IsLegacyXLS false positives")
	}
}

// buildFixture assembles a workbook the way other producers do: shared
// strings, numeric and boolean cells, a gap column, and a second sheet that
// must be ignored.
func buildFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	add("[Content_Types].xml", `<?xml version="1.0"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>`)
	add("_rels/.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`)
	// First sheet deliberately carries a non-default target name to prove the
	// reader resolves it through the rels rather than assuming sheet1.xml.
	add("xl/workbook.xml", `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Data" sheetId="1" r:id="rId7"/><sheet name="Ignored" sheetId="2" r:id="rId8"/></sheets>
</workbook>`)
	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId7" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
<Relationship Id="rId8" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
</Relationships>`)
	add("xl/sharedStrings.xml", `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
<si><t>identifier</t></si>
<si><r><t>Zo</t></r><r><t>ë rich</t></r></si>
<si><t xml:space="preserve"> spaced </t></si>
</sst>`)
	// A1 shared, C1 shared (B1 gap), A2 number, B2 boolean, C2 inline.
	add("xl/worksheets/sheet2.xml", `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2"><v>42.50</v></c><c r="B2" t="b"><v>1</v></c><c r="C2" t="inlineStr"><is><t>inline</t></is></c></row>
<row r="3"><c r="A3" t="s"><v>2</v></c></row>
</sheetData>
</worksheet>`)
	add("xl/worksheets/sheet1.xml", `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>WRONG SHEET</t></is></c></row></sheetData>
</worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadFixture(t *testing.T) {
	got, err := Read(buildFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"identifier", "", "Zoë rich"},
		{"42.5", "TRUE", "inline"},
		{" spaced "},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixture read:\n got %q\nwant %q", got, want)
	}
}

func TestReadRejectsNonZip(t *testing.T) {
	if _, err := Read([]byte("identifier,name\na,b")); err == nil {
		t.Fatal("plain CSV accepted as xlsx")
	}
	if _, err := Read([]byte{0x50, 0x4b, 0x03, 0x04, 0xff, 0xff}); err == nil {
		t.Fatal("corrupt zip accepted")
	}
}

// TestWriteSheetsKeepsEverySheet: a workbook with several tabs really carries
// them all, each with its own rows. Read only ever returns the first sheet, so
// the parts are checked through the zip directly — which is also the only way
// to prove the other sheets are there at all.
func TestWriteSheetsKeepsEverySheet(t *testing.T) {
	var buf bytes.Buffer
	sheets := []Sheet{
		{Name: "Buys with a card", Rows: [][]string{{"a", "b"}, {"c"}}},
		{Name: "Claims a free ticket", Rows: [][]string{{"d"}}},
		{Name: "Everything else", Rows: [][]string{{"e"}, {"f"}, {"g"}}},
	}
	if err := WriteSheets(&buf, sheets); err != nil {
		t.Fatalf("write: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(b)
	}
	for i, sh := range sheets {
		name := fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		body, ok := parts[name]
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		for _, row := range sh.Rows {
			for _, cell := range row {
				if !strings.Contains(body, ">"+cell+"<") {
					t.Errorf("%s does not carry %q", name, cell)
				}
			}
		}
		// Every sheet needs its content-type override and its relationship,
		// or Excel refuses the whole file rather than one tab of it.
		if !strings.Contains(parts["[Content_Types].xml"], name[len("xl/"):]) &&
			!strings.Contains(parts["[Content_Types].xml"], "/"+name) {
			t.Errorf("%s has no content-type override", name)
		}
		if !strings.Contains(parts["xl/workbook.xml"], `name="`+sh.Name+`"`) {
			t.Errorf("%s is not listed in the workbook", sh.Name)
		}
	}
	if strings.Count(parts["xl/_rels/workbook.xml.rels"], "<Relationship ") != len(sheets) {
		t.Errorf("the workbook has %d sheets and a different number of relationships",
			len(sheets))
	}
	// The first sheet still reads back the ordinary way.
	rows, err := Read(buf.Bytes())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "a" {
		t.Errorf("first sheet read back as %v", rows)
	}
}

// TestSheetNamesSurviveExcel: Excel will not open a workbook whose tab names
// use a character it reserves, run past 31 characters, or repeat — so a file
// that will not open is a failure this writer must not be able to produce.
func TestSheetNamesSurviveExcel(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Buys with a card", "Buys with a card"},
		{"Claims a free ticket / invite", "Claims a free ticket   invite"},
		{"Who: what [when]?", "Who  what  when"},
		{"", "Sheet"},
		{strings.Repeat("ab", 30), strings.Repeat("ab", 15) + "a"},
	} {
		if got := SheetName(c.in); got != c.want {
			t.Errorf("SheetName(%q) = %q, want %q", c.in, got, c.want)
		}
		if n := len([]rune(SheetName(c.in))); n > 31 {
			t.Errorf("SheetName(%q) is %d characters", c.in, n)
		}
	}
	// A collision takes a suffix, and the suffix is made room for rather than
	// pushing the name past the cap.
	var buf bytes.Buffer
	long := strings.Repeat("Claims a free ticket now and", 3)
	if err := WriteSheets(&buf, []Sheet{
		{Name: "Tickets", Rows: [][]string{{"a"}}},
		{Name: "Tickets", Rows: [][]string{{"b"}}},
		{Name: "Tickets:", Rows: [][]string{{"c"}}},
		{Name: long, Rows: [][]string{{"d"}}},
		{Name: long + " more", Rows: [][]string{{"e"}}},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var wb string
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			wb = string(b)
		}
	}
	names := map[string]bool{}
	for _, m := range strings.Split(wb, `<sheet name="`)[1:] {
		name := m[:strings.Index(m, `"`)]
		if names[strings.ToLower(name)] {
			t.Errorf("two tabs are both called %q", name)
		}
		names[strings.ToLower(name)] = true
		if n := len([]rune(name)); n > 31 {
			t.Errorf("tab %q is %d characters", name, n)
		}
	}
	if len(names) != 5 {
		t.Errorf("the workbook names %d tabs, want 5", len(names))
	}
}

// TestWriteSheetsRefusesAnEmptyWorkbook: a workbook with no sheets is not a
// file anything can open, and a writer that produced one silently would push
// the failure out to whoever tried to read it.
func TestWriteSheetsRefusesAnEmptyWorkbook(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSheets(&buf, nil); err == nil {
		t.Errorf("wrote a workbook with no sheets")
	}
}

// workbook is a one-sheet file with the given sheetData rows and, when
// shared is not empty, a sharedStrings part holding it. sharedTarget
// lets a test point the relationship at a part that is not there.
func workbook(t *testing.T, rows, shared, sharedTarget string) []byte {
	t.Helper()
	return workbookSheets(t, 1, rows, shared, sharedTarget)
}

// workbookSheets lists the same worksheet part `tabs` times, which is
// legal and is how one small file can ask for many sheets' worth of
// memory.
func workbookSheets(t *testing.T, tabs int, rows, shared, sharedTarget string) []byte {
	t.Helper()
	return workbookParts(t, tabs, `<worksheet><sheetData>`+rows+`</sheetData></worksheet>`, shared, sharedTarget)
}

// workbookParts is workbookSheets with the worksheet part given whole,
// for a test about what that part is.
func workbookParts(t *testing.T, tabs int, sheetPart, shared, sharedTarget string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, content string) {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	var sheets strings.Builder
	for i := 1; i <= tabs; i++ {
		fmt.Fprintf(&sheets, `<sheet name="S%d" sheetId="%d" r:id="rId1"/>`, i, i)
	}
	add("xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`+sheets.String()+`</sheets></workbook>`)
	rels := `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>`
	if sharedTarget != "" {
		rels += `<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="` + sharedTarget + `"/>`
	}
	add("xl/_rels/workbook.xml.rels", rels+`</Relationships>`)
	if shared != "" {
		add("xl/sharedStrings.xml", `<sst>`+shared+`</sst>`)
	}
	add("xl/worksheets/sheet1.xml", sheetPart)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A reference names a column, and a row is padded out to it. One cell
// at ZZZZZZ1 used to ask for 321 million slots: a file of a few hundred
// bytes that could exhaust an importer's memory. Past Excel's last
// column is refused, and padding counts toward the cell budget.
func TestReadRefusesReferencesPastTheLastColumn(t *testing.T) {
	for _, ref := range []string{"ZZZZZZ1", "XFE1", strings.Repeat("Z", 40) + "1"} {
		_, err := Read(workbook(t, `<row r="1"><c r="`+ref+`" t="inlineStr"><is><t>x</t></is></c></row>`, "", ""))
		if err == nil {
			t.Errorf("a cell at %s was read", ref)
		}
	}
	got, err := Read(workbook(t, `<row r="1"><c r="XFD1" t="inlineStr"><is><t>last</t></is></c></row>`, "", ""))
	if err != nil || len(got[0]) != 16384 || got[0][16383] != "last" {
		t.Fatalf("the last real column: %v, %d cells", err, len(got[0]))
	}
}

func TestReadCountsPaddingTowardTheCellBudget(t *testing.T) {
	var rows strings.Builder
	// 70 rows of one cell at XFD: 70 * 16384 slots is past the budget,
	// though the file names only 70 cells.
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&rows, `<row r="%d"><c r="XFD%d" t="inlineStr"><is><t>x</t></is></c></row>`, i, i)
	}
	if _, err := Read(workbook(t, rows.String(), "", "")); err == nil {
		t.Fatal("a sparse file padded past the cell budget was read")
	}
}

// A workbook that declares shared strings and cannot supply them is
// damaged; reading on would turn every text cell into "".
func TestReadFailsWhenDeclaredSharedStringsAreMissing(t *testing.T) {
	_, err := Read(workbook(t, `<row r="1"><c r="A1" t="s"><v>0</v></c></row>`, "", "sharedStrings.xml"))
	if err == nil {
		t.Fatal("a missing sharedStrings part read as success")
	}
}

// Inline strings can be rich text, and shared strings can carry a
// phonetic guide that is not part of the value.
func TestReadRichInlineTextAndSkipsPhoneticGuides(t *testing.T) {
	got, err := Read(workbook(t,
		`<row r="1"><c r="A1" t="inlineStr"><is><r><t>Zo</t></r><r><t>ë rich</t></r></is></c><c r="B1" t="s"><v>0</v></c></row>`,
		`<si><t>東京</t><rPh sb="0" eb="2"><t>とうきょう</t></rPh></si>`, "sharedStrings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Zoë rich", "東京"}; !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %q, want %q", got[0], want)
	}
}

// _xHHHH_ is spreadsheet text's own escape. A value that merely
// contains the spelling must come back as written, and a control
// character XML cannot carry must survive the trip too.
func TestTextEscapesRoundTrip(t *testing.T) {
	values := []string{"_x0041_", "_x0041_x0042_", "a_b", "_x005F_", "bell\x01ring", "tab\tand\nnewline", "_x12G4_", "non\uFFFEchar\uFFFF"}
	var buf bytes.Buffer
	if err := Write(&buf, "S", [][]string{values}); err != nil {
		t.Fatal(err)
	}
	got, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0], values) {
		t.Fatalf("round trip:\n got %q\nwant %q", got[0], values)
	}
	// And what Excel itself writes: a carriage return as _x000D_.
	got, err = Read(workbook(t, `<row r="1"><c r="A1" t="inlineStr"><is><t>a_x000D_b</t></is></c></row>`, "", ""))
	if err != nil || got[0][0] != "a\rb" {
		t.Fatalf("Excel's _x000D_ read as %q (%v)", got[0][0], err)
	}
}

// Cutting a name to 31 characters can expose an apostrophe at the new
// end, which Excel refuses as it would at the old one.
func TestSheetNameTrimsAfterCutting(t *testing.T) {
	got := SheetName(strings.Repeat("a", 30) + "'b")
	if strings.HasSuffix(got, "'") || got == "" || len([]rune(got)) > 31 {
		t.Fatalf("SheetName = %q", got)
	}
	if got := SheetName(strings.Repeat("'", 40)); got != "Sheet" {
		t.Fatalf("a name of apostrophes = %q, want Sheet", got)
	}
}

// The cell budget is the workbook's, not each sheet's: a file may list
// one sparse sheet many times, and every sheet read stays in memory.
func TestCellBudgetCoversTheWholeWorkbook(t *testing.T) {
	var rows strings.Builder
	for i := 1; i <= 40; i++ { // 40 * 16384 slots: under the cap once, over it twice
		fmt.Fprintf(&rows, `<row r="%d"><c r="XFD%d" t="inlineStr"><is><t>x</t></is></c></row>`, i, i)
	}
	if _, err := ReadSheets(workbookSheets(t, 1, rows.String(), "", "")); err != nil {
		t.Fatalf("one sheet under the budget: %v", err)
	}
	if _, err := ReadSheets(workbookSheets(t, 2, rows.String(), "", "")); err == nil {
		t.Fatal("the same sheet listed twice went past the workbook's budget")
	}
}

// A cell of many rich-text runs is read in linear time. Repeated
// string concatenation turned forty thousand one-letter runs, a small
// file, into hundreds of megabytes of copying.
func TestManyRichTextRunsReadLinearly(t *testing.T) {
	var runs strings.Builder
	for i := 0; i < 40000; i++ {
		runs.WriteString(`<r><t>a</t></r>`)
	}
	file := workbook(t, `<row r="1"><c r="A1" t="inlineStr"><is>`+runs.String()+`</is></c></row>`, "", "")
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := Read(file)
	runtime.ReadMemStats(&after)
	if err != nil || len(got[0][0]) != 40000 {
		t.Fatalf("read %d letters: %v", len(got[0][0]), err)
	}
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 200 {
		t.Fatalf("reading one cell allocated %d MB", mb)
	}
}

// A shared-string reference that does not resolve fails the read: a
// populated row that reads as empty is an import that looks fine.
func TestUnresolvedSharedStringIsAnError(t *testing.T) {
	for name, file := range map[string][]byte{
		"out of range":          workbook(t, `<row r="1"><c r="A1" t="s"><v>5</v></c></row>`, `<si><t>only</t></si>`, "sharedStrings.xml"),
		"malformed":             workbook(t, `<row r="1"><c r="A1" t="s"><v>x</v></c></row>`, `<si><t>only</t></si>`, "sharedStrings.xml"),
		"no shared part at all": workbook(t, `<row r="1"><c r="A1" t="s"><v>0</v></c></row>`, "", ""),
	} {
		if _, err := Read(file); err == nil {
			t.Errorf("%s: read as success", name)
		}
	}
}

// Excel writes a character outside the BMP as two escaped UTF-16
// halves; they decode together.
func TestEscapedSurrogatePairsDecodeTogether(t *testing.T) {
	got, err := Read(workbook(t, `<row r="1"><c r="A1" t="inlineStr"><is><t>hi _xD83D__xDE00_!</t></is></c></row>`, "", ""))
	if err != nil || got[0][0] != "hi 😀!" {
		t.Fatalf("read %q (%v)", got[0][0], err)
	}
}

// An empty <row/> still costs a slice, and a 50 MB part holds millions
// of them; every row counts toward the cap, not only cells.
func TestReadCountsEmptyRowsTowardTheCap(t *testing.T) {
	rows := strings.Repeat("<row/>", maxCells+1)
	if _, err := Read(workbook(t, rows, "", "")); err == nil {
		t.Fatal("more empty rows than the cap were read")
	}
}

// Text is budgeted across the workbook as well as cells: one sheet of a
// hundred 8 KiB strings, listed a hundred times, is ten thousand cells
// and 80 MB.
func TestReadBudgetsTextAcrossTheWorkbook(t *testing.T) {
	big := strings.Repeat("x", 8<<10)
	var rows strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&rows, `<row r="%d"><c r="A%d" t="inlineStr"><is><t>%s</t></is></c></row>`, i, i, big)
	}
	if _, err := ReadSheets(workbookSheets(t, 1, rows.String(), "", "")); err != nil {
		t.Fatalf("one sheet of 800 KB: %v", err)
	}
	if _, err := ReadSheets(workbookSheets(t, 100, rows.String(), "", "")); err == nil {
		t.Fatal("80 MB of text across one workbook was read")
	}
}

// A producer may omit empty rows and give each row its number; the gap
// is restored, so A3 reads back as the third row, not the second.
func TestReadRestoresOmittedRows(t *testing.T) {
	got, err := Read(workbook(t,
		`<row r="1"><c r="A1" t="inlineStr"><is><t>one</t></is></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>three</t></is></c></row>`, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"one"}, nil, {"three"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Refused as a row past the last, before any gap is padded: the
	// cell budget would refuse it too, but only after allocating.
	if _, err := Read(workbook(t, `<row r="1048577"><c t="inlineStr"><is><t>x</t></is></c></row>`, "", "")); err == nil || !strings.Contains(err.Error(), "last row") {
		t.Fatalf("a row past Excel's last: %v", err)
	}
}

// Write refuses what Excel cannot open, rather than handing out a file
// that fails later: a value past XFD, or more rows than a sheet holds.
func TestWriteRefusesPastExcelsLimits(t *testing.T) {
	wide := make([]string, maxColumns+1)
	wide[maxColumns] = "too far"
	if err := Write(io.Discard, "S", [][]string{wide}); err == nil {
		t.Error("a value in column 16,385 was written")
	}
	// Empty cells past the limit are fine: they are never written.
	if err := Write(io.Discard, "S", [][]string{make([]string, maxColumns+5)}); err != nil {
		t.Errorf("trailing empty cells: %v", err)
	}
	if err := Write(io.Discard, "S", make([][]string, maxRows+1)); err == nil {
		t.Error("more rows than a sheet holds were written")
	}
}

// What a read decompresses is budgeted across the workbook: one 1 MiB
// sheet listed two hundred times is a 2 KB file and 200 MiB of work.
func TestReadBudgetsUnpackedBytesAcrossTheWorkbook(t *testing.T) {
	comment := "<!--" + strings.Repeat("x", 1<<20) + "-->"
	if _, err := ReadSheets(workbookSheets(t, 1, comment, "", "")); err != nil {
		t.Fatalf("one 1 MiB sheet: %v", err)
	}
	if _, err := ReadSheets(workbookSheets(t, 201, comment, "", "")); err == nil {
		t.Fatal("a 1 MiB sheet unpacked 201 times was read")
	}
}

// A sheet part that is not a worksheet is a damaged file, not an empty
// sheet.
func TestReadRefusesAPartThatIsNotAWorksheet(t *testing.T) {
	for name, part := range map[string]string{
		"empty":            "",
		"declaration only": `<?xml version="1.0"?>`,
		"another document": `<html>not a worksheet</html>`,
	} {
		if _, err := Read(workbookParts(t, 1, part, "", "")); err == nil {
			t.Errorf("%s: read as an empty sheet", name)
		}
	}
	if got, err := Read(workbookParts(t, 1, `<?xml version="1.0"?><worksheet/>`, "", "")); err != nil || len(got) != 0 {
		t.Fatalf("an empty worksheet: %q, %v", got, err)
	}
}

// Excel counts a tab name in UTF-16 units: twenty emoji are twenty
// runes but forty units, over the 31 limit.
func TestSheetNameCountsUTF16Units(t *testing.T) {
	name := SheetName(strings.Repeat("😀", 20))
	if n := utf16Len(name); n > 31 || n == 0 {
		t.Fatalf("SheetName kept %d UTF-16 units: %q", n, name)
	}
	if !utf8.ValidString(name) {
		t.Fatalf("SheetName split a character: %q", name)
	}
	names := sheetNames([]Sheet{{Name: strings.Repeat("😀", 20)}, {Name: strings.Repeat("😀", 20)}})
	for _, n := range names {
		if utf16Len(n) > 31 || !utf8.ValidString(n) {
			t.Fatalf("a de-duplicated name is %d units or split: %q", utf16Len(n), n)
		}
	}
	if names[0] == names[1] {
		t.Fatalf("two tabs share a name: %q", names)
	}
}

// A cell holds at most 32,767 UTF-16 units; Write refuses more rather
// than hand out a file Excel truncates or repairs.
func TestWriteRefusesACellPastExcelsLength(t *testing.T) {
	for _, v := range []string{strings.Repeat("a", 32768), strings.Repeat("😀", 16384)} {
		if err := Write(io.Discard, "S", [][]string{{v}}); err == nil {
			t.Errorf("a %d-unit cell was written", utf16Len(v))
		}
	}
	if err := Write(io.Discard, "S", [][]string{{strings.Repeat("a", 32767)}}); err != nil {
		t.Errorf("a cell at the limit: %v", err)
	}
}

// Tab names that differ only in characters XML cannot carry would be
// written as one name twice; they are normalised before de-duplication.
func TestSheetNamesDeduplicateAfterNormalising(t *testing.T) {
	names := sheetNames([]Sheet{{Name: "A\x00"}, {Name: "A\x01"}})
	if names[0] == names[1] || strings.ContainsAny(names[0]+names[1], "\x00\x01\uFFFD") {
		t.Fatalf("names = %q", names)
	}
}
