package authored

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Func is one authored unit, keyed as internal/codelimits keys it: a
// function by its name, a method by Recv.Name, a package-level function
// literal by its variable and one nested in another value by
// <variable>.func<N> or <variable>.action<N>. Lines are 1-based and
// inclusive.
type Func struct {
	Key        string
	Start, End int
}

// File is a file with authored code in it. Whole says the scaffold has
// no such file, so every function in it is the agent's.
type File struct {
	Path        string
	Test, Whole bool
	Funcs       []Func
}

// Code is what the agent wrote.
type Code struct{ Files []File }

// RunModule is the module path in appDir's go.mod.
func RunModule(appDir string) (string, error) {
	text, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", fmt.Errorf("%s/go.mod has no module line", appDir)
}

// Authored compares the app with its scaffold and returns the code that
// differs: every function of a file the scaffold lacks, and the
// functions of other files that are new or whose tokens changed. Spaces
// and comments are not tokens, so formatting and comment edits are not
// authorship, and a file whose functions are all unchanged has no
// authored code, whatever its imports or types say. Generated, vendored, static, gen and testdata files are
// never the agent's.
func Authored(appDir, scaffoldDir string) (Code, error) {
	paths, err := goFiles(appDir)
	if err != nil {
		return Code{}, err
	}
	var code Code
	for _, relative := range paths {
		mine, err := readUnits(filepath.Join(appDir, relative))
		if err != nil {
			return Code{}, err
		}
		if mine.skipped {
			continue
		}
		file := File{
			Path: relative, Test: strings.HasSuffix(relative, "_test.go"),
		}
		original, err := readUnits(filepath.Join(scaffoldDir, relative))
		switch {
		case os.IsNotExist(err):
			file.Whole = true
			file.Funcs = mine.funcs()
		case err != nil:
			return Code{}, err
		default:
			file.Funcs = mine.changedFrom(original)
			if len(file.Funcs) == 0 {
				continue
			}
		}
		code.Files = append(code.Files, file)
	}
	return code, nil
}

// goFiles lists the .go files that can be authored, slash-separated and
// relative to root, in path order.
func goFiles(root string) ([]string, error) {
	var found []string
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return visitDirectory(root, path, entry.Name())
		}
		if strings.HasSuffix(path, ".go") {
			relative, err := filepath.Rel(root, path)
			found = append(found, filepath.ToSlash(relative))
			return err
		}
		return nil
	}
	return found, filepath.WalkDir(root, visit)
}

// visitDirectory skips what is not the agent's to write: hidden and
// underscore directories, vendored code, test data, the static assets
// and generated code directories the scaffold fills, and nested modules.
func visitDirectory(root, path, name string) error {
	if path == root {
		return nil
	}
	switch name {
	case "testdata", "vendor", "node_modules", "static", "gen":
		return filepath.SkipDir
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return filepath.SkipDir
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

var generatedLine = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

const vendoredMarker = "//rastrillo:vendored "

// unit is one function or literal: where it is and what it says, as
// tokens.
type unit struct {
	Func
	tokens string
}

// parsed is a file read for comparison.
type parsed struct {
	units   []unit
	skipped bool // generated or vendored
}

func (file parsed) funcs() []Func {
	var found []Func
	for _, one := range file.units {
		found = append(found, one.Func)
	}
	return found
}

func (file parsed) changedFrom(original parsed) []Func {
	before := map[string]string{}
	for _, one := range original.units {
		before[one.Key] = one.tokens
	}
	var found []Func
	for _, one := range file.units {
		if tokens, ok := before[one.Key]; !ok || tokens != one.tokens {
			found = append(found, one.Func)
		}
	}
	return found
}

func readUnits(path string) (parsed, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return parsed{}, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return parsed{}, err
	}
	reader := &reader{
		fset: fset, source: source, file: file, seen: map[string]int{},
		imports: importNames(file),
	}
	result := parsed{}
	result.skipped = reader.skippable()
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			result.units = append(result.units, reader.declaration(typed)...)
		case *ast.GenDecl:
			result.units = append(result.units, reader.variables(typed)...)
		}
	}
	return result, nil
}

type reader struct {
	fset    *token.FileSet
	source  []byte
	file    *ast.File
	seen    map[string]int
	imports map[string]string
}

// skippable reads the comments above the package clause, which is where
// a generated or vendored marker means anything.
func (read *reader) skippable() bool {
	for _, group := range read.file.Comments {
		if group.Pos() >= read.file.Package {
			return false
		}
		for _, comment := range group.List {
			if generatedLine.MatchString(comment.Text) {
				return true
			}
			rest, ok := strings.CutPrefix(comment.Text, vendoredMarker)
			if ok && strings.TrimSpace(rest) != "" {
				return true
			}
		}
	}
	return false
}

// next numbers namesakes the way codelimits does: the second func init
// is init~2.
func (read *reader) next(name string) string {
	read.seen[name]++
	if count := read.seen[name]; count > 1 {
		return name + "~" + strconv.Itoa(count)
	}
	return name
}

func (read *reader) unit(key string, from, to token.Pos) unit {
	return unit{
		Func: Func{
			Key:   key,
			Start: read.fset.PositionFor(from, false).Line,
			End:   read.fset.PositionFor(to, false).Line,
		},
		tokens: read.tokens(from, to),
	}
}

// tokens is the source between two positions as its tokens joined,
// with no spaces, comments or automatic semicolons in them.
func (read *reader) tokens(from, to token.Pos) string {
	start := read.fset.Position(from).Offset
	end := read.fset.Position(to).Offset
	text := read.source[start:end]
	var out strings.Builder
	var scan scanner.Scanner
	scan.Init(token.NewFileSet().AddFile("", -1, len(text)), text, nil, 0)
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			return out.String()
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		out.WriteString(kind.String() + "\x00" + literal + "\x00")
	}
}

func (read *reader) declaration(decl *ast.FuncDecl) []unit {
	if decl.Body == nil {
		return nil
	}
	from := decl.Pos()
	if decl.Doc != nil {
		from = decl.Doc.Pos()
	}
	one := read.unit(read.next(declName(decl)), decl.Pos(), decl.End())
	one.Start = read.fset.PositionFor(from, false).Line
	return []unit{one}
}

func declName(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}
	return receiverName(decl.Recv.List[0].Type) + "." + decl.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		return receiverName(typed.X)
	case *ast.ParenExpr:
		return receiverName(typed.X)
	case *ast.IndexExpr:
		return receiverName(typed.X)
	case *ast.IndexListExpr:
		return receiverName(typed.X)
	case *ast.Ident:
		return typed.Name
	}
	return "?"
}

// variables finds the function literals a package-level var holds. One
// that is the whole value is the variable's unit; those nested in other
// values are numbered, actions apart from the rest.
func (read *reader) variables(decl *ast.GenDecl) []unit {
	if decl.Tok != token.VAR {
		return nil
	}
	var found []unit
	for _, spec := range decl.Specs {
		value := spec.(*ast.ValueSpec)
		for index, initializer := range value.Values {
			if !holdsLiteral(initializer) {
				continue
			}
			name := value.Names[min(index, len(value.Names)-1)].Name
			name = read.next(name)
			found = append(found, read.initializer(name, initializer)...)
		}
	}
	return found
}

func holdsLiteral(value ast.Expr) bool {
	found := false
	ast.Inspect(value, func(node ast.Node) bool {
		_, isLiteral := node.(*ast.FuncLit)
		found = found || isLiteral
		return !found
	})
	return found
}

func (read *reader) initializer(name string, value ast.Expr) []unit {
	var found []unit
	whole, isWhole := value.(*ast.FuncLit)
	if isWhole {
		found = append(found, read.unit(name, whole.Pos(), whole.End()))
	}
	actions, others := 0, 0
	ast.Inspect(value, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok || (isWhole && literal == whole) {
			return true
		}
		word, number := "func", &others
		if read.isAction(literal.Type) {
			word, number = "action", &actions
		}
		*number++
		key := fmt.Sprintf("%s.%s%d", name, word, *number)
		found = append(found, read.unit(key, literal.Pos(), literal.End()))
		return true
	})
	return found
}
