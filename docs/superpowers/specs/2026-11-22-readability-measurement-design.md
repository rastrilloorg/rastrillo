# Measuring how readable agent-written apps are: design

**Date:** 2026-11-22
**Status:** Draft, for Paul's review.
**Branch:** `readability-measurement`
**Written by:** Claude Opus 5.5 (claude-opus-5-5), from a conversation
with Paul.

## Why

The readable-code rules (`2026-10-29-readable-code-design.md`) were
written from one rewritten Patrix handler, a seven-persona focus group
of agents, and Paul's taste. Four measured limits are enforced by
`rastrillo budget code`; the other ten rules are checked by nobody. No
one has read code that an agent wrote under these rules and judged
whether it is easier to follow. So we cannot say whether the rules work,
and we cannot tell whether the next change to them helps.

Paul asked how we make sure that an app someone generates with
Rastrillo is "delightful and understandable to read from a human point
of view", and then for a plan "backed up by real testing and
benchmarking as we go". Two changes are queued that need that evidence:

1. **Following Go's conventions.** Paul favours Go's community
   conventions (Google's Go style guide as the base) over rule 1's
   "spell every name out". That is a change to the rules, the gate, the
   scaffold and the skill, and it should be shown not to make apps
   harder to read.
2. **gopls for agents.** gopls, Go's language server, has an MCP mode
   (`gopls mcp`) with tools to find references, list a package's API,
   rename safely and report diagnostics. It may cut the turns agents
   spend grepping and rebuilding, but it adds tool definitions to every
   session.

A third, **turning Rastrillo's strings into Go types** (page names,
template fields, locale keys, icon names) so gopls can check them,
should be aimed at what agents actually stumble on, which only the
transcripts can show.

This design is the measuring. The two changes and the third get their
own designs once there is something to measure them with.

## What we have

- **The agent benchmark** (`bench/agent`) runs the same brief through
  arms, records cost, turns, context and messages by stage, and
  measures the live app (page weight, axe, Lighthouse, headers). It
  measures nothing about the code.
- **Kept runs.** `~/.cache/agent-bench/{1,accept-1,cost-1,weight-1}`
  hold about 20 Rastrillo-arm apps with their transcripts
  (`rec/*.jsonl`). All four rounds ran on commits that had the rules in
  `skill/code.md` (step 2) but not the rewritten scaffold (step 4). The
  agents never committed, so each `app/` is the scaffold plus
  everything the agent changed, with no history.
- **Briefs** `groomer` (the dev brief, which changes may be designed
  from) and `lending` and `noticeboard` (held out: run only to accept a
  change). Each has a `words.txt` that the framework must never contain.
- **Paul's bar** for accepting a change on the bench: clearly better on
  two or more metrics, on the held-out briefs.
- **The runner** passes `--strict-mcp-config --mcp-config
  '{"mcpServers":{}}'`, so every arm runs with no MCP servers today.
- **gopls v0.24.0** is installed on docs-t1 (`~/go/bin/gopls`).

## Goals

1. A readability score for the code an agent writes in a Rastrillo
   app, in three layers:
   - **Counts:** cheap, deterministic measures that run on every build.
   - **Comprehension:** readers from another provider answer questions
     about the app from its code alone, with answers checked against
     facts, not judged.
   - **Taste:** Paul judges pairs of excerpts blind. A model's blind
     pairwise judgement stands in for him in later rounds, but only if
     it agrees with him often enough.
2. Every layer is trusted for a stated reason before anything is
   decided with it: comprehension because its answers are checked against
   facts and its noise is measured; counts and taste because they agree
   with Paul's blind judgements.
3. A gopls arm on the bench, and a round that compares it with the
   plain Rastrillo arm on all three briefs. That round also gives the
   first readability baseline on today's scaffold.
4. A report of where agents waste turns in Rastrillo apps, by kind of
   mistake, to aim the strings-to-types work.

## Non-goals

- **Changing the rules, the gate, the scaffold or the skill.** That is
  the conventions design, which comes after this one and is measured
  with it.
- **Shipping gopls in scaffolded apps.** The round decides whether to.
- **Scoring Rastrillo's own framework code.** The score is for app
  code. The framework already has its gate, and its readability
  matters less than what agents copy into apps.
- **A Rastrillo language server or development MCP server.** Not
  until the waste report shows agents need what `rastrillo routes`,
  `budget code` and the rest cannot give them.

## Anti-goals

It fails, even if every part works, if:

- the score rewards code Paul finds worse to read, which is what the
  calibration is for;
- the score can be raised by gaming one count (more comments, more
  tiny functions) without the code getting easier to follow;
- a comprehension answer is graded by a model's opinion of whether it
  "sounds right" instead of against a fact;
- the score is tuned on the same pairs it is checked on;
- the kept runs are modified, or a held-out brief is used to design a
  change.

## Approaches

**Who decides what "readable" means.** Three ways were considered:

- **A, a reader panel from another provider.** Cheap to repeat and
  checkable, but models may not feel "calm" the way Paul does.
- **B, Paul judging pairs blind.** The real ground truth, but slow and
  small.
- **C, counts only.** Cheap and repeatable, but easy to game.

**Chosen (Paul, 2026-11-22): all three, layered.** Counts run on every
build, comprehension is the main score, and Paul's pairwise judgements
check the layers and decide whether a model may stand in for his taste.

**Order of the work.** Measurement first, then the conventions
amendment measured against today's rules, then strings to types aimed
by the transcripts. Chosen by Paul in the same conversation.

## The design

### Held-out briefs stay held out

Steps 1 to 4 read only `groomer` runs, kept or new. The kept `lending`
and `noticeboard` runs and transcripts are not opened until round
`read-1` scores them alongside its own runs. The waste report that aims
the strings-to-types design counts `groomer` only; the held-out briefs'
waste is reported separately and used only to check that design's
result. The held-out briefs' questions are written by a fresh agent
that has read the brief and nothing else: no transcript, no app, no
earlier step's report.

### What code is scored

Only what the agent wrote. For each run, the bench scaffolds a fresh
app at the run's Rastrillo commit with the flags the agent used, and
compares:

- a file the scaffold does not have is the agent's;
- a file the agent changed is scored by its functions: a function that
  is new or whose body differs from the scaffold's is the agent's;
- an unchanged file is not scored.

Never scored: generated files (`// Code generated ... DO NOT EDIT.`),
`gen/`, `vendor/`, `static/`, `testdata/`. Templates are scored by the
comprehension layer only (readers read them), not by the counts. Tests
are scored separately from app code, because their shape differs.

The flags the agent used to scaffold are in its build transcript (the
`rastrillo new` command). A run where that command cannot be found is
reported as unscorable, not guessed.

### Layer 1: counts

Per agent-written function and per run, all computed by pinned
libraries so a number means the same thing in every round:

- the four `budget code` offences (columns, file, func, names), with
  the limits of the scorer's pinned Rastrillo, not the run's;
- cognitive complexity (as `gocognit` computes it);
- deepest nesting;
- duplicated blocks (as `dupl` finds them);
- comment lines as a share of code lines;
- names one or two characters long whose use is more than 10 lines from
  their declaration (scope-proportional naming, the convention Paul now
  favours);
- an interface with one implementation in the module.

The run's summary is the median and the 90th percentile of each count
over functions, plus totals per thousand lines. A **composite**, one
number per excerpt for the calibration, is fixed before Paul sees any
pair.

### Layer 2: comprehension

**Questions are per brief**, written once, about what the app does,
not how: "What does a visitor see after submitting the booking form with
the date left empty?", "Who can delete a listing?". Each asks for a
structured answer (a status code, a role, a field name, a route, yes or
no, a short phrase from a fixed set) so it can be checked without a
model's judgement. About ten per brief. They live in
`bench/agent/briefs/<brief>/questions.txt`, so they are held out with
their brief.

**Each app gets an answer key.** A key-maker (Claude, full budget)
answers every question for that app by reading the code, running its
tests and, for a behavioural question, running the app on `127.0.0.1`
and making the request. A behavioural answer is recorded with the
request and the response that settles it. A key the key-maker could not
settle drops that question for that app.

**Readers are Astra** (`codex exec -m gpt-6-astra`, read-only sandbox,
`-c model_reasoning_effort=high`), because the apps are written by
Claude and the session rule is that an Opus session reviews with Astra.
A reader gets a copy of the app's source and of Rastrillo's module at
the run's commit, in a scratch directory, with one question, a fixed
token budget, and no network. Reading the framework counts towards
effort, as it would for a person. Effort is compared only between runs
on the same Rastrillo commit, so framework changes between kept rounds
do not leak into an app's score. Per run it reports:

- **accuracy:** answers matching the key;
- **effort:** tokens and tool calls spent per question, which is the
  readability signal: an app read in fewer steps to the same right
  answer is the easier one.

**Noise check, before it is trusted:** read the same app twice, and
compare the spread between two reads of one app with the spread
between the three runs of one brief. If the first is not clearly
smaller, the panel needs more readers per question before its numbers
mean anything.

### Layer 3: taste, calibrated by Paul

**Pairs.** 30 pairs of excerpts from the kept runs, each two functions
(or a function and its helpers) that do the same job in two different
apps, cut to at most about 60 lines. They are chosen to spread across
the composite's range, so that some pairs are close and some far
apart.

**Paul judges them blind** on a small page the bench serves on the
Tailscale IP: the two excerpts side by side in random order, and three
buttons: left reads better, right reads better, no difference. It takes
about 20 minutes. Answers go to `results/calibration-<date>.json`.

**The same pairs go to Astra** with the same instruction, blind, in the
same random order.

**Agreement.** 15 pairs are used to settle the composite's weights; the
other 15 check it. Only pairs where Paul picked a side count. If fewer
than 12 check pairs have a side, more pairs are prepared in advance and
shown until 12 do. The composite and Astra each agree with him or they
don't:

- agreement on at least 80% of the decided check pairs: that layer is
  trusted;
- below: the layer is revised and checked again on 15 new pairs, never
  the old ones.

If Astra is trusted, its blind pairwise judgement becomes the taste
score in later rounds, comparing an arm's excerpts with the baseline's
for the same job. If not, taste is judged by Paul on a smaller sample
each round.

### The gopls arm

- **`carlos-gopls`**: the `carlos` arm's prompt, word for word, plus
  `--mcp-config` naming one server, `gopls mcp`, at a pinned gopls
  version on the run's `PATH`. Nothing else differs, except what the
  spike below needs. The round's
  results header records the gopls version.
- **A feasibility spike comes first.** The runner starts `claude` in
  an empty `app/`, so gopls starts before any `go.mod` exists, and its
  instructions ask for `go_vulncheck`, whose host (`vuln.go.dev`) the
  sandbox does not allow. The spike checks that gopls sees a module
  created after it starts, on a smoke brief. If it does not, the
  smallest change that makes gopls work is allowed (start it later,
  pass it an argument, allow `vuln.go.dev`), applied to both arms
  where it can be, and recorded in the round's header. A gopls that
  cannot work is not measured.
- **Its instructions** reach the agent only the way MCP delivers them
  (the server's own instructions); nothing is added to the prompt.
- **New measures for every arm:** builds and test runs that failed (a
  non-zero `go build`, `go test`, `go vet` or `make` in the transcript),
  and calls to each gopls tool.
- **Round `read-1`:** `carlos` at the current Rastrillo commit and
  `carlos-gopls` at the same commit, on all three briefs, three runs
  each, `-j 1`. Eighteen builds. At accept-1's cost (about $7 to $8 a
  build) that is roughly $130 to $150 of the token's usage, spread over
  about two days because of the five-hour limit. Every build is scored
  on all three layers.
- **Accepting gopls:** better on at least two of cost, turns, failed
  builds and comprehension effort, on the held-out briefs, by more than
  the run-to-run range, and not worse on any other measure (including
  readability) beyond that range.

`read-1`'s `carlos` arm is also the readability baseline on today's
scaffold: the kept runs predate it.

### Where agents waste turns

From the transcripts of the kept runs and `read-1`, every failed
command and every reported error is sorted into a kind:

- a Go compile or vet error;
- a template error (an unknown template, a field the data lacks);
- a locale key, icon name or asset path that does not exist;
- a wrong route or path, or a 404 the agent caused;
- a gate lane that failed, by lane;
- anything else.

For each kind: how often, and how many turns passed between the error
and the fix. This extends `internal/classify`. The result is
`results/waste-<round>.md`, the input to the strings-to-types design.

### gopls in our own sessions

Separately, and right away: a project `.mcp.json` in Rastrillo naming
`gopls mcp`, so sessions working on Rastrillo have it. The bench is
unaffected (`--strict-mcp-config`). This is informal; the round is the
evidence.

## Order of work

Each step ends at a checkpoint whose result is reported to Paul.

1. **Counts on the kept runs.** Scaffold comparison and layer 1, one
   report per run. Checkpoint: a table, and a look at the worst and
   best functions it found, to see that the counts point at real
   problems.
2. **Comprehension on the kept runs.** Questions for `groomer` only
   (the dev brief), answer keys, readers, and the noise check.
   Checkpoint: accuracy, effort and noise.
3. **Calibration with Paul.** The pair page, Paul's 20 minutes, the
   agreement check. Checkpoint: which layers are trusted. A layer that
   is not trusted is revised before step 5 relies on it.
4. **The gopls arm and the waste classifier.**
5. **Round `read-1`.** Questions for the held-out briefs are written
   just before this round, without reading any held-out transcript.
   Checkpoint: the round's results, the gopls decision against the
   acceptance bar, and the waste report.

Then the conventions design and the strings-to-types design, each
measured with `read-1`'s `carlos` arm as its baseline.

## Constraints

- The bench's rules hold: held-out briefs are not used to design
  changes; no brief's words enter the framework; runs never read
  `~/.claude`.
- Kept runs are read, never modified. Scoring works on copies in
  scratch space that is deleted afterwards.
- Anything served (the pair page, an app run for an answer key) binds
  to `127.0.0.1` or the Tailscale IP, never all interfaces, and is
  stopped when its step ends.
- `codex exec` runs with the Claude sandbox off and `-s read-only`.
- Rounds run one at a time on docs-t1, detached, and resume after the
  five-hour limit.
- **Scoring's budget (Paul, 2026-11-22):** answer keys run on the
  bench token, in the bench's clean environment, scheduled between
  builds; readers run on Paul's ChatGPT account through `codex`.
  Expected on top of `read-1`'s builds: about $60 to $120 of answer
  keys for about 30 apps, and 300 to 600 Astra reads.

## Decided on the way to the next designs

Recorded here so the next designs start from them:

- Paul favours Go's conventions over spelled-out names
  (2026-11-22), with Google's Go style guide as the base and
  Rastrillo's own rules written as differences from it.
- Still open, for the conventions design: whether 80 columns stays a
  hard limit; whether the function limit counts blank lines (the
  recommendation is not to, so an action's four beats can be spaced
  out); `v` or `visit` for an action's `*action.Visit`.

## Left to the builder

- How the scorer finds functions and their bodies (`go/ast` is
  enough) and which versions of `gocognit` and `dupl` it pins.
- The composite's form, as long as it is fixed before Paul's first
  pair and its weights are settled on the first 15 pairs only.
- How excerpts are matched by job across apps (by route, then by
  reading), and the exact 60-line cut.
- The pair page's look, within: blind, random order, three buttons,
  nothing else on screen.
- The reader's token budget, set from the noise check.
- Where the new `bench` subcommands go and what they are called.

## Calls made in writing this up

- Readers are Astra, by the session rule for cross-provider review.
- A comprehension answer is checked against a key built by running the
  app, not graded by a model.
- 30 pairs, split 15 to tune and 15 to check, and 80% agreement to
  trust a layer.
- Comprehension effort, not only accuracy, is the main readability
  measure.
- The gopls arm changes only the MCP config; its instructions arrive
  through MCP.
- `read-1` runs all three briefs, because accepting gopls is a change
  and changes are accepted on the held-out briefs.
- Steps 1 to 4 read `groomer` runs only; held-out questions are written
  by a fresh agent that has read only the brief (builder check).
- Readers get Rastrillo's module at the run's commit, and effort is
  compared only within one commit (builder check).
- Comprehension is trusted through checkable keys and the noise check,
  not calibrated against Paul (builder check).
- The gopls arm may deviate from the `carlos` arm only as far as a
  feasibility spike shows gopls needs, recorded (builder check).
- Agreement counts only pairs Paul decided, with at least 12 decided
  check pairs (builder check).

Tightened after Astra's review of the plan (2026-11-22):

- "By more than the run-to-run range" means the arms' ranges do not
  overlap; anything else is inconclusive and never a win.
- Cost and turns are build plus follow-up, since the scored source is
  the final app.
- An arm with more missing functionality (questions its app does not
  answer because it did not build that part) cannot be accepted.
- The noise check is a gate: a measure whose reads vary more than half
  as much as runs do is inconclusive until it passes.
- Tune and check pairs come from different runs; a judged pair is never
  shown again; a layer is revised at most once.
- The composite sums a job's counts without dividing by lines, so
  padding and trivial helpers cannot lower it.
- Readers are isolated to their two source trees and a clean codex
  home; keys are bound to a source hash and need recorded evidence.
- Arms are interleaved in the round's schedule.

Changed after the gopls spike (2026-11-22, controller ruling R27):

- The gopls arm measures gopls as its own documentation sets it up, not
  bare. In the first spike the agent was offered gopls's eight tools and
  called none: gopls v0.24.0 sends no MCP instructions, and it missed a
  go.mod created after it started, because Claude Code never answers its
  request for the workspace roots. So the arm now (a) runs gopls behind a
  bench-owned wrapper that restarts it, inside the same MCP session, once
  the app's go.mod appears (the "start it later" this design allows), and
  (b) appends gopls's own instructions (`gopls mcp -instructions`) to that
  arm's system prompt only, as gopls's documentation tells users to load
  them. Both are fingerprinted and named in the round header. This
  replaces "Its instructions reach the agent only the way MCP delivers
  them". In the second spike the agent called gopls tools, and every
  call was answered.
