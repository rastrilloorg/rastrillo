package counts

import (
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mibk/dupl/job"
	"github.com/mibk/dupl/syntax"
	"golang.org/x/tools/go/packages"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// duplThreshold is dupl's own default: a clone is at least 100 nodes of
// its serialized syntax.
const duplThreshold = 100

// offenceTally attaches codelimits offences to authored code, each at
// most once however many units it falls in.
type offenceTally struct {
	all      []codelimits.Offence
	attached []bool
}

func newOffenceTally(all []codelimits.Offence) *offenceTally {
	return &offenceTally{all: all, attached: make([]bool, len(all))}
}

// nestedKey is the suffix codelimits gives a literal inside a unit.
var nestedKey = regexp.MustCompile(`\.(action|func)\d+$`)

// overLimit takes the offences inside the file's authored units and
// returns the lines they add up to for each unit: a wide line or a
// one-letter name is one, a long function is the lines past its limit.
// Each offence goes to one unit only, so units that overlap (a literal
// nested in a variable's value) are not counted twice: a wide line or a
// name to the innermost unit holding its line, a length offence to the
// unit with its key, or else to the unit its nested key (Name.action2)
// is inside.
func (tally *offenceTally) overLimit(file authored.File) []int {
	lines := make([]int, len(file.Funcs))
	for index, offence := range tally.all {
		if offence.Path != file.Path {
			continue
		}
		unit := owner(offence, file.Funcs)
		if unit < 0 {
			continue
		}
		tally.attached[index] = true
		if offence.Kind == codelimits.FuncLines {
			lines[unit] += offence.Size - offence.Limit
		} else {
			lines[unit]++
		}
	}
	return lines
}

// owner is the index of the unit an offence belongs to, or -1.
func owner(offence codelimits.Offence, units []authored.Func) int {
	best := -1
	switch offence.Kind {
	case codelimits.FuncLines:
		stripped := nestedKey.ReplaceAllString(offence.Key, "")
		for index, unit := range units {
			if unit.Key == offence.Key {
				return index
			}
			if unit.Key == stripped && best < 0 {
				best = index
			}
		}
	case codelimits.Columns, codelimits.Names:
		for index, unit := range units {
			if offence.Line < unit.Start || offence.Line > unit.End {
				continue
			}
			if best < 0 || unit.End-unit.Start <
				units[best].End-units[best].Start {
				best = index
			}
		}
	}
	return best
}

// attachFile takes what the units did not: every offence of a whole
// file, and the length of a file that holds any authored code.
func (tally *offenceTally) attachFile(file authored.File) {
	for index, offence := range tally.all {
		if offence.Path != file.Path {
			continue
		}
		if offence.Kind == codelimits.FileLines || file.Whole {
			tally.attached[index] = true
		}
	}
}

// totals counts the offences attached, in test files or outside them.
func (tally *offenceTally) totals(tests bool) map[codelimits.Kind]int {
	totals := map[codelimits.Kind]int{}
	for _, kind := range []codelimits.Kind{
		codelimits.Columns, codelimits.FileLines,
		codelimits.FuncLines, codelimits.Names,
	} {
		totals[kind] = 0
	}
	for index, offence := range tally.all {
		if tally.attached[index] &&
			strings.HasSuffix(offence.Path, "_test.go") == tests {
			totals[offence.Kind]++
		}
	}
	return totals
}

// duplicatedLines runs dupl over the app's Go files and returns, per
// file, the lines that sit in a clone of at least duplThreshold nodes.
// A line counts whether or not the other copy is authored: the caller
// only asks about authored lines.
func duplicatedLines(appDir string) (map[string]map[int]bool, error) {
	paths, err := goFiles(appDir)
	if err != nil {
		return nil, err
	}
	copied := map[string]map[int]bool{}
	if len(paths) == 0 {
		return copied, nil
	}
	feed := make(chan string)
	go func() {
		for _, path := range paths {
			feed <- path
		}
		close(feed)
	}()
	tree, data, done := job.BuildTree(job.Parse(feed))
	<-done
	tree.Update(&syntax.Node{Type: -1})
	groups := map[string][][]*syntax.Node{}
	for found := range tree.FindDuplOver(duplThreshold) {
		match := syntax.FindSyntaxUnits(*data, found, duplThreshold)
		if len(match.Frags) > 0 {
			groups[match.Hash] = append(groups[match.Hash], match.Frags...)
		}
	}
	for _, group := range groups {
		if err := markClones(appDir, copied, group); err != nil {
			return nil, err
		}
	}
	return copied, nil
}

// markClones records the lines of each copy in a group of clones. A
// group is every fragment matched under one hash, so a copy found
// twice is skipped on its first node's position.
func markClones(
	appDir string, copied map[string]map[int]bool, group [][]*syntax.Node,
) error {
	seen := map[string]bool{}
	for _, fragment := range group {
		first, last := fragment[0], fragment[len(fragment)-1]
		key := fmt.Sprintf("%s:%d", first.Filename, first.Pos)
		if seen[key] {
			continue
		}
		seen[key] = true
		source, err := os.ReadFile(first.Filename)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(appDir, first.Filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if copied[relative] == nil {
			copied[relative] = map[int]bool{}
		}
		from := 1 + strings.Count(string(source[:first.Pos]), "\n")
		to := 1 + strings.Count(string(source[:max(last.End-1, 0)]), "\n")
		for line := from; line <= to; line++ {
			copied[relative][line] = true
		}
	}
	return nil
}

// goFiles lists the app's Go files as absolute paths, leaving out
// hidden and underscore directories, vendored code, test data and
// nested modules, as authored does.
func goFiles(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(
		path string, entry fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDirectory(root, path, entry.Name())
		}
		if strings.HasSuffix(path, ".go") {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

func skipDirectory(root, path, name string) error {
	if path == root {
		return nil
	}
	if slices.Contains([]string{"testdata", "vendor", "node_modules"}, name) ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return filepath.SkipDir
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

// interfaces counts the interfaces the agent wrote, in files it wrote
// whole, that exactly one type in the app implements. Only the app's
// own packages are looked at, tests left out. It is nil when the app
// does not load and type-check: the number is then unavailable.
func interfaces(appDir string, code authored.Code) *int {
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports,
		Dir: appDir,
		Env: append(os.Environ(), "GOFLAGS=-mod=readonly", "GOPROXY=off"),
	}, "./...")
	if err != nil || len(loaded) == 0 {
		return nil
	}
	var failed bool
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		failed = failed || len(pkg.Errors) > 0
	})
	if failed {
		return nil
	}
	whole := map[string]bool{}
	for _, file := range code.Files {
		if file.Whole {
			whole[canonical(filepath.Join(appDir, file.Path))] = true
		}
	}
	single := 0
	for _, pkg := range loaded {
		for _, name := range pkg.Types.Scope().Names() {
			declared := pkg.Fset.Position(pkg.Types.Scope().Lookup(name).Pos())
			if !whole[canonical(declared.Filename)] {
				continue
			}
			if implementations(loaded, pkg.Types.Scope().Lookup(name)) == 1 {
				single++
			}
		}
	}
	return &single
}

// implementations is how many named non-interface types in the app
// implement object, if it is an interface with methods; else -1.
func implementations(loaded []*packages.Package, object types.Object) int {
	named, ok := object.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return -1
	}
	wanted, ok := named.Underlying().(*types.Interface)
	if !ok || wanted.NumMethods() == 0 {
		return -1
	}
	count := 0
	for _, pkg := range loaded {
		for _, name := range pkg.Types.Scope().Names() {
			if implements(pkg.Types.Scope().Lookup(name), wanted) {
				count++
			}
		}
	}
	return count
}

func implements(object types.Object, wanted *types.Interface) bool {
	declared, ok := object.(*types.TypeName)
	if !ok || declared.IsAlias() {
		return false
	}
	named, ok := declared.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return false
	}
	if _, isInterface := named.Underlying().(*types.Interface); isInterface {
		return false
	}
	return types.Implements(named, wanted) ||
		types.Implements(types.NewPointer(named), wanted)
}

// canonical is a path made absolute with symlinks resolved, so the same
// file reads the same whether the app was named relatively, through a
// link, or by the loader.
func canonical(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return absolute
	}
	return resolved
}
