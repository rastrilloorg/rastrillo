# Fingerprinted Assets + Scaffolded Test Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `rastrillo.Assets` (content-hashed static asset URLs served immutable-cacheable) and make `rastrillo new` scaffold a passing test harness, per `docs/superpowers/specs/2026-08-11-assets-and-tdd-scaffold-design.md`.

**Architecture:** A root-package `Assets` type wraps the app's embedded `StaticFS` (F8 landed after the spec was drafted: `static/` is compiled in via `//go:embed static`, served with `http.FileServerFS` and no `StripPrefix`, and `rastrillo dev` already watches `static/` and rebuilds on save). `Path(name)` maps an FS path to its hashed absolute URL path; `Handler()` serves hashed names with `Cache-Control: public, max-age=31536000, immutable` and bare/stale names with `no-cache`. The scaffold wires it into `Ctx`, renders an HTML index page that links the hashed stylesheet, and gains `internal/<pkg>test/` with a harness and example tests that assert exactly this behavior.

**Tech Stack:** Go stdlib only (`crypto/sha256`, `io/fs`, `net/http`, `regexp`). No new dependencies.

## Global Constraints

- Go 1.22 `http.ServeMux` patterns (`GET /static/`), matching the codebase.
- No new module dependencies anywhere.
- Comment style: prose-heavy doc comments explaining *why*, matching the repo's voice (see `ctx.go`, `cmd/rastrillo/new.go`).
- All test runs in this sandbox need: `export GOCACHE="$TMPDIR/gocache" GOFLAGS=-mod=mod GOPRIVATE='*' GONOSUMDB='*'` (the build cache default path is sandbox-read-only, and nested `go` invocations can't reach the module proxy).
- Branch: `worktree-assets-tdd-story` (PR #26). Commit per task. Never push to main.
- Cache header values are exact strings: `public, max-age=31536000, immutable` and `no-cache`.
- Hash: SHA-256 of file content, first 16 lowercase hex chars, inserted before the basename's extension (`tokens.css` → `tokens.<16hex>.css`; an extension-less `README` → `README.<16hex>`).

---

### Task 1: Amend the spec for the embedded-assets reality

The codebase moved after the spec was approved: F8 embedded `static/` into the binary and `dev` now watches `static/`. Three spec details are stale. This is a docs-only task.

**Files:**
- Modify: `docs/superpowers/specs/2026-08-11-assets-and-tdd-scaffold-design.md`

**Interfaces:**
- Produces: the amended contract every later task implements — `NewAssets(fsys fs.FS)`, `Path(name string) string` taking an FS path (e.g. `"static/tokens.css"`) and returning an absolute URL path (e.g. `"/static/tokens.<16hex>.css"`), `Handler()` mounted without `StripPrefix`.

- [ ] **Step 1: Edit the spec**

Make these amendments (keep everything else):

1. In "Hashing and freshness", replace the stat-per-lookup dev-freshness rationale with the embed reality: `static/` is embedded (F8), `rastrillo dev` watches `static/` and rebuilds+restarts on save, so an edit becomes a new embedded FS → new hash → new URL on the next reload. embed.FS reports zero mtimes, so hashes are computed once per file per process — correct, since embedded content can't change without a restart. Keep the (mtime, size)-keyed cache: it's what makes an `os.DirFS`-backed `Assets` (an app that opted out of embedding) stay fresh without restart, and it costs one `Stat` per lookup.
2. In the API section: `Path` takes the FS path (`Path("static/tokens.css")`) and returns the absolute URL path (`"/static/tokens.<16hex>.css"`) — with `http.FileServerFS` semantics (no `StripPrefix`, the F8 idiom), URL path = `"/"` + FS path, so `Assets` is *not* mount-agnostic and doesn't need to be. Missing file → `"/" + name` unchanged. Mount line becomes `mux.Handle("GET /static/", assets.Handler())`.
3. In "Scaffold wiring": `NewAssets(app.StaticFS)` (the embedded FS from the scaffolded `assets.go`), not `os.DirFS("static")`; drop the `StripPrefix` from the mount snippet; the stylesheet href is `ctx.Assets.Path("static/tokens.css")` with no manual prefixing.
4. Delete the sentence claiming `rastrillo dev` doesn't watch `static/` (it does, since F8).

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-08-11-assets-and-tdd-scaffold-design.md
git commit -m "Spec amendment: assets are embedded now (F8), Path returns absolute URL paths"
```

---

### Task 2: `Assets` hashing and `Path`

**Files:**
- Create: `assets.go` (root package `rastrillo`)
- Test: `assets_test.go`

**Interfaces:**
- Produces: `func NewAssets(fsys fs.FS) *Assets`; `func (a *Assets) Path(name string) string`. Task 3 adds `Handler()` to the same type; Tasks 4–6 call both.

- [ ] **Step 1: Write the failing tests**

Create `assets_test.go`:

```go
package rastrillo

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"
)

// hashedCSS is the URL shape Path promises: the FS path with 16 hex
// chars of the content hash inserted before the extension, absolute.
var hashedCSS = regexp.MustCompile(`^/static/tokens\.[0-9a-f]{16}\.css$`)

func TestPathInsertsContentHash(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	got := a.Path("static/tokens.css")
	if !hashedCSS.MatchString(got) {
		t.Errorf("Path = %q, want /static/tokens.<16 hex>.css", got)
	}
}

func TestPathIsStableForSameContent(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	if first, second := a.Path("static/tokens.css"), a.Path("static/tokens.css"); first != second {
		t.Errorf("same content, different paths: %q then %q", first, second)
	}
}

func TestPathDiffersAcrossContent(t *testing.T) {
	one := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	two := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("main{}")}})
	if p1, p2 := one.Path("static/tokens.css"), two.Path("static/tokens.css"); p1 == p2 {
		t.Errorf("different content, same path %q", p1)
	}
}

// A missing file degrades to the bare absolute path: the 404 then
// surfaces at request time, visibly, instead of a panic at render time.
func TestPathMissingFileReturnsBareName(t *testing.T) {
	a := NewAssets(fstest.MapFS{})
	if got := a.Path("static/nope.css"); got != "/static/nope.css" {
		t.Errorf("Path on missing file = %q, want /static/nope.css", got)
	}
}

// An extension-less name gets the hash appended at the end.
func TestPathExtensionless(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/LICENSE": {Data: []byte("mit")}})
	got := a.Path("static/LICENSE")
	if !regexp.MustCompile(`^/static/LICENSE\.[0-9a-f]{16}$`).MatchString(got) {
		t.Errorf("Path = %q, want /static/LICENSE.<16 hex>", got)
	}
}

// The freshness contract for a live-directory FS (an app using
// os.DirFS instead of embedding): editing the file changes the hash on
// the next lookup, no restart, because the (mtime, size) cache key
// notices the stat change.
func TestPathSeesFileEdits(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(dir, "static", "tokens.css")
	if err := os.WriteFile(css, []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewAssets(os.DirFS(dir))
	before := a.Path("static/tokens.css")

	// A distinct mtime as well as distinct content: some filesystems
	// have coarse timestamps, and the cache keys on (mtime, size).
	if err := os.WriteFile(css, []byte("main{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(css, future, future); err != nil {
		t.Fatal(err)
	}

	after := a.Path("static/tokens.css")
	if before == after {
		t.Errorf("file edited but Path stayed %q", before)
	}
}
```

Add `"time"` to the imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestPath' ./ -v 2>&1 | tail -20`
Expected: FAIL to compile — `undefined: NewAssets`.

- [ ] **Step 3: Write the implementation**

Create `assets.go`:

```go
package rastrillo

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"
)

// Assets fingerprints an app's static files so they can be cached
// forever and still update on an ordinary reload (see the assets +
// TDD-scaffold design doc). Path maps a file to a URL carrying its
// content hash; Handler (handler half of the type) serves that URL
// with an immutable Cache-Control. Because the hash changes whenever
// the content does, the HTML always links a URL the browser has never
// cached stale.
//
// The FS is served with http.FileServerFS semantics — URL path = "/" +
// FS path — matching how the scaffold embeds static/ (assets.go's
// //go:embed static): NewAssets(app.StaticFS), mounted at
// "GET /static/" with no StripPrefix.
type Assets struct {
	fsys fs.FS

	mu    sync.Mutex
	cache map[string]assetInfo
}

// assetInfo caches one file's hash keyed by the stat that produced it.
// For an embedded FS the mtime is the zero time and never changes —
// one hash per process, which is right, because embedded content can't
// change without a rebuild and restart. For a live directory
// (os.DirFS), an edit changes (mtime, size) and the next lookup
// rehashes — which is what keeps a non-embedding app fresh without a
// restart.
type assetInfo struct {
	hash  string
	mtime time.Time
	size  int64
}

// NewAssets wraps a file tree — the scaffold's embedded StaticFS, or
// os.DirFS for an app serving a live directory — in a content-hash
// registry.
func NewAssets(fsys fs.FS) *Assets {
	return &Assets{fsys: fsys, cache: make(map[string]assetInfo)}
}

// assetHashLen is how much of the SHA-256 hex survives into the URL.
// 16 hex chars = 64 bits: collisions would need billions of asset
// versions, and shorter names keep the HTML readable.
const assetHashLen = 16

// Path maps an FS path to its currently-hashed absolute URL path:
//
//	Path("static/tokens.css") → "/static/tokens.d1e8a70b5ccab1dc.css"
//
// A missing file returns "/" + name unchanged, so the 404 surfaces at
// request time — visible in the network tab — instead of a render-time
// panic.
func (a *Assets) Path(name string) string {
	h, err := a.hashFor(name)
	if err != nil {
		return "/" + name
	}
	dir, base := path.Split(name)
	ext := path.Ext(base)
	return "/" + dir + strings.TrimSuffix(base, ext) + "." + h + ext
}

// hashFor returns name's content hash, recomputing only when the
// file's (mtime, size) differ from the cached stat.
func (a *Assets) hashFor(name string) (string, error) {
	info, err := fs.Stat(a.fsys, name)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fs.ErrNotExist
	}

	a.mu.Lock()
	cached, ok := a.cache[name]
	a.mu.Unlock()
	if ok && cached.mtime.Equal(info.ModTime()) && cached.size == info.Size() {
		return cached.hash, nil
	}

	b, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	h := hex.EncodeToString(sum[:])[:assetHashLen]

	a.mu.Lock()
	a.cache[name] = assetInfo{hash: h, mtime: info.ModTime(), size: info.Size()}
	a.mu.Unlock()
	return h, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestPath' ./ -v 2>&1 | tail -12`
Expected: all six PASS.

- [ ] **Step 5: Commit**

```bash
git add assets.go assets_test.go
git commit -m "rastrillo.Assets: content-hashed asset paths (Path half)"
```

---

### Task 3: `Assets.Handler` serving rules

**Files:**
- Modify: `assets.go`
- Test: `assets_test.go`

**Interfaces:**
- Consumes: `Assets.hashFor` from Task 2.
- Produces: `func (a *Assets) Handler() http.Handler` — mounted as `mux.Handle("GET /static/", assets.Handler())`.

- [ ] **Step 1: Write the failing tests**

Append to `assets_test.go` (add imports `"net/http"`, `"net/http/httptest"`, `"strings"`):

```go
// serveAsset runs one GET through Handler and returns the recorder.
func serveAsset(t *testing.T, a *Assets, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandlerServesHashedNameImmutable(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, a.Path("static/tokens.css"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable year", got)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("body = %q, want the file content", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
}

// A bare name keeps working — deep links, hand-written URLs — it just
// forgoes the long cache.
func TestHandlerServesBareNameNoCache(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, "/static/tokens.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

// A stale page asking for an old version gets the *current* content,
// un-cached: a slightly-stale stylesheet on a stale page beats a 404.
func TestHandlerServesStaleHashCurrentContent(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	rec := serveAsset(t, a, "/static/tokens.0123456789abcdef.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache for a stale hash", got)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("body = %q, want current content", rec.Body.String())
	}
}

// A file whose real name happens to look hashed is served under that
// real name — literal names win over hash-stripping.
func TestHandlerLiteralNameWins(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/v.0123456789abcdef.css": {Data: []byte("real{}")}})
	rec := serveAsset(t, a, "/static/v.0123456789abcdef.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if rec.Body.String() != "real{}" {
		t.Errorf("body = %q, want the literal file", rec.Body.String())
	}
}

func TestHandlerMissing404s(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/tokens.css": {Data: []byte("body{}")}})
	for _, target := range []string{"/static/nope.css", "/static/nope.0123456789abcdef.css", "/static/"} {
		if rec := serveAsset(t, a, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", target, rec.Code)
		}
	}
}

func TestHandlerSubdirectoryAsset(t *testing.T) {
	a := NewAssets(fstest.MapFS{"static/img/logo.svg": {Data: []byte("<svg/>")}})
	href := a.Path("static/img/logo.svg")
	rec := serveAsset(t, a, href)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", href, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable year", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestHandler' ./ -v 2>&1 | tail -12`
Expected: FAIL to compile — `a.Handler undefined`.

- [ ] **Step 3: Write the implementation**

Append to `assets.go` (add imports `"bytes"`, `"net/http"`, `"regexp"`):

```go
// hashedName matches a basename carrying an inserted hash —
// "tokens.<16 hex>.css" or extension-less "LICENSE.<16 hex>" — and
// captures the pieces needed to reconstruct the original name.
var hashedName = regexp.MustCompile(`^(.+)\.([0-9a-f]{16})(\.[^.]*)?$`)

// Handler serves the tree with the fingerprinting contract:
//
//   - a hashed name matching the file's current content is immutable —
//     Cache-Control: public, max-age=31536000, immutable — because that
//     exact URL can never serve different bytes;
//   - a hashed name that no longer matches (a stale page asking for an
//     old version) serves the *current* content with no-cache: a
//     slightly-stale stylesheet on a stale page beats a 404;
//   - a bare name serves no-cache, so deep links keep working;
//   - a real file whose name merely looks hashed wins over
//     hash-stripping.
//
// Mount it where the FS layout says — for the scaffold's embedded
// static/:
//
//	mux.Handle("GET /static/", assets.Handler())
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if a.serveFile(w, r, name, "no-cache") {
			return
		}
		dir, base := path.Split(name)
		m := hashedName.FindStringSubmatch(base)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		orig := dir + m[1] + m[3]
		current, err := a.hashFor(orig)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		cc := "no-cache"
		if current == m[2] {
			cc = "public, max-age=31536000, immutable"
		}
		if !a.serveFile(w, r, orig, cc) {
			http.NotFound(w, r)
		}
	})
}

// serveFile writes name's content with the given Cache-Control,
// reporting whether name resolved to a servable file. ServeContent
// picks the Content-Type from the name's extension and handles ranges;
// there is no ETag — the URL is the validator.
func (a *Assets) serveFile(w http.ResponseWriter, r *http.Request, name, cacheControl string) bool {
	info, err := fs.Stat(a.fsys, name)
	if err != nil || info.IsDir() {
		return false
	}
	b, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(b))
	return true
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./ -v -run 'TestPath|TestHandler' 2>&1 | tail -18`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add assets.go assets_test.go
git commit -m "rastrillo.Assets: Handler serving rules (immutable / no-cache / stale-hash)"
```

---

### Task 4: `Ctx.Assets` + scaffold templates render a fingerprinted page

**Files:**
- Modify: `ctx.go` (new field), `cmd/rastrillo/new.go` (`mainTemplate`, `actionTemplate`)
- Test: `cmd/rastrillo/new_test.go`

**Interfaces:**
- Consumes: `rastrillo.NewAssets`, `(*Assets).Path`, `(*Assets).Handler`.
- Produces: `Ctx.Assets *Assets`; a scaffolded `main.go` containing `assets := rastrillo.NewAssets(app.StaticFS)` and `mux.Handle("GET /static/", assets.Handler())`; a scaffolded `actions/index.GET.go` whose page contains `<h1>Hello, World — this is a rastrillo app.</h1>` and links `ctx.Assets.Path("static/tokens.css")`. Task 5's harness relies on exactly these strings.

- [ ] **Step 1: Update the template tests to demand the new wiring**

In `cmd/rastrillo/new_test.go`, replace `TestMainTemplateServesTheStaticDir` with:

```go
// The generated app serves its own static directory, fingerprinted:
// rastrillo.Serve never serves CSS — that is the app's job, in the
// app's own code — and the scaffold wires rastrillo.Assets so those
// files are immutable-cacheable from day one.
func TestMainTemplateServesFingerprintedStatic(t *testing.T) {
	src := fmt.Sprintf(mainTemplate, "blogapp")
	for _, want := range []string{
		`assets := rastrillo.NewAssets(app.StaticFS)`,
		`mux.Handle("GET /static/", assets.Handler())`,
		`Assets: assets`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go template missing %q:\n%s", want, src)
		}
	}
	// The mount has to come after the router exists, or it has nothing
	// to attach to.
	if strings.Index(src, "gen.Router(") > strings.Index(src, `assets.Handler()`) {
		t.Error("the static handler is registered before gen.Router builds the mux")
	}
}

// The starter action is a real HTML page linking the stylesheet by its
// content-hashed URL — the scaffold demonstrating its own asset story.
func TestActionTemplateLinksFingerprintedStylesheet(t *testing.T) {
	for _, want := range []string{
		`ctx.Assets.Path("static/tokens.css")`,
		`<h1>Hello, World — this is a rastrillo app.</h1>`,
		`text/html; charset=utf-8`,
	} {
		if !strings.Contains(actionTemplate, want) {
			t.Errorf("action template missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/rastrillo/ -run 'TestMainTemplate|TestActionTemplate' -v 2>&1 | tail -8`
Expected: FAIL (old template content).

- [ ] **Step 3: Add the `Ctx.Assets` field**

In `ctx.go`, after the `Logger` field:

```go
	// Assets is the app's fingerprinted static-file registry, when
	// the app wires one — the scaffold does, over its embedded
	// static/ tree. Actions link assets by hashed URL:
	// ctx.Assets.Path("static/tokens.css"). Nil for an app that
	// serves assets some other way — the same contract as DB.
	Assets *Assets
```

- [ ] **Step 4: Rewrite `mainTemplate` and `actionTemplate`**

In `cmd/rastrillo/new.go`, `mainTemplate` becomes:

```go
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
```

`actionTemplate` becomes (note the backtick-concatenation for the raw string inside — the file already uses this idiom for the build-constraint mention):

```go
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
```

(The scaffolded file's `%s` must land literally — `actionTemplate` is written verbatim, never through `fmt.Sprintf`, so nothing escapes it.)

- [ ] **Step 5: Run the cmd tests**

Run: `go test ./cmd/rastrillo/ 2>&1 | tail -4`
Expected: PASS (all existing scaffold tests — tokens, version pin, tagged actions, hyphenated names — still green; `runNew`'s in-test generate exercises the new action template end to end).

- [ ] **Step 6: Run the full suite**

Run: `go test ./... 2>&1 | grep -v '^ok\|^rastrillo generate\|^  ' | head`
Expected: no FAIL lines.

- [ ] **Step 7: Commit**

```bash
git add ctx.go cmd/rastrillo/new.go cmd/rastrillo/new_test.go
git commit -m "Scaffold wires rastrillo.Assets: fingerprinted static, HTML index page"
```

---

### Task 5: `rastrillo new` scaffolds the test harness

**Files:**
- Modify: `cmd/rastrillo/new.go` (two new template consts; write + print the files; closing hint → `go test ./...`)
- Test: `cmd/rastrillo/new_test.go`

**Interfaces:**
- Consumes: `packageName(name)` (existing), the exact page strings Task 4 produced.
- Produces: scaffolded `internal/<pkg>test/harness_test.go` (`newApp`, `get`, `post`) and `internal/<pkg>test/index_test.go` (three example tests).

- [ ] **Step 1: Write the failing tests**

Append to `cmd/rastrillo/new_test.go`:

```go
// Out of the box you get a tested app: rastrillo new scaffolds a
// harness (the blog example's blogtest pattern, delivered as
// app-owned files) plus example tests that pass immediately and pin
// the asset-fingerprinting behavior.
func TestNewScaffoldsTestHarness(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"my-blog"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	harness, err := os.ReadFile(filepath.Join("my-blog", "internal", "myblogtest", "harness_test.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded harness: %v", err)
	}
	for _, want := range []string{
		"package myblogtest",
		"func newApp(t *testing.T) http.Handler",
		`app "my-blog"`,
		"rastrillo.NewAssets(app.StaticFS)",
		"rastrillo.OpenDB",
	} {
		if !strings.Contains(string(harness), want) {
			t.Errorf("harness_test.go missing %q:\n%s", want, harness)
		}
	}
	index, err := os.ReadFile(filepath.Join("my-blog", "internal", "myblogtest", "index_test.go"))
	if err != nil {
		t.Fatalf("expected scaffolded example tests: %v", err)
	}
	for _, want := range []string{
		"func TestIndexRenders",
		"func TestIndexLinksFingerprintedStylesheet",
		"func TestBareAssetNameStaysFresh",
		"public, max-age=31536000, immutable",
	} {
		if !strings.Contains(string(index), want) {
			t.Errorf("index_test.go missing %q:\n%s", want, index)
		}
	}
}

// The scaffold's own tests pass, from zero, before the developer
// writes a line: go test ./... against the freshly generated app,
// with the rastrillo require replaced by this checkout (the same
// scratch-module pattern internal/manifest's goeval tests use).
func TestScaffoldedAppTestsPass(t *testing.T) {
	root := repoRoot(t)
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	f, err := os.OpenFile(filepath.Join("blogapp", "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace github.com/carlosframework/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = "blogapp"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scaffolded app's tests fail:\n%s", out)
	}
}

// repoRoot locates this checkout from the test file's own path, so
// the scaffolded app's replace directive can point at it.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
```

Add `"os/exec"` and `"runtime"` to the file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/rastrillo/ -run 'TestNewScaffoldsTestHarness|TestScaffoldedAppTestsPass' -v 2>&1 | tail -8`
Expected: FAIL — no harness files scaffolded.

- [ ] **Step 3: Add the harness templates and wire them into `runNew`**

In `cmd/rastrillo/new.go`, add two consts (both `fmt.Sprintf`'d with `%[1]s` = app name / module path, `%[2]s` = `packageName(name)`; backtick-concatenation where the scaffolded file itself needs a raw string):

```go
// harnessTemplate is the scaffolded test harness: the blog example's
// blogtest pattern, delivered once as app-owned files. It builds the
// whole app exactly as cmd/<name>/main.go does, so a test exercises
// the real generated router, the real Ctx wiring, the real asset
// handler — not a parallel universe.
const harnessTemplate = `// Package %[2]stest tests the app from the outside: real HTTP
// requests against the real generated router, wired exactly as
// cmd/%[1]s/main.go wires it.
//
// The TDD loop: write a failing test here, make it pass in actions/
// (or internal/), repeat. Tests import gen, so after editing actions/
// run ` + "`rastrillo generate`" + ` before ` + "`go test ./...`" + ` — or leave
// ` + "`rastrillo dev`" + ` running, which regenerates on save.
package %[2]stest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"

	app "%[1]s"
	"%[1]s/gen"
)

// newApp builds the whole app per test, exactly as main.go does. When
// the app grows a database, open a fresh one here per test — a real
// temp file, not :memory:, because SetMaxOpenConns(1)+WAL is the
// configuration under test:
//
//	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "app.db"), migrations)
//	t.Cleanup(func() { db.Close() })
//
// and put it on the Ctx below, mirroring main.go's move from
// Options.Mux to Options.Router.
func newApp(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	assets := rastrillo.NewAssets(app.StaticFS)
	ctx := &rastrillo.Ctx{Logger: logger, Assets: assets}
	mux := gen.Router(func(*http.Request) *rastrillo.Ctx { return ctx })
	mux.Handle("GET /static/", assets.Handler())
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
// first ` + "`go test`" + `, and pinning the out-of-the-box asset story — the
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
```

(Every literal `%` in the scaffolded test bodies is doubled — these two consts DO go through `fmt.Sprintf`.)

In `runNew`, add the directory and files (`pkg := packageName(name)` is already computed for `assetsTemplate`):

- to `dirs`: `filepath.Join(name, "internal", pkg+"test")`
- to `files`:
  ```go
  filepath.Join(name, "internal", pkg+"test", "harness_test.go"): fmt.Sprintf(harnessTemplate, name, pkg),
  filepath.Join(name, "internal", pkg+"test", "index_test.go"):   fmt.Sprintf(indexTestTemplate, name, pkg),
  ```
- to the printed listing, after the `static/tokens.css` line:
  ```go
  fmt.Printf("  internal/%stest/harness_test.go\n", pkg)
  fmt.Printf("  internal/%stest/index_test.go\n", pkg)
  ```
- the closing hint becomes the scaffold's own passing tests:
  ```go
  fmt.Printf("\ncd %s && go test ./...\n", name)
  ```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/rastrillo/ -v -run 'TestNew|TestScaffolded|TestMainTemplate|TestActionTemplate' 2>&1 | tail -20`
Expected: all PASS, including `TestScaffoldedAppTestsPass` (the slow one — it runs `go test` inside the scaffold).

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/new.go cmd/rastrillo/new_test.go
git commit -m "rastrillo new scaffolds a test harness whose example tests pass from zero"
```

---

### Task 6: The blog example adopts Assets

**Files:**
- Modify: `examples/blog/internal/blog/view.go` (Assets var + `asset` func), `examples/blog/internal/blog/templates/layout.html`, `examples/blog/cmd/blog/main.go`
- Test: `examples/blog/internal/blogtest/tokens_test.go`

**Interfaces:**
- Consumes: `rastrillo.NewAssets`, `Path`, `Handler`; the root package's `blogassets "blog"` embed (`StaticFS`).
- Produces: `blog.Assets` (package `examples/blog/internal/blog`), the `asset` template func, hashed hrefs in every rendered layout.

- [ ] **Step 1: Write the failing tests**

In `examples/blog/internal/blogtest/tokens_test.go`, add (imports it may need: `"regexp"`):

```go
// Every rendered screen links its stylesheets by fingerprinted URL,
// and that URL is served cacheable-forever: with the platform's edge
// cache in front, a hibernating blog's assets never wake it.
func TestScreensLinkFingerprintedStylesheets(t *testing.T) {
	h, _ := newApp(t)
	body := get(t, h, "/").Body.String()
	href := regexp.MustCompile(`/static/tokens\.[0-9a-f]{16}\.css`).FindString(body)
	if href == "" {
		t.Fatalf("front page links no fingerprinted tokens.css:\n%s", body)
	}
	rec := get(t, h, href)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", href, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("GET %s: Cache-Control = %q, want immutable year", href, got)
	}
	if bare := regexp.MustCompile(`/static/blog\.[0-9a-f]{16}\.css`).FindString(body); bare == "" {
		t.Error("front page links no fingerprinted blog.css")
	}
}
```

Check the existing embedded-serving test in that file: it asserts `/static/tokens.css` still resolves — keep it, and extend it to assert `Cache-Control: no-cache` if it doesn't already conflict.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd examples/blog && go test ./internal/blogtest/ -run 'TestScreensLinkFingerprinted' -v 2>&1 | tail -6; cd ../..`
Expected: FAIL — layout links bare `/static/tokens.css`.

- [ ] **Step 3: Implement**

1. In `examples/blog/internal/blog/view.go`, next to `buildPages`:

   ```go
   // Assets fingerprints the app's embedded static files. One instance
   // shared by the "asset" template func and main.go's /static/ mount,
   // so the URL a layout renders is always one the handler serves
   // immutable.
   var Assets = rastrillo.NewAssets(blogassets.StaticFS)
   ```

   with imports `"github.com/carlosframework/rastrillo"` and `blogassets "blog"` added, and the FuncMap line extended:

   ```go
   base := template.New("").Funcs(ui.Funcs()).Funcs(template.FuncMap{"T": genT, "asset": Assets.Path})
   ```

2. In `examples/blog/internal/blog/templates/layout.html`, lines 15–16 become:

   ```html
   <link rel="stylesheet" href="{{asset "static/tokens.css"}}">
   <link rel="stylesheet" href="{{asset "static/blog.css"}}">
   ```

3. In `examples/blog/cmd/blog/main.go`, the mount becomes:

   ```go
   mux.Handle("GET /static/", blog.Assets.Handler())
   ```

   (the `blogassets` import stays only if still referenced; drop it otherwise, and update the comment above the mount to mention fingerprinting).

- [ ] **Step 4: Run the blog suite**

Run: `cd examples/blog && go test ./... 2>&1 | tail -4; cd ../..`
Expected: PASS — including every pre-existing screen test.

- [ ] **Step 5: Commit**

```bash
git add examples/blog
git commit -m "Blog example adopts rastrillo.Assets: hashed hrefs via the asset template func"
```

---

### Task 7: Full verification, push, PR update

- [ ] **Step 1: Run everything**

Run: `go test ./... && (cd examples/blog && go test ./...) && (cd examples/helloworld && go test ./... 2>/dev/null; true) && go vet ./...`
Expected: no failures.

- [ ] **Step 2: Verify it in the real app (smoke)**

Scaffold an app into a temp dir with the locally-built CLI, add the `replace`, run its tests, and curl the served page:

```bash
REPO="$(pwd)"
go build -o "$TMPDIR/rastrillo" ./cmd/rastrillo
cd "$TMPDIR" && rm -rf smokeapp && ./rastrillo new smokeapp
printf '\nreplace github.com/carlosframework/rastrillo => %s\n' "$REPO" >> smokeapp/go.mod
cd smokeapp && go test ./...
```

Expected: the scaffold's own suite passes.

- [ ] **Step 3: Push and update PR #26**

```bash
git push origin worktree-assets-tdd-story
gh pr edit 26 --title "Fingerprinted assets + scaffolded test harness" # body update describing implementation
gh pr ready 26
```
