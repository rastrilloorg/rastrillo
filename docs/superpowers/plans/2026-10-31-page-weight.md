# A page ships only what it uses (#158): implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A scaffolded app's plain page loads under 40 kB of framework
CSS and JS, gzipped; a page with a select or a date field adds only
that component's script; and the gate keeps it there.

**Architecture:** `rastrillo.Assets` serves each vendored file under
`static/` minified (tdewolff/minify) and precompressed, fingerprinted
by the bytes it sends; the app's own files are served as written.
`ui.Pages` reads each page's parse tree once at boot, finds the
component scripts its partials name with `{{needs "select.js"}}`, and
binds that page's list to `neededScripts`, which the layouts range
over in `<head>`. A page with a live region or a polling fragment gets
every component script. `perftest.FirstLoad` sums the framework files
a page links, as served, gzipped at the default level, and fails a
page over its shell's limit, naming the file that grew; `Screens` runs
it on every app's GET routes, and the framework's gate runs it on each
shipped layout.

**Tech Stack:** Go (go directive stays 1.25.0), `html/template` and
`text/template/parse`, `github.com/tdewolff/minify/v2`,
`compress/gzip`, chi, chromedp for the browser rigs.

**Spec:** `docs/superpowers/specs/2026-10-31-page-weight-design.md`
(changes 1 to 4, "How we know"). Settled there and not reopened: the
minifier; only `ui.VendoredNames` files are minified; live and polling
pages load every component script; the budget counts framework CSS+JS
as served, gzip default level, deferred scripts in, HTML out; 40 kB
for a plain column or topbar page plus each loaded component script's
measured size; sidebar, console and stage at their measured size after
changes 1 and 2, plus 10%.

## Global Constraints

### Decisions this plan makes (the spec left them open)

- **What counts as vendored when served:** an FS path of exactly
  `static/<name>` with `<name>` in the vendored set. That is where the
  scaffold puts them and how `Assets` is documented to be mounted. An
  app's `static/admin/select.js`, and a top-level `tokens.css` (the
  gallery's own hashing tree in `internal/designsystem/hashed.go`), are
  served as written.
- **Minified lazily**, on a file's first `Path` or request, cached
  beside its hash and rebuilt when its (mtime, size) changes, as the
  hash is today. Nothing new runs at `NewAssets`, which apps call in a
  package variable.
- **The minifier's settings:** CSS `css.Minify`; JS
  `&js.Minifier{KeepVarNames: true}`, so the served file differs from
  the source by comments and whitespace as nearly as the minifier
  allows, and a stack trace still names the source's variables.
- **gzip only, no brotli.** The minifier ships no brotli encoder and the
  standard library has none, so brotli would be a second dependency.
  Precompressed once at `gzip.BestCompression`; the budget measures at
  `gzip.DefaultCompression` regardless.
- **A file the minifier refuses** is served as written (still gzipped),
  never as a 500 or an empty body.
- **How the layout learns a page's scripts:** statically, at
  `NewPages`, from the page's parse tree, not from a per-render set.
  The scripts sit in `<head>`, which executes before `content`, so a
  set filled by partials during the render would be empty when the
  head is written. Go templates name every `{{template}}` with a
  constant, so the walk sees every partial a page can reach; a partial
  inside a branch that does not run still counts, which errs towards
  loading.
- **datetime.js brings calendar.js**: the date partials name
  `datetime.js` only, and the list adds `calendar.js` before it, as the
  layouts order them today.
- **Live or polling page:** the page's set defines `region:<name>` for
  a name other than `live.Notice`, or a template reachable from the
  entry has text containing `data-poll`.
- **Outside `ui.Pages`** (the gallery's shell demos, any app executing
  `ui.Funcs()` directly), `neededScripts` lists all three component
  scripts, so those renders are unchanged.
- **stage.html is left alone.** It loads no component script today and
  its comment says why.
- **The budget's unit:** 40 kB is 40,000 bytes.
- **The shell** is read off the page's markup: `rst-shell-console`,
  then `rst-shell-sidebar`, then `rst-shell-topbar`, then
  `<body rst-stage`; anything else is column.
- **"Names the file that grew":** `perftest` pins a reference size for
  every vendored file, measured after Tasks 1 to 5 and rounded up to
  the next 100 bytes so a compressor change of a few bytes is not a
  failure. A component script's reference is also what it adds to a
  page's limit. theme.css's reference is the largest theme's.
- **No budgets.txt record** for first-load weight: only framework files
  are counted, so a page goes over only when a vendored file grows or
  a page loads framework files its shell does not, and the message
  says which.
- **The browser proof:** `galleryrig.Tree`, which every gallery and
  sweep browser test serves the tree through, serves the framework's
  files minified, so the whole gallery drive runs against them. The
  deployed gallery tree is unchanged: its Getting started page links
  each file and gives its size as written, and must keep agreeing with
  it. ui's own browser tests keep serving the source files.
- **The examples link no component script** (checked: blog and tickets
  link CSS only, notes links rastrillo.js, busy.js and live.js), so
  change 2 edits no example layout. Their go.sum files do change in
  Task 1.

### For every task

- Code reads like a sentence (`skill/code.md`): spelled-out names
  (receivers too: `assets`, `pages`), why-only comments in one or two
  sentences, 80 columns, 40-line functions, 400-line files. `make code`
  passes with no new records; functions a task rewrites have their
  records lowered (`go run ./cmd/rastrillo budget code --lower`).
  Recorded ceilings this plan works against: `ui/funcs.go` file 827,
  `ui/pages.go` NewPages 82, `perf/perftest/screens.go` screens 64,
  `internal/designsystem/galleryrig/rig.go` file 514. A task that would
  grow one extracts instead, as its steps say.
- `make budget`: ui's test-line record (40,060) rises by exactly the
  lines Tasks 4 and 5 add, with a reason; `cmd/rastrillo` (6,164)
  does not move: its edits are line-neutral.
- No new SUITE role: every new test lives in a package already in
  `root` or in `browser.internal.designsystem`. If one is added anyway,
  `.amadan/ci.d/50-suite@35` becomes `@36` in the same commit.
- Test evidence per CONTRIBUTING §3: each new test red in a detached
  worktree at the task's base, with compiling stubs where needed, then
  green; outputs under `~/.cache/page-weight/evidence/task<N>-red.txt`
  and `-green.txt`.
- Hand `go` commands at the root with `GOFLAGS=-mod=mod`; targeted
  `go test -run` only (CI runs the suites). Git unsandboxed. Commit with
  `amadan commit`, body ending
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- No change in behaviour: every existing test, browser tests included,
  passes unchanged against the served files. A browser test that fails
  on behaviour under minified files is a minifier problem: stop and
  report, do not adjust the test.
- User-facing text (NewPages' new errors, perftest's failure line, the
  docs, the CHANGELOG, the scaffold's comments) is drafted in Tasks 4
  to 7 and copy-reviewed with Paul in Task 7. SKILL.md and `skill/` are
  for agents and are not.
- Before landing: CONTRIBUTING §2's review by another provider's model,
  and green CI on the fleet.

## Review Focus

- An app's edited `static/select.js` that the minifier cannot parse:
  served as written with 200, never a 500 or an empty body (Task 2).
- An app serving `static/` from a live directory edits `tokens.css`:
  the next `Path` and the next request carry the new minified bytes
  and their hash, not the first minified copy (Task 2).
- A client refusing gzip (`Accept-Encoding: gzip;q=0`, or none): the
  minified bytes, uncompressed; each encoding has its own ETag and
  `Vary: Accept-Encoding`, so no cache hands gzip to a client that
  refused it (Task 2).
- An existing app that has not made the layout edit (three literal
  `<script>` tags): `NewPages` accepts it and every page still loads
  all three scripts (Task 5).
- A select arriving in a live region or a polled fragment on a page
  that otherwise has none: the page already loaded select.js (Task 4).

---

## File Structure

```
internal/vendored/      names.go (the list, moved from ui; Is;
                        ComponentScripts), minify.go, *_test.go
assets.go               lookup replaces hashFor; serveFile's vendored branch
assets_vendored.go      servedForms, acceptsGzip; assets_vendored_test.go
ui/vendored.go          VendoredNames reads internal/vendored
ui/scripts.go           needs, allComponentScripts, pageScripts,
                        scriptFuncs, the NewPages messages; scripts_test.go
ui/pages.go             addPage extracted from NewPages; reserved names
ui/funcs.go             scriptFuncs merged into Funcs
ui/partials/field-{select,date,datetime,time}.html  {{needs ...}}
ui/layouts/{column,topbar,sidebar,console}.html     the range line
ui/layouts_scripts_test.go
internal/designsystem/galleryrig/tree.go   Tree, TreeRecording moved,
                                           minifiedFramework
internal/designsystem/minified_browser_test.go
perf/perftest/firstload.go, firstload_test.go,
              firstload_framework_test.go; screens.go (startServer)
cmd/rastrillo/new.go, new_budget.go (comments), new_test.go
go.mod, go.sum, examples/*/go.sum, internal/*/testdata/*/go.sum
SKILL.md, skill/screens.md, skillmd_test.go (only if the ceiling moves)
docs/site/{assets,templates,testing}.md, docs/site/reference/perftest.md
CHANGELOG.md, .rastrillo/budgets.txt, .rastrillo/code.txt
```

---

### Task 1: `internal/vendored`, the list and the minifier

**Files:** Create `internal/vendored/names.go`, `minify.go`,
`vendored_test.go` (package `vendored_test`, so it can import `ui`);
modify `ui/vendored.go` (the list and its order comment move;
`VendoredNames` returns `vendored.Names()`), `go.mod`, `go.sum`, and
the go.sum of every module that links the root package.

**Interfaces:**
- Produces: `func Names() []string` (a copy, today's order);
  `func ComponentScripts() []string` (`select.js`, `calendar.js`,
  `datetime.js`, a copy); `func Is(path string) bool` (`static/<name>`
  only); `func Minify(name string, body []byte) ([]byte, error)` (by
  `path.Ext(name)`: `.css`, `.js`; anything else comes back unchanged).

- [ ] **Step 1: Add the dependency.** `go get
  github.com/tdewolff/minify/v2@latest` (unsandboxed if the proxy is
  refused); the go directive must stay `1.25.0`: if latest raises it,
  take the newest version that does not. Record the version in the
  commit body.

- [ ] **Step 2: Write the failing tests**

```go
func TestMinifyDropsCommentsAndWhitespace(t *testing.T)   // css "/* why */\nbody {\n  color: red;\n}\n" and a js function with // and /* */ comments: no comment text left, shorter
func TestMinifyKeepsRegexAndTemplateLiterals(t *testing.T) // js: /\/\*kept\*\//g, `a /* b */ ${x} // c`, "http://x": each survives byte for byte
func TestMinifyKeepsLocalNames(t *testing.T)               // function f(){ var longName = 1; return longName } keeps "longName"
func TestMinifyLeavesOtherFilesAlone(t *testing.T)         // "LICENSE", "x.svg": body returned unchanged, no error
func TestMinifyRefusesBrokenJS(t *testing.T)               // "function (": an error
func TestIsMatchesOnlyTheScaffoldsPlace(t *testing.T)      // static/tokens.css, static/theme.css true; tokens.css, static/admin/select.js, static/app.css false
func TestEveryShippedFileMinifies(t *testing.T)            // every ui.VendoredAssets(theme) file, every theme: no error, smaller, non-empty
func TestComponentScriptsAreVendored(t *testing.T)         // each ComponentScripts() name is in Names()
```

- [ ] **Step 3: Red run** (`task1-red.txt`, with stubs).
- [ ] **Step 4: Implement**; move the list, keeping
  `TestVendoredNamesMatchVendoredAssets` in ui green.
- [ ] **Step 5: go.sum everywhere it is needed.** For each module
  under the repo with a go.sum whose `go list -deps ./...` includes
  `amadan.net/rastrillo/rastrillo` (the four examples,
  `internal/routelist/testdata/{app,none,troubled,two}`,
  `internal/tmplmigrate/testdata/gocheckmod`, and any
  `internal/codelimits/testdata` module that has a go.sum), run
  `go mod tidy`. The diff may add `github.com/tdewolff/*` lines and,
  in the testdata modules, catch `github.com/go-json-experiment/json`
  up to the root's version (they already lag it: `-mod=mod` moves it
  today with no change of ours). Anything else moving is reverted and
  reported.
- [ ] **Step 6: Green run**: `go test ./internal/vendored/ ./ui/ -run
  'Minify|Is|Shipped|ComponentScripts|VendoredNames'`;
  `go test ./internal/routelist/ ./internal/tmplmigrate/`;
  `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./internal/vendored/`;
  `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./internal/vendored/`;
  `make code`.
- [ ] **Step 7: Commit** "vendored: one list of the framework's files, and a minifier for them".

---

### Task 2: `Assets` serves the vendored files minified and gzipped

**Files:** Modify `assets.go`; create `assets_vendored.go`,
`assets_vendored_test.go`. Existing `assets_test.go` passes unchanged.

**Interfaces:**
- Consumes: `vendored.Is`, `vendored.Minify`.
- Produces (unexported): `assetInfo` gains `served, gzipped []byte`
  (nil `served` means serve the file as read);
  `func (assets *Assets) lookup(name string) (assetInfo, error)`
  replaces `hashFor`, and the hash is of `served` when set;
  `func servedForms(name string, body []byte) (served, gzipped []byte)`
  (a minify error gives `body` itself); `func acceptsGzip(request
  *http.Request) bool` (a `gzip` token with no `q=0`, case-insensitive,
  `*` counts). `Path` and `Handler` keep their signatures.

Response for a vendored file: the identity bytes with ETag
`"<hash>"`, or, when `acceptsGzip`, the gzipped bytes with
`Content-Encoding: gzip` and ETag `"<hash>-gzip"`; both carry
`Vary: Accept-Encoding`. Cache-Control and the stale-hash rules are
unchanged.

- [ ] **Step 1: Write the failing tests**

```go
func TestAVendoredFileIsServedMinified(t *testing.T)            // static/tokens.css with a comment: the body has no comment and is shorter; static/rastrillo.js likewise
func TestPathFingerprintsTheBytesSent(t *testing.T)             // Path's 16 hex == sha256 of the served identity body
func TestAnAppsOwnFileIsServedAsWritten(t *testing.T)           // static/app.css, static/admin/select.js, tokens.css at the root: byte for byte
func TestAVendoredFileIsGzippedForAClientThatAsks(t *testing.T) // "gzip, deflate": Content-Encoding gzip, Vary, gunzip == identity body, ETag "<hash>-gzip"
func TestAClientRefusingGzipGetsTheMinifiedBytes(t *testing.T)  // "gzip;q=0", "identity", none: no Content-Encoding, Vary set, ETag "<hash>"
func TestEachEncodingRevalidatesOnItsOwnETag(t *testing.T)      // each ETag with its own Accept-Encoding: 304; the gzip ETag on an identity request: 200
func TestAFileTheMinifierRefusesIsServedAsWritten(t *testing.T) // static/select.js "function (": 200, the bytes as written
func TestALiveDirectoryEditIsMinifiedAgain(t *testing.T)        // os.DirFS, as TestPathSeesFileEdits: new hash, new minified body
```

- [ ] **Step 2: Red run** (`task2-red.txt`).
- [ ] **Step 3: Implement**; `Handler` and `serveFile` stay within 40
  lines each, rewritten functions spell out their names, records
  lowered.
- [ ] **Step 4: Green run**: `go test . -run 'Path|Handler|Vendored|Gzip|ETag|Minifier|Edit'`;
  the blog example, which serves through `Assets`:
  `cd examples/blog && GOFLAGS=-mod=mod go test ./internal/blogtest/ -run 'TestEmbeddedStaticServesTokensCSS|TestScreensLinkFingerprintedStylesheets'`;
  `make code`.
- [ ] **Step 5: Commit** "assets: serve the framework's files minified and gzipped".

---

### Task 3: The gallery's browser drive runs on the minified files

**Files:** Create `internal/designsystem/galleryrig/tree.go` (`Tree`
and `TreeRecording` move out of `rig.go`, taking its 514 record down,
plus `minifiedFramework`); modify `galleryrig/tree_test.go`; create
`internal/designsystem/minified_browser_test.go`.

**Interfaces:**
- Consumes: `vendored.Names`, `vendored.Minify`.
- Produces: `func minifiedFramework(files map[string][]byte)
  map[string][]byte`: a copy in which every file whose name, with its
  16-hex fingerprint removed, is a vendored name or `theme-*.css` is
  minified (a refusal keeps the bytes); `gallery.css`, `gallery.js`,
  pages and pictures are untouched. `TreeRecording` serves its result,
  computed once per call. `Tree`'s signature is unchanged.

- [ ] **Step 1: Write the failing tests**

```go
// galleryrig/tree_test.go (browser tag, as the file is)
func TestTheTreeServesTheFrameworksFilesMinified(t *testing.T) // tokens.0123456789abcdef.css and theme-day.<hex>.css with comments: served without; gallery.<hex>.css: as written

// minified_browser_test.go
func TestComponentsEnhanceFromTheMinifiedFiles(t *testing.T)
// treeHandler(t); the served tokens.css (assetHref) has no "/*";
// a preview document carrying data-rst-select with ten or more options
// (found with componentPages and frameDocs): [rst-combo] appears;
// one carrying data-rst-date: [rst-dtp-pick] appears and
// typeof window.rastrilloCalendar === "function"; no console error
```

- [ ] **Step 2: Red run** (`task3-red.txt`):
  `go test -tags browser ./internal/designsystem/galleryrig/ ./internal/designsystem/ -run 'Minified'`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Green run**, the same command. The whole gallery and
  sweep drive on the minified tree is CI's (the
  `browser.internal.designsystem*` roles); push after this task and
  read those roles before starting Task 6. If a test there fails
  only because it reads a framework file's commented text, give that
  test an unminified tree through a new `TreeAsWritten(t, files,
  mountPrefix)` in `tree.go`, with a comment saying why; a failure on
  behaviour stops the work (Global Constraints).
- [ ] **Step 5: Commit** "galleryrig: drive the gallery on the framework's files as served".

---

### Task 4: `needs`, and each page's scripts worked out at boot

**Files:** Create `ui/scripts.go`, `ui/scripts_test.go`; modify
`ui/pages.go`, `ui/funcs.go`, `ui/funcs_test.go` (its two name lists),
`ui/partials/field-select.html` (`{{needs "select.js"}}`),
`field-date.html`, `field-datetime.html`, `field-time.html`
(`{{needs "datetime.js"}}`), each inside its `define`. Partial goldens
stay unchanged: `needs` renders nothing.

**Interfaces:**
- Consumes: `vendored.ComponentScripts`, `live.Notice`.
- Produces: `func needs(name string) (string, error)`: `""` for a
  component script, else an error listing them;
  `func allComponentScripts() []string`: `static/select.js`,
  `static/calendar.js`, `static/datetime.js`;
  `func scriptFuncs() template.FuncMap`: `needs` and `neededScripts`
  (bound to `allComponentScripts`); `func pageScripts(page
  *template.Template, entry string) ([]string, error)`: the
  `static/<name>` paths a page needs, in `ComponentScripts` order, each
  once; `func (pages *Pages) addPage(base *template.Template, name
  string, file PageFile, app fs.FS) error`: NewPages' per-page loop
  body (clone, parse, `pageScripts`, then `Funcs` on the clone binding
  `neededScripts` to that page's list), which takes NewPages' 82
  record down. Template authors see `{{needs "select.js"}}` and
  `{{range neededScripts}}`.

`pageScripts` walks from `page.Lookup(entry)` through every node
(action, if, range, with and their else lists, template calls into
`page.Lookup(name)`, each template once). A `needs` command whose
argument is not a string literal, or names no component script, is an
error naming the page. Live or polling (Global Constraints) gives all
three. `datetime.js` adds `calendar.js`.

`Funcs` gains the two names without growing funcs.go past 827 lines:
its locale loop becomes `maps.Copy(m, localeFuncs(c))` and
`maps.Copy(m, scriptFuncs())` follows it. NewPages refuses an app
function named `needs` or `neededScripts` with `reservedFuncMsg`; the
check moves into one `reservedFunc(name string) bool` beside it.
pages.go stays under 400 lines: the new messages live in scripts.go:

```go
needsLiteralMsg = "ui: page %q: needs takes a script name in quotes, such as {{needs \"select.js\"}}"
needsUnknownMsg = "ui: page %q: needs %q, which is not a component script: %s"
```

- [ ] **Step 1: Write the failing tests** (pages built from small
  `fstest.MapFS` layouts whose head is
  `{{range neededScripts}}<script src="{{.}}"></script>{{end}}`; each
  asserts the rendered `src` list)

```go
func TestAPlainPageNeedsNoComponentScript(t *testing.T)
func TestASelectFieldLoadsSelectJS(t *testing.T)                 // ["static/select.js"]
func TestADateFieldBringsTheCalendar(t *testing.T)               // field-date, field-datetime, field-time: [calendar, datetime]
func TestScriptsAreListedOnceInLayoutOrder(t *testing.T)         // two selects and a date: [select, calendar, datetime]
func TestAPartialReachedThroughAnotherCounts(t *testing.T)       // api-tokens: [select]; field-daterange: [calendar, datetime]
func TestALivePageLoadsEveryComponentScript(t *testing.T)        // defines region:items: all three; calls job-status: all three; defines only region:notice: none
func TestEachPageKeepsItsOwnScripts(t *testing.T)                // two pages of one Pages, rendered alternately from 8 goroutines (-race): each its own list
func TestNeedsInAPagesOwnMarkup(t *testing.T)                    // content with {{needs "select.js"}}: [select]
func TestNeedsRefusesAnUnknownScript(t *testing.T)               // {{needs "selct.js"}}: NewPages error naming the page and "selct.js"
func TestNeedsTakesOnlyAQuotedName(t *testing.T)                 // {{needs .Script}}: NewPages error naming the page
func TestAnAppCannotReplaceTheScriptFuncs(t *testing.T)          // Funcs {"needs": ...} and {"neededScripts": ...}: reservedFuncMsg
func TestFuncsOutsidePagesListEveryComponentScript(t *testing.T) // ui.Funcs()["neededScripts"] gives all three; {{needs "select.js"}} renders ""
```

- [ ] **Step 2: Red run** (`task4-red.txt`, with stubs):
  `go test -race ./ui/ -run 'Needs|ComponentScript|SelectField|DateField|LayoutOrder|ReachedThrough|LivePage|OwnScripts|ScriptFuncs'`.
- [ ] **Step 3: Implement**, then the partials' `needs` lines.
- [ ] **Step 4: Green run**: the same command; the partial goldens
  and Pages tests (`go test ./ui/ -run 'Golden|Pages'`); `make code`;
  `make budget` (raise ui's test record by the lines added, with a
  reason).
- [ ] **Step 5: Commit** "ui: a page loads the component scripts its fields name".

---

### Task 5: The layouts load only what the page needs

**Files:** Modify `ui/layouts/column.html`, `topbar.html`,
`sidebar.html`, `console.html`: the three component `<script>` lines
become one, in the same place after `{{iconAssets}}`, with a one-line
why comment above it:

```html
{{range neededScripts}}<script defer src="{{path $ (asset .)}}"></script>{{end}}
```

Create `ui/layouts_scripts_test.go`. Modify `cmd/rastrillo/new_test.go`
(`TestNewScaffoldsSelectJS` asserts the layout carries that line, not
the literal `static/select.js`) and new.go's comment on the static
files (lines 254 to 266), which says every shell links them: the
layouts load them where a page's fields need them, line-neutral.

**Interfaces:**
- Consumes: Task 4's `neededScripts` and `needs`.

- [ ] **Step 1: Write the failing tests**

```go
func TestEachShellLoadsOnlyTheScriptsItsFieldsNeed(t *testing.T)
// for column, topbar, sidebar, console (ui.Layout): pages plain,
// select (field-select, 12 options), date (field-date), live
// (region:items): the srcs, in order, are none / select / calendar,
// datetime / all three; rastrillo.js, busy.js, live.js, light.js,
// anchor.js on every page, and shell.js where the shell had it
func TestAPagesHooksAlwaysHaveTheirScript(t *testing.T)
// every page above: data-rst-select in the HTML implies select.js
// loaded; data-rst-date, data-rst-time, data-rst-range imply
// calendar.js and datetime.js
func TestStageStillLoadsNoComponentScript(t *testing.T)
func TestAnUneditedLayoutStillLoadsEveryComponentScript(t *testing.T)
// ui/testdata/pages/scaffold/layout.html (the literal tags) with a
// plain page: NewPages accepts it; all three load
```

- [ ] **Step 2: Red run** (`task5-red.txt`):
  `go test ./ui/ -run 'Shell.*Scripts|Hooks|Stage.*Component|Unedited'`.
- [ ] **Step 3: Edit the four layouts and the scaffold test.**
- [ ] **Step 4: Green run**: the same; `go test ./cmd/rastrillo/ -run
  'TestNewScaffoldsSelectJS|TestLayoutTemplate'`;
  `go test ./internal/designsystem/ -run 'Shell|Script'` (the shell
  demos render through `ui.Funcs()`, so they still load all three);
  each example's tests (`cd examples/<name> && GOFLAGS=-mod=mod go
  test ./...`, four runs) since they parse ui's partials; `make code`;
  `make budget`.
- [ ] **Step 5: Commit** "layouts: load a component's script only on a page that uses it".

---

### Task 6: The first-load budget

**Files:** Create `perf/perftest/firstload.go`, `firstload_test.go`
(package `perftest`, the `fakeT` and `run` style of `screens_test.go`),
`firstload_framework_test.go` (package `perftest_test`); modify
`perf/perftest/screens.go` (extract `startServer`; `screens` calls
`checkFirstLoad` after the timing loop), `file.go`'s package comment,
`cmd/rastrillo/new_budget.go` (perf_test.go's and the Makefile's perf
lane comments say the lane also holds first-load weight, everywhere
rather than only on CI; line-neutral), `docs/site/reference/perftest.md`
(draft).

**Interfaces:**
- Consumes: `vendored.Names`, `vendored.ComponentScripts`, `ui.Layout`,
  `ui.LayoutNames`, `ui.VendoredAssets`, `ui.ThemeNames`,
  `rastrillo.NewAssets`, `ui.NewPages`.
- Produces: `func FirstLoad(t testing.TB, cfg ScreenConfig)`: the same
  inventory as `Screens` (budgets.txt skips honoured), not skipped
  under `-short`, callable more than once;
  `type firstLoadBudget struct { shells, references map[string]int }`;
  `var frameworkBudget firstLoadBudget`;
  `func checkFirstLoad(t testing.TB, client *http.Client, base string,
  targets []target, budget firstLoadBudget)`;
  `func linkedFiles(page []byte) []string` (each `<link
  rel="stylesheet">` href and `<script src>`, document order, once
  each); `func frameworkName(url string) (string, bool)`;
  `func shellOf(page []byte) string`; `func gzippedSize(body []byte)
  int` (`gzip.DefaultCompression`); `func startServer(t testing.TB,
  cfg ScreenConfig) (*http.Client, *httptest.Server, *atomic.Int32)`
  (screens' server, jar and sign-in, lowering its 64 record).

A page is checked when it answers 200 with `text/html`. Its limit is
`shells[shellOf(page)]` plus `references[name]` for each component
script it links. Each linked framework file is fetched through the
same client and must answer 200. Weight failures are `t.Errorf` on
every machine, not only on CI. Lines (copy-reviewed in Task 7):

```
GET /orders: 31240 bytes of framework CSS and JS, gzipped; budget 45100
GET /orders: 47310 bytes of framework CSS and JS, gzipped, over its 45100-byte budget (40000 for a column page, plus select.js); tokens.css grew from 20100 to 26170 bytes
GET /orders: ... over its 40000-byte budget (40000 for a column page); no file grew: it loads tokens.css, theme.css, shell.css, rastrillo.js, ...
GET /orders links /static/select.0123456789abcdef.js, which answers 404
```

The first is `say`'d; the others fail.

- [ ] **Step 1: Write the failing tests**

```go
// firstload_test.go, with a small budget table of its own
func TestFirstLoadCountsOnlyTheFrameworksFiles(t *testing.T)     // links tokens.<hex>.css, /static/app.css, /pow/pow.js: only tokens counted
func TestAPageOverItsBudgetNamesTheFileThatGrew(t *testing.T)    // tokens.css over its reference: the "grew from" line
func TestAPageOverItsBudgetWithNothingGrownListsItsFiles(t *testing.T)
func TestAComponentScriptRaisesThePagesBudget(t *testing.T)      // passes with select.js linked where the shell's limit alone would fail
func TestTheShellComesFromThePage(t *testing.T)                  // shellOf: each marker, and column by default
func TestFirstLoadSkipsWhatIsNotAPage(t *testing.T)              // a 303, a JSON 200: not checked, no error
func TestAFrameworkFileThatDoesNotLoadFailsByName(t *testing.T)  // a linked select.js answering 404: the 404 line, never 0 bytes
func TestFrameworkNameStripsTheFingerprint(t *testing.T)         // /static/tokens.<hex>.css, /acme/static/rastrillo.<hex>.js?v=1, /static/app.css (false), /pow/pow.js (false)
func TestLinkedFilesReadsStylesheetsAndScripts(t *testing.T)     // rel after href; rel="preload" ignored; inline <script> ignored; a repeat once
func TestTheBudgetCoversEveryFileAndShell(t *testing.T)          // frameworkBudget: a reference per vendored.Names(), a limit per ui.LayoutNames()

// firstload_framework_test.go
func TestEveryShellsPagesMeetTheFirstLoadBudget(t *testing.T)
// per shell in ui.LayoutNames() and theme in ui.ThemeNames(): a
// ui.Pages over ui.Layout(shell) with pages plain (<h1> and a date as
// text), select, date and live as in Task 5; static from
// rastrillo.NewAssets over ui.VendoredAssets(theme) under static/;
// a chi router GET /plain, /select, /date, /live; FirstLoad on it
```

- [ ] **Step 2: Red run** (`task6-red.txt`, with stubs):
  `go test ./perf/perftest/ -run 'FirstLoad|Budget|Shell|Framework|LinkedFiles'`.
- [ ] **Step 3: Implement**, with `frameworkBudget` holding 40,000 for
  column and topbar and provisional values elsewhere.
- [ ] **Step 4: Measure and pin.** `go test ./perf/perftest/ -run
  TestEveryShellsPagesMeetTheFirstLoadBudget -v` and read each page's
  per-file sizes. Set each reference to its file's size rounded up to
  the next 100 bytes (theme.css: the largest theme's), and sidebar,
  console and stage to their plain page's total times 1.1, rounded up
  to the next 100. If a column or topbar plain page is over 40,000,
  stop and report BLOCKED with the numbers: the spec's goal is missed,
  and 40,000 does not move. Put the measured numbers in the commit
  body.
- [ ] **Step 5: Green run**: the Step 2 command; the scaffold's perf
  lane end to end, which now runs `checkFirstLoad` on a real app:
  `go test ./cmd/rastrillo/ -run
  'TestScaffoldServesItsLocales|TestNewScaffoldsTestHarness'`;
  `make code`.
- [ ] **Step 6: Commit** "perftest: hold each page's framework CSS and JS to a first-load budget".

---

### Task 7: SKILL.md, the docs, the CHANGELOG, and the copy review

**Files:** `SKILL.md`, `skill/screens.md`, `skillmd_test.go` (only if
the ceiling moves), `docs/site/assets.md`, `docs/site/templates.md`,
`docs/site/testing.md`, `docs/site/reference/perftest.md` (Task 6's
draft), `CHANGELOG.md`.

- [ ] **Step 1: SKILL.md.** One bullet in "Never", in this sense:
  "Never add a `<script>` tag for a framework component: the `field-*`
  partials load their own; hand-written markup asks with
  `{{needs "select.js"}}`." SKILL.md is 9,084 of 9,100 bytes: trim
  genuinely redundant prose in the same file to fit; if that would
  cost a load-bearing fact, raise `skillCoreBudget` by what the bullet
  needs, with a dated reason in `skillmd_test.go`, per AGENTS.md.
- [ ] **Step 2: skill/screens.md.** "Tests and budgets" gains that
  `make perf` also holds each page's framework CSS and JS to 40 kB
  gzipped plus the component scripts it loads, and that a failure
  names the file that grew (within 9,000 bytes).
- [ ] **Step 3: Draft the docs.** assets.md: a section on the vendored
  files served minified and gzipped (no edit, the copy in `static/`
  keeps its comments, your own files as written, a file the minifier
  cannot read as written). templates.md: `needs` and `neededScripts`
  in the functions list, and a short section on component scripts (the
  partials load their own, live pages load all three, `needs` for
  hand-written markup, the layout line). testing.md: a first-load row
  in the budget table and that it fails everywhere, not only on CI.
- [ ] **Step 4: Draft the CHANGELOG** under Unreleased, upgrade steps
  first: pages are lighter with no edit (served minified); to load
  component scripts only where used, replace the three
  `select.js`/`calendar.js`/`datetime.js` lines in
  `templates/layout.html` with the range line; `make perf` now checks
  first-load weight, and a failure names the file, which
  `rastrillo doctor --fix` re-copies. What changed: `needs`,
  `neededScripts`, `perftest.FirstLoad`, the new dependency
  `github.com/tdewolff/minify/v2` at its version.
- [ ] **Step 5: Copy review.** The controller runs a copy review with
  Paul over every user-facing string Tasks 4 to 7 drafted: NewPages'
  two messages, perftest's lines, the scaffold's comments, the four
  docs pages and the CHANGELOG. Apply the approved text verbatim.
- [ ] **Step 6: Green run**: `go test -run Skill .`;
  `go test ./internal/docsite/`; `go test ./ui/ -run Needs` and
  `go test ./perf/perftest/ -run FirstLoad` (messages changed);
  `make code`; `make budget`.
- [ ] **Step 7: Commit** "Say what page weight changes: skill, docs and changelog".

---

### Task 8: The benchmark round

- [ ] Hand back to the operator's session for the groomer-only round.

---

## Self-review

- **Spec coverage.** Change 1: Tasks 1 to 3 (minified copy,
  fingerprinted by bytes sent, app files as written, gzip, the browser
  test on the gallery). Change 2: Tasks 4 and 5 (partials name scripts,
  the layout emits each once, always-on scripts kept, live pages load
  all, `needs` for app markup, layouts app-owned so the CHANGELOG
  carries the edit, doctor and the vendored test unchanged). Change 3:
  Task 6 (in-process, framework CSS+JS only, as served, default gzip,
  deferred included, HTML out; 40,000 plus component scripts; other
  shells measured plus 10%; the scaffold inherits it through
  `Screens`; the framework gate per layout; names the file). Change 4:
  Task 7. "How we know": Task 8.
- **Out of scope, noticed:** the gallery's Getting started page gives
  each file's size as written and gzipped; with apps now serving the
  minified copy, its "what an app downloads" total overstates. Not in
  the spec; left for the operator.
- **Types.** `vendored.Names/ComponentScripts/Is/Minify`, `lookup`,
  `servedForms`, `acceptsGzip`, `needs`, `allComponentScripts`,
  `pageScripts`, `scriptFuncs`, `addPage`, `minifiedFramework`,
  `FirstLoad`, `checkFirstLoad`, `firstLoadBudget`, `frameworkBudget`,
  `startServer` are each defined once and used under the same name.
