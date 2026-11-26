//go:build browser

package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// With pow.js refused, as a network failure or a CSP would, Continue
// can never be enabled, so the card says why: pow's hint, a live region
// hidden from sight until then, is drawn. This is the one case where
// the card may move.
func TestABlockedProofScriptExplainsTheSigninForm(t *testing.T) {
	app, build := newScreenApp(t)
	hold := &moduleHold{blocked: powModule}
	rig := harness.New(t, hold.wrap(app, build))
	run(t, rig, chromedp.EmulateViewport(320, 640),
		chromedp.Navigate(rig.Origin+"/signin-plain"),
		chromedp.WaitVisible(chromedp.CSS(continueButton)),
		chromedp.Sleep(500*time.Millisecond))
	var got struct {
		Status   string
		Disabled bool
	}
	eval(t, rig, `(() => {
	  const s = document.querySelector("form[action='/signin'] "+
	    "[data-pow-status]");
	  const r = s.getBoundingClientRect();
	  return {Disabled: document.querySelector(`+jsString(continueButton)+
		`).disabled, Status: [r.width > 1 && s.checkVisibility() ?
	    "drawn" : "not drawn", s.getAttribute("role") || "no role",
	    s.textContent].join(" | ")};
	})()`, &got)
	if !strings.HasPrefix(got.Status, "drawn | status | ") ||
		!strings.Contains(got.Status, "reload the page") {
		t.Errorf("with pow.js blocked the status is %q, want the hint "+
			"drawn in a status region", got.Status)
	}
	if !got.Disabled {
		t.Error("with pow.js blocked Continue is enabled")
	}
}
