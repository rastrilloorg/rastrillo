// Package handfile is what the generator does to a hand-written file
// before it compiles: hand actions under actions/ and hand operations
// under ops/ are never built in place. Each is parsed, its package
// clause rewritten to the generated package's name, its build
// constraint stripped, and the result written under gen/. It lives
// apart from internal/generate so the operations emitter, which
// internal/generate imports, can apply the same rewrite without the
// two keeping copies that would drift.
package handfile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"strings"
)

// BuildTag is the build constraint every hand-written action and
// operation file carries (`//go:build rastrillo_actions`). It is never
// satisfied by a normal build, so `go build ./...`, `go vet ./...` and
// `go test ./...` skip the originals instead of failing on generator
// input: a multi-action directory declares Handle twice, and an
// operation file names types that only exist in the generated package.
const BuildTag = "rastrillo_actions"

// Rewrite returns src with only its package clause changed to pkg. The
// rest of the file, imports, functions and helpers, travels unmodified,
// with one exception: any build-constraint comment (the BuildTag
// convention, or a legacy `// +build` line) is stripped, because the
// copy is the form that must compile and a surviving constraint would
// exclude it too. name is how an error names the file.
func Rewrite(src []byte, name, pkg string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	if file.Name == nil {
		return nil, fmt.Errorf("%s: missing package clause", name)
	}
	file.Name = ast.NewIdent(pkg)

	if file.Doc != nil && isConstraintGroup(file.Doc) {
		file.Doc = nil
	}
	var kept []*ast.CommentGroup
	for _, g := range file.Comments {
		if g.End() < file.Package && isConstraintGroup(g) {
			continue
		}
		kept = append(kept, g)
	}
	file.Comments = kept

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("format %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// isConstraintGroup reports whether any line of g is a build-constraint
// comment: `//go:build ...`, or the legacy `// +build ...` spelling that
// gofmt keeps alongside it in pre-1.17 files.
func isConstraintGroup(g *ast.CommentGroup) bool {
	for _, c := range g.List {
		text := strings.TrimSpace(c.Text)
		if strings.HasPrefix(text, "//go:build ") || strings.HasPrefix(text, "// +build ") {
			return true
		}
	}
	return false
}

// SkippedByDefaultBuild reports whether a default build skips src,
// which is exactly what lets `go build ./...` pass over generator
// input. The scaffolded spelling is `//go:build rastrillo_actions`
// (BuildTag), but the check evaluates the file with the go tool's own
// rules (go/build's MatchFile: constraints, GOOS/GOARCH and release
// tags included) rather than grepping for the tag: a file with
// `//go:build !rastrillo_actions` names the tag yet compiles in every
// default build, and any other never-satisfied constraint earns the
// same skip the scaffolded one does.
func SkippedByDefaultBuild(src []byte) bool {
	ctxt := build.Default
	ctxt.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	match, err := ctxt.MatchFile(".", "hand.go")
	if err != nil {
		// Unparseable header: let the compiler complain, not --check.
		return false
	}
	return !match
}
