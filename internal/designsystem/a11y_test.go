//go:build browser

// The accessibility gate.
//
// "And everything is WCAG 2.2 AA, right?" — this file is the half of
// that answer a machine can give. It boots the committed tree in a real
// Chromium, injects the vendored axe-core, and runs it over a
// representative set of pages with the WCAG 2.2 AA tag set. Any
// violation fails the build, printing rule id, impact and the offending
// selector, so a regression names itself.
//
// Run it with:
//
//	go test -tags browser -run A11y ./internal/designsystem/
//
// Three honest limits, written down here rather than implied:
//
//   - Automated scanning catches roughly half of the WCAG success
//     criteria — the machine-checkable half. The rest (meaningful
//     alternative text, sensible reading order, whether a label says
//     something true) is a human's job. A green run here is a floor,
//     not a certificate.
//   - axe checks what is on the screen, so this file scans a sample:
//     three themes, two schemes, an RTL page, the modal, a shell, and
//     eight preview documents — one per family, plus the first four
//     idioms. 180 pages × 2 schemes in a browser is a CI budget nobody
//     would pay twice, and a sample chosen for what it would catch is
//     worth more than a sweep chosen for its size.
//   - Two criteria axe cannot see at all get their own drives below:
//     1.4.10 Reflow (a 320px viewport that does not scroll sideways)
//     and 2.4.7/2.1.2 (a keyboard walk that proves focus is visible at
//     every stop and that it can always move on).
package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/carlosframework/rastrillo/harness"
)

// axeTags is the ruleset, and it is not negotiable downwards. WCAG 2.2
// AA is cumulative: 2.2 AA means every A and AA criterion from 2.0, 2.1
// and 2.2, so all five tags have to be named or the older half of the
// standard silently stops being checked.
//
// Deliberately absent: best-practice, experimental and ACT tags. They
// are useful advice and they are not the standard the question asked
// about; mixing them in would make a failure here mean two different
// things.
var axeTags = []string{"wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"}

// axeExempt names the rules this tree does not fix, with the reason,
// one line each — the colorMixSkip convention from ui/contrast_test.go:
// a gate that cannot enforce something says so in a named table instead
// of quietly not enforcing it.
//
// The key is the axe rule id. Adding one is a ruling, not a workaround:
// it needs a reason a reviewer can disagree with, and it should be the
// smallest thing that can be exempted — never a tag, never the whole
// ruleset.
//
// Empty is the goal. Every entry here is a debt.
var axeExempt = map[string]string{
	// (none)
}

// focusRingExempt is the keyboard walk's version of axeExempt: the tab
// stops where "nothing visible changed" is the browser's doing and not
// the page's, named with the reason. Same convention as colorMixSkip in
// ui/contrast_test.go — the gate says out loud what it is not checking.
var focusRingExempt = map[string]string{
	"iframe": "Chromium makes a scrollable frame a tab stop of its own, and it paints " +
		"nothing on it. No author CSS can, either: probed on this tree, an iframe with " +
		"focus inside it matches neither :focus nor :focus-visible from the embedding " +
		"document, and its box matches neither :focus-within nor :has(> iframe:focus). " +
		"A ring here would have to come from the user agent. Every stop INSIDE the frame " +
		"— the sample's own links, inputs and buttons — is walked and held to 2.4.7 " +
		"normally, because the walk follows focus into the frame's document.",
}

// axeFile is the vendored engine, pinned and checksummed in
// ui/testdata/axe/README.md. Test data: it is never embedded, never
// served, and TestVendoredAxeStaysOutOfTheTree holds it to that.
const axeFile = "../../ui/testdata/axe/axe.min.js"

func axeSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(axeFile))
	if err != nil {
		t.Fatalf("reading the vendored axe-core: %v", err)
	}
	return string(b)
}

// mustJSON is a JavaScript string literal for s. Used to hand half a
// megabyte of engine source across the CDP boundary as data rather than
// as code, so nothing in it needs escaping by hand.
func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// committedTree serves docs/design-system — the bytes in the
// repository, not a fresh Render(). The freshness gate
// (TestDesignSystemIsCurrent) already proves the two are the same, so
// scanning the committed copy costs nothing and means something more:
// what CI publishes is what CI scanned.
func committedTree(t *testing.T) http.Handler {
	t.Helper()
	if _, err := os.Stat(treeDir); err != nil {
		t.Fatalf("the committed tree is missing: %v (run `go generate ./...`)", err)
	}
	return http.StripPrefix(mountPath, http.FileServer(http.Dir(treeDir)))
}

// ── The scan ─────────────────────────────────────────────────────────

// axeNode is one failing element: where it is, and why.
type axeNode struct {
	Impact  string `json:"impact"`
	Target  string `json:"target"`
	Summary string `json:"summary"`
}

// axeViolation is one rule that found something.
type axeViolation struct {
	ID     string    `json:"id"`
	Impact string    `json:"impact"`
	Help   string    `json:"help"`
	Nodes  []axeNode `json:"nodes"`
}

// axeRun is the expression that runs the engine and hands back a JSON
// string. A string rather than an object graph because chromedp has to
// serialise whatever comes back over CDP, and axe's result carries the
// whole checked DOM in it; stringifying the three fields a failure
// report needs keeps the payload small enough to be instant.
//
// resultTypes: ["violations"] tells axe to stop collecting node detail
// for passes and incompletes, which is most of the cost of a run — the
// rule ids still come back, and the count of them is what proves the
// engine looked at something. A scan that checks nothing passes too.
func axeRun(engine, target, iframes string) string {
	tags, _ := json.Marshal(axeTags)
	return fmt.Sprintf(`(async () => {
  const engine = %s;
  const target = %s;
  if (!engine) return JSON.stringify({error: "axe did not load"});
  const res = await engine.run(target, {
    runOnly: {type: "tag", values: %s},
    resultTypes: ["violations"],
    iframes: %s,
  });
  const doc = target.nodeType === 9 ? target : (target.ownerDocument || document);
  return JSON.stringify({
    doc: doc.title + " head=" + (doc.head ? doc.head.children.length : -1) + " ready=" + doc.readyState,
    passed: res.passes.length,
    incomplete: res.incomplete.map(i => i.id),
    violations: res.violations.map(v => ({
    id: v.id,
    impact: v.impact,
    help: v.help,
    nodes: v.nodes.map(n => ({
      impact: n.impact,
      target: n.target.map(t => Array.isArray(t) ? t.join(" >> ") : String(t)).join(" >> "),
      summary: (n.failureSummary || "").replace(/\s+/g, " ").trim(),
    })),
  }))});
})()`, engine, target, tags, iframes)
}

// scan runs axe in the page the browser is already on and returns what
// it found. ctx is an axe context expression (JavaScript source, not a
// selector string) and iframes says whether to descend into frames.
func scan(t *testing.T, bctx context.Context, where, engine, target, iframes string) []axeViolation {
	t.Helper()
	var raw string
	err := chromedp.Run(bctx, chromedp.Evaluate(axeRun(engine, target, iframes), &raw,
		func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
	if err != nil {
		t.Fatalf("axe.run: %v", err)
	}
	var out struct {
		Error      string         `json:"error"`
		Doc        string         `json:"doc"`
		Passed     int            `json:"passed"`
		Incomplete []string       `json:"incomplete"`
		Violations []axeViolation `json:"violations"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decoding axe results: %v (payload begins %.200q)", err, raw)
	}
	if out.Error != "" {
		t.Fatalf("axe: %s", out.Error)
	}
	// A scan that examined nothing reports no violations, which is the
	// one way this gate could go green while being switched off — a
	// selector that matches no frame, an engine that failed to reach a
	// document. The count of rules that ran is the proof it did.
	if out.Passed == 0 {
		t.Errorf("%s: axe ran no rules to completion — the scan reached no content", where)
	}
	t.Logf("%s: %d axe rules passed [%s]", where, out.Passed, out.Doc)
	sort.Slice(out.Violations, func(i, j int) bool { return out.Violations[i].ID < out.Violations[j].ID })
	if len(out.Incomplete) > 0 {
		sort.Strings(out.Incomplete)
		// Incomplete is axe saying "a human has to look at this" — a
		// contrast it could not compute, usually. Never a failure, and
		// never silent either: it is the seam between the scanned half
		// and the reviewed half.
		t.Logf("%s: needs review, axe could not decide: %s", where, strings.Join(out.Incomplete, ", "))
	}
	return out.Violations
}

// report turns one page's findings into test output: exempted rules are
// logged (so an exemption that stops being needed is visible), the rest
// fail. Every line carries the rule, the impact and a selector, because
// "the gallery has a violation" is not a bug report.
func report(t *testing.T, where string, vs []axeViolation) (failed int) {
	t.Helper()
	for _, v := range vs {
		if reason, ok := axeExempt[v.ID]; ok {
			t.Logf("EXEMPT %s: %s (%d node(s)) — %s", where, v.ID, len(v.Nodes), reason)
			continue
		}
		failed += len(v.Nodes)
		var b strings.Builder
		fmt.Fprintf(&b, "%s: %s [%s] — %s (%d node(s))", where, v.ID, v.Impact, v.Help, len(v.Nodes))
		for i, n := range v.Nodes {
			if i == 6 {
				fmt.Fprintf(&b, "\n    … and %d more", len(v.Nodes)-6)
				break
			}
			fmt.Fprintf(&b, "\n    %s", n.Target)
		}
		if len(v.Nodes) > 0 && v.Nodes[0].Summary != "" {
			fmt.Fprintf(&b, "\n    why: %s", v.Nodes[0].Summary)
		}
		t.Error(b.String())
	}
	return failed
}

// paint sets the colour scheme the way gallery.js does — data-theme on
// <html>, and on every same-origin frame's <html> — so a dark scan is
// measuring the dark palette rather than the browser's idea of the OS.
// "light" and "dark" are the two values the themes' toggle rules match;
// removing the attribute is "system", which is not a scan target
// because it is one of these two at run time.
const paintJS = `(scheme => {
  const set = doc => { if (scheme) doc.documentElement.setAttribute("data-theme", scheme); else doc.documentElement.removeAttribute("data-theme"); };
  set(document);
  document.querySelectorAll("iframe").forEach(f => { try { if (f.contentDocument) set(f.contentDocument); } catch (e) {} });
  return document.documentElement.getAttribute("data-theme") || "system";
})`

func paint(t *testing.T, bctx context.Context, scheme string) {
	t.Helper()
	var got string
	expr := fmt.Sprintf("%s(%q)", paintJS, scheme)
	if err := chromedp.Run(bctx, chromedp.Evaluate(expr, &got)); err != nil {
		t.Fatalf("painting %s: %v", scheme, err)
	}
	if got != scheme {
		t.Fatalf("painting %s: page reports %q", scheme, got)
	}
}

// a11yTarget is one page under scan, and why it is in the sample.
type a11yTarget struct {
	name string
	href string
	why  string
}

// a11yTargets is the representative set. The argument for each one is
// the "why": a sample is only representative if you can say what would
// be missed without each member.
func a11yTargets() []a11yTarget {
	return []a11yTarget{
		{"day/en index", indexHref("day", "en"), "the root page, the default theme, and the page with every component on it"},
		{"plain/en index", indexHref("plain", "en"), "the theme with the least colour — where a contrast floor is closest to the line"},
		{"signal/en index", indexHref("signal", "en"), "the theme with the most colour — the other end of the same risk"},
		{"day/ar index", indexHref("day", "ar"), "RTL: dir=rtl reverses every logical property, and a landmark or a label lost in the mirror is invisible in en"},
		{"day/en modal", modalHref("day", "en"), "the one page in the tree with no JavaScript at all, and the one whose structure is a dialog"},
		{"day/en sidebar shell", shellHref("day", "en", "sidebar"), "the richest shell: a skip link, a rail, a disclosure and a main column"},
	}
}

// a11ySchemes is both halves of every theme. Light and dark are
// authored by hand in the same file — never inverted — so they are two
// palettes, and a scan of one says nothing about the other.
var a11ySchemes = []string{"light", "dark"}

// TestA11yScansTheGallery is the gate.
//
// Bug classes it exists to catch, each of which leaves a page that
// looks perfectly fine to whoever wrote it:
//
//   - a landmark with no accessible name, so a screen-reader user
//     hears "navigation" three times and cannot tell them apart;
//   - a control whose only label is its icon, or whose label was
//     translated into a key that never resolved;
//   - a colour pair that passes in light and fails in dark, or passes
//     in day and fails in plain;
//   - a heading level skipped, so the document outline lies;
//   - an id used twice, which quietly breaks every aria-*="…" pointing
//     at it;
//   - anything the next partial adds that nobody thought to check.
func TestA11yScansTheGallery(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return committedTree(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()

	axeJS := axeSource(t)
	total := 0
	for _, tc := range a11yTargets() {
		t.Logf("scanning %s — %s", tc.name, tc.why)
		for _, scheme := range a11ySchemes {
			where := tc.name + " (" + scheme + ")"
			// Injected per page rather than as a boot script: the
			// index frames 110 preview documents, and an init script
			// would parse half a megabyte of engine into every one of
			// them. The frames get their own pass below.
			if err := chromedp.Run(ctx,
				chromedp.Navigate(rig.Origin+tc.href),
				chromedp.WaitReady("body"),
				chromedp.Evaluate(axeJS, nil),
			); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			paint(t, ctx, scheme)
			total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
		}
	}
	if total == 0 {
		t.Logf("clean: %d pages × %d schemes, %v", len(a11yTargets()), len(a11ySchemes), axeTags)
	}
}

// TestA11yScansThePreviewDocuments scans inside the frames.
//
// This is where the components actually live. Every example on an index
// page is an <iframe srcdoc> holding a whole document — the partial,
// the tree's stylesheets, and nothing else — so a scan of the index
// with iframes:false has not looked at a single component. It has
// looked at the gallery's furniture.
//
// Scanned in the frame, not extracted and re-served: a component's
// accessible name and its contrast are properties of the document it is
// in, and lifting the markup out to a page of our own would be scanning
// something the reader never sees. axe descends into a same-origin
// frame when it is loaded there too, which the boot script below
// arranges.
//
// One frame per family, plus the first idiom — the families are the
// axis the samples are grouped on, so one per family is the smallest
// sample that touches every kind of component. The frames are
// loading="lazy", so each is scrolled into view and waited for.
func TestA11yScansThePreviewDocuments(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return committedTree(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()

	axeJS := axeSource(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+indexHref("day", "en")),
		chromedp.WaitReady("body"),
	); err != nil {
		t.Fatalf("loading the index: %v", err)
	}

	// The frames to scan: the first preview in each family section, and
	// the first idiom. Selected in the page rather than hard-coded, so
	// a new family is scanned the day it is added.
	const pick = `(() => {
	  const out = [];
	  document.querySelectorAll("section.ds-family, article.ds-partial").forEach(sec => {
	    if (sec.matches("article.ds-partial") && sec.closest("section.ds-family")) return;
	    const f = sec.querySelector("iframe.ds-view__frame");
	    if (f) { f.id = f.id || ("a11y-" + sec.id); out.push({sel: "#" + f.id, of: sec.id}); }
	  });
	  return JSON.stringify(out.slice(0, 8));
	})()`
	var picked string
	if err := chromedp.Run(ctx, chromedp.Evaluate(pick, &picked)); err != nil {
		t.Fatalf("picking preview frames: %v", err)
	}
	var frames []struct{ Sel, Of string }
	if err := json.Unmarshal([]byte(picked), &frames); err != nil {
		t.Fatalf("decoding the frame list: %v", err)
	}
	if len(frames) < 4 {
		t.Fatalf("expected a preview frame per family, picked %d: %s", len(frames), picked)
	}

	// Every picked frame is taken off lazy loading in one mutation,
	// before any of them is waited for. Setting loading="eager" on a
	// frame the viewport has already reached restarts its load, and a
	// restart landing between "this document is ready" and "scan it" is
	// a scan of a half-built document. That is not a hypothetical: the
	// first version of this test reported four different preview pages
	// as having no <title>, four different pages on each run, every one
	// of which plainly had one. One mutation, then wait for stability,
	// then scan.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll("iframe.ds-view__frame[id^=a11y-]").forEach(f => { f.loading = "eager"; })`, nil)); err != nil {
		t.Fatalf("taking the preview frames off lazy loading: %v", err)
	}

	total := 0
	for _, f := range frames {
		// Stable, not merely ready: the same document object twice,
		// 150ms apart, complete and populated both times. A frame still
		// being replaced fails the second look and the poll goes round.
		ready := fmt.Sprintf(`(async () => {
		  const f = document.querySelector(%q);
		  f.scrollIntoView({block: "center"});
		  const settled = () => {
		    const d = f.contentDocument;
		    return d && d.readyState === "complete" && d.body && d.body.children.length ? d : null;
		  };
		  for (let i = 0; i < 200; i++) {
		    const d = settled();
		    if (d) {
		      await new Promise(r => setTimeout(r, 150));
		      if (settled() === d) return "ok " + JSON.stringify(d.title);
		    }
		    await new Promise(r => setTimeout(r, 50));
		  }
		  return "never settled";
		})()`, f.Sel)
		var state string
		if err := chromedp.Run(ctx, chromedp.Evaluate(ready, &state,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			t.Fatalf("%s: waiting for the frame: %v", f.Of, err)
		}
		if !strings.HasPrefix(state, "ok") {
			t.Fatalf("%s: preview frame %s %s", f.Of, f.Sel, state)
		}
		// The engine goes in here, into the document that just
		// settled, rather than as a boot script the browser replays
		// into every document a page creates. Two reasons, both
		// learned the hard way. A boot script would parse half a
		// megabyte of engine into all hundred-odd preview frames the
		// index holds, most of which are never scanned. And an engine
		// installed at document creation is bound to the document that
		// existed then: when a lazy frame reloaded underneath it, the
		// engine stayed attached to the empty document it was born in
		// and cheerfully reported the sample as having no title, no
		// headings and nothing to check. Injecting after the document
		// has settled binds the two together by construction.
		inject := fmt.Sprintf(`(() => {
		  const d = document.querySelector(%q).contentDocument;
		  const s = d.createElement("script");
		  s.textContent = %s;
		  d.head.appendChild(s);
		  return typeof d.defaultView.axe;
		})()`, f.Sel, mustJSON(axeJS))
		var loaded string
		if err := chromedp.Run(ctx, chromedp.Evaluate(inject, &loaded)); err != nil {
			t.Fatalf("%s: injecting axe into the frame: %v", f.Of, err)
		}
		if loaded != "object" {
			t.Fatalf("%s: axe did not load in the frame (typeof axe = %s)", f.Of, loaded)
		}
		// The frame's OWN engine, on the frame's own document. axe can
		// reach into a frame from the embedder by postMessage, but that
		// path went quiet after the third frame on a page holding a
		// hundred of them and reported the empty result as a clean one —
		// which is precisely the failure this gate exists to notice.
		// Same-origin srcdoc means contentWindow.axe is right there, so
		// the scan runs where the document is.
		where := "preview in " + f.Of + " " + state[3:]
		engine := fmt.Sprintf("document.querySelector(%q).contentWindow.axe", f.Sel)
		target := fmt.Sprintf("document.querySelector(%q).contentDocument", f.Sel)
		total += report(t, where, scan(t, ctx, where, engine, target, "false"))
	}
	if total == 0 {
		t.Logf("clean: %d preview documents, %v", len(frames), axeTags)
	}
}

// ── The two axe cannot do ────────────────────────────────────────────

// TestA11yReflowsAt320 is WCAG 2.2 AA 1.4.10 Reflow: content at a
// 320 CSS-pixel viewport must not need horizontal scrolling. axe cannot
// check this — it scans one viewport and has no opinion about another —
// and it is the criterion a design system breaks first, because a
// component that overflows only shows it on the narrowest phone.
//
// The gallery's own furniture is what is measured: a preview frame is a
// scroll container by design (a desktop-width sample inside a 320px
// column is meant to scroll inside its box), and a <pre> of source is
// another. Those are allowed to scroll; the page is not.
func TestA11yReflowsAt320(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return committedTree(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// 320×640 is the criterion's own number: 1280 CSS px at 400% zoom.
	pages := []struct{ name, href string }{
		{"day/en index", indexHref("day", "en")},
		{"day/ar index", indexHref("day", "ar")},
		{"day/en modal", modalHref("day", "en")},
		{"day/en sidebar shell", shellHref("day", "en", "sidebar")},
	}
	// Overflow is measured on both edges, because "sideways" is not
	// one direction: an LTR page spills past the right edge and an RTL
	// page spills past the left, and a measurement that only knows
	// about the right one reports the Arabic page as 330px wide with
	// nothing to blame.
	const measure = `(() => {
	  const de = document.documentElement;
	  const over = [];
	  document.querySelectorAll("body *").forEach(el => {
	    const r = el.getBoundingClientRect();
	    if (r.width === 0) return;
	    if (r.right > de.clientWidth + 1 || r.left < -1) {
	      const cls = (el.className || "").toString().trim().split(/\s+/)[0];
	      over.push(el.tagName.toLowerCase() + (cls ? "." + cls : "") + " [" + Math.round(r.left) + "…" + Math.round(r.right) + "]");
	    }
	  });
	  return JSON.stringify({doc: de.scrollWidth, client: de.clientWidth, body: document.body.scrollWidth, over: over.slice(0, 8)});
	})()`
	for _, p := range pages {
		if err := chromedp.Run(ctx,
			chromedp.ActionFunc(func(c context.Context) error {
				return emulation.SetDeviceMetricsOverride(320, 640, 1, true).Do(c)
			}),
			chromedp.Navigate(rig.Origin+p.href),
			chromedp.WaitReady("body"),
		); err != nil {
			t.Fatalf("%s at 320px: %v", p.name, err)
		}
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &raw)); err != nil {
			t.Fatalf("%s: measuring: %v", p.name, err)
		}
		var m struct {
			Doc, Client, Body int
			Over              []string
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("%s: decoding the measurement: %v", p.name, err)
		}
		if m.Doc > m.Client || m.Body > m.Client {
			t.Errorf("%s scrolls sideways at 320px (WCAG 1.4.10): document %dpx, body %dpx in a %dpx viewport\n    widest: %s",
				p.name, m.Doc, m.Body, m.Client, strings.Join(m.Over, "\n    "))
		}
	}
}

// focusablesJS is the selector the keyboard tests agree on: what a
// browser puts in the tab order on this tree. No [tabindex] hunt beyond
// the explicit ones, because the gallery writes none.
const focusablesJS = `'a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea:not([disabled]), summary, iframe, [tabindex]:not([tabindex="-1"])'`

// walkJS reports where focus actually is, and whether it shows.
//
// Two things it has to get right that a first attempt does not.
//
// Focus inside a frame: the index is a hundred and ten <iframe srcdoc>
// documents, and once focus enters one, the top document's
// activeElement is the frame and stays the frame for every element
// inside it. Read naively that looks like focus refusing to move —
// which is exactly what a keyboard trap looks like, and it is not one.
// So the probe descends: through the frame, into the document, to the
// element a person is actually on.
//
// Whether the ring shows: a focus indicator is a style that only exists
// while the element is focused, so it can only be measured against the
// same element unfocused. Blur it, measure, focus it again. The refocus
// is checked to have restored the same style — :focus-visible is a
// heuristic about how focus arrived, and the walk arrived by Tab, so it
// holds; if it ever stopped holding, the measurement would be lying and
// the test says so rather than passing quietly.
const walkJS = `(() => {
  let el = document.activeElement, doc = document, depth = 0;
  while (el && el.tagName === "IFRAME" && depth++ < 4) {
    let d = null;
    try { d = el.contentDocument; } catch (e) { d = null; }
    const inner = d && d.activeElement;
    if (!inner || inner === d.body || inner === d.documentElement) break;
    doc = d; el = inner;
  }
  if (!el || el === document.body || el === document.documentElement) return JSON.stringify({tag: "body"});
  const uaFrame = el.tagName === "IFRAME";
  const one = e => {
    if (!e) return "";
    const s = (e.ownerDocument.defaultView || window).getComputedStyle(e);
    return [s.outlineStyle, s.outlineWidth, s.outlineColor, s.outlineOffset,
            s.boxShadow, s.borderColor, s.borderWidth, s.backgroundColor,
            s.color, s.textDecorationLine].join("|");
  };
  // The element and two ancestors, because a ring is not always painted
  // on the thing that has focus: a wrapper with :focus-within or :has()
  // is a normal way to do it, and the gallery's preview boxes do
  // exactly that — the frame is scaled and clipped, so the outline goes
  // on the box around it.
  const snap = e => [one(e), one(e.parentElement), one(e.parentElement && e.parentElement.parentElement)].join("||");
  const focused = snap(el);
  el.blur();
  const blurred = snap(el);
  el.focus();
  const cls = (el.className && el.className.toString) ? el.className.toString().trim().split(/\s+/)[0] : "";
  const label = el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") + (cls ? "." + cls : "") +
    (doc !== document ? " (in " + JSON.stringify(doc.title) + ")" : "");
  const same = window.__prev === el;
  window.__prev = el;
  return JSON.stringify({
    tag: label,
    ring: focused !== blurred,
    restored: snap(el) === focused,
    same: same,
    uaFrame: uaFrame,
    stillOn: doc.activeElement === el,
  });
})()`

// TestA11yWalksTheKeyboard is 2.4.7 Focus Visible and 2.1.2 No Keyboard
// Trap, neither of which axe can see: a focus indicator is a computed
// style that only exists while an element is focused, and a trap is a
// property of a sequence rather than of a node.
//
// The walk: thirty Tabs through the index with real key events — real,
// because :focus-visible is a heuristic about how the focus arrived,
// and a script calling .focus() out of nowhere does not always earn the
// ring. Every stop must show something (2.4.7) and must be a different
// element from the last (2.1.2, locally). Then the modal demo is walked
// all the way round, which is the trap question answered globally: a
// reader can always Tab their way back out.
//
// Thirty stops on the index reaches past the gallery's own chrome and
// into the first preview documents, so the components' focus rings are
// walked too, not only the furniture around them.
func TestA11yWalksTheKeyboard(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return committedTree(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()

	var count int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+indexHref("day", "en")),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.querySelectorAll(`+focusablesJS+`).length`, &count),
	); err != nil {
		t.Fatalf("preparing the keyboard walk: %v", err)
	}
	t.Logf("index: %d focusable elements in its own document, before any frame", count)

	const steps = 30
	rings := 0
	for i := 0; i < steps; i++ {
		var raw string
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(walkJS, &raw)); err != nil {
			t.Fatalf("tab %d: %v", i+1, err)
		}
		var s struct {
			Tag                                    string
			Ring, Restored, Same, StillOn, UAFrame bool
		}
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("tab %d: decoding: %v", i+1, err)
		}
		if s.Tag == "body" {
			t.Fatalf("tab %d: focus left the document — the index has fewer than %d stops?", i+1, steps)
		}
		if s.Same {
			t.Errorf("tab %d: focus did not move off %s — keyboard trap (WCAG 2.1.2)", i+1, s.Tag)
		}
		if !s.UAFrame && (!s.StillOn || !s.Restored) {
			t.Errorf("tab %d: %s — measuring the focus ring did not leave focus as it found it (stillOn=%v restored=%v); the reading below cannot be trusted",
				i+1, s.Tag, s.StillOn, s.Restored)
		}
		if !s.Ring {
			if reason, ok := focusRingExempt["iframe"]; ok && s.UAFrame {
				t.Logf("EXEMPT tab %d: %s — %s", i+1, s.Tag, reason)
				continue
			}
			t.Errorf("tab %d: %s has no visible focus indicator — outline, shadow, border and colours all compute the same focused and unfocused (WCAG 2.4.7)", i+1, s.Tag)
			continue
		}
		rings++
	}
	t.Logf("index: %d of %d stops showed a focus indicator", rings, steps)

	// The full circuit, on the page small enough to walk all of.
	var modalCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+modalHref("day", "en")),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.querySelectorAll(`+focusablesJS+`).length`, &modalCount),
	); err != nil {
		t.Fatalf("loading the modal demo: %v", err)
	}
	if modalCount == 0 {
		t.Fatal("the modal demo has nothing focusable — Close is a link, so this is a rendering failure")
	}
	const firstOf = `(() => { const e = document.activeElement; return e && e !== document.body ? (e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") + "|" + (e.textContent || "").trim().slice(0, 40)) : "body"; })()`
	var first string
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(firstOf, &first)); err != nil {
		t.Fatalf("modal: first tab: %v", err)
	}
	// One extra tab past the count is the browser's own chrome, which
	// shows up as body; one more comes back to the first element. Both
	// are ways out, and either proves there is no trap.
	escaped := false
	for i := 0; i < modalCount+3 && !escaped; i++ {
		var here string
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(firstOf, &here)); err != nil {
			t.Fatalf("modal: tab %d: %v", i+2, err)
		}
		if here == "body" || here == first {
			escaped = true
		}
	}
	if !escaped {
		t.Errorf("modal demo: %d tabs never returned to %q — focus is trapped (WCAG 2.1.2)", modalCount+3, first)
	}
}
