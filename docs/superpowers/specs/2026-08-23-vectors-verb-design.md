# `rastrillo vectors` — Go↔JS parity vectors as a framework verb

**Issue:** rastrilloorg/rastrillo#78 · **Date:** 2026-08-23 · **Status:** draft for review

## 0. The gap

Any derivation over sealed content runs client-side, but the sidecar, operator
tools and tests want the same derivation in Go — so the engine exists twice,
and two engines drifting is the most dangerous E2EE bug class: a wrong answer
with nothing looking broken. Kass solved it by hand (`cmd/genvectors` →
`web/test/vectors.json` → a `node:test` suite that must reproduce every
vector). Rastrillo lives by the same discipline internally but gives apps no
machinery for it. This promotes the discipline to a verb — and fixes what
adversarial review found bit-rotted in the prototype on the way through.

## 1. Shape

Three pieces: a small Go library, a vendored JS helper module, and a CLI verb
with an init mode and a check mode.

### 1.1 `vectors/` — the library

```go
type Set struct{ /* ordered cases */ }
func New() *Set
func (s *Set) Add(name, why string, fields map[string]any)
func (s *Set) WriteTo(w io.Writer) error
```

`Add` takes a `why` because kass proved its worth: every vector names the rule
it pins, and the JS test titles become `name — why`. `WriteTo` emits a JSON
array, 2-space indent. Normalisation is scoped honestly: **top-level `fields`
values only** — a nil slice or nil map sitting directly in `fields` marshals
as `[]`/`{}`, never `null` ("no bests has to look the same on both sides").
Inside app-typed values the library never walks (json tags, omitempty,
custom marshalers make a generic deep walk a lie); kass's own generator
normalises exactly its two top-level output fields by hand, and that stays
the app's discipline. The `-init` template says so where the app will read it.

Two treaty rules the file format carries implicitly, said out loud in docs:
the **key names** in `fields` are part of the contract (the JS suite consumes
them by name; changing one means changing both sides in the same commit, and
nothing mechanical checks the key sets agree), and `time.Time` values
round-trip via RFC 3339 → `new Date(v.now)` — put times in as `time.Time`,
not pre-formatted strings.

There is deliberately **no hash-pin export**. Eventlog hash-pins its merge
vectors because they are a frozen external spec; app vectors change by
design, and the check mode's byte-compare (§1.3) already catches
regenerate-drift strictly better than a hash an author would just update.
A vectors file that *is* a frozen external spec (crypto's invites) keeps
using eventlog's plain-test pattern in the app's own suite.

### 1.2 `vectors/js/vectors.mjs` — the vendored helper

Embedded via `vectors/js.go` (`func JS() []byte`), delivered to apps once,
app-owned after (the tokens.css/shim contract). Named `vectors.mjs` — not
`*.test.mjs` — so bare `node --test` discovery never executes the helper as
a test. Exports:

- `loadVectors(path)` — read + parse the JSON file.
- `canonical(x)` — **kass's real normaliser, blind spot included**: sort
  object keys recursively; drop `undefined`/`null` members; drop scalar
  zero values (`0`, `false`, `""`) to match Go `omitempty`. Without the
  zero-strip, Go dropping `reps: 0` while JS computes it fails the diff on
  encoder behaviour rather than arithmetic — but the same rule means a
  meaningful explicit zero on one side and a missing field on the other
  compare equal. Kass covers that hole with hand-written explicit-value
  assertions beside the loop, and the scaffolded suite does the same by
  construction (§1.3). Empty arrays are kept as `[]` on both sides — the
  generator's top-level normalisation (§1.1) supplies them from Go.

### 1.3 The verb

`rastrillo vectors [dir]` — one more `case` in `cmd/rastrillo/main.go`'s
dispatch; the optional dir argument resolves and absolutises like
`generate`'s, and all `go run` happens with the command's working directory
set to the app's module root (the manifest goEval precedent).

- **`rastrillo vectors`** — runs `go run ./cmd/genvectors`, writes stdout to
  `test/vectors.json`. The generator is the app's own `package main` (kass's
  shape verbatim): it imports the app's pure fold, enumerates cases, prints a
  `vectors.Set`. Convention over configuration: the verb errors with guidance
  if `cmd/genvectors` doesn't exist. The root `test/` directory is a new
  scaffold convention (today's scaffold has none; kass uses `web/test/`) —
  chosen because the JS suite is neither a Go package nor a static asset.
- **`rastrillo vectors -check`** — the pre-ship gate, loud:
  1. regenerate to a temp file, byte-compare against the committed
     `test/vectors.json` — a diff means the Go engine changed without
     regenerating in the same commit;
  2. run `node --test test/parity.test.mjs` — **explicit file, never a
     directory**: directory arguments stopped working on Node ≥ 21 (kass's
     own `node --test web/test/unit/` Makefile line fails on current node —
     the prototype's invocation is bit-rotted, and a kass fix should follow).
     In check mode a missing `node` is a **failure**, not a skip — silent
     while iterating, loud before ship.
- **`rastrillo vectors -init`** — scaffolds the app-side files into an
  existing app: `cmd/genvectors/main.go` (a worked template anchored on
  `eventlog.Derive` — a tiny example fold, two cases, comments marking the
  treaty: "these key names are shared with parity.test.mjs; change both in
  one commit"), `test/parity.test.mjs` (loads `vectors.json`, calls the
  app's fold, asserts `deepEqual(canonical(got), canonical(want))` per
  vector, **plus a marked belt section of explicit-value assertions** —
  seeded with working examples against the template fold — and kass's
  sanity floor on vector count so silent generation failure can't pass),
  `test/vectors.mjs` (the vendored helper copy), and the Go belt + pin
  from §1.5. Not part of `rastrillo new` — most apps have no client-side
  fold; adoption is one command when they grow one.

### 1.4 Wiring into the existing gate

`rastrillo generate -check` already runs the pre-ship checks (missing locale
keys, untagged actions, dry-run manifests, unconfirmed write tools, unknown
icon slugs) against the resolved app root it takes as its dir argument. When
`cmd/genvectors` exists under that root, `-check` additionally runs the
vectors check from §1.3 — one gate before ship, not two to remember.

### 1.5 The Go-side belt

`-init` writes `internal/<pkg>test/parity_test.go`: the crypto `js_test.go`
exec-shim pattern — `exec.LookPath("node")`, `t.Skip` if absent, else run
`node --test` on the parity file and fail on nonzero exit — with the working
directory set correctly: `go test` runs the test in `internal/<pkg>test/`,
so the shim sets `cmd.Dir` to the module root (two levels up) and names
`test/parity.test.mjs` from there. So a plain `go test ./...` gates both
engines on any machine with node, and degrades gracefully elsewhere.

The vendored `test/vectors.mjs` copy gets its own pin: the scaffold's
`vendored_test.go` map is bare filenames rooted at `internal/<pkg>/static/`,
so `-init` appends nothing there — it writes a separate
`internal/<pkg>test/vectors_vendored_test.go` pinning `../../test/vectors.mjs`
byte-identical to `vectors.JS()`, same delete-the-line-if-deliberate
contract.

## 2. What this is not

Not a general fixture framework: one JSON file, one shape (array of named
cases), one comparison rule with a documented blind spot and a mandated
belt. Not a codegen path: the app's fold stays hand-written in both
languages; the vectors are the treaty between them. Not mandatory: no
`cmd/genvectors`, no gate.

## 3. Testing

- Library: `Set` ordering, top-level nil-normalisation, map-key determinism.
- Helper: a golden content test pinning `vectors.mjs` (the shim-vocabulary
  pattern), including the zero-strip cases.
- Verb: an `internal/` test scaffolds a fixture app (the `new_test.go` /
  `generate_test.go` pattern, including the
  `replace github.com/carlosframework/rastrillo => <checkout>` dance and
  `GOFLAGS=-mod=mod`), runs `-init`, then `vectors`, then `-check` green;
  mutates the Go fold and asserts `-check` fails on the byte-compare;
  mutates the JS fold and asserts `-check` fails from node. Node-dependent
  legs skip without node on PATH (rastrillo's own CI has node for the
  crypto twin already).
