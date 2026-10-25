//go:build browser

package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// The two promises this file holds, pixel for pixel: an app that takes
// the new tokens.css and keeps its old theme renders as it did (C2,
// A15), and plain and signal render as they did (A13). The page is the
// depth fixture; it is photographed once per state set (rest, hovered,
// pressed, focused and current) under the v0.28 stylesheet pair and
// under the new one, and every component is cropped out of each shot
// with a margin for its shadow; the two crops must agree. Antialiasing
// at a rounded corner differs where a hairline is now painted by a
// gradient instead of a border colour (A9): Chromium antialiases the
// inner corners of the padding-box clip under a transparent border
// differently from an opaque border's inner edge, measured at 9 to 16
// units on 2 to 10 pixels per card, in every theme, with the straight
// edges and interiors identical. So the comparison allows a per-channel
// delta of 24 (the measured 16 with headroom for drift between Chromium
// builds) on at most 1% of a crop's pixels and nothing more; the count
// rule is what catches a faint tint across a whole component, and a cap
// of 8 failed every card on its corners. Per crop, not per page: on the
// 1280x1600 page 1% was twenty thousand pixels, and a 3% tint over a
// whole hovered button passed under it; on a 90x34 crop it is a corner's
// worth, and the second negative control holds it there.
//
// The server pairs a stylesheet set with a directory: /old/ serves the
// snapshots, /new/ the library, and the page links its stylesheets
// relatively so each directory resolves its own.

func matrixDoc() string {
	return strings.Replace(strings.Replace(sizingDoc("matrix", depthBody), `href="/tokens.css"`, `href="tokens.css"`, 1), `href="/theme.css"`, `href="theme.css"`, 1)
}

// legacyRig serves /old/ and /new/ with their own stylesheet bytes in
// front of sizingMux. The two routes are matched here, not registered
// on the mux: stylesheets() already owns GET /tokens.css and
// GET /theme.css there, and a second registration panics. Anything in
// head goes after the theme link on both pages: an app's own
// stylesheet, for the drive that holds a flat theme to an app's
// overrides.
func legacyRig(t *testing.T, oldTokens, oldTheme, newTokens, newTheme []byte, withLight bool, head ...string) *harness.Rig {
	t.Helper()
	doc := matrixDoc()
	if withLight {
		doc = strings.Replace(doc, `<script defer src="/busy.js"></script>`, `<script defer src="/busy.js"></script><script defer src="/light.js"></script>`, 1)
	}
	if len(head) > 0 {
		const link = `<link rel="stylesheet" href="theme.css">`
		if !strings.Contains(doc, link) {
			t.Fatalf("the matrix page has no %s to put an app stylesheet after", link)
		}
		doc = strings.Replace(doc, link, link+strings.Join(head, ""), 1)
	}
	pages := map[string]string{"/old/": doc, "/new/": doc}
	return harness.New(t, func(string) http.Handler {
		mux := sizingMux(t, pages)
		sets := map[string][2][]byte{"/old/": {oldTokens, oldTheme}, "/new/": {newTokens, newTheme}}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for prefix, set := range sets {
				if r.URL.Path == prefix+"tokens.css" || r.URL.Path == prefix+"theme.css" {
					w.Header().Set("Content-Type", "text/css")
					if strings.HasSuffix(r.URL.Path, "tokens.css") {
						w.Write(set[0])
					} else {
						w.Write(set[1])
					}
					return
				}
			}
			mux.ServeHTTP(w, r)
		})
	}, harness.WithHover())
}

func legacyFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/legacy/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// snap photographs one element in one forced state (the light.js
// old-theme drive in Task 10 uses it): a full-page shot cropped to the
// element's box. Not chromedp.Screenshot(selector): on this Chromium it
// scrolled, waited and never returned, which is what timed the first
// matrix out.
func snap(t *testing.T, ctx context.Context, selector string, states ...string) []byte {
	t.Helper()
	forceState(t, ctx, selector, states...)
	x, y, w, h := rect(t, ctx, selector)
	var buf []byte
	mustRun(t, ctx, chromedp.FullScreenshot(&buf, 100))
	forceState(t, ctx, selector)
	return cropPNG(t, decodePNG(t, buf), x, y, w, h, 0)
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// cropPNG cuts one element's box out of a page shot, grown by margin on
// every side and clipped to the page, and encodes it. The box is
// floored and ceiled to whole pixels so a fractional edge keeps the
// antialiased column it lands on.
func cropPNG(t *testing.T, img image.Image, x, y, w, h float64, margin int) []byte {
	t.Helper()
	r := image.Rect(int(math.Floor(x))-margin, int(math.Floor(y))-margin, int(math.Ceil(x+w))+margin, int(math.Ceil(y+h))+margin).Intersect(img.Bounds())
	crop := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(crop, crop.Bounds(), img, r.Min, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, crop); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// matrixMargin is how far past a target's box its crop reaches: the
// ambient shadow is offset by up to 3px and blurred by 3px, and a card's
// later rim is drawn inside its box, so 8px covers every painted pixel
// a state can add outside the element.
const matrixMargin = 8

// samePixels reports whether two PNGs agree within the tolerance above.
func samePixels(t *testing.T, a, b []byte) (bool, string) {
	t.Helper()
	return samePixelsWithin(t, a, b, 24)
}

// samePixelsWithin is samePixels with the per-channel cap chosen by the
// caller; the 1% count rule is the same. The app-override drive needs
// a higher cap: the corner antialiasing above scales with how far the
// hairline sits from the surface, and an app's hairline can sit
// further than the stock pair's.
func samePixelsWithin(t *testing.T, a, b []byte, maxDelta int) (bool, string) {
	t.Helper()
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if ia.Bounds() != ib.Bounds() {
		return false, fmt.Sprintf("sizes differ: %v vs %v", ia.Bounds(), ib.Bounds())
	}
	bad, total := 0, 0
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			ra, ga, ba, _ := ia.At(x, y).RGBA()
			rb, gb, bb, _ := ib.At(x, y).RGBA()
			total++
			d := func(p, q uint32) int {
				v := int(p>>8) - int(q>>8)
				if v < 0 {
					v = -v
				}
				return v
			}
			if m := max(d(ra, rb), d(ga, gb), d(ba, bb)); m > maxDelta {
				return false, fmt.Sprintf("pixel %d,%d differs by %d", x, y, m)
			} else if m > 0 {
				bad++
			}
		}
	}
	if float64(bad) > float64(total)*0.01 {
		return false, fmt.Sprintf("%d of %d pixels differ", bad, total)
	}
	return true, ""
}

// matrixTargets is every component and every state the depth language
// touches, in both spellings where the fixture has both. force is the
// element the state is pinned on when it is not the one photographed:
// a row's overlay link going active tints the row.
var matrixTargets = []struct {
	selector, force string
	states          [][]string
}{
	{"#b-plain", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}, {"focus", "focus-visible"}}},
	{"#b-plain-c", "", [][]string{{}, {"hover"}, {"hover", "active"}}},
	{"#b-primary", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}, {"focus", "focus-visible"}}},
	{"#b-primary-c", "", [][]string{{}, {"hover"}, {"hover", "active"}}},
	{"#b-danger", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}}},
	{"#b-ghost", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}}},
	{"#b-link", "", [][]string{{}, {"hover"}, {"hover", "active"}}},
	{"#b-off", "", [][]string{{}, {"hover"}}},
	{"#box", "", [][]string{{}}},
	{"#stats", "", [][]string{{}}},
	{"#list", "", [][]string{{}}},
	{"#lra", "", [][]string{{}, {"hover"}}},
	{"#lra", "#lra-link", [][]string{{"active"}}},
	{"#card", "", [][]string{{}}},
	{"#row-a", "", [][]string{{}, {"hover"}}},
	{"#row-a", "#link-a", [][]string{{"active"}}},
	{"#row-c", "", [][]string{{}, {"hover"}}},
	{"#row-inert", "", [][]string{{}, {"hover"}}},
	{"#tile", "", [][]string{{}, {"hover"}}},
	{"#tile", "#tile-link", [][]string{{"active"}}},
	{"#sidebar", "", [][]string{{}}},
	{"#nav-x", "", [][]string{{"hover"}, {"hover", "active"}, {"active"}}},
	{"#console", "", [][]string{{}}},
	{"#topbar", "", [][]string{{}}},
	{"#tabs", "", [][]string{{}}},
	{"#tab-1", "", [][]string{{"hover"}, {"hover", "active"}, {"active"}}},
	{"#pages", "", [][]string{{}}},
	{"#page-1", "", [][]string{{"hover"}, {"hover", "active"}, {"active"}}},
	// Focused too: the outline follows border-radius, so a radius the
	// face brings rounds the focus ring of a theme that has no face.
	{"#dd-sum", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}, {"focus", "focus-visible"}}},
	{"#sw", "", [][]string{{}, {"hover"}}},
	{"#field", "", [][]string{{}, {"hover"}, {"focus", "focus-visible"}}},
	// The on switch and the tblock: the knob, the track and the hover
	// sheen all read new tokens, and a flat theme's must paint nothing.
	{"#sw-on", "", [][]string{{}, {"hover"}}},
	{"#tb", "", [][]string{{}}},
	{"#tb", "#tb-head", [][]string{{"hover"}}},
	// The other two fields: the inset shadow and the focus halo. The
	// search box shows focus on :focus-within, pinned on its input.
	{"#area", "", [][]string{{}, {"focus", "focus-visible"}}},
	{"#search", "", [][]string{{}}},
	{"#search", "#search-in", [][]string{{"focus", "focus-visible"}}},
	{"#dd-menu", "", [][]string{{}}},
	{"#dd-nested-sum", "", [][]string{{}, {"hover"}, {"hover", "active"}}},
	{"#menu-a", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}}},
	{"#menu-btn", "", [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}}},
	{"#opt-active", "", [][]string{{}}},
	// An index-view root at desktop width is the rail beside the page
	// (the scaffold's home page): its items hover and press like any rail's.
	{"#isidebar", "", [][]string{{}}},
	{"#inav-x", "", [][]string{{"hover"}, {"hover", "active"}, {"active"}}},
}

// matrixStates is every state set the targets name, in a fixed order.
var matrixStates = [][]string{{}, {"hover"}, {"hover", "active"}, {"active"}, {"focus", "focus-visible"}}

// matrixKey names one crop: scheme, the pinned states and the target.
func matrixKey(scheme string, states []string, selector string) string {
	return scheme + " " + strings.Join(states, "+") + " " + selector
}

// matrixShots photographs the whole page once per state set under one
// stylesheet pair and scheme, with the state pinned on every target that
// lists it at the same time, and returns each target's crop keyed by
// matrixKey. One full-page shot per state set rather than one shot per
// element: the per-element version took over ten minutes for the five
// pairs, and the pixels are the same pixels. The crops are compared one
// by one so the 1% allowance is a component's, not the page's.
func matrixShots(t *testing.T, ctx context.Context, origin, dir, scheme string) map[string][]byte {
	t.Helper()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 1600), chromedp.Navigate(origin+dir), chromedp.WaitVisible("#field", chromedp.ByQuery),
		chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), document.getElementById("dd").open = true, true`, nil))
	still(t, ctx)
	out := map[string][]byte{}
	for _, states := range matrixStates {
		var pinned []string
		type box struct {
			key        string
			x, y, w, h float64
		}
		var boxes []box
		for _, target := range matrixTargets {
			for _, s := range target.states {
				if strings.Join(s, "+") == strings.Join(states, "+") {
					force := target.force
					if force == "" {
						force = target.selector
					}
					forceState(t, ctx, force, states...)
					pinned = append(pinned, force)
					// The box is read with the state pinned, so a state that
					// moves the element (a press drop) crops where it is.
					x, y, w, h := rect(t, ctx, target.selector)
					boxes = append(boxes, box{matrixKey(scheme, states, target.selector), x, y, w, h})
				}
			}
		}
		var buf []byte
		mustRun(t, ctx, chromedp.FullScreenshot(&buf, 100))
		img := decodePNG(t, buf)
		for _, b := range boxes {
			out[b.key] = cropPNG(t, img, b.x, b.y, b.w, b.h, matrixMargin)
		}
		for _, force := range pinned {
			forceState(t, ctx, force)
		}
	}
	return out
}

// TestEveryThemeRendersAsItDidExceptDay is a preservation test: it does
// not fail before its change, it fails if any later change breaks the
// two promises. It runs the matrix in both schemes for three pairs (new
// tokens with the old day, plain and signal) against the old tokens
// with the same old theme, and for plain and signal also the new tokens
// with the NEW theme against the old pair: those two must not change at
// all (A13). Its sensitivity is proved by the negative control at the
// end, which breaks compatibility on purpose and must be caught.
func TestEveryThemeRendersAsItDidExceptDay(t *testing.T) {
	oldTokens := legacyFile(t, "tokens-v028.css")
	for _, c := range []struct{ name, oldTheme, newTheme string }{
		{"old day theme", "day-v028.css", ""},
		{"old plain theme", "plain-v028.css", ""},
		{"old signal theme", "signal-v028.css", ""},
		{"plain", "plain-v028.css", "plain"},
		{"signal", "signal-v028.css", "signal"},
	} {
		t.Run(c.name, func(t *testing.T) {
			oldTheme := legacyFile(t, c.oldTheme)
			newTheme := oldTheme
			if c.newTheme != "" {
				newTheme, _ = ThemeCSS(c.newTheme)
			}
			rig := legacyRig(t, oldTokens, oldTheme, TokensCSS(), newTheme, false)
			ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
			defer cancel()
			for _, scheme := range []string{"light", "dark"} {
				old := matrixShots(t, ctx, rig.Origin, "/old/", scheme)
				now := matrixShots(t, ctx, rig.Origin, "/new/", scheme)
				keys := make([]string, 0, len(old))
				for key := range old {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if ok, why := samePixels(t, old[key], now[key]); !ok {
						t.Errorf("%s renders differently under the new tokens.css: %s", key, why)
					}
				}
			}
		})
	}
	t.Run("negative control", func(t *testing.T) {
		// The old day theme plus one new token painted red: the new
		// tokens.css reads it, the old did not, and the matrix must see
		// the difference, or every pass above proves nothing.
		oldTheme := legacyFile(t, "day-v028.css")
		broken := append(append([]byte{}, oldTheme...), []byte("\n:root { --rst-accent-fill: #ff0000; }\n")...)
		rig := legacyRig(t, oldTokens, oldTheme, TokensCSS(), broken, false)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		defer cancel()
		old := matrixShots(t, ctx, rig.Origin, "/old/", "light")
		now := matrixShots(t, ctx, rig.Origin, "/new/", "light")
		key := matrixKey("light", nil, "#b-primary")
		if ok, _ := samePixels(t, old[key], now[key]); ok {
			t.Fatal("a primary button filled red passed the matrix; the comparison is blind")
		}
	})
	t.Run("negative control, faint tint", func(t *testing.T) {
		// The old day theme under a tokens.css whose ghost hover fallback
		// is a 3% black tint in place of transparent: one component, one
		// state, a few hundred pixels each off by 7 or 8. The matrix must
		// see it, or a faint tint on a whole component is a free change.
		oldTheme := legacyFile(t, "day-v028.css")
		tinted := bytes.Replace(TokensCSS(), []byte("var(--rst-ghost-face-hover, transparent)"), []byte("var(--rst-ghost-face-hover, rgb(0 0 0 / 0.03))"), 1)
		if bytes.Equal(tinted, TokensCSS()) {
			t.Fatal("the ghost hover fallback is not where this control expects it; the tint was not applied")
		}
		rig := legacyRig(t, oldTokens, oldTheme, tinted, oldTheme, false)
		ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
		defer cancel()
		old := matrixShots(t, ctx, rig.Origin, "/old/", "light")
		now := matrixShots(t, ctx, rig.Origin, "/new/", "light")
		key := matrixKey("light", []string{"hover"}, "#b-ghost")
		if ok, _ := samePixels(t, old[key], now[key]); ok {
			t.Fatal("a ghost button tinted 3% on hover passed the matrix; a whole component can change under the allowance")
		}
	})
}
