//go:build browser

package signin

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
)

const addButton = `[data-rst-passkey-register]`

// scriptGate stands between the browser and the add page: it holds
// requests to some paths until release, as a slow network does, refuses
// another (blocked), as a network failure or a CSP would, and counts
// the register begins the page makes.
type scriptGate struct {
	mu      sync.Mutex
	held    map[string]chan struct{}
	blocked string
	begins  atomic.Int64
}

func (gate *scriptGate) hold(path string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.held == nil {
		gate.held = map[string]chan struct{}{}
	}
	gate.held[path] = make(chan struct{})
}

// release lets through what is held at paths, or at every path when
// none are named.
func (gate *scriptGate) release(paths ...string) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	for path, wait := range gate.held {
		if len(paths) == 0 || slices.Contains(paths, path) {
			close(wait)
			delete(gate.held, path)
		}
	}
}

func (gate *scriptGate) serve(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		path := request.URL.Path
		if path == RegisterBeginPath {
			gate.begins.Add(1)
		}
		gate.mu.Lock()
		wait, blocked := gate.held[path], path == gate.blocked
		gate.mu.Unlock()
		if blocked {
			http.NotFound(response, request)
			return
		}
		if wait != nil {
			select {
			case <-wait:
			case <-request.Context().Done():
				return
			}
		}
		handler.ServeHTTP(response, request)
	})
}

// gatedRig is the sign-in app behind a scriptGate, with ada signed in.
func gatedRig(t *testing.T, app *browserApp) (*harness.Rig, *scriptGate) {
	t.Helper()
	gate := &scriptGate{}
	build := newBrowserApp(t, app)
	rig := harness.New(t, func(origin string) http.Handler {
		return gate.serve(build(origin))
	})
	// After the rig, so it runs first: the rig's server waits for every
	// request, held ones too, before it closes.
	t.Cleanup(func() { gate.release() })
	signInByCode(t, rig, app)
	return rig, gate
}

// openAdd opens the add page without waiting for its load, which a
// held module script holds back.
func openAdd(t *testing.T, rig *harness.Rig) {
	t.Helper()
	walk(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, cdppage.Navigate,
			cdppage.NavigateParams{URL: rig.Origin + AddPasskeyPath})
		return err
	}), chromedp.WaitVisible(chromedp.CSS(addButton)))
}

// press presses the add button with the mouse, as a person does: a
// trusted event, unlike element.click().
func press(t *testing.T, rig *harness.Rig) {
	t.Helper()
	var at []float64
	evaluate(t, rig, `(() => {
	  const r = document.querySelector("`+addButton+`")
	    .getBoundingClientRect();
	  return [r.x + r.width / 2, r.y + r.height / 2];
	})()`, &at)
	walk(t, rig, chromedp.MouseClickXY(at[0], at[1]))
}

// The add page's button is enabled in its first frame, by the probe in
// the head, and a press before register.mjs has loaded still makes the
// passkey once it does: held here as a slow network would hold it.
func TestAPasskeyAddPressBeforeTheModuleCounts(t *testing.T) {
	rig, gate := gatedRig(t, &browserApp{})
	gate.hold(RegisterModulePath)
	openAdd(t, rig)
	var enabled bool
	evaluate(t, rig, `!document.querySelector("`+addButton+`").disabled`,
		&enabled)
	if !enabled {
		t.Error("the add button was disabled while register.mjs loaded")
	}
	press(t, rig)
	gate.release()
	walk(t, rig, arrive(`a[rst-btn][autofocus]`))
	want := rastrillo.BaseCatalog()["rastrillo.ui.signin_added"]
	if got := textAt(t, rig, "[rst-signin-door] > p"); got != want {
		t.Fatalf("after the early press the page says %q, want %q", got,
			want)
	}
}

// A press still waiting for the WebAuthn module ends when the person
// leaves the page: nothing is begun once the module arrives. Back on the
// page, one press is one registration. The back-forward cache keeps a
// page with no request in flight, so leaving and coming back is its two
// events, as the browser fires them, while the module is held.
func TestAPendingAddPressEndsWithThePage(t *testing.T) {
	rig, gate := gatedRig(t, &browserApp{})
	gate.hold(WebauthnModulePath)
	openAdd(t, rig)
	press(t, rig)
	var ok bool
	evaluate(t, rig, `dispatchEvent(new PageTransitionEvent("pagehide", `+
		`{persisted: true})), dispatchEvent(new PageTransitionEvent(`+
		`"pageshow", {persisted: true})), true`, &ok)
	gate.release()
	walk(t, rig, chromedp.Sleep(time.Second))
	if begins := gate.begins.Load(); begins != 0 {
		t.Errorf("leaving the page began %d registrations, want none",
			begins)
	}
	press(t, rig)
	walk(t, rig, arrive(`a[rst-btn][autofocus]`))
	if begins := gate.begins.Load(); begins != 1 {
		t.Errorf("one press after coming back began %d registrations, "+
			"want 1", begins)
	}
}

// When register.mjs cannot load, a press says the passkey did not work
// instead of doing nothing.
func TestABlockedAddModuleSaysItFailed(t *testing.T) {
	rig, gate := gatedRig(t, &browserApp{})
	gate.blocked = RegisterModulePath
	walk(t, rig, chromedp.Navigate(rig.Origin+AddPasskeyPath),
		chromedp.WaitVisible(chromedp.CSS(addButton)))
	press(t, rig)
	said, err := chromedp.Run(rig.Context(), chromedp.Evaluate[string](
		`(async () => {
		  const msg = document.getElementById("rst-signin-passkey-msg");
		  for (let i = 0; i < 200 && !msg.textContent; i++) {
		    await new Promise((r) => setTimeout(r, 25));
		  }
		  return msg.textContent;
		})()`, chromedp.EvalAwaitPromise))
	if err != nil {
		t.Fatal(err)
	}
	want := rastrillo.BaseCatalog()["rastrillo.ui.signin_add_passkey_failed"]
	if said != want {
		t.Errorf("with register.mjs blocked the page said %q, want %q",
			said, want)
	}
}

// An app that upgrades without adding signin-view-head keeps a working
// add button: with no probe, register.mjs enables it once it loads, as
// it did before #433.
func TestAnAddPageWithoutTheHeadPartialKeepsItsButton(t *testing.T) {
	app := &browserApp{noHead: true}
	rig := harness.New(t, newBrowserApp(t, app))
	signInByCode(t, rig, app)
	walk(t, rig, chromedp.Navigate(rig.Origin+AddPasskeyPath))
	enabled, err := chromedp.Run(rig.Context(), chromedp.Evaluate[bool](
		`(async () => {
		  const b = document.querySelector("`+addButton+`");
		  for (let i = 0; i < 200 && b.disabled; i++) {
		    await new Promise((r) => setTimeout(r, 25));
		  }
		  return !b.disabled;
		})()`, chromedp.EvalAwaitPromise))
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("with no probe the add button stayed disabled")
	}
	walk(t, rig, chromedp.Click(chromedp.CSS(addButton)),
		arrive(`a[rst-btn][autofocus]`))
	want := rastrillo.BaseCatalog()["rastrillo.ui.signin_added"]
	if got := textAt(t, rig, "[rst-signin-door] > p"); got != want {
		t.Fatalf("with no probe the add page says %q, want %q", got, want)
	}
}

// pageReady waits for the add page's load, which a held WebAuthn helper
// does not hold back: register.mjs has then claimed the button.
func pageReady(t *testing.T, rig *harness.Rig) {
	t.Helper()
	walk(t, rig, chromedp.Poll[chromedp.Void](
		`document.readyState === "complete"`))
}

// A script's click on the add button begins nothing, while the WebAuthn
// helper is still loading and after (review of #433).
func TestAScriptClickAddsNoPasskey(t *testing.T) {
	rig, gate := gatedRig(t, &browserApp{})
	gate.hold(WebauthnModulePath)
	openAdd(t, rig)
	pageReady(t, rig)
	var ok bool
	evaluate(t, rig, `document.querySelector("`+addButton+`").click(), true`,
		&ok)
	gate.release()
	walk(t, rig, chromedp.Sleep(time.Second))
	if begins := gate.begins.Load(); begins != 0 {
		t.Errorf("a script's click began %d registrations, want none",
			begins)
	}
}

// A cancelled press leaves the button to the press that followed it,
// as on the sign-in door: press with the WebAuthn helper held, leave
// and come back, press again, then let the helper load. The second
// registration's begin is held, so it is still running when the first
// press's end comes, and a third press must not start another.
func TestACancelledAddPressLeavesTheNextOneBusy(t *testing.T) {
	rig, gate := gatedRig(t, &browserApp{})
	gate.hold(WebauthnModulePath)
	gate.hold(RegisterBeginPath)
	openAdd(t, rig)
	pageReady(t, rig)
	press(t, rig)
	var ok bool
	evaluate(t, rig, `dispatchEvent(new PageTransitionEvent("pagehide", `+
		`{persisted: true})), dispatchEvent(new PageTransitionEvent(`+
		`"pageshow", {persisted: true})), true`, &ok)
	press(t, rig)
	gate.release(WebauthnModulePath)
	busy := func() bool {
		var busy bool
		walk(t, rig, chromedp.Sleep(time.Second))
		evaluate(t, rig, `document.querySelector("`+addButton+`")`+
			`.getAttribute("aria-busy") === "true"`, &busy)
		return busy
	}
	if busy := busy(); gate.begins.Load() != 1 || !busy {
		t.Errorf("the second press: %d registrations began and the "+
			"button is busy %v, want 1 and busy", gate.begins.Load(),
			busy)
	}
	press(t, rig)
	if busy := busy(); gate.begins.Load() != 1 || !busy {
		t.Errorf("a third press while the second runs: %d registrations "+
			"began and the button is busy %v, want 1 and busy",
			gate.begins.Load(), busy)
	}
}
