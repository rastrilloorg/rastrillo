package counts

import (
	"go/scanner"
	"go/token"
	"strings"
)

// lineKinds says, for each line of a file (index 0 is unused), whether
// it holds code and whether it holds a comment. A line with both is
// code. Neither is blank.
type lineKinds struct{ code, comment []bool }

func kindsOf(source []byte) lineKinds {
	total := strings.Count(string(source), "\n") + 2
	kinds := lineKinds{make([]bool, total), make([]bool, total)}
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, source, nil, scanner.ScanComments)
	for {
		position, kind, text := scan.Scan()
		if kind == token.EOF {
			return kinds
		}
		if kind == token.SEMICOLON && text == "\n" {
			continue
		}
		first := file.Line(position)
		last := first + strings.Count(text, "\n")
		for line := first; line <= last && line < total; line++ {
			if kind == token.COMMENT {
				kinds.comment[line] = true
			} else {
				kinds.code[line] = true
			}
		}
	}
}

// count is the code lines and the comment-only lines from first to last.
func (kinds lineKinds) count(first, last int) (code, comments int) {
	for line := first; line <= last && line < len(kinds.code); line++ {
		switch {
		case kinds.code[line]:
			code++
		case kinds.comment[line]:
			comments++
		}
	}
	return code, comments
}

// ranks numbers the code lines: rank[line] is how many code lines there
// are up to and including it, so the difference of two is the distance
// between them in lines of code.
func (kinds lineKinds) ranks() []int {
	ranks := make([]int, len(kinds.code))
	for line := 1; line < len(kinds.code); line++ {
		ranks[line] = ranks[line-1]
		if kinds.code[line] {
			ranks[line]++
		}
	}
	return ranks
}

// countWhere is count over the lines marked, each once.
func (kinds lineKinds) countWhere(marked []bool) (code, comments int) {
	for line, yes := range marked {
		switch {
		case !yes || line >= len(kinds.code):
		case kinds.code[line]:
			code++
		case kinds.comment[line]:
			comments++
		}
	}
	return code, comments
}
