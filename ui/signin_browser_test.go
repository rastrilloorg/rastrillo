//go:build browser

package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/harness"
)

// signinCases are the screens the layout drive measures, each rendered
// into its own container: the link one-tap (the label with a <bdi>),
// both passkey doors live (button hidden, its script found nothing) and
// in Preview (button shown), and a brand with every part.
func signinCases() []struct {
	id      string
	st      auth.SigninState
	preview bool
} {
	pk := remembered("passkey", "")
	return []struct {
		id      string
		st      auth.SigninState
		preview bool
	}{
		{"link", auth.SigninState{Step: auth.StepReturning, Remembered: remembered("magiclink", "grace@example.com")}, false},
		{"ask-live", auth.SigninState{Step: auth.StepAsk, Passkey: signinDoor}, false},
		{"ask-preview", auth.SigninState{Step: auth.StepAsk, Passkey: signinDoor}, true},
		{"passkey-live", auth.SigninState{Step: auth.StepReturning, Remembered: pk, Passkey: signinDoor}, false},
		{"passkey-preview", auth.SigninState{Step: auth.StepReturning, Remembered: pk, Passkey: signinDoor}, true},
	}
}

func signinPage(t *testing.T) http.Handler {
	t.Helper()
	var body strings.Builder
	for _, c := range signinCases() {
		data := signinData(c.st)
		data["Brand"] = map[string]any{
			"Name": "Harbour", "Pitch": "Moorings and berths, booked in a minute.",
			"Mark": template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor"/></svg>`),
		}
		if c.preview {
			data["Preview"] = true
		}
		// Each case renders the same ids; the drive scopes every query
		// to its container, and the page is never read by anything else.
		fmt.Fprintf(&body, `<div id="case-%s">%s</div>`, c.id, render(t, "signin", data))
	}
	return signinServer(body.String())
}

// signinServer serves body as the page at /, with the stylesheets and
// an empty passkey script beside it.
func signinServer(body string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, _ := ThemeCSS(ThemeNames()[0])
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	// The door's script, empty: the drive measures the door as a
	// browser leaves it when no WebAuthn is found, button still hidden.
	mux.HandleFunc("GET /static/passkey-signin.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
			`<title>signin</title><link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme.css"></head><body>`+body+`</body></html>`)
	})
	return mux
}

type signinLayout struct {
	// The link one-tap: the space between "as" and the address, and
	// one space's width in the button's font.
	LabelGap, SpaceWidth float64
	// Per case, the door's children the flex column lays out that have
	// no height — each still takes a gap.
	ZeroHeight map[string][]string
	// The Preview passkey door: the wrapper against its button while
	// the message is empty, and the space above the message once the
	// script has written to it.
	WrapperHeight, ButtonHeight, MessageSpacing float64
	// The brand column's type: pitch, name, and the door's heading.
	Pitch, Name, Heading float64
	// The mark's colour against the accent's.
	Mark, Accent string
}

// A signin card that looks right in the markup can still be wrong on
// screen, three ways, all seen in a screenshot of the gallery:
//
//   - the link one-tap's label split across the button's flex gap, a
//     double space before the address;
//   - an empty passkey message, or the wrapper of a hidden button,
//     taking one of the door's gaps for nothing;
//   - a brand pitch no larger than the name above it, so a brand with a
//     pitch reads as two labels rather than a line with a label.
func TestTheSigninCardLaysOutAsDesigned(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return signinPage(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	var got signinLayout
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`#case-link [rst-signin]`, chromedp.ByQuery),
		chromedp.Evaluate(`(function () {
  const out = { ZeroHeight: {} };
  const btn = document.querySelector("#case-link [rst-signin-form] button");
  const bdi = btn.querySelector("bdi");
  const node = bdi.previousSibling;
  const end = node.data.trimEnd().length;
  const r = document.createRange();
  r.setStart(node, end - 1);
  r.setEnd(node, end);
  out.LabelGap = bdi.getBoundingClientRect().left - r.getBoundingClientRect().right;
  const ctx = document.createElement("canvas").getContext("2d");
  ctx.font = getComputedStyle(bdi).font;
  out.SpaceWidth = ctx.measureText(" ").width;

  for (const c of document.querySelectorAll("[id^=case-]")) {
    const door = c.querySelector("[rst-signin-door]");
    out.ZeroHeight[c.id] = [...door.children]
      .filter(el => getComputedStyle(el).display !== "none" && el.getBoundingClientRect().height === 0)
      .map(el => el.outerHTML.slice(0, 80));
  }

  const wrap = document.querySelector("#case-passkey-preview [rst-signin-passkey]");
  const pk = wrap.querySelector("button");
  out.WrapperHeight = wrap.getBoundingClientRect().height;
  out.ButtonHeight = pk.getBoundingClientRect().height;
  const msg = wrap.querySelector("[rst-signin-passkey-msg]");
  msg.textContent = "No passkey was used.";
  out.MessageSpacing = msg.getBoundingClientRect().top - pk.getBoundingClientRect().bottom;

  const size = sel => parseFloat(getComputedStyle(document.querySelector("#case-link " + sel)).fontSize);
  out.Pitch = size("[rst-signin-pitch]");
  out.Name = size("[rst-signin-name]");
  out.Heading = size("h1");
  out.Mark = getComputedStyle(document.querySelector("#case-link [rst-signin-mark]")).color;
  const probe = document.createElement("span");
  probe.style.color = "var(--rst-accent)";
  document.body.appendChild(probe);
  out.Accent = getComputedStyle(probe).color;
  return out;
})()`, &got),
	); err != nil {
		t.Fatal(err)
	}

	if got.SpaceWidth <= 0 {
		t.Fatalf("measured a space as %.2fpx; the drive is not reading the button's font", got.SpaceWidth)
	}
	if got.LabelGap < got.SpaceWidth/2 || got.LabelGap > got.SpaceWidth*1.5 {
		t.Errorf("the link one-tap has %.2fpx between \"as\" and the address, and a space is %.2fpx; want one space", got.LabelGap, got.SpaceWidth)
	}
	for id, zero := range got.ZeroHeight {
		if len(zero) > 0 {
			t.Errorf("%s: the door lays out children with no height, each taking a gap: %v", id, zero)
		}
	}
	if d := got.WrapperHeight - got.ButtonHeight; d < -0.5 || d > 0.5 {
		t.Errorf("with its message empty the passkey pair is %.2fpx tall and its button %.2fpx; the empty message is taking space", got.WrapperHeight, got.ButtonHeight)
	}
	if got.MessageSpacing < 4 {
		t.Errorf("a written passkey message sits %.2fpx under its button; it needs space once it has something to say", got.MessageSpacing)
	}
	if got.Pitch <= got.Heading || got.Pitch <= got.Name {
		t.Errorf("the pitch is %.1fpx, the door's heading %.1fpx and the name %.1fpx; the pitch is the column's dominant line", got.Pitch, got.Heading, got.Name)
	}
	if got.Mark != got.Accent {
		t.Errorf("the mark's colour is %s, the accent %s; a currentColor mark takes the theme's accent", got.Mark, got.Accent)
	}
}

// fitJS reports, per case container, every button and form control in
// the card whose box leaves the card's content box by more than half a
// pixel on either side. Hidden controls have no box and are skipped.
const fitJS = `(function () {
  const out = {};
  for (const c of document.querySelectorAll("[id^=case-]")) {
    const card = c.querySelector("[rst-signin]");
    const cs = getComputedStyle(card);
    const r = card.getBoundingClientRect();
    const left = r.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft);
    const right = r.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight);
    const bad = [];
    for (const el of card.querySelectorAll("[rst-btn], input:not([type=hidden]), select, textarea")) {
      const b = el.getBoundingClientRect();
      if (b.width === 0 && b.height === 0) continue;
      if (b.left < left - 0.5 || b.right > right + 0.5) {
        bad.push(el.outerHTML.slice(0, 60) + " spans " + b.left.toFixed(1) + ".." + b.right.toFixed(1) +
          " in " + left.toFixed(1) + ".." + right.toFixed(1));
      }
    }
    if (bad.length) out[c.id + " (" + c.title + ")"] = bad;
  }
  return out;
})()`

// A full-width control drawn wider than its card pokes out past the
// card's edge on a phone. It was seen on Continue's link: an <a> is
// content-box, unlike a <button>, so width: 100% plus its padding and
// border is wider than the column it fills. Every state, live and in
// Preview (where the passkey button shows), at the two phone widths
// the gallery and WCAG's reflow measure.
func TestSigninControlsStayInsideTheCardOnAPhone(t *testing.T) {
	var body strings.Builder
	for i, c := range signinStates() {
		for _, preview := range []bool{false, true} {
			// Continue's live page refreshes to keymail at once and would
			// take the drive with it; its Preview is the same card with
			// the link pointing at #.
			if !preview && c.st.Door() == "continue" {
				continue
			}
			data := signinData(c.st)
			data["Brand"] = map[string]any{"Name": "Harbour", "Pitch": "Moorings and berths, booked in a minute."}
			if preview {
				data["Preview"] = true
			}
			fmt.Fprintf(&body, `<div id="case-%d-%v" title=%q>%s</div>`, i, preview, c.name, render(t, "signin", data))
		}
	}
	rig := harness.New(t, func(string) http.Handler { return signinServer(body.String()) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	for _, width := range []int64{320, 390} {
		var got map[string][]string
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 900),
			chromedp.Navigate(rig.Origin+"/"),
			chromedp.WaitVisible(`#case-0-false [rst-signin]`, chromedp.ByQuery),
			chromedp.Evaluate(fitJS, &got),
		); err != nil {
			t.Fatal(err)
		}
		for id, bad := range got {
			t.Errorf("%dpx, %s: controls outside the card's content box:\n\t%s", width, id, strings.Join(bad, "\n\t"))
		}
	}
}

// orState is one door's divider as the browser draws it.
type orState struct {
	Shown bool
	// The rule on each side of the word, and the word's and the rules'
	// colours against the theme's muted text and hairline.
	Before, After             float64
	Colour, Muted, Rule, Line string
}

// The divider says "or" between two ways in, so it shows exactly when
// the passkey button does: hidden while the live door's script has not
// revealed the button (here, an empty script that never will), shown in
// Preview, and shown the moment a script unhides the button — the
// wrapper's :has() rule, not a second script, is what brings it back.
func TestTheOrDividerShowsOnlyWithThePasskeyButton(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return signinPage(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()

	const read = `(function () {
  const probe = document.createElement("span");
  probe.style.color = "var(--rst-text-muted)";
  document.body.appendChild(probe);
  const muted = getComputedStyle(probe).color;
  probe.style.color = "var(--rst-line)";
  const line = getComputedStyle(probe).color;
  probe.remove();
  const out = {};
  for (const id of ["ask-live", "ask-preview", "passkey-live", "passkey-preview"]) {
    const or = document.querySelector("#case-" + id + " [rst-signin-or]");
    if (!or) { out[id] = null; continue; }
    const r = or.getBoundingClientRect();
    out[id] = {
      Shown: getComputedStyle(or).display !== "none" && r.height > 0,
      Before: parseFloat(getComputedStyle(or, "::before").width) || 0,
      After: parseFloat(getComputedStyle(or, "::after").width) || 0,
      Colour: getComputedStyle(or).color, Muted: muted,
      Rule: getComputedStyle(or, "::before").borderTopColor + " " + getComputedStyle(or, "::after").borderTopColor, Line: line,
    };
  }
  return out;
})()`
	var before, after map[string]*orState
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`#case-ask-preview [rst-signin]`, chromedp.ByQuery),
		chromedp.Evaluate(read, &before),
		// What the door's script does once it has found a working
		// WebAuthn: it unhides the button, and nothing else.
		chromedp.Evaluate(`document.querySelectorAll("[data-rst-passkey]").forEach(b => { b.hidden = false; }); "ok"`, new(string)),
		chromedp.Evaluate(read, &after),
	); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ask-live", "ask-preview", "passkey-live", "passkey-preview"} {
		b, a := before[id], after[id]
		if b == nil {
			t.Errorf("%s: no divider on the page", id)
			continue
		}
		live := strings.HasSuffix(id, "-live")
		if b.Shown == live {
			t.Errorf("%s: the divider shown is %v with the passkey button hidden %v; it goes with the button", id, b.Shown, live)
		}
		if !a.Shown {
			t.Errorf("%s: the passkey button is showing and the divider is not", id)
			continue
		}
		if a.Before < 8 || a.After < 8 {
			t.Errorf("%s: the divider's rules are %.1fpx and %.1fpx; want a line on each side of the word", id, a.Before, a.After)
		}
		if a.Colour != a.Muted {
			t.Errorf("%s: the divider's text is %s, the muted text colour %s", id, a.Colour, a.Muted)
		}
		if a.Rule != a.Line+" "+a.Line {
			t.Errorf("%s: the divider's rules are %s, the hairline %s", id, a.Rule, a.Line)
		}
	}
}
