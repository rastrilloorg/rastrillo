//go:build browser

package ui

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestATextareaGrowsWithWhatIsTypedInIt: rst-textarea is as tall as its
// text, from its 5rem minimum to half the viewport, and scrolls past
// that, so a long paste cannot push the rest of the form off the
// screen. Read in both spellings at 1280 with a mouse and 390 with
// touch: empty, two lines (still the minimum), a dozen (taller than
// the minimum, shorter than the cap) and two hundred (the cap, with the
// rest scrolled).
func TestATextareaGrowsWithWhatIsTypedInIt(t *testing.T) {
	// markup-spelling: old-spelling begin — both spellings grow, and
	// apps still write the class one.
	body := `<div rst-page><textarea rst-textarea id="attr" name="a"></textarea><textarea class="rst-textarea" id="class" name="c"></textarea></div>`
	// markup-spelling: old-spelling end
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280x900 mouse", 1280, 900, false}, {"390x844 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, leg.coarse, map[string]string{"/": sizingDoc("textarea", body)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitReady("#class", chromedp.ByQuery))
			requirePointer(t, ctx, leg.coarse)
			for _, id := range []string{"attr", "class"} {
				var g struct {
					Min, Cap                  float64
					Empty, Few, Dozen, Many   float64
					ManyScrolls, DozenScrolls bool
				}
				at(t, ctx, `(() => {
				  const ta = document.getElementById("`+id+`"), rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
				  const h = n => { ta.value = Array.from({length: n}, (_, i) => "line " + (i + 1)).join("\n"); ta.dispatchEvent(new Event("input", {bubbles: true})); return ta.getBoundingClientRect().height; };
				  const out = {Min: 5 * rem, Cap: innerHeight / 2, Empty: h(0), Few: h(2)};
				  out.Dozen = h(12); out.DozenScrolls = ta.scrollHeight > ta.clientHeight + 1;
				  out.Many = h(200); out.ManyScrolls = ta.scrollHeight > ta.clientHeight + 1;
				  return JSON.stringify(out);
				})()`, &g)
				where := leg.name + " #" + id
				if math.Abs(g.Empty-g.Min) > 0.5 || math.Abs(g.Few-g.Min) > 0.5 {
					t.Errorf("%s: empty it is %.1fpx and with two lines %.1fpx; both should be the %.0fpx minimum", where, g.Empty, g.Few, g.Min)
				}
				if g.Dozen <= g.Min+0.5 || g.Dozen >= g.Cap || g.DozenScrolls {
					t.Errorf("%s: with twelve lines it is %.1fpx (scrolls %v); it should grow past the %.0fpx minimum to fit them, under the %.0fpx cap", where, g.Dozen, g.DozenScrolls, g.Min, g.Cap)
				}
				if math.Abs(g.Many-g.Cap) > 0.5 || !g.ManyScrolls {
					t.Errorf("%s: with two hundred lines it is %.1fpx (scrolls %v); it should stop at half the viewport, %.0fpx, and scroll", where, g.Many, g.ManyScrolls, g.Cap)
				}
			}
		})
	}
}
