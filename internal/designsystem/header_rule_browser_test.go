//go:build browser

// The gallery-wide half of the tinted hairline's evidence (design doc
// §6-v2.2). ui/header_rule_browser_test.go proves the rule resolves to
// the ruled mix, in isolation, against an oklab implementation of its
// own. This file proves the other half of the claim, which isolation
// cannot: that the retired ::after draws nothing, and the rule is the
// theme's token, on the pages people actually look at — every theme,
// every locale, inside the component previews as well as on the page.
//
// The preview iframes are the reason this exists rather than being
// waved through. They are separate documents with their own copy of the
// stylesheet and their own resolved scheme, they are the surface a
// reader judges a component by, and they are lazily loaded, so a drive
// that only queries the top document would report a clean sweep having
// looked at the least interesting header on the page. Every frame is
// forced to load and read.
//
// ── The control ──────────────────────────────────────────────────────
//
// §7-v2: a gate nobody has watched fail is a gate nobody has tested.
// RST_RAKE_LINE_CONTROL=1 appends the retired flourish back onto the
// tokens.css this tree serves, so a sweep that finds 180 clean headers
// can be watched finding them drawing it again:
//
//	RST_RAKE_LINE_CONTROL=1 go test -tags browser ./internal/designsystem/ \
//	  -run TestNoGalleryPageStillDrawsTheRakeLine -v
//
// Unset — which is how CI runs it — it does nothing at all.
package designsystem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/ui"
)

// headerRuleSweep forces every iframe to load, then reads every page
// header in the document and in every frame it can reach.
//
// The colour comes back as a hex string painted through a 1×1 canvas,
// for the reason ui's drive gives: getComputedStyle serialises a
// color-mix() result differently across engine versions, and a drive
// that parses a serialisation breaks on a Chromium upgrade while the
// pixels are fine.
//
// Each document is asked for its OWN --rst-header-rule, through a probe
// it appends and removes, and the header's border is compared with
// that. So this file checks the wiring — the rule the browser painted
// is the token the theme declared — and leaves the arithmetic to ui's
// drive, which computes the mix independently. Neither alone is enough:
// this one would agree with a wrong token, that one never opens a
// gallery page.
const headerRuleSweep = `(async () => {
  const frames = [...document.querySelectorAll("iframe")];
  await Promise.all(frames.map(f => new Promise(done => {
    const ready = () => {
      try { return f.contentDocument && f.contentDocument.readyState === "complete" && f.contentDocument.body; }
      catch (e) { return false; }
    };
    if (ready()) { done(); return; }
    f.addEventListener("load", () => done(), { once: true });
    // Re-assigning srcdoc reloads a frame the lazy loader has not
    // reached yet, which below the fold is most of them.
    f.loading = "eager";
    if (f.hasAttribute("srcdoc")) { f.srcdoc = f.getAttribute("srcdoc"); }
    setTimeout(done, 5000);
  })));

  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const g = canvas.getContext("2d", { willReadFrequently: true });
  const hex = css => {
    g.clearRect(0, 0, 1, 1);
    g.fillStyle = "#000000";
    g.fillStyle = css;
    g.fillRect(0, 0, 1, 1);
    const d = g.getImageData(0, 0, 1, 1).data;
    return "#" + [d[0], d[1], d[2]].map(n => n.toString(16).padStart(2, "0")).join("");
  };

  const out = { Headers: [], Frames: frames.length, FramesRead: 0, FramesWithHeaders: 0 };
  const scan = (doc, where) => {
    const view = doc.defaultView;
    const probe = doc.createElement("span");
    probe.style.background = "var(--rst-header-rule)";
    doc.body.appendChild(probe);
    const tokenRaw = view.getComputedStyle(probe).backgroundColor;
    probe.remove();
    const found = doc.querySelectorAll("[rst-page-header]");
    for (const h of found) {
      const s = view.getComputedStyle(h);
      const a = view.getComputedStyle(h, "::after");
      out.Headers.push({
        Where: where,
        Rule: hex(s.borderBottomColor),
        Token: hex(tokenRaw),
        TokenRaw: tokenRaw,
        Width: s.borderBottomWidth,
        Style: s.borderBottomStyle,
        AfterContent: a.content,
        AfterWidth: Math.round(parseFloat(a.width) || 0),
        Dir: view.getComputedStyle(doc.documentElement).direction,
      });
    }
    return found.length;
  };

  scan(document, "page");
  for (const f of frames) {
    let d = null;
    try { d = f.contentDocument; } catch (e) { d = null; }
    if (!d || !d.body) { continue; }
    out.FramesRead++;
    if (scan(d, "frame: " + (f.getAttribute("title") || "?")) > 0) { out.FramesWithHeaders++; }
  }
  return JSON.stringify(out);
})()`

type headerRuleHeader struct {
	Where        string
	Rule         string
	Token        string
	TokenRaw     string
	Width        string
	Style        string
	AfterContent string
	AfterWidth   int
	Dir          string
}

type headerRuleSweepResult struct {
	Headers           []headerRuleHeader
	Frames            int
	FramesRead        int
	FramesWithHeaders int
}

// TestNoGalleryPageStillDrawsTheRakeLine is the sweep.
//
// Coverage, stated so nobody has to infer it: index.html in all three
// themes and all twelve locales — which is every locale the tree ships,
// ar included — plus the two pages that carry headers somewhere other
// than the top of the document, in en and in ar. list-screen.html holds
// sixteen preview iframes, one of which is the page-header component
// itself; demo.html is the full-page application demo, whose three
// screens each have a header of their own.
//
// Bug classes it exists to catch:
//
//   - the flourish survives in a preview iframe, which is the one place
//     a reader inspects a component closely and the one place a drive
//     over the top document would never look;
//   - a page links a stale vendored stylesheet, so the tree renders two
//     different header rules and the gallery disagrees with itself;
//   - the token is undefined in some document — a frame that links
//     tokens.css and no theme — leaving the border at currentColor,
//     which on a dark scheme is a bright line nobody asked for;
//   - the rule stops being 1px solid, e.g. because a shorthand somewhere
//     took the border back.
func TestNoGalleryPageStillDrawsTheRakeLine(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return rakeLineHandler(t) })

	// Wall-clock against a real browser over roughly fifty page loads,
	// each one waiting on up to sixteen iframes. The budget exists so a
	// hang fails as itself, not to race a busy runner.
	ctx, cancel := context.WithTimeout(rig.Context(), 15*time.Minute)
	defer cancel()

	type target struct{ theme, locale, file string }
	var targets []target
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			targets = append(targets, target{theme, locale, "index.html"})
		}
		// The pages whose headers are not at the top of the document:
		// the component previews and the full-page application demo. In
		// en and in ar, so the RTL rendering of each is driven too.
		for _, locale := range []string{"en", "ar"} {
			targets = append(targets,
				target{theme, locale, "list-screen.html"},
				target{theme, locale, "demo.html"})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.theme != b.theme {
			return a.theme < b.theme
		}
		if a.locale != b.locale {
			return a.locale < b.locale
		}
		return a.file < b.file
	})

	rtl := map[string]bool{"ar": true}
	headers, framesRead, framesWithHeaders, pagesWithFrames := 0, 0, 0, 0

	for _, tg := range targets {
		name := tg.theme + "/" + tg.locale + "/" + tg.file
		url := rig.Origin + pageHref(mountPath, tg.theme, tg.locale, tg.file)

		var raw string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(1280, 900),
			chromedp.Navigate(url),
			chromedp.WaitVisible(`[rst-page-header]`, chromedp.ByQuery),
			chromedp.Evaluate(headerRuleSweep, &raw,
				func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
		); err != nil {
			t.Fatalf("%s: driving the page: %v", name, err)
		}
		var got headerRuleSweepResult
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: reading the sweep (%q): %v", name, raw, err)
		}
		if len(got.Headers) == 0 {
			t.Errorf("%s: no page header was measured; a sweep that finds nothing is not a clean sweep", name)
			continue
		}
		if got.Frames > 0 {
			pagesWithFrames++
			if got.FramesRead != got.Frames {
				t.Errorf("%s: read %d of %d preview frames; an unread frame is an unchecked one, and the previews are where a reader judges a component", name, got.FramesRead, got.Frames)
			}
			framesRead += got.FramesRead
			framesWithHeaders += got.FramesWithHeaders
		}
		// list-screen.html is in the target list BECAUSE one of its
		// sixteen previews is the page-header component itself. Reading
		// the frames is not enough: a frame whose scan finds nothing
		// passes every assertion below vacuously, and §6-v3 stage 3
		// flips the markup spelling, so a preview that stopped matching
		// [rst-page-header] would hollow out the frame half of this gate
		// without failing it. Pin the floor where the reason is.
		if tg.file == "list-screen.html" && got.FramesWithHeaders == 0 {
			t.Errorf("%s: read %d preview frames and not one of them contained a page header. This page is driven for its previews; frames that match nothing make the sweep look thorough and check nothing", name, got.FramesRead)
		}
		headers += len(got.Headers)

		for _, h := range got.Headers {
			where := name + " (" + h.Where + ")"
			if h.AfterContent != "none" {
				t.Errorf("%s: [rst-page-header]::after has content %q, want \"none\" — the rake line is retired", where, h.AfterContent)
			}
			if h.AfterWidth != 0 {
				t.Errorf("%s: [rst-page-header]::after still lays out a %dpx box", where, h.AfterWidth)
			}
			if h.TokenRaw == "rgba(0, 0, 0, 0)" {
				t.Errorf("%s: --rst-header-rule resolves to nothing in this document, so the border fell back to currentColor. This document has tokens.css without a theme", where)
			}
			if h.Rule != h.Token {
				t.Errorf("%s: the header rule painted %s, but this document's --rst-header-rule is %s (%s)", where, h.Rule, h.Token, h.TokenRaw)
			}
			if h.Width != "1px" || h.Style != "solid" {
				t.Errorf("%s: the header rule is %s %s, want 1px solid", where, h.Width, h.Style)
			}
			wantDir := "ltr"
			if rtl[tg.locale] {
				wantDir = "rtl"
			}
			// A frame is its own document and carries its own dir, set
			// from the same locale as the page that holds it.
			if h.Dir != wantDir {
				t.Errorf("%s: the document resolved direction %q, want %q", where, h.Dir, wantDir)
			}
		}
	}

	// The sweep has to have swept. Without this, every assertion above is
	// vacuously satisfied by a run that loaded nothing — which is the
	// failure mode this branch has shipped four times.
	if headers < len(targets) {
		t.Errorf("measured %d headers over %d pages; every page has at least one", headers, len(targets))
	}
	if pagesWithFrames == 0 || framesRead == 0 || framesWithHeaders == 0 {
		t.Errorf("read %d preview frames over %d pages carrying them, %d of which held a page header; the component previews are half of what this test is for", framesRead, pagesWithFrames, framesWithHeaders)
	}
	t.Logf("swept %d page headers over %d pages: %d preview frames read, %d of them carrying a header", headers, len(targets), framesRead, framesWithHeaders)
}

// rakeLineHandler is the tree, optionally with the retired flourish put
// back. See "The control" at the top of this file: the appended rule is
// the one this branch deleted, byte for byte, so the sweep is watched
// failing on exactly the thing it exists to prevent rather than on a
// stand-in for it.
func rakeLineHandler(t *testing.T) http.Handler {
	t.Helper()
	tree := treeHandler(t)
	if os.Getenv("RST_RAKE_LINE_CONTROL") == "" {
		return tree
	}
	const rakeLine = `
[rst-page-header]::after {
  background: var(--rst-accent);
  block-size: 2px;
  content: "";
  inline-size: 2.5rem;
  inset-block-end: -1px;
  inset-inline-start: 0;
  position: absolute;
}
`
	t.Logf("CONTROL: serving tokens.css with the rake line appended; this run is EXPECTED to fail on every page")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tokens.css") {
			tree.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		tree.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
		w.Write([]byte(rakeLine))
	})
}
