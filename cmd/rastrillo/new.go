package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/carlosframework/rastrillo/ui"
)

// runNew implements `rastrillo new <name>`: go.mod, one starter action,
// a main.go wiring Run to the (not-yet-generated) router, then runs
// generate once so `go build` works immediately (design doc §11).
//
// The starter is a plain hand-written action, not a Resource/TOML
// manifest: manifests are optional per route (design doc §3), and a
// hello-world has nothing to administer. manifest/ is scaffolded empty
// with a README instead, so the first admin screen is one TOML file
// away.
//
// The scaffold also makes the app a good citizen of the family's two
// hosts: a Makefile whose `ci` target is the one local/CI gate
// definition, and .amadan/ci + .amadan/ci.d/ steps that exec those
// targets — amadan's runner convention, where the steps must be
// executable or the job silently resolves "skipped" (the known failure
// mode; scaffolding writes them 0o755 so it cannot happen here). CARLOS
// stays the golden deployment target, not a requirement: Run speaks the
// activation contract, and `./app -addr :8080` works anywhere.
func runNew(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: rastrillo new <name>")
	}
	name := args[0]
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}

	dirs := []string{
		name,
		filepath.Join(name, "actions"),
		filepath.Join(name, "cmd", name),
		filepath.Join(name, "manifest"),
		filepath.Join(name, "static"),
		filepath.Join(name, ".amadan", "ci.d"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	files := map[string]string{
		filepath.Join(name, "go.mod"):                  fmt.Sprintf(goModTemplate, name),
		filepath.Join(name, "actions", "index.GET.go"): actionTemplate,
		filepath.Join(name, "cmd", name, "main.go"):    fmt.Sprintf(mainTemplate, name),
		// The design-token stylesheet, delivered once. rastrillo.Serve
		// never serves CSS at runtime; from here on this is an ordinary
		// app-owned file that new/generate never touch again.
		filepath.Join(name, "static", "tokens.css"):  string(ui.TokensCSS()),
		filepath.Join(name, "manifest", "README.md"): manifestReadme,
		filepath.Join(name, "Makefile"):              makefileTemplate,
		filepath.Join(name, "CLAUDE.md"):             fmt.Sprintf(claudeMDTemplate, name),
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// CI scripts must be executable: amadan's runner resolves a
	// non-executable job "skipped" with only a hint — the single most
	// common failure mode, closed at scaffold time.
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
	fmt.Println("  go.mod")
	fmt.Println("  actions/index.GET.go")
	fmt.Printf("  cmd/%s/main.go\n", name)
	fmt.Println("  manifest/            (drop a <name>.toml here for a whole admin screen set)")
	fmt.Println("  static/tokens.css")
	fmt.Println("  Makefile             (make ci = vet + fmt + test, the one gate definition)")
	fmt.Println("  .amadan/ci, ci.d/    (amadan runner CI, executable, delegating to make)")
	fmt.Println("  CLAUDE.md")

	if err := runGenerate([]string{name}); err != nil {
		return fmt.Errorf("initial generate: %w", err)
	}
	// go build ./... discards its output whenever it matches more than one
	// package (actions, gen, gen/actions/..., cmd/<name> — see `go help
	// build`), even though cmd/<name> is the only `main` among them. Build
	// that package by its own import path so the binary actually lands here.
	fmt.Printf("\ncd %s && go build ./cmd/%[1]s\n", name)
	return nil
}

const manifestReadme = `# manifest/

Manifests live here (design doc §3). One TOML file declares a whole
admin screen set — List → Show → Edit/New plus the confirm-page delete
flow — generated as real actions under gen/ by ` + "`rastrillo generate`" + `:

    name  = "ticket_types"
    route = "/admin/ticket_types"

    [list]
    columns = [{ field = "Name", kind = "text" }, { field = "Price", kind = "money" }]
    search  = true

    [form]
    basics = [{ name = "Name", required = true }, { name = "Price", kind = "money" }]

Wire the generated migrations in cmd/<app>/main.go:
` + "`Migrations: genmanifest.Migrations()`" + ` (import <module>/gen/manifest).

Drop to a .go manifest in this directory (package manifest, a
` + "`var X = rastrillo.Resource{...}`" + `) the moment you need a function
value — a custom Column.Render, say. To take any single generated
screen over by hand, write your own action file at the same computed
path under actions/ — the generator skips that one from then on.
`

// makefileTemplate is the one gate definition: CI steps exec these
// targets, never their own copies of the commands (amadan's own rule).
const makefileTemplate = `.PHONY: build test vet fmt-check generate ci

build: generate
	go build ./...

generate:
	rastrillo generate

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. amadan's runner falls back to this
# target when .amadan/ci is absent.
ci: vet fmt-check test
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

A [rastrillo](https://github.com/carlosframework/rastrillo) app. The
conventions below are load-bearing; the framework enforces most of them
mechanically.

## Layout

- ` + "`actions/`" + ` — one file per route (` + "`<name>.<VERB>.go`" + `, bracket dirs
  for params: ` + "`orders/[id]/cancel.POST.go`" + `). Files carry
  ` + "`//go:build rastrillo_actions`" + ` and are never compiled in place —
  ` + "`rastrillo generate`" + ` copies them under ` + "`gen/`" + `. actions/ cannot
  hold shared code: put it in an ordinary package (` + "`internal/...`" + `)
  and import it.
- ` + "`manifest/`" + ` — TOML or .go Resource manifests; each generates a full
  screen set. A hand-written action file at a generated path takes that
  screen over, silently, by existence.
- ` + "`gen/`" + ` — generated; never edit. Regenerate with ` + "`rastrillo generate`" + `
  (or let ` + "`rastrillo dev`" + ` watch and do it).

## Rules the family holds hard

- Migrations are additive and idempotent, applied at boot via
  ` + "`rastrillo.Options.Migrations`" + `. Never rewrite one that shipped.
- Money is integer cents. A float never touches a value a person will
  be held to.
- Screens work with JavaScript disabled; destructive actions get their
  own confirm-page URL.
- The gate is ` + "`make ci`" + ` (vet + gofmt + tests) — the same definition
  CI runs. Run it before every push.

## Serving

` + "`rastrillo.Run`" + ` speaks the CARLOS activation contract (socket/addr/db
argv, LISTEN_FDS, $STATE_DIRECTORY, ` + "`sidecar run`" + `) — the golden
deployment target, not a requirement: ` + "`./app -addr :8080`" + ` serves
anywhere. ` + "`GET /healthz`" + ` and ` + "`GET /api/version`" + ` are answered by the
framework.
`

const goModTemplate = `module %s

go 1.22

require github.com/carlosframework/rastrillo v0.1.0
`

const actionTemplate = `// actions/ is generator input, never compiled in place: rastrillo
// generate copies each file under gen/ (stripping this constraint).
// The tag keeps ` + "`go build ./...`" + ` and friends off the originals.
//go:build rastrillo_actions

package actions

import (
	"fmt"
	"net/http"

	"github.com/carlosframework/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, World — this is a rastrillo app.")
}
`

const mainTemplate = `package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/carlosframework/rastrillo"

	"%[1]s/gen"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// A single shared Ctx for now: this app has no per-request state
	// yet (no DB, no locale, no scope). Once it needs a database,
	// switch Options.Mux for Options.Router and build the mux from
	// the *sql.DB Serve hands back.
	ctx := &rastrillo.Ctx{Logger: logger}
	mux := gen.Router(func(*http.Request) *rastrillo.Ctx { return ctx })

	// The app serves its own static files — the framework never does.
	// static/tokens.css was scaffolded here once by rastrillo new; edit
	// it, replace it, or delete this handler if you serve assets some
	// other way.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Run speaks the platform's activation contract: -socket/-addr/-db
	// flags for agent exec children, or a bare "serve" subcommand for
	// carlos-app@ unit tenants (see rastrillo.Run).
	if err := rastrillo.Run(rastrillo.Options{
		Mux:    mux,
		Logger: logger,
	}); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
`
