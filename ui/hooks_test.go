package ui

import (
	"html/template"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
	"amadan.net/rastrillo/rastrillo/locales"
)

// hookPages builds a Pages over layout, one page per entry of contents.
// Nothing renders: the plan is read off the parse trees.
func hookPages(
	t *testing.T, layout string, contents map[string]string,
) (*Pages, error) {
	t.Helper()
	config, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	app := fstest.MapFS{"layout.html": {Data: []byte(layout)}}
	pages := map[string]PageFile{}
	for name, body := range contents {
		app[name+".html"] = &fstest.MapFile{Data: []byte(body)}
		pages[name] = PageFile{Path: name + ".html"}
	}
	return NewPages(PagesConfig{
		App: app, Layout: "layout.html", Pages: pages,
		Locale: config, LocaleFS: app,
		Funcs: template.FuncMap{
			"asset":      func(name string) string { return "/" + name },
			"iconAssets": func() template.HTML { return "" },
		},
	})
}

// everyFile is an app that has deleted no vendored file.
func everyFile(string) bool { return true }

// planOne plans the one page body over layout.
func planOne(
	t *testing.T, layout, body string, has func(string) bool,
) pagePlan {
	t.Helper()
	pages, err := hookPages(t, layout, map[string]string{"page": body})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planPage("page", pages.pages["page"], pages.entry, has)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// hooksOf is pageHooks for the one page body over scriptsLayout.
func hooksOf(t *testing.T, body string) map[string]bool {
	t.Helper()
	pages, err := hookPages(t, scriptsLayout, map[string]string{"page": body})
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := pageHooks("page", pages.pages["page"], pages.entry)
	if err != nil {
		t.Fatal(err)
	}
	return hooks
}

func requireHooks(t *testing.T, body string, want ...string) {
	t.Helper()
	hooks := hooksOf(t, body)
	for _, hook := range want {
		if !hooks[hook] {
			t.Errorf("%s: hooks %v lack %s", body, hooks, hook)
		}
	}
}

func columnLayout(t *testing.T) string {
	t.Helper()
	layout, ok := Layout("column")
	if !ok {
		t.Fatal("no column layout")
	}
	return string(layout)
}

func static(scripts ...string) []string {
	var paths []string
	for _, script := range scripts {
		paths = append(paths, "static/"+script)
	}
	return paths
}

// requireLoads plans body over layout and requires its scripts to
// include, or to leave out, script.
func requireLoads(
	t *testing.T, layout, body, script string, loads bool,
) {
	t.Helper()
	scripts := planOne(t, layout, body, everyFile).scripts
	if slices.Contains(scripts, "static/"+script) != loads {
		t.Errorf("%s loads %v; want %s loaded: %v",
			body, scripts, script, loads)
	}
}

func TestPageHooksIncludeBranchesNotTaken(t *testing.T) {
	requireHooks(t,
		// A class counts as well as an attribute.
		// markup-spelling: old-spelling begin
		content(`{{if .X}}<p rst-a>{{else}}<p class="rst-b">{{end}}`),
		// markup-spelling: old-spelling end
		"rst-a", "rst-b")
}

func TestPageHooksFollowPartials(t *testing.T) {
	requireHooks(t, content(`{{template "empty-state" .}}`), "rst-empty")
}

func TestPageHooksIncludeStringArguments(t *testing.T) {
	requireHooks(t, content(`{{template "wide" dict "Hook" "rst-wide"}}`)+
		`{{define "wide"}}<div {{.Hook}}></div>{{end}}`, "rst-wide")
}

// select.js copies the classes an option's data-rst-class names onto
// the rows it builds, so those classes are hooks the page shows.
func TestPageHooksIncludeTheClassesSelectCopies(t *testing.T) {
	requireHooks(t, content(`<select data-rst-select>`+
		// The attribute select.js reads is the case here.
		// markup-spelling: old-spelling begin
		`<option data-rst-class="rst-serif">Serif</option></select>`),
		// markup-spelling: old-spelling end
		"rst-serif")
}

func TestPageHooksIncludeNeeds(t *testing.T) {
	requireHooks(t, content(`{{needs "rst-card"}}`), "rst-card")
	for _, name := range []string{"rst", "card"} {
		_, err := hookPages(t, scriptsLayout, map[string]string{
			"orders": content(`{{needs "` + name + `"}}`),
		})
		if err == nil {
			t.Errorf("needs %q: no error", name)
			continue
		}
		for _, want := range []string{
			`"orders"`, `"` + name + `"`, "busy.js", "select.js", "rst-card",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("needs %q: %v; want it to name %s", name, err, want)
			}
		}
	}
}

func TestNeedsRendersNothingForAHookOrAScript(t *testing.T) {
	page := template.Must(template.New("page").Funcs(Funcs()).
		Parse(`[{{needs "rst-card"}}{{needs "busy.js"}}]`))
	var out strings.Builder
	if err := page.Execute(&out, nil); err != nil || out.String() != "[]" {
		t.Errorf("needs rendered %q, %v; want nothing", out.String(), err)
	}
}

func TestBusyLoadsOnlyWithTheAppsOwnForm(t *testing.T) {
	for _, shell := range []string{
		"column", "topbar", "sidebar", "console", "stage",
	} {
		layout, ok := Layout(shell)
		if !ok {
			t.Fatalf("no %s layout", shell)
		}
		requireLoads(t, string(layout), content(`<h1>Orders</h1>`),
			"busy.js", false)
	}
	layout := columnLayout(t)
	requireLoads(t, layout, content(`<form method="post"></form>`),
		"busy.js", true)
	requireLoads(t, layout, content(`{{template "confirm-form" .}}`),
		"busy.js", true)
	requireLoads(t, layout, content(`{{needs "busy.js"}}`), "busy.js", true)
}

func TestLiveLoadsOnLivePages(t *testing.T) {
	region := content(`<h1>Jobs</h1>`) + `{{define "region:items"}}{{end}}`
	polling := content(`<div data-poll="/jobs/1"></div>`)
	every := static(append([]string{"fiesta.js"}, optionalScripts()...)...)
	for _, body := range []string{region, polling} {
		plan := planOne(t, scriptsLayout, body, everyFile)
		if !plan.live || !slices.Equal(plan.scripts, every) {
			t.Errorf("%s: live %v, scripts %v; want live, with %v",
				body, plan.live, plan.scripts, every)
		}
	}
	plain := planOne(t, scriptsLayout, content(`<h1>Orders</h1>`), everyFile)
	if plain.live || !slices.Equal(plain.scripts, static("fiesta.js")) {
		t.Errorf("a plain page: live %v, scripts %v; want fiesta.js only",
			plain.live, plain.scripts)
	}
}

func TestLightLoadsWithTheHooksItLights(t *testing.T) {
	for _, hook := range []string{
		"rst-btn", "rst-box", "rst-card", "rst-stats", "rst-list",
	} {
		requireLoads(t, scriptsLayout, content(`<div `+hook+`></div>`),
			"light.js", true)
	}
	requireLoads(t, scriptsLayout, content(`<p rst-note>Saved</p>`),
		"light.js", false)
}

func TestAnchorLoadsWithAMenu(t *testing.T) {
	requireLoads(t, scriptsLayout, content(`<details rst-dropdown>`),
		"anchor.js", true)
	requireLoads(t, scriptsLayout,
		content(`<details name="rst-menus"></details>`), "anchor.js", true)
	requireLoads(t, scriptsLayout, content(`<h1>Orders</h1>`),
		"anchor.js", false)
	requireLoads(t, columnLayout(t), content(`<h1>Orders</h1>`),
		"anchor.js", true)
}

// calendar.js comes before datetime.js, which calls it, as the
// layouts have always loaded them.
func TestPlannedScriptsFollowTheLayoutsOrder(t *testing.T) {
	body := content(`<form><button rst-btn></button></form>` +
		`{{needs "live.js"}}{{template "field-date" .}}` +
		`{{template "field-select" .}}`)
	got := planOne(t, columnLayout(t), body, everyFile).scripts
	want := static("fiesta.js", "busy.js", "live.js", "light.js",
		"anchor.js", "select.js", "calendar.js", "datetime.js")
	if !slices.Equal(got, want) {
		t.Errorf("scripts %v, want %v", got, want)
	}
}

// Review Focus 2: the docs let an app delete light.js and anchor.js, and
// a page that linked either would answer 404 for it.
func TestADeletedScriptIsNeverLinked(t *testing.T) {
	deleted := func(name string) bool {
		return name != "static/light.js" && name != "static/anchor.js"
	}
	body := content(`<button rst-btn>Save</button><details rst-dropdown>` +
		`{{needs "light.js"}}{{needs "anchor.js"}}`)
	scripts := planOne(t, columnLayout(t), body, deleted).scripts
	for _, script := range static("light.js", "anchor.js") {
		if slices.Contains(scripts, script) {
			t.Errorf("scripts %v link the deleted %s", scripts, script)
		}
	}
}

func TestAPagesHooksIncludeWhatItsScriptsCreate(t *testing.T) {
	selectPlan := planOne(t, scriptsLayout,
		content(`{{template "field-select" .}}`), everyFile)
	if !slices.Contains(selectPlan.hooks, "rst-combo") {
		t.Errorf("a select page's hooks %v lack rst-combo", selectPlan.hooks)
	}
	plain := planOne(t, scriptsLayout, content(`<h1>Orders</h1>`), everyFile)
	if slices.Contains(plain.hooks, "rst-combo") {
		t.Errorf("a plain page's hooks %v hold rst-combo", plain.hooks)
	}
}

// Spec 3a: a hook a framework function writes is in no template, so the
// page's hooks take it from the function's listed hooks.
func TestAStagePageHasTheBackdropsHooks(t *testing.T) {
	layout, ok := Layout("stage")
	if !ok {
		t.Fatal("no stage layout")
	}
	plan := planOne(t, string(layout), content(`<h1>Sign in</h1>`), everyFile)
	for _, hook := range []string{
		"rst-stage-art", "rst-stage-art-glow", "rst-stage-art-lines",
	} {
		if !slices.Contains(plan.hooks, hook) {
			t.Errorf("a stage page's hooks %v lack %s", plan.hooks, hook)
		}
	}
}

func TestEachFunctionWritesExactlyItsListedHooks(t *testing.T) {
	outputs := map[string][]string{
		"stageArt":  {string(stageArt("rastrillo")), string(stageArt("x"))},
		"menuGroup": {menuGroup(nil)},
	}
	names := slices.Sorted(maps.Keys(outputs))
	if listed := vendored.WritingFunctions(); !slices.Equal(listed, names) {
		t.Errorf("vendored lists %v, the test reads %v", listed, names)
	}
	for name, texts := range outputs {
		for _, text := range texts {
			got := vendored.HooksIn([]byte(text))
			if want := vendored.WrittenHooks(name); !slices.Equal(got, want) {
				t.Errorf("%s writes %v, but vendored.WrittenHooks lists %v",
					name, got, want)
			}
		}
	}
}
