// Package calibrate checks the readability counts against a person's
// taste. It cuts the code that does one job out of each app, pairs two
// apps' excerpts of the same job, and scores each excerpt with a
// composite of the counts, so that a person's blind verdicts on the
// pairs can say whether the composite reads code as they do.
package calibrate

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
)

// MaxLines is the most lines an excerpt may have and still be shown.
const MaxLines = 60

// Excerpt is the code that does one job in one run's app: its
// functions' source and counts. Run is <round>/<run directory>. An
// excerpt with no Funcs is a job the app does not do in code its agent
// wrote; one over MaxLines is too long to show. Both are kept, so that
// coverage can count them, and neither is ever paired. Inner are the
// authored units inside Funcs: shown as part of them, and counted for
// the over-limit lines counts gives the innermost unit alone.
type Excerpt struct {
	Run, Job, Text string
	Arm            brief.Arm
	Funcs          []counts.FuncCounts
	Inner          []counts.FuncCounts `json:",omitempty"`
}

// Lines is how many lines the excerpt shows.
func (excerpt Excerpt) Lines() int {
	return strings.Count(excerpt.Text, "\n")
}

// pairable is whether the excerpt can be shown in a pair.
func (excerpt Excerpt) pairable() bool {
	return len(excerpt.Funcs) > 0 && excerpt.Lines() <= MaxLines
}

// Cut cuts the functions named path:Key out of the app, in source
// order: a line naming each file, then its functions whole, doc
// comments included, a blank line between them. Only functions in
// counted (the app's authored functions) are cut; a function inside
// another one cut is shown once, as part of it, and is one of the
// excerpt's Inner, as is any other counted unit inside one. It is false
// when the excerpt has no function or is over MaxLines.
func Cut(
	appDir string, funcs []string, counted []counts.FuncCounts,
) (Excerpt, bool) {
	byName := map[string]counts.FuncCounts{}
	for _, one := range counted {
		byName[one.Path+":"+one.Key] = one
	}
	var chosen []counts.FuncCounts
	for _, name := range funcs {
		one, found := byName[name]
		if found {
			chosen = append(chosen, one)
			delete(byName, name)
		}
	}
	slices.SortFunc(chosen, func(a, b counts.FuncCounts) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Start, b.Start), cmp.Compare(b.End, a.End))
	})
	chosen = outermost(chosen)
	text, err := excerptText(appDir, chosen)
	if err != nil || len(chosen) == 0 {
		return Excerpt{}, false
	}
	excerpt := Excerpt{Text: text, Funcs: chosen,
		Inner: inside(chosen, counted)}
	return excerpt, excerpt.Lines() <= MaxLines
}

// inside is every unit of counted that lies within one of shown and is
// not that one.
func inside(shown, counted []counts.FuncCounts) []counts.FuncCounts {
	var found []counts.FuncCounts
	for _, one := range counted {
		if slices.ContainsFunc(shown, func(outer counts.FuncCounts) bool {
			return one.Path == outer.Path && one.Key != outer.Key &&
				one.Start >= outer.Start && one.End <= outer.End
		}) {
			found = append(found, one)
		}
	}
	return found
}

// outermost drops each function that lies inside the one before it, in
// a list sorted by path, start and, for the same start, longest first.
func outermost(sorted []counts.FuncCounts) []counts.FuncCounts {
	var kept []counts.FuncCounts
	for _, one := range sorted {
		if last := len(kept) - 1; last >= 0 && kept[last].Path == one.Path &&
			one.End <= kept[last].End {
			continue
		}
		kept = append(kept, one)
	}
	return kept
}

func excerptText(appDir string, funcs []counts.FuncCounts) (string, error) {
	var text strings.Builder
	var lines []string
	for index, one := range funcs {
		if index == 0 || funcs[index-1].Path != one.Path {
			source, err := os.ReadFile(filepath.Join(appDir, one.Path))
			if err != nil {
				return "", err
			}
			lines = strings.Split(string(source), "\n")
			if index > 0 {
				text.WriteString("\n")
			}
			text.WriteString("// " + one.Path + "\n")
		} else {
			text.WriteString("\n")
		}
		for _, line := range lines[one.Start-1 : min(one.End, len(lines))] {
			text.WriteString(line + "\n")
		}
	}
	return text.String(), nil
}
