package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/carlosframework/rastrillo/vectors"
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

	"github.com/carlosframework/rastrillo/eventlog"
	"github.com/carlosframework/rastrillo/vectors"
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

	"github.com/carlosframework/rastrillo/vectors"
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
