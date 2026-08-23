//go:build browser

package harness

import (
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// junkValues is what a missing field or an unhandled shape looks like
// once it has been through String() — the bug class that renders
// perfectly and says nothing. Kass's full set. Substring "null" can
// false-positive on legitimate prose, which is why every hit carries
// its surrounding text and AllowText exists for the rare deliberate
// case.
var junkValues = []string{"undefined", "null", "[object Object]", "NaN"}

// AllowText exempts one exact string from the junk scan — for the
// screen whose legitimate prose contains a junk substring ("null and
// void"). The allowance is the surrounding string, not the junk value:
// everything else on the screen is still scanned.
func (r *Rig) AllowText(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowedText = append(r.allowedText, s)
}

// junkScanJS reads the screen the way a person would, plus the places
// a person cannot see: input and textarea values, and the labels a
// screen reader announces. The scan root is the screen's own selector
// ("body" is the whole-page case). Each hit carries its surrounding
// text so a prose false-positive is legible at a glance.
const junkScanJS = `((sel, junk, allowed) => {
  const root = document.querySelector(sel);
  if (!root) return ["the junk-scan root " + sel + " is not on the page"];
  const hay = [root.textContent ?? ""];
  for (const f of root.querySelectorAll("input, textarea")) hay.push(f.value ?? "");
  for (const n of root.querySelectorAll("[aria-label]")) hay.push(n.getAttribute("aria-label") ?? "");
  const hits = [];
  for (let text of hay) {
    for (const a of allowed) text = text.split(a).join("");
    for (const j of junk) {
      const at = text.indexOf(j);
      if (at < 0) continue;
      const around = text.slice(Math.max(0, at - 40), at + j.length + 40).replace(/\s+/g, " ").trim();
      hits.push(j + ' in: "' + around + '"');
    }
  }
  return hits;
})`

// junkHits runs the scan rooted at selector and returns what it found.
func (r *Rig) junkHits(selector string) []string {
	r.t.Helper()
	r.mu.Lock()
	allowed := append([]string(nil), r.allowedText...)
	r.mu.Unlock()
	if allowed == nil {
		allowed = []string{} // marshal as [], never null — JS iterates it
	}
	var args []string
	for _, v := range []any{selector, junkValues, allowed} {
		b, err := json.Marshal(v)
		if err != nil {
			r.t.Fatalf("harness: junk scan args: %v", err)
		}
		args = append(args, string(b))
	}
	var hits []string
	expr := fmt.Sprintf("%s(%s, %s, %s)", junkScanJS, args[0], args[1], args[2])
	if err := chromedp.Run(r.ctx, chromedp.Evaluate(expr, &hits)); err != nil {
		r.t.Fatalf("harness: junk scan: %v", err)
	}
	return hits
}
