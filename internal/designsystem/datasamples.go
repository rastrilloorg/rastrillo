package designsystem

import (
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

// componentStylesheetLinks links each component stylesheet a preview's
// body shows, after its theme, as an app's layout does: data.css on a
// table's or a chart's preview, and on no other.
func componentStylesheetLinks(mount, body string) string {
	var links strings.Builder
	shown := vendored.HooksIn([]byte(body))
	for _, sheet := range vendored.NeededStylesheets(shown) {
		links.WriteString(`<link rel="stylesheet" href="` +
			assetURL(mount, sheet) + `">` + "\n")
	}
	return links.String()
}

// dataFamily is the data table and the three charts, a page of their
// own: Fiesta 1.10's data.css, which each preview links only when it
// shows one. On the Display page their four sections took every
// language's page past its byte budget. It sits in its own function
// because families is already past the function limit. The figures
// are Fiesta's own examples', so the two galleries show the same
// eight weeks.
func dataFamily() family {
	return family{
		Key:   "tables-and-charts",
		Title: "Tables and charts",
		Blurb: "A table of figures and three charts: a bar chart, a " +
			"line chart and a sparkline. The server draws them without " +
			"JavaScript.",
		Partials: dataDocs(),
	}
}

func dataDocs() []partialDoc {
	return []partialDoc{
		{
			Name: "data-table",
			Blurb: "A table of figures in a card. On a narrow screen, " +
				"scroll the card sideways to see every column. Row " +
				"headers stay in view.",
			States: dataTableStates(),
		},
		{
			Name: "chart-bar",
			Blurb: "A bar chart drawn as a table: a name, a bar and a " +
				"value in each row. Values must be zero or more.",
			States: chartBarStates(),
		},
		{
			Name: "chart-line",
			Blurb: "One series as a line over evenly spaced points, " +
				"with every value in a table under it.",
			States: chartLineStates(),
		},
		{
			Name: "sparkline",
			Blurb: "A small line beside its caption, for a trend next " +
				"to a number.",
			States: []sample{{State: "Rising, with a dip",
				Data: map[string]any{
					"Period": "Last 8 weeks",
					"Desc": "Sales rose from 120 to 690, with a dip " +
						"in week 4.",
					"Points": weekPoints(-1),
				},
				Note: "The caption is all a screen reader gets. Give " +
					"the period, and describe any dip or spike."}},
		},
	}
}

// weekSales is the eight weeks of sales Fiesta's examples draw.
var weekSales = []int{120, 180, 260, 240, 390, 610, 650, 690}

// weekPoints is weekSales as points, with week gap missing (-1 for
// none).
func weekPoints(gap int) []any {
	var points []any
	for index, sold := range weekSales {
		point := map[string]any{"Label": index + 1, "Value": sold}
		if index+1 == gap {
			point["Missing"] = true
		}
		points = append(points, point)
	}
	return points
}

// salesColumns are the weekly table's columns, sortable by Sold and
// Target, with the list sorted by Sold when sorted is true.
func salesColumns(sorted bool) []any {
	sold := map[string]any{"Label": "Sold", "Num": true}
	target := map[string]any{"Label": "Target", "Num": true}
	if sorted {
		sold["Href"], sold["Sort"] = "/sales?sort=sold&dir=asc", "descending"
		target["Href"] = "/sales?sort=target&dir=desc"
	}
	return []any{map[string]any{"Label": "Week"}, sold, target,
		map[string]any{"Label": "Refunds", "Num": true}}
}

// salesRows are the weeks as table rows: week, sold, target, refunds.
func salesRows(weeks ...int) []any {
	refunds := []int{2, 0, 5, 1, 3, 4, 0, 2}
	var rows []any
	for _, week := range weeks {
		rows = append(rows, []any{week, weekSales[week-1], 600,
			refunds[week-1]})
	}
	return rows
}

func dataTableStates() []sample {
	return []sample{
		{State: "Sorted by a number", Data: map[string]any{
			"ID": "sales", "Title": "Sales by week",
			"Columns": salesColumns(true), "Rows": salesRows(3, 4, 2, 1),
			"Foot": []any{"Total", 800, 2400, 8},
		}, Note: "Link a column heading to the sorted list, then set " +
			"Sort on that column. Mark number columns with Num."},
		{State: "Dense, with a fixed header", Data: map[string]any{
			"ID": "sales-dense", "Title": "Sales by week",
			"Dense": true, "Bounded": true, "Columns": salesColumns(false),
			"Rows": salesRows(1, 2, 3, 4, 5, 6, 7, 8),
		}, Note: "Dense tightens the rows. Bounded caps the height and " +
			"keeps the header in view while the rows scroll."},
		{State: "No rows match", Data: map[string]any{
			"ID": "sales-none", "Title": "Sales by week",
			"Columns": salesColumns(false), "Empty": "No weeks match.",
		}, Note: "With no rows, the header stays and Empty says why."},
	}
}

func chartBarStates() []sample {
	sources := []any{}
	for _, source := range []struct {
		name string
		sold int
	}{{"Online", 412}, {"Box office", 236}, {"Partners", 98},
		{"Newsletter", 0}} {
		sources = append(sources, map[string]any{
			"Label": source.name, "Value": source.sold})
	}
	chart := map[string]any{"ID": "sources", "Title": "Tickets by source",
		"Desc": "Most tickets sold online, and none through the " +
			"newsletter yet.",
		"Category": "Source", "Measure": "Tickets", "Points": sources}
	return []sample{
		{State: "Four sources on one scale", Data: chart,
			Note: "Every bar shares one scale. To compare two charts, " +
				"give both the same Max."},
		{State: "Nothing sold yet", Data: map[string]any{
			"ID": "sources-none", "Title": "Tickets by source",
			"Category": "Source", "Measure": "Tickets",
			"Empty": "No tickets sold yet.",
		}, Note: "With no points, Empty takes the chart's place."},
	}
}

func chartLineStates() []sample {
	line := func(id, desc string, gap int) map[string]any {
		return map[string]any{"ID": id, "Title": "Weekly sales",
			"Desc": desc, "Category": "Week", "Measure": "Sold",
			"Points": weekPoints(gap), "Goal": 600, "GoalLabel": "Target",
			"Hover": gap < 0}
	}
	return []sample{
		{State: "Weekly sales against a target", Data: line("weekly",
			"Sales rose almost every week and passed the target of 600 "+
				"in week 6.", -1),
			Note: "Write Desc yourself. Say what the line does, not " +
				"only where it ends. Set Hover to add a tooltip to each " +
				"point."},
		{State: "A week with no data", Data: line("weekly-gap",
			"No sales were recorded in week 5; the line breaks there.", 5),
			Note: "A missing value breaks the line and reads No data in " +
				"the table. It is never drawn as 0."},
	}
}
