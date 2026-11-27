package chart

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/number"
)

// weeks is Fiesta's sample series: eight weeks of sales, week 4 with
// no data where missing says so.
func weeks(missing bool) []Point {
	var points []Point
	for index, value := range []number.Decimal{
		"120", "180", "260", "240", "390", "610", "650", "690",
	} {
		label := string(rune('1' + index))
		points = append(points, Point{Label: label, Value: value,
			Missing: missing && index == 3})
	}
	return points
}

func sales(points []Point) Line {
	return Line{ID: "sales", Title: "Weekly sales", Desc: "Sales rose.",
		Category: "Week", Measure: "Sold", Points: points}
}

func TestTheAxisStepsInOnesTwosOrFives(t *testing.T) {
	for _, test := range []struct {
		low, high string
		ticks     []number.Decimal
	}{
		{"0", "690", []number.Decimal{"800", "600", "400", "200", "0"}},
		{"0", "0.07", []number.Decimal{"0.08", "0.06", "0.04", "0.02",
			"0.00"}},
		{"-30", "45", []number.Decimal{"60", "40", "20", "0", "-20",
			"-40"}},
		{"0", "0", []number.Decimal{"1.0", "0.8", "0.6", "0.4", "0.2",
			"0.0"}},
	} {
		axis, err := niceScale(exact(number.Decimal(test.low)),
			exact(number.Decimal(test.high)))
		if err != nil || !slices.Equal(axis.ticks, test.ticks) {
			t.Errorf("%s to %s: ticks %q (%v), want %q", test.low,
				test.high, axis.ticks, err, test.ticks)
		}
	}
}

func TestRoundingIsHalfToEvenAndWritesItsPlaces(t *testing.T) {
	for _, test := range []struct {
		value  number.Decimal
		places int
		want   number.Decimal
	}{
		{"2.5", 0, "2"}, {"3.5", 0, "4"}, {"-2.5", 0, "-2"},
		{"1.005", 2, "1.00"}, {"1.015", 2, "1.02"}, {"120", 1, "120.0"},
		{"-0.04", 1, "0.0"}, {"9.96", 1, "10.0"},
	} {
		if got := rounded(test.value, test.places); got != test.want {
			t.Errorf("%s to %d places: %s, want %s", test.value,
				test.places, got, test.want)
		}
	}
}

// An integer past 2^53 is a count, not a float: it is written digit for
// digit, as {{number}} writes one.
func TestValuesAreTakenExactly(t *testing.T) {
	for value, want := range map[any]number.Decimal{
		int64(9007199254740993):      "9007199254740993",
		uint64(18446744073709551615): "18446744073709551615",
		0.1:                          "0.1",
		number.Decimal("12.50"):      "12.50",
		math.Copysign(0, -1):         "0",
	} {
		got, set, err := decimalOf(value)
		if err != nil || !set || got != want {
			t.Errorf("%v (%T): %q %v %v, want %q", value, value, got, set,
				err, want)
		}
	}
	for _, value := range []any{"4.1k", "1200", number.Decimal("1,200")} {
		if _, _, err := decimalOf(value); err == nil {
			t.Errorf("%q (%T) is taken as a number", value, value)
		}
	}
}

func TestAMissingPointBreaksTheLineAndIsNeverZero(t *testing.T) {
	plot, err := sales(weeks(true)).Plot()
	if err != nil {
		t.Fatal(err)
	}
	if len(plot.Runs) != 2 {
		t.Fatalf("runs %q, want the line broken in two at week 4",
			plot.Runs)
	}
	drawn := strings.Join(plot.Runs, " ")
	if strings.Contains(drawn, ",1000") || strings.Contains(drawn,
		"428.6,") {
		t.Errorf("week 4 is drawn: %q", plot.Runs)
	}
	if row := plot.Rows[3]; !row.Missing || row.Value != "" {
		t.Errorf("week 4's row is %+v, want Missing", row)
	}
}

// A point alone between gaps is a dot, or the line would not show it.
func TestAPointAloneBetweenGapsIsADot(t *testing.T) {
	points := weeks(false)
	points[2].Missing, points[4].Missing = true, true
	plot, err := sales(points).Plot()
	if err != nil {
		t.Fatal(err)
	}
	if len(plot.Lone) != 1 || !strings.HasPrefix(plot.Lone[0], "M428.6 ") {
		t.Errorf("lone dots %q, want week 4's", plot.Lone)
	}
}

func TestAGoalOfZeroIsAGoal(t *testing.T) {
	line := sales(weeks(false))
	line.Goal, line.GoalLabel = Bound{Value: "0", Set: true}, "Floor"
	plot, err := line.Plot()
	if err != nil {
		t.Fatal(err)
	}
	if !plot.HasGoal || plot.Goal != "M0 1000H1000" ||
		plot.GoalValue != "0" {
		t.Errorf("goal %v %q %q, want a line at zero", plot.HasGoal,
			plot.Goal, plot.GoalValue)
	}
	line.GoalLabel = ""
	if _, err := line.Plot(); err == nil ||
		!strings.Contains(err.Error(), "GoalLabel") {
		t.Errorf("a goal with no label: %v", err)
	}
}

// Min set lets the axis start above zero; the goal is always on it.
func TestTheAxisCoversTheGoalAndZeroUnlessMinIsSet(t *testing.T) {
	line := sales(weeks(false))
	line.Goal, line.GoalLabel = Bound{Value: "900", Set: true}, "Target"
	plot, _ := line.Plot()
	if !slices.Equal(plot.Ticks, []number.Decimal{
		"1000", "800", "600", "400", "200", "0"}) {
		t.Errorf("ticks %q, want 0 to 1000 for a goal of 900", plot.Ticks)
	}
	line = sales(weeks(false)[5:])
	line.Min = Bound{Value: "500", Set: true}
	plot, _ = line.Plot()
	if !slices.Equal(plot.Ticks, []number.Decimal{
		"700", "650", "600", "550", "500"}) {
		t.Errorf("ticks %q, want 500 to 700 with Min 500", plot.Ticks)
	}
}

func TestEightPointsKeepTheLabelsFiestaShows(t *testing.T) {
	var kept []string
	for _, label := range xLabels(weeks(false)) {
		if label.Keep {
			kept = append(kept, label.Text)
		}
	}
	if !slices.Equal(kept, []string{"1", "3", "5", "8"}) {
		t.Errorf("kept %q, want 1, 3, 5 and 8", kept)
	}
}

// Past a dozen points only the kept labels carry text, or 31 dates
// collide at any width; every point still has its place.
func TestALongSeriesLabelsOnlyTheKeptPoints(t *testing.T) {
	points := make([]Point, 31)
	for index := range points {
		points[index].Label = "Nov " + string(rune('A'+index))
	}
	labels := xLabels(points)
	var shown []int
	for index, label := range labels {
		if label.Text != "" {
			shown = append(shown, index)
		}
	}
	if len(labels) != 31 || !slices.Equal(shown,
		[]int{0, 6, 12, 18, 24, 30}) {
		t.Errorf("%d labels, text on %v", len(labels), shown)
	}
}

func TestTheChartsRefuseWhatTheyCannotDraw(t *testing.T) {
	tooMany := sales(make([]Point, 367))
	huge := "1" + strings.Repeat("0", 400)
	wide := sales([]Point{{Label: "a", Value: number.Decimal("-" + huge)},
		{Label: "b", Value: number.Decimal(huge)}})
	spaced := sales(weeks(false))
	spaced.ID = "weekly sales"
	untitled := sales(weeks(false))
	untitled.Desc = ""
	for name, test := range map[string]struct {
		line Line
		want string
	}{
		"367 points":     {tooMany, "367 points is more than the 366"},
		"a vast span":    {wide, "too wide a range"},
		"a spaced ID":    {spaced, `ID "weekly sales" is invalid`},
		"no description": {untitled, "Desc is empty"},
	} {
		_, err := test.line.Plot()
		if err == nil || !strings.Contains(err.Error(), test.want) ||
			!strings.HasPrefix(err.Error(), "chart-line: ") {
			t.Errorf("%s: %v, want an error saying %q", name, err, test.want)
		}
	}
}

func TestBarsRefuseANegativeValueAndMoreThanFifty(t *testing.T) {
	bars := Bars{ID: "sources", Title: "Sources", Category: "Source",
		Measure: "Tickets", Points: []Point{{Label: "Refunds",
			Value: "-3"}}}
	if _, err := bars.Plot(); err == nil ||
		!strings.Contains(err.Error(), `("Refunds") is -3`) {
		t.Errorf("a negative bar: %v", err)
	}
	bars.Points = make([]Point, 51)
	if _, err := bars.Plot(); err == nil ||
		!strings.Contains(err.Error(), "51 bars") {
		t.Errorf("51 bars: %v", err)
	}
}

// The bars share one scale; Max widens it, and all zeros draw empty
// bars rather than dividing by zero.
func TestBarsShareOneScale(t *testing.T) {
	bars := Bars{ID: "sources", Title: "Sources", Category: "Source",
		Measure: "Tickets", Points: []Point{{Label: "Online",
			Value: "412"}, {Label: "Box office", Value: "236"},
			{Label: "Newsletter", Value: "0"}, {Label: "Partners",
				Missing: true}}}
	widths := func() []string {
		plot, err := bars.Plot()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range plot.Rows {
			got = append(got, row.Width)
		}
		return got
	}
	if got := widths(); !slices.Equal(got,
		[]string{"1000", "572.8", "0", ""}) {
		t.Fatalf("widths %q, want 1000, 572.8, 0 and none", got)
	}
	bars.Max = Bound{Value: "824", Set: true}
	if got := widths(); got[0] != "500" {
		t.Errorf("with Max 824 the largest bar is %q, want 500", got[0])
	}
	bars.Max, bars.Points = Bound{}, []Point{{Label: "a", Value: "0"}}
	if got := widths(); !slices.Equal(got, []string{"0"}) {
		t.Errorf("all zeros: %q", got)
	}
}

func TestATableRefusesRowsThatDoNotFitItsColumns(t *testing.T) {
	table := Table{ID: "sales", Title: "Sales", Columns: []Column{
		{Label: "Week"}, {Label: "Sold", Num: true}}}
	for name, test := range map[string]struct {
		rows [][]any
		foot []any
		want string
	}{
		"a short row":    {[][]any{{"1"}}, nil, "row 1 has 1 cells for 2"},
		"a long footer":  {nil, []any{"a", 1, 2}, "Foot has 3 cells for 2"},
		"a fitting foot": {[][]any{{"1", 2}}, []any{"Total"}, ""},
	} {
		table.Rows, table.Foot = test.rows, test.foot
		plot, err := table.Plot()
		switch {
		case test.want == "" && (err != nil || len(plot.Foot) != 2):
			t.Errorf("%s: %v, foot %v", name, err, plot.Foot)
		case test.want != "" && (err == nil ||
			!strings.Contains(err.Error(), test.want)):
			t.Errorf("%s: %v, want %q", name, err, test.want)
		}
	}
}

func TestASparklineNeedsItsPeriodAndDescription(t *testing.T) {
	spark := Spark{Period: "Last 8 weeks", Points: weeks(false)}
	if _, err := spark.Plot(); err == nil ||
		!strings.Contains(err.Error(), "Desc is empty") {
		t.Errorf("no description: %v", err)
	}
	spark.Desc = "Sales rose from 120 to 690, with a dip in week 4."
	plot, err := spark.Plot()
	if err != nil || plot.End != "M100 0h0" ||
		!strings.HasPrefix(plot.Runs[0], "0,24 ") {
		t.Errorf("%+v %v", plot, err)
	}
}

// A domain far below a float's range still spreads its marks: the axis
// steps come from the exact decimals, not from a span that underflows
// to zero (Astra, round 2).
func TestTheSmallestValuesStillSpreadTheirMarks(t *testing.T) {
	smallest := strconv.FormatFloat(math.SmallestNonzeroFloat64, 'f', -1,
		64)
	for _, tiny := range []string{smallest, "0." +
		strings.Repeat("0", 330) + "7", "0." + strings.Repeat("0", 400) +
		"3"} {
		plot, err := sales([]Point{{Label: "a", Value: "0"},
			{Label: "b", Value: number.Decimal(tiny)}}).Plot()
		if err != nil {
			t.Errorf("%.12s…: %v", tiny, err)
			continue
		}
		top := exact(plot.Ticks[0])
		ratio := new(big.Rat).Quo(top, exact(number.Decimal(tiny)))
		if ratio.Cmp(big.NewRat(1, 1)) < 0 ||
			ratio.Cmp(big.NewRat(2, 1)) > 0 {
			t.Errorf("%.12s…: the axis tops out at %.12s…, not within "+
				"twice the value", tiny, plot.Ticks[0])
		}
		if !strings.HasPrefix(plot.End, "M1000 ") ||
			strings.HasSuffix(plot.End, " 1000h0") {
			t.Errorf("%.12s…: the larger value is drawn at %s, on zero",
				tiny, plot.End)
		}
	}
}
