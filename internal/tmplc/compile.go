// Package tmplc compiles an app's captured template sets to Go: the
// rastrillo_templates_gen.go that rastrillo generate writes into the
// package that built the sets. Each template an entry reaches, in the
// entry's escaped trees, is lowered to a small statement tree (block,
// stmt, expr in lowered.go), which the writer writes as a function that
// renders what html/template would. The entry's function is registered
// under its fingerprint, so a set runs it only while its templates are
// the ones it was compiled from.
//
// The semantics are text/template's, and where they depend on the data
// the generated code calls tmplrt's ports of them. What does not depend
// on the data is decided here: which builtin a name is, how many
// arguments it was given, which variable a $name is.
//
// A template that uses a construct the compiler does not handle stays
// on html/template, as does every template that calls it, and is
// reported in Output.Unsupported. The rest of the set still compiles.
package tmplc

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// Input is one package's captured sets.
type Input struct {
	// PkgPath and PkgName are the package the file is written into, which
	// built the sets.
	PkgPath, PkgName string
	// Types is the package as type-checked without its generated file,
	// so that the file's names can be chosen not to clash with its own.
	// It may be nil.
	Types *types.Package
	Sets  []tmpl.CapturedSet
	// Escaped holds, for each of Sets, tmplescape.EscapeAll's result for
	// its templates: each entry is compiled from its own escaped trees.
	Escaped []map[string]tmplescape.Result
}

// Output is the generated file and what it leaves on html/template.
type Output struct {
	// Source is the file, gofmt'ed.
	Source      []byte
	Unsupported []Note
}

// Note says why a template is not compiled.
type Note struct {
	Owner, Set, Template, Reason string
}

// Compile writes the generated file for in's sets. Its error is for
// input that does not fit together; a template the compiler cannot
// handle is a Note.
func Compile(in Input) (Output, error) {
	if len(in.Escaped) != len(in.Sets) {
		return Output{}, fmt.Errorf(
			"tmplc: %d sets but %d escaping results",
			len(in.Sets), len(in.Escaped))
	}
	generated := newFile(in)
	var notes []Note
	left := map[string]bool{} // fingerprints
	for index, set := range in.Sets {
		escaped := in.Escaped[index]
		for _, entry := range slices.Sorted(maps.Keys(set.Fingerprints)) {
			reason, quiet, err := generated.entry(set, escaped, entry)
			if err != nil {
				return Output{}, err
			}
			if reason == "" {
				continue
			}
			left[set.Fingerprints[entry]] = true
			if !quiet {
				notes = append(notes, Note{
					Owner: in.PkgPath, Set: set.Name,
					Template: entry, Reason: reason,
				})
			}
		}
	}
	source, err := generated.source(slices.Sorted(maps.Keys(left)))
	if err != nil {
		return Output{}, err
	}
	// Clones of one set report the same notes.
	slices.SortFunc(notes, compareNotes)
	return Output{Source: source, Unsupported: slices.Compact(notes)}, nil
}

func compareNotes(first, second Note) int {
	return cmp.Or(
		cmp.Compare(first.Set, second.Set),
		cmp.Compare(first.Template, second.Template),
		cmp.Compare(first.Reason, second.Reason),
	)
}

// entry compiles one entry of set, and returns why it cannot be, or "",
// and whether the reason is not worth reporting.
func (generated *file) entry(
	set tmpl.CapturedSet, escaped map[string]tmplescape.Result, entry string,
) (why string, quiet bool, err error) {
	fingerprint := set.Fingerprints[entry]
	if _, done := generated.binds[fingerprint]; done {
		return "", false, nil
	}
	if why := set.Uncompilable[entry]; why != "" {
		return why, false, nil
	}
	result, ok := escaped[entry]
	if !ok {
		return "", false, fmt.Errorf(
			"tmplc: set %q: no escaping result for %q", set.Name, entry)
	}
	if result.Err != nil {
		// html/template fails the same way at runtime, so the set falls
		// back and errors as it always did. In a set the app clones, as
		// ui.NewPages clones its base for each page, the template it
		// calls may be defined only in the clones: the set is built to
		// be cloned, not run, and there is nothing to tell the author.
		why := "html/template cannot escape it: " + result.Err.Error()
		return why, set.Cloned, nil
	}
	lowering := newLowerer(generated, set, result.Trees)
	// A callee the capture could not record faithfully is compiled from
	// a tree that is not the one the app runs.
	for name, why := range set.Uncompilable {
		if name != entry {
			lowering.checked[name] = "cannot be compiled: " + why
		}
	}
	if _, err := lowering.template(entry); err != nil {
		return err.Error(), false, nil
	}
	return "", false, generated.add(fingerprint, entry, lowering)
}

func newLowerer(
	file *file, set tmpl.CapturedSet, trees map[string]*parse.Tree,
) *lowerer {
	return &lowerer{
		file:    file,
		set:     set,
		trees:   trees,
		missing: missingKey(set.Option),
		checked: map[string]string{},
		bodies:  map[string]*body{},
	}
}

// missingKey is the tmplrt constant for a set's missingkey option.
func missingKey(option string) string {
	switch option {
	case "zero":
		return "MissingKeyZero"
	case "error":
		return "MissingKeyError"
	}
	return "MissingKeyDefault"
}
