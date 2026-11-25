package waste

import (
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// shape is what a Bash command says about its check and about whether
// its exit status can be believed.
type shape struct {
	// checks name every build, test or gate the command runs, in order,
	// such as "go vet" then "go test", or "make ci".
	checks []string
	// trusted is true when the command's exit status is its check's: no
	// pipe, no `;` or newline between commands, no `||`, no `&`. `&&`
	// chains are fine, since one that failed stops there.
	trusted bool
	// lastIsCheck is true when the last simple command the shell runs
	// in the list is a check, so the list's exit status is that check's
	// whenever the list gets that far.
	lastIsCheck bool
}

// shapeOf reads a command as the shell would. One that does not parse
// is never trusted, and is a check if it looks like one.
func shapeOf(command string) shape {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		if check := looksLikeCheck(command); check != "" {
			return shape{checks: []string{check}}
		}
		return shape{}
	}
	var found shape
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if ok {
			if check := checkOfCall(call); check != "" {
				found.checks = append(found.checks, check)
			}
		}
		return true
	})
	found.trusted = len(file.Stmts) == 1 && plain(file.Stmts[0])
	found.lastIsCheck = lastIsCheck(file.Stmts)
	return found
}

// lastIsCheck follows a list to its last simple command: the last
// statement, the right of an operator or pipe, the inside of a
// subshell or block. A backgrounded statement exits 0 at once.
func lastIsCheck(stmts []*syntax.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	stmt := stmts[len(stmts)-1]
	if stmt.Background || stmt.Coprocess || stmt.Negated {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		return checkOfCall(cmd) != ""
	case *syntax.BinaryCmd:
		return lastIsCheck([]*syntax.Stmt{cmd.Y})
	case *syntax.Subshell:
		return lastIsCheck(cmd.Stmts)
	case *syntax.Block:
		return lastIsCheck(cmd.Stmts)
	}
	return false
}

// plain is a statement whose exit status is its last command's own.
func plain(stmt *syntax.Stmt) bool {
	if stmt.Background || stmt.Coprocess || stmt.Negated {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		return true
	case *syntax.BinaryCmd:
		return cmd.Op == syntax.AndStmt && plain(cmd.X) && plain(cmd.Y)
	}
	return false
}

var looksLike = regexp.MustCompile(
	`\bgo (build|test|vet)\b|\bmake (\w[\w-]*)|\bstaticcheck\b`)

func looksLikeCheck(command string) string {
	return looksLike.FindString(command)
}

// wrappers run the command that follows them.
var wrappers = map[string]bool{
	"timeout": true, "time": true, "env": true, "nohup": true, "nice": true,
}

// rastrilloChecks are the rastrillo subcommands that judge an app.
var rastrilloChecks = map[string]bool{
	"generate": true, "budget": true, "routes": true,
	"migration": true, "doctor": true,
}

func checkOfCall(call *syntax.CallExpr) string {
	words := make([]string, len(call.Args))
	for i, word := range call.Args {
		words[i] = literal(word)
	}
	for len(words) > 0 && wrappers[path.Base(words[0])] {
		words = words[1:]
		for len(words) > 0 && skippable(words[0]) {
			words = words[1:]
		}
	}
	if len(words) == 0 {
		return ""
	}
	rest := words[1:]
	switch path.Base(words[0]) {
	case "go":
		return goCheck(rest)
	case "make":
		for _, arg := range rest {
			if arg != "" && !strings.HasPrefix(arg, "-") &&
				!strings.Contains(arg, "=") {
				return "make " + arg
			}
		}
		return "make"
	case "staticcheck":
		return "staticcheck"
	case "rastrillo":
		return rastrilloCheck(rest)
	}
	return ""
}

// skippable is a flag, a duration or a VAR=value after a wrapper.
func skippable(word string) bool {
	if strings.HasPrefix(word, "-") || strings.Contains(word, "=") {
		return true
	}
	return regexp.MustCompile(`^\d+[smhd]?$`).MatchString(word)
}

func goCheck(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "build", "test", "vet", "generate":
		return "go " + args[0]
	case "run":
		for i, arg := range args[1:] {
			switch {
			case strings.Contains(arg, "staticcheck"):
				return "staticcheck"
			case path.Base(arg) == "rastrillo":
				return rastrilloCheck(args[i+2:])
			}
		}
		return "go run"
	case "tool":
		if len(args) > 1 && args[1] == "rastrillo" {
			return rastrilloCheck(args[2:])
		}
	}
	return ""
}

func rastrilloCheck(args []string) string {
	if len(args) > 0 && rastrilloChecks[args[0]] {
		return "rastrillo " + args[0]
	}
	return ""
}

// literal is a word's text when it is made of literals and quotes.
func literal(word *syntax.Word) string {
	var out strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			out.WriteString(part.Value)
		case *syntax.SglQuoted:
			out.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				if lit, ok := inner.(*syntax.Lit); ok {
					out.WriteString(lit.Value)
				}
			}
		}
	}
	return out.String()
}
