# Multi-currency money Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store every amount with its ISO 4217 currency, format and parse it on the server from CLDR data in the reader's locale, link that data as one opt-in pack per language, and render every language from one shared, boot-time template set that takes the request's localizer as data.

**Architecture:** A generated CLDR core (`locales`) plus one pack per base language (`locales/<lang>`) feed a small `currency` package (format, seed, parse, rescale). Boot validates the declared locales (`locales.CheckConfig`) inside the serving binary, and a `RASTRILLO_CHECK_CONFIG=1` probe reports that check without opening anything. Templates move to localizer-first calls (`{{T $L "key"}}`, `{{money $L .Amount .Currency}}`) executed by `ui.Pages`, strict by default; a codemod (`rastrillo migrate-templates`) and a doctor preflight migrate apps. The generator stores a currency beside every amount, upgrades legacy rows once at boot with a tripwire, and versions money-bearing events.

**Tech Stack:** Go 1.26 (root module `amadan.net/rastrillo/rastrillo`), `html/template` and `text/template/parse`, `database/sql` over modernc SQLite, `migrate`/`eventlog`, sqlc, `golang.org/x/tools/go/packages` (new direct dependency), cldr-json 48.0.0 and archived ISO 4217 editions (harness only), Node's ICU (harness only).

**Spec:** `docs/superpowers/specs/2026-10-09-multi-currency-money-design.md` (approved by Paul). Every task's implementer reads the spec section its task names. The executable prototypes it cites are `hack/moneycldr` and `hack/templatemem`; each task says which prototype file it starts from. Promote proven code; do not re-derive it.

## Why the tasks are in this order

1. **Copy is reviewed before any of it is written** (Task 1). Nearly every task writes a user-facing or operator-facing string, and in `internal/designsystem` and the catalogs the English is the translation key. Task 1 reviews every string known before code exists, including package docs, flag help, generated comments and screen copy (with previews). Docs prose, `DESIGN.md` and the changelog's API list get a second review (Task 28) because they can only be drafted from what shipped.
2. **The data and the arithmetic come next** (Tasks 2 to 5). Everything else formats, parses or validates through them, and a wrong exponent or a wrong fallback is the failure that silently changes money. Task 5 points the ICU oracle at the shipped code straight away, so the riskiest code is checked against an independent formatter before anything is built on it.
3. **Declared locales are written into apps before boot refuses them** (Task 6 before Task 7). Task 6 makes `generate` and `rastrillo new` link the packs an app declares and makes `generate --check` enforce the declared catalogs; only then does Task 7 make `Serve` refuse an app that links none.
4. **Templates move in the order that keeps every consumer working** (Tasks 9 to 14). The function map learns both call forms first (Task 9, with its existing tests migrated), so legacy and migrated templates both render. The codemod exists before anything is migrated (Tasks 10, 11), because the partials are migrated *by* it and its validation is the guard against corrupting them. Then ui's partials migrate (Task 12), then `ui.Pages` is built and tested over the real migrated partials (Task 13), then the shells get the request's language, direction and switcher (Task 14). The brief suggested Pages before the codemod; this order is the reverse because Pages' corpus test needs migrated partials, and migrating by hand would skip the codemod's validation.
5. **Storage libraries before the generator** (Tasks 15 to 17 before 19 to 22). The generator only emits calls into tested libraries (`eventlog`, `currency/replay`, `currency/sqlmoney`), so the risky SQL and replay logic is written and tested once, not as template-rendered text. Task 20 makes generated apps unable to serve without running the generated `Boot` before any money reaches storage (Tasks 21, 22).
6. **Apps last** (Tasks 23 to 27): the examples and the scaffold adopt `ui.Pages`. The gallery sample and the `pt-PT` retag are separable. Docs follow what shipped (Tasks 28, 29). The prototypes are retired only once every test they hold has been ported (Task 30), and the full gate runs once, there.

## Global Constraints

Every task's requirements include these. Decision text is copied verbatim from the spec.

**Decisions (Paul), verbatim** (spec "Decisions (Paul)", reflowed onto one line each; four plan resolutions that depart from the spec text are recorded in Tasks 16, 17, 21 and 22; decision 11 means no `Deprecated:` marker in this release, and decision 14 means `money/` does not change):
- 1. **The currency is stored with each amount**: an integer count of the currency's minor unit plus its ISO 4217 code. Generated CRUD stores a currency column beside each money column.
- 2. **Formatting happens on the server** from CLDR data, in the reader's locale. The currency always comes from the data, never from the locale. Browser `Intl` is at most a progressive enhancement.
- 3. **One template function for money**: `{{money $L .Amount .Currency}}`, taking the request's localizer first like every locale function (§10; the original form was `{{money .Amount .Currency}}`).
- 4. **`form.Money` takes a currency** and accepts that currency's decimals. `FormatCents` and friends keep working.
- 5. **Locale data is opt-in, one pack per base language.** A pack holds that language's CLDR formatting data for all its modern regional variants: the `de` pack covers `de`, `de-AT`, `de-CH`, `de-LI` and the rest. Packs are shaped to carry the framework's translated strings and `{{number}}`/date data later. Packs live at `locales/<lang>`. An app declares the locale codes it offers; the packs it imports are derived from those codes (§3). `rastrillo new` and `rastrillo generate` write blank imports for exactly those packs. **New apps start with one locale**, the one picked, English by default, with no `/xx/` prefixes and no switcher. Adding a language is one line in `manifest/app.toml`. An "all modern" import stays available. Expanding the framework's translations to more languages is separate future work (§13).
- 6. **Follow CLDR for symbol placement**, not ICU.
- 7. **`{{number}}` moves onto the new data later, separately.** Until then it keeps its current float, `uint64`, integer-string and precision behaviour.
- 8. **The gallery's Portuguese is retagged `pt-PT`**, and existing `pt` catalog keys and URLs keep working (§9).
- 9. **No currency picker in v1.** Changing a record's currency is a separate, explicit operation (§7).
- 10. **The default currency is per resource**, set in the manifest. The legacy backfill currency is a separate setting.
- 11. **`FormatCents` gets `// Deprecated:` only after** a documented migration path and at least one release that ships it.
- 12. **Negative amounts**: the parser reads signs; each field states whether it allows them.
- 13. **Storage-scale digits are always shown.** A nonzero minor unit is never rounded away.
- 14. **No Tito extraction now.** Locale data stays out of `money/`.
- 15. **`Options.DefaultLocale` alone sets the formatting locale**, without turning on locale routing.
- 16. **The hidden parsing-locale input stays.** It is preserved with the echoed text and its shape is validated.
- 17. **v1 requires a stop-start upgrade and forbids rollback** to currency-unaware writers for apps that store money; their rollback is a backup restore (round-two finding 1; §8).
- 18. **A form carries the currency it displayed**, and the POST rejects a mismatch (round-two finding 2; §5). That value is only a consistency check; it never decides the currency.
- 19. **The event-log payload column is renamed only in apps whose events carry money.** Whether they do is declared, never guessed from data. eventlog detects which column it has at startup, and apps without money keep `payload` and upgrade normally (§8).
- 20. **The configuration probe is the environment variable `RASTRILLO_CHECK_CONFIG=1`**, not a flag (§3).
- 21. **No template set per language, and none per request.** The base (partials and layout) is parsed once at boot, and each page is one boot-time clone of it with the page's own definitions. Every language and request executes those. Locale-dependent output is resolved per execution from the localizer in the template data (`{{T $L "key"}}`, `{{money $L .Price .PriceCurrency}}`), dispatch is strict in production, and request values come from the data too (§10).
- 22. **Numeric input is normalised by default.** A money field accepts input with exactly one plausible reading: white space and bidi marks, its own currency in any CLDR spelling on either side (a symbol it shares with other currencies included, with the ISO code shown beside the input), grouping in valid positions (Indian grouping in every locale), the other decimal mark where that is unambiguous, and a sign or parentheses (then the field's negative policy). It refuses another currency's explicit spelling, input whose readings disagree (an over-precise reading counts), a nonzero digit beyond the exponent, malformed separators, and anything else. Nothing is rounded. An unchanged edit-form seed is recognised by the form, not guessed by the parser. `Field.Strict` turns it off and gives today's strict errors (§5; Paul's rulings on Astra's review of §5.1).
- 23. **The page's own group mark wins.** When the input's one separator has three digits after it and the page's locale uses that separator for grouping, in a valid position, the grouping reading is taken: `en` `1,000` USD is 1000, `de` `1.500` EUR is 1500, `ja` `￥1,285` is 1285, `en` `1,500` KWD is 1500. Only the other direction stays ambiguous: the separator is not the page's group mark and a group reading also exists (`en` `1.500` for KWD or USD). The accepted cost: a decimal-comma amount pasted into an English page, German `1,500` meaning 1.5 KWD, is read as 1500 (§5.1).

**Spec rules that bind every task, verbatim:**
- "The one rule: a currency is present if and only if an amount is present."
- "There is no schema default currency."
- "The stored exponent is always `money.MinorUnits(code)`."
- "The storage set only grows." "Removing a code or changing an exponent fails the build."
- "Configuration is never repaired." "Request tags (`?lang`, `Accept-Language`, a stored row) stay forgiving, through `Resolve`."
- "The currency never changes; only the conventions fall back."
- "The client value is never authoritative."
- "It **always exits and never serves**: 0 for an accepted configuration, 1 for problems, and 2 for any value other than `1`."
- "no function map is ever changed after boot."
- "The preflight is advisory. The runtime check is the guarantee."
- "A rewrite that fails validation is not written."
- "Every event a new writer emits carries `V: 2`." "The version is checked before the kind."
- "Any failure rolls everything back and fails boot." "It never guesses."

**Repository rules:**
- Work only in `/home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies` on branch `money-currencies`. Never `cd` to the main checkout. Do not push.
- One atomic commit per task (or per numbered commit step), keeping the branch building and its targeted tests green. Imperative subject; the body says why (the failure prevented, the alternative rejected), not what the diff shows. Every commit message ends with the line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Git may need to run outside the sandbox (sandboxed git sees phantom dotfiles).
- Comments say why a thing is the way it is and name the failure it prevents. No comment that restates the code.
- User-facing and operator-facing English is used **exactly as approved**: strings in code from the spec's Appendix C (written by Task 1), docs pages as approved in Task 28's review. Never reword an approved string in code. No em dashes in any copy. In `internal/designsystem` and the `locales/*.toml` catalogs the English is the translation key: write the approved English, then draft the eleven translations (`ga`, `zh-Hans`, `es`, `hi`, `pt` (or `pt-PT` after Task 27), `bn`, `ru`, `ja`, `yue`, `vi`, `ar`) yourself, and say in the commit body that they are unreviewed drafts.
- Go commands run as the Makefile runs them: `GOFLAGS=-mod=mod CGO_ENABLED=0` (the race target alone uses `CGO_ENABLED=1`). Leave `GOCACHE`, `GOMODCACHE` and `GOTMPDIR` at their defaults; never point them under `/tmp`. Scratch goes in `$TMPDIR` and is deleted when the task ends; `$TMPDIR` sits under a foreign `.git`, so Go builds there need `-buildvcs=false`.
- While iterating, run only the targeted tests each task names. The full `make ci` runs once, in Task 30.
- The examples under `examples/` are separate modules with a `replace` back to the checkout: test them from their own directories (`make example-<name>`).
- Directory budgets: 5,000 source and 8,000 test lines per directory unless `.rastrillo/budgets.txt` says otherwise. `cmd/rastrillo` has 171 source lines of headroom (5,479 of 5,650): put command logic in `internal/` packages and keep `cmd/rastrillo` changes to flag parsing and wiring. `internal/generate` has 444 (4,556 of 5,000): new code that does not need the package's private helpers goes in subpackages, and this plan places it so: `internal/generate/packs` (Task 6), `internal/generate/catalogs` (Task 6), `internal/generate/prune` and `internal/generate/bootgen` (Task 20). What stays in `internal/generate` itself is the money changes to the existing store, action, template and mergeable emitters (Tasks 19, 21, 22) and `migrationsgen.go` (Task 21), which need `columns`/`sqlName`; Tasks 19, 21 and 22 run `go run ./cmd/rastrillo budget size`, and the first that crosses 5,000 adds a `.rastrillo/budgets.txt` line (`size internal/generate <measured+5%> - the generated-store emitters share private column helpers; splitting them is owed`) in its own commit. Check with `go run ./cmd/rastrillo budget size` after any task that adds code.
- `SKILL.md` is 3 bytes under its 30,750-byte ceiling (`skillmd_test.go`). Trim redundant prose before adding; if a load-bearing fact cannot fit, raise the ceiling in the test and say why there (Task 29).
- `.amadan/ci.d/50-suite@33` must keep matching the Makefile's `SUITE`. This plan adds no suite role (new packages run in `root`); if you add one, raise the `@N` in the same commit (`amadan_ci_test.go` fails otherwise).
- Vendored asset copies: `examples/*/static` and the scaffold's `static/` hold copies of ui assets checked by pin tests and `rastrillo doctor`. If a task changes a shipped asset (none is planned), refresh every copy in the same commit.
- `golang.org/x/tools` becomes a direct requirement of the root module in Task 11 (spec §10: "When the implementation moves the preflight into `cmd/rastrillo`, it adds the requirement to the root module").
- New packages that must stay GORM-free join `GORM_FREE` in the Makefile: `./locales`, `./currency`, `./currency/replay`, `./currency/sqlmoney`.
- **No task invents copy.** Every user-facing or operator-facing string (messages, logs meant for operators, flag help, usage lines, package doc comments of public packages, comments in generated files an app author reads, catalog values, gallery prose, docs) is quoted from Appendix C (Task 1, or the second copy review's C.9 for the strings listed at the end of this plan) or from Task 28's approved docs. A task that finds it needs a string not there stops, runs the `copy-review` skill on just that string (with a preview if it appears on a screen), adds it to Appendix C in the same commit, and only then writes it. Doc comments on exported identifiers are code comments, held to the comment rule above and checked in code review.
- **A task that changes a contract migrates every existing test, fixture and consumer of it in the same task**, preserving the behaviour those tests assert. Each task's Files list names the ones known; `grep` for the changed identifier before committing and fix what it finds.
- **Changing generator output:** regenerate every example (`make build-cli && for e in blog tickets notes helloworld; do .build/rastrillo generate examples/$e; done`), read the diff, then `git add 'examples/*/gen'` so the regenerated tree is the baseline, and only then run `make generate-check`. Its final `git diff --exit-code` compares the working tree with the index, so after staging it checks that a second generation changes nothing, which is what it is for. Never commit a regenerated example without running that example's tests (`make example-<name>`) and every direct consumer of a changed generated identifier.

## Review Focus

The five input classes most likely to bite a person using this, each pinned by a named test in the task that owns the code.

1. **Silent monetary value change: an amount rescaled twice when two instances boot against the same legacy database at once.** The spec covers reruns but not concurrent first boots; a double rescale turns KWD 12.500 into 125.000. Expected: exactly one boot migrates, the other sees the marker and changes nothing. Pinned by `TestUpgradeConcurrentBootsRescaleOnce` in Task 17. Related pins: `TestStorageSetIsFrozen` (Task 2), `TestRoundTripEveryLocale` (Task 4), `TestCurrencyEchoMismatchKeepsText` (Task 8).
2. **Wrong locale in production: a request names a linked or unlinked locale the app does not offer** (a stale `rastrillo_locale` cookie, a stored preference, `Accept-Language: de-CH`). Expected: pages render the app's default identity's catalog and format; a stored preference for an unlinked locale formats in the nearest linked parent or the app default, never root, and the missing pack is logged once. Pinned by `TestFormatForUnofferedIdentity` (Task 3), `TestMissingPackWarnsOncePerPack` (Task 4) and `TestPagesLocalizerForUnofferedIdentity` (Task 13).
3. **Upgrade hazard: an inherited `RASTRILLO_CHECK_CONFIG` reaching a database entry point, and an old binary started after the upgrade.** Expected: the probe exits before `sidecar run`, `Serve`, `Handler`, `OpenDB` and `db.Open` refuse with exit 2 and open nothing, and an old binary's queries and its unchanged edit POST fail (SQLite's "no such column: price" or "has no column named price") while changing no row. Pinned by `TestProbeRunsBeforeSidecarDispatch` and `TestGuardsRefuseBeforeOpening` (Task 7), `TestOldBinaryQueriesFailAfterUpgrade` (Task 17), `TestOldEditHandlerIsRefusedAfterUpgrade` (Task 21) and `TestEventlogUpgradeRollbackUpgrade` (Task 15).
4. **Template leakage: a strict-mode failure halfway through a page.** html/template writes as it executes, so a page that fails at a partial reached without `L` would send half a page under a 200. Expected: `Pages.Execute` writes nothing on error. Pinned by `TestPagesExecuteWritesNothingOnError` (Task 13). Related pins: `TestPagesTwoLanguagesIsolated` (Task 13) and `TestMoneyEscapesInEveryContext` (Task 9).
5. **Codemod corruption of a real app's file: CRLF line endings, a UTF-8 byte-order mark, `{{-`/`-}}` trim markers, template comments, and literal text that looks like an insertion.** The spec's fixtures are LF-only and comment-free. Expected: the rewrite only inserts its allowed texts, comments are byte-identical, and a call inside `{{/* */}}` is left alone. Pinned by `TestRewriteOnlyInserts` (Task 10).

---

## File Structure

| Path | Responsibility | Task |
|---|---|---|
| `docs/superpowers/specs/2026-10-09-multi-currency-money-design.md` Appendix C | Every approved string, by id | 1, 28 |
| `locales/data.go`, `locales/doc.go` | Locale record types, the pack registry, merged lookup, storage-set and index accessors | 2 |
| `locales/core_gen.go`, `locales/<lang>/pack_gen.go` (101), `locales/modern/` | Generated CLDR data | 2 |
| `locales/storage_test.go` | Frozen storage-set test | 2 |
| `locales/resolve.go`, `config.go`, `probe.go`, `localestest/` | Resolution, `CheckConfig`, the probe's logic | 3 |
| `currency/` | Format, seed, parse, rescale, missing-pack warning | 4 |
| `hack/moneycldr/cmd/{gen,check,packsize,xtext}` | Harness repointed at the shipped packages | 5 |
| `internal/manifest/app.go`, `internal/generate/packs/`, `internal/generate/catalogs/` | `app.toml`, `gen/locales/packs.go`, the declared-catalog check | 6 |
| `internal/checkconfig/`, `probe.go` (root) | The database-entry guard; `ProbeConfig`, `FormatLocale` | 7 |
| `form/moneyfield.go`, `ui/partials/field-money.html`, `ui/field_money_browser_test.go` | The money form contract and its control | 8 |
| `ui/localizer.go`, `ui/localefuncs.go` | `Localizer`, two-form dispatch in `ui.Funcs` | 9 |
| `internal/tmplmigrate/` | The codemod, the Go preflight, the command body | 10, 11 |
| `internal/localecheck/` | Doctor's locale checks | 11 |
| `ui/pages.go`, `localemw.go` (`Negotiate`) | `ui.Pages`; resolving a request's locale without the middleware | 13 |
| `ui/layouts/*.html` | Request `lang`/`dir` and the switcher | 14 |
| `eventlog/money.go`, `eventlog/money/migrations/0001_payload_v2.sql` | `MoneySchema`, `UpgradeForMoney`, column detection, upgrade boundary | 15 |
| `currency/replay/` | Event money rules, explicit clears, the unversioned-event rule | 16 |
| `currency/sqlmoney/` | SQL legacy step, added columns, pairing check, version marker | 17 |
| `manifest.go` (root) | `Resource.Currency`, `Resource.Legacy` | 18 |
| `internal/generate/bootgen/`, `internal/generate/prune/` | Generated `Boot`, the `Booted` router, pruning stale generated files | 20 |
| `internal/generate/*.go` | Templates syntax (19), SQL money (21), event money (22) | 19-22 |
| `internal/designsystem/formats.go` | The Money sample | 26 |
| `retag.go` | The `pt` to `pt-PT` retag | 27 |
| `docs/site/**` | Docs pages | 28 |
| `SKILL.md`, `DESIGN.md`, `CHANGELOG.md`, `ui/funcs.go` (FuncsWith comment), `cmd/rastrillo/new.go` (manifest README) | Written from approved copy | 29 |

---

### Task 1: Copy review, batch one: every string known before code exists

Spec: §6 (error copy), §8 (changelog draft), §9 (sample body), and every operator-facing message the spec and prototypes draft.
Starts from: the spec's tables and the string literals in `hack/moneycldr/proto/proto.go`, `proto/probe.go`, `replay/replay.go`, `hack/templatemem/entry/entry.go`, `pages/pages.go`, `codemod/codemod.go`, `codemod/gocheck.go`, `cmd/migrate-templates/main.go`.

This is a human gate. No code changes. It is first because Task 2 already writes a package doc and flag help, and because the catalog English and the gallery English are translation keys: a word changed afterwards costs eleven redrafted translations. It covers every string Tasks 2 to 27 write. Docs pages, `DESIGN.md`, the changelog's list of new API and the migration guidance in code comments and READMEs are batch two (Task 28), drafted from what shipped and reviewed before Task 29 writes them.

**The inventory rule.** Before Step 1, read every later task's Files and Interfaces and list each string it will print, log for an operator, show on a screen, put in a catalog, a flag's help, a usage line, a public package's doc comment, or a comment in a file `generate` or `rastrillo new` writes into an app. Every one needs an id below or a new id you add in the same section. If a later task's implementer still finds an unlisted string, the Global Constraints rule applies (stop, review that string, add it to Appendix C).

**Files:**
- Create (scratch, never committed): `copy-review/strings.json`, `copy-review/` server output
- Modify: `docs/superpowers/specs/2026-10-09-multi-currency-money-design.md` (new "Appendix C: Approved copy")

**Interfaces:**
- Produces: Appendix C, a table per section with columns `id`, `English (approved)`, `Where`. Every later task quotes strings from it by `id`. The `id`s are fixed here and used verbatim in later tasks.

- [ ] **Step 1: Draft `copy-review/strings.json`**

Use the `copy-review` skill's format. One entry per string, in these sections and with these ids. Give each a `context` in plain words saying where it appears and who reads it. Draft text is below; do not improve it before the review, the reviewer does that. Mark section B and C entries `"notes": "English only, no translations"`; section A entries `"notes": "translated into eleven languages after approval; the English is the key's value"`. `%q`, `%s`, `%d` and `%v` are values filled in at run time; say so in `notes` and keep them in place.

Section A, "Form errors" (shown under a money field):

| id | draft |
|---|---|
| `rastrillo.ui.money_invalid` | Enter a valid amount. (existing, shown for consistency) |
| `rastrillo.ui.money_precision` | Enter an amount with at most 2 decimal places. (existing) |
| `rastrillo.ui.money_precision_0` | Enter a whole amount, with no decimal places. |
| `rastrillo.ui.money_precision_3` | Enter an amount with at most 3 decimal places. |
| `rastrillo.ui.money_decimal_comma` | Use a comma for decimals, like 12,50. |
| `rastrillo.ui.money_decimal_point` | Use a point for decimals, like 12.50. |
| `rastrillo.ui.money_currency` | Enter the amount without a currency sign. |
| `rastrillo.ui.money_negative` | Enter an amount of 0 or more. |
| `rastrillo.ui.money_reenter` | Enter the amount again. |
| `rastrillo.ui.money_currency_changed` | The currency changed while you were editing. Check the amount and save again. |

Section B, "Messages for the person running the app" (logs, stderr, boot refusals, CLI output):

| id | draft |
|---|---|
| `op.config.prefix` | locale configuration: (followed by the problems, joined with "; ") |
| `op.config.empty` | an empty locale code |
| `op.config.whitespace` | %q contains white space; write %q |
| `op.config.not_canonical` | %q is not in canonical form; write %q |
| `op.config.malformed` | %q is not a well-formed language tag |
| `op.config.duplicate` | %q is offered twice |
| `op.config.unknown` | %q is not a locale CLDR's modern set formats |
| `op.config.missing_pack` | %q needs the %s pack: import _ "amadan.net/rastrillo/rastrillo/locales/%s" |
| `op.config.default_not_offered` | default locale %q is not one of the offered locales %v |
| `op.packs.malformed` | offered locale %q is not a well-formed language tag |
| `op.packs.unknown` | offered locale %q is not a locale CLDR's modern set formats |
| `op.probe.bad_value` | RASTRILLO_CHECK_CONFIG=%q: set it to 1 to check the configuration, or unset it to serve |
| `op.probe.done` | RASTRILLO_CHECK_CONFIG=1: checked the configuration and exited without serving; unset it to serve |
| `op.guard` | RASTRILLO_CHECK_CONFIG is set but %s was reached before the probe ran: call rastrillo.ProbeConfig (or Resolve, or Run) before opening the database; nothing was opened |
| `op.boot.refused` | rastrillo: refusing to start: %v |
| `op.currency.missing_pack` | currency: %s needs the %s pack, which is not linked; amounts are formatted in %s instead. Import _ "amadan.net/rastrillo/rastrillo/locales/%s" |
| `op.legacy.undeclared` | %s has %d rows from before money had a currency. Add [legacy] currency and scale to manifest/%s.toml and run rastrillo generate. |
| `op.legacy.inexact` | %d at scale %d is %s, which %s cannot hold |
| `op.legacy.overflow` | %d at scale %d is too large for %s |
| `op.legacy.failed` | %s.%s: %d rows cannot be converted, and nothing was changed: |
| `op.legacy.pairs` | %s.%s: %d rows have an amount without a currency or a currency without an amount; fix them before starting |
| `op.legacy.newer` | this database's money data is version %d, and this binary understands version 2. Run the newer version, or restore the backup taken before it ran. |
| `op.legacy.scale_missing` | manifest/%s.toml: [legacy] needs scale, the number of decimal places the old integers carry (2 for cents, 0 for whole units) |
| `op.legacy.currency_missing` | manifest/%s.toml: [legacy] needs currency, the currency the old integers were in |
| `op.legacy.added_columns` | %s.%s: added %s_minor and %s_currency; existing rows have no amount |
| `op.legacy.drop_blocked` | %s.%s cannot be replaced by %s_minor because %v; remove that index, view or trigger and start again |
| `op.replay.version` | event %d: version %d is not one this binary understands |
| `op.replay.currency_no_amount` | event %d: %s has a currency but no amount |
| `op.replay.amount_no_currency` | event %d: %s has an amount but no currency |
| `op.replay.currency_change` | event %d: %s changes currency %s to %s: a currency change needs its own event |
| `op.replay.v1_currency` | event %d: a version-1 event carries a currency for %s |
| `op.replay.v1_needs_legacy` | event %d: a version-1 amount for %s needs [legacy] in the manifest |
| `op.replay.after_upgrade` | %s: an event from writer %s (seq %d) has no version but was written after the money upgrade, so a currency-unaware version wrote it |
| `op.eventlog.both` | eventlog: the table has both payload and payload_v2; restore the backup taken before the upgrade |
| `op.eventlog.upgrade_refused` | eventlog: could not rename payload for money: %v. If this app keeps its tables in Options.Migrations, apply eventlog.Schema with migrate.Apply instead; see the eventlog reference |
| `op.replay.half_clear` | event %d: %s clears the amount or the currency but not both |
| `op.boot.not_booted` | %s: gen.Boot has not run for this database; call it after the schema is applied and before serving |
| `op.boot.wrong_db` | %s: this request's database is not the one gen.Boot checked; pass the same *sql.DB to gen.Boot and to the router's contexts |
| `op.generate.migration_appended` | %s: appended %s to gen/store/%s/migrations.go; released statements are never edited |
| `op.eventlog.neither` | eventlog: the table has neither payload nor payload_v2; apply eventlog.Schema first |
| `op.generate.no_currency` | manifest/%s.toml has a money field but no currency; add currency = "EUR" (or another ISO 4217 code), the default for new records |
| `op.generate.bad_currency` | manifest/%s.toml: currency %q: %v |
| `op.generate.ineligible` | warning: manifest/%s.toml: %s is still accepted for storage but is no longer issued; choose a current currency for new records |
| `op.generate.legacy_currency` | manifest/%s.toml: [legacy] currency %q: %v |
| `op.generate.legacy_scale` | manifest/%s.toml: [legacy] scale %d must be from 0 to 18 |
| `op.generate.default_locale_removed` | --default-locale was removed: the default comes from default_locale in manifest/app.toml |
| `op.catalog.missing` | manifest/app.toml offers %q, so locales/%s.toml is required: %s |
| `op.catalog.reason_no_framework` | the framework's own strings are not translated into it |
| `op.catalog.reason_app_keys` | this app has its own strings, and they are in English |
| `op.generate.pruned` | removed %s: generated by an earlier run, and nothing generates it now |
| `op.generate.stale` | %s was generated by an earlier run and nothing generates it now; run rastrillo generate |
| `op.generate.reserved_app` | manifest/app.toml holds the app's locales, so no resource can be named "app" |
| `op.generate.money_schema_added` | %s keeps money in events, so the next deploy renames the event log's payload column. Take a backup, stop every running copy, then start the new version. Never run old and new versions side by side. To go back, restore the backup. |
| `op.pages.no_localizer` | ui: no localizer |
| `op.pages.nil_localizer` | %s called with a nil $L (the data reached this template without "L") |
| `op.pages.page_data` | in the page data for %q |
| `op.pages.legacy_call` | ui: legacy locale call; run rastrillo migrate-templates |
| `op.pages.compat_on` | ui: COMPATIBILITY MODE: legacy locale calls and missing localizers render in the default locale %q instead of failing; finish rastrillo migrate-templates and turn it off |
| `op.pages.compat_call` | ui: COMPATIBILITY MODE: a legacy or localizer-less %s call rendered in the default locale |
| `op.pages.reserved_func` | ui: app function %q would replace a framework function |
| `op.money.first_arg` | money: the first argument must be $L (money has no legacy form) |
| `op.check.side_flip` | a modern locale now moves the symbol for letter-final symbols (%v); decision 6 applies (follow CLDR); review the oracle's side-flip explanation |
| `op.money.arity` | money: want $L, an amount and a currency; got %d arguments after $L |
| `op.doctor.probe_failed` | the configuration probe failed (exit %d): %s |
| `op.doctor.packs_stale` | manifest/app.toml offers %v but gen/locales/packs.go was generated for %v; run rastrillo generate |
| `op.new.locale_unsupported` | rastrillo new --locale %q: the framework's own strings ship in %s. Start with one of those, then add your language to manifest/app.toml |
| `op.migrate.gen_stale` | %s holds generated templates in the old syntax; run rastrillo generate |
| `op.migrate.*` | every string `hack/templatemem/cmd/migrate-templates/main.go` prints, and every `What`/fix string in `hack/templatemem/codemod/gocheck.go` and `codemod.go` (`ConflictError`, review and no-data reports). One entry each; id `op.migrate.<short name>`; draft text verbatim from the prototype. |

Section B1, "Command-line help and package docs" (English only; shown by `-h`, `rastrillo help`, and on pkg.go.dev):

| id | draft |
|---|---|
| `flag.gen.root` | the root module's locales directory, where the generated data is written |
| `flag.migrate.write` | rewrite the templates (without it, report what would change and change nothing) |
| `usage.migrate_templates` | migrate-templates [-write] PATH...    move templates to the localizer-first syntax; exits 1 while anything is left to do |
| `flag.new.locale` | the one language the app starts with (default en); one of: %s |
| `usage.generate` | generate [--check] [DIR]    write gen/ from manifest/; --check verifies without writing |
| `pkgdoc.locales` | the doc comment drafted in this plan's Task 2, Step 3 (`locales/doc.go`), verbatim |
| `pkgdoc.pack` | Package %s links CLDR money-formatting data for every modern "%s" locale. Import it for its side effect. |
| `pkgdoc.modern` | Package modern links every language pack: all CLDR modern-coverage locales. Import it for its side effect. |
| `pkgdoc.currency` | Package currency formats, seeds and parses money: an integer count of a currency's minor unit plus its ISO 4217 code, written in a reader's locale from CLDR data. |
| `pkgdoc.replay` | Package replay holds the money rules generated event stores replay with: a currency if and only if an amount, and versions checked before kinds. |
| `pkgdoc.sqlmoney` | Package sqlmoney upgrades generated SQL money columns once at boot and refuses to start on rows that break "a currency if and only if an amount". |

Section B1b, "Comments in files written into an app" (read by the app's author):

| id | draft |
|---|---|
| `gen.packs.doc` | Package locales links the formatting packs manifest/app.toml's locales need. Wire DefaultLocale and Locales into rastrillo.Options. |
| `gen.packs.default` | DefaultLocale is manifest/app.toml's default_locale. |
| `gen.packs.locales` | Locales is manifest/app.toml's locales. It is nil when the app offers one locale, so a one-locale app gets no /xx/ prefixes and no switcher. |
| `gen.packs.test` | The packs this package imports are exactly the ones its offered locales need. generate --check proves this file matches manifest/app.toml; the RASTRILLO_CHECK_CONFIG probe proves main links this package. |
| `gen.boot.doc` | Boot upgrades this app's money data once and checks it, then returns the value Router needs. Call it after the schema is applied and before serving; a non-nil error must stop the start. |
| `gen.boot.booted` | Booted proves Boot ran for this database. Router takes it so an app cannot serve without it. |
| `gen.template.contract_l` | L  *ui.Localizer, required: set by the action from the request; ui.Pages' middleware puts it there |
| `gen.template.contract_money` | Money  map[string]form.MoneyInput, required for money fields: the text, the locale it is written in, and the currency, per field |
| `scaffold.app_toml` | The locales this app offers. Add a code to offer another language, then run rastrillo generate; with two or more, URLs gain /xx/ prefixes and pages a language switcher. |

Section B2, "The scaffolded app's AGENTS.md" (read by the people and agents working on a new app):

| id | draft |
|---|---|
| `scaffold.agents.money` | Money is a whole number of the currency's smallest unit, stored with its ISO 4217 code beside it, and shown with `money`. A float never touches a value a person will be held to. |

Section C, "Changelog" (`CHANGELOG.md`, top of the next release): the spec §8 draft, verbatim, as entry `changelog.money`. Add one entry `changelog.money.columns`: "Each money column `<name>` is replaced by `<name>_minor` and `<name>_currency`." (Task 17 replaces the column instead of renaming it; see its resolution note.)

Section D, "Gallery" (the design-system gallery: the Money section of the Dates, numbers and names page, and the new `field-money` component on the Form page):

| id | draft |
|---|---|
| `gallery.money.title` | Money (existing title, shown for context) |
| `gallery.money.body` | Store each amount as a whole number of the currency's smallest unit, such as cents, with its currency code beside it. Show it with `money`, which writes it the way the reader's language does. The currency always comes from your data, never from the reader. |
| `gallery.field_money.blurb` | A money amount in one currency. The currency code shows beside the label and is sent with the form, so a currency that changed while someone was typing is caught on save. Pair it with form.Field's Currency on the server. |
| `gallery.field_money.label` | Ticket price |
| `gallery.field_money.hint` | Without the currency sign. |

The `field-money` states reuse the gallery's existing state names ("Bare", "With a hint", "With an error"), and its error state shows the approved English of `rastrillo.ui.money_decimal_point`.

- [ ] **Step 2: Build previews for the screen copy**

Sections A and D appear on screens, so the reviewer must see them in place (the skill's step 1b). Build `copy-review/preview/` as static HTML styled with `ui/tokens.css` and the `day` theme:
- `field-money.html`: the `field-money` control as Task 8 will render it (copy `field-text`'s markup and add the currency code in the label), once per Section A message, each under the field it belongs to, in a `<form rst-form>` with `form-foot`. Each string's `context` says "Shown under a money field when …".
- `gallery-money.html`: the Dates, numbers and names page's Money section with the new body and three formatted amounts (`€1,284.50`, `¥1,285`, `KWD 1,284.500`), and the Form page's `field-money` section with its blurb, label and hint. Snapshot the current gallery page (`go generate ./...` renders `.design-system/`) and edit the two sections in place, so the surroundings are real.

Sections B to C are plain text with no page; the review is the list. Section C (the changelog) is Markdown: give it a rendered preview (the skill renders docs).

- [ ] **Step 3: Run the review**

Invoke the `copy-review` skill on `copy-review/strings.json` with the previews. Wait for Paul's edits. Apply them verbatim. Do not proceed until he has finished.

- [ ] **Step 4: Record the approved copy in the spec**

Append to the spec, after Appendix B:

```markdown
## Appendix C: Approved copy

Approved by Paul in the copy review of <date>. Code uses these strings
verbatim; the ids are the contract. Section A's English is each
catalog key's value; its eleven translations are drafted afterwards and
are not reviewed.

### C.1 Form errors
| id | English (approved) |
|---|---|
| ... one row per Section A entry ... |

### C.2 Operator messages
| id | English (approved) |
|---|---|
| ... |

### C.3 Changelog
...

### C.4 Gallery
| id | English (approved) |
|---|---|
| ... |

### C.5 Command-line help and package docs
| id | English (approved) |
|---|---|
| ... |

### C.6 Comments in files written into an app
| id | English (approved) |
|---|---|
| ... |

### C.7 The scaffold's AGENTS.md
| id | English (approved) |
|---|---|
| ... |
```

Fill every row from the review's output. Also add one line at the top of §6: "Approved; see Appendix C.1."

- [ ] **Step 5: Remove the scratch and commit**

```bash
rm -rf copy-review
git add docs/superpowers/specs/2026-10-09-multi-currency-money-design.md
git commit -F - <<'MSG'
Record the approved copy for multi-currency money

The catalog English and the gallery English are translation keys, so a
word changed after code is written costs eleven redrafted translations.
Every string the implementation writes is reviewed once, here, and the
code tasks quote this appendix by id instead of drafting their own.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 2: The `locales` core: generated CLDR data and the storage set in the root module

Spec: §1 (storage rule), §2 (packages), §3 ("What a pack holds", the two parent maps), Appendix A.3, A.4.
Starts from: `hack/moneycldr/table/table.go`, `hack/moneycldr/table/core_gen.go`, `hack/moneycldr/packs/*`, `hack/moneycldr/proto/proto_test.go:147-153` (`TestExponentsAreMinorUnits`).

This task **copies** the committed generated output into the root module and gives `cmd/gen` a second output that writes the same files there. The hack copies stay until Task 5, so the prototype and its tests keep running unchanged while the real packages are built; Task 5 deletes them.

**Files:**
- Create: `locales/data.go` (from `hack/moneycldr/table/table.go`)
- Create: `locales/core_gen.go` (from `hack/moneycldr/table/core_gen.go`)
- Create: `locales/LICENSE-UNICODE` (copy of `hack/moneycldr/LICENSE-UNICODE`)
- Create: `locales/<lang>/pack_gen.go` for all 101 packs, `locales/modern/modern_gen.go`
- Create: `locales/doc.go`
- Create: `locales/storage_test.go`, `locales/data_test.go`
- Modify: `hack/moneycldr/cmd/gen/main.go` (second output, `-root` flag)
- Modify: `Makefile` (`GORM_FREE` gains `./locales`; `money-cldr`'s diff covers `locales`)

**Interfaces:**
- Consumes: nothing new.
- Produces (package `amadan.net/rastrillo/rastrillo/locales`):
  - `type Locale struct { Parent, CurrencyDecimal, CurrencyGroup, Decimal, Minus, Digits, MinGrouping, Standard, Alpha, Spacing string; Symbols map[string]string; Narrow map[string]string; Overrides map[string]Override }` (fields and comments exactly as `table.Locale` at spec commit `f69ef958` or later: `Narrow` holds CLDR's narrow symbols, which formatting never uses and Task 4's normalising parser accepts)
  - `type Override struct { Pattern, Decimal, Group string }`
  - `type Currency struct { Exponent int; Eligible bool }`
  - `const CLDRVersion = "48.0.0"`
  - `func Register(pack string, locs map[string]Locale)` (packs call it from `init`)
  - `func Linked(id string) bool`, `func LinkedPacks() []string`, `func LinkedIDs() []string`
  - `func Data(id string) (Locale, bool)`: `table.Resolve` renamed, because Task 3's `Resolve(tag)` is the tag resolver and the two must not share a name
  - `func Merge(child, parent Locale) Locale`
  - `func Storage(code string) (Currency, bool)`: exact, upper-case lookup in the storage set
  - `func StorageCodes() []string`: sorted
  - `func DigitSets() []string`: a copy of every shipped digit set
  - `func PackOf(id string) (string, bool)`: the pack carrying a shipped locale id, linked or not
  - `func Parent(id string) string`: CLDR fallback parent, `"root"` at the top
  - Unexported generated tables, used by Task 3 and Task 4 inside the package: `root Locale`, `index`, `parents`, `aliases`, `likelyScript map[string]string`, `allDigits []string`, `currencies map[string]Currency`

The generated tables become unexported. In the prototype they were exported maps; in a framework's public API an exported, mutable storage set is a way for any imported package to change the exponent of every stored amount at init time. Accessors return copies or values.

- [ ] **Step 1: Write the failing tests**

Generate the hand-edited copy once from the committed TSV, then never regenerate it (that is the point: it is the second, independent copy the spec asks for):

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies
awk -F'\t' '!/^#/ && ($5=="eligible"||$5=="stored") {printf "\t\"%s\": %s,\n", $1, $2}' hack/moneycldr/testdata/storage.tsv > "$TMPDIR/frozen.txt"
wc -l "$TMPDIR/frozen.txt"   # expect 174
```

`locales/storage_test.go`, with the 174 lines from `$TMPDIR/frozen.txt` pasted into `frozen`:

```go
package locales

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/money"
)

// frozen is a second, hand-maintained copy of the storage set's codes
// and exponents. The generated set and money.MinorUnits could drift
// together (one regeneration run against a changed money module); this
// copy cannot, so removing a code or changing an exponent fails here
// and needs a human to edit both. Codes are only ever added: an
// amount stored under a code must stay readable at the same scale
// forever (spec §1, "The storage set only grows").
var frozen = map[string]int{
	// 174 lines pasted from $TMPDIR/frozen.txt, e.g.:
	// "AED": 2,
	// "JPY": 0,
	// "KWD": 3,
	// "MGA": 0,
	// "SLL": 2,
}

func TestStorageSetIsFrozen(t *testing.T) {
	for code, exp := range frozen {
		c, ok := Storage(code)
		if !ok {
			t.Errorf("%s was removed from the storage set; stored amounts in it would become unreadable", code)
			continue
		}
		if c.Exponent != exp {
			t.Errorf("%s exponent changed from %d to %d; every stored %s amount would change value", code, exp, c.Exponent, code)
		}
	}
	for _, code := range StorageCodes() {
		if _, ok := frozen[code]; !ok {
			t.Errorf("%s is new in the storage set: add it to frozen by hand, after checking its pinned ISO edition", code)
		}
	}
}

func TestExponentsAreMinorUnits(t *testing.T) {
	for _, code := range StorageCodes() {
		c, _ := Storage(code)
		if c.Exponent != money.MinorUnits(code) {
			t.Errorf("%s: storage exponent %d, money.MinorUnits %d", code, c.Exponent, money.MinorUnits(code))
		}
	}
}

func TestStorageExpectedCodes(t *testing.T) {
	for code, want := range map[string]Currency{
		"SLL": {2, false}, // historical amounts are real: stored, not a new default
		"HRK": {2, false},
		"MGA": {0, true},  // the one reviewed deviation: money stores whole ariary
		"CHE": {2, false}, // fund code: stored only
		"USD": {2, true},
		"JPY": {0, true},
		"KWD": {3, true},
	} {
		if got, ok := Storage(code); !ok || got != want {
			t.Errorf("Storage(%s) = %+v, %v; want %+v", code, got, ok, want)
		}
	}
	for _, code := range []string{"BYR", "ITL", "CLF", "UYW", "KDW", "usd", ""} {
		if _, ok := Storage(code); ok {
			t.Errorf("Storage(%q) accepted a code outside the storage set", code)
		}
	}
	if n := len(StorageCodes()); n != 174 {
		t.Errorf("storage set has %d codes, want 174 (153 eligible + 21 stored only)", n)
	}
}
```

`locales/data_test.go`:

```go
package locales_test

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
)

// Every shipped locale must rebuild from its data parent inside its
// own pack: a pack that needed another to rebuild would format
// wrongly, not fail, when only it is linked (spec §3, "Two parent maps").
func TestEveryLinkedLocaleRebuilds(t *testing.T) {
	ids := locales.LinkedIDs()
	if len(ids) != 429 {
		t.Fatalf("modern links %d locales, want 429", len(ids))
	}
	for _, id := range ids {
		l, ok := locales.Data(id)
		if !ok {
			t.Errorf("%s: no data", id)
			continue
		}
		for name, v := range map[string]string{"CurrencyDecimal": l.CurrencyDecimal, "CurrencyGroup": l.CurrencyGroup,
			"Decimal": l.Decimal, "Minus": l.Minus, "Digits": l.Digits, "MinGrouping": l.MinGrouping,
			"Standard": l.Standard, "Alpha": l.Alpha, "Spacing": l.Spacing} {
			if v == "" {
				t.Errorf("%s: %s is empty after merging", id, name)
			}
		}
	}
	if got := len(locales.LinkedPacks()); got != 101 {
		t.Errorf("modern links %d packs, want 101", got)
	}
	if p, ok := locales.PackOf("de-CH"); !ok || p != "de" {
		t.Errorf("PackOf(de-CH) = %q, %v", p, ok)
	}
	if got := locales.Parent("hi-Latn"); got != "en-IN" {
		t.Errorf("Parent(hi-Latn) = %q, want en-IN (a fallback parent that crosses packs)", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./locales/ -count=1`
Expected: FAIL to build: `package amadan.net/rastrillo/rastrillo/locales: no non-test Go files` or `undefined: Storage`.

- [ ] **Step 3: Copy and adapt the generated data**

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies
H=hack/moneycldr
cp $H/table/core_gen.go locales/core_gen.go
cp $H/table/table.go locales/data.go
cp $H/LICENSE-UNICODE locales/LICENSE-UNICODE
for d in $H/packs/*/; do p=$(basename "$d"); mkdir -p "locales/$p"; cp "$d"*.go "locales/$p/"; done
# package clause, import paths and the registry's name
sed -i 's/^package table$/package locales/' locales/core_gen.go locales/data.go
find locales -name '*_gen.go' -path 'locales/*/*' -exec sed -i \
  -e 's#amadan.net/rastrillo/rastrillo/hack/moneycldr/table#amadan.net/rastrillo/rastrillo/locales#' \
  -e 's#amadan.net/rastrillo/rastrillo/hack/moneycldr/packs/#amadan.net/rastrillo/rastrillo/locales/#' \
  -e 's/\btable\.\(Register\|Locale\|Override\)\b/locales.\1/g' \
  -e 's/^var locales = map/var data = map/' -e 's/Register(\("[^"]*"\), locales)/Register(\1, data)/' {} +
# unexport the generated tables (core_gen.go and data.go only)
sed -i -e 's/\bRoot\b/root/g' -e 's/\bIndex\b/index/g' -e 's/\bParents\b/parents/g' \
  -e 's/\bAllDigits\b/allDigits/g' -e 's/\bCurrencies\b/currencies/g' \
  -e 's/\bAliases\b/aliases/g' -e 's/\bLikelyScript\b/likelyScript/g' locales/core_gen.go locales/data.go
gofmt -l locales
go build ./locales/...   # every pack must compile: see the note below
```

Each prototype pack declares its records as `var locales = map[string]table.Locale{…}` (`hack/moneycldr/packs/de/pack_gen.go:16`). Once the import is the package `locales`, a package-level `var locales` in the same file would collide with the import name and no pack would compile, so the sed renames the variable to `data` (and its use in `init`).

Then edit by hand:
- In every generated header comment, replace "(LICENSE-UNICODE at the module root)" with "(locales/LICENSE-UNICODE)". The header spans two comment lines; do it in `cmd/gen` (Step 4) and here identically.
- `locales/data.go`: rename `func Resolve(id string) (Locale, bool)` to `Data`, including its recursive call; rename `linked`/`packs` stay unexported. The `root` rename made `if id == "root" { return Root, true }` read `return root, true`; check that the string literal `"root"` was not touched (the sed replaced only the identifier `Root`, so confirm with `grep -n '"root"' locales/data.go`). Replace the package comment with `locales/doc.go`'s.
- Add the accessors to `locales/data.go`:

```go
// Storage reports the storage set's entry for code, which must already
// be upper-case: callers normalise once, through currency.Normalize, so
// a lower-case lookup here is a bug to surface, not to forgive.
func Storage(code string) (Currency, bool) {
	c, ok := currencies[code]
	return c, ok
}

// StorageCodes lists the storage set, sorted.
func StorageCodes() []string {
	out := make([]string, 0, len(currencies))
	for c := range currencies {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// DigitSets returns every shipped numbering system's digits, so a
// parser reads them whichever packs are linked. A copy: the slice is
// package state.
func DigitSets() []string { return append([]string(nil), allDigits...) }

// PackOf names the pack that carries a shipped locale id, linked or
// not, so a missing pack can be named in an error.
func PackOf(id string) (string, bool) {
	p, ok := index[id]
	return p, ok
}

// Parent is CLDR's fallback parent for a shipped id, "root" at the top.
// Unlike Locale.Parent (the data parent, always inside the same pack),
// this crosses packs: hi-Latn falls back to en-IN.
func Parent(id string) string {
	if p, ok := parents[id]; ok {
		return p
	}
	return "root"
}
```

`locales/doc.go`, whose text is Appendix C.5's `pkgdoc.locales` (the draft below was what Task 1 reviewed; write the approved version):

```go
// Package locales is the locale-data core: the registry language packs
// add themselves to, CLDR's root locale, and the lookup tables every
// resolution needs (core_gen.go). A language's formatting data lives in
// its own pack, locales/<lang>, which an app links with a blank import.
// Go links only imported packages, so a language nobody imports costs
// nothing; locales/modern imports them all.
//
// The packs sit beside the framework's translated catalogs
// (locales/<code>.toml) so a later pack can carry its language's
// strings and number and date data in the same record, with no second
// registry and no second import per language.
//
// Generated by hack/moneycldr/cmd/gen from cldr-json 48.0.0 and pinned
// ISO 4217 editions; `make money-cldr` regenerates and fails on any
// difference. Data © Unicode, Inc., under the Unicode License v3
// (LICENSE-UNICODE in this directory).
package locales
```

- [ ] **Step 4: Teach `cmd/gen` to write the root copy**

In `hack/moneycldr/cmd/gen/main.go`, add `root := flag.String("root", "../../locales", <Appendix C.5 flag.gen.root>)`. The packs' and `modern`'s package doc lines are Appendix C.5's `pkgdoc.pack` and `pkgdoc.modern`: if the review changed them, change the generator's template and the copied files together. Wherever the generator renders `table/core_gen.go`, `packs/<lang>/pack_gen.go` and `packs/modern/modern_gen.go`, render a second copy into `*root` with exactly the substitutions Step 3 applied (package `locales`, import path `amadan.net/rastrillo/rastrillo/locales`, `locales.Register`/`locales.Locale`/`locales.Override`, the seven unexported table names, the licence wording). Implement it as one function `rootCopy(src []byte) []byte` applying those replacements plus the `var locales` to `var data` rename in packs (Step 3's note), used for every file, so the two outputs cannot diverge by more than that function. In the `Makefile`'s `money-cldr` target, change `git diff --exit-code -- table packs testdata` to `git diff --exit-code -- table packs testdata ../../locales`.

If network access is available, verify now (downloads about 45 MB of pinned CLDR and ISO files into `.build/cldr`; the hash manifest refuses anything else):

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies/hack/moneycldr
GOFLAGS=-mod=mod go run ./cmd/fetch -dir ../../.build/cldr && GOFLAGS=-mod=mod go run ./cmd/gen -dir ../../.build/cldr
git -C ../.. diff --exit-code -- locales hack/moneycldr/table hack/moneycldr/packs
```

Expected: no diff. If the network is unavailable, say so in the commit body; Task 5 runs `make money-cldr` and must pass.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./locales/... -count=1 && GOFLAGS=-mod=mod go vet ./locales/... && (cd hack/moneycldr && GOFLAGS=-mod=mod go vet ./...)`
Expected: PASS, no vet findings.

Add `./locales` to `GORM_FREE` in the `Makefile`, then run `make gorm-free`. Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add locales hack/moneycldr/cmd/gen/main.go Makefile
git commit -F - <<'MSG'
Add the locales core: CLDR money data and the frozen storage set

The currency formatter, the parser and boot's locale check all read
this data, so it lands first and on its own. The generated tables are
unexported: an exported storage-set map would let any package imported
by an app change the exponent of every stored amount at init.

The storage set is pinned twice, against money.MinorUnits and against a
hand-edited copy, so a regeneration that drops a code or moves an
exponent fails the build instead of silently re-scaling stored money.

cmd/gen writes this copy beside the prototype's until the harness is
repointed, so the oracle keeps running while the real packages are
built.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 3: Locale resolution, `CheckConfig`, `PacksFor` and the probe's logic

Spec: §3 (resolving a tag, steps 1-7; validating the configuration; the probe's output and exit codes), §12 ("Resolution", `proto/fallbacktest`, `proto/crosspacktest`), Appendix C.2 (`op.config.*`, `op.packs.*`, `op.probe.*`).
Starts from: `hack/moneycldr/proto/proto.go:64-212` (Resolution, app default, `Resolve`, `linkedFrom`, `PacksFor`), `:214-402` (`Config`, `CanonicalIdentity`, `CheckConfig`, `parseTag`, `isAlnum`), `:843-860` (`isAlpha`, `isDigits`), `hack/moneycldr/proto/probe.go`; tests `proto/proto_test.go:81-108`, `proto/fallbacktest/fallback_test.go`, `proto/crosspacktest/crosspack_test.go`.

**Files:**
- Create: `locales/resolve.go`, `locales/config.go`, `locales/probe.go`
- Create: `locales/resolve_test.go`, `locales/config_test.go`, `locales/probe_test.go`
- Create: `locales/localestest/gallery/doc.go`, `locales/localestest/gallery/gallery_test.go`
- Create: `locales/localestest/crosspack/doc.go`, `locales/localestest/crosspack/crosspack_test.go`

**Interfaces:**
- Consumes (Task 2): `Linked`, `LinkedPacks`, `Data`, `PackOf`, `Parent`, the unexported `index`, `aliases`, `likelyScript`, `parents`.
- Produces (package `locales`):
  - `type Resolution struct { ID, Wanted, MissingPack string; Malformed, UsedDefault bool }`
  - `func Resolve(tag string) Resolution`
  - `func SetDefault(id string)`: panics unless `Linked(id)`
  - `func SetDefaultForTest(id string) (restore func())`: no linked check; clears the installed configuration; `restore` puts the previous default and configuration back
  - `type Config struct { Default, DefaultFormat string; Offered []string; Formats map[string]string; Packs, LinkedPacks []string }`
  - `func CanonicalIdentity(code string) (string, error)`
  - `func CheckConfig(defaultLocale string, offered []string) (Config, error)`
  - `func PacksFor(offered []string) ([]string, error)`
  - `func Install(cfg Config)`: `SetDefault(cfg.DefaultFormat)` plus the identity-to-format map `FormatFor` reads; called once by `Serve` after `CheckConfig` succeeds
  - `func FormatFor(identity string) string`: the installed `Formats[identity]`; otherwise `Resolve(identity).ID` (which falls back to the installed default, or `"root"` with nothing installed)
  - `type ProbeReport struct` with JSON fields `rastrillo_check_config`, `ok`, `default`, `default_format`, `offered`, `formats`, `packs_required`, `packs_linked`, `problems` (as `proto.ProbeReport`)
  - `func Probe(env, defaultLocale string, offered []string, stdout, stderr io.Writer) (run bool, exit int)`

- [ ] **Step 1: Write the failing tests**

Port the prototype tests, changing `proto.` to the `locales` package (internal test files use bare names), `ResolveLocale(tag)` to `Resolve(tag).ID`, and `table.X` to the Task 2 accessors:
- `locales/resolve_test.go`, in package `locales_test` (an internal test file could not blank-import `locales/modern`, which imports `locales`), blank-importing `amadan.net/rastrillo/rastrillo/locales/modern`: `TestResolveLocale` from `proto_test.go:81-108` with every case unchanged.
- `locales/config_test.go` (package `locales_test`, modern linked): `TestPacksFor` from `fallbacktest/fallback_test.go:55-66` and `TestCheckConfig` from `:69-110` with every case except the two that need a pack to be missing (`{"de", nil, "needs the de pack"}` and `{"en", ["en", "fr-CH"], "needs the fr pack"}`): with every pack linked they cannot fail, so they move to the gallery package below. Also `TestCanonicalIdentityByRole` from `:149-178` unchanged.
- `locales/probe_test.go` (package `locales_test`, modern linked): only the new `TestProbeOutputFields` below, whose assertions do not depend on which packs are linked. `TestProbe` from `fallback_test.go:127-147` is ported into the gallery package below unchanged, because its expected `packs_linked` is exactly the twelve gallery packs and its missing-pack case needs `de` absent. Add to `probe_test.go`:

```go
func TestProbeOutputFields(t *testing.T) {
	var out, errOut strings.Builder
	run, code := locales.Probe("1", "en", nil, &out, &errOut)
	if !run || code != 0 {
		t.Fatalf("Probe(1) = %v, %d", run, code)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out.String()), &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "default default_format formats offered ok packs_linked packs_required problems rastrillo_check_config"
	if strings.Join(keys, " ") != want {
		t.Errorf("probe fields %v, want %s: doctor and scripts read these by name", keys, want)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("probe wrote %q; want exactly one JSON line on stdout", out.String())
	}
	if !strings.Contains(errOut.String(), "exited without serving") {
		t.Errorf("stderr %q does not say it exited without serving", errOut.String())
	}
	for _, env := range []string{"0", "true", "yes", " 1"} {
		if run, code := locales.Probe(env, "en", nil, io.Discard, io.Discard); !run || code != 2 {
			t.Errorf("Probe(%q) = %v, %d; want run and exit 2: only the exact value 1 checks", env, run, code)
		}
	}
}
```

- `locales/localestest/gallery/gallery_test.go`: `TestDefaultPacksOnly` (without its `Format` assertion, which Task 4 adds), `TestProbe` (unchanged), the two missing-pack `CheckConfig` cases moved out of `config_test.go` (as `TestCheckConfigNamesTheMissingPack`), and `TestDefaultFallback`'s `Resolve` half, from `fallback_test.go` (not `TestPacksFor`, which is `config_test.go`'s), blank-importing exactly the 12 packs `ar bn en es ga hi ja pt ru vi yue zh` from `amadan.net/rastrillo/rastrillo/locales/<lang>`. `doc.go` says: "Package gallery links only the twelve gallery packs, so its tests see what an app offering those languages sees: a de-AT request with no de pack names the missing pack and falls back."
- `locales/localestest/crosspack/crosspack_test.go`: `TestCrossPackFallback` without its `Format` comparison (Task 4 adds it), blank-importing `en`, `fr`, `no`; replace `table.Parents["hi-Latn"]` with `locales.Parent("hi-Latn")` and `table.Index["hi-Latn"]` with `locales.PackOf("hi-Latn")`.

Add the Review Focus 2 test to `locales/localestest/gallery/gallery_test.go`:

```go
// Review focus 2: a stored preference or a cookie names an identity the
// app does not offer. FormatFor must never answer root inside an app:
// the amount would be written in conventions the page around it does
// not use.
func TestFormatForUnofferedIdentity(t *testing.T) {
	cfg, err := locales.CheckConfig("en", []string{"en", "zh-TW"})
	if err != nil {
		t.Fatal(err)
	}
	restore := locales.SetDefaultForTest("")
	defer restore()
	locales.Install(cfg)
	for identity, want := range map[string]string{
		"en":    "en",
		"zh-TW": "zh-Hant", // offered: CheckConfig's format, not the identity
		"es-MX": "es-MX",   // not offered but linked: its own conventions
		"de-CH": "en",      // its pack is not linked: the app default, never root
		"xx":    "en",      // no pack carries it
		"":      "en",
	} {
		if got := locales.FormatFor(identity); got != want {
			t.Errorf("FormatFor(%q) = %q, want %q", identity, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./locales/... -count=1`
Expected: FAIL to build: `undefined: locales.Resolve` (and the other new names).

- [ ] **Step 3: Port the implementation**

Copy the line ranges named above into `resolve.go` (Resolution, appDefault, SetDefault, SetDefaultForTest, fallback, Resolve, linkedFrom, parseTag, isAlnum, isAlpha, isDigits), `config.go` (PacksFor, Config, CanonicalIdentity, CheckConfig) and `probe.go` (ProbeReport, Probe, nonNil). Then:
- `table.Index[x]` becomes `index[x]`, `table.Aliases` `aliases`, `table.LikelyScript` `likelyScript`, `table.Parents[id]` `Parent(id)`, `table.Linked` `Linked`, `table.LinkedPacks` `LinkedPacks`.
- Delete `ResolveLocale`; callers use `Resolve(tag).ID`.
- `SetDefaultForTest` returns a restore function:

```go
// SetDefaultForTest replaces the default without the linked check,
// clears any installed configuration, and returns a function that puts
// both back, so a test that installs a default or a configuration
// cannot leak it into the next test in the binary (the gallery tests
// expect root fallback when nothing is installed).
func SetDefaultForTest(id string) (restore func()) {
	mu.Lock()
	prevDefault, prevInstalled := appDefault, installed
	appDefault, installed = id, nil
	mu.Unlock()
	return func() {
		mu.Lock()
		appDefault, installed = prevDefault, prevInstalled
		mu.Unlock()
	}
}
```

- Guard `appDefault` and the installed formats with one `sync.RWMutex` (`mu`). The prototype used a bare package variable; `Serve` writes it at boot while tests and tools may read it from other goroutines, and `-race` would flag it.
- Add `Install` and `FormatFor`:

```go
var installed map[string]string // identity -> formatting locale, from CheckConfig

// Install makes an accepted configuration the process's: its default
// formats any tag that falls past every linked parent, and its Formats
// answer FormatFor. Serve calls it once, after CheckConfig accepts the
// configuration, so request-time formatting reads the same mapping boot
// validated (zh-TW formats as zh-Hant) instead of re-deriving it.
func Install(cfg Config) {
	SetDefault(cfg.DefaultFormat)
	m := make(map[string]string, len(cfg.Formats))
	for k, v := range cfg.Formats {
		m[k] = v
	}
	mu.Lock()
	installed = m
	mu.Unlock()
}

// FormatFor is the formatting locale for a request's identity: the
// installed mapping when the identity was offered, else whatever
// Resolve finds for it, which falls back to the installed default.
func FormatFor(identity string) string {
	mu.RLock()
	f, ok := installed[identity]
	mu.RUnlock()
	if ok {
		return f
	}
	return Resolve(identity).ID
}
```

- Every message string comes verbatim from Appendix C.2 (`op.config.*`, `op.packs.*`, `op.probe.bad_value`, `op.probe.done`). Where the approved text differs from the prototype's literal, the approved text wins; update any test substring that asserted the old wording, and say so in the commit body.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./locales/... -count=1 && CGO_ENABLED=1 GOFLAGS=-mod=mod go test -race ./locales/ -run 'TestCheckConfig|TestProbe' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add locales
git commit -F - <<'MSG'
Resolve locale tags and check an app's declared locales

Request tags stay forgiving and configuration stays strict: a
configured identity that only worked after repair would be routed,
stored and cached under its unrepaired spelling, so CheckConfig names
the canonical form and refuses. Past every linked parent a tag formats
in the app's installed default, as the page's words do; root is only
for code running without an app.

Install keeps boot's identity-to-format mapping for request time, so
zh-TW formats as zh-Hant without being resolved twice two different
ways.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 4: The `currency` package: format, seed, strict and normalising parse, rescale

Spec: §2 (`currency`'s role), §3 ("Formatting algorithm", "An unlinked pack at request time is never silent"), §4, §5 (both readings' shared rules), §5.1 (the normalising reading, decisions 22 and 23), §5.2 (strict reading), §12 ("Normalise" in the harness list), §12 (Format, Parse, `AmountFromAny`, `InputValue`, round trip), Appendix A.1, Appendix C.2 (`op.currency.missing_pack`).
Starts from: `hack/moneycldr/proto/normalize.go` (all; spec commit `5c1ffa39`) and `proto/normalize_test.go` (all seven tests); `hack/moneycldr/proto/proto.go:23-62` (errors, `NormalizeCurrency`, `EligibleDefault`), `:404-860` (`AmountFromAny` through `isDigits`, minus what Task 3 took), `hack/moneycldr/replay/replay.go:107-129` (`Rescale`); tests `proto/proto_test.go` (all but `TestResolveLocale`), the `Format` halves of `fallbacktest` and `crosspacktest`, `replay_test.go`'s rescale cases.

**Files:**
- Create: `currency/currency.go`, `currency/format.go`, `currency/parse.go`, `currency/normalize.go`
- Create: `currency/currency_test.go`, `currency/format_test.go`, `currency/parse_test.go`, `currency/roundtrip_test.go`, `currency/normalize_test.go` (the hand-written tables)
- Create: `currency/normalizeprop/doc.go`, `currency/normalizeprop/props_test.go` (the five property tests, which take about 34 s together on the harness, so they get their own package and time budget instead of pushing `currency` past its 10 s)
- Modify: `.rastrillo/budgets.txt` (`time currency/normalizeprop <measured, rounded up to the next 10s> the normalising parser's properties run over about 600,000 generated inputs in every shipped locale; a sample would no longer prove the stated ambiguity shape exactly`)
- Modify: `locales/localestest/gallery/gallery_test.go`, `locales/localestest/crosspack/crosspack_test.go` (add the `Format` assertions the prototype had)
- Modify: `Makefile` (`GORM_FREE` gains `./currency`)

**Interfaces:**
- Consumes (Tasks 2, 3): `locales.Data`, `locales.Storage`, `locales.DigitSets`, `locales.Resolve`, `locales.PackOf`.
- Produces (package `amadan.net/rastrillo/rastrillo/currency`):
  - errors: `ErrMalformedCurrency`, `ErrUnsupportedCurrency`, `ErrNotInteger`, `ErrOutOfRange`, `ErrInvalid`, `ErrPrecision`, `ErrDecimalComma`, `ErrDecimalPoint`, `ErrCurrencySign` (texts as the prototype's; the last five carry the catalog key's suffix, e.g. `"money_invalid"`, which `form` maps to keys)
  - `func Normalize(code string) (string, error)`: trim Unicode space, three ASCII letters, upper-case, storage set
  - `func EligibleDefault(code string) bool`
  - `func Exponent(code string) (int, error)`: normalises first
  - `func Resolve(tag string) locales.Resolution`: `locales.Resolve`, re-exported because the form contract validates a locale echo with `currency.Resolve(v).ID == v` (spec §5)
  - `func Format(locale string, minor int64, code string) (string, error)`
  - `func InputValue(locale string, minor int64, code string) (string, error)`
  - `func Clean(s string) string`
  - `func Parse(locale, s, code string) (int64, error)`: the strict reading (§5.2); a blank-after-`Clean` input returns `0, nil`; callers decide what blank means
  - `func ParseNormalized(locale, s, code string) (int64, error)`: the normalising reading (§5.1), the prototype's `proto.Normalize`. It is not called `Normalize` here, because `currency.Normalize` is the currency-code check (§2's API list); the pairing with `Parse` says which reading is which. Blank-after-`Clean` returns `0, nil`, as `Parse` does
  - `var ErrWrongCurrency` (`"money_wrong_currency"`): another currency's explicit spelling
  - `type Reading struct { Decimal string; Storable bool; Minor int64 }` and `type AmbiguousError struct { Readings []Reading }` with `func (e *AmbiguousError) Error() string` (`"money_ambiguous"`) and `func (e *AmbiguousError) Choices(locale, code string) (a, b string)`: the two choices the message shows, written as §5.1 says (ISO code, no grouping, the parsing locale's decimal mark, a zero added to a three-digit fraction)
  - `type Legacy struct { Currency string; Scale int }`: the manifest's `[legacy]` table, shared by `currency/replay` and `currency/sqlmoney`
  - `func Rescale(v int64, scale int, code string) (int64, error)`: exact or an error; never rounds
  - `func AmountFromAny(v any) (minor int64, present bool, err error)`

- [ ] **Step 1: Write the failing tests**

The normalising reading, from the prototype at the spec commit (`5c1ffa39`), in two places:
- `currency/normalize_test.go` (package `currency_test`, modern linked): `TestNormalizeExpected` and `TestSharedSymbols` verbatim, with `Normalize(` renamed `ParseNormalized(` and `proto.` dropped. Every expected value stays as written: they are the hand-written statements of intent (130 locale cases, 44 shared-symbol cases), including decision 23's `en` `1,000` USD = 100000, `de` `1.500` EUR = 150000 and `ja` `￥1,285` = 1285.
- `currency/normalizeprop/props_test.go` (package `normalizeprop`, modern linked): `TestNormalizeNeverOverridesStrict`, `TestNormalizeRedisplay`, `TestNormalizeReadsEveryDisplay`, `TestAmbiguousChoicesAreUnambiguous` and `TestAmbiguityHasExactlyTheStatedShape`, verbatim apart from the renames. Their corpus-coverage assertion (the corpus must reach every branch, spec §12) stays. If a helper they need is unexported in `currency` (the prototype's tests were in-package), export nothing for tests: copy the helper into the test package, since it only builds inputs.

Port from `hack/moneycldr/proto/proto_test.go`, in package `currency_test`, blank-importing `amadan.net/rastrillo/rastrillo/locales/modern`, renaming `NormalizeCurrency` to `Normalize`: `TestFormatExpected`, `TestNormalizeCurrency`, `TestAmountFromAny`, `TestParseExpected`, `TestInputValueExpected`, `TestRoundTripEveryLocale`. Every expected string stays exactly as written there (they were confirmed against ICU). Replace `TestExponentsAreMinorUnits` with:

```go
func TestExponent(t *testing.T) {
	for code, want := range map[string]int{"usd": 2, " JPY ": 0, "KWD": 3, "MGA": 0, "SLL": 2} {
		if got, err := currency.Exponent(code); err != nil || got != want {
			t.Errorf("Exponent(%q) = %d, %v; want %d", code, got, err, want)
		}
	}
	if _, err := currency.Exponent("KDW"); !errors.Is(err, currency.ErrUnsupportedCurrency) {
		t.Errorf("Exponent(KDW) = %v", err)
	}
}
```

Port the rescale cases from `hack/moneycldr/replay/replay_test.go` into `currency_test.go` as a direct test of `Rescale`, with every expected value an exact integer:

```go
func TestRescale(t *testing.T) {
	for _, c := range []struct {
		v     int64
		scale int
		code  string
		want  int64
		err   string
	}{
		{1250, 2, "USD", 1250, ""},
		{1200, 2, "JPY", 12, ""},
		{1250, 2, "JPY", 0, "1250 at scale 2 is 12.50, which JPY cannot hold"},
		{1250, 2, "KWD", 12500, ""},
		{1250, 2, "SLL", 1250, ""},
		{math.MaxInt64 / 5, 2, "KWD", 0, "is too large for KWD"},
		{-1250, 2, "KWD", -12500, ""},
		{0, 18, "JPY", 0, ""},
	} {
		got, err := currency.Rescale(c.v, c.scale, c.code)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("Rescale(%d, %d, %s) = %d, %v; want error %q", c.v, c.scale, c.code, got, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Rescale(%d, %d, %s) = %d, %v; want %d", c.v, c.scale, c.code, got, err, c.want)
		}
	}
}
```

The two error strings are Appendix C.2's `op.legacy.inexact` and `op.legacy.overflow` drafts filled in; if Task 1's review changed either, write the approved text filled in the same way. The JPY row asserts the whole message, so it also pins `decimalAt`.

Add the missing-pack warning test (Review Focus 2), in `locales/localestest/gallery/gallery_test.go` because that package lacks the `de` pack. The warning is once per process, and other tests in the same binary format `de-AT` too, so this test re-executes the test binary and asserts inside a process where nothing has formatted yet; no test-only reset is added to `currency`:

```go
// An unlinked pack at request time is never silent, and never noisy:
// one warning per missing pack per process, however many requests ask.
// Run in a fresh process: the once-per-process state is real state,
// and earlier tests in this binary have already consumed it.
func TestMissingPackWarnsOncePerPack(t *testing.T) {
	if os.Getenv("WARN_ONCE_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMissingPackWarnsOncePerPack$", "-test.count=1")
		cmd.Env = append(os.Environ(), "WARN_ONCE_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		return
	}
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	for i := 0; i < 3; i++ {
		currency.Format("de-AT", 128450, "EUR")
		currency.Format("de-CH", 128450, "CHF")
		currency.Format("fr-CH", 128450, "CHF")
	}
	if n := strings.Count(buf.String(), "the de pack"); n != 1 {
		t.Errorf("de warned %d times, want once:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "the fr pack"); n != 1 {
		t.Errorf("fr warned %d times, want once:\n%s", n, buf.String())
	}
}
```

Add back the `Format` assertions (where the prototype wrote `proto.SetDefault("en"); defer proto.SetDefaultForTest("")`, write `restore := locales.SetDefaultForTest("en"); defer restore()`): in `gallery_test.go`'s `TestDefaultPacksOnly`, `Format("de-AT", 128450, "EUR")` with no default installed is `"€\u00a01,284.50"`; in `TestDefaultFallback`, after `locales.SetDefault("en")` it is `"€1,284.50"`; in `crosspack_test.go`, `Format(tag, -128450, "EUR") == Format(parent, -128450, "EUR")` for each of the four walks. Write `"pack"` wording to match `op.currency.missing_pack`.

Add an escaping-relevant guard used later by Task 9:

```go
// Format's output goes straight into templates. It must never contain
// markup characters, whatever the locale's symbols are, so the only
// escaping it needs is html/template's ordinary text escaping.
func TestFormatHasNoMarkup(t *testing.T) {
	for _, id := range locales.LinkedIDs() {
		for _, code := range []string{"USD", "EUR", "JPY", "KWD", "SAR", "INR"} {
			s, err := currency.Format(id, -1234567850, code)
			if err != nil || strings.ContainsAny(s, "<>&\"") {
				t.Errorf("Format(%s, %s) = %q, %v", id, code, s, err)
			}
		}
	}
}
```

(`'` is a real CLDR group mark in some Swiss locales, so it is not in the list; `<`, `>`, `&` and `"` never are.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/... ./locales/localestest/... -count=1`
Expected: FAIL to build: `no non-test Go files` in `currency`.

- [ ] **Step 3: Port the implementation**

Port `hack/moneycldr/proto/normalize.go` into `currency/normalize.go` with `Normalize` renamed `ParseNormalized`, `NormalizeCurrency` calls renamed `Normalize`, `table.` references replaced as below, and its comments kept (each names the spec rule or the review finding it implements). It needs the packs' narrow symbols, `locales.Locale.Narrow` (Task 2 copies them). Port `proto.go`'s `resolvedCache` with `lookup`: merging copies every symbol map, which made each parse cost hundreds of map writes, and the resolved id already reflects the app default and the linked packs, which register only at init.


Copy the named ranges of `proto.go` into `currency.go` (errors, `Normalize`, `EligibleDefault`, `Exponent`, `Resolve`, `AmountFromAny`, `Legacy`, `Rescale` from `replay.go:107-129`), `format.go` (`resolved`, `lookup`, `moneyFmt`, `forCurrency`, `grouping`, `symbol`, `magnitude`, `splitDigits`, `localDigits`, `isSZ`, `format`, `Format`, `group`, `InputValue`) and `parse.go` (`bidi`, `Clean`, `foldSpace`, `Parse`, `hasPrefixMarker`, `hasSuffixMarker`, `validGroups`, `digitValue`, `isAlpha`, `isDigits`). Then:
- `table.Currencies[code]` becomes `locales.Storage(code)`; `table.Resolve(...)` becomes `locales.Data(...)`; `table.AllDigits` becomes `locales.DigitSets()` (read once into the package's `digitValue` map at init).
- `lookup(tag)` resolves through `locales.Resolve(tag)` and, when `MissingPack != ""`, warns once per pack:

```go
var warned sync.Map // pack -> struct{}

// warnMissing logs one warning per missing pack per process. A stored
// preference or explicit app code can still ask for a language whose
// pack the binary lacks; the currency never changes, only the
// conventions fall back, and the operator needs to hear about it once,
// not on every request.
func warnMissing(r locales.Resolution) {
	if r.MissingPack == "" {
		return
	}
	if _, seen := warned.LoadOrStore(r.MissingPack, struct{}{}); seen {
		return
	}
	slog.Warn(fmt.Sprintf(missingPackMsg, r.Wanted, r.MissingPack, r.ID, r.MissingPack))
}
```

  `missingPackMsg` is a `const` holding Appendix C.2's `op.currency.missing_pack` verbatim, with its four verbs in the order shown.

- Delete `FormatWithoutSideFlip`; keep `format(tag, minor, code, sideFlip bool)` with `Format` passing `true` (decision 6). Task 5 replaces the harness's use of it with a data check.
- In `Rescale`, replace the prototype's `errors.New("overflow")` and its inexact message with Appendix C.2's `op.legacy.overflow` and `op.legacy.inexact`. `op.legacy.inexact` names the decimal value: build it with `money.Decimal(v, ...)`-style formatting at `scale` places (write a small `decimalAt(v int64, scale int) string` helper: the digits of `|v|` with a point `scale` places from the right, zero-padded, and a leading `-` for negatives; `decimalAt(1250, 2) == "12.50"`). `TestRescale`'s JPY row already asserts the exact message.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/ ./locales/... -count=1 && GOFLAGS=-mod=mod go vet ./currency/`
Then `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/normalizeprop/ -count=1 -v` and note its time for the `budgets.txt` line.
Expected: PASS. `TestRoundTripEveryLocale` covers 429 locales; `currency` must stay under 10 s (the package budget); `normalizeprop` is held to its own `time` line.

Mutation checks (do not commit them; spec "What the evidence says" lists them): make `ParseNormalized` accept the first reading, drop decision 23's group precedence, and drop Indian grouping; each must fail at least one ported test. Record the results in the commit body. Add `./currency` to `GORM_FREE` and run `make gorm-free`.

- [ ] **Step 5: Commit**

```bash
git add currency locales/localestest Makefile .rastrillo/budgets.txt
git commit -F - <<'MSG'
Add the currency package: CLDR formatting, input seeds and both readings

Promoted from the prototype the ICU oracle checked row by row, with
its expected values unchanged, so the shipped formatter starts from
evidence rather than a re-derivation. Rescale is exact or an error: a
legacy integer that a currency cannot hold is refused, never rounded.

A missing pack is warned about once per pack per process: silent would
hide an unlinked language, and per-request would bury the log.

The normalising reading (decisions 22 and 23) accepts input with one
plausible reading and refuses disagreeing readings instead of picking
one; its hand-written tables and properties come across unchanged, the
properties in their own package so the 10 s package budget holds.
Mutation checks: <results>.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 5: Point the CLDR harness at the shipped packages and retire the prototype's copies

Spec: Appendix B, §"What the evidence says".
Starts from: `hack/moneycldr/cmd/{gen,check,packsize,xtext}/main.go`, `hack/moneycldr/replay/replay.go`, `hack/templatemem/{main.go,pages/pages.go,pages/*_test.go,entry/*.go}`.

After this task the ICU oracle checks the code apps run, not a copy, and there is one generated data set.

**Files:**
- Modify: `hack/moneycldr/cmd/gen/main.go` (root is the only output; the `-root` flag becomes the default and only target)
- Modify: `hack/moneycldr/cmd/check/main.go`, `cmd/check/exemptions.go`, `cmd/packsize/main.go`, `cmd/xtext/main.go`
- Modify: `hack/moneycldr/replay/replay.go` (until Task 16 deletes it)
- Modify: `hack/moneycldr/go.mod` (require the root module, `replace amadan.net/rastrillo/rastrillo => ../..`)
- Modify: `hack/templatemem/main.go`, `pages/pages.go`, `pages/pages_test.go`, `pages/integration_test.go`, `entry/entry.go`, `entry/entry_test.go`, `hack/templatemem/go.mod` (drop the moneycldr requirement)
- Modify: `hack/moneycldr/doc.go`, `Makefile` (`money-cldr`)
- Delete: `hack/moneycldr/table/`, `hack/moneycldr/packs/`, `hack/moneycldr/proto/` (including `normalize.go`, ported by Task 4, and `field.go`, which Task 8 ports from commit `c62d25e6`), `hack/moneycldr/LICENSE-UNICODE`

**Interfaces:**
- Consumes: Tasks 2, 3, 4's packages.
- Produces: nothing new for later tasks.

- [ ] **Step 1: Repoint every consumer**

Mechanical replacements, file by file:
- `proto.Format` and `proto.InputValue` and `proto.Parse` to `currency.*`; `proto.Resolve` to `currency.Resolve`; `proto.ResolveLocale(x)` to `currency.Resolve(x).ID`; `proto.NormalizeCurrency` to `currency.Normalize`; `proto.AmountFromAny` to `currency.AmountFromAny`; `proto.CheckConfig`/`proto.Probe`/`proto.SetDefault`/`proto.SetDefaultForTest` to `locales.*` (`SetDefaultForTest` now returns a restore func; call it).
- `table.Currencies` iteration to `locales.StorageCodes()` plus `locales.Storage(code)`; `table.Index[id]` to `locales.PackOf(id)`; `table.Parents[id]` to `locales.Parent(id)`; `table.LinkedIDs()` to `locales.LinkedIDs()`.
- Blank imports `hack/moneycldr/packs/<x>` to `amadan.net/rastrillo/rastrillo/locales/<x>`.
- `cmd/check`'s one `proto.FormatWithoutSideFlip` use: replace with a data check, because the shipped package deliberately has no second formatting path. For every linked id, read `locales.Data(id)`; if `Alpha != Standard` and the currency sign `¤` sits on a different side of the number in `Alpha` than in `Standard`, record the id. The check fails if any is recorded and prints Appendix C.2's `op.check.side_flip` with the recorded ids. Today none is recorded (spec: "All 22 locales whose alphaNextToNumber pattern moves the symbol to the other side are outside the modern set").
- `cmd/gen`: write only the root output; delete the hack output paths and `rootCopy`'s identity case. The generator still imports types: `table.Locale`, `table.Override`, `table.Currency` become `locales.*`; the rebuild self-check (`main.go:99-112`: `table.Root = rootData`, `table.Register`, `table.Resolve`) becomes a local function over the generator's own fresh data, `rebuild(id string, byID map[string]locales.Locale, rootData locales.Locale) locales.Locale`, which walks `Parent` up to `"root"` and folds with `locales.Merge`. It must use the `rootData` it just read from CLDR, never `locales.Data("root")`: that is the previous generation compiled into the generator, so a changed root would be checked against stale data, and the generator would depend on the output it is writing. The generator imports `locales` only for the types and the pure `Merge`; it must not call `Register` (that would also conflict with any linked pack). `emitPack` writes `var data = map[string]locales.Locale{` and `Register(%q, data)`, the Task 2 shape.
- `Makefile` `money-cldr`: the regen diff becomes `git diff --exit-code -- ../../locales testdata`.

- [ ] **Step 2: Delete the copies**

```bash
git rm -r hack/moneycldr/table hack/moneycldr/packs hack/moneycldr/proto hack/moneycldr/LICENSE-UNICODE
```

Update `hack/moneycldr/doc.go`: the generator now writes `locales/` in the root module, `proto` is gone ("the shipped `currency` and `locales` packages are what the oracle checks").

- [ ] **Step 3: Verify**

Run:
```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies
(cd hack/moneycldr && GOFLAGS=-mod=mod go vet ./... && GOFLAGS=-mod=mod go test ./... -count=1)
(cd hack/templatemem && GOFLAGS=-mod=mod go vet ./... && CGO_ENABLED=1 GOFLAGS=-mod=mod go test -race -count=1 ./pages ./entry ./codemod ./cmd/migrate-templates && GOFLAGS=-mod=mod go test -count=1 ./upgradeprobe)
make money-cldr
```
Expected: all PASS. `make money-cldr` downloads about 45 MB on first run, needs Node, and must end with the oracle's completion line, 0 unexplained differences, 0 round-trip failures and no diff under `locales/`. If the network or Node is unavailable, stop and report it: this check is the evidence that the shipped formatter equals the one the spec measured, and later tasks build on it.

- [ ] **Step 4: Commit**

```bash
git add -A hack Makefile
git commit -F - <<'MSG'
Check the shipped currency package against ICU, not a prototype

The oracle measured a prototype; after promotion it must measure what
apps run, or a porting slip would pass every harness check. One
generated data set remains, in the root module, regenerated and
diffed by make money-cldr.

The side-flip comparison becomes a data check: the shipped package
has one formatting path by design, and the check only needs to know
whether any modern locale's patterns move the symbol.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 6: `manifest/app.toml`, the generated pack imports, and the declared-catalog check

Spec: §3 ("Declaring what an app offers", "New apps start with one locale", "Adding a language is one line", "`app.toml` is a reserved name", "The generated test"), §12 item 7 ("`generate --check` fails when the packs file and `app.toml` disagree or a catalog is missing"), Appendix C.2 (`op.packs.*`, `op.generate.reserved_app`, `op.generate.default_locale_removed`, `op.catalog.*`), C.6 (`gen.packs.*`, `scaffold.app_toml`).
Starts from: `hack/moneycldr/proto/fallbacktest/fallback_test.go:55-66` (`TestPacksFor`).

This lands before boot refuses an app without packs (Task 7), so every in-repo app already links its default locale's pack when that happens.

**Files:**
- Create: `internal/manifest/app.go`, `internal/manifest/app_test.go`
- Modify: `internal/manifest/manifest.go` (`Load` skips `app.toml`; a resource named `app` is refused)
- Create: `internal/generate/packs/packs.go`, `internal/generate/packs/packs_test.go`
- Modify: `cmd/rastrillo/generate.go` (write and check `gen/locales/packs.go`; the catalog checks take the default from `app.toml`; `--default-locale` is refused with `op.generate.default_locale_removed`), `cmd/rastrillo/main.go` (`usage.generate`)
- Create: `internal/generate/catalogs/catalogs.go`, `internal/generate/catalogs/catalogs_test.go` (the declared-catalog contract; a subpackage because `internal/generate` has 444 source lines of headroom and this needs none of its private helpers)
- Modify: `cmd/rastrillo/generate_test.go` (`TestGenerateCheckHonoursTheDefaultLocaleFlag` becomes `TestGenerateCheckTakesTheDefaultFromAppTOML`, same assertion with `default_locale = "fr"` in `manifest/app.toml`; a new test that the flag is refused with the approved message)
- Modify: `cmd/rastrillo/new.go` (write `manifest/app.toml`, `gen/locales/packs.go`, and blank-import `<module>/gen/locales` from `internal/<app>/app.go`)
- Create: `examples/{blog,tickets,notes,helloworld}/manifest/app.toml`
- Modify: `examples/helloworld/cmd/helloworld/main.go`, and each example's `cmd/<app>/main.go` to wire `Locales`/`DefaultLocale` from `gen/locales`
- Regenerate: `examples/*/gen/locales/packs.go`, `packs_test.go`

**Interfaces:**
- Consumes: `locales.PacksFor`, `locales.CanonicalIdentity` (Task 3).
- Produces:
  - `internal/manifest`: `type App struct { Locales []string \`toml:"locales"\`; DefaultLocale string \`toml:"default_locale"\` }`; `func LoadApp(dir string) (App, error)`: reads `<dir>/app.toml` strictly (unknown keys are errors); a missing file is `App{Locales: []string{"en"}, DefaultLocale: "en"}`; an empty `DefaultLocale` becomes `Locales[0]`
  - `internal/generate/packs`: `func Emit(genDir string, app manifest.App) ([]string, error)` writes `gen/locales/packs.go` and `gen/locales/packs_test.go`, returning the written paths (for `generate --check`'s diff)
  - `internal/generate/catalogs`: `func Check(appDir string, app manifest.App) ([]string, error)`: the problems with the declared catalogs, each an `op.catalog.missing` line naming its reason, plus the existing `MissingKeys`/`MissingFrameworkKeys` findings computed against `app.DefaultLocale`. An offered code needs `locales/<code>.toml` when the framework's own strings are not translated into it (it is not in `rastrillo.BaseLocales()`), or when the app has its own strings (a `gen/locales/en.toml` exists, or any `locales/*.toml`) and the code is not `en`, because app strings are authored in English. Used by `generate --check` here and by doctor in Task 11, so the two never disagree.
  - Every emitter's returned paths (here `packs.Emit`'s) are every file it emitted in this run, whether or not the bytes changed. Task 20's `Prune` removes generated files not in that set, so an emitter that reported only changed files would get its unchanged output deleted on the next run.
  - Generated `gen/locales` (package `locales`): `const DefaultLocale string`; `var Locales []string` (nil for one offered locale, the full list otherwise); unexported `offered []string` (always the full list)

`gen/locales/packs.go` for `locales = ["en", "zh-TW"]`, exactly (its comments are Appendix C.6's `gen.packs.*`; the drafts below are what Task 1 reviewed, so write the approved text):

```go
// Code generated by rastrillo generate; DO NOT EDIT.

// Package locales links the formatting packs manifest/app.toml's
// locales need. Wire DefaultLocale and Locales into rastrillo.Options.
package locales

import (
	_ "amadan.net/rastrillo/rastrillo/locales/en"
	_ "amadan.net/rastrillo/rastrillo/locales/zh"
)

// DefaultLocale is manifest/app.toml's default_locale.
const DefaultLocale = "en"

// Locales is manifest/app.toml's locales. It is nil when the app
// offers one locale, so a one-locale app gets no /xx/ prefixes and no
// switcher.
var Locales = []string{"en", "zh-TW"}

var offered = []string{"en", "zh-TW"}
```

With one locale, the `Locales` line reads `var Locales []string`. `gen/locales/packs_test.go`, exactly:

```go
// Code generated by rastrillo generate; DO NOT EDIT.

package locales

import (
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/locales"
)

// The packs this package imports are exactly the ones its offered
// locales need. generate --check proves this file matches
// manifest/app.toml; the RASTRILLO_CHECK_CONFIG probe proves main links
// this package.
func TestPacksMatchOfferedLocales(t *testing.T) {
	want, err := locales.PacksFor(append([]string{DefaultLocale}, offered...))
	if err != nil {
		t.Fatal(err)
	}
	if got := locales.LinkedPacks(); !slices.Equal(got, want) {
		t.Errorf("gen/locales links %v, manifest/app.toml needs %v: run rastrillo generate", got, want)
	}
}
```

When `gen/locales` already exists with a `locales.go` (the manifest catalog), `packs.go` sits beside it in the same package. If the package already has a `TestMain` or a test named `TestPacksMatchOfferedLocales`, there is no conflict today; keep the name.

- [ ] **Step 1: Write the failing tests**

`internal/manifest/app_test.go`:

```go
func TestLoadApp(t *testing.T) {
	dir := t.TempDir()
	app, err := LoadApp(dir)
	if err != nil || !slices.Equal(app.Locales, []string{"en"}) || app.DefaultLocale != "en" {
		t.Fatalf("no app.toml: %+v, %v; want en only", app, err)
	}
	write := func(s string) { os.WriteFile(filepath.Join(dir, "app.toml"), []byte(s), 0o644) }
	write("locales = [\"es\", \"en\"]\n")
	if app, err = LoadApp(dir); err != nil || app.DefaultLocale != "es" {
		t.Errorf("default from locales[0]: %+v, %v", app, err)
	}
	write("locales = [\"en\"]\ndefault_lang = \"en\"\n")
	if _, err = LoadApp(dir); err == nil || !strings.Contains(err.Error(), "default_lang") {
		t.Errorf("a misspelled key must be an error naming it, got %v", err)
	}
}

func TestLoadSkipsAppTOMLAndRefusesResourceApp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.toml"), []byte("locales = [\"en\"]\n"), 0o644)
	if rs, err := Load(dir, dir); err != nil || len(rs) != 0 {
		t.Fatalf("Load read app.toml as a resource: %v, %v", rs, err)
	}
	os.WriteFile(filepath.Join(dir, "things.toml"), []byte("name = \"app\"\nroute = \"/app\"\n[form]\nbasics = [{ name = \"Title\" }]\n"), 0o644)
	if _, err := Load(dir, dir); err == nil || !strings.Contains(err.Error(), `"app"`) {
		t.Errorf("a resource named app was accepted: %v", err)
	}
}
```

`internal/generate/packs/packs_test.go`:

```go
func TestEmitWritesDerivedPacks(t *testing.T) {
	gen := t.TempDir()
	if _, err := Emit(gen, manifest.App{Locales: []string{"en", "zh-Hans", "zh-Hant", "pt-PT", "yue"}, DefaultLocale: "en"}); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join(gen, "locales", "packs.go"))
	for _, p := range []string{"en", "pt", "yue", "zh"} {
		if !strings.Contains(string(src), `_ "amadan.net/rastrillo/rastrillo/locales/`+p+`"`) {
			t.Errorf("packs.go lacks pack %s:\n%s", p, src)
		}
	}
	if strings.Count(string(src), `_ "amadan.net`) != 4 {
		t.Errorf("packs.go imports something extra:\n%s", src)
	}
	if !strings.Contains(string(src), `var Locales = []string{"en", "zh-Hans", "zh-Hant", "pt-PT", "yue"}`) {
		t.Errorf("Locales not written as declared:\n%s", src)
	}
}

func TestEmitOneLocaleHasNoRouting(t *testing.T) {
	gen := t.TempDir()
	Emit(gen, manifest.App{Locales: []string{"en"}, DefaultLocale: "en"})
	src, _ := os.ReadFile(filepath.Join(gen, "locales", "packs.go"))
	if !strings.Contains(string(src), "var Locales []string\n") {
		t.Errorf("a one-locale app must leave Locales nil (no prefixes, no switcher):\n%s", src)
	}
}

func TestEmitRefusesBadCodes(t *testing.T) {
	for _, codes := range [][]string{{"en", "de--CH"}, {"en", "xx"}, {"en_US"}, {"en", "en"}} {
		if _, err := Emit(t.TempDir(), manifest.App{Locales: codes, DefaultLocale: codes[0]}); err == nil {
			t.Errorf("Emit(%v) accepted it", codes)
		}
	}
}
```

`Emit` validates every code with `locales.CanonicalIdentity`, refuses duplicates, requires the default to be offered, then calls `locales.PacksFor`; the errors are the Appendix C.2 strings. `internal/generate/packs` links no pack itself: `PacksFor` needs only the index (`locales.PackOf`), which the core always has, so the generator never needs the data.

`PacksFor` calls `Resolve`, which reads only `index`, `aliases` and `likelyScript`, so it needs no pack linked; this test package links none, which proves it.

Add to `cmd/rastrillo/generate_test.go` a test that `rastrillo generate --check` on a copy of `examples/tickets` fails after `manifest/app.toml` gains `"es"` without regenerating, naming `gen/locales/packs.go`.

`internal/generate/catalogs/catalogs_test.go` (package `catalogs`):

```go
func TestCheck(t *testing.T) {
	for name, c := range map[string]struct {
		app   manifest.App
		files map[string]string // path under the app -> content
		want  string            // substring of the joined problems; "" means none
	}{
		"one locale, no app strings": {manifest.App{Locales: []string{"en"}, DefaultLocale: "en"}, nil, ""},
		"framework language, no app strings": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"}, nil, ""},
		"framework language, app strings, no file": {manifest.App{Locales: []string{"en", "es"}, DefaultLocale: "en"},
			map[string]string{"gen/locales/en.toml": "\"resource.x.name\" = \"Things\"\n"}, "locales/es.toml"},
		"language the framework lacks": {manifest.App{Locales: []string{"en", "de"}, DefaultLocale: "en"}, nil, "locales/de.toml"},
		"Spanish default, file missing": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"gen/locales/en.toml": "\"resource.x.name\" = \"Things\"\n"}, "locales/es.toml"},
		"Spanish default, complete": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"gen/locales/en.toml": "\"resource.x.name\" = \"Things\"\n", "locales/es.toml": "\"resource.x.name\" = \"Cosas\"\n"}, ""},
		"Spanish default, en catalog behind": {manifest.App{Locales: []string{"es", "en"}, DefaultLocale: "es"},
			map[string]string{"locales/es.toml": "a = \"A\"\nb = \"B\"\n", "locales/en.toml": "a = \"A\"\n"}, "b"},
	} {
		dir := t.TempDir()
		for path, content := range c.files {
			os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755)
			os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644)
		}
		problems, err := Check(dir, c.app)
		got := strings.Join(problems, "\n")
		if err != nil || (c.want == "") != (got == "") || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, %v; want %q", name, got, err, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/manifest/ ./internal/generate/packs/ -count=1`
Expected: FAIL to build: `undefined: LoadApp`, `no non-test Go files` in `packs`.

- [ ] **Step 3: Implement**

- `LoadApp` with `toml.DecodeFile` and `md.Undecoded()` like `decodeTOML`.
- `Load`: skip `filepath.Base(path) == "app.toml"`; after `Validate`, refuse `s.resource.Name == "app"` with `op.generate.reserved_app`.
- `packs.Emit`: render the two files above with `go/format`; write with `os.WriteFile` only when content changed (reuse the pattern of `internal/generate/store.go:writeFileIfChanged`; it is unexported there, so copy its four lines).
- `internal/generate/catalogs/catalogs.go`: `Check` as specified, calling `generate.MissingKeys` and `generate.MissingFrameworkKeys` with `app.DefaultLocale`. `cmd/rastrillo/generate.go`: `--check` replaces its two catalog checks with one `catalogs.Check` call; the `--default-locale` flag stays defined only so that setting it returns `op.generate.default_locale_removed` (a removed flag would fail with Go's generic "flag provided but not defined", which names no way out). After loading, `app, err := manifest.LoadApp(filepath.Join(dir, "manifest"))`, then `packs.Emit(genDir, app)` on a normal run. Under `--check`, emit into the existing check temp dir and compare `locales/packs.go` and `locales/packs_test.go` byte for byte with `gen/`, failing with `op.doctor.packs_stale`'s wording. Keep the `cmd/rastrillo` change under 30 lines; the logic lives in `packs`.
- `cmd/rastrillo/new.go`: write `manifest/app.toml` as below, its comment being Appendix C.6's `scaffold.app_toml` wrapped at 72 columns

```toml
# The locales this app offers. Add a code to offer another language,
# then run rastrillo generate; with two or more, URLs gain /xx/
# prefixes and pages a language switcher.
locales = ["en"]
default_locale = "en"
```

  then call `packs.Emit(<app>/gen, manifest.App{Locales: []string{"en"}, DefaultLocale: "en"})`, and add `_ "<module>/gen/locales"` to the scaffolded `internal/<app>/app.go` imports with a comment whose text is Appendix C `scaffold.app_go_packs` (added by the second copy review, listed at the end of this plan; it says why the import is in the app package rather than in main: the app's tests must link the packs too, because boot refuses a configuration whose packs are missing) Main gains `Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale` in the `Options` literal it passes to `Resolve` (import `genlocales "<module>/gen/locales"`).
- Examples: add `manifest/app.toml` with `locales = ["en"]` and `default_locale = "en"` to blog, tickets, notes and helloworld (helloworld has no `manifest/`; create it with only `app.toml`). Run `make build-cli && for e in blog tickets notes helloworld; do .build/rastrillo generate examples/$e; done`. Wire `Locales`/`DefaultLocale` from `gen/locales` in each `cmd/<app>/main.go`, and make sure the package that builds the handler in tests imports `gen/locales` (tickets, notes and blog already do through `render.go`/`genrender.go`; helloworld imports it from `main.go`, which is fine because it has no handler tests; if `make example-helloworld` shows otherwise, add a blank import where its tests build the handler).

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/manifest/ ./internal/generate/... -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./cmd/rastrillo/ -run 'Generate|New' -count=1
go run ./cmd/rastrillo budget size
git add 'examples/*/gen' examples/*/manifest   # the regenerated tree is the baseline (Global Constraints)
make generate-check example-blog example-tickets example-notes example-helloworld scaffold-smoke
```
Expected: PASS; each example's generated `gen/locales/packs_test.go` passes inside `make example-*`.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest internal/generate/packs internal/generate/catalogs internal/generate/locales.go cmd/rastrillo examples
git commit -F - <<'MSG'
Declare offered locales in manifest/app.toml and link their packs

Offered codes are identities (routing, cookies, catalogs); packs are
derived from them and only ever derived, so zh-Hans and zh-Hant link
one zh pack and an app never names a pack by hand. A one-locale app
leaves Options.Locales nil, so it gets no /xx/ prefixes.

This lands before boot refuses a configuration whose packs are
missing, so every in-repo app already links what it declares. The
scaffold imports gen/locales from its app package rather than main so
its tests link the packs too.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 7: Boot validation and the `RASTRILLO_CHECK_CONFIG` probe in every entry point

Spec: §3 ("Validating the configuration at boot", "The configuration probe": where it runs, every entry point that touches the database guards, what it prints, how it exits, production behaviour), §12 items 9 and 12, Appendix C.2 (`op.guard`, `op.boot.refused`, `op.probe.*`).
Starts from: `hack/templatemem/entry/entry.go`, `entry/entry_test.go` (all three tests), `run.go:36-60`, `sidecar.go:35`.

**Files:**
- Create: `internal/checkconfig/guard.go`; `internal/checkconfig/guardtest/doc.go` and `internal/checkconfig/guardtest/guard_test.go` (the re-exec test lives in its own package so it can import `db` and the root package while `checkconfig` imports only the standard library)
- Create: `probe.go`, `probe_test.go` (root package)
- Modify: `run.go` (`Run` and `Resolve` probe first), `serve.go` (`Serve`, `Handler`, `OpenDB` guard first; `Handler` runs `CheckConfig` and `Install`), `db/db.go` (`Open` guards first)
- Create: `packs_test.go` in every framework test package whose tests build a handler (at least `.`, `auth`, `pow`, `perf`, `ui`, `harness`), each containing only the blank import `_ "amadan.net/rastrillo/rastrillo/locales/en"` and a comment saying boot now refuses an app that links no pack
- Delete: `hack/templatemem/entry/` (ported here; its tests would otherwise keep testing the prototype's dispatch, not `Run`'s)
- Modify: `Makefile` (`money-cldr`'s `hack/templatemem` line drops `./entry`)

**Interfaces:**
- Consumes: `locales.CheckConfig`, `locales.Install`, `locales.Probe`, `locales.FormatFor` (Task 3).
- Produces:
  - `internal/checkconfig`: `const Env = "RASTRILLO_CHECK_CONFIG"`; `func Guard(entry string)`: if `os.Getenv(Env) != ""`, print `op.guard` naming `entry` to stderr and `os.Exit(2)`
  - root: `func ProbeConfig(opts Options)`: runs the probe for `opts.DefaultLocale`/`opts.Locales`; with the variable set it prints, then exits the process; unset, it returns
  - root: `func FormatLocale(r *http.Request) string`: `locales.FormatFor(LocaleFrom(r))`, the formatting locale generated actions pass to `currency.Format`
  - unexported, for tests: `func probe(opts Options, getenv func(string) string, stdout, stderr io.Writer, exit func(int)) bool` and `func run(opts Options, args []string, getenv func(string) string, stdout, stderr io.Writer, exit func(int)) error`

- [ ] **Step 1: Write the failing tests**

`probe_test.go` (root, package `rastrillo`), ported from `hack/templatemem/entry/entry_test.go` onto the real `run`:

```go
// exited is what the test exit function panics with, so run stops at
// the point a real process would have exited.
type exited struct{}

// callRun runs run with the given environment and argv, recording the
// exit code instead of exiting. code is -1 when run returned without
// exiting.
func callRun(t *testing.T, opts Options, args []string, env map[string]string) (code int, stdout string) {
	t.Helper()
	code = -1
	var out strings.Builder
	func() {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(exited); !ok {
					panic(v)
				}
			}
		}()
		run(opts, args, func(k string) string { return env[k] }, &out, io.Discard, func(c int) { code = c; panic(exited{}) })
	}()
	return code, out.String()
}

// The probe is the first statement of Run, before the sidecar
// dispatch: a sidecar invocation reaches the app's Options.Sidecar work
// without passing Resolve or Serve, so a probe placed any later would
// let an inherited variable start background work against a live
// database (spec §3, confirmation round gap 1).
func TestProbeRunsBeforeSidecarDispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"serve"}, {"-db", "x.db"}, {"sidecar", "run"}} {
		opts := Options{
			Sidecar: func(context.Context) (time.Time, error) { t.Errorf("%v: Options.Sidecar ran under the probe", args); return time.Time{}, nil },
			Mux:     http.NewServeMux(),
		}
		code, out := callRun(t, opts, args, map[string]string{checkconfig.Env: "1"})
		if code != 0 || !strings.Contains(out, `"rastrillo_check_config":1`) {
			t.Errorf("%v: exit %d, stdout %q; want the report and exit 0", args, code, out)
		}
	}
}
```

Port `TestDispatchUnchangedWithoutProbe` (variable unset: `sidecar run` reaches the sidecar path, anything else reaches Resolve and Serve; give `Serve` a pre-closed listener through a test hook or assert on the error from `Serve` with an invalid `Addr`, whichever the existing `run_test.go` already does for `Run`) and add:

```go
func TestProbeValueOtherThanOneExitsTwo(t *testing.T) {
	code, out := callRun(t, Options{Mux: http.NewServeMux()}, nil, map[string]string{checkconfig.Env: "0"})
	if code != 2 || out != "" {
		t.Errorf("RASTRILLO_CHECK_CONFIG=0: exit %d, stdout %q; want 2 and nothing: a stray 0 is an error, not a silent check", code, out)
	}
}

func TestProbeReportsProblemsAndExitsOne(t *testing.T) {
	code, out := callRun(t, Options{Mux: http.NewServeMux(), DefaultLocale: "de--CH"}, nil, map[string]string{checkconfig.Env: "1"})
	if code != 1 || !strings.Contains(out, `"ok":false`) {
		t.Errorf("exit %d, %s", code, out)
	}
}
```

`internal/checkconfig/guard_test.go`, Review Focus 3, using the re-exec pattern because `Guard` really exits:

```go
// Every entry point that opens the database guards as its first
// statement. With the variable set there, the probe did not run first,
// so the entry point must exit 2 before creating or touching the file.
func TestGuardsRefuseBeforeOpening(t *testing.T) {
	if entry := os.Getenv("GUARD_HELPER"); entry != "" {
		path := os.Getenv("GUARD_DB")
		switch entry {
		case "db.Open":
			db.Open(path, slog.Default())
		case "rastrillo.OpenDB":
			rastrillo.OpenDB(path, nil)
		case "rastrillo.Handler":
			rastrillo.Handler(rastrillo.Options{DBPath: path, Mux: http.NewServeMux()})
		case "rastrillo.Serve":
			rastrillo.Serve(rastrillo.Options{DBPath: path, Mux: http.NewServeMux(), Addr: "127.0.0.1:0"})
		}
		os.Exit(0) // reaching here means the guard did not fire
	}
	for _, entry := range []string{"db.Open", "rastrillo.OpenDB", "rastrillo.Handler", "rastrillo.Serve"} {
		path := filepath.Join(t.TempDir(), "app.db")
		cmd := exec.Command(os.Args[0], "-test.run=^TestGuardsRefuseBeforeOpening$")
		cmd.Env = append(os.Environ(), "GUARD_HELPER="+entry, "GUARD_DB="+path, checkconfig.Env+"=1")
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("%s: err %v, output %s; want exit 2", entry, err, out)
		}
		if !strings.Contains(string(out), entry) || !strings.Contains(string(out), "ProbeConfig") {
			t.Errorf("%s: message %q must name the entry point and ProbeConfig", entry, out)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s created or touched %s before refusing", entry, path)
		}
	}
}
```

Put this test in a new package `internal/checkconfig/guardtest` (an external test package can import `db` and root without a cycle; `internal/checkconfig` itself must import only the standard library). Its directory needs a `doc.go` (`package guardtest`) and the test file blank-imports `locales/en`.

`serve_test.go` (root) gains:

```go
func TestHandlerRefusesEveryCheckConfigFailure(t *testing.T) {
	for name, opts := range map[string]Options{
		"no pack for the default":   {DefaultLocale: "de"},
		"default not offered":       {DefaultLocale: "es", Locales: []string{"en"}},
		"non-canonical identity":    {Locales: []string{"en", "pt-pt"}},
		"duplicate":                 {Locales: []string{"en", "en"}},
		"malformed":                 {Locales: []string{"en", "de--CH"}},
	} {
		opts.Mux = http.NewServeMux()
		if _, closeFn, err := Handler(opts); err == nil {
			closeFn()
			t.Errorf("%s: Handler accepted %+v", name, opts)
		} else if !strings.Contains(err.Error(), "refusing to start") {
			t.Errorf("%s: %v does not say it refused to start", name, err)
		}
	}
	for name, opts := range map[string]Options{
		"no manifest, nothing set": {},
		"DefaultLocale only":       {DefaultLocale: "en"},
	} {
		opts.Mux = http.NewServeMux()
		_, closeFn, err := Handler(opts)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		closeFn()
	}
}
```

The root test binary links only `locales/en` (its `packs_test.go`), so "no pack for the default" (`de`) fails as it would in an app.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . ./internal/checkconfig/... -run 'Probe|Guard|Refuses' -count=1`
Expected: FAIL to build (`undefined: run`, `undefined: checkconfig`).

- [ ] **Step 3: Implement**

- `internal/checkconfig/guard.go`: `Env`, `Guard(entry string)` as specified. The message is `op.guard`.
- `probe.go`: `probe(...)` calls `locales.Probe(getenv(checkconfig.Env), opts.DefaultLocale, opts.Locales, stdout, stderr)`; if it ran, `exit(code)` and return true. `ProbeConfig(opts)` is `probe(opts, os.Getenv, os.Stdout, os.Stderr, os.Exit)`. `FormatLocale` as specified.
- `run.go`: `Run(opts)` becomes `return run(opts, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, os.Exit)`; `run` probes first, then `isSidecarInvocation(args)`, then `resolveInvocation` and `Serve`. `Resolve(opts)` probes first (`probe(opts, os.Getenv, ...)`), then resolves. Comment on `run`: "The probe is first, before the sidecar dispatch: `sidecar run` reaches Options.Sidecar without passing Resolve or Serve (sidecar.go:44), so any later placement would let an inherited RASTRILLO_CHECK_CONFIG start background work against a live database."
- `serve.go`: `checkconfig.Guard("rastrillo.Serve")` first in `Serve`; `checkconfig.Guard("rastrillo.Handler")` first in `Handler`; `checkconfig.Guard("rastrillo.OpenDB")` first in `OpenDB`. In `Handler`, after the guard and the Mux/Router check and **before** opening the database: `cfg, err := locales.CheckConfig(opts.DefaultLocale, opts.Locales)`; on error return `fmt.Errorf(<op.boot.refused>, err)`; then `locales.Install(cfg)`. Comment: "Checked in the serving binary, because only it knows which packs are linked; before the database opens, so a misconfigured deploy changes nothing."
- `db/db.go`: `checkconfig.Guard("db.Open")` first in `Open`.
- Framework test packages: run `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... 2>&1 | grep -l 'needs the en pack'`-style triage: run `go test` for `.`, `auth`, `pow`, `perf`, `ui`, `harness`, `cmd/rastrillo` and any package that fails with the `op.config.missing_pack` message gets `packs_test.go`. Expected minimum set: `.`, `auth`, `pow`, `perf`, `ui`, `harness`.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test . ./db/ ./internal/checkconfig/... ./auth/ ./pow/ ./perf/ ./harness/ -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'Test[^B]' -count=1
make example-blog example-tickets example-notes example-helloworld scaffold-smoke
```
Expected: PASS. (`-run 'Test[^B]'` skips ui's browser tests while iterating; they run in Task 30.)

- [ ] **Step 5: Commit**

```bash
git add internal/checkconfig probe.go probe_test.go run.go serve.go serve_test.go db/db.go Makefile $(git ls-files -o --exclude-standard | grep packs_test.go)
git rm -r -q hack/templatemem/entry
git commit -F - <<'MSG'
Check locales at boot and probe them before anything opens

Serve refuses a configuration whose identities are not canonical or
whose packs are not linked, because only the serving binary knows what
it links. The probe is the first statement of Run (before the sidecar
dispatch), Resolve and ProbeConfig, so RASTRILLO_CHECK_CONFIG=1 reports
and exits before a database, migration or background job exists.

Every database entry point guards too: reaching one with the variable
set means an app opened its database before probing, and it exits 2
without touching the file rather than serving with a stray variable.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 8: The form's money contract (strict and normalising, the unchanged-seed check) and the `field-money` partial

Spec: §5 (all of it: the two readings, §5.3's field options, the unchanged-seed check and what a field shows, the per-field contract table with `<Name>.seed`, the locale check, the currency check, "The client value is never authoritative", "Without `LocaleEcho`"), decisions 22 and 23, §6 (including the two new keys), §12 items 4, 5, 14 and 15, Appendix C.1 (all ten keys), C.4 (`gallery.field_money.*`), and the second copy review's `rastrillo.ui.money_wrong_currency` and `rastrillo.ui.money_ambiguous` (listed at the end of this plan; this task cannot write them before that review).
Starts from: `hack/moneycldr/proto/field.go` and `proto/field_test.go` (`TestFieldStrictDispatch`, `TestFieldSeed`, `TestFieldSeedSurvivesEcho`, and `Field.Echo`, added by spec commit `c62d25e6`); Task 5 deleted `proto`, so read them with `git show c62d25e6:hack/moneycldr/proto/field.go` (and `…/field_test.go`); `form/fields.go:113-125` (today's Money branch), `form/money.go` (the `Error` type and its key/English pairing), `hack/moneycldr/proto/proto_test.go:180-235` (`TestParseExpected`, for the cases the form maps to keys).

`FormatCents`, `FormatCentsPlain`, `ParseCents`, `form.Money` with no `Currency`, and `Parsed.Cents` keep working unchanged (decision 4), and get no `Deprecated:` marker (decision 11).

**Files:**
- Create: `form/moneyfield.go`, `form/moneyfield_test.go`
- Modify: `form/fields.go` (the `Field` options; `Parse` dispatches a `Money` field with a `Currency` to the new path)
- Modify: `locales/en.toml` and the eleven other framework catalogs `locales/{ga,zh-Hans,es,hi,pt,bn,ru,ja,yue,vi,ar}.toml` (ten new keys: the eight from C.1, plus `money_wrong_currency` with its `{currency}` placeholder and `money_ambiguous` with `{a}` and `{b}`, from the second copy review)
- Create: `ui/partials/field-money.html`
- Modify: `ui/ui_test.go` (the per-partial golden table gains `field-money`), `ui/sizing_fixture_test.go` and any other "every partial" fixture that fails without it
- Create: `ui/field_money_browser_test.go` (build tag `browser`, like every `ui/*_browser_test.go`)
- Modify: `internal/designsystem/samples.go` (a `field-money` partial doc on the Form page), `internal/designsystem/prose.go` (its blurb, label and hint, with eleven translations)

**Interfaces:**
- Consumes: `currency.Normalize`, `currency.Clean`, `currency.Parse`, `currency.Resolve`, `currency.Exponent`, `currency.InputValue`, the `currency.Err*` values (Task 4).
- Produces (package `form`):
  - `Field` gains `Currency string` (a non-empty value turns a `Money` field into a currency-aware one), `Negative bool`, `Locale string` (the parsing locale when `LocaleEcho` is off), `LocaleEcho bool`, `Strict bool` (decision 22: false, the default, reads with `currency.ParseNormalized`; true reads with `currency.Parse`, today's exact format), and `Stored *int64` (on an edit form, the record's current amount, re-read by the handler before `Parse`; nil on create and for an empty field). `Stored` is what the unchanged-seed check compares against; the seed itself is never a value
  - `type MoneyInput struct { Value, Locale, Currency, Seed string }`: what a money input renders and echoes; `Seed` goes into the hidden `<Name>.seed`. On a failure re-render the echo carries the submitted seed when the text was that verified seed (equal after trimming, and the record still rendering to it), so an unchanged ambiguous value survives an error on another field (spec §5.3, `c62d25e6`); any other echo has an empty seed
  - `func SeedMoney(renderLocale string, minor *int64, code string, strict bool) (MoneyInput, error)`: `Value` is `""` for nil, else `currency.InputValue(renderLocale, *minor, code)`; `Seed` equals `Value` when `!strict` and `minor != nil`, and is empty otherwise (a strict reading of a seed is exact, so a strict field needs none)
  - `func (p *Parsed) ErrorArgs() map[string]map[string]any`: per field, the values its error message interpolates: `{"currency": "USD"}` for `money_wrong_currency`, `{"a": "1.5 USD", "b": "1500 USD"}` for `money_ambiguous` (from `AmbiguousError.Choices` in the parsing locale). A renderer passes them to `Tf` with the key; fields without arguments are absent
  - `func (p *Parsed) Minor(name string) (minor int64, present bool)`: `present` is false for a blank field and for a field with an error
  - `func (p *Parsed) MoneyInput(name, renderLocale string) (in MoneyInput, reseed bool)`: the re-render value for a submitted money field; `reseed` true means the locale metadata was rejected and the caller must seed from the server instead (blank on create, `SeedMoney` of the record on edit)
  - error keys, as constants in `form/moneyfield.go`: `moneyInvalidKey = "rastrillo.ui.money_invalid"`, `moneyPrecisionKey` (2 places), `moneyPrecision0Key`, `moneyPrecision3Key`, `moneyDecimalCommaKey`, `moneyDecimalPointKey`, `moneyCurrencyKey`, `moneyNegativeKey`, `moneyReenterKey`, `moneyCurrencyChangedKey`, `moneyWrongCurrencyKey`, `moneyAmbiguousKey`; a blank required field reports the existing `fieldRequiredKey`
  - `ui/partials/field-money.html`, define `"field-money"`, keys: `Name`, `Label`, `Value`, `Locale`, `Currency`, `Seed`, `Required`, `Hint`, `Error` (as `field-text`), rendering three hidden inputs `<Name>.locale`, `<Name>.currency` and `<Name>.seed` (the last always present, empty when `Seed` is), rendering one text input with `inputmode="decimal"`, the currency code in `<span class="rst-mono">` inside the label (no new CSS, so no vendored asset changes), and two hidden inputs `<Name>.locale` and `<Name>.currency`

- [ ] **Step 1: Write the failing tests**

`form/moneyfield_test.go` (package `form_test`, blank-imports `locales/en` and `locales/de`):

```go
func post(values url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

var price = form.Field{Name: "Price", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Required: true}

// Spec §12 item 5: round two's KWD scenario, on a Strict field, where
// the locale echo alone decides: an English form shows 1.500 (one and
// a half dinars); parsed in the locale that rendered it, it is 1500
// fils, and in German it is 1,500,000.
func TestLocaleEchoParsesInTheRenderingLocale(t *testing.T) {
	strict := price
	strict.Strict = true
	p := form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}}), strict)
	if m, ok := p.Minor("Price"); !p.OK() || !ok || m != 1500 {
		t.Fatalf("en echo: %d, %v, %v", m, ok, p.Errors())
	}
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"de"}, "Price.currency": {"KWD"}}), strict)
	if m, _ := p.Minor("Price"); m != 1500000 {
		t.Errorf("de echo: %d, want 1500000 (1.500 is fifteen hundred in German)", m)
	}
	// Normalising (the default): the same English text typed fresh has
	// two readings and is refused with both; German text is read the
	// page's way (decision 23).
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"en"}, "Price.currency": {"KWD"}}), price)
	if p.Errors()["Price"] != "rastrillo.ui.money_ambiguous" || fmt.Sprint(p.ErrorArgs()["Price"]) != "map[a:1.5 KWD b:1500 KWD]" {
		t.Errorf("normalising en 1.500 KWD: %v %v", p.Errors(), p.ErrorArgs())
	}
	p = form.Parse(post(url.Values{"Price": {"1.500"}, "Price.locale": {"de"}, "Price.currency": {"KWD"}}), price)
	if m, _ := p.Minor("Price"); m != 1500000 {
		t.Errorf("normalising de 1.500 KWD: %d, want 1500000", m)
	}
}

func TestLocaleEchoRejectedValuesReseed(t *testing.T) {
	for name, locs := range map[string][]string{
		"missing":       nil,
		"duplicated":    {"en", "en"},
		"malformed":     {"de--CH"},
		"non-canonical": {"EN"},
		"too long":      {strings.Repeat("a", 36)},
		"bad bytes":     {"en_US"},
	} {
		v := url.Values{"Price": {"12.500"}, "Price.currency": {"KWD"}}
		if locs != nil {
			v["Price.locale"] = locs
		}
		p := form.Parse(post(v), price)
		if p.Errors()["Price"] != "rastrillo.ui.money_reenter" {
			t.Errorf("%s: errors %v, want money_reenter", name, p.Errors())
		}
		if _, reseed := p.MoneyInput("Price", "en"); !reseed {
			t.Errorf("%s: a rejected locale must reseed from the server, not echo text of unknown meaning", name)
		}
	}
}

// Spec §12 item 4 and Review Focus 1. The currency echo is a
// consistency check only: equal changes nothing, different is
// rejected with the text kept, and the value never decides the
// currency.
func TestCurrencyEchoMismatchKeepsText(t *testing.T) {
	p := form.Parse(post(url.Values{"Price": {"12.50"}, "Price.locale": {"en"}, "Price.currency": {"USD"}}),
		form.Field{Name: "Price", Kind: form.Money, Currency: "JPY", LocaleEcho: true})
	if p.Errors()["Price"] != "rastrillo.ui.money_currency_changed" {
		t.Fatalf("errors %v", p.Errors())
	}
	if _, ok := p.Minor("Price"); ok {
		t.Error("a mismatched currency produced an amount")
	}
	in, reseed := p.MoneyInput("Price", "en")
	if reseed || in != (form.MoneyInput{Value: "12.50", Locale: "en", Currency: "JPY"}) {
		t.Errorf("MoneyInput = %+v, %v; want the typed text, its locale and the server's currency", in, reseed)
	}
	// Spec §12 item 4: a forged currency equal to the server's changes
	// nothing (after Normalize: trimmed, upper case); any other is
	// rejected. The server's currency is what Minor's amount is in.
	for forged, accepted := range map[string]bool{"EUR": true, " eur ": true, "USD": false, "usd": false, "XXX": false, "": false} {
		p := form.Parse(post(url.Values{"Price": {"12.50"}, "Price.locale": {"en"}, "Price.currency": {forged}}),
			form.Field{Name: "Price", Kind: form.Money, Currency: "EUR", LocaleEcho: true})
		m, ok := p.Minor("Price")
		if ok != accepted || accepted && m != 1250 {
			t.Errorf("forged %q: %d, accepted=%v; want accepted=%v (1250 when accepted)", forged, m, ok, accepted)
		}
		if !accepted && p.Errors()["Price"] != "rastrillo.ui.money_currency_changed" {
			t.Errorf("forged %q: error %q", forged, p.Errors()["Price"])
		}
	}
	// Blank text needs no currency echo: an empty optional field has neither.
	p = form.Parse(post(url.Values{"Price": {"  "}, "Price.locale": {"en"}}),
		form.Field{Name: "Price", Kind: form.Money, Currency: "EUR", LocaleEcho: true})
	if _, ok := p.Minor("Price"); ok || !p.OK() {
		t.Errorf("blank optional: present=%v errors=%v", ok, p.Errors())
	}
}

func TestMoneyErrorsAreKeys(t *testing.T) {
	for _, c := range []struct {
		code, locale, text string
		strict, negative   bool
		want               string
	}{
		// Strict: today's errors.
		{"JPY", "en", "12.5", true, false, "rastrillo.ui.money_precision_0"},
		{"USD", "en", "12.345", true, false, "rastrillo.ui.money_precision"},
		{"KWD", "en", "1.2345", true, false, "rastrillo.ui.money_precision_3"},
		{"EUR", "de", "1.2,50", true, false, "rastrillo.ui.money_decimal_comma"},
		{"USD", "en", "1,2.50", true, false, "rastrillo.ui.money_decimal_point"},
		{"USD", "en", "US$ 5 £", true, false, "rastrillo.ui.money_currency"},
		{"USD", "en", "five", true, false, "rastrillo.ui.money_currency"},
		{"USD", "en", "-5", true, false, "rastrillo.ui.money_negative"},
		{"USD", "en", "-5", true, true, ""},
		// Normalising (the default): spec §5.1's examples.
		{"USD", "en", "€5", false, false, "rastrillo.ui.money_wrong_currency"},
		{"CAD", "en", "US$5", false, false, "rastrillo.ui.money_wrong_currency"},
		{"USD", "en", "1.500", false, false, "rastrillo.ui.money_ambiguous"},
		{"USD", "en", "12.3456", false, false, "rastrillo.ui.money_precision"},
		{"USD", "en", "12,.50", false, false, "rastrillo.ui.money_decimal_point"},
		{"USD", "en", "(5)", false, false, "rastrillo.ui.money_negative"},
		{"USD", "en", "-(5)", false, false, "rastrillo.ui.money_invalid"},
		{"USD", "en", "5 dollars", false, false, "rastrillo.ui.money_invalid"},
		{"USD", "en", "US$ 1,284.5", false, false, ""},
		{"USD", "en", "\u200f \u00a0", false, false, "rastrillo.ui.field_required"}, // blank after Clean
	} {
		f := form.Field{Name: "A", Kind: form.Money, Currency: c.code, Locale: c.locale, Strict: c.strict, Negative: c.negative, Required: true}
		p := form.Parse(post(url.Values{"A": {c.text}}), f)
		if got := p.Errors()["A"]; got != c.want {
			t.Errorf("strict=%v %s %s %q: error %q, want %q", c.strict, c.code, c.locale, c.text, got, c.want)
		}
	}
	p := form.Parse(post(url.Values{"A": {"€5"}}), form.Field{Name: "A", Kind: form.Money, Currency: "USD", Locale: "en"})
	if fmt.Sprint(p.ErrorArgs()["A"]) != "map[currency:USD]" {
		t.Errorf("money_wrong_currency args %v; the message names the field's ISO code", p.ErrorArgs()["A"])
	}
}

// Spec §12 item 15 and the prototype's TestFieldStrictDispatch, through
// the field's whole POST path: the same posts with Strict set and unset.
func TestStrictAndNormalisingOnTheSamePosts(t *testing.T) {
	for _, c := range []struct {
		locale, text, code string
		strict             bool
		want               int64
		err                string // "" means accepted
	}{
		{"en", "US$ 5", "USD", true, 0, "rastrillo.ui.money_currency"},
		{"en", "US$ 5", "USD", false, 500, ""},
		{"en", "+5", "USD", true, 0, "rastrillo.ui.money_invalid"},
		{"en", "+5", "USD", false, 500, ""},
		{"en", "1.284,50", "USD", true, 0, "rastrillo.ui.money_decimal_point"},
		{"en", "1.284,50", "USD", false, 128450, ""},
		{"en", "€5", "USD", true, 0, "rastrillo.ui.money_currency"},
		{"en", "€5", "USD", false, 0, "rastrillo.ui.money_wrong_currency"},
		{"en", "1,500", "USD", true, 150000, ""},
		{"en", "12.5000", "USD", true, 0, "rastrillo.ui.money_precision"},
		{"en", "12.5000", "USD", false, 1250, ""},
		{"de", "1.500", "KWD", true, 1500000, ""},
		{"ar", "١٢\u066b٥٠", "SAR", true, 0, "rastrillo.ui.money_invalid"},
		{"ar", "١٢\u066b٥٠", "SAR", false, 1250, ""},
		{"en", "(5)", "USD", true, 0, "rastrillo.ui.money_invalid"},
		{"en", "-5", "USD", true, 0, "rastrillo.ui.money_negative"},
		{"en", "(5)", "USD", false, 0, "rastrillo.ui.money_negative"},
		{"en", "1.500", "KWD", false, 0, "rastrillo.ui.money_ambiguous"},
		{"de", "1.500", "KWD", false, 1500000, ""}, // decision 23
	} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: c.code, LocaleEcho: true, Strict: c.strict}
		p := form.Parse(post(url.Values{"P": {c.text}, "P.locale": {c.locale}, "P.currency": {c.code}}), f)
		m, _ := p.Minor("P")
		if got := p.Errors()["P"]; got != c.err || c.err == "" && m != c.want {
			t.Errorf("Strict=%v %s %q %s: %d, %q; want %d, %q", c.strict, c.locale, c.text, c.code, m, got, c.want, c.err)
		}
	}
	// A posted seed changes nothing on a Strict field: it renders none
	// and reads the text exactly.
	stored := int64(1500)
	f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Strict: true, Stored: &stored}
	p := form.Parse(post(url.Values{"P": {"9.999"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"9.999"}}), f)
	if m, ok := p.Minor("P"); !ok || m != 9999 {
		t.Errorf("strict field with a seed: %d, %v; want 9999", m, p.Errors())
	}
	if in, _ := form.SeedMoney("en", &stored, "KWD", true); in.Seed != "" {
		t.Errorf("a Strict field rendered a seed %q", in.Seed)
	}
}

// Spec §5.3: the unchanged-seed check. The seed is never a value: a
// match keeps the record's stored amount only if the record still
// renders to exactly that seed.
func TestUnchangedSeed(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	in, err := form.SeedMoney("en", i(1500), "KWD", false)
	if err != nil || in.Value != "1.500" || in.Seed != "1.500" {
		t.Fatalf("en seed %+v, %v", in, err)
	}
	if in, _ := form.SeedMoney("de", i(1500), "KWD", false); in.Seed != "1,500" {
		t.Fatalf("de seed %+v", in)
	}
	for _, c := range []struct {
		name                         string
		text, locale, currency, seed string
		stored                       *int64
		want                         int64
		err                          string
	}{
		{"unchanged, en", "1.500", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"unchanged, de", "1,500", "de", "KWD", "1,500", i(1500), 1500, ""},
		{"unchanged, trimmed", " 1.500\t", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"no seed: the rule applies", "1.500", "en", "KWD", "", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"edited to the same digits differently", "1.5", "en", "KWD", "1.500", i(1500), 1500, ""},
		{"edited, ambiguous", "2.500", "en", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"a bidi mark is not trimming", "1.500\u200f", "en", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_ambiguous"},
		{"tampered: seed forged to the text", "9.999", "en", "KWD", "9.999", i(1500), 0, "rastrillo.ui.money_reenter"},
		{"tampered: seed in another locale", "1,500", "en", "KWD", "1,500", i(1500), 0, "rastrillo.ui.money_reenter"},
		{"record changed since the form was drawn", "1.500", "en", "KWD", "1.500", i(2000), 0, "rastrillo.ui.money_reenter"},
		{"a seed on create was not ours", "1.500", "en", "KWD", "1.500", nil, 0, "rastrillo.ui.money_reenter"},
		{"any seed on create", "2", "en", "KWD", "1.500", nil, 0, "rastrillo.ui.money_reenter"},
		{"no seed on create: the rule applies", "2", "en", "KWD", "", nil, 2000, ""},
		{"currency echo still checked first", "1.500", "en", "USD", "1.500", i(1500), 0, "rastrillo.ui.money_currency_changed"},
		{"locale echo still checked first", "1.500", "EN", "KWD", "1.500", i(1500), 0, "rastrillo.ui.money_reenter"},
	} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Stored: c.stored}
		p := form.Parse(post(url.Values{"P": {c.text}, "P.locale": {c.locale}, "P.currency": {c.currency}, "P.seed": {c.seed}}), f)
		m, _ := p.Minor("P")
		if got := p.Errors()["P"]; got != c.err || c.err == "" && m != c.want {
			t.Errorf("%s: %d, %q; want %d, %q", c.name, m, got, c.want, c.err)
		}
	}
	// A stored refund survives an unchanged edit only where negatives
	// are allowed, as it would if the seed were parsed.
	for negative, want := range map[bool]string{false: "rastrillo.ui.money_negative", true: ""} {
		f := form.Field{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Negative: negative, Stored: i(-1500)}
		p := form.Parse(post(url.Values{"P": {"-1.500"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"-1.500"}}), f)
		if got := p.Errors()["P"]; got != want {
			t.Errorf("negative seed, Negative=%v: %q, want %q", negative, got, want)
		}
	}
}

// Spec §5.3 (c62d25e6) and the prototype's TestFieldSeedSurvivesEcho:
// an error on another field must not turn an unchanged ambiguous value
// into money_ambiguous on the second submission, and every submission
// re-verifies the seed against the current record.
func TestSeedSurvivesAFailureElsewhere(t *testing.T) {
	stored := int64(1500) // 1.500 KWD
	fields := func(st *int64) []form.Field {
		return []form.Field{{Name: "Name", Required: true},
			{Name: "P", Kind: form.Money, Currency: "KWD", LocaleEcho: true, Stored: st}}
	}
	first := url.Values{"Name": {""}, "P": {"1.500"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"1.500"}}
	p := form.Parse(post(first), fields(&stored)...)
	if p.Errors()["Name"] == "" || p.Errors()["P"] != "" {
		t.Fatalf("first submission: %v", p.Errors())
	}
	in, reseed := p.MoneyInput("P", "en")
	if reseed || in != (form.MoneyInput{Value: "1.500", Locale: "en", Currency: "KWD", Seed: "1.500"}) {
		t.Fatalf("echo %+v, %v; want the text, its locale and the verified seed", in, reseed)
	}
	second := url.Values{"Name": {"Fee"}, "P": {in.Value}, "P.locale": {in.Locale}, "P.currency": {in.Currency}, "P.seed": {in.Seed}}
	if m, ok := form.Parse(post(second), fields(&stored)...).Minor("P"); !ok || m != 1500 {
		t.Errorf("second submission: %d, %v; want 1500", m, ok)
	}
	changed := int64(2000)
	if got := form.Parse(post(second), fields(&changed)...).Errors()["P"]; got != "rastrillo.ui.money_reenter" {
		t.Errorf("second submission after the record changed: %q; want money_reenter", got)
	}
	// Edited text and a forged seed are echoed with no seed.
	for _, v := range []url.Values{
		{"Name": {""}, "P": {"2.500"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"1.500"}},
		{"Name": {""}, "P": {"9.999"}, "P.locale": {"en"}, "P.currency": {"KWD"}, "P.seed": {"9.999"}},
	} {
		if in, _ := form.Parse(post(v), fields(&stored)...).MoneyInput("P", "en"); in.Seed != "" {
			t.Errorf("text %q echoed seed %q", v.Get("P"), in.Seed)
		}
	}
}

func TestSeedMoney(t *testing.T) {
	m := int64(-128450)
	in, err := form.SeedMoney("de", &m, "EUR", false)
	if err != nil || in != (form.MoneyInput{Value: "-1284,50", Locale: "de", Currency: "EUR", Seed: "-1284,50"}) {
		t.Errorf("SeedMoney = %+v, %v", in, err)
	}
	if in, _ := form.SeedMoney("en", nil, "EUR", false); in.Value != "" || in.Seed != "" || in.Currency != "EUR" {
		t.Errorf("nil seed = %+v; an empty field shows the currency it will be saved in", in)
	}
}

// Decision 4: the cents API is untouched.
func TestCentsPathUnchanged(t *testing.T) {
	p := form.Parse(post(url.Values{"P": {"12.34"}}), form.Field{Name: "P", Kind: form.Money})
	if p.Cents("P") != 1234 || !p.OK() {
		t.Errorf("legacy Money: %d %v", p.Cents("P"), p.Errors())
	}
}
```

Adjust the two `money_currency` rows if `currency.Parse` maps "five" to `ErrInvalid` instead (it returns `ErrCurrencySign` for any letter, per `proto.go:713-714`; keep whatever the ported parser returns and assert that).

`ui/ui_test.go`: add `field-money` to `allPartials()` (`ui_test.go:946`, beside `field-text` at `:1028`), which `TestRenderEverythingSmoke` and the other every-partial tests range over, and add a `TestFieldMoneyPartial` that renders it data `{"Name": "Price", "Label": "Price", "Value": "12.50", "Locale": "en", "Currency": "EUR", "Seed": "12.50", "Required": true, "Error": "Enter a valid amount."}`, and assert the rendered HTML contains, exactly: `name="Price.locale" value="en"`, `name="Price.currency" value="EUR"`, `name="Price.seed" value="12.50"`, `inputmode="decimal"`, `<span class="rst-mono">EUR</span>`, `aria-invalid="true"`.

`ui/field_money_browser_test.go`, the spec's "`field-money` with JavaScript off" (§12 item 14). It uses the existing browser rig (`harness.New`, as `rowMenuRig` in `ui/rowmenu_browser_test.go` does), with script execution disabled in the engine:

```go
//go:build browser

// With no JavaScript, a real browser submits the text and the three
// hidden inputs exactly as rendered, the ISO code is on the page beside
// the input (spec §12 item 15), and the server reads the text in the
// rendering locale: an English page's "US$ 1,284.5" is 128450 cents.
func TestFieldMoneySubmitsWithNoScript(t *testing.T) {
	got := make(chan string, 1)
	rig := harness.New(t, func(string) http.Handler {
		mux := http.NewServeMux()
		tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
		tmpl = template.Must(tmpl.New("page").Parse(`<!doctype html><html lang="en"><body><form rst-form method="post" action="/save">{{template "field-money" dict "Name" "Price" "Label" "Price" "Value" "US$ 1,284.5" "Locale" "en" "Currency" "USD"}}<button id="save" type="submit">Save</button></form></body></html>`))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { tmpl.ExecuteTemplate(w, "page", nil) })
		mux.HandleFunc("POST /save", func(w http.ResponseWriter, r *http.Request) {
			p := form.Parse(r, form.Field{Name: "Price", Kind: form.Money, Currency: "USD", LocaleEcho: true, Required: true})
			m, ok := p.Minor("Price")
			got <- fmt.Sprintf("%d %v %v", m, ok, p.Errors())
			w.Write([]byte("ok"))
		})
		return mux
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var label string
	mustRun(t, ctx, emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#save", chromedp.ByQuery),
		chromedp.Text("label[for='Price']", &label, chromedp.ByQuery),
		chromedp.Click("#save", chromedp.ByQuery))
	if !strings.Contains(label, "USD") {
		t.Errorf("the ISO code is not shown with the input: label %q", label)
	}
	select {
	case s := <-got:
		if s != "128450 true map[]" {
			t.Errorf("server read %q, want 128450 cents with no errors: the default reading normalises US$ 1,284.5", s)
		}
	case <-ctx.Done():
		t.Fatal("the form was never submitted")
	}
}
```

The test package must link the `en` pack for `currency.Resolve("en").ID == "en"` (Task 7's `ui/packs_test.go` already does; it carries no build tag, so the browser build sees it too).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./form/ -count=1 && GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'TestRenderEverythingSmoke|TestFieldMoneyPartial' -count=1`
Expected: FAIL: `unknown field Currency in struct literal of type form.Field`; ui: `no such template "field-money"`.

- [ ] **Step 3: Implement**

In `form/fields.go`, add the four `Field` options with comments that say why (for example on `LocaleEcho`: "The text was typed into a page rendered in some locale; parsing it in any other can read 1.500 as fifteen hundred. The echo names that locale and the displayed currency, and Parse checks both."). Add `minor map[string]int64`, `present map[string]bool`, `echoLocale map[string]string`, `reseed map[string]bool`, `moneyCurrency map[string]string`, `args map[string]map[string]any`, `verifiedSeed map[string]string` to `Parsed`, initialised in `Parse`. The order of checks is the prototype's `Field.Read` (`git show c62d25e6:hack/moneycldr/proto/field.go`): locale echo, blank, currency echo, seed, reading, negative policy. Comment `Strict` with why it exists (a field that must take exactly the locale's own format, such as an import column) and `Stored` with why the seed check needs it (the seed is compared with the record's current rendering, never trusted as a value). In the `case Money:` branch, `if f.Currency != "" { p.parseMoney(r, f); continue }` before today's code.

`form/moneyfield.go`:

```go
func (p *Parsed) parseMoney(r *http.Request, f Field) {
	code, err := currency.Normalize(f.Currency)
	if err != nil {
		// A field's currency is the server's own value (the resource
		// default or the record's). A bad one is a programming error,
		// and failing loudly beats storing an amount in no currency.
		panic(fmt.Sprintf("form: field %q: currency %q: %v", f.Name, f.Currency, err))
	}
	raw := r.PostFormValue(f.Name)
	p.echo[f.Name] = raw
	p.moneyCurrency[f.Name] = code
	locale := f.Locale
	if f.LocaleEcho {
		v, ok := oneValue(r, f.Name+".locale")
		if !ok || !validLocaleEcho(v) {
			p.errs[f.Name] = moneyReenterKey
			p.reseed[f.Name] = true
			return
		}
		locale = v
	}
	p.echoLocale[f.Name] = locale
	if currency.Clean(raw) == "" {
		if f.Required {
			p.errs[f.Name] = fieldRequiredKey
		}
		return
	}
	if f.LocaleEcho {
		v, ok := oneValue(r, f.Name+".currency")
		if n, err := currency.Normalize(v); !ok || err != nil || n != code {
			// Compared, then discarded: the stored currency is always
			// the server's, so a forged value can only cause this
			// rejection, never a different denomination.
			p.errs[f.Name] = moneyCurrencyChangedKey
			return
		}
	}
	var minor int64
	seed, _ := oneValue(r, f.Name+".seed") // absent and empty are the same: no seed
	switch {
	case !f.Strict && seed != "" && f.Stored == nil:
		// A create form, or an empty field, never renders a seed.
		p.errs[f.Name] = moneyReenterKey
		p.reseed[f.Name] = true
		return
	case !f.Strict && seed != "" && strings.TrimSpace(raw) == seed:
		// The person left the rendered text alone, so the parser is not
		// run: an ambiguous seed such as en "1.500" for 1.5 KWD saves
		// unchanged. The seed is never a value. The record must still
		// render to exactly this seed, or it changed since the form was
		// drawn (or the seed was forged): re-enter, store nothing.
		if want, _ := currency.InputValue(locale, *f.Stored, code); want != seed {
			p.errs[f.Name] = moneyReenterKey
			p.reseed[f.Name] = true
			return
		}
		minor = *f.Stored
		p.verifiedSeed[f.Name] = seed // echoed if the form fails elsewhere; re-verified on the next POST
	default:
		read := currency.ParseNormalized
		if f.Strict {
			read = currency.Parse
		}
		var err error
		if minor, err = read(locale, raw, code); err != nil {
			p.errs[f.Name] = moneyErrorKey(err, code)
			var amb *currency.AmbiguousError
			switch {
			case errors.As(err, &amb):
				a, b := amb.Choices(locale, code)
				p.args[f.Name] = map[string]any{"a": a, "b": b}
			case errors.Is(err, currency.ErrWrongCurrency):
				p.args[f.Name] = map[string]any{"currency": code}
			}
			return
		}
	}
	if minor < 0 && !f.Negative {
		p.errs[f.Name] = moneyNegativeKey
		return
	}
	p.minor[f.Name], p.present[f.Name] = minor, true
}

// oneValue is the single submitted value of name. Absent or repeated
// is refused: two locale values would let the client choose which one
// the server reads.
func oneValue(r *http.Request, name string) (string, bool) {
	vs := r.PostForm[name]
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

// validLocaleEcho is the shape check from spec §5: at most 35 bytes of
// [A-Za-z0-9-], and exactly a shipped id as Resolve spells it, so the
// echo can only name a locale this binary would itself have rendered.
func validLocaleEcho(v string) bool {
	if v == "" || len(v) > 35 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-') {
			return false
		}
	}
	return currency.Resolve(v).ID == v
}

func moneyErrorKey(err error, code string) string {
	switch {
	case errors.Is(err, currency.ErrPrecision):
		switch exp, _ := currency.Exponent(code); exp {
		case 0:
			return moneyPrecision0Key
		case 3:
			return moneyPrecision3Key
		default:
			return moneyPrecisionKey
		}
	case errors.Is(err, currency.ErrDecimalComma):
		return moneyDecimalCommaKey
	case errors.Is(err, currency.ErrDecimalPoint):
		return moneyDecimalPointKey
	case errors.Is(err, currency.ErrCurrencySign):
		return moneyCurrencyKey
	case errors.Is(err, currency.ErrWrongCurrency):
		return moneyWrongCurrencyKey
	case errors.As(err, new(*currency.AmbiguousError)):
		return moneyAmbiguousKey
	}
	return moneyInvalidKey
}
```

`Minor`, `MoneyInput`, `ErrorArgs` and `SeedMoney` as specified in Interfaces. `MoneyInput` for a field that parsed or failed after the locale check returns `{Value: p.echo[name], Locale: p.echoLocale[name], Currency: p.moneyCurrency[name], Seed: p.verifiedSeed[name]}, false`: the seed only when this submission verified it (the prototype's `Field.Echo`), so changed text is echoed with no seed and read by §5.1 next time, while unchanged text keeps its seed. The echoed seed is trusted no more than the first: the next POST verifies it against the record again; for a reseed it returns `{Locale: renderLocale, Currency: p.moneyCurrency[name]}, true`. The storage set has exponents 0, 2 and 3 only (Task 2's test pins it), which is why three precision keys suffice; say so in a comment on `moneyErrorKey`.

Catalogs: add the eight new keys to `locales/en.toml` with Appendix C.1's English exactly, and to the eleven other framework catalogs with drafted translations (keep `{}` placeholders and punctuation conventions of each file). `money_invalid` and `money_precision` keep their current text unless Appendix C.1 changed them; if it did, change all twelve catalogs and `form/money.go`'s `moneyInvalidEN`/`moneyPrecisionEN` together.

`ui/partials/field-money.html`: copy `field-text.html`'s structure (label, hint, error, `aria-describedby`, required star) and its header comment's style; document each key; explain in the comment why the two hidden inputs exist (spec §5) and why the code is shown in the label (the currency always comes from the data, and a person must see which currency they are typing in). Call `T` only in the legacy form here (`{{T "..."}}`) if it needs any default string at all; Task 12 migrates every partial with the codemod.

Gallery: add a `partialDoc{Name: "field-money", Blurb: <gallery.field_money.blurb>, Wrap: wrapForm, States: [...]}` after `field-url` in `samples.go`, with states "Bare" (`Name: "fm_bare", Label: <gallery.field_money.label>, Locale: "en", Currency: "EUR"`), "With a hint" (adds `Hint: <gallery.field_money.hint>`, as re-approved by the second copy review: Task 1's draft, "Without the currency sign.", is wrong advice once the field accepts its own currency's sign) and "With an error" (adds `Value: "12,50"`, `Error: <rastrillo.ui.money_decimal_point's approved English>`). Add the blurb, label and hint to `prose.go` with eleven drafted translations each.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./form/ -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'Test[^B]' -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/designsystem/ -run 'TestEveryPartialAppearsAcrossThePages|TestEveryProseKeyIsTranslated' -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test . -run 'Catalog|Base' -count=1
```
Expected: PASS, including the framework's catalog-completeness tests (every key in all twelve catalogs). Then the browser test, which needs Chrome as the `browser` roles do: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test -tags browser ./ui/ -run TestFieldMoneySubmitsWithNoScript -count=1`; expected PASS.

- [ ] **Step 5: Commit**

```bash
git add form locales/*.toml ui/partials/field-money.html ui/*_test.go internal/designsystem
git commit -F - <<'MSG'
Read money as people type it, in its currency, and catch a changed currency

A money field now takes the currency it is in and reads the person's
text in the locale that rendered it, named by a hidden input. By
default it accepts any text with one plausible reading and refuses
text with two, naming both; Strict keeps the exact format. An edit
form's seed lets an unchanged ambiguous value save, but the seed is
never a value: it only keeps what the record still holds. The displayed currency
travels with the form and the POST rejects a mismatch, keeping the
typed text: a default changed by a deploy between GET and POST would
otherwise store an amount in a currency nobody saw.

The cents API is unchanged. The eleven translations of the new keys
are unreviewed drafts.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 9: A localizer-aware function map in `ui.Funcs`

Spec: §10 ("Binding, and what the risk actually is", "Two call syntaxes", the call contract table, "A call's form is decided by its first argument's type", "One localizer representation: a pointer, everywhere", "What lives in the function map"), §4 (money's argument errors, nil writes `""`), Appendix C.2 (`op.money.*`).
Starts from: `hack/templatemem/pages/pages.go:47-64` (`Localizer`, `Localized`), `:206-252` (`localizerType`, `localizer`, `withL`), `:293-322` (the `money` function); `ui/funcs.go:126-150` (`Funcs`), `ui/number.go`.

This task makes `ui.Funcs` accept both call forms, so legacy templates keep rendering and migrated ones work, before anything is migrated. It does not make anything strict; that is `ui.Pages` (Task 13).

**Files:**
- Create: `ui/localizer.go`, `ui/localefuncs.go`
- Create: `ui/localizer_test.go`, `ui/localefuncs_test.go`
- Modify: `ui/packs_test.go` (Task 7 created it with `locales/en`): it also blank-imports `locales/es` and `locales/ar`, because this task's tests format Spanish money and Arabic money with its bidi marks, and Tasks 13 and 14 rely on the same ui test binary linking them. It has no build tag, so the `browser` build links them too.
- Modify: `ui/funcs.go` (`Funcs` registers the dispatching entries plus `localizer`, `withL`, `money`; its doc comment's count of entries the partials need)
- Modify (same task, because their contract changes here): `ui/number_test.go` (`:37` asserts `func(any) any`), `ui/funcs_test.go` (the two "exactly 17 entries" checks at `:153` and `:218`; the `T`, `Tf` and `dateWords` type assertions at `:221`, `:322`, `:350`, `:374`, `:387`, `:417`), and every other test that type-asserts one of the seven locale entries (`grep -rn '\["\(T\|Tf\|Tbdi\|number\|displayValue\|dateWords\)"\]\.(' --include=*_test.go .`)

**Interfaces:**
- Consumes: `currency.Format`, `currency.AmountFromAny` (Task 4); `locales.FormatFor` (Task 3).
- Produces (package `ui`):
  - `type Localizer struct` (unexported fields: `identity`, `format string`; `t func(key string, args ...any) string`)
  - `func NewLocalizer(identity, format string, t func(key string, args ...any) string) *Localizer`: `t` must do `{name}` interpolation (it is `rastrillo.Locales.Tf` bound to one identity in practice)
  - `func (l *Localizer) Identity() string`, `func (l *Localizer) Format() string`
  - `type Localized struct { L *Localizer; Locales []rastrillo.LocaleItem; Return string }`: what a page needs from its request (the localizer, and the switcher's items and return path, which the shells read in Task 14); `func (l Localized) Localizer() *Localizer` and `func (l Localized) Request() Localized`, promoted to every struct that embeds it, so an app's render function can read a generated view's request values through `interface{ Request() ui.Localized }`
  - `func ForRequest(r *http.Request) Localized`: `{L: LocalizerFrom(r), Locales: rastrillo.LocaleItems(r), Return: r.URL.RequestURI()}`
  - `func WithLocalizer(r *http.Request, l *Localizer) *http.Request`, `func LocalizerFrom(r *http.Request) *Localizer` (nil when absent)
  - Template functions in `Funcs()`: `localizer` (`func(data any) *Localizer`), `withL` (`func(data any, l *Localizer) any`), and `T`, `Tf`, `Tbdi`, `number`, `displayValue`, `dateWords`, `money`, each `func(args ...any) (any, error)` dispatching on `args[0]`'s type
  - `var LocaleFuncs = []string{"T", "Tf", "Tbdi", "number", "displayValue", "dateWords", "money"}`

Dispatch rule inside `ui.Funcs` (not strict; Pages is):
- first argument of type `*Localizer`, non-nil: that localizer's catalog (`t`) and formatting locale (`format`);
- first argument of type `*Localizer`, nil: the `Funcs` options' translator and locale, which is today's behaviour, so a migrated partial reached by an app that still renders with `ui.Funcs` renders as it did;
- any other first argument (the legacy form): the `Funcs` options, exactly as today; `money` alone has no legacy form and errors with `op.money.first_arg`.
- `money` with a nil `*Localizer` formats in the `Funcs` options' `WithLocale` locale, else `locales.FormatFor("")` (the installed default).
- `money $L amount code`: `currency.AmountFromAny(amount)`; a nil pointer amount writes `""`; a float, string, unsigned value above MaxInt64, unsupported code or wrong argument count is an error that stops the template.

- [ ] **Step 1: Record today's legacy output, then write the failing tests**

Before touching `ui`, record what today's function map renders for nil and edge values, so "unchanged" is checked against real output rather than a re-derivation. Run this once and keep its output:

```bash
cat > "$TMPDIR/legacy_test.go" <<'GO'
package ui
import ("bytes";"html/template";"testing")
func TestRecordLegacy(t *testing.T) {
	var nilp *int
	data := map[string]any{"Nil": nil, "NilP": nilp, "N": 1234567, "F": 1234.5, "S": "1234567", "U": uint64(18446744073709551615), "Evil": "<b>x</b>"}
	src := `[{{number .Nil}}][{{displayValue .Nil}}][{{number .NilP}}][{{number .N}}][{{number .F}}][{{number .S}}][{{displayValue .S}}][{{number .U}}][{{T "rastrillo.ui.cancel"}}][{{Tf "rastrillo.ui.error_ref" "ref" .Evil}}][{{Tbdi "rastrillo.ui.error_ref" "ref" .Evil}}]`
	for _, opts := range [][]Option{nil, {WithLocale("es")}} {
		var b bytes.Buffer
		if err := template.Must(template.New("x").Funcs(Funcs(opts...)).Parse(src)).Execute(&b, data); err != nil {
			t.Fatalf("recording today's output failed, so there is nothing to compare against: %v", err)
		}
		t.Logf("%q", b.String())
	}
}
GO
cp "$TMPDIR/legacy_test.go" ui/zz_record_legacy_test.go
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run TestRecordLegacy -v -count=1 | grep -F '"['
rm ui/zz_record_legacy_test.go "$TMPDIR/legacy_test.go"
```

Paste the two logged strings into `ui/localefuncs_test.go` as `wantLegacyEN` and `wantLegacyES`. (If `rastrillo.ui.error_ref` is not a key, pick any `Tf` key the partials use from `locales/en.toml` and use it in both the recorder and the test.)

`ui/localefuncs_test.go`:

```go
const legacySrc = `[{{number .Nil}}][{{displayValue .Nil}}][{{number .NilP}}][{{number .N}}][{{number .F}}][{{number .S}}][{{displayValue .S}}][{{number .U}}][{{T "rastrillo.ui.cancel"}}][{{Tf "rastrillo.ui.error_ref" "ref" .Evil}}][{{Tbdi "rastrillo.ui.error_ref" "ref" .Evil}}]`

// The same calls, migrated, with no localizer in the data: what an app
// still rendering with ui.Funcs gets from a migrated partial.
const migratedSrc = `{{$L := localizer $}}[{{number $L .Nil}}][{{displayValue $L .Nil}}][{{number $L .NilP}}][{{number $L .N}}][{{number $L .F}}][{{number $L .S}}][{{displayValue $L .S}}][{{number $L .U}}][{{T $L "rastrillo.ui.cancel"}}][{{Tf $L "rastrillo.ui.error_ref" "ref" .Evil}}][{{Tbdi $L "rastrillo.ui.error_ref" "ref" .Evil}}]`

func edgeData() map[string]any {
	var nilp *int
	return map[string]any{"Nil": nil, "NilP": nilp, "N": 1234567, "F": 1234.5, "S": "1234567", "U": uint64(18446744073709551615), "Evil": "<b>x</b>"}
}

func execTmpl(t *testing.T, funcs template.FuncMap, src string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := template.Must(template.New("x").Funcs(funcs).Parse(src)).Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestLegacyAndNilLocalizerUnchanged(t *testing.T) {
	for _, c := range []struct {
		opts []Option
		want string
	}{{nil, wantLegacyEN}, {[]Option{WithLocale("es")}, wantLegacyES}} {
		if got := execTmpl(t, Funcs(c.opts...), legacySrc, edgeData()); got != c.want {
			t.Errorf("legacy form changed:\n got %q\nwant %q", got, c.want)
		}
		if got := execTmpl(t, Funcs(c.opts...), migratedSrc, edgeData()); got != c.want {
			t.Errorf("migrated form with no L differs from today's output:\n got %q\nwant %q", got, c.want)
		}
	}
}

func TestLocalizerFormUsesTheLocalizer(t *testing.T) {
	es := NewLocalizer("es", "es", func(k string, args ...any) string {
		if k == "app.hello" {
			return "Hola"
		}
		return k
	})
	src := `{{$L := localizer $}}{{T $L "app.hello"}}|{{number $L .N}}|{{money $L .P .C}}|{{money $L .Nil .C}}|{{template "p" (withL (dict "X" 1) $L)}}{{define "p"}}{{$L := localizer $}}{{T $L "app.hello"}}{{end}}`
	var nilAmount *int64
	got := execTmpl(t, Funcs(), src, map[string]any{"L": es, "N": 1234567, "P": int64(128450), "C": "EUR", "Nil": nilAmount})
	if want := "Hola|1.234.567|1284,50\u00a0€||Hola"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = execTmpl(t, Funcs(), `{{$L := localizer $}}{{T $L "app.hello"}}`, struct{ Localized }{Localized{L: es}})
	if got != "Hola" {
		t.Errorf("struct path: %q", got)
	}
}

func TestMoneyArgumentErrorsStopTheTemplate(t *testing.T) {
	es := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	for name, src := range map[string]string{
		"legacy form":     `{{money .P .C}}`,
		"missing code":    `{{$L := localizer $}}{{money $L .P}}`,
		"float":           `{{$L := localizer $}}{{money $L .F .C}}`,
		"string":          `{{$L := localizer $}}{{money $L "12" .C}}`,
		"huge unsigned":   `{{$L := localizer $}}{{money $L .U .C}}`,
		"unsupported":     `{{$L := localizer $}}{{money $L .P "KDW"}}`,
	} {
		tmpl := template.Must(template.New("x").Funcs(Funcs()).Parse(src))
		err := tmpl.Execute(io.Discard, map[string]any{"L": es, "P": int64(1), "C": "EUR", "F": 1.5, "U": uint64(1 << 63)})
		if err == nil {
			t.Errorf("%s: executed without error", name)
		}
	}
}

// Review Focus 4: money's output in every html/template context. The
// formatter's output carries no-break spaces and bidi marks, never
// markup; the escaper must still treat it as text everywhere.
func TestMoneyEscapesInEveryContext(t *testing.T) {
	ar := NewLocalizer("ar", "ar", func(k string, _ ...any) string { return k })
	want, _ := currency.Format("ar", 128450, "SAR")
	src := `{{$L := localizer $}}<p>{{money $L .P .C}}</p><a title="{{money $L .P .C}}" href="/pay?amount={{money $L .P .C}}">x</a><script>var v = {{money $L .P .C}};</script>`
	out := execTmpl(t, Funcs(), src, map[string]any{"L": ar, "P": int64(128450), "C": "SAR"})
	if !strings.Contains(out, "<p>"+template.HTMLEscapeString(want)+"</p>") {
		t.Errorf("text context: %q", out)
	}
	if !strings.Contains(out, `title="`+template.HTMLEscapeString(want)+`"`) {
		t.Errorf("attribute context: %q", out)
	}
	js, _ := json.Marshal(want)
	if !strings.Contains(out, "var v = "+string(js)+";") {
		t.Errorf("script context: %q, want a JSON string %s", out, js)
	}
	i := strings.Index(out, `href="`) + len(`href="`)
	u, err := url.Parse(html.UnescapeString(out[i : i+strings.Index(out[i:], `"`)]))
	if err != nil || u.Query().Get("amount") != want {
		t.Errorf("URL context: %q decodes to %q, %v", out, u.Query().Get("amount"), err)
	}
}

func TestFuncsKeepsEveryEntry(t *testing.T) {
	m := Funcs()
	for _, name := range append(append([]string{}, LocaleFuncs...), "localizer", "withL", "dict", "list", "menuGroup",
		"searchClear", "icon", "iconAssets", "opt", "stageArt", "displayURL", "safeHref", "rowMenuItems") {
		if m[name] == nil {
			t.Errorf("Funcs lost %q: shipped partials stop parsing without it", name)
		}
	}
}
```

Migrate the existing tests that assert the old signatures, keeping every behavioural assertion. Add one helper to `ui/funcs_test.go` and route them through it:

```go
// call invokes a template function the way a template does, so the
// tests check behaviour, not the Go signature: the locale entries are
// variadic dispatchers now, and asserting func(any) any would only
// pin an implementation detail.
func call(t *testing.T, m template.FuncMap, name string, args ...any) any {
	t.Helper()
	fn := reflect.ValueOf(m[name])
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	out := fn.Call(in)
	if len(out) == 2 && !out[1].IsNil() {
		t.Fatalf("%s(%v): %v", name, args, out[1].Interface())
	}
	return out[0].Interface()
}
```

`number_test.go:37` becomes `got := call(t, Funcs(WithLocale(tc.locale)), "number", tc.value)` with the same table; the `T`/`Tf`/`dateWords` assertions in `funcs_test.go` become `call(t, f, "T", "k")` and so on, with the same expected values. The two entry-count checks become `len(f) != 20` with the message naming the three new entries (`localizer`, `withL`, `money`). Assertions on `icon` and `iconAssets` types are untouched (those entries do not change).

`ui/localizer_test.go`: the `localizer`/`withL` cases from the prototype (`localizer` on a map, a struct, a struct embedding `Localized`, a pointer to one, an `L` that is a string (typed nil), nil data; `withL` copies a map and leaves the original without `L`, returns a struct unchanged), and `LocalizerFrom(WithLocalizer(r, l)) == l`, `LocalizerFrom(r) == nil` without it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'TestLegacy|TestLocalizer|TestMoney|TestFuncsKeeps' -count=1`
Expected: FAIL to build (`undefined: NewLocalizer`).

- [ ] **Step 3: Implement**

`ui/localizer.go`: port `Localizer` (with the fields above instead of the prototype's per-locale function map: a localizer holds its translator and formatting locale, never a function map, so there is nothing per locale to rebind), `Localized`, `localizerType`, `localizer`, `withL` from the prototype; add `NewLocalizer`, `Identity`, `Format`, and the context helpers with an unexported key type.

`ui/localefuncs.go`: one helper decides the form:

```go
// pick decides a locale call's form by its first argument's type,
// never its value: a *Localizer, nil or not, is the localizer form, so
// a nil value passed to a legacy {{number .Value}} can never be taken
// for a missing localizer. A nil localizer here means the data reached
// the template without "L"; ui.Funcs answers with its own options
// (today's behaviour), and ui.Pages replaces these entries with strict
// ones.
func pick(args []any) (l *Localizer, localizerForm bool, rest []any) {
	if len(args) > 0 && reflect.TypeOf(args[0]) == localizerType {
		l, _ := args[0].(*Localizer)
		return l, true, args[1:]
	}
	return nil, false, args
}
```

and each entry (`T`, `Tf`, `Tbdi`, `number`, `displayValue`, `dateWords`, `money`) as a closure over the `config` built in `Funcs`, using `l.t`/`l.format` when `l != nil` and `c.t`/`c.tf`/`c.locale` otherwise. Validate arity and argument types explicitly (`T` needs a string key; `number` exactly one value; `dateWords` none) and return an error naming the function, because html/template no longer checks a variadic `any` signature; its text is Appendix C `op.funcs.arity` or `op.funcs.arg_type` (second copy review, listed at the end of this plan). `Tbdi` keeps returning `template.HTML` and `dateWords` a string; the escaper honours the dynamic type. `money` as specified, with errors from Appendix C.2.

In `Funcs`, register the dispatchers in place of today's entries, plus `"localizer": localizer`, `"withL": withL`, `"money": ...`. Update the doc comment: the count of entries an app must not drop, and that the locale entries take an optional leading `$L`.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'Test[^B]' -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/designsystem/ -count=1 -short
make example-blog example-tickets example-notes
```
Expected: PASS: every existing golden is unchanged, because no template has been migrated and legacy calls render as before.

- [ ] **Step 5: Commit**

```bash
git add ui
git commit -F - <<'MSG'
Let ui's locale functions take the request's localizer first

A call's form is decided by its first argument's type, so a nil value
passed to a legacy {{number .Value}} is never mistaken for a missing
localizer. Without a localizer, ui.Funcs answers exactly as it did, so
partials can be migrated next without changing any app that still
renders through ui.Funcs.

The localizer carries a translator and a formatting locale, not a
function map: nothing is rebound per request or per language, which is
what lets one template set serve every language without one request's
locale reaching another's output.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 10: The template codemod

Spec: §10 ("The codemod: `rastrillo migrate-templates [-write] PATH...`": from the parse tree, validated before writing, `$L` scope, reported for a person, dry run and idempotent, repo counts).
Starts from: `hack/templatemem/codemod/codemod.go` (all of it), `codemod/codemod_test.go` (`TestRewriteCases`, `TestRewriteRefusesConflictingL`, `TestRewriteReportsNoData`, `TestValidateRejectsBadRewrites`, `TestRewriteRepoTemplates`).

**Files:**
- Create: `internal/tmplmigrate/rewrite.go` (from `codemod.go`), `internal/tmplmigrate/doc.go`
- Create: `internal/tmplmigrate/rewrite_test.go`, `internal/tmplmigrate/corpus_test.go`

**Interfaces:**
- Consumes: `ui.LocaleFuncs` (Task 9) for the set of locale functions (replacing the prototype's private `localeFuncs` map, so the codemod and the runtime cannot disagree about which calls take `$L`).
- Produces (package `amadan.net/rastrillo/rastrillo/internal/tmplmigrate`):
  - `type Stats struct { Calls, Dicts, Wrapped, Preludes int; Review, NoData []string }`, `func (s *Stats) Add(o Stats)`
  - `type ConflictError struct` (as the prototype), `func (e *ConflictError) Error() string`
  - `func Rewrite(name, src string) (string, Stats, error)`: validated; on a failed validation it returns the input unchanged and an error
  - `func RewriteFS(fsys fs.FS, root string) (fstest.MapFS, Stats, error)`
  - `const Prelude = "{{$L := localizer $}}"`

- [ ] **Step 1: Write the failing tests**

Port the four unit tests verbatim into `rewrite_test.go` (package `tmplmigrate`, `P` becomes `Prelude`). Add Review Focus 5:

```go
// Review Focus 5: the rewrite only ever inserts, and only the texts
// it is allowed to insert. A real app's file may have CRLF line
// endings, a byte-order mark, trim markers, comments, and literal text
// that looks like an insertion; none of it may change.
func TestRewriteOnlyInserts(t *testing.T) {
	for name, in := range map[string]string{
		"crlf":       "{{define \"a\"}}\r\n<p>{{T \"k\"}}</p>\r\n{{template \"p\" dict \"A\" 1}}\r\n{{end}}\r\n",
		"bom":        "\ufeff<p>{{T \"k\"}}</p>\n",
		"trim":       "{{- T \"k\" -}}\n{{- template \"p\" dict \"A\" 1 -}}\n{{- .X | number -}}",
		"comment":    "{{/* {{T \"k\"}} dict \"A\" */}}{{- /* T $L withL */ -}}<p>{{T \"k\"}}</p>",
		"two defs":   "{{define \"a\"}}{{T \"k\"}}{{end}}\n{{define \"b\"}}{{Tf \"k\" \"n\" 1}}{{end}}",
		"look-alike": "<p>T $L \"L\" $L (withL (x) $L)</p>{{printf \"%s\" \" $L\"}}{{T \"k\"}}",
		"nested":     "{{template \"p\" makeData (printf \"%s\" (Tf \"k\" \"n\" (number .N)))}}",
		"multiline":  "{{template \"p\"\n  makeData\n  .Item}}\n{{T\n \"k\"}}",
	} {
		out, _, err := Rewrite(name, in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if extra, ok := onlyAllowedInsertions(in, out); !ok {
			t.Errorf("%s: the rewrite changed input bytes or inserted something else (%q):\n in %q\nout %q", name, extra, in, out)
		}
		if got, want := comments(out), comments(in); !slices.Equal(got, want) {
			t.Errorf("%s: comments changed:\n got %q\nwant %q", name, got, want)
		}
		if strings.HasPrefix(in, "\ufeff") && !strings.HasPrefix(out, "\ufeff") {
			t.Errorf("%s: the byte-order mark moved: %q", name, out)
		}
		if strings.Count(out, "\r\n") != strings.Count(in, "\r\n") {
			t.Errorf("%s: line endings changed", name)
		}
	}
}

// allowed are the only texts Rewrite may insert.
var allowed = []string{Prelude, ` "L" $L`, ` $L`, `(withL (`, `) $L)`}

// onlyAllowedInsertions reports whether out is in with only allowed
// insertions added. It searches every alignment (memoised), because a
// greedy one fails on the commonest insertion: " $L" begins with the
// same space as the input it is inserted before. On failure it returns
// out from the furthest position any alignment reached, for the message.
func onlyAllowedInsertions(in, out string) (extra string, ok bool) {
	type state struct{ i, j int }
	dead := map[state]bool{}
	far := 0
	var walk func(i, j int) bool
	walk = func(i, j int) bool {
		if j > far {
			far = j
		}
		if j == len(out) {
			return i == len(in)
		}
		if dead[state{i, j}] {
			return false
		}
		if i < len(in) && in[i] == out[j] && walk(i+1, j+1) {
			return true
		}
		for _, a := range allowed {
			if strings.HasPrefix(out[j:], a) && walk(i, j+len(a)) {
				return true
			}
		}
		dead[state{i, j}] = true
		return false
	}
	if walk(0, 0) {
		return "", true
	}
	return out[far:], false
}

var commentRE = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

func comments(s string) []string { return commentRE.FindAllString(s, -1) }
```

A stray ` $L` inside a comment is caught by the comment comparison, and one inside a string literal by the look-alike case's assertion below; the alignment alone cannot tell where an insertion landed. Also assert, for the "look-alike" case, that the paragraph text and the `printf` literal appear unchanged in `out`, and for "comment", that `Stats.Calls` is 1 (only the call outside the comment).

`corpus_test.go`:

```go
// The repo's own templates, live. Each migration task lowers these
// counts; once every corpus is migrated they are all zero, and the
// test then guards against a new template written in the old syntax.
// The generated templates under examples/*/gen are counted here; the
// command skips gen/ (regenerating migrates them), the codemod does not.
func TestRepoTemplates(t *testing.T) {
	want := map[string]Stats{
		"ui/partials": {Calls: 114, Dicts: 5, Wrapped: 5, Preludes: 22},
		"ui/layouts":  {Calls: 20, Dicts: 0, Wrapped: 0, Preludes: 10},
		"examples":    {Calls: 113, Dicts: 96, Wrapped: 6, Preludes: 26},
	}
	for root, w := range want {
		out, got, err := RewriteFS(os.DirFS("../.."), root)
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if got.Calls != w.Calls || got.Dicts != w.Dicts || got.Wrapped != w.Wrapped || got.Preludes != w.Preludes || len(got.NoData) != 0 {
			t.Errorf("%s: calls %d dicts %d wrapped %d preludes %d no-data %d; want %+v",
				root, got.Calls, got.Dicts, got.Wrapped, got.Preludes, len(got.NoData), w)
		}
		for path, f := range out {
			again, st, err := Rewrite(path, string(f.Data))
			if err != nil || again != string(f.Data) || st.Calls+st.Dicts+st.Wrapped+st.Preludes != 0 {
				t.Errorf("%s: a second run changed it (%v)", path, err)
			}
		}
	}
}
```

The `want` numbers are the spec's table without the scaffold row (ui/partials 114/5/5/22, ui/layouts 20/0/0/10, the three examples together 113/96/6/26). Task 8 added `field-money.html`; if it contains any locale call, the ui/partials numbers grow by what it contributes. Run the test once; if it differs from the spec only by `field-money.html`'s contribution, pin the measured numbers and say so in a comment. Any other difference is a codemod porting error: stop and find it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -count=1`
Expected: FAIL to build (`undefined: Rewrite`).

- [ ] **Step 3: Port the implementation**

Copy `codemod.go` into `rewrite.go` (package `tmplmigrate`). Replace the private `localeFuncs` map with one built from `ui.LocaleFuncs`; export `Prelude`. Keep every comment that explains a rule (scope tracking, quote-aware pipe end, validation by parse-tree comparison). If `TestRewriteOnlyInserts`'s BOM case fails because a top-of-file prelude lands before U+FEFF, make the insertion position for a file-level prelude skip a leading BOM, with a comment naming the failure (a BOM moved off byte 0 is no longer a BOM, and the file renders a stray character).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -count=1 -v -run 'TestRewrite|TestValidate|TestRepoTemplates'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmplmigrate
git commit -F - <<'MSG'
Add the template codemod for localizer-first calls

Every edit is an insertion at a position the template parser reports,
and the result is re-parsed and compared before anything is written,
so a regrouped pipeline or a lost $L is refused rather than shipped.
A file that already binds $L to something else is refused whole.

The repo's own templates are pinned live: each migration lowers the
counts, and at zero the test stops new templates being written in the
old syntax. The insertion-only test covers what real files have and
the fixtures did not: CRLF, a byte-order mark, trim markers, comments.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 11: The Go preflight, `rastrillo migrate-templates`, and `rastrillo doctor`'s checks

Spec: §10 ("Every app runs a template preflight", "The preflight is advisory", the type-checked registration scan, "It fails while anything is left to do", the dependency note), §3 ("Doctor"), §12 item 9 ("Doctor runs the probe"), Appendix C.2 (`op.migrate.*`, `op.doctor.*`).
Starts from: `hack/templatemem/codemod/gocheck.go`, `codemod/codemod_test.go:117-170` (`TestCheckGo`), `codemod/testdata/gocheckmod/`, `cmd/migrate-templates/main.go` and `main_test.go`.

**Files:**
- Create: `internal/tmplmigrate/gocheck.go`, `internal/tmplmigrate/gocheck_test.go`, `internal/tmplmigrate/testdata/gocheckmod/` (copied)
- Create: `internal/tmplmigrate/run.go`, `internal/tmplmigrate/run_test.go`
- Create: `internal/localecheck/localecheck.go`, `internal/localecheck/localecheck_test.go`
- Modify: `cmd/rastrillo/main.go` (`migrate-templates` subcommand and usage line), `cmd/rastrillo/doctor.go` (runs the preflight, the probe and the static locale checks; new exit codes), `cmd/rastrillo/doctor_test.go`
- Delete: `hack/templatemem/codemod/`, `hack/templatemem/cmd/migrate-templates/`, `hack/templatemem/pages/` and `hack/templatemem/main.go`. The first two are ported by Tasks 10 and 11; `pages` and `main.go` import `codemod`, so they cannot outlive it, and the codemod's prototype tests pin the examples' Go findings and the repo's template counts, which Task 12 changes. Task 13 ports `pages` from the branch's base commit (`git show $(git merge-base HEAD origin/main):hack/templatemem/pages/pages.go`). `Makefile`'s `money-cldr` line for `hack/templatemem` keeps only `go vet ./... && go test -count=1 -v ./upgradeprobe` (Task 15 removes that too).
- Modify: `go.mod`, `go.sum` (`golang.org/x/tools v0.47.0` becomes a direct requirement)

**Interfaces:**
- Consumes: `Rewrite`, `Stats`, `ConflictError` (Task 10); `manifest.LoadApp` (Task 6); `checkconfig.Env` (Task 7).
- Produces:
  - `tmplmigrate`: `type GoFinding struct { Pos, What, Fix string; Review bool }`; `func CheckGo(dir string) ([]GoFinding, error)`; `func Run(args []string, stdout, stderr io.Writer) int` (the command: `[-write] PATH...`; exit 0 nothing left, 1 anything left, 2 usage or I/O error); `Run` skips `gen/` and `static/` directories and reports `op.migrate.gen_stale` for a `gen/` holding unmigrated templates
  - `localecheck`: `func Static(appDir string) []string` (problems: `op.doctor.packs_stale`, and `op.catalog.*` from `catalogs.Check`); `func Probe(ctx context.Context, appDir string) (report []byte, problems []string, err error)` (runs `go run ./cmd/<name>` for each `cmd/*/main.go` in the app module with only `RASTRILLO_CHECK_CONFIG=1` added to the environment; a non-zero exit or `"ok":false` is a problem, `op.doctor.probe_failed`)
  - `doctor` exit codes: existing 0-5 unchanged in meaning and precedence; new `exitTemplates = 6` (the preflight has anything left) and `exitLocales = 7` (the probe or a static locale check failed). Precedence, highest first: 5 pre-move, 4 skew, 3 drift, 6 templates, 7 locales, because an unmigrated template stops the app rendering at all while a locale problem stops it starting with a named reason; doctor prints every section whatever the code

- [ ] **Step 1: Write the failing tests**

Copy `hack/templatemem/codemod/testdata/gocheckmod` to `internal/tmplmigrate/testdata/gocheckmod`; change its `go.mod` `replace amadan.net/rastrillo/rastrillo => ../../../../..` to `=> ../../../..`. Port `TestCheckGo` verbatim into `gocheck_test.go`, with the example paths changed to `../../examples/<ex>` (the three examples must each report one `function map entry "T"` until Task 23/24 remove their registrations; the test pins that, and those tasks change it to zero findings) and the full 23-line `want` list unchanged.

Port `hack/templatemem/cmd/migrate-templates/main_test.go`'s two tests into `run_test.go` against `Run`, unchanged in substance: the single-file `{{T "app.hello"}}` dry run exits 1 and leaves the file unchanged, `-write` exits 0, a second dry run exits 0; a Go finding alone exits 1. Add:

```go
func TestRunSkipsGeneratedTemplatesButSaysSo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "gen", "templates", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, "gen", "templates", "x", "list.html"), []byte(`{{T "k"}}`), 0o644)
	var out strings.Builder
	code := Run([]string{"-write", dir}, &out, io.Discard)
	src, _ := os.ReadFile(filepath.Join(dir, "gen", "templates", "x", "list.html"))
	if string(src) != `{{T "k"}}` {
		t.Errorf("Run rewrote a generated file: %q", src)
	}
	if code != 1 || !strings.Contains(out.String(), "rastrillo generate") {
		t.Errorf("exit %d, output %q; want exit 1 and the advice to regenerate", code, out.String())
	}
}
```

`internal/localecheck/localecheck_test.go`:

```go
func TestStatic(t *testing.T) {
	app := t.TempDir()
	os.MkdirAll(filepath.Join(app, "manifest"), 0o755)
	os.MkdirAll(filepath.Join(app, "gen", "locales"), 0o755)
	// de: the framework's own strings are not translated into it, so
	// locales/de.toml is required even with no app strings (Task 6's rule).
	os.WriteFile(filepath.Join(app, "manifest", "app.toml"), []byte("locales = [\"en\", \"de\"]\n"), 0o644)
	if _, err := packs.Emit(filepath.Join(app, "gen"), manifest.App{Locales: []string{"en"}, DefaultLocale: "en"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Static(app), "\n")
	if !strings.Contains(got, "run rastrillo generate") || !strings.Contains(got, "locales/de.toml") {
		t.Errorf("Static = %q; want the stale packs file and the missing de catalog", got)
	}
}

// The probe runs in the app's own binary and opens nothing: run it on
// a scaffolded app whose database file does not exist, and the file
// must still not exist afterwards (spec §12 item 12).
func TestProbeOpensNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scaffolded app")
	}
	app := scaffold(t) // `rastrillo new` into t.TempDir(), via the CLI built from this checkout
	report, problems, err := Probe(context.Background(), app)
	if err != nil || len(problems) != 0 || !strings.Contains(string(report), `"ok":true`) {
		t.Fatalf("probe: %s, %v, %v", report, problems, err)
	}
	matches, _ := filepath.Glob(filepath.Join(app, "*.db*"))
	if len(matches) != 0 {
		t.Errorf("the probe created %v", matches)
	}
}
```

`scaffold(t)` builds the CLI with `go build -buildvcs=false -o <tmp>/rastrillo ../../cmd/rastrillo`, runs `rastrillo new probeapp` with `cmd.Dir` set to a `t.TempDir()` (`new` has no directory flag; it scaffolds into the working directory), then appends `replace amadan.net/rastrillo/rastrillo => <absolute checkout root>` to the app's `go.mod` and runs `go mod tidy` there, as `cmd/rastrillo/new_scaffold_test.go:345-356` does, with `GOFLAGS=-mod=mod`.

In `cmd/rastrillo/doctor_test.go` add a test that doctor on a copy of `examples/tickets` (still unmigrated at this point; in the copy, rewrite `go.mod`'s `replace … => ../..` to the absolute checkout root so the probe can build) exits 6 and prints the pending templates and the Go finding, and that doctor on a freshly scaffolded app exits 0 once Task 25 lands; until then assert only the exit-6 case.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ ./internal/localecheck/ -count=1`
Expected: FAIL to build (`undefined: CheckGo`, `undefined: Run`, `undefined: Static`).

- [ ] **Step 3: Implement**

- `go get golang.org/x/tools@v0.47.0` in the root module (it is already in `go.sum` as indirect); `go mod tidy` must leave everything else unchanged. Commit body notes the spec's "When the implementation moves the preflight into `cmd/rastrillo`, it adds the requirement to the root module".
- `gocheck.go`: port verbatim; `UIPath` stays `amadan.net/rastrillo/rastrillo/ui`; the locale-function names come from `ui.LocaleFuncs` plus `localizer` and `withL`.
- `run.go`: port `cmd/migrate-templates/main.go`'s `run` and `hasGo` as `Run`, with every printed string from Appendix C.2's `op.migrate.*`, the `gen/` and `static/` skips, and the `gen_stale` report.
- `cmd/rastrillo/main.go`: `case "migrate-templates": if code := tmplmigrate.Run(os.Args[2:], os.Stdout, os.Stderr); code != 0 { err = exitError{code: code} }`; the usage line is Appendix C.5's `usage.migrate_templates`, and the `-write` flag's help is `flag.migrate.write`.
- `localecheck`: `Static` reads `manifest/app.toml` with `manifest.LoadApp`, renders what `packs.Emit` would write into a temp dir under `$TMPDIR`-respecting `os.MkdirTemp("", ...)` and compares with `gen/locales/packs.go`; and returns `catalogs.Check(appDir, app)`'s problems (Task 6), so doctor and `generate --check` apply one rule. `Probe` runs `exec.CommandContext(ctx, "go", "run", "./cmd/"+name)` with `Dir: appDir`, `Env: append(os.Environ(), "RASTRILLO_CHECK_CONFIG=1")`, a 2-minute timeout, and parses the single stdout line as `locales.ProbeReport`.
- `cmd/rastrillo/doctor.go`: after the existing diagnosis, run `tmplmigrate.Run([]string{dir}, &buf, &buf)` (dry run), `localecheck.Static(dir)` and `localecheck.Probe(ctx, dir)`; print each section under a heading whose text is an Appendix C id added by the second copy review (`op.doctor.heading_templates`, `op.doctor.heading_locales`, listed at the end of this plan); map to the new exit codes. Keep `doctor.go`'s growth under 40 lines (the budget); everything else is in the two internal packages. Update the exit-code comment table.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ ./internal/localecheck/ -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./cmd/rastrillo/ -run 'Doctor|MigrateTemplates' -count=1
go run ./cmd/rastrillo budget size
```
Expected: PASS; no budget over its ceiling.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/tmplmigrate internal/localecheck cmd/rastrillo Makefile
git rm -r -q hack/templatemem/codemod hack/templatemem/cmd/migrate-templates hack/templatemem/pages hack/templatemem/main.go
git commit -F - <<'MSG'
Add rastrillo migrate-templates and make doctor run it and the probe

Every app needs this step, money or not: a migrated partial calls
T($L, key), and an app that registers its own one-argument T breaks
when that partial executes, which parsing does not catch. The Go scan
is type-checked, so a key counts only when the type checker gives it a
constant value; anything else is a review item, and review items fail
the dry run.

The preflight is advisory; Pages refusing a reserved function name and
strict dispatch are the guarantee. Doctor also runs the configuration
probe in the app's own binary, which is safe against a live database
because the probe opens nothing.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 12: Migrate ui's partials and layouts

Spec: §10 (the call contract; "`{{template "x" dict …}}` passes it as `"L" $L`"; `withL`; "Template precedence is unchanged").
Starts from: the codemod (Task 10) and `rastrillo migrate-templates` (Task 11).

**Files:**
- Modify: every `ui/partials/*.html` and `ui/layouts/*.html` the codemod rewrites
- Modify: `internal/tmplmigrate/corpus_test.go` (ui counts become zero)
- Modify: `examples/{blog,tickets,notes}` render code (the bridge below), and `internal/tmplmigrate/gocheck_test.go` (the examples' findings change, below)
- Modify: `internal/generate/templates_test.go`: `TestListHTMLDropdownMatchesPartial` (`:270-330`) registers a one-argument `T` and executes the emitted `list.html` through ui's partials, so it fails once the partials call `T $L …`; replace `.Funcs(template.FuncMap{"T": T})` with `.Funcs(ui.Funcs(ui.WithT(func(k string, _ ...any) string { return T(k) })))`, keeping every assertion. `TestEmittedTemplatesParse` (`:567`) only parses, so it would still pass, but it states the environment an app provides; change its stub the same way so it stops documenting a registration that no longer works. Before committing, `grep -rn '"T": *func\|"T": *genT' --include=*.go . | grep -v hack` must list nothing.

The three examples register their own one-argument `T` (`blog/internal/blog/view.go:64`, `notes/internal/notes/genrender.go:39`, `tickets/internal/tickets/render.go:47`). A migrated partial calls `T $L "key"`, which a one-argument `T` cannot take, so those examples break the moment ui's partials migrate. This task therefore also replaces each registration with `ui.WithT` over a `rastrillo.Locales` built from the example's generated catalog: `ui.Funcs`' dispatching `T` then serves both the examples' legacy templates and the migrated partials, with the same lookup order as today plus the framework's English and `{name}` interpolation for framework keys (today those render as the bare key, because `genT` knows only manifest keys). Task 23 and Task 24 replace this with `ui.Pages`.

**Interfaces:**
- Consumes: `ui.Funcs`' two-form dispatch (Task 9); `tmplmigrate.Run` (Task 11); `rastrillo.NewLocales`, `(*rastrillo.Locales).Tf`.
- Produces: migrated `ui/partials` and `ui/layouts`, which every later task relies on.

- [ ] **Step 1: Pin the zero and run the dry run**

Change `corpus_test.go`'s `ui/partials` and `ui/layouts` entries to all-zero `Stats{}`. Run `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -run TestRepoTemplates -count=1`; expected FAIL with the current counts. Then run the dry run and keep its report:

```bash
GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates ui/partials ui/layouts | tee "$TMPDIR/ui-dry.txt"
```

Expected: exit 1, the pending files, 0 refused, the 5 review sites in `ui/partials` (spec table), 0 no-data calls.

- [ ] **Step 2: Check each review site**

For each "review" line, open the partial and confirm the wrapped data is a map built with `dict` at every in-repo call site, or a struct that carries `L`. The spec names `field-daterange`'s `.Start`/`.End` dicts as the map path. If a site is reached with struct data in the gallery, the examples or the scaffold, note it in the commit body; Task 13's strict tests and Task 23-24's example runs will fail on it if it lacks `L`.

- [ ] **Step 3: Write**

```bash
GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates -write ui/partials ui/layouts
GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates ui/partials ui/layouts   # expect exit 0
```

The second run must exit 0: a site wrapped by the first run is `(withL …)` already, so the codemod (idempotent, Task 10) wraps and reports nothing the second time. A non-zero exit here is a codemod defect to fix in `internal/tmplmigrate`, not to accept.

- [ ] **Step 4: Bridge the examples**

In each of the three render files, replace `.Funcs(ui.Funcs()).Funcs(template.FuncMap{"T": genT, ...})` with `.Funcs(ui.Funcs(ui.WithT(catalogT))).Funcs(template.FuncMap{...the remaining app functions, e.g. "asset"...})`, where:

```go
// catalog is the generated manifest catalog layered over the
// framework's, so ui's migrated partials and this app's templates both
// resolve through one translator: ui.Funcs' T takes the localizer-first
// form the partials now use, and a one-argument T registered here
// could not.
var catalog = func() *rastrillo.Locales {
	l, err := rastrillo.NewLocales([]string{locales.DefaultLocale}, locales.DefaultLocale, locales.BaseCatalog, nil)
	if err != nil {
		panic(err) // the code is generated and checked at generate time
	}
	return l
}()

func catalogT(key string, args ...any) string { return catalog.Tf(locales.DefaultLocale, key, args...) }
```

Delete `genT`. Update the comments that described `genT`. The Go preflight reports every `ui.WithT` call as a per-request binding to review, so change `gocheck_test.go`'s expectation for each of the three examples from one `function map entry "T"` finding to exactly one `ui.WithT (a per-request T binding)` finding; Tasks 23 and 24 take it to zero.

- [ ] **Step 5: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'Test[^B]' -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/designsystem/ ./cmd/dsgen/ ./internal/generate/ -count=1 -short
make example-blog example-tickets example-notes scaffold-smoke
```
Expected: PASS. Every ui golden is unchanged: the partials now call `T $L …`, and with no `L` in their data `ui.Funcs` answers exactly as before (Task 9's guarantee). If an example's golden changes because a framework key now renders its English instead of the bare key, update that golden and say so in the commit body.

- [ ] **Step 6: Commit**

```bash
git add ui/partials ui/layouts internal/tmplmigrate internal/generate/templates_test.go examples
git commit -F - <<'MSG'
Migrate ui's partials and layouts to localizer-first calls

Done by rastrillo migrate-templates, so every edit was validated by
re-parsing; the review sites were checked by hand. Output is unchanged
for every existing caller, because ui.Funcs answers a call with no
localizer exactly as before.

The three examples registered their own one-argument T, which a
migrated partial cannot call; they now pass their generated catalog
through ui.WithT until they move to ui.Pages. Framework keys in those
examples now render their English instead of the bare key.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 13: `ui.Pages`: one boot-time set, strict by default

Spec: §10 (the decision; binding and the risk; "Strict in production; compatibility only on request"; "What lives in the function map"; Measured; "Budgets, as implementation tests" 1-4; "Tests in the prototype"), §3 (`Config` feeding `ui.Pages`; the zh-TW integration), §4 ("The locale comes from the localizer"), Appendix C.2 (`op.pages.*`).
Starts from (deleted from the tree in Task 11; read them with `git show $(git merge-base HEAD origin/main):<path>`): `hack/templatemem/pages/pages.go` (all), `pages/pages_test.go` (all ten tests), `pages/integration_test.go`, `hack/templatemem/main.go:220-350` (the representative form page and the per-request measurement); and, still in the tree until Task 30, `hack/templatemem/testdata/scaffold/`.

**Files:**
- Create: `ui/pages.go`, `ui/pages_test.go`, `ui/pages_budget_test.go`, `ui/pages_integration_test.go`, `ui/pages_errorpage_test.go`
- Modify: `localemw.go` (the resolution order moves into an exported `(*Locales).Negotiate(r)`, which `Middleware` calls), `localemw_test.go`
- Create: `ui/testdata/pages/` (fixture app: the prototype's `raw` fixtures and the scaffold's four templates, migrated with `rastrillo migrate-templates -write` when copied)
- Modify: `Makefile` (`race` target gains `./ui/ -run '^TestPages'`)

**Interfaces:**
- Consumes: `Localizer`, `NewLocalizer`, `localizer`, `withL`, `pick`, `Funcs`, `LocaleFuncs`, `WithLocalizer` (Task 9); `locales.Config` (Task 3); `rastrillo.NewLocales`, `rastrillo.Catalog`, `rastrillo.LocaleFrom`; `currency.Format`, `currency.AmountFromAny` (Task 4).
- Produces (package `ui`):

```go
type PagesConfig struct {
	App      fs.FS             // the app's templates
	Layout   string            // the layout file in App, parsed into the base
	Shared   []string          // other shared files in App, parsed into the base
	Pages    map[string]PageFile // page name -> the page's own file
	Entry    string            // the template Execute runs; "layout" when empty
	Locale   locales.Config    // locales.CheckConfig's result
	Catalog  rastrillo.Catalog // Options.BaseCatalog
	LocaleFS fs.FS             // Options.LocaleFS: locales/<code>.toml
	Funcs    template.FuncMap  // the app's request-independent functions (icon, iconAssets, asset)
	Compat   bool              // opt-in, for apps mid-migration; logged loudly
	Logger   *slog.Logger      // nil means slog.Default()
}

// PageFile is one page's own template file. FS nil means App: the
// examples read pages from more than one tree (gen/templates and an
// ejection root), so each page names its own.
type PageFile struct {
	FS   fs.FS
	Path string
}

type Pages struct { /* unexported */ }

var ErrNoLocalizer error // text: op.pages.no_localizer
var ErrLegacyCall error  // text: op.pages.legacy_call

func NewPages(cfg PagesConfig) (*Pages, error)
func (p *Pages) Localizer(identity string) *Localizer // the default's for anything not offered; never allocates
func (p *Pages) Execute(w io.Writer, page string, data any) error
func (p *Pages) Middleware(next http.Handler) http.Handler // puts p.LocalizerFor(r) on the request (LocalizerFrom)

// LocalizerFor is the request's localizer wherever the request is:
// LocalizerFrom(r) when the middleware ran; else the locale the locale
// middleware put on the context (rastrillo.LocaleFrom); else the
// request resolved directly, by the same rules (URL prefix, cookie,
// Accept-Language, default) through the catalogs' Negotiate. Error
// pages use it: Serve's panic recovery wraps the locale middleware and
// calls Options.ErrorPage with the original request, and handlers call
// ErrorPage directly, so neither can rely on the middleware's context.
func (p *Pages) LocalizerFor(r *http.Request) *Localizer
```

Root, in `localemw.go`: `func (l *Locales) Negotiate(r *http.Request) string`: the declared code for `r` by URL prefix, then cookie, then `Accept-Language`, then the default, without touching the context. `Middleware` calls it (behaviour unchanged; `localemw_test.go` keeps passing), and `Pages` holds the `*rastrillo.Locales` it built so `LocalizerFor` can call it.

`NewPages` behaviour: identities are `cfg.Locale.Offered`, or `[]string{cfg.Locale.Default}` when none are offered (a one-locale app); one `rastrillo.NewLocales(identities, Default, base, LocaleFS)`, where `base` is `rastrillo.BaseCatalog()` with `cfg.Catalog` copied over it, exactly the merge `buildHandler` does (`serve.go:565-573`): without the framework's English underneath, a framework key in an offered locale the framework does not translate (`de`) would render as the raw key under Pages while rendering in English under `Serve`'s own translator; one `NewLocalizer(id, cfg.Locale.Formats[id], func(k string, a ...any) string { return catalogs.Tf(id, k, a...) })` per identity, refusing an identity with no format (Appendix C `op.pages.no_format`, second copy review); the function map is `Funcs()` with the seven locale entries replaced by strict/compat dispatchers and `cfg.Funcs` added after refusing any name in `LocaleFuncs`, `localizer` or `withL` (`op.pages.reserved_func`); the base is `ui.Templates()` (`*.html`) then `Layout` and `Shared` from `App`, parsed once; one `Clone` per page with its file; the base is dropped. Nothing is cloned, re-parsed or rebound after `NewPages` returns.

Strict dispatch: a localizer-form call with a nil `$L` is `ErrNoLocalizer` naming the function (`op.pages.nil_localizer`); any legacy-form call is `ErrLegacyCall` naming it; `money` without `$L` first is `op.money.first_arg`. Compat: both render with the default identity's localizer (the app's own catalog, not the framework's English), logged once at `NewPages` (`op.pages.compat_on`) and once per function (`op.pages.compat_call`).

`Execute`: refuses data without a localizer (`ErrNoLocalizer` plus `op.pages.page_data`); executes `Entry` into a buffer from a `sync.Pool` and copies it to `w` only on success.

- [ ] **Step 1: Write the failing tests**

Build the fixture app in `ui/testdata/pages/`: write the prototype's `raw` map (`pages_test.go:34-53`) as files (`layout.html`, `corpus.html`, `who.html`, `anon.html`, `legacy.html`, `nil.html`, `nilnew.html`, `arity.html`) and copy `hack/templatemem/testdata/scaffold/*.html` beside them as `scaffold/*.html`. Migrate every file except `legacy.html`, `nil.html`, `nilnew.html` and `arity.html` (`keepRaw`, unmigrated or wrong on purpose):

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies
GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates -write ui/testdata/pages/layout.html ui/testdata/pages/corpus.html ui/testdata/pages/who.html ui/testdata/pages/anon.html ui/testdata/pages/scaffold
```

Add `ui/testdata/pages/locales/en.toml` and `es.toml` with the prototype's `appKeys` (`pages_test.go:72-77`): `app.title` Shop/Tienda, `app.hello`, `app.delete`, and an es-only `app.only`.

`ui/pages_test.go` (package `ui`; it blank-imports `locales/de` for the test below; `en`, `es` and `ar` come from `ui/packs_test.go`, Task 9; packs import only `locales`, so this is safe). Port, renaming each to start with `TestPages` so the race target can select them:
- `TestCorpusExecutes` as `TestPagesCorpusExecutes`: every expected substring unchanged (es `1.234.567` and `1284,50\u00a0€`; en `1,234,567` and `€1,284.50`; `#ZgotmplZ`; the `<bdi>`-escaped `Tbdi`; the app title and `field-date`'s framework strings in each language), the map path through `field-daterange` and the struct path through `Localized`;
- `TestStrictFailures` as `TestPagesStrictFailures` (struct without `L` gives `ErrNoLocalizer`; `legacy` gives `ErrLegacyCall`; `arity` errors; page data without `L`; an `L` that is a string);
- `TestCompatUsesTheAppsDefault` as `TestPagesCompatUsesTheAppsDefault`: Spanish-default app, the unmigrated `{{T "app.only"}}` renders `Hola`-equivalent es-only text, and the logs contain `COMPATIBILITY MODE` at construction and once for `T`;
- `TestNilValuesKeepTodaysBehaviour` as `TestPagesNilValues`: compare against Task 9's recorded `wantLegacyEN` segment for `[{{number .Nil}}][{{displayValue .Nil}}]` (compat, legacy form) and `[{{number $L .Nil}}][{{displayValue $L .Nil}}]` (strict);
- `TestTwoLanguagesConcurrentlyIsolated` as `TestPagesTwoLanguagesIsolated`, unchanged (200 goroutines, own title only, own user only, exactly one `user-`);
- `TestJunkLocaleAllocatesNothing` as `TestPagesLocalizerAllocatesNothing`;
- `TestNewRefusals` as `TestPagesNewRefusals` (app `T`, app `localizer`, app `withL`, duplicate offered identity, syntax error, an identity missing from `Formats`, an unreadable `locales/es.toml`);
- `TestPagesKeepTheirOwnDefinitions`, unchanged.

Add:

```go
// The framework's English sits under every Pages catalog, as it does
// under Serve's own: a locale the framework does not translate still
// renders its strings in English, never as raw keys.
func TestPagesFrameworkEnglishUnderneath(t *testing.T) {
	cfg, err := locales.CheckConfig("en", []string{"en", "de"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPages(PagesConfig{App: fstest.MapFS{
		"layout.html": {Data: []byte(`{{define "layout"}}{{$L := localizer $}}{{T $L "rastrillo.ui.cancel"}}{{end}}`)},
		"home.html":   {Data: []byte(``)},
	}, Layout: "layout.html", Pages: map[string]PageFile{"home": {Path: "home.html"}}, Locale: cfg})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := p.Execute(&b, "home", map[string]any{"L": p.Localizer("de")}); err != nil || b.String() != rastrillo.BaseCatalog()["rastrillo.ui.cancel"] {
		t.Errorf("de: %q, %v; want the framework's English", b.String(), err)
	}
}

// Review Focus 4: html/template writes as it executes. A strict failure
// halfway down a page must not leave half a page on the wire under a
// 200; the caller gets the error and writes its own 500.
func TestPagesExecuteWritesNothingOnError(t *testing.T) {
	p := strictPages(t)
	var w strings.Builder
	err := p.Execute(&w, "anon", page(p, "en", "u")) // "who" reached with a struct lacking L
	if !errors.Is(err, ErrNoLocalizer) || w.Len() != 0 {
		t.Errorf("err %v, wrote %d bytes; want ErrNoLocalizer and nothing written", err, w.Len())
	}
}

// Review Focus 2: a cookie or Accept-Language names an identity the app
// does not offer, linked or not. The page renders as the default
// identity, whose localizer carries the default's catalog and format.
func TestPagesLocalizerForUnofferedIdentity(t *testing.T) {
	p := strictPages(t) // offers en and es, default en
	for _, id := range []string{"de", "de-CH", "es-MX", "xx", "", "EN"} {
		if l := p.Localizer(id); l.Identity() != "en" || l.Format() != "en" {
			t.Errorf("Localizer(%q) = %s/%s, want the default en/en", id, l.Identity(), l.Format())
		}
	}
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l := LocalizerFrom(r); l == nil || l.Identity() != "en" {
			t.Errorf("Middleware put %v on a request with no locale", l)
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
```

`ui/pages_errorpage_test.go` (spec-silent, raised in review: error pages outside the middleware):

```go
// A panic is recovered outside the locale middleware and the Pages
// middleware, and handlers call ErrorPage directly; both must still
// render the error page in the request's language, and strictly.
func TestPagesErrorPagesFindTheRequestLanguage(t *testing.T) {
	p := strictPages(t) // offers en and es; has an "errors" page using ui's error-page partial
	errorPage := func(w http.ResponseWriter, r *http.Request, status int, ref string) {
		w.WriteHeader(status)
		if err := p.Execute(w, "errors", map[string]any{"L": p.LocalizerFor(r), "Status": status, "Ref": ref}); err != nil {
			t.Errorf("error page: %v", err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) { errorPage(w, r, 500, "r1") })
	h, closeFn, err := rastrillo.Handler(rastrillo.Options{Locales: []string{"en", "es"}, Mux: mux, ErrorPage: errorPage})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	es := rastrillo.BaseCatalogs()["es"]["rastrillo.ui.error_title"] // use the key ui's error-page partial renders
	for _, c := range []struct {
		path   string
		header map[string]string
	}{
		{"/es/boom", nil},
		{"/boom", map[string]string{"Accept-Language": "es"}},
		{"/boom", map[string]string{"Cookie": rastrillo.LocaleCookie + "=es"}},
		{"/es/fail", nil},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		for k, v := range c.header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), template.HTMLEscapeString(es)) {
			t.Errorf("%s %v: %d, body lacks the Spanish error title:\n%s", c.path, c.header, rec.Code, rec.Body.String())
		}
	}
}
```

Use the error-page partial's actual title key in place of `rastrillo.ui.error_title` if it differs. The `errors` page joins the fixture app (`ui/testdata/pages/errors.html`, migrated, calling `error-page` with `dict "L" $L …`).

`ui/pages_integration_test.go`: port `TestCheckConfigFeedsPagesAndCatalogs` as `TestPagesCheckConfigFeedsCatalogs` with `locales.CheckConfig("zh-TW", []string{"zh-TW", "en"})` fed straight into `PagesConfig.Locale`, catalogs in an in-memory `LocaleFS` (`locales/zh-TW.toml` with `price = "價格"`, `locales/en.toml` with `price = "Price"`), expected `"價格: $1,284.50"` and `"Price: NT$1,284.50"` exactly. It blank-imports `locales/en` and `locales/zh`.

`ui/pages_budget_test.go` (spec §10 budgets 1-3). These measure memory, so they are not parallel and skip under `-race`, whose instrumentation changes allocation. Every measurement first proves the work it measures succeeded: a template that fails early allocates less, and a budget must never pass because a page broke.

```go
//go:build !race

// servedAll builds Pages for the fixture app and the scaffold pages
// with the given offered locales, executes every page in every
// locale, and fails the test unless each execution succeeds and
// contains that page's marker in that locale's language.
func servedAll(t *testing.T, offered []string) *Pages {
	t.Helper()
	cfg, err := locales.CheckConfig("en", offered)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPages(budgetConfig(cfg)) // the fixture app plus ui/testdata/pages/scaffold
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range offered {
		for page, marker := range budgetMarkers(id) { // page -> a string only a correct render in id contains
			var b strings.Builder
			if err := p.Execute(&b, page, budgetData(p.Localizer(id), page)); err != nil {
				t.Fatalf("%s/%s: %v", id, page, err)
			}
			if !strings.Contains(b.String(), marker) {
				t.Fatalf("%s/%s rendered without %q; a broken page cannot be measured", id, page, marker)
			}
		}
	}
	return p
}

func heapAfter(f func() any) (int64, any) {
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	v := f()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	return int64(after.HeapAlloc) - int64(before.HeapAlloc), v
}

// Budgets 1 and 2, measured on the real structure at 1 and 12 offered
// locales. Pages retains templates (which must not grow per locale),
// one localizer per locale, and the catalogs. The catalogs alone are
// measured with the same codes through rastrillo.NewLocales, so the
// localizer's share is what remains.
func TestPagesBudgetMemoryAtOneAndTwelveLocales(t *testing.T) {
	twelve := rastrillo.BaseLocales()
	pages1, keep1 := heapAfter(func() any { return servedAll(t, []string{"en"}) })
	pages12, keep12 := heapAfter(func() any { return servedAll(t, twelve) })
	cat1, keepC1 := heapAfter(func() any { return mustLocales(t, []string{"en"}) })
	cat12, keepC12 := heapAfter(func() any { return mustLocales(t, twelve) })
	runtime.KeepAlive([]any{keep1, keep12, keepC1, keepC12})

	templates1, templates12 := pages1-cat1, pages12-cat12
	perLocale := (float64(templates12) - float64(templates1)) / 11
	t.Logf("Pages: %d B at 1 locale, %d B at 12; catalogs %d and %d; per extra locale beyond catalogs %.0f B",
		pages1, pages12, cat1, cat12, perLocale)
	if d := math.Abs(float64(templates12)-float64(templates1)) / float64(templates1); d > 0.10 {
		t.Errorf("template memory %d B at 1 locale, %d B at 12 (%.0f%%); budget 10%%", templates1, templates12, d*100)
	}
	if perLocale > 4096 {
		t.Errorf("each offered locale costs %.0f B beyond its catalog; budget 4 KB (1.4 KB measured in the spec)", perLocale)
	}
}

// Budget 3: per request, the scaffold overview page through Pages is
// within 1.5x the bytes of the same page on one escaped tree with no
// localization; Pages.Localizer allocates nothing.
func TestPagesBudgetPerRequest(t *testing.T) {
	p := servedAll(t, []string{"es", "en"})
	base := oneTreeOverview(t)
	es := p.Localizer("es")
	shared := bytesPerRun(t, func() (string, error) {
		var b strings.Builder
		err := p.Execute(&b, "overview", overviewData(es))
		return b.String(), err
	}, budgetMarkers("es")["overview"])
	oneTree := bytesPerRun(t, func() (string, error) {
		var b strings.Builder
		err := base.ExecuteTemplate(&b, "layout", overviewData(nil))
		return b.String(), err
	}, budgetMarkers("en")["overview"])
	if float64(shared) > 1.5*float64(oneTree) {
		t.Errorf("overview page: %d bytes per request, one-tree baseline %d; budget 1.5x", shared, oneTree)
	}
	if n := testing.AllocsPerRun(1000, func() { p.Localizer("es") }); n != 0 {
		t.Errorf("Pages.Localizer allocates %v", n)
	}
}

// bytesPerRun checks one run renders correctly, then measures 200.
func bytesPerRun(t *testing.T, f func() (string, error), marker string) uint64 {
	t.Helper()
	out, err := f()
	if err != nil || !strings.Contains(out, marker) {
		t.Fatalf("the measured render failed (%v) or lacks %q; refusing to measure it", err, marker)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 200; i++ {
		if _, err := f(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / 200
}
```

Write the helpers in the same file: `budgetConfig(cfg)` is the `PagesConfig` for the fixture app plus `ui/testdata/pages/scaffold` (overview, index, errors and the representative form page from `hack/templatemem/main.go:220-236`, migrated), with `locales/<code>.toml` catalogs for the app keys in every language where the fixture has them; `budgetMarkers(id)` maps each page to a string only a correct render in that language contains (the framework's `rastrillo.ui.*` value from `rastrillo.BaseCatalogs()[id]` that the page shows, or the app title); `budgetData(l, page)` is the map each page's handler would pass, with `"L": l`; `mustLocales(t, codes)` is `rastrillo.NewLocales(codes, "en", nil, <the same LocaleFS>)`; `oneTreeOverview` parses `ui.Templates()` plus the scaffold's unmigrated layout and overview (copy `hack/templatemem/testdata/scaffold/{layout,overview}.html` into `ui/testdata/pages/scaffold-legacy/`) with `Funcs()`; `overviewData(l)` is the scaffold overview handler's map, with `"L": l` when `l != nil`. The file blank-imports the twelve gallery packs.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run '^TestPages' -count=1`
Expected: FAIL to build (`undefined: NewPages`).

- [ ] **Step 3: Port the implementation**

Port `pages.go` into `ui/pages.go` with these changes from the prototype (its other error texts, "offered twice", "default is not offered", "no page", and the base and per-page parse wraps, become Appendix C `op.pages.offered_twice`, `op.pages.default_not_offered`, `op.pages.no_page`, `op.pages.base_parse` and `op.pages.page_parse`, from the second copy review): the config shape above (catalogs from `rastrillo.NewLocales`, formats from `locales.Config`, no `Partials` field because the base is always `ui.Templates()`); the localizer from Task 9 (no per-locale function map: `T`/`Tf`/`Tbdi`/`dateWords` call `l.t`, `number`/`displayValue` call `formatNumber(v, l.format, …)`, `money` calls `currency.Format(l.format, …)`); `Logf` replaced by the `*slog.Logger`; `Execute` through a pooled buffer; `Middleware`. Keep the prototype's package comment (moved onto `Pages`'s doc comment) explaining why the function map is never rebound: rebinding a map other requests are executing mixes outputs without any data race for `-race` to see.

Add `./ui/ -run '^TestPages'` to the `Makefile`'s `race` target as its own line: `CGO_ENABLED=1 go test -race -count=1 -run '^TestPages' ./ui/`. This is not a new suite role, so `@33` is unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run '^TestPages' -count=1 -v
CGO_ENABLED=1 GOFLAGS=-mod=mod go test -race -count=1 -run '^TestPages' ./ui/
```
Expected: PASS, both.

- [ ] **Step 5: Mutation checks (do not commit them)**

Make each change, run the named test, confirm it FAILS, revert:
1. In the strict dispatcher, return the default localizer for a nil `$L` instead of `ErrNoLocalizer`: `TestPagesStrictFailures` must fail.
2. Route the locale through shared state (store the last localizer in a package variable in `Execute` and read it in `T`): `TestPagesTwoLanguagesIsolated` must fail with mixed outputs.
3. Make `Execute` write straight to `w`: `TestPagesExecuteWritesNothingOnError` must fail.

Record the three results in the commit body.

- [ ] **Step 6: Commit**

```bash
git add ui/pages.go ui/pages_test.go ui/pages_budget_test.go ui/pages_integration_test.go ui/pages_errorpage_test.go ui/testdata/pages localemw.go localemw_test.go Makefile
git commit -F - <<'MSG'
Add ui.Pages: one template set for every language, strict by default

The base is parsed once and each page cloned once at boot; every
language and request executes those clones, and locale output comes
from the localizer in the data. Nothing is rebound after boot, because
rebinding a function map other requests are executing mixes one
request's language or user into another's output with no data race for
-race to report; isolation is asserted per output instead.

Strict is the production default: a locale call that reaches a
template without a localizer fails the request instead of rendering in
the wrong language, and Execute buffers so a failure sends nothing.
Compat mode renders with the app's own default catalog and says so in
capitals. Mutation checks: <the three results>.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 14: The request's language, direction and switcher in the shells

Spec: §3 ("With two or more locales, prefixes and the switcher turn on"; "New apps start with one locale … no switcher"), decision 5, §10 (request values come from the data).
Starts from: `ui/layouts/*.html` (each opens `<html lang="{{block "lang" .}}en{{end}}" dir="{{block "dir" .}}ltr{{end}}">`; topbar, sidebar and console have an empty `{{block "locale" .}}{{end}}`; column and stage have none), `ui/partials/locale-menu.html`, `localemw.go:203-240` (`LocaleItem`, `LocaleItems`), `locale.go:138` (`Dir`).

Today every shell says `lang="en" dir="ltr"` unless an app overrides two blocks, and no shell renders the switcher, so adding a second locale turns on prefixes but no way to change language, and an Arabic app lays out left to right. This task makes the shells take both from the request's data.

**Files:**
- Modify: `ui/layouts/{column,topbar,sidebar,console,stage}.html`
- Modify: `ui/localizer.go` (`func (l *Localizer) Dir() string`, which is `rastrillo.Dir(l.identity)`)
- Create: `ui/shell_locale_test.go`
- Modify: any shell golden or browser fixture whose rendered `<html>` line changes (`grep -rln 'lang="en" dir="ltr"' ui internal/designsystem --include=*_test.go`)

**Interfaces:**
- Consumes: `Localizer`, `localizer`, `opt` (Task 9), `ui.Pages` (Task 13), `rastrillo.LocaleItems`, `rastrillo.Dir`.
- Produces: the shells read two optional keys from the page data, `Locales` (`[]rastrillo.LocaleItem`, from `rastrillo.LocaleItems(r)`) and `Return` (the current path and query), and the localizer's identity and direction. A page whose data has no `Locales` renders no switcher (`locale-menu` renders nothing for empty items, so a one-locale app needs no condition). Tasks 23 to 25 put `Locales` and `Return` in their page data.

- [ ] **Step 1: Write the failing test**

`ui/shell_locale_test.go`:

```go
func TestShellsTakeLanguageDirectionAndSwitcherFromTheRequest(t *testing.T) {
	two := []rastrillo.LocaleItem{{Code: "en", Name: "English", Href: "/en/", Current: false}, {Code: "ar", Name: "العربية", Href: "/ar/", Current: true}}
	for _, shell := range LayoutNames() {
		for name, c := range map[string]struct {
			identity string
			items    []rastrillo.LocaleItem
			html     string
			switcher bool
		}{
			"one locale":  {"en", nil, `<html lang="en" dir="ltr">`, false},
			"two locales": {"es", two, `<html lang="es" dir="ltr">`, true},
			"Arabic":      {"ar", two, `<html lang="ar" dir="rtl">`, true},
		} {
			out := renderShell(t, shell, c.identity, map[string]any{"Locales": c.items, "Return": "/x"})
			if !strings.Contains(out, c.html) {
				t.Errorf("%s/%s: want %s in\n%.300s", shell, name, c.html, out)
			}
			if got := strings.Contains(out, "rst-locale"); got != c.switcher && shell != "stage" {
				t.Errorf("%s/%s: switcher present = %v, want %v", shell, name, got, c.switcher)
			}
		}
	}
}
```

The ui test binary links `en`, `es` and `ar` through `ui/packs_test.go` (Task 9), which has no build tag, so this test sees them in every build. `renderShell` builds a `Pages` over `Layout(shell)` plus a page whose `content` is `<p>x</p>`, with `locales.CheckConfig("en", []string{"en", "es", "ar"})`, and executes it with `"L": p.Localizer(identity)` added to the data. The stage shell (the sign-in screen) gets `lang`/`dir` but its switcher placement is asserted only where it renders one; give it a `locale` block like the others if the sign-in screen has room for it, and drop the `shell != "stage"` exception if so.

- [ ] **Step 2: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run TestShellsTakeLanguageDirectionAndSwitcherFromTheRequest -count=1`
Expected: FAIL (`lang="en"` for es and ar; no switcher).

- [ ] **Step 3: Implement**

In each shell, the two blocks' default bodies become (the block bodies are templates of their own, so each starts with the prelude; write them in the migrated syntax directly and confirm with `migrate-templates` that nothing is pending):

```
<html lang="{{block "lang" .}}{{$L := localizer $}}{{if $L}}{{$L.Identity}}{{else}}en{{end}}{{end}}" dir="{{block "dir" .}}{{$L := localizer $}}{{if $L}}{{$L.Dir}}{{else}}ltr{{end}}{{end}}">
```

The fallback keeps today's output for an app still rendering through `ui.Funcs` with no localizer. The existing empty `locale` blocks in topbar, sidebar and console get the default body `{{$L := localizer $}}{{template "locale-menu" dict "L" $L "Items" (opt $ "Locales") "Return" (opt $ "Return")}}`; column gains the same block as the first child of `<main rst-page>`, so a one-column app has its switcher at the top of the page. An app that overrides `lang`, `dir` or `locale` keeps its own, as block precedence already guarantees. Update shell goldens only where the `<html>` line or the new block's empty output changes them; nothing else may move.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./ui/ -run 'Test[^B]' -count=1
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ ./internal/designsystem/ -count=1 -short
GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates ui/layouts; echo "exit $?"
```
Expected: PASS; the dry run reports nothing pending.

- [ ] **Step 5: Commit**

```bash
git add ui internal/designsystem
git commit -F - <<'MSG'
Take each page's language, direction and switcher from the request

Every shell said lang="en" dir="ltr" unless an app overrode it, and
none rendered the switcher, so a second locale turned on /xx/ prefixes
with no way to change language, and an Arabic app laid out left to
right. The shells now read both from the request's localizer and the
switcher from the page data, which renders nothing for a one-locale
app.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 15: The event log's money upgrade: `MoneySchema`, `UpgradeForMoney`, column detection, upgrade boundary

Spec: §8 ("Two tripwires", "Which apps rename the event log", "eventlog detects its column at startup", "The pre-ledger path", "Tested through `migrate.Apply` on three start points", "An app that adds money later", "An app that removes money later", "Rollback", "Detecting legacy writes after the upgrade"), §12 item 11, Appendix C.2 (`op.eventlog.*`).
Starts from: `hack/templatemem/upgradeprobe/probe_test.go` (`moneySchema`, `payloadColumn`, `appendOld`, `ledger`, `TestEventlogMoneyUpgrade`, `TestEventlogWithoutMoneyIsUntouched`, `TestEventlogUpgradeRollbackUpgrade`, `TestEventlogRenameCost`).

**Files:**
- Create: `eventlog/money/migrations/0001_payload_v2.sql`
- Create: `eventlog/money.go`, `eventlog/money_test.go`
- Modify: `eventlog/eventlog.go` (`Open` detects the column; every query uses it)
- Delete: `hack/templatemem/upgradeprobe/` (ported here; its "today's code fails" assertions call the real `eventlog`, which now detects `payload_v2` and works, so they would go red), and the `Makefile`'s `money-cldr` line for `hack/templatemem` (nothing testable is left there; Task 30 deletes the module)

**Interfaces:**
- Consumes: `migrate.MustFromFS`, `migrate.Apply`, `migrate.Merge`.
- Produces (package `eventlog`):
  - `var MoneySchema *migrate.Set`: namespace `eventlog_money`, one migration `0001_payload_v2`, post-adoption
  - `type StreamWriter struct { Stream, Writer string }`
  - `func UpgradeBoundary(ctx context.Context, db *sql.DB) (map[StreamWriter]int64, error)`: the snapshot `MoneySchema` took; an error if the table does not exist (an app without money never calls it), whose text is Appendix C `op.eventlog.no_boundary` (second copy review)
  - `func (l *Log) PayloadColumn() string`: `"payload"` or `"payload_v2"`
  - `func UpgradeForMoney(ctx context.Context, db *sql.DB) error`: makes the event log money-ready whatever way the app applies its schema, and is what the generated `Boot` calls (Task 22). In order: if the column is already `payload_v2`, return nil; if the table has neither column, `op.eventlog.neither`. If the migration ledger (`rastrillo_migrations`) exists and has any row, the app applies its schema with `migrate.Apply`, so run `migrate.Apply(ctx, migrate.Pool(db), migrate.Merge(Schema, MoneySchema))`: with a non-empty ledger there is no adoption, `eventlog/0001_init` is `CREATE … IF NOT EXISTS` (a no-op if the app created the table some other way), and the rename and snapshot run as `eventlog_money/0001_payload_v2`, recorded in the ledger. Otherwise the app has no ledger (generated stores wired through `Options.Migrations`), so run `MoneySchema`'s SQL directly in one `BEGIN IMMEDIATE` transaction after re-checking the column inside it, creating no ledger (a ledger created here would make the app's own later `migrate.Apply` adopt a database it did not describe). Any error is wrapped in `op.eventlog.upgrade_refused`.
  - `Open(db, writer)` now reads `pragma_table_info('eventlog')` once per `*sql.DB` (cached after a successful detection only) and returns `op.eventlog.both` or `op.eventlog.neither` as errors

`eventlog/money/migrations/0001_payload_v2.sql`, exactly:

```sql
-- rastrillo: post-adoption
-- Only for apps whose events carry money (eventlog.MoneySchema). A
-- currency-unaware binary then fails on every read, append and ingest
-- instead of misreading amounts. The snapshot records, per stream and
-- writer, the last event written before the upgrade, so replay can
-- refuse an unversioned event an old binary appended afterwards.
ALTER TABLE eventlog RENAME COLUMN payload TO payload_v2;
CREATE TABLE eventlog_upgrade (
  stream TEXT    NOT NULL,
  writer TEXT    NOT NULL,
  seq    INTEGER NOT NULL,
  PRIMARY KEY (stream, writer)
);
INSERT INTO eventlog_upgrade (stream, writer, seq)
  SELECT stream, writer, MAX(seq) FROM eventlog GROUP BY stream, writer;
```

- [ ] **Step 1: Write the failing tests**

Port the four `upgradeprobe` tests into `eventlog/money_test.go` (package `eventlog_test`), replacing the in-test `moneySchema` with `eventlog.MoneySchema` and `withMoney` with `migrate.Merge(eventlog.Schema, eventlog.MoneySchema)`, keeping every assertion: the three start points (fresh; ledger-backed with events; pre-ledger with events and `Result.Adopted`), each ending with `payload_v2`, the ledger `eventlog/0001_init eventlog_money/0001_payload_v2`, events intact, and today's code failing (`appendOld` runs the old `INSERT … payload` SQL and must fail); the marker-removed variant reproducing "cannot be adopted safely"; the app without money untouched; backup with `VACUUM INTO`, upgrade, restore, check `payload` and a ledger of only `eventlog/0001_init`, upgrade again; the 200,000-event timing logged, not asserted. The prototype's `open(t)` used `db.Open`; use `openMem`/`openPath` (below) instead, because `eventlog` must not import `db`.

Add:

```go
func TestOpenDetectsThePayloadColumn(t *testing.T) {
	for name, ddl := range map[string]string{
		"payload":    "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload TEXT)",
		"payload_v2": "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload_v2 TEXT)",
		"both":       "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload TEXT, payload_v2 TEXT)",
		"neither":    "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT)",
	} {
		db := openMem(t)
		mustExec(t, db, ddl)
		l, err := eventlog.Open(db, "w1")
		switch name {
		case "payload", "payload_v2":
			if err != nil || l.PayloadColumn() != name {
				t.Errorf("%s: %v, %v", name, l, err)
			}
		default:
			if err == nil {
				t.Errorf("%s: Open accepted a table with %s", name, name)
			}
		}
	}
}

// Detection must not cache a failure: Open before the schema exists
// fails, and Open after migrate.Apply creates it succeeds.
func TestOpenBeforeSchemaThenAfter(t *testing.T) {
	db := openMem(t)
	if _, err := eventlog.Open(db, "w1"); err == nil {
		t.Fatal("Open succeeded with no eventlog table")
	}
	if _, err := migrate.Apply(context.Background(), migrate.Pool(db), eventlog.Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := eventlog.Open(db, "w1"); err != nil {
		t.Errorf("Open after the schema exists: %v", err)
	}
}

func TestUpgradeBoundarySnapshotsEveryPair(t *testing.T) {
	db := openMem(t)
	ctx := context.Background()
	migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema)
	for _, w := range []string{"w1", "w2"} {
		l, _ := eventlog.Open(db, w)
		for i := 0; i < 3; i++ {
			l.Append(ctx, "ticket/1", "updated", "human", map[string]any{"Price": 100})
		}
	}
	migrate.Apply(ctx, migrate.Pool(db), migrate.Merge(eventlog.Schema, eventlog.MoneySchema))
	b, err := eventlog.UpgradeBoundary(ctx, db)
	if err != nil || len(b) != 2 || b[eventlog.StreamWriter{"ticket/1", "w1"}] != 3 {
		t.Errorf("boundary = %v, %v", b, err)
	}
}
```

Two helpers in `money_test.go` (`eventlog` cannot import `db` or the root package's tests, so they open SQLite directly with the pragmas `rastrillo.OpenDB` uses):

```go
// openPath opens path as rastrillo.OpenDB does: busy_timeout before
// WAL, one connection. Each call is a separate handle, which is what
// the concurrent-start test needs.
func openPath(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// openMem is a fresh database file (in-memory SQLite loses the
// database across connections, so "mem" means "throwaway").
func openMem(t *testing.T) *sql.DB { return openPath(t, filepath.Join(t.TempDir(), "app.db")) }
```

The test file blank-imports `modernc.org/sqlite` for the driver.

The column cache must not outlive the rename on the same handle. Add:

```go
// A handle that opened the log before the money upgrade (Open cached
// "payload") must see payload_v2 afterwards: UpgradeForMoney drops the
// handle's cache entry when it renames.
func TestOpenAfterUpgradeOnTheSameHandle(t *testing.T) {
	ctx := context.Background()
	db := openMem(t)
	migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema)
	before, err := eventlog.Open(db, "w1")
	if err != nil || before.PayloadColumn() != "payload" {
		t.Fatalf("before: %v, %v", before, err)
	}
	if err := eventlog.UpgradeForMoney(ctx, db); err != nil {
		t.Fatal(err)
	}
	after, err := eventlog.Open(db, "w1")
	if err != nil || after.PayloadColumn() != "payload_v2" {
		t.Fatalf("after: %v, %v", after, err)
	}
	if _, err := after.Append(ctx, "s/1", "created", "human", map[string]any{"V": 2}); err != nil {
		t.Errorf("append after the upgrade: %v", err)
	}
}
```

`UpgradeForMoney` across every way an app can hold its event log:

```go
func TestUpgradeForMoneyEveryStartPoint(t *testing.T) {
	ctx := context.Background()
	for name, setup := range map[string]func(*sql.DB){
		"fresh, ledger app":       func(db *sql.DB) { migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema) },
		"ledger app with events":  func(db *sql.DB) { migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema); appendOld(t, ctx, db) },
		"Options.Migrations app": func(db *sql.DB) {
			for _, m := range eventlog.Schema.All() {
				mustExec(t, db, m.SQL)
			}
			mustExec(t, db, `CREATE TABLE IF NOT EXISTS ticket_types (id INTEGER PRIMARY KEY, name TEXT)`) // other tables, no ledger
			appendOld(t, ctx, db)
		},
		"ledger app, event log made outside it": func(db *sql.DB) {
			migrate.Apply(ctx, migrate.Pool(db), migrate.MustFromFS(fstest.MapFS{"migrations/0001_x.sql": {Data: []byte("CREATE TABLE x (id INTEGER);")}}, "app"))
			for _, m := range eventlog.Schema.All() {
				mustExec(t, db, m.SQL)
			}
			appendOld(t, ctx, db)
		},
	} {
		db := openMem(t)
		setup(db)
		for i := 0; i < 2; i++ { // the second call is a no-op
			if err := eventlog.UpgradeForMoney(ctx, db); err != nil {
				t.Fatalf("%s, call %d: %v", name, i+1, err)
			}
		}
		if col := payloadColumn(t, db); col != "payload_v2" {
			t.Errorf("%s: column %s", name, col)
		}
		if _, err := eventlog.UpgradeBoundary(ctx, db); err != nil {
			t.Errorf("%s: no boundary: %v", name, err)
		}
		if appendOld(t, ctx, db) == nil {
			t.Errorf("%s: today's append still works after the upgrade", name)
		}
		ledgered := strings.Contains(strings.Join(ledger(t, db), " "), "eventlog_money/0001_payload_v2")
		if ledgerExpected := !strings.Contains(name, "Options.Migrations"); ledgered != ledgerExpected {
			t.Errorf("%s: ledger records the rename = %v, want %v", name, ledgered, ledgerExpected)
		}
	}
}

// Two instances starting at once against the same Options.Migrations
// database: one renames, the other finds payload_v2 and does nothing.
func TestUpgradeForMoneyConcurrentStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	seed := openPath(t, path)
	for _, m := range eventlog.Schema.All() {
		mustExec(t, seed, m.SQL)
	}
	seed.Close()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = eventlog.UpgradeForMoney(context.Background(), openPath(t, path)) }(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Errorf("concurrent starts: %v, %v", errs[0], errs[1])
	}
}
```

(`appendOld`, `payloadColumn` and `ledger` are the prototype's helpers, ported with the tests above; `appendOld` returns the old `INSERT … payload` error.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./eventlog/ -count=1`
Expected: FAIL to build (`undefined: eventlog.MoneySchema`).

- [ ] **Step 3: Implement**

`eventlog/money.go`: `//go:embed money/migrations/*.sql`, `fs.Sub(…, "money")`, `MoneySchema = migrate.MustFromFS(sub, "eventlog_money")`, `StreamWriter`, `UpgradeBoundary`, `UpgradeForMoney` as specified, and a package-level `columns sync.Map` (`*sql.DB` to `string`). In `eventlog.go`, `Open` detects (`SELECT name FROM pragma_table_info('eventlog') WHERE name IN ('payload','payload_v2')`), stores the column on `Log`, and every query that names `payload` uses `l.col` (two fixed identifiers, never user input, so building the SQL with it is safe; say so in a comment). `UpgradeForMoney` calls `columns.Delete(db)` after it renames, so the next `Open` on that handle detects again. Comment on the cache: "Once per handle: the column changes only in UpgradeForMoney, which drops this handle's entry, or in a migration applied through another handle before this process opened the log; a failed detection is not cached, so Open works once the schema exists. A Log value opened before the upgrade keeps the column it saw: generated apps open their logs only after gen.Boot (Task 20), and hand-written apps must do the same." The map holds one small entry per handle an app ever opened; apps open a handful, so it is never evicted.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./eventlog/ -count=1 -v -run 'Money|Open|Boundary|Eventlog'`
Expected: PASS; the rename-cost test logs its timing.

- [ ] **Step 5: Commit**

```bash
git add eventlog Makefile
git rm -r -q hack/templatemem/upgradeprobe
git commit -F - <<'MSG'
Add eventlog.MoneySchema: the event log's money tripwire

Only apps whose events carry money include it, and that is declared,
never guessed from data: apps without money keep payload and upgrade
normally. The rename makes an old binary fail on every read and write
instead of misreading amounts, and it is post-adoption so a database
from before the ledger is adopted against eventlog.Schema first.

The same migration snapshots the last pre-upgrade event of every
stream and writer, because the log has no global position and replay
needs to tell old history from an old binary writing after the
upgrade. Open detects which column it has, so an app that later drops
its money fields keeps working.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 16: `currency/replay`: the money rules for events, explicit clears, and the upgrade boundary

Spec: §7 ("Event stores": every new event carries `V: 2`; version before kind; "a currency if and only if an amount"; "unchanged" after `Normalize`; why "adds money later" is replayable; version 1), §8 ("Detecting legacy writes after the upgrade"), §12 items 6 and 13, Appendix C.2 (`op.replay.*`).
Starts from: `hack/moneycldr/replay/replay.go`, `replay/replay_test.go` (both tests, all cases).

**Files:**
- Create: `currency/replay/replay.go`, `currency/replay/replay_test.go`
- Delete: `hack/moneycldr/replay/` (ported)
- Modify: `Makefile` (`GORM_FREE` gains `./currency/replay`)

**Interfaces:**
- Consumes: `currency.Normalize`, `currency.Rescale`, `currency.Legacy` (Task 4); `eventlog.StreamWriter`, `eventlog.Event` (Task 15).
- Produces (package `amadan.net/rastrillo/rastrillo/currency/replay`):
  - `const Version = 2`
  - `type Event struct { V *int; Kind string; Amounts map[string]int64; Currencies map[string]string; Cleared map[string]bool }`
  - `type Field struct { Minor *int64; Currency *string }`
  - `type Money struct { Field, AmountKey, CurrencyKey string }`: one money field and its two payload keys (`Price`, `"price"`, `"price_currency"`)
  - `func Decode(ev eventlog.Event, fields []Money) (Event, error)`: reads `V` (absent is nil; anything but an integer is an error, Appendix C `op.replay.bad_version`, second copy review) and, per field, each key as absent, JSON `null`, or a value; both keys `null` sets `Cleared[field]`; one `null` with the other absent or set is `op.replay.half_clear`
  - `type Rules struct { Fields []Money; Legacy *currency.Legacy; Boundary map[eventlog.StreamWriter]int64; HasBoundary bool }`
  - `func (r Rules) Apply(rec map[string]Field, i int, ev eventlog.Event) error`: `Decode`, then for an event with no `V`, `CheckUnversioned` (when `HasBoundary`), then `Step`. Every fold a generated store does (`Get`, `List`, the boot check) goes through `Apply`, so an unversioned event ingested after the app started is refused on the next read, not only at the next boot.
  - `func Step(rec map[string]Field, i int, ev Event, fields []string, legacy *currency.Legacy) error`: applies one event to `rec` (which it mutates); `i` is the event's index, for messages
  - `func Replay(events []Event, fields []string, legacy *currency.Legacy) (map[string]Field, error)`: `Step` over all
  - `func CheckUnversioned(boundary map[eventlog.StreamWriter]int64, ev eventlog.Event) error`: for an event whose payload has no `V`: an error (`op.replay.after_upgrade`) when its pair has no boundary row or its `Seq` is above it

The generated mergeable store (Task 22) folds every read through `Rules.Apply`.

**Clearing an optional money field (a resolution; the spec's "neither means unchanged" leaves no way to empty a field).** A version-2 event that clears a field carries both of its keys with JSON `null`: `{"V":2,"price":null,"price_currency":null}`. `Step` then makes the field empty (both nil). Chosen over an explicit clear marker or a new event kind because absent keeps meaning "unchanged" for every event already written, so no history changes meaning; because a clear pairs its two nulls exactly as SQL stores do (`NULL`/`NULL`), keeping "a currency if and only if an amount" true of every event; and because it needs no new kind that older version-2 readers would have to learn. A half clear (one key `null`) is a replay error. A version-1 event with a `null` money key is a replay error too: legacy writers omitted empty values and never wrote `null`.

- [ ] **Step 1: Write the failing tests**

Clears, decoding and the boundary on every fold (add to `replay_test.go`):

```go
var priceMoney = []Money{{Field: "Price", AmountKey: "price", CurrencyKey: "price_currency"}}

func ev(stream, writer string, seq int64, payload string) eventlog.Event {
	return eventlog.Event{Stream: stream, Writer: writer, Seq: seq, Kind: "updated", Payload: json.RawMessage(payload)}
}

func TestClearAndHalfClear(t *testing.T) {
	r := Rules{Fields: priceMoney}
	rec := map[string]Field{}
	for i, e := range []eventlog.Event{
		ev("t/1", "w", 1, `{"V":2,"price":1250,"price_currency":"EUR"}`),
		ev("t/1", "w", 2, `{"V":2,"title":"x"}`),                          // absent: unchanged
		ev("t/1", "w", 3, `{"V":2,"price":null,"price_currency":null}`), // explicit clear
	} {
		if err := r.Apply(rec, i, e); err != nil {
			t.Fatal(err)
		}
		if i == 1 && amt(rec["Price"]) != "EUR 1250" {
			t.Errorf("an absent key changed the field: %s", amt(rec["Price"]))
		}
	}
	if amt(rec["Price"]) != "empty" {
		t.Errorf("after a clear: %s", amt(rec["Price"]))
	}
	for _, bad := range []string{
		`{"V":2,"price":null}`,
		`{"V":2,"price":null,"price_currency":"EUR"}`,
		`{"V":2,"price":5,"price_currency":null}`,
		`{"price":null}`,       // version 1 never wrote null
		`{"V":"2","price":5}`, // V must be an integer
	} {
		if err := r.Apply(map[string]Field{}, 0, ev("t/1", "w", 1, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// Review finding 4: the boundary applies to every fold, so an
// unversioned event ingested after the app started is refused by the
// next Get or List, not only by the next boot.
func TestRulesApplyEnforcesTheBoundary(t *testing.T) {
	r := Rules{Fields: priceMoney, Legacy: &currency.Legacy{Currency: "USD", Scale: 2},
		Boundary: map[eventlog.StreamWriter]int64{{Stream: "t/1", Writer: "w"}: 1}, HasBoundary: true}
	if err := r.Apply(map[string]Field{}, 0, ev("t/1", "w", 1, `{"price":1250}`)); err != nil {
		t.Errorf("a pre-upgrade legacy event: %v", err)
	}
	if err := r.Apply(map[string]Field{}, 1, ev("t/1", "w", 2, `{"price":1250}`)); err == nil {
		t.Error("an unversioned event after the boundary was accepted")
	}
}
```

Port `replay_test.go` verbatim into package `replay`, changing `&Legacy{"USD", 2}` to `&currency.Legacy{Currency: "USD", Scale: 2}` and every expected error substring to Appendix C.2's approved `op.replay.*` wording (the prototype's substrings were drafts of the same messages). Every case stays, with its exact expected value (`"EUR 1250"`, `"JPY 12"`, `"KWD 12500"`, `"empty"`). Add:

```go
func TestStepMatchesReplay(t *testing.T) {
	events := []Event{
		v2("created", nil, nil),
		v2("updated", map[string]int64{"Price": 1250}, map[string]string{"Price": "EUR"}),
		v2("updated", nil, nil),
		v2("updated", map[string]int64{"Price": 1300}, map[string]string{"Price": "eur"}),
	}
	rec := map[string]Field{}
	for i, ev := range events {
		if err := Step(rec, i, ev, []string{"Price"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	all, err := Replay(events, []string{"Price"}, nil)
	if err != nil || amt(rec["Price"]) != "EUR 1300" || amt(all["Price"]) != "EUR 1300" {
		t.Errorf("Step %s, Replay %s, %v", amt(rec["Price"]), amt(all["Price"]), err)
	}
}

// Spec §12 item 6, last case: an unversioned event after the upgrade
// marker is a replay error, judged per stream and writer because the
// log has no global position.
func TestCheckUnversioned(t *testing.T) {
	boundary := map[eventlog.StreamWriter]int64{{Stream: "ticket/1", Writer: "w1"}: 3}
	for _, c := range []struct {
		stream, writer string
		seq            int64
		ok             bool
	}{
		{"ticket/1", "w1", 3, true},  // written before the upgrade
		{"ticket/1", "w1", 4, false}, // the same writer, after it
		{"ticket/1", "w2", 1, false}, // a writer that had written nothing before
		{"ticket/2", "w1", 1, false}, // a stream that did not exist before
	} {
		err := CheckUnversioned(boundary, eventlog.Event{Stream: c.stream, Writer: c.writer, Seq: c.seq})
		if (err == nil) != c.ok {
			t.Errorf("%s/%s/%d: %v, want ok=%v", c.stream, c.writer, c.seq, err, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/replay/ -count=1`
Expected: FAIL to build.

- [ ] **Step 3: Port the implementation**

Copy `replay.go`; split the loop body into `Step` (adding the `Cleared` rule: a version-2 clear empties the field and is allowed whether or not it had an amount); add `Money`, `Decode`, `Rules` and `Apply`; replace `proto.NormalizeCurrency` with `currency.Normalize`, the local `Rescale` with `currency.Rescale`, `Legacy` with `currency.Legacy`; messages from Appendix C.2; add `CheckUnversioned`. Keep the comment that the version is checked before the kind, and why (a newer writer's tombstone is as unreadable as its update). `git rm -r hack/moneycldr/replay`. Add `./currency/replay` to `GORM_FREE`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/... -count=1 && (cd hack/moneycldr && GOFLAGS=-mod=mod go vet ./...) && make gorm-free`
Expected: PASS.

- Mutation check (do not commit): move the version check after `if ev.Kind == "deleted" { continue }`; the "unknown version deleted" and "explicit version 0" cases must fail. Revert and record it in the commit body.

- [ ] **Step 5: Commit**

```bash
git add -A currency/replay hack/moneycldr Makefile
git commit -F - <<'MSG'
Add the replay rules for money in events

One rule for every version-2 event: a currency if and only if an
amount. That is what makes "adds a money field later" replayable:
history written before the field existed carries no amount, so the
field is simply empty until an event sets an amount with its currency.
A currency change is refused here; it needs its own event kind.

The version is checked before the kind, so a newer writer's tombstone
is refused like its updates. Mutation-checked: <result>.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 17: `currency/sqlmoney`: the SQL legacy step, added money columns, the pairing check and the version marker

Spec: §1 (stored shape; the one rule; no schema default; "Legacy is decided by the column"; added columns nullable; model fields are pointers), §7 ("Schema and boot", all bullets), §8 (the SQL tripwire row; mixed versions; version markers), §12 items 1, 2, 3 and 13, Appendix C.2 (`op.legacy.*`).
Starts from: `hack/templatemem/upgradeprobe/probe_test.go:265-287` (`TestMoneyColumnRename`).

**Resolution recorded here (see the reply's ambiguity list).** The spec renames `<f>` to `<f>_minor`. A rename keeps the old column's `NOT NULL DEFAULT 0`, while §1 requires an empty field to be NULL in both columns and the generated schema (Task 19) declares `<f>_minor INTEGER` nullable, with `*int64` model fields. On an upgraded table, writing an empty optional field would then fail with a NOT NULL error, and a fresh table and an upgraded one would differ. So the legacy step **adds** `<f>_minor INTEGER` and `<f>_currency TEXT`, copies each rescaled amount with its currency, and **drops** `<f>`, all in the one `BEGIN IMMEDIATE` transaction. The tripwire is the same: every query an old binary was generated with names `<f>`, which no longer exists, and its `CREATE TABLE IF NOT EXISTS` is a no-op on the existing table. The cost differs: `DROP COLUMN` rewrites the table, so it is proportional to the table's size rather than a schema-text change; it is still one transaction that rolls back whole.

**Files:**
- Create: `currency/sqlmoney/sqlmoney.go`, `currency/sqlmoney/sqlmoney_test.go`
- Modify: `Makefile` (`GORM_FREE` gains `./currency/sqlmoney`)

**Interfaces:**
- Consumes: `currency.Rescale`, `currency.Legacy`, `currency.Normalize` (Task 4).
- Produces (package `amadan.net/rastrillo/rastrillo/currency/sqlmoney`):
  - `const Version = 2`
  - `type Table struct { Name string; Fields []string; Legacy *currency.Legacy }`: `Fields` are SQL base names (`"price"` for columns `price_minor`/`price_currency`); `Legacy` is the resource's `[legacy]`, nil when undeclared
  - `func Upgrade(ctx context.Context, db *sql.DB, tables []Table) error`: the whole boot step; idempotent
  - `func CheckPairs(ctx context.Context, db *sql.DB, tables []Table) error`: the iff-rule check alone (also run inside `Upgrade`)

`Upgrade`, in order, on one pinned connection inside `BEGIN IMMEDIATE`:
1. `CREATE TABLE IF NOT EXISTS rastrillo_money (version INTEGER NOT NULL)`; read `MAX(version)`; above 2 is `op.legacy.newer`.
2. For each table and field: read `pragma_table_info`. If `<f>` exists the column is legacy: with `Legacy == nil`, count the rows: with any, fail with `op.legacy.undeclared`; with none there is nothing to rescale, so convert the empty column without a currency (this is the path of a fresh database whose released `CREATE TABLE` still declares the legacy column, Task 21); with `Legacy` set, add `<f>_minor INTEGER` and `<f>_currency TEXT` if absent, compute `currency.Rescale(old, Legacy.Scale, Legacy.Currency)` for every row with a non-NULL `<f>`, collect failures (`op.legacy.inexact`/`op.legacy.overflow`, at most 20 listed, with the row id), fail with `op.legacy.failed` plus the list if any, else write each row's minor amount and currency, then `ALTER TABLE <t> DROP COLUMN <f>` (an error there is `op.legacy.drop_blocked`). If `<f>` does not exist and neither new column does, the money field was added to a table that already existed (regenerating only rewrites `CREATE TABLE IF NOT EXISTS`, which adds no column to an existing table): add `<f>_minor INTEGER` and `<f>_currency TEXT`, both nullable with no default, so every existing row has no amount, and log `op.legacy.added_columns`. This is the additive migration for money, and it is why a generated app needs no hand-written migration when a manifest gains a money field (Task 21 tests the whole workflow). If exactly one of the two new columns exists, refuse with `op.legacy.pairs`.
3. The pairing check over every field: `SELECT COUNT(*) FROM <t> WHERE (<f>_minor IS NULL) <> (<f>_currency IS NULL)`; non-zero is `op.legacy.pairs`.
4. With no marker row, insert `version = 2`.
5. `COMMIT`. Any error before it rolls everything back.

Table and field names are generator constants, validated by the manifest's identifier rules; still, refuse any name that is not `[a-z0-9_]+` before interpolating it, with a comment naming why.

- [ ] **Step 1: Write the failing tests**

`currency/sqlmoney/sqlmoney_test.go` (package `sqlmoney_test`, blank-imports `modernc.org/sqlite` through `rastrillo.OpenDB` or opens with `sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")`). The old schema and queries are today's generated tickets store, copied literally:

```go
const oldSchema = `CREATE TABLE IF NOT EXISTS ticket_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  max_per_order TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const newSchema = `CREATE TABLE IF NOT EXISTS ticket_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  price_minor INTEGER,
  price_currency TEXT,
  status TEXT NOT NULL DEFAULT '',
  max_per_order TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

func legacyDB(t *testing.T, prices ...int64) *sql.DB {
	db := open(t)
	mustExec(t, db, oldSchema)
	for _, p := range prices {
		mustExec(t, db, `INSERT INTO ticket_types (name, price, created_at, updated_at) VALUES ('t', ?, 'x', 'x')`, p)
	}
	return db
}

func table(cur string, scale int) []sqlmoney.Table {
	return []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}, Legacy: &currency.Legacy{Currency: cur, Scale: scale}}}
}

func rows(t *testing.T, db *sql.DB) []string {
	var out []string
	rs, err := db.Query(`SELECT COALESCE(price_minor, 'NULL'), COALESCE(price_currency, 'NULL') FROM ticket_types ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	for rs.Next() {
		var m, c string
		rs.Scan(&m, &c)
		out = append(out, m+" "+c)
	}
	return out
}

// Spec §12 item 1, every case with an exact expected value.
func TestUpgradeLegacyRows(t *testing.T) {
	for _, c := range []struct {
		name   string
		prices []int64
		cur    string
		want   []string // "" for a refusal
		err    string
	}{
		{"USD to USD", []int64{1250, 0}, "USD", []string{"1250 USD", "0 USD"}, ""},
		{"USD scale 2 to JPY", []int64{1200}, "JPY", []string{"12 JPY"}, ""},
		{"JPY inexact", []int64{1200, 1250}, "JPY", nil, "1250 at scale 2 is 12.50, which JPY cannot hold"},
		{"to KWD", []int64{1250}, "KWD", []string{"12500 KWD"}, ""},
		{"SLL", []int64{1250}, "SLL", []string{"1250 SLL"}, ""},
		{"overflow", []int64{math.MaxInt64 / 5}, "KWD", nil, "too large"},
	} {
		db := legacyDB(t, c.prices...)
		err := sqlmoney.Upgrade(context.Background(), db, table(c.cur, 2))
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: %v, want %q", c.name, err, c.err)
			}
			var p int64
			if db.QueryRow(`SELECT price FROM ticket_types ORDER BY id LIMIT 1`).Scan(&p); p != c.prices[0] {
				t.Errorf("%s: a refused upgrade changed the database (price %d)", c.name, p)
			}
			continue
		}
		if err != nil || !slices.Equal(rows(t, db), c.want) {
			t.Errorf("%s: %v, %v; want %v", c.name, rows(t, db), err, c.want)
		}
	}
}
```

Replace the two error substrings with Appendix C.2's approved `op.legacy.inexact` (filled in) and `op.legacy.overflow` wording. Add:

```go
// A legacy column with no rows needs no [legacy]: nothing is rescaled.
// A fresh database created by a released CREATE TABLE that still has
// the legacy column (Task 21's append-only migrations) takes this path.
func TestUpgradeEmptyLegacyColumnNeedsNoLegacy(t *testing.T) {
	db := legacyDB(t) // no rows
	if err := sqlmoney.Upgrade(context.Background(), db, []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ticket_types (name, price_minor, price_currency, created_at, updated_at) VALUES ('a', 5, 'EUR', 'x', 'x')`); err != nil {
		t.Errorf("the final columns are not there: %v", err)
	}
}

func TestUpgradeRefusesUndeclaredLegacyRows(t *testing.T) {
	db := legacyDB(t, 1250, 300)
	err := sqlmoney.Upgrade(context.Background(), db, []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}}})
	if err == nil || !strings.Contains(err.Error(), "2 rows") || !strings.Contains(err.Error(), "[legacy]") {
		t.Errorf("undeclared legacy rows: %v; want a refusal naming the row count and the keys to add", err)
	}
}

func TestUpgradeIsIdempotentAndFreshGetsMarker(t *testing.T) {
	db := legacyDB(t, 1250)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := sqlmoney.Upgrade(ctx, db, table("KWD", 2)); err != nil {
			t.Fatalf("boot %d: %v", i+1, err)
		}
	}
	if got := rows(t, db); !slices.Equal(got, []string{"12500 KWD"}) {
		t.Errorf("after two boots: %v; the second must change nothing", got)
	}
	fresh := open(t)
	mustExec(t, fresh, newSchema)
	if err := sqlmoney.Upgrade(ctx, fresh, table("KWD", 2)); err != nil {
		t.Fatal(err)
	}
	var v int
	fresh.QueryRow(`SELECT version FROM rastrillo_money`).Scan(&v)
	if v != 2 {
		t.Errorf("fresh database marker %d, want 2", v)
	}
}

// Review Focus 1: two instances boot at once against a legacy
// database. BEGIN IMMEDIATE serialises them; the second must find the
// legacy column gone and rescale nothing, or KWD 12.500 becomes 125.000.
func TestUpgradeConcurrentBootsRescaleOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	seed := openPath(t, path)
	mustExec(t, seed, oldSchema)
	mustExec(t, seed, `INSERT INTO ticket_types (name, price, created_at, updated_at) VALUES ('t', 1250, 'x', 'x')`)
	seed.Close()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := openPath(t, path) // a separate handle per instance
			errs[i] = sqlmoney.Upgrade(context.Background(), db, table("KWD", 2))
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("boots: %v, %v", errs[0], errs[1])
	}
	if got := rows(t, openPath(t, path)); !slices.Equal(got, []string{"12500 KWD"}) {
		t.Errorf("after two concurrent boots: %v; want the amount rescaled exactly once", got)
	}
}

// Spec §12 item 2: an old binary pointed at the upgraded database
// fails every money query and changes no row. SQLite words the
// failures differently per statement, so each is asserted as it is.
// Spec §12 item 3 (the old generated handler, posted to) is Task 21's
// TestOldEditHandlerIsRefusedAfterUpgrade, which runs the real old code.
func TestOldBinaryQueriesFailAfterUpgrade(t *testing.T) {
	db := legacyDB(t, 1200)
	if err := sqlmoney.Upgrade(context.Background(), db, table("JPY", 2)); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, oldSchema) // the old binary's boot migration: a no-op on the existing table
	for q, want := range map[string]string{
		`UPDATE ticket_types SET name = 't', price = 1200, status = '', updated_at = 'y' WHERE id = 1`:                                   "no such column: price",
		`INSERT INTO ticket_types (name, price, status, max_per_order, created_at, updated_at) VALUES ('n', 5, '', '', 'x', 'x')`: "has no column named price",
		`SELECT id, name, price, status FROM ticket_types`:                                                                              "no such column: price",
	} {
		_, err := db.Exec(q)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("old query %q: %v; want %q", q, err, want)
		}
	}
	if got := rows(t, db); !slices.Equal(got, []string{"12 JPY"}) {
		t.Errorf("rows after the old binary's attempts: %v", got)
	}
}

// Spec §12 item 13 and §1: a money field added to a table that has
// rows. Upgrade adds the two columns itself (no hand migration), the
// existing rows are NULL/NULL, and boot passes; a row with exactly one
// of the two NULL fails boot.
func TestAddedMoneyFieldAndPairingCheck(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema) // no fee columns: the table predates the field
	mustExec(t, db, `INSERT INTO ticket_types (name, price_minor, price_currency, created_at, updated_at) VALUES ('a', 100, 'EUR', 'x', 'x')`)
	tables := []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price", "fee"}}}
	for i := 0; i < 2; i++ {
		if err := sqlmoney.Upgrade(context.Background(), db, tables); err != nil {
			t.Fatalf("boot %d with an added money field: %v", i+1, err)
		}
	}
	var feeMinor, feeCur sql.NullString
	db.QueryRow(`SELECT fee_minor, fee_currency FROM ticket_types`).Scan(&feeMinor, &feeCur)
	if feeMinor.Valid || feeCur.Valid {
		t.Errorf("an existing row got an amount for a field it never had: %v %v", feeMinor, feeCur)
	}
	mustExec(t, db, `UPDATE ticket_types SET fee_minor = 5`)
	if err := sqlmoney.CheckPairs(context.Background(), db, tables); err == nil {
		t.Error("an amount without a currency passed the pairing check")
	}
}

func TestUpgradeRefusesNewerData(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema)
	mustExec(t, db, `CREATE TABLE rastrillo_money (version INTEGER NOT NULL)`)
	mustExec(t, db, `INSERT INTO rastrillo_money VALUES (3)`)
	if err := sqlmoney.Upgrade(context.Background(), db, table("USD", 2)); err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Errorf("newer data: %v", err)
	}
}

func TestUpgradeDropBlockedRollsBack(t *testing.T) {
	db := legacyDB(t, 1250)
	mustExec(t, db, `CREATE INDEX ticket_types_price ON ticket_types (price)`)
	if err := sqlmoney.Upgrade(context.Background(), db, table("USD", 2)); err == nil {
		t.Fatal("upgrade succeeded with an index on the legacy column")
	}
	var p int64
	if err := db.QueryRow(`SELECT price FROM ticket_types`).Scan(&p); err != nil || p != 1250 {
		t.Errorf("after a blocked drop: price %d, %v; want the database untouched", p, err)
	}
}
```

`open(t)` is `openPath(t, filepath.Join(t.TempDir(), "app.db"))`; `openPath` opens with the `rastrillo.OpenDB` pragmas and `t.Cleanup(db.Close)`. If SQLite allows dropping an indexed column in the version the module pins, `TestUpgradeDropBlockedRollsBack` will fail at its first check; then use a view (`CREATE VIEW v AS SELECT price FROM ticket_types`) instead of the index, and keep the test.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/sqlmoney/ -count=1`
Expected: FAIL to build.

- [ ] **Step 3: Implement** `Upgrade` and `CheckPairs` as specified above. Pin one connection (`db.Conn(ctx)`), `ExecContext(ctx, "BEGIN IMMEDIATE")`, `defer` a `ROLLBACK` unless committed. The rows to rescale are read fully before any write (do not update while iterating a cursor on the same connection). Comment `BEGIN IMMEDIATE`: it takes the write lock before reading, so a second instance booting at the same moment waits and then finds the legacy column gone instead of rescaling it again.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./currency/sqlmoney/ -count=1 -v && make gorm-free`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add currency/sqlmoney Makefile
git commit -F - <<'MSG'
Add the SQL money upgrade: rescale legacy rows once, then trip old binaries

One transaction adds <f>_minor and <f>_currency, rescales every legacy
amount exactly into its declared currency, and drops <f>, so an old
binary's queries fail on the missing column instead of storing 1200
yen for twelve dollars. Any inexact or overflowing row, or legacy rows
with no [legacy] declared, refuse boot with nothing changed.

The column is replaced rather than renamed because a rename keeps the
old NOT NULL DEFAULT 0, which would make an empty optional field
unwritable on upgraded tables only, and would make fresh and upgraded
tables differ. BEGIN IMMEDIATE serialises two instances booting at
once, so an amount is never rescaled twice.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 18: Manifest: a default currency per resource and `[legacy]`

Spec: §7 ("Manifest": `currency`, `[legacy]`, "An ineligible default generates with a warning, so an unchanged manifest never stops generating", decision 10), §1 (eligible versus stored), Appendix C.2 (`op.generate.no_currency`, `bad_currency`, `ineligible`, `legacy_currency`, `legacy_scale`, `op.legacy.scale_missing`, `op.legacy.currency_missing`).
Starts from: `manifest.go:75-113` (`Resource`, `Validate`).

**`scale` must be explicit (review finding 1).** With `Scale int`, a `[legacy]` table that names only a currency decodes as scale 0, and every old `1250` (12.50 dollars) would become 125000 USD minor units. So `Scale` is a pointer: omitted is nil and refused with `op.legacy.scale_missing`; an explicit `scale = 0` is valid (old integers were whole units).

**Files:**
- Modify: `manifest.go` (root), `manifest_test.go`
- Modify: `cmd/rastrillo/generate.go` (print the ineligible warning)
- Modify: `internal/manifest/manifest_test.go` (TOML round trip of the new keys, including `[legacy]` with and without `scale`)
- Migrate, in this task, every fixture with a money field and no currency, because each becomes invalid here (found with `grep -rlnE 'Kind: *(rastrillo\.)?Money|kind *= *"money"|"kind": *"money"' --include=*.go --include=*.toml --include=*.json --include=*.golden . | grep -v hack`): `manifest_test.go` (`validResource` and every money case), `internal/manifest/manifest_test.go`, `internal/manifest/goeval_test.go` (Go-manifest fixtures), `internal/generate/actions_test.go`, `internal/generate/store_test.go`, `internal/generate/manifestgen_test.go` and their goldens, the example manifest in `cmd/rastrillo/new.go`'s manifest README, and `examples/tickets` (manifest and regenerated `gen/manifest.json`). Each gains `Currency: "USD"` (or `currency = "USD"`); the explicit missing-currency cases stay and must still be rejected. `form/fields_test.go` uses `form.Money`, not a manifest, and is untouched.

**Interfaces:**
- Consumes: `currency.Normalize`, `currency.EligibleDefault` (Task 4).
- Produces (root package):
  - `Resource.Currency string` (`toml:"currency" json:"currency,omitempty"`): the default currency for new records
  - `Resource.Legacy *Legacy` (`toml:"legacy" json:"legacy,omitempty"`)
  - `type Legacy struct { Currency string \`toml:"currency" json:"currency"\`; Scale *int \`toml:"scale" json:"scale"\` }`: `Scale` is a pointer so omission is detectable; Go manifests write `Scale: &two`
  - `func (r *Resource) HasMoney() bool`: any List column or Form field of kind `Money`
  - `Validate` normalises `Currency` and `Legacy.Currency` in place (trimmed, upper case), so every emitter reads canonical codes

`omitempty` keeps `gen/manifest.json` byte-identical for resources without money, as `Scope` already does.

- [ ] **Step 1: Write the failing tests**

In `manifest_test.go`:

```go
func TestValidateMoneyCurrency(t *testing.T) {
	base := func() Resource {
		return Resource{Name: "ticket_types", Route: "/t", Form: Form{Basics: []Field{{Name: "Price", Kind: Money}}}}
	}
	for name, c := range map[string]struct {
		mutate func(*Resource)
		err    string
	}{
		"missing currency":     {func(r *Resource) {}, "no currency"},
		"malformed currency":   {func(r *Resource) { r.Currency = "EURO" }, "currency"},
		"unsupported currency": {func(r *Resource) { r.Currency = "ITL" }, "currency"},
		"eligible":             {func(r *Resource) { r.Currency = " eur " }, ""},
		"stored-only default":  {func(r *Resource) { r.Currency = "SLL" }, ""}, // generates with a warning
		"legacy ok":            {func(r *Resource) { r.Currency = "JPY"; r.Legacy = &Legacy{Currency: "usd", Scale: scale(2)} }, ""},
		"legacy SLL":           {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "SLL", Scale: scale(2)} }, ""},
		"legacy scale 0":       {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "JPY", Scale: scale(0)} }, ""},
		"legacy scale omitted": {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD"} }, "scale"},
		"legacy currency omitted": {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Scale: scale(2)} }, "currency"},
		"legacy bad code":      {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "DEM", Scale: scale(2)} }, "[legacy] currency"},
		"legacy scale -1":      {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD", Scale: scale(-1)} }, "scale"},
		"legacy scale 19":      {func(r *Resource) { r.Currency = "EUR"; r.Legacy = &Legacy{Currency: "USD", Scale: scale(19)} }, "scale"},
	} {
		r := base()
		c.mutate(&r)
		err := r.Validate()
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %v, want %q", name, err, c.err)
		}
	}
	r := base()
	r.Currency = " eur "
	r.Validate()
	if r.Currency != "EUR" {
		t.Errorf("Validate left Currency %q; emitters must see the canonical code", r.Currency)
	}
}
```

with `func scale(n int) *int { return &n }` in the test file. In `internal/manifest/manifest_test.go`, decode TOML end to end: `[legacy]\ncurrency = "USD"\n` (no `scale`) must fail naming `scale`; `[legacy]\ncurrency = "JPY"\nscale = 0\n` must load with `*Scale == 0`; and `gen/manifest.json` round-trips both `0` and `2`.

Adjust the error substrings to Appendix C.2's approved wording. `[legacy]` scale is bounded to 0-18 because `Rescale` multiplies or divides by ten once per digit of difference, and 10^18 is the largest power of ten an `int64` holds; a larger scale can only ever be inexact or overflow.

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . -run TestValidateMoneyCurrency -count=1`
Expected: FAIL to build (`unknown field Currency`).

- [ ] **Step 3: Implement** the fields, `HasMoney`, and the checks in `Validate` (after the existing field checks). The approved messages begin `manifest/%s.toml: `; `Validate` returns each message without that leading location, because `internal/manifest.Load` already prefixes every `Validate` error with its source (`internal/manifest/manifest.go:77`: `fmt.Errorf("%s: %w", s.file, err)`), which is the real file for TOML manifests and the Go file for Go manifests. Writing it twice would print the path twice, and a resource-name path would be wrong for Go manifests. This splits approved text at its colon; it does not reword it. `cmd/rastrillo/generate.go`'s ineligible warning is printed outside `Load`, so it uses the whole approved text with the TOML path. In `cmd/rastrillo/generate.go`, after manifests load, print `op.generate.ineligible` to stderr for each resource whose `Currency` is set and `!currency.EligibleDefault(r.Currency)`; generation continues.

- [ ] **Step 4: Run the tests to verify they pass**

First add `currency = "USD"` and `[legacy]` with `currency = "USD"`, `scale = 2` to `examples/tickets/manifest/ticket_types.toml`, with a comment: "USD at scale 2 is what this resource stored before money had a currency; a database from then is upgraded once at boot." Regenerate it and stage the result (Global Constraints). Then run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . ./internal/manifest/ ./internal/generate/... ./cmd/rastrillo/ -count=1 && make generate-check example-tickets`
Expected: PASS, every migrated fixture included.

- [ ] **Step 5: Commit**

```bash
git add manifest.go manifest_test.go internal/manifest internal/generate cmd/rastrillo examples/tickets/manifest examples/tickets/gen
git commit -F - <<'MSG'
Give each money resource a default currency and a [legacy] setting

The default currency is per resource and decides new records only;
[legacy] is separate because the rows already stored were written in
whatever the app always meant, which may be neither today's default
nor a currency still issued. A default that is stored but no longer
issued generates with a warning, so an unchanged manifest never stops
generating.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 19: Generated templates in the localizer-first syntax

Spec: §10 ("Go-embedded templates (the generator, the scaffold, the gallery's samples) are emitted in the new syntax by their emitters, and the examples are regenerated"; "generated view structs gain the field"), §4.
Starts from: `internal/generate/templates.go` (every emitted `(T "…")`, `dict` and `{{template}}` call), `internal/generate/actions.go` (the view structs `formView`, `showView`, `listView`, and the filter views).

The examples still render through `ui.Funcs` (Task 12's bridge), where a nil `$L` answers as before, so regenerating them here changes no output. `ui.Pages` arrives in their render code in Tasks 23 and 24.

`view.Render`'s `RenderFunc` carries no request, so the app's render function cannot find the request's localizer for a generated view. Generated actions therefore set `Localized: ui.ForRequest(r)` themselves (the localizer, which `(*ui.Pages).Middleware` (Task 13) puts on the request, plus the switcher's items and return path the shells read in Task 14).

**Files:**
- Modify: `internal/generate/templates.go`, `internal/generate/actions.go`, their goldens and tests
- Modify: `internal/tmplmigrate/corpus_test.go` (the examples' counts lose their generated part)
- Regenerate: `examples/*/gen`

**Interfaces:**
- Consumes: `ui.Localized`, `ui.ForRequest` (Task 9).
- Produces: every generated template begins each body with `{{$L := localizer $}}`, passes `$L` to every locale call and `"L" $L` to every `dict`; every generated view struct embeds `ui.Localized` and every action sets it to `ui.ForRequest(r)`.

Target shape, for `examples/tickets/gen/templates/ticket_types/form.html` (the header comment's data contract gains Appendix C.6's `gen.template.contract_l` line):

```
{{define "content"}}{{$L := localizer $}}
{{if .IsNew}}
<form rst-form method="post" action="/admin/ticket_types">
{{template "field-text" dict "L" $L "Name" "Name" "Label" (T $L "resource.ticket_types.field.name") "Value" .Fields.Name "Error" (T $L (index .Errors "Name")) "Required" true}}
```

- [ ] **Step 1: Write the failing tests**

In `internal/generate/templates_test.go`:

```go
// Every emitted template is already in the localizer-first syntax: the
// codemod has nothing to do on generator output. If an emitter adds a
// call in the old syntax, this fails instead of an app failing in
// strict mode.
func TestEmittedTemplatesNeedNoMigration(t *testing.T) {
	for _, r := range fixtureResources(t) { // every resource shape the template tests already build
		for name, src := range map[string][]byte{"list": listHTML(r), "show": showHTML(r), "form": formHTML(r), "confirm": confirmHTML(r)} {
			out, st, err := tmplmigrate.Rewrite(name, string(src))
			if err != nil || out != string(src) || st.Calls+st.Dicts+st.Wrapped+st.Preludes != 0 || len(st.NoData) != 0 {
				t.Errorf("%s/%s: the codemod would change it (%+v, %v)", r.Name, name, st, err)
			}
		}
	}
}
```

(`fixtureResources` is whatever helper `templates_test.go` already uses to build its resources; if there is none, build three: text-only, with money, with filters and advanced fields.)

In `internal/generate/actions_test.go`, assert that every emitted action that renders a view sets `Localized: ui.ForRequest(r)` and imports `amadan.net/rastrillo/rastrillo/ui`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/ -run 'TestEmittedTemplatesNeedNoMigration|Localized' -count=1`
Expected: FAIL (the codemod reports calls on every emitted template).

- [ ] **Step 3: Implement**

In `templates.go`, change every emitted string: `(T "` to `(T $L "`, `dict "` to `dict "L" $L "`, each `{{define "…"}}` body to begin with `{{$L := localizer $}}`; a `{{template "x" .Something}}` whose data is not a `dict` becomes `{{template "x" (withL (.Something) $L)}}`. Easiest and safest: emit as today, then run the emitted text through `tmplmigrate.Rewrite` inside the emitter and fail generation on any error. That keeps one implementation of the syntax; do it in one helper `migrated(name string, src []byte) []byte` with a comment saying why. In `actions.go`, embed `ui.Localized` in each emitted view struct and set it in every literal. Regenerate the goldens (`go test ./internal/generate/ -update` if the package has that flag; otherwise follow the package's documented golden refresh). Regenerate the examples:

```bash
make build-cli && for e in blog tickets notes helloworld; do .build/rastrillo generate examples/$e; done
```

Run `TestRepoTemplates` and update its `examples` entry to the measured counts (the hand-written templates only); note the numbers in the commit body.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
go run ./cmd/rastrillo budget size   # over 5,000 in internal/generate: add the budgets.txt line (Global Constraints) in this commit
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/... ./internal/tmplmigrate/ -count=1
git add 'examples/*/gen'   # the regenerated tree is the baseline
make generate-check example-blog example-tickets example-notes example-helloworld
```
Expected: PASS, with every example test unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/generate .rastrillo/budgets.txt internal/tmplmigrate examples
git commit -F - <<'MSG'
Emit generated templates in the localizer-first syntax

The emitters run their output through the codemod, so there is one
implementation of the new syntax and the generated templates can never
drift from what migrate-templates writes. Generated views carry the
request's localizer themselves, because RenderFunc has no request and
the app's render function could not supply it.

Output is unchanged until the examples move to ui.Pages: ui.Funcs
answers a nil localizer exactly as before.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 20: A generated `Boot` that apps cannot skip, and pruning stale generated files

Spec: §7 ("The legacy step runs once … after `Migrations`", "Boot also checks the rule", "A boot-time replay check surfaces any of these errors before a request does"), §8 ("An app that adds money later", "An app that removes money later"). Review findings 2 and 3.
Starts from: `internal/generate/generate.go:292-330` (`Router`), `internal/generate/manifestgen.go:81-147` (`emitPipeline`), `:330-420` (`diffGenerated`, `extraLocaleFiles`), every caller of `gen.Router` (`grep -rn 'gen.Router' examples cmd/rastrillo internal --include=*.go`).

Money upgrades must run before an app serves, and nothing the generator writes is called unless the app's own code calls it. So the generated router stops being buildable without proof that the generated `Boot` ran: when an app has any manifest resource, `gen.Router` takes a `gen.Booted`, which only `gen.Boot` returns. An existing app that regenerates fails to compile at its `gen.Router(...)` call until it calls `Boot`, which is the one-line change its upgrade needs, and it cannot be forgotten. In this task `Boot` does nothing yet; Task 21 adds the SQL money upgrade to it and Task 22 the event log's, so each lands with its own tests and every app is already wired.

Generated output can also go stale: removing a resource, or a store changing shape, leaves files from earlier runs that nothing regenerates. Today's emitters only write, so such files survive and keep compiling. This task makes `generate` remove them, and `generate --check` report them.

**Files:**
- Create: `internal/generate/bootgen/bootgen.go` (emits `gen/boot.go`; a subpackage, because it needs only `rastrillo.Resource` and `internal/generate` has little source-line headroom), `internal/generate/boot_test.go` (scratch-app tests stay beside the other generator tests)
- Modify: `internal/generate/generate.go` (`Router` takes `Booted` when the app has manifest resources)
- Create: `internal/generate/prune/prune.go` (a subpackage for the same reason), `internal/generate/prune_test.go`
- Modify: `internal/generate/manifestgen.go` (collect every written path; prune; `--check` reports stale files), `cmd/rastrillo/generate.go` (print `op.generate.pruned`)
- Modify, because their contract changes here: `examples/tickets/cmd/tickets/main.go`, `examples/tickets/internal/ticketstest/harness_test.go`, `examples/blog/cmd/blog/main.go`, `examples/blog/internal/blogtest/harness_test.go`, `examples/notes/internal/notes/app.go`, every scratch app the generator and CLI tests build that calls `gen.Router`, and the scaffold's manifest README recipe in `cmd/rastrillo/new.go` (the code lines only; its prose is rewritten in Task 29). `examples/helloworld` has no manifest resource, so its router is unchanged.
- Regenerate: every example

**Interfaces:**
- Produces, in a generated app with at least one manifest resource:

```go
// Code generated by rastrillo generate; DO NOT EDIT.

package gen

// <Appendix C.6 gen.boot.booted>
type Booted struct{ db *sql.DB }

// <Appendix C.6 gen.boot.doc>
func Boot(ctx context.Context, db *sql.DB) (Booted, error) {
	return Booted{db: db}, nil
}
```

  and `func Router(b Booted, ctxFactory func(*http.Request) *rastrillo.Ctx) *http.ServeMux`. `Router` panics with `op.boot.not_booted` when `b.db == nil` (a zero `gen.Booted{}` written by hand compiles; the panic catches it at startup). `Booted` carries the database `Boot` checked, because a boolean would let `Boot(ctx, dbA)` vouch for a router whose contexts use `dbB`, which nothing checked or upgraded: after `ctxFactory` builds a request's `Ctx`, a nil `Ctx.DB` is set to `b.db`, and any other database is refused before dispatch with a 500 (body: `http.Error`'s existing `Something went wrong.`, the text `recoverPanics` already sends) and a log line carrying `op.boot.wrong_db`; the action never runs.
- `internal/generate/bootgen`: `func Emit(genDir string, rs []rastrillo.Resource) ([]string, error)`. `internal/generate/prune`: `func Prune(genDir string, written []string, exclusiveStores []string) (removed []string, err error)` and `func Stale(genDir string, written []string, exclusiveStores []string) ([]string, error)` (what `Prune` would remove, for `--check`); `exclusiveStores` are the store directories of resources that are exclusive in this run.

`Prune`'s rule: a file under `gen/` is removed when it starts with a "Code generated by rastrillo generate" header and was not written by this run, or it starts with sqlc's "Code generated by sqlc" header and its directory is not the store directory of a resource that is an exclusive (sqlc) store in this run. Keeping sqlc output merely because its directory received some generated file is wrong: a resource that changes from exclusive to mergeable still gets `store.go` and `migrations.go` in the same directory, and the old `db.go`, `models.go` and `queries.sql.go` would then declare a second `Queries`, `New` and model and stop the package compiling. A file without either header is never touched: it is the app's. The header check reads the first line in each generated file's comment syntax: `//` in Go, `--` in `schema.sql` and `queries.sql`, `#` in `sqlc.yaml`, and `{{/*` in generated templates (`gen/templates/**.html` begin `{{/* Code generated by rastrillo generate; DO NOT EDIT.`). JSON has no comments, so `gen/manifest.json` is kept or removed by path: it is generated exactly when a manifest resource exists. Directories left empty are removed.

- [ ] **Step 1: Write the failing tests**

`internal/generate/prune_test.go`:

```go
func TestRegeneratingAnExistingTreeRemovesWhatNothingGenerates(t *testing.T) {
	app := scratchApp(t, map[string]string{ // the manifestgen_test.go scratch-module helper
		"manifest/a.toml": resourceTOML("a", "exclusive"),
		"manifest/b.toml": resourceTOML("b", "mergeable"),
	})
	mustGenerate(t, app)
	os.WriteFile(filepath.Join(app, "gen", "store", "b", "notes.md"), []byte("mine"), 0o644) // a hand file in gen/
	os.Remove(filepath.Join(app, "manifest", "b.toml"))
	mustGenerate(t, app)
	for _, gone := range []string{"gen/store/b/store.go", "gen/store/b/migrations.go", "gen/templates/b", "gen/actions/b"} {
		if _, err := os.Stat(filepath.Join(app, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived regeneration", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(app, "gen", "store", "b", "notes.md")); err != nil {
		t.Errorf("a file without a generated header was removed: %v", err)
	}
	for _, kept := range []string{"gen/store/a/models.go", "gen/store/a/queries.sql.go", "gen/store/a/migrations.go"} {
		if _, err := os.Stat(filepath.Join(app, kept)); err != nil {
			t.Errorf("%s was removed though its store is still generated", kept)
		}
	}
}

// Review round two: a resource that changes from exclusive to
// mergeable keeps its store directory, but sqlc's files in it must go,
// or the package declares two Queries and two New and stops compiling.
func TestExclusiveToMergeableTransition(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	for _, f := range []string{"db.go", "models.go", "queries.sql.go"} {
		if _, err := os.Stat(filepath.Join(app, "gen", "store", "a", f)); err != nil {
			t.Fatalf("exclusive store lacks sqlc's %s: %v", f, err)
		}
	}
	os.WriteFile(filepath.Join(app, "manifest", "a.toml"), []byte(resourceTOML("a", "mergeable")), 0o644)
	if err := checkGenerate(app); err == nil {
		t.Error("--check passed before regenerating the changed store")
	}
	mustGenerate(t, app)
	for _, f := range []string{"db.go", "models.go", "queries.sql.go", "schema.sql", "queries.sql"} {
		if _, err := os.Stat(filepath.Join(app, "gen", "store", "a", f)); !os.IsNotExist(err) {
			t.Errorf("%s survived the change to a mergeable store", f)
		}
	}
	if out, err := goBuild(app); err != nil {
		t.Errorf("the app does not compile after the transition:\n%s", out)
	}
	if err := checkGenerate(app); err != nil {
		t.Errorf("--check after regenerating: %v", err)
	}
}

func TestCheckReportsStaleGeneratedFiles(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	stale := filepath.Join(app, "gen", "store", "old", "store.go")
	os.MkdirAll(filepath.Dir(stale), 0o755)
	os.WriteFile(stale, []byte("// Code generated by rastrillo generate; DO NOT EDIT.\npackage oldstore\n"), 0o644)
	if err := checkGenerate(app); err == nil || !strings.Contains(err.Error(), "gen/store/old/store.go") {
		t.Errorf("--check passed with a stale generated file: %v", err)
	}
}
```

`internal/generate/boot_test.go`:

```go
// An app with manifest resources cannot build its router without
// Boot: the wiring every existing app had stops compiling, and the
// new wiring builds.
func TestRouterRequiresBoot(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeMain(t, app, `mux := gen.Router(func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: db} })`)
	if out, err := goBuild(app); err == nil || !strings.Contains(out, "gen.Router") {
		t.Fatalf("the old wiring still builds:\n%s", out)
	}
	writeMain(t, app, `b, err := gen.Boot(context.Background(), db)
	if err != nil { panic(err) }
	mux := gen.Router(b, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: db} })`)
	if out, err := goBuild(app); err != nil {
		t.Fatalf("the Boot wiring does not build:\n%s", out)
	}
}

// Review round two: Booted vouches for one database. A router whose
// contexts carry another refuses every request before any action runs,
// and leaves that database untouched.
func TestRouterRefusesADatabaseBootDidNotCheck(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeFile(t, app, "gen/twodb_test.go", `package gen

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	astore "scratch/gen/store/a"
)

func open(t *testing.T, name string, migrations []string) *sql.DB {
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), name), migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTwoDatabases(t *testing.T) {
	a, b := open(t, "a.db", astore.Migrations), open(t, "b.db", nil) // a has the app's schema; b is untouched
	noop := func(*rastrillo.Ctx, http.ResponseWriter, string, int, any) {} // the action's render; writing nothing leaves 200
	booted, err := Boot(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Router(booted, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{DB: b, Render: noop} }).ServeHTTP(rec, httptest.NewRequest("GET", "/a", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a router on an unchecked database answered %d", rec.Code)
	}
	var n int
	b.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&n)
	if n != 0 {
		t.Errorf("the unchecked database was touched: %d objects", n)
	}
	rec = httptest.NewRecorder()
	Router(booted, func(*http.Request) *rastrillo.Ctx { return &rastrillo.Ctx{Render: noop} }).ServeHTTP(rec, httptest.NewRequest("GET", "/a", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("a context with no database answered %d; it must get the checked one and list a's rows", rec.Code)
	}
}
`)
	if out, err := goTest(app, "./gen/"); err != nil {
		t.Fatalf("%s", out)
	}
}

func TestRouterPanicsOnAForgedBooted(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/a.toml": resourceTOML("a", "exclusive")})
	mustGenerate(t, app)
	writeFile(t, app, "gen/forged_test.go", `package gen

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

func TestForged(t *testing.T) {
	defer func() {
		if v := recover(); v == nil || !strings.Contains(fmt.Sprint(v), "gen.Boot") {
			t.Errorf("a zero Booted was accepted or the panic does not say what to call: %v", v)
		}
	}()
	Router(Booted{}, func(*http.Request) *rastrillo.Ctx { return nil })
}
`)
	if out, err := goTest(app, "./gen/"); err != nil {
		t.Fatalf("%s", out)
	}
}
```

`scratchApp` (whose module path is `scratch`, so its store packages are `scratch/gen/store/<name>`), `mustGenerate`, `checkGenerate`, `writeMain`, `writeFile`, `goBuild`, `goTest` and `resourceTOML` are small helpers over `manifestgen_test.go:32-88`'s scratch-module pattern (module under `t.TempDir()`, `replace` to the checkout, `GOFLAGS=-mod=mod`, `-buildvcs=false`, sqlc via `go get -tool` as that test does); put them in `internal/generate/scratch_test.go` so Tasks 21 and 22 reuse them.

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/ -run 'Regenerating|Stale|RouterRequiresBoot|Forged|TwoDatabases|RefusesADatabase|ExclusiveToMergeable' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

`bootgen.Emit` writes the file above (comments from Appendix C.6), only when the app has manifest resources. `Router` gains the `Booted` parameter, the panic, and the per-request database check described above, done once in its `handle` helper before any action is called. `emitPipeline`, the action rewrites, the router, tools, packs and boot emitters all return written paths; `GenerateManifests` and `runGenerate` pass the union, with the exclusive store directories, to `prune.Prune` on a normal run and to `prune.Stale` under `--check`, which fails naming each file with `op.generate.stale`. Wire `Boot` into every caller listed under Files: call it after the app's schema is applied (after `migrate.Apply` in notes, at the top of `Router` in tickets and blog, which run after `OpenDB` applied `Options.Migrations`), fail startup on its error, and pass the result to `gen.Router`.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/... ./cmd/rastrillo/ -count=1
make build-cli && for e in blog tickets notes helloworld; do .build/rastrillo generate examples/$e; done
git add 'examples/*/gen'
make generate-check example-blog example-tickets example-notes example-helloworld scaffold-smoke
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/generate cmd/rastrillo examples
git commit -F - <<'MSG'
Make generated apps run gen.Boot before they can build a router

Money upgrades must run before an app serves, and nothing generated is
called unless the app calls it. gen.Router now takes the gen.Booted
that only gen.Boot returns, so an existing app's old wiring stops
compiling at the one line that needs changing, instead of serving
un-upgraded data. Boot does nothing yet; the next two changes give it
the SQL and event-log upgrades.

generate also removes files earlier runs wrote that nothing generates
now, and --check reports them: a stale store left behind keeps
compiling and keeps whatever behaviour it had.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 21: Money in generated SQL stores, actions and templates, from boot to clear

Spec: §1 (two columns; pointers; empty fields; added fields nullable), §4 (generated actions format in Go, 500 with the row id and code on error), §5 (the per-field contract table; `formView.Money` for both form groups), §7 ("Schema and boot", "Inserts and updates follow the rule"), §8 (mixed versions), §12 items 1-5, 13 and 14, Appendix C.1, C.2, C.6 (`gen.template.contract_money`). Review findings 2, 5, 6, 12, 15 and 20.
Starts from: `internal/generate/store.go:161-290` (`kindSQLType`, `schemaSQL`, `migrationsGo`), `:393-483` (queries, `sqlcYAML`), `internal/generate/actions.go:281-311` (`fieldExpr`, `fieldsMapLiteral`) and every `rastrillo.Money` branch (`grep -n 'Money' internal/generate/actions.go`), `internal/generate/templates.go:116-145` (`fieldPartial`, `fieldMono`), `internal/generate/bootgen/bootgen.go` (Task 20), `examples/tickets/internal/ticketstest/`.

**Files:**
- Modify: `internal/generate/store.go`, `actions.go`, `templates.go`, `internal/generate/bootgen/bootgen.go`, their tests and goldens
- Create: `internal/generate/money_flow_test.go` (scratch-app tests, using Task 20's helpers)
- Create: `internal/generate/migrationsgen.go`, `internal/generate/migrationsgen_test.go` (append-only generated migrations)
- Create (before regenerating anything): `examples/tickets/internal/oldgen/` — a frozen copy of the pre-money generated code as it stands at the start of this task (Tasks 19 and 20 changed its templates and router, not these two packages' money handling): `gen/actions/admin/ticket_types/id/edit_basics_post` and `gen/store/ticket_types`, with import paths rewritten to `tickets/internal/oldgen/...` and a `doc.go` saying it is the pre-upgrade generated code, kept so a test can run the real old handler against an upgraded database
- Modify: `examples/tickets/internal/ticketstest/harness_test.go` (`newApp` installs the locale configuration; `newAppAt(t, path)`; `seed` writes `price_minor`/`price_currency`), `required_test.go` and `roundtrip_test.go` (every `row.Price` becomes `*row.PriceMinor` with a `PriceCurrency` assertion; posts carry `Price.locale` and `Price.currency`; a blank required price expects `rastrillo.ui.field_required`'s English), and every other consumer of a changed generated identifier (`grep -rn 'Price\b\|FormatCents\|ParseCents' examples/*/internal examples/*/cmd`)
- Create: `examples/tickets/internal/ticketstest/money_test.go`, `examples/tickets/internal/ticketstest/upgrade_test.go`
- Regenerate: every example (blog and notes have no money, but `sqlc.yaml` changes for every exclusive store)

**Interfaces:**
- Consumes: `currency.Format`, `currency.Normalize` (Task 4); `form.Field{Currency, LocaleEcho}`, `form.SeedMoney`, `(*form.Parsed).Minor`, `(*form.Parsed).MoneyInput`, `form.MoneyInput` (Task 8); `rastrillo.FormatLocale` (Task 7); `sqlmoney.Upgrade`, `sqlmoney.Table` (Task 17); `Resource.Currency`, `Resource.Legacy`, `Resource.HasMoney` (Task 18); `ui.LocalizerFrom` (Task 9); `gen.Boot`, `gen.Booted` (Task 20).
- Produces:
  - SQL for a money field `Price`: `price_minor INTEGER` and `price_currency TEXT` (no `NOT NULL`, no default) in `schema.sql` (sqlc's view, always the final shape) and in a resource's first `migrations.go`; an already-released `migrations.go` is never edited (below)
  - `internal/generate`: `type History map[string][]string` (resource name to its released statements) and `func ReadHistory(genDir string, rs []rastrillo.Resource) (History, error)`, which reads every `gen/store/<r>/migrations.go` from the app's real `gen/` tree; `emitPipeline` takes the `History` as an input, separate from the directory it writes to, so `generate` (writing into `gen/`) and `generate --check` (writing into its empty temp dir, `manifestgen.go:52-62`) emit from the same released statements. Without that, `--check` would emit first-release statements into the temp dir and report every upgraded tree as stale.
  - `internal/generate`: `func releasedMigrations(path string) ([]string, error)` (the string literals of an existing generated `migrations.go`'s `Migrations`, in order, read with `go/parser`), and `func appendMigrations(released []string, r rastrillo.Resource) ([]string, error)` (released plus the statements that bring a replay of them to the resource's non-money columns)

**Generated migrations are append-only once released (a resolution; review round two, findings 2 and 5).** Apps keep generated statements in a ledger under stable IDs: notes wraps `bookmarksstore.Migrations` as `bookmarks_gen/0001_init`, `0002_init`, … by index (`examples/notes/internal/notes/migrations.go:31`). If this task rewrote a released `CREATE TABLE` (splitting `price` into two columns), `migrate.Apply` would refuse the changed checksum (`migrate/apply.go:120`) before `Boot` could run. The alternative, an explicit baseline step, would ask every existing app's operator to stamp the ledger by hand at exactly the release that needs a stop-start, which is the riskiest moment to add a manual step. So:
- When `gen/store/<r>/migrations.go` already exists, its statements are kept byte for byte, in order, as a prefix. The generator replays them into memory (`migrate.Replay`), compares the table with the resource's columns, and appends one statement per missing non-money column (`ALTER TABLE <t> ADD COLUMN <c> TEXT NOT NULL DEFAULT ''`) and the owner index if missing, printing `op.generate.migration_appended`. Nothing is ever removed or rewritten; a removed field's column stays, as migrations are never reversed.
- Money columns never appear in an appended statement. `Boot`'s `sqlmoney.Upgrade` (Task 17) owns them: it splits a released legacy column (with `[legacy]` when there are rows; without, on an empty table) and adds the columns of a money field added later. A fresh database therefore runs the released statements and then reaches the final shape in `Boot`.
- A resource with no `migrations.go` yet (a new resource) gets the final shape in its first statement, which is then released like any other.
- Index-based IDs stay valid because indexes only ever grow, so notes' wrapper needs no change, and apps on `Options.Migrations` (tickets) run the same list.
- Until a resource is first released, an author who wants a clean first statement deletes `gen/store/<r>/migrations.go` and regenerates; Task 28's docs say so.
  - `sqlc.yaml` gains `emit_pointers_for_null_types: true`: the model has `PriceMinor *int64` and `PriceCurrency *string`
  - `gen.Boot` now runs, before returning `Booted`, `sqlmoney.Upgrade(ctx, db, []sqlmoney.Table{…})` with one `Table` per exclusive resource that has money, `Legacy` filled from its `[legacy]` (`Scale: *r.Legacy.Scale`), nil without one. For tickets:

```go
func Boot(ctx context.Context, db *sql.DB) (Booted, error) {
	if err := sqlmoney.Upgrade(ctx, db, []sqlmoney.Table{
		{Name: "ticket_types", Fields: []string{"price"}, Legacy: &currency.Legacy{Currency: "USD", Scale: 2}},
	}); err != nil {
		return Booted{}, err
	}
	return Booted{db: db}, nil
}
```

  - `formView` gains `Money map[string]form.MoneyInput` for both form groups (its doc line in the template's data contract is Appendix C.6's `gen.template.contract_money`; `MoneyInput.Seed` was added after that line was approved, so the line's wording is re-checked in the second copy review, row `gen.template.contract_money_seed`) and `ErrorArgs map[string]map[string]any` (from `p.ErrorArgs()`), so a message with values renders them
  - display (list rows, show fields): `currency.Format(rastrillo.FormatLocale(r), *n.PriceMinor, *n.PriceCurrency)`; `""` when nil; on an error the action calls `view.Fail` (500) naming the row id and the code
  - form fields: `form.Field{Name: "Price", Kind: form.Money, Currency: <cur>, LocaleEcho: true, Required: …}`, `<cur>` being the resource default on create and on edit the record's currency, or the resource default when the record has none
  - writes: a blank field writes NULL to both columns (this is how an optional amount is cleared); an amount writes `minor` and `<cur>`; an update never writes a currency different from a set one, because `<cur>` is the record's when it has one
  - `fieldPartial(rastrillo.Money)` is `"field-money"`; the form passes `(index .Money "Price")`'s `Value`, `Locale`, `Currency` and `Seed`, and every field's error renders as `(Tf $L (index .Errors "Price") (index .ErrorArgs "Price"))` (a missing entry is a nil map, which interpolates nothing, so fields without arguments render as before)

- [ ] **Step 1: Freeze the old handler, then write the failing tests**

Copy the two generated packages into `examples/tickets/internal/oldgen/` now, before any regeneration, and make them build there (`go build ./internal/oldgen/...` from `examples/tickets`). Copy the released `examples/tickets/gen/store/ticket_types/migrations.go`, its one statement, and `examples/tickets/manifest/ticket_types.toml` as they are at the branch's base commit (`git show $(git merge-base HEAD origin/main):<path>`; the manifest gained a currency in Task 18, so the working copy is not the released one) into `internal/generate/testdata/released/`. Then the tests.

Generator unit tests (`store_test.go`, `actions_test.go`, `boot_test.go`): the exact column lines (`  price_minor INTEGER,` and `  price_currency TEXT,`), the `sqlc.yaml` line, `gen/boot.go` byte for byte for the tickets resource (the block above), a `Boot` with no `sqlmoney` call for a resource set without money, and the emitted create action's field literal `form.Field{Name: "Price", Kind: form.Money, Currency: "USD", LocaleEcho: true, Required: true}`.

Whole-app behaviour in tickets, `money_test.go` (spec §12 items 4 and 5):

```go
func moneyPost(price, locale, cur string) url.Values {
	return url.Values{"Name": {"VIP"}, "Price": {price}, "Price.locale": {locale}, "Price.currency": {cur}, "Status": {"draft"}}
}

func TestCreateStoresMinorUnitsAndCurrency(t *testing.T) {
	app, db := newApp(t)
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("1,284.50", "en", "USD")), http.StatusSeeOther)
	var minor int64
	var cur string
	db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types`).Scan(&minor, &cur)
	if minor != 128450 || cur != "USD" {
		t.Errorf("stored %d %s, want 128450 USD", minor, cur)
	}
	show := get(t, app, "/admin/ticket_types/1")
	wantContains(t, show.Body.String(), "$1,284.50")
}

// A create form whose currency changed between GET and POST: the text
// is kept, nothing is stored, and the person is told.
func TestCreateWithChangedCurrencyKeepsTextAndStoresNothing(t *testing.T) {
	app, db := newApp(t)
	rec := post(t, app, "/admin/ticket_types", moneyPost("12.50", "en", "EUR"))
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()
	wantContains(t, body, `value="12.50"`)
	wantContains(t, body, `name="Price.currency" value="USD"`)
	wantContains(t, body, "<the approved English of rastrillo.ui.money_currency_changed, HTML-escaped>")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM ticket_types`).Scan(&n)
	if n != 0 {
		t.Errorf("a mismatched currency stored %d rows", n)
	}
}

// Spec §12 item 15, through the generated tickets screens.
func TestNormalisedInputSavesAndReseeds(t *testing.T) {
	app, db := newApp(t)
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("US$ 1,284.5", "en", "USD")), http.StatusSeeOther)
	var minor int64
	db.QueryRow(`SELECT price_minor FROM ticket_types`).Scan(&minor)
	if minor != 128450 {
		t.Fatalf("stored %d, want 128450", minor)
	}
	edit := get(t, app, "/admin/ticket_types/1/edit").Body.String()
	wantContains(t, edit, `value="1284.50"`)
	wantContains(t, edit, `name="Price.seed" value="1284.50"`)
	// An unchanged edit saves through the seed check.
	v := moneyPost("1284.50", "en", "USD")
	v.Set("Price.seed", "1284.50")
	wantStatus(t, post(t, app, "/admin/ticket_types/1/edit-basics", v), http.StatusSeeOther)
	// A forged seed, and a seed on create, store nothing.
	v = moneyPost("9.99", "en", "USD")
	v.Set("Price.seed", "9.99")
	wantStatus(t, post(t, app, "/admin/ticket_types/1/edit-basics", v), http.StatusBadRequest)
	wantStatus(t, post(t, app, "/admin/ticket_types", v), http.StatusBadRequest)
	db.QueryRow(`SELECT price_minor FROM ticket_types WHERE id = 1`).Scan(&minor)
	if minor != 128450 {
		t.Errorf("a forged seed changed the record to %d", minor)
	}
	// A failure elsewhere echoes the person's own text with no seed, and
	// the resubmission reads it the same way.
	v = moneyPost("US$ 1,284.5", "en", "USD")
	v.Set("Name", "")
	rec := post(t, app, "/admin/ticket_types", v)
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), `value="US$ 1,284.5"`)
	wantContains(t, rec.Body.String(), `name="Price.seed" value=""`) // typed text, not a verified seed
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("US$ 1,284.5", "en", "USD")), http.StatusSeeOther)
	// The two new messages, with their values.
	body := post(t, app, "/admin/ticket_types", moneyPost("€5", "en", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.ReplaceAll(catalogEN("rastrillo.ui.money_wrong_currency"), "{currency}", "USD")))
	body = post(t, app, "/admin/ticket_types", moneyPost("1.500", "en", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.NewReplacer("{a}", "1.5 USD", "{b}", "1500 USD").Replace(catalogEN("rastrillo.ui.money_ambiguous"))))
}

func TestEditRejectedLocaleReseedsFromTheRecord(t *testing.T) {
	app, _ := newApp(t)
	post(t, app, "/admin/ticket_types", moneyPost("12.50", "en", "USD"))
	v := moneyPost("99", "", "USD")
	v.Del("Price.locale")
	rec := post(t, app, "/admin/ticket_types/1/edit-basics", v)
	wantStatus(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), `value="12.50"`) // the server's seed, not the unparseable text
}
```

`catalogEN(k)` is `rastrillo.BaseCatalog()[k]`, so the test follows whatever the second copy review approves; Task 23's two-locale test checks the same two messages in Spanish. The KWD cases of item 15 (an unchanged 1.500 KWD edit keeps 1500, the same text typed into a create form is `money_ambiguous`, a record changed between GET and POST is `money_reenter`) run in the `fees` scratch app below, which is KWD.

Replace the bracketed `wantContains` argument with the literal approved string from Appendix C.1, HTML-escaped as html/template writes it. Keep `TestFourStateRoundTrip`'s `"$12.00"` assertion: the edit form's seed is `InputValue("en", 1200, "USD") == "12.00"`, and the unchanged resubmission must store 1200 USD again.

`upgrade_test.go`, the existing app's upgrade path and the round-two regression (spec §12 items 1-3):

```go
// An existing tickets database, as today's binary left it, is upgraded
// by the app's own startup: Boot runs before the router exists.
func TestLegacyDatabaseIsUpgradedAtBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.db")
	old := openRaw(t, path)
	mustExec(t, old, oldTicketTypesSchema) // today's generated CREATE TABLE, copied literally
	mustExec(t, old, `INSERT INTO ticket_types (name, price, status, max_per_order, created_at, updated_at) VALUES ('GA', 1250, 'on_sale', '', 'x', 'x')`)
	old.Close()
	app, db := newAppAt(t, path)
	var minor int64
	var cur string
	if err := db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types`).Scan(&minor, &cur); err != nil || minor != 1250 || cur != "USD" {
		t.Fatalf("after boot: %d %s %v", minor, cur, err)
	}
	wantContains(t, get(t, app, "/admin/ticket_types/1").Body.String(), "$12.50")
}

// Spec §12 item 3 with the real old code: a JPY record shows "1200" in
// its edit form; posted unchanged to the old binary's handler, it would
// parse as 1200 cents and store 120000. After the upgrade that handler
// fails and the row is unchanged.
func TestOldEditHandlerIsRefusedAfterUpgrade(t *testing.T) {
	app, db := newApp(t)
	_ = app
	mustExec(t, db, `INSERT INTO ticket_types (name, price_minor, price_currency, status, max_per_order, created_at, updated_at) VALUES ('Tokyo', 1200, 'JPY', 'on_sale', '', 'x', 'x')`)
	old := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", "1")
		oldeditbasics.Handle(&rastrillo.Ctx{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Render: discardRender}, w, r)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/ticket_types/1/edit-basics", strings.NewReader(url.Values{"Name": {"Tokyo"}, "Price": {"1200"}, "Status": {"on_sale"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	old.ServeHTTP(rec, req)
	if rec.Code < 500 {
		t.Errorf("the old handler answered %d; it must fail on the missing column", rec.Code)
	}
	var minor int64
	var cur string
	db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types WHERE id = 1`).Scan(&minor, &cur)
	if minor != 1200 || cur != "JPY" {
		t.Errorf("the row changed to %d %s", minor, cur)
	}
}
```

(`oldeditbasics` is `tickets/internal/oldgen/editbasics`; `discardRender` is a `rastrillo.RenderFunc` that writes nothing; `openRaw` opens with `rastrillo.OpenDB(path, nil)`.)

Append-only migrations, unit level (`migrationsgen_test.go`): `appendMigrations` on a released list equal to today's `examples/tickets/gen/store/ticket_types/migrations.go` statement returns that statement unchanged as element 0; adding a text field `Venue` appends exactly `ALTER TABLE ticket_types ADD COLUMN venue TEXT NOT NULL DEFAULT ''`; adding a money field appends nothing; removing a field appends nothing and keeps element 0; running it twice appends nothing the second time; a resource with no released list gets one `CREATE TABLE` with `price_minor INTEGER` and `price_currency TEXT`.

The whole existing-app path through a ledger (round two's required test), in `money_flow_test.go`:

```go
// An app that keeps generated statements in its migration ledger under
// index IDs (notes' pattern) and was released before money had a
// currency: change the manifest, regenerate, start through migrate.Apply
// and Boot. The released statement's checksum is unchanged, only the
// appended statement is new in the ledger, and Boot finishes the money.
func TestLedgerAppUpgradesThroughApplyAndBoot(t *testing.T) {
	app := scratchApp(t, map[string]string{
		"manifest/ticket_types.toml":                   ticketTypesTOMLWithoutCurrency, // today's examples/tickets manifest
		"gen/store/ticket_types/migrations.go":         todaysTicketTypesMigrationsGo,   // copied literally from examples/tickets at the commit before this task
		"internal/app/migrations.go":                   notesStyleWrapper,               // wraps ticket_typesstore.Migrations as ticket_types_gen/%04d_init, like notes
	})
	db := filepath.Join(app, "app.db")
	runInApp(t, app, "apply-old", db) // a scratch main_test: migrate.Apply(wrapper) and INSERT one row with price = 1250
	writeFile(t, app, "manifest/ticket_types.toml", ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit)
	mustGenerate(t, app)
	got := releasedOrFail(t, filepath.Join(app, "gen/store/ticket_types/migrations.go"))
	if got[0] != todaysTicketTypesStatement || len(got) != 2 || !strings.Contains(got[1], "ADD COLUMN venue") {
		t.Fatalf("regenerated migrations: %q", got)
	}
	runInApp(t, app, "start", db) // migrate.Apply(wrapper), then gen.Boot, then GET /admin/ticket_types/1 must show $12.50
	ledger := ledgerIDs(t, db)
	if strings.Join(ledger, " ") != "ticket_types_gen/0001_init ticket_types_gen/0002_init" {
		t.Errorf("ledger %v", ledger)
	}
	assertRow(t, db, `SELECT price_minor, price_currency, venue, deposit_minor IS NULL FROM ticket_types`, "1250 USD  1")
}
```

Round three's regression, the check against an upgraded tree, also in `money_flow_test.go`:

```go
// generate --check emits into an empty temp dir. It must read the
// released statements from the real gen/ tree, as generate does, or it
// emits first-release SQL and reports the upgraded tree as stale.
func TestCheckPassesOnAnUpgradedTreeAndChangesNothing(t *testing.T) {
	app := scratchApp(t, map[string]string{
		"manifest/ticket_types.toml":           ticketTypesTOMLWithoutCurrency,
		"gen/store/ticket_types/migrations.go": todaysTicketTypesMigrationsGo,
	})
	writeFile(t, app, "manifest/ticket_types.toml", ticketTypesTOMLWithCurrencyLegacyVenueAndDeposit) // a money field and a non-money field added
	mustGenerate(t, app)
	before := treeHashes(t, app) // path -> SHA-256 of every file under the app
	if err := checkGenerate(app); err != nil {
		t.Fatalf("--check on the tree generate just wrote: %v", err)
	}
	if after := treeHashes(t, app); !maps.Equal(before, after) {
		t.Errorf("--check changed the source tree")
	}
	got := releasedOrFail(t, filepath.Join(app, "gen/store/ticket_types/migrations.go"))
	if got[0] != todaysTicketTypesStatement {
		t.Errorf("the released statement's bytes changed:\n%q", got[0])
	}
}
```

Add the unit counterpart in `migrationsgen_test.go`: `emitPipeline` into an empty directory with a `History` holding the released statement produces a `migrations.go` byte-identical to the one `generate` wrote into the real tree.

`runInApp(t, app, step, dbPath)` runs the scratch module's `main_test.go` (written by the test) with `STEP` and `DB` set, so each step is a real process start against the same database file; `todaysTicketTypesMigrationsGo`, `todaysTicketTypesStatement` and `ticketTypesTOMLWithoutCurrency` are copied into `internal/generate/testdata/released/` from `examples/tickets` before this task changes anything (do it in Step 1, like `oldgen`), so the test keeps what was released, not what the generator now writes.

Scratch-app tests in `internal/generate/money_flow_test.go`, using Task 20's helpers, for what tickets cannot show (money in both form groups, a three-decimal currency, two locales, an optional field, and a field added later). The scratch app has one resource:

```toml
name = "fees"
route = "/fees"
store = "exclusive"
currency = "KWD"

[list]
columns = [{ field = "Name" }, { field = "Price", kind = "money" }]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money", required = true }]
advanced = [{ name = "Note", required = true }, { name = "Deposit", kind = "money" }]
```

with `manifest/app.toml` offering `["en", "de"]`, and a `main_test.go` (written by the test into the scratch module) that builds the handler through `rastrillo.Handler(rastrillo.Options{Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale, BaseCatalog: genlocales.BaseCatalog, Router: …})`, whose `Router` calls `gen.Boot` and serves `gen.Router` behind a `ui.Pages` render (layout `{{define "layout"}}{{$L := localizer $}}{{template "content" (withL (.) $L)}}{{end}}` plus the generated pages, and `pages.Middleware` around the mux). It then runs these flows and fails the outer test on any failure:

1. **en→de, Basics.** A fee with Price 1500 fils and Deposit 2500 fils. `GET /fees/1/edit` (English) shows Price `value="1.500"` with `Price.locale` `en`. `POST /de/fees/1/edit-basics` with `Name=""`, `Price=1.500`, `Price.locale=en`, `Price.currency=KWD` is a 400 whose page (in German) echoes Price `value="1.500"` with `Price.locale` `en`, and seeds the other group's Deposit in German as `value="2,500"` with `Deposit.locale` `de`. Resubmitting to `/de/fees/1/edit-basics` with `Name="Fee"` and the same Price fields stores `price_minor = 1500`, not 1500000.
2. **en→de, Advanced.** The same, through `edit-advanced`, with `Note=""` failing and Deposit `2.500` echoed with locale `en`; the resubmission with a Note stores `deposit_minor = 2500`, and the Basics group in the failed page is seeded in German.
2b. **Seeds, KWD (spec §12 item 15).** With the fee's Price at 1500 fils: `GET /fees/1/edit` (English) renders `value="1.500"` and `name="Price.seed" value="1.500"`; posting that text and seed back unchanged stores 1500; posting `1.500` with no seed to `POST /fees` is a 400 showing both choices (`1.5 KWD`, `1500 KWD`); changing the stored price to 2000 between the GET and the POST of the unchanged seed gives `money_reenter`, stores nothing, and re-renders `value="2.000"` with a fresh seed. Two submissions around an error on another field (spec §12 item 15, `c62d25e6`): posting the unchanged `1.500` with its seed and an empty Name is a 400 whose page keeps `value="1.500"`, `Price.locale` `en` and `name="Price.seed" value="1.500"`; resubmitting that page's values with a Name stores 1500; doing the same after changing the stored price to 2000 between the two submissions gives `money_reenter`.
3. **Clearing.** `POST /fees/1/edit-advanced` with `Note=x` and `Deposit=""` stores NULL in both `deposit_minor` and `deposit_currency`, and the show page shows no deposit amount.
4. **Adding money later.** A second scratch app starts with the resource without `Deposit`, boots, creates a fee; then `Deposit` is added to the manifest, `rastrillo generate` runs, and the app boots again against the same database file. Boot adds `deposit_minor`/`deposit_currency` (no hand migration), the existing fee has NULL in both, its show page renders, and an edit that sets Deposit `5` stores `5000 KWD`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/ -count=1` and `make example-tickets`
Expected: FAIL on the new assertions.

- [ ] **Step 3: Implement**

- `store.go`: a money field writes two SQL columns in `schemaSQL` and the queries' column lists, and the queries bind both; `migrationsGo` emits `appendMigrations(history[r.Name], r)` (`migrationsgen.go`), so a released file only ever grows; `GenerateManifests` calls `ReadHistory(<the real gen dir>, rs)` once, before choosing the output directory, and passes it to `emitPipeline` in both modes; `sqlcYAML` adds `emit_pointers_for_null_types: true` under `go:`.
- `actions.go`: the display path calls a small helper the action file emits once, `money(r, id, minor, cur) (string, error)`, returning `""` for nil and `currency.Format(rastrillo.FormatLocale(r), *minor, *cur)` otherwise; on its error the action answers `view.Fail` with Appendix C `op.money.display_failed` (second copy review), filled with the resource, row id, field and code. The form seed is `form.SeedMoney(rastrillo.FormatLocale(r), n.PriceMinor, cur, false)` (generated fields are never `Strict`), and the edit POST's field gets `Stored: n.PriceMinor` from the record the action has just re-read, so the unchanged-seed check compares against the current value. On a failed POST, the submitted group's money inputs come from `p.MoneyInput(name, render)`; when `reseed` is true, use the server seed (blank on create); the other group's come from the record, seeded in the render locale. Writes bind the pointers per the rules above. Generated code stops using `form.FormatCents`, `FormatCentsPlain` and `ParseCents` (they stay in `form` for hand-written apps).
- `templates.go`: `fieldPartial(rastrillo.Money)` is `"field-money"`; the form passes the money data from `.Money`, with `Seed`; errors go through `Tf` with `.ErrorArgs`; the data contract gains the `Money` line. Generated money fields are never `Strict` (decision 22's default).
- `bootgen.go`: the emitted `Boot` gains the `sqlmoney.Upgrade` call.
- Budget: `migrationsgen.go` stays in `internal/generate` because it needs the package's private column helpers (`columns`, `sqlName`). Run `go run ./cmd/rastrillo budget size`; if `internal/generate` is over 5,000 source lines, add `size internal/generate <measured+5%> - the generated-store emitters share private column helpers; splitting them is owed` to `.rastrillo/budgets.txt` in this commit.
- Tickets: `harness_test.go`'s `newApp` calls `locales.Install(cfg)` with `cfg` from `locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)` before `gen.Boot`, because without it the display formats in CLDR root (`US$12.00`), which is what code outside an app gets; `newAppAt(t, path)` opens a given file; `seed` and every `row.Price` consumer move to the two fields.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/... ./cmd/rastrillo/ -count=1
make build-cli && for e in blog tickets notes helloworld; do .build/rastrillo generate examples/$e; done
git add 'examples/*/gen'
make generate-check example-tickets example-blog example-notes example-helloworld
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/generate .rastrillo/budgets.txt cmd/rastrillo examples
git commit -F - <<'MSG'
Store generated money with its currency and upgrade old rows in Boot

Each money field is two nullable columns, read as pointers, so an
empty field has neither an amount nor a currency and clearing an
optional amount writes NULL to both. Forms carry the locale and
currency they displayed; the server parses in that locale, stores in
its own currency, and rejects a currency that changed under the form.

gen.Boot upgrades legacy rows once, adds the columns of a money field
added to an existing table, and refuses to start on data that breaks
the rule; the old generated edit handler, kept as a fixture, is shown
failing against the upgraded table instead of storing 1200 yen as
120000.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 22: Money in generated event stores, `V: 2` on every event, and the event log's upgrade in `Boot`

Spec: §7 ("Event stores", all rules), §8 ("Which apps rename the event log": declared from the manifests; the stop-start notice; "An app that adds money later"; "An app that removes money later"; "Detecting legacy writes after the upgrade"), §12 items 6, 11 and 13, Appendix C.2 (`op.generate.money_schema_added`, `op.replay.*`, `op.eventlog.upgrade_refused`). Review findings 2, 4 and 6.
Starts from: `internal/generate/mergeablestore.go` (`mergeableGoType`, the payload struct, `reduce`, `Create`/`Update`/`Delete`, the migrations file), `internal/generate/testdata/mergeable/*.golden`, `internal/generate/bootgen/bootgen.go` (Tasks 20, 21).

**Resolution recorded here.** The spec says `generate` adds `eventlog.MoneySchema` "after `eventlog.Schema` in the generated `BootSchema`". There is no generated `BootSchema`: generated stores export `Migrations []string` for `Options.Migrations` (tickets) or are wrapped by hand into a `migrate.Set` (notes), and either way nothing the generator writes reaches the app's migration call. So the event log's upgrade runs in the generated `Boot`, which every app with manifest resources must already call (Task 20): when any mergeable resource has money, `Boot` calls `eventlog.UpgradeForMoney` (Task 15), which applies `MoneySchema` through `migrate.Apply` for apps with a ledger and directly, in one transaction, for apps without one. Whether to rename is still declared, never guessed from data: it follows from the manifests, and `generate` prints `op.generate.money_schema_added` the first time a regeneration adds that call. Mergeable stores keep exporting `Migrations` (their `CREATE TABLE IF NOT EXISTS` is a no-op on a renamed table), so nothing becomes obsolete.

**Files:**
- Modify: `internal/generate/mergeablestore.go`, `mergeablestore_test.go`, `internal/generate/testdata/mergeable/*.golden`
- Create: `internal/generate/testdata/mergeable/prices_store.go.golden` and its fixture manifest in the test
- Modify: `internal/generate/bootgen/bootgen.go` (the emitted `Boot` also runs `eventlog.UpgradeForMoney` and each money-bearing mergeable store's `CheckReplay`)
- Create: `internal/generate/wiring_test.go` (scratch-app tests of the complete startup for each store mix)
- Modify: `cmd/rastrillo/generate.go` (print `op.generate.money_schema_added` when `gen/boot.go` newly calls `eventlog.UpgradeForMoney`)
- Modify: `examples/tickets/internal/ticketstest/announcements_test.go` (payloads now carry `V`) and any other consumer of the payload shape
- Regenerate: every example (tickets' mergeable `announcements` gains `V`; it has no money, so its `Boot` gains no event-log call)

**Interfaces:**
- Consumes: `eventlog.UpgradeForMoney`, `eventlog.UpgradeBoundary`, `eventlog.Event` (Task 15); `replay.Rules`, `replay.Money`, `replay.Field`, `replay.Version` (Task 16); `currency.Legacy` (Task 4); `gen.Boot` (Tasks 20, 21).
- Produces, in a generated mergeable store package with money:
  - writes: every append (created, updated, deleted) carries `"V":2`; a money field with an amount writes `"price":<minor>,"price_currency":"<code>"`; a cleared field writes `"price":null,"price_currency":null` (Task 16's clear); an unchanged field writes neither key. The payload is built as a `map[string]any` for the money keys (a struct with `omitempty` cannot write an explicit `null`), merged with the existing struct for the other fields
  - reads: `Get`, `List` and `CheckReplay` all fold through one `replay.Rules{Fields, Legacy, Boundary, HasBoundary}` per store and database, built on first use from `eventlog.UpgradeBoundary` and cached per `*sql.DB` (the snapshot never changes once written); the first error makes the call fail (a 500), never a guessed record
  - `func CheckReplay(ctx context.Context, db *sql.DB) error`: every stream with this store's prefix, folded with the same `Rules`; the first error, wrapped with Appendix C `op.replay.stream` (second copy review) to name the stream
  - in every mergeable store, money or not: `"V":2` on every append, and an event whose `V` is present and not 2 fails the fold (version checked before kind)
  - `gen.Boot`, in order: `eventlog.UpgradeForMoney` (only when a mergeable resource has money), `sqlmoney.Upgrade` (Task 21), then each money-bearing mergeable store's `CheckReplay`

- [ ] **Step 1: Write the failing tests**

Golden tests: regenerate expectations for `announcements_store.go.golden` (gains `V`), and add `prices_store.go.golden` for a fixture mergeable resource `prices` with `Title` (text) and `Amount` (money), `currency = "EUR"`, `[legacy] currency = "USD"`, `scale = 2`.

`internal/generate/wiring_test.go`, using Task 20's scratch-app helpers. One table of three apps, each started through its real generated wiring (`main_test.go` in the scratch module calls `gen.Boot` then `gen.Router` behind `rastrillo.Handler`, exactly as `examples/tickets` does), each against a database file prepared the way today's binary would have left it:

| App | Resources | Database before | After `Boot` |
|---|---|---|---|
| SQL-only | `fees` exclusive with money, `[legacy]` USD scale 2 | today's `CREATE TABLE fees (… price INTEGER NOT NULL DEFAULT 0 …)`, one row `price = 1250` | `price_minor = 1250`, `price_currency = 'USD'`, `price` gone; event log untouched |
| mergeable money | `prices` mergeable with money, `[legacy]` USD scale 2 | `eventlog.Schema`'s DDL run raw (no ledger, as `Options.Migrations` does), one legacy created event `{"amount":1250}` | column `payload_v2`, a boundary row, `GET /prices/1` shows `$12.50` |
| mixed | both of the above, plus `notes` mergeable without money | both of the above | both of the above |

For each, assert also: a second start is a no-op; `GET` of the list and a show page answer 200; and a start with a deliberately broken row (an SQL row with an amount and no currency; an event with `"V":3`) refuses to start with the approved message. For the mergeable and mixed apps, add the ledger variant: the app applies its schema with `migrate.Apply(…, migrate.Merge(eventlog.Schema, <its own set>))` before `Boot`, and after `Boot` the ledger lists `eventlog_money/0001_payload_v2`.

Event-store behaviour, in the same scratch module (`prices`):

These two run inside the scratch module (its `main_test.go`), where `newPricesApp(t)` starts the app through `gen.Boot` and `gen.Router` on a fresh database file and returns the handler and the `*sql.DB`, and `do(t, h, method, path, form)` returns the recorder:

```go
// Review finding 6: clearing an optional amount through HTTP empties
// it, the stored event says so explicitly, and replay agrees after a
// restart.
func TestPricesClearThroughHTTPAndReplay(t *testing.T) {
	h, db := newPricesApp(t)
	money := func(v string) url.Values {
		return url.Values{"Title": {"Lunch"}, "Amount": {v}, "Amount.locale": {"en"}, "Amount.currency": {"EUR"}}
	}
	if rec := do(t, h, "POST", "/prices", money("12.50")); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/prices/1/edit-basics", money("")); rec.Code != http.StatusSeeOther {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body)
	}
	if body := do(t, h, "GET", "/prices/1", nil).Body.String(); strings.Contains(body, "12.50") {
		t.Errorf("the cleared amount still shows:\n%s", body)
	}
	var payload string
	db.QueryRow(`SELECT payload_v2 FROM eventlog ORDER BY seq DESC LIMIT 1`).Scan(&payload)
	if !strings.Contains(payload, `"amount":null`) || !strings.Contains(payload, `"amount_currency":null`) || !strings.Contains(payload, `"V":2`) {
		t.Errorf("clear event payload %s", payload)
	}
	if _, err := gen.Boot(context.Background(), db); err != nil {
		t.Fatalf("restart after a clear: %v", err)
	}
	if body := do(t, h, "GET", "/prices/1", nil).Body.String(); strings.Contains(body, "12.50") {
		t.Errorf("after replay the cleared amount is back")
	}
}

// Review finding 4: the upgrade boundary holds on every read. An
// unversioned event ingested after the app started is refused by the
// next Get and List, not only by the next boot.
func TestPricesIngestAfterBootIsRefusedOnRead(t *testing.T) {
	h, db := newPricesApp(t)
	do(t, h, "POST", "/prices", url.Values{"Title": {"Lunch"}, "Amount": {"12.50"}, "Amount.locale": {"en"}, "Amount.currency": {"EUR"}})
	l, err := eventlog.Open(db, "w1")
	if err != nil {
		t.Fatal(err)
	}
	stream := "prices/1" // the store's stream for record 1; use the generated store's prefix constant
	if err := l.Ingest(context.Background(), []eventlog.Event{{Stream: stream, Writer: "w9", Seq: 1, Lamport: 99,
		TS: time.Now().UTC(), Actor: "human", Kind: "updated", Payload: json.RawMessage(`{"amount":999}`)}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/prices/1", "/prices"} {
		if rec := do(t, h, "GET", path, nil); rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s after an unversioned ingest: %d, want 500", path, rec.Code)
		}
	}
	if _, err := gen.Boot(context.Background(), db); err == nil {
		t.Error("Boot accepted an unversioned event written after the upgrade")
	}
}
```

Also, in the scratch module, the spec's event cases at app level: every new event, the delete included, has `"V":2`; a created event with no money keys (history from before the field existed) derives an empty field; a hand-appended `{"V":3}` deleted event makes `Get` fail; a legacy (unversioned, pre-upgrade) amount of 1250 with `[legacy]` USD scale 2 derives `USD 1250`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/ -run 'Mergeable|Wiring|Prices' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** the emitter changes above. `mergeableGoType(Money)` becomes `*int64` with the companion `*string` on the model. In `cmd/rastrillo/generate.go`, compare the new `gen/boot.go` with the one on disk before writing; if it newly calls `eventlog.UpgradeForMoney`, print `op.generate.money_schema_added` naming the resources. Regenerate every example and stage `examples/*/gen`.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
go run ./cmd/rastrillo budget size   # over 5,000 in internal/generate: add the budgets.txt line (Global Constraints) in this commit
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/generate/... -count=1
make generate-check example-tickets example-blog example-notes example-helloworld
```
Expected: PASS; `TestAnnouncementsDeleteIsATombstone` and the other announcements tests pass with `V` present.

- [ ] **Step 5: Commit**

```bash
git add internal/generate .rastrillo/budgets.txt cmd/rastrillo examples
git commit -F - <<'MSG'
Version every generated event and carry currencies in money events

Every event a new writer appends carries V: 2, so a future binary can
refuse data newer than it understands. Money fields carry their
currency in the same event under one rule, a currency if and only if
an amount; a cleared field writes both keys as null, because an absent
key already means "unchanged".

The event log's money rename runs in gen.Boot, which every generated
app must call, whether the app keeps a migration ledger or not. Every
read checks the upgrade boundary, so an unversioned event ingested
after start is refused on the next request, not only the next boot.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 23: The tickets example on `ui.Pages`, in two locales

Spec: §10 (strict in production), §12 item 8 ("each example's own suite passing after migration in two locales"), §3 (`CheckConfig` feeding `ui.Pages`).
Starts from: `examples/tickets/internal/tickets/render.go`, `examples/tickets/cmd/tickets/main.go`, `examples/tickets/internal/ticketstest/harness_test.go`.

**Files:**
- Modify: `examples/tickets/internal/tickets/render.go` (an `App` holding `*ui.Pages`; `Render` as its method), `examples/tickets/internal/tickets/templates/layout.html` (migrated; embedded by `render.go` as `layoutFS`), `examples/tickets/cmd/tickets/main.go`
- Modify: `examples/tickets/internal/ticketstest/harness_test.go`
- Create: `examples/tickets/internal/ticketstest/locales_test.go`
- Modify: `internal/tmplmigrate/corpus_test.go`, `internal/tmplmigrate/gocheck_test.go` (tickets reaches zero)

**Interfaces:**
- Consumes: `ui.NewPages`, `ui.PagesConfig`, `ui.PageFile`, `(*ui.Pages).Execute`, `(*ui.Pages).Middleware`, `ui.Localized.Localizer()` (Tasks 9, 13); `locales.CheckConfig`; `gen.Boot` (Tasks 20-22).
- Produces (package `tickets`): `type App struct { pages *ui.Pages }`; `func NewApp(cfg locales.Config) (*App, error)`; `func (a *App) Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any)`; `func (a *App) Middleware(next http.Handler) http.Handler` (the pages' middleware).

- [ ] **Step 1: Write the failing test**

`examples/tickets/internal/ticketstest/locales_test.go`:

```go
// The example in two locales: the same generated screens, one template
// set, rendered in English and Spanish by the request's locale. App
// keys have no Spanish catalog, so they fall back to English, as the
// layered catalogs promise; framework strings and money are Spanish.
func TestTicketTypesInTwoLocales(t *testing.T) {
	app, _ := newAppWith(t, "en", []string{"en", "es", "ar"})
	wantStatus(t, post(t, app, "/admin/ticket_types", moneyPost("1,284.50", "en", "USD")), http.StatusSeeOther)
	for _, c := range []struct{ path, locale string }{{"/admin/ticket_types/1", "en"}, {"/es/admin/ticket_types/1", "es"}} {
		body := get(t, app, c.path).Body.String()
		want, _ := currency.Format(c.locale, 128450, "USD")
		wantContains(t, body, template.HTMLEscapeString(want))
		wantContains(t, body, template.HTMLEscapeString(rastrillo.BaseCatalogs()[c.locale]["rastrillo.ui.shell_language"]))
	}
	// The real embedded layout rendered strictly, in the request's
	// language and direction, with the switcher.
	esBody := get(t, app, "/es/admin/ticket_types/1").Body.String()
	wantContains(t, esBody, `<html lang="es" dir="ltr">`)
	wantContains(t, esBody, "rst-locale")
	wantContains(t, get(t, app, "/ar/admin/ticket_types/1").Body.String(), `<html lang="ar" dir="rtl">`)
	// The normalising messages in a translated locale (spec §12 item 15).
	es := rastrillo.BaseCatalogs()["es"]
	body := post(t, app, "/es/admin/ticket_types", moneyPost("€5", "es", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.ReplaceAll(es["rastrillo.ui.money_wrong_currency"], "{currency}", "USD")))
	body = post(t, app, "/es/admin/ticket_types", moneyPost("1,500", "es", "USD")).Body.String()
	wantContains(t, body, html.EscapeString(strings.NewReplacer("{a}", "1,5 USD", "{b}", "1500 USD").Replace(es["rastrillo.ui.money_ambiguous"])))
	one, _ := newAppWith(t, "en", nil)
	if body := get(t, one, "/admin/ticket_types").Body.String(); strings.Contains(body, "rst-locale") || !strings.Contains(body, `<html lang="en" dir="ltr">`) {
		t.Errorf("a one-locale tickets has a switcher or the wrong language:\n%.400s", body)
	}
	// The edit form in Spanish seeds the amount in Spanish conventions
	// and says so in its hidden locale input.
	body := get(t, app, "/es/admin/ticket_types/1/edit").Body.String()
	wantContains(t, body, `name="Price.locale" value="es"`)
	wantContains(t, body, `value="1284,50"`)
}
```

(`rastrillo.ui.shell_language` is the switcher's summary, Language/Idioma, which the layout renders once two locales are offered.) `newAppWith(t, def, offered)` builds the handler through `rastrillo.Handler(rastrillo.Options{Locales: offered, DefaultLocale: def, BaseCatalog: genlocales.BaseCatalog, Router: …})` so the locale middleware, `CheckConfig` and `Install` run as in production; the test file blank-imports `amadan.net/rastrillo/rastrillo/locales/es` and `locales/ar`. `newApp(t)` becomes `newAppWith(t, genlocales.DefaultLocale, genlocales.Locales)`, and Task 21's `newAppAt(t, path)` becomes a thin wrapper over a `newAppWithAt(t, path, def, offered)` that both share, so the legacy-upgrade test keeps its database path.

- [ ] **Step 2: Run it to verify it fails**

Run: `make example-tickets`
Expected: FAIL (`undefined: newAppWith`, then Spanish output missing).

- [ ] **Step 3: Implement**

- Migrate the hand-written layout: `GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates -write examples/tickets/internal/tickets/templates`; check the review lines.
- Edit the migrated layout by hand: the codemod only inserts `$L` and cannot add markup. In `examples/tickets/internal/tickets/templates/layout.html`, replace the literal `<html lang="en">` with `<html lang="{{$L.Identity}}" dir="{{$L.Dir}}">` (the prelude the codemod added at the top of the `layout` body defines `$L`; strict rendering guarantees it is non-nil), and make `{{template "locale-menu" dict "L" $L "Items" .Locales "Return" .Return}}` the first child of `<div rst-page>`. `pageData` embeds the `ui.Localized` it copies from the view, so `.Locales` and `.Return` are there; for a one-locale deploy `.Locales` is empty and the partial renders nothing.
- `render.go`: `NewApp(cfg)` calls `ui.NewPages(ui.PagesConfig{App: layoutFS, Layout: "templates/layout.html", Pages: <one PageFile per gen/templates/<res>/<page>.html, FS: the gen sub-FS>, Locale: cfg, Catalog: genlocales.BaseCatalog})`. `Render` wraps the generated view in the layout's data with the view's request values: `v, ok := data.(interface{ Request() ui.Localized })`; when `!ok` (data that is not a generated view), log it with the page name (Appendix C `op.render.not_a_view`, second copy review) and answer 500 without executing, because strict rendering would refuse it anyway and a type-assertion panic would hide which page was wrong; otherwise `rv := v.Request()`; `pageData{Localized: rv, Head: …, Data: data}`, so the layout finds `L`, `Locales` and `Return` (Task 14); on an `Execute` error, log it and answer 500 (nothing was written, Task 13). Delete the `ui.WithT` bridge and `catalogT`.
- `main.go`: inside `Router`, after `gen.Boot` (Task 20) and before `gen.Router`: `cfg, err := locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)` (already validated by `Handler`; this is to get the value), `app, err := tickets.NewApp(cfg)`, ctx factory `Render: app.Render`, and serve `app.Middleware(mux)` from an outer `ServeMux` (`Router` must return a `*http.ServeMux`): `outer := http.NewServeMux(); outer.Handle("/", app.Middleware(mux)); return outer, nil`.
- Update `corpus_test.go`'s `examples` counts (tickets' hand template now migrated) and `gocheck_test.go` (tickets: zero findings).

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
make example-tickets
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -count=1
GOFLAGS=-mod=mod go run ./cmd/rastrillo doctor examples/tickets; echo "exit $?"
```
Expected: PASS; doctor's template preflight and locale checks report nothing for tickets. Its exit is 0, or 3 or 4 only for vendored-file drift or skew that predates this branch; if so, confirm with `git diff origin/main -- examples/tickets/static` (empty) and say so in the commit body.

- [ ] **Step 5: Commit**

```bash
git add examples/tickets internal/tmplmigrate
git commit -F - <<'MSG'
Serve the tickets example from one template set in every language

The generated screens render through ui.Pages, strict: a partial that
reached a template without the request's localizer would fail the
request rather than render in the wrong language. The new test serves
the same screens in English and Spanish from the one set, with money
written in each reader's conventions and still in dollars.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 24: The blog and notes examples on `ui.Pages`

Spec: §10, §12 item 8.
Starts from: `examples/blog/internal/blog/view.go:55-110`, `examples/blog/internal/blog/genrender.go`, `examples/notes/internal/notes/render.go`, `examples/notes/internal/notes/genrender.go`, the hand-written templates under `examples/blog/internal/blog/templates` and `examples/notes/internal/notes/templates`.

**Files:**
- Modify: the render files above, each example's `cmd/<app>/main.go`, and each hand-written template the codemod rewrites
- Modify by hand (the codemod cannot add markup): `examples/blog/internal/blog/templates/layout.html`, `examples/notes/internal/notes/templates/layout.html` and `examples/notes/internal/notes/templates/genlayout.html`, each of which opens with a literal `<html lang="en">` and renders no switcher
- Modify: each example's handlers that build page data, so map data carries `"L"` and struct data embeds `ui.Localized`
- Create: `examples/blog/internal/blog/locales_test.go` or the example's test package equivalent, and the same for notes: one two-locale test each (as Task 23's, on one hand-written page and one generated page)
- Modify: `internal/tmplmigrate/corpus_test.go` (`examples` reaches all zeros), `internal/tmplmigrate/gocheck_test.go` (zero findings for blog and notes)

**Interfaces:**
- Consumes: as Task 23.
- Produces: `internal/tmplmigrate`'s corpus test now pins every repo template at zero, which is the guard against a new template in the old syntax.

- [ ] **Step 1: Write the failing tests**

For each example, a locale test like Task 23's: one generated screen and one hand-written screen (blog: the post list; notes: the notes index and a generated bookmarks screen, so both of notes' layouts are covered), offered `en`, `es` and `ar`, asserting for each screen: a framework string in each language; `<html lang="es" dir="ltr">` under `/es/` and `<html lang="ar" dir="rtl">` under `/ar/`; `rst-locale` present; and, with one offered locale, `<html lang="en" dir="ltr">` and no `rst-locale`. For notes, also that the fragment endpoint (`fragmentTmpl`) renders through `ui.Pages`. Change `corpus_test.go`'s `examples` entry to `Stats{}`.

- [ ] **Step 2: Run them to verify they fail**

Run: `make example-blog example-notes; GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -run TestRepoTemplates -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

- `GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates -write examples/blog examples/notes`; read every review line; for each wrapped struct-data site, make the struct embed `ui.Localized` and set it where it is built, or turn the call's data into a `dict`. Every no-data `{{template "p"}}` call must get data.
- Build `ui.Pages` in each app the way Task 23 does, with `PageFile` per page (blog's `resourceSources()` already resolves each page's FS; keep that logic and feed it into `Pages`). Notes' `fragmentTmpl` becomes a page of the same `Pages` (a page whose `Entry`-level template is the fragment; if `Entry` must differ per page, add a second `ui.Pages` for fragments with `Entry: "fragment"` rather than widening the API).
- Hand handlers: every `render(...)` call passes the request's values in its data: `"L"`, `"Locales"` and `"Return"` from `ui.ForRequest(r)` for map data, an embedded `ui.Localized` set to `ui.ForRequest(r)` for struct data; error pages use `pages.LocalizerFor(r)` (Task 13), because they can be reached outside the middleware.
- In the three layouts, replace `<html lang="en">` with `<html lang="{{$L.Identity}}" dir="{{$L.Dir}}">` and render `{{template "locale-menu" dict "L" $L "Items" .Locales "Return" .Return}}`: in notes' two layouts as the last child of `<header>`, in blog's as the first child of `<div rst-page>`. The layouts' data carries `Locales` and `Return` from `ui.ForRequest(r)` (hand pages) or from the generated view's embedded `ui.Localized` (generated pages).
- A render function that receives data without request values logs Appendix C `op.render.not_a_view` (second copy review) and answers 500, as tickets does (Task 23).
- Delete the `ui.WithT` bridges.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
make example-blog example-notes example-tickets
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/tmplmigrate/ -count=1
for e in blog notes; do GOFLAGS=-mod=mod go run ./cmd/rastrillo migrate-templates examples/$e; echo "$e exit $?"; done
```
Expected: PASS; both dry runs exit 0.

- [ ] **Step 5: Commit**

```bash
git add examples/blog examples/notes internal/tmplmigrate
git commit -F - <<'MSG'
Serve the blog and notes examples through ui.Pages

With these, every template in the repository is in the localizer-first
syntax, and the codemod's corpus test pins that at zero so a new
template written the old way fails here rather than in an app's strict
render.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 25: The scaffold: one picked locale, `ui.Pages`, and money in its docs

Spec: §3 ("New apps start with one locale": `rastrillo new` writes the one picked, English by default, no prefixes and no switcher; `main.go` wires `Options.Locales` only with more than one; the scaffold puts `Locales` and `DefaultLocale` in the `Options` literal it passes to `Resolve`), §10 (the scaffold's three pages), §11 (`cmd/rastrillo/new.go:1612`), Appendix C.2 (`op.new.locale_unsupported`), B2 (`scaffold.agents.money`).
Starts from: `cmd/rastrillo/new.go:640-700` (`render.go`'s template), `:360-390` (`main.go`'s template), `:1600-1620` (the AGENTS.md money line), `hack/templatemem/testdata/scaffold/` (the scaffold's templates, which the codemod's counts covered).

**Files:**
- Modify: `cmd/rastrillo/new.go` (the `--locale` flag; `app.toml` with the picked code; `render.go` on `ui.Pages`; the scaffold's handlers pass the request into `render`; its templates in the new syntax; the AGENTS.md money line)
- Modify: `cmd/rastrillo/new_test.go`, `new_scaffold_test.go`, `doctor_test.go`
- Modify: `hack/scaffold-smoke.sh` if it asserts on template text it no longer finds

**Interfaces:**
- Consumes: `packs.Emit`, `manifest.App` (Task 6); `ui.NewPages`, `ui.PagesConfig`, `ui.LocalizerFrom` (Tasks 9, 13); `locales.CheckConfig`; `tmplmigrate.Run` (Task 11, in tests).
- Produces: `rastrillo new [--locale=<code>] <name>`; `<code>` must be one of `rastrillo.BaseLocales()` (the twelve languages whose framework strings ship), else `op.new.locale_unsupported`.

- [ ] **Step 1: Write the failing tests**

In `cmd/rastrillo/new_scaffold_test.go`. That file already scaffolds and builds an app in a temp dir; write two thin wrappers over its existing helper, `scaffoldApp(t, name string, flags ...string) (dir string)` (fails the test on error) and `runNewInTemp(t, args ...string) (string, error)` (returns the error), and use them:

```go
// A new app's templates are already migrated: the preflight finds
// nothing to do, and doctor is clean on day one.
func TestScaffoldNeedsNoTemplateMigration(t *testing.T) {
	app := scaffoldApp(t, "notes") // existing helper
	var out strings.Builder
	if code := tmplmigrate.Run([]string{app}, &out, &out); code != 0 {
		t.Errorf("migrate-templates dry run on a new app: exit %d\n%s", code, out.String())
	}
}

func TestNewWithLocale(t *testing.T) {
	app := scaffoldApp(t, "boutique", "--locale=es")
	toml, _ := os.ReadFile(filepath.Join(app, "manifest", "app.toml"))
	if !strings.Contains(string(toml), `locales = ["es"]`) || !strings.Contains(string(toml), `default_locale = "es"`) {
		t.Errorf("app.toml:\n%s", toml)
	}
	packs, _ := os.ReadFile(filepath.Join(app, "gen", "locales", "packs.go"))
	if !strings.Contains(string(packs), `"amadan.net/rastrillo/rastrillo/locales/es"`) || strings.Contains(string(packs), "locales/en\"") {
		t.Errorf("packs.go links the wrong packs:\n%s", packs)
	}
	if _, err := runNewInTemp(t, "--locale=de", "x"); err == nil {
		t.Error("rastrillo new accepted a locale whose framework strings do not ship")
	}
}
```

Language, direction, switcher and error pages in real scaffolds (review findings 7 and 8). In `new_scaffold_test.go`, with `scaffoldApp`, write one extra test file into each scaffolded app and run the app's own `go test` on it:

```go
// written into <app>/internal/<app>/locale_extra_test.go, package <app>
func TestLanguageDirectionSwitcherAndErrorPage(t *testing.T) {
	h := testHandler(t) // the scaffold's own test helper that builds the handler through rastrillo.Handler
	get := func(path string, header map[string]string) string {
		r := httptest.NewRequest("GET", path, nil)
		for k, v := range header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Body.String()
	}
	%s
}
```

The test fills `%s` per scaffold, as Go source the outer test writes:
- **one locale** (`rastrillo new notes`): `/` contains `<html lang="en" dir="ltr">` and no `rst-locale`.
- **two locales** (`rastrillo new notes`, then `locales = ["en", "es"]` in `manifest/app.toml` and `rastrillo generate`): `/` contains `rst-locale` and both autonyms; `/es/` contains `<html lang="es" dir="ltr">`; and `ErrorPage` called directly with `httptest.NewRequest("GET", "/es/x", nil)`, and with `Accept-Language: es` on `/x`, renders the Spanish value of the error-page partial's title (`rastrillo.BaseCatalogs()["es"][<that key>]`).
- **Arabic** (`rastrillo new --locale=ar notes`): `/` contains `<html lang="ar" dir="rtl">` and no `rst-locale`.

Add also to the scaffolded app's own test template (the `TestOverviewRenders` the scaffold writes, `new.go:761`) an assertion that the page renders through `ui.Pages` in strict mode: a request with no localizer in context still renders (the middleware supplies the default), and the response contains the default locale's framework string for the shell's menu label. Update `doctor_test.go`: doctor on a freshly scaffolded app exits 0.

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./cmd/rastrillo/ -run 'TestScaffoldNeedsNoTemplateMigration|TestNewWithLocale|Doctor' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

- Get the migrated text of the scaffold's templates mechanically: scaffold an app into `$TMPDIR` with the current CLI, run `migrate-templates -write` on its `internal/<app>/templates`, and port the result into `new.go`'s template constants. Do not hand-edit the syntax.
- `render.go` template: build `pages` once in `App()` from `locales.CheckConfig(genlocales.DefaultLocale, genlocales.Locales)` and `ui.NewPages` with the app's icons and `asset` in `Funcs`; `render(w, r, name, data)` puts `"L"`, `"Locales"` and `"Return"` from `ui.ForRequest(r)` in the data map and calls `pages.Execute`, answering 500 on error (`Execute` wrote nothing); `ErrorPage(w, r, status, ref)` builds its data with `"L": pages.LocalizerFor(r)` instead, because Serve's panic recovery and direct calls reach it outside the middleware (Task 13); `Mux` wraps the router in `pages.Middleware`.
- `main.go` template: `rastrillo.Resolve(rastrillo.Options{DBPath: …, Logger: logger, Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale})` (from Task 6), unchanged here except for comments.
- `--locale` flag, whose help is Appendix C.5's `flag.new.locale` and whose usage line is Appendix C `usage.new` (second copy review); validated against `rastrillo.BaseLocales()`; writes `app.toml` and `packs.Emit` with it.
- AGENTS.md money line: Appendix C B2 `scaffold.agents.money`, verbatim.
- Keep `cmd/rastrillo` within its 5,650-line ceiling (`go run ./cmd/rastrillo budget size`); the scaffold's templates are Go constants, so if the ceiling is exceeded, raise it in `.rastrillo/budgets.txt` by the measured overage with the reason "the scaffold's render.go now builds ui.Pages" rather than splitting `new.go` in this task.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./cmd/rastrillo/ -count=1
make scaffold-smoke
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo hack/scaffold-smoke.sh .rastrillo/budgets.txt
git commit -F - <<'MSG'
Scaffold new apps with one picked locale and ui.Pages

A new app offers the one language picked, English unless --locale
says otherwise, so it has no /xx/ prefixes and no switcher and links
one formatting pack. Adding a language is a line in manifest/app.toml.
Its templates are written in the localizer-first syntax and render
strictly, so doctor and the template preflight are clean on day one.

The picked locale is limited to the twelve whose framework strings
ship, because any other would fail generate --check on a missing
translation before the app's author has written a line.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 26: The gallery's Money sample

Spec: §9 ("The Money sample renders three currencies with `currency.Format` in the page's locale"), the old sample at `internal/designsystem/formats.go:84-93`, Appendix C.4 (`gallery.money.body`).
Starts from: `internal/designsystem/formats.go` (`formatDoc`, the `money` entry, the code at `:189` that builds previews), `internal/designsystem/prose.go:2161` (the old body's translations).

**Files:**
- Modify: `internal/designsystem/formats.go` (`formatDoc` gains `MarkupFor func(locale string) string`; the money entry uses it)
- Modify: `internal/designsystem/prose.go` (remove the old body and its eleven translations; add the approved body with eleven drafted translations)
- Create: `internal/designsystem/packs.go` (blank imports of the twelve gallery packs: the gallery is not an app and has no `gen/locales`)
- Create: `internal/designsystem/money_browser_test.go` (build tag `browser`): the spec's "browser runs of the gallery … with JavaScript off"
- Modify: `internal/designsystem/designsystem_test.go` (or the formats page's test file)

**Interfaces:**
- Consumes: `currency.Format` (Task 4); `locales.CheckConfig` over `rastrillo.BaseLocales()` to map each gallery locale to its formatting locale.
- Produces: nothing new for other tasks.

- [ ] **Step 1: Write the failing test**

```go
// The Money sample writes three currencies the way each page's
// language does; the currency comes from the data, so the euro stays a
// euro on the Japanese page.
func TestMoneySampleIsFormattedPerLocale(t *testing.T) {
	for locale, wants := range map[string][]string{
		"en": {"€1,284.50", "¥1,285", "KWD\u00a01,284.500"}, // English writes the yen with U+00A5
		"ja": {"￥1,285"},                                    // Japanese with the fullwidth U+FFE5 (Appendix A.1)
		"es": {"1284,50\u00a0€"},
		"pt": {"€\u00a01.284,50"}, // CLDR pt is Brazilian; Task 27 retags the gallery to pt-PT
		"ar": {"\u200f1,284.50\u00a0€\u200f"},
	} {
		got := moneySampleMarkup(locale)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", locale, got, w)
			}
		}
	}
}
```

The three stored amounts are `128450 EUR`, `1285 JPY` and `1284500 KWD`. The en and ja yen differ on purpose: the pinned `en` pack's symbol for JPY is `¥` (U+00A5) and `ja`'s is `￥` (U+FFE5), so the test pins both. The expected strings are Appendix A.1's where it has them (en, es, pt); compute any you add from `currency.Format` only after checking one by hand against Appendix A.1's table, and keep at least the three above. The `ar` line is the expected CLDR shape; if `currency.Format("ar", 128450, "EUR")` differs, use its output and say so in the commit body (EUR in ar is not in Appendix A.1).

`internal/designsystem/money_browser_test.go`, following the existing JavaScript-off gallery tests (`prefs_browser_test.go` and `index_browser_test.go` disable script execution with `emulation.SetScriptExecutionDisabled(true)` on the gallery rig):

```go
//go:build browser

// The Money sample is server-rendered text: with no JavaScript, the
// Spanish page shows the amounts written the Spanish way, and the
// Form page's field-money control is a plain input with its hidden
// locale and currency.
func TestMoneySamplesWithNoScript(t *testing.T) {
	rig := galleryRig(t) // the helper the other gallery browser tests use
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var formats, form string
	mustRun(t, ctx, emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(rig.Origin+"/es/formats/"), chromedp.Text("#money", &formats, chromedp.ByQuery),
		chromedp.Navigate(rig.Origin+"/es/form/"), chromedp.OuterHTML("[name='fm_bare']", &form, chromedp.ByQuery))
	if !strings.Contains(formats, "1284,50\u00a0€") {
		t.Errorf("Spanish money sample with no script: %q", formats)
	}
	if !strings.Contains(form, `inputmode="decimal"`) {
		t.Errorf("field-money with no script: %s", form)
	}
}
```

Use the gallery's real paths and section ids for the formats and form pages (the `-money` anchor is `formats.go`'s `Key`), and the existing rig helper's name.

- [ ] **Step 2: Run it to verify it fails**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/designsystem/ -run TestMoneySampleIsFormattedPerLocale -count=1` (and, with Chrome, `go test -tags browser ./internal/designsystem/ -run TestMoneySamplesWithNoScript -count=1`)
Expected: FAIL (`undefined: moneySampleMarkup`).

- [ ] **Step 3: Implement**

`moneySampleMarkup(locale)` returns the same `<section rst-box><dl rst-detail>` shape as today's sample, with three `<dt>`/`<dd>` pairs: the `<dt>` is the ISO code (no new words to translate), the `<dd>` the formatted amount, HTML-escaped. The money `formatDoc` sets `MarkupFor: moneySampleMarkup`, and the preview code at `formats.go:189` uses `MarkupFor(locale)` when set. The formatting locale for a gallery page is `CheckConfig("en", rastrillo.BaseLocales()).Formats[locale]`, computed once. Replace the body in `formats.go` with Appendix C.4's `gallery.money.body`; in `prose.go`, delete the old English key and its translations and add the new key with eleven drafted translations; check that `proseCode` renders `money` in the body as code.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/designsystem/ ./cmd/dsgen/ -count=1 -short`
Expected: PASS, including the prose completeness tests (every English key has eleven translations).

- [ ] **Step 5: Commit**

```bash
git add internal/designsystem
git commit -F - <<'MSG'
Show money in three currencies on every gallery page

The sample used to admit Rastrillo only wrote US dollars. It now
formats euros, yen and dinars with the shipped formatter in each
page's language, which also shows the rule the body states: the
currency comes from the data, never from the reader. The eleven
translations of the new body are unreviewed drafts.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 27: Retag the gallery's and the framework's Portuguese as `pt-PT`

Spec: decision 8, §9 ("The `pt` → `pt-PT` retag": the catalog moves, `Options.Locales` lists `pt-PT`, `/pt/` redirects 301, a `rastrillo_locale=pt` cookie reads as `pt-PT`, `pt` lookups fall back to `pt-PT`, an app's own `pt.toml` keeps working, Portuguese money reads `1284,50 €`).

The spec lists this retag both as decision 8 (§9) and under §13's follow-on work. It is planned here as its own task, last of the code changes, because decision 8 is Paul's and the brief asked for it; it is independent of everything after Task 26 and can be split onto its own branch without touching other tasks.

**Files:**
- Rename: `locales/pt.toml` to `locales/pt-PT.toml`
- Modify: `basecatalog.go` (`baseLocales`), `locale.go` (`NewLocales`: framework catalog fallback and app catalog layering), `localemw.go` (prefix redirect, cookie), `internal/generate/locales.go` (`MissingFrameworkKeys` treats `pt` as shipped through `pt-PT`)
- Create: `retag.go` (root): the one retag table
- Modify: `internal/generate/catalogs/catalogs.go` and `catalogs_test.go` (Task 6's `Check` decides framework coverage from `rastrillo.BaseLocales()`, which now lists `pt-PT`; an app offering plain `pt` must still count as covered through `rastrillo.RetagOf`, or `generate --check` and doctor would demand a `locales/pt.toml` the app never needed)
- Check: `ui/datetime.js` and `ui/testdata/datetime/regressions.json` (cases with `"lang": "pt"`): if the parser keys any behaviour on the page's full `lang` rather than its primary subtag, add `pt-PT` twins of those cases; if it uses the primary subtag (as `rastrillo.Dir` does), note that in the commit body
- Modify: every test that lists `rastrillo.BaseLocales()` by value or names `pt` (`basecatalog_test.go`, `internal/designsystem` and `internal/designsystem/sweep` tests, `cmd/dsgen/main_test.go`), found with `grep -rn '"pt"\|`pt`' --include=*_test.go . | grep -v hack`
- Modify: `internal/designsystem/prose.go` (every `` `pt`: `` entry becomes `` `pt-PT`: ``), `internal/copyedit`, and any other `"pt"` literal `grep -rn '"pt"' --include=*.go . | grep -v '^./hack'` finds
- Modify: `cmd/dsgen/main.go` (write a redirect stub under `pt/` for every page it writes under `pt-PT/`)
- Tests: `locale_test.go`, `localemw_test.go`, `basecatalog_test.go`, `internal/generate/locales_test.go`, `internal/designsystem` (the money sample's pt expectation), `cmd/dsgen/main_test.go`

**Interfaces:**
- Produces (root, unexported): `var retagged = map[string]string{"pt": "pt-PT"}` and `func retagOf(code string) (string, bool)`; used by `NewLocales`, the locale middleware and `MissingFrameworkKeys` (exported for that as `rastrillo.RetagOf(code string) (string, bool)` since `internal/generate` is another package).

- [ ] **Step 1: Write the failing tests**

```go
func TestPTRetagKeepsOldURLsAndCookies(t *testing.T) {
	h := localeHandler(t, []string{"en", "pt-PT"}) // existing localemw_test helper shape
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/pt/notes?x=1", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/pt-PT/notes?x=1" {
		t.Errorf("/pt/notes: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	r := httptest.NewRequest("GET", "/notes", nil)
	r.AddCookie(&http.Cookie{Name: LocaleCookie, Value: "pt"})
	if got := negotiatedLocale(t, h, r); got != "pt-PT" {
		t.Errorf("cookie pt read as %q", got)
	}
	// An app that still offers plain pt keeps it: the retag applies
	// only when pt-PT is offered and pt is not.
	h = localeHandler(t, []string{"en", "pt"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/pt/notes", nil))
	if rec.Code == http.StatusMovedPermanently {
		t.Error("an app offering pt was redirected")
	}
}

func TestPTCatalogsUnderBothCodes(t *testing.T) {
	fsys := fstest.MapFS{"locales/pt.toml": {Data: []byte("\"app.hello\" = \"Olá\"\n")}}
	l, err := NewLocales([]string{"en", "pt-PT"}, "en", nil, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.T("pt-PT", "app.hello"); got != "Olá" {
		t.Errorf("an app's pt.toml under pt-PT: %q", got)
	}
	if got := l.T("pt-PT", "rastrillo.ui.cancel"); got == "rastrillo.ui.cancel" || got == BaseCatalog()["rastrillo.ui.cancel"] {
		t.Errorf("framework pt-PT catalog not found: %q", got)
	}
	l2, _ := NewLocales([]string{"en", "pt"}, "en", nil, nil)
	if l2.T("pt", "rastrillo.ui.cancel") != l.T("pt-PT", "rastrillo.ui.cancel") {
		t.Error("an app offering pt lost the framework's Portuguese")
	}
}
```

In `internal/generate/catalogs/catalogs_test.go`, add the case `"retagged pt still covered": {manifest.App{Locales: []string{"en", "pt"}, DefaultLocale: "en"}, nil, ""}` to `TestCheck`; it fails before this task's change to `Check` and passes after.

Change Task 26's gallery expectation for `pt` to the `pt-PT` page: `"1284,50\u00a0€"` (spec §9, Appendix A.1 pt-PT EUR). Add a `cmd/dsgen` test that a `pt/` stub exists for a page and its body is a `<meta http-equiv="refresh">` to the `pt-PT/` URL with a `rel="canonical"` link (a static host cannot answer 301; the stub keeps old gallery links working).

- [ ] **Step 2: Run them to verify they fail**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . ./internal/generate/... ./internal/designsystem/ ./cmd/dsgen/ -run 'PT|Retag|MoneySample|Stub|TestCheck' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** the rename and the retag table. In `NewLocales`, the framework catalog for an offered code `c` is `baseCatalogs[c]`, or, when there is none and `c` is retagged, `baseCatalogs[retagged[c]]` (so an app offering `pt` gets `pt-PT`'s); for an offered `pt-PT`, the app catalog layers `locales/pt-PT.toml` over `locales/pt.toml` when both exist. The middleware redirects a `/pt/` prefix with 301 to `/pt-PT/` and reads a `pt` cookie as `pt-PT`, both only when `pt-PT` is offered and `pt` is not. `MissingFrameworkKeys` treats an app's `pt` as covered by the shipped `pt-PT`. Rename every `pt` prose key. `dsgen` writes the stubs.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go test . ./internal/... ./cmd/dsgen/ -count=1 -short
make generate-check example-blog example-tickets example-notes scaffold-smoke
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A locales basecatalog.go locale.go localemw.go retag.go internal cmd/dsgen ui *_test.go
git commit -F - <<'MSG'
Retag the framework's Portuguese as pt-PT and keep pt working

The catalog was always European Portuguese, but tagged pt it formats
money in Brazilian conventions (€ 1.284,50). As pt-PT it reads
1284,50 €. Old /pt/ URLs redirect permanently, a pt cookie reads as
pt-PT, an app's own pt.toml is read under both codes, and an app that
offers plain pt keeps it, so nobody's links or catalogs break.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 28: Copy review, batch two: the docs pages, `DESIGN.md`, the changelog's API list and the migration guidance; write the docs pages

Spec: §11 (every docs item), §3 (the localization.md doc line), Appendix C.
Starts from: the current `docs/site/*.md` pages §11 names, and the shipped behaviour of Tasks 2-27.

A human gate, like Task 1, for prose that can only be drafted once the code exists. It reviews everything Task 29 will write except `SKILL.md` (which an agent loads instead of the source; it is reviewed like code, per AGENTS.md, and must agree with the approved docs): the docs pages, the new `DESIGN.md` money section, the changelog's list of new API (its upgrade steps were approved in Task 1 as C.3), and the migration guidance spec §11 requires removing or rewriting outside the docs site: the `ui.FuncsWith` and `ui.WithT` doc comments in `ui/funcs.go` (today they teach the per-request Clone pattern), and the scaffold's `manifest/README.md` and `README.md` text in `cmd/rastrillo/new.go` (today they teach the old renderer and wiring). The docs pages are written here; the other approved text is recorded in Appendix C.8 for Task 29.

**Files:**
- Modify: `docs/site/forms.md` (minor units; the field options, `Strict` and `Stored` among them; the normalising and strict readings with §5.1's examples table, decision 23's accepted cost and the shared-symbol families; the unchanged-seed check; the errors, `money_wrong_currency` and `money_ambiguous` included; `field-money`; all three hidden inputs; what follows was the old wording: the parsing rules, the errors, `field-money`, both hidden inputs), `docs/site/templates.md` (lines 374-392: `{{money}}`, remove "does not do this for you yet"; every `{{T …}}` example becomes `{{T $L …}}`; partial calls pass `"L" $L`; the per-request Clone pattern at :48 removed in favour of the localizer), `docs/site/reference/form.md` (the new options and the legacy path), `docs/site/reference/ui.md` (`money`, `ui.Pages`, the localizer, `$L`, strict mode, the call-syntax table from §10), `docs/site/manifests.md` (`currency`, `[legacy]`, `app.toml`'s `locales` and `default_locale`, the column replacement, the boot refusals), `docs/site/localization.md` (the doc line from §3), `docs/site/reference/money.md` (the storage set), `docs/site/reference/eventlog.md` (`MoneySchema`, when to include it, the column detection), `docs/site/cli.md` (`migrate-templates`, doctor's new checks and exit codes 6 and 7, `new --locale`)
- Create: `docs/site/reference/currency.md`, `docs/site/reference/locales.md` (the packs, the fallback, `CheckConfig`, the `RASTRILLO_CHECK_CONFIG` probe, `doctor`), `docs/site/migrate-templates.md` (the command, its dry run, the review list)
- Modify: `docs/site/nav.json` (the three new pages)
- Modify: `docs/superpowers/specs/2026-10-09-multi-currency-money-design.md` (Appendix C.8: `design.money`, `changelog.api`, `doc.funcswith`, `doc.witht`, `scaffold.manifest_readme`, `scaffold.readme_render`)
- Modify: `internal/docsite/symbols_test.go` (removes `owedPages`, `localePackCoverage`'s `seenOwed` bookkeeping and the two guards checking them, and adds `locales` and `currency` to `referencePages`)

- [ ] **Step 1: Draft** every change as Markdown, following the repo's "User-facing copy" rules (short sentences, plain words, tell the reader what to do; the reasoning belongs in code comments and the spec; no em dashes). Use the `humane-docs` skill's guidance for structure. Every code example must be copied from code that compiles in this branch (a scaffolded app or an example), not written fresh.
  Draft also, as Markdown with the same rules: the `DESIGN.md` money section (replacing `DESIGN.md:315-324`, matching decisions 1, 2, 5, 13 and 21); the changelog's bullet list of new API (`currency`, `locales`, `locales/<lang>`, `locales/modern`, `form.Field.Currency/Negative/Locale/LocaleEcho`, `form.MoneyInput`, `form.SeedMoney`, `Parsed.Minor`, `Parsed.MoneyInput`, `ui.Pages`, `ui.Localizer`, `ui.Localized`, `ui.ForRequest`, `field-money`, `eventlog.MoneySchema`, `eventlog.UpgradeForMoney`, `gen.Boot`, `rastrillo.ProbeConfig`, `rastrillo.FormatLocale`, `(*rastrillo.Locales).Negotiate`, `rastrillo migrate-templates`, `rastrillo new --locale`, doctor exit codes 6 and 7, the removed `generate --default-locale`), one sentence each; the new `ui.FuncsWith` and `ui.WithT` doc comments (they remain for apps that keep their own rendering until they migrate; they point at `ui.Pages` and say the per-request Clone pattern is no longer the recommendation); and the scaffold's `manifest/README.md` and `README.md` passages about rendering and wiring (`gen.Boot` before `gen.Router`, `ui.Pages`, `{{$L := localizer $}}`).
- [ ] **Step 2: Review.** Invoke the `copy-review` skill with every draft as a rendered Markdown preview (the skill renders docs; Go doc comments are previewed as the text `go doc` prints). Each entry's `context` says where it appears (which page, which file, which command shows it). Wait for Paul's edits and apply them verbatim.
- [ ] **Step 3: Write** the approved pages into `docs/site/`, and record the other approved text as Appendix C.8 in the spec. Run the docs site's own checks: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./internal/docsite/ -count=1` (link and nav checks).
- [ ] **Step 4: Remove `copy-review/` and commit**

```bash
rm -rf copy-review
git add docs/site docs/superpowers/specs/2026-10-09-multi-currency-money-design.md
git commit -F - <<'MSG'
Document multi-currency money, language packs and ui.Pages

Reviewed before writing. The pages tell an app author what to do:
store minor units with a code, declare locales in app.toml, run
migrate-templates and doctor, call gen.Boot before gen.Router, and
include eventlog.MoneySchema in hand-written apps whose events carry
money. Why each piece works the way it does stays in the
code comments and the spec.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 29: `SKILL.md`, `DESIGN.md`, the CHANGELOG, the CLI help and the migration guidance in code

Spec: §11 (`SKILL.md:233-235`, `:107-110`, `:494-500`; `DESIGN.md:315-324`; the CHANGELOG; "the per-request Clone pattern (templates.md:48, SKILL.md:494, the `ui.FuncsWith` comment) is removed in favour of the localizer"), §8 (the changelog draft, approved as Appendix C.3), Appendix C.8 (Task 28).
Starts from: those line ranges; `skillmd_test.go:142` (`skillBudget = 30_750`, the file at 30,747 bytes).

**Files:**
- Modify: `SKILL.md`, `skillmd_test.go` (only if the ceiling must move), `DESIGN.md`, `CHANGELOG.md`, `cmd/rastrillo/main.go` (`usage()` lists `migrate-templates` with C.5's `usage.migrate_templates`, and `generate` with `usage.generate`), `ui/funcs.go` (the `FuncsWith` and `WithT` doc comments, C.8's `doc.funcswith` and `doc.witht`), `cmd/rastrillo/new.go` (the scaffold's `manifest/README.md` and `README.md` passages, C.8's `scaffold.manifest_readme` and `scaffold.readme_render`)

- [ ] **Step 1: Write the failing check**

Add to `skillmd_test.go`:

```go
// SKILL.md is what an agent loads instead of the source. After this
// release, the per-request Clone recipe and ui.WithLocale binding it
// taught fail in strict mode, and money is no longer cents only; an
// agent following the old lines writes code that does not render.
func TestSkillTeachesTheLocalizerAndCurrencyContract(t *testing.T) {
	b, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, gone := range []string{"Bind `ui.WithLocale(rastrillo.LocaleFrom(r))`", "each request's cloned template", "Money is `int64` cents"} {
		if strings.Contains(s, gone) {
			t.Errorf("SKILL.md still says %q", gone)
		}
	}
	for _, want := range []string{"ui.Pages", "{{$L := localizer $}}", "{{money $L", "form.SeedMoney", "Strict: true", "manifest/app.toml", "RASTRILLO_CHECK_CONFIG", "gen.Boot"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md does not mention %q", want)
		}
	}
}
```

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . -run 'TestSkillTeachesTheLocalizerAndCurrencyContract' -count=1`
Expected: FAIL, naming the three old phrases (`SKILL.md:494-495` and `:233`) and the missing new terms. The existing byte-budget test (`go test . -run Skill`) passes now and must still pass after Step 2.

- [ ] **Step 2: Edit**

- `SKILL.md:233-235` (forms and money; the phrases the new test names come from `SKILL.md:233` and `:494-495`): replace the cents sentence with the new contract, in the fewest words that stay accurate (money fields read input normalised by default, `Strict: true` keeps the exact format, and the edit form's `<Name>.seed` is why an unchanged ambiguous seed saves): `form.Field{Kind: form.Money, Currency: code, LocaleEcho: true}`, read `p.Minor`, seed `form.SeedMoney`, render `field-money`, display `{{money $L .Amount .Currency}}`; `FormatCents` still works for USD-only legacy code.
- `SKILL.md:107-110` (locales): `manifest/app.toml`'s `locales`/`default_locale`, packs derived, `RASTRILLO_CHECK_CONFIG=1` probes, boot refuses missing packs.
- `SKILL.md:494-500`: remove the per-request Clone and `ui.WithLocale` guidance; templates render through `ui.Pages` with `{{$L := localizer $}}` and localizer-first calls; strict by default.
- `SKILL.md` §1's `main.go` order gains nothing new except that `Resolve` probes first; say it in one clause if it fits.
- Byte budget: trim genuinely redundant prose first (the old Clone recipe frees bytes). If a load-bearing fact still cannot fit, raise `skillBudget` by the exact overage and extend the comment above it with why (the release changed how every template calls `T`, which the file must say), as `AGENTS.md` requires. Never cut a load-bearing fact to fit.
- `DESIGN.md:315-324`: Appendix C.8's `design.money`, verbatim.
- `ui/funcs.go`: the `FuncsWith` and `WithT` doc comments become C.8's `doc.funcswith` and `doc.witht`; the per-request Clone recipe is gone.
- `cmd/rastrillo/new.go`: the scaffold's README passages become C.8's text. A scaffold test that greps for the old wording is updated to the new.
- `CHANGELOG.md`: a new top section for the next release containing Appendix C.3's approved changelog text verbatim, followed by the normalising paragraph (Appendix C `changelog.money.normalising`, second copy review, unless Task 1's review of the §8 draft already covered it: §8 now ends with that paragraph, and Task 1 copies §8 verbatim), with its step 4 sentence about money columns replaced by Appendix C `changelog.money.step4` (second copy review: C.3 says the columns "are renamed", but Task 17 replaces them), first in the release (it can damage data and changes behaviour without a compile error), plus Appendix C.8's `changelog.api` list, verbatim.
- `cmd/rastrillo/main.go` `usage()`: the approved lines.

- [ ] **Step 3: Run the checks**

Run: `GOFLAGS=-mod=mod CGO_ENABLED=0 go test . -run 'Skill|Changelog|Markup' -count=1 && GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./cmd/rastrillo/ ./ui/ -run 'Usage|New|Scaffold|Funcs' -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add SKILL.md skillmd_test.go DESIGN.md CHANGELOG.md cmd/rastrillo ui/funcs.go
git commit -F - <<'MSG'
Teach SKILL.md and the changelog the new money and template contract

SKILL.md is what an agent loads instead of the source, so the old
per-request Clone recipe and the cents-only money line would now
mislead it into code that fails in strict mode. The changelog leads
the release with the upgrade steps, because apps that store money
need a backup and a stop-start and every app needs the template
preflight.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

### Task 30: Retire the promoted prototypes and run the full gate

Spec: Appendix B.
Starts from: what is left of `hack/templatemem/` (its packages were deleted by the tasks that ported them: `entry` in Task 7; `codemod`, `cmd/migrate-templates`, `pages` and `main.go` in Task 11; `upgradeprobe` in Task 15; what remains is `go.mod`, `go.sum` and `testdata/scaffold/`, which Task 13 copied), the `Makefile`'s `money-cldr` target and its comment.

**Files:**
- Delete: the rest of `hack/templatemem/`
- Modify: `Makefile` (the `money-cldr` comment still describes the template measurements Task 15's edit removed; rewrite it to what the target does now), `hack/moneycldr/doc.go` (what remains: the generator, the oracle, the pack-size and x/text measurements)
- Modify: `docs/superpowers/specs/2026-10-09-multi-currency-money-design.md` (one paragraph at the top of Appendix B: "Implemented. The prototypes in `hack/templatemem` were promoted and removed; their tests live in `ui`, `internal/tmplmigrate`, `eventlog`, `currency/sqlmoney` and the root package. `hack/moneycldr` remains as the data generator and the ICU oracle.")

- [ ] **Step 1: Check every prototype test has a home**

List each test function the deleted prototypes held (`git show $(git merge-base HEAD origin/main) --stat -- hack/templatemem` lists the files; read them with `git show`) and name the test that replaced it (Tasks 7, 10, 11, 13, 15, 17). Any without a replacement is ported now, in the package that owns its code. Put the list in the commit body.

- [ ] **Step 2: Delete and repoint**

```bash
git rm -r hack/templatemem
```

Update the `money-cldr` comment.

- [ ] **Step 3: Run the full gate, once**

```bash
cd /home/paulca/amadan.net/rastrillo/rastrillo/.claude/worktrees/money-currencies
df -h . "$TMPDIR" | tail -2
make ci
make money-cldr
GOFLAGS=-mod=mod go run ./cmd/rastrillo budget size
```

Expected: `make ci` passes (gofmt, chromedp-graph, gorm-free, generate-check, and all 33 suite roles including staticcheck, govulncheck, gitleaks, budgets, race, the four examples, scaffold-smoke and the browser roles, which exercise `field-money` and the gallery with JavaScript off). `make money-cldr` passes with no diff under `locales/` and the oracle's completion line. If a browser role fails, fix the cause in the task that owns it, as a new commit on this branch, and rerun only that role (`make browser.<role>`), then `make ci` once more at the end.

- [ ] **Step 4: Commit**

```bash
git add -A hack Makefile docs/superpowers/specs/2026-10-09-multi-currency-money-design.md
git commit -F - <<'MSG'
Retire the template and upgrade prototypes now that they ship

Each prototype test now lives beside the code it checks, so the copies
in hack/templatemem could only drift. hack/moneycldr stays: it
regenerates the CLDR data and runs the ICU oracle against the shipped
currency package.

Prototype tests and their replacements: <list>.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```

---

## Self-review

**Spec coverage.** §1 stored shape and storage rule: Tasks 2, 17, 21. §2 packages: Tasks 2, 3, 4. §3 packs, declaration, resolution, `CheckConfig`, the probe and its placement, doctor, unlinked-pack warning, prefixes and switcher: Tasks 3, 4, 6, 7, 11, 14. §4 display: Tasks 4, 9, 21. §5 parsing and the form contract: Tasks 4, 8, 21. §6 error copy: Tasks 1, 8. §7 manifest, legacy migration, boot checks, events: Tasks 16, 17, 18, 20, 21, 22. §8 upgrade, rollback, tripwires, changelog: Tasks 1, 15, 17, 20, 21, 22, 29. §9 gallery sample and retag: Tasks 26, 27. §10 templates: Tasks 9-14, 19, 23-25. §11 docs: Tasks 28, 29. §12 test plan: items 1-3 Tasks 17, 21; 4-5 Tasks 8, 21; 6 Task 16, 22; 7 Task 6; 8 Tasks 13, 23, 24; 9 Task 7; 10 Tasks 3, 4; 11 Tasks 15, 22; 12 Tasks 7, 11; 13 Tasks 17, 21, 22; 14 Tasks 8, 18, 21, 26, 30. §13 out of scope: nothing planned beyond the retag, which decision 8 asks for.

**Review round one (Astra, 22 findings), where each is closed.** 1 explicit `[legacy] scale`: Task 18. 2 startup wiring for every store mix and existing apps: Tasks 20, 21, 22 (`gen.Boot` required by `gen.Router`; `TestRouterRequiresBoot`, the three-app wiring table, `TestLegacyDatabaseIsUpgradedAtBoot`). 3 stale generated files: Task 20 (`Prune`, `Stale`, regeneration over an existing tree). 4 boundary on every read: Tasks 16, 22. 5 adding money to an existing table: Tasks 17, 21. 6 clearing: Tasks 16, 21, 22. 7 error pages outside the middleware: Tasks 13, 25. 8 switcher and `lang`/`dir`: Tasks 14, 23, 25. 9 declared catalogs and the default from `app.toml`: Task 6. 10 existing `ui` tests: Task 9. 11 manifest fixtures: Task 18. 12 every regenerated example and its consumers: Task 21 and the Global Constraints rule. 13 the generate-check baseline: Global Constraints and every regenerating task. 14 tickets' layout path: Task 23. 15 money in both groups, en→de, JavaScript off: Tasks 8, 21, 26. 16 copy inventory and previews: Tasks 1, 28 and the Global Constraints rule. 17 allocation tests: Task 13. 18 isolated warning test: Task 4. 19 comment bytes and look-alike text: Task 10. 20 SQLite error texts and the real old handler: Tasks 17, 21. 21 the English yen: Task 26. 22 `FuncsWith` comment and scaffold READMEs: Tasks 28, 29.

**Review round two (four open items).** A, released generated migrations vs ledger checksums: Task 21 makes generated `migrations.go` append-only once released, with `Boot` owning money columns, tested by `TestLedgerAppUpgradesThroughApplyAndBoot`; Task 17 lets an empty legacy column convert without `[legacy]`. B, the examples' own layouts: Tasks 23 and 24 edit them by hand and test `lang`/`dir`/switcher in `es`, `ar` and one-locale form. C, `Booted` carries the checked `*sql.DB` and `Router` refuses any other before dispatch (`TestRouterRefusesADatabaseBootDidNotCheck`, Task 20). D, `Prune` keeps sqlc output only in current exclusive store directories (`TestExclusiveToMergeableTransition`, Task 20).

**Preflight scan (2026-10-11).** Every row and finding of `.superpowers/sdd/2026-10-10-multi-currency-money/preflight.md` is addressed in the task it names; the strings it found without an Appendix C id are rows 1-18 of "Strings needing a second copy review" below.

**Spec amendment (decisions 22 and 23, §5.1-§5.3, commits `f69ef958..c62d25e6`).** Task 2 copies the packs' narrow symbols (`Locale.Narrow`); Task 4 promotes the normalising parser as `currency.ParseNormalized` with its hand-written tables and, in their own time-budgeted package, its properties; Task 8 adds `Field.Strict`, `Field.Stored`, the `<Name>.seed` check (with the verified seed kept through a failure re-render, `c62d25e6`) and the two new errors with their arguments, porting the prototype's field tests; Task 21 seeds and checks seeds in generated forms and runs §12 item 15 through tickets and the KWD scratch app; Task 23 checks the new messages in Spanish; Tasks 28 and 29 document `Strict` and the normalising changelog paragraph. Rows 19-25 of the strings section are the amendment's new or changed strings.

**Placeholders.** Strings that only Paul's review can fix are referenced by their Appendix C id, which Task 1 (or Task 28 for C.8) creates before any task uses them; nothing else is deferred.

**Type consistency.** `locales.Data` (not `Resolve`) for merged data; `locales.Resolve(tag) Resolution` for tags; `currency.Legacy{Currency string; Scale int}` (runtime) versus the manifest's `rastrillo.Legacy{Currency string; Scale *int}` (declaration; the generated `Boot` dereferences after validation); `ui.Localized{L, Locales, Return}` with `ui.ForRequest(r)` in Tasks 9, 14, 19, 23-25; `ui.PageFile` in Tasks 13, 23, 24; `(*ui.Pages).LocalizerFor` in Tasks 13, 25; `form.MoneyInput{Value, Locale, Currency}` in Tasks 8 and 21; `eventlog.StreamWriter` in Tasks 15 and 16; `replay.Rules`/`replay.Money` in Tasks 16 and 22; `gen.Boot`/`gen.Booted` in Tasks 20-25.

## Strings needing a second copy review

Strings that tasks write and that have no Appendix C id from Task 1, or whose Task 1 draft the spec's §5.1 amendment (decisions 22 and 23) made wrong. Each gets the id shown when reviewed (add them to Appendix C as a new section, C.9) before the task that writes it. No wording is drafted here; the description says what the string must convey and where it appears.

| # | Task | Proposed id | What it is, where it appears |
|---|---|---|---|
| 1 | 6 | `scaffold.app_go_packs` | The comment on the `_ "<module>/gen/locales"` import in a scaffolded app's `internal/<app>/app.go`: why the packs are linked from the app package rather than `main` (so the app's tests link them, since boot refuses missing packs). Read by the app's author. |
| 2 | 9 | `op.funcs.arity` | Error from a locale template function (`T`, `Tf`, `Tbdi`, `number`, `displayValue`, `dateWords`) called with the wrong number of arguments; names the function and the counts. Stops the template; seen in logs and test output. |
| 3 | 9 | `op.funcs.arg_type` | Error from the same functions when an argument has the wrong type (a non-string key); names the function, the argument and its type. |
| 4 | 11 | `op.doctor.heading_templates` | The heading `rastrillo doctor` prints above the template preflight's findings. |
| 5 | 11 | `op.doctor.heading_locales` | The heading `rastrillo doctor` prints above the probe's report and the static locale checks. |
| 6 | 13 | `op.pages.no_format` | `ui.NewPages` refusal: an offered identity has no formatting locale, so the config was not `locales.CheckConfig`'s result; names the identity. |
| 7 | 13 | `op.pages.offered_twice` | `ui.NewPages` refusal: an identity appears twice in the offered list. |
| 8 | 13 | `op.pages.default_not_offered` | `ui.NewPages` refusal: the default identity is not among the offered ones. |
| 9 | 13 | `op.pages.no_page` | `Pages.Execute` error: no page by that name. |
| 10 | 13 | `op.pages.base_parse` | `ui.NewPages` wrap for a parse error in the layout or shared files. |
| 11 | 13 | `op.pages.page_parse` | `ui.NewPages` wrap for a parse error in one page's file; names the page. |
| 12 | 15 | `op.eventlog.no_boundary` | `eventlog.UpgradeBoundary` error when the upgrade snapshot table does not exist (the money upgrade has not run on this database). |
| 13 | 16 | `op.replay.bad_version` | `replay.Decode` error: an event's `V` is present but not an integer; names the event. |
| 14 | 21 | `op.money.display_failed` | Operator log line when a generated action cannot format a stored amount and answers 500; names the resource, row id, field and currency code. |
| 15 | 22 | `op.replay.stream` | Wrap `CheckReplay` puts around a replay error so boot's refusal names the stream. |
| 16 | 23, 24 | `op.render.not_a_view` | Operator log line when an example's render function receives data that carries no request values (not a generated view); names the page. |
| 17 | 25 | `usage.new` | The `rastrillo help` usage line for `new`, now with `--locale`. |
| 18 | 29 | `changelog.money.step4` | A corrected sentence for step 4 of the approved changelog (C.3): it says money columns "are renamed", but Task 17 replaces each with `<name>_minor` and `<name>_currency`; the sentence must say what an old version meets instead. |
| 19 | 8 | `rastrillo.ui.money_wrong_currency` | Spec §6 draft (new, normalising): the error under a money field when the text names another currency explicitly (`€5` in a USD field). Interpolates `{currency}`, the field's ISO code. Translated into eleven languages after approval; the English is the catalog value. |
| 20 | 8 | `rastrillo.ui.money_ambiguous` | Spec §6 draft (new, normalising): the error when the text has two readings (`en` `1.500` in a USD field). Interpolates `{a}` and `{b}`, the smallest and largest readings, each written with the ISO code, no grouping, and the parsing locale's decimal mark (`1,5 KWD`, `1500 KWD`). Translated after approval. |
| 21 | 8 | `rastrillo.ui.money_ambiguous` (over-precise case) | Spec §6 asks copy review to decide whether a choice that is over-precise for its currency (`1,2340 EUR`, `1.5 JPY`) needs its own sentence. If it does, a third key and a rule for when the form uses it; if not, nothing. |
| 22 | 8 | `gallery.field_money.hint` (re-review) | Task 1's draft, "Without the currency sign.", tells the reader not to type a sign the field now accepts (decision 22). The hint under the gallery's `field-money` "With a hint" state needs new advice. |
| 23 | 8 | `rastrillo.ui.money_currency` (context only) | Wording unchanged, but it now appears only on `Strict` fields; its review context in Task 1 does not say so. Confirm it reads right where only an import-style field shows it. |
| 24 | 21 | `gen.template.contract_money_seed` | The generated form template's data-contract line for `Money` (Appendix C.6 `gen.template.contract_money`) predates `MoneyInput.Seed`; the line must also say the seed is the text the server rendered, sent back to recognise an unchanged edit. |
| 25 | 29 | `changelog.money.normalising` | Spec §8's new closing paragraph of the changelog draft: money fields accept amounts as people type them, still refuse another currency, two-way amounts and extra decimals, and `Strict: true` keeps the old format. Skip if Task 1's review of §8 already approved it. |
