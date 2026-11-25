# Agent benchmark

This benchmark gives the same app brief to up to four agents, called arms:

- **carlos** builds on Rastrillo and deploys to Carloku.
- **carlos-gopls** is carlos with gopls, set up the way gopls's own docs set it up. Its prompt is carlos's, word for word. It differs from carlos in two ways, and both are recorded in the round's header:
  - **One MCP server, `gopls mcp`.** It runs behind `cmd/goplsmcp`, a small wrapper in this module. A gopls started in the empty app never sees the module the agent scaffolds there: gopls watches for new modules only in directories a client names as roots, and Claude Code's MCP never lets it ask. So once the app's go.mod appears, the wrapper starts gopls again in that module, behind the same session. It waits until no request is in flight, and gives the new gopls the session messages the client sent. Each run logs this to `rec/goplsmcp.log`.
  - **gopls's own instructions.** gopls sends none over MCP. Its docs say to load the output of `gopls mcp -instructions` into the session, so that text follows the environment notes in this arm's system prompt. carlos's system prompt is unchanged.

  It deploys to the same Carloku account as carlos, under its own app names (`groomer-gopls-r<round>-<n>`), so the two never meet.
- **control** gets no framework guidance and deploys to the platform you choose.
- **spec** gets no framework either, but must meet the CARLOS app specification at https://spec.carlosframework.com. It deploys where the control does, so the only difference between the two is the bar.

Every arm deploys its app, then gets the same follow-up prompt. The benchmark records what each build cost and measures each live app.

The design is in `docs/superpowers/specs/2026-10-30-agent-benchmark-design.md`. Results are in `results/`, one JSON file and one Markdown file per round.

## Before a round

1. **Create a token.** Run `claude setup-token` and export the result as `CLAUDE_CODE_OAUTH_TOKEN`. The bench passes that one variable to `claude` and nothing else from your environment. It never reads your `~/.claude`.
2. **Install Lighthouse**, once:

       npm install --prefix ~/.cache/agent-bench/tools lighthouse@12

   Without it, rounds still run, but each one carries a note that Lighthouse wasn't run.
3. **Choose the control's platform** with `go run ./cmd/bench poll`. It asks five fresh sessions where they would deploy the `groomer` brief and writes their answers to `results/poll-<date>.json`. Create a demo account on the most common answer and sign in to its CLI on this machine.

## Running a round

Run every command from `bench/agent`.

    go run ./cmd/bench run -round 1 \
      -briefs groomer,lending,noticeboard \
      -rastrillo "$(git rev-parse HEAD)" -listen-ip "$(tailscale ip -4)" \
      -platform Fly.io -control-account 'the Fly.io organisation "demo"' \
      -control-hosts '*.fly.dev,api.fly.io' -poll poll-2026-10-31.json

- **`-rastrillo`** is `latest`, which gives the agent what `go install ...@latest` returns, or a commit. A commit is served as an unreleased version, `v0.N.0-bench.g<sha>`, through a local module proxy. Its SKILL.md is served over HTTP on `-listen-ip`. The agent works in a sandbox with its own network, so that address must be one it can reach, such as the machine's Tailscale IP, not 127.0.0.1.
- **`-arms`** picks which of `carlos`, `carlos-gopls`, `control` and `spec` run. The default is `carlos,control`. `control` and `spec` both need `-platform` and `-control-account`.
- **`-gopls`** is the gopls binary the `carlos-gopls` arm runs, such as `~/go/bin/gopls`, and that arm needs it. The round builds the wrapper into `~/.cache/agent-bench/<round>/bin/` and names it in the arm's MCP config, with gopls's absolute path, the app's directory and the Go settings the agent gets. `gopls version` and the sha256 of `gopls mcp -instructions` go in the round's header; the instructions text is saved beside the runs. The version, the instructions and the wrapper's source are all in the round's fingerprint, so a resumed round cannot continue on another gopls. A round that ran gopls needs `-gopls` on every resume, even one whose `-arms` leaves `carlos-gopls` out.
- **The gopls arm is checked on every prompt.** claude's own init record must show gopls connected, and no other server; any other arm must show no server at all. The wrapper logs a line with `goplsmcp: trouble:` whenever gopls was missing for part of a session: it did not start, it died, or it could not be started again in the module (the session then keeps the old gopls). A restart waits at most 30 seconds for the requests in flight; one still unanswered then, or a session that ends first, is trouble too. Each time a gopls starts or restarts in the module, the wrapper logs `goplsmcp: module active: gopls <n> in <module>`. It keeps watching after a restart, so a module the agent moves (scaffolded into `app/<name>`, then moved up into `app/`) gets a gopls started where it now is. The wrapper restarts gopls only once go.mod and go.sum have been unchanged for three seconds, and a request the client cancels no longer holds the restart off.
- **A prompt counts only on the log's evidence.** After each prompt, the run is a harness failure, retried like an API error, if `rec/goplsmcp.log` holds any trouble line, cannot be read, or, when the app's go.mod and go.sum had settled more than five seconds before the prompt ended, holds no module active line from this prompt naming the app's module as it is at the end. The run would otherwise be carlos under the gopls label.
- **`-n`** is the number of runs per arm per brief. The default is 3.
- **`-j`** is the number of runs at once. The default is 2. Measurements never overlap each other, but they can overlap another run's build, which makes Lighthouse's performance score noisier. Use `-j 1` for a round you mean to publish.
- **`-reuse-control 1`** copies round 1's control runs instead of paying for new ones. That's allowed only when the model and the `claude` version match.

Runs go in an interleaved order: run 1 of each brief for every arm, then run 2, and so on, so `carlos 1`, `carlos-gopls 1`, `carlos 2`. A drift over a round's days (the model, Carloku, the token's limit) then falls on every arm alike. The order is saved in `results/<round>.json` when the round starts, and a resume keeps it, whatever order `-arms` lists them in. A resume that asks for more runs adds them after the saved ones.

Runs go to `~/.cache/agent-bench/<round>/`: the app, the transcripts, and screenshots of each page. None of that is committed. If a round stops partway through, run the same command again. It picks up where the round stopped.

The round stops straight away if a run loads a plugin, uses an output style, or reports a different `claude` version from the round's. Each of those means the config is no longer clean. Other harness failures, such as an API error or a usage limit, are retried twice. Anything the agent gets wrong is a result, not a failure of the harness: no deploy, a broken sign-in, a page it can't name.

## What the numbers mean

- **cost ($)** and **turns** come from `claude` itself, from the result record at the end of each prompt.
- **context at first write** is how much context the agent had read when it first wrote a file.
- **failed builds and test runs** counts the `go build`, `go test`, `go vet` and `make` calls that failed, read from their output as `bench waste` reads it (below), so a gate piped to `tail` still counts. Rounds before this measure show 0 until `bench rescore` reads their transcripts again.
- **gopls calls** counts calls to each gopls tool. A round with no gopls has no such rows.
- **messages by stage** is what each assistant message was for, judged by its first tool call: reading the framework, reading the app, scaffolding, writing, building and testing, gates, screenshots, deploying and live checks.
- **over the wire (kB)** is what a first visit to that page downloads, compressed as sent. It's measured in a real browser, so scripts loaded by other scripts are counted too.
- **axe serious or critical** is the number of axe-core rules the page breaks at those two impact levels.
- **Lighthouse** scores run from 0 to 100. Lighthouse runs on public pages only.
- **security headers present** counts the protective headers on the home page: CSP, HSTS, `X-Content-Type-Options`, `Referrer-Policy`, frame protection and `Permissions-Policy`.

Each cell is the median, with the range in brackets. The first row of each table counts the runs that had any agent failure. The failures themselves are listed under the table.

## Not tuning Rastrillo to the briefs

- `groomer` is the dev brief: changes to Rastrillo are designed from its transcripts. `lending` and `noticeboard` are held out. They run only to accept a set of changes. A change that helps `groomer` but makes either held-out brief worse doesn't ship as it stands.
- Each brief's `words.txt` lists its domain words. `TestFrameworkCarriesNoBenchmarkWords` in `cmd/rastrillo` fails if any of them appear in SKILL.md, `skill/`, or anything `rastrillo new` writes.
- When a transcript prompts a change, name the general need in the commit ("an app that signs in by username, not email"), not the brief that ran into it.
- After a held-out brief has been used to accept changes three times, it becomes a dev brief and a new one is written to replace it.

## After a round

`go run ./cmd/bench cleanup -round 1` prints the commands that delete the round's deployed apps. It runs none of them. Keep the latest round's apps up if a published comparison links to them.

## Scoring

Scoring reads only the code the agent wrote. `internal/authored` finds it without git history:

1. It reads the build transcript for the `rastrillo new` call that made the app: the last one whose name is the last element of the app's module path, ignoring `--help` calls and anything inside a commit message or heredoc. Two such calls in the last command with different flags make the run unscorable.
2. It builds `cmd/rastrillo` from the run's own version in the module cache and runs `new` again with the same flags.
3. It compares the app with that scaffold. A file the scaffold lacks is the agent's, every function of it. In other files, a function is the agent's if it is new or its tokens changed. Formatting and comments don't count, and nor do imports or types alone.

Units are keyed as `internal/codelimits` keys them (`Name`, `Recv.Name`, `<variable>`, `<variable>.func<N>`, and `<variable>.action<N>` for an action, whether its `*Visit` is `*action.Visit` or a dot-imported `*Visit`), so its offences can be matched to them. `internal/authored/testdata/dotaction` is measured by both in one test. No kept app dot-imports the action package. Generated files, vendored files, and anything under `gen/`, `static/`, `vendor/` or `testdata/` are never the agent's. Test files are marked, since the limits and the reader treat them differently.

### Scoring a round, in order

These are the commands that scored round `read-1`, in the order to run them. Each is described in its own section below.

    go run ./cmd/bench rescore -round read-1
    go run ./cmd/bench score -rounds read-1 \
        -briefs groomer,lending,noticeboard -out readability-read-1
    go run ./cmd/bench score -rounds 1,accept-1 \
        -briefs lending,noticeboard -out readability-kept-heldout
    go run ./cmd/bench waste -rounds read-1 -briefs groomer -out read-1
    go run ./cmd/bench waste -rounds read-1 \
        -briefs lending,noticeboard -out heldout
    go run ./cmd/bench keys -rounds read-1 \
        -briefs groomer,lending,noticeboard -j 3 -out keys-read-1
    go run ./cmd/bench read -rounds read-1 \
        -briefs groomer,lending,noticeboard -reads 3 -budget 180000 \
        -out read-1

- `rescore` goes first, because it and `report` write `results/<round>.md` again from the JSON. The round's decision goes in `results/<round>-decision.md`, written by hand; neither command touches it, and `results/<round>.md` ends with a line pointing to it whenever it exists. Round `read-1`'s is `results/read-1-decision.md`.
- `waste` per arm (`-arms carlos -out heldout-carlos`, then `-arms carlos-gopls -out heldout-carlos-gopls`) gives the per-arm mistake counts a decision quotes; `results/waste-heldout.md` is both arms together.
- The kept held-out runs get counts only: their apps are on older Rastrillo versions, so they are never compared with the round's.
- `waste` reports the dev brief and the held-out briefs apart, because changes are designed from `groomer`'s mistakes only.
- `keys` runs Claude on the bench token, one session per app, so it needs the token's usage limit to have room. `read` needs the keys and runs on the operator's codex login. Both pick up where they stopped when run again.
- A kept run is one the round accepted: `score`, `keys`, `read`, `pairs` and `waste` find runs through the round's records, `results/<round>.json`, never by directory name alone. A run directory no record accepted (a failed last attempt left at the run's own path, a build still going, a run the round left out) is skipped and named on stderr; a reused run is read in the round it was made in. Round `accept-1` keeps five such directories (`lending-carlos-2` and `-3`, `noticeboard-carlos-1` to `-3`), which its notes say are out.
- Taste pairs across arms (`bench pairs -across carlos,carlos-gopls`) are worth judging only once a taste layer has been trusted in calibration. Neither was for `read-1`, so it has none.

### Counts

`bench score` counts the authored code of kept runs with `internal/counts`:

    go run ./cmd/bench score -rounds 1,accept-1,cost-1,weight-1 \
        -briefs groomer -out readability-kept

It reads each run's directory under `~/.cache/agent-bench/<round>/` (never writing there), finds the authored code as above with the scaffold made again in a scratch directory, and writes `results/<out>.json` and `.md`: one row per run, and a named "unscorable" row, with the reason, for any run whose scaffold call cannot be found or whose Rastrillo version is not in the module cache. `-arms` picks the arms. Its default, here and in `keys`, `read`, `waste` and `pairs`, is `carlos,carlos-gopls`, so a round with the gopls arm is never read as carlos alone. Text you write after a `## Checkpoint` heading in the report is kept when it is run again.

Per function, from parsing alone (so a file that does not compile is still counted): cognitive complexity (gocognit), nesting depth (if, for, switch and select count; an else if does not), one- and two-letter names used more than 10 lines of code from where they are declared, lines inside a clone of at least 100 nodes (dupl v1.1.0, its default), and lines over a limit (wide lines, one-letter names, lines past the length limit). Blank lines and comments change none of these but `Lines`. Per app: the `codelimits` offences inside authored code (a length offence on a literal in a function, `Name.action2`, belongs to `Name`; a file offence counts when the file is whole or holds authored code), comment lines over code lines, and the interfaces the agent wrote, in files it wrote whole, that exactly one type in the app implements. That last one needs the app to type-check; when it does not, it is "unavailable", never zero. Tests are summarised and totalled apart from app code: the app table's files, code lines, offences and comment share are the app's own, and the report gives the tests' in a table of their own.

### Where agents waste turns

`bench waste` sorts the mistakes agents made while building from the kept transcripts, to show which ones a design that turns strings into Go types would remove:

    go run ./cmd/bench waste -rounds 1,accept-1,cost-1,weight-1 \
        -briefs groomer -out kept

It reads each run's `rec/build.jsonl` and `rec/followup.jsonl` and makes no paid call. A mistake is a Bash call that runs `go build`, `go test`, `go vet`, `go run`, `go generate`, `make`, `staticcheck` or a `rastrillo` check (generate, budget, routes, migration, doctor) as a command, read with the shell parser, so `grep "go test" README.md` is not one. Whether it failed comes from the output (`internal/waste`'s `OutcomeOf`), not only `is_error`, because a gate piped to `tail` exits 0 and a pipe to `grep` that finds nothing exits 1:

- A failure line (`FAIL`, `make: *** ... Error`, a compile error) is Failed. So is `is_error` when the command has no pipe, `;`, newline, `||` or `&`, so its exit status is its check's own (`&&` chains are fine). Claude Code starts a failed command's result with `Exit code N`, the status of the last simple command in the list, so that line is a failure only when the last command is a check: `cd app; go test ./...` failed, but `make ci; which chromium` is not a failed gate because `which` found nothing.
- An `ok` line, `PASS` or the gate's own last line is Passed, and so is silence from such an unpiped check, since `go vet` and `go build` print nothing when they pass.
- Anything else is Unknown, which never counts as a fix. So are timeouts, kills and a read-only Go cache: the machine failed, not the code.

Each mistake has a kind: `go-compile`, `template` (html/template, or compiled templates out of date), `name-missing` (a locale key, icon or asset), `route`, `gate` (a make lane, named, that failed for none of those), or `other` (a test's assertion, a bad flag). The kind is the most specific one the output shows. Turns to fix are the messages from the failure until the same check next passes. A passing `go test` fixes any Go mistake, and a silent `go build` or `go vet` fixes a compile error only. The kept runs hold no locale-key or asset mistake, so those fixtures are the formats in Rastrillo's source (`internal/generate/catalogs`, `internal/designsystem/hashed.go`, the only asset message, the gallery's) with made-up arguments; the gate's icon message (`icons-gate.json`) is a real kept result, and the Fiesta icon case is the format in `internal/iconsets/iconsets.go` with an invented slug. `results/waste-<out>.md` gives each kind's count, how many were fixed, and the median turns to fix, with three examples of each. Tool results keep up to 4,000 runes, the first 2,000 and the last 1,993, so a long result's closing lines survive.

### Answer keys

Each brief has about ten questions about what its app does, in `briefs/<brief>/questions.txt`: blocks of `id`, `kind` (`status`, `choice`, `yesno`, `route` or `word`), `ask`, `choices` for a choice, and `behavioural: yes` for a question about what a request gets. `bench keys` has Claude (Opus, on the bench token, in the clean environment a run gets) answer them for each kept run:

    go run ./cmd/bench keys -rounds 1,accept-1,cost-1,weight-1 \
        -briefs groomer -j 3 -out keys-groomer

The key-maker works on a scratch copy of the app, may read it, run its tests and start it on `127.0.0.1`, and has Rastrillo's source at the app's version beside it. Go runs offline (`GOPROXY=off`, `-mod=readonly`), and its `TMPDIR` is inside the scratch directory, so everything it builds or leaves goes with it. Its keys go to `~/.cache/agent-bench/scoring/<round>/<run>/keys.json`, with its transcript beside them, outside the runs (never written to) and outside anything a reader is shown. A run that already has keys is not keyed again. `keys.json` records a hash of `questions.txt` and of the key-maker's instructions; when either has changed since, the run is refused until its `keys.json` is deleted and made again. `-out` writes a summary to `results/<out>.md`. The command exits non-zero if any run has no keys.

A key is unsettled, and kept as such, when the key-maker says it could not settle it, when the app's source (its `.go`, template, style, script and config files) changed while it worked, when a behavioural key's evidence does not quote a request it really made and that request's output, or when a choice answer is not one of the choices. The quote must be a whole line of a command it ran (or a part of one between `;` and `&&`, never a line of a heredoc), and a `curl` or `wget` naming `127.0.0.1` or `localhost`; and at least one of the output lines quoted after it must be in what that shell call printed, as the transcript recorded it (a line may leave text out with `...`; the parts it keeps must match). Keys made before are held to the same check against the `keymaker.jsonl` kept beside them whenever `keys` or `read` reads them, and a behavioural key with no transcript beside it is unsettled. Re-checking the 12 kept key files this way unsettled none of their keys. Before the key-maker starts, the bench builds the copy with `go build ./...`; if that fails, every behavioural key is unsettled, never "not built". A key for something the app does not do is settled, with the answer `not built`.

Readers (`internal/codex`) run `codex exec -s read-only` inside bubblewrap, which shows them only their directories (as `/work`), codex itself, the system's libraries and a scratch `CODEX_HOME` with a copy of the operator's login and an empty config. Nothing else of `$HOME` is visible, and the keys never are. Codex reaches its model over the network; the commands it runs do not. If codex refreshes the login while it runs, the refreshed one is copied back to `~/.codex/auth.json`, because the old refresh token stops working. It goes back only if `auth.json` still holds the login that was copied, and every reader in every bench process copies the login and puts one back under one lock, `~/.codex/auth.json.bench.lock`, so concurrent readers never write over a newer login or leave half of one. A write-back that fails is the reader's error.

### Reading

`bench read` has Astra (`gpt-6-astra`, high effort) answer each question from each kept app's code, and grades the answers against the keys:

    go run ./cmd/bench read -rounds 1,accept-1,cost-1,weight-1 \
        -briefs groomer -reads 2 -budget 200000 -out kept

Each reader gets one question and a scratch directory holding `app/`, a copy of the app without its `.git`, and `rastrillo/`, the framework at the app's version from the module cache. It cannot run the app or reach the network, so it answers from the code, and replies `{"answer": "..."}`. Questions whose key is unsettled are not asked. `-reads` is how many times each question is asked of each app, `-j` (default 1) how many readers run at once (parallel readers share one codex login and its refresh token; raise `-j` only knowing a parallel refresh can log the operator out), `-timeout` each reader's time.

Tokens are codex's input tokens (cached included) plus output tokens. A reading that spends more than `-budget` is graded wrong. Grading is by kind: a status may be given by number or name (`422` is `Unprocessable Entity`), yes or no ignores case and full stops, a route's parameters match whatever they are called (`/bookings/:id` is `/bookings/{reference}`, and `{rest...}` is a parameter too) and its method counts only when both sides give one, and a choice must be one of its choices. A parameter matches another parameter but never a concrete segment: `/booked/ABC123` is not `/booked/{ref}`. Go's `{$}` only marks the end of a pattern, so `/{$}` is `/`. A status in a sentence is the code beside its name, else the last code that is not part of a path. A key for something the app does not do is answered `not built`.

Accuracy is right answers over all the brief's questions, so an unsettled key's question counts as not right for that app rather than being dropped. Effort (tokens and commands) is compared only on questions both sides answered right, and only between runs on the same Rastrillo version.

Each reading is saved as it comes in to `~/.cache/agent-bench/scoring/<round>/<run>/readings.json`, ungraded, so an interrupted read picks up where it stopped and only asks what is missing. Grading happens when the report is written, at that command's `-budget`: running again with another budget re-grades without reading again. A `readings.json` made by another reader, prompt, question set or app source is refused until it is deleted.

The report, `results/comprehension-<out>.json` and `.md`, has each read of each app, which questions each app's reads got right, effort between runs, the budget (the 95th percentile of tokens on answers that were right before any budget, rounded up to the next 10,000), and the noise check: per app the spread between its reads, per brief the spread between its runs, and the gate. A measure passes when the median difference between two reads of one app is at most half the median difference between runs of one brief. One that fails is inconclusive for every decision until it passes. So is one whose runs do not differ at all: its verdict is "cannot separate runs", never a pass. Only complete apps go into the gate: an app is complete when every read asked for holds a reading of every settled question. A reading that failed would otherwise count as a wrong answer, the same one in every read, and could pass the accuracy gate on harness failures alone. An incomplete app is marked so in the report, with how many readings it lacks. The gate judges accuracy without the budget, grading an answer over it on what it says, so that token noise, which has its own gate, cannot pass for a difference in understanding; the accuracy reported still applies the budget. Text after a `## Checkpoint` heading is kept when the report is written again. The command exits non-zero if any reading failed; those are listed in the report and asked again next time.

### Pairs

Counts only matter if they agree with a person about which code reads better. `bench pairs` prepares that check: it cuts the code that does one job out of each kept app, and pairs two apps' excerpts of the same job, to be judged blind.

    go run ./cmd/bench pairs -rounds 1,accept-1,cost-1,weight-1 \
        -briefs groomer -seed 20261122 -j 3

The jobs are in `briefs/<brief>/jobs.txt`, one a line: showing free slots, booking, signing in, and so on. For each app, Claude (Opus, on the bench token, in the clean environment the key-maker gets) reads a copy of the app and names the functions that do each job. It may name only functions the bench lists for it, as `path:Name`, and an answer naming anything else, or leaving a job out, is refused. Its answer goes to `~/.cache/agent-bench/scoring/<round>/<run>/jobs.json`, with its transcript beside it, and records hashes of `jobs.txt`, of its instructions and of the app's source. An app whose jobs were found before is not asked again; when any of the three, or the model, has changed, the run is refused until its `jobs.json` is deleted. Ctrl-C (or SIGTERM) stops every session it started and removes their scratch; the jobs found by then are kept, and running again finds only the rest. `bench keys`, `read`, `run` and `poll` stop the same way.

Of the functions named, only those the agent wrote are kept. An excerpt is those functions whole, doc comments included, in source order, each file's path on a line before its functions. One with none of the agent's code is "missing"; one over 60 lines is "too long". Neither is ever paired.

Each excerpt has a composite: the weighted sum of its functions' cognitive complexity, nesting, lines over a limit, duplicated lines and far short names, helpers included. Lower reads better. It is never divided by lines or functions, so splitting code into more pieces or padding it cannot lower it, and the excerpt's size is reported beside it, not folded in. A function literal inside a function the excerpt shows is shown once, as part of it, and adds its lines over a limit to the composite: counts gives those to the innermost unit, and the rest of its counts are already in the function around it. None of the 49 excerpts judged on 2026-11-22 holds such a literal, so their composites are unchanged.

The seed splits the runs in half. 15 tune pairs come from one half, and 15 check and 10 spare pairs from the other, so the composite is never checked on the apps it was tuned on. A pair is one job in two runs. Each set is spread evenly over the difference between the two sides' composites (all weights 1), so some pairs are close and some far apart. No set shows one excerpt in more than two pairs, so one app cannot carry a set's verdicts; when no pair is left under that cap, the set stops short. Left and right are shuffled. When a set cannot be filled, the command fails and writes nothing. The pairs judged on 2026-11-22 were chosen while the cap could still give way, but no set of them shows an excerpt more than twice (a test checks the committed file), so they stand.

`-judged results/calibration-*.json` (quote the glob, or separate files with commas) leaves out every pair named in those files, in either order, so a second calibration sees only new pairs. Each file must record the seed it was chosen with, and it must be this `-seed`: the seed decides which runs tune and which check, and another would check on runs that were tuned on. A pair is named by its two runs and its job's text from `jobs.txt`, so rewording a job makes the pairs judged before unrecognisable: change `jobs.txt` only before a calibration, never between one and its revision. `-across baseline,other` instead pairs two arms job by job, run 1 against run 1 and so on, with no sampling, and reports per arm how many jobs were missing or too long.

The pairs go to `results/pairs-<date>.json` with the seed and each run's tally: jobs shown, missing and too long, how many of the named functions the agent wrote, and what finding the jobs cost when it was done. An existing file is never overwritten, since a calibration may name its pairs; pass `-out` for another label.

### Calibration

`bench calibrate` serves the pairs for Paul to judge blind, on this machine's Tailscale IP:

    go run ./cmd/bench calibrate -pairs results/pairs-2026-11-22.json \
        -listen-ip "$(tailscale ip -4)" -port 8790

It prints the page's address. The page shows two excerpts side by side (one above the other on a phone), the question, a line on when a pair is not the same job, a count of pairs, and four buttons: left reads better, right reads better, no difference, and not the same job. It never shows the runs, the job, the pair's number or any score; the form posts only how many pairs were judged before it, so a stale page or a second click changes nothing. Tune and check pairs come in a shuffled order; spares follow only while fewer than 12 check pairs have a side. Each verdict is written to `results/calibration-<date>.json` (the date in the pairs file's name, so a session resumed the next day keeps its file) as it is given, with the pair's `Canonical`, the pairs file and its seed, so `bench pairs -judged` can leave those pairs out later. Reloading the page, or serving again after Ctrl-C, picks up where it stopped. The command returns once the page has said every pair is judged. `-listen-ip` takes `127.0.0.1` or a Tailscale address, never an empty or all-interfaces one.

`bench agree` has Astra (`gpt-6-astra`, high effort) judge the same pairs with the same question, the same line on not the same job, and the same left and right, then compares:

    go run ./cmd/bench agree -pairs results/pairs-2026-11-22.json
    go run ./cmd/bench agree -pairs results/pairs-2026-11-22.json \
        -calibration results/calibration-2026-11-22.json

Astra sees only the two excerpts in its prompt, from an empty directory, and never Paul's verdicts. Its picks are saved one by one to `results/astra-picks-<date>.json`, with the model, effort and a hash of its instructions; later runs reuse them and ask only for missing pairs, and picks made by another model or other instructions are refused until the file is deleted. Without `-calibration` it picks every pair, which can be done before Paul judges. With it, it picks every pair Paul judged, tunes the composite's weights on the tune pairs (each 0, 1, 2 or 3, the most agreement with Paul; ties go to the weights nearest all 1s, then to those with fewer zeros, then to the larger sum), and adds an attempt to `results/agreement-<date>.md`. `-weights c,n,o,d,f` checks those weights instead of tuning, for a revision checked on new pairs.

In the report, only pairs Paul gave a side count, and spares count as check pairs. A layer (the tuned composite, or Astra) is trusted when it picks Paul's side on at least 80% of at least 12 such check pairs; "no difference" or "not the same job" against his side is a disagreement. Each layer also shows the lowest and highest agreement with each run's pairs left out in turn, since one run can sit on one side of several pairs. Each run of `bench agree` is an attempt in the report: a new pairs file, calibration or weights is added after the attempts already there, and the same one again replaces itself. Fixed weights are told apart by their values (`fixed weights 1,3,1,1,1`), so checking other weights on the same pairs adds an attempt and never writes over the one before; whether a revision was checked on fresh pairs is for the reader of the report to see. A revision passes the first report's label with `-out` so that both attempts are listed together. Text after a `## Checkpoint` heading is kept.
