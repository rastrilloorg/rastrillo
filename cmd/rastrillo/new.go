package main

import (
	"flag"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/carlosframework/rastrillo/internal/iconsets"
	"github.com/carlosframework/rastrillo/ui"
)

// runNew implements `rastrillo new <name>`: the middle-layer app shape
// (SKILL.md §1) — GORM models, a chi router, embedded templates, and a
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
	if err := fset.Parse(args); err != nil {
		return err
	}
	rest := fset.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: rastrillo new [--icons=%s] [--icon-delivery=%s] [--ux=%s] <name>",
			strings.Join(iconsets.Names(), "|"), strings.Join(iconsets.Deliveries(), "|"),
			strings.Join(profileNames(), "|"))
	}
	name := rest[0]
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

	pkg := packageName(name)
	dirs := []string{
		name,
		filepath.Join(name, "cmd", name),
		filepath.Join(name, "manifest"),
		filepath.Join(name, "internal", pkg),
		filepath.Join(name, "internal", pkg, "static"),
		filepath.Join(name, "internal", pkg, "templates"),
		filepath.Join(name, "internal", pkg, "migrations"),
		filepath.Join(name, "internal", pkg, "icons"),
		filepath.Join(name, "internal", pkg+"test"),
		filepath.Join(name, ".amadan", "ci.d"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	appDir := filepath.Join(name, "internal", pkg)
	files := map[string]string{
		filepath.Join(name, "go.mod"): fmt.Sprintf(goModTemplate,
			name, rastrilloVersion(), chiPinnedVersion, gormPinnedVersion),
		filepath.Join(name, "cmd", name, "main.go"):       fmt.Sprintf(mainTemplate, name, pkg, strings.ToUpper(pkg)),
		filepath.Join(appDir, "app.go"):                   fmt.Sprintf(appTemplate, pkg),
		filepath.Join(appDir, "models.go"):                fmt.Sprintf(modelsTemplate, pkg),
		filepath.Join(appDir, "migrations.go"):            fmt.Sprintf(migrationsTemplate, pkg),
		filepath.Join(appDir, "handlers.go"):              fmt.Sprintf(handlersTemplate, pkg),
		filepath.Join(appDir, "render.go"):                fmt.Sprintf(renderTemplate, name, pkg),
		filepath.Join(appDir, "templates", "layout.html"): layoutTemplate,
		filepath.Join(appDir, "templates", "index.html"):  indexTemplate,
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
		// The design-token stylesheet, delivered once. rastrillo.Serve
		// never serves CSS at runtime; from here on this is an ordinary
		// app-owned file that new/generate never touch again.
		filepath.Join(appDir, "static", "tokens.css"): string(ui.TokensCSS()),
		// The fragment shim, delivered once like tokens.css: app-owned
		// from here on, loaded by the layout via the same fingerprinting
		// {{asset ...}} helper.
		filepath.Join(appDir, "static", "rastrillo.js"): string(ui.ShimJS()),
		// field-select's searchable enhancement, on the same terms.
		// Inert until a select opts in with data-rst-select, which
		// field-select emits past ten options — so an app that never has
		// a select that big can delete this and its script tag.
		filepath.Join(appDir, "static", "select.js"): string(ui.SelectJS()),
		// The test harness, delivered once like tokens.css: app-owned
		// from here on — edit it, grow it, or delete it. The example
		// tests pass on a fresh scaffold and pin the out-of-the-box
		// asset-fingerprinting behavior.
		filepath.Join(name, "internal", pkg+"test", "harness_test.go"): fmt.Sprintf(harnessTemplate, name, pkg),
		// The pin that makes the vendored bytes above verifiable: a
		// reviewer runs the suite and knows static/'s ~56KB is the
		// library's, not app diff to read line by line.
		filepath.Join(name, "internal", pkg+"test", "vendored_test.go"): fmt.Sprintf(vendoredTestTemplate, pkg),
		filepath.Join(name, "internal", pkg+"test", "index_test.go"):    fmt.Sprintf(indexTestTemplate, name, pkg),
		// The browser drive, on the same delivered-once terms as the
		// harness. browser_test.go, never harness_test.go: that name
		// (and word) already belongs to the httptest HTTP harness.
		filepath.Join(name, "internal", pkg+"test", "browser_test.go"): fmt.Sprintf(browserTestTemplate, name, pkg, strings.ToUpper(pkg)),
		filepath.Join(name, "README.md"):                               fmt.Sprintf(readmeTemplate, name, pkg),
		filepath.Join(name, "manifest", "README.md"):                   fmt.Sprintf(manifestReadme, name, pkg),
		filepath.Join(name, "Makefile"):                                fmt.Sprintf(makefileTemplate, name),
		filepath.Join(name, ".gitignore"):                              fmt.Sprintf(gitignoreTemplate, name),
		// The app's icon set, on the same terms as tokens.css and
		// rastrillo.js: delivered once, app-owned from here on.
		filepath.Join(appDir, "icons", "icons.go"): string(rendered.Source),
		// AGENTS.md carries the instructions — it is the cross-agent
		// file, so they reach whatever agent someone uses. CLAUDE.md is
		// an @AGENTS.md import and nothing else.
		filepath.Join(name, "AGENTS.md"): fmt.Sprintf(agentsMDTemplate, name) + string(conventions),
		filepath.Join(name, "CLAUDE.md"): claudeMDPointer,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// CI scripts must be executable: amadan's runner resolves a
	// non-executable job "skipped" with only a hint — the known silent
	// failure mode, closed at scaffold time.
	ciScripts := map[string]string{
		filepath.Join(name, ".amadan", "ci"):                         amadanCI,
		filepath.Join(name, ".amadan", "ci.d", "10-vet"):             amadanStep("vet"),
		filepath.Join(name, ".amadan", "ci.d", "20-fmt"):             amadanStep("fmt-check"),
		filepath.Join(name, ".amadan", "ci.d", "30-test"):            amadanStep("test"),
		filepath.Join(name, ".amadan", "ci.d", "40-migration-check"): amadanStep("migration-check"),
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
	fmt.Printf("  internal/%stest/     (harness + example tests, passing out of the box;\n", pkg)
	fmt.Println("                        browser_test.go = the browser drive, go test -tags browser ./...)")
	fmt.Println("  manifest/            (the declarative path: drop a <name>.toml here, see its README)")
	fmt.Println("  Makefile             (make ci = vet + fmt + test + migration check, the one gate definition;")
	fmt.Println("                        make release = the stripped binary)")
	fmt.Println("  .gitignore           (build output and the local database)")
	fmt.Println("  .amadan/ci, ci.d/    (amadan runner CI, executable, delegating to make)")
	fmt.Printf("  internal/%s/icons/   (%s, %s — app-owned, edit freely)\n", pkg, *iconSet, *iconDelivery)
	fmt.Println("  AGENTS.md            (instructions + UX conventions, the source of truth)")
	fmt.Println("  CLAUDE.md            (an @AGENTS.md import, nothing more)")
	fmt.Println("  README.md            (what this app is, and how the browser drive runs)")
	if rendered.AttribName != "" {
		fmt.Printf("  %s   (%s requires attribution — keep it)\n", rendered.AttribName, *iconSet)
	}

	// Informed consent, stated once. A supported choice that nags on
	// every build is not really supported.
	if rendered.Notice != "" {
		fmt.Printf("\nnote: --icon-delivery=%s\n", *iconDelivery)
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
	github.com/carlosframework/rastrillo %s
	github.com/go-chi/chi/v5 %s
	gorm.io/gorm %s
)
`

const mainTemplate = `// Command %[1]s wires the app: resolve the platform's activation
// argv, open the database, build the app, serve.
package main

import (
	"log/slog"
	"os"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/db"

	%[2]s "%[1]s/internal/%[2]s"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Resolve, not Run: this app opens its own database handle via
	// db.Open — a *gorm.DB with a split reader/writer pool, not the
	// bare *sql.DB Options.Router would hand back — so Options.DBPath
	// must be empty again by the Serve call below, or Serve would open
	// a second, unused connection to the same file.
	opts, err := rastrillo.Resolve(rastrillo.Options{DBPath: "%[1]s.db", Logger: logger})
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

	opts.Mux = mux
	opts.DBPath = ""
	if err := rastrillo.Serve(opts); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
`

const appTemplate = `package %[1]s

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/carlosframework/rastrillo/csrf"
	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/migrate"
)

// App wires the whole app: schema, router, static files. It returns a
// *http.ServeMux because rastrillo.Options.Mux is typed that way — the
// chi router mounts inside it. origin is the app's external origin
// ("https://app.example.com") — the CSRF check's yardstick, and, once
// the app grows accounts, sessions.Config.Origin too.
//
// Growing the app is SKILL.md's five-file shape: models in models.go,
// handlers in handlers.go, and — for a multi-user app — the sessions
// core plus an identity plugin wired right here (examples/notes in the
// rastrillo repo is the worked example to copy). Adding a subsystem
// also means adding its Schema to migrations.go's BootSchema — see
// the comment there.
func App(d *db.DB, origin string, logger *slog.Logger) (*http.ServeMux, error) {
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

	r := chi.NewRouter()
	// App-wide, from day one: every state-changing route added below
	// is born covered. Origin-checking, not tokens — nothing to mint
	// or forget in a form.
	r.Use(csrf.Protect(origin))
	r.Get("/", a.index)

	mux := http.NewServeMux()
	// The app serves its own static files — the framework never does.
	// They are embedded (render.go) and fingerprinted: templates link
	// them via {{asset ...}}, which returns a content-hashed URL the
	// handler serves cacheable-forever.
	mux.Handle("GET /static/", assets.Handler())
	mux.Handle("/", r)
	return mux, nil
}
`

const modelsTemplate = `package %[1]s

import "time"

// Models is every model the schema generator manages. Keep it in step
// with the structs below: ` + "`rastrillo migration generate`" + ` reads
// it to work out what the database should look like, and
// ` + "`rastrillo migration check`" + ` fails CI when it and the
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

	"github.com/carlosframework/rastrillo/migrate"
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
// A fresh scaffold uses no subsystem yet, so today this is just
// Schema. Adding one (say, sessions) means editing this line, not
// Schema:
//
//	var BootSchema = migrate.Merge(sessions.Schema, Schema)
//
// Merge's argument order is apply order, so a subsystem whose
// migrations another one reads (auth after sessions) goes first.
// Get this line wrong and either boot silently skips a subsystem's
// tables, or generate/check try to drop them.
var BootSchema = migrate.Merge(Schema)
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

	"github.com/carlosframework/rastrillo/db"
)

// app holds what every handler needs. When the app grows accounts,
// this is where the sessions handle lands too.
type app struct {
	db     *db.DB
	logger *slog.Logger
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	render(w, "index", nil)
}
`

const renderTemplate = `package %[2]s

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"

	"github.com/carlosframework/rastrillo"

	"%[1]s/internal/%[2]s/icons"
)

//go:embed templates static
var appFS embed.FS

// assets fingerprints static/: {{asset "static/tokens.css"}} in a
// template returns a content-hashed URL, served immutable-cacheable by
// the handler App mounts.
var assets = rastrillo.NewAssets(appFS)

// pages is one *template.Template per page, each combining layout.html
// with that page's own file — kept separate so every page can define a
// template named "content" without colliding with the others.
var pages = map[string]*template.Template{}

func init() {
	for _, name := range []string{"index"} {
		pages[name] = template.Must(template.New("layout").
			Funcs(template.FuncMap{
				"asset": assets.Path,
				// The app's own icon set (internal/%[2]s/icons), scaffolded
				// by rastrillo new. iconAssets is whatever <head> markup the
				// chosen delivery needs — empty for the vendored-inline
				// default, so the layout calls it unconditionally and
				// switching delivery later needs no template edit.
				"icon":       icons.Icon,
				"iconAssets": icons.Assets,
			}).
			ParseFS(appFS, "templates/layout.html", "templates/"+name+".html"))
	}
}

// render executes name's layout into a buffer before anything touches
// the wire: a template error becomes a clean 500 instead of garbage
// appended to a half-written page, and headers (flash cookies, say)
// stay settable until the status line is written.
func render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	buf.WriteTo(w)
}
`

const layoutTemplate = `{{define "layout"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{block "title" .}}Hello{{end}}</title>
<link rel="stylesheet" href="{{asset "static/tokens.css"}}">
<script defer src="{{asset "static/rastrillo.js"}}"></script>
{{iconAssets}}
<script defer src="{{asset "static/select.js"}}"></script>
</head>
<body>
<main>
{{template "content" .}}
</main>
</body>
</html>
{{end}}
`

const indexTemplate = `{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// harnessTemplate is the scaffolded test harness, delivered once as
// app-owned files: it builds the whole app exactly as
// cmd/<name>/main.go does — a real temp-file database through db.Open
// (the split-pool configuration under test, never :memory:), the real
// router, the real asset handler — so a test exercises the app, not a
// parallel universe.
const harnessTemplate = `// Package %[2]stest tests the app from the outside: real HTTP
// requests against the real router, wired exactly as
// cmd/%[1]s/main.go wires it.
//
// The TDD loop: write a failing test here, make it pass in
// internal/%[2]s, repeat.
package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/db"

	%[2]s "%[1]s/internal/%[2]s"
)

// testOrigin is the origin newApp wires and post presents as its
// same-origin evidence — the pair a browser form submission would be.
const testOrigin = "http://app.test"

// newApp builds the whole app per test over a fresh temp database,
// exactly as main.go does.
func newApp(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), logger)
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
const vendoredTestTemplate = `package %[1]stest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// The scaffold delivered these files once; they are app-owned from
// then on. This test pins each vendored copy byte-identical to the
// library it came from: a reviewer runs the suite instead of reading
// ~56KB of assets as app diff, and a framework upgrade that forgets
// to re-copy is caught instead of drifting silently. If you edit one
// DELIBERATELY, delete its line below — the file is yours.
func TestVendoredAssetsMatchTheLibrary(t *testing.T) {
	for name, lib := range map[string][]byte{
		"tokens.css":   ui.TokensCSS(),
		"rastrillo.js": ui.ShimJS(),
		"select.js":    ui.SelectJS(),
	} {
		vendored, err := os.ReadFile(filepath.Join("..", "%[1]s", "static", name))
		if err != nil {
			t.Errorf("read vendored %%s: %%v", name, err)
			continue
		}
		if !bytes.Equal(vendored, lib) {
			t.Errorf("static/%%s differs from the library copy; re-copy it (or delete its pin if the edit was deliberate)", name)
		}
	}
}
`

const indexTestTemplate = `package %[2]stest

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// hashedStylesheet is the fingerprinted URL shape the index page
// links: /static/tokens.<16 hex>.css.
var hashedStylesheet = regexp.MustCompile(` + "`" + `/static/tokens\.[0-9a-f]{16}\.css` + "`" + `)

func TestIndexRenders(t *testing.T) {
	rec := get(t, newApp(t), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status %%d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "this is a rastrillo app") {
		t.Errorf("GET /: page is missing its heading:\n%%s", rec.Body.String())
	}
}

func TestIndexLinksFingerprintedStylesheet(t *testing.T) {
	h := newApp(t)
	href := hashedStylesheet.FindString(get(t, h, "/").Body.String())
	if href == "" {
		t.Fatal("index page links no fingerprinted stylesheet")
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
	rec := get(t, newApp(t), "/static/tokens.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/tokens.css: status %%d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %%q, want no-cache", got)
	}
}
`

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
package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/harness"
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

// readmeTemplate is the scaffolded app README. Small on purpose:
// AGENTS.md carries the conventions; this says what the app is and
// how the browser drive runs — including that CI participation is the
// app's own call.
const readmeTemplate = `# %[1]s

A [rastrillo](https://github.com/rastrilloorg/rastrillo) app. ` + "`make ci`" + `
is the gate — vet, gofmt, tests, migration check, one definition for
CI and for you. AGENTS.md carries the working conventions.

## Browser drive

` + "`internal/%[2]stest/browser_test.go`" + ` walks the app in a real
Chromium — loud on any console error, failed request, or JS value that
leaked to the screen:

` + "```sh" + `
go test -tags browser ./...   # real browser, loud on any console error
                              # — not part of the plain suite
` + "```" + `

It needs a Chromium: on PATH, via ` + "`RASTRILLO_CHROME`" + `, or in a
Playwright cache. **A skip is not a pass:** with no browser it fails,
unless ` + "`RASTRILLO_BROWSER_OPTIONAL=1`" + ` makes the skip a deliberate,
visible choice. ` + "`make ci`" + ` runs the plain suite only — whether CI
also runs the browser drive, and on what runner, is this app's call.
`

const manifestReadme = `# manifest/ — the declarative path

Manifest-driven and code-driven are equal, optional paths: declare the
resources that are pure CRUD, hand-write the ones that aren't, per
resource, in one app. One TOML file here declares a whole screen set —
List → Show → Edit/New plus the confirm-page delete flow — generated
as real committed code under gen/ by ` + "`rastrillo generate`" + `:

    name  = "ticket_types"
    route = "/admin/ticket_types"

    [list]
    columns = [{ field = "Name", kind = "text" }, { field = "Price", kind = "money" }]
    search  = true

    [form]
    basics = [{ name = "Name", required = true }, { name = "Price", kind = "money" }]

The vocabulary covers one flat table per resource, three field kinds,
no relations. Add ` + "`scope = \"user\"`" + ` and every generated query is
owner-filtered by the session subject — someone else's row answers
404 — for resources a user owns; mount those routes behind
` + "`sessions.Require`" + `. Add ` + "`store = \"mergeable\"`" + ` and the resource lives as
event-log streams (tombstone deletes, derive-on-read) instead of a
sqlc table — it needs no sqlc tool. Relations or a custom flow:
hand-write it.

Adding the first manifest to this app:

1. ` + "`go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc`" + ` (once — the
   generated store is sqlc input).
2. ` + "`rastrillo generate`" + ` writes gen/ — committed, never hand-edited.
3. Mount the generated router beside the chi router in
   internal/%[2]s/app.go, and wire its Ctx (the generated actions
   render through Ctx.Render — examples/blog's internal/blog/genrender.go
   in the rastrillo repo is the adapter to copy):

       gmux := gen.Router(func(*http.Request) *rastrillo.Ctx {
           writer, _ := d.G.DB()
           return &rastrillo.Ctx{DB: writer, Logger: logger,
               Actor: rastrillo.Actor{Human: true}, Render: render}
       })
       mux.Handle("/admin/", gmux)

4. Append the generated migrations to the schema step, and add
   ` + "`rastrillo generate --check`" + ` to the Makefile's ci target.

Drop to a .go manifest in this directory (package manifest, a
` + "`var X = rastrillo.Resource{...}`" + `) the moment you need a function
value — a custom Column.Render, say. To take any single generated
screen over by hand, write your own action file at the same computed
path under actions/ — the generator skips that one from then on.
`

// makefileTemplate is the one gate definition: CI steps exec these
// targets, never their own copies of the commands (amadan's own rule).
const makefileTemplate = `APP := %[1]s

# What ` + "`carlos ship -target`" + ` defaults to, and therefore what a release
# must be built for. Building for your own machine and shipping that is a
# silent architecture mismatch: the binary uploads fine and fails to exec
# on the instance. Override for a one-off:
#   make release RELEASE_GOARCH=amd64
RELEASE_GOOS   ?= linux
RELEASE_GOARCH ?= arm64
RELEASE_BIN := releases/$(APP)-$(RELEASE_GOOS)-$(RELEASE_GOARCH)

.PHONY: build release test vet fmt-check migration-check ci

# build is the compile check: with more than one package matched, go
# build discards the output, so this catches a broken package without
# producing an artifact.
build:
	CGO_ENABLED=0 go build ./...

# release is what ships. -s -w drops the symbol table and DWARF, which on
# this family's apps is 23-31%% off the binary and 45-53%% off the
# compressed artifact — and compressed size is what actually gets
# transferred. carlos is built the same way (see platform's Makefile).
#
# What survives: panic traces keep their function names AND line numbers
# (Go's pclntab is untouched by these flags), debug.ReadBuildInfo works,
# and go version -m still reports module metadata. What you lose is
# source-level debugging with delve or gdb — build without the flags for
# that. rastrillo dev never strips, for the same reason.
release:
	@mkdir -p releases
	CGO_ENABLED=0 GOOS=$(RELEASE_GOOS) GOARCH=$(RELEASE_GOARCH) \
		go build -ldflags="-s -w" -o $(RELEASE_BIN) ./cmd/$(APP)
	@echo
	@echo "built $(RELEASE_BIN)"
	@echo "ship it with:"
	@echo "  carlos ship -app $(APP) -target $(RELEASE_GOOS)-$(RELEASE_GOARCH) $(RELEASE_BIN)"

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# A step of its own, not folded into another target's recipe: ci.d/
# steps each exec one make target, so migration-check needs its own
# name to get its own reported step on a runner with step support.
#
# go run, not a bare "rastrillo": go.mod already requires rastrillo, so
# this needs nothing on PATH but the Go toolchain, and works offline
# after go mod download. A bare "rastrillo migration check" failed a
# fresh clone's very first CI run with "make: rastrillo: Command not
# found", and nothing in the scaffold said what to install.
migration-check:
	go run github.com/carlosframework/rastrillo/cmd/rastrillo migration check

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. amadan's runner falls back to this
# target when .amadan/ci is absent; ci.d/ carries the same targets,
# reported one by one, for a runner with step support. migration-check
# fails the build the moment models.go and migrations/ disagree,
# instead of at boot on whatever machine notices next. If the app
# declares manifest resources, also add: rastrillo generate --check
ci: vet fmt-check test migration-check
`

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

const agentsMDTemplate = `# %s

A [rastrillo](https://github.com/rastrilloorg/rastrillo) app. Read the
framework's own SKILL.md (repo root, or in the module cache) before
writing app code — it is the whole app story in ~15KB. The conventions
below are load-bearing; the framework enforces most of them
mechanically.

## Layout

- ` + "`internal/<app>/`" + ` — the app: ` + "`models.go`" + ` (plain GORM structs
  + Models), ` + "`migrations.go`" + ` + ` + "`migrations/`" + ` (Schema, BootSchema),
  ` + "`app.go`" + ` (migrate.Apply + chi router), ` + "`handlers.go`" + `,
  ` + "`render.go`" + ` + ` + "`templates/`" + `. This is SKILL.md's five-file shape.
- ` + "`manifest/`" + ` — the declarative path, optional and equal: TOML or .go
  Resource manifests, each generating a full screen set under ` + "`gen/`" + `
  (see manifest/README.md for the mounting recipe). A hand-written
  action file at a generated path (carrying ` + "`//go:build rastrillo_actions`" + `)
  takes that one screen over, by existence.
- ` + "`gen/`" + ` — generated when manifests are used; never edit. Regenerate
  with ` + "`rastrillo generate`" + `.

## Rules the family holds hard

- Every query touching user-owned rows goes through ` + "`scope.Owned`" + ` —
  reads AND writes, transactions included. A row that isn't yours 404s,
  never 403s.
- Never bind a request body onto a GORM model: explicit
  ` + "`map[string]any`" + ` + ` + "`.Select`" + ` allowlist.
- Migrations apply at boot, once each, and are never re-run or reversed.
- Schema changes: edit models.go, then run ` + "`rastrillo migration generate`" + `
  and read the SQL it writes — it may be a full table rebuild, not just
  an ` + "`ADD COLUMN`" + `. Never edit a migration that has shipped.
  Renames are hand-written (` + "`rastrillo migration new rename_x`" + `) because
  a rename is indistinguishable from a drop plus an add.
- Money is integer cents. A float never touches a value a person will
  be held to.
- Screens work with JavaScript disabled; destructive actions get their
  own confirm-page URL.
- The gate is ` + "`make ci`" + ` (vet + gofmt + tests) — the same definition
  CI runs. Run it before every push. ` + "`CGO_ENABLED=0`" + ` throughout: the
  stack is cgo-free by design.

## Serving

` + "`rastrillo.Resolve`" + ` + ` + "`Serve`" + ` speak the CARLOS activation contract
(socket/addr/db argv, LISTEN_FDS, $STATE_DIRECTORY, ` + "`sidecar run`" + `) —
the golden deployment target, not a requirement: ` + "`./app -addr :8080`" + `
serves anywhere. ` + "`GET /healthz`" + ` and ` + "`GET /api/version`" + ` are answered
by the framework, outside app middleware.
`
