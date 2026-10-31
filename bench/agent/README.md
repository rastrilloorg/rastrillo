# Agent benchmark

This benchmark gives the same app brief to up to three agents, called arms:

- **carlos** builds on Rastrillo and deploys to Carloku.
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
- **`-arms`** picks which of `carlos`, `control` and `spec` run. The default is `carlos,control`. `control` and `spec` both need `-platform` and `-control-account`.
- **`-n`** is the number of runs per arm per brief. The default is 3.
- **`-j`** is the number of runs at once. The default is 2. Measurements never overlap each other, but they can overlap another run's build, which makes Lighthouse's performance score noisier. Use `-j 1` for a round you mean to publish.
- **`-reuse-control 1`** copies round 1's control runs instead of paying for new ones. That's allowed only when the model and the `claude` version match.

Runs go to `~/.cache/agent-bench/<round>/`: the app, the transcripts, and screenshots of each page. None of that is committed. If a round stops partway through, run the same command again. It picks up where the round stopped.

The round stops straight away if a run loads a plugin, uses an output style, or reports a different `claude` version from the round's. Each of those means the config is no longer clean. Other harness failures, such as an API error or a usage limit, are retried twice. Anything the agent gets wrong is a result, not a failure of the harness: no deploy, a broken sign-in, a page it can't name.

## What the numbers mean

- **cost ($)** and **turns** come from `claude` itself, from the result record at the end of each prompt.
- **context at first write** is how much context the agent had read when it first wrote a file.
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
