package tmplescape

import (
	"fmt"
	"text/template/parse"
)

// template stands in for the *text/template.Template that html/template's
// escaper holds: a name and a parse tree. Embedding the tree keeps the
// forked code's t.Tree and t.Root as they are.
type template struct {
	name string
	*parse.Tree
}

func (tmpl *template) Name() string { return tmpl.name }

// nameSpace stands in for html/template's nameSpace and the text/template
// namespace under it, for one escaping pass.
type nameSpace struct {
	// set is the trees EscapeAll was given. A nil tree is a template the
	// set names but never defined. These trees are never edited.
	set map[string]*parse.Tree
	// text is this pass's templates: a copy of each tree from set the pass
	// has looked at, which commit edits, and the templates it derived.
	text map[string]*template
	// opts stands in for the GODEBUG settings html/template reads.
	opts Options
}

func newNameSpace(set map[string]*parse.Tree, opts Options) *nameSpace {
	return &nameSpace{set: set, text: map[string]*template{}, opts: opts}
}

// lookup is text/template's Lookup. It copies a tree the first time the
// pass asks for it, so a pass edits only its own copies.
func (ns *nameSpace) lookup(name string) *template {
	if found, ok := ns.text[name]; ok {
		return found
	}
	tree := ns.set[name]
	if tree == nil {
		return nil
	}
	copied := &template{name: name, Tree: tree.Copy()}
	ns.text[name] = copied
	return copied
}

// addParseTree is text/template's AddParseTree for a derived template.
func (ns *nameSpace) addParseTree(derived *template) {
	ns.text[derived.Name()] = derived
}

// Options are the settings html/template reads from GODEBUG, which the
// fork cannot read itself (internal/godebug is not importable). The
// caller passes the values the app's process would see.
type Options struct {
	// MetaContentURLEscape is GODEBUG htmlmetacontenturlescape: true,
	// html/template's default, filters a URL in
	// <meta http-equiv="refresh" content="...">; false
	// (htmlmetacontenturlescape=0) leaves it as it is.
	MetaContentURLEscape bool
}

// Result is one entry's escaping: what html/template would do on the
// first Execute of that template on a fresh set.
type Result struct {
	// Trees holds the entry's escaped tree and every template it reaches,
	// keyed by name. That includes the templates escaping derived for
	// callees called from other contexts, such as a block inside <title>,
	// named name$htmltemplate_ and the context, as html/template names
	// them. Each {{template}} call names the callee it now needs, and each
	// action's pipeline ends in the _html_template_* escapers
	// html/template would add, which tmplrt implements under the same
	// names. Trees is nil when Err is set.
	Trees map[string]*parse.Tree
	// Err is the error html/template's Execute would return before writing
	// anything: an *Error with html/template's ErrorCode, or the error for
	// a template that has no tree.
	Err error
}

// EscapeAll escapes every template in trees as an entry, starting in
// plain HTML text, and returns each entry's Result keyed by its name. The
// trees passed in are not changed.
//
// Each entry is escaped on its own copies, and its Result holds only what
// that entry reaches, because html/template's output depends on which
// entry ran first. It edits a set's trees in place as each entry first
// executes and derives later copies from the edited trees, so escaping
// all entries in one pass would give a partial called from a <script>
// the escapers of its first, plain-HTML use as well. An app serving a
// page runs that page's entry first.
//
// The same template can therefore appear in several Results. Its tree is
// usually the same in each, but not always: an app may define a template
// whose name a derived one would have, which html/template then escapes
// in the derived context for one entry and in plain text for another.
func EscapeAll(trees map[string]*parse.Tree, opts Options) map[string]Result {
	results := make(map[string]Result, len(trees))
	for name, tree := range trees {
		results[name] = escapeEntry(trees, opts, name, tree)
	}
	return results
}

func escapeEntry(
	trees map[string]*parse.Tree, opts Options, name string,
	tree *parse.Tree,
) Result {
	// html/template's lookupAndEscapeTemplate refuses these before
	// escaping, with this error.
	if tree == nil || tree.Root == nil {
		return Result{Err: fmt.Errorf(
			"html/template: %q is an incomplete template", name)}
	}
	escaper := makeEscaper(newNameSpace(trees, opts))
	err := escapeTemplate(&escaper, escaper.template(name).Root, name)
	if err != nil {
		return Result{Err: err}
	}
	// escaper.output names every template this pass escaped, in each
	// context it reached it in, so it is the entry's whole reach.
	reached := make(map[string]*parse.Tree, len(escaper.output))
	for escapedName := range escaper.output {
		reached[escapedName] = escaper.template(escapedName).Tree
	}
	return Result{Trees: reached}
}
