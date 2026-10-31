package routelist

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

// parsed is one file of source as Find and Unmounted read it.
func parsed(t *testing.T, source string) []sourceFile {
	t.Helper()
	fileSet := token.NewFileSet()
	syntax, err := parser.ParseFile(fileSet, "app.go",
		"package app\n"+source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return []sourceFile{{
		syntax: syntax, fileSet: fileSet, actionNames: actionNames(syntax),
	}}
}

const (
	importAction = `import "amadan.net/rastrillo/rastrillo/action"` + "\n"
	appStruct    = "type App struct{}\n"
)

// shapes are Routes functions and the target each gives, or misfit
// when it has a shape the listing program cannot call.
var shapes = map[string]foundRoutes{
	importAction + appStruct +
		"func Routes(routes *action.Routes, app *App) {}": {
		target: Target{AppType: "App", Pointer: true}},
	`import web "amadan.net/rastrillo/rastrillo/action"` + "\n" +
		appStruct + "func Routes(routes *(web.Routes), app App) {}": {
		target: Target{AppType: "App"}},
	`import . "amadan.net/rastrillo/rastrillo/action"` + "\n" +
		appStruct + "func Routes(_ *Routes, _ *App) {}": {
		target: Target{AppType: "App", Pointer: true}},
	`import "example.com/action"` + "\n" + appStruct +
		"func Routes(routes *action.Routes, app *App) {}": {
		misfit: "func Routes(routes *action.Routes, app *App)"},
	importAction + "type app struct{}\n" +
		"func Routes(routes *action.Routes, a *app) {}": {
		misfit: "func Routes(routes *action.Routes, a *app)"},
	importAction + `import "net/http"` + "\n" +
		"func Routes(routes *action.Routes, server *http.Server) {}": {
		misfit: "func Routes(routes *action.Routes, server *http.Server)"},
	importAction + appStruct +
		"func Routes[T any](routes *action.Routes, app *App) {}": {
		misfit: "func Routes[T any](routes *action.Routes, app *App)"},
	importAction + appStruct +
		"func Routes(routes *action.Routes, app *App) error {}": {
		misfit: "func Routes(routes *action.Routes, app *App) error"},
	importAction + appStruct +
		"func Routes(routes *action.Routes, app *App, more int) {}": {
		misfit: "func Routes(routes *action.Routes, app *App, more int)"},
	importAction + appStruct +
		"func Routes(routes action.Routes, app *App) {}": {
		misfit: "func Routes(routes action.Routes, app *App)"},
}

func TestFindChecksTheWholeShapeOfRoutes(t *testing.T) {
	for source, want := range shapes {
		found := findRoutes(parsed(t, source))
		if len(found) != 1 || found[0] != want {
			t.Errorf("from\n%s\nfound %+v, want %+v", source, found, want)
		}
	}
	method := importAction + appStruct +
		"func (App) Routes(routes *action.Routes, app *App) {}"
	if found := findRoutes(parsed(t, method)); len(found) != 0 {
		t.Errorf("a method called Routes was found: %+v", found)
	}
}

func TestActionsAreNamedAsTheRuntimeNamesThem(t *testing.T) {
	source := importAction + `
func show(visit *action.Visit)                  {}
func (*App) edit(visit *(action.Visit))         {}
func (report Report) Show(visit *action.Visit)  {}
func (list *List[T]) Show(visit *action.Visit)  {}
func (pair Pair[K, V]) Show(visit *action.Visit) {}
func notAction(visit *action.Visit) error      { return nil }
func alsoNot(visit action.Visit)                {}
func twoVisits(first, second *action.Visit)     {}
`
	listed := listedPackage{ImportPath: "example.com/app/internal/app"}
	want := []string{
		"example.com/app/internal/app.show",
		"example.com/app/internal/app.(*App).edit",
		"example.com/app/internal/app.Report.Show",
		"example.com/app/internal/app.(*List[...]).Show",
		"example.com/app/internal/app.Pair[...].Show",
	}
	if got := actionsIn(listed, parsed(t, source)); !reflect.DeepEqual(
		got, want,
	) {
		t.Errorf("actionsIn named\n%q\nwant\n%q", got, want)
	}
	listed.Name = "main"
	got := actionsIn(listed, parsed(t, source))
	if len(got) == 0 || got[0] != "main.show" {
		t.Errorf("actions in a main package are named %q", got)
	}
}
