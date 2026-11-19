package main

import (
	"fmt"
	"html"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/iconsets"
	"amadan.net/rastrillo/rastrillo/locales"
)

// The copy `rastrillo new --scaffold bare` shows, one constant each so
// the wording that comes back from copy review is one edit.
const (
	newBareSummaryMsg = "rastrillo new: scaffolded %s/ (bare: a page and " +
		"a route)"
	newUXBareMsg = "--ux does nothing with --scaffold bare, which writes " +
		"no AGENTS.md: leave it out"
)

// writeBare is the bare shape: one page and its route on a ServeMux,
// the design system single writes, and go test ./... as the whole gate.
// No database, no sign-in, no manifest and no gen/: it is for someone
// building a small dashboard who found single too much. With
// --design-system=false it drops the design system too, for an app that
// writes its own HTML or JSON: the *_plain templates, which call no
// render, pages or ErrorPage, as the app has none to compile against.
func writeBare(options newOpts) error {
	choices, err := resolveChoices(options)
	if err != nil {
		return err
	}
	name, pkg := options.Name, options.Pkg
	appDir := filepath.Join(name, "internal", pkg)
	design := bareDesignSystem(options, choices, appDir)
	replacer, err := bareReplacer(options, choices, design)
	if err != nil {
		return err
	}
	files := bareFiles(options, replacer)
	if options.DesignSystem {
		pages := pageList(append(design.pages, "errors")...)
		files[filepath.Join(appDir, "render.go")] = bareRender(name, pkg,
			pages)
		maps.Copy(files, design.files)
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	printBareSummary(options, choices.rendered)
	return nil
}

func printBareSummary(options newOpts, rendered iconsets.Rendered) {
	fmt.Printf(newBareSummaryMsg+"\n", options.Name)
	if options.DesignSystem {
		if rendered.AttribName != "" {
			fmt.Printf("  %s   (%s requires attribution — keep it)\n",
				rendered.AttribName, options.IconSet)
		}
		printIconNotice(rendered.Notice, options.IconDelivery)
	}
	fmt.Printf("\ncd %s && go mod tidy && go test ./...\n", options.Name)
}

// bareFiles is the files every bare app has: its module, README,
// command, route table, test and locale. The *_plain templates are the
// ones for --design-system=false.
func bareFiles(
	options newOpts, replacer *strings.Replacer,
) map[string]string {
	bare := func(file string) string {
		return replacer.Replace(scaffoldFile("bare/" + file + ".tmpl"))
	}
	plain := ""
	if !options.DesignSystem {
		plain = "_plain"
	}
	name := options.Name
	appDir := filepath.Join(name, "internal", options.Pkg)
	return map[string]string{
		filepath.Join(name, "go.mod"):     bare("go.mod"),
		filepath.Join(name, "README.md"):  bare("README" + plain + ".md"),
		filepath.Join(name, ".gitignore"): bare("gitignore"),
		filepath.Join(name, "cmd", name, "main.go"): bare(
			"main" + plain + ".go"),
		filepath.Join(appDir, "app.go"): bare("app" + plain + ".go"),
		filepath.Join(appDir, "app_test.go"): bare(
			"app" + plain + "_test.go"),
		filepath.Join(appDir, "locale.go"): bare("locale" + plain + ".go"),
	}
}

// bareReplacer fills the bare templates' __PLACEHOLDERS__. render.go
// checks Locale when the package loads, so the pack it needs is linked
// in the app's package: pt-PT's is pt, zh-Hans' zh.
func bareReplacer(
	options newOpts, choices newChoices, design bareDesign,
) (*strings.Replacer, error) {
	packs, err := locales.PacksFor([]string{choices.code})
	if err != nil {
		return nil, err
	}
	var imports strings.Builder
	for _, pack := range packs {
		fmt.Fprintf(&imports,
			"\t_ \"amadan.net/rastrillo/rastrillo/locales/%s\"\n", pack)
	}
	// The two-page shells' section test reads its body with strings.
	testImports := ""
	if design.tests != "" {
		testImports = "\t\"strings\"\n"
	}
	return strings.NewReplacer("__NAME__", options.Name,
		"__PKG__", options.Pkg, "__CODE__", choices.code,
		"__VERSION__", rastrilloVersion(),
		"__PACKS__\n", imports.String(), "__STRINGS__", testImports,
		"__ROUTES__", design.routes, "__HANDLERS__\n", design.handlers,
		"__TESTS__\n", design.tests), nil
}

// bareDesign is what the design system gives a bare app: the files that
// make its pages (frame, index, error page, static files, icons and an
// icon licence's attribution), and the pages' names. The sidebar and
// console shells open on a phone to the index, with its content hidden
// there (ui's phone layout), so those two alone get a second page to
// hold it, plus the route, handler and test that go with it.
type bareDesign struct {
	files                   map[string]string
	pages                   []string
	routes, handlers, tests string
}

// bareDesignSystem is the design system's files for a bare app in
// appDir: the same layout, pages, icons and vendored files single
// writes (see the list below writeSingle).
func bareDesignSystem(
	options newOpts, choices newChoices, appDir string,
) bareDesign {
	inApp := func(parts ...string) string {
		return filepath.Join(append([]string{appDir}, parts...)...)
	}
	design := bareDesign{pages: []string{"index"}, files: map[string]string{
		inApp("templates", "layout.html"): string(choices.layout),
		inApp("templates", "index.html"):  indexTemplate,
		inApp("templates", "errors.html"): errorsTemplate,
		inApp("icons", "icons.go"):        string(choices.rendered.Source),
	}}
	if twoPageShells[options.Shell] {
		brand := html.EscapeString(options.Name)
		design.files[inApp("templates", "index.html")] = fmt.Sprintf(
			sectionsIndexTemplate, scaffoldOverview, brand)
		design.files[inApp("templates", "overview.html")] = fmt.Sprintf(
			overviewTemplate, scaffoldOverview, brand)
		design.pages = append(design.pages, "overview")
		design.routes, design.handlers = bareOverviewRoute,
			bareOverviewHandler
		design.tests = overviewTest
	}
	// Some licences oblige the app, not the framework. Writing the file
	// is the only way that obligation travels with the code.
	if choices.rendered.AttribName != "" {
		attribution := filepath.Join(options.Name,
			choices.rendered.AttribName)
		design.files[attribution] = string(choices.rendered.Attribution)
	}
	for file, content := range choices.vendored {
		design.files[inApp("static", file)] = string(content)
	}
	return design
}

// The two-page shells' extra route and handler in a bare app, whose
// handlers are plain funcs on its own route table.
const (
	bareOverviewRoute   = "\n\t\t{\"GET /overview\", overview},"
	bareOverviewHandler = `
func overview(w http.ResponseWriter, r *http.Request) {
	render(w, r, "overview", nil)
}
`
)

// pageList is the page names as the Go list render.go loops over.
func pageList(names ...string) string {
	return `"` + strings.Join(names, `", "`) + `"`
}

// bareRender is a bare app's render.go: single's is a method set on App
// with live pages and flash, none of which a bare app has.
func bareRender(name, pkg, pages string) string {
	return strings.NewReplacer("__NAME__", name, "__PKG__", pkg,
		"__PAGES__", pages).Replace(scaffoldFile("bare/render.go.tmpl"))
}
