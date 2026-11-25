package main

import (
	"fmt"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// comprehensionMarkdown is the report: each read of each app, the
// answers question by question, effort between runs on one Rastrillo
// version, the noise check, and what the budget should be.
func comprehensionMarkdown(label string, report comprehension) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Comprehension, %s\n\n", label)
	fmt.Fprintf(&out, "Read by `bench read` with %s at %s effort, "+
		"%d reads of each question per app, budget %d tokens (input, "+
		"cached included, plus output; a reading over it is wrong). "+
		"Accuracy is right answers over all the brief's questions: an "+
		"unsettled key's question is not read and counts as not right. "+
		"Median tokens and commands are over right answers.\n\n",
		report.Model, report.Effort, report.Reads, report.Budget)
	out.WriteString(runsTable(report.Apps))
	out.WriteString(answersTable(report.Apps))
	out.WriteString(effortTable(report.Apps))
	out.WriteString(noiseSection(report))
	out.WriteString(budgetSection(report))
	if len(report.Failed) > 0 {
		out.WriteString("\n## Failed readings\n\nNot saved; running " +
			"again asks them again.\n\n")
		for _, failure := range report.Failed {
			fmt.Fprintf(&out, "- %s\n",
				strings.ReplaceAll(firstLine(failure), "|", "\\|"))
		}
	}
	return out.String()
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func runsTable(apps []comprehend.AppReads) string {
	var out strings.Builder
	out.WriteString("## Runs\n\n| Round | Run | Rastrillo | Read | " +
		"Accuracy | Median tokens | Median commands | Unsettled | " +
		"Not built |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, app := range apps {
		run := app.Run
		if !app.Complete() {
			run += " (incomplete)"
		}
		for read, reading := range app.Reads {
			fmt.Fprintf(&out, "| %s | %s | %s | %d | %.2f | %.0f | %.1f "+
				"| %d | %d |\n", app.Round, run, app.Version, read+1,
				reading.Accuracy, reading.MedianTokens,
				reading.MedianCommands, reading.Unsettled, reading.Missing)
		}
	}
	return out.String()
}

// answersTable is, per question and app, how many reads got it right:
// where the readers stumble, and whether they stumble alike.
func answersTable(apps []comprehend.AppReads) string {
	var out strings.Builder
	out.WriteString("\n## Answers right, by question\n\nRight reads " +
		"over reads; - where the key is unsettled.\n\n| Question |")
	for _, app := range apps {
		fmt.Fprintf(&out, " %s %s |", app.Round,
			strings.TrimPrefix(app.Run, app.Brief+"-"))
	}
	out.WriteString("\n|---|" + strings.Repeat("---|", len(apps)) + "\n")
	for _, question := range questionIDs(apps) {
		fmt.Fprintf(&out, "| %s |", question)
		for _, app := range apps {
			right, read := 0, 0
			for _, reading := range app.Reads {
				for _, answer := range reading.Readings {
					if answer.QuestionID != question {
						continue
					}
					read++
					if answer.Correct {
						right++
					}
				}
			}
			if read == 0 {
				out.WriteString(" - |")
			} else {
				fmt.Fprintf(&out, " %d/%d |", right, read)
			}
		}
		out.WriteString("\n")
	}
	return out.String()
}

func questionIDs(apps []comprehend.AppReads) []string {
	var ids []string
	for _, app := range apps {
		for _, reading := range app.Reads {
			for _, answer := range reading.Readings {
				if !slices.Contains(ids, answer.QuestionID) {
					ids = append(ids, answer.QuestionID)
				}
			}
		}
	}
	return ids
}

// effortTable compares every two runs of one brief on one Rastrillo
// version, read by read, on the questions both got right.
func effortTable(apps []comprehend.AppReads) string {
	var out strings.Builder
	out.WriteString("\n## Effort between runs\n\nOnly runs of one brief " +
		"on one Rastrillo version are compared, read against read, on " +
		"the questions both answered right.\n\n| Rastrillo | Runs | Read " +
		"| Both right | Median tokens | Median commands |\n" +
		"|---|---|---|---|---|---|\n")
	for i, first := range apps {
		for _, second := range apps[i+1:] {
			if first.Brief != second.Brief || first.Version != second.Version {
				continue
			}
			for read := range min(len(first.Reads), len(second.Reads)) {
				effort := comprehend.CompareEffort(
					first.Reads[read].Readings, second.Reads[read].Readings)
				fmt.Fprintf(&out, "| %s | %s/%s vs %s | %d | %d "+
					"| %.0f vs %.0f | %.1f vs %.1f |\n", first.Version,
					first.Round, first.Run, second.Run, read+1,
					effort.Questions, effort.Tokens[0], effort.Tokens[1],
					effort.Commands[0], effort.Commands[1])
			}
		}
	}
	return out.String()
}

func noiseSection(report comprehension) string {
	var out strings.Builder
	out.WriteString("\n## Noise\n\nFor each measure, the median absolute " +
		"difference between two reads of one app must be at most half " +
		"the median absolute difference between runs of one brief (on " +
		"one Rastrillo version). A measure that fails is inconclusive " +
		"for every decision until it passes, and so is one whose runs do " +
		"not differ at all. Accuracy here grades an answer over the " +
		"budget on what it says, so token noise cannot pass for a " +
		"difference in understanding. Effort differences are on " +
		"the questions both reads, or both runs, got right.\n\n")
	out.WriteString("### Between the reads of each app\n\n| Round | Run " +
		"| Accuracy | Tokens | Commands |\n|---|---|---|---|---|\n")
	for _, app := range report.Apps {
		fmt.Fprintf(&out, "| %s | %s |", app.Round, app.Run)
		for _, measure := range comprehend.Measures {
			out.WriteString(" " + spread(measure,
				comprehend.ReadDifferences(measure, app)) + " |")
		}
		out.WriteString("\n")
	}
	out.WriteString("\n### Between the runs of each brief\n\n| Brief " +
		"| Accuracy | Tokens | Commands |\n|---|---|---|---|\n")
	for _, brief := range briefsOf(report.Apps) {
		var ofBrief []comprehend.AppReads
		for _, app := range report.Apps {
			if app.Brief == brief {
				ofBrief = append(ofBrief, app)
			}
		}
		fmt.Fprintf(&out, "| %s |", brief)
		for _, measure := range comprehend.Measures {
			out.WriteString(" " + spread(measure,
				comprehend.RunDifferences(measure, ofBrief)) + " |")
		}
		out.WriteString("\n")
	}
	var incomplete []string
	for _, app := range report.Apps {
		if !app.Complete() {
			incomplete = append(incomplete, fmt.Sprintf("%s/%s (%d missing)",
				app.Round, app.Run, app.MissingReadings))
		}
	}
	if len(incomplete) > 0 {
		out.WriteString("\nIncomplete, so left out of the gate: " +
			strings.Join(incomplete, ", ") + ". An app is complete when " +
			"every read holds a reading of every settled question.\n")
	}
	out.WriteString("\n### The gate\n\n| Measure | Between reads " +
		"| Between runs | Pairs (reads, runs) | Verdict |\n" +
		"|---|---|---|---|---|\n")
	for _, gate := range report.Gates {
		verdict := gate.Verdict
		if !gate.Passes {
			verdict += ": inconclusive"
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %d, %d | %s |\n", gate.Measure,
			number(gate.Measure, gate.Reads), number(gate.Measure, gate.Runs),
			gate.ReadPairs, gate.RunPairs, verdict)
	}
	return out.String()
}

// spread is the median of differences, with how many there were.
func spread(measure string, differences []float64) string {
	if len(differences) == 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%d)",
		number(measure, comprehend.Median(differences)), len(differences))
}

func number(measure string, value float64) string {
	switch measure {
	case "accuracy", "commands":
		return fmt.Sprintf("%.2f", value)
	}
	return fmt.Sprintf("%.0f", value)
}

func briefsOf(apps []comprehend.AppReads) []string {
	var briefs []string
	for _, app := range apps {
		if !slices.Contains(briefs, app.Brief) {
			briefs = append(briefs, app.Brief)
		}
	}
	return briefs
}

// budgetSection suggests the budget: the 95th percentile of tokens on
// right answers, before any budget, rounded up to the next 10,000.
func budgetSection(report comprehension) string {
	budget := report.Budgeting
	return fmt.Sprintf("\n## Budget\n\n95th percentile of tokens on the "+
		"%d answers right before any budget: %.0f, so -budget %d (rounded "+
		"up to the next 10,000). Right answers this run's budget of %d "+
		"made wrong: %d.\n", budget.RightAnswers, budget.Percentile95,
		budget.Suggested, report.Budget, budget.Cut)
}
