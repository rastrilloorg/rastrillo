package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func appendResult(out string, r result) error {
	f, err := os.OpenFile(filepath.Join(out, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func readResults(out string) ([]result, error) {
	b, err := os.ReadFile(filepath.Join(out, "results.jsonl"))
	if err != nil {
		return nil, err
	}
	var rs []result
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var r result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		return a.Rep < b.Rep
	})
	return rs, nil
}

// retallyDir applies the current classifier to transcripts already on
// disk, so a classifier fix does not cost a rerun. The paths it needs
// are the ones the run used, which the directory layout fixes.
func retallyDir(ctx context.Context, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	rs, err := readResults(out)
	if err != nil {
		return err
	}
	stale := moduleCacheCopies(ctx)
	for i, r := range rs {
		if r.Dry {
			continue
		}
		dir := filepath.Join(out, r.Variant, r.Task, fmt.Sprint(r.Rep))
		// The transcript names the paths the run used, so those classify
		// it, wherever the results directory has since moved; results
		// from before they were recorded fall back to the layout.
		export, app := r.Export, r.App
		if export == "" {
			export, app = filepath.Join(out, r.Variant, "rastrillo"), filepath.Join(dir, strings.ReplaceAll(r.Task, "-", ""))
		}
		t, err := tallyFile(filepath.Join(dir, "transcript.jsonl"), export, app, stale)
		if err != nil {
			log.Printf("%s/%s/%d: %v", r.Variant, r.Task, r.Rep, err)
			continue
		}
		rs[i].tally = t
	}
	if err := os.Rename(filepath.Join(out, "results.jsonl"), filepath.Join(out, "results.jsonl.bak")); err != nil {
		return err
	}
	for _, r := range rs {
		if err := appendResult(out, r); err != nil {
			return err
		}
	}
	return writeSummary(out)
}

func writeSummary(out string) error {
	rs, err := readResults(out)
	if err != nil {
		return err
	}
	var b strings.Builder
	kb := func(n int) string { return fmt.Sprintf("%.1f", float64(n)/1024) }
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return "**no**"
	}
	b.WriteString("Bytes are charged to the most expensive bucket a call touched; calls count every bucket a call reached. A `~` marks a run with no result event: its tokens are summed from what it streamed and its cost is unknown.\n\n")
	b.WriteString("| task | variant | rep | done | ci | check | tokens (k) | cost | turns | read SKILL.md | calls: skill / docs / examples / source | source+examples KB | go doc | web | stale copy | gate edited |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		tokens, cost := fmt.Sprint(r.Tokens/1000), fmt.Sprintf("$%.2f", r.CostUSD)
		if r.Partial {
			tokens, cost = "~"+tokens, "?"
		}
		if r.Dry {
			tokens, cost = "dry", "dry"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %s | %s | %d | %s | %d / %d / %d / %d | %s | %d | %d | %d | %s |\n",
			r.Task, r.Variant, r.Rep, yes(r.Finished), yes(r.CI), yes(r.Check),
			tokens, cost, r.Turns, yes(r.Touched[bSkill] > 0),
			r.Touched[bSkill], r.Touched[bDocs], r.Touched[bExamples], r.Touched[bSource],
			kb(r.Buckets[bSource].Bytes+r.Buckets[bExamples].Bytes),
			r.Touched[bGoDoc], r.Touched[bWeb], r.StaleCalls, strings.Join(r.GateEdited, " "))
	}
	for _, r := range rs {
		if r.Error == "" && len(r.SourceReads) == 0 && r.Buckets[bFWWrite].Calls == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n**%s / %s / %d**", r.Task, r.Variant, r.Rep)
		if r.Error != "" {
			fmt.Fprintf(&b, " error: %s", r.Error)
		}
		if n := r.Buckets[bFWWrite].Calls; n > 0 {
			fmt.Fprintf(&b, " (tried to write the framework %d times)", n)
		}
		b.WriteString("\n")
		for _, s := range r.SourceReads {
			fmt.Fprintf(&b, "- `%s`\n", strings.ReplaceAll(s, "`", "'"))
		}
	}
	if err := os.WriteFile(filepath.Join(out, "summary.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Print(b.String())
	return nil
}
