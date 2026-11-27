package chart

import (
	"reflect"
	"slices"
)

// Column is one column of a data table. Num marks a column of numbers:
// end-aligned, in tabular figures, each written in the page's locale.
// Href makes its head a link to the list sorted by it; Sort,
// "ascending" or "descending", marks the column the list is sorted by.
type Column struct {
	Label string
	Num   bool
	Href  string
	Sort  string
}

// Cell is a table cell that links somewhere, such as a row's name to
// its record. Any other value is a cell as it is.
type Cell struct {
	Value any
	Href  string
}

// Table is a data table. Each row has one value per column, and the
// first is the row's header. Foot is an optional row of totals, no
// longer than the columns. Empty is the sentence a table with no rows
// shows ("No weeks match."). Dense tightens the rows; Bounded caps the
// table's height and keeps its head row in view.
type Table struct {
	ID, Title      string
	Columns        []Column
	Rows           [][]any
	Foot           []any
	Empty          string
	Dense, Bounded bool
}

// TablePlot is a table as data-table writes it.
type TablePlot struct {
	ID, Title, Empty string
	Dense, Bounded   bool
	Columns          []Column
	Rows             [][]TableCell
	Foot             []TableCell
}

// TableCell is one cell as data-table writes it: a header for the
// first of a row, a number for a cell in a Num column.
type TableCell struct {
	Value     any
	Href      string
	Num, Head bool
}

const (
	rowLengthMsg = "%s: row %d has %d cells for %d columns; give it " +
		"one per column"
	footLengthMsg = "%s: Foot has %d cells for %d columns; give it " +
		"one per column at most"
	sortMsg = "%s: column %q sorts %q; write \"ascending\" or " +
		"\"descending\", or nothing"
)

// ReadTable reads data-table's data: a Table as its Chart, or the keys
// Table has, with Columns as a list of Label, Num, Href and Sort.
func ReadTable(data any) (Table, error) {
	if table, ok := typed[Table](data); ok {
		return table, nil
	}
	reading := reader{data: data}
	table := Table{
		ID: reading.text("ID"), Title: reading.text("Title"),
		Columns: reading.columns(), Rows: reading.rows(),
		Foot: reading.list("Foot"), Empty: reading.text("Empty"),
		Dense: reading.flag("Dense"), Bounded: reading.flag("Bounded"),
	}
	return table, prefixed("data-table", reading.err)
}

// DrawTable reads data and lays it out: the template function behind
// data-table.
func DrawTable(data any) (TablePlot, error) {
	table, err := ReadTable(data)
	if err != nil {
		return TablePlot{}, err
	}
	return table.Plot()
}

// Plot checks the table and lays out its cells.
func (table Table) Plot() (TablePlot, error) {
	if err := table.check(); err != nil {
		return TablePlot{}, err
	}
	plot := TablePlot{
		ID: table.ID, Title: table.Title, Empty: table.Empty,
		Dense: table.Dense, Bounded: table.Bounded, Columns: table.Columns,
	}
	for _, row := range table.Rows {
		plot.Rows = append(plot.Rows, table.cells(row))
	}
	if len(table.Foot) > 0 {
		foot := slices.Concat(table.Foot, make([]any,
			len(table.Columns)-len(table.Foot)))
		plot.Foot = table.cells(foot)
	}
	return plot, nil
}

func (table Table) check() error {
	checking := check{partial: "data-table"}
	checking.id(table.ID)
	checking.required("Title", table.Title, "name the table; it is "+
		"its heading")
	if len(table.Columns) == 0 {
		checking.fail(missingMsg, "Columns", "give the table its columns")
	}
	for _, column := range table.Columns {
		checking.required("a column's Label", column.Label,
			"give every column a heading")
		if column.Sort != "" && column.Sort != "ascending" &&
			column.Sort != "descending" {
			checking.fail(sortMsg, column.Label, column.Sort)
		}
	}
	for index, row := range table.Rows {
		if len(row) != len(table.Columns) {
			checking.fail(rowLengthMsg, index+1, len(row),
				len(table.Columns))
		}
	}
	if len(table.Foot) > len(table.Columns) {
		checking.fail(footLengthMsg, len(table.Foot), len(table.Columns))
	}
	return checking.err
}

func (table Table) cells(row []any) []TableCell {
	cells := make([]TableCell, len(row))
	for index, value := range row {
		cell := cellOf(value)
		cells[index] = TableCell{Value: cell.Value, Href: cell.Href,
			Num: table.Columns[index].Num, Head: index == 0}
	}
	return cells
}

// cellOf is a value as a cell: a Cell, or an item with an Href and a
// Value, as a dict builds a linked cell, or any other value as it is.
func cellOf(value any) Cell {
	if cell, ok := value.(Cell); ok {
		return cell
	}
	href, linked := field(value, "Href")
	if text, ok := href.(string); linked && ok {
		content, _ := field(value, "Value")
		return Cell{Value: content, Href: text}
	}
	return Cell{Value: value}
}

func (reading *reader) columns() []Column {
	value, _ := field(reading.data, "Columns")
	if columns, ok := value.([]Column); ok {
		return columns
	}
	var columns []Column
	for _, item := range reading.items("Columns", value) {
		inner := reader{data: item}
		columns = append(columns, Column{
			Label: inner.text("Label"), Num: inner.flag("Num"),
			Href: inner.text("Href"), Sort: inner.text("Sort"),
		})
		if inner.err != nil {
			reading.fail("column %d: %v", len(columns), inner.err)
		}
	}
	return columns
}

func (reading *reader) rows() [][]any {
	value, _ := field(reading.data, "Rows")
	if rows, ok := value.([][]any); ok {
		return rows
	}
	var rows [][]any
	for _, item := range reading.items("Rows", value) {
		rows = append(rows, reading.items("a row", item))
	}
	return rows
}

func (reading *reader) list(key string) []any {
	value, _ := field(reading.data, key)
	return reading.items(key, value)
}

// items is a list's items: nil is none, and anything but a list is a
// problem.
func (reading *reader) items(key string, value any) []any {
	list := reflect.ValueOf(value)
	if value == nil {
		return nil
	}
	if list.Kind() != reflect.Slice && list.Kind() != reflect.Array {
		reading.fail(wrongTypeMsg, key, value, "a list")
		return nil
	}
	items := make([]any, list.Len())
	for index := range items {
		items[index] = list.Index(index).Interface()
	}
	return items
}
