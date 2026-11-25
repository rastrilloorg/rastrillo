# Readability measurement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Score how readable agent-written Rastrillo app code is, in
three layers checked against facts and against Paul, and use the score
in a round that decides whether agents get gopls.

**Architecture:** New packages in the `bench/agent` module: `authored`
finds the code an agent wrote by re-scaffolding and diffing; `counts`
measures it; `comprehend` builds answer keys (Claude) and runs readers
(Astra through `codex exec`); `calibrate` cuts excerpt pairs, serves
Paul's blind page and computes agreement; `waste` sorts transcript
errors. `cmd/bench` gains one subcommand per step. The runner gains a
per-arm MCP config for the `carlos-gopls` arm.

**Tech Stack:** Go 1.25 (`go/ast`, `go/parser`, `go/printer`),
`github.com/uudashr/gocognit`, `github.com/mibk/dupl`, Rastrillo's
`internal/codelimits` (imported through a `replace` to the repo root),
`claude -p` on the bench token, `codex exec -m gpt-6-astra`, gopls
v0.24.0.

**Spec:** `docs/superpowers/specs/2026-11-22-readability-measurement-design.md`

## Global Constraints

- All new code lives in the `bench/agent` module; nothing in Rastrillo's
  framework, scaffold or skill changes (spec, Non-goals). The one file
  outside it is the repo-root `.mcp.json` (Task 1).
- Steps 1 to 4 (Tasks 2 to 9) read only `groomer` runs. The kept
  `lending` and `noticeboard` runs and transcripts are not opened until
  Task 10.
- Kept runs under `~/.cache/agent-bench/` are read, never modified.
  Scratch goes under `$TMPDIR` (use absolute paths; it differs sandboxed
  and not) and is deleted when the command ends.
- Anything served binds to `127.0.0.1` or the Tailscale IP passed with
  `-listen-ip`, never all interfaces, and stops when its command ends.
- `codex exec` runs with the Claude bash sandbox off, always with
  `-s read-only --skip-git-repo-check`, prompt on stdin, `-o` for the
  answer. Exit 0 with no `-o` file is a harness failure, never an
  answer.
- Readers: `-m gpt-6-astra -c model_reasoning_effort=high`.
- Answer keys, job finding and the held-out question writer run on the
  bench token (`~/.config/agent-bench/token`) in the bench's clean
  environment (`cleanenv.Config`, `cleanenv.Env`), scheduled between
  round builds.
- Excerpts at most 60 lines. 30 pairs, 15 to tune and 15 to check;
  at least 12 decided check pairs; a layer is trusted at 80% agreement
  on decided check pairs.
- Far short name: a name of 1 or 2 characters used more than 10 lines
  from its declaration.
- Round `read-1`: arms `carlos` and `carlos-gopls`, briefs `groomer`,
  `lending`, `noticeboard`, `-n 3`, `-j 1`, gopls v0.24.0.
- Accepting gopls: better on at least two of cost, turns, failed builds
  and comprehension effort on the held-out briefs, by more than the
  run-to-run range, and not worse on any other measure beyond it.
  "By more than the range" means the two arms' ranges (min to max over
  their three runs) do not overlap. Anything else on a measure is
  "inconclusive", reported as such, and never counts as a win.
- Cost and turns in the decision are build plus follow-up, because the
  scored source is the final app after both phases.
- Required functionality: a question whose key says the app does not
  do it ("not built") is missing functionality. An arm with more
  missing functionality than the other on a held-out brief cannot be
  accepted, whatever its other numbers.
- Noise gate: for each comprehension measure, the median absolute
  difference between two reads of one app must be at most half the
  median absolute difference between runs of one brief. A measure that
  fails the gate is inconclusive for every decision until it passes.
- Retries of calibration: at most one revision per layer; every attempt
  is reported.
- The round's schedule interleaves arms (`carlos` 1, `carlos-gopls` 1,
  `carlos` 2, ...), and the schedule is saved so a resume keeps it.
- Locally, Go commands in this repo need `GOFLAGS=-mod=mod`; git runs
  unsandboxed. `make bench` is the gate lane for this module.
- Commit subjects under 80 characters, body after a blank line, ending
  with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **An agent that scaffolded in `$TMPDIR` and copied, or ran
   `rastrillo new` several times** (the kept runs do both, inside
   pipelines, `;` lists and heredocs): commands are parsed as shell
   syntax, and the scaffold used is the last simple command
   `rastrillo new` whose name argument matches the last element of the
   app's `go.mod` module path. No match, or two different flag sets in
   the final position, is "unscorable", never a guess. Tested in Task 2
   with commands copied from the kept groomer transcripts.
2. **A file changed only by gofmt or comments**: unchanged. Bodies are
   compared as `go/printer` output with comments dropped. Tested in
   Task 2.
3. **A kept app that does not compile**: counts still come out (parse
   only, no type check); the key-maker marks behavioural questions
   unsettled rather than failing the run. Tested in Tasks 3 and 4.
4. **codex exiting 0 with no answer file** (a bad model slug or auth
   does this): a harness failure, retried twice, then the command stops
   with the log's last lines. Tested in Task 4.
5. **An answer right in a different form** ("422" and "Unprocessable
   Entity"; `/bookings/{id}` and `/bookings/:id`; "Yes" and "yes."):
   normalised by the question's kind before comparing. Tested in Task 5.

---

### Task 1: gopls for sessions working on Rastrillo

**Files:**
- Create: `.mcp.json`

- [ ] **Step 1: Write `.mcp.json`**

```json
{"mcpServers": {"gopls": {"command": "gopls", "args": ["mcp"]}}}
```

- [ ] **Step 2: Check it loads**

Run (unsandboxed, from the worktree): `claude mcp get gopls`
Expected: the server listed with command `gopls mcp`, scope project.

- [ ] **Step 3: Check the bench ignores it**

Run: `grep -n 'strict-mcp-config' bench/agent/internal/runner/runner.go`
Expected: the flag is present, so no run inherits the project file.

- [ ] **Step 4: Commit**

`git add .mcp.json && git commit` with subject
`Give sessions working on Rastrillo gopls over MCP`.

---

### Task 2: Find the code an agent wrote

**Files:**
- Create: `bench/agent/internal/authored/scaffold.go`
- Create: `bench/agent/internal/authored/authored.go`
- Test: `bench/agent/internal/authored/authored_test.go`,
  `bench/agent/internal/authored/testdata/` (two tiny apps: `scaffold/`
  and `app/`, plus `build.jsonl` with three `rastrillo new` calls)
- Modify: `bench/agent/README.md` (a "Scoring" section, added here and
  extended by later tasks)

**Interfaces:**
- Consumes: `transcript.Parse`, `transcript.Transcript`.
- Produces:
  - `type Scaffold struct{ Name string; Flags []string }`
  - `func ScaffoldOf(run transcript.Transcript, module string) (Scaffold, error)`;
    `ErrUnscorable` when no call matches.
  - `func Rescaffold(rastrilloDir string, scaffold Scaffold, into string) error`:
    builds `cmd/rastrillo` from `rastrilloDir` (the module cache copy at
    the run's version, `GOFLAGS=-mod=mod GOPROXY=off`) into `into` and
    runs `new` there with the flags.
  - `type Func struct{ Key string; Start, End int }`: an authored
    unit, a function, a method or a package-level function literal,
    keyed exactly as `codelimits` keys them (`Name`, `Recv.Name`,
    `<variable>`, `<variable>.func<N>`); lines 1-based, inclusive
  - `type File struct{ Path string; Test, Whole bool; Funcs []Func }`
    (Whole: the scaffold has no such file)
  - `type Code struct{ Files []File }`
  - `func Authored(appDir, scaffoldDir string) (Code, error)`
  - `func RunModule(appDir string) (string, error)`: the `go.mod`
    module path.

- [ ] **Step 1: Write the failing tests**

```go
func TestScaffoldCallsAreFoundInsidePipelinesAndLists(t *testing.T)
// the real accept-1 command "cd $TMPDIR && ... && rastrillo new
// --auth=password --shell=topbar groomer 2>&1 | tail -20 && find ..."
func TestRastrilloNewInsideAHeredocOrCommitMessageIsIgnored(t *testing.T)
func TestAPackageLevelFuncLiteralIsAnAuthoredUnit(t *testing.T)
// var handler = func(...) {...} changed: Funcs == [handler]
func TestTheLastMatchingScaffoldCallWins(t *testing.T)
// build.jsonl: "rastrillo new --help", "rastrillo new --shell=topbar groomer"
// in $TMPDIR, "rastrillo new --auth=password --shell topbar groomer".
// ScaffoldOf(run, "groomer") == Scaffold{"groomer",
//   []string{"--auth=password", "--shell", "topbar"}}

func TestNoMatchingScaffoldCallIsUnscorable(t *testing.T)
// module "salon": errors.Is(err, ErrUnscorable)

func TestAuthoredCodeIsNewFilesAndChangedFunctions(t *testing.T)
// app has a new bookings.go (Whole, all funcs), app.go with New changed
// and Router untouched (Funcs == [New]), handlers.go untouched (absent).

func TestGofmtAndCommentOnlyChangesAreNotAuthored(t *testing.T)
// render.go reindented and a comment added: absent from Code.Files.

func TestGeneratedAndVendoredFilesAreNeverAuthored(t *testing.T)
// gen/x.go, a "// Code generated ... DO NOT EDIT." file, static/a.go,
// vendor/, testdata/: all absent.

func TestTestFilesAreMarked(t *testing.T)
// bookings_test.go present with Test == true.
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd bench/agent && go test ./internal/authored/ -count=1`
Expected: FAIL, undefined `ScaffoldOf`.

- [ ] **Step 3: Implement `scaffold.go` and `authored.go`**

`ScaffoldOf` parses each Bash tool input with `mvdan.cc/sh/v3/syntax`,
walks its simple-command nodes (so pipelines, lists and redirections
fall away and heredoc bodies are never commands), drops `-h`/`--help`
calls, and keeps the last `rastrillo new` whose positional argument
equals `path.Base(module)`. Bodies compare as
`go/printer` output of the `*ast.FuncDecl` with `Doc` and comments
removed; files compare by that set plus imports.

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd bench/agent && go test ./internal/authored/ -count=1`
Expected: PASS.

- [ ] **Step 5: Try it on one kept run**

Run a throwaway `go test -run TestKeptRun` guarded by
`BENCH_KEPT=~/.cache/agent-bench/1/groomer-carlos-1` (skipped when
unset), which rescaffolds into `$TMPDIR` and prints file and function
counts. Expected: a non-empty Code, no scaffold file (e.g.
`handlers.go` untouched) listed.

- [ ] **Step 6: Commit**

Subject: `bench: find the code an agent wrote by re-scaffolding`.

---

### Task 3: Counts, and the step 1 checkpoint

**Files:**
- Create: `bench/agent/internal/counts/counts.go` (per function),
  `names.go` (far short names), `module.go` (offences, duplication,
  comment share, interfaces), `summary.go`
- Test: `bench/agent/internal/counts/counts_test.go`, `testdata/`
- Create: `bench/agent/cmd/bench/score.go`; Modify: `cmd/bench/main.go`
  (register `score`), `bench/agent/go.mod` (require gocognit, dupl and
  `amadan.net/rastrillo/rastrillo` with `replace => ../..`)

**Interfaces:**
- Consumes: `authored.Code`, `authored.Func`;
  `codelimits.Measure(root string) ([]codelimits.Offence, error)`.
- Produces:
  - `type FuncCounts struct{ Path, Key string; Test bool; Lines, Cognitive, Nesting, FarShortNames, DuplicatedLines, OverLimitLines int }`
  - `type RunCounts struct{ Funcs []FuncCounts; Offences map[codelimits.Kind]int; CommentShare float64; SingleImplementationInterfaces *int; AuthoredLines int }`
    (`SingleImplementationInterfaces` is nil when the app does not
    type-check: reported "unavailable", never zero)
  - `func Measure(appDir string, code authored.Code) (RunCounts, error)`
  - `type Spread struct{ Median, P90 float64 }`
  - `func Summarise(run RunCounts) map[string]Spread` keyed
    `lines`, `cognitive`, `nesting`, `far-short-names`, `duplicated`,
    `over-limit`; app code and tests summarised apart
    (`Summarise` takes app funcs only; `SummariseTests` the rest).
  - `bench score -rounds 1,accept-1,cost-1,weight-1 -briefs groomer -out readability-kept`
    writes `results/readability-kept.json` and `.md`: one row per run,
    plus "unscorable" rows with the reason.

- [ ] **Step 1: Write the failing tests**

```go
func TestCognitiveComplexityMatchesGocognit(t *testing.T)   // fixture func, value from gocognit
func TestNestingCountsTheDeepestBlock(t *testing.T)         // if>for>switch == 3
func TestAShortNameUsedFarAwayIsCounted(t *testing.T)       // "r" used 11 lines on: 1; at 10: 0
func TestOnlyAuthoredOffencesAreCounted(t *testing.T)       // wide line in an untouched func: 0
func TestDuplicatedBlocksCountTheirLines(t *testing.T)      // two 20-line copies: 40
func TestAnInterfaceWithOneImplementationIsCounted(t *testing.T)
func TestAFileThatDoesNotCompileIsStillCounted(t *testing.T) // undefined identifier: counts present
func TestTestFunctionsAreSummarisedApart(t *testing.T)
func TestAFileGrownPastFourHundredLinesIsCounted(t *testing.T) // scaffold file + authored funcs, offence at line 1: counted
func TestInterfacesAreUnavailableWhenTheAppDoesNotTypeCheck(t *testing.T)
func TestBlankLinesAndCommentsChangeNoCount(t *testing.T)    // padded copy of a fixture: identical FuncCounts except Lines
func TestDuplicationEitherSideOfTheThreshold(t *testing.T)   // dupl v1.1.0, threshold 100: 99 not counted, 100 counted
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd bench/agent && go test ./internal/counts/ -count=1`
Expected: FAIL, undefined `Measure`.

- [ ] **Step 3: Implement**

Parse (`go/parser`) for every count; type-check (`go/packages`) only
for interfaces, and only when the app builds. Far short names come
from `go/ast` scopes: declaration line against each use's line.
Offences: run `codelimits.Measure` on the app and keep `func`, `names`
and `columns` offences inside an authored unit or whole file, and a
`file` offence when its file is whole or holds any authored unit.
Duplication: `github.com/mibk/dupl` v1.1.0 at threshold 100 (its own
default, in its serialized syntax units), counted where at least one
copy is authored. Comment share: comment lines over code lines in
authored functions and whole files.

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd bench/agent && GOFLAGS=-mod=mod go test ./internal/counts/ -count=1`
Expected: PASS.

- [ ] **Step 5: Wire `bench score` and run it on the kept groomer runs**

Run: `cd bench/agent && GOFLAGS=-mod=mod go run ./cmd/bench score -rounds 1,accept-1,cost-1,weight-1 -briefs groomer -out readability-kept`
Expected: 12 rows (or fewer, each missing one named "unscorable" with
its reason); `make bench` passes.

- [ ] **Step 6: Checkpoint 1**

Read the five worst and five best authored functions by cognitive
complexity in the report, and write in
`results/readability-kept.md` whether they are, by reading, the
hardest and easiest to follow. Post the table and that judgement to a
branch discussion ("Checkpoint 1: counts on the kept runs") and tell
Paul.

- [ ] **Step 7: Commit**

Subject: `bench: count what makes an agent's code hard to read`.

---

### Task 4: Questions, answer keys, and the codex wrapper

**Files:**
- Create: `bench/agent/briefs/groomer/questions.txt`
- Create: `bench/agent/internal/comprehend/questions.go`, `keys.go`
- Create: `bench/agent/internal/codex/codex.go`
- Test: `internal/comprehend/questions_test.go`, `keys_test.go`;
  `internal/codex/codex_test.go`, `internal/codex/testdata/exec.jsonl`
  (captured from one real `codex exec --json` run in Step 5)
- Create: `bench/agent/cmd/bench/keys.go`; Modify: `cmd/bench/main.go`

**Interfaces:**
- Produces:
  - `type Kind string` with `Status`, `Choice`, `YesNo`, `Route`, `Word`
  - `type Question struct{ ID, Ask string; Kind Kind; Choices []string }`
  - `func LoadQuestions(briefDir string) ([]Question, error)`; format,
    blocks separated by a blank line:
    ```
    id: empty-date
    kind: choice
    ask: What does a visitor see after submitting the booking form with the date left empty?
    choices: the form again with a message | an error page | a confirmation | nothing changes
    ```
  - `type Key struct{ ID, Answer, Evidence, SourceHash string; Settled, Missing bool }`
    (`Missing`: the app does not do what the question asks; its answer
    is "not built")
  - `func MakeKeys(ctx context.Context, setup KeySetup, questions []Question) ([]Key, error)`
    where `KeySetup{ AppDir, RastrilloDir, Token, BenchRoot, Scratch string }`.
    Runs `claude -p` once per app on a scratch copy, clean env, prompt
    asks for a JSON array of Key, allows reading, running tests and
    starting the app on `127.0.0.1`. The copy's source hash is taken
    before and after; if it changed, every key is unsettled. A
    behavioural key is settled only if its Evidence holds the exact
    command run and its output.
  - `type Exec struct{ Model, Effort, Dir string; AddDirs []string; Timeout time.Duration }`
  - `type Reply struct{ Text string; InputTokens, CachedTokens, OutputTokens, Commands int }`
  - `func (e Exec) Run(ctx context.Context, prompt string) (Reply, error)`;
    `ErrSilent` for exit 0 with no `-o` file, carrying the log's last
    20 lines. Isolated: codex runs under `bwrap` with only `Dir`,
    `AddDirs`, the codex binary, system libraries and a scratch
    `CODEX_HOME` holding a copy of `~/.codex/auth.json` and an empty
    config (no operator instructions, MCP servers or skills); nothing
    else of `$HOME` is visible.
  - `bench keys -rounds ... -briefs groomer` writes
    `~/.cache/agent-bench/<round>/<run>/keys.json` (beside the run, not
    inside `app/`; the run's own files stay untouched).

- [ ] **Step 1: Write the groomer questions**

Ten questions on what the app does, from `briefs/groomer/brief.txt`
and `followup.txt` only, at least four behavioural (what a request
gets), each with a kind. No question names a file, function or route
the brief does not.

- [ ] **Step 2: Write the failing tests**

```go
func TestQuestionsParseEveryKind(t *testing.T)
func TestAChoiceQuestionNeedsChoices(t *testing.T)          // error names the id
func TestKeysParseFromTheKeyMakersAnswer(t *testing.T)      // fixture text with prose around the JSON
func TestAnUnsettledKeyIsKeptAndMarked(t *testing.T)
func TestCodexRepliesCarryTokensAndCommands(t *testing.T)   // testdata/exec.jsonl
func TestCodexWithNoAnswerFileIsSilent(t *testing.T)        // fake codex on PATH: exit 0, no file -> ErrSilent
func TestAReaderCannotSeeOutsideItsDirectories(t *testing.T) // fake codex runs "cat <keys.json path>" and "ls $HOME": both fail
func TestKeysAreUnsettledWhenTheSourceChanged(t *testing.T)
func TestABehaviouralKeyWithoutCommandAndOutputIsUnsettled(t *testing.T)
```

- [ ] **Step 3: Run them to see them fail**

Run: `cd bench/agent && go test ./internal/comprehend/ ./internal/codex/ -count=1`
Expected: FAIL.

- [ ] **Step 4: Implement**

`Exec.Run` runs `codex exec -m <Model> -c model_reasoning_effort=<Effort> -s read-only --skip-git-repo-check -C <Dir> [--add-dir ...] --json -o <tmp> -`
with the prompt on stdin, retries `ErrSilent` twice. Tokens come from
the `--json` usage events; Commands counts command-execution items.

- [ ] **Step 5: Capture the fixture**

Run one real `codex exec --json` (unsandboxed) on a one-line prompt,
save its stdout as `internal/codex/testdata/exec.jsonl`, and make
`TestCodexRepliesCarryTokensAndCommands` assert its real numbers.

- [ ] **Step 6: Run the tests to see them pass**

Expected: PASS.

- [ ] **Step 7: Make keys for the kept groomer runs**

Run (unsandboxed): `cd bench/agent && go run ./cmd/bench keys -rounds 1,accept-1,cost-1,weight-1 -briefs groomer`
Expected: a `keys.json` per scorable run; a run whose app does not
build has its behavioural keys unsettled, not missing. Check every key
of three apps by reading the code and, for behavioural ones, by
re-running the recorded command. If more than 1 in 10 is wrong, run a
second key-maker on every app and mark a question unsettled wherever
the two disagree.

- [ ] **Step 8: Commit**

Subject: `bench: questions per brief, answer keys, and a codex wrapper`.

---

### Task 5: Readers, scoring and the noise check (checkpoint 2)

**Files:**
- Create: `bench/agent/internal/comprehend/reader.go`, `grade.go`
- Test: `internal/comprehend/reader_test.go`, `grade_test.go`
- Create: `bench/agent/cmd/bench/read.go`; Modify: `cmd/bench/main.go`

**Interfaces:**
- Consumes: `Question`, `Key`, `codex.Exec`, `codex.Reply`.
- Produces:
  - `func Normalise(kind Kind, answer string) string`
  - `func Correct(question Question, key Key, answer string) bool`
  - `type Reading struct{ QuestionID string; Answer string; Correct, OverBudget bool; Tokens, Commands int }`
  - `func Read(ctx context.Context, exec codex.Exec, question Question, budget int) (Reading, error)`:
    the reader's `Dir` is a scratch copy holding `app/` and
    `rastrillo/` (the module at the run's version); prompt asks for
    `{"answer": "..."}` only. Tokens over `budget` mark `OverBudget`,
    which grades wrong.
  - `type RunReading struct{ Run string; Readings []Reading; Accuracy, MedianTokens, MedianCommands float64; Unsettled, Missing int }`
    Accuracy is correct over all the brief's questions (a fixed
    denominator); unsettled and missing questions are counted and
    reported, not dropped. Effort (tokens, commands) is compared
    between two groups only on questions both answered correctly.
  - `bench read -rounds ... -briefs groomer -reads 2 -budget 200000`
    writes `results/comprehension-<out>.json` and `.md`, with a noise
    section: per run, the spread between its two reads; per brief, the
    spread between runs. Effort is compared only between runs on the
    same Rastrillo version, so the report groups runs by
    `RunRecord.RastrilloVersion`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStatusAnswersNormaliseNamesAndNumbers(t *testing.T) // "Unprocessable Entity" == "422"
func TestRoutesNormaliseTheirParameters(t *testing.T)       // "/bookings/:id" == "/bookings/{id}" == "GET /bookings/{bookingID}" (method kept when both give one)
func TestYesNoIgnoresCaseAndFullStops(t *testing.T)
func TestAChoiceMustBeOneOfItsChoices(t *testing.T)
func TestAccuracyHasAFixedDenominator(t *testing.T)         // 10 questions, 2 unsettled, 7 right: 0.7, Unsettled 2
func TestEffortIsComparedOnlyOnQuestionsBothGotRight(t *testing.T)
func TestTheNoiseGateFailsWhenReadsVaryAsMuchAsRuns(t *testing.T)
func TestOverBudgetGradesWrong(t *testing.T)
func TestReadersAnswerIsTheJSONField(t *testing.T)          // prose around {"answer":"422"}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd bench/agent && go test ./internal/comprehend/ -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement `grade.go` and `reader.go`**

- [ ] **Step 4: Run the tests to see them pass**

Expected: PASS.

- [ ] **Step 5: Read the kept groomer runs twice**

Run (unsandboxed): `cd bench/agent && go run ./cmd/bench read -rounds 1,accept-1,cost-1,weight-1 -briefs groomer -reads 2 -budget 200000 -out kept`
Expected: about 240 reads; the report's noise section filled in.

- [ ] **Step 6: Checkpoint 2**

Apply the noise gate (Global Constraints). If a measure fails it,
raise `-reads` to 3 and re-run once; if it still fails, that measure is
inconclusive for every later decision, and the report says so. Set `-budget` to the
95th percentile of tokens on correct answers, rounded up. Post
accuracy, effort and noise to a branch discussion ("Checkpoint 2:
comprehension") and tell Paul.

- [ ] **Step 7: Commit**

Subject: `bench: Astra reads each app and answers, graded against keys`.

---

### Task 6: Excerpt pairs and the composite

**Files:**
- Create: `bench/agent/briefs/groomer/jobs.txt` (one job a line, about
  nine, from the brief: showing free slots, booking, owner sign-in,
  editing a service, editing opening hours, listing bookings, cancelling
  a booking, seeding demo data, validating a booking)
- Create: `bench/agent/internal/calibrate/jobs.go`, `excerpt.go`,
  `composite.go`, `pairs.go`
- Test: `internal/calibrate/excerpt_test.go`, `composite_test.go`,
  `pairs_test.go`
- Create: `bench/agent/cmd/bench/pairs.go`; Modify: `cmd/bench/main.go`

**Interfaces:**
- Consumes: `authored.Code`, `counts.FuncCounts`,
  `comprehend.KeySetup`; job finding uses `claude -p` on the bench token
  as Task 4's key-maker does.
- Produces:
  - `func FindJobs(ctx context.Context, setup comprehend.KeySetup, jobs []string) (map[string][]string, error)`:
    job to `path:Key` list, functions the app authored only.
  - `type Excerpt struct{ Run, Job, Text string; Funcs []counts.FuncCounts }`
  - `func Cut(appDir string, funcs []string, counted []counts.FuncCounts) (Excerpt, bool)`:
    the job's functions in source order, false when over 60 lines.
  - `type Weights struct{ Cognitive, Nesting, OverLimit, Duplicated, FarShortNames float64 }`
    and `var Even = Weights{1, 1, 1, 1, 1}`
  - `func Composite(excerpt Excerpt, weights Weights) float64`: the
    weighted sum of the counts over all the job's functions, helpers
    included, not divided by lines; lower reads better. Size is
    reported beside it, never folded in.
  - `type Pair struct{ ID int; Canonical string; Job string; Left, Right Excerpt; Set string }`
    (`Set` is `tune`, `check` or `spare`; `Canonical` is the two
    excerpts' run and job, sorted, so a pair is the same pair in either
    order)
  - `func Choose(excerpts []Excerpt, seed int64, judged []string) []Pair`:
    the seed first splits the runs in half; tune pairs use only one
    half's excerpts and check and spare pairs only the other's. 15
    tune, 15 check and 10 spare pairs, same job and different runs,
    spread evenly over the composite difference under `Even`, left and
    right shuffled, never a `Canonical` in `judged`.
  - `func ChooseAcross(excerpts []Excerpt, baseline, other brief.Arm, seed int64) ([]Pair, Coverage)`:
    for each job and each run index, one pair, baseline arm against the
    other, no difference-based sampling; `Coverage` counts per arm the
    jobs missing or over 60 lines.
  - `bench pairs -rounds ... -briefs groomer -seed <n> [-judged results/calibration-*.json] [-across <arm>,<arm>]`
    writes `results/pairs-<date>.json` (the seed recorded); `-across`
    switches to `ChooseAcross`.

- [ ] **Step 1: Write the failing tests**

```go
func TestAnExcerptOverSixtyLinesIsDropped(t *testing.T)
func TestAnExcerptKeepsSourceOrder(t *testing.T)
func TestPaddingDoesNotChangeTheComposite(t *testing.T)     // blank lines and comments added: same score
func TestExtractingATrivialHelperDoesNotLowerTheComposite(t *testing.T)
func TestTuneAndCheckPairsComeFromDifferentRuns(t *testing.T)
func TestAJudgedPairIsNeverChosenAgainInEitherOrder(t *testing.T)
func TestChooseAcrossPairsTheTwoArmsByJobAndReportsCoverage(t *testing.T)
func TestPairsAreTheSameJobFromDifferentRuns(t *testing.T)
func TestPairsSpreadAcrossTheCompositeDifference(t *testing.T) // each quintile of difference holds 5 to 7 pairs
func TestTheSameSeedChoosesTheSamePairs(t *testing.T)
func TestThereAreFifteenTuneFifteenCheckAndTenSpares(t *testing.T)
```

- [ ] **Step 2: Run them to see them fail**

Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests to see them pass**

Expected: PASS.

- [ ] **Step 5: Choose the pairs**

Run (unsandboxed): `cd bench/agent && go run ./cmd/bench pairs -rounds 1,accept-1,cost-1,weight-1 -briefs groomer -seed 20261122`
Expected: 40 pairs. Read five to check each side really does the job.

- [ ] **Step 6: Commit**

Subject: `bench: cut same-job excerpts into blind pairs`.

---

### Task 7: Paul's page, Astra's picks, agreement (checkpoint 3)

**Files:**
- Create: `bench/agent/internal/calibrate/page.go`, `page.html`
  (embedded), `agree.go`
- Test: `internal/calibrate/page_test.go`, `agree_test.go`
- Create: `bench/agent/cmd/bench/calibrate.go`, `agree.go`; Modify:
  `cmd/bench/main.go`

**Interfaces:**
- Consumes: `Pair`, `Weights`, `Composite`, `codex.Exec`.
- Produces:
  - `type Verdict string` with `LeftBetter`, `RightBetter`, `Same`
  - `type Judgement struct{ PairID int; Verdict Verdict; At time.Time }`
  - `func Serve(ctx context.Context, address string, pairs []Pair, out string) error`:
    shows tune and check pairs in a shuffled order, then spares only
    while fewer than 12 check pairs have a side; writes `out` after
    each judgement, so a reload resumes; returns when done.
  - `func AstraPick(ctx context.Context, exec codex.Exec, pair Pair) (Verdict, error)`:
    same instruction text as the page, same left and right.
  - `func Agreement(pairs []Pair, paul, other map[int]Verdict, set string) (agreed, decided int)`
  - `func Tune(pairs []Pair, paul map[int]Verdict) Weights`: each weight
    in {0, 1, 2, 3}, the most agreement on `tune` pairs, ties to the
    weights nearest `Even`.
  - `bench calibrate -pairs results/pairs-<date>.json -listen-ip $(tailscale ip -4) -port 8790`
    prints the page's URL and writes `results/calibration-<date>.json`.
  - `bench agree -pairs ... -calibration ...` runs Astra on every
    judged pair, tunes, and writes `results/agreement-<date>.md`: the
    tuned weights, and for the composite and for Astra, agreed of
    decided on check pairs and whether that is at least 80%.

- [ ] **Step 1: Write the failing tests**

```go
func TestThePageShowsOnlyTheTwoExcerptsAndThreeButtons(t *testing.T) // no run, job or score in the HTML
func TestAJudgementIsWrittenAtOnce(t *testing.T)
func TestSparesAreShownUntilTwelveCheckPairsHaveASide(t *testing.T)
func TestServeRefusesAnAllInterfacesAddress(t *testing.T)    // ":8790", "0.0.0.0:8790" refused
func TestAgreementCountsOnlyDecidedPairs(t *testing.T)
func TestTuneUsesOnlyTunePairs(t *testing.T)                // check pairs' verdicts change nothing
func TestTuneTiesGoToEvenWeights(t *testing.T)
```

- [ ] **Step 2: Run them to see them fail**

Expected: FAIL.

- [ ] **Step 3: Implement**

The instruction, on the page and to Astra: "Two pieces of code do the
same job in two different apps. Which reads better: which would you
rather open to understand or change? Left, right, or no difference."
This is user-facing copy: run it through copy review (the
`copy-review` skill) before Step 5.

- [ ] **Step 4: Run the tests to see them pass**

Expected: PASS.

- [ ] **Step 5: Checkpoint 3: Paul judges**

Run (unsandboxed): `cd bench/agent && go run ./cmd/bench calibrate -pairs results/pairs-<date>.json -listen-ip $(tailscale ip -4) -port 8790`
Send Paul the printed URL as a Markdown link with the full URL as its
text. Stop the server when it returns. Then run
`go run ./cmd/bench agree ...`, post the agreement report to a branch
discussion ("Checkpoint 3: calibration"), and tell Paul which layers
are trusted. A layer under 80% is revised once (weights or counts) and
checked on 15 new pairs from a fresh `bench pairs -seed` that passes
every judged `Canonical` as `judged`. If it is still under 80%, it is
not trusted and Task 10 does not use it. The agreement report lists
every attempt.

- [ ] **Step 6: Commit**

Subject: `bench: Paul judges excerpt pairs blind, Astra and counts agree`.

---

### Task 8: Where agents waste turns

**Files:**
- Modify: `bench/agent/internal/transcript/transcript.go` (keep each
  tool result's text, capped at 4,000 runes, and its `is_error`)
- Create: `bench/agent/internal/waste/waste.go`
- Test: `internal/transcript/transcript_test.go` (extend),
  `internal/waste/waste_test.go`, `internal/waste/testdata/`
- Create: `bench/agent/cmd/bench/waste.go`; Modify: `cmd/bench/main.go`

**Interfaces:**
- Produces:
  - `transcript.Tool` gains `Result string` and `IsError bool`.
  - `type Kind string`: `GoCompile`, `Template`, `NameMissing` (locale
    key, icon, asset), `Route`, `Gate`, `Other`
  - `type Outcome string`: `Passed`, `Failed`, `Unknown`
  - `func OutcomeOf(tool transcript.Tool) Outcome`: from the output, not
    only `IsError`, because a piped `go test ... | tail` or
    `make ci ... | tail` exits 0 after a failure. `Failed` on a
    failure line (`FAIL`, `make: *** [...] Error`, a compile error);
    `Passed` only on positive evidence (`ok  ` package lines, `PASS`,
    the gate's own pass line) and no failure line; otherwise `Unknown`.
  - `type Mistake struct{ Kind Kind; Detail string; Message, FixedAt int }`
    (`FixedAt` is the message index where the same check next
    `Passed`; -1 if never, `Unknown` never counts as fixed)
  - `func Find(run transcript.Transcript) []Mistake`
  - `bench waste -rounds ... -briefs groomer -out kept` writes
    `results/waste-kept.md`: per kind, count and median turns to fix.

- [ ] **Step 1: Write the failing tests**

```go
func TestToolResultsKeepTheirTextAndError(t *testing.T)
func TestGoCompileErrorsAreFound(t *testing.T)        // "./bookings.go:12:3: undefined: x"
func TestTemplateErrorsAreFound(t *testing.T)         // the exact messages html/template and Rastrillo's ui print
func TestMissingLocaleKeysIconsAndAssetsAreFound(t *testing.T) // Rastrillo's own messages for each
func TestAGateFailureNamesItsLane(t *testing.T)       // "make: *** [Makefile:58: code] Error 1" -> Gate, "code"
func TestTurnsToFixCountToTheSameCommandsSuccess(t *testing.T)
func TestAPipedFailureIsAFailure(t *testing.T)        // real kept groomer results: is_error false, gate failed
func TestOutputWithNoEvidenceIsUnknown(t *testing.T)
```

The template, locale, icon and asset messages are copied from
Rastrillo's source (`ui/`, `locales/`, the icon and asset helpers) at
the commit this branch is on, one fixture line each.

- [ ] **Step 2: Run them to see them fail**

Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests to see them pass**

Expected: PASS; `make bench` passes (existing transcript tests too).

- [ ] **Step 5: Run it on the kept groomer runs**

Run: `cd bench/agent && go run ./cmd/bench waste -rounds 1,accept-1,cost-1,weight-1 -briefs groomer -out kept`
Expected: a table; read three mistakes of each kind in the transcripts
to check they are what the kind says.

- [ ] **Step 6: Commit**

Subject: `bench: sort the mistakes agents make by kind and time to fix`.

---

### Task 9: The gopls arm

**Files:**
- Modify: `bench/agent/internal/brief/brief.go` (`CarlosGopls Arm =
  "carlos-gopls"`, framed by `arms/carlos.txt`)
- Modify: `bench/agent/internal/runner/runner.go` (`Spec.MCPConfig
  string`, default `{"mcpServers":{}}`, passed to `--mcp-config`)
- Modify: `bench/agent/cmd/bench/run.go` (`-gopls <path>` required
  with `carlos-gopls`; records `gopls version` in `Round.Gopls` and the
  config fingerprint)
- Modify: `bench/agent/internal/round/round.go` (`Round.Gopls`;
  `Cost.FailedBuilds int`, `Cost.GoplsCalls map[string]int`; both in
  `Markdown`; `armsIn`'s fixed list gains `carlos-gopls`)
- Modify every other path that names `brief.Carlos`, so the new arm is
  handled as the Rastrillo arm it is: `appName` (a distinct name, so
  the two arms never deploy over each other), `deployVars` (the demo
  account), `RastrilloVersion` recording, `cleanup`, and the job
  schedule (arms interleaved by run index, the order saved in the
  round's JSON and reused on resume). Find them with
  `grep -rn 'brief.Carlos\|"carlos"' bench/agent`.
- Test: `brief_test.go`, `runner_test.go`, `round_test.go`, `run_test.go`
- Create: `bench/agent/results/spike-gopls.md`

**Interfaces:**
- Consumes: `transcript.Tool.IsError` (Task 8).
- Produces: arm `carlos-gopls`; `FailedBuilds` counts Bash calls
  running `go build`, `go test`, `go vet` or `make` whose
  `waste.OutcomeOf` is `Failed`; `GoplsCalls` counts tool names
  starting `mcp__gopls__`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCarlosGoplsIsFramedExactlyAsCarlos(t *testing.T)
func TestOnlyTheGoplsArmGetsAnMCPServer(t *testing.T)   // args for carlos: {"mcpServers":{}}
func TestTheGoplsArmNeedsAGoplsPath(t *testing.T)
func TestTheGoplsVersionIsInTheFingerprint(t *testing.T)
func TestFailedBuildsAndGoplsCallsAreCounted(t *testing.T)
func TestTheTwoRastrilloArmsGetDifferentAppNames(t *testing.T)
func TestBothRastrilloArmsReachRecordsSummariesAndCleanup(t *testing.T)
func TestTheScheduleInterleavesArmsAndSurvivesAResume(t *testing.T)
```

- [ ] **Step 2: Run them to see them fail**

Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests to see them pass**

Expected: PASS; `make bench` passes.

- [ ] **Step 5: Spike on the smoke brief**

Run (unsandboxed, detached): `cd bench/agent && go run ./cmd/bench run -round spike-gopls -briefs smoke -arms carlos-gopls -n 1 -rastrillo "$(git rev-parse HEAD)" -listen-ip "$(tailscale ip -4)" -gopls ~/go/bin/gopls`
Expected, from the transcript: `go_workspace` reports the app's module
after `go.mod` exists, and at least one other gopls tool returns a
result. Write what happened in `results/spike-gopls.md`. If gopls does
not see the module, or `go_vulncheck` fails on `vuln.go.dev`, apply
the smallest change that fixes it (spec, "The gopls arm"), to both
arms where it can apply, note it in the round header, and repeat the
spike.

- [ ] **Step 6: Commit**

Subject: `bench: a carlos-gopls arm, failed builds and gopls calls`.

---

### Task 10: Round read-1 (checkpoint 5)

**Files:**
- Create: `bench/agent/briefs/lending/questions.txt`, `jobs.txt`,
  `bench/agent/briefs/noticeboard/questions.txt`, `jobs.txt`
- Create: `bench/agent/results/read-1.json`, `read-1.md`,
  `readability-read-1.*`, `comprehension-read-1.*`, `waste-read-1.md`,
  `waste-heldout.md`
- Modify: `bench/agent/README.md` (the scoring commands, in order)

- [ ] **Step 1: Held-out questions, from a fresh agent**

Dispatch a fresh subagent with only `briefs/lending/brief.txt`,
`followup.txt`, `briefs/noticeboard/brief.txt`, `followup.txt`, the
question format from Task 4, and `briefs/groomer/questions.txt` and
`jobs.txt` as examples. It reads nothing else, and writes
`questions.txt` and `jobs.txt` for both briefs. Commit its four files.

- [ ] **Step 2: Run the round**

Run (unsandboxed, `setsid nohup`, resumed after any five-hour stop):
`cd bench/agent && go run ./cmd/bench run -round read-1 -briefs groomer,lending,noticeboard -arms carlos,carlos-gopls -n 3 -j 1 -rastrillo "$(git rev-parse origin/main)" -listen-ip "$(tailscale ip -4)" -gopls ~/go/bin/gopls`
Expected: 18 builds; `results/read-1.md` written.

- [ ] **Step 3: Score every build, and the kept held-out runs**

Run, between builds or after: `bench score`, `bench keys`, `bench
read` (with Task 5's settings) and `bench waste` on `read-1`, and
`bench score`/`keys`/`read` on the kept `lending` and `noticeboard`
runs. Taste: `bench pairs -across carlos,carlos-gopls` (`ChooseAcross`) on
`read-1`; Astra picks if it was trusted in Task 7, otherwise Paul
judges 15 of them. Coverage (missing and oversized jobs per arm) goes
in the report beside the result.

- [ ] **Step 4: Decide gopls against the bar**

In `results/read-1.md`, a "Decision" section: for each of cost and
turns (build plus follow-up), failed builds and comprehension effort
on `lending` and `noticeboard`, the two arms' medians and ranges,
"better", "worse" or "inconclusive" by the Global Constraints' rule,
missing functionality per arm, any measure the noise gate made
inconclusive, and the verdict against the bar.

- [ ] **Step 5: Checkpoint 5**

Post the decision, the readability baseline (`carlos` arm) and
`waste-read-1.md` (groomer only; `waste-heldout.md` reported apart) to
a branch discussion ("Checkpoint 5: read-1") and tell Paul. Run `bench
cleanup -round read-1` and hand Paul its commands; delete nothing
without his word.

- [ ] **Step 6: Commit**

Subject: `bench: round read-1, readability baseline and gopls decision`.
