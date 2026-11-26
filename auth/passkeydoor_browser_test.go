//go:build browser

package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// moduleHold holds every request for one script until release, as a
// slow network does, so a drive can act while the passkey door's
// modules are still loading. It can also refuse one script outright
// (blocked), as a network failure or a CSP would.
type moduleHold struct {
	mu      sync.Mutex
	path    string
	gate    chan struct{}
	blocked string
}

// The scripts a drive holds: the WebAuthn helper the door's module
// imports, the door's module itself, and the probe in the head.
const (
	webauthnModule = "/static/webauthn.mjs"
	doorModule     = "/static/passkey-signin.mjs"
	doorProbe      = "/static/passkey-probe.js"
)

func (hold *moduleHold) arm(path string) {
	hold.mu.Lock()
	defer hold.mu.Unlock()
	hold.path, hold.gate = path, make(chan struct{})
}

func (hold *moduleHold) wait(path string) {
	hold.mu.Lock()
	gate := hold.gate
	if path != hold.path {
		gate = nil
	}
	hold.mu.Unlock()
	if gate != nil {
		<-gate
	}
}

func (hold *moduleHold) release() {
	hold.mu.Lock()
	defer hold.mu.Unlock()
	if hold.gate != nil {
		close(hold.gate)
		hold.gate = nil
	}
}

// wrap serves the screen's app with the hold in front of it, and
// /remember-passkey, which remembers a passkey sign-in as a
// finished ceremony would and goes to the sign-in page.
func (hold *moduleHold) wrap(
	app *screenApp, build func(origin string) http.Handler,
) func(origin string) http.Handler {
	return func(origin string) http.Handler {
		handler := build(origin)
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			if request.URL.Path == "/remember-passkey" {
				app.auth.RememberJar().Remember(response,
					lastsignin.Record{Method: lastsignin.MethodPasskey})
				http.Redirect(response, request, "/signin",
					http.StatusSeeOther)
				return
			}
			hold.mu.Lock()
			blocked := request.URL.Path == hold.blocked
			hold.mu.Unlock()
			if blocked {
				http.NotFound(response, request)
				return
			}
			hold.wait(request.URL.Path)
			handler.ServeHTTP(response, request)
		})
	}
}

// shiftWatch runs before any page script. It keeps every layout shift
// no input caused, Continue's box in the first frame drawn with it, the
// element each click hit and the button each submit came from; it
// stops the submit, so every case starts from the same screen.
const shiftWatch = `(() => {
  const name = (el) => !el ? "nothing" : el.id ? el.tagName + "#" + el.id :
    el.closest("form[action='/signin'] button[type=submit]") ? "Continue" :
    el.tagName + (el.getAttribute("rst-btn") !== null ? "[rst-btn]" : "");
  window.__shifts = [];
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) {
      if (e.hadRecentInput) continue;
      window.__shifts.push(e.value.toFixed(4) + " moving " +
        e.sources.map((s) => name(s.node)).join(", "));
    }
  }).observe({ type: "layout-shift", buffered: true });
  const frame = () => {
    const b = document.querySelector(` + "`" + continueButton + "`" + `);
    if (!b) return requestAnimationFrame(frame);
    const r = b.getBoundingClientRect();
    window.__firstBox = [r.x, r.y, r.width, r.height].join(",");
  };
  requestAnimationFrame(frame);
  addEventListener("click", (e) => { window.__hit = name(e.target); }, true);
  addEventListener("submit", (e) => {
    if (e.target.getAttribute("action") !== "/signin") return;
    window.__submitter = name(e.submitter);
    e.preventDefault();
  }, true);

})()`

const continueButton = `form[action="/signin"] button[type="submit"]`

// moduleLoaded resolves once webauthn.mjs has arrived and two frames
// have been drawn since, time for the door's module to act on it.
const moduleLoaded = `(async () => {
  const arrived = () => performance.getEntriesByType("resource")
    .some((e) => e.name.endsWith("` + webauthnModule + `"));
  for (let i = 0; i < 200 && !arrived(); i++) {
    await new Promise((r) => setTimeout(r, 25));
  }
  for (let i = 0; i < 2; i++) await new Promise(requestAnimationFrame);
  await new Promise((r) => setTimeout(r, 100));
  return arrived();
})()`

// doorAim is what one case reads: where Continue was when the person
// aimed, and what the page saw by the time the module had loaded.
type doorAim struct {
	FirstBox, Box, Door string
	Shifts              []string
	X, Y                float64
}

// TestThePasskeyDoorNeverMovesContinue is issue #433: the door ships
// hidden and its module showed it once webauthn.mjs loaded, after the
// load event, moving Continue about 50px at 320px. A click aimed at
// Continue before that and pressed after it landed on the email field.
// With the module held back, nothing on either screen may move, and
// that click must send the form.
func TestThePasskeyDoorNeverMovesContinue(t *testing.T) {
	app, build := newScreenApp(t)
	app.proofOff = true
	hold := &moduleHold{}
	t.Cleanup(hold.release)
	rig := harness.New(t, hold.wrap(app, build))
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		if _, err := cdp.Call(ctx, target, network.SetCacheDisabled,
			network.SetCacheDisabledParams{CacheDisabled: true}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{Source: shiftWatch})
		return err
	}))
	widths := [][2]int64{{320, 640}, {1280, 800}}
	run(t, rig, chromedp.Navigate(rig.Origin+"/remember-passkey"),
		chromedp.WaitVisible(chromedp.CSS(continueButton)))
	for _, size := range widths {
		aimAtContinue(t, rig, hold, "the Returning (passkey) screen",
			"before", size)
	}
	// No cookies, no remembered sign-in: the Ask screen.
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, network.ClearBrowserCookies,
			cdp.Empty{})
		return err
	}))
	for _, size := range widths {
		aimAtContinue(t, rig, hold, "the Ask screen", "after", size)
	}
	// The probe held too: a page that drew anything before it arrived
	// would move that when the card came.
	time.AfterFunc(300*time.Millisecond, hold.release)
	aimAtContinue(t, rig, hold, "the Ask screen, its probe held",
		"after", widths[0])
}

// aimAtContinue loads the sign-in page with the module held, aims at
// Continue, lets the module load, then presses where it aimed.
func aimAtContinue(
	t *testing.T, rig *harness.Rig, hold *moduleHold,
	screen, door string, size [2]int64,
) {
	t.Helper()
	what := fmt.Sprintf("%s at %dpx", screen, size[0])
	held := webauthnModule
	if strings.Contains(screen, "probe") {
		held = doorProbe
	}
	hold.arm(held)
	run(t, rig, chromedp.EmulateViewport(size[0], size[1]),
		chromedp.Navigate(rig.Origin+"/signin"),
		chromedp.WaitVisible(chromedp.CSS(continueButton)),
		chromedp.SetValue(chromedp.CSS("#rst-signin-email"),
			"ada@example.com"))
	var got doorAim
	eval(t, rig, `(() => {
	  const r = document.querySelector(`+jsString(continueButton)+`)
	    .getBoundingClientRect();
	  return {X: r.x + r.width / 2, Y: r.y + r.height / 2};
	})()`, &got)
	hold.release()
	var loaded bool
	eval(t, rig, moduleLoaded, &loaded)
	if !loaded {
		t.Fatalf("%s: webauthn.mjs never arrived", what)
	}
	run(t, rig, chromedp.Poll[chromedp.Void](`!document.querySelector(`+
		jsString(continueButton)+`).disabled`,
		chromedp.WithPollingTimeout(10*time.Second)))
	eval(t, rig, `(() => {
	  const r = document.querySelector(`+jsString(continueButton)+`)
	    .getBoundingClientRect();
	  const door = document.querySelector("[rst-signin-passkey]");
	  const form = document.querySelector("form[action='/signin']");
	  return {FirstBox: window.__firstBox, Shifts: window.__shifts,
	    Door: !door ? "none" : door.compareDocumentPosition(form) &
	      Node.DOCUMENT_POSITION_FOLLOWING ? "before" : "after",
	    Box: [r.x, r.y, r.width, r.height].join(","),
	    X: `+fmt.Sprint(got.X)+`, Y: `+fmt.Sprint(got.Y)+`};
	})()`, &got)
	t.Logf("%s: Continue first drawn at %s, at %s once the module "+
		"loaded; %d layout shifts %q", what, got.FirstBox, got.Box,
		len(got.Shifts), got.Shifts)
	if got.Door != door {
		t.Fatalf("%s: the passkey door is %s the form, want %s",
			what, got.Door, door)
	}
	if len(got.Shifts) != 0 {
		t.Errorf("%s: %d layout shifts with no input: %q",
			what, len(got.Shifts), got.Shifts)
	}
	if got.FirstBox == "" || got.FirstBox != got.Box {
		t.Errorf("%s: Continue was first drawn at %q and is at %q "+
			"once the module has loaded", what, got.FirstBox, got.Box)
	}
	pressWhereAimed(t, rig, what, got.X, got.Y)
}

// pressWhereAimed presses at across, down and requires that the press sent the
// email form from Continue.
func pressWhereAimed(
	t *testing.T, rig *harness.Rig, what string, across, down float64,
) {
	t.Helper()
	run(t, rig, chromedp.MouseClickXY(across, down))
	var hit [2]string
	eval(t, rig, `(async () => {
	  for (let i = 0; i < 40 && !window.__submitter; i++) {
	    await new Promise((r) => setTimeout(r, 25));
	  }
	  return [window.__hit || "nothing", window.__submitter || "none"];
	})()`, &hit)
	if hit[1] != "Continue" {
		t.Errorf("%s: a press aimed at Continue while the module "+
			"loaded hit %s and sent the form from %s, want Continue",
			what, hit[0], hit[1])
	}
}

// TestAPasskeyPressBeforeTheModuleCounts: the door is drawn from the
// first frame, so it can be pressed before its module has loaded, with
// the module itself held and with the WebAuthn helper it imports held.
// That press, a real one, must run the ceremony once the module
// arrives; here the stand-in authenticator refuses it, so the door
// says so.
func TestAPasskeyPressBeforeTheModuleCounts(t *testing.T) {
	rig, hold := pressRig(t, false)
	for _, held := range []string{doorModule, webauthnModule} {
		hold.arm(held)
		loadHeld(t, rig)
		tap(t, rig, passkeyDoor)
		hold.release()
		if said, want := doorSays(t, rig); said != want.cancelled {
			t.Errorf("pressed while %s loaded: the door said %q, want %q",
				held, said, want.cancelled)
		}
	}
}

// consoleErrors keeps every console.error a page logs, for a drive to
// read back as window.__errors.
const consoleErrors = `(() => {
  window.__errors = [];
  const real = console.error.bind(console);
  console.error = (...args) => {
    window.__errors.push(args.join(" "));
    real(...args);
  };
})()`

// An app that upgrades without adding signin-head to its head keeps its
// passkey door: with no probe, the module shows the door once it has
// loaded, as before #433, so it moves Continue late, and the console
// names what to add. The door works: here the stand-in authenticator
// refuses, and the door says so.
func TestAnAppWithoutTheHeadPartialKeepsItsDoor(t *testing.T) {
	app, build := newScreenApp(t)
	app.proofOff, app.noHead = true, true
	hold := &moduleHold{}
	rig := harness.New(t, hold.wrap(app, build))
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{
				Source: passkeyStandIn + ";" + consoleErrors})
		return err
	}))
	for _, start := range []string{"/remember-passkey", "/signin"} {
		run(t, rig, chromedp.Navigate(rig.Origin+start),
			chromedp.WaitVisible(chromedp.CSS(continueButton)))
		var got struct {
			Shown  bool
			Errors []string
		}
		eval(t, rig, `(async () => {
		  const b = document.querySelector("[data-rst-passkey]");
		  for (let i = 0; i < 200 && b.hidden; i++) {
		    await new Promise((r) => setTimeout(r, 25));
		  }
		  return {Shown: b.checkVisibility(), Errors: window.__errors};
		})()`, &got)
		said, want := "", doorWords{}
		if got.Shown {
			tap(t, rig, passkeyDoor)
			said, want = doorSays(t, rig)
		}
		if !got.Shown || said != want.cancelled {
			t.Errorf("%s with no probe: the door was shown %v and said "+
				"%q, want shown and %q", start, got.Shown, said,
				want.cancelled)
		}
		if !strings.Contains(strings.Join(got.Errors, "\n"),
			"signin-head") {
			t.Errorf("%s with no probe: the console said %q, want a "+
				"line naming signin-head", start, got.Errors)
		}
		// No cookies, no remembered sign-in: the Ask screen next.
		run(t, rig, chromedp.Func(func(
			ctx context.Context, target *chromedp.Target,
		) error {
			_, err := cdp.Call(ctx, target, network.ClearBrowserCookies,
				cdp.Empty{})
			return err
		}))
	}
}
