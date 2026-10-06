# Gallery usability (Part B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The generated design-system gallery gets a Code tab worth copying from (the `{{template}}` call first, formatted and highlighted HTML under a disclosure, a copy button). It also becomes a site you can find things in: real page titles, the sidebar shell's own phone index and back control, a pinned desktop bar that keeps your place, synonym search, a Buttons entry, one view choice for the whole page, compact rows, Mobile previews at their real size, previews that pan correctly right to left, and previews served as files so all of it fits inside 128 KiB per page.

**Architecture:** All of it is in `internal/designsystem` (the renderer, `gallery.js`, `gallery.css` and their gates), plus a new pure package `internal/codeview` that formats, highlights and writes template calls without knowing anything about the gallery. The one framework change is a `button` sample in `ui.Styleguide`. Preview documents move out of `srcdoc` into files under `<theme>/<locale>/<page>/<group>.html`, and that move pays for the rest. The frame becomes the shipped sidebar layout's markup, so `ui/shell.js` and `ui/shell.css` give the phone index, the back control and the slide with no gallery script.

**Tech Stack:** Go 1.26 (`GOTOOLCHAIN` pinned by the Makefile), `html/template` and `text/template`, hand-written CSS, a dependency-free ES5-style IIFE (`gallery.js`), chromedp through `harness` (`-tags browser`), and axe-core for accessibility.

**Spec:** `docs/superpowers/specs/2026-09-27-gallery-usability-design.md` (approved by the operator 2026-10-04). Read it beside this plan. The plan argues from it, and the spec's sections are cited here as "spec 1.1" and so on. Those citations live only in this plan: **never** write a spec section, the plan, or a review round into a code comment.

## Global Constraints

- **Where commands run:** from the worktree root `/home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/gallery-usability`, branch `gallery-usability`. Every `git`, `go`, `make`, `node` and `jq` command runs with the Bash sandbox disabled (`dangerouslyDisableSandbox: true`). Sandboxed git sees phantom dotfiles, and `go run` tooling needs the real module cache. Set `GOFLAGS=-mod=mod` on every `go` command, because three scratch-module packages fail on go.sum without it. Use the default Go build cache, and never set `GOCACHE`, `GOMODCACHE` or `GOTMPDIR` under `/tmp`. Scratch files go under `$TMPDIR` (on disk), with logs kept in `$TMPDIR/gate/` until the task's report is written. Delete them when the task ends.
- **Every command whose output is piped runs under `set -o pipefail`**, keeps its full log with `tee`, and has its exit status checked before anything read from the log is believed. A timing or a `grep` match from a run that failed is not a measurement. Where a step shows a pipeline, it is written that way. Where it does not pipe, the exit status is the command's own.
- **The task gate.** Every task ends green, in this order, and is not pushed until it is:
  1. `GOFLAGS=-mod=mod go vet ./... && make gofmt && RASTRILLO_TEST_REQUIRE_NODE=1 GOFLAGS=-mod=mod go test ./... -count=1`. This is the operator's line, `GOFLAGS=-mod=mod go vet ./... && gofmt -l . && GOFLAGS=-mod=mod go test ./...`, with `make gofmt` standing in for `gofmt -l .`: `gofmt -l` exits 0 even when it prints, while `make gofmt` fails on any output and skips `.build/tmp`.
  2. `make ci`, which AGENTS.md requires before every push, run and timed like this (replace `N` with the task number):

     ```bash
     set -o pipefail; mkdir -p "$TMPDIR/gate"; start=$(date +%s)
     RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp make ci 2>&1 | tee "$TMPDIR/gate/ci-task-N.log"; status=$?
     echo "make ci exit $status after $(( $(date +%s) - start ))s"
     grep -E '^(ok|FAIL)[[:space:]]' "$TMPDIR/gate/ci-task-N.log"
     ```

     `make ci` covers `budget` (the per-directory line budget), `root`, `staticcheck`, the examples, `race`, and `browser`. The browser list is `./harness/ ./webauthn/ ./ui/ ./pow/ ./internal/designsystem/ ./auth/`. Task 2 adds `./internal/designsystem/galleryrig/` and Task 11 adds `./internal/designsystem/sweep/`. The gate passes only when `status` is 0.
  3. **The time bound, checked on every task, not only at the end.** From the `ok` lines, put three numbers in the task's report: the seconds for `internal/designsystem`, for `internal/designsystem/sweep`, and the whole `make ci` wall clock. The spec's bounds are a third of each package's 20-minute limit left free, so **at most 800 s per package**, and **all of `make ci` under 20 minutes** on a quiet runner (load under 1 per core). A task that crosses either bound stops and reports to the controller with the three numbers before it pushes. A later task cannot win the time back. Task 19 is the final acceptance check.

  While you iterate inside a task, the focused commands each step gives are enough. The gate marks the end of the task. Run `gofmt -w` on every Go file you write. Code blocks here are not guaranteed to be column-aligned.
- **Browser tests:** build tag `//go:build browser`. Run them with `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser …`. Never set `RASTRILLO_BROWSER_OPTIONAL`, because a skip is not a pass. **Every `chromedp.Run` error fails the test** (`t.Fatalf`, or `t.Errorf` and `return` inside a helper). No error is logged and ignored. The one helper that retries is `galleryrig.Until` (Task 6). It polls across a document swap, where an evaluation can land on a page that is going away, and it fails at its deadline naming the last error. A touch leg uses `harness.WithCoarsePointer()` and first asserts `matchMedia("(pointer: coarse)").matches` (`galleryrig.RequireCoarse`, Task 12). `ui`'s `requirePointer` is a test helper of another package and cannot be imported.
- **The browser packages.** The spec's time bound gives each browser package 20 minutes with a third of it free. The design-system package already takes 7 to 8 of them, so the new heavy sweeps live in a package of their own from the start:
  - `internal/designsystem/galleryrig` (Task 2): a browser-tagged, non-test package of the drive helpers both packages share. That covers the tree handler, waiting for frames, polling, a scriptless tab, a touch rig, anchor positioning off, measuring frame heights, and the height-coverage table. Its files carry `//go:build browser`, like `harness`, so `chromedp` stays out of the ordinary build graph (`make chromedp-graph`).
  - `internal/designsystem/sweep` (Task 11): browser tests only. Its sweeps run over many themes, languages and widths with no axe: the pinned bar's fit and its language menu (Task 11), every language from the phone index (Task 12), position across a switch (Task 15), and the narrowest-frame heights (Task 18).
  - The hooks `internal/designsystem` exports for them, in `internal/designsystem/hooks.go` and nothing more: `PageKinds() []string`, `PageFile(kind string) string`, `PageHref(mount, theme, locale, file string) string` (Task 11) and `ExampleCounts() map[string]int` (Task 18). Everything else comes from `Render`, `DefaultMount` and `RootTheme`, which are already exported.
  - The axe legs stay in `internal/designsystem`, beside the axe tables they share (`axeTags`, `axeExempt`). In each package, a `rig_browser_test.go` adapts the shared helpers to the short names the tests are written with (`eagerly`, `until`, `phoneRig`, …): one-line wrappers, no logic.
- **A page restored from the back/forward cache fires no load event.** After any history traversal, a drive polls with `chromedp.Evaluate` and clicks with `el.click()`. It never uses `chromedp.Click`, `WaitVisible` or `NavigateBack` across the traversal (`ui/browser_test.go:2343-2375` documents this).
- **Node:** `RASTRILLO_TEST_REQUIRE_NODE=1` (the `make ci` default) turns a missing Node into a failure.
- **Copy.** Every new user-facing English string is the operator's approved text, read by id from two gitignored result files of the form `{"action":"approve","strings":[{"id":…,"text":…}]}`:
  - `copy-review/batch-b1-result.json`: the 17 gallery strings, all approved unchanged on 2026-10-04.
  - `copy-review/batch-b2-result.json`: five state labels that carried em dashes (`state.idle`, `state.working`, `state.twelve_plain`, `state.plain`, `state.anything_else`) and the button sample's labels (`button_sample.cancel`, `button_sample.save`, `button_sample.preview`, `button_sample.delete`, `button_sample.view_orders`, `button_sample.continue`, `button_sample.archived`). Read first by Task 8.
  - `copy-review/batch-b3-result.json`: the five error-status labels (`state.status_404`, `state.status_403`, `state.status_422`, `state.status_500`, `state.status_503`), whose review opens once B2 is submitted. Read only by Task 9.

  A task that needs a result file that does not exist yet, or an id a file lacks, stops and reports to the controller.

  Copy never reaches a file from this plan's text. It reaches the file through **`internal/copyedit`**, committed and tested in Task 4 and run as `go run ./internal/copyedit/run.go -edit <edit.json>`. An edit file names the result files to trust and does three things:
  - `add`: new prose keys, each with the English its translations were drafted from. The tool refuses to write unless that English is exactly the approved text for the id.
  - `remove`: retired keys.
  - `fill`: source files whose `⟦id⟧` markers it replaces with the approved text.
  Each task gives its edit file in full. No task copies code from another. Production source names copy only by its `⟦id⟧` marker: a template's `{{P "⟦gallery.copy.done⟧"}}`, a sample's `Note: "⟦gallery.note.tone⟧"`. The tool fills the marker, and the English never passes through this plan's text into a source file. Tests may name the approved English, because an assertion that disagrees with the approved text should fail loudly.

  The ids map to the spec's copy table like this:

  | Spec | Id | Spec | Id |
  |---|---|---|---|
  | C1 | `gallery.intro.live` | C10 | `gallery.note.tone` |
  | C2 | `gallery.intro.code` | C11 | `gallery.button.blurb` |
  | C3 | `gallery.code.rendered` | C12 | `gallery.start.without` |
  | C4 | `gallery.code.wrapper` | C13 | `gallery.start.link_lead` |
  | C5 | `gallery.copy.button` | C14 | `gallery.locale_menu.note` |
  | C6 | `gallery.copy.done` | C15 | `gallery.title` (" · " separators) |
  | C7 | `gallery.copy.failed` | C16 | `gallery.frame.title` (", " before the state) |
  | C8 | `gallery.view.label` | C17 | `gallery.copy.name` (", " joiner) |
  | C9 | `gallery.view.auto` | | |

  `changelog.scroll_padding.*` belong to the `back-strip-scroll-padding` branch. Nothing here writes them.
  In `internal/designsystem` the English is also the prose key. Each new key gets eleven translations in `prose.go`. These are best-effort machine drafts, written by the task that adds the key, not reviewed, and labelled as machine-drafted in the branch description (Task 19). **No em dash (U+2014) in any new user-facing string or translation.** `copyedit` refuses one. C15 to C17 are separators, not keys: they are Go constants and are not translated.
- **Comments** say why a thing is the way it is and name the failure it prevents. A comment that restates the code is not written. Comments never mention this plan, the spec's section numbers or review rounds. **Commits:** imperative subject, and a body that says why (the failure prevented, the alternative rejected), ending in a blank line and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit each task, staging its files by name and checking the staged list against the task's Files list first (each commit step shows how), then `git push origin gallery-usability`. The amadan remote sometimes answers 503, so retry. The branch lands later through `amadan branch merge -squash`, run by the controller, never by an implementer.
- **The separate branch.** `back-strip-scroll-padding` gives the framework's phone back strip its `scroll-padding`, and it lands on its own. This branch neither sets nor depends on it. Do not edit `ui/tokens.css`. The gallery's own `scroll-padding` exists only inside `@media (min-width: 800px)`. No test here asserts where a fragment lands below 800px under the back strip.
- **SKILL.md** is not touched.
- **Budgets:**
  - Every page is at most 131,072 bytes and `pageBudgetDebt` ends empty (Task 1).
  - **`gallery.js` is at most 22,832 bytes.** That is the whole script this plan writes, measured, plus 10%. The supplied code was weighed by applying every task's edits to today's file in order:

    | After | Bytes | Added |
    |---|---|---|
    | today | 9,553 | |
    | Task 6 (copy buttons) | 12,508 | 2,955 |
    | Task 13 (search terms) | 12,975 | 467 |
    | Task 14 (the page-wide view, and shared storage helpers) | 15,555 | 2,580 |
    | Task 15 (keeping your place, the scheme after a cache restore) | 20,756 | 5,201 |

    The spec's 15 KiB came from estimates that undercounted the code and its comments. The code was slimmed first: one storage pair shared by the scheme and the view, no second copy of a helper, `Promise` doing the copy's try/catch, and comments cut where they repeated each other. Nothing in `shell.js` or `rastrillo.js` is reusable, because both are closed IIFEs that expose nothing. The why-comments that remain are load-bearing, so the cap rises instead. The script is the docs site's own and is never shipped to an app; it loads once, blocking in `<head>`, and is cached from then on. Task 6 sets the cap with this reasoning, and each script task checks its own total against the table. A task that lands more than 200 bytes over its row has added something the table did not weigh, and says so in its report.
  - Directory line budgets live in `.rastrillo/budgets.txt`. Task 1 raises `internal/designsystem` to 12000 source and 13000 test. If a later task's `make budget` reports a directory over, raise the crossed column in that task to the measured count rounded up to the next 500 and keep the reason text. The ratchet fails any ceiling more than 1/0.6 of the count, so never raise further than that. `galleryrig`, `sweep`, `codeview` and `copyedit` are new directories inside the default budget.
- **No inline scripts, no inline handlers, no `Cache-Control: no-store`** anywhere in the tree. The tree handler in the drives sends none either.
- **Determinism:** `TestRenderIsDeterministic` holds every render to identical bytes, so anything ranged over a map is sorted first.

## Review Focus

These are the inputs the spec implies that no spec test names, most likely first. Each one has its test in the task that owns the code.

1. **A sample string that is awkward as a template literal**: a quote, a backslash, a newline, `{{`, or non-ASCII such as `−`, `…` and `✕`. A reasonable reader expects the call to parse and render the same bytes. *Test: Task 4, `TestCallQuotesAwkwardStrings`.*
2. **Source holding characters that are markup**: `&`, an entity such as `&euro;`, `<` in text, `>` inside a quoted attribute value, `{{` inside an attribute, upper-case tags, a comment. A reader expects to see, and to copy, exactly that source. *Test: Task 3, `TestHighlightRoundTripsAwkwardMarkup`.*
3. **Rendered HTML holding a `<pre>` or `<textarea>` whose content looks like tags, or an attribute value with a `>` in it.** A reader expects the formatter to leave both alone, byte for byte. *Test: Task 3, `TestFormatLeavesRawZonesAndQuotedBrackets`.*
4. **A browser whose storage throws** (private mode, site data refused). A reader expects the page-wide view buttons to apply the choice to this page anyway, with no exception, and only persistence lost. *Test: Task 14, `TestThePageWideViewWorksWhenStorageThrows`.*
5. **A fragment that is not a gallery anchor** (`#main` from the skip link, `#ds-prefs`, or a typo such as `#nope`) when a reader switches theme. A reader expects to land where they were reading, never on a fragment that names nothing. *Test: Task 15, the "non-anchor fragment" leg of `TestPositionSurvivesASwitch`.*

## Where the code forced a choice the spec did not make

Each choice is decided below. A reviewer who disagrees should say so before the task that owns it.

- **A new package, `internal/codeview`**, holds the formatter, the highlighter and the call writer. `internal/designsystem` has 571 lines of source budget left, and these three pieces know nothing about the gallery. `callFor`, which reads the partials' `Keys:` blocks and the samples, stays in `internal/designsystem` as the spec says.
- **Copy is written by a committed tool, `internal/copyedit`** (Task 4), rather than by a script each task carries. The library holds the rules (approved text only, no em dash, every locale, the same placeholders) and is unit-tested. `run.go` is a `//go:build ignore` main that applies an edit file, so it adds no binary to the module.
- **The `{wrapper}` in "Put this inside {wrapper}."** is the wrapper's opening markup as one `<code>` element: `<section rst-box><form rst-form>`, `<div rst-list>`, `<section rst-box>` or `<div rst-stats>`. The spec's illustrative "in an `rst-box`" would put untranslated English inside a translated sentence.
- **Separators C15 to C17 are Go constants in `page.go`** (`titleSep`, `frameSep`, `copyJoin`), each checked against the approved text by the step that writes it (a test cannot read the gitignored result file in CI). The copy joiner reaches `gallery.js` as `data-join` on the live region, beside `data-copy`, `data-copied` and `data-failed`, so the script still says nothing of its own. Titles without a theme (modal, shell demos, demo application) use two parts joined by the same separator.
- **The state labels with em dashes inside them** are replaced in Task 9 with approved batch-B2 text, and their translations are re-keyed and redrafted. That covers the five error-status labels as well ("404 — not found" and so on), which go into frame titles just the same. Their approved text comes from batch B3.
- **The copy button goes immediately before its `<pre>`**, as a sibling right-aligned by `gallery.css`, not inside the `<pre>`. Inside, it would scroll away with a wide block and sit inside the element whose text is copied.
- **The page-wide view group sits after the page header, before the body**, on the pages that have at least one Code tab.
- **A preview file is recognised by its `<meta name="robots" content="noindex">`.** Only preview documents carry it. A shell demo under `shells/` has the same path shape as a preview file, so the path alone cannot tell them apart.
- **The test tree handler records requests for missing files and fails in `t.Cleanup`.** A `t.Errorf` from the server goroutine after the test returns would panic.
- **`srcdoc()` is renamed `previewDoc()`**, since nothing is a `srcdoc` any more. The signin-matrix page in `a11y_test.go` calls it by the new name.
- **The scheme journey and every other drive that clicks a switcher are scoped to `.ds-top`.** On the Overview the controls are written twice, and the first in document order is the hidden foot copy.
- **Highlight colours:** tag `--rst-accent`, attribute name `--rst-tone-negative-fg`, attribute value `--rst-tone-positive-fg`, action `--rst-text-muted`, string `--rst-tone-warning-fg`. All of them were measured against `--rst-surface` in all three themes and both schemes before this plan was written. The lowest is 5.28:1 (`day` accent, light).
- **The live region is on every gallery page**, because the Icons and Getting started snippets get copy buttons too, and a page is cheaper to reason about than a list of pages.
- **The package split is planned now** (Global Constraints, "The browser packages"), not held back as a fallback. The axe legs stay with the axe tables in `internal/designsystem`. Every sweep with no axe in it moves out.
- **The locale subset for the narrow height test** is found in Task 18 by a measured sweep and written into the test with the measurement beside it. The full twelve run under a new `make browser-sweep` with a deadline of their own.

---

## File Structure

Created:

| File | Responsibility |
|---|---|
| `internal/codeview/pieces.go` | Splitting markup into tags, comments and text, honouring quoted attribute values. |
| `internal/codeview/format.go` | `Format`: line breaks between block-level tags only. |
| `internal/codeview/highlight.go` | `Highlight`: escaping plus `<ds-t>`, `<ds-a>`, `<ds-v>`, `<ds-x>`, `<ds-s>`. |
| `internal/codeview/call.go` | `Call`: a `{{template}}` action from a partial name and its data. |
| `internal/codeview/*_test.go` | Their unit tests. |
| `internal/copyedit/copyedit.go`, `copyedit_test.go` | Applying approved copy: prose keys added and removed, `⟦id⟧` markers filled. |
| `internal/copyedit/run.go` | The `//go:build ignore` command each copy task runs. |
| `internal/designsystem/calls.go` | `callFor`, `call`, the `Keys:` block reader. |
| `internal/designsystem/calls_test.go` | The call contract, in every locale. |
| `internal/designsystem/hooks.go` | `PageKinds`, `PageFile`, `PageHref`, `ExampleCounts`: what the sweep package reads. |
| `internal/designsystem/galleryrig/*.go` | The shared drive helpers (browser-tagged), and `tree_test.go` for the handler's own test. |
| `internal/designsystem/rig_browser_test.go` | The design-system package's short names for the shared helpers. |
| `internal/designsystem/code_browser_test.go` | Copy, page-wide view, Code-selected axe, sideways scroll at 390px. |
| `internal/designsystem/bar_browser_test.go` | The pinned bar's painting, focus clearance and right-to-left placement. |
| `internal/designsystem/index_browser_test.go` | The phone index and the way back, search on it, axe on both views. |
| `internal/designsystem/cache_browser_test.go` | The scheme after a back/forward cache restore. |
| `internal/designsystem/realsize_browser_test.go` | Mobile at its real size, and the right-to-left pan. |
| `internal/designsystem/sweep/rig_test.go` | The sweep package's short names for the shared helpers and the hooks. |
| `internal/designsystem/sweep/bar_test.go` | The pinned bar's fit in every theme and locale, and its language menu. |
| `internal/designsystem/sweep/languages_test.go` | Every language reachable from the phone index. |
| `internal/designsystem/sweep/place_test.go` | Keeping your place across a switch. |
| `internal/designsystem/sweep/narrow_test.go` | `TestPreviewFrameHeightsFitAtThePhonesNarrowest`. |

Modified: `internal/designsystem/{designsystem.go,page.go,samples.go,screens.go,formats.go,prose.go,gallery.js,gallery.css}`, `internal/designsystem/{designsystem_test.go,a11y_test.go,browser_test.go,header_rule_test.go,header_rule_browser_test.go,flipevidence_test.go}`, `ui/styleguide.go`, `docs/site/reference/ui.md` (the identifier list), `.rastrillo/budgets.txt`, `Makefile` (`browser`, `browser-sweep`), `docs/superpowers/specs/2026-09-27-gallery-usability-design.md` (the `gallery.js` budget, in this plan's own commit).

---
### Task 1: Previews become files, and the gallery stops loading three scripts

Spec 1.5, the migrated Go gates, and the single-sample preview contract. This task frees the bytes that every later task spends. Form is 225 bytes over the cap on a debt entry today, and loses about 47 KB here.

**Files:**
- Modify: `internal/designsystem/page.go`: `previewView` (around line 1121), `srcdoc` (1411) renamed `previewDoc`, `newPreview` (1525), `buildFamilies` (957), `buildIdioms` (1634), `renderGallery` (628), `viewTemplate` (the `<iframe>` line), `pageTemplate` (the three `<script defer>` lines for select/calendar/datetime), the `srcdocScripts` comment
- Modify: `internal/designsystem/screens.go:230-274` (`buildScreens`), `internal/designsystem/formats.go:181-197` (`buildFormats`)
- Modify: `internal/designsystem/designsystem.go:115-170` (`Render`: `put`)
- Modify: `.rastrillo/budgets.txt` (the `internal/designsystem` line)
- Test: `internal/designsystem/designsystem_test.go`, `internal/designsystem/header_rule_test.go`, `internal/designsystem/a11y_test.go` (line 1283, and the settle at 791-803), `internal/designsystem/flipevidence_test.go:253`, `internal/designsystem/header_rule_browser_test.go:71-80`

**Interfaces:**
- Consumes: nothing new.
- Produces (later tasks rely on these exact names):
  - `type previewFile struct{ Path, Doc string }`, `type previewSet map[string]string`, `func (s previewSet) add(f previewFile) error`
  - `func newPreview(mount, theme, locale, page, group, title, source, id string) (previewView, previewFile)`. `page` is the page's file stem (`fam.Key`, `"primitives"`, `"screens"`, `"formats"`).
  - `func previewDoc(mount, theme, locale, title, body string) string`
  - `previewView` loses `Doc`. `Src` is always set.
  - `buildFamilies(mount, tmpl, theme, locale string, files previewSet)`, `buildIdioms(…, files previewSet)`, `buildScreens(mount, theme, locale string, tmpl *template.Template, files previewSet)`, `buildFormats(mount, theme, locale string, files previewSet) ([]formatView, error)`
  - `func put(out map[string][]byte, name string, body []byte) error` in `designsystem.go`
  - tests: `const previewMarker`, `func isPreviewDoc(doc string) bool`, `type frameDoc struct{ Src, Name, Doc string }`, `var frameSrc`, `func frameDocs(t *testing.T, files map[string][]byte, page string) []frameDoc`, `func sampleTree(t *testing.T, locale string) *template.Template`, `func pageKindOf(name string) string`, `func previewBody(doc string) string`

- [ ] **Step 1: Raise the directory budget the branch will need**

In `.rastrillo/budgets.txt`, replace the `internal/designsystem` line with:

```
size internal/designsystem 12000 13000 the design-system site generator and its checks: one sample per ui component, the Code tab's calls and the drives that hold the phone index, the pinned bar and the copy button; split by section owed
```

Run: `GOFLAGS=-mod=mod go run ./cmd/rastrillo budget size`
Expected: `budget size: 58 directories within budget`. The ratchet allows it: 9,929 × 10 ≥ 12,000 × 6, and 8,016 × 10 ≥ 13,000 × 6.

- [ ] **Step 2: Write the failing tests**

In `internal/designsystem/designsystem_test.go`, replace `srcdocAttr`'s comment and the `srcdocs` function (lines 529-549) with:

```go
// srcdocAttr finds a frame that still carries its document inline. No
// frame in the tree may: the previews are files, and an escaped second
// copy of every sample inside a page is the weight that put Form over
// its budget. TestEveryPreviewIsAFileOneFrameNames holds the absence.
var srcdocAttr = regexp.MustCompile(`<iframe[^>]*\ssrcdoc="`)

// previewMarker is the line only a preview document carries. A shell
// demo under shells/ has the same path shape as a preview file, so the
// path cannot tell the two apart and this line can.
const previewMarker = `<meta name="robots" content="noindex">`

func isPreviewDoc(doc string) bool { return strings.Contains(doc, previewMarker) }

// frameDoc is one preview widget's frame and the document it shows.
type frameDoc struct {
	Src  string // the src attribute, as written
	Name string // the tree path it names
	Doc  string
}

// frameSrc finds every preview widget's frame. The class is the marker,
// so a frame a sample draws inside its own document is not one.
var frameSrc = regexp.MustCompile(`<iframe class="ds-view__frame"[^>]*\ssrc="([^"]*)"`)

// frameDocs returns every .ds-view__frame on a page with the document
// it shows, read from the tree by src. A frame naming nothing fails
// here, so a gate reading previews through this cannot pass on a frame
// a reader would see as a 404.
func frameDocs(t *testing.T, files map[string][]byte, page string) []frameDoc {
	t.Helper()
	var out []frameDoc
	for _, m := range frameSrc.FindAllStringSubmatch(page, -1) {
		name := strings.TrimPrefix(m[1], mountPrefix)
		body, ok := files[name]
		if !ok {
			t.Errorf("a preview frame names %s, which is not in the tree", m[1])
			continue
		}
		out = append(out, frameDoc{Src: m[1], Name: name, Doc: string(body)})
	}
	return out
}

// previewBody is a preview document's body: what the frame shows,
// without the head that loads its stylesheets and scripts.
func previewBody(doc string) string {
	_, after, _ := strings.Cut(doc, "<body>\n")
	body, _, _ := strings.Cut(after, "\n</body>")
	return body
}

// sampleTree is the tree renderSample executes in for one locale: ui's
// partials with the gallery's funcs, and the hand-written samples.
func sampleTree(t *testing.T, locale string) *template.Template {
	t.Helper()
	tmpl, err := partialTree(locale)
	if err != nil {
		t.Fatalf("parsing partials for %s: %v", locale, err)
	}
	if err := parseRawSamples(tmpl); err != nil {
		t.Fatalf("parsing the hand-written samples for %s: %v", locale, err)
	}
	return tmpl
}

// pageKindOf maps a tree path to the gallery page it belongs to: the
// page itself, or a file in the directory named after it. A path
// belonging to no page kind (a demo, the modal) maps to "".
func pageKindOf(name string) string {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 3 {
		return ""
	}
	for _, pk := range pageKinds() {
		if parts[2] == pk.File || strings.HasPrefix(parts[2], strings.TrimSuffix(pk.File, ".html")+"/") {
			return pk.Kind
		}
	}
	return ""
}

// After the move no frame anywhere in the tree carries srcdoc, every
// frame's src names a file, and every preview file is named by exactly
// one frame. An orphan is a document nothing shows, written 36 times,
// and a file two frames share is two examples showing one document.
func TestEveryPreviewIsAFileOneFrameNames(t *testing.T) {
	files := render(t)
	named := map[string]int{}
	var frames, previews int
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		page := string(body)
		if srcdocAttr.MatchString(page) {
			t.Errorf("%s still carries a frame with srcdoc", name)
		}
		for _, fd := range frameDocs(t, files, page) {
			frames++
			if isPreviewDoc(fd.Doc) {
				named[fd.Name]++
			}
		}
	}
	for name, body := range files {
		if !isPreviewDoc(string(body)) {
			continue
		}
		previews++
		if n := named[name]; n != 1 {
			t.Errorf("%s is named by %d frames, want exactly 1", name, n)
		}
	}
	if frames == 0 || previews == 0 {
		t.Fatalf("%d frames and %d preview files in the tree; this gate is checking nothing", frames, previews)
	}
	t.Logf("%d frames, %d preview files", frames, previews)
}

// The gallery pages load no enhancement script of their own. Every
// control select.js, calendar.js or datetime.js boots on is inside a
// preview document, which loads what it needs; the page loading them
// as well cost 150,823 bytes a visit for nothing on it.
func TestNoGalleryPageLoadsTheEnhancementScripts(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, name := range galleryFiles(theme, locale) {
				for _, s := range []string{"select.js", "calendar.js", "datetime.js"} {
					if strings.Contains(string(files[name]), `src="`+mountPrefix+s+`"`) {
						t.Errorf("%s loads %s, which nothing on the page uses", name, s)
					}
				}
			}
		}
	}
}

// A preview document is the sample, wrapped and deadened, and nothing
// else: its body is exactly deaden(wrap(renderSample)) and then the
// sink, if and only if the sample holds a form. The Code tab's call is
// held to renderSample's bytes separately, so between the two gates
// the preview and the call cannot drift apart unseen.
func TestEveryPreviewIsItsSampleWrappedAndDeadened(t *testing.T) {
	files := render(t)
	const sink = "\n<iframe name=\"ds-void\" hidden></iframe>"
	var checked int
	for _, locale := range rastrillo.BaseLocales() {
		tmpl := sampleTree(t, locale)
		for _, fam := range families() {
			for _, doc := range fam.Partials {
				for i, s := range doc.States {
					html, err := renderSample(tmpl, doc.Name, i, s, locale)
					if err != nil {
						t.Fatalf("%s (%s): %v", doc.Name, s.State, err)
					}
					want := deaden(mountPath, wrap(doc.Wrap, string(html)))
					if strings.Contains(want, "<form") {
						want += sink
					}
					name := fmt.Sprintf("%s/%s/%s/%s-%d.html", RootTheme(), locale, fam.Key, anchorID("partial", doc.Name), i)
					file, ok := files[name]
					if !ok {
						t.Errorf("no preview file %s", name)
						continue
					}
					if got := previewBody(string(file)); got != want {
						t.Errorf("%s: the preview body is not the sample wrapped and deadened\n got: %.300s\nwant: %.300s", name, got, want)
					}
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no preview checked")
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestEveryPreviewIsAFileOneFrameNames|TestNoGalleryPageLoadsTheEnhancementScripts|TestEveryPreviewIsItsSampleWrappedAndDeadened' -count=1 ./internal/designsystem/`
Expected: FAIL. The file does not compile until the old `srcdocs` callers are migrated, so first make Step 5's test edits. Then expect `…/form.html still carries a frame with srcdoc`, `… loads select.js, which nothing on the page uses`, and `no preview file day/en/list-screen/partial-page-header-0.html`.

- [ ] **Step 4: Render the previews as files**

In `page.go`, replace the `previewView` doc comment and struct (from `// previewView is one example's widget.` through the closing brace) with:

```go
// previewView is one example's widget. Src is the document the frame
// loads: a preview file written for it (newPreview), or a page of this
// tree that already exists (the shell demos, the demo application).
// Source empty means no Code tab: only those framed pages, whose source
// is a Go template or a whole application rather than markup to copy.
type previewView struct {
	Group string       // the radio group's name, unique on the page
	Style template.CSS // --ds-h and --ds-hm: the frame's virtual height
	// Class is " ds-view--page" on the examples whose document is a
	// whole page, and empty on the rest; see pageFrame. It carries its
	// own leading space so the template can write class="ds-view{{.Class}}"
	// without a conditional.
	Class  string
	Src    string
	Source string
	Title  string
}

// previewFile is one example's document as a file of the tree: its path
// under the theme × locale directory, and the document.
type previewFile struct {
	Path string
	Doc  string
}

// previewSet collects one directory's preview files. A path produced
// twice is a build error rather than an overwrite: two examples sharing
// a radio-group name would show one document in both frames, and every
// other gate would still pass.
type previewSet map[string]string

func (s previewSet) add(f previewFile) error {
	if _, dup := s[f.Path]; dup {
		return fmt.Errorf("preview %s is produced twice", f.Path)
	}
	s[f.Path] = f.Doc
	return nil
}
```

Rename `func srcdoc(` to `func previewDoc(`. Replace the first two sentences of its doc comment with `// previewDoc builds the document one example is previewed in, which is written to a file of its own:`. After the viewport line, add:

```go
	// A preview is a fragment, not a page: thousands of them sit under
	// the gallery's directories, and a search engine listing them would
	// send a reader to a lone field with nothing around it.
	b.WriteString(`<meta name="robots" content="noindex">` + "\n")
```

In the `srcdocScripts` comment, replace the sentences from `datetime.js scans on` through `the control this overlay exists to replace, restored by a script tag in the wrong order.` with:

```
	// datetime.js scans the moment it runs, and loaded before
	// calendar.js it finds no factory: every field in the frame
	// enhances without a calendar, and the button falls back to the
	// browser's own picker, the control this overlay exists to
	// replace.
```

Replace `newPreview` and its comment with:

```go
// newPreview is one example's widget and the file its frame loads: the
// source as written for the Code tab, and a document holding the same
// markup with its links deadened. page is the page's file stem, so the
// file sits in a directory named after the page that frames it.
//
// id is the example's anchor id. Both the height and the width class
// are read off it, because the two tables that size a preview are keyed
// the same way, so a caller cannot pass one example's id and another's
// measurements.
func newPreview(mount, theme, locale, page, group, title, source, id string) (previewView, previewFile) {
	file := page + "/" + group + ".html"
	return previewView{
			Group:  group,
			Style:  previewStyle(id, heightOf(id)),
			Class:  previewClass(id),
			Src:    pageHref(mount, theme, locale, file),
			Source: source,
			Title:  title,
		}, previewFile{
			Path: file,
			Doc:  previewDoc(mount, theme, locale, title, deaden(mount, source)),
		}
}
```

In `buildFamilies`, change the signature to `func buildFamilies(mount string, tmpl *template.Template, theme, locale string, files previewSet) ([]familyView, error)` and replace the `pv.States = append(...)` block with:

```go
				preview, file := newPreview(mount, theme, locale, fam.Key,
					fmt.Sprintf("%s-%d", pv.ID, i),
					previewTitle(locale, doc.Name, s.State),
					wrap(doc.Wrap, string(html)), pv.ID)
				if err := files.add(file); err != nil {
					return nil, err
				}
				pv.States = append(pv.States, stateView{
					State:   proseIn(locale, s.State),
					Note:    proseIn(locale, s.Note),
					Preview: preview,
				})
```

In `buildIdioms`, change the signature to `func buildIdioms(mount string, tmpl *template.Template, theme, locale string, files previewSet) ([]idiomView, error)` and replace the `view.Preview = newPreview(...)` statement with:

```go
		preview, file := newPreview(mount, theme, locale, "primitives", view.ID+"-0",
			previewTitle(locale, name, "UI primitives"),
			samples[name], view.ID)
		if err := files.add(file); err != nil {
			return nil, err
		}
		view.Preview = preview
```

In `screens.go`, change `buildScreens` to `func buildScreens(mount, theme, locale string, tmpl *template.Template, files previewSet) ([]screenView, error)`, and replace its two `view.Preview = newPreview(...)` statements with:

```go
		if doc.Signin == nil {
			preview, file := newPreview(mount, theme, locale, "screens", id+"-0", title, doc.Markup, id)
			if err := files.add(file); err != nil {
				return nil, err
			}
			view.Preview = preview
			out = append(out, view)
			continue
		}
```

and

```go
		preview, file := newPreview(mount, theme, locale, "screens", id+"-0", title, buf.String(), id)
		if err := files.add(file); err != nil {
			return nil, err
		}
		view.Preview = preview
		view.Preview.Source = signinSource
```

In `formats.go`, replace `buildFormats` with:

```go
// buildFormats renders every section for one theme and locale.
func buildFormats(mount, theme, locale string, files previewSet) ([]formatView, error) {
	docs := formatDocs()
	out := make([]formatView, 0, len(docs))
	for _, doc := range docs {
		id := anchorID("format", doc.Key)
		preview, file := newPreview(mount, theme, locale, "formats", id+"-0",
			previewTitle(locale, doc.Key, "Dates, numbers and names"), doc.Markup, id)
		if err := files.add(file); err != nil {
			return nil, err
		}
		out = append(out, formatView{
			Title:   proseIn(locale, doc.Title),
			ID:      id,
			Marker:  marker("format", doc.Key),
			Body:    proseIn(locale, doc.Body),
			Preview: preview,
		})
	}
	return out, nil
}
```

In `renderGallery`, after the `structuralGroups` call, declare `files := previewSet{}`. Pass `files` to `buildFamilies`, `buildIdioms` and `buildScreens`, and before `base := pageView{` add:

```go
	formats, err := buildFormats(mount, theme, locale, files)
	if err != nil {
		return nil, err
	}
```

In the `pageView` literal, use `Formats: formats,`. Replace `out := make(map[string][]byte, len(pageKinds()))` with `out := make(map[string][]byte, len(pageKinds())+len(files))`. Before the final `return out, nil`, add:

```go
	// The preview files share the directory with the pages. A preview
	// whose path a page also takes would replace that page silently.
	for path, doc := range files {
		if _, clash := out[path]; clash {
			return nil, fmt.Errorf("preview %s collides with a page", path)
		}
		out[path] = []byte(doc)
	}
```

Update `renderGallery`'s doc comment: replace `keyed by filename` with `keyed by path under the directory: the pages, and the preview files their frames load`.

In `viewTemplate`, replace the `<iframe …>` element with:

```
<iframe class="ds-view__frame" title="{{.Title}}" src="{{.Src}}" loading="lazy"></iframe>
```

In `pageTemplate`, delete the three lines `<script defer src="{{.Mount}}/select.js"></script>`, `…/calendar.js…` and `…/datetime.js…`. In the comment block that begins `// ── Preview widgets`, replace the bullet ending `(The outer page stays clean either way; html/template writes a srcdoc's quotes as &#34;, so nothing inside one is markup here.)` so that it ends at `…is no longer a duplicate id on this page.`

In `designsystem.go`, add after `CleanMount`:

```go
// put adds one file to the tree and refuses a second writer. The
// preview files share their directories with the pages and the shell
// demos, and a name one of them took from another would replace a
// document with no gate noticing.
func put(out map[string][]byte, name string, body []byte) error {
	if _, dup := out[name]; dup {
		return fmt.Errorf("designsystem: %s is produced twice", name)
	}
	out[name] = body
	return nil
}
```

In `Render`, replace each of the four assignments `out[dir+file] = page`, `out[dir+"modal.html"] = modal`, `out[dir+name] = doc` and `out[dir+"shells/"+name] = doc` with the `put` call returning its error, for example:

```go
			for file, page := range pages {
				if err := put(out, dir+file, page); err != nil {
					return nil, err
				}
			}
```

Add `//	<theme>/<locale>/<page>/<group>.html  one preview document per example` to the tree diagram in `Render`'s doc comment, under the `demo-request.html` line.

- [ ] **Step 5: Migrate the Go gates**

In `designsystem_test.go`:

(a) `TestEveryPageIsAWholeLocalisedDocument`: replace the loop body and the assertion after it with:

```go
	var framed int
	for _, name := range names {
		body := string(files[name])
		wholeDocument(t, files, name, localeOfPath(name), body)
		if isPreviewDoc(body) {
			framed++
		}
	}
	// Asserted rather than assumed: previews that stopped being written
	// as files, or lost the line that marks them, would leave this gate
	// checking the pages only and passing.
	if framed == 0 {
		t.Error("no preview documents in the tree at all; they are written as files, each one marked noindex")
	}
	t.Logf("%d documents, %d of them previews", len(names), framed)
```

and in its doc comment replace `A srcdoc is a page too` sentences with `The preview files are documents in the tree like any other, so the loop holds them to the same contract.`

(b) `TestTreeShapeIsComplete`: inside the theme × locale loop, after the shells, add:

```go
			// Every preview file a page's frames name: the shape of the
			// tree includes them, and the count below stays exact.
			for _, page := range galleryFiles(theme, locale) {
				for _, m := range frameSrc.FindAllStringSubmatch(string(files[page]), -1) {
					if name := strings.TrimPrefix(m[1], mountPrefix); isPreviewDoc(string(files[name])) {
						want = append(want, name)
					}
				}
			}
```

(c) `TestEnhancedControlsAreOnTheComponentPages`: replace the `frames = append(frames, srcdocs(...)...)` line with:

```go
		for _, fd := range frameDocs(t, files, galleryPage(t, files, RootTheme(), "en", pk.Kind)) {
			frames = append(frames, fd.Doc)
		}
```

(d) `TestSampleLinksAndFormsAreDeadInThePreviews`: replace the `docs` loop with:

```go
	for _, name := range galleryFiles(RootTheme(), "en") {
		for _, fd := range frameDocs(t, files, string(files[name])) {
			if isPreviewDoc(fd.Doc) {
				docs = append(docs, fd.Doc)
			}
		}
	}
```

(e) `TestNoPageCarriesTheSameIdTwice`: replace the loop and the check after it with:

```go
	var framed int
	for _, name := range names {
		page := string(files[name])
		// A preview with no id in it is an ordinary sample; a page of
		// this tree always carries main's.
		preview := isPreviewDoc(page)
		if preview {
			framed++
		}
		uniqueIDs(t, name, page, !preview)
	}
	if framed == 0 {
		t.Error("no preview documents found at all; half this gate is checking nothing")
	}
```

Update its doc comment: replace `So it walks both: the 181 files, and the 3,959 documents the frames carry.` with `So it walks every file, and the preview documents are files now.`

(f) `proseFixtureCollisions`: change the three values to page kinds (`"list-screen"`, `"primitives"`, `"primitives"`), and change the type comment's last paragraph to say the value is the page kind whose page or preview files may carry the key in English. In `TestNoEnglishProseReachesATranslatedPage`, replace the collision check with `if on, ok := proseFixtureCollisions[key]; ok && pageKindOf(name) == on {`. Replace the sweep-count assertion with:

```go
	// The preview files are documents in every language too: one per
	// preview frame on the English root theme's pages, in every theme
	// and translated locale.
	var perDir int
	for _, page := range galleryFiles(RootTheme(), "en") {
		for _, m := range frameSrc.FindAllStringSubmatch(string(files[page]), -1) {
			if isPreviewDoc(string(files[strings.TrimPrefix(m[1], mountPrefix)])) {
				perDir++
			}
		}
	}
	if want := len(ui.ThemeNames()) * (len(rastrillo.BaseLocales()) - 1) * (len(pageKinds()) + 2 + 3 + len(ui.LayoutNames()) + 2 + perDir); len(names) != want {
		t.Errorf("sweeping %d translated pages, want %d", len(names), want)
	}
```

(g) `TestTheSigninScreensAreThePartial`: replace the per-key section check

```go
		if !strings.Contains(section, "rst-signin") || !strings.Contains(section, "rst-stage") {
```

and the brand check under it with a read of the section's frame:

```go
		docs := frameDocs(t, files, section)
		if len(docs) != 1 {
			t.Errorf("screen %s frames %d documents, want 1", key, len(docs))
			continue
		}
		if !strings.Contains(docs[0].Doc, "rst-signin") || !strings.Contains(docs[0].Doc, "rst-stage") {
			t.Errorf("screen %s is not the signin partial in a stage frame", key)
		}
		// A brand column with a name alone is half a card of nothing;
		// the sample brand shows every part an app's can have.
		if !strings.Contains(docs[0].Doc, "rst-signin-mark") || !strings.Contains(docs[0].Doc, "rst-signin-pitch") {
			t.Errorf("screen %s shows a brand with no mark or pitch", key)
		}
```

and replace the counting block (`frames := regexp.MustCompile(...)` to the `t.Errorf("the Screens page frames …")`) with:

```go
	frames := frameDocs(t, files, page)
	stages := 0
	for _, f := range frames {
		if strings.Contains(f.Doc, "<div rst-stage>") {
			stages++
		}
	}
	if len(frames) != len(signinKeys)+2 || stages != len(signinKeys) {
		t.Errorf("the Screens page frames %d screens, %d of them stage frames; want %d, %d", len(frames), stages, len(signinKeys)+2, len(signinKeys))
	}
```

(h) `pageBudgetDebt`: delete the `"form.html": 132 << 10,` entry and its comment. The map literal becomes `var pageBudgetDebt = map[string]int{}`. The unused-entry check would otherwise fail, since Form drops far under the cap.

(i) The `maxPageBytes` comment's paragraph beginning `Each sample is written into the page twice:` describes the escaped srcdoc. Replace it with: `Each sample was once written into its page twice, as the escaped document its frame carried and as its source. The documents are files now, so a page carries the source and the widget around it: about 450 bytes of widget per example, and whatever the Code tab shows.`

In `header_rule_test.go`, replace the loop body of `TestEveryPageWithAHeaderLinksTheOneStylesheetThatRetiresTheRakeLine` (from `body := string(files[name])` to the end of the loop) with:

```go
		body := string(files[name])
		documents++
		if !strings.Contains(body, "rst-page-header") {
			continue
		}
		withHeader++
		// A preview is a separate document; the page that frames it
		// retires nothing on its behalf.
		if isPreviewDoc(body) {
			framesChecked++
		}
		if !strings.Contains(body, link) {
			t.Errorf("%s renders a page header and links no %s/tokens.css; whatever retires the rake line, it is not reaching this document", name, mountPath)
		}
```

and delete the comment above the old `srcdocs` loop.

In `a11y_test.go` line 1283, replace `srcdoc(` with `previewDoc(`.

- [ ] **Step 6: Make the three browser waits refuse about:blank**

A file-backed frame shows `about:blank`, complete, for a moment before its document arrives. These three waits checked the blank state only for `srcdoc` frames.

In `flipevidence_test.go`, replace line 253 with:

```js
    if ((f.hasAttribute("srcdoc") || f.hasAttribute("src")) && d.URL === "about:blank") return null;
```

and in the comment above it replace `showing its srcdoc` with `showing its own document`, and `A fresh <iframe srcdoc>` with `A fresh frame`.

In `header_rule_browser_test.go`, replace the `ready` arrow function and the two lines after `f.loading = "eager";` with:

```js
    const ready = () => {
      try { const d = f.contentDocument; return d && d.URL !== "about:blank" && d.readyState === "complete" && d.body; }
      catch (e) { return false; }
    };
    if (ready()) { done(); return; }
    f.addEventListener("load", () => done(), { once: true });
    f.loading = "eager";
    setTimeout(done, 5000);
```

(Task 2 replaces this wait with the shared one.)

In `a11y_test.go`, in `TestA11yScansThePreviewDocuments`' `ready` script, change the `settled` line to:

```js
				    return d && d.URL === f.src && d.readyState === "complete" && d.body && d.body.children.length ? d : null;
```

and in the comment at line 857 replace `srcdoc means contentWindow.axe is right there` with `a same-origin frame means contentWindow.axe is right there`.

- [ ] **Step 7: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS. In `-v` output `TestEveryPageStaysUnderItsBudget` logs a heaviest page near 109 KB (Date and time or Form), and `TestEveryPreviewIsAFileOneFrameNames` logs about 5,220 preview files.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestA11yScansThePreviewDocuments|TestTheFlipDidNotMoveAPixel|TestNoGalleryPageStillDrawsTheRakeLine|TestPreviewFrameHeightsFitTheirContent' -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS.

- [ ] **Step 8: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 9: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want=".rastrillo/budgets.txt \
  internal/designsystem/page.go \
  internal/designsystem/screens.go \
  internal/designsystem/formats.go \
  internal/designsystem/designsystem.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/header_rule_test.go \
  internal/designsystem/a11y_test.go \
  internal/designsystem/flipevidence_test.go \
  internal/designsystem/header_rule_browser_test.go"
maybe=""
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Write every preview as a file instead of a srcdoc

Each sample was in its page twice, once as the escaped document its
frame carried. On Form that copy was 47 KB and put the page over the
128 KiB cap on a debt entry, and every Code tab improvement still to
come needs bytes it could not spend. The rejected alternative, keeping
srcdoc and trimming elsewhere, was measured: it frees 7 KB and the new
Code tab costs 10 KB before any highlighting.

The documents are marked noindex because they are fragments. A path
produced twice fails the build rather than overwriting a page. The
pages stop loading select.js, calendar.js and datetime.js, which only
the previews ever used.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 2: Frames load by condition, not by sleep, from a shared drive package

Spec Tests "Migrated" (`eagerly`, `header_rule_browser_test.go`) and "CI bound" (no fixed sleeps in the wide height drive, a deadline taken from measurement, a missing file failing the leg, Form's request count logged). This task also creates `internal/designsystem/galleryrig`, the shared drive package Global Constraints describes, because the helpers rewritten here are the first that the sweep package (Task 11) also needs.

**Files:**
- Create: `internal/designsystem/galleryrig/rig.go`, `internal/designsystem/galleryrig/tree_test.go`
- Create: `internal/designsystem/rig_browser_test.go`
- Modify: `internal/designsystem/browser_test.go`: delete `treeHandler` (56-79), `eagerly` (1473-1486) and `clickEvery` (1461-1472); `clickedMobile` and `clickedDesktop` (1454, 1459); `TestPreviewFrameHeightsFitTheirContent` (939-1055)
- Modify: `internal/designsystem/header_rule_browser_test.go`: `headerRuleSweep`'s wait (67-82) and its caller (around 217-223)
- Modify: `Makefile` (the `browser` target's package list)

**Interfaces:**
- Consumes: Task 1's file-backed frames.
- Produces:
  - In `galleryrig` (package `galleryrig`, every file `//go:build browser`; it imports nothing of the gallery's, because `internal/designsystem`'s own tests import it, and an import back would be a cycle):
    - `func Tree(t *testing.T, files map[string][]byte, mountPrefix string) http.Handler`
    - `func TreeRecording(t *testing.T, files map[string][]byte, mountPrefix string, miss func(path string)) http.Handler`
    - `const SettleFrames`, `func Eagerly(t *testing.T, ctx context.Context, where string)`
    - `const SettleMobile`, `func MobileSettle(t *testing.T, ctx context.Context, where string)`
    - `func ClickEvery(mod string) string`
  - In `internal/designsystem` tests: `treeHandler(t)`, `eagerly`, `mobileSettle`, `clickedMobile` and `clickedDesktop` keep their names and signatures. Every existing call site is unchanged.

- [ ] **Step 1: Write the failing test**

Create `internal/designsystem/galleryrig/tree_test.go`:

```go
//go:build browser

package galleryrig

import (
	"net/http/httptest"
	"testing"
)

// The tree handler is what makes a missing preview file loud: a frame
// whose src names nothing loads a 404 page, which settles, measures and
// scans like any other document. The control asks for a file the tree
// does not have and requires the request to be recorded, while an
// asset that exists and a request outside the mount are not misses.
func TestTheTreeHandlerRecordsAMissingFile(t *testing.T) {
	const prefix = "/design-system/"
	files := map[string][]byte{"tokens.css": []byte(":root{}")}
	var missing []string
	h := TreeRecording(t, files, prefix, func(path string) { missing = append(missing, path) })
	for _, path := range []string{prefix + "tokens.css", prefix + "day/en/form/no-such-preview.html", "/favicon.ico"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if len(missing) != 1 || missing[0] != prefix+"day/en/form/no-such-preview.html" {
		t.Errorf("recorded %v, want exactly the missing preview", missing)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -count=1 ./internal/designsystem/galleryrig/`
Expected: FAIL to compile, `undefined: TreeRecording`.

- [ ] **Step 3: Write `galleryrig/rig.go`**

```go
//go:build browser

// Package galleryrig is the design-system gallery's shared drive
// equipment: the rendered tree served at its mount, frames waited on
// until each shows its own document, and the other conditions that
// internal/designsystem's browser tests and its sweep package both
// need. A package rather than test files because two test packages use
// it; browser-tagged like harness, so chromedp stays out of the
// ordinary build graph. It imports nothing of the gallery's: the
// gallery's own tests import it, and an import back would be a cycle,
// so the rendered files are handed in.
package galleryrig

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Tree serves a rendered tree at the mount its pages expect, and fails
// the test that uses it if anything asked for a file the tree does not
// have. Every URL in a page is absolute under the mount, so serving the
// tree anywhere else would 404 the stylesheet and a drive would measure
// an unstyled page; and a preview frame naming a missing file loads a
// 404 page that settles, measures and scans like any other document, so
// the miss has to be counted here.
//
// The failure is reported from t.Cleanup rather than from the request:
// lazy frames can still be loading after the test function returns,
// and t.Errorf from a server goroutine after that panics.
func Tree(t *testing.T, files map[string][]byte, mountPrefix string) http.Handler {
	t.Helper()
	var mu sync.Mutex
	var missing []string
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if len(missing) > 0 {
			t.Errorf("the tree was asked for %d file(s) it does not have: %v", len(missing), missing)
		}
	})
	return TreeRecording(t, files, mountPrefix, func(p string) {
		mu.Lock()
		missing = append(missing, p)
		mu.Unlock()
	})
}

// TreeRecording is Tree with each miss handed to the caller, so the
// recording itself can be tested.
func TreeRecording(t *testing.T, files map[string][]byte, mountPrefix string, miss func(path string)) http.Handler {
	t.Helper()
	types := map[string]string{
		".html": "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "text/javascript; charset=utf-8",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The browser asks for /favicon.ico on its own; nothing outside
		// the mount is the tree's to answer.
		if !strings.HasPrefix(r.URL.Path, mountPrefix) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, mountPrefix)
		body, ok := files[name]
		if !ok {
			miss(r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if ct, ok := types[path.Ext(name)]; ok {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(body)
	})
}

// SettleFrames turns every preview frame eager in one pass and waits
// until each shows its own document: the address its src names (a
// fresh frame is about:blank, complete, for a moment before its file
// replaces it), complete, populated, and the same height across 150ms.
// It answers with the frames that never got there, by title, and with
// how many frame documents the page requested, which is the cost a
// reader pays for reading the whole page.
const SettleFrames = `(async () => {
  const frames = [...document.querySelectorAll(".ds-view__frame")];
  frames.forEach(f => { f.loading = "eager"; });
  const reading = f => {
    let d = null;
    try { d = f.contentDocument; } catch (e) { return null; }
    if (!d || d.URL !== f.src || d.readyState !== "complete" || !d.body || !d.body.children.length) return null;
    return d.body.scrollHeight;
  };
  const deadline = Date.now() + 30000;
  let pending = frames;
  while (pending.length && Date.now() < deadline) {
    const before = pending.map(reading);
    await new Promise(r => setTimeout(r, 150));
    pending = pending.filter((f, i) => before[i] === null || reading(f) !== before[i]);
  }
  const requested = performance.getEntriesByType("resource").filter(e => e.initiatorType === "iframe").length;
  return JSON.stringify({Pending: pending.map(f => f.title), Frames: frames.length, Requested: requested});
})()`

// Eagerly loads every frame on the page and waits until each settles.
// Measuring what a reader sees means reaching inside each frame, and a
// frame that never loaded reports zero height, which reads exactly like
// a collapsed preview: so a frame that does not settle within 30s fails
// the leg by name rather than being measured.
func Eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(SettleFrames, &raw,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("%s: loading the framed documents: %v", where, err)
	}
	var got struct {
		Pending           []string
		Frames, Requested int
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: reading the frame settle (%q): %v", where, raw, err)
	}
	if len(got.Pending) > 0 {
		t.Fatalf("%s: %d of %d frames never settled within 30s: %v", where, len(got.Pending), got.Frames, got.Pending)
	}
	t.Logf("%s: %d frames settled; the page requested %d frame documents", where, got.Frames, got.Requested)
}

// SettleMobile waits after every Mobile radio has been clicked: every
// frame laid out no wider than a phone, and every frame's width and
// content height the same across 150ms.
const SettleMobile = `(async () => {
  const frames = [...document.querySelectorAll(".ds-view__frame")];
  const read = () => frames.map(f => {
    let h = -1;
    try { h = f.contentDocument.body.scrollHeight; } catch (e) {}
    return f.offsetWidth + ":" + h;
  }).join(",");
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const a = read();
    await new Promise(r => setTimeout(r, 150));
    if (a === read() && frames.every(f => f.offsetWidth <= 390)) return "ok";
  }
  return "never settled: " + frames.filter(f => f.offsetWidth > 390).map(f => f.title).slice(0, 5).join("; ");
})()`

// MobileSettle runs SettleMobile and fails the leg if it times out.
func MobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var state string
	if err := chromedp.Run(ctx, chromedp.Evaluate(SettleMobile, &state,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("%s: waiting for the Mobile layout: %v", where, err)
	}
	if state != "ok" {
		t.Fatalf("%s: the frames %s", where, state)
	}
}

// ClickEvery is the script that clicks every radio of one tab ("d",
// "m" or "c") and reports how many clicks moved something. It is the
// instrument's own control: a reading taken after a click that did not
// land is a reading of the state before it.
func ClickEvery(mod string) string {
	return `(() => {
  let clicked = 0, moved = 0;
  document.querySelectorAll(".ds-view__tab--` + mod + ` input").forEach(i => {
    const before = i.checked;
    i.click();
    clicked++;
    if (i.checked && !before) moved++;
  });
  return JSON.stringify({clicked: clicked, moved: moved});
})()`
}
```

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -count=1 ./internal/designsystem/galleryrig/`
Expected: PASS.

- [ ] **Step 4: Give this package its short names, and delete the old helpers**

Create `internal/designsystem/rig_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// The shared drive helpers, under the names this package's tests are
// written with. One line each: the implementation is galleryrig's, which
// the sweep package uses too, so the two cannot drift.

func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return galleryrig.Tree(t, files, mountPrefix)
}

func eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.Eagerly(t, ctx, where)
}

func mobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.MobileSettle(t, ctx, where)
}
```

In `browser_test.go`, delete `treeHandler` and its comment, `eagerly` and its comment, and `clickEvery` and its comment. Change the two declarations to `var clickedMobile = galleryrig.ClickEvery("m")` and `var clickedDesktop = galleryrig.ClickEvery("d")`, keeping their comments. Add the `galleryrig` import, and drop any import the deletions leave unused (`path` if nothing else uses it).

- [ ] **Step 5: Take the sleeps out of the wide height drive**

In `TestPreviewFrameHeightsFitTheirContent`, replace the `chromedp.Run` block inside `for _, tc := range rows` with:

```go
		kind := tc.kind
		where := kind + " at 1500px"
		var desktop, mobile, clicked string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1500, 1000),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf(kind))),
			chromedp.WaitVisible(`.ds-view__frame`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s: loading: %v", where, err)
		}
		eagerly(t, ctx, where)
		if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &desktop)); err != nil {
			t.Fatalf("%s: measuring Desktop: %v", where, err)
		}
		// And the same page on the other tab. The mobile height is one
		// factor off the desktop one rather than a second table, so this
		// is where that factor is checked.
		if err := chromedp.Run(ctx, chromedp.Evaluate(clickedMobile, &clicked)); err != nil {
			t.Fatalf("%s: choosing Mobile: %v", where, err)
		}
		mobileSettle(t, ctx, where)
		if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &mobile)); err != nil {
			t.Fatalf("%s: measuring Mobile: %v", where, err)
		}
```

Delete the old `var desktop, mobile string` line.

- [ ] **Step 6: Put the header-rule sweep on the shared wait**

In `header_rule_browser_test.go`, delete the `await Promise.all(frames.map(...))` block at the top of `headerRuleSweep` (keep `const frames = [...document.querySelectorAll("iframe")];`). Change the comment's first line to `// headerRuleSweep reads every page header in the document and in every frame it can reach; the caller has already loaded every frame with eagerly.` At the caller, split the run:

```go
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(url),
			chromedp.WaitVisible(`[rst-page-header]`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("%s: driving the page: %v", name, err)
		}
		eagerly(t, ctx, name)
		if err := chromedp.Run(ctx, chromedp.Evaluate(headerRuleSweep, &raw,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			t.Fatalf("%s: sweeping the headers: %v", name, err)
		}
```

- [ ] **Step 7: Add the package to `make browser`**

In the `Makefile`'s `browser` target, add `./internal/designsystem/galleryrig/` after `./internal/designsystem/` in the package list.

- [ ] **Step 8: Run the drives and set the deadline from measurement**

```bash
set -o pipefail; mkdir -p "$TMPDIR/gate"
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestPreviewFrameHeightsFitTheirContent|TestNoGalleryPageStillDrawsTheRakeLine|TestThePreviewWidgetIsUsableOnAPhone' -count=1 -v -timeout 20m ./internal/designsystem/ 2>&1 | tee "$TMPDIR/gate/task2-drives.log"; echo "exit $?"
grep -E '^(--- |ok|FAIL)|frames settled' "$TMPDIR/gate/task2-drives.log"
```

Expected: `exit 0`, all PASS, and Form's line reads `form at 1500px: 35 frames settled; the page requested 35 frame documents`. On a quiet machine, run it twice and take the larger `--- PASS: TestPreviewFrameHeightsFitTheirContent (Ds)` as `D`. Only an `exit 0` run counts.

Replace `420*time.Second` in `TestPreviewFrameHeightsFitTheirContent` with `2*D` rounded up to the next 10 seconds, and put the measurement above it:

```go
	// Twice its measured time on a quiet runner (D seconds on
	// <date>, with frames waited on rather than slept for), so a
	// shared box has room and a real hang still dies.
```

Write the real `D` and the date. Then run the same command once more with the new deadline. Expected: `exit 0`.

- [ ] **Step 9: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound). Expected: all green. Report the design-system package's seconds.

- [ ] **Step 10: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="Makefile \
  internal/designsystem/galleryrig/rig.go \
  internal/designsystem/galleryrig/tree_test.go \
  internal/designsystem/rig_browser_test.go \
  internal/designsystem/browser_test.go \
  internal/designsystem/header_rule_browser_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Wait for each preview frame's own document instead of sleeping

The drives slept six or eight seconds and measured whatever had loaded.
A frame that never loaded measured as a collapsed preview, and the
header-rule sweep counted a timeout as done. File-backed frames make a
real condition possible: each frame must show the address its src
names, complete and stable, or the leg fails by name. A request for a
file the tree does not have now fails the test too, because a 404 page
settles and measures like a preview.

The helpers move to galleryrig, a browser-tagged package, because the
heavy sweeps get a test package of their own to stay inside the CI time
bound, and both packages need one copy of them. The wide height
drive's deadline is twice its measured time.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---

### Task 3: `internal/codeview`: the formatter and the highlighter

Spec 1.2 (the formatter's rule, and the "formatting moves only whitespace" test) and 1.3 (five token kinds as two-letter custom elements, escaping only `&`, `<` and `>`). This task covers pure functions and their unit tests. Nothing in the gallery calls them until Task 5.

**Files:**
- Create: `internal/codeview/pieces.go`, `internal/codeview/format.go`, `internal/codeview/highlight.go`
- Test: `internal/codeview/format_test.go`, `internal/codeview/highlight_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func codeview.Format(html string) string`, `func codeview.Highlight(src string) template.HTML`. In the output the five elements are `ds-t` (a tag's `<name` or `</name`), `ds-a` (attribute name), `ds-v` (attribute value with its quotes), `ds-x` (`{{`, `}}`, identifiers and `.Fields` inside an action) and `ds-s` (a string literal inside an action).

- [ ] **Step 1: Write the failing tests**

Create `internal/codeview/format_test.go`:

```go
package codeview

import (
	"regexp"
	"strings"
	"testing"
)

// normalise removes what Format may add: whitespace-only text between
// two tags where either is block-level, outside <pre>, <textarea> and
// <svg>. It is written here, apart from Format's own walk, so a walk
// that dropped some other whitespace could not drop it from both sides
// of the comparison at once.
var (
	normaliseTag = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)[^>]*>`)
	normaliseBlock = map[string]bool{
		"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
		"dialog": true, "dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
		"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
		"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
		"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
		"search": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
		"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
	}
)

func normalise(s string) string {
	var b strings.Builder
	at, prev, raw := 0, "", ""
	for _, m := range normaliseTag.FindAllStringSubmatchIndex(s, -1) {
		name, closing, gap := strings.ToLower(s[m[4]:m[5]]), m[3] > m[2], s[at:m[0]]
		if raw != "" || prev == "" || strings.TrimSpace(gap) != "" || !(normaliseBlock[prev] || normaliseBlock[name]) {
			b.WriteString(gap)
		}
		b.WriteString(s[m[0]:m[1]])
		switch {
		case raw != "" && closing && name == raw:
			raw = ""
		case raw == "" && !closing && (name == "pre" || name == "textarea" || name == "svg"):
			raw = name
		}
		prev, at = name, m[1]
	}
	return b.String() + s[at:]
}

var formatCases = []string{
	`<section rst-box><form rst-form method="post" action="#"><div rst-field><label rst-field-label for="t">Title</label><input rst-input id="t" name="t"></div></form></section>`,
	`<div rst-list><div rst-lrow><a class="rst-nm" href="/p/1">Release notes<small>2 August</small></a> <span rst-pill>Draft</span></div></div>`,
	`<span rst-meter><meter rst-meter-bar value="100" min="0" max="100" aria-hidden="true"></meter><span rst-meter-num>700/500</span></span>`,
	"<ul>\n  <li>One</li>\n\n<li>Two</li></ul><p>After</p>",
	`<details rst-dropdown name="rst-menus"><summary>Filter<span rst-caret aria-hidden="true"><svg class="icon" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg></span></summary><div rst-dropdown-menu><a href="/a">A</a></div></details>`,
}

func TestFormatMovesOnlyWhitespaceBetweenBlockTags(t *testing.T) {
	for _, in := range formatCases {
		out := Format(in)
		if normalise(out) != normalise(in) {
			t.Errorf("Format changed more than whitespace between block tags:\n in: %s\nout: %s", in, out)
		}
		if Format(out) != out {
			t.Errorf("Format is not idempotent on:\n%s", out)
		}
	}
	// The comparison's own control: a formatter that dropped the space
	// between two inline elements, a space a reader sees, must fail it.
	in := formatCases[1]
	broken := strings.Replace(Format(in), "</a> <span", "</a><span", 1)
	if broken == Format(in) {
		t.Fatal("the control fixture has no space between </a> and <span> to drop")
	}
	if normalise(broken) == normalise(in) {
		t.Error("normalise cannot see a dropped space between two inline elements; this test would pass a formatter that loses it")
	}
}

// The space between two inline elements, and between text and an
// inline element, is rendered, so Format keeps it exactly.
func TestFormatKeepsTheSpaceBetweenInlineElements(t *testing.T) {
	for in, want := range map[string]string{
		`<div><a href="/a">A</a> <span>B</span></div>`:     "<div>\n  <a href=\"/a\">A</a> <span>B</span>\n</div>",
		`<p>Hello <b>world</b> <i>again</i></p>`:           "<p>Hello <b>world</b> <i>again</i>\n</p>",
		`<span rst-a>x</span>  <span rst-b>y</span>`:       `<span rst-a>x</span>  <span rst-b>y</span>`,
		"<li><a href=\"/x\">X</a>\n<a href=\"/y\">Y</a></li>": "<li>\n  <a href=\"/x\">X</a>\n<a href=\"/y\">Y</a>\n</li>",
	} {
		if got := Format(in); got != want {
			t.Errorf("Format(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestFormatBreaksAtBlockTagsAndIndents(t *testing.T) {
	got := Format(`<section rst-box><form rst-form><div rst-field><label>Title</label><input name="t"></div></form></section>`)
	want := "<section rst-box>\n  <form rst-form>\n    <div rst-field>\n      <label>Title</label><input name=\"t\">\n    </div>\n  </form>\n</section>"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// An inline run stays one line however long: breaking inside it
	// would change the space a reader sees between two words.
	meter := formatCases[2]
	if got := Format(meter); got != meter {
		t.Errorf("an inline run was broken:\n%s", got)
	}
}

// A <pre> or <textarea> whose content looks like tags,
// an svg, and an attribute value holding a '>' all pass through byte
// for byte.
func TestFormatLeavesRawZonesAndQuotedBrackets(t *testing.T) {
	for _, raw := range []string{
		"<pre>  <div>\n not a tag </div>\n</pre>",
		"<textarea name=\"n\"><p>typed</p>\n  </textarea>",
		`<svg viewBox="0 0 2 2"><g><path d="M0 0"/></g></svg>`,
	} {
		in := "<div>" + raw + "</div>"
		out := Format(in)
		if !strings.Contains(out, raw) {
			t.Errorf("a raw zone changed:\n in: %s\nout: %s", in, out)
		}
	}
	in := `<div data-tip="a > b"><p title='x <div> y'>t</p></div>`
	out := Format(in)
	for _, v := range []string{`data-tip="a > b"`, `title='x <div> y'`} {
		if !strings.Contains(out, v) {
			t.Errorf("an attribute value changed: %s", out)
		}
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("a '>' inside a quoted value was taken for the end of a tag:\n%s", out)
	}
}
```

Create `internal/codeview/highlight_test.go`:

```go
package codeview

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

var highlightTag = regexp.MustCompile(`</?ds-[tavxs]>`)

// plain is what a reader sees and copies: the highlighted markup with
// its five elements removed and its entities decoded.
func plain(h string) string { return html.UnescapeString(highlightTag.ReplaceAllString(h, "")) }

func TestHighlightMarksTheFiveKinds(t *testing.T) {
	got := string(Highlight(`{{template "meter" dict "Percent" 140 "Items" .Locales}}`))
	want := `<ds-x>{{</ds-x><ds-x>template</ds-x> <ds-s>"meter"</ds-s> <ds-x>dict</ds-x> <ds-s>"Percent"</ds-s> 140 <ds-s>"Items"</ds-s> <ds-x>.Locales</ds-x><ds-x>}}</ds-x>`
	if got != want {
		t.Errorf("call:\n got %s\nwant %s", got, want)
	}
	got = string(Highlight(`<a rst-btn href="/x">Go</a><input disabled>`))
	want = `<ds-t>&lt;a</ds-t> <ds-a>rst-btn</ds-a> <ds-a>href</ds-a>=<ds-v>"/x"</ds-v>&gt;Go<ds-t>&lt;/a</ds-t>&gt;<ds-t>&lt;input</ds-t> <ds-a>disabled</ds-a>&gt;`
	if got != want {
		t.Errorf("markup:\n got %s\nwant %s", got, want)
	}
}

// The text on screen is the source, whatever is in it: entities, a
// '>' inside a quoted value, {{ inside an attribute, upper case.
func TestHighlightRoundTripsAwkwardMarkup(t *testing.T) {
	for _, src := range []string{
		`<a href="/x?a=1&b=2" data-tip="a > b">Tom &amp; Jerry &euro; < 3</a>`,
		`<DIV Title='it"s'>{{x}}</DIV><!-- a comment <b> -->`,
		`<div title="{{.Name}}">a {{ "}}" }} b</div>`,
		"{{template \"x\" dict\n    \"A\" \"a\\\"b\"\n    \"B\" `raw`}}",
		`a < b > c & d`,
		`<span rst-stat-num>&minus;3%</span>`,
	} {
		h := string(Highlight(src))
		if got := plain(h); got != src {
			t.Errorf("round trip lost bytes:\n src: %s\nplain: %s\n html: %s", src, got, h)
		}
		if strings.Contains(highlightTag.ReplaceAllString(h, ""), "<") {
			t.Errorf("an unescaped '<' reached the page: %s", h)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/codeview/`
Expected: FAIL. The build fails because `Format` and `Highlight` are undefined.

- [ ] **Step 3: Write `pieces.go`**

```go
// Package codeview lays out and colours the source a design-system
// gallery shows in its Code tab. It knows nothing about the gallery: it
// takes HTML or template source and gives back the same text, broken
// into lines or marked up for colour, so that the bytes a reader copies
// are the bytes the source holds.
package codeview

import "strings"

// piece is one run of markup: a tag, a comment, or the text between.
type piece struct {
	text string
	kind pieceKind
	name string // lower-case element name, for a tag
	end  bool   // a closing tag
	void bool   // opens no element: a void element or a self-closed tag
}

type pieceKind int

const (
	textPiece pieceKind = iota
	tagPiece
	commentPiece
)

// voidElements open nothing, so they never deepen the indent.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// tagEnd returns the index just past the tag that starts at s[i], or -1
// when s[i] does not open one. A '>' inside a quoted attribute value
// does not close the tag: data-tip="a > b" is one tag and not two.
func tagEnd(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '/' {
		j++
	}
	if j >= len(s) || !isLetter(s[j]) {
		return -1
	}
	var quote byte
	for ; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		}
	}
	return -1
}

// pieces splits markup into tags, comments and text. A '<' that opens
// no tag ("a < b") stays text.
func pieces(s string) []piece {
	var out []piece
	text := 0
	flush := func(at int) {
		if at > text {
			out = append(out, piece{text: s[text:at]})
		}
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				break
			}
			flush(i)
			j := i + 4 + end + 3
			out = append(out, piece{text: s[i:j], kind: commentPiece})
			i, text = j, j
			continue
		}
		j := tagEnd(s, i)
		if j < 0 {
			i++
			continue
		}
		flush(i)
		tag := s[i:j]
		closing := strings.HasPrefix(tag, "</")
		n := 1
		if closing {
			n = 2
		}
		k := n
		for k < len(tag) && !isSpace(tag[k]) && tag[k] != '>' && tag[k] != '/' {
			k++
		}
		name := strings.ToLower(tag[n:k])
		out = append(out, piece{
			text: tag, kind: tagPiece, name: name, end: closing,
			void: !closing && (voidElements[name] || strings.HasSuffix(tag, "/>")),
		})
		i, text = j, j
	}
	flush(len(s))
	return out
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
```

- [ ] **Step 4: Write `format.go`**

```go
package codeview

import "strings"

// blockElements are the elements whose boxes are blocks: whitespace
// between two tags where one of these is involved renders as nothing,
// so a line break there moves no pixel. Everything else (a, span,
// button, label, input, svg) is inline or inline-block, where a space
// is a space a reader sees, and is never broken.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"dialog": true, "dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
	"search": true, "section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
}

// rawElements keep every byte inside them. A <pre> or <textarea>
// renders its whitespace as written, script and style are not markup,
// and an svg's paths are drawing, not structure a reader reads.
var rawElements = map[string]bool{"pre": true, "textarea": true, "svg": true, "script": true, "style": true}

// Format lays rendered HTML out for reading: a line break and two
// spaces of indent per open element are inserted between two tags only
// where at least one is block-level, replacing whatever whitespace-only
// text sat there. Text, inline runs, attribute values and everything
// inside a raw element are left byte for byte. No column limit is
// promised: an inline run stays one line however long it is, and the
// page soft-wraps it instead.
func Format(html string) string {
	return layout(html, func(depth int) string { return "\n" + strings.Repeat("  ", depth) })
}

// layout walks the markup and writes brk(depth) wherever Format may
// break, and the original bytes everywhere else.
func layout(s string, brk func(depth int) string) string {
	ps := pieces(s)
	var b strings.Builder
	depth := 0
	var prev *piece // the last tag written, while only whitespace has followed it
	pending := ""
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		if p.kind != tagPiece {
			if p.kind == textPiece && prev != nil && strings.TrimSpace(p.text) == "" {
				pending += p.text
				continue
			}
			b.WriteString(pending)
			b.WriteString(p.text)
			pending, prev = "", nil
			continue
		}
		if p.end {
			depth = max(depth-1, 0)
		}
		if prev != nil && (blockElements[prev.name] || blockElements[p.name]) {
			b.WriteString(brk(depth))
		} else {
			b.WriteString(pending)
		}
		pending = ""
		b.WriteString(p.text)
		last := p
		if !p.end && !p.void {
			if rawElements[p.name] {
				j := closing(ps, i)
				for k := i + 1; k <= j; k++ {
					b.WriteString(ps[k].text)
				}
				i, last = j, ps[j]
			} else {
				depth++
			}
		}
		prev = &last
	}
	b.WriteString(pending)
	return b.String()
}

// closing is the index of the tag that closes the raw element opened
// at ps[i], counting nested elements of the same name; the last piece
// when the markup never closes it.
func closing(ps []piece, i int) int {
	name, open := ps[i].name, 1
	for j := i + 1; j < len(ps); j++ {
		if ps[j].kind != tagPiece || ps[j].name != name {
			continue
		}
		if ps[j].end {
			open--
			if open == 0 {
				return j
			}
		} else if !ps[j].void {
			open++
		}
	}
	return len(ps) - 1
}
```

- [ ] **Step 5: Write `highlight.go`**

```go
package codeview

import (
	"html/template"
	"strings"
)

// Highlight escapes source for an HTML text context and wraps five
// kinds of token in two-letter custom elements that the gallery's
// stylesheet colours: <ds-t> a tag's name with its opening bracket,
// <ds-a> an attribute name, <ds-v> an attribute value with its quotes,
// <ds-x> a template action's delimiters, keywords and fields, and <ds-s>
// a string literal inside an action.
//
// Custom elements rather than <span class>: a token costs 13 bytes this
// way and 29 the other, and on the heaviest page that difference was
// the page budget. An undefined element is inline with no role, so it
// changes nothing for assistive technology or for textContent.
//
// Only &, < and > are escaped. Quotes stay literal, which is valid in
// text and spares four bytes on every attribute a sample writes.
func Highlight(src string) template.HTML {
	var b strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "{{"):
			end := actionEnd(src, i)
			action(&b, src[i:end])
			i = end
		case src[i] == '<' && tagEnd(src, i) > 0:
			end := tagEnd(src, i)
			tag(&b, src[i:end])
			i = end
		default:
			j := i + 1
			for j < len(src) && !strings.HasPrefix(src[j:], "{{") && !(src[j] == '<' && tagEnd(src, j) > 0) {
				j++
			}
			escape(&b, src[i:j])
			i = j
		}
	}
	return template.HTML(b.String())
}

func escape(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteByte(s[i])
		}
	}
}

func wrapped(b *strings.Builder, kind, s string) {
	b.WriteString("<ds-" + kind + ">")
	escape(b, s)
	b.WriteString("</ds-" + kind + ">")
}

// tag writes one tag: its name, each attribute's name and value, and
// the whitespace and closing bracket between them as plain text.
func tag(b *strings.Builder, t string) {
	n := 1
	if strings.HasPrefix(t, "</") {
		n = 2
	}
	for n < len(t) && !isSpace(t[n]) && t[n] != '>' && t[n] != '/' {
		n++
	}
	wrapped(b, "t", t[:n])
	for i := n; i < len(t); {
		c := t[i]
		switch {
		case isSpace(c) || c == '/' || c == '>':
			escape(b, t[i:i+1])
			i++
		case c == '=':
			b.WriteByte('=')
			i++
			j := i
			if j < len(t) && (t[j] == '"' || t[j] == '\'') {
				q := t[j]
				j++
				for j < len(t) && t[j] != q {
					j++
				}
				if j < len(t) {
					j++
				}
			} else {
				for j < len(t) && !isSpace(t[j]) && t[j] != '>' {
					j++
				}
			}
			wrapped(b, "v", t[i:j])
			i = j
		default:
			j := i
			for j < len(t) && !isSpace(t[j]) && t[j] != '=' && t[j] != '>' && !(t[j] == '/' && j+1 < len(t) && t[j+1] == '>') {
				j++
			}
			wrapped(b, "a", t[i:j])
			i = j
		}
	}
}

// actionEnd returns the index just past the action starting at s[i],
// treating "}}" inside a string literal as part of the string.
func actionEnd(s string, i int) int {
	for j := i + 2; j < len(s); j++ {
		switch s[j] {
		case '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' {
					j++
				}
			}
		case '`':
			for j++; j < len(s) && s[j] != '`'; j++ {
			}
		case '}':
			if j+1 < len(s) && s[j+1] == '}' {
				return j + 2
			}
		}
	}
	return len(s)
}

// action writes one {{…}}: the delimiters, identifiers and .Fields as
// <ds-x>, string literals as <ds-s>, and everything else as text.
func action(b *strings.Builder, a string) {
	body, closed := strings.CutSuffix(a[2:], "}}")
	wrapped(b, "x", "{{")
	for i := 0; i < len(body); {
		c := body[i]
		switch {
		case c == '"' || c == '`':
			j := i + 1
			for j < len(body) && body[j] != c {
				if c == '"' && body[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(body))
			wrapped(b, "s", body[i:j])
			i = j
		case isLetter(c) || c == '_' || c == '.':
			j := i + 1
			for j < len(body) && (isLetter(body[j]) || body[j] == '_' || body[j] == '.' || body[j] >= '0' && body[j] <= '9') {
				j++
			}
			wrapped(b, "x", body[i:j])
			i = j
		default:
			escape(b, body[i:i+1])
			i++
		}
	}
	if closed {
		wrapped(b, "x", "}}")
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/codeview/`
Expected: PASS. If `TestHighlightMarksTheFiveKinds` fails on whitespace or bracket placement, the expected strings in the test are the contract, so fix the code and leave the test alone.

Then prove the inline-space tests bite. In `layout`, change the `else` branch's `b.WriteString(pending)` to `b.WriteString(strings.TrimSpace(pending))`, which is a formatter that drops the space between two inline elements. Run the command again. Expected: FAIL in `TestFormatKeepsTheSpaceBetweenInlineElements` (`<a href="/a">A</a><span>` where `</a> <span>` was wanted) and in `TestFormatMovesOnlyWhitespaceBetweenBlockTags` (`Format changed more than whitespace`). Restore the line and run once more. Expected: PASS.

- [ ] **Step 7: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green. The new directory sits well under the default 5,000/8,000-line budget.

- [ ] **Step 8: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/codeview/pieces.go \
  internal/codeview/format.go \
  internal/codeview/highlight.go \
  internal/codeview/format_test.go \
  internal/codeview/highlight_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Add a formatter and a highlighter for the gallery's Code tab

The Code tab showed rendered HTML on one line, up to 3,817px wide on a
phone, with no colour. These two functions lay it out and mark it up
without changing a byte a reader copies. Breaks go only between tags
where one is block-level, so rendering cannot change. The highlighter
escapes only &, < and >, and the round trip is tested on the awkward
cases: entities, a > inside a quoted value, {{ inside an attribute.

It is a package of its own because it knows nothing about the gallery
and the gallery's directory is near its line budget.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 4: The call: `codeview.Call`, `callFor` and the contract in every locale

Spec 1.1 (the generator, the value table, placeholders, key order, layout, defaults, the contract) and the inventory row for locale-menu's note (C14). Nothing renders a call yet: Task 5 puts it in the Code tab.

**Files:**
- Create: `internal/codeview/call.go`, `internal/codeview/call_test.go`
- Create: `internal/designsystem/calls.go`, `internal/designsystem/calls_test.go`
- Modify: `internal/designsystem/samples.go`: `sample` gains `Bind` (around line 66), and locale-menu's state (around line 616)
- Modify: `internal/designsystem/prose.go`: replace the old locale-menu note's entry with the approved one, through the tool
- Create: `internal/copyedit/copyedit.go`, `internal/copyedit/copyedit_test.go`, `internal/copyedit/run.go`

**Interfaces:**
- Consumes: `renderSample`, `partialTree`, `parseRawSamples`, `families()`, and Task 1's `sampleTree`.
- Produces:
  - `func codeview.Call(partial string, data any, order []string, bind map[string]string) (source string, dot map[string]any, err error)`
  - `var codeview.ErrUnwritable`
  - `type call struct{ Source string; Dot map[string]any }`
  - `func callFor(partial string, s sample, locale string) (call, error)` and `func partialKeys() map[string][]string`
  - `sample.Bind map[string]string` (data key to dot field)
  - `internal/copyedit`: `Edit`, `Entry`, `LoadApproved`, `ApplyProse`, `Fill`, `Locales`, and the command `GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit <file>` every later copy task runs

- [ ] **Step 1: Check the approved copy this task writes**

```bash
test "$(jq -r '.action' copy-review/batch-b1-result.json)" = approve
jq -r --arg id gallery.locale_menu.note '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json
```

Expected: `This one posts to /_locale, so it needs a server. Its Items come from rastrillo.LocaleItems(r). The switcher in this page's own header is the links-only version.` If either command fails or the text differs, stop and report to the controller.

- [ ] **Step 2: Write the failing unit tests**

Create `internal/codeview/call_test.go`:

```go
package codeview

import (
	"errors"
	"html/template"
	"strings"
	"testing"
	ttemplate "text/template"
)

// run executes a call's source against a one-partial tree that prints
// its dot as Go syntax, so a test can see exactly the value the call
// hands the partial.
func run(t *testing.T, src string, dot map[string]any) string {
	t.Helper()
	tmpl := ttemplate.Must(ttemplate.New("").Funcs(ttemplate.FuncMap{
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
		"list": func(v ...any) []any { return v },
	}).Parse(`{{define "p"}}{{printf "%#v" .}}{{end}}{{define "call"}}` + src + `{{end}}`))
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "call", dot); err != nil {
		t.Fatalf("executing %s: %v", src, err)
	}
	return b.String()
}

func TestCallWritesEachKindOfValue(t *testing.T) {
	src, dot, err := Call("p", map[string]any{"Percent": 140, "Text": "700/500"}, []string{"Percent", "Text"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Percent" 140 "Text" "700/500"}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
	if len(dot) != 0 {
		t.Errorf("an unbound call has dot %v, want empty", dot)
	}
	for _, c := range []struct {
		data any
		want string
	}{
		{"Post saved.", `{{template "p" "Post saved."}}`},
		{7, `{{template "p" 7}}`},
		{true, `{{template "p" true}}`},
		{map[string]any{"F": 1.5, "U": uint8(3), "B": false}, `{{template "p" dict "B" false "F" 1.5 "U" 3}}`},
		{map[string]any{"Hidden": [][2]string{{"csrf", "t"}, {"id", "1"}}}, `{{template "p" dict "Hidden" (list (list "csrf" "t") (list "id" "1"))}}`},
		{map[string]any{"M": map[string]any{"b": 1, "a": 2}}, `{{template "p" dict "M" (dict "a" 2 "b" 1)}}`},
		{map[string]any{}, `{{template "p" dict}}`},
	} {
		got, _, err := Call("p", c.data, nil, nil)
		if err != nil {
			t.Errorf("%#v: %v", c.data, err)
			continue
		}
		if got != c.want {
			t.Errorf("%#v:\n got %s\nwant %s", c.data, got, c.want)
		}
	}
}

func TestCallOrdersKeysByThePartialsOwnList(t *testing.T) {
	src, _, err := Call("p", map[string]any{"A": 1, "Z": 2, "Label": "x", "Name": "n"}, []string{"Name", "Label", "Missing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Name" "n" "Label" "x" "A" 1 "Z" 2}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
}

func TestCallBreaksALongCallOnePairPerLine(t *testing.T) {
	data := map[string]any{
		"Name": "digest", "Type": "radio",
		"Options": []any{
			map[string]any{"Value": "daily", "Title": "Daily digest"},
			map[string]any{"Value": "weekly", "Title": "Weekly digest"},
		},
	}
	src, _, err := Call("choice-field", data, []string{"Name", "Type", "Options"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `{{template "choice-field" dict
    "Name" "digest"
    "Type" "radio"
    "Options" (list
        (dict "Title" "Daily digest" "Value" "daily")
        (dict "Title" "Weekly digest" "Value" "weekly"))}}`
	if src != want {
		t.Errorf("got\n%s\nwant\n%s", src, want)
	}
	if got, one := run(t, strings.Replace(src, `"choice-field"`, `"p"`, 1), nil), run(t, `{{template "p" dict "Name" "digest" "Type" "radio" "Options" (list (dict "Title" "Daily digest" "Value" "daily") (dict "Title" "Weekly digest" "Value" "weekly"))}}`, nil); got != one {
		t.Errorf("the broken layout executes differently from the one-line form:\n%s\n%s", got, one)
	}
}

func TestCallBindsAPlaceholder(t *testing.T) {
	items := []struct{ Code string }{{"en"}, {"ga"}}
	src, dot, err := Call("p", map[string]any{"Items": items, "Return": "/"}, []string{"Items", "Return"}, map[string]string{"Items": "Locales"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Items" .Locales "Return" "/"}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
	if _, ok := dot["Locales"]; !ok || len(dot) != 1 {
		t.Errorf("dot is %v, want exactly Locales", dot)
	}
}

func TestCallRefusesWhatItCannotWriteTruthfully(t *testing.T) {
	var nilPtr *int
	for name, data := range map[string]any{
		"a struct":      map[string]any{"V": struct{ A int }{1}},
		"a pointer":     map[string]any{"V": new(int)},
		"nil":           map[string]any{"V": nil},
		"a nil pointer": map[string]any{"V": nilPtr},
		"template.HTML": map[string]any{"V": template.HTML("<b>x</b>")},
		"a func":        map[string]any{"V": func() {}},
		"int keys":      map[int]any{1: "x"},
	} {
		if _, _, err := Call("p", data, nil, nil); !errors.Is(err, ErrUnwritable) {
			t.Errorf("%s: err = %v, want ErrUnwritable", name, err)
		}
	}
}

// Strings that are awkward as template literals still
// round-trip through the call to the exact value.
func TestCallQuotesAwkwardStrings(t *testing.T) {
	for _, s := range []string{`say "hi"`, `back\slash`, "two\nlines", "{{not an action}}", "−6 … ✕", "tab\there", "`tick`"} {
		src, _, err := Call("p", map[string]any{"S": s}, nil, nil)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if got, want := run(t, src, nil), `map[string]interface {}{"S":`+strconvQuote(s)+`}`; got != want {
			t.Errorf("%q: the call hands the partial %s, want %s", s, got, want)
		}
	}
}
```

and add `func strconvQuote(s string) string { return strconv.Quote(s) }` with the `strconv` import, because `%#v` prints a string as `strconv.Quote` does.

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/codeview/`
Expected: FAIL to compile, `undefined: Call` and `undefined: ErrUnwritable`.

- [ ] **Step 4: Write `internal/codeview/call.go`**

```go
package codeview

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ErrUnwritable is a value a call cannot show truthfully. A struct is
// one on purpose: a struct caller has different missing-field semantics
// from a map caller, so writing it as a dict would show a call the
// reader did not write. A value like that is the app's own, and a
// placeholder bound to it says so.
var ErrUnwritable = errors.New("bind it to a placeholder or give it as a map")

// lineLimit is the width under which a call stays on one line.
const lineLimit = 80

// Call writes the {{template}} action that renders partial with data,
// and the dot that action must run against. A map's keys are written
// in order's order first, then the rest alphabetically, so the call is
// the same on every render. A key in bind is written as the placeholder
// .Field it names, and dot carries the value under that field.
//
// A call that fits in 80 columns is one line. A longer one breaks after
// dict, one key and value per line, four spaces in, and a list of dicts
// puts each dict on a line of its own four spaces further in. Go
// templates allow newlines inside an action, so every layout parses.
func Call(partial string, data any, order []string, bind map[string]string) (string, map[string]any, error) {
	w := &callWriter{bind: bind, dot: map[string]any{}}
	head := "{{template " + strconv.Quote(partial)
	v := reflect.ValueOf(data)
	if s, ok := scalar(v); ok {
		return head + " " + s + "}}", w.dot, nil
	}
	if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
		return "", nil, fmt.Errorf("the data is %s: %w", describe(v), ErrUnwritable)
	}
	keys := keysOf(v, order)
	one := []string{head + " dict"}
	long := []string{head + " dict"}
	for _, k := range keys {
		short, err := w.value(k, v.MapIndex(reflect.ValueOf(k)), "", true)
		if err != nil {
			return "", nil, err
		}
		wide, err := w.value(k, v.MapIndex(reflect.ValueOf(k)), "    ", false)
		if err != nil {
			return "", nil, err
		}
		one = append(one, strconv.Quote(k)+" "+short)
		long = append(long, "    "+strconv.Quote(k)+" "+wide)
	}
	if line := strings.Join(one, " ") + "}}"; len(line) <= lineLimit && !strings.Contains(line, "\n") {
		return line, w.dot, nil
	}
	return strings.Join(long, "\n") + "}}", w.dot, nil
}

type callWriter struct {
	bind map[string]string
	dot  map[string]any
}

// value writes one argument. path is the top-level key it came from,
// which is the only level a placeholder can bind; indent is the indent
// of the line it starts on; oneLine forbids the per-dict lines a long
// call gives a list of dicts.
func (w *callWriter) value(path string, v reflect.Value, indent string, oneLine bool) (string, error) {
	if field, ok := w.bind[path]; ok && indent != "nested" {
		w.dot[field] = v.Interface()
		return "." + field, nil
	}
	for v.IsValid() && v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if s, ok := scalar(v); ok {
		return s, nil
	}
	if !v.IsValid() {
		return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
	}
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
		}
		parts := []string{"(dict"}
		for _, k := range keysOf(v, nil) {
			s, err := w.value(path, v.MapIndex(reflect.ValueOf(k)), "nested", true)
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Quote(k), s)
		}
		return strings.Join(parts, " ") + ")", nil
	case reflect.Slice, reflect.Array:
		items := make([]string, 0, v.Len())
		dicts := false
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			for item.Kind() == reflect.Interface {
				item = item.Elem()
			}
			dicts = dicts || item.Kind() == reflect.Map
			s, err := w.value(path, item, "nested", true)
			if err != nil {
				return "", err
			}
			items = append(items, s)
		}
		if oneLine || !dicts {
			return strings.TrimSpace("(list "+strings.Join(items, " ")) + ")", nil
		}
		inner := indent + "    "
		return "(list\n" + inner + strings.Join(items, "\n"+inner) + ")", nil
	}
	return "", fmt.Errorf("%s is %s: %w", path, describe(v), ErrUnwritable)
}

// scalar writes a value a template can spell as a literal: a string of
// type string (a named string type such as template.HTML is not, since
// the partial would receive a plain string from the call and could
// render it differently), any integer, a float64, a bool.
func scalar(v reflect.Value) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	switch v.Kind() {
	case reflect.String:
		if v.Type() == reflect.TypeOf("") {
			return strconv.Quote(v.String()), true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), true
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	}
	return "", false
}

// keysOf is a string-keyed map's keys: those order names first, in its
// order, then the rest alphabetically.
func keysOf(v reflect.Value, order []string) []string {
	have := map[string]bool{}
	for _, k := range v.MapKeys() {
		have[k.String()] = true
	}
	out := make([]string, 0, len(have))
	for _, k := range order {
		if have[k] {
			out = append(out, k)
			delete(have, k)
		}
	}
	rest := make([]string, 0, len(have))
	for k := range have {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func describe(v reflect.Value) string {
	if !v.IsValid() {
		return "nil"
	}
	return "a " + v.Type().String()
}
```

Two details matter here. `indent == "nested"` is how nested values refuse to bind. Only a top-level key's value is the app's own, so a nested map or list item never becomes a placeholder. And the one-line list is written as `(list a b)`, with `(list)` when empty.

- [ ] **Step 5: Run the codeview tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/codeview/`
Expected: PASS.

- [ ] **Step 6: Write the failing contract test**

Create `internal/designsystem/calls_test.go`:

```go
package designsystem

import (
	"errors"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
)

// Every generated call renders the partial's own output, byte for byte,
// in all twelve locales: the call is parsed into a fresh copy of that
// locale's partial tree, run against its dot, and compared with what
// the preview was rendered from. Twelve because a default resolved
// through T differs by language while the call does not, and a call
// that only matched in English would be a call that leaves something
// out.
func TestEveryGeneratedCallRendersThePartialsOwnOutput(t *testing.T) {
	var checked int
	for _, locale := range rastrillo.BaseLocales() {
		samples := sampleTree(t, locale)
		calls, err := partialTree(locale)
		if err != nil {
			t.Fatal(err)
		}
		type job struct {
			name, what string
			c          call
			want       string
		}
		var jobs []job
		for _, fam := range families() {
			for _, doc := range fam.Partials {
				for i, s := range doc.States {
					if s.Raw != "" {
						continue
					}
					want, err := renderSample(samples, doc.Name, i, s, locale)
					if err != nil {
						t.Fatalf("%s/%s (%s): %v", locale, doc.Name, s.State, err)
					}
					c, err := callFor(doc.Name, s, locale)
					if err != nil {
						t.Errorf("%s: %v", locale, err)
						continue
					}
					name := fmt.Sprintf("ds-call-%d", len(jobs))
					if _, err := calls.Parse(`{{define "` + name + `"}}` + c.Source + `{{end}}`); err != nil {
						t.Errorf("%s/%s (%s): the call does not parse: %v\n%s", locale, doc.Name, s.State, err, c.Source)
						continue
					}
					jobs = append(jobs, job{name, doc.Name + " (" + s.State + ")", c, string(want)})
				}
			}
		}
		for _, j := range jobs {
			var b strings.Builder
			if err := calls.ExecuteTemplate(&b, j.name, j.c.Dot); err != nil {
				t.Errorf("%s/%s: the call does not run: %v\n%s", locale, j.what, err, j.c.Source)
				continue
			}
			if b.String() != j.want {
				t.Errorf("%s/%s: the call renders different markup from the preview\ncall: %s\n got: %.300s\nwant: %.300s", locale, j.what, j.c.Source, b.String(), j.want)
			}
			checked++
		}
	}
	if checked < 12*50 {
		t.Errorf("only %d calls checked across twelve locales; the sample set has far more", checked)
	}
}

func TestCallForNamesThePartialTheStateAndTheType(t *testing.T) {
	for name, data := range map[string]any{
		"struct":        map[string]any{"V": struct{}{}},
		"pointer":       map[string]any{"V": new(string)},
		"nil":           map[string]any{"V": nil},
		"template.HTML": map[string]any{"V": template.HTML("x")},
	} {
		_, err := callFor("notice", sample{State: "Probe " + name, Data: data}, "en")
		if !errors.Is(err, codeview.ErrUnwritable) {
			t.Errorf("%s: err = %v, want ErrUnwritable", name, err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, "notice") || !strings.Contains(msg, "Probe "+name) {
			t.Errorf("%s: %q does not name the partial and the state", name, msg)
		}
	}
}

// The Keys block order is read off the partials themselves, so a key
// added to a partial's documentation moves its calls on the next render.
func TestThePartialKeyOrderIsReadOffTheKeysBlocks(t *testing.T) {
	keys := partialKeys()
	for partial, want := range map[string][]string{
		"meter":        {"Percent", "Text"},
		"confirm-form": {"Action", "Label", "Danger", "Hidden", "CancelHref", "CancelLabel"},
		"locale-menu":  {"Items", "Return", "Action", "MenuGroup"},
	} {
		if got := keys[partial]; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v, want %v", partial, got, want)
		}
	}
	if got := keys["row-menu"]; len(got) != 3 {
		t.Errorf("row-menu: keys %v, want Name, Items, MenuGroup (the nested item keys are not the partial's)", got)
	}
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod go test -run 'TestEveryGeneratedCall|TestCallFor|TestThePartialKeyOrder' -count=1 ./internal/designsystem/`
Expected: FAIL to compile, `undefined: callFor`, `undefined: call`, `undefined: partialKeys`.

- [ ] **Step 8: Write `internal/designsystem/calls.go`**

```go
package designsystem

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/ui"
)

// call is one sample's template call: the source the Code tab shows,
// and the value . must be when that source runs, which is empty unless
// the sample binds a placeholder.
type call struct {
	Source string
	Dot    map[string]any
}

// callFor is the call that renders one sample, generated from the same
// value the preview was rendered from. The Code tab shows the call
// first because the rendered HTML alone taught the hand-rolling the
// framework forbids, and lost the partial's own logic: meter clamps
// Percent 140 to 100, and HTML copied out of it carries the wrong
// number. An error names the partial, the state and the value's type,
// and fails the build.
func callFor(partial string, s sample, locale string) (call, error) {
	data := s.Data
	if s.Build != nil {
		data = s.Build(locale)
	}
	src, dot, err := codeview.Call(partial, data, partialKeys()[partial], s.Bind)
	if err != nil {
		return call{}, fmt.Errorf("%s (%s): %w", partial, s.State, err)
	}
	return call{Source: src, Dot: dot}, nil
}

var (
	keysOnce sync.Once
	keysByPartial map[string][]string
)

// partialKeys is each partial's keys in the order its own Keys: comment
// block lists them, read off ui's templates once. A call written in
// that order reads like the partial's documentation; a key the block
// does not list follows alphabetically, so either way the order is the
// same on every render.
func partialKeys() map[string][]string {
	keysOnce.Do(func() {
		keysByPartial = map[string][]string{}
		files, err := fs.Glob(ui.Templates(), "*.html")
		if err != nil {
			panic("designsystem: listing ui's templates: " + err.Error())
		}
		for _, name := range files {
			src, err := fs.ReadFile(ui.Templates(), name)
			if err != nil {
				panic("designsystem: reading " + name + ": " + err.Error())
			}
			for partial, keys := range parseKeys(string(src)) {
				keysByPartial[partial] = keys
			}
		}
	})
	return keysByPartial
}

var (
	keysDefine = regexp.MustCompile(`\{\{define "([^"]+)"\}\}`)
	// A key line: one or more capitalised names, comma-separated, then
	// at least two spaces and the type. Continuation lines and prose in
	// the block do not match: they start lower-case, or have one space
	// after their first word.
	keysLine = regexp.MustCompile(`^(\s+)([A-Z][A-Za-z]*(?:, [A-Z][A-Za-z]*)*)\s{2,}\S`)
)

// parseKeys reads one template file's Keys: blocks and gives each to
// the {{define}} that follows it. Only lines at the block's own indent
// count, so row-menu's nested item keys (Label, Href, …) are not taken
// for the partial's.
func parseKeys(src string) map[string][]string {
	out := map[string][]string{}
	var pending []string
	in, indent := false, ""
	for _, line := range strings.Split(src, "\n") {
		if m := keysDefine.FindStringSubmatch(line); m != nil {
			out[m[1]] = pending
			pending, in = nil, false
			continue
		}
		if strings.TrimSpace(line) == "Keys:" {
			pending, in, indent = nil, true, ""
			continue
		}
		if !in {
			continue
		}
		m := keysLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if indent == "" {
			indent = m[1]
		}
		if m[1] != indent {
			continue
		}
		pending = append(pending, strings.Split(m[2], ", ")...)
	}
	return out
}
```

- [ ] **Step 9: Bind locale-menu and rewrite its note**

In `samples.go`, add to `sample` after `Build`:

```go
	// Bind maps a data key to the dot field its call shows instead of
	// the value: locale-menu's Items are []rastrillo.LocaleItem, a value
	// an app gets from rastrillo.LocaleItems(r) and never writes out, so
	// its call reads .Locales. The rest of the data stays literal.
	Bind map[string]string
```

Replace locale-menu's state with:

```go
						{State: "Twelve languages, current highlighted.", Build: localeMenuData,
							Bind: map[string]string{"Items": "Locales"},
							Note: "⟦gallery.locale_menu.note⟧"},
```

Its prose entry is written by the copy tool, which the next step creates.

- [ ] **Step 10: Write the copy tool every copy task runs**

Approved copy reaches the source through one committed, tested tool, so no task carries its own script and the rules are enforced the same way every time. Create `internal/copyedit/copyedit_test.go`:

```go
package copyedit

import (
	"strings"
	"testing"
)

const prose = "package designsystem\n\nvar prose = map[string]map[string]string{\n\t`Old {name}`: {\n\t\t`ga`: `Sean {name}`,\n\t},\n}\n"

func tr(s string) map[string]string {
	out := map[string]string{}
	for _, l := range Locales {
		out[l] = s
	}
	return out
}

func TestApplyProseAddsRemovesAndFormats(t *testing.T) {
	approved := map[string]string{"x.new": "New {name}"}
	got, err := ApplyProse(prose, approved, []string{"Old {name}"}, []Entry{{ID: "x.new", EN: "New {name}", TR: tr("Nua {name}")}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "`Old {name}`") || !strings.Contains(got, "\t`New {name}`: {\n") || !strings.Contains(got, "\t\t`zh-Hans`: `Nua {name}`,") {
		t.Errorf("not applied:\n%s", got)
	}
}

func TestApplyProseRefusesWhatWasNotApproved(t *testing.T) {
	approved := map[string]string{"x.new": "New {name}"}
	bad := map[string]Entry{
		"an id with no approved text":     {ID: "x.other", EN: "New {name}", TR: tr("Nua {name}")},
		"English other than the approved": {ID: "x.new", EN: "Newer {name}", TR: tr("Nua {name}")},
		"an em dash":                      {ID: "x.new", EN: "New {name}", TR: tr("Nua — {name}")},
		"a backtick":                      {ID: "x.new", EN: "New {name}", TR: tr("Nua `{name}`")},
		"a dropped placeholder":           {ID: "x.new", EN: "New {name}", TR: tr("Nua")},
		"a missing locale":                {ID: "x.new", EN: "New {name}", TR: map[string]string{"ga": "Nua {name}"}},
	}
	for why, e := range bad {
		if _, err := ApplyProse(prose, approved, nil, []Entry{e}); err == nil {
			t.Errorf("%s was written", why)
		}
	}
	if _, err := ApplyProse(prose, approved, []string{"Gone"}, nil); err == nil {
		t.Error("removing a key that is not there succeeded")
	}
}

func TestFillReplacesMarkersWithApprovedText(t *testing.T) {
	approved := map[string]string{"b.save": "Save", "b.bad": `Say "hi"`}
	got, n, err := Fill("x := `<b>⟦b.save⟧</b>` + \"⟦b.save⟧\"", approved)
	if err != nil || n != 2 || got != "x := `<b>Save</b>` + \"Save\"" {
		t.Errorf("Fill = %q, %d, %v", got, n, err)
	}
	for _, src := range []string{"⟦b.missing⟧", "⟦b.bad⟧"} {
		if _, _, err := Fill(src, approved); err == nil {
			t.Errorf("Fill(%q) succeeded", src)
		}
	}
}
```

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/copyedit/`
Expected: FAIL to compile, `undefined: ApplyProse`.

Create `internal/copyedit/copyedit.go`:

```go
// Package copyedit writes the operator's approved copy into the
// gallery's source and refuses anything else. Copy is approved by id in
// a copy-review result file; a translation is drafted from that
// English, so a translation whose English moved since is a translation
// of something else, and is refused rather than written.
package copyedit

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Locales are the eleven the gallery translates into; en is the key.
var Locales = []string{"ga", "zh-Hans", "es", "hi", "pt", "bn", "ru", "ja", "yue", "vi", "ar"}

// Entry is one new prose key: its copy-review id, the English its
// translations were drafted from, and the translations.
type Entry struct {
	ID string            `json:"id"`
	EN string            `json:"en"`
	TR map[string]string `json:"tr"`
}

// Edit is one task's edit file.
type Edit struct {
	Approved []string `json:"approved"`
	Prose    string   `json:"prose"`
	Remove   []string `json:"remove"`
	Add      []Entry  `json:"add"`
	Fill     []string `json:"fill"`
}

// LoadApproved reads result files and returns id → approved text. A
// file whose action is not approve, or two files approving one id
// differently, is an error: neither is an approval.
func LoadApproved(paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r struct {
			Action  string `json:"action"`
			Strings []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"strings"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if r.Action != "approve" {
			return nil, fmt.Errorf("%s: action is %q, not approve", p, r.Action)
		}
		for _, s := range r.Strings {
			if was, ok := out[s.ID]; ok && was != s.Text {
				return nil, fmt.Errorf("%s: %s approved twice with different text", p, s.ID)
			}
			out[s.ID] = s.Text
		}
	}
	return out, nil
}

var placeholder = regexp.MustCompile(`\{[a-z]+\}`)

func placeholders(s string) string {
	ps := placeholder.FindAllString(s, -1)
	sort.Strings(ps)
	return strings.Join(ps, " ")
}

// clean is the rule every written string keeps: no em dash, which the
// operator does not want in copy, and no backtick, which would end the
// raw string literal prose.go writes every entry as.
func clean(where, s string) error {
	if strings.Contains(s, "—") || strings.Contains(s, "`") {
		return fmt.Errorf("%s: %q carries an em dash or a backtick", where, s)
	}
	return nil
}

// ApplyProse removes keys from prose.go's source and adds entries before
// the map's closing brace, then formats it.
func ApplyProse(src string, approved map[string]string, remove []string, add []Entry) (string, error) {
	for _, key := range remove {
		start := strings.Index(src, "\t`"+key+"`: {\n")
		if start < 0 {
			return "", fmt.Errorf("prose.go has no key %q to remove", key)
		}
		end := strings.Index(src[start:], "\n\t},\n")
		src = src[:start] + src[start+end+len("\n\t},\n"):]
	}
	var b strings.Builder
	for _, e := range add {
		text, ok := approved[e.ID]
		if !ok {
			return "", fmt.Errorf("%s: no approved text", e.ID)
		}
		if text != e.EN {
			return "", fmt.Errorf("%s: approved %q, but the translations were drafted from %q; redraft them from the approved text", e.ID, text, e.EN)
		}
		if strings.Contains(src, "\t`"+text+"`: {\n") {
			return "", fmt.Errorf("%s: prose.go already has %q", e.ID, text)
		}
		if err := clean(e.ID, text); err != nil {
			return "", err
		}
		if len(e.TR) != len(Locales) {
			return "", fmt.Errorf("%s: %d translations, want %d", e.ID, len(e.TR), len(Locales))
		}
		b.WriteString("\t`" + text + "`: {\n")
		for _, l := range Locales {
			t, ok := e.TR[l]
			if !ok || strings.TrimSpace(t) == "" {
				return "", fmt.Errorf("%s: no %s translation", e.ID, l)
			}
			if err := clean(e.ID+" "+l, t); err != nil {
				return "", err
			}
			if placeholders(t) != placeholders(text) {
				return "", fmt.Errorf("%s %s: placeholders %q, want %q", e.ID, l, placeholders(t), placeholders(text))
			}
			b.WriteString("\t\t`" + l + "`: `" + t + "`,\n")
		}
		b.WriteString("\t},\n")
	}
	tail := strings.LastIndex(src, "\n}\n")
	if tail < 0 {
		return "", fmt.Errorf("prose.go does not end with the map's closing brace")
	}
	out, err := format.Source([]byte(src[:tail+1] + b.String() + src[tail+1:]))
	return string(out), err
}

var marker = regexp.MustCompile(`⟦([a-z0-9_.]+)⟧`)

// Fill replaces every ⟦id⟧ marker in a source file with the approved
// text, and reports how many it replaced. The text lands inside Go
// string literals of either kind, so a quote, a backslash or a backtick
// is refused rather than escaped: copy is written as approved or not
// at all.
func Fill(src string, approved map[string]string) (string, int, error) {
	var err error
	n := 0
	out := marker.ReplaceAllStringFunc(src, func(m string) string {
		id := marker.FindStringSubmatch(m)[1]
		text, ok := approved[id]
		switch {
		case !ok:
			err = fmt.Errorf("%s: no approved text", id)
		case strings.ContainsAny(text, "\"\\"):
			err = fmt.Errorf("%s: %q cannot sit in a Go string literal unescaped", id, text)
		default:
			if e := clean(id, text); e != nil {
				err = e
			}
		}
		n++
		return text
	})
	return out, n, err
}
```

Create `internal/copyedit/run.go`:

```go
//go:build ignore

// run applies one copyedit edit file to the tree:
//
//	GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit edit.json
//
// It is a //go:build ignore program rather than a command package
// because only the copy tasks run it, and it should add no binary to
// the module.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/internal/copyedit"
)

func main() {
	path := flag.String("edit", "", "the edit file")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, "copyedit:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var e copyedit.Edit
	if err := json.Unmarshal(b, &e); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	approved, err := copyedit.LoadApproved(e.Approved)
	if err != nil {
		return err
	}
	if len(e.Remove)+len(e.Add) > 0 {
		if e.Prose == "" {
			e.Prose = "internal/designsystem/prose.go"
		}
		src, err := os.ReadFile(e.Prose)
		if err != nil {
			return err
		}
		out, err := copyedit.ApplyProse(string(src), approved, e.Remove, e.Add)
		if err != nil {
			return err
		}
		if err := os.WriteFile(e.Prose, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Printf("copyedit: prose.go: +%d -%d\n", len(e.Add), len(e.Remove))
	}
	for _, f := range e.Fill {
		src, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		out, n, err := copyedit.Fill(string(src), approved)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		if n == 0 {
			return fmt.Errorf("%s: no ⟦id⟧ marker to fill", f)
		}
		if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
			return err
		}
		fmt.Printf("copyedit: %s: %d filled\n", f, n)
	}
	return nil
}
```

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/copyedit/ && GOFLAGS=-mod=mod go vet ./internal/copyedit/`
Expected: PASS. `go vet ./...` skips `run.go` by its build tag; `make gofmt` still formats it.

- [ ] **Step 11: Rewrite locale-menu's note with the tool**

Write `$TMPDIR/copy-task-4.json` and apply it:

```bash
cat > "$TMPDIR/copy-task-4.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [
  "This one posts to /_locale, which would require a server. The switcher in this page's own header is the links-only version."
 ],
 "add": [
  {
   "id": "gallery.locale_menu.note",
   "en": "This one posts to /_locale, so it needs a server. Its Items come from rastrillo.LocaleItems(r). The switcher in this page's own header is the links-only version.",
   "tr": {
    "ga": "Postálann an ceann seo chuig /_locale, mar sin teastaíonn freastalaí uaidh. Tagann a Items ó rastrillo.LocaleItems(r). Is é an t-athraitheoir i gceanntásc an leathanaigh seo an leagan nach bhfuil ann ach naisc.",
    "zh-Hans": "这一个会 POST 到 /_locale，所以需要服务器。它的 Items 来自 rastrillo.LocaleItems(r)。本页页眉里的切换器是纯链接的版本。",
    "es": "Este envía a /_locale, así que necesita un servidor. Sus Items vienen de rastrillo.LocaleItems(r). El selector de la cabecera de esta página es la versión hecha solo con enlaces.",
    "hi": "यह /_locale पर पोस्ट करता है, इसलिए इसे सर्वर चाहिए। इसके Items rastrillo.LocaleItems(r) से आते हैं। इस पृष्ठ के अपने हेडर वाला स्विचर सिर्फ़ लिंक वाला संस्करण है।",
    "pt": "Este envia para /_locale, por isso precisa de um servidor. Os seus Items vêm de rastrillo.LocaleItems(r). O seletor no cabeçalho desta página é a versão só com ligações.",
    "bn": "এটি /_locale-এ পোস্ট করে, তাই এর একটি সার্ভার লাগে। এর Items আসে rastrillo.LocaleItems(r) থেকে। এই পাতার নিজস্ব হেডারের সুইচারটি কেবল লিংক দিয়ে তৈরি সংস্করণ।",
    "ru": "Этот отправляет POST на /_locale, поэтому ему нужен сервер. Его Items берутся из rastrillo.LocaleItems(r). Переключатель в шапке самой страницы сделан только на ссылках.",
    "ja": "これは /_locale へ POST するので、サーバーが必要です。Items は rastrillo.LocaleItems(r) から取ります。このページ自身のヘッダーにある切り替えは、リンクだけで作った版です。",
    "yue": "呢個會 POST 去 /_locale，所以要有伺服器。佢嘅 Items 嚟自 rastrillo.LocaleItems(r)。本頁自己個頁首嘅切換器係淨係用連結嘅版本。",
    "vi": "Cái này gửi POST tới /_locale, nên cần có máy chủ. Items của nó lấy từ rastrillo.LocaleItems(r). Bộ chuyển trong phần đầu của chính trang này là bản chỉ dùng liên kết.",
    "ar": "هذا يرسل إلى /_locale، لذا يحتاج إلى خادم. تأتي Items الخاصة به من rastrillo.LocaleItems(r). أمّا المبدّل في ترويسة هذه الصفحة فهو النسخة المبنيّة على الروابط وحدها."
   }
  }
 ],
 "fill": [
  "internal/designsystem/samples.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-4.json"
```

Expected: `copyedit: prose.go: +1 -1`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/samples.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/samples.go` prints `0` for each.

- [ ] **Step 12: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/codeview/ ./internal/copyedit/ ./internal/designsystem/`
Expected: PASS, including `TestEveryProseKeyIsTranslated`. If a call does not render the preview's bytes, the spec's rule is that the sample changes, not the test. Report any such sample to the controller with the diff rather than editing the comparison.

- [ ] **Step 13: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 14: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/codeview/call.go \
  internal/codeview/call_test.go \
  internal/copyedit/copyedit.go \
  internal/copyedit/copyedit_test.go \
  internal/copyedit/run.go \
  internal/designsystem/calls.go \
  internal/designsystem/calls_test.go \
  internal/designsystem/samples.go \
  internal/designsystem/prose.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Generate each sample's template call and hold it to the preview

The gallery showed rendered HTML and never the call that produces it,
so it taught readers to hand-roll the markup the framework forbids,
and copying meter's HTML copied a clamped 100 instead of the 140 the
sample passes. Each call is written from the value the preview was
rendered from, in the order the partial's own Keys block documents.
In all twelve locales it must render the partial's bytes exactly.

A struct, pointer, nil or named string type is a build error rather
than a dict guess, because a dict rendering of a struct shows a call
nobody wrote. locale-menu binds Items to .Locales, and its note now
says where that value comes from.

Approved copy is written by a small committed tool, internal/copyedit,
which refuses English other than the approved text and translations
with an em dash, a missing locale or a lost placeholder. Every later
copy change goes through it instead of a script each task carries.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 5: The Code tab: the call first, the rendered HTML formatted under a disclosure, every block highlighted

Spec 1.1 ("the call comes first"), 1.2 (the disclosure, the wrapper line, soft wrap, "only the Code tab's copy is formatted") and 1.3 (colours from theme tokens, contrast gated in Go). Copy: C3 (`gallery.code.rendered`) and C4 (`gallery.code.wrapper`). Tests: "The Code tab never carries the demo wrapper", "The text on screen is the text copied" (the Go half), "Highlight colours pass 4.5:1", and the migrated source-reading gates.

**Files:**
- Modify: `internal/designsystem/page.go`: `previewView` (`Source` becomes `template.HTML`, plus new `Call` and `Wrapper` fields), `newPreview` (takes the frame body and the code separately), `buildFamilies`, `buildIdioms`, `viewTemplate`, and a new `wrapperMarkup`
- Modify: `internal/designsystem/screens.go` (`buildScreens`), `internal/designsystem/formats.go` (`buildFormats`)
- Modify: `internal/designsystem/gallery.css` (the `.ds-src code` rule, the code panel, five colour rules)
- Modify: `internal/designsystem/prose.go` (two keys)
- Test: `internal/designsystem/designsystem_test.go`

**Interfaces:**
- Consumes: `codeview.Format`, `codeview.Highlight` (Task 3), `callFor` (Task 4), `previewSet` and `newPreview` (Task 1).
- Produces:
  - `func newPreview(mount, theme, locale, page, group, title, body, code, id string) (previewView, previewFile)`. `body` is what the frame shows. `code` is the markup the Code tab shows, formatted and highlighted by `newPreview`, and `""` means no Code tab.
  - `previewView.Call template.HTML`, `previewView.Wrapper template.HTML`, `previewView.Source template.HTML`
  - `func wrapperMarkup(w wrapper) string`
  - The Code panel DOM, which Tasks 6, 14 and 16 rely on: `<div class="ds-view__code">`, then optionally `<p class="ds-wrap">…</p>`, then the call `<pre class="ds-src rst-mono"><code>…</code></pre>`, then `<details class="ds-html"><summary>Rendered HTML</summary><pre class="ds-src rst-mono"><code>…</code></pre></details>`. A widget with no call has the single `<pre>`.
  - tests: `var sourceBlock`, `func sourceTexts(fragment string) []string`, `func codePanels(page string) []string`, `func darkHalf(v string) string`

- [ ] **Step 1: Check the approved copy this task writes**

```bash
for id in gallery.code.rendered gallery.code.wrapper; do jq -r --arg id $id '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json; done
```

Expected, in order: `Rendered HTML`, then `Put this inside {wrapper}.` Stop and report if either differs.

- [ ] **Step 2: Write the failing tests**

Append to `designsystem_test.go`:

```go
// sourceBlock finds every source block on a page: the code a reader
// copies, highlighted.
var sourceBlock = regexp.MustCompile(`(?s)<pre class="ds-src[^"]*"[^>]*><code>(.*?)</code></pre>`)

var anyTag = regexp.MustCompile(`<[^>]*>`)

// sourceTexts is every source block in a fragment as a reader sees and
// copies it: the highlighting removed and the entities decoded. Gates
// that look for a route or a tag in the source read this, because
// highlighting splits href="…" into three elements.
func sourceTexts(fragment string) []string {
	var out []string
	for _, m := range sourceBlock.FindAllStringSubmatch(fragment, -1) {
		out = append(out, html.UnescapeString(anyTag.ReplaceAllString(m[1], "")))
	}
	return out
}

var codePanel = regexp.MustCompile(`(?s)<div class="ds-view__code">.*?</div>`)

// codePanels cuts a page into its widgets' Code panels, in page order.
func codePanels(page string) []string { return codePanel.FindAllString(page, -1) }

// The text on screen is the text copied: every Code panel's blocks,
// with the highlighting removed, are exactly the source they stand
// for, in order. That is the call's Source, then the formatted
// rendering, for a partial sample; the formatted markup for a
// hand-written sample, an idiom, a screen or a format; signinSource for
// a sign-in screen. Entities in a sample (the stat band's &euro;) must
// survive as written.
func TestTheTextOnScreenIsTheTextCopied(t *testing.T) {
	files := render(t)
	for _, locale := range []string{"en", "ar"} {
		tmpl := sampleTree(t, locale)
		want := map[string][]string{}
		for _, fam := range families() {
			for _, doc := range fam.Partials {
				for i, s := range doc.States {
					rendered, err := renderSample(tmpl, doc.Name, i, s, locale)
					if err != nil {
						t.Fatal(err)
					}
					if s.Raw == "" {
						c, err := callFor(doc.Name, s, locale)
						if err != nil {
							t.Fatal(err)
						}
						want[fam.Key] = append(want[fam.Key], c.Source)
					}
					want[fam.Key] = append(want[fam.Key], codeview.Format(string(rendered)))
				}
			}
		}
		samples := ui.Styleguide()
		names := make([]string, 0, len(samples))
		for name := range samples {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			want["primitives"] = append(want["primitives"], codeview.Format(samples[name]))
		}
		for _, doc := range formatDocs() {
			want["formats"] = append(want["formats"], codeview.Format(doc.Markup))
		}
		for _, doc := range screenDocs() {
			if doc.Signin != nil {
				want["screens"] = append(want["screens"], signinSource)
			} else {
				want["screens"] = append(want["screens"], codeview.Format(doc.Markup))
			}
		}
		for kind, blocks := range want {
			page := galleryPage(t, files, RootTheme(), locale, kind)
			var got []string
			for _, panel := range codePanels(page) {
				got = append(got, sourceTexts(panel)...)
			}
			if len(got) != len(blocks) {
				t.Errorf("%s/%s: %d source blocks in the Code panels, want %d", locale, kind, len(got), len(blocks))
				continue
			}
			for i := range blocks {
				if got[i] != blocks[i] {
					t.Errorf("%s/%s block %d: on screen\n%s\nwant\n%s", locale, kind, i, got[i], blocks[i])
				}
			}
		}
	}
	if s := strings.Join(sourceTexts(galleryPage(t, files, RootTheme(), "en", "primitives")), "\n"); !strings.Contains(s, "&euro;48,210") {
		t.Error("the stat band's &euro; did not survive as written in its source block")
	}
}

// wrapperPrefixes is the markup wrap puts around a sample for its
// frame. It is the page's container and not the partial's, so it is
// never in the Code tab, and the placeholder action="#" never reaches a
// clipboard.
var wrapperPrefixes = []string{"<section rst-box>", "<div rst-list>", "<div rst-stats>"}

func TestTheCodeTabNeverCarriesTheDemoWrapper(t *testing.T) {
	files := render(t)
	for _, name := range galleryFiles(RootTheme(), "en") {
		page := string(files[name])
		for _, text := range sourceTexts(page) {
			if strings.Contains(text, `action="#"`) {
				t.Errorf("%s: a source block carries the placeholder action=\"#\"", name)
			}
		}
	}
	for _, pk := range componentPages() {
		page := galleryPage(t, files, RootTheme(), "en", pk.Kind)
		for i, panel := range codePanels(page) {
			for _, text := range sourceTexts(panel) {
				for _, w := range wrapperPrefixes {
					if strings.HasPrefix(text, w) {
						t.Errorf("%s panel %d: a source block starts with the demo wrapper %s", pk.Kind, i, w)
					}
				}
			}
		}
	}
	// And the wrapper is said once, above the code, for a wrapped sample.
	form := galleryPage(t, files, RootTheme(), "en", "form")
	if n := strings.Count(form, `<p class="ds-wrap">Put this inside <code>&lt;section rst-box&gt;&lt;form rst-form&gt;</code>.</p>`); n == 0 {
		t.Error("no wrapped sample on Form says which container it goes in")
	}
}

// darkHalf is lightHalf's other side: the second argument of a
// whole-value light-dark(<light>, <dark>), split at the top-level comma.
func darkHalf(v string) string {
	const prefix = "light-dark("
	if !strings.HasPrefix(v, prefix) || !strings.HasSuffix(v, ")") {
		return v
	}
	inner, depth := v[len(prefix):len(v)-1], 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[i+1:])
			}
		}
	}
	return v
}

var highlightRule = regexp.MustCompile(`(?m)^(ds-[tavxs]) \{ color: var\((--rst-[a-z0-9-]+)\); \}`)

// Every highlight colour reaches 4.5:1 against the code block's
// --rst-surface, in every theme and both schemes, resolved from the
// themes' own light-dark() pairs. The axe leg checks the rendered
// cascade; this checks every pair, including the ones a page with no
// such token on it would never show axe.
func TestHighlightColoursPassAgainstTheCodeBackground(t *testing.T) {
	pairs := map[string]string{}
	for _, m := range highlightRule.FindAllStringSubmatch(string(GalleryCSS()), -1) {
		pairs[m[1]] = m[2]
	}
	if len(pairs) != 5 {
		t.Fatalf("gallery.css colours %d highlight elements, want 5: %v", len(pairs), pairs)
	}
	for _, theme := range ui.ThemeNames() {
		raw, _ := ui.ThemeCSS(theme)
		body, err := blockBody(string(raw), ":root {")
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for _, m := range declPattern.FindAllStringSubmatch(body, -1) {
			values[m[1]] = strings.TrimSpace(m[2])
		}
		for scheme, half := range map[string]func(string) string{"light": lightHalf, "dark": darkHalf} {
			bg := half(values["--rst-surface"])
			for el, token := range pairs {
				fg := half(values[token])
				r, err := ui.ContrastRatio(fg, bg)
				if err != nil {
					t.Errorf("%s %s %s (%s on %s): %v", theme, scheme, el, fg, bg, err)
					continue
				}
				if r < 4.5 {
					t.Errorf("%s %s: %s is %s (%s) on %s, %.2f:1, under 4.5:1", theme, scheme, el, token, fg, bg, r)
				}
			}
		}
	}
}
```

Add `"amadan.net/rastrillo/rastrillo/internal/codeview"` to the test file's imports.

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheTextOnScreenIsTheTextCopied|TestTheCodeTabNeverCarriesTheDemoWrapper|TestHighlightColoursPassAgainstTheCodeBackground' -count=1 ./internal/designsystem/`
Expected: FAIL. `0 source blocks in the Code panels`, `a source block starts with the demo wrapper <section rst-box>`, and `gallery.css colours 0 highlight elements, want 5`.

- [ ] **Step 4: Build the Code panel**

In `page.go`, in `previewView` replace `Source string` with:

```go
	// Call is the template call the Code tab leads with, highlighted;
	// empty where the markup is not a partial's (a hand-written sample,
	// an idiom, a screen, a format). Wrapper is the one line above the
	// code naming the container a wrapped sample needs: the frame wears
	// that container, the code does not, so the reader is told rather
	// than handed a placeholder form to delete.
	Call    template.HTML
	Wrapper template.HTML
	// Source is the markup the Code tab shows, formatted and
	// highlighted: under a Rendered HTML disclosure when there is a
	// call, on its own when there is not. Empty means no Code tab.
	Source template.HTML
```

Replace `newPreview` with:

```go
// newPreview is one example's widget and the file its frame loads. body
// is what the frame shows, and its links are deadened there; code is
// what the Code tab shows, and is laid out and highlighted here. The
// two differ for a partial sample, whose frame wears the container the
// partial assumes while its code does not. code empty means no Code
// tab. page is the page's file stem, so the file sits in a directory
// named after the page that frames it.
//
// Only the Code tab's copy is formatted: the frame keeps the bytes as
// rendered, so a measured frame height cannot move because the source
// beside it was laid out for reading.
//
// id is the example's anchor id. Both the height and the width class
// are read off it, because the two tables that size a preview are keyed
// the same way.
func newPreview(mount, theme, locale, page, group, title, body, code, id string) (previewView, previewFile) {
	file := page + "/" + group + ".html"
	view := previewView{
		Group: group,
		Style: previewStyle(id, heightOf(id)),
		Class: previewClass(id),
		Src:   pageHref(mount, theme, locale, file),
		Title: title,
	}
	if code != "" {
		view.Source = codeview.Highlight(codeview.Format(code))
	}
	return view, previewFile{Path: file, Doc: previewDoc(mount, theme, locale, title, deaden(mount, body))}
}

// wrapperMarkup is the opening markup of the container a wrapper puts
// a sample in, as the Code tab names it. method and action are left
// out: they belong to the reader's form, and the gallery's action="#"
// is a placeholder nobody should copy.
func wrapperMarkup(w wrapper) string {
	switch w {
	case wrapList:
		return `<div rst-list>`
	case wrapForm:
		return `<section rst-box><form rst-form>`
	case wrapBox:
		return `<section rst-box>`
	case wrapStats:
		return `<div rst-stats>`
	}
	return ""
}
```

Add `"amadan.net/rastrillo/rastrillo/internal/codeview"` to `page.go`'s imports.

In `buildFamilies`, replace the `preview, file := newPreview(...)` statement with:

```go
				preview, file := newPreview(mount, theme, locale, fam.Key,
					fmt.Sprintf("%s-%d", pv.ID, i),
					previewTitle(locale, doc.Name, s.State),
					wrap(doc.Wrap, string(html)), string(html), pv.ID)
				if s.Raw == "" {
					c, err := callFor(doc.Name, s, locale)
					if err != nil {
						return nil, err
					}
					preview.Call = codeview.Highlight(c.Source)
				}
				if w := wrapperMarkup(doc.Wrap); w != "" {
					preview.Wrapper = proseMarkup(locale, "⟦gallery.code.wrapper⟧",
						"wrapper", template.HTML("<code>"+template.HTMLEscapeString(w)+"</code>"))
				}
```

In `buildIdioms`, pass `samples[name], samples[name]` for `body, code`. In `buildScreens`, pass `doc.Markup, doc.Markup` for a hand-written screen. For a sign-in screen pass `buf.String(), ""`, then replace `view.Preview.Source = signinSource` with `view.Preview.Source = codeview.Highlight(signinSource)`. In `buildFormats`, pass `doc.Markup, doc.Markup`. Add the `codeview` import to `screens.go` only, which calls `codeview.Highlight` for the sign-in source. `formats.go` gets none: its highlighting happens inside `newPreview`, and Go rejects an unused import.

In `viewTemplate`, replace the line `{{if .Source}}<pre class="ds-src ds-view__code rst-mono"><code>{{.Source}}</code></pre>{{end}}` with:

```
{{if .Source}}<div class="ds-view__code">
{{if .Wrapper}}<p class="ds-wrap">{{.Wrapper}}</p>
{{end}}{{if .Call}}<pre class="ds-src rst-mono"><code>{{.Call}}</code></pre>
<details class="ds-html"><summary>{{P "⟦gallery.code.rendered⟧"}}</summary><pre class="ds-src rst-mono"><code>{{.Source}}</code></pre></details>
{{else}}<pre class="ds-src rst-mono"><code>{{.Source}}</code></pre>
{{end}}</div>{{end}}
```

- [ ] **Step 5: Style it**

In `gallery.css`, replace `.ds-src code { white-space: pre; }` with:

```css
/* Source soft-wraps rather than scrolling sideways. An inline run (a
   meter's whole output, a choice card) is one line however long,
   because breaking inside it would change the space a reader sees;
   so the block wraps it on screen, and copying takes textContent,
   which has no soft breaks in it. */
.ds-src code { overflow-wrap: anywhere; white-space: pre-wrap; }
/* The five token colours, from theme tokens so they follow theme and
   scheme. TestHighlightColoursPassAgainstTheCodeBackground holds each
   to 4.5:1 on --rst-surface, the block's background, in every theme
   and both schemes; it reads these lines, one rule per element. */
ds-t { color: var(--rst-accent); }
ds-a { color: var(--rst-tone-negative-fg); }
ds-v { color: var(--rst-tone-positive-fg); }
ds-x { color: var(--rst-text-muted); }
ds-s { color: var(--rst-tone-warning-fg); }
.ds-wrap { color: var(--rst-text-muted); font-size: var(--rst-fs-sm); margin: 0 0 var(--rst-sp-2); }
.ds-html { margin-block-start: var(--rst-sp-2); }
.ds-html > summary { color: var(--rst-text-muted); cursor: pointer; font-size: var(--rst-fs-sm); }
.ds-html > summary:focus-visible { outline: 2px solid var(--rst-accent); outline-offset: 2px; }
```

The existing `.ds-view__code { display: none; margin-block-start: 0; }` and the rule that shows it now apply to the panel `div`. Leave them unchanged.

- [ ] **Step 6: Add the two prose keys**

Write `$TMPDIR/copy-task-5.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-5.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [],
 "add": [
  {
   "id": "gallery.code.rendered",
   "en": "Rendered HTML",
   "tr": {
    "ga": "HTML rindreáilte",
    "zh-Hans": "渲染出的 HTML",
    "es": "HTML generado",
    "hi": "रेंडर हुआ HTML",
    "pt": "HTML gerado",
    "bn": "রেন্ডার হওয়া HTML",
    "ru": "Итоговый HTML",
    "ja": "レンダリング後の HTML",
    "yue": "渲染出嚟嘅 HTML",
    "vi": "HTML được tạo ra",
    "ar": "HTML الناتج"
   }
  },
  {
   "id": "gallery.code.wrapper",
   "en": "Put this inside {wrapper}.",
   "tr": {
    "ga": "Cuir é seo taobh istigh de {wrapper}.",
    "zh-Hans": "把这段放进 {wrapper} 里。",
    "es": "Pon esto dentro de {wrapper}.",
    "hi": "इसे {wrapper} के अंदर रखें।",
    "pt": "Coloque isto dentro de {wrapper}.",
    "bn": "এটি {wrapper}-এর ভেতরে রাখুন।",
    "ru": "Поместите это внутрь {wrapper}.",
    "ja": "これは {wrapper} の中に置きます。",
    "yue": "將呢段放入 {wrapper} 入面。",
    "vi": "Đặt đoạn này bên trong {wrapper}.",
    "ar": "ضع هذا داخل {wrapper}."
   }
  }
 ],
 "fill": [
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-5.json"
```

Expected: `copyedit: prose.go: +2 -0`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 7: Migrate the gates that read source**

In `designsystem_test.go`:

(a) `TestEveryExampleIsFramedDesktopMobileAndCode`: replace the `if strings.Contains(w, "ds-view__tab--c") { … }` block with a check by source kind:

```go
			if strings.Contains(w, "ds-view__tab--c") {
				withCode++
				pres := strings.Count(w, `<pre class="ds-src`)
				switch disclosures := strings.Count(w, `<details class="ds-html">`); {
				case !strings.Contains(w, `<div class="ds-view__code">`):
					t.Errorf("%s widget %d offers a Code tab with no source behind it", name, i)
				case disclosures == 1:
					// A partial sample: its call, then its rendering.
					withCall++
					if pres != 2 || !strings.Contains(w, `<code><ds-x>{{</ds-x><ds-x>template</ds-x>`) {
						t.Errorf("%s widget %d: a Rendered HTML disclosure without a call before it (%d blocks)", name, i, pres)
					}
				case disclosures == 0 && pres != 1:
					t.Errorf("%s widget %d: %d source blocks and no call; markup with no call is one block", name, i, pres)
				}
			}
```

Declare `var withCode, withCall int` in place of `var withCode int`. After the widget loop, add:

```go
		// One call per partial sample that is not hand-written, and none
		// anywhere else: the families say how many each page owes.
		wantCalls := 0
		for _, fam := range families() {
			if strings.HasSuffix(name, "/"+fam.Key+".html") {
				for _, doc := range fam.Partials {
					for _, s := range doc.States {
						if s.Raw == "" {
							wantCalls++
						}
					}
				}
			}
		}
		if withCall != wantCalls {
			t.Errorf("%s: %d widgets lead with a call, want %d (every partial sample with data)", name, withCall, wantCalls)
		}
```

(b) `TestSampleLinksAndFormsAreDeadInThePreviews`: replace the `kept` loop with:

```go
	var kept bool
	for _, pk := range componentPages() {
		for _, text := range sourceTexts(galleryPage(t, files, RootTheme(), "en", pk.Kind)) {
			kept = kept || strings.Contains(text, `href="/posts/1/edit"`)
		}
	}
```

(c) `TestNoGalleryPageOpensAModalOverTheGallery`: replace the escaped-source check with:

```go
			if !strings.Contains(strings.Join(sourceTexts(page), "\n"), `<div rst-modal-overlay>`) {
				t.Errorf("%s/%s/%s does not show the modal sample as source", theme, locale, fileOf("primitives"))
			}
```

and in its doc comment replace the paragraph from `The cure is the shells'` through `would actually lay the overlay out.` with: `The cure is the shells': the source in a <pre>, highlighted and therefore escaped, with the markup live at its own URL. In a source block the < that opens the tag is &lt;, so <div rst-modal-overlay occurs only where a browser would lay the overlay out.`

(d) `TestNoPageCarriesTheSameIdTwice`: change the call to `uniqueIDs(t, name, escapedSource(page), !preview)` and add above it:

```go
		// Ids are read with the source blocks cut out: whether the
		// highlighting happens to split an id="…" a sample writes must
		// not decide what counts as an element's id.
```

`escapedSource` already strips every `<pre class="ds-src` block. The call block, the disclosure's block and the snippet blocks all keep that prefix.

- [ ] **Step 8: Run the tests and weigh the pages**

```bash
set -o pipefail; mkdir -p "$TMPDIR/gate"
GOFLAGS=-mod=mod go test -run TestEveryPageStaysUnderItsBudget -v -count=1 ./internal/designsystem/ 2>&1 | tee "$TMPDIR/gate/task5-budget.log"; echo "exit $?"
grep 'heaviest page' "$TMPDIR/gate/task5-budget.log"
GOFLAGS=-mod=mod go run ./cmd/dsgen -out "$TMPDIR/ds" > "$TMPDIR/gate/task5-dsgen.log" 2>&1; echo "dsgen exit $?"
wc -c "$TMPDIR"/ds/*/*/form.html "$TMPDIR"/ds/*/*/date-and-time.html | sort -n | tail -3; rm -rf "$TMPDIR/ds"
```

Expected: `exit 0` with a heaviest page well under 131,072 bytes, `dsgen exit 0`, and then the three largest of the two pages the spec watches. A size read after a non-zero exit is not a measurement.

If the heaviest `form.html` exceeds 125,952 bytes, within 5 KB of the cap, **stop and report to the controller** with the number. The spec names dropping attribute-value highlighting as the first cut. That changes what Task 14's axe leg asserts, so the operator orders it rather than the implementer.

- [ ] **Step 9: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green. `TestPreviewWidgetDrivesTheWholeJourney` reads `.ds-view__code` by class, which the panel `div` keeps.

- [ ] **Step 10: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/page.go \
  internal/designsystem/screens.go \
  internal/designsystem/formats.go \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Lead every Code tab with the template call, and lay out the HTML

The Code tab now shows the call a reader should paste, then the
rendered HTML under a disclosure. The HTML is broken at block-level
tags and coloured with theme tokens. The demo's own wrapper is no
longer glued to the front of the source: a wrapped sample says which
container it goes in, in one line above the code, so action=\"#\"
never reaches a clipboard.

The gates that looked for routes and tags in escaped source now read
the text a reader sees, because highlighting splits an attribute into
three elements. A Go test holds every highlight colour to 4.5:1 on the
code background in each theme and scheme.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 6: A copy button on every source block

Spec 1.4. Copy: C5 (`gallery.copy.button`), C6 (`gallery.copy.done`), C7 (`gallery.copy.failed`), C17 (`gallery.copy.name`, the ", " joiner). Tests: the Copy browser leg, the Go check on `data-ds-nocopy`, and the raised `gallery.js` cap.

**Files:**
- Modify: `internal/designsystem/samples.go`: `sample` gains `Illustration`, and form-foot's "Working" state sets it
- Modify: `internal/designsystem/page.go`: `previewView.NoCopy`, `buildFamilies`, `viewTemplate` (the single `<pre>`), `pageTemplate` (the live region), `pageView.CopyJoin`, `const copyJoin`
- Modify: `internal/designsystem/gallery.js` (the header list and a Copy section), `internal/designsystem/gallery.css` (`.ds-copy`)
- Modify: `internal/designsystem/prose.go` (three keys)
- Test: `internal/designsystem/designsystem_test.go` (`TestGalleryScriptStaysInertAndFirstParty`'s cap and a new nocopy gate); Create: `internal/designsystem/code_browser_test.go`; Modify: `internal/designsystem/galleryrig/rig.go`, `internal/designsystem/rig_browser_test.go`

**Interfaces:**
- Consumes: Task 5's Code panel DOM.
- Produces:
  - `sample.Illustration bool`, `previewView.NoCopy bool`, `const copyJoin = ", "`
  - The live region `<p class="rst-sr-only" role="status" data-ds-copy-status data-copy="…" data-copied="…" data-failed="…" data-join=", "></p>`, one per page, at the end of `[rst-page]`
  - `button.ds-copy`, inserted by `gallery.js` immediately before each `pre.ds-src:not([data-ds-nocopy])`, with the text node label and a `span.rst-sr-only` name suffix
  - In `galleryrig`: `func AddInit(js string) chromedp.Action`, `func Until(t *testing.T, ctx context.Context, where, expr string)`; in `rig_browser_test.go` their short names `addInit` and `until`. Tasks 12 to 15 use them.
  - In `code_browser_test.go`: `const clipboardStub`, `const noClipboard`.

- [ ] **Step 1: Check the approved copy this task writes**

```bash
for id in gallery.copy.button gallery.copy.done gallery.copy.failed gallery.copy.name; do jq -r --arg id $id '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json; done
```

Expected: `Copy`, `Copied`, `Copy failed. Select the code and copy it yourself.`, `Copy field-text, Required`. The last one fixes the joiner between the section and the state as `, ` and the space after the visible label. Stop and report if any differs.

- [ ] **Step 2: Write the failing tests**

Two helpers go into `galleryrig`, because the sweep package needs them too. Append to `internal/designsystem/galleryrig/rig.go` (add `"time"` and `"github.com/chromedp/cdproto/page"` to its imports):

```go
// AddInit runs js in every document the tab loads, before the page's
// own scripts: the only way to stand in for an API a script reads at
// DOMContentLoaded, such as the clipboard or a storage that throws.
func AddInit(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(js).Do(ctx)
		return err
	})
}

// Until polls expr until it is true, for ten seconds, and fails the leg
// naming it otherwise. Polling rather than waiting on an event, because
// what these legs wait for (a promise settling, a timer, a page restored
// from the back/forward cache) raises nothing chromedp can wait on. An
// evaluation that lands while one document is being swapped for the
// next fails, and the next poll reads the new page; so an error is
// retried, and one that lasts to the deadline fails the leg by name, as
// a false would.
func Until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for {
		var ok bool
		err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok))
		if err == nil && ok {
			return
		}
		if err != nil {
			last = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: never true within 10s (last error: %v): %s", where, last, expr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
```

and to `internal/designsystem/rig_browser_test.go` (add the `chromedp` import):

```go
func addInit(js string) chromedp.Action { return galleryrig.AddInit(js) }

func until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	galleryrig.Until(t, ctx, where, expr)
}
```

Create `internal/designsystem/code_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// clipboardStub replaces the clipboard with one the leg controls:
// every write is recorded, and window.__clipMode decides whether it
// resolves or rejects. A headless engine's own clipboard needs a
// permission the leg cannot grant per call, and would test the engine.
const clipboardStub = `window.__clipCalls = []; window.__clipMode = "resolve";
Object.defineProperty(Navigator.prototype, "clipboard", {configurable: true, get() { return {
  writeText: s => { window.__clipCalls.push(s); return window.__clipMode === "resolve" ? Promise.resolve() : Promise.reject(new DOMException("denied", "NotAllowedError")); }
}; }});`

// noClipboard is a plain-HTTP origin's clipboard: not there at all.
const noClipboard = `Object.defineProperty(Navigator.prototype, "clipboard", {configurable: true, get() { return undefined; }});`

func TestTheCopyButtonCopiesAnnouncesAndFailsSafely(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("form"))

	if err := chromedp.Run(ctx,
		addInit(clipboardStub),
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(form),
		chromedp.WaitReady(`.ds-copy`, chromedp.ByQuery),
		// Show the first widget's code, so the selection the failure
		// makes is a selection a reader could see.
		chromedp.Evaluate(`document.querySelector(".ds-view__tab--c input").click(); true`, nil),
	); err != nil {
		t.Fatalf("loading Form with a stubbed clipboard: %v", err)
	}

	// Every block a reader can copy has a button, and every button's
	// accessible name is unique on the page and begins with its label.
	assertNames(t, ctx, "form")

	// A resolved write: the clipboard gets exactly the block's text, the
	// region says Copied, and the label says it too.
	const first = `document.querySelector(".ds-view__code .ds-copy")`
	if err := chromedp.Run(ctx, chromedp.Evaluate(first+`.click(); true`, nil)); err != nil {
		t.Fatalf("clicking Copy: %v", err)
	}
	until(t, ctx, "a resolved copy", `document.querySelector("[data-ds-copy-status]").textContent === document.querySelector("[data-ds-copy-status]").dataset.copied`)
	var got struct {
		Calls []string
		Text  string
		Label string
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({Calls: window.__clipCalls,
	  Text: `+first+`.nextElementSibling.querySelector("code").textContent,
	  Label: `+first+`.firstChild.nodeValue})`, &raw)); err != nil {
		t.Fatalf("reading the copy: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if len(got.Calls) != 1 || got.Calls[0] != got.Text {
		t.Errorf("the clipboard got %q, want exactly the block's text %q", got.Calls, got.Text)
	}
	if !strings.HasPrefix(got.Text, "{{template ") {
		t.Errorf("the first block on Form copied %q; it should be the call", got.Text)
	}
	if got.Label != proseIn("en", "Copied") {
		t.Errorf("the button reads %q after a copy, want %q", got.Label, proseIn("en", "Copied"))
	}
	until(t, ctx, "the label returns", first+`.firstChild.nodeValue === document.querySelector("[data-ds-copy-status]").dataset.copy`)

	// A rejected write: the failure is announced and the block's text
	// is selected, so Ctrl or Cmd+C works at once. Nothing throws.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__clipMode = "reject"; window.__errors = 0;
	  addEventListener("error", () => window.__errors++); addEventListener("unhandledrejection", () => window.__errors++);
	  `+first+`.click(); true`, nil)); err != nil {
		t.Fatalf("clicking Copy with a refusing clipboard: %v", err)
	}
	until(t, ctx, "a refused copy", `document.querySelector("[data-ds-copy-status]").textContent === document.querySelector("[data-ds-copy-status]").dataset.failed`)
	until(t, ctx, "the block selected", `getSelection().toString() === `+first+`.nextElementSibling.querySelector("code").textContent && window.__errors === 0`)

	// Display has the most blocks that share a heading: names must still
	// be unique there.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("display"))),
		chromedp.WaitReady(`.ds-copy`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("loading Display: %v", err)
	}
	assertNames(t, ctx, "display")

	// No clipboard API, no button: a control that cannot work is not
	// drawn, and the <pre> stays selectable.
	tab, closeTab := chromedp.NewContext(rig.Context())
	defer closeTab()
	var buttons, blocks int
	if err := chromedp.Run(tab,
		addInit(noClipboard),
		chromedp.Navigate(form),
		chromedp.WaitReady(`[data-ds-copy-status]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll(".ds-copy").length`, &buttons),
		chromedp.Evaluate(`document.querySelectorAll("pre.ds-src").length`, &blocks),
	); err != nil {
		t.Fatalf("loading Form with no clipboard: %v", err)
	}
	if buttons != 0 || blocks == 0 {
		t.Errorf("with no clipboard API: %d buttons over %d blocks, want none over some", buttons, blocks)
	}
}

// assertNames holds one page's copy buttons to the two promises: one
// per copyable block, and names unique, each starting with the visible
// label so a voice user saying "Copy" reaches them (label in name).
func assertNames(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
	  Names: [...document.querySelectorAll(".ds-copy")].map(b => b.textContent),
	  Blocks: document.querySelectorAll("pre.ds-src:not([data-ds-nocopy])").length,
	  Label: document.querySelector("[data-ds-copy-status]").dataset.copy})`, &raw)); err != nil {
		t.Fatalf("%s: reading the copy buttons: %v", where, err)
	}
	var got struct {
		Names  []string
		Blocks int
		Label  string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	if len(got.Names) == 0 || len(got.Names) != got.Blocks {
		t.Fatalf("%s: %d copy buttons for %d copyable blocks", where, len(got.Names), got.Blocks)
	}
	seen := map[string]bool{}
	for _, n := range got.Names {
		if seen[n] {
			t.Errorf("%s: two copy buttons are both named %q", where, n)
		}
		seen[n] = true
		if !strings.HasPrefix(n, got.Label+" ") {
			t.Errorf("%s: %q does not start with the visible label %q", where, n, got.Label)
		}
	}
}
```

Append to `designsystem_test.go`:

```go
// A block marked data-ds-nocopy is an illustration, what a script
// writes at run time and not markup to paste, and it is the only kind
// of block that gets no copy button. So it may sit only on a sample
// marked Illustration, which has no call.
func TestOnlyAnIllustrationRefusesTheCopyButton(t *testing.T) {
	files := render(t)
	for _, pk := range componentPages() {
		page := galleryPage(t, files, RootTheme(), "en", pk.Kind)
		want := 0
		for _, fam := range families() {
			if fam.Key != pk.Kind {
				continue
			}
			for _, doc := range fam.Partials {
				for _, s := range doc.States {
					if s.Illustration {
						want++
						if s.Raw == "" {
							t.Errorf("%s (%s) is an illustration with data; an illustration is markup no call produces", doc.Name, s.State)
						}
					}
				}
			}
		}
		if got := strings.Count(page, `<pre class="ds-src rst-mono" data-ds-nocopy>`); got != want {
			t.Errorf("%s: %d blocks refuse a copy button, want %d", pk.Kind, got, want)
		}
		for _, panel := range codePanels(page) {
			if strings.Contains(panel, "data-ds-nocopy") && strings.Contains(panel, `<details class="ds-html">`) {
				t.Errorf("%s: an illustration's panel carries a call", pk.Kind)
			}
		}
	}
	if !strings.Contains(galleryPage(t, files, RootTheme(), "en", "form"), "data-ds-nocopy") {
		t.Error("form-foot's Working state is not marked as an illustration")
	}
}
```

In `TestGalleryScriptStaysInertAndFirstParty`, replace the comment block and the check that begins `// 8 KiB was the budget` with:

```go
	// 22,832 bytes: the whole script the gallery plans, weighed by
	// applying each feature's code to the file in turn, plus 10%. The
	// first ceilings (8 KiB, then 10 KiB, outgrown with 32 bytes to
	// spare) and a later estimate of 15 KiB all undercounted the code
	// and the comments that say why it is the way it is; a ceiling that
	// tight buys cut comments rather than less code. It is the docs
	// site's own script, never shipped to an app, loaded once and then
	// cached. Measured running totals, each feature's addition beside it:
	//
	//   the colour scheme, its frames and the rail filter      9,553
	//   copy buttons                                   +2,955 12,508
	if n := len(js); n > 22832 {
		t.Errorf("gallery.js is %d bytes; it is the gallery's own furniture and should stay readable in one sitting", n)
	}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run TestOnlyAnIllustrationRefusesTheCopyButton -count=1 ./internal/designsystem/`
Expected: FAIL to compile, `s.Illustration undefined`.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestTheCopyButtonCopiesAnnouncesAndFailsSafely -count=1 ./internal/designsystem/`
Expected: FAIL (after the Go half compiles), timing out on `WaitReady .ds-copy`.

- [ ] **Step 4: Mark the illustration and render the live region**

In `samples.go`, add to `sample` after `Bind`:

```go
	// Illustration marks a state that pictures what a script writes at
	// run time (form-foot's Working state, rastrillo.js mid-submit), not
	// markup an app pastes. Its block gets no copy button; its State
	// label already says what it is.
	Illustration bool
```

and add `Illustration: true,` to form-foot's `Working — what rastrillo.js writes on the way out` state.

In `page.go`, add `NoCopy bool // the sample is an Illustration: no copy button` to `previewView`. In `buildFamilies` set `preview.NoCopy = s.Illustration` beside the call. In `viewTemplate`, change the single-block branch to:

```
{{else}}<pre class="ds-src rst-mono"{{if .NoCopy}} data-ds-nocopy{{end}}><code>{{.Source}}</code></pre>
```

Add, near `previewTitle`:

```go
// copyJoin joins the parts of a copy button's accessible name, the
// section and then the state: "Copy field-text, Required". It is the
// joiner copy review approved, and a separator rather than words, so
// it is one constant and not a prose key.
const copyJoin = ", "
```

Add `CopyJoin string` to `pageView`, with the comment `// CopyJoin is copyJoin, handed to gallery.js on the live region so the script writes no punctuation of its own.` Set `CopyJoin: copyJoin,` in `renderGallery`'s `base`. In `pageTemplate`, after the `<nav class="ds-updown" …>` line and before `</div>\n</main>`, add:

```
<p class="rst-sr-only" role="status" data-ds-copy-status data-copy="{{P "⟦gallery.copy.button⟧"}}" data-copied="{{P "⟦gallery.copy.done⟧"}}" data-failed="{{P "⟦gallery.copy.failed⟧"}}" data-join="{{.CopyJoin}}"></p>
```

- [ ] **Step 5: Add the three prose keys**

Write `$TMPDIR/copy-task-6.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-6.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [],
 "add": [
  {
   "id": "gallery.copy.button",
   "en": "Copy",
   "tr": {
    "ga": "Cóipeáil",
    "zh-Hans": "复制",
    "es": "Copiar",
    "hi": "कॉपी करें",
    "pt": "Copiar",
    "bn": "কপি করুন",
    "ru": "Копировать",
    "ja": "コピー",
    "yue": "複製",
    "vi": "Sao chép",
    "ar": "نسخ"
   }
  },
  {
   "id": "gallery.copy.done",
   "en": "Copied",
   "tr": {
    "ga": "Cóipeáilte",
    "zh-Hans": "已复制",
    "es": "Copiado",
    "hi": "कॉपी हो गया",
    "pt": "Copiado",
    "bn": "কপি হয়েছে",
    "ru": "Скопировано",
    "ja": "コピーしました",
    "yue": "複製咗",
    "vi": "Đã sao chép",
    "ar": "تم النسخ"
   }
  },
  {
   "id": "gallery.copy.failed",
   "en": "Copy failed. Select the code and copy it yourself.",
   "tr": {
    "ga": "Theip ar an gcóipeáil. Roghnaigh an cód agus cóipeáil é tú féin.",
    "zh-Hans": "复制失败。请选中代码，自己复制。",
    "es": "No se pudo copiar. Selecciona el código y cópialo tú.",
    "hi": "कॉपी नहीं हो सका। कोड चुनें और खुद कॉपी करें।",
    "pt": "Não foi possível copiar. Selecione o código e copie-o você.",
    "bn": "কপি করা যায়নি। কোডটি বেছে নিয়ে নিজে কপি করুন।",
    "ru": "Не удалось скопировать. Выделите код и скопируйте его сами.",
    "ja": "コピーできませんでした。コードを選択して、ご自身でコピーしてください。",
    "yue": "複製唔到。揀咗段代碼，自己複製啦。",
    "vi": "Không sao chép được. Hãy chọn đoạn mã và tự sao chép.",
    "ar": "تعذّر النسخ. حدّد الشيفرة وانسخها بنفسك."
   }
  }
 ],
 "fill": [
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-6.json"
```

Expected: `copyedit: prose.go: +3 -0`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 6: Draw the buttons in `gallery.js`**

In the header comment, replace the list from `What it adds:` through `iframes the examples are drawn in` with a list that later features each add one line to:

```
   a complete document with scripts off. What it adds, a section each:
   - the colour scheme: data-theme on <html>, remembered as
     rst-ds-scheme, and painted into every preview frame;
   - the rail filter;
   - copy buttons.
```

Before the closing `})();`, add:

```js
  // ── Copy ────────────────────────────────────────────────────────────
  //
  // A button on every source block, drawn only where the clipboard API
  // exists. On a plain-HTTP origin (a tailnet address) it does not, and
  // a button that cannot work is worse than a <pre> a reader can
  // select. The words are the page's, read off the live region it
  // renders in its own language; a button's name is built from text
  // already on screen, so the server writes nothing per block.
  ready(function () {
    var region = document.querySelector("[data-ds-copy-status]");
    if (!region || !navigator.clipboard || !navigator.clipboard.writeText) return;
    var d = region.dataset, pres = document.querySelectorAll("pre.ds-src:not([data-ds-nocopy])");

    // The name after the label: the disclosure's summary when the block
    // is in one, the section (its anchored heading, or on a page of
    // prose the last heading before it), and the nearest state label
    // above. Labels are unique within a section, so names are unique
    // wherever the labels are.
    function name(pre) {
      var sec = pre.closest("[data-ds-anchor]"), h = sec && sec.querySelector("h1, h2, h3, h4");
      var all = document.querySelectorAll("main :is(h1, h2, h3, h4)"), sample = pre.closest(".ds-sample");
      if (!h) for (var i = 0; i < all.length && all[i].compareDocumentPosition(pre) & 4; i++) h = all[i];
      for (var s = pre.previousElementSibling; s && !s.matches(".ds-state"); s = s.previousElementSibling);
      s = s || sample && sample.querySelector(":scope > .ds-state");
      var html = pre.closest("details.ds-html");
      return [html && html.querySelector("summary"), h, s].filter(Boolean).map(function (e) {
        return e.textContent.trim();
      }).join(d.join);
    }

    // Emptied and written a beat later, so Copied twice in a row is
    // still announced.
    function say(text) {
      region.textContent = "";
      setTimeout(function () { region.textContent = text; }, 50);
    }

    for (var i = 0; i < pres.length; i++) (function (pre) {
      var code = pre.querySelector("code") || pre, b = document.createElement("button");
      var label = document.createTextNode(d.copy), suffix = document.createElement("span"), timer;
      b.type = "button";
      b.className = "ds-copy";
      suffix.className = "rst-sr-only";
      suffix.textContent = " " + name(pre);
      b.append(label, suffix);
      pre.before(b);
      // Copied is said only once the write resolves. A refusal (a
      // permission, lost focus, a policy) or a throw is announced as a
      // failure and the block selected, so Ctrl or Cmd+C works at once.
      b.addEventListener("click", function () {
        new Promise(function (ok) { ok(navigator.clipboard.writeText(code.textContent)); }).then(function () {
          say(d.copied);
          label.data = d.copied;
          clearTimeout(timer);
          timer = setTimeout(function () { label.data = d.copy; }, 2000);
        }, function () {
          say(d.failed);
          getSelection().selectAllChildren(code);
        });
      });
    })(pres[i]);
  });
```

(`compareDocumentPosition(pre) & 4` is "the block comes after this heading". The loop stops at the first heading that does not precede the block, so it keeps the last one that does.)

- [ ] **Step 7: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/ && RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestTheCopyButtonCopiesAnnouncesAndFailsSafely|TestA11yWalksTheKeyboard|TestA11yScansTheGallery' -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS. The keyboard walk now passes through copy buttons. They are real buttons with a visible focus ring, so the walk's ring check holds. `wc -c internal/designsystem/gallery.js` prints 12,508. If it is more than 200 bytes over, the task has added something the cap's table did not weigh; say so in the report.

- [ ] **Step 8: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 9: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/samples.go \
  internal/designsystem/page.go \
  internal/designsystem/gallery.js \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/code_browser_test.go \
  internal/designsystem/rig_browser_test.go \
  internal/designsystem/galleryrig/rig.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Add a copy button to every source block

Copying a call meant selecting a soft-wrapped block by hand. The
button copies exactly the block's text and says Copied only once the
write resolves. If the write fails, it says so and selects the block,
so Ctrl or Cmd+C works at once. With no clipboard API, as on a
plain-HTTP origin, no button is drawn.

Names are built in the script from the heading and state label
already on the page, so the server writes no per-block attribute.
Repeating a translated name on every block would have cost several
KB on Form. form-foot's Working state is an illustration of what a
script writes, and gets no button. gallery.js's cap is 22,832 bytes:
the whole planned script, measured, plus 10%, because every estimate
undercounted it and a tight ceiling buys cut comments.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 7: Plain prose: two sentences in place of the preamble, and Getting started says attributes

Spec 2.8: the intros, no callout on Shells, Getting started's lead and its `<link>` snippet, the inventory rows these touch, and the first leak-gate sentinel. Copy: C1 (`gallery.intro.live`), C2 (`gallery.intro.code`), C12 (`gallery.start.without`), C13 (`gallery.start.link_lead`).

**Files:**
- Modify: `internal/designsystem/page.go`: delete `deadLinkCallout` (2413-2418); edit `familyBody` (2548-2552), `primitivesBody` (2572), `shellsBody` (2588), `gettingStartedBody` (the "Using it without the framework" section); add `assetsView.Links` and set it in `buildAssets`
- Modify: `internal/designsystem/formats.go` (`formatsBody`), `internal/designsystem/screens.go` (`screensBody`)
- Modify: `internal/designsystem/prose.go` (4 keys in, 7 out)
- Test: `internal/designsystem/designsystem_test.go` (`proseSentinels`, a new intro gate)

**Interfaces:**
- Consumes: `codeview.Highlight` (Task 3).
- Produces: `assetsView.Links template.HTML`. The sentinel list becomes `"An overview of everything the design system provides."`, `"Each example is live, but its links go nowhere."` and `"Screens stack vertically"`. Task 9 replaces the first.

- [ ] **Step 1: Check the approved copy this task writes**

```bash
for id in gallery.intro.live gallery.intro.code gallery.start.without gallery.start.link_lead; do jq -r --arg id $id '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json; done
```

Expected: `Each example is live, but its links go nowhere.`, `Code shows the template call to copy.`, `The names above are links. Take tokens.css and one theme and you have the whole visual system: attributes on ordinary HTML, no build step.`, `Link them in this order, tokens.css first:`. Stop and report if any differs.

- [ ] **Step 2: Write the failing test**

Append to `designsystem_test.go`:

```go
// Every page that frames examples opens by saying, once, that the
// examples are live and their links go nowhere; the five component
// pages add that Code holds the call to copy. That replaced a callout
// and four notes saying the same at length, and Shells, which has no
// Code tab and frames whole pages of the tree, says neither.
func TestTheFramedPagesOpenWithTheTwoPlainSentences(t *testing.T) {
	files := render(t)
	live, code := proseIn("en", "Each example is live, but its links go nowhere."), proseIn("en", "Code shows the template call to copy.")
	components := map[string]bool{}
	for _, pk := range componentPages() {
		components[pk.Kind] = true
	}
	for _, pk := range pageKinds() {
		page := galleryPage(t, files, RootTheme(), "en", pk.Kind)
		framed := components[pk.Kind] || pk.Kind == "primitives" || pk.Kind == "formats" || pk.Kind == "screens"
		if got := strings.Count(page, live); framed != (got == 1) || got > 1 {
			t.Errorf("%s says %q %d times, want %d", pk.Kind, live, got, map[bool]int{true: 1}[framed])
		}
		if got := strings.Count(page, code); components[pk.Kind] != (got == 1) || got > 1 {
			t.Errorf("%s says %q %d times, want %d", pk.Kind, code, got, map[bool]int{true: 1}[components[pk.Kind]])
		}
		// <div rst-callout is a live callout: in a source block the < is &lt;.
		if strings.Contains(page, `<div rst-callout`) && pk.Kind != "primitives" && pk.Kind != "screens" {
			t.Errorf("%s still opens with a callout", pk.Kind)
		}
	}
	gs := galleryPage(t, files, RootTheme(), "en", "getting-started")
	snippet := `<link rel="stylesheet" href="tokens.css">` + "\n" + `<link rel="stylesheet" href="theme-` + RootTheme() + `.css">`
	if texts := sourceTexts(gs); !slices.Contains(texts, snippet) {
		t.Errorf("Getting started has no copyable link snippet %q; its source blocks are %q", snippet, texts)
	}
}
```

(`primitives` keeps the two idiom-rule callouts and `screens` keeps its warning callouts. Those are content, not the preamble.)

In `proseSentinels`, replace `"Links here are inactive",` with `"Each example is live, but its links go nowhere.",` and in its comment's sentinel description replace `the dead-link callout on the pages that frame samples` with `the live-examples sentence on the pages that frame samples`.

- [ ] **Step 3: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheFramedPagesOpenWithTheTwoPlainSentences|TestNoEnglishProseReachesATranslatedPage' -count=1 ./internal/designsystem/`
Expected: FAIL, with `form says "Each example is live, but its links go nowhere." 0 times, want 1` and `sentinel "Each example is live, but its links go nowhere." is not on the English page`.

- [ ] **Step 4: Rewrite the bodies**

In `page.go`, delete `deadLinkCallout` and its comment. In `familyBody`, replace the five lines from `<p class="ds-lead">{{P "Pre-built, consistent UI elements, rendered server-side."}}</p>` through `<p class="ds-note">{{P "Sample content in English. Sample shells translated."}}</p>` with:

```
<p class="ds-lead">{{P "⟦gallery.intro.live⟧"}} {{P "⟦gallery.intro.code⟧"}}</p>
```

and in its doc comment replace `and the four sentences that are true of all of them` with `and the two sentences true of all of them`, and `the four notes below belong on each of them` with `the two sentences belong on each of them`. In `primitivesBody`, replace `` ` + deadLinkCallout + ` `` with `<p class="ds-lead">{{P "⟦gallery.intro.live⟧"}}</p>`. In `shellsBody`, delete the `` ` + deadLinkCallout + ` `` line. In `formats.go`'s `formatsBody` and `screens.go`'s `screensBody`, add `<p class="ds-lead">{{P "⟦gallery.intro.live⟧"}}</p>` directly after the page's first `<p class="ds-lead">…</p>`.

In `gettingStartedBody`, replace the paragraph under `{{P "Using it without the framework"}}` with:

```
<p class="ds-lead">{{P "⟦gallery.start.without⟧"}}</p>
<p class="ds-lead">{{P "⟦gallery.start.link_lead⟧"}}</p>
<pre class="ds-src rst-mono"><code>{{.Assets.Links}}</code></pre>
```

Add to `assetsView`:

```go
	// Links is the two <link> tags a page outside the framework needs,
	// highlighted: the order is the point, tokens.css then the theme,
	// because the theme's values fill the references tokens.css makes.
	Links template.HTML
```

and in `buildAssets` set, in the `out := assetsView{…}` literal:

```go
		Links: codeview.Highlight(`<link rel="stylesheet" href="tokens.css">` + "\n" + `<link rel="stylesheet" href="theme-` + theme + `.css">`),
```

- [ ] **Step 5: Retire the old keys and add the new**

Write `$TMPDIR/copy-task-7.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-7.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [
  "Pre-built, consistent UI elements, rendered server-side.",
  "The framework's own vocabulary calls these partials: ui.Templates() returns partials, and docs/site/templates.md documents them under that name. The word on this page changed; the code's did not.",
  "Links here are inactive",
  "Links inactive, sample source provided.",
  "Each sample below in its own frame.",
  "Sample content in English. Sample shells translated.",
  "The names above are links. Take tokens.css and one theme and you have the whole visual system: plain classes, ordinary HTML, no build step."
 ],
 "add": [
  {
   "id": "gallery.intro.live",
   "en": "Each example is live, but its links go nowhere.",
   "tr": {
    "ga": "Tá gach sampla beo, ach ní théann a naisc áit ar bith.",
    "zh-Hans": "每个示例都能真实操作，但其中的链接不会跳转到任何地方。",
    "es": "Cada ejemplo funciona de verdad, pero sus enlaces no llevan a ninguna parte.",
    "hi": "हर उदाहरण असली है, पर उसके लिंक कहीं नहीं ले जाते।",
    "pt": "Cada exemplo funciona de verdade, mas as suas ligações não levam a lado nenhum.",
    "bn": "প্রতিটি উদাহরণ সত্যিই চলে, কিন্তু এর লিংকগুলো কোথাও নিয়ে যায় না।",
    "ru": "Каждый пример работает, но его ссылки никуда не ведут.",
    "ja": "どの例も実際に動きますが、リンクはどこにも移動しません。",
    "yue": "每個例子都真係用得，不過入面啲連結唔會帶你去邊度。",
    "vi": "Mỗi ví dụ đều hoạt động thật, nhưng các liên kết trong đó không dẫn đến đâu.",
    "ar": "كل مثال يعمل فعلًا، لكن روابطه لا تؤدي إلى أي مكان."
   }
  },
  {
   "id": "gallery.intro.code",
   "en": "Code shows the template call to copy.",
   "tr": {
    "ga": "Taispeánann Cód an glao teimpléid le cóipeáil.",
    "zh-Hans": "“代码”里是可以复制的模板调用。",
    "es": "Código muestra la llamada a la plantilla que puedes copiar.",
    "hi": "कोड में कॉपी करने लायक टेम्पलेट कॉल दिखती है।",
    "pt": "Código mostra a chamada ao template para copiar.",
    "bn": "কোড-এ কপি করার মতো টেমপ্লেট কল দেখায়।",
    "ru": "На вкладке «Код» показан вызов шаблона, который можно скопировать.",
    "ja": "「コード」には、コピーして使うテンプレート呼び出しが表示されます。",
    "yue": "「代碼」度有可以複製嘅模板呼叫。",
    "vi": "Mã hiển thị lời gọi template để sao chép.",
    "ar": "تعرض «الشيفرة» استدعاء القالب الجاهز للنسخ."
   }
  },
  {
   "id": "gallery.start.without",
   "en": "The names above are links. Take tokens.css and one theme and you have the whole visual system: attributes on ordinary HTML, no build step.",
   "tr": {
    "ga": "Is naisc iad na hainmneacha thuas. Tóg tokens.css agus téama amháin agus tá an córas amhairc iomlán agat: tréithe ar ghnáth-HTML, gan chéim tógála.",
    "zh-Hans": "上面的名字就是链接。拿走 tokens.css 和一个主题，你就有了整套视觉系统：普通 HTML 上的属性，不需要构建步骤。",
    "es": "Los nombres de arriba son enlaces. Coge tokens.css y un tema y ya tienes todo el sistema visual: atributos sobre HTML corriente, sin paso de compilación.",
    "hi": "ऊपर दिए नाम ही लिंक हैं। tokens.css और एक थीम लीजिए और पूरा विज़ुअल सिस्टम आपके पास है: आम HTML पर एट्रिब्यूट, कोई बिल्ड स्टेप नहीं।",
    "pt": "Os nomes acima são links. Pegue no tokens.css e num tema e tem o sistema visual inteiro: atributos sobre HTML comum, sem passo de build.",
    "bn": "উপরের নামগুলিই লিঙ্ক। tokens.css আর একটি থিম নিন, গোটা ভিজ্যুয়াল সিস্টেম আপনার: সাধারণ HTML-এ অ্যাট্রিবিউট, কোনও বিল্ড স্টেপ নেই।",
    "ru": "Имена выше являются ссылками. Возьмите tokens.css и одну тему, и у вас есть вся визуальная система: атрибуты на обычном HTML, без шага сборки.",
    "ja": "上の名前はそのままリンクです。tokens.css とテーマを 1 つ持っていけば、視覚システムはそれで全部です。ふつうの HTML に属性を付けるだけで、ビルド手順はありません。",
    "yue": "上面啲名就係連結。攞走 tokens.css 同一個主題，你就有成套視覺系統：普通 HTML 上面嘅屬性，唔使建置步驟。",
    "vi": "Các tên ở trên chính là liên kết. Lấy tokens.css và một chủ đề là bạn có trọn hệ thống hình ảnh: thuộc tính trên HTML thường, không cần bước build.",
    "ar": "الأسماء أعلاه هي الروابط. خذ tokens.css وسمة واحدة، فيصير النظام البصري كلّه بين يديك: سمات على HTML عادي، وبلا خطوة بناء."
   }
  },
  {
   "id": "gallery.start.link_lead",
   "en": "Link them in this order, tokens.css first:",
   "tr": {
    "ga": "Nasc iad san ord seo, tokens.css ar dtús:",
    "zh-Hans": "按这个顺序链接，tokens.css 在前：",
    "es": "Enlázalos en este orden, primero tokens.css:",
    "hi": "इन्हें इसी क्रम में लिंक करें, पहले tokens.css:",
    "pt": "Ligue-os por esta ordem, tokens.css primeiro:",
    "bn": "এই ক্রমে লিংক করুন, আগে tokens.css:",
    "ru": "Подключайте их в таком порядке, сначала tokens.css:",
    "ja": "この順に読み込みます。tokens.css が先です:",
    "yue": "跟呢個次序連結，tokens.css 行先：",
    "vi": "Liên kết theo thứ tự này, tokens.css trước:",
    "ar": "اربطها بهذا الترتيب، tokens.css أولًا:"
   }
  }
 ],
 "fill": [
  "internal/designsystem/page.go",
  "internal/designsystem/formats.go",
  "internal/designsystem/screens.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-7.json"
```

Expected: `copyedit: prose.go: +4 -7`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, `internal/designsystem/formats.go`, `internal/designsystem/screens.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go internal/designsystem/formats.go internal/designsystem/screens.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 6: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS, including `TestEveryProseKeyIsTranslated` (no stale and no missing key) and `TestNoUnregisteredEnglishInThePageTemplates`.

- [ ] **Step 7: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 8: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/page.go \
  internal/designsystem/formats.go \
  internal/designsystem/screens.go \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Open each framed page with two plain sentences

Every component page opened with a callout and four notes telling the
reader what the samples were, at changelog length (\"The word on this
page changed; the code's did not.\"). Two sentences now say what a
reader needs: the examples are live, their links go nowhere, and Code
holds the call to copy. Shells gets neither, because it has no Code
tab.

Getting started said the system was plain classes, but it is
attributes. It now gives the two <link> tags in the order that
matters. Seven retired keys go with their 77 translations, because
the parity gate fails on a stale key as well as a missing one.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 8: A Buttons entry in `ui.Styleguide`

Spec 2.5 and decision 4. Copy: C11 (`gallery.button.blurb`). This is the one framework change: a new key in `ui.Styleguide`, so it appears on UI primitives with no gallery-only section, and `TestEveryStyleguideSampleAppearsAcrossThePages` covers it with no edit.

**Files:**
- Modify: `ui/styleguide.go` (a `"button"` sample, its labels filled from batch B2)
- Create: `ui/styleguide_button_test.go`
- Modify: `internal/designsystem/page.go` (`idiomBlurbs["button"]`, `previewHeights["idiom-button"]`)
- Modify: `internal/designsystem/prose.go` (one key)
- Modify: `docs/site/reference/ui.md:468-470` (the identifier list)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ui.Styleguide()["button"]`, the anchor `idiom-button` (Task 13's synonyms name it), and the prose key C11 (Task 9's sentinel).

- [ ] **Step 1: Check the approved copy this task writes**

```bash
jq -r --arg id gallery.button.blurb '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json
test "$(jq -r .action copy-review/batch-b2-result.json)" = approve
for id in cancel save preview delete view_orders continue archived; do jq -r --arg id button_sample.$id '.strings[]|select(.id==$id)|"\(.id)\t\(.text)"' copy-review/batch-b2-result.json; done
```

Expected: the blurb `Buttons come in four variants and three sizes, and a link can look like one. For the spinner a form shows while it submits, see form-foot.`, then seven `button_sample.*` lines, one per label. Stop and report if the blurb differs, if batch B2 is missing or not approved, or if any of the seven ids is absent. The labels are written from that file by id in Step 4, whatever their approved wording.

- [ ] **Step 2: Write the failing test**

Create `ui/styleguide_button_test.go`:

```go
package ui

import (
	"regexp"
	"strings"
	"testing"
)

// The button sample is the set that behaves differently, not every
// combination: four variants at the default size, small and large on
// primary, a link wearing the look, a full-width one and a disabled
// one. The busy state is form-foot's idle/working pair, so it is not
// drawn twice. A combination missing here is a size or variant a
// reader of the gallery never sees on a button.
func TestTheButtonSampleShowsEachDistinctBehaviour(t *testing.T) {
	sample, ok := Styleguide()["button"]
	if !ok {
		t.Fatal("Styleguide has no button sample")
	}
	for _, want := range []string{
		`<button rst-btn type="button">`,
		`<button rst-btn="primary" type="button">`,
		`<button rst-btn="ghost" type="button">`,
		`<button rst-btn="danger" type="button">`,
		`<button rst-btn="primary sm" type="button">`,
		`<button rst-btn="primary lg" type="button">`,
		`<a rst-btn href="`,
		`<button rst-btn="primary block" type="button">`,
		`<button rst-btn type="button" disabled>`,
	} {
		if !strings.Contains(sample, want) {
			t.Errorf("the button sample has no %s", want)
		}
	}
	if n := len(regexp.MustCompile(`\srst-btn[\s=>]`).FindAllString(sample, -1)); n != 9 {
		t.Errorf("the button sample draws %d buttons, want the 9 that behave differently", n)
	}
	if strings.Contains(sample, "aria-busy") || strings.Contains(sample, "rst-spin") {
		t.Error("the button sample draws the busy state, which form-foot's pair already shows")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod go test -run TestTheButtonSampleShowsEachDistinctBehaviour -count=1 ./ui/`
Expected: FAIL, `Styleguide has no button sample`.

- [ ] **Step 4: Add the sample**

In `ui/styleguide.go`, add before `"box":`:

```go
	// button: the set that behaves differently rather than every
	// combination. Variant and size are words in one attribute, so
	// rst-btn="primary sm" composes the way a class list would; a link
	// takes the same look, because a GET is a link even when it looks
	// like an action. The busy state is not here: form-foot's idle and
	// working pair is the one picture of it.
	"button": `<p><button rst-btn type="button">⟦button_sample.cancel⟧</button> <button rst-btn="primary" type="button">⟦button_sample.save⟧</button> <button rst-btn="ghost" type="button">⟦button_sample.preview⟧</button> <button rst-btn="danger" type="button">⟦button_sample.delete⟧</button></p>
<p><button rst-btn="primary sm" type="button">⟦button_sample.save⟧</button> <button rst-btn="primary lg" type="button">⟦button_sample.save⟧</button> <a rst-btn href="/orders">⟦button_sample.view_orders⟧</a></p>
<p><button rst-btn="primary block" type="button">⟦button_sample.continue⟧</button></p>
<p><button rst-btn type="button" disabled>⟦button_sample.archived⟧</button></p>`,
```

The `⟦id⟧` markers are the labels' copy-review ids. The copy tool replaces them with the approved text:

```bash
cat > "$TMPDIR/copy-task-8-labels.json" <<'EDIT'
{"approved": ["copy-review/batch-b2-result.json"], "fill": ["ui/styleguide.go"]}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-8-labels.json"
grep -c '⟦' ui/styleguide.go
```

Expected: `copyedit: ui/styleguide.go: 9 filled`, then `0`. The labels are sample data: English on every page of the gallery, like every other Styleguide sample, so they are not prose keys and get no translations.

In `docs/site/reference/ui.md`, change the identifier list from ``keyed by idiom name (`box`, `list-grid`, `dropdown`,`` to ``keyed by idiom name (`box`, `button`, `list-grid`, `dropdown`,``. This adds one identifier to an existing list of identifiers, with no new prose.

- [ ] **Step 5: Give it a blurb and a height**

In `page.go`, add to `idiomBlurbs`, first in the map:

```go
	"button":        "⟦gallery.button.blurb⟧",
```

and to `previewHeights`, under `// The markup idioms.`:

```go
	"idiom-button":        240, // four rows: the variants, the sizes and a link, full width, disabled
```

Write `$TMPDIR/copy-task-8.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-8.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [],
 "add": [
  {
   "id": "gallery.button.blurb",
   "en": "Buttons come in four variants and three sizes, and a link can look like one. For the spinner a form shows while it submits, see form-foot.",
   "tr": {
    "ga": "Tá ceithre leagan agus trí mhéid de chnaipí ann, agus is féidir le nasc cuma cnaipe a bheith air. Don roithleán a thaispeánann foirm agus í á seoladh, féach form-foot.",
    "zh-Hans": "按钮有四种样式和三种尺寸，链接也可以做成按钮的样子。表单提交时显示的加载圈，见 form-foot。",
    "es": "Los botones tienen cuatro variantes y tres tamaños, y un enlace puede parecer uno. Para el indicador que muestra un formulario mientras se envía, mira form-foot.",
    "hi": "बटन चार रूपों और तीन आकारों में आते हैं, और कोई लिंक भी बटन जैसा दिख सकता है। फ़ॉर्म भेजते समय दिखने वाले स्पिनर के लिए form-foot देखें।",
    "pt": "Os botões têm quatro variantes e três tamanhos, e uma ligação pode parecer um. Para o indicador que um formulário mostra enquanto é enviado, veja form-foot.",
    "bn": "বোতাম আসে চার রকমে আর তিন মাপে, আর একটি লিংকও বোতামের মতো দেখাতে পারে। ফর্ম পাঠানোর সময় যে স্পিনার দেখায়, তার জন্য form-foot দেখুন।",
    "ru": "Кнопки бывают четырёх видов и трёх размеров, а ссылка может выглядеть как кнопка. Индикатор, который форма показывает во время отправки, смотрите в form-foot.",
    "ja": "ボタンには 4 つの種類と 3 つのサイズがあり、リンクもボタンの見た目にできます。フォーム送信中に出るスピナーは form-foot を見てください。",
    "yue": "按鈕有四種款式同三種大細，連結都可以整到似按鈕。表單提交緊嗰陣出現嘅轉圈，睇 form-foot。",
    "vi": "Nút có bốn kiểu và ba cỡ, và một liên kết cũng có thể trông như nút. Về vòng quay mà biểu mẫu hiện khi đang gửi, xem form-foot.",
    "ar": "تأتي الأزرار بأربعة أنواع وثلاثة أحجام، ويمكن لرابط أن يبدو كزر. أما مؤشر الانتظار الذي يظهر أثناء إرسال النموذج فانظره في form-foot."
   }
  }
 ],
 "fill": [
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-8.json"
```

Expected: `copyedit: prose.go: +1 -0`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 6: Run the tests, the framework's included**

Run: `GOFLAGS=-mod=mod go test -count=1 ./ui/ ./internal/designsystem/`
Expected: PASS. `TestIdiomClassesAreStyled` holds the sample to attributes `tokens.css` styles, and every attribute used here is one it styles.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestEveryTapTargetIsAtLeast44Pixels|TestDesktopDensityIsPinned|TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens' -count=1 ./ui/`
Expected: PASS. The sizing fixture renders every Styleguide sample, and the existing `[rst-btn]` inventory row ("Buttons, all sizes") measures the new buttons, the disabled one included. A failure here names a button size the touch floor misses: that is a framework bug to report to the controller, not a reason to drop the button from the sample.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestPreviewFrameHeightsFitTheirContent -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS. If it fails on `idiom-button`, set its height to the number the failure gives.

- [ ] **Step 7: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 8: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="ui/styleguide.go \
  ui/styleguide_button_test.go \
  docs/site/reference/ui.md \
  internal/designsystem/page.go \
  internal/designsystem/prose.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Add the button set to ui.Styleguide

A gallery search for \"button\" found nothing, because buttons had no
entry and sm and block were drawn nowhere. The sample is the set that
behaves differently: four variants, small and large, a link wearing
the look, full width and disabled. The busy state stays form-foot's
alone, so there is one picture of it.

It is a Styleguide key rather than a gallery-only section because the
primitives page promises every sample on it is exactly what
ui.Styleguide returns.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---

### Task 9: Real titles: each page's `<h1>` is its topic, the outline is unbroken, and the separators are the approved ones

Spec 2.1 (all but the brand and the index title, which arrive with the frame in Task 10), the second leak-gate sentinel, and copy items C15 (`gallery.title`) and C16 (`gallery.frame.title`).

**Files:**
- Modify: `internal/designsystem/page.go`: `pageTemplate`'s `<title>` and page header; `pageView` (`Sub` out, `DocTitle` and `TitleID` in); delete `subhead`; `renderGallery`; `previewTitle`; `renderShell`; `renderDemo`; `modalData` and `modalTemplate`; every body's title block and heading levels (`overviewBody`, `gettingStartedBody`, `iconsBody`, `tokensBody`, `familyBody`, `primitivesBody`, `shellsBody`); add `titleSep`, `frameSep` and `docTitle`
- Modify: `internal/designsystem/formats.go` (`formatsBody`), `internal/designsystem/screens.go` (`screensBody`)
- Modify: `internal/designsystem/gallery.css` (`.ds-partial__name`, `.ds-shell__name`)
- Modify: `internal/designsystem/prose.go` (the subtitle key out; ten state labels re-keyed)
- Modify: `internal/designsystem/samples.go` (the state labels that carried em dashes, filled from batches B2 and B3)
- Test: `internal/designsystem/designsystem_test.go`, `internal/designsystem/header_rule_test.go:146`

**Interfaces:**
- Consumes: Task 8's blurb (the new sentinel).
- Produces: `const titleSep = " · "`, `const frameSep = ", "`, `func docTitle(page, site, theme string) string`, `pageView.DocTitle`, `pageView.TitleID`, and the classes `ds-partial__name` and `ds-shell__name`. Headings in bodies: a partial, an idiom, a format, a Tokens group, a shell and Screens' "Signing in" are `<h2>`; a screen is `<h3>`.

- [ ] **Step 1: Check the approved copy this task writes**

```bash
for id in gallery.title gallery.frame.title; do jq -r --arg id $id '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json; done
for batch in b2 b3; do test "$(jq -r .action copy-review/batch-$batch-result.json)" = approve || echo "MISSING or unapproved: batch $batch"; done
for id in idle working twelve_plain plain anything_else; do
  jq -e -r --arg id state.$id '.strings[]|select(.id==$id)|"\(.id)\t\(.text)"' copy-review/batch-b2-result.json || echo "MISSING state.$id in B2"
done
for id in status_404 status_403 status_422 status_500 status_503; do
  jq -e -r --arg id state.$id '.strings[]|select(.id==$id)|"\(.id)\t\(.text)"' copy-review/batch-b3-result.json || echo "MISSING state.$id in B3"
done
```

Expected: `Form · rastrillo design system · day` and `{name} sample standalone preview, Required`, then five `state.*` lines from B2 and five `state.status_*` lines from B3. The first two fix `titleSep` as ` · `, with no tail after the theme, and `frameSep` as `, `. The prefix `{name} sample standalone preview` is the existing key, unchanged. Stop and report if either differs, or if any line says `MISSING`. The translations in Step 6 were drafted from `404: not found`, `403: forbidden`, `422: unprocessable`, `500: server error` and `503: unavailable`; if B3 approved other words, the copy tool refuses until those entries are redrafted.

- [ ] **Step 2: Write the failing tests**

Append to `designsystem_test.go`:

```go
var (
	mainRegion = regexp.MustCompile(`(?s)<main rst-shell-main id="main">(.*)</main>`)
	headingTag = regexp.MustCompile(`<h([1-6])\b[^>]*>(.*?)</h[1-6]>`)
	titleTag   = regexp.MustCompile(`<title>([^<]*)</title>`)
)

// Every page's main opens with an <h1> that is its own topic, no two
// pages in a directory share one, and no heading skips a level: the
// outline a screen reader lists is the page's real structure. The
// pages used to share one <h1>, "rastrillo design system", with the
// real title an <h2> further down.
func TestEveryPagesH1IsItsOwnTitle(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			seen := map[string]string{}
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				m := mainRegion.FindStringSubmatch(string(files[name]))
				if m == nil {
					t.Errorf("%s: no main", name)
					continue
				}
				hs := headingTag.FindAllStringSubmatch(m[1], -1)
				if len(hs) == 0 || hs[0][1] != "1" {
					t.Errorf("%s: main does not open with an h1", name)
					continue
				}
				if want := template.HTMLEscapeString(proseIn(locale, pk.Title)); hs[0][2] != want {
					t.Errorf("%s: the h1 is %q, want the page's own title %q", name, hs[0][2], want)
				}
				if other, dup := seen[hs[0][2]]; dup {
					t.Errorf("%s and %s share the h1 %q", other, name, hs[0][2])
				}
				seen[hs[0][2]] = name
				level := 1
				for _, h := range hs[1:] {
					n := int(h[1][0] - '0')
					if n == 1 {
						t.Errorf("%s: a second h1 in main, %q", name, h[2])
					}
					if n > level+1 {
						t.Errorf("%s: %q is an h%d after an h%d; the outline skips a level", name, h[2], n, level)
					}
					level = n
				}
			}
		}
	}
}

// Every document title and frame name uses the separators copy review
// approved, which replaced an em dash a screen reader announces or
// swallows depending on its settings: " · " between a page, the site
// and the theme, and ", " before a frame's state.
func TestTitlesUseTheApprovedSeparators(t *testing.T) {
	files := render(t)
	for _, pk := range pageKinds() {
		name := RootTheme() + "/en/" + pk.File
		m := titleTag.FindStringSubmatch(string(files[name]))
		if want := template.HTMLEscapeString(docTitle(proseIn("en", pk.Title), "rastrillo design system", RootTheme())); m == nil || m[1] != want {
			t.Errorf("%s: <title> %v, want %q", name, m, want)
		}
	}
	if got := docTitle("Form", "rastrillo design system", "day"); got != "Form · rastrillo design system · day" {
		t.Errorf("docTitle gives %q", got)
	}
	if got := docTitle("The modal route", "rastrillo design system", ""); got != "The modal route · rastrillo design system" {
		t.Errorf("docTitle with no theme gives %q", got)
	}
	if got := previewTitle("en", "field-text", "Required"); got != "field-text sample standalone preview, Required" {
		t.Errorf("previewTitle gives %q", got)
	}
}

var stateLabel = regexp.MustCompile(`<p class="ds-state">([^<]*)</p>`)

// No title, frame name or state label anywhere in the tree carries an
// em dash, in any language. The state labels used to, and they reach a
// frame's name through its qualifier, so replacing the separators alone
// left "Working — what rastrillo.js writes" in a frame title.
func TestNoTitleOrStateLabelCarriesAnEmDash(t *testing.T) {
	frameTitle := regexp.MustCompile(`<iframe[^>]*\stitle="([^"]*)"`)
	for name, body := range render(t) {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		for what, re := range map[string]*regexp.Regexp{"title": titleTag, "frame name": frameTitle, "state label": stateLabel} {
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				if strings.Contains(m[1], "—") {
					t.Errorf("%s: the %s %q carries an em dash", name, what, m[1])
				}
			}
		}
	}
}
```

In `proseSentinels`, replace `"An overview of everything the design system provides.",` with `"Buttons come in four variants and three sizes, and a link can look like one.",` (a prefix of the C11 blurb, which the leak gate matches as a substring). In its comment, replace `the opening sentence on every one of them` with `the Buttons blurb under UI primitives`.

In `header_rule_test.go:146`, replace `<h3 class="ds-sub"` with `<h2 class="ds-sub"`.

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestEveryPagesH1IsItsOwnTitle|TestTitlesUseTheApprovedSeparators|TestNoTitleOrStateLabelCarriesAnEmDash' -count=1 ./internal/designsystem/`
Expected: FAIL to compile, `undefined: docTitle`. After Step 4's helpers, expect `the h1 is "rastrillo design system", want the page's own title "Overview"`, and from the sweep `the frame name "form-foot sample standalone preview, Working — what rastrillo.js writes on the way out" carries an em dash`, which Step 6 fixes.

- [ ] **Step 4: Title the pages**

In `page.go`, beside `copyJoin`, add:

```go
// titleSep and frameSep are the separators copy review approved for
// titles: " · " between a page, the site and the theme ("Form ·
// rastrillo design system · day"), and ", " before a preview frame's
// state ("field-text sample standalone preview, Required"). They
// replaced an em dash, which a screen reader announces or swallows
// depending on its settings. Separators, not words, so not translated.
const (
	titleSep = " · "
	frameSep = ", "
)

// docTitle is a gallery document's <title>: the page first, because a
// tab strip truncates from the end, then the site, then the theme when
// the document has one. The modal, shell and demo documents have none.
func docTitle(page, site, theme string) string {
	t := page + titleSep + site
	if theme != "" {
		t += titleSep + theme
	}
	return t
}
```

In `previewTitle`, replace `return t + " — " + proseIn(locale, qualifier)` with `return t + frameSep + proseIn(locale, qualifier)`.

In `pageView`, delete the `Sub` field and its comment, and add after `Title string`:

```go
	// DocTitle is the <title>, and TitleID the id the page's <h1>
	// carries: the anchor its old body heading had (#tokens,
	// #overview, …), kept so a link to it still lands on the title.
	// The family pages had none and have none.
	DocTitle string
	TitleID  string
```

Delete `subhead` and its comment, and remove `Sub: subhead(...)` from `base` in `renderGallery`. In the page loop after `view.Title = …`, add:

```go
		view.DocTitle = docTitle(view.Title, proseIn(locale, "rastrillo design system"), theme)
		if view.Family == nil {
			view.TitleID = pk.Kind
		}
```

(This line goes after `view.Family = familyOf(…)`.)

In `pageTemplate`, replace `<title>{{.Title}} — {{P "rastrillo design system"}} — {{.Theme}}</title>` with `<title>{{.DocTitle}}</title>`, and replace the two lines

```
    <h1>{{P "rastrillo design system"}}</h1>
    <p rst-page-header-sub>{{.Sub}}</p>
```

with `    <h1{{with .TitleID}} id="{{.}}"{{end}}>{{.Title}}</h1>`.

In `renderShell`, set `Title: docTitle(proseIn(locale, "The {shell} shell", "shell", shell), proseIn(locale, "rastrillo design system"), ""),`. In `renderDemo`, set `Title: docTitle(proseIn(locale, "The demo application"), proseIn(locale, "rastrillo design system"), ""),`. Add `Title string` to `modalData`, set it in `renderModal` to `docTitle(proseIn(locale, "The modal route"), proseIn(locale, "rastrillo design system"), "")`, and in `modalTemplate` replace the `<title>…</title>` line with `<title>{{.Title}}</title>`.

- [ ] **Step 5: Shift every body heading up one level**

In each body, delete the title block and promote the headings under it:

- `overviewBody`: delete `<div class="ds-head"><h2 id="overview">{{P "Overview"}}</h2></div>`. Change both `<h3 class="ds-sub">…</h3>` to `<h2 class="ds-sub">…</h2>`.
- `gettingStartedBody`: delete `<div class="ds-head"><h2 id="getting-started">…</h2></div>`. Change every `<h3 class="ds-sub">` and `</h3>` to `<h2 class="ds-sub">` and `</h2>`.
- `iconsBody`, `tokensBody`: delete the `ds-head` line. Change every `<h3 class="ds-sub"…>` to `<h2 class="ds-sub"…>`, keeping `id` and `data-ds-anchor` on the Tokens groups, and `</h3>` to `</h2>`.
- `familyBody`: replace `{{with .Family}}\n<div class="ds-head"><h2>{{.Title}}</h2></div>\n<p class="ds-lead">{{.Blurb}}</p>\n{{end}}` with `{{with .Family}}\n<p class="ds-lead">{{.Blurb}}</p>\n{{end}}`, and `<h3 class="rst-mono">{{.Name}}</h3>` with `<h2 class="ds-partial__name rst-mono">{{.Name}}</h2>`. In its doc comment, replace the last paragraph with: `The page's own title is its <h1>, so a partial is an <h2>.`
- `primitivesBody`: delete the `ds-head` line, and change `<h3 class="rst-mono">{{.Name}}</h3>` to `<h2 class="ds-partial__name rst-mono">{{.Name}}</h2>`.
- `shellsBody`: delete the `ds-head` line, and change `<h3>{{.Name}}</h3>` to `<h2 class="ds-shell__name">{{.Name}}</h2>`.
- `formatsBody`: delete the `ds-head` line, and change `<h3>{{.Title}}</h3>` to `<h2 class="ds-partial__name">{{.Title}}</h2>`.
- `screensBody`: delete `<div class="ds-head"><h2 id="screens">…</h2></div>`, change `<div class="ds-head"><h3 id="signing-in">{{P "Signing in"}}</h3></div>` to `<div class="ds-head"><h2 id="signing-in">{{P "Signing in"}}</h2></div>`, and `<h4>{{.Name}}</h4>` to `<h3 class="ds-partial__name">{{.Name}}</h3>`.

In `buildIdioms`' doc comment, replace the paragraph beginning `An idiom's own heading is an h3` with: `An idiom's heading is an <h2> under the page's <h1>, with nothing between: a deeper level there would skip one, which is WCAG 1.3.1 and what the accessibility gate found the first time it ran.`

In `gallery.css`, replace `.ds-partial > :is(h3, h4) { … }` with `.ds-partial__name { font-size: var(--rst-fs-base); margin: 0 0 var(--rst-sp-1); }`, and `.ds-shell h3 { … }` with `.ds-shell__name { font-size: 1.05rem; margin: 0 0 var(--rst-sp-1); }`. Add above the first:

```css
/* The section headings are styled by class, not by level: the outline
   moved once when the pages got real titles, and a rule keyed to h3 or
   h4 would silently unstyle every heading the next time it moves. */
```

- [ ] **Step 6: Retire the subtitle key, and relabel the states that carried em dashes**

In `samples.go`, replace each label below with its copy-review id as a marker. The copy tool fills the markers with the approved text, from batch B2 for the first five and batch B3 for the error statuses:

| Line (today) | From | To |
|---|---|---|
| 523 | `"Idle — the button before anything happens"` | `"⟦state.idle⟧"` |
| 526 | `"Working — what rastrillo.js writes on the way out"` | `"⟦state.working⟧"` |
| 779 | `"Twelve options, Plain — the enhancement opted out"` | `"⟦state.twelve_plain⟧"` |
| 816, 849 | `"Plain — the bare native input"` | `"⟦state.plain⟧"` |
| 874 | `"Anything else — the generic pair"` | `"⟦state.anything_else⟧"` |
| 884 to 892 (`statusLabel`) | `"404 — not found"`, `"403 — forbidden"`, `"422 — unprocessable"`, `"500 — server error"`, `"503 — unavailable"` | `"⟦state.status_404⟧"`, `"⟦state.status_403⟧"`, `"⟦state.status_422⟧"`, `"⟦state.status_500⟧"`, `"⟦state.status_503⟧"` |

Then retire the subtitle and the ten old keys, add the ten new ones and fill the markers in one edit. The translations are re-keyed and redrafted: each one is the old translation with its em dash replaced by a colon (a full-width colon in Chinese, Cantonese and Japanese). If batch B2 approved different English for an id, the tool refuses, and that entry's translations must be redrafted from the approved text first.

```bash
cat > "$TMPDIR/copy-task-9.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json",
  "copy-review/batch-b2-result.json",
  "copy-review/batch-b3-result.json"
 ],
 "remove": [
  "An overview of everything the design system provides. Theme: {theme}. Language: {language}.",
  "Idle — the button before anything happens",
  "Working — what rastrillo.js writes on the way out",
  "Twelve options, Plain — the enhancement opted out",
  "Plain — the bare native input",
  "Anything else — the generic pair",
  "404 — not found",
  "403 — forbidden",
  "422 — unprocessable",
  "500 — server error",
  "503 — unavailable"
 ],
 "add": [
  {
   "id": "state.idle",
   "en": "Idle: the button before anything happens",
   "tr": {
    "ga": "Díomhaoin: an cnaipe sula dtarlaíonn aon rud",
    "zh-Hans": "空闲：任何事情发生之前的按钮",
    "es": "En reposo: el botón antes de que ocurra nada",
    "hi": "निष्क्रिय: कुछ भी होने से पहले का बटन",
    "pt": "Em repouso: o botão antes de acontecer alguma coisa",
    "bn": "নিষ্ক্রিয়: কিছু ঘটার আগের বোতাম",
    "ru": "В покое: кнопка до того, как что-либо произошло",
    "ja": "待機中：何も起きていないときのボタン",
    "yue": "閒置：乜都未發生之前嘅掣",
    "vi": "Nghỉ: nút trước khi có chuyện gì xảy ra",
    "ar": "في السكون: الزر قبل أن يحدث أي شيء"
   }
  },
  {
   "id": "state.working",
   "en": "Working: what rastrillo.js writes on the way out",
   "tr": {
    "ga": "Ag obair: an rud a scríobhann rastrillo.js agus an fhoirm ag imeacht",
    "zh-Hans": "工作中：表单送出时 rastrillo.js 写下的样子",
    "es": "Trabajando: lo que rastrillo.js escribe al salir el formulario",
    "hi": "काम चल रहा है: फ़ॉर्म जाते समय rastrillo.js जो लिखता है",
    "pt": "A trabalhar: o que o rastrillo.js escreve quando o formulário sai",
    "bn": "কাজ চলছে: ফর্ম বেরোনোর সময় rastrillo.js যা লেখে",
    "ru": "В работе: то, что rastrillo.js пишет на выходе",
    "ja": "処理中：フォームが出ていくときに rastrillo.js が書くもの",
    "yue": "處理緊：表單送出嗰陣 rastrillo.js 寫低嘅嘢",
    "vi": "Đang xử lý: thứ rastrillo.js viết ra khi biểu mẫu rời đi",
    "ar": "قيد العمل: ما يكتبه rastrillo.js عند خروج النموذج"
   }
  },
  {
   "id": "state.twelve_plain",
   "en": "Twelve options, Plain: the enhancement opted out",
   "tr": {
    "ga": "Dhá rogha dhéag, Plain: diúltaíodh don fheabhsú",
    "zh-Hans": "十二个选项，Plain：明确不要增强",
    "es": "Doce opciones, Plain: la mejora rechazada",
    "hi": "बारह विकल्प, Plain: संवर्धन से मना कर दिया गया",
    "pt": "Doze opções, Plain: a melhoria recusada",
    "bn": "বারোটি অপশন, Plain: উন্নতিটি নাকচ করা",
    "ru": "Двенадцать вариантов, Plain: от улучшения отказались",
    "ja": "選択肢が 12、Plain：拡張を断った場合",
    "yue": "十二個選項，Plain：明確唔要嗰個加強",
    "vi": "Mười hai lựa chọn, Plain: đã từ chối phần tăng cường",
    "ar": "اثنا عشر خيارًا، Plain: مع رفض التحسين"
   }
  },
  {
   "id": "state.plain",
   "en": "Plain: the bare native input",
   "tr": {
    "ga": "Plain: an gnáth-ionchur dúchasach lom",
    "zh-Hans": "Plain：不加修饰的原生输入框",
    "es": "Plain: el campo nativo tal cual",
    "hi": "Plain: बिना कुछ जोड़े मूल इनपुट",
    "pt": "Plain: o campo nativo tal e qual",
    "bn": "Plain: নিছক নেটিভ ইনপুট",
    "ru": "Plain: голое родное поле",
    "ja": "Plain：素のネイティブ入力",
    "yue": "Plain：冇加嘢嘅原生輸入框",
    "vi": "Plain: ô nhập gốc để trơn",
    "ar": "Plain: الحقل الأصلي كما هو"
   }
  },
  {
   "id": "state.anything_else",
   "en": "Anything else: the generic pair",
   "tr": {
    "ga": "Aon rud eile: an péire ginearálta",
    "zh-Hans": "其他任何情况：通用的一对",
    "es": "Cualquier otro caso: el par genérico",
    "hi": "बाक़ी सब कुछ: सामान्य जोड़ी",
    "pt": "Qualquer outro caso: o par genérico",
    "bn": "বাকি সব কিছু: সাধারণ জোড়া",
    "ru": "Всё остальное: общая пара",
    "ja": "それ以外すべて：汎用の組",
    "yue": "其他任何情況：通用嗰對",
    "vi": "Mọi trường hợp khác: cặp chung",
    "ar": "أي حالة أخرى: الزوج العام"
   }
  },
  {
   "id": "state.status_404",
   "en": "404: not found",
   "tr": {
    "ga": "404: gan aimsiú",
    "zh-Hans": "404：找不到",
    "es": "404: no encontrado",
    "hi": "404: नहीं मिला",
    "pt": "404: não encontrado",
    "bn": "404: পাওয়া যায়নি",
    "ru": "404: не найдено",
    "ja": "404：見つかりません",
    "yue": "404：搵唔到",
    "vi": "404: không tìm thấy",
    "ar": "404: غير موجود"
   }
  },
  {
   "id": "state.status_403",
   "en": "403: forbidden",
   "tr": {
    "ga": "403: toirmiscthe",
    "zh-Hans": "403：无权限",
    "es": "403: prohibido",
    "hi": "403: पहुँच नहीं",
    "pt": "403: proibido",
    "bn": "403: প্রবেশাধিকার নেই",
    "ru": "403: нет доступа",
    "ja": "403：アクセス権なし",
    "yue": "403：冇權限",
    "vi": "403: không có quyền",
    "ar": "403: ممنوع"
   }
  },
  {
   "id": "state.status_422",
   "en": "422: unprocessable",
   "tr": {
    "ga": "422: nach féidir a phróiseáil",
    "zh-Hans": "422：无法处理",
    "es": "422: no procesable",
    "hi": "422: संसाधित नहीं हो सका",
    "pt": "422: não processável",
    "bn": "422: প্রক্রিয়া করা যায়নি",
    "ru": "422: не удалось обработать",
    "ja": "422：処理できません",
    "yue": "422：處理唔到",
    "vi": "422: không xử lý được",
    "ar": "422: تعذّرت المعالجة"
   }
  },
  {
   "id": "state.status_500",
   "en": "500: server error",
   "tr": {
    "ga": "500: earráid freastalaí",
    "zh-Hans": "500：服务器错误",
    "es": "500: error del servidor",
    "hi": "500: सर्वर त्रुटि",
    "pt": "500: erro do servidor",
    "bn": "500: সার্ভার ত্রুটি",
    "ru": "500: ошибка сервера",
    "ja": "500：サーバーエラー",
    "yue": "500：伺服器出錯",
    "vi": "500: lỗi máy chủ",
    "ar": "500: خطأ في الخادم"
   }
  },
  {
   "id": "state.status_503",
   "en": "503: unavailable",
   "tr": {
    "ga": "503: gan fáil",
    "zh-Hans": "503：暂时不可用",
    "es": "503: no disponible",
    "hi": "503: उपलब्ध नहीं",
    "pt": "503: indisponível",
    "bn": "503: এখন পাওয়া যাচ্ছে না",
    "ru": "503: недоступно",
    "ja": "503：利用できません",
    "yue": "503：用唔到",
    "vi": "503: không khả dụng",
    "ar": "503: غير متاح"
   }
  }
 ],
 "fill": [
  "internal/designsystem/samples.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-9.json"
grep -c '⟦' internal/designsystem/samples.go
```

Expected: `copyedit: prose.go: +10 -11`, `copyedit: internal/designsystem/samples.go: 11 filled`, then `0`.

- [ ] **Step 7: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS. `TestTheSidebarLinksEverythingOnThePageExactlyOnce` still holds, because the Tokens and Screens anchors kept their ids and `data-ds-anchor`. `proseMarkup`'s comment mentions the page's opening line. Replace `the page's opening line, which puts the theme name in <strong> and the language's autonym in a <strong lang=…>` with `a sentence whose placeholder is markup: the Icons page's link to lucide.dev, and the Code tab's wrapper line`.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestA11yScansTheGallery|TestA11yWalksTheKeyboard' -count=1 -timeout 25m ./internal/designsystem/`
Expected: PASS. axe's heading-order rule now sees the unbroken outline.

- [ ] **Step 8: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 9: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/page.go \
  internal/designsystem/formats.go \
  internal/designsystem/screens.go \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/samples.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/header_rule_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Title every gallery page by its topic and close the outline

Every page's h1 was \"rastrillo design system\" with the same subtitle.
The real title was an h2 further down, so a screen reader's heading
list began with the same words on every page. Each page's h1 is now
its topic and keeps the old heading's id. The subtitle goes, because
theme and language are on screen in their own controls. Every body
heading moves up one level, and the section headings are styled by
class so the next outline change cannot unstyle them.

Titles and frame names lose their em dashes for the separators copy
review approved. So do the ten state labels that carried one, which
reached frame names through their qualifiers: their approved text
comes from copy batches B2 and B3, and their translations are re-keyed. A sweep
now fails on an em dash in any title, frame name or state label. The
leak gate's subtitle sentinel becomes the Buttons blurb.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 10: The frame is the shipped sidebar layout: the phone index, the back control, and a pinned bar on desktop

Spec 2.2 and 2.3 (the DOM, the CSS for both views, the index pieces written on the Overview alone, the controls written twice, no prerender, scroll padding at 800px and up only, touch sizes) and 2.1's brand and index title. Tests: "The gallery is the shipped shell", "Every way up names its row", "The two copies of the controls agree", "No prerender". Migrated: `TestTheChromeCarriesTheThreeSwitchers`, `TestTheSidebarIsTheShellTheGalleryDocuments`, `TestA11yWalksTheKeyboard`, `TestThePreviewWidgetIsUsableOnAPhone`, `TestThePreviewDefaultIsMonotoneInStageWidth`, and the scheme journey. Retired: `TestTheSectionTabsNameEveryPage`, with the "Sections" key. Tasks 11 and 12 drive this frame in a browser.

**Files:**
- Modify: `internal/designsystem/page.go`: `navSection.Kind`; `galleryNav`; `pageView` (`Pages`, `Themes`, `Schemes` and `Locales` out; `Up`, `Home`, `Rows`, `Demos`, `Bar` and `Foot` in); delete `pageTabs`; add `controls`, `newControls`, `indexRow` and `indexRows`; `renderGallery`; `pageTemplate` (rewritten)
- Modify: `internal/designsystem/gallery.css` (the frame section; delete `.ds-switch` and the three `.ds-chrome` rules)
- Modify: `internal/designsystem/prose.go` ("Sections" out)
- Test: `internal/designsystem/designsystem_test.go`, `internal/designsystem/browser_test.go`, `internal/designsystem/a11y_test.go`

**Interfaces:**
- Consumes: Task 9's `pageView.DocTitle`, `TitleID` and the `<h1>`.
- Produces:
  - `type controls struct{ Themes []navLink; Schemes []schemeButton; Locales []localeLink; LocaleName string }`, `func newControls(mount, theme, locale, file, localeName, fragment string) controls`
  - `type indexRow struct{ ID, Title, Href string }`, `func indexRows(nav []navSection) ([]indexRow, navSection)`
  - DOM: `<div rst-shell-sidebar="index|page">`, `<div rst-shell-back>` (content pages), `<h1 rst-shell-title>`, `p.ds-index-lead`, `nav.ds-index` and `<div rst-shell-rail-foot id="ds-prefs">` (Overview only), `header.ds-top` holding `a.ds-top__brand` and `div.ds-top__controls`. Rows are `<a id="nav-<kind>" href="…">`.
  - CSS: `--ds-bar-h` on `:root` per band. Task 11 measures it.

- [ ] **Step 1: Write the failing Go tests**

Append to `designsystem_test.go`:

```go
var (
	shellRoot = regexp.MustCompile(`<div rst-shell-sidebar="([a-z]+)">`)
	backLink  = regexp.MustCompile(`<a rst-skip href="#main">[^<]*</a>\n<div rst-shell-back><a href="([^"]*)" rel="up" aria-label="([^"]*)">([^<]*)</a></div>`)
	railFoot  = regexp.MustCompile(`(?s)<div rst-shell-rail-foot id="ds-prefs">(.*?)</aside>`)
	barCtl    = regexp.MustCompile(`(?s)<div class="ds-top__controls">(.*?)</header>`)
	indexNav  = regexp.MustCompile(`(?s)<nav class="ds-index" rst-shell-nav aria-label="([^"]*)">(.*?)</nav>`)
	schemeVal = regexp.MustCompile(`data-ds-scheme="([a-z]+)"`)
)

// The gallery is the sidebar layout an app is scaffolded with,
// attribute for attribute, so every shell rule in tokens.css and
// shell.css applies to it and the gallery re-implements none of the
// phone navigation: the Overview is the index, every other page a
// content page whose back control comes first after the skip link.
// The index's own pieces are on the Overview alone, because on any
// other page they would show at no width.
func TestTheGalleryIsTheShippedShell(t *testing.T) {
	files := render(t)
	for name, body := range files {
		if strings.HasSuffix(name, ".html") && strings.Contains(string(body), "rst-shell-chrome") {
			t.Errorf("%s writes the retired drawer, rst-shell-chrome", name)
		}
	}
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			tr := translator(locale)
			label := tr("rastrillo.ui.shell_up_label")
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				page := string(files[name])
				index := pk.Kind == "overview"
				want := map[bool]string{true: "index", false: "page"}[index]
				if m := shellRoot.FindAllStringSubmatch(page, -1); len(m) != 1 || m[0][1] != want {
					t.Errorf("%s: shell root %v, want one rst-shell-sidebar=%q", name, m, want)
				}
				backs := backLink.FindAllStringSubmatch(page, -1)
				if n := strings.Count(page, "<div rst-shell-back>"); index && n != 0 || !index && (n != 1 || len(backs) != 1) {
					t.Errorf("%s: %d back controls (%d right after the skip link), want %d", name, n, len(backs), map[bool]int{false: 1}[index])
				} else if !index {
					if aria := template.HTMLEscapeString(tr("rastrillo.ui.shell_up", "name", label)); backs[0][2] != aria || backs[0][3] != template.HTMLEscapeString(label) {
						t.Errorf("%s: the back control reads %q named %q, want %q named %q", name, backs[0][3], backs[0][2], label, aria)
					}
				}
				for _, piece := range []string{`<h1 rst-shell-title>`, `<p class="ds-index-lead">`, `<nav class="ds-index"`, `<div rst-shell-rail-foot id="ds-prefs">`} {
					if strings.Contains(page, piece) != index {
						t.Errorf("%s: %s present=%v; it belongs on the Overview and nowhere else", name, piece, !index)
					}
				}
				if index {
					if m := railFoot.FindStringSubmatch(page); m == nil || strings.TrimSpace(m[1]) == "</div>" {
						t.Errorf("%s: the index's foot is empty", name)
					}
				}
				for _, want := range []string{
					`<link rel="stylesheet" href="` + mountPrefix + `shell.css">`,
					`<script defer blocking="render" src="` + mountPrefix + `shell.js"></script>`,
				} {
					if !strings.Contains(page, want) {
						t.Errorf("%s: no %s; the slide, Back through history and the focus return are shell.css and shell.js", name, want)
					}
				}
			}
		}
	}
}

// Every content page's back control returns to its own row on the
// index: index.html#nav-<kind>, a fragment naming a link inside the
// rows nav whose href is that very page. The rows are the sections
// the tree lists, in its order, then Demos and the tree's demo links.
func TestEveryWayUpNamesItsRow(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			overview := galleryPage(t, files, theme, locale, "overview")
			m := indexNav.FindStringSubmatch(overview)
			if m == nil {
				t.Fatalf("%s/%s: no rows nav on the Overview", theme, locale)
			}
			if want := template.HTMLEscapeString(translator(locale)("rastrillo.ui.shell_up_label")); m[1] != want {
				t.Errorf("%s/%s: the rows nav is labelled %q, want the back control's word %q, so Back to it lands on a list of that name", theme, locale, m[1], want)
			}
			rows := m[2]
			for _, pk := range pageKinds() {
				if pk.Kind == "overview" {
					continue
				}
				page := galleryPage(t, files, theme, locale, pk.Kind)
				b := backLink.FindStringSubmatch(page)
				href := pageHref(mountPath, theme, locale, pk.File)
				if want := indexHref(mountPath, theme, locale) + "#nav-" + pk.Kind; b == nil || b[1] != want {
					t.Errorf("%s/%s/%s: way up %v, want %s", theme, locale, pk.File, b, want)
				}
				if row := `<a id="nav-` + pk.Kind + `" href="` + href + `">`; !strings.Contains(rows, row) {
					t.Errorf("%s/%s: no row %s", theme, locale, row)
				}
			}
			// The same sections as the tree, in the same order.
			var tree, listed []string
			for _, g := range railSections(railOf(t, "overview", overview)) {
				tree = append(tree, g.Title)
			}
			for _, r := range regexp.MustCompile(`<a id="nav-[^"]+" href="[^"]*">([^<]*)</a>`).FindAllStringSubmatch(rows, -1) {
				listed = append(listed, r[1])
			}
			if want := tree[1 : len(tree)-1]; strings.Join(listed, "|") != strings.Join(want, "|") {
				t.Errorf("%s/%s: rows %v, want the tree's sections but the Overview and Demos, %v", theme, locale, listed, want)
			}
			if !strings.Contains(rows, `<p rst-shell-group>`+tree[len(tree)-1]+`</p>`) {
				t.Errorf("%s/%s: the rows do not end with the Demos group", theme, locale)
			}
		}
	}
}

// The Overview writes its controls twice, in the bar for a wide screen
// and in the index's foot for a phone, and only one is ever shown. The
// two must not drift when the next control is added to one: same
// themes, same schemes, same languages, in the same order. The foot's
// links carry #ds-prefs, so a reader switching there lands with the
// controls on screen; the bar's carry no fragment, because gallery.js
// sets one at the moment of the click. Neither carries an id but the
// foot's own, since an id written twice is two elements one fragment
// cannot both reach.
func TestTheTwoCopiesOfTheControlsAgree(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			page := galleryPage(t, files, theme, locale, "overview")
			foot, bar := railFoot.FindStringSubmatch(page), barCtl.FindStringSubmatch(page)
			if foot == nil || bar == nil {
				t.Fatalf("%s/%s: foot found %v, bar found %v", theme, locale, foot != nil, bar != nil)
			}
			fh, bh := anchorHref.FindAllStringSubmatch(foot[1], -1), anchorHref.FindAllStringSubmatch(bar[1], -1)
			if len(fh) == 0 || len(fh) != len(bh) {
				t.Errorf("%s/%s: %d links in the foot, %d in the bar", theme, locale, len(fh), len(bh))
				continue
			}
			for i := range fh {
				if strings.Contains(bh[i][1], "#") || fh[i][1] != bh[i][1]+"#ds-prefs" {
					t.Errorf("%s/%s link %d: foot %s, bar %s; want the bar's address and the foot's with #ds-prefs", theme, locale, i, fh[i][1], bh[i][1])
				}
			}
			if a, b := schemeVal.FindAllString(foot[1], -1), schemeVal.FindAllString(bar[1], -1); strings.Join(a, " ") != strings.Join(b, " ") || len(a) != 3 {
				t.Errorf("%s/%s: scheme buttons %v in the foot, %v in the bar", theme, locale, a, b)
			}
			for where, html := range map[string]string{"foot": foot[1], "bar": bar[1]} {
				if elementID.MatchString(html) {
					t.Errorf("%s/%s: the %s's controls carry an id", theme, locale, where)
				}
			}
		}
	}
}

// No page asks to be prerendered. The gallery is static files, served
// by a site with no headers file, and an inline speculation ruleset
// would need a CSP the framework deliberately avoids; reading down the
// index would also fetch pages of up to ~120 KB nobody opens, on a
// phone's data. The slide and the focus return work without it.
func TestNoGalleryPageAsksForPrerender(t *testing.T) {
	for name, body := range render(t) {
		if strings.Contains(string(body), "speculationrules") || strings.Contains(string(body), "_speculation-rules") {
			t.Errorf("%s asks for prerendering", name)
		}
	}
}
```

Replace the body of the per-page loop in `TestTheChromeCarriesTheThreeSwitchers` (from `// The chrome is read out of the page by its own element` through the checks on `rest` and `chromeStart`) with:

```go
				// The bar is the shell's grid item between the rail and
				// main, outside main, so it is the page's one banner and
				// the skip link skips it with the rail.
				at := strings.Index(page, `<header class="ds-top">`)
				if at < 0 || !strings.Contains(page[:at], "</aside>") {
					t.Errorf("%s: no bar after the rail", name)
					continue
				}
				bar, rest, ok := strings.Cut(page[at:], "</header>")
				if !ok {
					t.Errorf("%s: the bar never closes", name)
					continue
				}
				if !strings.HasPrefix(strings.TrimSpace(rest), `<main rst-shell-main id="main">`) {
					t.Errorf("%s: main does not follow the bar", name)
				}
				if _, m, _ := strings.Cut(rest, `<main rst-shell-main id="main">`); !strings.HasPrefix(strings.TrimSpace(m), `<div rst-page>`) {
					t.Errorf("%s: main does not open with the content column", name)
				}
				if want := `<a class="ds-top__brand" href="` + indexHref(mountPath, theme, locale) + `">`; !strings.Contains(bar, want) {
					t.Errorf("%s: the bar's brand does not link the Overview", name)
				}
				// The link count reads the controls, not the bar: the
				// brand is an in-tree link too.
				_, chrome, ok := strings.Cut(bar, `<div class="ds-top__controls">`)
				if !ok {
					t.Errorf("%s: the bar has no controls", name)
					continue
				}
```

Keep the rest of that loop unchanged. It reads `chrome`. Update the test's doc comment: replace `The three switchers sit in the gallery's own <header>, above main,` with `The three switchers sit in the pinned bar, between the rail and main,`.

In `TestTheSidebarIsTheShellTheGalleryDocuments`, delete `` `<div rst-shell-sidebar>`, `` and `` `<details rst-shell-chrome>`, `` from the first list. Replace the comment sentence `The mobile collapse comes with it — the <details> chrome strip is the shell's own, so the rail folds away below 800px with no JavaScript and nothing here to write.` with `The markup itself is held by TestTheGalleryIsTheShippedShell; this one holds the rail and its filter.`

Delete `TestTheSectionTabsNameEveryPage` and its comment. In `TestEverySectionOfTheRailRoutesToItsOwnPage`'s comment, nothing changes. It still holds the rail on desktop.

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestTheGalleryIsTheShippedShell|TestEveryWayUpNamesItsRow|TestTheTwoCopiesOfTheControlsAgree|TestNoGalleryPageAsksForPrerender|TestTheChromeCarriesTheThreeSwitchers' -count=1 ./internal/designsystem/`
Expected: FAIL, with `writes the retired drawer, rst-shell-chrome`, `no rows nav on the Overview`, `no bar after the rail`.

- [ ] **Step 3: Build the frame's data**

In `page.go`, add `Kind string // the page kind; "" for Demos` to `navSection`, and set `Kind: pk.Kind,` in `galleryNav`'s section literal.

Delete `pageTabs` and its comment. Delete `pageView.Pages` and its comment, and the `Themes`, `Schemes` and `Locales` fields. Add to `pageView`, after `Mount`:

```go
	// Home is the Overview's address, which the bar's brand links, and
	// Up a content page's way back: the Overview with this page's own
	// row as the fragment, so focus returns to it even with scripts off.
	// The Overview has no Up; its way up would be itself.
	Home string
	Up   string

	// Rows and Demos are the phone index, the Overview's alone. Below
	// 800px the Overview is the index and every other page a content
	// page, the way the shipped sidebar layout works, and on any other
	// page these would show at no width.
	Rows  []indexRow
	Demos navSection

	// Bar and Foot are the theme, scheme and language controls. The
	// Overview writes them twice, in the pinned bar for a wide screen
	// and in the index's foot for a phone, because the rail is a grid
	// item and nothing inside it can be placed in the bar's cell; one
	// copy is ever shown. Every other page has the bar's alone.
	Bar  controls
	Foot controls
```

Add, after `schemeButtons`:

```go
// controls is one copy of the switchers.
type controls struct {
	Themes     []navLink
	Schemes    []schemeButton
	Locales    []localeLink
	LocaleName string
}

// newControls builds the switchers for one page. fragment is appended
// to every theme and language link: "" in the bar, where gallery.js
// adds the reader's place at the moment of the click, and #ds-prefs in
// the phone index's foot, a fixed place that needs no script, so a
// reader who switches there lands with the controls on screen.
func newControls(mount, theme, locale, file, localeName, fragment string) controls {
	c := controls{
		Themes:     themeLinks(mount, theme, locale, file),
		Schemes:    schemeButtons(locale),
		Locales:    localeLinks(mount, theme, locale, file),
		LocaleName: localeName,
	}
	for i := range c.Themes {
		c.Themes[i].Href += fragment
	}
	for i := range c.Locales {
		c.Locales[i].Href += fragment
	}
	return c
}

// indexRow is one row of the phone index: a section's page, by title,
// carrying the id its pages' back controls name.
type indexRow struct {
	ID, Title, Href string
}

// indexRows reads the phone index off the rail, so the rows and the
// tree cannot list different sections: one row per page kind but the
// Overview, which is the index itself, and the Demos section as is.
// Rows are plain links, not the tree's disclosures, because shell.js
// returns focus to the first nav link whose path is the page just
// left, and in the tree that link is inside a closed <details> that
// cannot take focus. anchorID never writes a nav- id, and the kinds are
// unique, so the ids cannot collide with anything else on the page.
func indexRows(nav []navSection) ([]indexRow, navSection) {
	rows := make([]indexRow, 0, len(nav))
	for _, s := range nav[:len(nav)-1] {
		if s.Kind == "overview" {
			continue
		}
		rows = append(rows, indexRow{ID: "nav-" + s.Kind, Title: s.Title, Href: s.Href})
	}
	return rows, nav[len(nav)-1]
}
```

In `renderGallery`, delete `Schemes: schemeButtons(locale),` from `base` and add `Home: indexHref(mount, theme, locale),`. In the page loop, replace the three lines setting `view.Pages`, `view.Themes` and `view.Locales` with:

```go
		view.Bar = newControls(mount, theme, locale, pk.File, localeName, "")
		if pk.Kind != "overview" {
			view.Up = view.Home + "#nav-" + pk.Kind
		}
```

and after `view.Nav = galleryNav(…)` add:

```go
		if pk.Kind == "overview" {
			view.Rows, view.Demos = indexRows(view.Nav)
			view.Foot = newControls(mount, theme, locale, pk.File, localeName, "#ds-prefs")
		}
```

- [ ] **Step 4: Rewrite `pageTemplate`**

Replace the doc comment's last paragraph (from `The frame is the sidebar shell, class for class:` to the end of that paragraph) with:

```
// The frame is the sidebar layout an app is scaffolded with
// (ui/layouts/sidebar.html), attribute for attribute: below 800px the
// Overview is the index and every other page a content page with the
// shell's back control, and shell.js and shell.css give the slide, Back
// through history and the focus return with nothing written here. Two
// departures, each for a reason: the index's pieces are on the Overview
// only, and the controls live in a bar pinned over main on a wide
// screen and in the index's foot on a phone, so the Overview writes them
// twice. That is dogfooding with a point: the shell is one of the
// things this page documents.
```

Replace `const pageTemplate = …` with:

```go
const pageTemplate = `{{define "ds-controls"}}<nav rst-seg-tabs aria-label="{{P "Theme"}}">{{range .Themes}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
<div class="ds-scheme" role="group" aria-label="{{P "Colour scheme"}}">{{range .Schemes}}<button type="button" data-ds-scheme="{{.Value}}" aria-pressed="{{.Pressed}}">{{.Label}}</button>{{end}}</div>
<details rst-dropdown rst-locale name="rst-menus">
<summary>{{T "rastrillo.ui.shell_language"}}<span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span><span class="rst-sr-only">{{P ", currently {language}" "language" .LocaleName}}</span></summary>
<div rst-dropdown-menu>{{range .Locales}}<a href="{{.Href}}" lang="{{.Code}}" dir="{{.Dir}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>{{end}}</div>
</details>{{end}}
{{define "ds-page"}}<!doctype html>
<html lang="{{.Locale}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>{{.DocTitle}}</title>
<link rel="stylesheet" href="{{.Mount}}/tokens.css">
<link rel="stylesheet" href="{{.Mount}}/theme-{{.Theme}}.css">
<link rel="stylesheet" href="{{.Mount}}/shell.css">
<link rel="stylesheet" href="{{.Mount}}/gallery.css">
<script src="{{.Mount}}/gallery.js"></script>
<script defer src="{{.Mount}}/rastrillo.js"></script>
<script defer blocking="render" src="{{.Mount}}/shell.js"></script>
</head>
<body>
<div rst-shell-sidebar="{{if .Rows}}index{{else}}page{{end}}">
<a rst-skip href="#main">{{T "rastrillo.ui.shell_skip"}}</a>
{{with .Up}}<div rst-shell-back><a href="{{.}}" rel="up" aria-label="{{Tf "rastrillo.ui.shell_up" "name" (T "rastrillo.ui.shell_up_label")}}">{{T "rastrillo.ui.shell_up_label"}}</a></div>
{{end}}<aside class="ds-rail" rst-shell-rail>
{{if .Rows}}<h1 rst-shell-title>{{P "rastrillo design system"}}</h1>
<p class="ds-index-lead">{{P "The Rastrillo design system aims to be a starter framework for any app to get a consistent, polished, accessible UI with no or minimal JavaScript dependence, available in multiple languages, and using clean, modern HTML and CSS. It's designed to be delightful to use with or without LLM assistance, and easily remixable."}}</p>
{{end}}  <search class="ds-search">
    <label class="rst-sr-only" for="ds-filter">{{P "Filter"}}</label>
    <input id="ds-filter" type="search" placeholder="{{P "Filter"}}" autocomplete="off" aria-controls="ds-nav" data-ds-filter>
  </search>
  <p class="ds-nav__empty" data-ds-filter-empty role="status" hidden>{{P "No matches"}}</p>
{{if .Rows}}  <nav class="ds-index" rst-shell-nav aria-label="{{T "rastrillo.ui.shell_up_label"}}">{{range .Rows}}<a id="{{.ID}}" href="{{.Href}}">{{.Title}}</a>{{end}}<p rst-shell-group>{{.Demos.Title}}</p>{{range .Demos.Items}}<a href="{{.Href}}" target="_blank" rel="noopener">{{.Label}}</a>{{end}}</nav>
{{end}}  <nav class="ds-nav" rst-shell-nav id="ds-nav" aria-label="{{P "Sections and demos"}}">
{{range .Nav}}{{if .Items}}    <details{{if .Current}} open aria-current="page"{{end}}><summary><span rst-caret aria-hidden="true">{{icon "chevron-down"}}</span>{{.Title}}</summary>{{range .Items}}<a href="{{.Href}}"{{if .Aria}} aria-label="{{.Aria}}"{{end}}{{if .Code}} class="rst-mono"{{end}}{{if .Blank}} target="_blank" rel="noopener"{{end}}>{{.Label}}</a>{{end}}</details>
{{else}}    <a class="ds-nav__page" href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Title}}</a>
{{end}}{{end}}  </nav>
{{if .Rows}}<div rst-shell-rail-foot id="ds-prefs">{{template "ds-controls" .Foot}}</div>
{{end}}</aside>
<header class="ds-top">
<a class="ds-top__brand" href="{{.Home}}">{{P "rastrillo design system"}}</a>
<div class="ds-top__controls">{{template "ds-controls" .Bar}}</div>
</header>
<main rst-shell-main id="main">
<div rst-page>

<header rst-page-header>
  <div rst-page-header-titles>
    <h1{{with .TitleID}} id="{{.}}"{{end}}>{{.Title}}</h1>
  </div>
</header>

{{.Body}}

<nav class="ds-updown" aria-label="{{P "Previous and next"}}">{{with .Prev}}<a class="ds-updown__prev" href="{{.Href}}">{{.Label}}</a>{{end}}{{with .Next}}<a class="ds-updown__next" href="{{.Href}}">{{.Label}}</a>{{end}}</nav>
<p class="rst-sr-only" role="status" data-ds-copy-status data-copy="{{P "⟦gallery.copy.button⟧"}}" data-copied="{{P "⟦gallery.copy.done⟧"}}" data-failed="{{P "⟦gallery.copy.failed⟧"}}" data-join="{{.CopyJoin}}"></p>

</div>
</main>
</div>
</body>
</html>
{{end}}`
```

Retire the "Sections" key with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-10.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [
  "Sections"
 ],
 "add": [],
 "fill": [
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-10.json"
```

Expected: `copyedit: prose.go: +0 -1`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go` prints `0` for each. Nothing in this task needs a copy check: every word above is an existing key or a framework catalog string.

- [ ] **Step 5: Style the frame**

In `gallery.css`, delete the `.ds-switch` rule, the three `.ds-chrome` rules, and the comment block above the `.ds-rail` rule that begins `/* The sidebar. The rail itself is the framework's own sidebar shell`. Replace the deleted comment with:

```css
/* The rail is the shipped sidebar layout's, below 800px the phone
   index on the Overview and hidden on every other page, all by
   tokens.css's own rules. What is left here is what a rail of links
   has no attribute for: a collapsible section per group, and the
   filter over them. */
```

At the end of the file, add:

```css
/* ── The frame ───────────────────────────────────────────────────────
   At 800px and up the bar is pinned over main. A grid item's
   containing block is its grid area and a sticky box cannot leave it,
   so a bar in a one-row area has no room to stick; it gets the rail's
   shape instead, an area spanning every row, with the first row
   reserving its height. Lines are numbered, not sided, so in Arabic
   the rail is on the right and the bar over main on the left with no
   rule here.

   --ds-bar-h is the bar's border-box height per band, the measured
   tallest over every theme and language: one row from 1024px, and
   room for two between 800 and 1023px. TestThePinnedBarFitsItsReservation
   measures what is inside the bar against it. */
:root { --ds-bar-h: 3.5rem; }
@media (min-width: 800px) and (max-width: 1023.98px) { :root { --ds-bar-h: 6rem; } }
.ds-top { align-items: center; border-block-end: 1px solid var(--rst-line); display: flex; flex-wrap: wrap; gap: var(--rst-sp-2) var(--rst-sp-3); justify-content: space-between; padding: var(--rst-sp-2) var(--rst-sp-4); }
.ds-top__brand { color: var(--rst-text); font-weight: 650; text-decoration: none; }
.ds-top__controls { align-items: center; display: flex; flex-wrap: wrap; gap: var(--rst-sp-3); justify-content: flex-end; }
.ds-top__controls [rst-dropdown] { position: relative; }
.ds-top__controls [rst-dropdown] > summary { padding-block: 0.25rem; }
@media (min-width: 800px) {
  body > [rst-shell-sidebar] { grid-template-rows: var(--ds-bar-h) 1fr; }
  body > [rst-shell-sidebar] > [rst-shell-rail] { grid-column: 1; grid-row: 1 / -1; }
  /* Opaque, and above page content (auto, the positioned preview boxes
     and transformed frames included) but below the skip link's 60. */
  .ds-top { align-self: start; background: var(--rst-bg); block-size: var(--ds-bar-h); box-sizing: border-box; grid-column: 2; grid-row: 1 / -1; inset-block-start: 0; position: sticky; z-index: 10; }
  body > [rst-shell-sidebar] > [rst-shell-main] { grid-column: 2; grid-row: 2; }
  /* Fragments and focus stop below the pinned bar (WCAG technique C43).
     Only here: below 800px what is pinned is the framework's back
     strip, and its clearance is tokens.css's to set; a rule on html at
     every width would override that. */
  html { scroll-padding-block-start: calc(var(--ds-bar-h) + var(--rst-sp-2)); }
}
/* Pieces only the phone index shows, written on the Overview alone. At
   800px and up the Overview is a page with the rail beside it, as the
   shell's index is, and these would be a second list under the tree
   and a second set of controls beside the bar's. */
.ds-index-lead, .ds-rail > .ds-index, .ds-rail > [rst-shell-rail-foot] { display: none; }
@media (max-width: 799.98px) {
  /* On a phone a section page is the way back and the content, and the
     controls are in the index's foot, under the list. */
  .ds-top { display: none; }
  [rst-shell-sidebar~="index"] .ds-index-lead { color: var(--rst-text-muted); display: block; margin: 0 0 var(--rst-sp-4); padding-inline: 0.25rem; }
  [rst-shell-sidebar~="index"] > .ds-rail > :is(.ds-index, [rst-shell-rail-foot]) { display: flex; }
  /* A query swaps the rows for the tree it filters. Two rules, so an
     engine without :has() drops both and shows rows and tree one after
     the other: long, but with no route missing. */
  [rst-shell-sidebar~="index"]:not(:has(#ds-filter:not(:placeholder-shown))) > .ds-rail > .ds-nav { display: none; }
  [rst-shell-sidebar~="index"]:has(#ds-filter:not(:placeholder-shown)) > .ds-rail > .ds-index { display: none; }
}
/* The scheme buttons are the gallery's own and under 30px tall; the
   theme links and the language summary are framework controls the
   touch block already sizes. Same query as tokens.css's touch block. */
@media (pointer: coarse), (max-width: 40rem) {
  .ds-scheme button { min-block-size: var(--rst-tap); }
}
```

- [ ] **Step 6: Migrate the browser tests the frame moves**

In `browser_test.go`, `TestSchemeToggleDrivesTheWholeJourney`: prefix every scheme selector with `.ds-top `. That covers `'[data-ds-scheme][aria-pressed="true"]'` in `pressed`, the `WaitVisible` and both `Click`s, and both `document.querySelector(".ds-scheme")`. Add above `pressed`:

```go
	// Every selector is scoped to the bar: on the Overview the controls
	// are written twice, and the first copy in the document is the
	// phone index's foot, hidden at this width.
```

Prefix the `Click` at line 778 (in `TestPreviewWidgetDrivesTheWholeJourney`) the same way.

In `TestThePreviewWidgetIsUsableOnAPhone`, change `for _, kind := range []string{"display", "overview"}` to `for _, kind := range []string{"display", "shells"}`, and add above it:

```go
	// Display for the component width class and Shells for the page
	// one. Not the Overview: below 800px it is the phone index, its main
	// has no box, and its framed demo application is a Demos row.
```

In `TestThePreviewDefaultIsMonotoneInStageWidth`, make the same change, `{"display", "overview"}` to `{"display", "shells"}`, and in its comment replace `the Overview frames the demo application at 1200px` with `Shells frames five page demos at 1200px`.

In `a11y_test.go`'s `TestA11yWalksTheKeyboard`, replace the seek with:

```go
	// Tab past the rail and the bar. The stop this lands on is the
	// first focusable in the content column, which is the first stop
	// the thirty below assert. Bounded rather than counted: the rail's
	// length is a property of how many partials ui ships.
	const seekLimit = 250
	inPage := `!!(document.activeElement && document.activeElement.closest("[rst-page]"))`
	var reached bool
	for i := 0; i < seekLimit && !reached; i++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(inPage, &reached)); err != nil {
			t.Fatalf("seeking to the content, tab %d: %v", i+1, err)
		}
	}
	if !reached {
		t.Fatalf("%d Tabs never reached the content column; the walk would have covered the rail and nothing else", seekLimit)
	}
```

and in the loop comment replace `the tab strip itself` with `the first focusable in the content`.

- [ ] **Step 7: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS. `TestEveryPageStaysUnderItsBudget` logs an Overview near 36 KB on `signal/bn`.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS: the whole package, since the frame touches every drive. `TestA11yReflowsAt320` now loads the Overview as the phone index, and a sideways overflow there names the element. `TestA11yScansTheGallery` scans the Overview at the default 800px window, where it is a page with the bar.

- [ ] **Step 8: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 9: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/page.go \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/browser_test.go \
  internal/designsystem/a11y_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Build the gallery's frame from the shipped sidebar layout

The gallery kept a hand copy of the old sidebar layout, drawer and
all, and a 13-link section strip that wrapped to five rows on a phone
and put the first component 900px down. It now uses the layout an app
is scaffolded with. On a phone the Overview is the index, every other
page has the shell's back control to its own row, and shell.js and
shell.css give the slide and the focus return. On a wide screen the
controls sit in a bar pinned over main. It is placed by grid line
across every row, because a sticky box cannot leave a one-row grid
area.

The Overview writes its controls twice, one copy for each view,
because nothing inside the rail can sit in the bar's cell. A test
holds the copies together. No ruleset asks for prerendering: the site
is static files, and reading down the index would fetch pages nobody
opens.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 11: The pinned bar, driven: it fits, nothing paints over it, focus is never under it, and right to left mirrors it

Spec Tests "What is pinned fits its reservation", "Nothing paints over the bar or the back strip", "Focus is never under the pinned bar", and 2.10's 1280px leg ("in `day/ar` the rail lies to the right of `<main>` and the bar above `<main>`"). This task owns `--ds-bar-h`'s values. Task 10 set first estimates, and the fit drive here measures them over every theme and locale.

**Files:**
- Create: `internal/designsystem/hooks.go`
- Create: `internal/designsystem/sweep/rig_test.go`, `internal/designsystem/sweep/bar_test.go` (the fit in every theme × locale, and the bar's language menu)
- Create: `internal/designsystem/bar_browser_test.go` (focus clearance, painting, right to left)
- Modify: `internal/designsystem/galleryrig/rig.go`, `internal/designsystem/rig_browser_test.go`, `Makefile` (`browser` gains `./internal/designsystem/sweep/`)
- Modify (only if the fit drive says so): `internal/designsystem/gallery.css` (`--ds-bar-h`)

**Interfaces:**
- Consumes: Task 10's frame, `eagerly` (Task 2), `clickAll` and `clickedDesktop` (existing).
- Produces: the hooks `designsystem.PageKinds() []string`, `designsystem.PageFile(kind string) string`, `designsystem.PageHref(mount, theme, locale, file string) string`; in `galleryrig`, `func NoScripts(t *testing.T, parent context.Context) context.Context`, `const WithoutAnchorPositioning` (JS) and `func RequireAnchorPositioning(t *testing.T, ctx context.Context, where string, want bool)`; the package `internal/designsystem/sweep` with its short-name adapters in `rig_test.go`. Task 12 adds to all three.

- [ ] **Step 1: Write the drives**

Create `internal/designsystem/hooks.go`:

```go
package designsystem

// The few things the browser sweeps in internal/designsystem/sweep read
// from the renderer. The sweeps are a package of their own to keep this
// package's browser run inside its share of the CI time bound, and a
// test in another package can only reach what is exported. Each hook is
// the unexported function the gallery itself uses, so a sweep cannot
// build a URL or name a page differently from the gallery.

// PageKinds is every page kind, in the order the rail lists them.
func PageKinds() []string {
	out := make([]string, 0, len(pageKinds()))
	for _, pk := range pageKinds() {
		out = append(out, pk.Kind)
	}
	return out
}

// PageFile is one page kind's file name.
func PageFile(kind string) string { return fileOf(kind) }

// PageHref is one page's absolute address under mount.
func PageHref(mount, theme, locale, file string) string { return pageHref(mount, theme, locale, file) }
```

Append to `internal/designsystem/galleryrig/rig.go` (add the `emulation` import):

```go
// NoScripts is a tab with the page's scripts switched off at the
// engine. Evaluate still runs, through the debugger, so a leg can read
// a scriptless page without the page having run anything.
func NoScripts(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	ctx, cancel := chromedp.NewContext(parent)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("switching scripts off: %v", err)
	}
	return ctx
}

// WithoutAnchorPositioning replaces the page's tokens.css with a copy
// whose anchor-positioning @supports condition can never hold, which is
// what an engine without the feature gets: the menu panel positioned
// absolutely under its summary instead of fixed against it.
const WithoutAnchorPositioning = `(async () => {
  const link = document.querySelector('link[href$="/tokens.css"]');
  const css = await (await fetch(link.href)).text();
  const style = document.createElement("style");
  style.textContent = css.replaceAll("@supports (position-area: block-end) and (position-try-fallbacks: flip-block)", "@supports (position-area: rastrillo-no-such-value)");
  link.replaceWith(style);
  return true;
})()`

// RequireAnchorPositioning is the control for every leg that runs with
// and without the feature: the first menu panel on the page computes
// position: fixed when anchor positioning applies and absolute when it
// does not. A leg that asked for "without" and still got fixed would
// pass on the path it claims to have left.
func RequireAnchorPositioning(t *testing.T, ctx context.Context, where string, want bool) {
	t.Helper()
	var pos string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.querySelector("[rst-dropdown-menu]")).position`, &pos)); err != nil {
		t.Fatalf("%s: reading the menu panel's position: %v", where, err)
	}
	if (pos == "fixed") != want {
		t.Fatalf("%s: the menu panel is position: %s; this leg needs anchor positioning %v", where, pos, want)
	}
}
```

Create `internal/designsystem/sweep/rig_test.go`:

```go
//go:build browser

// Package sweep holds the gallery's browser sweeps that run over many
// themes, languages and widths with no axe in them. They are a package
// of their own because go test gives each package its own time limit,
// and the design-system package was already 7 to 8 minutes of its 20.
package sweep

import (
	"context"
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/designsystem"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// The gallery's hooks and the shared drive helpers, under the names
// these sweeps are written with. One line each: the implementations are
// the gallery's and galleryrig's.

const (
	mountPath   = designsystem.DefaultMount
	mountPrefix = mountPath + "/"
)

func pageHref(mount, theme, locale, file string) string {
	return designsystem.PageHref(mount, theme, locale, file)
}

func indexHref(mount, theme, locale string) string { return pageHref(mount, theme, locale, "index.html") }

func fileOf(kind string) string { return designsystem.PageFile(kind) }

func treeHandler(t *testing.T) http.Handler {
	t.Helper()
	files, err := designsystem.Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return galleryrig.Tree(t, files, mountPrefix)
}

func noScripts(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	return galleryrig.NoScripts(t, parent)
}

func requireAnchorPositioning(t *testing.T, ctx context.Context, where string, want bool) {
	t.Helper()
	galleryrig.RequireAnchorPositioning(t, ctx, where, want)
}

const withoutAnchorPositioning = galleryrig.WithoutAnchorPositioning
```

Create `internal/designsystem/sweep/bar_test.go`:

```go
//go:build browser

package sweep

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// barFit reads the bar's box and the union of what is inside it: the
// brand and each control. The bar's own box is fixed at --ds-bar-h, so
// it cannot measure its own overflow; its children can.
const barFit = `(() => {
  const bar = document.querySelector(".ds-top"), b = bar.getBoundingClientRect(), cs = getComputedStyle(bar);
  let top = Infinity, bottom = -Infinity, left = Infinity, right = -Infinity;
  for (const el of bar.querySelectorAll(".ds-top__brand, .ds-top__controls > *")) {
    const r = el.getBoundingClientRect();
    if (!r.width && !r.height) continue;
    top = Math.min(top, r.top); bottom = Math.max(bottom, r.bottom);
    left = Math.min(left, r.left); right = Math.max(right, r.right);
  }
  return JSON.stringify({Top: b.top, Bottom: b.bottom, Left: b.left, Right: b.right,
    UTop: top, UBottom: bottom, ULeft: left, URight: right, Over: bar.scrollWidth - bar.clientWidth,
    Need: bottom - b.top + parseFloat(cs.paddingBlockEnd) + parseFloat(cs.borderBlockEndWidth)});
})()`

type barReading struct {
	Top, Bottom, Left, Right, UTop, UBottom, ULeft, URight, Over, Need float64
}

// Every theme × locale, scripts on and off, at both edges of both
// bands: what is in the bar lies inside it, nothing in it overflows
// sideways, and it is still at the top after scrolling to the end. A
// failure names the height the band needs, which is the number to
// write into --ds-bar-h.
func TestThePinnedBarFitsItsReservation(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	need := map[string]float64{}
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, theme := range ui.ThemeNames() {
			for _, locale := range rastrillo.BaseLocales() {
				url := rig.Origin + pageHref(mountPath, theme, locale, fileOf("form"))
				if err := chromedp.Run(tab, chromedp.EmulateViewport(1440, 900), chromedp.Navigate(url), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery)); err != nil {
					t.Fatalf("%s/%s scripts=%v: loading: %v", theme, locale, scripts, err)
				}
				for _, w := range []int64{1440, 1024, 1023, 800} {
					where := fmt.Sprintf("%s/%s at %dpx, scripts %v", theme, locale, w, scripts)
					band := "1024 and up"
					if w < 1024 {
						band = "800 to 1023"
					}
					var raw string
					var after float64
					if err := chromedp.Run(tab,
						chromedp.EmulateViewport(w, 900),
						chromedp.Evaluate(`scrollTo(0, 0)`, nil),
						chromedp.Evaluate(barFit, &raw),
						chromedp.Evaluate(`scrollTo(0, document.documentElement.scrollHeight); document.querySelector(".ds-top").getBoundingClientRect().top`, &after),
					); err != nil {
						t.Fatalf("%s: reading the bar: %v", where, err)
					}
					var r barReading
					if err := json.Unmarshal([]byte(raw), &r); err != nil {
						t.Fatalf("%s: decoding %q: %v", where, raw, err)
					}
					need[band] = max(need[band], r.Need)
					if r.UTop < r.Top-0.5 || r.UBottom > r.Bottom+0.5 || r.ULeft < r.Left-0.5 || r.URight > r.Right+0.5 {
						t.Errorf("%s: the bar's contents [%v…%v × %v…%v] spill out of its box [%v…%v × %v…%v]; this band needs --ds-bar-h of at least %.0fpx", where, r.ULeft, r.URight, r.UTop, r.UBottom, r.Left, r.Right, r.Top, r.Bottom, r.Need)
					}
					if r.Over > 0.5 {
						t.Errorf("%s: the bar overflows sideways by %.1fpx", where, r.Over)
					}
					if after < -0.5 || after > 0.5 {
						t.Errorf("%s: scrolled to the end, the bar's top is at %.1fpx, not pinned at 0", where, after)
					}
				}
			}
		}
	}
	t.Logf("tallest bar contents per band (the --ds-bar-h each needs): %v", need)
}

// The bar's language menu, opened, lies inside the viewport and every
// language is reachable by keyboard: left to right and right to left,
// with anchor positioning and without, with scripts and without, at
// both edges of both bands.
func TestTheBarsLanguageMenuStaysOnScreen(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, locale := range []string{"en", "ar"} {
			for _, anchored := range []bool{true, false} {
				for _, w := range []int64{1440, 1024, 1023, 800} {
					where := fmt.Sprintf("day/%s at %dpx, scripts %v, anchor positioning %v", locale, w, scripts, anchored)
					if err := chromedp.Run(tab, chromedp.EmulateViewport(w, 900),
						chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", locale, fileOf("form"))),
						chromedp.WaitReady(`.ds-top__controls [rst-locale]`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if !anchored {
						if err := chromedp.Run(tab, chromedp.Evaluate(withoutAnchorPositioning, nil,
							func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
							t.Fatalf("%s: switching anchor positioning off: %v", where, err)
						}
					}
					requireAnchorPositioning(t, tab, where, anchored)
					var raw string
					if err := chromedp.Run(tab, chromedp.Evaluate(`(() => {
					  const d = document.querySelector(".ds-top__controls [rst-locale]");
					  d.open = true;
					  const p = d.querySelector("[rst-dropdown-menu]").getBoundingClientRect();
					  d.querySelector("summary").focus();
					  return JSON.stringify({L: p.left, R: p.right, T: p.top, B: p.bottom, W: innerWidth, H: innerHeight});
					})()`, &raw)); err != nil {
						t.Fatalf("%s: opening the menu: %v", where, err)
					}
					var p struct{ L, R, T, B, W, H float64 }
					if err := json.Unmarshal([]byte(raw), &p); err != nil {
						t.Fatalf("%s: decoding %q: %v", where, raw, err)
					}
					if p.L < -0.5 || p.T < -0.5 || p.R > p.W+0.5 || p.B > p.H+0.5 {
						t.Errorf("%s: the open panel [%v…%v × %v…%v] is not inside the %vx%v viewport", where, p.L, p.R, p.T, p.B, p.W, p.H)
					}
					for i := 0; i < len(rastrillo.BaseLocales()); i++ {
						var at string
						if err := chromedp.Run(tab, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`(() => {
						  const a = document.activeElement, r = a.getBoundingClientRect();
						  const inside = r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth;
						  return a.closest(".ds-top__controls [rst-dropdown-menu]") && inside ? a.lang : "not a visible language: " + a.outerHTML.slice(0, 60);
						})()`, &at)); err != nil {
							t.Fatalf("%s: tab %d: %v", where, i+1, err)
						}
						if want := rastrillo.BaseLocales()[i]; at != want {
							t.Errorf("%s: tab %d lands on %s, want the %s link on screen", where, i+1, at, want)
							break
						}
					}
				}
			}
		}
	}
}
```

Add to `internal/designsystem/rig_browser_test.go`:

```go
func noScripts(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	return galleryrig.NoScripts(t, parent)
}
```

Create `internal/designsystem/bar_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// focusTopJS is the top of whatever has focus, or of the element a
// fragment targets, in the page's coordinates. Inside a preview frame
// the element's offset is in the frame's own pixels, and the frame is
// drawn at scale(--ds-k), so the offset is multiplied by the frame's
// rendered width over its layout width before the frame's top is added.
const focusTopJS = `((el) => {
  let top = 0, k = 1, doc = document;
  el = el || document.activeElement;
  while (el && el.tagName === "IFRAME") {
    const r = el.getBoundingClientRect();
    top += r.top; k *= r.width / el.offsetWidth;
    const d = el.contentDocument;
    if (!d || !d.activeElement || d.activeElement === d.body) break;
    el = d.activeElement; doc = d;
  }
  const r = el.getBoundingClientRect();
  return JSON.stringify({Top: el.tagName === "IFRAME" ? top : top + r.top * k, What: el.tagName.toLowerCase() + (el.id ? "#" + el.id : ""),
    Bar: document.querySelector(".ds-top").getBoundingClientRect().bottom,
    End: Math.abs(scrollY + innerHeight - document.documentElement.scrollHeight) < 2});
})`

type focusReading struct {
	Top, Bar float64
	What     string
	End      bool
}

func readFocus(t *testing.T, ctx context.Context, where, target string) focusReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(focusTopJS+"("+target+")", &raw)); err != nil {
		t.Fatalf("%s: reading focus: %v", where, err)
	}
	var r focusReading
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return r
}

// Focus and fragment targets stop below the pinned bar, never under
// it (WCAG 2.4.11): forty Tabs on from the first control in main and
// forty back, the skip link's target, and every rail fragment of the
// page. A target near the end of the page may stop lower, because the
// browser clamps the scroll at the end, but never higher.
func TestFocusIsNeverUnderThePinnedBar(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, locale := range []string{"en", "ar"} {
		for _, w := range []int64{1440, 800} {
			where := fmt.Sprintf("day/%s form at %dpx", locale, w)
			url := rig.Origin + pageHref(mountPath, "day", locale, fileOf("form"))
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(w, 900), chromedp.Navigate(url), chromedp.WaitReady(`main`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			under := func(how string, r focusReading) {
				if r.Top < r.Bar-1 {
					t.Errorf("%s: %s puts %s at %.1fpx, under the bar ending at %.1fpx", where, how, r.What, r.Top, r.Bar)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("main [rst-page]").querySelector("a[href], button, summary, input:not([type=hidden])").focus(); true`, nil)); err != nil {
				t.Fatalf("%s: focusing main's first control: %v", where, err)
			}
			for i := 0; i < 40; i++ {
				if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
					t.Fatalf("%s: tab %d: %v", where, i+1, err)
				}
				under(fmt.Sprintf("tab %d", i+1), readFocus(t, ctx, where, ""))
			}
			for i := 0; i < 40; i++ {
				if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift))); err != nil {
					t.Fatalf("%s: shift+tab %d: %v", where, i+1, err)
				}
				under(fmt.Sprintf("shift+tab %d", i+1), readFocus(t, ctx, where, ""))
			}
			// The skip link's target.
			if err := chromedp.Run(ctx, chromedp.Evaluate(`scrollTo(0, 2000); document.querySelector("[rst-skip]").click(); true`, nil)); err != nil {
				t.Fatalf("%s: following the skip link: %v", where, err)
			}
			under("the skip link", readFocus(t, ctx, where, `document.getElementById("main")`))
			// Every rail fragment of this page.
			var hrefs []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll("#ds-nav a[href*='#']")].filter(a => a.pathname === location.pathname).map(a => a.hash.slice(1))`, &hrefs)); err != nil {
				t.Fatalf("%s: listing the rail's fragments: %v", where, err)
			}
			if len(hrefs) == 0 {
				t.Fatalf("%s: the rail links no fragment of this page", where)
			}
			for _, id := range hrefs {
				if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('#ds-nav a[href$="#%s"]').click(); true`, id), nil)); err != nil {
					t.Fatalf("%s: following #%s: %v", where, id, err)
				}
				r := readFocus(t, ctx, where, fmt.Sprintf(`document.getElementById(%q)`, id))
				under("#"+id, r)
				if !r.End && r.Top > r.Bar+40 {
					t.Errorf("%s: #%s lands at %.1fpx, far below the bar's %.1fpx with the page not at its end; the scroll padding is not what the fragment stopped at", where, id, r.Top, r.Bar)
				}
			}
		}
	}
}

// Nothing in the content paints over the pinned chrome: at 1440px the
// bar, and at 390px the shell's back strip, are what elementFromPoint
// finds at their centre and both inline edges, everywhere down a page
// of positioned boxes and transformed frames.
func TestNothingPaintsOverTheBarOrTheBackStrip(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 240*time.Second)
	defer cancel()
	for _, c := range []struct {
		w     int64
		strip string
	}{{1440, ".ds-top"}, {390, "[rst-shell-back]"}} {
		where := fmt.Sprintf("form at %dpx", c.w)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(c.w, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
			chromedp.WaitVisible(c.strip, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: loading: %v", where, err)
		}
		eagerly(t, ctx, where)
		clickAll(t, ctx, where, clickedDesktop, "Desktop")
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
		  const strip = document.querySelector(%q), bad = [];
		  for (let y = 0; y < document.documentElement.scrollHeight; y += 300) {
		    scrollTo(0, y);
		    const r = strip.getBoundingClientRect(), mid = (r.top + r.bottom) / 2;
		    for (const x of [r.left + 2, (r.left + r.right) / 2, r.right - 2]) {
		      const hit = document.elementFromPoint(x, mid);
		      if (!hit || !strip.contains(hit)) bad.push(y + ": " + (hit ? hit.tagName + "." + hit.className : "nothing"));
		    }
		  }
		  return JSON.stringify(bad.slice(0, 6));
		})()`, c.strip), &raw)); err != nil {
			t.Fatalf("%s: scanning: %v", where, err)
		}
		if raw != "[]" {
			t.Errorf("%s: something paints over %s: %s", where, c.strip, raw)
		}
	}
}

// Right to left, the shell's grid puts the rail on the right and the
// bar over main on the left, with no rule of the gallery's: the grid
// lines are numbered, not sided.
func TestTheFrameMirrorsRightToLeft(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "ar", fileOf("form"))),
		chromedp.WaitVisible(`.ds-top`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
		  const b = s => document.querySelector(s).getBoundingClientRect();
		  const rail = b("[rst-shell-rail]"), bar = b(".ds-top"), main = b("main");
		  return JSON.stringify({RailLeft: rail.left, MainRight: main.right, BarBottom: bar.bottom, MainTop: main.top, BarLeft: bar.left, BarRight: bar.right, MainLeft: main.left});
		})()`, &raw)); err != nil {
		t.Fatalf("day/ar form at 1280px: %v", err)
	}
	var r struct{ RailLeft, MainRight, BarBottom, MainTop, BarLeft, BarRight, MainLeft float64 }
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if r.RailLeft < r.MainRight-0.5 {
		t.Errorf("in Arabic the rail starts at %.1fpx, left of main's right edge %.1fpx; it belongs on the right", r.RailLeft, r.MainRight)
	}
	if r.BarBottom > r.MainTop+0.5 || r.BarLeft < r.MainLeft-0.5 || r.BarRight > r.MainRight+0.5 {
		t.Errorf("in Arabic the bar [%v…%v, bottom %v] is not above main [%v…%v, top %v]", r.BarLeft, r.BarRight, r.BarBottom, r.MainLeft, r.MainRight, r.MainTop)
	}
}
```

In the `Makefile`'s `browser` target, add `./internal/designsystem/sweep/` after `./internal/designsystem/galleryrig/`.

- [ ] **Step 2: Run them**

```bash
set -o pipefail; mkdir -p "$TMPDIR/gate"
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestThePinnedBarFitsItsReservation|TestTheBarsLanguageMenuStaysOnScreen|TestFocusIsNeverUnderThePinnedBar|TestNothingPaintsOverTheBarOrTheBackStrip|TestTheFrameMirrorsRightToLeft' -count=1 -v -timeout 20m ./internal/designsystem/ ./internal/designsystem/sweep/ 2>&1 | tee "$TMPDIR/gate/task11-drives.log"; echo "exit $?"
grep -E '^(---|ok|FAIL)|tallest bar|spill|under the bar|paints over|not inside|lands on' "$TMPDIR/gate/task11-drives.log"
```

Expected: `exit 0`, every test PASS, and the log line `tallest bar contents per band`. If the fit drive fails, set each band's `--ds-bar-h` in `gallery.css` to that band's logged value rounded up to the next quarter rem (4px), in rem, and run again. Each value is a measurement, so give it the comment `measured over every theme and locale on <date>: <N>px`. If the focus drive fails at 800px only, the bar is taller than its reservation there. The fix is the same value, not a different scroll padding.

- [ ] **Step 3: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 4: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="Makefile \
  internal/designsystem/hooks.go \
  internal/designsystem/bar_browser_test.go \
  internal/designsystem/rig_browser_test.go \
  internal/designsystem/galleryrig/rig.go \
  internal/designsystem/sweep/rig_test.go \
  internal/designsystem/sweep/bar_test.go"
maybe=".rastrillo/budgets.txt internal/designsystem/gallery.css"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Drive the pinned bar in every theme and language

The bar's box is fixed at --ds-bar-h, so its own size cannot report
an overflow. The drive measures what is inside it instead, in every
theme and locale, with scripts on and off, at both edges of both
bands, and the reservation is the measured tallest. Other drives hold
the language menu on screen and every language reachable by keyboard,
with and without anchor positioning. Focus and fragment targets stop
below the bar. Nothing in a page of transformed frames paints over the
bar or the phone's back strip. In Arabic the rail is on the right with
no rule of the gallery's.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 12: The phone index, driven: rows, the way back, every language within reach, and axe on both views

Spec Tests "The phone index and the way back" (all but the filter sub-leg, which needs Task 13's synonyms and lands there) and "Every language is reachable from the phone index". This replaces the drawer-era legs the spec retired before they were written.

**Files:**
- Create: `internal/designsystem/index_browser_test.go` (the index and the way back, axe on both views)
- Create: `internal/designsystem/sweep/languages_test.go` (every language, 16 combinations)
- Modify: `internal/designsystem/galleryrig/rig.go`, `internal/designsystem/rig_browser_test.go`, `internal/designsystem/sweep/rig_test.go`

**Interfaces:**
- Consumes: Task 10's frame; `noScripts`, `withoutAnchorPositioning` and `requireAnchorPositioning` (Task 11); `until` (Task 6); `axeSource`, `paint`, `scan` and `report` (existing, `a11y_test.go`).
- Produces: in `galleryrig`, `func PhoneRig(t *testing.T, tree func() http.Handler) *harness.Rig`, `func RequireCoarse(t *testing.T, ctx context.Context)`, `func SettleMotion(t *testing.T, ctx context.Context, where string)`; their short names `phoneRig`, `requireCoarse` (both packages) and `settleMotion` (`internal/designsystem`). Tasks 13, 15 and 17 use them.

- [ ] **Step 1: Write the drives**

Append to `internal/designsystem/galleryrig/rig.go` (add the `harness` import):

```go
// PhoneRig is a browser whose primary pointer is a touch screen,
// serving what tree builds. It is a launch flag: CDP's touch emulation
// leaves (pointer: coarse) false in this engine.
func PhoneRig(t *testing.T, tree func() http.Handler) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler { return tree() }, harness.WithCoarsePointer())
}

// RequireCoarse is every touch leg's control: a leg running on a fine
// pointer would measure desktop sizes and pass.
func RequireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	var coarse bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`matchMedia("(pointer: coarse)").matches`, &coarse)); err != nil {
		t.Fatalf("reading the pointer: %v", err)
	}
	if !coarse {
		t.Fatal("(pointer: coarse) is false; this rig is not the phone this leg claims to measure")
	}
}

// SettleMotion waits out every finite animation and any view
// transition: the shell slides between the index and a page, and the
// row the reader came back to flashes. axe reading either mid-flight
// measures colours at partial opacity that no reader sees once it has
// landed.
func SettleMotion(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => {
	  if (document.activeViewTransition) await document.activeViewTransition.finished.catch(() => null);
	  await Promise.all(document.getAnimations()
	    .filter(a => a.effect && a.effect.getComputedTiming().endTime !== Infinity)
	    .map(a => a.finished.catch(() => null)));
	  return true;
	})()`, nil, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("%s: waiting for motion to end: %v", where, err)
	}
}
```

Add to `internal/designsystem/rig_browser_test.go` (add the `harness` import), and the same two functions to `internal/designsystem/sweep/rig_test.go` (add its `harness` import):

```go
func phoneRig(t *testing.T) *harness.Rig {
	t.Helper()
	return galleryrig.PhoneRig(t, func() http.Handler { return treeHandler(t) })
}

func requireCoarse(t *testing.T, ctx context.Context) {
	t.Helper()
	galleryrig.RequireCoarse(t, ctx)
}
```

and to `rig_browser_test.go` alone:

```go
func settleMotion(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.SettleMotion(t, ctx, where)
}
```

Create `internal/designsystem/index_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/ui"
)


// boxOf says whether the first element matching sel takes up room.
const boxOf = `(sel => { const e = document.querySelector(sel); if (!e) return false; const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0; })`

// focusID names whatever has focus the way the expected sequences below
// are written: an input or a row by id, a link by its href, a scheme
// button by its value, a summary by its tag.
const focusID = `(() => { const a = document.activeElement;
  if (!a || a === document.body) return "body";
  if (a.id) return "#" + a.id;
  if (a.dataset && a.dataset.dsScheme) return "scheme:" + a.dataset.dsScheme;
  if (a.tagName === "A") return a.getAttribute("href");
  return a.tagName.toLowerCase(); })()`

func TestThePhoneIndexAndTheWayBack(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, locale := range []string{"en", "ar"} {
		where := "day/" + locale + " at 390px"
		index := rig.Origin + indexHref(mountPath, "day", locale)
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(index), chromedp.WaitVisible(`.ds-index`, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: loading the index: %v", where, err)
		}
		requireCoarse(t, ctx)

		// The Overview is the index: the rail is the page, main and the
		// bar have no box, the one visible h1 is the index title, and the
		// rows are the page kinds in table order, then Demos.
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		  const box = `+boxOf+`;
		  const h1s = [...document.querySelectorAll("h1")].filter(h => h.getBoundingClientRect().height > 0).map(h => h.textContent.trim());
		  const rows = [...document.querySelectorAll(".ds-index > a")].map(a => ({T: a.textContent.trim(), H: a.getBoundingClientRect().height, ID: a.id}));
		  return JSON.stringify({Rail: box("[rst-shell-rail]"), Main: box("main"), Bar: box(".ds-top"), Lead: box(".ds-index-lead"), H1: h1s,
		    Group: (document.querySelector(".ds-index > [rst-shell-group]") || {}).textContent || "", Rows: rows});
		})()`, &raw)); err != nil {
			t.Fatalf("%s: reading the index: %v", where, err)
		}
		var ix struct {
			Rail, Main, Bar, Lead bool
			H1                    []string
			Group                 string
			Rows                  []struct {
				T, ID string
				H     float64
			}
		}
		if err := json.Unmarshal([]byte(raw), &ix); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, raw, err)
		}
		if !ix.Rail || ix.Main || ix.Bar || !ix.Lead {
			t.Errorf("%s: rail %v, main %v, bar %v, lead %v; the index is the rail and its lead alone", where, ix.Rail, ix.Main, ix.Bar, ix.Lead)
		}
		if len(ix.H1) != 1 || ix.H1[0] != proseIn(locale, "rastrillo design system") {
			t.Errorf("%s: visible h1s %q, want exactly the index title", where, ix.H1)
		}
		var want []string
		for _, pk := range pageKinds() {
			if pk.Kind != "overview" {
				want = append(want, proseIn(locale, pk.Title))
			}
		}
		if ix.Group != proseIn(locale, "Demos") {
			t.Errorf("%s: the rows' group label is %q, want Demos", where, ix.Group)
		}
		if len(ix.Rows) < len(want) {
			t.Fatalf("%s: %d rows, want at least %d", where, len(ix.Rows), len(want))
		}
		for i, w := range want {
			if ix.Rows[i].T != w {
				t.Errorf("%s: row %d is %q, want %q", where, i, ix.Rows[i].T, w)
			}
		}
		for _, r := range ix.Rows {
			if r.H < 44 {
				t.Errorf("%s: the row %q is %.1fpx tall, under a 44px target", where, r.T, r.H)
			}
		}

		// Tab runs filter, rows, then the foot's controls, in screen order.
		var seq []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement && document.activeElement.blur(); scrollTo(0, 0); true`, nil)); err != nil {
			t.Fatalf("%s: resetting focus: %v", where, err)
		}
		var expect []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`["#ds-filter", ...[...document.querySelectorAll(".ds-index > a")].map(a => a.id ? "#" + a.id : a.getAttribute("href")),
		  ...[...document.querySelectorAll("#ds-prefs [rst-seg-tabs] a")].map(a => a.getAttribute("href")),
		  "scheme:system", "scheme:light", "scheme:dark", "summary"]`, &expect)); err != nil {
			t.Fatalf("%s: listing the expected order: %v", where, err)
		}
		for range expect {
			var at string
			if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(focusID, &at)); err != nil {
				t.Fatalf("%s: tabbing: %v", where, err)
			}
			seq = append(seq, at)
		}
		if strings.Join(seq, " ") != strings.Join(expect, " ") {
			t.Errorf("%s: Tab order\n got %v\nwant %v", where, seq, expect)
		}
		var short []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll("#ds-prefs [data-ds-scheme]")].filter(b => b.getBoundingClientRect().height < 44).map(b => b.dataset.dsScheme)`, &short)); err != nil {
			t.Fatalf("%s: measuring the scheme buttons: %v", where, err)
		}
		if len(short) > 0 {
			t.Errorf("%s: scheme buttons under 44px: %v", where, short)
		}

		// A row opens its page as a content page: Back first after the
		// skip link, and no rail, bar or switcher with a box.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("nav-form").click(); true`, nil)); err != nil {
			t.Fatalf("%s: tapping the Form row: %v", where, err)
		}
		until(t, ctx, where+", Form loaded", `location.pathname.endsWith("/form.html") && document.readyState === "complete"`)
		var page string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const box = `+boxOf+`;
		  const back = document.querySelector("[rst-skip] + [rst-shell-back] > a");
		  return JSON.stringify({Back: !!back && box("[rst-shell-back]"), Href: back ? back.getAttribute("href") : "",
		    Rail: box("[rst-shell-rail]"), Bar: box(".ds-top"),
		    Switch: [...document.querySelectorAll("[data-ds-scheme], [rst-seg-tabs] a, [rst-locale]")].some(e => e.getBoundingClientRect().width > 0)});
		})()`, &page)); err != nil {
			t.Fatalf("%s: reading Form: %v", where, err)
		}
		var cp struct {
			Back, Rail, Bar, Switch bool
			Href                    string
		}
		if err := json.Unmarshal([]byte(page), &cp); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, page, err)
		}
		if !cp.Back || cp.Rail || cp.Bar || cp.Switch {
			t.Errorf("%s: Form shows back %v, rail %v, bar %v, a switcher %v; a content page is the way back and the content", where, cp.Back, cp.Rail, cp.Bar, cp.Switch)
		}
		if want := indexHref(mountPath, "day", locale) + "#nav-form"; cp.Href != want {
			t.Errorf("%s: Back goes to %q, want %q", where, cp.Href, want)
		}

		// Back returns to the index with the Form row focused. This is a
		// history traversal, so the leg polls and clicks through the page.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("[rst-shell-back] a").click(); true`, nil)); err != nil {
			t.Fatalf("%s: going back: %v", where, err)
		}
		until(t, ctx, where+", back on the index", `location.pathname.endsWith("/index.html") && document.activeElement && document.activeElement.id === "nav-form"`)

		// A foot theme link lands on the other theme's index with the
		// controls on screen.
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#ds-prefs [rst-seg-tabs] a[href*="/signal/"]').click(); true`, nil)); err != nil {
			t.Fatalf("%s: switching theme in the foot: %v", where, err)
		}
		until(t, ctx, where+", switched to signal", `location.pathname.includes("/signal/") && location.hash === "#ds-prefs" && (() => { const r = document.getElementById("ds-prefs").getBoundingClientRect(); return r.top >= 0 && r.top < innerHeight; })()`)
	}

	// Scripts off: Back is the link, the row is the :target, and the
	// next Tab lands on the row after it.
	off := noScripts(t, ctx)
	where := "day/en at 390px, scripts off"
	if err := chromedp.Run(off, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`[rst-shell-back]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector("[rst-shell-back] a").click(); true`, nil)); err != nil {
		t.Fatalf("%s: going back: %v", where, err)
	}
	until(t, off, where, `location.hash === "#nav-form" && document.querySelector(":target") && document.querySelector(":target").id === "nav-form"`)
	var next, after string
	if err := chromedp.Run(off,
		chromedp.Evaluate(`document.getElementById("nav-form").nextElementSibling.id || document.getElementById("nav-form").nextElementSibling.getAttribute("href")`, &after),
		chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(focusID, &next)); err != nil {
		t.Fatalf("%s: tabbing from the target: %v", where, err)
	}
	if strings.TrimPrefix(next, "#") != after {
		t.Errorf("%s: the Tab after Back lands on %s, want the row after Form, %s", where, next, after)
	}
}

// axe on both phone views, every theme and both schemes, motion
// settled first.
func TestA11yScansThePhoneViews(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for _, theme := range ui.ThemeNames() {
		for _, view := range []struct{ name, file, ready string }{{"index", "index.html", ".ds-index"}, {"content page", "form.html", "[rst-shell-back]"}} {
			for _, scheme := range a11ySchemes {
				where := fmt.Sprintf("%s/en %s at 390px (%s)", theme, view.name, scheme)
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
					chromedp.Navigate(rig.Origin+pageHref(mountPath, theme, "en", view.file)),
					chromedp.WaitVisible(view.ready, chromedp.ByQuery)); err != nil {
					t.Fatalf("%s: loading: %v", where, err)
				}
				requireCoarse(t, ctx)
				settleMotion(t, ctx, where)
				if err := chromedp.Run(ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
					t.Fatalf("%s: loading axe: %v", where, err)
				}
				paint(t, ctx, scheme)
				total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
			}
		}
	}
	if total == 0 {
		t.Logf("clean: both phone views in %d themes × %d schemes", len(ui.ThemeNames()), len(a11ySchemes))
	}
}
```

Create `internal/designsystem/sweep/languages_test.go`:

```go
//go:build browser

package sweep

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
)

// Every language in the index foot's menu can be reached: by Tab, with
// scripts, and by scrolling the way a finger would, the panel's own
// scroll and then the document's, in every combination. Which way the
// panel opens is not asserted: with anchor positioning it may flip
// upward, and without it, it opens downward and extends the document.
// The index is scrolled to its end first, the case with no room below.
func TestEveryLanguageIsReachableFromThePhoneIndex(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	for _, scripts := range []bool{true, false} {
		tab := ctx
		if !scripts {
			tab = noScripts(t, ctx)
		}
		for _, w := range []int64{390, 320} {
			for _, locale := range []string{"en", "ar"} {
				for _, anchored := range []bool{true, false} {
					where := fmt.Sprintf("day/%s index at %dpx, scripts %v, anchor positioning %v", locale, w, scripts, anchored)
					if err := chromedp.Run(tab, chromedp.EmulateViewport(w, 700),
						chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", locale)),
						chromedp.WaitVisible(`#ds-prefs`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if !anchored {
						if err := chromedp.Run(tab, chromedp.Evaluate(withoutAnchorPositioning, nil,
							func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
							t.Fatalf("%s: switching anchor positioning off: %v", where, err)
						}
					}
					requireAnchorPositioning(t, tab, where, anchored)
					requireCoarse(t, tab)
					if err := chromedp.Run(tab, chromedp.Evaluate(`scrollTo(0, document.documentElement.scrollHeight);
					  const d = document.querySelector("#ds-prefs [rst-locale]"); d.open = true; d.querySelector("summary").focus(); true`, nil)); err != nil {
						t.Fatalf("%s: opening the menu: %v", where, err)
					}
					for i, code := range rastrillo.BaseLocales() {
						if scripts {
							var at string
							if err := chromedp.Run(tab, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(`(() => { const a = document.activeElement, r = a.getBoundingClientRect();
							  return r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth ? a.lang : "off screen: " + a.outerHTML.slice(0, 60); })()`, &at)); err != nil {
								t.Fatalf("%s: tab %d: %v", where, i+1, err)
							}
							if at != code {
								t.Errorf("%s: tab %d is %s, want the %s link on screen", where, i+1, at, code)
							}
						}
						var hit bool
						if err := chromedp.Run(tab, chromedp.Evaluate(fmt.Sprintf(`(() => {
						  const a = document.querySelector('#ds-prefs [rst-dropdown-menu] a[lang=%q]');
						  a.scrollIntoView({block: "nearest", inline: "nearest"});
						  const r = a.getBoundingClientRect();
						  return document.elementFromPoint((r.left + r.right) / 2, (r.top + r.bottom) / 2) === a;
						})()`, code), &hit)); err != nil {
							t.Fatalf("%s: scrolling to %s: %v", where, code, err)
						}
						if !hit {
							t.Errorf("%s: scrolled into view, the %s link is not what a finger at its centre touches", where, code)
						}
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run them**

```bash
set -o pipefail; mkdir -p "$TMPDIR/gate"
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestThePhoneIndexAndTheWayBack|TestA11yScansThePhoneViews|TestEveryLanguageIsReachableFromThePhoneIndex|TestA11yReflowsAt320' -count=1 -v -timeout 20m ./internal/designsystem/ ./internal/designsystem/sweep/ 2>&1 | tee "$TMPDIR/gate/task12-drives.log"; echo "exit $?"
grep -E '^(--- |ok|FAIL)' "$TMPDIR/gate/task12-drives.log"
```

Expected: `exit 0`, every test PASS. A failure here is the frame's (Task 10) or the framework's. A framework failure, for example a language unreachable with anchor positioning off at 320px, goes to the controller with the drive's message, because `tokens.css` is not this branch's to edit.

- [ ] **Step 3: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 4: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/index_browser_test.go \
  internal/designsystem/rig_browser_test.go \
  internal/designsystem/galleryrig/rig.go \
  internal/designsystem/sweep/rig_test.go \
  internal/designsystem/sweep/languages_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Drive the gallery's phone index and its way back

On a phone the gallery now navigates by the shell's index and back
control, and these drives hold what that promises at 390px under a
touch pointer, in English and Arabic. The rows are the sections in
order and each is at least 44px. Tab runs filter, rows, then the
controls. A row opens a content page with Back first, and Back returns
with the row focused, or targeted when scripts are off. A theme link
in the foot lands with the controls on screen. Every language is
reachable at 390 and 320px, with anchor positioning and without, by
keyboard and by scrolling. axe scans both views in every theme and
scheme.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 13: Search finds what people call things

Spec 2.4 (synonyms keyed by anchor id, English only, ride on rail links as `data-ds-terms`, plain page links filter like every entry), the Tests entry "Search terms", and the filter sub-leg of "The phone index and the way back".

**Files:**
- Modify: `internal/designsystem/samples.go` (`searchTerms`)
- Modify: `internal/designsystem/page.go`: `navItem.Terms`, `galleryNav`, and the rail link in `pageTemplate`
- Modify: `internal/designsystem/gallery.js` (the filter section)
- Test: `internal/designsystem/designsystem_test.go` (gallery.js cap comment and a terms gate), `internal/designsystem/index_browser_test.go` (a search leg)

**Interfaces:**
- Consumes: Task 8's `idiom-button` anchor; `phoneRig`, `requireCoarse` (Task 12); `until` (Task 6).
- Produces: `var searchTerms map[string]string` (anchor id to space-separated words), `navItem.Terms string`, rail links carrying `data-ds-terms="…"`.

- [ ] **Step 1: Write the failing tests**

Append to `designsystem_test.go`:

```go
// Every synonym names an anchor that exists, and rides on the rail
// link to it. A synonym whose anchor was renamed would leave its words
// pointing at nothing, and a reader typing "checkbox" would see No
// matches again.
func TestEverySearchTermNamesAnAnchor(t *testing.T) {
	files := render(t)
	var all, rail string
	for _, name := range galleryFiles(RootTheme(), "en") {
		all += string(files[name])
	}
	rail = railOf(t, "overview", galleryPage(t, files, RootTheme(), "en", "overview"))
	for id, terms := range searchTerms {
		if !strings.Contains(all, `id="`+id+`" data-ds-anchor`) {
			t.Errorf("searchTerms names %q, which no page anchors", id)
		}
		if !regexp.MustCompile(`<a href="[^"]*#` + regexp.QuoteMeta(id) + `"[^>]* data-ds-terms="` + regexp.QuoteMeta(terms) + `"`).MatchString(rail) {
			t.Errorf("the rail link to %s does not carry its terms %q", id, terms)
		}
	}
	if len(searchTerms) == 0 {
		t.Fatal("no search terms")
	}
}
```

Append to `index_browser_test.go` (add the `net/http` and `harness` imports):

```go
// The filter finds a component by what a reader calls it, on desktop
// and on the phone index: "checkbox" finds field-check, "dialog" the
// modal, "button" the Buttons entry, and junk says No matches with no
// page link left showing beside it. On the index, a query swaps the
// rows for the tree it filters, and a result opens its section with
// the partial in view.
func TestSearchFindsWhatPeopleCallThings(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	visible := func(sel string) string {
		return `(() => { const e = document.querySelector('` + sel + `'); return !!e && e.getBoundingClientRect().height > 0; })()`
	}
	typeInto := func(where, q string) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const i = document.getElementById("ds-filter"); i.value = ""; i.dispatchEvent(new Event("input")); return true; })()`, nil),
			chromedp.SendKeys(`#ds-filter`, q, chromedp.ByQuery)); err != nil {
			t.Fatalf("%s: typing %q: %v", where, q, err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form: %v", err)
	}
	for q, id := range map[string]string{"checkbox": "partial-field-check", "dialog": "idiom-modal", "button": "idiom-button", "CHECKBOX": "partial-field-check"} {
		where := "desktop, " + q
		typeInto(where, q)
		until(t, ctx, where, visible(`#ds-nav a[href$="#`+id+`"]`))
	}
	typeInto("desktop, junk", "zzqxv")
	until(t, ctx, "desktop, junk", visible(`[data-ds-filter-empty]`)+` && ![...document.querySelectorAll("#ds-nav a")].some(a => a.getBoundingClientRect().height > 0)`)

	phone := phoneRig(t)
	pctx, pcancel := context.WithTimeout(phone.Context(), 120*time.Second)
	defer pcancel()
	where := "the phone index"
	if err := chromedp.Run(pctx, chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(phone.Origin+indexHref(mountPath, "day", "en")),
		chromedp.WaitVisible(`#ds-filter`, chromedp.ByQuery),
		chromedp.SendKeys(`#ds-filter`, "checkbox", chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: typing: %v", where, err)
	}
	requireCoarse(t, pctx)
	until(t, pctx, where+", rows swapped for the tree", `document.querySelector(".ds-index").getBoundingClientRect().height === 0 && `+visible(`#ds-nav a[href$="#partial-field-check"]`))
	if err := chromedp.Run(pctx, chromedp.Evaluate(`(() => { const i = document.getElementById("ds-filter"); i.value = ""; i.dispatchEvent(new Event("input")); return true; })()`, nil)); err != nil {
		t.Fatalf("%s: clearing: %v", where, err)
	}
	until(t, pctx, where+", rows back", visible(`.ds-index`))
	if err := chromedp.Run(pctx, chromedp.SendKeys(`#ds-filter`, "checkbox", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#ds-nav a[href$="#partial-field-check"]').click(); true`, nil)); err != nil {
		t.Fatalf("%s: following the result: %v", where, err)
	}
	until(t, pctx, where+", Form at the partial", `location.pathname.endsWith("/form.html") && location.hash === "#partial-field-check" && (() => { const r = document.getElementById("partial-field-check").getBoundingClientRect(); return r.top >= 0 && r.top < innerHeight; })()`)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run TestEverySearchTermNamesAnAnchor -count=1 ./internal/designsystem/`
Expected: FAIL to compile, `undefined: searchTerms`.

- [ ] **Step 3: Add the terms**

In `samples.go`, after `tones`:

```go
// searchTerms are the words people type for a component whose name is
// not their word, keyed by the anchor id the rail links (anchorID's
// kind-prefixed id). They ride on the rail link and the filter matches
// them beside the name. English and untranslated: these are the
// web-platform words a developer types, on the same footing as the
// partial names, which are code. A matching sentence (the Blurb) was
// measured instead and would have put 11 KB of Hindi into every rail.
// TestEverySearchTermNamesAnAnchor fails on an id that no longer exists.
var searchTerms = map[string]string{
	"partial-field-check":  "checkbox toggle switch",
	"idiom-modal":          "dialog popup",
	"partial-choice-field": "radio",
	"idiom-list-grid":      "table",
	"idiom-box":            "card panel",
	"partial-person":       "avatar user",
	"idiom-button":         "button submit cta",
}
```

In `page.go`, add to `navItem` after `Aria`:

```go
	// Terms are the synonyms the filter matches beside the label, from
	// searchTerms; empty for most entries.
	Terms string
```

In `galleryNav`, after `section.Items = append(…)`, add:

```go
			for i := range section.Items {
				if _, id, ok := strings.Cut(section.Items[i].Href, "#"); ok {
					section.Items[i].Terms = searchTerms[id]
				}
			}
```

In `pageTemplate`'s rail link, insert `{{if .Terms}} data-ds-terms="{{.Terms}}"{{end}}` after `{{if .Blank}} target="_blank" rel="noopener"{{end}}`.

- [ ] **Step 4: Match the terms, and the page links, in `gallery.js`**

In the filter section, replace the line `for (var i = 0; i < links.length; i++) links[i].dsText = fold(links[i].textContent);` with:

```js
    // An entry's synonyms (data-ds-terms) match beside its name, and
    // the plain page links filter like every entry, so No matches and
    // a visible Overview link never show together.
    for (var i = 0; i < links.length; i++) links[i].dsText = fold(links[i].textContent + " " + (links[i].getAttribute("data-ds-terms") || ""));
    var pages = nav.querySelectorAll(":scope > a");
```

In `run`, before `if (!q) chosen = null;`, add:

```js
      for (var p = 0; p < pages.length; p++) {
        pages[p].hidden = q && pages[p].dsText.indexOf(q) < 0;
        if (q && !pages[p].hidden) found = true;
      }
```

In `designsystem_test.go`'s `TestGalleryScriptStaysInertAndFirstParty`, add to the cap comment's running totals:

```go
	//   search terms and the plain page links            +467 12,975
```

- [ ] **Step 5: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/ && RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestSearchFindsWhatPeopleCallThings|TestTheSidebarFilterDrivesTheWholeJourney' -count=1 ./internal/designsystem/`
Expected: PASS, and `wc -c internal/designsystem/gallery.js` prints 12,975. `TestTheRailIsTheSameOnEveryPage` still passes, because the terms are the same on every page.

- [ ] **Step 6: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 7: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/samples.go \
  internal/designsystem/page.go \
  internal/designsystem/gallery.js \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/index_browser_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Let the rail filter find components by what people call them

\"button\", \"checkbox\", \"toggle\", \"dialog\", \"table\", \"card\"
and \"avatar\" all said No matches, because the filter matched names
only. A short English synonym list rides on the rail links. It costs
about 350 bytes a page, against 11 KB for matching each entry's
translated blurb. The plain page links now filter too, so No matches
and a visible Overview link cannot show together. On the phone index
a query swaps the rows for the filtered tree.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---

### Task 14: One view choice for the whole page

Spec 2.6, and Tests "Page-wide view", "No code block scrolls sideways at 390px" and "Axe with Code selected". Copy: C8 (`gallery.view.label`) and C9 (`gallery.view.auto`). The Review Focus item for storage that throws is pinned here.

**Files:**
- Modify: `internal/designsystem/page.go`: `pageView.ViewGroup`, `renderGallery`, `pageTemplate`
- Modify: `internal/designsystem/gallery.js` (a page-wide view section), `internal/designsystem/gallery.css` (`.ds-viewall`, and the touch rule)
- Modify: `internal/designsystem/prose.go` (two keys)
- Test: `internal/designsystem/designsystem_test.go`, `internal/designsystem/code_browser_test.go`

**Interfaces:**
- Consumes: Task 5's Code panel; `addInit` and `until` (Task 6); `noScripts` (Task 11); `axeSource`, `paint`, `scan`, `report` (existing).
- Produces: `div.ds-viewall[role=group]` with `button[data-ds-view=auto|desktop|mobile|code]`, written on pages with at least one Code tab, after the page header. The `rst-ds-view` storage key (`desktop`, `mobile` or `code`; Auto removes it). `gallery.js` applies a stored view at `DOMContentLoaded`, which Task 15 depends on. JS helper: `const pressView = (v) => …` in tests.

- [ ] **Step 1: Check the approved copy this task writes**

```bash
for id in gallery.view.label gallery.view.auto; do jq -r --arg id $id '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json; done
```

Expected: `Show every example as`, then `Auto`. Stop and report if either differs.

- [ ] **Step 2: Write the failing tests**

Append to `designsystem_test.go`:

```go
// The page-wide group is on exactly the pages that have a Code tab,
// Auto pressed as rendered, labelled in the page's language, and
// hidden until gallery.js says it can work.
func TestThePageWideViewIsOnEveryPageWithCode(t *testing.T) {
	files := render(t)
	for _, locale := range []string{"en", "ja"} {
		for _, pk := range pageKinds() {
			page := galleryPage(t, files, RootTheme(), locale, pk.Kind)
			hasCode := strings.Contains(page, "ds-view__tab--c")
			group := regexp.MustCompile(`<div class="ds-viewall" role="group" aria-label="([^"]*)">(.*?)</div>`).FindStringSubmatch(page)
			if hasCode != (group != nil) {
				t.Errorf("%s/%s: Code tab %v, page-wide group %v; the group is for pages with a Code tab", locale, pk.Kind, hasCode, group != nil)
				continue
			}
			if group == nil {
				continue
			}
			if group[1] != template.HTMLEscapeString(proseIn(locale, "Show every example as")) {
				t.Errorf("%s/%s: the group is labelled %q", locale, pk.Kind, group[1])
			}
			for _, v := range []string{`data-ds-view="auto" aria-pressed="true"`, `data-ds-view="desktop" aria-pressed="false"`, `data-ds-view="mobile" aria-pressed="false"`, `data-ds-view="code" aria-pressed="false"`} {
				if !strings.Contains(group[2], v) {
					t.Errorf("%s/%s: the group has no %s", locale, pk.Kind, v)
				}
			}
		}
	}
	css := string(GalleryCSS())
	if !strings.Contains(css, ".ds-viewall { display: none; }") || !strings.Contains(css, ":root[data-rst-js] .ds-viewall") {
		t.Error("the page-wide group is not hidden until gallery.js sets data-rst-js")
	}
}
```

Append to `code_browser_test.go`:

```go
// viewState is the page-wide group's pressed button and every widget's
// checked radio: "d", "m", "c", or "" for none.
const viewState = `JSON.stringify({
  Pressed: [...document.querySelectorAll(".ds-viewall [aria-pressed=true]")].map(b => b.dataset.dsView),
  Widgets: [...document.querySelectorAll(".ds-view")].map(v => { const i = v.querySelector(".ds-view__tab input:checked"); return i ? i.closest(".ds-view__tab").className.slice(-1) : ""; }),
  Stored: (() => { try { return localStorage.getItem("rst-ds-view") || ""; } catch (e) { return "throws"; } })()})`

type viewReading struct {
	Pressed []string
	Widgets []string
	Stored  string
}

func readView(t *testing.T, ctx context.Context, where string) viewReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(viewState, &raw)); err != nil {
		t.Fatalf("%s: reading the view: %v", where, err)
	}
	var v viewReading
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return v
}

func press(t *testing.T, ctx context.Context, where, view string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="`+view+`"]').click(); true`, nil)); err != nil {
		t.Fatalf("%s: pressing %s: %v", where, view, err)
	}
}

func every(ws []string, want string) bool {
	for _, w := range ws {
		if w != want {
			return false
		}
	}
	return len(ws) > 0
}

func TestThePageWideViewAppliesToEveryWidget(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("form"))
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(form), chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form: %v", err)
	}
	if v := readView(t, ctx, "fresh"); len(v.Pressed) != 1 || v.Pressed[0] != "auto" || !every(v.Widgets, "") {
		t.Fatalf("fresh: %+v, want Auto pressed and nothing checked", v)
	}
	press(t, ctx, "Code", "code")
	if v := readView(t, ctx, "Code"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") || v.Stored != "code" {
		t.Errorf("after Code: %+v, want Code pressed, every widget on Code, code stored", v)
	}
	// A reader choosing Mobile in one widget leaves nothing pressed: the
	// page is mixed, and the group says only what is true of every widget.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector(".ds-view__tab--m input").click(); true`, nil)); err != nil {
		t.Fatalf("a local Mobile: %v", err)
	}
	until(t, ctx, "mixed", `document.querySelectorAll(".ds-viewall [aria-pressed=true]").length === 0`)
	// Pressing Code again re-applies it, the case a radio cannot express.
	press(t, ctx, "Code again", "code")
	if v := readView(t, ctx, "Code again"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") {
		t.Errorf("after Code again: %+v", v)
	}
	// The next page opens in Code.
	if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("display"))), chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Display: %v", err)
	}
	if v := readView(t, ctx, "the next page"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") {
		t.Errorf("Display after Code on Form: %+v, want it opened in Code", v)
	}
	press(t, ctx, "Auto", "auto")
	if v := readView(t, ctx, "Auto"); len(v.Pressed) != 1 || v.Pressed[0] != "auto" || !every(v.Widgets, "") || v.Stored != "" {
		t.Errorf("after Auto: %+v, want nothing checked and nothing stored", v)
	}
	// Scripts off, the group has no box: the per-sample radios are the
	// scriptless behaviour, unchanged.
	off := noScripts(t, ctx)
	var box float64
	if err := chromedp.Run(off, chromedp.Navigate(form), chromedp.WaitReady(`.ds-viewall`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector(".ds-viewall").getBoundingClientRect().height`, &box)); err != nil {
		t.Fatalf("scripts off: %v", err)
	}
	if box != 0 {
		t.Errorf("with scripts off the group is %.0fpx tall; a control that cannot work is not shown", box)
	}
}

// Storage that throws on every access (private mode,
// refused site data) costs persistence and nothing else.
func TestThePageWideViewWorksWhenStorageThrows(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		addInit(`Object.defineProperty(window, "localStorage", {configurable: true, get() { throw new DOMException("denied", "SecurityError"); }});
		  window.__errors = 0; addEventListener("error", () => window.__errors++);`),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("form"))),
		chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form with storage that throws: %v", err)
	}
	press(t, ctx, "Code", "code")
	if v := readView(t, ctx, "Code"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") || v.Stored != "throws" {
		t.Errorf("with storage throwing: %+v, want Code applied and pressed", v)
	}
	until(t, ctx, "no exception escaped", `window.__errors === 0`)
}

// With Code chosen for the whole page and every Rendered HTML
// disclosure open, no source block on the two heaviest pages scrolls
// sideways at 390px: the soft wrap holds even a 3,800px inline run.
func TestNoCodeBlockScrollsSidewaysOnAPhone(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	for _, kind := range []string{"form", "date-and-time"} {
		var wide []string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf(kind))),
			chromedp.WaitReady(`.ds-viewall`, chromedp.ByQuery),
			chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="code"]').click();
			  document.querySelectorAll("details.ds-html").forEach(d => d.open = true); true`, nil),
			chromedp.Evaluate(`[...document.querySelectorAll(".ds-src")].filter(p => p.getBoundingClientRect().height > 0 && p.scrollWidth > p.clientWidth).map(p => p.textContent.slice(0, 50))`, &wide)); err != nil {
			t.Fatalf("%s at 390px: %v", kind, err)
		}
		if len(wide) > 0 {
			t.Errorf("%s at 390px: %d source blocks scroll sideways: %q", kind, len(wide), wide)
		}
	}
}

// axe with the Code panels open: the scan of every page never saw one
// before, because the panels are display: none until a tab is chosen.
// Each leg first asserts which highlight elements are on the page, so
// the coverage cannot quietly shrink: all five on Form (its calls carry
// actions and strings, its HTML tags, attributes and values), the three
// markup ones on UI primitives, whose samples hold no template action.
func TestA11yScansTheCodePanels(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for _, c := range []struct {
		kind string
		want []string
	}{{"form", []string{"ds-t", "ds-a", "ds-v", "ds-x", "ds-s"}}, {"primitives", []string{"ds-t", "ds-a", "ds-v"}}} {
		for _, theme := range ui.ThemeNames() {
			for _, scheme := range a11ySchemes {
				where := fmt.Sprintf("%s/en %s, Code chosen (%s)", theme, c.kind, scheme)
				var shown []string
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
					chromedp.Navigate(rig.Origin+pageHref(mountPath, theme, "en", fileOf(c.kind))),
					chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery),
					chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="code"]').click();
					  document.querySelectorAll("details.ds-html").forEach(d => d.open = true);
					  ["ds-t", "ds-a", "ds-v", "ds-x", "ds-s"].filter(n => [...document.querySelectorAll(n)].some(e => e.getBoundingClientRect().width > 0))`, &shown)); err != nil {
					t.Fatalf("%s: loading: %v", where, err)
				}
				for _, w := range c.want {
					if !slices.Contains(shown, w) {
						t.Fatalf("%s: no visible %s; this scan would not be checking that colour", where, w)
					}
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
					t.Fatalf("%s: loading axe: %v", where, err)
				}
				paint(t, ctx, scheme)
				total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
			}
		}
	}
	if total == 0 {
		t.Log("clean: the Code panels on Form and UI primitives in every theme and scheme")
	}
}
```

Add `"fmt"`, `"slices"`, `"amadan.net/rastrillo/rastrillo/ui"` to `code_browser_test.go`'s imports.

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run TestThePageWideViewIsOnEveryPageWithCode -count=1 ./internal/designsystem/`
Expected: FAIL: `form: Code tab true, page-wide group false`.

- [ ] **Step 4: Render the group**

In `page.go`, add to `pageView` after `TitleID`:

```go
	// ViewGroup is whether the page has a Code tab anywhere, which is
	// when the page-wide view buttons are worth drawing: read off the
	// rendered body, so a page that gains its first Code tab gets them.
	ViewGroup bool
```

In `renderGallery`, after `view.Body = body`, add `view.ViewGroup = strings.Contains(string(body), "ds-view__tab--c")`. In `pageTemplate`, after the page header's closing `</header>` and before `{{.Body}}`, add:

```
{{if .ViewGroup}}<div class="ds-viewall" role="group" aria-label="{{P "⟦gallery.view.label⟧"}}"><button type="button" data-ds-view="auto" aria-pressed="true">{{P "⟦gallery.view.auto⟧"}}</button><button type="button" data-ds-view="desktop" aria-pressed="false">{{P "Desktop"}}</button><button type="button" data-ds-view="mobile" aria-pressed="false">{{P "Mobile"}}</button><button type="button" data-ds-view="code" aria-pressed="false">{{P "Code"}}</button></div>
{{end}}
```

In `gallery.css`, replace the `.ds-scheme button`, `.ds-scheme button + button`, `:hover`, `[aria-pressed="true"]` and `:focus-visible` rules' selectors with the pair `.ds-scheme button, .ds-viewall button` (and the same for each state), so the two groups are one look. After `:root[data-rst-js] .ds-scheme { … }`, add:

```css
/* The page-wide view group, hidden until gallery.js is there to make
   it work: with scripts off the per-sample radios are the whole of the
   widget, as they always were. */
.ds-viewall { display: none; }
:root[data-rst-js] .ds-viewall { align-items: stretch; border: 1px solid var(--rst-line); border-radius: var(--rst-radius-sm); display: inline-flex; margin: 0 0 var(--rst-sp-4); overflow: hidden; }
```

and change the touch rule from Task 10 to `.ds-scheme button, .ds-viewall button { min-block-size: var(--rst-tap); }`.

- [ ] **Step 5: Drive it from `gallery.js`**

The view needs the same storage wrappers the scheme has, so first make them shared. Replace `stored()` and `remember()` (and keep the comment above them, which says why both sides are wrapped) with:

```js
  // The scheme and the page-wide view share them; a list's first
  // value is its default, which is stored as no key at all.
  function load(key, values) {
    try {
      var v = localStorage.getItem(key);
      return values.indexOf(v) > 0 ? v : values[0];
    } catch (e) {
      return values[0];
    }
  }

  function save(key, value, values) {
    try {
      if (value === values[0]) localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch (e) {
      /* the choice still applies to this page; it just will not survive */
    }
  }

  function stored() {
    return load(KEY, SCHEMES);
  }
```

and in the scheme's click handler replace `remember(scheme);` with `save(KEY, scheme, SCHEMES);`.

In the header list, insert `   - the page-wide view, remembered as rst-ds-view;` before `   - copy buttons.`

Before the `// ── Copy ──` section, add:

```js
  // ── One view for the whole page ─────────────────────────────────────
  //
  // Buttons, not radios, so pressing the pressed one re-applies it after
  // a reader changed one widget by hand. Pressed is read off the radios,
  // never off what a width shows: Auto when nothing is checked; a view
  // when every widget with that tab has it checked and every other has
  // nothing checked; else none. .checked fires no change event and needs
  // none, since the panels follow :has(:checked). Applied at
  // DOMContentLoaded, before load puts a fragment in place against it.
  var VIEWS = ["auto", "desktop", "mobile", "code"];
  ready(function () {
    var group = document.querySelector(".ds-viewall");
    if (!group) return;
    var widgets = document.querySelectorAll(".ds-view"), buttons = group.querySelectorAll("button");
    // .ds-view__tab--d, --m or --c. Auto has none, nor does a view a
    // widget lacks (a framed page has no Code): it is left on Auto.
    function radio(w, view) {
      var tab = w.querySelector(".ds-view__tab--" + view.charAt(0));
      return tab && tab.querySelector("input");
    }
    function choose(view) {
      for (var i = 0; i < widgets.length; i++) {
        var want = radio(widgets[i], view), inputs = widgets[i].querySelectorAll(".ds-view__tab input");
        for (var j = 0; j < inputs.length; j++) inputs[j].checked = inputs[j] === want;
      }
    }
    function show() {
      var on = "";
      for (var v = 0; v < VIEWS.length && !on; v++) {
        for (var i = 0, all = true; i < widgets.length && all; i++) {
          var r = v && radio(widgets[i], VIEWS[v]);
          all = r ? r.checked : !widgets[i].querySelector(".ds-view__tab input:checked");
        }
        if (all) on = VIEWS[v];
      }
      for (var k = 0; k < buttons.length; k++) buttons[k].setAttribute("aria-pressed", String(buttons[k].dataset.dsView === on));
    }
    choose(load("rst-ds-view", VIEWS));
    show();
    group.addEventListener("click", function (event) {
      var b = event.target.closest("button");
      if (!b) return;
      choose(b.dataset.dsView);
      save("rst-ds-view", b.dataset.dsView, VIEWS);
      show();
    });
    document.addEventListener("change", show);
  });
```

In `TestGalleryScriptStaysInertAndFirstParty`'s running totals, add:

```go
	//   the page-wide view, and storage shared with it  +2,580 15,555
```

- [ ] **Step 6: Add the two prose keys**

Write `$TMPDIR/copy-task-14.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-14.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [],
 "add": [
  {
   "id": "gallery.view.label",
   "en": "Show every example as",
   "tr": {
    "ga": "Taispeáin gach sampla mar",
    "zh-Hans": "所有示例显示为",
    "es": "Mostrar todos los ejemplos como",
    "hi": "हर उदाहरण ऐसे दिखाएँ",
    "pt": "Mostrar todos os exemplos como",
    "bn": "সব উদাহরণ এভাবে দেখাও",
    "ru": "Показывать все примеры как",
    "ja": "すべての例の表示",
    "yue": "所有例子顯示做",
    "vi": "Hiển thị mọi ví dụ dạng",
    "ar": "اعرض كل الأمثلة بصيغة"
   }
  },
  {
   "id": "gallery.view.auto",
   "en": "Auto",
   "tr": {
    "ga": "Uathoibríoch",
    "zh-Hans": "自动",
    "es": "Automático",
    "hi": "स्वचालित",
    "pt": "Automático",
    "bn": "স্বয়ংক্রিয়",
    "ru": "Авто",
    "ja": "自動",
    "yue": "自動",
    "vi": "Tự động",
    "ar": "تلقائي"
   }
  }
 ],
 "fill": [
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-14.json"
```

Expected: `copyedit: prose.go: +2 -0`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/page.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 7: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/ && RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestThePageWideView|TestNoCodeBlockScrollsSidewaysOnAPhone|TestA11yScansTheCodePanels|TestA11yWalksTheKeyboard|TestThePhoneIndexAndTheWayBack' -count=1 -timeout 25m ./internal/designsystem/`
Expected: PASS, with `wc -c internal/designsystem/gallery.js` printing 15,555. The keyboard walk's first stop is now the group's Auto button.

- [ ] **Step 8: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 9: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/page.go \
  internal/designsystem/gallery.js \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/code_browser_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Add one view choice for every example on a page

Reading Form's code took 35 clicks, one tab bar per example. Four
buttons at the top of each page with a Code tab now set every widget at
once and are remembered for the next page. They are buttons rather
than radios, so pressing Code again after changing one widget by hand
re-applies it. What they show pressed is read off the widgets' own
radios, never off what the width happens to display, so no resize
handling is needed. With scripts off the group is hidden and the
per-example radios work as before. Storage that throws costs only
persistence.

A Code-selected axe pass and a 390px sideways-scroll check now cover
the panels, which no scan saw while they were display: none.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 15: Keeping your place across a switch, and the scheme after a back/forward restore

Spec 2.3 "The reading line", "Keeping your place" (desktop rules 1 to 3, the canonical address, the phone index's fixed fragment) and "A page restored from the back/forward cache keeps up with the scheme". Tests: "Position survives a switch" with every sub-leg, its Go check that anchor ids match across themes and locales, "The scheme follows a page back from the cache", and Review Focus 5 (a fragment that is no anchor).

**Files:**
- Modify: `internal/designsystem/gallery.js`: the scheme's `pressed` moves to top level; a `pageshow` resync; a "Keeping your place" section
- Create: `internal/designsystem/sweep/place_test.go` (position across a switch), `internal/designsystem/cache_browser_test.go` (the scheme after a cache restore)
- Modify: `internal/designsystem/sweep/rig_test.go` (`until`)
- Test: `internal/designsystem/designsystem_test.go` (the anchor-id gate and the cap comment)

**Interfaces:**
- Consumes: Task 10's bar (`.ds-top`, `.ds-top__controls`); Task 14's stored view applied at `DOMContentLoaded`; `until`, `addInit` (Task 6); `phoneRig`, `requireCoarse` (Task 12).
- Produces: in `gallery.js`, a shared `pressed(scheme)`, `place()` and the switcher click handler delegated from `.ds-top`. Every bar switcher link keeps its rendered `href` as `dsHref`.

- [ ] **Step 1: Write the failing tests**

Append to `designsystem_test.go`:

```go
// A section's anchor id is the same in every theme and locale, because
// it is built from English: that is what lets a switch keep the reader
// on the section they were reading, and a link into the gallery survive
// a change of language.
func TestAnchorIDsAreTheSameInEveryThemeAndLocale(t *testing.T) {
	files := render(t)
	for _, pk := range pageKinds() {
		var want string
		for _, m := range anchorMarker.FindAllStringSubmatch(galleryPage(t, files, RootTheme(), "en", pk.Kind), -1) {
			want += m[1] + " "
		}
		for _, theme := range ui.ThemeNames() {
			for _, locale := range rastrillo.BaseLocales() {
				var got string
				for _, m := range anchorMarker.FindAllStringSubmatch(galleryPage(t, files, theme, locale, pk.Kind), -1) {
					got += m[1] + " "
				}
				if got != want {
					t.Errorf("%s/%s/%s: anchors differ from %s/en's", theme, locale, pk.File, RootTheme())
				}
			}
		}
	}
}
```

Add to `internal/designsystem/sweep/rig_test.go` (the design-system package has its own from Task 6):

```go
func until(t *testing.T, ctx context.Context, where, expr string) {
	t.Helper()
	galleryrig.Until(t, ctx, where, expr)
}
```

Create `internal/designsystem/sweep/place_test.go`:

```go
//go:build browser

package sweep

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// geometryJS is the rule a switch falls back to, written again here so
// the drive checks the script rather than agreeing with it: among the
// anchors whose top is at or above the reading line (the scroll
// padding plus a pixel), those with the greatest top, the first of
// them in document order.
const geometryJS = `(() => {
  const line = parseFloat(getComputedStyle(document.documentElement).scrollPaddingBlockStart) + 1;
  let best = null, top = -Infinity;
  for (const a of document.querySelectorAll("[data-ds-anchor]")) {
    const r = a.getBoundingClientRect();
    if (!r.width && !r.height) continue;
    if (r.top <= line && r.top > top) { best = a; top = r.top; }
  }
  return best ? best.id : "";
})()`

const landingJS = `(() => {
  const id = decodeURIComponent(location.hash.slice(1)), el = id && document.getElementById(id);
  return JSON.stringify({Hash: location.hash, Path: location.pathname, Top: el ? el.getBoundingClientRect().top : -1,
    Line: parseFloat(getComputedStyle(document.documentElement).scrollPaddingBlockStart) + 1,
    End: Math.abs(scrollY + innerHeight - document.documentElement.scrollHeight) < 2, Y: scrollY});
})()`

type landing struct {
	Hash, Path   string
	Top, Line, Y float64
	End          bool
}

// place drives one tab through switches on desktop, where the
// switchers are.
type place struct {
	t      *testing.T
	ctx    context.Context
	origin string
}

func (p *place) run(where string, actions ...chromedp.Action) {
	p.t.Helper()
	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("%s: %v", where, err)
	}
}

// clickWith is a real mouse click on the first match, with a modifier
// or another button: a synthetic click cannot open a tab, and the legs
// below are about what a real Ctrl-click and middle-click do.
func (p *place) clickWith(where, sel string, opts ...chromedp.MouseOption) {
	p.t.Helper()
	var nodes []*cdp.Node
	p.run(where, chromedp.Nodes(sel, &nodes, chromedp.ByQuery))
	p.run(where, chromedp.MouseClickNode(nodes[0], opts...))
}

// frames waits two animation frames: the record is written one frame
// after this document puts a target in place.
func (p *place) frames(where string) {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, nil,
		func(e *runtime.EvaluateParams) *runtime.EvaluateParams { return e.WithAwaitPromise(true) }))
}

func (p *place) open(where, url string) {
	p.t.Helper()
	p.run(where, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(url), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery))
	until(p.t, p.ctx, where, `document.readyState === "complete"`)
	p.frames(where)
}

func (p *place) geometry(where string) string {
	p.t.Helper()
	var id string
	p.run(where, chromedp.Evaluate(geometryJS, &id))
	return id
}

// follow clicks a rail link to an anchor on this page, a real followed
// fragment.
func (p *place) follow(where, id string) {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('#ds-nav a[href$="#%s"]').click(); true`, id), nil))
	p.frames(where)
}

// switchTheme clicks the bar's link to the next theme and waits for
// that theme's page; switchLanguage does the same through the language
// menu. Both report where the new page landed.
func (p *place) switchTheme(where string) landing {
	p.t.Helper()
	var next string
	p.run(where, chromedp.Evaluate(`(() => { const a = document.querySelector('.ds-top__controls [rst-seg-tabs] a:not([aria-current])'); a.click(); return a.textContent; })()`, &next))
	p.run(where, chromedp.WaitReady(`link[href$="/theme-`+next+`.css"]`, chromedp.ByQuery))
	return p.landed(where)
}

func (p *place) switchLanguage(where, code string) landing {
	p.t.Helper()
	p.run(where, chromedp.Evaluate(`(() => { const d = document.querySelector(".ds-top__controls [rst-locale]"); d.open = true; d.querySelector('a[lang="`+code+`"]').click(); return true; })()`, nil))
	p.run(where, chromedp.WaitReady(`html[lang="`+code+`"]`, chromedp.ByQuery))
	return p.landed(where)
}

func (p *place) landed(where string) landing {
	p.t.Helper()
	until(p.t, p.ctx, where, `document.readyState === "complete"`)
	var raw string
	p.run(where, chromedp.Evaluate(landingJS, &raw))
	var l landing
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		p.t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return l
}

// lands asserts a switch landed on id: the fragment names it, and its
// top is on the reading line, or the page is scrolled to its end with
// the top below the line, because the browser clamps at the end.
func (p *place) lands(where string, l landing, id string) {
	p.t.Helper()
	if l.Hash != "#"+id {
		p.t.Errorf("%s: landed at %q, want #%s", where, l.Hash, id)
		return
	}
	if math.Abs(l.Top-l.Line) > 2 && !(l.End && l.Top > l.Line) {
		p.t.Errorf("%s: #%s's top is at %.1fpx, not on the reading line at %.1fpx", where, id, l.Top, l.Line)
	}
}

func TestPositionSurvivesASwitch(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()
	p := &place{t: t, ctx: ctx, origin: rig.Origin}
	form := func(theme, locale string) string { return rig.Origin + pageHref(mountPath, theme, locale, fileOf("form")) }
	// Form's last partial, read off the page: the case where the page
	// clamps before the target reaches the reading line.
	var last string
	p.open("finding the last partial", form("day", "en"))
	p.run("finding the last partial", chromedp.Evaluate(`[...document.querySelectorAll("article.ds-partial[data-ds-anchor]")].pop().id`, &last))

	// A section on the reading line survives a theme switch and then a
	// language switch.
	p.open("scrolled", form("day", "en"))
	p.run("scrolled", chromedp.Evaluate(`document.getElementById("partial-field-select").scrollIntoView({block: "start"}); true`, nil))
	p.lands("scrolled, then theme", p.switchTheme("scrolled, then theme"), "partial-field-select")
	p.lands("scrolled, then language", p.switchLanguage("scrolled, then language", "ja"), "partial-field-select")

	// A followed rail fragment, then theme, then language, with no
	// scrolling in between: the same section every time.
	p.open("followed", form("day", "en"))
	p.follow("followed", "partial-field-check")
	p.lands("followed, then theme", p.switchTheme("followed, then theme"), "partial-field-check")
	p.lands("followed, then language", p.switchLanguage("followed, then language", "ga"), "partial-field-check")

	// The two cases geometry cannot answer. The last partial: the page
	// clamps before it reaches the line, so geometry would pick the one
	// above it.
	p.open("last", form("day", "en"))
	p.follow("last", last)
	p.lands("the last partial", p.switchTheme("the last partial"), last)
	// And rule 1 lets go: a screen's scroll later the id is geometry's.
	p.open("last, scrolled", form("day", "en"))
	p.follow("last, scrolled", last)
	p.run("last, scrolled", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	want := p.geometry("last, scrolled")
	p.lands("last, scrolled, then theme", p.switchTheme("last, scrolled, then theme"), want)

	// Icons: a row of glyphs shares one top. Following the first glyph
	// of the second row lands on it, not on the row's last.
	icons := rig.Origin + pageHref(mountPath, "day", "en", fileOf("icons"))
	p.open("icons", icons)
	var glyph string
	p.run("icons", chromedp.Evaluate(`(() => { const all = [...document.querySelectorAll(".ds-icons > [data-ds-anchor]")];
	  const firstTop = all[0].getBoundingClientRect().top; return all.find(a => a.getBoundingClientRect().top > firstTop + 1).id; })()`, &glyph))
	p.follow("icons", glyph)
	p.lands("icons, a row's first glyph", p.switchTheme("icons, a row's first glyph"), glyph)

	// The record's start: Code stored as the page-wide view, Form loaded
	// at the last partial, and switched at once: that partial, because
	// gallery.js put the target in place after Code changed the layout.
	p.open("code stored", form("day", "en"))
	p.run("code stored", chromedp.Evaluate(`localStorage.setItem("rst-ds-view", "code"); true`, nil))
	p.open("loaded at the last partial", form("day", "en")+"#"+last)
	p.lands("loaded at the last partial, then theme", p.switchTheme("loaded at the last partial, then theme"), last)
	// The same load, a screen up, then a reload: a restored scroll is
	// not a fragment the reader is on, so geometry decides.
	p.open("reloaded", form("day", "en")+"#"+last)
	p.run("reloaded", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil), chromedp.Reload(), chromedp.WaitReady(`.ds-top`, chromedp.ByQuery))
	until(t, ctx, "reloaded", `document.readyState === "complete"`)
	p.frames("reloaded")
	want = p.geometry("reloaded")
	p.lands("reloaded, then theme", p.switchTheme("reloaded, then theme"), want)
	p.run("code cleared", chromedp.Evaluate(`localStorage.removeItem("rst-ds-view"); true`, nil))

	// The record's renewal: following the same fragment again after a
	// layout change above it (no hashchange fires) puts it back.
	p.open("renewed", form("day", "en"))
	p.follow("renewed", last)
	p.run("renewed", chromedp.Evaluate(`document.querySelector(".ds-view__tab--m input").click(); true`, nil))
	p.follow("renewed", last)
	p.lands("renewed, then theme", p.switchTheme("renewed, then theme"), last)

	// History is not a destination: Back restores a scrolled position,
	// not the target's, so the address naming the last partial again
	// does not make it the place.
	p.open("history", form("day", "en"))
	p.follow("history", last)
	p.run("history", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	p.follow("history", "partial-field-text")
	p.run("history", chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, "history, back", `location.hash === "#`+last+`"`)
	p.frames("history, back")
	want = p.geometry("history, back")
	if want == last {
		t.Fatalf("history, back: geometry also picks %s, so this leg cannot tell rule 1 from rule 2; scroll further", last)
	}
	p.lands("history, back, then theme", p.switchTheme("history, back, then theme"), want)
	// And Forward, the same journey on a fresh page: Back to the last
	// partial with the reader scrolled away, then Forward to the other,
	// all within one document, then switch.
	p.open("history, forward", form("day", "en"))
	p.follow("history, forward", last)
	p.run("history, forward", chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
	p.follow("history, forward", "partial-field-text")
	p.run("history, forward", chromedp.Evaluate(`scrollBy(0, innerHeight); history.back(); true`, nil))
	until(t, ctx, "history, forward, back", `location.hash === "#`+last+`"`)
	p.run("history, forward", chromedp.Evaluate(`history.forward(); true`, nil))
	until(t, ctx, "history, forward", `location.hash === "#partial-field-text"`)
	p.frames("history, forward")
	want = p.geometry("history, forward")
	p.lands("history, forward, then theme", p.switchTheme("history, forward, then theme"), want)

	// Only a followed link renews: a Ctrl-click opens a tab and moves
	// nothing here; a click another listener cancels moves nothing either.
	for _, how := range []string{"ctrl-click", "cancelled"} {
		where := "only a followed link, " + how
		p.open(where, form("day", "en"))
		p.follow(where, last)
		p.run(where, chromedp.Evaluate(`scrollBy(0, -innerHeight); true`, nil))
		sel := `#ds-nav a[href$="#` + last + `"]`
		if how == "ctrl-click" {
			p.run(where, chromedp.Evaluate(`document.querySelector('`+sel+`').closest("details").open = true; true`, nil))
			p.clickWith(where, sel, chromedp.ButtonModifiers(input.ModifierCtrl))
		} else {
			p.run(where, chromedp.Evaluate(`document.addEventListener("click", e => e.preventDefault(), {once: true}); document.querySelector('`+sel+`').click(); true`, nil))
		}
		p.frames(where)
		want := p.geometry(where)
		p.lands(where+", then theme", p.switchTheme(where+", then theme"), want)
	}

	// A fragment that is not an anchor (the skip link's
	// #main, a typo) is never carried, and never decides the place.
	for _, frag := range []string{"main", "nope"} {
		where := "loaded at #" + frag
		p.open(where, form("day", "en")+"#"+frag)
		want := p.geometry(where)
		l := p.switchTheme(where + ", then theme")
		if want == "" && l.Hash != "" || want != "" && l.Hash != "#"+want {
			t.Errorf("%s: the switch carried %q, want %q", where, l.Hash, map[bool]string{true: "", false: "#" + want}[want == ""])
		}
	}

	// The switcher's own address is never left changed. A Ctrl-click
	// from deep in the page opens the computed place in a new tab and
	// leaves this link canonical; a plain click from above every anchor
	// lands at the other page's top; a middle-click opens the top; and a
	// page back from the back/forward cache still has canonical links.
	where := "canonical"
	p.open(where, form("day", "en"))
	p.run(where, chromedp.Evaluate(`document.getElementById("partial-field-select").scrollIntoView({block: "start"}); true`, nil))
	opened := chromedp.WaitNewTarget(ctx, func(info *target.Info) bool { return strings.Contains(info.URL, "/form.html") && !strings.Contains(info.URL, "/day/") })
	p.clickWith(where, `.ds-top__controls [rst-seg-tabs] a:not([aria-current])`, chromedp.ButtonModifiers(input.ModifierCtrl))
	select {
	case id := <-opened:
		tab, closeTab := chromedp.NewContext(ctx, chromedp.WithTargetID(id))
		var url string
		if err := chromedp.Run(tab, chromedp.Location(&url)); err != nil {
			t.Fatalf("%s: reading the new tab: %v", where, err)
		}
		closeTab()
		if !strings.HasSuffix(url, "#partial-field-select") {
			t.Errorf("%s: the Ctrl-clicked tab opened %s, want the computed place #partial-field-select", where, url)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: Ctrl-clicking a theme link opened no tab", where)
	}
	until(t, ctx, where+", restored", `![...document.querySelectorAll(".ds-top__controls a")].some(a => a.getAttribute("href").includes("#"))`)
	p.run(where, chromedp.Evaluate(`scrollTo(0, 0); true`, nil))
	top := p.switchTheme(where + ", from the top")
	if top.Hash != "" || top.Y != 0 {
		t.Errorf("%s: a plain click from above every anchor landed at %q, scrolled %.0fpx; want the other page's top", where, top.Hash, top.Y)
	}
	p.run(where, chromedp.Evaluate(`history.back(); true`, nil))
	until(t, ctx, where+", back", `location.pathname.includes("/day/") && ![...document.querySelectorAll(".ds-top__controls a")].some(a => a.getAttribute("href").includes("#"))`)
	middle := chromedp.WaitNewTarget(ctx, func(info *target.Info) bool { return strings.Contains(info.URL, "/form.html") && !strings.Contains(info.URL, "/day/") })
	p.clickWith(where, `.ds-top__controls [rst-seg-tabs] a:not([aria-current])`, chromedp.ButtonMiddle)
	select {
	case id := <-middle:
		tab, closeTab := chromedp.NewContext(ctx, chromedp.WithTargetID(id))
		var url string
		if err := chromedp.Run(tab, chromedp.Location(&url)); err != nil {
			t.Fatalf("%s: reading the middle-clicked tab: %v", where, err)
		}
		closeTab()
		if strings.Contains(url, "#") {
			t.Errorf("%s: a middle-click opened %s; it reads the link as rendered, the top of the page", where, url)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: middle-clicking a theme link opened no tab", where)
	}
}
```

Create `internal/designsystem/cache_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The scheme follows a page back from the back/forward cache. On a
// phone the toggle is on the index only, and shell.js sends Back
// through history, so a section page comes back from the cache
// without re-running anything: open Form in System, go Back to the
// index, choose Dark, go Forward, and Form must be dark, previews and
// all. The control first: the Forward really was a cache restore. A
// leg that silently reloaded Form would pass on start-up code alone.
func TestTheSchemeFollowsAPageBackFromTheCache(t *testing.T) {
	rig := phoneRig(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	where := "day/en at 390px"
	if err := chromedp.Run(ctx,
		addInit(`addEventListener("pageshow", e => { window.__restored = e.persisted; });`),
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+indexHref(mountPath, "day", "en")),
		chromedp.WaitVisible(`.ds-index`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading the index: %v", where, err)
	}
	requireCoarse(t, ctx)
	for _, c := range []struct{ scheme, attr string }{{"dark", "dark"}, {"system", ""}} {
		step := where + ", " + c.scheme
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById("nav-form").click(); true`, nil)); err != nil {
			t.Fatalf("%s: opening Form: %v", step, err)
		}
		until(t, ctx, step+", Form", `location.pathname.endsWith("/form.html") && document.readyState === "complete"`)
		var started float64
		if err := chromedp.Run(ctx, chromedp.Evaluate(`performance.getEntriesByType("navigation")[0].startTime + performance.timeOrigin`, &started)); err != nil {
			t.Fatalf("%s: reading Form's navigation: %v", step, err)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
			t.Fatalf("%s: going back: %v", step, err)
		}
		until(t, ctx, step+", the index", `location.pathname.endsWith("/index.html") && !!document.querySelector('#ds-prefs [data-ds-scheme="`+c.scheme+`"]')`)
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#ds-prefs [data-ds-scheme="`+c.scheme+`"]').click(); history.forward(); true`, nil)); err != nil {
			t.Fatalf("%s: choosing %s and going forward: %v", step, c.scheme, err)
		}
		until(t, ctx, step+", Form again", `location.pathname.endsWith("/form.html") && window.__restored !== undefined`)
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({Restored: window.__restored,
		  Started: performance.getEntriesByType("navigation")[0].startTime + performance.timeOrigin,
		  Why: JSON.stringify((performance.getEntriesByType("navigation")[0] || {}).notRestoredReasons || null)})`, &raw)); err != nil {
			t.Fatalf("%s: reading the restore: %v", step, err)
		}
		var r struct {
			Restored bool
			Started  float64
			Why      string
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("%s: decoding %q: %v", step, raw, err)
		}
		if !r.Restored || r.Started != started {
			t.Fatalf("%s: Forward was not a back/forward cache restore (persisted %v, navigation %s then %s; the engine says %s). The tree handler must not send Cache-Control: no-store", step, r.Restored, strconv.FormatFloat(started, 'f', 0, 64), strconv.FormatFloat(r.Started, 'f', 0, 64), r.Why)
		}
		until(t, ctx, step+", painted", fmt.Sprintf(`(document.documentElement.getAttribute("data-theme") || "") === %q &&
		  [...document.querySelectorAll(".ds-view__frame")].every(f => { const d = f.contentDocument;
		    return !d || d.URL !== f.src || (d.documentElement.getAttribute("data-theme") || "") === %q; })`, c.attr, c.attr))
		if err := chromedp.Run(ctx, chromedp.Evaluate(`history.back(); true`, nil)); err != nil {
			t.Fatalf("%s: back to the index: %v", step, err)
		}
		until(t, ctx, step+", index again", `location.pathname.endsWith("/index.html")`)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestPositionSurvivesASwitch|TestTheSchemeFollowsAPageBackFromTheCache' -count=1 ./internal/designsystem/ ./internal/designsystem/sweep/`
Expected: FAIL, `scrolled, then theme: landed at "", want #partial-field-select`, and `Form again, painted: never true`.

- [ ] **Step 3: Write the script**

In `gallery.js`, delete the scheme's `pressed` function from inside the `ready` closure (keep its `pressed(stored());` call and the one in the click handler), and before `// Phase one, at parse time` add the shared version and the cache-restore handler:

```js
  // Every scheme button: the Overview has two sets, the bar's and the
  // phone index's, and both stay in step.
  function pressed(scheme) {
    var buttons = document.querySelectorAll("[data-ds-scheme]");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute("aria-pressed", buttons[i].dataset.dsScheme === scheme ? "true" : "false");
    }
  }

  // A page back from the back/forward cache is reactivated, not re-run,
  // so none of this happens again. On a phone the toggle is on the index
  // only and Back goes through history, so without this a page left in
  // Light comes back Light after Dark was chosen on the index, with no
  // control on it to fix that. Theme and language are addresses, not
  // stored choices, and need nothing.
  addEventListener("pageshow", function (event) {
    if (!event.persisted) return;
    apply(stored());
    pressed(stored());
    frames(stored());
  });
```

In the header list, insert `   - keeping your place across a theme or language switch;` before `   - the page-wide view, remembered as rst-ds-view;`.

Before the `// ── One view for the whole page ──` section, add:

```js
  // ── Keeping your place ──────────────────────────────────────────────
  //
  // A theme or language link in the pinned bar carries the section being
  // read, worked out at the click; the address bar is never rewritten.
  //  1. A fragment this document put in place, while its target has not
  //     moved (2px): the case geometry cannot answer, the last partial of
  //     a page that clamps, or a glyph in a row of icons sharing a top.
  //  2. Else the anchor at the reading line (the scroll padding plus a
  //     pixel, where a fragment lands): the greatest top at or above it,
  //     first in document order.
  //  3. Above every anchor, nothing.
  // The rule-1 record is written only when this document scrolls to a
  // target: at load on a fresh, untouched navigation (redoing the parser's
  // scroll, which the stored view's layout has since moved), and a frame
  // after a plain, uncancelled click on a fragment link here, the only
  // signal when that fragment is already in the address. History drops
  // it, because Back restores where the reader had scrolled.
  var record = null, touched = false;

  function anchor(id) {
    var el = id && document.getElementById(id);
    return el && el.hasAttribute("data-ds-anchor") ? el : null;
  }

  function fragment(hash) {
    try {
      return decodeURIComponent(hash.slice(1));
    } catch (e) {
      return hash.slice(1);
    }
  }

  function keep(el) {
    record = { id: el.id, top: el.getBoundingClientRect().top };
  }

  ["wheel", "touchstart", "pointerdown", "keydown"].forEach(function (type) {
    addEventListener(type, function () { touched = true; }, { capture: true, passive: true });
  });

  addEventListener("load", function () {
    var nav = performance.getEntriesByType("navigation")[0], el = anchor(fragment(location.hash));
    if (!el || touched || !nav || nav.type !== "navigate") return;
    el.scrollIntoView({ block: "start" });
    requestAnimationFrame(function () { keep(el); });
  });

  document.addEventListener("click", function (event) {
    var a = event.target.closest && event.target.closest("a[href]"), el = a && anchor(fragment(a.hash));
    if (!el || event.button || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || a.target || a.hasAttribute("download") ||
        a.origin + a.pathname + a.search !== location.origin + location.pathname + location.search) return;
    requestAnimationFrame(function () {
      if (!event.defaultPrevented) keep(el);
    });
  });

  addEventListener("popstate", function () { record = null; });
  if (window.navigation) navigation.addEventListener("navigate", function (event) {
    if (event.navigationType === "traverse") record = null;
  });

  function place() {
    var el = record && fragment(location.hash) === record.id && anchor(record.id);
    if (el && Math.abs(el.getBoundingClientRect().top - record.top) <= 2) return el.id;
    var line = (parseFloat(getComputedStyle(root).scrollPaddingBlockStart) || 0) + 1;
    var all = document.querySelectorAll("[data-ds-anchor]"), best = "", top = -Infinity;
    for (var i = 0; i < all.length; i++) {
      var r = all[i].getBoundingClientRect();
      if ((r.width || r.height) && r.top <= line && r.top > top) {
        best = all[i].id;
        top = r.top;
      }
    }
    return best;
  }

  // A link's address is restored by a timer scheduled before it is
  // changed, so nothing failing between can leave it set. Not a
  // microtask: that checkpoint comes before activation reads href. Again
  // on pageshow, for a page back from the cache whose timer never ran.
  ready(function () {
    var bar = document.querySelector(".ds-top");
    if (!bar) return;
    var links = bar.querySelectorAll(".ds-top__controls a[href]");
    for (var i = 0; i < links.length; i++) links[i].dsHref = links[i].getAttribute("href");
    function canonical() {
      for (var i = 0; i < links.length; i++) links[i].setAttribute("href", links[i].dsHref);
    }
    addEventListener("pageshow", canonical);
    bar.addEventListener("click", function (event) {
      var a = event.target.closest("a[href]"), id;
      if (!a || a.dsHref === undefined) return;
      setTimeout(canonical, 0);
      if ((id = place())) a.setAttribute("href", a.dsHref + "#" + encodeURIComponent(id));
    });
  });
```

In `TestGalleryScriptStaysInertAndFirstParty`'s running totals, add:

```go
	//   keeping your place, the scheme after a
	//     back/forward cache restore                  +5,201 20,756
```

- [ ] **Step 4: Run the tests**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/ && RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestPositionSurvivesASwitch|TestTheSchemeFollowsAPageBackFromTheCache|TestSchemeToggleDrivesTheWholeJourney|TestThePhoneIndexAndTheWayBack' -count=1 -timeout 20m ./internal/designsystem/ ./internal/designsystem/sweep/`
Expected: PASS, with `wc -c internal/designsystem/gallery.js` printing 20,756, under the 22,832 cap. If the cache leg fails with a reason from the engine, read it. Headless Chromium declines a page that has an open `BroadcastChannel`, unload handlers, or a `Cache-Control: no-store` main resource, and none of those is the gallery's. Report any other reason to the controller rather than weakening the control.

- [ ] **Step 5: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 6: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/gallery.js \
  internal/designsystem/cache_browser_test.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/sweep/place_test.go \
  internal/designsystem/sweep/rig_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Keep the reader's place when they switch theme or language

Switching theme or language dropped the reader at the top of the other
page. The bar's links now carry the section being read, worked out at
the moment of the click from the reading line the scroll padding
defines. A fragment this page itself scrolled to wins while its target
has not moved, because geometry cannot pick the last partial on a page
that clamps, or the first glyph of an icon row. History never writes
that record, Ctrl-clicks and cancelled clicks do not renew it, and the
link's own address goes back to canonical on a timer and on pageshow.

A page restored from the back/forward cache now resyncs the scheme. On
a phone the toggle is on the index only, so a section page restored
from the cache had no control to fix it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 16: Compact rows for status-pill, badge and meter

Spec 2.7: one frame per partial holding a `<ul>` of its states, the Code panel's per-state calls and one disclosure, notes as "label: note", the duplicate status-pill state removed, and the frame flush with its tabs. Copy: C10 (`gallery.note.tone`). Tests: "No two states of a partial render alike" and the grouped-preview contract.

**Files:**
- Modify: `internal/designsystem/samples.go` (`partialDoc.Row`, the three partials, `statusPillStates`)
- Modify: `internal/designsystem/page.go`: `partialView.Notes`, `previewView.Rows`, `type rowCode`, `rowsStyle`, `previewDocStyled`, `groupedPreview`, `buildFamilies`, `viewTemplate`, `familyBody`, and the heights of the three partials
- Modify: `internal/designsystem/gallery.css` (`.ds-view--rows`)
- Modify: `internal/designsystem/prose.go` (one key in, one out)
- Test: `internal/designsystem/designsystem_test.go`

**Interfaces:**
- Consumes: `callFor` (Task 4), the Code panel (Task 5), `TestEveryPreviewIsItsSampleWrappedAndDeadened` (Task 1), `TestTheTextOnScreenIsTheTextCopied` (Task 5), and the source-kind check in `TestEveryExampleIsFramedDesktopMobileAndCode` (Task 5).
- Produces: `partialDoc.Row bool`, `type rowCode struct{ State string; Call, Source template.HTML }`, `func groupedPreview(mount, theme, locale, page string, tmpl *template.Template, doc partialDoc, id string) (previewView, previewFile, error)`, `func previewDocStyled(mount, theme, locale, title, style, body string) string`, and the widget class ` ds-view--rows`.

- [ ] **Step 1: Check the approved copy this task writes**

```bash
jq -r --arg id gallery.note.tone '.strings[]|select(.id==$id).text' copy-review/batch-b1-result.json
```

Expected: `Tone defaults to neutral, so this call leaves it out.` Stop and report if it differs.

- [ ] **Step 2: Write the failing tests**

Append to `designsystem_test.go`:

```go
// Two states of one partial that render the same bytes are one state
// shown twice, which is what status-pill's "Default tone (neutral)"
// and "neutral" were. Only byte-identical output counts: badge's quiet
// default renders differently from its neutral and stays.
func TestNoTwoStatesOfAPartialRenderAlike(t *testing.T) {
	tmpl := sampleTree(t, "en")
	for _, fam := range families() {
		for _, doc := range fam.Partials {
			seen := map[string]string{}
			for i, s := range doc.States {
				html, err := renderSample(tmpl, doc.Name, i, s, "en")
				if err != nil {
					t.Fatal(err)
				}
				if other, dup := seen[string(html)]; dup {
					t.Errorf("%s: %q and %q render the same bytes", doc.Name, other, s.State)
				}
				seen[string(html)] = s.State
			}
		}
	}
}
```

In `TestEveryPreviewIsItsSampleWrappedAndDeadened`, replace the per-state loop with one that knows the grouped shape:

```go
			for _, doc := range fam.Partials {
				id := anchorID("partial", doc.Name)
				if doc.Row {
					// One <ul>, one <li> per state in samples.go order: the
					// label, then the sample wrapped and deadened. Each
					// state is here once, and in no single-state file.
					want := "<ul>"
					for i, s := range doc.States {
						html, err := renderSample(tmpl, doc.Name, i, s, locale)
						if err != nil {
							t.Fatalf("%s (%s): %v", doc.Name, s.State, err)
						}
						want += "<li><p>" + template.HTMLEscapeString(proseIn(locale, s.State)) + "</p>" + deaden(mountPath, wrap(doc.Wrap, string(html))) + "</li>"
						if _, single := files[fmt.Sprintf("%s/%s/%s/%s-%d.html", RootTheme(), locale, fam.Key, id, i+1)]; single {
							t.Errorf("%s/%s: a grouped partial also has a single-state preview %d", locale, doc.Name, i+1)
						}
					}
					want += "</ul>"
					if strings.Contains(want, "<form") {
						want += sink
					}
					name := fmt.Sprintf("%s/%s/%s/%s-0.html", RootTheme(), locale, fam.Key, id)
					if got := previewBody(string(files[name])); got != want {
						t.Errorf("%s: the grouped preview is not its states in order\n got: %.300s\nwant: %.300s", name, got, want)
					}
					checked++
					continue
				}
				for i, s := range doc.States {
```

(The single-state body under `for i, s := range doc.States {` stays as it is, closing with its extra brace.)

In `TestTheTextOnScreenIsTheTextCopied`, replace the inner per-state loop with:

```go
				var calls, rendered []string
				for i, s := range doc.States {
					html, err := renderSample(tmpl, doc.Name, i, s, locale)
					if err != nil {
						t.Fatal(err)
					}
					if s.Raw == "" {
						c, err := callFor(doc.Name, s, locale)
						if err != nil {
							t.Fatal(err)
						}
						calls = append(calls, c.Source)
					}
					rendered = append(rendered, codeview.Format(string(html)))
					// A partial with a widget per state shows its call and
					// then its rendering, state by state.
					if !doc.Row {
						want[fam.Key] = append(want[fam.Key], calls...)
						want[fam.Key] = append(want[fam.Key], rendered...)
						calls, rendered = nil, nil
					}
				}
				// A grouped partial shows every call, then one disclosure
				// with every rendering, under the same labels.
				want[fam.Key] = append(want[fam.Key], calls...)
				want[fam.Key] = append(want[fam.Key], rendered...)
```

In `TestEveryExampleIsFramedDesktopMobileAndCode`, change `widgetOpen` to:

```go
var widgetOpen = regexp.MustCompile(`<div class="ds-view(?: ds-view--(?:page|rows))?" style=`)
```

and its comment's `in either width class` to `in either width class, or a grouped row`. In the source-kind `switch`, replace the `disclosures == 1` case with:

```go
				case disclosures == 1:
					// A partial sample's call then its rendering; a grouped
					// row's calls, one per state, then one disclosure with
					// a rendering per state.
					calls := strings.Count(w, `<code><ds-x>{{</ds-x><ds-x>template</ds-x>`)
					withCall += calls
					if calls == 0 || pres != 2*calls {
						t.Errorf("%s widget %d: %d calls and %d blocks; each call has its rendering in the disclosure", name, i, calls, pres)
					}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod go test -run 'TestNoTwoStatesOfAPartialRenderAlike|TestEveryPreviewIsItsSampleWrappedAndDeadened' -count=1 ./internal/designsystem/`
Expected: FAIL to compile, `doc.Row undefined`. Once `Row` exists, expect `status-pill: "Default tone (neutral)" and "neutral" render the same bytes`.

- [ ] **Step 4: Group the three partials**

In `samples.go`, add to `partialDoc`:

```go
	// Row puts every state in one frame, a row that wraps into rows on a
	// phone, instead of one widget per state. For small inline pieces
	// (a pill, a badge, a meter), where a widget of their own cost a
	// 190px card and a tab bar apiece and Display ran to 10,900px on a
	// phone. A list, not a judgement: a partial is grouped by saying so.
	Row bool
```

Set `Row: true,` on `status-pill`, `badge` and `meter`. Replace `statusPillStates` with:

```go
func statusPillStates() []sample {
	labels := map[string]string{
		"neutral": "Draft", "positive": "Published", "warning": "Scheduled", "negative": "Failed",
	}
	out := make([]sample, 0, len(tones))
	for _, tone := range tones {
		// Neutral is the default, so its call leaves Tone out: the call a
		// reader copies is the shortest one that renders this pill.
		if tone == "neutral" {
			out = append(out, sample{State: tone, Data: map[string]any{"Label": labels[tone]},
				Note: "⟦gallery.note.tone⟧"})
			continue
		}
		out = append(out, sample{State: tone, Data: map[string]any{"Tone": tone, "Label": labels[tone]}})
	}
	return out
}
```

In `page.go`, add to `partialView`:

```go
	// Notes are a grouped partial's notes, "label: note" in state order,
	// under its one widget; an ungrouped partial's notes sit under each
	// state's widget instead.
	Notes []string
```

and to `previewView`:

```go
	// Rows is a grouped partial's Code panel: each state's label, call
	// and rendering. Source is still set, to the first rendering, so the
	// Code tab is drawn.
	Rows []rowCode
```

and, near `previewFile`:

```go
// rowCode is one state of a grouped partial in its Code panel.
type rowCode struct {
	State        string
	Call, Source template.HTML
}

// rowsStyle lays a grouped preview out as a row that wraps into rows on
// a phone. It is in the document's own <style> because a ds- class
// would need gallery.css inside every preview.
const rowsStyle = "body > ul { display: flex; flex-wrap: wrap; gap: 1rem 1.5rem; list-style: none; margin: 0; padding: 0; }\n" +
	"body > ul > li > p { color: var(--rst-text-muted); font-size: var(--rst-fs-xs); margin: 0 0 0.35rem; }\n"

// groupedPreview is one grouped partial's widget and the file its frame
// loads: every state, labelled, in one frame. The frame title is the
// partial's own, still unique on the page.
func groupedPreview(mount, theme, locale, page string, tmpl *template.Template, doc partialDoc, id string) (previewView, previewFile, error) {
	view := previewView{
		Group: id + "-0",
		Style: previewStyle(id, heightOf(id)),
		Class: previewClass(id) + " ds-view--rows",
		Title: previewTitle(locale, doc.Name, ""),
	}
	var body strings.Builder
	body.WriteString("<ul>")
	for i, s := range doc.States {
		html, err := renderSample(tmpl, doc.Name, i, s, locale)
		if err != nil {
			return previewView{}, previewFile{}, fmt.Errorf("%s (%s): %w", doc.Name, s.State, err)
		}
		c, err := callFor(doc.Name, s, locale)
		if err != nil {
			return previewView{}, previewFile{}, err
		}
		label := proseIn(locale, s.State)
		body.WriteString("<li><p>" + template.HTMLEscapeString(label) + "</p>" + deaden(mount, wrap(doc.Wrap, string(html))) + "</li>")
		view.Rows = append(view.Rows, rowCode{State: label, Call: codeview.Highlight(c.Source), Source: codeview.Highlight(codeview.Format(string(html)))})
	}
	body.WriteString("</ul>")
	view.Source = view.Rows[0].Source
	file := page + "/" + view.Group + ".html"
	view.Src = pageHref(mount, theme, locale, file)
	return view, previewFile{Path: file, Doc: previewDocStyled(mount, theme, locale, view.Title, rowsStyle, body.String())}, nil
}
```

Rename `previewDoc`'s body to `previewDocStyled(mount, theme, locale, title, style, body string) string`, writing `style` into the `<style>` element after its two existing lines. Keep `previewDoc` as:

```go
// previewDoc is previewDocStyled with no style of the example's own.
func previewDoc(mount, theme, locale, title, body string) string {
	return previewDocStyled(mount, theme, locale, title, "", body)
}
```

In `buildFamilies`, at the top of the partial loop body after `pv := partialView{…}`, add:

```go
			if doc.Row {
				preview, file, err := groupedPreview(mount, theme, locale, fam.Key, tmpl, doc, pv.ID)
				if err != nil {
					return nil, err
				}
				if err := files.add(file); err != nil {
					return nil, err
				}
				pv.States = []stateView{{Preview: preview}}
				for _, s := range doc.States {
					if s.Note != "" {
						pv.Notes = append(pv.Notes, proseIn(locale, s.State)+": "+proseIn(locale, s.Note))
					}
				}
				view.Partials = append(view.Partials, pv)
				continue
			}
```

In `viewTemplate`, replace `{{if .Source}}<div class="ds-view__code">` through its closing `</div>{{end}}` with:

```
{{if .Rows}}<div class="ds-view__code">
{{range .Rows}}<p class="ds-state">{{.State}}</p>
<pre class="ds-src rst-mono"><code>{{.Call}}</code></pre>
{{end}}<details class="ds-html"><summary>{{P "⟦gallery.code.rendered⟧"}}</summary>{{range .Rows}}<p class="ds-state">{{.State}}</p>
<pre class="ds-src rst-mono"><code>{{.Source}}</code></pre>
{{end}}</details>
</div>{{else if .Source}}<div class="ds-view__code">
{{if .Wrapper}}<p class="ds-wrap">{{.Wrapper}}</p>
{{end}}{{if .Call}}<pre class="ds-src rst-mono"><code>{{.Call}}</code></pre>
<details class="ds-html"><summary>{{P "⟦gallery.code.rendered⟧"}}</summary><pre class="ds-src rst-mono"><code>{{.Source}}</code></pre></details>
{{else}}<pre class="ds-src rst-mono"{{if .NoCopy}} data-ds-nocopy{{end}}><code>{{.Source}}</code></pre>
{{end}}</div>{{end}}
```

In `familyBody`, after `{{range .States}}…{{end}}` and before `</article>`, add `{{range .Notes}}<p class="ds-note">{{.}}</p>{{end}}`.

In `previewHeights`, change the three Display rows to:

```go
	"partial-status-pill": 100, // grouped: four labelled pills in one row
	"partial-badge":       100, // grouped: five labelled badges in one row
	"partial-meter":       100, // grouped: five labelled meters in one row
```

and add to `previewMobileHeights`, with the map's comment extended by `A grouped row is one too: its states sit side by side at 900px and wrap into rows on a phone.`:

```go
	"partial-status-pill": 170, // the row of four wraps into rows
	"partial-badge":       170,
	"partial-meter":       260,
```

In `gallery.css`, after `.ds-view--page { --ds-wd: 1200px; }`, add:

```css
/* A grouped row's frame sits flush with its tabs, not centred: its
   states start at the inline start like the labels above them. */
.ds-view--rows .ds-view__box { margin-inline-start: 0; }
```

- [ ] **Step 5: The prose**

Write `$TMPDIR/copy-task-16.json` and apply it with the committed copy tool (Task 4):

```bash
cat > "$TMPDIR/copy-task-16.json" <<'EDIT'
{
 "approved": [
  "copy-review/batch-b1-result.json"
 ],
 "remove": [
  "Default tone (neutral)"
 ],
 "add": [
  {
   "id": "gallery.note.tone",
   "en": "Tone defaults to neutral, so this call leaves it out.",
   "tr": {
    "ga": "Is é neutral an Tone réamhshocraithe, mar sin fágann an glao seo ar lár é.",
    "zh-Hans": "Tone 默认是 neutral，所以这个调用省略了它。",
    "es": "Tone es neutral por defecto, así que esta llamada lo omite.",
    "hi": "Tone अपने-आप neutral होता है, इसलिए यह कॉल उसे छोड़ देती है।",
    "pt": "Tone é neutral por omissão, por isso esta chamada omite-o.",
    "bn": "Tone ডিফল্টভাবে neutral, তাই এই কলে সেটি বাদ রাখা হয়েছে।",
    "ru": "По умолчанию Tone равен neutral, поэтому в этом вызове его нет.",
    "ja": "Tone の既定値は neutral なので、この呼び出しでは省いています。",
    "yue": "Tone 預設係 neutral，所以呢個呼叫冇寫佢。",
    "vi": "Tone mặc định là neutral, nên lời gọi này bỏ nó đi.",
    "ar": "قيمة Tone الافتراضية هي neutral، لذا يحذفها هذا الاستدعاء."
   }
  }
 ],
 "fill": [
  "internal/designsystem/samples.go",
  "internal/designsystem/page.go"
 ]
}
EDIT
GOFLAGS=-mod=mod go run ./internal/copyedit/run.go -edit "$TMPDIR/copy-task-16.json"
```

Expected: `copyedit: prose.go: +1 -1`. Then one `copyedit: <file>: N filled` line for each of `internal/designsystem/samples.go`, `internal/designsystem/page.go`, whose `⟦id⟧` markers this task wrote; afterwards `grep -c '⟦' internal/designsystem/samples.go internal/designsystem/page.go` prints `0` for each. The tool refuses to write if an `en` is not the approved text for its id, if a translation is missing, carries an em dash or a backtick, or drops or adds a placeholder. The `tr` values are machine drafts of the `en`.

- [ ] **Step 6: Run the tests and measure the three frames**

Run: `GOFLAGS=-mod=mod go test -count=1 ./internal/designsystem/`
Expected: PASS. `TestEveryFrameTitleIsUniqueOnThePage` holds the grouped titles.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestPreviewFrameHeightsFitTheirContent|TestTheCopyButtonCopiesAnnouncesAndFailsSafely|TestThePreviewWidgetIsUsableOnAPhone' -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS. If the height drive names one of the three ids, set its height to the number the failure gives, Desktop in `previewHeights` and Mobile in `previewMobileHeights`. The copy-name check on Display now reads per-state names inside the grouped panels.

- [ ] **Step 7: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 8: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/samples.go \
  internal/designsystem/page.go \
  internal/designsystem/gallery.css \
  internal/designsystem/prose.go \
  internal/designsystem/designsystem_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Show status-pill, badge and meter as one row of states each

A single pill cost a 190px card with its own tab bar and a 900px
frame, and Display ran to 10,900px on a phone. These three now frame
all their states in one row that wraps on a phone. The Code panel
gives each state's label and call, then one disclosure with the
rendered HTML under the same labels. That takes 12 frames off Display.

status-pill's \"Default tone (neutral)\" rendered the same bytes as
neutral, so it goes. The neutral call drops Tone and says why. A test
now fails on any two states of a partial that render alike.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 17: On a phone Mobile is the page at its real size, and Desktop pans from the inline start in either direction

Spec 2.9 (the bleed, `--ds-wm`, no media query deciding "phone") and 2.10 (the frame anchored at the inline start). Tests: "Mobile is the page at its real size" and "Right to left". Migrated: `TestThePreviewWidgetIsUsableOnAPhone`'s expected Mobile width and `boxes`' width reading, and `TestThePreviewDefaultIsMonotoneInStageWidth`'s comments.

**Files:**
- Modify: `internal/designsystem/gallery.css` (the `.ds-view` rule, the three Mobile declarations, the `@supports` block, `.ds-view__frame`, and a bleed block at the end)
- Modify: `internal/designsystem/browser_test.go`: `previewBox`, `readBoxes`, `agree`, `opensOn` and its call sites, the Desktop pan check in `TestThePreviewWidgetIsUsableOnAPhone`, and the monotone sweep's comments
- Modify: `internal/designsystem/designsystem_test.go` (the gallery.css rules `TestEveryExampleIsFramedDesktopMobileAndCode` asserts)
- Create: `internal/designsystem/realsize_browser_test.go`

**Interfaces:**
- Consumes: `phoneRig`, `requireCoarse` (Task 12); `eagerly` (Task 2); `clickAll`, `clickedDesktop`, `boxes` (existing).
- Produces: CSS `--ds-wm` and `--ds-bleed`. `previewBox.Width` now reads the frame's laid-out width (`offsetWidth` in px), plus new `BoxL`, `BoxR` and `DocW` fields. `func mobileWidth(r previewBox) string`. Task 18 relies on Mobile frames below 390px.

- [ ] **Step 1: Write the failing drives**

Create `internal/designsystem/realsize_browser_test.go`:

```go
//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// realSize reads every Mobile frame's transform, its laid-out and
// rendered widths, a text field's computed and rendered font size, and
// the box's inline edges against the document's.
const realSize = `JSON.stringify([...document.querySelectorAll(".ds-view")].filter(v => getComputedStyle(v.querySelector(".ds-view__stage")).display !== "none").map(v => {
  const box = v.querySelector(".ds-view__box"), f = v.querySelector(".ds-view__frame"), b = box.getBoundingClientRect(), fr = f.getBoundingClientRect();
  const lit = [...v.querySelectorAll(".ds-view__tab")].findIndex(l => getComputedStyle(l).fontWeight === "600");
  let field = null;
  try { const i = f.contentDocument.querySelector('input:not([type]), input[type=text], input[type=email], input[type=url]'); if (i) field = parseFloat(f.contentDocument.defaultView.getComputedStyle(i).fontSize); } catch (e) {}
  return {ID: (v.closest("article, section") || {}).id || "", Lit: lit, Transform: getComputedStyle(f).transform,
    Layout: f.offsetWidth, Rendered: fr.width, Field: field, BoxL: b.left, BoxR: b.right, DocW: document.documentElement.clientWidth};
}))`

type realReading struct {
	ID                                   string
	Lit                                  int
	Transform                            string
	Layout, Rendered, BoxL, BoxR, DocW   float64
	Field                                *float64
}

func readReal(t *testing.T, ctx context.Context, where string) []realReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(realSize, &raw)); err != nil {
		t.Fatalf("%s: reading the frames: %v", where, err)
	}
	var rs []realReading
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		t.Fatalf("%s: decoding: %v", where, err)
	}
	if len(rs) == 0 {
		t.Fatalf("%s: no frame showing", where)
	}
	return rs
}

// On a phone, Mobile is the page at its real size: the frame is laid
// out at the stage's width up to 390px and never scaled, so a 16px
// field is 16px, and the box reaches the screen's edges. Auto opens on
// Mobile there, as it always has; the Mobile tab lit is the first
// thing asserted, or the rest measures some other rendering.
func TestMobileIsThePageAtItsRealSize(t *testing.T) {
	coarse := phoneRig(t)
	cctx, ccancel := context.WithTimeout(coarse.Context(), 240*time.Second)
	defer ccancel()
	fine := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	fctx, fcancel := context.WithTimeout(fine.Context(), 240*time.Second)
	defer fcancel()
	for _, rig := range []struct {
		name   string
		ctx    context.Context
		origin string
		widths []int64
	}{{"coarse", cctx, coarse.Origin, []int64{390, 320}}, {"fine", fctx, fine.Origin, []int64{390}}} {
		for _, kind := range []string{"form", "shells"} {
			for _, locale := range []string{"en", "ar"} {
				for _, w := range rig.widths {
					where := fmt.Sprintf("day/%s %s at %dpx, %s pointer", locale, kind, w, rig.name)
					if err := chromedp.Run(rig.ctx, chromedp.EmulateViewport(w, 844),
						chromedp.Navigate(rig.origin+pageHref(mountPath, "day", locale, fileOf(kind))),
						chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
						t.Fatalf("%s: loading: %v", where, err)
					}
					if rig.name == "coarse" {
						requireCoarse(t, rig.ctx)
					}
					eagerly(t, rig.ctx, where)
					var sideways float64
					if err := chromedp.Run(rig.ctx, chromedp.Evaluate(`document.documentElement.scrollWidth - document.documentElement.clientWidth`, &sideways)); err != nil {
						t.Fatalf("%s: %v", where, err)
					}
					if sideways > 0.5 {
						t.Errorf("%s: the page scrolls %.1fpx sideways", where, sideways)
					}
					fields := 0
					for _, r := range readReal(t, rig.ctx, where) {
						if r.Lit != 1 {
							t.Fatalf("%s: %s opens with tab %d lit; Auto on a phone opens on Mobile, and the readings below assume it", where, r.ID, r.Lit)
						}
						if r.Transform != "matrix(1, 0, 0, 1, 0, 0)" && r.Transform != "none" {
							t.Errorf("%s: %s is transformed %s; Mobile is never scaled", where, r.ID, r.Transform)
						}
						if math.Abs(r.Layout-r.Rendered) > 0.5 || r.Layout > 390 {
							t.Errorf("%s: %s lays out at %.1fpx and renders at %.1fpx; want one width, at most 390px", where, r.ID, r.Layout, r.Rendered)
						}
						if math.Abs(r.BoxL) > 1 || math.Abs(r.BoxR-r.DocW) > 1 {
							t.Errorf("%s: %s's box runs %.1f…%.1f in a %.0fpx document; on a phone the stage is the screen's width", where, r.ID, r.BoxL, r.BoxR, r.DocW)
						}
						if r.Field != nil {
							fields++
							if rendered := *r.Field * r.Rendered / r.Layout; math.Abs(*r.Field-16) > 0.1 || math.Abs(rendered-16) > 0.1 {
								t.Errorf("%s: %s's text field computes %.2fpx and renders %.2fpx; a phone's field is 16px", where, r.ID, *r.Field, rendered)
							}
						}
					}
					if kind == "form" && fields == 0 {
						t.Errorf("%s: no text field measured on Form; the 16px claim was checked against nothing", where)
					}
				}
			}
		}
	}
	// The control: on a desktop the Mobile rendering is the 390px phone
	// it has always been, centred in its stage.
	where := "day/en form at 1280px, Mobile chosen"
	if err := chromedp.Run(fctx, chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(fine.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
		t.Fatalf("%s: loading: %v", where, err)
	}
	clickAll(t, fctx, where, clickedMobile, "Mobile")
	for _, r := range readReal(t, fctx, where)[:3] {
		if r.Layout != 390 {
			t.Errorf("%s: %s lays out at %.1fpx, want 390", where, r.ID, r.Layout)
		}
	}
}

// Desktop on a phone pans, and it pans from the page's start in either
// direction: in Arabic the frame is anchored at the box's right edge
// and the box scrolls left. Anchored at the physical left, a
// right-to-left box could not scroll at all, and the start of an
// Arabic page, its right-hand 339px, was cropped.
func TestDesktopPansFromTheInlineStart(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()
	for _, c := range []struct {
		kind  string
		width float64
	}{{"form", 900}, {"shells", 1200}} {
		for _, locale := range []string{"ar", "en"} {
			where := fmt.Sprintf("day/%s %s at 390px, Desktop chosen", locale, c.kind)
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", locale, fileOf(c.kind))),
				chromedp.WaitVisible(`.ds-view__box`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			// Each widget's own Desktop radio, because Shells has no
			// Code tab and so no page-wide group, and one route serves
			// both pages.
			clickAll(t, ctx, where, clickedDesktop, "Desktop")
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			  const rtl = document.documentElement.dir === "rtl", out = [];
			  for (const v of document.querySelectorAll(".ds-view")) {
			    const box = v.querySelector(".ds-view__box"), f = v.querySelector(".ds-view__frame");
			    const checked = v.querySelector(".ds-view__tab--d input").checked;
			    const startEdge = () => { const b = box.getBoundingClientRect(), r = f.getBoundingClientRect(); return rtl ? r.right - b.right : r.left - b.left; };
			    const endEdge = () => { const b = box.getBoundingClientRect(), r = f.getBoundingClientRect(); return rtl ? b.left - r.left : r.right - b.right; };
			    const atStart = startEdge();
			    box.scrollLeft = rtl ? -(box.scrollWidth - box.clientWidth) : box.scrollWidth - box.clientWidth;
			    const atEnd = endEdge();
			    box.scrollLeft = 0;
			    out.push({ID: (v.closest("article, section") || {}).id || "", Checked: checked, Layout: f.offsetWidth,
			      Pans: box.scrollWidth > box.clientWidth, Start: atStart, End: atEnd});
			  }
			  return JSON.stringify(out);
			})()`, &raw)); err != nil {
				t.Fatalf("%s: measuring: %v", where, err)
			}
			var rs []struct {
				ID          string
				Checked     bool
				Layout      float64
				Pans        bool
				Start, End  float64
			}
			if err := json.Unmarshal([]byte(raw), &rs); err != nil {
				t.Fatalf("%s: decoding: %v", where, err)
			}
			// The control: every widget really is on Desktop at its
			// class's width. A widget left on Auto or Mobile would never
			// overflow, and everything after this would pass on nothing.
			for _, r := range rs {
				if !r.Checked || r.Layout != c.width {
					t.Fatalf("%s: %s has Desktop checked %v and lays out at %.0fpx, want %.0fpx", where, r.ID, r.Checked, r.Layout, c.width)
				}
			}
			for _, r := range rs {
				if !r.Pans {
					t.Errorf("%s: %s's box does not scroll", where, r.ID)
				}
				if math.Abs(r.Start) > 1 {
					t.Errorf("%s: at rest %s's frame starts %.1fpx from its box's inline start", where, r.ID, r.Start)
				}
				if math.Abs(r.End) > 1 {
					t.Errorf("%s: scrolled to the end, %s's frame ends %.1fpx from its box's inline end", where, r.ID, r.End)
				}
			}
		}
	}
}
```

In `designsystem_test.go`'s `TestEveryExampleIsFramedDesktopMobileAndCode`, change the two asserted rules ending `--ds-w: 390px; }` to end `--ds-w: var(--ds-wm); }`, and add to the list:

```go
		// Mobile is the stage's width up to 390px, so on a phone it is
		// the phone's page at 1:1 and never scaled.
		`.ds-view { --ds-wm: min(390px, 100cqw); }`,
		// Anchored at the inline start: a right-to-left box can only
		// scroll leftward, so a frame hanging off its right edge was
		// unreachable.
		`[dir="rtl"] .ds-view__frame { transform-origin: top right; }`,
```

- [ ] **Step 2: Run them to verify they fail**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestMobileIsThePageAtItsRealSize|TestDesktopPansFromTheInlineStart' -count=1 ./internal/designsystem/`
Expected: FAIL. The first fails with `is transformed matrix(0.83…` and `box runs 33.0…357.0`. The second fails in Arabic with `does not scroll`, which is the control the spec measured against today's `gallery.css`.

- [ ] **Step 3: The CSS**

In `gallery.css`:

(a) In the `.ds-view { … }` rule, insert `--ds-wm: 390px;` after `--ds-w: var(--ds-wd);`.

(b) Replace `--ds-w: 390px;` with `--ds-w: var(--ds-wm);` in the three Mobile declarations: the two inside the `not (min-width: …)` container queries, and `.ds-view:has(.ds-view__tab--m input:checked) .ds-view__box`.

(c) In the `@supports (block-size: calc(1px * clamp(…)))` block, before the `.ds-view__box` rule, add:

```css
  /* Mobile is the stage's width up to 390px. Only below 390px of stage
     was Mobile ever scaled, and that is exactly when laying the page
     out at the stage's width is what that screen would show; min()
     keeps a 760px window's Mobile the 390px phone rather than a 760px
     tablet. --ds-k is then at least 1, so Mobile is never scaled. The
     cqw resolves where --ds-w is used, inside .ds-view, its container.
     An engine without container units keeps 390px and the old scale. */
  .ds-view { --ds-wm: min(390px, 100cqw); }
```

(d) Replace `left: 0;` and `top: 0;` in `.ds-view__frame { … }` with `inset-block-start: 0; inset-inline-start: 0;` in alphabetical position, and after the rule add:

```css
/* transform-origin has no logical keywords, so the side is a selector,
   as tokens.css does for its drawn chevrons. Anchored at the physical
   left, a right-to-left box could not scroll to the frame's start. */
[dir="rtl"] .ds-view__frame { transform-origin: top right; }
```

(e) At the end of the file, add:

```css
@media (max-width: 799.98px) {
  /* On a phone the stage is the screen's width. The bleed is what sits
     between the widget and the screen's edge: the page's padding, and
     inside a sample card its padding and border as well. Written per
     context rather than measured, so a change to either shows up as a
     sideways scroll in the reflow and phone drives instead of a quietly
     narrower stage. The tabs and the code stay in the text column. */
  .ds-view { --ds-bleed: var(--rst-sp-4); margin-inline: calc(-1 * var(--ds-bleed)); }
  .ds-sample .ds-view { --ds-bleed: calc(2 * var(--rst-sp-4) + 1px); }
  .ds-view__tabs, .ds-view__code { margin-inline: var(--ds-bleed); }
  /* Content-box sizing under max-inline-size: 100%: a border here
     would make a full-width box 2px wider than the screen. */
  .ds-view__box { border-inline-width: 0; border-radius: 0; }
}
```

In the long comment above `.ds-view`, replace the paragraph that begins `54rem is also comfortably over the 718px` from `Under it the 390px rendering is the opening view:` to the paragraph's end with: `Under it the Mobile rendering is the opening view, and on a phone it is that phone's page at its own size: the stage is the screen's width and Mobile is never scaled.`

- [ ] **Step 4: Read widths off the frame in the phone drive**

In `browser_test.go`:

(a) In `previewBox`, change the `Width` comment to `// the frame's laid-out width, "NNNpx": which rendering is on screen. Read off the frame, because Mobile's --ds-w is now min(390px, 100cqw) and an unregistered property reads back as that text.` and add after `OverX`:

```go
	BoxL, BoxR float64 // the box's inline edges in the viewport
	DocW       float64 // the document's client width
```

(b) In `readBoxes`, replace the `Width:` line with `Width: frame.offsetWidth + "px",` and add `BoxL: box.getBoundingClientRect().left, BoxR: box.getBoundingClientRect().right, DocW: document.documentElement.clientWidth,`.

(c) Add after `tabName`:

```go
// mobileWidth is the Mobile rendering's width for one widget: the
// stage's, up to 390px. On a desktop that is 390px; on a phone it is
// the phone's own width.
func mobileWidth(r previewBox) string {
	return fmt.Sprintf("%dpx", int(math.Round(math.Min(390, r.View))))
}
```

(d) In `agree`, replace `w = "390px"` with `w = mobileWidth(r)`. In `opensOn`, make an empty `width` mean the Mobile width: add `want := width; if want == "" { want = mobileWidth(r) }` at the loop's top and compare `r.Width != want` in its message. Change every `opensOn(…, 1, "390px")` call to `opensOn(…, 1, "")`.

(e) In `TestThePreviewWidgetIsUsableOnAPhone`, after the `panned` check, add:

```go
		// And it pans across the whole screen: on a phone the box bleeds
		// to the viewport's edges, so a reader pans the screen's width of
		// it, not a column's.
		for _, r := range chosen {
			if math.Abs(r.BoxL) > 1 || math.Abs(r.BoxR-r.DocW) > 1 {
				t.Errorf("%s at 390px with Desktop chosen: %s's box runs %.1f…%.1f in a %.0fpx document; it should span the screen", kind, r.ID, r.BoxL, r.BoxR, r.DocW)
			}
		}
```

(f) In `TestThePreviewDefaultIsMonotoneInStageWidth`'s `monotoneSweep`, replace the comment sentence `1184 and 1186 straddle the threshold itself: with the rail in, the stage is the window less 321px, so those two windows put it at 863px and 865px.` with `1184 and 1186 straddle the threshold itself: with the rail in, the stage is the window less 321px, so those two windows put it at 863px and 865px. Below 800px the stage is the window less a scrollbar, because the widget bleeds to the screen's edges, so a component opens on Desktop in a window about 66px narrower than it once did; the property asserted, monotone in the stage, does not move.`

- [ ] **Step 5: Run the drives**

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run 'TestMobileIsThePageAtItsRealSize|TestDesktopPansFromTheInlineStart|TestThePreviewWidgetIsUsableOnAPhone|TestThePreviewDefaultIsMonotoneInStageWidth|TestA11yReflowsAt320|TestPreviewFrameHeightsFitTheirContent|TestNothingPaintsOverTheBarOrTheBackStrip' -count=1 -timeout 25m ./internal/designsystem/`
Expected: PASS. The wide height drive still measures Mobile at a 390px frame in a 1500px window. Mobile boxes on a phone are `--ds-hm` tall rather than `--ds-hm × 0.83`, which Task 18 sizes for the narrowest frame.

- [ ] **Step 6: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green.

- [ ] **Step 7: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="internal/designsystem/gallery.css \
  internal/designsystem/browser_test.go \
  internal/designsystem/designsystem_test.go \
  internal/designsystem/realsize_browser_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Show Mobile at its real size on a phone, and pan right to left

On a 390px phone, Mobile laid a 390px page into a 324px stage and
scaled it to 0.83, so a 16px field showed at 13px and the preview was
no longer the thing it previewed. Below 800px the widget now bleeds to
the screen's edges, and Mobile is the stage's width up to 390px. It is
never scaled: on a phone it is that phone's page, and on a desktop it
is the 390px phone it always was. min() decides, not a media query, so
a narrow desktop window gets the same answer.

The frame is anchored at the inline start. In Arabic, Desktop on a
phone could not scroll at all and the start of the page was cropped,
because a right-to-left box only scrolls leftward.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---
### Task 18: Mobile heights fit the narrowest phone frame, with a CI bound

Spec 2.9 ("Mobile heights are sized for the narrowest frame"), the Tests entry `TestPreviewFrameHeightsFitAtThePhonesNarrowest` (coverage by viewport, the locale subset, the assertions), and "CI bound": `make browser-sweep`, no fixed sleeps, deadlines from measurement, and the narrow test in a package of its own.

**Files:**
- Modify: `internal/designsystem/hooks.go` (`ExampleCounts`)
- Modify: `internal/designsystem/galleryrig/rig.go` (`MeasureFrames`, `ReadFrames`, `Measured`, `HeightRow`, `HeightRows`, `FramesNothing`)
- Modify: `internal/designsystem/browser_test.go`: the wide drive reads the shared instrument and table; delete its `measure`, `rows`, `framesNothing` and the package's `measured`
- Modify: `internal/designsystem/rig_browser_test.go`, `internal/designsystem/sweep/rig_test.go` (`heightRows`, and in the sweep package `eagerly`, `mobileSettle`, `clickedMobile`)
- Create: `internal/designsystem/sweep/narrow_test.go`
- Modify: `internal/designsystem/page.go` (`previewMobileHeights` and its comment)
- Modify: `Makefile` (a `browser-sweep` target, its `.PHONY` entry and its order-only `$(BIN)/tmp` prerequisite)

**Interfaces:**
- Consumes: `galleryrig.Eagerly`, `galleryrig.MobileSettle`, `galleryrig.ClickEvery` (Task 2); the hooks (Task 11); Task 17's Mobile at the stage's width.
- Produces: `func designsystem.ExampleCounts() map[string]int`; in `galleryrig`, `const MeasureFrames` (JS), `func ReadFrames(t *testing.T, tab, raw string) map[string][3]int` (decode, no judgement), `func Measured(t *testing.T, tab, raw string, shellOnly bool) map[string][3]int` (judged), `type HeightRow struct{ Kind string; Least int; Owed string }`, `func HeightRows(kinds []string, counts map[string]int) []HeightRow`, `var FramesNothing map[string]string`. Also the `RASTRILLO_GALLERY_SWEEP` variable and `make browser-sweep`.

- [ ] **Step 1: Share the wide drive's instrument and its coverage table**

Add to `internal/designsystem/hooks.go` (add the `ui` import):

```go
// ExampleCounts is, per page kind that frames examples, the least number
// of its sections with an example to measure: every partial of a
// family, every Styleguide sample, every shell, screen and format, and
// the Overview's demo application. The height drives read it, so a
// component documented with nothing to look at fails there rather than
// only on a reader's screen.
func ExampleCounts() map[string]int {
	out := map[string]int{
		"overview":   1,
		"primitives": len(ui.Styleguide()),
		"shells":     len(ui.LayoutNames()),
		"screens":    len(screenDocs()),
		"formats":    len(formatDocs()),
	}
	for _, fam := range families() {
		out[fam.Key] = len(fam.Partials)
	}
	return out
}
```

Append to `internal/designsystem/galleryrig/rig.go` (add the `sort` import):

```go
// MeasureFrames reads every frame on the page: section id → [what its
// document needs, what its box gives it, 1 if it frames a sidebar-shell
// page]. The tallest state of a section is the one recorded, so one
// number per section keeps the boxes down a column the same size.
const MeasureFrames = `(() => {
  const out = {};
  for (const f of document.querySelectorAll(".ds-view__frame")) {
    const section = f.closest("article, section");
    const id = section ? section.id : "?";
    const d = f.contentDocument;
    const need = d ? Math.ceil(Math.max(d.body.getBoundingClientRect().height, d.body.scrollHeight)) : -1;
    const box = Math.round(parseFloat(getComputedStyle(f).height));
    const shell = d && d.querySelector("[rst-shell-sidebar]") ? 1 : 0;
    const was = out[id];
    if (!was || need > was[0]) out[id] = [need, box, shell];
  }
  return JSON.stringify(out);
})()`

// ReadFrames decodes one MeasureFrames reading: section id → [what its
// document needs, its box, 1 for a sidebar-shell page]. It judges no
// height, so a sweep can gather every language's readings before it
// asserts anything; it fails only on what makes a reading worthless: a
// reading that does not decode, a page with no rendered example, or a
// frame with no document in it.
func ReadFrames(t *testing.T, tab, raw string) map[string][3]int {
	t.Helper()
	var got map[string][3]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: reading the measurements: %v", tab, err)
	}
	if len(got) == 0 {
		t.Fatalf("%s: no section on this page has a rendered example at all; either the page rendered none, or no frame on it loaded", tab)
	}
	for id, r := range got {
		if r[0] < 0 {
			t.Errorf("%s %s: the frame has no document in it", tab, id)
		}
	}
	return got
}

// Measured is ReadFrames with each section held to its box. shellOnly
// decides who gets 48px of grace: the sidebar shell's rail is 100dvh
// tall, so a page framing one is always the frame's height plus the
// margin under its content, and no box can fit it. The wide drive
// grants the grace to every frame, as it always has.
func Measured(t *testing.T, tab, raw string, shellOnly bool) map[string][3]int {
	t.Helper()
	got := ReadFrames(t, tab, raw)
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, id := range names {
		need, box, shell := got[id][0], got[id][1], got[id][2]
		grace := 48
		if shellOnly && shell == 0 {
			grace = 0
		}
		if need >= 0 && need > box+grace {
			t.Errorf("%s %s: its document needs %dpx and its frame is %dpx; raise its height (previewHeights for Desktop, previewMobileHeights for Mobile) to at least %d", tab, id, need, box, need+20)
		}
		if need >= 0 && box > need*4 && box-need > 120 {
			t.Logf("%s %s: %dpx of frame for %dpx of document; deliberate headroom, or a number to bring down", tab, id, box, need)
		}
	}
	return got
}

// HeightRow is one page kind the height drives measure, the least
// number of its sections with an example, and why it owes them.
type HeightRow struct {
	Kind  string
	Least int
	Owed  string
}

// owed says why each page that is not a family page owes its sections.
var owed = map[string]string{
	"overview":   "the demo application is framed here",
	"primitives": "every sample ui.Styleguide() ships has a section here",
	"shells":     "every shell ui.LayoutNames() reports has a section here",
	"screens":    "every screen screenDocs() ships has a section here",
	"formats":    "every section formatDocs() ships has a sample here",
}

// HeightRows is the height drives' coverage table: each page kind in
// counts, in the order kinds lists them.
func HeightRows(kinds []string, counts map[string]int) []HeightRow {
	var out []HeightRow
	for _, k := range kinds {
		n, ok := counts[k]
		if !ok {
			continue
		}
		why := owed[k]
		if why == "" {
			why = "every partial samples.go puts in this family has a section here"
		}
		out = append(out, HeightRow{Kind: k, Least: n, Owed: why})
	}
	return out
}

// FramesNothing names the page kinds with no preview frame, each with
// why, so a page kind that grows frames cannot go unmeasured: a kind in
// neither table fails both height drives.
var FramesNothing = map[string]string{
	"tokens":          "a swatch grid and two scale tables; no preview frames",
	"icons":           "inline SVG drawn directly on the page, not framed",
	"getting-started": "prose, links and two source blocks",
}
```

Add to `internal/designsystem/rig_browser_test.go`:

```go
func heightRows() []galleryrig.HeightRow { return galleryrig.HeightRows(PageKinds(), ExampleCounts()) }
```

In `browser_test.go`'s `TestPreviewFrameHeightsFitTheirContent`, delete the `measure` script, the `rows` table and the local `framesNothing` map, keeping their comments above the loop. Loop over `heightRows()` (fields `Kind`, `Least`, `Owed`), use `galleryrig.FramesNothing` in the coverage check, `galleryrig.MeasureFrames` for both readings, and `len(galleryrig.Measured(t, kind+" "+tab.name, tab.raw, false))` for the count. Delete the package's own `measured` function.

Run: `RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestPreviewFrameHeightsFitTheirContent -count=1 -timeout 20m ./internal/designsystem/`
Expected: PASS, the same drive as before on a shared instrument.

- [ ] **Step 2: Write the failing narrow drive**

Add to `internal/designsystem/sweep/rig_test.go` (add the `galleryrig` import if missing):

```go
func eagerly(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.Eagerly(t, ctx, where)
}

func mobileSettle(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	galleryrig.MobileSettle(t, ctx, where)
}

var clickedMobile = galleryrig.ClickEvery("m")

func heightRows() []galleryrig.HeightRow {
	return galleryrig.HeightRows(designsystem.PageKinds(), designsystem.ExampleCounts())
}
```

Create `internal/designsystem/sweep/narrow_test.go`:

```go
//go:build browser

package sweep

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// narrowExempt is the page kinds the narrow drive does not measure, each
// with its reason. An entry naming a page that does display a preview
// below 800px fails the drive, so the list cannot grow quietly.
var narrowExempt = map[string]string{
	"overview": "below 800px the Overview is the phone index: its main, which frames the demo application, is display: none, so no preview is displayed to measure. The demo's own pages are checked at phone width by TestA11yReflowsAt320",
}

// narrowLocales is the routine run's languages: en, ar, and the two
// whose labels wrap worst at the narrowest frame, found by one full
// sweep (make browser-sweep) comparing, per page, how far each
// language's content height exceeds en's. RASTRILLO_GALLERY_SWEEP=1
// runs all twelve, which is owed before any release that changes locale
// strings or preview content.
var narrowLocales = []string{"en", "ar"}

// The two runs' deadlines, each twice its own measured time on a quiet
// runner: the routine subset, and the twelve-language sweep, which does
// three times the work and would be killed by the routine's deadline.
const (
	routineDeadline = 600 * time.Second
	sweepDeadline   = 1500 * time.Second
)

// TestPreviewFrameHeightsFitAtThePhonesNarrowest holds every Mobile
// frame to its box at the narrowest frame the gallery commits to: a
// 320px viewport, the reflow width. Below 390px a Mobile frame lays out
// at the stage's width, so text that fits a line at 390px can wrap at
// 305px, and a height measured at 390px is no longer a ceiling.
func TestPreviewFrameHeightsFitAtThePhonesNarrowest(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	locales, deadline := narrowLocales, routineDeadline
	if os.Getenv("RASTRILLO_GALLERY_SWEEP") == "1" {
		locales, deadline = rastrillo.BaseLocales(), sweepDeadline
	}
	ctx, cancel := context.WithTimeout(rig.Context(), deadline)
	defer cancel()
	root := designsystem.RootTheme()
	covered := map[string]bool{}
	for _, row := range heightRows() {
		covered[row.Kind] = true
	}
	for _, kind := range designsystem.PageKinds() {
		if !covered[kind] && galleryrig.FramesNothing[kind] == "" {
			t.Errorf("page kind %q has no height row and is not listed as framing nothing", kind)
		}
	}
	// The exemptions, held to their reason: no visible preview at 320px.
	for kind := range narrowExempt {
		var shown int
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, root, "en", fileOf(kind))),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
			chromedp.Evaluate(`[...document.querySelectorAll(".ds-view__frame")].filter(f => f.getBoundingClientRect().height > 0).length`, &shown)); err != nil {
			t.Fatalf("exempt %s at 320px: %v", kind, err)
		}
		if shown > 0 {
			t.Errorf("narrowExempt names %s, which shows %d previews at 320px; measure it instead", kind, shown)
		}
	}

	worst := map[string]int{}               // id → the largest need across the languages run
	box := map[string]int{}                 // id → its Mobile box
	shell := map[string]bool{}              // id → frames a sidebar-shell page
	byLocale := map[string]map[string]int{} // locale → id → need, for the sweep's comparison
	for _, locale := range locales {
		byLocale[locale] = map[string]int{}
		for _, row := range heightRows() {
			if narrowExempt[row.Kind] != "" {
				continue
			}
			where := fmt.Sprintf("%s/%s at 320px", locale, row.Kind)
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 844),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, root, locale, fileOf(row.Kind))),
				chromedp.WaitVisible(`.ds-view__frame`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			if err := chromedp.Run(ctx, chromedp.Evaluate(clickedMobile, nil)); err != nil {
				t.Fatalf("%s: choosing Mobile: %v", where, err)
			}
			mobileSettle(t, ctx, where)
			// The control: every frame is laid out at its stage's width,
			// under 390px, so this is the narrow frame and not the 390px
			// one the wide drive already measures.
			var wide []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll(".ds-view")].filter(v => {
			  const f = v.querySelector(".ds-view__frame"); return !(f.offsetWidth < 390 && Math.abs(f.offsetWidth - v.clientWidth) <= 1); }).map(v => (v.closest("article, section") || {}).id)`, &wide)); err != nil {
				t.Fatalf("%s: checking the frame widths: %v", where, err)
			}
			if len(wide) > 0 {
				t.Fatalf("%s: frames %v are not at the stage's narrow width; the readings would be of the 390px frame", where, wide)
			}
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(galleryrig.MeasureFrames, &raw)); err != nil {
				t.Fatalf("%s: measuring: %v", where, err)
			}
			// Read, not judged: every language is measured before any
			// height is asserted, so one run gives the whole table.
			got := galleryrig.ReadFrames(t, where, raw)
			if len(got) < row.Least {
				t.Errorf("%s: %d sections measured, want at least %d: %s", where, len(got), row.Least, row.Owed)
			}
			for id, r := range got {
				byLocale[locale][id] = r[0]
				box[id], shell[id] = r[1], r[2] == 1
				if r[0] > worst[id] {
					worst[id] = r[0]
				}
			}
		}
	}
	ids := make([]string, 0, len(worst))
	for id := range worst {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// The only height assertion, made once over every language run. A
	// component preview gets no grace, so nothing in it is reached by
	// scrolling inside it; a sidebar-shell page keeps the 48px its
	// 100dvh rail always costs.
	for _, id := range ids {
		grace := 0
		if shell[id] {
			grace = 48
		}
		if worst[id] > box[id]+grace {
			t.Errorf("%s: at the narrowest phone frame its document needs %dpx in its tallest language and its Mobile box is %dpx; set previewMobileHeights[%q] to at least %d", id, worst[id], box[id], id, worst[id]+20)
		}
	}
	// What the sweep is for: per language, the most any preview's height
	// exceeds en's. The routine subset is the worst one or two of these.
	for _, locale := range locales {
		most, at := 0, ""
		for id, need := range byLocale[locale] {
			if d := need - byLocale["en"][id]; d > most {
				most, at = d, id
			}
		}
		t.Logf("%s: at most %dpx taller than en (%s)", locale, most, at)
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

```bash
set -o pipefail; mkdir -p "$TMPDIR/gate"
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestPreviewFrameHeightsFitAtThePhonesNarrowest -count=1 -v -timeout 20m ./internal/designsystem/sweep/ 2>&1 | tee "$TMPDIR/gate/task18-narrow.log"; echo "exit $?"
grep -E 'set previewMobileHeights|taller than en|^(---|ok|FAIL)' "$TMPDIR/gate/task18-narrow.log"
```

Expected: `exit 1`, FAIL, and every error a `set previewMobileHeights` line naming a preview that wraps taller at 305px than its 1.25 factor allows, with the number to write. The drive reads every page in every language before it judges a height, so those lines are its only overflow diagnostic. Any other error makes this the wrong red: a compile error, `reading the measurements`, `no section on this page`, `the frame has no document`, `never settled`, a context deadline, or a frame-width control. Read the full log and fix that first.

- [ ] **Step 4: Find the routine languages, then size the heights**

Run the full sweep once:

```bash
set -o pipefail
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod RASTRILLO_GALLERY_SWEEP=1 go test -tags browser -run TestPreviewFrameHeightsFitAtThePhonesNarrowest -count=1 -v -timeout 40m ./internal/designsystem/sweep/ 2>&1 | tee "$TMPDIR/gate/task18-sweep.log"; echo "exit $?"
grep -E 'set previewMobileHeights|taller than en|^(---|ok|FAIL)' "$TMPDIR/gate/task18-sweep.log"
```

Expected: a FAIL whose only errors are `set previewMobileHeights` lines, plus twelve `taller than en` lines. Any other error (a load, decode or settle failure, or a timeout) means the sweep did not measure all twelve, and its numbers are not used. From the `taller than en` lines, take the one or two languages with the largest excess. Write them into `narrowLocales` after `"en", "ar"`, with the measurement in its comment, for example `// measured <date>: hi at most 64px taller than en (partial-choice-field), bn 48px (idiom-form-layout)`, using the real numbers. Then, for every `set previewMobileHeights[…] to at least N` line in the sweep, add an entry to `previewMobileHeights` with that `N` and a short reason ("wraps taller at the narrowest phone frame"). Extend the map's comment:

```go
// previewMobileHeights overrides the 1.25× factor for the examples
// whose Mobile layout is taller than the factor allows: one that
// reflows onto a different axis at 390px, and, since Mobile lays out at
// the stage's own width below 390px, one whose text wraps taller at
// the narrowest phone frame, a 320px viewport. Measured by the two
// height drives and held by them: a number too small fails, and the
// slack a number too large leaves is logged.
```

Run the full sweep again. Expected: `exit 0`.

- [ ] **Step 5: Add `make browser-sweep`**

In the `Makefile`, add `browser-sweep` to the `.PHONY` list and to the order-only line `root staticcheck … browser browser-sweep: | $(BIN)/tmp`, and after the `browser` target:

```make
# browser-sweep runs the narrow-phone height drive over all twelve
# locales. Routine CI runs it on a measured subset (en, ar and the worst
# wrappers, see narrowLocales in internal/designsystem/sweep), because
# the full twelve take most of a package's time budget. Not part of ci
# and not in .amadan/ci.d/: run it, and record its result on the branch,
# before any release that changes locale strings or preview content.
browser-sweep:
	TMPDIR="$${TMPDIR:-/var/tmp}" RASTRILLO_GALLERY_SWEEP=1 go test -tags browser -timeout 40m -run TestPreviewFrameHeightsFitAtThePhonesNarrowest ./internal/designsystem/sweep/ -count=1
```

- [ ] **Step 6: Set both deadlines from measurement, then re-run both**

On a quiet machine, time each mode twice. Only a run that exits 0 counts:

```bash
set -o pipefail
for i in 1 2; do
  RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestPreviewFrameHeightsFitAtThePhonesNarrowest -count=1 -v -timeout 20m ./internal/designsystem/sweep/ 2>&1 | tee "$TMPDIR/gate/task18-routine-$i.log"; echo "routine $i exit $?"
  RASTRILLO_CHROME=/usr/bin/chromium make browser-sweep 2>&1 | tee "$TMPDIR/gate/task18-full-$i.log"; echo "full $i exit $?"
done
grep -h -- '--- PASS: TestPreviewFrameHeightsFitAtThePhonesNarrowest' "$TMPDIR"/gate/task18-*-[12].log
```

Expected: four `exit 0` lines and four `--- PASS … (Ns)` lines. Take the larger routine time `R` and the larger full time `S`. Set `routineDeadline` to `2*R` and `sweepDeadline` to `2*S`, each rounded up to the next 10 seconds, and replace the comment above them with the measurements: `// … twice its own measured time on a quiet runner: R seconds for the routine subset (en, ar, …) and S seconds for all twelve, on <date>.`, using the real values. If `2*S` plus 5 minutes exceeds `browser-sweep`'s `-timeout 40m`, raise that `-timeout` to `2*S` plus 5 minutes, so `go test` never kills the run before its own deadline reports. Then run both modes once more with the deadlines set. Expected: `exit 0` for each.

- [ ] **Step 7: Run the task gate** (Global Constraints: the gate line, `make ci` timed and logged, and the time bound, with the package seconds in the report). Expected: all green, with `internal/designsystem/sweep` under 800 s.

- [ ] **Step 8: Commit and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="Makefile \
  internal/designsystem/hooks.go \
  internal/designsystem/browser_test.go \
  internal/designsystem/rig_browser_test.go \
  internal/designsystem/page.go \
  internal/designsystem/galleryrig/rig.go \
  internal/designsystem/sweep/rig_test.go \
  internal/designsystem/sweep/narrow_test.go"
maybe=".rastrillo/budgets.txt"
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Hold Mobile heights at the narrowest phone frame

Mobile now lays out at the stage's own width below 390px, so a height
measured at 390px stopped being a ceiling: text wraps taller at the
305px a 320px phone gives. A second drive measures every preview there,
with no grace for a component that would otherwise be scrolled inside
its box. The Overview is exempt by name with its reason, and the
exemption fails if that page ever shows a preview at phone width.

It lives in the sweep package, so it has a time limit of its own. Routine
CI runs en, ar and the measured worst wrappers. make browser-sweep runs
all twelve and is owed before a release that changes strings. Each mode
has a deadline of twice its own measured run, so the full sweep is not
killed by the routine deadline.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

---

### Task 19: Final acceptance: the whole gate, measured, and what the branch description must carry

Spec "CI bound": the whole of `make ci` must stay under 20 minutes on a quiet runner, measured not estimated, recorded here with the per-package times, and each browser package must leave a third of its 20-minute limit free. Every task since Task 2 has checked those bounds at its own gate, so this is the final acceptance on the finished branch, not the first look. Also the branch description: what landed, the machine-drafted translations, and the measurements the spec's Risks asked for.

**Files:** none, unless the gate finds something. A fix goes in a new commit whose body names the task it belongs to.

**Interfaces:** consumes everything above.

- [ ] **Step 1: Measure the whole gate on a quiet runner**

Check that nothing else is building on the machine (`uptime`, load under 1 per core), then run:

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/gallery-usability
set -o pipefail; mkdir -p "$TMPDIR/gate"; start=$(date +%s)
RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp make ci 2>&1 | tee "$TMPDIR/gate/ci-final.log"; status=$?
echo "make ci exit $status after $(( $(date +%s) - start ))s"
grep -E '^(ok|FAIL)[[:space:]]' "$TMPDIR/gate/ci-final.log"
```

Expected: `make ci exit 0` and a time under 1,200 s. If `status` is not 0, nothing below is a measurement: read the log, fix the failure in a commit naming its task, and run this step again. The `ok` lines give each package's time, for example `ok  amadan.net/rastrillo/rastrillo/internal/designsystem  612.3s`.

- [ ] **Step 2: Check the bounds**

- `internal/designsystem` and `internal/designsystem/sweep` must each be at most 800 seconds (two thirds of 20 minutes).
- The whole `make ci` must be under 20 minutes. The amadan CI job limit is a hard 30 minutes, so the margin is the point.

If either fails, stop and report to the controller with the per-package times and the time history from each task's report.

- [ ] **Step 3: Record the measurement here**

Edit this plan file: fill in the table below with the measured values, and commit it with the next step.

| Measured on | `make ci` wall clock | `internal/designsystem` (browser) | `internal/designsystem/sweep` (browser) | `ui` (browser) | other browser packages | `make browser-sweep` |
|---|---|---|---|---|---|---|
| (date, machine, load) | (m:ss) | (s) | (s) | (s) | (s) | (s) |

Run `make browser-sweep` the same way (`set -o pipefail`, `tee` to `$TMPDIR/gate/sweep-final.log`, exit status checked) for its column.

Also record from `go run ./cmd/dsgen -out "$TMPDIR/ds"` (then delete it), whose exit status must be 0:
- the tree's file count and total bytes, against the spec's projection of ~5,738 files and ~34.4 MB;
- the heaviest `form.html` and `date-and-time.html` in bytes, against the projection of ~122,400 and ~96,400, with `form.html`'s headroom under 131,072;
- Form's frame-document request count. `make ci` runs `go test` without `-v`, so a passing run prints no `t.Logf` line. Take the count from a run of its own:

  ```bash
  set -o pipefail
  RASTRILLO_CHROME=/usr/bin/chromium TMPDIR=/var/tmp GOFLAGS=-mod=mod go test -tags browser -run TestPreviewFrameHeightsFitTheirContent -count=1 -v -timeout 20m ./internal/designsystem/ 2>&1 | tee "$TMPDIR/gate/heights-final.log"; echo "exit $?"
  grep 'form at 1500px' "$TMPDIR/gate/heights-final.log"
  ```

  The run must print `exit 0`. Record N from `form at 1500px: … the page requested N frame documents`;
- `wc -c internal/designsystem/gallery.js`, against the 20,756 the cap's table predicts.

Keep `ci-final.log`, `sweep-final.log` and `heights-final.log` in `$TMPDIR/gate/` until Step 5's branch description quotes their numbers. They are the evidence for the table, so they are deleted last.

- [ ] **Step 4: Commit the record and push**

Stage the task's files by name, never with a directory, and check the staging before committing. The block prints `staged exactly this task's files` and the list, or `STOP` with what is missing or left over. On `STOP`, nothing is committed: either the task changed a file its Files list does not name, so add it to the list in the task's report and to `want`, or a listed file was never changed, so finish the step that changes it. `maybe` holds the files a task changes only when a measurement demands it, such as a budget ceiling.

```bash
want="docs/superpowers/plans/2026-10-04-gallery-usability.md"
maybe=""
git add -- $want
for f in $maybe; do git diff --quiet -- "$f" || git add -- "$f"; done
staged=$(git diff --cached --name-only)
missing=""; for f in $want; do if git diff --cached --quiet -- "$f"; then missing="$missing $f"; fi; done
extra=""; for f in $staged; do case " $(echo $want $maybe) " in *" $f "*) ;; *) extra="$extra $f" ;; esac; done
left=$(git status --porcelain --untracked-files=all | grep -v '^[MADR] ' || true)
if [ -n "$missing$extra$left" ]; then echo "STOP. Listed but unchanged:$missing"; echo "Staged but not this task's:$extra"; echo "Changed but not staged: $left"; false; else echo "staged exactly this task's files"; echo "$staged"; fi
```

Only when it printed `staged exactly this task's files`:

```bash
git commit -m "Record the gallery branch's measured CI time and tree size

The spec bounds make ci at 20 minutes on a quiet runner, and asks that
the time be measured on the finished branch rather than estimated. It
also asks for the file count, the tree's bytes and Form's preview
requests the new previews cost. This records all of them beside the
plan that predicted them.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push origin gallery-usability
```

- [ ] **Step 5: Describe the branch**

Follow the amadan skill (`.claude/skills/amadan/SKILL.md`) to describe `gallery-usability`. The description is the PR body. It must carry:

- what landed, task by task, linking the spec and this plan;
- **"The eleven translations of every new gallery string are machine-drafted and unreviewed"**, listing the 14 new prose keys;
- the measurements from Step 3;
- **docs owed:** `docs/site/templates.md`'s design-system section (around lines 666-750) still describes the drawer ("Below 800px the rail folds into the shell's own `<details>` chrome strip"), the header "top right", Mobile always at 390px, and the Code tab as markup only. That is public documentation, user-facing copy, and none of it was in this branch's copy batch. It needs a copy-review batch before merge, and the controller runs it;
- that `make browser-sweep` passed on the final commit, with its date.

End the description with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

The branch lands later through `amadan branch merge -squash`, run by the controller after review.

---
## Self-review

I ran this against the spec after writing it, and the fixes are already folded into the tasks above.

**Spec coverage.**

- 1.1: Task 4.
- 1.2: Tasks 3 and 5.
- 1.3: Tasks 3 and 5.
- 1.4: Task 6.
- 1.5: Tasks 1 and 2.
- 2.1: Tasks 9 and 10.
- 2.2: Task 10.
- 2.3: Task 10 (DOM, CSS, prerender), Task 11 (pinned bar, scroll padding, painting, focus), Task 12 (phone index, foot menu), Task 15 (keeping your place, the back/forward cache).
- 2.4: Task 13.
- 2.5: Task 8.
- 2.6: Task 14.
- 2.7: Task 16.
- 2.8: Task 4 (C14), Task 5 (C3, C4), Task 6 (C5 to C7), Task 7 (C1, C2, C12, C13, the retired preamble keys, the first sentinel), Task 8 (C11), Task 9 (the subtitle and its sentinel), Task 10 ("Sections"), Task 14 (C8, C9), Task 16 (C10, "Default tone (neutral)"). The fixture exemptions and sweep count are in Task 1.
- 2.9: Tasks 17 and 18.
- 2.10: Task 17, plus Task 11's 1280px Arabic leg.
- Copy C15 and C16: Task 9. C17: Task 6.
- Budgets: pages and debt in Task 1; `gallery.js` in Tasks 6, 13, 14 and 15, against a measured table and a cap of 22,832 bytes (the spec's Budgets updated in this plan's commit); the directory budget in Task 1.
- Tests (New): every entry maps to a test named in its task, and the Review Focus lines name the five that are this plan's own.
- Migrated: every row of the spec's table is a step. `TestEveryGalleryPageLinksTheStylesheet` needs no edit, because the preview files carry no `ds-` class and Task 16's grouped document uses an unclassed `<ul>` for that reason. `TestTheSigninScreensAreThePartial` also read `srcdoc` and was not in the spec's table. Task 1 migrates it.
- Retired: Task 10.
- CI bound: the package split (Tasks 2 and 11), each task's timed gate, deadlines (Tasks 2 and 18), and final acceptance (Task 19).
- Risks: Form's 5 KB margin is checked in Task 5, and the file count, bytes and requests are recorded in Task 19.

**Placeholders.** Four numbers can only come from measurement: the wide drive's deadline (Task 2), the narrow drive's two deadlines, routine and full sweep (Task 18), and the narrow locale subset (Task 18). Each step says exactly how to take them and where to write them. `--ds-bar-h` (Task 11) and new `previewMobileHeights` entries (Tasks 16 and 18) take the number the failing drive prints. Every other step carries its code.

**Names used across tasks.**
- Task 1: `previewFile`, `previewSet`, `newPreview`, `previewDoc`, `put`, `frameDocs`, `frameDoc`, `frameSrc`, `isPreviewDoc`, `previewMarker`, `previewBody`, `sampleTree`, `pageKindOf`.
- Task 2: the package `galleryrig` with `Tree`, `TreeRecording`, `SettleFrames`, `Eagerly`, `SettleMobile`, `MobileSettle`, `ClickEvery`; `rig_browser_test.go`'s `treeHandler`, `eagerly`, `mobileSettle`.
- Task 3: `codeview.Format`, `codeview.Highlight`, `layout`, `pieces`, `tagEnd`.
- Task 4: `codeview.Call`, `codeview.ErrUnwritable`, `call`, `callFor`, `partialKeys`, `parseKeys`, `sample.Bind`; the package `copyedit` (`Edit`, `Entry`, `LoadApproved`, `ApplyProse`, `Fill`, `Locales`) and `run.go`.
- Task 5: `previewView.Call`, `Wrapper`, `Source` (now `template.HTML`), the 9-parameter `newPreview`, `wrapperMarkup`, `sourceTexts`, `codePanels`, `darkHalf`.
- Task 6: `sample.Illustration`, `previewView.NoCopy`, `copyJoin`, `pageView.CopyJoin`, `galleryrig.AddInit`, `galleryrig.Until` (and their short names), `clipboardStub`.
- Task 7: `assetsView.Links`.
- Task 9: `titleSep`, `frameSep`, `docTitle`, `pageView.DocTitle`, `TitleID`.
- Task 10: `controls`, `newControls`, `indexRow`, `indexRows`, `navSection.Kind`, `pageView.Up`, `Home`, `Rows`, `Demos`, `Bar`, `Foot`.
- Task 11: the hooks `PageKinds`, `PageFile`, `PageHref`; `galleryrig.NoScripts`, `WithoutAnchorPositioning`, `RequireAnchorPositioning`; the package `sweep` and its `rig_test.go`.
- Task 12: `galleryrig.PhoneRig`, `RequireCoarse`, `SettleMotion`, and their short names.
- Task 13: `searchTerms`, `navItem.Terms`.
- Task 14: `pageView.ViewGroup`.
- Task 15: the shared `pressed`, `place`, `record`, `anchor`, `fragment`, `keep` in `gallery.js`.
- Task 16: `partialDoc.Row`, `rowCode`, `groupedPreview`, `previewDocStyled`, `rowsStyle`.
- Task 17: `mobileWidth`, `previewBox.BoxL`, `BoxR`, `DocW`.
- Task 18: `ExampleCounts`; `galleryrig.MeasureFrames`, `Measured`, `HeightRow`, `HeightRows`, `FramesNothing`; `heightRows` in both packages; `narrowExempt`, `narrowLocales`, `routineDeadline`, `sweepDeadline`.

I checked every use against its definition. `newPreview` deliberately changes signature in Task 5, and Task 5's step updates all of its callers.

**Review Focus.** Each of the five lines has its test in the task that owns the code: Tasks 4, 3, 3, 14 and 15.

**Plan review round 1 (Astra, on 21ee1d2d): 3 Blockers, 6 Important, all folded in, with the controller's rulings.**
- `gallery.js` was weighed for real: 24,651 bytes as first written, 20,756 after slimming. The cap is now 22,832, measured plus 10%, with the reason in the spec and the test.
- Task 5 imports `codeview` into `screens.go` only.
- Task 9 tests the separators only. A separate sweep forbids an em dash in any title, frame name or state label, and Task 9 relabels those states from batch B2.
- Copy goes through the committed, tested `internal/copyedit`, so no task copies another's code.
- The formatter's conservation test uses an independent normaliser, with inline-space fixtures and a control that a dropped space fails.
- The button labels are filled from batch B2 by id.
- The narrow drive's routine run and full sweep have separately measured deadlines, and both are re-run after setting them.
- The package split is designed now (`galleryrig`, `sweep`, four hooks), and every task's gate checks the time bound.
- Every measurement runs under `pipefail` with a full log, and its exit status is checked first.

**Plan review round 2 (Astra, on 49ecbbdd): 1 Blocker, 2 Important, 2 Minor, all folded in.**
- Every task stages its files by name and checks the staging before it commits. Every listed file must be changed and nothing else may be left changed. Tasks 11 and 12 had staged only part of their work.
- The error-status labels come from batch B3, which Global Constraints and Task 9 now name.
- Task 18 reads every language's frames before judging any height, so its first sweep fails only on `set previewMobileHeights` lines, while load, decode, settle and timeout failures still stop it.
- Task 19 takes Form's request count from a `-v` run of its own and keeps the logs until the branch description quotes them.
- Every approved B1 string reaches source as an `⟦id⟧` marker filled by `copyedit`, not only the B2 labels.

**Spec ambiguities this plan resolved.** These are listed above under "Where the code forced a choice", plus the following:

- The bar's language-menu leg runs on `day/en` and `day/ar`, not all 36 theme × locale pages. The fit of the bar's contents runs on all 36. A menu that stays on screen depends on direction and width, not on palette, and 36 × 4 widths × anchor on and off × scripts on and off would cost about 19 minutes of a 20-minute package.
- The Code-selected axe leg is its own function (`TestA11yScansTheCodePanels`) beside `TestA11yScansTheGallery`, not a branch inside it. It keeps its own timeout, and its five-element precondition fails on its own.
- The phone index's filter sub-leg ("typing checkbox…") lands in Task 13 with the synonyms, because before them "checkbox" matches nothing.
- The five error-status state labels ("404 — not found" and so on) carry em dashes into frame titles just like the five labels batch B2 covers. They are reviewed as batch B3, and Task 9 reads them from `copy-review/batch-b3-result.json` as `state.status_404` to `state.status_503`.
- `docs/site/templates.md` describes the old frame. Rewriting it is user-facing copy outside this batch, so Task 19 lists it as owed in the branch description rather than writing unreviewed prose.
