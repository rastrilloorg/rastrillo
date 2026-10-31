package main

import (
	"cmp"
	"embed"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/internal/appname"
	"amadan.net/rastrillo/rastrillo/internal/generate/apigen"
	"amadan.net/rastrillo/rastrillo/internal/generate/contract"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/iconsets"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
	"amadan.net/rastrillo/rastrillo/ui"
)

// Appendix C.5 flag.new.locale and C.2 op.new.locale_unsupported, verbatim.
const (
	newLocaleFlagMsg        = "the one language the app starts with (default en); one of: %s"
	newLocaleUnsupportedMsg = "rastrillo new --locale %q: the framework's own strings ship in %s. Start with one of those, then add your language to manifest/app.toml"
)

// scaffoldFS is the scaffold's newer files as the files they become, not
// Go string constants: they read as what the app gets, and they leave
// the CLI's own code budget to its code. scaffoldFile panics on a
// missing one, a build bug every scaffold test finds.
//
//go:embed scaffold
var scaffoldFS embed.FS

func scaffoldFile(name string) string {
	b, err := scaffoldFS.ReadFile("scaffold/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// runNew implements `rastrillo new <name>`: the middle-layer app shape
// (SKILL.md's app shape) — GORM models, a chi router, embedded templates, and a
// main.go wiring Resolve → db.Open → New → Serve — plus a passing test
// harness, so the first `go test ./...` is green before the developer
// writes a line.
//
// The declarative path stays one TOML file away: manifest/ is
// scaffolded empty with a README carrying the mounting recipe.
// Manifest-driven and code-driven are equal, optional paths — an app
// uses either or both, per resource.
//
// The scaffold also makes the app a good citizen of the family's two
// hosts: a Makefile whose `ci` target is the one local/CI gate
// definition, and .amadan/ci + .amadan/ci.d/ steps that exec those
// targets — amadan's runner convention, where the steps must be
// executable or the job silently resolves "skipped". CARLOS stays the
// golden deployment target, not a requirement: Resolve/Serve speak the
// activation contract, and `./app -addr :8080` works anywhere.
func runNew(args []string) error {
	fset := flag.NewFlagSet("new", flag.ContinueOnError)
	iconSet := fset.String("icons", "lucide", "icon set: "+strings.Join(iconsets.Names(), ", "))
	iconDelivery := fset.String("icon-delivery", "inline", "how icons load: "+strings.Join(iconsets.Deliveries(), ", "))
	uxProfile := fset.String("ux", "considered", "UX convention profile: "+strings.Join(profileNames(), ", "))
	theme := fset.String("theme", "day", "colour, type and shape theme: "+strings.Join(ui.ThemeNames(), ", "))
	shell := fset.String("shell", "column", "layout shell: "+strings.Join(ui.LayoutNames(), ", "))
	// No default: flag would print (default "en") after the help's own.
	locale := fset.String("locale", "", fmt.Sprintf(newLocaleFlagMsg, strings.Join(rastrillo.BaseLocales(), ", ")))
	authKind := fset.String("auth", "none", "sign-in the app starts with: "+strings.Join(authKinds, ", "))
	if err := fset.Parse(args); err != nil {
		return err
	}
	rest := fset.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: rastrillo new [--icons=%s] [--icon-delivery=%s] [--ux=%s] [--theme=%s] [--shell=%s] [--locale=%s] [--auth=%s] <name>",
			strings.Join(iconsets.Names(), "|"), strings.Join(iconsets.Deliveries(), "|"),
			strings.Join(profileNames(), "|"), strings.Join(ui.ThemeNames(), "|"),
			strings.Join(ui.LayoutNames(), "|"), strings.Join(rastrillo.BaseLocales(), "|"),
			strings.Join(authKinds, "|"))
	}
	if !slices.Contains(authKinds, *authKind) {
		return fmt.Errorf("unknown --auth %q: known kinds are %s", *authKind, strings.Join(authKinds, ", "))
	}
	name := rest[0]
	if err := appname.CheckPackage(name, packageName(name)); err != nil {
		return err
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}

	// Resolve every choice before creating anything: an unknown set,
	// delivery or profile must fail while the working directory is still
	// clean, never halfway through a scaffold.
	rendered, err := iconsets.Render(*iconSet, iconsets.Delivery(*iconDelivery))
	if err != nil {
		return err
	}
	// The conventions record the RESOLVED choices, not the profile's
	// defaults: the flag wins, and the file says what actually happened.
	conventions, err := conventionsSection(*uxProfile, *iconSet, *iconDelivery)
	if err != nil {
		return err
	}
	// The vendored set — tokens.css, the chosen theme, and the three
	// scripts — comes from ui.VendoredAssets, which is the one place
	// that list is written down (ui/vendored.go). The generated
	// vendored_test.go below and `rastrillo doctor` read the same
	// function, so a file added to the set reaches the scaffold, the
	// pin and the doctor together rather than one at a time.
	vendored, ok := ui.VendoredAssets(*theme)
	if !ok {
		return fmt.Errorf("unknown theme %q: known themes are %s", *theme, strings.Join(ui.ThemeNames(), ", "))
	}
	layoutHTML, ok := ui.Layout(*shell)
	if !ok {
		return fmt.Errorf("unknown shell %q: known shells are %s", *shell, strings.Join(ui.LayoutNames(), ", "))
	}
	// Only a language whose framework strings ship: any other fails
	// generate --check on its missing catalog before the author writes a line.
	code := cmp.Or(*locale, "en")
	if to, ok := rastrillo.RetagOf(code); ok { // a code from before a retag: pt means pt-PT
		code = to
	}
	if !slices.Contains(rastrillo.BaseLocales(), code) {
		return fmt.Errorf(newLocaleUnsupportedMsg, code, strings.Join(rastrillo.BaseLocales(), ", "))
	}

	pkg := packageName(name)
	appDir := filepath.Join(name, "internal", pkg)
	inApp := func(parts ...string) string {
		return filepath.Join(append([]string{appDir}, parts...)...)
	}
	inTests := func(file string) string {
		return filepath.Join(name, "internal", pkg+"test", file)
	}
	atRoot := func(parts ...string) string {
		return filepath.Join(append([]string{name}, parts...)...)
	}
	env, prefix := strings.ToUpper(pkg), apitoken.DerivePrefix(name)

	// The sidebar and console shells open on a phone to the index, whose
	// content is hidden there, so they alone get a second page to hold
	// it, with its path, route, action and test.
	indexHTML, pages := indexTemplate, `"index", "errors", "tokens"`
	var paths, routes, actions, extraTests string
	if twoPageShells[*shell] {
		indexHTML = fmt.Sprintf(sectionsIndexTemplate, scaffoldOverview,
			html.EscapeString(name))
		pages = `"index", "overview", "errors", "tokens"`
		paths, routes = overviewPath, overviewRoute
		actions, extraTests = overviewAction, overviewTest
	}

	files := map[string]string{}
	files[atRoot("go.mod")] = fmt.Sprintf(goModTemplate,
		name, rastrilloVersion(), chiPinnedVersion, gormPinnedVersion)
	files[atRoot("cmd", name, "main.go")] = fmt.Sprintf(mainTemplate,
		name, pkg, env)
	files[inApp("app.go")] = fmt.Sprintf(appTemplate, pkg, name)
	files[inApp("routes.go")] = fmt.Sprintf(routesTemplate, pkg, paths, routes)
	files[inApp("models.go")] = fmt.Sprintf(modelsTemplate, pkg)
	files[inApp("migrations.go")] = fmt.Sprintf(migrationsTemplate, pkg)
	files[inApp("handlers.go")] = fmt.Sprintf(handlersTemplate, pkg, actions)
	files[inApp("render.go")] = fmt.Sprintf(renderTemplate, name, pkg, pages)
	files[inApp("api.go")] = fmt.Sprintf(apiTemplate, pkg, name, prefix)
	files[inApp("tokens.go")] = fmt.Sprintf(tokensTemplate, pkg)
	files[inApp("templates", "tokens.html")] = scaffoldFile("tokens.html")
	files[inApp("templates", "layout.html")] = string(layoutHTML)
	files[inApp("templates", "index.html")] = indexHTML
	files[inApp("templates", "errors.html")] = errorsTemplate
	files[inApp("migrations", "0001_init.sql")] = initMigrationTemplate
	files[inApp("migrations", "schema.sql")] = schemaSQLTemplate
	files[inApp("icons", "icons.go")] = string(rendered.Source)
	files[inTests("api_test.go")] = fmt.Sprintf(apiTestTemplate,
		name, pkg, prefix)
	files[inTests("harness_test.go")] = fmt.Sprintf(harnessTemplate, name, pkg)
	files[inTests("vendored_test.go")] = fmt.Sprintf(vendoredTestTemplate,
		pkg, *theme)
	files[inTests("index_test.go")] = fmt.Sprintf(indexTestTemplate,
		name, pkg, extraTests)
	files[inTests("browser_test.go")] = fmt.Sprintf(browserTestTemplate,
		name, pkg)
	files[inTests("perf_test.go")] = fmt.Sprintf(perfTestTemplate, name, pkg)
	files[atRoot(".gitleaks.toml")] = scaffoldFile("gitleaks.toml")
	files[atRoot(".rastrillo", "budgets.txt")] = budgetsTxtTemplate
	files[atRoot("README.md")] = fmt.Sprintf(readmeTemplate, name, pkg, env)
	files[atRoot("manifest", "README.md")] = fmt.Sprintf(manifestReadme,
		name, pkg)
	files[atRoot("manifest", "app.toml")] = fmt.Sprintf(appTOMLTemplate,
		code)
	files[atRoot("Makefile")] = fmt.Sprintf(makefileTemplate, name,
		staticcheckVersion, govulncheckVersion, gitleaksVersion, goToolchain)
	files[atRoot(".gitignore")] = fmt.Sprintf(gitignoreTemplate, name)
	// AGENTS.md carries the instructions, for whatever agent someone
	// uses; CLAUDE.md is an @AGENTS.md import and nothing else.
	files[atRoot("AGENTS.md")] = fmt.Sprintf(agentsMDTemplate, name) +
		string(conventions)
	files[atRoot("CLAUDE.md")] = claudeMDPointer
	// The vendored static files: see the list below runNew.
	if twoPageShells[*shell] {
		files[filepath.Join(appDir, "templates", "overview.html")] = fmt.Sprintf(overviewTemplate, scaffoldOverview, html.EscapeString(name))
	}

	for name, content := range vendored {
		files[filepath.Join(appDir, "static", name)] = string(content)
	}

	if *authKind == "magic" {
		if err := applyMagicAuth(files, name, pkg, appDir); err != nil {
			return err
		}
	}

	if err := writeScaffold(name, files); err != nil {
		return err
	}
	// The packs app.toml above declares, written now so a new app
	// builds and boots before its first rastrillo generate. So are
	// gen/api.go, which api.go imports, and api/v1.json, without which
	// generate --check fails: both as generate writes them for an app
	// with no resources, so the gate is green before the first manifest.
	if _, err := packs.Emit(filepath.Join(name, "gen"), manifest.App{Locales: []string{code}, DefaultLocale: code}); err != nil {
		return err
	}
	apiFile, err := apigen.File(name, nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(name, "gen", "api.go"), apiFile, 0o644); err != nil {
		return err
	}
	if _, err := contract.Write(name, nil, false); err != nil {
		return err
	}

	// CI scripts must be executable: amadan's runner resolves a
	// non-executable job "skipped" with only a hint — the known silent
	// failure mode, closed at scaffold time.
	ciScripts := map[string]string{
		filepath.Join(name, ".amadan", "ci"): amadanCI,
	}
	for _, step := range runnerSteps {
		path := filepath.Join(name, ".amadan", "ci.d", step.file)
		ciScripts[path] = amadanStep(step.target)
	}
	for path, content := range ciScripts {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return err
		}
	}

	// Some licences oblige the app, not the framework. Writing the file
	// is the only way that obligation travels with the code.
	if rendered.AttribName != "" {
		if err := os.WriteFile(filepath.Join(name, rendered.AttribName), rendered.Attribution, 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("rastrillo new: scaffolded %s/\n", name)
	fmt.Printf("  cmd/%s/main.go       (Resolve -> db.Open -> New -> Serve)\n",
		name)
	fmt.Printf("  internal/%s/         (app, routes, handlers, models, "+
		"migrations, render, templates, static)\n", pkg)
	fmt.Printf("  internal/%stest/     (harness + example tests, passing out of the box;\n", pkg)
	fmt.Println("                        browser_test.go = the browser drive, go test -tags browser ./...)")
	fmt.Println("  manifest/            (the declarative path: drop a <name>.toml here, see its README)")
	fmt.Println("  Makefile             (make ci = vet + fmt + staticcheck + " +
		"govulncheck + gitleaks + budget + code + routes + test + perf + " +
		"migration check + generate check, the one gate definition;")
	fmt.Println("                        make release = the stripped binary)")
	fmt.Println("  .gitignore           (build output and the local database)")
	fmt.Println("  .amadan/ci, ci.d/    (amadan runner CI, executable, delegating to make)")
	fmt.Printf("  internal/%s/icons/   (%s, %s — app-owned, edit freely)\n", pkg, *iconSet, *iconDelivery)
	fmt.Printf("  internal/%s/static/theme.css (the %s theme — app-owned; --theme picks from %s)\n",
		pkg, *theme, strings.Join(ui.ThemeNames(), ", "))
	fmt.Printf("  internal/%s/templates/layout.html (the %s shell — app-owned; --shell picks from %s)\n",
		pkg, *shell, strings.Join(ui.LayoutNames(), ", "))
	fmt.Println("  AGENTS.md            (instructions + UX conventions, the source of truth)")
	fmt.Println("  CLAUDE.md            (an @AGENTS.md import, nothing more)")
	fmt.Println("  README.md            (what this app is, and how the browser drive runs)")
	if rendered.AttribName != "" {
		fmt.Printf("  %s   (%s requires attribution — keep it)\n", rendered.AttribName, *iconSet)
	}

	printIconNotice(rendered.Notice, *iconDelivery)

	newTemplates(name)

	// The scaffold ships with passing tests (the harness above), so the
	// first suggested command is running them: the TDD loop starts from
	// green, not from "figure out how to test this at all".
	fmt.Printf("\ncd %s && go mod tidy && go test ./...\n", name)
	return nil
}

// The vendored static files rastrillo new writes into static/, from the
// one list in ui.VendoredAssets. Each is delivered once and app-owned
// from then on: rastrillo.Serve never serves CSS at runtime, and
// new/generate never touch these again.
//
//	tokens.css     the design-token stylesheet — structure only
//	theme.css      the colour, type family and shape tokens
//	               tokens.css paints its classes with; swap it for
//	               another of ui.ThemeNames() or edit it freely
//	shell.css      the sidebar and console shells' slide between
//	               the index and a page on a phone
//	rastrillo.js   the fragment shim behind data-poll and menu dismiss
//	busy.js        the busy rule: every submit button, data-busy opts out
//	live.js        live pages: inert until a page carries the
//	               rastrillo-live meta
//	light.js       the light the depth styles read; delete it for the
//	               still design
//	anchor.js      a dropdown's menu kept on the screen where CSS anchor
//	               positioning is missing; inert where it is not, and
//	               deletable once every browser the app supports has it
//	shell.js       the same two shells' phone navigation: the slide's
//	               direction, a back control that reuses history,
//	               focus returned to the section the reader left
//	select.js      field-select's searchable enhancement, inert
//	               until a <select> opts in with data-rst-select
//	datetime.js    the date fields' natural-language combobox,
//	               inert until an input opts in with data-rst-date
//	calendar.js    the month grid that combobox's button opens,
//	               inert until datetime.js asks it for a panel
//
// An app that never renders a big select or a date field can delete
// that script and its tag; nothing else changes. The date fields are
// two files, and deleting one of the pair is the awkward case: drop
// calendar.js alone and the field still works, with its button
// falling back to the browser's own picker. shell.js and shell.css
// are written for every shell like the rest (select.js and calendar.js
// already ship to apps that never link them), and are deletable where
// the shell does not link them; the app's vendoring test then needs
// them in vendoredIsMine, or it fails on the missing files.

// The scaffold's chi and gorm pins. The CLI's own build info can't
// supply these (cmd/rastrillo imports neither), so they are constants
// — kept honest by a test that compares them against the rastrillo
// module's own go.mod, the versions the framework's suite actually ran
// with. Bump them together with the root go.mod, like
// rastrilloFallbackVersion is bumped per release.
const (
	chiPinnedVersion  = "v5.3.2"
	gormPinnedVersion = "v1.31.2"
)

const goModTemplate = `module %s

go 1.24

require (
	amadan.net/rastrillo/rastrillo %s
	github.com/go-chi/chi/v5 %s
	gorm.io/gorm %s
)

// The CLI make ci runs. Without this line go mod tidy drops its
// dependencies from go.sum, and make ci fails on a missing entry.
tool amadan.net/rastrillo/rastrillo/cmd/rastrillo
`

// The app's Go files, as the files they become: %[n]s are filled in at
// scaffold time, in the order each runNew call gives.
var (
	mainTemplate         = scaffoldFile("main.go.tmpl")
	appTemplate          = scaffoldFile("app.go.tmpl")
	routesTemplate       = scaffoldFile("routes.go.tmpl")
	modelsTemplate       = scaffoldFile("models.go.tmpl")
	migrationsTemplate   = scaffoldFile("migrations.go.tmpl")
	handlersTemplate     = scaffoldFile("handlers.go.tmpl")
	renderTemplate       = scaffoldFile("render.go.tmpl")
	harnessTemplate      = scaffoldFile("harness_test.go.tmpl")
	vendoredTestTemplate = scaffoldFile("vendored_test.go.tmpl")
	indexTestTemplate    = scaffoldFile("index_test.go.tmpl")
	browserTestTemplate  = scaffoldFile("browser_test.go.tmpl")
	perfTestTemplate     = scaffoldFile("perf_test.go.tmpl")
	apiTemplate          = scaffoldFile("api.go.tmpl")
	tokensTemplate       = scaffoldFile("tokens.go.tmpl")
	apiTestTemplate      = scaffoldFile("api_test.go.tmpl")
	readmeTemplate       = scaffoldFile("README.md.tmpl")
	makefileTemplate     = scaffoldFile("Makefile.tmpl")
	agentsMDTemplate     = scaffoldFile("AGENTS.md.tmpl")
)

// appTOMLTemplate's comment is Appendix C.6's scaffold.app_toml.
const appTOMLTemplate = `# The locales this app offers. Add a code to offer another language,
# then run rastrillo generate; with two or more, URLs gain /xx/
# prefixes and pages a language switcher.
locales = [%[1]q]
default_locale = %[1]q
`

// initMigrationTemplate is what rastrillo migration generate writes for
// the Note model, pasted here so new needs no network or build.
// TestScaffoldMigratesAndPassesCheck keeps the two in step.
const initMigrationTemplate = "CREATE TABLE `notes` (`id` integer PRIMARY KEY AUTOINCREMENT,`title` text,`body` text,`created_at` datetime,`updated_at` datetime);\n\n"

// schemaSQLTemplate is the snapshot migration generate writes beside it.
const schemaSQLTemplate = `-- Generated by rastrillo migration generate; DO NOT EDIT.
-- The schema every migration in this directory adds up to.

CREATE TABLE ` + "`notes`" + ` (` + "`id`" + ` integer PRIMARY KEY AUTOINCREMENT,` + "`title`" + ` text,` + "`body`" + ` text,` + "`created_at`" + ` datetime,` + "`updated_at`" + ` datetime);

`

const indexTemplate = `{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// twoPageShells are the shells whose phone layout is an index page and a
// back control. A scaffold of one of them with a single page would open
// on a phone to a title and an empty list, because that page is the
// index and its content is hidden there, so these get an index with one
// section and the page it opens.
var twoPageShells = map[string]bool{"sidebar": true, "console": true}

// scaffoldOverview is the one section's name. It is user-facing copy, so
// a change to it goes through the copy review.
const scaffoldOverview = "Overview"

// sectionsIndexTemplate is templates/index.html for the two-page shells.
// %[1]s is the section's name and %[2]s the app's, which is the brand:
// the layout's default brand, Home, would title the app "Home" on a phone.
const sectionsIndexTemplate = `{{/* The home page, and on a phone the list
     of sections. Give each section a nav link with an id, and its page
     an up block naming {{path $ "/#<that id>"}}, so a phone comes back
     to the row it left. */}}
{{define "view"}}index{{end}}
{{define "brand"}}<a rst-shell-brand href="{{path $ "/"}}">%[2]s</a>{{end}}
{{define "nav"}}<a id="nav-overview" href="{{path $ "/overview"}}">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// overviewTemplate is templates/overview.html, the one section page,
// with the same two arguments: a block is per page here, so every page
// names the brand.
const overviewTemplate = `{{/* A section. up is its way back: the
     index's row for it. */}}
{{define "up"}}{{path $ "/#nav-overview"}}{{end}}
{{define "brand"}}<a rst-shell-brand href="{{path $ "/"}}">%[2]s</a>{{end}}
{{define "nav"}}<a id="nav-overview" href="{{path $ "/overview"}}" aria-current="page">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// The two-page shells' extra path, route and action.
const (
	overviewPath   = "\nconst overviewPage action.Path = \"/overview\""
	overviewRoute  = "\n\t\tguarded.Get(overviewPage, app.overview)"
	overviewAction = `
func (app *App) overview(visit *action.Visit) {
	visit.Render("overview", http.StatusOK, nil)
}
`
)

// overviewTest is appended to index_test.go for the two-page shells: the
// section page renders, and its back control goes to the index's row.
const overviewTest = `
func TestOverviewRenders(t *testing.T) {
	t.Parallel()
	recorder := get(t, newApp(t), "/overview")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /overview: status %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(),
		` + "`" + `href="/#nav-overview" rel="up"` + "`" + `) {
		t.Errorf("GET /overview: no way back to the index's row:\n%s",
			recorder.Body.String())
	}
}
`

// errorsTemplate is templates/errors.html: ui's error-page partial in
// the layout, so restyling the error page means editing this one file.
const errorsTemplate = `{{/* The app's error page, drawn by ErrorPage.
     Hand the partial Title, Body, HomeHref or BackHref to change its
     wording; ui/partials/error-page.html says what it accepts. */}}
{{define "content"}}{{$L := localizer $}}{{template "error-page" dict "L" $L "Status" .Status "Ref" .Ref "HomeHref" (path $ "/")}}{{end}}
`

// manifestReadme is manifest/README.md, the recipe for mounting the
// first manifest resource. %[1]s is the module, %[2]s the app package.
var manifestReadme = scaffoldFile("manifest-README.md")

// staticcheckVersion is the staticcheck release a scaffolded app's gate
// runs, and the one rastrillo's own Makefile pins;
// TestStaticcheckPinMatchesRastrillosGate holds the two together.
// v0.8.2 rather than v0.7.0 so a move of goToolchain to Go 1.27 does not
// also need this: v0.7.0 cannot read 1.27's export data and fails on
// every package ("export data version 5 is greater than maximum
// supported version 2"), turning the gate red with nothing wrong in the
// code. v0.8.2 needs go 1.26 to build, which goToolchain already is.
const staticcheckVersion = "v0.8.2"

// govulncheckVersion, gitleaksVersion and goToolchain are the scaffold's
// copies of the other pins in rastrillo's own Makefile, held to them by
// TestGatePinsMatchRastrillosGate.
const (
	govulncheckVersion = "v1.8.0"
	gitleaksVersion    = "v8.30.1"
	goToolchain        = "go1.26.10"
)

const gitignoreTemplate = `# Build output and the local database.
/releases/
/%[1]s.db
/%[1]s.db-wal
/%[1]s.db-shm
`

const amadanCI = `#!/bin/sh
# For a runner without steps. Keep it executable, or the job is skipped.
exec make ci
`

// runnerSteps are .amadan/ci.d/'s steps, each the make target of a lane
// on the ci line, so a runner that reports steps runs the same gate.
var runnerSteps = []struct{ file, target string }{
	{"10-vet", "vet"}, {"15-budget", "budget"}, {"16-code", "code"},
	{"17-routes", "routes"}, {"20-fmt", "fmt-check"},
	{"25-staticcheck", "staticcheck"}, {"26-govulncheck", "govulncheck"},
	{"27-gitleaks", "gitleaks"}, {"30-test", "test"}, {"35-perf", "perf"},
	{"40-migration-check", "migration-check"},
	{"45-generate-check", "generate-check"},
}

// amadanStep emits one .amadan/ci.d/ step: exec a Makefile target, so
// CI and the local gate stay one definition.
func amadanStep(target string) string {
	return "#!/bin/sh\nexec make " + target + "\n"
}
