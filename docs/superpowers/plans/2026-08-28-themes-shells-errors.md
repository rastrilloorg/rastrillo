# Themes, Shells and Error Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split colour out of tokens.css into three WCAG-gated themes (ink/teal/warm), ship three layout shells (column/topbar/sidebar) with `rastrillo new --theme/--shell`, and give every app styled, localised, in-shell 404/403/422/500/503 pages with panic recovery.

**Architecture:** Themes are small colour-only CSS files embedded in `ui` (`ThemeCSS`); `tokens.css` keeps everything structural. Shells are embedded layout templates (`ui.Layout`) plus `rst-shell-*` class idioms. Error pages are an `error-page` partial rendered through a new `Ctx.ErrorPage`/`Options.ErrorPage` seam that `view.Fail`/`view.NotFound`/`view.Forbidden` and a new panic-recovery wrapper in `Serve` all call; copy comes from `rastrillo.ui.error_*` keys in all twelve base catalogs.

**Tech Stack:** Go stdlib + html/template, embedded CSS/TOML, the existing `internal/catalog` decoder, `cmd/rastrillo` scaffold.

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` §1, §2, §4b (+§0). This is PR 2 of §6.

## Global Constraints

- Locale codes, verbatim: `en ga zh-Hans es hi pt bn ru ja yue vi ar`. Every base catalog holds exactly the `en` key set (gated).
- No user-visible string hardcoded in a partial or shell: `{{T "rastrillo.ui.<key>"}}` only.
- Theme names: `ink`, `teal`, `warm` — ink first, the default, byte-identical to today's palette. Every theme passes `TestThemeTokenContrastMeetsWCAG`'s 26 pairs; the three theme files declare identical custom-property sets.
- Shell names: `column` (default; today's layout), `topbar`, `sidebar`.
- `go test . ./ui/ ./internal/... ./cmd/...` and each example suite (`examples/blog`, `examples/tickets`, `examples/notes`) stay green. Any `ui/tokens.css` or theme change is copied byte-identical to every `examples/*/static` that vendors it, and the examples' vendored-pin tests grow a `theme.css` line.
- `SKILL.md` ≤ 18,000 bytes (currently 15,135).
- `internal/docsite`'s symbol gate: every new exported symbol gets a reference-page entry.
- Never `git merge` to main; never bare `git stash`. Build with `GOFLAGS=-mod=mod`; if the Go build cache is read-only use `GOCACHE=$TMPDIR/gocache`.
- Error pages never reveal the error: no message, no stack, no path. A 500 shows only a short `Ref`.
- Rulings already made (ledger these as context, do not revisit): the switcher partial keeps `rst-dropdown rst-locale` (spec §2.4 amended in Task 10); generated-actions/plugin adoption of the error helpers is deferred to PR 3 (spec §4b note in Task 10).

---

## File map

| File | Responsibility |
|---|---|
| `ui/themes/{ink,teal,warm}.css` | colour+font per theme (create) |
| `ui/tokens.css` | loses its three colour blocks; gains shell + error-page classes; logical-properties pass |
| `ui/ui.go` | `ThemeNames`, `ThemeCSS`, `LayoutNames`, `Layout`, embeds |
| `ui/layouts/{column,topbar,sidebar}.html` | the shells (create) |
| `ui/partials/error-page.html` | the error partial (create) |
| `ui/contrast_test.go` | gate reads `ThemeCSS(name)` per theme; parity gate |
| `ui/ui_test.go` | shell samples, layout tests, error-page test, partial count 29→30 |
| `locales/*.toml` | +14 `error_*` keys ×12 |
| `basecatalog_test.go` | `IsBaseKey` assertions (debt) |
| `ctx.go`, `serve.go` | `Ctx.ErrorPage`, `Options.ErrorPage`, panic recovery |
| `view/view.go` (+test) | ref minting, `Fail` rework, `NotFound`, `Forbidden` |
| `cmd/rastrillo/new.go` (+test) | `--theme`, `--shell`, theme.css write, ui.Funcs wiring, errors.html |
| `examples/*/static`, `examples/*` layouts/pins | theme.css vendored + linked |
| docs: `templates.md`, `cli.md`, `getting-started.md`, `app-shape.md`, `reference/{ui,rastrillo,view}.md`, `SKILL.md`, spec §2.4/§4b | Task 10 |

---

### Task 1: Split the ink theme out of tokens.css

**Files:** Create `ui/themes/ink.css`; modify `ui/ui.go`, `ui/tokens.css`, `ui/contrast_test.go`, `ui/ui_test.go`.

**Interfaces (produces):**
```go
func ThemeNames() []string                  // {"ink", "teal", "warm"} once Task 2 lands; {"ink"} now
func ThemeCSS(name string) ([]byte, bool)   // embedded ui/themes/<name>.css
```

- [ ] **Step 1: Failing tests.** In `ui/ui_test.go` add:

```go
func TestThemesDeclareIdenticalTokenSets(t *testing.T) {
	names := ThemeNames()
	if len(names) == 0 || names[0] != "ink" {
		t.Fatalf("ThemeNames = %v; ink must exist and come first", names)
	}
	want := themePropSet(t, "ink")
	if len(want) == 0 {
		t.Fatal("ink declares no --rst- properties")
	}
	for _, n := range names[1:] {
		got := themePropSet(t, n)
		for p := range want {
			if !got[p] {
				t.Errorf("theme %s is missing %s", n, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("theme %s declares %s, which ink does not", n, p)
			}
		}
	}
}

// themePropSet: every --rst-* name declared anywhere in the theme file.
func themePropSet(t *testing.T, name string) map[string]bool {
	t.Helper()
	css, ok := ThemeCSS(name)
	if !ok {
		t.Fatalf("ThemeCSS(%q) missing", name)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:`).FindAllStringSubmatch(string(css), -1) {
		out[m[1]] = true
	}
	return out
}

func TestTokensCSSHasNoColourLiterals(t *testing.T) {
	// After the split, structural tokens.css may reference colours only
	// via var(); bare hex in a *declaration value* means a colour leaked
	// back in. Exempt: none — rgba() shadows live in the themes now too.
	for i, line := range strings.Split(string(TokensCSS()), "\n") {
		if strings.Contains(line, "#") && regexp.MustCompile(`:\s*[^;]*#[0-9a-fA-F]{3,6}`).MatchString(line) {
			t.Errorf("tokens.css line %d declares a colour literal: %s", i+1, strings.TrimSpace(line))
		}
	}
}
```

`regexp` and `strings` are already imported in ui_test.go; verify.

- [ ] **Step 2: Run** `GOFLAGS=-mod=mod go test ./ui/ -run 'Themes|TokensCSSHasNoColour' -v` — expect `undefined: ThemeNames`.

- [ ] **Step 3: Implement.** Move from `tokens.css` into `ui/themes/ink.css`, byte-for-byte values: the WCAG header table comment; the `:root` font/scale block's `--rst-font` line ONLY (spacing/radius/font-size stay structural — copy `--rst-font` into the theme and delete it from tokens.css); the `:root, :root[data-theme="light"]` colour block; the `@media (prefers-color-scheme: dark)` block; the `:root[data-theme="dark"]` block; and any rgba()/shadow token declarations those blocks hold. `ink.css` starts with a comment: `/* ink — rastrillo's default theme: iron-gall violet on cool-violet neutrals. Structure lives in tokens.css; this file is colour and type only. rastrillo new writes it once as static/theme.css. */`. In `ui/ui.go`:

```go
//go:embed themes/*.css
var themesFS embed.FS

var themeNames = []string{"ink", "teal", "warm"} // Task 2 adds the files; trim to what exists via init check? No — keep the slice matching the shipped files exactly; in this task it is []string{"ink"}.

func ThemeNames() []string { return append([]string(nil), themeNames...) }

func ThemeCSS(name string) ([]byte, bool) {
	b, err := fs.ReadFile(themesFS, "themes/"+name+".css")
	return b, err == nil
}
```

- [ ] **Step 4: Repoint the contrast gate.** In `ui/contrast_test.go`, `themeTokens` currently parses `TokensCSS()`; change it to take a theme name and parse `ThemeCSS(name)` (same three block headers), and make `TestThemeTokenContrastMeetsWCAG` and `TestBothThemesDeclareEveryColourToken` (find it; adapt similarly) iterate `ThemeNames()` with `t.Run(name+"/"+blockName, ...)`. The math and the 26-pair table do not change.

- [ ] **Step 5:** `GOFLAGS=-mod=mod go test ./ui/` — everything green. Note styles: any tokens.css rule that referenced a moved token still uses `var(--rst-*)` so nothing else changes. Examples are updated in Task 3, so their pins are red right now — do NOT run example suites in this task; note it in the report instead.

- [ ] **Step 6: Commit** `ui: colour moves out of tokens.css — themes/ink.css, ThemeNames, ThemeCSS`.

---

### Task 2: The teal and warm themes

**Files:** Create `ui/themes/teal.css`, `ui/themes/warm.css`; modify `ui/ui.go` (`themeNames`).

The palettes below were tuned against the gate's exact WCAG formula and pass all 26 pairs in both light and dark. Use them verbatim; if a pair still fails (a transcription slip), adjust the failing token minimally and re-run the gate.

- [ ] **Step 1:** Set `themeNames = []string{"ink", "teal", "warm"}`. Run `go test ./ui/ -run Themes` — parity gate fails (files missing).

- [ ] **Step 2: Write the files.** Same block structure as ink (light `:root, :root[data-theme="light"]`, dark `@media` + `:root[data-theme="dark"]`), same property set (the parity gate enforces it). Compute each file's header table with the gate itself (`go test ./ui/ -run Contrast -v` prints ratios on failure; or transcribe from a quick script) — the header comment lists at minimum the tightest pair per block.

`teal.css` — amadan's teal on green-grey neutrals; `--rst-font` gains a monospace-leaning UI stack: `--rst-font: "SF Mono", ui-monospace, "Cascadia Code", Menlo, Consolas, system-ui, sans-serif;`

```
LIGHT: bg #f0f4f2  surface #ffffff  surface-2 #f7faf8  line #dee6e1  line-strong #788f85
       text #16211c  text-muted #485a52  text-faint #54685f
       accent #0b6e63  accent-strong #08544c  accent-soft #e2f1ed  on-accent #ffffff
       tone-neutral #41544c/#e3ebe7  positive #17603f/#d7ecdf  warning #74490e/#f4e4c9  negative #93262f/#f9dcdd
DARK:  bg #0b100e  surface #131a17  surface-2 #0f1512  line #25302b  line-strong #657d73
       text #e7efeb  text-muted #9fb3aa  text-faint #8fa69b
       accent #5fd3c2  accent-strong #83e0d2  accent-soft #15342e  on-accent #052b25
       tone-neutral #b4c6bd/#25322c  positive #74dba8/#16301f  warning #eab86c/#322512  negative #f58c95/#35191e
```

`warm.css` — messenger's paper neutrals, rust accent; `--rst-font` stays the system stack:

```
LIGHT: bg #efe3d6  surface #fbf6ee  surface-2 #f5ecdf  line #ddcdba  line-strong #8a7660
       text #241c12  text-muted #5b4c39  text-faint #675741
       accent #8f3c1f  accent-strong #732f16  accent-soft #f3e0d2  on-accent #ffffff
       tone-neutral #544636/#e7dac9  positive #175f38/#d5e8d5  warning #6f4408/#f2e0bd  negative #8f2430/#f6d9d3
DARK:  bg #171009  surface #211810  surface-2 #1c140c  line #382c1f  line-strong #84705a
       text #f0e7da  text-muted #c0ac93  text-faint #b3a08a
       accent #eb9c6e  accent-strong #f2b48d  accent-soft #3a2415  on-accent #2b1507
       tone-neutral #c7b8a4/#332a1e  positive #8ed6a4/#1a2f1e  warning #e8b968/#33250f  negative #f79b91/#381a15
```

Shadow tokens (`--rst-shadow-pop`): copy ink's rgba values per block.

- [ ] **Step 3:** `GOFLAGS=-mod=mod go test ./ui/ -run 'Contrast|Themes' -v` — all three themes, both dark blocks, green.
- [ ] **Step 4: Commit** `ui: teal and warm themes, WCAG-gated`.

---

### Task 3: Scaffold and examples carry theme.css

**Files:** Modify `cmd/rastrillo/new.go`, `cmd/rastrillo/new_test.go`; every `examples/*/static` that has `tokens.css` gains `theme.css` (= `ui.ThemeCSS("ink")`) and a fresh `tokens.css` copy; each such example's layout template links it; each example's `vendored_test.go`-equivalent pin gains a line.

- [ ] **Step 1: Failing test.** In `new_test.go`, extend the scaffold-files assertion list with `filepath.Join("static", "theme.css")` and add:

```go
func TestNewThemeFlag(t *testing.T) {
	// --theme=teal writes teal's bytes as static/theme.css; unknown
	// themes fail before any file is created.
	dir := t.TempDir()
	// (follow this file's existing pattern for invoking runNew in a temp cwd)
	if err := runNewIn(t, dir, "--theme=teal", "app"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "app", "static", "theme.css"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := ui.ThemeCSS("teal")
	if !bytes.Equal(got, want) {
		t.Error("static/theme.css is not the teal theme")
	}
	if err := runNewIn(t, dir, "--theme=nope", "app2"); err == nil {
		t.Fatal("unknown theme must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "app2")); !os.IsNotExist(err) {
		t.Error("a failed new must not leave a directory behind")
	}
}
```

If `new_test.go` has no `runNewIn`-shaped helper, adapt to however its existing tests invoke `runNew` (they do — find the pattern and follow it; add the helper if extraction is cleaner).

- [ ] **Step 2: Implement.** In `runNew`: `theme := fset.String("theme", "ink", "colour theme: "+strings.Join(ui.ThemeNames(), ", "))`; validate via `ui.ThemeCSS(*theme)` before creating anything (the resolve-before-scaffold rule already in that function); write the bytes at `filepath.Join(appDir, "static", "theme.css")`. In `layoutTemplate`, after the tokens.css link add `<link rel="stylesheet" href="{{asset "static/theme.css"}}">`. In `vendoredTestTemplate`'s map add `"theme.css": mustTheme("%[3]s")` — simpler: emit the theme name as a constant in the generated test and use `ui.ThemeCSS(vendoredTheme)`; the generated test text becomes:

```go
vendoredTheme := %[3]q // the --theme this app was scaffolded with
themeCSS, _ := ui.ThemeCSS(vendoredTheme)
for name, lib := range map[string][]byte{
	"tokens.css":   ui.TokensCSS(),
	"theme.css":    themeCSS,
	"rastrillo.js": ui.ShimJS(),
	"select.js":    ui.SelectJS(),
} {
```

(adjust the format verbs; the template already uses `%[1]s`/`%[2]s`).

- [ ] **Step 3: Examples.** For each of `examples/blog`, `examples/tickets` (the two with `static/tokens.css`): `cp ui/tokens.css <ex>/static/tokens.css`; write `ui/themes/ink.css` bytes to `<ex>/static/theme.css` (`cp ui/themes/ink.css <ex>/static/theme.css`); find the example's layout template (`grep -rn "tokens.css" <ex>/templates <ex>/internal`) and add the `theme.css` link beside it; find the example's vendored-pin test (`grep -rln TestVendoredAssetsMatchTheLibrary <ex>`) and add the `theme.css` line (`ui.ThemeCSS("ink")` — handle the two-value return).

- [ ] **Step 4:** `GOFLAGS=-mod=mod go test ./cmd/... ./ui/` and both example suites (blog, tickets; notes has no static dir but run it anyway) — green.
- [ ] **Step 5: Commit** `rastrillo new --theme; scaffold and examples link theme.css`.

---

### Task 4: Shell class idioms and the logical-properties pass

**Files:** Modify `ui/tokens.css`, `ui/ui_test.go` (styleguideSamples).

- [ ] **Step 1: Samples first** (they drive the class↔CSS gate). Add to `styleguideSamples`:

```go
	"shell-topbar": `<div class="rst-shell-topbar">
  <a class="rst-skip" href="#main">Skip to content</a>
  <header class="rst-shell__bar"><a class="rst-shell__brand" href="/">Notes</a>
    <nav class="rst-shell__nav"><a href="/" aria-current="page">Home</a><a href="/archive">Archive</a></nav>
    <details class="rst-dropdown rst-shell__account"><summary>Account<span class="rst-caret" aria-hidden="true">…</span></summary>
      <div class="rst-dropdown__menu"><a href="/settings">Settings</a></div></details>
  </header>
  <main class="rst-page" id="main">…</main>
  <footer class="rst-shell__foot">Made with rastrillo</footer>
</div>`,
	"shell-sidebar": `<div class="rst-shell-sidebar">
  <a class="rst-skip" href="#main">Skip to content</a>
  <details class="rst-shell__chrome"><summary>Menu</summary></details>
  <aside class="rst-shell__rail"><a class="rst-shell__brand" href="/">Notes</a>
    <nav class="rst-shell__nav"><span class="rst-shell__group">Work</span><a href="/" aria-current="page">Dashboard</a><a href="/reports">Reports</a></nav>
  </aside>
  <main class="rst-shell__main" id="main"><div class="rst-page">…</div></main>
</div>`,
```

(Replace `…` in the summary with an `iconSVG("chevron-down")` call if the sample map's existing entries do; follow their style.) Run the styleguide render + class↔css tests; they fail on the missing classes.

- [ ] **Step 2: The CSS.** Add a `/* ── Shells ── */` section to tokens.css using **logical properties throughout** (`inset-inline-start`, `padding-inline`, `margin-inline-start`, `border-inline-end`): `.rst-skip` (visually hidden until focus — position absolute, on focus a small surface chip top-start); `.rst-shell-topbar .rst-shell__bar` (flex, gap, padding-inline, border-block-end 1px var(--rst-line), background var(--rst-surface)); `.rst-shell__brand` (font-weight 650, color inherit, no underline); `.rst-shell__nav` (flex, gap; `a` muted, `a[aria-current]` text colour + 2px accent border-block-end); `.rst-shell__account` (margin-inline-start auto); `.rst-shell__foot` (muted, small, padding, border-block-start); `.rst-shell-sidebar` (≥800px: grid `grid-template-columns: 15rem 1fr`; `.rst-shell__rail` border-inline-end, background var(--rst-surface), padding, sticky top 0 height 100vh overflow auto; `.rst-shell__group` faint uppercase xs with margin-block; `.rst-shell__nav` vertical — rail links block, padding, radius, hover accent-soft, `[aria-current]` accent-soft + accent text; `.rst-shell__chrome` hidden ≥800px); <800px: rail hidden unless `.rst-shell__chrome[open] + .rst-shell__rail` (chrome bar is a `<details>` strip: summary styled as a bar with border-block-end; rail becomes static full-width when open). `.rst-shell__main` min-width 0.

- [ ] **Step 3: The existing-CSS logical pass.** `grep -nE "margin-left|margin-right|padding-left|padding-right|\bleft:|\bright:|text-align: left|text-align: right|border-left|border-right" ui/tokens.css` — convert each hit to its logical equivalent (`margin-inline-start/-end`, `padding-inline-start/-end`, `inset-inline-start/-end`, `text-align: start/end`, `border-inline-start/-end`) EXCEPT: dropdown/row-menu panels positioned `right: 0` become `inset-inline-end: 0`; anything inside a `@keyframes` or transform stays physical. Re-run the full ui suite after.

- [ ] **Step 4:** Copy tokens.css to the two examples again (pins). `go test ./ui/` + examples green.
- [ ] **Step 5: Commit** `ui: shell class idioms; tokens.css goes logical for RTL`.

---

### Task 5: `ui.Layout` and the three shells

**Files:** Create `ui/layouts/{column,topbar,sidebar}.html`; modify `ui/ui.go`, `ui/ui_test.go`.

**Interfaces (produces):**
```go
func LayoutNames() []string             // {"column","topbar","sidebar"}
func Layout(name string) ([]byte, bool) // raw layout.html text for the scaffold to write
```

Every layout defines `layout`, executes `{{template "content" .}}`, and declares overridable blocks with working defaults: `title`, `brand` (default: `{{block "brand" .}}<a class="rst-shell__brand" href="/">{{T "rastrillo.ui.shell_menu"}}</a>{{end}}` — no: brand default is the bare app link with text "Home"? A brand needs a name the framework cannot know. Default brand text: `{{T "rastrillo.ui.shell_menu"}}` is wrong. Use an empty-safe default: `<a class="rst-shell__brand" href="/">&#8203;</a>` is worse. **Decision: the default `brand` block renders `<a class="rst-shell__brand" href="/">Home</a>` via a new key** — no new key: reuse nothing, hardcoding "Home" violates §0. Add `rastrillo.ui.shell_home` = "Home" to the key set in Task 7's catalog edit (all twelve languages; it is the twelve-locale word for Home). `nav` (default empty), `account` (default empty), `locale` (default empty), `foot` (topbar only; default empty). The `<html>` tag: `<html lang="{{block "lang" .}}en{{end}}"{{block "dir" .}}{{end}}>` — apps override `lang`/`dir` blocks or leave defaults; simpler and struct-data-safe than reading fields.

The `<head>` in all three (matching today's scaffold layout): meta charset, viewport, `{{block "title" .}}Hello{{end}}` in `<title>`, stylesheet links `{{asset "static/tokens.css"}}` and `{{asset "static/theme.css"}}`, `<script defer {{asset "static/rastrillo.js"}}>`, `{{iconAssets}}`, `<script defer {{asset "static/select.js"}}>`.

Bodies: `column` = today's `<main>{{template "content" .}}</main>` wrapped in nothing (add `<a class="rst-skip" href="#main">{{T "rastrillo.ui.shell_skip"}}</a>` and `id="main"`); `topbar` and `sidebar` = the Task 4 sample structures with the blocks slotted in (`{{block "nav" .}}{{end}}` inside `.rst-shell__nav`, etc.) and skip-link text/summary labels through `T`: `shell_skip`, `shell_menu`, `shell_account`.

- [ ] **Step 1: Failing tests** in `ui/ui_test.go`:

```go
func TestLayoutsParseAndRender(t *testing.T) {
	if got := LayoutNames(); !reflect.DeepEqual(got, []string{"column", "topbar", "sidebar"}) {
		t.Fatalf("LayoutNames = %v", got)
	}
	for _, name := range LayoutNames() {
		t.Run(name, func(t *testing.T) {
			src, ok := Layout(name)
			if !ok {
				t.Fatal("missing")
			}
			tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
				"asset":      func(p string) string { return "/" + p },
				"iconAssets": func() template.HTML { return "" },
			}).Parse(string(src)))
			template.Must(tmpl.Parse(`{{define "content"}}CONTENT-SENTINEL{{end}}`))
			var b strings.Builder
			if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
				t.Fatal(err)
			}
			out := b.String()
			for _, want := range []string{"CONTENT-SENTINEL", "static/theme.css", "static/tokens.css", `id="main"`, "Skip to content"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s: missing %q", name, want)
				}
			}
			if strings.Contains(out, "rastrillo.ui.") {
				t.Errorf("%s: an unresolved catalog key leaked into the page", name)
			}
		})
	}
}
```

- [ ] **Step 2:** RED (undefined `LayoutNames`), implement (`//go:embed layouts/*.html`, same shape as ThemeCSS), GREEN.
- [ ] **Step 3: Commit** `ui: Layout/LayoutNames — the column, topbar and sidebar shells`.

---

### Task 6: `rastrillo new --shell` and ui.Funcs in the scaffold

**Files:** Modify `cmd/rastrillo/new.go`, `cmd/rastrillo/new_test.go`.

- [ ] **Step 1: Failing test:** `TestNewShellFlag` mirroring `TestNewThemeFlag`: `--shell=topbar` writes `templates/layout.html` == `ui.Layout("topbar")`; default is `column`; unknown shell fails clean. Also assert the scaffolded app's `render.go`/`main.go` template-func wiring still compiles: the existing `TestNewAppBuildsAndServes`-style test (find it) covers this — make sure it still passes with the new funcs.

- [ ] **Step 2: Implement.** `shell := fset.String("shell", "column", "layout shell: "+strings.Join(ui.LayoutNames(), ", "))`; validate before scaffolding; the `layoutTemplate` const is replaced by `ui.Layout(*shell)` (delete the const; `column` reproduces today's layout — the Task 5 file is the source of truth now). The scaffold's `init()` func-map changes to:

```go
Funcs(ui.Funcs(ui.WithIcons(icons.Icon, icons.Assets))).
Funcs(template.FuncMap{"asset": assets.Path}).
ParseFS(ui.Templates(), "*.html")  // partials available to every page
```

then `ParseFS(appFS, "templates/layout.html", "templates/"+name+".html")` as today. Check `ui.Funcs` already provides `icon`/`iconAssets` via WithIcons (it does — funcs.go) so the old two entries drop. The generated file imports `amadan.net/rastrillo/rastrillo/ui`.

- [ ] **Step 3:** `go test ./cmd/...` green (the scaffold-compile test proves the generated app builds with ui wired). **Commit** `rastrillo new --shell; scaffolded templates get ui.Funcs and the partials`.

---

### Task 7: The error keys, in all twelve catalogs

**Files:** Modify `locales/*.toml` ×12, `basecatalog_test.go`.

Keys (append to every catalog, en order): `error_404_title/_body`, `error_403_title/_body`, `error_422_title/_body`, `error_500_title/_body`, `error_503_title/_body`, `error_generic_title/_body`, `error_back`, `error_home`, `error_ref`, `shell_home`. All prefixed `rastrillo.ui.`.

`en` values (verbatim; spec §4b.1's table):
- 404: "We can't find that page" / "The link may be out of date, or the page may have moved. Check the address, or go back to the start."
- 403: "You can't see this" / "Your account doesn't have access here. If you think it should, ask whoever runs this site."
- 422: "That didn't go through" / "Something in what was sent wasn't right. Go back and try again; nothing was saved."
- 500: "Something went wrong on our side" / "It's not you. The problem has been recorded. Try again in a moment; if it keeps happening, quote the reference below."
- 503: "We're briefly unavailable" / "The site is being updated or is busy. Try again in a minute."
- generic: "Something's not right" / "That request couldn't be completed. Go back and try again."
- `error_back` = "Go back", `error_home` = "Start page", `error_ref` = "Reference: {ref}", `shell_home` = "Home".

- [ ] **Step 1:** The key-set gate (`TestBaseCatalogsShareOneKeySet`) fails as soon as `en` gains the keys — add them to `en.toml` first, run it RED against the other eleven.
- [ ] **Step 2:** Translate into the other eleven catalogs yourself — faithful, plain-register translations of the en values; keep `{ref}` verbatim in every `error_ref`; keep each file's key order matching en. You are drafting for files whose headers already say machine-drafted.
- [ ] **Step 3: The `IsBaseKey` debt.** In `basecatalog_test.go` add:

```go
func TestIsBaseKey(t *testing.T) {
	if !IsBaseKey("rastrillo.ui.error_404_title") || !IsBaseKey("rastrillo.ui.cancel") {
		t.Error("shipped keys must report true")
	}
	if IsBaseKey("rastrillo.ui.nope") || IsBaseKey("app.title") {
		t.Error("unshipped keys must report false")
	}
}
```

- [ ] **Step 4:** `go test .` green. **Commit** `Base catalogs: the error-page and shell_home keys, twelve languages`.

---

### Task 8: The error-page partial and the plumbing

**Files:** Create `ui/partials/error-page.html`; modify `ui/tokens.css` (+example copies), `ui/ui_test.go` (30 partials), `ctx.go`, `serve.go`, `view/view.go`, `view/view_test.go` (create if absent), `serve_test.go` or `serve_router_test.go` (panic recovery).

**Interfaces (produces):**
```go
// ctx.go
ErrorPage func(w http.ResponseWriter, r *http.Request, status int, ref string) // field on Ctx
// serve.go
ErrorPage func(w http.ResponseWriter, r *http.Request, status int, ref string) // field on Options; used by panic recovery
// view
func NewRef() string                                    // 6 lowercase base32 chars from 4 random bytes
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, what string, err error) // NOTE: gains r
func NotFound(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
func Forbidden(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
```

`view.Fail` gains the `*http.Request` parameter — find every existing caller (`grep -rn "view.Fail" --include=*.go .`) and update them in this task; if a caller has no request in scope, pass `nil` and `Fail` must tolerate a nil `r` (fall back to plain text). Behaviour: mint `ref := NewRef()`; log `what` with `err` AND `ref`; if `ctx.ErrorPage != nil && r != nil` call it with (500, ref), else today's `http.Error`. `NotFound`/`Forbidden`: no ref, statuses 404/403, same nil-fallbacks (`http.NotFound` / plain 403). JSON: when `r` has `Accept: application/json`, all three write `{"status":<n>,"ref":"<ref>"}` (empty ref omitted) instead of HTML — implement in one unexported helper the three share.

Panic recovery in `buildHandler`: wrap the final handler (inside securityHeaders) in:

```go
func recoverPanics(errorPage func(http.ResponseWriter, *http.Request, int, string), logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				ref := view? // NO — view imports rastrillo; the ref helper must live in the root package.
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

**Import direction:** `view` imports `rastrillo`, so the ref minter lives in the ROOT package as `rastrillo.NewRef()` and `view` re-uses it (`func NewRef() string { return rastrillo.NewRef() }` — no: don't re-export; view calls `rastrillo.NewRef` directly and the interface list above corrects to `rastrillo.NewRef`). Recovery logs the stack (`debug.Stack()`) with the ref, then, if `opts.ErrorPage != nil` and nothing is written yet, calls it with (500, ref); double-write is acceptable-risk (headers may be gone mid-stream — guard with a `wrote` check via a thin ResponseWriter wrapper only if serve.go already has one; otherwise document that a mid-stream panic yields a broken page, same as net/http today).

The partial (`error-page`, count 29→30 in the defined-list test):

```html
{{define "error-page"}}<div class="rst-error">
  <p class="rst-error__status">{{.Status}}</p>
  <h1 class="rst-error__title">{{if .Title}}{{.Title}}{{else}}{{T (printf "rastrillo.ui.error_%d_title" .Status)}}{{end}}</h1>
  <p class="rst-error__body">{{if .Body}}{{.Body}}{{else}}{{T (printf "rastrillo.ui.error_%d_body" .Status)}}{{end}}</p>
  <p class="rst-error__cta"><a class="rst-btn rst-btn--primary" href="{{or .HomeHref "/"}}">{{T "rastrillo.ui.error_home"}}</a></p>
  {{with .Ref}}<p class="rst-error__ref rst-mono">{{Tf "rastrillo.ui.error_ref" "ref" .}}</p>{{end}}
</div>{{end}}
```

Statuses without keys (any not in {404,403,422,500,503}) must fall to `error_generic_*`: `T` returns the key itself on a miss, so the partial's template comment instructs callers to pass only the five statuses or Title/Body; ALSO make the partial itself guard: compute `{{$known := or (eq .Status 404) (eq .Status 403) (eq .Status 422) (eq .Status 500) (eq .Status 503)}}` and use `error_generic_*` keys when not known. Check `Tf` exists in ui.Funcs — it does not (Funcs registers `T`, `dict`, `list`, `icon`). EITHER register `Tf` in ui.Funcs (small, useful, do it — follow `T`'s WithT plumbing: default interpolates `{name}` via a tiny local replacer) OR render the ref line as `{{T "rastrillo.ui.error_ref"}}` with the caller pre-interpolating. **Do the former**; add its test. "Go back" (`error_back`): a plain `<a href="javascript:history.back()">` violates no-JS; instead render Back only when `.BackHref` is set (the app passes a same-site Referer it validated) — keys stay, contract documented in the partial comment.

CSS: `.rst-error` (centred column, max-width 28rem, margin-block auto-ish, padding-block 4rem), `__status` (faint, xs, letterspaced), `__title` (h1 scale), `__body` (muted), `__ref` (faint, monospace, margin-block-start 2rem). Copy tokens.css to examples after.

- [ ] Steps: failing partial-render test (all five statuses + generic 418 + Ref + custom Title) → RED → implement partial+CSS+Funcs Tf → view tests (Fail logs ref and renders through a recorded ErrorPage; nil ctx.ErrorPage falls back; JSON accept; NotFound/Forbidden statuses; NewRef shape `^[a-z2-7]{6}$` roughly — assert length 6 and no error) → serve test (a Mux route that panics: with Options.ErrorPage set, response is 500 and the page body, and the process does not crash; without, 500 plain) → full suite → **Commit** `Error pages: the partial, Ctx/Options.ErrorPage, view helpers, panic recovery`.

---

### Task 9: The scaffold's errors.html and wiring

**Files:** Modify `cmd/rastrillo/new.go` (+test).

- [ ] Scaffold gains `templates/errors.html`:

```html
{{define "content"}}{{template "error-page" dict "Status" .Status "Ref" .Ref}}{{end}}
```

and the generated `render.go` gains (adapt names to the actual generated file — read the current template consts first):

```go
// errorPage renders the error-page partial inside the layout — wire it
// into your Ctx/handlers as needed; the scaffold serves it for panics
// via Options.ErrorPage in main.go.
func errorPage(w http.ResponseWriter, r *http.Request, status int, ref string) {
	w.WriteHeader(status)
	render(w, "errors", map[string]any{"Status": status, "Ref": ref})
}
```

`"errors"` joins the `pages` init loop's name list, and the generated `main.go` sets `opts.ErrorPage = <app>.ErrorPage` (export it from the internal package — `ErrorPage` calling `errorPage`; keep one exported name, drop the unexported indirection if simpler). New scaffold-level test: build the scaffolded app (the existing compile-and-serve test), GET a path that panics? The scaffold has no panicking route — instead assert the files exist and the app compiles, and add one runtime assertion: request an unknown static-asset-era path? Keep it to: `errors.html` in the file list + the compile test green.

- [ ] **Commit** `scaffold: errors.html and Options.ErrorPage wiring`.

---

### Task 10: Docs, SKILL.md, spec amendments

**Files:** `docs/site/templates.md`, `docs/site/cli.md`, `docs/site/getting-started.md`, `docs/site/app-shape.md`, `docs/site/reference/ui.md`, `docs/site/reference/rastrillo.md`, `docs/site/reference/view.md`, `SKILL.md`, `docs/superpowers/specs/2026-08-28-design-system-design.md`.

- [ ] `templates.md`: a **Themes** section (the three, what a theme file holds, swap = replace static/theme.css, the WCAG gate), a **Shells** section (the three, the block contract with a worked topbar override example, the shell classes, the locale block), an **Error pages** section (the partial contract, the five statuses + generic, the Ref line, BackHref rule). `cli.md`: `--theme`/`--shell` rows beside `--icons`. `getting-started.md`/`app-shape.md`: mention the flags and errors.html where the scaffold's files are listed (read first; smallest true edit). Reference pages: every new exported symbol (`ThemeNames`, `ThemeCSS`, `LayoutNames`, `Layout` in ui.md; `Options.ErrorPage`, `NewRef` in rastrillo.md; `NotFound`, `Forbidden`, changed `Fail` signature in view.md) — the docsite symbol gate is the arbiter.
- [ ] `SKILL.md`: in §1, one addition to the scaffold sentence (`rastrillo new --theme=ink|teal|warm --shell=column|topbar|sidebar`); in §5 or §7 a two-line error-page note (view.Fail/NotFound/Forbidden render styled in-shell pages; a 500 shows a ref that matches the log line; panics recover to the 500 page). Budget stays ≤18,000 (~2.8KB headroom exists).
- [ ] Spec amendments, each one sentence marked "(as built, 2026-08-28)": §2.4 — the switcher rides `rst-dropdown rst-locale`, not `rst-shell__menu`; §3.3 — `NewLocales` signature unchanged, framework catalogs read internally; §2.2 — `Dir` lives in the root package; §4b.2 — generated-actions and identity-plugin adoption of the view helpers moves to PR 3; §4b.1 — Back renders only from a caller-validated `BackHref`.
- [ ] All gates: root, ui, internal (docsite!), cmd, three examples, SKILL byte count. **Commit** `Docs: themes, shells, error pages; spec amended to as-built`.

---

### Task 11: PR

- [ ] Push `themes-shells-errors`; `gh pr create` titled "Themes, shells, and error pages" with a body summarising §1/§2/§4b delivery, the ruling that PR 3 adopts the view helpers in generated actions, and the theme palettes' WCAG gating; watch CI.

---

## Self-review

**Coverage:** spec §1.1–1.3 → Tasks 1–3; §2.1–2.3 → Tasks 4–6; §2.2 strings → Task 5 via `shell_*` keys (+`shell_home`, Task 7); §4b.1 → Tasks 7–8; §4b.2 → Tasks 8–9 with the PR-3 deferral amended into the spec (Task 10); §0 logical properties → Task 4; debts (IsBaseKey, spec amendments) → Tasks 7, 10. Not here by design: §4b's generated-actions sweep (PR 3), `FrameworkHas` consumption (shells read nothing per-locale — the `lang`/`dir` blocks are app-owned).

**Types:** `ThemeCSS(name) ([]byte, bool)` used in Tasks 1, 2, 3; `Layout(name) ([]byte, bool)` in Tasks 5, 6; `ErrorPage func(w, r, status int, ref string)` identical on Ctx and Options (Tasks 8, 9); `Fail` gains `r` and every caller updates in Task 8.

**Known tensions written into the tasks:** Task 1 leaves example pins red until Task 3 (stated); the partial's `Tf` registration is decided (register it); the brand-default question is decided (`shell_home`).
