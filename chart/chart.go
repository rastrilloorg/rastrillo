// Package chart is the model behind ui's data partials: the data
// table, the bar chart, the line chart and the sparkline. A partial's
// dict, or one of these types passed as its Chart, is read into the
// model, checked, and drawn from it: the marks, the axis labels, the
// hover titles and the table rows all come from one set of points, so
// the table a screen reader gets holds every value the plot draws.
//
// Values are exact: a number.Decimal, or an integer or a float written
// as its shortest exact decimal. Floats exist only for the geometry.
package chart

import "amadan.net/rastrillo/rastrillo/number"

// Point is one value of a series and its label. A Missing point, or
// one whose Value is "", is not zero: it breaks the line and reads
// "No data" in the table.
type Point struct {
	Label   string
	Value   number.Decimal
	Missing bool
}

// Bound is an optional figure: a goal, or an end of the axis. Set says
// whether there is one, so a goal of 0 is a goal.
type Bound struct {
	Value number.Decimal
	Set   bool
}

// Rounding rounds the figures a chart writes, half to even, to Places
// digits after the point, and writes that many. It is not
// number.Options.Places, which pads and never rounds; unset, every
// figure is written exactly.
type Rounding struct {
	Places int
	Set    bool
}

// Line is a line chart of one series over evenly spaced points, from
// first to last. ID names the chart on the page; its heading, its
// description and its table take their ids from it. Category and
// Measure head the table's two columns ("Week", "Sold"). The axis
// covers every value, Min and Max when set, the goal, and zero unless
// Min is set. Percent writes 50 as 50 %. Hover gives each point a
// tooltip with its value.
type Line struct {
	ID, Title, Desc   string
	Category, Measure string
	Points            []Point
	Goal              Bound
	GoalLabel         string
	Min, Max          Bound
	Round             Rounding
	Percent           bool
	Hover             bool
	Empty             string
}

// Bars is a bar chart of one series of values of zero or more, drawn
// as a table of name, bar and value, with every bar on one scale: from
// zero to the largest value, or to Max when it is larger.
type Bars struct {
	ID, Title, Desc   string
	Category, Measure string
	Points            []Point
	Max               Bound
	Round             Rounding
	Percent           bool
	Empty             string
}

// Spark is a sparkline: a small line and a caption. Period says what
// it covers ("Last 8 weeks") and Desc what the line does, a dip or a
// spike included, since the caption is all a screen reader gets.
type Spark struct {
	Period, Desc string
	Points       []Point
}
