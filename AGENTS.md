# Working in this repository

## Never merge to main directly

Every change lands through a pull request and then a squash merge. That
holds for changes that would merge cleanly on their own, for one-line
fixes, and for documentation. Do not present merging locally as an option
and do not ask which route to take — open the PR.

Confirm first only where there is real doubt: the base branch is not
`main`, the work depends on another branch that has not landed yet, or
the change is not yours to ship.

## The gate

One definition, run before pushing and by CI:

```
go vet ./... && gofmt -l . && go test ./...
```

`gofmt -l` must print nothing. A scaffolded app's own gate is its
`Makefile`'s `ci` target, mirrored one step per file in `.amadan/ci.d/`;
add to both or step-reporting runners silently skip what you added.

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
