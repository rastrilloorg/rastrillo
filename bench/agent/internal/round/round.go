// Package round is what a round commits: every run's numbers, and the
// medians a person reads.
package round

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/classify"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/measure"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

type Cost struct {
	// Complete is false when the prompt never finished (a timeout, a
	// crash): claude wrote no result, so its cost is unknown. Unknown is
	// left out of the medians; counting it as $0 would make an arm whose
	// runs time out look cheap.
	Complete                                                     bool
	Messages, ToolCalls, NumTurns, Output, CacheRead, CacheWrite int
	CostUSD                                                      float64
	DurationS, FirstWriteContext, DeployMessages                 int
	Stages                                                       map[classify.Stage]classify.Tally
}

type RunRecord struct {
	Brief            string
	Arm              brief.Arm
	N                int
	App              string // the deployed app's name, for cleanup
	RastrilloVersion string // what the app's go.mod required, carlos arm only
	Build, Followup  Cost
	Measured         [2]measure.Measurement // after build, after follow-up
	AgentFailures    []string
	ReusedFrom       string // the round a reused control run came from
}

type Round struct {
	Label, Date, Model, ClaudeVersion string
	Rastrillo                         string
	Platform, Poll                    string
	GettingStartedCommit, PolicyHash  string
	// Config fingerprints everything that defines the experiment, so a
	// resumed round cannot quietly continue with a different Rastrillo,
	// framing or platform. ControlConfig covers what the control arm
	// depends on, which is what reusing its runs requires to match.
	Config, ControlConfig string
	Notes                 []string
	Runs                  []RunRecord
}

// CostOf reads a phase's cost off its transcript. Turns and dollars are
// claude's own figures; the rest are counted from the messages.
func CostOf(run transcript.Transcript, where classify.Where) Cost {
	cost := Cost{
		Messages:          len(run.Messages),
		FirstWriteContext: classify.FirstWriteContext(run, where),
		Stages:            classify.Summarise(run, where),
	}
	for _, message := range run.Messages {
		cost.ToolCalls += len(message.Tools)
	}
	cost.DeployMessages = cost.Stages[classify.Deploy].Messages +
		cost.Stages[classify.LiveCheck].Messages
	if result := run.Result; result != nil {
		cost.Complete = true
		cost.NumTurns = result.NumTurns
		cost.CostUSD = result.PhaseCostUSD
		cost.DurationS = int(result.DurationMS / 1000)
		cost.Output = result.Usage.Output
		cost.CacheRead = result.Usage.CacheRead
		cost.CacheWrite = result.Usage.CacheWrite
	}
	return cost
}

// Spread is one metric across an arm's runs. Samples is how many runs
// gave a value; fewer than Runs means some runs could not be measured.
type Spread struct {
	Median, Min, Max        float64
	Failures, Runs, Samples int
}

type metric struct {
	name  string
	value func(RunRecord) (float64, bool)
}

var arms = []brief.Arm{brief.Carlos, brief.Control, brief.Spec}

// armsIn are the arms a round ran, in column order; a two-arm round
// keeps two columns.
func armsIn(r Round) []brief.Arm {
	var present []brief.Arm
	for _, arm := range arms {
		for _, run := range r.Runs {
			if run.Arm == arm {
				present = append(present, arm)
				break
			}
		}
	}
	return present
}

// Summarise is brief -> arm -> metric. Failures counts the runs with
// any agent failure, and stays beside every metric so a good median
// from two runs out of three never reads as three good runs.
func Summarise(r Round) map[string]map[brief.Arm]map[string]Spread {
	summary := map[string]map[brief.Arm]map[string]Spread{}
	for _, name := range briefNames(r) {
		summary[name] = map[brief.Arm]map[string]Spread{}
		for _, arm := range arms {
			runs := runsOf(r, name, arm)
			if len(runs) == 0 {
				continue
			}
			failures := 0
			for _, run := range runs {
				if len(run.AgentFailures) > 0 {
					failures++
				}
			}
			spreads := map[string]Spread{}
			for _, m := range metricsFor(r, name) {
				var values []float64
				for _, run := range runs {
					if value, ok := m.value(run); ok {
						values = append(values, value)
					}
				}
				if len(values) == 0 {
					continue
				}
				spread := spreadOf(values)
				spread.Failures, spread.Runs = failures, len(runs)
				spread.Samples = len(values)
				spreads[m.name] = spread
			}
			summary[name][arm] = spreads
		}
	}
	return summary
}

func spreadOf(values []float64) Spread {
	sort.Float64s(values)
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		median = (values[middle-1] + values[middle]) / 2
	}
	return Spread{Median: median, Min: values[0], Max: values[len(values)-1]}
}

func briefNames(r Round) []string {
	var names []string
	for _, run := range r.Runs {
		if !contains(names, run.Brief) {
			names = append(names, run.Brief)
		}
	}
	return names
}

func runsOf(r Round, name string, arm brief.Arm) []RunRecord {
	var runs []RunRecord
	for _, run := range r.Runs {
		if run.Brief == name && run.Arm == arm {
			runs = append(runs, run)
		}
	}
	return runs
}

func contains(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

// metricsFor lists the rows of a brief's table, in the order they
// print. Page rows follow the roles the runs measured.
func metricsFor(r Round, name string) []metric {
	metrics := []metric{}
	for index, phase := range []string{"build", "follow-up"} {
		cost := func(run RunRecord) Cost {
			if index == 0 {
				return run.Build
			}
			return run.Followup
		}
		metrics = append(metrics,
			metric{phase + " cost ($)", func(run RunRecord) (float64, bool) {
				return cost(run).CostUSD, cost(run).Complete
			}},
			metric{phase + " turns", func(run RunRecord) (float64, bool) {
				return float64(cost(run).NumTurns), cost(run).Complete
			}},
			metric{phase + " minutes", func(run RunRecord) (float64, bool) {
				return float64(cost(run).DurationS) / 60, cost(run).Complete
			}},
			metric{phase + " context at first write (k)", func(run RunRecord) (float64, bool) {
				return float64(cost(run).FirstWriteContext) / 1000, cost(run).FirstWriteContext > 0
			}},
			metric{phase + " deploy and live-check messages", func(run RunRecord) (float64, bool) {
				return float64(cost(run).DeployMessages), true
			}},
		)
		for _, stage := range stageOrder {
			metrics = append(metrics, metric{
				fmt.Sprintf("%s messages: %s", phase, stage),
				func(run RunRecord) (float64, bool) {
					return float64(cost(run).Stages[stage].Messages), true
				}})
		}
		metrics = append(metrics, metric{phase + " home: security headers present",
			func(run RunRecord) (float64, bool) {
				headers := run.Measured[index].Headers
				return float64(protections(headers)), headers != nil
			}})
		for _, role := range rolesOf(r, name) {
			metrics = append(metrics, pageMetrics(phase, index, role)...)
		}
	}
	return metrics
}

// protections counts the six protections the README names. CSP's
// frame-ancestors and X-Frame-Options do one job, so an app sending
// both is not counted as better protected than one sending either.
func protections(headers map[string]string) int {
	count := 0
	for _, name := range []string{
		"content-security-policy", "strict-transport-security",
		"x-content-type-options", "referrer-policy", "permissions-policy",
	} {
		if headers[name] != "" {
			count++
		}
	}
	if headers["frame-ancestors"] != "" || headers["x-frame-options"] != "" {
		count++
	}
	return count
}

var stageOrder = []classify.Stage{
	classify.FrameworkRead, classify.AppRead, classify.Scaffold,
	classify.Write, classify.BuildTest, classify.Gates,
	classify.Screenshots, classify.Deploy, classify.LiveCheck,
	classify.Other,
}

func rolesOf(r Round, name string) []string {
	var roles []string
	for _, run := range r.Runs {
		if run.Brief != name {
			continue
		}
		for _, measurement := range run.Measured {
			for _, page := range measurement.Pages {
				if !contains(roles, page.Role) {
					roles = append(roles, page.Role)
				}
			}
		}
	}
	return roles
}

func pageMetrics(phase string, index int, role string) []metric {
	page := func(run RunRecord) (measure.Page, bool) {
		for _, p := range run.Measured[index].Pages {
			if p.Role == role && p.Failure == "" {
				return p, true
			}
		}
		return measure.Page{}, false
	}
	kilobytes := func(label string, size func(measure.Weight) int) metric {
		return metric{fmt.Sprintf("%s %s: %s over the wire (kB)", phase, role, label),
			func(run RunRecord) (float64, bool) {
				p, ok := page(run)
				return float64(size(p.Weight)) / 1000, ok
			}}
	}
	score := func(category string) metric {
		return metric{fmt.Sprintf("%s %s: Lighthouse %s", phase, role, category),
			func(run RunRecord) (float64, bool) {
				p, ok := page(run)
				value, scored := p.Lighthouse[category]
				return float64(value), ok && scored
			}}
	}
	return []metric{
		kilobytes("total", measure.Weight.WireTotal),
		kilobytes("CSS", func(w measure.Weight) int { return w.Wire[measure.CSS] }),
		kilobytes("JS", func(w measure.Weight) int { return w.Wire[measure.JS] }),
		{fmt.Sprintf("%s %s: axe serious or critical", phase, role),
			func(run RunRecord) (float64, bool) {
				p, ok := page(run)
				return float64(p.Axe["serious"] + p.Axe["critical"]), ok
			}},
		score("performance"),
		score("accessibility"),
		score("best-practices"),
	}
}

// Markdown is the round as a person reads it: one table per brief,
// each cell the median with the range beside it.
func Markdown(r Round) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Agent benchmark, round %s\n\n", r.Label)
	for _, line := range [][2]string{
		{"Date", r.Date}, {"Model", r.Model}, {"Claude Code", r.ClaudeVersion},
		{"Rastrillo", r.Rastrillo}, {"Control platform", r.Platform},
		{"Platform poll", r.Poll},
		{"Getting-started skill", r.GettingStartedCommit},
		{"Machine policy (sha256)", r.PolicyHash},
	} {
		if line[1] != "" {
			fmt.Fprintf(&out, "- %s: %s\n", line[0], line[1])
		}
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&out, "- %s\n", note)
	}
	summary := Summarise(r)
	for _, name := range briefNames(r) {
		columns := armsIn(r)
		header, rule := "| |", "|---|"
		for _, arm := range columns {
			header += " " + string(arm) + " |"
			rule += "---|"
		}
		fmt.Fprintf(&out, "\n## %s\n\n%s\n%s\n", name, header, rule)
		runsRow := "| runs (with agent failures) |"
		for _, arm := range columns {
			runs := runsOf(r, name, arm)
			failed := 0
			for _, run := range runs {
				if len(run.AgentFailures) > 0 {
					failed++
				}
			}
			if len(runs) == 0 {
				runsRow += " – |"
			} else {
				runsRow += fmt.Sprintf(" %d (%d) |", len(runs), failed)
			}
		}
		out.WriteString(runsRow + "\n")
		for _, m := range metricsFor(r, name) {
			row := "| " + m.name + " |"
			for _, arm := range columns {
				spread, present := summary[name][arm][m.name]
				if !present {
					row += " – |"
					continue
				}
				row += " " + cell(spread, strings.Contains(m.name, "($)")) + " |"
			}
			out.WriteString(row + "\n")
		}
		failures := ""
		for _, arm := range columns {
			for _, run := range runsOf(r, name, arm) {
				failures += failureLines(run)
			}
		}
		if failures != "" {
			out.WriteString("\nFailures and measurement notes:\n\n" + failures)
		}
	}
	return out.String()
}

func failureLines(run RunRecord) string {
	var lines string
	for _, failure := range run.AgentFailures {
		lines += fmt.Sprintf("- %s %d: %s\n", run.Arm, run.N, failure)
	}
	for index, phase := range []string{"build", "follow-up"} {
		for _, note := range run.Measured[index].Failures {
			lines += fmt.Sprintf("- %s %d: %s measurement: %s\n",
				run.Arm, run.N, phase, note)
		}
	}
	return lines
}

func cell(spread Spread, dollars bool) string {
	format := func(value float64) string {
		if dollars {
			return fmt.Sprintf("%.2f", value)
		}
		if value == math.Trunc(value) {
			return fmt.Sprintf("%.0f", value)
		}
		return fmt.Sprintf("%.1f", value)
	}
	var details []string
	if spread.Min != spread.Max {
		details = append(details, format(spread.Min)+"–"+format(spread.Max))
	}
	if spread.Samples < spread.Runs {
		details = append(details,
			fmt.Sprintf("%d of %d", spread.Samples, spread.Runs))
	}
	if len(details) == 0 {
		return format(spread.Median)
	}
	return fmt.Sprintf("%s (%s)", format(spread.Median),
		strings.Join(details, "; "))
}

// Fingerprint hashes named inputs in name order. Each name and value is
// length-prefixed, so no two different sets can run together into the
// same bytes.
func Fingerprint(inputs map[string]string) string {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		fmt.Fprintf(hash, "%d:%s%d:%s", len(name), name,
			len(inputs[name]), inputs[name])
	}
	return fmt.Sprintf("%x", hash.Sum(nil))[:16]
}
