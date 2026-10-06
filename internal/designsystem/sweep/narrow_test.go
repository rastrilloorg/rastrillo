//go:build browser

package sweep

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/designsystem"
	"amadan.net/rastrillo/rastrillo/internal/designsystem/galleryrig"
)

// narrowExempt is the page kinds the narrow drive does not measure, each
// with its reason. An entry naming a page that does display a preview
// below 800px fails the drive, so the list cannot grow quietly.
var narrowExempt = map[string]string{
	"overview": "below 800px the Overview is the phone index: its main, which frames the demo application, is display: none, so no preview is displayed to measure. The demo's own pages are checked at phone width by TestA11yReflowsAt320",
}

// narrowLocales is the routine run's languages for the narrowest-frame
// height drive: en, ar, and the worst wrapper(s) found by one full
// sweep (make browser-sweep, RASTRILLO_SWEEP=full) comparing, per page,
// how far each language's content height exceeds en's. sweepFull()
// runs all twelve, which is owed before any release that changes locale
// strings or preview content.
//
// measured 2026-10-06: ru at most 85px taller than en (partial-badge),
// ga 69px (shell-column and shell-topbar, tied) — the two widest margins
// of the twelve, and each stresses different previews, so routine CI
// still exercises both.
func narrowLocales() []string {
	if sweepFull() {
		return rastrillo.BaseLocales()
	}
	return []string{"en", "ar", "ru", "ga"}
}

// The two runs' deadlines, each about four times its own measured time
// on a quiet runner: the routine subset, and the twelve-language sweep,
// which does three times the work and would be killed by the routine's
// deadline. Twice was not enough: CI runners here sit at load 40 to 80,
// several times the load of the measurements below.
//
// measured 2026-10-06 on the quietest run of several, load average
// given as its own 1-minute figure at the moment the run started: 26.14s
// for the routine subset (en, ar, ru, ga) at load 5.16, 73.79s for all
// twelve at load 4.20.
const (
	routineDeadline = 105 * time.Second
	sweepDeadline   = 300 * time.Second
)

// TestPreviewFrameHeightsFitAtThePhonesNarrowest holds every Mobile
// frame to its box at the narrowest frame the gallery commits to: a
// 320px viewport, the reflow width. Below 390px a Mobile frame lays out
// at the stage's width, so text that fits a line at 390px can wrap at
// 305px, and a height measured at 390px is no longer a ceiling.
func TestPreviewFrameHeightsFitAtThePhonesNarrowest(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return treeHandler(t) })
	locales, deadline := narrowLocales(), routineDeadline
	if sweepFull() {
		deadline = sweepDeadline
	}
	ctx, cancel := context.WithTimeout(rig.Context(), deadline)
	defer cancel()
	root := designsystem.RootTheme()
	covered := map[string]bool{}
	for _, row := range heightRows() {
		covered[row.Kind] = true
	}
	for _, kind := range designsystem.PageKinds() {
		if !covered[kind] && galleryrig.FramesNothing[kind] == "" {
			t.Errorf("page kind %q has no height row and is not listed as framing nothing", kind)
		}
	}
	// The exemptions, held to their reason: no visible preview at 320px.
	for kind := range narrowExempt {
		var shown int
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 844),
			chromedp.Navigate(rig.Origin+pageHref(mountPath, root, "en", fileOf(kind))),
			chromedp.WaitReady(`body`, chromedp.ByQuery),
			chromedp.Evaluate(`[...document.querySelectorAll(".ds-view__frame")].filter(f => f.getBoundingClientRect().height > 0).length`, &shown)); err != nil {
			t.Fatalf("exempt %s at 320px: %v", kind, err)
		}
		if shown > 0 {
			t.Errorf("narrowExempt names %s, which shows %d previews at 320px; measure it instead", kind, shown)
		}
	}

	worst := map[string]int{}               // id → the largest need across the languages run
	box := map[string]int{}                 // id → its Mobile box
	shell := map[string]bool{}              // id → frames a sidebar-shell page
	byLocale := map[string]map[string]int{} // locale → id → need, for the sweep's comparison
	for _, locale := range locales {
		byLocale[locale] = map[string]int{}
		for _, row := range heightRows() {
			if narrowExempt[row.Kind] != "" {
				continue
			}
			where := fmt.Sprintf("%s/%s at 320px", locale, row.Kind)
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(320, 844),
				chromedp.Navigate(rig.Origin+pageHref(mountPath, root, locale, fileOf(row.Kind))),
				chromedp.WaitVisible(`.ds-view__frame`, chromedp.ByQuery)); err != nil {
				t.Fatalf("%s: loading: %v", where, err)
			}
			eagerly(t, ctx, where)
			if err := chromedp.Run(ctx, chromedp.Evaluate(clickedMobile, nil)); err != nil {
				t.Fatalf("%s: choosing Mobile: %v", where, err)
			}
			mobileSettle(t, ctx, where)
			// The control: every frame is laid out at its stage's width,
			// under 390px, so this is the narrow frame and not the 390px
			// one the wide drive already measures.
			var wide []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll(".ds-view")].filter(v => {
			  const f = v.querySelector(".ds-view__frame"); return !(f.offsetWidth < 390 && Math.abs(f.offsetWidth - v.clientWidth) <= 1); }).map(v => (v.closest("article, section") || {}).id)`, &wide)); err != nil {
				t.Fatalf("%s: checking the frame widths: %v", where, err)
			}
			if len(wide) > 0 {
				t.Fatalf("%s: frames %v are not at the stage's narrow width; the readings would be of the 390px frame", where, wide)
			}
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(galleryrig.MeasureFrames, &raw)); err != nil {
				t.Fatalf("%s: measuring: %v", where, err)
			}
			// Read, not judged: every language is measured before any
			// height is asserted, so one run gives the whole table.
			got := galleryrig.ReadFrames(t, where, raw)
			if len(got) < row.Least {
				t.Errorf("%s: %d sections measured, want at least %d: %s", where, len(got), row.Least, row.Owed)
			}
			for id, r := range got {
				byLocale[locale][id] = r[0]
				box[id], shell[id] = r[1], r[2] == 1
				if r[0] > worst[id] {
					worst[id] = r[0]
				}
			}
		}
	}
	ids := make([]string, 0, len(worst))
	for id := range worst {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// The only height assertion, made once over every language run. A
	// component preview gets no grace, so nothing in it is reached by
	// scrolling inside it; a sidebar-shell page keeps the 48px its
	// 100dvh rail always costs.
	for _, id := range ids {
		grace := 0
		if shell[id] {
			grace = 48
		}
		if worst[id] > box[id]+grace {
			t.Errorf("%s: at the narrowest phone frame its document needs %dpx in its tallest language and its Mobile box is %dpx; set previewMobileHeights[%q] to at least %d", id, worst[id], box[id], id, worst[id]+20)
		}
	}
	// What the sweep is for: per language, the most any preview's height
	// exceeds en's. The routine subset is the worst one or two of these.
	for _, locale := range locales {
		// Ties go to the first id in order, not to map order, so the line
		// names the same preview on every run: ga's two shells tie.
		most, at := 0, ""
		for id, need := range byLocale[locale] {
			if d := need - byLocale["en"][id]; d > most || d == most && d > 0 && id < at {
				most, at = d, id
			}
		}
		t.Logf("%s: at most %dpx taller than en (%s)", locale, most, at)
	}
}
