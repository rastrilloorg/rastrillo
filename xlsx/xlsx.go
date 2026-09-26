// Package xlsx is a deliberately minimal .xlsx reader and writer, on
// the standard library alone. It covers exactly what an app's exports
// and imports need: write sheets of strings, and read them back as
// strings. Anything fancier (styles, formulas, dates as dates) is out
// of scope by design; reach for a real library only if that changes.
//
// Every cell is written as an inline string, which is also why a value
// that looks like a formula is safe here: a spreadsheet never evaluates
// an inline string. The CSV formula guard (package table) must not be
// applied on this path — it would only corrupt the values.
//
// Ported from Tito Go's internal/xlsx, where it serves every report
// export and the attendee and invitation importers.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// IsZip reports whether data starts with the local-file zip signature —
// every .xlsx (and any zip) does.
func IsZip(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4b && data[2] == 0x03 && data[3] == 0x04
}

// IsLegacyXLS reports whether data is an OLE compound file — the pre-2007
// binary .xls container (also .doc/.ppt). We never parse these; the caller
// shows a "save as .xlsx or CSV" message.
func IsLegacyXLS(data []byte) bool {
	sig := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	return len(data) >= len(sig) && bytes.Equal(data[:len(sig)], sig)
}

const (
	// maxPartBytes caps a single decompressed XML part — a zip-bomb guard,
	// far above any legitimate import.
	maxPartBytes = 50 << 20
	// maxCells caps the cells read, counting the empty slots a row is
	// padded with as well as the cells a file names: a single cell at
	// XFD1 costs 16,384 slots, and counting only the cell would let a
	// tiny file allocate as much as its references can reach.
	maxCells = 1 << 20
	// maxColumns is Excel's own limit (XFD). A reference past it is not
	// a spreadsheet anyone made, and following it would size a row by
	// a number the file chose: ZZZZZZ1 alone asked for 321 million
	// slots before this cap.
	maxColumns = 16384
)

// Sheet is one tab in a workbook: a name and its rows of strings.
type Sheet struct {
	Name string
	Rows [][]string
}

// Write emits a minimal single-sheet workbook: every cell an inline string.
func Write(w io.Writer, sheetName string, rows [][]string) error {
	return WriteSheets(w, []Sheet{{Name: sheetName, Rows: rows}})
}

// WriteSheets emits a workbook with one tab per sheet, in the order given.
// Read returns the FIRST sheet only; ReadSheets returns all of them, named.
//
// Sheet names are put through SheetName here rather than trusted from the
// caller: Excel refuses to open a workbook naming a tab with a character it
// reserves, or naming two tabs the same, and a file that will not open is the
// one failure a spreadsheet writer must not be able to produce.
func WriteSheets(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		return errors.New("xlsx: a workbook needs at least one sheet")
	}
	names := sheetNames(sheets)
	zw := zip.NewWriter(w)
	part := func(name, content string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, content)
		return err
	}
	var types, wbSheets, wbRels strings.Builder
	for i := range sheets {
		fmt.Fprintf(&types, "<Override PartName=\"/xl/worksheets/sheet%d.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml\"/>\n", i+1)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(names[i]), i+1, i+1)
		fmt.Fprintf(&wbRels, "<Relationship Id=\"rId%d\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\" Target=\"worksheets/sheet%d.xml\"/>\n", i+1, i+1)
	}
	if err := part("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
`+types.String()+`</Types>`); err != nil {
		return err
	}
	if err := part("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`); err != nil {
		return err
	}
	if err := part("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets>`+wbSheets.String()+`</sheets>
</workbook>`); err != nil {
		return err
	}
	if err := part("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
`+wbRels.String()+`</Relationships>`); err != nil {
		return err
	}
	for i, sh := range sheets {
		sheet, err := zw.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1))
		if err != nil {
			return err
		}
		if _, err := io.WriteString(sheet, sheetXML(sh.Rows)); err != nil {
			return err
		}
	}
	return zw.Close()
}

func sheetXML(rows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>`)
	for r, row := range rows {
		fmt.Fprintf(&sb, `<row r="%d">`, r+1)
		for c, value := range row {
			// Empty cells are simply omitted; Read restores them as "".
			if value == "" {
				continue
			}
			fmt.Fprintf(&sb, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
				columnName(c), r+1, xmlEscape(encodeText(value)))
		}
		sb.WriteString(`</row>`)
	}
	sb.WriteString(`</sheetData>
</worksheet>`)
	return sb.String()
}

// sheetInvalid is the set Excel reserves in a tab name. A leading or trailing
// apostrophe is refused too, which SheetName trims.
var sheetInvalid = strings.NewReplacer(
	"[", " ", "]", " ", ":", " ", "*", " ", "?", " ", "/", " ", `\`, " ")

// SheetName makes one tab name Excel will open: the reserved characters
// replaced, the 31-character cap applied by RUNE so a multi-byte name is not
// cut mid-character, and never empty.
func SheetName(name string) string {
	name = strings.TrimSpace(sheetInvalid.Replace(name))
	name = strings.Trim(name, "'")
	if r := []rune(name); len(r) > 31 {
		// Cutting can expose an apostrophe or a space at the new end,
		// which Excel refuses just as it would have at the old one.
		name = strings.TrimSpace(strings.Trim(strings.TrimSpace(string(r[:31])), "'"))
	}
	if name == "" {
		name = "Sheet"
	}
	return name
}

// sheetNames resolves every tab name at once, because uniqueness is a
// property of the SET: two names that differ only past the 31-rune cap, or
// only in a reserved character, arrive here different and leave the same.
// A collision takes a numeric suffix, and the suffix is made room for rather
// than appended past the cap.
func sheetNames(sheets []Sheet) []string {
	out := make([]string, len(sheets))
	taken := map[string]bool{}
	for i, sh := range sheets {
		name := SheetName(sh.Name)
		for n := 2; taken[strings.ToLower(name)]; n++ {
			suffix := " " + strconv.Itoa(n)
			trimmed := []rune(SheetName(sh.Name))
			if len(trimmed)+len([]rune(suffix)) > 31 {
				trimmed = trimmed[:31-len([]rune(suffix))]
			}
			name = strings.TrimSpace(string(trimmed)) + suffix
		}
		taken[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}

// Read parses the workbook's first sheet into rows of strings.
func Read(data []byte) ([][]string, error) {
	sheets, err := ReadSheets(data)
	if err != nil {
		return nil, err
	}
	return sheets[0].Rows, nil
}

// ReadSheets parses EVERY tab, in workbook order, with the name each one
// carries. It exists because a workbook read back can be one an app
// wrote with a tab per section (Tito's translation round trip is one tab
// per journey), and a reader that took the first tab would silently drop
// the rest.
//
// A tab that will not parse fails the whole read rather than being
// skipped: a partial workbook that looks complete is the failure a
// round trip can least afford.
func ReadSheets(data []byte) ([]Sheet, error) {
	if !IsZip(data) {
		return nil, errors.New("not an xlsx file")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	parts := map[string]*zip.File{}
	for _, f := range zr.File {
		parts[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := parts[name]
		if !ok {
			return nil, fmt.Errorf("xlsx: missing part %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		content, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
		if err != nil {
			return nil, err
		}
		if len(content) > maxPartBytes {
			return nil, errors.New("xlsx: part too large")
		}
		return content, nil
	}
	paths, sharedPath, err := sheetPaths(read)
	if err != nil {
		return nil, err
	}
	var shared []string
	if sharedPath != "" {
		// A workbook that declares shared strings and cannot supply
		// them is damaged: reading on would turn every text cell into
		// "" and report success, an import that looks fine and is not.
		raw, err := read(sharedPath)
		if err != nil {
			return nil, err
		}
		if shared, err = parseSharedStrings(raw); err != nil {
			return nil, err
		}
	}
	out := make([]Sheet, 0, len(paths))
	for _, sp := range paths {
		raw, err := read(sp.path)
		if err != nil {
			return nil, err
		}
		rows, err := parseSheet(raw, shared)
		if err != nil {
			return nil, err
		}
		out = append(out, Sheet{Name: sp.name, Rows: rows})
	}
	return out, nil
}

// sheetRef is one tab as the workbook lists it: the name a reader sees on the
// tab, and the part its cells live in.
type sheetRef struct{ name, path string }

// sheetPaths resolves every sheet's part path (and the shared-strings path,
// if any) through xl/_rels/workbook.xml.rels — producers are free to name
// sheet parts anything.
func sheetPaths(read func(string) ([]byte, error)) ([]sheetRef, string, error) {
	wb, err := read("xl/workbook.xml")
	if err != nil {
		return nil, "", err
	}
	var workbook struct {
		Sheets struct {
			Sheet []struct {
				ID   string `xml:"id,attr"`
				Name string `xml:"name,attr"`
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	if err := xml.Unmarshal(wb, &workbook); err != nil {
		return nil, "", err
	}
	if len(workbook.Sheets.Sheet) == 0 {
		return nil, "", errors.New("xlsx: workbook has no sheets")
	}
	relsRaw, err := read("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, "", err
	}
	var rels struct {
		Relationship []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsRaw, &rels); err != nil {
		return nil, "", err
	}
	resolve := func(target string) string {
		if strings.HasPrefix(target, "/") {
			return strings.TrimPrefix(target, "/")
		}
		return path.Join("xl", target)
	}
	byID, shared := map[string]string{}, ""
	for _, rel := range rels.Relationship {
		byID[rel.ID] = resolve(rel.Target)
		if strings.HasSuffix(rel.Type, "/sharedStrings") {
			shared = resolve(rel.Target)
		}
	}
	out := make([]sheetRef, 0, len(workbook.Sheets.Sheet))
	for _, sh := range workbook.Sheets.Sheet {
		target, ok := byID[sh.ID]
		if !ok {
			return nil, "", fmt.Errorf("xlsx: sheet %q has no relationship", sh.Name)
		}
		out = append(out, sheetRef{name: sh.Name, path: target})
	}
	return out, shared, nil
}

// parseSharedStrings flattens each <si> to the concatenation of its <t>
// descendants (plain and rich-text runs alike).
func parseSharedStrings(raw []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var out []string
	var current strings.Builder
	depth := 0 // inside an <si>
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch el := token.(type) {
		case xml.StartElement:
			if el.Name.Local == "si" {
				if len(out) >= maxCells {
					return nil, errors.New("xlsx: too many shared strings")
				}
				depth = 1
				current.Reset()
			} else if depth > 0 && el.Name.Local == "rPh" {
				// A phonetic guide (the reading over 東京) is not part
				// of the value; collecting its <t> would import
				// 東京とうきょう.
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
			} else if depth > 0 && el.Name.Local == "t" {
				var text string
				if err := decoder.DecodeElement(&text, &el); err != nil {
					return nil, err
				}
				current.WriteString(text)
			}
		case xml.EndElement:
			if el.Name.Local == "si" && depth > 0 {
				out = append(out, decodeText(current.String()))
				depth = 0
			}
		}
	}
}

// parseSheet streams sheetData into rows. Cell references place values;
// missing cells inside a row read as ""; trailing empties are trimmed.
func parseSheet(raw []byte, shared []string) ([][]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var rows [][]string
	slots := 0 // cells named plus the empty slots padding them in
	inData := false
	var row []string
	appendCell := func(col int, value string) error {
		if col >= maxColumns {
			return errors.New("xlsx: cell reference past the last column")
		}
		if grow := col + 1 - len(row); grow > 0 {
			if slots += grow; slots > maxCells {
				return errors.New("xlsx: too many cells")
			}
			row = append(row, make([]string, grow)...)
		}
		row[col] = value
		return nil
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		switch el := token.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "sheetData":
				inData = true
			case "row":
				if inData {
					row = nil
				}
			case "c":
				if !inData {
					continue
				}
				var ref, typ string
				for _, attr := range el.Attr {
					switch attr.Name.Local {
					case "r":
						ref = attr.Value
					case "t":
						typ = attr.Value
					}
				}
				// An inline string is either plain text or rich-text runs,
				// each with its own <t>; phonetic guides (rPh) are left
				// out, as they are not part of the value.
				var cell struct {
					V  string `xml:"v"`
					Is struct {
						Text []string `xml:"t"`
						Runs []struct {
							Text []string `xml:"t"`
						} `xml:"r"`
					} `xml:"is"`
				}
				if err := decoder.DecodeElement(&cell, &el); err != nil {
					return nil, err
				}
				inline := strings.Join(cell.Is.Text, "")
				for _, run := range cell.Is.Runs {
					inline += strings.Join(run.Text, "")
				}
				col := columnIndex(ref)
				if col < 0 {
					col = len(row)
				}
				if err := appendCell(col, cellValue(typ, cell.V, inline, shared)); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if el.Name.Local == "row" && inData {
				for len(row) > 0 && row[len(row)-1] == "" {
					row = row[:len(row)-1]
				}
				rows = append(rows, row)
				row = nil
			} else if el.Name.Local == "sheetData" {
				inData = false
			}
		}
	}
}

func cellValue(typ, v, inline string, shared []string) string {
	switch typ {
	case "s":
		index, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || index < 0 || index >= len(shared) {
			return ""
		}
		return shared[index]
	case "inlineStr":
		return decodeText(inline)
	case "b":
		if strings.TrimSpace(v) == "1" {
			return "TRUE"
		}
		return "FALSE"
	case "str", "":
		if typ == "str" {
			return decodeText(v)
		}
		// A typeless cell is numeric: render canonically, no trailing zeros.
		// Dates surface as their serial numbers, because a date is a number
		// with a style this reader does not read. Unparseable values pass
		// through as-is.
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return v
	default:
		return v
	}
}

// ColumnName is the spreadsheet letter for a 0-based column: 0 → A, 26 → AA.
// It is for naming a column to a person (a formula hint that must point
// at the column the export actually used) rather than a letter typed into
// a string that goes stale when the columns move.
func ColumnName(index int) string { return columnName(index) }

// columnName converts 0 → A, 25 → Z, 26 → AA.
func columnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

// columnIndex extracts the 0-based column from an A1-style reference; -1 if
// the reference is absent or malformed.
func columnIndex(ref string) int {
	col := 0
	seen := false
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			col = col*26 + int(r-'A') + 1
			seen = true
			if col > maxColumns {
				// Past the last column already; keep counting and a
				// long enough reference overflows into a valid-looking
				// index.
				return maxColumns
			}
		} else {
			break
		}
	}
	if !seen {
		return -1
	}
	return col - 1
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// Spreadsheet text has an escape of its own on top of XML's: _xHHHH_ is
// the character U+HHHH. Excel writes a control character that XML cannot
// carry that way (_x0001_), and reads any _xHHHH_ it finds as one, so a
// value that merely contains the spelling must have its underscore
// escaped as _x005F_ or it comes back as a different string.

// encodeText escapes value for a spreadsheet text cell: each underscore
// that starts an _xHHHH_ spelling becomes _x005F_, and each control
// character XML cannot carry becomes _x00HH_ (xml.EscapeText would put
// U+FFFD in its place, losing it). Every underscore is checked, not
// every match, because the closing underscore of one spelling can open
// the next: _x0041_x0042_ needs both escaped to survive.
func encodeText(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '_' && isEscapeAt(value, i):
			b.WriteString("_x005F_")
		case c < 0x20 && c != '\t' && c != '\n' && c != '\r':
			fmt.Fprintf(&b, "_x%04X_", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// isEscapeAt reports whether s has the _xHHHH_ spelling starting at i.
func isEscapeAt(s string, i int) bool {
	if i+7 > len(s) || s[i] != '_' || s[i+1] != 'x' || s[i+6] != '_' {
		return false
	}
	for _, h := range s[i+2 : i+6] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", h) {
			return false
		}
	}
	return true
}

// decodeText undoes encodeText, and Excel's own escapes with it, left to
// right as Excel reads them.
func decodeText(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if isEscapeAt(s, i) {
			n, _ := strconv.ParseUint(s[i+2:i+6], 16, 32)
			b.WriteRune(rune(n))
			i += 6
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
