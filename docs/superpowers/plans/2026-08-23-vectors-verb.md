# Vectors Verb Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote kass's golden-vector discipline to framework machinery: a `vectors/` library that writes the Go↔JS treaty file, a vendored `vectors.mjs` helper carrying kass's real `canonical()` (blind spot included), and a `rastrillo vectors` verb with `-init` and `-check` modes, folded into `rastrillo generate -check` when the app opts in.

**Architecture:** A small Go package (`vectors/`) builds an ordered set of named cases and emits `test/vectors.json` (2-space indent, top-level-only nil normalisation); a vendored ES module (`vectors/js/vectors.mjs`, embedded via `vectors.JS()`) supplies `loadVectors` and `canonical` to the app's scaffolded `test/parity.test.mjs`. The CLI verb (`cmd/rastrillo/vectors.go` + `vectorsinit.go`) runs the app's own `cmd/genvectors` with `cmd.Dir` set to the module root (the manifest goEval precedent), byte-compares on `-check`, runs `node --test test/parity.test.mjs` (explicit file, never a directory), and scaffolds the app-side kit on `-init` — including the Go-side belt and the vendored-helper pin in `internal/<pkg>test/`.

**Tech Stack:** Go stdlib only (`encoding/json`, `reflect`, `os/exec`, `embed`) — no new module dependencies. Node ≥ 21 with `node:test` for the JS legs. Existing in-repo patterns: `crypto/js.go` embed, `crypto/js_test.go` exec-shim, `cmd/rastrillo/generate.go` FlagSet + dir absolutising, `cmd/rastrillo/new.go` template consts, `cmd/rastrillo/generate_test.go` `scaffold`/`repoRoot` helpers.

**Spec:** docs/superpowers/specs/2026-08-23-vectors-verb-design.md

## Global Constraints

- Every go command runs with `export GOFLAGS=-mod=mod CGO_ENABLED=0`.
- gofmt-clean, `go vet ./...` clean, `go test ./...` green after every task.
- Do not touch SKILL.md (hard byte budget 14,986/15,000 — TestSkillMDStaysWithinBudget).
- node-dependent test legs must skip gracefully without node on PATH (except where the spec says check-mode requires node).
- Doc comments in the house voice (read cmd/rastrillo/generate.go and crypto/js_test.go for the register).

Additional house rules for this branch:

- Module path is `amadan.net/rastrillo/rastrillo`. New package lives at `vectors/`; new CLI files at `cmd/rastrillo/vectors.go`, `cmd/rastrillo/vectorsinit.go` (main.go's convention: one concern per file).
- Never merge to main directly — this branch becomes a PR, then squash-merge (MEMORY.md, PR-only workflow).
- Facts already established by adversarial review — bake in, do not re-derive: `node --test <dir>` fails on Node ≥ 21 (explicit file always); `canonical()` drops `undefined`/`null` members AND scalar zeros (`0`, `false`, `""`) recursively, sorts keys, keeps empty arrays; the `-init` parity template carries a marked belt section of explicit-value assertions plus a vector-count floor; `Set.WriteTo` normalises nil slices/maps at TOP LEVEL of `fields` only (inner shapes are the app's discipline, said where the app will read it); no Pin/hash-pin export in this package; the treaty includes field key names and RFC 3339 time round-trip, documented in the templates.

### One deliberate deviation from the spec

**§1.1 `WriteTo` signature.** The spec writes `func (s *Set) WriteTo(w io.Writer) error`. `go vet`'s stdmethods analyzer rejects that exact shape (verified: `method WriteTo(w io.Writer) error should have signature WriteTo(io.Writer) (int64, error)`), and the global constraint requires a clean vet. The method keeps its spec'd name and behaviour but adopts the canonical `io.WriterTo` signature: `func (s *Set) WriteTo(w io.Writer) (int64, error)`. Callers that only care about the error write `if _, err := set.WriteTo(os.Stdout); err != nil { … }` — the scaffolded generator template does exactly that.

---

### Task 1: `vectors/` — the Set library

**Files**
- Create: `vectors/vectors.go`
- Test: `vectors/vectors_test.go`

**Interfaces**
- Consumes: `io.Writer`, `encoding/json`, `reflect`.
- Produces: `type Set struct{ /* ordered cases */ }`, `func New() *Set`, `func (s *Set) Add(name, why string, fields map[string]any)`, `func (s *Set) WriteTo(w io.Writer) (int64, error)` (see the deviation note above).

**Steps**

- [ ] Write the failing test file `vectors/vectors_test.go`:

```go
package vectors

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// The file must read in the order the rules were written, and
// regenerate byte-identically — Add order is the order, always.
func TestWriteToKeepsAddOrder(t *testing.T) {
	s := New()
	s.Add("zebra", "added first, stays first", map[string]any{"n": 1})
	s.Add("aardvark", "added second, stays second", map[string]any{"n": 2})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Index(out, `"zebra"`) > strings.Index(out, `"aardvark"`) {
		t.Errorf("cases were reordered:\n%s", out)
	}
}

// The exact wire shape: a JSON array, 2-space indent, trailing
// newline, name and why leading each object, then the fields sorted.
func TestWriteToShape(t *testing.T) {
	s := New()
	s.Add("one", "shape", map[string]any{"n": 1})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\n    \"name\": \"one\",\n    \"why\": \"shape\",\n    \"n\": 1\n  }\n]\n"
	if got := buf.String(); got != want {
		t.Errorf("WriteTo = %q, want %q", got, want)
	}
}

func TestWriteToEmptySet(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New().WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "[]\n" {
		t.Errorf("empty Set = %q, want %q", got, "[]\n")
	}
}

// The library's whole normalisation: nil slices and maps sitting
// DIRECTLY in fields become []/{}, never null — "no bests has to look
// the same on both sides".
func TestWriteToNormalisesTopLevelNils(t *testing.T) {
	var nilSlice []int
	var nilMap map[string]int
	s := New()
	s.Add("empty", "nil collections read as empty on both sides", map[string]any{
		"log":   nilSlice,
		"index": nilMap,
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"log": []`) {
		t.Errorf("nil slice should write as []:\n%s", out)
	}
	if !strings.Contains(out, `"index": {}`) {
		t.Errorf("nil map should write as {}:\n%s", out)
	}
	if strings.Contains(out, "null") {
		t.Errorf("a top-level nil leaked through as null:\n%s", out)
	}
}

// The boundary of that normalisation, pinned on purpose: inside an
// app-typed value the library never walks (json tags, omitempty,
// custom marshalers make a generic deep walk a lie), so an inner nil
// slice stays null — the app's own discipline, exactly as kass
// normalises its two output fields by hand.
func TestWriteToDoesNotWalkInnerShapes(t *testing.T) {
	type inner struct {
		Log []int `json:"log"`
	}
	s := New()
	s.Add("inner", "inner shapes are the app's discipline", map[string]any{
		"wrapped": inner{},
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"log": null`) {
		t.Errorf("inner nils must NOT be normalised — that is the documented boundary:\n%s", buf.String())
	}
}

// Two identical Sets write identical bytes: field keys sorted, inner
// map keys sorted (encoding/json's own ordering), envelope first.
func TestWriteToIsDeterministic(t *testing.T) {
	build := func() *Set {
		s := New()
		s.Add("m", "map keys must not wobble between runs", map[string]any{
			"zed":   1,
			"alpha": 2,
			"tally": map[string]int{"b": 2, "a": 1, "c": 3},
		})
		return s
	}
	var one, two bytes.Buffer
	if _, err := build().WriteTo(&one); err != nil {
		t.Fatal(err)
	}
	if _, err := build().WriteTo(&two); err != nil {
		t.Fatal(err)
	}
	if one.String() != two.String() {
		t.Error("two identical Sets wrote different bytes")
	}
	out := one.String()
	if strings.Index(out, `"alpha"`) > strings.Index(out, `"zed"`) {
		t.Errorf("field keys are not sorted:\n%s", out)
	}
	if strings.Index(out, `"name"`) > strings.Index(out, `"alpha"`) {
		t.Errorf("name/why must lead each object:\n%s", out)
	}
	if strings.Index(out, `"a": 1`) > strings.Index(out, `"b": 2`) {
		t.Errorf("inner map keys are not sorted:\n%s", out)
	}
}

// The time half of the treaty: a time.Time round-trips via RFC 3339,
// so the JS side can new Date(v.now) it.
func TestWriteToTimeIsRFC3339(t *testing.T) {
	s := New()
	s.Add("clock", "times ride as RFC 3339", map[string]any{
		"now": time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
	})
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"now": "2026-01-02T15:04:05Z"`) {
		t.Errorf("time did not round-trip as RFC 3339:\n%s", buf.String())
	}
}

// name and why are the vector's envelope; a field by either name
// would duplicate a JSON key, which no parser reads back honestly.
func TestWriteToRejectsReservedFieldKeys(t *testing.T) {
	s := New()
	s.Add("x", "the envelope owns name and why", map[string]any{"why": "no"})
	if _, err := s.WriteTo(io.Discard); err == nil {
		t.Fatal("a field named why must be refused, not written as a duplicate key")
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./vectors/` — verify it FAILS to compile (`New`, `Set`, `Add`, `WriteTo` undefined). That is the failing state.
- [ ] Implement `vectors/vectors.go`:

```go
// Package vectors emits the golden vectors that pin an app's JS
// derivation engine to its Go one. Any derivation over sealed content
// runs client-side, but the sidecar, operator tools and tests want
// the same derivation in Go — so the engine exists twice, and two
// engines drifting is the most dangerous E2EE bug class: a wrong
// answer with nothing looking broken. Kass solved it by hand
// (cmd/genvectors → vectors.json → a node:test suite that must
// reproduce every vector); this package is that discipline as
// framework machinery.
//
// A Set is built by the app's own cmd/genvectors (scaffolded by
// `rastrillo vectors -init`), written to test/vectors.json by
// `rastrillo vectors`, and consumed by the app's test/parity.test.mjs
// through the vendored helper this package embeds (see JS).
//
// Two treaty rules ride the file format implicitly, said out loud
// here: the KEY NAMES in fields are part of the contract — the JS
// suite consumes them by name, changing one means changing both sides
// in the same commit, and nothing mechanical checks the key sets
// agree — and time.Time values round-trip via RFC 3339 →
// new Date(v.now): put times in as time.Time, never pre-formatted
// strings.
//
// There is deliberately no hash-pin export. Eventlog hash-pins its
// merge vectors because they are a frozen external spec; app vectors
// change by design, and `rastrillo vectors -check`'s byte-compare
// catches regenerate-drift strictly better than a hash an author
// would just update. A vectors file that IS a frozen external spec
// keeps using eventlog's plain-test pattern in the app's own suite.
package vectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// Set is an ordered collection of named vectors — ordered so the
// file reads in the order the rules were written, and two runs over
// the same cases write identical bytes.
type Set struct {
	cases []vcase
}

type vcase struct {
	name, why string
	fields    map[string]any
}

// New returns an empty Set.
func New() *Set { return &Set{} }

// Add appends one vector. name identifies it; why names the rule it
// pins — kass proved the worth of that: the JS test titles become
// "name — why". fields is the vector's payload, and its key names
// are the Go↔JS treaty ("name" and "why" are reserved for the
// vector's own envelope; WriteTo refuses them).
func (s *Set) Add(name, why string, fields map[string]any) {
	s.cases = append(s.cases, vcase{name: name, why: why, fields: fields})
}

// WriteTo emits the whole set as a JSON array, 2-space indent,
// trailing newline — test/vectors.json's exact contract. (The design
// doc sketched a bare error return; the canonical io.WriterTo shape
// is used instead so go vet's stdmethods check stays clean.)
//
// Normalisation is scoped honestly: TOP-LEVEL fields values only — a
// nil slice or nil map sitting directly in fields marshals as []/{},
// never null ("no bests has to look the same on both sides"). Inside
// app-typed values this method never walks: json tags, omitempty and
// custom marshalers make a generic deep walk a lie, so inner
// nil-vs-empty stays the app's own discipline, normalised by hand in
// its cmd/genvectors the way kass normalises its two output fields.
func (s *Set) WriteTo(w io.Writer) (int64, error) {
	var compact bytes.Buffer
	compact.WriteByte('[')
	for i, c := range s.cases {
		if i > 0 {
			compact.WriteByte(',')
		}
		if err := c.marshalInto(&compact); err != nil {
			return 0, err
		}
	}
	compact.WriteByte(']')

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return 0, err
	}
	out.WriteByte('\n')
	n, err := w.Write(out.Bytes())
	return int64(n), err
}

// marshalInto writes one case as a compact JSON object: name and why
// first (the file reads like the test titles it becomes), then the
// fields sorted by key, so regeneration is deterministic.
func (c vcase) marshalInto(buf *bytes.Buffer) error {
	keys := make([]string, 0, len(c.fields))
	for k := range c.fields {
		if k == "name" || k == "why" {
			return fmt.Errorf("vectors: case %q: field key %q is reserved for the vector envelope", c.name, k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteByte('{')
	if err := writeMember(buf, "name", c.name); err != nil {
		return err
	}
	buf.WriteByte(',')
	if err := writeMember(buf, "why", c.why); err != nil {
		return err
	}
	for _, k := range keys {
		buf.WriteByte(',')
		if err := writeMember(buf, k, normalizeTopLevel(c.fields[k])); err != nil {
			return fmt.Errorf("vectors: case %q, field %q: %w", c.name, k, err)
		}
	}
	buf.WriteByte('}')
	return nil
}

func writeMember(buf *bytes.Buffer, key string, val any) error {
	kb, err := json.Marshal(key)
	if err != nil {
		return err
	}
	vb, err := json.Marshal(val)
	if err != nil {
		return err
	}
	buf.Write(kb)
	buf.WriteByte(':')
	buf.Write(vb)
	return nil
}

// normalizeTopLevel is the whole of the library's normalisation: a
// nil slice becomes an empty one, a nil map an empty one — and
// nothing else, at no other depth.
func normalizeTopLevel(v any) any {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return v
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l vectors/ && go vet ./vectors/ && go test ./vectors/` — verify all tests PASS, gofmt output empty, vet clean.
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — verify the whole tree is still green.
- [ ] Commit:

```
vectors: a Set that writes the treaty file, normalised at the top level only

An ordered set of named cases (name, why, fields), emitted as the
JSON array test/vectors.json carries: 2-space indent, envelope first,
fields sorted, trailing newline. Nil slices and maps are normalised
to []/{} at the top level of fields ONLY — inside app-typed values
the library never walks, because json tags, omitempty and custom
marshalers make a generic deep walk a lie; that stays the app's
discipline, and the -init template says so where the app will read
it. Deliberately no hash-pin export: app vectors change by design,
and -check's byte-compare catches regenerate-drift strictly better
than a hash an author would just update.

WriteTo takes the canonical io.WriterTo shape rather than the design
sketch's bare error: go vet's stdmethods check rejects a WriteTo
that almost-implements the interface, and a clean vet outranks a
sketch signature.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 2: `vectors/js/vectors.mjs` — the vendored helper, embedded and pinned

**Files**
- Create: `vectors/js/vectors.mjs`, `vectors/js.go`, `vectors/js/canonical.test.mjs`
- Test: `vectors/js_test.go`

**Interfaces**
- Consumes: `embed`, `os/exec` (node shim), node's `node:test` / `node:assert/strict` / `node:fs`.
- Produces: `func JS() []byte`; ES exports `loadVectors(path)` and `canonical(x)`.

**Steps**

- [ ] Write the failing test file `vectors/js_test.go`:

```go
package vectors

import (
	"os/exec"
	"strings"
	"testing"
)

// TestJSHelperVocabulary pins the helper's contract the way ui's
// shim tests pin theirs: the exports the scaffolded parity suite
// imports by name, and canonical()'s exact comparison rule — the
// zero-strip included, blind spot and all — because a helper that
// quietly stopped dropping 0/false/"" would fail every app's suite
// on encoder behaviour rather than arithmetic.
func TestJSHelperVocabulary(t *testing.T) {
	js := string(JS())
	for _, want := range []string{
		"export function loadVectors",
		"export function canonical",
		"Object.keys(value).sort()",
		"if (v === undefined || v === null) continue;",
		`if (v === 0 || v === false || v === "") continue; // Go's omitempty`,
		"blind spot",
		"Empty arrays are kept",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("vectors.mjs does not contain %q", want)
		}
	}
	if strings.Contains(js, "\t") {
		t.Error("vectors.mjs uses two-space indentation, not tabs")
	}
}

// The helper must never be executed as a test by bare `node --test`
// discovery — that is why it is vectors.mjs, not *.test.mjs. Pin the
// name through the embed path.
func TestJSHelperIsNotNamedLikeATest(t *testing.T) {
	if strings.Contains(string(JS()), "node:test") {
		t.Error("the helper imports node:test; it must stay a plain module, never a test file")
	}
}

// TestJSHelperBehaviour runs the helper's own node:test suite
// (js/canonical.test.mjs) — the zero-strip cases through a real JS
// engine — when node is on PATH. Skipped otherwise: the twin is part
// of the contract, but a Go toolchain without node still gets a
// green, honest build.
func TestJSHelperBehaviour(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS helper suite not exercised")
	}
	out, err := exec.Command(node, "--test", "js/canonical.test.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test js/canonical.test.mjs failed: %v\n%s", err, out)
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./vectors/` — verify it FAILS to compile (`JS` undefined).
- [ ] Create `vectors/js/vectors.mjs` (two-space indent, no tabs):

```js
// vectors.mjs — rastrillo's parity-vector helper, vendored into an
// app once by `rastrillo vectors -init` as test/vectors.mjs and
// app-owned from then on (the tokens.css/shim contract): edit it
// deliberately and delete the pin in internal/<pkg>test/
// vectors_vendored_test.go, or leave it and let a framework upgrade
// re-copy it. Named vectors.mjs — not *.test.mjs — so bare
// `node --test` discovery never executes the helper as a test.

import { readFileSync } from "node:fs";

// loadVectors reads and parses a vectors.json written by
// `rastrillo vectors`. Resolve the path from your suite's own URL —
// loadVectors(fileURLToPath(new URL("./vectors.json", import.meta.url)))
// — so the suite is independent of the directory node started in.
export function loadVectors(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

// canonical is the comparison rule, blind spot included: sort object
// keys recursively; drop undefined/null members; drop scalar zero
// values (0, false, "") to match Go's omitempty. Without the
// zero-strip, Go dropping a zero field while JS computes it fails
// the diff on encoder behaviour rather than arithmetic — but the
// same rule means a meaningful explicit zero on one side and a
// missing field on the other compare equal. Cover that hole with
// explicit-value assertions beside the loop: the scaffolded
// parity.test.mjs carries a marked BELT section for exactly this.
// Empty arrays are kept as [] on both sides — `rastrillo vectors`'
// generator supplies them from Go by normalising top-level nils.
export function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === "object") {
    const out = {};
    for (const key of Object.keys(value).sort()) {
      const v = canonical(value[key]);
      if (v === undefined || v === null) continue;
      if (v === 0 || v === false || v === "") continue; // Go's omitempty
      out[key] = v;
    }
    return out;
  }
  return value;
}
```

- [ ] Create `vectors/js/canonical.test.mjs` (two-space indent):

```js
// The helper's own suite: canonical()'s rule, zero-strip and blind
// spot included, pinned through a real engine. Run by the package's
// TestJSHelperBehaviour shim; named *.test.mjs deliberately — this
// one IS a test.

import { test } from "node:test";
import assert from "node:assert/strict";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { loadVectors, canonical } from "./vectors.mjs";

test("canonical sorts object keys recursively", () => {
  assert.equal(
    JSON.stringify(canonical({ b: { d: 2, c: 1 }, a: 3 })),
    JSON.stringify({ a: 3, b: { c: 1, d: 2 } }),
  );
});

test("canonical drops null and undefined members", () => {
  assert.deepEqual(canonical({ a: 1, b: null, c: undefined }), { a: 1 });
});

test("canonical drops scalar zeros to match Go's omitempty", () => {
  assert.deepEqual(canonical({ n: 0, f: false, s: "", keep: 1 }), { keep: 1 });
});

test("the blind spot is real: an explicit zero equals a missing field", () => {
  // The documented hole the scaffolded belt section exists for —
  // pinned so nobody "fixes" it without noticing what breaks.
  assert.deepEqual(canonical({ reps: 0 }), canonical({}));
});

test("empty arrays are kept", () => {
  assert.deepEqual(canonical({ log: [] }), { log: [] });
});

test("arrays recurse without being pruned", () => {
  assert.deepEqual(canonical([{ b: 0, a: 1 }]), [{ a: 1 }]);
});

test("scalars pass through untouched", () => {
  assert.equal(canonical(0), 0);
  assert.equal(canonical("x"), "x");
  assert.equal(canonical(null), null);
});

test("loadVectors reads and parses a vectors file", () => {
  const path = join(mkdtempSync(join(tmpdir(), "vectors-")), "vectors.json");
  writeFileSync(path, '[{"name":"one","why":"exists"}]\n');
  const vs = loadVectors(path);
  assert.equal(vs.length, 1);
  assert.equal(vs[0].name, "one");
});
```

- [ ] Create `vectors/js.go`:

```go
package vectors

import _ "embed"

//go:embed js/vectors.mjs
var jsHelper []byte

// JS returns the raw bytes of the vendored JS helper
// (js/vectors.mjs): loadVectors and canonical, the vocabulary the
// scaffolded parity suite imports by name. `rastrillo vectors -init`
// delivers it to an app once as test/vectors.mjs, app-owned after —
// the tokens.css/shim contract — with a byte-identity pin scaffolded
// alongside so vendored-then-forgotten is caught instead of drifting
// silently.
func JS() []byte { return jsHelper }
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l vectors/ && go vet ./vectors/ && go test ./vectors/` — verify PASS (the node leg runs where node exists; on a node-less machine confirm it reports SKIP, not FAIL).
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green.
- [ ] Commit:

```
vectors: vendor the JS helper — kass's real canonical(), blind spot included

loadVectors and canonical as an embedded ES module (the crypto/js.go
shape), delivered to apps once by -init and pinned byte-identical
after. canonical sorts keys recursively, drops undefined/null members
and drops scalar zeros to match Go's omitempty — which also means an
explicit zero and a missing field compare equal; that hole is
documented at the definition and covered by the belt section the
parity template scaffolds. Named vectors.mjs, never *.test.mjs, so
bare node --test discovery cannot execute the helper as a test; its
own suite (canonical.test.mjs) pins the rule through a real engine,
skipped honestly when node is absent.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 3: the verb — `rastrillo vectors [dir]` runs the app's generator

**Files**
- Create: `cmd/rastrillo/vectors.go`
- Modify: `cmd/rastrillo/main.go` (dispatch case + usage text)
- Test: `cmd/rastrillo/vectors_test.go`

**Interfaces**
- Consumes: `flag.NewFlagSet` (the generate.go pattern: flags before dir, `filepath.Abs`), `exec.Command("go", "run", "./cmd/genvectors")` with `cmd.Dir = dir` (the goEval precedent), `scaffold(t, files)` and `repoRoot(t)` from `cmd/rastrillo/generate_test.go`.
- Produces: `func runVectors(args []string) error`, `func vectorsGenerate(dir string) error`, `func runGenvectors(dir string) ([]byte, error)`.

**Steps**

- [ ] Write the failing test file `cmd/rastrillo/vectors_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureGenvectorsSrc is a stand-in generator with no rastrillo
// dependency at all, so these tests run offline and fast: what the
// verb needs from cmd/genvectors is only "a package main that prints
// the vectors file to stdout".
const fixtureGenvectorsSrc = `package main

import "fmt"

func main() {
	fmt.Print("[\n  {\n    \"name\": \"one\",\n    \"why\": \"fixture\"\n  }\n]\n")
}
`

func TestVectorsWritesTheAppsVectors(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("runVectors: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		t.Fatalf("expected test/vectors.json: %v", err)
	}
	want := "[\n  {\n    \"name\": \"one\",\n    \"why\": \"fixture\"\n  }\n]\n"
	if string(b) != want {
		t.Errorf("test/vectors.json = %q, want %q", b, want)
	}
}

// Convention over configuration, with guidance when the convention
// is not met: no cmd/genvectors means an error that names both the
// missing piece and the one command that scaffolds it.
func TestVectorsErrorsWithGuidanceWithoutAGenerator(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	err := runVectors([]string{dir})
	if err == nil {
		t.Fatal("want an error: the app has no cmd/genvectors")
	}
	for _, want := range []string{"cmd/genvectors", "-init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

// A generator that fails must surface its own stderr, not a bare
// exit status — the goEval precedent.
func TestVectorsSurfacesTheGeneratorsStderr(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vector case exploded")
	os.Exit(1)
}
`,
	})
	err := runVectors([]string{dir})
	if err == nil {
		t.Fatal("want the generator's failure to propagate")
	}
	if !strings.Contains(err.Error(), "vector case exploded") {
		t.Errorf("error should carry the generator's stderr: %v", err)
	}
}

func TestVectorsRejectsInitPlusCheck(t *testing.T) {
	if err := runVectors([]string{"-init", "-check", "."}); err == nil {
		t.Fatal("-init and -check together must be refused")
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./cmd/rastrillo/` — verify FAIL to compile (`runVectors` undefined).
- [ ] Create `cmd/rastrillo/vectors.go` (`vectorsCheck` and `vectorsInit` arrive in Tasks 4 and 5; stub them here so this task compiles):

```go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runVectors implements `rastrillo vectors [flags] [dir]`: Go↔JS
// parity vectors as a verb (design doc: 2026-08-23-vectors-verb).
// The derivation engine an app runs client-side exists twice by
// necessity, and two engines drifting is the E2EE bug class where a
// wrong answer looks fine — so the app's cmd/genvectors enumerates
// golden cases from the Go engine, this verb writes them to
// test/vectors.json, and the app's JS suite must reproduce every one.
//
// Flags come before the directory, exactly as runGenerate parses:
// FlagSet.Parse stops at the first non-flag argument.
func runVectors(args []string) error {
	fset := flag.NewFlagSet("vectors", flag.ContinueOnError)
	check := fset.Bool("check", false, "verify without writing: regenerate + byte-compare, then run the JS parity suite (node required)")
	initMode := fset.Bool("init", false, "scaffold cmd/genvectors, the test/ parity suite, and the go-test belt into an existing app")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if *check && *initMode {
		return fmt.Errorf("-init scaffolds and -check gates; pick one")
	}

	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if *initMode {
		return vectorsInit(dir)
	}
	if *check {
		return vectorsCheck(dir)
	}
	return vectorsGenerate(dir)
}

// vectorsGenerate is the plain mode: run the app's own generator and
// write its stdout to test/vectors.json. The root test/ directory is
// the convention here (new for the scaffold; kass used web/test/) —
// the JS suite is neither a Go package nor a static asset, so it
// gets a home that is neither.
func vectorsGenerate(dir string) error {
	out, err := runGenvectors(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "test"), 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "test", "vectors.json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("rastrillo vectors: wrote test/vectors.json (%d bytes)\n", len(out))
	return nil
}

// runGenvectors runs the app's own `go run ./cmd/genvectors` with
// the working directory set to the app's module root — the manifest
// goEval precedent — and returns its stdout. The generator is the
// app's own package main (kass's shape verbatim): it imports the
// app's pure fold, enumerates cases, prints a vectors.Set.
// Convention over configuration: no cmd/genvectors, no vectors — the
// error says what to do about it.
func runGenvectors(dir string) ([]byte, error) {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "genvectors")); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no cmd/genvectors in %s; the vectors verb runs the app's own generator — scaffold one with `rastrillo vectors -init`", dir)
		}
		return nil, err
	}
	cmd := exec.Command("go", "run", "./cmd/genvectors")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("go run ./cmd/genvectors: %s", msg)
	}
	return stdout.Bytes(), nil
}

// vectorsCheck lands in the -check task.
func vectorsCheck(dir string) error {
	return fmt.Errorf("vectors -check: not implemented yet")
}

// vectorsInit lands in the -init task.
func vectorsInit(dir string) error {
	return fmt.Errorf("vectors -init: not implemented yet")
}
```

- [ ] Modify `cmd/rastrillo/main.go`: add the dispatch case after `case "migration":`:

```go
	case "vectors":
		err = runVectors(os.Args[2:])
```

  and add to `usage()`'s string, after the `rastrillo migration` block (before the closing backtick):

```
  rastrillo vectors [flags] [dir]               Go↔JS parity vectors: run cmd/genvectors, write test/vectors.json (default dir: .)
       -init                                     scaffold cmd/genvectors, the test/ parity suite, and the go-test belt (once)
       -check                                    pre-ship gate: regenerate + byte-compare, then node --test test/parity.test.mjs
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l cmd/ && go vet ./cmd/... && go test ./cmd/rastrillo/` — verify the new tests PASS and nothing else broke.
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green.
- [ ] Commit:

```
rastrillo vectors: run the app's own generator, write test/vectors.json

One more case in the dispatch; the optional dir argument resolves and
absolutises like generate's, and the generator runs as
`go run ./cmd/genvectors` with the command's working directory set to
the app's module root — the manifest goEval precedent. The generator
is the app's own package main, kass's shape verbatim; when it does
not exist the verb errors with the one command that scaffolds it
rather than inventing configuration. Its stdout lands in the root
test/ directory — a new scaffold convention, chosen because the JS
suite is neither a Go package nor a static asset.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 4: `-check` — byte-compare leg plus the node leg, loud on purpose

**Files**
- Modify: `cmd/rastrillo/vectors.go` (replace the `vectorsCheck` stub)
- Test: `cmd/rastrillo/vectors_test.go` (append)

**Interfaces**
- Consumes: `runGenvectors(dir)`, `bytes.Equal`, `exec.LookPath("node")`, `exec.Command(node, "--test", "test/parity.test.mjs")` with `cmd.Dir = dir`.
- Produces: `func vectorsCheck(dir string) error` — also called by `runGenerate` in Task 6.

**Steps**

- [ ] Append the failing tests to `cmd/rastrillo/vectors_test.go`:

```go
// passingParityFixture and failingParityFixture stand in for an
// app's real suite: the -check contract under test here is "run this
// exact file and believe its exit code", not the suite's content.
const passingParityFixture = `import { test } from "node:test";
test("ok", () => {});
`

const failingParityFixture = `import { test } from "node:test";
test("no", () => { throw new Error("JS engine disagrees"); });
`

// needsNode skips a test leg that cannot run without node — the
// crypto/js_test.go posture, for the legs where a skip stays honest.
func needsNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS leg not exercised")
	}
	return node
}

func TestVectorsCheckFailsWithoutACommittedFile(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
	})
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: nothing committed to check against")
	}
	if !strings.Contains(err.Error(), "test/vectors.json") {
		t.Errorf("error should name the missing file: %v", err)
	}
}

// Leg 1: a diff means the Go engine changed without regenerating in
// the same commit. It must fail BEFORE the node leg, so this test
// needs no node at all.
func TestVectorsCheckFailsOnByteDrift(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/vectors.json":      "[]\n",
	})
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: the committed file does not match a regenerate")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("error should say the file is stale: %v", err)
	}
}

// Leg 2's precondition, spec §1.3: in check mode a missing node is a
// FAILURE, not a skip — silent while iterating, loud before ship.
// PATH is narrowed to a directory holding only go, so the go run in
// leg 1 still works while LookPath("node") cannot.
func TestVectorsCheckDemandsNode(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   passingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(goBin, filepath.Join(binDir, "go")); err != nil {
		t.Skipf("cannot symlink go: %v", err)
	}
	t.Setenv("PATH", binDir)
	err = runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: check mode without node must be loud, never a skip")
	}
	if !strings.Contains(err.Error(), "node") {
		t.Errorf("error should say node is required: %v", err)
	}
}

func TestVectorsCheckGreen(t *testing.T) {
	needsNode(t)
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   passingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	if err := runVectors([]string{"-check", dir}); err != nil {
		t.Fatalf("both legs should be green: %v", err)
	}
}

func TestVectorsCheckFailsWhenTheJSSuiteFails(t *testing.T) {
	needsNode(t)
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   failingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: the JS suite failed")
	}
	if !strings.Contains(err.Error(), "parity.test.mjs") {
		t.Errorf("error should name the suite that failed: %v", err)
	}
}
```

  Add `"os/exec"` to the test file's imports.

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./cmd/rastrillo/ -run TestVectorsCheck` — verify the new tests FAIL against the stub ("not implemented yet").
- [ ] Replace the `vectorsCheck` stub in `cmd/rastrillo/vectors.go` with:

```go
// vectorsCheck is the pre-ship gate, loud on purpose (spec §1.3):
// silent while iterating, failing before ship. Leg 1 regenerates and
// byte-compares against the committed test/vectors.json — a diff
// means the Go engine changed without `rastrillo vectors` in the
// same commit. Leg 2 runs the JS parity suite as an EXPLICIT file,
// never a directory: `node --test <dir>` stopped working on Node
// ≥ 21 (kass's own Makefile line is bit-rotted this way). Here a
// missing node is a FAILURE, not the skip the Go-side belt test
// allows itself, because a gate that quietly skipped one engine
// would be the drift it exists to catch. Also run by `rastrillo
// generate -check` when cmd/genvectors exists — one gate before
// ship, not two to remember.
func vectorsCheck(dir string) error {
	fresh, err := runGenvectors(dir)
	if err != nil {
		return err
	}
	committed, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no test/vectors.json to check against; run `rastrillo vectors` and commit the result")
		}
		return err
	}
	if !bytes.Equal(fresh, committed) {
		return fmt.Errorf("test/vectors.json is stale: a regenerate differs from the committed file — the Go engine changed without regenerating; run `rastrillo vectors` and commit the result in the same commit as the engine change")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("check mode requires node to run the JS half of the gate (test/parity.test.mjs): a check that skipped one engine would be the drift it exists to catch")
	}
	cmd := exec.Command(node, "--test", "test/parity.test.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("node --test test/parity.test.mjs failed — the JS engine disagrees with the Go one, or the suite is broken: %v\n%s", err, out)
	}
	fmt.Println("rastrillo vectors -check: vectors regenerate byte-identical, JS parity suite green")
	return nil
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l cmd/ && go vet ./cmd/... && go test ./cmd/rastrillo/` — verify PASS (node-dependent legs skip where node is absent, except TestVectorsCheckDemandsNode which manufactures its own node-less PATH).
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green.
- [ ] Commit:

```
rastrillo vectors -check: byte-compare plus the JS leg, loud before ship

Leg one regenerates and byte-compares against the committed
test/vectors.json — a diff is the Go engine having changed without a
regenerate in the same commit, which is exactly the drift a hash-pin
would only launder. Leg two runs node --test on the explicit parity
file, never a directory: directory arguments stopped working on Node
21, and kass's own Makefile line is bit-rotted that way. In check
mode a missing node is a failure, not a skip — the go-test belt gets
to skip so builds stay honest, but a pre-ship gate that quietly ran
half its engines would be the bug class this verb exists for.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 5: `-init` — scaffold the parity kit into an existing app

**Files**
- Create: `cmd/rastrillo/vectorsinit.go`
- Modify: `cmd/rastrillo/vectors.go` (delete the `vectorsInit` stub)
- Test: `cmd/rastrillo/vectorsinit_test.go`

**Interfaces**
- Consumes: `modulePath(dir)` (modpath.go), `packageName(name)` (new.go), `path.Base`, `vectors.JS()` (`amadan.net/rastrillo/rastrillo/vectors` — new import for cmd/rastrillo), `eventlog.Derive` (referenced by the emitted template, not by the CLI itself).
- Produces: `func vectorsInit(dir string) error`; scaffolded files `cmd/genvectors/main.go`, `test/parity.test.mjs`, `test/vectors.mjs`, `internal/<pkg>test/parity_test.go`, `internal/<pkg>test/vectors_vendored_test.go`.

**Steps**

- [ ] Write the failing test file `cmd/rastrillo/vectorsinit_test.go`:

```go
package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/vectors"
)

// readInit reads one -init-scaffolded file or fails the test — the
// readScaffold shape, absolute because runVectors absolutised dir.
func readInit(t *testing.T, dir string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{dir}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(b)
}

func TestVectorsInitScaffoldsTheParityKit(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}

	gen := readInit(t, dir, "cmd", "genvectors", "main.go")
	for _, want := range []string{
		"TREATY",
		"same commit",
		"RFC 3339",
		"eventlog.Derive",
		"vectors.New()",
		"TOP LEVEL",
	} {
		if !strings.Contains(gen, want) {
			t.Errorf("genvectors template does not carry %q", want)
		}
	}

	parity := readInit(t, dir, "test", "parity.test.mjs")
	for _, want := range []string{
		`from "./vectors.mjs"`,
		"new Date(v.now)",
		"canonical(",
		"vectors.length >= 2",
		"did generation fail?",
		"BELT",
		"same commit",
	} {
		if !strings.Contains(parity, want) {
			t.Errorf("parity template does not carry %q", want)
		}
	}

	belt := readInit(t, dir, "internal", "demotest", "parity_test.go")
	for _, want := range []string{
		`exec.LookPath("node")`,
		"t.Skip",
		`"--test", "test/parity.test.mjs"`,
		`cmd.Dir = "../.."`,
	} {
		if !strings.Contains(belt, want) {
			t.Errorf("go belt template does not carry %q", want)
		}
	}

	pin := readInit(t, dir, "internal", "demotest", "vectors_vendored_test.go")
	for _, want := range []string{
		"vectors.JS()",
		`filepath.Join("..", "..", "test", "vectors.mjs")`,
		"delete this pin",
	} {
		if !strings.Contains(pin, want) {
			t.Errorf("vendored-pin template does not carry %q", want)
		}
	}

	for name, src := range map[string]string{
		"main.go":                  gen,
		"parity_test.go":           belt,
		"vectors_vendored_test.go": pin,
	} {
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, 0); err != nil {
			t.Errorf("scaffolded %s does not parse: %v", name, err)
		}
	}

	vendored := readInit(t, dir, "test", "vectors.mjs")
	if !bytes.Equal([]byte(vendored), vectors.JS()) {
		t.Error("test/vectors.mjs is not byte-identical to vectors.JS()")
	}
}

// Delivered once, app-owned after: a second -init must refuse, not
// clobber files the app may have grown into its own.
func TestVectorsInitRefusesToClobber(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("first -init: %v", err)
	}
	err := runVectors([]string{"-init", dir})
	if err == nil {
		t.Fatal("second -init must refuse to overwrite app-owned files")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should say what already exists: %v", err)
	}
}

// The internal/<pkg>test destination follows the scaffold's own
// derivation: module path base, cleaned to an identifier.
func TestVectorsInitDerivesPkgFromModulePath(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module github.com/acme/fancy-app\n\ngo 1.24\n"})
	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}
	belt := readInit(t, dir, "internal", "fancyapptest", "parity_test.go")
	if !strings.Contains(belt, "package fancyapptest") {
		t.Errorf("belt should live in package fancyapptest:\n%s", belt)
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./cmd/rastrillo/ -run TestVectorsInit` — verify FAIL against the stub.
- [ ] Create `cmd/rastrillo/vectorsinit.go` (delete the `vectorsInit` stub from vectors.go in the same change):

```go
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"amadan.net/rastrillo/rastrillo/vectors"
)

// vectorsInit scaffolds the app-side parity kit into an existing app
// (spec §1.3): the generator, the JS suite, the vendored helper, and
// the Go-side belt + pin from §1.5. Not part of `rastrillo new` —
// most apps have no client-side fold; adoption is this one command
// when they grow one. Every file is delivered once and app-owned
// after (the tokens.css contract), so an existing file is a refusal,
// never an overwrite.
func vectorsInit(dir string) error {
	module, err := modulePath(dir)
	if err != nil {
		return err
	}
	pkg := packageName(path.Base(module))

	files := map[string]string{
		filepath.Join(dir, "cmd", "genvectors", "main.go"):                     genvectorsTemplate,
		filepath.Join(dir, "test", "parity.test.mjs"):                          parityTestTemplate,
		filepath.Join(dir, "test", "vectors.mjs"):                              string(vectors.JS()),
		filepath.Join(dir, "internal", pkg+"test", "parity_test.go"):           fmt.Sprintf(parityGoTemplate, pkg),
		filepath.Join(dir, "internal", pkg+"test", "vectors_vendored_test.go"): fmt.Sprintf(vectorsVendoredTemplate, pkg),
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%s already exists; vectors -init delivers once and the files are app-owned after — delete it first if you really want a fresh copy", p)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(files[p]), 0o644); err != nil {
			return err
		}
	}

	fmt.Println("rastrillo vectors -init: scaffolded —")
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			rel = p
		}
		fmt.Printf("  %s\n", rel)
	}
	fmt.Println("next: replace the example fold in cmd/genvectors/main.go and test/parity.test.mjs with your app's own (both sides, same commit), then run `rastrillo vectors`")
	return nil
}

// genvectorsTemplate is the worked generator, anchored on
// eventlog.Derive: a tiny example fold, two cases, and the treaty
// said where the app will read it.
const genvectorsTemplate = `// Command genvectors writes the golden vectors that pin this app's
// JS derivation engine to the Go one. The engine exists twice
// because the derivation runs client-side too, and two engines
// drifting is the bug class where a wrong answer looks fine.
//
// Regenerate with: rastrillo vectors
// Gate with:       rastrillo vectors -check
//                  (also runs under rastrillo generate -check)
//
// TREATY: the field key names below ("events", "now", "tally") are
// shared with test/parity.test.mjs, which consumes them by name.
// Nothing mechanical checks the two key sets agree — change one
// side, change the other in the same commit. time.Time values
// round-trip as RFC 3339: put times in as time.Time (the JS side
// does new Date(v.now)), never as pre-formatted strings.
//
// The fold below is a worked placeholder anchored on
// eventlog.Derive. Replace tally/fold with your app's real read
// model, keep one Add per rule, and give every vector a why — the JS
// test titles become "name — why".
package main

import (
	"fmt"
	"os"
	"time"

	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/vectors"
)

// tally is the example read model: how many events the stream holds,
// and whether it has gone quiet.
//
// vectors.Set normalises nil slices/maps at the TOP LEVEL of Add's
// fields only; inside app-typed values like this one, omitempty and
// nil-vs-empty are this file's own discipline (kass normalises its
// two output fields by hand for exactly this reason).
type tally struct {
	Count int  ` + "`" + `json:"count"` + "`" + `
	Quiet bool ` + "`" + `json:"quiet"` + "`" + `
}

// fold is the Go half of the derivation under treaty: a pure fold
// over the event log (eventlog.Derive), plus one rule that needs the
// clock. Same events, same now, same answer — in both languages.
func fold(events []eventlog.Event, now time.Time) tally {
	t := eventlog.Derive(events, func(t tally, ev eventlog.Event) tally {
		t.Count++
		return t
	})
	t.Quiet = true
	for _, ev := range events {
		if now.Sub(ev.TS) < 24*time.Hour {
			t.Quiet = false
		}
	}
	return t
}

var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func event(seq int64, kind string, at time.Time) eventlog.Event {
	return eventlog.Event{Stream: "s", Writer: "w", Seq: seq, Lamport: seq, TS: at, Actor: "human", Kind: kind}
}

func main() {
	set := vectors.New()

	// One Add per rule. A nil events slice is fine at the top level:
	// Set.WriteTo writes it as [], so "no events" looks the same on
	// both sides of the comparison.
	set.Add("empty-log", "no events: count zero, and quiet", map[string]any{
		"events": []eventlog.Event(nil),
		"now":    epoch,
		"tally":  fold(nil, epoch),
	})

	recent := []eventlog.Event{
		event(1, "note.created", epoch.Add(-48*time.Hour)),
		event(2, "note.edited", epoch.Add(-time.Hour)),
	}
	set.Add("recent-activity", "an event within a day: counted, not quiet", map[string]any{
		"events": recent,
		"now":    epoch,
		"tally":  fold(recent, epoch),
	})

	if _, err := set.WriteTo(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

// parityTestTemplate is the scaffolded JS suite: the parity loop,
// kass's sanity floor on vector count, and the marked belt section
// that covers canonical()'s documented blind spot with explicit
// values. JS string concatenation instead of template literals keeps
// backticks out of this Go const.
const parityTestTemplate = `// The JS engine must agree with the Go engine, vector for vector.
//
// Regenerate the vectors with: rastrillo vectors
// This suite runs under:       rastrillo vectors -check
//                              (and go test ./... via the belt in
//                              internal/<pkg>test/parity_test.go)
//
// TREATY: the field key names ("events", "now", "tally") are shared
// with cmd/genvectors/main.go, which writes them. Nothing mechanical
// checks the key sets agree — change one side, change the other in
// the same commit. Times arrive as RFC 3339 strings: new Date(v.now).

import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";

import { loadVectors, canonical } from "./vectors.mjs";

const vectors = loadVectors(fileURLToPath(new URL("./vectors.json", import.meta.url)));

// fold is the JS half of the derivation under treaty. The scaffold
// keeps it inline; when your real engine lives in its own module,
// replace this with an import of it.
function fold(events, now) {
  const t = { count: 0, quiet: true };
  for (const ev of events) {
    t.count += 1;
    if (now - new Date(ev.ts) < 24 * 60 * 60 * 1000) t.quiet = false;
  }
  return t;
}

// The sanity floor: a generation failure that writes few vectors
// must not pass as a short green run. Raise it as vectors accrue.
test("there are vectors to check", () => {
  assert.ok(vectors.length >= 2, "only " + vectors.length + " vectors — did generation fail?");
});

for (const v of vectors) {
  test(v.name + " — " + v.why, () => {
    const got = fold(v.events, new Date(v.now));
    assert.deepEqual(canonical(got), canonical(v.tally),
      "JS and Go disagree on " + v.name);
  });
}

// ── BELT: explicit-value assertions ────────────────────────────────
// canonical() drops 0, false and "" to match Go's omitempty, so the
// loop above cannot tell a meaningful zero from a missing field. Pin
// every meaningful zero here by hand — this section is the other
// half of the comparison rule, not an optional extra.

function byName(name) {
  const v = vectors.find((x) => x.name === name);
  assert.ok(v, "no vector named " + name);
  return v;
}

test("belt: an empty log counts zero, explicitly", () => {
  const v = byName("empty-log");
  const got = fold(v.events, new Date(v.now));
  assert.equal(got.count, 0);
  assert.equal(got.quiet, true);
});

test("belt: recent activity is not quiet, explicitly", () => {
  const v = byName("recent-activity");
  const got = fold(v.events, new Date(v.now));
  assert.equal(got.count, 2);
  assert.equal(got.quiet, false);
});
`

// parityGoTemplate is the Go-side belt (spec §1.5): the crypto
// js_test.go exec-shim, with the working directory corrected for
// where go test actually runs it.
const parityGoTemplate = `package %[1]stest

import (
	"os/exec"
	"testing"
)

// TestJSParity runs the JS parity suite (test/parity.test.mjs) — the
// same vectors cmd/genvectors wrote, through the JS engine — when
// node is on PATH. Skipped otherwise: a Go toolchain without node
// still gets a green, honest build; ` + "`" + `rastrillo vectors -check` + "`" + ` is
// where a missing node turns loud.
//
// go test runs this file in internal/%[1]stest/, so the shim sets
// the working directory to the module root and names the parity file
// from there — an explicit file, never a directory, because
// ` + "`" + `node --test <dir>` + "`" + ` stopped working on Node 21.
func TestJSParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS parity suite not exercised")
	}
	cmd := exec.Command(node, "--test", "test/parity.test.mjs")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test test/parity.test.mjs failed: %%v\n%%s", err, out)
	}
}
`

// vectorsVendoredTemplate pins the vendored helper byte-identical to
// the library copy. A SEPARATE file from the scaffold's
// vendored_test.go on purpose: that test's map is bare filenames
// rooted at internal/<pkg>/static/, and this copy lives at the
// module root's test/ — a non-static path the existing map cannot
// hold.
const vectorsVendoredTemplate = `package %[1]stest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/vectors"
)

// rastrillo vectors -init delivered test/vectors.mjs once; it is
// app-owned from then on. This pin keeps the vendored copy
// byte-identical to the library's vectors.JS(), so a framework
// upgrade that forgets to re-copy is caught instead of drifting
// silently. If you edit the helper DELIBERATELY, delete this pin —
// the file is yours.
func TestVendoredVectorsHelperMatchesTheLibrary(t *testing.T) {
	vendored, err := os.ReadFile(filepath.Join("..", "..", "test", "vectors.mjs"))
	if err != nil {
		t.Fatalf("read vendored test/vectors.mjs: %%v", err)
	}
	if !bytes.Equal(vendored, vectors.JS()) {
		t.Error("test/vectors.mjs differs from the library copy; re-copy it (or delete this pin if the edit was deliberate)")
	}
}
`
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l cmd/ && go vet ./cmd/... && go test ./cmd/rastrillo/` — verify PASS.
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green.
- [ ] Commit:

```
rastrillo vectors -init: scaffold the parity kit into an existing app

Five files, delivered once and app-owned after, refused rather than
overwritten on a second run: a worked cmd/genvectors anchored on
eventlog.Derive with the treaty written where the app will read it
(key names shared by name, changed both sides in one commit; times
as time.Time, RFC 3339 on the wire); a parity suite with kass's
vector-count floor and a marked belt of explicit-value assertions,
because canonical()'s zero-strip cannot tell a meaningful zero from
a missing field; the vendored helper; the crypto-shaped go-test belt
with cmd.Dir set to the module root, since go test runs it two
levels down; and a separate byte-identity pin for the helper — the
scaffold's vendored_test.go map holds bare static/ filenames, and
test/vectors.mjs is not one. Not part of rastrillo new: most apps
have no client-side fold, and adoption is one command when they
grow one.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 6: fold the vectors gate into `rastrillo generate -check`

**Files**
- Modify: `cmd/rastrillo/generate.go`
- Test: `cmd/rastrillo/vectors_test.go` (append)

**Interfaces**
- Consumes: `vectorsCheck(dir)` from Task 4, `os.Stat(filepath.Join(dir, "cmd", "genvectors"))`.
- Produces: no new symbols — `runGenerate`'s `--check` branch grows one gate.

**Steps**

- [ ] Append the failing tests to `cmd/rastrillo/vectors_test.go`:

```go
// Spec §1.4: when cmd/genvectors exists under the resolved app root,
// generate -check additionally runs the vectors check — one gate
// before ship, not two to remember. The stale committed file makes
// the byte-compare leg fail, so no node is needed here.
func TestGenerateCheckRunsTheVectorsGate(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/vectors.json":      "[]\n",
	})
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want the vectors byte-compare failure to surface through generate --check")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("error should be the vectors staleness failure: %v", err)
	}
}

// No cmd/genvectors, no gate: vectors stay opt-in, and every
// existing app's --check is untouched.
func TestGenerateCheckIgnoresVectorsWithoutAGenerator(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("an app with no cmd/genvectors must not grow a vectors gate: %v", err)
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./cmd/rastrillo/ -run 'TestGenerateCheckRunsTheVectorsGate|TestGenerateCheckIgnoresVectorsWithoutAGenerator'` — verify the first FAILS (generate --check passes today where it must now fail).
- [ ] Modify `cmd/rastrillo/generate.go`: inside the `if *check {` block, insert immediately after the unknown-icons block (after its closing `}` and before `fmt.Printf("rastrillo generate --check: …")`):

```go
		// Go↔JS parity vectors (vectors design §1.4): when the app has
		// a cmd/genvectors, the vectors check joins this gate —
		// regenerate-and-byte-compare plus the JS parity suite — so
		// there is one gate before ship, not two to remember. Absent
		// generator, absent gate: vectors are opt-in.
		if _, err := os.Stat(filepath.Join(dir, "cmd", "genvectors")); err == nil {
			if err := vectorsCheck(dir); err != nil {
				return fmt.Errorf("vectors check: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l cmd/ && go vet ./cmd/... && go test ./cmd/rastrillo/` — verify PASS, including every pre-existing generate test.
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green.
- [ ] Commit:

```
generate -check: fold the vectors gate in when cmd/genvectors exists

generate --check is already the one pre-ship gate — locale catalogs,
action tags, dry-run manifests, consent sentences, icon slugs — so
the vectors check joins it rather than becoming a second thing to
remember. Opt-in stays honest: no cmd/genvectors under the resolved
app root, no gate, and every existing app's --check is byte-for-byte
what it was.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 7: end-to-end — the whole story on a fixture app

**Files**
- Test: `cmd/rastrillo/vectors_e2e_test.go`

**Interfaces**
- Consumes: `runVectors`, `scaffold(t, files)`, `repoRoot(t)`, the `replace amadan.net/rastrillo/rastrillo => <checkout>` + `GOFLAGS=-mod=mod` dance (new_test.go / generate_test.go pattern), `go mod tidy` with the sqlc-shaped network skip.
- Produces: `TestVectorsEndToEndOnAFixtureApp` — spec §3's verb test: `-init` → `vectors` → `-check` green; Go fold mutated → byte-compare fails; JS fold mutated → node leg fails.

**Steps**

- [ ] Write the test file `cmd/rastrillo/vectors_e2e_test.go` (it fails until run against the complete implementation — with Tasks 3–5 landed it should pass on first run; if it does not, the failure is a real integration bug to fix before committing):

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVectorsEndToEndOnAFixtureApp drives the whole story the way an
// app would live it: -init into a bare module, regenerate, gate
// green, then each engine mutated in turn and the gate failing the
// correct leg. The fixture resolves rastrillo to THIS checkout via a
// replace directive (the new_test.go dance), so the scaffolded
// generator compiles against the code under test, never a published
// version.
func TestVectorsEndToEndOnAFixtureApp(t *testing.T) {
	goMod := fmt.Sprintf("module fixtureapp\n\ngo 1.25.0\n\n"+
		"require amadan.net/rastrillo/rastrillo v0.0.0\n\n"+
		"replace amadan.net/rastrillo/rastrillo => %s\n", repoRoot(t))
	dir := scaffold(t, map[string]string{"go.mod": goMod})
	t.Setenv("GOFLAGS", "-mod=mod")

	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}

	// Resolve the fixture's dependency graph (rastrillo's own,
	// through the replace). A cold module cache needs the network —
	// the sqlc-shaped skip, not a failure.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Skipf("go mod tidy in the fixture failed (likely a network issue): %v\n%s", err, out)
	}

	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("vectors: %v", err)
	}
	vectorsJSON, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		t.Fatalf("expected test/vectors.json: %v", err)
	}
	for _, want := range []string{`"name": "empty-log"`, `"why"`, `"events": []`} {
		if !strings.Contains(string(vectorsJSON), want) {
			t.Errorf("test/vectors.json is missing %s:\n%s", want, vectorsJSON)
		}
	}

	_, nodeErr := exec.LookPath("node")
	if nodeErr == nil {
		if err := runVectors([]string{"-check", dir}); err != nil {
			t.Fatalf("-check on a fresh -init should be green: %v", err)
		}
	}

	// Mutate the Go fold: the byte-compare leg must catch the engine
	// changing without a regenerate. This leg needs no node — leg 1
	// fails before leg 2 runs.
	genPath := filepath.Join(dir, "cmd", "genvectors", "main.go")
	genSrc, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(genSrc), "t.Count++", "t.Count += 2", 1)
	if mutated == string(genSrc) {
		t.Fatal("mutation found nothing to change; the template's fold moved")
	}
	if err := os.WriteFile(genPath, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runVectors([]string{"-check", dir})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a changed Go engine must fail the byte-compare leg, got: %v", err)
	}

	if nodeErr != nil {
		t.Log("node not on PATH; the JS-mutation leg is not exercised")
		return
	}

	// Restore the Go engine, regenerate, then mutate the JS fold:
	// the node leg must catch the JS engine drifting.
	if err := os.WriteFile(genPath, genSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("regenerate after restore: %v", err)
	}
	parityPath := filepath.Join(dir, "test", "parity.test.mjs")
	paritySrc, err := os.ReadFile(parityPath)
	if err != nil {
		t.Fatal(err)
	}
	jsMutated := strings.Replace(string(paritySrc), "t.count += 1", "t.count += 2", 1)
	if jsMutated == string(paritySrc) {
		t.Fatal("JS mutation found nothing to change; the template's fold moved")
	}
	if err := os.WriteFile(parityPath, []byte(jsMutated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runVectors([]string{"-check", dir}); err == nil {
		t.Fatal("a changed JS engine must fail the node leg")
	}
}
```

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./cmd/rastrillo/ -run TestVectorsEndToEndOnAFixtureApp -v` — verify PASS (or an honest network skip on a cold cache; rerun once warmed). If it fails, debug the integration — the templates, the treaty key names and the mutation anchors (`t.Count++`, `t.count += 1`) must all line up.
- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && gofmt -l cmd/ && go vet ./cmd/... && go test ./...` — green.
- [ ] Commit:

```
vectors: prove the whole story end to end on a fixture app

-init into a bare module wired to this checkout by a replace
directive, regenerate, gate green — then each engine broken in turn:
a mutated Go fold fails the byte-compare leg with no node involved,
and a mutated JS fold fails the node leg after a clean regenerate.
The node-dependent legs skip without node on PATH; the network-cold
go mod tidy skips the sqlc way rather than failing a machine that
cannot fetch.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

### Task 8: README — the parity-vectors story

**Files**
- Modify: `README.md`

**Interfaces**
- Consumes: the shipped behaviour of Tasks 1–7.
- Produces: a `## Parity vectors` section between `## Browser tests` and `## Try it`.

**Steps**

- [ ] Insert the following section into `README.md`, after the end of the `## Browser tests` section (the paragraph ending "…should not raise the module's Go floor for everyone who imports rastrillo.") and before `## Try it`:

````markdown
## Parity vectors

Any derivation over sealed content runs client-side, but the sidecar,
operator tools and tests want the same derivation in Go — so the
engine exists twice, and two engines drifting is the most dangerous
E2EE bug class: a wrong answer with nothing looking broken.
`rastrillo vectors` promotes kass's golden-vector discipline to a
verb:

```
rastrillo vectors -init    # once: scaffold cmd/genvectors, test/parity.test.mjs,
                           # test/vectors.mjs and the go-test belt
rastrillo vectors          # regenerate test/vectors.json from the app's Go engine
rastrillo vectors -check   # pre-ship gate: regenerate + byte-compare, then
                           # node --test test/parity.test.mjs
```

The app's `cmd/genvectors` enumerates cases through `vectors.New()` /
`Add(name, why, fields)` / `WriteTo` — every vector names the rule it
pins, and the JS test titles become `name — why`. Two treaty rules
ride the file: the field key names are shared with the JS suite by
name (change both sides in the same commit; nothing mechanical checks
they agree), and `time.Time` round-trips as RFC 3339 →
`new Date(v.now)`.

The comparison rule (`canonical()` in the vendored `test/vectors.mjs`)
sorts keys, drops `null`/`undefined` members and drops scalar zeros
(`0`, `false`, `""`) to match Go's `omitempty` — deliberately, blind
spot included: a meaningful explicit zero on one side and a missing
field on the other compare equal. The scaffolded suite covers that
hole with a marked belt section of explicit-value assertions; keep it
fed as vectors accrue. Nil normalisation is top-level only: a nil
slice or map directly in `fields` writes as `[]`/`{}`, while inner
shapes stay the app's own discipline.

Vectors are opt-in: no `cmd/genvectors`, no gate. When the generator
exists, `rastrillo generate -check` runs the vectors check too — one
gate before ship, not two to remember. In `-check` a missing `node`
is a failure, not a skip; the scaffolded `go test` belt skips without
node so ordinary builds stay green and honest. There is deliberately
no hash-pin export: app vectors change by design, and the
byte-compare catches regenerate-drift strictly better than a hash an
author would just update.
````

  (The four-backtick fence is this plan's own quoting device; the README itself gets exactly the content between the fences, with its plain three-backtick code block like every other section.)

- [ ] Run `export GOFLAGS=-mod=mod CGO_ENABLED=0 && go test ./...` — green (README has no test, but the SKILL.md budget test and the rest of the tree must still pass untouched).
- [ ] Commit:

```
README: the parity-vectors story

The verb, the treaty (key names by hand, times as RFC 3339), the
comparison rule with its documented blind spot and the belt that
covers it, and the postures that make the gate honest: opt-in by
convention, folded into generate -check, node required at the gate
and skippable in the belt, and no hash-pin on purpose.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Self-review checklist result

**Spec coverage** — every numbered spec section lands in a task:
- §1.1 `Set`/`New`/`Add`/`WriteTo`, 2-space indent, top-level-only nil normalisation, key-name + RFC 3339 treaty in docs, no hash-pin export → Task 1 (package doc says both treaty rules and the no-hash-pin rationale; templates repeat them in Task 5).
- §1.2 `vectors/js.go` embed + `JS()`, `vectors.mjs` naming rationale, kass's real `canonical()` with zero-strip, blind spot documented, empty arrays kept → Task 2.
- §1.3 verb: one dispatch case, dir absolutised like generate's, `go run ./cmd/genvectors` with `cmd.Dir` = module root, guidance error, root `test/` convention → Task 3; `-check` two legs, explicit file, missing node = failure → Task 4; `-init` five files including belt + separate vendored pin → Task 5.
- §1.4 `generate -check` hook, opt-in by `cmd/genvectors` presence → Task 6.
- §1.5 Go belt with `cmd.Dir = "../.."` and skip-without-node; separate `vectors_vendored_test.go` pinning `../../test/vectors.mjs` (the existing map can't hold non-static paths — said in the template's doc comment) → Task 5.
- §2 honoured by omission: no fixture framework, no codegen, no mandatory gate.
- §3 testing list: Set ordering / nil-normalisation / determinism (Task 1), helper golden-content + zero-strip cases (Task 2), fixture-app `-init` → `vectors` → `-check` green, Go-fold mutation fails byte-compare, JS-fold mutation fails from node, node legs skip gracefully (Task 7).

**Placeholder scan** — no step says "similar to Task N"; every code step carries the complete file or the complete inserted block. The only intentionally deferred bodies are the two stubs in Task 3 (`vectorsCheck`/`vectorsInit` returning explicit "not implemented yet" errors), each replaced in its own task; the plan text says so at the point of writing them.

**Type consistency** — `runVectors(args []string) error` matches main.go's dispatch shape (`err = runVectors(os.Args[2:])`); `vectorsCheck(dir string) error` is called identically from Task 4's verb path and Task 6's generate hook; `vectors.JS() []byte` matches both the Task 5 scaffold write (`string(vectors.JS())`) and the emitted pin test (`bytes.Equal(vendored, vectors.JS())`); the generator template calls `set.WriteTo(os.Stdout)` discarding the `int64`, consistent with the `(int64, error)` signature; the emitted templates' treaty key names (`events`, `now`, `tally`; `count`, `quiet`) match between `genvectorsTemplate`, `parityTestTemplate`, and Task 7's assertions and mutation anchors (`t.Count++` in Go, `t.count += 1` in JS). One deliberate signature deviation (`WriteTo` returning `(int64, error)` for vet's stdmethods) is declared in Global Constraints and in the code's own doc comment.
