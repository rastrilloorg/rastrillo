# The amadan move — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move rastrillo from `github.com/rastrilloorg/rastrillo` to
`amadan.net/rastrillo/rastrillo` — repository, import path, CI and
review — without the gate ever going dark or the install path ever
breaking.

**Architecture:** Three code changes (rename the module path; write an
amadan CI gate that both a runner and a human execute from one
definition; rewrite the prose the move makes untrue) followed by six
operational steps that relocate the repo, prove the new gate against
the old one, publish a tag under the new path, and only then archive
GitHub.

**Tech Stack:** Go 1.25+, GNU make, `amadan` CLI, amadan's cloud CI
runner (arm64, Go 1.26.5, Node 22, Playwright 1.62.1).

**Spec:** `docs/superpowers/specs/2026-08-30-amadan-move-design.md`

## Global Constraints

- Old module path, verbatim: `github.com/carlosframework/rastrillo`
- New module path, verbatim: `amadan.net/rastrillo/rastrillo`
- Remote, verbatim: `https://amadan.net/rastrillo/rastrillo` (Public tier)
- First version under the new path: **`v0.20.0`**. No earlier tag is
  installable under the new path — their `go.mod` declares the old one.
- The gate, one definition: `make ci`. CI steps delegate to `make`
  targets and never keep their own copies of the commands.
- `.amadan/ci` and every `.amadan/ci.d/*` file must be **mode 755** and
  committed with the exec bit. A non-executable script resolves to
  `skipped`, and a skip is not a pass.
- `RASTRILLO_BROWSER_OPTIONAL` stays unset everywhere.
- `CGO_ENABLED=0` everywhere except the `race` target, which sets
  `CGO_ENABLED=1` for its own command and no other.
- `GOFLAGS=-mod=mod` — the scratch-module tests need it.
- Until Task 9, work still lands through a **GitHub PR** under today's
  `AGENTS.md` rules. Never merge to main directly.
- Commit bodies explain *why* — the failure prevented, the alternative
  rejected — not what the diff shows.

---

### Task 1: Rename the module path across the tree

**Files:**
- Modify: `go.mod` (line 1)
- Modify: every tracked file containing the old path — **562
  occurrences across 250 files** as of `ac287b5`
- Modify: `examples/{helloworld,blog,tickets,notes}/go.mod` (the
  `require` line and the `replace … => ../..` line in each)
- Modify: `cmd/rastrillo/version.go:10` — `rastrilloFallbackVersion`
- Modify: `.github/workflows/ci.yml:127` — the `go mod edit -replace`
  line in the scaffold smoke
- Test: the existing suite; no new test file

**Interfaces:**
- Consumes: nothing.
- Produces: the import path `amadan.net/rastrillo/rastrillo` and the
  constant `rastrilloFallbackVersion = "v0.20.0"`, which Tasks 3, 4 and
  6 depend on.

- [ ] **Step 1: Confirm the starting count, so the rewrite is checkable**

```bash
git grep -o 'github\.com/carlosframework/rastrillo' | wc -l   # expect 562
git grep -l 'github\.com/carlosframework/rastrillo' | wc -l   # expect 250
```

Record both numbers. If they differ from 562/250 the base has moved;
that is fine, but use the numbers you actually see in Step 3.

- [ ] **Step 2: Run the gate now, so a later failure is attributable**

```bash
GOFLAGS=-mod=mod CGO_ENABLED=0 go build ./... && \
GOFLAGS=-mod=mod CGO_ENABLED=0 go vet ./... && \
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... -count=1
```

Expected: PASS. If anything fails here it is pre-existing — stop and
report it rather than renaming on top of a red tree.

- [ ] **Step 3: Rewrite the path across tracked files**

`git grep -l` lists tracked files only, which is what we want: no
`.git/`, no build output, no untracked scratch.

```bash
git grep -l 'github\.com/carlosframework/rastrillo' \
  | xargs sed -i 's|github\.com/carlosframework/rastrillo|amadan.net/rastrillo/rastrillo|g'
git grep -c 'github\.com/carlosframework/rastrillo' ; echo "exit=$?"
```

Expected: no output and `exit=1` — `git grep` exits 1 when it matches
nothing. Any remaining match is a miss.

- [ ] **Step 4: Bump the scaffold's fallback version**

`cmd/rastrillo/version.go:10`. This is what a locally built binary
writes into a scaffolded app's `go.mod`, so it has to name the first
version that will exist under the new path.

```go
const rastrilloFallbackVersion = "v0.20.0"
```

- [ ] **Step 5: Tidy the module graph**

```bash
GOFLAGS=-mod=mod go mod tidy
for e in helloworld blog tickets notes; do (cd examples/$e && GOFLAGS=-mod=mod go mod tidy); done
```

The examples keep `require amadan.net/rastrillo/rastrillo v0.1.0`
alongside `replace amadan.net/rastrillo/rastrillo => ../..`. That
version never resolves and never needs to: a filesystem `replace` makes
the requirement local, which is why no `go.sum` entry is needed for it.
Do **not** "fix" it to `v0.20.0`.

- [ ] **Step 6: Format, then run the full gate**

```bash
gofmt -l .    # expect: no output
GOFLAGS=-mod=mod CGO_ENABLED=0 go build ./... && \
GOFLAGS=-mod=mod CGO_ENABLED=0 go vet ./... && \
GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... -count=1
```

Expected: PASS. The tests that matter most here are
`cmd/rastrillo`'s — `generate_test.go`, `vectors_e2e_test.go` and
`new_test.go` assert the exact `require`/`replace`/import lines a
scaffolded app gets, so they are the ones that catch a missed call
site. `internal/docsite`'s eleven checks catch a missed docs page.

- [ ] **Step 7: Run the example sweeps, which the root sweep skips**

```bash
for e in helloworld blog tickets notes; do
  (cd examples/$e && GOFLAGS=-mod=mod CGO_ENABLED=0 go build ./... \
    && GOFLAGS=-mod=mod CGO_ENABLED=0 go vet ./... \
    && GOFLAGS=-mod=mod CGO_ENABLED=0 go test ./... -count=1) || echo "FAILED: $e"
done
```

Expected: PASS for all four, no `FAILED:` lines.

- [ ] **Step 8: Verify the generator's committed output did not drift**

`.gitignore` learns about `.build/` here, before anything is built into
it — Step 9 runs `git add -A`, which would otherwise commit the binary.

```bash
grep -qx '.build/' .gitignore || echo '.build/' >> .gitignore
mkdir -p .build && GOFLAGS=-mod=mod CGO_ENABLED=0 go build -o .build/rastrillo ./cmd/rastrillo
for e in helloworld blog tickets notes; do .build/rastrillo generate --check examples/$e; done
for e in helloworld blog tickets notes; do .build/rastrillo generate examples/$e; done
git diff --exit-code
```

Expected: `generate --check` silent, and `git diff --exit-code` exits 0.
A non-empty diff means the generator writes the path differently from
how the committed `gen/` output spells it — find the template that Step
3 missed.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "Rename the module path to amadan.net/rastrillo/rastrillo

The repository moves to amadan.net, so the import path moves with it:
a path naming github.com/carlosframework would point at a repo that is
about to become an archive, and the scaffold writes that path into
every app it generates.

rastrilloFallbackVersion moves to v0.20.0 in this same commit and not
later. It is what a locally built binary pins a scaffolded go.mod to,
and no tag before v0.20.0 is installable under the new path - their
go.mod declares the old one, so the proxy rejects them outright.

The examples keep 'require ... v0.1.0' beside their filesystem
replace. That version resolves through the replace and never through
the proxy; raising it would imply a published version that will not
exist.

The GitHub workflow is updated rather than deleted, so this - the
largest mechanical diff this repo has taken - lands with a gate
watching it."
```

---

### Task 2: Rewrite the prose the rename makes untrue

**Files:**
- Modify: `docs/site/addons.md:42-44` (the vanity-path paragraph)
- Modify: `README.md` — the `go install` line, plus a new sentence about
  the pre-`v0.20.0` tags
- Modify: `internal/docsite/docsite.go:8` (the website repo reference,
  if it still names `carlosframework`)
- Test: `go test ./internal/docsite/ -count=1`

**Interfaces:**
- Consumes: Task 1's new import path.
- Produces: nothing later tasks read.

**Before writing any of this copy:** this is user-facing documentation
prose, not a mechanical substitution. Invoke the `copy-review` skill and
put the new strings through it before writing them into the files.

The steps below therefore give the *constraints* on the new sentences —
what each paragraph must still accomplish, and what it must stop
claiming — and deliberately do not pre-write them. The reviewed copy is
what ships, verbatim; an executor's own wording is not.

- [ ] **Step 1: Find every place the old host is load-bearing prose**

```bash
git grep -n 'carlosframework' -- '*.md' '*.go'
git grep -n 'vanity path' -- docs/
```

Task 1 already replaced the full module path. What survives is prose
that names the *host* — the `docs/site/addons.md` paragraph, and any
`carlosframework/<other-repo>` link, which stays as-is because those
are different repositories.

- [ ] **Step 2: Rewrite `docs/site/addons.md`'s vanity-path paragraph**

It currently reads:

> The vanity path is `amadan.net`, not `github.com/carlosframework`,
> because an addon is a separate module in a separate repository on its
> own release schedule — do not guess `github.com/carlosframework/idear`,
> which does not exist. Use the path above verbatim.

The contrast is dead: core is on the same host now. The replacement must
still tell an agent the one thing this paragraph exists to prevent —
guessing a sibling path for an addon. Keep "separate module, separate
repository, its own release schedule"; drop the host contrast.

- [ ] **Step 3: Update `README.md`'s install line and note the tag floor**

```
go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@latest
```

Add one sentence, near it, saying that versions before `v0.20.0` exist
as git tags for history but are not installable under this path,
because their `go.mod` declares the old one. One sentence — a reader
should learn this here rather than from a proxy error.

- [ ] **Step 4: Run the docsite gates**

```bash
GOFLAGS=-mod=mod go test ./internal/docsite/ -count=1
```

Expected: PASS. These eleven checks are what prove a docs edit did not
break a fence, a nav entry, an anchor or a symbol reference.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "Docs: say what is still true now that core lives on amadan

addons.md explained idear's path by contrasting amadan.net with
github.com/carlosframework. That contrast dies with this move - core is
on the same host - but the paragraph's real job survives: stop an agent
guessing a sibling path for an addon. It now says separate module,
separate repository, own release schedule, and leans on none of it.

The README's install line follows the module, and gains one sentence
about the pre-v0.20.0 tags: they are history, not versions the proxy
will serve under this path. Better read here than inferred from a
resolution error."
```

---

### Task 3: The amadan gate — one definition for runner and human

**Files:**
- Create: `Makefile`
- Create: `hack/scaffold-smoke.sh` (mode 755)
- Create: `.amadan/ci` (mode 755)
- Create: `.amadan/ci.d/10-gofmt` … `.amadan/ci.d/99-browser` (mode 755)
- Modify: `.gitignore` — add `.build/`
- Modify: `AGENTS.md` — "The gate" section (closes issue #94)
- Test: `make ci` itself

**Interfaces:**
- Consumes: Task 1's import path (the scaffold smoke's `-replace`).
- Produces: `make ci` and the eleven `make` targets `gofmt`, `root`,
  `chromedp-graph`, `race`, `example-helloworld`, `example-blog`,
  `example-tickets`, `example-notes`, `generate-check`,
  `scaffold-smoke`, `browser`. Task 5 runs these on the cloud runner;
  Task 8 compares them against GitHub's.

- [ ] **Step 1: Write the `Makefile`**

Every target is one current CI step, verbatim in behaviour. `ci` is the
whole gate.

```makefile
.PHONY: ci gofmt root chromedp-graph race generate-check scaffold-smoke browser \
        example-helloworld example-blog example-tickets example-notes $(BIN)/rastrillo

# The READMEs' documented sweeps all run with GOFLAGS=-mod=mod: the tests
# that build scratch modules (replace => this repo) rely on it to resolve
# module data the scratch go.sum does not carry. Without it, -mod=readonly
# fails every nested go build on a fresh machine.
export GOFLAGS = -mod=mod

# CGO_ENABLED=0 across the tree is an acceptance criterion, not a
# preference: static binaries, and gormlite must never quietly pull in a
# cgo driver because a C toolchain happened to be present. The race
# target is the single deliberate exception, scoped to its own command.
export CGO_ENABLED = 0

BIN := $(CURDIR)/.build
EXAMPLES := helloworld blog tickets notes

# ci is the one gate: what a runner executes and what you run before
# pushing are the same definition. .amadan/ci.d/ reports these one by
# one; it never keeps its own copy of a command.
ci: gofmt root chromedp-graph race \
    example-helloworld example-blog example-tickets example-notes \
    generate-check scaffold-smoke browser

gofmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; fi

root:
	go build ./...
	go vet ./...
	go test ./... -count=1

# The README promises chromedp stays out of the ordinary build graph.
# This is that sentence, executable.
chromedp-graph:
	@if go list -deps ./... | grep -i chromedp; then \
		echo "go list -deps ./... pulls chromedp - the README's promise is broken"; \
		exit 1; \
	fi

# -race needs cgo, so this one target overrides the file-wide
# CGO_ENABLED=0: the static-binary criterion is about what ships, not
# what a detector build links. Scoped to the two packages whose tests
# are actually concurrent - jobs (Start spawns a goroutine per job while
# Get reads the same map) and sessions.
race:
	CGO_ENABLED=1 go test -race -count=1 ./jobs/ ./sessions/

# The examples are separate modules with a replace back to this
# checkout, so the root sweep does not compile them - each needs its own
# ./... . blog, tickets and notes carry a `go tool sqlc` directive, so
# their first run fetches sqlc through the module proxy; that network
# access is load-bearing - do not cache it away without keeping the
# module download path working.
example-%:
	cd examples/$* && go build ./... && go vet ./... && go test ./... -count=1

# Phony on purpose though it names a real file: a stale binary from an
# earlier checkout would let both steps below pass against a generator
# that is no longer the one in the tree.
$(BIN)/rastrillo:
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/rastrillo

# generate --check is the ship gate; the second loop plus git diff
# proves the committed gen/ output still matches what today's generator
# writes.
generate-check: $(BIN)/rastrillo
	@for e in $(EXAMPLES); do $(BIN)/rastrillo generate --check examples/$$e || exit 1; done
	@for e in $(EXAMPLES); do $(BIN)/rastrillo generate examples/$$e || exit 1; done
	git diff --exit-code

# Depends on the binary, NOT on generate-check: sharing that prerequisite
# would make step 95 re-run step 90's work and fail for step 90's reasons,
# so a red run would not say which gate actually broke.
scaffold-smoke: $(BIN)/rastrillo
	./hack/scaffold-smoke.sh

# The browser drive: the ui select journey, the harness's own checks,
# the design system's, and webauthn's PRF ceremonies including the
# prfByAssertion fallback. -p 1 serialises the packages - parallel
# Chromium cold-starts contend for one machine. RASTRILLO_BROWSER_OPTIONAL
# stays unset on purpose: a skip is not a pass, so a machine that loses
# its browser fails loudly instead of reporting green.
browser:
	go test -tags browser -p 1 ./harness/ ./webauthn/ ./ui/ ./internal/designsystem/ -count=1
```

- [ ] **Step 2: Write `hack/scaffold-smoke.sh`**

The background server and its readiness poll are too awkward for a make
recipe, where each line is its own shell.

```sh
#!/bin/sh
# Scaffold an app with the built CLI, build it, run it, and prove it
# answers /healthz. The replace points at this checkout on purpose:
# this smoke proves the *generator*, not the published module. Task 6
# is what proves the published module, with no replace at all.
set -e

repo=$(cd "$(dirname "$0")/.." && pwd)
bin="$repo/.build/rastrillo"
appdir=$(mktemp -d)
trap 'rm -rf "$appdir"' EXIT

cd "$appdir"
"$bin" new smokeapp
cd smokeapp
go mod edit -replace amadan.net/rastrillo/rastrillo="$repo"
go mod tidy
go build ./...
go vet ./...
go build -o smokeapp ./cmd/smokeapp

./smokeapp -addr 127.0.0.1:8199 &
server=$!
trap 'kill "$server" 2>/dev/null; rm -rf "$appdir"' EXIT

for _ in $(seq 1 20); do
	if curl -sf http://127.0.0.1:8199/healthz >/dev/null; then break; fi
	sleep 0.5
done
curl -sf http://127.0.0.1:8199/healthz
```

```bash
chmod 755 hack/scaffold-smoke.sh
```

- [ ] **Step 3: Write `.amadan/ci` and the eleven step scripts**

```bash
mkdir -p .amadan/ci.d

cat > .amadan/ci <<'EOF'
#!/bin/sh
# amadan CI entry (single-script fallback for runners without step
# support). The steps in ci.d/ are the same targets, reported one by
# one. Must stay executable: a non-executable script resolves "skipped".
set -e
exec make ci
EOF

set -- 10-gofmt:gofmt 20-root:root 30-chromedp-graph:chromedp-graph \
       40-race:race 50-helloworld:example-helloworld 60-blog:example-blog \
       70-tickets:example-tickets 80-notes:example-notes \
       90-generate-check:generate-check 95-scaffold-smoke:scaffold-smoke \
       99-browser:browser
for pair in "$@"; do
  file=${pair%%:*}; target=${pair#*:}
  printf '#!/bin/sh\nexec make %s\n' "$target" > ".amadan/ci.d/$file"
done

chmod 755 .amadan/ci .amadan/ci.d/*
ls -l .amadan/ci .amadan/ci.d/
```

Expected: twelve files, every one `-rwxr-xr-x`.

- [ ] **Step 4: Ignore the build directory**

```bash
grep -qx '.build/' .gitignore || echo '.build/' >> .gitignore
```

`generate-check` ends in `git diff --exit-code`, which reads tracked
files only — but leaving `.build/` untracked-and-visible makes every
`git status` noisy.

- [ ] **Step 5: Rewrite `AGENTS.md`'s gate section — this closes #94**

The current text gives `go vet ./... && gofmt -l . && go test ./...`,
which is issue #94's complaint: it does not reproduce CI, because CI
runs with `GOFLAGS=-mod=mod`. Replace it so there is exactly one
definition:

> ## The gate
>
> One definition, run before pushing and by CI:
>
> ```
> make ci
> ```
>
> The `Makefile` carries `GOFLAGS=-mod=mod` and `CGO_ENABLED=0`, so
> running it by hand and running it on a runner are the same thing —
> which the old three-command gate line was not (issue #94: it omitted
> `GOFLAGS`, so it passed where CI failed). `.amadan/ci.d/` reports the
> same targets one step at a time and never keeps its own copy of a
> command; add to the `Makefile` and to `ci.d/` together, or a
> step-reporting runner silently skips what you added.

Keep the existing paragraph about `examples/` being separate modules —
it is still true and still load-bearing.

- [ ] **Step 6: Run the whole gate through the new entry point**

```bash
make ci
```

Expected: PASS, every target. This must be run on a machine with a
chromium — `browser` is in `ci` on purpose, and an absent browser is
meant to fail loudly rather than skip.

- [ ] **Step 7: Prove a step script actually fails the run**

A green suite cannot tell a working gate from one that reports
`skipped`. Break something on purpose:

```bash
printf '\n\n\nvar unusedDeliberateBreak int\n' >> run.go
sh .amadan/ci.d/20-root; echo "exit=$?"
git checkout run.go
```

Expected: a non-zero `exit=`. If it exits 0, the step is not running
what you think it is.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "The gate becomes make ci, and amadan's runner executes it

One definition, so the command a human runs before pushing and the
command a runner runs are the same string. AGENTS.md's old gate line
was three commands without GOFLAGS=-mod=mod, which is exactly issue
#94: it passed locally where CI failed. That divergence cannot recur
when both sides call the same target.

.amadan/ci.d/ delegates to make and never copies a command, which is
amadan's own rule for the same reason. Every script is committed mode
755 deliberately: a non-executable step resolves 'skipped', and a skip
renders as a pass - a gate that silently stops running is worse than
no gate.

The scaffold smoke moves to hack/ because a background server and a
readiness poll do not survive make's line-per-shell recipes.

Closes #94."
```

---

### Task 4: Relocate the repository

**Files:** none — this task changes remotes, not the tree.

**Interfaces:**
- Consumes: Tasks 1–3 merged to `main` on GitHub.
- Produces: `origin` pointing at amadan, `main` and 22 tags pushed, the
  prompt ledger initialised. Task 5 needs all of it.

- [ ] **Step 1: Land Tasks 1–3 on GitHub, under today's rules**

Open a PR, wait for the GitHub workflow to go green, squash-merge. This
is the last change to land through GitHub, and it lands the ordinary
way.

```bash
gh pr create --repo rastrilloorg/rastrillo --fill
gh pr checks --watch
```

Do **not** continue until that PR is merged and `origin/main` carries it.

- [ ] **Step 2: Confirm the destination is what the spec assumed**

```bash
curl -sS "https://amadan.net/rastrillo/rastrillo?go-get=1"
git ls-remote https://amadan.net/rastrillo/rastrillo; echo "refs-exit=$?"
```

Expected: the meta tag naming `amadan.net/rastrillo/rastrillo`, and
**no refs** with exit 0 — an existing, empty, Public repo. If refs come
back, something was pushed already; stop and report rather than
pushing over it.

- [ ] **Step 3: Point the remotes at their new roles**

```bash
git remote rename origin github
git remote add origin https://amadan.net/rastrillo/rastrillo
git remote -v
```

GitHub stays reachable as `github` for the overlap window (Task 8) and
is dropped in Task 9.

- [ ] **Step 4: Push main and the tags — and only those**

```bash
git fetch github --prune
git push origin github/main:refs/heads/main
git push origin --tags
```

The 59 remote branches are deliberately left behind: they are spent PR
branches whose content is in main's squashed history, and carrying them
would fill a fresh Branches tab with dead handles. The archived GitHub
repo keeps them readable.

- [ ] **Step 5: Verify what landed**

```bash
git ls-remote --heads origin          # expect exactly one: refs/heads/main
git ls-remote --tags origin | grep -c 'refs/tags/v'   # expect 22 (plus peeled ^{})
```

- [ ] **Step 6: Track the new origin**

```bash
git fetch origin
git branch --set-upstream-to=origin/main main
git status -sb | head -1
```

- [ ] **Step 7: Initialise the prompt ledger**

```bash
amadan ledger init -level full -remote origin
```

This installs the post-commit hook, the Claude Code hooks and the push
refspecs. It is not optional decoration: `amadan ledger landed` writes
the merge marker whose second parent retains a pre-squash branch tip,
which is what keeps those objects reachable after the branch is
deleted.

**Say this out loud to the user before running it, once:** the repo is
Public, so the ledger is plaintext and world-readable — every prompt it
records is published.

- [ ] **Step 8: Commit whatever `ledger init` wrote**

```bash
git status --short
git add -A && git commit -m "Opt into the prompt ledger on amadan

Level 'full', per the move spec. The provenance record is the point,
but the merge marker is the mechanism the workflow now depends on:
squashing onto main and deleting the branch would otherwise leave the
pre-squash tip unreferenced, and 'amadan ledger landed' is what retains
it as the marker's second parent.

The repo is Public, so this ledger is plaintext and world-readable.
That is the accepted trade, not an oversight."
git push origin main
```

---

### Task 5: Prove the gate on the cloud runner — go/no-go

**Files:**
- Possibly modify (in the **amadan** repo, not this one):
  `hack/cloud-runner-user-data.sh`

**Interfaces:**
- Consumes: Task 4's pushed `main` and Task 3's `.amadan/ci.d/`.
- Produces: a green CI status on a real commit. Nothing after this task
  may start until that exists.

**This is the go/no-go. Tasks 6–9 do not begin until every step reports
green.**

- [ ] **Step 1: Watch the push from Task 4 get claimed**

A push queues a job. Open the repo's commits view and the commit page
for `main`'s tip.

```bash
open https://amadan.net/rastrillo/rastrillo/commits   # or just visit it
```

Expected: a CI dot against the tip, resolving to per-step results.

- [ ] **Step 2: Read every step's result, not just the overall verdict**

Confirm all eleven steps ran. Specifically confirm **none reports
`skipped`** — a skip is the failure mode that looks like success, and
the usual cause is a step file that lost its exec bit in transit.

```bash
git ls-tree origin/main .amadan/ci .amadan/ci.d/   # expect mode 100755 on every entry
```

- [ ] **Step 3: Expect `40-race` to fail, and read why**

The runner's user-data installs `git make curl ca-certificates
bubblewrap` and no C toolchain; `race` needs one. The predicted failure
is a cgo/`gcc: not found` error from `CGO_ENABLED=1 go test -race`.

If it fails that way, fix the box — do not weaken the gate. In
`~/github.com/amadannet/amadan`, add a compiler to
`hack/cloud-runner-user-data.sh`'s `apt-get install` line, land it
there under that repo's own rules, and **replace the instance**: the
runner is strictly immutable by design, so there is no way to install
onto the running box.

Dropping the race detector instead is rejected: `jobs` spawns a
goroutine per job while `Get` reads the same map, and `-race` is what
makes that test able to report the race rather than only a bad outcome.

- [ ] **Step 4: Read `99-browser` carefully — arm64 is unproven here**

The box carries Playwright 1.62.1 (the version this repo pins) with
chromium linked at `/usr/local/bin/chromium`, which `harness/chrome.go`
finds by PATH with no configuration. What is unproven is
chromium-headless-shell driving **this** suite on **arm64**, now across
four packages (`./harness/ ./webauthn/ ./ui/ ./internal/designsystem/`)
rather than two.

If it is flaky on arm where it is green on x86, capture the evidence —
the failing drive, the widget state, the screenshots the harness
writes — and record it as a known gap with that evidence attached. Do
**not** set `RASTRILLO_BROWSER_OPTIONAL`: that converts a real failure
into a silent skip, which is the thing this whole task is checking for.

- [ ] **Step 5: Re-push and confirm green**

After any box replacement, push an empty commit to queue a fresh job
rather than waiting for other work:

```bash
git commit --allow-empty -m "Queue a CI job against the replaced runner box

The runner is immutable by design, so proving a change to it means
proving it against a real job on a real commit."
git push origin main
```

Expected, before proceeding: **all eleven steps green.**

---

### Task 6: Publish v0.20.0 and prove the install path

**Files:** none — this task publishes a tag and verifies it.

**Interfaces:**
- Consumes: Task 5's green gate, Task 1's `rastrilloFallbackVersion`.
- Produces: `v0.20.0` resolvable at `amadan.net/rastrillo/rastrillo`.
  Task 9 (archiving GitHub) depends on this having succeeded.

- [ ] **Step 1: Confirm the constant and the tag will agree**

```bash
grep -n 'rastrilloFallbackVersion' cmd/rastrillo/version.go
```

Expected: `v0.20.0`. If it says anything else, Task 1 Step 4 did not
land — fix that first, or a locally built `rastrillo new` emits an app
requiring a version that does not exist.

- [ ] **Step 2: Tag and push**

```bash
git tag -a v0.20.0 -m "v0.20.0 - the first release under amadan.net/rastrillo/rastrillo

No earlier tag is installable under this path: each declares
github.com/carlosframework/rastrillo in its own go.mod, which the proxy
rejects for this module. They remain as history."
git push origin v0.20.0
```

- [ ] **Step 3: Prove the module resolves through the proxy**

This exercises the whole chain — vanity meta tag, git over HTTPS,
`proxy.golang.org`, the checksum database.

```bash
curl -sS 'https://proxy.golang.org/amadan.net/rastrillo/rastrillo/@v/list'
```

Expected: `v0.20.0`. The proxy fetches on first request, so an empty
first response warrants one retry after a few seconds; a persistent
empty result is a real failure, not slowness.

- [ ] **Step 4: Install from an empty module cache**

```bash
cache=$(mktemp -d)
GOMODCACHE="$cache" GOFLAGS= go install amadan.net/rastrillo/rastrillo/cmd/rastrillo@v0.20.0
"$(go env GOPATH)/bin/rastrillo" version
```

Expected: a clean install and a version report of `v0.20.0` — proving
`rastrilloVersion()` reads the real tag from build info rather than
falling back to the constant.

- [ ] **Step 5: Scaffold with no replace — the test today's CI cannot do**

`95-scaffold-smoke` always replaces back to the checkout, so the
published module resolving for a scaffolded app has never actually been
proven. Prove it now:

```bash
tmp=$(mktemp -d) && cd "$tmp"
"$(go env GOPATH)/bin/rastrillo" new provingapp
cd provingapp
grep -n 'amadan.net/rastrillo/rastrillo' go.mod     # expect: require ... v0.20.0, no replace
go mod tidy
go build ./...
go test ./... -count=1
```

Expected: PASS with **no `replace` directive anywhere**. This is the
step that earns the right to archive GitHub.

---

### Task 6A: Homebrew keeps working

**Runs after Task 6** (it needs the `v0.20.0` tag) **and must complete
before Task 9's archive step** — the formula's `head` currently tracks
the repo Task 9 archives.

**Files:**
- Create: `hack/release-artifacts.sh` (mode 755)
- Modify (in **`carlosframework/homebrew-tap`**, a different repo):
  `Formula/rastrillo.rb`
- Publish (in **`carlosframework/releases`**, a different repo): a
  release tagged `rastrillo-v0.20.0` with four binaries

**Interfaces:**
- Consumes: Task 6's published `v0.20.0`.
- Produces: a working `brew install carlosframework/tap/rastrillo`.

- [ ] **Step 1: Write `hack/release-artifacts.sh`**

Cross-compiles the CLI for the four platforms `carlos.rb` covers, named
to match its convention.

```sh
#!/bin/sh
# Build the release binaries Homebrew installs. CGO_ENABLED=0 is the
# repo-wide criterion and matters doubly here: these are the artifacts
# other people run, on machines whose C libraries we know nothing about.
set -e

version=${1:?usage: release-artifacts.sh vX.Y.Z}
out=${2:-dist}
mkdir -p "$out"

for platform in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
	os=${platform%/*}
	arch=${platform#*/}
	echo "building $os/$arch"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOFLAGS=-mod=mod \
		go build -trimpath -o "$out/rastrillo-$os-$arch" ./cmd/rastrillo
done

# Homebrew needs these verbatim in the formula; print them rather than
# making someone re-derive them.
( cd "$out" && shasum -a 256 rastrillo-* )
```

```bash
chmod 755 hack/release-artifacts.sh
```

- [ ] **Step 2: Build the artifacts and capture the checksums**

```bash
./hack/release-artifacts.sh v0.20.0
ls -l dist/
```

Expected: four binaries and four sha256 lines. Record the checksums —
Step 4 needs them verbatim.

- [ ] **Step 3: Verify a built binary actually runs and reports v0.20.0**

Run the one matching this machine (linux/amd64 here):

```bash
./dist/rastrillo-linux-amd64 version
```

Expected: `v0.20.0`. A cross-compiled binary that reports the fallback
constant instead means the build lost its version stamping — stop and
report rather than publishing it.

- [ ] **Step 4: Publish the release — STOP AND CONFIRM FIRST**

This writes to a repository outside this worktree and is visible to
anyone. Confirm with the user before running it.

```bash
gh release create rastrillo-v0.20.0 \
  --repo carlosframework/releases \
  --title "rastrillo v0.20.0" \
  --notes "First release under amadan.net/rastrillo/rastrillo. Source: https://amadan.net/rastrillo/rastrillo" \
  dist/rastrillo-darwin-arm64 dist/rastrillo-darwin-amd64 \
  dist/rastrillo-linux-arm64 dist/rastrillo-linux-amd64
```

The `rastrillo-` tag prefix is deliberate: that repo already holds
`v0.13.0`–`v0.17.0` for carlos, which will reach `v0.20.0` in time.

- [ ] **Step 5: Rewrite the formula — STOP AND CONFIRM FIRST**

Also a different repository. In a checkout of
`carlosframework/homebrew-tap`, replace `Formula/rastrillo.rb`'s source
build with the prebuilt shape `carlos.rb` already uses: `version
"0.20.0"`, `homepage "https://amadan.net/rastrillo/rastrillo"`, an
`on_macos`/`on_linux` × `on_arm`/`on_intel` block per binary with the
Step 2 checksums, an `install` that puts the binary at `bin/"rastrillo"`
and chmods it 0755, and no `depends_on "go"`.

Delete the `head` line. It tracks the GitHub repo Task 9 archives, and
a `--HEAD` install that silently builds a frozen archive is worse than
no `--HEAD` at all.

Keep the `test do` block meaningful: assert the binary reports its
version, as `carlos.rb` does.

- [ ] **Step 6: Prove the formula installs**

```bash
brew uninstall rastrillo 2>/dev/null || true
brew install carlosframework/tap/rastrillo
rastrillo version
```

Expected: `v0.20.0`. If `brew` is unavailable on this machine, say so
plainly in the report rather than claiming an untested formula works.

- [ ] **Step 7: Commit the build script**

Only `hack/release-artifacts.sh` belongs to this repository; the other
two changes live in their own repos and are committed there.

```bash
git add hack/release-artifacts.sh
git commit -m "Build the release binaries Homebrew installs

The formula fetched a GitHub source tarball, and amadan serves none -
/archive/refs/tags/<tag>.tar.gz, /archive/<tag>.tar.gz and
/tarball/<tag> all 404, because it is stock git over HTTPS and nothing
more. Archiving the GitHub repo would have left that URL resolving
forever while never gaining another version, which is the failure shape
that looks like success.

So rastrillo joins carlosframework/releases the way carlos already
does: prebuilt per-platform binaries with checksums. The tap and the
brew command are unchanged, which is the point - README.md needed no
edit.

Tags there carry a rastrillo- prefix. That repo already holds
v0.13.0-v0.17.0 for carlos, and carlos will reach v0.20.0 eventually."
```

---

### Task 7: The workflow moves to amadan

**Files:**
- Modify: `AGENTS.md` — replace the "Never merge to main directly"
  section
- Test: the branch this task lands on is itself the test

**Interfaces:**
- Consumes: Task 6's proven install path.
- Produces: the documented workflow Task 8 and Task 9 follow.

This task is the first real exercise of the new workflow: create the
branch on amadan, describe it, stop for review, then squash it.

- [ ] **Step 1: Branch and push to amadan**

```bash
git checkout -b workflow-on-amadan
```

- [ ] **Step 2: Replace `AGENTS.md`'s merge section**

Delete "Never merge to main directly" and its confirm-first paragraph.
Write in its place:

> ## How work lands
>
> Branch, push to amadan, then fill the branch description and its
> tasks:
>
> ```
> amadan branch describe <name> -body -
> amadan task add -branch <name> -title "..."
> ```
>
> **Then stop.** Review happens on the branch's Changes tab, and it is
> not yours to skip. Once it is approved, squash onto `main`, record the
> landing, push, and delete the branch in both places:
>
> ```
> git checkout main && git merge --squash <name> && git commit
> amadan ledger landed $(git rev-parse HEAD) -branch <name> -via squash
> git push origin main
> git branch -D <name> && git push origin --delete <name>
> ```
>
> `amadan ledger landed` is not bookkeeping. Its merge marker keeps the
> pre-squash branch tip reachable as the marker's second parent; without
> it, deleting the branch orphans those objects.
>
> Two things will look wrong and are not. amadan renders a
> squash-merged branch as **Closed → deleted**, not "Merged" — reading
> the ledger's merge marker on that path is a deferred amadan
> follow-up, and the tombstone is the correct outcome under a different
> name. And `merge_require_green` is off: it guards amadan's merge
> button, which this workflow does not use.
>
> There is no `gh`. `/code-review` and anything reaching for `gh pr`
> has no counterpart here; review is the Changes tab.

- [ ] **Step 3: Run the gate and push the branch**

```bash
make ci
git add -A
git commit -m "AGENTS.md: how work lands on amadan

The PR-and-squash rule survives the move in substance - branch, get it
reviewed, squash - but every noun in it changed. There are no pull
requests on amadan; review is a branch's Changes tab, and the squash is
a plain git squash rather than a host feature.

'amadan ledger landed' is written into the rule rather than left to
memory: the merge marker's second parent is the only thing keeping the
pre-squash tip reachable once the branch is deleted.

The two surprising renderings are stated here so nobody diagnoses them
twice: a squashed branch shows Closed/deleted rather than Merged, and
merge_require_green guards a button this workflow never presses."
git push -u origin workflow-on-amadan
```

- [ ] **Step 4: Describe the branch, then stop**

```bash
amadan branch describe workflow-on-amadan -body - <<'EOF'
Rewrites AGENTS.md's merge section for amadan: branch, describe, review
on the Changes tab, squash onto main with `amadan ledger landed`.

Also the first exercise of that workflow — this branch is reviewed the
way it describes.
EOF
```

Stop here and hand it to the user for review. Do not squash it
yourself until they have approved it on the Changes tab.

- [ ] **Step 5: After approval, land it the new way**

```bash
git checkout main && git merge --squash workflow-on-amadan && git commit
amadan ledger landed "$(git rev-parse HEAD)" -branch workflow-on-amadan -via squash
git push origin main
git branch -D workflow-on-amadan && git push origin --delete workflow-on-amadan
```

Then confirm the branch renders under **Closed → deleted** on amadan,
and that a fresh clone can still read the old tip:

```bash
git ls-remote origin | grep workflow-on-amadan   # expect nothing on refs/heads
```

---

### Task 8: Run both gates side by side

**Files:** none — this task is a measurement, not a change.

**Interfaces:**
- Consumes: Task 7's workflow.
- Produces: the evidence that closes the overlap window and unlocks
  Task 9.

The overlap ends when **two consecutive landings** show both gates green
*and agreeing step for step* — not merely two green ticks. One green run
proves a runner can execute steps; it does not distinguish a gate that
agrees with GitHub from one that is quietly weaker.

- [ ] **Step 1: Mirror every landing to GitHub while the window is open**

After each `git push origin main` in the Task 7 workflow, also:

```bash
git push github main
```

GitHub is a mirror during this window, not a second place to review.
All branching, describing and reviewing stays on amadan.

- [ ] **Step 2: For each of two landings, record both verdicts**

For the amadan side, read the per-step results on the commit page. For
GitHub:

```bash
gh run list --repo rastrilloorg/rastrillo --limit 3
gh run view --repo rastrilloorg/rastrillo <run-id>
```

Write down, per landing: which steps ran, which passed, which were
skipped.

- [ ] **Step 3: Prove they fail the same way, not just pass the same way**

Two green runs cannot distinguish a real gate from a gate that is not
running. Once, on a throwaway branch, break something both gates should
catch:

```bash
git checkout -b gate-agreement-probe
printf '\n\n\nvar unusedDeliberateBreak int\n' >> run.go
git commit -am "Deliberately broken: probing that both gates refuse it"
git push origin gate-agreement-probe
git push github gate-agreement-probe
```

Expected: **both** gates fail, and both fail at the equivalent step
(`20-root` on amadan; *root module* on GitHub). Then delete the branch
in both places:

```bash
git checkout main
git branch -D gate-agreement-probe
git push origin --delete gate-agreement-probe
git push github --delete gate-agreement-probe
```

- [ ] **Step 4: If they disagree, the disagreement is the finding**

Do not close the window on a disagreement you cannot explain. A step
green on amadan and red on GitHub (or the reverse) means the two gates
are not the same gate — most likely a `skipped` step, an arm64
difference in `99-browser`, or a toolchain difference. Explain it,
record it, and keep the window open.

---

### Task 9: Retire GitHub

**Files:**
- Delete: `.github/workflows/ci.yml`
- Modify: `README.md` — a line saying where the repo lives now

**Interfaces:**
- Consumes: Task 6's proven install path, Task 8's two agreeing
  landings.
- Produces: the finished move.

**Do not start this task unless Task 6 Step 5 passed, Task 6A
republished the Homebrew formula, and Task 8 produced two agreeing
landings.** Archiving before Task 6A leaves the formula's `head`
tracking an archive. Archiving is the one step with no
cheap undo.

- [ ] **Step 1: Branch on amadan and delete the workflow**

```bash
git checkout -b retire-github
git rm .github/workflows/ci.yml
rmdir .github/workflows .github 2>/dev/null || true
```

- [ ] **Step 2: Point the README at the new home**

Add a line near the top saying the repository is
`https://amadan.net/rastrillo/rastrillo` and that the GitHub repo is an
archive. Keep it to a sentence.

- [ ] **Step 3: Gate, push, describe, and hand over for review**

```bash
make ci
git add -A
git commit -m "Retire the GitHub workflow

Two landings have now shown both gates green and refusing the same
deliberately broken commit at the same step, which is the evidence the
move spec asked for before this file could go. A single green run would
not have been: a step that resolves 'skipped' reports as a pass, so
agreement on a failure is the part that proves the gate is real.

The repository is amadan.net/rastrillo/rastrillo. GitHub is an archive
and the README now says so."
git push -u origin retire-github
amadan branch describe retire-github -body 'Deletes the GitHub workflow and points the README at amadan. Land only after the overlap evidence is recorded.'
```

Stop for review. Land it per Task 7 Step 5.

- [ ] **Step 4: Migrate the five open issues**

#94 closed with Task 3 and is not migrated. For each of **#82, #81,
#77, #30, #23**, read it from GitHub and re-file it, preserving title
and body and adding a provenance line:

```bash
for n in 82 81 77 30 23; do
  title=$(gh issue view "$n" --repo rastrilloorg/rastrillo --json title --jq .title)
  body=$(gh issue view "$n" --repo rastrilloorg/rastrillo --json body --jq .body)
  printf '%s\n\n---\nwas rastrilloorg/rastrillo#%s\n' "$body" "$n" \
    | amadan discuss new rastrillo/rastrillo -title "$title" -body -
done
```

Read each new discussion's id from the output as it is created.
`discuss new` takes no `-type`, so label each one afterwards:

```bash
amadan discuss type rastrillo/rastrillo <id> -type Issue
```

Confirm all five, and that `Issue` is in the repo's configured type set:

```bash
amadan discuss types rastrillo/rastrillo
amadan discuss list rastrillo/rastrillo -state open -json
```

Closed issues are **not** migrated — the archive keeps them, which is
what archiving is for. Committed spec prose citing `#77`-style numbers
is left alone: those documents are dated records, and rewriting their
cross-references would make them lie about their own history.

- [ ] **Step 5: Close the migrated issues on GitHub with a pointer**

```bash
for n in 82 81 77 30 23; do
  gh issue close "$n" --repo rastrilloorg/rastrillo \
    --comment "Moved to https://amadan.net/rastrillo/rastrillo — this repo is now an archive."
done
```

- [ ] **Step 6: Archive the GitHub repository**

**Confirm with the user before running this.** It is outward-facing and
awkward to reverse.

```bash
gh repo archive rastrilloorg/rastrillo --yes
```

- [ ] **Step 7: Drop the mirror remote**

```bash
git remote remove github
git remote -v      # expect only origin -> https://amadan.net/rastrillo/rastrillo
```

- [ ] **Step 8: Final verification, from nothing**

```bash
tmp=$(mktemp -d) && cd "$tmp"
git clone https://amadan.net/rastrillo/rastrillo
cd rastrillo && make ci
```

Expected: a clean clone from amadan alone, and a green gate that needed
nothing from the old checkout. The move is done when this passes.
