package tmplmigrate

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing/fstest"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/ui"
)

// localeFuncs is built from ui.LocaleFuncs rather than listed here, so
// the codemod and the runtime cannot disagree about which calls take
// $L: a function added to one list and not the other would be left in
// the legacy form and fail strict dispatch in production.
var localeFuncs = func() map[string]bool {
	m := make(map[string]bool, len(ui.LocaleFuncs))
	for _, f := range ui.LocaleFuncs {
		m[f] = true
	}
	return m
}()

// Prelude binds $L at the top of every template body that uses it.
const Prelude = `{{$L := localizer $}}`

// Stats counts what Rewrite did.
type Stats struct {
	Calls    int
	Dicts    int
	Wrapped  int
	Preludes int
	Review   []string // wrapped sites: struct data needs an L field
	NoData   []string // {{template "x"}} with no data: the callee gets nil
}

// Add accumulates another file's counts and reports into s.
func (s *Stats) Add(o Stats) {
	s.Calls += o.Calls
	s.Dicts += o.Dicts
	s.Wrapped += o.Wrapped
	s.Preludes += o.Preludes
	s.Review = append(s.Review, o.Review...)
	s.NoData = append(s.NoData, o.NoData...)
}

// ConflictError is a file that binds $L to something else.
type ConflictError struct {
	File  string
	Sites []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: $L already has another meaning, so the file is left unchanged: %s", e.File, strings.Join(e.Sites, "; "))
}

type insert struct {
	at   int
	text string
}

func parseAll(name, src string) (map[string]*parse.Tree, error) {
	t := parse.New(name)
	t.Mode = parse.SkipFuncCheck | parse.ParseComments
	trees := map[string]*parse.Tree{}
	if _, err := t.Parse(src, "", "", trees); err != nil {
		return nil, err
	}
	return trees, nil
}

func sortedNames(trees map[string]*parse.Tree) []string {
	var names []string
	for n, tr := range trees {
		if tr.Root != nil {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Rewrite migrates one template file's text. On any error it returns
// src unchanged, so a caller that writes the result regardless can
// never empty or half-migrate a file.
func Rewrite(name, src string) (string, Stats, error) {
	trees, err := parseAll(name, src)
	if err != nil {
		return src, Stats{}, err
	}
	var st Stats
	var ins []insert
	var wrappedPipes []string
	var conflicts []string
	for _, n := range sortedNames(trees) {
		tr := trees[n]
		before := len(ins)
		ours := hasPrelude(tr.Root)
		walk(tr.Root, 0, false, func(node parse.Node, depth int, dotMoved bool) {
			switch x := node.(type) {
			case *parse.ActionNode:
				conflicts = append(conflicts, declConflicts(x.Pipe, depth, ours && x == firstAction(tr.Root))...)
			case *parse.RangeNode:
				conflicts = append(conflicts, declConflicts(x.Pipe, 1, false)...)
			case *parse.WithNode:
				conflicts = append(conflicts, declConflicts(x.Pipe, 1, false)...)
			case *parse.IfNode:
				conflicts = append(conflicts, declConflicts(x.Pipe, 1, false)...)
			case *parse.CommandNode:
				if id, ok := x.Args[0].(*parse.IdentifierNode); ok && localeFuncs[id.Ident] && !passesL(x) {
					ins = append(ins, insert{int(id.Pos) + len(id.Ident), " $L"})
					st.Calls++
				}
			case *parse.TemplateNode:
				switch {
				case x.Pipe == nil:
					st.NoData = append(st.NoData, fmt.Sprintf("%s: {{template %q}} passes no data, so %q gets no localizer", name, x.Name, x.Name))
				case dictCall(x.Pipe) != nil:
					if d := dictCall(x.Pipe); !hasLKey(d) {
						ins = append(ins, insert{int(d.Args[0].Position()) + len("dict"), ` "L" $L`})
						st.Dicts++
					}
				case isWithL(x.Pipe) || passesSame(x.Pipe, dotMoved):
				default:
					start := int(x.Pipe.Position())
					end, err := pipeEnd(src, start)
					if err != nil {
						conflicts = append(conflicts, err.Error())
						return
					}
					ins = append(ins, insert{start, "(withL ("}, insert{end, ") $L)"})
					wrappedPipes = append(wrappedPipes, canonical(x.Pipe))
					st.Wrapped++
					st.Review = append(st.Review, fmt.Sprintf("%s: template %q %s", name, x.Name, strings.TrimSpace(src[start:end])))
				}
			}
		})
		if len(ins) > before && !ours {
			ins = append(ins, insert{preludeAt(src, tr.Root), Prelude})
			st.Preludes++
		}
	}
	if len(conflicts) > 0 {
		return src, Stats{}, &ConflictError{File: name, Sites: conflicts}
	}
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at > ins[j].at })
	out := src
	for _, in := range ins {
		out = out[:in.at] + in.text + out[in.at:]
	}
	if err := validate(name, out, wrappedPipes); err != nil {
		return src, Stats{}, fmt.Errorf("%s: the rewrite did not validate, so nothing was changed: %w", name, err)
	}
	return out, st, nil
}

// preludeAt is where a body's prelude goes: its start, as the template
// renders it, and never inside white space a trim marker removes (the
// space would then render). A body's parsed position is its first
// token's, which needs correcting:
//   - A leading comment's token begins after "{{" and any "- " trim
//     marker, so inserting there puts the prelude inside the comment's
//     delimiters and the result no longer parses.
//   - A leading "{{-" has already trimmed the white space before it.
//     The prelude goes before that white space, so the trim marker
//     still removes it.
//   - A file that starts with a byte-order mark keeps it at byte 0: a
//     BOM moved off byte 0 is no longer a BOM, and the page renders a
//     stray character.
//
// A file's top-level body that begins after a {{define}} has the
// define's position, and the define is not one of its nodes. So the
// comment correction applies only when the comment is the token at the
// body's position, and every index is bounds-checked: reading the
// first node's kind alone moved a top-level position before byte 0
// and panicked.
func preludeAt(src string, root *parse.ListNode) int {
	bom := 0
	if strings.HasPrefix(src, "\ufeff") {
		bom = len("\ufeff")
	}
	at := int(root.Pos)
	if at < bom || at > len(src) {
		return bom
	}
	if len(root.Nodes) > 0 && root.Nodes[0].Position() == root.Pos {
		if _, ok := root.Nodes[0].(*parse.CommentNode); ok {
			switch {
			case at-4 >= bom && src[at-4:at-2] == "{{" && src[at-2] == '-' && isTrimSpace(src[at-1]):
				at -= 4
			case at-2 >= bom && src[at-2:at] == "{{":
				at -= 2
			}
		}
	}
	if strings.HasPrefix(src[at:], "{{-") && len(src) > at+3 && isTrimSpace(src[at+3]) {
		for at > bom && isTrimSpace(src[at-1]) {
			at--
		}
	}
	return at
}

// isTrimSpace is the white space a trim marker removes, and the space
// that must follow "{{-" for it to be a trim marker (text/template's
// spaceChars).
func isTrimSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// validate parses the rewritten text and checks the rewrite's promises
// on the new tree: every locale call passes $L, every template call
// passes it (or passes nothing and was reported), every body using $L
// starts with the prelude, and each wrapped pipeline is exactly the
// original one, so no argument was regrouped.
func validate(name, out string, wrapped []string) error {
	trees, err := parseAll(name, out)
	if err != nil {
		return err
	}
	var got []string
	var problems []string
	for _, n := range sortedNames(trees) {
		tr := trees[n]
		uses := false
		walk(tr.Root, 0, false, func(node parse.Node, _ int, dotMoved bool) {
			switch x := node.(type) {
			case *parse.CommandNode:
				if id, ok := x.Args[0].(*parse.IdentifierNode); ok && localeFuncs[id.Ident] {
					uses = true
					if !passesL(x) {
						problems = append(problems, x.String()+" lacks $L")
					}
				}
			case *parse.TemplateNode:
				if x.Pipe == nil {
					return
				}
				switch {
				case dictCall(x.Pipe) != nil:
					uses = true
					if !hasLKey(dictCall(x.Pipe)) {
						problems = append(problems, x.String()+` lacks "L"`)
					}
				case isWithL(x.Pipe):
					uses = true
					got = append(got, canonical(withLInner(x.Pipe)))
				case passesSame(x.Pipe, dotMoved):
				default:
					problems = append(problems, x.String()+" passes data without $L")
				}
			}
		})
		if uses && !hasPrelude(tr.Root) {
			problems = append(problems, "template "+n+" uses $L without the prelude")
		}
	}
	// Wrapped pipelines compare as sets of strings in traversal order;
	// idempotent re-runs wrap nothing, so only this run's appear.
	for i, w := range wrapped {
		if i >= len(got) || !contains(got, w) {
			problems = append(problems, "wrapped pipeline changed: "+w)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// withLInner returns the original pipeline inside (withL (P) $L).
func withLInner(p *parse.PipeNode) *parse.PipeNode {
	c := p.Cmds[0]
	if inner, ok := c.Args[0].(*parse.PipeNode); ok && len(c.Args) == 1 {
		return withLInner(inner)
	}
	if inner, ok := c.Args[1].(*parse.PipeNode); ok {
		return inner
	}
	return &parse.PipeNode{NodeType: parse.NodePipe, Cmds: []*parse.CommandNode{{NodeType: parse.NodeCommand, Args: []parse.Node{c.Args[1]}}}}
}

// canonical renders a pipeline with the codemod's own permitted
// insertions removed: $L as a locale call's first argument and "L" $L
// in a dict. A wrapped pipeline must equal the original under this
// normalisation, so the rewrite may add the localizer inside it (as in
// makeData (T "key")) but may not regroup or change anything else.
func canonical(p *parse.PipeNode) string {
	c := p.Copy().(*parse.PipeNode)
	strip(c)
	return c.String()
}

func strip(n parse.Node) {
	switch x := n.(type) {
	case *parse.PipeNode:
		if x == nil {
			return
		}
		for _, c := range x.Cmds {
			strip(c)
		}
	case *parse.CommandNode:
		if id, ok := x.Args[0].(*parse.IdentifierNode); ok {
			if localeFuncs[id.Ident] && passesL(x) {
				x.Args = append(x.Args[:1:1], x.Args[2:]...)
			}
			if id.Ident == "dict" {
				for i := 1; i+1 < len(x.Args); i += 2 {
					if s, ok := x.Args[i].(*parse.StringNode); ok && s.Text == "L" {
						if v, ok := x.Args[i+1].(*parse.VariableNode); ok && len(v.Ident) == 1 && v.Ident[0] == "$L" {
							x.Args = append(x.Args[:i:i], x.Args[i+2:]...)
							break
						}
					}
				}
			}
		}
		for _, a := range x.Args {
			strip(a)
		}
	}
}

func firstAction(root *parse.ListNode) *parse.ActionNode {
	for _, n := range root.Nodes {
		switch x := n.(type) {
		case *parse.ActionNode:
			return x
		case *parse.TextNode:
			text := string(x.Text)
			if x.Pos == 0 {
				// preludeAt leaves a file's byte-order mark before the
				// prelude; without this the prelude goes unseen and every
				// file with a BOM fails validation.
				text = strings.TrimPrefix(text, "\ufeff")
			}
			if strings.TrimSpace(text) != "" {
				return nil
			}
		case *parse.CommentNode:
		default:
			return nil
		}
	}
	return nil
}

func hasPrelude(root *parse.ListNode) bool {
	a := firstAction(root)
	return a != nil && isPrelude(a.Pipe)
}

func isPrelude(p *parse.PipeNode) bool {
	return p != nil && !p.IsAssign && len(p.Decl) == 1 && p.Decl[0].Ident[0] == "$L" &&
		len(p.Cmds) == 1 && p.Cmds[0].String() == "localizer $"
}

// declConflicts reports any binding of $L except the prelude at the top
// of a body.
func declConflicts(p *parse.PipeNode, depth int, isOurPrelude bool) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, d := range p.Decl {
		if d.Ident[0] != "$L" {
			continue
		}
		if depth == 0 && isOurPrelude && isPrelude(p) {
			continue
		}
		verb := ":="
		if p.IsAssign {
			verb = "="
		}
		out = append(out, fmt.Sprintf("line %d: $L %s %s", lineOf(p), verb, p.String()))
	}
	return out
}

func lineOf(p *parse.PipeNode) int { return p.Line }

func passesL(c *parse.CommandNode) bool {
	if len(c.Args) < 2 {
		return false
	}
	v, ok := c.Args[1].(*parse.VariableNode)
	return ok && len(v.Ident) == 1 && v.Ident[0] == "$L"
}

func dictCall(p *parse.PipeNode) *parse.CommandNode {
	if len(p.Cmds) != 1 || len(p.Decl) > 0 {
		return nil
	}
	c := p.Cmds[0]
	if id, ok := c.Args[0].(*parse.IdentifierNode); ok && id.Ident == "dict" {
		return c
	}
	if inner, ok := c.Args[0].(*parse.PipeNode); ok && len(c.Args) == 1 {
		return dictCall(inner)
	}
	return nil
}

func isWithL(p *parse.PipeNode) bool {
	if len(p.Cmds) != 1 {
		return false
	}
	c := p.Cmds[0]
	if inner, ok := c.Args[0].(*parse.PipeNode); ok && len(c.Args) == 1 {
		return isWithL(inner)
	}
	id, ok := c.Args[0].(*parse.IdentifierNode)
	return ok && id.Ident == "withL"
}

func hasLKey(c *parse.CommandNode) bool {
	for i := 1; i < len(c.Args); i += 2 {
		if s, ok := c.Args[i].(*parse.StringNode); ok && s.Text == "L" {
			return true
		}
	}
	return false
}

func passesSame(p *parse.PipeNode, dotMoved bool) bool {
	if len(p.Cmds) != 1 || len(p.Cmds[0].Args) != 1 || len(p.Decl) > 0 {
		return false
	}
	switch a := p.Cmds[0].Args[0].(type) {
	case *parse.DotNode:
		return !dotMoved
	case *parse.VariableNode:
		return len(a.Ident) == 1 && a.Ident[0] == "$"
	}
	return false
}

// pipeEnd is where a template call's pipeline ends: before the action's
// closing delimiter and any trim marker. It tokenises quote-aware, so a
// "}}" inside an interpreted string, a raw string or a rune literal is
// skipped, and the result is then confirmed by validate's re-parse.
func pipeEnd(src string, start int) (int, error) {
	i := start
	for i < len(src) {
		switch c := src[i]; c {
		case '"', '\'':
			i++
			for i < len(src) && src[i] != c {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case '`':
			j := strings.IndexByte(src[i+1:], '`')
			if j < 0 {
				return 0, fmt.Errorf("unterminated raw string at byte %d", i)
			}
			i += j + 2
		case '}':
			if strings.HasPrefix(src[i:], "}}") {
				end := i
				if end > start && src[end-1] == '-' && end-1 > start && isTrimSpace(src[end-2]) {
					end--
				}
				for end > start && isTrimSpace(src[end-1]) {
					end--
				}
				return end, nil
			}
			i++
		default:
			i++
		}
	}
	return 0, fmt.Errorf("no closing delimiter after byte %d", start)
}

// walk visits every node with its control depth and whether "." has
// moved away from the template's $ (inside range and with).
func walk(n parse.Node, depth int, dotMoved bool, f func(parse.Node, int, bool)) {
	switch x := n.(type) {
	case nil:
		return
	case *parse.ListNode:
		if x == nil {
			return
		}
		f(n, depth, dotMoved)
		for _, c := range x.Nodes {
			walk(c, depth, dotMoved, f)
		}
	case *parse.PipeNode:
		if x == nil {
			return
		}
		f(n, depth, dotMoved)
		for _, c := range x.Cmds {
			walk(c, depth, dotMoved, f)
		}
	case *parse.ActionNode:
		f(n, depth, dotMoved)
		walk(x.Pipe, depth, dotMoved, f)
	case *parse.CommandNode:
		f(n, depth, dotMoved)
		for _, a := range x.Args {
			walk(a, depth, dotMoved, f)
		}
	case *parse.IfNode:
		f(n, depth, dotMoved)
		walk(x.Pipe, depth, dotMoved, f)
		walk(x.List, depth+1, dotMoved, f)
		walk(x.ElseList, depth+1, dotMoved, f)
	case *parse.RangeNode:
		f(n, depth, dotMoved)
		walk(x.Pipe, depth, dotMoved, f)
		walk(x.List, depth+1, true, f)
		walk(x.ElseList, depth+1, dotMoved, f)
	case *parse.WithNode:
		f(n, depth, dotMoved)
		walk(x.Pipe, depth, dotMoved, f)
		walk(x.List, depth+1, true, f)
		walk(x.ElseList, depth+1, dotMoved, f)
	case *parse.TemplateNode:
		f(n, depth, dotMoved)
		walk(x.Pipe, depth, dotMoved, f)
	default:
		f(n, depth, dotMoved)
	}
}

// rewriteFile is Rewrite, held in a variable so a test can make it
// panic and check that RewriteFS contains it.
var rewriteFile = Rewrite

// rewriteContained turns a panic while rewriting one file into that
// file's error, with the file left unchanged. Without it, one template
// that trips a bug ends the whole migrate-templates run and reports
// nothing about the files already done.
func rewriteContained(path, src string) (text string, st Stats, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, st = src, Stats{}
			err = fmt.Errorf("%s: the rewrite did not validate, so nothing was changed: %w", path, errors.New(fmt.Sprint(r)))
		}
	}()
	return rewriteFile(path, src)
}

// RewriteFS migrates every .html file of fsys under root into memory.
func RewriteFS(fsys fs.FS, root string) (fstest.MapFS, Stats, error) {
	out := fstest.MapFS{}
	var total Stats
	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") || strings.Contains(path, "/static/") {
			return err
		}
		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		text, st, err := rewriteContained(path, string(src))
		if err != nil {
			return err
		}
		total.Add(st)
		out[path] = &fstest.MapFile{Data: []byte(text)}
		return nil
	})
	return out, total, err
}
