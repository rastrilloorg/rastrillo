package tmpl_test

import (
	"slices"
	"testing"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// Inspect is how ui reads what a page's templates use before the page
// runs: every node of the entry and of each template it calls, once,
// and nothing of a template it does not reach.
func TestInspectVisitsWhatATemplateReaches(t *testing.T) {
	set := tmpl.Must(tmpl.New("page").Parse(
		`{{template "a" .}}{{if .}}{{template "b" .}}{{end}}` +
			`{{template "a" .}}{{template "missing" .}}` +
			`{{define "a"}}in a{{template "b" .}}{{end}}` +
			`{{define "b"}}in b{{end}}` +
			`{{define "unreached"}}in unreached{{end}}`))
	var texts []string
	set.Inspect(func(node parse.Node) {
		if text, ok := node.(*parse.TextNode); ok {
			texts = append(texts, string(text.Text))
		}
	})
	slices.Sort(texts)
	if want := []string{"in a", "in b"}; !slices.Equal(texts, want) {
		t.Errorf("visited text %q, want %q", texts, want)
	}
}

// InspectNamed also says which template a node is in, so a caller can
// tell the layout's own pieces from a page's: the same kind of node
// means something different in "menu" than in "form".
func TestInspectNamedSaysWhichTemplateANodeIsIn(t *testing.T) {
	set := tmpl.Must(tmpl.New("page").Parse(
		`on the page{{template "menu" .}}{{template "form" .}}` +
			`{{define "menu"}}in menu{{end}}` +
			`{{define "form"}}in form{{end}}` +
			`{{define "unreached"}}in unreached{{end}}`))
	var found []string
	set.InspectNamed(func(name string, node parse.Node) {
		if text, ok := node.(*parse.TextNode); ok {
			found = append(found, name+": "+string(text.Text))
		}
	})
	slices.Sort(found)
	want := []string{"form: in form", "menu: in menu", "page: on the page"}
	if !slices.Equal(found, want) {
		t.Errorf("visited %q, want %q", found, want)
	}
}
