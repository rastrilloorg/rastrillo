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
