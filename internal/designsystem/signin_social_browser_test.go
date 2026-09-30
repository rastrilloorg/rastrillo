//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The social sign-in screen is markup an app copies, so what it looks
// like in the frame is what it will look like in the app. Its first
// cut shipped two forms that touched — each button's border drawn on
// the other's — and two text-only buttons under a blurb promising the
// buttons "drawn the way each company requires". Neither shows up in
// the bytes: a gap is a computed box, and an icon that is present but
// sized off the text, pushed off-centre or painted in a fixed colour
// that vanishes on a dark surface is still present. So this reads the
// rendered frame, in both schemes, because the Apple mark following
// the button's text colour is only proven by a scheme that changes it.
func TestTheSocialSignInButtonsStackAndCarryTheirMarks(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1500, 1000),
		chromedp.Navigate(rig.Origin+pageHref(mountPath, RootTheme(), "en", fileOf("screens"))),
		chromedp.WaitVisible(`#screen-signin-social .ds-view__frame`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
		  const f = document.querySelector("#screen-signin-social .ds-view__frame");
		  f.loading = "eager"; f.scrollIntoView();
		  return "ok";
		})()`, new(string)),
		chromedp.Poll(`(() => {
		  const d = document.querySelector("#screen-signin-social .ds-view__frame").contentDocument;
		  return !!d && d.readyState === "complete" && !!d.querySelector("[rst-btn]");
		})()`, nil, chromedp.WithPollingTimeout(30*time.Second)),
	); err != nil {
		t.Fatalf("loading the social sign-in frame: %v", err)
	}

	type mark struct {
		Label         string
		FirstIsSVG    bool
		Hidden        string
		Focusable     string
		IconH, FontPx float64
		CentreOff     float64 // icon's vertical centre minus the button's
		LabelGap      float64 // icon's end edge to the label's start
		BtnGap        float64 // the button's own column gap
		Fill, Colour  string  // the last path's computed fill, the button's text colour
	}
	type reading struct {
		Buttons   int
		Gap, Sp3  float64
		Widths    []float64
		Container float64
		Marks     []mark
	}

	for _, scheme := range []string{"light", "dark"} {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		  const d = document.querySelector("#screen-signin-social .ds-view__frame").contentDocument;
		  d.documentElement.setAttribute("data-theme", `+"`"+scheme+"`"+`);
		  const probe = d.createElement("div");
		  probe.style.blockSize = "var(--rst-sp-3)";
		  d.body.append(probe);
		  const sp3 = probe.getBoundingClientRect().height;
		  probe.remove();
		  const btns = [...d.querySelectorAll("button[rst-btn]")];
		  const r = btns.map(b => b.getBoundingClientRect());
		  const wrap = btns.length ? btns[0].closest("form").parentElement : null;
		  const cs = wrap ? getComputedStyle(wrap) : null;
		  const inner = wrap ? wrap.getBoundingClientRect().width - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) : 0;
		  const marks = btns.map((b, i) => {
		    const svg = b.firstElementChild;
		    const isSVG = !!svg && svg.localName === "svg";
		    const text = [...b.childNodes].find(n => n.nodeType === 3 && n.textContent.trim());
		    const out = { Label: b.textContent.trim(), FirstIsSVG: isSVG, FontPx: parseFloat(getComputedStyle(b).fontSize),
		      BtnGap: parseFloat(getComputedStyle(b).columnGap), Colour: getComputedStyle(b).color };
		    if (!isSVG) return out;
		    const s = svg.getBoundingClientRect();
		    out.Hidden = svg.getAttribute("aria-hidden") || "";
		    out.Focusable = svg.getAttribute("focusable") || "";
		    out.IconH = s.height;
		    out.CentreOff = (s.top + s.height / 2) - (r[i].top + r[i].height / 2);
		    if (text) {
		      const range = d.createRange();
		      range.selectNodeContents(text);
		      out.LabelGap = range.getBoundingClientRect().left - s.right;
		    }
		    const paths = svg.querySelectorAll("path");
		    if (paths.length) out.Fill = getComputedStyle(paths[paths.length - 1]).fill;
		    return out;
		  });
		  return JSON.stringify({
		    Buttons: btns.length,
		    Gap: btns.length > 1 ? r[1].top - r[0].bottom : -1,
		    Sp3: sp3,
		    Widths: r.map(x => x.width),
		    Container: inner,
		    Marks: marks,
		  });
		})()`, &raw)); err != nil {
			t.Fatalf("%s: reading the frame: %v", scheme, err)
		}
		var got reading
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: %v in %s", scheme, err, raw)
		}
		if got.Buttons != 2 {
			t.Fatalf("%s: %d provider buttons, want Google and Apple", scheme, got.Buttons)
		}
		// The shipped card spaces its controls by --rst-sp-3; two
		// buttons with no gap read as one control split in half.
		if math.Abs(got.Gap-got.Sp3) > 0.5 {
			t.Errorf("%s: the buttons are %.1fpx apart, want the card's --rst-sp-3 (%.1fpx)", scheme, got.Gap, got.Sp3)
		}
		for i, w := range got.Widths {
			if math.Abs(w-got.Container) > 0.5 {
				t.Errorf("%s: button %d is %.1fpx wide in a %.1fpx stack; the two only align if both fill it", scheme, i, w, got.Container)
			}
		}
		for i, m := range got.Marks {
			want := []string{"Continue with Google", "Continue with Apple"}[i]
			if m.Label != want {
				t.Errorf("%s: button %d says %q, want %q", scheme, i, m.Label, want)
			}
			if !m.FirstIsSVG {
				t.Errorf("%s: %q has no logo before its label", scheme, m.Label)
				continue
			}
			if m.Hidden != "true" || m.Focusable != "false" {
				t.Errorf("%s: %q's logo is aria-hidden=%q focusable=%q; the label already names the provider, so the logo must be silent and untabbable",
					scheme, m.Label, m.Hidden, m.Focusable)
			}
			if math.Abs(m.IconH-1.1*m.FontPx) > 1 {
				t.Errorf("%s: %q's logo is %.1fpx tall on %.1fpx text; want about 1.1em", scheme, m.Label, m.IconH, m.FontPx)
			}
			if math.Abs(m.CentreOff) > 1 {
				t.Errorf("%s: %q's logo sits %.1fpx off the button's vertical centre", scheme, m.Label, m.CentreOff)
			}
			if math.Abs(m.LabelGap-m.BtnGap) > 1 {
				t.Errorf("%s: %q's logo is %.1fpx from its label; rst-btn's own gap is %.1fpx", scheme, m.Label, m.LabelGap, m.BtnGap)
			}
		}
		// Apple's mark is one path in currentColor: a fixed fill would
		// be black on the dark scheme's dark button, i.e. invisible.
		if a := got.Marks[1]; a.FirstIsSVG && a.Fill != a.Colour {
			t.Errorf("%s: the Apple logo is filled %s on a button whose text is %s; it must follow the text colour", scheme, a.Fill, a.Colour)
		}
	}
}
