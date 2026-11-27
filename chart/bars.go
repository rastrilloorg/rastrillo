package chart

import (
	"math/big"
	"slices"
)

// BarsPlot is a bar chart as chart-bar draws it: one row per point, in
// order, each bar's width out of 1000. A row with no value has no bar.
type BarsPlot struct {
	ID, Title, Desc, Category, Measure, Empty string
	Format                                    Format
	Rows                                      []BarRow
}

// BarRow is one row of a bar chart.
type BarRow struct {
	Row
	Width string
}

const negativeMsg = "%s: point %d (%q) is %s; a bar chart draws " +
	"values of zero or more"

// ReadBars reads chart-bar's data: a Bars as its Chart, or the keys
// Bars has, with Max as a number and Places as a count.
func ReadBars(data any) (Bars, error) {
	if bars, ok := typed[Bars](data); ok {
		return bars, nil
	}
	reading := reader{data: data}
	bars := Bars{
		ID: reading.text("ID"), Title: reading.text("Title"),
		Desc: reading.text("Desc"), Category: reading.text("Category"),
		Measure: reading.text("Measure"), Points: reading.points(),
		Max: reading.bound("Max"), Round: reading.rounding(),
		Percent: reading.flag("Percent"), Empty: reading.text("Empty"),
	}
	return bars, prefixed("chart-bar", reading.err)
}

// DrawBars reads data and draws it: the template function behind
// chart-bar.
func DrawBars(data any) (BarsPlot, error) {
	bars, err := ReadBars(data)
	if err != nil {
		return BarsPlot{}, err
	}
	return bars.Plot()
}

// Plot checks the bars and works out every row: the bars share one
// scale, from zero to the largest value or Max, whichever is larger.
func (bars Bars) Plot() (BarsPlot, error) {
	values, err := bars.check()
	if err != nil {
		return BarsPlot{}, err
	}
	plot := BarsPlot{
		ID: bars.ID, Title: bars.Title, Desc: bars.Desc,
		Category: bars.Category, Measure: bars.Measure, Empty: bars.Empty,
		Format: formatOf(bars.Round, bars.Percent),
	}
	top := bars.top(values)
	for index, row := range rowsOf(bars.Points, values, bars.Round) {
		bar := BarRow{Row: row}
		switch {
		case values[index] == nil:
		case top.Sign() == 0:
			bar.Width = "0"
		default:
			bar.Width = coordinate(along(values[index], new(big.Rat), top,
				1000))
		}
		plot.Rows = append(plot.Rows, bar)
	}
	return plot, nil
}

func (bars Bars) check() ([]*big.Rat, error) {
	checking := check{partial: "chart-bar"}
	checking.id(bars.ID)
	checking.required("Title", bars.Title, "name the chart; it is "+
		"its heading")
	checking.required("Category", bars.Category, "give the names "+
		"column a heading (\"Source\")")
	checking.required("Measure", bars.Measure, "give the values "+
		"column a heading (\"Tickets\")")
	checking.count(bars.Points, maxBars, "bars")
	checking.rounding(bars.Round)
	checking.bound("Max", bars.Max)
	values := checking.values(bars.Points)
	for index, value := range values {
		if value != nil && value.Sign() < 0 {
			checking.fail(negativeMsg, index+1, bars.Points[index].Label,
				bars.Points[index].Value)
		}
	}
	return values, checking.err
}

// top is the end of the bars' scale. With no value above zero every
// bar is empty, rather than a division by zero.
func (bars Bars) top(values []*big.Rat) *big.Rat {
	top := new(big.Rat)
	if bars.Max.Set {
		top = exact(bars.Max.Value)
	}
	for _, value := range slices.DeleteFunc(slices.Clone(values),
		func(value *big.Rat) bool { return value == nil }) {
		if value.Cmp(top) > 0 {
			top = value
		}
	}
	return top
}
