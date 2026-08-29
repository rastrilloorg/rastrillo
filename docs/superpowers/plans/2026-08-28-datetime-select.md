# Date/Time Fields, Select Optgroups and Error-Helper Adoption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Progressively-enhanced, fully localisable date/time fields (a port of Tito Go's smart-datetime with its English vocabulary replaced by catalog keys and Intl), `form.Date/Time/DateTime` + `form.Range`, optgroup support in the searchable select, localisable form-error messages, and the deferred adoption of `view.NotFound`/`Forbidden` in generated actions.

**Architecture:** The native inputs are the value carriers; `ui/datetime.js` (new, ~30KB, dependency-free, inert without `data-rst-date`) builds a WAI-ARIA combobox over them. Parser vocabulary rides on data attributes from `rastrillo.ui.date_*` keys; weekday/month names and digit folding come from `Intl` for the page's `lang`. A Node harness runs the parser from `go test` with a fixture table per shipped locale. `form` gains three kinds whose error values are catalog KEYS (safe passthrough: `T` returns unknown strings verbatim, so plain-English app messages still render).

**Tech Stack:** Go stdlib, html/template, vanilla ES (no modules — match select.js's IIFE style), Node ≥18 for the test harness (v24 present; tests skip loudly without it).

**Spec:** `docs/superpowers/specs/2026-08-28-design-system-design.md` §4 (as amended), §4.5, §4b.2's deferred adoption. This is PR 3 of §6.

## Global Constraints

- Reference implementation to port: `/home/paulca/github.com/tito/titogo/internal/instance/static/smart-datetime.js` (47KB) and its harness `/home/paulca/github.com/tito/titogo/internal/instance/smart_datetime_node.mjs`. READ THEM before porting; keep Tito Go's behavioural decisions (unparsed text arms nothing; native input never leaves the DOM; `showPicker()` button) unless a ruling below changes one. Do not copy Tito Go's English word lists — that is the point of the port.
- **No English in datetime.js.** Vocabulary from `data-rst-date-*` attributes; calendar names/digits from `Intl.DateTimeFormat`/`NumberFormat` via `formatToParts`; locale from `document.documentElement.lang` (per-field override `data-rst-date-lang`).
- Locale codes, verbatim: `en ga zh-Hans es hi pt bn ru ja yue vi ar`; every base catalog holds exactly the en key set (gated).
- Wire formats unchanged by enhancement: `2006-01-02`, `15:04`, `2006-01-02T15:04` — POSTs byte-identical with scripts off.
- Every new stylesheet rule: logical properties, `var(--rst-*)` colours only (gates live). tokens.css changes copy to examples/blog+tickets static (pins).
- `go test . ./ui/ ./view/ ./form/ ./internal/... ./cmd/...` and the three example suites green at every commit; `GOFLAGS=-mod=mod`; `GOCACHE=$TMPDIR/gocache` if the cache is read-only. Never bare `git stash`. `SKILL.md` ≤ 18,000 (15,842 now).
- New exported symbols get reference-page entries in the same task that exports them (the docsite gate is branch-red otherwise; PR 2's pattern).
- Error pages/dates never leak internals; the date parser never guesses (Enter on unparsed text refuses).

---

## File map

| File | Responsibility |
|---|---|
| `form/fields.go`, `form/datetime.go` (new), tests | kinds Date/Time/DateTime, `Field.Location`, accessors, `Range` |
| `locales/*.toml` ×12 | `date_*`, `field_required`, `date_invalid`, `date_end_before_start` keys |
| `ui/partials/field-date.html`, `field-time.html`, `field-datetime.html`, `field-daterange.html` | the four partials |
| `ui/datetime.js` (new), `ui/datetime_node.mjs` (new), `ui/datetime_test.go` (new), `ui/testdata/datetime/<locale>.json` ×12 | the enhancement + Node-harnessed parser tests |
| `ui/select.js`, `ui/ui_test.go`, `ui/browser_test.go` | optgroup listbox groups, `data-rst-select="false"` opt-out |
| `ui/tokens.css` (+example copies) | combobox popover/quick-pick styles (`rst-dtp-*`) |
| `internal/generate/actions.go` + goldens + regenerated examples | `view.NotFound`/`Forbidden` adoption; error values through `T` |
| `cmd/rastrillo/new.go` | scaffold links `datetime.js`; vendored pin line |
| docs: `forms.md`, `templates.md`, `reference/{form,ui}.md`, `SKILL.md`, spec as-built | Task 9 |

---

### Task 1: `form` kinds and `Range`

**Files:** Modify `form/fields.go`; create `form/datetime.go`, extend `form/fields_test.go` (or new `form/datetime_test.go`).

**Interfaces (produces):**
```go
const (
	Date     Kind = "date"     // wire 2006-01-02
	Time     Kind = "time"     // wire 15:04
	DateTime Kind = "datetime" // wire 2006-01-02T15:04
)
type Field struct { Name string; Kind Kind; Required bool; Location *time.Location } // Location: Date/DateTime parse zone; nil = time.UTC
func (p *Parsed) Date(name string) time.Time          // zero when empty/invalid/unknown
func (p *Parsed) Time(name string) (h, m int, ok bool)
func (p *Parsed) DateTime(name string) time.Time
func Range(p *Parsed, start, end string)              // adds error on `end` when it precedes start; both must be Date or DateTime fields already parsed
```

Error VALUES for the new kinds are catalog keys: an unparseable value → `rastrillo.ui.date_invalid`; `Range` failure → `rastrillo.ui.date_end_before_start`; and (behaviour change, spec §4.4) the Required message for the new kinds only is the key `rastrillo.ui.field_required` — existing Text/Textarea/Money keep their humanised English until the Task 7 sweep routes rendering through `T` (then a follow-up may migrate them; NOT this PR — record as an as-built spec note in Task 9). Echo always seeds the raw submission back. Empty optional values are zero time / `ok=false`, never errors. Parsing is `time.ParseInLocation` on the exact wire format.

- [ ] **Step 1: Failing tests.** Table-driven: valid date/time/datetime parse (including a `Location` case asserting the returned instant's zone); empty optional → zero + no error; empty Required → `field_required` key; garbage → `date_invalid` key + echoed raw; `Range` ok / end-before-start (key on the `end` field) / equal instants ok / one side empty → no error; accessors on unknown names → zeros. Follow `fields_test.go`'s existing table style.
- [ ] **Step 2:** RED (`undefined: Date`), implement (`form/datetime.go` holds the parse helpers + accessors + `Range`; `fields.go`'s `Parse` switch gains three cases; `Parsed` gains `dates map[string]time.Time` and `times map[string][2]int` — mirror the `cents` pattern), GREEN.
- [ ] **Step 3:** `GOFLAGS=-mod=mod go test ./form/ -count=1` then the root suite. **Commit** `form: Date, Time and DateTime kinds, Location, and Range`.

---

### Task 2: The date vocabulary keys, twelve languages

**Files:** `locales/*.toml` ×12; `basecatalog_test.go` untouched (the key-set gate does the work).

Keys (all `rastrillo.ui.`; en values fixed here, the other eleven are the implementer's faithful machine drafts — plain calm register, headers already say machine-drafted):

```
date_today = "today"                       date_tomorrow = "tomorrow"
date_yesterday = "yesterday"               date_next = "next"
date_last = "last"                         date_in = "in"
date_ago = "ago"                           date_at = "at"
date_day = "day|days"                      date_week = "week|weeks"
date_month = "month|months"                date_hour = "hour|hours|h"
date_minute = "minute|minutes|min|mins|m"  date_noon = "noon|midday"
date_midnight = "midnight"                 date_am = "am|a.m."
date_pm = "pm|p.m."
date_set = "Set"                           date_hint = "Try: {example}"
date_pick = "Open the calendar"            date_results = "{n} suggestions"
date_result_one = "1 suggestion"           date_quick_today = "Today"
date_quick_tomorrow = "Tomorrow"           date_quick_next_week = "In a week"
date_quick_plus_1h = "An hour later"       date_quick_plus_2h = "Two hours later"
date_quick_end_of_day = "End of that day"  date_quick_next_day = "Same time next day"
date_invalid = "That date couldn't be read. Try the format shown, or use the calendar."
date_end_before_start = "The end comes before the start."
field_required = "This field is required."
```

Rules: `|` separates accepted spellings (vocabulary keys) — display keys (`date_set`, quick picks, hints, the two error messages, `field_required`) never contain `|`. Keep `{example}`/`{n}` verbatim. Singular|plural order: singular first. Languages without plural marking (ja, zh-Hans, yue, vi) put the one form ("日", "天", "週間" etc.); languages with other affix orders put the word the speaker would type ("後" for ja's `date_in` is CORRECT — the matcher accepts affixes either side of the quantity). ga: use the forms people type ("amárach", "inné", "seachtain|seachtainí"). ar: include common unvocalised spellings.

- [ ] Steps: en first → key-set gate RED ×11 → translate → full root suite green → **Commit** `Base catalogs: the date vocabulary and form-error keys, twelve languages`.

---

### Task 3: The four field partials

**Files:** Create the four partials; modify `ui/ui_test.go` (defined list 30 → 34; render tests).

Each is `field-text`'s envelope (label/required-star/hint/error wiring, byte-similar — the family deliberately repeats the wrapper) around a native input. Contract keys: `Name`, `Label`, `Value`, `Required`, `Hint`, `Error`, `Min`, `Max`, `Plain`. Unless `Plain`, the input carries `data-rst-date` (field-time: `data-rst-time`; field-datetime: `data-rst-date` on a datetime-local input — the script keys off input type) plus the full attribute set, every value through `T`/`Tf`:

```
data-rst-date-words='<JSON: {"today":"today","tomorrow":"tomorrow",...}>' — ONE attribute holding the vocabulary
   as a JSON object built by a new template helper {{dateWords}} registered in ui.Funcs (reads the date_* vocabulary
   keys through the bound T so it localises per request; JSON-encodes; returns template.HTMLAttr-safe string).
data-rst-date-set / -hint / -pick / -results / -result-one / plus the seven quick-pick labels — individual attrs.
```

`field-daterange`: two datetime (or date) fields in a `rst-field-row`, wrapped in `<div data-rst-range{{if .Seed}}="{{.Seed}}"{{end}}>`; keys `Start`/`End` sub-dicts (each the single-field contract), `Legend`, `Seed` (`"session"` seeds end = start+1h browser-side).

- [ ] Steps: extend the defined-list test (34) + a render test per partial (attribute presence incl. the JSON words attr parsing as JSON, `Plain` emitting no data attrs, error wiring) → RED → write partials + the `dateWords` func (+ its own unit test: valid JSON, localised via a stub T) → GREEN → **Commit** `ui: field-date, field-time, field-datetime, field-daterange`.

---

### Task 4: `ui/datetime.js` — the port

**Files:** Create `ui/datetime.js`, `ui/datetime_node.mjs`, `ui/datetime_test.go`, `ui/testdata/datetime/en.json`; modify `ui/tokens.css` (+example copies), `ui/ui_test.go` (self-contained gate covers the new file), `cmd/rastrillo/new.go` (scaffold writes `static/datetime.js`, layouts already link nothing new — ADD a `<script defer src="{{asset "static/datetime.js"}}">` line to all three `ui/layouts/*.html` after select.js, and to the vendored pin + `ui.DatetimeJS()` accessor mirroring `ShimJS`).

Read Tito Go's file first. Port, restructured:

1. **Core parser as a pure function** `parse(text, vocab, localeTables, now)` — no DOM. `vocab` = the words attribute's JSON (each value split on `|`, matcher accent-folds NFD + lowercases both sides). `localeTables` built once per locale from Intl: long/short month and weekday names via `formatToParts` over 12/7 probe dates; digit map from `NumberFormat().formatToParts` so Arabic-Indic/Devanagari digits fold to ASCII. Grammar is word-set based, not positional: {in, 2, weeks} however ordered; affix vocabulary (ja 後) matches adjacent to the quantity. Absolute forms: bare wire formats, `25 dec`, `dec 25`, `25 dec 6pm`, `14:30`, `3pm`, a bare 4-digit year. Unparsed → null (never a guess).
2. **The combobox** — Tito Go's behaviour: hidden-but-present native input as carrier; preview row + "Set ↵"; quick picks (the seven keys; range-end variants when inside `data-rst-range`); `showPicker()` button (suppressed where unsupported); ARIA 1.2 combobox with the exact attribute set select.js uses; Escape reverts; focusout commits; `data-rst-range="session"` seeds end = start+1h on start commit.
3. **Style**: IIFE like select.js, idempotent via `data-rst-enhanced`, strings only from attributes, zero fetches (the self-contained gate will scan it — verify the gate's file list and add datetime.js).

`ui/datetime_node.mjs`: loads datetime.js in Node (the file exposes its parser for tests the way Tito Go's does — a guarded `module.exports`/`globalThis` hook that browsers ignore), reads a fixture JSON path + vocab, runs cases, prints failures. `ui/datetime_test.go`: skips WITH A LOG when `exec.LookPath("node")` fails; otherwise runs the harness per fixture file in `ui/testdata/datetime/`; also asserts every shipped locale has a fixture (`rastrillo.BaseLocales()` — ui already imports the root package). This task ships `en.json` only (≥25 cases: the survey's regression set — "type 2030 must not commit today", "next fri 9am", "in 2 weeks", "25 dec 6pm", "14:30", "tomorrow", unparsable junk → null); the per-locale fixture gate is added but the assertion is `t.Skip`-guarded behind a `fixturesComplete` const flipped in Task 5 (so this commit stays green honestly — note it).

CSS: `rst-dtp` popover (surface, line border, radius, shadow-pop, z-index 40 like row-menu), `rst-dtp__row`/`__quick`/`__set`/`__hint` — logical properties, tokens only.

- [ ] Steps: parser-first TDD via the Node harness (write en.json cases, watch them fail, implement until green), then the DOM layer (browser-tag test added to browser_test.go mirroring the select drive: type "tomorrow", Enter, assert the native input's value = tomorrow's wire date — keep it in the `browser` build tag), then wiring (layouts, scaffold, pin, accessor `ui.DatetimeJS()` + reference/ui.md entry), full suites + examples. **Commit** `ui: datetime.js — the localisable natural-language date combobox`.

---

### Task 5: Eleven more fixture files

**Files:** `ui/testdata/datetime/{ga,zh-Hans,es,hi,pt,bn,ru,ja,yue,vi,ar}.json`; flip `fixturesComplete`; `ui/datetime_test.go`.

Each fixture: ≥20 cases in that language using Task 2's vocabulary and the locale's own month/weekday names and digits (hi/bn/ar include native-digit cases; ja/zh/yue include affix-order relative forms; ar cases are RTL text with LTR times). Cases are (input, expected wire value | null, now). Every case must FAIL if the vocabulary key it exercises is emptied — spot-verify two per language by temporarily blanking a key (report the mutation check).

- [ ] Steps: write fixtures → run harness per locale → fix parser gaps the non-English cases expose (expected: digit folding, affix matching — keep changes minimal and re-run en) → flip `fixturesComplete` → all green → **Commit** `ui: datetime parser fixtures for all twelve locales`.

---

### Task 6: Select optgroups and the markup opt-out

**Files:** Modify `ui/select.js`, `ui/partials/field-select.html` (comment only), `ui/browser_test.go`, `ui/ui_test.go` if it asserts select.js internals.

- `data-rst-select="false"` on a `<select>` opts out regardless of size (mirror Tito Go's `data-searchable="false"`).
- A `<select>` containing `<optgroup>` is currently flattened by the mirror; instead render each group as `role="group"` with an `aria-label` from the optgroup's label and a non-interactive heading `<li>` styled `rst-select__group`; filtering hides a group whose options are all filtered out. Keep the native select untouched as carrier. CSS: `.rst-select__group` heading (faint, xs, uppercase — match `.rst-lrow--head`'s recipe) in tokens.css (+copies).
- Browser-tag test: an optgroup'd select enhances, groups render, filter hides an emptied group; `data-rst-select="false"` never enhances.

- [ ] Steps: RED via the browser test where runnable (if the sandbox blocks chromedp, write the test, run `go vet -tags browser`, and note the drive runs in CI like the existing ones — the F86 memory says ui drives are CI-flaky on GH Actions; keep assertions robust) → implement → suites + pins → **Commit** `ui: select.js learns optgroups and a markup opt-out`.

---

### Task 7: Generated actions adopt the error helpers

**Files:** Modify `internal/generate/actions.go` (+goldens), regenerate `examples/{blog,tickets,notes,helloworld}` gen trees; possibly `internal/generate/templates.go` (form-error rendering through `T`).

- Every emitted `http.NotFound(w, r)` → `view.NotFound(ctx, w, r)`; emitted 403s (`http.Error(w, "signed out", 403)` at actions.go:443-ish) → `view.Forbidden(ctx, w, r)`; emitted plain 400/"Bad request." STAYS (no key exists; note it). The emitters already have `ctx` in scope — verify each site.
- Where generated templates render `.Errors` values, wrap with the bound `T` (the generated render path has one — find `genT` wiring) so Task 1's key-valued errors localise and plain-English values pass through verbatim. Confirm with a golden showing `T` applied.
- Regenerate all examples; their suites must stay green with byte-mechanical diffs (the PR-2 pattern: verify with a scratch re-run of the generator).

- [ ] Steps: goldens RED → emitters → regenerate → `internal/generate`, `cmd`, examples green → **Commit** `Generated actions: view.NotFound/Forbidden, and form errors through T`.

---

### Task 8: Docs, SKILL.md, spec as-built

**Files:** `docs/site/forms.md` (the three kinds, Location, Range, error keys + passthrough semantics), `docs/site/templates.md` (the four partials + the enhancement's contract + the select's optgroups/opt-out), `docs/site/reference/form.md` (does it exist? — the reference dir has form.md; extend), `docs/site/reference/ui.md` (`DatetimeJS`; already-added entries verified), `SKILL.md` (one §4 sentence: the kinds + Range + error keys; budget!), spec §4 as-built sentences (Required-key migration scoped to new kinds; the words-JSON attribute design; fixture-gate mechanism; anything else that diverged).
- [ ] Steps: docsite gate RED on any missing symbol → write → all gates + examples + `wc -c SKILL.md` → **Commit** `Docs: date and time fields, select groups; spec amended to as-built`.

---

### Task 9: PR

- [ ] Final whole-branch review happens before the push (controller's standing order). Then push `datetime-select`, `gh pr create` — title "Date and time fields, select optgroups, and error-helper adoption", body summarising §4/§4.5/§4b.2 delivery, the no-English-in-the-parser design, and the fixture gate; watch CI.

---

## Self-review

**Spec coverage:** §4.1 partials → T3; §4.2 script (vocabulary/Intl/digits/word-order/quick picks/showPicker/refusal) → T4; §4.3 Node harness + per-locale fixtures → T4+T5; §4.4 kinds/Location/Range/error keys → T1 (+T7 rendering); §4.5 select → T6; §4b.2 adoption → T7; §0 localisability → T2 keys + T4 Intl + T5 fixtures; docs → T8. Deviations pre-declared for the spec: Required-key change scoped to new kinds; plugins keep plain text (no Ctx — spec already says so).

**Types:** `Field.Location *time.Location` (T1) referenced nowhere else; `dateWords` helper (T3) consumed by T4's attribute contract; `ui.DatetimeJS()` (T4) consumed by scaffold pin (T4) and docs (T8); fixture schema (input/expected/now) shared T4/T5; `view.NotFound(ctx, w, r)` signature matches PR 2's shipped helper.

**Order-sensitive:** T2 before T3 (partials' T lookups), T3 before T4 (attribute contract), T4 before T5 (parser exists). T1, T6, T7 independent of those.
