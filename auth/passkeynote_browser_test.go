//go:build browser

package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"amadan.net/rastrillo/rastrillo/harness"
)

// everyShift keeps every layout shift, those right after a tap too:
// the message lands within half a second of the press, where Chrome
// marks a shift as input-caused and the door's other drives skip it.
const everyShift = `(() => {
  window.__allShifts = [];
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) window.__allShifts.push(e.value);
  }).observe({ type: "layout-shift", buffered: true });
})()`

// box is an element's rectangle on the screen; empty when it is absent.
type box struct{ X, Y, W, H float64 }

func (rect box) drawn() bool { return rect.W > 0 && rect.H > 0 }

func (rect box) overlaps(other box) bool {
	return rect.drawn() && other.drawn() &&
		rect.X < other.X+other.W && other.X < rect.X+rect.W &&
		rect.Y < other.Y+other.H && other.Y < rect.Y+rect.H
}

// noteState is the passkey door's note and what sits around it.
type noteState struct {
	Said, Live, Focus                   string
	Note, Door, Email, Continue, Others box
	CloseShown                          bool
	Shifts                              int
}

const noteStateJS = `(() => {
  const box = (el) => {
    if (!el) return {};
    const r = el.getBoundingClientRect();
    return {X: r.x, Y: r.y, W: r.width, H: r.height};
  };
  const door = document.querySelector("[data-rst-passkey]");
  const msg = document.getElementById("rst-signin-passkey-msg");
  const note = msg && msg.closest("[rst-signin-passkey-note]");
  const close = note && note.querySelector("[rst-signin-passkey-close]");
  return {Said: msg ? msg.textContent : "",
    Live: msg ? msg.getAttribute("aria-live") || "" : "",
    Focus: document.activeElement === door ? "door" :
      document.activeElement ? document.activeElement.tagName : "",
    Note: box(note), Door: box(door),
    Email: box(document.getElementById("rst-signin-email")),
    Continue: box(document.querySelector(` + "`" + continueButton + "`" + `)),
    Others: box(document.querySelector("[rst-signin-door] a[rst-btn~=ghost]")),
    CloseShown: !!close && !close.hidden && close.checkVisibility(),
    Shifts: window.__allShifts.length};
})()`

func readNote(t *testing.T, rig *harness.Rig) noteState {
	t.Helper()
	return noteAfter(t, rig, "true")
}

// noteAfter waits up to five seconds for until, a script expression
// over msg, the message, and then two frames, and reads the note.
func noteAfter(t *testing.T, rig *harness.Rig, until string) noteState {
	t.Helper()
	var got noteState
	eval(t, rig, `(async () => {
	  const msg = document.getElementById("rst-signin-passkey-msg") ||
	    {textContent: ""};
	  for (let i = 0; i < 200 && !(`+until+`); i++) {
	    await new Promise((r) => setTimeout(r, 25));
	  }
	  for (let i = 0; i < 2; i++) await new Promise(requestAnimationFrame);
	  return `+noteStateJS+`;
	})()`, &got)
	return got
}

// noteShown waits for the message, and fails the test without it.
func noteShown(t *testing.T, rig *harness.Rig, what string) noteState {
	t.Helper()
	shown := noteAfter(t, rig, "msg.textContent")
	if shown.Said == "" {
		t.Fatalf("%s: the door said nothing after a failed tap", what)
	}
	return shown
}

// noteGone waits for the message to go, and fails the test if it stays.
func noteGone(
	t *testing.T, rig *harness.Rig, what, after string,
) noteState {
	t.Helper()
	gone := noteAfter(t, rig, "!msg.textContent")
	if gone.Said != "" {
		t.Errorf("%s: %s, the message still says %q", what, after,
			gone.Said)
	}
	return gone
}

func noteRig(t *testing.T, oneWay bool) *harness.Rig {
	t.Helper()
	app, build := newScreenApp(t)
	app.oneWay = oneWay
	rig := harness.New(t, (&moduleHold{}).wrap(app, build))
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{
				Source: passkeyStandIn + ";" + keepSubmit + ";" + everyShift})
		return err
	}))
	return rig
}

// openDoor loads address and waits until the door shows and Continue,
// if the page has one, has its proof: the screen as a person meets it.
func openDoor(t *testing.T, rig *harness.Rig, address string) {
	t.Helper()
	run(t, rig, chromedp.Navigate(address),
		chromedp.WaitVisible(chromedp.CSS(passkeyDoor)),
		chromedp.Poll[chromedp.Void](`(() => {
		  const b = document.querySelector(`+jsString(continueButton)+`);
		  return !b || !b.matches(":disabled, [aria-busy]");
		})()`, chromedp.WithPollingTimeout(10*time.Second)))
}

// TestThePasskeyMessageNeverMovesContinue is #437: after a passkey tap
// the door's message took a line above the form and moved Continue
// 28px at 320px. It is now a note beside the door, taking no space:
// below a door that follows the form, above one that leads it, over
// neither the email field nor Continue, in a polite live region, and
// gone on Escape, on its close button, when the form is sent and when
// a new attempt starts.
func TestThePasskeyMessageNeverMovesContinue(t *testing.T) {
	rig := noteRig(t, false)
	screens := []struct{ name, start, opens string }{
		{"the Returning (passkey) screen", "/remember-passkey", "above"},
		{"the Ask screen", "/signin", "below"},
	}
	for _, screen := range screens {
		if screen.opens == "below" {
			clearCookies(t, rig)
		}
		for _, size := range [][2]int64{{320, 640}, {1280, 800}} {
			what := fmt.Sprintf("%s at %dpx", screen.name, size[0])
			run(t, rig, chromedp.EmulateViewport(size[0], size[1]))
			openDoor(t, rig, rig.Origin+screen.start)
			checkNote(t, rig, what, screen.opens)
			clearsOnSendAndPress(t, rig, what)
		}
	}
}

func checkNote(t *testing.T, rig *harness.Rig, what, opens string) {
	t.Helper()
	before := readNote(t, rig)
	tap(t, rig, passkeyDoor)
	shown := noteShown(t, rig, what)
	t.Logf("%s: Continue %v before the message, %v with it; note %v, "+
		"door %v; %d layout shifts", what, before.Continue,
		shown.Continue, shown.Note, shown.Door,
		shown.Shifts-before.Shifts)
	if shown.Continue != before.Continue || shown.Email != before.Email {
		t.Errorf("%s: the message moved Continue from %v to %v and "+
			"the email field from %v to %v", what, before.Continue,
			shown.Continue, before.Email, shown.Email)
	}
	if shifts := shown.Shifts - before.Shifts; shifts != 0 {
		t.Errorf("%s: %d layout shifts when the message came", what, shifts)
	}
	checkPlace(t, what, opens, shown)
	if shown.Live != "polite" || !shown.CloseShown {
		t.Errorf("%s: the message's region is aria-live %q and its close "+
			"button shown %v, want polite and shown", what, shown.Live,
			shown.CloseShown)
	}
	run(t, rig, chromedp.KeyEvent(kb.Escape))
	if gone := noteGone(t, rig, what, "after Escape"); gone.CloseShown ||
		gone.Continue != before.Continue {
		t.Errorf("%s: after Escape the close button shows %v and "+
			"Continue is at %v", what, gone.CloseShown, gone.Continue)
	}
	tap(t, rig, passkeyDoor)
	if again := noteShown(t, rig, what); !again.CloseShown {
		t.Fatalf("%s: no close button to press", what)
	}
	tap(t, rig, "[rst-signin-passkey-close]")
	if gone := noteGone(t, rig, what, "closed"); gone.Focus != "door" {
		t.Errorf("%s: closed, focus went to %s, want the door",
			what, gone.Focus)
	}
}

// checkPlace holds the note to its side of the door and off the
// controls a person may be aiming at.
func checkPlace(t *testing.T, what, opens string, shown noteState) {
	t.Helper()
	switch {
	case !shown.Note.drawn():
		t.Errorf("%s: the note is not drawn: %v", what, shown.Note)
	case opens == "below" && shown.Note.Y < shown.Door.Y+shown.Door.H:
		t.Errorf("%s: the note at %v is not below the door at %v",
			what, shown.Note, shown.Door)
	case opens == "above" && shown.Note.Y+shown.Note.H > shown.Door.Y:
		t.Errorf("%s: the note at %v is not above the door at %v",
			what, shown.Note, shown.Door)
	}
	for name, control := range map[string]box{"the email field": shown.Email,
		"Continue": shown.Continue, "Other ways": shown.Others} {
		if shown.Note.overlaps(control) {
			t.Errorf("%s: the note at %v covers %s at %v", what,
				shown.Note, name, control)
		}
	}
}

// clearsOnSendAndPress: sending the email form clears the message, and
// so does the next press, at its start, before it has an answer.
func clearsOnSendAndPress(t *testing.T, rig *harness.Rig, what string) {
	t.Helper()
	tap(t, rig, passkeyDoor)
	noteShown(t, rig, what)
	run(t, rig, chromedp.SetValue(chromedp.CSS("#rst-signin-email"),
		"ada@example.com"))
	tap(t, rig, continueButton)
	noteGone(t, rig, what, "the form sent")
	tap(t, rig, passkeyDoor)
	noteShown(t, rig, what)
	run(t, rig, chromedp.Evaluate[chromedp.Void](`window.__hold = true`))
	tap(t, rig, passkeyDoor)
	pressed := noteGone(t, rig, what, "pressed again")
	if pressed.CloseShown {
		t.Errorf("%s: a new press left the close button showing", what)
	}
}

// On the one-way screen the door leads and "Other ways to sign in"
// follows it, so the note opens above, over neither.
func TestTheOneWayPasskeyNoteOpensAbove(t *testing.T) {
	rig := noteRig(t, true)
	for _, size := range [][2]int64{{320, 640}, {1280, 800}} {
		what := fmt.Sprintf("the one-way passkey door at %dpx", size[0])
		run(t, rig, chromedp.EmulateViewport(size[0], size[1]))
		openDoor(t, rig, rig.Origin+"/signin?way=passkey")
		checkNote(t, rig, what, "above")
	}
}

func clearCookies(t *testing.T, rig *harness.Rig) {
	t.Helper()
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, network.ClearBrowserCookies,
			cdp.Empty{})
		return err
	}))
}

// Under reduced motion a busy Continue's spinner takes its label's
// place, as it does with motion: beside the label it pushed the words
// along inside the button while the proof computed (Fiesta 1.8.0).
func TestABusyContinueKeepsItsLabelUnderReducedMotion(t *testing.T) {
	app, build := newScreenApp(t)
	hold := &moduleHold{}
	rig := harness.New(t, hold.wrap(app, build))
	// After the rig, so it runs first: the rig's server waits for every
	// request, held ones too, before it closes.
	t.Cleanup(hold.release)
	run(t, rig, chromedp.EmulateViewport(320, 640),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, emulation.SetEmulatedMedia,
				emulation.SetEmulatedMediaParams{
					Features: []*emulation.MediaFeature{{
						Name: "prefers-reduced-motion", Value: "reduce"}}})
			return err
		}))
	run(t, rig, chromedp.Func(func(
		ctx context.Context, target *chromedp.Target,
	) error {
		_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{Source: busyLabel})
		return err
	}))
	// pow.js held, so the label is read idle before anything is busy.
	hold.arm(powModule)
	run(t, rig, chromedp.Navigate("about:blank"),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, page.Navigate,
				page.NavigateParams{URL: rig.Origin + "/signin-plain"})
			return err
		}),
		chromedp.WaitVisible(chromedp.CSS(continueButton)))
	idle := harness.Evaluate[string](rig, `window.__label()`)
	hold.release()
	run(t, rig, chromedp.Poll[chromedp.Void](`!!window.__busyLabel`,
		chromedp.WithPollingTimeout(10*time.Second)))
	busy := harness.Evaluate[string](rig, `window.__busyLabel`)
	if busy != idle {
		t.Errorf("under reduced motion Continue's label moved from %s "+
			"to %s when it went busy", idle, busy)
	}
}

// busyLabel reads where Continue's words are drawn, and keeps where
// they were the moment it first went busy: the solve at this
// difficulty can be over before a drive could look.
const busyLabel = `(() => {
  const button = () =>
    document.querySelector(` + "`" + continueButton + "`" + `);
  window.__label = () => {
    const text = [...button().childNodes].find((n) => n.nodeType === 3);
    const range = document.createRange();
    range.selectNodeContents(text);
    const r = range.getBoundingClientRect();
    return [r.x, r.y].join(",");
  };
  new MutationObserver(() => {
    const b = button();
    if (!window.__busyLabel && b && b.getAttribute("aria-busy") === "true")
      window.__busyLabel = window.__label();
  }).observe(document, { subtree: true, childList: true, attributes: true });
})()`
