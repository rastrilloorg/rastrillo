//go:build browser

package harness

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// screenBudget bounds the wait for a screen to arrive. Wall-clock
// against a real browser: generous enough for a loaded CI box, and far
// faster than Go's default test timeout — a missing screen fails as
// itself, not as a hung suite (ui/browser_test.go's 60s reasoning,
// kept).
const screenBudget = 60 * time.Second

// Screen is the gate a drive passes at every screen boundary: wait for
// selector, run the junk scan, then flush the problem list — any
// accumulated problem fails the test naming the screen it surfaced on.
// "body" is the whole-page case for rastrillo's server-rendered apps,
// which have no #app convention to hard-fail on.
func (rig *Rig) Screen(selector, note string) {
	rig.t.Helper()
	ctx, cancel := context.WithTimeout(rig.ctx, screenBudget)
	defer cancel()
	if err := chromedp.Do(ctx, chromedp.WaitVisible(chromedp.CSS(selector))); err != nil {
		rig.fail(selector, note,
			fmt.Sprintf("screen %q never arrived: %v", selector, err))
	}
	if hits := rig.junkHits(selector); len(hits) > 0 {
		rig.fail(selector, note, "a JS value leaked to the screen:\n  "+
			strings.Join(hits, "\n  "))
	}
	if probs := rig.take(); len(probs) > 0 {
		rig.fail(selector, note, "the page reported problems:\n  "+
			strings.Join(probs, "\n  "))
	}
}

// fail fails the test with what was on screen: a failure report always
// includes what a person would have seen.
func (rig *Rig) fail(selector, note, msg string) {
	rig.t.Helper()
	rig.t.Fatalf("harness: %s: %s\non screen:\n%s",
		note, msg, rig.onScreen(selector))
}
