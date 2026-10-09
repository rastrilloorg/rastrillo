//go:build browser

package ui

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// bulkBarCase is one bulk bar the drives below render: the gallery's
// own sample, one with nothing to escalate to, the gallery's sample
// written the length four of its locales would write it, and two
// links longer than any phone's bar, one of them a single word with
// nowhere to break. At 390 the Russian and Arabic links are wider than
// the room beside the count and run on under Actions, below its row;
// that is the case the overlap checks exist for.
type bulkBarCase struct {
	ID, Count, Escalate, Menu string
}

var bulkBarCases = []bulkBarCase{
	{"en", "3 selected", "Select all 412 matching", "Actions"},
	{"short", "3 selected", "", "Actions"},
	{"pt", "3 selecionados", "Selecionar os 412 resultados", "Ações"},
	{"ru", "Выбрано: 3", "Выбрать все 412 подходящих", "Действия"},
	{"bn", "৩টি নির্বাচিত", "মিলে যাওয়া সব ৪১২টি নির্বাচন করুন", "কাজগুলো"},
	{"ar", "تم تحديد 3", "تحديد كل العناصر المطابقة وعددها 412", "إجراءات"},
	{"long", "3 selected", "Select all 4,812 matching records in every folder you can see", "Actions"},
	{"word", "3 selected", "Selectallfourthousandeighthundredtwelvematchingrecords", "Actions"},
}

// bulkBarPage is every case in a list card, as select mode shows it,
// in one direction.
func bulkBarPage(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<div rst-page>`)
	for _, c := range bulkBarCases {
		data := map[string]any{
			"DoneHref": "/posts", "DoneLabel": "Done selecting", "Count": c.Count,
			"MenuLabel": c.Menu, "MenuGroup": "rst-bulk-" + c.ID,
			"Actions": []any{map[string]any{"Value": "export", "Label": "Export"}},
		}
		if c.Escalate != "" {
			data["EscalateHref"], data["EscalateLabel"] = "/posts?select=all", c.Escalate
		}
		b.WriteString(`<div rst-list id="bulk-` + c.ID + `">` + render(t, "bulk-bar", data) + `</div>`)
	}
	b.WriteString(`</div>`)
	return strings.Replace(sizingDoc("bulk-bar", b.String()), `dir="ltr"`, `dir="`+dir+`"`, 1)
}

// box is a rectangle as the page lays it out.
type box struct{ L, T, R, B float64 }

func (b box) w() float64  { return b.R - b.L }
func (b box) cy() float64 { return (b.T + b.B) / 2 }
func (b box) overlaps(o box) bool {
	return b.L < o.R-0.5 && o.L < b.R-0.5 && b.T < o.B-0.5 && o.T < b.B-0.5
}

// bulkBarReading is one bar measured: its content box, each part's
// border box, how many lines each text took, and the column gap.
type bulkBarReading struct {
	ID                            string
	Bar, Close, Count, Summary    box
	Escalate                      *box
	CountLines, EscalateLines     int
	CountOneLine, EscalateOneLine float64 // each text's width on one line
	Gap                           float64
	RTL                           bool
}

const bulkBarJS = `(() => {
  const r = el => { const b = el.getBoundingClientRect(); return {L: b.left, T: b.top, R: b.right, B: b.bottom}; };
  const lines = el => { const g = document.createRange(); g.selectNodeContents(el); return new Set([...g.getClientRects()].filter(x => x.width > 0).map(x => Math.round(x.top))).size; };
  return JSON.stringify([...document.querySelectorAll("[rst-list][id^=bulk-]")].map(list => {
    const bar = list.querySelector("[rst-bulkbar]"), cs = getComputedStyle(bar), b = bar.getBoundingClientRect();
    const esc = bar.querySelector("[rst-bulkbar-escalate]");
    const one = el => { if (!el) return 0; const c = el.cloneNode(true); c.style.cssText = "position: absolute; visibility: hidden; white-space: nowrap; max-inline-size: none; inline-size: max-content"; el.parentNode.appendChild(c); const w = c.getBoundingClientRect().width; c.remove(); return w; };
    return {ID: list.id.slice(5), RTL: cs.direction === "rtl", Gap: parseFloat(cs.columnGap),
      Bar: {L: b.left + parseFloat(cs.paddingLeft), T: b.top + parseFloat(cs.paddingTop), R: b.right - parseFloat(cs.paddingRight), B: b.bottom - parseFloat(cs.paddingBottom)},
      Close: r(bar.querySelector("[rst-bulkbar-close]")), Count: r(bar.querySelector("[rst-bulkbar-count]")),
      Summary: r(bar.querySelector("[rst-dropdown] > summary")), Escalate: esc ? r(esc) : null,
      CountLines: lines(bar.querySelector("[rst-bulkbar-count]")), EscalateLines: esc ? lines(esc) : 0, EscalateOneLine: one(esc), CountOneLine: one(bar.querySelector("[rst-bulkbar-count]"))};
  }));
})()`

func readBulkBars(t *testing.T, ctx context.Context) map[string]bulkBarReading {
	t.Helper()
	var got []bulkBarReading
	at(t, ctx, bulkBarJS, &got)
	if len(got) != len(bulkBarCases) {
		t.Fatalf("measured %d bulk bars, want %d", len(got), len(bulkBarCases))
	}
	out := map[string]bulkBarReading{}
	for _, g := range got {
		out[g.ID] = g
	}
	return out
}

// TestTheBulkBarNeverWrapsItsText: on a 390px phone the bar squeezed
// "3 selected" and "Select all 412 matching" onto two lines each,
// between a close button at one end and Actions at the other. Each
// text keeps one line. When the bar's parts fit one row they stay on
// it; when they do not, the escalate link moves to a second row that
// spans the bar, starting where the close button starts, and the close
// button, the count and Actions keep the first, Actions at the
// trailing edge. A link wider than the whole bar wraps, as the one
// thing left that keeps it inside the card. The legs pin both shapes
// on the gallery's own sample (two rows at 390, one at 540) and on a
// bar with no escalate link (one row), and hold the longer locales,
// the over-long links, 320px and a right-to-left page to the same
// rule.
func TestTheBulkBarNeverWrapsItsText(t *testing.T) {
	for _, leg := range []struct {
		name    string
		w       int64
		dir     string
		twoRows map[string]bool // the shape a case must take; absent: whichever its width calls for
	}{
		{"390 touch", 390, "ltr", map[string]bool{"en": true, "short": false}},
		{"540 touch", 540, "ltr", map[string]bool{"en": false, "short": false}},
		{"390 touch rtl", 390, "rtl", map[string]bool{"short": false}},
		{"320 touch", 320, "ltr", map[string]bool{"short": false, "long": true, "word": true}},
		{"320 touch rtl", 320, "rtl", map[string]bool{"short": false, "long": true, "word": true}},
	} {
		t.Run(leg.name, func(t *testing.T) {
			rig := sizingRig(t, true, map[string]string{"/": bulkBarPage(t, leg.dir)})
			ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
			defer cancel()
			mustRun(t, ctx, chromedp.EmulateViewport(leg.w, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#bulk-ar", chromedp.ByQuery))
			requirePointer(t, ctx, true)
			bars := readBulkBars(t, ctx)
			for _, c := range bulkBarCases {
				checkBulkBar(t, bars[c.ID], leg.dir == "rtl", leg.twoRows)
			}
		})
	}
}

func checkBulkBar(t *testing.T, g bulkBarReading, rtl bool, twoRows map[string]bool) {
	t.Helper()
	if g.RTL != rtl {
		t.Fatalf("%s: the bar's direction is rtl=%v, the leg needs %v", g.ID, g.RTL, rtl)
	}
	// One line unless the first row, close, count and Actions with the
	// gaps between them, is wider than the bar with the count on one.
	if first := g.Close.w() + g.CountOneLine + g.Summary.w() + 2*g.Gap; first <= g.Bar.w()+0.5 && g.CountLines != 1 {
		t.Errorf("%s: the count takes %d lines; the first row needs %.1fpx of the bar's %.1f with it on one", g.ID, g.CountLines, first, g.Bar.w())
	}
	sameRow := func(a, b box) bool { return math.Abs(a.cy()-b.cy()) < 2 }
	// start and end read the inline axis whichever way it runs.
	start := func(b box) float64 {
		if rtl {
			return -b.R
		}
		return b.L
	}
	end := func(b box) float64 {
		if rtl {
			return -b.L
		}
		return b.R
	}
	if math.Abs(g.Close.T-g.Count.T) > 0.5 || math.Abs(g.Count.T-g.Summary.T) > 0.5 {
		t.Errorf("%s: the close button (%.1f), the count (%.1f) and Actions (%.1f) do not start one row", g.ID, g.Close.T, g.Count.T, g.Summary.T)
	}
	if !(end(g.Close) <= start(g.Count)+0.5 && end(g.Count) <= start(g.Summary)+0.5) {
		t.Errorf("%s: the first row is not close, count, Actions in reading order: %+v %+v %+v", g.ID, g.Close, g.Count, g.Summary)
	}
	if d := end(g.Bar) - end(g.Summary); math.Abs(d) > 0.5 {
		t.Errorf("%s: Actions ends %.1fpx short of the bar's trailing edge, want it at the edge", g.ID, d)
	}
	for name, b := range map[string]box{"close": g.Close, "count": g.Count, "Actions": g.Summary} {
		if b.L < g.Bar.L-0.5 || b.R > g.Bar.R+0.5 {
			t.Errorf("%s: the %s button runs outside the bar: %+v in %+v", g.ID, name, b, g.Bar)
		}
	}
	if g.Escalate == nil {
		if want, ok := twoRows[g.ID]; ok && want {
			t.Fatalf("%s: the leg wants two rows, but the bar has no escalate link", g.ID)
		}
		return
	}
	e := *g.Escalate
	// One line unless the link on one line is wider than the bar.
	if g.EscalateOneLine <= g.Bar.w()+0.5 && g.EscalateLines != 1 {
		t.Errorf("%s: the escalate link takes %d lines; on one it is %.1fpx, and the bar is %.1f", g.ID, g.EscalateLines, g.EscalateOneLine, g.Bar.w())
	}
	if e.L < g.Bar.L-0.5 || e.R > g.Bar.R+0.5 {
		t.Errorf("%s: the escalate link runs outside the bar: %+v in %+v", g.ID, e, g.Bar)
	}
	for name, b := range map[string]box{"close": g.Close, "count": g.Count, "Actions": g.Summary} {
		if e.overlaps(b) {
			t.Errorf("%s: the escalate link overlaps the %s: %+v and %+v", g.ID, name, e, b)
		}
	}
	// One row fits when the four parts and the gaps between them do.
	fits := g.Close.w()+g.Count.w()+g.EscalateOneLine+g.Summary.w()+3*g.Gap <= g.Bar.w()+0.5
	two := !sameRow(e, g.Count)
	if want, ok := twoRows[g.ID]; ok && want != two {
		t.Errorf("%s: two rows is %v, this leg wants %v (one row needs %.1fpx of the bar's %.1f)", g.ID, two, want, g.Close.w()+g.Count.w()+g.EscalateOneLine+g.Summary.w()+3*g.Gap, g.Bar.w())
	}
	if fits && two {
		t.Errorf("%s: the parts fit one row (%.1fpx of %.1f) but the escalate link took a second", g.ID, g.Close.w()+g.Count.w()+g.EscalateOneLine+g.Summary.w()+3*g.Gap, g.Bar.w())
	}
	if !two {
		if !(end(g.Count) <= start(e)+0.5 && end(e) <= start(g.Summary)+0.5) {
			t.Errorf("%s: on one row the escalate link is not between the count and Actions: %+v %+v %+v", g.ID, g.Count, e, g.Summary)
		}
		return
	}
	if e.T < g.Count.B-0.5 {
		t.Errorf("%s: the escalate link's second row starts at %.1f, above the count's bottom, %.1f", g.ID, e.T, g.Count.B)
	}
	if d := start(e) - start(g.Close); math.Abs(d) > 0.5 {
		t.Errorf("%s: the escalate link starts %.1fpx from the close button's start, want its row to span the bar", g.ID, d)
	}
}

// TestTheBulkBarIsUnchangedOnADesktop pins the gallery sample's bar at
// 1280 with a mouse, measured before the phone's two-row layout
// arrived: every part's box relative to the bar's, and the bar's
// height. The phone's layout is in the touch query, and this is the
// proof that none of it leaked out.
func TestTheBulkBarIsUnchangedOnADesktop(t *testing.T) {
	rig := sizingRig(t, false, map[string]string{"/": bulkBarPage(t, "ltr")})
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	mustRun(t, ctx, chromedp.EmulateViewport(1280, 844), chromedp.Navigate(rig.Origin+"/"), chromedp.WaitVisible("#bulk-en", chromedp.ByQuery))
	requirePointer(t, ctx, false)
	var got map[string]box
	at(t, ctx, `(() => { const bar = document.querySelector("#bulk-en [rst-bulkbar]"), o = bar.getBoundingClientRect();
	  const r = sel => { const b = bar.querySelector(sel).getBoundingClientRect(); return {L: b.left - o.left, T: b.top - o.top, R: b.right - o.left, B: b.bottom - o.top}; };
	  return JSON.stringify({Bar: {L: 0, T: 0, R: o.width, B: o.height}, Close: r("[rst-bulkbar-close]"), Count: r("[rst-bulkbar-count]"), Escalate: r("[rst-bulkbar-escalate]"), Summary: r("[rst-dropdown] > summary")}); })()`, &got)
	want := map[string]box{
		"Bar":      {0, 0, 990, 47},
		"Close":    {9.59375, 7, 41.59375, 39},
		"Count":    {52.78125, 13.625, 125.328125, 32.375},
		"Escalate": {136.515625, 7, 288.921875, 39},
		"Summary":  {890.328125, 7, 976.40625, 39},
	}
	for name, w := range want {
		g := got[name]
		if math.Abs(g.L-w.L) > 0.05 || math.Abs(g.T-w.T) > 0.05 || math.Abs(g.R-w.R) > 0.05 || math.Abs(g.B-w.B) > 0.05 {
			t.Errorf("%s at 1280: %+v, pinned at %+v", name, g, w)
		}
	}
}
