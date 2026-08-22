package main

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

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
	if len(args) != 1 {
		return fmt.Errorf("usage: rastrillo new <name>")
	}
	name := args[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
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
		filepath.Join(name, "cmd", name, "main.go"):       fmt.Sprintf(mainTemplate, name, pkg),
		filepath.Join(appDir, "app.go"):                   fmt.Sprintf(appTemplate, pkg),
		filepath.Join(appDir, "models.go"):                fmt.Sprintf(modelsTemplate, pkg),
		filepath.Join(appDir, "migrations.go"):            fmt.Sprintf(migrationsTemplate, pkg),
		filepath.Join(appDir, "handlers.go"):              fmt.Sprintf(handlersTemplate, pkg),
		filepath.Join(appDir, "render.go"):                fmt.Sprintf(renderTemplate, pkg),
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
		// The test harness, delivered once like tokens.css: app-owned
		// from here on — edit it, grow it, or delete it. The example
		// tests pass on a fresh scaffold and pin the out-of-the-box
		// asset-fingerprinting behavior.
		filepath.Join(name, "internal", pkg+"test", "harness_test.go"): fmt.Sprintf(harnessTemplate, name, pkg),
		filepath.Join(name, "internal", pkg+"test", "index_test.go"):   fmt.Sprintf(indexTestTemplate, name, pkg),
		filepath.Join(name, "manifest", "README.md"):                   fmt.Sprintf(manifestReadme, name, pkg),
		filepath.Join(name, "Makefile"):                                makefileTemplate,
		filepath.Join(name, "CLAUDE.md"):                               fmt.Sprintf(claudeMDTemplate, name),
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
		filepath.Join(name, ".amadan", "ci"):              amadanCI,
		filepath.Join(name, ".amadan", "ci.d", "10-vet"):  amadanStep("vet"),
		filepath.Join(name, ".amadan", "ci.d", "20-fmt"):  amadanStep("fmt-check"),
		filepath.Join(name, ".amadan", "ci.d", "30-test"): amadanStep("test"),
	}
	for path, content := range ciScripts {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return err
		}
	}

	fmt.Printf("rastrillo new: scaffolded %s/\n", name)
	fmt.Printf("  cmd/%s/main.go       (Resolve -> db.Open -> App -> Serve)\n", name)
	fmt.Printf("  internal/%s/         (models, migrations, app, handlers, render, templates, static)\n", pkg)
	fmt.Printf("  internal/%stest/     (harness + example tests, passing out of the box)\n", pkg)
	fmt.Println("  manifest/            (the declarative path: drop a <name>.toml here, see its README)")
	fmt.Println("  Makefile             (make ci = vet + fmt + test, the one gate definition)")
	fmt.Println("  .amadan/ci, ci.d/    (amadan runner CI, executable, delegating to make)")
	fmt.Println("  CLAUDE.md")

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

	mux, err := %[2]s.App(d, logger)
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

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/migrate"
)

// App wires the whole app: schema, router, static files. It returns a
// *http.ServeMux because rastrillo.Options.Mux is typed that way — the
// chi router mounts inside it.
//
// Growing the app is SKILL.md's five-file shape: models in models.go,
// handlers in handlers.go, and — for a multi-user app — the sessions
// core plus an identity plugin wired right here (examples/notes in the
// rastrillo repo is the worked example to copy). Adding a subsystem
// also means adding its Schema to migrations.go's BootSchema — see
// the comment there.
func App(d *db.DB, logger *slog.Logger) (*http.ServeMux, error) {
	// Apply BootSchema, not Schema: BootSchema is everything this
	// app needs at boot (its own migrations plus any subsystem's),
	// while Schema (migrations.go) stays just this app's own so the
	// generator and its check never see a subsystem's tables and try
	// to drop them.
	if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil {
		return nil, err
	}

	a := &app{db: d, logger: logger}

	r := chi.NewRouter()
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

// schemaSQLTemplate is the snapshot ` + "`rastrillo migration generate`" + `
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

const renderTemplate = `package %[1]s

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"

	"github.com/carlosframework/rastrillo"
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
			Funcs(template.FuncMap{"asset": assets.Path}).
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
	mux, err := %[2]s.App(d, logger)
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
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
`

// indexTestTemplate is the scaffolded example suite: passing from the
// first `go test`, and pinning the out-of-the-box asset story — the
// index page links a fingerprinted stylesheet, that URL is immutable,
// the bare name stays fresh.
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
` + "`sessions.Require`" + `. Relations or a custom flow: hand-write it.

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
const makefileTemplate = `.PHONY: build test vet fmt-check ci

build:
	CGO_ENABLED=0 go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. amadan's runner falls back to this
# target when .amadan/ci is absent. rastrillo migration check fails
# the build the moment models.go and migrations/ disagree, instead of
# at boot on whatever machine notices next. If the app declares
# manifest resources, also add: rastrillo generate --check
ci: vet fmt-check test
	rastrillo migration check
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

const claudeMDTemplate = `# %s

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
- Migrations are additive-only, applied at boot. Never rewrite one that
  shipped.
- Schema changes: edit models.go, then run ` + "`rastrillo migration generate`" + `
  and read the SQL it writes. Never edit a migration that has shipped.
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
