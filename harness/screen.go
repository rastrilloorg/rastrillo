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
// selector, then flush the problem list — any accumulated problem
// fails the test naming the screen it surfaced on. "body" is the
// whole-page case for rastrillo's server-rendered apps, which have no
// #app convention to hard-fail on.
func (r *Rig) Screen(selector, note string) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, screenBudget)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
		r.fail(selector, note, fmt.Sprintf("screen %q never arrived: %v", selector, err))
	}
	if probs := r.take(); len(probs) > 0 {
		r.fail(selector, note, "the page reported problems:\n  "+strings.Join(probs, "\n  "))
	}
}

// fail fails the test with what was on screen: a failure report always
// includes what a person would have seen.
func (r *Rig) fail(selector, note, msg string) {
	r.t.Helper()
	r.t.Fatalf("harness: %s: %s\non screen:\n%s", note, msg, r.onScreen(selector))
}
