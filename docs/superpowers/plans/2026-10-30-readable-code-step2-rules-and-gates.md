# Step 2, the rules and Rastrillo's own gates: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the readable-code rules where agents load them, and make
Rastrillo's own gate enforce the four limits with ceilings that only
ratchet down.

**Architecture:** A new package `internal/codelimits` measures Go
files with `go/ast` and judges each offence against
`.rastrillo/code.txt`, the list of offences that existed before the
gate, each recorded by identity. A record is a permission for that
exact offence and no other: touching a long line, renaming a function
or adding a short name elsewhere makes a new offence, which must fit.
`rastrillo budget code` runs it, and a new suite role, `code`, runs it
for the root module, `money` and every example. The rules go into
`skill/code.md` and a short `SKILL.md` section.

**Tech Stack:** Go 1.26 standard library (`go/parser`, `go/ast`,
`go/token`, `crypto/sha256`), `git` for the base comparison, the
existing `cmd/rastrillo` budget command and suite.

**Spec:** `docs/superpowers/specs/2026-10-29-readable-code-design.md`
("The rules", "The limits", "Where the rules live", "Order of work"
step 2). Steps 3 to 10 get their own plans. Step 1 landed as
841e34e6 (package `action`). Reviewed by GPT-6 Astra (discussion on
the branch).

## Global Constraints

### What is measured

- **columns:** a line wider than 80 columns, with tab stops every 4
  columns (as `expand -t4` renders it) and each rune one column. Every
  Go file, tests included.
- **file:** a non-test `.go` file over 400 lines (newlines, plus one
  for a last line without one, as `budgetsize` counts).
- **func:** a function's body is the lines strictly between the line of
  its opening brace and the line of its closing brace. Over 40 is over;
  an action is over at 15. Non-test files only. Measured on their own,
  as well as inside whatever holds them:
  - every function and method declaration;
  - a function literal assigned at package level (`var x = func...`),
    named by its variable; a literal nested anywhere else in a
    package-level variable's initializer (a map of callbacks) is named
    `<variable>.func<N>`, N counting from 1 in source order within that
    initializer;
  - a function literal of action type, wherever it appears, named
    `<enclosing>.action<N>`, where the enclosing name is the function
    or package-level variable holding it and N counts from 1 in source
    order within it; limit 15.
- **An action** is a function, method or literal whose parameters are
  exactly one `*Visit` from package `amadan.net/rastrillo/rastrillo/action`
  and which returns nothing. The package is resolved from the file's
  imports, whatever name it is imported under. An unqualified `*Visit`
  is it only in the package whose import path (the module path from
  `go.mod` plus the directory) is exactly that path. A different
  package that happens to be called `action` is not. Type aliases of
  `Visit` are not followed (ruled: an alias of `Visit` would be its own
  smell, caught in review).
- **names:** a single-letter name a file declares: a variable (`var`,
  `:=`, `range`, a type switch's), a constant, a parameter, a receiver,
  a named result. `_` is not a name. Allowed: `t` typed `*testing.T` or
  `testing.TB`; `b` typed `*testing.B`; `f` typed `*testing.F`; a
  one-capital-letter type parameter. Struct fields, labels, and type
  and function names are not counted. Every Go file, tests included.

### What is not measured

- **Generated files**, a line matching
  `^// Code generated .* DO NOT EDIT\.$` before the package clause
  (after any `//go:build` line). They stay excluded: step 5 decides,
  generator by generator, which output it makes fit and measures
  (Rastrillo's app generator); the CLDR and locale-pack tables made
  by `hack/` tools stay excluded as data.
- **Data files:** a file whose comment before the package clause holds
  `//rastrillo:data <reason>` is exempt from the file and columns
  limits, not from names or functions. Mark
  `internal/designsystem/prose.go` and `sampleprose.go` so (the spec
  names them), and any other file that is a table of words or values
  rather than code; list each in the commit notes with its reason. The
  spec exempts data files from the file limit; columns are exempted too
  because a table's entries are long strings that cannot be wrapped
  without making them worse (ruled in the ledger; Paul sees it).
- `testdata`, `vendor`, `node_modules`, directories starting with `.`
  or `_`, and nested modules (each measured from its own root, as
  `budget size` does).

### The ratchet

- **Identity.** A columns offence is the file plus the SHA-256 of the
  line's text with trailing white space removed (first 12 hex digits;
  leading white space is kept, so re-indenting a line makes it new),
  with a count for identical lines. A names offence is the
  file, the enclosing function (or `-` at package level) and the name,
  with a count. A file offence is the file. A func offence is the file
  and the function's name (`Name`, `Recv.Name`, a package-level
  variable's name, or `<enclosing>.action<N>`), and its ceiling is its
  current body length.
- **Judging.** An offence with no record fails. A func or file offence
  longer than its recorded ceiling fails; shorter means the record must
  be lowered. A columns or names record whose count is now higher fails,
  lower must be lowered. A record with no offence fails (the code was
  fixed, or the file or function is gone: delete it). `--lower` lowers
  and deletes records to match, and never adds or raises one.
- **Records only go down.** When it can find the branch's base
  (`git merge-base HEAD origin/main`), `budget code` also compares the
  file with the base's copy: a record added, or a ceiling raised, fails
  unless that line carries a reason after its count (the same habit as
  `.rastrillo/budgets.txt`), so a raise is visible and explained in
  review. A reason is the deliberate way through: the spec's ratchet
  is for the offences that existed before the gate, and a person
  reviewing the branch decides whether a raise is earned (ruled; this
  is how `.rastrillo/budgets.txt` already works). Records written by
  `--init` carry no reason. When there is no base (no `origin/main`,
  or the file is new at base), it says so in one line and skips this
  part.
- `--init` writes records for every offence and refuses if the file
  exists.

### For every task

- Every new Go file meets the limits with no records: Task 3 writes the
  baselines from the tree before this branch, so nothing this branch
  adds is grandfathered.
- User-facing strings (the gate's messages, `budget code`'s usage and
  help lines, the CLI docs section and the CHANGELOG entry) are drafted
  in Tasks 2 and 3 and reviewed with Paul in Task 5 before merge;
  approved text is applied verbatim. `SKILL.md` and `skill/code.md`
  are written for agents and are not copy-reviewed (the copy-review
  skill exempts text consumed by LLMs).
- `SKILL.md`'s existing examples (`a.owned(r)`, `(a *app)`) stay: they
  mirror what the scaffold writes today, and step 4 renames both
  together.
- Test evidence per CONTRIBUTING §3: red run in a detached worktree at
  the task's BASE with compiling stubs, saved under
  `~/.cache/code-gates/evidence/`; green run after.
- Git runs unsandboxed; `GOFLAGS=-mod=mod` for hand `go` commands at
  the root; commit with `amadan commit`, body ending
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- A file with a `//go:build` line before its generated-code comment:
  still generated, still skipped (Task 1).
- A line whose width comes from a multi-byte rune or a mid-line tab:
  measured in columns, not bytes (Task 1).
- A wrapped signature whose opening brace is on a later line than
  `func`: the body is measured from the brace (Task 1).
- Editing one long line in a file with other recorded long lines: the
  edited line is a new offence and fails; the others still pass
  (Task 2).
- `--init` where `.rastrillo/code.txt` exists: refused, file untouched
  (Task 3).

---

## File Structure

```
internal/codelimits/
  doc.go          what the package measures and why it ratchets
  measure.go      Measure(root): walk, skip, parse, collect offences
  inspect.go      per-file checks: columns, file, functions, names
  actions.go      resolving *action.Visit through a file's imports
  ceilings.go     Load/Format of .rastrillo/code.txt; Check; Lower
  base.go         the comparison with the branch base's copy
  messages.go     every user-facing message, one place for review
  run.go          Run(args, root, out) for the CLI
  *_test.go, testdata/
cmd/rastrillo/budget.go, main.go     dispatch and help, a few lines
internal/docsite/cli_test.go         sees budget's subcommands
Makefile, .amadan/ci.d/50-suite@35, amadan_ci_test.go   the code role
.rastrillo/budgets.txt               cmd/rastrillo's ceiling, with reason
.rastrillo/code.txt, money/.rastrillo/code.txt,
examples/*/.rastrillo/code.txt       baselines from the pre-branch tree
skill/code.md, SKILL.md, skillmd_test.go, AGENTS.md
docs/site/cli.md, CHANGELOG.md
```

---

### Task 1: Measuring

**Files:** Create `internal/codelimits/doc.go`, `measure.go`,
`inspect.go`, `actions.go`, `measure_test.go`, `testdata/`.

**Interfaces:**
- Produces:
  - `type Kind string`: `Columns = "columns"`, `FileLines = "file"`,
    `FuncLines = "func"`, `Names = "names"`
  - `type Offence struct { Kind Kind; Path string; Key string; Size
    int; Limit int; Line int }`: `Key` is the identity within the file
    (the line hash, `function/name`, the function's name, or empty for
    a file offence); `Size` is the width, the line count or the body
    length (1 for a name); `Line` is where it is, for messages.
  - `func Measure(root string) ([]Offence, error)`, sorted by Path,
    Kind, Key, Line.
  - Constants `MaxColumns = 80`, `MaxFileLines = 400`,
    `MaxFuncLines = 40`, `MaxActionLines = 15`.

- [ ] **Step 1: Write the failing tests**, each fixture a tiny module
  (its own `go.mod`) under `testdata/`:

```go
func TestMeasureFindsEachKindOfOffence(t *testing.T) {
	// testdata/offences: wide.go has an 81-column line via a mid-line
	// tab and one via a multi-byte rune; big.go 401 lines; long.go a
	// 41-line function, a 16-line action method, a 16-line action
	// literal passed to a call, and a 41-line package-level literal
	// `var handler = func...`; short.go declares a var x, a const n,
	// a range variable i and a parameter p.
	// Want exactly those offences, with their keys, sizes and limits.
}

func TestMeasureLeavesAloneWhatIsAllowed(t *testing.T) {
	// testdata/allowed: t *testing.T, t testing.TB, b *testing.B,
	// f *testing.F; a 60-line test function; Opt[T any]; struct fields
	// X, Y; a 40-line function; an 80-column line; a wrapped signature
	// with a 40-line body from its brace; a generated file (//go:build
	// first) full of offences; a //rastrillo:data file with long lines
	// and 500 lines; testdata/, .hidden/ and a nested module full of
	// offences. Want no offences.
}

func TestActionsResolveThroughImports(t *testing.T) {
	// A 16-line handler taking *act.Visit with action imported as act:
	// an action offence. A 16-line handler taking *action.Visit where
	// "action" is another module's package: no offence (under 40).
	// Inside a fixture package named action importing nothing, a
	// 16-line func(*Visit): an action offence.
}
```

- [ ] **Step 2: Stubs, red run** (`task1-red.txt`).
- [ ] **Step 3: Implement.** One `parser.ParseFile` per file with
  comments; widths from the raw source lines.
- [ ] **Step 4: Green run**, `go vet ./internal/codelimits/`.
- [ ] **Step 5: Commit** "codelimits: measure the four readable-code limits".

---

### Task 2: Records and the ratchet

**Files:** Create `internal/codelimits/ceilings.go`, `base.go`,
`messages.go`, `ceilings_test.go`, `base_test.go`.

**Interfaces:**
- Consumes: Task 1's `Offence`, `Kind`.
- Produces:
  - `const FileName = ".rastrillo/code.txt"`
  - `type Record struct { Kind Kind; Path, Key string; Count int;
    Reason string; Line int }` (`Count` is the ceiling for func and
    file, how many identical offences for columns and names; `Line` is
    the record's line in the file)
  - `type File struct { Records []Record }`
  - `func Load(root string) (*File, error)`; `func Parse(data []byte)
    (*File, error)`: a missing file is empty; a malformed line is an
    error naming its line number.
  - `func Format(file *File) []byte`: a header comment saying what the
    file is and that records only go down, then one line per record,
    `<kind> <path> <key or -> <count>[ <reason>]`, sorted.
  - `func Init(offences []Offence) *File`
  - `func Check(offences []Offence, file *File) []string`
  - `func Lower(offences []Offence, file *File) *File`
  - `func CompareWithBase(current, base *File) []string`: one message
    per record added or raised without a reason.
  - `func LoadBase(root string) (*File, bool, error)`: the file at
    `git merge-base HEAD origin/main`; false when there is no base.
- All message text lives in `messages.go`, each saying what is wrong,
  where, and what to do (rule 13).

- [ ] **Step 1: Write the failing tests**

```go
func TestAnUnrecordedOffenceFails(t *testing.T)
func TestEditingARecordedLongLineMakesANewOffence(t *testing.T)
func TestAFunctionGrowingPastItsCeilingFails(t *testing.T)
func TestShrinkingMeansLowering(t *testing.T)
func TestAStaleRecordFails(t *testing.T)
func TestLowerNeverAddsOrRaises(t *testing.T)
func TestFormatThenParseRoundTrips(t *testing.T)   // every kind, a reason with spaces
func TestAMalformedLineNamesItsLine(t *testing.T)
func TestARaiseWithoutAReasonFailsAgainstTheBase(t *testing.T)
func TestARaiseWithAReasonPasses(t *testing.T)
func TestAnAddedRecordWithoutAReasonFails(t *testing.T)
func TestNoBaseSkipsTheComparison(t *testing.T)    // a scratch git repo with no origin/main
```

- [ ] **Step 2: Stubs, red run** (`task2-red.txt`).
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Green run.**
- [ ] **Step 5: Commit** "codelimits: records that only go down".

---

### Task 3: `rastrillo budget code`, the role and the baselines

**Files:** Create `internal/codelimits/run.go` and `run_test.go`;
modify `cmd/rastrillo/budget.go` and `main.go`,
`internal/docsite/cli_test.go`, `.rastrillo/budgets.txt`, `Makefile`,
`amadan_ci_test.go` if it names roles; rename
`.amadan/ci.d/50-suite@34` to `@35`; create the baselines; draft
`docs/site/cli.md`'s section and a CHANGELOG entry.

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces:
  - `type Outcome struct { Messages []string; Records int; Malformed
    bool; Exists bool }` and `func Run(mode Mode, root string)
    (Outcome, error)` in `codelimits`, with `type Mode int` and
    constants `Check`, `Init`, `Lower`. `Check` measures, compares
    with the base and judges; `Init` writes `FileName` from `Init` and
    sets `Exists` without writing if it is there; `Lower` rewrites
    through `Lower`, then checks. `Malformed` is set when the file
    cannot be parsed.
  - `cmd/rastrillo/budget.go`: `case "code":` calls a `budgetCode(args,
    out)` that parses `[--init | --lower] [dir]` with `flag.FlagSet`
    (so the flags read as `--init` and `--lower`, where the docsite CLI
    scanner finds them), calls `codelimits.Run`, prints each message,
    and returns `exitError{code: 1}` for messages and
    `exitError{code: 2}` for `Malformed` or `Exists`, as `budgetSize`
    does; else prints one line with the record count. The usage line
    and `main.go`'s help name `budget code`. The directory is at its
    ceiling (5,995 of 5,995), so raise its record in
    `.rastrillo/budgets.txt` by exactly the lines added, with a reason
    in the file's style ("Raised from 5995 to N for `budget code`: its
    flags and exit codes, beside budget size's; the measuring lives in
    internal/codelimits").
  - `internal/docsite/cli_test.go`: its dispatch scan reads
    `budget.go`'s switch with the prefix `budget `, as it reads
    `migration.go`'s, so `budget code`, `budget size` and `budget test`
    must each be documented.
  - Makefile: a target `code`, beside `budget`, runs `go run
    ./cmd/rastrillo budget code` for the root, `money` and each of
    `$(EXAMPLES)`; `code` joins `SUITE` after `budget`; the suite step
    becomes `50-suite@35`; `amadan_ci_test.go` agrees.

- [ ] **Step 1: Write the failing tests**

```go
// internal/codelimits/run_test.go
func TestRunInitWritesThenReportsItExists(t *testing.T)
func TestRunFailsANewOffence(t *testing.T)
func TestRunLowerThenPasses(t *testing.T)
func TestRunReportsAMalformedFile(t *testing.T)

// cmd/rastrillo/budget_code_test.go, through the built command
func TestBudgetCodeExitCodes(t *testing.T) // 0 clean, 1 an offence, 2 malformed, 2 --init over an existing file
```

- [ ] **Step 2: Stubs, red run** (`task3-red.txt`).
- [ ] **Step 3: Implement** `run.go`, the dispatch, help, docsite scan,
  Makefile target and role, and the step rename.
- [ ] **Step 4: Write the baselines from the tree before this branch.**
  Make a detached worktree at the branch's base commit (841e34e6) and
  add the same `//rastrillo:data` markers there as in this branch
  (only those lines; nothing else), so the baselines never record what
  the markers exempt. Build this branch's `rastrillo` binary, run
  `budget code --init` in that worktree's root, `money` and each
  example, and copy each `code.txt` into place. Remove the worktree. Then
  `make code` on this branch must pass: anything this branch added is
  measured with no record. Put the record counts by kind, per module,
  and the data files with their reasons, in the commit notes.
- [ ] **Step 5: Draft the docs.** `docs/site/cli.md`'s `rastrillo
  budget` section gains `budget code` in the same voice as `budget
  size`: what it measures, the file it reads, what `--init` and
  `--lower` do, the reason a raise needs, the exit codes. An
  Unreleased CHANGELOG entry. Both are drafts for Task 5.
- [ ] **Step 6: Green run**: the new tests, `go test -run Amadan
  -count=1 .`, `go test ./internal/docsite/ -count=1`, `make code`,
  `make budget`.
- [ ] **Step 7: Commit** "Gate the readable-code limits: rastrillo
  budget code and the code role".

---

### Task 4: The rules where agents load them

**Files:** Create `skill/code.md`; modify `SKILL.md`,
`skillmd_test.go` (only if the budget must move), `AGENTS.md`.

- **`skill/code.md`**: the fourteen rules from the spec's "The rules",
  each with one short example; the action shape (the four beats, with
  the handler from `docs/site/reference/action.md`); the operation
  shape (rules and database work, `op` refusals, no HTTP); the limits
  and exactly how each is measured (from this plan's Global
  Constraints); what each `budget code` message means and how to fix
  it, including `--lower` and a raise's reason. Rule 9 says the
  canonical example app is coming (step 6) and, until then, points at
  the `action` reference page's example; if the topic names
  `examples/` at all it must carry the caveat `skillmd_test.go`
  requires (`TestSkillMDSaysExamplesAreNotInTheModule`). Under the
  topic budget (9,000 bytes).
- **`SKILL.md`**: a short "Write code that reads like a sentence"
  section: the limits one line each, and rules 1 to 7 a sentence each,
  then a pointer to `skill/code.md`; and an index row: "write or
  change any Go code: names, function and file size, comments, an
  action's shape | `skill/code.md`". Fit the 8,000-byte core budget by
  trimming redundant prose first; if a rule every task needs cannot
  fit, raise `skillCoreBudget` and say why in its comment, as that
  comment asks.
- **`AGENTS.md`** (Rastrillo's own): "Comments" gains rule 7's one or
  two sentences and the pointer to the spec or commit for long
  reasoning; a short "Code" section points at `skill/code.md` and
  `make code`.

- [ ] **Step 1:** Write `skill/code.md`.
- [ ] **Step 2:** Edit `SKILL.md` and `AGENTS.md`.
- [ ] **Step 3: Run** `GOFLAGS=-mod=mod go test -run Skill -count=1 .`
  and `go test ./internal/docsite/ -count=1`. Expected: PASS.
- [ ] **Step 4: Commit** "Teach the readable-code rules: skill/code.md
  and SKILL.md".

---

### Task 5: Copy review, then the whole gate

- [ ] **Step 1:** The controller collects every user-facing string
  Tasks 2 and 3 drafted (each message in `messages.go`, `budget
  code`'s usage and help lines, the `cli.md` section, the CHANGELOG
  entry) into a copy review with Paul.
- [ ] **Step 2:** Apply the approved text verbatim; re-run Tasks 2 and
  3's tests.
- [ ] **Step 3:** `make ci` once; the branch's CI on the fleet must be
  green.
- [ ] **Step 4:** Commit and push.
