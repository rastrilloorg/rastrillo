package ui

import (
	"html/template"
	"strings"
	"testing"
)

func mediaGridData(items ...any) map[string]any {
	return map[string]any{"Items": items}
}

// mediaTile is the full fixture's first tile: every key set.
func mediaTile() map[string]any {
	return map[string]any{
		"Src": "/images/4471.png", "Name": "Checkout, step 2",
		"Alt":  "The payment step of the checkout",
		"Desc": "The payment step, with the card form open.",
		"Meta": "1280 × 3200 · 412 KB",
		"Href": "/images/4471", "Width": 1280, "Height": 3200,
		"Fit":    "contain",
		"Select": "4471", "Selected": true,
		"Menu": []any{
			map[string]any{"Label": "Edit", "Href": "/images/4471/edit"},
			map[string]any{"Label": "Delete image…", "Href": "/images/4471/delete", "Danger": true},
		},
	}
}

// The fewest keys: a source and a name. Everything optional is absent
// from the page rather than rendered empty — a missing map key must
// never reach the page as "<no value>" — and the description line is
// the catalog's "No description", so the tile keeps its height.
func TestMediaGridMinimalFixture(t *testing.T) {
	got := render(t, "media-grid", mediaGridData(map[string]any{"Src": "/images/4471.png", "Name": "Checkout, step 2"}))
	for _, want := range []string{
		`<div rst-media>`, `<div rst-media-tile>`, `<div rst-media-frame>`,
		`<img src="/images/4471.png" alt="" loading="lazy" decoding="async">`,
		`<div rst-media-name><span>Checkout, step 2</span></div>`,
		`<p rst-media-desc="none">` + defaultTf("rastrillo.ui.no_description") + `</p>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, bad := range []string{`width=`, `height=`, `<a `, `rst-selbox`, `rst-row-menu`, `rst-media-meta`, `="contain"`, `<no value>`} {
		if strings.Contains(got, bad) {
			t.Errorf("optional part %q rendered without data:\n%s", bad, got)
		}
	}
}

func TestMediaGridFullFixture(t *testing.T) {
	got := render(t, "media-grid", mediaGridData(mediaTile()))
	for _, want := range []string{
		`<div rst-media-frame="contain">`,
		`<img src="/images/4471.png" alt="The payment step of the checkout" loading="lazy" decoding="async" width="1280" height="3200">`,
		`<label rst-selbox><input type="checkbox" name="id" value="4471" checked aria-label="` +
			template.HTMLEscapeString(defaultTf("rastrillo.ui.media_select", "name", "Checkout, step 2")) + `"></label>`,
		`<div rst-media-name><a href="/images/4471">Checkout, step 2</a><details rst-row-menu name="rst-menus">`,
		`<summary aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Checkout, step 2")) + `">`,
		`<a href="/images/4471/edit">Edit</a>`,
		`<a class="rst-danger" href="/images/4471/delete">Delete image…</a>`,
		`<p rst-media-desc>The payment step, with the card form open.</p>`,
		`<p rst-media-meta>1280 × 3200 · 412 KB</p>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, defaultTf("rastrillo.ui.no_description")) {
		t.Errorf("a described tile still says there is no description:\n%s", got)
	}
}

// Every attribute the partial can emit has a selector in tokens.css, in
// the attribute spelling the partial writes. The same drift check the
// styleguide samples get; no sample carries a media grid.
func TestMediaGridAttributesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	seen := rstVocabulary(render(t, "media-grid", mediaGridData(mediaTile(), map[string]any{"Src": "/images/1.png", "Name": "One"})))
	for _, name := range []string{"rst-media", "rst-media-tile", "rst-media-frame", "rst-media-frame~=contain", "rst-media-caption", "rst-media-name", "rst-media-desc", "rst-media-desc~=none", "rst-media-meta"} {
		if !seen[name] {
			t.Errorf("the fixtures never render %q", name)
		}
	}
	for name := range seen {
		if !qualifiedOnly[name] && !tokensStyle(css, name) {
			t.Errorf("tokens.css has no selector for %q", name)
		}
	}
}

// "No description" is the catalog's line for a tile with none, and only
// for one with none: an app's own description is shown as written.
func TestMediaGridSaysNoDescriptionOnlyWhenThereIsNone(t *testing.T) {
	none := defaultTf("rastrillo.ui.no_description")
	for _, c := range []struct {
		name string
		desc any
		want bool
	}{
		{"absent", nil, true},
		{"empty", "", true},
		{"given", "The payment step.", false},
	} {
		item := map[string]any{"Src": "/images/1.png", "Name": "One"}
		if c.desc != nil {
			item["Desc"] = c.desc
		}
		got := render(t, "media-grid", mediaGridData(item))
		if has := strings.Contains(got, none); has != c.want {
			t.Errorf("%s: says %q: %v, want %v:\n%s", c.name, none, has, c.want, got)
		}
		if marked := strings.Contains(got, `rst-media-desc="none"`); marked != c.want {
			t.Errorf("%s: the description line is marked none: %v, want %v", c.name, marked, c.want)
		}
	}
}

// Fit is the caller's word: "contain" letterboxes, and anything else,
// including nothing, is the default cover. The partial never guesses
// from the image's shape.
func TestMediaGridFitContainVersusTheDefault(t *testing.T) {
	for _, c := range []struct {
		fit     any
		contain bool
	}{
		{nil, false}, {"", false}, {"cover", false}, {"contain", true},
	} {
		item := map[string]any{"Src": "/images/1.png", "Name": "One"}
		if c.fit != nil {
			item["Fit"] = c.fit
		}
		got := render(t, "media-grid", mediaGridData(item))
		if has := strings.Contains(got, `<div rst-media-frame="contain">`); has != c.contain {
			t.Errorf("Fit %v: contain %v, want %v:\n%s", c.fit, has, c.contain, got)
		}
		if !c.contain && !strings.Contains(got, `<div rst-media-frame>`) {
			t.Errorf("Fit %v: the frame lost its default spelling:\n%s", c.fit, got)
		}
	}
}

// The size attributes are what stops the layout shifting as images
// load, and they are written only as a pair: a width alone would give
// the image a wrong ratio, which is worse than none.
func TestMediaGridWritesTheImageSizeOnlyWhenBothAreGiven(t *testing.T) {
	for _, c := range []struct {
		name          string
		width, height any
		want          string
	}{
		{"both", 1280, 3200, ` width="1280" height="3200"`},
		{"width only", 1280, nil, ""},
		{"height only", nil, 3200, ""},
		{"zero", 0, 0, ""},
	} {
		item := map[string]any{"Src": "/images/1.png", "Name": "One"}
		if c.width != nil {
			item["Width"] = c.width
		}
		if c.height != nil {
			item["Height"] = c.height
		}
		got := render(t, "media-grid", mediaGridData(item))
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: missing %q:\n%s", c.name, c.want, got)
		}
		if c.want == "" && (strings.Contains(got, "width=") || strings.Contains(got, "height=")) {
			t.Errorf("%s: a size attribute rendered without its pair:\n%s", c.name, got)
		}
	}
}

// Href makes the tile a link through its one name link, which tokens.css
// stretches across the tile; the image is never wrapped in an anchor of
// its own, so there is one tab stop per tile and never a nested anchor.
func TestMediaGridLinksTheTileThroughItsName(t *testing.T) {
	linked := render(t, "media-grid", mediaGridData(map[string]any{"Src": "/images/1.png", "Name": "One", "Href": "/images/1"}))
	if n := strings.Count(linked, "<a "); n != 1 {
		t.Errorf("%d anchors, want exactly one, the name:\n%s", n, linked)
	}
	if !strings.Contains(linked, `<div rst-media-name><a href="/images/1">One</a></div>`) {
		t.Errorf("the name is not the link:\n%s", linked)
	}
	if img, a := strings.Index(linked, "<img"), strings.Index(linked, "<a "); img == -1 || a < img {
		t.Errorf("the image sits inside the anchor:\n%s", linked)
	}
	plain := render(t, "media-grid", mediaGridData(map[string]any{"Src": "/images/1.png", "Name": "One"}))
	if strings.Contains(plain, "<a ") || !strings.Contains(plain, "<span>One</span>") {
		t.Errorf("a tile with no Href still links somewhere, or lost its name:\n%s", plain)
	}
}

// The checkbox is the row checkbox of select mode (selbox), carried in
// the frame: a value per tile under one name the bulk-bar's form
// collects, "id" unless the caller says otherwise, checked when the
// server says so, and absent entirely when the tile has no Select.
func TestMediaGridSelectFollowsTheBulkBarsForm(t *testing.T) {
	item := func(extra map[string]any) map[string]any {
		m := map[string]any{"Src": "/images/1.png", "Name": "One", "Select": "4471"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	got := render(t, "media-grid", mediaGridData(item(nil)))
	if !strings.Contains(got, `<label rst-selbox><input type="checkbox" name="id" value="4471" aria-label=`) {
		t.Errorf("the checkbox is not a selbox named id with the tile's value:\n%s", got)
	}
	if strings.Contains(got, " checked") {
		t.Errorf("an unselected tile rendered checked:\n%s", got)
	}
	if got := render(t, "media-grid", mediaGridData(item(map[string]any{"Selected": true}))); !strings.Contains(got, `value="4471" checked `) {
		t.Errorf("Selected did not check the box:\n%s", got)
	}
	named := map[string]any{"Items": []any{item(nil)}, "SelectName": "image"}
	if got := render(t, "media-grid", named); !strings.Contains(got, `name="image" value="4471"`) || strings.Contains(got, `name="id"`) {
		t.Errorf("SelectName did not rename the checkbox:\n%s", got)
	}
	// Selected without Select is a server bug, not a checkbox: there is
	// no value to post, so nothing renders and the outline never shows.
	none := render(t, "media-grid", mediaGridData(map[string]any{"Src": "/images/1.png", "Name": "One", "Selected": true}))
	if strings.Contains(none, "checkbox") {
		t.Errorf("a tile with no Select rendered a checkbox:\n%s", none)
	}
}

// Both controls say which tile they belong to, through the catalog's
// patterns, so a page of twenty checkboxes is not twenty "checkbox"es
// to a screen reader; and a name that looks like markup or a
// placeholder is escaped once and printed, never re-substituted.
func TestMediaGridNamesTheCheckboxAndKebabForTheItem(t *testing.T) {
	name := `<b>"Ada" & Co</b> {name}`
	got := render(t, "media-grid", mediaGridData(map[string]any{
		"Src": "/images/1.png", "Name": name, "Select": "1",
		"Menu": []any{map[string]any{"Label": "Edit", "Href": "/images/1/edit"}},
	}))
	if strings.Contains(got, "<b>") {
		t.Errorf("the name reached the page as markup:\n%s", got)
	}
	for _, key := range []string{"rastrillo.ui.media_select", "rastrillo.ui.row_menu"} {
		want := `aria-label="` + template.HTMLEscapeString(defaultTf(key, "name", name)) + `"`
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	// Once in the visible name, once in each of the two labels.
	if n := strings.Count(got, "{name}"); n != 3 {
		t.Errorf("the name's own {name} appears %d times, want 3 (it was substituted into, or lost):\n%s", n, got)
	}
}

// One MenuGroup on the grid reaches every kebab, so a grid inside a
// menu of its own does not close that menu by opening a kebab; the
// default is the shared group every other menu is in.
func TestMediaGridPassesTheMenuGroupToEveryKebab(t *testing.T) {
	menu := []any{map[string]any{"Label": "Edit", "Href": "/e"}}
	items := []any{
		map[string]any{"Src": "/images/1.png", "Name": "One", "Menu": menu},
		map[string]any{"Src": "/images/2.png", "Name": "Two", "Menu": menu},
	}
	if got := render(t, "media-grid", map[string]any{"Items": items}); strings.Count(got, `name="`+MenuGroupDefault+`"`) != 2 {
		t.Errorf("the kebabs are not both in the shared group:\n%s", got)
	}
	if got := render(t, "media-grid", map[string]any{"Items": items, "MenuGroup": "rst-images"}); strings.Count(got, `name="rst-images"`) != 2 || strings.Contains(got, `name="rst-menus"`) {
		t.Errorf("MenuGroup did not reach both kebabs:\n%s", got)
	}
}

// An empty list is an empty grid, not an error: the caller decides
// whether to show empty-state instead, as every list does.
func TestMediaGridRendersAnEmptyListAsAnEmptyGrid(t *testing.T) {
	got := render(t, "media-grid", mediaGridData())
	if !strings.Contains(got, `<div rst-media>`) || strings.Contains(got, "rst-media-tile") {
		t.Errorf("an empty Items did not render an empty grid:\n%s", got)
	}
}

func TestMediaGridWorksForAStructCaller(t *testing.T) {
	type item struct {
		Label, Href string
		Danger      bool
	}
	type tile struct {
		Src, Name, Alt, Desc, Meta, Href, Fit, Select string
		Width, Height                                 int
		Selected                                      bool
		Menu                                          []item
	}
	type grid struct {
		Items      []tile
		SelectName string
	}
	got := render(t, "media-grid", grid{SelectName: "image", Items: []tile{
		{Src: "/images/1.png", Name: "One", Desc: "First.", Meta: "1 KB", Href: "/images/1", Fit: "contain", Width: 40, Height: 30, Select: "1", Selected: true, Menu: []item{{Label: "Edit", Href: "/images/1/edit"}}},
		{Src: "/images/2.png", Name: "Two"},
	}})
	for _, want := range []string{
		`rst-media-frame="contain"`, ` width="40" height="30"`, `name="image" value="1" checked`,
		`<a href="/images/1">One</a>`, `<p rst-media-desc>First.</p>`, `<p rst-media-meta>1 KB</p>`, `<a href="/images/1/edit">Edit</a>`,
		`<span>Two</span>`, `rst-media-desc="none"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("struct caller: missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "<no value>") {
		t.Errorf("struct caller: an empty field reached the page as <no value>:\n%s", got)
	}
}
