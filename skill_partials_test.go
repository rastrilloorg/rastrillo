package rastrillo

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"text/template/parse"
)

var updateSkill = flag.Bool("update-skill", false, "rewrite skill/partials.md from ui/partials")

// skill/partials.md is generated, because the skilleval baseline's
// agents read ui/partials/*.html one by one to learn what each partial
// takes (19 of their source reads), and a hand-kept list would drift
// from the templates the first time a key was added. The keys come
// from each template's parse tree, the summary from its leading
// comment, so the reference says what the templates read, not what
// someone remembered them reading.
const partialsPath = "skill/partials.md"

func TestSkillPartialsReferenceIsCurrent(t *testing.T) {
	got, err := renderPartialsReference("ui/partials")
	if err != nil {
		t.Fatal(err)
	}
	if *updateSkill {
		if err := os.WriteFile(partialsPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(partialsPath)
	if err != nil {
		t.Fatalf("%s: %v (go test -run TestSkillPartialsReferenceIsCurrent -update-skill writes it)", partialsPath, err)
	}
	if string(want) != got {
		t.Errorf("%s is out of date with ui/partials: go test -run TestSkillPartialsReferenceIsCurrent -update-skill", partialsPath)
	}
}

type partialDoc struct {
	name, summary string
	keys          []string
	scalar        bool // renders dot itself: called with a value, not a dict
}

func renderPartialsReference(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil {
		return "", err
	}
	var docs []partialDoc
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		d, err := partialDocs(filepath.Base(f), string(b))
		if err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		docs = append(docs, d...)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].name < docs[j].name })
	var b strings.Builder
	b.WriteString("# ui partials\n\n")
	b.WriteString("Generated from ui/partials by TestSkillPartialsReferenceIsCurrent; do not edit.\n")
	b.WriteString("Call one as `{{template \"name\" dict \"L\" $L \"Key\" value ...}}`; ")
	b.WriteString("`L` is the localizer every page starts with. A key `X[]` is a list ")
	b.WriteString("whose items carry the keys in brackets. Keys a Go helper reads for ")
	b.WriteString("the template (row-menu's and pagination's items, say) are not listed; ")
	b.WriteString("each template's own comment says more.\n\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "- **%s**", d.name)
		if d.summary != "" {
			fmt.Fprintf(&b, ": %s", d.summary)
		}
		if len(d.keys) > 0 {
			fmt.Fprintf(&b, " Keys: %s.", strings.Join(d.keys, ", "))
		}
		if d.scalar {
			fmt.Fprintf(&b, " Takes a value, not a dict: `{{template \"%s\" .Message}}`.", d.name)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// partialDocs reads every {{define}} in one file. The summary is the
// first sentence of the file's leading comment, or of a comment just
// inside the define.
func partialDocs(file, text string) ([]partialDoc, error) {
	tr := parse.New(file)
	tr.Mode = parse.SkipFuncCheck | parse.ParseComments
	trees := map[string]*parse.Tree{}
	if _, err := tr.Parse(text, "{{", "}}", trees); err != nil {
		return nil, err
	}
	var docs []partialDoc
	for name, t := range trees {
		if name == file || t.Root == nil {
			continue
		}
		// A define's summary is the comment written just above it, or
		// failing that one just inside it; never another define's, as a
		// file's first comment can be (list-bar-search.html opens with
		// list-search-submit's).
		summary := commentAbove(text, name)
		if summary == "" {
			summary = firstComment(t.Root)
		}
		st := &walkState{keys: map[string]map[string]bool{}}
		walkKeys(t.Root, 0, "", st)
		keys := formatKeys(st.keys)
		docs = append(docs, partialDoc{name: name, summary: sentence(name, summary), keys: keys, scalar: st.dot && len(keys) == 0})
	}
	return docs, nil
}

// commentAbove is the comment that ends just before {{define "name"}},
// with only whitespace between.
func commentAbove(text, name string) string {
	loc := regexp.MustCompile(`\{\{-?\s*define\s+"` + regexp.QuoteMeta(name) + `"`).FindStringIndex(text)
	if loc == nil {
		return ""
	}
	before := strings.TrimRight(text[:loc[0]], " \t\r\n")
	if !strings.HasSuffix(before, "*/}}") && !strings.HasSuffix(before, "*/ -}}") {
		return ""
	}
	start := strings.LastIndex(before, "{{/*")
	if i := strings.LastIndex(before, "{{- /*"); i > start {
		start = i
	}
	if start < 0 {
		return ""
	}
	c := before[start:]
	c = strings.TrimPrefix(strings.TrimPrefix(c, "{{- "), "{{")
	c = strings.TrimSuffix(strings.TrimSuffix(c, " -}}"), "}}")
	return c
}

func firstComment(l *parse.ListNode) string {
	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.CommentNode:
			return n.Text
		case *parse.TextNode:
			if strings.TrimSpace(string(n.Text)) != "" {
				return ""
			}
		default:
			return ""
		}
	}
	return ""
}

var leadName = regexp.MustCompile(`^[a-z0-9-]+\s*(:|—|-)\s*`)

// sentence is the comment's first sentence, without its "name:" lead.
func sentence(name, comment string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(comment), "/*"), "*/"))
	s = strings.Join(strings.Fields(s), " ")
	s = leadName.ReplaceAllString(s, "")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if s != "" && !strings.HasSuffix(s, ".") {
		s += "."
	}
	if s != "" {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}

// walkKeys collects the keys a template reads from its own data: a
// field on dot where dot is still the data the caller passed (depth
// 0), or $.X anywhere. Inside a range or with over .X, dot is X's
// item, and its fields are recorded under X.
type walkState struct {
	keys map[string]map[string]bool
	dot  bool // the template renders its data itself at some point
}

func walkKeys(n parse.Node, depth int, under string, st *walkState) {
	keys := st.keys
	add := func(k, sub string) {
		if keys[k] == nil {
			keys[k] = map[string]bool{}
		}
		if sub != "" {
			keys[k][sub] = true
		}
	}
	switch n := n.(type) {
	case nil:
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			walkKeys(c, depth, under, st)
		}
	case *parse.ActionNode:
		walkKeys(n.Pipe, depth, under, st)
	case *parse.PipeNode:
		if n == nil {
			return
		}
		for _, c := range n.Cmds {
			walkKeys(c, depth, under, st)
		}
	case *parse.CommandNode:
		// opt . "Key" and opt $ "Key" read Key as surely as .Key does.
		if len(n.Args) >= 3 {
			if id, ok := n.Args[0].(*parse.IdentifierNode); ok && id.Ident == "opt" {
				if k, ok := n.Args[2].(*parse.StringNode); ok {
					switch on := n.Args[1].(type) {
					case *parse.DotNode:
						switch {
						case depth == 0:
							add(k.Text, "")
						case depth == 1 && under != "":
							add(under, k.Text)
						}
					case *parse.VariableNode:
						if on.Ident[0] == "$" && len(on.Ident) == 1 {
							add(k.Text, "")
						}
					}
					return
				}
			}
		}
		for _, a := range n.Args {
			if _, ok := a.(*parse.DotNode); ok && depth == 0 {
				st.dot = true
			}
			walkKeys(a, depth, under, st)
		}
	case *parse.ChainNode:
		walkKeys(n.Node, depth, under, st)
	case *parse.FieldNode:
		switch {
		case depth == 0:
			add(n.Ident[0], "")
		case depth == 1 && under != "":
			add(under, n.Ident[0])
		}
	case *parse.VariableNode:
		if n.Ident[0] == "$" && len(n.Ident) > 1 {
			add(n.Ident[1], "")
		}
	case *parse.TemplateNode:
		// Passing dot on to another template is not rendering it.
		was := st.dot
		walkKeys(n.Pipe, depth, under, st)
		st.dot = was
	case *parse.IfNode:
		walkKeys(n.Pipe, depth, under, st)
		walkKeys(n.List, depth, under, st)
		walkKeys(n.ElseList, depth, under, st)
	case *parse.RangeNode:
		walkBranch(&n.BranchNode, depth, under, st, "[]")
	case *parse.WithNode:
		walkBranch(&n.BranchNode, depth, under, st, "")
	}
}

func walkBranch(n *parse.BranchNode, depth int, under string, st *walkState, suffix string) {
	keys := st.keys
	walkKeys(n.Pipe, depth, under, st)
	inner := ""
	if depth == 0 && len(n.Pipe.Cmds) == 1 && len(n.Pipe.Cmds[0].Args) == 1 {
		if f, ok := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode); ok && len(f.Ident) == 1 {
			inner = f.Ident[0]
			if suffix != "" {
				// Record the list under its [] name, not as a plain key.
				delete(keys, inner)
				inner += suffix
				if keys[inner] == nil {
					keys[inner] = map[string]bool{}
				}
			}
		}
	}
	walkKeys(n.List, depth+1, inner, st)
	walkKeys(n.ElseList, depth, under, st)
}

func formatKeys(keys map[string]map[string]bool) []string {
	var out []string
	for k, subs := range keys {
		s := "`" + k + "`"
		if len(subs) > 0 {
			var ss []string
			for sub := range subs {
				ss = append(ss, sub)
			}
			sort.Strings(ss)
			s += " (" + strings.Join(ss, ", ") + ")"
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// TestPartialDocsReadsWhatTemplatesRead pins the extraction on small
// templates, independently of the generated file: a key read through
// opt counts as much as a field, a range's item keys sit under it, a
// partial that renders dot itself is called with a value rather than a
// dict, and each define takes the comment written just above it.
func TestPartialDocsReadsWhatTemplatesRead(t *testing.T) {
	text := `{{/* box: a box with a title. More words. */}}
{{define "box"}}<b>{{.Title}}</b>{{if opt . "Note"}}{{opt $ "Note"}}{{end}}{{range .Items}}{{.Label}}{{end}}{{end}}

{{/* hint: one line of help; renders nothing when empty. */}}
{{define "hint"}}{{if .}}<p>{{.}}</p>{{end}}{{end}}`
	docs, err := partialDocs("box.html", text)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]partialDoc{}
	for _, d := range docs {
		got[d.name] = d
	}
	if box := got["box"]; box.summary != "A box with a title." || strings.Join(box.keys, ", ") != "`Items[]` (Label), `Note`, `Title`" || box.scalar {
		t.Errorf("box = %+v", box)
	}
	if hint := got["hint"]; hint.summary != "One line of help; renders nothing when empty." || !hint.scalar || len(hint.keys) != 0 {
		t.Errorf("hint = %+v", hint)
	}
}
