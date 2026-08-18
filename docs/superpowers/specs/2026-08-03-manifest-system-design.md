# The manifest system, slice 1 — design

**Date:** 2026-08-03
**Status:** approved (design review); spec pending user review
**Delivers:** design doc §9's headline — "a `rastrillo.Resource{List: ..., Form: ...}`
generates all four canonical states (Blank → List → Show → Edit/New)
without the author naming them individually" — for the Exclusive-store,
minimal-Kind subset, proven by partial adoption in `examples/blog`.
(Design doc: `carlosframework/platform`
`docs/superpowers/specs/2026-08-01-carlos-framework-design.md`.)

## Decisions recorded

1. **TOML library: BurntSushi/toml** — closes design-doc open question 1
   (§15). Manifests need tables, inline tables and arrays; that grammar
   is no longer "a page of code" to hand-roll, and BurntSushi/toml is
   mature with zero transitive dependencies. `internal/catalog` stays
   hand-rolled for flat locale files (its package doc already says why).
2. **sqlc, now.** The doc says "sqlc glue" and this slice follows it
   literally. sqlc is version-pinned via a Go 1.24+ `tool` directive in
   the app's go.mod and invoked as `go tool sqlc` from `rastrillo
   generate` — no unmanaged binary, upgrades reviewed like any
   dependency bump.
3. **Both manifest sources from day one.** TOML (`manifest/*.toml`) and
   typed Go (`manifest/*.go`) both work in this slice; §2's "the
   canonical manifest is typed Go" holds from the start.
4. **All four states, minimal Kinds.** `Text`, `Textarea`, `Money`
   (integer cents). `Meter`, `Blob`, and function-valued `Render` defer.
   `Mergeable` parses but fails validation with a "not yet built" error.
5. **Delete is not generated.** Destructive actions require their own
   confirm-page route (§9) and the `confirm` partial is not in the
   library yet; delete + confirm is a later vocabulary slice. Apps keep
   hand-writing delete actions (the blog already does).
6. **The evaluated manifest is a stable, documented JSON artifact** —
   not a private wire format between the Go-manifest driver and the
   generator. Field names are chosen deliberately and versioned
   additively, so any future renderer (a SwiftUI/native skeleton
   generator is the recorded aspiration; the LLM tool schema of §8 is
   another) can consume the same artifact without touching the Go
   evaluator. Native UI generation itself is out of scope here; this
   decision is what keeps it cheap later.
7. **Blog proves the slice by partial adoption**, exercising
   coexistence and ejection — not by being fully generated.

## Types (root package `rastrillo`)

```go
type Resource struct {
	Name  string // snake_case; the table name, route noun, and key prefix
	Route string // e.g. "/admin/posts"; well-formed, no trailing slash
	Store StoreKind
	List  List
	Form  Form
}

type StoreKind string // Exclusive | Mergeable (Mergeable: validation error, slice 1)

type List struct {
	Columns []Column
	Search  bool     // GET round-trip search over Text/Textarea columns
	Filter  []string // field names; each must be a declared Column
}

type Column struct {
	Field string
	Kind  Kind // zero value = Text
}

type Form struct {
	Basics   []Field
	Advanced []Field // optional; generates its own POST action when non-empty
}

type Field struct {
	Name string
	Kind Kind
}

type Kind string // Text | Textarea | Money
```

`(*Resource).Validate() error` is the single validation home, used
identically for TOML- and Go-sourced manifests: snake_case `Name`,
well-formed `Route`, at least one List column or Form field, `Filter`
entries declared as columns, Kinds in vocabulary, Store in vocabulary
(`Mergeable` → "not yet built"). Money's integer-only rule is enforced
at codegen (the SQL/Go boundary), not here.

## Discovery and evaluation (`internal/manifest`)

- **TOML:** every `manifest/*.toml` decodes via BurntSushi/toml directly
  into `rastrillo.Resource` (strict: unknown keys are errors).
- **Go:** the generator parses `manifest/*.go` with `go/ast` to find
  exported package-level vars of type `rastrillo.Resource`, writes a
  temp driver `main` that imports the app's manifest package and prints
  those vars as the JSON artifact, and `go run`s it. A typo'd field is
  a compile error of the driver run, surfaced verbatim (§2: generate
  and recompile, never interpret).
- Both sources merge into one resource set; two resources sharing a
  `Name` (or a `Route`) is a build failure regardless of source.
- **The JSON artifact** (one document, `[]Resource` in defined order) is
  documented in the package doc and written to `gen/manifest.json` on
  every generate, so external renderers consume a committed file, not a
  process pipe. Additive evolution only.

## Codegen (`rastrillo generate`, scaffolding-with-skip into `gen/`)

Per resource (using `posts`, route `/admin/posts` as the running
example):

### Store: `gen/store/posts/`

- `schema.sql` — `CREATE TABLE posts (id INTEGER PRIMARY KEY, <declared
  columns>, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`;
  Kind→SQL: Text/Textarea → `TEXT NOT NULL DEFAULT ''`, Money →
  `INTEGER NOT NULL DEFAULT 0`. A Money field mapping to anything but
  INTEGER is a generation error.
- `queries.sql` — sqlc-annotated: `List` + `Count` (optional search
  `LIKE` over text columns and equality filters over `Filter` fields,
  compose with AND; ordered `created_at DESC, id DESC`; LIMIT/OFFSET),
  `Get`, `Create` (all fields), `UpdateBasics`, `UpdateAdvanced` (each
  scoped to exactly its own field group — a basics save can never
  clobber an advanced setting, by construction).
- sqlc config generated at `gen/store/sqlc.yaml`; `rastrillo generate`
  runs `go tool sqlc generate` over it; output Go lands beside the SQL.
- `gen/store/posts/migrations.go` exports the schema as a migrations
  slice the app appends to `Options.Migrations`. Schema changes beyond
  the initial CREATE are the app's own additive migrations (the
  additive-migration vet gate is a later slice).

### Actions: `gen/actions/admin/posts/`

Standard signature, standard file conventions, one file per action:

| File | Route | Renders/does |
|---|---|---|
| `index.GET.go` | `GET /admin/posts` | List + Blank (search/filter/pagination; empty-state when truly empty) |
| `index.POST.go` | `POST /admin/posts` | Create from the New form; redirect to Show |
| `new.GET.go` | `GET /admin/posts/new` | New form |
| `[id]/index.GET.go` | `GET /admin/posts/{id}` | Show |
| `[id]/edit.GET.go` | `GET /admin/posts/{id}/edit` | Edit form(s) |
| `[id]/edit-basics.POST.go` | `POST .../edit-basics` | Save basics fields only |
| `[id]/edit-advanced.POST.go` | `POST .../edit-advanced` | Save advanced fields only (generated only when Advanced is non-empty) |

A hand-written file at the same computed path in `actions/` skips that
one file's generation; a hand-written and generated path landing on the
same **route** is a build failure (collision check). Generated actions
use the DB handle from `Ctx` exactly as hand-written ones do.

### Templates: `gen/templates/posts/`

`list.html`, `show.html`, `form.html` (New and Edit share it) —
`html/template` files composing the shipped partials by name:
list-bar (+ `Filter` dropdown when the manifest declares filters),
list-row-action, pagination, empty-state, field-text, field-textarea,
form-foot, page-header, and **detail-list** for Show. `detail-list`
does not exist yet: **this slice adds it to `ui/partials/`** (label →
value rows in the established conventions; the one library gap).
Override-by-existence: a hand-written file at the computed
`templates/posts/<name>.html` path stops regeneration of that file.

### Localization: `gen/locales/<default>.toml`

`resource.posts.name`, `resource.posts.field.title`, … — values default
to title-cased identifiers, layered **under** the app's catalogs so any
app entry wins. Generated templates render labels through `{{T}}`; the
existing catalog-completeness gate then covers generated keys for every
declared locale.

### Checks (`rastrillo generate --check` additions)

- **Collision:** hand-written vs generated path on the same route or
  file — build failure (extends the existing route-collision check).
- **Idempotency:** re-running generate with no manifest change produces
  a byte-identical `gen/` tree.
- **Money boundary:** Money fields must land as INTEGER.

`rastrillo dev` already watches `manifest/`; a manifest edit re-runs the
whole loop.

## Blog adoption (the proof)

- `examples/blog/manifest/posts.toml`: List (Title text column,
  `search = true`), Form basics `title` (text) + `body` (textarea).
- Generated Show/Edit/New screens and CRUD actions are used **as-is**;
  the blog's hand-written admin list stays by **ejection** — its file
  at the computed template path (status pills, All/Drafts/Published
  filter) wins, exercising override-by-existence for real.
- `publish`/`unpublish`/`delete` remain hand-written actions on the
  same tree; the collision check passing is the coexistence proof.
- The `published` column stays a blog-owned additive migration layered
  on the generated schema (status is not in this slice's vocabulary).
- The blog's existing hand-rolled `internal/blog/store.go` shrinks to
  what the manifest doesn't cover (publish flips, published-only public
  queries).

## Testing

- Unit: `Validate` table; TOML/Go equivalence (the same manifest
  expressed both ways yields identical JSON artifact and identical
  `gen/` output); driver compile-error surfacing.
- Golden files: one fixture manifest → committed expected `gen/` tree;
  idempotency asserted by double-run byte comparison.
- Collision and skip: fixtures with a hand-written action/template at a
  computed path.
- sqlc output compiles and its queries run: covered by the blog's suite
  (blogtest exercises the generated screens end to end: create, edit
  basics, show, list search).
- The zero-JS property holds by construction (no `<script>` in
  generated templates), asserted the same way ui tests already do.

## Out of scope (later slices)

Mergeable store and `Derive`; `Meter`/`Blob`/`Render` function values;
LLM tool schemas (§8); delete + the `confirm` partial; additive-
migration, mergeable-convergence, line-cap, and zero-JS-headless vet
gates; sqlc dialect-portability lint; JSON API rendering of generated
actions; **native (SwiftUI) skeleton generation** — enabled by the
stable JSON artifact (decision 6) and desired ("a native app UI
skeleton we could iterate on from the manifest would be hugely
appealing"), but its real prerequisite is the JSON API rendering of
actions, so both belong to one future client-facing slice.
