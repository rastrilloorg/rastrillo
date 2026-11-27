package chart

import (
	"fmt"
	"math/big"
	"regexp"

	"amadan.net/rastrillo/rastrillo/number"
)

// The most points a line or a sparkline draws, a year of days, and the
// most rows a bar chart draws. Past them a plot is a smear, and the
// table under it is no longer something a person reads.
const (
	maxPoints = 366
	maxBars   = 50
	maxPlaces = 20
)

// idText is an id that needs no escaping in a selector or an
// aria-labelledby list: a letter, then letters, digits, - and _.
var idText = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

const (
	missingMsg = "%s: %s is empty; %s"
	badIDMsg   = "%s: ID %q is invalid: start with a letter and use " +
		"only letters, digits, - and _"
	tooManyMsg = "%s: %d %s is more than the %d it draws; " +
		"aggregate them first"
	placesMsg   = "%s: Places is %d; set it to a value from 0 to %d"
	goalNameMsg = "%s: a Goal needs a GoalLabel, the words beside its " +
		"line (\"Target\")"
)

// A check gathers what is wrong with a chart before it is drawn, and
// says the first thing: what is wrong, where, and what to change.
type check struct {
	partial string
	err     error
}

func (checking *check) fail(format string, args ...any) {
	if checking.err == nil {
		checking.err = fmt.Errorf(format,
			append([]any{checking.partial}, args...)...)
	}
}

func (checking *check) required(key, value, why string) {
	if value == "" {
		checking.fail(missingMsg, key, why)
	}
}

func (checking *check) id(id string) {
	checking.required("ID", id, "give the chart an id unique on "+
		"the page, such as \"sales\"")
	if id != "" && !idText.MatchString(id) {
		checking.fail(badIDMsg, id)
	}
}

func (checking *check) count(points []Point, most int, what string) {
	if len(points) > most {
		checking.fail(tooManyMsg, len(points), what, most)
	}
}

func (checking *check) rounding(rounding Rounding) {
	if rounding.Set && (rounding.Places < 0 ||
		rounding.Places > maxPlaces) {
		checking.fail(placesMsg, rounding.Places, maxPlaces)
	}
}

// values checks each point's figure and returns the present ones,
// exact, in order, with nil for a missing one.
func (checking *check) values(points []Point) []*big.Rat {
	values := make([]*big.Rat, len(points))
	for index, point := range points {
		if point.Missing || point.Value == "" {
			continue
		}
		if _, _, err := checkedDecimal(point.Value); err != nil {
			checking.fail("%s: point %d (%q): %v", index+1, point.Label,
				err)
			continue
		}
		values[index] = exact(point.Value)
	}
	return values
}

func (checking *check) bound(key string, bound Bound) {
	if !bound.Set {
		return
	}
	if _, set, err := checkedDecimal(bound.Value); err != nil || !set {
		checking.fail("%s: %s: "+notDecimalMsg, key, bound.Value)
	}
}

// shown is a figure as the chart writes it: rounded when the chart
// rounds, exact otherwise.
func shown(value number.Decimal, rounding Rounding) number.Decimal {
	if !rounding.Set || value == "" {
		return value
	}
	return rounded(value, rounding.Places)
}

// Format is how a chart's template writes its figures: {{number $L
// .Value "places" .Places .Style}}, where Style is "percent" or
// "grouped".
type Format struct {
	Places int
	Style  string
}

func formatOf(rounding Rounding, percent bool) Format {
	format := Format{Style: "grouped"}
	if rounding.Set {
		format.Places = rounding.Places
	}
	if percent {
		format.Style = "percent"
	}
	return format
}

// Row is one row of a chart's table, and one point's hover title: its
// label and its figure as written, or Missing.
type Row struct {
	Label   string
	Value   number.Decimal
	Missing bool
}

func rowsOf(points []Point, values []*big.Rat, rounding Rounding) []Row {
	rows := make([]Row, len(points))
	for index, point := range points {
		rows[index] = Row{Label: point.Label, Missing: values[index] == nil}
		if values[index] != nil {
			rows[index].Value = shown(point.Value, rounding)
		}
	}
	return rows
}
