# The amadan move — rastrillo's repository becomes `amadan.net/rastrillo/rastrillo`

**Date:** 2026-08-30 · **Status:** DRAFT for review ·
**Host:** `amadan.net` (`~/github.com/amadannet/amadan`) — the repo
already exists at `rastrillo/rastrillo`, **Public** tier, empty.
**Precedent:** `amadan.net/rastrillo/idear`, the addon that already
lives there end to end.

## 0. What this is, and the boundary

rastrillo moves off GitHub. `amadan.net/rastrillo/rastrillo` becomes
the repository, the import path, and the place work is reviewed;
`github.com/rastrilloorg/rastrillo` is archived read-only with a
pointer. This is a relocation, not a redesign: no package gains or
loses a feature, and the gate that has to stay green is the gate that
is green today.

One safety property orders everything below. **GitHub is not archived
until a fresh `go install amadan.net/rastrillo/rastrillo/cmd/rastrillo`
has been proven to work against a real published tag.** Until that
moment the old repo is a working fallback, and every step before it is
reversible by pushing nothing further.

Decided before this was written, and not reopened here: full move with
GitHub archived; branch and review on amadan, then squash onto main
locally; the prompt ledger at `full`; the six open issues re-filed as
amadan discussions; and both CI gates running side by side for a couple
of landings rather than handed over on the first green.

## 1. Identity — the module path

`github.com/carlosframework/rastrillo` becomes
`amadan.net/rastrillo/rastrillo`. As of `ac287b5` that string appears
**562 times across 250 tracked files**: the root module, the four
example modules under `examples/` and their `replace` directives, the
scaffold's generated `go.mod`, `README.md`, `SKILL.md`, and the docs
corpus under `docs/site/`.

The rewrite is mechanical — one substitution across tracked files —
but three things about it are not.

**The scaffold writes the path.** `cmd/rastrillo` emits a `go.mod` for
every new app, and its tests assert the exact `require` and `replace`
lines (`generate_test.go`, `vectors_e2e_test.go`, `new_test.go`). Those
assertions are the spec of what a scaffolded app looks like; they move
with the path, and their failure is the signal that a call site was
missed.

**`SKILL.md` gets smaller.** The new path is 30 bytes against the old
one's 35, so every occurrence returns 5 bytes. The file is 17,602 of
its 18,000 budget (`skillmd_test.go`), so this move eases the budget
rather than pressing on it. No line is trimmed to pay for it.

**The docsite gates are the proof.** `internal/docsite`'s eleven
checks — fences, nav, links, anchors, templates, CLI coverage, exported
symbols, per-package pages — already fail on a broken code fence or a
dead symbol reference. A rename that misses a doc page is caught there,
not by reading.

### 1.1 The tags cannot come with us

This is the one consequence with no workaround, and it must be
understood before the move rather than discovered after it.

A module's identity lives in the `go.mod` at each tag. All 24 existing
tags declare `module github.com/carlosframework/rastrillo`, so the
proxy will reject `amadan.net/rastrillo/rastrillo@v0.19.0` with
*"module declares its path as…"*. **No existing version is installable
under the new path.**

Therefore:

- The tags are still pushed to amadan. They are history — `git
  describe`, `git log v0.18.0..`, release archaeology — and they cost
  nothing. They are *not* installable, and `README.md` says so once
  rather than leaving a reader to discover it from a proxy error.
- **The new path starts at the first tag cut after the move lands, and
  this branch names no version anywhere.** That tag is written
  `<first-tag-after-the-move>` throughout this spec and the plan; the
  release-prep step at merge time chooses the concrete number as the
  next unused version above whatever `main` last tagged.
- `cmd/rastrillo/version.go`'s `rastrilloFallbackVersion` is therefore
  **left exactly as `main` has it** by the rename commit. That constant
  is what a locally built binary writes into a scaffolded app's
  `go.mod`, so **the tag must exist on amadan before `rastrillo new`
  produces an app that resolves** — which is why the constant moves in
  a release-prep commit at the moment the PR lands, in step 5 below,
  and not before.
- `README.md`'s install line becomes `go install
  amadan.net/rastrillo/rastrillo/cmd/rastrillo@latest`, and says which
  tags are installable without naming a floor version.

**Why no version is named here.** Earlier drafts of this spec fixed the
floor at `v0.20.0`, then at `v0.21.0`. Both were overtaken: while this
work was in flight `main` cut `v0.20.0` (#117), `v0.21.0` (#123) and
`v0.22.0` (#125), each declaring the *old* module path, so each in turn
became unavailable to the new one — not skipped, but taken. A hardcoded
floor also reaches further than it looks: it had been quoted in
`README.md` prose, so every correction sent an approved copy string
back through the human review gate for a number that was stale again
within a day. The race is structural, because `main` tags releases
faster than a branch of this size can land. A branch that names a
version re-enters that race on every re-derive; one that does not,
does not. So the number is chosen once, at merge time, by the release
prep — and nothing on the branch depends on knowing it in advance.

### 1.2 What `docs/site/addons.md` has to stop saying

That page currently explains idear's path with:

> The vanity path is `amadan.net`, not `github.com/carlosframework`,
> because an addon is a separate module in a separate repository on its
> own release schedule

The contrast it draws stops being true the moment core moves to the
same host. The paragraph is rewritten to say what still distinguishes
an addon — separate module, separate repository, its own release
schedule — without leaning on a host difference that no longer exists.

## 2. The git move

Push `main` and all 22 tags to `https://amadan.net/rastrillo/rastrillo`.

**The 59 remote branches are deliberately left behind.** They are spent
PR branches whose content is in main's squashed history; the archived
GitHub repo keeps them readable if one is ever wanted. Carrying them
would populate a fresh repo's Branches tab with 59 dead handles on day
one.

Remotes: `origin` becomes amadan. GitHub is kept as a second remote
named `github` until step 7, then dropped.

Then `amadan ledger init -level full -remote origin`, which installs
the post-commit hook, the Claude Code hooks, and the push refspecs.

The ledger is not decoration here. On a **Public** repo it is plaintext
and world-readable — every prompt recorded is published — and that is
the accepted trade for the factor-X provenance record the project
already claims. It is also load-bearing for §4: `amadan ledger landed`
writes the merge marker whose second parent retains the pre-squash
branch tip, which is what keeps those objects reachable after the
branch is deleted (`e2e_test.go`'s
`TestE2ELedgerPlaintextPromptsRedactAndLandedSquash` in the amadan repo
proves exactly this flow).

## 3. CI

The shape is idear's, which is amadan's rule: **CI steps delegate to
`make` targets and never keep their own copies of the commands**, so
what a runner executes and what you run before pushing are one
definition.

- `Makefile` — a target per current CI step, plus `ci` running them in
  order.
- `.amadan/ci` — `#!/bin/sh` … `exec make ci`. Must stay executable; a
  non-executable script resolves to **skipped**, which is not a pass.
- `.amadan/ci.d/NN-name` — one script per step, run in filename order,
  fail-fast, each reported separately on the commit.

Mapping today's `.github/workflows/ci.yml` — the `test` job's ten
named steps, plus the `browser` job's tagged run:

```
10-gofmt            20-root              30-chromedp-graph   40-race
50-helloworld       60-blog              70-tickets          80-notes
90-generate-check   95-scaffold-smoke    99-browser
```

`$GITHUB_WORKSPACE` becomes `$PWD` in the scaffold smoke's `go mod edit
-replace`. `GOFLAGS=-mod=mod` and `CGO_ENABLED=0` move from the job env
into the `Makefile`, where `40-race` overrides `CGO_ENABLED=1` for its
own command and no other — the static-binary criterion is about what
ships, not what a detector build links.

`99-browser` runs `go test -tags browser -p 1 ./harness/ ./webauthn/
./ui/ ./internal/designsystem/`. The GitHub job's *install pinned
chromium* step has no counterpart: the cloud runner already carries
that browser (§3.1), so the step disappears rather than being ported.
`RASTRILLO_BROWSER_OPTIONAL` stays unset on purpose — a skip is not a
pass, so a runner that loses its browser fails loudly.

### 3.1 What the cloud runner already has, and the two risks

The durable runner (`hack/cloud-runner-user-data.sh` in the amadan
repo) carries Go 1.26.5, Node 22, and **Playwright 1.62.1** — the exact
version this repo's browser job pins — with chromium linked at
`/usr/local/bin/chromium`, which `harness/chrome.go` finds by PATH with
no environment configuration. That is a better fit than the move had
any right to expect.

Two risks are real and are not assumed away.

**No gcc.** The box's user-data installs `git make curl ca-certificates
bubblewrap`. `40-race` needs a C toolchain. This is the step expected to
fail first. The resolution is to add a compiler to
`hack/cloud-runner-user-data.sh` and replace the box — the runner is
strictly immutable by design, so changing it means replacing it.
Dropping the race detector to make the move simpler is rejected: `jobs`
spawns a goroutine per job while `Get` reads the same map, and losing
`-race` there trades a real invariant for convenience.

**arm64.** The runner is arm64 where GitHub Actions is x86_64. Go does
not care; chromium-headless-shell driving this suite on arm is
unproven. If `99-browser` proves flaky on arm in a way it is not on
x86, it is recorded as a known gap with its captured evidence — the
practice that got issue #86 diagnosed and then fixed — and never
hidden behind `RASTRILLO_BROWSER_OPTIONAL`.

### 3.2 Issue #94 closes by construction

#94 records that `AGENTS.md`'s gate line (`go vet ./... && gofmt -l . &&
go test ./...`) does not reproduce CI, because CI runs with
`GOFLAGS=-mod=mod`. Making the gate `make ci` — one definition, shared
by the runner and the human — fixes it structurally. #94 is closed as
part of this move rather than migrated to amadan.

## 4. The workflow, rewritten

`AGENTS.md`'s "Never merge to main directly" section is replaced. The
new rule:

> Branch, push to amadan, then fill the branch description and its
> tasks (`amadan branch describe`, `amadan task add`). **Stop there.**
> Paul reviews the branch's Changes tab. Then squash onto main, run
> `amadan ledger landed <sha> -via squash`, push, and delete the branch
> locally and on the remote.

Squash is a git concern, not a platform concern; amadan's merge card is
not in this path and neither is `merge_require_green`, the per-repo
switch that guards it.

Two honest notes go into that text rather than being learned the hard
way:

- **amadan will render a squash-merged branch as Closed → deleted, not
  Merged.** Deleting the head writes the `refs/amadan/closed/<name>`
  tombstone, which is the correct outcome by a different name. Reading
  the ledger's merge marker on that path is a deferred amadan
  follow-up (`docs/STATUS.md` lines 87 and 179). Worth filing upstream;
  not a blocker here.
- **`gh`-based review stops applying.** The `/code-review` flow and
  anything reaching for `gh pr` has no counterpart; review is the
  Changes tab.

## 5. Issues

Six are open: **#94, #82, #81, #77, #30, #23**. #94 closes under §3.2.
The remaining five are re-filed with `amadan discuss new` typed
`Issue`, title and body preserved, each carrying a provenance line —
`was rastrilloorg/rastrillo#N`.

Closed issues are not migrated; the archived GitHub repo keeps them
readable, which is what archiving is for.

Committed spec prose that cites `#77`-style numbers is **left alone**.
Those documents are dated records of what was true when written, and
rewriting their cross-references would make them lie about their own
history. The provenance line on each new discussion is what closes the
loop.

## 6. Order of operations

The order exists to keep a working CI and a working install path at
every point.

1. **Rename.** Module path, scaffold assertions, docs corpus.
   `rastrilloFallbackVersion` is left at whatever `main` has. Green
   locally.
2. **Add the amadan gate.** `Makefile`, `.amadan/ci`, `.amadan/ci.d/`.
   **Keep `.github/workflows/ci.yml`**, updated for the new path — see
   §6.1.
3. **Land it on GitHub** as an ordinary PR under today's rules, then
   push `main` and the tags to amadan and run `ledger init`.
4. **Prove the gate on the cloud runner.** ← **go/no-go.** Nothing
   below happens until every step reports green, including `40-race`
   and `99-browser`.
5. **Choose and tag `<first-tag-after-the-move>`** on amadan — the
   next unused version above whatever `main` last tagged — setting
   `rastrilloFallbackVersion` to it in the same release-prep commit.
   Verify `go install
   amadan.net/rastrillo/rastrillo/cmd/rastrillo@<first-tag-after-the-move>`
   from an empty module cache, then `rastrillo new` an app in a clean
   directory and build it — with no `replace` directive.
6. **First amadan branch:** rewrite `AGENTS.md` per §4 and
   `README.md` per §1.1. The first real exercise of the new workflow.
   `.github/workflows/ci.yml` stays.
7. **Run both gates side by side** for at least two landings — see
   §6.2.
8. **Retire GitHub:** delete `.github/workflows/ci.yml`, migrate the
   five issues, archive the repo, drop the `github` remote.

### 6.1 Why the GitHub workflow survives step 2

Deleting `.github/workflows/ci.yml` in the same change that renames the
module would land the largest mechanical diff this repo has ever taken
with no CI watching it — the PR would have no workflow to run. Keeping
it, updated, means both gates run over the rename itself. The cost is
one file's path strings updated; it is the correct price.

### 6.2 The overlap, and what ends it

The two gates run **alongside each other for at least two landings**,
not as a handover on the first green. One green run proves a runner can
execute the steps; it does not distinguish a gate that agrees with
GitHub from one that is quietly weaker — a step that resolves
**skipped** reports as a pass, and arm64 gives `99-browser` a genuinely
different substrate to be flaky on.

Mechanically the overlap is one extra push: while it lasts, `main` goes
to both remotes, so GitHub's workflow keeps firing on the same commits
the amadan runner is claiming. Work is still branched, reviewed and
squashed on amadan per §4 — GitHub is a mirror during this window, not
a second place to review.

The overlap ends when two consecutive landings show both gates green
**and agreeing step for step** — the same failures on a deliberately
broken commit, not merely two green ticks. If they disagree, the
disagreement is the finding and the overlap continues until it is
explained. Step 8 is what closes the window.

## 7. Testing

The gate is the test; a relocation that keeps it green has kept
everything. Three checks exist only because of this move:

1. **Fresh clone.** `git clone https://amadan.net/rastrillo/rastrillo`
   into an empty directory and run `make ci`. Proves the pushed repo is
   complete and the gate needs nothing from the old checkout.
2. **Published-module install.** `go install
   amadan.net/rastrillo/rastrillo/cmd/rastrillo@<first-tag-after-the-move>`
   with
   `GOMODCACHE` pointed at an empty directory. This exercises the whole
   chain — vanity meta tag, git-over-HTTPS, `proxy.golang.org`, sumdb —
   and is the step 4/5 go/no-go.
3. **Scaffold against the published tag.** `rastrillo new` in a clean
   directory, then build with **no `replace` directive**. Today's
   `95-scaffold-smoke` always replaces back to the checkout, so the
   published module resolving for a scaffolded app has never actually
   been proven. From amadan it will be.

## 8. Out of scope, named

- **The E2EE tier.** rastrillo is Public and stays Public: `go get`
  needs stock git over HTTPS, and an E2EE repo is reachable only
  through the `amadan` CLI. Nothing here is a step toward encrypting
  it.
- **amadan's squash-merge detection.** Filed upstream, not built here.
- **A `gh`-equivalent review harness for amadan.** Review is the
  Changes tab; automating it is separate work.
- **Migrating closed issues, discussions, or PR history.** The archive
  holds them.
- **`carlosframework/skills` and `carlosframework/platform`**, which
  `README.md` links. Other repositories, other decisions. The Homebrew
  tap and releases repo were in this list until 2026-08-30; §9 is why
  they left it.
- **The rastrillo.org docs deploy**, which ships from the local
  checkout via carlos and is indifferent to where the repo is hosted.

## 9. Homebrew distribution (added 2026-08-30)

`README.md` advertises `brew install carlosframework/tap/rastrillo`, and
the move breaks it in a way that leaving it alone does not fix.

**What breaks.** `Formula/rastrillo.rb` fetches
`github.com/carlosframework/rastrillo/archive/refs/tags/v0.5.0.tar.gz`
and its `head` tracks that repo's `main`. Archiving keeps both URLs
resolving forever while guaranteeing they never gain another version —
the worst failure shape available, because nothing appears broken. The
formula is also already pinned at **v0.5.0** against a v0.19.0 repo, so
it has been silently stale since well before this move.

**Why amadan alone cannot host it.** amadan serves stock git over HTTPS
and nothing else: `/archive/refs/tags/<tag>.tar.gz`, `/archive/<tag>.tar.gz`
and `/tarball/<tag>` all 404. A tag clone *does* work
(`git clone --depth 1 --branch v0.1.1 https://amadan.net/rastrillo/idear`
resolves correctly), so Homebrew's git strategy — `using: :git, tag:,
revision:` — is a genuine option. It is not the one taken: it would
route every `brew install` at amadan.net rather than a CDN.

**The shape, which is the house pattern.** `carlosframework/releases`
already distributes carlos this way, and `carlos.rb` reads its prebuilt
per-platform binaries with sha256s — "pre-built since
`carlosframework/platform` is private." rastrillo joins that repo rather
than opening a new one, and the tap stays `carlosframework/tap`, so
**`brew install carlosframework/tap/rastrillo` keeps working unchanged**
and `README.md`'s line needs no edit at all.

Two details that are not free:

- **Tags are prefixed.** `carlosframework/releases` already carries
  `v0.13.0`–`v0.17.0` for carlos, which will reach `v0.20.0` in time.
  rastrillo publishes `rastrillo-<first-tag-after-the-move>`; carlos's
  existing unprefixed tags are not touched. Asset names disambiguate the files, the prefix
  disambiguates the release.
- **The formula stops building from source.** It gains prebuilt
  darwin/linux × arm64/amd64 binaries and drops `depends_on "go" =>
  :build`, matching `carlos.rb`. That is a real change in what the
  formula promises, and the release process grows a cross-compile step
  and four checksums.

Still out of scope: bottling, `brew audit` compliance beyond what the
existing formulae meet, and Windows artifacts (carlos ships them; nothing
asks rastrillo to).
