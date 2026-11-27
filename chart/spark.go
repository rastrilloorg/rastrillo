package chart

import (
	"math/big"
	"slices"
)

// SparkPlot is a sparkline as the sparkline partial draws it, in a
// viewBox of 0 0 100 24 from its lowest value at the bottom to its
// highest at the top. NoData leaves the caption alone.
type SparkPlot struct {
	Period, Desc string
	Runs, Lone   []string
	End          string
	NoData       bool
}

// ReadSpark reads sparkline's data: a Spark as its Chart, or the keys
// Spark has.
func ReadSpark(data any) (Spark, error) {
	if spark, ok := typed[Spark](data); ok {
		return spark, nil
	}
	reading := reader{data: data}
	spark := Spark{
		Period: reading.text("Period"), Desc: reading.text("Desc"),
		Points: reading.points(),
	}
	return spark, prefixed("sparkline", reading.err)
}

// DrawSpark reads data and draws it: the template function behind
// sparkline.
func DrawSpark(data any) (SparkPlot, error) {
	spark, err := ReadSpark(data)
	if err != nil {
		return SparkPlot{}, err
	}
	return spark.Plot()
}

// Plot checks the sparkline and draws it. A line of one value all
// along is drawn across the middle.
func (spark Spark) Plot() (SparkPlot, error) {
	checking := check{partial: "sparkline"}
	checking.required("Period", spark.Period, "say what period the "+
		"line covers (\"Last 8 weeks\")")
	checking.required("Desc", spark.Desc, "describe what the line "+
		"does, including any dip or spike; its end values alone do "+
		"not describe it")
	checking.count(spark.Points, maxPoints, "points")
	values := checking.values(spark.Points)
	if checking.err != nil {
		return SparkPlot{}, checking.err
	}
	plot := SparkPlot{Period: spark.Period, Desc: spark.Desc}
	shown := slices.DeleteFunc(slices.Clone(values),
		func(value *big.Rat) bool { return value == nil })
	if len(shown) == 0 {
		plot.NoData = true
		return plot, nil
	}
	low, high := extent(shown)
	if low.Cmp(high) == 0 {
		low = new(big.Rat).Sub(low, big.NewRat(1, 1))
		high = new(big.Rat).Add(high, big.NewRat(1, 1))
	}
	drawn := drawMarks(values, low, high, 100, 24)
	plot.Runs, plot.Lone, plot.End = drawn.runs, drawn.lone, drawn.end
	return plot, nil
}
