package ui

import (
	"fmt"
	"html/template"
	"strings"
	"testing"
)

// options builds n option dicts, labelled so a filter test can pick one.
func options(n int) []any {
	out := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, map[string]any{
			"Value": fmt.Sprint(i),
			"Label": fmt.Sprintf("Option %d", i),
		})
	}
	return out
}

func selectData(n int) map[string]any {
	return map[string]any{
		"ID": "author", "Name": "author", "Label": "Author",
		"Options": options(n),
	}
}

// The whole convention in one assertion: whatever the option count, the
// thing in the form is a real <select> carrying every option. This is
// the path that must never break, and the one a no-JS user gets.
func TestFieldSelectIsAlwaysARealNativeSelect(t *testing.T) {
	for _, n := range []int{1, 9, 10, 40} {
		got := render(t, "field-select", selectData(n))
		if !strings.Contains(got, `<select`) || !strings.Contains(got, `name="author"`) {
			t.Errorf("n=%d: no native select: %s", n, got)
		}
		if strings.Count(got, "<option") != n {
			t.Errorf("n=%d: rendered %d options, want %d", n, strings.Count(got, "<option"), n)
		}
	}
}

// The judgement lives in the partial: below the threshold nothing is
// emitted, so the script has nothing to find and a handful of options
// stays a plain select.
func TestFieldSelectEnhancesOnlyPastTheThreshold(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want bool
	}{{1, false}, {9, false}, {10, true}, {40, true}} {
		got := render(t, "field-select", selectData(tc.n))
		if has := strings.Contains(got, "data-rst-select"); has != tc.want {
			t.Errorf("n=%d: data-rst-select present=%v, want %v", tc.n, has, tc.want)
		}
	}
}

// Plain is the escape hatch, at any size.
func TestFieldSelectPlainOptsOut(t *testing.T) {
	d := selectData(40)
	d["Plain"] = true
	got := render(t, "field-select", d)
	if strings.Contains(got, "data-rst-select") {
		t.Errorf("Plain did not opt out: %s", got)
	}
	if strings.Count(got, "<option") != 40 {
		t.Error("Plain must not change what the select contains")
	}
}

// An absent Options key must not blow up at Execute — len of an untyped
// nil is a template error, so the guard order in the partial matters.
func TestFieldSelectWithNoOptions(t *testing.T) {
	got := render(t, "field-select", map[string]any{
		"ID": "a", "Name": "a", "Label": "A",
	})
	if !strings.Contains(got, "</select>") {
		t.Errorf("no select rendered: %s", got)
	}
	if strings.Contains(got, "<option") {
		t.Errorf("no Options supplied, so none should render: %s", got)
	}
	if strings.Contains(got, "data-rst-select") {
		t.Errorf("an empty select must not be enhanced: %s", got)
	}
}

// The strings the browser will speak are catalog-resolved, not
// hardcoded English in the script.
func TestFieldSelectCarriesLocalisedStringsForTheScript(t *testing.T) {
	got := render(t, "field-select", selectData(10))
	for _, want := range []string{
		`data-rst-select-filter="Type to filter"`,
		`data-rst-select-results="{n} results"`,
		`data-rst-select-result-one="1 result"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// Rebinding T must reach these too, or an app's locale stops at the
// edge of the enhancement.
func TestFieldSelectStringsFollowTheCatalogRebind(t *testing.T) {
	tmpl, err := template.New("").
		Funcs(FuncsWith(func(key string, _ ...any) string { return "xx:" + key })).
		ParseFS(Templates(), "*.html")
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "field-select", selectData(10)); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	if !strings.Contains(buf.String(), `data-rst-select-filter="xx:rastrillo.ui.select_filter"`) {
		t.Errorf("the enhancement's strings ignore the catalog rebind: %s", buf.String())
	}
}

// The envelope is unchanged by the enhancement: an enhanced select still
// wires its error and help exactly as a plain one does.
func TestFieldSelectEnvelopeSurvivesEnhancement(t *testing.T) {
	d := selectData(10)
	d["Error"] = "Pick an author"
	got := render(t, "field-select", d)
	for _, want := range []string{
		`aria-invalid="true"`, `aria-describedby="author-error"`,
		`id="author-error"`, `role="alert"`, "Pick an author",
		"data-rst-select",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}
