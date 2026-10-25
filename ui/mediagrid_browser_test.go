//go:build browser

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// Two pictures the grid has to hold without stretching a tile: a page
// screenshot three times taller than it is wide, and a banner six times
// wider than it is tall. SVG so the test owns the ratio, served by the
// drive's own mux at the routes the items name. Not data: URLs:
// html/template's URL filter writes #ZgotmplZ for one, and a drive that
// measured the broken-image boxes those leave would pass with the
// cover and contain rules deleted.
const (
	mediaTallSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 900" width="300" height="900"><rect width="300" height="900" fill="#cfd8e3"/><rect width="300" height="60" fill="#2464e0"/></svg>`
	mediaWideSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 200" width="1200" height="200"><rect width="1200" height="200" fill="#e5e7eb"/><circle cx="100" cy="100" r="60" fill="#2464e0"/></svg>`
	mediaTallSrc = "/images/tall.svg"
	mediaWideSrc = "/images/wide.svg"
)

// mediaPictures serves the two pictures at their routes, ahead of the
// fixtures' catch-all /images/ handler.
func mediaPictures(mux *http.ServeMux) {
	for route, body := range map[string]string{mediaTallSrc: mediaTallSVG, mediaWideSrc: mediaWideSVG} {
		body := body
		mux.HandleFunc("GET "+route, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, body)
		})
	}
}

// fixtureImages serves the pictures the shared fixtures name under
// /images/, as one small SVG, for every drive that renders allPartials:
// the harness reports a 404 as a problem on the page, and a media-grid
// fixture with no picture to load would fail a drive about spelling or
// target size for a reason that is neither.
func fixtureImages(mux *http.ServeMux) {
	mux.HandleFunc("GET /images/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4 3" width="400" height="300"><rect width="4" height="3" fill="#cfd8e3"/></svg>`)
	})
}

// mediaItems is the grid under test: the two shapes, each at the
// default cover and at contain, one tile selected, every tile linked
// and with a kebab, so one grid exercises every control the partial
// draws. A long description, so the clamp has something to clamp.
func mediaItems() []any {
	menu := func(id string) []any {
		return []any{map[string]any{"Label": "Edit", "Href": "/go/" + id + "-edit"}}
	}
	return []any{
		map[string]any{"Src": mediaTallSrc, "Name": "Checkout, step 2, the payment form with every field open", "Desc": "The payment step, with the card form open and the error for a card number that does not pass its check digit shown under the field.", "Meta": "300 × 900 · 41 KB",
			"Href": "/go/tall", "Width": 300, "Height": 900, "Select": "tall", "Menu": menu("tall")},
		map[string]any{"Src": mediaWideSrc, "Name": "Spring banner", "Desc": "The banner at the top of the shop.", "Meta": "1200 × 200 · 18 KB",
			"Href": "/go/wide", "Width": 1200, "Height": 200, "Select": "wide", "Menu": menu("wide")},
		map[string]any{"Src": mediaWideSrc, "Name": "Spring banner, contained", "Meta": "1200 × 200 · 18 KB", "Fit": "contain",
			"Href": "/go/wide-contain", "Width": 1200, "Height": 200, "Select": "wide-contain", "Selected": true, "Menu": menu("wide-contain")},
		map[string]any{"Src": mediaTallSrc, "Name": "Checkout, contained", "Desc": "Letterboxed.", "Fit": "contain",
			"Href": "/go/tall-contain", "Width": 300, "Height": 900, "Select": "tall-contain", "Menu": menu("tall-contain")},
	}
}

// mediaPage is the grid in a list card under a bulk bar, in one
// direction, the way an Images page in select mode is laid out. The
// ids are for the probes.
func mediaPage(t *testing.T, dir string) string {
	t.Helper()
	body := `<script>document.documentElement.dataset.scripted = "1"</script>` +
		`<div rst-page><form id="select" method="post" action="/select"><div rst-list id="card">` +
		render(t, "bulk-bar", map[string]any{"DoneHref": "/go/done", "Count": "1 selected", "MenuLabel": "Actions",
			"Actions": []any{map[string]any{"Value": "export", "Label": "Export"}}}) +
		render(t, "media-grid", map[string]any{"Items": mediaItems()}) +
		`</div></form></div>`
	return strings.Replace(sizingDoc("media-grid", body), `dir="ltr"`, `dir="`+dir+`"`, 1)
}

// mediaMux serves the page in both directions, a landing page for every
// /go/ href, and the form's action, which hands what it was posted to
// the drive.
func mediaMux(t *testing.T, posted chan<- url.Values) *http.ServeMux {
	t.Helper()
	mux := sizingMux(t, map[string]string{
		"/":    mediaPage(t, "ltr"),
		"/rtl": mediaPage(t, "rtl"),
		"/go/": sizingDoc("landed", `<p id="landed">landed</p>`),
	})
	mediaPictures(mux)
	mux.HandleFunc("POST /select", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posted <- r.PostForm
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, sizingDoc("posted", `<p id="posted">posted</p>`))
	})
	return mux
}

// mediaReading is one tile measured: its boxes, the image's, the
// caption's last line's, the two controls', and whether anything in the
// tile is cut off by it.
type mediaReading struct {
	Name                                  string
	Tile, Frame, Img, Caption, Last, Link box
	Check, Kebab                          *box
	Overflow                              float64 // scrollHeight - clientHeight of the tile
	Contain                               bool
	Selected                              bool
	Outline                               string
	// The picture as decoded, and how the frame fits it: naturalWidth
	// is 0 for a picture that did not decode, so a 404 or a filtered
	// src reads as the zero it is.
	Decoded              bool
	NaturalW, NaturalH   int
	ObjectFit, ObjectPos string
	Src                  string
}

const mediaJS = `(() => {
  const r = el => { if (!el) return null; const b = el.getBoundingClientRect(); return {L: b.left, T: b.top, R: b.right, B: b.bottom}; };
  const out = [];
  document.querySelectorAll("[rst-media-tile]").forEach(tile => {
    const frame = tile.querySelector("[rst-media-frame]"), cap = tile.querySelector("[rst-media-caption]");
    const last = cap.querySelector("[rst-media-meta]") || cap.querySelector("[rst-media-desc]");
    const img = frame.querySelector("img"), is = getComputedStyle(img);
    out.push({Name: tile.querySelector("[rst-media-name] > :first-child").textContent,
      Tile: r(tile), Frame: r(frame), Img: r(img), Caption: r(cap), Last: r(last),
      Decoded: img.complete && img.naturalWidth > 0, NaturalW: img.naturalWidth, NaturalH: img.naturalHeight,
      ObjectFit: is.objectFit, ObjectPos: is.objectPosition, Src: img.getAttribute("src"),
      Link: r(tile.querySelector("[rst-media-name] > a")),
      Check: r(tile.querySelector("[rst-selbox]")), Kebab: r(tile.querySelector("[rst-row-menu] > summary")),
      Overflow: tile.scrollHeight - tile.clientHeight,
      Contain: frame.getAttribute("rst-media-frame") === "contain",
      Selected: !!tile.querySelector("[rst-selbox] input:checked"),
      Outline: getComputedStyle(tile).outlineStyle + " " + getComputedStyle(tile).outlineWidth});
  });
  const gridEl = document.querySelector("[rst-media]"), gs = getComputedStyle(gridEl);
  const grid = r(gridEl), inner = gridEl.clientWidth - parseFloat(gs.paddingLeft) - parseFloat(gs.paddingRight);
  const floor = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--rst-target")) * parseFloat(getComputedStyle(document.documentElement).fontSize);
  return JSON.stringify({Tiles: out, Grid: grid, GridInner: inner, Floor: floor, Width: innerWidth, RTL: document.documentElement.dir === "rtl",
    Scripted: document.documentElement.dataset.scripted === "1"});
})()`

type mediaPageReading struct {
	Tiles     []mediaReading
	Grid      box
	GridInner float64
	Floor     float64
	Width     int
	RTL       bool
	Scripted  bool
}

func readMedia(t *testing.T, ctx context.Context) mediaPageReading {
	t.Helper()
	// Every picture has finished loading, well or badly, before the
	// reading: complete is true for a decode and for a failure alike,
	// and naturalWidth tells them apart.
	settleUntil(t, ctx, `[...document.images].every(i => i.complete)`)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(mediaJS, &raw)); err != nil {
		t.Fatalf("reading the grid: %v", err)
	}
	var got mediaPageReading
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	if len(got.Tiles) != len(mediaItems()) {
		t.Fatalf("read %d tiles, want %d; the selectors no longer match the partial", len(got.Tiles), len(mediaItems()))
	}
	return got
}

// insideBox says whether inner is inside outer, to a pixel.
func insideBox(inner, outer box) bool {
	return inner.L >= outer.L-1 && inner.T >= outer.T-1 && inner.R <= outer.R+1 && inner.B <= outer.B+1
}

// TestMediaGridFramesAreEvenAndCaptionsStayInTheTile is the partial's
// claim, measured: whatever the picture's shape, a 1:3 screenshot or a
// 6:1 banner, its frame is 4:3 and the image is held inside it; the
// name, the description and the meta line are inside the tile with
// nothing cut off; the checkbox and the kebab are at the floor, in the
// frame's and the caption's start corners, in both directions; one
// column at a phone's width and several at a desktop's.
func TestMediaGridFramesAreEvenAndCaptionsStayInTheTile(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
		dir    string
		floor  float64
		cols   int // the least number of columns
	}{
		{"1024 mouse", 1024, 768, false, "ltr", 32, 3},
		{"1024 mouse rtl", 1024, 768, false, "rtl", 32, 3},
		{"390 touch", 390, 844, true, "ltr", 44, 1},
		{"390 touch rtl", 390, 844, true, "rtl", 44, 1},
		{"320 touch", 320, 568, true, "ltr", 44, 1},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := mediaRig(t, leg.coarse, mediaMux(t, make(chan url.Values, 4)))
			ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
			defer cancel()
			path := "/"
			if leg.dir == "rtl" {
				path = "/rtl"
			}
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h), chromedp.Navigate(rig.Origin+path), chromedp.WaitVisible("[rst-media]", chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			requirePointer(t, ctx, leg.coarse)
			got := readMedia(t, ctx)
			if got.RTL != (leg.dir == "rtl") {
				t.Fatalf("the page is dir=%v, this leg wants %s", got.RTL, leg.dir)
			}
			if got.Floor != leg.floor {
				t.Errorf("--rst-target is %vpx, want %v", got.Floor, leg.floor)
			}
			lefts := map[int]bool{}
			for _, tile := range got.Tiles {
				if tile.Tile.B > got.Tiles[0].Tile.B+1 {
					continue // a later row
				}
				lefts[int(math.Round(tile.Tile.L))] = true
			}
			if len(lefts) < leg.cols {
				t.Errorf("%d column(s) in the first row, want at least %d", len(lefts), leg.cols)
			}
			if leg.cols == 1 && len(lefts) != 1 {
				t.Errorf("a phone shows %d columns, want one", len(lefts))
			}
			if leg.cols == 1 {
				// One column, and the column is the grid: a tile narrower
				// than the grid would be a grid that reserved room for a
				// second tile it cannot fit.
				if tw, gw := got.Tiles[0].Tile.w(), got.GridInner; math.Abs(tw-gw) > 1 {
					t.Errorf("the one column is %.0fpx wide in a grid whose content box is %.0fpx", tw, gw)
				}
			}
			var selected int
			for _, tile := range got.Tiles {
				where := leg.name + ": " + tile.Name
				if ratio := tile.Frame.w() / (tile.Frame.B - tile.Frame.T); math.Abs(ratio-4.0/3) > 0.02 {
					t.Errorf("%s: the frame is %.0f×%.0f, ratio %.3f, want 4:3", where, tile.Frame.w(), tile.Frame.B-tile.Frame.T, ratio)
				}
				// The picture decoded at the shape the drive drew, so what
				// is measured below is the real 1:3 or 6:1 image in its
				// frame and not a broken-image box.
				wantW, wantH := 300, 900
				if tile.Src == mediaWideSrc {
					wantW, wantH = 1200, 200
				} else if tile.Src != mediaTallSrc {
					t.Fatalf("%s: the image's src is %q, not a route this drive serves", where, tile.Src)
				}
				if !tile.Decoded || tile.NaturalW != wantW || tile.NaturalH != wantH {
					t.Errorf("%s: the picture decoded %v at %d×%d, want %d×%d", where, tile.Decoded, tile.NaturalW, tile.NaturalH, wantW, wantH)
				}
				// cover from the top for the default, contain for Fit: the
				// two rules that make a tall screenshot show its top and a
				// logo show whole, read off the engine rather than inferred
				// from a box the frame's own size would give either way.
				if tile.Contain {
					if tile.ObjectFit != "contain" {
						t.Errorf("%s: Fit contain computes object-fit %q, want contain", where, tile.ObjectFit)
					}
				} else if tile.ObjectFit != "cover" || tile.ObjectPos != "50% 0%" {
					t.Errorf("%s: the default computes object-fit %q at %q, want cover at 50%% 0%% (top centre)", where, tile.ObjectFit, tile.ObjectPos)
				}
				if !insideBox(tile.Img, tile.Frame) {
					t.Errorf("%s: the image %+v is not held inside its frame %+v", where, tile.Img, tile.Frame)
				}
				if tile.Img.w() < tile.Frame.w()-1 || (tile.Img.B-tile.Img.T) < (tile.Frame.B-tile.Frame.T)-1 {
					t.Errorf("%s: the image box %+v does not fill its frame %+v; the frame's shape would show around it", where, tile.Img, tile.Frame)
				}
				if !insideBox(tile.Caption, tile.Tile) || !insideBox(tile.Last, tile.Tile) {
					t.Errorf("%s: the caption %+v or its last line %+v leaves the tile %+v", where, tile.Caption, tile.Last, tile.Tile)
				}
				if tile.Caption.T < tile.Frame.B-1 {
					t.Errorf("%s: the caption starts at %.0f, over the frame that ends at %.0f", where, tile.Caption.T, tile.Frame.B)
				}
				if tile.Overflow > 0.5 {
					t.Errorf("%s: %.0fpx of the tile is cut off; a caption must be visible without scrolling the tile", where, tile.Overflow)
				}
				if tile.Check == nil || tile.Kebab == nil {
					t.Fatalf("%s: no checkbox or no kebab was rendered", where)
				}
				for name, b := range map[string]box{"checkbox": *tile.Check, "kebab": *tile.Kebab} {
					if b.w() < got.Floor-0.5 || (b.B-b.T) < got.Floor-0.5 {
						t.Errorf("%s: the %s is %.0f×%.0f, under the %.0fpx floor", where, name, b.w(), b.B-b.T, got.Floor)
					}
				}
				if !insideBox(*tile.Check, tile.Frame) {
					t.Errorf("%s: the checkbox %+v is not inside the frame %+v", where, *tile.Check, tile.Frame)
				}
				// The start corners: the checkbox in the frame's, the
				// kebab at the caption's end, mirrored under dir=rtl.
				if got.RTL {
					if tile.Frame.R-tile.Check.R > 8 || tile.Check.T-tile.Frame.T > 8 {
						t.Errorf("%s: in RTL the checkbox %+v is not in the frame's top right corner (frame %+v)", where, *tile.Check, tile.Frame)
					}
					if tile.Kebab.L > tile.Link.L {
						t.Errorf("%s: in RTL the kebab (%.0f) is not at the left of the name (%.0f)", where, tile.Kebab.L, tile.Link.L)
					}
				} else {
					if tile.Check.L-tile.Frame.L > 8 || tile.Check.T-tile.Frame.T > 8 {
						t.Errorf("%s: the checkbox %+v is not in the frame's top left corner (frame %+v)", where, *tile.Check, tile.Frame)
					}
					if tile.Kebab.R < tile.Link.R {
						t.Errorf("%s: the kebab (%.0f) is not at the right of the name (%.0f)", where, tile.Kebab.R, tile.Link.R)
					}
				}
				if tile.Selected {
					selected++
					if !strings.HasPrefix(tile.Outline, "solid 2px") {
						t.Errorf("%s: the selected tile's outline is %q, want a solid 2px accent outline", where, tile.Outline)
					}
				} else if !strings.HasPrefix(tile.Outline, "none") {
					t.Errorf("%s: an unselected tile has an outline (%q)", where, tile.Outline)
				}
			}
			if selected != 1 {
				t.Errorf("%d tiles read as selected, want the one the server checked", selected)
			}
		})
	}
}

// mediaRig is sizingRig with a mux of the drive's own: the form's
// action is a handler, not a page.
func mediaRig(t *testing.T, coarse bool, mux *http.ServeMux) *harness.Rig {
	t.Helper()
	var opts []harness.Option
	if coarse {
		opts = append(opts, harness.WithCoarsePointer())
	}
	return harness.New(t, func(string) http.Handler { return mux }, opts...)
}

// TestMediaGridTileIsTheTarget is the tile's half of the whole-row
// rule: a click on the picture, on the description and on the tile's
// empty corner all open the tile's Href, and the checkbox and the kebab
// each do their own thing without navigating, with a mouse and with a
// touch screen.
func TestMediaGridTileIsTheTarget(t *testing.T) {
	for _, leg := range []struct {
		name   string
		w, h   int64
		coarse bool
	}{{"1280 mouse", 1280, 900, false}, {"390 touch", 390, 844, true}} {
		t.Run(leg.name, func(t *testing.T) {
			rig := mediaRig(t, leg.coarse, mediaMux(t, make(chan url.Values, 4)))
			ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(leg.w, leg.h)); err != nil {
				t.Fatal(err)
			}
			load := func() {
				t.Helper()
				if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-media]", chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			load()
			requirePointer(t, ctx, leg.coarse)
			// The ids the probes read back: the first tile's link, box
			// and kebab, by position in the DOM.
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const tile = document.querySelector("[rst-media-tile]");
			  tile.id = "tile"; tile.querySelector("[rst-media-frame]").id = "frame"; tile.querySelector("img").id = "img";
			  tile.querySelector("[rst-media-name] > a").id = "link"; tile.querySelector("[rst-selbox] input").id = "check";
			  tile.querySelector("[rst-row-menu]").id = "menu"; tile.querySelector("[rst-row-menu] > summary").id = "kebab";
			  tile.querySelector("[rst-media-desc]").id = "desc"; return true; })()`, nil)); err != nil {
				t.Fatal(err)
			}
			for _, c := range []struct {
				where, sel string
				fx, fy     float64
			}{
				{"the middle of the picture", "#frame", 0.5, 0.5},
				{"the description", "#desc", 0.5, 0.5},
				{"the tile's bottom end corner", "#tile", 0.97, 0.97},
			} {
				p := probe(t, ctx, c.sel, c.fx, c.fy, 0, 0)
				if p.Hit != "link" {
					t.Errorf("%s: at %s the element under the pointer is %s, want the tile's name link", leg.name, c.where, p.Hit)
				}
				clickAndLand(t, ctx, p, "/go/tall")
				load()
				if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const tile = document.querySelector("[rst-media-tile]");
				  tile.id = "tile"; tile.querySelector("[rst-media-frame]").id = "frame"; tile.querySelector("[rst-media-name] > a").id = "link";
				  tile.querySelector("[rst-selbox] input").id = "check"; tile.querySelector("[rst-row-menu]").id = "menu";
				  tile.querySelector("[rst-row-menu] > summary").id = "kebab"; tile.querySelector("[rst-media-desc]").id = "desc"; return true; })()`, nil)); err != nil {
					t.Fatal(err)
				}
			}
			box := probe(t, ctx, "#check", 0.5, 0.5, 0, 0)
			if box.Hit != "check" {
				t.Errorf("%s: over the checkbox the element under the pointer is %s", leg.name, box.Hit)
			}
			clickAndStay(t, ctx, box, "", `document.getElementById("check").checked`)
			settleUntil(t, ctx, `getComputedStyle(document.getElementById("tile")).outlineStyle === "solid"`)
			// The pointer lands on the icon inside the summary, so the hit
			// is read as the control it is inside rather than by its id.
			kebab := probe(t, ctx, "#kebab", 0.5, 0.5, 0, 0)
			var onKebab bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`!!document.elementFromPoint(%v, %v).closest("#kebab")`, kebab.X, kebab.Y), &onKebab)); err != nil {
				t.Fatal(err)
			}
			if !onKebab {
				t.Errorf("%s: over the kebab the element under the pointer is %s, outside the summary", leg.name, kebab.Hit)
			}
			clickAndStay(t, ctx, kebab, "", `document.getElementById("menu").open`)
		})
	}
}

// TestMediaGridSelectionReachesTheBulkBarsForm is the no-JavaScript
// round trip: with script execution disabled in the engine, a reader
// ticks a tile, opens the bulk bar's menu and presses an action, and
// the form posts every checked tile's value under one name beside the
// action, which is all a handler needs to act on the selection.
func TestMediaGridSelectionReachesTheBulkBarsForm(t *testing.T) {
	posted := make(chan url.Values, 4)
	rig := mediaRig(t, true, mediaMux(t, posted))
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, emulation.SetScriptExecutionDisabled(true), chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("[rst-media]", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	// The page's own inline script marks the document when it runs;
	// CDP's evaluate runs regardless, so the marker is the proof.
	if readMedia(t, ctx).Scripted {
		t.Fatal("the page's script ran; this drive is the no-JavaScript round trip")
	}
	// The first tile's box, then the bulk bar's menu and its one action.
	// Scripts are off, so each is a real click at the control's box.
	click := func(sel string) {
		t.Helper()
		p := probeScripted(t, ctx, false, sel, 0.5, 0.5, 0, 0)
		if err := chromedp.Run(ctx, chromedp.MouseClickXY(p.X, p.Y)); err != nil {
			t.Fatal(err)
		}
	}
	click(`[rst-media-tile]:first-child [rst-selbox] input`)
	click(`[rst-bulkbar] [rst-dropdown] > summary`)
	click(`[rst-bulkbar] [rst-dropdown-menu] button[value="export"]`)
	var form url.Values
	select {
	case form = <-posted:
	case <-time.After(10 * time.Second):
		t.Fatal("the form never posted: the action button did not submit it with scripts off")
	}
	select {
	case again := <-posted:
		t.Fatalf("the form posted twice; the second time with %v", again)
	case <-time.After(500 * time.Millisecond):
	}
	if got, want := form["id"], []string{"tall", "wide-contain"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the selection posted id=%v, want %v: the tile ticked by hand and the one the server checked", got, want)
	}
	if got := form.Get("action"); got != "export" {
		t.Errorf("the action posted is %q, want export", got)
	}
}
