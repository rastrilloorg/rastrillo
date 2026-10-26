package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"amadan.net/rastrillo/rastrillo/locales"
)

// The copy `rastrillo new --scaffold bare` shows, one constant each so
// the wording that comes back from copy review is one edit.
const (
	newBareSummaryMsg = "rastrillo new: scaffolded %s/ (bare: a page and a route)"
	newUXBareMsg      = "--ux does nothing with --scaffold bare, which writes no AGENTS.md: leave it out"
)

// writeBare is the bare shape: one page and its route on a ServeMux,
// the design system single writes, and go test ./... as the whole gate.
// No database, no sign-in, no manifest and no gen/: it is for someone
// building a small dashboard who found single too much. With
// --design-system=false it drops the design system too, for an app that
// writes its own HTML or JSON: the *_plain templates, which call no
// render, pages or ErrorPage, as the app has none to compile against.
func writeBare(o newOpts) error {
	c, err := resolveChoices(o)
	if err != nil {
		return err
	}
	name, pkg := o.Name, o.Pkg
	appDir := filepath.Join(name, "internal", pkg)
	d := designSystem(o, c, appDir, false)

	// render.go checks Locale when the package loads, so the pack it
	// needs is linked in the app's package: pt-PT's is pt, zh-Hans' zh.
	packs, err := locales.PacksFor([]string{c.code})
	if err != nil {
		return err
	}
	var imports strings.Builder
	for _, p := range packs {
		fmt.Fprintf(&imports, "\t_ \"amadan.net/rastrillo/rastrillo/locales/%s\"\n", p)
	}
	// The two-page shells' section test reads its body with strings.
	testImports := ""
	if d.tests != "" {
		testImports = "\t\"strings\"\n"
	}
	r := strings.NewReplacer("__NAME__", name, "__PKG__", pkg, "__CODE__", c.code,
		"__VERSION__", rastrilloVersion(), "__PACKS__\n", imports.String(), "__STRINGS__", testImports,
		"__ROUTES__", d.routes, "__HANDLERS__\n", d.handlers, "__TESTS__\n", d.tests)
	bare := func(file string) string { return r.Replace(scaffoldFile("bare/" + file + ".tmpl")) }
	app, test, cmd, locale, readme := "app.go", "app_test.go", "main.go", "locale.go", "README.md"
	if !o.DesignSystem {
		app, test, cmd, locale, readme = "app_plain.go", "app_plain_test.go", "main_plain.go", "locale_plain.go", "README_plain.md"
	}

	files := map[string]string{
		filepath.Join(name, "go.mod"):               bare("go.mod"),
		filepath.Join(name, "README.md"):            bare(readme),
		filepath.Join(name, ".gitignore"):           bare("gitignore"),
		filepath.Join(name, "cmd", name, "main.go"): bare(cmd),
		filepath.Join(appDir, "app.go"):             bare(app),
		filepath.Join(appDir, "app_test.go"):        bare(test),
		filepath.Join(appDir, "locale.go"):          bare(locale),
	}
	if o.DesignSystem {
		files[filepath.Join(appDir, "render.go")] = renderSource(name, pkg, pageList(append(d.pages, "errors")...), false)
		maps.Copy(files, d.files)
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	fmt.Printf(newBareSummaryMsg+"\n", name)
	if o.DesignSystem {
		printIconNotice(o, c.rendered)
	}
	fmt.Printf("\ncd %s && go mod tidy && go test ./...\n", name)
	return nil
}
