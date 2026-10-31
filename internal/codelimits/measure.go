package codelimits

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxColumns     = 80
	MaxFileLines   = 400
	MaxFuncLines   = 40
	MaxActionLines = 15

	// tabWidth is the tab stop `expand -t4` uses, which is how the
	// columns limit is judged: a tab is not one column.
	tabWidth = 4

	// hashDigits is how much of a line's SHA-256 names it. Twelve hex
	// digits keep a record short and still make a collision between two
	// long lines of one file a one-in-2^48 event.
	hashDigits = 12
)

// Kind says which limit an offence is against.
type Kind string

const (
	Columns   Kind = "columns"
	FileLines Kind = "file"
	FuncLines Kind = "func"
	Names     Kind = "names"
)

// Offence is one place that is over a limit.
//
// Key names it within its file, so that a record of known offences
// survives edits elsewhere in the file: the hash of the line for
// Columns, "function/name" for Names (the function is "-" at package
// level), the function's name for FuncLines and empty for FileLines.
// A function's name repeated in its file, as two func init() are, has
// "~2", "~3" on the second and later in source order, short or long.
// Size is the width, the line count or the body length, and 1 for a
// name. Limit is the number Size is held to, and 0 for a name, which
// has no number: any single letter is over. Line is where to look.
type Offence struct {
	Kind  Kind
	Path  string
	Key   string
	Size  int
	Limit int
	Line  int
}

// Measure walks the module at root and returns every offence, sorted by
// Path, Kind, Key and Line. Paths are slash-separated and relative to
// root.
func Measure(root string) ([]Offence, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	var found []Offence
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return visitDirectory(root, path, entry.Name())
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		offences, err := measureFile(root, module, path)
		found = append(found, offences...)
		return err
	}
	if err := filepath.WalkDir(root, visit); err != nil {
		return nil, err
	}
	slices.SortFunc(found, compareOffences)
	return found, nil
}

// visitDirectory decides whether the walk goes into a directory.
func visitDirectory(root, path, name string) error {
	if path == root {
		return nil
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "testdata" || name == "vendor" || name == "node_modules" {
		return filepath.SkipDir
	}
	// A nested module is its own tree, measured from its own root:
	// counting it into the parent would charge the parent for code it
	// does not build.
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

// modulePath reads the module path from root's go.mod. A tree with no
// go.mod has none, and then no unqualified Visit is the action package's.
func modulePath(root string) (string, error) {
	text, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", nil
}

func measureFile(root, module, path string) ([]Offence, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	unit, err := parseUnit(filepath.ToSlash(relative), module, source)
	if err != nil {
		return nil, err
	}
	if unit.generated || unit.vendored {
		return nil, nil
	}
	var found []Offence
	if !unit.data {
		found = append(found, columnOffences(unit.path, source)...)
		found = append(found, fileOffences(unit, source)...)
	}
	found = append(found, unit.functionOffences()...)
	return append(found, unit.nameOffences()...), nil
}

// ColumnOffences is the columns limit Measure applies to the file at
// path, judged on source that is not on disk yet: rastrillo new
// measures an app before it writes it, so a refused name leaves nothing
// behind. A file Measure skips or exempts from columns has none.
func ColumnOffences(path string, source []byte) ([]Offence, error) {
	unit, err := parseUnit(path, "", source)
	if err != nil {
		return nil, err
	}
	if unit.generated || unit.vendored || unit.data {
		return nil, nil
	}
	return columnOffences(unit.path, source), nil
}

// fileOffences counts lines the way wc -l does: newlines, plus one for
// a final line without one. Test files may be as long as they need.
func fileOffences(unit *unit, source []byte) []Offence {
	if unit.test {
		return nil
	}
	lines := bytes.Count(source, []byte("\n"))
	if len(source) > 0 && source[len(source)-1] != '\n' {
		lines++
	}
	if lines <= MaxFileLines {
		return nil
	}
	return []Offence{{
		Kind: FileLines, Path: unit.path, Size: lines,
		Limit: MaxFileLines, Line: 1,
	}}
}

// columnOffences judges every line, tests included, on the raw source:
// the syntax tree has no blanks or comments to be wide.
func columnOffences(path string, source []byte) []Offence {
	var found []Offence
	lines := strings.Split(string(source), "\n")
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		width := displayWidth(line)
		if width <= MaxColumns {
			continue
		}
		found = append(found, Offence{
			Kind: Columns, Path: path, Key: lineKey(line), Size: width,
			Limit: MaxColumns, Line: index + 1,
		})
	}
	return found
}

// displayWidth is the width of a line with tab stops every tabWidth
// columns and each rune one column, so a line of accented letters is not
// charged for its bytes.
func displayWidth(line string) int {
	column := 0
	for _, character := range line {
		if character == '\t' {
			column += tabWidth - column%tabWidth
			continue
		}
		column++
	}
	return column
}

// lineKey drops trailing white space, which an editor adds and removes
// without anyone meaning to change the line, and keeps leading white
// space, because re-indenting a line is a change to it.
func lineKey(line string) string {
	trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])[:hashDigits]
}

func compareOffences(left, right Offence) int {
	for _, order := range []int{
		strings.Compare(left.Path, right.Path),
		strings.Compare(string(left.Kind), string(right.Kind)),
		strings.Compare(left.Key, right.Key),
		left.Line - right.Line,
	} {
		if order != 0 {
			return order
		}
	}
	return 0
}
