# Manifest Slice 2: Filter Values + Required Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Declared filter values generate a real list filter dropdown, `Required` fields validate server-side, manifest-only apps are legal, and a fully generated `examples/tickets` proves the unmasked path.

**Architecture:** Additive fields on the slice-1 types (`List.Filters []Filter`, `Field.Required`), threaded through the existing emitters: store (filter-field union, no query-shape change), list action (dropdown data generalizing the blog's hand pattern), templates (list-bar's existing `Filter` seam; field partials' existing `Required` key), locales (per-value keys + `ui.all`), and the create/edit actions' existing 400-re-render error path. The tickets example is a third module with zero hand actions.

**Tech Stack:** Everything already in the tree (Go stdlib, BurntSushi/toml, sqlc via tool directive). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-04-filters-required-design.md` (approved). Where plan and spec disagree, the spec governs.

## Global Constraints

- **Additive artifact:** `filters`/`required` are NEW JSON/TOML fields; the frozen `filter []string` keeps validate-only semantics (its doc comment now says "superseded by Filters"). Never reshape existing artifact fields.
- **At most one `Filters` entry per resource** (v1); values `^[a-z0-9_-]+$`, non-empty, unique; Filters.Field must be a declared column.
- **Required-Money = non-empty input**; "0"/"0.00" valid. Text/Textarea required = `strings.TrimSpace(v) == ""` rejected. Error strings literal English containing "required", via the existing `Errors` map + 400 re-render path (see the Money parse-error path in the generated actions — `internal/generate/actions.go`).
- **No-Filters output is byte-identical to slice 1's** for actions and templates (except where Required changes form emission for resources that declare it) — the blog's regen diff in Task 7 is the proof.
- **Idempotency and DO-NOT-EDIT discipline** exactly as slice 1 (write-on-change helper, `generate --check` gates).
- Comments state constraints the code can't show, never narration; match each file's voice.
- Sweep before every commit, all clean, in every module the task touched (root / examples/blog / examples/helloworld / examples/tickets once it exists): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go build ./...`, same env `go vet ./...`, `go test ./... -count=1`, `gofmt -l .` empty. Sandbox off only for network (`go get -tool`) / nested-go / read-only-fs needs.
- Regen command (run in the app module): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .`
- Commit style: short imperative subject; body says why; trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Branch: `worktree-filters-required` (current worktree). Never switch branches.

## Reference files (read before implementing; they are the pattern)

- Types/validation: `manifest.go` (root) — slice 1's Validate structure, reserved names, canonical-ident check.
- Emitters: `internal/generate/{store,templates,actions,manifestlocales,manifestgen}.go` + their `_test.go` goldens.
- The blog's hand filter pattern to generalize: `examples/blog/internal/blog/view.go` (`BuildStatusFilter`, `NormalizeStatus`, `BuildPagination`, `NoMatchNote`) and `examples/blog/actions/admin/posts/index.GET.go`.
- The F3 seam: `ui/partials/list-bar.html` (`Filter` key → `dropdown`), `ui/partials/dropdown.html` (Label/Aria/Items{Href,Label,Current}).
- Render wiring pattern for tickets: `examples/blog/internal/blog/genrender.go` + `examples/blog/cmd/blog/main.go`.

## Fixture evolution

The slice-1 `notes` fixture (`internal/generate` tests) gains, in a NEW
sibling fixture `filteredFixtureResource()` (do not mutate the existing
fixtures — their goldens pin slice-1 behavior):

```go
func filteredFixtureResource() rastrillo.Resource {
	return rastrillo.Resource{
		Name:  "events",
		Route: "/admin/events",
		Store: rastrillo.Exclusive,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Status"}},
			Search:  true,
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"draft", "live"}}},
		},
		Form: rastrillo.Form{
			Basics: []rastrillo.Field{
				{Name: "Title", Required: true},
				{Name: "Status"},
			},
		},
	}
}
```

---

### Task 1: types + Validate additions

**Files:**
- Modify: `manifest.go` (root; `List`, `Field`, new `Filter` type, `Validate`)
- Test: `manifest_test.go`

**Interfaces:**
- Produces (exact):

```go
type Filter struct {
	Field  string   `json:"field" toml:"field"`
	Values []string `json:"values" toml:"values"`
}
// List gains:  Filters []Filter `json:"filters" toml:"filters"`
// Field gains: Required bool   `json:"required" toml:"required"`
```

- [ ] **Step 1: Write the failing tests** — extend `TestValidateRejections`'s table (keep every existing case):

```go
{"two filters", func(r *Resource) {
	r.List.Filters = []Filter{{Field: "Title", Values: []string{"a"}}, {Field: "Price", Values: []string{"b"}}}
}, "one filter"},
{"filter field not a column", func(r *Resource) {
	r.List.Filters = []Filter{{Field: "Nope", Values: []string{"a"}}}
}, "column"},
{"filter no values", func(r *Resource) {
	r.List.Filters = []Filter{{Field: "Title", Values: nil}}
}, "value"},
{"filter bad value", func(r *Resource) {
	r.List.Filters = []Filter{{Field: "Title", Values: []string{"On Sale"}}}
}, "value"},
{"filter duplicate value", func(r *Resource) {
	r.List.Filters = []Filter{{Field: "Title", Values: []string{"a", "a"}}}
}, "value"},
```

Plus an accept case: `validResource()` extended in a NEW test (not mutating the shared helper) with one well-formed Filters entry + `Required: true` on a field → `Validate` passes; and assert bare `Filter` + `Filters` naming the same field passes (union semantics).

- [ ] **Step 2: Run to verify failure** — `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ -run TestValidate -v` → compile error (no Filter type).
- [ ] **Step 3: Implement.** Types as pinned (Filter's doc comment as in the spec). Validate: after the existing filter-field check, add — `len(Filters) > 1` → error containing "one filter"; each entry's Field must be a declared column ("column"); Values non-empty/unique/matching `^[a-z0-9_-]+$` (one more package-level regexp var; errors contain "value"). Update bare `Filter`'s field doc comment: "superseded by Filters; still validated, generates the WHERE clause but no control." `Required` needs no rule.
- [ ] **Step 4: Run to verify pass.** Also `go doc rastrillo.Filter` shows the contract.
- [ ] **Step 5: Sweep (root) + commit** — `manifest: Filters values and Required flag (additive)`.

---

### Task 2: store emitter — filter-field union

**Files:**
- Modify: `internal/generate/store.go` (wherever the filter-field list derives from `r.List.Filter`)
- Test: `internal/generate/store_test.go`

**Interfaces:**
- Produces: an unexported `filterFields(r) []string` — ordered union of bare `Filter` and `Filters[].Field` (first-mention order, no duplicates) — used by queries emission AND (Task 3) the action emitter. Query/golden shapes unchanged for existing fixtures.

- [ ] **Step 1: Write the failing test** — `filteredFixtureResource()` (add the fixture) through `EmitStore`: its `queries.sql` contains `filter_status` args in List/Count exactly as a bare-filter field would (assert the two WHERE lines byte-exactly, mirroring the existing notes golden's filter lines with `status`); a resource declaring the SAME field in both `Filter` and `Filters` emits the arg ONCE.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** — extract `filterFields`, use it where `r.List.Filter` was read; existing goldens must not change (the union of old fixtures is identical to before).
- [ ] **Step 4: Run to verify pass** (all existing store goldens too).
- [ ] **Step 5: Sweep (root) + commit** — `generate: filter-field union feeds the store queries`.

---

### Task 3: list action + template — the generated dropdown

**Files:**
- Modify: `internal/generate/actions.go` (`actionIndexGET` + list view types), `internal/generate/templates.go` (list.html emission: `"Filter" .Filter` on the list-bar call ONLY when Filters declared)
- Test: `internal/generate/actions_test.go`, `templates_test.go`

**Interfaces:**
- Produces, in the generated list action (per the blog's hand pattern, generalized):
  - filter param name = `sqlName(field)` (e.g. `status`);
  - `normalize<Field>(raw)`: declared value → itself, else `""`;
  - `filterView` data matching the dropdown partial: `Label` (current value's label or all-label), `Aria` ("Filter by <field label>: <current label>"), `Items` (All first, then each declared value; hrefs carry `q` when Search is on and the filter param when non-empty, never `page`; `Current` marks the applied one);
  - `Carry` gains `[2]string{param, value}` when a filter is applied; pagination href builder gains the filter pair (order: q, filter, page — mirroring `examples/blog/internal/blog/view.go`'s BuildPagination);
  - labels resolve at RENDER time: the action passes T KEYS (never resolved strings) in the filterView, and the template calls `(T .LabelKey)` per item. **Binding decision:** the generated list.html emits the dropdown MARKUP inline — a `<details class="rst-dropdown">` block ranging `.Filter.Items` with `{{icon "chevron-down"}}`/`{{icon "check"}}` and `(T .LabelKey)` inline — rather than dispatching the `dropdown` partial, because the partial's one-dict contract cannot express per-item T resolution inside a range (Go templates can't build dicts in a loop). The inline markup MUST structurally match what the partial renders — same classes and attributes (`rst-dropdown`, `rst-btn rst-dropdown__summary`, `aria-label` on the summary, `rst-dropdown__menu`, `aria-current="true"` + check icon on the applied item) — so the existing CSS and a11y contract hold unchanged. A test asserts this: render the real dropdown partial with the fixture's resolved labels and compare the structural markup against the emitted block's rendered output. The emitted template's comment states why the partial isn't dispatched.
  - `filterView` Go shape (action-side, exact):

```go
type filterItem struct {
	Href     string
	LabelKey string
	Current  bool
}
type filterView struct {
	SummaryKey string // T key of the applied value's label (or ui.all)
	AriaField  string // resolved field-label T key: resource.<name>.field.<sqlCol>
	Items      []filterItem
}
```

- [ ] **Step 1: Write the failing goldens** — `filteredFixtureResource()` through `EmitActions`/`EmitTemplates`: pin the new `index.GET.go` golden (param parse + normalize + filterView build + Carry + pagination hrefs carrying `status`) and the new list.html golden (search form + inline dropdown block). Plus: a no-Filters resource's outputs BYTE-IDENTICAL to the slice-1 goldens (assert against the existing pinned constants — regression gate). Plus the markup-equivalence test vs the real dropdown partial.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run to verify pass** (every slice-1 golden untouched and green).
- [ ] **Step 5: Sweep (root) + commit** — `generate: declared filter values render the list dropdown (F3 seam, generated consumer)`.

---

### Task 4: Required — server-side validation + template pass-through

**Files:**
- Modify: `internal/generate/actions.go` (create + edit-basics + edit-advanced emission), `internal/generate/templates.go` (form field dicts gain `"Required" true` when set)
- Test: `internal/generate/actions_test.go`, `templates_test.go`

**Interfaces:**
- In generated actions, after parsing and BEFORE store writes: for each required field in the action's field group — Text/Textarea: `strings.TrimSpace(v) == ""` → `errs["<Field>"] = "<label-ish> is required"`; Money: raw input `""` → same (parse errors keep their existing message; a present-but-invalid Money value gets the parse error, not the required error). Any errs → the existing 400 re-render.

- [ ] **Step 1: Write the failing goldens/tests** — `filteredFixtureResource()` (Title required) pins the new create/edit-basics goldens' required blocks; the `TestMoneyHelpersRoundTrip`-style real-execution test (see `internal/generate/actions_test.go`'s pattern of running generated code) gains required cases: blank Title → 400 + error containing "required"; blank required Money → 400; `"0"` for required Money → accepted. A resource with NO required fields emits byte-identical actions to slice 1 (regression assertion against existing goldens).
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** (templates: the field dict gains `"Required" true` — partials handle the rest; actions: the check block).
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — `generate: Required fields validate server-side on the 400 path`.

---

### Task 5: manifest-only apps + locales keys

**Files:**
- Modify: `cmd/rastrillo/generate.go` (missing `actions/` = empty hand-action set, not an error), `internal/generate/manifestlocales.go` (+ per-value keys + `ui.all`)
- Test: `cmd/rastrillo` tests (or internal/generate where the check lives — find the "actions directory" error first: `grep -rn "actions" cmd/rastrillo/generate.go internal/generate/generate.go | grep -i "requir\|no such\|missing"`), `internal/generate/manifestlocales` tests

**Interfaces:**
- Locale keys added: `resource.<name>.filter.<sqlName(field)>.<value>` per declared value (title-cased fallback: `on_sale` → "On sale") and shared `ui.all` = "All". The key-set-matches-templates test (`TestEmitLocalesKeySetMatchesTemplates`) must keep passing — it derives from the templates' T calls, which now include the filter LabelKeys the ACTION passes; extend the test's derivation to include action-passed keys for a Filters resource (the action's SummaryKey/LabelKey strings are `resource...filter...` and `ui.all` — assert the emitted catalog covers every key the generated action can produce for the fixture).

- [ ] **Step 1: Failing tests** — (a) a temp module with `manifest/` but NO `actions/` directory: `GenerateManifests`/the CLI path succeeds (reuse the existing temp-module fixture helpers in `internal/generate/sqlcrun_test.go`); (b) locales: `filteredFixtureResource()` emits the status value keys + `ui.all` with the pinned fallbacks; key-set coverage test extended.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run to verify pass.**
- [ ] **Step 5: Sweep (root) + commit** — `generate: manifest-only apps; filter value locale keys`.

---

### Task 6: examples/tickets — the fully generated proof

**Files:**
- Create: `examples/tickets/` — `go.mod` (module tickets; require+replace rastrillo; sqlc tool directive via `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc`, network), `manifest/ticket_types.toml` (EXACTLY the spec's), `cmd/tickets/main.go`, `internal/tickets/` render wiring (the blog's `genrender.go` pattern, minimal: parse ui partials + gen/templates, one layout, Render resolving `ticket_types/list|show|form`), `static/tokens.css` (from `ui.TokensCSS()` — copy the blog's vendored file), an `assets.go` embed (the F8 shape), NO `actions/`, NO `templates/`
- Create: `examples/tickets/internal/ticketstest/` — the no-mask regression suite
- Generated: `examples/tickets/gen/` (committed, like the blog's)

**The suite (each a real httptest round-trip through the generated mux — copy the blogtest harness pattern):**
- Four states: create (all fields) → 303 → show renders values (Price "$12.00" style) → edit-basics save unchanged → 303 (the slice-1 Money Critical's app-level regression) → list shows the row.
- Filter: `?status=draft` shows only drafts; compose with `q=`; pagination hrefs carry both; the All item's href clears; `aria-current` on the applied item; unknown `?status=bogus` = all.
- Required: blank Name → 400 + "required"; blank Price → 400; `"0"` Price → accepted; body of the 400 page carries the re-rendered form with the error visible.
- `generate --check` green; double-regen byte-identical (`git diff --exit-code gen/` style assertion or a test).

- [ ] Steps: scaffold module → `go get -tool` (sandbox off) → write manifest → regen → wire main/render → write the suite FIRST where feasible (it fails until wiring lands) → iterate to green → full sweep in examples/tickets AND root → commit — `tickets: fully generated example — the no-mask proof (filters, Required, Money)`.

---

### Task 7: blog — Required title, un-retired tests

**Files:**
- Modify: `examples/blog/manifest/posts.toml` (`{ name = "Title", required = true }`), regen `examples/blog/gen/`
- Test: `examples/blog/internal/blogtest/admin_form_test.go` (or wherever the accept-empty contract tests live — grep `SucceedsNoServerSideValidation`)

**Contract:** the two accept-empty contract tests flip back to 400-assertions (blank title on create and on edit-basics → 400, field error containing "required", nothing written — the slice-1 retirement reversed by the machinery that now exists; keep the tests' names honest, e.g. `TestCreateWithAnEmptyTitleIs400AgainstGeneratedValidation`). Body stays optional (no new assertion). Regen diff should show ONLY the posts create/edit actions + form template gaining the required block/attr — verify and state in the report.

- [ ] Steps: flip tests → verify fail → edit manifest → regen → verify pass → full blog+root sweeps → commit — `blog: Title is required again — generated validation restores the 400s`.

---

### Task 8: docs

**Files:**
- Modify: `README.md` (Manifests section: `filters` + `required` documented with the tickets TOML as the example; the superseded bare `filter` note; manifest-only apps sentence; the migration caveat sentence — manifest edits regenerate code but existing databases need app-owned additive migrations, per the roadmap), `examples/tickets/README.md` (what it is: the no-mask proof, one short page), `examples/blog/README.md` (Required note in the adoption section)

- [ ] Steps: write (verify every claim against the shipped code), sweep all modules for the record, commit — `docs: filters, Required, manifest-only apps, the tickets example`.

---

## Verification (whole slice)

1. Sweeps green: root, examples/blog, examples/helloworld, examples/tickets.
2. `generate --check` green in blog AND tickets; double-regen byte-identity in both.
3. No-Filters/no-Required resources regenerate byte-identically vs slice 1 (blog regen diff scoped to posts' required additions only).
4. Manual smoke (optional): `rastrillo dev` in examples/tickets — filter each status, search within a filter, save an unchanged Money edit, submit a blank Name.
5. Push branch, open draft PR titled "manifest slice 2: declared filter values + Required (fully generated tickets example)".

## Self-review notes

- **Binding decision recorded in Task 3:** the generated list emits the dropdown MARKUP inline (structural byte-match with the partial asserted by test) because the partial's dict contract can't express per-item T resolution inside a range; documented in the emitted template comment. If the reviewer finds a cleaner shape that keeps the partial dispatch, that's a welcome fix — the a11y/CSS contract is what's binding, not the inline emission.
- Type consistency: `Filter{Field, Values}`/`Filters`/`Required` (Task 1) consumed by Tasks 2-5; `filterFields` (Task 2) used in Task 3's action emission; `filterView`/`filterItem` shapes fixed in Task 3 and rendered by its template; locale keys `resource.<name>.filter.<sqlName>.<value>` + `ui.all` shared by Tasks 3/5.
- Spec coverage: types (T1), store union (T2), dropdown (T3), Required (T4), manifest-only + locales (T5), tickets (T6), blog (T7), docs incl. migration caveat (T8). Out-of-scope list untouched.
- Delegated with guardrails: the actions/-optional error site located by grep (T5); the exact goldens derived under byte-exact test discipline with the no-change regression assertions as the safety net (T3/T4); tickets Render wiring copies a referenced real file (T6).
