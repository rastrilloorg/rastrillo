# 🤖 xlsx

`amadan.net/rastrillo/rastrillo/xlsx`

Read and write .xlsx workbooks of plain strings, using only the standard
library. Use it for exports, and for reading a spreadsheet someone
uploads.

Every cell is text. Styles, formulas and dates are not supported: a date
in an uploaded file reads as the number Excel stores it as.

## Writing

```go
type Sheet struct {
	Name string
	Rows [][]string
}

func Write(w io.Writer, sheetName string, rows [][]string) error
func WriteSheets(w io.Writer, sheets []Sheet) error
func SheetName(name string) string
```

`Write` writes a workbook with one sheet. `WriteSheets` writes one tab
for each `Sheet`, in order. Tab names go through `SheetName` first, so a
name Excel would refuse, or the same name twice, still gives a file that
opens.

`SheetName` makes a name Excel will accept: reserved characters
replaced, cut to 31 characters, and never empty.

## Reading

```go
func Read(data []byte) ([][]string, error)
func ReadSheets(data []byte) ([]Sheet, error)
```

`Read` returns the first sheet's rows. `ReadSheets` returns every tab
with its name. A tab that will not parse fails the whole read, so you
never get part of a workbook that looks complete.

An empty cell inside a row reads as `""`, and empty cells at the end of
a row are dropped. A file with more than a million cells, or a part over
50 MB unpacked, is refused.

```go
func IsZip(data []byte) bool
func IsLegacyXLS(data []byte) bool
```

`IsZip` and `IsLegacyXLS` check an upload's first bytes. Use them to ask
someone to save an old .xls file as .xlsx or CSV, instead of showing
them a parse error.

## ColumnName

```go
func ColumnName(index int) string
```

`ColumnName` turns a column index into its letter: 0 is A, 26 is AA. Use
it when you need to name a column to a person.
