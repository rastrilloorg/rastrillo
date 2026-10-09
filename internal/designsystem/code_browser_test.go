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
		// Show the first widget's template call, so the selection the
		// failure makes is a selection a reader could see.
		chromedp.Evaluate(`document.querySelector(".ds-view__tab--t input").click(); true`, nil),
	); err != nil {
		t.Fatalf("loading Form with a stubbed clipboard: %v", err)
	}

	// Every block a reader can copy has a button, and every button's
	// accessible name is unique on the page and begins with its label.
	assertNames(t, ctx, "form")

	// A resolved write: the clipboard gets exactly the block's text, the
	// region says Copied, and the label says it too.
	const first = `document.querySelector(".ds-view__code--t .ds-copy")`
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

// With every widget's HTML tab chosen, and then every Template tab, no
// source block on the two heaviest pages scrolls sideways at 390px: the
// soft wrap holds even a 3,800px inline run. Each pass first requires
// that what it chose is on screen, every widget's panel: a tab that
// never opened leaves nothing visible to measure, and nothing scrolls.
func TestNoCodeBlockScrollsSidewaysOnAPhone(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	for _, kind := range []string{"form", "date-and-time"} {
		for _, tab := range []string{"h", "t"} {
			where := fmt.Sprintf("%s at 390px, every ds-view__tab--%s chosen", kind, tab)
			var raw string
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf(kind))),
				chromedp.WaitReady(`.ds-view`, chromedp.ByQuery),
				chromedp.Evaluate(`document.querySelectorAll(".ds-view__tab--`+tab+` input").forEach(i => i.checked = true); true`, nil),
				chromedp.Evaluate(`(() => {
				  const shown = p => p.checkVisibility() && p.getBoundingClientRect().height > 0;
				  const views = [...document.querySelectorAll(".ds-view")].filter(v => v.querySelector(".ds-view__code--`+tab+`"));
				  const blind = [];
				  let blocks = 0;
				  views.forEach(v => {
				    const n = [...v.querySelectorAll(".ds-view__code--`+tab+` .ds-src")].filter(shown).length;
				    if (n === 0) blind.push(v.querySelector("iframe").title);
				    blocks += n;
				  });
				  const wide = [...document.querySelectorAll(".ds-src")].filter(p => shown(p) && p.scrollWidth > p.clientWidth).map(p => p.textContent.slice(0, 50));
				  return JSON.stringify({Views: views.length, Blocks: blocks, Blind: blind, Wide: wide});
				})()`, &raw)); err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			var got struct {
				Views, Blocks int
				Blind, Wide   []string
			}
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("%s: decoding %q: %v", where, raw, err)
			}
			if got.Views == 0 || len(got.Blind) > 0 {
				t.Fatalf("%s: %d widgets with the panel, and %d show no source block: %q", where, got.Views, len(got.Blind), got.Blind)
			}
			if len(got.Wide) > 0 {
				t.Errorf("%s: %d of %d source blocks scroll sideways: %q", where, len(got.Wide), got.Blocks, got.Wide)
			}
		}
	}
}

// axe with the HTML and Template panels open: the scan of every page
// never saw one before, because the panels are display: none until a
// tab is chosen. Each leg first asserts which highlight elements are
// on the page, so the coverage cannot quietly shrink: Form's HTML tags,
// attributes and values, its calls' actions and strings, and the three
// markup ones on UI primitives, whose samples hold no template action.
func TestA11yScansTheCodePanels(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 600*time.Second)
	defer cancel()
	axeJS := axeSource(t)
	total := 0
	for _, c := range []struct {
		kind, tab string
		want      []string
	}{{"form", "h", []string{"ds-t", "ds-a", "ds-v"}}, {"form", "t", []string{"ds-x", "ds-s"}}, {"primitives", "h", []string{"ds-t", "ds-a", "ds-v"}}} {
		for _, theme := range ui.ThemeNames() {
			for _, scheme := range a11ySchemes {
				where := fmt.Sprintf("%s/en %s, ds-view__tab--%s chosen (%s)", theme, c.kind, c.tab, scheme)
				var shown []string
				if err := chromedp.Run(ctx, chromedp.EmulateViewport(1280, 900),
					chromedp.Navigate(rig.Origin+pageHref(mountPath, theme, "en", fileOf(c.kind))),
					chromedp.WaitVisible(`.ds-view`, chromedp.ByQuery),
					chromedp.Evaluate(`document.querySelectorAll(".ds-view__tab--`+c.tab+` input").forEach(i => i.checked = true);
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
		t.Log("clean: the HTML and Template panels on Form and UI primitives in every theme and scheme")
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
