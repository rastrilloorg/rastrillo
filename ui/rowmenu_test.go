package ui

import (
	"html/template"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

func rowMenuData(items ...any) map[string]any {
	return map[string]any{"Name": "Grace Hopper", "Items": items}
}

func TestRowMenuRendersEveryKindOfItem(t *testing.T) {
	got := render(t, "row-menu", rowMenuData(
		map[string]any{"Label": "Edit", "Href": "/orders/AB3PX/edit"},
		map[string]any{"Label": "Archive", "Action": "/orders/AB3PX/archive", "Hidden": [][2]string{{"state", "archived"}, {"from", "list"}}},
		map[string]any{"Label": "Delete order…", "Href": "/orders/AB3PX/delete", "Danger": true},
	))
	for _, want := range []string{
		`<details rst-row-menu name="rst-menus">`,
		`aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Grace Hopper")) + `"`,
		string(rastrillo.Icon("kebab")),
		`<a href="/orders/AB3PX/edit">Edit</a>`,
		// One-button form, Hidden pairs in caller order, no token field:
		// csrf.Protect is an origin check.
		`<form method="post" action="/orders/AB3PX/archive"><input type="hidden" name="state" value="archived"><input type="hidden" name="from" value="list"><button type="submit">Archive</button></form>`,
		`<a class="rst-danger" href="/orders/AB3PX/delete">Delete order…</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if n := strings.Count(got, "<hr>"); n != 1 {
		t.Errorf("%d rules, want 1: %s", n, got)
	}
	if hr, archive, del := strings.Index(got, "<hr>"), strings.Index(got, "Archive</button>"), strings.Index(got, "Delete order…"); !(archive < hr && hr < del) {
		t.Errorf("the rule is not between the last plain item and the first destructive one: %s", got)
	}
}

// The <hr> goes before the first danger item that follows a plain one,
// and nowhere else.
func TestRowMenuDrawsOneRuleBeforeTheFirstDangerAfterAPlainItem(t *testing.T) {
	plain := map[string]any{"Label": "Edit", "Href": "/e"}
	danger := func(n string) map[string]any {
		return map[string]any{"Label": "Delete " + n + "…", "Href": "/d" + n, "Danger": true}
	}
	for _, c := range []struct {
		name  string
		items []any
		rules int
	}{
		{"danger only", []any{danger("a")}, 0},
		{"plain only", []any{plain}, 0},
		{"plain then two dangers", []any{plain, danger("a"), danger("b")}, 1},
		{"danger, plain, danger", []any{danger("a"), plain, danger("b")}, 1},
	} {
		got := render(t, "row-menu", rowMenuData(c.items...))
		if n := strings.Count(got, "<hr>"); n != c.rules {
			t.Errorf("%s: %d rules, want %d: %s", c.name, n, c.rules, got)
		}
	}
}

// Invalid items fail at Execute, naming the item, the way dict fails on
// an odd argument count: silently dropping an item, or rendering a
// destructive POST, are the two failures this rules out.
func TestRowMenuRefusesItemsItCannotRender(t *testing.T) {
	tmpl := parseAll(t)
	for _, c := range []struct {
		name string
		data any
		want string
	}{
		{"no Items", map[string]any{"Name": "Grace Hopper"}, "Items"},
		{"empty Items", rowMenuData(), "Items"},
		{"no Label", rowMenuData(map[string]any{"Href": "/e"}), "item 0 has no Label"},
		{"Href and Action", rowMenuData(map[string]any{"Label": "Edit", "Href": "/e", "Action": "/e"}), `item 0 ("Edit") wants exactly one of Href`},
		{"neither", rowMenuData(map[string]any{"Label": "Edit"}), `item 0 ("Edit") wants exactly one of Href`},
		{"Danger with Action", rowMenuData(map[string]any{"Label": "Delete…", "Action": "/d", "Danger": true}), `item 0 ("Delete…") is Danger`},
		{"Hidden without Action", rowMenuData(map[string]any{"Label": "Edit", "Href": "/e", "Hidden": [][2]string{{"a", "b"}}}), `item 0 ("Edit") carries Hidden`},
	} {
		err := tmpl.ExecuteTemplate(&strings.Builder{}, "row-menu", c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", c.name, err, c.want)
		}
	}
}

// An empty or nil Hidden is not "carrying Hidden": a struct caller has the
// field on every item, link items included.
func TestRowMenuAcceptsAnEmptyHiddenOnALink(t *testing.T) {
	for _, hidden := range []any{nil, [][2]string(nil), [][2]string{}, []any{}} {
		got := render(t, "row-menu", rowMenuData(map[string]any{"Label": "Edit", "Href": "/e", "Hidden": hidden}))
		if !strings.Contains(got, `<a href="/e">Edit</a>`) {
			t.Errorf("Hidden %#v on a link item: %s", hidden, got)
		}
	}
}

// A menu of links alone is the only kind the contract allows inside a
// bulk-selection form: a POST item's own <form> would be dropped by the
// parser there, and its button would submit the whole selection to the
// wrong action. This holds the partial to emitting no form and no button
// for such a menu, so a links-only menu inside that form stays safe.
func TestALinksOnlyRowMenuEmitsNoFormOrButton(t *testing.T) {
	got := render(t, "row-menu", rowMenuData(
		map[string]any{"Label": "Edit", "Href": "/orders/CD4QY/edit"},
		map[string]any{"Label": "Delete order…", "Href": "/orders/CD4QY/delete", "Danger": true},
	))
	if strings.Contains(got, "<form") || strings.Contains(got, "<button") {
		t.Errorf("a links-only row menu emitted a form or a button, which inside a selection form would submit the selection: %s", got)
	}
	if !strings.Contains(got, `<a href="/orders/CD4QY/edit">Edit</a>`) || !strings.Contains(got, `<a class="rst-danger" href="/orders/CD4QY/delete">`) {
		t.Errorf("a links-only row menu lost an item: %s", got)
	}
}

func TestRowMenuDefaultsToTheSharedGroupAndTakesAnother(t *testing.T) {
	item := map[string]any{"Label": "Edit", "Href": "/e"}
	if got := render(t, "row-menu", rowMenuData(item)); !strings.Contains(got, `name="`+MenuGroupDefault+`"`) {
		t.Errorf("row-menu is outside the shared group by default: %s", got)
	}
	d := rowMenuData(item)
	d["MenuGroup"] = "row-menus-x"
	if got := render(t, "row-menu", d); !strings.Contains(got, `name="row-menus-x"`) || strings.Contains(got, `name="rst-menus"`) {
		t.Errorf("MenuGroup did not replace the default: %s", got)
	}
}

// A row name that looks like markup or a placeholder is escaped once and
// printed, never re-substituted.
func TestRowMenuEscapesTheNameItShowsBack(t *testing.T) {
	name := `<b>"Ada" & Co</b> {name}`
	got := render(t, "row-menu", map[string]any{"Name": name, "Items": []any{map[string]any{"Label": "Edit", "Href": "/e"}}})
	if strings.Contains(got, "<b>") {
		t.Errorf("the name reached the page as markup: %s", got)
	}
	want := `aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", name)) + `"`
	if !strings.Contains(got, want) {
		t.Errorf("missing %s in %s", want, got)
	}
	if strings.Count(got, "{name}") != 1 {
		t.Errorf("the name's own {name} was substituted into, or lost: %s", got)
	}
	row := render(t, "list-row-action", map[string]any{"Href": "/p", "Main": name, "Menu": []any{map[string]any{"Label": "Edit", "Href": "/e"}}})
	if strings.Contains(row, "<b>") || !strings.Contains(row, want) {
		t.Errorf("list-row-action's Menu names its trigger unsafely: %s", row)
	}
}

func TestRowMenuWorksForAStructCaller(t *testing.T) {
	type item struct {
		Label, Href, Action string
		Hidden              [][2]string
		Danger              bool
	}
	type menu struct {
		Name  string
		Items []item
	}
	got := render(t, "row-menu", menu{Name: "Grace Hopper", Items: []item{
		{Label: "Archive", Action: "/a", Hidden: [][2]string{{"x", "1"}}},
		{Label: "Delete…", Href: "/d", Danger: true},
	}})
	for _, want := range []string{`action="/a"`, `name="x" value="1"`, `<a class="rst-danger" href="/d">`, "<hr>"} {
		if !strings.Contains(got, want) {
			t.Errorf("struct caller: missing %q in %s", want, got)
		}
	}
}

// listRowActionGolden is list-row-action's output for its full fixture,
// captured before Menu existed. Without Menu the row must be these
// bytes exactly: every app's list screens render through it.
const listRowActionGolden = "<div rst-row>\n  <span rst-row-lead data-lead=\"positive\" aria-hidden=\"true\">RN</span>\n  <span rst-row-main><a href=\"/posts/1\">Release notes, August</a><small rst-row-sub>Published 2 August · 4 min read</small></span>\n  <span rst-status rst-tone=\"positive\">Published</span>\n  <a rst-row-action href=\"/posts/1/edit\" aria-label=\"Edit Release notes, August\">Edit</a>\n</div>"

func fullRowData() map[string]any {
	return map[string]any{
		"Href": "/posts/1", "Main": "Release notes, August",
		"Sub":        "Published 2 August · 4 min read",
		"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
		"ActionAria": "Edit Release notes, August",
		"StatusTone": "positive", "StatusLabel": "Published",
		"Lead": "positive", "LeadInitial": "RN",
	}
}

func TestListRowActionWithoutMenuIsUnchanged(t *testing.T) {
	if got := render(t, "list-row-action", fullRowData()); got != listRowActionGolden {
		t.Errorf("list-row-action without Menu changed:\n got %q\nwant %q", got, listRowActionGolden)
	}
}

// Pill for the one frequent action, kebab for the rest, in that order
// in the DOM and on screen, the kebab named for Main.
func TestListRowActionWithMenuRendersThePillThenTheKebab(t *testing.T) {
	d := fullRowData()
	d["Menu"] = []any{map[string]any{"Label": "Archive", "Action": "/posts/1/archive"}}
	got := render(t, "list-row-action", d)
	status, pill, kebab := strings.Index(got, "rst-status"), strings.Index(got, "rst-row-action"), strings.Index(got, "<details rst-row-menu")
	if !(status >= 0 && status < pill && pill < kebab) {
		t.Errorf("order is status %d, pill %d, kebab %d; want status, pill, kebab: %s", status, pill, kebab, got)
	}
	if want := `aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Release notes, August")) + `"`; !strings.Contains(got, want) {
		t.Errorf("the kebab is not named for Main (%s): %s", want, got)
	}
}

// A struct caller written before Menu existed still executes: the key is
// read through opt, the menuGroup precedent.
func TestListRowActionStructCallerWithoutMenuStillExecutes(t *testing.T) {
	type row struct{ Href, Main, Sub, ActionHref, ActionLabel, ActionAria, StatusTone, StatusLabel, Lead, LeadInitial string }
	got := render(t, "list-row-action", row{Href: "/p", Main: "M"})
	if strings.Contains(got, "rst-row-menu") {
		t.Errorf("a struct with no Menu field rendered a menu: %s", got)
	}
}

var betweenTags = regexp.MustCompile(`>\s+<`)

// normaliseMarkup drops the whitespace between tags, which is template
// layout and not content.
func normaliseMarkup(s string) string {
	return strings.TrimSpace(betweenTags.ReplaceAllString(s, "><"))
}

// The styleguide sample is raw markup (samples are handed to the
// gallery unexecuted), so it cannot call the partial; this holds its
// kebab equal to what the partial renders for the same data, so the two
// cannot drift.
func TestTheListGridSampleCarriesTheRowMenuPartialsOutput(t *testing.T) {
	sample := Styleguide()["list-grid"]
	start := strings.Index(sample, "<details rst-row-menu")
	if start < 0 {
		t.Fatalf("the list-grid sample has no row menu: %s", sample)
	}
	end := strings.Index(sample[start:], "</details>")
	if end < 0 {
		t.Fatalf("the list-grid sample's row menu is never closed: %s", sample)
	}
	got := normaliseMarkup(sample[start : start+end+len("</details>")])
	want := normaliseMarkup(render(t, "row-menu", rowMenuData(
		map[string]any{"Label": "View", "Href": "/orders/AB3PX"},
		map[string]any{"Label": "Refund order…", "Href": "/orders/AB3PX/refund", "Danger": true},
	)))
	if got != want {
		t.Errorf("the list-grid sample's kebab is not the partial's output.\nsample:  %s\npartial: %s", got, want)
	}
}
