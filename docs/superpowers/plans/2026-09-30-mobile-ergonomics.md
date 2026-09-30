# Mobile ergonomics (Part H) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On small or touch screens a rastrillo app gets 16px type and 44px tap targets, list rows that are clickable across their width, a shipped `row-menu` partial, a topbar menu that floats over the page instead of shoving it down, sidebar and console shells whose phone navigation is an index page with a back control instead of a hamburger drawer, and prerendered shell navigation through a `Speculation-Rules` header, with desktop density unchanged except for the three approved changes.

**Architecture:** Almost all of it is `ui/tokens.css`: one `@media (pointer: coarse), (max-width: 40rem)` query moves the type scale and sets tap floors; the whole-row overlay and the index/back views are ordinary rules keyed on attributes the layouts emit. Two blocks (`view`, `up`) are how a page tells its sidebar or console shell which view it is. Behaviour that CSS cannot do lives in small first-party scripts: `rastrillo.js` learns the topbar card's Escape and outside-click rules; a new vendored `shell.js` (with `shell.css`) adds the slide, history reuse and focus return. `rastrillo.Serve` sends the speculation-rules header and serves the rules file.

**Tech Stack:** Go 1.26, `html/template`, hand-written CSS (no preprocessor), dependency-free ES5-style IIFEs, chromedp through `harness` (`-tags browser`), axe-core for a11y, Node for the existing JS twin tests.

**Spec:** `docs/superpowers/specs/2026-09-30-mobile-ergonomics-design.md` (four review rounds, operator sign-offs recorded in its Decisions). Read it beside this plan: section references below (§1.3 and so on) are to it, and so are `P/…` citations (the approved prototype, `/home/paulca/.cache/tmp/mobile-proto/`).

## Global Constraints

- **Where commands run:** from the worktree root `/home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/mobile-ergonomics`, branch `mobile-ergonomics`. Every `git`, `go`, `node`, `jq` and `make` command runs with the Bash sandbox disabled (`dangerouslyDisableSandbox: true`). Export `GOFLAGS=-mod=mod` for every `go` command (three scratch-module packages fail on go.sum without it). The shared repo has a stale read-only `.git/config.lock`: do not touch it. Push with `git push origin mobile-ergonomics` (no `-u`); the amadan remote occasionally answers 503, so retry.
- **The task gate.** Every task ends GREEN on all three of these, run in this order, and a task is not done until they pass:
  1. `GOFLAGS=-mod=mod go vet ./... && make gofmt && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1` (this is the operator's gate line `go vet && gofmt -l . && go test`, with `make gofmt` standing in for `gofmt -l .` because `gofmt -l` exits 0 even when it prints, and the Makefile target fails on output and prunes `.build/tmp`);
  2. `make example-blog example-tickets` (the examples are separate modules with byte copies of `tokens.css`);
  3. `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -p 1 ./harness/ ./webauthn/ ./ui/ ./pow/ ./internal/designsystem/ ./auth/ -count=1` (the Makefile's `browser` package list, verbatim).
  Task 14 runs `make ci` whole. Run `gofmt -w` on every Go file you write before the gate: code blocks in this plan are not guaranteed column-aligned.
- **Browser tests:** `//go:build browser`, `RASTRILLO_CHROME=/usr/bin/chromium`, `TMPDIR=/var/tmp`. Never set `RASTRILLO_BROWSER_OPTIONAL`: a skip is not a pass. New browser files live in `./ui/`, `./harness/` or `./internal/designsystem/`, which the Makefile list already covers; do not add packages to that list.
- **Touch is a launch flag, not CDP emulation.** Measured on this machine's Chromium (2026-09-30): `Emulation.setTouchEmulationEnabled`, a mobile `setDeviceMetricsOverride` and `setEmulatedMedia` all leave `matchMedia("(pointer: coarse)")` false; `--blink-settings=primaryPointerType=2,availablePointerTypes=2,primaryHoverType=1,availableHoverTypes=1` makes it true. Task 2 adds `harness.WithCoarsePointer()`, and every touch leg asserts `matchMedia("(pointer: coarse)").matches` before it measures anything (spec §10's control).
- **Prerender in headless Chromium.** Measured: a `Speculation-Rules` header makes Chromium fetch the target with `Sec-Purpose: prefetch;prerender` on hover (mouse) and on pointer-down (coarse), but the prerender is never activated headless (the navigation is delivered as `deliveryType: "navigational-prefetch"`, `activationStart` 0), and enabling the CDP `Preload` domain disables prerendering outright (`PrerenderingDisabledByDevTools`). So the tests assert on the server-side `Sec-Purpose` request, never enable `Preload`, and the true prerender-activation leg of §10.3 moves to the by-hand list (Task 14).
- **A page restored from the back/forward cache fires no load event and raises no `DOM.documentUpdated`**, so after any history traversal a drive uses `settle`-style polling of `chromedp.Evaluate` and clicks through the page (`el.click()`), never `chromedp.Click`/`WaitVisible`/`NavigateBack` (the pattern `ui/browser_test.go:2343-2375` already documents; re-measured for this plan).
- **Examples are separate modules.** Any task that edits `ui/tokens.css` re-copies it in the same commit: `cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css`.
- **Class/attribute twins:** every rule naming an `rst-` class carries its attribute twin in the same rule at the same weight, per `internal/markup` (`__part` → `-part`, `--variant` → `~="variant"`, the seven utilities such as `rst-nm` keep their class). Write the class selector, then the exact string `markup.Selector` makes of it. `ui/markup_v3_test.go` enforces both directions; Task 8 extends it to `shell.css`. Every new class also goes into `extraFixture` in `ui/markup_v3_browser_test.go` or its pairing is unproven.
- **Motion gate:** every `transition`/`animation` outside a `@media (prefers-reduced-motion: reduce) {` block (that header, byte for byte: the gate matches it literally) needs the exact same selector with `…: none` inside one. Each `:active-view-transition-type()` selector sits in a rule of its own, reduce blocks included.
- **No inline styles and no inline scripts** in `ui/partials/*` or `ui/layouts/*` (`TestPartialsAndLayoutsEmitNoInlineStyles`). The default CSP (`serve.go:428-430`) is not widened.
- **Catalogs:** the 12 files in `locales/` share one key set (`TestBaseCatalogsShareOneKeySet`), every key `rastrillo.ui.*`. Partials call `Tf` (never `T`) for a string with `{name}` in it: `T` ignores its extra arguments (`ui/funcs.go:163`).
- **Struct callers stay working:** a new optional key read by a shipped partial goes through `opt` (`ui/funcs.go:389`), never `.Key` inline.
- **Copy.** No user-facing English is written into a tracked file before the operator approved it in copy review, and **no em dashes in any new user-facing copy** (operator preference). Batch 1 (the six strings in `copy-review/strings.json`) is approved before execution starts; its result is `copy-review/result.json` (gitignored), `{"action":"approve","strings":[{"id":…,"text":…}]}`, which Task 1 keeps as `copy-review/batch1-result.json` because every later review run overwrites `result.json`; batches 2 and 3 are kept the same way as `batch2-result.json` and `batch3-result.json`. Batch 2 (Task 5) and batch 3 (Task 12) are run by the controller with the `copy-review` skill before the files they feed are written. Translations are not reviewed: draft the eleven from the approved English. English shown in a task is the DRAFT that went to review; the approved text from `result.json` replaces it wherever the two differ, and any translation drafted from a changed string is redrafted. Tests reference catalog keys (`defaultT("rastrillo.ui.row_menu")`) or prose keys through the same variable the renderer uses, never the English literally.
- **Reference docs before batch 3.** A new exported symbol (`ui.ShellJS`, `ui.ShellCSS`, `harness.WithCoarsePointer`, `rastrillo.SpeculationRulesPath`) gets its signature line added to the existing Go code fence of its reference page in the task that adds it, because `TestExportedSymbolsAreDocumented` checks every task. A signature is code, not copy; the prose explaining it comes with batch 3.
- **SKILL.md:** ≤ 30,000 bytes (`skillmd_test.go:124`); 26,577 today. Reviewed like code: no inaccurate line.
- **Budgets:** `rastrillo.js` ≤ 16,384 bytes (`ui/shim_test.go:262`, 9,784 today); `shell.js` ≤ 8,192 (new test); gallery pages ≤ 128 KiB (`internal/designsystem/designsystem_test.go:76`).
- **The gallery's own frame is not touched** (`internal/designsystem/page.go:2290-2306`, pinned by `TestTheSidebarIsTheShellTheGalleryDocuments`): it keeps `<details rst-shell-chrome>` and the legacy CSS keeps it working until B. The gallery's demo application and shell previews do convert (Task 9).
- **Comments** say why and name the failure prevented; a comment that restates the code is not written. **Commits:** imperative subject; body says why; last line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit per task, then `git push origin mobile-ergonomics`: on amadan the branch is the PR.

## Review Focus

The inputs the spec implies but no spec test names, most likely first. Each has its test in the task that owns the code.

1. **A sidebar nav that starts with a group label, or has one link, or has links with no group at all.** The index rows' borders and rounded corners come from `a:first-child`, `[rst-shell-group] + a`, `a:last-child` and `a:has(+ [rst-shell-group])`; a reasonable person expects every row to have a full border and the first and last rows of each run rounded, whatever the markup starts with. *Test: Task 9, `TestTheIndexRowsRoundEachRunOfLinks`.*
2. **A page that writes its `view` block with whitespace** (`{{define "view"}}\n  index\n{{end}}`, which is how a template author formats a multi-line file) must still be the index. `~="index"` matches a whitespace-separated word, so it should; the test proves the layout does not trim or quote it into something else. *Test: Task 9, `TestAViewBlockWithWhitespaceIsStillTheIndex`.*
3. **Back-link and nav hrefs that are not plain paths**: an `up` with a query (`/orders?status=open#nav-orders`), nav hrefs written absolute (`http://host/orders`), and a fragment id that needs decoding (`#nav-caf%C3%A9`). History reuse and focus return compare resolved path and query, and the fragment rule decodes the id. *Test: Task 8, `TestShellJSComparesResolvedURLsNotStrings`.*
4. **A row name that looks like markup or a placeholder** (`<b>"Ada" & Co</b> {name}`) in `row-menu`'s trigger name and in `list-row-action`'s `Menu`: escaped once, not re-substituted, no broken attribute. *Test: Task 6, `TestRowMenuEscapesTheNameItShowsBack`.*
5. **An app that already owns `/_speculation-rules` or answers HEAD**: the framework's `GET` pattern wins over an app catch-all, HEAD is answered like GET, and a `POST` to the path still reaches the app. *Test: Task 11, `TestSpeculationRulesRouteIsGETOnly`.*

## Where the code forced a choice the spec did not make

Each is decided below; a reviewer who disagrees should say so before the task that owns it.

- **Touch legs use a launch flag** (`harness.WithCoarsePointer()`), because CDP cannot make `pointer: coarse` true here (Global Constraints). A touch leg and a mouse leg are therefore two rigs, two browsers.
- **The index/back rules and the card rules are scoped to `@media (max-width: 799.98px)`** rather than written as narrow defaults undone at 800px. §5.1 already requires this for the card; doing the same for §4.3 means nothing needs undoing and the wide rules are untouched. The effect at every width is what §4.3 describes.
- **The tap floors live in one touch block at the end of `tokens.css`** (before the utilities), so they win ties with the component rules above them. The three zoom floors are the exception §1.3 requires: each sits directly after its component's font reset.
- **Selectors in the touch block repeat the weight of the rule they raise**, e.g. `[rst-shell-topbar] [rst-shell-menu] > summary`, not `[rst-shell-menu] > summary`: the existing `min-block-size: 24px` on that summary is (0,2,1) and would beat a (0,1,1) floor wherever it sat.
- **`[rst-selbox]` becomes `inline-flex` and `justify-self: start`** so its label is 24×24 (desktop) and 44×44 (touch) rather than as wide as its grid track or its line. Today it is `display: flex` and measures 992×16 standalone.
- **The primary link keeps a visible change on the link itself when focused** (an underline) as well as the ring on its overlay: `TestA11yWalksTheKeyboard` reads the focused element and two ancestors, never pseudo-elements, and would otherwise see no indicator at all.
- **Console gets the title `<h1>` in its rail too**, so its phone index has one h1 like the sidebar's. `view` and `up` sit in the console in source order; the back strip is after the bar.
- **The gallery's demo is four documents**: `demo.html` (the index; desktop shows the dashboard), `demo-dashboard.html`, `demo-requests.html`, `demo-request.html`. The sidebar and console shell previews are `shells/<shell>.html` (the index, still the address the gallery links) and `shells/<shell>-page.html` (the content page), so every existing link keeps its target.
- **The pre-H layouts are committed as test fixtures** (`ui/testdata/legacy/sidebar.html`, `console.html`), copied from `a7a24dd`, rather than read from git at test time, so the compatibility legs run on a checkout with no history.
- **A mid-plan copy batch (Task 5).** The row menu's gallery sample needs a blurb and state labels, the demo's kebab a label, and the Getting Started page one line per new asset; all are prose keys that need eleven translations, and `buildFamilies`/`TestTheGettingStartedPageWeighsTheRealAssets` fail until they exist. Batch 3 keeps what the operator placed there: docs, SKILL.md, the changelog body and the gallery's shell blurbs. The shell blurbs therefore describe the drawer for Tasks 9 to 12 and are corrected in Task 13.
- **The doctor advisory is printed as `  <layout path>: <approved sentence>`**, adding no words of its own beyond the path.
- **`SKILL.md` gains "a GET never mutates"**: §4.6 says SKILL.md already states it, and it does not (verified). Prerender makes it load-bearing, so batch 3 adds it.

---

## File Structure

Created:

| File | Responsibility |
|---|---|
| `ui/partials/row-menu.html` | The `row-menu` partial. |
| `ui/shell.js` | Sidebar/console phone navigation: slide direction, history reuse, focus return. |
| `ui/shell.css` | The cross-document slide, opted into only by pages that link it. |
| `ui/rowmenu_test.go` | `rowMenuItems` and the partial's unit tests; the styleguide-sample equality test. |
| `ui/sizing_test.go` | Unit gates on the zoom floors and the tap-target inventory. |
| `ui/sizing_fixture_test.go` | `sizingFixture`: every partial, every sample and the small-parent extras, shared by the unit and browser sizing tests. |
| `ui/touch_browser_test.go` | Shared helpers for touch and mouse rigs, the fixture page and its assets. |
| `ui/sizing_browser_test.go` | §10.1: type, targets, calendar, desktop pins. |
| `ui/rows_browser_test.go` | §10.2: whole-row targets. |
| `ui/rowmenu_browser_test.go` | §10.5: the row menu. |
| `ui/card_browser_test.go` | §10.4: the topbar and console card. |
| `ui/shelljs_browser_test.go` | §10.3's scripted legs, on hand-written markup (Task 8). |
| `ui/index_browser_test.go` | §10.3's scriptless, wide, compatibility and a11y legs on the real layouts (Task 9). |
| `ui/prerender_browser_test.go` | §10.6a's browser legs (Task 11). |
| `ui/testdata/legacy/sidebar.html`, `ui/testdata/legacy/console.html` | The pre-H layouts, for the compatibility legs. |
| `speculation.go`, `speculation_test.go` | `SpeculationRulesPath`, the rules body, its handler; unit tests. |
| `cmd/rastrillo/shellscaffold_test.go` | The two-page scaffold and the upgrade legs (§10.6). |

Modified: `ui/tokens.css` (+ both examples' copies), `ui/rastrillo.js`, `ui/ui.go`, `ui/vendored.go`, `ui/funcs.go`, `ui/funcs_test.go`, `ui/styleguide.go`, `ui/layouts/{sidebar,console,topbar}.html` (topbar: comment only), `ui/partials/list-row-action.html`, `ui/ui_test.go`, `ui/shim_test.go`, `ui/markup_v3_test.go`, `ui/markup_v3_browser_test.go`, `ui/shell_browser_test.go`, `ui/console_shell_browser_test.go`, `ui/browser_test.go`, `harness/rig.go`, `harness/rig_test.go`, `locales/*.toml` (12), `basecatalog_test.go`, `serve.go`, `buildhandler_test.go`, `cmd/rastrillo/{new.go,doctor.go,new_test.go,doctor_test.go}`, `internal/designsystem/{designsystem.go,page.go,samples.go,prose.go,designsystem_test.go,a11y_test.go,browser_test.go}`, `docs/site/{templates.md,icons.md}`, `docs/site/reference/{ui.md,harness.md,rastrillo.md}`, `SKILL.md`, `CHANGELOG.md`.

---
### Task 1: The approved catalog strings (copy batch 1)

The operator reviewed batch 1 before execution. This task writes its three catalog strings into all twelve catalogs; the other three strings of the batch (`scaffold.overview`, `doctor.layout_advisory`, `changelog.heading`) are read from the same `result.json` by Tasks 10 and 13.

**Files:**
- Modify: `locales/{en,ga,zh-Hans,es,hi,pt,bn,ru,ja,yue,vi,ar}.toml` (after the `rastrillo.ui.shell_home` line, which is line 33 in all twelve)
- Test: `basecatalog_test.go`

**Interfaces:**
- Consumes: `copy-review/result.json` (gitignored), `rastrillo.BaseCatalogs()`.
- Produces: catalog keys `rastrillo.ui.shell_up_label`, `rastrillo.ui.shell_up` (`{name}`), `rastrillo.ui.row_menu` (`{name}`), in all twelve catalogs. Tasks 6 and 9 call them through `T`/`Tf`.

- [ ] **Step 1: Confirm the batch was approved and read its text**

```bash
jq -e '.action == "approve"' copy-review/result.json
test -f copy-review/batch1-result.json || cp copy-review/result.json copy-review/batch1-result.json
jq -r '.strings[] | "\(.id)\t\(.text)"' copy-review/batch1-result.json
```

Expected: `true`, then six lines. The copy goes to `copy-review/batch1-result.json` because the next copy-review run (Task 5) overwrites `result.json`, and Tasks 10 and 13 read batch 1's other three strings from this copy. If `result.json` is missing or `.action` is not `approve`, stop and report to the controller: nothing in this plan writes unapproved copy. The drafts were `Sections`, `Back to {name}`, `Actions for {name}`; if the approved text differs, the translations in Step 3 are redrafted from it (keep `{name}` exactly once, no em dash).

- [ ] **Step 2: Write the failing test**

Append to `basecatalog_test.go` (it already imports `strings` and `testing`):

```go
// The three strings the phone shells and the row menu say. They are
// checked as a set rather than left to TestBaseCatalogsShareOneKeySet
// because two of them carry {name}, and a translation that drops or
// doubles the placeholder renders "Back to" with nothing after it on a
// screen reader, which no visual check catches. The em-dash rule is the
// operator's for all new copy; it is asserted here because a translator
// reaching for one is the likeliest way it comes back.
func TestTheShellAndRowMenuStringsAreInEveryCatalog(t *testing.T) {
	keys := map[string]int{ // key -> how many {name} it must carry
		"rastrillo.ui.shell_up_label": 0,
		"rastrillo.ui.shell_up":       1,
		"rastrillo.ui.row_menu":       1,
	}
	all := BaseCatalogs()
	if len(all) != 12 {
		t.Fatalf("%d base catalogs, want 12", len(all))
	}
	en := all["en"]
	for code, c := range all {
		for key, names := range keys {
			v, ok := c[key]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s.toml: missing or empty %s", code, key)
				continue
			}
			if got := strings.Count(v, "{name}"); got != names {
				t.Errorf("%s.toml: %s = %q carries {name} %d times, want %d", code, key, v, got, names)
			}
			if names == 0 && strings.ContainsAny(v, "{}") {
				t.Errorf("%s.toml: %s = %q has a placeholder brace in a string that takes no value", code, key, v)
			}
			if strings.Contains(v, "—") {
				t.Errorf("%s.toml: %s = %q contains an em dash", code, key, v)
			}
			if code != "en" && v == en[key] {
				t.Errorf("%s.toml: %s is still the English %q", code, key, v)
			}
		}
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod go test -run TestTheShellAndRowMenuStringsAreInEveryCatalog -count=1 .`
Expected: FAIL, `en.toml: missing or empty rastrillo.ui.shell_up_label` and the same for every key and locale.

- [ ] **Step 4: Write the strings**

Save as `$TMPDIR/h-catalog.py` and run it with `python3 "$TMPDIR/h-catalog.py"`. It takes the English from `result.json`, so the approved words are the ones written; the translations are drafts of the drafts and must be redrafted first if Step 1 showed different English.

```python
import json, pathlib, sys
approved = {s["id"]: s["text"] for s in json.load(open("copy-review/batch1-result.json"))["strings"]}
en = {k: approved[k] for k in ("rastrillo.ui.shell_up_label", "rastrillo.ui.shell_up", "rastrillo.ui.row_menu")}
drafts = {"rastrillo.ui.shell_up_label": "Sections", "rastrillo.ui.shell_up": "Back to {name}", "rastrillo.ui.row_menu": "Actions for {name}"}
if en != drafts:
    sys.exit("approved English differs from the drafts; redraft the translations below from it first: %r" % en)
tr = {
    "ga":      ("Rannóga", "Ar ais chuig {name}", "Gníomhartha do {name}"),
    "zh-Hans": ("栏目", "返回{name}", "{name} 的操作"),
    "es":      ("Secciones", "Volver a {name}", "Acciones para {name}"),
    "hi":      ("अनुभाग", "{name} पर वापस जाएँ", "{name} के लिए कार्रवाइयाँ"),
    "pt":      ("Secções", "Voltar a {name}", "Ações para {name}"),
    "bn":      ("বিভাগসমূহ", "{name}-এ ফিরে যান", "{name}-এর জন্য কাজ"),
    "ru":      ("Разделы", "Назад: {name}", "Действия: {name}"),
    "ja":      ("セクション", "{name}に戻る", "{name} の操作"),
    "yue":     ("欄目", "返回{name}", "{name} 嘅操作"),
    "vi":      ("Mục", "Quay lại {name}", "Thao tác cho {name}"),
    "ar":      ("الأقسام", "العودة إلى {name}", "إجراءات {name}"),
}
rows = {"en": (en["rastrillo.ui.shell_up_label"], en["rastrillo.ui.shell_up"], en["rastrillo.ui.row_menu"]), **tr}
for code, (label, up, menu) in rows.items():
    p = pathlib.Path("locales") / (code + ".toml")
    lines = p.read_text(encoding="utf-8").splitlines(keepends=True)
    at = next(i for i, l in enumerate(lines) if l.startswith("rastrillo.ui.shell_home "))
    new = [f'rastrillo.ui.shell_up_label = "{label}"\n', f'rastrillo.ui.shell_up = "{up}"\n', f'rastrillo.ui.row_menu = "{menu}"\n']
    p.write_text("".join(lines[:at + 1] + new + lines[at + 1:]), encoding="utf-8")
```

(Russian uses "Назад: {name}" and "Действия: {name}" because the preposition forms need a case the substituted word does not carry.)

- [ ] **Step 5: Run the test and the catalog gates**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheShellAndRowMenuStringsAreInEveryCatalog|TestBaseCatalogsShareOneKeySet' -count=1 .`
Expected: PASS.

- [ ] **Step 6: Run the task gate** (Global Constraints, all three commands). Expected: all green.

- [ ] **Step 7: Commit and push**

```bash
git add locales/*.toml basecatalog_test.go
git commit -m "Add the phone shells' and row menu's strings to every catalog

The back control, its accessible name and the row menu's trigger name
are the three framework strings Part H shows. They land first, from the
operator's approved batch, so every later task can render them in all
twelve languages; the test pins the {name} placeholder per locale
because a dropped one reads as \"Back to\" and nothing else.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 2: The type scale, the zoom floor, and a coarse pointer the rig can produce

Spec §1.1–§1.3 and the type half of §1.5 and §10.1.

**Files:**
- Modify: `harness/rig.go` (option + launch flag), `harness/rig_test.go`, `docs/site/reference/harness.md` (signature line only)
- Modify: `ui/tokens.css`: `:root` scale block (lines 77-111), after it the scale query; `.rst-lrow--head` (line 1044) and `.rst-tip::after` (line 1486) literal sizes; three zoom floors after lines 569-574, 1262 and 1285; the old `@media (pointer: coarse)` block and its comment (lines 2064-2092)
- Modify: `examples/blog/static/tokens.css`, `examples/tickets/static/tokens.css` (copies)
- Modify: `internal/designsystem/browser_test.go:1340-1345` (the `kMin` comment)
- Create: `ui/sizing_test.go`, `ui/sizing_fixture_test.go`, `ui/touch_browser_test.go`, `ui/sizing_browser_test.go`

**Interfaces:**
- Consumes: `harness.New`, `harness.Option`; `ui` test helpers `render`, `allPartials`, `Styleguide`, `stripCSSComments`, `collapseSpace`, `stylesheets`.
- Produces (later tasks rely on these exact names):
  - `func harness.WithCoarsePointer() harness.Option`
  - CSS: `--rst-tap: 2.75rem` at `:root`; the query string `@media (pointer: coarse), (max-width: 40rem)`.
  - `ui` tests (untagged): `func sizingFixture(t *testing.T) string`, `const sizingExtras string`.
  - `ui` tests (`browser` tag): `func sizingMux(t *testing.T, pages map[string]string) *http.ServeMux`, `func sizingDoc(title, body string) string`, `func sizingRig(t *testing.T, coarse bool, pages map[string]string) *harness.Rig`, `func requirePointer(t *testing.T, ctx context.Context, coarse bool)`, `const tokensJS`, `const fontsJS`, `type typeReading struct{ Lg, Base, Sm, Xs, Body float64; Coarse bool; Width int }`.

- [ ] **Step 1: Write the failing harness test**

Append to `harness/rig_test.go` (already `//go:build browser`, package `harness`, imports `fmt`, `net/http`, `strings`, `testing`, `chromedp`):

```go
// WithCoarsePointer is the only way a drive gets a phone's pointer:
// CDP's touch emulation leaves (pointer: coarse) false in this engine.
// The default rig is the control — if it already reported coarse, the
// option's true would prove nothing about the option.
func TestWithCoarsePointerMakesThePrimaryPointerCoarse(t *testing.T) {
	build := func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>pointer</title></head><body><p>pointer</p></body></html>`)
		})
		return mux
	}
	read := func(r *Rig) (coarse, hover bool) {
		r.Run(
			chromedp.Navigate(r.Origin+"/"),
			chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &coarse),
			chromedp.Evaluate(`matchMedia("(hover: hover)").matches`, &hover),
		)
		return coarse, hover
	}
	if coarse, _ := read(New(t, build)); coarse {
		t.Fatal("CONTROL: a default rig already reports a coarse pointer, so the option's reading below proves nothing")
	}
	coarse, hover := read(New(t, build, WithCoarsePointer()))
	if !coarse {
		t.Error("WithCoarsePointer: (pointer: coarse) is false; every touch leg would measure desktop density")
	}
	if hover {
		t.Error("WithCoarsePointer: (hover: hover) is true; a phone's primary pointer cannot hover")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestWithCoarsePointerMakesThePrimaryPointerCoarse -count=1 ./harness/`
Expected: FAIL to compile, `undefined: WithCoarsePointer`.

- [ ] **Step 3: Add the option**

In `harness/rig.go`, add the field to `config` and the option after `WithScrollbars`:

```go
type config struct {
	withoutPRFAtCreation bool
	withScrollbars       bool
	coarsePointer        bool
}
```

```go
// WithCoarsePointer launches Chromium with a touch screen as its primary
// pointer, so (pointer: coarse) matches and (hover: hover) does not.
//
// It is a launch option because nothing later can do it: measured on
// 2026-09-30, Emulation.setTouchEmulationEnabled, a mobile
// setDeviceMetricsOverride and setEmulatedMedia all leave
// matchMedia("(pointer: coarse)") false in this Chromium, while the
// Blink settings below, read once at launch, make it true. A phone-
// layout drive that silently ran on a fine pointer would pass on
// desktop sizes, so every touch leg also asserts the media query
// before it measures.
func WithCoarsePointer() Option {
	return func(c *config) { c.coarsePointer = true }
}
```

In `New`, after the `withScrollbars` block:

```go
	if cfg.coarsePointer {
		// 2 is Blink's PointerType coarse and 1 its HoverType none; the
		// available* pair has to agree or any-pointer disagrees with
		// pointer, which no real phone does.
		allocOpts = append(allocOpts, chromedp.Flag("blink-settings",
			"primaryPointerType=2,availablePointerTypes=2,primaryHoverType=1,availableHoverTypes=1"))
	}
```

In `docs/site/reference/harness.md`, add `func WithCoarsePointer() Option` as the last line of the `## Options` Go fence (after `func WithScrollbars() Option`). No prose: it arrives with batch 3 (Task 13).

- [ ] **Step 4: Run it to verify it passes**

Run the Step 2 command. Expected: PASS.

- [ ] **Step 5: Write the failing unit test for the floors**

Create `ui/sizing_test.go`:

```go
package ui

import (
	"regexp"
	"strings"
	"testing"
)

// touchQuery is the one media query every touch-only rule sits in (spec
// §1.1). Written once so a typo in one floor cannot quietly scope it to
// a query nothing matches.
const touchQuery = "@media (pointer: coarse), (max-width: 40rem) {"

// zoomFloorRule finds a floor: the touch query holding exactly one rule
// whose only declaration is the floor. Read off comment-stripped CSS.
var zoomFloorRule = regexp.MustCompile(`@media \(pointer: coarse\), \(max-width: 40rem\) \{\s*([^{}]+?)\s*\{\s*font-size: max\(1rem, 1em\);\s*\}\s*\}`)

// bareZoomFloor is the zero-weight floor for an app's own inputs: an
// input with no rst- attribute is sized by the UA stylesheet, which any
// author rule beats, so :where() still lifts it and any app rule that
// sizes it still wins.
const bareZoomFloor = ":where(input:not([type=checkbox]):not([type=radio]):not([type=range]), select, textarea)"

// TestTheZoomFloorsSitWhereTheyCannotBeResetOrWin is §1.3's position
// gate. Each component floor must come DIRECTLY after the rule that
// resets its font (`font: inherit` resets font-size, so a floor before
// it is switched off — round 1's textarea finding) and, for the input,
// before the rule that makes the primary field big (at equal weight the
// later rule wins, and max(1rem, 1em) would pull the 19px field down to
// 16px — the 1em trap this rewrite exists to remove). Moving any floor
// one rule up or down fails here.
func TestTheZoomFloorsSitWhereTheyCannotBeResetOrWin(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	floors := map[string][2]int{}
	for _, m := range zoomFloorRule.FindAllStringSubmatchIndex(css, -1) {
		sel := collapseSpace(css[m[2]:m[3]])
		if _, dup := floors[sel]; dup {
			t.Errorf("two zoom floors for %q", sel)
		}
		floors[sel] = [2]int{m[0], m[1]}
	}
	if len(floors) != 4 {
		t.Errorf("tokens.css carries %d zoom floors, want 4 (bare elements, input, textarea, search): %v", len(floors), floors)
	}
	if _, ok := floors[bareZoomFloor]; !ok {
		t.Errorf("no bare-element floor spelled %q", bareZoomFloor)
	}
	if !strings.HasPrefix(bareZoomFloor, ":where(") || !strings.HasSuffix(bareZoomFloor, ")") || specificity(bareZoomFloor) != [3]int{} {
		t.Errorf("the bare-element floor is not wholly inside :where(): %v", specificity(bareZoomFloor))
	}
	base := func(prelude string) (start, end int) {
		loc := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(prelude) + ` \{`).FindStringIndex(css)
		if loc == nil {
			t.Fatalf("tokens.css has no top-level rule %q", prelude)
		}
		close := strings.IndexByte(css[loc[1]:], '}')
		return loc[0], loc[1] + close + 1
	}
	for _, c := range []struct{ floor, after, before string }{
		{".rst-input, [rst-input]", ".rst-input, [rst-input]", `.rst-input--primary, [rst-input~="primary"]`},
		{".rst-textarea, [rst-textarea]", ".rst-textarea, [rst-textarea]", ""},
		{`.rst-search input[type="search"], [rst-search] input[type="search"]`, `.rst-search input[type="search"], [rst-search] input[type="search"]`, ""},
	} {
		at, ok := floors[c.floor]
		if !ok {
			t.Errorf("no zoom floor for %q", c.floor)
			continue
		}
		_, end := base(c.after)
		if gap := strings.TrimSpace(css[end:at[0]]); gap != "" {
			t.Errorf("the floor for %q is not directly after the rule that resets its font; between them:\n%.200s", c.floor, gap)
		}
		if c.before != "" {
			if start, _ := base(c.before); at[1] > start {
				t.Errorf("the floor for %q comes after %q, so it overrides the field that is big on purpose", c.floor, c.before)
			}
		}
	}
	// The block this replaces: one pointer-only query sizing the
	// components at a weight that beat the primary field.
	if strings.Contains(css, "@media (pointer: coarse) {") {
		t.Error("tokens.css still has a pointer-only query; every touch rule uses the one query of §1.1")
	}
}

// The type step itself (§1.2), read as text so a desktop-only edit
// cannot move it: the four tokens, in the touch query, at :root.
func TestTheTypeScaleMovesUpOneStepOnSmallOrTouchScreens(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	re := regexp.MustCompile(regexp.QuoteMeta(touchQuery) + `\s*:root\s*\{([^{}]*)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no :root block inside the touch query")
	}
	for _, want := range []string{"--rst-fs-lg: 1.1875rem;", "--rst-fs-base: 1rem;", "--rst-fs-sm: 0.875rem;", "--rst-fs-xs: 0.8125rem;"} {
		if !strings.Contains(m[1], want) {
			t.Errorf("the touch :root block lacks %q:\n%s", want, m[1])
		}
	}
	if !regexp.MustCompile(`--rst-tap:\s*2\.75rem;`).MatchString(css) {
		t.Error("--rst-tap: 2.75rem is not declared")
	}
	// The two components that spelled the xs step as a literal paint
	// with the token now, or they would stay 11.5px on a phone.
	for _, sel := range []string{".rst-lrow--head, [rst-lrow~=\"head\"]", ".rst-tip::after, [rst-tip]::after"} {
		loc := strings.Index(css, sel+" {")
		if loc < 0 {
			t.Fatalf("no rule %q", sel)
		}
		body := css[loc : loc+strings.IndexByte(css[loc:], '}')]
		if strings.Contains(body, "0.71875rem") || !strings.Contains(body, "font-size: var(--rst-fs-xs)") {
			t.Errorf("%s does not paint with var(--rst-fs-xs): %s", sel, body)
		}
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheZoomFloorsSitWhereTheyCannotBeResetOrWin|TestTheTypeScaleMovesUpOneStepOnSmallOrTouchScreens' -count=1 ./ui/`
Expected: FAIL: `tokens.css carries 0 zoom floors, want 4`, `no :root block inside the touch query`, `tokens.css still has a pointer-only query`.

- [ ] **Step 7: Edit `ui/tokens.css`**

(a) In the `:root` scale block (after `--rst-sp-6: 2.5rem;`), add:

```css
  /* The tap floor on small or touch screens: 44px at the default root,
     and it tracks a reader who raised their default size. Declared here
     so an app's own CSS can use it; only rules inside the touch query
     below read it, so desktop density never sees it. */
  --rst-tap: 2.75rem;
```

(b) Directly after the `:root { … }` scale block closes (before `/* ── Base.`), add:

```css
/* ── Small or touch screens: the type scale moves up one step ───────
   One query decides "a phone, a tablet, or a window too narrow to read
   at desktop density", and every touch-only rule in this file sits in
   it. pointer: coarse is the PRIMARY pointer, so a touchscreen laptop
   driven by its trackpad keeps desktop density; any-pointer would
   switch a mouse user's whole interface because a touchscreen exists.
   max-width: 40rem catches a narrow desktop window and the design
   gallery's 390px Mobile tab, a fine pointer showing what a phone
   shows (a rem in a media query is the browser's initial size, not the
   root's). The shells' 800px is a different axis: between 640 and
   800px under a mouse is a split window, which gets the narrow LAYOUT
   and keeps mouse DENSITY.

   The four tokens move and nothing else: the root size, the rem
   spacing and every px geometry stay, and controls grow because their
   text grows and because the touch block at the end of this file gives
   them a minimum box. Display sizes (page titles, stat numbers, avatar
   initials) stay literal on purpose, so a 22px title sits over 16px
   body text. ─────────────────────────────────────────────────────── */
@media (pointer: coarse), (max-width: 40rem) {
  :root {
    --rst-fs-lg: 1.1875rem;    /* 19px */
    --rst-fs-base: 1rem;       /* 16px */
    --rst-fs-sm: 0.875rem;     /* 14px */
    --rst-fs-xs: 0.8125rem;    /* 13px */
  }
}
```

(c) Line 1044: in `.rst-lrow--head, [rst-lrow~="head"] { … }` replace `font-size: 0.71875rem;` with `font-size: var(--rst-fs-xs);`. Line 1486: in `.rst-tip::after, [rst-tip]::after { … }` replace `font-size: 0.71875rem;` with `font-size: var(--rst-fs-xs);`. Desktop output is identical: the token is 0.71875rem there.

(d) Directly after the rule `.rst-search input[type="search"], [rst-search] input[type="search"] { … font-size: var(--rst-fs-sm); … }` (lines 566-574), insert:

```css
/* The zoom floor for the search input. Mobile Safari zooms a page that
   focuses a text control under 16px, and this input is set to
   --rst-fs-sm, which is 14px even after the touch step: this is the one
   floor that fires inside the rastrillo idioms. It sits directly after
   the rule above because at equal weight the later rule wins; see the
   input floor below for the rest of the reasoning. */
@media (pointer: coarse), (max-width: 40rem) {
  .rst-search input[type="search"], [rst-search] input[type="search"] { font-size: max(1rem, 1em); }
}
```

(e) Directly after the line `.rst-input, [rst-input] { … font: inherit; … }` (line 1262), insert:

```css
/* The zoom floor for the input, and why it is HERE. `font: inherit` in
   the rule above resets font-size, so a floor placed before it would be
   reset with it; the primary rule below sizes the field on purpose, so
   the floor must come before that or it wins the tie and pulls a 19px
   field down to 16. max(1rem, 1em) lifts a field whose parent is small
   (a bulk bar, a menu panel) to 16px; 1em in font-size is the PARENT's
   size, which is exactly why it cannot also keep a large field large —
   the bug the old single floor had. A :where() spelling was rejected: at
   zero weight it loses to `font: inherit` above and switches itself off.
   ui/sizing_test.go holds all three floors to their positions. */
@media (pointer: coarse), (max-width: 40rem) {
  .rst-input, [rst-input] { font-size: max(1rem, 1em); }
}
```

(f) Directly after the line `.rst-textarea, [rst-textarea] { … font: inherit; … }` (line 1285), insert:

```css
/* The textarea's zoom floor, directly after the rule whose `font:
   inherit` would reset it (round 1's finding: a floor above that line
   is dead). Reasoning as for the input floor above. */
@media (pointer: coarse), (max-width: 40rem) {
  .rst-textarea, [rst-textarea] { font-size: max(1rem, 1em); }
}
```

(g) Replace the comment and block at lines 2064-2092 (from `/* ---…Touch: the two zooms nobody asked for.` through the closing `}` of `@media (pointer: coarse) { … }`) with the following, keeping the `:where(a[href], button, …) { touch-action: manipulation; }` rule that follows unchanged:

```css
/* ---------------------------------------------------------------------
   Touch: the two zooms nobody asked for.

   Mobile Safari zooms the whole page when it focuses a form control
   whose text is under 16px. The touch step at the top of this file
   makes body text 16px, but controls inherit their size and some sit in
   a smaller parent, so every text-entry control still has a floor. It
   is written twice, because one spelling cannot do both jobs:

   - the component floors (.rst-input, .rst-textarea, the search input)
     sit directly after the rule that resets each one's font, at that
     rule's weight, and before any rule that sizes the field on purpose.
     max(1rem, 1em) is the floor; 1em in font-size is the PARENT's size,
     so a floor that came after the primary field's rule would shrink
     it. That was this file's bug until H: the old single floor sat
     here, after [rst-input~="primary"] at equal weight, and turned the
     17px primary field into 16px.
   - the floor below, for an app's own input with no rst- attribute,
     is wholly inside :where(): the UA stylesheet is what sizes such a
     field, any author rule beats it, and any rule the app writes to size
     its own field still wins.

   rem rather than px so a reader who raised the browser default keeps
   the increase. user-scalable=no in the viewport tag would buy the same
   quiet by taking pinch-zoom from everyone, which fails WCAG 1.4.4.

   Double-tap is the second zoom: tap a button or a row twice in quick
   succession and the second tap lands as a zoom gesture rather than a
   click. touch-action: manipulation drops that gesture on interactive
   elements only, so panning and pinch-zoom are untouched, and so is
   double-tapping prose or an image, which is a real way to read a
   narrow column. :where() weighs nothing, so any rule can override it.
   --------------------------------------------------------------------- */

@media (pointer: coarse), (max-width: 40rem) {
  :where(input:not([type=checkbox]):not([type=radio]):not([type=range]), select, textarea) {
    font-size: max(1rem, 1em);
  }
}
```

Then copy the file into both examples:

```bash
cp ui/tokens.css examples/blog/static/tokens.css && cp ui/tokens.css examples/tickets/static/tokens.css
```

- [ ] **Step 8: Run the unit tests to verify they pass**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheZoomFloors|TestTheTypeScale|TestEveryClassSelectorHasAnAttributeTwin|TestNoAttributeSelectorIsAnOrphan|TestReducedMotion' -count=1 ./ui/`
Expected: PASS. Mutation check (do it, then undo it): move the `.rst-input` floor block above line 1262's rule; the position test must fail with "is not directly after the rule that resets its font". Restore.

- [ ] **Step 9: Write the sizing fixture (untagged)**

Create `ui/sizing_fixture_test.go`:

```go
package ui

import (
	"sort"
	"strings"
	"testing"
)

// sizingExtras is what no partial and no sample renders: text controls
// in small parents — a bulk bar and a menu panel, whose 14px (desktop)
// is where 1em is under the floor and the floor has something to do —
// a bare input with no rst- attribute (the zero-weight floor's case),
// and the primary field (the 1em trap's case). The menu that holds a
// field is opened by the drives that measure it; everything else is
// read closed, because a font size is computed whether or not it shows.
const sizingExtras = `<div rst-page data-extra="small-parents">
<div rst-bulkbar id="sizing-bulk"><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-bulk-search" aria-label="Find in the selection"></form><input rst-input type="text" id="sizing-bulk-input" aria-label="Rename the selection"><textarea rst-textarea id="sizing-bulk-note" aria-label="Note on the selection"></textarea></div>
<details rst-dropdown name="rst-sizing-extra" id="sizing-menu"><summary>Find</summary><div rst-dropdown-menu><form rst-search method="get" action="/x"><input type="search" name="q" id="sizing-menu-search" aria-label="Find a filter"></form><input rst-input type="text" id="sizing-menu-input" aria-label="Name the filter"><textarea rst-textarea id="sizing-menu-note" aria-label="Note on the filter"></textarea></div></details>
<div rst-field><label rst-field-label for="sizing-bare">Bare</label><input type="text" id="sizing-bare" name="sizing_bare"></div>
<div rst-field><label rst-field-label for="sizing-primary">Title</label><input rst-input="primary" type="text" id="sizing-primary" name="sizing_primary"></div>
</div>`

// sizingFixture is one page body with every partial (each with its own
// test data, the set TestRenderEverythingSmoke renders), every
// styleguide sample except the modal, and the extras above. The modal
// is its own page: its overlay is fixed over the whole viewport and
// would occlude every hit test on this one (spec §10.1: overlays are
// measured in their own states, one at a time).
func sizingFixture(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(sizingExtras)
	samples := Styleguide()
	names := make([]string, 0, len(samples))
	for name := range samples {
		if name != "modal" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString(`<div rst-page data-sample="` + name + `">` + samples[name] + `</div>`)
	}
	for _, p := range allPartials() {
		b.WriteString(`<div rst-page data-partial="` + p.Name + `">` + render(t, p.Name, p.Data) + `</div>`)
	}
	return b.String()
}
```

- [ ] **Step 10: Write the browser helpers**

Create `ui/touch_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// sizingDoc is a page carrying the stylesheet, a theme, the shim and
// busy.js (light dismiss and the busy spinner, which the row-menu legs
// drive), and the scripts that enhance controls (select.js and
// datetime.js change what a field renders, calendar.js draws the grid
// the calendar legs measure). No CSP: this is a test page.
func sizingDoc(title, body string) string {
	return `<!doctype html><html lang="en" dir="ltr"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width"><title>` + title + `</title>` +
		`<link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/theme.css">` +
		`<script defer src="/rastrillo.js"></script><script defer src="/busy.js"></script><script defer src="/select.js"></script>` +
		`<script defer src="/calendar.js"></script><script defer src="/datetime.js"></script>` +
		`</head><body>` + body + `</body></html>`
}

// sizingMux serves pages by path ("/" is the root only) and the assets
// sizingDoc links.
func sizingMux(t *testing.T, pages map[string]string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	stylesheets(t, mux)
	for name, body := range map[string][]byte{
		"rastrillo.js": ShimJS(), "busy.js": BusyJS(), "select.js": SelectJS(), "datetime.js": DatetimeJS(), "calendar.js": CalendarJS(),
	} {
		body := body
		mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(body)
		})
	}
	for path, html := range pages {
		html, pattern := html, "GET "+path
		if path == "/" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		})
	}
	return mux
}

// sizingRig boots one browser, touch or mouse. Two rigs rather than one
// switched: the pointer is a launch flag (harness.WithCoarsePointer).
func sizingRig(t *testing.T, coarse bool, pages map[string]string, opts ...harness.Option) *harness.Rig {
	t.Helper()
	if coarse {
		opts = append(opts, harness.WithCoarsePointer())
	}
	return harness.New(t, func(string) http.Handler { return sizingMux(t, pages) }, opts...)
}

// requirePointer is §10's control: a touch leg that is really running
// on a fine pointer would pass on desktop sizes, so it fails first.
func requirePointer(t *testing.T, ctx context.Context, coarse bool) {
	t.Helper()
	var got bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &got)); err != nil {
		t.Fatalf("reading the pointer: %v", err)
	}
	if got != coarse {
		t.Fatalf("(pointer: coarse) is %v, this leg needs %v; the rig is not the device this leg claims to measure", got, coarse)
	}
}

// tokensJS reads the four type tokens as pixels, the body size, and the
// two halves of the query, so a leg can say which half it exercised.
const tokensJS = `(() => {
  const probe = v => { const s = document.createElement("span"); s.style.fontSize = "var(" + v + ")"; document.body.appendChild(s); const px = parseFloat(getComputedStyle(s).fontSize); s.remove(); return px; };
  return JSON.stringify({Lg: probe("--rst-fs-lg"), Base: probe("--rst-fs-base"), Sm: probe("--rst-fs-sm"), Xs: probe("--rst-fs-xs"),
    Body: parseFloat(getComputedStyle(document.body).fontSize), Coarse: matchMedia("(pointer: coarse)").matches, Width: innerWidth});
})()`

type typeReading struct {
	Lg, Base, Sm, Xs, Body float64
	Coarse                 bool
	Width                  int
}

// fontsJS reads every text-entry control's computed size, visible or
// not — a size is computed for a field inside a closed menu too, and a
// field that zooms when its menu opens is the bug.
const fontsJS = `(() => {
  const TEXTY = 'input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not([type=range]):not([type=submit]):not([type=button]):not([type=color]):not([type=file]), select, textarea';
  const out = [];
  document.querySelectorAll(TEXTY).forEach(el => out.push({
    Name: el.id || el.name || el.getAttribute("aria-label") || el.outerHTML.slice(0, 80),
    Px: parseFloat(getComputedStyle(el).fontSize)}));
  return JSON.stringify(out);
})()`

type fontReading struct {
	Name string
	Px   float64
}
```

- [ ] **Step 11: Write the failing type drive**

Create `ui/sizing_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// readType loads path at w×h and returns the token and field readings.
func readType(t *testing.T, ctx context.Context, url string, w, h int64) (typeReading, []fontReading, float64) {
	t.Helper()
	var rawType, rawFonts string
	var primary float64
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(w, h),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(tokensJS, &rawType),
		chromedp.Evaluate(fontsJS, &rawFonts),
		chromedp.Evaluate(`parseFloat(getComputedStyle(document.getElementById("sizing-primary")).fontSize)`, &primary),
	); err != nil {
		t.Fatalf("reading type at %dx%d: %v", w, h, err)
	}
	var tr typeReading
	var fonts []fontReading
	if err := json.Unmarshal([]byte(rawType), &tr); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rawFonts), &fonts); err != nil {
		t.Fatal(err)
	}
	if len(fonts) < 20 {
		t.Fatalf("only %d text-entry controls on the sizing page; the fixture is not the one this drive measures", len(fonts))
	}
	return tr, fonts, primary
}

// TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens is §10.1's type
// half: at 390 with touch, at 1024 with touch (the pointer half of the
// query alone) and at 600 with a mouse (the width half alone), the four
// tokens are one step up, every text-entry control is at least 16px —
// including the ones in a bulk bar and a menu panel — and the primary
// field is exactly --rst-fs-lg, 19px (the 1em bug gave 16).
func TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens(t *testing.T) {
	pages := map[string]string{"/": sizingDoc("sizing", sizingFixture(t))}
	for _, coarse := range []bool{true, false} {
		rig := sizingRig(t, coarse, pages)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		legs := []struct {
			name string
			w, h int64
		}{{"600x800 mouse, the width half alone", 600, 800}}
		if coarse {
			legs = []struct {
				name string
				w, h int64
			}{{"390x844 touch", 390, 844}, {"1024x768 touch, the pointer half alone", 1024, 768}}
		}
		for _, leg := range legs {
			tr, fonts, primary := readType(t, ctx, rig.Origin+"/", leg.w, leg.h)
			requirePointer(t, ctx, coarse)
			if tr.Lg != 19 || tr.Base != 16 || tr.Sm != 14 || tr.Xs != 13 || tr.Body != 16 {
				t.Errorf("%s: tokens %v/%v/%v/%v body %v, want 19/16/14/13 body 16", leg.name, tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
			}
			for _, f := range fonts {
				if f.Px < 16 {
					t.Errorf("%s: %s is %vpx; a phone zooms the page when it is focused", leg.name, f.Name, f.Px)
				}
			}
			if primary != 19 {
				t.Errorf("%s: the primary field is %vpx, want --rst-fs-lg (19px); 16 is the 1em trap", leg.name, primary)
			}
		}
		cancel()
	}
}

// TestDesktopDensityIsPinned is §10.1's 1280×900 mouse leg: every value
// below is today's, measured, and any change fails. Task 3 adds the
// control sizes and Task 4 the whole-row changes the operator approved.
func TestDesktopDensityIsPinned(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("sizing", sizingFixture(t))})
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()
	tr, fonts, primary := readType(t, ctx, rig.Origin+"/", 1280, 900)
	requirePointer(t, ctx, false)
	if tr.Lg != 17 || tr.Base != 14 || tr.Sm != 12.5 || tr.Xs != 11.5 || tr.Body != 14 {
		t.Errorf("desktop tokens %v/%v/%v/%v body %v, want 17/14/12.5/11.5 body 14", tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
	}
	if primary != 17 {
		t.Errorf("desktop primary field %vpx, want 17", primary)
	}
	// The control for the touch drive's "every field ≥ 16": on a desktop
	// some field is under 16, so the instrument can see a small one.
	small := false
	for _, f := range fonts {
		small = small || f.Px < 16
	}
	if !small {
		t.Error("CONTROL: every text control is ≥16px on a 1280px desktop, so the touch drive's ≥16 reading proves nothing")
	}
}
```

- [ ] **Step 12: Run the drives**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens|TestDesktopDensityIsPinned' -count=1 -v ./ui/`
Expected: PASS. (They are written after the CSS because the harness option had to exist; verify they are not vacuous: temporarily delete the touch `:root` block from `ui/tokens.css`, rerun, see the 390 leg fail on `tokens 17/14/12.5/11.5`; restore it. Then move the `.rst-input` floor after the primary rule and see `the primary field is 16px`; restore.)

- [ ] **Step 13: Recheck the gallery's scale floor**

In `internal/designsystem/browser_test.go`, the `kMin` comment (lines 1340-1345) reasons from a 12.5px `--rst-fs-sm`. Append to that comment:

```go
// Rechecked for H: the Mobile tab is a 390px frame, inside the touch
// query, where --rst-fs-sm is 14px and 14 × 0.72 is 10.1px, so the floor
// only gets more legible there and the value stands.
```

- [ ] **Step 14: Run the task gate** (all three commands).

The gallery drives now render the Mobile tab at 16px body text. If `TestPreviewFrameHeightsFitTheirContent` or `TestThePreviewWidgetIsUsableOnAPhone` fails, read its message: it names the sample and the measured height, and `heightOf` in `internal/designsystem/page.go:1342` is where a frame's height is set. Raise the named entry to what the drive measured and rerun; do not lower any floor in the test.

- [ ] **Step 15: Commit and push**

```bash
git add harness/rig.go harness/rig_test.go docs/site/reference/harness.md ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/sizing_test.go ui/sizing_fixture_test.go ui/touch_browser_test.go ui/sizing_browser_test.go internal/designsystem/browser_test.go internal/designsystem/page.go
git commit -m "Move the type scale up a step on small or touch screens and fix the zoom floor

Phones read rastrillo apps at 14px with a floor that shrank the one
field meant to be big: max(1rem, 1em) sat after the primary rule at
equal weight, and 1em in font-size is the parent's size. Each component
floor now sits directly after its own font reset and before any rule
that sizes it on purpose, a unit test pins the positions, and an app's
bare input gets a zero-weight floor. The touch legs need a coarse
pointer that CDP cannot give, so the harness gains a launch option.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 3: Tap targets, the docked calendar, and the desktop pins

Spec §1.4, the control half of §1.5 and §10.1, the row checkbox and kebab sizes of §2.3, and `--rst-col-menu`.

**Files:**
- Modify: `ui/tokens.css`: `:root` scale block (`--rst-col-menu`); the list-grid comment and narrow rule (lines 1030-1057); `[rst-selbox]` (line 1499); a new touch block before the `Utilities, last on purpose` section
- Modify: `ui/styleguide.go` (`list-grid` sample: `--rst-cols: 2fr 110px var(--rst-col-menu)`), `ui/ui.go` (package doc lines 108 and 125: the same variable)
- Modify: `examples/*/static/tokens.css` (copies)
- Modify: `ui/sizing_test.go`, `ui/sizing_fixture_test.go`, `ui/sizing_browser_test.go`
- Test: the files above

**Interfaces:**
- Consumes: Task 2's `sizingFixture`, `sizingDoc`, `sizingRig`, `requirePointer`, `touchQuery`; `ui` test helpers `leafRules`, `splitSelectorList`, `stripCSSComments`.
- Produces:
  - CSS: `--rst-col-menu` (`32px` at `:root`, `var(--rst-tap)` in the touch query); the touch block (`/* ── Tap targets on small or touch screens` … `}`), the last rules before the utilities. Later tasks add their own touch rules at the end of this block.
  - Tests: `var tapInventory []tapEntry` with `type tapEntry struct{ Key, Idiom, Marker string }`, `var tapExempt map[string]string`, `func interactiveCSSRules(css string) []leafRule`, `const targetsJS`, `const overlayJS`, `const calJS`, `type targetReading struct{ Name, Hit string; W, H float64; Fits, Owns, Inline bool }`, `func readTargets(t *testing.T, ctx context.Context, js string) []targetReading`, `func assertTargets(t *testing.T, where string, got []targetReading)`, `func openCalendar(t *testing.T, ctx context.Context, url string, w, h int64) calReading`. Later tasks append inventory rows and reuse the drive.

- [ ] **Step 1: Write the failing inventory gate**

Append to `ui/sizing_test.go`:

```go
// tapEntry is one row of spec §1.4's table: Key is a substring of the
// attribute spelling of the rules that style the idiom, Marker a string
// the sizing fixture (or the modal sample) must contain for the idiom to
// be measured at all. For controls a script draws at run time, the
// Marker is the attribute that asks for them.
type tapEntry struct{ Key, Idiom, Marker string }

var tapInventory = []tapEntry{
	{"[rst-btn]", "Buttons, all sizes", "rst-btn"},
	{"[rst-input]", "Inputs and selects", "rst-input"},
	{"[rst-textarea]", "Textareas", "rst-textarea"},
	{"[rst-search]", "Search box", "rst-search"},
	{"[rst-search-clear]", "Search box: its clear link", "rst-search-clear"},
	{"[rst-ftok] a", "Filter chip's remove link", "rst-ftok"},
	{"[rst-help]", "Help link", "rst-help"},
	{"[rst-dropdown] > summary", "Dropdown summaries (list-bar, header, account, locale)", "rst-dropdown"},
	{"[rst-menu-group] > summary", "Nested menu-group summaries", "rst-menu-group"},
	{"[rst-dropdown-menu] a", "Menu items: dropdown", "rst-dropdown-menu"},
	{"[rst-dropdown-menu] button", "Menu items: dropdown buttons", "rst-dropdown-menu"},
	{"[rst-row-menu-panel] a", "Menu items: row menu", "rst-row-menu-panel"},
	{"[rst-row-menu-panel] button", "Menu items: row menu buttons", "rst-row-menu-panel"},
	{"[rst-locale] button", "Menu items: locale", "rst-locale"},
	{"[rst-combo-option]", "Combobox options", "data-rst-select"},
	{"[rst-dtp-row]", "Date-picker rows", "data-rst-date"},
	{"[rst-dtp-pick]", "Date-picker pick button", "data-rst-date"},
	{"[rst-cal-nav]", "Calendar nav", "data-rst-date"},
	{"[rst-cal-day]", "Calendar days", "data-rst-date"},
	{"[rst-row-action]", "Row action pill", "rst-row-action"},
	{"[rst-row-menu] > summary", "Row kebab", "rst-row-menu"},
	{"[rst-selbox]", "Row checkbox", "rst-selbox"},
	{"[rst-person]", "Standalone person link", "rst-person"},
	{"[rst-pagination] a", "Pagination chips", "rst-pagination"},
	{"[rst-seg-tabs] a", "Segmented tabs", "rst-seg-tabs"},
	{"[rst-switch]", "Switch", "rst-switch"},
	{"[rst-choice-cards] label", "Choice cards", "rst-choice-cards"},
	{"[rst-tblock-head]", "Toggle-block head", "rst-tblock-head"},
	{"[rst-bulkbar-close]", "Bulk bar: close", "rst-bulkbar-close"},
	{"[rst-bulkbar-escalate]", "Bulk bar: escalate link", "rst-bulkbar-escalate"},
	{"[rst-modal-close]", "Modal close", "rst-modal-close"},
	{"[rst-modal-panel] > nav a", "Modal panel nav links", "rst-modal-panel"},
	{"[rst-back-nav] a", "Back-nav link", "rst-back-nav"},
	{"[rst-shell-brand]", "Shell: brand", "rst-shell-brand"},
	{"[rst-shell-nav] a", "Shell: nav links", "rst-shell-nav"},
	{"[rst-shell-menu] > summary", "Shell: Menu summary", "rst-shell-menu"},
	{"[rst-shell-chrome] > summary", "Legacy sidebar drawer summary", "rst-shell-chrome"},
	{"a.rst-nm", "List grid identity link (its own box until Task 4 stretches it)", "rst-nm"},
}

// tapExempt are the interactive rules that are deliberately not held to
// the floor, each with its reason. Nothing else may be added here: a
// control that does not fit the floor gets a rule in the touch block.
var tapExempt = map[string]string{
	"[rst-row-main] > a":         "the stretched primary link: the tap target is the whole row (spec §2)",
	"[rst-no-match] a":           "a link in running text, WCAG 2.5.8's inline exception (spec §1.4, Exempt)",
	".rst-sr-only:focus-visible": "the hidden search submit: it exists for the keyboard and un-hides on focus; no pointer reaches it",
}

// endsOnControlElement reports whether a selector's last compound is an
// a, button, summary or label: the rule styles that element.
func endsOnControlElement(sel string) bool {
	depth, last := 0, 0
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; c {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ' ', '>', '+', '~':
			if depth == 0 {
				last = i + 1
			}
		}
	}
	return regexp.MustCompile(`^(a|button|summary|label)($|[^a-zA-Z0-9_-])`).MatchString(strings.TrimSpace(sel[last:]))
}

// interactiveCSSRules is every innermost rule in css that carries cursor:
// pointer or styles an a, button, summary or label.
func interactiveCSSRules(css string) []leafRule {
	var out []leafRule
	for _, r := range leafRules(stripCSSComments(css)) {
		hit := strings.Contains(r.body, "cursor: pointer")
		for _, s := range splitSelectorList(r.selector) {
			hit = hit || endsOnControlElement(s)
		}
		if hit {
			out = append(out, r)
		}
	}
	return out
}

// TestEveryInteractiveRuleIsInTheTapInventory is §10.1's inventory
// gate: a new control cannot arrive in tokens.css without a decision
// about its touch size, because its rule has to name an idiom in the
// table above (or an exemption with a reason), and every idiom in the
// table has to be on the page the browser drive measures.
func TestEveryInteractiveRuleIsInTheTapInventory(t *testing.T) {
	rules := interactiveCSSRules(string(TokensCSS()))
	if len(rules) < 40 {
		t.Fatalf("found only %d interactive rules in tokens.css; the reader is broken, not the file", len(rules))
	}
	for _, r := range rules {
		named := false
		for _, s := range splitSelectorList(r.selector) {
			for _, e := range tapInventory {
				named = named || strings.Contains(s, e.Key)
			}
			for key := range tapExempt {
				named = named || strings.Contains(s, key)
			}
		}
		if !named {
			t.Errorf("tokens.css styles a control no row of §1.4's inventory names:\n\t%s\n\tadd a touch rule and a tapInventory row, or say why it is exempt", r.selector)
		}
	}
	page := sizingFixture(t) + Styleguide()["modal"]
	for _, e := range tapInventory {
		if !strings.Contains(page, e.Marker) {
			t.Errorf("%s (%s): the sizing fixture renders nothing carrying %q, so the drive never measures it", e.Idiom, e.Key, e.Marker)
		}
	}
}
```

Add `"regexp"` to that file's imports if Task 2 did not already (it did).

- [ ] **Step 2: Run it**

Run: `GOFLAGS=-mod=mod go test -run TestEveryInteractiveRuleIsInTheTapInventory -count=1 -v ./ui/`
Expected: FAIL: `data-rst-select` is not in the fixture (every `field-select` in `allPartials` has two options, below the ten that arm `select.js`), and `rst-btn` sizes `sm`/`lg` are not pinned yet. Every CSS rule should already be named; if any rule is reported, it is a real gap: add its row to the touch block in Step 4 and its key here.

- [ ] **Step 3: Add the missing fixture pieces**

In `ui/sizing_fixture_test.go`, add to the end of `sizingExtras` (before its closing `</div>`):

```html
<p data-extra="buttons"><button rst-btn="sm" type="button" id="sizing-btn-sm">Small</button> <button rst-btn type="button" id="sizing-btn">Default</button> <button rst-btn="lg" type="button" id="sizing-btn-lg">Large</button></p>
```

and in `sizingFixture`, after the extras and before the samples:

```go
	// An enhanced select: select.js arms a field past ten options, and
	// the combobox options it draws are a row of §1.4's table.
	var options []any
	for i := 1; i <= 12; i++ {
		options = append(options, map[string]any{"Value": fmt.Sprint(i), "Label": fmt.Sprintf("Option %d", i)})
	}
	b.WriteString(`<div rst-page data-extra="combobox">` + render(t, "field-select", map[string]any{
		"ID": "sizing-combo", "Name": "sizing_combo", "Label": "Country", "Options": options,
	}) + `</div>`)
```

(add `"fmt"` to the imports). Mark the bare input as an app's own control rather than an idiom, so the target drive skips it (its font size is the zoom floor's business, not the tap floor's): change `<input type="text" id="sizing-bare" name="sizing_bare">` in `sizingExtras` to `<input type="text" id="sizing-bare" name="sizing_bare" data-sizing-not-an-idiom>`.

- [ ] **Step 4: Write the touch block and the desktop changes**

In `ui/tokens.css`:

(a) In the `:root` scale block, after `--rst-tap`, add:

```css
  /* The list grid's kebab column. A variable rather than the literal
     32px the samples used to write, so the column can grow to a 44px
     kebab on touch screens (the touch block redefines it). An app that
     keeps a literal 32px still works: the kebab is justify-self: end,
     so it overflows 12px toward the inline start, into the grid's
     0.85rem gap, without reaching the cell beside it. */
  --rst-col-menu: 32px;
```

(b) In the list-grid comment (line 1036) replace `trailing 32px for the kebab` with `trailing var(--rst-col-menu) for the kebab`, and in the `@media (max-width: 800px)` rule (line 1055) replace `minmax(0, 1fr) auto 32px` with `minmax(0, 1fr) auto var(--rst-col-menu)`.

(c) Replace the `[rst-selbox]` rule (line 1499) with:

```css
/* The row checkbox's target is its LABEL, padded so the unchanged 16px
   box sits in a 24×24 target (WCAG 2.5.8) on every screen. Inside a row
   whose whole width is a link (spec §2) the spacing exception cannot
   rescue a 16px target, so this is one of the three desktop changes the
   operator approved. inline-flex and justify-self: start keep the label
   its own size in a grid cell and in a line, where display: flex made
   it as wide as either. A larger invisible hit area was rejected: it
   would overlap the row's own overlay and take clicks meant for it. */
.rst-selbox, [rst-selbox] { align-items: center; display: inline-flex; justify-self: start; margin: 0; padding: 4px; }
```

(d) Immediately before the `/* ---…\n   Utilities, last on purpose.` comment, add the touch block:

```css
/* ── Tap targets on small or touch screens ──────────────────────────
   Every interactive idiom is at least --rst-tap (44px) on both axes,
   measured as the box a tap ACTIVATES: the label of a label-backed
   control, the row of a stretched link. Anything that is display:
   inline today becomes inline-flex or flex here, because min sizes do
   nothing on an inline box. Each selector repeats the weight of the
   rule it raises: a floor lighter than the size it raises loses
   wherever it sits (the topbar's Menu summary is (0,2,1)).

   The block is last before the utilities so it wins ties with every
   component above it. ui/sizing_test.go fails when a rule styling an
   a, button, summary or label arrives without a row in the inventory
   this block implements, and ui/sizing_browser_test.go measures every
   control at 390 and 1024 with a coarse pointer and at 600 with a
   mouse. ─────────────────────────────────────────────────────────── */
@media (pointer: coarse), (max-width: 40rem) {
  :root { --rst-col-menu: var(--rst-tap); }

  .rst-btn, [rst-btn] { justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-input, [rst-input], .rst-textarea, [rst-textarea] { min-block-size: var(--rst-tap); }
  /* The box is the target, and so is the input inside it: a tap focuses
     the input, so the input stretches to the box's full height. */
  .rst-search, [rst-search] { min-block-size: var(--rst-tap); padding-block: 0; }
  .rst-search input[type="search"], [rst-search] input[type="search"] { align-self: stretch; }
  .rst-search__clear, [rst-search-clear] { min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-ftok a, [rst-ftok] a { block-size: var(--rst-tap); inline-size: var(--rst-tap); }
  .rst-help, [rst-help] { block-size: var(--rst-tap); inline-size: var(--rst-tap); }

  .rst-dropdown > summary, [rst-dropdown] > summary { align-items: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-dropdown__menu .rst-menu-group > summary, [rst-dropdown-menu] [rst-menu-group] > summary { align-items: center; display: flex; min-block-size: var(--rst-tap); }
  .rst-dropdown__menu a, [rst-dropdown-menu] a, .rst-dropdown__menu button, [rst-dropdown-menu] button, .rst-row-menu__panel a, [rst-row-menu-panel] a, .rst-row-menu__panel button, [rst-row-menu-panel] button, .rst-locale button, [rst-locale] button { align-items: center; display: flex; min-block-size: var(--rst-tap); }
  .rst-combo__option, [rst-combo-option], .rst-dtp__row, [rst-dtp-row] { align-items: center; box-sizing: border-box; display: flex; min-block-size: var(--rst-tap); }

  /* The date field: a 44px pick button, and room in the input so typed
     text never runs under it. */
  .rst-dtp__input, [rst-dtp-input] { padding-inline-end: 3rem; }
  .rst-dtp__pick, [rst-dtp-pick] { block-size: var(--rst-tap); inline-size: var(--rst-tap); inset-inline-end: 0; }
  .rst-cal__nav, [rst-cal-nav] { block-size: var(--rst-tap); inline-size: var(--rst-tap); }
  .rst-cal__day, [rst-cal-day] { block-size: var(--rst-tap); }
  /* The calendar docks to the bottom of the viewport on every small or
     touch screen. Anchored, it is placed from the field's inline start,
     and a date field can sit at the trailing end of a field row at any
     width; docked, it is always inside the viewport. The grid asks for
     seven taps and the panel is fit-content around it, with the
     scrollbar gutter reserved, so a classic scrollbar widens the PANEL
     and never narrows the DAYS (review round 4). Both max sizes clamp to
     the viewport: below a 342px viewport the days narrow to about 41px
     and stay 44px tall, the design exception the operator approved. The
     height cap is why the panel scrolls itself: six weeks of 44px rows
     are taller than a phone in landscape, and a fixed panel's clipped
     top cannot be reached by scrolling the page. top: auto because the
     base rule's physical top: 100% would otherwise be measured against
     the viewport; the block-start inset and the margins reset because
     .is-above sets them (the :is() weighs (0,3,0) and beats it). */
  :is(.rst-cal, .rst-dtp .rst-cal, .rst-dtp.is-above .rst-cal), :is([rst-cal], [rst-dtp] [rst-cal], [rst-dtp].is-above [rst-cal]) { box-sizing: border-box; inline-size: fit-content; inset-block-end: var(--rst-sp-2); inset-block-start: auto; inset-inline: 0; margin: 0 auto; max-block-size: calc(100dvh - 2 * var(--rst-sp-2)); max-inline-size: calc(100vw - 2 * var(--rst-sp-2)); overflow-y: auto; overscroll-behavior: contain; position: fixed; scrollbar-gutter: stable; top: auto; }
  :is(.rst-cal, .rst-dtp .rst-cal) .rst-cal__grid, :is([rst-cal], [rst-dtp] [rst-cal]) [rst-cal-grid] { inline-size: calc(7 * var(--rst-tap)); max-inline-size: 100%; }

  /* The row's own controls (spec §2.3). The checkbox's label grows by
     padding, so the 16px box stays 16px and the label is 44×44. */
  .rst-row__action, [rst-row-action] { align-items: center; box-sizing: border-box; display: inline-flex; justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); padding-inline: 1rem; }
  .rst-row-menu > summary, [rst-row-menu] > summary { block-size: var(--rst-tap); inline-size: var(--rst-tap); }
  .rst-selbox, [rst-selbox] { box-sizing: border-box; justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); padding: 0; }
  .rst-lrow > .rst-nm, [rst-lrow] > .rst-nm { align-content: center; display: grid; min-block-size: var(--rst-tap); }
  a.rst-person, a[rst-person] { min-block-size: var(--rst-tap); }

  .rst-pagination a, [rst-pagination] a, .rst-pagination span, [rst-pagination] span { align-items: center; box-sizing: border-box; display: inline-flex; justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-seg-tabs a, [rst-seg-tabs] a { align-items: center; box-sizing: border-box; display: inline-flex; justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-switch, [rst-switch], .rst-choice__cards label, [rst-choice-cards] label, .rst-tblock > .rst-tblock__head, [rst-tblock] > [rst-tblock-head] { min-block-size: var(--rst-tap); }

  .rst-bulkbar__close, [rst-bulkbar-close] { block-size: var(--rst-tap); inline-size: var(--rst-tap); }
  .rst-bulkbar__escalate, [rst-bulkbar-escalate] { align-items: center; display: inline-flex; min-block-size: var(--rst-tap); }
  .rst-modal-close, [rst-modal-close] { align-items: center; display: inline-flex; justify-content: center; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-modal-panel > nav a, [rst-modal-panel] > nav a { align-items: center; display: flex; min-block-size: var(--rst-tap); }
  .rst-back-nav a, [rst-back-nav] a { align-items: center; display: inline-flex; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }

  .rst-shell__brand, [rst-shell-brand] { align-items: center; display: inline-flex; min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-shell-topbar .rst-shell__nav a, [rst-shell-topbar] [rst-shell-nav] a, .rst-shell-sidebar .rst-shell__nav a, [rst-shell-sidebar] [rst-shell-nav] a, .rst-shell-console .rst-shell__nav a, [rst-shell-console] [rst-shell-nav] a { align-items: center; box-sizing: border-box; display: flex; min-block-size: var(--rst-tap); }
  .rst-shell-topbar .rst-shell__menu > summary, [rst-shell-topbar] [rst-shell-menu] > summary, .rst-shell-console .rst-shell__menu > summary, [rst-shell-console] [rst-shell-menu] > summary { min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-shell-sidebar > .rst-shell__chrome > summary, [rst-shell-sidebar] > [rst-shell-chrome] > summary { min-block-size: var(--rst-tap); }
}
```

(e) In `ui/styleguide.go` (`list-grid` sample) and `ui/ui.go` (package doc, lines 108 and 125) replace the literal `32px` kebab column with `var(--rst-col-menu)`: the sample becomes `<div rst-card style="--rst-cols: 2fr 110px var(--rst-col-menu)">`, the doc's CSS line `.orders { --rst-cols: 2fr 110px var(--rst-col-menu); }` and its sentence `(trailing var(--rst-col-menu) reserved for a kebab)`.

Copy tokens.css into both examples.

- [ ] **Step 5: Run the unit gates**

Run: `GOFLAGS=-mod=mod go test -run 'TestEveryInteractiveRuleIsInTheTapInventory|TestEveryClassSelectorHasAnAttributeTwin|TestNoAttributeSelectorIsAnOrphan|TestIdiomClassesAreStyled|TestTheZoomFloors' -count=1 ./ui/`
Expected: PASS. Mutation check: delete the `[rst-cal-nav]` row from `tapInventory` and see the gate name the `.rst-cal__nav` rules; restore.

- [ ] **Step 6: Write the target drive**

Append to `ui/sizing_browser_test.go` (add `"fmt"`, `"math"` and `"github.com/chromedp/chromedp/kb"` to its imports):

```go
// targetReading is one control, measured as its activation area.
type targetReading struct {
	Name, Hit            string
	W, H                 float64
	Fits, Owns, Inline   bool
}

// measureFn is the measuring half every target reading shares, written
// once so the closed page, each opened overlay and the modal are the
// same instrument. The activation area is the element a tap activates:
// a checkbox or radio is measured as its label, a stretched link (an
// absolutely positioned ::after with content) as its positioned
// ancestor, the row. Each area is scrolled into view (scrollIntoView
// scrolls every scrolling ancestor, a rail or a menu panel included) and
// measured again after the scroll, and the point tested is the centre of
// the part inside the viewport. elementFromPoint there must return the
// control, its area, or a descendant of either (a switch's track, a
// summary's icon): anything else is an occlusion failure, never a skip
// (review round 2, finding 11). Only a control that is not rendered at
// all is skipped. An element marked data-sizing-not-an-idiom is an
// app's own control on the fixture for the font test and is not an
// idiom this floor covers.
const measureFn = `function measure(root, skip) {
  const CONTROLS = 'a[href], button, summary, input:not([type=hidden]), select, textarea, [rst-cal-day], [role="option"]';
  const describe = el => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    [...el.attributes].filter(a => a.name.startsWith("rst-")).map(a => "[" + a.name + (a.value ? "=" + a.value : "") + "]").join("") +
    " “" + (el.getAttribute("aria-label") || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 30) + "”";
  const rendered = el => {
    if (el.checkVisibility && !el.checkVisibility({visibilityProperty: true})) return false;
    const r = el.getBoundingClientRect();
    return r.width >= 2 && r.height >= 2 && getComputedStyle(el).clipPath !== "inset(50%)";
  };
  const area = el => {
    if (el.matches("input[type=checkbox], input[type=radio]")) {
      const l = el.closest("label") || (el.id && document.querySelector('label[for="' + el.id + '"]'));
      if (l) return l;
    }
    if (el.tagName === "A") {
      const after = getComputedStyle(el, "::after");
      if (after.content !== "none" && after.position === "absolute" && el.offsetParent) return el.offsetParent;
    }
    return el;
  };
  const out = [], seen = new Set();
  root.querySelectorAll(CONTROLS).forEach(el => {
    if (skip && skip(el)) return;
    if (el.closest("[data-sizing-not-an-idiom]")) return;
    const a = area(el);
    if (seen.has(a) || !rendered(a)) return;
    seen.add(a);
    a.scrollIntoView({block: "center", inline: "center"});
    const r = a.getBoundingClientRect();
    const x0 = Math.max(r.left, 0), y0 = Math.max(r.top, 0), x1 = Math.min(r.right, innerWidth), y1 = Math.min(r.bottom, innerHeight);
    const hit = document.elementFromPoint((x0 + x1) / 2, (y0 + y1) / 2);
    out.push({Name: describe(el), W: r.width, H: r.height,
      Fits: r.left >= -0.5 && r.top >= -0.5 && r.right <= innerWidth + 0.5 && r.bottom <= innerHeight + 0.5,
      Owns: !!hit && (hit === el || hit === a || el.contains(hit) || a.contains(hit)),
      Hit: hit ? describe(hit) : "nothing",
      Inline: el.tagName === "A" && !el.hasAttribute("rst-btn") && !!el.closest("p, [rst-field-help]") && !el.closest("nav")});
  });
  return out;
}`

// targetsJS measures the page as it loads: every overlay closed.
const targetsJS = `(() => { ` + measureFn + `; return JSON.stringify(measure(document)); })()`

// overlayJS opens the i-th <details> on the page (and every <details>
// around it), measures what it revealed — its own summary was measured
// closed — and closes everything again, so one open overlay never
// counts as occluding the next (review round 3, finding 2).
const overlayJS = `((i) => { ` + measureFn + `;
  const all = document.querySelectorAll("details");
  if (i >= all.length) return JSON.stringify({Done: true});
  const d = all[i];
  for (let p = d; p; p = p.parentElement && p.parentElement.closest("details")) p.open = true;
  const summary = d.querySelector(":scope > summary");
  const got = measure(d, el => el === summary);
  document.querySelectorAll("details[open]").forEach(x => { x.open = false; });
  return JSON.stringify({Done: false, Targets: got});
})(%d)`

func readTargets(t *testing.T, ctx context.Context, js string) []targetReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("measuring targets: %v", err)
	}
	var got []targetReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("reading targets (%q): %v", raw, err)
	}
	return got
}

// assertTargets holds every measured control to 44×44, whole in the
// viewport and hittable at its centre. A link in running text is the
// one exemption (WCAG 2.5.8's inline exception).
func assertTargets(t *testing.T, where string, got []targetReading) {
	t.Helper()
	for _, g := range got {
		switch {
		case !g.Fits:
			t.Errorf("%s: %s is %.1f×%.1f and cannot be brought wholly into the viewport; too big to hit", where, g.Name, g.W, g.H)
		case !g.Owns:
			t.Errorf("%s: %s is occluded: a tap at its centre lands on %s", where, g.Name, g.Hit)
		case g.Inline:
		case g.W < 43.5 || g.H < 43.5:
			t.Errorf("%s: %s is %.1f×%.1f, under the 44×44 floor", where, g.Name, g.W, g.H)
		}
	}
}

// measureEverything reads the closed page, then every overlay alone,
// then the combobox's list and the date field's list, each opened by
// the script that owns it.
func measureEverything(t *testing.T, ctx context.Context, where string) int {
	t.Helper()
	settleUntil(t, ctx, `!!document.querySelector("[rst-combo] [role=combobox]") && !!document.querySelector("[rst-dtp-pick]")`)
	got := readTargets(t, ctx, targetsJS)
	n := len(got)
	assertTargets(t, where+", overlays closed", got)
	for i := 0; ; i++ {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(overlayJS, i), &raw)); err != nil {
			t.Fatalf("%s: opening overlay %d: %v", where, i, err)
		}
		var o struct {
			Done    bool
			Targets []targetReading
		}
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			t.Fatal(err)
		}
		if o.Done {
			break
		}
		n += len(o.Targets)
		assertTargets(t, fmt.Sprintf("%s, overlay %d open", where, i), o.Targets)
	}
	for _, open := range []struct{ name, js, list string }{
		{"the combobox's list", `(() => { const i = document.querySelector("#sizing-combo").closest("[rst-field]").querySelector("[role=combobox]"); i.focus(); i.click(); return true; })()`, "[rst-combo-list]"},
		{"the date field's list", `(() => { const i = document.querySelector("[rst-dtp] [role=combobox]"); i.focus(); i.value = "t"; i.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`, "[rst-dtp-list]"},
	} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(open.js, nil)); err != nil {
			t.Fatalf("%s: opening %s: %v", where, open.name, err)
		}
		settleUntil(t, ctx, `(() => { const l = document.querySelector("`+open.list+`"); return !!l && l.checkVisibility() && l.querySelectorAll("[role=option]").length > 0; })()`)
		got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document.querySelector("`+open.list+`"))); })()`)
		if len(got) == 0 {
			t.Fatalf("%s: %s opened with no options to measure", where, open.name)
		}
		n += len(got)
		assertTargets(t, where+", "+open.name, got)
		chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape))
	}
	return n
}

// settleUntil polls a page expression until it is true or 10s pass.
func settleUntil(t *testing.T, ctx context.Context, expr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var yes bool
		// Errors are tolerated: an evaluation that lands while a document
		// is being swapped in fails, and the next one reads the new page.
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &yes)); err == nil && yes {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("never became true within 10s: %s", expr)
}

// TestEveryTapTargetIsAtLeast44Pixels is §10.1's target half, at 390
// and 1024 with a coarse pointer and at 600 with a mouse, then the modal
// on a page of its own.
func TestEveryTapTargetIsAtLeast44Pixels(t *testing.T) {
	pages := map[string]string{
		"/":      sizingDoc("sizing", sizingFixture(t)),
		"/modal": sizingDoc("modal", Styleguide()["modal"]),
	}
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{
		{"390x844 touch", 390, 844, true},
		{"1024x768 touch", 1024, 768, true},
		{"600x800 mouse", 600, 800, false},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, pages)
			ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/")); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			n := measureEverything(t, ctx, leg.name)
			t.Logf("%s: %d controls measured", leg.name, n)
			if n < 100 {
				t.Errorf("%s: measured only %d controls; the fixture is not the page this drive thinks it is", leg.name, n)
			}
			if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/modal")); err != nil {
				t.Fatal(err)
			}
			got := readTargets(t, ctx, targetsJS)
			if len(got) < 3 {
				t.Fatalf("%s: the modal page gave up %d controls, want its close link and two nav links", leg.name, len(got))
			}
			assertTargets(t, leg.name+", the modal", got)
		})
	}
}

// TestTheKebabOverflowsIntoTheGapOnALiteralColumn is §2.3's claim for
// an app that keeps --rst-cols' literal 32px: on a wide touch screen the
// 44px kebab is justify-self: end, so it spills 12px toward the inline
// start, into the 0.85rem gap, and never over the cell beside it.
func TestTheKebabOverflowsIntoTheGapOnALiteralColumn(t *testing.T) {
	page := sizingDoc("literal", `<div rst-page><div rst-card style="--rst-cols: 1fr 110px 32px"><div rst-lrow>`+
		`<a class="rst-nm" href="#">Grace Hopper</a><span id="cell" class="rst-cell-mut">Paid</span>`+
		`<details rst-row-menu name="rst-menus"><summary id="kebab" aria-label="Actions">⋮</summary><div rst-row-menu-panel><a href="#">View</a></div></details>`+
		`</div></div></div>`)
	rig := sizingRig(t, true, map[string]string{"/": page})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1024, 768), chromedp.Navigate(rig.Origin+"/"),
		chromedp.Evaluate(`(() => { const c = document.getElementById("cell").getBoundingClientRect(), k = document.getElementById("kebab").getBoundingClientRect();
		  return JSON.stringify({CellEnd: c.right, KebabStart: k.left, W: k.width}); })()`, &raw)); err != nil {
		t.Fatal(err)
	}
	requirePointer(t, ctx, true)
	var g struct{ CellEnd, KebabStart, W float64 }
	json.Unmarshal([]byte(raw), &g)
	if g.W < 43.5 {
		t.Fatalf("the kebab is %.1fpx wide on a touch screen; the premise (a 44px kebab in a 32px track) is not met", g.W)
	}
	if g.KebabStart < g.CellEnd-0.5 {
		t.Errorf("the 44px kebab starts at %.1f and the cell beside it ends at %.1f: it overlaps the neighbouring cell", g.KebabStart, g.CellEnd)
	}
}
```

- [ ] **Step 7: Write the calendar drive**

Append to `ui/sizing_browser_test.go`:

```go
// calJS reads the open calendar: where the panel is, whether it
// scrolls, how big each day is, and whether every day and both month
// buttons can be scrolled to and hit.
const calJS = `(() => {
  const cal = document.querySelector("[rst-cal]");
  if (!cal || !cal.checkVisibility()) return JSON.stringify({Open: false});
  const items = [...cal.querySelectorAll("[rst-cal-nav], [rst-cal-day]")];
  let minW = 1e9, minH = 1e9; const unhittable = [];
  for (const el of items) {
    el.scrollIntoView({block: "nearest", inline: "nearest"});
    const b = el.getBoundingClientRect();
    if (el.matches("[rst-cal-day]")) { minW = Math.min(minW, b.width); minH = Math.min(minH, b.height); }
    const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    if (!hit || !(hit === el || el.contains(hit))) unhittable.push((el.getAttribute("data-rst-day") || "nav") + " -> " + (hit ? hit.tagName : "nothing"));
  }
  cal.scrollTop = 0;
  const r = cal.getBoundingClientRect(), g = cal.querySelector("[rst-cal-grid]").getBoundingClientRect(), cs = getComputedStyle(cal);
  return JSON.stringify({Open: true, Position: cs.position, InlineSize: cs.inlineSize, Left: r.left, Top: r.top, Right: r.right, Bottom: r.bottom, H: r.height,
    VW: document.documentElement.clientWidth, VH: innerHeight, GridW: g.width, ScrollH: cal.scrollHeight, ClientH: cal.clientHeight,
    Days: cal.querySelectorAll("[rst-cal-day]").length, Navs: cal.querySelectorAll("[rst-cal-nav]").length,
    MinDayW: minW, MinDayH: minH, Unhittable: unhittable});
})()`

type calReading struct {
	Open                                  bool
	Position, InlineSize                  string
	Left, Top, Right, Bottom, H, VW, VH   float64
	GridW, ScrollH, ClientH, MinDayW, MinDayH float64
	Days, Navs                            int
	Unhittable                            []string
}

// openCalendar loads url at w×h, presses the first date field's
// calendar button (through the page, which is what a tap does) and
// reads the panel.
func openCalendar(t *testing.T, ctx context.Context, url string, w, h int64) calReading {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(w, h), chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `!!document.querySelector("[rst-dtp-pick]")`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("[rst-dtp-pick]").click(), true`, nil)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `document.querySelectorAll("[rst-cal] [rst-cal-day]").length === 42`)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(calJS, &raw)); err != nil {
		t.Fatal(err)
	}
	var c calReading
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("reading the calendar (%q): %v", raw, err)
	}
	if !c.Open || c.Days != 42 || c.Navs != 2 {
		t.Fatalf("the calendar is open=%v with %d days and %d month buttons, want six weeks and two buttons", c.Open, c.Days, c.Navs)
	}
	return c
}

func inViewport(c calReading) bool {
	return c.Left >= -0.5 && c.Top >= -0.5 && c.Right <= c.VW+0.5 && c.Bottom <= c.VH+0.5
}

// TestTheCalendarDocksAndItsDaysAreTaps is §10.1's calendar: docked
// inside the viewport with 44×44 days at 390; no taller than a
// landscape phone and scrolling itself at 640×320, with every day and
// both month buttons reachable; with classic scrollbars at 600×320 and
// 390×320, mouse and touch, still scrolling, days still 44 wide, the
// grid exactly seven taps (round 4: the gutter widens the panel, never
// narrows a day); at 320 the approved exception (days ≥24 wide, 44
// tall, no overflow); and with the field at the inline end of a field
// row, still inside the viewport.
func TestTheCalendarDocksAndItsDaysAreTaps(t *testing.T) {
	field := render(t, "field-date", map[string]any{"Name": "due", "Label": "Due", "Value": "2026-08-28"})
	pages := map[string]string{
		"/":    sizingDoc("calendar", `<div rst-page>`+field+`</div>`),
		"/end": sizingDoc("calendar at the end of a row", `<div rst-page><div rst-field-row><div rst-field class="rst-grow"><label rst-field-label for="n">Name</label><input rst-input id="n" name="n"></div><div>`+field+`</div></div></div>`),
	}
	touch := sizingRig(t, true, pages)
	ctx, cancel := context.WithTimeout(touch.Context(), 180*time.Second)
	defer cancel()

	c := openCalendar(t, ctx, touch.Origin+"/", 390, 844)
	requirePointer(t, ctx, true)
	if c.Position != "fixed" || !inViewport(c) {
		t.Errorf("390 touch: the calendar is %s at %.0f..%.0f × %.0f..%.0f in a %.0f×%.0f viewport; it is not docked inside it", c.Position, c.Left, c.Right, c.Top, c.Bottom, c.VW, c.VH)
	}
	if c.MinDayW < 43.5 || c.MinDayH < 43.5 {
		t.Errorf("390 touch: the smallest day is %.1f×%.1f, want 44×44", c.MinDayW, c.MinDayH)
	}
	if math.Abs(c.GridW-308) > 0.5 {
		t.Errorf("390 touch: the grid is %.1fpx, want seven taps (308)", c.GridW)
	}

	c = openCalendar(t, ctx, touch.Origin+"/", 640, 320)
	if c.H > c.VH+0.5 || c.ScrollH <= c.ClientH || len(c.Unhittable) > 0 {
		t.Errorf("640x320 touch: panel %.0fpx in a %.0fpx viewport, scrolls=%v, unreachable %v", c.H, c.VH, c.ScrollH > c.ClientH, c.Unhittable)
	}

	c = openCalendar(t, ctx, touch.Origin+"/", 320, 640)
	if c.MinDayW < 24 || c.MinDayH < 43.5 || c.Right > c.VW+0.5 || c.Left < -0.5 {
		t.Errorf("320 touch: days %.1f×%.1f, panel %.0f..%.0f in %.0f; the approved exception is ≥24 wide, 44 tall, no overflow", c.MinDayW, c.MinDayH, c.Left, c.Right, c.VW)
	}

	c = openCalendar(t, ctx, touch.Origin+"/end", 390, 844)
	if !inViewport(c) {
		t.Errorf("390 touch, field at the row's end: the calendar is at %.0f..%.0f in %.0f", c.Left, c.Right, c.VW)
	}

	// Classic scrollbars, which the harness hides by default: the leg
	// round 4 found, where the gutter came out of the days.
	for _, coarse := range []bool{false, true} {
		rig := sizingRig(t, coarse, pages, harness.WithScrollbars())
		sctx, scancel := context.WithTimeout(rig.Context(), 90*time.Second)
		for _, vp := range [][2]int64{{600, 320}, {390, 320}} {
			c := openCalendar(t, sctx, rig.Origin+"/", vp[0], vp[1])
			requirePointer(t, sctx, coarse)
			where := fmt.Sprintf("%dx%d classic scrollbars, coarse=%v", vp[0], vp[1], coarse)
			if c.ScrollH <= c.ClientH {
				t.Errorf("%s: the panel is not scrolling (%.0f ≤ %.0f); the gutter case this leg is for has not arisen", where, c.ScrollH, c.ClientH)
			}
			if c.MinDayW < 43.5 {
				t.Errorf("%s: a day is %.1fpx wide; the scrollbar's gutter came out of the days", where, c.MinDayW)
			}
			if math.Abs(c.GridW-308) > 0.5 {
				t.Errorf("%s: the grid is %.1fpx, want exactly seven taps (308)", where, c.GridW)
			}
			if !inViewport(c) {
				t.Errorf("%s: the panel is at %.0f..%.0f × %.0f..%.0f, outside the viewport", where, c.Left, c.Right, c.Top, c.Bottom)
			}
		}
		scancel()
	}
}
```

Add `"amadan.net/rastrillo/rastrillo/harness"` to the file's imports.

- [ ] **Step 8: Extend the desktop pins**

In `TestDesktopDensityIsPinned`, after the type assertions, add:

```go
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
	  const box = sel => { const r = document.querySelector(sel).getBoundingClientRect(); return [Math.round(r.width * 100) / 100, Math.round(r.height * 100) / 100]; };
	  return JSON.stringify({Sm: box("#sizing-btn-sm")[1], Md: box("#sizing-btn")[1], Lg: box("#sizing-btn-lg")[1],
	    Kebab: box('[data-sample="list-grid"] [rst-row-menu] > summary'), Label: box('[data-sample="selbox"] [rst-selbox]'),
	    Box: box('[data-sample="selbox"] [rst-selbox] input')});
	})()`, &raw)); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Sm, Md, Lg        float64
		Kebab, Label, Box [2]float64
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	// Measured before H: 27.69, 33.88 and 43.97 (the "about 28/34/44"
	// of tokens.css's header), a 26×26 kebab, a 16px checkbox.
	if math.Round(d.Sm) != 28 || math.Round(d.Md) != 34 || math.Round(d.Lg) != 44 {
		t.Errorf("desktop button heights %v/%v/%v, want 28/34/44", d.Sm, d.Md, d.Lg)
	}
	if d.Kebab != [2]float64{26, 26} {
		t.Errorf("desktop kebab %v, want 26×26", d.Kebab)
	}
	if d.Box != [2]float64{16, 16} || d.Label != [2]float64{24, 24} {
		t.Errorf("desktop row checkbox %v in a label %v, want the 16px box in a 24×24 target", d.Box, d.Label)
	}
	c := openCalendar(t, ctx, rig.Origin+"/", 1280, 900)
	if c.Position != "absolute" || c.InlineSize != "288px" {
		t.Errorf("desktop calendar is %s, inline-size %s; want today's anchored 18rem (288px) panel", c.Position, c.InlineSize)
	}
```

(`TestDesktopDensityIsPinned` already has `rig` and `ctx`; add `"math"` to the imports if Step 6 did not.)

- [ ] **Step 9: Run the drives**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestEveryTapTargetIsAtLeast44Pixels|TestTheKebabOverflowsIntoTheGapOnALiteralColumn|TestTheCalendarDocksAndItsDaysAreTaps|TestDesktopDensityIsPinned|TestTextControlsAreSixteenPixels' -count=1 -v ./ui/`
Expected: PASS. If a control is reported under 44 or occluded, it is either missing from the touch block (add the rule and, if it is a new selector shape, its `tapInventory` row) or a real occlusion to fix in CSS; never widen `assertTargets`. Mutation checks, one at a time, each restored: delete `.rst-cal__day, [rst-cal-day] { block-size … }` → the calendar leg fails on day height; remove `scrollbar-gutter: stable` from the docking rule → the classic-scrollbar leg fails on day width; change `padding: 4px` on `[rst-selbox]` to `0` → the desktop pin fails.

- [ ] **Step 10: Run the task gate** (all three commands). Note the axe and reflow drives in `internal/designsystem` now see 44px targets on the Mobile tab; if `TestA11yReflowsAt320` reports a page scrolling sideways at 320, the offender is named in its output: fix it in the touch block (a `min-inline-size` that should not apply, usually), not in the test.

- [ ] **Step 11: Commit and push**

```bash
git add ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/styleguide.go ui/ui.go ui/sizing_test.go ui/sizing_fixture_test.go ui/sizing_browser_test.go
git commit -m "Give every control a 44px target on small or touch screens and dock the calendar

Targets were about 28px under a thumb. One touch block, last before the
utilities so it wins its ties, raises every idiom in the spec's
inventory to --rst-tap, measured as the box a tap activates, and a unit
gate fails when a new control rule arrives without a decision. The
calendar docks to the viewport's bottom with a grid of seven taps and a
reserved gutter, so a classic scrollbar widens the panel instead of
narrowing the days. On desktop only the row checkbox changes: its label
becomes a 24x24 target around the same 16px box.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 4: Whole-row targets

Spec §2 and §10.2. On desktop this lands two of the three approved changes: the whole-row target and its focus ring (the third, the 24×24 checkbox label, landed in Task 3).

**Files:**
- Modify: `ui/tokens.css`: the list-row-action block (lines 654-715: hover, focus, the status-pill lift), the list-grid block (lines 1043-1047: hover, overlay), and a new "controls above the overlay" rule after the list-grid block
- Modify: `examples/*/static/tokens.css` (copies)
- Modify: `ui/sizing_test.go` (inventory: the list grid's identity links are now stretched)
- Modify: `ui/ui.go` (package doc: the list grid paragraph gains one sentence on the whole-row rule; lines 101-126)
- Modify: `ui/markup_v3_browser_test.go` (`extraFixture`: a person-identity row, a row with no link)
- Create: `ui/rows_browser_test.go`

**Interfaces:**
- Consumes: Task 3's `measureFn`, `assertTargets`, `readTargets`, `settleUntil`, `sizingDoc`, `sizingRig`, `requirePointer`.
- Produces: CSS vocabulary other tasks rely on: the primary link of a row is `[rst-row-main] > a`, `[rst-lrow] > a.rst-nm` or `[rst-lrow] > a[rst-person]`; every other `a[href]`, `button`, `summary`, `label`, `input`, `select`, `textarea` inside `[rst-row]`/`[rst-lrow]` is lifted to `z-index: 1` at zero weight. Test helpers: `func rowsFixture(t *testing.T) string`, `const kebabA string`, `func at(t *testing.T, ctx context.Context, js string, v any)`, `type point struct{ X, Y float64; Hit string }`, `func probe(t, ctx, sel string, fx, fy, dx, dy float64) point`, `func clickAndLand(t, ctx, p point) string`, `func home(t, ctx, origin string)`.

- [ ] **Step 1: Write the failing drive**

Create `ui/rows_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// kebabA is a row menu written by hand, the idiom as it stands before
// Task 6 ships the partial: five items, so its open panel reaches down
// over the row below it.
const kebabA = `<details rst-row-menu name="rst-menus" id="menu-a"><summary id="kebab-a" aria-label="Actions for Grace Hopper">⋮</summary>` +
	`<div rst-row-menu-panel id="panel-a"><a href="/go/a-view">View</a><a href="/go/a-edit">Edit</a><a href="/go/a-copy">Duplicate</a><a href="/go/a-move">Move</a><hr><a class="rst-danger" href="/go/a-delete">Delete…</a></div></details>`

// rowsFixture is every row idiom of spec §2.2 that has a primary link,
// one that has none, and a row holding the three controls that
// position themselves (a switch, a date field, an enhanced select) with
// the same three outside any row to compare against.
func rowsFixture(t *testing.T) string {
	t.Helper()
	var opts []any
	for i := 1; i <= 12; i++ {
		opts = append(opts, map[string]any{"Value": fmt.Sprint(i), "Label": fmt.Sprintf("Option %d", i)})
	}
	sw := func(id string) string {
		return `<label rst-switch id="` + id + `"><input type="checkbox" name="` + id + `"><span rst-switch-track aria-hidden="true"></span> On</label>`
	}
	return `<div rst-page>
<a id="before" href="#before">Before the lists</a>
<div rst-list id="lra">` + render(t, "list-row-action", map[string]any{
		"Href": "/go/lra", "Main": "Release notes, August", "Sub": "Published 2 August",
		"StatusTone": "positive", "StatusLabel": "Published",
		"ActionHref": "/go/lra-edit", "ActionLabel": "Edit", "ActionAria": "Edit Release notes, August",
	}) + `</div>
<div rst-card id="grid" style="--rst-cols: auto minmax(0, 1fr) 110px var(--rst-col-menu)">
<div rst-lrow id="row-a"><label rst-selbox id="label-a"><input type="checkbox" id="check-a" aria-label="Select Grace Hopper"></label><a class="rst-nm" id="link-a" href="/go/row-a">Grace Hopper<small>AB3PX</small></a><span class="rst-m-hide rst-cell-mut" id="cell-a">Paid</span>` + kebabA + `</div>
<div rst-lrow id="row-b"><label rst-selbox id="label-b"><input type="checkbox" id="check-b" aria-label="Select Alan Turing"></label><a class="rst-nm" id="link-b" href="/go/row-b">Alan Turing<small>CD4QY</small></a><span class="rst-m-hide rst-cell-mut">Due</span><details rst-row-menu name="rst-menus" id="menu-b"><summary id="kebab-b" aria-label="Actions for Alan Turing">⋮</summary><div rst-row-menu-panel><a href="/go/b-view">View</a></div></details></div>
</div>
<div rst-card id="people" style="--rst-cols: minmax(0, 1fr) 110px"><div rst-lrow id="row-p"><a rst-person href="/go/person" id="link-p"><span rst-person-av aria-hidden="true">A</span><span rst-person-meta><span rst-person-name>Ada Lovelace</span><span rst-person-email>ada@example.com</span></span></a><span class="rst-cell-mut">Owner</span></div></div>
<div rst-card id="inert" style="--rst-cols: minmax(0, 1fr) 110px"><div rst-lrow id="row-n"><span rst-person><span rst-person-av aria-hidden="true">B</span><span rst-person-meta><span rst-person-name>Barbara Liskov</span></span></span><span class="rst-cell-mut">Viewer</span></div></div>
<div rst-card id="controls" style="--rst-cols: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1.5fr)"><div rst-lrow id="row-c"><a class="rst-nm" href="/go/row-c" id="link-c">Settings</a>` +
		sw("switch-in") + `<div id="date-in">` + render(t, "field-date", map[string]any{"Name": "due_in", "Label": "Due", "Value": "2026-08-28"}) + `</div>` +
		`<div id="combo-in">` + render(t, "field-select", map[string]any{"ID": "combo_in", "Name": "combo_in", "Label": "Country", "Options": opts}) + `</div></div></div>
<section rst-box id="outside"><div style="display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1.5fr); gap: 0.85rem"><span></span>` +
		sw("switch-out") + `<div id="date-out">` + render(t, "field-date", map[string]any{"Name": "due_out", "Label": "Due", "Value": "2026-08-28"}) + `</div>` +
		`<div id="combo-out">` + render(t, "field-select", map[string]any{"ID": "combo_out", "Name": "combo_out", "Label": "Country", "Options": opts}) + `</div></div></section>
</div>`
}

// rowsPages serves the fixture at / and a landing page for every /go/
// href, so a click that navigates is a URL change the drive can read.
func rowsPages(t *testing.T) map[string]string {
	return map[string]string{
		"/":    sizingDoc("rows", rowsFixture(t)),
		"/go/": sizingDoc("landed", `<p id="landed">landed</p>`),
	}
}

// at evaluates a JS expression returning JSON into v.
func at(t *testing.T, ctx context.Context, js string, v any) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluating: %v\n%s", err, js)
	}
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
}

// point is a place on screen and what elementFromPoint finds there.
type point struct {
	X, Y float64
	Hit  string
}

// probe returns the point at (x, y) in the box of sel, where fx/fy are
// fractions of the width and height and dx/dy pixel offsets from that.
func probe(t *testing.T, ctx context.Context, sel string, fx, fy, dx, dy float64) point {
	t.Helper()
	var p point
	at(t, ctx, fmt.Sprintf(`(() => { const r = document.querySelector(%q).getBoundingClientRect();
	  const x = r.left + r.width * %v + %v, y = r.top + r.height * %v + %v; const h = document.elementFromPoint(x, y);
	  return JSON.stringify({X: x, Y: y, Hit: h ? (h.id || h.tagName) : "nothing"}); })()`, sel, fx, dx, fy, dy), &p)
	return p
}

// clickAndLand clicks the page at p and reports the path it ends on.
func clickAndLand(t *testing.T, ctx context.Context, p point) string {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(p.X, p.Y)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	var path string
	settle := time.Now().Add(5 * time.Second)
	for time.Now().Before(settle) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`location.pathname`, &path)); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return path
}

func home(t *testing.T, ctx context.Context, origin string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/"), chromedp.WaitVisible("#grid", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	settleUntil(t, ctx, `!!document.querySelector("#combo-in [role=combobox]") && !!document.querySelector("#date-in [rst-dtp-pick]")`)
}

// TestTheWholeRowIsTheTarget is §10.2 at 1280 with a mouse and 390 with
// a coarse pointer. For every row idiom with a primary link, a click at
// the row's far empty edge and inside an empty cell goes to the primary
// href; the status pill is part of the target now; the row's own
// controls each do their own thing and never navigate.
func TestTheWholeRowIsTheTarget(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280 mouse", 1280, 900, false}, {"390 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, rowsPages(t))
			ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			home(t, ctx, rig.Origin)
			requirePointer(t, ctx, leg.coarse)

			for _, c := range []struct {
				where, sel   string
				fx, fy, dx   float64
				link, target string
				wideOnly     bool
			}{
				{"the list grid row's far edge", "#row-a", 1, 0.5, -12, "link-a", "/go/row-a", false},
				// The cell is rst-m-hide: below 800px the list grid is three
				// columns and the column that is hidden has no box to probe.
				{"an empty cell of the list grid row", "#cell-a", 0.5, 0.5, 0, "link-a", "/go/row-a", true},
				{"the person row's far edge", "#row-p", 1, 0.5, -12, "link-p", "/go/person", false},
				{"list-row-action's empty main area", "#lra [rst-row-main]", 1, 0.5, -8, "A", "/go/lra", false},
				{"list-row-action's status pill", "#lra [rst-status]", 0.5, 0.5, 0, "A", "/go/lra", false},
			} {
				if c.wideOnly && leg.coarse {
					continue
				}
				home(t, ctx, rig.Origin)
				p := probe(t, ctx, c.sel, c.fx, c.fy, c.dx, 0)
				if p.Hit != c.link {
					t.Errorf("%s: at %s the element under the pointer is %s, want the primary link %s", leg.name, c.where, p.Hit, c.link)
				}
				if got := clickAndLand(t, ctx, p); got != c.target {
					t.Errorf("%s: a click at %s went to %q, want %q", leg.name, c.where, got, c.target)
				}
			}

			// The row's own controls.
			home(t, ctx, rig.Origin)
			if got := clickAndLand(t, ctx, probe(t, ctx, "#lra [rst-row-action]", 0.5, 0.5, 0, 0)); got != "/go/lra-edit" {
				t.Errorf("%s: the action pill went to %q, want its own href", leg.name, got)
			}
			home(t, ctx, rig.Origin)
			if got := clickAndLand(t, ctx, probe(t, ctx, "#kebab-a", 0.5, 0.5, 0, 0)); got != "/" {
				t.Errorf("%s: the kebab navigated to %q", leg.name, got)
			}
			var open bool
			chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("menu-a").open`, &open))
			if !open {
				t.Errorf("%s: a click on the kebab did not open its menu", leg.name)
			}
			for _, c := range []struct {
				where      string
				fx, fy, dx float64
			}{{"the checkbox itself", 0.5, 0.5, 0}, {"its label's padding", 0, 0, 2}} {
				home(t, ctx, rig.Origin)
				p := probe(t, ctx, "#label-a", c.fx, c.fy, c.dx, c.dx)
				if got := clickAndLand(t, ctx, p); got != "/" {
					t.Errorf("%s: a click on %s navigated to %q", leg.name, c.where, got)
				}
				var checked bool
				chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("check-a").checked`, &checked))
				if !checked {
					t.Errorf("%s: a click on %s did not toggle the checkbox (hit %s)", leg.name, c.where, p.Hit)
				}
			}

			// Sizes: 44×44 for everything in the rows on a phone; on the
			// desktop the checkbox's label is the approved 24×24.
			home(t, ctx, rig.Origin)
			if leg.coarse {
				assertTargets(t, leg.name+", rows", readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify([...measure(document.getElementById("lra")), ...measure(document.getElementById("grid")), ...measure(document.getElementById("people"))]); })()`))
			} else {
				var box [2]float64
				at(t, ctx, `(() => { const r = document.getElementById("label-a").getBoundingClientRect(); return JSON.stringify([r.width, r.height]); })()`, &box)
				if box != [2]float64{24, 24} {
					t.Errorf("%s: the row checkbox's label is %v, want 24×24", leg.name, box)
				}
			}
		})
	}
}

// TestAnOpenRowMenuStaysAboveTheRowsBelowIt: only the summary is lifted
// (§2.3), so the open panel — z-index 40, or position: fixed under
// anchor positioning — paints above the next row's lifted controls, and
// a click on an item lying over row B's kebab hits the item.
func TestAnOpenRowMenuStaysAboveTheRowsBelowIt(t *testing.T) {
	for _, coarse := range []bool{false, true} {
		rig := sizingRig(t, coarse, rowsPages(t))
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		w := int64(1280)
		if coarse {
			w = 390
		}
		chromedp.Run(ctx, chromedp.EmulateViewport(w, 844))
		home(t, ctx, rig.Origin)
		chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("kebab-a").click(), true`, nil))
		var got struct {
			Overlapping int
			Wrong       []string
		}
		at(t, ctx, `(() => {
		  const k = document.getElementById("kebab-b").getBoundingClientRect(), wrong = []; let n = 0;
		  for (const item of document.querySelectorAll("#panel-a a")) {
		    const r = item.getBoundingClientRect();
		    const x0 = Math.max(r.left, k.left), x1 = Math.min(r.right, k.right), y0 = Math.max(r.top, k.top), y1 = Math.min(r.bottom, k.bottom);
		    if (x1 - x0 < 2 || y1 - y0 < 2) continue;
		    n++;
		    const h = document.elementFromPoint((x0 + x1) / 2, (y0 + y1) / 2);
		    if (!(h === item || item.contains(h))) wrong.push(item.textContent + " -> " + (h ? (h.id || h.tagName) : "nothing"));
		  }
		  return JSON.stringify({Overlapping: n, Wrong: wrong});
		})()`, &got)
		if got.Overlapping == 0 {
			t.Fatalf("coarse=%v: no item of row A's open menu lies over row B's kebab; the case this leg is for has not arisen", coarse)
		}
		if len(got.Wrong) > 0 {
			t.Errorf("coarse=%v: over row B's kebab, a click on row A's menu lands elsewhere: %v", coarse, got.Wrong)
		}
		cancel()
	}
}

// TestControlsInARowKeepTheirOwnBoxes: the lifting rule is wholly inside
// :where(), so it gives a static control a position and never replaces
// one a component chose (review round 2, finding 4). A switch's input
// stays over its track, the date field's pick button inside its field,
// select.js's native select out of flow; each works by a click at the
// centre of what a person sees.
func TestControlsInARowKeepTheirOwnBoxes(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900))
	home(t, ctx, rig.Origin)
	var g map[string]string
	at(t, ctx, `(() => {
	  const pos = el => getComputedStyle(el).position;
	  const own = sel => { const el = document.querySelector(sel), r = el.getBoundingClientRect(), h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return h && (h === el || el.contains(h)) ? "ok" : "hit " + (h ? h.tagName : "nothing"); };
	  const pick = side => { const p = document.querySelector("#date-" + side + " [rst-dtp-pick]"), d = p.closest("[rst-dtp]"); return Math.round(d.getBoundingClientRect().right - p.getBoundingClientRect().right) + "px from the end"; };
	  return JSON.stringify({
	    switchIn: pos(document.querySelector("#switch-in input")), switchOut: pos(document.querySelector("#switch-out input")),
	    pickIn: pos(document.querySelector("#date-in [rst-dtp-pick]")) + " " + pick("in"), pickOut: pos(document.querySelector("#date-out [rst-dtp-pick]")) + " " + pick("out"),
	    selectIn: pos(document.querySelector("#combo_in")), selectOut: pos(document.querySelector("#combo_out")),
	    switchHit: own("#switch-in"), dateHit: own("#date-in [role=combobox]"), pickHit: own("#date-in [rst-dtp-pick]"), comboHit: own("#combo-in [role=combobox]")});
	})()`, &g)
	for _, pair := range [][2]string{{"switchIn", "switchOut"}, {"pickIn", "pickOut"}, {"selectIn", "selectOut"}} {
		if g[pair[0]] != g[pair[1]] {
			t.Errorf("in a row %s is %q; outside one it is %q", pair[0], g[pair[0]], g[pair[1]])
		}
	}
	if g["switchIn"] != "absolute" || g["selectIn"] != "absolute" {
		t.Errorf("the lifting rule replaced a component's own position: switch input %q, native select %q", g["switchIn"], g["selectIn"])
	}
	for _, k := range []string{"switchHit", "dateHit", "pickHit", "comboHit"} {
		if g[k] != "ok" {
			t.Errorf("in a row, %s: %s", k, g[k])
		}
	}
}

// TestARowWithNoLinkLooksAndActsInert: no overlay, no hover fill. The
// linked row beside it is the control: its hover DOES fill.
func TestARowWithNoLinkLooksAndActsInert(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900))
	home(t, ctx, rig.Origin)
	bg := func(sel string) string {
		p := probe(t, ctx, sel, 0.5, 0.5, 0, 0)
		chromedp.Run(ctx, chromedp.MouseEvent("mouseMoved", p.X, p.Y))
		var s string
		chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`getComputedStyle(document.querySelector(%q)).backgroundColor`, sel), &s))
		return s
	}
	chromedp.Run(ctx, chromedp.MouseEvent("mouseMoved", 1, 1))
	var restN, restA string
	chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.getElementById("row-n")).backgroundColor`, &restN),
		chromedp.Evaluate(`getComputedStyle(document.getElementById("row-a")).backgroundColor`, &restA))
	if hovered := bg("#row-a"); hovered == restA {
		t.Fatal("CONTROL: hovering a linked row does not change its background, so the inert row's unchanged background proves nothing")
	}
	if hovered := bg("#row-n"); hovered != restN {
		t.Errorf("hovering a row with no link fills it (%s -> %s): it looks clickable and is not", restN, hovered)
	}
	if got := clickAndLand(t, ctx, probe(t, ctx, "#row-n", 0.5, 0.5, 0, 0)); got != "/" {
		t.Errorf("a click on a row with no link went to %q", got)
	}
}

// TestFocusDrawsTheRingAroundTheWholeRow: the primary link's ring moves
// to its overlay, 2px inside the row, so the focused row is outlined
// whole; the link itself also underlines, which is what the keyboard
// walk's element-and-ancestors reading sees.
func TestFocusDrawsTheRingAroundTheWholeRow(t *testing.T) {
	rig := sizingRig(t, false, rowsPages(t))
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900))
	home(t, ctx, rig.Origin)
	if err := chromedp.Run(ctx, chromedp.Focus("#check-a", chromedp.ByQuery), chromedp.KeyEvent(kb.Tab)); err != nil {
		t.Fatal(err)
	}
	var g struct {
		Active, Style, Width, Offset, Own, Deco, Corner1, Corner2 string
		Visible                                                  bool
	}
	at(t, ctx, `(() => {
	  const a = document.activeElement, row = document.getElementById("row-a"), r = row.getBoundingClientRect(), after = getComputedStyle(a, "::after");
	  const hit = (x, y) => { const h = document.elementFromPoint(x, y); return h ? (h.id || h.tagName) : "nothing"; };
	  return JSON.stringify({Active: a.id, Visible: a.matches(":focus-visible"), Style: after.outlineStyle, Width: after.outlineWidth, Offset: after.outlineOffset,
	    Own: getComputedStyle(a).outlineStyle, Deco: getComputedStyle(a).textDecorationLine,
	    Corner1: hit(r.left + 3, r.top + 3), Corner2: hit(r.right - 3, r.bottom - 3)});
	})()`, &g)
	if g.Active != "link-a" || !g.Visible {
		t.Fatalf("Tab from the checkbox focused %q (focus-visible %v), want link-a; the rest of this leg reads nothing", g.Active, g.Visible)
	}
	if g.Style != "solid" || g.Width != "2px" || g.Offset != "-2px" {
		t.Errorf("the overlay's ring is %s %s offset %s, want a solid 2px ring inset 2px", g.Style, g.Width, g.Offset)
	}
	if g.Own != "none" || g.Deco != "underline" {
		t.Errorf("the link itself has outline %s and decoration %s, want none and underline", g.Own, g.Deco)
	}
	if g.Corner1 != "link-a" || g.Corner2 != "link-a" {
		t.Errorf("the overlay does not reach the row's corners (%s, %s): the ring is not around the whole row", g.Corner1, g.Corner2)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTheWholeRowIsTheTarget|TestAnOpenRowMenuStaysAboveTheRowsBelowIt|TestControlsInARowKeepTheirOwnBoxes|TestARowWithNoLinkLooksAndActsInert|TestFocusDrawsTheRingAroundTheWholeRow' -count=1 ./ui/`
Expected: FAIL: the list grid far edge hits `DIV`, not `link-a`; the status pill is `SPAN`; the ring is on the link, not its overlay; `TestARowWithNoLinkLooksAndActsInert` passes already on hover only if the `<span rst-person>` row does not fill (it does today: `:has(> [rst-person])`), so it fails too.

- [ ] **Step 3: Write the CSS**

In `ui/tokens.css`:

(a) Replace `.rst-row:hover, [rst-row]:hover { background: var(--rst-surface-2); }` with:

```css
/* Only a row that is a link looks like one: the fill follows the
   primary link, so a hand-written row with nothing to open stays flat. */
.rst-row:has(> .rst-row__main > a):hover, [rst-row]:has(> [rst-row-main] > a):hover {
  background: var(--rst-surface-2);
}
```

(b) Replace the comment above `.rst-row__main > a, [rst-row-main] > a` with the whole-row rule, and after the `::after` rule add the focus rules:

```css
/* A row with a destination has ONE primary link, and its ::after covers
   the row, so the row is the target on every screen (spec §2): the
   cursor, the status-bar URL, middle-click and "open in new tab" all
   work across the whole width. Everything else in the row that can be
   operated is lifted above the overlay by the :where() rule under the
   list grid. The row is not a link, it contains one, so nested anchors
   stay impossible. */
```

```css
/* Focus draws the ring on the overlay, inset 2px so a card's corner
   cannot cut it, so a keyboard user sees the whole row outlined. The
   link also underlines: a check that reads the focused element and its
   ancestors (the gallery's keyboard walk) never sees a pseudo-element,
   and forced-colors mode keeps text decoration. */
.rst-row__main > a:focus-visible, [rst-row-main] > a:focus-visible, .rst-lrow > a.rst-nm:focus-visible, [rst-lrow] > a.rst-nm:focus-visible, .rst-lrow > a.rst-person:focus-visible, [rst-lrow] > a[rst-person]:focus-visible { outline: none; text-decoration: underline; }
.rst-row__main > a:focus-visible::after, [rst-row-main] > a:focus-visible::after, .rst-lrow > a.rst-nm:focus-visible::after, [rst-lrow] > a.rst-nm:focus-visible::after, .rst-lrow > a.rst-person:focus-visible::after, [rst-lrow] > a[rst-person]:focus-visible::after { border-radius: var(--rst-radius-sm); outline: 2px solid var(--rst-accent); outline-offset: -2px; }
```

(c) Replace the status-pill lift (`/* A status pill inside a row sits above …` and its rule) with:

```css
/* A status pill inside a row is part of the row's target, not a
   control: a dead spot inside a row you can tap is a mis-tap on a
   phone. It was lifted above the overlay until H. */
.rst-row .rst-status, [rst-row] [rst-status] {
  flex: none;
}
```

(d) In the list-grid block replace the hover rule (line 1045) and add the overlay:

```css
.rst-lrow:not(.rst-lrow--head):has(> a.rst-nm, > a.rst-person):hover, [rst-lrow]:not([rst-lrow~="head"]):has(> a.rst-nm, > a[rst-person]):hover { background: var(--rst-accent-soft); }
/* The list grid's primary link is its identity cell: the rst-nm link,
   or a person link when a person is who the row is. Its ::after covers
   the row, as list-row-action's does. One identity cell per row: with
   both, the later overlay wins and the earlier link is unreachable. */
.rst-lrow > a.rst-nm::after, [rst-lrow] > a.rst-nm::after, .rst-lrow > a.rst-person::after, [rst-lrow] > a[rst-person]::after { content: ""; inset: 0; position: absolute; }
```

(e) After the `.rst-count-line` rule (before the `@media (max-width: 800px)` list-grid block), add:

```css
/* Controls above the overlay. A positioned overlay paints above every
   element that is not positioned, whatever the DOM order, so without
   this a checkbox before the name link would be covered and a tap on it
   would open the item.

   It lifts the CONTROLS, never their containers: lifting a row menu's
   <details> would make it a stacking context and trap its open panel
   under the next row's lifted controls. The :not() is attached to the
   :is() with no space, so it filters the controls themselves; with a
   space it is a descendant combinator and lifts things inside them.
   The primary link is excluded, or its ::after would measure against
   the link instead of the row.

   The whole selector is inside :where(), so it weighs nothing: its job
   is to give an otherwise static control a position and a z-index, and
   never to replace a position a component chose. The switch's input,
   the date field's pick button and select.js's hidden native select are
   all absolutely placed, and at zero weight their own rules win
   position while z-index still applies (review round 2, finding 4). */
:where(:is(.rst-row, .rst-lrow) :is(a[href], button, summary, label, input, select, textarea):not(.rst-row__main > a, .rst-lrow > a.rst-nm, .rst-lrow > a.rst-person)), :where(:is([rst-row], [rst-lrow]) :is(a[href], button, summary, label, input, select, textarea):not([rst-row-main] > a, [rst-lrow] > a.rst-nm, [rst-lrow] > a[rst-person])) { position: relative; z-index: 1; }
```

(f) The action pill keeps its own `position: relative; z-index: 1` (the rule above now does the same for it at zero weight; leave the pill's rule alone, its weight is harmless and it predates this).

Copy tokens.css into both examples.

- [ ] **Step 4: Update the inventory and the fixtures**

In `ui/sizing_test.go`, remove the `{"a.rst-nm", …}` row from `tapInventory` and add to `tapExempt`:

```go
	"a.rst-nm":                   "the list grid's stretched identity link: the tap target is the whole row (spec §2)",
	"[rst-lrow] > a[rst-person]": "the list grid's stretched person link: the tap target is the whole row (spec §2)",
	".rst-lrow > a.rst-person":   "the class spelling of the same stretched person link",
```

In `ui/markup_v3_browser_test.go`'s `extraFixture`, after the `rst-card` block that ends with `<p class="rst-count-line">3 of 40</p>`, add:

```html
  <div class="rst-card"><div class="rst-lrow"><a class="rst-person" href="#a"><span class="rst-person__av">A</span><span class="rst-person__meta"><span class="rst-person__name">Ada</span></span></a><span class="rst-cell-mut">Owner</span></div></div>
```

In `ui/ui.go`'s package doc, after the list-grid sentence ending `— no JavaScript:` and before the sample, add:

```go
// A row with a primary link is clickable across its whole width: the
// identity link's ::after covers the row, and every other link, button,
// summary, label and form control in it sits above that overlay. A row
// with no link gets neither, so it never looks clickable.
```

- [ ] **Step 5: Run the drives and the unit gates**

Run the Step 2 command, then `GOFLAGS=-mod=mod go test -run 'TestEveryInteractiveRuleIsInTheTapInventory|TestEveryClassSelectorHasAnAttributeTwin|TestNoAttributeSelectorIsAnOrphan' -count=1 ./ui/`.
Expected: PASS. Mutation checks, each restored: add a space before `:not(` in the attribute half of the lifting rule (the twin gate fails first; fix the class half too and the checkbox leg fails because the checkbox is covered); remove the list-grid `::after` rule (the far-edge legs fail with `DIV`).

- [ ] **Step 6: Run the task gate** (all three commands). `TestBothSpellingsComputeTheSameStyles` compares the new rules in both spellings through `extraFixture`; `TestA11yWalksTheKeyboard` sees the underline on a focused row link.

- [ ] **Step 7: Commit and push**

```bash
git add ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/sizing_test.go ui/markup_v3_browser_test.go ui/ui.go ui/rows_browser_test.go
git commit -m "Make every row with a destination clickable across its whole width

The list grid, which most list screens use, filled on hover as if the
whole row were a link and then only took clicks on the name text. Its
identity link now stretches over the row as list-row-action's always
did, every other control in a row is lifted above that overlay at zero
weight so no component's own positioning changes, the status pill stops
being a dead spot, a row with no link no longer fills on hover, and a
focused row is outlined whole.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 5: Copy review, batch 2: the gallery's words for the row menu, the demo and the two new assets (controller)

Run by the controller with the `copy-review` skill, not by an implementer: the operator reviews the English, and nothing below is written into a tracked file by this task. Tasks 6, 8 and 9 write the approved text.

**Files:**
- Create (gitignored): `copy-review/strings.json` (batch 2), `copy-review/batch2-result.json`

**Interfaces:**
- Consumes: `copy-review/batch1-result.json` (kept by Task 1).
- Produces: `copy-review/batch2-result.json`, `{"action":"approve","strings":[…]}` with the ids below. Every later task that writes one of these strings reads it from that file with `jq -r '.strings[] | select(.id=="<id>") | .text' copy-review/batch2-result.json`.

- [ ] **Step 1: Keep batch 1's result**

```bash
test -f copy-review/batch1-result.json || cp copy-review/result.json copy-review/batch1-result.json
jq -e '.action == "approve"' copy-review/batch1-result.json
```

- [ ] **Step 2: Write `copy-review/strings.json`**

These are the drafts. Every one is a gallery prose key (English is the key, eleven translations follow), so short and plain; no em dashes.

```json
[
  {"id": "gallery.row_menu.blurb", "section": "Gallery: the row menu", "label": "Row menu, one-line description", "text": "A row's other actions, behind a ⋮ button. A destructive one links to its confirm page.", "context": "The description under the row-menu partial's name on the gallery's List screen page. The partial is new: a ⋮ button at the end of a list row that opens a small menu of actions for that row."},
  {"id": "gallery.row_menu.state_mixed", "section": "Gallery: the row menu", "label": "First example's label", "text": "Links, a form and a destructive item", "context": "Label above the first example: a menu with an Edit link, an Archive button that posts a form, and a destructive 'Delete order…' link, separated by a rule."},
  {"id": "gallery.row_menu.note_mixed", "section": "Gallery: the row menu", "label": "First example's note", "text": "Put the destructive item last and end its label with …. It opens a page that asks before anything is deleted.", "context": "A note under the first example, telling a developer how to order the items."},
  {"id": "gallery.row_menu.state_links", "section": "Gallery: the row menu", "label": "Second example's label", "text": "Links only, for a list inside a selection form", "context": "Label above the second example: every item is a link. Used when the list sits inside the form that bulk selection submits."},
  {"id": "gallery.row_menu.note_links", "section": "Gallery: the row menu", "label": "Second example's note", "text": "Inside a form, every item must be a link. An action that posts goes through a page of its own.", "context": "Why: a form cannot sit inside another form, so a posting item would submit the whole selection form instead."},
  {"id": "gallery.demo.close_request", "section": "Gallery: the demo application", "label": "Row menu item in the demo's request list", "text": "Close request…", "context": "The destructive item in each row's ⋮ menu on the demo application's list of support requests. The other item is 'Reply', which the demo already says."},
  {"id": "gallery.demo.callout_title", "section": "Gallery: the demo application", "label": "Callout title on the request screen", "text": "Every screen has its own address", "context": "Replaces 'Three screens, three addresses'. The demo used to be one page switching views with CSS; it is now four pages, like a real app, and on a phone its navigation is a list with a back control."},
  {"id": "gallery.demo.callout_body", "section": "Gallery: the demo application", "label": "Callout body on the request screen", "text": "Each screen is a page of its own, like any rastrillo screen, and works the same with JavaScript off. On a phone, the sections are a list and each screen has a way back.", "context": "Replaces 'Every view has its own address, like any rastrillo screen. Turn JavaScript off and it behaves the same. Switching uses CSS.', which is no longer true."},
  {"id": "gallery.assets.shell_js", "section": "Gallery: Getting started, the asset list", "label": "shell.js, one line", "text": "Phone navigation for the sidebar and console shells: the slide between pages, Back that reuses history, and focus returned to the section you left. Deletable on its own.", "context": "One line beside the new vendored file in the list of files every app gets. The existing lines read like 'The busy rule: while a form sends, its button shows a spinner…'."},
  {"id": "gallery.assets.shell_css", "section": "Gallery: Getting started, the asset list", "label": "shell.css, one line", "text": "The slide between pages on a phone, for the sidebar and console shells. Deletable with shell.js.", "context": "One line beside the new vendored stylesheet in the same list."}
]
```

- [ ] **Step 3: Run the review**

Invoke the `copy-review` skill on `copy-review/strings.json` and follow it to the end (serve, wait for the operator, handle a reroll by rewriting and relaunching). On approve:

```bash
jq -e '.action == "approve"' copy-review/result.json && cp copy-review/result.json copy-review/batch2-result.json
jq -r '.strings[] | "\(.id)\t\(.text)"' copy-review/batch2-result.json
```

Expected: ten lines. No commit: `copy-review/` is gitignored and no tracked file changed, so the tree is exactly Task 4's green tree.

---

### Task 6: The `row-menu` partial, `list-row-action`'s `Menu`, and the gallery sample

Spec §3 and §10.5.

**Files:**
- Create: `ui/partials/row-menu.html`, `ui/rowmenu_test.go`, `ui/rowmenu_browser_test.go`
- Modify: `ui/funcs.go` (`rowMenuItems`, `Funcs` doc and map), `ui/funcs_test.go` (13 entries), `ui/partials/list-row-action.html` (`Menu`), `ui/tokens.css` (row-menu form margin, the spinner slot) + both examples, `ui/styleguide.go` (`list-grid` sample), `ui/ui.go` (doc: the list-grid sample), `ui/ui_test.go` (`TestAllPartialsAreDefined`, `allPartials`, `TestEveryControlHasAnAccessibleName`), `ui/markup_v3_browser_test.go` (`extraFixture`: a spinner in a row-menu button)
- Modify: `docs/site/templates.md` (the ```` ```text ```` partial list only: add `row-menu`)
- Modify: `internal/designsystem/samples.go` (a `row-menu` doc after `list-row-action`), `internal/designsystem/prose.go` (the batch 2 strings this task uses), `internal/designsystem/page.go` (the demo's request list gains a kebab per row), `internal/designsystem/a11y_test.go` (`pickPreviewFrames` also scans the row menu's frame)

**Interfaces:**
- Consumes: `optKey`, `optString`, `optPairs`, `deref`, `menuGroup` (`ui/funcs.go`); catalog key `rastrillo.ui.row_menu` (Task 1); `copy-review/batch2-result.json` (Task 5); Task 3's `measureFn`, `assertTargets`; Task 4's `at`, `probe`, `clickAndLand`, `settleUntil`.
- Produces:
  - Partial `row-menu`, data: `Name string` (required), `Items` (required, list of `{Label string; Href string | Action string; Hidden [][2]string (POST only); Danger bool (Href only)}`), `MenuGroup string` (optional).
  - `list-row-action` key `Menu`: the `Items` list alone; the trigger is named for `Main`.
  - `func rowMenuItems(data any) ([]rowMenuItem, error)` registered as the template func `rowMenuItems`; `type rowMenuItem struct{ Label, Href, Action string; Hidden [][2]string; Danger, Rule bool }`.
  - Test helper `func normaliseMarkup(s string) string`.

- [ ] **Step 1: Write the failing unit tests**

Create `ui/rowmenu_test.go`:

```go
package ui

import (
	"html/template"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

func rowMenuData(items ...any) map[string]any {
	return map[string]any{"Name": "Grace Hopper", "Items": items}
}

func TestRowMenuRendersEveryKindOfItem(t *testing.T) {
	got := render(t, "row-menu", rowMenuData(
		map[string]any{"Label": "Edit", "Href": "/orders/AB3PX/edit"},
		map[string]any{"Label": "Archive", "Action": "/orders/AB3PX/archive", "Hidden": [][2]string{{"state", "archived"}, {"from", "list"}}},
		map[string]any{"Label": "Delete order…", "Href": "/orders/AB3PX/delete", "Danger": true},
	))
	for _, want := range []string{
		`<details rst-row-menu name="rst-menus">`,
		`aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Grace Hopper")) + `"`,
		string(rastrillo.Icon("kebab")),
		`<a href="/orders/AB3PX/edit">Edit</a>`,
		// One-button form, Hidden pairs in caller order, no token field:
		// csrf.Protect is an origin check.
		`<form method="post" action="/orders/AB3PX/archive"><input type="hidden" name="state" value="archived"><input type="hidden" name="from" value="list"><button type="submit">Archive</button></form>`,
		`<a class="rst-danger" href="/orders/AB3PX/delete">Delete order…</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if n := strings.Count(got, "<hr>"); n != 1 {
		t.Errorf("%d rules, want 1: %s", n, got)
	}
	if hr, archive, del := strings.Index(got, "<hr>"), strings.Index(got, "Archive</button>"), strings.Index(got, "Delete order…"); !(archive < hr && hr < del) {
		t.Errorf("the rule is not between the last plain item and the first destructive one: %s", got)
	}
}

// The <hr> goes before the first danger item that follows a plain one,
// and nowhere else.
func TestRowMenuDrawsOneRuleBeforeTheFirstDangerAfterAPlainItem(t *testing.T) {
	plain := map[string]any{"Label": "Edit", "Href": "/e"}
	danger := func(n string) map[string]any { return map[string]any{"Label": "Delete " + n + "…", "Href": "/d" + n, "Danger": true} }
	for _, c := range []struct {
		name  string
		items []any
		rules int
	}{
		{"danger only", []any{danger("a")}, 0},
		{"plain only", []any{plain}, 0},
		{"plain then two dangers", []any{plain, danger("a"), danger("b")}, 1},
		{"danger, plain, danger", []any{danger("a"), plain, danger("b")}, 1},
	} {
		got := render(t, "row-menu", rowMenuData(c.items...))
		if n := strings.Count(got, "<hr>"); n != c.rules {
			t.Errorf("%s: %d rules, want %d: %s", c.name, n, c.rules, got)
		}
	}
}

// Invalid items fail at Execute, naming the item, the way dict fails on
// an odd argument count: silently dropping an item, or rendering a
// destructive POST, are the two failures this rules out.
func TestRowMenuRefusesItemsItCannotRender(t *testing.T) {
	tmpl := parseAll(t)
	for _, c := range []struct {
		name string
		data any
		want string
	}{
		{"no Items", map[string]any{"Name": "Grace Hopper"}, "Items"},
		{"empty Items", rowMenuData(), "Items"},
		{"no Label", rowMenuData(map[string]any{"Href": "/e"}), "item 0 has no Label"},
		{"Href and Action", rowMenuData(map[string]any{"Label": "Edit", "Href": "/e", "Action": "/e"}), `item 0 ("Edit") wants exactly one of Href`},
		{"neither", rowMenuData(map[string]any{"Label": "Edit"}), `item 0 ("Edit") wants exactly one of Href`},
		{"Danger with Action", rowMenuData(map[string]any{"Label": "Delete…", "Action": "/d", "Danger": true}), `item 0 ("Delete…") is Danger`},
		{"Hidden without Action", rowMenuData(map[string]any{"Label": "Edit", "Href": "/e", "Hidden": [][2]string{{"a", "b"}}}), `item 0 ("Edit") carries Hidden`},
	} {
		err := tmpl.ExecuteTemplate(&strings.Builder{}, "row-menu", c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", c.name, err, c.want)
		}
	}
}

func TestRowMenuDefaultsToTheSharedGroupAndTakesAnother(t *testing.T) {
	item := map[string]any{"Label": "Edit", "Href": "/e"}
	if got := render(t, "row-menu", rowMenuData(item)); !strings.Contains(got, `name="`+MenuGroupDefault+`"`) {
		t.Errorf("row-menu is outside the shared group by default: %s", got)
	}
	d := rowMenuData(item)
	d["MenuGroup"] = "row-menus-x"
	if got := render(t, "row-menu", d); !strings.Contains(got, `name="row-menus-x"`) || strings.Contains(got, `name="rst-menus"`) {
		t.Errorf("MenuGroup did not replace the default: %s", got)
	}
}

// Review Focus 4: a row name that looks like markup or a placeholder is
// escaped once and printed, never re-substituted.
func TestRowMenuEscapesTheNameItShowsBack(t *testing.T) {
	name := `<b>"Ada" & Co</b> {name}`
	got := render(t, "row-menu", map[string]any{"Name": name, "Items": []any{map[string]any{"Label": "Edit", "Href": "/e"}}})
	if strings.Contains(got, "<b>") {
		t.Errorf("the name reached the page as markup: %s", got)
	}
	want := `aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", name)) + `"`
	if !strings.Contains(got, want) {
		t.Errorf("missing %s in %s", want, got)
	}
	if strings.Count(got, "{name}") != 1 {
		t.Errorf("the name's own {name} was substituted into, or lost: %s", got)
	}
	row := render(t, "list-row-action", map[string]any{"Href": "/p", "Main": name, "Menu": []any{map[string]any{"Label": "Edit", "Href": "/e"}}})
	if strings.Contains(row, "<b>") || !strings.Contains(row, want) {
		t.Errorf("list-row-action's Menu names its trigger unsafely: %s", row)
	}
}

func TestRowMenuWorksForAStructCaller(t *testing.T) {
	type item struct {
		Label, Href, Action string
		Hidden              [][2]string
		Danger              bool
	}
	type menu struct {
		Name  string
		Items []item
	}
	got := render(t, "row-menu", menu{Name: "Grace Hopper", Items: []item{
		{Label: "Archive", Action: "/a", Hidden: [][2]string{{"x", "1"}}},
		{Label: "Delete…", Href: "/d", Danger: true},
	}})
	for _, want := range []string{`action="/a"`, `name="x" value="1"`, `<a class="rst-danger" href="/d">`, "<hr>"} {
		if !strings.Contains(got, want) {
			t.Errorf("struct caller: missing %q in %s", want, got)
		}
	}
}

// listRowActionGolden is list-row-action's output for its full fixture,
// captured before Menu existed. Without Menu the row must be these
// bytes exactly: every app's list screens render through it.
const listRowActionGolden = "<div rst-row>\n  <span rst-row-lead data-lead=\"positive\" aria-hidden=\"true\">RN</span>\n  <span rst-row-main><a href=\"/posts/1\">Release notes, August</a><small rst-row-sub>Published 2 August · 4 min read</small></span>\n  <span rst-status rst-tone=\"positive\">Published</span>\n  <a rst-row-action href=\"/posts/1/edit\" aria-label=\"Edit Release notes, August\">Edit</a>\n</div>"

func fullRowData() map[string]any {
	return map[string]any{
		"Href": "/posts/1", "Main": "Release notes, August",
		"Sub":        "Published 2 August · 4 min read",
		"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
		"ActionAria": "Edit Release notes, August",
		"StatusTone": "positive", "StatusLabel": "Published",
		"Lead":       "positive", "LeadInitial": "RN",
	}
}

func TestListRowActionWithoutMenuIsUnchanged(t *testing.T) {
	if got := render(t, "list-row-action", fullRowData()); got != listRowActionGolden {
		t.Errorf("list-row-action without Menu changed:\n got %q\nwant %q", got, listRowActionGolden)
	}
}

// Pill for the one frequent action, kebab for the rest, in that order
// in the DOM and on screen, the kebab named for Main.
func TestListRowActionWithMenuRendersThePillThenTheKebab(t *testing.T) {
	d := fullRowData()
	d["Menu"] = []any{map[string]any{"Label": "Archive", "Action": "/posts/1/archive"}}
	got := render(t, "list-row-action", d)
	status, pill, kebab := strings.Index(got, "rst-status"), strings.Index(got, "rst-row-action"), strings.Index(got, "<details rst-row-menu")
	if !(status >= 0 && status < pill && pill < kebab) {
		t.Errorf("order is status %d, pill %d, kebab %d; want status, pill, kebab: %s", status, pill, kebab, got)
	}
	if want := `aria-label="` + template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Release notes, August")) + `"`; !strings.Contains(got, want) {
		t.Errorf("the kebab is not named for Main (%s): %s", want, got)
	}
}

// A struct caller written before Menu existed still executes: the key is
// read through opt, the menuGroup precedent.
func TestListRowActionStructCallerWithoutMenuStillExecutes(t *testing.T) {
	type row struct{ Href, Main, Sub, ActionHref, ActionLabel, ActionAria, StatusTone, StatusLabel, Lead, LeadInitial string }
	got := render(t, "list-row-action", row{Href: "/p", Main: "M"})
	if strings.Contains(got, "rst-row-menu") {
		t.Errorf("a struct with no Menu field rendered a menu: %s", got)
	}
}

var betweenTags = regexp.MustCompile(`>\s+<`)

// normaliseMarkup drops the whitespace between tags, which is template
// layout and not content.
func normaliseMarkup(s string) string { return strings.TrimSpace(betweenTags.ReplaceAllString(s, "><")) }

// The styleguide sample is raw markup (samples are handed to the
// gallery unexecuted), so it cannot call the partial; this holds its
// kebab equal to what the partial renders for the same data, so the two
// cannot drift (spec §3.5).
func TestTheListGridSampleCarriesTheRowMenuPartialsOutput(t *testing.T) {
	sample := Styleguide()["list-grid"]
	start := strings.Index(sample, "<details rst-row-menu")
	end := strings.Index(sample[start:], "</details>")
	if start < 0 || end < 0 {
		t.Fatalf("the list-grid sample has no row menu: %s", sample)
	}
	got := normaliseMarkup(sample[start : start+end+len("</details>")])
	want := normaliseMarkup(render(t, "row-menu", rowMenuData(
		map[string]any{"Label": "View", "Href": "/orders/AB3PX"},
		map[string]any{"Label": "Refund order…", "Href": "/orders/AB3PX/refund", "Danger": true},
	)))
	if got != want {
		t.Errorf("the list-grid sample's kebab is not the partial's output.\nsample:  %s\npartial: %s", got, want)
	}
}
```

In `ui/funcs_test.go`, change both name lists to include `"rowMenuItems"` and both counts from `12` to `13` (`TestFuncsRegistersExactlyTheDocumentedHelpers`, `TestFuncsWithReplacesOnlyTAndTf`).

In `ui/ui_test.go`: add `"row-menu"` to `TestAllPartialsAreDefined`'s list and change `36` to `37` in both the check and its message; add to `allPartials()` after `list-row-action`:

```go
		{"row-menu", map[string]any{
			"Name": "Grace Hopper",
			"Items": []any{
				map[string]any{"Label": "Edit", "Href": "/orders/AB3PX/edit"},
				map[string]any{"Label": "Archive", "Action": "/orders/AB3PX/archive", "Hidden": [][2]string{{"state", "archived"}}},
				map[string]any{"Label": "Delete order…", "Href": "/orders/AB3PX/delete", "Danger": true},
			},
		}},
```

and to `TestEveryControlHasAnAccessibleName`:

```go
	menu := render(t, "row-menu", fixtureFor(t, "row-menu"))
	if !strings.Contains(menu, `<summary aria-label="`+template.HTMLEscapeString(defaultTf("rastrillo.ui.row_menu", "name", "Grace Hopper"))+`">`) {
		t.Errorf("the row menu's trigger has no name: %s", menu)
	}
```

(`ui_test.go` already imports `html/template`.)

- [ ] **Step 2: Run to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestRowMenu|TestListRowAction|TestTheListGridSample|TestFuncs|TestAllPartialsAreDefined|TestEveryControlHasAnAccessibleName' -count=1 ./ui/`
Expected: FAIL: `ExecuteTemplate("row-menu"): html/template: no such template "row-menu"`, `Funcs() is missing "rowMenuItems"`, `partial "row-menu" is not defined`. `TestListRowActionWithoutMenuIsUnchanged` passes (the golden was captured from today's partial) and must keep passing.

- [ ] **Step 3: Write `rowMenuItems`**

In `ui/funcs.go`, add `"rowMenuItems": rowMenuItems,` to the map `Funcs` returns, change "must not drop these twelve" to "these thirteen" in its doc, and add to that doc comment after the `stageArt` paragraph:

```go
// rowMenuItems checks the row-menu partial's Items and hands the partial
// back one ready-to-render item each, failing Execute for an item it
// cannot render (see its own comment).
```

Then, after `opt`:

```go
// rowMenuItem is one row-menu entry as the partial renders it. Rule is
// set on the first destructive item that follows a plain one: the
// partial draws its <hr> there and nowhere else.
type rowMenuItem struct {
	Label, Href, Action string
	Hidden              [][2]string
	Danger, Rule        bool
}

// rowMenuItems validates row-menu's Items, a dict-built list or a slice
// of structs, and fails loudly — at Execute, the way dict does for an
// odd argument count — on an item it cannot render:
//
//   - no Label;
//   - both or neither of Href (a link) and Action (a POST);
//   - Danger without Href. A destructive item is a link to its confirm
//     page (SKILL.md §7: confirm-form on its own URL, never fired from
//     the row), so Action alone has nowhere to confirm;
//   - Hidden without Action. Hidden fields ride a POST.
//
// Silently dropping an item, or rendering a destructive POST, are the
// two failures this exists to rule out.
func rowMenuItems(data any) ([]rowMenuItem, error) {
	v := optKey(data, "Items")
	if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || v.Len() == 0 {
		return nil, fmt.Errorf("ui: row-menu wants Items, a non-empty list of items")
	}
	out := make([]rowMenuItem, 0, v.Len())
	plain, ruled := false, false
	for i := 0; i < v.Len(); i++ {
		item, ok := deref(v.Index(i))
		if !ok {
			return nil, fmt.Errorf("ui: row-menu item %d is nil", i)
		}
		it := item.Interface()
		m := rowMenuItem{Label: optString(it, "Label"), Href: optString(it, "Href"), Action: optString(it, "Action"), Hidden: optPairs(it, "Hidden")}
		if d := optKey(it, "Danger"); d.IsValid() && d.Kind() == reflect.Bool {
			m.Danger = d.Bool()
		}
		switch {
		case m.Label == "":
			return nil, fmt.Errorf("ui: row-menu item %d has no Label", i)
		case (m.Href == "") == (m.Action == ""):
			return nil, fmt.Errorf("ui: row-menu item %d (%q) wants exactly one of Href (a link) and Action (a POST)", i, m.Label)
		case m.Danger && m.Href == "":
			return nil, fmt.Errorf("ui: row-menu item %d (%q) is Danger, so it needs Href: a destructive item links to its confirm page", i, m.Label)
		case optKey(it, "Hidden").IsValid() && m.Action == "":
			return nil, fmt.Errorf("ui: row-menu item %d (%q) carries Hidden, which only a POST (Action) item sends", i, m.Label)
		}
		if m.Danger && plain && !ruled {
			m.Rule, ruled = true, true
		}
		if !m.Danger {
			plain = true
		}
		out = append(out, m)
	}
	return out, nil
}
```

- [ ] **Step 4: Write the partial and the `Menu` key**

Create `ui/partials/row-menu.html`:

```html
{{/* row-menu — a row's secondary actions behind a kebab: a native
     <details> in the shared menu group, so opening one closes any other
     menu, with no script. rastrillo.js closes it on an outside click and
     on Escape. Render it in a list grid row's last cell (--rst-cols
     ending in var(--rst-col-menu)) or through list-row-action's Menu.

     Its summary is lifted above the row's overlay (tokens.css, the
     controls-above-the-overlay rule), so tapping it never opens the row;
     the <details> itself is not, so it makes no stacking context and its
     open panel paints over the rows below.

     A POST item is a one-button form: csrf.Protect is an origin check,
     so it carries no token. busy.js shows its spinner beside the label,
     in a slot tokens.css reserves so the item never changes size.

     INSIDE A BULK-SELECTION FORM, EVERY ITEM MUST BE A LINK (Href). A
     POST item is its own <form>, HTML forbids a form inside a form, and
     the parser drops the inner tag: the button would submit the whole
     selection form, every checked box with it, to the wrong action. An
     action that must POST from such a list goes through a page of its
     own, the confirm-page pattern a destructive item already uses.

     Keys:
       Name       string, required — the row's name, used only in the
                  trigger's accessible name ("Actions for {name}")
       Items      list, required — in the order they render:
                    Label   string, required
                    Href    string — a GET link, or
                    Action  string — a POST; exactly one of the two
                    Hidden  [][2]string, optional, POST only, in order
                    Danger  bool, optional, Href only: the destructive
                            item, a link to its confirm page. Put it
                            last, its label ending in "…"; a rule is
                            drawn before the first one that follows a
                            plain item.
                  rowMenuItems fails Execute on an item that breaks
                  these rules rather than dropping it.
       MenuGroup  string, optional — the <details name> group, read
                  through menuGroup; default rst-menus. */}}
{{define "row-menu"}}<details rst-row-menu name="{{menuGroup .}}">
  <summary aria-label="{{Tf "rastrillo.ui.row_menu" "name" .Name}}">{{icon "kebab"}}</summary>
  <div rst-row-menu-panel>
    {{- range rowMenuItems .}}
    {{- if .Rule}}
    <hr>
    {{- end}}
    {{- if .Action}}
    <form method="post" action="{{.Action}}">{{range .Hidden}}<input type="hidden" name="{{index . 0}}" value="{{index . 1}}">{{end}}<button type="submit">{{.Label}}</button></form>
    {{- else}}
    <a{{if .Danger}} class="rst-danger"{{end}} href="{{.Href}}">{{.Label}}</a>
    {{- end}}
    {{- end}}
  </div>
</details>{{end}}
```

In `ui/partials/list-row-action.html`, add to the Keys comment after `LeadInitial`:

```
       Menu         list, optional — row-menu's Items, rendered as a
                    kebab after the status pill and the action pill (the
                    pill for the one frequent action, the kebab for the
                    rest). Its trigger is named for Main, so there is no
                    second copy of the name to drift. Read through opt,
                    so a struct written before Menu existed still renders.
```

and change the template's tail from

```
  {{- if .ActionHref}}
  <a rst-row-action href="{{.ActionHref}}"{{if .ActionAria}} aria-label="{{.ActionAria}}"{{end}}>{{.ActionLabel}}</a>
  {{- end}}
</div>{{end}}
```

to

```
  {{- if .ActionHref}}
  <a rst-row-action href="{{.ActionHref}}"{{if .ActionAria}} aria-label="{{.ActionAria}}"{{end}}>{{.ActionLabel}}</a>
  {{- end}}
  {{- with opt . "Menu"}}
  {{template "row-menu" dict "Name" $.Main "Items" .}}
  {{- end}}
</div>{{end}}
```

(Without `Menu` the `{{- with}}` trims the newline and indent before it and renders nothing, so the bytes are the golden's.)

- [ ] **Step 5: The CSS and the sample**

In `ui/tokens.css`, after the `.rst-row-menu__panel .rst-danger:hover` rule, add:

```css
/* A POST item is a one-button form: no margin, so it lines up with a
   link item. busy.js puts a spinner in front of a submit button's label,
   and a menu item is not an [rst-btn], so the spinner sits BESIDE the
   label; this reserves its slot at the inline end whether or not the
   item is busy, so a long translated label can never widen the panel or
   wrap mid-submit. The cost is 1.5rem of padding on POST items. */
.rst-row-menu__panel form, [rst-row-menu-panel] form { margin: 0; }
.rst-row-menu__panel button, [rst-row-menu-panel] button { padding-inline-end: calc(0.65rem + 1rem + var(--rst-sp-2)); position: relative; }
.rst-row-menu__panel button > .rst-spin, [rst-row-menu-panel] button > [rst-spin] { inset-block: 0; inset-inline-end: 0.65rem; margin-block: auto; position: absolute; }
```

Copy tokens.css into both examples.

In `ui/styleguide.go`, replace the `list-grid` sample's `<details rst-row-menu …>…</details>` (lines 31-33) with the partial's output for `Name: "Grace Hopper"` and items `View` → `/orders/AB3PX` and `Refund order…` → `/orders/AB3PX/refund` (Danger). Take the markup from the failure message of `TestTheListGridSampleCarriesTheRowMenuPartialsOutput` (its `partial:` line), which is exactly what must be pasted, indented to fit the sample:

```
    <details rst-row-menu name="rst-menus">
      <summary aria-label="Actions for Grace Hopper"><svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="1"/><circle cx="12" cy="5" r="1"/><circle cx="12" cy="19" r="1"/></svg></summary>
      <div rst-row-menu-panel><a href="/orders/AB3PX">View</a><hr><a class="rst-danger" href="/orders/AB3PX/refund">Refund order…</a></div>
    </details>
```

(`Actions for Grace Hopper` is the draft; the approved batch-1 English goes there, which is what the test compares against.) Make the same change to the sample in `ui/ui.go`'s package doc (lines 116-118): the doc's `<details rst-row-menu>` becomes `{{template "row-menu" dict "Name" "Grace Hopper" "Items" (list (dict "Label" "View" "Href" "/orders/AB3PX") (dict "Label" "Refund order…" "Href" "/orders/AB3PX/refund" "Danger" true))}}`, with the sentence before it ending "and the per-row overflow menu is the row-menu partial:".

In `ui/markup_v3_browser_test.go`'s `extraFixture`, change the hand-written row menu's `<button class="rst-danger" type="button">Delete…</button>` to `<button class="rst-danger" type="button"><span class="rst-spin" aria-hidden="true"></span>Delete…</button>`, so the spinner-slot rule is rendered in both spellings.

In `docs/site/templates.md`, add `row-menu` to the ```` ```text ```` partial list (alphabetical position, column layout as the block has it); no prose.

- [ ] **Step 6: Run the unit tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./ui/ ./internal/docsite/`
Expected: PASS, including `TestTheListGridSampleCarriesTheRowMenuPartialsOutput`, `TestIdiomClassesAreStyled` (the sample still carries `rst-danger` and `rst-menus`), `TestRenderEverythingSmoke` (balanced tags, unique ids), `TestTemplatesPageListsEveryPartial`. Mutation check: drop the `{{- if .Rule}}` block and see the rule tests fail; restore.

- [ ] **Step 7: The gallery sample and the demo's kebab**

Read the approved text:

```bash
for id in gallery.row_menu.blurb gallery.row_menu.state_mixed gallery.row_menu.note_mixed gallery.row_menu.state_links gallery.row_menu.note_links gallery.demo.close_request; do
  printf '%s\t%s\n' "$id" "$(jq -r --arg id "$id" '.strings[] | select(.id==$id) | .text' copy-review/batch2-result.json)"; done
```

In `internal/designsystem/samples.go`, after the `list-row-action` doc in the `list-screen` family, add (the English literals are the approved texts from the lines above; the drafts are shown):

```go
				{
					Name:  "row-menu",
					Blurb: "A row's other actions, behind a ⋮ button. A destructive one links to its confirm page.",
					States: []sample{
						{State: "Links, a form and a destructive item", Data: map[string]any{
							"Name": "Grace Hopper",
							"Items": []any{
								map[string]any{"Label": "Edit", "Href": "/orders/AB3PX/edit"},
								map[string]any{"Label": "Archive", "Action": "/orders/AB3PX/archive", "Hidden": [][2]string{{"state", "archived"}}},
								map[string]any{"Label": "Delete order…", "Href": "/orders/AB3PX/delete", "Danger": true},
							},
						}, Note: "Put the destructive item last and end its label with …. It opens a page that asks before anything is deleted."},
						{State: "Links only, for a list inside a selection form", Data: map[string]any{
							"Name": "Alan Turing",
							"Items": []any{
								map[string]any{"Label": "Edit", "Href": "/orders/CD4QY/edit"},
								map[string]any{"Label": "Delete order…", "Href": "/orders/CD4QY/delete", "Danger": true},
							},
						}, Note: "Inside a form, every item must be a link. An action that posts goes through a page of its own."},
					},
				},
```

The sample data ("Edit", "Archive", "Delete order…", the names) stays English on every page and must not be a prose key: `grep -c '^	`Edit`: {' internal/designsystem/prose.go` and the same for the other three must print 0 (they do today).

In `internal/designsystem/page.go`'s `demoTemplate`, in `#view-requests`: change the card's `--rst-cols: minmax(0, 1fr) 120px 120px` to `--rst-cols: minmax(0, 1fr) 120px 120px var(--rst-col-menu)`, add `<span></span>` as the last cell of its head row, and end each of the four data rows (after the `Updated` cell) with, for that row's subject:

```
{{template "row-menu" dict "Name" "Invoice #4471 never arrived" "Items" (list (dict "Label" (P "Reply") "Href" "#view-request") (dict "Label" (P "Close request…") "Href" "#view-request" "Danger" true))}}
```

(`Reply` is already a prose key; `Close request…` is the approved `gallery.demo.close_request`.)

In `internal/designsystem/prose.go`, add one entry per new English string (the blurb, both state labels, both notes, `Close request…`), each with all eleven translations (`ga`, `zh-Hans`, `es`, `hi`, `pt`, `bn`, `ru`, `ja`, `yue`, `vi`, `ar`), raw string literals, in the table's existing style. Draft them from the approved English; keep `…` and `⋮` as they are, translate nothing that is an identifier.

In `internal/designsystem/a11y_test.go`'s `pickPreviewFrames`, replace the component-page tail `return got.Frames[:1]` with:

```go
	// The first frame, and the row menu's wherever it is: spec §10.5 asks
	// for axe on that sample in every theme and scheme, and it is not
	// first on its page.
	picked := got.Frames[:1]
	for _, f := range got.Frames[1:] {
		if f.Of == anchorID("partial", "row-menu") {
			picked = append(picked, f)
		}
	}
	return picked
```

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS (`buildFamilies` now claims `row-menu`; `TestEveryProseKeyIsTranslated` finds all eleven translations; the leak gate finds no prose key in English on a translated page).

- [ ] **Step 8: Write the browser drive**

Create `ui/rowmenu_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// rowMenuPage is three list-grid rows, each with a row menu rendered by
// the partial: a GET item, a POST item carrying two hidden fields, and
// a destructive link. The third row's POST label is forty-odd
// characters, the case the spinner slot exists for. /bottom is the same
// list pushed to the foot of the viewport.
func rowMenuPage(t *testing.T) (map[string]string, chan string) {
	t.Helper()
	row := func(id, name, post string) string {
		return `<div rst-lrow id="row-` + id + `"><a class="rst-nm" href="/go/row-` + id + `">` + name + `</a><span class="rst-m-hide rst-cell-mut">Paid</span>` +
			render(t, "row-menu", map[string]any{"Name": name, "Items": []any{
				map[string]any{"Label": "View", "Href": "/go/view-" + id},
				map[string]any{"Label": post, "Action": "/act/" + id, "Hidden": [][2]string{{"state", "archived"}, {"from", "list"}}},
				map[string]any{"Label": "Delete order…", "Href": "/go/delete-" + id, "Danger": true},
			}}) + `</div>`
	}
	list := `<div rst-card id="list" style="--rst-cols: minmax(0, 1fr) 110px var(--rst-col-menu)">` +
		row("a", "Grace Hopper", "Archive") + row("b", "Alan Turing", "Archive") +
		row("c", "Ada Lovelace", "Archive this order and notify the customer") + `</div>`
	return map[string]string{
		"/":       sizingDoc("row menus", `<div rst-page><p id="outside">Outside every menu.</p>`+list+`</div>`),
		"/bottom": sizingDoc("row menus at the bottom", `<div rst-page><div style="block-size: 150vh"></div>`+list+`</div>`),
		"/go/":    sizingDoc("landed", `<p id="landed">landed</p>`),
	}, make(chan string, 8)
}

func rowMenuRig(t *testing.T, coarse bool, opts ...harness.Option) (*harness.Rig, chan string) {
	t.Helper()
	pages, posted := rowMenuPage(t)
	if coarse {
		opts = append(opts, harness.WithCoarsePointer())
	}
	rig := harness.New(t, func(string) http.Handler {
		mux := sizingMux(t, pages)
		mux.HandleFunc("POST /act/", func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			posted <- r.URL.Path + "?" + string(body)
			http.Redirect(w, r, "/go/done", http.StatusSeeOther)
		})
		return mux
	}, opts...)
	return rig, posted
}

func openMenus(t *testing.T, ctx context.Context) string {
	t.Helper()
	var s string
	chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll("[rst-row-menu]")].filter(d => d.open).map(d => d.closest("[rst-lrow]").id).join(",")`, &s))
	return s
}

// TestRowMenusExcludeEachOtherWithNoScript: the native group, with
// script execution disabled in the engine.
func TestRowMenusExcludeEachOtherWithNoScript(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery),
		chromedp.Click("#row-a summary", chromedp.ByQuery), chromedp.Click("#row-b summary", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if got := openMenus(t, ctx); got != "row-b" {
		t.Errorf("with no script, open menus are %q after opening A then B, want only row-b", got)
	}
}

// TestRowMenuDismissesAndHandsFocusBack: an outside click and Escape
// close it (rastrillo.js), and Escape puts focus back on its summary.
// Clicking an item goes to the item, never the row.
func TestRowMenuDismissesAndHandsFocusBack(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery))
	var focus string
	if err := chromedp.Run(ctx,
		chromedp.Click("#row-a summary", chromedp.ByQuery), chromedp.Click("#outside", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if got := openMenus(t, ctx); got != "" {
		t.Errorf("after an outside click, %q is still open", got)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click("#row-a summary", chromedp.ByQuery), chromedp.Focus("#row-a [rst-row-menu-panel] a", chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Evaluate(`document.activeElement.closest("[rst-lrow]").id + " " + document.activeElement.tagName`, &focus)); err != nil {
		t.Fatal(err)
	}
	if got := openMenus(t, ctx); got != "" || focus != "row-a SUMMARY" {
		t.Errorf("after Escape: open %q, focus on %q; want none open and focus on row-a's summary", got, focus)
	}
	chromedp.Run(ctx, chromedp.Click("#row-a summary", chromedp.ByQuery))
	if got := clickAndLand(t, ctx, probe(t, ctx, "#row-a [rst-row-menu-panel] a", 0.5, 0.5, 0, 0)); got != "/go/view-a" {
		t.Errorf("clicking the View item went to %q, want the item's href, never the row's", got)
	}
}

// TestARowMenuNearTheBottomOpensUpward: anchor positioning's flip-block
// (Chromium implements it; §3.3), so the last row's menu is not cut off.
func TestARowMenuNearTheBottomOpensUpward(t *testing.T) {
	rig, _ := rowMenuRig(t, false)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var g struct{ PanelBottom, SummaryTop, VH float64 }
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 700), chromedp.Navigate(rig.Origin+"/bottom"), chromedp.WaitReady("#list", chromedp.ByQuery),
		chromedp.Evaluate(`window.scrollTo(0, document.documentElement.scrollHeight), true`, nil),
		chromedp.Evaluate(`document.querySelector("#row-c summary").click(), true`, nil))
	at(t, ctx, `(() => { const p = document.querySelector("#row-c [rst-row-menu-panel]").getBoundingClientRect(), s = document.querySelector("#row-c summary").getBoundingClientRect(); return JSON.stringify({PanelBottom: p.bottom, SummaryTop: s.top, VH: innerHeight}); })()`, &g)
	if g.SummaryTop < g.VH-200 {
		t.Fatalf("the last row's kebab is at %.0f in a %.0fpx viewport; it is not near the bottom and this leg proves nothing", g.SummaryTop, g.VH)
	}
	if g.PanelBottom > g.SummaryTop+0.5 {
		t.Errorf("the panel ends at %.0f, below its summary's top (%.0f): it opened downward off the screen", g.PanelBottom, g.SummaryTop)
	}
}

// postBusyJS clicks a row's POST item and reads, in the same task, the item
// and the panel before and after busy.js has put its spinner in.
const postBusyJS = `((row) => {
  const d = document.querySelector("#row-" + row + " [rst-row-menu]"); d.open = true;
  const btn = d.querySelector("[rst-row-menu-panel] button"), panel = d.querySelector("[rst-row-menu-panel]");
  const box = el => { const r = el.getBoundingClientRect(); return [r.width, r.height]; };
  const before = [box(btn), box(panel)];
  btn.click();
  const spin = btn.querySelector("[rst-spin]");
  const sr = spin ? spin.getBoundingClientRect() : null, br = btn.getBoundingClientRect();
  return JSON.stringify({Before: before, After: [box(btn), box(panel)], Spin: !!spin,
    InSlot: !!sr && sr.left >= br.right - 40 && sr.right <= br.right + 0.5,
    Animation: spin ? getComputedStyle(spin).animationName : ""});
})(%q)`

// TestAPostItemsSpinnerNeverMovesTheMenu: busy.js's spinner sits in its
// reserved slot at the inline end, and neither the item's nor the
// panel's box changes size, including for a forty-odd-character label
// at 320px; under reduced motion the ring is still. The request goes to
// the item's own action with only its own fields.
func TestAPostItemsSpinnerNeverMovesTheMenu(t *testing.T) {
	for _, leg := range []struct {
		name    string
		w       int64
		row     string
		reduced bool
	}{{"1280, a short label", 1280, "a", false}, {"320, a long label", 320, "c", false}, {"320, reduced motion", 320, "c", true}} {
		rig, posted := rowMenuRig(t, false)
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		acts := []chromedp.Action{chromedp.EmulateViewport(leg.w, 800)}
		if leg.reduced {
			acts = append(acts, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}))
		}
		acts = append(acts, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery))
		chromedp.Run(ctx, acts...)
		var g struct {
			Before, After [2][2]float64
			Spin, InSlot  bool
			Animation     string
		}
		at(t, ctx, fmt.Sprintf(postBusyJS, leg.row), &g)
		if !g.Spin {
			t.Fatalf("%s: no spinner appeared; busy.js did not run and this leg proves nothing", leg.name)
		}
		if g.Before != g.After {
			t.Errorf("%s: item and panel went from %v to %v when the spinner appeared", leg.name, g.Before, g.After)
		}
		if !g.InSlot {
			t.Errorf("%s: the spinner is not in the reserved slot at the item's inline end", leg.name)
		}
		if leg.reduced && g.Animation != "none" {
			t.Errorf("%s: the spinner animates (%s) under reduced motion", leg.name, g.Animation)
		}
		select {
		case got := <-posted:
			if want := "/act/" + leg.row + "?state=archived&from=list"; got != want {
				t.Errorf("%s: the POST was %q, want %q (its own action, only its own fields, in order)", leg.name, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: the POST item never submitted", leg.name)
		}
		cancel()
	}
}

// TestRowMenuTargetsAreTapsOnAPhone: trigger and items at 44px.
func TestRowMenuTargetsAreTapsOnAPhone(t *testing.T) {
	rig, _ := rowMenuRig(t, true)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#list", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector("#row-a [rst-row-menu]").open = true`, nil))
	requirePointer(t, ctx, true)
	got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document.querySelector("#row-a [rst-row-menu]"))); })()`)
	if len(got) != 4 {
		t.Fatalf("measured %d controls in row A's open menu, want the summary and three items", len(got))
	}
	assertTargets(t, "390 touch, row A's menu", got)
}
```

- [ ] **Step 9: Run the drive**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestRowMenu|TestARowMenuNearTheBottomOpensUpward|TestAPostItemsSpinnerNeverMovesTheMenu' -count=1 -v ./ui/`
Expected: PASS. Mutation check: remove the `padding-inline-end` from the POST-button rule; the 320 long-label leg fails on the item's box; restore.

- [ ] **Step 10: Run the task gate** (all three commands). If `TestPreviewFrameHeightsFitTheirContent` names the new `partial-row-menu` frames, set their heights in `heightOf` to what it measured.

- [ ] **Step 11: Commit and push**

```bash
git add ui/partials/row-menu.html ui/partials/list-row-action.html ui/funcs.go ui/funcs_test.go ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/styleguide.go ui/ui.go ui/ui_test.go ui/rowmenu_test.go ui/rowmenu_browser_test.go ui/markup_v3_browser_test.go docs/site/templates.md internal/designsystem/samples.go internal/designsystem/prose.go internal/designsystem/page.go internal/designsystem/a11y_test.go
git commit -m "Ship the row menu as a partial, and let list-row-action carry one

The per-row kebab was markup every app copied by hand, which is how a
destructive item ended up as a POST button in the sample. row-menu
renders links and one-button POST forms from a checked item list,
refuses a destructive POST or a stray Hidden at Execute, names its
trigger through Tf, and reserves the busy spinner's slot so a long
label never moves the menu. list-row-action reads Menu through opt, so
struct callers and rows without one render byte for byte as before; the
styleguide sample is held equal to the partial's output.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 7: The topbar's menu as a floating card, and `rastrillo.js` learns it

Spec §5 and the topbar half of §10.4. The console's bar gets the same card in Task 9, with its index; its rail is still gated by the old `:has()` rule until then, and its browser drives do not load `rastrillo.js`, so nothing here changes them.

**Files:**
- Modify: `ui/tokens.css`: a card block after the topbar's `@media (min-width: 800px)` block (after line 1772); a keyframe and a reduced-motion rule; the touch block gains the open Menu summary and the account summary in the card
- Modify: `examples/*/static/tokens.css` (copies)
- Modify: `ui/rastrillo.js` (header comment; `MENUS`, `TAIL`, `menuAround`, `closeMenus`, `dismissMenus`)
- Modify: `ui/shim_test.go` (`TestShimContract`, the `TestShimIsSmall` history comment)
- Modify: `ui/shell_browser_test.go` (`TestTheTopbarCollapsesItsTailBehindOneDisclosure`), `ui/browser_test.go` (`menuPage`, `TestMenuExclusivityAndDropdownDismissDrive`)
- Modify: `ui/sizing_test.go` (inventory keys for the card's summaries), `ui/layouts/topbar.html` (the comment at lines 26-33 only)
- Create: `ui/card_browser_test.go`

**Interfaces:**
- Consumes: Task 2/3 helpers (`sizingMux`, `requirePointer`, `settleUntil`, `measureFn`, `assertTargets`, `readTargets`), Task 4's `at`, `probe`, `clickAndLand`, `classSpelling` (`ui/markup_v3_browser_test.go:468`).
- Produces:
  - JS: `MENUS` now includes `[rst-shell-menu][open],.rst-shell__menu[open]`; `var TAIL = "[rst-shell-tail],.rst-shell__tail"`; `function menuAround(node)`.
  - CSS: the card rules for `[rst-shell-topbar]` in `@media (max-width: 799.98px)`; `@keyframes rst-shell-drop`. Task 9 adds the same selectors for `[rst-shell-console]`.
  - Test helpers: `func topbarPage(t *testing.T, dir string) string`, `func cardRig(t *testing.T, pages map[string]string, scripts bool) *harness.Rig`, `const cardJS`, `type cardReading struct{…}`.

- [ ] **Step 1: Write the failing contract test**

In `ui/shim_test.go`'s `TestShimContract`, add to the wanted list:

```go
		// The topbar's and console's narrow Menu is an overlay since H,
		// so it is in the light-dismiss list, in both spellings written
		// out (the rewrite above would give .rst-shell-menu, and the
		// class is .rst-shell__menu), with its tail counted as inside it
		// only while its summary is rendered.
		`MENUS += ",[rst-shell-menu][open],.rst-shell__menu[open]"`,
		`TAIL = "[rst-shell-tail],.rst-shell__tail"`, "menuAround", "getClientRects().length",
```

and replace the comment above the `rst-shell-chrome`/`rst-tblock` check with:

```go
	// The old sidebar drawer and the toggle-block stay out of it: neither
	// is a menu, and dismissing them on an outside click would fight the
	// user. (The topbar's Menu is in, above: it is a card that overlays
	// the page now, which is what light dismiss is for.)
```

Run: `GOFLAGS=-mod=mod go test -run TestShimContract -count=1 ./ui/`
Expected: FAIL, `shim does not mention "MENUS += \",[rst-shell-menu][open],.rst-shell__menu[open]\""` and the three others.

- [ ] **Step 2: Teach `rastrillo.js` the card**

In `ui/rastrillo.js`, in the header comment replace the `Menus:` paragraph with:

```
   Menus: an open <details rst-dropdown>, <details rst-row-menu>, a
   nested <details rst-menu-group>, or the topbar's and console's narrow
   <details rst-shell-menu> card (whose content is its next sibling, the
   tail) closes on an outside click and on Escape, in EITHER spelling,
   so upgrading this file before running `rastrillo markup` leaves no
   dead menus. Exclusivity stays native.
```

Replace the paragraph of the light-dismiss comment that begins `// Shell chrome and the toggle-block are deliberately absent from` and the two `MENUS` lines, and `closeMenus`, with:

```js
  // The old sidebar drawer and the toggle-block are deliberately absent
  // from MENUS: neither is a menu, and closing one because a click
  // landed elsewhere would fight the user. The topbar's and console's
  // narrow Menu IS in, since it became a floating card: an overlay, which
  // is what this list is for. It is not in the rst-menus name group,
  // which is document-wide; there, opening the account menu inside the
  // card would close the card around it.
  var MENUS = "[rst-dropdown][open],[rst-menu-group][open],[rst-row-menu][open]";
  MENUS += "," + MENUS.replace(/\[(rst[-\w]+)\]/g, ".$1");
  // Written out rather than derived: the class spelling of rst-shell-menu
  // is .rst-shell__menu, which the rewrite above would spell wrong.
  MENUS += ",[rst-shell-menu][open],.rst-shell__menu[open]";
  var TAIL = "[rst-shell-tail],.rst-shell__tail";

  // menuAround is closest(MENUS) plus one logical parent. The card's
  // content (the tail) is the shell menu's next SIBLING, not its child,
  // so without this a click on the account menu inside the card would
  // close the card, and Escape from a plain nav link in it would find no
  // menu at all. Only while the Menu summary is rendered: at 800px and
  // up it is display: none, while a <details> opened at 390 and then
  // widened keeps [open], and climbing to it would hand focus to a
  // summary nobody can see.
  function menuAround(node) {
    var d = node.closest(MENUS), t, s;
    if (d) return d;
    t = node.closest(TAIL);
    d = t && t.previousElementSibling;
    s = d && d.matches(MENUS) && d.querySelector("summary");
    return s && s.getClientRects().length ? d : null;
  }

  // except is the clicked node: every menu around it, directly or through
  // menuAround's climb, stays open, which is what keeps a click on a menu
  // item, or on the summary of a menu being opened right now, from
  // closing the thing being used.
  function closeMenus(except) {
    var keep = [], m;
    for (m = except && menuAround(except); m; m = m.parentElement && menuAround(m.parentElement)) keep.push(m);
    document.querySelectorAll(MENUS).forEach(function (d) {
      if (keep.indexOf(d) < 0 && !(except && d.contains(except))) d.open = false;
    });
  }
```

In `dismissMenus`, replace the two lines that compute and climb `host`:

```js
    var host = el && el.closest ? menuAround(el) : null;
    // Climb to the OUTERMOST open menu around the focus. menuAround
    // finds the innermost, which for focus inside a submenu is the
    // submenu, and for focus in the account menu inside the card is the
    // account menu: their summaries are inside the thing about to close,
    // so focusing one would hand focus to something no longer rendered.
    while (host && host.parentElement && menuAround(host.parentElement)) {
      host = menuAround(host.parentElement);
    }
```

(and delete the four-line comment the old climb carried, which the new one replaces). `dismissMenus` still appears exactly three times.

Check the size: `wc -c ui/rastrillo.js` must print under 16384 (about 10,900 expected). In `ui/shim_test.go`, add a paragraph to the history comment above `TestShimIsSmall`, after the paragraph that ends `…select.js is split out because it is a whole widget; …` and before `The cap is still the point`:

```go
// H added the topbar card to light dismiss: 9,784 → <the byte count
// `wc -c ui/rastrillo.js` prints after this step> bytes, for two
// selector constants, menuAround, two call sites and their comments.
// The busy.js split had freed the room; this did not come near the cap.
```

Run: `GOFLAGS=-mod=mod go test -run 'TestShimContract|TestShimIsSmall|TestScriptsAreSelfContained' -count=1 ./ui/`
Expected: PASS.

- [ ] **Step 3: Write the failing card drive**

Create `ui/card_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// topbarPage renders the real topbar layout with a nav whose current
// item is Posts, an account menu, a language menu, and a link in the
// page far enough down that the card never covers it.
func topbarPage(t *testing.T, dir string) string {
	t.Helper()
	src, ok := Layout("topbar")
	if !ok {
		t.Fatal("no topbar layout")
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	for _, def := range []string{
		`{{define "dir"}}` + dir + `{{end}}`,
		`{{define "nav"}}<a id="nav-posts" href="/go/posts" aria-current="page">Posts</a><a id="nav-drafts" href="/go/drafts">Drafts</a><a id="nav-settings" href="/go/settings">Settings</a>{{end}}`,
		`{{define "account"}}<a id="acct-profile" href="/go/profile">Profile</a><a href="/go/signout">Sign out</a>{{end}}`,
		`{{define "locale"}}<details rst-dropdown rst-locale id="bar-locale" name="rst-menus"><summary>Language</summary><div rst-dropdown-menu><a href="/go/en" lang="en">English</a><a href="/go/ga" lang="ga">Gaeilge</a></div></details>{{end}}`,
		`{{define "content"}}<h1>Posts</h1><div class="spacer"></div><p><a id="main-link" href="/go/main">A link in the page</a></p><div class="spacer"></div>{{end}}`,
		`{{define "head"}}<style>.spacer { block-size: 450px; }</style>{{end}}`,
	} {
		template.Must(tmpl.Parse(def))
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatalf("rendering the topbar: %v", err)
	}
	return b.String()
}

// cardRig serves pages with the assets the layouts link. scripts=false
// disables script execution in the engine, which is the no-JavaScript
// half of every claim below.
func cardRig(t *testing.T, pages map[string]string, scripts bool) (*harness.Rig, context.Context, context.CancelFunc) {
	t.Helper()
	pages["/go/"] = sizingDoc("landed", `<p id="landed">landed</p>`)
	rig := harness.New(t, func(string) http.Handler { return sizingMux(t, pages) }, harness.WithCoarsePointer())
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	if !scripts {
		if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
			t.Fatal(err)
		}
	}
	return rig, ctx, cancel
}

// cardJS is one reading of the bar: the card's box, main's box, what is
// open, where focus is, and the current item's marks.
const cardJS = `(() => {
  const q = s => document.querySelector(s);
  const menu = q("[rst-shell-menu], .rst-shell__menu"), tail = q("[rst-shell-tail], .rst-shell__tail");
  const acct = q("[rst-shell-account], .rst-shell__account"), main = q("main");
  const cur = q("#nav-posts"), cs = getComputedStyle(cur), t = tail.getBoundingClientRect(), m = main.getBoundingClientRect();
  const a = document.activeElement;
  return JSON.stringify({MenuOpen: menu.open, AcctOpen: acct.open, TailDisplay: getComputedStyle(tail).display,
    CardLeft: t.left, CardRight: t.right, CardTop: t.top, VW: document.documentElement.clientWidth,
    MainBox: [m.left, m.top, m.width, m.height].map(Math.round).join(","),
    Focus: a ? (a.id || (a.closest("[rst-shell-menu], .rst-shell__menu") ? "menu-summary" : a.closest("[rst-shell-account], .rst-shell__account") ? "account-summary" : a.tagName)) : "none",
    FocusShown: !!a && a.getClientRects().length > 0,
    CurBg: cs.backgroundColor, CurShadow: cs.boxShadow, CurStart: cs.borderInlineStartWidth, CurEnd: cs.borderBlockEndWidth,
    AccentSoft: getComputedStyle(document.documentElement).getPropertyValue("--rst-accent-soft").trim(),
    AcctPanel: getComputedStyle(acct.querySelector("[rst-dropdown-menu], .rst-dropdown__menu")).position,
    Path: location.pathname});
})()`

type cardReading struct {
	MenuOpen, AcctOpen, FocusShown       bool
	TailDisplay, MainBox, Focus, Path    string
	CardLeft, CardRight, CardTop, VW     float64
	CurBg, CurShadow, CurStart, CurEnd   string
	AccentSoft, AcctPanel                string
}

func readCard(t *testing.T, ctx context.Context) cardReading {
	t.Helper()
	var c cardReading
	at(t, ctx, cardJS, &c)
	return c
}

// resolvedColor turns a colour custom property into the colour a
// computed style reports, so the current item's fill can be compared.
func resolvedColor(t *testing.T, ctx context.Context, prop string) string {
	t.Helper()
	var s string
	chromedp.Run(ctx, chromedp.Evaluate(`(() => { const e = document.createElement("span"); e.style.backgroundColor = "var(`+prop+`)"; document.body.appendChild(e); const c = getComputedStyle(e).backgroundColor; e.remove(); return c; })()`, &s))
	return s
}

// TestTheTopbarMenuIsACardOverThePage is §10.4 for the topbar, with
// scripts off and on, LTR and RTL, at 390 with a coarse pointer.
func TestTheTopbarMenuIsACardOverThePage(t *testing.T) {
	for _, dir := range []string{"ltr", "rtl"} {
		for _, scripts := range []bool{false, true} {
			name := dir + map[bool]string{false: ", no script", true: ", scripts on"}[scripts]
			t.Run(name, func(t *testing.T) {
				rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, dir)}, scripts)
				defer cancel()
				load := func() {
					if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery)); err != nil {
						t.Fatal(err)
					}
				}
				load()
				requirePointer(t, ctx, true)
				closed := readCard(t, ctx)
				if err := chromedp.Run(ctx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				open := readCard(t, ctx)
				if !open.MenuOpen {
					t.Fatal("the Menu did not open")
				}
				if open.MainBox != closed.MainBox {
					t.Errorf("opening the card moved the page: main %s -> %s", closed.MainBox, open.MainBox)
				}
				gap := open.VW - open.CardRight
				if dir == "rtl" {
					gap = open.CardLeft
				}
				if gap < 0 || gap > 12+1 {
					t.Errorf("the card's inline-end edge is %.1fpx from the viewport's, want within 0.75rem + 1px", gap)
				}
				if soft := resolvedColor(t, ctx, "--rst-accent-soft"); open.CurBg != soft || open.CurShadow != "none" || open.CurStart != "0px" || open.CurEnd != "0px" {
					t.Errorf("the current item is bg %s shadow %s border-start %s border-end %s; want %s, none, 0, 0", open.CurBg, open.CurShadow, open.CurStart, open.CurEnd, soft)
				}
				// An outside tap over a link in the page closes the card
				// and does not follow the link.
				if got := clickAndLand(t, ctx, probe(t, ctx, "#main-link", 0.5, 0.5, 0, 0)); got != "/" {
					t.Errorf("a tap outside the card followed the link under it to %q", got)
				}
				if c := readCard(t, ctx); c.MenuOpen {
					t.Error("a tap outside the card did not close it")
				}
			})
		}
	}
}

// TestEscapeClosesTheWholeCard: from a plain nav link in the card, and
// from inside the account menu in it, one Escape closes everything and
// focus lands on the Menu summary; opening the account menu keeps the
// card open. Both spellings of the whole topbar.
func TestEscapeClosesTheWholeCard(t *testing.T) {
	for _, spelling := range []string{"attribute", "class"} {
		t.Run(spelling, func(t *testing.T) {
			page := topbarPage(t, "ltr")
			menu, acct := "[rst-shell-menu] > summary", "[rst-shell-account] > summary"
			if spelling == "class" {
				page, menu, acct = classSpelling(t, page), ".rst-shell__menu > summary", ".rst-shell__account > summary"
				if !strings.Contains(page, "rst-shell__tail") {
					t.Fatal("the class spelling of the topbar has no rst-shell__tail; the leg would test the attribute spelling twice")
				}
			}
			rig, ctx, cancel := cardRig(t, map[string]string{"/": page}, true)
			defer cancel()
			open := func() {
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
					chromedp.WaitVisible(menu, chromedp.ByQuery), chromedp.Click(menu, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			open()
			chromedp.Run(ctx, chromedp.Focus("#nav-drafts", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
			if c := readCard(t, ctx); c.MenuOpen || c.Focus != "menu-summary" {
				t.Errorf("Escape from a nav link: open=%v focus=%s, want closed and the Menu summary", c.MenuOpen, c.Focus)
			}
			open()
			chromedp.Run(ctx, chromedp.Click(acct, chromedp.ByQuery))
			if c := readCard(t, ctx); !c.MenuOpen || !c.AcctOpen {
				t.Errorf("opening the account menu inside the card: card open=%v, account open=%v; want both", c.MenuOpen, c.AcctOpen)
			}
			chromedp.Run(ctx, chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
			if c := readCard(t, ctx); c.MenuOpen || c.AcctOpen || c.Focus != "menu-summary" {
				t.Errorf("Escape from inside the account menu: card %v account %v focus %s; want both closed by one Escape and focus on the Menu summary", c.MenuOpen, c.AcctOpen, c.Focus)
			}
		})
	}
}

// TestTheCardSurvivesAResize: open the card at 390 and widen to 1280,
// and the bar is today's (the tail inline, the current item underlined)
// and a click on a link in the page follows it while the <details> is
// still open (no leftover layer); open the account menu in the card,
// widen, and it is today's positioned panel, and Escape focuses its
// visible summary; narrow again and the card is closed, because that
// Escape closed every menu, the hidden one included.
func TestTheCardSurvivesAResize(t *testing.T) {
	rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, "ltr")}, true)
	defer cancel()
	load := func() {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
			chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery),
			chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
	}
	load()
	chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900))
	wide := readCard(t, ctx)
	if !wide.MenuOpen {
		t.Fatal("the <details> closed on the resize; the leftover-layer case this leg is for has not arisen")
	}
	if wide.TailDisplay != "contents" {
		t.Errorf("widened with the card open: the tail is display %s, want today's contents", wide.TailDisplay)
	}
	var underline string
	chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.getElementById("nav-posts")).borderBlockEndWidth`, &underline))
	if underline != "2px" {
		t.Errorf("widened: the current item's underline is %s, want today's 2px", underline)
	}
	if got := clickAndLand(t, ctx, probe(t, ctx, "#main-link", 0.5, 0.5, 0, 0)); got != "/go/main" {
		t.Errorf("widened with the card open: a click on a link in the page went to %q; a leftover layer is swallowing clicks", got)
	}

	load()
	chromedp.Run(ctx, chromedp.Click("[rst-shell-account] > summary", chromedp.ByQuery), chromedp.EmulateViewport(1280, 900))
	if c := readCard(t, ctx); !c.AcctOpen || c.AcctPanel == "static" {
		t.Errorf("widened with the account menu open: open %v, panel position %s; want today's positioned panel", c.AcctOpen, c.AcctPanel)
	}
	chromedp.Run(ctx, chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
	if c := readCard(t, ctx); c.Focus != "account-summary" || !c.FocusShown {
		t.Errorf("Escape after widening focused %q (shown %v); want the account summary, which is visible", c.Focus, c.FocusShown)
	}
	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844))
	if c := readCard(t, ctx); c.MenuOpen {
		t.Error("narrowed again after Escape: the card is open, but Escape closes every menu, the hidden one included")
	}
}

// TestTheCardsControlsAreTaps: the Menu summary, the nav rows and the
// account and language summaries in the open card, at 44px.
func TestTheCardsControlsAreTaps(t *testing.T) {
	rig, ctx, cancel := cardRig(t, map[string]string{"/": topbarPage(t, "ltr")}, true)
	defer cancel()
	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
	got := readTargets(t, ctx, `(() => { `+measureFn+`; return JSON.stringify(measure(document.querySelector("[rst-shell-bar]"))); })()`)
	if len(got) < 6 {
		t.Fatalf("measured %d controls in the open bar, want the brand, the Menu, three nav rows and two summaries", len(got))
	}
	assertTargets(t, "390 touch, the open card", got)
}
```

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTheTopbarMenuIsACardOverThePage|TestEscapeClosesTheWholeCard|TestTheCardSurvivesAResize|TestTheCardsControlsAreTaps' -count=1 ./ui/`
Expected: FAIL: `opening the card moved the page`, `a tap outside the card followed the link`, the current item has an underline, and (scripts on) Escape from a nav link leaves the card open.

- [ ] **Step 4: Write the card CSS**

In `ui/tokens.css`, directly after the topbar's `@media (min-width: 800px) { … }` block (it ends at line 1772), add:

```css
/* The topbar's narrow menu is a floating card: it overlays the page and
   never pushes it down. The open tail is placed against the bar, at its
   inline end (logical insets, so an RTL page anchors it left), scrolling
   itself if a long nav does not fit. The account and language menus
   inside it expand in place: a popover inside a popover has nowhere good
   to go on a phone. It is the menu panels' surface (9px, their border
   and shadow), so every menu reads as one system. The current item is a
   fill and weight, never a border or an inset bar.

   EVERY rule here is scoped below 800px rather than written narrow and
   undone at 800: the wide tail's display: contents removes the tail's
   box and nothing else, so the account panel would stay static, the nav
   would lose its underline, and the layer below would still swallow
   clicks on a desktop someone widened with the menu open. These rules
   come after the narrow ones above and win at equal weight.

   CLOSING ON AN OUTSIDE TAP, with and without scripts: while open, the
   summary grows an invisible ::before over the viewport, under the card
   and over the page. A tap anywhere outside the card lands on it, which
   is a click on the summary, which closes the <details> natively, and
   the tap never reaches the page. It is position: fixed inside the bar,
   so it breaks if an app gives the bar or an ancestor a transform,
   filter or backdrop-filter (each makes a containing block for fixed
   descendants). The bar's z-index 45 is above the in-page panels
   (dropdown 30, row menu 40), so a menu left open without scripts
   cannot paint over the card, and below the skip link (60). rastrillo.js
   adds Escape. */
@media (max-width: 799.98px) {
  .rst-shell-topbar .rst-shell__bar, [rst-shell-topbar] [rst-shell-bar] { position: relative; z-index: 45; }
  .rst-shell-topbar .rst-shell__menu[open] > summary, [rst-shell-topbar] [rst-shell-menu][open] > summary { background: var(--rst-accent-soft); border-color: var(--rst-accent); }
  .rst-shell-topbar .rst-shell__menu[open] > summary::before, [rst-shell-topbar] [rst-shell-menu][open] > summary::before { content: ""; cursor: default; inset: 0; position: fixed; z-index: 1; }
  .rst-shell-topbar .rst-shell__menu[open] + .rst-shell__tail, [rst-shell-topbar] [rst-shell-menu][open] + [rst-shell-tail] { animation: rst-shell-drop 0.14s ease-out; background: var(--rst-surface); border: 1px solid var(--rst-line); border-radius: 9px; box-shadow: var(--rst-shadow-pop); box-sizing: border-box; flex-basis: auto; gap: 0; inline-size: min(20rem, calc(100vw - 1.5rem)); inset-block-start: calc(100% + 6px); inset-inline-end: 0.75rem; max-block-size: calc(100dvh - 5rem); overflow-y: auto; overscroll-behavior: contain; padding: 0.35rem; position: absolute; z-index: 2; }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__nav, [rst-shell-topbar] [rst-shell-tail] > [rst-shell-nav] { align-items: stretch; gap: 0; }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__nav a, [rst-shell-topbar] [rst-shell-tail] > [rst-shell-nav] a { border-block-end: 0; border-radius: var(--rst-radius-sm); color: var(--rst-text); padding-block: 0.55rem; padding-inline: 0.75rem; }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__nav a[aria-current], [rst-shell-topbar] [rst-shell-tail] > [rst-shell-nav] a[aria-current] { background: var(--rst-accent-soft); box-shadow: none; color: var(--rst-accent); font-weight: 600; }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__account, [rst-shell-topbar] [rst-shell-tail] > [rst-shell-account] { border-block-start: 1px solid var(--rst-line); margin-block-start: 0.35rem; padding-block-start: 0.35rem; }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__account > summary, [rst-shell-topbar] [rst-shell-tail] > [rst-shell-account] > summary { justify-content: space-between; padding-inline: 0.75rem; }
  .rst-shell-topbar .rst-shell__tail .rst-dropdown__menu, [rst-shell-topbar] [rst-shell-tail] [rst-dropdown-menu] { border: 0; box-shadow: none; inset: auto; margin: 0; max-block-size: none; min-width: 0; padding-block: 0; padding-inline: 0.5rem 0; position: static; }
}
@keyframes rst-shell-drop { from { opacity: 0; transform: translateY(-4px); } }
@media (prefers-reduced-motion: reduce) {
  .rst-shell-topbar .rst-shell__menu[open] + .rst-shell__tail, [rst-shell-topbar] [rst-shell-menu][open] + [rst-shell-tail] { animation: none; }
}
```

In the touch block, at its end, add:

```css
  /* The card's own summaries: the open Menu (its pressed state is a
     (0,3,1) rule) and the account menu inside it. */
  .rst-shell-topbar .rst-shell__menu[open] > summary, [rst-shell-topbar] [rst-shell-menu][open] > summary { min-block-size: var(--rst-tap); min-inline-size: var(--rst-tap); }
  .rst-shell-topbar .rst-shell__tail > .rst-shell__account > summary, [rst-shell-topbar] [rst-shell-tail] > [rst-shell-account] > summary { min-block-size: var(--rst-tap); }
```

In `ui/sizing_test.go`'s `tapInventory`, add:

```go
	{"[rst-shell-menu][open] > summary", "Shell: the open Menu summary", "rst-shell-menu"},
	{"[rst-shell-account] > summary", "Shell: the account summary inside the card", "rst-shell-account"},
```

In `ui/layouts/topbar.html`, replace the sentence `The sidebar collapses at the same width behind the same icon: two shells, one idiom.` in the comment at lines 26-33 with `Below 800px the open tail is a card over the page (tokens.css), which rastrillo.js closes on an outside tap and on Escape; the sidebar has no such control since H, its phone navigation is an index page.`

Copy tokens.css into both examples.

- [ ] **Step 5: Rewrite the two existing drives the card changes**

In `ui/shell_browser_test.go`'s `TestTheTopbarCollapsesItsTailBehindOneDisclosure`: add `MainTop int` to `barReading`, add `MainTop: Math.round(document.querySelector("main").getBoundingClientRect().top),` to its measure, and after the `open.Rows != 3` check add:

```go
	// Since H the opened tail is a card OVER the page, not a column in
	// the bar's flow: the page under it does not move.
	if open.MainTop != closed.MainTop {
		t.Errorf("opening the disclosure moved the page down %dpx; the tail is a card that overlays it", open.MainTop-closed.MainTop)
	}
```

Update its doc comment's claim 2 to read "Narrow, one control. Below 800px the tail is not drawn until the disclosure is opened, and opened it is a card over the page: three stacked rows that push nothing down."

In `ui/browser_test.go`'s `menuPage` `GET /` body, add inside the `<header rst-shell-bar>` after the account `</details>`:

```go
			`<details rst-shell-menu name="rst-shell-menu" id="shellmenu"><summary id="shellmenu-summary">Menu</summary></details>`+
			`<div rst-shell-tail><a id="tail-link" href="#tail">Tail</a></div>`+
```

and append a leg to `TestMenuExclusivityAndDropdownDismissDrive`, before `rig.Screen(…)`:

```go
	// The shell menu joined light dismiss in H: an outside click closes
	// it, and Escape from a link in its tail (its content, a sibling)
	// closes it and hands focus to its summary. The legacy chrome strip
	// is still left alone.
	var shellAfterOutside, shellAfterEsc bool
	var shellFocus string
	if err := chromedp.Run(ctx,
		chromedp.Click(`#shellmenu-summary`, chromedp.ByQuery),
		chromedp.Click(`#elsewhere`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("shellmenu").open`, &shellAfterOutside),
		chromedp.Click(`#shellmenu-summary`, chromedp.ByQuery),
		chromedp.Focus(`#tail-link`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Evaluate(`document.getElementById("shellmenu").open`, &shellAfterEsc),
		chromedp.Evaluate(`document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : "none"`, &shellFocus),
	); err != nil {
		t.Fatalf("driving the shell menu: %v", err)
	}
	if shellAfterOutside || shellAfterEsc || shellFocus != "shellmenu-summary" {
		t.Errorf("shell menu: open after an outside click %v, after Escape %v, focus %q; want closed, closed, shellmenu-summary", shellAfterOutside, shellAfterEsc, shellFocus)
	}
```

and in its doc comment's bug list replace "shell chrome or the toggle-block swept into the group or the dismiss" with "the old sidebar chrome strip or the toggle-block swept into the group or the dismiss, or the shell menu card left out of the dismiss".

- [ ] **Step 6: Run the drives**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTheTopbarMenuIsACardOverThePage|TestEscapeClosesTheWholeCard|TestTheCardSurvivesAResize|TestTheCardsControlsAreTaps|TestTheTopbarCollapsesItsTailBehindOneDisclosure|TestMenuExclusivityAndDropdownDismissDrive|TestLightDismissWorksInBothSpellings' -count=1 ./ui/`
Expected: PASS. Mutation checks, each restored: delete the `getClientRects().length` condition from `menuAround` (leave `return d;`) → `TestTheCardSurvivesAResize` fails with focus on the hidden Menu summary; delete the summary `::before` rule → the no-script outside-tap leg follows the link.

- [ ] **Step 7: Run the task gate** (all three commands).

- [ ] **Step 8: Commit and push**

```bash
git add ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/rastrillo.js ui/shim_test.go ui/shell_browser_test.go ui/browser_test.go ui/sizing_test.go ui/layouts/topbar.html ui/card_browser_test.go
git commit -m "Open the topbar's narrow menu as a card over the page

The tail used to open into the page flow and shove everything down. It
is now a right-aligned card, closed by an outside tap through an
invisible layer on the summary, which needs no script, and by Escape
through rastrillo.js, which learns the tail is the menu's content even
though it is a sibling. Every card rule is scoped below 800px, so a
window widened with the menu open is the ordinary bar with no layer
left swallowing clicks, and the climb stops at a summary nobody can see.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 8: `shell.js` and `shell.css`, vendored everywhere, with their behaviour driven

Spec §4.5 (the script, the stylesheet, their gates, the vendoring, the gallery's copies) and the scripted legs of §10.3, driven on hand-written markup so they land before the layouts change. The layouts link both files in Task 9.

**Files:**
- Create: `ui/shell.js`, `ui/shell.css`, `ui/shelljs_browser_test.go`, `cmd/rastrillo/shellscaffold_test.go`
- Modify: `ui/ui.go` (embeds, `ShellJS`, `ShellCSS`), `ui/vendored.go` (the set and its comment)
- Modify: `ui/shim_test.go` (`TestShellContract`; `TestScriptsAreSelfContained` covers `shell.js`), `ui/ui_test.go` (the motion gate becomes a helper run over both stylesheets), `ui/markup_v3_test.go` (the twin gates become helpers run over both)
- Modify: `cmd/rastrillo/new.go` (the vendored-files comment at lines 163-181; the `vendoredIsMine` comment in `vendoredTestTemplate`)
- Modify: `internal/designsystem/designsystem.go` (`Render`'s asset map and doc), `internal/designsystem/page.go` (`buildAssets`: two rows), `internal/designsystem/prose.go` (their two blurbs), `internal/designsystem/designsystem_test.go` (`TestTreeShapeIsComplete`)
- Modify: `docs/site/reference/ui.md` (two signature lines in the vendored-assets fence)

**Interfaces:**
- Consumes: `copy-review/batch2-result.json` (`gallery.assets.shell_js`, `gallery.assets.shell_css`); `setSandboxGoEnv`, `repoRoot`, `runNew`, `runDoctor` (cmd/rastrillo); `harness.New`.
- Produces:
  - `func ui.ShellJS() []byte`, `func ui.ShellCSS() []byte`; `ui.VendoredNames()` = `tokens.css, theme.css, shell.css, rastrillo.js, busy.js, shell.js, select.js, datetime.js, calendar.js`.
  - The page vocabulary shell.js reads (Task 9's layouts write it): a root `[rst-shell-sidebar~="index"|"page"]` or `[rst-shell-console~=…]` (and `.rst-shell-sidebar--index` …), a back link `[rst-shell-back] a[href]`, nav links `[rst-shell-nav] a[href]`; the sessionStorage keys `rst-shell-return` and `rst-shell-back`.
  - `func assertReducedMotion(t *testing.T, name, css string)`, `func assertTwins(t *testing.T, name, css string, floor int)`, `func assertNoOrphans(t *testing.T, name, css string)`.

- [ ] **Step 1: Write the failing unit tests**

In `ui/shim_test.go`, add `"shell.js": string(ShellJS()),` to `TestScriptsAreSelfContained`'s map and append:

```go
// shell.js holds to the contract every scaffolded script does, with an
// 8 KiB cap of its own (busy.js's precedent): the three behaviours it
// exists for, named; both spellings of what it reads; the storage guard;
// the prerender and pageswap facts its ordering depends on.
func TestShellContract(t *testing.T) {
	js := string(ShellJS())
	for _, want := range []string{
		// 1. Direction.
		"pagereveal", "viewTransition.types.add", `"back"`, `"forward"`, "navigationType", `"traverse"`,
		// 2. History reuse, only when proven.
		"history.back()", "navigation.entries()", "sameDocument", "e.button !== 0", "defaultPrevented",
		// 3. Focus return, record first, never while prerendering.
		"rst-shell-return", "pageswap", "pagehide", "document.prerendering", "decodeURIComponent", "sessionStorage",
		// Both spellings.
		"[rst-shell-back]", ".rst-shell__back", `[rst-shell-sidebar~="index"]`, ".rst-shell-sidebar--index",
		`[rst-shell-console~="page"]`, ".rst-shell-console--page",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("shell.js does not mention %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(js), "/*") || !strings.Contains(js, "(function () {") || !strings.HasSuffix(strings.TrimSpace(js), "})();") {
		t.Error("shell.js should be its contract comment and a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") || strings.Contains(js, "\t") {
		t.Error("shell.js must stay CSP-clean and use two-space indentation")
	}
	if n := len(js); n > 8*1024 {
		t.Fatalf("shell.js is %d bytes; keep it readable in one sitting (8 KiB)", n)
	}
}
```

In `ui/ui_test.go`, turn the motion gate into a helper: rename the body of `TestReducedMotionDisablesEveryTransition` to `func assertReducedMotion(t *testing.T, name, css string)` (replacing its first line `css := string(TokensCSS())`, and "tokens.css" with `name` in its two fatal messages), then:

```go
func TestReducedMotionDisablesEveryTransition(t *testing.T) {
	assertReducedMotion(t, "tokens.css", string(TokensCSS()))
}

// shell.css is the other stylesheet a shell links, and every rule in it
// animates: the gate holds it to the same exact-selector rule, unchanged
// in what it accepts, so the slide cannot escape it.
func TestShellCSSReducedMotionDisablesEveryAnimation(t *testing.T) {
	assertReducedMotion(t, "shell.css", string(ShellCSS()))
}
```

In `ui/markup_v3_test.go`, do the same for the twin gates: `func assertTwins(t *testing.T, name, css string, floor int)` holds the body of `TestEveryClassSelectorHasAnAttributeTwin` (`css` a parameter; every `"tokens.css:%d"` becomes `name+":%d"`; the `paired < 300` check becomes `paired < floor` with the message `"only %d selectors are paired in %s, the floor is %d, so this is a wholesale loss rather than an edit"`), and `func assertNoOrphans(t *testing.T, name, css string)` holds `TestNoAttributeSelectorIsAnOrphan`'s. Then:

```go
func TestEveryClassSelectorHasAnAttributeTwin(t *testing.T) {
	assertTwins(t, "tokens.css", string(TokensCSS()), 300)
}

func TestNoAttributeSelectorIsAnOrphan(t *testing.T) {
	assertNoOrphans(t, "tokens.css", string(TokensCSS()))
}

// shell.css is written with exactly one pair — the console bar's
// view-transition-name — and its floor is that one, so the gate cannot
// pass on a shell.css that lost it (review round 2, finding 14): the
// 300 floor tokens.css carries would make any small file fail.
func TestShellCSSPairsBothSpellings(t *testing.T) {
	assertTwins(t, "shell.css", string(ShellCSS()), 1)
	assertNoOrphans(t, "shell.css", string(ShellCSS()))
}
```

Run: `GOFLAGS=-mod=mod go test -run 'TestShellContract|TestScriptsAreSelfContained|TestReducedMotion|TestShellCSS|TestEveryClassSelector|TestNoAttributeSelector' -count=1 ./ui/`
Expected: FAIL to compile, `undefined: ShellJS`, `undefined: ShellCSS`.

- [ ] **Step 2: Write `ui/shell.js`**

```js
/* shell.js — phone navigation for the sidebar and console shells.
   First-party, dependency-free, CSP-clean, and optional: delete it (and
   its <script> tag) and every page still works, because the index and
   the back control are ordinary server-rendered pages and links. It is
   app-owned from the moment it is scaffolded.

   What it reads, all written by the layouts, in either spelling:
     [rst-shell-sidebar~="index"|"page"], [rst-shell-console~=…]
                           which view the server rendered (the "view"
                           block): .rst-shell-sidebar--index and so on
     [rst-shell-back] a    the back control; its href is the "up" block
     [rst-shell-nav] a     the index's rows

   What it adds, three things:
   1. Direction. In pagereveal it types the view transition "back" or
      "forward", which shell.css turns into a slide.
   2. Back reuses history. A plain click on the back control calls
      history.back() when, and only when, the Navigation API proves the
      entry behind this one is the up page, in another document; the
      index then comes back from the back/forward cache as it was left.
   3. Focus returns to the section you left. Leaving a content page
      records its path in sessionStorage; revealing the index at phone
      width focuses the nav link to that path, or else the one the URL's
      #fragment names.

   The layouts load it with blocking="render": the direction is set in
   pagereveal, at the first render, and a deferred script can run after
   it. An engine that ignores blocking gets an untyped transition, which
   shell.css draws as a cross-fade. */
(function () {
  "use strict";

  var ROOT = "[rst-shell-sidebar],[rst-shell-console],.rst-shell-sidebar,.rst-shell-console";
  var INDEX = '[rst-shell-sidebar~="index"],[rst-shell-console~="index"],.rst-shell-sidebar--index,.rst-shell-console--index';
  var PAGE = '[rst-shell-sidebar~="page"],[rst-shell-console~="page"],.rst-shell-sidebar--page,.rst-shell-console--page';
  var BACK = "[rst-shell-back] a[href],.rst-shell__back a[href]";
  var NAV = "[rst-shell-nav] a[href],.rst-shell__nav a[href]";
  var NARROW = "(max-width: 799.98px)";
  var RETURN = "rst-shell-return", WENT_BACK = "rst-shell-back";

  // sessionStorage can throw: storage disabled, a quota, a sandboxed
  // frame. Any throw reads as "no record", so the fragment rule below
  // still works.
  function store(fn) {
    try { return fn(window.sessionStorage); } catch (e) { return null; }
  }
  // One-shot: read and remove, so a record is consumed at most once.
  function take(key) {
    return store(function (s) { var v = s.getItem(key); s.removeItem(key); return v; });
  }
  // A URL as this script compares URLs: resolved against the page, path
  // and query, fragment ignored; null for another origin.
  function place(href) {
    var u = new URL(href, location.href);
    return u.origin === location.origin ? u.pathname + u.search : null;
  }
  function view(sel) {
    var r = document.querySelector(ROOT);
    return !!r && r.matches(sel);
  }

  // 3. Focus return, read when an index is revealed and never while
  // prerendering: a prerendered index runs its first scripts with a copy
  // of storage taken before the reader left the page, and pagereveal at
  // activation is the first moment the fresh record is there. The record
  // wins over the fragment: after going back to /#nav-invoices and then
  // visiting Orders, a history return restores the old fragment.
  function returnFocus() {
    if (document.prerendering || !view(INDEX) || !matchMedia(NARROW).matches) return;
    var rec = take(RETURN), links = document.querySelectorAll(NAV), hit = null, i, id;
    for (i = 0; rec && !hit && i < links.length; i++) {
      if (place(links[i].href) === rec) hit = links[i];
    }
    if (!hit && location.hash) {
      try { id = decodeURIComponent(location.hash.slice(1)); } catch (e) { id = location.hash.slice(1); }
      hit = document.getElementById(id);
      if (hit && !hit.matches(NAV)) hit = null;
    }
    if (hit) hit.focus();
  }

  // 1. Direction. Back when the page being left set the one-shot flag
  // because its back control was used, or when the Navigation API
  // reports a traverse to an earlier entry (the browser's own Back);
  // forward otherwise. A URL pattern cannot decide it: the new page
  // cannot know which URL is the index, and the pattern is the app's.
  function reveal(e) {
    var back = take(WENT_BACK) === "1", a = window.navigation && navigation.activation;
    if (!back && a && a.navigationType === "traverse" && a.from && a.entry) back = a.entry.index < a.from.index;
    if (e.viewTransition) e.viewTransition.types.add(back ? "back" : "forward");
    returnFocus();
  }
  if ("onpagereveal" in window) window.addEventListener("pagereveal", reveal);
  else window.addEventListener("pageshow", returnFocus);

  // The record is written when a content page is LEFT, which covers
  // every way back: the back control, the browser's Back, a deep link.
  // pageswap fires on the outgoing document for every navigation, the
  // activation of a prerendered page included, before the new one is
  // revealed; pagehide is the fallback where there is no pageswap.
  // Never both: a pagehide landing after the index took the record
  // would leave a stale one behind.
  function leave() {
    if (view(PAGE)) store(function (s) { s.setItem(RETURN, location.pathname + location.search); });
  }
  window.addEventListener("onpageswap" in window ? "pageswap" : "pagehide", leave);

  // 2. Back reuses history, only when it can prove what is behind: a
  // plain primary click, the Navigation API, and the entry before this
  // one a DIFFERENT document whose path and query are the back link's
  // (index, page, #section on the same page: Back must not stop at the
  // fragment). A redirect behind "up" fails the test and the link is
  // followed. Anything unproven follows the link and history grows by
  // one, which is the scriptless behaviour, not a failure. The referrer
  // is not consulted: it names the document's referrer, not the entry
  // behind this one.
  document.addEventListener("click", function (e) {
    var a = e.target.closest && e.target.closest(BACK), nav = window.navigation, cur, prev, want;
    if (!a || e.defaultPrevented || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    store(function (s) { s.setItem(WENT_BACK, "1"); });
    cur = nav && nav.currentEntry;
    if (!cur || cur.index < 1) return;
    prev = nav.entries()[cur.index - 1];
    want = place(a.href);
    if (want !== null && prev && !prev.sameDocument && prev.url && place(prev.url) === want) {
      e.preventDefault();
      history.back();
    }
  });
})();
```

- [ ] **Step 3: Write `ui/shell.css`**

```css
/* shell.css — the slide between the index and a page on a phone, for
   the sidebar and console shells, the only layouts that link it.

   It is a file of its own because the opt-in cannot be scoped: written
   in tokens.css, @view-transition would opt every page of every shell
   into cross-document transitions at narrow widths, the sign-in page
   included. Both documents must opt in, and a stylesheet only these two
   shells link is exactly that set. Delete it (and its <link>) and
   navigation is instant; nothing else changes.

   shell.js types each transition "forward" or "back" in pagereveal.
   Each :active-view-transition-type() selector is a rule of its own: an
   engine that does not know the pseudo-class drops the whole rule, and a
   co-listed selector would take its neighbours with it (the trap the
   console's rules in tokens.css record). The slide is a physical
   translateX, so a right-to-left page has four rules of its own that
   slide the other way. Under reduced motion nothing opts in and
   navigation is instant; the block at the end sets every animation here
   to none as well, which is the form the motion gate reads. */
@media (max-width: 799.98px) and (prefers-reduced-motion: no-preference) {
  @view-transition { navigation: auto; }
  ::view-transition-group(root) { animation-duration: 0.32s; animation-timing-function: cubic-bezier(0.2, 0.8, 0.2, 1); }
  html:active-view-transition-type(forward)::view-transition-old(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-out-start; }
  html:active-view-transition-type(forward)::view-transition-new(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-in-end; }
  html:active-view-transition-type(back)::view-transition-old(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-out-end; z-index: 1; }
  html:active-view-transition-type(back)::view-transition-new(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-in-start; }
  html[dir="rtl"]:active-view-transition-type(forward)::view-transition-old(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-out-start-rtl; }
  html[dir="rtl"]:active-view-transition-type(forward)::view-transition-new(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-in-end-rtl; }
  html[dir="rtl"]:active-view-transition-type(back)::view-transition-old(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-out-end-rtl; z-index: 1; }
  html[dir="rtl"]:active-view-transition-type(back)::view-transition-new(root) { animation: 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) both rst-shell-in-start-rtl; }
  /* The console's bar holds still while the page under it slides. */
  .rst-shell-console > .rst-shell__bar, [rst-shell-console] > [rst-shell-bar] { view-transition-name: rst-shell-bar; }
}
@keyframes rst-shell-in-end { from { transform: translateX(100%); } }
@keyframes rst-shell-out-start { to { opacity: 0.7; transform: translateX(-25%); } }
@keyframes rst-shell-in-start { from { opacity: 0.7; transform: translateX(-25%); } }
@keyframes rst-shell-out-end { to { transform: translateX(100%); } }
@keyframes rst-shell-in-end-rtl { from { transform: translateX(-100%); } }
@keyframes rst-shell-out-start-rtl { to { opacity: 0.7; transform: translateX(25%); } }
@keyframes rst-shell-in-start-rtl { from { opacity: 0.7; transform: translateX(25%); } }
@keyframes rst-shell-out-end-rtl { to { transform: translateX(-100%); } }
@media (prefers-reduced-motion: reduce) {
  html:active-view-transition-type(forward)::view-transition-old(root) { animation: none; }
  html:active-view-transition-type(forward)::view-transition-new(root) { animation: none; }
  html:active-view-transition-type(back)::view-transition-old(root) { animation: none; }
  html:active-view-transition-type(back)::view-transition-new(root) { animation: none; }
  html[dir="rtl"]:active-view-transition-type(forward)::view-transition-old(root) { animation: none; }
  html[dir="rtl"]:active-view-transition-type(forward)::view-transition-new(root) { animation: none; }
  html[dir="rtl"]:active-view-transition-type(back)::view-transition-old(root) { animation: none; }
  html[dir="rtl"]:active-view-transition-type(back)::view-transition-new(root) { animation: none; }
}
```

- [ ] **Step 4: Embed and vendor them**

In `ui/ui.go`, after the `calendar.js` embed, add:

```go
//go:embed shell.js
var shellJS []byte

//go:embed shell.css
var shellCSS []byte
```

and after `CalendarJS`:

```go
// ShellJS returns shell.js — the sidebar and console shells' phone
// navigation: the slide's direction, a back control that reuses history
// when it can prove what is behind it, and focus returned to the section
// the reader left. Delivered once by rastrillo new like ShimJS and
// app-owned from then on; linked only by the sidebar and console
// layouts, and optional there: without it the index and the back
// control are ordinary pages and links.
func ShellJS() []byte { return shellJS }

// ShellCSS returns shell.css — the slide between the index and a page
// on a phone. It is a stylesheet of its own because the cross-document
// opt-in cannot be scoped from tokens.css, where it would opt every
// shell's pages in, the sign-in page included.
func ShellCSS() []byte { return shellCSS }
```

In `ui/vendored.go`, set `vendoredNames` to `{"tokens.css", "theme.css", "shell.css", "rastrillo.js", "busy.js", "shell.js", "select.js", "datetime.js", "calendar.js"}`, add `"shell.css": ShellCSS(),` and `"shell.js": ShellJS(),` to `VendoredAssets`' map, and rewrite the order comment: "the structural stylesheet, the theme that colours it and the shells' slide, then the scripts: the shim and the busy rule beside it, the shells' navigation, and calendar.js last because it is the only one that is not an enhancement in its own right: it draws the month grid datetime.js asks it for."

In `docs/site/reference/ui.md`, add `func ShellJS() []byte` and `func ShellCSS() []byte` as the last two lines of the Go fence under `## The vendored assets`.

In `cmd/rastrillo/new.go`, add to the vendored-files comment (lines 163-181) a line for each: `shell.js and shell.css — the sidebar and console shells' phone navigation; written for every shell like the rest (select.js and calendar.js already ship to apps that never link them), and deletable where the shell does not link them.` In `vendoredTestTemplate`, change the `vendoredIsMine` literal to:

```go
var vendoredIsMine = map[string]bool{
	// "tokens.css": true,
	//
	// Only the sidebar and console shells link shell.js and shell.css.
	// An app on another shell that deletes them records it here, or this
	// test fails on the missing files:
	// "shell.js": true, "shell.css": true,
}
```

(Doctor strips comments before reading the map (`uncommented`, `doctor.go`), so the example entries are not read as claims.)

Run: `GOFLAGS=-mod=mod go test -count=1 ./ui/ ./cmd/rastrillo/ ./internal/docsite/`
Expected: `./ui/` PASS (the Step 1 tests, `TestVendoredNamesMatchVendoredAssets`); `./cmd/rastrillo/` PASS (the scaffold writes and pins the new files through `VendoredAssets`; `TestDoctorIsCleanOnAFreshScaffold` counts `VendoredNames()`); docsite PASS.

- [ ] **Step 5: Write the failing upgrade test**

Create `cmd/rastrillo/shellscaffold_test.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldWithReplace runs rastrillo new with args, points the app at
// this checkout, and tidies it, the scratch-module pattern
// TestScaffoldMigratesAndPassesCheck uses.
func scaffoldWithReplace(t *testing.T, name string, args ...string) string {
	t.Helper()
	root := repoRoot(t)
	if err := runNew(append(args, name)); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(name, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = name
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy:\n%s", out)
	}
	dir, _ := filepath.Abs(name)
	return dir
}

func goTestIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"test", "-count=1"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestAnAppMissingTheShellFilesIsToldAndFixed is spec §10.6's upgrade
// leg. Upgrading the module adds two files to the app's own vendoring
// test, and an app that has not got them fails it with the existing
// missing-file diagnostic (its test file is not regenerated by a module
// upgrade, so the doctor hint on the mismatch branch never shows);
// `rastrillo doctor --fix` writes them and the test passes; an app that
// deletes them on purpose and says so in vendoredIsMine passes too.
func TestAnAppMissingTheShellFilesIsToldAndFixed(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "upapp", "--shell=topbar")
	static := filepath.Join(dir, "internal", "upapp", "static")
	pin := filepath.Join(dir, "internal", "upapptest", "vendored_test.go")
	remove := func() {
		for _, n := range []string{"shell.js", "shell.css"} {
			if err := os.Remove(filepath.Join(static, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func() (string, error) {
		return goTestIn(dir, "./internal/upapptest/", "-run", "TestVendoredAssetsMatchTheLibrary")
	}

	if out, err := run(); err != nil {
		t.Fatalf("a fresh scaffold fails its own vendoring test:\n%s", out)
	}
	remove()
	out, err := run()
	if err == nil || !strings.Contains(out, "read vendored shell.js") || !strings.Contains(out, "read vendored shell.css") {
		t.Fatalf("an app missing the shell files: err %v, want the missing-file diagnostic for both:\n%s", err, out)
	}
	if err := runDoctor([]string{"--fix", dir}); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	if out, err := run(); err != nil {
		t.Fatalf("after doctor --fix the vendoring test still fails:\n%s", out)
	}

	remove()
	src, _ := os.ReadFile(pin)
	claimed := strings.Replace(string(src), "var vendoredIsMine = map[string]bool{", "var vendoredIsMine = map[string]bool{\n\t\"shell.js\": true,\n\t\"shell.css\": true,", 1)
	if claimed == string(src) {
		t.Fatal("could not find vendoredIsMine in the scaffolded pin")
	}
	os.WriteFile(pin, []byte(claimed), 0o644)
	if out, err := run(); err != nil {
		t.Fatalf("deleting the shell files and listing them in vendoredIsMine still fails:\n%s", out)
	}
}
```

Run: `GOFLAGS=-mod=mod go test -run TestAnAppMissingTheShellFilesIsToldAndFixed -count=1 ./cmd/rastrillo/`
Expected: PASS already, if Step 4's vendoring is in (the test pins the upgrade path; it was written after the vendoring because nothing before Step 4 knows the files exist). Verify it is not vacuous: temporarily delete `"shell.js": ShellJS(),` from `VendoredAssets` and see the first assertion fail (`read vendored shell.js` never printed); restore.

- [ ] **Step 6: The gallery serves and lists them**

Read the two blurbs: `jq -r '.strings[] | select(.id=="gallery.assets.shell_js" or .id=="gallery.assets.shell_css") | "\(.id)\t\(.text)"' copy-review/batch2-result.json`.

In `internal/designsystem/designsystem.go`'s `Render`, add `"shell.js": ui.ShellJS(), "shell.css": ui.ShellCSS(),` to `out`, and in the doc comment's tree listing replace the two lines `rastrillo.js select.js datetime.js    the framework's four scripts` / `calendar.js  (calendar.js draws…` with:

```go
//	shell.css                             the sidebar and console shells'
//	rastrillo.js busy.js shell.js         slide, and the framework's
//	select.js datetime.js calendar.js     scripts (calendar.js draws the
//	                                      month grid datetime.js opens)
```

In `internal/designsystem/page.go`'s `buildAssets`, keep the rows in `ui.VendoredNames()` order: after the theme loop add

```go
	add(&out, "shell.css", ui.ShellCSS(),
		"The slide between pages on a phone, for the sidebar and console shells. Deletable with shell.js.")
```

and after the `busy.js` row add

```go
	add(&out, "shell.js", ui.ShellJS(),
		"Phone navigation for the sidebar and console shells: the slide between pages, Back that reuses history, and focus returned to the section you left. Deletable on its own.")
```

(the English is the approved text; drafts shown). Add both to `internal/designsystem/prose.go` with eleven translations each (keep `shell.js`, `shell.css` and `Back` as identifiers where the source uses them as names of things; "Back" here is the browser's button and is translated as the platform does).

In `internal/designsystem/designsystem_test.go`'s `TestTreeShapeIsComplete`, add `"shell.js", "shell.css",` to the asset list, and change the comment's "the eight shared assets" to "the ten shared assets".

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS (`TestTheGettingStartedPageWeighsTheRealAssets` finds both rows and both files served; `TestEveryProseKeyIsTranslated` finds the translations).

- [ ] **Step 7: Write the behaviour drive**

Create `ui/shelljs_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// shellDoc is a page in the vocabulary the layouts write (Task 9 makes
// the real layouts write it; this drive is about shell.js, so the markup
// is by hand): a root marked with its view, a back link, a rail of nav
// links, and a main with a same-page link.
func shellDoc(view, up, nav, main string) string {
	return `<!doctype html><html lang="en" dir="ltr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>shell</title>` +
		`<link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/theme.css"><link rel="stylesheet" href="/shell.css">` +
		`<script defer blocking="render" src="/shell.js"></script></head><body>` +
		`<div rst-shell-sidebar="` + view + `"><div rst-shell-back><a id="back" href="` + up + `" rel="up">Sections</a></div>` +
		`<aside rst-shell-rail><nav rst-shell-nav>` + nav + `</nav></aside>` +
		`<main rst-shell-main id="main">` + main + `</main></div></body></html>`
}

// shellSite is three small apps on one origin. / has ids on its nav and
// fragments on its up links; /b/ has neither (focus return must come
// from the record alone); /q/ has an index with a query, nav hrefs
// written absolute, and an id that needs decoding (Review Focus 3). /old
// redirects to /, the "up that redirects" case.
func shellSite(t *testing.T) func(origin string) http.Handler {
	return func(origin string) http.Handler {
		nav := `<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a><a id="nav-r" href="/r/page">Redirecting</a>`
		content := func(name string) string {
			return `<h1>` + name + `</h1><p><a id="section-link" href="#part">To a part of this page</a></p><div style="block-size: 150vh"></div><h2 id="part">Part</h2>`
		}
		navB := `<a href="/b/invoices">Invoices</a><a href="/b/orders">Orders</a>`
		navQ := `<a id="nav-q-orders" href="` + origin + `/q/orders?status=open">Orders</a><a id="nav-café" href="/q/cafe">Café</a>`
		pages := map[string]string{
			"/":           shellDoc("index", "/", nav, `<h1>Home</h1>`),
			"/invoices":   shellDoc("page", "/#nav-invoices", nav, content("Invoices")),
			"/orders":     shellDoc("page", "/#nav-orders", nav, content("Orders")),
			"/r/page":     shellDoc("page", "/old#nav-r", nav, content("Redirecting")),
			"/b/":         shellDoc("index", "/b/", navB, `<h1>B</h1>`),
			"/b/invoices": shellDoc("page", "/b/", navB, content("B invoices")),
			"/q/":         shellDoc("index", "/q/?tab=all", navQ, `<h1>Q</h1>`),
			"/q/orders":   shellDoc("page", "/q/?tab=all#nav-q-orders", navQ, content("Q orders")),
			"/q/cafe":     shellDoc("page", "/q/?tab=all#nav-caf%C3%A9", navQ, content("Café")),
		}
		mux := http.NewServeMux()
		stylesheets(t, mux)
		for name, body := range map[string][]byte{"shell.js": ShellJS(), "shell.css": ShellCSS()} {
			body, ct := body, map[bool]string{true: "text/javascript", false: "text/css"}[strings.HasSuffix(name, ".js")]
			mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", ct)
				w.Write(body)
			})
		}
		mux.HandleFunc("GET /old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusFound) })
		for path, html := range pages {
			html, pattern := html, "GET "+path
			if strings.HasSuffix(path, "/") {
				pattern += "{$}"
			}
			mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, html)
			})
		}
		return mux
	}
}

// tab opens a fresh tab — a fresh session history, so "what is behind
// this entry" is only what the leg put there — at 390 wide, with an
// optional script run in every document before the page's own.
func tab(t *testing.T, rig *harness.Rig, init string, media ...*emulation.MediaFeature) (context.Context, func(), *[]string) {
	t.Helper()
	ctx, cancel := chromedp.NewContext(rig.Context())
	ctx, stop := context.WithTimeout(ctx, 90*time.Second)
	thrown := &[]string{}
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*runtime.EventExceptionThrown); ok {
			*thrown = append(*thrown, e.ExceptionDetails.Error())
		}
	})
	acts := []chromedp.Action{chromedp.EmulateViewport(390, 844)}
	if len(media) > 0 {
		acts = append(acts, emulation.SetEmulatedMedia().WithFeatures(media))
	}
	if init != "" {
		acts = append(acts, chromedp.ActionFunc(func(c context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(init).Do(c)
			return err
		}))
	}
	if err := chromedp.Run(ctx, acts...); err != nil {
		t.Fatal(err)
	}
	return ctx, func() { stop(); cancel() }, thrown
}

type shellState struct {
	Path, Focus string
	Len         int
}

// state is where the tab is, how long its history is, and what has
// focus (an id, else the href, else the tag).
func state(t *testing.T, ctx context.Context) shellState {
	t.Helper()
	var s shellState
	at(t, ctx, `JSON.stringify({Path: location.pathname + location.search + location.hash, Len: history.length,
	  Focus: (a => a.id || a.getAttribute("href") || a.tagName)(document.activeElement)})`, &s)
	return s
}

// visit navigates with a real load; follow clicks through the page,
// which is what works after a back/forward-cache restore.
func visit(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		t.Fatal(err)
	}
}

func follow(t *testing.T, ctx context.Context, sel, until string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`setTimeout(() => document.querySelector(%q).click(), 0), true`, sel), nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	settleUntil(t, ctx, until)
}

// TestTheBackControlReusesHistoryWhenItCanProveWhatIsBehind is §10.3's
// scripted legs.
func TestTheBackControlReusesHistoryWhenItCanProveWhatIsBehind(t *testing.T) {
	rig := harness.New(t, shellSite(t))

	t.Run("index, page, back: history reused, focus on the link followed", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
		if after := state(t, ctx); after.Len != before.Len || after.Path != "/" || after.Focus != "nav-invoices" {
			t.Errorf("after the back control: %+v (before it %+v); want history unchanged, at /, focus on nav-invoices", after, before)
		}
		if len(*thrown) > 0 {
			t.Errorf("uncaught: %v", *thrown)
		}
	})

	t.Run("index, page, #fragment, back: the link is followed", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		follow(t, ctx, "#section-link", `location.hash === "#part"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/" && document.activeElement.id === "nav-invoices"`)
		if after := state(t, ctx); after.Len != before.Len+1 || after.Focus != "nav-invoices" {
			t.Errorf("back from a fragment on the same page: %+v (before %+v); want the link followed (history +1) and focus on nav-invoices", after, before)
		}
	})

	t.Run("a deep link, back: the link is followed and the record focuses", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/orders")
		follow(t, ctx, "#back", `location.pathname === "/"`)
		settleUntil(t, ctx, `document.activeElement.id === "nav-orders"`)
	})

	t.Run("no ids, no fragment: focus from the record alone", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/b/invoices")
		follow(t, ctx, "#back", `location.pathname === "/b/"`)
		settleUntil(t, ctx, `document.activeElement.getAttribute("href") === "/b/invoices"`)
	})

	const brokenStorage = `Object.defineProperty(window, "sessionStorage", {configurable: true, get() { throw new DOMException("denied", "SecurityError"); }});`
	t.Run("storage throws, with a fragment: focus from the fragment", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, brokenStorage)
		defer done()
		visit(t, ctx, rig.Origin+"/invoices")
		follow(t, ctx, "#back", `location.pathname === "/"`)
		settleUntil(t, ctx, `document.activeElement.id === "nav-invoices"`)
		if len(*thrown) > 0 {
			t.Errorf("storage that throws broke the script: %v", *thrown)
		}
	})

	t.Run("storage throws, no fragment: browser default, nothing thrown", func(t *testing.T) {
		ctx, done, thrown := tab(t, rig, brokenStorage)
		defer done()
		visit(t, ctx, rig.Origin+"/b/invoices")
		follow(t, ctx, "#back", `location.pathname === "/b/"`)
		time.Sleep(300 * time.Millisecond)
		if s := state(t, ctx); s.Focus != "BODY" {
			t.Errorf("with no record and no fragment, focus is on %q, want the browser's default", s.Focus)
		}
		if len(*thrown) > 0 {
			t.Errorf("uncaught: %v", *thrown)
		}
	})

	t.Run("modified and middle clicks are not intercepted", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		for _, init := range []string{"{ctrlKey: true}", "{metaKey: true}", "{shiftKey: true}", "{button: 1}"} {
			var prevented bool
			// A listener on window runs after shell.js's on document,
			// records whether it was intercepted, then stops the real
			// navigation so the next click starts from the same page.
			js := `(() => { let seen = null; addEventListener("click", e => { seen = e.defaultPrevented; e.preventDefault(); }, {once: true});
			  document.getElementById("back").dispatchEvent(new MouseEvent("click", Object.assign({bubbles: true, cancelable: true, button: 0}, ` + init + `)));
			  return seen; })()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(js, &prevented)); err != nil {
				t.Fatal(err)
			}
			if prevented {
				t.Errorf("a click with %s on the back control was intercepted; it must do what the browser does", init)
			}
		}
		// The control: a plain click IS intercepted here.
		var plain bool
		chromedp.Run(ctx, chromedp.Evaluate(`(() => { let seen = null; addEventListener("click", e => { seen = e.defaultPrevented; }, {once: true});
		  document.getElementById("back").dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, button: 0})); return seen; })()`, &plain))
		if !plain {
			t.Error("CONTROL: a plain click on the back control was not intercepted either, so the modifier legs prove nothing")
		}
	})

	t.Run("an up that redirects: the link is followed", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-r", `location.pathname === "/r/page"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/"`)
		if after := state(t, ctx); after.Len != before.Len+1 {
			t.Errorf("an up behind a redirect: history %d -> %d; the link must be followed, not history reused", before.Len, after.Len)
		}
	})

	t.Run("two sections in a row: the record beats a stale fragment", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/invoices")
		follow(t, ctx, "#back", `location.pathname === "/" && location.hash === "#nav-invoices"`)
		follow(t, ctx, "#nav-orders", `location.pathname === "/orders"`)
		if err := chromedp.Run(ctx, chromedp.Evaluate(`setTimeout(() => history.back(), 0), true`, nil)); err != nil {
			t.Fatal(err)
		}
		settleUntil(t, ctx, `location.pathname === "/" && document.activeElement.id === "nav-orders"`)
	})

	t.Run("query, absolute hrefs and an encoded id (Review Focus 3)", func(t *testing.T) {
		ctx, done, _ := tab(t, rig, "")
		defer done()
		visit(t, ctx, rig.Origin+"/q/?tab=all")
		follow(t, ctx, "#nav-q-orders", `location.pathname === "/q/orders"`)
		before := state(t, ctx)
		follow(t, ctx, "#back", `location.pathname === "/q/" && document.activeElement.id === "nav-q-orders"`)
		if after := state(t, ctx); after.Len != before.Len {
			t.Errorf("an up with a query and an absolute nav href: history %d -> %d; path+query were not compared resolved", before.Len, after.Len)
		}
		ctx2, done2, _ := tab(t, rig, brokenStorage)
		defer done2()
		visit(t, ctx2, rig.Origin+"/q/cafe")
		follow(t, ctx2, "#back", `location.pathname === "/q/"`)
		settleUntil(t, ctx2, `document.activeElement.id === "nav-café"`)
	})
}

// recordReveal logs every pagereveal's transition types into
// sessionStorage, read after all listeners have run.
const recordReveal = `addEventListener("pagereveal", e => { const vt = e.viewTransition; setTimeout(() => {
  const log = JSON.parse(sessionStorage.getItem("reveal-log") || "[]");
  log.push(location.pathname + ":" + (vt ? [...vt.types].join("+") || "untyped" : "none"));
  sessionStorage.setItem("reveal-log", JSON.stringify(log)); }, 0); });`

// TestTheSlideKnowsWhichWayItIsGoing: forward going in, back coming out
// through the back control, and no transition at all under reduced
// motion (shell.css never opts in).
func TestTheSlideKnowsWhichWayItIsGoing(t *testing.T) {
	rig := harness.New(t, shellSite(t))
	for _, reduced := range []bool{false, true} {
		var media []*emulation.MediaFeature
		want := `["/:none","/invoices:forward","/:back"]`
		if reduced {
			media = []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}
			want = `["/:none","/invoices:none","/:none"]`
		}
		ctx, done, _ := tab(t, rig, recordReveal, media...)
		visit(t, ctx, rig.Origin+"/")
		follow(t, ctx, "#nav-invoices", `location.pathname === "/invoices"`)
		follow(t, ctx, "#back", `location.pathname === "/"`)
		time.Sleep(600 * time.Millisecond)
		var log string
		chromedp.Run(ctx, chromedp.Evaluate(`sessionStorage.getItem("reveal-log")`, &log))
		if log != want {
			t.Errorf("reduced motion %v: pagereveal saw %s, want %s", reduced, log, want)
		}
		done()
	}
}
```

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTheBackControlReusesHistoryWhenItCanProveWhatIsBehind|TestTheSlideKnowsWhichWayItIsGoing' -count=1 -v ./ui/`
Expected: PASS. Mutation checks, each restored: make `place()` return `href` unchanged (the query/absolute leg fails); drop `!prev.sameDocument` (the fragment leg fails with history unchanged); swap the order of the record and fragment lookups (the two-sections leg fails with focus on nav-invoices).

- [ ] **Step 8: Run the task gate** (all three commands).

- [ ] **Step 9: Commit and push**

```bash
git add ui/shell.js ui/shell.css ui/ui.go ui/vendored.go ui/shim_test.go ui/ui_test.go ui/markup_v3_test.go ui/shelljs_browser_test.go cmd/rastrillo/new.go cmd/rastrillo/shellscaffold_test.go internal/designsystem/designsystem.go internal/designsystem/page.go internal/designsystem/prose.go internal/designsystem/designsystem_test.go docs/site/reference/ui.md
git commit -m "Add shell.js and shell.css, vendored for every app

The phone index and back control work as plain pages; shell.js makes
them feel like one app: it types the slide's direction, reuses history
on the back control only when the Navigation API proves the entry behind
is the up page in another document, and returns focus to the section
you left from a record written when the page is left, which beats a
stale fragment. The slide's opt-in lives in shell.css because in
tokens.css it could not be scoped away from the sign-in page. Both
files ship through VendoredAssets, so the scaffold, the pin test and
doctor learn them at once, and the motion and twin gates now read
shell.css too.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 9: The sidebar and console as an index and a back control, and the gallery's demo and previews with them

Spec §4.1-§4.4, §4.8, §4.9, and the scriptless, wide, compatibility and a11y legs of §10.3 and §10.4's console. One task because it must be (spec Rollout step 5): the gallery's demo and shell previews render through `ui.Layout`, so the layout change reaches them at once, and the gallery's narrow drives wait for the drawer this removes.

**Files:**
- Create: `ui/testdata/legacy/sidebar.html`, `ui/testdata/legacy/console.html` (the pre-H layouts, verbatim), `ui/index_browser_test.go`
- Modify: `ui/layouts/sidebar.html`, `ui/layouts/console.html` (rewritten below)
- Modify: `ui/tokens.css`: the sidebar block's comments and the legacy drawer rules (lines 1774-1837); the console's `:has()` gate and its wide undo (lines 1883, 1929-1930) scoped to an unmarked root; the card block from Task 7 gains the console; a new index/back block after the console block; the touch block gains the back link and the index rows; `examples/*/static/tokens.css` (copies)
- Modify: `ui/styleguide.go` (`shell-sidebar` sample and its comment), `ui/ui.go` (the shell paragraph of the package doc)
- Modify: `ui/ui_test.go` (`TestEveryChromeShellCollapsesBehindTheMenuIcon`, `TestTheShellsKeepTheirOverridableBlockNames`, `TestIdiomClassesAreStyled`'s shell list, `TestEveryMenuDefaultsToTheSharedExclusivityGroup`'s comment; two new unit tests), `ui/sizing_test.go` (two inventory rows), `ui/markup_v3_browser_test.go` (`extraFixture`: the index and page views of both shells)
- Modify: `ui/shell_browser_test.go` (the rail drive's 390 leg), `ui/console_shell_browser_test.go` (`consolePage`, the fold drive, the `:has()` drive), `ui/card_browser_test.go` (a console card drive)
- Modify: `internal/designsystem/page.go` (the demo as four documents; the sidebar and console previews as two), `internal/designsystem/designsystem.go` (`Render` writes them), `internal/designsystem/prose.go` (the demo callout's two strings replaced), `internal/designsystem/designsystem_test.go` (`TestTreeShapeIsComplete`, `TestNoTwoPageKindsShareAName`), `internal/designsystem/a11y_test.go` (targets, `TestA11yScansTheShellsCollapsed`, `TestA11yReflowsAt320`), `internal/designsystem/browser_test.go` (the demo drive)

**Interfaces:**
- Consumes: Task 1's catalog keys; Task 7's card block and `cardRig`/`readCard`; Task 8's `shell.js`/`shell.css` and their vocabulary; `copy-review/batch2-result.json` (`gallery.demo.callout_title`, `gallery.demo.callout_body`).
- Produces:
  - Layout blocks `view` (default `page`; a page writes `{{define "view"}}index{{end}}`) and `up` (default `/`) in `sidebar` and `console`; block order sidebar `lang, dir, title, head, view, up, title, brand, nav, locale, account, content`, console `lang, dir, title, head, view, brand, account, locale, up, title, nav, content, foot`.
  - Attributes `rst-shell-back`, `rst-shell-title`, values `rst-shell-sidebar="index"|"page"`, `rst-shell-console="index"|"page"` (classes `.rst-shell__back`, `.rst-shell__title`, `.rst-shell-sidebar--index` …).
  - Gallery files `<theme>/<locale>/demo-dashboard.html`, `demo-requests.html`, `demo-request.html`, `shells/sidebar-page.html`, `shells/console-page.html`; funcs `demoPageHref(mount, theme, locale, view string) string`, `shellPageHref(mount, theme, locale, shell string) string`; `renderDemo` and `renderShell` return `map[string][]byte`.
  - Test helpers: `func shellLayoutPage(t *testing.T, src []byte, dir string, defs ...string) string`, `func legacyLayout(t *testing.T, name string) []byte`.

- [ ] **Step 1: Keep the old layouts as fixtures**

```bash
mkdir -p ui/testdata/legacy
git show a7a24dd:ui/layouts/sidebar.html > ui/testdata/legacy/sidebar.html
git show a7a24dd:ui/layouts/console.html > ui/testdata/legacy/console.html
cmp ui/testdata/legacy/sidebar.html ui/layouts/sidebar.html && cmp ui/testdata/legacy/console.html ui/layouts/console.html && echo same
```

Expected: `same` (the layouts are still the old ones at this point).

- [ ] **Step 2: Write the failing unit tests**

In `ui/ui_test.go`:

(a) `TestTheShellsKeepTheirOverridableBlockNames`: change the two entries to

```go
		"sidebar": {"lang", "dir", "title", "head", "view", "up", "title", "brand", "nav", "locale", "account", "content"},
		// ...console comment unchanged, plus: view and up are the phone
		// index's two blocks, as in sidebar; title appears twice because
		// the rail's <h1> is the page's own title, reused rather than
		// redefined.
		"console": {"lang", "dir", "title", "head", "view", "brand", "account", "locale", "up", "title", "nav", "content", "foot"},
```

(b) Replace `TestEveryChromeShellCollapsesBehindTheMenuIcon`'s table and sample loop:

```go
	for _, c := range []struct{ layout, class string }{
		{"topbar", "rst-shell-menu"},
		// console reuses the topbar's control for its bar; its rail is an
		// index page since H, like the sidebar's.
		{"console", "rst-shell-menu"},
	} {
		// … the body of the loop unchanged …
	}
	// The sidebar has no disclosure at all since H: on a phone its
	// navigation is an index page, and every other page carries a back
	// link to it. A drawer control here would be the hamburger the
	// guidance now discourages.
	sidebar, _ := Layout("sidebar")
	if strings.Contains(string(sidebar), "<details") {
		t.Error("layouts/sidebar.html still has a <details>; its phone navigation is the index page")
	}
	if !strings.Contains(string(sidebar), `<div rst-shell-back><a href=`) {
		t.Error("layouts/sidebar.html has no back link")
	}
	if !strings.Contains(Styleguide()["shell-topbar"], `<path d="M4 12h16"/>`) {
		t.Error(`styleguide sample "shell-topbar" shows a collapse summary with no menu icon in it`)
	}
	if s := Styleguide()["shell-sidebar"]; strings.Contains(s, "<details") || !strings.Contains(s, "rst-shell-back") {
		t.Error(`styleguide sample "shell-sidebar" should show the back link and no drawer`)
	}
```

and rename it `TestTheBarShellsCollapseBehindTheMenuIconAndTheSidebarHasNoDrawer`.

(c) In `TestIdiomClassesAreStyled`'s shell list, replace `"rst-shell-chrome"` with `"rst-shell-back", "rst-shell-title"`.

(d) In `TestEveryMenuDefaultsToTheSharedExclusivityGroup`, change the comment `The sidebar's chrome strip is the narrow-screen nav rail: closing it because a filter opened would take the whole navigation away.` to `The sidebar has no disclosure of its own since H; it must not grow one in the menus' group.` (the assertion stays).

(e) Append:

```go
// renderShellLayout renders a shipped layout with extra defines and
// nil data, the way TestLayoutsParseAndRender does.
func renderShellLayout(t *testing.T, name string, defs ...string) string {
	t.Helper()
	src, ok := Layout(name)
	if !ok {
		t.Fatalf("no %s layout", name)
	}
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + p },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	template.Must(tmpl.Parse(`{{define "content"}}<h1>Content</h1>{{end}}`))
	for _, d := range defs {
		template.Must(tmpl.Parse(d))
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// §4.1-§4.2: a page says which view it is with the view block (default
// page), and names its way back with up (default "/", the brand's
// href). The back control's visible label is inside its accessible name
// (WCAG 2.5.3), rel="up" says the relationship, the rail's <h1> is the
// page's title, the drawer is gone, and only these two shells link
// shell.js and shell.css, the script with blocking="render".
func TestTheSidebarAndConsoleNameTheirViewAndTheirWayBack(t *testing.T) {
	label := defaultT("rastrillo.ui.shell_up_label")
	name := defaultTf("rastrillo.ui.shell_up", "name", label)
	if !strings.Contains(name, label) {
		t.Fatalf("the back control's name %q does not contain its visible label %q", name, label)
	}
	for _, shell := range []string{"sidebar", "console"} {
		page := renderShellLayout(t, shell)
		for _, want := range []string{
			`<div rst-shell-` + shell + `="page">`,
			`<div rst-shell-back><a href="/" rel="up" aria-label="` + template.HTMLEscapeString(name) + `">` + template.HTMLEscapeString(label) + `</a></div>`,
			`<h1 rst-shell-title>Hello</h1>`,
			`<link rel="stylesheet" href="/static/shell.css">`,
			`<script defer blocking="render" src="/static/shell.js"></script>`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: missing %q", shell, want)
			}
		}
		if strings.Contains(page, "rst-shell-chrome") {
			t.Errorf("%s: the drawer is still in the layout", shell)
		}
		skip, back, rail := strings.Index(page, "rst-skip"), strings.Index(page, "rst-shell-back"), strings.Index(page, "<aside rst-shell-rail>")
		if !(skip < back && back < rail) {
			t.Errorf("%s: the back control is not after the skip link and before the rail (focus order must match what is seen)", shell)
		}
		if index := renderShellLayout(t, shell, `{{define "view"}}index{{end}}`); !strings.Contains(index, `<div rst-shell-`+shell+`="index">`) {
			t.Errorf("%s: the view block does not reach the root", shell)
		}
		if up := renderShellLayout(t, shell, `{{define "up"}}/#nav-invoices{{end}}`); !strings.Contains(up, `<a href="/#nav-invoices" rel="up"`) {
			t.Errorf("%s: the up block does not reach the back link", shell)
		}
	}
	for _, shell := range []string{"column", "topbar", "stage"} {
		if src, _ := Layout(shell); strings.Contains(string(src), "shell.js") || strings.Contains(string(src), "shell.css") {
			t.Errorf("%s links shell.js or shell.css; only the sidebar and console need them", shell)
		}
	}
}

// Review Focus 2: a view block written the way a template author writes
// a multi-line file is still the index, because the value is a word
// list and [rst-shell-sidebar~="index"] matches a word.
func TestAViewBlockWithWhitespaceIsStillTheIndex(t *testing.T) {
	out := renderShellLayout(t, "sidebar", "{{define \"view\"}}\n  index\n{{end}}")
	m := regexp.MustCompile(`rst-shell-sidebar="([^"]*)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no view on the root: %s", out)
	}
	if words := strings.Fields(html.UnescapeString(m[1])); len(words) != 1 || words[0] != "index" {
		t.Errorf("the root's view is %q, want the one word index", m[1])
	}
}
```

(add `"html"` to `ui_test.go`'s imports.)

Run: `GOFLAGS=-mod=mod go test -run 'TestTheShellsKeepTheirOverridableBlockNames|TestTheBarShellsCollapse|TestIdiomClassesAreStyled|TestTheSidebarAndConsoleNameTheirViewAndTheirWayBack|TestAViewBlockWithWhitespace' -count=1 ./ui/`
Expected: FAIL on every one (old layouts, old sample).

- [ ] **Step 3: Rewrite the two layouts**

`ui/layouts/sidebar.html`:

```html
{{define "layout"}}<!doctype html>
<html lang="{{block "lang" .}}en{{end}}" dir="{{block "dir" .}}ltr{{end}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>{{block "title" .}}Hello{{end}}</title>
<link rel="stylesheet" href="{{asset "static/tokens.css"}}">
<link rel="stylesheet" href="{{asset "static/theme.css"}}">
{{/* shell.css is the slide between the index and a page on a phone;
     only the two shells that have an index link it, because the opt-in
     it holds cannot be scoped from tokens.css. */}}
<link rel="stylesheet" href="{{asset "static/shell.css"}}">
<script defer src="{{asset "static/rastrillo.js"}}"></script>
<script defer src="{{asset "static/busy.js"}}"></script>
{{/* blocking="render": shell.js types the slide in pagereveal, at the
     first render, and a plain deferred script can run after it. An
     engine that ignores the attribute gets an untyped cross-fade. */}}
<script defer blocking="render" src="{{asset "static/shell.js"}}"></script>
{{iconAssets}}
<script defer src="{{asset "static/select.js"}}"></script>
<script defer src="{{asset "static/calendar.js"}}"></script>
<script defer src="{{asset "static/datetime.js"}}"></script>
{{/* head is the app's own slot in <head>: a favicon, a meta tag, one
     more stylesheet. Empty by default, last in the head so an app's
     own CSS wins over the framework's, and the only block in the
     shells that is not chrome. */}}
{{block "head" .}}{{end}}
</head>
<body>
{{/* Below 800px this shell is two views of every URL, and the page says
     which. The index page defines the view block as index: the rail is
     then the whole page, a list of sections at a real URL, and there is
     no hamburger. Every other page is a content page (the default, so a
     page that forgets is content with a way back, never a hidden page)
     and names its way back in the up block, normally the index with the
     page's own nav link as the fragment, /#nav-invoices, so focus comes
     back to where the reader was even with scripts off.

     Two blocks and not one: a template cannot capture a block's output
     to branch on it, so "an empty up means index" would have to be read
     by CSS, through :has(), and without :has() the index would render as
     a content page with a back link to itself and no navigation.

     An app with its own template called view renames it: the name is
     generic, and this one is now the shell's. At 800px and up both views
     are the rail beside the page. */}}
<div rst-shell-sidebar="{{block "view" .}}page{{end}}">
<a rst-skip href="#main">{{T "rastrillo.ui.shell_skip"}}</a>
{{/* The back control: first after the skip link, so focus order is what
     is seen; a link, not a script; its visible word inside its
     accessible name (WCAG 2.5.3); rel="up" for tools that read it. An
     app that wants its name on it ("‹ Ledger") edits this line, which is
     the app's own from the moment it is scaffolded. */}}
<div rst-shell-back><a href="{{block "up" .}}/{{end}}" rel="up" aria-label="{{Tf "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a></div>
<aside rst-shell-rail>
{{/* The index's heading is the page's own title, reused rather than
     redefined (a second definition would replace it), so on the index,
     whose title is the app's name, it needs no new string. tokens.css
     shows it only on the phone index, so no page ever exposes two h1s. */}}
<h1 rst-shell-title>{{template "title" .}}</h1>
{{block "brand" .}}<a rst-shell-brand href="/">{{T "rastrillo.ui.shell_home"}}</a>{{end}}
<nav rst-shell-nav>{{block "nav" .}}{{end}}</nav>
{{/* The rail's foot: the language switcher, then the person, pinned to
     the bottom of the rail by the wrapper's auto margin. The wrapper is
     the layout's, not the app's, because both blocks inside it are
     app-supplied markup the shell cannot style by class — the box it
     puts them in is the only handle it has.

     account here is a bare slot: an override supplies the whole thing,
     chrome included. Topbar's account block is the other shape — the
     menu body inside a details/summary the layout owns — so moving a
     block between the two shells needs an edit.

     Written on one line on purpose: an un-overridden shell must leave
     this wrapper genuinely :empty, and a newline between the blocks
     would be a text node that stops it matching. */}}
<div rst-shell-rail-foot>{{block "locale" .}}{{end}}{{block "account" .}}{{end}}</div>
</aside>
<main rst-shell-main id="main">
<div rst-page>
{{template "content" .}}
</div>
</main>
</div>
</body>
</html>
{{end}}
```

(Comments must never contain the text `{{block "` or `{{template "`: the block-name gate reads them as blocks.)

`ui/layouts/console.html`: keep everything down to and including `</header>` except as follows, and replace from `</header>` to the end:

- the root `<div rst-shell-console>` becomes `<div rst-shell-console="{{block "view" .}}page{{end}}">`;
- the head gains the `shell.css` link and the `shell.js` script exactly as in the sidebar (with their comments), in the same places;
- in the big comment at the top of `<body>`, replace the paragraph "THE TWO CHROMES, AND WHY THERE IS STILL ONE CONTROL." and the "The reveals are written as HIDE-WHEN-CLOSED…" paragraph with:

```
     THE TWO CHROMES, ONE PATTERN EACH. Below 800px the bar's tail (the
     account and language menus) folds behind the one <details
     rst-shell-menu>, which opens as a card over the page, like the
     topbar's. The rail is an index page, like the sidebar's: the page
     says which view it is with the view block and names its way back
     with up, and the back strip sits under the bar. The rail used to be
     gated on the same [open] as the tail through :has(); tokens.css keeps
     that rule for old layouts, scoped to a root with no view.
```

- after `</header>`:

```html
<div rst-shell-back><a href="{{block "up" .}}/{{end}}" rel="up" aria-label="{{Tf "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a></div>
{{/* The rail carries navigation and nothing else — the language
     switcher and the person are up on the bar, which is what makes
     this shell a console rather than a sidebar with a strip above it.
     So it has no rail-foot, and the bug that cost this project a
     release — a person pinned to the bottom of a rail that hung 32px
     under the window — has nothing here to pin. What it has instead is
     a nav that sticks: see the max-block-size in tokens.css, which is
     measured against the viewport and gated against the viewport. Its
     <h1> is the page's title, shown only on the phone index, as in the
     sidebar. */}}
<aside rst-shell-rail>
<h1 rst-shell-title>{{template "title" .}}</h1>
<nav rst-shell-nav>{{block "nav" .}}{{end}}</nav>
</aside>
```

followed by the existing `<main …>`, `<footer …>` and closing lines unchanged. Also replace the `name="rst-shell-menu"` comment's sentence "and in this shell that closes the rail too, because the rail is gated on the very same [open]. One wrong attribute takes both chromes away at once." with "and the card would close under the reader's finger."

- [ ] **Step 4: The CSS**

In `ui/tokens.css`:

(a) Mark the drawer as legacy. Replace the sidebar block's opening comment (`/* sidebar shell — a rail beside the page. Below 800px the rail is hidden behind a <details> chrome strip …`) with:

```css
/* sidebar shell — a rail beside the page. Below 800px the rail is not
   shown on a content page and IS the page on the index (the index/back
   block after the console's). The main column carries min-inline-size:
   0 so a wide child (a list grid, a <pre>) scrolls itself instead of
   stretching the grid track.

   LEGACY, kept for layouts written before H: the <details
   rst-shell-chrome> drawer, which opened the rail into the page flow.
   Its rules are the ones naming rst-shell-chrome, below and in the wide
   block; none of the index/back rules match an old layout's root (it
   has no view), so an app that re-vendors tokens.css and keeps its old
   layout keeps its drawer. Delete them together once no layout and no
   gallery page writes rst-shell-chrome. */
```

and put `/* LEGACY: the pre-H drawer, see above. */` on the line before each of the three `rst-shell-chrome` rules and before `.rst-shell-sidebar > .rst-shell__chrome, [rst-shell-sidebar] > [rst-shell-chrome] { display: none; }` in the wide block.

(b) Scope the console's `:has()` gate to an old layout. Replace the narrow hide rule (line 1883) with:

```css
/* LEGACY: the rail gated on the Menu's [open], for console layouts
   written before H. A root with a view is new markup, where the rail
   follows the view and no :has() decides it; the :not() keeps this rule
   out of that fight entirely. */
.rst-shell-console:not(.rst-shell-console--page, .rst-shell-console--index):has(.rst-shell__menu:not([open])) > .rst-shell__rail, [rst-shell-console]:not([rst-shell-console~="page"], [rst-shell-console~="index"]):has([rst-shell-menu]:not([open])) > [rst-shell-rail] { display: none; }
```

and the wide undo (lines 1929-1930) with the same two selectors (its comment unchanged):

```css
  .rst-shell-console:not(.rst-shell-console--page, .rst-shell-console--index):has(.rst-shell__menu:not([open])) > .rst-shell__rail,
  [rst-shell-console]:not([rst-shell-console~="page"], [rst-shell-console~="index"]):has([rst-shell-menu]:not([open])) > [rst-shell-rail] { display: flex; }
```

Update the console block's opening comment ("TWO CHROMES, ONE CONTROL. …") to say the same as the layout's: one pattern per chrome; the tail folds behind the Menu as a card (the card block), the rail is an index (the index/back block), and the `:has()` pair below is legacy.

(c) Give the card to the console too. In Task 7's card block, add the console spelling to every rule: each `.rst-shell-topbar X, [rst-shell-topbar] Y` gains `, .rst-shell-console X, [rst-shell-console] Y` (the bar, the open summary, its `::before`, the open tail, the account in the tail, its summary, the panels in the tail; the nav rules stay topbar-only, the console's nav is in its rail). Do the same to the reduced-motion rule after it. Add to the block's comment: "The console's bar folds the same way; its nav is in the rail, so its card holds the account and language menus only."

(d) After the console's `@media (min-width: 800px) { … }` block (before `/* ── rst-signin`), add the index/back block:

```css
/* ── Sidebar and console on a phone: the index and the back control ──
   Below 800px these two shells are two views of every URL, and the page
   says which with the view block the layout writes into the root:
   [rst-shell-sidebar~="index"] is the rail as a full-screen page of
   sections at a real URL; [rst-shell-sidebar~="page"] is the content,
   with a back control at the top inline-start whose href is the page's
   up block. No hamburger. The console is the same for its rail, under
   its bar.

   An old layout's root has no view, so none of these rules match it and
   it keeps its drawer (the legacy rules above). Scoped below 800px, like
   the card: at 800 and up both views are the rail beside the page, the
   back strip and the title are hidden by the two rules just below, and
   nothing here needs undoing. ─────────────────────────────────────── */
.rst-shell__back, [rst-shell-back], .rst-shell__title, [rst-shell-title] { display: none; }
@media (max-width: 799.98px) {
  /* The content view: a sticky strip of its own at the top, first in
     the reading order after the skip link. */
  .rst-shell-sidebar--page > .rst-shell__back, [rst-shell-sidebar~="page"] > [rst-shell-back], .rst-shell-console--page > .rst-shell__back, [rst-shell-console~="page"] > [rst-shell-back] { align-items: center; background: var(--rst-surface); border-block-end: 1px solid var(--rst-line); display: flex; inset-block-start: 0; min-block-size: 2.75rem; padding-inline: var(--rst-sp-1); position: sticky; z-index: 20; }
  .rst-shell__back a, [rst-shell-back] a { align-items: center; border-radius: var(--rst-radius-sm); color: var(--rst-accent); display: inline-flex; font-size: 1rem; font-weight: 500; gap: 0.5rem; min-block-size: 2.75rem; padding-inline: 0.75rem; text-decoration: none; }
  /* The chevron is drawn, not an icon: there is no left chevron in the
     set and no icon mirroring, and a drawn one stays out of the
     accessible name. Two borders of a small square, turned; physical on
     purpose, like the switch's knob, because it is a drawing, and
     mirrored by the rule after it. */
  .rst-shell__back a::before, [rst-shell-back] a::before { block-size: 0.55rem; border-left: 2px solid currentColor; border-top: 2px solid currentColor; content: ""; flex: none; inline-size: 0.55rem; transform: rotate(-45deg); }
  [dir="rtl"] .rst-shell__back a::before, [dir="rtl"] [rst-shell-back] a::before { transform: rotate(135deg); }
  .rst-shell__back a:focus-visible, [rst-shell-back] a:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: -2px; }
  .rst-shell-console--page > .rst-shell__rail, [rst-shell-console~="page"] > [rst-shell-rail] { display: none; }

  /* The index view: the rail is the page. main and the skip link go with
     it, so the skip link never points at nothing; the sidebar's brand,
     which would link to this very page, gives way to the title, the one
     h1 here. On the index a page has no main landmark, which is the
     point: the page is navigation. */
  .rst-shell-sidebar--index > .rst-shell__main, [rst-shell-sidebar~="index"] > [rst-shell-main], .rst-shell-sidebar--index > .rst-skip, [rst-shell-sidebar~="index"] > [rst-skip], .rst-shell-console--index > .rst-shell__main, [rst-shell-console~="index"] > [rst-shell-main], .rst-shell-console--index > .rst-skip, [rst-shell-console~="index"] > [rst-skip], .rst-shell-sidebar--index .rst-shell__brand, [rst-shell-sidebar~="index"] [rst-shell-brand] { display: none; }
  .rst-shell-sidebar--index > .rst-shell__rail, [rst-shell-sidebar~="index"] > [rst-shell-rail] { background: var(--rst-bg); border-block-end: 0; display: flex; gap: 0; min-block-size: 100dvh; padding: var(--rst-sp-5) var(--rst-sp-4); }
  .rst-shell-console--index > .rst-shell__rail, [rst-shell-console~="index"] > [rst-shell-rail] { background: var(--rst-bg); border-block-end: 0; display: flex; gap: 0; padding: var(--rst-sp-5) var(--rst-sp-4); }
  .rst-shell-sidebar--index .rst-shell__title, [rst-shell-sidebar~="index"] [rst-shell-title], .rst-shell-console--index .rst-shell__title, [rst-shell-console~="index"] [rst-shell-title] { display: block; font-size: 1.75rem; font-weight: 700; letter-spacing: -0.02em; line-height: 1.2; margin: 0 0 var(--rst-sp-2); padding-inline: 0.25rem; }
  /* The rows: every link a full-width row, at least 3rem, 1rem text, a
     chevron at the end. The grouping is the markup apps already write,
     a group label and then its links: a run of rows is rounded at its
     first link (the nav's first, or the one after a label) and its last
     (the nav's last, or the one before a label). The :has() half is a
     rule of its own, so an engine without :has() drops only it and a
     run's last corner is square, which is cosmetic. aria-current is not
     marked here: the index is its own page, where "current" is not
     true. */
  .rst-shell-sidebar--index .rst-shell__nav, [rst-shell-sidebar~="index"] [rst-shell-nav], .rst-shell-console--index .rst-shell__nav, [rst-shell-console~="index"] [rst-shell-nav] { gap: 0; }
  .rst-shell-sidebar--index .rst-shell__nav > a, [rst-shell-sidebar~="index"] [rst-shell-nav] > a, .rst-shell-sidebar--index .rst-shell__nav > a[aria-current], [rst-shell-sidebar~="index"] [rst-shell-nav] > a[aria-current], .rst-shell-console--index .rst-shell__nav > a, [rst-shell-console~="index"] [rst-shell-nav] > a, .rst-shell-console--index .rst-shell__nav > a[aria-current], [rst-shell-console~="index"] [rst-shell-nav] > a[aria-current] { align-items: center; background: var(--rst-surface); border: 1px solid var(--rst-line); border-block-start-width: 0; border-radius: 0; box-sizing: border-box; color: var(--rst-text); display: flex; font-size: 1rem; font-weight: 450; gap: var(--rst-sp-3); min-block-size: 3rem; padding-block: 0; padding-inline: 1rem 0.75rem; }
  .rst-shell-sidebar--index .rst-shell__nav > a:first-child, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:first-child, .rst-shell-sidebar--index .rst-shell__nav > .rst-shell__group + a, [rst-shell-sidebar~="index"] [rst-shell-nav] > [rst-shell-group] + a, .rst-shell-console--index .rst-shell__nav > a:first-child, [rst-shell-console~="index"] [rst-shell-nav] > a:first-child, .rst-shell-console--index .rst-shell__nav > .rst-shell__group + a, [rst-shell-console~="index"] [rst-shell-nav] > [rst-shell-group] + a { border-block-start-width: 1px; border-start-end-radius: var(--rst-radius); border-start-start-radius: var(--rst-radius); }
  .rst-shell-sidebar--index .rst-shell__nav > a:last-child, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:last-child, .rst-shell-console--index .rst-shell__nav > a:last-child, [rst-shell-console~="index"] [rst-shell-nav] > a:last-child { border-end-end-radius: var(--rst-radius); border-end-start-radius: var(--rst-radius); }
  .rst-shell-sidebar--index .rst-shell__nav > a:has(+ .rst-shell__group), [rst-shell-sidebar~="index"] [rst-shell-nav] > a:has(+ [rst-shell-group]), .rst-shell-console--index .rst-shell__nav > a:has(+ .rst-shell__group), [rst-shell-console~="index"] [rst-shell-nav] > a:has(+ [rst-shell-group]) { border-end-end-radius: var(--rst-radius); border-end-start-radius: var(--rst-radius); }
  .rst-shell-sidebar--index .rst-shell__nav > a::after, [rst-shell-sidebar~="index"] [rst-shell-nav] > a::after, .rst-shell-console--index .rst-shell__nav > a::after, [rst-shell-console~="index"] [rst-shell-nav] > a::after { block-size: 0.5rem; border-right: 2px solid var(--rst-text-faint); border-top: 2px solid var(--rst-text-faint); content: ""; flex: none; inline-size: 0.5rem; margin-inline-start: auto; transform: rotate(45deg); }
  [dir="rtl"] .rst-shell-sidebar--index .rst-shell__nav > a::after, [dir="rtl"] [rst-shell-sidebar~="index"] [rst-shell-nav] > a::after, [dir="rtl"] .rst-shell-console--index .rst-shell__nav > a::after, [dir="rtl"] [rst-shell-console~="index"] [rst-shell-nav] > a::after { transform: rotate(-135deg); }
  .rst-shell-sidebar--index .rst-shell__nav > a:hover, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:hover, .rst-shell-sidebar--index .rst-shell__nav > a:active, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:active, .rst-shell-console--index .rst-shell__nav > a:hover, [rst-shell-console~="index"] [rst-shell-nav] > a:hover, .rst-shell-console--index .rst-shell__nav > a:active, [rst-shell-console~="index"] [rst-shell-nav] > a:active { background: var(--rst-accent-soft); }
  .rst-shell-sidebar--index .rst-shell__nav > a:focus-visible, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:focus-visible, .rst-shell-console--index .rst-shell__nav > a:focus-visible, [rst-shell-console~="index"] [rst-shell-nav] > a:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: -2px; }
  /* "You came from here": the back link points at #nav-<section>, and
     that row flashes once as the index comes back. */
  .rst-shell-sidebar--index .rst-shell__nav > a:target, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:target, .rst-shell-console--index .rst-shell__nav > a:target, [rst-shell-console~="index"] [rst-shell-nav] > a:target { animation: rst-shell-flash 1.4s ease-out; }
  .rst-shell-sidebar--index .rst-shell__group, [rst-shell-sidebar~="index"] [rst-shell-group], .rst-shell-console--index .rst-shell__group, [rst-shell-console~="index"] [rst-shell-group] { margin-block: var(--rst-sp-5) var(--rst-sp-2); padding-inline: 1rem; }
  /* The foot FOLLOWS the nav on the index, by a fixed gap, never auto:
     the rail has a min-height here, and an auto margin would float the
     foot to the bottom of the screen, where the language menu would open
     off it (the rail-foot comment above has the long version). */
  .rst-shell-sidebar--index .rst-shell__rail-foot, [rst-shell-sidebar~="index"] [rst-shell-rail-foot] { margin-block-start: var(--rst-sp-6); }
}
@keyframes rst-shell-flash { from { background: var(--rst-accent-soft); } }
@media (prefers-reduced-motion: reduce) {
  .rst-shell-sidebar--index .rst-shell__nav > a:target, [rst-shell-sidebar~="index"] [rst-shell-nav] > a:target, .rst-shell-console--index .rst-shell__nav > a:target, [rst-shell-console~="index"] [rst-shell-nav] > a:target { animation: none; }
}
```

(e) At the end of the touch block add:

```css
  /* The back control (its strip is 2.75rem already; the link is the
     target) and the index rows (3rem, already over the floor). */
  .rst-shell__back a, [rst-shell-back] a { min-inline-size: var(--rst-tap); }
```

and in `ui/sizing_test.go`'s `tapInventory`:

```go
	{"[rst-shell-back] a", "Shell: the back control", "rst-shell-back"},
	{"[rst-shell-nav] > a", "Shell: the index rows (3rem)", "rst-shell-nav"},
```

Copy tokens.css into both examples.

- [ ] **Step 5: The sample, the doc and the v3 fixture**

In `ui/styleguide.go`, replace the `shell-sidebar` sample and its comment with (the two English strings are the approved batch-1 `shell_up_label` and `shell_up`; drafts shown):

```go
	// shell-sidebar — the same frame with a rail instead of a bar, as a
	// content page: below 800px the rail is not shown and the back link
	// at the top returns to the index, the page whose view block says
	// index, where the rail is the whole page. The back link's href is
	// the page's up block, the index with this page's nav link as the
	// fragment. No JavaScript: every view is a server-rendered page.
	"shell-sidebar": `<div rst-shell-sidebar="page">
  <a rst-skip href="#main">Skip to content</a>
  <div rst-shell-back><a href="/#nav-reports" rel="up" aria-label="Back to Sections">Sections</a></div>
  <aside rst-shell-rail><h1 rst-shell-title>Notes</h1><a rst-shell-brand href="/">Notes</a>
    <nav rst-shell-nav><span rst-shell-group>Work</span><a id="nav-dashboard" href="/dashboard">Dashboard</a><a id="nav-reports" href="/reports" aria-current="page">Reports</a></nav>
  </aside>
  <main rst-shell-main id="main"><div rst-page>Content.</div></main>
</div>`,
```

In `ui/ui.go`'s package doc, replace the `shell —` paragraph's sidebar and console sentences with:

```go
// rst-shell-sidebar wraps a rst-shell-rail of rst-shell-group-labelled
// nav beside rst-shell-main. Below 800px it is two views of every URL,
// named by the page's view block: the index (rst-shell-sidebar="index"),
// where the rail is the whole page under its rst-shell-title, and a
// content page (rst-shell-sidebar="page", the default), which carries a
// rst-shell-back link to the page's up block. No JavaScript; shell.js
// adds a slide, history reuse and focus return. rst-shell-console is
// both at once: its bar's tail folds behind the one <details
// rst-shell-menu> as a card over the page, and its rail is an index like
// the sidebar's.
```

(and drop the `<details rst-shell-chrome>` and `:has()` wording from that paragraph).

In `ui/markup_v3_browser_test.go`'s `extraFixture`, after the existing shell lines add:

```html
  <div class="rst-shell-sidebar rst-shell-sidebar--index"><div class="rst-shell__back"><a href="#a">Sections</a></div><div class="rst-shell__rail"><h1 class="rst-shell__title">App</h1><a class="rst-shell__brand" href="#a">App</a><nav class="rst-shell__nav"><p class="rst-shell__group">Work</p><a href="#a">One</a><a href="#a">Two</a><p class="rst-shell__group">More</p><a href="#a">Three</a></nav><div class="rst-shell__rail-foot"></div></div><main class="rst-shell__main">Main</main></div>
  <div class="rst-shell-sidebar rst-shell-sidebar--page"><div class="rst-shell__back"><a href="#a">Sections</a></div><div class="rst-shell__rail"><h1 class="rst-shell__title">App</h1><nav class="rst-shell__nav"><a href="#a">One</a></nav></div><main class="rst-shell__main">Main</main></div>
  <div class="rst-shell-console rst-shell-console--index"><div class="rst-shell__bar"><a class="rst-shell__brand" href="#a">App</a><details class="rst-shell__menu" name="rst-fixture-menus9" open><summary>Menu</summary></details><div class="rst-shell__tail"><details class="rst-shell__account rst-dropdown" name="rst-fixture-menus10"><summary class="rst-btn">Ana</summary><div class="rst-dropdown__menu"><a href="#a">Sign out</a></div></details></div></div><div class="rst-shell__back"><a href="#a">Sections</a></div><div class="rst-shell__rail"><h1 class="rst-shell__title">App</h1><nav class="rst-shell__nav"><a href="#a">One</a><a href="#a">Two</a></nav></div><main class="rst-shell__main">Main</main></div>
  <div class="rst-shell-console rst-shell-console--page"><div class="rst-shell__bar"><a class="rst-shell__brand" href="#a">App</a></div><div class="rst-shell__back"><a href="#a">Sections</a></div><div class="rst-shell__rail"><nav class="rst-shell__nav"><a href="#a">One</a></nav></div><main class="rst-shell__main">Main</main></div>
```

The drive that reads this fixture runs at the browser's default width, where no rule scoped below 800px applies, so the new narrow rules would be paired on paper only. In `TestBothSpellingsComputeTheSameStyles`, give `read` a `width int64` parameter and put `chromedp.EmulateViewport(width, 900)` before its `Navigate`; wrap everything from `byClass, byAttr, bare := read("class"), read("attr"), read("bare")` to the end of the function in `for _, width := range []int64{1280, 390} { … }`, passing `width` to the three reads, turning the `return` after `rig.Screen(…)` into `continue`, and adding the width to every failure message (`"at %dpx: …"`). Both passes must agree. (The fixture's shell blocks are also what `assertFixtureCoversTokensCSS` needs for the new classes.)

Run: `GOFLAGS=-mod=mod go test -count=1 ./ui/`
Expected: PASS, including Step 2's tests, `TestLayoutsParseAndRender`, `TestLayoutClassesAreStyled`, the twin gates, the motion gate (the flash's `none`), `TestEveryInteractiveRuleIsInTheTapInventory`.

- [ ] **Step 6: The ui browser drives**

Create `ui/index_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"html/template"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// legacyLayout is a layout as it was before H, kept as a fixture.
func legacyLayout(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/legacy/" + name + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// shellLayoutPage renders a layout's source with a nav of three
// sections (ids, so the scriptless focus return has fragments to use),
// a title, a language menu, and the given extra defines.
func shellLayoutPage(t *testing.T, src []byte, dir string, defs ...string) string {
	t.Helper()
	tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
		"asset":      func(p string) string { return "/" + strings.TrimPrefix(p, "static/") },
		"iconAssets": func() template.HTML { return "" },
	}).Parse(string(src)))
	base := []string{
		`{{define "dir"}}` + dir + `{{end}}`,
		`{{define "title"}}Harbour{{end}}`,
		`{{define "nav"}}<a id="nav-invoices" href="/invoices">Invoices</a><a id="nav-orders" href="/orders">Orders</a><a id="nav-team" href="/team">Team</a>{{end}}`,
		`{{define "locale"}}<details rst-dropdown rst-locale id="rail-locale" name="rst-menus"><summary>Language</summary><div rst-dropdown-menu><a href="/go/en" lang="en">English</a><a href="/go/ga" lang="ga">Gaeilge</a></div></details>{{end}}`,
		`{{define "content"}}<h1>Invoices</h1><p>Content.</p>{{end}}`,
	}
	for _, d := range append(base, defs...) {
		template.Must(tmpl.Parse(d))
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// shellAssets adds shell.js and shell.css to the sizing mux. A
// *http.ServeMux, so Task 11 can hand it to rastrillo.Handler.
func shellAssets(t *testing.T, pages map[string]string) *http.ServeMux {
	mux := sizingMux(t, pages)
	mux.HandleFunc("GET /shell.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(ShellJS())
	})
	mux.HandleFunc("GET /shell.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(ShellCSS())
	})
	return mux
}

// viewJS is what a reader of a shell page sees.
const viewJS = `(() => {
  const shown = el => !!el && el.getClientRects().length > 0 && el.getBoundingClientRect().width > 0;
  const q = s => document.querySelector(s), back = q("[rst-shell-back] a"), br = back ? back.getBoundingClientRect() : null;
  const title = q("[rst-shell-title]");
  return JSON.stringify({Rail: shown(q("[rst-shell-rail] [rst-shell-nav]")), Main: shown(q("[rst-shell-main]")),
    SkipDisplay: getComputedStyle(q("[rst-skip]")).display, Title: shown(title) ? title.textContent.trim() : "",
    Back: shown(back), BackStart: br ? Math.round(document.documentElement.dir === "rtl" ? document.documentElement.clientWidth - br.right : br.left) : -1,
    BackTop: br ? Math.round(br.top) : -1, Drawer: !!q("[rst-shell-chrome]"),
    H1s: [...document.querySelectorAll("h1")].filter(shown).length, Len: history.length});
})()`

type viewReading struct {
	Rail, Main, Back, Drawer bool
	SkipDisplay, Title       string
	BackStart, BackTop, H1s  int
	Len                      int
}

// TestThePhoneIndexWorksWithNoScript is §10.3's scriptless leg, both
// shells, LTR and RTL, at 390 with a coarse pointer and scripts
// disabled in the engine: the index is the rail with the title as its
// one visible h1 and no main, no skip link and no drawer; a nav link
// opens the content page with the back control at the top inline-start
// and no rail; the back control lands on /#nav-… where that link is the
// target and the next Tab goes to the link after it. History grows one
// entry per step, which is the scriptless trade (logged).
func TestThePhoneIndexWorksWithNoScript(t *testing.T) {
	for _, shell := range []string{"sidebar", "console"} {
		for _, dir := range []string{"ltr", "rtl"} {
			t.Run(shell+" "+dir, func(t *testing.T) {
				src, _ := Layout(shell)
				pages := map[string]string{
					"/":         shellLayoutPage(t, src, dir, `{{define "view"}}index{{end}}`),
					"/invoices": shellLayoutPage(t, src, dir, `{{define "up"}}/#nav-invoices{{end}}`),
				}
				rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
				ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
				defer cancel()
				if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(390, 844),
					chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body")); err != nil {
					t.Fatal(err)
				}
				requirePointer(t, ctx, true)
				var idx, pg, back viewReading
				at(t, ctx, viewJS, &idx)
				if !idx.Rail || idx.Main || idx.SkipDisplay != "none" || idx.Title != "Harbour" || idx.Back || idx.Drawer || idx.H1s != 1 {
					t.Errorf("the index: %+v; want the rail, the title as the one h1, no main, no skip link, no back control, no drawer", idx)
				}
				chromedp.Run(ctx, chromedp.Click("#nav-invoices", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-main]", chromedp.ByQuery))
				at(t, ctx, viewJS, &pg)
				if pg.Rail || !pg.Main || !pg.Back || pg.BackStart > 8 || pg.H1s != 1 {
					t.Errorf("the content page: %+v; want main, the back control within 8px of the top inline-start, no rail, one h1", pg)
				}
				if shell == "sidebar" && pg.BackTop > 1 {
					t.Errorf("the sidebar's back control is %dpx from the top", pg.BackTop)
				}
				chromedp.Run(ctx, chromedp.Click("[rst-shell-back] a", chromedp.ByQuery), chromedp.WaitVisible("[rst-shell-rail] [rst-shell-nav]", chromedp.ByQuery))
				at(t, ctx, viewJS, &back)
				var target bool
				var next string
				chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("nav-invoices").matches(":target")`, &target),
					chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`document.activeElement.id`, &next))
				if !target || next != "nav-orders" {
					t.Errorf("back on the index: #nav-invoices is the target %v and the next Tab went to %q; want true and nav-orders", target, next)
				}
				t.Logf("%s %s: history %d -> %d -> %d (scripts off: one entry per step)", shell, dir, idx.Len, pg.Len, back.Len)
				if pg.Len != idx.Len+1 || back.Len != pg.Len+1 {
					t.Errorf("history grew %d then %d, want 1 and 1", pg.Len-idx.Len, back.Len-pg.Len)
				}
			})
		}
	}
}

// boxesJS is the rail's and main's boxes, for comparing two layouts.
const boxesJS = `(() => { const b = s => { const r = document.querySelector(s).getBoundingClientRect(); return [r.left, r.top, r.width, r.height].map(Math.round).join(","); };
  return JSON.stringify({Rail: b("[rst-shell-rail]"), Main: b("[rst-shell-main]")}); })()`

// TestTheWideLayoutIsUnchanged: at 1280 both views are exactly the
// pre-H layout (the same markup rendered through the legacy layout is
// the golden), with no back control and no second h1.
func TestTheWideLayoutIsUnchanged(t *testing.T) {
	for _, shell := range []string{"sidebar", "console"} {
		src, _ := Layout(shell)
		pages := map[string]string{
			"/index":  shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`),
			"/page":   shellLayoutPage(t, src, "ltr"),
			"/legacy": shellLayoutPage(t, legacyLayout(t, shell), "ltr"),
		}
		rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
		ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
		read := func(path string) (map[string]string, viewReading) {
			var boxes map[string]string
			var v viewReading
			chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("body"))
			at(t, ctx, boxesJS, &boxes)
			at(t, ctx, viewJS, &v)
			return boxes, v
		}
		golden, _ := read("/legacy")
		for _, path := range []string{"/index", "/page"} {
			got, v := read(path)
			if got["Rail"] != golden["Rail"] || got["Main"] != golden["Main"] {
				t.Errorf("%s %s at 1280: rail %s main %s; the pre-H layout gives rail %s main %s", shell, path, got["Rail"], got["Main"], golden["Rail"], golden["Main"])
			}
			if v.Back || v.Title != "" || v.H1s != 1 {
				t.Errorf("%s %s at 1280: back %v, title %q, %d h1s; want no back control and only the page's own h1", shell, path, v.Back, v.Title, v.H1s)
			}
		}
		cancel()
	}
}

// TestAnOldSidebarLayoutKeepsItsDrawer: an app that re-vendors
// tokens.css and keeps its pre-H layout still opens its drawer at 390.
func TestAnOldSidebarLayoutKeepsItsDrawer(t *testing.T) {
	pages := map[string]string{"/": shellLayoutPage(t, legacyLayout(t, "sidebar"), "ltr")}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var before, after viewReading
	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-chrome] > summary", chromedp.ByQuery))
	at(t, ctx, viewJS, &before)
	chromedp.Run(ctx, chromedp.Click("[rst-shell-chrome] > summary", chromedp.ByQuery))
	at(t, ctx, viewJS, &after)
	if before.Rail || !after.Rail || !after.Main {
		t.Errorf("the old layout's drawer: rail before %v, after opening %v, main %v; want closed, then open with the page still there", before.Rail, after.Rail, after.Main)
	}
}

// TestTheIndexRowsRoundEachRunOfLinks is Review Focus 1: whatever the
// nav starts with, every row has its side and bottom borders, the first
// row of each run its top border and top corners, and the last row of
// each run its bottom corners.
func TestTheIndexRowsRoundEachRunOfLinks(t *testing.T) {
	src, _ := Layout("sidebar")
	navs := map[string]string{
		"/group-first": `<p rst-shell-group>Sales</p><a href="/a">A</a><a href="/b">B</a><p rst-shell-group>Settings</p><a href="/c">C</a>`,
		"/one":         `<a href="/a">A</a>`,
		"/no-groups":   `<a href="/a">A</a><a href="/b">B</a><a href="/c" aria-current="page">C</a>`,
	}
	pages := map[string]string{}
	for path, nav := range navs {
		pages[path] = shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "nav"}}`+nav+`{{end}}`)
	}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	for path := range navs {
		var bad []string
		chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+path), chromedp.WaitReady("body"))
		at(t, ctx, `(() => {
		  const bad = [];
		  for (const a of document.querySelectorAll("[rst-shell-nav] > a")) {
		    const cs = getComputedStyle(a), prev = a.previousElementSibling, next = a.nextElementSibling;
		    const first = !prev || prev.hasAttribute("rst-shell-group"), last = !next || next.hasAttribute("rst-shell-group");
		    const px = v => parseFloat(v) || 0;
		    if (px(cs.borderLeftWidth) !== 1 || px(cs.borderRightWidth) !== 1 || px(cs.borderBottomWidth) !== 1) bad.push(a.textContent + ": sides/bottom");
		    if ((px(cs.borderTopWidth) === 1) !== first) bad.push(a.textContent + ": top border " + cs.borderTopWidth);
		    if ((px(cs.borderStartStartRadius) > 0) !== first) bad.push(a.textContent + ": top corner " + cs.borderStartStartRadius);
		    if ((px(cs.borderEndStartRadius) > 0) !== last) bad.push(a.textContent + ": bottom corner " + cs.borderEndStartRadius);
		    if (a.getBoundingClientRect().height < 47.5) bad.push(a.textContent + ": under 3rem");
		  }
		  return JSON.stringify(bad);
		})()`, &bad)
		if len(bad) > 0 {
			t.Errorf("%s: %v", path, bad)
		}
	}
}

// Review Focus 2, in the engine: a view block with whitespace matches
// the index rules.
func TestAWhitespaceViewIsTheIndexInTheBrowser(t *testing.T) {
	src, _ := Layout("sidebar")
	pages := map[string]string{"/": shellLayoutPage(t, src, "ltr", "{{define \"view\"}}\n  index\n{{end}}")}
	rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var v viewReading
	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("body"))
	at(t, ctx, viewJS, &v)
	if !v.Rail || v.Main {
		t.Errorf("a view written as \"\\n  index\\n\" renders rail %v main %v; want the index", v.Rail, v.Main)
	}
}
```

In `ui/shell_browser_test.go`'s `TestTheSidebarRailPutsThePersonAtItsFootAndTheLanguageMenuOpensUpward`: clone a third tree before any Execute (`index := template.Must(tmpl.Clone())`, after `tall :=`), parse `{{define "view"}}index{{end}}` into it, execute it into `indexHTML`, serve it at `GET /index`, and add to `railMeasure`'s object `FootAfterNav: Math.round(document.querySelector("[rst-shell-rail-foot]").getBoundingClientRect().top - document.querySelector("[rst-shell-nav]").getBoundingClientRect().bottom), MenuBottom: Math.round(m.bottom)` (and the two fields to `railReading`). Replace the 390 leg (from `var narrow string` through the `small.MenuLift >= 0` check) with:

```go
	// And the phone index, which replaced the drawer in H. The rail IS the
	// page there, at least the height of the window, and its foot FOLLOWS
	// the nav by a fixed gap rather than floating to the bottom: an auto
	// margin in a rail with a min-height would put the language menu at
	// the foot of the screen, opening off it. So the foot's distance from
	// the nav is the measurement, and the language menu is asserted to be
	// on screen whichever way it opened.
	var narrow string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 780),
		chromedp.Navigate(rig.Origin+"/index"),
		chromedp.WaitVisible(`#rail-person`, chromedp.ByQuery),
		chromedp.Click(`#rail-locale > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`#rail-locale [rst-dropdown-menu]`, chromedp.ByQuery),
		chromedp.Evaluate(railMeasure, &narrow),
	); err != nil {
		t.Fatalf("driving the phone index: %v", err)
	}
	var small railReading
	if err := json.Unmarshal([]byte(narrow), &small); err != nil {
		t.Fatalf("reading the index measurement (%q): %v", narrow, err)
	}
	t.Logf("index: rail %dpx in a %dpx viewport, foot %dpx after the nav", small.RailHeight, small.Viewport, small.FootAfterNav)
	if small.RailHeight < small.Viewport-1 {
		t.Errorf("the phone index's rail is %dpx in a %dpx window; the rail is the page there", small.RailHeight, small.Viewport)
	}
	if small.FootAfterNav < 30 || small.FootAfterNav > 50 {
		t.Errorf("the foot is %dpx after the nav on the index; it should follow it by var(--rst-sp-6), not float to the bottom", small.FootAfterNav)
	}
	if small.MenuBottom > small.Viewport {
		t.Errorf("the language menu on the index ends %dpx below the window", small.MenuBottom-small.Viewport)
	}
```

and in its doc comment, replace the paragraph about the collapse with a sentence saying the 390 leg measures the phone index (§4.3).

In `ui/console_shell_browser_test.go`:

- `consolePage` takes extra defines: `func consolePage(t *testing.T, nav, dir string, defs ...string) (page, control string)`, parsing each after the existing ones; add `legacyConsolePage(t, nav, dir)` rendering `legacyLayout(t, "console")` the same way (factor the body into `consolePageFrom(t, src, nav, dir, defs...)`).
- `TestTheConsoleFoldsBothChromesBehindOneControl` becomes `TestTheConsoleFoldsItsBarAndIndexesItsRail`: legs 1, 1b, 2, 2b, 5 and 6 unchanged in substance (leg 2's "one visible summary" still holds: the back control is a link); leg 3 becomes "one click reveals the tail card and not the rail" (wait for `[rst-shell-tail] [rst-shell-account] > summary`, assert `open.TailShown && !open.RailShown`, drop `TailAboveRail`); leg 4 keeps `MenuOpen`, `TailShown`, `AccountMenu` and asserts `!trap.RailShown`; a new leg 3b renders `consolePage(t, nav, "ltr", `{{define "view"}}index{{end}}`)` at 390 and asserts the rail is shown with the disclosure closed and exactly one visible summary. Rewrite the doc comment's claims 2-4 to match: one control for the bar, the rail follows the page's view.
- `TestTheConsoleDegradesTheWayItSaysItDoesWithoutHas`: build its page with `legacyConsolePage` (the `:has()` gate now lives only for old markup, and the legs are about that gate); add at the end a new-markup leg: serve `consolePage(…)` (page view) and `consolePage(…, `{{define "view"}}index{{end}}`)` against the stripped stylesheet at 390, and assert the rail is hidden on the first and shown on the second: the new markup needs no `:has()` at all.

In `ui/card_browser_test.go`, add:

```go
// TestTheConsoleBarIsTheSameCard: the console's tail folds into the same
// card as the topbar's (§4.8): the page does not move, the card sits at
// the inline end, an outside tap closes it with and without scripts,
// and one Escape from inside its account menu closes both.
func TestTheConsoleBarIsTheSameCard(t *testing.T) {
	src, _ := Layout("console")
	for _, scripts := range []bool{false, true} {
		page := shellLayoutPage(t, src, "ltr", `{{define "account"}}<a id="acct-profile" href="/go/profile">Profile</a>{{end}}`,
			`{{define "content"}}<h1>Invoices</h1><div style="block-size: 450px"></div><p><a id="main-link" href="/go/main">A link in the page</a></p>{{end}}`)
		pages := map[string]string{"/": page}
		rig := harness.New(t, func(string) http.Handler { return shellAssets(t, pages) }, harness.WithCoarsePointer())
		ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
		acts := []chromedp.Action{chromedp.EmulateViewport(390, 844)}
		if !scripts {
			acts = append(acts, emulation.SetScriptExecutionDisabled(true))
		}
		acts = append(acts, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery))
		chromedp.Run(ctx, acts...)
		var before, open struct{ Main string; Gap float64 }
		read := `(() => { const m = document.querySelector("main").getBoundingClientRect(), t = document.querySelector("[rst-shell-tail]").getBoundingClientRect();
		  return JSON.stringify({Main: [m.left, m.top, m.width].map(Math.round).join(","), Gap: document.documentElement.clientWidth - t.right}); })()`
		at(t, ctx, read, &before)
		chromedp.Run(ctx, chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery))
		at(t, ctx, read, &open)
		if open.Main != before.Main || open.Gap < 0 || open.Gap > 13 {
			t.Errorf("scripts %v: main %s -> %s, card %.1fpx from the edge; want a card over a page that did not move", scripts, before.Main, open.Main, open.Gap)
		}
		if got := clickAndLand(t, ctx, probe(t, ctx, "#main-link", 0.5, 0.5, 0, 0)); got != "/" {
			t.Errorf("scripts %v: a tap outside the card followed the link to %q", scripts, got)
		}
		if scripts {
			chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-shell-menu] > summary", chromedp.ByQuery),
				chromedp.Click("[rst-shell-menu] > summary", chromedp.ByQuery), chromedp.Click("[rst-shell-account] > summary", chromedp.ByQuery),
				chromedp.Focus("#acct-profile", chromedp.ByQuery), chromedp.KeyEvent(kb.Escape))
			var s struct{ Menu, Acct bool; Focus string }
			at(t, ctx, `JSON.stringify({Menu: document.querySelector("[rst-shell-menu]").open, Acct: document.querySelector("[rst-shell-account]").open, Focus: document.activeElement.closest("[rst-shell-menu]") ? "menu" : document.activeElement.tagName})`, &s)
			if s.Menu || s.Acct || s.Focus != "menu" {
				t.Errorf("Escape from the account menu in the console's card: %+v; want both closed and focus on the Menu summary", s)
			}
		}
		cancel()
	}
}
```

(`card_browser_test.go` needs the `kb` and `emulation` imports it already has.)

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -p 1 -count=1 ./ui/`
Expected: PASS (the whole ui browser package: every drive above, the rewritten ones, the card and row drives, `TestBothSpellingsComputeTheSameStyles` at both widths). Mutation checks, each restored: remove `[rst-shell-sidebar~="index"] > [rst-shell-main]` from the hide rule (the scriptless leg fails with `Main: true`); remove the `:not(…)` from the attribute half of the console gate and its class half (the fold drive's index leg fails with the rail hidden until the Menu opens).

- [ ] **Step 7: The gallery's demo as four documents**

In `internal/designsystem/page.go`:

(a) Add after `demoHref`:

```go
// demoPageHref is one content page of the demo application. The demo is
// four documents since H, like the app it stands for: demo.html is the
// index (on a desktop it shows the dashboard beside the rail), and the
// dashboard, the request list and one request are pages whose back
// control returns to demo.html#nav-…. One document switching views with
// :target could not express two server-rendered views of one URL.
func demoPageHref(mount, theme, locale, view string) string {
	return mount + "/" + theme + "/" + locale + "/demo-" + view + ".html"
}
```

(b) `demoData` gains `View, Up, Home, Dashboard, Requests, Request string`; `renderDemo` returns `(map[string][]byte, error)`, parses once, and executes the layout five times off clones of the parsed tree (parse, then `tmpl.Clone()` before each Execute, since html/template refuses to clone after executing) for `View` in `index`, `dashboard`, `requests`, `request`, writing `demo.html` for `index` and `demo-<view>.html` for the others, with `Up` = `demoHref(…) + "#nav-dashboard"` for the dashboard and `"#nav-requests"` for the other two, `Home` = `demoHref(…)`, and the three hrefs from `demoPageHref`.

(c) Replace `demoCSS` with the one rule it still needs, and its comment with one that says why the switching rules went:

```go
// demoCSS is the whole of the demo's own stylesheet: one margin. The
// view switching it used to carry went in H, when the demo became four
// documents, and the rail's current item is aria-current written by the
// server now, as in a real app.
const demoCSS = `
[rst-stats] { margin-block-end: var(--rst-sp-5); }
`
```

(d) Replace `demoTemplate` with (the callout's two P keys are the approved batch-2 `gallery.demo.callout_title` and `gallery.demo.callout_body`; drafts shown; everything else is the template as it was, with its `#view-…` hrefs now real pages):

```go
const demoTemplate = `
{{define "head"}}<script src="{{.Mount}}/gallery.js"></script>
<style>` + demoCSS + `</style>{{end}}
{{define "lang"}}{{.Locale}}{{end}}
{{define "dir"}}{{.Dir}}{{end}}
{{define "title"}}{{.Title}}{{end}}
{{define "view"}}{{if eq .View "index"}}index{{else}}page{{end}}{{end}}
{{define "up"}}{{.Up}}{{end}}
{{define "brand"}}<a rst-shell-brand href="{{.Home}}">Harbour</a>{{end}}
{{define "nav"}}<a id="nav-dashboard" href="{{.Dashboard}}"{{if or (eq .View "index") (eq .View "dashboard")}} aria-current="page"{{end}}>{{P "Dashboard"}}</a><a id="nav-requests" href="{{.Requests}}"{{if or (eq .View "requests") (eq .View "request")}} aria-current="page"{{end}}>{{P "Requests"}}</a>{{end}}
{{define "locale"}}<details rst-dropdown rst-locale name="rst-menus"><summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span></summary><div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div></details>{{end}}
{{define "account"}}<div rst-shell-account><a rst-person href="{{.Dashboard}}"><span rst-person-av aria-hidden="true">A</span><span rst-person-meta><span rst-person-name>Ada Lovelace</span><span rst-person-email>ada@example.com</span></span></a></div>{{end}}
{{define "content"}}
{{if or (eq .View "index") (eq .View "dashboard")}}
<section class="app-view" id="view-dashboard">
{{template "page-header" dict "Title" (P "Dashboard") "Sub" (P "Everything the team has waiting this morning.")}}
<div rst-stats>
{{template "stat" dict "Label" (P "Open requests") "Value" "24" "Lead" true "Delta" "−6" "Tone" "positive" "Note" (P "since Monday")}}
{{template "stat" dict "Label" (P "Waiting on us") "Value" "6"}}
{{template "stat" dict "Label" (P "Resolved this week") "Value" "41" "Delta" "+12" "Tone" "positive" "Note" (P "since Monday")}}
</div>
<div rst-box-head><h2>{{P "Mailbox storage"}}</h2></div>
<section rst-box>{{template "meter" dict "Percent" 82 "Text" "412 / 500"}}</section>
<div rst-box-head><h2>{{P "Latest activity"}}</h2><a rst-btn href="{{.Requests}}">{{P "Requests"}}</a></div>
<div rst-card style="--rst-cols: minmax(0, 1fr) 120px">
<div rst-lrow="head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Invoice #4471 never arrived<small><bdi>Fiona Reid</bdi> · 09:12</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Card declined on renewal<small><bdi>Otto Neurath</bdi> · 08:40</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Seat count is wrong on the invoice<small><bdi>Hedy Lamarr</bdi> · 11 August</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span></div>
</div>
</section>
{{else if eq .View "requests"}}
<section class="app-view" id="view-requests">
{{template "page-header" dict "Title" (P "Requests") "Sub" (P "Every request in the queue, newest first.") "ActionHref" .Requests "ActionLabel" (P "New request") "ActionIcon" "plus"}}
{{template "seg-tabs" dict "Label" (P "Requests") "Items" (list (dict "Label" (P "All") "Href" .Requests "Current" true) (dict "Label" (P "Open") "Href" .Requests) (dict "Label" (P "Resolved") "Href" .Requests))}}
<div rst-card style="--rst-cols: minmax(0, 1fr) 120px 120px var(--rst-col-menu)">
{{template "list-bar" dict "SearchAction" .Requests "Placeholder" (P "Search requests")}}
<div rst-lrow="head"><span>{{P "Subject"}}</span><span class="rst-m-hide">{{P "Status"}}</span><span class="rst-m-hide">{{P "Updated"}}</span><span></span></div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Invoice #4471 never arrived<small><bdi>Fiona Reid</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}}</span><span class="rst-cell-mut rst-m-hide">09:12</span>{{template "row-menu" dict "Name" "Invoice #4471 never arrived" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Card declined on renewal<small><bdi>Otto Neurath</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">08:40</span>{{template "row-menu" dict "Name" "Card declined on renewal" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Export takes twenty minutes<small><bdi>Mary Sherman</bdi> · Data</small></a><span class="rst-m-hide">{{template "status-pill" dict "Label" (P "Open")}}</span><span class="rst-cell-mut rst-m-hide">12 August</span>{{template "row-menu" dict "Name" "Export takes twenty minutes" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
<div rst-lrow><a class="rst-nm" href="{{.Request}}">Seat count is wrong on the invoice<small><bdi>Hedy Lamarr</bdi> · Billing</small></a><span class="rst-m-hide">{{template "status-pill" dict "Tone" "positive" "Label" (P "Resolved")}}</span><span class="rst-cell-mut rst-m-hide">11 August</span>{{template "row-menu" dict "Name" "Seat count is wrong on the invoice" "Items" (list (dict "Label" (P "Reply") "Href" .Request) (dict "Label" (P "Close request…") "Href" .Request "Danger" true))}}</div>
</div>
<p rst-count-line>{{P "{shown} of {total} requests" "shown" "1–4" "total" "24"}}</p>
{{template "pagination" dict "Items" (list (dict "Label" "1" "Current" true) (dict "Label" "2" "Href" .Requests) (dict "Label" "3" "Href" .Requests))}}
</section>
{{else}}
<section class="app-view" id="view-request">
{{template "back-nav" dict "Href" .Requests "Label" (P "Requests")}}
{{template "page-header" dict "Title" "Invoice #4471 never arrived" "Sub" (P "Reported by {person}, and still waiting on us." "person" "Fiona Reid")}}
<p>{{template "status-pill" dict "Tone" "warning" "Label" (P "Waiting")}} {{template "badge" dict "Label" "Billing"}}</p>
<div rst-box-head><h2>{{P "Details"}}</h2></div>
<section rst-box>{{template "detail-list" dict "Items" (list (dict "Label" (P "Reference") "Value" "REQ-4471" "Mono" true) (dict "Label" (P "Reported by") "Value" "fiona@example.com") (dict "Label" (P "Queue") "Value" "Billing") (dict "Label" (P "Opened") "Value" "14 August, 09:12" "DateTime" "2026-08-14T09:12"))}}</section>
<div rst-box-head><h2>{{P "Reply"}}</h2></div>
<section rst-box><form rst-form method="post" action="{{.Request}}">
{{template "field-textarea" dict "Name" "reply" "Label" (P "Your reply") "Rows" 4 "Hint" (P "The person who reported this gets it by email.")}}
{{template "form-foot" dict "Submit" (P "Send reply") "CancelHref" .Requests "CancelLabel" (P "Cancel")}}
</form></section>
{{template "callout" dict "Tone" "info" "Title" (P "Every screen has its own address") "Body" (P "Each screen is a page of its own, like any rastrillo screen, and works the same with JavaScript off. On a phone, the sections are a list and each screen has a way back.")}}
</section>
{{end}}
{{end}}
`
```

Rewrite the demo's comment block above `demoShell` to say it is four documents, one per view, each a real page, and that `TestTheDemoApplicationWorksWithNoScript` drives it.

In `internal/designsystem/prose.go`, replace the entries for `Three screens, three addresses` and `Every view has its own address, like any rastrillo screen. Turn JavaScript off and it behaves the same. Switching uses CSS.` with entries for the two approved strings, each with eleven translations (the gate fails on the stale keys otherwise).

(e) In `designsystem.go`'s `Render`, write the demo's documents: `docs, err := renderDemo(mount, theme, locale)`; `for name, doc := range docs { out[dir+name] = doc }`. Update the doc comment's tree listing: `<theme>/<locale>/demo.html` and `demo-{dashboard,requests,request}.html`, "144 documents of the demo app, four per gallery".

(f) Shell previews. `shellData` gains `View, Up, PageHref string`; add

```go
// shellPageHref is the content page of a shell demo that has an index
// (sidebar and console): shells/<shell>.html is the index, the address
// the gallery has always linked, and this is the page its nav opens.
func shellPageHref(mount, theme, locale, shell string) string {
	return mount + "/" + theme + "/" + locale + "/shells/" + shell + "-page.html"
}
```

`renderShell` returns `(map[string][]byte, error)`: for `sidebar` and `console` it executes clones of the tree twice, `View: "index", Up: shellHref(…)` into `<shell>.html` and `View: "page", Up: shellHref(…) + "#nav-posts"` into `<shell>-page.html`, both with `PageHref: shellPageHref(…)`; the other shells execute once into `<shell>.html` with `PageHref: "#"`. `shellTemplate` gains

```
{{define "view"}}{{if eq .View "index"}}index{{else}}page{{end}}{{end}}
{{define "up"}}{{.Up}}{{end}}
```

and its nav becomes `<a id="nav-posts" href="{{.PageHref}}" aria-current="page">Posts</a><a id="nav-comments" href="{{.PageHref}}">Comments</a><a id="nav-settings" href="{{.PageHref}}">Settings</a>` (the shells without a view never execute the two new defines). `Render` writes every returned file under `dir + "shells/"`.

(g) Tests. In `designsystem_test.go`: `TestTreeShapeIsComplete` expects the three `demo-*.html` and `shells/sidebar-page.html`, `shells/console-page.html` in every directory; `TestNoTwoPageKindsShareAName` adds `"demo-dashboard.html", "demo-requests.html", "demo-request.html"` to its taken list.

In `a11y_test.go`: add to the targets list (after the demo app entries)

```go
		{"day/en demo requests page", demoPageHref(mountPath, "day", "en", "requests"), "the demo's list as its own page since H: a list grid with a row menu per row, under the back control"},
		{"day/en sidebar content page", shellPageHref(mountPath, "day", "en", "sidebar"), "the sidebar's content view: the back control and the page"},
```

and fix the two demo descriptions ("three screens in one document" becomes "the index of a four-page application: the rail and, on a desktop, the dashboard beside it").

Replace `TestA11yScansTheShellsCollapsed`'s table and loop body for `sidebar` and `console` (topbar unchanged) with a pass over every theme and scheme of both documents of each: load `shellHref` at 390; assert the index shows `[rst-shell-rail] [rst-shell-nav] a` and does not show `[rst-shell-main]` (patency: the view is the index before axe runs); scan; for the console also open `[rst-shell-menu] > summary` and assert `[rst-shell-tail] [rst-shell-account] > summary` is shown, then scan again; then load `shellPageHref`, assert `[rst-shell-back] a` is shown and the rail's nav is not, scan, and measure the back link: at least 44×44 (it is inside the width half of the touch query) with a non-empty visible label. The sidebar's old 24px summary floor goes with its summary. Loop the sidebar and console over `ui.ThemeNames()`; the topbar leg stays `day` only.

In `TestA11yReflowsAt320`, add `shellPageHref(… "sidebar")`, `shellHref(… "console")`, `shellPageHref(… "console")`, `demoHref(…)` and `demoPageHref(… "requests")` to the page list.

In `browser_test.go`, replace `TestTheDemoApplicationSwitchesViewsWithNoScript` with:

```go
// TestTheDemoApplicationWorksWithNoScript is the demo's claim since H:
// four pages, each a real document, and with script execution DISABLED
// in the engine every journey still works. At 1280 the rail and the
// pages: land, follow the rail to the list, a row into the request, the
// back link out. At 390 the phone index: the rail is the page, a row
// opens the list with a back control and no rail, and the back control
// returns to the index with the section it left as the :target.
func TestTheDemoApplicationWorksWithNoScript(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	const views = `[...document.querySelectorAll(".app-view")].filter(v => getComputedStyle(v).display !== "none").map(v => v.id).join(",")`
	shown := func(sel string) bool {
		var b bool
		chromedp.Run(ctx, chromedp.Evaluate(`(() => { const e = document.querySelector(`+"`"+sel+"`"+`); return !!e && e.getClientRects().length > 0; })()`, &b))
		return b
	}
	var ranScript, landed, list, detail, back string
	if err := chromedp.Run(ctx,
		emulation.SetScriptExecutionDisabled(true),
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+demoHref(mountPath, RootTheme(), "en")), chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.documentElement.getAttribute("data-rst-js") ?? "(none)"`, &ranScript),
		chromedp.Evaluate(views, &landed),
		chromedp.Click(`#nav-requests`, chromedp.ByQuery), chromedp.WaitReady("#view-requests", chromedp.ByQuery),
		chromedp.Evaluate(views, &list),
		chromedp.Click(`#view-requests [rst-lrow] a.rst-nm`, chromedp.ByQuery), chromedp.WaitReady("#view-request", chromedp.ByQuery),
		chromedp.Evaluate(views, &detail),
		chromedp.Click(`#view-request [rst-back-nav] a`, chromedp.ByQuery), chromedp.WaitReady("#view-requests", chromedp.ByQuery),
		chromedp.Evaluate(views, &back),
	); err != nil {
		t.Fatalf("driving the demo at 1280: %v", err)
	}
	if ranScript != "(none)" {
		t.Fatalf("gallery.js ran with script execution disabled (data-rst-js=%q); this drive proves nothing", ranScript)
	}
	for _, s := range []struct{ where, got, want string }{
		{"landing on demo.html", landed, "view-dashboard"}, {"the rail to the list", list, "view-requests"},
		{"a row into the request", detail, "view-request"}, {"the back link out", back, "view-requests"},
	} {
		if s.got != s.want {
			t.Errorf("%s: the page shows %q, want exactly %q", s.where, s.got, s.want)
		}
	}

	chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(rig.Origin+demoHref(mountPath, RootTheme(), "en")), chromedp.WaitReady("body"))
	if !shown("#nav-requests") || shown("[rst-shell-main]") {
		t.Errorf("demo.html at 390 is not the index (rail shown %v, main shown %v)", shown("#nav-requests"), shown("[rst-shell-main]"))
	}
	chromedp.Run(ctx, chromedp.Click("#nav-requests", chromedp.ByQuery), chromedp.WaitReady("#view-requests", chromedp.ByQuery))
	if !shown("[rst-shell-back] a") || shown("#nav-requests") {
		t.Errorf("the list at 390: back control shown %v, rail shown %v; want the back control and no rail", shown("[rst-shell-back] a"), shown("#nav-requests"))
	}
	var target bool
	chromedp.Run(ctx, chromedp.Click("[rst-shell-back] a", chromedp.ByQuery), chromedp.WaitReady("#nav-requests", chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("nav-requests").matches(":target")`, &target))
	if !target {
		t.Error("back on the index, the Requests row is not the :target; the scriptless focus return has nothing to start from")
	}
}
```

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/` then `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -count=1 ./internal/designsystem/`
Expected: PASS. If `TestPreviewFrameHeightsFitTheirContent` or `TestThePreviewWidgetIsUsableOnAPhone` reports a frame (the demo preview frames `demo.html`, now the index with the dashboard beside the rail; the shell previews now index documents), set the heights it measured in `heightOf`; the drive's own messages say what to change.

- [ ] **Step 8: Run the task gate** (all three commands).

- [ ] **Step 9: Commit and push**

```bash
git add ui/layouts/sidebar.html ui/layouts/console.html ui/testdata/legacy ui/tokens.css examples/blog/static/tokens.css examples/tickets/static/tokens.css ui/styleguide.go ui/ui.go ui/ui_test.go ui/sizing_test.go ui/markup_v3_browser_test.go ui/index_browser_test.go ui/shell_browser_test.go ui/console_shell_browser_test.go ui/card_browser_test.go internal/designsystem
git commit -m "Give the sidebar and console a phone index and a back control instead of a drawer

On a phone the sidebar hid its navigation behind a hamburger and the
console pushed its rail into the page. Both are now two views of every
URL: the index, where the rail is the page, and content pages with a
back control whose href the page names in its up block, so it works
with no script and focus returns through the fragment. The view block
defaults to page, so a forgotten one shows content with a way back. The
console's bar gets the topbar's card. Old layouts keep their drawer on
the new stylesheet, pinned by fixtures of the pre-H layouts. The
gallery's demo becomes four real pages and its shell previews two each,
because one document switching with :target cannot show two
server-rendered views of a URL.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 10: The scaffold's two pages for the sidebar and console, and doctor's layout advisory

Spec §4.7, §7 (doctor), §10.6 (the two-page case; the scaffolded app passes its own tests; the advisory with exit 0).

**Files:**
- Modify: `cmd/rastrillo/new.go` (`twoPageShells`, `scaffoldOverview`, `sectionsIndexTemplate`, `overviewTemplate`, `overviewHandler`, `overviewTest`; `appTemplate`, `handlersTemplate`, `renderTemplate`, `indexTestTemplate` gain one placeholder each; `runNew` fills them)
- Modify: `cmd/rastrillo/doctor.go` (`doctorLayoutAdvisory`, `oldShellLayout`, `report.layoutAdvice`, read in `diagnose`, printed in `print`)
- Modify: `cmd/rastrillo/new_test.go` (`TestRenderTemplateWiresErrorPage` passes the page list), `cmd/rastrillo/doctor_test.go`, `cmd/rastrillo/shellscaffold_test.go`

**Interfaces:**
- Consumes: `copy-review/batch1-result.json` (`scaffold.overview`, `doctor.layout_advisory`); `ui/testdata/legacy/*.html` (Task 9); Task 8's `scaffoldWithReplace`, `goTestIn`.
- Produces: `rastrillo new --shell=sidebar|console` writes `templates/index.html` (view index, one nav item), `templates/overview.html` (up `/#nav-overview`), the route `r.Get("/overview", a.overview)` and its handler, and a scaffolded `TestOverviewRenders`. `func oldShellLayout(src string) bool`; `const doctorLayoutAdvisory`.

- [ ] **Step 1: Read the two approved strings**

```bash
jq -r '.strings[] | select(.id=="scaffold.overview" or .id=="doctor.layout_advisory") | "\(.id)\t\(.text)"' copy-review/batch1-result.json
```

The code below shows the drafts (`Overview`; `This layout still has the old mobile menu. See "Upgrading" in the templates guide.`); write the approved text wherever they appear.

- [ ] **Step 2: Write the failing tests**

Append to `cmd/rastrillo/shellscaffold_test.go` (add `"amadan.net/rastrillo/rastrillo/ui"` to its imports):

```go
// §4.7: a sidebar or console app opens on a phone to its index, whose
// content is hidden there, so the scaffold writes the c3 shape: an index
// with one section in its nav and the section page it opens, with its
// way back. The other shells keep today's one page.
func TestNewWritesAnIndexAndASectionForTheShellsThatHaveOne(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, shell := range ui.LayoutNames() {
		name := "app" + shell
		if err := runNew([]string{"--shell=" + shell, name}); err != nil {
			t.Fatalf("%s: runNew: %v", shell, err)
		}
		dir := filepath.Join(name, "internal", name)
		index := readScaffold(t, dir, "templates", "index.html")
		app := readScaffold(t, dir, "app.go")
		handlers := readScaffold(t, dir, "handlers.go")
		render := readScaffold(t, dir, "render.go")
		overview, err := os.ReadFile(filepath.Join(dir, "templates", "overview.html"))
		if !twoPageShells[shell] {
			if err == nil || strings.Contains(app, "/overview") || index != indexTemplate {
				t.Errorf("%s: the one-page shells keep today's single page", shell)
			}
			continue
		}
		for _, c := range []struct{ file, got, want string }{
			{"index.html", index, `{{define "view"}}index{{end}}`},
			{"index.html", index, `<a id="nav-overview" href="/overview">` + scaffoldOverview + `</a>`},
			{"overview.html", string(overview), `{{define "up"}}/#nav-overview{{end}}`},
			{"overview.html", string(overview), `<a id="nav-overview" href="/overview" aria-current="page">` + scaffoldOverview + `</a>`},
			{"app.go", app, `r.Get("/overview", a.overview)`},
			{"handlers.go", handlers, `func (a *app) overview(w http.ResponseWriter, r *http.Request) {`},
			{"render.go", render, `[]string{"index", "overview", "errors"}`},
		} {
			if !strings.Contains(c.got, c.want) {
				t.Errorf("%s: %s lacks %q", shell, c.file, c.want)
			}
		}
		if err != nil {
			t.Errorf("%s: no overview.html: %v", shell, err)
		}
	}
}

// §10.6: the two-page scaffold builds, vets under the browser tag, and
// passes its own tests, the new overview test among them.
func TestAScaffoldedSidebarAppPassesItsOwnTests(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "phoneapp", "--shell=sidebar")
	out, err := goTestIn(dir, "-v", "./...")
	if err != nil {
		t.Fatalf("the scaffolded sidebar app's tests fail:\n%s", out)
	}
	if !strings.Contains(out, "--- PASS: TestOverviewRenders") {
		t.Errorf("the scaffolded suite did not run TestOverviewRenders:\n%s", out)
	}
	vet := exec.Command("go", "vet", "-tags", "browser", "./...")
	vet.Dir = dir
	if b, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go vet -tags browser:\n%s", b)
	}
}
```

In `cmd/rastrillo/new_test.go`'s `TestRenderTemplateWiresErrorPage`, change the Sprintf to `fmt.Sprintf(renderTemplate, "blogapp", "blogapp", `"index", "errors"`)`.

Append to `cmd/rastrillo/doctor_test.go`:

```go
// §7: doctor cannot diff a layout (it is the app's own), but it can
// recognise the pre-H shells: the drawer, or a sidebar or console root
// with no view block. It says so in one line naming the upgrade notes,
// and the exit code does not change.
func TestDoctorAdvisesAnOldShellLayout(t *testing.T) {
	for _, c := range []struct {
		name   string
		layout func(t *testing.T) []byte
		advise bool
	}{
		{"pre-H sidebar", func(t *testing.T) []byte { b, _ := os.ReadFile("../../ui/testdata/legacy/sidebar.html"); return b }, true},
		{"pre-H console", func(t *testing.T) []byte { b, _ := os.ReadFile("../../ui/testdata/legacy/console.html"); return b }, true},
		{"today's sidebar", func(t *testing.T) []byte { b, _ := ui.Layout("sidebar"); return b }, false},
		{"today's console", func(t *testing.T) []byte { b, _ := ui.Layout("console"); return b }, false},
		{"topbar", func(t *testing.T) []byte { b, _ := ui.Layout("topbar"); return b }, false},
	} {
		dir := doctorApp(t, rastrilloVersion(), "day")
		body := c.layout(t)
		if len(body) == 0 {
			t.Fatalf("%s: no layout to test with", c.name)
		}
		mustWrite(t, filepath.Join(dir, "internal", "demoapp", "templates", "layout.html"), string(body))
		rep, err := diagnose(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		out := printed(rep, false)
		line := filepath.Join("internal", "demoapp", "templates", "layout.html") + ": " + doctorLayoutAdvisory
		if got := strings.Contains(out, line); got != c.advise {
			t.Errorf("%s: advisory printed %v, want %v:\n%s", c.name, got, c.advise, out)
		}
		if code := exitCode(t, rep.exit()); code != 0 {
			t.Errorf("%s: exit %d; the advisory must not change the exit code", c.name, code)
		}
	}
	if strings.Contains(doctorLayoutAdvisory, "\u2014") {
		t.Error("the advisory carries an em dash")
	}
}
```

Run: `GOFLAGS=-mod=mod go test -run 'TestNewWritesAnIndexAndASection|TestRenderTemplateWiresErrorPage|TestDoctorAdvisesAnOldShellLayout' -count=1 ./cmd/rastrillo/`
Expected: FAIL to compile (`undefined: twoPageShells`, `scaffoldOverview`, `doctorLayoutAdvisory`).

- [ ] **Step 3: The scaffold**

In `cmd/rastrillo/new.go`, add near the page templates:

```go
// twoPageShells are the shells whose phone layout is an index page and a
// back control (spec §4.7). A scaffold of one of them with a single page
// would open on a phone to a title and an empty list, because that page
// is the index and its content is hidden there, so these get an index
// with one section and the page it opens.
var twoPageShells = map[string]bool{"sidebar": true, "console": true}

// scaffoldOverview is the one section's name (copy review, batch 1).
const scaffoldOverview = "Overview"

// sectionsIndexTemplate is templates/index.html for the two-page shells.
// %[1]s is the section's name.
const sectionsIndexTemplate = `{{/* index.html — the home page, and on a phone the list of
     sections: the view block says this page is the index, so below
     800px the rail is the whole page and this content is hidden; on a
     desktop the rail sits beside it. Give each section a nav link with
     an id here, and give its page an up block naming /#<that id>, so a
     phone comes back to the row it left. */}}
{{define "view"}}index{{end}}
{{define "nav"}}<a id="nav-overview" href="/overview">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// overviewTemplate is templates/overview.html, the one section page.
const overviewTemplate = `{{/* overview.html — a section. up is its way back: the index, with
     this section's nav link as the fragment, which is where focus
     returns on a phone even with scripts off. */}}
{{define "up"}}/#nav-overview{{end}}
{{define "nav"}}<a id="nav-overview" href="/overview" aria-current="page">%[1]s</a>{{end}}
{{define "content"}}
<h1>Hello, World — this is a rastrillo app.</h1>
{{end}}
`

// overviewHandler is appended to handlers.go for the two-page shells.
const overviewHandler = `
func (a *app) overview(w http.ResponseWriter, r *http.Request) {
	render(w, "overview", nil)
}
`

// overviewTest is appended to index_test.go for the two-page shells: the
// section page renders, and its back control goes to the index's row.
const overviewTest = `
func TestOverviewRenders(t *testing.T) {
	rec := get(t, newApp(t), "/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /overview: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), ` + "`" + `href="/#nav-overview" rel="up"` + "`" + `) {
		t.Errorf("GET /overview: no back control to the index's row:\n%s", rec.Body.String())
	}
}
`
```

Change the templates:

- `appTemplate`: `r.Get("/", a.index)` becomes `r.Get("/", a.index)%[2]s`;
- `handlersTemplate`: append `%[2]s` after the `index` handler's closing brace;
- `renderTemplate`: `for _, name := range []string{"index", "errors"} {` becomes `for _, name := range []string{%[3]s} {`;
- `indexTestTemplate`: append `%[3]s` at its very end (after `TestErrorPageRendersFrameworkPartial`'s closing brace).

In `runNew`, before the `files` map:

```go
	indexHTML, routes, handlers, pages, extraTests := indexTemplate, "", "", `"index", "errors"`, ""
	if twoPageShells[*shell] {
		indexHTML = fmt.Sprintf(sectionsIndexTemplate, scaffoldOverview)
		routes = "\n\tr.Get(\"/overview\", a.overview)"
		handlers = overviewHandler
		pages = `"index", "overview", "errors"`
		extraTests = overviewTest
	}
```

and use them: `fmt.Sprintf(appTemplate, pkg, routes)`, `fmt.Sprintf(handlersTemplate, pkg, handlers)`, `fmt.Sprintf(renderTemplate, name, pkg, pages)`, `fmt.Sprintf(indexTestTemplate, name, pkg, extraTests)`, `indexHTML` for `templates/index.html`; after the map, `if twoPageShells[*shell] { files[filepath.Join(appDir, "templates", "overview.html")] = fmt.Sprintf(overviewTemplate, scaffoldOverview) }`.

(`handlersTemplate` and `indexTestTemplate` already import `net/http`, `strings` and `testing`; `indexTestTemplate`'s existing `%%` escapes stay as they are.)

- [ ] **Step 4: Doctor's advisory**

In `cmd/rastrillo/doctor.go`, add `layoutAdvice string // relative path of a pre-H shell layout, if the app has one` to `report`, and:

```go
// doctorLayoutAdvisory is the one line doctor prints for a shell layout
// written before H (copy review, batch 1). "Upgrading" is the section of
// docs/site/templates.md that gives the edit.
const doctorLayoutAdvisory = `This layout still has the old mobile menu. See "Upgrading" in the templates guide.`

// oldShellLayout reports whether an app's layout.html predates the phone
// index: the drawer is in it, or it is a sidebar or console layout with
// no view block. Doctor cannot diff a layout, which is the app's own and
// edited from day one, but it can recognise these two shapes, and an app
// that re-vendored tokens.css keeps working on them (the legacy rules),
// so it is advice and never a failure.
func oldShellLayout(src string) bool {
	if strings.Contains(src, "rst-shell-chrome") {
		return true
	}
	shell := strings.Contains(src, "rst-shell-sidebar") || strings.Contains(src, "rst-shell-console")
	return shell && !strings.Contains(src, `{{block "view"`)
}
```

In `diagnose`, after `r.staticDir = rel(dir, staticDir)`:

```go
	layout := filepath.Join(filepath.Dir(staticDir), "templates", "layout.html")
	if b, err := os.ReadFile(layout); err == nil && oldShellLayout(string(b)) {
		r.layoutAdvice = rel(dir, layout)
	}
```

In `print`, directly after the file list's closing `fmt.Fprintln(w)`:

```go
	if r.layoutAdvice != "" {
		fmt.Fprintf(w, "%s: %s\n\n", r.layoutAdvice, doctorLayoutAdvisory)
	}
```

- [ ] **Step 5: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./cmd/rastrillo/`
Expected: PASS, including the whole existing scaffold and doctor suites, `TestAScaffoldedSidebarAppPassesItsOwnTests` (slow: it tidies and tests an app) and `TestScaffoldedAppTestsPass` (the default shell, unchanged). Mutation check: change `sectionsIndexTemplate`'s view to `page` and see the scaffold test fail; restore.

Then prove it by hand, the way a person would meet it (from a temp dir: `rastrillo new` takes a module name, not a path):

```bash
make build-cli
repo=$(pwd); tmp=$(mktemp -d)
(cd "$tmp" && "$repo/.build/rastrillo" new --shell=console consoleapp && cd consoleapp \
  && go mod edit -replace amadan.net/rastrillo/rastrillo="$repo" && GOFLAGS=-mod=mod go mod tidy && GOFLAGS=-mod=mod go test ./...)
```

Expected: `ok` for every package.

- [ ] **Step 6: Run the task gate** (all three commands).

- [ ] **Step 7: Commit and push**

```bash
git add cmd/rastrillo/new.go cmd/rastrillo/doctor.go cmd/rastrillo/new_test.go cmd/rastrillo/doctor_test.go cmd/rastrillo/shellscaffold_test.go
git commit -m "Scaffold an index and a section for the sidebar and console, and flag old shell layouts

A one-page sidebar or console app opened on a phone to an empty index,
because its only page was the index and content is hidden there. Those
two shells now scaffold the index with one section and the section page
with its way back. doctor recognises a layout written before the phone
index and prints one line pointing at the upgrade notes; the exit code
stays what it was, because the old layout still works on the new
stylesheet.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 11: Prerender through a `Speculation-Rules` header

Spec §4.6 and §10.6a. On by default; `Options.NoSpeculationRules` turns it off.

**Files:**
- Create: `speculation.go`, `speculation_test.go`, `ui/prerender_browser_test.go`
- Modify: `serve.go` (`Options.NoSpeculationRules`; `securityHeaders` takes the flag; `buildHandler` registers the route and passes the flag at both return paths)
- Modify: `docs/site/reference/rastrillo.md` (one Go fence with the constant, at the end of `## Options`; no prose)

**Interfaces:**
- Consumes: `buildHandler`, `helloMux`, `get`, `doGet` (root tests); Task 9's `shellLayoutPage`, `shellAssets`, `Layout("sidebar")`, `Layout("stage")`; Task 8's `tab`.
- Produces: `const SpeculationRulesPath = "/_speculation-rules"`; `Options.NoSpeculationRules bool`; `func securityHeaders(csp string, speculate bool, next http.Handler) http.Handler`.

- [ ] **Step 1: Write the failing unit tests**

Create `speculation_test.go`:

```go
package rastrillo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rulesHeader = `"` + SpeculationRulesPath + `"`

func catchAll() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("app")) })
	return mux
}

func mustBuild(t *testing.T, o Options) http.Handler {
	t.Helper()
	h, err := buildHandler(o)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	return h
}

// Every response names the rules file (a structured-field list of one
// string): browsers act on it for documents and ignore it elsewhere, so
// it is not sniffed for. The CSP is untouched: a header-delivered
// ruleset is not a script, and no CSP stops it loading.
func TestEveryResponseNamesTheSpeculationRules(t *testing.T) {
	h := mustBuild(t, Options{Mux: helloMux(&captured{})})
	for _, path := range []string{"/hello", "/healthz"} {
		rec := get(h, path, "")
		if got := rec.Header().Get("Speculation-Rules"); got != rulesHeader {
			t.Errorf("%s: Speculation-Rules = %q, want %q", path, got, rulesHeader)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != defaultCSP {
			t.Errorf("%s: the CSP changed: %q", path, got)
		}
	}
}

// The file: 200, the one content type browsers accept for a ruleset,
// cached a day, and document rules scoped by selector in both spellings
// to shell navigation and the back control.
func TestTheSpeculationRulesFileIsServed(t *testing.T) {
	for _, o := range []Options{
		{Mux: helloMux(&captured{})},
		{Mux: helloMux(&captured{}), Locales: []string{"en", "fr"}},
	} {
		rec := get(mustBuild(t, o), SpeculationRulesPath, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("locales %v: GET %s = %d", o.Locales, SpeculationRulesPath, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/speculationrules+json" {
			t.Errorf("Content-Type = %q; a browser refuses a ruleset served as anything else", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
			t.Errorf("Cache-Control = %q", cc)
		}
		var rules struct {
			Prerender []struct {
				Source    string `json:"source"`
				Where     struct{ SelectorMatches string `json:"selector_matches"` } `json:"where"`
				Eagerness string `json:"eagerness"`
			} `json:"prerender"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
			t.Fatalf("the rules are not JSON: %v\n%s", err, rec.Body.String())
		}
		if len(rules.Prerender) != 1 || rules.Prerender[0].Source != "document" || rules.Prerender[0].Eagerness != "moderate" {
			t.Fatalf("rules = %+v, want one moderate document rule", rules)
		}
		sel := rules.Prerender[0].Where.SelectorMatches
		for _, want := range []string{"[rst-shell-sidebar]", ".rst-shell-sidebar", "[rst-shell-console]", ".rst-shell-console",
			"[rst-shell-nav]", ".rst-shell__nav", "[rst-shell-back]", ".rst-shell__back", "a[href]"} {
			if !strings.Contains(sel, want) {
				t.Errorf("the selector %q does not name %q", sel, want)
			}
		}
	}
}

// The method-and-path pattern is more specific than an app's "/", so the
// framework answers the rules path even under a catch-all.
func TestTheFrameworkAnswersTheRulesPathOverACatchAll(t *testing.T) {
	rec := get(mustBuild(t, Options{Mux: catchAll()}), SpeculationRulesPath, "")
	if rec.Body.String() == "app" || !strings.Contains(rec.Body.String(), `"prerender"`) {
		t.Errorf("under a catch-all the app answered the rules path: %q", rec.Body.String())
	}
}

// The switch: no header and no route, so the path falls to the app like
// any other: a 404 from an app with nothing there, the app's own answer
// from one with a catch-all.
func TestNoSpeculationRulesSendsNothingAndServesNothing(t *testing.T) {
	h := mustBuild(t, Options{Mux: http.NewServeMux(), NoSpeculationRules: true})
	rec := get(h, "/", "")
	if got := rec.Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("NoSpeculationRules: the header is still sent: %q", got)
	}
	if rec := get(h, SpeculationRulesPath, ""); rec.Code != http.StatusNotFound {
		t.Errorf("NoSpeculationRules: GET %s = %d, want the app's 404", SpeculationRulesPath, rec.Code)
	}
	if rec := get(mustBuild(t, Options{Mux: catchAll(), NoSpeculationRules: true}), SpeculationRulesPath, ""); rec.Body.String() != "app" {
		t.Errorf("NoSpeculationRules: a catch-all app did not get the path: %q", rec.Body.String())
	}
}

// Set before any app code runs, like the other baseline headers, so a
// handler or Options.Wrap that deletes it wins.
func TestAHandlerCanDropTheSpeculationRulesHeader(t *testing.T) {
	h := mustBuild(t, Options{Mux: helloMux(&captured{}), Wrap: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("Speculation-Rules")
			next.ServeHTTP(w, r)
		})
	}})
	if got := get(h, "/hello", "").Header().Get("Speculation-Rules"); got != "" {
		t.Errorf("a Wrap that Dels the header did not remove it: %q", got)
	}
}

// Review Focus 5: the framework's route is GET (and so HEAD); a POST to
// the path is the app's business.
func TestSpeculationRulesRouteIsGETOnly(t *testing.T) {
	h := mustBuild(t, Options{Mux: catchAll()})
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, SpeculationRulesPath, nil))
	if head.Code != http.StatusOK || head.Header().Get("Content-Type") != "application/speculationrules+json" {
		t.Errorf("HEAD %s = %d %q, want the GET route's answer", SpeculationRulesPath, head.Code, head.Header().Get("Content-Type"))
	}
	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, SpeculationRulesPath, nil))
	if post.Body.String() != "app" {
		t.Errorf("POST %s went to the framework (%d %q), want the app", SpeculationRulesPath, post.Code, post.Body.String())
	}
}
```

Run: `GOFLAGS=-mod=mod go test -run 'Speculation|TheFrameworkAnswersTheRulesPath|NoSpeculationRules|AHandlerCanDrop' -count=1 .`
Expected: FAIL to compile (`undefined: SpeculationRulesPath`, unknown field `NoSpeculationRules`).

- [ ] **Step 2: Write `speculation.go`**

```go
package rastrillo

import (
	"io"
	"net/http"
)

// SpeculationRulesPath is where Serve answers with its speculation
// rules, and what the Speculation-Rules header on every response names.
// The rules prerender the sidebar and console shells' navigation and
// back control, so a phone's next page is ready before it is tapped;
// they match nothing on any other page. Options.NoSpeculationRules
// turns both the header and the route off.
const SpeculationRulesPath = "/_speculation-rules"

// speculationRules is the whole ruleset: document rules scoped by a
// selector, in both markup spellings, to shell navigation and the back
// control. moderate prerenders on a 200ms hover on a desktop and on
// pointer-down on a phone. A GET never mutates, and a handler can tell a
// prerender from its Sec-Purpose: prefetch;prerender request header.
//
// It is delivered by the header rather than inline because the default
// CSP refuses an inline <script type="speculationrules">, and a header
// ruleset is not a script: no CSP stops it loading. Each prerender is
// still a same-origin navigation that carries the app's own CSP.
const speculationRules = `{"prerender":[{"source":"document","where":{"selector_matches":":is([rst-shell-sidebar],[rst-shell-console],.rst-shell-sidebar,.rst-shell-console) :is([rst-shell-nav],[rst-shell-back],.rst-shell__nav,.rst-shell__back) a[href]"},"eagerness":"moderate"}]}`

// serveSpeculationRules answers SpeculationRulesPath. The content type
// is the one browsers accept for a ruleset: a static file would go out
// as application/json, which they refuse.
func serveSpeculationRules(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "application/speculationrules+json")
	h.Set("Cache-Control", "public, max-age=86400")
	io.WriteString(w, speculationRules)
}
```

- [ ] **Step 3: Wire it into `serve.go`**

In `Options`, after `CSP`:

```go
	// NoSpeculationRules turns off prerendering of shell navigation. By
	// default every response carries a Speculation-Rules header naming
	// SpeculationRulesPath, which Serve answers; with this set there is
	// neither, and the path reaches the app like any other. Named as a
	// negative because the default is on and a zero Options must mean
	// the default (Options.CSP is the precedent). An app whose pages are
	// expensive to render turns it off here, or Dels the header in a
	// handler or Options.Wrap; a CSP is not an off switch, because a
	// header ruleset is not a script.
	NoSpeculationRules bool
```

Change `securityHeaders`:

```go
func securityHeaders(csp string, speculate bool, next http.Handler) http.Handler {
	if csp == "" {
		csp = defaultCSP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		// … the existing headers unchanged …
		h.Set("Strict-Transport-Security", "max-age=31536000")
		// On every response rather than sniffed for HTML: browsers act
		// on it for documents and ignore it elsewhere. A structured-field
		// list of one string, which is the header's form.
		if speculate {
			h.Set("Speculation-Rules", `"`+SpeculationRulesPath+`"`)
		}
		next.ServeHTTP(w, r)
	})
}
```

In `buildHandler`, after the `NextDue` registration and before `app := …`:

```go
	// Before mux.Handle("/", app) and before the no-locales return, so
	// both return paths serve it; the method-and-path pattern outranks an
	// app's "/" catch-all. The locale middleware lets an unprefixed path
	// through (it negotiates a locale rather than requiring a prefix).
	if !opts.NoSpeculationRules {
		mux.HandleFunc("GET "+SpeculationRulesPath, serveSpeculationRules)
	}
```

and change both `securityHeaders(opts.CSP, …)` calls to `securityHeaders(opts.CSP, !opts.NoSpeculationRules, …)`.

In `docs/site/reference/rastrillo.md`, at the end of the `## Options` section (after the `ErrorPage` paragraph and its code), add a Go fence holding only `const SpeculationRulesPath = "/_speculation-rules"`.

Run: `GOFLAGS=-mod=mod go test -count=1 . ./internal/docsite/`
Expected: PASS, including every existing `buildHandler` and `Wrap` test (`TestBuildHandlerSetsSecurityHeaders` and `TestBuildHandlerCSPOverride` see the CSP unchanged).

- [ ] **Step 4: Write the browser drive**

Create `ui/prerender_browser_test.go`:

```go
//go:build browser

package ui

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
)

// prerenderSite is a sidebar app served through rastrillo.Handler, so
// the headers are the framework's own (the CSP included): the index,
// two sections, a plain link in the index's content, and a stage page
// with a link on it. Every document request is reported with its
// Sec-Purpose, which is how a prerender is seen (headless Chromium
// fetches the page for it; enabling CDP's Preload domain would switch
// prerendering off, measured).
func prerenderSite(t *testing.T, noRules bool, hits chan<- string) func(string) http.Handler {
	return func(string) http.Handler {
		src, _ := Layout("sidebar")
		stage, _ := Layout("stage")
		pages := map[string]string{
			"/":         shellLayoutPage(t, src, "ltr", `{{define "view"}}index{{end}}`, `{{define "content"}}<h1>Harbour</h1><p><a id="plain" href="/plain">A link in the page</a></p>{{end}}`),
			"/invoices": shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-invoices{{end}}`),
			"/orders":   shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-orders{{end}}`),
			"/team":     shellLayoutPage(t, src, "ltr", `{{define "up"}}/#nav-team{{end}}`),
			"/plain":    shellLayoutPage(t, src, "ltr"),
			"/signin":   shellLayoutPage(t, stage, "ltr", `{{define "content"}}<p><a id="stage-link" href="/orders">Orders</a></p>{{end}}`),
		}
		mux := shellAssets(t, pages)
		h, closeAll, err := rastrillo.Handler(rastrillo.Options{Mux: recordDocuments(mux, hits), NoSpeculationRules: noRules})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeAll() })
		return h
	}
}

// recordDocuments reports every request for a page (not an asset) with
// its Sec-Purpose.
func recordDocuments(next *http.ServeMux, hits chan<- string) *http.ServeMux {
	out := http.NewServeMux()
	out.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ".") {
			select {
			case hits <- r.URL.Path + " " + r.Header.Get("Sec-Purpose"):
			default:
			}
		}
		next.ServeHTTP(w, r)
	})
	return out
}

func drain(c chan string) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

// waitFor reports the first hit for path within d, or "".
func waitFor(c chan string, path string, d time.Duration) string {
	deadline := time.After(d)
	for {
		select {
		case h := <-c:
			if strings.HasPrefix(h, path+" ") {
				return h
			}
		case <-deadline:
			return ""
		}
	}
}

func centre(t *testing.T, ctx context.Context, sel string) (float64, float64) {
	p := probe(t, ctx, sel, 0.5, 0.5, 0, 0)
	return p.X, p.Y
}

// TestShellNavigationIsPrerendered is §10.6a's browser half: a hover on
// a nav link (desktop) and a pointer-down on one (phone) start a
// prerender of it, with no CSP violation; a link outside the shell's nav
// and back control starts none; a stage page prerenders nothing; and
// with Options.NoSpeculationRules the same hover starts nothing, which
// is the control that says the header is what did it.
func TestShellNavigationIsPrerendered(t *testing.T) {
	hits := make(chan string, 64)
	rig := harness.New(t, prerenderSite(t, false, hits))
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	rig.Run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(rig.Origin+"/"))
	rig.Screen("[rst-shell-nav]", "the index, served with the framework's CSP")
	drain(hits)
	x, y := centre(t, ctx, "#nav-invoices")
	chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/invoices", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Errorf("a hover on a nav link started no prerender (got %q)", h)
	}
	x, y = centre(t, ctx, "#plain")
	chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/plain", 2*time.Second); h != "" {
		t.Errorf("a link outside the shell's navigation was prerendered: %q", h)
	}
	rig.Run(chromedp.Navigate(rig.Origin + "/signin"))
	rig.Screen("#stage-link", "the stage page")
	drain(hits)
	x, y = centre(t, ctx, "#stage-link")
	chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(hits, "/orders", 2*time.Second); h != "" {
		t.Errorf("a stage page prerendered %q; the rules match nothing outside the sidebar and console", h)
	}

	touchHits := make(chan string, 64)
	touch := harness.New(t, prerenderSite(t, false, touchHits), harness.WithCoarsePointer())
	tctx, tcancel := context.WithTimeout(touch.Context(), 60*time.Second)
	defer tcancel()
	touch.Run(chromedp.EmulateViewport(390, 844), chromedp.Navigate(touch.Origin+"/"))
	touch.Screen("[rst-shell-nav]", "the phone index")
	requirePointer(t, tctx, true)
	drain(touchHits)
	x, y = centre(t, tctx, "#nav-orders")
	chromedp.Run(tctx, chromedp.MouseEvent(input.MousePressed, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if h := waitFor(touchHits, "/orders", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Errorf("a pointer-down on a nav row on a phone started no prerender (got %q)", h)
	}
	chromedp.Run(tctx, chromedp.MouseEvent(input.MouseReleased, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))

	offHits := make(chan string, 64)
	off := harness.New(t, prerenderSite(t, true, offHits))
	octx, ocancel := context.WithTimeout(off.Context(), 60*time.Second)
	defer ocancel()
	off.Run(chromedp.EmulateViewport(1280, 900), chromedp.Navigate(off.Origin+"/"))
	drain(offHits)
	x, y = centre(t, octx, "#nav-invoices")
	chromedp.Run(octx, chromedp.MouseEvent(input.MouseMoved, x, y))
	if h := waitFor(offHits, "/invoices", 3*time.Second); h != "" {
		t.Errorf("CONTROL: with NoSpeculationRules a hover still fetched %q, so the header is not what the legs above measured", h)
	}
}

// TestAPrefetchedIndexStillReturnsFocus: headless Chromium does not
// activate a prerender (it delivers the page from the prefetch, measured:
// deliveryType "navigational-prefetch"), so this is the nearest the
// harness gets to §10.3's prerendered-index leg: a deep link's back
// control, pressed long enough for the rules to fetch the index, lands
// on an index delivered from the speculation, with focus on the section
// it left. The activation itself is on the by-hand list.
func TestAPrefetchedIndexStillReturnsFocus(t *testing.T) {
	hits := make(chan string, 64)
	rig := harness.New(t, prerenderSite(t, false, hits), harness.WithCoarsePointer())
	ctx, done, _ := tab(t, rig, "")
	defer done()
	visit(t, ctx, rig.Origin+"/invoices")
	settleUntil(t, ctx, `!!document.querySelector("[rst-shell-back] a")`)
	x, y := centre(t, ctx, "[rst-shell-back] a")
	chromedp.Run(ctx, chromedp.MouseEvent(input.MousePressed, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if h := waitFor(hits, "/", 4*time.Second); !strings.Contains(h, "prerender") {
		t.Fatalf("pressing the back control fetched no speculation of the index (got %q); the leg has not arisen", h)
	}
	chromedp.Run(ctx, chromedp.MouseEvent(input.MouseReleased, x, y, chromedp.ButtonLeft, chromedp.ClickCount(1)))
	settleUntil(t, ctx, `location.pathname === "/"`)
	var delivery string
	chromedp.Run(ctx, chromedp.Evaluate(`performance.getEntriesByType("navigation")[0].deliveryType`, &delivery))
	if delivery != "navigational-prefetch" {
		t.Fatalf("the index was delivered as %q, not from the speculation; the leg has not arisen", delivery)
	}
	settleUntil(t, ctx, `document.activeElement.id === "nav-invoices"`)
}
```

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestShellNavigationIsPrerendered|TestAPrefetchedIndexStillReturnsFocus' -count=1 -v ./ui/`
Expected: PASS (the Screen calls also prove no CSP violation and no 404 for `shell.js`/`shell.css` on a page served with the framework's own headers). Mutation check: drop `[rst-shell-back],.rst-shell__back` from the rules' selector; `TestAPrefetchedIndexStillReturnsFocus` fails on its premise; restore. (The control leg is the mutation check for the header itself.)

- [ ] **Step 5: Run the task gate** (all three commands).

- [ ] **Step 6: Commit and push**

```bash
git add speculation.go speculation_test.go serve.go docs/site/reference/rastrillo.md ui/prerender_browser_test.go
git commit -m "Prerender shell navigation through a Speculation-Rules header

The prototype prerendered the next section with an inline rules block,
which the default CSP refuses. Serve now sends a Speculation-Rules
header on every response naming a rules file it serves itself, scoped
by selector to sidebar and console navigation and the back control, so
it matches nothing anywhere else and no CSP has to widen. It is on by
default; Options.NoSpeculationRules turns off the header and the route,
and a handler can still Del the header like any baseline header.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 12: Copy review, batch 3: the docs, SKILL.md, the changelog body and the gallery's shell blurbs (controller)

Run by the controller with the `copy-review` skill once Tasks 1-11 are green, so every sentence describes code that exists. Nothing is written into a tracked file here; Task 13 writes the approved text.

**Files:**
- Create (gitignored): `copy-review/strings.json` (batch 3), `copy-review/batch3-result.json`

**Interfaces:**
- Consumes: the green tree of Task 11; `copy-review/batch1-result.json` (the changelog heading is already approved there and is not re-reviewed).
- Produces: `copy-review/batch3-result.json` with the ids below.

- [ ] **Step 1: Keep batch 2's result**

```bash
test -f copy-review/batch2-result.json || { jq -e '.action == "approve"' copy-review/result.json && cp copy-review/result.json copy-review/batch2-result.json; }
```

- [ ] **Step 2: Write `copy-review/strings.json`**

The drafts. Every one is new user-facing copy: plain, short, an instruction where one is possible, no em dashes (the files they land in use em dashes in older text; new sentences do not). Markdown and code spans are part of the text and are reviewed as written.

```json
[
  {"id": "docs.templates.funcs", "section": "templates.md: Template functions", "label": "The functions sentence", "text": "`ui.Funcs()` registers `dict`, `list`, `menuGroup`, `searchClear`, `icon`, `iconAssets`, `T`, `Tf`, `dateWords`, `opt`, `Tbdi`, `stageArt` and `rowMenuItems`.", "context": "Replaces the same sentence without rowMenuItems."},
  {"id": "docs.templates.rows", "section": "templates.md: list rows", "label": "Whole-row rule", "text": "A row that stands for a record is a link across its whole width: its name link covers the row, and the row's other buttons, checkboxes and menus sit on top of it. Give each row one name link. A row with no link does not look or act clickable.", "context": "New paragraph after the one on rst-lrow being a layout grid (templates.md:137-146)."},
  {"id": "docs.templates.row_menu", "section": "templates.md: list rows", "label": "The row menu", "text": "Put a row's other actions in `row-menu`, at the end of the row. Each item is a link (`Href`) or a form that posts (`Action`, with `Hidden` fields). Put a destructive item last: it links to its confirm page and its label ends with …. Inside a bulk-selection form, every item must be a link. `list-row-action` takes the same list as `Menu` and puts the menu after its action pill.", "context": "New paragraph after the whole-row rule."},
  {"id": "docs.templates.cols", "section": "templates.md: Set a grid's columns", "label": "The kebab column", "text": "End `--rst-cols` with `var(--rst-col-menu)` when the rows have a ⋮ menu. The column is 32px on a desktop and 44px on a phone.", "context": "Added under the CSS sample, which changes to `.orders { --rst-cols: 2fr 110px var(--rst-col-menu); }`."},
  {"id": "docs.templates.menus_group", "section": "templates.md: Menus close each other", "label": "What stays out of the group", "text": "The toggle-block is outside the group: it is not a menu. So is the Menu button of the topbar and console on a phone: the account menu opens inside it, and in the same group it would close it.", "context": "Replaces 'The sidebar shell's rst-shell-chrome strip and the toggle-block are deliberately outside the group…' (templates.md:167-170)."},
  {"id": "docs.templates.shells_sidebar", "section": "templates.md: Shells table", "label": "sidebar row", "text": "a rail of nav groups beside the page; on a phone, an index page of sections and a back control", "context": "The table cell for `sidebar`, replacing 'a left rail of nav groups, collapsing to a <details> chrome bar below 800px'."},
  {"id": "docs.templates.shells_console", "section": "templates.md: Shells table", "label": "console row", "text": "both at once: a bar across the top with the rail beneath it down the side; on a phone, the bar's menu opens as a card and the rail is an index page", "context": "The table cell for `console`."},
  {"id": "docs.templates.blocks", "section": "templates.md: Shells", "label": "The blocks sentence", "text": "The blocks are `title`, `lang`, `dir` and `head` in all five shells, plus `brand`, `nav`, `account` and `locale` in `topbar`, `sidebar` and `console`, `view` and `up` in `sidebar` and `console`, `foot` in `topbar`, `console` and `stage`, and `backdrop` in `stage`.", "context": "Replaces the same sentence without view and up."},
  {"id": "docs.templates.attributes", "section": "templates.md: Shells", "label": "The attributes paragraph", "text": "The chrome attributes live in `tokens.css` like every other idiom: `rst-shell-topbar`, `rst-shell-bar`, `rst-shell-brand`, `rst-shell-nav`, `rst-shell-account` and `rst-shell-foot` for the topbar, with `rst-shell-menu` and `rst-shell-tail` for its phone menu; `rst-shell-sidebar`, `rst-shell-rail`, `rst-shell-group`, `rst-shell-main`, `rst-shell-title` and `rst-shell-back` for the sidebar; `rst-shell-console` for the console, which reuses the rest; and `rst-skip`, the skip link, in all five shells. None of it needs JavaScript.", "context": "Replaces templates.md:970-980, which ended with the sidebar drawer."},
  {"id": "docs.templates.phone", "section": "templates.md: Shells", "label": "New section: On a phone", "text": "### On a phone: an index and a way back\n\nBelow 800px, `sidebar` and `console` show each page in one of two ways. Your index page is the list of sections. Every other page shows its content, with a back control at the top that returns to that list. There is no menu button to find.\n\nMark your index page with the `view` block:\n\n```html\n{{define \"view\"}}index{{end}}\n```\n\nGive every other page an `up` block that points back to its own row on the index, and give that row the matching id:\n\n```html\n{{define \"up\"}}/#nav-invoices{{end}}\n{{define \"nav\"}}<a id=\"nav-invoices\" href=\"/invoices\" aria-current=\"page\">Invoices</a>…{{end}}\n```\n\nWith JavaScript off, the fragment brings the reader back to the row they left. With `shell.js`, the back control uses the browser's history when it can, the pages slide, and focus returns to the row. A page with no `view` block is a content page, so a page you forget still shows its content and a way back.\n\nIn `topbar` and `console`, the Menu button on a phone opens a card over the page. A tap outside it, or Escape, closes it. The page underneath does not move.", "context": "A new section after the shells' attribute paragraph."},
  {"id": "docs.templates.prerender", "section": "templates.md: Shells", "label": "Prerendering", "text": "`rastrillo.Serve` prerenders the pages your sidebar and console navigation link to, so the next page is ready when it is tapped. Nothing on other pages is prerendered. A prerender is a GET, so a GET must never change anything. To turn it off, set `Options.NoSpeculationRules`.", "context": "A paragraph at the end of the new section."},
  {"id": "docs.templates.console", "section": "templates.md: The console", "label": "Console section, new heading and body", "text": "### The console on a phone\n\n`console` has two pieces of chrome to put away below 800px, and each goes the way it goes in the shell it comes from. The bar's account and language menus go behind the Menu button, which opens a card, as in `topbar`. The rail is an index page, as in `sidebar`: mark the index with `view` and give other pages `up`.\n\nNothing is reordered at any width. The DOM order, bar, rail, page, is the reading order and the focus order at 320px and at 1280px, in both directions of the language.", "context": "Replaces '### The console folds two chromes behind one control' and its three paragraphs (templates.md:990-1009)."},
  {"id": "docs.templates.upgrading", "section": "templates.md: Upgrading", "label": "New section: Upgrading to the phone index", "text": "### Upgrading to the phone index\n\nA layout from before this release keeps working: its sidebar drawer still opens. To move to the index:\n\n1. Upgrade the module and run `rastrillo doctor --fix`. It re-copies `tokens.css` and adds `shell.js` and `shell.css`.\n2. Replace `templates/layout.html` with the new shell (`ui.Layout(\"sidebar\")` or `ui.Layout(\"console\")`), and carry your own edits across.\n3. Add `{{define \"view\"}}index{{end}}` to your index page, and an `up` block to every other page.\n4. If you already have a template called `view` or `up`, rename it. The shells use those names now.\n5. In a list grid, end `--rst-cols` with `var(--rst-col-menu)` instead of `32px`.\n\n`rastrillo new --shell=sidebar` and `--shell=console` now write two pages, an index and an Overview section, to show the shape.\n\nAn app on `topbar`, `column` or `stage` can delete `shell.js` and `shell.css`. Add both to `vendoredIsMine` in `vendored_test.go`, or that test fails on the missing files.", "context": "A new section before '### Upgrading: the topbar's tail is a level deeper'. rastrillo doctor's advisory points here ('See \"Upgrading\" in the templates guide')."},
  {"id": "docs.icons.menu", "section": "icons.md", "label": "menu icon sentence", "text": "The topbar and console use `menu` for their phone menu.", "context": "Replaces 'The shells use `menu` when they collapse.' (icons.md:34)."},
  {"id": "docs.ref.ui.shell", "section": "reference/ui.md", "label": "ShellJS and ShellCSS", "text": "`ShellJS` is `shell.js` and `ShellCSS` is `shell.css`: the sidebar and console shells' phone navigation, with the slide between pages, a back control that uses the browser's history when it can, and focus returned to the section you left. Only those two layouts link them, and both are optional.", "context": "After the vendored-assets code block."},
  {"id": "docs.ref.ui.funcs", "section": "reference/ui.md", "label": "Funcs sentence", "text": "Registers `dict`, `list`, `menuGroup`, `searchClear`, `icon`, `iconAssets`, `T`, `Tf`, `dateWords`, `opt`, `Tbdi`, `stageArt` and `rowMenuItems`. `rowMenuItems` checks `row-menu`'s items and stops the render on one it cannot show.", "context": "Replaces the Funcs sentence."},
  {"id": "docs.ref.rastrillo.speculation", "section": "reference/rastrillo.md: Options", "label": "NoSpeculationRules", "text": "**`NoSpeculationRules`**: turns off prerendering. By default every response names `SpeculationRulesPath`, where `Serve` answers with rules that prerender sidebar and console navigation. A content-security policy does not turn it off; this field does, or deleting the `Speculation-Rules` header in a handler.", "context": "A new Options entry, above the constant's code block."},
  {"id": "docs.ref.harness.coarse", "section": "reference/harness.md: Options", "label": "WithCoarsePointer", "text": "`WithCoarsePointer` launches Chromium with a touch screen as its main pointer, so `(pointer: coarse)` matches. CDP's touch emulation does not do this, and a phone drive that ran on a mouse pointer would pass at desktop sizes.", "context": "After the WithScrollbars paragraph."},
  {"id": "skill.shells", "section": "SKILL.md", "label": "Shells on a phone", "text": "Shells on a phone: `topbar` and `console` put their narrow chrome in the Menu card. `sidebar` and `console` rails become an index page: mark it `{{define \"view\"}}index{{end}}`; every other page names its way back with `{{define \"up\"}}/#nav-x{{end}}` and the nav link gets `id=\"nav-x\"`. Never build a hamburger drawer.", "context": "SKILL.md is what an LLM loads to build an app; near the shells paragraph (SKILL.md:369-373)."},
  {"id": "skill.rows", "section": "SKILL.md", "label": "Rows and row menus", "text": "A row that stands for an item is a link across its whole width through its one name link; never link only the name. Row actions use `row-menu`, and a destructive one is a link to its confirm page. Inside a bulk-selection form, row-menu items are links only.", "context": "Near the menus sentence (SKILL.md:445-448)."},
  {"id": "skill.get", "section": "SKILL.md", "label": "GET never mutates", "text": "A GET never changes anything: `Serve` prerenders shell navigation (turn it off with `Options.NoSpeculationRules`).", "context": "SKILL.md does not say this today, and prerendering makes it load-bearing."},
  {"id": "changelog.body", "section": "CHANGELOG", "label": "Entry body", "text": "An app pinned below v0.26.0 zooms on every form on a phone. Upgrade the module, then run `rastrillo doctor --fix`: it re-copies `tokens.css` and adds `shell.js` and `shell.css`.\n\nOn a phone or a narrow window, text is one step bigger (16px body text) and every control is at least 44px. Desktops are unchanged, except three things: a list row is clickable across its width, its focus ring goes round the whole row, and a row's checkbox has a 24px target.\n\nThe sidebar and console shells have no menu button on a phone. The index page lists the sections, and every other page has a back control. Mark your index with `{{define \"view\"}}index{{end}}` and give other pages an `up` block; see \"Upgrading\" in the [templates guide](/docs/templates). Old layouts keep working, and `rastrillo doctor` tells you when yours is one. The topbar's menu opens as a card over the page and closes on a tap outside it or Escape.\n\nNew: the `row-menu` partial, `Menu` on `list-row-action`, `--rst-col-menu`, `ui.ShellJS` and `ui.ShellCSS`, `rastrillo.SpeculationRulesPath` and `Options.NoSpeculationRules`. `Serve` prerenders sidebar and console navigation by default.\n\nWatch for two things. A row control made from a `<div>` with a click handler is now under the row's link; use a real button or link. A template of yours called `view` or `up` clashes with the new blocks; rename it.", "context": "The body under the approved heading from batch 1. Its first paragraph leads with the upgrade, as the spec requires."},
  {"id": "gallery.shell_blurb_sidebar", "section": "Gallery: the Shells page", "label": "sidebar blurb", "text": "A navigation rail beside the page. On a phone, the rail is an index page and every other page has a back control. No JavaScript needed.", "context": "Replaces 'A navigation rail beside the page, collapsing below 800px into a details disclosure. No JavaScript.' Eleven translations follow."},
  {"id": "gallery.shell_blurb_console", "section": "Gallery: the Shells page", "label": "console blurb", "text": "A bar across the top and a navigation rail down the side at once, the shape most admin consoles are. On a phone, the bar's menu opens as a card and the rail is an index page. No JavaScript needed.", "context": "Replaces '… Below 800px one disclosure folds both. No JavaScript.'"},
  {"id": "gallery.idiom_blurb_sidebar", "section": "Gallery: UI primitives", "label": "shell-sidebar idiom blurb", "text": "The sidebar shell's chrome on a content page: on a phone, a back control to the index.", "context": "Replaces 'The sidebar shell's chrome, collapsing below 800px into a details disclosure.'"}
]
```

- [ ] **Step 3: Run the review**

Invoke the `copy-review` skill on it and follow it to the end. On approve:

```bash
jq -e '.action == "approve"' copy-review/result.json && cp copy-review/result.json copy-review/batch3-result.json
jq -r '.strings[].id' copy-review/batch3-result.json | wc -l
```

Expected: `25`. No commit.

---

### Task 13: Write batch 3: the docs, SKILL.md, the changelog, and the gallery's shell blurbs

Spec §6, the changelog half of §7, §11.

**Files:**
- Modify: `docs/site/templates.md`, `docs/site/icons.md`, `docs/site/reference/ui.md`, `docs/site/reference/rastrillo.md`, `docs/site/reference/harness.md`
- Modify: `SKILL.md`
- Modify: `CHANGELOG.md` (a new entry at the top of `## Unreleased`)
- Modify: `internal/designsystem/page.go` (`shellViews`' sidebar and console blurbs; `idiomBlurbs["shell-sidebar"]`), `internal/designsystem/prose.go` (the three old entries replaced by the approved three, eleven translations each)

**Interfaces:**
- Consumes: `copy-review/batch3-result.json`, `copy-review/batch1-result.json` (`changelog.heading`).
- Produces: documentation only; no code interfaces.

- [ ] **Step 1: Write the approved text into its files**

Read each string with `jq -r --arg id <id> '.strings[] | select(.id==$id) | .text' copy-review/batch3-result.json` and place it exactly as its `context` says, verbatim:

- `docs/site/templates.md`: `docs.templates.funcs` over lines 24-26; `docs.templates.rows` and then `docs.templates.row_menu` as new paragraphs after line 146; the `.orders` sample at line 313 becomes `.orders { --rst-cols: 2fr 110px var(--rst-col-menu); }` with `docs.templates.cols` under it; `docs.templates.menus_group` over lines 167-170; the table cells `docs.templates.shells_sidebar` and `docs.templates.shells_console` (lines 927-928); `docs.templates.blocks` over lines 948-953; `docs.templates.attributes` over lines 970-980; `docs.templates.phone` then `docs.templates.prerender` as a new section after the `stage` paragraph (line 988); `docs.templates.console` over lines 990-1009; `docs.templates.upgrading` as a new section immediately before `### Upgrading: the topbar's tail is a level deeper`. Line 619's sentence about the rail folding into a `<details>` chrome strip stays: it describes the gallery's own frame, which converts with B.
- `docs/site/icons.md:34`: `docs.icons.menu`.
- `docs/site/reference/ui.md`: `docs.ref.ui.funcs` over the Funcs sentence; `docs.ref.ui.shell` after the vendored-assets fence's existing paragraph about `TokensCSS`/`ShimJS`.
- `docs/site/reference/rastrillo.md`: `docs.ref.rastrillo.speculation` directly above the constant's fence (Task 11).
- `docs/site/reference/harness.md`: `docs.ref.harness.coarse` after the `WithScrollbars` paragraph.
- `SKILL.md`: `skill.shells` after the paragraph at lines 369-377 (the design-system default), `skill.rows` and `skill.get` into the bullet at lines 440-452, after its menus sentence.

Run: `wc -c SKILL.md` (must be ≤ 30,000; about 27,300 expected) and `GOFLAGS=-mod=mod go test -count=1 . ./internal/docsite/`
Expected: PASS (`TestSkillMDStaysWithinBudget`, `TestInternalLinksResolve`, `TestAnchorsAreUnique`, `TestGoFencesParse`, `TestTemplatesPageListsEveryPartial`, `TestExportedSymbolsAreDocumented`). If the budget test fails, trim genuinely redundant prose elsewhere in SKILL.md; never a load-bearing fact (AGENTS.md).

- [ ] **Step 2: The changelog**

At the top of `## Unreleased` in `CHANGELOG.md`, add the approved heading (`jq -r '.strings[] | select(.id=="changelog.heading") | .text' copy-review/batch1-result.json`, which is already a `### …` line) followed by a blank line and the approved `changelog.body`.

- [ ] **Step 3: The gallery's shell blurbs**

In `internal/designsystem/page.go`, set `shellViews`' `"sidebar"` and `"console"` blurbs to `gallery.shell_blurb_sidebar` and `gallery.shell_blurb_console`, and `idiomBlurbs["shell-sidebar"]` to `gallery.idiom_blurb_sidebar`. In `internal/designsystem/prose.go`, delete the three entries keyed by the old English (`A navigation rail beside the page, collapsing below 800px into a details disclosure. No JavaScript.`, the console's `… Below 800px one disclosure folds both. No JavaScript.`, and `The sidebar shell's chrome, collapsing below 800px into a details disclosure.`) and add the three approved strings, each with its eleven translations drafted from the approved English.

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS (`TestEveryProseKeyIsTranslated` finds no stale entry and every new key translated; the leak gate is clean).

- [ ] **Step 4: A last read for truth**

`grep -n "rst-shell-chrome\|chrome strip\|drawer\|hamburger" docs/site/*.md docs/site/reference/*.md SKILL.md ui/*.go ui/layouts/*.html` must show only: the legacy notes in `tokens.css`-adjacent comments, the gallery-frame sentence in `templates.md` (line 619), the upgrade section's "drawer still opens", and SKILL.md's "Never build a hamburger drawer". Any other hit is a sentence still describing the drawer as current: fix it in code comments directly (comments are code), or stop and take it to the controller if it is user-facing copy that batch 3 missed.

- [ ] **Step 5: Run the task gate** (all three commands).

- [ ] **Step 6: Commit and push**

```bash
git add docs/site/templates.md docs/site/icons.md docs/site/reference/ui.md docs/site/reference/rastrillo.md docs/site/reference/harness.md SKILL.md CHANGELOG.md internal/designsystem/page.go internal/designsystem/prose.go
git commit -m "Document the phone index, the card, whole rows, the row menu and prerendering

The docs, SKILL.md and the gallery still described a sidebar drawer
that no longer ships. They now say how a page marks the index and names
its way back, how to upgrade an old layout (doctor's advisory points
there), that rows are links across their width, how row-menu takes its
items, and that Serve prerenders shell navigation, which is why SKILL.md
now says a GET never changes anything. The changelog leads with the
upgrade, because apps pinned before the zoom fix still zoom.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin mobile-ergonomics
```

---

### Task 14: The whole gate, the push, and what only a person can check

**Files:** none changed unless the gate finds something.

**Interfaces:** consumes everything above.

- [ ] **Step 1: Run the whole gate**

Run: `make ci`
Expected: every target green: `gofmt`, `money`, `root` (with `RASTRILLO_TEST_REQUIRE_NODE=1`), `chromedp-graph`, `gorm-free`, `race`, `example-helloworld`, `example-blog`, `example-tickets`, `example-notes`, `generate-check`, `scaffold-smoke`, `browser`. A failure is fixed in the task it belongs to (amend nothing; a new commit on the branch whose body names the task).

- [ ] **Step 2: Check the budgets by hand, once**

```bash
wc -c ui/rastrillo.js ui/shell.js SKILL.md
```

Expected: under 16,384, 8,192 and 30,000. Confirm the number written into `TestShimIsSmall`'s comment in Task 7 is the one `wc` prints.

- [ ] **Step 3: Describe the branch**

Follow the amadan skill (`.claude/skills/amadan/SKILL.md`) to describe `mobile-ergonomics`: the description is the PR body. It summarises what landed, links the spec and this plan, and carries this list for the person who merges, unchecked, because no drive in this repository can do it:

```markdown
## By hand, before merge (spec §10.8)

On a real iPhone (Safari) and a real Android phone (Chrome), on a scaffolded sidebar app and the gallery's demo:

- [ ] No field zooms the page when focused: a plain field, the primary field, the search box, a date field, a field in a bulk bar.
- [ ] The slide goes the right way both ways, and in an RTL locale mirrors.
- [ ] The system back swipe (iOS Safari) does not animate twice.
- [ ] VoiceOver and TalkBack on the index: the heading is announced, each row is a link with its name; the back control is announced as "Back to …"; the topbar's Menu card opens, reads its items, and closes on Escape (external keyboard) and on a tap outside.
- [ ] A prerendered index: open a section, press and hold the back control, release; the index appears at once and focus is on the row you left (headless Chromium cannot activate a prerender, so the drives cover the prefetched case only).
```

End the description with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

- [ ] **Step 4: Push**

```bash
git push origin mobile-ergonomics
```

(retry on a 503 from amadan). The branch lands through `amadan branch merge` after review, never a squash (AGENTS.md).

---

## Self-review

Run against the spec after writing; fixes are already folded into the tasks above.

**Spec coverage.** §1.1-1.3 Task 2; §1.4 and the calendar Task 3; §1.5 Tasks 2-4 (desktop pins); §2 Task 4 (§2.3's sizes in Task 3); §3 Task 6; §4.1-4.4 Task 9; §4.5 Task 8 (script, stylesheet, gates, vendoring, gallery copies, upgrade leg); §4.6 Task 11; §4.7 Task 10; §4.8 Task 9 (and the card, Task 7's CSS extended); §4.9 Task 9; §5 Task 7; §6 Tasks 12-13; §7 Tasks 10 (doctor) and 13 (changelog); §8 twins (every task), budgets (Tasks 7, 8, 14), CSP (Tasks 6, 11), floor (no change); §8.1's gate matrix: each row is a step in the task that touches it; §9 every named test rewritten in Task 7 or 9; §10.1 Tasks 2-3; §10.2 Task 4; §10.3 Tasks 8 (scripted), 9 (scriptless, wide, compat, axe, reflow), 11 (the prefetched index), 14 (the activated prerender, by hand); §10.4 Tasks 7 and 9; §10.5 Task 6; §10.6 Tasks 8 and 10; §10.6a Task 11; §10.7 Tasks 7 and 8; §10.8 Task 14; §11 Tasks 1, 5, 12.

**Deliberate departures, each argued where it is made.** Touch through a launch flag (Global Constraints); the prerender-activation leg by hand (Global Constraints, Task 11); the index/back rules scoped rather than undone (decisions list); a mid-plan copy batch (decisions list); SKILL.md gaining "a GET never changes anything", which the spec assumed was already there.

**Placeholders.** The only text a task cannot contain is the operator's approved wording, which the task reads from `copy-review/batch*-result.json` by id; every such place shows the draft that went to review.

**Names used across tasks.** `sizingFixture`, `sizingDoc`, `sizingMux`, `sizingRig`, `requirePointer`, `settleUntil`, `measureFn`, `targetsJS`, `readTargets`, `assertTargets` (Tasks 2-3); `at`, `probe`, `clickAndLand` (Task 4); `rowMenuItems`, `rowMenuItem`, `normaliseMarkup` (Task 6); `topbarPage`, `cardRig`, `readCard`, `menuAround`, `TAIL` (Task 7); `ShellJS`, `ShellCSS`, `assertReducedMotion`, `assertTwins`, `assertNoOrphans`, `tab`, `visit`, `follow`, `state`, `scaffoldWithReplace`, `goTestIn` (Task 8); `shellLayoutPage`, `shellAssets`, `legacyLayout`, `demoPageHref`, `shellPageHref` (Task 9); `twoPageShells`, `scaffoldOverview`, `oldShellLayout`, `doctorLayoutAdvisory` (Task 10); `SpeculationRulesPath`, `NoSpeculationRules`, `serveSpeculationRules` (Task 11).
