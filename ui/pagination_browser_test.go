//go:build browser

package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// pageStrip builds a pagination strip's Items the way the blog example
// and the generated list actions do: Previous, the page numbers with a
// Gap wherever a run is skipped, and Next, disabled at either end, the
// two steps marked with their Rel. Every
// page in shown is listed; gaps go between pages that are not adjacent.
func pageStrip(current int, shown ...int) []any {
	items := []any{map[string]any{"Label": "Previous", "Href": fmt.Sprintf("/posts?page=%d", current-1), "Rel": "prev"}}
	if current == shown[0] {
		items[0] = map[string]any{"Label": "Previous", "Disabled": true, "Rel": "prev"}
	}
	for i, n := range shown {
		if i > 0 && n != shown[i-1]+1 {
			items = append(items, map[string]any{"Gap": true})
		}
		if n == current {
			items = append(items, map[string]any{"Label": fmt.Sprint(n), "Current": true})
		} else {
			items = append(items, map[string]any{"Label": fmt.Sprint(n), "Href": fmt.Sprintf("/posts?page=%d", n)})
		}
	}
	if current == shown[len(shown)-1] {
		return append(items, map[string]any{"Label": "Next", "Disabled": true, "Rel": "next"})
	}
	return append(items, map[string]any{"Label": "Next", "Href": fmt.Sprintf("/posts?page=%d", current+1), "Rel": "next"})
}

// paginationCases are the strips the drives render, each with what a
// phone must show: ‹ and › for the chevrons, the page numbers, … for
// an ellipsis. "mid" is the gallery's sample. "flat" lists every page
// with no gaps, the way the generated list actions do, so the phone's
// ellipses there are the partial's own.
var paginationCases = []struct {
	ID     string
	Items  []any
	Narrow string
}{
	{"mid", pageStrip(4, 1, 3, 4, 5, 9), "‹ 1 … 4 … 9 ›"},
	{"first", pageStrip(1, 1, 2), "‹ 1 2 ›"},
	{"p2", pageStrip(2, 1, 2, 3, 9), "‹ 1 2 … 9 ›"},
	{"p3", pageStrip(3, 1, 2, 3, 4, 9), "‹ 1 … 3 … 9 ›"},
	{"p8", pageStrip(8, 1, 7, 8, 9), "‹ 1 … 8 9 ›"},
	{"last", pageStrip(9, 1, 8, 9), "‹ 1 … 9 ›"},
	{"flat", pageStrip(4, 1, 2, 3, 4, 5, 6, 7), "‹ 1 … 4 … 7 ›"},
	{"only", pageStrip(1, 1), "‹ 1 ›"},
}

func paginationPage(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<div rst-page>`)
	for _, c := range paginationCases {
		b.WriteString(`<div id="pg-` + c.ID + `">` + render(t, "pagination", map[string]any{"Label": "Posts pages", "Items": c.Items}) + `</div>`)
	}
	b.WriteString(`</div>`)
	return strings.Replace(sizingDoc("pagination", b.String()), `dir="ltr"`, `dir="`+dir+`"`, 1)
}

// stripItem is one visible child of a strip: its box, its text, whether
// it shows an icon, how wide its words are drawn, which way its icon's
// point faces along the inline axis (-1 left, 1 right), and whether it
// is a target (a link or a span that is not an ellipsis).
type stripItem struct {
	Text           string
	L, T, R, B     float64
	Icon           bool
	WordsW, Point  float64
	Target, Ellips bool
}

type stripReading struct {
	ID          string
	L, R        float64 // the strip's content box
	Items       []stripItem
	Hidden      []string // every child laid out with no box, by text
	Center, RTL bool
}

// paginationJS reads every strip. An icon's direction is read off the
// glyph: the midpoint of its path is the chevron's point, its two ends
// the arms, and the point's offset from the arms' middle, run through
// the icon's transform, says which way it faces on screen.
const paginationJS = `(() => JSON.stringify([...document.querySelectorAll("[id^=pg-] [rst-pagination]")].map(nav => {
  const cs = getComputedStyle(nav), nb = nav.getBoundingClientRect();
  const out = {ID: nav.parentElement.id.slice(3), L: nb.left + parseFloat(cs.paddingLeft), R: nb.right - parseFloat(cs.paddingRight), RTL: cs.direction === "rtl", Items: [], Hidden: []};
  for (const el of nav.children) {
    const b = el.getBoundingClientRect(), text = el.textContent.trim();
    if (b.width === 0 && b.height === 0) { out.Hidden.push(text); continue; }
    const svg = el.querySelector("svg"), sb = svg ? svg.getBoundingClientRect() : null, icon = !!sb && sb.width > 0;
    let point = 0;
    if (icon) {
      const p = svg.querySelector("path"), len = p.getTotalLength(), a = p.getPointAtLength(0), z = p.getPointAtLength(len), m = p.getPointAtLength(len / 2);
      const t = new DOMMatrix(getComputedStyle(svg).transform === "none" ? undefined : getComputedStyle(svg).transform);
      const v = t.transformPoint(new DOMPoint(m.x - (a.x + z.x) / 2, m.y - (a.y + z.y) / 2)), o = t.transformPoint(new DOMPoint(0, 0));
      point = Math.sign(Math.round((v.x - o.x) * 100));
    }
    let words = 0;
    for (const n of el.querySelectorAll("*")) if (n.tagName !== "svg" && !n.closest("svg") && n.textContent.trim()) words = Math.max(words, n.getBoundingClientRect().width);
    if (!el.children.length || [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim())) { const g = document.createRange(); g.selectNodeContents(el); words = Math.max(words, g.getBoundingClientRect().width); }
    const ellips = el.hasAttribute("rst-pagination-gap") || text === "…";
    out.Items.push({Text: text, L: b.left, T: b.top, R: b.right, B: b.bottom, Icon: icon, WordsW: words, Point: point, Target: !ellips, Ellips: ellips});
  }
  return out;
})))()`

func readStrips(t *testing.T, ctx context.Context) map[string]stripReading {
	t.Helper()
	var got []stripReading
	at(t, ctx, paginationJS, &got)
	if len(got) != len(paginationCases) {
		t.Fatalf("measured %d strips, want %d", len(got), len(paginationCases))
	}
	out := map[string]stripReading{}
	for _, g := range got {
		out[g.ID] = g
	}
	return out
}

// narrowSequence spells a strip as a phone shows it: a chevron for an
// item that draws an icon and no words, its text otherwise.
func narrowSequence(s stripReading) string {
	var parts []string
	for i, it := range s.Items {
		switch {
		case it.Icon && it.WordsW <= 1 && i == 0:
			parts = append(parts, "‹")
		case it.Icon && it.WordsW <= 1:
			parts = append(parts, "›")
		default:
			parts = append(parts, it.Text)
		}
	}
	return strings.Join(parts, " ")
}

// TestPaginationIsOneBalancedLineOnAPhone: at 390 the gallery's "middle
// of a long list" strip (Previous 1 … 3 4 5 … 9 Next) wrapped onto two
// rows. On a phone Previous and Next are chevrons that keep their words
// for a screen reader, only the first, current and last pages show,
// with one ellipsis wherever pages are skipped, and the strip is one
// line spread across the width. The cases put the current page at
// either end and beside each, where a careless rule draws two ellipses
// in a row or one between two adjacent pages. Each target stays a tap,
// down to a 320px phone.
func TestPaginationIsOneBalancedLineOnAPhone(t *testing.T) {
	for _, leg := range []struct {
		w   int64
		dir string
	}{{390, "ltr"}, {390, "rtl"}, {320, "ltr"}, {320, "rtl"}} {
		dir := leg.dir
		t.Run(fmt.Sprintf("%d touch %s", leg.w, dir), func(t *testing.T) {
			rig := sizingRig(t, true, map[string]string{"/": paginationPage(t, dir)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#pg-only", chromedp.ByQuery))
			requirePointer(t, ctx, true)
			strips := readStrips(t, ctx)
			for _, c := range paginationCases {
				s := strips[c.ID]
				if s.RTL != (dir == "rtl") {
					t.Fatalf("%s: the strip's direction is rtl=%v, the leg needs %s", c.ID, s.RTL, dir)
				}
				if got := narrowSequence(s); got != c.Narrow {
					t.Errorf("%s: the phone shows %q, want %q", c.ID, got, c.Narrow)
					continue
				}
				checkStripLine(t, c.ID, s)
				for i, it := range s.Items {
					if it.Target && (it.R-it.L < 43.5 || it.B-it.T < 43.5) {
						t.Errorf("%s: %q is %.1f×%.1f, under the 44px tap", c.ID, it.Text, it.R-it.L, it.B-it.T)
					}
					if !it.Icon {
						continue
					}
					// The chevron points out of the strip, at the end it
					// sits on: Previous at the start of the line, Next at
					// its end, whichever way the line runs.
					first := i == 0
					wantText := map[bool]string{true: "Previous", false: "Next"}[first]
					if it.Text != wantText {
						t.Errorf("%s: the chevron at position %d reads %q to a screen reader, want %q", c.ID, i, it.Text, wantText)
					}
					onLeft := (first && dir == "ltr") || (!first && dir == "rtl")
					wantPoint := map[bool]float64{true: -1, false: 1}[onLeft]
					if it.Point != wantPoint {
						t.Errorf("%s: %s's chevron points %v, want %v (toward its own end of the strip)", c.ID, it.Text, it.Point, wantPoint)
					}
					if (onLeft && it.L-s.L > 0.5) || (!onLeft && s.R-it.R > 0.5) {
						t.Errorf("%s: %s is not at its end of the strip: %.1f–%.1f in %.1f–%.1f", c.ID, it.Text, it.L, it.R, s.L, s.R)
					}
				}
			}
		})
	}
}

// checkStripLine holds a strip to one line, spread from edge to edge
// with the same space between every pair of neighbours.
func checkStripLine(t *testing.T, id string, s stripReading) {
	t.Helper()
	items := s.Items
	for _, it := range items[1:] {
		if math.Abs(it.T-items[0].T) > 1 {
			t.Errorf("%s: %q sits at %.1f and %q at %.1f; the strip is not one line", id, it.Text, it.T, items[0].Text, items[0].T)
		}
	}
	left, right := items[0].L, items[0].R
	for _, it := range items {
		left, right = math.Min(left, it.L), math.Max(right, it.R)
	}
	if left-s.L > 0.5 || s.R-right > 0.5 {
		t.Errorf("%s: the strip's items span %.1f–%.1f of %.1f–%.1f; it is not spread across the width", id, left, right, s.L, s.R)
	}
	var spaces []float64
	for i := 1; i < len(items); i++ {
		a, b := items[i-1], items[i]
		if s.RTL {
			spaces = append(spaces, a.L-b.R)
		} else {
			spaces = append(spaces, b.L-a.R)
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, sp := range spaces {
		lo, hi = math.Min(lo, sp), math.Max(hi, sp)
	}
	if hi-lo > 1 {
		t.Errorf("%s: the spaces between items run from %.1f to %.1f; the strip is not balanced", id, lo, hi)
	}
}

// TestPaginationIsUnchangedOnADesktop pins the gallery's strip at 1280
// with a mouse as it was before the phone's strip arrived: every item
// shown with its words and no icon, at the same place and size.
func TestPaginationIsUnchangedOnADesktop(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": paginationPage(t, "ltr")})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#pg-only", chromedp.ByQuery))
	requirePointer(t, ctx, false)
	s := readStrips(t, ctx)["mid"]
	type pinned struct {
		Text string
		L, W float64
	}
	want := []pinned{{"Previous", 0, 74.016}, {"1", 78.016, 53.188}, {"…", 135.203, 22.5}, {"3", 161.703, 53.188}, {"4", 218.891, 53.188},
		{"5", 276.078, 53.188}, {"…", 333.266, 22.5}, {"9", 359.766, 53.188}, {"Next", 416.953, 53.188}}
	var got []pinned
	for _, it := range s.Items {
		if it.Icon {
			t.Errorf("%q draws an icon on a desktop", it.Text)
		}
		if it.T-s.Items[0].T != 0 || it.B-it.T != s.Items[0].B-s.Items[0].T {
			t.Errorf("%q is at %.2f, %.2f tall; the first item is at %.2f, %.2f tall", it.Text, it.T, it.B-it.T, s.Items[0].T, s.Items[0].B-s.Items[0].T)
		}
		got = append(got, pinned{it.Text, math.Round((it.L-s.L)*1000) / 1000, math.Round((it.R-it.L)*1000) / 1000})
	}
	if len(s.Hidden) != 0 {
		t.Errorf("the desktop strip hides %q", s.Hidden)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the desktop strip moved:\n got %v\nwant %v", got, want)
	}
	if h := s.Items[0].B - s.Items[0].T; math.Abs(h-32) > 0.01 {
		t.Errorf("the desktop chips are %.2fpx tall, pinned at 32", h)
	}
}
