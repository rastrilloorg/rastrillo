package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
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
