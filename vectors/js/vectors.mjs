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
