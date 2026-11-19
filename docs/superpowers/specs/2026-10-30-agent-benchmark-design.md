# Agent benchmark: Rastrillo against an unguided control

Status: design, 2026-10-30. Branch `agent-bench`. Feeds #158 and #159.

## Why

CARLOS wants to say that building on Rastrillo is cheaper for an agent,
and gives a lighter, better app, than building without it. On
2026-10-30 a one-off test said the opposite. An agent on Rastrillo
v0.29.0 took $7.04 to build a small booking app; an unguided control
took $2.77. The Rastrillo app's home page also shipped about 75 kB of CSS
and 68 kB of JS gzipped, where the control shipped 40 kB in total and no
JS.

A turn-by-turn read of the two transcripts (#159, reply 1) found the gap
is mostly context. The Rastrillo agent read about 117k tokens of skill
files, scaffold and module source before it wrote its first model.
Every later turn re-read that context. `make ci` was not the problem.

That test lived in `~/.cache/demo-replays`, outside the repo, and ran
once per side. We can't publish one run, and a Rastrillo change can't be
measured against a harness that lives elsewhere. This design moves the
benchmark into the repo so that a change and its numbers land together.

## Goal

Rastrillo should come out clearly better than the control on more than
one metric: build cost, first-load page weight and quality. The
benchmark is how we find out whether it does, and it's the evidence
behind any public claim.

## Non-goals

- A general agent-evaluation product. This measures Rastrillo.
- Other models or harnesses. One model per round, named in the results.
- Tuning the prompts. The briefs are fixed once written; see
  "Overfitting".

## Anti-goals: things that make it worthless even if it runs

- The two arms are treated differently. Both get the same sandbox, the
  same environment notes and the same model. Both deploy. Both get the
  same follow-up prompt.
- Rastrillo is tuned to the benchmark's apps rather than to apps in
  general.
- A result that can't be reproduced from what's committed.

## Shape

    bench/agent/
      README.md              how to run a round; what each number means
      briefs/<brief>/        one directory per app brief
        brief.txt            the app, in the words a person would use
        carlos.txt           the Rastrillo arm's framing (see Arms)
        control.txt          the control arm's framing
        followup.txt         the second prompt ("make it look good")
        pages.txt            the page roles to weigh and audit
      env.txt                environment notes appended to both arms
      results/<round>.json   one file per round, committed
      results/<round>.md     the same, as tables a person reads
      ...tools...

Transcripts, apps and screenshots stay on disk under
`~/.cache/agent-bench/<round>/` and are not committed. They run to
several MB per run.

**The tools are the builder's call**, but they're checked: the
transcript analysis runs in `make ci` against a small fixture transcript,
so a change to the classifier can't silently change the numbers. The
existing Python in `~/.cache/demo-replays/tools/` is the starting point,
not the design. Write the analysis in Go unless there's a reason not to.
If a separate module keeps it out of the root gate's compile, that's
fine, provided its tests still run in `make ci` through `ci.d`.

## Briefs

A brief is the app request, written once, in a person's words. Each arm
gets the brief plus its own framing:

- **carlos.txt** says to use CARLOS principles and Rastrillo. It says to
  read the getting-started skill and Rastrillo's SKILL.md first, and to
  deploy to Carloku as app `<app>` on the demo account. That's the
  framing the 2026-10-30 run used.
- **control.txt** says to build, test and deploy to `<platform>`. The
  platform is the one an unguided agent picks (see Deploy).

Three briefs to start, deliberately different in shape:

| brief | set | shape |
|---|---|---|
| `groomer` | dev | public booking form with time slots; owner admin for services, hours and bookings; username sign-in, no email |
| `lending` | held out | internal tool: members sign in, borrow and return equipment; an admin sees overdue items and exports CSV; roles |
| `noticeboard` | held out | read-heavy public site: listings with search and filters, a contact form, light admin; mostly static pages |

`groomer` is the 2026-10-30 prompt, unchanged apart from moving the
framing into the arm files. The held-out briefs are written now, in this
branch, at the same length and level of detail as `groomer`. They are
reviewed for clarity, and frozen before #159's changes start.

## Overfitting

The risk the user named is optimising Rastrillo for one app. The
defences:

1. **Dev and held-out sets.** Changes for #158 and #159 are designed
   from `groomer` transcripts only. The held-out briefs are run only to
   accept a set of changes, before a release or a public claim. A change
   that improves `groomer` but makes either held-out brief worse doesn't
   ship as-is.
2. **No domain words in what the agent reads first.** Each brief carries
   `words.txt`, an explicit list of its domain words (for `groomer`:
   groom, dog, salon, pet). Generic words like sign in, admin, search
   and listing don't go on it. A test fails if any listed word appears in
   SKILL.md, `skill/`, or the files `rastrillo new` writes. It doesn't
   scan `docs/site/` or `examples/`, which already mention salons and
   borrowing for their own reasons. (My call on the scope.)
3. **Each change names a general need.** A SKILL.md or scaffold change
   motivated by a transcript names the need in general terms in its
   commit ("an app that signs in by username, not email"), not the app
   that hit it.
4. **Rotation.** When a held-out brief has been used to accept changes
   three times, it moves to dev and a new held-out brief replaces it.

## Arms and fairness

- **Model and harness:** the same `claude -p` invocation for both arms:
  the model pinned, no MCP, no hooks, `--disable-slash-commands`, and
  the same appended environment notes. Record `claude --version` and the
  model in the round. If the version changes partway through a round,
  stop and restart the round, so the two arms never run on different
  versions. (My call.)
- **Config: clean, decided by the user.** The 2026-10-30 runs were not
  clean. `run-phase.sh` set `CLAUDE_CONFIG_DIR` to the operator's own
  config, so both arms loaded eight plugins (frontend-design, impeccable
  and superpowers among them), the Concise output style, a personal
  CLAUDE.md and that config's sandbox allowlist. Both arms had them
  equally, but the issues' "no plugins" was wrong, and it can't be
  reproduced from the repo. So each round gets a fresh config directory,
  built from settings committed under `bench/agent/`: no plugins, the
  default output style, no CLAUDE.md, and a sandbox allowlist covering
  both arms' hosts. Auth is a long-lived token the operator creates with
  `claude setup-token` and passes in through the environment at run
  time. The harness never writes it to the config directory, the repo or
  the run records, and never reads the operator's own config. The
  harness also copies nothing else from the operator's environment:
  their `~/.claude/settings.json` holds unrelated secrets in `env`.
- **Sandbox:** the same for both arms. The 2026-10-30 runs both lost
  turns to taking screenshots in the sandbox. That's a fact about the
  harness, and it stays equal across arms rather than being fixed for
  one.
- **Rastrillo under test:** a round names a Rastrillo version: a tag,
  or a commit on a branch. For an unreleased commit, the agent must
  still get Rastrillo the way a user would: `go install ...@<version>`
  and SKILL.md over HTTP. The builder picks the mechanism, for example
  a local module proxy and a local file server on 127.0.0.1 with the
  SKILL.md URL rewritten. The prompt text may differ from the released
  prompt only in that URL and the version, and the round records the
  difference. The CARLOS getting-started skill is external. It's read
  as published, and its commit is recorded.
- **Control reuse:** the control doesn't depend on Rastrillo. A round
  may reuse the control runs from an earlier round with the same model,
  the same `claude --version` and the same brief, and it says so. Rerun
  the control when any of those change.

## The spec arm (added 2026-10-31)

The control isn't held to anything, so its low cost partly reflects doing
less: in the smoke run it sent no security headers where Rastrillo sent
six. A third arm, `spec`, builds without a framework but must meet
https://spec.carlosframework.com. The spec names no framework. It deploys
where the control does, so control against spec measures the cost of the
CARLOS bar, and spec against carlos measures what Rastrillo saves in
meeting it. The carlos arm's framing is unchanged from round 1, so rounds
stay comparable. Only the spec arm's sandbox may reach the spec site,
so the shared settings, and with them the reuse of round 1's control
runs, stay as they were.

## Deploy

Both arms deploy, because the 2026-10-30 Rastrillo run paid about 14
turns for a deploy the control never did.

- **Rastrillo arm:** Carloku, demo account (short id `bdf`), app name
  `<brief>-<n>` per run, so parallel runs don't collide.
- **Control arm:** before the first round, ask a clean agent five times
  with the `groomer` brief and no framing: "Which hosting platform would
  you deploy this to? Answer with one name." Take the most common
  answer, then the user creates a demo account there. The agent never
  signs up for anything. The control framing then says the agent is
  signed in, exactly as the Carloku framing does. Record the poll's
  answers in the round. Poll again when the model changes.
- **Cleanup:** the latest accepted round stays live, because it's what a
  published comparison links to. Earlier rounds' apps are deleted after
  they're measured, on both platforms.

## Measurements

Each run reports the following. A round reports the median and the range
of three runs per arm per brief.

**When (my call).** Weight and quality are measured twice: after the
build prompt is deployed, and again after the follow-up is deployed.
The harness waits between the two prompts, rather than chaining them the
way `chain.sh` does. The control's follow-up says "deploy it again", the
same as the Rastrillo arm's, now that both arms deploy.

**Which pages (my call).** Each agent chooses its own routes, so
`pages.txt` names page roles, not paths: for example home, a public form
and an owner page. After each deploy, the harness resumes the finished
session with one extra prompt. That prompt is fixed and identical across
arms, and its tokens are excluded from build cost. It asks for JSON
mapping each role to its live URL, plus the demo sign-in details. The
harness checks that each URL answers before measuring it, and keeps the
mapping in the run.

**Failures (my call).** A harness failure is re-run and doesn't count:
an API error, a usage limit, a crash before the agent's first tool call,
or the harness's own bug. An agent failure is a result and counts in
the median, with its cost: it didn't deploy, sign-in doesn't work, or a
page role is missing. Its missing weight and quality numbers are
reported as failures, not left out. The round's table shows failures per
arm next to the medians.

**Build cost**, per prompt (build, then follow-up):

- assistant messages (turns), tool calls, output tokens, cache reads,
  cache writes, and dollars at the model's published rates. Look the
  rates up; don't remember them. Record them in the round.
- context size at the first write to the app's own source
- time from start to finish
- stages, classified per message, not per tool call: reading framework
  docs or source, reading the app, scaffolding, writing, building and
  testing, gates, screenshots, deploy, live checks. Deploy and live
  checks are also reported separately, so Rastrillo's build cost can be
  compared with and without the platform.

**First-load weight**, live, per page in `pages.txt`: the bytes over the
wire (as served, compressed) and the decompressed size, split by HTML,
CSS, JS, fonts and images, counting everything the page loads on first
paint. That includes scripts loaded by scripts, so measure in a real
browser, not by scraping tags the way `weight.py` does. Signed-in pages
sign in with the demo details the brief makes the app show.

**Quality**, live:

- axe-core violations per page, by impact. Use the axe build the
  design-system tests already vendor if there is one.
- Lighthouse performance, accessibility, best-practices and SEO scores
  per page. Lighthouse is an npm download. Ask before installing it if
  it is over a few hundred MB.
- security headers on the home page: CSP, HSTS, X-Content-Type-Options,
  Referrer-Policy, frame protection, Permissions-Policy. Each is present
  or absent, with its value.
- the deploy works: the home page answers 200 over HTTPS, and sign-in
  with the demo details reaches a signed-in page. Keep screenshots of
  each page for a person to look at. There's no scripted test of each
  app's features, because the two arms' markup differs.

## Rounds

- **Round 0** imports the 2026-10-30 transcripts as they are: one run
  per arm, `groomer` only, Rastrillo v0.29.0, no deploy for the
  control, and the operator's plugins loaded in both arms. It's labelled
  as such. It's history, not a baseline.
- **Round 1** is the baseline: current `main` and all three briefs,
  three runs per arm, both arms deploying. That's about $45 for
  `groomer` and $90 for the two held-out briefs, at round 0's rates.
- Then each #159 or #158 change set gets a `groomer`-only round, and the
  held-out briefs run once before a change set is accepted.

## Left to the builder

- tool language and layout inside `bench/agent/`, within the rule that
  the analysis is tested in `make ci`
- the mechanism for serving an unreleased Rastrillo
- how runs are scheduled and parallelised on this shared machine (bind
  to 127.0.0.1 and clean up, as `env.txt` already tells the agent)
- the exact wording of the two held-out briefs, within the shapes above
