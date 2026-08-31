//go:build browser

// The accessibility gate.
//
// "And everything is WCAG 2.2 AA, right?" — this file is the half of
// that answer a machine can give. It serves what Render produces in a
// real Chromium — the exact bytes dsgen would publish, not a copy of
// them — injects the vendored axe-core, and runs it over a
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

	"amadan.net/rastrillo/rastrillo/harness"
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
//
// Keyed by the focused element's tag name, and looked up as a map: an
// element of any other kind with no ring is a failure, and adding a
// second kind here is a ruling somebody has to write down.
var focusRingExempt = map[string]string{
	"iframe": "Chromium makes a scrollable frame a tab stop of its own, and it paints " +
		"nothing on it. No author CSS can, either: probed on this tree, an iframe with " +
		"focus inside it matches neither :focus nor :focus-visible from the embedding " +
		"document, and its box matches neither :focus-within nor :has(> iframe:focus). " +
		"A ring here would have to come from the user agent. Every stop INSIDE the frame " +
		"— the sample's own links, inputs and buttons — is walked and held to 2.4.7 " +
		"normally, because the walk follows focus into the frame's document.",
}

// axeFloor is the least number of axe rules a real scan runs to
// completion. See the comment at its use in scan() for the measured
// numbers it sits under.
const axeFloor = 5

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
	// document. The count of rules that ran to completion is the proof
	// it did.
	//
	// The floor is five rather than one because one is not a floor:
	// axe scoped to a single element still clears it (measured: one to
	// two rules pass on a lone button), so a context that had collapsed
	// to one node would look like a scan. What the real scans measure,
	// on this tree: 30 rules on an index page, 15 on the modal, 13 on
	// the sidebar shell, and 6 on the smallest preview document — the
	// status-pill sample, which is a span of text and nothing else.
	// Five is under the smallest real one and over the degenerate ones.
	if out.Passed < axeFloor {
		t.Errorf("%s: only %d axe rules ran to completion, under the floor of %d — the scan reached almost no content",
			where, out.Passed, axeFloor)
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
	page := func(theme, locale, kind string) string { return pageHref(mountPath, theme, locale, fileOf(kind)) }
	return []a11yTarget{
		// Every one of the default theme's pages in English. They share
		// a stylesheet and a rail, so all but the first are cheap; each
		// one carries markup the others do not, and a heading level, a
		// landmark or a duplicate id can only be wrong on the page it
		// is on.
		{"day/en overview", page("day", "en", "overview"), "the root page: the chrome, the rail and the page header with nothing else in the way"},
		{"day/en getting started", page("day", "en", "getting-started"), "a page of prose, a list of links out to the assets and two source blocks: the plainest document in the tree, and the one where a heading level or a list semantic has nothing else to hide behind"},
		{"day/en tokens", page("day", "en", "tokens"), "the swatch grid — every palette token painted at once, in the theme's own colours"},
		{"day/en icons", page("day", "en", "icons"), "one inline SVG per slug the framework answers, each aria-hidden beside its own name: the one page in the tree where an icon is the content rather than furniture, and where an accessible name accidentally coming off a decorative glyph would show"},
		// The five component pages. Since the split they are one
		// template with a different family in it, so the furniture this
		// scan can see — the tab fieldsets, the state headings, the
		// escaped source under each frame — is the same shape on all
		// five and what differs is how much of it there is and which
		// ids it carries. Each is named for the thing it has that its
		// neighbours do not.
		{"day/en list screen", page("day", "en", "list-screen"), "sixteen preview widgets, with the escaped source of a toolbar, a search form and a pagination strip beside them"},
		{"day/en display", page("day", "en", "display"), "thirty widgets over nine partials — five states each of the pill, the badge and the meter — which is a great many radio groups and generated ids in one document"},
		{"day/en form", page("day", "en", "form"), "the field partials, whose source blocks carry the labels, hints and error messages a reader copies out"},
		{"day/en date and time", page("day", "en", "date-and-time"), "the heaviest page in the tree and the one nearest the byte budget: whatever is added to the gallery next is most likely to be added here"},
		{"day/en route", page("day", "en", "route"), "the shortest of the five, and the only one whose samples are whole responses rather than pieces of one"},
		{"day/en primitives", page("day", "en", "primitives"), "the markup idioms, the callouts they carry, and the sample whose structure is a dialog"},
		{"day/en shells", page("day", "en", "shells"), "the four page frames, each framed at full page size"},
		// The two colour ends, on the two pages that carry colour: the
		// palette itself and the display vocabulary painted in it.
		{"plain/en tokens", page("plain", "en", "tokens"), "the theme with the least colour — where a contrast floor is closest to the line"},
		{"plain/en display", page("plain", "en", "display"), "the same theme in the page that carries the most tinted markup of its own"},
		{"signal/en tokens", page("signal", "en", "tokens"), "the theme with the most colour — the other end of the same risk"},
		{"signal/en display", page("signal", "en", "display"), "and the same page at the other end of the same risk"},
		{"day/ar form", page("day", "ar", "form"), "RTL: dir=rtl reverses every logical property, and a landmark or a label lost in the mirror is invisible in en"},
		{"day/en modal", modalHref(mountPath, "day", "en"), "the one page in the tree with no JavaScript at all, and the one whose structure is a dialog"},
		{"day/en sidebar shell", shellHref(mountPath, "day", "en", "sidebar"), "the richest shell: a skip link, a rail, a disclosure and a main column"},
		{"day/en console shell", shellHref(mountPath, "day", "en", "console"), "the only page in the tree with two chromes at once — a banner bar and a complementary rail, both landmarks, in one document with one <main> and one contentinfo. A shell that is two other shells is exactly where a duplicated landmark, a second control with the same name, or a nav with nothing to tell it from the bar would come from, and none of the three shows on a shell that has only one of them"},
		{"day/en demo app", demoHref(mountPath, "day", "en"), "the demo application: three screens in one document, a form, a data grid and a rail — the page a first-time reader meets before any of the vocabulary"},
		{"day/ar demo app", demoHref(mountPath, "day", "ar"), "the demo application mirrored: its rail, its grid columns and its back link all flip, and a label lost in the mirror is invisible in en"},
	}
}

// TestEveryPageKindHasAnAccessibilityTarget holds the curated list to
// the table.
//
// a11yTargets() is a REPRESENTATIVE sample with a written reason per
// entry, and that is worth keeping: scanning 253 pages would say
// nothing the twelve do not, and the reasons are what make the sample
// arguable. What it must not be is a list that quietly stops covering
// something.
//
// A page kind added to pageKinds() whose author forgets an entry here
// is never axe-scanned at all, and the only evidence is that a log line
// says "16 pages" where it used to say 14 — a number nobody reads.
// TestA11yReflowsAt320 already walks pageKinds(); this is the same
// assurance for the scan, without giving up the curation: the entry
// still has to be written by hand, with its own reason, and this says
// so when it has not been.
//
// The check is on the FILE, not on a theme or a locale, because which
// theme and language a page kind is best scanned in is exactly the
// judgement the list exists to record.
func TestEveryPageKindHasAnAccessibilityTarget(t *testing.T) {
	targets := a11yTargets()
	if len(targets) == 0 {
		t.Fatal("a11yTargets() is empty; the scan is scanning nothing")
	}
	for _, pk := range pageKinds() {
		found := ""
		for _, tc := range targets {
			if strings.Contains(tc.href, "/"+pk.File) {
				found = tc.name
				break
			}
		}
		if found == "" {
			t.Errorf("no axe target covers %s (page kind %q). Every page kind is scanned: add an entry to a11yTargets() "+
				"naming the theme and locale you chose and the reason that page is worth a scan of its own.", pk.File, pk.Kind)
		}
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
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 1200*time.Second)
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

// shownIn reports whether the first element matching sel has a box on
// screen. It is the one reading the patency check and its control both
// use, so the two are the same instrument pointed at two moments rather
// than two instruments that agree until one of them is edited.
func shownIn(t *testing.T, bctx context.Context, where, sel string) bool {
	t.Helper()
	var shown bool
	js := `(() => {
	  const el = document.querySelector(` + "`" + sel + "`" + `);
	  if (!el) return false;
	  const r = el.getBoundingClientRect();
	  return r.width > 0 && r.height > 0;
	})()`
	if err := chromedp.Run(bctx, chromedp.Evaluate(js, &shown)); err != nil {
		t.Fatalf("%s: reading whether %q is on screen: %v", where, sel, err)
	}
	return shown
}

// TestA11yScansTheShellsCollapsed is the scan the others did not
// cover: every chrome shell below its 800px breakpoint, with the
// disclosure open.
//
// Every scan above runs at the browser's default width, where the
// sidebar's chrome strip is display: none and the topbar's collapse
// control does not exist at all. So the two controls a reader on a
// phone meets FIRST were the two controls axe had never seen — which is
// the same gap, in a different shape, as a rail whose height was only
// ever compared to itself: the shells are demonstrated as chrome, and
// nobody drove them narrow.
//
// It also measures the one thing axe will not: WCAG 2.2 target size.
// 2.5.8 asks for 24×24 CSS px, axe's target-size rule is not in the
// tag set this gate runs, and both summaries are new or newly
// icon-bearing, so measure them here rather than assume.
func TestA11yScansTheShellsCollapsed(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()

	axeJS := axeSource(t)
	total := 0
	for _, sh := range []struct {
		shell string
		open  string
		// revealed is what the click has to bring on screen. Nothing
		// here asserted it before: Click, then axe, and a summary that
		// had stopped opening — a gallery-side regression, a stray
		// pointer-events, an overlay — would have been scanned CLOSED
		// and reported clean, which is the shape of every gate this
		// project has shipped that gated nothing. The scan is of the
		// disclosed document or it is not the scan this test claims.
		revealed []string
	}{
		{"topbar", "[rst-shell-menu] > summary", []string{"[rst-shell-tail] [rst-shell-nav] a"}},
		{"sidebar", "[rst-shell-chrome] > summary", []string{"[rst-shell-rail] [rst-shell-nav] a"}},
		// console discloses TWO chromes from this one summary — the
		// bar's tail and the rail — so the collapsed document it
		// produces is the largest of the three and the only one where
		// a landmark revealed by a disclosure sits beside another
		// landmark revealed by the same one. Both are named here: half
		// a reveal is exactly the regression the ui drives gate on
		// their own fixture, and this is the gallery's own page.
		{"console", "[rst-shell-menu] > summary", []string{
			"[rst-shell-tail] [rst-shell-account] > summary",
			"[rst-shell-rail] [rst-shell-nav] a",
		}},
	} {
		for _, scheme := range a11ySchemes {
			where := "day/en " + sh.shell + " shell at 390px, disclosed (" + scheme + ")"
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(390, 780),
				chromedp.Navigate(rig.Origin+shellHref(mountPath, "day", "en", sh.shell)),
				chromedp.WaitVisible(sh.open, chromedp.ByQuery),
			); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			// The CONTROL, and it costs one evaluate: the same reading,
			// on the same page, one click earlier, where the answer is
			// already known. A patency check hard-wired true — a
			// selector that matches something the disclosure does not
			// gate, a shown() that cannot return false — passes the
			// assertion below and fails here.
			for _, sel := range sh.revealed {
				if shownIn(t, ctx, where, sel) {
					t.Errorf("%s: %q is on screen with the disclosure still CLOSED, so it is not gated by the disclosure and its visibility proves nothing about the click", where, sel)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Click(sh.open, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: opening: %v", where, err)
			}
			// Patency. Before axe, because a scan of the closed
			// document reported as a scan of the open one is worse than
			// no scan: it is a gate saying it looked.
			for _, sel := range sh.revealed {
				if !shownIn(t, ctx, where, sel) {
					t.Fatalf("%s: clicking the disclosure did not bring %q on screen. axe would have scanned the COLLAPSED document and reported it as the disclosed one", where, sel)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
				t.Fatalf("%s: loading axe: %v", where, err)
			}
			paint(t, ctx, scheme)
			total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))

			// The target the reader taps. Measured on the summary
			// itself, which is the control: a 24px floor on both axes,
			// and the accessible name the icon must not have replaced.
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			  const s = document.querySelector(`+"`"+sh.open+"`"+`);
			  const r = s.getBoundingClientRect();
			  return JSON.stringify({W: Math.round(r.width), H: Math.round(r.height), Text: s.innerText.trim()});
			})()`, &raw)); err != nil {
				t.Fatalf("%s: measuring the summary: %v", where, err)
			}
			var got struct {
				W, H int
				Text string
			}
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("%s: reading the measurement (%q): %v", where, raw, err)
			}
			if got.W < 24 || got.H < 24 {
				t.Errorf("%s: the disclosure's target is %d×%dpx, under WCAG 2.2 SC 2.5.8's 24×24", where, got.W, got.H)
			}
			if got.Text == "" {
				t.Errorf("%s: the disclosure has no visible label; the icon beside it is aria-hidden, so the control would have no accessible name at all", where)
			}
		}
	}
	if total == 0 {
		t.Logf("clean: 3 shells × %d schemes at 390px, disclosed, %v", len(a11ySchemes), axeTags)
	}
}

// previewFrame is one preview widget picked for scanning, and the
// section it belongs to.
type previewFrame struct{ Sel, Of string }

// pickPreviewFrames chooses the frames to scan on the page the browser
// is on, and says what it chose.
//
// It is a REPRESENTATIVE sample with a written reason: one frame per
// component page — which is one per family, because a family is a page
// — plus the first four idioms. Scanning all hundred and ten in six
// theme-scheme combinations would say very little the sample does not
// and cost hours.
//
// The family half is the part that must not rot: a family is the axis
// the samples are grouped on, so one frame per family is the smallest
// sample that touches every kind of component, and a family added
// tomorrow is a page added tomorrow and is scanned tomorrow — the
// caller reads its list off componentPages().
//
// A component page also owes a frame for EVERY partial on it, not only
// for the one that gets scanned: a partial documented with nothing to
// look at reads exactly like a section that was never rendered, and
// this is where the two are told apart.
func pickPreviewFrames(t *testing.T, bctx context.Context, kind string) []previewFrame {
	t.Helper()
	const pick = `(() => {
	  const out = [];
	  const articles = document.querySelectorAll("article.ds-partial");
	  articles.forEach(a => {
	    const f = a.querySelector("iframe.ds-view__frame");
	    if (!f) return;
	    f.id = f.id || ("a11y-" + a.id);
	    out.push({Sel: "#" + f.id, Of: a.id});
	  });
	  return JSON.stringify({frames: out, articles: articles.length});
	})()`
	var raw string
	if err := chromedp.Run(bctx, chromedp.Evaluate(pick, &raw)); err != nil {
		t.Fatalf("picking preview frames: %v", err)
	}
	var got struct {
		Frames   []previewFrame
		Articles int
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding the frame list: %v", err)
	}
	if kind == "primitives" {
		if len(got.Frames) < 4 {
			t.Fatalf("expected four idiom previews, picked %d of %d idiom articles", len(got.Frames), got.Articles)
		}
		return got.Frames[:4]
	}
	// A component page. Every partial on it gave up a frame, and the
	// first of them is the one that gets scanned.
	if got.Articles == 0 {
		t.Fatalf("no partial sections at all on the %s page", kind)
	}
	if len(got.Frames) != got.Articles {
		t.Fatalf("%d partial sections on the %s page but only %d gave up a preview frame", got.Articles, kind, len(got.Frames))
	}
	return got.Frames[:1]
}

// previewPageKinds is every page kind that frames a sample worth
// scanning: the component pages, read off componentPages() so a family
// added to samples.go is scanned the day its row lands, and the
// primitives page, which is where the markup idioms are.
func previewPageKinds() []string {
	out := make([]string, 0, len(families())+1)
	for _, pk := range componentPages() {
		out = append(out, pk.Kind)
	}
	return append(out, "primitives")
}

// TestA11yScansThePreviewDocuments scans inside the frames.
//
// This is where the components actually live. Every example on a
// gallery page is an <iframe srcdoc> holding a whole document — the partial,
// the tree's stylesheets, and nothing else — so a scan of the index
// with iframes:false has not looked at a single component. It has
// looked at the gallery's furniture.
//
// Scanned in the frame, not extracted and re-served: a component's
// accessible name and its contrast are properties of the document it is
// in, and lifting the markup out to a page of our own would be scanning
// something the reader never sees.
//
// The axis is the point, and it is the same axis the page scan uses:
// three themes × two schemes, plus an RTL page. A component's contrast
// is a property of the palette it is painted in, so "the field sample
// is clean" is a claim about one theme in one scheme until it has been
// scanned in the others; and dir=rtl reverses every logical property a
// component lays itself out with. Frames are loading="lazy", so each is
// scrolled into view and waited for.
func TestA11yScansThePreviewDocuments(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 900*time.Second)
	defer cancel()

	axeJS := axeSource(t)
	pages := []struct{ theme, locale string }{
		{"day", "en"}, {"plain", "en"}, {"signal", "en"}, {"day", "ar"},
	}

	total, scans := 0, 0
	for _, pg := range pages {
		for _, kind := range previewPageKinds() {
			if err := chromedp.Run(ctx,
				chromedp.Navigate(rig.Origin+pageHref(mountPath, pg.theme, pg.locale, fileOf(kind))),
				chromedp.WaitReady("body"),
			); err != nil {
				t.Fatalf("loading %s/%s %s: %v", pg.theme, pg.locale, kind, err)
			}
			frames := pickPreviewFrames(t, ctx, kind)

			// Every picked frame is taken off lazy loading in one mutation,
			// before any of them is waited for. Setting loading="eager" on
			// a frame the viewport has already reached restarts its load,
			// and a restart landing between "this document is ready" and
			// "scan it" is a scan of a half-built document. That is not a
			// hypothetical: the first version of this test reported four
			// different preview pages as having no <title>, four different
			// pages on each run, every one of which plainly had one. One
			// mutation, then wait for stability, then scan.
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`document.querySelectorAll("iframe.ds-view__frame[id^=a11y-]").forEach(f => { f.loading = "eager"; })`, nil)); err != nil {
				t.Fatalf("taking the preview frames off lazy loading: %v", err)
			}

			titles := make([]string, len(frames))
			for i, f := range frames {
				// Stable, not merely ready: the same document object twice,
				// 150ms apart, complete and populated both times. A frame
				// still being replaced fails the second look and the poll
				// goes round.
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
				titles[i] = state[3:]

				// The engine goes in here, into the document that just
				// settled, rather than as a boot script the browser replays
				// into every document a page creates. Two reasons, both
				// learned the hard way. A boot script would parse half a
				// megabyte of engine into all hundred-odd preview frames the
				// index holds, most of which are never scanned. And an
				// engine installed at document creation is bound to the
				// document that existed then: when a lazy frame reloaded
				// underneath it, the engine stayed attached to the empty
				// document it was born in and cheerfully reported the sample
				// as having no title, no headings and nothing to check.
				// Injecting after the document has settled binds the two
				// together by construction.
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
			}

			for _, scheme := range a11ySchemes {
				// paint reaches into every frame's own <html>, which is what
				// gallery.js does and the only way a frame gets the page's
				// scheme: a frame declares color-scheme of its own, so the
				// embedder's value is ignored.
				paint(t, ctx, scheme)
				for i, f := range frames {
					// The frame's OWN engine, on the frame's own document.
					// axe can reach into a frame from the embedder by
					// postMessage, but that path went quiet after the third
					// frame on a page holding a hundred of them and reported
					// the empty result as a clean one — which is precisely
					// the failure this gate exists to notice. Same-origin
					// srcdoc means contentWindow.axe is right there, so the
					// scan runs where the document is.
					where := fmt.Sprintf("%s/%s %s %s preview %s %s", pg.theme, pg.locale, kind, scheme, f.Of, titles[i])
					engine := fmt.Sprintf("document.querySelector(%q).contentWindow.axe", f.Sel)
					target := fmt.Sprintf("document.querySelector(%q).contentDocument", f.Sel)
					total += report(t, where, scan(t, ctx, where, engine, target, "false"))
					scans++
				}
			}
		}
	}
	if total == 0 {
		t.Logf("clean: %d preview scans over %d galleries × 2 pages × %d schemes, %v", scans, len(pages), len(a11ySchemes), axeTags)
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
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// 320×640 is the criterion's own number: 1280 CSS px at 400% zoom.
	// Every page kind, because reflow is a property of the content: the
	// token grid, the sample frames, the class-idiom callouts and the
	// shell sections each lay themselves out differently, and a split
	// that put them on separate pages put them in separate documents to
	// measure.
	pages := []struct{ name, href string }{}
	for _, pk := range pageKinds() {
		pages = append(pages, struct{ name, href string }{"day/en " + pk.Kind, pageHref(mountPath, "day", "en", pk.File)})
	}
	pages = append(pages,
		struct{ name, href string }{"day/ar form", pageHref(mountPath, "day", "ar", fileOf("form"))},
		struct{ name, href string }{"day/ar tokens", pageHref(mountPath, "day", "ar", fileOf("tokens"))},
		struct{ name, href string }{"day/en modal", modalHref(mountPath, "day", "en")},
		struct{ name, href string }{"day/en sidebar shell", shellHref(mountPath, "day", "en", "sidebar")},
	)
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
// Focus inside a frame: the form page is thirty <iframe
// srcdoc> documents (the Overview it split off from carries none), and
// once focus enters one, the top document's activeElement is the frame
// and stays the frame for every element inside it. Read naively that looks like focus refusing to move —
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
  // ── Is there a ring, really ───────────────────────────────────────
  //
  // A first version of this compared raw computed values and was
  // fooled twice, both times passing a page with no visible focus at
  // all:
  //
  //   - outline-offset counts for nothing on its own. Chromium reports
  //     an offset on an element whose outline is "none", and the value
  //     changes with focus, so "outline: none" showed a ring.
  //   - a ring you cannot see is not a ring. "outline-color:
  //     transparent" is a different colour string from the unfocused
  //     one, so a page where every indicator was invisible scored 29
  //     of 30.
  //
  // So every property is normalised to present-and-visible or absent
  // before anything is compared. Absent means: outline with style
  // none, zero width, or a fully transparent colour; box-shadow of
  // none, or one drawn entirely in transparent colours; a border with
  // no width or no visible colour; a transparent background or text
  // colour. Offset rides along inside the outline, where it is a real
  // difference, and contributes nothing when there is no outline.
  const clear = v => {
    v = (v || "").trim();
    if (v === "transparent") return true;
    const m = /^rgba?\(([^)]*)\)$/.exec(v);
    if (!m) return false;
    const parts = m[1].split(/[\s,\/]+/).filter(Boolean);
    return parts.length > 3 && parseFloat(parts[3]) === 0;
  };
  const anyVisible = v => (String(v).match(/rgba?\([^)]*\)/g) || []).some(c => !clear(c));
  const widest = v => Math.max(...String(v).split(/\s+/).map(parseFloat).map(n => isNaN(n) ? 0 : n), 0);
  const one = e => {
    if (!e) return "";
    const s = (e.ownerDocument.defaultView || window).getComputedStyle(e);
    const bits = [];
    if (s.outlineStyle !== "none" && parseFloat(s.outlineWidth) > 0 && !clear(s.outlineColor)) {
      bits.push("outline:" + s.outlineStyle + " " + s.outlineWidth + " " + s.outlineColor + " " + s.outlineOffset);
    }
    if (s.boxShadow && s.boxShadow !== "none" && anyVisible(s.boxShadow)) {
      bits.push("shadow:" + s.boxShadow);
    }
    if (widest(s.borderWidth) > 0 && anyVisible(s.borderColor)) {
      bits.push("border:" + s.borderWidth + " " + s.borderColor);
    }
    if (!clear(s.backgroundColor)) bits.push("bg:" + s.backgroundColor);
    if (!clear(s.color)) bits.push("fg:" + s.color);
    if (s.textDecorationLine && s.textDecorationLine !== "none") bits.push("dec:" + s.textDecorationLine);
    return bits.join(" ");
  };
  // The element and two ancestors, because a ring is not always painted
  // on the thing that has focus: a wrapper with :focus-within or :has()
  // is a normal way to do it, and this tree does it — a switch's track,
  // a choice card's label, a preview tab's box.
  const snap = e => [one(e), one(e.parentElement), one(e.parentElement && e.parentElement.parentElement)].join(" || ");
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
    kind: el.tagName.toLowerCase(),
    focused: focused,
    blurred: blurred,
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
// The walk: thirty Tabs with real key events — real, because
// :focus-visible is a heuristic about how the focus arrived, and a
// script calling .focus() out of nowhere does not always earn the ring.
// Every stop must show something (2.4.7) and must be a different
// element from the last (2.1.2, locally). Then the modal demo is walked
// all the way round, which is the trap question answered globally: a
// reader can always Tab their way back out.
//
// WHERE the thirty start is the whole design of this test, and it took
// a review to get right. From the top of the page they land on the skip
// link, the mobile chrome disclosure, the filter box and then
// sixty-odd rail links — one control repeated until the budget runs
// out, and nothing below the rail covered at all. Everything the split
// added lives below it: the section tab strip, which is the only
// visible way between the five pages once the shell folds the rail away
// below 800px, and then the page's own content.
//
// So the walk Tabs past the rail first, unasserted, and spends its
// thirty stops from the tab strip onwards. The seek is Tab presses too,
// not a .focus() call, so the thirty are still a keyboard journey and
// :focus-visible still means what it means. Where the rail's own stops
// are covered: they are the same anchors on every page, and the axe
// scan reads the rail on all twelve of its targets.
func TestA11yWalksTheKeyboard(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
	defer cancel()

	var count int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "en", fileOf("form"))),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.querySelectorAll(`+focusablesJS+`).length`, &count),
	); err != nil {
		t.Fatalf("preparing the keyboard walk: %v", err)
	}
	t.Logf("form: %d focusable elements in its own document, before any frame", count)

	// Tab past the rail. The stop this lands on is the first entry of
	// the section tab strip, which is the first thing after the rail in
	// the reading order — and the first stop the thirty below assert.
	// Bounded rather than counted: the rail's length is a property of
	// how many partials ui ships, and a seek that had to be updated
	// every time one landed would be a second list to maintain.
	const seekLimit = 250
	inStrip := `!!(document.activeElement && document.activeElement.closest(".ds-switch"))`
	var reached bool
	for i := 0; i < seekLimit && !reached; i++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(inStrip, &reached)); err != nil {
			t.Fatalf("seeking to the section tabs, tab %d: %v", i+1, err)
		}
	}
	if !reached {
		t.Fatalf("%d Tabs never reached the section tab strip; the walk would have covered the rail and nothing else", seekLimit)
	}

	const steps = 30
	rings := 0
	for i := 0; i < steps; i++ {
		// The first stop is where the seek left focus — the tab strip
		// itself — so the Tab comes after the reading, not before it.
		var raw string
		actions := []chromedp.Action{chromedp.Evaluate(walkJS, &raw)}
		if i > 0 {
			actions = []chromedp.Action{chromedp.KeyEvent(kb.Tab), chromedp.Evaluate(walkJS, &raw)}
		}
		if err := chromedp.Run(ctx, actions...); err != nil {
			t.Fatalf("tab %d: %v", i+1, err)
		}
		var s struct {
			Tag, Kind, Focused, Blurred            string
			Ring, Restored, Same, StillOn, UAFrame bool
		}
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("tab %d: decoding: %v", i+1, err)
		}
		if s.Tag == "body" {
			t.Fatalf("tab %d: focus left the document — the form page has fewer than %d stops after its rail?", i+1, steps)
		}
		if s.Same {
			t.Errorf("tab %d: focus did not move off %s — keyboard trap (WCAG 2.1.2)", i+1, s.Tag)
		}
		if !s.UAFrame && (!s.StillOn || !s.Restored) {
			t.Errorf("tab %d: %s — measuring the focus ring did not leave focus as it found it (stillOn=%v restored=%v); the reading below cannot be trusted",
				i+1, s.Tag, s.StillOn, s.Restored)
		}
		if !s.Ring {
			if reason, ok := focusRingExempt[s.Kind]; ok {
				t.Logf("EXEMPT tab %d: %s — %s", i+1, s.Tag, reason)
				continue
			}
			t.Errorf("tab %d: %s has no visible focus indicator (WCAG 2.4.7)\n    focused: %s\n    blurred: %s",
				i+1, s.Tag, s.Focused, s.Blurred)
			continue
		}
		rings++
	}
	t.Logf("form: %d of %d stops below the rail showed a focus indicator", rings, steps)

	// The full circuit, on the page small enough to walk all of.
	var modalCount int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+modalHref(mountPath, "day", "en")),
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
