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

// runNew implements `rastrillo new <name>`: go.mod, one starter action,
// a main.go wiring Run to the (not-yet-generated) router, then runs
// generate once so `go build` works immediately (design doc §11).
//
// The starter is a plain hand-written action, not a Resource/TOML
// manifest: manifests are optional per route (design doc §3), and the
// manifest/codegen-with-skip system isn't built yet (see README.md) —
// scaffolding one would be scaffolding a feature that doesn't exist.
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
		filepath.Join(name, "static"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	files := map[string]string{
		filepath.Join(name, "go.mod"):                  fmt.Sprintf(goModTemplate, name, rastrilloVersion()),
		filepath.Join(name, "actions", "index.GET.go"): actionTemplate,
		filepath.Join(name, "cmd", name, "main.go"):    fmt.Sprintf(mainTemplate, name),
		filepath.Join(name, "assets.go"):               fmt.Sprintf(assetsTemplate, packageName(name)),
		// The design-token stylesheet, delivered once. rastrillo.Serve
		// never serves CSS at runtime; from here on this is an ordinary
		// app-owned file that new/generate never touch again.
		filepath.Join(name, "static", "tokens.css"): string(ui.TokensCSS()),
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("rastrillo new: scaffolded %s/\n", name)
	fmt.Println("  go.mod")
	fmt.Println("  actions/index.GET.go")
	fmt.Printf("  cmd/%s/main.go\n", name)
	fmt.Println("  assets.go")
	fmt.Println("  static/tokens.css")

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

// packageName derives a valid Go identifier from the app name for the
// scaffolded root package: name also serves as the module path, where
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

const goModTemplate = `module %s

go 1.22

require github.com/carlosframework/rastrillo %s
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, page, ctx.Assets.Path("static/tokens.css"))
}

// page links the design-token stylesheet by its content-hashed URL:
// cacheable forever, and a brand-new URL the moment the file changes.
const page = ` + "`" + `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Hello, World</title>
<link rel="stylesheet" href="%s">
</head>
<body>
<main>
<h1>Hello, World — this is a rastrillo app.</h1>
</main>
</body>
</html>
` + "`" + `
`

// assetsTemplate is the scaffolded assets.go: static/ compiled into
// the binary, because the platform deploys the binary alone and a
// cwd-relative static directory would not travel with it (F8).
const assetsTemplate = `// The app's static files, compiled into the binary: the platform
// deploys the binary alone, so a loose static/ directory would not
// travel with it.
package %[1]s

import "embed"

//go:embed static
var StaticFS embed.FS
`

const mainTemplate = `package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/carlosframework/rastrillo"

	app "%[1]s"
	"%[1]s/gen"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// The app's static files, embedded (see assets.go) and
	// content-fingerprinted: ctx.Assets.Path("static/tokens.css")
	// returns a URL carrying the file's hash, and the handler below
	// serves that URL cacheable-forever. Edit static/ and rebuild —
	// rastrillo dev does that on save — and the hash (so the URL)
	// changes, which is why a plain reload always sees fresh assets.
	assets := rastrillo.NewAssets(app.StaticFS)

	// A single shared Ctx for now: this app has no per-request state
	// yet (no DB, no locale, no scope). Once it needs a database,
	// switch Options.Mux for Options.Router and build the mux from
	// the *sql.DB Serve hands back.
	ctx := &rastrillo.Ctx{Logger: logger, Assets: assets}
	mux := gen.Router(func(*http.Request) *rastrillo.Ctx { return ctx })

	// The app serves its own static files — the framework never does.
	mux.Handle("GET /static/", assets.Handler())

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
