//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// readType loads path at w×h and returns the token and field readings.
func readType(t *testing.T, ctx context.Context, url string, w, h int64) (typeReading, []fontReading, float64) {
	t.Helper()
	var rawType, rawFonts string
	var primary float64
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(w, h),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(tokensJS, &rawType),
		chromedp.Evaluate(fontsJS, &rawFonts),
		chromedp.Evaluate(`parseFloat(getComputedStyle(document.getElementById("sizing-primary")).fontSize)`, &primary),
	); err != nil {
		t.Fatalf("reading type at %dx%d: %v", w, h, err)
	}
	var tr typeReading
	var fonts []fontReading
	if err := json.Unmarshal([]byte(rawType), &tr); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rawFonts), &fonts); err != nil {
		t.Fatal(err)
	}
	// The small-parent fields and the bare and primary inputs are the
	// cases this drive exists for; each is required by name, so losing
	// one from the fixture fails here rather than shrinking the sweep.
	named := map[string]bool{}
	for _, f := range fonts {
		named[f.Name] = true
	}
	for _, id := range []string{"sizing-bulk-search", "sizing-bulk-input", "sizing-bulk-note", "sizing-menu-search", "sizing-menu-input", "sizing-menu-note", "sizing-bare", "sizing-primary"} {
		if !named[id] {
			t.Fatalf("the sizing page has no %s; the fixture is not the one this drive measures", id)
		}
	}
	return tr, fonts, primary
}

// TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens is §10.1's type
// half: at 390 with touch, at 1024 with touch (the pointer half of the
// query alone) and at 600 with a mouse (the width half alone), the four
// tokens are one step up, every text-entry control is at least 16px —
// including the ones in a bulk bar and a menu panel — and the primary
// field is exactly --rst-fs-lg, 19px (the 1em bug gave 16).
func TestTextControlsAreSixteenPixelsOnSmallOrTouchScreens(t *testing.T) {
	pages := map[string]string{"/": sizingDoc("sizing", sizingFixture(t))}
	for _, coarse := range []bool{true, false} {
		rig := sizingRig(t, coarse, pages)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		legs := []struct {
			name string
			w, h int64
		}{{"600x800 mouse, the width half alone", 600, 800}}
		if coarse {
			legs = []struct {
				name string
				w, h int64
			}{{"390x844 touch", 390, 844}, {"1024x768 touch, the pointer half alone", 1024, 768}}
		}
		for _, leg := range legs {
			tr, fonts, primary := readType(t, ctx, rig.Origin+"/", leg.w, leg.h)
			requirePointer(t, ctx, coarse)
			// Each leg exists to exercise one half of the query: the 1024
			// touch leg proves the pointer half only if the page really is
			// wider than 40rem, and the 600 mouse leg the width half only if
			// the pointer really is fine.
			if tr.Width != int(leg.w) || tr.Coarse != coarse {
				t.Fatalf("%s: the page read width %d, coarse %v; this leg needs width %d, coarse %v", leg.name, tr.Width, tr.Coarse, leg.w, coarse)
			}
			if tr.Lg != 19 || tr.Base != 16 || tr.Sm != 14 || tr.Xs != 13 || tr.Body != 16 {
				t.Errorf("%s: tokens %v/%v/%v/%v body %v, want 19/16/14/13 body 16", leg.name, tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
			}
			for _, f := range fonts {
				if f.Px < 16 {
					t.Errorf("%s: %s is %vpx; a phone zooms the page when it is focused", leg.name, f.Name, f.Px)
				}
			}
			if primary != 19 {
				t.Errorf("%s: the primary field is %vpx, want --rst-fs-lg (19px); 16 is the 1em trap", leg.name, primary)
			}
		}
		cancel()
	}
}

// TestDesktopDensityIsPinned is §10.1's 1280×900 mouse leg: every value
// below is today's, measured, and any change fails. Task 4 adds the
// control sizes and Task 5 the whole-row changes the operator approved.
func TestDesktopDensityIsPinned(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": sizingDoc("sizing", sizingFixture(t))})
	ctx, cancel := context.WithTimeout(rig.Context(), 90*time.Second)
	defer cancel()
	tr, fonts, primary := readType(t, ctx, rig.Origin+"/", 1280, 900)
	requirePointer(t, ctx, false)
	if tr.Lg != 17 || tr.Base != 14 || tr.Sm != 12.5 || tr.Xs != 11.5 || tr.Body != 14 {
		t.Errorf("desktop tokens %v/%v/%v/%v body %v, want 17/14/12.5/11.5 body 14", tr.Lg, tr.Base, tr.Sm, tr.Xs, tr.Body)
	}
	if primary != 17 {
		t.Errorf("desktop primary field %vpx, want 17", primary)
	}
	// The control for the touch drive's "every field ≥ 16": on a desktop
	// some field is under 16, so the instrument can see a small one.
	small := false
	for _, f := range fonts {
		small = small || f.Px < 16
	}
	if !small {
		t.Error("CONTROL: every text control is ≥16px on a 1280px desktop, so the touch drive's ≥16 reading proves nothing")
	}
}
