//go:build browser

// The console rail's nav scrolls, so it clips at its padding box, and
// with no padding it cut off the current item's ring and contact
// shadow, which paint outside the item's border box. A geometry check
// cannot see that, so this drive compares the pixels around the item
// with a nav that has more room, and a control with the room taken
// away must differ, or the strip is not looking at the ring.
package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// ringFixture is one rail: its links, the window it is shown in, a
// stylesheet for every variant of it, and what is scrolled first: the
// document to its foot, and the nav to its end.
type ringFixture struct {
	nav, css    string
	height      int64
	foot, inner bool
}

// ringFixtures puts the current item first, where its ring meets the
// top of the nav, and last, where its contact shadow meets the foot.
// The tall rails overflow a short window beside a long page scrolled
// to its foot, so the nav is stuck; the last of them also scrolls the
// nav to its end, where its block-end padding is the room.
var ringFixtures = map[string]ringFixture{
	"first": {nav: `<a href="#" aria-current="page">Overview</a>` +
		`<a href="#">Apps</a><a href="#">Operators</a>` +
		`<a href="#">Billing</a>`, height: 900},
	"last": {nav: `<a href="#">Overview</a><a href="#">Apps</a>` +
		`<a href="#">Operators</a>` +
		`<a href="#" aria-current="page">Billing</a>`, height: 900},
	"tall first": {nav: railTallNav, css: ringLongPage, height: 420,
		foot: true},
	"tall last": {nav: ringTallLast, css: ringLongPage, height: 420,
		foot: true, inner: true},
}

const ringLongPage = `[rst-shell-main] { min-block-size: 2400px; }`

var ringTallLast = strings.Replace(
	strings.Replace(railTallNav, ` aria-current="page"`, "", 1),
	`<a href="#">Settings</a>`,
	`<a href="#" aria-current="page">Settings</a>`, 1)

// ringLights are where light.js can put the light (each axis runs
// from -1 to 1): straight above, which is the static design, and the
// four corners. The contact shadow leans 1.5px across and falls 1px
// to 2.2px down, so the corners are where it reaches furthest.
var ringLights = [][2]string{
	{"0", "-1"}, {"-1", "-1"}, {"1", "-1"}, {"-1", "1"}, {"1", "1"},
}

// The reference is more room, not overflow: visible, because a nav
// that stops scrolling spills its links into the page, which then
// scrolls to a different foot. Each variant keeps the items where the
// shipped nav has them; the clipped one is the rule before the fix.
var ringVariants = map[string]string{
	"shipped": "",
	"roomy": ringNavRule + ` { margin: -12px !important;` +
		` padding: 12px !important;` +
		` inset-block-start: calc(var(--rst-sp-4) - 12px) !important;` +
		` max-block-size: calc(100dvh - 2 * var(--rst-sp-4) + 24px)` +
		` !important; }`,
	"clipped": ringNavRule + ` { margin: 0 !important;` +
		` padding: 0 !important;` +
		` inset-block-start: var(--rst-sp-4) !important;` +
		` max-block-size: calc(100dvh - 2 * var(--rst-sp-4))` +
		` !important; }`,
}

const ringNavRule = `[rst-shell-console] > [rst-shell-rail] > [rst-shell-nav]`

// ringMargin is how far past the item's border box the strip reaches:
// past the contact shadow at any light position (1.5px across, 2.2px
// down, and the tail of a 2px blur), so a cut anywhere in it shows.
const ringMargin = 8

const ringMeasure = `(() => {
  const item = document.querySelector("[rst-shell-nav] a[aria-current]");
  const box = item.getBoundingClientRect();
  return JSON.stringify({Left: box.left, Top: box.top,
    Right: box.right, Bottom: box.bottom,
    ScrollX: window.scrollX, ScrollY: window.scrollY,
    NavScrolled: item.parentElement.scrollTop,
    NavEnd: item.parentElement.scrollHeight -
      item.parentElement.clientHeight});
})()`

// ringBox is the current item's border box in the window, and how far
// the document is scrolled: a capture's clip is in document pixels.
type ringBox struct {
	Left, Top, Right, Bottom float64
	ScrollX, ScrollY         float64
	NavScrolled, NavEnd      float64
}

// ringPixel is the first pixel where two strips disagree, in the
// strip's own coordinates, and the strip's size.
type ringPixel struct {
	X, Y          int
	Got, Want     []int
	Width, Height int
}

// edge names the side of the item the pixel is nearest. The ring
// straddles a fractional edge, so a cut at the top can land on the
// item's first pixel row: past the inline edges, the half of the strip
// it is in says which end it is at.
func (px ringPixel) edge() string {
	switch {
	case px.X < ringMargin:
		return "left"
	case px.X >= px.Width-ringMargin:
		return "right"
	case px.Y < px.Height/2:
		return "top"
	}
	return "bottom"
}

func TestTheConsoleRailDrawsTheCurrentItemWhole(t *testing.T) {
	rig := ringServe(t)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	drive := ringDrive{ctx: ctx, origin: rig.Origin}
	for _, dir := range []string{"ltr", "rtl"} {
		for _, at := range []string{
			"first", "last", "tall first", "tall last",
		} {
			for _, light := range ringLights {
				name := fmt.Sprintf("%s, %s rail, light at %s,%s",
					dir, at, light[0], light[1])
				drive.leg(t, name, ringPath(dir, at, ""),
					ringFixtures[at], light)
			}
		}
	}
}

// ringDrive is one browser and the server it reads the fixtures from.
type ringDrive struct {
	ctx    context.Context
	origin string
}

// leg photographs one fixture three ways at one light position.
func (drive ringDrive) leg(t *testing.T, name, base string,
	fixture ringFixture, light [2]string) {
	t.Helper()
	shipped, box := drive.shot(t, base+"shipped", fixture, light)
	roomy, roomyBox := drive.shot(t, base+"roomy", fixture, light)
	clipped, clippedBox := drive.shot(t, base+"clipped", fixture, light)
	ringScrolled(t, name, fixture, box)
	if box != roomyBox || box != clippedBox {
		t.Errorf("%s: the current item is at %+v as shipped, %+v with "+
			"more room and %+v with the nav's room taken away. The "+
			"room for the ring must not move the items",
			name, box, roomyBox, clippedBox)
		return
	}
	if _, cut := ringDiff(clipped, roomy); !cut {
		t.Errorf("CONTROL FAILED: %s: with the nav's room taken away "+
			"the strip still matches the unclipped ring, so it is not "+
			"looking at the ring and the pass below says nothing", name)
		return
	}
	if px, cut := ringDiff(shipped, roomy); cut {
		t.Errorf("%s: the current item's ring is cut off at its %s "+
			"edge: %d,%d in the strip is %s and is %s with more room "+
			"in the nav. The nav is a scroll container and "+
			"clips at its padding box: it needs padding for the "+
			"ring and an equal negative margin",
			name, px.edge(), px.X, px.Y, rgbString(px.Got),
			rgbString(px.Want))
	}
}

// ringServe renders every fixture in both directions, in every variant.
func ringServe(t *testing.T) *harness.Rig {
	t.Helper()
	mux := http.NewServeMux()
	stylesheets(t, mux)
	for _, dir := range []string{"ltr", "rtl"} {
		for at, fixture := range ringFixtures {
			page, _ := consolePage(t, fixture.nav, dir)
			for variant, css := range ringVariants {
				body := ringStyled(t, page, fixture.css+css)
				mux.HandleFunc("GET "+ringPath(dir, at, variant),
					ringPage(body))
			}
		}
	}
	return harness.New(t, func(string) http.Handler { return mux })
}

// ringPath is where one variant of one fixture is served.
func ringPath(dir, fixture, variant string) string {
	return "/" + dir + "/" + strings.ReplaceAll(fixture, " ", "-") +
		"/" + variant
}

// ringScrolled stops a leg whose fixture is not in the state it is
// there for: a tall nav that is not stuck in the window, or one that
// was to be scrolled to its end and was not.
func ringScrolled(t *testing.T, name string, fixture ringFixture,
	box ringBox) {
	t.Helper()
	stuck := box.ScrollY > 1000 && box.Top >= 0 &&
		box.Bottom <= float64(fixture.height)
	if fixture.foot && !stuck {
		t.Fatalf("%s: at the page's foot (scrolled %.0fpx) the current "+
			"item is at %.0f..%.0fpx in a %dpx window: the nav is not "+
			"stuck, so this leg is not photographing a stuck nav",
			name, box.ScrollY, box.Top, box.Bottom, fixture.height)
	}
	if fixture.inner && (box.NavEnd <= 0 ||
		box.NavScrolled < box.NavEnd-1) {
		t.Fatalf("%s: the nav scrolled %.0fpx of its %.0fpx, not to its "+
			"end; the leg is not photographing the room at its foot",
			name, box.NavScrolled, box.NavEnd)
	}
}

func ringPage(body string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, body)
	}
}

// ringStyled adds a variant's stylesheet after the shipped one, so it
// wins on order as well as on !important.
func ringStyled(t *testing.T, page, css string) string {
	t.Helper()
	if css == "" {
		return page
	}
	at := strings.Index(page, "</head>")
	if at < 0 {
		t.Fatal("the console shell rendered no </head> to style")
	}
	return page[:at] + "<style>" + css + "</style>" + page[at:]
}

// shot loads one page wide, sets the light, and photographs the
// current item with ringMargin pixels of rail around it.
func (drive ringDrive) shot(t *testing.T, path string,
	fixture ringFixture, light [2]string) (image.Image, ringBox) {
	t.Helper()
	setup := `(() => { const root = document.documentElement;
  root.style.setProperty("--rst-light-x", "` + light[0] + `");
  root.style.setProperty("--rst-light-y", "` + light[1] + `");
  if (` + fmt.Sprint(fixture.foot) + `) window.scrollTo(0, root.scrollHeight);
  const nav = document.querySelector("[rst-shell-nav]");
  if (` + fmt.Sprint(fixture.inner) + `) nav.scrollTop = nav.scrollHeight;
  return "ok"; })()`
	var raw string
	err := chromedp.Do(drive.ctx,
		chromedp.EmulateViewport(1280, fixture.height),
		chromedp.Navigate(drive.origin+path),
		chromedp.WaitVisible(chromedp.CSS(`[rst-shell-nav] a`)),
		harness.Into(new(string), chromedp.Evaluate[string](setup)),
		harness.Into(&raw, chromedp.Evaluate[string](ringMeasure)),
	)
	if err != nil {
		t.Fatalf("driving %s: %v", path, err)
	}
	var box ringBox
	if err := json.Unmarshal([]byte(raw), &box); err != nil {
		t.Fatalf("reading %s's item (%q): %v", path, raw, err)
	}
	return drive.capture(t, path, box), box
}

// capture photographs the box grown by ringMargin, at one image
// pixel per CSS pixel, so two strips of the same box line up exactly.
func (drive ringDrive) capture(t *testing.T, path string,
	box ringBox) image.Image {
	t.Helper()
	clip := &cdppage.Viewport{
		X:      box.ScrollX + math.Floor(box.Left) - ringMargin,
		Y:      box.ScrollY + math.Floor(box.Top) - ringMargin,
		Width:  math.Ceil(box.Right) - math.Floor(box.Left) + 2*ringMargin,
		Height: math.Ceil(box.Bottom) - math.Floor(box.Top) + 2*ringMargin,
		Scale:  1,
	}
	var shot []byte
	capture := func(ctx context.Context, target *chromedp.Target) error {
		got, err := cdp.Call(ctx, target, cdppage.CaptureScreenshot,
			cdppage.CaptureScreenshotParams{
				Format:      cdppage.CaptureScreenshotFormatPng,
				FromSurface: ptr(true),
				Clip:        clip,
			})
		shot = got.Data
		return err
	}
	if err := chromedp.Do(drive.ctx, chromedp.Func(capture)); err != nil {
		t.Fatalf("photographing %s's current item: %v", path, err)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatalf("decoding %s's strip: %v", path, err)
	}
	return img
}

// ringDiff finds the first pixel where got and want differ by more
// than antialiasing would, or reports that they match.
func ringDiff(got, want image.Image) (ringPixel, bool) {
	bounds := got.Bounds()
	at := func(img image.Image, col, row int) []int {
		red, green, blue, _ := img.At(col, row).RGBA()
		return []int{int(red >> 8), int(green >> 8), int(blue >> 8)}
	}
	inner := ringPixel{Width: bounds.Dx(), Height: bounds.Dy()}
	for row := bounds.Min.Y; row < bounds.Max.Y; row++ {
		for col := bounds.Min.X; col < bounds.Max.X; col++ {
			mine, theirs := at(got, col, row), at(want, col, row)
			if !rgbClose(mine, theirs, 3) {
				inner.X, inner.Y = col-bounds.Min.X, row-bounds.Min.Y
				inner.Got, inner.Want = mine, theirs
				return inner, true
			}
		}
	}
	return ringPixel{}, false
}
