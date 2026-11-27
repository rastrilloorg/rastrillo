package chart_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/chart"
	"amadan.net/rastrillo/rastrillo/number"
)

// weekly is the line Fiesta's example draws, as a dict: eight weeks of
// sales against a target of 600, week 5 with no data.
func weekly() map[string]any {
	var points []any
	for week, sold := range []any{120, 180, 260, 240, nil, 610, 650, 690} {
		points = append(points, map[string]any{"Label": week + 1,
			"Value": sold})
	}
	return map[string]any{
		"ID": "weekly", "Title": "Weekly sales", "Category": "Week",
		"Measure": "Sold", "Points": points, "Goal": 600,
		"GoalLabel": "Target", "Hover": true,
		"Desc": "No sales were recorded in week 5; the line breaks there.",
	}
}

func sources() map[string]any {
	return map[string]any{
		"ID": "sources", "Title": "Tickets by source", "Category": "Source",
		"Measure": "Tickets", "Points": []chart.Point{
			{Label: "Online", Value: "412"},
			{Label: "Box office", Value: "236"},
			{Label: "Partners", Missing: true},
			{Label: "Newsletter", Value: "0"},
		},
	}
}

var (
	tableRow = regexp.MustCompile(
		`<tr><th scope="row">([^<]*)</th>(?:<td rst-table-bar>.*?</td>)?` +
			`<td rst-num>([^<]*)</td></tr>`)
	footRow = regexp.MustCompile(`<tfoot>` + tableRow.String())
	hit     = regexp.MustCompile(`<span(?: title="([^"]*)")?></span>`)
	bar     = regexp.MustCompile(`<rect width="([^"]*)"`)
)

// tableOf is each row a chart's table holds: its header and its value.
func tableOf(page string) [][2]string {
	var rows [][2]string
	for _, match := range tableRow.FindAllStringSubmatch(page, -1) {
		rows = append(rows, [2]string{match[1], match[2]})
	}
	return rows
}

// The table a screen reader gets holds every point the plot draws, in
// order, with the missing one as No data and never 0, and the goal.
func TestEveryChartsTableHoldsEveryPointInOrder(t *testing.T) {
	line := renderIn(t, "en", "chart-line", weekly())
	want := [][2]string{{"1", "120"}, {"2", "180"}, {"3", "260"},
		{"4", "240"}, {"5", "No data"}, {"6", "610"}, {"7", "650"},
		{"8", "690"}, {"Target", "600"}}
	if got := tableOf(line); !slices.Equal(got, want) {
		t.Errorf("the line's table holds %q, want %q", got, want)
	}
	if goal := footRow.FindStringSubmatch(line); goal == nil ||
		goal[1] != "Target" {
		t.Errorf("the goal is not the table's footer:\n%s", line)
	}
	bars := renderIn(t, "en", "chart-bar", sources())
	want = [][2]string{{"Online", "412"}, {"Box office", "236"},
		{"Partners", "No data"}, {"Newsletter", "0"}}
	if got := tableOf(bars); !slices.Equal(got, want) {
		t.Errorf("the bar chart's table holds %q, want %q", got, want)
	}
}

// The marks come from the same points: one hover slice per point, the
// missing one untitled; one bar per value, none for the missing one.
func TestTheMarksAgreeWithTheTable(t *testing.T) {
	line := renderIn(t, "en", "chart-line", weekly())
	var titles []string
	for _, match := range hit.FindAllStringSubmatch(line, -1) {
		titles = append(titles, match[1])
	}
	want := []string{"1: 120", "2: 180", "3: 260", "4: 240", "",
		"6: 610", "7: 650", "8: 690"}
	if !slices.Equal(titles, want) {
		t.Errorf("hover titles %q, want %q", titles, want)
	}
	if got := strings.Count(line, "<polyline rst-chart-line"); got != 2 {
		t.Errorf("%d polylines, want the line broken in two", got)
	}
	if !strings.Contains(line, `<p rst-chart-key>Target: 600</p>`) {
		t.Errorf("no key names the goal:\n%s", line)
	}
	var widths []string
	bars := renderIn(t, "en", "chart-bar", sources())
	for _, match := range bar.FindAllStringSubmatch(bars, -1) {
		widths = append(widths, match[1])
	}
	if !slices.Equal(widths, []string{"1000", "572.8", "0"}) {
		t.Errorf("bars %q, want 1000, 572.8 and 0, and none for Partners",
			widths)
	}
}

// Rounding and Percent reach every figure the chart writes: the axis,
// the hover titles, the key and the table. 50 is 50 %.
func TestPlacesAndPercentReachEveryFigure(t *testing.T) {
	data := map[string]any{
		"ID": "uptime", "Title": "Uptime", "Desc": "Steady.",
		"Category": "Day", "Measure": "Up", "Percent": true, "Places": 1,
		"Hover": true, "Goal": number.Decimal("99.95"), "GoalLabel": "SLO",
		"Points": []chart.Point{{Label: "Mon", Value: "99.96"},
			{Label: "Tue", Value: "50"}},
	}
	page := renderIn(t, "de", "chart-line", data)
	for _, want := range []string{
		`<td rst-num>100,0 %</td>`, `<td rst-num>50,0 %</td>`,
		`title="Tue: 50,0 %"`, `<p rst-chart-key>SLO: 100,0 %</p>`,
		`<span>100 %</span>`,
	} {
		// German puts a no-break space before the percent sign.
		want = strings.ReplaceAll(want, " %", "\u00a0%")
		if !strings.Contains(page, want) {
			t.Errorf("no %s in:\n%s", want, page)
		}
	}
}

func TestTwoChartsOnOnePageShareNoID(t *testing.T) {
	second := weekly()
	second["ID"] = "monthly"
	page := renderIn(t, "en", "chart-line", weekly()) +
		renderIn(t, "en", "chart-line", second)
	ids := regexp.MustCompile(` id="([^"]*)"`).FindAllStringSubmatch(page, -1)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id[1]] {
			t.Errorf("id %q twice", id[1])
		}
		seen[id[1]] = true
	}
	for _, reference := range regexp.MustCompile(
		`aria-(?:labelledby|describedby)="([^"]*)"`).FindAllStringSubmatch(
		page, -1) {
		if !seen[reference[1]] {
			t.Errorf("%s names no element", reference[0])
		}
	}
}

func TestAChartWithNoValuesSaysSo(t *testing.T) {
	data := weekly()
	data["Points"] = []any{map[string]any{"Label": "1"}}
	data["Goal"], data["Empty"] = nil, "No sales yet."
	line := renderIn(t, "en", "chart-line", data)
	if strings.Contains(line, "rst-chart-plot") ||
		!strings.Contains(line, `<p rst-chart-empty>No sales yet.</p>`) {
		t.Errorf("an empty line draws a plot:\n%s", line)
	}
	bars := sources()
	bars["Points"] = nil
	page := renderIn(t, "en", "chart-bar", bars)
	if !strings.Contains(page, `<p rst-chart-empty>No data yet.</p>`) {
		t.Errorf("empty bars say:\n%s", page)
	}
}

func TestADataTableWritesItsCellsByColumn(t *testing.T) {
	page := renderIn(t, "en", "data-table", map[string]any{
		"ID": "sales", "Title": "Sales by week", "Dense": true,
		"Columns": []any{
			map[string]any{"Label": "Week"},
			map[string]any{"Label": "Sold", "Num": true,
				"Href": "/sales?sort=sold", "Sort": "descending"},
		},
		"Rows": []any{
			[]any{map[string]any{"Value": "Week 3", "Href": "/weeks/3"}, 1260},
			[]any{"Week 1", number.Decimal("120.5")},
		},
		"Foot": []any{"Total"},
	})
	for _, want := range []string{
		`<table rst-table="dense" aria-labelledby="sales-h">`,
		`<th scope="col" rst-num aria-sort="descending">` +
			`<a href="/sales?sort=sold">Sold<svg class="icon"`,
		`<th scope="row"><a href="/weeks/3">Week 3</a></th>` +
			`<td rst-num>1,260</td>`,
		`<td rst-num>120.5</td>`,
		`<tfoot><tr><th scope="row">Total</th><td rst-num></td>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s in:\n%s", want, page)
		}
	}
	empty := renderIn(t, "en", "data-table", map[string]any{
		"ID": "sales", "Title": "Sales", "Empty": "No weeks match.",
		"Columns": []chart.Column{{Label: "Week"}, {Label: "Sold"}},
	})
	if !strings.Contains(empty, `<tr rst-table-empty><td colspan="2">`+
		`No weeks match.</td></tr>`) {
		t.Errorf("an empty table says:\n%s", empty)
	}
}

// A typed chart passed as Chart draws as its dict does.
func TestATypedChartDrawsAsItsDict(t *testing.T) {
	typed := chart.Bars{ID: "sources", Title: "Tickets by source",
		Category: "Source", Measure: "Tickets",
		Points: sources()["Points"].([]chart.Point)}
	got := renderIn(t, "en", "chart-bar", map[string]any{"Chart": typed})
	if want := renderIn(t, "en", "chart-bar", sources()); got != want {
		t.Errorf("Chart draws\n%s\nwant\n%s", got, want)
	}
}

func TestARenderStopsOnWhatItCannotDraw(t *testing.T) {
	line := weekly()
	line["Points"] = []any{map[string]any{"Label": "1", "Value": "4.1k"}}
	spark := map[string]any{"Period": "Last 8 weeks",
		"Points": []chart.Point{{Label: "1", Value: "3"}}}
	table := map[string]any{"ID": "t", "Title": "T",
		"Columns": []any{map[string]any{"Label": "A"}},
		"Rows":    []any{[]any{1, 2}}}
	for partial, test := range map[string]struct {
		data map[string]any
		want string
	}{
		"chart-line": {line, "chart-line: point 1: 4.1k (string) is " +
			"not a number"},
		"sparkline":  {spark, "sparkline: Desc is empty"},
		"data-table": {table, "data-table: row 1 has 2 cells for 1"},
	} {
		err := renderError(t, partial, test.data)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want %q", partial, err, test.want)
		}
	}
}

func TestASparklineIsItsCaption(t *testing.T) {
	page := renderIn(t, "en", "sparkline", map[string]any{
		"Period": "Last 8 weeks",
		"Desc":   "Sales rose from 120 to 690, with a dip in week 4.",
		"Points": weekly()["Points"],
	})
	for _, want := range []string{
		`<figure rst-spark><svg viewBox="0 0 100 24"`, `aria-hidden="true"`,
		`<path rst-chart-dot d="M100 0h0"/>`,
		`<figcaption><strong>Last 8 weeks</strong> Sales rose`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s in:\n%s", want, page)
		}
	}
}

// chart_point carries the label and the value once each in every
// language, and the rest carry no placeholder: a lost {value} leaves a
// tooltip that names a week and no number.
func TestTheChartStringsAreInEveryCatalog(t *testing.T) {
	keys := map[string][]string{
		"rastrillo.ui.chart_numbers": nil, "rastrillo.ui.chart_no_data": nil,
		"rastrillo.ui.chart_empty": nil, "rastrillo.ui.chart_bar": nil,
		"rastrillo.ui.chart_point": {"{label}", "{value}"},
	}
	for code, catalog := range rastrillo.BaseCatalogs() {
		for key, placeholders := range keys {
			text := catalog[key]
			braces := strings.Count(text, "{")
			for _, placeholder := range placeholders {
				if strings.Count(text, placeholder) != 1 {
					t.Errorf("%s: %s = %q lacks %s", code, key, text,
						placeholder)
				}
			}
			if text == "" || braces != len(placeholders) {
				t.Errorf("%s: %s = %q", code, key, fmt.Sprint(text))
			}
		}
	}
}

// A value with more digits than {{number}} pads to still draws: each
// tick's exact decimal carries its own digits (Astra, round 2).
func TestAValueOfTwentyOneDecimalPlacesStillDraws(t *testing.T) {
	data := weekly()
	data["Goal"] = nil
	data["Points"] = []chart.Point{
		{Label: "1", Value: "0.00000000000000000001"},
		{Label: "2", Value: "0"},
	}
	page := renderIn(t, "en", "chart-line", data)
	for _, want := range []string{
		"<span>0.000000000000000000010</span>",
		"<td rst-num>0.00000000000000000001</td>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s in:\n%s", want, page)
		}
	}
}
