//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// clipboardStub replaces the clipboard with one the leg controls:
// every write is recorded, and window.__clipMode decides whether it
// resolves or rejects. A headless engine's own clipboard needs a
// permission the leg cannot grant per call, and would test the engine.
const clipboardStub = `window.__clipCalls = []; window.__clipMode = "resolve";
Object.defineProperty(Navigator.prototype, "clipboard", {configurable: true, get() { return {
  writeText: s => { window.__clipCalls.push(s); return window.__clipMode === "resolve" ? Promise.resolve() : Promise.reject(new DOMException("denied", "NotAllowedError")); }
}; }});`

// noClipboard is a plain-HTTP origin's clipboard: not there at all.
const noClipboard = `Object.defineProperty(Navigator.prototype, "clipboard", {configurable: true, get() { return undefined; }});`

func TestTheCopyButtonCopiesAnnouncesAndFailsSafely(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("form"))

	if err := chromedp.Run(ctx,
		addInit(clipboardStub),
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(form),
		chromedp.WaitReady(`.ds-copy`, chromedp.ByQuery),
		// Show the first widget's code, so the selection the failure
		// makes is a selection a reader could see.
		chromedp.Evaluate(`document.querySelector(".ds-view__tab--c input").click(); true`, nil),
	); err != nil {
		t.Fatalf("loading Form with a stubbed clipboard: %v", err)
	}

	// Every block a reader can copy has a button, and every button's
	// accessible name is unique on the page and begins with its label.
	assertNames(t, ctx, "form")

	// A resolved write: the clipboard gets exactly the block's text, the
	// region says Copied, and the label says it too.
	const first = `document.querySelector(".ds-view__code .ds-copy")`
	if err := chromedp.Run(ctx, chromedp.Evaluate(first+`.click(); true`, nil)); err != nil {
		t.Fatalf("clicking Copy: %v", err)
	}
	until(t, ctx, "a resolved copy", `document.querySelector("[data-ds-copy-status]").textContent === document.querySelector("[data-ds-copy-status]").dataset.copied`)
	var got struct {
		Calls []string
		Text  string
		Label string
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({Calls: window.__clipCalls,
	  Text: `+first+`.nextElementSibling.querySelector("code").textContent,
	  Label: `+first+`.firstChild.nodeValue})`, &raw)); err != nil {
		t.Fatalf("reading the copy: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if len(got.Calls) != 1 || got.Calls[0] != got.Text {
		t.Errorf("the clipboard got %q, want exactly the block's text %q", got.Calls, got.Text)
	}
	if !strings.HasPrefix(got.Text, "{{template ") {
		t.Errorf("the first block on Form copied %q; it should be the call", got.Text)
	}
	if got.Label != proseIn("en", "Copied") {
		t.Errorf("the button reads %q after a copy, want %q", got.Label, proseIn("en", "Copied"))
	}
	until(t, ctx, "the label returns", first+`.firstChild.nodeValue === document.querySelector("[data-ds-copy-status]").dataset.copy`)

	// A rejected write: the failure is announced and the block's text
	// is selected, so Ctrl or Cmd+C works at once. Nothing throws.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__clipMode = "reject"; window.__errors = 0;
	  addEventListener("error", () => window.__errors++); addEventListener("unhandledrejection", () => window.__errors++);
	  `+first+`.click(); true`, nil)); err != nil {
		t.Fatalf("clicking Copy with a refusing clipboard: %v", err)
	}
	until(t, ctx, "a refused copy", `document.querySelector("[data-ds-copy-status]").textContent === document.querySelector("[data-ds-copy-status]").dataset.failed`)
	until(t, ctx, "the block selected", `getSelection().toString() === `+first+`.nextElementSibling.querySelector("code").textContent && window.__errors === 0`)

	// Display has the most blocks that share a heading: names must still
	// be unique there.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("display"))),
		chromedp.WaitReady(`.ds-copy`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("loading Display: %v", err)
	}
	assertNames(t, ctx, "display")

	// No clipboard API, no button: a control that cannot work is not
	// drawn, and the <pre> stays selectable.
	tab, closeTab := chromedp.NewContext(rig.Context())
	defer closeTab()
	var buttons, blocks int
	if err := chromedp.Run(tab,
		addInit(noClipboard),
		chromedp.Navigate(form),
		chromedp.WaitReady(`[data-ds-copy-status]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll(".ds-copy").length`, &buttons),
		chromedp.Evaluate(`document.querySelectorAll("pre.ds-src").length`, &blocks),
	); err != nil {
		t.Fatalf("loading Form with no clipboard: %v", err)
	}
	if buttons != 0 || blocks == 0 {
		t.Errorf("with no clipboard API: %d buttons over %d blocks, want none over some", buttons, blocks)
	}
}

// assertNames holds one page's copy buttons to the two promises: one
// per copyable block, and names unique, each starting with the visible
// label so a voice user saying "Copy" reaches them (label in name).
func assertNames(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
	  Names: [...document.querySelectorAll(".ds-copy")].map(b => b.textContent),
	  Blocks: document.querySelectorAll("pre.ds-src:not([data-ds-nocopy])").length,
	  Label: document.querySelector("[data-ds-copy-status]").dataset.copy})`, &raw)); err != nil {
		t.Fatalf("%s: reading the copy buttons: %v", where, err)
	}
	var got struct {
		Names  []string
		Blocks int
		Label  string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	if len(got.Names) == 0 || len(got.Names) != got.Blocks {
		t.Fatalf("%s: %d copy buttons for %d copyable blocks", where, len(got.Names), got.Blocks)
	}
	seen := map[string]bool{}
	for _, n := range got.Names {
		if seen[n] {
			t.Errorf("%s: two copy buttons are both named %q", where, n)
		}
		seen[n] = true
		if !strings.HasPrefix(n, got.Label+" ") {
			t.Errorf("%s: %q does not start with the visible label %q", where, n, got.Label)
		}
	}
}

// viewState is the page-wide group's pressed button and every widget's
// checked radio: "d", "m", "c", or "" for none.
const viewState = `JSON.stringify({
  Pressed: [...document.querySelectorAll(".ds-viewall [aria-pressed=true]")].map(b => b.dataset.dsView),
  Widgets: [...document.querySelectorAll(".ds-view")].map(v => { const i = v.querySelector(".ds-view__tab input:checked"); return i ? i.closest(".ds-view__tab").className.slice(-1) : ""; }),
  Stored: (() => { try { return localStorage.getItem("rst-ds-view") || ""; } catch (e) { return "throws"; } })()})`

type viewReading struct {
	Pressed []string
	Widgets []string
	Stored  string
}

func readView(t *testing.T, ctx context.Context, where string) viewReading {
	t.Helper()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(viewState, &raw)); err != nil {
		t.Fatalf("%s: reading the view: %v", where, err)
	}
	var v viewReading
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	return v
}

func press(t *testing.T, ctx context.Context, where, view string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="`+view+`"]').click(); true`, nil)); err != nil {
		t.Fatalf("%s: pressing %s: %v", where, view, err)
	}
}

func every(ws []string, want string) bool {
	for _, w := range ws {
		if w != want {
			return false
		}
	}
	return len(ws) > 0
}

func TestThePageWideViewAppliesToEveryWidget(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	form := rig.Origin + pageHref(mountPath, RootTheme(), "en", fileOf("form"))
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900), chromedp.Navigate(form), chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form: %v", err)
	}
	if v := readView(t, ctx, "fresh"); len(v.Pressed) != 1 || v.Pressed[0] != "auto" || !every(v.Widgets, "") {
		t.Fatalf("fresh: %+v, want Auto pressed and nothing checked", v)
	}
	press(t, ctx, "Code", "code")
	if v := readView(t, ctx, "Code"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") || v.Stored != "code" {
		t.Errorf("after Code: %+v, want Code pressed, every widget on Code, code stored", v)
	}
	// A reader choosing Mobile in one widget leaves nothing pressed: the
	// page is mixed, and the group says only what is true of every widget.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector(".ds-view__tab--m input").click(); true`, nil)); err != nil {
		t.Fatalf("a local Mobile: %v", err)
	}
	until(t, ctx, "mixed", `document.querySelectorAll(".ds-viewall [aria-pressed=true]").length === 0`)
	// Pressing Code again re-applies it, the case a radio cannot express.
	press(t, ctx, "Code again", "code")
	if v := readView(t, ctx, "Code again"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") {
		t.Errorf("after Code again: %+v", v)
	}
	// The next page opens in Code.
	if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("display"))), chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Display: %v", err)
	}
	if v := readView(t, ctx, "the next page"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") {
		t.Errorf("Display after Code on Form: %+v, want it opened in Code", v)
	}
	press(t, ctx, "Auto", "auto")
	if v := readView(t, ctx, "Auto"); len(v.Pressed) != 1 || v.Pressed[0] != "auto" || !every(v.Widgets, "") || v.Stored != "" {
		t.Errorf("after Auto: %+v, want nothing checked and nothing stored", v)
	}
	// Scripts off, the group has no box: the per-sample radios are the
	// scriptless behaviour, unchanged.
	off := noScripts(t, ctx)
	var box float64
	if err := chromedp.Run(off, chromedp.Navigate(form), chromedp.WaitReady(`.ds-viewall`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector(".ds-viewall").getBoundingClientRect().height`, &box)); err != nil {
		t.Fatalf("scripts off: %v", err)
	}
	if box != 0 {
		t.Errorf("with scripts off the group is %.0fpx tall; a control that cannot work is not shown", box)
	}
}

// Storage that throws on every access (private mode,
// refused site data) costs persistence and nothing else.
func TestThePageWideViewWorksWhenStorageThrows(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx,
		addInit(`Object.defineProperty(window, "localStorage", {configurable: true, get() { throw new DOMException("denied", "SecurityError"); }});
		  window.__errors = 0; addEventListener("error", () => window.__errors++);`),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("form"))),
		chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery)); err != nil {
		t.Fatalf("loading Form with storage that throws: %v", err)
	}
	press(t, ctx, "Code", "code")
	if v := readView(t, ctx, "Code"); len(v.Pressed) != 1 || v.Pressed[0] != "code" || !every(v.Widgets, "c") || v.Stored != "throws" {
		t.Errorf("with storage throwing: %+v, want Code applied and pressed", v)
	}
	until(t, ctx, "no exception escaped", `window.__errors === 0`)
}

// With Code chosen for the whole page and every Rendered HTML
// disclosure open, no source block on the two heaviest pages scrolls
// sideways at 390px: the soft wrap holds even a 3,800px inline run.
// The leg first requires that what it opened is on screen, every
// widget's code and every disclosure's markup: a Code tab that never
// opened leaves nothing visible to measure, and nothing scrolls.
func TestNoCodeBlockScrollsSidewaysOnAPhone(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	for _, kind := range []string{"form", "date-and-time"} {
		var raw string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf(kind))),
			chromedp.WaitReady(`.ds-viewall`, chromedp.ByQuery),
			chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="code"]').click();
			  document.querySelectorAll("details.ds-html").forEach(d => d.open = true); true`, nil),
			chromedp.Evaluate(`(() => {
			  // checkVisibility, not height alone: a block inside a closed
			  // disclosure still measures, because reading its box lays out
			  // the content the closed details skips.
			  const shown = p => p.checkVisibility() && p.getBoundingClientRect().height > 0;
			  const views = [...document.querySelectorAll(".ds-view")].filter(v => v.querySelector(".ds-view__code"));
			  const blind = [];
			  let blocks = 0;
			  views.forEach(v => {
			    const code = [...v.querySelectorAll(".ds-view__code .ds-src")].filter(p => !p.closest("details") && shown(p)).length;
			    const html = [...v.querySelectorAll("details.ds-html")].map(d => [...d.querySelectorAll(".ds-src")].filter(shown).length);
			    if (code === 0 || html.includes(0)) blind.push(v.querySelector("iframe").title);
			    blocks += code + html.reduce((a, b) => a + b, 0);
			  });
			  const wide = [...document.querySelectorAll(".ds-src")].filter(p => shown(p) && p.scrollWidth > p.clientWidth).map(p => p.textContent.slice(0, 50));
			  return JSON.stringify({Views: views.length, Blocks: blocks, Blind: blind, Wide: wide});
			})()`, &raw)); err != nil {
			t.Fatalf("%s at 390px: %v", kind, err)
		}
		var got struct {
			Views, Blocks int
			Blind, Wide   []string
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s at 390px: decoding %q: %v", kind, raw, err)
		}
		if got.Views == 0 || len(got.Blind) > 0 {
			t.Fatalf("%s at 390px: %d widgets with code, and %d show no source block with Code chosen and Rendered HTML open: %q", kind, got.Views, len(got.Blind), got.Blind)
		}
		if len(got.Wide) > 0 {
			t.Errorf("%s at 390px: %d of %d source blocks scroll sideways: %q", kind, len(got.Wide), got.Blocks, got.Wide)
		}
	}
}

// axe with the Code panels open: the scan of every page never saw one
// before, because the panels are display: none until a tab is chosen.
// Each leg first asserts which highlight elements are on the page, so
// the coverage cannot quietly shrink: all five on Form (its calls carry
// actions and strings, its HTML tags, attributes and values), the three
// markup ones on UI primitives, whose samples hold no template action.
func TestA11yScansTheCodePanels(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for _, c := range []struct {
		kind string
		want []string
	}{{"form", []string{"ds-t", "ds-a", "ds-v", "ds-x", "ds-s"}}, {"primitives", []string{"ds-t", "ds-a", "ds-v"}}} {
		for _, theme := range ui.ThemeNames() {
			for _, scheme := range a11ySchemes {
				where := fmt.Sprintf("%s/en %s, Code chosen (%s)", theme, c.kind, scheme)
				var shown []string
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
					chromedp.Navigate(rig.Origin+pageHref(mountPath, theme, "en", fileOf(c.kind))),
					chromedp.WaitVisible(`.ds-viewall`, chromedp.ByQuery),
					chromedp.Evaluate(`document.querySelector('.ds-viewall [data-ds-view="code"]').click();
					  document.querySelectorAll("details.ds-html").forEach(d => d.open = true);
					  ["ds-t", "ds-a", "ds-v", "ds-x", "ds-s"].filter(n => [...document.querySelectorAll(n)].some(e => e.getBoundingClientRect().width > 0))`, &shown)); err != nil {
					t.Fatalf("%s: loading: %v", where, err)
				}
				for _, w := range c.want {
					if !slices.Contains(shown, w) {
						t.Fatalf("%s: no visible %s; this scan would not be checking that colour", where, w)
					}
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(axeJS, nil)); err != nil {
					t.Fatalf("%s: loading axe: %v", where, err)
				}
				paint(t, ctx, scheme)
				total += report(t, where, scan(t, ctx, where, "window.axe", "document", "false"))
			}
		}
	}
	if total == 0 {
		t.Log("clean: the Code panels on Form and UI primitives in every theme and scheme")
	}
}

// Code is left to right on every page, Arabic's included. A <pre>
// inherits the page's dir="rtl", and in a right-to-left run the
// bidi algorithm mirrors brackets and moves punctuation to the far
// end: `<link rel="stylesheet">` reads as `<"link rel="stylesheet>`
// on the screen while its copy is correct, so a reader comparing the
// two trusts neither. Computed style rather than a screenshot, so
// every block on every page kind is read, open or not; the counts are
// the control, because a selector that matched nothing would pass.
func TestCodeIsLeftToRightOnAnArabicPage(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	var blocks, wrapped int
	for _, pk := range pageKinds() {
		where := "day/ar " + pk.Kind
		var raw string
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, "day", "ar", pk.File)),
			chromedp.WaitReady("body"),
			chromedp.Evaluate(`(() => {
			  if (document.documentElement.dir !== "rtl") return JSON.stringify({Page: document.documentElement.dir});
			  const bad = [];
			  const read = sel => { const es = [...document.querySelectorAll(sel)]; es.forEach(e => {
			    const s = getComputedStyle(e);
			    if (s.direction !== "ltr" || !["start", "left"].includes(s.textAlign)) bad.push(sel + " " + s.direction + "/" + s.textAlign + ": " + e.textContent.slice(0, 40));
			  }); return es.length; };
			  return JSON.stringify({Page: "rtl", Src: read(".ds-src"), Wrap: read(".ds-wrap code"), Bad: bad.slice(0, 6)});
			})()`, &raw)); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		var got struct {
			Page      string
			Src, Wrap int
			Bad       []string
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: decoding %q: %v", where, raw, err)
		}
		if got.Page != "rtl" {
			t.Fatalf("%s: the page is dir=%q, so this leg reads nothing a right-to-left page does to code", where, got.Page)
		}
		blocks += got.Src
		wrapped += got.Wrap
		if len(got.Bad) > 0 {
			t.Errorf("%s: code laid out right to left: %q", where, got.Bad)
		}
	}
	if blocks == 0 || wrapped == 0 {
		t.Fatalf("read %d source blocks and %d wrapper-line names over every ar page; want both above zero", blocks, wrapped)
	}
	if !t.Failed() {
		t.Logf("%d source blocks and %d wrapper-line names, all left to right", blocks, wrapped)
	}
}
