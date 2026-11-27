package chart

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/number"
)

// LinePlot is a line chart as chart-line draws it. The plot's viewBox
// is 0 0 1000 1000, stretched to its box. Rows are the table's rows,
// one per point in order, and the hover titles; NoData puts the empty
// sentence in place of the plot.
type LinePlot struct {
	ID, Title, Desc, Category, Measure, Empty string
	Format, TickFormat                        Format
	Rows                                      []Row
	NoData, Hover                             bool
	Ticks                                     []number.Decimal
	Grid, Base, Goal                          string
	Runs, Lone                                []string
	End                                       string
	X                                         []AxisLabel
	GoalLabel                                 string
	GoalValue                                 number.Decimal
	HasGoal                                   bool
}

// AxisLabel is one point's place on the x axis. Every point has one,
// so the labels line up with the points; Keep marks those a narrow
// chart still shows, and Text is empty on a long series for the rest.
type AxisLabel struct {
	Text string
	Keep bool
}

// ReadLine reads chart-line's data: a Line as its Chart, or the keys
// Line has, with Goal, Min and Max as numbers and Places as a count.
func ReadLine(data any) (Line, error) {
	if line, ok := typed[Line](data); ok {
		return line, nil
	}
	reading := reader{data: data}
	line := Line{
		ID: reading.text("ID"), Title: reading.text("Title"),
		Desc: reading.text("Desc"), Category: reading.text("Category"),
		Measure: reading.text("Measure"), Points: reading.points(),
		Goal: reading.bound("Goal"), GoalLabel: reading.text("GoalLabel"),
		Min: reading.bound("Min"), Max: reading.bound("Max"),
		Round: reading.rounding(), Percent: reading.flag("Percent"),
		Hover: reading.flag("Hover"), Empty: reading.text("Empty"),
	}
	return line, prefixed("chart-line", reading.err)
}

// DrawLine reads data and draws it: the template function behind
// chart-line.
func DrawLine(data any) (LinePlot, error) {
	line, err := ReadLine(data)
	if err != nil {
		return LinePlot{}, err
	}
	return line.Plot()
}

// Plot checks the line and works out everything chart-line writes.
func (line Line) Plot() (LinePlot, error) {
	values, err := line.check()
	if err != nil {
		return LinePlot{}, err
	}
	plot := LinePlot{
		ID: line.ID, Title: line.Title, Desc: line.Desc,
		Category: line.Category, Measure: line.Measure, Empty: line.Empty,
		Format: formatOf(line.Round, line.Percent), Hover: line.Hover,
		Rows: rowsOf(line.Points, values, line.Round), X: xLabels(line.Points),
		GoalLabel: line.GoalLabel, HasGoal: line.Goal.Set,
		GoalValue: shown(line.Goal.Value, line.Round),
	}
	plot.NoData = !slices.ContainsFunc(values, present)
	if plot.NoData {
		return plot, nil
	}
	axis, err := niceScale(line.domain(values))
	if err != nil {
		return LinePlot{}, prefixed("chart-line", err)
	}
	plot.draw(line, values, axis)
	return plot, nil
}

func (line Line) check() ([]*big.Rat, error) {
	checking := check{partial: "chart-line"}
	checking.id(line.ID)
	checking.required("Title", line.Title, "name the chart; it is "+
		"its heading")
	checking.required("Desc", line.Desc, "describe the line in a "+
		"sentence, including any dip or spike")
	checking.required("Category", line.Category, "give the table's "+
		"first column a heading (\"Week\")")
	checking.required("Measure", line.Measure, "give the table's "+
		"value column a heading (\"Sold\")")
	checking.count(line.Points, maxPoints, "points")
	checking.rounding(line.Round)
	checking.bound("Goal", line.Goal)
	checking.bound("Min", line.Min)
	checking.bound("Max", line.Max)
	if line.Goal.Set && line.GoalLabel == "" {
		checking.fail(goalNameMsg)
	}
	values := checking.values(line.Points)
	return values, checking.err
}

// domain is what the axis must cover: every value, the goal, Min and
// Max when set, and zero unless Min is set.
func (line Line) domain(values []*big.Rat) (low, high *big.Rat) {
	covered := []*big.Rat{}
	for _, value := range values {
		if value != nil {
			covered = append(covered, value)
		}
	}
	for _, bound := range []Bound{line.Goal, line.Min, line.Max} {
		if bound.Set {
			covered = append(covered, exact(bound.Value))
		}
	}
	if !line.Min.Set {
		covered = append(covered, new(big.Rat))
	}
	return extent(covered)
}

func extent(values []*big.Rat) (low, high *big.Rat) {
	low, high = values[0], values[0]
	for _, value := range values[1:] {
		if value.Cmp(low) < 0 {
			low = value
		}
		if value.Cmp(high) > 0 {
			high = value
		}
	}
	return low, high
}

// draw works out the plot's marks and axes on axis.
func (plot *LinePlot) draw(line Line, values []*big.Rat, axis scale) {
	plot.Ticks = axis.ticks
	// No padding: each tick's exact decimal carries its own digits,
	// past the 20 places {{number}} pads to.
	plot.TickFormat = Format{Style: plot.Format.Style}
	level := func(value *big.Rat) string {
		top := along(value, axis.low, axis.high, 1000)
		return coordinate(top.Sub(big.NewRat(1000, 1), top))
	}
	var grid strings.Builder
	for _, tick := range axis.ticks[:len(axis.ticks)-1] {
		grid.WriteString("M0 " + level(exact(tick)) + "H1000")
	}
	plot.Grid, plot.Base = grid.String(), "M0 1000H1000"
	if line.Goal.Set {
		plot.Goal = "M0 " + level(exact(line.Goal.Value)) + "H1000"
	}
	drawn := drawMarks(values, axis.low, axis.high, 1000, 1000)
	plot.Runs, plot.Lone, plot.End = drawn.runs, drawn.lone, drawn.end
}

// xLabels gives every point its place on the x axis, and marks the
// ones a narrow chart keeps: the first, the last, and every k-th, but
// none within k of the last, so about six stay. Past a dozen points
// only those carry text at any width, so labels never collide.
func xLabels(points []Point) []AxisLabel {
	last := len(points) - 1
	every := max(1, (last+4)/5)
	labels := make([]AxisLabel, len(points))
	for index, point := range points {
		keep := index == 0 || index == last ||
			index%every == 0 && last-index >= every
		labels[index] = AxisLabel{Keep: keep}
		if keep || len(points) <= 12 {
			labels[index].Text = point.Label
		}
	}
	return labels
}

// prefixed puts the partial's name before an error that has none.
func prefixed(partial string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", partial, err)
}
