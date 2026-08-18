# Manifest slice 2: declared filter values + Required — design

**Date:** 2026-08-04
**Status:** approved (design review)
**Extends:** the manifest system (slice 1, merged in #18; spec
`2026-08-03-manifest-system-design.md`). Closes two of its recorded v1
cuts: generated lists gain a real filter control (values are now
declarable), and generated create/edit gain server-side required-field
validation.

## Decisions recorded

1. **`List.Filters []Filter` is a NEW field** — the JSON artifact's
   additive-only contract forbids reshaping the frozen
   `filter []string`, which keeps its validate-only semantics and is
   documented as superseded by `filters`.
2. **At most one `Filters` entry per resource in v1.** list-bar
   composes one dropdown; multi-filter is a later slice. `Validate`
   enforces it.
3. **Required-Money means non-empty input.** "0"/"0.00" are valid (a
   free ticket is a real price); client `required` attribute and server
   check agree exactly.
4. **Proof is a fully generated second adopter**, `examples/tickets` —
   zero hand actions, zero ejections — closing the partial-adoption
   blind spot both slice-1 Criticals hid in. The blog additionally
   adopts `required` on title, un-retiring its empty-title-400 tests
   against generated validation.
5. **Manifest-only apps become legal**: the generator's hard
   requirement of an `actions/` directory (slice-1 deferred wart) is
   removed in this slice because the tickets example needs it.
6. **Action-side error strings stay literal English** in v1 (matching
   the Money parser's precedent); catalog-routed errors are future
   work. Ledger note carries it.

## Types (root package, additive)

```go
// Filter declares one list filter with its selectable values. Field
// must name a declared column; values travel in URLs and become
// translation keys, so they are restricted to [a-z0-9_-]+.
type Filter struct {
	Field  string   `json:"field" toml:"field"`
	Values []string `json:"values" toml:"values"`
}
```

- `List` gains `Filters []Filter` (json/toml tag `filters`).
- `Field` gains `Required bool` (json/toml tag `required`).
- `Validate` additions: each Filters.Field names a declared column;
  Values non-empty, no duplicates, each matching `^[a-z0-9_-]+$`
  (error mentions "value"); at most one Filters entry ("one filter");
  a field may appear in both bare `filter` and `filters` without
  error (the sets union at codegen). `Required` needs no validation.

TOML shape:

```toml
[[list.filters]]
field  = "Status"
values = ["draft", "on_sale", "sold_out"]
```

## Codegen

**Store:** unchanged in shape — the effective filter-field set is the
union of bare `Filter` and `Filters[].Field`, feeding the existing
`filter_<col>` equality args. (A field in `filters` therefore gets its
WHERE clause exactly as bare `filter` fields do today.)

**List action** (generalizing the blog's hand `BuildStatusFilter`
pattern): parse the filter's query param (named by the column's
`sqlName`), normalize against declared values — unknown or absent → ""
(all rows); build dropdown data: summary label = current value's label
or the all-label, `Aria` = "Filter by <field label>: <current>", items
= All + each declared value, hrefs carrying `q` (when search is on)
and the filter param, never `page` (filter click resets paging),
`aria-current` on the applied item. `Carry` gains the filter pair so
searching keeps the filter; pagination hrefs carry both `q` and the
filter param, page last. When no `Filters` is declared, the action and
template are byte-identical to slice 1's output (idempotency-friendly).

**Templates:** list.html passes the action's dropdown data through
list-bar's existing `Filter` key (the F3 seam's first generated
consumer). form.html passes `Required` through to the field partials
(client-side attribute + marker, supported since F2).

**Required validation (create, edit-basics, edit-advanced):** after
parsing, each required field with a blank submitted value gets a field
error ("required" in the string) in the existing `Errors` map and the
action re-renders the form at 400 — the exact path the Money parser
already uses. Money required = blank input only; parse errors keep
their own message. Text/textarea required = `strings.TrimSpace(v) == ""`.

**Locales:** new keys, title-cased fallbacks, emitted by the same
mechanism: `resource.<name>.filter.<field>.<value>` per declared value
(`on_sale` → "On sale"), and the shared `ui.all` ("All"). The
key-set-matches-templates test extends to cover them.

**Checks:** `generate --check`'s idempotency/collision gates unchanged;
the artifact gains the new fields additively (`filters`, `required`).

## Manifest-only apps

`cmd/rastrillo`'s generate path no longer errors when `actions/` is
absent: an app may be entirely manifest-driven. Hand-action discovery
treats a missing directory as an empty set; the collision and skip
machinery is unaffected. (`rastrillo dev` already watches `manifest/`.)

## examples/tickets (the fully generated proof)

The design doc's own running example, live and unmasked:

```toml
name  = "ticket_types"
route = "/admin/ticket_types"
store = "exclusive"

[list]
columns = [{ field = "Name" }, { field = "Price", kind = "money" }, { field = "Status" }]
search  = true

[[list.filters]]
field  = "Status"
values = ["draft", "on_sale", "sold_out"]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money", required = true }, { name = "Status" }]
advanced = [{ name = "MaxPerOrder" }]
```

- No `actions/` directory, no `templates/` overrides — everything the
  app serves under /admin is generated. main.go + one layout + the
  Render wiring (same pattern as the blog's, minimal) + embedded
  static/ + go.mod with the sqlc tool directive.
- Its test suite (internal/ticketstest or equivalent) is the permanent
  no-mask regression host: full four-state round-trip, the filter
  round-trip (apply each value, compose with search, paging carries
  both, All resets), required-400s for Name and Price (blank), "0"
  accepted for Price, Money display/edit round-trip (the slice-1
  Critical's regression test at app level), `generate --check` green,
  regen byte-identity.
- README: what it is (the no-mask proof), one paragraph; noted in the
  root README beside the blog.

## Blog adoption

`posts.toml`: `{ name = "Title", required = true }` (body stays
optional, matching its old server behavior — only title was validated).
Regen; the two retired empty-title tests return as
create/update-blank-title → 400 with the field error, against the
generated actions. The accept-empty contract tests they replaced are
themselves replaced back. No filter adoption (the blog's status filter
derives from the app-owned `published` column and stays hand-written —
that remains the ejection/coexistence proof).

## Testing

Validate table additions; emitter goldens (fixture grows a Filters
entry + required flags — pinned outputs updated deliberately); the
real-sqlc integration keeps all four shape fixtures and gains the
tickets shape (filter + required + advanced + money, fully generated
actions compiled); blogtest un-retired 400 tests; the tickets suite
above; three-module sweeps plus the new example's.

## Out of scope

Multi-filter; filter labels beyond title-casing; min/max or
cross-field validation; additive-migration emission (roadmap:
manifest-diff → ALTER); catalog-routed action error strings; JSON API
/ native rendering (unchanged from slice 1's list).
