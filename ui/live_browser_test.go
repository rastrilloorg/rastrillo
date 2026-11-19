//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/live"
)

const livePaused = "Live updates have paused. Refresh the page to start them again."

// liveApp is a page with live regions, served by a real live.Hub: the
// stream, its catch-up from the URL's versions and the fragment
// answers are the server's own, so these drives check live.js against
// the wire format rather than against a fake of it. Every region
// follows topic "a".
type liveApp struct {
	hub *live.Hub

	mu        sync.Mutex
	order     []string          // region names, in page order
	inner     map[string]string // each region's inner HTML, as rendered now
	topNotice bool              // render <div id="notice"> before the regions
	head      string            // more markup for <head>, before live.js
	hold      int               // hold this many of the next fragment requests
	release   chan struct{}     // closing it lets the held requests answer
	inflight  int
	most      int      // the most fragment requests ever in flight at once
	fragments []string // every Rastrillo-Fragment value, in order
	streams   []string // every stream request's query, in order

	down atomic.Bool // the stream answers 503
}

func newLiveApp(t *testing.T, topNotice bool, regions ...string) *liveApp {
	t.Helper()
	hub, err := live.New(live.Config{
		Path:   "/live",
		Topics: map[string]live.Allow{"a": func(*http.Request, string) bool { return true }},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	a := &liveApp{hub: hub, inner: map[string]string{}, topNotice: topNotice, release: make(chan struct{})}
	for i := 0; i+1 < len(regions); i += 2 {
		a.order = append(a.order, regions[i])
		a.inner[regions[i]] = regions[i+1]
	}
	return a
}

// change rewrites a region and announces it, as a handler's write and
// Notify would.
func (a *liveApp) change(name, inner string) {
	a.mu.Lock()
	a.inner[name] = inner
	a.mu.Unlock()
	a.hub.Notify("a")
}

// set rewrites a region without announcing it: a form's own write,
// whose answer the page fetches itself.
func (a *liveApp) set(name, inner string) {
	a.mu.Lock()
	a.inner[name] = inner
	a.mu.Unlock()
}

// remove takes a region off the server's page and announces it, so a
// fetch for it is answered by navigating.
func (a *liveApp) remove(name string) {
	a.mu.Lock()
	delete(a.inner, name)
	a.mu.Unlock()
	a.hub.Notify("a")
}

func (a *liveApp) holdNext(n int) {
	a.mu.Lock()
	a.hold, a.release = n, make(chan struct{})
	a.mu.Unlock()
}

func (a *liveApp) letGo() {
	a.mu.Lock()
	close(a.release)
	a.mu.Unlock()
}

func (a *liveApp) read(f func(a *liveApp) bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return f(a)
}

func (a *liveApp) region(r *http.Request, name string) string {
	return `<div id="` + name + `" ` + string(a.hub.View(r).Attrs("a")) + `>` + a.inner[name] + `</div>`
}

func (a *liveApp) handler(t *testing.T) http.Handler {
	mux := sizingMux(t, nil)
	stream := a.hub.Handler()
	mux.HandleFunc("GET /live", func(w http.ResponseWriter, r *http.Request) {
		if a.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		a.mu.Lock()
		a.streams = append(a.streams, r.URL.RawQuery)
		a.mu.Unlock()
		stream.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /away", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>away</title></head><body><p id="there">Away</p></body></html>`)
	})
	mux.Handle("GET /{$}", a.hub.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if names, ok := live.Wanted(r); ok {
			a.fragment(w, r, names)
			return
		}
		a.mu.Lock()
		var body strings.Builder
		if a.topNotice {
			body.WriteString(`<div id="notice" role="status"></div>`)
		}
		for _, n := range a.order {
			if _, ok := a.inner[n]; ok {
				body.WriteString(a.region(r, n))
			}
		}
		head := a.head
		a.mu.Unlock()
		// No Cache-Control: no-store here, unlike a real live page, or
		// Chromium would keep the page out of the back/forward cache
		// and the restore drive would test a reload instead.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>live</title>`+
			string(a.hub.View(r).Head(livePaused, "Could not confirm."))+head+
			`<script defer src="/live.js"></script></head><body>`+body.String()+
			`<a id="away" href="/away">Away</a></body></html>`)
	})))
	return mux
}

// fragment renders the regions asked for as they stand when the
// request arrives, then holds the answer if the drive asked it to: a
// held response is an old one by the time it lands.
func (a *liveApp) fragment(w http.ResponseWriter, r *http.Request, names []string) {
	a.mu.Lock()
	a.fragments = append(a.fragments, strings.Join(names, " "))
	var body strings.Builder
	var rendered []string
	for _, n := range names {
		if _, ok := a.inner[n]; ok {
			body.WriteString(a.region(r, n))
			rendered = append(rendered, n)
		}
	}
	held, release := a.hold > 0, a.release
	if held {
		a.hold--
	}
	a.inflight++
	a.most = max(a.most, a.inflight)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.inflight--
		a.mu.Unlock()
	}()
	if held {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	live.NoStore(w)
	live.Respond(w, r, rendered, []byte(body.String()))
}

func liveRig(t *testing.T, a *liveApp) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler { return a.handler(t) })
}

// liveEval evaluates expr, failing the test on error.
func liveEval(t *testing.T, rig *harness.Rig, expr string, out any) {
	t.Helper()
	raw, err := chromedp.Run(rig.Context(), chromedp.Evaluate[[]byte](expr))
	if err == nil && out != nil {
		err = json.Unmarshal(raw, out)
	}
	if err != nil {
		t.Fatalf("evaluating %s: %v", expr, err)
	}
}

// liveUntil polls a page expression until it is true. Evaluation
// errors count as not yet: a document being swapped in from the
// back/forward cache fails evaluations for a moment.
func liveUntil(t *testing.T, rig *harness.Rig, what, expr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		ctx, cancel := context.WithTimeout(rig.Context(), 2*time.Second)
		yes, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expr))
		cancel()
		if err == nil && yes {
			return
		}
		if time.Now().After(deadline) {
			body, _ := chromedp.Run(rig.Context(), chromedp.Evaluate[string](
				`document.body ? document.body.outerHTML : ""`))
			t.Fatalf("%s: not true within %s: %s\npage: %s", what, within, expr, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// liveServerUntil polls the app's own state the same way.
func liveServerUntil(t *testing.T, a *liveApp, what string, within time.Duration, f func(a *liveApp) bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !a.read(f) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not true within %s", what, within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func liveText(t *testing.T, rig *harness.Rig, sel string) string {
	t.Helper()
	var s string
	liveEval(t, rig, `(document.querySelector(`+liveQuote(sel)+`) || {}).textContent || ""`, &s)
	return s
}

func liveQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (a *liveApp) open(t *testing.T, rig *harness.Rig) {
	t.Helper()
	// A page in a background tab gets no focus events from focus() or
	// blur(), and leaving a field is what ends editing.
	focus := emulation.SetFocusEmulationEnabledParams{Enabled: true}
	rig.Run(send(emulation.SetFocusEmulationEnabled, focus),
		chromedp.Navigate(rig.Origin+"/"))
	liveServerUntil(t, a, "the page opened its stream", 10*time.Second, func(a *liveApp) bool { return len(a.streams) > 0 })
}

func TestALiveRegionSwapsOnAChangeEvent(t *testing.T) {
	a := newLiveApp(t, true, "list", `<p class="v">one</p>`)
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `document.getElementById("list").__old = true`, nil)

	a.change("list", `<p class="v">two</p>`)
	liveUntil(t, rig, "the region swapped", `document.querySelector("#list .v").textContent === "two"`, 10*time.Second)

	var got struct {
		Replaced bool
		Live     string
		Boot     string
	}
	liveEval(t, rig, `(() => { const l = document.getElementById("list");
	  return { Replaced: !l.__old, Live: l.getAttribute("data-live"), Boot: l.getAttribute("data-live-boot") }; })()`, &got)
	if !got.Replaced {
		t.Error("the region's element was edited in place, not replaced")
	}
	if got.Live != "a@1" || got.Boot != a.hub.Boot() {
		t.Errorf("the swapped region carries data-live %q boot %q, want a@1 and %q", got.Live, got.Boot, a.hub.Boot())
	}
	a.mu.Lock()
	fragments, streams := slices.Clone(a.fragments), slices.Clone(a.streams)
	a.mu.Unlock()
	if !slices.Equal(fragments, []string{"list"}) {
		t.Errorf("fragment requests %q, want one asking for list (never the notice)", fragments)
	}
	q, _ := url.ParseQuery(streams[0])
	if q.Get("boot") != a.hub.Boot() || !slices.Equal(q["t"], []string{"a@0"}) {
		t.Errorf("the stream was opened with %q, want boot=%s&t=a@0", streams[0], a.hub.Boot())
	}
	rig.Screen("#list", "a live region after a change event")
}

// Two orderings: a change that lands while its region's fetch is out
// waits behind it rather than racing it, and a response the page has
// abandoned (pagehide) never lands on top of the newer one fetched
// after the restore.
func TestAStaleResponseNeverOverwritesANewerOne(t *testing.T) {
	a := newLiveApp(t, true, "list", `<p class="v">one</p>`)
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `window.seen = []; new MutationObserver(() => {
	  const v = document.querySelector("#list .v"); if (v && v.textContent !== seen[seen.length - 1]) seen.push(v.textContent);
	}).observe(document.body, { childList: true, subtree: true })`, nil)

	a.holdNext(1)
	a.change("list", `<p class="v">two</p>`)
	liveServerUntil(t, a, "the first fetch is in flight", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	a.change("list", `<p class="v">three</p>`)
	time.Sleep(500 * time.Millisecond)
	var most, started int
	a.read(func(a *liveApp) bool { most, started = a.most, len(a.fragments); return true })
	if most != 1 || started != 1 {
		t.Errorf("a change during a fetch started another at once (%d in flight at most, %d started); want it queued behind the first", most, started)
	}
	a.letGo()
	liveUntil(t, rig, "the queued change applied", `document.querySelector("#list .v").textContent === "three"`, 10*time.Second)
	var seen []string
	liveEval(t, rig, `seen`, &seen)
	if !slices.Equal(seen, []string{"two", "three"}) {
		t.Errorf("the region showed %q in turn, want [two three]", seen)
	}

	a.holdNext(1)
	a.change("list", `<p class="v">four</p>`)
	liveServerUntil(t, a, "the doomed fetch is in flight", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	liveEval(t, rig, `dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true }))`, nil)
	a.change("list", `<p class="v">five</p>`)
	liveEval(t, rig, `dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true }))`, nil)
	liveUntil(t, rig, "the fetch after the restore applied", `document.querySelector("#list .v").textContent === "five"`, 10*time.Second)
	a.letGo()
	time.Sleep(500 * time.Millisecond)
	liveEval(t, rig, `seen`, &seen)
	if got := liveText(t, rig, "#list .v"); got != "five" || slices.Contains(seen, "four") {
		t.Errorf("after the abandoned response was released the region reads %q, having shown %q; want five, and four never", got, seen)
	}
	rig.Screen("#list", "a region after an abandoned fetch")
}

// liveForm is a region's inner HTML: a one-field form and a version
// line the drives read.
func liveForm(v string) string {
	return `<form action="/" method="post"><input id="field" name="q"></form><p class="v">` + v + `</p>`
}

// Someone typing in a region must not have it pulled from under them.
func TestARegionBeingEditedIsNotSwapped(t *testing.T) {
	form := liveForm
	a := newLiveApp(t, true, "list", form("one"))
	rig := liveRig(t, a)
	a.open(t, rig)
	stale := func(what string, want string) {
		t.Helper()
		liveUntil(t, rig, what+": marked stale", `document.getElementById("list").hasAttribute("data-live-stale")`, 10*time.Second)
		var kept bool
		liveEval(t, rig, `!!document.getElementById("list").__mark`, &kept)
		if got := liveText(t, rig, "#list .v"); got != want || !kept {
			t.Errorf("%s: the region reads %q (same element: %v), want %q left alone", what, got, kept, want)
		}
	}

	liveEval(t, rig, `document.getElementById("list").__mark = 1; document.getElementById("field").focus()`, nil)
	a.change("list", form("two"))
	stale("the field focused", "one")
	liveEval(t, rig, `document.getElementById("field").blur()`, nil)
	liveUntil(t, rig, "leaving the field swapped it", `document.querySelector("#list .v").textContent === "two" && !document.getElementById("list").hasAttribute("data-live-stale")`, 10*time.Second)

	// The fetch is already out when editing starts: the check runs again
	// immediately before the swap.
	liveEval(t, rig, `document.getElementById("list").__mark = 1`, nil)
	a.holdNext(1)
	a.change("list", form("three"))
	liveServerUntil(t, a, "the fetch is in flight", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	liveEval(t, rig, `document.getElementById("field").focus()`, nil)
	a.letGo()
	stale("focused while the fetch was out", "two")
	liveEval(t, rig, `document.getElementById("field").blur()`, nil)
	liveUntil(t, rig, "leaving the field swapped it", `document.querySelector("#list .v").textContent === "three"`, 10*time.Second)

	// Unsent input with nothing focused is still editing, and leaving
	// a field does not end it.
	liveEval(t, rig, `document.getElementById("list").__mark = 1; document.getElementById("field").value = "draft"`, nil)
	a.change("list", form("four"))
	stale("unsent input", "three")
	liveEval(t, rig, `document.getElementById("field").focus(); document.getElementById("field").blur()`, nil)
	time.Sleep(500 * time.Millisecond)
	var draft string
	liveEval(t, rig, `document.getElementById("field").value`, &draft)
	if got := liveText(t, rig, "#list .v"); got != "three" || draft != "draft" {
		t.Errorf("a form with unsent input was swapped: region %q, field %q; want three and draft", got, draft)
	}
	rig.Screen("#list", "a region being edited")
}

func TestSwapFocusFallsBackToTheRegion(t *testing.T) {
	a := newLiveApp(t, true, "list", `<button id="keep">Keep</button><button id="approve-1">Approve</button><p class="v">one</p>`)
	rig := liveRig(t, a)
	a.open(t, rig)
	type focus struct {
		ID, Tabindex string
		Old, Body    bool
	}
	read := func() focus {
		var f focus
		liveEval(t, rig, `(() => { const e = document.activeElement;
		  return { ID: e.id, Tabindex: e.getAttribute("tabindex") || "", Old: !!e.__old, Body: e === document.body }; })()`, &f)
		return f
	}

	liveEval(t, rig, `document.getElementById("keep").__old = true; document.getElementById("keep").focus()`, nil)
	a.change("list", `<button id="keep">Keep</button><button id="approve-2">Approve</button><p class="v">two</p>`)
	liveUntil(t, rig, "swap two", `document.querySelector("#list .v").textContent === "two"`, 10*time.Second)
	if f := read(); f.ID != "keep" || f.Old {
		t.Errorf("focus after a swap that kept the focused id: %+v, want the new #keep", f)
	}

	liveEval(t, rig, `document.getElementById("approve-2").focus()`, nil)
	a.change("list", `<button id="keep">Keep</button><p class="v">three</p>`)
	liveUntil(t, rig, "swap three", `document.querySelector("#list .v").textContent === "three"`, 10*time.Second)
	if f := read(); f.ID != "list" || f.Tabindex != "-1" || f.Body {
		t.Errorf("focus after the focused button was approved away: %+v, want the region itself with tabindex -1", f)
	}

	// The region itself focused: the replacement region takes it.
	liveEval(t, rig, `document.getElementById("list").__old = true`, nil)
	a.change("list", `<p class="v">four</p>`)
	liveUntil(t, rig, "swap four", `document.querySelector("#list .v").textContent === "four"`, 10*time.Second)
	if f := read(); f.ID != "list" || f.Old || f.Tabindex != "-1" {
		t.Errorf("focus after swapping a focused region: %+v, want the new region", f)
	}

	// Focus outside the region stays where it is.
	liveEval(t, rig, `document.getElementById("away").focus()`, nil)
	a.change("list", `<p class="v">five</p>`)
	liveUntil(t, rig, "swap five", `document.querySelector("#list .v").textContent === "five"`, 10*time.Second)
	if f := read(); f.ID != "away" {
		t.Errorf("a swap moved focus from outside the region to %+v", f)
	}
	rig.Screen("#list", "focus after swaps")
}

// The notice is a persistent role=status element: replacing it would
// drop what it is saying and what a screen reader is announcing. Here
// it sits inside a live region, so the swap is the only way it could
// be replaced.
func TestTheNoticeElementSurvivesASwap(t *testing.T) {
	a := newLiveApp(t, false, "main", `<div id="notice" role="status"></div><p class="v">one</p>`)
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `(() => { const n = document.getElementById("notice"); n.__mark = 7; n.textContent = "Saved."; })()`, nil)
	a.change("main", `<div id="notice" role="status"></div><p class="v">two</p>`)
	liveUntil(t, rig, "the region swapped", `document.querySelector("#main .v").textContent === "two"`, 10*time.Second)
	var got struct {
		Mark   int
		Text   string
		Inside bool
		Count  int
	}
	liveEval(t, rig, `(() => { const n = document.getElementById("notice");
	  return { Mark: n.__mark || 0, Text: n.textContent, Inside: document.getElementById("main").contains(n), Count: document.querySelectorAll("#notice").length }; })()`, &got)
	if got.Mark != 7 || got.Text != "Saved." || !got.Inside || got.Count != 1 {
		t.Errorf("after the swap the notice reads %+v, want the same element (mark 7), still saying Saved., inside the new region, once", got)
	}
	rig.Screen("#main", "a region holding the notice, after a swap")
}

func TestThePausedNoteShowsWhenTheStreamStaysDown(t *testing.T) {
	a := newLiveApp(t, true, "list", `<p class="v">one</p>`)
	a.down.Store(true)
	// The drives render the note an app's layout would: the English
	// catalog's, so a reworded note cannot leave them checking old text.
	if want := rastrillo.BaseCatalogs()["en"]["rastrillo.ui.live.paused"]; livePaused != want {
		t.Errorf("livePaused is %q, want the en catalog's %q", livePaused, want)
	}
	rig := liveRig(t, a)
	rig.Allow(http.MethodGet, "/live", http.StatusServiceUnavailable)
	start := time.Now()
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	liveUntil(t, rig, "the paused note", `document.getElementById("notice").textContent === `+liveQuote(livePaused), 14*time.Second)
	if took := time.Since(start); took < 9*time.Second {
		t.Errorf("the paused note showed after %s; a restart shorter than the 10s grace must not flash it", took)
	}
	a.down.Store(false)
	liveUntil(t, rig, "the note cleared once the stream opened", `document.getElementById("notice").textContent === ""`, 20*time.Second)
	if a.read(func(a *liveApp) bool { return len(a.streams) == 0 }) {
		t.Error("the note cleared without a stream opening")
	}
	rig.Screen("#list", "the page after the stream came back")
}

// Back from the back/forward cache, the page reopens its stream from
// the versions its regions hold, so it catches up on what changed
// while it was away.
func TestBackForwardRestoreCatchesUp(t *testing.T) {
	a := newLiveApp(t, true, "list", `<p class="v">one</p>`)
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `addEventListener("pageshow", e => { window.__restored = e.persisted; }); document.getElementById("away").click()`, nil)
	liveUntil(t, rig, "on the other page", `location.pathname === "/away" && !!document.getElementById("there")`, 10*time.Second)
	// The first stream ended on pagehide: the server sees no open stream
	// held by a page that is in the cache.
	a.change("list", `<p class="v">two</p>`)
	liveEval(t, rig, `setTimeout(() => history.back(), 0)`, nil)
	liveUntil(t, rig, "back on the live page", `location.pathname === "/" && window.__restored !== undefined`, 10*time.Second)
	var restored bool
	liveEval(t, rig, `window.__restored`, &restored)
	if !restored {
		var why string
		liveEval(t, rig, `JSON.stringify((performance.getEntriesByType("navigation")[0] || {}).notRestoredReasons || null)`, &why)
		t.Fatalf("going back reloaded the page instead of restoring it from the back/forward cache (%s), so this drive proves nothing about pageshow", why)
	}
	liveUntil(t, rig, "the restored page caught up", `document.querySelector("#list .v").textContent === "two"`, 10*time.Second)
	a.mu.Lock()
	streams := slices.Clone(a.streams)
	a.mu.Unlock()
	if len(streams) != 2 {
		t.Fatalf("stream requests %q, want two: one before leaving, one after the restore", streams)
	}
	if q, _ := url.ParseQuery(streams[1]); !slices.Equal(q["t"], []string{"a@0"}) {
		t.Errorf("the restored page reopened its stream with %q, want t=a@0, the version its region held", streams[1])
	}
	// A restored document raises no DOM.documentUpdated, so chromedp's
	// node cache still describes /away and Screen's selector wait would
	// hang. A fresh load gives it a document to read; the problems it
	// flushes are the whole drive's.
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#list", "a live page restored from the back/forward cache")
}

// liveSeam is a <head> script that makes live.js export its send seam,
// through the same module guard select.js uses for Node. Task 6's forms
// call it from inside the file; this is how a drive calls it before
// they exist.
const liveSeam = `<script>var module = { exports: {} };</script>`

// A form's own answer swaps the form it was sent from. That form holds
// what was just typed, and Enter leaves its field focused, so the
// stream's edit check would discard every form answer as stale.
func TestAFormsOwnAnswerSwapsDespiteUnsentInput(t *testing.T) {
	a := newLiveApp(t, true, "list", liveForm("one"))
	a.head = liveSeam
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `{ const f = document.getElementById("field"); f.value = "draft"; f.focus() }`, nil)
	a.set("list", liveForm("two"))
	liveEval(t, rig, `module.exports.send("/", ["list"], {}, "list"); true`, nil)
	liveUntil(t, rig, "the form's answer swapped", `document.querySelector("#list .v").textContent === "two"`, 10*time.Second)
	var got struct {
		Stale bool
		Focus string
		Value string
	}
	liveEval(t, rig, `({ Stale: document.getElementById("list").hasAttribute("data-live-stale"),
	  Focus: document.activeElement.id, Value: document.getElementById("field").value })`, &got)
	if got.Stale || got.Focus != "field" || got.Value != "" {
		t.Errorf("after a form's answer: %+v, want the region swapped (not stale), focus on the new field, its value the answer's", got)
	}

	// The same answer through the stream's path is held back: naming the
	// form's region is the whole difference.
	liveEval(t, rig, `document.getElementById("field").value = "draft"`, nil)
	a.set("list", liveForm("three"))
	liveEval(t, rig, `module.exports.send("/", ["list"], {}, null); true`, nil)
	liveUntil(t, rig, "the stream's answer held back", `document.getElementById("list").hasAttribute("data-live-stale")`, 10*time.Second)
	if v := liveText(t, rig, "#list .v"); v != "two" {
		t.Errorf("a stream answer swapped a region holding unsent input: it reads %q, want two", v)
	}
	rig.Screen("#list", "a form's answer and a stream's")
}

// A stream fetch answered "navigate" (the page no longer has that
// region) must not throw away what someone is typing elsewhere on the
// page. It waits, as a stale region does, until the editing ends.
func TestANavigationWaitsWhileThePageIsEdited(t *testing.T) {
	a := newLiveApp(t, true, "list", `<p class="v">one</p>`, "edit", liveForm("e"))
	rig := liveRig(t, a)
	a.open(t, rig)
	liveEval(t, rig, `window.__here = 1; document.getElementById("field").value = "draft"`, nil)
	a.remove("list")
	liveUntil(t, rig, "the navigating region held back", `document.getElementById("list").hasAttribute("data-live-stale")`, 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	var here bool
	var draft string
	liveEval(t, rig, `window.__here === 1`, &here)
	liveEval(t, rig, `document.getElementById("field").value`, &draft)
	if !here || draft != "draft" {
		t.Fatalf("a background navigation went ahead over unsent input (still here: %v, field %q)", here, draft)
	}
	if !a.read(func(a *liveApp) bool { return slices.Contains(a.fragments, "list") }) {
		t.Fatal("the list region was never fetched, so the drive did not reach the navigation")
	}

	// Leaving some other control with the draft still unsent is not the
	// end of the editing: the held-back region waits on the whole page,
	// not on itself, so it costs no fetch.
	count := func() int {
		n := 0
		a.read(func(a *liveApp) bool {
			for _, f := range a.fragments {
				if f == "list" {
					n++
				}
			}
			return true
		})
		return n
	}
	before := count()
	liveEval(t, rig, `{ const away = document.getElementById("away"); away.focus(); away.blur() }`, nil)
	time.Sleep(300 * time.Millisecond)
	if n := count(); n != before {
		t.Errorf("a focusout with the draft still unsent fetched the held-back region again (%d fetches, was %d)", n, before)
	}

	liveEval(t, rig, `{ const f = document.getElementById("field"); f.value = ""; f.focus(); f.blur() }`, nil)
	liveUntil(t, rig, "navigated once the editing ended", `window.__here === undefined && document.readyState === "complete" && !document.getElementById("list") && !!document.getElementById("edit")`, 10*time.Second)
	rig.Screen("#edit", "the page navigated to after the editing ended")
}
