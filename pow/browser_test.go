//go:build browser

// The gate the whole scheme rests on.
//
// A Go-only test proves the verifier and nothing else. The failure it
// cannot see is the one that actually happens: the browser and the
// server building a different preimage from the same inputs — a stray
// Unicode case fold, a different separator, a counter formatted another
// way — so every honest visitor's solution is rejected as too short,
// with no error they can read and nothing in the logs to explain it.
// The only way to know the two agree is to run the shipped module in a
// real browser and check its answer here.
//
// This is also the test that makes shipping both halves from one module
// worth something. Vendored into an app, these files could drift from
// the verifier they are checked against and nothing would notice.
//
// Run it with:
//
//	go test -tags browser ./pow/

package pow_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/ui"
)

// awaitPromise makes Evaluate wait for the expression's promise instead
// of handing back a pending object. Every script here uses dynamic
// import, which is asynchronous by nature. chromedp exports no such
// option of its own.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// powRig serves the package's own browser assets and one blank page to
// run them from — nothing else, because nothing else is under test.
func powRig(t *testing.T) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler {
		assets := rastrillo.NewAssets(pow.Assets())
		mux := http.NewServeMux()
		mux.Handle("GET /pow/", http.StripPrefix("/pow/", assets.Handler()))
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<!doctype html><title>pow</title><main>ready</main>"))
		})
		return mux
	})
}

// browserSolve runs the shipped solver in the page and returns its
// counter.
const browserSolve = `(async () => {
	const m = await import("/pow/powcore.js");
	return m.solve(%q, %q, %d, null);
})()`

// browserDifficulty is low deliberately: the property under test is
// agreement, not cost. A realistic 18 bits would make this take as long
// as a real submission for no extra information.
const browserDifficulty = 12

const testNonce = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"

func TestBrowserSolutionSatisfiesGoVerifier(t *testing.T) {
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	// Mixed case and surrounding space, because the normaliser is
	// exactly where the two sides disagree if they are going to.
	const binding = "  Alice@Example.COM "

	var counter string
	rig.Run(chromedp.Evaluate(
		fmt.Sprintf(browserSolve, testNonce, binding, browserDifficulty),
		&counter, awaitPromise))

	if counter == "" {
		t.Fatal("the browser returned no counter")
	}
	if !pow.Verify(testNonce, binding, counter, browserDifficulty) {
		t.Fatalf("the browser solved %q for %q but Go rejects it: the two sides "+
			"are building different preimages", counter, binding)
	}
}

func TestBrowserSolutionDoesNotTransfer(t *testing.T) {
	// The binding is inside the hash, and this is what that buys. If it
	// were ever dropped — or built differently on the two sides — one
	// solve would buy every address in a list.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	var counter string
	rig.Run(chromedp.Evaluate(
		fmt.Sprintf(browserSolve, testNonce, "alice@example.com", browserDifficulty),
		&counter, awaitPromise))

	if pow.Verify(testNonce, "bob@example.com", counter, browserDifficulty) {
		t.Fatal("a solution the browser found for one address verified for another")
	}
}

func TestBrowserSHA256MatchesGo(t *testing.T) {
	// The two implementations must agree byte for byte, not merely
	// agree about which counters pass. This catches a divergence
	// directly rather than waiting for one to show up as a rejected
	// submission.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	inputs := []string{
		"",
		"a",
		"abc",
		"the quick brown fox jumps over the lazy dog",
		// 55, 56 and 64 bytes: the padding boundaries, where a
		// transcribed SHA-256 goes wrong if it is going to.
		"5555555555555555555555555555555555555555555555555555555",
		"66666666666666666666666666666666666666666666666666666666",
		"6666666666666666666666666666666666666666666666666666666666666666",
	}

	const digestJS = `(async () => {
		const m = await import("/pow/sha256.js");
		const b = new TextEncoder().encode(%q);
		return m.hex(m.sha256(b, b.length));
	})()`

	for _, in := range inputs {
		var got string
		rig.Run(chromedp.Evaluate(fmt.Sprintf(digestJS, in), &got, awaitPromise))

		sum := sha256.Sum256([]byte(in))
		if want := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("sha256(%q):\n browser %s\n go      %s", in, got, want)
		}
	}
}

func TestBrowserNormaliserMatchesGo(t *testing.T) {
	// normalize has no exported Go twin, so this reaches it through
	// Verify: if asciiLower and normalize ever disagree on a string,
	// the solution the browser finds for it stops verifying. Checking
	// the fold directly says which string broke, and how.
	rig := powRig(t)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))

	const lowerJS = `(async () => {
		const m = await import("/pow/powcore.js");
		return m.asciiLower(%q);
	})()`

	// Each of these folds differently under a Unicode-aware lowercase,
	// which is why neither side is allowed to use one.
	for _, in := range []string{"İSTANBUL", "ǅ", "STRASSE", "Alice@Example.COM", "ΣΊΣΥΦΟΣ"} {
		var got string
		rig.Run(chromedp.Evaluate(fmt.Sprintf(lowerJS, in), &got, awaitPromise))

		// The Go side, spelled out here rather than imported, so this
		// test fails if either implementation drifts from the rule.
		want := []byte(in)
		for i, c := range want {
			if c >= 'A' && c <= 'Z' {
				want[i] = c + 32
			}
		}
		if got != string(want) {
			t.Errorf("asciiLower(%q) = %q, want %q — the two normalisers disagree",
				in, got, string(want))
		}
	}
}

// honeypotRig serves one form carrying Fields through rastrillo.Handler,
// so the page gets the Content-Security-Policy an app really sends:
// the default when csp is empty, a replacement otherwise.
func honeypotRig(t *testing.T, csp string) *harness.Rig {
	t.Helper()
	return harness.New(t, func(string) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<!doctype html><title>pow</title><form method=post>%s</form>",
				pow.Challenge{}.Fields())
		})
		h, closeDB, err := rastrillo.Handler(rastrillo.Options{Mux: mux, CSP: csp})
		if err != nil {
			t.Fatalf("rastrillo.Handler: %v", err)
		}
		t.Cleanup(func() { closeDB() })
		return h
	})
}

// honeypotPosition is the computed position of the honeypot's wrapper:
// "absolute" when its inline style was applied, "static" when the page's
// policy blocked it and the trap is sitting in plain view.
const honeypotPosition = `getComputedStyle(document.querySelector('[aria-hidden="true"]')).position`

func TestBrowserHoneypotStaysOffScreenUnderTheDefaultCSP(t *testing.T) {
	// The default policy has no 'unsafe-inline' for styles, so the
	// honeypot's style attribute survives only because its hash is
	// listed. If the two ever disagree a person sees a "Leave this field
	// empty" box on every public form — and some will fill it in.
	rig := honeypotRig(t, "")
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	var got string
	rig.Run(chromedp.Evaluate(honeypotPosition, &got))
	if got != "absolute" {
		t.Fatalf("honeypot wrapper position = %q under the default CSP, want absolute", got)
	}
}

func TestBrowserHoneypotStyleIsBlockedWithoutItsHash(t *testing.T) {
	// The control: without this, the test above would pass just as well
	// against a browser that ignored style-src for attributes entirely.
	rig := honeypotRig(t, "default-src 'self'; style-src 'self'")
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	var got string
	rig.Run(chromedp.Evaluate(honeypotPosition, &got))
	if got != "static" {
		t.Fatalf("honeypot wrapper position = %q under style-src 'self', want static (blocked)", got)
	}
}

// formRig serves pow's assets, one page built from a real Guard, and a
// POST endpoint that Checks the submission and says what it decided.
type formRig struct {
	*harness.Rig
	g         *pow.Guard
	scriptURL string
	posts     atomic.Int32
	postedAt  atomic.Int64 // unix ms of the last POST
	last      atomic.Value // "ok" or the refusal reason
}

type rigOpts struct {
	cfg         func(*pow.Config)
	page        func(r *formRig) string
	csp         string
	delayModule time.Duration // serve pow.<hash>.js this late
}

func newFormRig(t *testing.T, o rigOpts) *formRig {
	t.Helper()
	fr := &formRig{}
	fr.Rig = harness.New(t, func(string) http.Handler {
		assets := rastrillo.NewAssets(pow.Assets())
		fr.scriptURL = "/pow" + assets.Path("pow.js")
		cfg := pow.Config{InstanceKey: "browser", Nonces: pow.MemoryNonces(), Difficulty: browserDifficulty,
			MinAge: 50 * time.Millisecond, ScriptURL: fr.scriptURL, WorkerURL: "/pow" + assets.Path("pow-worker.js")}
		if o.cfg != nil {
			o.cfg(&cfg)
		}
		g, err := pow.New(cfg)
		if err != nil {
			t.Fatalf("pow.New: %v", err)
		}
		fr.g = g
		mux := http.NewServeMux()
		powFiles := http.StripPrefix("/pow/", assets.Handler())
		mux.Handle("GET /pow/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if o.delayModule > 0 && r.URL.Path == fr.scriptURL {
				time.Sleep(o.delayModule)
			}
			powFiles.ServeHTTP(w, r)
		}))
		pageHandler := func(w http.ResponseWriter, r *http.Request) {
			if o.csp != "" {
				w.Header().Set("Content-Security-Policy", o.csp)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, o.page(fr))
		}
		mux.HandleFunc("GET /{$}", pageHandler)
		mux.HandleFunc("GET /again", pageHandler)
		mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
			fr.posts.Add(1)
			fr.postedAt.Store(time.Now().UnixMilli())
			a := g.Check(r, pow.Want{Scope: "s", Binding: r.FormValue("email")})
			res := "ok"
			if !a.OK {
				res = string(a.Reason)
			}
			fr.last.Store(res)
			io.WriteString(w, "<!doctype html><title>done</title><p id=result>"+res+"</p>")
		})
		mux.HandleFunc("GET /elsewhere", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "<!doctype html><title>elsewhere</title><p id=elsewhere>elsewhere</p>")
		})
		mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(3 * time.Second)
			io.WriteString(w, "<!doctype html><title>slow</title><p id=slow>slow</p>")
		})
		mux.HandleFunc("GET /bad-worker.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, `throw new Error("a worker that cannot start");`)
		})
		mux.HandleFunc("GET /busy.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(ui.BusyJS())
		})
		return mux
	})
	return fr
}

// powPage renders the ordinary protected form. bound adds the binding
// marker to the email field; outside moves the submit out of the form;
// extra is markup after the form.
func powPage(bound, outside bool, extra string) func(*formRig) string {
	return func(fr *formRig) string {
		f := fr.g.Form(time.Now(), "s")
		binding := ""
		if bound {
			binding = " data-pow-binding"
		}
		button := `<button id=go type=submit data-pow-submit disabled>Send</button>`
		inside, after := button, ""
		if outside {
			inside, after = "", `<button id=go type=submit form=f data-pow-submit disabled>Send</button>`
		}
		return `<!doctype html><title>pow</title><form id=f method=post action=/submit ` + string(f.Attrs()) + `>` +
			string(f.Fields()) + `<input name=email id=email value="a@example.com"` + binding + `>` + inside +
			string(f.StatusLine("If this form does not respond, reload the page.")) +
			`<noscript><p id=ns>This form needs JavaScript.</p></noscript></form>` + after + extra + string(f.Script())
	}
}

func (fr *formRig) waitReady(t *testing.T) {
	t.Helper()
	fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.WaitReady(`form[data-pow-ready]`, chromedp.ByQuery))
}

func (fr *formRig) result(t *testing.T) string {
	t.Helper()
	var res string
	fr.Run(chromedp.WaitVisible(`#result`, chromedp.ByQuery), chromedp.Text(`#result`, &res, chromedp.ByQuery))
	return res
}

const whenSolvedJS = `(async () => {
	const m = await import(%q);
	try { return JSON.stringify(await m.whenSolved(document.getElementById("f"), %s)); }
	catch (e) { return "rejected: " + e.message; }
})()`

func TestBrowserUnboundSolvesBeforeSubmit(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.waitReady(t)
	var got string
	fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
	if !strings.Contains(got, `"pow_counter":"`) || strings.Contains(got, `"pow_counter":""`) {
		t.Fatalf("whenSolved before any click = %s, want a counter", got)
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("submission = %s", res)
	}
}

func TestBrowserFastClickIsHeldNotRefused(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 1500 * time.Millisecond },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("a click before the minimum age = %s, want held and then ok", res)
	}
}

func TestBrowserBoundFormResolvesAfterTheBindingChanges(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.Bind = true; c.MinAge = 1500 * time.Millisecond },
		page: powPage(true, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("email").value = "b@example.com"`, nil))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("binding changed during the hold = %s, want re-solved and ok (a stale proof is pow_short)", res)
	}
}

// TestBrowserAnEmptyBindingSaysSo: the binding input here is optional,
// so native validation passes it empty or holding only spaces. pow
// cannot bind to nothing; without its own error the visitor would get
// neither a message nor a submit.
func TestBrowserAnEmptyBindingSaysSo(t *testing.T) {
	for _, c := range []struct{ name, value string }{{"empty", ""}, {"only spaces", "   "}} {
		t.Run(c.name, func(t *testing.T) {
			fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Bind = true }, page: powPage(true, false, "")})
			fr.waitReady(t)
			fr.Run(chromedp.Evaluate(fmt.Sprintf(`document.getElementById("email").value = %q`, c.value), nil),
				chromedp.Click(`#go`, chromedp.ByQuery))
			time.Sleep(500 * time.Millisecond)
			var msg string
			fr.Run(chromedp.Evaluate(`document.getElementById("email").validationMessage`, &msg))
			if msg == "" {
				t.Fatal("submitting with an empty binding reported no error on the binding input")
			}
			if n := fr.posts.Load(); n != 0 {
				t.Fatalf("%d POSTs with an empty binding, want 0", n)
			}
			// Typing clears the error pow set, and the corrected form goes.
			fr.Run(chromedp.SendKeys(`#email`, "b@example.com", chromedp.ByQuery))
			fr.Run(chromedp.Evaluate(`document.getElementById("email").validationMessage`, &msg))
			if msg != "" {
				t.Fatalf("after typing, the binding still reports %q", msg)
			}
			fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
			if res := fr.result(t); res != "ok" {
				t.Fatalf("the corrected form = %s, want ok", res)
			}
		})
	}
}

func TestBrowserLeavingDuringAHoldNeverSubmits(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, `<a id=slow href=/slow>elsewhere</a>`),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#slow`, chromedp.ByQuery))
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after the visitor navigated away during the hold, want 0", n)
	}
}

func TestBrowserBackForwardRestoresTheForm(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pagehide", {persisted: true}))`, nil))
	time.Sleep(2500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("a held submit fired after pagehide: %d POSTs", n)
	}
	// The cache restores the DOM as it was left, and a page can leave a
	// submit disabled (busy.js does while it holds). Without disabling it
	// here, "enabled" below would be true whether or not pageshow ran.
	fr.Run(chromedp.Evaluate(`document.getElementById("go").disabled = true`, nil))
	var enabled bool
	fr.Run(chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true})); !document.getElementById("go").disabled`, &enabled))
	if !enabled {
		t.Fatal("pageshow from the back-forward cache left the submit disabled")
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("after restore = %s", res)
	}
}

func TestBrowserPageshowRestartsUnfinishedWork(t *testing.T) {
	// Difficulty 40 never finishes, so the solve is certainly unfinished at
	// pagehide and the only thing that can start another worker is the
	// pageshow handler. (A difficulty that merely "usually" outlasts
	// pagehide would leave the restart branch unreached whenever it won
	// the race, which is how it went unpinned.) Counting Worker
	// constructions observes the restart without waiting for a result.
	fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = 40 }, page: powPage(false, false, "")})
	fr.waitReady(t)
	var started int
	fr.Run(chromedp.Evaluate(`(() => {
		window.workersStarted = 0;
		const W = window.Worker;
		window.Worker = class extends W { constructor(...a) { super(...a); window.workersStarted++; } };
		dispatchEvent(new PageTransitionEvent("pagehide", {persisted: true}));
		dispatchEvent(new PageTransitionEvent("pageshow", {persisted: false}));
		return window.workersStarted;
	})()`, &started))
	if started != 0 {
		t.Fatalf("a pageshow that was not a cache restore started %d workers, want 0", started)
	}
	fr.Run(chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true})); window.workersStarted`, &started))
	if started != 1 {
		t.Fatalf("pageshow from the back-forward cache started %d workers, want 1: unfinished unbound work must restart", started)
	}
}

func TestBrowserInitAfterDocumentWrite(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.waitReady(t)
	var ready bool
	fr.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
		const html = await (await fetch("/again")).text();
		document.open(); document.write(html); document.close();
		const m = await import(%q);
		m.init(document);
		return document.querySelector("form").hasAttribute("data-pow-ready");
	})()`, fr.scriptURL), &ready, awaitPromise))
	if !ready {
		t.Fatal("init(document) after document.write did not wire the new form")
	}
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("after document.write = %s", res)
	}
}

func TestBrowserNavigationIsStillCancelledAfterDocumentWrite(t *testing.T) {
	// document.open erases the window's listeners. init has to put the
	// lifecycle back, or after a step-up screen nothing stops a held
	// submit from replacing the navigation the visitor chose.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, `<a id=slow href=/slow>elsewhere</a>`),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
		const html = await (await fetch("/again")).text();
		document.open(); document.write(html); document.close();
		(await import(%q)).init(document);
	})()`, fr.scriptURL), nil, awaitPromise))
	fr.Run(chromedp.WaitReady(`form[data-pow-ready]`, chromedp.ByQuery),
		chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#slow`, chromedp.ByQuery))
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after navigating away from a replaced document, want 0", n)
	}
}

func TestBrowserPagehideStillEndsAHoldAfterDocumentWrite(t *testing.T) {
	// The test above is covered by the hold's own beforeunload listener,
	// which proves nothing about the window listeners init re-attaches.
	// A synthetic pagehide fires no beforeunload, so only the re-attached
	// pagehide handler can end this hold.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 2 * time.Second },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
		const html = await (await fetch("/again")).text();
		document.open(); document.write(html); document.close();
		(await import(%q)).init(document);
	})()`, fr.scriptURL), nil, awaitPromise))
	fr.Run(chromedp.WaitReady(`form[data-pow-ready]`, chromedp.ByQuery),
		chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`dispatchEvent(new PageTransitionEvent("pagehide", {persisted: true}))`, nil))
	time.Sleep(2500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs: a hold survived pagehide on a replaced document", n)
	}
}

func TestBrowserRealBackNavigationLeavesAUsableForm(t *testing.T) {
	// The synthetic-event test above pins the handlers; this one leaves
	// for real while a submit is held and comes back. The hold is the
	// minimum age, not a slow solve: a solve long enough to be sure of
	// would make the return trip wait for it too. Headless Chrome may or
	// may not restore from the back-forward cache: either way the held
	// submit must never fire and the form must work again.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 3 * time.Second },
		page: powPage(false, false, `<a id=away href=/elsewhere>away</a><script>addEventListener("pageshow", e => { window.restored = e.persisted })</script>`),
	})
	fr.waitReady(t)
	// Past this point chromedp's own waits cannot be trusted. NavigateBack
	// blocks on a load lifecycle event that a back-forward cache restore
	// never fires, and WaitReady, Poll and Click all work from node ids
	// and helpers that the restored document does not honour. Plain
	// Evaluate talks to the live page, so the return trip is driven with it.
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#away`, chromedp.ByQuery),
		chromedp.WaitVisible(`#elsewhere`, chromedp.ByQuery),
		chromedp.Evaluate(`history.back()`, nil))
	deadline := time.Now().Add(10 * time.Second)
	for {
		var back bool
		fr.Run(chromedp.Evaluate(`!!document.querySelector("form[data-pow-ready]")`, &back))
		if back {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the form never came back after history.back()")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var restored bool
	fr.Run(chromedp.Evaluate(`window.restored === true`, &restored))
	t.Logf("restored from the back-forward cache: %v", restored)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs: the submit held when the visitor left fired anyway", n)
	}
	// The check above runs well before the minimum age. A hold that
	// survived the trip would fire when it elapses, so wait that out.
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after the minimum age: the submit held when the visitor left resumed on return", n)
	}
	fr.Run(chromedp.Evaluate(`document.getElementById("go").click()`, nil))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("after coming back = %s", res)
	}
}

func TestBrowserARemovedSubmitterCancelsTheHold(t *testing.T) {
	// Released without its button, the form would post to its default
	// action with the button's name and value missing.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = 1500 * time.Millisecond },
		page: powPage(false, false, ""),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery),
		chromedp.Evaluate(`document.getElementById("go").remove()`, nil))
	time.Sleep(2500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after the submitter was removed during the hold, want 0", n)
	}
}

func TestBrowserSubmitOutsideTheForm(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, true, "")})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("form= submit = %s", res)
	}
}

func TestBrowserFailureLeavesTheStatusLineShowing(t *testing.T) {
	cases := map[string]rigOpts{
		"missing module":  {cfg: func(c *pow.Config) { c.ScriptURL = "/pow/missing.js" }},
		"blocked by CSP":  {csp: "default-src 'self'; script-src 'none'"},
		"throwing worker": {cfg: func(c *pow.Config) { c.WorkerURL = "/bad-worker.js" }},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			o.page = powPage(false, false, "")
			fr := newFormRig(t, o)
			fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.WaitVisible(`[data-pow-status]`, chromedp.ByQuery))
			fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
			time.Sleep(500 * time.Millisecond)
			var visible bool
			fr.Run(chromedp.Evaluate(`!document.querySelector("[data-pow-status]").hidden`, &visible))
			if !visible {
				t.Fatal("the status line is hidden although the form cannot work")
			}
			if n := fr.posts.Load(); n != 0 {
				t.Fatalf("%d POSTs from a form whose check could not run", n)
			}
		})
	}
}

func TestBrowserWhenSolvedContract(t *testing.T) {
	t.Run("NoProof resolves at once", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = pow.NoProof }, page: powPage(false, false, "")})
		fr.Run(chromedp.Navigate(fr.Origin + "/"))
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
		if !strings.Contains(got, `"pow_seal"`) {
			t.Fatalf("whenSolved on a NoProof form = %s, want its fields", got)
		}
	})
	t.Run("bound rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Bind = true }, page: powPage(true, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{}"), &got, awaitPromise))
		if !strings.HasPrefix(got, "rejected:") {
			t.Fatalf("whenSolved on a bound form = %s, want rejected", got)
		}
	})
	t.Run("worker failure rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.WorkerURL = "/bad-worker.js" }, page: powPage(false, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{timeout: 60000}"), &got, awaitPromise))
		if !strings.Contains(got, "worker failed") {
			t.Fatalf("whenSolved with a failing worker = %s, want rejected because the worker failed (not a timeout)", got)
		}
	})
	t.Run("leaving the page rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = 40 }, page: powPage(false, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(`(async () => {
			const m = await import(%q);
			const p = m.whenSolved(document.getElementById("f"));
			dispatchEvent(new PageTransitionEvent("pagehide", {persisted: true}));
			try { await p; return "resolved"; } catch (e) { return "rejected: " + e.message; }
		})()`, fr.scriptURL), &got, awaitPromise))
		if !strings.Contains(got, "page was left") {
			t.Fatalf("whenSolved across pagehide = %s, want rejected because the page was left", got)
		}
	})
	t.Run("timeout rejects", func(t *testing.T) {
		fr := newFormRig(t, rigOpts{cfg: func(c *pow.Config) { c.Difficulty = 40 }, page: powPage(false, false, "")})
		fr.waitReady(t)
		var got string
		fr.Run(chromedp.Evaluate(fmt.Sprintf(whenSolvedJS, fr.scriptURL, "{timeout: 200}"), &got, awaitPromise))
		if !strings.Contains(got, "timed out") {
			t.Fatalf("whenSolved past its timeout = %s, want rejected as timed out", got)
		}
	})
}

func TestBrowserNoProofWorksWithoutJavaScript(t *testing.T) {
	fr := newFormRig(t, rigOpts{
		cfg: func(c *pow.Config) { c.Difficulty = pow.NoProof; c.MinAge = 3 * time.Second },
		page: func(fr *formRig) string {
			f := fr.g.FollowOn(time.Now(), "s")
			return `<!doctype html><title>np</title><form id=f method=post action=/submit>` + string(f.Fields()) +
				`<button id=go type=submit>Send</button></form>`
		},
	})
	fr.Run(emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(fr.Origin+"/"), chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("NoProof follow-on form without JavaScript = %s, want ok", res)
	}
}

func TestBrowserProofFormWithoutJavaScriptCannotSubmit(t *testing.T) {
	fr := newFormRig(t, rigOpts{page: powPage(false, false, "")})
	fr.Run(emulation.SetScriptExecutionDisabled(true), chromedp.Navigate(fr.Origin+"/"),
		chromedp.WaitVisible(`#ns`, chromedp.ByQuery), chromedp.Click(`#go`, chromedp.ByQuery))
	time.Sleep(500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs from a disabled proof form with JavaScript off", n)
	}
}

func TestBrowserEnterBeforeReadyPostsNothing(t *testing.T) {
	// Review focus 4: implicit submission with the default button
	// disabled must do nothing, and the visitor sees why.
	fr := newFormRig(t, rigOpts{page: powPage(false, false, ""), delayModule: 2 * time.Second})
	fr.Run(chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(fr.Origin + "/").Do(ctx)
		return err
	}), chromedp.WaitVisible(`#email`, chromedp.ByQuery), chromedp.SendKeys(`#email`, kb.Enter, chromedp.ByQuery))
	time.Sleep(500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs from Enter before the module loaded", n)
	}
	var visible bool
	fr.Run(chromedp.Evaluate(`!document.querySelector("[data-pow-status]").hidden`, &visible))
	if !visible {
		t.Fatal("the status line is not showing while the form is not ready")
	}
}

func TestBrowserBusyDoesNotHoldAPowRelease(t *testing.T) {
	// The visitor already watched pow's hold; busy.js adding its own
	// 650ms after it is a second wait for nothing.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.MinAge = time.Second },
		page: powPage(false, false, `<script defer src="/busy.js"></script>`),
	})
	fr.waitReady(t)
	clicked := time.Now()
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("with busy.js loaded = %s", res)
	}
	if n := fr.posts.Load(); n != 1 {
		t.Fatalf("%d POSTs, want 1", n)
	}
	if d := time.UnixMilli(fr.postedAt.Load()).Sub(clicked); d > 1500*time.Millisecond {
		t.Fatalf("POST %v after the click: busy.js held a submit pow had already held", d)
	}
}

func TestBrowserBusyAndPowShareTheButton(t *testing.T) {
	// During pow's hold the button stays busy (busy.js must not hand it
	// back a tick later), and a binding changed mid-hold still submits:
	// the deadlock this guards against left the form busy and the button
	// disabled for good.
	fr := newFormRig(t, rigOpts{
		cfg:  func(c *pow.Config) { c.Bind = true; c.MinAge = 1500 * time.Millisecond },
		page: powPage(true, false, `<script defer src="/busy.js"></script>`),
	})
	fr.waitReady(t)
	fr.Run(chromedp.Click(`#go`, chromedp.ByQuery))
	time.Sleep(500 * time.Millisecond)
	var busy string
	fr.Run(chromedp.Evaluate(`document.getElementById("go").getAttribute("aria-busy") || ""`, &busy))
	if busy != "true" {
		t.Fatalf("aria-busy during pow's hold = %q, want true", busy)
	}
	fr.Run(chromedp.Evaluate(`document.getElementById("email").value = "b@example.com"`, nil))
	if res := fr.result(t); res != "ok" {
		t.Fatalf("binding changed during the hold, with busy.js loaded = %s", res)
	}
}

func TestBrowserBusyHoldIsCancelledByNavigation(t *testing.T) {
	// busy.js on its own had the same race: its 650ms hold cancelled only
	// on pagehide, which fires after a slow destination commits.
	fr := newFormRig(t, rigOpts{page: func(*formRig) string {
		return `<!doctype html><title>busy</title><form method=post action=/submit>` +
			`<button id=go type=submit>Send</button></form><a id=slow href=/slow>elsewhere</a>` +
			`<script defer src="/busy.js"></script>`
	}})
	fr.Run(chromedp.Navigate(fr.Origin+"/"), chromedp.Click(`#go`, chromedp.ByQuery), chromedp.Click(`#slow`, chromedp.ByQuery))
	time.Sleep(3500 * time.Millisecond)
	if n := fr.posts.Load(); n != 0 {
		t.Fatalf("%d POSTs after navigating away during busy.js's hold, want 0", n)
	}
}
