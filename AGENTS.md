# Working in this repository

## This project lives on amadan

`https://amadan.net/rastrillo/rastrillo` is where the work is: branches,
discussions, tasks, review and the prompt ledger. The GitHub remote is a
**mirror**. Nothing lands there, nothing is reviewed there, nothing is
opened there — no PRs, no issues, no `gh` anything.

The house rules for working here are the `amadan` skill, installed at
`.claude/skills/amadan/SKILL.md` and refreshed with `amadan agent skill
-install -force`. Read it; it is the procedure. In short: post as your
agent account, take a worktree and a branch before the first edit,
`amadan branch describe` it (the description is the PR body — there is
no PR object), keep the plan in `amadan task`, and put anything you
would otherwise sit on into `amadan discuss`.

## Never merge to main directly, and never squash

Every change lands on its own branch through `amadan branch merge`. That
holds for changes that would merge cleanly on their own, for one-line
fixes, and for documentation. Do not present merging locally as an option
and do not ask which route to take — push the branch and describe it.

**The squash is what broke this.** amadan decides a branch is merged by
asking git whether its tip is an ancestor of the default tip. A squash
rewrites the commits, so the answer comes back no and the branch stays
open while its content is already in. That is not a display bug: #142 was
squash-merged on the mirror, and three pieces of SKILL.md sat there for
days looking landed while `main` here had never seen them. `amadan branch
merge` makes an ordinary merge commit and will not squash and will not
fast-forward, for exactly this reason. Do not work around it.

Confirm first only where there is real doubt: the base branch is not
`main`, the work depends on another branch that has not landed yet, or
the change is not yours to ship.

## The mirror

    make mirror-check   # before you branch
    make mirror         # after every merge

`mirror` fast-forwards the GitHub remote's `main` from `origin/main` and
carries the ledger refs with it. `mirror-check` fetches both and fails
loudly, listing what the mirror has that origin does not.

Neither runs in `make ci`: a runner must not push, and a drift alarm
inside the gate would turn every branch red for something no branch did.
That makes them yours to run — the check before you branch, so you find
out before you build on a stale base, and the push after you land, so the
window where the two disagree is seconds instead of days.

If `make mirror` is rejected as a non-fast-forward, someone worked on the
mirror. Do not force it. Read what is there, carry it across as a branch
off `origin/main`, land it here, then mirror.

## The gate

One definition, run before pushing and by CI:

```
make ci
```

The `Makefile` carries `GOFLAGS=-mod=mod` and `CGO_ENABLED=0`, so
running it by hand and running it on a runner are the same thing —
which the old three-command gate line was not (issue #94: it omitted
`GOFLAGS`, so it passed where CI failed). `.amadan/ci.d/` reports the
same targets one step at a time and never keeps its own copy of a
command; add to the `Makefile` and to `ci.d/` together, or a
step-reporting runner silently skips what you added.

The examples under `examples/` are **separate Go modules** with a
`replace` back to the checkout, so the root `go test ./...` does not
compile them. Test them from their own directories.

## Commits

Imperative subject. The body explains *why* — the failure being
prevented, the alternative rejected — not what the diff already shows.

## Comments

The same rule as commits, and it is enforced in review: comments say why
a thing is the way it is, naming the failure it prevents. A comment that
restates the code is noise; a subtlety with no comment is a bug waiting
for the next reader.

## SKILL.md

Byte-budgeted and reviewed like code — it is what an LLM loads instead of
reading the source, so an inaccurate line there is worse than a missing
one. When it must grow, trim genuinely redundant prose. Never delete a
load-bearing fact to fit a number; raise the ceiling and say why in the
test that enforces it.

## User-facing copy

Short sentences, plain words, and tell the reader what to do. A note
beside a component reads "Add `Lead` to one stat in the row to make it
big", not "One band, one lead cell. There is no second component for a
headline stat." Both are true; only one is usable. Every note that had
to be rewritten failed the same way — it described the design decision
instead of the action.

**The reasoning is not the copy.** Why a component works the way it does
belongs in the code comment and the spec, where the next maintainer
needs it. The reader of the page needs the instruction. Mixing them
produces a page explaining itself to its own author.

**Brevity is the goal; flippancy is how it fails.** Cut words, not
seriousness. No winks, no flourishes, no sentence whose job is to sound
good.

Every user-facing string goes through the copy review before it ships,
and in `internal/designsystem` the English is also the translation key —
a word changed afterwards costs eleven redrafted translations. Review
the English first, then draft the eleven.
