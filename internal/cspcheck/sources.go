package cspcheck

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// inspectGo reads the markup in a Go file's string literals, and only
// there: go/scanner knows a literal from a comment, so a comment that
// explains the rule is not taken for markup that breaks it. A generated
// file is skipped, because the template it was made from is reported.
func inspectGo(source []byte) []problem {
	if isGenerated(source) {
		return nil
	}
	files := token.NewFileSet()
	file := files.AddFile("", files.Base(), len(source))
	var reader scanner.Scanner
	// No error handler: a file that does not compile still says where
	// its strings are, and the build reports the rest.
	reader.Init(file, source, nil, 0)
	var problems []problem
	for {
		position, kind, literal := reader.Scan()
		if kind == token.EOF {
			return problems
		}
		if kind != token.STRING {
			continue
		}
		line := file.Line(position)
		problems = append(problems, inspectLiteral(literal, line)...)
	}
}

// isGenerated reads a "Code generated ... DO NOT EDIT." comment as Go
// does: before the package clause.
func isGenerated(source []byte) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "", source,
		parser.PackageClauseOnly|parser.ParseComments)
	return err == nil && ast.IsGenerated(file)
}

// inspectLiteral reads one Go string literal on line, blanking template
// actions and comments first, since a Go string is often a template. A
// raw string's lines are its source lines; an interpreted one is all on
// one line, whatever newlines it escapes.
func inspectLiteral(literal string, line int) []problem {
	if strings.HasPrefix(literal, "`") {
		return inspectMarkup(blankTemplate(strings.Trim(literal, "`")),
			line)
	}
	text, err := strconv.Unquote(literal)
	if err != nil {
		return nil
	}
	return onLine(inspectMarkup(blankTemplate(text), line), line)
}

// onLine moves every problem to line.
func onLine(problems []problem, line int) []problem {
	for index := range problems {
		problems[index].line = line
	}
	return problems
}
