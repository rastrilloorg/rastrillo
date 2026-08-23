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
