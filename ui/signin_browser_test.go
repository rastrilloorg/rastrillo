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
			`<link rel="stylesheet" href="/theme.css"></head><body>`+body.String()+`</body></html>`)
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
