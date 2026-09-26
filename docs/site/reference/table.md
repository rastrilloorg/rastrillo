# 🤖 table

`amadan.net/rastrillo/rastrillo/table`

Serve one table, a header and its rows, as a CSV or XLSX download. It
also protects CSV exports against formula injection.

## Serve

```go
const (
	CSV  = "csv"
	XLSX = "xlsx"
)

type Export struct {
	Filename string
	Sheet    string
	Header   []string
	Rows     [][]string
}

func Serve(w http.ResponseWriter, format string, e Export) error
```

```go
err := table.Serve(w, r.URL.Query().Get("format"), table.Export{
	Filename: "orders",
	Header:   []string{"Order", "Name", "Total"},
	Rows:     rows,
})
```

`Serve` sends `e` as a download in `format`. Pass `table.XLSX` for a
workbook; anything else, including a value taken straight from a query
string, gives CSV. The file is named `Filename` plus the extension, and
the workbook's tab is `Sheet`, or `Filename` if that is empty.

The response starts before the first row is written, so an error from
`Serve` means the client went away. Log it; there is no one left to
answer.

## Formula injection

```go
func Guard(field string) string
func GuardRow(rec []string) []string
```

A CSV cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage
return runs as a formula when someone opens the file in a spreadsheet. A
formula can send the row to another website, or run a command.

`Serve` puts every CSV cell, headers included, through `Guard`, which
adds a leading apostrophe so the spreadsheet shows the text instead. A
plain number like `-50` is left alone. XLSX cells are never run as
formulas, so they are not changed.

If you write CSV yourself, put every cell someone could have typed
through `Guard`, or each row through `GuardRow`.
