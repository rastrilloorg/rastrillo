//go:build browser

package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

const passkeyDoor = `[data-rst-passkey]`

// keepSubmit stops the email form's submit, so a drive can send it and
// stay on the page to see what the passkey door does next.
const keepSubmit = `addEventListener("submit", (e) => {
  if (e.target.getAttribute("action") === "/signin") e.preventDefault();
}, true)`

// pressRig is the screen's app with proof off, the passkey stand-in
// counting begins, and a hold in front of its scripts; oneWay builds
// the one-way screen.
func pressRig(t *testing.T, oneWay bool) (*harness.Rig, *moduleHold) {
	t.Helper()
	app, build := newScreenApp(t)
	app.proofOff, app.oneWay = true, oneWay
	hold := &moduleHold{}
	rig := harness.New(t, hold.wrap(app, build))
	// After the rig, so it runs first: the rig's server waits for every
	// request, held ones too, before it closes.
	t.Cleanup(hold.release)
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{
				Source: passkeyStandIn + ";" + keepSubmit})
		return err
	}))
	return rig, hold
}

// loadHeld opens the sign-in page without waiting for its load, which a
// held module script holds back, and waits for the door to be parsed.
func loadHeld(t *testing.T, rig *harness.Rig) {
	t.Helper()
	loadHeldAt(t, rig, rig.Origin+"/signin")
}

func loadHeldAt(t *testing.T, rig *harness.Rig, address string) {
	t.Helper()
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.Navigate,
			page.NavigateParams{URL: address})
		return err
	}), chromedp.WaitVisible(chromedp.CSS(passkeyDoor)))
}

// tap presses the middle of selector's element with the mouse, as a
// person does: a trusted event, unlike element.click().
func tap(t *testing.T, rig *harness.Rig, selector string) {
	t.Helper()
	var at struct{ X, Y float64 }
	eval(t, rig, `(() => {
	  const r = document.querySelector(`+jsString(selector)+`)
	    .getBoundingClientRect();
	  return {X: r.x + r.width / 2, Y: r.y + r.height / 2};
	})()`, &at)
	run(t, rig, chromedp.MouseClickXY(at.X, at.Y))
}

// doorWords is what the door can say.
type doorWords struct{ cancelled, unavailable string }

// doorSays waits up to five seconds for the door's message, and returns
// it with the words the door carries.
func doorSays(t *testing.T, rig *harness.Rig) (string, doorWords) {
	t.Helper()
	var got struct{ Said, Cancelled, Unavailable string }
	eval(t, rig, `(async () => {
	  const b = document.querySelector(`+jsString(passkeyDoor)+`);
	  const msg = document.getElementById("rst-signin-passkey-msg");
	  for (let i = 0; i < 200 && !msg.textContent; i++) {
	    await new Promise((r) => setTimeout(r, 25));
	  }
	  return {Said: msg.textContent,
	    Cancelled: b.getAttribute("data-rst-passkey-cancelled"),
	    Unavailable: b.getAttribute("data-rst-passkey-unavailable")};
	})()`, &got)
	return got.Said, doorWords{got.Cancelled, got.Unavailable}
}

// settled is the door after a second: how many ceremonies began since
// the page loaded, and whether it is still busy.
func settled(t *testing.T, rig *harness.Rig) (begins int, busy bool) {
	t.Helper()
	var got struct {
		Begins int
		Busy   bool
	}
	eval(t, rig, `(async () => {
	  await new Promise((r) => setTimeout(r, 1000));
	  return {Begins: window.__begins, Busy: document.querySelector(`+
		jsString(passkeyDoor)+`).getAttribute("aria-busy") === "true"};
	})()`, &got)
	return got.Begins, got.Busy
}

// Only a person's press starts a ceremony: not markup that says a
// press was made, and not a script's click, whether the probe or the
// module hears it (review of #433). The module is heard while its
// WebAuthn helper is still held.
func TestAnUntrustedPressIsNotKept(t *testing.T) {
	rig, hold := pressRig(t, false)
	for _, row := range []struct{ what, held, press string }{
		{"markup saying it was pressed", doorModule,
			`.setAttribute("data-rst-passkey-queued", "")`},
		{"a script's click to the probe", doorModule, `.click()`},
		{"a script's click to the module", webauthnModule, `.click()`},
	} {
		hold.arm(row.held)
		loadHeld(t, rig)
		if row.held == webauthnModule {
			run(t, rig, chromedp.Poll[chromedp.Void](
				`document.readyState === "complete"`))
		}
		var ok bool
		eval(t, rig, `document.querySelector(`+jsString(passkeyDoor)+`)`+
			row.press+`, true`, &ok)
		hold.release()
		if begins, _ := settled(t, rig); begins != 0 {
			t.Errorf("%s began %d ceremonies, want none", row.what, begins)
		}
	}
}

// A cancelled press leaves the door to the press that followed it:
// press with the WebAuthn helper held, leave and come back, press
// again, then let the helper load. The first press's end must not mark
// the door idle while the second's ceremony runs (held at the prompt
// here), or a third press would start it over (review of #433).
func TestACancelledPressLeavesTheNextOneBusy(t *testing.T) {
	rig, hold := pressRig(t, false)
	hold.arm(webauthnModule)
	loadHeld(t, rig)
	run(t, rig, chromedp.Poll[chromedp.Void](
		`document.readyState === "complete"`))
	var ok bool
	eval(t, rig, `window.__hold = true`, &ok)
	tap(t, rig, passkeyDoor)
	eval(t, rig, `dispatchEvent(new PageTransitionEvent("pagehide", `+
		`{persisted: true})), dispatchEvent(new PageTransitionEvent(`+
		`"pageshow", {persisted: true})), true`, &ok)
	tap(t, rig, passkeyDoor)
	hold.release()
	if begins, busy := settled(t, rig); begins != 1 || !busy {
		t.Errorf("the second press: %d ceremonies began and the door is "+
			"busy %v, want 1 and busy", begins, busy)
	}
	tap(t, rig, passkeyDoor)
	if begins, busy := settled(t, rig); begins != 1 || !busy {
		t.Errorf("a third press while the second runs: %d ceremonies "+
			"began and the door is busy %v, want 1 and busy", begins, busy)
	}
}

// A press still waiting for its module ends when the person sends the
// email form instead, or leaves the page: no ceremony starts once the
// module arrives. Back on the page, one press is one ceremony. Both
// waits are held: the door's module, while the probe keeps the press,
// and the WebAuthn helper, while the module does.
func TestAPendingPressEndsWithTheAttempt(t *testing.T) {
	rig, hold := pressRig(t, false)
	for _, held := range []string{doorModule, webauthnModule} {
		hold.arm(held)
		loadHeld(t, rig)
		tap(t, rig, passkeyDoor)
		run(t, rig, chromedp.SetValue(chromedp.CSS("#rst-signin-email"),
			"ada@example.com"))
		tap(t, rig, continueButton)
		hold.release()
		if begins, busy := settled(t, rig); begins != 0 || busy {
			t.Errorf("%s held, sending the email form: %d ceremonies "+
				"began and the door is busy %v, want none and not busy",
				held, begins, busy)
		}
		leaveAndReturn(t, rig, hold, held)
	}
}

// leaveAndReturn presses the door with held still loading, then leaves
// and comes back. The back-forward cache keeps a page with no request
// in flight, so that is its two events, as the browser fires them.
func leaveAndReturn(
	t *testing.T, rig *harness.Rig, hold *moduleHold, held string,
) {
	t.Helper()
	hold.arm(held)
	loadHeld(t, rig)
	tap(t, rig, passkeyDoor)
	var ok bool
	eval(t, rig, `dispatchEvent(new PageTransitionEvent("pagehide", `+
		`{persisted: true})), dispatchEvent(new PageTransitionEvent(`+
		`"pageshow", {persisted: true})), true`, &ok)
	hold.release()
	if begins, busy := settled(t, rig); begins != 0 || busy {
		t.Errorf("%s held, leaving the page: %d ceremonies began and the "+
			"door is busy %v, want none and not busy", held, begins, busy)
	}
	tap(t, rig, passkeyDoor)
	if begins, _ := settled(t, rig); begins != 1 {
		t.Errorf("%s held, one press after coming back began %d "+
			"ceremonies, want 1", held, begins)
	}
}

// When the door's module cannot load (a network failure, or a CSP that
// drops it), a press says passkeys do not work here, and on the one-way
// screen, where the door is the only way in, opens the email way.
func TestABlockedModuleSaysPasskeysDoNotWork(t *testing.T) {
	rig, hold := pressRig(t, false)
	hold.blocked = doorModule
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin"),
		chromedp.WaitVisible(chromedp.CSS(passkeyDoor)))
	tap(t, rig, passkeyDoor)
	if said, want := doorSays(t, rig); said != want.unavailable {
		t.Errorf("with its module blocked the door said %q, want %q",
			said, want.unavailable)
	}

	rig, hold = pressRig(t, true)
	hold.blocked = doorModule
	run(t, rig, chromedp.Navigate(rig.Origin+"/signin?way=passkey"),
		chromedp.WaitVisible(chromedp.CSS(passkeyDoor)))
	tap(t, rig, passkeyDoor)
	run(t, rig, chromedp.WaitVisible(chromedp.CSS("#rst-signin-email")))
	if here := read(t, rig, chromedp.Location()); !strings.Contains(
		here, "way=email") {
		t.Errorf("with its module blocked the one-way door went to %s, "+
			"want the email way", here)
	}
}
