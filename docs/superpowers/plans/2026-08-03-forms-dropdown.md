# F2 Form Partials + F3 Dropdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the `field-text`, `field-textarea`, `form-foot` and `dropdown` partials in `rastrillo/ui`, fix the focus-ring scope (F2), and prove all of it by adopting it in `examples/blog` — forms on the partials, a status filter on the admin list (F3).

**Architecture:** Four new single-`{{define}}` files in `ui/partials/`, styles and one new `--rst-shadow` token in `ui/tokens.css`, an optional `Filter` key on `list-bar`. The blog gains a `status` parameter through store → handler → view, renders the filter through `list-bar`, and rewrites its two admin forms onto the partials, deleting the form half of `blog.css`.

**Tech Stack:** Go stdlib `html/template`, modernc.org/sqlite (blog, already a dependency). No new dependencies, no JavaScript.

**Spec:** `docs/superpowers/specs/2026-08-03-forms-dropdown-design.md` (approved).

## Global Constraints

- **Zero JS.** No `<script>` anywhere. The dropdown is a native `<details>`; every filter application is a plain GET link.
- **As-built ui conventions:** one `{{define}}` per file in `ui/partials/<name>.html`; PascalCase dict keys; data contract in a `{{/* */}}` comment above the define; every optional key guarded (an absent key renders nothing — no empty attributes); `rst-` class prefix; no catalog strings — callers pass all visible text.
- **tokens.css conventions:** every value comes from a token; themed tokens are declared in ALL theme blocks (`:root`, `prefers-color-scheme: dark`, `:root[data-theme="dark"]`, `:root[data-theme="light"]`) — `TestBothThemesDeclareEveryColourToken` enforces the list it is given; section banners like `/* ── dropdown ───… */`; properties alphabetical within a rule (match neighboring rules).
- **blog.css's two tested rules keep holding:** tokens only (no literal colours/px), and it never styles an `.rst-` class.
- **Additive API.** Existing partials render byte-identically when the new keys are absent. Blog store signatures may change (it is an example app, same repo, same PR).
- **Formatting in Go, not templates** (blog convention): computed strings live in `view.go` where a test reaches them.
- Comments state constraints the code can't show, never narration. Match each file's existing comment style.
- Sweep before every commit, all clean, in **both** modules touched by the task (root and/or `examples/blog`):
  `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go build ./...`, same env `go vet ./...`, same env `go test ./... -count=1`, and `gofmt -l .` (empty). On "read-only file system" errors, rerun with the sandbox disabled.
- Commit messages: short imperative subject; body says why; trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Work happens on the current worktree branch `worktree-f2-f3-forms-dropdown`.

---

### Task 1: `dropdown` partial + `--rst-shadow` token + menu styles

**Files:**
- Create: `ui/partials/dropdown.html`
- Modify: `ui/tokens.css` (token blocks near the top; new section banner after the `.rst-btn` section ~line 315)
- Test: `ui/ui_test.go`

**Interfaces:**
- Produces: `{{define "dropdown"}}` taking keys `Label` (string, required), `Items` (slice of values with `.Href`, `.Label`, `.Current` — dicts or structs), `Aria` (string, optional). Tasks 5 and 7 call it by this contract. Also the `--rst-shadow` token.
- Consumes: `{{icon "chevron-down"}}` and `{{icon "check"}}` (already vendored in icons.go), `.rst-btn` (existing class, reused on the summary).

- [ ] **Step 1: Write the failing tests** — append to `ui/ui_test.go`:

```go
func TestDropdownRendersADetailsMenuOfLinks(t *testing.T) {
	got := render(t, "dropdown", map[string]any{
		"Label": "All",
		"Aria":  "Filter by status: All",
		"Items": []any{
			map[string]any{"Href": "/admin/posts", "Label": "All", "Current": true},
			map[string]any{"Href": "/admin/posts?status=draft", "Label": "Drafts"},
		},
	})
	for _, want := range []string{
		`<details class="rst-dropdown">`,
		`<summary class="rst-btn rst-dropdown__summary" aria-label="Filter by status: All">All`,
		`<a href="/admin/posts" aria-current="true">All`,
		`<a href="/admin/posts?status=draft">Drafts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// The current item is marked twice — attribute and check icon; the
	// non-current one carries neither.
	if !strings.Contains(got, "icon-check") {
		t.Errorf("current item lost its check icon: %s", got)
	}
	if strings.Count(got, "aria-current") != 1 {
		t.Errorf("aria-current should mark exactly the current item: %s", got)
	}
}

func TestDropdownMinimalFixture(t *testing.T) {
	got := render(t, "dropdown", map[string]any{
		"Label": "Sort",
		"Items": []any{map[string]any{"Href": "/x", "Label": "Newest"}},
	})
	if strings.Contains(got, "aria-label") {
		t.Errorf("Aria was absent but an aria-label rendered: %s", got)
	}
	if !strings.Contains(got, "icon-chevron-down") {
		t.Errorf("summary lost its disclosure chevron: %s", got)
	}
}
```

Before writing, check how vendored icons mark themselves: `render(t, ...)` output of an existing partial, or `grep -n "icon-" icons.go`. If the svg class is not `icon-check`/`icon-chevron-down`, use the actual marker (`{{icon "search"}}` appears in list-bar-search and `.rst-search .icon` is styled in tokens.css, so at minimum `class="icon…"` exists). Adjust the two icon assertions to the real substring before running.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -run TestDropdown -v`
Expected: FAIL — `ExecuteTemplate("dropdown")`: no such template.

- [ ] **Step 3: Write `ui/partials/dropdown.html`**

```html
{{/* dropdown — a zero-JS disclosure: a summary button that opens a
     panel of plain links. Native <details> is the only disclosure HTML
     has without JavaScript; the accepted cost is that the panel closes
     on a second click of the summary (or Esc in some browsers), not on
     an outside click.

     Every item is an ordinary link, so applying a filter is one click
     and a GET — no submit button, nothing to script. The current item
     carries aria-current="true" and a check icon; state is never
     colour alone.

     Keys:
       Label  string, required — the summary's visible text, e.g. the
              currently applied value ("All", "Newest").
       Aria   string, optional — an aria-label for the summary when its
              visible text is not a full accessible name. "All" alone
              does not say all of *what*; "Filter by status: All" does.
       Items  list, required — one dict per choice:
                Href     string, required — the link target
                Label    string, required — the visible text
                Current  bool, optional — marks the applied choice */}}
{{define "dropdown"}}<details class="rst-dropdown">
  <summary class="rst-btn rst-dropdown__summary"{{if .Aria}} aria-label="{{.Aria}}"{{end}}>{{.Label}} {{icon "chevron-down"}}</summary>
  <div class="rst-dropdown__menu">
    {{- range .Items}}
    <a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}{{if .Current}} {{icon "check"}}{{end}}</a>
    {{- end}}
  </div>
</details>{{end}}
```

- [ ] **Step 4: Add the `--rst-shadow` token and dropdown styles to `ui/tokens.css`**

Token: in **every** block that declares `--rst-line` (the light `:root` block ~line 134, the `prefers-color-scheme: dark` block, the `:root[data-theme="dark"]` block, and the `:root[data-theme="light"]` block if it re-declares colours — mirror exactly what `--rst-line` does), add alongside the other themed tokens:

```css
  /* light blocks */
  --rst-shadow: 0 8px 24px rgb(27 26 35 / 0.12);
  /* dark blocks */
  --rst-shadow: 0 8px 24px rgb(0 0 0 / 0.5);
```

Add `"--rst-shadow"` to the `themed` list in `TestBothThemesDeclareEveryColourToken` (ui/ui_test.go ~line 72) so the theme-completeness gate covers it.

Styles, new section after the Buttons section (~line 315), matching banner style:

```css
/* ── dropdown ─────────────────────────────────────────────────────── */
.rst-dropdown {
  position: relative;
}
/* The summary reuses .rst-btn; only the default disclosure marker has
   to go — the chevron icon is the affordance. */
.rst-dropdown__summary {
  list-style: none;
}
.rst-dropdown__summary::-webkit-details-marker {
  display: none;
}
.rst-dropdown__menu {
  background: var(--rst-surface);
  border: 1px solid var(--rst-line);
  border-radius: var(--rst-radius-sm);
  box-shadow: var(--rst-shadow);
  display: flex;
  flex-direction: column;
  inset-inline-start: 0;
  margin-block-start: var(--rst-sp-1);
  min-inline-size: 11rem;
  padding: var(--rst-sp-1);
  position: absolute;
  z-index: 1;
}
.rst-dropdown__menu a {
  align-items: center;
  border-radius: var(--rst-radius-sm);
  color: var(--rst-text);
  display: flex;
  font-size: var(--rst-fs-sm);
  gap: var(--rst-sp-2);
  justify-content: space-between;
  padding: var(--rst-sp-2) var(--rst-sp-3);
  text-decoration: none;
}
.rst-dropdown__menu a:hover {
  background: var(--rst-surface-2);
  color: var(--rst-accent);
}
.rst-dropdown__menu a[aria-current="true"] {
  color: var(--rst-accent);
  font-weight: 600;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -count=1`
Expected: PASS (including the extended theme-completeness test).

- [ ] **Step 6: Sweep + commit**

Root-module sweep (Global Constraints), then:

```bash
git add ui/partials/dropdown.html ui/tokens.css ui/ui_test.go
git commit -m "ui: dropdown — zero-JS details menu of links (F3)"
```

---

### Task 2: `field-text` partial + field-family styles

**Files:**
- Create: `ui/partials/field-text.html`
- Modify: `ui/tokens.css` (new section after the dropdown section from Task 1)
- Test: `ui/ui_test.go`

**Interfaces:**
- Produces: `{{define "field-text"}}` with keys `Name` (required), `Label` (required), `Value`, `Type` (default `text`), `Required` (bool), `Hint`, `Error`, `Autocomplete`; classes `.rst-field`, `.rst-field__label`, `.rst-field__required`, `.rst-input`, `.rst-field__hint`, `.rst-field__error`. Task 3 duplicates this wrapper; Task 8 calls the partial.

- [ ] **Step 1: Write the failing tests** — append to `ui/ui_test.go`:

```go
func TestFieldTextMaximalFixture(t *testing.T) {
	got := render(t, "field-text", map[string]any{
		"Name": "title", "Label": "Title", "Value": "Hello", "Type": "text",
		"Required": true, "Hint": "Shown in the list.", "Error": "Title is required.",
		"Autocomplete": "off",
	})
	for _, want := range []string{
		`<div class="rst-field">`,
		`<label class="rst-field__label" for="title">Title`,
		`<span class="rst-field__required" aria-hidden="true">*</span>`,
		`<input class="rst-input" id="title" name="title" type="text" value="Hello" autocomplete="off" required aria-invalid="true" aria-describedby="title-hint title-error">`,
		`<small class="rst-field__hint" id="title-hint">Shown in the list.</small>`,
		`<small class="rst-field__error" id="title-error">Title is required.</small>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFieldTextMinimalFixture(t *testing.T) {
	got := render(t, "field-text", map[string]any{"Name": "q", "Label": "Query"})
	if !strings.Contains(got, `<input class="rst-input" id="q" name="q" type="text">`) {
		t.Errorf("minimal input wrong: %s", got)
	}
	for _, absent := range []string{"aria-describedby", "aria-invalid", "required", "value=", "rst-field__hint", "rst-field__error"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q rendered without its key: %s", absent, got)
		}
	}
}

// aria-describedby lists only ids that exist: hint alone, error alone.
func TestFieldTextDescribedByMatchesRenderedIds(t *testing.T) {
	hintOnly := render(t, "field-text", map[string]any{"Name": "a", "Label": "A", "Hint": "h"})
	if !strings.Contains(hintOnly, `aria-describedby="a-hint"`) {
		t.Errorf("hint-only describedby wrong: %s", hintOnly)
	}
	errOnly := render(t, "field-text", map[string]any{"Name": "a", "Label": "A", "Error": "e"})
	if !strings.Contains(errOnly, `aria-describedby="a-error"`) {
		t.Errorf("error-only describedby wrong: %s", errOnly)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -run TestFieldText -v`
Expected: FAIL — no such template.

- [ ] **Step 3: Write `ui/partials/field-text.html`**

```html
{{/* field-text — one labelled text input with optional hint and error
     lines, wired together for assistive tech: the hint and error get
     ids derived from Name, aria-describedby lists exactly the ids that
     render, and an Error also sets aria-invalid.

     The required marker's * is aria-hidden: the input's own required
     attribute is the programmatic signal, so the star is presentation
     on top of it, not a substitute.

     field-textarea repeats this wrapper rather than composing it: Go
     templates cannot capture a rendered fragment into a variable, so
     composition would push control rendering back into every caller's
     Go code. A few duplicated lines beat that.

     Keys:
       Name          string, required — the input's name and id, and
                     the prefix for the hint/error ids
       Label         string, required — the visible label
       Value         string, optional — the current value
       Type          string, optional — the input type, default "text"
       Required      bool, optional — required attribute + visible *
       Hint          string, optional — muted guidance under the input
       Error         string, optional — validation message; also sets
                     aria-invalid
       Autocomplete  string, optional — the autocomplete attribute */}}
{{define "field-text"}}<div class="rst-field">
  <label class="rst-field__label" for="{{.Name}}">{{.Label}}{{if .Required}} <span class="rst-field__required" aria-hidden="true">*</span>{{end}}</label>
  <input class="rst-input" id="{{.Name}}" name="{{.Name}}" type="{{if .Type}}{{.Type}}{{else}}text{{end}}"{{if .Value}} value="{{.Value}}"{{end}}{{if .Autocomplete}} autocomplete="{{.Autocomplete}}"{{end}}{{if .Required}} required{{end}}{{if .Error}} aria-invalid="true"{{end}}{{if or .Hint .Error}} aria-describedby="{{if .Hint}}{{.Name}}-hint{{end}}{{if and .Hint .Error}} {{end}}{{if .Error}}{{.Name}}-error{{end}}"{{end}}>
  {{- if .Hint}}
  <small class="rst-field__hint" id="{{.Name}}-hint">{{.Hint}}</small>
  {{- end}}
  {{- if .Error}}
  <small class="rst-field__error" id="{{.Name}}-error">{{.Error}}</small>
  {{- end}}
</div>{{end}}
```

- [ ] **Step 4: Add field-family styles to `ui/tokens.css`** — new section after the dropdown section; `.rst-textarea` is declared here alongside `.rst-input` so Task 3 adds no CSS:

```css
/* ── field family ─────────────────────────────────────────────────── */
.rst-field {
  display: flex;
  flex-direction: column;
  gap: var(--rst-sp-1);
}
.rst-field__label {
  color: var(--rst-text-muted);
  font-size: var(--rst-fs-sm);
  font-weight: 600;
}
.rst-field__required {
  color: var(--rst-tone-negative-fg);
}
.rst-input,
.rst-textarea {
  background: var(--rst-surface);
  border: 1px solid var(--rst-line-strong);
  border-radius: var(--rst-radius-sm);
  color: var(--rst-text);
  font: inherit;
  padding: var(--rst-sp-2) var(--rst-sp-3);
}
.rst-textarea {
  line-height: 1.6;
  resize: vertical;
}
.rst-field__hint {
  color: var(--rst-text-muted);
  font-size: var(--rst-fs-sm);
}
.rst-field__error {
  color: var(--rst-tone-negative-fg);
  font-size: var(--rst-fs-sm);
  font-weight: 600;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -count=1`
Expected: PASS.

- [ ] **Step 6: Sweep + commit**

```bash
git add ui/partials/field-text.html ui/tokens.css ui/ui_test.go
git commit -m "ui: field-text — labelled input with wired hint/error (F2)"
```

---

### Task 3: `field-textarea` partial

**Files:**
- Create: `ui/partials/field-textarea.html`
- Test: `ui/ui_test.go`

**Interfaces:**
- Consumes: the `.rst-field` family classes and `.rst-textarea` (all styled in Task 2).
- Produces: `{{define "field-textarea"}}` with keys `Name` (required), `Label` (required), `Value`, `Rows` (int, attribute omitted when unset), `Required`, `Hint`, `Error`. Task 8 calls it.

- [ ] **Step 1: Write the failing tests** — append to `ui/ui_test.go`:

```go
func TestFieldTextareaMaximalFixture(t *testing.T) {
	got := render(t, "field-textarea", map[string]any{
		"Name": "body", "Label": "Body", "Value": "Hello\n\nWorld",
		"Rows": 18, "Required": true, "Hint": "Plain text.", "Error": "Too long.",
	})
	for _, want := range []string{
		`<label class="rst-field__label" for="body">Body`,
		`<textarea class="rst-textarea" id="body" name="body" rows="18" required aria-invalid="true" aria-describedby="body-hint body-error">Hello`,
		`<small class="rst-field__hint" id="body-hint">Plain text.</small>`,
		`<small class="rst-field__error" id="body-error">Too long.</small>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFieldTextareaMinimalFixture(t *testing.T) {
	got := render(t, "field-textarea", map[string]any{"Name": "notes", "Label": "Notes"})
	if !strings.Contains(got, `<textarea class="rst-textarea" id="notes" name="notes"></textarea>`) {
		t.Errorf("minimal textarea wrong: %s", got)
	}
	if strings.Contains(got, "rows=") {
		t.Errorf("rows rendered without the key: %s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -run TestFieldTextarea -v`
Expected: FAIL — no such template.

- [ ] **Step 3: Write `ui/partials/field-textarea.html`**

```html
{{/* field-textarea — field-text's wrapper around a <textarea>. The
     wrapper is repeated, not composed — see field-text's comment.

     Keys:
       Name      string, required — name, id, and hint/error id prefix
       Label     string, required — the visible label
       Value     string, optional — the current content
       Rows      int, optional — the rows attribute; omitted when unset
                 so the browser default applies
       Required  bool, optional — required attribute + visible *
       Hint      string, optional — muted guidance under the control
       Error     string, optional — validation message; also sets
                 aria-invalid */}}
{{define "field-textarea"}}<div class="rst-field">
  <label class="rst-field__label" for="{{.Name}}">{{.Label}}{{if .Required}} <span class="rst-field__required" aria-hidden="true">*</span>{{end}}</label>
  <textarea class="rst-textarea" id="{{.Name}}" name="{{.Name}}"{{if .Rows}} rows="{{.Rows}}"{{end}}{{if .Required}} required{{end}}{{if .Error}} aria-invalid="true"{{end}}{{if or .Hint .Error}} aria-describedby="{{if .Hint}}{{.Name}}-hint{{end}}{{if and .Hint .Error}} {{end}}{{if .Error}}{{.Name}}-error{{end}}"{{end}}>{{if .Value}}{{.Value}}{{end}}</textarea>
  {{- if .Hint}}
  <small class="rst-field__hint" id="{{.Name}}-hint">{{.Hint}}</small>
  {{- end}}
  {{- if .Error}}
  <small class="rst-field__error" id="{{.Name}}-error">{{.Error}}</small>
  {{- end}}
</div>{{end}}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -count=1`
Expected: PASS.

- [ ] **Step 5: Sweep + commit**

```bash
git add ui/partials/field-textarea.html ui/ui_test.go
git commit -m "ui: field-textarea — the field wrapper around a textarea (F2)"
```

---

### Task 4: `form-foot` partial + `.rst-form` container + focus-ring scope fix

**Files:**
- Create: `ui/partials/form-foot.html`
- Modify: `ui/tokens.css` (field-family section from Task 2; the `:focus-visible` rule ~line 252)
- Test: `ui/ui_test.go`

**Interfaces:**
- Produces: `{{define "form-foot"}}` with keys `Submit` (required), `CancelHref`/`CancelLabel` (optional pair); classes `.rst-form`, `.rst-form__foot`. The app owns the `<form class="rst-form">` element (like `.rst-page`/`.rst-list`). Task 8 uses both.
- The F2 focus fix: `.rst-page` joins the `:focus-visible` `:where()` scope list.

- [ ] **Step 1: Write the failing tests** — append to `ui/ui_test.go`:

```go
func TestFormFootRendersSubmitAndCancel(t *testing.T) {
	got := render(t, "form-foot", map[string]any{
		"Submit": "Save", "CancelHref": "/admin/posts", "CancelLabel": "Back to posts",
	})
	for _, want := range []string{
		`<div class="rst-form__foot">`,
		`<button class="rst-btn rst-btn--primary" type="submit">Save</button>`,
		`<a class="rst-btn" href="/admin/posts">Back to posts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFormFootMinimalFixture(t *testing.T) {
	got := render(t, "form-foot", map[string]any{"Submit": "Create"})
	if strings.Contains(got, "<a ") {
		t.Errorf("cancel link rendered without CancelHref: %s", got)
	}
}

// F2's second half: the focus ring covers the whole app column, so a
// hand-rolled control inside .rst-page no longer restates the outline.
func TestFocusRingScopeIncludesThePageColumn(t *testing.T) {
	css := string(TokensCSS())
	if !strings.Contains(css, ":where(.rst-page,") {
		t.Error("tokens.css :focus-visible scope does not start with .rst-page")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -run 'TestFormFoot|TestFocusRing' -v`
Expected: FAIL — no such template; scope assertion fails.

- [ ] **Step 3: Write `ui/partials/form-foot.html`**

```html
{{/* form-foot — a form's closing action row: one primary submit and an
     optional cancel link. The cancel is a plain <a> styled as a
     button: leaving a form is navigation, not an action, so it must
     work as a link (open in new tab, middle-click, no JS).

     Destructive actions do not belong here — they get their own
     confirm-page route, per the zero-JS rule.

     Wrap the whole form in <form class="rst-form"> — the app owns that
     element, as it owns .rst-page and .rst-list.

     Keys:
       Submit       string, required — the submit button's text
       CancelHref   string, optional — the cancel link target, with
       CancelLabel  string           — its visible text */}}
{{define "form-foot"}}<div class="rst-form__foot">
  <button class="rst-btn rst-btn--primary" type="submit">{{.Submit}}</button>
  {{- if .CancelHref}}
  <a class="rst-btn" href="{{.CancelHref}}">{{.CancelLabel}}</a>
  {{- end}}
</div>{{end}}
```

- [ ] **Step 4: tokens.css — `.rst-form` container and the focus scope**

In the field-family section (Task 2), add at its top:

```css
.rst-form {
  display: flex;
  flex-direction: column;
  gap: var(--rst-sp-2);
  max-inline-size: 44rem;
}
.rst-form__foot {
  display: flex;
  gap: var(--rst-sp-3);
  margin-block-start: var(--rst-sp-3);
}
```

Change the `:focus-visible` rule (~line 252) — `.rst-page` first, existing entries kept (a partial rendered outside a `.rst-page` column keeps its ring), and update the comment above it:

```css
/* Focus is visible everywhere inside the app column (.rst-page) and
   inside any component rendered outside one. :where() contributes no
   specificity, so any rule below can override it. */
:where(.rst-page, .rst-page-header, .rst-list, .rst-lbar, .rst-search, .rst-empty, .rst-pagination) :focus-visible {
  outline: 2px solid var(--rst-accent);
  outline-offset: 2px;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -count=1`
Expected: PASS.

- [ ] **Step 6: Sweep + commit**

```bash
git add ui/partials/form-foot.html ui/tokens.css ui/ui_test.go
git commit -m "ui: form-foot + .rst-form; focus ring covers the page column (F2)"
```

---

### Task 5: `list-bar` gains the optional `Filter` key

**Files:**
- Modify: `ui/partials/list-bar.html`
- Test: `ui/ui_test.go` (`TestListBarWrapsTheSearchFormInAToolbarStrip` ~line 314, and one new test)

**Interfaces:**
- Consumes: `{{define "dropdown"}}` (Task 1).
- Produces: `list-bar` key `Filter` — a dict/struct passed **whole** to `dropdown` (so `.Label`, `.Items`, `.Aria` per Task 1's contract). Task 8's template and Task 7's `blog.Filter` struct match it.

- [ ] **Step 1: Update the tests** — in `TestListBarWrapsTheSearchFormInAToolbarStrip`, replace the trailing no-dropdown assertion and its comment:

```go
	// Without a Filter, the bar renders no dropdown — the key, not the
	// slice boundary, is now what gates it.
	if strings.Contains(got, "<details") {
		t.Errorf("list-bar rendered a dropdown without a Filter: %s", got)
	}
```

Append a new test:

```go
func TestListBarRendersAFilterDropdownWhenGivenOne(t *testing.T) {
	got := render(t, "list-bar", map[string]any{
		"SearchAction": "/admin/posts",
		"Filter": map[string]any{
			"Label": "All",
			"Aria":  "Filter by status: All",
			"Items": []any{
				map[string]any{"Href": "/admin/posts", "Label": "All", "Current": true},
				map[string]any{"Href": "/admin/posts?status=draft", "Label": "Drafts"},
			},
		},
	})
	for _, want := range []string{
		`<details class="rst-dropdown">`,
		`aria-label="Filter by status: All"`,
		`<a href="/admin/posts?status=draft">Drafts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify the new one fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -run TestListBar -v`
Expected: `TestListBarRendersAFilterDropdownWhenGivenOne` FAILS (no dropdown rendered); the updated existing test passes.

- [ ] **Step 3: Update `ui/partials/list-bar.html`** — full new content:

```html
{{/* list-bar — the toolbar strip at the top of a list card: the search
     form, and an optional filter dropdown in the space to its right
     (the search field is capped at 20rem in tokens.css precisely so
     that room stays free).

     Keys, search ones passed straight through to list-bar-search:
       SearchAction  string, optional — the search form's GET target
       Query         string, optional — the current q value
       Placeholder   string, optional — the field's placeholder and
                     accessible name
       Hidden        [][2]string, optional — name/value pairs carried
                     across the search (a page size — or the current
                     filter, so searching keeps it applied)
       Filter        dict, optional — passed whole to dropdown: Label,
                     Aria, Items (Href/Label/Current). See dropdown's
                     own contract.

     To override the submit button's screen-reader text, call
     list-bar-search directly inside your own <div class="rst-lbar">. */}}
{{define "list-bar"}}<div class="rst-lbar">
  {{template "list-bar-search" dict "Action" .SearchAction "Query" .Query "Placeholder" .Placeholder "Hidden" .Hidden}}
  {{- if .Filter}}
  {{template "dropdown" .Filter}}
  {{- end}}
</div>{{end}}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./ui/ -count=1`
Expected: PASS.

- [ ] **Step 5: Sweep + commit**

```bash
git add ui/partials/list-bar.html ui/ui_test.go
git commit -m "ui: list-bar takes an optional Filter dropdown (F3)"
```

---

### Task 6: blog store — `List`/`Count` gain a status filter

**Files:**
- Modify: `examples/blog/internal/blog/store.go` (`List` ~line 101, `Count` ~line 118)
- Modify: `examples/blog/actions/admin/posts/index.GET.go:19-29` (call sites compile — full handler rewrite is Task 7)
- Test: `examples/blog/internal/blogtest/store_test.go`

**Interfaces:**
- Produces: `func List(db *sql.DB, q, status string, offset, limit int) ([]Post, error)` and `func Count(db *sql.DB, q, status string) (int, error)`. `status` is `""` (all), `"draft"`, or `"published"`; any other value means all — the handler normalizes before calling (Task 7). Task 7 relies on these signatures exactly.

- [ ] **Step 1: Write the failing tests** — append to `store_test.go`, matching its existing fixture style (open a DB the way neighboring tests do — read the file first and copy its setup helper usage):

```go
func TestListFiltersByStatus(t *testing.T) {
	_, db := newApp(t)
	draftID, err := blog.Create(db, "Draft one", "b")
	if err != nil {
		t.Fatal(err)
	}
	pubID, err := blog.Create(db, "Published one", "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := blog.SetPublished(db, pubID, true); err != nil {
		t.Fatal(err)
	}

	drafts, err := blog.List(db, "", "draft", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != draftID {
		t.Errorf("draft filter: got %v", drafts)
	}

	pubs, err := blog.List(db, "", "published", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 1 || pubs[0].ID != pubID {
		t.Errorf("published filter: got %v", pubs)
	}

	// Search and status compose with AND.
	both, err := blog.List(db, "one", "draft", 0, blog.PageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 1 || both[0].ID != draftID {
		t.Errorf("search+status: got %v", both)
	}

	n, err := blog.Count(db, "", "draft")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count draft = %d, want 1", n)
	}
}
```

(If `store_test.go`'s tests build their DB another way than `newApp`, use that way — the assertion bodies stay.)

- [ ] **Step 2: Run tests to verify they fail to compile**

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./internal/blogtest/ -run TestListFiltersByStatus -v`
Expected: compile error — `List` takes 4 args, not 5.

- [ ] **Step 3: Implement** — replace `List` and `Count` in `store.go` with a shared WHERE builder (add `"strings"` to imports if absent):

```go
// listWhere builds the WHERE clause List and Count share. A status
// outside draft/published means no status condition — the handler
// normalizes, and the store stays forgiving about raw values.
func listWhere(q, status string) (string, []any) {
	var conds []string
	var args []any
	if q != "" {
		conds = append(conds, `title LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(q))
	}
	switch status {
	case "draft":
		conds = append(conds, "published = 0")
	case "published":
		conds = append(conds, "published = 1")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// List returns posts newest first, filtered by a title search when q is
// non-empty and by status ("draft" or "published") when set.
func List(db *sql.DB, q, status string, offset, limit int) ([]Post, error) {
	where, args := listWhere(q, status)
	args = append(args, limit, offset)
	rows, err := db.Query(`SELECT `+selectColumns+` FROM posts`+where+`
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Count counts the posts List would page through.
func Count(db *sql.DB, q, status string) (int, error) {
	where, args := listWhere(q, status)
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM posts`+where, args...).Scan(&n)
	return n, err
}
```

Update the two existing call sites in `examples/blog/actions/admin/posts/index.GET.go` minimally so the module compiles (`blog.Count(ctx.DB, "", "")`, `blog.Count(ctx.DB, q, "")`, `blog.List(ctx.DB, q, "", …)`), then regenerate so `gen/` matches:

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .`

- [ ] **Step 4: Run tests to verify they pass**

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./... -count=1`
Expected: PASS — new test and the whole existing suite.

- [ ] **Step 5: Sweep + commit**

Sweep in `examples/blog` (root module untouched by this task), then:

```bash
git add examples/blog/internal/blog/store.go examples/blog/actions/admin/posts/index.GET.go examples/blog/gen examples/blog/internal/blogtest/store_test.go
git commit -m "blog: store List/Count take a status filter"
```

---

### Task 7: blog admin list — status filter end to end

**Files:**
- Modify: `examples/blog/internal/blog/view.go` (`AdminListView` ~line 163, `BuildPagination` ~line 285; new `Filter` types + builders)
- Modify: `examples/blog/actions/admin/posts/index.GET.go` (full handler)
- Modify: `examples/blog/internal/blog/templates/pages/admin_list.html`
- Test: `examples/blog/internal/blogtest/admin_list_test.go`

**Interfaces:**
- Consumes: Task 6's `List`/`Count` signatures; Task 5's `list-bar` `Filter` key; Task 1's `dropdown` item contract.
- Produces: `blog.Filter{Label, Aria string; Items []FilterItem}`, `blog.FilterItem{Href, Label string; Current bool}`, `blog.BuildStatusFilter(q, status string) Filter`, `blog.NoMatchNote(q, status string) string`, `blog.NormalizeStatus(raw string) string`; `BuildPagination(base, q, status string, page, total int)` (public index passes `""`).

- [ ] **Step 1: Write the failing tests** — append to `admin_list_test.go` (match its existing helpers: `newApp`, `get`, and whatever assertion helpers `assert_test.go` provides — read both first and use them):

```go
func TestAdminListFiltersByStatus(t *testing.T) {
	h, db := newApp(t)
	if _, err := blog.Create(db, "Draft post", "b"); err != nil {
		t.Fatal(err)
	}
	pubID, err := blog.Create(db, "Published post", "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := blog.SetPublished(db, pubID, true); err != nil {
		t.Fatal(err)
	}

	rec := get(t, h, "/admin/posts?status=draft")
	body := rec.Body.String()
	if !strings.Contains(body, "Draft post") || strings.Contains(body, "Published post") {
		t.Errorf("draft filter listed the wrong rows: %s", body)
	}
	// The applied choice is marked, and the summary names it.
	if !strings.Contains(body, `aria-current="true"`) {
		t.Errorf("current filter item not marked: %s", body)
	}
	if !strings.Contains(body, `aria-label="Filter by status: Drafts"`) {
		t.Errorf("summary does not name the applied filter: %s", body)
	}
	// Searching from a filtered list keeps the filter.
	if !strings.Contains(body, `<input type="hidden" name="status" value="draft">`) {
		t.Errorf("search form does not carry the filter: %s", body)
	}
}

func TestAdminListFilterComposesWithSearchAndPaging(t *testing.T) {
	h, db := newApp(t)
	for i := 0; i < blog.PageSize+1; i++ {
		if _, err := blog.Create(db, fmt.Sprintf("Note %02d", i), "b"); err != nil {
			t.Fatal(err)
		}
	}
	rec := get(t, h, "/admin/posts?q=Note&status=draft")
	body := rec.Body.String()
	// Pagination carries both q and status.
	if !strings.Contains(body, "q=Note") || !strings.Contains(body, "status=draft") {
		t.Errorf("pagination dropped a parameter: %s", body)
	}
}

func TestAdminListFilterWithNoMatchesSaysSo(t *testing.T) {
	h, db := newApp(t)
	if _, err := blog.Create(db, "Only draft", "b"); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/admin/posts?status=published")
	body := rec.Body.String()
	if !strings.Contains(body, "No published posts yet.") {
		t.Errorf("missing the filtered no-match note: %s", body)
	}
	if strings.Contains(body, "Every blog starts empty") {
		t.Errorf("empty-state card shown for a filter miss: %s", body)
	}
}
```

Add `"fmt"` / `"strings"` imports as needed; `blog` is already imported by the file's neighbors.

- [ ] **Step 2: Run tests to verify they fail**

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./internal/blogtest/ -run TestAdminListFilter -v`
Expected: FAIL — no dropdown in the page, no hidden status input, generic no-match note.

- [ ] **Step 3: Implement `view.go` additions**

Add types + builders (near `AdminListView`); extend the view struct; extend `BuildPagination`:

```go
// FilterItem is one dropdown choice; Filter is list-bar's Filter value.
// The field names match the dropdown partial's key contract.
type FilterItem struct {
	Href    string
	Label   string
	Current bool
}

type Filter struct {
	Label string
	Aria  string
	Items []FilterItem
}

// statusLabels resolves a normalized status to its visible label.
var statusLabels = map[string]string{"": "All", "draft": "Drafts", "published": "Published"}

// NormalizeStatus maps a raw query value onto the three states the
// screen has. Anything unrecognized is "all", not an error: a stale
// bookmark should show posts, not a 400.
func NormalizeStatus(raw string) string {
	if raw == "draft" || raw == "published" {
		return raw
	}
	return ""
}

// BuildStatusFilter builds the admin list's status dropdown. Hrefs
// carry the current search and reset paging — changing a filter starts
// at page 1 by construction.
func BuildStatusFilter(q, status string) Filter {
	href := func(s string) string {
		var params []string
		if q != "" {
			params = append(params, "q="+url.QueryEscape(q))
		}
		if s != "" {
			params = append(params, "status="+s)
		}
		if len(params) == 0 {
			return "/admin/posts"
		}
		return "/admin/posts?" + strings.Join(params, "&")
	}
	f := Filter{
		Label: statusLabels[status],
		Aria:  "Filter by status: " + statusLabels[status],
	}
	for _, s := range []string{"", "draft", "published"} {
		f.Items = append(f.Items, FilterItem{Href: href(s), Label: statusLabels[s], Current: s == status})
	}
	return f
}

// NoMatchNote words the "nothing matched" note for the applied search
// and filter. Formatting stays in Go, where a test reaches it.
func NoMatchNote(q, status string) string {
	subject := map[string]string{"": "posts", "draft": "drafts", "published": "published posts"}[status]
	if q != "" {
		return fmt.Sprintf("No %s match “%s”.", subject, q)
	}
	return fmt.Sprintf("No %s yet.", subject)
}
```

`AdminListView` gains two fields (after `Query`):

```go
	Filter      Filter
	NoMatchNote string
```

…and its `Carry` comment gains the new reason: the handler sets `Carry` to `[][2]string{{"status", status}}` when a filter is applied, so a search keeps it.

`BuildPagination` — new signature `func BuildPagination(base, q, status string, page, total int) Pagination`; the href builder becomes:

```go
	href := func(n int) string {
		var params []string
		if q != "" {
			params = append(params, "q="+url.QueryEscape(q))
		}
		if status != "" {
			params = append(params, "status="+status)
		}
		params = append(params, "page="+strconv.Itoa(n))
		return base + "?" + strings.Join(params, "&")
	}
```

(keep the hand-built-order comment; add `"strings"` to view.go's imports if absent). Update the **public** index call site — find it with `grep -rn "BuildPagination" examples/blog/actions` — to pass `""` for status, and any view tests calling it (`view_test.go`).

- [ ] **Step 4: Rewrite the handler** — `examples/blog/actions/admin/posts/index.GET.go` body:

```go
// Handle is GET /admin/posts.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := blog.NormalizeStatus(r.URL.Query().Get("status"))
	page := blog.PageParam(r)

	all, err := blog.Count(ctx.DB, "", "")
	if err != nil {
		blog.Fail(ctx, w, "counting posts", err)
		return
	}
	total, err := blog.Count(ctx.DB, q, status)
	if err != nil {
		blog.Fail(ctx, w, "counting matching posts", err)
		return
	}
	posts, err := blog.List(ctx.DB, q, status, blog.Offset(page), blog.PageSize)
	if err != nil {
		blog.Fail(ctx, w, "loading posts", err)
		return
	}

	var carry [][2]string
	if status != "" {
		// A search from a filtered list keeps the filter.
		carry = [][2]string{{"status", status}}
	}

	blog.Render(ctx, w, "admin_list", http.StatusOK, blog.AdminListView{
		Head:        blog.Head{Title: "Posts"},
		Query:       q,
		Carry:       carry,
		Filter:      blog.BuildStatusFilter(q, status),
		NoMatchNote: blog.NoMatchNote(q, status),
		Rows:        blog.AdminRows(posts),
		Pagination:  blog.BuildPagination("/admin/posts", q, status, page, total),
		// The true blank state gets the empty-state card; a search or
		// filter that matched nothing gets a plain note instead. Telling
		// a writer with forty posts that their blog is empty is a lie.
		Empty:   all == 0,
		NoMatch: all > 0 && total == 0,
	})
}
```

- [ ] **Step 5: Update `admin_list.html`** — the list-bar call gains `"Filter" .Filter`, the note uses the computed text:

```html
{{template "list-bar" dict "SearchAction" "/admin/posts" "Query" .Query "Placeholder" "Search posts" "Hidden" .Carry "Filter" .Filter}}
{{if .NoMatch}}<p class="blog-note">{{.NoMatchNote}}</p>
```

- [ ] **Step 6: Regenerate and run the suite**

Run (in `examples/blog`):
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .`
then `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./... -count=1`
Expected: PASS, including the three new tests. `TestAdminListScreenCarriesItsStockComponents` (screens_test.go) may need the dropdown added to its expectations — if it fails, extend it, don't weaken it.

- [ ] **Step 7: Sweep + commit**

```bash
git add examples/blog
git commit -m "blog: admin list gains the status filter dropdown (F3)"
```

---

### Task 8: blog forms onto the partials; blog.css sheds its form half

**Files:**
- Modify: `examples/blog/internal/blog/templates/pages/admin_new.html`
- Modify: `examples/blog/internal/blog/templates/pages/admin_edit.html`
- Modify: `examples/blog/static/blog.css`
- Test: `examples/blog/internal/blogtest/admin_form_test.go`, `screens_test.go`

**Interfaces:**
- Consumes: `field-text`, `field-textarea`, `form-foot` (Tasks 2–4), `.rst-form` (Task 4).

- [ ] **Step 1: Update the tests first.** Read `admin_form_test.go`'s render assertions (`TestNewPostFormRenders`, `TestEditShowsCurrentValuesAndTheDraftPill`) and re-point any that assert the old markup (`blog-form`, bare `<label for=`, `<input type="text" id="title"`) at the new: `class="rst-form"`, `class="rst-input" id="title"`, `class="rst-textarea" id="body"`. Add one assertion each for new/edit that the page contains `class="rst-field"`. In `screens_test.go`, `TestAllEightPartialsAppearAcrossTheApp` (~line 73): add the four new partial markers to whatever inventory it checks (and rename it `TestAllStockPartialsAppearAcrossTheApp`).

- [ ] **Step 2: Run to verify the updated tests fail**

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./internal/blogtest/ -run 'TestNewPostForm|TestEditShows|TestAllStock' -v`
Expected: FAIL — screens still render the hand-rolled markup.

- [ ] **Step 3: Rewrite the two form templates.**

`admin_new.html`, full content:

```html
{{define "content"}}
{{template "page-header" dict "Title" "New post" "Sub" "It starts as a draft — nothing is public until you publish it."}}
{{if .Error}}<p class="blog-error" role="alert">{{.Error}}</p>{{end}}
<form class="rst-form" method="post" action="{{.Action}}">
{{template "field-text" dict "Name" "title" "Label" "Title" "Value" .FormTitle "Required" true}}
{{template "field-textarea" dict "Name" "body" "Label" "Body" "Value" .Body "Rows" 18}}
{{template "form-foot" dict "Submit" "Create post" "CancelHref" "/admin/posts" "CancelLabel" "Cancel"}}
</form>
{{end}}
```

`admin_edit.html` — same transformation; the status strip block at the top is untouched; the form becomes:

```html
<form class="rst-form" method="post" action="{{.Action}}">
{{template "field-text" dict "Name" "title" "Label" "Title" "Value" .FormTitle "Required" true}}
{{template "field-textarea" dict "Name" "body" "Label" "Body" "Value" .Body "Rows" 18}}
{{template "form-foot" dict "Submit" "Save" "CancelHref" "/admin/posts" "CancelLabel" "Back to posts"}}
</form>
```

- [ ] **Step 4: Shrink `blog.css`.** Delete the `.blog-form` block (container, label, input/textarea, `__actions`) and the focus-ring restatement (`.blog-form :focus-visible, .blog-status :focus-visible` — `.rst-page` now scopes the ring over both). Rewrite the header comment: the file now covers page styling the library deliberately leaves to the app (error/note lines, the status strip, the article measure, the footer) — the F2 reference moves to past tense. `.blog-error`, `.blog-note`, `.blog-status`, `.blog-article`, `.blog-footer` rules stay.

- [ ] **Step 5: Run the full blog suite**

Run (in `examples/blog`): `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod go test ./... -count=1`
Expected: PASS — including `TestBlogCSSIsSelfContained`, `TestBlogCSSStylesNoLibraryClass`, `TestBlogCSSUsesTokensNotLiterals` against the shrunk file, and the form round-trip tests (create with empty title still 400s: `required` is client-side; the server validation is untouched).

- [ ] **Step 6: Sweep + commit**

```bash
git add examples/blog
git commit -m "blog: forms on field-text/field-textarea/form-foot; blog.css sheds its form half (F2)"
```

---

### Task 9: documentation — ui package doc, friction log, READMEs

**Files:**
- Modify: `ui/ui.go` (package comment, lines 15-26)
- Modify: `examples/blog/README.md` (F2 ~line 109, F3 ~line 116)
- Modify: `README.md` (only if it enumerates the partials — `grep -n "eight partials\|list-row-action" README.md`; otherwise untouched)

- [ ] **Step 1: `ui/ui.go` package comment.** "The eight partials are…" becomes the twelve, and the container list gains the form element:

```
// The twelve partials are page-header, list-bar, list-bar-search,
// list-search-submit, list-row-action, status-pill, empty-state,
// pagination, field-text, field-textarea, form-foot and dropdown. Each
// takes exactly one data value; build it inline with dict (see Funcs).
// Each partial's own file carries its data contract in a template
// comment above the {{define}}.
//
// Three container elements the partials assume but do not emit, because
// they belong to the app's own page markup:
//
//	<div class="rst-page">   — the centred content column every screen sits in
//	<div class="rst-list">   — the card wrapping a list-bar and a run of rows
//	<form class="rst-form">  — the column a run of fields and a form-foot sit in
```

- [ ] **Step 2: friction log.** In `examples/blog/README.md`, append *Fixed:* postscripts in F9/F10's established voice:

To **F2**: `*Fixed:* the field-text/field-textarea/form-foot partials landed and these forms use them (see admin_new.html); tokens.css now scopes the focus ring to .rst-page, so the restatement is gone. blog.css kept only the page styling the library leaves to the app.`

To **F3**: `*Fixed:* list-bar takes a Filter dropdown — a native <details> menu of links — and this list filters by status with it (All / Drafts / Published, composing with search and paging).`

- [ ] **Step 3: Sweep both modules + commit**

```bash
git add ui/ui.go examples/blog/README.md README.md
git commit -m "docs: ui partial inventory + friction log F2/F3 closed"
```

---

## Verification (whole slice)

1. Root module and `examples/blog` and `examples/helloworld`: build, vet, `test ./... -count=1`, `gofmt -l` — all clean.
2. `grep -rn "<script" ui/ examples/blog/internal/blog/templates/` — nothing.
3. Manual smoke (optional, via `rastrillo dev` in `examples/blog` or the run skill): create a draft, publish another, filter by Drafts, search within the filter, page — the filter survives every hop; tab through the edit form — every control shows the ring; open the dropdown with keyboard only.
4. Push the branch, open a draft PR titled "ui: form partials + dropdown; blog adopts them (F2, F3)".

## Self-review notes

- Spec coverage: field-text (Task 2), field-textarea (Task 3), form-foot + `.rst-form` + focus fix (Task 4), dropdown + `--rst-shadow` (Task 1), list-bar Filter (Task 5), blog store/handler/template adoption (Tasks 6–7), forms + blog.css (Task 8), friction log + docs (Task 9). Out-of-scope list untouched.
- Type consistency: `Filter`/`FilterItem` field names = dropdown's key contract (`Label`, `Aria`, `Items`, `Href`, `Current`); `List(db, q, status, offset, limit)` and `Count(db, q, status)` used identically in Tasks 6–7; `BuildPagination(base, q, status, page, total)` updated at both call sites.
- Known judgment calls delegated to the implementer with guardrails: icon class marker (Task 1 Step 1), store test fixture style (Task 6 Step 1), screens_test inventory shape (Task 8 Step 1) — each says "read the file, keep the assertion, adapt the fixture".
