//go:build browser

package designsystem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// gate is one held response: a handler blocks on it until the leg has
// looked at the state the hold keeps open. Opened at most once, and by
// the deferred call at the latest, or the server's Close waits on it.
// A nil gate holds nothing, so a leg names only the holds it is about.
type gate struct {
	c    chan struct{}
	once sync.Once
}

func newGate() *gate { return &gate{c: make(chan struct{})} }

func (g *gate) open() {
	if g != nil {
		g.once.Do(func() { close(g.c) })
	}
}

func (g *gate) wait() {
	if g != nil {
		<-g.c
	}
}

// holds are the places heldFrames can hold the first frame on a page.
//   - start: its document, before a byte of it is sent, so the frame
//     still holds the about:blank it was created with;
//   - sheet: the stylesheets its document links, so it is parsed and
//     unstyled;
//   - parse: the rest of its document after the body's first paragraph;
//   - load: an image in that rest, which holds the frame's load event
//     exactly as a slow subresource does;
//   - next, nextLoad: the same two for the document a navigation inside
//     the frame fetches with ?next.
type holds struct{ start, sheet, parse, load, next, nextLoad *gate }

// release opens every hold, for the deferred call: a handler still
// waiting would keep the server's Close waiting with it.
func (h *holds) release() {
	for _, g := range []*gate{h.start, h.sheet, h.parse, h.load, h.next, h.nextLoad} {
		g.open()
	}
}

// The two held images: held waits on load, heldNext on nextLoad.
const held, heldNext = mountPrefix + "held.png", mountPrefix + "held-next.png"

// heldFrames serves the gallery with the first preview frame on target
// held at h. Its document is always streamed up to its body and one
// paragraph before the rest, so a parse hold has a body to look at.
func heldFrames(t *testing.T, target string, h *holds) (*harness.Rig, string) {
	t.Helper()
	files, err := Render(mountPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The first frame on the page is the one held: its address is read
	// from the page rather than assumed.
	m := firstFrame.FindSubmatch(files[strings.TrimPrefix(target, mountPrefix)])
	if m == nil {
		t.Fatalf("no preview frame on %s", target)
	}
	frameSrc := string(m[1])
	rig := harness.New(t, func(string) http.Handler {
		tree := galleryrig.Tree(t, files, mountPrefix)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case held:
				h.load.wait()
				http.NotFound(w, r)
				return
			case heldNext:
				h.nextLoad.wait()
				http.NotFound(w, r)
				return
			}
			if r.URL.RawQuery == "held" {
				h.sheet.wait()
			}
			if r.URL.Path != frameSrc {
				tree.ServeHTTP(w, r)
				return
			}
			rec := httptest.NewRecorder()
			tree.ServeHTTP(rec, r)
			body := rec.Body.String()
			hold, img := h.parse, held
			if r.URL.RawQuery == "next" {
				hold, img = h.next, heldNext
			} else {
				h.start.wait()
				// Its own addresses for the gallery's stylesheets: the
				// gallery has those cached, and a held sheet the browser
				// already has is not held.
				if h.sheet != nil {
					body = strings.ReplaceAll(body, `.css">`, `.css?held">`)
				}
			}
			body = strings.Replace(body, "<body>", `<body><img src="`+img+`" alt="" width="1" height="1">`, 1)
			cut := strings.Index(body, "<body>") + len("<body>")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// Up to the body and one paragraph, so the document exists
			// and has a body while the rest is held.
			w.Write([]byte(body[:cut] + "<p>Held.</p>"))
			w.(http.Flusher).Flush()
			hold.wait()
			w.Write([]byte(body[cut:]))
		})
	})
	rig.Allow(http.MethodGet, held, http.StatusNotFound)
	rig.Allow(http.MethodGet, heldNext, http.StatusNotFound)
	return rig, frameSrc
}

// openChosen sets the gallery's scheme to chosen with the OS in os
// (system is no stored choice, as a reader who never touched the toggle
// has),
// then opens target without waiting for its load, which a held frame
// holds back.
func openChosen(t *testing.T, ctx context.Context, rig *harness.Rig, os, chosen, target string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: os}}),
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+indexHref(mountPath, RootTheme(), "en")),
		chromedp.Evaluate(`localStorage.setItem("rst-ds-scheme", "`+chosen+`"); if ("`+chosen+`" === "system") localStorage.removeItem("rst-ds-scheme"); true`, nil),
		chromedp.Navigate("about:blank"),
	); err != nil {
		t.Fatalf("setting the scheme: %v", err)
	}
	// page.Navigate rather than chromedp.Navigate: the page's load
	// event waits on the held frame, which is the point.
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(rig.Origin + target).Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("opening the page: %v", err)
	}
}

func twoFrames(t *testing.T, ctx context.Context, where string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, nil, awaitPromise)); err != nil {
		t.Fatalf("%s: waiting for a paint: %v", where, err)
	}
}

// A preview frame never shows the reader the other scheme, and shows as
// soon as it can be painted. A preview document carries no script, so
// until gallery.js writes data-theme on it, it resolves light-dark()
// against the operating system. A reader on a light Mac who chose Dark
// therefore got a white frame for the gap between the frame's first
// paint and gallery.js's write, on every lazy frame they scrolled to and
// on every frame in view after a reload at a remembered scroll: the
// white flashes Paul reported. gallery.css hides a frame until
// gallery.js has painted it.
//
// The gap was about 40ms on a local server, too short to catch, so the
// leg holds it open in two steps. First the frame's document is held
// half-parsed: the control proves the window was real (the frame's own
// document is in the other scheme), and the reader must see the page's
// background, not it. Then the document is let through but an image in
// it is held, so the frame is parsed and its load event is not: it must
// show now, painted, or a hung subresource would leave an empty box for
// as long as the network takes to give up. Last the image is let
// through and the frame must still show.
func TestAFrameNeverShowsTheOtherSchemeWhileItLoads(t *testing.T) {
	for _, c := range []struct{ os, chosen string }{{"light", "dark"}, {"dark", "light"}} {
		where := "the OS in " + c.os + ", the gallery in " + c.chosen
		t.Run(c.os+"-os-"+c.chosen+"-gallery", func(t *testing.T) {
			h := &holds{parse: newGate(), load: newGate()}
			defer h.release()
			target := pageHref(mountPath, RootTheme(), "en", fileOf("form"))
			rig, frameSrc := heldFrames(t, target, h)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			openChosen(t, ctx, rig, c.os, c.chosen, target)

			first := `document.querySelector(".ds-view__frame")`
			until(t, ctx, where+", the frame half-parsed",
				`(() => { const d = `+first+`.contentDocument; return !!d && d.URL.endsWith(`+strconv.Quote(frameSrc)+`) && d.readyState === "loading" && !!d.body; })()`)
			twoFrames(t, ctx, where)
			var during frameReading
			readFrame(t, ctx, where+", while it parses", &during)
			if during.Frame == during.Page {
				t.Fatalf("%s: the half-parsed frame's document is already painted %s, the gallery's colour; the window this leg is about was never open", where, during.Frame)
			}
			if during.Seen != during.Page {
				t.Errorf("%s: while the frame parses the reader sees %s in its box, the frame's own %s scheme, on a page painted %s", where, during.Seen, c.os, during.Page)
			}

			h.parse.open()
			until(t, ctx, where+", parsed with its load held",
				first+`.contentDocument.readyState === "interactive"`)
			twoFrames(t, ctx, where)
			var parsed frameReading
			readFrame(t, ctx, where+", parsed", &parsed)
			if parsed.Frame != parsed.Page || parsed.Seen != parsed.Page || parsed.Shown != "visible" {
				t.Errorf("%s: once parsed, with an image still loading, the frame must show in the gallery's scheme: its document is %s, the reader sees %s (visibility %s), the page is %s", where, parsed.Frame, parsed.Seen, parsed.Shown, parsed.Page)
			}

			h.load.open()
			until(t, ctx, where+", loaded", first+`.contentDocument.readyState === "complete"`)
			twoFrames(t, ctx, where)
			var after frameReading
			readFrame(t, ctx, where+", loaded", &after)
			if after.Frame != after.Page || after.Seen != after.Page || after.Shown != "visible" {
				t.Errorf("%s: once loaded the frame must show in the gallery's scheme: its document is %s, the reader sees %s (visibility %s), the page is %s", where, after.Frame, after.Seen, after.Shown, after.Page)
			}
		})
	}
}

// A preview frame shows the reader nothing but the page's own
// background until its document is parsed and styled, whatever the
// scheme toggle says. System is the case that matters: it is the
// default, and gallery.css once hid a frame only while Light or Dark
// was chosen, on the grounds that in System both sides follow the OS.
// They do, but that is not all a frame shows before its document
// paints.
//
// Safari (measured in Playwright's WebKit build, OS dark, System): a
// frame whose request has not answered yet holds the about:blank it was
// created with, and WebKit paints that document's canvas opaque white,
// because the frame element inherits the page's dark color-scheme and
// about:blank has none, so it is light. That was the white flash on
// every frame on an iPhone in dark mode. While its head parses it shows
// rgb(30,30,30), not the page's background. Chromium paints neither:
// its frame stays transparent at both moments, so in this engine the
// frame's visibility is the reading that failed first, and the pixels
// are the guarantee the reader gets in every engine.
//
// Two holds: first the frame's response, so it is still about:blank,
// then the stylesheets its document links, so it is parsed and
// unstyled. Hidden at both. With the sheets let through it shows, in
// the same colour as the page.
func TestAFrameShowsNothingUntilItsDocumentIsStyled(t *testing.T) {
	for _, os := range []string{"dark", "light"} {
		where := "the OS in " + os + ", the gallery on System"
		t.Run(os+"-os-system-gallery", func(t *testing.T) {
			h := &holds{start: newGate(), sheet: newGate()}
			defer h.release()
			target := pageHref(mountPath, RootTheme(), "en", fileOf("form"))
			rig, frameSrc := heldFrames(t, target, h)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			openChosen(t, ctx, rig, os, "system", target)
			first := `document.querySelector(".ds-view__frame")`

			until(t, ctx, where+", the frame still blank",
				`(() => { const f = `+first+`; return !!f && !!f.contentDocument && f.contentDocument.URL === "about:blank" && !document.documentElement.hasAttribute("data-theme"); })()`)
			twoFrames(t, ctx, where)
			var blank frameReading
			readFrame(t, ctx, where+", still about:blank", &blank)
			if blank.Shown != "hidden" || blank.Off != 0 {
				t.Errorf("%s: while the frame still holds about:blank it is %s, and %d pixels in its box are not the page's %s; Safari paints that document white whenever the frame can resolve dark", where, blank.Shown, blank.Off, blank.Page)
			}
			// The cause, for a reader without scripts, who gets no hiding:
			// WebKit paints about:blank opaque only when the frame's
			// color-scheme is not about:blank's own (measured: light dark
			// and dark are white, normal and light are transparent).
			if blank.Scheme != "normal" {
				t.Errorf("%s: the frame element's color-scheme is %q; anything that can resolve dark has Safari paint the blank frame white", where, blank.Scheme)
			}

			h.start.open()
			until(t, ctx, where+", parsed with its stylesheets held",
				`(() => { const d = `+first+`.contentDocument; return !!d && d.URL.endsWith(`+strconv.Quote(frameSrc)+`) && d.readyState !== "loading"; })()`)
			twoFrames(t, ctx, where)
			var bare frameReading
			readFrame(t, ctx, where+", parsed, unstyled", &bare)
			if bare.Frame != "rgba(0,0,0,0)" {
				t.Fatalf("%s: the parsed frame's body is already %s; its stylesheets were not held, and the window this leg is about was never open", where, bare.Frame)
			}
			if bare.Shown != "hidden" || bare.Off != 0 {
				t.Errorf("%s: parsed with its stylesheets still loading, the frame is %s, and %d pixels in its box are not the page's %s; its unstyled document would show", where, bare.Shown, bare.Off, bare.Page)
			}

			h.sheet.open()
			until(t, ctx, where+", shown", `getComputedStyle(`+first+`).visibility === "visible"`)
			twoFrames(t, ctx, where)
			var shown frameReading
			readFrame(t, ctx, where+", styled", &shown)
			if shown.Frame != shown.Page || shown.Seen != shown.Page {
				t.Errorf("%s: once styled the frame must show in the page's scheme: its document is %s, the reader sees %s, the page is %s", where, shown.Frame, shown.Seen, shown.Page)
			}
		})
	}
}

// A preview frame never takes the reader's focus, or moves the page, on
// its own. The stage shell is a sign-in screen, and five screen samples
// are too: each puts autofocus on its first field, which is right for
// the page and wrong inside the gallery. A same-origin frame's autofocus
// is honoured by the page around it, and focusing scrolls the frame
// into view, so a reader scrolling towards the stage shell was pulled
// down to it with the caret in its email field.
//
// It is also how the a11y gate went red on a runner whose Chromium
// loads lazy frames from further away: the stage frame loaded while the
// scan sat at the top, its autofocus scrolled the page, and the pinned
// bar then covered the console shell's Open button above it, which axe
// reported as a target too small (obscured, 186.4px by 13.4px).
//
// The screens' samples lose the attribute at render
// (TestNoPreviewAsksForFocus). The shell demo keeps it, because it is
// also opened on its own; gallery.js, which runs in it, holds the field
// inert in a frame until after load, so its request is refused, and
// then lets it go, so the reader can still use it.
func TestAFrameNeverMovesTheReader(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	target := pageHref(mountPath, RootTheme(), "en", fileOf("shells"))
	stage := `document.querySelector('.ds-view__frame[src=` + strconv.Quote(shellHref(mountPath, RootTheme(), "en", "stage")) + `]')`
	var at float64
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+target),
		// The frame just under the fold: near enough to load, far enough
		// that focusing anything in it has to scroll the page.
		chromedp.Evaluate(`(() => { const b = `+stage+`.parentElement.getBoundingClientRect(); scrollTo(0, scrollY + b.top - innerHeight - 100); return scrollY; })()`, &at),
	); err != nil {
		t.Fatalf("opening the shells page: %v", err)
	}
	until(t, ctx, "the stage shell loaded",
		`(() => { const f = `+stage+`, d = f.contentDocument; return !!d && d.URL.endsWith("/stage.html") && d.readyState === "complete" && f.hasAttribute("data-ds-painted"); })()`)
	twoFrames(t, ctx, "the stage shell")
	var got struct {
		Autofocus, Inert bool
		Y                float64
		Active           string
	}
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Sleep(200*time.Millisecond),
		// The control reads the file as served: in the frame the
		// attribute is gone, which is the fix.
		chromedp.Evaluate(`(async () => JSON.stringify({Autofocus: (await (await fetch(`+stage+`.src)).text()).includes(" autofocus"), Y: scrollY,
		  Inert: `+stage+`.contentDocument.querySelector("input[type=email]").inert,
		  Active: document.activeElement === document.body ? "body" : document.activeElement.outerHTML.slice(0, 120)}))()`, &raw, awaitPromise),
	); err != nil {
		t.Fatalf("reading the page: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if !got.Autofocus {
		t.Fatalf("the stage shell is served with no autofocus field any more; this leg needs a framed document that asks for focus")
	}
	if got.Active != "body" || got.Y != at {
		t.Errorf("the stage shell's frame took the reader: focus is on %s, and the page moved from %v to %v", got.Active, at, got.Y)
	}
	if got.Inert {
		t.Errorf("the stage shell's email field is still inert after its frame loaded; the reader cannot use it")
	}
}

// A navigation inside a preview frame (a sample's form or link loading
// a new document in it) gets the same hiding and the same showing as
// the first load. The frame keeps its painted mark across a navigation
// unless the old page's pagehide clears it, and the new document then
// paints in the OS's scheme, unhidden, until gallery.js reaches it: the
// white flash again, on a click. Held half-parsed, the new document
// must be hidden; parsed with an image of its own still loading, shown
// and painted, or a hung subresource would leave an empty box until the
// network gave up; loaded, still shown.
//
// Two legs, one per document the navigation leaves: one whose load has
// fired, and one shown at parse whose image is still held. pagehide was
// once wired up only at load, so the second left the old mark in place
// and the next document showed in the OS's scheme.
func TestAFrameHidesWhileItNavigates(t *testing.T) {
	const os, chosen = "light", "dark"
	for _, c := range []struct {
		name   string
		loaded bool
	}{{"from a loaded document", true}, {"from a document still loading", false}} {
		t.Run(strings.ReplaceAll(c.name, " ", "-"), func(t *testing.T) {
			h := &holds{load: newGate(), next: newGate(), nextLoad: newGate()}
			defer h.release()
			ready := `.contentDocument.readyState === "interactive"`
			if c.loaded {
				h.load.open()
				ready = `.contentDocument.readyState === "complete"`
			}
			target := pageHref(mountPath, RootTheme(), "en", fileOf("form"))
			rig, frameSrc := heldFrames(t, target, h)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			openChosen(t, ctx, rig, os, chosen, target)
			first := `document.querySelector(".ds-view__frame")`
			until(t, ctx, c.name+": the first document shown",
				`!!`+first+`.contentDocument && `+first+`.contentDocument.URL.endsWith(`+strconv.Quote(frameSrc)+`) && `+first+ready+` && `+first+`.hasAttribute("data-ds-painted")`)

			if err := chromedp.Run(ctx, chromedp.Evaluate(first+`.contentWindow.location.href = `+strconv.Quote(frameSrc+"?next")+`, true`, nil)); err != nil {
				t.Fatalf("%s: navigating the frame: %v", c.name, err)
			}
			until(t, ctx, c.name+": the next document half-parsed",
				`(() => { const d = `+first+`.contentDocument; return !!d && d.URL.endsWith("?next") && d.readyState === "loading" && !!d.body; })()`)
			twoFrames(t, ctx, c.name)
			var during frameReading
			readFrame(t, ctx, c.name+": the next document, while it parses", &during)
			if during.Frame == during.Page {
				t.Fatalf("%s: the half-parsed next document is already painted %s, the gallery's colour; the window this leg is about was never open", c.name, during.Frame)
			}
			if during.Seen != during.Page {
				t.Errorf("%s: while the frame's next document parses the reader sees %s in its box, the OS's %s scheme, on a page painted %s", c.name, during.Seen, os, during.Page)
			}

			h.next.open()
			until(t, ctx, c.name+": the next document parsed with its load held",
				first+`.contentDocument.URL.endsWith("?next") && `+first+`.contentDocument.readyState === "interactive"`)
			twoFrames(t, ctx, c.name)
			var parsed frameReading
			readFrame(t, ctx, c.name+": the next document, parsed", &parsed)
			if parsed.Frame != parsed.Page || parsed.Seen != parsed.Page || parsed.Shown != "visible" {
				t.Errorf("%s: once the next document is parsed, with an image still loading, the frame must show in the gallery's scheme: its document is %s, the reader sees %s (visibility %s), the page is %s", c.name, parsed.Frame, parsed.Seen, parsed.Shown, parsed.Page)
			}

			h.nextLoad.open()
			until(t, ctx, c.name+": the next document loaded", first+`.contentDocument.URL.endsWith("?next") && `+first+`.contentDocument.readyState === "complete"`)
			twoFrames(t, ctx, c.name)
			var after frameReading
			readFrame(t, ctx, c.name+": the next document, loaded", &after)
			if after.Frame != after.Page || after.Seen != after.Page || after.Shown != "visible" {
				t.Errorf("%s: once the next document loads the frame must show in the gallery's scheme: its document is %s, the reader sees %s (visibility %s), the page is %s", c.name, after.Frame, after.Seen, after.Shown, after.Page)
			}
		})
	}
}

// After pagehide the gallery looks for the frame's next document every
// 16ms, and a look that never ends is a timer holding the frame and its
// old document for the life of the page. Two ways it never ended: the
// frame navigated to a document the gallery cannot read (another
// origin; a data: URL stands in) whose load hangs, so the frame stayed
// hidden for good; and the frame was taken out of the page mid-look.
// The look now stops when the frame leaves the page, and gives up after
// a bound, showing the frame unpainted: a frame in the wrong scheme
// beats one that never appears.
//
// The looks are counted by wrapping the page's setTimeout before
// gallery.js runs; nothing else in it waits 16ms.
func TestAFrameStopsLookingForItsNextDocument(t *testing.T) {
	const os, chosen = "light", "dark"
	const count = `(() => {
	  const set = window.setTimeout;
	  window.dsLooks = 0;
	  window.setTimeout = function (fn, ms) {
	    if (ms === 16) window.dsLooks++;
	    return set.apply(this, arguments);
	  };
	})()`
	for _, c := range []struct {
		name   string
		remove bool
	}{{"a navigation that never loads", false}, {"a frame removed while it navigates", true}} {
		t.Run(strings.ReplaceAll(c.name, " ", "-"), func(t *testing.T) {
			h := &holds{nextLoad: newGate()}
			defer h.release()
			target := pageHref(mountPath, RootTheme(), "en", fileOf("form"))
			rig, frameSrc := heldFrames(t, target, h)
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, addInit(count)); err != nil {
				t.Fatalf("%s: counting the looks: %v", c.name, err)
			}
			openChosen(t, ctx, rig, os, chosen, target)
			first := `document.querySelector(".ds-view__frame")`
			until(t, ctx, c.name+": the first document shown",
				`!!`+first+`.contentDocument && `+first+`.contentDocument.URL.endsWith(`+strconv.Quote(frameSrc)+`) && `+first+`.contentDocument.readyState === "complete" && `+first+`.hasAttribute("data-ds-painted")`)

			// Unreadable from the gallery, and its image waits on nextLoad,
			// which stays shut: the frame's load never fires.
			elsewhere := `<!doctype html><p>Elsewhere.</p><img src="` + rig.Origin + heldNext + `" alt="" width="1" height="1">`
			if err := chromedp.Run(ctx, chromedp.Evaluate(first+`.src = "data:text/html," + encodeURIComponent(`+strconv.Quote(elsewhere)+`), true`, nil)); err != nil {
				t.Fatalf("%s: navigating the frame: %v", c.name, err)
			}
			until(t, ctx, c.name+": looking for the next document",
				`window.dsLooks > 3 && !`+first+`.hasAttribute("data-ds-painted")`)

			if c.remove {
				if err := chromedp.Run(ctx, chromedp.Evaluate(first+`.remove(), true`, nil)); err != nil {
					t.Fatalf("%s: removing the frame: %v", c.name, err)
				}
			} else {
				until(t, ctx, c.name+": the frame shown once the look gives up",
					first+`.hasAttribute("data-ds-painted") && getComputedStyle(`+first+`).visibility === "visible"`)
			}
			var before, after int
			if err := chromedp.Run(ctx,
				chromedp.Sleep(100*time.Millisecond),
				chromedp.Evaluate(`window.dsLooks`, &before),
				chromedp.Sleep(500*time.Millisecond),
				chromedp.Evaluate(`window.dsLooks`, &after),
			); err != nil {
				t.Fatalf("%s: reading the looks: %v", c.name, err)
			}
			if after != before {
				t.Errorf("%s: the gallery still looks for the frame's next document, %d times in half a second", c.name, after-before)
			}
		})
	}
}

func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }

// frameReading is one look at the first frame: the background its own
// document paints, the page's, whether the frame is visible, its own
// color-scheme, the pixel the reader actually gets inside the frame's
// box, in its body's padding where nothing but that background is
// drawn, and how many pixels across the whole box differ from the
// page's background.
type frameReading struct {
	Frame, Page, Seen, Shown, Scheme string
	X, Y                             float64
	Box                              [4]float64
	Off                              int
}

func readFrame(t *testing.T, ctx context.Context, where string, r *frameReading) {
	t.Helper()
	var raw string
	var shot []byte
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
		  const f = document.querySelector(".ds-view__frame"), b = f.getBoundingClientRect(), x = f.parentElement.getBoundingClientRect();
		  const rgb = s => s.replace(/\s+/g, "");
		  return JSON.stringify({Frame: rgb(getComputedStyle(f.contentDocument.body).backgroundColor),
		    Page: rgb(getComputedStyle(document.body).backgroundColor), Shown: getComputedStyle(f).visibility,
		    Scheme: getComputedStyle(f).colorScheme, X: b.left + b.width / 2, Y: b.top + 8,
		    Box: [x.left, x.top, x.right, Math.min(x.bottom, innerHeight)]});
		})()`, &raw),
		chromedp.CaptureScreenshot(&shot),
	); err != nil {
		t.Fatalf("%s: reading the frame: %v", where, err)
	}
	if err := json.Unmarshal([]byte(raw), r); err != nil {
		t.Fatalf("%s: decoding %q: %v", where, raw, err)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatalf("%s: decoding the screenshot: %v", where, err)
	}
	at := func(x, y int) string {
		cr, cg, cb, _ := img.At(x, y).RGBA()
		return fmt.Sprintf("rgb(%d,%d,%d)", cr>>8, cg>>8, cb>>8)
	}
	r.Seen = at(int(r.X), int(r.Y))
	// Inset past the box's border and rounded corners, which are the
	// page's own drawing, not the frame's.
	const inset = 6
	for y := int(r.Box[1]) + inset; y < int(r.Box[3])-inset; y++ {
		for x := int(r.Box[0]) + inset; x < int(r.Box[2])-inset; x++ {
			if at(x, y) != r.Page {
				r.Off++
			}
		}
	}
}

var firstFrame = regexp.MustCompile(`<iframe class="ds-view__frame"[^>]*\ssrc="([^"]+)"`)
