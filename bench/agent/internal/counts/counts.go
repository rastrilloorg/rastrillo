// Package counts measures what makes the code an agent wrote hard to
// read, with numbers a person can check against the source: how tangled
// each function is, how deep it nests, how far its short names travel,
// how much of it is copied, and how much sits over the limits Rastrillo
// holds code to. It counts only authored code (internal/authored).
//
// Everything but the interface count comes from parsing alone, so a
// file that does not compile is still counted. Counts never change with
// blank lines or comments: distances are in lines of code.
package counts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// FuncCounts is one authored function, method or function literal.
// Start and End are its first and last lines in the file and Lines its
// span, doc comment included; the rest do not move when blank lines or
// comments are added. OverLimitLines is the lines it has over a limit:
// each line wider than 80 columns, each one-letter name declared in it,
// and the lines past the length limit.
type FuncCounts struct {
	Path, Key       string
	Test            bool
	Start, End      int
	Lines           int
	Cognitive       int
	Nesting         int
	FarShortNames   int
	DuplicatedLines int
	OverLimitLines  int
}

// RunCounts is everything counted for one app: its functions, and the
// totals of its own code and of its tests, apart. SingleImplementation-
// Interfaces is nil when the app does not type-check: that is
// unavailable, not zero.
type RunCounts struct {
	Funcs                          []FuncCounts
	App, Tests                     Totals
	SingleImplementationInterfaces *int
}

// Totals are counts over one part of an app's authored code, its own
// code or its tests. Lines are the lines of code (no blank or comment
// lines) in authored functions and whole files, which is also what
// CommentShare divides by.
type Totals struct {
	Files, Lines int
	CommentShare float64
	Offences     map[codelimits.Kind]int
}

// Measure counts the authored code of the app in appDir.
func Measure(appDir string, code authored.Code) (RunCounts, error) {
	offences, err := codelimits.Measure(appDir)
	if err != nil {
		return RunCounts{}, err
	}
	copied, err := duplicatedLines(appDir)
	if err != nil {
		return RunCounts{}, err
	}
	run := RunCounts{}
	comments := map[*Totals]int{}
	counted := newOffenceTally(offences)
	for _, file := range code.Files {
		part := &run.App
		if file.Test {
			part = &run.Tests
		}
		read, err := readFile(appDir, file)
		if err != nil {
			return RunCounts{}, err
		}
		read.copied = copied[file.Path]
		funcs, err := read.funcCounts(counted)
		if err != nil {
			return RunCounts{}, err
		}
		run.Funcs = append(run.Funcs, funcs...)
		lines, comment := read.share()
		part.Files++
		part.Lines += lines
		comments[part] += comment
		counted.attachFile(file)
	}
	run.App.Offences, run.Tests.Offences = counted.totals(false),
		counted.totals(true)
	for _, part := range []*Totals{&run.App, &run.Tests} {
		if part.Lines > 0 {
			part.CommentShare = float64(comments[part]) / float64(part.Lines)
		}
	}
	run.SingleImplementationInterfaces = interfaces(appDir, code)
	return run, nil
}

// sourceFile is one authored file read for counting.
type sourceFile struct {
	authored.File
	fset   *token.FileSet
	syntax *ast.File
	lines  lineKinds
	copied map[int]bool
}

func readFile(appDir string, file authored.File) (*sourceFile, error) {
	source, err := os.ReadFile(filepath.Join(appDir, file.Path))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, file.Path, source,
		parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &sourceFile{
		File: file, fset: fset, syntax: syntax, lines: kindsOf(source),
	}, nil
}

func (file *sourceFile) line(position token.Pos) int {
	return file.fset.PositionFor(position, false).Line
}

// share is the code lines and the comment lines of what is authored in
// the file: all of it when whole, else the lines of its functions.
func (file *sourceFile) share() (code, comments int) {
	first, last := 1, len(file.lines.code)-1
	if !file.Whole {
		covered := make([]bool, len(file.lines.code))
		for _, unit := range file.Funcs {
			for line := unit.Start; line <= unit.End; line++ {
				if line < len(covered) {
					covered[line] = true
				}
			}
		}
		return file.lines.countWhere(covered)
	}
	return file.lines.count(first, last)
}

// funcCounts counts each unit. A unit that no function in the file
// matches is an error: counting it as zero would pass for simple code.
func (file *sourceFile) funcCounts(tally *offenceTally) ([]FuncCounts, error) {
	var found []FuncCounts
	nodes := file.functionNodes()
	over := tally.overLimit(file.File)
	for index, unit := range file.Funcs {
		node, ok := nodes[unit.Start][unit.End]
		if !ok {
			return nil, fmt.Errorf("%s: no function at lines %d-%d for %s",
				file.Path, unit.Start, unit.End, unit.Key)
		}
		found = append(found, FuncCounts{
			Path: file.Path, Key: unit.Key, Test: file.Test,
			Start: unit.Start, End: unit.End,
			Lines:           unit.End - unit.Start + 1,
			Cognitive:       cognitive(node),
			Nesting:         nesting(node.body),
			FarShortNames:   file.farShortNames(node),
			DuplicatedLines: file.duplicated(unit),
			OverLimitLines:  over[index],
		})
	}
	return found, nil
}

func (file *sourceFile) duplicated(unit authored.Func) int {
	lines := 0
	for line := unit.Start; line <= unit.End; line++ {
		if file.copied[line] && file.lines.code[line] {
			lines++
		}
	}
	return lines
}
