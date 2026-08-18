# Manifest System Slice 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rastrillo.Resource{List, Form}` in a TOML or Go manifest generates all four canonical states (Blank → List → Show → Edit/New) — store (sqlc), actions, templates, translation keys — proven by partial adoption in `examples/blog`.

**Architecture:** New root-package types (`Resource`/`List`/`Form`/…) with `Validate()` as the single validation home. `internal/manifest` loads TOML (BurntSushi, strict) and Go sources (go/ast discovery + a go-run driver emitting a stable JSON artifact), merges them, and hands the generator one resource set. `internal/generate` grows four emitters (store SQL + sqlc invocation, actions, templates, locales) all writing into `gen/` with scaffolding-with-skip, plus collision/idempotency checks on `--check`. The ui library gains `detail-list`. The blog adopts a posts manifest, ejects its list template, and keeps its hand actions.

**Tech Stack:** Go stdlib, BurntSushi/toml (new dep, root module), sqlc via Go 1.24+ `tool` directive (app modules), modernc.org/sqlite (existing, blog).

**Spec:** `docs/superpowers/specs/2026-08-03-manifest-system-design.md` (approved, merged in #17). Where this plan and the spec disagree, the spec governs.

## Global Constraints

- **Additive API.** Every new `Options` field is optional; apps setting none behave identically. `Serve`/`Run`/`Resolve` signatures unchanged.
- **Generate and recompile; never interpret at runtime.** Generated files are real committed files in `gen/`; a typo'd Go manifest is a compile error surfaced from the driver run.
- **Scaffolding-with-skip:** a hand-written file at a generated file's computed path (in `actions/` or `templates/`) skips that ONE file's generation, silently. A hand-written and generated path landing on the same ROUTE fails the build loudly.
- **Idempotency:** re-running generate with no manifest change produces a byte-identical `gen/` tree.
- **Kind vocabulary this slice:** `Text`, `Textarea`, `Money` (integer cents; INTEGER at the SQL boundary or generation fails). `Store`: `Exclusive` only; `Mergeable` parses but `Validate` rejects it with a "not yet built" error.
- **No delete generation** (needs the `confirm` partial — later slice).
- **The JSON artifact is stable:** field names below are the contract; evolution is additive. It is written to `gen/manifest.json` on every generate.
- **Zero JS in generated templates.** All visible strings in generated templates render through `{{T}}` with `resource.<name>.…` keys.
- **ui conventions** (partials): PascalCase dict keys, contract comment above the define, optional keys guarded, `rst-` prefix; tokens.css changes mirrored byte-identically into `examples/blog/static/tokens.css`.
- Comments state constraints the code can't show, never narration; match each file's voice.
- Sweep before every commit, all clean, in every module the task touched (root and/or `examples/blog` and/or `examples/helloworld`):
  `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go build ./...`, same env `go vet ./...`, same env `go test ./... -count=1`, and `gofmt -l .` (empty). On "read-only file system" errors, rerun with the sandbox disabled. Network fetches (new deps) may also need the sandbox disabled; `GOFLAGS=-mod=mod` stays, drop `GOPROXY=off` if any task set it.
- Blog regen command (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .`
- Commit style: short imperative subject; body says why; trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Branch: `worktree-manifest-system` (current worktree). Never switch branches.

## Fixture resource (used by every emitter's tests)

One fixture manifest drives all golden tests — `notes`, deliberately
exercising every Kind, search, filter, and a non-empty Advanced:

```toml
name  = "notes"
route = "/admin/notes"
store = "exclusive"

[list]
columns = [{ field = "Title", kind = "text" }, { field = "Price", kind = "money" }]
search  = true
filter  = ["Title"]

[form]
basics   = [{ name = "Title" }, { name = "Body", kind = "textarea" }]
advanced = [{ name = "Price", kind = "money" }]
```

---

### Task 1: root types + `Validate`

**Files:**
- Create: `manifest.go` (root package `rastrillo`)
- Test: `manifest_test.go`

**Interfaces:**
- Produces (exact, later tasks depend on all of it):

```go
type Kind string
const (
	Text     Kind = "text"
	Textarea Kind = "textarea"
	Money    Kind = "money"
)

type StoreKind string
const (
	Exclusive StoreKind = "exclusive"
	Mergeable StoreKind = "mergeable"
)

type Resource struct {
	Name  string    `json:"name" toml:"name"`
	Route string    `json:"route" toml:"route"`
	Store StoreKind `json:"store" toml:"store"`
	List  List      `json:"list" toml:"list"`
	Form  Form      `json:"form" toml:"form"`
}

type List struct {
	Columns []Column `json:"columns" toml:"columns"`
	Search  bool     `json:"search" toml:"search"`
	Filter  []string `json:"filter" toml:"filter"`
}

type Column struct {
	Field string `json:"field" toml:"field"`
	Kind  Kind   `json:"kind" toml:"kind"` // zero value means Text
}

type Form struct {
	Basics   []Field `json:"basics" toml:"basics"`
	Advanced []Field `json:"advanced" toml:"advanced"`
}

type Field struct {
	Name string `json:"name" toml:"name"`
	Kind Kind   `json:"kind" toml:"kind"` // zero value means Text
}

func (r *Resource) Validate() error
```

- [ ] **Step 1: Write the failing tests** — `manifest_test.go`, table-driven over `Validate`:

```go
package rastrillo

import (
	"strings"
	"testing"
)

func validResource() Resource {
	return Resource{
		Name:  "notes",
		Route: "/admin/notes",
		Store: Exclusive,
		List: List{
			Columns: []Column{{Field: "Title"}, {Field: "Price", Kind: Money}},
			Search:  true,
			Filter:  []string{"Title"},
		},
		Form: Form{
			Basics:   []Field{{Name: "Title"}, {Name: "Body", Kind: Textarea}},
			Advanced: []Field{{Name: "Price", Kind: Money}},
		},
	}
}

func TestValidateAcceptsTheFixture(t *testing.T) {
	r := validResource()
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Resource)
		want string // substring of the error
	}{
		{"empty name", func(r *Resource) { r.Name = "" }, "name"},
		{"non-snake name", func(r *Resource) { r.Name = "TicketTypes" }, "snake_case"},
		{"empty route", func(r *Resource) { r.Route = "" }, "route"},
		{"trailing slash", func(r *Resource) { r.Route = "/admin/notes/" }, "trailing"},
		{"no leading slash", func(r *Resource) { r.Route = "admin/notes" }, "route"},
		{"mergeable", func(r *Resource) { r.Store = Mergeable }, "not yet built"},
		{"unknown store", func(r *Resource) { r.Store = "weird" }, "store"},
		{"unknown kind", func(r *Resource) { r.List.Columns[0].Kind = "meter" }, "kind"},
		{"filter not a column", func(r *Resource) { r.List.Filter = []string{"Status"} }, "filter"},
		{"nothing declared", func(r *Resource) { r.List = List{}; r.Form = Form{} }, "at least one"},
		{"duplicate field", func(r *Resource) { r.Form.Basics = append(r.Form.Basics, Field{Name: "Title"}) }, "duplicate"},
		{"non-identifier field", func(r *Resource) { r.Form.Basics[0].Name = "my-field" }, "identifier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validResource()
			tc.mut(&r)
			err := r.Validate()
			if err == nil {
				t.Fatal("Validate accepted it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure** — `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ -run TestValidate -v` → compile error (types missing).

- [ ] **Step 3: Implement `manifest.go`.** The types exactly as in Interfaces. `Validate` rules, each with a clear error containing the substring the table expects:
  - `Name`: non-empty, matches `^[a-z][a-z0-9_]*$` ("snake_case").
  - `Route`: non-empty, starts with `/`, no trailing slash ("trailing"), each non-empty segment is either `{param}` or `[a-z0-9_-]+`.
  - `Store`: zero value defaults to `Exclusive`; `Mergeable` → error containing "not yet built"; anything else → error containing "store".
  - Kinds (columns and fields): zero value defaults to `Text` (normalize in place); otherwise must be Text/Textarea/Money → "kind".
  - `Filter` entries must name a declared column ("filter").
  - At least one List column or Form field ("at least one").
  - Field/column names: valid exported-able identifiers `^[A-Za-z][A-Za-z0-9]*$` ("identifier"); the union of column Fields + form Names has no case-insensitive duplicates ("duplicate"). (Columns and form fields MAY share names — a listed column is usually also a form field.)
  - Package doc for the file: manifests are the §9 sugar; the doc comment states the JSON artifact contract (tags above are the artifact's field names, additive evolution only).

- [ ] **Step 4: Run to verify pass** — same command → PASS.
- [ ] **Step 5: Sweep (root) + commit** — `git add manifest.go manifest_test.go`, subject `manifest: Resource/List/Form types with Validate`.

---

### Task 2: TOML loading (`internal/manifest`, BurntSushi dep)

**Files:**
- Create: `internal/manifest/manifest.go`
- Test: `internal/manifest/manifest_test.go`
- Modify: `go.mod`/`go.sum` (add `github.com/BurntSushi/toml`; latest v1.x; run `go get` with the sandbox disabled if the proxy fetch is blocked)

**Interfaces:**
- Consumes: Task 1's types + `Validate`.
- Produces:

```go
// Load reads every *.toml manifest in dir plus every Go manifest
// (Task 3 adds the Go half; until then goEval is a stub returning nil).
// It validates each resource and rejects duplicate names/routes.
func Load(dir string) ([]rastrillo.Resource, error)

// decodeTOML is the strict single-file decoder (unknown keys error).
func decodeTOML(path string) (rastrillo.Resource, error)
```

- [ ] **Step 1: Write the failing tests** — fixtures via `t.TempDir()` + `os.WriteFile`:

```go
func TestDecodeTOMLFixture(t *testing.T)      // the fixture TOML from the plan header decodes to exactly validResource()-shape (compare with reflect.DeepEqual against a hand-built Resource)
func TestDecodeTOMLUnknownKeyErrors(t *testing.T)   // add `colour = "red"` at top level → error naming the key (BurntSushi metadata: undecoded keys are errors)
func TestLoadRejectsDuplicateNames(t *testing.T)    // two files, same name= → error containing both filenames
func TestLoadRejectsDuplicateRoutes(t *testing.T)   // two files, same route= → error containing "route"
func TestLoadValidates(t *testing.T)                // a TOML with store="mergeable" → error containing "not yet built"
func TestLoadNoManifestDirIsEmpty(t *testing.T)     // dir absent → nil, nil (manifests are optional, per route)
```

Write them as real Go with exact fixture strings; assert error substrings.

- [ ] **Step 2: Run to verify failure** (package doesn't exist).
- [ ] **Step 3: Implement.** `decodeTOML` uses `toml.DecodeFile` + `md.Undecoded()` — any undecoded key is an error naming it. `Load` globs `*.toml`, decodes, `Validate`s (wrap errors with the filename), then checks name/route uniqueness across the set. Sorted by `Name` for deterministic downstream output. Package doc: this package owns discovery + the JSON artifact; TOML is the pure-data serialization of the same struct (§3).
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — subject `manifest: strict TOML loading with BurntSushi`.

---

### Task 3: Go-manifest evaluation (go/ast + driver) and the JSON artifact

**Files:**
- Modify: `internal/manifest/manifest.go` (wire `goEval` into `Load`)
- Create: `internal/manifest/goeval.go`
- Test: `internal/manifest/goeval_test.go`

**Interfaces:**
- Consumes: Task 1's types (JSON tags are the artifact).
- Produces:

```go
// evalGo finds exported package-level vars of type rastrillo.Resource
// in dir's *.go files (via go/ast — the type must be spelled
// rastrillo.Resource), generates a driver main that imports the app's
// manifest package, runs it with `go run` in moduleRoot, and decodes
// the JSON it prints. A compile error in the driver run is returned
// verbatim — a typo'd manifest IS that compile error.
func evalGo(moduleRoot, dir string) ([]rastrillo.Resource, error)

// Artifact renders the full resource set as the stable JSON artifact
// (two-space indent, trailing newline, resources sorted by name).
func Artifact(rs []rastrillo.Resource) []byte
```

`Load(dir)` becomes `Load(moduleRoot, dir string)` — it merges TOML + Go
sources before the uniqueness checks. (Update Task 2's tests' call
sites; behavior otherwise unchanged.)

- [ ] **Step 1: Write the failing tests:**

```go
func TestEvalGoFindsExportedResources(t *testing.T)
// t.TempDir() gets a full tiny module: go.mod (module scratch; go 1.25;
// require github.com/carlosframework/rastrillo v0.0.0 + replace → the
// repo root, computed absolute), manifest/notes.go declaring
// `var Notes = rastrillo.Resource{...fixture...}`. evalGo returns it.

func TestEvalGoCompileErrorSurfaces(t *testing.T)
// manifest/broken.go with a typo'd field (`Serach: true`) → error
// containing "Serach" (the compiler's message, verbatim).

func TestEvalGoIgnoresUnexportedAndOtherTypes(t *testing.T)
// unexported var + an int var + a rastrillo.List var → none returned.

func TestGoAndTOMLSameManifestSameArtifact(t *testing.T)
// The equivalence gate: fixture as TOML in one dir, as Go in another,
// Load each → Artifact bytes are identical.

func TestArtifactIsStableAndSorted(t *testing.T)
// two resources given in reverse order → sorted by name; golden string
// for the fixture resource pinned in the test (the documented artifact).
```

The golden artifact string for the fixture (pin exactly this, from the
Task 1 JSON tags — write it in the test verbatim):

```json
[
  {
    "name": "notes",
    "route": "/admin/notes",
    "store": "exclusive",
    "list": {
      "columns": [
        { "field": "Title", "kind": "text" },
        { "field": "Price", "kind": "money" }
      ],
      "search": true,
      "filter": ["Title"]
    },
    "form": {
      "basics": [
        { "name": "Title", "kind": "text" },
        { "name": "Body", "kind": "textarea" }
      ],
      "advanced": [{ "name": "Price", "kind": "money" }]
    }
  }
]
```

(Kinds appear normalized — `Validate` fills zero values with `text` —
and `json.MarshalIndent` with two-space indent; adjust the golden's
whitespace to Go's marshaler, not the other way.)

- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement `goeval.go`.** go/ast parse of `dir/*.go` (skip `_test.go`): collect exported `var X = rastrillo.Resource{...}` or `var X rastrillo.Resource` names (selector check: package alias resolved from the file's imports of the rastrillo module path). Empty → return nil without running anything. Driver: write to `t := os.MkdirTemp` a `main.go`:

```go
package main

import (
	"encoding/json"
	"os"

	m "<modulePath>/manifest"
	"github.com/carlosframework/rastrillo"
)

func main() {
	rs := []rastrillo.Resource{m.Notes /* , one per discovered var */}
	json.NewEncoder(os.Stdout).Encode(rs)
}
```

`<modulePath>` read from moduleRoot's go.mod (`golang.org/x/mod/modfile` is already an indirect dep of the toolchain — use `modfile.ModulePath(bytes)` if the module can take the direct dep cheaply, else parse the `module ` line by hand; hand-parse is fine and dependency-free — do that). Run `go run <driver>` with `cmd.Dir = moduleRoot`, env passthrough + the sweep's GOCACHE/GOFLAGS if set. stderr on failure → the returned error, verbatim. Decode stdout into `[]rastrillo.Resource`, then `Validate` each (same as TOML path — validation lives in ONE place: `Load` validates after merging, not each source loader). `Artifact`: sort by name, `json.MarshalIndent(rs, "", "  ")` + "\n".
- [ ] **Step 4: Run to verify pass** (these tests run `go run` — allow ~30s; if the sandbox blocks the nested go invocation's cache, rerun with sandbox disabled).
- [ ] **Step 5: Sweep (root) + commit** — subject `manifest: Go-source evaluation via go/ast driver; stable JSON artifact`.

---

### Task 4: `detail-list` partial (ui library)

**Files:**
- Create: `ui/partials/detail-list.html`
- Modify: `ui/tokens.css` (new section after the field family), `examples/blog/static/tokens.css` (byte-identical mirror)
- Modify: `ui/ui.go` (package doc partial inventory: twelve → thirteen)
- Test: `ui/ui_test.go`

**Interfaces:**
- Produces: `{{define "detail-list"}}` — keys `Items` (required; list of dicts: `Label` string, `Value` string, `Mono` bool optional). Task 7's Show template calls it.

- [ ] **Step 1: Write the failing tests:**

```go
func TestDetailListRendersLabelValueRows(t *testing.T) {
	got := render(t, "detail-list", map[string]any{
		"Items": []any{
			map[string]any{"Label": "Title", "Value": "Hello"},
			map[string]any{"Label": "Price", "Value": "$1.00", "Mono": true},
		},
	})
	for _, want := range []string{
		`<dl class="rst-detail">`,
		`<dt>Title</dt>`, `<dd>Hello</dd>`,
		`<dt>Price</dt>`, `<dd class="rst-mono">$1.00</dd>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestDetailListEmptyItemsRendersEmptyList(t *testing.T) {
	got := render(t, "detail-list", map[string]any{"Items": []any{}})
	if !strings.Contains(got, `<dl class="rst-detail">`) || strings.Contains(got, "<dt>") {
		t.Errorf("empty detail-list wrong: %s", got)
	}
}
```

- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Write the partial** (contract comment in house style: a record's labelled facts as a definition list — the Show state's body; `Mono` marks machine-ish values):

```html
{{define "detail-list"}}<dl class="rst-detail">
  {{- range .Items}}
  <dt>{{.Label}}</dt>
  <dd{{if .Mono}} class="rst-mono"{{end}}>{{.Value}}</dd>
  {{- end}}
</dl>{{end}}
```

CSS (new banner section, alphabetical props; grid two-column, dt muted small caps-ish per the label conventions already in `.rst-field__label`):

```css
/* ── detail-list ──────────────────────────────────────────────────── */
.rst-detail {
  display: grid;
  gap: var(--rst-sp-2) var(--rst-sp-5);
  grid-template-columns: max-content 1fr;
  margin: 0;
}
.rst-detail dt {
  color: var(--rst-text-muted);
  font-size: var(--rst-fs-sm);
  font-weight: 600;
}
.rst-detail dd {
  margin: 0;
}
.rst-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: var(--rst-fs-sm);
}
```

Mirror into the blog's vendored copy; update ui.go's inventory sentence (thirteen partials, add detail-list to the list).
- [ ] **Step 4: Run to verify pass** (root ui tests + the blog's vendored-css test).
- [ ] **Step 5: Sweep (both modules) + commit** — subject `ui: detail-list — the Show state's labelled facts`.

---

### Task 5: store emitter — schema.sql, queries.sql, sqlc.yaml, migrations.go

**Files:**
- Create: `internal/generate/store.go`
- Test: `internal/generate/store_test.go`

**Interfaces:**
- Consumes: `rastrillo.Resource` (validated, kinds normalized).
- Produces:

```go
// EmitStore writes gen/store/sqlc.yaml (one config covering every
// resource) and per resource gen/store/<name>/{schema.sql,queries.sql,
// migrations.go}. Returns the emitted file paths (for idempotency and
// skip accounting). Pure text generation — running sqlc is Task 6.
func EmitStore(genDir string, rs []rastrillo.Resource) ([]string, error)
```

- [ ] **Step 1: Write the failing golden tests.** Golden content pinned in the test as consts (exact — this IS the output contract). For the fixture resource:

`schema.sql` — column set = union of List columns and Form fields in
first-declaration order (List columns first, then Form basics, then
advanced, skipping names already present — for the fixture: Title,
Price from the list, then Body). SQL column names via
`sqlName("MaxPerOrder") == "max_per_order"` (insert `_` before interior
uppercase runs, lowercase everything). Kind→type: text/textarea →
`TEXT NOT NULL DEFAULT ''`, money → `INTEGER NOT NULL DEFAULT 0`:

```sql
-- Code generated by rastrillo generate; DO NOT EDIT.
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

`queries.sql` (sqlc named queries; search LIKE over text/textarea
columns declared in List; filter equality per Filter entry; both
optional via sqlc `@`-style args — use the `?`-positional form sqlc
supports for sqlite with `sqlc.arg()`):

```sql
-- Code generated by rastrillo generate; DO NOT EDIT.

-- name: ListNotes :many
SELECT * FROM notes
WHERE (sqlc.arg(search) = '' OR title LIKE '%' || sqlc.arg(search) || '%')
  AND (sqlc.arg(filter_title) = '' OR title = sqlc.arg(filter_title))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountNotes :one
SELECT COUNT(*) FROM notes
WHERE (sqlc.arg(search) = '' OR title LIKE '%' || sqlc.arg(search) || '%')
  AND (sqlc.arg(filter_title) = '' OR title = sqlc.arg(filter_title));

-- name: GetNote :one
SELECT * FROM notes WHERE id = sqlc.arg(id);

-- name: CreateNote :one
INSERT INTO notes (title, price, body, created_at, updated_at)
VALUES (sqlc.arg(title), sqlc.arg(price), sqlc.arg(body), sqlc.arg(now), sqlc.arg(now2))
RETURNING id;

-- name: UpdateNoteBasics :exec
UPDATE notes SET title = sqlc.arg(title), body = sqlc.arg(body), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: UpdateNoteAdvanced :exec
UPDATE notes SET price = sqlc.arg(price), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);
```

(Singularization for query names: strip one trailing `s` if present,
else use the name as-is, title-cased — `notes` → `Note`. Search clause
only when `Search: true`; one `filter_<col>` arg per Filter entry;
`UpdateNoteAdvanced` only when Advanced is non-empty. If sqlc's sqlite
engine rejects `sqlc.arg()` in any of these positions at Task 6, the
fallback contract is named `?` params with a documented arg order —
Task 6 owns reconciling this golden with what sqlc actually accepts,
and MUST come back and update this task's goldens in the same commit.)

`sqlc.yaml` (at `gen/store/sqlc.yaml`, one `sql:` entry per resource):

```yaml
# Code generated by rastrillo generate; DO NOT EDIT.
version: "2"
sql:
  - engine: "sqlite"
    schema: "notes/schema.sql"
    queries: "notes/queries.sql"
    gen:
      go:
        package: "notesstore"
        out: "notes"
```

`migrations.go` (in `gen/store/notes/`, package `notesstore`):

```go
// Code generated by rastrillo generate; DO NOT EDIT.
package notesstore

// Migrations is the schema for Options.Migrations. Additive changes
// beyond this initial CREATE are the app's own migrations.
var Migrations = []string{`CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`}
```

(schema.sql says `CREATE TABLE` — sqlc wants the clean DDL; Migrations
says `IF NOT EXISTS` — Serve reruns migrations at boot. Both generated
from one internal columns model so they cannot drift.)

Tests: golden comparison per file; a `Money` field kind mapped through
`sqlName`/type table asserting INTEGER (the boundary rule lives here:
emitter has a single Kind→SQL map and unknown Kind panics — Validate
guarantees it never fires); idempotency (double EmitStore into the same
dir → identical bytes, no spurious rewrites — compare file contents).

- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** (one internal `columns(r)` model shared by schema/queries/migrations; all writes via a helper that only rewrites on content change, reused later for idempotency).
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — subject `generate: store emitter — schema, sqlc queries, migrations`.

---

### Task 6: sqlc invocation + a compiled-store integration test

**Files:**
- Create: `internal/generate/sqlcrun.go`
- Test: `internal/generate/sqlcrun_test.go`

**Interfaces:**
- Consumes: Task 5's emitted `gen/store/` tree.
- Produces:

```go
// RunSqlc executes `go tool sqlc generate -f gen/store/sqlc.yaml` in
// moduleRoot. The app's go.mod must carry the sqlc tool directive; a
// missing tool is an error whose text says exactly what to add:
//   go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc
func RunSqlc(moduleRoot string) error
```

- [ ] **Step 1: Write the failing tests:**

```go
func TestRunSqlcMissingToolSaysHowToAddIt(t *testing.T)
// temp module WITHOUT the tool directive → error containing
// "go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc".

func TestRunSqlcGeneratesCompilingStore(t *testing.T) {
	// The slice's heaviest integration test, and worth it:
	// temp module (go.mod with rastrillo replace + sqlc tool directive
	// — run `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc` in the
	// temp module as part of the fixture setup, network needed),
	// EmitStore(fixture), RunSqlc, then `go build ./...` in the temp
	// module. Skip with t.Skip if `go get` fails for network reasons,
	// BUT: the blog adoption (Task 10) exercises the same path in CI,
	// so a skip here is not silent coverage loss.
}
```

- [ ] **Step 2: Run to verify failure** (package/function missing).
- [ ] **Step 3: Implement.** Check the tool is present (`go tool` list output contains `sqlc`), error with the exact `go get -tool …` line if not; then run `go tool sqlc generate -f <genDir>/store/sqlc.yaml`, `cmd.Dir = moduleRoot`, stderr → error verbatim. **If sqlc rejects any Task 5 query syntax** (sqlite engine + `sqlc.arg`), fix the Task 5 emitter + goldens in THIS commit to the closest accepted form (named args preferred, positional `?` as last resort with arg order documented in queries.sql's header comment) — the two tasks' outputs must agree by the end of this task.
- [ ] **Step 4: Run to verify pass** (sandbox likely disabled for the network fetch; keep the module-cache writes in mind).
- [ ] **Step 5: Sweep (root) + commit** — subject `generate: run sqlc via the app's go tool directive`.

---

### Task 7: template emitter — list.html, show.html, form.html

**Files:**
- Create: `internal/generate/templates.go`
- Test: `internal/generate/templates_test.go`

**Interfaces:**
- Consumes: `rastrillo.Resource`; the ui partials incl. Task 4's `detail-list`.
- Produces:

```go
// EmitTemplates writes gen/templates/<name>/{list,show,form}.html.
// A hand-written templates/<name>/<file>.html in the app skips that
// file (skip list returned for --check reporting).
func EmitTemplates(appRoot, genDir string, r rastrillo.Resource) (written, skipped []string, err error)
```

Generated templates define `content` the way the blog's pages do and
render every visible string through `{{T "resource.<name>...."}}` keys.
The exact list.html golden for the fixture (pin verbatim; the others
follow the same discipline):

```html
{{/* Code generated by rastrillo generate; DO NOT EDIT.
     Eject: copy to templates/notes/list.html and edit — generation of
     this one file then stops. */}}
{{define "content"}}
{{template "page-header" dict "Title" (T "resource.notes.name") "ActionHref" "/admin/notes/new" "ActionLabel" (T "ui.new")}}
{{if .Empty}}
{{template "empty-state" dict "Title" (T "resource.notes.empty.title") "Body" (T "resource.notes.empty.body") "ActionHref" "/admin/notes/new" "ActionLabel" (T "ui.new")}}
{{else}}
<div class="rst-list">
{{template "list-bar" dict "SearchAction" "/admin/notes" "Query" .Query "Placeholder" (T "ui.search") "Hidden" .Carry "Filter" .Filter}}
{{range .Rows}}{{template "list-row-action" dict "Href" .Href "Main" .Main "Sub" .Sub}}
{{end}}
</div>
{{if .Pagination.Show}}{{template "pagination" dict "Items" .Pagination.Items}}{{end}}
{{end}}
{{end}}
```

show.html: page-header (record's first text field as Title, edit action
pill) + `detail-list` over every declared field (Money rendered by the
action into a formatted string). form.html: `<form class="rst-form">`
with field-text/field-textarea per Kind for Basics; when Advanced is
non-empty, a second `<form>` posting to edit-basics/edit-advanced
targets respectively (New posts everything to the create route as one
form using Basics+Advanced fields); form-foot with `(T "ui.save")` /
cancel to the list. `{{T}}`/`Tf` availability: generated templates are
parsed into the app's tree exactly like hand templates — the blog
already binds T per locale (ui runtime), and `ui.Funcs()` carries dict;
the emitter targets that environment and the golden test parses the
emitted file with `ui.Funcs()` + a stub `T` to prove it compiles as a
template.

- [ ] **Step 1: Write the failing golden tests** — golden for all three files (write show.html and form.html goldens by the rules above — every visible string a `T` call, zero `<script`, every partial name one the library ships); plus:

```go
func TestEmitTemplatesSkipsEjectedFiles(t *testing.T)
// touch appRoot/templates/notes/list.html first → list.html reported
// skipped, not written; show/form still written.

func TestEmittedTemplatesParse(t *testing.T)
// template.New("").Funcs(ui.Funcs()).Funcs(FuncMap{"T": func(s string) string { return s }}).
// ParseFS over ui.Templates() then the emitted dir — no error.
```

- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** (text/template-driven emitter or string building — mirror whichever style `Router` in generate.go already uses; content-change-only writes as Task 5).
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — subject `generate: template emitter — the four states' screens`.

---

### Task 8: action emitter — the seven files

**Files:**
- Create: `internal/generate/actions.go`
- Test: `internal/generate/actions_test.go`

**Interfaces:**
- Consumes: Task 5's store package names/query names; Task 7's template names; `internal/generate.Discover`'s existing conventions (route mapping, `[id]` params, `//go:build rastrillo_actions` never applies here — generated actions go straight into `gen/actions/`, compiled normally, mirroring what `Rewrite` produces for hand actions).
- Produces:

```go
// EmitActions writes gen/actions/<route path>/... for r: index.GET,
// index.POST, new.GET, [id]/index.GET, [id]/edit.GET,
// [id]/edit-basics.POST, [id]/edit-advanced.POST (last one only when
// Advanced is non-empty). A hand-written actions/<same path> skips
// that file. Returns written/skipped like EmitTemplates.
func EmitActions(appRoot, genDir string, r rastrillo.Resource) (written, skipped []string, err error)
```

Each generated action is a normal `Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)` in its own package (existing `genDirFor`/`packageNameFor` conventions), importing the resource's store package and the app's render helper — **the render seam:** generated actions call `rastrillo.RenderPage(ctx, w, "<template name>", status, data)`? No such helper exists. The blog has its own `blog.Render`. Resolve it the way the spec's coexistence rule demands (generated code cannot depend on app-private helpers): **this task adds the tiny exported runtime hook to the root package:**

```go
// RenderFunc is how generated actions hand a page to the app's
// template tree. Options.Render supplies it; generated actions are the
// only framework code that calls it. (manifest.go, root package)
type RenderFunc func(ctx *Ctx, w http.ResponseWriter, page string, status int, data any)
```

wired as `Options.Render RenderFunc` (additive; serve.go threads it
onto `Ctx.render` — unexported field — at Ctx creation? `Ctx` is
app-constructed. Simplest honest seam given rastrillo's app-owns-Ctx
shape: generated actions call a package-level variable the app sets:
NO. Decision, recorded: generated actions receive the renderer via
`Ctx`: `Ctx` gains an exported optional field `Render RenderFunc`; the
app's ctx factory sets it (`&rastrillo.Ctx{DB: db, Render: blog.Render,
…}`). Generated actions nil-check it and 500 with a clear log line if
unset. The field is documented in ctx.go beside its peers.)

Action behaviors (exact contract, encode in goldens):
- `index.GET`: parse `q`, per-filter params, `page`; call List/Count with limit 10 offset; build `Rows` (Href `<route>/<id>`, Main = first text column's value, Sub = second column formatted — Money as dollars `$%d.%02d` cents split), `Filter` dropdown data when Filter declared (all values present in the column — v1: filter links carry `?<col>=<value>` for the distinct values? NO — YAGNI: v1 filter is a free-text equality via the search form's Hidden carry; the dropdown requires a value enumeration the manifest doesn't declare. **Correction, binding:** v1 generated list has Search only; `Filter` manifests validate but the generated list renders no filter control until a later slice adds declared filter values. Update Task 7's list.html golden: no `"Filter"` key. Record in the plan self-review.)
- `index.POST` (create): parse Basics+Advanced fields (money input parsed as decimal dollars into cents, rejecting >2 decimals), timestamps now UTC RFC3339, redirect 303 to Show.
- `[id]/index.GET` (show): Get or 404; data rows for detail-list (Money formatted).
- `new.GET`/`[id]/edit.GET`: render form.html with current values (edit) or zero values (new).
- `edit-basics.POST`/`edit-advanced.POST`: parse own field group only, update, redirect 303 back to Show. 400 re-render on a Money parse failure with the field's error in the form (field partial's `Error` key).

- [ ] **Step 1: Write the failing golden tests** — golden for `index.GET.go` and `edit-basics.POST.go` (the two hardest; pin full file contents you write to the contract above); written/skipped behavior test (hand file at `actions/admin/notes/index.GET.go` skips); a compile test: emitted actions + a stub store package compile (`go build` in a temp module fixture mirroring Task 6's).
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** (`ctx.go` gains the `Render RenderFunc` field with its constraint comment; emitter mirrors Task 5/7 write-on-change).
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — subject `generate: action emitter + Ctx.Render seam`.

---

### Task 9: locales fragment + orchestration + `--check` gates

**Files:**
- Create: `internal/generate/manifestgen.go` (orchestrator), `internal/generate/manifestlocales.go`
- Modify: `cmd/rastrillo/generate.go` (wire manifests into the generate flow), `serve.go` (+`Options.BaseCatalog Catalog` → `NewLocales` base param)
- Test: `internal/generate/manifestgen_test.go`, plus a serve_test addition

**Interfaces:**
- Consumes: everything above (`manifest.Load`, `Artifact`, `EmitStore`, `RunSqlc`, `EmitActions`, `EmitTemplates`).
- Produces:

```go
// GenerateManifests is the one entry cmd/rastrillo calls: load,
// artifact → gen/manifest.json, all emitters, sqlc, then the checks.
// check-only mode runs everything into a temp dir and diffs against
// gen/ (idempotency + collision without touching the tree).
func GenerateManifests(moduleRoot, genDir string, checkOnly bool) error
```

- Locales: `EmitLocales(genDir, defaultLocale string, rs []…) error` writes `gen/locales/<default>.toml` — keys `resource.<name>.name`, `.field.<f>`, `.empty.title`, `.empty.body`, values title-cased from identifiers (`MaxPerOrder` → `Max per order`; simple: split camel humps, lower-case tail words). Plus the tiny shared `ui.*` keys the templates use (`ui.new`, `ui.save`, `ui.search`, `ui.cancel`, `ui.edit`) — emitted once, same file.
- `serve.go`: `Options.BaseCatalog Catalog` (additive) → passed as `NewLocales`'s base argument (currently nil). Generated fragment loads via a generated `gen/locales/locales.go` exposing `var BaseCatalog = rastrillo.Catalog{…}` (parse-free at runtime: the TOML file is for humans/translators to read, the Go var is what the app wires — both emitted from one map, cannot drift).
- Collisions: generated routes join the existing `Discover` collision check (hand action route == generated action route → build failure, reusing the existing Collision reporting); file-level: a hand file at a generated path is a SKIP (allowed), but a generated file colliding with another GENERATED file (two resources computing the same path) is an error.
- Idempotency: `--check` re-runs all emitters into `os.MkdirTemp`, byte-compares against `gen/` (missing/extra/differing file → error listing them). Wire into the existing `generate --check` alongside the i18n gate.
- Default locale for EmitLocales: from the app's declared default if discoverable — it is not (Options is runtime). Emit `en` always; document: the fragment is authored in en; apps with a different default copy keys into their own catalogs (spec's "layered under app catalogs" holds via BaseCatalog regardless of locale set).

- [ ] **Step 1: Write the failing tests** — fixture module (reuse Task 6's fixture builder): full `GenerateManifests` run produces manifest.json + store + actions + templates + locales, second run byte-identical; check-only on a clean tree passes, after a hand-edit to a gen file fails naming it; route collision (hand action at the manifest's route) fails with the existing Collision formatting; `TestBaseCatalogLayersUnderAppCatalog` in root (serve_test.go style): `NewLocales` with base `{"resource.notes.name": "Notes"}` and an app catalog overriding it → app wins.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — subject `generate: manifest orchestration, locales fragment, collision + idempotency gates`.

---

### Task 10: blog adoption I — manifest, generated store, wiring

**Files:**
- Create: `examples/blog/manifest/posts.toml`
- Modify: `examples/blog/go.mod` (sqlc tool directive), `examples/blog/cmd/blog/main.go` (Migrations += generated, Ctx.Render = blog.Render, BaseCatalog), `examples/blog/internal/blog/store.go` (drop what the manifest now covers), regenerated `examples/blog/gen/`
- Test: `examples/blog/internal/blogtest/` — existing suite adapts

**The manifest:**

```toml
name  = "posts"
route = "/admin/posts"
store = "exclusive"

[list]
columns = [{ field = "Title" }]
search  = true

[form]
basics = [{ name = "Title" }, { name = "Body", kind = "textarea" }]
```

**Adoption contract:**
- `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc` in examples/blog (sandbox off for the fetch), regen.
- Generated schema vs existing table: the blog's existing migration owns `posts` (it has `published`); the generated `CREATE TABLE IF NOT EXISTS` is a no-op against it **only if column names/types agree** — verify `title`/`body` match the existing schema (they do: TEXT). Order generated Migrations BEFORE the blog's own in `Options.Migrations` so a fresh DB gets the generated table, then the blog's additive `ALTER TABLE posts ADD COLUMN published …` + its indexes. Rework the blog's migration constant into exactly that additive form (fresh-DB path is what blogtest exercises — this is the compatibility crux; if the blog's current CREATE TABLE can't be decomposed additively without data-shape change, STOP and report BLOCKED with what you found).
- Hand actions that now conflict by route with generated ones (`index.GET` on /admin/posts, edit/new/create) — DELETE the hand versions the manifest replaces, EXCEPT keep: publish/unpublish/delete actions, and the public site untouched. The blog's admin list stays hand-written via TEMPLATE ejection (Task 11), not action skip: its custom filter needs its own action too — **keep the hand `actions/admin/posts/index.GET.go`** (status filter, custom rows) — the file-level skip covers it (generated index.GET is skipped because the hand file exists at the computed path). Verify the skip fires rather than a route collision (same path = skip; collision is only for DIFFERENT files on one route).
- `blog.Render` signature matches `rastrillo.RenderFunc` — confirm; adapt with a tiny wrapper in main.go if the signature differs.
- store.go: `List`/`Count`/`Get`/`Create`/`Update` replaced by generated store calls where actions were replaced; keep publish flips + published-only public queries + PageSize/paging helpers. Update remaining hand actions' imports.

- [ ] **Step 1..N (TDD inside the module):** run the existing blogtest suite as the failing test (it must END green with the hand list + generated form/show flows), adapting tests whose markup changes (the generated form.html replaces admin_new/admin_edit rendering — port the form tests' assertions to the generated markup, keeping every behavioral case: create, 400 on empty title (`Required` is client-side; server validation in generated create is: empty required text field → 400 re-render — confirm the generated action's contract from Task 8 covers required-ness: it does not — **record: v1 generated create accepts empty text; the blog's empty-title 400 test moves to the hand-written publish flow or is retired with a ledger note; flag in the task report**), edit round-trip, show).
- [ ] **Sweep (blog + root) + commit** — subject `blog: posts manifest — generated store, form, show; hand list kept by skip`.

---

### Task 11: blog adoption II — template ejection + coexistence proof

**Files:**
- Create: `examples/blog/templates/posts/list.html` (the EJECTED file — moved from `internal/blog/templates/pages/admin_list.html`, adapted)
- Modify: blog view wiring so ejected templates parse from `templates/` (the blog currently embeds `internal/blog/templates`; adoption means the app-level `templates/` tree joins the parse — extend `blog.Render`'s template setup to ParseFS both trees; generated templates from `gen/templates/` join too)
- Test: blogtest — the full four-state round-trip

**Contract:**
- The ejected list keeps pills, status filter, search — and the emitter reports it SKIPPED (assert via `generate --check` output or the absence of `gen/templates/posts/list.html`).
- Generated `show.html`/`form.html` render through the same layout; blogtest asserts: list (hand) → show (generated) → edit (generated) → save basics → back to show; create via generated form; search still works; publish/unpublish/delete still work (hand actions).
- `generate --check` passes: collision clean, idempotent, i18n complete (generated keys covered by BaseCatalog; the blog's `fr` locale — if one exists, check — must gain the resource keys or the check message documents the gap; follow what the check reports).
- [ ] Steps: failing blogtest first (four-state round-trip), implement wiring, suite green, sweep (blog), commit — subject `blog: ejected list + generated show/form — the coexistence proof`.

---

### Task 12: docs

**Files:**
- Modify: `README.md` (a "Manifests" section: the fixture TOML, what generates, eject/skip rules, the sqlc tool directive, the JSON artifact and what it's for), `examples/blog/README.md` (adoption notes: what's generated, what's ejected, why published is an app migration), `ui/ui.go` already updated in Task 4.
- [ ] Write, sweep both modules, commit — subject `docs: manifests — generate, eject, coexist`.

---

## Verification (whole slice)

1. Root + examples/blog + examples/helloworld sweeps green; helloworld untouched (no manifest dir → generator no-ops — verify by running generate in helloworld and diffing gen/).
2. `rastrillo generate --check` green in the blog; second `generate` run produces no diff (`git diff --exit-code examples/blog/gen`).
3. Manual smoke via `rastrillo dev` in examples/blog: create a note— er, post — via the generated form, edit basics, view show, search the hand list, publish via hand action.
4. `gen/manifest.json` exists in the blog and matches the artifact golden shape.
5. Push branch, open draft PR titled "manifest system slice 1: Resource → four generated states (sqlc, actions, templates, locales)".

## Self-review notes

- **Corrections folded in during writing:** (a) generated list has Search only — `Filter` validates but renders no control in v1 (the manifest doesn't declare filter VALUES; a dropdown needs them); Task 7's golden must NOT pass a Filter key, and the spec's list-bar mention is satisfied by search alone — record this as a spec deviation in the final PR body. (b) Task 8's render seam decision recorded inline: `Ctx.Render` field, app-set. (c) v1 generated create has no server-side required-field validation — Task 10 flags the blog's empty-title test consequence in its report; the ledger carries it to the final review.
- Type consistency: `manifest.Load(moduleRoot, dir)` (Task 3 signature) is what Task 9's orchestrator calls; store package name `<name>store`; query names singularized; `RenderFunc`/`Ctx.Render` used in Tasks 8/10.
- Spec coverage: types+validation (T1), TOML (T2), Go eval + artifact (T3), detail-list (T4), store+sqlc (T5-6), templates (T7), actions (T8), locales+checks+orchestration (T9), blog adoption+ejection (T10-11), docs (T12). Out-of-scope list untouched.
- Delegated judgment with guardrails: sqlc arg-syntax reconciliation (T6 owns updating T5 goldens in-commit); blog migration decomposition (T10 BLOCKED-stop rule); `blog.Render` signature adaptation (T10); i18n check outcome for extra locales (T11 "follow what the check reports").
