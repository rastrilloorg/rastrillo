# Multi-currency money, language packs, and one template set for every language

Status: **implemented** on the `money-currencies` branch, from the plan
`docs/superpowers/plans/2026-10-10-multi-currency-money.md`. The
sections below are refreshed to describe what shipped, and the
prototypes they cite are marked historical. Before that, Paul approved
it to proceed, with §10 rewritten at his direction: no template set per
language (decision 21).
- Astra's final confirmation (of `cb1ba585`, before the §10 rewrite)
  found every finding from her five reviews resolved, and no new blocker
  or major defect: "Ready to write the implementation plan."
- Her review of the rewritten §10 (round eight, 12 findings, "not ready
  for planning") and her confirmation of it (round nine: 10 resolved,
  two partial, one new) are both answered. Each finding has a stated
  resolution and a prototype test with a negative fixture or mutation
  check. The round-nine fixes have not been re-reviewed.
- Paul then amended §5 (decision 22): money input is normalised by
  default, and `Field.Strict` keeps today's strict reading. Astra
  reviewed the amendment ("not ready", seven findings); Paul ruled on
  each, and Paul added decision 23. Astra confirmed all seven resolved
  and found one gap, the seed lost on a failure re-render; its fix
  (§5.3) has not been re-reviewed.
- No decision is outstanding. The copy drafts in §6, §8 and §9 still
  need the copy review, as plan tasks.

This is round five of the document:
- **Round one** was the first draft.
- **Round two** answered Astra's first review (15 findings).
- **Round three** answered her second (seven findings), and added one
  locale-data pack per language and the Tito template lesson.
- **Round four** answered her third (10 findings): new apps start with
  one locale, and packs live at `locales/<lang>`.
- **Round five** answered her fourth (eight findings), and recorded
  Paul's decisions to rename the event-log payload column only in apps
  whose events carry money, and to keep the probe as the
  `RASTRILLO_CHECK_CONFIG=1` environment variable.
- **This revision** closes the three gaps her confirmation round left:
  the probe before `Run`'s sidecar dispatch, canonical case by subtag
  role, and replaying history from before a money field existed.

Decisions marked **(Paul)** are his. Everything else is a recommendation
backed by the two harnesses committed beside this spec:
- `hack/moneycldr`: the CLDR and ISO 4217 generator and the ICU oracle,
  run with `make money-cldr` (Appendix B);
- `hack/templatemem`: the template measurements, run with
  `cd hack/templatemem && go run .`, plus the shared-set `ui.Pages`
  prototype (`pages/`, tested under `-race`), the template codemod
  (`codemod/`, `cmd/migrate-templates`), the probe's entry points
  (`entry/`) and the upgrade tripwire probe (`upgradeprobe/`).

**Historical paths.** `hack/templatemem`, `hack/moneycldr/proto` and
`hack/moneycldr/replay` no longer exist: each prototype was promoted
into the shipped packages and then deleted. Appendix B names where each
went. This spec still cites them as the evidence it was written from;
where the shipped code measures or behaves differently, the section
says so.

Citations are this repo at `58718d89` unless they name a harness file.

## Why

"Why is it USD only? Surely it should be every currency?" (Paul)

Today:
- `form.FormatCents` (form/money.go:28) hard-codes `$` and does not group
  digits: `$1284.50`.
- `form.ParseCents` (form/money.go:59) accepts dot-decimal input with at
  most two places.
- `form.Money` / `p.Cents` (form/fields.go:24, 217) store `int64` cents
  with no currency.
- The generator's `money` kind (manifest.go:33,
  internal/generate/store.go:171, actions.go:292) writes one `INTEGER`
  column and displays it through `FormatCents`.
- The gallery says "Rastrillo only knows how to write US dollars"
  (internal/designsystem/formats.go:86).

`money/` (a nested module, stdlib only, pinned by Tito) knows each
currency's minor units. `ui` formats plain numbers per locale through
`golang.org/x/text` (ui/number.go).

## Decisions (Paul)

1. **The currency is stored with each amount**: an integer count of the
   currency's minor unit plus its ISO 4217 code. Generated CRUD stores a
   currency column beside each money column.
2. **Formatting happens on the server** from CLDR data, in the reader's
   locale. The currency always comes from the data, never from the
   locale. Browser `Intl` is at most a progressive enhancement.
3. **One template function for money**: `{{money $L .Amount .Currency}}`,
   taking the request's localizer first like every locale function
   (§10; the original form was `{{money .Amount .Currency}}`).
4. **`form.Money` takes a currency** and accepts that currency's decimals.
   `FormatCents` and friends keep working.
5. **Locale data is opt-in, one pack per base language.** A pack holds
   that language's CLDR formatting data for all its modern regional
   variants: the `de` pack covers `de`, `de-AT`, `de-CH`, `de-LI` and
   the rest. Packs are shaped to carry the framework's translated strings
   and `{{number}}`/date data later. Packs live at `locales/<lang>`.
   An app declares the locale codes it offers; the packs it imports are
   derived from those codes (§3). `rastrillo new` and
   `rastrillo generate` write blank imports for exactly those packs.
   **New apps start with one locale**, the one picked, English by
   default, with no `/xx/` prefixes and no switcher. Adding a language
   is one line in `manifest/app.toml`. An "all modern" import stays
   available. Expanding the framework's translations to more languages
   is separate future work (§13).
6. **Follow CLDR for symbol placement**, not ICU.
7. **`{{number}}` moves onto the new data later, separately.** Until
   then it keeps its current float, `uint64`, integer-string and
   precision behaviour.
8. **The gallery's Portuguese is retagged `pt-PT`**, and existing `pt`
   catalog keys and URLs keep working (§9).
9. **No currency picker in v1.** Changing a record's currency is a
   separate, explicit operation (§7).
10. **The default currency is per resource**, set in the manifest. The
    legacy backfill currency is a separate setting.
11. **`FormatCents` gets `// Deprecated:` only after** a documented
    migration path and at least one release that ships it.
12. **Negative amounts**: the parser reads signs; each field states
    whether it allows them.
13. **Storage-scale digits are always shown.** A nonzero minor unit is
    never rounded away.
14. **No Tito extraction now.** Locale data stays out of `money/`.
15. **`Options.DefaultLocale` alone sets the formatting locale**, without
    turning on locale routing.
16. **The hidden parsing-locale input stays.** It is preserved with the
    echoed text and its shape is validated.
17. **v1 requires a stop-start upgrade and forbids rollback** to
    currency-unaware writers for apps that store money; their rollback
    is a backup restore (round-two finding 1; §8).
18. **A form carries the currency it displayed**, and the POST rejects a
    mismatch (round-two finding 2; §5). That value is only a consistency
    check; it never decides the currency.
19. **The event-log payload column is renamed only in apps whose events
    carry money.** Whether they do is declared, never guessed from data.
    eventlog detects which column it has at startup, and apps without
    money keep `payload` and upgrade normally (§8).
20. **The configuration probe is the environment variable
    `RASTRILLO_CHECK_CONFIG=1`**, not a flag (§3).
21. **No template set per language, and none per request.** The base
    (partials and layout) is parsed once at boot, and each page is one
    boot-time clone of it with the page's own definitions. Every
    language and request executes those. Locale-dependent output is
    resolved per execution from the localizer in the template data
    (`{{T $L "key"}}`, `{{money $L .Price .PriceCurrency}}`), dispatch is
    strict in production, and request values come from the data too
    (§10).
22. **Numeric input is normalised by default.** A money field accepts
    input with exactly one plausible reading: white space and bidi
    marks, its own currency in any CLDR spelling on either side (a
    symbol it shares with other currencies included, with the ISO code
    shown beside the input), grouping in valid positions (Indian
    grouping in every locale), the other decimal mark where that is
    unambiguous, and a sign or parentheses (then the field's negative
    policy). It refuses another currency's explicit spelling, input
    whose readings disagree (an over-precise reading counts), a nonzero
    digit beyond the exponent, malformed separators, and anything else.
    Nothing is rounded. An unchanged edit-form seed is recognised by the
    form, not guessed by the parser. `Field.Strict` turns it off and
    gives today's strict errors (§5; Paul's rulings on Astra's review of
    §5.1).
23. **The page's own group mark wins.** When the input's one separator
    has three digits after it and the page's locale uses that separator
    for grouping, in a valid position, the grouping reading is taken:
    `en` `1,000` USD is 1000, `de` `1.500` EUR is 1500, `ja` `￥1,285` is
    1285, `en` `1,500` KWD is 1500. Only the other direction stays
    ambiguous: the separator is not the page's group mark and a group
    reading also exists (`en` `1.500` for KWD or USD). The accepted
    cost: a decimal-comma amount pasted into an English page, German
    `1,500` meaning 1.5 KWD, is read as 1500 (§5.1).

## What the evidence says

All figures come from one run of `make money-cldr` (35 s; Node v24.19.0,
ICU 78.3, CLDR 48.0) and one run of `hack/templatemem` (historical; see
Appendix B), except the pack sizes, which were measured again on the
shipped packs. The round-two
document's oracle numbers are superseded: Astra showed that its grid
mixed categories and that some of its explanations were accepted by
locale name without being checked.

**Inputs.** 1,574 files are pinned by SHA-256 in
`hack/moneycldr/sources/inputs.sha256`: 1,539 cldr-json 48.0.0 files and
35 archived editions of ISO 4217 list one (2013-04 to 2026-09). The CLDR
bytes are identical to round two's. Regeneration is deterministic, and
`make money-cldr` fails if the committed output differs from a fresh
run.

**Coverage.** CLDR's modern set is 429 locales (plus root) in 101
language packs. The 12 gallery languages' packs hold 221 of those
locales; a new app links one pack.

**Oracle.** The grid has 698,436 rows. Each row is checked, and the
oracle must answer exactly those rows, in order, and end with a
completion line. A truncated run, a run without the completion line, a
reordered row and an injected formatting defect all fail; each was
tried.

| Category | Rows | Equal to ICU | Differ (all explained) | Unexplained | Round-trip failures |
|---|---|---|---|---|---|
| shipped: every linked locale | 671,814 | 647,697 | 22,551 | 0 | 0 |
| ordinary: tags that are not CLDR ids | 26,622 | 23,490 | 3,132 | 0 | 0 |
| the 13 gallery locale ids | 20,358 | **20,358** | 0 | 0 | 0 |

The shipped row includes 1,566 rows for `ht`, which ICU has no data for
and which are therefore not compared. The "ordinary" category is now
emitted once per tag: tags that are also CLDR ids (such as `en-IE`)
count as shipped.

**Every difference is explained by a check run on that row** (Appendix
A.2):
- 20,415 rows: ICU reports the locale but formats with a parent's or the
  English hub's data. This covers 23 `en-XX` locales, and each row
  equals our output for that parent.
- 3,132 rows: `uz-AF` and `pa-PK` need Arabic-script data that is not in
  the modern set. The harness runs with no app default, so they format
  with root; in an app they would format in its default locale (§3).
- 2,136 rows: three narrow, executable exemptions for fr-CH, pt-CV and
  sr-Cyrl-ME. Each must turn ICU's string into exactly ours.

**Parsing an independent formatter's output.** On rows where ICU's
string differs from ours, our parser reads it directly 6,353 times. It
reads it under the conventions the explanation names 16,300 times. For
3,030 rows no shipped locale has the conventions, so nothing can read
them. The parse is unexplained on 0 rows.

**Round trip.** `Parse(Format(x)) == x` and `Parse(InputValue(x)) == x`
hold on all 698,436 rows.

**Side-flip patterns: Paul's decision has no effect on shipped data
today.** All 22 locales whose `alphaNextToNumber` pattern moves the
symbol to the other side are outside the modern set. The formatter
still follows CLDR, so the decision takes effect if one ever ships.

**x/text, re-measured.** 0 of 14,028 gallery-locale rows match ICU, and
`currency.ParseISO` rejects 7 storage codes (MRU, SLE, VED, VES, XAD,
XCG, ZWG).

**Missing ICU results are gated** (round-three finding 8). The tags ICU
is expected not to format are pinned; today that is only `ht`. An empty
ICU result on any other row fails the run, and so does an ICU result for
a pinned tag. Exceptions inside the oracle are reported, never turned
into empty answers. A run with every ICU column blanked reports 696,870
missing results and fails.

**Pack sizes, measured by `cmd/packsize`** on the shipped
`locales/<lang>` packages, after they gained CLDR's narrow symbols for
normalisation (§5.1). Measured for Task 29 with
`cd hack/moneycldr && GOFLAGS=-mod=mod CGO_ENABLED=0 go run ./cmd/packsize`
(go1.26.5, linux/amd64); two runs gave identical figures. Source sizes
are exact, from `testdata/packs.tsv`. The baseline is a program that
formats one amount with only the core linked: 3,250,050 bytes.
- The core: 33.7 KB of generated source (`locales/core_gen.go`).
- One pack, `en` (a new app's default): 10.0 KB of source, adding
  **55.4 KB**.
- The 12 gallery languages' packs: 30.4 KB of source, adding 134.6 KB.
- Every pack (all 429 locales): 115 KB of source, adding **485 KB**.

One pack costs about 5 to 10 KB for a language with up to a dozen
locales, 22 KB for `es` and `ar`, and 55 KB for `en` (124 locales).
`ga` and `ja` measure only about 1.3 KB, while `vi`, also one locale,
measures 5.4 KB: single-pack deltas this small are not proportional to
the data, so plan with the combined rows. In the prototype, the narrow symbols added
8 KB to `en` and 54 KB to every pack together. Appendix A.4 has the
table.

**Normalisation (§5), in `proto/normalize_test.go` and
`proto/field_test.go`.** What the tests show, and what they cannot:
- **Hand-written expectations** carry the claims about intent: 130
  cases across en, de, de-CH, fr, hi, ar and ja for JPY, 2-decimal
  currencies and KWD, covering competing readings, malformed separator
  sequences (`12,.50`, `1,,234`, `12'.50`), signs and parentheses in
  every combination, junk and precision; 44 shared-symbol cases, every
  family in §5.1 with CAD and AUD among them; 17 field-level cases with
  `Strict` set and unset on the same posts, through the field's whole
  POST path; and 15 seed cases, a forged seed among them, plus two
submissions around an error on another field.
- **The stated ambiguity shape** is checked exactly: over 33,579
  generated inputs in 14 locales and 9 currencies, every
  digits-and-separators input is refused as ambiguous if and only if it
  has the shape §5.1 states, decision 23 included.
- **Consistency with strict reading, not proof of intent:** wherever
  strict accepts, normalising returns the same value (24,935 inputs) or
  refuses the input as ambiguous with strict's value among the readings
  (374). It never stores a different amount than today's parser.
  178,701 inputs only normalising accepts; whether each is what the
  person meant rests on the hand-written cases above.
- **Redisplay:** no stored value's display or seed is read back as a
  different single amount. Over every shipped locale and storage code,
  8,484 of 523,740 displays and 8,484 of 523,740 seeds are refused as
  ambiguous, every one in the decimal direction (a test asserts it):
  the 3-decimal currencies' amounts under 1000, such as `0.001` and
  `1.500` under `en`. Before decision 23 it was 21,660 in all. An
  unchanged seed still saves through the seed check.
- **The message's choices** are read under every convention by ten
  reader locales, and each has exactly one reading, its own.
- Mutation checks: accepting the first reading, letting an over-precise
  reading drop out, accepting a decimal mark after a group mark,
  dropping Indian grouping, matching the shortest spelling first,
  dropping the choice's extra zero, trusting the seed, ignoring
  `Strict`, and dropping decision 23's group precedence each fail at
  least one test.

**Templates (§10)**, measured on the real structure (the base parsed
once, one clone per page at boot):
- Templates: 2.0-2.2 MB for the scaffold's three pages plus a
  representative form page, the same with 1 or 12 offered locales.
- Per offered locale: 1.4 KB of localizer, plus about 4.4 KB of
  framework catalog.
- Per request: 29 µs, 9.1 KB and 316 allocations for the scaffold
  overview page, and 142 µs and 50 KB for the form page.
- Today's per-request clone costs 440 µs, 516 KB and 9,797 allocations,
  and the one-tree baseline 23 µs, 7.5 KB and 267 allocations.
- Strict dispatch, output isolation across two concurrent languages, and
  the codemod's validation are each tested and mutation-checked.
- The codemod changes 255 locale calls and 102 `dict` calls in 53 files,
  lists 11 sites for review, and its Go preflight reports the 3
  examples' own `T` registrations.

**Upgrade tripwires (§8)**, through the real `migrate.Apply`:
- The money-only, post-adoption eventlog migration renames `payload` on
  fresh, ledger-backed and pre-ledger databases, and the pre-ledger case
  is adopted. Without the post-adoption marker it is refused.
- After the rename, today's eventlog code fails on read, append and
  ingest. An app without money is untouched.
- Upgrade, backup restore, upgrade again: the ledger always agrees with
  the column.
- Removing a legacy money column makes old generated queries fail. The
  prototype renamed it; what shipped adds, copies and drops (§7).
- The money migration took 19.5 ms on 200,000 events in the prototype.
  The shipped migration takes about half a second (§8); the prototype
  figure is superseded.

**The probe** (§3): a pure function with tests for the exact output
line and exit codes, run at the top of `Resolve`, before the database is
opened.

## Design

### 1. Stored shape and the storage set

Each amount is two plain fields and two columns:

```go
type TicketType struct {
	PriceMinor    int64  // count of PriceCurrency's minor unit
	PriceCurrency string // ISO 4217 code, upper case, from the storage set
}
```

```sql
-- a column that existed before the upgrade, replaced by it (§7, §8):
price_minor    INTEGER,          -- added, filled from price, which is dropped
price_currency TEXT,
-- a money field added to an existing table after the upgrade:
fee_minor      INTEGER,          -- NULL: no amount yet
fee_currency   TEXT              -- NULL exactly when fee_minor is NULL
```

- **The one rule: a currency is present if and only if an amount is
  present** (confirmation round, gap 3). An empty field has neither,
  and every amount has its currency.
- **There is no schema default currency.** Before the upgrade's
  legacy step, a NULL currency beside an amount marks a legacy row, and
  only the checked migration in §7 turns it into a code.
  - "Legacy" is decided by the column, not the row: a column still
    named `<f>` is legacy. Once the upgrade drops `<f>`, a NULL
    currency is only ever an empty field, paired with a NULL amount.
- **A money field added to an existing table** is added as
  `<f>_minor INTEGER` with no `NOT NULL` and no default. Its existing
  rows get NULL, an empty field, not an amount.
  - This is where the rule would be unsafe if applied to the
    round-four definition. `NOT NULL DEFAULT 0` would give every
    existing row an amount of 0 with no currency, which the rule must
    reject, and which the round-two read path would have mistaken for a
    legacy row. So added columns are nullable, and the round-two "read
    a NULL row as legacy" path is withdrawn (§7).
- **Model fields for money are pointers** (`*int64`, `*string`), nil
  for an empty field. Display writes `""` for an empty field. An edit
  form shows an empty field with the resource's default currency, and
  that is the currency it echoes.
- **The legacy money column is replaced**: `<f>_minor` and
  `<f>_currency` are added, filled, and `<f>` is dropped. Dropping `<f>`
  is the tripwire that makes an old binary fail loudly, not corrupt data
  (§8). This spec first said rename; a rename keeps the old column's
  `NOT NULL DEFAULT 0`, so an empty optional field would be unwritable
  on upgraded tables only, and fresh and upgraded tables would differ.

**The storage rule** (round-two finding 7) is one rule, independent of
CLDR's display digits:
- A code may be stored when ISO 4217 gives it a numeric minor unit, in
  the newest pinned edition that lists it, and that number equals
  `money.MinorUnits(code)`.
- The stored exponent is always `money.MinorUnits(code)`.
- A disagreement is admitted only as a reviewed deviation, written in
  the generator with its reason. There is one, MGA: money stores whole
  ariary, ISO says 2, and the subunit is a fifth, so no decimal scale is
  exact.
- When two pinned editions disagree, the code is excluded and the
  disagreement is recorded. CLF is one: some editions say 0, others 4.

Archived editions are needed because the current ISO list drops a code
when it is withdrawn, and the historic list (list three) has no minor
units. So a withdrawn code is verified against the last pinned edition
that still listed it. `testdata/storage.tsv` records the edition used
for every code.

The result:

| Status | Count | Examples |
|---|---|---|
| eligible: stored, and allowed as a new default | 153 | USD, EUR, JPY, KWD, MGA (deviation), SLE, VES, XCG, ZWG |
| stored only | 21 | SLL, HRK, BGN, ANG, CUC, VEF, MRO, STD, ZWL, LTL, LVL; fund codes CHE, CHW, BOV, COU, MXV, USN, USS |
| excluded: ISO and money disagree | 4 | BYR, CLF, UYI, UYW |
| excluded: no minor unit (fund, metal or test code) | 14 | XAU, XDR, XXX |
| excluded: no pinned edition lists it | 116 | DEM, FRF, ITL, ESP, ZMK, TRL (withdrawn before 2013) |

- **Eligible** means stored, listed in the newest ISO edition, and legal
  tender in CLDR somewhere with no end date.
- **Historical amounts are real.** An app may hold SLL amounts in a
  column it denominated itself. The round-two claim that "no app holds
  SLL amounts" is withdrawn. SLL is now stored with exponent 2, verified
  against the 2023-02 edition, so `[legacy] currency = "SLL"` works.
- **The storage set only grows.** The implementation keeps it as a Go
  literal, with a test that checks it against `money.MinorUnits` and
  against a second hand-edited copy. Removing a code or changing an
  exponent fails the build.
  - **Adding a code** takes a pinned ISO edition that lists it with
    `money`'s exponent. A pre-2013 code needs an older pinned edition,
    or a reviewed deviation entry citing its source.
  - **Losing eligibility** (BGN ended in 2026-01) never removes a code.
    Its records keep their scale and stay editable.
- `currency.Rescale` (§2) converts imports from another scale, checked.

**Hand-written GORM apps** add a `<Field>Currency *string` column and
migrate legacy rows with `currency.Rescale` (§7). They never bulk-assign
a currency.

### 2. Packages and where the data lives

| Package | Module | Role |
|---|---|---|
| `money` | nested, stdlib only, Tito pins it | Minor units and arithmetic. **No change.** |
| `currency` | root | The single exported entry point: `Normalize`, `EligibleDefault`, `Exponent`, `Resolve`, `Format`, `InputValue`, `Parse`, `Clean`, `Rescale`, `AmountFromAny`. |
| `locales` | root | The locale-data core. It holds the registry packs add themselves to, CLDR's root locale (`und`), `CheckConfig`, `PacksFor`, and the lookup tables every resolution needs: the index of every shipped locale and its pack, the CLDR fallback parents, aliases, likely scripts, all digit sets, and the storage set. About 32 KB of generated source. |
| `locales/<lang>` | root | **One pack per base language** (`locales/de`, `locales/zh`, …). It registers its locales in `init`. |
| `locales/modern` | root | Blank-imports every pack. |
| `form`, `ui` | root | Call `currency`. |

**Why `locales/`** (decided by Paul). The root `locales/` directory already holds the
framework's translated catalogs (`locales/*.toml`, embedded by
basecatalog.go:22). A pack at `locales/<lang>` sits beside its
language's catalog, and can later embed it and add `{{number}}` and date
sections to the same per-locale record. The record already groups its
plain-number fields so a later reader can use them. The future
translation workstream then needs no second registry and no second
import per language.

**Why the root module, not a nested one.** A pack's records must match
the core's types and its lookup tables exactly. A nested module per pack
would need tags released in lockstep and a `require` per language, and
would buy nothing at run time: Go already links only imported packages,
so an app pays nothing for a pack it does not import. The cost is module
download size, about 140 KB of source for all packs.

`currency` is a separate package from `locales` because the locale data
serves numbers and dates too, while the money API does not.

### 3. Locale data, declaration, resolution and fallback

**What a pack holds, per locale.** It stores what differs from the
locale's *data parent*, and a self-check rebuilds every locale exactly:
- the currency decimal and group marks, the ordinary decimal mark, the
  minus sign and the digits;
- `minimumGroupingDigits`;
- the standard and `alphaNextToNumber` currency patterns;
- the currency-spacing insert;
- the symbols and the per-currency overrides.

**Two parent maps, kept apart** (round-three finding 1):

| Map | Where | Used for | Crosses packs? |
|---|---|---|---|
| Data parent (`Locale.Parent`) | each pack's records | rebuilding a record from its diff | **Never**: a locale whose CLDR parent is in another pack (hi-Latn from en-IN, ht from fr-HT, nb and nn from no) is stored whole, relative to root, so no pack needs another to rebuild. |
| Fallback parent (`locales.Parents`) | the core | walking to a linked locale when a pack is missing | **Yes**: it is CLDR's own parent, so hi-Latn without the hi pack falls to a linked en-IN, ht to fr-HT, nb and nn to no. |

`proto/crosspacktest` links only the en, fr and no packs, and checks
each of those four walks, including that the fallback formats exactly as
the parent does.

**Declaring what an app offers** (round-three finding 2;
decision 5 as revised). The app declares **offered locale codes** in
`manifest/app.toml`, not packs:

```toml
locales = ["en"]          # what rastrillo new writes, or the language picked
default_locale = "en"
```

- **Offered codes are identities.** They are what routing, the
  `rastrillo_locale` cookie, the catalog files (`locales/<code>.toml`)
  and the template cache key use: `zh-Hans`, `pt-PT`, `en`. They are
  never rewritten into pack names.
- **Packs are derived from them, and only ever derived.** `generate`
  computes the deduplicated set of base languages the offered codes
  resolve into, and writes those blank imports to
  `gen/locales/packs.go`. For example, `["en", "zh-Hans", "zh-Hant",
  "pt-PT", "yue"]` gives `en`, `pt`, `yue` and `zh`; that is
  `proto.PacksFor`, tested. A code that is malformed, or that no modern
  locale formats, fails `generate`.
- **New apps start with one locale** (Paul): `rastrillo new` writes the
  one picked, English by default, with no `/xx/` prefixes and no
  switcher. `main.go` wires `Options.Locales` only when more than one
  locale is offered, so a one-locale app sets just `DefaultLocale`
  (decision 15). A new app links one pack; for English that adds
  46.8 KB to its binary.
- **Adding a language is one line.** Doc line for localization.md: "To
  offer another language, add its code to `locales` in
  `manifest/app.toml` and run `rastrillo generate`. That links its
  formatting pack. Then add `locales/<code>.toml` with every
  `rastrillo.ui.*` key translated." With two or more locales, prefixes
  and the switcher turn on.
- `app.toml` is a reserved name in `manifest/`, and a resource named
  `app` is refused.

**Resolving a tag** (unchanged from round two except steps 6 and 7):
1. If the tag is exactly a shipped locale id, use it.
2. **Validate RFC 5646 grammar.** Malformed tags, such as `de--CH`,
   `de-CH-!!!`, `de-`, `de-CH-u` and `x-private`, never match a locale.
3. **Aliases apply their whole replacement**, field by field, wherever
   the tag left a field empty: `swc` becomes `sw-CD`.
4. **The likely script** comes from the language-region pair, else the
   language.
5. **Candidates are checked against every shipped locale, linked or
   not.** The first match is the *wanted* locale.
6. **If the wanted locale's pack is not linked, walk the fallback
   parents** to the nearest linked locale (the second map above), and
   record the missing pack.
7. **Past every linked parent, use the app's effective default locale**
   (round-three finding 10). The page's words already fall back through
   the catalogs to the default locale, so the amount is written in the
   same conventions as the text around it. The formatting locale used is
   `Config.DefaultFormat`, installed by `SetDefault` after `CheckConfig`.
   CLDR root (`und`) is used
   only where there is no app: library use, tests and tools. The round
   two justification for root ("English writes USD, AUD and CAD all as
   `$`") was wrong: English writes `$`, `A$` and `CA$`. That claim is
   withdrawn.

**Validating the configuration at boot, in the serving binary**
(round-three finding 3; round-four findings 5 and 6). `Serve` calls
`locales.CheckConfig(DefaultLocale, Locales)` before it listens, and
refuses to start on any problem.

1. **Every identity is checked strictly first**, with
   `CanonicalIdentity`, before the forgiving request resolver sees it.
   - Refused: whitespace anywhere, underscores, anything that is not
     RFC 5646 grammar, and non-canonical case. Case is decided **by each
     subtag's grammatical role, never by its length** (confirmation
     round, gap 2): `nu` in `en-u-nu-latn` is an extension subtag, not
     a region, so it stays lower. Canonical case is
     language lower, script title, region upper, the rest lower.
   - The error names the canonical form, for example: `"en_US" is not in
     canonical form; write "en-US"`, `"zh-hant" … write "zh-Hant"`,
     `" en " contains white space; write "en"`.
   - **A duplicate offered code is refused.**
   - Configuration is never repaired, because an identity that only
     works after repair would be routed, stored and cached under its
     unrepaired spelling. Request tags (`?lang`, `Accept-Language`, a
     stored row) stay forgiving, through `Resolve`.
2. **Then each identity is resolved**, and must be recognised and linked
   with no fallback. Root, or a parent standing in for a missing pack,
   is never accepted, and the error names the import to add.
3. **The effective default** is `DefaultLocale`, else `Locales[0]`, else
   `en`. It must be one of the offered codes when any are offered. So an
   app with no manifest, or with only `DefaultLocale`, is checked the
   same way.
4. **What it returns keeps identities and formatting locales apart:**

   ```go
   type Config struct {
       Default       string            // the declared effective default: an offered identity ("zh-TW")
       DefaultFormat string            // the linked locale that formats it ("zh-Hant")
       Offered       []string          // as declared, canonical
       Formats       map[string]string // identity -> formatting locale
       Packs         []string          // derived, deduplicated
       LinkedPacks   []string          // what this binary links
   }
   ```

   Catalogs, routing and `ui.Pages` (its localizers) take `Default`
   and `Offered`. Only
   money and number formatting reads `Formats`, and `SetDefault` (the
   fallback in step 7 of resolution) receives `DefaultFormat`.

   The harness test `pages/integration_test.go` feeds
   `CheckConfig("zh-TW", ["zh-TW", "en"])` into `pages.New`. The page
   renders the zh-TW catalog's string with TWD as `$1,284.50` (zh-Hant
   conventions), and the en page as `NT$1,284.50`.

`proto.CheckConfig` and `CanonicalIdentity` implement this, and the
tests cover role-based case (`en-u-nu-latn`, `de-CH-1996`, `zh-Hant-TW`,
`en-x-private` and `sr-Latn-RS-u-ca-gregory` accepted as written;
`EN-u-NU-latn`, `de-ch-1996`, `zh-hant-tw` and `en-X-Private` refused,
naming the canonical form; the private-use-only `x-private` refused as
having no language), nothing set, `DefaultLocale` only, regional identities,
`zh-TW`/`en-US` (identity kept, format separate), a default outside the
offered set, a missing pack, unknown and malformed codes, whitespace,
underscores, case and duplicates.

Upgrade consequence: every app that links `ui` links the locales core,
so every app needs at least one pack, which is its default locale's.
Generated apps get it from `generate`. Hand-written apps get the boot
error naming the import.

**The configuration probe** (round-three finding 9; round-four finding
1; `RASTRILLO_CHECK_CONFIG`, an environment variable, decided by Paul).
Only the app binary knows which packs it links and what `Options` it
computes, so the probe runs in the app binary, but before it touches
anything.

- **Where it runs** (confirmation round, gap 1). The probe is the
  **first statement of every entry point that can start app work**:
  - **`rastrillo.Run`**, before its `sidecar run` dispatch. Today
    `Run` dispatches `sidecar run` before anything else (run.go:36-40),
    and that path reaches the app's `Options.Sidecar` work
    (sidecar.go:64) without passing `Resolve` or `Serve`. The round-four
    claim that "`Run` calls `Resolve` first" was wrong and is withdrawn.
  - **`rastrillo.Resolve`**, which the scaffold's `main` calls before
    `db.Open` (cmd/rastrillo/new.go:373-379).
  - **`rastrillo.ProbeConfig(opts)`**, the first line of `main` for
    hand-written apps that call neither.

  So the probe runs before the database is opened, before any migration,
  before `App` builds the mux, and before any background or sidecar
  work. The scaffold puts `Locales` and `DefaultLocale` in the
  `Options` literal it passes to `Resolve`.
- **Every entry point that touches the database guards.** That is
  `Serve`, `Handler`, `OpenDB` and `db.Open`. Each checks the variable
  as its first statement. If it is set there, the probe did not run
  first, so it exits 2 without opening anything, and its message names
  `ProbeConfig`.
- **Prototype (historical; shipped in `run.go` and `probe.go`, tested
  in `probe_test.go`):** `hack/templatemem/entry` mirrors `Run`'s dispatch
  (its `isSidecarInvocation` is copied verbatim) with the probe first.
  - Tests run a plain invocation, `serve`, `-db x.db` and `sidecar run`
    with the variable set, and with sidecar and serve callbacks that fail
    the test if they ever run.
  - Without the variable, the dispatch is unchanged.
  - The guard refuses before opening.
  - A mutation that restores today's order (sidecar dispatch first) is
    caught on `sidecar run`.
- **What it prints.** One JSON object on stdout, always with these
  fields: `rastrillo_check_config` (the report format, 1), `ok`,
  `default`, `default_format`, `offered`, `formats`, `packs_required`,
  `packs_linked` and `problems`. Plus one line on stderr: "checked the
  configuration and exited without serving; unset it to serve".
- **How it exits.** It **always exits and never serves**: 0 for an
  accepted configuration, 1 for problems, and 2 for any value other than
  `1`.
- **Production behaviour, and why.** An inherited
  `RASTRILLO_CHECK_CONFIG=1` in production stops the instance at once,
  visibly, rather than serving. The alternative, warn and serve, would
  let doctor start a real server against a live database, which is the
  side effect the probe exists to avoid. An inherited variable is a
  misconfiguration, and a process that exits with a named reason shows
  up in the supervisor and the logs. Only the exact value `1` runs the
  probe, so a stray `RASTRILLO_CHECK_CONFIG=0` is an error, not a silent
  check.
- **Doctor.** Plain `rastrillo doctor` never builds or runs the app
  (Astra's final review, finding 2; Paul's ruling: an opt-in flag).
  The app's own code runs before the probe can stop it: an `init()`,
  or a `main` that writes files or opens its database before reaching
  the framework's entry points, does so in the app's directory against
  its real data. Doctor's static checks claim only what files show:
  that `manifest/app.toml` and `gen/locales/packs.go` agree, and that
  each offered code has a catalog. Without the flag, only they feed
  exit 7. `rastrillo doctor --probe` builds `./cmd/<app>` in the app's
  own module and runs that binary, with the variable set for that
  child only. It does not use `go run`: under `go run` a timeout kills
  `go` and orphans the app, and the exit code is `go run`'s own 1
  rather than the probe's. It runs nothing unless the command links the
  probe (`internal/checkconfig`), because a binary built against an
  older rastrillo ignores the variable and starts for real. The probe
  itself opens nothing, so `--probe` is safe against a live database
  only for an app whose startup reaches the framework's entry points
  before it touches anything.
- **The generated test** `gen/locales/packs_test.go` checks that
  `gen/locales` imports the packs `app.toml` derives. The probe is what
  proves `main` links them.
- `locales.Probe` is the logic (prototyped as `proto.Probe`, since
  deleted), and the tests pin the exact output line, the exit codes and
  the unset case.

**An unlinked pack at request time is never silent.**
- `Resolve` returns `{ID, Wanted, MissingPack, Malformed, UsedDefault}`.
- `currency.Format` and `{{money $L …}}` log a warning once per
  missing pack per process.
- The currency never changes; only the conventions fall back. A request
  can still ask for a missing pack, through a stored preference or
  explicit app code. The middleware only ever yields offered codes, and
  boot has proved those linked.

**Formatting algorithm.** Unchanged:
- the checked code and its frozen exponent;
- the per-currency overrides;
- `alphaNextToNumber`, following CLDR even when that moves the symbol
  (decision 6);
- the negative form;
- the `money.Decimal` magnitude;
- grouping that honours `minimumGroupingDigits`;
- the locale's digits and currency spacing.

### 4. Display

`currency.Format(locale, minor, code) (string, error)` is the Go form.
The template form is `{{money $L amount code}}` (§10's contract): the
request's `*Localizer` first, then the amount, then the ISO code. There
is no form without the localizer.
- An unsigned value above `MaxInt64`, a float, a string or an
  unsupported code is an error, and the error stops the template. So do
  a missing localizer and a missing argument (strict dispatch, §10).
- A nil pointer amount writes `""`.
- Generated actions format in Go, and on an error answer 500 and log the
  row id and the code.

The locale comes from the localizer, `$L`, whose formatting locale is
`CheckConfig`'s `Formats` entry for the request's identity (§3). It
does not come from `ui.WithLocale`. A monolingual app's only localizer
is its `DefaultLocale`'s (decision 15). The output shows the storage exponent's
digits (decision 13), CLDR's negative form, and its bidi marks and
no-break spaces.

### 5. Parsing and the form contract

A money field reads what a person typed in one of two ways (decision
22):
- **Normalising, the default.** It accepts input that has exactly one
  plausible reading and refuses input whose plausible readings disagree
  (§5.1). `Field.Strict` is false.
- **Strict, `Field.Strict: true`.** `currency.Parse`, round two's parser,
  unchanged (§5.2). Use it for a field that must take exactly the
  locale's own format, such as an import column.

Both readings share these rules:
- `Clean` runs first. It removes bidi marks (U+200E, U+200F, U+061C and
  the embedding and isolate controls) and folds every space separator,
  NBSP and NNBSP included, to a space, then trims.
- Digits come from any numbering system a shipped locale uses, linked or
  not.
- No rounding, ever.
- The parser reads signs. Whether a negative is allowed is the field's
  policy, applied afterwards (decision 12): `(5)` in a field without
  `Negative` is `money_negative`, never 5.
- One arithmetic path, through `money.ParseDecimal`.

The prototype was `hack/moneycldr/proto` (historical; deleted): `Parse`
(strict) and `Normalize` in `normalize.go`, and the field's whole POST
path (`Field.Read`: the echo checks, the seed check, the reading
`Strict` selects, the negative policy) in `field.go`. They ship as
`currency.Parse` and `currency.ParseNormalized` (`currency/parse.go`,
`currency/normalize.go`) and `form/moneyfield.go`.

#### 5.1 The normalising reading

1. **Clean.** A visually empty value is blank, and `Required` decides.
2. **Strip.** In any order, at most:
   - one sign: a leading `+`, `-` or U+2212, or a trailing `-` or
     U+2212; or one pair of parentheses around the whole value, meaning
     negative. Two signs, or a sign with parentheses (`(-5)`, `-(5)`),
     is `money_invalid`.
   - one currency spelling at either end. The spellings are every ISO
     code in the storage set, in any case, and every symbol and narrow
     symbol CLDR gives any currency in the parsing locale or in root.
     At each end the **longest** spelling that matches decides (so `US$`
     is never read as `US` plus `$`), and it must stand apart from any
     letter beside it (so `5 dollars` is not `5 doll` plus ARS):
     - a spelling **the field's own currency uses**, in the parsing
       locale or root, is stripped, even when other currencies use it
       too (shared symbols, below);
     - any other spelling, which names another currency explicitly, is
       `money_wrong_currency`: `US$` or `USD` in a CAD field, `A$` or
       `€` in a USD field;
     - a second own spelling (`$5 USD`) is `money_invalid`;
     - a spelling that is one of the locale's own separator marks is a
       separator there, not a currency (pt-CV's `$` decimal).
3. **Characters.** What is left must be digits and separators: `.`,
   `,`, space, `'`, U+2019, U+066B (Arabic decimal), U+066C (Arabic
   thousands), and the locale's own currency marks. Anything else is
   `money_invalid`.
4. **Readings.** The digits and separators are read under each
   convention in the decision table, with each grouping. A reading
   exists when:
   - a group mark follows digits and is followed by digits: `12,.50`,
     `1,234,.50`, `12.,50`, `12'.50` and `1,,234` have no reading;
   - at most one decimal mark appears, after every group mark. It may
     come first (`.50`) or last (`5.`);
   - every group mark is the same character;
   - the groups sit at valid positions: the last group has the primary
     size, earlier groups the secondary size, and the first group 1 up
     to the secondary size.

   Each reading is an exact amount. It is **over-precise** when a
   nonzero digit lies beyond the currency's exponent. Zeros beyond the
   exponent change nothing, so `12.5000` is 12.50 in a USD field.
   Readings with equal amounts are one reading.
5. **Decide.**
   - One reading, not over-precise: accept it.
   - Two or more readings, and the page's locale, with its own grouping,
     reads every separator in the input as its group mark: take that
     reading (decision 23). `en` `1,000` USD is 1000; `de` `1.234` EUR
     is 1234.
   - Otherwise two or more readings, at least one not over-precise:
     `money_ambiguous`. An over-precise reading still competes: that the
     store cannot hold 1.234 USD says nothing about whether the person
     meant it. So `en` `1.234` for USD is ambiguous, not 1234.
   - Only over-precise readings: `money_precision`, `money_precision_0`
     or `money_precision_3`.
   - No reading: `money_decimal_comma` or `money_decimal_point`, for the
     locale's own decimal mark, or `money_invalid` when it is neither.

**The decision table** (step 4):

| Convention | Decimal marks | Group marks |
|---|---|---|
| The locale's own | the currency decimal mark (with per-currency overrides), the ordinary decimal mark, and `.` unless `.` is the group mark | the currency group mark |
| Point | `.` | `,` |
| Comma | `,` | `.` |
| All three, also | U+066B | space, `'`, U+2019, U+066C |

| Grouping | Sizes |
|---|---|
| The locale's | primary and secondary from its currency pattern |
| Thousands | 3 and 3 |
| Indian | 3, then 2 (`1,23,456`), in every locale |

**The ambiguity, exactly.** Space, `'` and U+2019 only ever group and
U+066B only ever marks a decimal, so readings can disagree only over a
`.` or `,`. A second, different separator after it settles it
(`1.284,50`), and so does a repeat (`1.234.567`, two decimal marks
being impossible), and so does a space or apostrophe group beside it
(group marks must match). So input is ambiguous exactly when its only
separator is **one** `.` or `,`, with one to three digits before it and
**exactly three** after it, the two readings differ (they coincide only
for zero, `0,000`), and the page's locale does not group with that
separator (decision 23): `en` `1.500`, `de` `1,500`, `fr` `1.500`. The
readings are the thousands one (1500) and the decimal one (1.5). This
holds for every currency. The separator is then the page's decimal mark
(`en` `.`; `fr` reads `.` as a decimal too) or neither of its marks
(`de-CH` `,`), which stays ambiguous under the same rule.

The page's own displays and seeds are never read back as a different
amount. Displays group with the page's own mark, so they read back
exactly (`￥1,285` is 1285). The only refusals are 3-decimal amounts
under 1000 written with the decimal mark, such as `en` `KWD 1.500`
pasted back; a seed of that shape is kept by the seed check below.

**The accepted cost** (decision 23): input that means the other
direction is read the page's way. German `1,500` meaning 1.5 KWD,
pasted into an English page, is stored as 1500 KWD: 1000 times too
large. The page shows the stored value after the save, which is where
the person can see it.

**Unchanged seeds** are recognised by the form, not the parser
(§5.3), so an edit form saves unchanged even when its seed, such as
`en` `1.500` for 1.5 KWD, is ambiguous text.

**Shared symbols.** A symbol is the field's currency when the field's
own currency uses it in the parsing locale or root, even if other
currencies do too. The input always shows the field's ISO code beside
it (§5.3), so a bare shared symbol can only mean the
currency on the screen. A spelling only another currency uses is
refused. The families, from CLDR 48 root and the locales listed:

| Family | Bare symbol | Shared by (root) | Accepted | Refused |
|---|---|---|---|---|
| Dollar | `$` | 30 currencies, dollars and pesos: USD, CAD, AUD, NZD, HKD, SGD, TWD and others | `$5` in a CAD, AUD or USD field | `US$` in a CAD field; `A$` or `CA$` in a USD field |
| Peso | the same `$` | MXN, ARS, CLP, COP, CUP, DOP, UYU | `$5` and `MX$5` in an MXN field | `US$` or `USD` in an MXN field |
| Yen and yuan | `¥` (ja: `￥`) | JPY, CNY | `¥5` in a JPY or CNY field | `CN¥` in a JPY field; `JP¥` in a CNY field |
| Pound | `£` | GBP, FKP, GIP, SHP, SSP, SYP | `£5` in a GBP or GIP field; `E£5` in an EGP field | `£5` in an EGP field (EGP's spellings are `E£` and `EGP`) |
| Krona and krone | `kr` | SEK, NOK, DKK, ISK | `5 kr` in a SEK, NOK or DKK field | `SEK 5` in a NOK field; `kr` in a EUR field |
| Rupee | `Rs`, `₹` | `Rs`: PKR, LKR, NPR, MUR; `₹`: INR | `Rs 5` in a PKR or LKR field; `₹5` in an INR field | `Rs 5` in an INR field (CLDR gives INR no `Rs`); `₹5` in a PKR field |

Accepting a bare `$` in a CAD field means a USD price pasted with `$`
is stored as CAD. That is the cost of accepting the symbol people type,
and the visible ISO code is what keeps the field honest; `US$`, `USD`
and every other explicit spelling are still refused.

**Examples**, from `proto/normalize_test.go` (amounts in minor units;
the strict column is today's parser on the same input; ambiguity shows
the message's two choices):

| Locale | Input | Currency (exponent) | Normalised | Strict |
|---|---|---|---|---|
| en | `US$ 5` | USD (2) | 500 | `money_currency` |
| en | `5 usd` | USD (2) | 500 | 500 |
| en | `1.284,50` | USD (2) | 128450 | `money_decimal_point` |
| en | `12,50` | USD (2) | 1250 | `money_decimal_point` |
| en | `1,23,456` | USD (2) | 12345600 | `money_decimal_point` |
| en | `12.5000` | USD (2) | 1250 | `money_precision` |
| en | `1,500` | USD (2) | 150000 | 150000 |
| en | `1,000` | USD (2) | 100000 | 100000 |
| en | `1.500` | USD (2) | `money_ambiguous`: `1.5 USD` or `1500 USD` | `money_precision` |
| en | `1.234` | USD (2) | `money_ambiguous`: `1.2340 USD` or `1234 USD` | `money_precision` |
| en | `12.3456` | USD (2) | `money_precision` | `money_precision` |
| en | `12,.50` | USD (2) | `money_decimal_point` | `money_decimal_point` |
| en | `(5.00)` | USD (2) | -500, then field policy | `money_invalid` |
| en | `+5` | USD (2) | 500 | `money_invalid` |
| en | `-(5)` | USD (2) | `money_invalid` | `money_invalid` |
| en | `€5` | USD (2) | `money_wrong_currency` | `money_currency` |
| en | `A$5` | USD (2) | `money_wrong_currency` | `money_currency` |
| en | `5 dollars` | USD (2) | `money_invalid` | `money_currency` |
| en | `$5` | CAD (2) | 500 | `money_currency` |
| en | `US$5` | CAD (2) | `money_wrong_currency` | `money_currency` |
| en | `1,500` | KWD (3) | 1500000 | 1500000 |
| en | `1.500` | KWD (3) | `money_ambiguous`: `1.5 KWD` or `1500 KWD` | 1500 |
| en | `1.5` | KWD (3) | 1500 | 1500 |
| en | `1,500` | JPY (0) | 1500 | 1500 |
| en | `1.500` | JPY (0) | `money_ambiguous`: `1.5 JPY` or `1500 JPY` | `money_precision_0` |
| de | `12.50` | EUR (2) | 1250 | `money_decimal_comma` |
| de | `1,284.50` | EUR (2) | 128450 | `money_decimal_comma` |
| de | `1.234` | EUR (2) | 123400 | 123400 |
| de | `1,234` | EUR (2) | `money_ambiguous`: `1,2340 EUR` or `1234 EUR` | `money_precision` |
| de | `1.500` | EUR (2) | 150000 | 150000 |
| de | `12.,50` | EUR (2) | `money_decimal_comma` | `money_decimal_comma` |
| de | `1.500` | KWD (3) | 1500000 | 1500000 |
| de | `1,500` | KWD (3) | `money_ambiguous`: `1,5 KWD` or `1500 KWD` | 1500 |
| de | `1,5` | KWD (3) | 1500 | 1500 |
| de | `1.500` | JPY (0) | 1500 | 1500 |
| de | `1,500` | JPY (0) | `money_ambiguous`: `1,5 JPY` or `1500 JPY` | `money_precision_0` |
| de | `1,5` | JPY (0) | `money_precision_0` | `money_precision_0` |
| fr | `1 284,50 €` (NNBSP, NBSP) | EUR (2) | 128450 | 128450 |
| fr | `1.284,50` | EUR (2) | 128450 | `money_decimal_comma` |
| fr | `1.500` | EUR (2) | `money_ambiguous`: `1,5 EUR` or `1500 EUR` | `money_precision` |
| fr | `1 500` | KWD (3) | 1500000 | 1500000 |
| de-CH | `1,500` | CHF (2) | `money_ambiguous`: `1.5 CHF` or `1500 CHF` | `money_decimal_point` |
| de-CH | `1'500` | CHF (2) | 150000 | 150000 |
| hi | `1,23,456.50` | INR (2) | 12345650 | 12345650 |
| hi | `123,456.50` | INR (2) | 12345650 | 12345650 |
| hi | `१२३.५०` | INR (2) | 12350 | 12350 |
| hi | `1,500` | KWD (3) | 1500000 | 1500000 |
| hi | `1.500` | KWD (3) | `money_ambiguous`: `1.5 KWD` or `1500 KWD` | 1500 |
| ar | `١٢٫٥٠` | SAR (2) | 1250 | `money_invalid` |
| ar | `١٬٢٨٤٫٥٠ ر.س.` | SAR (2) | 128450 | `money_invalid` |
| ar | `١٫٥٠٠` | KWD (3) | 1500 | `money_invalid` |
| ar | `1,500` | KWD (3) | 1500000 | 1500000 |
| ar | `1.500` | KWD (3) | `money_ambiguous`: `1.5 KWD` or `1500 KWD` | 1500 |
| ar | `١٢٫٥` | JPY (0) | `money_precision_0` | `money_invalid` |
| ja | `￥1285` | JPY (0) | 1285 | 1285 |
| ja | `￥1,285` | JPY (0) | 1285 | 1285 |
| ja | `1.500` | JPY (0) | `money_ambiguous`: `1.5 JPY` or `1500 JPY` | `money_precision_0` |
| ja | `1285円` | JPY (0) | `money_invalid` | `money_currency` |

`ar` formats with Latin digits in CLDR 48, so today's parser refuses the
Arabic decimal mark; normalising reads it. `円` is not a CLDR symbol for
JPY in `ja` (`￥` is), so `1285円` is refused like any other word. Adding
spellings CLDR does not have is not part of this design.

**The choices in `money_ambiguous`** are the smallest and largest
readings, each written so that no reader, under any convention, can
take it for the other:
- the ISO code after the amount;
- no group separators, so the thousands choice is a bare run of digits
  (`1500 KWD`);
- the parsing locale's decimal mark (or its ordinary one where only that
  locale reads the currency mark as a decimal: pt-CV writes `,`, not
  `$`), and the fraction without trailing zeros (`1,5 KWD`);
- when exactly three fraction digits would remain, one zero added
  (`1,2340 EUR`). Three digits after a mark are what a thousands group
  looks like, and four never are.

A test reads each choice under every convention, through ten reader
locales, and requires exactly one reading, its own. An over-precise
choice (`1,2340 EUR`, `1.5 JPY`) is shown so the person sees both
readings; entering it gives the precision error, which is the truth.

#### 5.2 Strict reading

`currency.Parse(locale, s, code)` is round two's parser, unchanged:
- One sign is accepted: leading `-`, after a marker, or trailing. No
  `+` and no parentheses.
- One marker for this currency: its symbol in the parsing locale, or
  its ISO code in any case. Any other letter or currency sign is
  `money_currency`.
- Only the locale's own reading, with the locale's grouping. A group
  mark is accepted only at valid positions, and the error names the
  locale's decimal mark.
- More fraction digits than the exponent, zeros included, is the
  precision error.

#### 5.3 Field options and the text a field shows

`form.Field`, when `Currency != ""`:
- `Required` is checked after `Clean`.
- `Negative` is the field's policy (decision 12).
- `Strict` selects §5.2. It is false by default.
- `LocaleEcho` turns on the hidden-locale contract.

Normalisation is on for every money field with a currency, and every
generated money field has one after the upgrade. `form.ParseCents`, the
legacy path with no currency, is unchanged: its contract is documented
as exact, and hand-written callers did not opt in. Rastrillo has no
plain numeric field kind today; when one arrives (§13), it takes this
reading with no currency spelling, its declared scale as the exponent,
and the same `Strict` option.

**The unchanged-seed check** (normalising fields with `LocaleEcho`).
Beside `<Name>.locale` and `<Name>.currency`, the form carries
`<Name>.seed`: the exact text the server put in the input when it
filled it from the record. That is only on an edit form's server seed,
or on the echo of one (below). It is empty on create, after an echoed
failure of text the person changed, and on a `Strict` field, which
needs no seed because strict reading of a seed is exact.

On POST, after the locale and currency checks:
- A create form never renders a seed, so any non-empty `<Name>.seed` on
  create is `money_reenter`.
- If `<Name>.seed` is not empty and the submitted text, with only
  `strings.TrimSpace` applied, equals it byte for byte, the person left
  the field alone. The parser is not run, so an ambiguous seed such as
  `en` `1.500` for 1.5 KWD saves unchanged.
- **The seed is never a value.** On a match the server re-reads the
  record and renders its current value with `InputValue(<Name>.locale,
  stored, currency)`:
  - equal to the seed: the record's stored value is kept. The negative
    policy does not apply: it is for what the person typed, and the
    kept value is the record's own, so a stored refund in a field
    without `Negative` (a legacy row imported through `[legacy]`, say)
    saves unchanged and the record's other fields stay editable
    (Paul's ruling on the final review's M5). A changed amount is read
    by §5.1, and the policy applies to it as to any reading;
  - different (the record changed since the form was drawn, or the seed
    was forged): `money_reenter`, nothing is stored, and the input is
    re-seeded from the server with a fresh `<Name>.seed`.
- Any other text goes through §5.1 with no exception.

A forged seed can therefore only cause `money_reenter` or keep the
record's own value; it can never put an amount in the store. The
prototype's tests forge a seed equal to the text, send a seed in the
wrong locale, change the record between GET and POST, post a seed on
create, and resubmit an echoed seed after an error on another field,
with and without the record changing in between.

What the field shows:
- **The ISO code beside the input**, always, so a bare shared symbol is
  read against a currency the person can see.
- **After a successful save**, the stored value, never the typed text.
  The next render seeds the input with `InputValue(render locale,
  stored, currency)`, so `US$ 1,284.5` comes back as `1284.50`, with
  that text in `<Name>.seed`. A handler that re-renders in the same
  response seeds the same way.
- **After a validation error** on this or another field, the input
  echoes the person's own text, unchanged, with its parsing locale.
  - If that text was the field's verified seed (equal to it after
    trimming, and the seed equal to the record's fresh rendering), the
    echo carries the same `<Name>.seed` too. Otherwise an unchanged
    `1.500` KWD on an English edit form would save at the first
    submission and be `money_ambiguous` at the second.
  - Any other text is echoed with an empty seed, and is read by §5.1 on
    resubmission, exactly as before.
  - An echoed seed is trusted no more than the first: every submission
    verifies it against the current record, so a record changed between
    the two submissions still gives `money_reenter`.

**The per-field contract.** Every money input carries three hidden
values beside its text: `<Name>.locale`, the parsing locale of the
text; `<Name>.currency`, the currency the form displayed (decision 18);
and `<Name>.seed`, the server seed it rendered, if any (§5.3). The
generated `formView` carries `Money map[string]form.MoneyInput{Value,
Locale, Currency, Seed}` for both form groups.

| Step | Value | `<Name>.locale` | `<Name>.currency` (displayed) | `<Name>.seed` | Currency used to parse and store |
|---|---|---|---|---|---|
| GET new | `""` | render locale | resource default now | empty | n/a |
| GET edit | `InputValue(render, minor, cur)` | render locale | record's currency | the same text | n/a |
| POST create | raw | must be one valid shipped id | must equal the resource default **now** | must be empty, else `money_reenter` | resource default (server) |
| POST edit | raw | must be one valid shipped id | must equal the record's currency **now** | checked as §5.3 says | record's currency (server) |
| failure re-render, submitted group | echoed raw text | `EchoLocale`: the locale it was parsed in | the server's currency | the submitted seed if the text was that verified seed, else empty | same |
| failure re-render, other group | `InputValue(render, …)` from the record | render locale | record's | the same text | same |
| locale metadata rejected, or seed refused | server seed (blank on create) | render locale | server's | the server seed (empty on create) | same |
| currency metadata mismatched | **echoed raw text**, so nothing typed is lost | the echoed locale | the server's current currency | empty | same; the user must resubmit |

- **The locale check.** `<Name>.locale` must appear exactly once, be at
  most 35 bytes of `[A-Za-z0-9-]`, and satisfy
  `currency.Resolve(v).ID == v`. Otherwise the field gets
  `money_reenter` and the input is re-seeded from the server.
- **The currency check** (round-two finding 2). When the cleaned amount
  is not blank, `<Name>.currency` must appear once and, after
  `currency.Normalize`, equal the server's currency. Otherwise the field
  gets `money_currency_changed`. The page then shows the person's text
  beside the new currency's symbol and says what changed, and the next
  submission carries the new currency.
- **The client value is never authoritative.** It is compared and then
  discarded, and the stored currency is always the server's. A forged
  value can only cause a rejection, never a different denomination.
- The check covers a manifest default changed by a deploy between GET
  and POST, and an edit form left open across the explicit
  change-currency operation (§7).
- **Without `LocaleEcho`** (hand-written handlers and API clients),
  `Field.Locale` is the parsing locale and there is no currency echo.
  The handler owns consistency.

### 6. Error copy

Approved; see Appendix C.1, which code quotes verbatim. Each new key
needs eleven translations afterwards, drafted and not reviewed.

| Key | English (approved) |
|---|---|
| `rastrillo.ui.money_invalid` (existing) | Enter a valid amount. |
| `rastrillo.ui.money_precision` (existing, 2 places) | Enter an amount with no more than 2 decimal places. |
| `rastrillo.ui.money_precision_0` | Enter a whole amount, with no decimal places. |
| `rastrillo.ui.money_precision_3` | Enter an amount with no more than 3 decimal places. |
| `rastrillo.ui.money_decimal_comma` | Use a comma for decimals, like 12,50. |
| `rastrillo.ui.money_decimal_point` | Use a point for decimals, like 12.50. |
| `rastrillo.ui.money_currency` (strict fields only) | Enter the amount without a currency sign. |
| `rastrillo.ui.money_wrong_currency` (new, normalising) | Enter the amount in {currency}. |
| `rastrillo.ui.money_ambiguous` (new, normalising) | This could be {a} or {b}. Enter the one you mean. |
| `rastrillo.ui.money_ambiguous_precision` (new, normalising) | This could be {b}. {a} has too many decimal places for {currency}. Enter the amount you mean. |
| `rastrillo.ui.money_negative` | Enter an amount of 0 or more. |
| `rastrillo.ui.money_reenter` | Enter the amount again. |
| `rastrillo.ui.money_currency_changed` (new, round two) | The currency changed while you were editing. Check the amount and save again. |

Normalising fields (§5.1) reuse `money_invalid`, the three precision
keys, the two decimal-mark keys and `money_negative` unchanged. They add
three:
- `money_wrong_currency` replaces `money_currency` there. A normalising
  field accepts its own currency's sign, so "without a currency sign"
  would be wrong advice, and following it turns `€5` into 5 dollars
  without a word. `{currency}` is the field's ISO code (`USD`):
  packs carry symbols, not currency names, and a symbol such as `$` is
  shared.
- `money_ambiguous` is for input with two readings, such as `de`
  `1,500` in a KWD field or `en` `1.500` in a USD field. The
  decimal-mark message only helps a person who meant the decimal. `{a}`
  and `{b}` are the smallest and largest readings, written with the ISO
  code so no reader can take one for the other (§5.1): "This could be
  1,5 KWD or 1500 KWD."
- `money_ambiguous_precision` replaces `money_ambiguous` when one of the
  two readings has more decimal places than the currency allows
  (`en` `1.500` in a JPY field), because "This could be 1.5 JPY" names
  an amount that cannot exist. `{a}` is that reading, `{b}` the other,
  `{currency}` the ISO code. Copy review chose its own sentence for
  this case.

Operator-facing boot messages (§7, §8) are log text, not on this list.

### 7. Generator, legacy migration, event stores

**Manifest:**

```toml
currency = "EUR"     # default for new records: must pass currency.Normalize

[legacy]             # only for tables with rows from before this release
currency = "USD"     # any stored code, eligible or not (SLL is fine)
scale = 2            # decimal places the old integers carry
```

An ineligible default generates with a warning, so an unchanged
manifest never stops generating. `[legacy]` is separate from the
default (decision 10).

**Schema and boot.**
- **The legacy step runs once, in one `BEGIN IMMEDIATE` transaction**,
  in `gen.Boot` (`currency/sqlmoney.Upgrade`), after the app's
  migrations:
  1. Add `<f>_minor INTEGER` and `<f>_currency TEXT`, both nullable.
  2. Find the legacy rows: `<f>` set and neither new column set.
  3. For each one, compute `currency.Rescale(old, scale, currency)` and
     write it with the currency.
  4. Drop `<f>`. SQLite's `DROP COLUMN` rewrites the table, so the
     step's time grows with the table's size (§8). An index, view or
     trigger that names `<f>` blocks the drop, and boot stops.
  5. Insert the marker row `rastrillo_money(version) = 2`.

  What shipped is add, copy, drop, not the rename this section first
  described; §1 gives the reason. `BEGIN IMMEDIATE` serialises two
  instances booting at once, so no amount is rescaled twice.
- **Any failure rolls everything back and fails boot.** An inexact or
  overflowing value is a failure, and the message lists up to 20 rows,
  for example "1250 at scale 2 is 12.50, which JPY cannot hold".
- Legacy rows without a declared `[legacy]` also fail boot. The message
  names the resource, the row count and the keys to add. It never
  guesses.
- **A fresh database** gets the final schema and the marker directly,
  and the legacy step is a no-op.
- **Reruns are no-ops.** What the step does is decided by which columns
  exist: once `<f>` is gone there is nothing to do, and the scoped
  update can never touch a row written with a currency.
- **Boot also checks the rule.** It counts rows where exactly one of
  `<f>_minor` and `<f>_currency` is NULL, in every money field, and
  refuses to start if any exist. After the drop, an old binary cannot
  write, so no legacy row can appear later. The round-two
  read path that converted such rows is withdrawn.
- **Inserts and updates follow the rule.** An insert binds the resource
  default with any amount it writes. An update that gives an empty field
  its first amount writes the resource default with it. An update never
  changes a currency that is already set; that is the change-currency
  operation's job.

**The change-currency operation** (decision 9) is the only currency
mutation, and it is not in v1. When it exists, it is its own action,
with its own confirm page. It re-enters or rescales the amount
explicitly, emits its own event kind, and invalidates open edit forms
through the currency echo (§5).

**Event stores** (round-two finding 4; confirmation round, gap 3). The
rules, as the boot replay check applies them. They ship in
`currency/replay`, with its tests; the prototype, `hack/moneycldr/replay`,
was deleted (historical).

- **Every event a new writer emits carries `V: 2`**, whatever its kind:
  created, updated or deleted, with or without money fields.
  - A missing `V` means legacy (version 1).
  - A `V` this binary does not know (anything other than absent or 2,
    an explicit 0 included) is a replay error that names the stream and
    the version. **The version is checked before the kind**, so a newer
    writer's tombstone is refused like its updates. The binary never
    guesses at a newer writer's events.
- **Version 2: a currency if and only if an amount**, for each money
  field, in created and updated events alike:
  - an amount without its currency is a replay error, and so is a
    currency without an amount;
  - neither means "unchanged", and for a field that has never had an
    amount it means "still empty";
  - an amount with its currency sets the field. If the field already
    has a currency, the event's must be **unchanged**. That is decided
    by comparing the event's code, after `currency.Normalize` (trimmed,
    upper case, in the storage set), with the currency replay has
    derived for that field so far, in the merged event order. An equal
    code is accepted. A different one is a replay error, "a currency
    change needs its own event". Only the change-currency event kind
    may change it.
- **Why this makes "adds money later" replayable.** A resource that
  gains a money field already has version-2 history written before the
  field existed. Those events carry no amount for it, so under this rule
  they need no currency, and the field is empty for those records.
  Nothing guesses an amount or a currency for them. The first event that
  sets an amount carries its currency, which is the resource default the
  edit form showed. The round-four rule that "a version-2 created event
  must carry the currency of each money field" made that history
  unreplayable, and is replaced by this one.
- **Version 1 (legacy):**
  - a currency in a version-1 event is a replay error, because legacy
    writers had none;
  - an amount-free version-1 event needs nothing, and a legacy
    notes-only history replays without `[legacy]`;
  - a version-1 amount needs `[legacy]`, and is rescaled, checked, from
    `[legacy].scale` to the field's currency's exponent. That is the
    field's current currency if it has one, otherwise the legacy
    currency, which it then takes.
  - A version-1 event written after the upgrade is caught first by the
    per-(stream, writer) snapshot rule in §8.
- **Tests** (`currency/replay/replay_test.go`):
  - version-2 history from before the field existed, created plus
    amount-free updates, replays without `[legacy]` and leaves the field
    empty;
  - an update that then sets EUR 12.50, an amount-free edit, and another
    EUR amount give EUR 1300 minor units;
  - an amount without a currency, a currency without an amount, a
    currency change, an unsupported code and version 3 are each
    rejected, while a repeated same currency is accepted;
  - the version-1 cases: needs `[legacy]`, amount-free, rescale into
    EUR, inexact into JPY, exact into JPY, into KWD, and a version-1
    event carrying a currency.
- **The future change-currency event** has its own kind, carrying the
  field, the from code, the to code and the new amount.
- A boot-time replay check surfaces any of these errors before a request
  does.

### 8. Upgrade and rollback

**For an app that stores money, v1 requires a stop-start upgrade, and
rollback to a currency-unaware binary is forbidden** (decision 17). With
legacy USD at scale 2 and a JPY default, a new binary renders ¥1,200 as
`"1200"`, and an old binary parsing that unchanged POST with
`ParseCents` stores 120,000. So every store that holds money gets a
tripwire that makes an old binary fail loudly instead. An app that
stores no money gets no tripwire. **Every app, money or not, runs the
template preflight** (§10): this release changes how templates call
`T`, `number` and the other locale functions, and an app that registers
its own `T` breaks when a migrated partial calls it.

**Two tripwires, both tested through the real migration code**
against the real `eventlog`, `migrate` and SQLite driver
(`eventlog/money_test.go` and `currency/sqlmoney`; the prototype probe,
`hack/templatemem/upgradeprobe`, was deleted):

| Store | Tripwire | What an old binary does afterwards | Cost |
|---|---|---|---|
| SQL (generated exclusive stores) | Add `<f>_minor` and `<f>_currency`, copy the rescaled amounts, drop `<f>`, in the legacy step's transaction (§7) | Every query it was generated with names `<f>`, so it fails with "no such column". Re-running its boot `CREATE TABLE IF NOT EXISTS` does not restore the column. | Add, copy, drop. The drop rewrites the table, so the time grows with its size: about 150 ms for 20,000 rows and 1.4 s for 200,000 (one USD field, file database in WAL mode, measured for Task 29). |
| Event store, **only in apps whose events carry money** (Paul) | Rename `eventlog.payload` to `payload_v2` | Today's `Events`, `EventsByPrefix`, `Append` and `Ingest` all fail with "no such column: payload". Re-applying `eventlog.Schema` does not undo it. | The migration (ledger row, rename and boundary snapshot) takes about half a second per 200,000 events: 475 ms when Task 15 measured it, 415 to 422 ms in three runs of `TestEventlogRenameCost` for Task 29. The prototype's 19.5 ms is superseded. |

**Which apps rename the event log** (Paul's decision; round-four
findings 7 and 8):

- **Declared, never guessed from data.** The rename lives in its own
  migration set, `eventlog.MoneySchema` (namespace `eventlog_money`, one
  migration, `0001_payload_v2`). An app includes it in its migration set
  when its events carry money.
  - For a generated app, that is decided from the manifests: some
    resource has `store = "mergeable"` and a field of kind `money`.
    `generate` then makes the generated `gen.Boot` call
    `eventlog.UpgradeForMoney`, which applies `MoneySchema` through
    `migrate.Apply` when the app has a migration ledger, and the same SQL
    in one transaction when it has none. This spec first said
    `generate` adds the set to a generated `BootSchema`; there is none,
    because nothing the generator writes reaches the app's migration
    call, while every app with manifest resources must call `gen.Boot`.
  - A hand-written app whose events carry money adds the set itself; the
    docs say so.
  - Apps whose events carry no money never include it. They keep
    `payload` and upgrade normally, with no stop-start and no rollback
    restriction from this change.
- **eventlog detects its column at startup.** `eventlog.Open` reads
  `pragma_table_info('eventlog')` once per database handle (cached), and
  uses `payload` or `payload_v2`, whichever exists. It refuses a table
  with both, or with neither. The probe tests the detection on all three
  start points below and on an app without money.
- **The pre-ledger path.** The migration is **post-adoption**: its SQL
  carries `-- rastrillo: post-adoption`. A database built before the
  ledger existed has `payload`, so it matches `eventlog.Schema` without
  the rename. Adoption stamps that base, then runs the rename as a
  pending migration (migrate/adopt.go). Without the marker, adoption
  refuses the database ("cannot be adopted safely"). The probe shows
  both: removing the marker reproduces the refusal Astra predicted.
- **Tested through `migrate.Apply` on three start points**:
  - fresh;
  - ledger-backed (`eventlog.Schema` applied earlier, events present);
  - pre-ledger (eventlog's DDL run without a ledger, events present,
    `Result.Adopted` true).

  Each ends with `payload_v2`, the ledger
  `eventlog/0001_init eventlog_money/0001_payload_v2`, its events intact,
  and today's eventlog code failing.
- **An app that adds money later.** Once a mergeable resource gains a
  money field, the next `generate` adds the `eventlog.UpgradeForMoney`
  call to `gen.Boot`, and the next boot applies `MoneySchema`. That
  deploy is the money upgrade for its event log: it needs stop-start
  and a backup, and `generate` prints exactly that when it adds the
  call (`op.generate.money_schema_added`).
- **An app that removes money later** keeps the renamed column. The set
  stays applied in the ledger, and detection at startup keeps eventlog
  working whether or not `generate` still lists the set.

**Rollback** (round-four finding 7):
- **An app with money rolls back by restoring the backup** taken before
  the upgrade, and only that way. The restore brings back the database
  and its migration ledger together, so they cannot disagree. The probe
  runs it: back up with `VACUUM INTO`, upgrade, restore the files, and
  check that the column is `payload`, the ledger lists only
  `eventlog/0001_init`, and the old code works. Upgrading again renames
  again, and the ledger agrees.
- No hand-renaming rollback is offered, so no ledger entry can be left
  behind.
- **An app without money** never renames, so it needs no rollback path
  for this change. The round-three no-money path ("rename the column
  back") is withdrawn with the unconditional rename it existed for.

**Detecting legacy writes after the upgrade, without a global
position.** eventlog has no global append position: `seq` and
`lamport` advance per stream (eventlog.go:111-125). The rule uses the
table's own key, `(stream, writer, seq)`:
- In apps with money, the upgrade snapshots
  `eventlog_upgrade(stream, writer, seq)`, the highest seq of every
  (stream, writer) pair, in the same migration as the rename.
- On replay, an unversioned event is an error when its seq is above its
  pair's snapshot, or when its pair has no snapshot row.
- A backup restore removes the snapshot with everything else, so no
  stale boundary survives a rollback.

**Version markers.** `rastrillo_money(version)` in SQL and `V` in events
let any future binary refuse data newer than it understands.

**Hand-written apps** get the same advice. Both tripwires are
recommended to them; for the event log that means including
`eventlog.MoneySchema` when their events carry money.

**Changelog draft** (user-facing, so it goes to copy review; no em
dashes). Historical: the shipped text is Appendix C.3, whose step 4 says
each money column is replaced, not renamed.

> **Multi-currency money (breaking upgrade for apps with money).** Money
> is now stored with its currency, and the locale data that formats it
> comes from language packs.
> 1. Run `rastrillo doctor`. It lists every template and Go file the new
>    template calls affect, and what to change in each. Run
>    `rastrillo migrate-templates -write` on your templates, make the Go
>    changes it lists, and run `rastrillo doctor` again until it reports
>    nothing. Every app needs this step, whether it stores money or not.
> 2. Run the new version with `RASTRILLO_CHECK_CONFIG=1`. It checks the
>    locale setup, prints what it found, and exits without opening the
>    database. If it names a missing pack: generated apps run
>    `rastrillo generate`; other apps add the import it names.
> 3. For each money resource with existing data, add `[legacy] currency`
>    and `scale` to its manifest and run `rastrillo generate`. The app
>    refuses to start without them.
> 4. If the app stores money: take a backup, stop every running copy,
>    then start the new version. Never run old and new versions side by
>    side. To go back, restore the backup. Older versions refuse to run
>    against the upgraded database: money columns, and the event log in
>    apps that keep money in events, are renamed so they fail instead of
>    saving wrong amounts.
> 5. Apps that store no money need no backup or stop-start beyond the
>    template changes in step 1.
>
> Money fields now accept amounts the way people type them: `US$ 5`,
> `5 USD`, `1.284,50` on an English page. They still refuse another
> currency, an amount that could mean two things, and extra decimals.
> To keep the old, exact format for a field, set `Strict: true`.

### 9. Gallery, Portuguese retag, sample

- **The Money sample** renders three currencies with `currency.Format`
  in the page's locale, as round two described. The body text is a
  draft for copy review:

  > Store each amount as a whole number of the currency's smallest unit,
  > such as cents, with its currency code beside it. Show it with
  > `money`, which writes it the way the reader's language does. The
  > currency always comes from your data, never from the reader.

- **The `pt` → `pt-PT` retag** (decision 8) is a separate change:
  - The catalog moves to `locales/pt-PT.toml`.
  - `Options.Locales` lists `pt-PT`.
  - A request for the `/pt/` prefix redirects (301) to `/pt-PT/`, and a
    `rastrillo_locale=pt` cookie is read as `pt-PT`.
  - Catalog lookups for `pt` fall back to `pt-PT` through the existing
    layers. An app's own `pt.toml` keeps working, because its keys are
    read under both codes.
  - The `pt` pack already covers both `pt` and `pt-PT`.
  - After the retag, Portuguese money reads `1284,50 €`.

### 10. Templates: no per-language and no per-request template sets (Paul)

**The decision** (decision 21), stated precisely:
- **Not one template set per language, and not one per request.** At
  boot, the *base* (ui's partials, the app's layout and its shared
  template files) is parsed once.
- Each page is then one clone of that base, made once at boot, with the
  page's own definitions (`content`, `nav`, `brand`, …) added. Pages
  repeat those names, so they cannot share one namespace; one clone per
  page is Go's usual layout pattern and what the scaffold already does.
  The base is dropped once the page clones exist.
- Every language and every request executes those same page clones.
  Nothing is cloned per language or per request, and no function map is
  ever changed after boot.
- Locale-dependent output (`T`, `Tf`, `Tbdi`, `money`, `number`,
  `displayValue`, `dateWords`) is resolved during each execution, from
  the localizer the data carries. Request values (the current user, a
  flash) are data too.
- The round-three and round-four design (lazy per-locale sets) is
  superseded (review log).

**Binding, and what the risk actually is** (round-eight finding 12).
html/template requires a function's *name* to exist when it parses, and
evaluates its *arguments* when it executes. The implementation behind a
name can be replaced later with `Funcs`. Go synchronises those reads and
writes, so rebinding is not a data race. But rebinding a function map
that other requests are executing is shared mutable binding: one
request's locale or user can end up in another's output with no race for
`-race` to report. So `ui.Pages` builds its function map once and never
changes it, the locale arrives as an argument, and the tests assert
output isolation on every execution, not just a clean `-race` run.

**Two call syntaxes were evaluated:**

| | Methods on the data: `{{$.L.T "key"}}` | Functions taking the localizer first: `{{T $L "key"}}` |
|---|---|---|
| A misspelled name | Found only when that page executes | **Fails parse, so boot** |
| A missing localizer | "nil pointer evaluating …" | The function reports a precise error (strict), or uses the app's default (compat) |
| Pipelines | `{{.Key \| $.L.T}}` | `{{.Key \| T $L}}` |
| Reaching partials | must be passed | must be passed |
| Migration | every call takes a new shape | one inserted argument |

**Chosen: functions taking the localizer first.** The contract, for
every call:

| Call | Arguments | Notes |
|---|---|---|
| `{{T $L "key" args…}}` | `*Localizer`, key, optional interpolation pairs | |
| `{{Tf $L "key" "name" v …}}` | same | |
| `{{Tbdi $L "key" "name" v …}}` | same | user text is escaped and wrapped in `<bdi>` |
| `{{number $L v}}`, `{{displayValue $L v}}` | `*Localizer`, any value | a nil value renders exactly as today |
| `{{dateWords $L}}` | `*Localizer` | |
| `{{money $L amount code}}` | `*Localizer`, an integer amount (or a pointer to one, where nil writes `""`), an ISO code | there is no legacy form |

A call's form is decided by its first argument's **type**:
- a `*Localizer`, nil or not, is the localizer form;
- anything else is the legacy form.

`$L` is always a typed `*Localizer` (below), so a nil value passed to a
legacy `{{number .Value}}` can never be mistaken for a missing localizer
(round-eight finding 7).

**One localizer representation: a pointer, everywhere** (round-eight
finding 2).
- Pages put a `*Localizer` in their data: `L` in a map, or a field `L`.
  Hand-written view structs embed `ui.Localized{L *Localizer}`, and
  generated view structs gain the field.
- Each template body that uses it begins `{{$L := localizer $}}`.
  `localizer` returns the `*Localizer` found under `L` (embedded fields
  included), or a typed nil. It replaces round seven's `opt $ "L"`,
  which dereferenced the pointer.
- `{{template "x" dict …}}` passes it as `"L" $L`.
- Other data is wrapped as `(withL (PIPELINE) $L)`: a map is copied with
  `L` added; a struct passes through unchanged, and must carry its own
  `L`. The regression tests execute both paths: `field-daterange`'s
  `.Start`/`.End` dicts (the map path) and a struct embedding
  `Localized` (the struct path). A struct without `L` fails under strict
  dispatch.
- `{{template "x" .}}` (outside `range`/`with`) and `$` pass the same
  `$`.

**Strict in production; compatibility only on request** (round-eight
findings 3 and 6).
- **Strict is the default, in production.** A localizer-form call whose
  `$L` is nil is an execution error naming the function
  (`ErrNoLocalizer`), and so is any legacy-form call (`ErrLegacyCall`).
  So a conditional partial reached with struct data lacking `L` fails
  the request that reaches it. It never renders in the wrong language.
  `Pages.Execute` also refuses page data without a localizer.
- **Compat mode is opt-in** (`Config.Compat`), for apps mid-migration.
  Legacy-form calls and nil localizers render with the **app's own
  default-locale localizer**, its catalog including the app's keys, not
  the framework's English. It is logged at `New`, in capitals, and once
  per function on its first legacy call.
  - The test runs a Spanish-default app in compat mode. Its unmigrated
    `{{T "app.hello"}}` renders the app-only Spanish key `Hola`, and the
    logs say so.
- **Which legacy calls are supported:** only in compat mode, the
  one-argument-shorter forms of `T`, `Tf`, `Tbdi`, `number`,
  `displayValue` and `dateWords`. `money` has none. `ui.WithT`,
  `ui.FuncsWith` and per-request `Clone` are not supported by
  `ui.Pages`; they remain only for apps that keep their own rendering
  until they migrate.

**What lives in the function map:** ui's structural functions (`dict`,
`list`, `opt`, …), `localizer` and `withL`, the locale functions, and the
app's own request-independent functions (the scaffold's `icon`,
`iconAssets`, `asset`). An app function may not take the name of a
locale function, `localizer` or `withL`; `New` refuses it. Template
precedence is unchanged: ui's partials parse first, so an app's
`{{define}}` of a partial wins.

**Every app runs a template preflight on upgrade, money or not**
(round-eight finding 8). An app that registers its own `T` breaks even
if it never adopts `ui.Pages`: migrated partials call `T($L, key)`, a
one-argument `T` cannot take that, and parsing does not notice, so it
fails when the partial executes.

**The preflight is advisory. The runtime check is the guarantee.**
- Nothing static can see every way a Go program builds a function map,
  so the guarantee is at run time. `ui.Pages`'s `New` refuses an app
  function named after a locale function, `localizer` or `withL`, so the
  app does not boot. And strict dispatch makes any locale call that
  reaches a template without a localizer an execution error, so a page
  fails rather than rendering in the wrong language.
- The preflight exists to find these breaks before deploying, not to
  prove they are absent.

`rastrillo doctor` runs `rastrillo migrate-templates` as a dry run for
every app. It covers the `.html` files and, **type-checked with
`go/types` through `golang.org/x/tools/go/packages`** (round ten), the
app's Go packages, loaded by its own module, imports and build
constraints:
- **A registration** is any key written into a map whose **type** is
  `template.FuncMap` (text or html), however it was created: a literal,
  `make`, `ui.Funcs`, or a helper function's parameter. It is also any
  key written into a `map[string]any` that flows into a template's
  `Funcs`, `ui.FuncsWith` or `ui.WithT`, directly or through helper
  functions in the package.
- **A key counts as resolved only when the type checker gives it a
  constant string value** (`types.Info.Types[key].Value`). That handles
  shadowing, conversions and constant expressions exactly. A resolved
  key named after a locale function is a finding. **Every non-constant
  key is a review item**, and review items fail the dry run.
- **`ui.WithT` and `ui.FuncsWith`** are identified by the object the call
  resolves to. So an alias, a dot-import, another package called `ui`
  and a local variable named `ui` are each handled correctly.
- Each finding comes with its file, line and fix.
- **The dependency:** `golang.org/x/tools` (v0.47.0, already in the root
  module's `go.sum` as an indirect dependency) was required only by the
  `hack/templatemem` module. The preflight now ships in
  `internal/tmplmigrate`, and the root module requires
  `golang.org/x/tools` directly.

**It fails while anything is left to do** (round-nine finding 3). The
dry run exits 1 while any automatic template edit is still pending, even
in an app with nothing to review. It also exits 1 for a refused file, a
Go finding (review items included), a struct-data site and a no-data
call. After `-write`, the templates it rewrote no longer count.
`rastrillo doctor` treats any non-zero exit as a failing preflight. On the
repo it reports the three registrations, in blog (view.go:64), notes
(genrender.go:39) and tickets (render.go:47). A fixture checks that a
`"T"` inside a string, a comment or a non-function map is not reported.
§8's upgrade notes now include this step for every app.

**The codemod: `rastrillo migrate-templates [-write] PATH...`**
(round-eight findings 4 and 5). It ships as `internal/tmplmigrate`
and `cmd/rastrillo`'s `migrate-templates`; the prototype,
`hack/templatemem/codemod` and `cmd/migrate-templates`, was deleted.
- **From the parse tree:** every edit is an insertion at a position
  text/template's parser reports.
  - Locale calls gain `$L`.
  - `dict` calls gain `"L" $L`.
  - Other template data is wrapped **as a complete pipeline**:
    `{{template "p" makeData .Item}}` becomes
    `{{template "p" (withL (makeData .Item) $L)}}`, so argument grouping
    is kept. The pipeline's end is found by a quote-aware scan that skips
    `}}` inside interpreted strings, raw strings and rune literals.
  - Bodies gain the prelude.
- **Validated before writing.** The rewritten text is parsed again, and
  the new tree must satisfy every promise:
  - each locale call passes `$L`, and each `dict` call has `"L"`;
  - each body using `$L` starts with the prelude;
  - each wrapped pipeline is exactly the original, compared as parse
    trees after normalising away the codemod's own permitted insertions
    inside it (`$L` as a locale call's first argument, `"L" $L` in a
    dict). So `{{template "p" makeData (T "key")}}` validates as
    `(withL (makeData (T $L "key")) $L)`, while a regrouping or any
    other change fails (round-nine finding 1).

  A rewrite that fails validation is not written.
- **`$L` scope is tracked.** The only accepted binding of `$L` is the
  prelude at the very top of a body. Any other, at any depth (in an
  `if`, `range` or `with`, a range variable, an assignment, a different
  body-level meaning), makes the file **refused and left unchanged**,
  with each binding's line reported.
- **Reported for a person:**
  - every wrapped site (struct data needs an `L` field);
  - every `{{template "p"}}` with no data at all, whose callee receives
    nil and so no localizer (none exist in the repo);
  - every refused file;
  - every Go finding.
- **Dry run unless `-write`; idempotent.**
- **Repo counts.** Recounted for Task 29 with the shipped codemod
  (`tmplmigrate.RewriteFS`) on the tree just before the first template
  migration commit (`459615fd^`), over the roots the prototype's pinned
  test used. `ui/partials` has 116 calls where the prototype counted
  114: main's `pagination.html` (84e19457) added two `number` calls.
  The live test, `internal/tmplmigrate/corpus_test.go`, now pins every
  corpus at zero, since the tree is migrated.

  | Corpus | Files | Locale calls | `dict` calls | Wrapped (review) | Preludes |
  |---|---|---|---|---|---|
  | ui/partials | 20 | 116 | 5 | 5 | 22 |
  | ui/layouts | 5 | 20 | 0 | 0 | 10 |
  | examples/tickets | 9 | 66 | 37 | 1 | 9 |
  | examples/notes | 10 | 32 | 35 | 4 | 10 |
  | examples/blog | 7 | 15 | 24 | 1 | 7 |
  | the scaffold | 2 | 6 | 1 | 0 | 4 |
  | **total** | **53** | **255** | **102** | **11** | **62** |

  Plus 3 Go registrations (one in each of blog, notes and tickets,
found by the type-checked scan), 0 no-data calls and 0 refused files.
- **Go-embedded templates** (the generator, the scaffold, the gallery's
  samples) are emitted in the new syntax by their emitters, and the
  examples are regenerated.

**Measured** (historical: on the prototype, `hack/templatemem`; the
real `ui.Pages` structure; the scaffold's three pages plus a
representative form page built from ui partials (`field-text`,
`field-daterange`, `form-foot`, `pagination`, money and a number); ui's
real partials migrated by the codemod; the framework's real catalogs;
the real money formatter):

| Memory, separated (round-eight finding 11) | Retained |
|---|---|
| **Templates:** the base parsed once + 4 page clones, every page served | **2.0 to 2.2 MB, the same with 1 or 12 offered locales** (the difference is below GC noise) |
| **Per offered locale:** its localizer's function map | **1.4 KB** |
| **Per offered locale:** its framework catalog | about 4.4 KB (53.3 KB for all 12), plus the app's own keys |

| Per request | Cost |
|---|---|
| **Shared structure, scaffold overview page, es** | **29 µs, 9.1 KB, 316 allocs** |
| Shared structure, representative form page, es / ar | 142 / 145 µs, 50 / 56 KB, 1,414 / 1,421 allocs |
| Baseline: one escaped tree, no localization, overview page | 23 µs, 7.5 KB, 267 allocs |
| Today's multilingual pattern: Clone + Funcs + Execute, overview page | 440 µs (436-961 µs across runs), 516 KB, 9,797 allocs |
| Superseded: lazy per-locale sets, all 12 served (3 pages) | 15.83 MB retained |

**Budgets, as implementation tests** (bounded to what is measured):
1. **Template memory** for a given set of pages does not depend on the
   number of offered locales. With 1 and with 12 offered locales and
   every page served in each, retained template memory is within 10% of
   each other.
2. **Per-locale memory** is at most 4 KB of localizer per offered locale
   (1.4 KB measured), plus that locale's catalogs. It is measured at 1
   and 12 locales. Beyond 12, per-locale memory is linear in this
   measured term; no whole-heap bound is claimed for an unmeasured
   locale count.
3. **Per request:** the scaffold overview page in the shared structure
   is within 1.5 × the one-tree baseline's bytes for the same page, and
   `Pages.Localizer` allocates nothing.
4. **Two languages, 200 concurrent executions:** every output is checked
   to contain only its own language's strings and its own request's
   user, and the run is also under `-race`.

**Tests in the prototype** (historical: `hack/templatemem/pages`,
`codemod`, `entry`, under `-race`; they moved to `ui`,
`internal/tmplmigrate` and the root package). Every positive case has
a matching negative fixture that must fail.
- **The corpus executes** migrated fixtures with the production
  function signatures, through real ui partials, in es and en:
  - escaping in each context: HTML text, attribute, URL (`javascript:`
    becomes `#ZgotmplZ`), JavaScript (`<script>…`), and
    `Tbdi`'s escaped, `<bdi>`-isolated user text;
  - localized output: es `1.234.567` and `1284,50 €`, en `1,234,567`
    and `€1,284.50`, the app's title and the framework's `field-date`
    strings in each language;
  - the `withL` map path (`field-daterange`) and the struct path
    (`Localized`).
- **Negative fixtures, strict:** a partial reached with a struct lacking
  `L` (`ErrNoLocalizer`), an unmigrated call (`ErrLegacyCall`), `money`
  missing an argument, page data without `L`, and an `L` that is not a
  localizer.
- **Compat:** the Spanish-default app's app-only key, rendered and
  logged.
- **Nil values:** legacy `{{number .Nil}}` (compat) and migrated
  `{{number $L .Nil}}` (strict) both render exactly what today's
  framework renders, compared against the current `ui.Funcs` output.
- **Isolation** as in budget 4. **Mutation checks:**
  - restoring a silent fallback for a nil `$L` fails the strict test;
  - routing the locale through shared state fails the isolation test
    with mixed outputs;
  - restoring today's `Run` order fails the probe-entry test.
- **Codemod:** 14 rewrite cases (including grouping, pipelines and `}}`
  inside interpreted and raw strings), each checked for idempotence;
  five conflicting-`$L` refusals; the no-data report; validation
  rejecting a lost `$L`, a regrouped pipeline and a dict without `L`;
  the repo corpus; and the Go preflight on the examples and on a fixture.
- `New` refuses an app function named `T` or `localizer`, a duplicate
  offered locale, a syntax error and a broken catalog. Each page keeps
  its own definitions.

**Scope.** This is a rendering change that ships with money, because
`{{money $L …}}` is its first new call, and that release is already a
template-touching upgrade for every app (§8).

### 11. Docs to update

- forms.md: minor units, the field options (`Strict` among them), the
  normalising and strict readings with §5.1's examples, the errors,
  `field-money`, and both hidden inputs.
- templates.md:374-392: `{{money}}`, and remove "does not do this for
  you yet".
- reference/form.md: the new options and the legacy path.
- New pages: reference/currency.md and reference/locales.md (the packs,
  the fallback, `CheckConfig`, the `RASTRILLO_CHECK_CONFIG` probe,
  `doctor`).
- reference/ui.md: `money`, `ui.Pages`, the localizer, `$L`, strict
  mode, and the call-syntax table from §10.
- templates.md and SKILL.md: every `{{T …}}` example becomes
  `{{T $L …}}`; partial calls pass `"L" $L`; the per-request Clone
  pattern (templates.md:48, SKILL.md:494, the `ui.FuncsWith` comment) is
  removed in favour of the localizer.
- New page: `rastrillo migrate-templates`, its dry run, and the review
  list.
- manifests.md: `currency`, `[legacy]`, `app.toml`'s `locales` and
  `default_locale`, the renames, the boot refusals.
- localization.md: the doc line for adding a language (§3).
- reference/money.md: the storage set.
- SKILL.md:233-235, :107-110 and :494-500.
- DESIGN.md:315-324.
- cmd/rastrillo/new.go:1612.
- The CHANGELOG (§8 draft).

### 12. Test plan

**Already in the harness** (`cd hack/moneycldr && go test ./...`,
offline, about 40 s):
- Format: the gallery locales; fr-CH's currency marks; en-150's EUR
  override; pt-CV's CVE `$` decimal; zh-TW versus zh; en-IE and es-MX;
  `swc`'s sw-CD marks; malformed and unknown tags going to root; the
  `int64` boundaries at exponents 0, 2 and 3.
- Resolution:
  - aliases `swc`, `cnr`, `drw`, `prs` and `swc-TZ`;
  - malformed `de--CH`, `de-CH-!!!`, `-de`, `de-`, `de-CH-u`, `de-CH-x`,
    `x-private`, `toolonglanguage`, `d`, `""` and `123`;
  - `de-CH-u-nu-arab` and `de-1996`.
- Storage set:
  - SLL and HRK stored;
  - MGA eligible by deviation;
  - CHE stored but not eligible;
  - BYR, ITL, CLF, UYW and KDW refused;
  - every exponent equals `money.MinorUnits`.
- Parse: separators and their errors, `swc`'s KWD `1.500`, digits, signs
  (including trailing), markers, both boundaries, overflow, and
  blank-after-clean.
- Normalise (`proto/normalize_test.go`, `proto/field_test.go`, §5):
  - hand-written tables: per locale (en, de, de-CH, fr, hi, ar, ja; JPY,
    2-decimal currencies, KWD), shared symbols per family, field-level
    `Strict` dispatch, and the seed check with a forged seed;
  - the ambiguity's stated shape, checked exactly over a generated
    corpus;
  - consistency with strict reading: where strict accepts, the same
    value or an ambiguity holding it. This is consistency, not intent;
  - redisplay: a stored value's display or seed is never read as a
    different amount, and an unchanged seed saves;
  - `money_ambiguous`'s two choices each have one reading, their own,
    for every reader;
  - the corpus must reach every branch, or the test fails.
- `AmountFromAny`, `InputValue`, and the round trip over every linked
  locale.
- `proto/fallbacktest`: the 12 gallery packs only, with the missing pack
  named, plus `PacksFor`, `CheckConfig` and the app-default fallback.
- `proto/crosspacktest`: en, fr and no only, for the cross-pack fallback
  parents.

**`make money-cldr`**: the oracle grid with exact membership, the
completion line, executable explanations, gated parse failures,
determinism of the committed output, pack sizes, and x/text.

**For the implementation**, beyond copying the harness tests. Every
expected value is written as an exact string or integer:
1. **Legacy migration**:
   - USD at scale 2 to USD: unchanged;
   - to JPY: 1200 becomes 12, and 1250 is refused with the row listed;
   - to KWD: 1250 becomes 12500;
   - SLL at scale 2 to SLL: unchanged;
   - near `MaxInt64`: overflow is refused and the database is unchanged;
   - undeclared legacy rows: boot refuses;
   - a second boot is a no-op;
   - a fresh database gets the final schema.
2. **Tripwire**: an old generated binary pointed at a migrated database
   fails every money query with "no such column" and changes no row.
3. **Mixed-version scenario from round-two finding 1**: legacy USD and a
   JPY default; an unchanged edit POST replayed against the old binary's
   handler is refused. Dropping `<f>` is what makes it fail.
4. **Currency echo**: a create form whose default changed between GET and
   POST gets `money_currency_changed`, keeps the typed text, and stores
   nothing. A forged currency equal to the server's changes nothing. A
   forged different one is rejected.
5. **Locale echo**: round two's KWD `1.500` en→de→resubmit scenario, in
   both form groups. Missing, duplicated, malformed or non-canonical
   values each give `money_reenter`.
6. **Events**:
   - every new event, amount-free included, has `V: 2`;
   - the version is checked before the kind: `V: 3` and an explicit
     `V: 0` are replay errors on any kind, a `V: 3` deleted event
     included, and an absent `V` is legacy;
   - a currency if and only if an amount, per money field, in created
     and updated events alike:
     - an amount without its currency is a replay error, and so is a
       currency without an amount;
     - an amount with the field's unchanged currency is accepted;
     - an amount with a changed currency is a replay error, because a
       change belongs only in the change-currency event's own kind;
     - a historical version-2 create with neither field (written before
       the field existed) is accepted, and the field stays empty;
   - a legacy amount-free update needs no `[legacy]`;
   - a new EUR create followed by a legacy update with `Price=1250`
     gives EUR 1250;
   - a new JPY create followed by a legacy 1250 update is a replay
     error;
   - an unversioned event after the upgrade marker is a replay error.
7. **Packs**:
   - `generate` writes exactly the packs derived from the offered codes;
   - `generate --check` fails when the packs file and `app.toml`
     disagree or a catalog is missing;
   - boot refuses any `CheckConfig` failure;
   - the missing-pack warning is logged once per pack.
8. **Templates**: §10's budgets, the two-language race test with its
   mutation check, strict mode failing on an unmigrated call, the
   codemod corpus test over ui, the scaffold, blog, tickets and notes,
   and each example's own suite passing after migration in two
   locales.
9. **Locale configuration**: `Serve` refuses to start for each
   `CheckConfig` failure case, including an app with no manifest and an
   app with only `DefaultLocale`. `RASTRILLO_CHECK_CONFIG=1` prints the
   effective configuration and exits without listening. Doctor runs the
   probe.
10. **Cross-pack fallback**: hi-Latn, ht, nb and nn with their pack
    absent and the parent's present (already in `proto/crosspacktest`).
11. **Tripwires**: the `upgradeprobe` tests (three start points,
    without-money untouched, upgrade-restore-upgrade, marker
    load-bearing), ported into the eventlog and generator test suites.
    Also eventlog's startup column detection (payload, payload_v2, both
    or neither), `generate` adding the `eventlog.UpgradeForMoney` call
    to `gen.Boot` exactly when a mergeable resource has a money field,
    and the per-writer
    legacy-event rule.
12. **Probe**: run against a scaffolded app whose database file does not
    exist, it reports and exits without creating the file. A
    `RASTRILLO_CHECK_CONFIG=0` exits 2.
    - `Run` with `sidecar run` and the variable set exits without
      calling `Options.Sidecar`, as `probe_test.go` tests (the
      prototype, `hack/templatemem/entry`, is deleted).
    - Reaching `Serve`, `Handler`, `OpenDB` or `db.Open` with the
      variable set exits 2 without opening.
    - The JSON field set is pinned.
13. **Money fields added later**: in a generated SQL store, adding a
    money field to a table with rows leaves them NULL/NULL, and boot's
    pairing check passes. A row with exactly one of the two NULL fails
    boot. In an event store, the `replay` tests, ported.
14. Required-after-clean, negative policy, the template errors, the
   frozen storage set, generator goldens, and browser runs of the
   gallery and `field-money` with JavaScript off.
15. **Normalisation in forms**:
    - the harness's tables and properties, ported to `currency` and
      `form`;
    - a generated edit form: `US$ 1,284.5` saves 128450, and the next
      GET seeds `1284.50` with the same text in `<Name>.seed`;
    - a KWD record of 1.5 under `en`: the form saved unchanged keeps
      1500, and the same text typed into a create form is
      `money_ambiguous`;
    - a forged `<Name>.seed`, a seed on create, and a record changed
      between GET and POST each give `money_reenter` and store nothing;
    - a validation error on another field echoes `US$ 1,284.5`
      unchanged with an empty seed, and resubmitting it saves 128450;
    - an unchanged `1.500` on an English KWD edit form, with an error
      on another field: the echo keeps the seed, and the second
      submission saves 1.5 KWD; if the record changes between the two
      submissions, the second gives `money_reenter`;
    - `(5)` in a field without `Negative` is `money_negative`;
    - a `Strict` field gives today's errors for the same inputs;
    - the ISO code is rendered beside every money input, in the browser
      run of `field-money`;
    - `money_wrong_currency` names the field's code, and
      `money_ambiguous` both choices, in English and in one translated
      locale.

## 13. Follow-on work, out of scope

- Translating the framework's strings into more languages. The pack
  layout is ready to carry catalogs, but each new language is
  translation work, and it goes through copy review.
- `{{number}}` and dates reading the packs (decision 7).
- The change-currency operation.
- Admitting pre-2013 historical codes, which needs older pinned ISO
  editions.
- The `pt-PT` retag (§9).
- CLDR plural rules in the packs, which `Tf $L` uses to pick a plural
  form.
- A plain numeric field kind. It takes §5.1's reading with no currency
  spelling and its declared scale as the exponent, and has `Strict`.

## Open questions

None. The two left at round four, the event-log rename's scope and the
probe's form, were decided by Paul (decisions 19 and 20).

## Review log

**Round one** (15 findings): handled in the second revision. In round
three, Astra rated 1 and 5 resolved; 9 and 14 were still partial, and
are completed by round-three findings 1, 6 and 8.

**Round two** (7 findings): in round three, Astra rated 1-5 and 7
resolved and 6 partial (completed by round-three finding 8).

**Round two, as answered then:**

| # | Finding | Change |
|---|---|---|
| 1 | Mixed versions can multiply an amount | Stop-start upgrade and no rollback (decision 17, §8). Removing the legacy column makes an old binary fail loudly on SQL stores (shipped as add, copy, drop, §7; first drafted as a rename). Version markers. The detectability of each mix is stated, including the event-store case that cannot be detected. Changelog draft. |
| 2 | A stale create form can change denomination | `<Name>.currency` echo, compared on POST, mismatch rejected with the text kept, never authoritative. New key `money_currency_changed` (§5, §6). |
| 3 | Aliases drop regions; malformed tags accepted | The whole replacement applies field by field. RFC 5646 grammar is validated; malformed tags go to root. Expected-value tests for `swc`, `cnr`, `drw`, `prs` and 11 malformed tags (§3, §12). |
| 4 | Event versioning incomplete | Every new event carries `V: 2`. Unknown versions are rejected. Legacy amount-free updates need no `[legacy]`. Currency changes get their own event kind (§7). *Superseded in the confirmation round:* the round-two rules that rejected any update carrying a currency and any create lacking one were replaced by "a currency if and only if an amount" (see the confirmation-round tables). |
| 5 | Explanations were blanket exemptions | Every explanation is a per-row executable check, and the exemptions are narrow transformations in `cmd/check/exemptions.go`. An injected fr-CH defect fails (Appendix B). |
| 6 | Coverage not proved; categories wrong; parse failures ungated | Exact grid membership and order, a completion line, categories emitted once, parse failures classified and gated. A truncated run, a run without the trailer, and a reordered row each fail. Corrected numbers above. |
| 7 | Historical exclusion unsupported | One rule from pinned ISO 4217 editions, independent of CLDR display digits. SLL and 20 other withdrawn or fund codes stored. The "no app holds SLL" claim is removed (§1). |

**Round three:**

| # | Finding | Change |
|---|---|---|
| 1 | Pack independence broke the linked-parent fallback | Two maps: data parents stay inside packs, and CLDR fallback parents cross them. `proto/crosspacktest` checks hi-Latn → en-IN, ht → fr-HT, and nb and nn → no, each with its own pack absent (§3). |
| 2 | Packs conflated with offered locales | `app.toml` declares offered codes (`zh-Hans`, `pt-PT`) for routing, catalogs and templates. Packs are a derived, deduplicated set (`PacksFor`, tested) (§3). |
| 3 | The boot check accepted fallbacks and missed DefaultLocale-only apps | `CheckConfig` runs in `Serve` on the effective default and every offered code. Each must be well-formed, recognised and linked without any fallback. It covers apps without manifests, and it is tested (§3). |
| 4 | No concurrency contract for lazy sets | One `sync.Once` per offered locale in a map fixed at `New`, publication by the `Once`, errors cached and never retried. Race test with a mutation check (§10). |
| 5 | Cached functions had no lifetime | Only request-independent functions are cached, with stated precedence. Request functions are failing placeholders in cached sets and are bound only on `Clone`. Tests cover precedence, the app's icon and asset functions, template overrides, and that requests cannot see each other's values (§10). |
| 6 | Budget ignored the pristine trees | The real `pages.New` structure is measured: 2.62 MB with the default served, plus 1.20 MB per served locale. Budgets are stated from that (§10). |
| 7 | Event-store tripwire wrongly called impossible | The `eventlog.payload` rename is adopted, probed (16 ms on 200,001 events in the prototype; §8 has the shipped cost; old read, append and ingest fail), and costed. The global position is dropped, and replaced by a per-(stream, writer) snapshot rule (§8). |
| 8 | Empty ICU results bypassed the oracle | Unsupported tags are pinned (`ht`). Any other missing result fails, a pinned tag with a result fails, and oracle exceptions are reported. A blanked oracle fails with 696,870 missing results. |
| 9 | Doctor claimed what it cannot see | The app-side probe (`RASTRILLO_CHECK_CONFIG=1`) runs in the serving binary. Doctor runs it, and claims only what files show statically. A generated test covers what `gen/locales` imports (§3). |
| 10 | False English-symbol argument | Withdrawn: English writes `$`, `A$`, `CA$`. Reconsidered: the fallback past linked parents is now the app's default locale, matching how the page's words fall back. Root only applies outside an app (§3). |

**Round four** (Astra's verdict: not ready to plan):

| # | Finding | Change |
|---|---|---|
| 1 | The probe ran after the database and migrations | The probe runs at the top of `Resolve`, before `db.Open` in the scaffold and before anything in `Run`. `ProbeConfig` is the hook for other apps, and a `Serve` backstop refuses without listening. Fixed JSON fields, and exit 0, 1 or 2. It always exits and never serves, with the reason given. Prototype `proto.Probe` is tested (§3). |
| 2 | Locale functions could replace request placeholders | Placeholders are applied after the locale functions on every path. Reserved request names are refused at `New`. A locale map defining a request name is an initialization error. Tested (§10). |
| 3 | `Clone` bypassed cached initialization | One shared per-locale initializer for parse, `Execute` and `Clone`. The factory runs once per locale; a broken catalog fails once across 40 concurrent calls. Mutation-checked (§10). |
| 4 | The cache was public | `Set` removed. The API is `New`, `Execute` and `Clone`, and clones are built per call and owned by the caller. Tested by rebinding a clone (§10). |
| 5 | The default lost its offered identity | `Config.Default` is the declared identity, and `DefaultFormat` and `Formats` hold the formatting locales. `CheckConfig("zh-TW", …)` feeds `pages.New` and the catalogs in an integration test (§3). |
| 6 | Boot validation repaired malformed codes | `CanonicalIdentity` runs before `Resolve` and refuses whitespace, underscores, non-canonical case and duplicates, naming the canonical form. Tested (§3). |
| 7 | The no-money rollback left a false ledger | Apps without money no longer rename, so the path is gone. Apps with money roll back only by backup restore, which restores the ledger with the data. Upgrade, restore, upgrade is tested (§8). |
| 8 | The migration missed pre-ledger databases | The rename is a post-adoption migration in its own money-only set. Fresh, ledger-backed and pre-ledger databases are tested through `migrate.Apply`, and the marker's absence reproduces the refusal (§8). |

**Confirmation round** (six resolved, three gaps; verdict: not ready to
plan):

| Gap | Change |
|---|---|
| 1. The probe ran after `Run`'s `sidecar run` dispatch | The probe is the first statement of `Run`, before the sidecar dispatch, and of `Resolve` and `ProbeConfig`. `Serve`, `Handler`, `OpenDB` and `db.Open` guard and refuse before opening. Prototyped in `hack/templatemem/entry` (historical; it ships in `run.go` and `probe.go`, tested in `probe_test.go`), with tests whose sidecar and serve callbacks must never run, for plain, `serve`, `-db` and `sidecar run` invocations. Today's order, restored as a mutation, is caught (§3). |
| 2. Case was decided by subtag length | Case is decided by grammatical role: language and extlangs lower, script title, region upper, then variants, extensions and private use lower. Regression tests include `en-u-nu-latn`, `de-CH-1996`, `zh-Hant-TW`, `en-x-private`, their mis-cased forms, and `x-private` (§3). |
| Follow-up 1. The acceptance criteria still stated the old event rule | The test plan's event items now state the pairing rules: a currency if and only if an amount; an amount with its unchanged currency accepted; a changed currency only in its own event kind; historical creates with neither field accepted. §7 defines "unchanged" as equal after `Normalize` to the currency replay has derived so far. The whole spec was searched, and the round-two log row is marked superseded. |
| Follow-up 2. `replay` skipped deleted events before checking the version | The version is validated before the kind dispatch. `V` is a pointer, so an explicit 0 is refused and an absent `V` is legacy. Regression cases: a `V: 3` deleted event and an explicit `V: 0` are rejected, and a `V: 2` deleted event is fine. Restoring the old order fails both cases. |
| 3. Adding a money field made version-2 history unreplayable | One rule: a currency if and only if an amount. Pre-addition version-2 events carry no amount, so the field is empty and no currency is needed. A version-2 amount without a currency is still rejected. The replay check was prototyped in `hack/moneycldr/replay` (historical; it ships in `currency/replay`) and tested on pre-addition history followed by events that set the amount. In SQL the rule is only safe with nullable added columns, so the spec now requires them, and a boot check enforces the pairing (§1, §7). |

**Paul's §10 change** (after approving the spec): "Didn't we say we
wouldn't have one set of templates per language? The language strings
should be interpolated surely rather than one template per language."

| What | Status |
|---|---|
| The lazy per-locale template sets of round three (findings 4-6) and round four (findings 2-4): `sync.Once` per locale, the shared per-locale initializer, request-function placeholders, owned per-request clones, the 2.62 MB + 1.20 MB-per-locale budget | **Superseded** by one shared set with the localizer in the data (§10, decision 21). Those findings were about the per-locale cache's correctness, and that cache no longer exists. Their concerns that still apply carry over: no cross-talk between requests is the race test; nothing request-dependent is cached, because request values are data; app functions may not replace locale functions. |
| The per-request Clone pattern in today's docs | Removed (§11). |

**Round eight: Astra's review of the new §10** (12 findings; verdict:
not ready for planning):

| # | Finding | Change |
|---|---|---|
| 1 | The prototype re-parsed per page and called it one set | Stated precisely: the base is parsed once at boot, each page is one boot-time clone of it with its own definitions, and nothing is cloned per language or per request. Prototyped (`pages.New`) and re-measured on that structure. |
| 2 | `withL` rejected the localizer `opt` produced | One representation, `*Localizer`, everywhere. `{{$L := localizer $}}` replaces `opt`, which dereferenced it. The tests execute `field-daterange`'s `withL` map path and a `Localized` struct path. |
| 3 | Strictness was CI-only | Strict dispatch is the production default (`ErrNoLocalizer`, `ErrLegacyCall`). Compat is opt-in and logged. The codemod reports `{{template "p"}}` with no data. Mutation-checked. |
| 4 | The codemod regrouped pipelines and stopped at a quoted `}}` | It wraps the complete pipeline in parentheses, finds its end by a quote-aware scan, and validates by re-parsing and comparing parse trees before writing. Tests cover grouping, pipelines, and interpreted and raw strings containing `}}`. |
| 5 | `$L` detection ignored scope | The only accepted binding is the top-of-body prelude. Any other, at any depth, makes the file refused and unchanged, with the lines reported. Five refusal tests. |
| 6 | The legacy translator was the framework's | Compat uses the app's default-locale localizer. A Spanish-default app renders its app-only key in Spanish. |
| 7 | A nil value was mistaken for a missing localizer | The form is decided by the first argument's type (`*Localizer` or not). Nil-value tests compare against today's `ui.Funcs` output for `number` and `displayValue`. |
| 8 | Apps that never adopt `Pages` still break | A template preflight for every app: `doctor` runs the codemod's dry run, including a `go/parser` scan of function-map registrations and `ui.WithT`/`FuncsWith`. §8's no-money notes include it. |
| 9 | Decision 3 and §4 still described the old money call | Updated to `{{money $L amount code}}`. The supported legacy calls are listed explicitly, compat mode only (§10). |
| 10 | The corpus test only parsed | It executes migrated fixtures with production signatures, asserts escaping in HTML, attribute, URL, JS and `Tbdi` contexts and localized number and money output, and has negative fixtures that must fail. |
| 11 | The budget overreached | Template memory is separated from per-locale memory (1.4 KB of localizer + about 4.4 KB of catalog per locale), measured at 1 and 12 locales, with real catalogs and the real money function. Bounds are limited to the measured cases. |
| 12 | The binding/race explanation was wrong | Corrected: names are fixed at parse, implementations replaceable, and the risk is shared mutable binding, which can mix outputs without a data race. Isolation is asserted per output, not inferred from `-race`. |

**Round nine: Astra's confirmation of round eight** (10 of 12 resolved;
two partial, one new):

| # | Finding | Change |
|---|---|---|
| 4 (partial) | The validator rejected a pipeline the codemod had legitimately given `$L` | Wrapped pipelines are compared after normalising away the permitted insertions. Fixtures: `makeData (T "key")`, a nested `printf "%s" (Tf …)`, a pipeline form `.X \| pick (T "k")`, and a `dict` argument. Removing the normalisation fails all four. |
| 8 (partial) | Go detection missed aliases, dot-imports and constant keys | The ui import is resolved by path under any name (alias, dot-import), and a different package named `ui` is ignored. Keys are evaluated as package string constants with `go/constant`. Unresolvable keys are review items. Assignments are checked only on declared function maps. A fixture covers each case. |
| New (major) | The dry run passed with automatic edits pending | It exits 1 while any rewrite is pending, and `doctor` fails on any non-zero exit. The single-file `{{T "app.hello"}}` test: the dry run exits 1 and leaves the file unchanged, `-write` exits 0, and a second dry run exits 0. A Go finding alone also exits 1. |

**Round ten: Astra's confirmation of round nine** (the validator and
the dry-run exit resolved; Go detection still partial, with two false
negatives):

| Case | Change |
|---|---|
| A single-argument call treated as a conversion (`strings.ToUpper("t")`, `pick("x")`) | The syntax heuristics are gone. The check type-checks with `go/types`, and a key is resolved only when `types.Info.Types[key].Value` is a constant string. These are now review items. |
| A local shadowing a package constant (`keyAsset := "T"`) | The type checker resolves the identifier to the local variable, which is not constant, so it is a review item, not the shadowed constant's `"asset"`. |
| `make(template.FuncMap)` | Recognised by type: its index assignments are checked. |
| A function-computed key (`keyFor()`) | A review item. |
| A map passed through a helper | A helper's `FuncMap` parameter is checked by type, and a `map[string]any` reaching `Funcs` through a helper is tracked to the helper's parameter. |
| Also covered | Literal, constant, constant-expression, converted and other-package constant keys; text/template's `FuncMap`; aliased and dot-imported `WithT`/`FuncsWith`; not reported: another package called `ui`, a local variable named `ui`, an ordinary map, and a `map[string]any` that never reaches `Funcs`. All 23 expected findings, and every non-finding, are pinned in a fixture module. |
| Advisory | The spec states that the preflight is advisory, and that `Pages.New`'s refusal and strict dispatch are the guarantee. |

**Paul's amendment: normalised input** (decision 22; not yet reviewed):

| Asked | Change |
|---|---|
| Normalise by default, opt out per field | §5's two readings; `Field.Strict` keeps today's parser, tested through the field's POST path. |
| Accept exactly one plausible reading | §5.1's algorithm and decision table: three conventions, three groupings, and a value only when every reading agrees. |
| Strip white space and bidi marks | `Clean`, as before, now ahead of both readings. |
| Accept the field's own currency in any CLDR spelling | The ISO code, and the locale's and root's symbol and narrow symbol, either side. The packs now carry narrow symbols (Appendix A.4 re-measured). |
| Grouping in valid positions; the other decimal mark when unambiguous | The decision table. `de` `12.50` reads 12.50; `de` `1.500` for KWD is `money_ambiguous`. |
| Signs and parentheses only where negatives are allowed | Read by the parser, then `Negative` decides, as decision 12 already does. |
| Still reject another currency, ambiguity, excess decimals, junk | `money_wrong_currency`, `money_ambiguous`, `money_precision_N`, `money_invalid`. Nothing is rounded. |
| Re-seed after save; echo on error | §5.3. |
| Reuse errors; new ones are drafts | Seven reused. Two new drafts in §6, without em dashes, for copy review. |
| The test table and three properties | §12, `proto/normalize_test.go` and `proto/field_test.go`, mutation-checked; the properties' claims were narrowed after Astra's review. |

The first draft added one rule Paul's list left open: once the
locale's own reading could read the input, no other reading could take
its decimal mark for a group mark. Astra's review showed it picked a
value 1000 times smaller for pasted German input, and Paul withdrew it
(below).

**Astra's review of §5.1** ("not ready", seven findings), with Paul's
rulings:

| # | Finding | Change |
|---|---|---|
| 1 (high) | The decimal exception silently picks one of two plausible values; a stateless parser cannot tell a seed from the same text pasted | The exception is gone. Unchanged seeds are recognised by the form: `<Name>.seed` carries the text the server rendered, a byte-for-byte match after trimming keeps the record's value, and the seed is never a value. The server re-renders the record and compares; a mismatch or a seed on create is `money_reenter` (§5.3). Tests: unchanged in en and de, a forged seed, a seed in the wrong locale, a changed record, a seed on create. |
| 2 (high) | Shared symbols make "never another currency" untrue | Stated exactly (§5.1, step 2): a spelling the field's own currency uses in the locale or root is accepted even when shared, with the ISO code shown beside the input; any other explicit spelling or code is refused. The six families are listed and each is tested, with CAD and AUD. |
| 3 (high) | Discarding over-precise readings turns de `1.234` EUR into 1234 | Over-precise readings compete. Disagreeing readings are `money_ambiguous`; only over-precise readings are the precision error. de `1.234` EUR was ambiguous; decision 23 (below) later made the page's own group mark win, so it is 1234, and `en` `1.234` USD is the ambiguous case. |
| 4 (medium) | A decimal mark right after a group mark was accepted | No reading exists for it: `12,.50`, `1,234,.50`, `12.,50`, `12'.50`. A leading `.50` stays. |
| 5 (medium) | Indian grouping only in Indian locales | Indian grouping is a reading in every locale; equal values are one reading. `en` `1,23,456` is 123456. |
| 6 (medium) | The choices could still be misread | Choices carry the ISO code, no group separators, and never exactly three fraction digits (`1,2340 EUR`). A test reads each under every convention by ten reader locales and requires one reading, its own. |
| 7 (medium) | The properties cannot prove intent; strict equivalence compared a wrapper with itself | The claims are narrowed to consistency and redisplay. Intent rests on hand-written cases (122 locale, 44 shared-symbol, 17 field dispatch, 15 seed). `Strict` is tested through the field's whole POST path, not a wrapper. The corpus gained CAD, AUD, adjacent separators, doubled signs and sign-with-parentheses. |

**Astra's confirmation**, of the rulings and decision 23 below, found
all seven findings resolved and the
seed check sound, with one major gap: a failure re-render dropped the
seed, so an unchanged `1.500` KWD on an English edit form saved at the
first submission and was `money_ambiguous` at the second. The echo now
keeps a verified seed with its parsing locale, and every submission
re-verifies it against the current record (§5.3). The two-submission
test, with and without the record changing in between, covers it, and
dropping the echoed seed fails it.

**Paul's decision 23, after the rulings:** the page's own group mark
wins over the decimal reading (§5.1, step 5). It removes most refusals
of ordinary input (`en` `1,000` USD, `ja` `￥1,285`, `de` `1.500` EUR)
at a stated cost: a decimal-comma amount pasted into an English page is
read as thousands. The refused displays and seeds fell from 21,660 to
16,968, all in the decimal direction, and a test now asserts that
direction for every refusal. The zero-padding refinement below was
approved at the same time.

One refinement the rulings needed: zeros beyond the exponent are
accepted (`12.5000` is 12.50), and only a nonzero digit there is
over-precise. Without it the choice `1,2340 KWD`, the only way to write
1.234 KWD that no reader can take for 1234, could not be entered.
Nothing is rounded either way.

**Where this disagrees with a brief or review, with evidence:**

- **Rastrillo has no per-locale template sets today.** An earlier brief
  described `rastrillo.UI` holding one set per declared locale. No such
  type was ever committed; the real cost is per-request cloning. Astra
  confirmed this in round three.
- **Historical codes are not admitted wholesale.** Finding 7 asked to
  admit verified legacy codes such as SLL, and SLL is admitted. But 116
  codes withdrawn before 2013 have no pinned ISO source for their minor
  units, so they stay excluded as unverified, not guessed. Each can be
  admitted by pinning an older edition.
- **Following CLDR for symbol placement currently changes nothing.**
  None of the 22 side-flip locales is in the modern set. The decision
  is implemented for when one is.

## Appendix A: harness results

### A.1 Gallery locales

The values are ICU's. On every gallery-locale row of the grid, the
formatter's output equals ICU's. JPY and VND show 1284.5 rounded
because these samples used decimal input; stored amounts are whole and
never round.

Notation: `␣` U+00A0, `˽` U+202F, `[RLM]` U+200F, `[LRM]` U+200E.

| Locale, currency | 1284.50 | −1284.50 | 12,345,678.50 |
|---|---|---|---|
| en USD | `$1,284.50` | `-$1,284.50` | `$12,345,678.50` |
| es EUR | `1284,50␣€` | `-1284,50␣€` | `12.345.678,50␣€` |
| pt EUR | `€␣1.284,50` | `-€␣1.284,50` | `€␣12.345.678,50` |
| pt-PT EUR | `1284,50␣€` | `-1284,50␣€` | `12␣345␣678,50␣€` |
| ja JPY | `￥1,285` | `-￥1,285` | `￥12,345,679` |
| zh-Hans CNY | `¥1,284.50` | `-¥1,284.50` | `¥12,345,678.50` |
| yue HKD | `HK$1,284.50` | `-HK$1,284.50` | `HK$12,345,678.50` |
| ar SAR | `[RLM]1,284.50␣ر.س.[RLM]` | `[RLM][LRM]-1,284.50␣ر.س.[RLM]` | `[RLM]12,345,678.50␣ر.س.[RLM]` |
| hi INR | `₹1,284.50` | `-₹1,284.50` | `₹1,23,45,678.50` |
| bn BDT | `১,২৮৪.৫০৳` | `-১,২৮৪.৫০৳` | `১,২৩,৪৫,৬৭৮.৫০৳` |
| ru RUB | `1␣284,50␣₽` | `-1␣284,50␣₽` | `12␣345␣678,50␣₽` |
| vi VND | `1.285␣₫` | `-1.285␣₫` | `12.345.679␣₫` |
| ga EUR | `€1,284.50` | `-€1,284.50` | `€12,345,678.50` |
| en KWD | `KWD␣1,284.500` | `-KWD␣1,284.500` | `KWD␣12,345,678.500` |
| fr EUR | `1˽284,50␣€` | `-1˽284,50␣€` | `12˽345˽678,50␣€` |

x/text, for contrast, writes `$␠1,284.50`, `€␠1.284,50` and
`€␠-1.284,50`: always symbol first, then a breaking space, with the sign
inside.

### A.2 Every difference from ICU, explained per row

| Rows | Explanation (checked on each row) | Locales |
|---|---|---|
| 20,415 | ICU reports the locale but its string equals ours for a parent or the English hub | en-BE, en-CH, en-CZ, en-DE, en-DK, en-EE, en-ES, en-FI, en-FR, en-GE, en-HU, en-IT, en-LT, en-LV, en-NL, en-NO, en-PL, en-PT, en-RO, en-SE, en-SI, en-SK, en-UA |
| 3,132 | No shipped locale has the tag's script, and ours equals root | uz-AF, pa-PK (ordinary tags) |
| 1,404 | ICU's string, with its `$` decimal mark mapped to `,`, equals ours; every code except CVE | pt-CV |
| 723 | ICU's string, with U+0027 mapped to U+202F, equals ours | fr-CH |
| 9 | ICU's string, with `KM` mapped to `КМ`, equals ours; BAM only | sr-Cyrl-ME |

### A.3 Storage set (`hack/moneycldr/testdata/storage.tsv`)

153 eligible + 21 stored-only = 174 codes. The exclusions, with
reasons, are in §1's table. Eligible codes where CLDR displays fewer
digits than the exponent (AFN, ALL, COP, HUF, IDR, IQD, IRR, KPW, LAK,
LBP, MMK, PKR, RSD, SOS, SYP, YER) show the exponent's digits (decision
13).

### A.4 Pack sizes (`hack/moneycldr/testdata/packs.tsv`, `cmd/packsize`)

Measured for Task 29 on the shipped `locales/<lang>` packages, with
`cd hack/moneycldr && GOFLAGS=-mod=mod CGO_ENABLED=0 go run ./cmd/packsize`
(go1.26.5, linux/amd64). Two runs gave identical figures. Locales and
source bytes are `testdata/packs.tsv`'s; the core's is the size of
`locales/core_gen.go`. The baseline, a program that formats one amount
with only the core linked, is 3,250,050 bytes.

| Pack | Locales | Source bytes | Binary bytes added |
|---|---|---|---|
| en | 124 | 9,972 | 55,448 |
| ar | 28 | 6,612 | 21,920 |
| es | 28 | 4,531 | 22,088 |
| pt | 12 | 1,513 | 9,576 |
| zh | 10 | 1,456 | 9,560 |
| ru | 6 | 1,251 | 9,552 |
| yue | 5 | 1,006 | 9,517 |
| bn | 2 | 904 | 5,400 |
| hi | 2 | 886 | 5,392 |
| vi | 1 | 805 | 5,368 |
| ga | 2 | 715 | 1,296 |
| ja | 1 | 759 | 1,280 |
| **the 12 gallery packs together** | 221 | 30,410 | **134,613** |
| core (`locales/core_gen.go`) | root | 33,727 | (in the baseline) |
| **every pack** (101) | 429 | 115,454 | **484,798** |

The sum of single-pack deltas is 156,397, more than the combined
figure. The prototype's table (`en` +54,572, the twelve +137,069, every
pack +496,209) is superseded. Per-binary linker overhead decides which
way a sum of singles goes, so plan with the combined rows.

### A.5 `{{number}}` drift (first spike; not re-run; decision 7)

x/text writes es `1.284` where CLDR writes `1284`, and `ar` `٥٤٬١٧٣`
where CLDR 48 writes `54,173`. This stays until `{{number}}` moves onto
the packs.

## Appendix B: the harnesses

Implemented. The prototypes in `hack/templatemem`, `hack/moneycldr/proto`
and `hack/moneycldr/replay` were promoted and removed; the rows below
marked historical name where each went. `hack/moneycldr` remains as the
data generator, the ICU oracle and `cmd/packsize`.

`hack/moneycldr` is a nested module, so the root build, vet, budget and
staticcheck never enter it; `make gofmt` covers it.

| Path | What it is |
|---|---|
| `moneycldr/cldrsrc`, `cmd/fetch` | Fetches the 1,539 cldr-json files and 35 ISO editions (the latter through the Wayback Machine's unaltered `id_` URLs) and checks `sources/inputs.sha256`. It refuses any redirect that changes the capture timestamp and tries the other scheme instead. A re-fetch in round five was silently redirected to a later edition; the hash manifest caught it, and this fix makes the fetch exact. |
| `moneycldr/cmd/gen` | Writes `locales/core_gen.go`, `locales/<lang>/pack_gen.go` and `locales/modern` in the root module (the prototype wrote `table/` and `packs/` inside `hack/moneycldr`), and `testdata/storage.tsv` and `testdata/packs.tsv`. It applies the storage rule, refuses unsupported spacing rules and numbering systems, and checks that every locale rebuilds from its diffs. |
| `moneycldr/proto`, `proto/fallbacktest`, `proto/crosspacktest` (historical; now `currency`, `form`'s money field, `locales`, `locales/localestest/gallery` and `locales/localestest/crosspack`) | The reference formatter, strict parser, normalising parser (`normalize.go`, §5.1), the field's POST path with the seed check (`field.go`, §5.3), resolver, `PacksFor` and `CheckConfig`, and their offline expected-value and property tests, first with every pack linked, then with the 12 gallery packs, then with en, fr and no. |
| `moneycldr/cmd/check`, `oracle/intl.js`, `cmd/check/exemptions.go` | The ICU grid: exact membership, completion line, per-row explanations, gated parse failures. |
| `moneycldr/cmd/packsize`, `cmd/xtext` | Binary cost per pack, measured on the shipped `locales/<lang>` packages (Appendix A.4); the rejected x/text route. |
| `templatemem` (historical; deleted) | §10's measurements, on the scaffold's real templates (copied in `testdata/scaffold`) and the tickets example, including the real `pages.New` structure. |
| `templatemem/pages` (historical; now `ui/pages.go` and `ui/pages*_test.go`) | The `ui.Pages` prototype (base parsed once, one clone per page at boot, strict dispatch, opt-in compat) and its tests, run under `-race`: the executing corpus with escaping and localization, negative fixtures, compat on a Spanish-default app, nil values, isolation (mutation-checked), zero-allocation localizer lookup, `New`'s refusals, and the `zh-TW` integration with `CheckConfig`. |
| `templatemem/codemod`, `cmd/migrate-templates` (historical; now `internal/tmplmigrate` and `rastrillo migrate-templates`) | The template codemod, its validation and `$L` scope rules, the type-checked Go preflight (`gocheck.go`, with its fixture module `codemod/testdata/gocheckmod`), and the command `doctor` runs: rewrite cases, refusals, the no-data report, validation negatives, the repo corpus (53 files, 253 calls, 102 dicts, 11 review sites, 62 preludes; 255 calls on the real tree, §10) and the Go findings. |
| `templatemem/upgradeprobe` (historical; now `eventlog/money_test.go` and `currency/sqlmoney`) | §8's tripwires through the real `migrate.Apply`, `eventlog` and SQLite driver: three start points, an app without money, upgrade-restore-upgrade, and the post-adoption marker. |
| `moneycldr/proto/probe.go` (historical; now `locales/probe.go`) | The configuration probe's logic: fixed fields, exit codes. |
| `templatemem/entry` (historical; now `run.go`, `probe.go` and `probe_test.go`) | Where the probe sits: `Run`'s dispatch with the probe first, and the guard for database entry points. |
| `moneycldr/replay` (historical; now `currency/replay`) | The boot replay check's rule for money fields: a currency if and only if an amount; version 1, version 2 and unknown versions; checked rescale. |

The harness's checker was tested against bad inputs:
- a truncated oracle file fails ("no completion line after 1000 of
  698436 rows");
- a file without the completion line fails;
- a swapped row fails ("oracle row 5 is …, want the grid's …");
- an injected defect in one fr-CH row is reported as 1 unexplained
  difference, and the run fails.

## Appendix C: Approved copy

Approved by Paul in the copy review of 2026-10-12. Code uses these strings
verbatim; the ids are the contract. Section A's English is each
catalog key's value; its eleven translations are drafted afterwards and
are not reviewed.

A second review on 2026-10-12 approved the strings marked "added
2026-10-12" below, and revised three earlier ones (marked "revised
2026-10-12"). A revised row's text is the one to use.

### C.1 Form errors
| id | English (approved) |
|---|---|
| `rastrillo.ui.money_invalid` | Enter a valid amount. |
| `rastrillo.ui.money_precision` | Enter an amount with no more than 2 decimal places. |
| `rastrillo.ui.money_precision_0` | Enter a whole amount, with no decimal places. |
| `rastrillo.ui.money_precision_3` | Enter an amount with no more than 3 decimal places. |
| `rastrillo.ui.money_decimal_comma` | Use a comma for decimals, like 12,50. |
| `rastrillo.ui.money_decimal_point` | Use a point for decimals, like 12.50. |
| `rastrillo.ui.money_currency` | Enter the amount without a currency sign. |
| `rastrillo.ui.money_negative` | Enter an amount of 0 or more. |
| `rastrillo.ui.money_reenter` | Enter the amount again. |
| `rastrillo.ui.money_currency_changed` | The currency changed while you were editing. Check the amount and save again. |
| `rastrillo.ui.money_wrong_currency` | Enter the amount in {currency}. |
| `rastrillo.ui.money_ambiguous` | This could be {a} or {b}. Enter the one you mean. |
| `rastrillo.ui.money_ambiguous_precision` | This could be {b}. {a} has too many decimal places for {currency}. Enter the amount you mean. |

Revised 2026-10-12: `rastrillo.ui.money_currency`, wording unchanged; it
now appears only on `Strict` fields. Normalising fields use
`money_wrong_currency` instead.

Added 2026-10-12: `money_wrong_currency` (`{currency}` is the field's ISO
code), `money_ambiguous` (`{a}` and `{b}` are the smaller and larger
readings, each with the ISO code) and `money_ambiguous_precision`, used
instead of `money_ambiguous` when one reading has more decimal places
than the currency allows (`{a}` is that reading, `{b}` the other).

### C.2 Operator messages
| id | English (approved) |
|---|---|
| `op.config.prefix` | `locale configuration:` |
| `op.config.empty` | `an empty locale code` |
| `op.config.whitespace` | `%q contains white space; write %q` |
| `op.config.not_canonical` | `%q is not in canonical form; write %q` |
| `op.config.malformed` | `%q is not a well-formed language tag` |
| `op.config.duplicate` | `%q is offered twice` |
| `op.config.unknown` | `%q is not a locale CLDR's modern set formats` |
| `op.config.missing_pack` | `%q needs the %s pack: import _ "amadan.net/rastrillo/rastrillo/locales/%s"` |
| `op.config.default_not_offered` | `default locale %q is not one of the offered locales %v` |
| `op.packs.malformed` | `offered locale %q is not a well-formed language tag` |
| `op.packs.unknown` | `offered locale %q is not a locale CLDR's modern set formats` |
| `op.locales.default_not_linked` | `locales: default locale %s is not linked` |
| `op.probe.bad_value` | `RASTRILLO_CHECK_CONFIG=%q: set it to 1 to check the configuration, or unset it to serve` |
| `op.probe.done` | `RASTRILLO_CHECK_CONFIG=1: checked the configuration and exited without serving; unset it to serve` |
| `op.guard` | `RASTRILLO_CHECK_CONFIG is set but %s was reached before the probe ran: call rastrillo.ProbeConfig (or Resolve, or Run) before opening the database; nothing was opened` |
| `op.boot.refused` | `rastrillo: refusing to start: %v` |
| `op.currency.missing_pack` | `currency: %s needs the %s pack, which is not linked; amounts are formatted in %s instead. Import _ "amadan.net/rastrillo/rastrillo/locales/%s"` |
| `op.currency.malformed` | `currency code is not three ASCII letters` |
| `op.currency.unsupported` | `currency code is not in the storage set` |
| `op.currency.not_integer` | `amount is not an integer count of minor units` |
| `op.currency.not_integer_type` | `amount is not an integer count of minor units: got %T` |
| `op.currency.out_of_range` | `amount does not fit in int64` |
| `op.legacy.undeclared` | `%s has %d rows from before money had a currency. Add [legacy] currency and scale to manifest/%s.toml and run rastrillo generate.` |
| `op.legacy.inexact` | `%d at scale %d is %s, which %s cannot hold` |
| `op.legacy.overflow` | `%d at scale %d is too large for %s` |
| `op.legacy.failed` | `%s.%s: %d rows cannot be converted, and nothing was changed:` |
| `op.legacy.row` | `row %d: %s` |
| `op.legacy.more` | `and %d more` |
| `op.legacy.pairs` | `%s.%s: %d rows have an amount without a currency or a currency without an amount; fix them before starting` |
| `op.legacy.newer` | `this database's money data is version %d, and this binary understands version 2. Run the newer version, or restore the backup taken before it ran.` |
| `op.legacy.scale_missing` | `manifest/%s.toml: [legacy] needs scale, the number of decimal places the old integers carry (2 for cents, 0 for whole units)` |
| `op.legacy.currency_missing` | `manifest/%s.toml: [legacy] needs currency, the currency the old integers were in` |
| `op.legacy.added_columns` | `%s.%s: added %s_minor and %s_currency; existing rows have no amount` |
| `op.legacy.drop_blocked` | `%s.%s cannot be replaced by %s_minor because %v; remove that index, view or trigger and start again` |
| `op.legacy.bad_name` | `%q is not a table or column name the money upgrade can use: use only a-z, 0-9 and _` |
| `op.replay.version` | `event %d: version %d is not one this binary understands` |
| `op.replay.currency_no_amount` | `event %d: %s has a currency but no amount` |
| `op.replay.amount_no_currency` | `event %d: %s has an amount but no currency` |
| `op.replay.currency_change` | `event %d: %s changes currency %s to %s: a currency change needs its own event` |
| `op.replay.v1_currency` | `event %d: a version-1 event carries a currency for %s` |
| `op.replay.v1_needs_legacy` | `event %d: a version-1 amount for %s needs [legacy] in the manifest` |
| `op.replay.after_upgrade` | `%s: an event from writer %s (seq %d) has no version but was written after the money upgrade, so a currency-unaware version wrote it` |
| `op.eventlog.both` | `eventlog: the table has both payload and payload_v2; restore the backup taken before the upgrade` |
| `op.eventlog.upgrade_refused` | `eventlog: could not rename payload for money: %v. If this app keeps its tables in Options.Migrations, apply eventlog.Schema with migrate.Apply instead; see the eventlog reference` |
| `op.replay.half_clear` | `event %d: %s clears the amount or the currency but not both` |
| `op.replay.field_error` | `event %d: %s: %w` |
| `op.replay.bad_version` | `event %d: the version is not a whole number` |
| `op.replay.v1_null` | `event %d: a version-1 event has a null amount or currency for %s` |
| `op.replay.stream` | `%s: %v` |
| `op.boot.not_booted` | `%s: gen.Boot has not run for this database; call it after the schema is applied and before serving` |
| `op.boot.wrong_db` | `%s: this request's database is not the one gen.Boot checked; pass the same *sql.DB to gen.Boot and to the router's contexts` |
| `op.generate.migration_appended` | `%s: appended %s to gen/store/%s/migrations.go; released statements are never edited` |
| `op.eventlog.neither` | `eventlog: the table has neither payload nor payload_v2; apply eventlog.Schema first` |
| `op.generate.no_currency` | `manifest/%s.toml has a money field but no currency; add currency = "EUR" (or another ISO 4217 code), the default for new records` |
| `op.generate.bad_currency` | `manifest/%s.toml: currency %q: %v` |
| `op.generate.ineligible` | `warning: manifest/%s.toml: %s is still accepted for storage but is no longer issued; choose a current currency for new records` |
| `op.generate.legacy_currency` | `manifest/%s.toml: [legacy] currency %q: %v` |
| `op.generate.legacy_scale` | `manifest/%s.toml: [legacy] scale %d must be from 0 to 18` |
| `op.generate.default_locale_removed` | `--default-locale was removed: the default comes from default_locale in manifest/app.toml` |
| `op.catalog.missing` | `manifest/app.toml offers %q, so locales/%s.toml is required: %s` |
| `op.catalog.reason_no_framework` | `the framework's own strings are not translated into it` |
| `op.catalog.reason_app_keys` | `this app has its own strings, and they are in English` |
| `op.packs.test_mismatch` | `gen/locales links %v, manifest/app.toml needs %v: run rastrillo generate` |
| `op.generate.pruned` | `removed %s: generated by an earlier run, and nothing generates it now` |
| `op.generate.stale` | `%s was generated by an earlier run and nothing generates it now; run rastrillo generate` |
| `op.generate.reserved_app` | `manifest/app.toml holds the app's locales, so no resource can be named "app"` |
| `op.generate.money_schema_added` | `%s keeps money in events, so the next deploy renames the event log's payload column. Take a backup, stop every running copy, then start the new version. Never run old and new versions side by side. To go back, restore the backup.` |
| `op.generate.format_failed` | `%s %d: %s in %s` |
| `op.form.bad_currency` | `form: field %q: currency %q: %v` |
| `op.pages.no_localizer` | `ui: no localizer` |
| `op.pages.nil_localizer` | `%s called with a nil $L (the data reached this template without "L")` |
| `op.pages.page_data` | `in the page data for %q` |
| `op.pages.legacy_call` | `ui: legacy locale call; run rastrillo migrate-templates` |
| `op.pages.legacy_func` | `%s without $L` |
| `op.pages.compat_on` | `ui: COMPATIBILITY MODE: legacy locale calls and missing localizers render in the default locale %q instead of failing; finish rastrillo migrate-templates and turn it off` |
| `op.pages.compat_call` | `ui: COMPATIBILITY MODE: a legacy or localizer-less %s call rendered in the default locale` |
| `op.pages.reserved_func` | `ui: app function %q would replace a framework function` |
| `op.pages.offered_twice` | `ui: %q offered twice` |
| `op.pages.default_not_offered` | `ui: default %q is not offered` |
| `op.pages.no_format` | `ui: %q has no formatting locale; pass locales.CheckConfig's result` |
| `op.pages.wrap` | `ui: %s: %w` |
| `op.pages.base` | `ui: base: %w` |
| `op.pages.no_page` | `ui: no page %q` |
| `op.funcs.arity` | `%s: %d arguments, want %d` |
| `op.funcs.arg_type` | `%s: argument %d is %T, want %s` |
| `op.money.first_arg` | `money: the first argument must be $L (money has no legacy form)` |
| `op.check.side_flip` | `a modern locale now moves the symbol for letter-final symbols (%v); decision 6 applies (follow CLDR); review the oracle's side-flip explanation` |
| `op.money.arity` | `money: want $L, an amount and a currency; got %d arguments after $L` |
| `op.doctor.probe_failed` | `the configuration probe failed (exit %d): %s` |
| `op.doctor.packs_stale` | `manifest/app.toml offers %v but gen/locales/packs.go was generated for %v; run rastrillo generate` |
| `op.doctor.heading_templates` | `Templates:` |
| `op.doctor.heading_locales` | `Locales:` |
| `op.new.locale_unsupported` | `rastrillo new --locale %q: the framework's own strings ship in %s. Start with one of those, then add your language to manifest/app.toml` |
| `op.migrate.gen_stale` | `%s holds generated templates in the old syntax; run rastrillo generate` |
| `op.migrate.file_line` | `%-64s calls %3d  dicts %2d  wrapped %2d  preludes %2d` |
| `op.migrate.io_error` | `migrate-templates: %v` |
| `op.migrate.mode_dry` | `dry run` |
| `op.migrate.mode_written` | `written` |
| `op.migrate.summary` | `%s: %d templates; %d locale calls, %d dict calls, %d wrapped calls, %d preludes` |
| `op.migrate.refused` | `refused: %s` |
| `op.migrate.review` | `review (if this data is a struct, it needs an L field): %s` |
| `op.migrate.no_data` | `no data: %s` |
| `op.migrate.go_label` | `go` |
| `op.migrate.go_review_label` | `go (review)` |
| `op.migrate.go_line` | `%s: %s: %s; %s` |
| `op.migrate.pending` | `pending: %d templates need the automatic rewrite; run with -write` |
| `op.migrate.conflict` | `%s: $L already has another meaning, so the file is left unchanged: %s` |
| `op.migrate.conflict_site` | `line %d: $L %s %s` |
| `op.migrate.no_data_site` | `%s: {{template %q}} passes no data, so %q gets no localizer` |
| `op.migrate.review_site` | `%s: template %q %s` |
| `op.migrate.not_validated` | `%s: the rewrite did not validate, so nothing was changed: %w` |
| `op.migrate.lacks_l` | `%s lacks $L` |
| `op.migrate.lacks_l_key` | `%s lacks "L"` |
| `op.migrate.data_without_l` | `%s passes data without $L` |
| `op.migrate.no_prelude` | `template %s uses $L without the prelude` |
| `op.migrate.wrapped_changed` | `wrapped pipeline changed: %s` |
| `op.migrate.raw_string` | `unterminated raw string at byte %d` |
| `op.migrate.no_delimiter` | `no closing delimiter after byte %d` |
| `op.migrate.typecheck` | `type-checking %s: %s` |
| `op.migrate.go_not_constant` | `%s key %s is not a constant` |
| `op.migrate.go_not_constant_fix` | `make it a constant, or check by hand that it can never be T, Tf, Tbdi, number, displayValue, dateWords or money` |
| `op.migrate.go_entry` | `%s %q` |
| `op.migrate.go_entry_fix` | `remove it: the localizer's catalog now serves %s; give that catalog to ui.Pages as the locale's T` |
| `op.migrate.go_kind_entry` | `function map entry` |
| `op.migrate.go_kind_assignment` | `function map assignment` |
| `op.migrate.go_with_t` | `ui.%s (a per-request T binding)` |
| `op.migrate.go_with_t_fix` | `render through ui.Pages with the request's localizer in the data; drop the per-request Clone` |
| `op.eventlog.no_boundary` | `eventlog: this database has no eventlog_upgrade table, so the money upgrade has not run on it` |
| `op.render.not_a_view` | `page %q: the data has no request values, because it is not a generated view; nothing was rendered` |
| `op.doctor.probe_skipped` | `skipped the configuration probe: this app's rastrillo is %s, and the probe needs %s or later; upgrade, then run doctor again` |
| `op.doctor.probe_timeout` | `the configuration probe did not finish within %s and was stopped` |
| `op.doctor.probe_build_failed` | `could not build %s to run the configuration probe: %v` |
| `op.doctor.no_command` | `no cmd/%s/main.go to run the configuration probe against; nothing was checked` |

Added 2026-10-12: `op.eventlog.no_boundary`, `op.render.not_a_view`, `op.doctor.probe_skipped`, `op.doctor.probe_timeout`, `op.doctor.probe_build_failed`, `op.doctor.no_command`.

### C.3 Changelog

`changelog.money`, the entry at the top of the next release in `CHANGELOG.md`:

```markdown
**Multi-currency money (breaking upgrade for apps with money).** Money
is now stored with its currency, and the locale data that formats it
comes from language packs.
1. Run `rastrillo doctor`. It lists every template and Go file the new
   template calls affect, and what to change in each. Run
   `rastrillo migrate-templates -write` on your templates, make the Go
   changes it lists, and run `rastrillo doctor` again until it reports
   nothing. Every app needs this step, whether it stores money or not.
2. Run the new version with `RASTRILLO_CHECK_CONFIG=1`. It checks the
   locale setup, prints what it found, and exits without opening the
   database. If it names a missing pack: generated apps run
   `rastrillo generate`; other apps add the import it names.
3. For each money resource with existing data, add `[legacy] currency`
   and `scale` to its manifest and run `rastrillo generate`. The app
   refuses to start without them.
4. If the app stores money: take a backup, stop every running copy,
   then start the new version. Never run old and new versions side by
   side. To go back, restore the backup. Older versions refuse to run against the upgraded database: each money column is replaced by `<name>_minor` and `<name>_currency`, and in apps that keep money in events the event log's payload column is renamed, so an old version finds a column missing and fails instead of saving wrong amounts.
5. Apps that store no money need no backup or stop-start beyond the
   template changes in step 1.
```

Revised 2026-10-12: the last sentence of step 4 is now
`changelog.money.step4` (below), in place of the first review's sentence
saying the money columns "are renamed".

`changelog.money.step4` (revised 2026-10-12):

```markdown
Older versions refuse to run against the upgraded database: each money column is replaced by `<name>_minor` and `<name>_currency`, and in apps that keep money in events the event log's payload column is renamed, so an old version finds a column missing and fails instead of saving wrong amounts.
```

`changelog.money.normalising` (added 2026-10-12), the paragraph that
follows the numbered steps:

```markdown
Money fields now accept amounts the way people type them: `US$ 5`,
`5 USD`, `1.284,50` on an English page. They still refuse another
currency, an amount that could mean two things, and extra decimals.
To keep the old, exact format for a field, set `Strict: true`.
```

`changelog.money.columns`:

```markdown
Each money column `<name>` is replaced by `<name>_minor` and `<name>_currency`.
```

### C.4 Gallery
| id | English (approved) |
|---|---|
| `gallery.money.title` | Money |
| `gallery.money.body` | Store each amount as a whole number of the currency's smallest unit, such as cents, with its currency code beside it. Show it with `money`, which writes it the way the reader's language does. The currency always comes from your data, never from the reader. |
| `gallery.field_money.blurb` | A money amount in one currency. The currency code shows beside the label and is sent with the form, so a currency that changed while someone was typing is caught on save. Pair it with form.Field's Currency on the server. |
| `gallery.field_money.label` | Ticket price |
| `gallery.field_money.hint` | For example, 12.50 or €12.50. |

Revised 2026-10-12: `gallery.field_money.hint` was "Without the currency
sign.", which became wrong advice once the field accepts its own
currency's sign.

### C.5 Command-line help and package docs
| id | English (approved) |
|---|---|
| `flag.gen.root` | `the root module's locales directory, where the generated data is written` |
| `flag.migrate.write` | `rewrite the templates (without it, report what would change and change nothing)` |
| `usage.migrate_templates` | `migrate-templates [-write] PATH...    move templates to the localizer-first syntax; exits 1 while anything is left to do` |
| `flag.new.locale` | `the one language the app starts with (default en); one of: %s` |
| `usage.generate` | `generate [--check] [DIR]    write gen/ from manifest/; --check verifies without writing` |
| `pkgdoc.locales` | the text below, as the comment lines of `locales/doc.go` |
| `pkgdoc.pack` | `Package %s links CLDR money-formatting data for every modern "%s" locale. Import it for its side effect.` |
| `pkgdoc.modern` | `Package modern links every language pack: all CLDR modern-coverage locales. Import it for its side effect.` |
| `pkgdoc.currency` | `Package currency formats, seeds and parses money: an integer count of a currency's minor unit plus its ISO 4217 code, written in a reader's locale from CLDR data.` |
| `pkgdoc.replay` | `Package replay holds the money rules generated event stores replay with: a currency if and only if an amount, and versions checked before kinds.` |
| `pkgdoc.sqlmoney` | `Package sqlmoney upgrades generated SQL money columns once at boot and refuses to start on rows that break "a currency if and only if an amount".` |
| `pkgdoc.localestest_gallery` | `Package gallery links only the twelve gallery packs, so its tests see what an app offering those languages sees: a de-AT request with no de pack names the missing pack and falls back.` |
| `pkgdoc.localestest_crosspack` | `Package crosspack links only the en, fr and no packs, to check that a locale whose CLDR parent is in another pack falls back to that linked parent when its own pack is absent: hi-Latn to en-IN, ht to fr-HT, nb and nn to no.` |
| `usage.new` | `rastrillo new [--locale=<code>] <name>        scaffold a new app in ./<name>, offering one language (default en)` |
| `usage.doctor.exit6` | `6  templates still use the old syntax: run rastrillo migrate-templates` |
| `usage.doctor.exit7` | `7  the app's locale configuration would stop it starting` |

Added 2026-10-12: `usage.new`, `usage.doctor.exit6`, `usage.doctor.exit7`.

`pkgdoc.locales`:

```
Package locales is the locale-data core: the registry language packs
add themselves to, CLDR's root locale, and the lookup tables every
resolution needs (core_gen.go). A language's formatting data lives in
its own pack, locales/<lang>, which an app links with a blank import.
Go links only imported packages, so a language nobody imports costs
nothing; locales/modern imports them all.

The packs sit beside the framework's translated catalogs
(locales/<code>.toml) so a later pack can carry its language's
strings and number and date data in the same record, with no second
registry and no second import per language.

Generated by hack/moneycldr/cmd/gen from cldr-json 48.0.0 and pinned
ISO 4217 editions; `make money-cldr` regenerates and fails on any
difference. Data © Unicode, Inc., under the Unicode License v3
(LICENSE-UNICODE in this directory).
```

### C.6 Comments in files written into an app
| id | English (approved) |
|---|---|
| `gen.packs.doc` | `Package locales links the formatting packs manifest/app.toml's locales need. Wire DefaultLocale and Locales into rastrillo.Options.` |
| `gen.packs.default` | `DefaultLocale is manifest/app.toml's default_locale.` |
| `gen.packs.locales` | `Locales is manifest/app.toml's locales. It is nil when the app offers one locale, so a one-locale app gets no /xx/ prefixes and no switcher.` |
| `gen.packs.test` | `The packs this package imports are exactly the ones its offered locales need. generate --check proves this file matches manifest/app.toml; the RASTRILLO_CHECK_CONFIG probe proves main links this package.` |
| `gen.boot.doc` | `Boot upgrades this app's money data once and checks it, then returns the value Router needs. Call it after the schema is applied and before serving; a non-nil error must stop the start.` |
| `gen.boot.booted` | `Booted proves Boot ran for this database. Router takes it so an app cannot serve without it.` |
| `gen.template.contract_l` | `L  *ui.Localizer, required: set by the action from the request; ui.Pages' middleware puts it there` |
| `gen.template.contract_money` | `Money  map[string]form.MoneyInput, required for money fields: the text, the locale it is written in, and the currency, per field` |
| `scaffold.app_toml` | `The locales this app offers. Add a code to offer another language, then run rastrillo generate; with two or more, URLs gain /xx/ prefixes and pages a language switcher.` |
| `scaffold.app_go.packs_import` | `Links the formatting packs manifest/app.toml declares. It is here, not in main, so the app's tests link them too: boot refuses a configuration whose packs are missing.` |
| `scaffold.main.locales` | `Locales and DefaultLocale come from manifest/app.toml, through gen/locales. Boot refuses a configuration whose packs this binary does not link.` |
| `scaffold.render.pages` | `pages is every page of this app, built once at boot: ui's partials and layout.html parsed once, then one copy per page with that page's own file. Every language and every request runs these same templates; the localizer in each page's data decides the language.` |
| `scaffold.render.render` | `render adds the request's localizer, the language switcher's items and the current path to data, then executes name into a buffer before anything touches the wire: a template error becomes a clean 500 instead of half a page.` |
| `scaffold.render.error_page` | `It finds the request's language with pages.LocalizerFor rather than the middleware, because a recovered panic reaches it outside the middleware.` |
| `gen.template.contract_money_seed` | `Money  map[string]form.MoneyInput, required for money fields: the text, the locale it is written in, the currency, and the seed (the text the server rendered, sent back so an unchanged edit is recognised), per field` |

Added 2026-10-12: `gen.template.contract_money_seed`, which supersedes
`gen.template.contract_money` in generated templates: it adds the seed
that `MoneyInput.Seed` carries.

### C.7 The scaffold's AGENTS.md
| id | English (approved) |
|---|---|
| `scaffold.agents.money` | Money is a whole number of the currency's smallest unit, stored with its ISO 4217 code beside it, and shown with `money`. A float never touches a value a person will be held to. |

### C.8 DESIGN.md, the changelog's API list, and the doc comments and scaffold text Task 28 does not write

Approved by Paul in the copy review of 2026-10-13. Task 28 writes the
docs site pages (Appendix C of this section is not those; see
`docs/site/`) and records this section for Task 29, which writes each
piece into the file its own key names: `design.money` into `DESIGN.md`
(replacing `DESIGN.md:315-324`), `changelog.api` as one bullet list in
`CHANGELOG.md`'s new release section (after `changelog.money` and
`changelog.money.normalising`), `doc.funcswith` and `doc.witht` as the
doc comments above `ui.FuncsWith` and `ui.WithT` in `ui/funcs.go`, and
`scaffold.manifest_readme` and `scaffold.readme_render` into the
`manifest/README.md` and `README.md` text `cmd/rastrillo/new.go`
writes into a scaffolded app.

`design.money`, replacing `DESIGN.md:315-324` (matches decisions 1, 2,
5, 13 and 21):

```markdown
The rule is met. `{{number $L .Count}}` groups a quantity and
`{{money $L .Amount .Currency}}` writes money, both on the server, in
the reader's language, from CLDR data. In the browser, `Intl` is at
most an enhancement.

Money has two rules of its own. The currency always comes from the
data, never from the reader: an amount is stored as a whole number of
its currency's smallest unit, with its ISO 4217 code beside it, so a
price in euros is in euros on every page. And every digit of that
smallest unit is shown: 1.5 dinars is `KWD 1.500`, never `KWD 1.5`.

Each language's formatting data is a pack an app links only when it
offers that language, so a language costs nothing until then.
Templates are built once, and the localizer in each page's data
chooses the language, so one request's language never reaches
another's page.
```

`changelog.api`, one bullet each, in this order, as one list in
`CHANGELOG.md`'s new release section:

```markdown
- New package `currency` formats, seeds and parses money: an integer
  count of a currency's smallest unit with its ISO 4217 code, written
  in the reader's language from CLDR data.
- New packages `currency/replay` and `currency/sqlmoney` hold the
  rules generated stores use to replay money events and to upgrade and
  check SQL money columns.
- New package `locales` checks an app's languages at boot
  (`CheckConfig`), resolves a request's language to linked data with
  fallback (`Resolve`), and runs the configuration probe.
- New packages `locales/<lang>`, one per base language, link that
  language's CLDR money data when imported; `rastrillo generate`
  writes the ones an app needs to `gen/locales/packs.go`.
- New package `locales/modern` imports every language pack, for a tool
  that formats in any language.
- `form.Field` gains `Currency`, `Negative`, `Locale`, `LocaleEcho` and
  `Strict` for money fields, and `Stored`, the record's amount on an
  edit form.
- New type `form.MoneyInput` is what a money input shows: the text,
  its language, the currency and the seed.
- New function `form.SeedMoney` fills a money input from a stored
  amount, or from none.
- New method `(*form.Parsed).Minor` reads a money field's amount in
  its currency's minor units, and whether there is one.
- New method `(*form.Parsed).MoneyInput` gives back a money field's
  submitted text after a failed POST, or says to fill it from the
  record.
- New method `(*form.Parsed).ErrorArgs` holds the values each field's
  error message takes, for `Tf`.
- New type `ui.Pages`, built with `ui.NewPages`, parses an app's
  templates once at boot and renders every language and request from
  them, strict by default.
- New type `ui.Localizer` is one language's catalog and formatting
  locale: the `$L` every locale function now takes first.
- New type `ui.Localized` carries a page's localizer, switcher items
  and return path; embed it in a view struct.
- New function `ui.ForRequest` returns the `ui.Localized` for a
  request.
- New template functions: `money` writes an amount, `localizer` finds
  the page's `$L`, and `withL` passes it to a partial.
- New partial `field-money` draws a money input with its currency code
  in the label and three hidden inputs.
- New migration set `eventlog.MoneySchema` renames the event log's
  `payload` column to `payload_v2`, for an app whose events carry
  money; it takes about half a second per 200,000 events.
- New function `eventlog.UpgradeForMoney` applies `MoneySchema` with or
  without a migration ledger; generated apps call it from `gen.Boot`.
- `eventlog.Open` now fails when the event log table does not exist
  yet: apply `eventlog.Schema` first.
- Generated apps with manifest resources get `gen.Boot`, which
  upgrades and checks their money data, and `gen.Router` now takes its
  result.
- New function `rastrillo.ProbeConfig` runs the
  `RASTRILLO_CHECK_CONFIG=1` probe, for a `main` that calls neither
  `Run` nor `Resolve`.
- New function `rastrillo.FormatLocale` is the CLDR locale a request's
  amounts are written in.
- New method `(*rastrillo.Locales).Negotiate` picks a request's
  language without the middleware, for an error page.
- New functions `rastrillo.RetagOf` and `rastrillo.Retagged` list the
  codes the framework moved: Portuguese is now `pt-PT`, and `pt`
  addresses, cookies and catalogs keep working.
- New command `rastrillo migrate-templates` adds `$L` to an app's
  templates and lists what is left to do by hand.
- New flag `rastrillo new --locale` picks the one language a new app
  starts with.
- `rastrillo doctor` now checks templates and locales too, and exits 6
  for templates in the old syntax and 7 for a locale configuration
  that would stop the app starting.
- Removed: `rastrillo generate --default-locale`. The default now
  comes from `default_locale` in `manifest/app.toml`.
```

`doc.funcswith`, the doc comment above `func FuncsWith` in `ui/funcs.go`:

```
FuncsWith is Funcs(WithT(t)): the T entry replaced, everything else the
framework default.

It remains for an app that keeps its own rendering until it moves to
ui.Pages. Binding T on a per-request Clone of the templates is no
longer the recommendation: build the pages once with ui.Pages, and let
the localizer in each page's data choose the language. ui.Pages does
not take these options.

It also binds icon and iconAssets back to the framework's own set. An
app with its own icons passes both seams every time,
ui.Funcs(ui.WithT(t), ui.WithIcons(icons.Icon, icons.Assets)), or its
icons revert to Lucide on every request with no error.
```

`doc.witht`, the doc comment above `func WithT` in `ui/funcs.go`:

```
WithT replaces T and Tf for a locale call without $L: how an app with
its own rendering gets ui's partial defaults in the request's language.
It remains until the app moves to ui.Pages, where the localizer in each
page's data does this. See FuncsWith.

It replaces Tf as well, with the same function: a translator already
takes args (rastrillo.Tf's own shape), so binding only T would give an
app its own wording for every plain default and the framework's English
for the one sentence that interpolates a value.
```

`scaffold.manifest_readme`, the scaffold's `manifest/README.md`, shown
for an app named `shop` (`%[1]s` is the module name, `%[2]s` the
package name, in `cmd/rastrillo/new.go`'s own template syntax). Step 6
mounts the generated router inside the chi router `Router` returns,
not on the outer static-files mux `Mux` builds, which is the fix: the
earlier draft's step 6 mounted it on that outer mux, outside
`pages.Middleware`, so a generated screen never got the request's
localizer.

`scaffold.manifest_readme.step4`:

```markdown
4. Add the generated screens to the pages render.go builds, and give
   the generated actions their Ctx.Render seam. A generated action calls
   Ctx.Render with a page name that is always "<resource>/list",
   "<resource>/show", "<resource>/form" or "<resource>/confirm". In
   internal/shop/render.go's newPages, after the hand pages, add:

       gen, err := fs.Sub(shopassets.GenTemplatesFS, "gen/templates")
       if err != nil {
           return nil, err
       }
       genFiles, err := fs.Glob(gen, "*/*.html")
       if err != nil {
           return nil, err
       }
       for _, f := range genFiles {
           files[strings.TrimSuffix(f, ".html")] = ui.PageFile{FS: gen, Path: f}
       }

   and Catalog: genlocales.BaseCatalog to its ui.PagesConfig, so the
   generated strings are found. Import "io/fs", "strings" and
   shopassets "shop". Then add the seam:

       // GenRender is the Ctx.Render seam every generated action calls.
       func GenRender(ctx *rastrillo.Ctx, w http.ResponseWriter, name string, status int, data any) {
           var buf bytes.Buffer
           if err := pages.Execute(&buf, name, data); err != nil {
               if ctx != nil && ctx.Logger != nil {
                   ctx.Logger.Error("shop: rendering "+name, "err", err)
               }
               http.Error(w, "something went wrong", http.StatusInternalServerError)
               return
           }
           w.Header().Set("Content-Type", "text/html; charset=utf-8")
           w.WriteHeader(status)
           buf.WriteTo(w)
       }

   A generated view carries the request's localizer, switcher items and
   return path, so the layout renders it as it renders a hand page.
   When a generated screen needs this request's flash or signed-in
   state, make GenRender a func(r) returning a rastrillo.RenderFunc
   closed over r, and call it from the ctx factory: RenderFunc carries
   no *http.Request of its own.
```

`scaffold.manifest_readme.step5`, "the ledger trap" paragraph:

```markdown
   generate never edits a statement in gen/store/<name>/migrations.go
   once it has been released. Adding a field appends a statement, and
   genSchemaSet gives it the next ID, so the ledger applies it as a new
   migration.
```

`scaffold.manifest_readme.step6`:

```markdown
6. Boot the generated code and mount its router inside the chi router.
   In internal/shop/app.go's Router, after migrate.Apply and the routes,
   add (and import "shop/gen"):

       booted, err := gen.Boot(context.Background(), d.Writer())
       if err != nil {
           return nil, err
       }
       gmux := gen.Router(booted, func(*http.Request) *rastrillo.Ctx {
           return &rastrillo.Ctx{DB: d.Writer(), Logger: logger,
               Actor: rastrillo.Actor{Human: true}, Render: GenRender}
       })
       r.Handle("/admin/*", gmux)

   gen.Boot upgrades and checks the generated money columns, and
   gen.Router will not build without what it returns. Mounted inside
   the chi router, the generated screens get the same CSRF check and
   the same localizer as the hand ones.
```

`scaffold.readme_render`, inserted into the scaffold's `README.md`
after the paragraph ending "AGENTS.md carries the working
conventions.":

```markdown
## Templates

`internal/shop/render.go` builds every page once, at boot, with
`ui.Pages`. Every language and every request runs those same
templates, and the localizer in each page's data picks the language.
Start each template body with `{{$L := localizer $}}`, pass `$L` first
to `T`, `money` and the other locale functions, and pass `"L" $L` to
every partial. A call without it fails the page rather than render it
in the wrong language.
```
