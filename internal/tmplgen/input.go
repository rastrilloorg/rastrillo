package tmplgen

import (
	"go/types"
	"maps"
	"text/template/parse"

	"golang.org/x/tools/go/packages"

	"amadan.net/rastrillo/rastrillo/internal/tmplc"
	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// input re-parses and escapes the sets pkg built, for tmplc.
func input(
	pkg *packages.Package,
	sets []tmpl.CapturedSet,
	pkgs map[string]*packages.Package,
) (tmplc.Input, error) {
	in := tmplc.Input{
		PkgPath: pkg.PkgPath, PkgName: pkg.Name, Types: pkg.Types,
	}
	lookup := func(path string) *types.Package {
		if imported := pkgs[path]; imported != nil {
			return imported.Types
		}
		return nil
	}
	for _, set := range sets {
		set.Uncompilable = maps.Clone(set.Uncompilable)
		if set.Uncompilable == nil {
			set.Uncompilable = map[string]string{}
		}
		trees := parseTrees(set)
		set.Funcs = nameableFuncs(set.Funcs, pkg.PkgPath, lookup)
		in.Sets = append(in.Sets, set)
		in.Escaped = append(in.Escaped, tmplescape.EscapeAll(
			trees,
			tmplescape.Options{
				MetaContentURLEscape: set.MetaContentURLEscape,
			},
		))
	}
	return in, nil
}

// parseTrees parses the captured source of set's templates. One that
// does not parse is left out and marked uncompilable in set.
func parseTrees(set tmpl.CapturedSet) map[string]*parse.Tree {
	trees := make(map[string]*parse.Tree, len(set.Templates))
	for name, src := range set.Templates {
		tree := parse.New(name)
		tree.Mode = parse.SkipFuncCheck
		_, err := tree.Parse(src, "", "", map[string]*parse.Tree{})
		if err != nil {
			// Capture reported it already, and escaping sees the
			// template as undefined.
			if set.Uncompilable[name] == "" {
				set.Uncompilable[name] = unparsedSource + err.Error()
			}
			continue
		}
		trees[name] = tree
	}
	return trees
}

// unparsedSource is the reason a template whose captured source does
// not parse stays uncompiled.
const unparsedSource = "its captured source does not parse: "

// nameableFuncs is funcs, each with Nameable settled by nameable for
// generated code in package owner.
func nameableFuncs(
	funcs map[string]tmpl.FuncSig,
	owner string,
	lookup func(path string) *types.Package,
) map[string]tmpl.FuncSig {
	settled := make(map[string]tmpl.FuncSig, len(funcs))
	for name, sig := range funcs {
		sig.Nameable = nameable(sig, owner, lookup)
		settled[name] = sig
	}
	return settled
}

// nameable is whether generated code in package owner can write sig's
// type. Capture's answer is provisional: reflect cannot tell a type
// declared inside a function from a package-level one, so each type sig
// names must be a type at its package's top level, and exported unless
// it is owner's own. lookup returns a package's types, or nil.
func nameable(
	sig tmpl.FuncSig,
	owner string,
	lookup func(path string) *types.Package,
) bool {
	if !sig.Nameable {
		return false
	}
	for _, ref := range sig.Refs {
		pkg := lookup(ref.PkgPath)
		if pkg == nil {
			return false
		}
		tn, ok := pkg.Scope().Lookup(ref.Name).(*types.TypeName)
		if !ok || (ref.PkgPath != owner && !tn.Exported()) {
			return false
		}
	}
	return true
}
