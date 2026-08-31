# Component Vocabulary + Localization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build rastrillo's default component library and its localization system — the standalone half of design doc §9 and §10 that does not need the manifest system — so a rastrillo app renders correctly-worded, zero-JS, token-skinned, locale-resolved HTML out of the box.

**Architecture:** One new runtime value, `*rastrillo.UI`, built once at boot from three inputs: the framework's embedded component templates + base English catalog, the app's embedded `locales/<code>.toml` catalogs, and the app's embedded `templates/*.html` overrides. `UI` holds one `*html/template.Template` set **per declared locale**, each bound via `Funcs` to that locale's `T`/`Tf` — titogo's proven shape (§10), shipped as a library. `rastrillo.Serve` installs `UI.Middleware`, which resolves the request's locale (URL prefix → `Accept-Language` → cookie), strips a matched locale path prefix so one route serves every locale, and stashes the locale and the `UI` on the request context. Actions call package-level `rastrillo.Render(w, r, name, data)` and `rastrillo.T(r, key)`; templates compose components by name with a `dict` helper.

**Tech Stack:** Go stdlib only (`html/template`, `embed`, `io/fs`, `net/http`), plus a hand-rolled ~90-line flat-TOML catalog decoder in `internal/catalog`. No JS. No CSS preprocessor. No bundler. Icons are vendored Lucide SVG constants in `internal/icons`.

---

## Context: what this slice is, and what it defers

**Design doc:** `carlosframework/platform` `docs/superpowers/specs/2026-08-01-carlos-framework-design.md` (approved, merged 2026-08-01).

**Sections implemented here:**

- **§9 Component vocabulary and UI generation** — the default component library (the named list in §9), the token-based CSS skin (custom properties, light/dark via `prefers-color-scheme` plus `data-theme` override, WCAG AA, framework-neutral), the zero-JS baseline, and override-by-existence for templates.
- **§10 Localization** — TOML catalogs one file per locale, the framework's base English catalog for built-in component copy, layered fallback, `Locales`/`DefaultLocale` on `Serve`, per-request resolution middleware onto `ctx.Locale`, `{{T}}`/`{{Tf}}` bound per locale clone, zero-JS locale switching.
- **§11 / §13 (partial)** — `rastrillo generate --check` gains the **i18n-catalog completeness** check ("silent fallback while iterating, loud failure before ship"). The other `--check` items §11 lists (manifest collision, additive-migration, mergeable-convergence, line-cap, agent tool/consent-gate) belong to subsystems this slice does not build.
- **§12 (untouched)** — the preloaded `CLAUDE.md`/skill is not part of this slice.

**Explicitly deferred, and why:**

- **The manifest system (`Resource`/`List`/`Form`, TOML manifests, codegen-with-skip).** §9's headline claim — "a `rastrillo.Resource{List: ..., Form: ...}` generates all four canonical states (Blank → List → Show → Edit/New) without the author naming them individually" — needs the manifest system, which does not exist in this repo yet. **This slice builds the standalone subset it stands on: the component partials themselves, callable by name from any hand-written template.** When the manifest system lands, its generator composes exactly these partials; nothing here has to change to allow that. §3 already licenses this order — "manifests are optional, per route" and "a plain `actions/*.go` file ... is a completely normal, first-class way to use the framework."
- **§10's "manifest labels are translation keys by generation"** (`resource.ticket_types.field.price`, title-cased fallback). Same reason: there is no manifest to generate keys from. The **lookup** half is built here (an unknown key returns the key verbatim), so the generator later only has to emit keys.
- **Mergeable store, blobs, crypto, WebAuthn, agents, `sqlc`** — out of scope, per the brief.
- **The `carlos vet` line-cap, idempotency, and headless zero-JS gates (§13)** — separate work; the zero-JS property is honoured by construction here (no `<script>` tag exists anywhere in this slice) but not machine-gated.

**Design-doc open question 1 (§15, TOML library) stays open.** It is about the *manifest* format, which needs tables, inline tables and arrays. Locale catalogs are flat `key = "string"` by design (§10, and §14's "flat key → string only, v1"), so `internal/catalog` hand-rolls exactly that subset in a page of code and buys nothing by deferring. Its package doc says so, so nobody later mistakes it for a manifest decoder.

---

## Global Constraints

Every task's requirements implicitly include this section.

- **Verification gate for every task**, from the repo root, before the commit step:
  `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
  (Prefix each `go` invocation with the three env vars, or `export` them once in the shell.) Plus `gofmt -l` clean for every `.go` file the task touched.
- **Branch:** `components-i18n`. Commit after every task. Do not push, do not open a PR.
- **stdlib only.** `go.mod` must gain **no** new `require` line. The repo's one direct dependency stays `modernc.org/sqlite`. Hand-roll page-of-code concerns (existing precedent: `cmd/rastrillo/modpath.go`, `internal/devloop/watch.go`).
- **Icons are vendored Lucide as in-code SVG constants, never a CDN.** Emoji is content, icons are chrome. No `<link>` or `<script>` to any external host appears anywhere in this slice.
- **No JS build step, no bundler, no `<script>` tag.** §9: "every generated screen works with JavaScript disabled (search/filter/sort as GET round-trips, destructive actions as their own confirm-page URL, modals as their own routes)". Where interactivity is unavoidable, use native HTML (`<details>`, `<progress>`, `<select>` + submit), never JS.
- **Additive wire/API shapes.** Every new field on `rastrillo.Options` is optional; an app that sets none of them must behave exactly as it does today. `examples/helloworld` is deployed live — it may change, but only deliberately (Tasks 14–15).
- **Comment style:** comments state constraints and design rationale (why this shape, what breaks otherwise), never narrate the next line. Match `serve.go` and `internal/generate/generate.go`.
- **Tests are table-driven** where there is more than one case. Existing precedent: `internal/generate/generate_test.go`, `internal/devloop/watch_test.go`.
- **400 lines per Go file** (§13, backstop only). No file in this slice comes close; if one does, split by concern into `internal/`.
- **Component `{{define}}` names are the design doc's own names, verbatim** — `page-header`, `box`, `list-bar`, `list-row`, `person`, `status-pill`, `badge`, `meter`, `mono`, `detail-list`, `alert`, `field`, `toggle-block`, `seg-tabs`, `form-foot`, `menu`, `confirm`, `modal-route`, `empty-state`, `bulk-select`, `pagination`, `help-link`. CSS classes are all prefixed `r-`. The name collision between a framework define and an app define **is** the override mechanism (§9: "override-by-existence extends to templates").
- **Every string a built-in component renders comes from the catalog** (§9), never a baked literal in the markup.

---

## File map

| File | Responsibility | Task |
|---|---|---|
| `internal/catalog/catalog.go` | flat-TOML `key = "string"` decoder | 1 |
| `locale.go` | `Catalog`, `Locales`, layered lookup, `{name}` interpolation | 2 |
| `localemw.go` | locale resolution middleware, `LocaleFrom`, request-scoped `T`/`Tf` | 3 |
| `internal/icons/icons.go` | vendored Lucide SVG constants | 4 |
| `assets.go` + `ui/base.css` | embedded token stylesheet, `Stylesheet()`, `StylesheetPath` | 5 |
| `ui.go` + `ui/layout.html` + `ui/locales/en.toml` | `UI` runtime, per-locale sets, `Render`, base catalog | 6 |
| `serve.go`, `ctx.go` | `Options` fields, middleware + stylesheet route install | 7 |
| `ui/structure.html` | page-header, box, list-bar, list-row, empty-state, pagination | 8 |
| `ui/display.html` | person, status-pill, badge, meter, mono, detail-list, alert, help-link | 9 |
| `ui/form.html` | field family, toggle-block, seg-tabs, form-foot, menu, confirm, modal-route, bulk-select | 10 |
| `internal/generate/locales.go` | `MissingKeys` — catalog completeness | 11 |
| `cmd/rastrillo/generate.go`, `dev.go`, `main.go` | `--check` flag, watch `locales/` + `templates/` | 12 |
| `cmd/rastrillo/new.go` | scaffold `embed.go`, `locales/`, `templates/` | 13 |
| `examples/helloworld/{embed.go,locales/*}` | example app assets | 14 |
| `examples/helloworld/{templates,actions,cmd}` | example page, regenerated `gen/` | 15 |
| `README.md` | document the slice | 16 |

---

### Task 1: `internal/catalog` — the flat-TOML catalog decoder

**Files:**
- Create: `internal/catalog/catalog.go`
- Test: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: nothing from this repo.
- Produces (Tasks 6, 11, 13 import this exactly):
  - `func Decode(data []byte) (map[string]string, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/catalog/catalog_test.go`:

```go
package catalog

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "basic pairs",
			in:   "ui.save = \"Save\"\nui.cancel = \"Cancel\"\n",
			want: map[string]string{"ui.save": "Save", "ui.cancel": "Cancel"},
		},
		{
			name: "blank lines and comments",
			in:   "# a catalog\n\nui.save = \"Save\"\n\n# trailing note\n",
			want: map[string]string{"ui.save": "Save"},
		},
		{
			name: "dots in a key are literal, not table nesting",
			in:   "resource.ticket_types.field.price = \"Price\"\n",
			want: map[string]string{"resource.ticket_types.field.price": "Price"},
		},
		{
			name: "escapes in a basic string",
			in:   "k = \"one\\ntwo\\t\\\"quoted\\\"\"\n",
			want: map[string]string{"k": "one\ntwo\t\"quoted\""},
		},
		{
			name: "literal string keeps backslashes",
			in:   "k = 'C:\\path'\n",
			want: map[string]string{"k": `C:\path`},
		},
		{
			name: "trailing comment after a value",
			in:   "k = \"Save\"   # keep short\n",
			want: map[string]string{"k": "Save"},
		},
		{
			name: "hash inside a string is content, not a comment",
			in:   "k = \"tag #1\"\n",
			want: map[string]string{"k": "tag #1"},
		},
		{
			name: "CRLF line endings",
			in:   "k = \"Save\"\r\nj = \"Cancel\"\r\n",
			want: map[string]string{"k": "Save", "j": "Cancel"},
		},
		{
			name: "placeholder braces survive verbatim",
			in:   "ui.pagination.count = \"Page {page} of {pages}\"\n",
			want: map[string]string{"ui.pagination.count": "Page {page} of {pages}"},
		},
		{
			name: "empty input",
			in:   "",
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.in))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decode = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantSub string
	}{
		{"table header", "[list]\nk = \"v\"\n", "tables are not part"},
		{"unquoted value", "k = v\n", "must be a quoted string"},
		{"no equals", "just a line\n", `expected key = "value"`},
		{"duplicate key", "k = \"a\"\nk = \"b\"\n", `duplicate key "k"`},
		{"unterminated basic string", "k = \"oops\n", "unterminated string"},
		{"unterminated literal string", "k = 'oops\n", "unterminated literal string"},
		{"junk after value", "k = \"a\" b\n", "unexpected trailing content"},
		{"invalid key", "k y = \"a\"\n", "invalid key"},
		{"empty key", " = \"a\"\n", "invalid key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			if err == nil {
				t.Fatalf("want an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
			if !strings.HasPrefix(err.Error(), "line ") {
				t.Errorf("error = %q, want it to start with a line number", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/catalog/ -count=1`
Expected: FAIL — the package does not exist / `Decode` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/catalog/catalog.go`:

```go
// Package catalog decodes the flat `key = "string"` TOML subset
// rastrillo's locale catalogs use (design doc §10: "One TOML file per
// locale ... flat key → string"; §14: "No pluralization/ICU-style
// grammar rules in the localization catalog — flat key → string only,
// v1").
//
// This is deliberately NOT a general TOML decoder, and must not grow
// into one. The design doc's open question 1 (§15) — whether the
// *manifest* system takes a TOML dependency or hand-rolls a decoder —
// is about a format with tables, arrays and inline tables, and stays
// open. Catalogs need none of that, so a page of code covers them and
// deferring buys nothing.
package catalog

import (
	"fmt"
	"strconv"
	"strings"
)

// Decode parses one catalog file: one `key = "value"` per line, plus
// blank lines and # comments. A dot inside a key is a literal character,
// not table nesting — `resource.ticket_types.field.price` is one flat
// key, which is exactly the key shape design doc §10 specifies.
func Decode(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("line %d: tables are not part of the flat catalog format", i+1)
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf(`line %d: expected key = "value"`, i+1)
		}
		key := strings.TrimSpace(line[:eq])
		if !validKey(key) {
			return nil, fmt.Errorf("line %d: invalid key %q", i+1, key)
		}
		val, err := parseValue(strings.TrimSpace(line[eq+1:]))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", i+1, key)
		}
		out[key] = val
	}
	return out, nil
}

// validKey accepts TOML bare-key characters plus the dot, which catalogs
// use as a flat namespace separator rather than as nesting.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// parseValue accepts a TOML basic string ("..." with backslash escapes)
// or a literal string ('...', no escapes), optionally followed by a
// # comment. The closing quote is found before any comment is
// considered, so a # inside a translated string stays content.
func parseValue(s string) (string, error) {
	switch {
	case strings.HasPrefix(s, `"`):
		end := -1
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return "", fmt.Errorf("unterminated string")
		}
		if err := checkTrailer(s[end+1:]); err != nil {
			return "", err
		}
		// TOML basic-string escapes are a subset of Go's interpreted
		// string literal escapes, so Unquote is exactly right here.
		return strconv.Unquote(s[:end+1])
	case strings.HasPrefix(s, "'"):
		end := strings.Index(s[1:], "'")
		if end < 0 {
			return "", fmt.Errorf("unterminated literal string")
		}
		if err := checkTrailer(s[end+2:]); err != nil {
			return "", err
		}
		return s[1 : end+1], nil
	default:
		return "", fmt.Errorf("value must be a quoted string")
	}
}

func checkTrailer(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("unexpected trailing content %q", rest)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/catalog/ -count=1`
Expected: PASS — 10 `TestDecode` subtests, 9 `TestDecodeErrors` subtests.

- [ ] **Step 5: gofmt + full sweep**

Run: `gofmt -l internal/catalog/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/catalog/
git commit -m "catalog: flat key = \"string\" TOML decoder for locale catalogs (design doc §10)"
```

---

### Task 2: `locale.go` — `Locales`, layered lookup, `{name}` interpolation

**Files:**
- Create: `locale.go` (package `rastrillo`, repo root)
- Test: `locale_test.go`

**Interfaces:**
- Consumes: `catalog.Decode(data []byte) (map[string]string, error)` from Task 1.
- Produces (Tasks 3, 6, 7 use these exactly):
  - `type Catalog map[string]string`
  - `type Locales struct { ... }` — unexported fields
  - `func NewLocales(codes []string, def string, base Catalog, fsys fs.FS) (*Locales, error)`
  - `func (l *Locales) Codes() []string`
  - `func (l *Locales) Default() string`
  - `func (l *Locales) Has(code string) bool`
  - `func (l *Locales) T(locale, key string) string`
  - `func (l *Locales) Tf(locale, key string, args ...any) string`
  - `func interpolate(s string, args []any) string` (unexported; Task 3 calls it)

**Decision recorded (design doc silent):** the doc writes `{{Tf "key" .Args}}` without naming a placeholder syntax. This uses **named `{name}` placeholders** — word order changes between languages, so positional verbs are the wrong default — and accepts arguments either as **slog-style alternating name/value pairs** (the convention this repo already uses, see `serve.go`'s `logger.Info("rastrillo: serving", "addr", ..., "version", ...)`) **or as a single map**, which is exactly what the doc's own `.Args` example passes. Both forms are supported so the doc's literal example works verbatim.

**Decision recorded (design doc partially silent):** §10 says "missing keys fall back to the declared default locale during development" and says the framework ships a base English catalog. Lookup is therefore four layers, in order: the requested locale's app catalog → the default locale's app catalog → the framework base catalog → **the key itself, verbatim**. Returning the key rather than `""` makes a missing string visible on the page instead of silently blanking a sentence.

- [ ] **Step 1: Write the failing test**

Create `locale_test.go`:

```go
package rastrillo

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"locales/en.toml": {Data: []byte("app.title = \"Orders\"\napp.hi = \"Hello {name}\"\n")},
		"locales/fr.toml": {Data: []byte("app.title = \"Commandes\"\n")},
	}
}

func TestNewLocalesReadsCatalogs(t *testing.T) {
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Codes(); !reflect.DeepEqual(got, []string{"en", "fr"}) {
		t.Errorf("Codes = %v, want [en fr]", got)
	}
	if l.Default() != "en" {
		t.Errorf("Default = %q, want en", l.Default())
	}
	if !l.Has("fr") || l.Has("de") {
		t.Errorf("Has: fr should be declared, de should not")
	}
}

func TestLookupLayers(t *testing.T) {
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		locale, key  string
		want         string
	}{
		{"app catalog for the requested locale", "fr", "app.title", "Commandes"},
		{"falls back to the default locale's catalog", "fr", "app.hi", "Hello {name}"},
		{"falls back to the framework base catalog", "fr", "ui.save", "Save"},
		{"unknown key returns the key verbatim", "fr", "nope.nothing", "nope.nothing"},
		{"undeclared locale still resolves via the default", "de", "app.title", "Orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.T(tt.locale, tt.key); got != tt.want {
				t.Errorf("T(%q, %q) = %q, want %q", tt.locale, tt.key, got, tt.want)
			}
		})
	}
}

func TestTfInterpolation(t *testing.T) {
	l, err := NewLocales([]string{"en"}, "en", Catalog{
		"greet":   "Hello {name}, you have {count} orders",
		"noargs":  "Plain",
		"missing": "Hi {who}",
		"twice":   "{a} and {a}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		key  string
		args []any
		want string
	}{
		{"slog-style pairs", "greet", []any{"name", "Ada", "count", 3}, "Hello Ada, you have 3 orders"},
		{"single map[string]any", "greet", []any{map[string]any{"name": "Ada", "count": 3}}, "Hello Ada, you have 3 orders"},
		{"single map[string]string", "greet", []any{map[string]string{"name": "Ada", "count": "3"}}, "Hello Ada, you have 3 orders"},
		{"no placeholders", "noargs", nil, "Plain"},
		{"unmatched placeholder is left verbatim", "missing", []any{"name", "Ada"}, "Hi {who}"},
		{"repeated placeholder", "twice", []any{"a", "x"}, "x and x"},
		{"odd argument count ignores the tail", "greet", []any{"name", "Ada", "count"}, "Hello Ada, you have {count} orders"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.Tf("en", tt.key, tt.args...); got != tt.want {
				t.Errorf("Tf(%q, %v) = %q, want %q", tt.key, tt.args, got, tt.want)
			}
		})
	}
}

func TestNewLocalesDefaults(t *testing.T) {
	l, err := NewLocales(nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Codes(); !reflect.DeepEqual(got, []string{"en"}) {
		t.Errorf("Codes = %v, want [en] for an app that declares nothing", got)
	}
	if l.Default() != "en" {
		t.Errorf("Default = %q, want en", l.Default())
	}
}

func TestNewLocalesRejectsBadSets(t *testing.T) {
	tests := []struct {
		name    string
		codes   []string
		def     string
		wantSub string
	}{
		{"default not declared", []string{"fr", "de"}, "en", `default locale "en" is not in the declared set`},
		{"duplicate code", []string{"en", "en"}, "en", `duplicate locale code "en"`},
		{"empty code", []string{"en", ""}, "en", "empty locale code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLocales(tt.codes, tt.def, nil, nil)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestNewLocalesReportsABadCatalog(t *testing.T) {
	bad := fstest.MapFS{"locales/en.toml": {Data: []byte("[list]\n")}}
	_, err := NewLocales([]string{"en"}, "en", nil, bad)
	if err == nil {
		t.Fatal("want an error for an undecodable catalog, got nil")
	}
	if !strings.Contains(err.Error(), "locales/en.toml") {
		t.Errorf("error = %q, want it to name the offending file", err)
	}
}

func TestNewLocalesToleratesAMissingCatalog(t *testing.T) {
	// A single-locale app declares "en" and ships no locales/ at all:
	// the framework base catalog alone must carry it.
	l, err := NewLocales([]string{"en", "fr"}, "en", Catalog{"ui.save": "Save"}, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if got := l.T("fr", "ui.save"); got != "Save" {
		t.Errorf("T = %q, want Save", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: FAIL — `NewLocales`, `Catalog` undefined.

- [ ] **Step 3: Write the implementation**

Create `locale.go`:

```go
package rastrillo

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// Catalog is one locale's flat key → string table (design doc §10).
type Catalog map[string]string

// Locales is an app's declared locale set, its own catalogs, and the
// framework's base English catalog underneath them.
//
// Lookup is layered, in this order: the requested locale's app catalog,
// the default locale's app catalog, the framework base catalog, then the
// key itself. The middle layer is design doc §10's "missing keys fall
// back to the declared default locale during development"; the base
// layer is what lets a single-locale app get correctly-worded built-in
// components without writing a catalog at all. Returning the key —
// never "" — keeps a missing string visible on the page instead of
// silently blanking a sentence.
//
// The framework base catalog is English-only in v1, exactly as §10
// describes it ("its own base English catalog"), and is consulted last
// whatever the requested locale is.
type Locales struct {
	codes []string
	def   string
	app   map[string]Catalog
	base  Catalog
}

// NewLocales validates the declared set and reads locales/<code>.toml
// out of fsys for each declared code. fsys may be nil (framework base
// catalog only). A declared locale with no catalog file is not an error:
// a single-locale app declares "en" and ships no locales/ directory.
func NewLocales(codes []string, def string, base Catalog, fsys fs.FS) (*Locales, error) {
	if def == "" {
		def = "en"
	}
	if len(codes) == 0 {
		codes = []string{def}
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if c == "" {
			return nil, fmt.Errorf("rastrillo: empty locale code in %v", codes)
		}
		if seen[c] {
			return nil, fmt.Errorf("rastrillo: duplicate locale code %q", c)
		}
		seen[c] = true
	}
	if !seen[def] {
		return nil, fmt.Errorf("rastrillo: default locale %q is not in the declared set %v", def, codes)
	}

	l := &Locales{
		codes: append([]string(nil), codes...),
		def:   def,
		app:   map[string]Catalog{},
		base:  base,
	}
	if l.base == nil {
		l.base = Catalog{}
	}
	if fsys == nil {
		return l, nil
	}
	for _, c := range codes {
		name := path.Join("locales", c+".toml")
		data, err := fs.ReadFile(fsys, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("rastrillo: read %s: %w", name, err)
		}
		m, err := catalog.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("rastrillo: %s: %w", name, err)
		}
		l.app[c] = Catalog(m)
	}
	return l, nil
}

// Codes returns the declared locale codes in declaration order.
func (l *Locales) Codes() []string { return append([]string(nil), l.codes...) }

// Default returns the declared default locale.
func (l *Locales) Default() string { return l.def }

// Has reports whether code is one of the declared locales.
func (l *Locales) Has(code string) bool {
	for _, c := range l.codes {
		if c == code {
			return true
		}
	}
	return false
}

// T looks key up for locale, layered per this type's doc comment.
func (l *Locales) T(locale, key string) string {
	if v, ok := l.app[locale][key]; ok {
		return v
	}
	if v, ok := l.app[l.def][key]; ok {
		return v
	}
	if v, ok := l.base[key]; ok {
		return v
	}
	return key
}

// Tf is T plus {name} placeholder interpolation — design doc §10's
// `{{Tf "key" .Args}}`. args are slog-style alternating name/value
// pairs (the convention this repo already uses for key/value lists), or
// a single map[string]any / map[string]string, which is exactly what
// the doc's own .Args example passes.
func (l *Locales) Tf(locale, key string, args ...any) string {
	return interpolate(l.T(locale, key), args)
}

// interpolate substitutes {name} placeholders. A placeholder with no
// matching argument is left verbatim: a translator's typo then shows up
// in the page rather than silently deleting part of a sentence.
func interpolate(s string, args []any) string {
	if !strings.Contains(s, "{") {
		return s
	}
	m := argMap(args)
	var b strings.Builder
	for {
		i := strings.Index(s, "{")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		j += i
		if v, ok := m[s[i+1:j]]; ok {
			b.WriteString(s[:i])
			b.WriteString(v)
		} else {
			b.WriteString(s[:j+1])
		}
		s = s[j+1:]
	}
}

func argMap(args []any) map[string]string {
	if len(args) == 1 {
		switch v := args[0].(type) {
		case map[string]string:
			return v
		case map[string]any:
			m := make(map[string]string, len(v))
			for k, val := range v {
				m[k] = fmt.Sprint(val)
			}
			return m
		}
	}
	m := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		m[fmt.Sprint(args[i])] = fmt.Sprint(args[i+1])
	}
	return m
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1 -run 'Locale|Lookup|Tf'`
Expected: PASS.

- [ ] **Step 5: gofmt + full sweep**

Run: `gofmt -l locale.go locale_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add locale.go locale_test.go
git commit -m "i18n: Locales — layered catalog lookup and {name} interpolation (design doc §10)"
```

---

### Task 3: `localemw.go` — per-request locale resolution

**Files:**
- Create: `localemw.go`
- Test: `localemw_test.go`

**Interfaces:**
- Consumes from Task 2: `(*Locales).Has`, `(*Locales).T`, `(*Locales).Tf`, `(*Locales).Default`, the unexported fields `codes`/`def`, and `interpolate(s string, args []any) string`.
- Produces (Tasks 6, 7, 13, 15 use these exactly):
  - `const LocaleCookie = "rastrillo_locale"`
  - `func (l *Locales) Middleware(next http.Handler) http.Handler`
  - `func LocaleFrom(r *http.Request) string`
  - `func T(r *http.Request, key string) string`
  - `func Tf(r *http.Request, key string, args ...any) string`

**Decisions recorded (design doc silent):**
- §10 names "a stored preference" without naming the mechanism. This uses a **cookie, `rastrillo_locale`** — the only stored preference that works with JavaScript disabled (§9's hard rule).
- §10's precedence is stated literally as "URL path prefix, then `Accept-Language`, then a stored preference." That is unusual (a stored preference normally beats `Accept-Language`) but the doc is specific, so it is followed **verbatim**, and the code comment says so.
- A matched locale prefix is **stripped from the path** before the app's mux sees it, so one route serves every locale. §10 requires locale switching to be "a plain link to the same path under a different locale prefix"; that only works if `/fr/orders` and `/orders` reach the same handler.

- [ ] **Step 1: Write the failing test**

Create `localemw_test.go`:

```go
package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// probe records what the wrapped handler saw, so a test can assert on
// both the resolved locale and the path the mux would have matched.
func probe(t *testing.T, l *Locales, req *http.Request) (locale, path, translated string) {
	t.Helper()
	h := l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		locale, path, translated = LocaleFrom(r), r.URL.Path, T(r, "app.title")
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return locale, path, translated
}

func mwLocales(t *testing.T) *Locales {
	t.Helper()
	l, err := NewLocales([]string{"en", "fr", "de-informal"}, "en", Catalog{"ui.save": "Save"}, testFS())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResolutionPrecedence(t *testing.T) {
	l := mwLocales(t)
	tests := []struct {
		name       string
		path       string
		accept     string
		cookie     string
		wantLocale string
		wantPath   string
	}{
		{"path prefix wins over everything", "/fr/orders", "de-informal", "en", "fr", "/orders"},
		{"bare locale prefix becomes /", "/fr", "", "", "fr", "/"},
		{"default locale prefix is stripped too", "/en/orders", "", "", "en", "/orders"},
		{"Accept-Language when there is no prefix", "/orders", "fr", "en", "fr", "/orders"},
		{"Accept-Language honours q order", "/orders", "de-informal;q=0.4, fr;q=0.9", "", "fr", "/orders"},
		{"Accept-Language matches on the primary subtag", "/orders", "fr-CA", "", "fr", "/orders"},
		{"cookie only when the header names nothing declared", "/orders", "es", "fr", "fr", "/orders"},
		{"cookie ignored when it names an undeclared locale", "/orders", "", "es", "en", "/orders"},
		{"default when nothing matches", "/orders", "", "", "en", "/orders"},
		{"a path segment that merely looks like one is not a locale", "/design/fr", "", "", "en", "/design/fr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept-Language", tt.accept)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: LocaleCookie, Value: tt.cookie})
			}
			gotLocale, gotPath, _ := probe(t, l, req)
			if gotLocale != tt.wantLocale {
				t.Errorf("locale = %q, want %q", gotLocale, tt.wantLocale)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

func TestMiddlewareBindsTranslation(t *testing.T) {
	l := mwLocales(t)
	_, _, fr := probe(t, l, httptest.NewRequest("GET", "/fr/orders", nil))
	if fr != "Commandes" {
		t.Errorf("T(r, \"app.title\") = %q, want Commandes", fr)
	}
	_, _, en := probe(t, l, httptest.NewRequest("GET", "/orders", nil))
	if en != "Orders" {
		t.Errorf("T(r, \"app.title\") = %q, want Orders", en)
	}
}

func TestMiddlewareDoesNotMutateTheIncomingRequest(t *testing.T) {
	l := mwLocales(t)
	req := httptest.NewRequest("GET", "/fr/orders", nil)
	probe(t, l, req)
	if req.URL.Path != "/fr/orders" {
		t.Errorf("incoming request was mutated: path = %q", req.URL.Path)
	}
}

func TestTranslationOutsideAMiddlewareRequest(t *testing.T) {
	// A request that never went through Middleware must not guess a
	// locale: T returns the key, Tf interpolates it and nothing else.
	req := httptest.NewRequest("GET", "/", nil)
	if got := T(req, "ui.save"); got != "ui.save" {
		t.Errorf("T = %q, want the key verbatim", got)
	}
	if got := Tf(req, "hello {name}", "name", "Ada"); got != "hello Ada" {
		t.Errorf("Tf = %q, want %q", got, "hello Ada")
	}
}

func TestLocaleFromWithoutMiddleware(t *testing.T) {
	if got := LocaleFrom(httptest.NewRequest("GET", "/", nil)); got != "" {
		t.Errorf("LocaleFrom = %q, want \"\"", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: FAIL — `LocaleCookie`, `Middleware`, `LocaleFrom` undefined.

- [ ] **Step 3: Write the implementation**

Create `localemw.go`:

```go
package rastrillo

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// LocaleCookie is the stored-preference cookie the resolution chain
// consults last. Design doc §10 names "a stored preference" without
// naming the mechanism; a cookie is the only one that survives §9's
// zero-JS baseline.
const LocaleCookie = "rastrillo_locale"

type localeCtxKey struct{}
type localesCtxKey struct{}

// Middleware resolves this request's locale and puts it, and this set,
// on the request context for LocaleFrom/T/Tf.
//
// Precedence is design doc §10's, verbatim: URL path prefix, then
// Accept-Language, then the stored-preference cookie. (A stored
// preference beating Accept-Language would be the more usual order —
// the doc is specific, so the doc wins.)
//
// A matched locale prefix is stripped from the path before the app's
// mux sees it, so one route serves every locale. §10's zero-JS locale
// switch is "a plain link to the same path under a different locale
// prefix", which only works if /fr/orders and /orders reach the same
// handler.
func (l *Locales) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, rest := l.splitPrefix(r.URL.Path)
		if code == "" {
			code = l.negotiate(r.Header.Get("Accept-Language"))
		}
		if code == "" {
			if c, err := r.Cookie(LocaleCookie); err == nil && l.Has(c.Value) {
				code = c.Value
			}
		}
		if code == "" {
			code = l.def
		}

		ctx := context.WithValue(r.Context(), localeCtxKey{}, code)
		ctx = context.WithValue(ctx, localesCtxKey{}, l)
		// Clone, never mutate: the caller's request and URL belong to
		// whatever wrapped us, and a rewritten path must not leak out.
		r2 := r.Clone(ctx)
		if rest != "" {
			r2.URL.Path = rest
			r2.URL.RawPath = ""
		}
		next.ServeHTTP(w, r2)
	})
}

// splitPrefix returns the declared locale named by the path's first
// segment and the path with that segment removed, or ("", "") when the
// first segment is not a declared locale.
func (l *Locales) splitPrefix(p string) (code, rest string) {
	if !strings.HasPrefix(p, "/") {
		return "", ""
	}
	seg := p[1:]
	rest = "/"
	if i := strings.Index(seg, "/"); i >= 0 {
		seg, rest = seg[:i], seg[i:]
	}
	if seg == "" || !l.Has(seg) {
		return "", ""
	}
	return seg, rest
}

// negotiate picks the best declared locale for an Accept-Language
// header: exact tag match first, then a primary-subtag match, so an
// "fr-CA" request lands on a declared "fr" or "fr-informal". Highest q
// wins; declaration order breaks ties.
func (l *Locales) negotiate(header string) string {
	type pref struct {
		tag string
		q   float64
	}
	var prefs []pref
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			for _, p := range strings.Split(part[i+1:], ";") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						q = f
					}
				}
			}
		}
		if tag == "" || q <= 0 {
			continue
		}
		prefs = append(prefs, pref{strings.ToLower(tag), q})
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })

	for _, p := range prefs {
		for _, c := range l.codes {
			if strings.EqualFold(c, p.tag) {
				return c
			}
		}
	}
	for _, p := range prefs {
		for _, c := range l.codes {
			if strings.EqualFold(primarySubtag(c), primarySubtag(p.tag)) {
				return c
			}
		}
	}
	return ""
}

func primarySubtag(tag string) string {
	if i := strings.Index(tag, "-"); i >= 0 {
		return tag[:i]
	}
	return tag
}

// LocaleFrom returns the locale Middleware resolved for r, or "" if the
// request never went through it.
func LocaleFrom(r *http.Request) string {
	code, _ := r.Context().Value(localeCtxKey{}).(string)
	return code
}

// T translates key in the request's resolved locale — the lookup an
// action calls. Outside a request that went through Middleware it
// returns the key verbatim rather than guessing a locale.
func T(r *http.Request, key string) string {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok {
		return key
	}
	return l.T(LocaleFrom(r), key)
}

// Tf is T plus {name} placeholder interpolation. See (*Locales).Tf for
// the accepted argument forms.
func Tf(r *http.Request, key string, args ...any) string {
	l, ok := r.Context().Value(localesCtxKey{}).(*Locales)
	if !ok {
		return interpolate(key, args)
	}
	return l.Tf(LocaleFrom(r), key, args...)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: PASS — 10 `TestResolutionPrecedence` subtests plus the four standalone tests.

- [ ] **Step 5: gofmt + full sweep**

Run: `gofmt -l localemw.go localemw_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add localemw.go localemw_test.go
git commit -m "i18n: per-request locale resolution — prefix, Accept-Language, cookie (design doc §10)"
```

---

### Task 4: `internal/icons` — vendored Lucide SVG constants

**Files:**
- Create: `internal/icons/icons.go`
- Test: `internal/icons/icons_test.go`

**Interfaces:**
- Consumes: nothing from this repo.
- Produces (Task 6 imports these exactly):
  - `func SVG(name string) template.HTML`
  - `func Names() []string`

**Constraint:** icons are chrome, emoji is content. Vendored as in-code constants, **never** a CDN link, never a sprite fetched at runtime — a rastrillo app ships as one static binary (§11). The test asserts no icon body contains `http`.

- [ ] **Step 1: Write the failing test**

Create `internal/icons/icons_test.go`:

```go
package icons

import (
	"reflect"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	want := []string{
		"check", "chevron-down", "chevron-left", "chevron-right",
		"circle-check", "circle-help", "circle-x", "ellipsis", "info",
		"plus", "search", "triangle-alert", "user", "x",
	}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v (sorted)", got, want)
	}
}

func TestSVGWrapsEveryVendoredIcon(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			got := string(SVG(name))
			if !strings.HasPrefix(got, "<svg ") {
				t.Errorf("SVG(%q) does not start with <svg: %q", name, got)
			}
			if !strings.HasSuffix(got, "</svg>") {
				t.Errorf("SVG(%q) does not end with </svg>: %q", name, got)
			}
			if !strings.Contains(got, `viewBox="0 0 24 24"`) {
				t.Errorf("SVG(%q) missing the shared viewBox", name)
			}
			if !strings.Contains(got, `aria-hidden="true"`) {
				t.Errorf("SVG(%q) must be aria-hidden — icons are chrome, the label is text", name)
			}
			if strings.Contains(got, "<") == false {
				t.Errorf("SVG(%q) has no body", name)
			}
		})
	}
}

func TestNoIconReachesTheNetwork(t *testing.T) {
	// Vendored means vendored: no CDN, no sprite href, no remote
	// reference of any kind can appear in an icon constant.
	for _, name := range Names() {
		body := string(SVG(name))
		for _, bad := range []string{"http", "//", "xlink", "<use", "<image"} {
			if strings.Contains(body, bad) {
				t.Errorf("SVG(%q) contains %q — icons are vendored constants, never fetched", name, bad)
			}
		}
	}
}

func TestUnknownIconRendersNothing(t *testing.T) {
	if got := SVG("no-such-icon"); got != "" {
		t.Errorf("SVG(unknown) = %q, want empty — an unknown icon must not break the page", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/icons/ -count=1`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/icons/icons.go`:

```go
// Package icons holds vendored Lucide icons as in-code SVG constants.
//
// Icons are chrome, emoji is content — a CARLOS-wide rule. Never a CDN
// link, never a sprite fetched at runtime, never an <img>: the shipped
// artifact is one static binary (design doc §11), and a component
// library whose icons need the network is not a component library.
//
// Each entry below is the *inner* markup of Lucide's 24x24 outline icon;
// SVG wraps it in the shared <svg> attributes so viewBox, stroke width,
// linecap and linejoin are stated once. Add an icon by pasting its
// Lucide body here — never by widening the mechanism.
package icons

import (
	"html/template"
	"sort"
)

const openTag = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" ` +
	`fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" ` +
	`stroke-linejoin="round" class="r-icon" aria-hidden="true" focusable="false">`

var bodies = map[string]string{
	"check":          `<path d="M20 6 9 17l-5-5"/>`,
	"chevron-down":   `<path d="m6 9 6 6 6-6"/>`,
	"chevron-left":   `<path d="m15 18-6-6 6-6"/>`,
	"chevron-right":  `<path d="m9 18 6-6-6-6"/>`,
	"circle-check":   `<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>`,
	"circle-help":    `<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>`,
	"circle-x":       `<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>`,
	"ellipsis":       `<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`,
	"info":           `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`,
	"plus":           `<path d="M5 12h14"/><path d="M12 5v14"/>`,
	"search":         `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
	"triangle-alert": `<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>`,
	"user":           `<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>`,
	"x":              `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
}

// SVG returns the named icon as inline SVG markup, or "" if the name is
// not vendored — an unknown icon renders as nothing rather than
// breaking the page around it.
func SVG(name string) template.HTML {
	body, ok := bodies[name]
	if !ok {
		return ""
	}
	return template.HTML(openTag + body + "</svg>")
}

// Names returns the vendored icon names, sorted.
func Names() []string {
	out := make([]string, 0, len(bodies))
	for name := range bodies {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/icons/ -count=1`
Expected: PASS — 14 `TestSVGWrapsEveryVendoredIcon` subtests plus three standalone tests.

- [ ] **Step 5: gofmt + full sweep**

Run: `gofmt -l internal/icons/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/icons/
git commit -m "icons: vendored Lucide SVG constants — chrome, never a CDN (design doc §9)"
```

---

### Task 5: `assets.go` + `ui/base.css` — the token stylesheet

**Files:**
- Create: `assets.go`
- Create: `ui/base.css`
- Test: `assets_test.go`

**Interfaces:**
- Consumes: nothing from this repo.
- Produces (Tasks 6, 7 and every component template use these exactly):
  - `const StylesheetPath = "/_rastrillo/ui.css"`
  - `func Stylesheet() []byte`

**Decision recorded (design doc silent):** §9 mandates a token-based CSS skin but names no serving path. `/_rastrillo/ui.css` is fixed as a **constant, not an option** — component markup references it, so an app-configurable path would mean the components could not name it. The `_rastrillo/` prefix keeps it out of any plausible app route namespace.

**Palette note:** framework-neutral, explicitly not titogo's cornflower branding (§9). Body-text pairs are chosen for WCAG AA: `#5b6470` on `#ffffff` ≈ 5.1:1, `#2f5bd7` on `#ffffff` ≈ 6.7:1, `#ffffff` on `#2f5bd7` ≈ 6.7:1, `#a3adba` on `#0f1216` ≈ 9:1.

- [ ] **Step 1: Write the failing test**

Create `assets_test.go`:

```go
package rastrillo

import (
	"strings"
	"testing"
)

func TestStylesheetIsEmbedded(t *testing.T) {
	css := string(Stylesheet())
	if len(css) < 1000 {
		t.Fatalf("stylesheet is %d bytes, want the real thing", len(css))
	}
	for _, want := range []string{
		"--r-bg:", "--r-surface:", "--r-border:", "--r-text:", "--r-muted:",
		"--r-accent:", "--r-accent-text:", "--r-ok:", "--r-warn:", "--r-danger:",
		"--r-radius:", "--r-font:", "--r-mono:",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet is missing token %q — an app reskins by overriding tokens (design doc §9)", want)
		}
	}
}

func TestStylesheetSupportsBothThemesAndTheOverride(t *testing.T) {
	css := string(Stylesheet())
	for _, want := range []string{
		"@media (prefers-color-scheme: dark)",
		`:root[data-theme="dark"]`,
		`:root[data-theme="light"]`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet is missing %q — design doc §9 requires light/dark plus a data-theme override", want)
		}
	}
}

func TestStylesheetReachesNoNetwork(t *testing.T) {
	// No @import, no webfont, no CDN: the shipped artifact is one
	// static binary and the page must render with no third party.
	css := string(Stylesheet())
	for _, bad := range []string{"@import", "http://", "https://", "url("} {
		if strings.Contains(css, bad) {
			t.Errorf("stylesheet contains %q — it must be entirely self-contained", bad)
		}
	}
}

func TestStylesheetPathIsUnderTheFrameworkPrefix(t *testing.T) {
	if StylesheetPath != "/_rastrillo/ui.css" {
		t.Errorf("StylesheetPath = %q; component markup hard-codes it, so it is pinned", StylesheetPath)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: FAIL — `Stylesheet`, `StylesheetPath` undefined.

- [ ] **Step 3: Write `assets.go`**

```go
package rastrillo

import _ "embed"

// StylesheetPath is where Serve publishes Stylesheet(). The default
// component library's own markup references it, so it is a constant and
// not an Option: a configurable path would be one the components could
// not name. The _rastrillo/ prefix keeps it clear of app routes.
const StylesheetPath = "/_rastrillo/ui.css"

//go:embed ui/base.css
var stylesheet []byte

// Stylesheet is the framework's token-based component skin (design doc
// §9): custom properties, light/dark via prefers-color-scheme with a
// data-theme override, WCAG AA, framework-neutral. An app reskins by
// overriding the --r-* properties from its own stylesheet; component
// *structure* is not meant to be forked per app.
//
// The returned slice is the embedded one — read it, never write it.
func Stylesheet() []byte { return stylesheet }
```

- [ ] **Step 4: Write `ui/base.css`**

Create the directory `ui/` and the file `ui/base.css` with exactly this content:

```css
/* rastrillo default component skin — design doc §9.

   Tokens only: an app reskins by overriding these custom properties from
   its own stylesheet. Component *structure* is not meant to be forked
   per app, only its colours and type.

   Light is the default. Dark arrives two ways and both must work: the
   viewer's OS preference, and an explicit data-theme on <html> that has
   to win in either direction. That is why the dark block is written
   twice — there is no preprocessor here to share it, and a page of
   duplicated declarations is cheaper than a build step.

   Contrast pairs are chosen for WCAG AA on body text. Changing a colour
   token means re-checking its pair. */

:root {
  --r-bg: #ffffff;
  --r-surface: #f7f8fa;
  --r-surface-2: #eef0f4;
  --r-border: #d8dce3;
  --r-text: #14171c;
  --r-muted: #5b6470;
  --r-accent: #2f5bd7;
  --r-accent-text: #ffffff;
  --r-ok: #1b6b3a;
  --r-warn: #8a5a00;
  --r-danger: #b3261e;
  --r-focus: #2f5bd7;

  --r-radius: 8px;
  --r-radius-sm: 5px;
  --r-gap: 12px;
  --r-pad: 16px;
  --r-max: 68rem;
  --r-font: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --r-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace;
}

@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --r-bg: #0f1216;
    --r-surface: #171b21;
    --r-surface-2: #1f242c;
    --r-border: #2b323b;
    --r-text: #e7eaee;
    --r-muted: #a3adba;
    --r-accent: #8fb0ff;
    --r-accent-text: #0f1216;
    --r-ok: #7fd1a0;
    --r-warn: #e0b14a;
    --r-danger: #ff9c94;
    --r-focus: #8fb0ff;
  }
}

:root[data-theme="dark"] {
  --r-bg: #0f1216;
  --r-surface: #171b21;
  --r-surface-2: #1f242c;
  --r-border: #2b323b;
  --r-text: #e7eaee;
  --r-muted: #a3adba;
  --r-accent: #8fb0ff;
  --r-accent-text: #0f1216;
  --r-ok: #7fd1a0;
  --r-warn: #e0b14a;
  --r-danger: #ff9c94;
  --r-focus: #8fb0ff;
}

*, *::before, *::after { box-sizing: border-box; }

.r-body {
  margin: 0;
  background: var(--r-bg);
  color: var(--r-text);
  font-family: var(--r-font);
  font-size: 16px;
  line-height: 1.5;
  -webkit-text-size-adjust: 100%;
}

.r-main {
  max-width: var(--r-max);
  margin: 0 auto;
  padding: calc(var(--r-pad) * 1.5) var(--r-pad) calc(var(--r-pad) * 3);
}

a { color: var(--r-accent); }

:focus-visible {
  outline: 2px solid var(--r-focus);
  outline-offset: 2px;
  border-radius: var(--r-radius-sm);
}

.r-visually-hidden {
  position: absolute;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden; clip-path: inset(50%);
  white-space: nowrap;
}

.r-skip {
  position: absolute;
  left: -9999px;
}
.r-skip:focus {
  left: var(--r-pad);
  top: var(--r-pad);
  z-index: 10;
  background: var(--r-surface);
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius-sm);
  padding: 8px 12px;
}

.r-icon {
  width: 1em;
  height: 1em;
  flex: none;
  vertical-align: -0.125em;
}

/* buttons and links that look like buttons */

.r-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: 1px solid transparent;
  border-radius: var(--r-radius-sm);
  background: var(--r-accent);
  color: var(--r-accent-text);
  font: inherit;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
}
.r-btn-quiet {
  background: transparent;
  color: var(--r-text);
  border-color: var(--r-border);
}
.r-btn-danger { background: var(--r-danger); color: #ffffff; }
.r-btn.is-disabled, .r-btn[aria-disabled="true"] {
  opacity: 0.5;
  pointer-events: none;
}

/* page-header */

.r-page-header {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--r-gap);
  margin-bottom: calc(var(--r-pad) * 1.5);
}
.r-page-title { margin: 0; font-size: 1.5rem; line-height: 1.25; }
.r-page-subtitle { margin: 4px 0 0; color: var(--r-muted); }
.r-page-header-actions { display: flex; gap: 8px; flex-wrap: wrap; }

/* box */

.r-box {
  background: var(--r-surface);
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius);
  margin-bottom: var(--r-pad);
  overflow: hidden;
}
.r-box-title {
  margin: 0;
  padding: 12px var(--r-pad);
  font-size: 0.95rem;
  border-bottom: 1px solid var(--r-border);
}
.r-box-body { padding: var(--r-pad); }
.r-box-foot {
  padding: 12px var(--r-pad);
  border-top: 1px solid var(--r-border);
  background: var(--r-surface-2);
}

/* list-bar and list-row */

.r-list-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: var(--r-gap);
}
.r-list-bar-search {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 14rem;
  padding: 0 10px;
  background: var(--r-bg);
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius-sm);
}
.r-list-bar-search input {
  flex: 1;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  padding: 8px 0;
  min-width: 0;
}
.r-list-bar-search input:focus { outline: none; }
.r-list-bar-filter select,
.r-input, .r-select, .r-textarea {
  padding: 8px 10px;
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius-sm);
  background: var(--r-bg);
  color: inherit;
  font: inherit;
  width: 100%;
}

.r-list { list-style: none; margin: 0; padding: 0; border: 1px solid var(--r-border); border-radius: var(--r-radius); overflow: hidden; }
.r-list-row {
  display: flex;
  align-items: center;
  gap: var(--r-gap);
  padding: 12px var(--r-pad);
  background: var(--r-surface);
  border-bottom: 1px solid var(--r-border);
  color: inherit;
  text-decoration: none;
}
.r-list-row:last-child { border-bottom: 0; }
.r-list-row-main { min-width: 0; flex: 1; }
.r-list-row-primary { font-weight: 600; }
.r-list-row-secondary { color: var(--r-muted); font-size: 0.9rem; }
.r-list-row-trailing { margin-left: auto; display: flex; align-items: center; gap: 8px; }

/* empty-state and pagination */

.r-empty {
  text-align: center;
  padding: calc(var(--r-pad) * 3) var(--r-pad);
  border: 1px dashed var(--r-border);
  border-radius: var(--r-radius);
  color: var(--r-muted);
}
.r-empty-icon { font-size: 2rem; color: var(--r-muted); }
.r-empty-title { margin: 8px 0 4px; font-size: 1.1rem; color: var(--r-text); }
.r-empty-body { margin: 0 auto; max-width: 32rem; }
.r-empty-action { margin-top: var(--r-pad); }

.r-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--r-gap);
  margin-top: var(--r-pad);
}
.r-pagination-count { color: var(--r-muted); font-size: 0.9rem; }

/* person, pills, badges, meters, mono */

.r-person { display: inline-flex; align-items: center; gap: 8px; text-decoration: none; color: inherit; }
.r-person-avatar {
  display: inline-flex; align-items: center; justify-content: center;
  width: 2rem; height: 2rem; border-radius: 50%;
  background: var(--r-surface-2); color: var(--r-muted);
  font-size: 0.75rem; font-weight: 700;
}
.r-person-name { font-weight: 600; }
.r-person-email { color: var(--r-muted); font-size: 0.85rem; }

.r-pill, .r-badge {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 2px 9px;
  border-radius: 999px;
  border: 1px solid var(--r-border);
  background: var(--r-surface-2);
  color: var(--r-text);
  font-size: 0.8rem;
  font-weight: 600;
  white-space: nowrap;
}
.r-badge { border-radius: var(--r-radius-sm); }
.r-tone-ok { color: var(--r-ok); border-color: currentColor; }
.r-tone-warn { color: var(--r-warn); border-color: currentColor; }
.r-tone-danger { color: var(--r-danger); border-color: currentColor; }
.r-tone-info { color: var(--r-accent); border-color: currentColor; }

.r-meter { display: flex; align-items: center; gap: 8px; }
.r-meter progress { width: 8rem; height: 8px; }
.r-meter-label { color: var(--r-muted); font-size: 0.85rem; font-variant-numeric: tabular-nums; }

.r-mono { font-family: var(--r-mono); font-size: 0.9em; background: var(--r-surface-2); padding: 1px 5px; border-radius: 4px; }

/* detail-list, alert, help-link */

.r-detail-list { margin: 0; display: grid; grid-template-columns: minmax(6rem, 12rem) 1fr; gap: 8px var(--r-gap); }
.r-detail-list dt { color: var(--r-muted); }
.r-detail-list dd { margin: 0; }

.r-alert {
  display: flex; gap: 10px;
  padding: 12px var(--r-pad);
  border: 1px solid currentColor;
  border-radius: var(--r-radius);
  margin-bottom: var(--r-pad);
}
.r-alert-body { color: var(--r-text); }
.r-alert-title { font-weight: 700; }

.r-help-link { display: inline-flex; align-items: center; gap: 4px; color: var(--r-muted); font-size: 0.85rem; }

/* field family, toggle-block, seg-tabs, form-foot */

.r-field { display: block; margin-bottom: var(--r-pad); }
.r-field-label { display: block; font-weight: 600; margin-bottom: 4px; }
.r-field-required { color: var(--r-danger); }
.r-field-hint { display: block; color: var(--r-muted); font-size: 0.85rem; margin-top: 4px; }
.r-field-error { display: block; color: var(--r-danger); font-size: 0.85rem; margin-top: 4px; }
.r-textarea { min-height: 6rem; }

.r-toggle-block {
  display: flex; gap: 10px; align-items: flex-start;
  padding: 12px var(--r-pad);
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius);
  margin-bottom: var(--r-gap);
}

.r-seg-tabs { display: inline-flex; border: 1px solid var(--r-border); border-radius: var(--r-radius-sm); overflow: hidden; margin-bottom: var(--r-pad); }
.r-seg-tab { padding: 7px 14px; text-decoration: none; color: var(--r-text); border-right: 1px solid var(--r-border); }
.r-seg-tab:last-child { border-right: 0; }
.r-seg-tab.is-current { background: var(--r-accent); color: var(--r-accent-text); font-weight: 600; }

.r-form-foot { display: flex; gap: 8px; align-items: center; margin-top: var(--r-pad); }

/* menu, confirm, modal-route, bulk-select */

.r-menu { position: relative; display: inline-block; }
.r-menu > summary { list-style: none; cursor: pointer; }
.r-menu > summary::-webkit-details-marker { display: none; }
.r-menu-items {
  position: absolute; right: 0; z-index: 5;
  min-width: 12rem; margin: 4px 0 0; padding: 4px;
  list-style: none;
  background: var(--r-surface);
  border: 1px solid var(--r-border);
  border-radius: var(--r-radius);
}
.r-menu-items a { display: block; padding: 7px 10px; border-radius: var(--r-radius-sm); text-decoration: none; color: var(--r-text); }
.r-menu-items a.is-danger { color: var(--r-danger); }

.r-confirm { max-width: 34rem; margin: 0 auto; }
.r-confirm-body { color: var(--r-muted); }

.r-modal { max-width: 34rem; margin: 0 auto; }
.r-modal-head { display: flex; align-items: center; justify-content: space-between; gap: var(--r-gap); }
.r-modal-close { color: var(--r-muted); text-decoration: none; }

.r-bulk-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-top: var(--r-gap); }
.r-bulk-row { display: flex; align-items: center; gap: 10px; padding: 8px var(--r-pad); border-bottom: 1px solid var(--r-border); }
.r-bulk-row:last-child { border-bottom: 0; }

/* locale switch — a plain list of links, no picker widget (design doc §10) */

.r-locale-switch { display: flex; gap: 8px; justify-content: center; padding: var(--r-pad); color: var(--r-muted); font-size: 0.85rem; }
.r-locale-switch-item { text-transform: uppercase; letter-spacing: 0.04em; }
.r-locale-switch-item.is-current { color: var(--r-text); font-weight: 700; text-decoration: none; }
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1 -run Stylesheet`
Expected: PASS — 4 tests.

- [ ] **Step 6: gofmt + full sweep**

Run: `gofmt -l assets.go assets_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add assets.go assets_test.go ui/base.css
git commit -m "ui: token-based component skin, light/dark with a data-theme override (design doc §9)"
```

---

### Task 6: `ui.go` + `ui/layout.html` + `ui/locales/en.toml` — the UI runtime and base catalog

**Files:**
- Create: `ui.go`
- Create: `ui/layout.html`
- Create: `ui/locales/en.toml`
- Test: `ui_test.go`

**Interfaces:**
- Consumes: `catalog.Decode` (Task 1); `Catalog`, `NewLocales`, `(*Locales).Codes/Default/T/Tf` (Task 2); `(*Locales).Middleware`, `LocaleFrom` (Task 3); `icons.SVG` (Task 4); `StylesheetPath` (Task 5).
- Produces (Tasks 7–10, 13, 15 use these exactly):
  - `type UIOptions struct { Locales []string; DefaultLocale string; LocaleFS fs.FS; TemplateFS fs.FS }`
  - `func NewUI(o UIOptions) (*UI, error)`
  - `func (u *UI) Middleware(next http.Handler) http.Handler`
  - `func (u *UI) Render(w http.ResponseWriter, r *http.Request, name string, data any) error`
  - `func (u *UI) Locales() *Locales`
  - `func Render(w http.ResponseWriter, r *http.Request, name string, data any) error`
  - template funcs available to every component: `T`, `Tf`, `locale`, `locales`, `icon`, `dict`, `css`
  - layout partials `doc-open`, `doc-close`, `locale-switch`
  - the test helper `renderComponent(t *testing.T, locale, name string, data map[string]any) string`, reused by Tasks 8–10

**Decisions recorded (design doc silent):**
- **Layout is `doc-open` / `doc-close`, not a single wrapping `page` template.** One shared template set means two pages both defining `content` would collide; splitting the document shell into an opening and a closing partial sidesteps that entirely and keeps every page a plain top-level `{{define}}`.
- **App templates are parsed *after* the framework's**, because `html/template` lets a later `{{define}}` of the same name replace an earlier one. That is override-by-existence for markup (§9), for free, with no registry.
- **Component parameters are a `dict`**, because `html/template` has no map literal and a fixed struct per component would be a second place to edit every time a component grows a field.
- **`Render` buffers.** A template error halfway through must not leave a half-written page on the wire behind a committed 200.

- [ ] **Step 1: Write `ui/locales/en.toml`** — the framework's base English catalog

Every string any built-in component renders lives here and nowhere else (§9: copy "comes from the localization catalog, not a baked-in string"). Create `ui/locales/en.toml`:

```toml
# rastrillo's base component catalog (design doc §9, §10). Every string
# the built-in vocabulary renders is here, so a single-locale app gets
# correctly-worded components without writing a catalog of its own. An
# app overrides any of these by defining the same key in its own
# locales/<code>.toml.

ui.apply = "Apply"
ui.bulk.apply = "Apply to selected"
ui.cancel = "Cancel"
ui.close = "Close"
ui.confirm = "Confirm"
ui.confirm.body = "This cannot be undone."
ui.confirm.title = "Are you sure?"
ui.delete = "Delete"
ui.details = "Details"
ui.empty.body = "When there is something to show, it will appear here."
ui.empty.title = "Nothing here yet"
ui.help = "Help"
ui.language = "Language"
ui.menu = "Menu"
ui.more = "More actions"
ui.next = "Next"
ui.optional = "Optional"
ui.pagination.count = "Page {page} of {pages}"
ui.pagination.label = "Pagination"
ui.previous = "Previous"
ui.required = "Required"
ui.save = "Save"
ui.search = "Search"
ui.search.placeholder = "Search…"
ui.select.all = "Select all"
ui.selected = "{count} selected"
ui.sign.in = "Sign in"
ui.sign.out = "Sign out"
ui.skip.to.content = "Skip to content"
ui.status.label = "Status"
ui.tone.danger = "Error"
ui.tone.info = "Information"
ui.tone.ok = "Success"
ui.tone.warn = "Warning"
```

- [ ] **Step 2: Write `ui/layout.html`**

```html
{{/* The document shell, split in two on purpose: one shared template
     set means every page is a top-level {{define}}, and a single
     wrapping template would force them all to define the same inner
     block name and collide. A page reads:

       {{define "orders"}}{{template "doc-open" .}} … {{template "doc-close" .}}{{end}}

     Data is a dict; "title" and (for the locale switch) "path" are the
     only keys the shell reads. */}}

{{define "doc-open"}}<!doctype html>
<html lang="{{locale}}"{{with .theme}} data-theme="{{.}}"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{with .title}}{{.}}{{end}}</title>
<link rel="stylesheet" href="{{css}}">
</head>
<body class="r-body">
<a class="r-skip" href="#r-content">{{T "ui.skip.to.content"}}</a>
<main id="r-content" class="r-main">
{{end}}

{{define "doc-close"}}
</main>
{{template "locale-switch" .}}
</body>
</html>
{{end}}

{{/* Zero-JS locale switching (design doc §10): plain links to the same
     path under a different locale prefix. No picker widget, no script.
     Renders nothing at all for a single-locale app. */}}
{{define "locale-switch"}}{{if gt (len locales) 1}}{{$path := ""}}{{with .path}}{{$path = printf "%v" .}}{{end}}<nav class="r-locale-switch" aria-label="{{T "ui.language"}}">
{{range locales}}<a class="r-locale-switch-item{{if eq . locale}} is-current{{end}}" href="/{{.}}{{$path}}" hreflang="{{.}}" lang="{{.}}">{{.}}</a>
{{end}}</nav>{{end}}{{end}}
```

- [ ] **Step 3: Write the failing test**

Create `ui_test.go`:

```go
package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// renderComponent executes one component partial against a
// single-locale UI. Tasks 8-10's component tests all use it.
func renderComponent(t *testing.T, locale, name string, data map[string]any) string {
	t.Helper()
	ui, err := NewUI(UIOptions{Locales: []string{locale}, DefaultLocale: locale})
	if err != nil {
		t.Fatal(err)
	}
	set := ui.sets[locale]
	if set == nil {
		t.Fatalf("no template set for %q", locale)
	}
	var b strings.Builder
	if err := set.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("execute %q: %v", name, err)
	}
	return b.String()
}

func TestNewUIBuildsOneSetPerLocale(t *testing.T) {
	ui, err := NewUI(UIOptions{Locales: []string{"en", "fr"}, DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ui.sets) != 2 {
		t.Fatalf("got %d template sets, want 2", len(ui.sets))
	}
	if ui.Locales().Default() != "en" {
		t.Errorf("Default = %q, want en", ui.Locales().Default())
	}
}

func TestBaseCatalogIsLoaded(t *testing.T) {
	ui, err := NewUI(UIOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ key, want string }{
		{"ui.save", "Save"},
		{"ui.cancel", "Cancel"},
		{"ui.empty.title", "Nothing here yet"},
		{"ui.search.placeholder", "Search…"},
	}
	for _, tt := range tests {
		if got := ui.Locales().T("en", tt.key); got != tt.want {
			t.Errorf("T(en, %q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestDocShellRendersTitleLangAndStylesheet(t *testing.T) {
	got := renderComponent(t, "en", "doc-open", map[string]any{"title": "Orders"})
	for _, want := range []string{
		"<!doctype html>", `<html lang="en"`, "<title>Orders</title>",
		`href="/_rastrillo/ui.css"`, `id="r-content"`, "Skip to content",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doc-open missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script") {
		t.Error("doc-open emitted a <script> tag — the zero-JS baseline is a hard rule (design doc §9)")
	}
}

func TestDocShellHonoursAnExplicitTheme(t *testing.T) {
	got := renderComponent(t, "en", "doc-open", map[string]any{"title": "x", "theme": "dark"})
	if !strings.Contains(got, `data-theme="dark"`) {
		t.Errorf("doc-open missing the data-theme override:\n%s", got)
	}
}

func TestLocaleSwitchIsSilentForASingleLocaleApp(t *testing.T) {
	got := renderComponent(t, "en", "locale-switch", map[string]any{"path": "/orders"})
	if strings.TrimSpace(got) != "" {
		t.Errorf("locale-switch rendered %q for a single-locale app, want nothing", got)
	}
}

func TestLocaleSwitchLinksEveryLocaleWithNoScript(t *testing.T) {
	ui, err := NewUI(UIOptions{Locales: []string{"en", "fr"}, DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := ui.sets["fr"].ExecuteTemplate(&b, "locale-switch", map[string]any{"path": "/orders"}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`href="/en/orders"`, `href="/fr/orders"`, `hreflang="fr"`, "is-current"} {
		if !strings.Contains(got, want) {
			t.Errorf("locale-switch missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script") || strings.Contains(got, "onclick") {
		t.Error("locale switching must be plain links (design doc §10)")
	}
}

func TestAppTemplatesOverrideTheBuiltInsByExistence(t *testing.T) {
	app := fstest.MapFS{
		"templates/doc-open.html": {Data: []byte(`{{define "doc-open"}}EJECTED{{end}}`)},
	}
	ui, err := NewUI(UIOptions{Locales: []string{"en"}, DefaultLocale: "en", TemplateFS: app})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := ui.sets["en"].ExecuteTemplate(&b, "doc-open", nil); err != nil {
		t.Fatal(err)
	}
	if b.String() != "EJECTED" {
		t.Errorf("app template did not win: %q (design doc §9: override-by-existence)", b.String())
	}
}

func TestDictBuildsComponentArguments(t *testing.T) {
	m, err := dict("title", "Details", "tone", "ok")
	if err != nil {
		t.Fatal(err)
	}
	if m["title"] != "Details" || m["tone"] != "ok" {
		t.Errorf("dict = %v", m)
	}
	if _, err := dict("odd"); err == nil {
		t.Error("dict with an odd argument count must fail loudly")
	}
	if _, err := dict(1, "x"); err == nil {
		t.Error("dict with a non-string key must fail loudly")
	}
}

func TestRenderUsesTheRequestsLocale(t *testing.T) {
	app := fstest.MapFS{
		"locales/fr.toml":     {Data: []byte("page.title = \"Commandes\"\n")},
		"templates/page.html": {Data: []byte(`{{define "page"}}{{T "page.title"}}{{end}}`)},
	}
	ui, err := NewUI(UIOptions{Locales: []string{"en", "fr"}, DefaultLocale: "en", LocaleFS: app, TemplateFS: app})
	if err != nil {
		t.Fatal(err)
	}
	h := ui.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Render(w, r, "page", nil); err != nil {
			t.Errorf("Render: %v", err)
		}
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/fr/anything", nil))
	if got := rec.Body.String(); got != "Commandes" {
		t.Errorf("body = %q, want Commandes", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/anything", nil))
	if got := rec.Body.String(); got != "page.title" {
		t.Errorf("body = %q, want the key verbatim (no en catalog defines it)", got)
	}
}

func TestRenderWithoutAUIOnTheRequest(t *testing.T) {
	err := Render(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "doc-open", nil)
	if err == nil {
		t.Fatal("want an error when the request never went through Serve/Middleware")
	}
	if !strings.Contains(err.Error(), "rastrillo.Serve") {
		t.Errorf("error should say how to fix it: %v", err)
	}
}

func TestRenderDoesNotWriteAPartialPageOnError(t *testing.T) {
	app := fstest.MapFS{
		"templates/boom.html": {Data: []byte(`{{define "boom"}}before{{.Missing.Field}}after{{end}}`)},
	}
	ui, err := NewUI(UIOptions{Locales: []string{"en"}, DefaultLocale: "en", TemplateFS: app})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	var renderErr error
	ui.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderErr = ui.Render(w, r, "boom", struct{}{})
	})).ServeHTTP(rec, req)
	if renderErr == nil {
		t.Fatal("want a render error")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("half a page reached the wire: %q", rec.Body.String())
	}
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: FAIL — `NewUI`, `UIOptions`, `dict`, `Render` undefined.

- [ ] **Step 5: Write `ui.go`**

```go
package rastrillo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
	"amadan.net/rastrillo/rastrillo/internal/icons"
)

//go:embed ui
var uiFS embed.FS

//go:embed ui/locales/en.toml
var baseCatalogTOML []byte

type uiCtxKey struct{}

// UI is the app's rendering and localization runtime: the framework's
// default component library (design doc §9), the app's own templates
// parsed on top of it, and one html/template set per declared locale,
// each bound via Funcs to that locale's T/Tf. That per-locale clone is
// titogo's proven shape (§10's templatesFor(code)), shipped as a
// library instead of grown in place.
type UI struct {
	locales *Locales
	sets    map[string]*template.Template
}

// UIOptions configures NewUI. Every field is optional: the zero value
// builds a single-locale ("en") UI on the framework's own components and
// base catalog, which is exactly what an app that declares nothing gets.
type UIOptions struct {
	// Locales and DefaultLocale are design doc §10's own declaration:
	// Locales: []string{"en","fr","de"}, DefaultLocale: "en".
	Locales       []string
	DefaultLocale string

	// LocaleFS supplies the app's catalogs at locales/<code>.toml and
	// TemplateFS its templates at templates/*.html — normally an
	// embed.FS, because the shipped artifact is one static binary
	// (design doc §11). The directory names are fixed, not options:
	// §10 writes locales/en.toml, and one layout is the point.
	LocaleFS   fs.FS
	TemplateFS fs.FS
}

// NewUI parses the component library once per declared locale.
func NewUI(o UIOptions) (*UI, error) {
	base, err := catalog.Decode(baseCatalogTOML)
	if err != nil {
		return nil, fmt.Errorf("rastrillo: base catalog: %w", err)
	}
	locales, err := NewLocales(o.Locales, o.DefaultLocale, Catalog(base), o.LocaleFS)
	if err != nil {
		return nil, err
	}

	u := &UI{locales: locales, sets: map[string]*template.Template{}}
	for _, code := range locales.Codes() {
		t, err := template.New("rastrillo").Funcs(u.funcs(code)).ParseFS(uiFS, "ui/*.html")
		if err != nil {
			return nil, fmt.Errorf("rastrillo: parse component library: %w", err)
		}
		// App templates are parsed second on purpose: html/template lets
		// a later {{define}} of the same name replace an earlier one, so
		// a hand-written templates/box.html silently wins over the
		// built-in "box". That is override-by-existence for markup
		// (design doc §9) — the same one mechanism actions already use.
		if o.TemplateFS != nil {
			for _, pat := range []string{"templates/*.html", "templates/*/*.html"} {
				m, err := fs.Glob(o.TemplateFS, pat)
				if err != nil {
					return nil, fmt.Errorf("rastrillo: glob %s: %w", pat, err)
				}
				if len(m) == 0 {
					continue
				}
				if t, err = t.ParseFS(o.TemplateFS, pat); err != nil {
					return nil, fmt.Errorf("rastrillo: parse app templates: %w", err)
				}
			}
		}
		u.sets[code] = t
	}
	return u, nil
}

// Locales exposes the resolved locale set, for an app that needs to list
// or validate it.
func (u *UI) Locales() *Locales { return u.locales }

// funcs is the vocabulary every component template may use. It is
// deliberately tiny: anything bigger belongs in the action, not the
// template.
func (u *UI) funcs(code string) template.FuncMap {
	l := u.locales
	return template.FuncMap{
		"T":       func(key string) string { return l.T(code, key) },
		"Tf":      func(key string, args ...any) string { return l.Tf(code, key, args...) },
		"locale":  func() string { return code },
		"locales": func() []string { return l.Codes() },
		"icon":    func(name string) template.HTML { return icons.SVG(name) },
		"dict":    dict,
		"css":     func() string { return StylesheetPath },
	}
}

// dict builds the argument map a component partial takes:
//
//	{{template "box" (dict "title" (T "ui.details") "body" $body)}}
//
// html/template has no map literal, and a fixed struct per component
// would be a second place to edit every time a component grows a field.
func dict(args ...any) (map[string]any, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("dict: odd argument count %d", len(args))
	}
	m := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i, args[i])
		}
		m[k] = args[i+1]
	}
	return m, nil
}

// Middleware resolves the locale (see (*Locales).Middleware) and puts
// this UI on the request context, so an action can call the
// package-level Render/T/Tf without threading anything through.
func (u *UI) Middleware(next http.Handler) http.Handler {
	return u.locales.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), uiCtxKey{}, u)))
	}))
}

// Render executes name against the request's resolved locale set.
//
// It buffers: a template error halfway through must not leave half a
// page on the wire behind a status the caller can no longer change.
func (u *UI) Render(w http.ResponseWriter, r *http.Request, name string, data any) error {
	t, ok := u.sets[LocaleFrom(r)]
	if !ok {
		t = u.sets[u.locales.Default()]
	}
	var buf strings.Builder
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("rastrillo: render %q: %w", name, err)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, err := io.WriteString(w, buf.String())
	return err
}

// Render renders name for the request's UI — the call an action makes.
func Render(w http.ResponseWriter, r *http.Request, name string, data any) error {
	u, ok := r.Context().Value(uiCtxKey{}).(*UI)
	if !ok {
		return errors.New("rastrillo: no UI on this request — is it served through rastrillo.Serve?")
	}
	return u.Render(w, r, name, data)
}
```

The import block above is complete as written: `io` is there for `io.WriteString`, `strings` for the render buffer. Keep the gofmt grouping — stdlib first, this module second.

- [ ] **Step 6: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: PASS — every test in `ui_test.go`, `locale_test.go` and `localemw_test.go`.

- [ ] **Step 7: gofmt + full sweep**

Run: `gofmt -l ui.go ui_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add ui.go ui_test.go ui/layout.html ui/locales/en.toml
git commit -m "ui: per-locale template sets, base English catalog, document shell (design doc §9, §10)"
```

---

### Task 7: `serve.go` + `ctx.go` — wire the UI into `Serve`

**Files:**
- Modify: `serve.go` (the `Options` struct and the mux-building block inside `Serve`)
- Modify: `ctx.go` (the `Locale` field comment only)
- Test: `serve_ui_test.go`

**Interfaces:**
- Consumes from Task 6: `NewUI`, `UIOptions`, `(*UI).Middleware`; from Task 5: `Stylesheet()`, `StylesheetPath`.
- Produces (Tasks 13, 15 use these exactly): four new optional `Options` fields — `Locales []string`, `DefaultLocale string`, `LocaleFS fs.FS`, `TemplateFS fs.FS`.

**Additive:** an app that sets none of these behaves exactly as it does today, on the framework's base English catalog and built-in components.

- [ ] **Step 1: Write the failing test**

Create `serve_ui_test.go`:

```go
package rastrillo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// serveHandler builds exactly the handler tree Serve builds, without
// binding a listener — the mux assembly is what these tests are about.
func TestServeMuxServesTheStylesheet(t *testing.T) {
	h, err := buildHandler(Options{Mux: http.NewServeMux()})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", StylesheetPath, nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "--r-accent:") {
		t.Error("stylesheet body is not the token skin")
	}
}

func TestServeMuxStillAnswersHealthzAndVersion(t *testing.T) {
	h, err := buildHandler(Options{Mux: http.NewServeMux()})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/healthz": "ok", "/api/version": BuildVersion} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Body.String() != want {
			t.Errorf("GET %s = %q, want %q", path, rec.Body.String(), want)
		}
	}
}

func TestServeStripsTheLocalePrefixBeforeTheAppMux(t *testing.T) {
	app := http.NewServeMux()
	app.HandleFunc("GET /orders", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(LocaleFrom(r)))
	})
	h, err := buildHandler(Options{Mux: app, Locales: []string{"en", "fr"}, DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/fr/orders", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 — the locale prefix must not change which route matches", rec.Code)
	}
	if rec.Body.String() != "fr" {
		t.Errorf("locale = %q, want fr", rec.Body.String())
	}
}

func TestServeRejectsAnUndeclaredDefaultLocale(t *testing.T) {
	_, err := buildHandler(Options{Mux: http.NewServeMux(), Locales: []string{"fr"}, DefaultLocale: "en"})
	if err == nil {
		t.Fatal("want an error, got nil")
	}
}

func TestServeWiresTheAppsCatalogsAndTemplates(t *testing.T) {
	assets := fstest.MapFS{
		"locales/fr.toml":     {Data: []byte("hello = \"Bonjour\"\n")},
		"templates/hi.html":   {Data: []byte(`{{define "hi"}}{{T "hello"}}{{end}}`)},
	}
	app := http.NewServeMux()
	app.HandleFunc("GET /hi", func(w http.ResponseWriter, r *http.Request) {
		if err := Render(w, r, "hi", nil); err != nil {
			t.Errorf("Render: %v", err)
		}
	})
	h, err := buildHandler(Options{
		Mux: app, Locales: []string{"en", "fr"}, DefaultLocale: "en",
		LocaleFS: assets, TemplateFS: assets,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/fr/hi", nil))
	if rec.Body.String() != "Bonjour" {
		t.Errorf("body = %q, want Bonjour", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: FAIL — `buildHandler` undefined, `Options` has no field `Locales`.

- [ ] **Step 3: Add the `Options` fields**

In `serve.go`, immediately after the `Migrations []string` field and before `Socket`, insert:

```go
	// Locales are the locale codes this app declares and DefaultLocale
	// the one a missing key falls back to — design doc §10's own
	// declaration, verbatim:
	//
	//	Locales: []string{"en", "fr", "de"}, DefaultLocale: "en"
	//
	// Both optional: an app that declares neither is single-locale
	// ("en") on the framework's base English component copy, and
	// behaves exactly as it did before localization landed.
	Locales       []string
	DefaultLocale string

	// LocaleFS supplies this app's catalogs at locales/<code>.toml and
	// TemplateFS its own component overrides at templates/*.html. Both
	// are normally an embed.FS: the shipped artifact is one static
	// binary (design doc §11), so nothing is read from disk at boot.
	// The directory names are fixed rather than options — §10 writes
	// locales/en.toml, and one layout is the point.
	LocaleFS   fs.FS
	TemplateFS fs.FS
```

Add `"io/fs"` to `serve.go`'s import block.

- [ ] **Step 4: Extract `buildHandler` and use it in `Serve`**

In `Serve`, replace this block —

```go
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, BuildVersion)
	})
	mux.Handle("/", opts.Mux)
```

— with:

```go
	handler, err := buildHandler(opts)
	if err != nil {
		return err
	}
```

Then change `srv := &http.Server{Handler: mux}` to `srv := &http.Server{Handler: handler}`.

The line below it, `ln, err := listen(opts.Socket, opts.Addr)`, needs **no** change: `:=` is legal when at least one variable on the left is new, and `ln` is.

Add `buildHandler` at the end of `serve.go`:

```go
// buildHandler assembles the handler tree Serve binds to its listener:
// the framework's own always-answered routes, the component
// stylesheet, and the app's mux behind the UI middleware. Split out of
// Serve so the assembly is testable without binding a socket.
func buildHandler(opts Options) (http.Handler, error) {
	ui, err := NewUI(UIOptions{
		Locales:       opts.Locales,
		DefaultLocale: opts.DefaultLocale,
		LocaleFS:      opts.LocaleFS,
		TemplateFS:    opts.TemplateFS,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, BuildVersion)
	})
	// The component library's own markup references this path, so it is
	// served here rather than left for every app to remember.
	mux.HandleFunc("GET "+StylesheetPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(Stylesheet())
	})
	// Locale resolution wraps only the app's routes: /healthz and
	// /api/version are the platform's contract and must never move
	// under a locale prefix.
	mux.Handle("/", ui.Middleware(opts.Mux))
	return mux, nil
}
```

- [ ] **Step 5: Update `ctx.go`'s `Locale` comment**

Replace:

```go
	// Locale is the resolved locale for this request (design doc §10).
	// Empty until the localization system lands; defaults are the app's
	// problem for now.
	Locale string
```

with:

```go
	// Locale is the resolved locale for this request (design doc §10).
	// The framework resolves it onto the request context; an app's
	// ctxFactory copies it here with rastrillo.LocaleFrom(r). It is
	// per-request state, so a ctxFactory that returns one shared *Ctx
	// must not set it.
	Locale string
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1`
Expected: PASS.

- [ ] **Step 7: gofmt + full sweep**

Run: `gofmt -l serve.go ctx.go serve_ui_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add serve.go ctx.go serve_ui_test.go
git commit -m "serve: install the UI middleware and publish the component stylesheet (design doc §9, §10)"
```

---

## Component authoring rule (applies to Tasks 8, 9 and 10)

**Every dict key is read through `{{with}}`, `{{if}}` or `{{range}}`, never printed bare.** `html/template` renders a missing `map[string]any` key as the literal text `<no value>`; a component that assumes a caller passed something will put that string in a production page. Where a numeric default is needed, assign into a variable first (`{{$v := 0}}{{if .value}}{{$v = .value}}{{end}}`), because `or` treats 0 as absent.

**Every visible string is a catalog lookup** (`{{T "ui.save"}}` / `{{Tf "ui.pagination.count" ...}}`), never a literal, and every key used must already exist in `ui/locales/en.toml` from Task 6. If a component needs a string that catalog does not have, add the key to `ui/locales/en.toml` in the same task and say so in the commit message.

**No `<script>`, no inline event handler, no `style=` attribute.** Interactivity is native HTML only: `<details>` for menus, `<progress>` for meters, GET forms for search and filter, a separate route for a modal or a confirm.

---

### Task 8: `ui/structure.html` — page-header, box, list-bar, list-row, empty-state, pagination

**Files:**
- Create: `ui/structure.html`
- Test: `structure_test.go`

**Interfaces:**
- Consumes: the template funcs and `renderComponent` helper from Task 6.
- Produces: `{{define}}` names `page-header`, `box`, `list-bar`, `list`, `list-row`, `empty-state`, `pagination`, callable from any app template. (`list` is the `<ul>` wrapper `list-row` items sit in; it is not in §9's named list but a row needs somewhere to live, and giving it a name is smaller than making every caller hand-write the `<ul>`.)

- [ ] **Step 1: Write `ui/structure.html`**

```html
{{/* Structural components — design doc §9.

     page-header   title, subtitle?, actions? (HTML)
     box           title?, body (HTML or string), foot? (HTML)
     list-bar      action?, q?, searchable?, filters? ([]dict: name,label,options[]{value,label,selected})
     list          body (HTML: a run of list-row)
     list-row      href?, primary, secondary?, trailing? (HTML)
     empty-state   icon?, title?, body?, action? (HTML)
     pagination    prev? (href), next? (href), page, pages

     Search, filter and paging are GET round-trips; nothing here needs
     JavaScript to work. */}}

{{define "page-header"}}<header class="r-page-header">
  <div>
    <h1 class="r-page-title">{{with .title}}{{.}}{{end}}</h1>
    {{with .subtitle}}<p class="r-page-subtitle">{{.}}</p>{{end}}
  </div>
  {{with .actions}}<div class="r-page-header-actions">{{.}}</div>{{end}}
</header>{{end}}

{{define "box"}}<section class="r-box">
  {{with .title}}<h2 class="r-box-title">{{.}}</h2>{{end}}
  <div class="r-box-body">{{with .body}}{{.}}{{end}}</div>
  {{with .foot}}<div class="r-box-foot">{{.}}</div>{{end}}
</section>{{end}}

{{define "list-bar"}}<form class="r-list-bar" method="get" action="{{with .action}}{{.}}{{end}}">
  {{if .searchable}}<label class="r-list-bar-search">
    <span class="r-visually-hidden">{{T "ui.search"}}</span>
    {{icon "search"}}
    <input type="search" name="q" value="{{with .q}}{{.}}{{end}}" placeholder="{{T "ui.search.placeholder"}}">
  </label>{{end}}
  {{range .filters}}<label class="r-list-bar-filter">
    <span class="r-visually-hidden">{{with .label}}{{.}}{{end}}</span>
    <select name="{{with .name}}{{.}}{{end}}">{{range .options}}<option value="{{with .value}}{{.}}{{end}}"{{if .selected}} selected{{end}}>{{with .label}}{{.}}{{end}}</option>{{end}}</select>
  </label>{{end}}
  <button class="r-btn r-btn-quiet" type="submit">{{T "ui.apply"}}</button>
</form>{{end}}

{{define "list"}}<ul class="r-list">{{with .body}}{{.}}{{end}}</ul>{{end}}

{{define "list-row"}}<li>{{if .href}}<a class="r-list-row" href="{{.href}}">{{else}}<div class="r-list-row">{{end}}
  <div class="r-list-row-main">
    <div class="r-list-row-primary">{{with .primary}}{{.}}{{end}}</div>
    {{with .secondary}}<div class="r-list-row-secondary">{{.}}</div>{{end}}
  </div>
  {{with .trailing}}<div class="r-list-row-trailing">{{.}}</div>{{end}}
{{if .href}}</a>{{else}}</div>{{end}}</li>{{end}}

{{define "empty-state"}}<div class="r-empty">
  {{with .icon}}<div class="r-empty-icon">{{icon .}}</div>{{end}}
  <h2 class="r-empty-title">{{if .title}}{{.title}}{{else}}{{T "ui.empty.title"}}{{end}}</h2>
  <p class="r-empty-body">{{if .body}}{{.body}}{{else}}{{T "ui.empty.body"}}{{end}}</p>
  {{with .action}}<div class="r-empty-action">{{.}}</div>{{end}}
</div>{{end}}

{{define "pagination"}}{{$page := 1}}{{if .page}}{{$page = .page}}{{end}}{{$pages := 1}}{{if .pages}}{{$pages = .pages}}{{end}}<nav class="r-pagination" aria-label="{{T "ui.pagination.label"}}">
  {{if .prev}}<a class="r-btn r-btn-quiet" href="{{.prev}}" rel="prev">{{icon "chevron-left"}}{{T "ui.previous"}}</a>{{else}}<span class="r-btn r-btn-quiet is-disabled" aria-disabled="true">{{icon "chevron-left"}}{{T "ui.previous"}}</span>{{end}}
  <span class="r-pagination-count">{{Tf "ui.pagination.count" "page" $page "pages" $pages}}</span>
  {{if .next}}<a class="r-btn r-btn-quiet" href="{{.next}}" rel="next">{{T "ui.next"}}{{icon "chevron-right"}}</a>{{else}}<span class="r-btn r-btn-quiet is-disabled" aria-disabled="true">{{T "ui.next"}}{{icon "chevron-right"}}</span>{{end}}
</nav>{{end}}
```

- [ ] **Step 2: Write the test**

Create `structure_test.go`:

```go
package rastrillo

import (
	"html/template"
	"strings"
	"testing"
)

func TestPageHeader(t *testing.T) {
	got := renderComponent(t, "en", "page-header", map[string]any{
		"title":    "Ticket types",
		"subtitle": "Six on sale",
		"actions":  template.HTML(`<a class="r-btn" href="/new">New</a>`),
	})
	for _, want := range []string{
		`<h1 class="r-page-title">Ticket types</h1>`,
		`<p class="r-page-subtitle">Six on sale</p>`,
		`<a class="r-btn" href="/new">New</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestOptionalKeysNeverLeakNoValue(t *testing.T) {
	// html/template prints a missing map key as the literal "<no value>".
	// Every component must guard its optional keys.
	tests := []struct {
		name string
		data map[string]any
	}{
		{"page-header", map[string]any{"title": "T"}},
		{"box", map[string]any{}},
		{"list-bar", map[string]any{}},
		{"list", map[string]any{}},
		{"list-row", map[string]any{"primary": "P"}},
		{"empty-state", map[string]any{}},
		{"pagination", map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderComponent(t, "en", tt.name, tt.data)
			if strings.Contains(got, "no value") {
				t.Errorf("%s leaked a missing key:\n%s", tt.name, got)
			}
		})
	}
}

func TestBox(t *testing.T) {
	got := renderComponent(t, "en", "box", map[string]any{
		"title": "Details", "body": "Plain body", "foot": template.HTML(`<button>Save</button>`),
	})
	for _, want := range []string{`class="r-box"`, `<h2 class="r-box-title">Details</h2>`, "Plain body", "<button>Save</button>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestListBarIsAZeroJSGetForm(t *testing.T) {
	got := renderComponent(t, "en", "list-bar", map[string]any{
		"action": "/orders", "q": "ada", "searchable": true,
		"filters": []any{map[string]any{
			"name": "status", "label": "Status",
			"options": []any{
				map[string]any{"value": "open", "label": "Open", "selected": true},
				map[string]any{"value": "done", "label": "Done"},
			},
		}},
	})
	for _, want := range []string{
		`method="get"`, `action="/orders"`, `name="q"`, `value="ada"`,
		`placeholder="Search…"`, `<select name="status">`,
		`<option value="open" selected>Open</option>`,
		`<option value="done">Done</option>`,
		`type="submit"`, ">Apply<",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script") || strings.Contains(got, "onchange") {
		t.Error("search/filter must be a GET round-trip, not script (design doc §9)")
	}
}

func TestListRowLinksWhenGivenAnHref(t *testing.T) {
	linked := renderComponent(t, "en", "list-row", map[string]any{
		"href": "/orders/7", "primary": "Order 7", "secondary": "ada@example.com",
	})
	if !strings.Contains(linked, `<a class="r-list-row" href="/orders/7">`) {
		t.Errorf("expected a linked row:\n%s", linked)
	}
	plain := renderComponent(t, "en", "list-row", map[string]any{"primary": "Order 7"})
	if strings.Contains(plain, "<a ") {
		t.Errorf("a row with no href must not be a link:\n%s", plain)
	}
	if !strings.Contains(plain, `<div class="r-list-row">`) {
		t.Errorf("expected a plain row:\n%s", plain)
	}
}

func TestEmptyStateTeachesFromTheCatalogByDefault(t *testing.T) {
	got := renderComponent(t, "en", "empty-state", map[string]any{"icon": "search"})
	for _, want := range []string{"Nothing here yet", "When there is something to show", "<svg "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	custom := renderComponent(t, "en", "empty-state", map[string]any{"title": "No tickets"})
	if !strings.Contains(custom, "No tickets") || strings.Contains(custom, "Nothing here yet") {
		t.Errorf("a caller-supplied title must replace the default:\n%s", custom)
	}
}

func TestPagination(t *testing.T) {
	got := renderComponent(t, "en", "pagination", map[string]any{
		"prev": "/orders?page=1", "page": 2, "pages": 5,
	})
	for _, want := range []string{`href="/orders?page=1"`, `rel="prev"`, "Page 2 of 5", `aria-disabled="true"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	first := renderComponent(t, "en", "pagination", map[string]any{"next": "/orders?page=2"})
	if !strings.Contains(first, "Page 1 of 1") {
		t.Errorf("page/pages must default to 1, not blank:\n%s", first)
	}
}
```

- [ ] **Step 3: Run the test**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1 -run 'PageHeader|Box|ListBar|ListRow|EmptyState|Pagination|OptionalKeys'`
Expected: PASS. If a component was mistyped the failure names it and prints the rendered output.

- [ ] **Step 4: gofmt + full sweep**

Run: `gofmt -l structure_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add ui/structure.html structure_test.go
git commit -m "ui: structural components — header, box, list bar/row, empty state, pagination (design doc §9)"
```

---

### Task 9: `ui/display.html` — person, status-pill, badge, meter, mono, detail-list, alert, help-link

**Files:**
- Create: `ui/display.html`
- Test: `display_test.go`

**Interfaces:**
- Consumes: the template funcs and `renderComponent` helper from Task 6.
- Produces: `{{define}}` names `person`, `status-pill`, `badge`, `meter`, `mono`, `detail-list`, `alert`, `help-link`.

**Decision recorded (design doc silent):** `meter` renders a native `<progress>` rather than a div with an inline width. `html/template` refuses to interpolate a computed value into a `style=` attribute (it becomes `ZgotmplZ`), and `<progress>` is accessible, zero-JS and needs no CSS trick.

- [ ] **Step 1: Write `ui/display.html`**

```html
{{/* Display components — design doc §9.

     person       name, email?, href?, initials?
     status-pill  label, tone? (ok|warn|danger|info; default neutral)
     badge        label, tone?
     meter        value, max, label?
     mono         value
     detail-list  items ([]dict: label, value)
     alert        tone? (ok|warn|danger|info), title?, body
     help-link    href, label?

     tone drives colour only; the pill's own text carries the meaning, so
     the component is still readable in monochrome and to a screen
     reader. meter uses a native <progress>: html/template will not
     interpolate a computed width into a style attribute, and <progress>
     needs no such trick. */}}

{{define "person"}}{{if .href}}<a class="r-person" href="{{.href}}">{{else}}<span class="r-person">{{end}}
  <span class="r-person-avatar" aria-hidden="true">{{if .initials}}{{.initials}}{{else}}{{icon "user"}}{{end}}</span>
  <span>
    <span class="r-person-name">{{with .name}}{{.}}{{end}}</span>
    {{with .email}}<span class="r-person-email"> {{.}}</span>{{end}}
  </span>
{{if .href}}</a>{{else}}</span>{{end}}{{end}}

{{define "status-pill"}}<span class="r-pill{{with .tone}} r-tone-{{.}}{{end}}">{{with .tone}}{{if eq . "ok"}}{{icon "circle-check"}}{{else if eq . "danger"}}{{icon "circle-x"}}{{else if eq . "warn"}}{{icon "triangle-alert"}}{{end}}{{end}}{{with .label}}{{.}}{{end}}</span>{{end}}

{{define "badge"}}<span class="r-badge{{with .tone}} r-tone-{{.}}{{end}}">{{with .label}}{{.}}{{end}}</span>{{end}}

{{define "meter"}}{{$v := 0}}{{if .value}}{{$v = .value}}{{end}}{{$m := 100}}{{if .max}}{{$m = .max}}{{end}}<span class="r-meter">
  <progress value="{{$v}}" max="{{$m}}">{{$v}}/{{$m}}</progress>
  <span class="r-meter-label">{{if .label}}{{.label}}{{else}}{{$v}}/{{$m}}{{end}}</span>
</span>{{end}}

{{define "mono"}}<code class="r-mono">{{with .value}}{{.}}{{end}}</code>{{end}}

{{define "detail-list"}}<dl class="r-detail-list">
{{range .items}}  <dt>{{with .label}}{{.}}{{end}}</dt>
  <dd>{{with .value}}{{.}}{{end}}</dd>
{{end}}</dl>{{end}}

{{define "alert"}}<div class="r-alert{{with .tone}} r-tone-{{.}}{{end}}" role="{{if .tone}}{{if eq .tone "danger"}}alert{{else}}status{{end}}{{else}}status{{end}}">
  <span aria-hidden="true">{{with .tone}}{{if eq . "ok"}}{{icon "circle-check"}}{{else if eq . "danger"}}{{icon "circle-x"}}{{else if eq . "warn"}}{{icon "triangle-alert"}}{{else}}{{icon "info"}}{{end}}{{else}}{{icon "info"}}{{end}}</span>
  <div class="r-alert-body">
    <div class="r-alert-title">{{if .title}}{{.title}}{{else if .tone}}{{T (printf "ui.tone.%s" .tone)}}{{else}}{{T "ui.tone.info"}}{{end}}</div>
    {{with .body}}<div>{{.}}</div>{{end}}
  </div>
</div>{{end}}

{{define "help-link"}}<a class="r-help-link" href="{{with .href}}{{.}}{{end}}">{{icon "circle-help"}}<span>{{if .label}}{{.label}}{{else}}{{T "ui.help"}}{{end}}</span></a>{{end}}
```

- [ ] **Step 2: Write the test**

Create `display_test.go`:

```go
package rastrillo

import (
	"html/template"
	"strings"
	"testing"
)

func TestDisplayOptionalKeysNeverLeakNoValue(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
	}{
		{"person", map[string]any{"name": "Ada"}},
		{"status-pill", map[string]any{"label": "Open"}},
		{"badge", map[string]any{"label": "New"}},
		{"meter", map[string]any{}},
		{"mono", map[string]any{}},
		{"detail-list", map[string]any{}},
		{"alert", map[string]any{"body": "hi"}},
		{"help-link", map[string]any{"href": "/help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderComponent(t, "en", tt.name, tt.data)
			if strings.Contains(got, "no value") {
				t.Errorf("%s leaked a missing key:\n%s", tt.name, got)
			}
			if strings.Contains(got, "<script") || strings.Contains(got, "ZgotmplZ") {
				t.Errorf("%s emitted script or an unsafe-value placeholder:\n%s", tt.name, got)
			}
		})
	}
}

func TestPerson(t *testing.T) {
	got := renderComponent(t, "en", "person", map[string]any{
		"name": "Ada Lovelace", "email": "ada@example.com", "href": "/people/1", "initials": "AL",
	})
	for _, want := range []string{`<a class="r-person" href="/people/1">`, ">AL<", "Ada Lovelace", "ada@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	noInitials := renderComponent(t, "en", "person", map[string]any{"name": "Ada"})
	if !strings.Contains(noInitials, "<svg ") {
		t.Errorf("expected the vendored user icon as the avatar fallback:\n%s", noInitials)
	}
}

func TestStatusPillTonesCarryTextNotJustColour(t *testing.T) {
	tests := []struct{ tone, wantClass string }{
		{"ok", "r-tone-ok"},
		{"warn", "r-tone-warn"},
		{"danger", "r-tone-danger"},
		{"info", "r-tone-info"},
	}
	for _, tt := range tests {
		t.Run(tt.tone, func(t *testing.T) {
			got := renderComponent(t, "en", "status-pill", map[string]any{"label": "Paid", "tone": tt.tone})
			if !strings.Contains(got, tt.wantClass) {
				t.Errorf("missing %q:\n%s", tt.wantClass, got)
			}
			if !strings.Contains(got, "Paid") {
				t.Errorf("the pill's meaning must be in its text, not only its colour:\n%s", got)
			}
		})
	}
	neutral := renderComponent(t, "en", "status-pill", map[string]any{"label": "Draft"})
	if strings.Contains(neutral, "r-tone-") {
		t.Errorf("a toneless pill must carry no tone class:\n%s", neutral)
	}
}

func TestMeterUsesNativeProgress(t *testing.T) {
	got := renderComponent(t, "en", "meter", map[string]any{"value": 40, "max": 120})
	for _, want := range []string{`<progress value="40" max="120">`, "40/120"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "style=") {
		t.Error("meter must not use an inline style — html/template refuses computed style values")
	}
	zero := renderComponent(t, "en", "meter", map[string]any{"value": 0, "max": 10})
	if !strings.Contains(zero, `value="0"`) {
		t.Errorf("a zero value must render as 0, not fall back to a default:\n%s", zero)
	}
}

func TestDetailList(t *testing.T) {
	got := renderComponent(t, "en", "detail-list", map[string]any{
		"items": []any{
			map[string]any{"label": "Reference", "value": template.HTML(`<code class="r-mono">A-17</code>`)},
			map[string]any{"label": "Total", "value": "€12.34"},
		},
	})
	for _, want := range []string{"<dt>Reference</dt>", `<code class="r-mono">A-17</code>`, "<dt>Total</dt>", "€12.34"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestAlertTitleComesFromTheCatalog(t *testing.T) {
	tests := []struct{ tone, wantTitle, wantRole string }{
		{"ok", "Success", "status"},
		{"warn", "Warning", "status"},
		{"danger", "Error", "alert"},
		{"info", "Information", "status"},
	}
	for _, tt := range tests {
		t.Run(tt.tone, func(t *testing.T) {
			got := renderComponent(t, "en", "alert", map[string]any{"tone": tt.tone, "body": "Something happened."})
			if !strings.Contains(got, tt.wantTitle) {
				t.Errorf("missing catalog title %q:\n%s", tt.wantTitle, got)
			}
			if !strings.Contains(got, `role="`+tt.wantRole+`"`) {
				t.Errorf("missing role=%q:\n%s", tt.wantRole, got)
			}
		})
	}
	custom := renderComponent(t, "en", "alert", map[string]any{"tone": "ok", "title": "Saved", "body": "b"})
	if !strings.Contains(custom, "Saved") || strings.Contains(custom, "Success") {
		t.Errorf("a caller-supplied title must replace the catalog default:\n%s", custom)
	}
}

func TestMonoAndHelpLink(t *testing.T) {
	if got := renderComponent(t, "en", "mono", map[string]any{"value": "sha-2f5b"}); !strings.Contains(got, `<code class="r-mono">sha-2f5b</code>`) {
		t.Errorf("mono:\n%s", got)
	}
	got := renderComponent(t, "en", "help-link", map[string]any{"href": "/docs/tickets"})
	for _, want := range []string{`href="/docs/tickets"`, ">Help<", "<svg "} {
		if !strings.Contains(got, want) {
			t.Errorf("help-link missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 3: Run the test**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1 -run 'Display|Person|StatusPill|Meter|DetailList|Alert|MonoAndHelp'`
Expected: PASS.

- [ ] **Step 4: gofmt + full sweep**

Run: `gofmt -l display_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add ui/display.html display_test.go
git commit -m "ui: display components — person, pills, meter, detail list, alert, help link (design doc §9)"
```

---

### Task 10: `ui/form.html` — field family, toggle-block, seg-tabs, form-foot, menu, confirm, modal-route, bulk-select

**Files:**
- Create: `ui/form.html`
- Test: `form_test.go`

**Interfaces:**
- Consumes: the template funcs and `renderComponent` helper from Task 6.
- Produces: `{{define}}` names `field`, `field-text`, `field-textarea`, `field-select`, `toggle-block`, `seg-tabs`, `form-foot`, `menu`, `confirm`, `modal-route`, `bulk-select`.

**Decisions recorded (design doc silent):**
- **`field-text`/`field-textarea`/`field-select` each render their own label/hint/error wrapper rather than composing `field`.** Go templates cannot capture a rendered fragment into a variable, so composing would mean every caller pre-rendering the control in Go. Six duplicated lines beat that — "prefer duplication over the wrong abstraction" (§2).
- **`menu` is a native `<details>`**, the only zero-JS disclosure HTML has.
- **`toggle-block` emits a hidden empty input before its checkbox**, so an unchecked box still posts the field. Without it a zero-JS form cannot express "turn this off".
- **`confirm` and `modal-route` render page *bodies*, not overlays** — §9 requires "destructive actions as their own confirm-page URL, modals as their own routes", so each is the content of a real route, reached and left by plain links.

- [ ] **Step 1: Write `ui/form.html`**

```html
{{/* Form and interaction components — design doc §9.

     field           name?, label, hint?, error?, required?, control (HTML)
     field-text      name, label, value?, type?, hint?, error?, required?, autocomplete?
     field-textarea  name, label, value?, hint?, error?, required?
     field-select    name, label, options ([]dict: value,label,selected), hint?, error?, required?
     toggle-block    name, label, hint?, checked?
     seg-tabs        items ([]dict: href,label,current)
     form-foot       submit?, cancel? (href), danger?
     menu            label?, items ([]dict: href,label,danger)
     confirm         action, title?, body?, submit?, cancel? (href), danger?, csrf? (HTML)
     modal-route     title, body (HTML), close (href)
     bulk-select     action, items ([]dict: value,label), actions ([]dict: name,label,danger)

     Nothing here needs JavaScript. The disclosure is a native <details>;
     the confirm page and the modal are their own routes, reached and
     left by plain links; every destructive action is a POST from a form
     the person had to arrive at deliberately.

     The three field-* components repeat field's wrapper markup rather
     than composing it: Go templates cannot capture a rendered fragment
     into a variable, so composing would push control rendering back into
     every caller's Go code. Six duplicated lines beat that. */}}

{{define "field"}}<div class="r-field">
  <label class="r-field-label"{{with .name}} for="{{.}}"{{end}}>{{with .label}}{{.}}{{end}}{{if .required}} <span class="r-field-required" title="{{T "ui.required"}}">*</span>{{end}}</label>
  {{with .control}}{{.}}{{end}}
  {{with .hint}}<span class="r-field-hint">{{.}}</span>{{end}}
  {{with .error}}<span class="r-field-error">{{.}}</span>{{end}}
</div>{{end}}

{{define "field-text"}}<div class="r-field">
  <label class="r-field-label" for="{{with .name}}{{.}}{{end}}">{{with .label}}{{.}}{{end}}{{if .required}} <span class="r-field-required" title="{{T "ui.required"}}">*</span>{{end}}</label>
  <input class="r-input" id="{{with .name}}{{.}}{{end}}" name="{{with .name}}{{.}}{{end}}" type="{{if .type}}{{.type}}{{else}}text{{end}}" value="{{with .value}}{{.}}{{end}}"{{with .autocomplete}} autocomplete="{{.}}"{{end}}{{if .required}} required{{end}}{{if .error}} aria-invalid="true"{{end}}>
  {{with .hint}}<span class="r-field-hint">{{.}}</span>{{end}}
  {{with .error}}<span class="r-field-error">{{.}}</span>{{end}}
</div>{{end}}

{{define "field-textarea"}}<div class="r-field">
  <label class="r-field-label" for="{{with .name}}{{.}}{{end}}">{{with .label}}{{.}}{{end}}{{if .required}} <span class="r-field-required" title="{{T "ui.required"}}">*</span>{{end}}</label>
  <textarea class="r-textarea" id="{{with .name}}{{.}}{{end}}" name="{{with .name}}{{.}}{{end}}"{{if .required}} required{{end}}{{if .error}} aria-invalid="true"{{end}}>{{with .value}}{{.}}{{end}}</textarea>
  {{with .hint}}<span class="r-field-hint">{{.}}</span>{{end}}
  {{with .error}}<span class="r-field-error">{{.}}</span>{{end}}
</div>{{end}}

{{define "field-select"}}<div class="r-field">
  <label class="r-field-label" for="{{with .name}}{{.}}{{end}}">{{with .label}}{{.}}{{end}}{{if .required}} <span class="r-field-required" title="{{T "ui.required"}}">*</span>{{end}}</label>
  <select class="r-select" id="{{with .name}}{{.}}{{end}}" name="{{with .name}}{{.}}{{end}}"{{if .required}} required{{end}}>{{range .options}}<option value="{{with .value}}{{.}}{{end}}"{{if .selected}} selected{{end}}>{{with .label}}{{.}}{{end}}</option>{{end}}</select>
  {{with .hint}}<span class="r-field-hint">{{.}}</span>{{end}}
  {{with .error}}<span class="r-field-error">{{.}}</span>{{end}}
</div>{{end}}

{{define "toggle-block"}}<label class="r-toggle-block">
  {{/* The hidden empty input is load-bearing: an unchecked box posts
       nothing at all, so without it a zero-JS form cannot say "off". */}}
  <input type="hidden" name="{{with .name}}{{.}}{{end}}" value="">
  <input type="checkbox" name="{{with .name}}{{.}}{{end}}" value="1"{{if .checked}} checked{{end}}>
  <span>
    <span class="r-field-label">{{with .label}}{{.}}{{end}}</span>
    {{with .hint}}<span class="r-field-hint">{{.}}</span>{{end}}
  </span>
</label>{{end}}

{{define "seg-tabs"}}<nav class="r-seg-tabs">{{range .items}}<a class="r-seg-tab{{if .current}} is-current{{end}}" href="{{with .href}}{{.}}{{end}}"{{if .current}} aria-current="page"{{end}}>{{with .label}}{{.}}{{end}}</a>{{end}}</nav>{{end}}

{{define "form-foot"}}<div class="r-form-foot">
  <button class="r-btn{{if .danger}} r-btn-danger{{end}}" type="submit">{{if .submit}}{{.submit}}{{else}}{{T "ui.save"}}{{end}}</button>
  {{with .cancel}}<a class="r-btn r-btn-quiet" href="{{.}}">{{T "ui.cancel"}}</a>{{end}}
</div>{{end}}

{{define "menu"}}<details class="r-menu">
  <summary class="r-btn r-btn-quiet">{{icon "ellipsis"}}<span class="r-visually-hidden">{{if .label}}{{.label}}{{else}}{{T "ui.more"}}{{end}}</span>{{icon "chevron-down"}}</summary>
  <ul class="r-menu-items">{{range .items}}<li><a class="{{if .danger}}is-danger{{end}}" href="{{with .href}}{{.}}{{end}}">{{with .label}}{{.}}{{end}}</a></li>{{end}}</ul>
</details>{{end}}

{{define "confirm"}}<div class="r-confirm">
  <h1 class="r-page-title">{{if .title}}{{.title}}{{else}}{{T "ui.confirm.title"}}{{end}}</h1>
  <p class="r-confirm-body">{{if .body}}{{.body}}{{else}}{{T "ui.confirm.body"}}{{end}}</p>
  <form method="post" action="{{with .action}}{{.}}{{end}}">
    {{with .csrf}}{{.}}{{end}}
    <div class="r-form-foot">
      <button class="r-btn{{if .danger}} r-btn-danger{{end}}" type="submit">{{if .submit}}{{.submit}}{{else}}{{T "ui.confirm"}}{{end}}</button>
      {{with .cancel}}<a class="r-btn r-btn-quiet" href="{{.}}">{{T "ui.cancel"}}</a>{{end}}
    </div>
  </form>
</div>{{end}}

{{define "modal-route"}}<section class="r-modal" role="dialog" aria-labelledby="r-modal-title">
  <div class="r-modal-head">
    <h1 class="r-page-title" id="r-modal-title">{{with .title}}{{.}}{{end}}</h1>
    {{with .close}}<a class="r-modal-close" href="{{.}}">{{icon "x"}}<span class="r-visually-hidden">{{T "ui.close"}}</span></a>{{end}}
  </div>
  {{with .body}}<div>{{.}}</div>{{end}}
</section>{{end}}

{{define "bulk-select"}}<form method="post" action="{{with .action}}{{.}}{{end}}">
  {{with .csrf}}{{.}}{{end}}
  <ul class="r-list">{{range .items}}<li class="r-bulk-row">
    <input type="checkbox" name="selected" value="{{with .value}}{{.}}{{end}}">
    <span>{{with .label}}{{.}}{{end}}</span>
  </li>{{end}}</ul>
  <div class="r-bulk-actions">
    {{range .actions}}<button class="r-btn{{if .danger}} r-btn-danger{{end}}" type="submit" name="action" value="{{with .name}}{{.}}{{end}}">{{with .label}}{{.}}{{end}}</button>{{end}}
  </div>
</form>{{end}}
```

- [ ] **Step 2: Write the test**

Create `form_test.go`:

```go
package rastrillo

import (
	"html/template"
	"strings"
	"testing"
)

func TestFormOptionalKeysNeverLeakNoValue(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
	}{
		{"field", map[string]any{"label": "Name"}},
		{"field-text", map[string]any{"name": "n", "label": "Name"}},
		{"field-textarea", map[string]any{"name": "n", "label": "Notes"}},
		{"field-select", map[string]any{"name": "n", "label": "Status"}},
		{"toggle-block", map[string]any{"name": "n", "label": "On"}},
		{"seg-tabs", map[string]any{}},
		{"form-foot", map[string]any{}},
		{"menu", map[string]any{}},
		{"confirm", map[string]any{"action": "/x"}},
		{"modal-route", map[string]any{"title": "T"}},
		{"bulk-select", map[string]any{"action": "/x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderComponent(t, "en", tt.name, tt.data)
			if strings.Contains(got, "no value") {
				t.Errorf("%s leaked a missing key:\n%s", tt.name, got)
			}
			if strings.Contains(got, "<script") || strings.Contains(got, "onclick") {
				t.Errorf("%s is not zero-JS:\n%s", tt.name, got)
			}
		})
	}
}

func TestFieldText(t *testing.T) {
	got := renderComponent(t, "en", "field-text", map[string]any{
		"name": "email", "label": "Email", "value": "ada@example.com",
		"type": "email", "hint": "We never share it.", "required": true,
		"autocomplete": "email",
	})
	for _, want := range []string{
		`for="email"`, `id="email"`, `name="email"`, `type="email"`,
		`value="ada@example.com"`, `autocomplete="email"`, " required",
		"We never share it.", `title="Required"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	withErr := renderComponent(t, "en", "field-text", map[string]any{
		"name": "email", "label": "Email", "error": "Not an address",
	})
	for _, want := range []string{`aria-invalid="true"`, "Not an address"} {
		if !strings.Contains(withErr, want) {
			t.Errorf("missing %q:\n%s", want, withErr)
		}
	}
}

func TestFieldSelectAndTextarea(t *testing.T) {
	sel := renderComponent(t, "en", "field-select", map[string]any{
		"name": "status", "label": "Status",
		"options": []any{
			map[string]any{"value": "open", "label": "Open", "selected": true},
			map[string]any{"value": "shut", "label": "Shut"},
		},
	})
	for _, want := range []string{`<option value="open" selected>Open</option>`, `<option value="shut">Shut</option>`} {
		if !strings.Contains(sel, want) {
			t.Errorf("field-select missing %q:\n%s", want, sel)
		}
	}
	ta := renderComponent(t, "en", "field-textarea", map[string]any{"name": "notes", "label": "Notes", "value": "hello"})
	if !strings.Contains(ta, `name="notes"`) || !strings.Contains(ta, ">hello</textarea>") {
		t.Errorf("field-textarea:\n%s", ta)
	}
}

func TestToggleBlockCanExpressOff(t *testing.T) {
	got := renderComponent(t, "en", "toggle-block", map[string]any{"name": "notify", "label": "Email me", "checked": true})
	if !strings.Contains(got, `<input type="hidden" name="notify" value="">`) {
		t.Errorf("an unchecked box posts nothing; the hidden companion is required:\n%s", got)
	}
	if !strings.Contains(got, `<input type="checkbox" name="notify" value="1" checked>`) {
		t.Errorf("checkbox markup:\n%s", got)
	}
}

func TestSegTabsAndFormFoot(t *testing.T) {
	tabs := renderComponent(t, "en", "seg-tabs", map[string]any{
		"items": []any{
			map[string]any{"href": "/basics", "label": "Basics", "current": true},
			map[string]any{"href": "/advanced", "label": "Advanced"},
		},
	})
	for _, want := range []string{`href="/basics"`, "is-current", `aria-current="page"`, `href="/advanced"`} {
		if !strings.Contains(tabs, want) {
			t.Errorf("seg-tabs missing %q:\n%s", want, tabs)
		}
	}

	foot := renderComponent(t, "en", "form-foot", map[string]any{"cancel": "/back"})
	if !strings.Contains(foot, ">Save</button>") || !strings.Contains(foot, `href="/back"`) || !strings.Contains(foot, ">Cancel</a>") {
		t.Errorf("form-foot defaults come from the catalog:\n%s", foot)
	}
	danger := renderComponent(t, "en", "form-foot", map[string]any{"submit": "Delete", "danger": true})
	if !strings.Contains(danger, "r-btn-danger") || !strings.Contains(danger, ">Delete</button>") {
		t.Errorf("form-foot danger:\n%s", danger)
	}
}

func TestMenuIsANativeDisclosure(t *testing.T) {
	got := renderComponent(t, "en", "menu", map[string]any{
		"items": []any{
			map[string]any{"href": "/edit", "label": "Edit"},
			map[string]any{"href": "/delete", "label": "Delete", "danger": true},
		},
	})
	for _, want := range []string{`<details class="r-menu">`, "<summary", "More actions", `href="/edit"`, "is-danger"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestConfirmIsItsOwnPostPage(t *testing.T) {
	got := renderComponent(t, "en", "confirm", map[string]any{
		"action": "/orders/7/cancel", "cancel": "/orders/7", "danger": true,
		"csrf": template.HTML(`<input type="hidden" name="csrf" value="tok">`),
	})
	for _, want := range []string{
		"Are you sure?", "This cannot be undone.",
		`<form method="post" action="/orders/7/cancel">`,
		`name="csrf" value="tok"`, "r-btn-danger", ">Confirm</button>", `href="/orders/7"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestModalRouteIsAPageNotAnOverlay(t *testing.T) {
	got := renderComponent(t, "en", "modal-route", map[string]any{
		"title": "Edit order", "body": template.HTML("<p>form here</p>"), "close": "/orders/7",
	})
	for _, want := range []string{`role="dialog"`, "Edit order", "<p>form here</p>", `href="/orders/7"`, ">Close<"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestBulkSelectPostsNamedActions(t *testing.T) {
	got := renderComponent(t, "en", "bulk-select", map[string]any{
		"action": "/orders/bulk",
		"items": []any{
			map[string]any{"value": "7", "label": "Order 7"},
			map[string]any{"value": "8", "label": "Order 8"},
		},
		"actions": []any{
			map[string]any{"name": "archive", "label": "Archive"},
			map[string]any{"name": "delete", "label": "Delete", "danger": true},
		},
	})
	for _, want := range []string{
		`<form method="post" action="/orders/bulk">`,
		`type="checkbox" name="selected" value="7"`,
		`name="action" value="archive"`,
		`name="action" value="delete"`,
		"r-btn-danger",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 3: Run the test**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test . -count=1 -run 'Form|Field|Toggle|SegTabs|Menu|Confirm|Modal|BulkSelect'`
Expected: PASS.

- [ ] **Step 4: gofmt + full sweep**

Run: `gofmt -l form_test.go` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add ui/form.html form_test.go
git commit -m "ui: form and interaction components, all zero-JS (design doc §9)"
```

---

### Task 11: `internal/generate/locales.go` — catalog completeness

**Files:**
- Create: `internal/generate/locales.go`
- Test: `internal/generate/locales_test.go`

**Interfaces:**
- Consumes: `catalog.Decode` (Task 1).
- Produces (Task 12 calls this exactly):
  - `func MissingKeys(localesDir, defaultCode string) (map[string][]string, error)` — locale code → sorted missing keys; a nil/empty map means complete.

**Behaviour, from design doc §10 and §13:** "missing keys fall back to the declared default locale during development, but `carlos vet` fails the build on a key present in the default locale's catalog and silently missing from a declared non-default one — silent fallback while iterating, loud failure before ship." Only the **app's own** catalogs are compared; the framework's base catalog is never an app's to translate.

**Decision recorded (design doc silent):** the generator has no way to read `Options.DefaultLocale` out of the app's `main.go` without evaluating Go, so the default locale is a **flag** on `rastrillo generate --check`, defaulting to `"en"`. Task 12 wires it. A `locales/` directory holding catalogs but no `<default>.toml` is an error, not a pass.

Note: `contains` already exists in `internal/generate/generate_test.go` (same package) — do not redeclare it.

- [ ] **Step 1: Write the failing test**

Create `internal/generate/locales_test.go`:

```go
package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\nb = \"B\"\nc = \"C\"\n")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "de.toml", "a = \"A\"\nb = \"B\"\nc = \"C\"\n")

	got, err := MissingKeys(dir, "en")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"fr": {"b", "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MissingKeys = %v, want %v (de is complete and must not appear)", got, want)
	}
}

func TestMissingKeysIgnoresExtraKeysInANonDefaultLocale(t *testing.T) {
	// A locale carrying a key the default does not have is not a build
	// failure: design doc §10 only fails on the other direction.
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\nextra = \"E\"\n")

	got, err := MissingKeys(dir, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("MissingKeys = %v, want none", got)
	}
}

func TestMissingKeysWithNoLocalesDirectory(t *testing.T) {
	// A single-locale app ships no catalogs at all; that is not a
	// failure, it is the common case (design doc §10).
	got, err := MissingKeys(filepath.Join(t.TempDir(), "locales"), "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("MissingKeys = %v, want none", got)
	}
}

func TestMissingKeysRequiresTheDefaultCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "fr.toml", "a = \"A\"\n")

	_, err := MissingKeys(dir, "en")
	if err == nil {
		t.Fatal("want an error when other locales exist but the default's catalog does not")
	}
	if !strings.Contains(err.Error(), "en.toml") {
		t.Errorf("error should name the missing file: %v", err)
	}
}

func TestMissingKeysReportsAnUndecodableCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	writeCatalog(t, dir, "en.toml", "a = \"A\"\n")
	writeCatalog(t, dir, "fr.toml", "[table]\n")

	_, err := MissingKeys(dir, "en")
	if err == nil {
		t.Fatal("want an error for an undecodable catalog")
	}
	if !strings.Contains(err.Error(), "fr.toml") {
		t.Errorf("error should name the offending file: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/generate/ -count=1`
Expected: FAIL — `MissingKeys` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/generate/locales.go`:

```go
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

// MissingKeys reports, per non-default locale, the keys present in the
// default locale's catalog and absent from that one — design doc §10's
// pre-ship check: "silent fallback while iterating, loud failure before
// ship." The other direction (a key a non-default locale has and the
// default does not) is deliberately not a failure; §10 names only this
// one.
//
// Only the app's own catalogs are compared. The framework's base
// component catalog is never an app's to translate, and it is not in
// localesDir, so it cannot be reported here by construction.
//
// An app with no locales/ directory at all is the common single-locale
// case and returns no findings.
func MissingKeys(localesDir, defaultCode string) (map[string][]string, error) {
	entries, err := os.ReadDir(localesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	catalogs := map[string]map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(localesDir, name))
		if err != nil {
			return nil, err
		}
		m, err := catalog.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		catalogs[strings.TrimSuffix(name, ".toml")] = m
	}
	if len(catalogs) == 0 {
		return nil, nil
	}

	def, ok := catalogs[defaultCode]
	if !ok {
		return nil, fmt.Errorf("no %s.toml in %s, but %d other locale catalog(s) are there — the default locale's catalog is what every other one is checked against", defaultCode, localesDir, len(catalogs))
	}

	out := map[string][]string{}
	for code, c := range catalogs {
		if code == defaultCode {
			continue
		}
		var missing []string
		for key := range def {
			if _, ok := c[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			out[code] = missing
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./internal/generate/ -count=1`
Expected: PASS — 5 new tests plus the existing generator tests.

- [ ] **Step 5: gofmt + full sweep**

Run: `gofmt -l internal/generate/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/generate/locales.go internal/generate/locales_test.go
git commit -m "generate: i18n catalog completeness check (design doc §10, §13)"
```

---

### Task 12: `cmd/rastrillo` — `generate --check`, and watch the new trees

**Files:**
- Modify: `cmd/rastrillo/generate.go`
- Modify: `cmd/rastrillo/dev.go` (the `watchDirs` var and its comment only)
- Modify: `cmd/rastrillo/main.go` (the `usage()` text only)
- Test: `cmd/rastrillo/generate_test.go`

**Interfaces:**
- Consumes: `generate.Discover`, `generate.Rewrite`, `generate.Router` (existing) and `generate.MissingKeys` (Task 11).
- Produces: `rastrillo generate [--check] [--default-locale <code>] [dir]`.

**Compatibility note:** `runNew` and `runDev` both call `runGenerate([]string{dir})`. `flag.FlagSet.Parse` stops at the first non-flag argument, so a bare positional still works unchanged — but **flags must come before the directory**. Say so in the usage text.

- [ ] **Step 1: Write the failing test**

Create `cmd/rastrillo/generate_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scaffold(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const handleSrc = "package actions\n\nimport (\n\t\"net/http\"\n\n\t\"amadan.net/rastrillo/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

func TestGenerateWritesTheRouter(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
}

func TestGenerateCheckWritesNothing(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen")); !os.IsNotExist(err) {
		t.Fatal("--check must not write gen/")
	}
}

func TestGenerateCheckFailsOnAnIncompleteCatalog(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/fr.toml":      "a = \"A\"\n",
	})
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want a failure: fr.toml is missing a key en.toml has (design doc §10)")
	}
	if !strings.Contains(err.Error(), "catalog") {
		t.Errorf("error should name the check that failed: %v", err)
	}
}

func TestGenerateCheckPassesOnACompleteCatalog(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\n",
		"locales/fr.toml":      "a = \"A\"\n",
	})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("want a pass, got %v", err)
	}
}

func TestGenerateCheckHonoursTheDefaultLocaleFlag(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/fr.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/en.toml":      "a = \"A\"\n",
	})
	if err := runGenerate([]string{"--check", "--default-locale", "fr", dir}); err == nil {
		t.Fatal("with fr as the default, en.toml is the incomplete one")
	}
}

func TestGenerateWatchesLocalesAndTemplates(t *testing.T) {
	// A save to a catalog or a template must restart the dev loop, or
	// the running binary keeps serving the old embedded copy.
	for _, want := range []string{"locales", "templates"} {
		found := false
		for _, d := range watchDirs {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Errorf("watchDirs is missing %q: %v", want, watchDirs)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./cmd/rastrillo/ -count=1`
Expected: FAIL — `runGenerate` does not accept `--check`; `watchDirs` lacks the two entries.

- [ ] **Step 3: Rewrite `cmd/rastrillo/generate.go`**

Replace the whole file with:

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"amadan.net/rastrillo/rastrillo/internal/generate"
)

// runGenerate implements `rastrillo generate [flags] [dir]`: the
// one-shot generator rastrillo dev's watch loop and CI both call
// underneath (design doc §11) — one code path, not two.
//
// --check is the framework's half of `carlos vet` (§11): verify and
// report, write nothing. It covers route collisions (§4) and i18n
// catalog completeness (§10) today; the rest of §11's list arrives with
// the subsystems it checks.
//
// Flags come before the directory: FlagSet.Parse stops at the first
// non-flag argument, which is what keeps the older bare `rastrillo
// generate <dir>` form working unchanged.
func runGenerate(args []string) error {
	fset := flag.NewFlagSet("generate", flag.ContinueOnError)
	check := fset.Bool("check", false, "verify without writing (route collisions, i18n catalog completeness)")
	defaultLocale := fset.String("default-locale", "en", "locale every other catalog is checked against (design doc §10)")
	if err := fset.Parse(args); err != nil {
		return err
	}

	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	module, err := modulePath(dir)
	if err != nil {
		return err
	}

	actionsDir := filepath.Join(dir, "actions")
	if _, err := os.Stat(actionsDir); os.IsNotExist(err) {
		return fmt.Errorf("no actions/ directory in %s", dir)
	}

	actions, collisions, err := generate.Discover(actionsDir)
	if err != nil {
		return fmt.Errorf("discover actions: %w", err)
	}
	if len(collisions) > 0 {
		fmt.Fprintln(os.Stderr, "rastrillo generate: route collisions —")
		for _, c := range collisions {
			fmt.Fprintf(os.Stderr, "  %s claimed by:\n", c.Route)
			for _, s := range c.Sources {
				fmt.Fprintf(os.Stderr, "    actions/%s\n", s)
			}
		}
		return fmt.Errorf("%d route collision(s); build fails loudly on purpose (design doc §4)", len(collisions))
	}

	missing, err := generate.MissingKeys(filepath.Join(dir, "locales"), *defaultLocale)
	if err != nil {
		return fmt.Errorf("i18n catalog check: %w", err)
	}
	if len(missing) > 0 {
		codes := make([]string, 0, len(missing))
		for code := range missing {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		fmt.Fprintf(os.Stderr, "rastrillo generate: incomplete locale catalogs (default %q) —\n", *defaultLocale)
		for _, code := range codes {
			fmt.Fprintf(os.Stderr, "  locales/%s.toml is missing:\n", code)
			for _, key := range missing[code] {
				fmt.Fprintf(os.Stderr, "    %s\n", key)
			}
		}
		return fmt.Errorf("%d locale catalog(s) incomplete; silent fallback while iterating, loud failure before ship (design doc §10)", len(missing))
	}

	if *check {
		fmt.Printf("rastrillo generate --check: %d route(s), locale catalogs complete\n", len(actions))
		return nil
	}

	genDir := filepath.Join(dir, "gen")
	if err := os.RemoveAll(filepath.Join(genDir, "actions")); err != nil {
		return fmt.Errorf("clear stale generated actions: %w", err)
	}
	for _, a := range actions {
		if err := generate.Rewrite(actionsDir, genDir, a); err != nil {
			return fmt.Errorf("rewrite %s: %w", a.SourcePath, err)
		}
	}

	router, err := generate.Router(module, actions)
	if err != nil {
		return fmt.Errorf("render router.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "router.go"), router, 0o644); err != nil {
		return err
	}

	fmt.Printf("rastrillo generate: %d route(s) wired\n", len(actions))
	for _, a := range actions {
		fmt.Printf("  %-24s actions/%s\n", a.Route, a.SourcePath)
	}
	return nil
}
```

- [ ] **Step 4: Update `cmd/rastrillo/dev.go`'s `watchDirs`**

Replace:

```go
// watchDirs are the trees whose edits trigger the §11 loop: the design
// doc's app/, actions/, manifest/, plus cmd/ — rastrillo new scaffolds
// cmd/<name>/main.go, and a dev loop that ignores edits to it surprises
// people. gen/ is deliberately absent: it is the generator's output.
var watchDirs = []string{"actions", "app", "manifest", "cmd"}
```

with:

```go
// watchDirs are the trees whose edits trigger the §11 loop: the design
// doc's app/, actions/, manifest/, plus cmd/ — rastrillo new scaffolds
// cmd/<name>/main.go, and a dev loop that ignores edits to it surprises
// people — plus locales/ and templates/, which the app embeds into its
// binary (§9, §10): without a rebuild, a saved catalog or template
// keeps serving the copy compiled in at the last build. gen/ is
// deliberately absent: it is the generator's output.
var watchDirs = []string{"actions", "app", "manifest", "cmd", "locales", "templates"}
```

- [ ] **Step 5: Update `usage()` in `cmd/rastrillo/main.go`**

Replace the `generate` line with these two, keeping the existing column alignment:

```
  rastrillo generate [flags] [dir]     run the generator (flags before dir; default dir: .)
       --check --default-locale <code> verify only: route collisions, i18n catalogs
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./cmd/rastrillo/ -count=1`
Expected: PASS — the six new tests plus the existing `dev_test.go` ones.

- [ ] **Step 7: gofmt + full sweep**

Run: `gofmt -l cmd/rastrillo/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add cmd/rastrillo/generate.go cmd/rastrillo/dev.go cmd/rastrillo/main.go cmd/rastrillo/generate_test.go
git commit -m "cli: generate --check with the i18n catalog gate; dev watches locales/ and templates/ (design doc §10, §11)"
```

---

### Task 13: `rastrillo new` — scaffold `embed.go`, `locales/`, `templates/`

**Files:**
- Modify: `cmd/rastrillo/new.go`
- Test: `cmd/rastrillo/new_test.go`

**Interfaces:**
- Consumes: `rastrillo.Serve` `Options.Locales/DefaultLocale/LocaleFS/TemplateFS` (Task 7), `rastrillo.Render`/`rastrillo.T`/`rastrillo.LocaleFrom` (Tasks 3, 6), the `doc-open`/`page-header`/`box`/`doc-close` partials (Tasks 6, 8).
- Produces: the scaffolded layout Tasks 14–15 mirror in `examples/helloworld`.

**Decision recorded (design doc silent):** §10 puts catalogs at `locales/<code>.toml`, at the app root. `//go:embed` can only reach downwards from the file that declares it, so the embed declarations live in a **root-level `embed.go` whose package name is `app`** — the import path is the module path, the package identifier is `app`. That keeps §10's literal path and needs no new convention.

- [ ] **Step 1: Write the failing test**

Create `cmd/rastrillo/new_test.go`:

```go
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/catalog"
)

func TestScaffoldedGoFilesParse(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"embed.go", embedTemplate},
		{"actions/index.GET.go", actionTemplate},
		{"cmd/<name>/main.go", fmt.Sprintf(mainTemplate, "myapp")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parser.ParseFile(token.NewFileSet(), tt.name, tt.src, parser.AllErrors); err != nil {
				t.Fatalf("scaffolded %s does not parse: %v", tt.name, err)
			}
		})
	}
}

func TestScaffoldedMainWiresLocalizationAndTemplates(t *testing.T) {
	src := fmt.Sprintf(mainTemplate, "myapp")
	for _, want := range []string{
		`app "myapp"`, `"myapp/gen"`,
		"Locales:", "DefaultLocale:", "LocaleFS:", "TemplateFS:",
		"rastrillo.LocaleFrom(r)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("scaffolded main.go missing %q:\n%s", want, src)
		}
	}
	// A shared *Ctx would be a data race the moment Locale is per-request.
	if strings.Contains(src, "ctx := &rastrillo.Ctx{") {
		t.Error("scaffolded main.go must build a fresh Ctx per request, not share one")
	}
}

func TestScaffoldedEmbedDeclaresBothTrees(t *testing.T) {
	for _, want := range []string{"package app", "//go:embed locales", "//go:embed templates", "LocaleFS", "TemplateFS"} {
		if !strings.Contains(embedTemplate, want) {
			t.Errorf("scaffolded embed.go missing %q:\n%s", want, embedTemplate)
		}
	}
}

func TestScaffoldedCatalogDecodes(t *testing.T) {
	m, err := catalog.Decode([]byte(localesTemplate))
	if err != nil {
		t.Fatalf("scaffolded locales/en.toml does not decode: %v", err)
	}
	for _, key := range []string{"app.title", "app.tagline", "app.box.title", "app.box.body"} {
		if _, ok := m[key]; !ok {
			t.Errorf("scaffolded catalog missing %q: %v", key, m)
		}
	}
}

func TestScaffoldedTemplateParsesAndUsesTheVocabulary(t *testing.T) {
	stub := template.FuncMap{
		"T":       func(string) string { return "" },
		"Tf":      func(string, ...any) string { return "" },
		"locale":  func() string { return "en" },
		"locales": func() []string { return nil },
		"icon":    func(string) template.HTML { return "" },
		"dict":    func(...any) (map[string]any, error) { return nil, nil },
		"css":     func() string { return "" },
	}
	if _, err := template.New("t").Funcs(stub).Parse(indexTemplate); err != nil {
		t.Fatalf("scaffolded templates/index.html does not parse: %v", err)
	}
	for _, want := range []string{`{{define "index"}}`, `template "doc-open"`, `template "page-header"`, `template "box"`, `template "doc-close"`} {
		if !strings.Contains(indexTemplate, want) {
			t.Errorf("scaffolded index.html missing %q:\n%s", want, indexTemplate)
		}
	}
	if strings.Contains(indexTemplate, "<script") {
		t.Error("the scaffold must not ship JavaScript")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./cmd/rastrillo/ -count=1`
Expected: FAIL — `embedTemplate`, `localesTemplate`, `indexTemplate` undefined.

- [ ] **Step 3: Update `runNew`'s directory and file lists**

In `cmd/rastrillo/new.go`, replace the `dirs` slice with:

```go
	dirs := []string{
		name,
		filepath.Join(name, "actions"),
		filepath.Join(name, "cmd", name),
		filepath.Join(name, "locales"),
		filepath.Join(name, "templates"),
	}
```

replace the `files` map with:

```go
	files := map[string]string{
		filepath.Join(name, "go.mod"):                     fmt.Sprintf(goModTemplate, name),
		filepath.Join(name, "embed.go"):                   embedTemplate,
		filepath.Join(name, "actions", "index.GET.go"):    actionTemplate,
		filepath.Join(name, "cmd", name, "main.go"):       fmt.Sprintf(mainTemplate, name),
		filepath.Join(name, "locales", "en.toml"):         localesTemplate,
		filepath.Join(name, "templates", "index.html"):    indexTemplate,
	}
```

and replace the four `fmt.Print*` lines that list what was written with:

```go
	fmt.Printf("rastrillo new: scaffolded %s/\n", name)
	fmt.Println("  go.mod")
	fmt.Println("  embed.go                 locales/ and templates/, embedded")
	fmt.Println("  actions/index.GET.go")
	fmt.Printf("  cmd/%s/main.go\n", name)
	fmt.Println("  locales/en.toml          this app's catalog (design doc §10)")
	fmt.Println("  templates/index.html     a page on the built-in component vocabulary (§9)")
```

- [ ] **Step 4: Replace the template constants at the bottom of `new.go`**

Keep `goModTemplate` exactly as it is. Replace `actionTemplate` and `mainTemplate`, and add the three new constants:

```go
const embedTemplate = `// Package app holds this app's embedded assets.
//
// It sits at the module root because //go:embed only reaches downwards
// from the file declaring it, and the framework reads catalogs from
// locales/<code>.toml and templates from templates/*.html (design doc
// §9, §10). The import path is the module path; the package identifier
// is app.
package app

import "embed"

//go:embed locales
var LocaleFS embed.FS

//go:embed templates
var TemplateFS embed.FS
`

const actionTemplate = `package actions

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	// Render finds this request's locale-bound template set; T looks
	// the title up in locales/<locale>.toml, falling back to the
	// default locale and then to the framework's own catalog.
	if err := rastrillo.Render(w, r, "index", map[string]any{
		"title": rastrillo.T(r, "app.title"),
		"path":  r.URL.Path,
	}); err != nil {
		ctx.Logger.Error("render index", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
`

const localesTemplate = `# This app's catalog (design doc §10): flat key = "string", one file
# per locale, dots are literal. The framework's built-in component copy
# needs no entry here — only your own strings do.
#
# To add a locale: write locales/fr.toml, add "fr" to Options.Locales in
# cmd/<app>/main.go, and every page gets a /fr/... URL for free.
# rastrillo generate --check fails on any key present here and missing
# from a locale you declared.

app.title = "Hello, World"
app.tagline = "A rastrillo app."
app.box.title = "What's here"
app.box.body = "actions/index.GET.go rendered this through templates/index.html, on the framework's own component vocabulary."
`

const indexTemplate = `{{define "index"}}{{template "doc-open" .}}
{{template "page-header" (dict "title" (T "app.title") "subtitle" (T "app.tagline"))}}
{{template "box" (dict "title" (T "app.box.title") "body" (T "app.box.body"))}}
{{template "doc-close" .}}{{end}}
`

const mainTemplate = `package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo"

	app "%[1]s"
	"%[1]s/gen"
)

func main() {
	// -socket/-addr mirror the platform's activation contract (a
	// systemd-activated listener wins over either — see rastrillo.Serve).
	socket := flag.String("socket", "", "unix socket to listen on")
	addr := flag.String("addr", "", "TCP host:port to listen on")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// A fresh Ctx per request: Locale is per-request state, so a single
	// shared Ctx would be a data race the moment a second locale is
	// declared.
	mux := gen.Router(func(r *http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{Logger: logger, Locale: rastrillo.LocaleFrom(r)}
	})

	if err := rastrillo.Serve(rastrillo.Options{
		Mux:    mux,
		Socket: *socket,
		Addr:   *addr,
		Logger: logger,

		// Declare a locale here and write locales/<code>.toml to get
		// /<code>/... URLs for every route (design doc §10).
		Locales:       []string{"en"},
		DefaultLocale: "en",
		LocaleFS:      app.LocaleFS,
		TemplateFS:    app.TemplateFS,
	}); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
`
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go test ./cmd/rastrillo/ -count=1`
Expected: PASS.

- [ ] **Step 6: gofmt + full sweep**

Run: `gofmt -l cmd/rastrillo/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add cmd/rastrillo/new.go cmd/rastrillo/new_test.go
git commit -m "cli: rastrillo new scaffolds locales/, templates/ and the embed root (design doc §9, §10)"
```

---

### Task 14: `examples/helloworld` — embedded assets, two locales, one page template

**Files:**
- Create: `examples/helloworld/embed.go`
- Create: `examples/helloworld/locales/en.toml`
- Create: `examples/helloworld/locales/fr.toml`
- Create: `examples/helloworld/templates/index.html`

**Interfaces:**
- Consumes: nothing at compile time beyond `embed`; Task 15 imports `app "helloworld"` for `LocaleFS`/`TemplateFS`.
- Produces: `app.LocaleFS`, `app.TemplateFS` (package `app`, import path `helloworld`).

**Note:** `examples/helloworld` is its own Go module with `replace amadan.net/rastrillo/rastrillo => ../..`, so the repo-root sweep does **not** build it. This task and the next carry their own build command.

**Note:** this app is deployed live at `helloworld.dev.oncarlos.com`. Changing it is deliberate — the deployed page becomes the component-vocabulary demo on the next ship — not a side effect to be surprised by.

- [ ] **Step 1: Create `examples/helloworld/embed.go`**

```go
// Package app holds this app's embedded assets.
//
// It sits at the module root because //go:embed only reaches downwards
// from the file declaring it, and the framework reads catalogs from
// locales/<code>.toml and templates from templates/*.html (design doc
// §9, §10). The import path is the module path; the package identifier
// is app.
package app

import "embed"

//go:embed locales
var LocaleFS embed.FS

//go:embed templates
var TemplateFS embed.FS
```

- [ ] **Step 2: Create `examples/helloworld/locales/en.toml`**

```toml
# helloworld's catalog (design doc §10). Every key here must also exist
# in locales/fr.toml — `rastrillo generate --check` fails otherwise.

app.title = "Hello, World"
app.tagline = "A rastrillo app, on the framework's own component vocabulary."
app.box.title = "What you are looking at"
app.box.body = "Server-rendered Go. No JavaScript, no bundler, no CDN — the icons are vendored SVG constants and the stylesheet is embedded in the binary."
app.status = "Serving"
app.detail.version = "Build"
app.detail.locale = "Locale"
app.detail.path = "Path"
```

- [ ] **Step 3: Create `examples/helloworld/locales/fr.toml`**

```toml
# The same keys as en.toml, in French. Visit /fr/ to see them; the
# locale prefix is stripped before routing, so /fr/ and / are the same
# route (design doc §10).

app.title = "Bonjour, tout le monde"
app.tagline = "Une application rastrillo, sur le vocabulaire de composants du framework."
app.box.title = "Ce que vous regardez"
app.box.body = "Du Go rendu côté serveur. Pas de JavaScript, pas de bundler, pas de CDN — les icônes sont des constantes SVG intégrées et la feuille de style est compilée dans le binaire."
app.status = "En service"
app.detail.version = "Version"
app.detail.locale = "Langue"
app.detail.path = "Chemin"
```

- [ ] **Step 4: Create `examples/helloworld/templates/index.html`**

```html
{{define "index"}}{{template "doc-open" .}}
{{template "page-header" (dict
    "title" (T "app.title")
    "subtitle" (T "app.tagline")
    "actions" .statusPill)}}
{{template "box" (dict "title" (T "app.box.title") "body" (T "app.box.body"))}}
{{template "box" (dict "title" (T "app.detail.version") "body" .details)}}
{{template "doc-close" .}}{{end}}
```

- [ ] **Step 5: Verify the example module still builds**

Run:
```bash
cd /tmp/claude-1001/rastrillo-comp/examples/helloworld && \
  GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./...
```
Expected: clean. (`embed.go` compiles; `locales/` and `templates/` are both non-empty, which `//go:embed` requires.)

- [ ] **Step 6: Repo-root sweep**

Run from the repo root:
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean (unchanged — the example is a separate module).

- [ ] **Step 7: Commit**

```bash
git add examples/helloworld/embed.go examples/helloworld/locales examples/helloworld/templates
git commit -m "example: helloworld gets embedded catalogs (en/fr) and a page template"
```

---

### Task 15: `examples/helloworld` — render the vocabulary, regenerate

**Files:**
- Modify: `examples/helloworld/actions/index.GET.go`
- Modify: `examples/helloworld/cmd/helloworld/main.go`
- Regenerate: `examples/helloworld/gen/actions/index_get/index.GET.go` (by running the generator — do not hand-edit)

**Interfaces:**
- Consumes: `app.LocaleFS`/`app.TemplateFS` (Task 14); `rastrillo.Render`, `rastrillo.T`, `rastrillo.LocaleFrom`, `rastrillo.BuildVersion`, and `Options.Locales/DefaultLocale/LocaleFS/TemplateFS` (Tasks 3, 6, 7).

- [ ] **Step 1: Rewrite `examples/helloworld/actions/index.GET.go`**

```go
package actions

import (
	"html/template"
	"net/http"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	// Components take a dict; building the two composed fragments in Go
	// keeps the page template flat and readable. template.HTML here is
	// framework-generated markup, never user input.
	var pill, details strings.Builder
	if err := rastrillo.RenderTo(&pill, r, "status-pill", map[string]any{
		"label": rastrillo.T(r, "app.status"), "tone": "ok",
	}); err != nil {
		ctx.Logger.Error("render status-pill", "err", err)
	}
	if err := rastrillo.RenderTo(&details, r, "detail-list", map[string]any{
		"items": []any{
			map[string]any{"label": rastrillo.T(r, "app.detail.version"), "value": rastrillo.BuildVersion},
			map[string]any{"label": rastrillo.T(r, "app.detail.locale"), "value": rastrillo.LocaleFrom(r)},
			map[string]any{"label": rastrillo.T(r, "app.detail.path"), "value": r.URL.Path},
		},
	}); err != nil {
		ctx.Logger.Error("render detail-list", "err", err)
	}

	if err := rastrillo.Render(w, r, "index", map[string]any{
		"title":      rastrillo.T(r, "app.title"),
		"path":       r.URL.Path,
		"statusPill": template.HTML(pill.String()),
		"details":    template.HTML(details.String()),
	}); err != nil {
		ctx.Logger.Error("render index", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
```

This needs one small addition to the framework, made in this task because this is the first caller that needs it. Add to `ui.go`, directly after `(*UI).Render`:

```go
// RenderTo renders a component into a buffer instead of onto the wire,
// for an action that needs to compose one fragment into another's dict.
// Same locale resolution as Render; no headers are touched.
func RenderTo(w io.Writer, r *http.Request, name string, data any) error {
	u, ok := r.Context().Value(uiCtxKey{}).(*UI)
	if !ok {
		return errors.New("rastrillo: no UI on this request — is it served through rastrillo.Serve?")
	}
	t, ok := u.sets[LocaleFrom(r)]
	if !ok {
		t = u.sets[u.locales.Default()]
	}
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("rastrillo: render %q: %w", name, err)
	}
	return nil
}
```

The action's import block above is complete as written (`strings` for the two fragment buffers, `html/template` for the `template.HTML` wrapping).

- [ ] **Step 2: Add a framework test for `RenderTo`**

Append to `ui_test.go`:

```go
func TestRenderToComposesAFragment(t *testing.T) {
	ui, err := NewUI(UIOptions{Locales: []string{"en"}, DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	ui.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		if err := RenderTo(&b, r, "status-pill", map[string]any{"label": "Serving", "tone": "ok"}); err != nil {
			t.Errorf("RenderTo: %v", err)
		}
		got = b.String()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if !strings.Contains(got, "Serving") || !strings.Contains(got, "r-tone-ok") {
		t.Errorf("RenderTo = %q", got)
	}
}

func TestRenderToWithoutAUIOnTheRequest(t *testing.T) {
	var b strings.Builder
	if err := RenderTo(&b, httptest.NewRequest("GET", "/", nil), "status-pill", nil); err == nil {
		t.Fatal("want an error outside a Serve-handled request")
	}
}
```

- [ ] **Step 3: Rewrite `examples/helloworld/cmd/helloworld/main.go`**

```go
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"amadan.net/rastrillo/rastrillo"

	app "helloworld"
	"helloworld/gen"
)

func main() {
	// -socket/-addr mirror the platform's activation contract (a
	// systemd-activated listener wins over either — see rastrillo.Serve).
	socket := flag.String("socket", "", "unix socket to listen on")
	addr := flag.String("addr", "", "TCP host:port to listen on")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// A fresh Ctx per request: Locale is per-request state, so a single
	// shared Ctx would be a data race across two locales.
	mux := gen.Router(func(r *http.Request) *rastrillo.Ctx {
		return &rastrillo.Ctx{Logger: logger, Locale: rastrillo.LocaleFrom(r)}
	})

	if err := rastrillo.Serve(rastrillo.Options{
		Mux:    mux,
		Socket: *socket,
		Addr:   *addr,
		Logger: logger,

		Locales:       []string{"en", "fr"},
		DefaultLocale: "en",
		LocaleFS:      app.LocaleFS,
		TemplateFS:    app.TemplateFS,
	}); err != nil {
		logger.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Regenerate, and check the catalogs**

```bash
cd /tmp/claude-1001/rastrillo-comp && \
  GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go run ./cmd/rastrillo generate --check examples/helloworld && \
  GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go run ./cmd/rastrillo generate examples/helloworld
```
Expected: `--check` reports `1 route(s), locale catalogs complete`; the second command reports `1 route(s) wired  GET /  actions/index.GET.go`. Do not hand-edit anything under `examples/helloworld/gen/`.

- [ ] **Step 5: Build and exercise the example end to end**

```bash
cd /tmp/claude-1001/rastrillo-comp/examples/helloworld && \
  GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./cmd/helloworld && \
  ./helloworld -addr 127.0.0.1:9099 &
sleep 1
curl -s http://127.0.0.1:9099/            | grep -c "Hello, World"
curl -s http://127.0.0.1:9099/fr/         | grep -c "Bonjour, tout le monde"
curl -s http://127.0.0.1:9099/_rastrillo/ui.css | grep -c -- "--r-accent:"
curl -s http://127.0.0.1:9099/            | grep -c "<script" || true
curl -s http://127.0.0.1:9099/healthz
kill %1
```
Expected: `1`, `1`, `1`, then `0` for the `<script>` count (grep -c prints 0 and exits 1, hence the `|| true`), then `ok`. If `/fr/` returns 404, the locale prefix is not being stripped — re-check Task 3's `splitPrefix` and Task 7's middleware placement.

Remove the built binary afterwards: `rm -f /tmp/claude-1001/rastrillo-comp/examples/helloworld/helloworld` (it must not be committed).

- [ ] **Step 6: Repo-root sweep**

Run from the repo root:
`gofmt -l ui.go ui_test.go examples/helloworld/` (expect no output), then
`GOCACHE=/tmp/claude-1001/gocache GOFLAGS=-mod=mod GOPROXY=off go build ./... && go vet ./... && go test ./... -count=1`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add ui.go ui_test.go examples/helloworld/actions examples/helloworld/cmd examples/helloworld/gen
git commit -m "example: helloworld renders the component vocabulary in en and fr"
```

---

### Task 16: README — document the slice

**Files:**
- Modify: `README.md`

**Interfaces:** none.

- [ ] **Step 1: Move the two items out of "Not built yet"**

In the "Not built yet" paragraph, delete `the component/UI vocabulary, localization,` from the list, leaving the rest (manifest system, `sqlc`, `Mergeable`, blobs, crypto, WebAuthn, agents, preloaded `CLAUDE.md`/skill) intact.

- [ ] **Step 2: Add two bullets to the "Built" list**, after the `rastrillo.Serve` bullet, in the file's existing voice

Content they must state:

- **The component vocabulary (design doc §9)** — `page-header`, `box`, `list-bar`/`list-row`, `person`, `status-pill`/`badge`/`meter`/`mono`, `detail-list`, `alert`, the `field` family, `toggle-block`, `seg-tabs`, `form-foot`, `menu`, `confirm`, `modal-route`, `empty-state`, `bulk-select`, `pagination`, `help-link`, plus the `doc-open`/`doc-close` shell. Server-rendered Go, zero JavaScript, a token-based stylesheet served from the binary at `/_rastrillo/ui.css` (light/dark via `prefers-color-scheme` with a `data-theme` override), and vendored Lucide icons as in-code SVG constants — never a CDN. A hand-written `templates/<name>.html` defining the same name silently replaces a built-in: override-by-existence, the same one mechanism actions already use. **Not built:** the `Resource`-manifest generator that would compose these four canonical states for you — these are the partials it will compose, callable by name today from any hand-written template.
- **Localization (design doc §10)** — `Options.Locales`/`DefaultLocale`, catalogs at `locales/<code>.toml` (flat `key = "string"`, decoded by a page of hand-rolled code, no TOML dependency), one `html/template` set per locale bound to `{{T}}`/`{{Tf}}`, and a base English catalog for every string the built-in components render, so a single-locale app writes no catalog at all. Per-request resolution is URL path prefix → `Accept-Language` → the `rastrillo_locale` cookie; a matched prefix is stripped before routing, so `/fr/orders` and `/orders` are one route and locale switching is a plain link. `rastrillo generate --check` fails on a key the default locale's catalog has and a declared locale's does not.

- [ ] **Step 3: Update the "Try it" block**

The scaffold now writes more files; add a line after the existing `rastrillo new myapp` example noting that the scaffolded app comes with `locales/en.toml` and `templates/index.html` on the built-in vocabulary, and that adding `locales/fr.toml` plus `"fr"` to `Options.Locales` gives every route a `/fr/...` URL.

- [ ] **Step 4: Update the "Live" section**

Add one sentence: `helloworld.dev.oncarlos.com` now renders through the component vocabulary and is available in English and French (`/fr/`), and will show that from the next ship.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: README covers the component vocabulary and localization"
```

---

## Manual end-to-end verification (session lead, after review)

Not a subagent task — run in the session once every task above is green.

1. `cd /tmp/claude-1001/rastrillo-comp && go run ./cmd/rastrillo new /tmp/claude-1001/scaffoldcheck` — confirm it writes `embed.go`, `locales/en.toml`, `templates/index.html` and reports the generated route.
2. In `examples/helloworld`, run the built binary and load `/` and `/fr/` in a real browser with **JavaScript disabled**. Both must render fully; the locale switch links must work.
3. In the browser devtools, toggle the OS colour scheme and confirm the page follows it; then set `<html data-theme="light">` while the OS is dark and confirm the override wins.
4. Confirm the network panel shows exactly two requests: the document and `/_rastrillo/ui.css`. Any third request means something reached outside the binary.
5. Delete a key from `examples/helloworld/locales/fr.toml`, run `go run ./cmd/rastrillo generate --check examples/helloworld`, confirm it fails and names the key. Restore it.

---

## Self-review notes

- **Spec coverage.** §9's component list is covered name-for-name (Tasks 8–10), plus the token skin (Task 5), the zero-JS baseline (asserted in every component test), and override-by-existence for templates (Task 6). §10 is covered end to end: catalogs (Tasks 1, 6), base English catalog (Task 6), layered fallback (Task 2), `Options` declaration (Task 7), per-request resolution (Task 3), `{{T}}`/`{{Tf}}` per-locale binding (Task 6), zero-JS switching (Task 6), and the completeness gate (Tasks 11, 12). §9's manifest-driven four-state generation and §10's generated manifest keys are the two pieces deliberately deferred, both stated in the Context section with their reason.
- **Type consistency.** `Catalog`, `Locales`, `NewLocales`, `UI`, `UIOptions`, `NewUI`, `Render`, `RenderTo`, `LocaleFrom`, `T`, `Tf`, `Stylesheet`, `StylesheetPath`, `MissingKeys`, `dict`, `interpolate` are each defined in exactly one task and used with those exact signatures everywhere else. `buildHandler` is introduced and used only in Task 7.
- **Known ordering constraint.** Task 15 adds `RenderTo` to `ui.go` because it is the first caller that needs it. If an implementer prefers, it can be folded into Task 6 instead — the signature is identical either way.
