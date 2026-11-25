package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/waste"
)

// wasteCommand is `bench waste`: sort the failed checks in the kept
// runs' transcripts by kind and by turns to fix. It reads transcripts
// only and makes no paid call.
func wasteCommand(args []string) error {
	flags := flag.NewFlagSet("waste", flag.ContinueOnError)
	rounds := flags.String("rounds", "", "comma-separated round labels")
	briefs := flags.String("briefs", "", "comma-separated brief names")
	arms := flags.String("arms", rastrilloArms, "comma-separated arms to read")
	out := flags.String("out", "", "label for results/waste-<label>.md")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rounds == "" || *briefs == "" || *out == "" {
		return errors.New("usage: waste -rounds a,b -briefs x,y " +
			"[-arms carlos,carlos-gopls] -out <label>")
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	runs, err := runsRoot()
	if err != nil {
		return err
	}
	var found []located
	count := 0
	for _, label := range strings.Split(*rounds, ",") {
		kept, err := keptRuns(root, runs, label,
			strings.Split(*briefs, ","), strings.Split(*arms, ","))
		if err != nil {
			return err
		}
		for _, run := range kept {
			count++
			more, err := wasteOfRun(label, run.dir)
			if err != nil {
				return err
			}
			found = append(found, more...)
		}
	}
	path := resultPath(root, "waste-"+*out, ".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return replaceFile(path,
		[]byte(wasteMarkdown(count, found)+judgement(path)))
}

// located is a mistake and the phase of the run it was made in.
type located struct {
	Where string
	waste.Mistake
}

// wasteOfRun reads the build and the follow-up apart: each has its own
// message numbering, and a check is fixed within its own phase.
func wasteOfRun(round, dir string) ([]located, error) {
	var found []located
	for _, phase := range []string{"build", "followup"} {
		path := filepath.Join(dir, "rec", phase+".jsonl")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		parsed, err := readTranscript(path)
		if err != nil {
			return nil, err
		}
		where := round + "/" + filepath.Base(dir) + " " + phase
		for _, mistake := range waste.Find(parsed) {
			found = append(found, located{where, mistake})
		}
	}
	return found, nil
}

const wasteExamples = 3

func wasteMarkdown(runs int, found []located) string {
	mistakes := make([]waste.Mistake, len(found))
	for i, one := range found {
		mistakes[i] = one.Mistake
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Where agents waste turns\n\n"+
		"%d kept runs, build and follow-up phases. A mistake is a "+
		"build, test or gate command whose output shows it failed. "+
		"Turns to fix are the messages from the failure to the same "+
		"check next passing. A check that passes is one with a pass "+
		"line, or a silent one with no pipe, ';' or '||' to hide its "+
		"exit status; anything else is unknown and never a fix. A "+
		"passing go test fixes any Go mistake; a silent go build or "+
		"go vet fixes only a compile error.\n\n",
		runs)
	out.WriteString("| Kind | Mistakes | Fixed | Never fixed | " +
		"Median turns to fix |\n|---|---|---|---|---|\n")
	for _, row := range waste.Summarise(mistakes) {
		median := "-"
		if row.Fixed > 0 {
			median = fmt.Sprint(row.Median)
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %s |\n",
			row.Kind, row.Count, row.Fixed, row.Count-row.Fixed, median)
	}
	out.WriteString("\n## Examples\n")
	for _, kind := range waste.Kinds {
		fmt.Fprintf(&out, "\n### %s\n\n", kind)
		shown := 0
		for _, one := range found {
			if one.Kind == kind && shown < wasteExamples {
				fmt.Fprintf(&out, "- %s, message %d: `%s`\n",
					one.Where, one.Message, one.Detail)
				shown++
			}
		}
	}
	return out.String()
}
