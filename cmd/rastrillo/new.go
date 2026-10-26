package main

import (
	"cmp"
	"embed"
	"flag"
	"fmt"
	"go/token"
	"html"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
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
// (SKILL.md's app shape) — GORM models, a route table on a ServeMux, embedded templates, and a
// main.go wiring Resolve → db.Open → App → Serve — plus a passing test
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
	// No default, like --locale: the help's own wording carries it.
	scaffold := fset.String("scaffold", "", newScaffoldFlagMsg)
	if err := fset.Parse(args); err != nil {
		return err
	}
	rest := fset.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: rastrillo new [--scaffold=%s] [--icons=%s] [--icon-delivery=%s] [--ux=%s] [--theme=%s] [--shell=%s] [--locale=%s] [--auth=%s] <name>",
			strings.Join(shapeNames(), "|"),
			strings.Join(iconsets.Names(), "|"), strings.Join(iconsets.Deliveries(), "|"),
			strings.Join(profileNames(), "|"), strings.Join(ui.ThemeNames(), "|"),
			strings.Join(ui.LayoutNames(), "|"), strings.Join(rastrillo.BaseLocales(), "|"),
			strings.Join(authKinds, "|"))
	}
	// Every refusal comes before the directory exists: a refused run must
	// not leave a half-made app that makes the next, correct, attempt
	// fail with "already exists".
	chosen, err := lookupShape(cmp.Or(*scaffold, defaultShape))
	if err != nil {
		return err
	}
	if !slices.Contains(authKinds, *authKind) {
		return fmt.Errorf("unknown --auth %q: known kinds are %s", *authKind, strings.Join(authKinds, ", "))
	}
	name := rest[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}
	set := map[string]bool{}
	fset.Visit(func(f *flag.Flag) { set[f.Name] = true })
	o := newOpts{
		Name: name, Pkg: packageName(name), Locale: *locale,
		IconSet: *iconSet, IconDelivery: *iconDelivery, UX: *uxProfile,
		Theme: *theme, Shell: *shell, Auth: *authKind,
		DesignSystem: true, Set: set,
	}
	if err := checkChoices(o); err != nil {
		return err
	}
	return chosen.Write(o)
}

// newChoices are the icon, UX, theme, shell and locale flags resolved
// to what a scaffold writes.
type newChoices struct {
	rendered    iconsets.Rendered
	conventions []byte
	vendored    map[string][]byte
	layout      []byte
	code        string
}

// checkChoices refuses a bad icon, delivery, profile, theme, shell or
// locale. runNew calls it for every shape before the shape's Write: an
// unknown choice must fail while the working directory is still clean,
// never halfway through a scaffold, and no shape may forget to.
func checkChoices(o newOpts) error {
	_, err := resolveChoices(o)
	return err
}

func resolveChoices(o newOpts) (newChoices, error) {
	rendered, err := iconsets.Render(o.IconSet, iconsets.Delivery(o.IconDelivery))
	if err != nil {
		return newChoices{}, err
	}
	// The conventions record the RESOLVED choices, not the profile's
	// defaults: the flag wins, and the file says what actually happened.
	conventions, err := conventionsSection(o.UX, o.IconSet, o.IconDelivery)
	if err != nil {
		return newChoices{}, err
	}
	// The vendored set — tokens.css, the chosen theme, and the three
	// scripts — comes from ui.VendoredAssets, which is the one place
	// that list is written down (ui/vendored.go). The generated
	// vendored_test.go below and `rastrillo doctor` read the same
	// function, so a file added to the set reaches the scaffold, the
	// pin and the doctor together rather than one at a time.
	vendored, ok := ui.VendoredAssets(o.Theme)
	if !ok {
		return newChoices{}, fmt.Errorf("unknown theme %q: known themes are %s", o.Theme, strings.Join(ui.ThemeNames(), ", "))
	}
	layoutHTML, ok := ui.Layout(o.Shell)
	if !ok {
		return newChoices{}, fmt.Errorf("unknown shell %q: known shells are %s", o.Shell, strings.Join(ui.LayoutNames(), ", "))
	}
	// Only a language whose framework strings ship: any other fails
	// generate --check on its missing catalog before the author writes a line.
	code := cmp.Or(o.Locale, "en")
	if to, ok := rastrillo.RetagOf(code); ok { // a code from before a retag: pt means pt-PT
		code = to
	}
	if !slices.Contains(rastrillo.BaseLocales(), code) {
		return newChoices{}, fmt.Errorf(newLocaleUnsupportedMsg, code, strings.Join(rastrillo.BaseLocales(), ", "))
	}
	return newChoices{rendered, conventions, vendored, layoutHTML, code}, nil
}

// writeSingle is the single shape: one app, its SQLite data, and the
// design system. It is what `rastrillo new` has always written.
func writeSingle(o newOpts) error {
	name, pkg := o.Name, o.Pkg

	c, err := resolveChoices(o)
	if err != nil {
		return err
	}
	rendered, conventions, vendored, layoutHTML, code := c.rendered, c.conventions, c.vendored, c.layout, c.code

	dirs := []string{
		name,
		filepath.Join(name, "cmd", name),
		filepath.Join(name, "manifest"),
		filepath.Join(name, "internal", pkg),
		filepath.Join(name, "internal", pkg, "static"),
		filepath.Join(name, "internal", pkg, "templates"),
		filepath.Join(name, "internal", pkg, "migrations"),
		filepath.Join(name, "internal", pkg, "icons"),
		filepath.Join(name, ".amadan", "ci.d"),
		filepath.Join(name, ".rastrillo"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	appDir := filepath.Join(name, "internal", pkg)

	// The sidebar and console shells open on a phone to the index, with
	// its content hidden there (ui's phone layout), so those two shells
	// alone get a second page to hold it, plus the route, handler and
	// test that go with it. Every other shell keeps today's one page.
	indexHTML, routes, handlers, pages, extraTests := indexTemplate, "", "", `"index", "errors", "tokens"`, ""
	if twoPageShells[o.Shell] {
		indexHTML = fmt.Sprintf(sectionsIndexTemplate, scaffoldOverview, html.EscapeString(name))
		routes = "\n\t\t{\"GET /overview\", a.overview},"
		handlers = overviewHandler
		pages = `"index", "overview", "errors", "tokens"`
		extraTests = overviewTest
	}

	files := map[string]string{
		filepath.Join(name, "go.mod"): fmt.Sprintf(goModTemplate,
			name, rastrilloVersion(), gormPinnedVersion),
		filepath.Join(name, "cmd", name, "main.go"): fmt.Sprintf(mainTemplate, name, pkg, strings.ToUpper(pkg)),
		filepath.Join(appDir, "app.go"):             fmt.Sprintf(appTemplate, pkg, routes, name),
		filepath.Join(appDir, "models.go"):          fmt.Sprintf(modelsTemplate, pkg),
		filepath.Join(appDir, "migrations.go"):      fmt.Sprintf(migrationsTemplate, pkg),
		filepath.Join(appDir, "handlers.go"):        fmt.Sprintf(handlersTemplate, pkg, handlers),
		filepath.Join(appDir, "render.go"):          fmt.Sprintf(renderTemplate, name, pkg, pages),
		// The JSON API and its token page (JSON API spec §3, §5). The
		// prefix is worked out here, once, from the app's name.
		filepath.Join(appDir, "api.go"):                   fmt.Sprintf(scaffoldFile("api.go.tmpl"), pkg, name, apitoken.DerivePrefix(name)),
		filepath.Join(appDir, "tokens.go"):                fmt.Sprintf(scaffoldFile("tokens.go.tmpl"), pkg),
		filepath.Join(appDir, "templates", "tokens.html"): scaffoldFile("tokens.html"),
		filepath.Join(appDir, "api_test.go"):              fmt.Sprintf(scaffoldFile("api_test.go.tmpl"), name, pkg, apitoken.DerivePrefix(name)),
		filepath.Join(name, ".gitleaks.toml"):             scaffoldFile("gitleaks.toml"),
		// The page frame, chosen by --shell and delivered verbatim from
		// ui.Layout: app-owned from here on, exactly like tokens.css.
		// Its chrome is blocks with working defaults (title, lang, dir,
		// and in the chrome shells brand/nav/account/locale/foot), so a
		// page overrides only what it cares about.
		filepath.Join(appDir, "templates", "layout.html"): string(layoutHTML),
		filepath.Join(appDir, "templates", "index.html"):  indexHTML,
		filepath.Join(appDir, "templates", "errors.html"): errorsTemplate,
		// The initial migration and the schema.sql snapshot it adds up
		// to, both static: the scaffold's Note model is fixed and
		// known at the time this template is written, so unlike a real
		// app's later migrations, generating this one at `new` time
		// would buy nothing but a network fetch and a full compile —
		// exactly what `new` promises not to need. Produced once by
		// hand (scaffold an app, run `rastrillo migration generate`
		// against it, paste the result here); TestScaffoldMigratesAndPassesCheck
		// is what keeps it from drifting off of modelsTemplate's Note.
		filepath.Join(appDir, "migrations", "0001_init.sql"): initMigrationTemplate,
		filepath.Join(appDir, "migrations", "schema.sql"):    schemaSQLTemplate,
		// The test harness, delivered once like tokens.css: app-owned
		// from here on — edit it, grow it, or delete it. The example
		// tests pass on a fresh scaffold and pin the out-of-the-box
		// asset-fingerprinting behavior.
		filepath.Join(appDir, "harness_test.go"): fmt.Sprintf(harnessTemplate, name, pkg),
		// The pin that makes the vendored bytes above verifiable: a
		// reviewer runs the suite and knows static/'s ~56KB is the
		// library's, not app diff to read line by line.
		filepath.Join(appDir, "vendored_test.go"): fmt.Sprintf(vendoredTestTemplate, pkg, o.Theme),
		filepath.Join(appDir, "index_test.go"):    fmt.Sprintf(indexTestTemplate, name, pkg, extraTests),
		// The migration check, as a test go test ./... runs (make test).
		filepath.Join(appDir, "migrations_test.go"): fmt.Sprintf(scaffoldFile("migrations_test.go.tmpl"), name, pkg),
		// The browser drive, on the same delivered-once terms as the
		// harness. browser_test.go, never harness_test.go: that name
		// (and word) already belongs to the httptest HTTP harness.
		filepath.Join(appDir, "browser_test.go"): fmt.Sprintf(browserTestTemplate, name, pkg, strings.ToUpper(pkg)),
		// The perf lane and the exemptions file every budget reads.
		filepath.Join(appDir, "perf_test.go"):            fmt.Sprintf(perfTestTemplate, name, pkg),
		filepath.Join(name, ".rastrillo", "budgets.txt"): budgetsTxtTemplate,
		filepath.Join(name, "README.md"):                 fmt.Sprintf(scaffoldFile("README.md.tmpl"), name, pkg),
		filepath.Join(name, "manifest", "README.md"):     fmt.Sprintf(manifestReadme, name, pkg),
		filepath.Join(name, "manifest", "app.toml"):      fmt.Sprintf(appTOMLTemplate, code),
		filepath.Join(name, "Makefile"):                  fmt.Sprintf(makefileTemplate, name, staticcheckVersion, govulncheckVersion, gitleaksVersion, goToolchain),
		filepath.Join(name, ".gitignore"):                fmt.Sprintf(gitignoreTemplate, name),
		// The app's icon set, on the same terms as tokens.css and
		// rastrillo.js: delivered once, app-owned from here on.
		filepath.Join(appDir, "icons", "icons.go"): string(rendered.Source),
		// AGENTS.md carries the instructions — it is the cross-agent
		// file, so they reach whatever agent someone uses. CLAUDE.md is
		// an @AGENTS.md import and nothing else.
		filepath.Join(name, "AGENTS.md"): fmt.Sprintf(scaffoldFile("AGENTS.md.tmpl"), name) + string(conventions),
		filepath.Join(name, "CLAUDE.md"): claudeMDPointer,
	}
	// The vendored static files, written from the one list. Each is
	// delivered once and app-owned from here on: rastrillo.Serve never
	// serves CSS at runtime, and new/generate never touch these again.
	//
	//	tokens.css     the design-token stylesheet — structure only
	//	theme.css      the colour, type family and shape tokens
	//	               tokens.css paints its classes with; swap it for
	//	               another of ui.ThemeNames() or edit it freely
	//	shell.css      the sidebar and console shells' slide between
	//	               the index and a page on a phone
	//	rastrillo.js   the fragment shim behind data-poll and menu dismiss
	//	busy.js        the busy rule: every submit button, data-busy opts out
	//	light.js       the light the depth styles read; delete it for the still design
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
	if twoPageShells[o.Shell] {
		files[filepath.Join(appDir, "templates", "overview.html")] = fmt.Sprintf(overviewTemplate, scaffoldOverview, html.EscapeString(name))
	}

	for name, content := range vendored {
		files[filepath.Join(appDir, "static", name)] = string(content)
	}

	if o.Auth == "magic" {
		if err := applyMagicAuth(files, name, pkg, appDir); err != nil {
			return err
		}
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
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
		filepath.Join(name, ".amadan", "ci"):                        amadanCI,
		filepath.Join(name, ".amadan", "ci.d", "10-vet"):            amadanStep("vet"),
		filepath.Join(name, ".amadan", "ci.d", "15-budget"):         amadanStep("budget"),
		filepath.Join(name, ".amadan", "ci.d", "20-fmt"):            amadanStep("fmt-check"),
		filepath.Join(name, ".amadan", "ci.d", "25-staticcheck"):    amadanStep("staticcheck"),
		filepath.Join(name, ".amadan", "ci.d", "26-govulncheck"):    amadanStep("govulncheck"),
		filepath.Join(name, ".amadan", "ci.d", "27-gitleaks"):       amadanStep("gitleaks"),
		filepath.Join(name, ".amadan", "ci.d", "30-test"):           amadanStep("test"),
		filepath.Join(name, ".amadan", "ci.d", "35-perf"):           amadanStep("perf"),
		filepath.Join(name, ".amadan", "ci.d", "45-generate-check"): amadanStep("generate-check"),
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
	fmt.Printf("  cmd/%s/main.go       (Resolve -> db.Open -> App -> Serve)\n", name)
	fmt.Printf("  internal/%s/         (models, migrations, app, handlers, render, templates, static)\n", pkg)
	fmt.Printf("  internal/%s/*_test.go (harness + example tests beside the code, passing out of the box;\n", pkg)
	fmt.Println("                        browser_test.go = the browser drive, go test -tags browser ./...)")
	fmt.Println("  manifest/            (the declarative path: drop a <name>.toml here, see its README)")
	fmt.Println("  Makefile             (make ci = vet + fmt + staticcheck + govulncheck + gitleaks + budget + test + perf + generate check, the one gate definition;")
	fmt.Println("                        make release = the stripped binary)")
	fmt.Println("  .gitignore           (build output and the local database)")
	fmt.Println("  .amadan/ci, ci.d/    (amadan runner CI, executable, delegating to make)")
	fmt.Printf("  internal/%s/icons/   (%s, %s — app-owned, edit freely)\n", pkg, o.IconSet, o.IconDelivery)
	fmt.Printf("  internal/%s/static/theme.css (the %s theme — app-owned; --theme picks from %s)\n",
		pkg, o.Theme, strings.Join(ui.ThemeNames(), ", "))
	fmt.Printf("  internal/%s/templates/layout.html (the %s shell — app-owned; --shell picks from %s)\n",
		pkg, o.Shell, strings.Join(ui.LayoutNames(), ", "))
	fmt.Println("  AGENTS.md            (instructions + UX conventions, the source of truth)")
	fmt.Println("  CLAUDE.md            (an @AGENTS.md import, nothing more)")
	fmt.Println("  README.md            (what this app is, and how the browser drive runs)")
	if rendered.AttribName != "" {
		fmt.Printf("  %s   (%s requires attribution — keep it)\n", rendered.AttribName, o.IconSet)
	}

	// Informed consent, stated once. A supported choice that nags on
	// every build is not really supported.
	if rendered.Notice != "" {
		fmt.Printf("\nnote: --icon-delivery=%s\n", o.IconDelivery)
		for _, line := range strings.Split(strings.TrimRight(rendered.Notice, "\n"), "\n") {
			fmt.Println("  " + line)
		}
	}

	// The scaffold ships with passing tests (the harness above), so the
	// first suggested command is running them: the TDD loop starts from
	// green, not from "figure out how to test this at all".
	fmt.Printf("\ncd %s && go mod tidy && go test ./...\n", name)
	return nil
}

// packageName derives a valid Go identifier from the app name for the
// scaffolded app package: name also serves as the module path, where
// hyphens (and other punctuation) are legal, but a package clause
// needs an identifier. Non-identifier runes are dropped rather than
// rejected, so every name rastrillo new already accepts keeps working.
// A leading digit (decoded properly, not just the first byte, so a
// multi-byte leading digit is still caught) or a bare Go keyword — a
// package clause can't start with either — earns the same "app" prefix.
func packageName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	first, _ := utf8.DecodeRuneInString(out)
	if out == "" || unicode.IsDigit(first) || token.IsKeyword(out) {
		out = "app" + out
	}
	return out
}

// The scaffold's chi and gorm pins; chi only for --auth=magic, whose
// sign-in groups are chi's. The CLI's own build info can't
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
	gorm.io/gorm %s
)

// The CLI this app's own gate runs: make budget, test, perf and
// generate-check. It is
// declared as a tool so that go mod tidy records the CLI's build
// dependencies in go.sum alongside the app's own. Nothing an app
// imports reaches, say, the TOML parser behind manifest, so without
// this line tidy prunes it and the first make ci on a clean scaffold
// dies with "missing go.sum entry for github.com/BurntSushi/toml" —
// a dependency that is real at go run time and invisible at tidy
// time. Deleting this line breaks the gate, not the app.
tool amadan.net/rastrillo/rastrillo/cmd/rastrillo
`

const mainTemplate = `// Command %[1]s wires the app: resolve the platform's activation
// argv, open the database, build the app, serve.
package main

import (
	"log/slog"
	"os"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/db"

	genlocales "%[1]s/gen/locales"
	%[2]s "%[1]s/internal/%[2]s"
)

func main() {
	// First, so perf's cold budget counts the whole start: a hibernated
	// instance pays all of it on someone's click.
	started := time.Now()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Resolve, not Run: this app opens its own database handle via
	// db.Open — a *gorm.DB with a split reader/writer pool, not the
	// bare *sql.DB Options.Router would hand back — so Options.DBPath
	// must be empty again by the Serve call below, or Serve would open
	// a second, unused connection to the same file.
	opts, err := rastrillo.Resolve(rastrillo.Options{
		DBPath: "%[1]s.db",
		Logger: logger,
		// Locales and DefaultLocale come from manifest/app.toml, through
		// gen/locales. Boot refuses a configuration whose packs this
		// binary does not link.
		Locales:       genlocales.Locales,
		DefaultLocale: genlocales.DefaultLocale,
	})
	if err != nil {
		logger.Error("resolve activation", "err", err)
		os.Exit(1)
	}

	d, err := db.Open(opts.DBPath, logger)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	// Origin decides the CSRF same-origin check (and, when the app
	// grows accounts, the Secure/__Host- cookie attributes) — so
	// defaulting it silently in production would be a real bug. Loud
	// on purpose, harmless in local dev.
	origin := os.Getenv("%[3]s_ORIGIN")
	if origin == "" {
		origin = "http://localhost:8080"
		logger.Warn("%[3]s_ORIGIN not set; defaulting", "origin", origin)
	}

	mux, err := %[2]s.App(d, origin, logger)
	if err != nil {
		logger.Error("build app", "err", err)
		os.Exit(1)
	}

	// Configure sets Mux, ErrorPage (wire render.go's ErrorPage to
	// Ctx.ErrorPage too as the app grows handlers) and perf's timing,
	// the same options the perf test measures through.
	%[2]s.Configure(&opts, mux, started)
	opts.DBPath = ""
	if err := rastrillo.Serve(opts); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
`

// appTOMLTemplate's comment is Appendix C.6's scaffold.app_toml.
const appTOMLTemplate = `# The locales this app offers. Add a code to offer another language,
# then run rastrillo generate; with two or more, URLs gain /xx/
# prefixes and pages a language switcher.
locales = [%[1]q]
default_locale = %[1]q
`

const appTemplate = `package %[1]s

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/perf"

	// Links the formatting packs manifest/app.toml declares. It is
	// here, not in main, so the app's tests link them too: boot refuses
	// a configuration whose packs are missing.
	_ "%[3]s/gen/locales"
)

// App wires the whole app: schema, router, JSON API, static files. It
// returns a *http.ServeMux because rastrillo.Options.Mux is typed that
// way — the router mounts inside it. Router and Mux are its two
// halves, separate only so the perf test can list the routes while
// measuring through the same handler production serves. origin is the app's external origin
// ("https://app.example.com") — the CSRF check's yardstick, and, once
// the app grows accounts, sessions.Config.Origin too.
//
// Growing the app is SKILL.md's five-file shape: models in models.go,
// handlers in handlers.go, and — for a multi-user app — the sessions
// core plus an identity plugin wired right here (examples/notes is the
// worked example to copy — it lives in the repository at
// amadan.net/rastrillo/rastrillo, not in the module you depend on, so
// read it there rather than looking for the directory). Adding a subsystem
// also means adding its Schema to migrations.go's BootSchema — see
// the comment there. Past 5,000 lines here, split by feature: a package
// per feature under internal/%[1]s/, each with Mount(r, deps) called from
// Router, registering through r.Handle (docs/site/testing.md).
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
	r, err := Router(d, origin, logger)
	if err != nil {
		return nil, err
	}
	apiTree, err := API(d, logger)
	if err != nil {
		return nil, err
	}
	return Mux(r, apiTree), nil
}

// Router applies the schema and builds the routes from the route table.
func Router(d *db.DB, origin string, logger *slog.Logger) (*Routes, error) {
	// pagesErr is render.go's: a template that does not parse, or a
	// locale configuration boot refuses. Either stops the app here,
	// before any migration runs, not at the first request.
	if pagesErr != nil {
		return nil, pagesErr
	}
	// Apply BootSchema, not Schema: BootSchema is everything this
	// app needs at boot (its own migrations plus any subsystem's),
	// while Schema (migrations.go) stays just this app's own so the
	// generator and its check never see a subsystem's tables and try
	// to drop them.
	migrated, err := migrate.Apply(context.Background(), d, BootSchema)
	if err != nil {
		return nil, err
	}
	// One line per boot. Result exists so this can be logged, and
	// until something does, a fleet-wide adoption — every database
	// stamped, no DDL run, exactly once, never again — produces no
	// output at all, which is the last event you want to be silent.
	logger.Info("migrate", "applied", migrated.Applied,
		"skipped", migrated.Skipped, "adopted", migrated.Adopted)

	a := &app{db: d, logger: logger}
` + stdlibRouterTail + routesType + `
// route is one entry in the route table: a full ServeMux pattern,
// method included, and its handler.
type route struct {
	Pattern string
	Handler http.HandlerFunc
}

` + stdlibRoutesDoc + `func (a *app) routes() []route {
	return []route{
		{"GET /{$}", a.index},%[2]s
	}
}

// Mux mounts the static files and the JSON API (api.go) beside the router.
func Mux(r, apiTree http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	// The app serves its own static files — the framework never does.
	// They are embedded (render.go) and fingerprinted: templates link
	// them via {{asset ...}}, which returns a content-hashed URL the
	// handler serves cacheable-forever.
	mux.Handle("GET /static/", assets.Handler())
	h := pages.Middleware(r)
	// Without pages, every request gets ErrorPage's plain 500 instead of
	// a panic in the middleware. App never builds a Mux like this, since
	// Router fails first; a caller that builds one directly, such as a
	// test, does.
	if pagesErr != nil {
		h = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { ErrorPage(w, req, http.StatusInternalServerError, "") })
	}
	mux.Handle("/", h)
	// Outside the router on purpose: no CSRF check and no page middleware.
	// A bearer token is the API's only credential, so a page cannot make
	// it act with the browser's cookies. Both patterns, so a bare /api/v1
	// gets the API's JSON 404 rather than the mux's own redirect.
	mux.Handle("/api/v1", apiTree)
	mux.Handle("/api/v1/", apiTree)
	return mux
}

// Configure is the one place the serving options are set, so main.go and
// the perf test cannot drift apart: what the perf lane measures is what
// production serves.
//
// ErrorPage is the page a handler's own error path renders, so a panic
// and a handled failure look identical. perf.Middleware times every
// request against 150ms to first byte (500ms for the first one after
// start, counted from started) and logs what is over budget.
func Configure(opts *rastrillo.Options, mux *http.ServeMux, started time.Time) {
	opts.Mux = mux
	opts.ErrorPage = ErrorPage
	var rec perf.Recorder
	opts.Wrap = perf.Middleware(&rec, perf.Options{Started: started, Logger: opts.Logger})
}
`

// stdlibRouterTail, routesType and stdlibRoutesDoc are the parts of
// appTemplate that are a ServeMux's: applyMagicAuth swaps exactly these
// for chi's, so they are named once.
const stdlibRouterTail = `
	// App-wide, from day one: every state-changing route on r is born
	// covered. Origin-checking, not tokens — nothing to mint or forget
	// in a form.
	r := &Routes{mux: http.NewServeMux()}
	r.h = csrf.Protect(origin)(r.mux)
	for _, rt := range a.routes() {
		r.HandleFunc(rt.Pattern, rt.Handler)
	}
	return r, nil
}
`

const routesType = `
// Routes is the app's router: a ServeMux that records the GET routes
// registered on it. A ServeMux cannot list what it serves, so every
// route goes through Handle, from the table below, tokens.go or a
// feature's Mount, and the perf lane measures what GETPatterns says.
// A route put on a ServeMux of its own would be served and never
// measured.
type Routes struct {
	mux  *http.ServeMux
	h    http.Handler // mux behind the CSRF check
	gets []string
}

// Handle registers h, and records pattern's path when it serves GET:
// "GET /x", or "/x" with no method, which serves every method.
func (r *Routes) Handle(pattern string, h http.Handler) {
	r.mux.Handle(pattern, h)
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		r.gets = append(r.gets, method)
	} else if method == http.MethodGet {
		r.gets = append(r.gets, strings.TrimSpace(path))
	}
}

func (r *Routes) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle(pattern, h) }

func (r *Routes) ServeHTTP(w http.ResponseWriter, req *http.Request) { r.h.ServeHTTP(w, req) }

// GETPatterns is every GET route registered so far, without the method:
// the form perftest.ScreenConfig.Patterns takes.
func (r *Routes) GETPatterns() []string { return append([]string(nil), r.gets...) }
`

const stdlibRoutesDoc = `// routes is the app's own route table, registered by Router.
//
// The index is "GET /{$}", not "GET /": a bare "/" matches every path,
// so an unknown URL would get the home page instead of a 404.
`

const modelsTemplate = `package %[1]s

import "time"

// Models is every model the schema generator manages. Keep it in step
// with the structs below: ` + "`rastrillo migration generate`" + ` reads
// it to work out what the database should look like, and
// TestModelsMatchMigrations (migrations_test.go) fails when it and the
// migrations disagree.
var Models = []any{
	&Note{},
}

// Note is a starting example model — rename it, replace it, add more
// beside it. Whatever it becomes, the rule stays the same: every query
// touching user-owned rows goes through scope.Owned (SKILL.md) — reads
// AND writes — and a request body is never bound onto a model directly
// (explicit map[string]any + .Select allowlist).
type Note struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
`

// migrationsTemplate declares the two schema variables an app needs,
// and only two: see the doc comments below for why one is not enough.
const migrationsTemplate = `package %[1]s

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is this app's own migrations, in order — nothing else. It is
// what ` + "`rastrillo migration generate`" + ` and ` + "`rastrillo migration check`" + ` read
// and write: both work by replaying a set into an in-memory database
// and diffing the result against Models, so Schema must never carry a
// framework subsystem's migrations too — mixed in, the diff would see
// tables (sessions, auth_links, ...) that Models knows nothing about
// and want to drop every one of them.
//
// Add to it with ` + "`rastrillo migration generate`" + ` after changing a
// model, or ` + "`rastrillo migration new <name>`" + ` to write one by hand.
// Migrations apply at boot and are never re-run or reversed: never
// edit one that has shipped, add a new one instead.
var Schema = migrate.MustFromFS(migrationFS, %[1]q)

// BootSchema is everything applied at boot, in order — this app's own
// Schema plus every framework subsystem's — and it is what App() below
// hands to migrate.Apply. It is a separate variable from Schema for
// the same reason Schema must stay narrow: generate and check would
// misread a subsystem's tables as ones Models forgot.
//
// A fresh scaffold uses one subsystem, apitoken, whose table the JSON
// API reads on every request (api.go). Adding another (say, sessions)
// means editing this line, not Schema:
//
//	var BootSchema = migrate.Merge(sessions.Schema, apitoken.Schema, Schema)
//
// Merge's argument order is apply order, so a subsystem whose
// migrations another one reads (auth after sessions) goes first.
// Get this line wrong and either boot silently skips a subsystem's
// tables, or generate/check try to drop them.
var BootSchema = migrate.Merge(apitoken.Schema, Schema)
`

// initMigrationTemplate is the scaffold's first migration: static,
// not generated at `new` time (see the files map's comment on why),
// but produced the same way `rastrillo migration generate` would
// produce it — by running the generator once, by hand, against the
// Note model above, and pasting the result here. Written verbatim by
// migrationGenerate's own body-building loop:
// strings.TrimSpace(sql) + "\n\n" per statement.
const initMigrationTemplate = "CREATE TABLE `notes` (`id` integer PRIMARY KEY AUTOINCREMENT,`title` text,`body` text,`created_at` datetime,`updated_at` datetime);\n\n"

// schemaSQLTemplate is the snapshot `rastrillo migration generate`
// would write alongside 0001_init.sql above — the schema every
// migration in the directory adds up to. Kept in the same format
// migrate.SchemaSQL produces so a diff against a real regeneration is
// silent.
const schemaSQLTemplate = `-- Generated by rastrillo migration generate; DO NOT EDIT.
-- The schema every migration in this directory adds up to.

CREATE TABLE ` + "`notes`" + ` (` + "`id`" + ` integer PRIMARY KEY AUTOINCREMENT,` + "`title`" + ` text,` + "`body`" + ` text,` + "`created_at`" + ` datetime,` + "`updated_at`" + ` datetime);

`

const handlersTemplate = `package %[1]s

import (
	"log/slog"
	"net/http"

	"amadan.net/rastrillo/rastrillo/db"
)

// app holds what every handler needs. When the app grows accounts,
// this is where the sessions handle lands too.
type app struct {
	db     *db.DB
	logger *slog.Logger
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	render(w, r, "index", nil)
}
%[2]s`

const renderTemplate = `package %[2]s

import (
	"bytes"
	"embed"
	"html/template"
	"maps"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"

	genlocales "%[1]s/gen/locales"
	"%[1]s/internal/%[2]s/icons"
)

//go:embed templates static
var appFS embed.FS

// assets fingerprints static/: {{asset "static/tokens.css"}} in a
// template returns a content-hashed URL, served immutable-cacheable by
// the handler App mounts.
var assets = rastrillo.NewAssets(appFS)

// pages is every page of this app, built once at boot: ui's partials and
// layout.html parsed once, then one copy per page with that page's own
// file. Every language and every request runs these same templates; the
// localizer in each page's data decides the language.
var pages, pagesErr = newPages()

func newPages() (*ui.Pages, error) {
	cfg, err := locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)
	if err != nil {
		return nil, err
	}
	files := map[string]ui.PageFile{}
	for _, name := range []string{%[3]s} {
		files[name] = ui.PageFile{Path: "templates/" + name + ".html"}
	}
	return ui.NewPages(ui.PagesConfig{
		App:    appFS,
		Layout: "templates/layout.html",
		Pages:  files,
		Locale: cfg,
		// icon and iconAssets point ui's two icon seams at the app's own set
		// (internal/%[2]s/icons, scaffolded by rastrillo new). Pass both, always:
		// passing only icon would leave iconAssets on the framework default and
		// silently break cdn and js delivery. iconAssets is whatever <head>
		// markup the chosen delivery needs, empty for the vendored-inline
		// default, so the layout calls it unconditionally and switching
		// delivery later needs no template edit.
		Funcs: template.FuncMap{"icon": icons.Icon, "iconAssets": icons.Assets, "asset": assets.Path},
	})
}

// render adds the request's localizer, the language switcher's items,
// the current path and the colour scheme the cookie chose to data, then
// executes name into a buffer before anything touches the wire: a
// template error becomes a clean 500 instead of half a page. Scheme is
// what the layout writes as data-theme; without it a chosen Dark would
// paint System on every reload.
func render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	req := ui.ForRequest(r)
	page := map[string]any{}
	maps.Copy(page, data)
	page["L"], page["Locales"], page["Return"], page["Scheme"] = req.L, req.Locales, req.Return, req.Scheme
	if err := pages.Execute(w, name, page); err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
	}
}

// ErrorPage renders the "errors" page — templates/errors.html, which
// wraps ui's error-page partial in the layout — for status inside ref
// (rastrillo.NewRef, or empty when there is none to show). It is the
// rastrillo.ErrorPageFunc cmd/%[1]s/main.go points Options.ErrorPage at,
// so a panic gets the same page a handled failure would; wire it to
// Ctx.ErrorPage too as the app grows handlers of its own. It finds the
// request's language with pages.LocalizerFor rather than the middleware,
// because a recovered panic reaches it outside the middleware.
func ErrorPage(w http.ResponseWriter, r *http.Request, status int, ref string) {
	// Without pages there is nothing to render, so a failed boot still
	// answers a plain 500 rather than panicking.
	var buf bytes.Buffer
	if pagesErr != nil || pages.Execute(&buf, "errors", map[string]any{"L": pages.LocalizerFor(r), "Scheme": rastrillo.SchemeFrom(r), "Status": status, "Ref": ref}) != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	buf.WriteTo(w)
}
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
// the phone index's heading is the brand, and the layout's default
// brand is the word Home, which on a phone would title the app "Home".
const sectionsIndexTemplate = `{{/* index.html: the home page, and on a phone the list of
     sections: the view block says this page is the index, so below
     800px the rail is the whole page and this content is hidden; on a
     desktop the rail sits beside it. Give each section a nav link with
     an id here, and give its page an up block naming /#<that id>, so a
     phone comes back to the row it left. */}}
{{define "view"}}index{{end}}
{{define "brand"}}<a rst-shell-brand href="/">%[2]s</a>{{end}}
{{define "nav"}}<a id="nav-overview" href="/overview">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// overviewTemplate is templates/overview.html, the one section page,
// with the same two arguments: a block is per page here, so every page
// names the brand.
const overviewTemplate = `{{/* overview.html: a section. up is its way back: the index, with
     this section's nav link as the fragment, which is where focus
     returns on a phone even with scripts off. */}}
{{define "up"}}/#nav-overview{{end}}
{{define "brand"}}<a rst-shell-brand href="/">%[2]s</a>{{end}}
{{define "nav"}}<a id="nav-overview" href="/overview" aria-current="page">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// overviewHandler is appended to handlers.go for the two-page shells.
const overviewHandler = `
func (a *app) overview(w http.ResponseWriter, r *http.Request) {
	render(w, r, "overview", nil)
}
`

// overviewTest is appended to index_test.go for the two-page shells: the
// section page renders, and its back control goes to the index's row.
const overviewTest = `
func TestOverviewRenders(t *testing.T) {
	rec := get(t, newApp(t), "/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /overview: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), ` + "`" + `href="/#nav-overview" rel="up"` + "`" + `) {
		t.Errorf("GET /overview: no back control to the index's row:\n%s", rec.Body.String())
	}
}
`

// errorsTemplate is templates/errors.html: the whole page is ui's
// error-page partial (dict builds its one data value inline — no
// per-page view-model type for a two-field page), so restyling the
// error page or adding a Body/BackHref override means editing this one
// file, not the partial ui ships.
const errorsTemplate = `{{/* errors.html — the app's error page: layout.html's content block
     rendered by render.go's ErrorPage, which cmd/<app>/main.go points
     Options.ErrorPage at (so a panic gets this page) and which the app
     can wire to Ctx.ErrorPage too as handlers grow. All the wording
     and layout live in the partial below — restyle the error page by
     editing this file, or hand it Title/Body/HomeHref/BackHref to
     override the partial's own catalog defaults; see
     ui/partials/error-page.html for what it accepts. */}}
{{define "content"}}{{$L := localizer $}}{{template "error-page" dict "L" $L "Status" .Status "Ref" .Ref}}{{end}}
`

// harnessTemplate is the scaffolded test harness, delivered once as
// app-owned files: it builds the whole app exactly as
// cmd/<name>/main.go does — a real temp-file database through db.Open
// (the split-pool configuration under test, never :memory:), the real
// router, the real asset handler — so a test exercises the app, not a
// parallel universe.
const harnessTemplate = `// Package %[2]s_test tests the app from the outside: real HTTP
// requests against the real router, wired exactly as
// cmd/%[1]s/main.go wires it.
//
// The TDD loop: write a failing test here, make it pass in
// internal/%[2]s, repeat.
package %[2]s_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/budget"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/dbtest"

	%[2]s "%[1]s/internal/%[2]s"
)

// testOrigin is the origin newApp wires and post presents as its
// same-origin evidence — the pair a browser form submission would be.
const testOrigin = "http://app.test"

// schema is migrated once per test binary and copied for each test:
// migrating a fresh database in every test is the cost Tito Go measured
// at 207ms a test against 2.5ms for a copy.
var schema = dbtest.FromSet(%[2]s.BootSchema)

// TestMain prints this package's time for rastrillo budget test (make
// test), then removes the template, which outlives every test that
// copied it.
func TestMain(m *testing.M) {
	code := budget.Main(m)
	schema.Remove()
	os.Exit(code)
}

// newApp builds the whole app per test over its own copy of the
// migrated database, exactly as main.go does.
func newApp(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatalf("db.Open: %%v", err)
	}
	t.Cleanup(func() { d.Close() })
	mux, err := %[2]s.App(d, testOrigin, logger)
	if err != nil {
		t.Fatalf("App: %%v", err)
	}
	return mux
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// post is here before anything calls it: the first form test you write
// needs the Origin header below, and nothing about a 403 says so.
//
//lint:ignore U1000 kept for the app's first form test; see above
func post(t *testing.T, h http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// The same-origin evidence csrf.Protect requires — every current
	// browser sends it on a form POST; a test client must too.
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
`

// indexTestTemplate is the scaffolded example suite: passing from the
// first `go test`, and pinning the out-of-the-box asset story — the
// index page links a fingerprinted stylesheet, that URL is immutable,
// the bare name stays fresh.
// vendoredTestTemplate pins the scaffold-delivered static files
// byte-identical to the library copies they came from — the same pin
// examples/blog carries for tokens.css. Vendored-then-forgotten is the
// known failure (tickets' stylesheet drifted for months), and without
// the pin a reviewer meets ~56KB of static assets as unverifiable app
// diff.
const vendoredTestTemplate = `package %[1]s_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/ui"
)

// vendoredTheme is the theme this app was scaffolded with. Change it
// here when you swap static/theme.css for another of ui.ThemeNames().
const vendoredTheme = "%[2]s"

// vendoredIsMine names the vendored files this app has edited or
// deleted DELIBERATELY. Add one here when you mean to diverge: this
// test stops pinning it, and rastrillo doctor leaves it alone too. The
// file was yours from the moment the scaffold wrote it; this is only
// where you record that the edit, or the deletion, was on purpose, so
// the next reader (and the next upgrade) can tell it from drift.
var vendoredIsMine = map[string]bool{
	// "tokens.css": true,
	//
	// A file you deleted goes here too, or this test fails on the
	// missing file. light.js is deletable on any shell (the still
	// design); only the sidebar and console shells link shell.js and
	// shell.css:
	// "light.js": true,
	// "shell.js": true, "shell.css": true,
}

// The scaffold delivered these files once; they are app-owned from
// then on. This test pins each vendored copy byte-identical to the
// library it came from: a reviewer runs the suite instead of reading
// ~56KB of assets as app diff, and a framework upgrade that forgets
// to re-copy is caught instead of drifting silently.
//
// The list is ui.VendoredAssets — the same function rastrillo new
// wrote these files from and rastrillo doctor compares them against —
// so a file added to the library's vendored set is pinned here without
// this file changing.
func TestVendoredAssetsMatchTheLibrary(t *testing.T) {
	t.Parallel()
	assets, ok := ui.VendoredAssets(vendoredTheme)
	if !ok {
		t.Fatalf("unknown theme %%q", vendoredTheme)
	}
	for name, lib := range assets {
		if vendoredIsMine[name] {
			continue
		}
		vendored, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Errorf("read vendored %%s: %%v; if you deleted it on purpose, add it to vendoredIsMine", name, err)
			continue
		}
		if !bytes.Equal(vendored, lib) {
			t.Errorf("static/%%s differs from the library copy; re-copy it with rastrillo doctor --fix (or add it to vendoredIsMine if the edit was deliberate)", name)
		}
	}
}
`

const indexTestTemplate = `package %[2]s_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"

	genlocales "%[1]s/gen/locales"
	%[2]s "%[1]s/internal/%[2]s"
)

// hashedStylesheet is the fingerprinted URL shape the index page
// links: /static/tokens.<16 hex>.css.
var hashedStylesheet = regexp.MustCompile(` + "`" + `/static/tokens\.[0-9a-f]{16}\.css` + "`" + `)

// These two tests are about the PLACEHOLDER index page, and they are
// yours to rewrite. They ship because a scaffold whose suite asserts
// nothing teaches nothing, not because "/" answering 200 with those
// words in it is a property of your app.
//
// The first feature that breaks them is sign-in, which is also the
// first feature most apps add: put "/" in a Group behind
// sessions.Require and a signed-out GET is a 303 to SigninPath, so
// the status assertion fails and the redirect's body carries neither
// the heading nor a stylesheet link. That is the app working. Point
// them at a page that is public in your app, or delete them — the
// rest of this file (the asset URLs, the error page) is about the
// framework's behaviour rather than the placeholder's, and keeps
// earning its place.
//
// Said in the failure messages too, and not only here. The reader who
// needs it is looking at a red gate, and the gate prints messages
// rather than source.
//
// Each test's first statement is t.Parallel(): anything before it runs
// serially, and a fixture built there is held for the whole run.
func TestIndexRenders(t *testing.T) {
	t.Parallel()
	rec := get(t, newApp(t), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status %%d, want 200. If you have put %%q behind sign-in this is the placeholder test, not a fault: a signed-out GET redirects, and this test is yours to repoint or delete", rec.Code, "/")
	}
	if !strings.Contains(rec.Body.String(), "this is a rastrillo app") {
		t.Errorf("GET /: page is missing its heading. If you have replaced the placeholder index this test is yours to rewrite:\n%%s", rec.Body.String())
	}
	skip := template.HTMLEscapeString(rastrillo.BaseCatalogs()[genlocales.DefaultLocale]["rastrillo.ui.shell_skip"])
	if !strings.Contains(rec.Body.String(), skip) {
		t.Errorf("GET /: page is missing its skip link in %%s, the default language:\n%%s", genlocales.DefaultLocale, rec.Body.String())
	}
}

func TestIndexLinksFingerprintedStylesheet(t *testing.T) {
	t.Parallel()
	h := newApp(t)
	href := hashedStylesheet.FindString(get(t, h, "/").Body.String())
	if href == "" {
		t.Fatal("index page links no fingerprinted stylesheet. If you have put \"/\" behind sign-in, the redirect body has no <link> in it and this test wants pointing at a page that is still public")
	}
	rec := get(t, h, href)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %%s: status %%d, want 200", href, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("GET %%s: Cache-Control = %%q, want immutable year", href, got)
	}
}

// The bare name keeps working, just never long-cached — so a changed
// file always shows on an ordinary reload.
func TestBareAssetNameStaysFresh(t *testing.T) {
	t.Parallel()
	rec := get(t, newApp(t), "/static/tokens.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/tokens.css: status %%d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %%q, want no-cache", got)
	}
}

// ErrorPage is what main.go points Options.ErrorPage at (and the seam
// to wire into Ctx.ErrorPage as handlers grow); called directly, it
// pins that a panic's page is the framework's error-page partial, not
// a stray net/http default.
func TestErrorPageRendersFrameworkPartial(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	%[2]s.ErrorPage(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusInternalServerError, "abc123")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ErrorPage: status %%d, want %%d", rec.Code, http.StatusInternalServerError)
	}
	body := rec.Body.String()
	if !strings.Contains(body, template.HTMLEscapeString(rastrillo.BaseCatalogs()[genlocales.DefaultLocale]["rastrillo.ui.error_500_title"])) {
		t.Errorf("ErrorPage body missing the 500 title:\n%%s", body)
	}
	if !strings.Contains(body, "abc123") {
		t.Errorf("ErrorPage body missing the ref:\n%%s", body)
	}
}

// The index is served at "GET /{$}" because a bare "GET /" matches
// every path: an unknown URL would get the home page, not a 404.
func TestUnknownPathIs404(t *testing.T) {
	t.Parallel()
	if rec := get(t, newApp(t), "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /nope: status %%d, want 404", rec.Code)
	}
}
%[3]s`

// browserTestTemplate is the scaffolded browser drive, delivered once
// like the harness: app-owned from here on. The existing
// harness_test.go is the httptest HTTP harness and keeps its name —
// the browser rig takes a different one everywhere (file, tag,
// invocation), because "harness" already means the HTTP one throughout
// the scaffold's prose.
const browserTestTemplate = `//go:build browser

// The browser drive: the whole app in a real Chromium, wired exactly
// as cmd/%[1]s/main.go wires it — loud on any console error, failed
// request, 4xx/5xx, or JS value that leaked to the screen unrendered.
// Not part of the plain suite:
//
//	go test -tags browser ./...
//
// It needs a Chromium: on PATH, via RASTRILLO_CHROME, or in a
// Playwright cache. A skip is not a pass — with no browser this fails,
// unless RASTRILLO_BROWSER_OPTIONAL is set, which makes the skip a
// deliberate visible choice rather than an accident.
package %[2]s_test

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/harness"
	"github.com/chromedp/chromedp"

	%[2]s "%[1]s/internal/%[2]s"
)

// TestBrowserWalk is the minimal loud walk: the home screen arrives
// with nothing wrong anywhere the rig watches. Grow it with the app —
// each screen a flow reaches earns a Screen() line, and an expected
// probe (a signed-out 401, say) earns a rig.Allow.
func TestBrowserWalk(t *testing.T) {
	rig := harness.New(t, func(origin string) http.Handler {
		// The scaffold's own wiring, origin included: the rig hands
		// the app its localhost origin before serving — the same seam
		// main.go fills from %[3]s_ORIGIN.
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), logger)
		if err != nil {
			t.Fatalf("db.Open: %%v", err)
		}
		t.Cleanup(func() { d.Close() })
		mux, err := %[2]s.App(d, origin, logger)
		if err != nil {
			t.Fatalf("App: %%v", err)
		}
		return mux
	})
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("body", "home")
}
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

// makefileTemplate is the one gate definition: CI steps exec these
// targets, never their own copies of the commands (amadan's own rule).
var makefileTemplate = scaffoldFile("Makefile.tmpl")

const gitignoreTemplate = `# Build output. make release writes here; nothing in it is source.
/releases/

# The local SQLite database the app creates when you run it, and its
# write-ahead log. Real data lives on the platform, never in the repo.
/%[1]s.db
/%[1]s.db-wal
/%[1]s.db-shm
`

const amadanCI = `#!/bin/sh
# amadan CI entry (single-script fallback for runners without step
# support). The steps in ci.d/ are the same targets, reported one by
# one. Must stay executable: a non-executable script resolves "skipped".
set -e
exec make ci
`

// amadanStep emits one .amadan/ci.d/ step: exec a Makefile target, so
// CI and the local gate stay one definition.
func amadanStep(target string) string {
	return "#!/bin/sh\nexec make " + target + "\n"
}
