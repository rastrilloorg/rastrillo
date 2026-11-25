// Package authored finds the code an agent wrote: what is in its app
// and not in the scaffold `rastrillo new` made for it, and nothing
// else. The scaffold is made again, with the version and the flags the
// run used, so the comparison needs no git history.
package authored

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// ErrUnscorable says the run's scaffold cannot be found, or is not one
// call, so there is nothing to compare the app against.
var ErrUnscorable = errors.New("authored: no single scaffold call found")

// Scaffold is one `rastrillo new` call: the app's name and every other
// argument, in order.
type Scaffold struct {
	Name  string
	Flags []string
}

// ScaffoldOf finds the call that made the app: the last `rastrillo new`
// whose name is the last element of module. Where the call ran does not
// matter, since an agent may scaffold in a scratch directory and copy.
// Calls that only ask for help are not scaffolds. Two calls in the
// last command to hold one, with different flags, are ambiguous.
func ScaffoldOf(run transcript.Transcript, module string) (Scaffold, error) {
	name := path.Base(module)
	var found []Scaffold
	for _, message := range run.Messages {
		for _, tool := range message.Tools {
			command, _ := tool.Input["command"].(string)
			if tool.Name != "Bash" || command == "" {
				continue
			}
			if calls := scaffoldCalls(command, name); len(calls) > 0 {
				found = calls
			}
		}
	}
	if len(found) == 0 {
		return Scaffold{}, fmt.Errorf(
			"%w: no `rastrillo new` for %q", ErrUnscorable, name)
	}
	last := found[len(found)-1]
	for _, call := range found {
		if !slices.Equal(call.Flags, last.Flags) {
			return Scaffold{}, fmt.Errorf("%w: %q is scaffolded twice "+
				"with different flags", ErrUnscorable, name)
		}
	}
	return last, nil
}

// scaffoldCalls parses one command line and returns its `rastrillo new`
// calls for name. A command the shell grammar rejects has none.
func scaffoldCalls(command, name string) []Scaffold {
	program, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}
	var found []Scaffold
	syntax.Walk(program, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if scaffold, ok := scaffoldFrom(call, name); ok {
			found = append(found, scaffold)
		}
		return true
	})
	return found
}

func scaffoldFrom(call *syntax.CallExpr, name string) (Scaffold, bool) {
	var words []string
	for _, word := range call.Args {
		words = append(words, literal(word))
	}
	if len(words) < 3 || path.Base(words[0]) != "rastrillo" ||
		words[1] != "new" {
		return Scaffold{}, false
	}
	arguments := words[2:]
	if slices.ContainsFunc(arguments, isHelp) {
		return Scaffold{}, false
	}
	at := slices.Index(arguments, name)
	if at < 0 || strings.HasPrefix(name, "-") {
		return Scaffold{}, false
	}
	flags := slices.Concat(arguments[:at], arguments[at+1:])
	return Scaffold{Name: name, Flags: flags}, true
}

func isHelp(argument string) bool {
	return argument == "-h" || argument == "-help" || argument == "--help"
}

// literal is the text of a shell word, with a placeholder where an
// expansion is. $TMPDIR/bin/rastrillo is still a call to rastrillo, and
// no placeholder equals a name.
func literal(word *syntax.Word) string {
	var text strings.Builder
	for _, part := range word.Parts {
		switch typed := part.(type) {
		case *syntax.Lit:
			text.WriteString(typed.Value)
		case *syntax.SglQuoted:
			text.WriteString(typed.Value)
		case *syntax.DblQuoted:
			text.WriteString(quoted(typed))
		default:
			text.WriteString(placeholder)
		}
	}
	return text.String()
}

const placeholder = "\x00"

func quoted(word *syntax.DblQuoted) string {
	var text strings.Builder
	for _, part := range word.Parts {
		if lit, ok := part.(*syntax.Lit); ok {
			text.WriteString(lit.Value)
		} else {
			text.WriteString(placeholder)
		}
	}
	return text.String()
}

// Rescaffold builds cmd/rastrillo from rastrilloDir, the module cache
// copy at the run's version, and runs `new` in into with the same
// arguments the agent gave. The app lands in into/<scaffold.Name>.
func Rescaffold(rastrilloDir string, scaffold Scaffold, into string) error {
	scratch, err := os.MkdirTemp("", "rescaffold-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	binary := filepath.Join(scratch, "rastrillo")
	environment := append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")

	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary,
		"./cmd/rastrillo")
	build.Dir, build.Env = rastrilloDir, environment
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build rastrillo: %w\n%s", err, output)
	}
	arguments := slices.Concat(
		[]string{"new"}, scaffold.Flags, []string{scaffold.Name})
	scaffolding := exec.Command(binary, arguments...)
	scaffolding.Dir, scaffolding.Env = into, environment
	if output, err := scaffolding.CombinedOutput(); err != nil {
		return fmt.Errorf("rastrillo new: %w\n%s", err, output)
	}
	return nil
}
