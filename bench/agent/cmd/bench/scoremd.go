package main

import (
	"fmt"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// ranked is an authored function with the run it came from.
type ranked struct {
	run string
	counts.FuncCounts
}

// scoreMarkdown is the report: one row per run, the runs that could not
// be counted with why, and the worst and best functions by how tangled
// they are, which a person reads to see whether the number means it.
func scoreMarkdown(label string, rows []scoreRow) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Readability counts, %s\n\n", label)
	out.WriteString("Counts over the code the agent wrote, app code only; " +
		"its tests are counted apart, below. Each spread is median/90th " +
		"percentile over functions.\n\n")
	out.WriteString("| run | Rastrillo | files | code lines | funcs | " +
		"cognitive | nesting | lines | far short names | duplicated | " +
		"over limit | wide | long funcs | names | long files | " +
		"comment share | one-impl interfaces |\n")
	out.WriteString("|" + strings.Repeat("---|", 17) + "\n")
	var unscorable []scoreRow
	for _, row := range rows {
		if row.Unscorable != "" {
			unscorable = append(unscorable, row)
			continue
		}
		out.WriteString(scoreLine(row))
	}
	out.WriteString("\n## Tests\n\nThe test files the agent wrote, " +
		"counted as the app code is.\n\n| run | files | code lines | funcs " +
		"| wide | long funcs | names | long files | comment share |\n")
	out.WriteString("|" + strings.Repeat("---|", 9) + "\n")
	for _, row := range rows {
		if row.Unscorable == "" {
			out.WriteString(testLine(row))
		}
	}
	if len(unscorable) > 0 {
		out.WriteString("\n## Unscorable\n\n")
		for _, row := range unscorable {
			fmt.Fprintf(&out, "- %s/%s: %s\n",
				row.Round, row.Run, row.Unscorable)
		}
	}
	worst, best := extremes(rows, 5)
	out.WriteString("\n## Highest cognitive complexity\n\n")
	out.WriteString(functionTable(worst))
	out.WriteString("\n## Lowest cognitive complexity\n\n")
	out.WriteString("Ties go to the longer function.\n\n")
	out.WriteString(functionTable(best))
	return out.String()
}

func scoreLine(row scoreRow) string {
	spread := func(name string) string {
		one := row.App[name]
		return fmt.Sprintf("%.4g/%.4g", one.Median, one.P90)
	}
	interfaces := "unavailable"
	if row.Interfaces != nil {
		interfaces = fmt.Sprint(*row.Interfaces)
	}
	code := row.AppCode
	return fmt.Sprintf("| %s/%s | %s | %d | %d | %d | %s | %s | %s | %s "+
		"| %s | %s | %d | %d | %d | %d | %.2f | %s |\n",
		row.Round, row.Run, row.Version, code.Files, code.Lines,
		funcsOf(row, false), spread("cognitive"), spread("nesting"),
		spread("lines"), spread("far-short-names"), spread("duplicated"),
		spread("over-limit"), code.Offences[codelimits.Columns],
		code.Offences[codelimits.FuncLines], code.Offences[codelimits.Names],
		code.Offences[codelimits.FileLines], code.CommentShare, interfaces)
}

func testLine(row scoreRow) string {
	code := row.TestCode
	return fmt.Sprintf("| %s/%s | %d | %d | %d | %d | %d | %d | %d "+
		"| %.2f |\n", row.Round, row.Run, code.Files, code.Lines,
		funcsOf(row, true), code.Offences[codelimits.Columns],
		code.Offences[codelimits.FuncLines], code.Offences[codelimits.Names],
		code.Offences[codelimits.FileLines], code.CommentShare)
}

// funcsOf is how many of the run's functions are tests, or are not.
func funcsOf(row scoreRow, tests bool) int {
	funcs := 0
	for _, one := range row.Funcs {
		if one.Test == tests {
			funcs++
		}
	}
	return funcs
}

// extremes is the n most and the n least cognitively complex app
// functions over all the runs.
func extremes(rows []scoreRow, n int) (worst, best []ranked) {
	var all []ranked
	for _, row := range rows {
		for _, one := range row.Funcs {
			if !one.Test {
				all = append(all, ranked{row.Round + "/" + row.Run, one})
			}
		}
	}
	byTangle := func(a, b ranked) int {
		if a.Cognitive != b.Cognitive {
			return b.Cognitive - a.Cognitive
		}
		return b.Lines - a.Lines
	}
	slices.SortStableFunc(all, byTangle)
	n = min(n, len(all))
	worst = all[:n]
	// The simplest are the ties at the bottom, the longest first.
	floor := slices.Clone(all)
	slices.Reverse(floor)
	slices.SortStableFunc(floor, func(a, b ranked) int {
		if a.Cognitive != b.Cognitive {
			return a.Cognitive - b.Cognitive
		}
		return b.Lines - a.Lines
	})
	best = floor[:n]
	return worst, best
}

func functionTable(funcs []ranked) string {
	var out strings.Builder
	out.WriteString("| run | function | cognitive | nesting | lines |\n")
	out.WriteString("|---|---|---|---|---|\n")
	for _, one := range funcs {
		fmt.Fprintf(&out, "| %s | %s `%s` | %d | %d | %d |\n", one.run,
			one.Path, one.Key, one.Cognitive, one.Nesting, one.Lines)
	}
	return out.String()
}
