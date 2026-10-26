//go:build browser

package ui

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/live"
)

// formPost is one form submission as the server received it.
type formPost struct {
	Method, Path, Fragment, Page, Type string
	Form                               url.Values
}

// formApp is liveApp with somewhere to post: every POST, and every GET
// of /search, is recorded and then answered by answer, which a drive
// sets for what it is checking. The default saves the way a handler
// does today: write, set the flash, 303 back to the page.
type formApp struct {
	*liveApp
	posts  []formPost // under liveApp.mu
	answer func(w http.ResponseWriter, r *http.Request)
}

func newFormApp(t *testing.T, regions ...string) *formApp {
	t.Helper()
	f := &formApp{liveApp: newLiveApp(t, true, regions...)}
	f.answer = f.save
	return f
}

// save writes q into list (and into other, when the page has it) as
// a handler's write would, without Notify, sets the flash, and
// redirects back to the page.
func (f *formApp) save(w http.ResponseWriter, r *http.Request) {
	q := r.PostForm.Get("q")
	f.set("list", swapForm(q))
	f.mu.Lock()
	_, other := f.inner["other"]
	f.mu.Unlock()
	if other {
		f.set("other", `<p class="o">`+q+`</p>`)
	}
	f.set("notice", "Saved.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (f *formApp) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", f.liveApp.handler(t))
	record := func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil && err != http.ErrNotMultipart {
			_ = r.ParseForm()
		}
		form := r.PostForm
		if r.Method == http.MethodGet {
			form = r.URL.Query()
		}
		f.mu.Lock()
		f.posts = append(f.posts, formPost{
			Method: r.Method, Path: r.URL.Path, Form: form,
			Fragment: r.Header.Get("Rastrillo-Fragment"), Page: r.Header.Get("Rastrillo-Page"),
			Type: strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0],
		})
		answer := f.answer
		f.mu.Unlock()
		answer(w, r)
	}
	mux.HandleFunc("POST /", record)
	mux.HandleFunc("GET /search", record)
	// Another page of the app: a form's redirect that lands here is
	// answered "navigate", as live.Elsewhere answers it in an app.
	mux.HandleFunc("GET /there", func(w http.ResponseWriter, r *http.Request) {
		if live.Elsewhere(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>there</title></head><body><p id="there">There</p></body></html>`)
	})
	// A page with no rastrillo-live meta: live.js stays inert, so its
	// swapping form is an ordinary one and busy.js's to guard.
	mux.HandleFunc("GET /plain", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>plain</title>`+
			`<script defer src="/busy.js"></script><script defer src="/live.js"></script></head><body>`+
			`<div id="list" data-live="a@0"><form id="f" method="post" action="/plain-save" data-live-form>`+
			`<button id="go" name="do" value="a">Go</button></form></div></body></html>`)
	})
	return mux
}

func formRig(t *testing.T, f *formApp) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler { return f.handler(t) })
}

func (f *formApp) sent() []formPost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.posts)
}

// swapForm is the list region's inner HTML: a field, three ways to
// submit it, and the version line the drives read.
func swapForm(v string) string {
	return `<form id="f" method="post" action="/save" data-live-form="other">` +
		`<input id="field" name="q">` +
		`<button id="go" name="do" value="a">A</button>` +
		`<button id="b" name="do" value="b">B</button>` +
		`<button id="c" name="do" value="c" formaction="/save-c" formenctype="multipart/form-data">C</button>` +
		`</form><p class="v">` + v + `</p>`
}

// formErrors records what the page threw, for the drive that cannot
// call Screen: a dropped connection is a failed request, which the rig
// counts as a problem whatever the page did about it.
const formErrors = `<script>window.__errs = [];
addEventListener("error", e => __errs.push(String(e.message)));
addEventListener("unhandledrejection", e => __errs.push(String(e.reason)));</script>`

func TestASwappingFormSwapsItsRegionAndNotice(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"), "other", `<p class="o">one</p>`)
	rig := formRig(t, f)
	f.open(t, rig)
	liveEval(t, rig, `window.__here = 1; document.getElementById("notice").__mark = 7;
	  document.getElementById("field").value = "two";
	  new MutationObserver(() => { const v = document.querySelector("#list .v");
	    if (v && v.textContent === "two" && !window.__swapped) window.__swapped = performance.now(); })
	    .observe(document.body, { childList: true, subtree: true });
	  window.__t0 = performance.now(); true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "the form's regions and the notice swapped", `document.querySelector("#list .v").textContent === "two" &&
	  document.querySelector("#other .o").textContent === "two" && document.getElementById("notice").textContent === "Saved."`, 10*time.Second)

	var got struct {
		Here, Mark, Took float64
		Focus, Busy      string
	}
	liveEval(t, rig, `({ Here: window.__here || 0, Mark: document.getElementById("notice").__mark || 0,
	  Took: window.__swapped - window.__t0, Focus: document.activeElement.id,
	  Busy: document.getElementById("f").getAttribute("aria-busy") || "" })`, &got)
	if got.Here != 1 {
		t.Error("the page navigated; a swapping form's answer should swap in place")
	}
	if got.Mark != 7 {
		t.Error("the notice element was replaced; its contents must change in place to be announced")
	}
	if got.Took < 640 {
		t.Errorf("the answer swapped in %.0fms; the busy state holds at least 650ms", got.Took)
	}
	if got.Focus != "go" || got.Busy != "" {
		t.Errorf("after the swap focus is on %q and the form is aria-busy %q; want the new #go, not busy", got.Focus, got.Busy)
	}
	posts := f.sent()
	if len(posts) != 1 {
		t.Fatalf("the server saw %d submissions, want 1: %+v", len(posts), posts)
	}
	p := posts[0]
	if p.Path != "/save" || p.Fragment != "list other notice" || p.Page != "/" || p.Form.Get("q") != "two" || p.Type != "application/x-www-form-urlencoded" {
		t.Errorf("the form sent %+v; want POST /save, Rastrillo-Fragment \"list other notice\", Rastrillo-Page \"/\", q=two, urlencoded", p)
	}
	rig.Screen("#list", "a swapping form after its answer")
}

func TestASwappingFormSendsItsSubmitter(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	f.open(t, rig)
	rig.Run(chromedp.Click("#b", chromedp.ByID))
	liveUntil(t, rig, "the B submit swapped", `document.getElementById("notice").textContent === "Saved."`, 10*time.Second)
	liveEval(t, rig, `document.getElementById("notice").textContent = ""; document.getElementById("field").value = "see"; true`, nil)
	rig.Run(chromedp.Click("#c", chromedp.ByID))
	liveUntil(t, rig, "the C submit swapped", `document.querySelector("#list .v").textContent === "see"`, 10*time.Second)

	posts := f.sent()
	if len(posts) != 2 {
		t.Fatalf("the server saw %d submissions, want 2: %+v", len(posts), posts)
	}
	if got := posts[0].Form["do"]; !slices.Equal(got, []string{"b"}) {
		t.Errorf("clicking B sent do=%q, want only b: the submitter's own name and value", got)
	}
	if c := posts[1]; c.Path != "/save-c" || c.Type != "multipart/form-data" || c.Form.Get("do") != "c" || c.Form.Get("q") != "see" || c.Fragment != "list other notice" {
		t.Errorf("clicking C sent %+v; want its formaction /save-c, its formenctype multipart, do=c, q=see, and the regions", c)
	}
	rig.Screen("#list", "a swapping form after two submitters")
}

func TestValidationErrorsSwapInPlaceAndTakeFocus(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		f.set("list", `<form id="f" method="post" action="/save" data-live-form>`+
			`<p id="summary">2 problems</p>`+
			`<input id="name" name="n" value="ok">`+
			`<input id="field" name="q" aria-invalid="true">`+
			`<input id="later" name="z" aria-invalid="true">`+
			`<button id="go">A</button></form><p class="v">invalid</p>`)
		f.mu.Lock()
		body := f.region(r, "list")
		f.mu.Unlock()
		live.Answer(w, r, []string{"list"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, body)
	}
	rig := formRig(t, f)
	rig.Allow(http.MethodPost, "/save", http.StatusUnprocessableEntity)
	f.open(t, rig)
	liveEval(t, rig, `window.__here = 1`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "the errors swapped in", `document.querySelector("#list .v").textContent === "invalid"`, 10*time.Second)
	var got struct {
		Here  float64
		Focus string
	}
	liveEval(t, rig, `({ Here: window.__here || 0, Focus: document.activeElement.id })`, &got)
	if got.Here != 1 || got.Focus != "field" {
		t.Errorf("after a 422: still here %v, focus on %q; want the page kept and focus on #field, the first invalid field", got.Here == 1, got.Focus)
	}
	rig.Screen("#list", "a swapping form showing its errors")
}

func TestAnUnknownOutcomeKeepsInputAndNeverResubmits(t *testing.T) {
	const unconfirmed = "Could not confirm."
	f := newFormApp(t, "list", swapForm("one"))
	f.head = formErrors
	rig := formRig(t, f)
	rig.Allow(http.MethodPost, "/save", http.StatusServiceUnavailable)
	f.open(t, rig)
	check := func(leg string, posts int) {
		t.Helper()
		liveUntil(t, rig, leg+": the unconfirmed note", `document.getElementById("notice").textContent === `+liveQuote(unconfirmed), 10*time.Second)
		// Long enough for a retry, or the page's own resubmit, to show.
		time.Sleep(time.Second)
		var got struct {
			Value, Busy string
			Disabled    bool
			Stale       bool
			Errs        []string
		}
		liveEval(t, rig, `({ Value: document.getElementById("field").value,
		  Busy: document.getElementById("go").getAttribute("aria-busy") || "", Disabled: document.getElementById("go").disabled,
		  Stale: document.getElementById("list").hasAttribute("data-live-stale"), Errs: window.__errs })`, &got)
		if n := len(f.sent()); n != posts {
			t.Errorf("%s: the server saw %d submissions, want %d: an unknown outcome is never resubmitted", leg, n, posts)
		}
		if got.Value != "keep me" {
			t.Errorf("%s: the field holds %q, want what was typed kept", leg, got.Value)
		}
		if got.Busy != "" || got.Disabled {
			t.Errorf("%s: the button is aria-busy %q, disabled %v; want it handed back so the person can try again", leg, got.Busy, got.Disabled)
		}
		// The regions are fetched again, and held back while the typed
		// input is unsent.
		if !got.Stale {
			t.Errorf("%s: the form's region was not fetched again (or was swapped over the input)", leg)
		}
		if len(got.Errs) > 0 {
			t.Errorf("%s: the page threw %q", leg, got.Errs)
		}
	}

	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream gone", http.StatusServiceUnavailable)
	}
	liveEval(t, rig, `document.getElementById("field").value = "keep me"; true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	check("a 503 without regions", 1)
	rig.Screen("#list", "a swapping form whose outcome is unknown")

	// The POST is recorded, so it may have committed; then the
	// connection drops partway through the answer. Not before its first
	// byte: Chromium itself resends a request whose reused connection
	// closes unanswered, which would count here as a resubmit the page
	// never made. A dropped request is a problem to the rig whatever the
	// page does, so this leg ends on the page's own error record, not on
	// Screen.
	f.mu.Lock()
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nRastrillo-Regions: list notice\r\nContent-Length: 4096\r\n\r\n<div id=\"list\">")
		buf.Flush()
		conn.Close()
	}
	f.mu.Unlock()
	liveEval(t, rig, `document.getElementById("notice").textContent = ""; document.getElementById("list").removeAttribute("data-live-stale"); true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	check("a dropped connection", 2)
}

func TestASecondSubmitWhileOutIsRefused(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	f.open(t, rig)
	f.holdNext(1)
	// Twice in one task: the second click lands before the button is
	// disabled, so only the guard can refuse it.
	liveEval(t, rig, `{ const b = document.getElementById("go"); b.click(); b.click(); } true`, nil)
	liveServerUntil(t, f.liveApp, "the answer is held", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	liveEval(t, rig, `document.getElementById("f").requestSubmit(); true`, nil)
	time.Sleep(300 * time.Millisecond)
	if n := len(f.sent()); n != 1 {
		t.Errorf("while the first was out the server saw %d submissions, want 1", n)
	}
	f.letGo()
	liveUntil(t, rig, "the answer swapped", `document.getElementById("notice").textContent === "Saved."`, 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	if n := len(f.sent()); n != 1 {
		t.Errorf("after the answer the server had seen %d submissions, want 1", n)
	}
	rig.Screen("#list", "a swapping form after a refused second submit")
}

// busy.js hands a swapping form back the tick after a script cancels
// its submit, which would clear live.js's busy state mid-request. Its
// listener can run before live.js's or after, by script order, so
// both orders are driven.
func TestBusyLeavesSwappingFormsToLive(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	for _, order := range []struct{ name, head string }{
		{"busy.js before live.js", `<script src="/busy.js"></script><script>window.__busy = 1</script>`},
		{"busy.js after live.js", `<script>document.addEventListener("DOMContentLoaded", () => {
		  const s = document.createElement("script"); s.src = "/busy.js"; s.onload = () => { window.__busy = 1; }; document.head.append(s); });</script>`},
	} {
		f.mu.Lock()
		f.head, f.posts, f.streams = order.head, nil, nil
		f.inner["list"] = swapForm("one")
		f.mu.Unlock()
		f.open(t, rig)
		liveUntil(t, rig, order.name+": busy.js loaded", `window.__busy === 1`, 10*time.Second)
		f.holdNext(1)
		rig.Run(chromedp.Click("#go", chromedp.ByID))
		liveServerUntil(t, f.liveApp, order.name+": the answer is held", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
		// Past busy.js's hand-back tick and its 650ms hold.
		time.Sleep(900 * time.Millisecond)
		var got struct {
			Busy, Form string
			Disabled   bool
			Spinners   int
		}
		liveEval(t, rig, `(() => { const b = document.getElementById("go");
		  return { Busy: b.getAttribute("aria-busy") || "", Form: document.getElementById("f").getAttribute("aria-busy") || "",
		    Disabled: b.disabled, Spinners: b.querySelectorAll("[rst-spin]").length }; })()`, &got)
		if got.Busy != "true" || got.Form != "true" || !got.Disabled || got.Spinners != 1 {
			t.Errorf("%s: mid-request the button reads %+v; want aria-busy, its form aria-busy, disabled, one spinner", order.name, got)
		}
		f.letGo()
		liveUntil(t, rig, order.name+": the answer swapped", `document.getElementById("notice").textContent === "Saved."`, 10*time.Second)
		time.Sleep(800 * time.Millisecond)
		if posts := f.sent(); len(posts) != 1 || posts[0].Fragment == "" {
			t.Errorf("%s: the server saw %+v; want one swapping submission, none sent again by busy.js", order.name, posts)
		}
		rig.Screen("#list", order.name+": a swapping form after its answer")
	}

	// live.js inert (no meta): the form is ordinary, and busy.js's.
	f.mu.Lock()
	f.posts = nil
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>sent</title></head><body><p id="sent">Sent</p></body></html>`)
	}
	f.mu.Unlock()
	rig.Run(chromedp.Navigate(rig.Origin + "/plain"))
	liveEval(t, rig, `window.__seen = false; new MutationObserver(() => {
	  if (document.getElementById("go").getAttribute("aria-busy") === "true") window.__seen = true;
	}).observe(document.getElementById("go"), { attributes: true }); true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "the plain form went busy", `window.__seen === true || !!document.getElementById("sent")`, 10*time.Second)
	var seen bool
	liveEval(t, rig, `window.__seen === true`, &seen)
	liveUntil(t, rig, "the plain form submitted natively", `!!document.getElementById("sent")`, 10*time.Second)
	if posts := f.sent(); !seen || len(posts) != 1 || posts[0].Fragment != "" {
		t.Errorf("with live.js inert the button went busy: %v, and the server saw %+v; want busy.js's busy state and one native POST", seen, posts)
	}
	rig.Screen("#sent", "a swapping form on a page live.js leaves alone")
}

// A form live.js cannot send faithfully is sent by the browser: no
// fetch, no Rastrillo-Fragment, the page navigates (or a tab opens) as
// it always did.
func TestFormsLiveCannotSendSubmitNatively(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>sent</title></head><body><p id="sent">Sent</p></body></html>`)
	}
	cross := strings.Replace(rig.Origin, "localhost", "127.0.0.1", 1) + "/save"
	for _, leg := range []struct{ name, form, method, path string }{
		{"a target", `<form id="f" method="post" action="/save" target="_blank" data-live-form><input name="q" value="x"><button id="go">Go</button></form>`, "POST", "/save"},
		{"a GET form", `<form id="f" action="/search" data-live-form><input name="q" value="x"><button id="go">Go</button></form>`, "GET", "/search"},
		{"a formmethod=get submitter", `<form id="f" method="post" action="/search" data-live-form><input name="q" value="x"><button id="go" formmethod="get">Go</button></form>`, "GET", "/search"},
		{"a cross-origin action", `<form id="f" method="post" action="` + cross + `" data-live-form><input name="q" value="x"><button id="go">Go</button></form>`, "POST", "/save"},
		{"a text/plain encoding", `<form id="f" method="post" action="/save" enctype="text/plain" data-live-form><input name="q" value="x"><button id="go">Go</button></form>`, "POST", "/save"},
	} {
		f.mu.Lock()
		f.posts, f.streams = nil, nil
		f.inner["list"] = leg.form
		f.mu.Unlock()
		f.open(t, rig)
		rig.Run(chromedp.Click("#go", chromedp.ByID))
		liveServerUntil(t, f.liveApp, leg.name+": the server got the submission", 10*time.Second, func(*liveApp) bool { return len(f.posts) > 0 })
		time.Sleep(200 * time.Millisecond)
		posts := f.sent()
		if len(posts) != 1 || posts[0].Method != leg.method || posts[0].Path != leg.path || posts[0].Fragment != "" || posts[0].Page != "" {
			t.Errorf("%s: the server saw %+v; want one native %s %s with no Rastrillo-Fragment or Rastrillo-Page", leg.name, posts, leg.method, leg.path)
		}
	}
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#list", "the page after native submissions")
}

// A form's answer must outrank the stream in both orders: a stream
// fetch already out when the form is sent is superseded (the answer
// that started last applies), and a change that arrives while the
// form's fetch is out waits behind it rather than overtaking it.
func TestAFormsAnswerOutranksTheStream(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	f.open(t, rig)
	liveEval(t, rig, `window.seen = []; new MutationObserver(() => {
	  const v = document.querySelector("#list .v"); if (v && v.textContent !== seen[seen.length - 1]) seen.push(v.textContent);
	}).observe(document.body, { childList: true, subtree: true }); true`, nil)

	f.holdNext(1)
	f.change("list", swapForm("stream"))
	liveServerUntil(t, f.liveApp, "the stream's fetch is out", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	liveEval(t, rig, `document.getElementById("field").value = "form"; true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "the form's answer applied", `document.querySelector("#list .v").textContent === "form"`, 10*time.Second)
	f.letGo()
	time.Sleep(500 * time.Millisecond)
	var seen []string
	liveEval(t, rig, `seen`, &seen)
	if v := liveText(t, rig, "#list .v"); v != "form" || slices.Contains(seen, "stream") {
		t.Errorf("after the older stream fetch answered the region reads %q, having shown %q; want form, and stream never", v, seen)
	}

	f.mu.Lock()
	f.most = f.inflight
	f.mu.Unlock()
	f.holdNext(1)
	liveEval(t, rig, `seen = ["form"]; document.getElementById("field").value = "form2"; true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveServerUntil(t, f.liveApp, "the form's answer is held", 10*time.Second, func(a *liveApp) bool { return a.inflight == 1 })
	f.change("list", swapForm("stream2"))
	time.Sleep(500 * time.Millisecond)
	var most int
	f.read(func(a *liveApp) bool { most = a.most; return true })
	if most != 1 {
		t.Errorf("a change during the form's fetch started a stream fetch beside it (%d in flight at most); want it queued behind", most)
	}
	f.letGo()
	liveUntil(t, rig, "the queued change applied", `document.querySelector("#list .v").textContent === "stream2"`, 10*time.Second)
	liveEval(t, rig, `seen`, &seen)
	if !slices.Equal(seen, []string{"form", "form2", "stream2"}) {
		t.Errorf("the region showed %q in turn, want [form form2 stream2]", seen)
	}
	rig.Screen("#list", "a swapping form beside the stream")
}

// An answer without the regions is shown as the browser would have
// shown it: a whole page replaces this one, and "navigate" navigates.
func TestAFormAnswerWithoutItsRegionsIsShownAsTheBrowserWould(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	rig := formRig(t, f)
	rig.Allow(http.MethodPost, "/save", http.StatusForbidden)
	for _, leg := range []struct {
		name, check string
		answer      func(w http.ResponseWriter, r *http.Request)
	}{
		{"a whole page", `location.pathname === "/" && !!document.getElementById("whole") && !document.getElementById("list")`,
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>no</title></head><body><p id="whole">Not yours</p></body></html>`)
			}},
		{"a redirect to a page without regions", `location.pathname === "/away" && !!document.getElementById("there") && !document.getElementById("list")`,
			func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/away", http.StatusSeeOther) }},
		{"a redirect to another page", `location.pathname === "/there" && !!document.getElementById("there") && !document.getElementById("list")`,
			func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/there", http.StatusSeeOther) }},
	} {
		f.mu.Lock()
		f.answer, f.streams = leg.answer, nil
		f.mu.Unlock()
		f.open(t, rig)
		rig.Run(chromedp.Click("#go", chromedp.ByID))
		liveUntil(t, rig, leg.name, leg.check, 10*time.Second)
	}
	var navigated bool
	liveEval(t, rig, `performance.getEntriesByType("navigation")[0].name.endsWith("/there")`, &navigated)
	if !navigated {
		t.Error("the 204 and Rastrillo-Location answer did not navigate to /there")
	}

	// The form that navigated was left busy, as a native submit's page
	// is. Back from the back/forward cache, it is handed back.
	f.mu.Lock()
	f.answer = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/there", http.StatusSeeOther) }
	f.mu.Unlock()
	f.open(t, rig)
	liveEval(t, rig, `addEventListener("pageshow", e => { window.__restored = e.persisted; }); true`, nil)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "navigated to /there", `location.pathname === "/there" && !!document.getElementById("there")`, 10*time.Second)
	liveEval(t, rig, `setTimeout(() => history.back(), 0); true`, nil)
	liveUntil(t, rig, "back on the form's page", `location.pathname === "/" && window.__restored !== undefined`, 10*time.Second)
	var back struct {
		Restored, Disabled bool
		Busy, Form         string
	}
	liveEval(t, rig, `({ Restored: window.__restored, Disabled: document.getElementById("go").disabled,
	  Busy: document.getElementById("go").getAttribute("aria-busy") || "", Form: document.getElementById("f").getAttribute("aria-busy") || "" })`, &back)
	if !back.Restored {
		t.Fatal("going back reloaded the page instead of restoring it, so this leg proves nothing about pageshow")
	}
	if back.Disabled || back.Busy != "" || back.Form != "" {
		t.Errorf("restored from the back/forward cache the form reads %+v; want it handed back", back)
	}
	// A restored document leaves chromedp's node cache stale (see
	// TestBackForwardRestoreCatchesUp): a fresh load before Screen.
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#list", "the page after answers without regions")
}

// A region held back while the page is edited waits for a focusout.
// A form's swap can end the editing by replacing the focused element,
// which then never fires one, so the swap must re-check.
func TestAFormSwapReleasesARegionWaitingOnEditing(t *testing.T) {
	f := newFormApp(t, "gone", `<p class="g">g</p>`, "list", swapForm("one"))
	rig := formRig(t, f)
	f.open(t, rig)
	liveEval(t, rig, `window.__here = 1; document.getElementById("field").value = "draft"; true`, nil)
	f.remove("gone")
	liveUntil(t, rig, "the navigating region held back", `document.getElementById("gone").hasAttribute("data-live-stale")`, 10*time.Second)
	rig.Run(chromedp.Click("#go", chromedp.ByID))
	liveUntil(t, rig, "navigated once the form's swap ended the editing",
		`window.__here === undefined && document.readyState === "complete" && !document.getElementById("gone") && !!document.getElementById("list")`, 10*time.Second)
	rig.Screen("#list", "the page after a form's swap released a held navigation")
}

// A handler on the form runs after live.js's capture listener, and the
// browser sends nothing for a submit it cancels: a Cancel on a confirm
// must not delete the row, and a handler doing its own fetch must not
// post twice. Driven in both script orders, since busy.js reads the
// same submit.
func TestASwappingFormDefersToAHandlerThatCancels(t *testing.T) {
	f := newFormApp(t, "list", swapForm("one"))
	f.answer = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	rig := formRig(t, f)
	for _, order := range []struct{ name, head string }{
		{"no busy.js", `<script>window.__busy = 1</script>`},
		{"busy.js before live.js", `<script src="/busy.js"></script><script>window.__busy = 1</script>`},
		{"busy.js after live.js", `<script>document.addEventListener("DOMContentLoaded", () => {
		  const s = document.createElement("script"); s.src = "/busy.js"; s.onload = () => { window.__busy = 1; }; document.head.append(s); });</script>`},
	} {
		for _, leg := range []struct {
			name, handler string
			posts         int
		}{
			{"a confirm answered Cancel", `e.preventDefault()`, 0},
			{"a handler doing its own fetch", `e.preventDefault(); fetch("/own", { method: "POST", body: new FormData(f) })`, 1},
		} {
			name := order.name + ", " + leg.name
			f.mu.Lock()
			f.head, f.posts, f.streams = order.head, nil, nil
			f.mu.Unlock()
			f.open(t, rig)
			liveUntil(t, rig, name+": scripts loaded", `window.__busy === 1`, 10*time.Second)
			liveEval(t, rig, `{ const f = document.getElementById("f"); f.addEventListener("submit", e => { `+leg.handler+` }); } true`, nil)
			rig.Run(chromedp.Click("#go", chromedp.ByID))
			// Past live.js's hold and busy.js's, so a send or a hand-back
			// either would make has happened.
			time.Sleep(1200 * time.Millisecond)
			var got struct {
				Busy, Form string
				Disabled   bool
				Spinners   int
			}
			liveEval(t, rig, `(() => { const b = document.getElementById("go");
			  return { Busy: b.getAttribute("aria-busy") || "", Form: document.getElementById("f").getAttribute("aria-busy") || "",
			    Disabled: b.disabled, Spinners: b.querySelectorAll("[rst-spin]").length }; })()`, &got)
			posts := f.sent()
			if len(posts) != leg.posts {
				t.Errorf("%s: the server saw %d requests %+v, want %d", name, len(posts), posts, leg.posts)
			}
			for _, p := range posts {
				if p.Path != "/own" || p.Fragment != "" {
					t.Errorf("%s: the server saw %+v; only the handler's own request should go", name, p)
				}
			}
			if got.Busy != "" || got.Form != "" || got.Disabled || got.Spinners != 0 {
				t.Errorf("%s: after a cancelled submit the button reads %+v; want no busy state left", name, got)
			}
		}
	}
	rig.Screen("#list", "a swapping form whose submit a handler cancelled")
}
