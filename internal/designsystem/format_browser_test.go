//go:build browser

package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
	"amadan.net/rastrillo/rastrillo/ui"
)

// formatPair is one sample as its preview frame holds it and as the
// HTML tab lays it out, each inside the same wrapper.
type formatPair struct {
	name     string
	raw, fmt string
}

// formatPairs is every sample with an HTML tab, in one locale: each
// partial state (a grouped partial's states one at a time, as its panel
// shows them), each ui.Styleguide idiom, each format and each
// hand-written screen. The sign-in screens have no HTML tab.
func formatPairs(t *testing.T, locale string) []formatPair {
	t.Helper()
	tmpl := sampleTree(t, locale)
	var out []formatPair
	add := func(name string, w wrapper, html string) {
		out = append(out, formatPair{name: name, raw: wrap(w, html), fmt: wrap(w, codeview.Format(html))})
	}
	for _, fam := range families() {
		for _, doc := range fam.Partials {
			for i, s := range doc.States {
				html, err := renderSample(tmpl, doc.Name, i, s, locale)
				if err != nil {
					t.Fatal(err)
				}
				add(fmt.Sprintf("%s (%s)", doc.Name, s.State), doc.Wrap, string(html))
			}
		}
	}
	idioms := ui.Styleguide()
	names := make([]string, 0, len(idioms))
	for name := range idioms {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add("idiom "+name, wrapBare, localMarkup(locale, idioms[name]))
	}
	for _, d := range formatDocs() {
		add("format "+d.Key, wrapBare, localMarkup(locale, d.Markup))
	}
	for _, d := range screenDocs() {
		if d.Markup != "" {
			add("screen "+d.Key, wrapBare, localMarkup(locale, d.Markup))
		}
	}
	for i, f := range formatRegressions {
		add(fmt.Sprintf("regression %d", i), wrapBare, f)
	}
	return out
}

// formatRegressions are markup no sample holds yet, each a case the
// formatter once got wrong, rendered both ways like the samples.
var formatRegressions = []string{
	// A select is inline-level: the formatter put a line break between
	// it and the word after it, and the break showed as a space.
	`<p>Pick <select><option>A</option><option>B</option></select><span>B</span> after.</p>`,
	// A button in a flex row whose own style preserves whitespace: the
	// formatter gave its label a line and an indent, and under
	// white-space: pre both render.
	`<div rst-form-foot><button rst-btn type="button" style="white-space: pre">Pay <em>now</em></button><a rst-btn href="/">Back</a></div>`,
	// The same button with spaces around its style's =, which HTML
	// allows and the formatter's attribute reader did not.
	`<div rst-form-foot><button rst-btn type="button" style = "white-space: pre">Pay <em>now</em></button><a rst-btn href="/">Back</a></div>`,
}

// layoutProbe reads what a reader sees: every element's boxes, every
// run of text's boxes, in document order, and the page's visible text.
// Whitespace-only text is left out on purpose: the formatted copy has
// more of it, and the point is that none of it draws anything.
//
// Every animation is stopped at its start first: the busy spinner turns,
// and a turning square's bounding box is a different size from one
// frame to the next whatever the markup.
const layoutProbe = `(() => {
  for (const a of document.getAnimations()) { a.pause(); a.currentTime = 0; }
  const round = r => [r.x, r.y, r.width, r.height];
  const boxes = [];
  for (const el of document.body.querySelectorAll("*")) {
    boxes.push(el.tagName + " " + JSON.stringify([...el.getClientRects()].map(round)));
  }
  const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n = walk.nextNode(); n; n = walk.nextNode()) {
    if (!n.data.trim()) continue;
    const range = document.createRange();
    range.selectNodeContents(n);
    boxes.push("#text " + JSON.stringify(n.data.trim()) + " " + JSON.stringify([...range.getClientRects()].map(round)));
  }
  return JSON.stringify({boxes, text: document.body.innerText});
})()`

type layoutReading struct {
	Boxes []string
	Text  string
}

// disclosureState puts a document's disclosures into state k and
// resolves once the page has drawn it: -1 closes every <details>, -2
// opens every one (in document order, so where a name group allows only
// one open the last of each group is), and k >= 0 opens the k-th one,
// the disclosures it sits in, and whatever else it takes for it to be
// seen, with every other closed. Two frames and a task, because a
// details' toggle event is queued and the scripts that place a menu
// answer it.
//
// "Whatever else it takes" is the case a DOM walk misses. A phone
// topbar's account menu sits in [rst-shell-tail], which is display: none
// until the shell's Menu, a SIBLING of the tail, is open, so opening the
// account menu and its ancestors leaves it unseen. When the chosen
// disclosure has no box, the others are tried one at a time, each with
// its own ancestors and never one that would close a disclosure already
// open through a shared name, and kept if the chosen one then shows.
const disclosureState = `k => new Promise(done => {
  const all = [...document.querySelectorAll("details")];
  const chain = d => { const c = []; for (; d; d = d.parentElement && d.parentElement.closest("details")) c.unshift(d); return c; };
  const shown = d => (d.querySelector(":scope > summary") || d).getClientRects().length > 0;
  for (const d of all) d.open = false;
  if (k === -2) for (const d of all) d.open = true;
  if (k >= 0) {
    const target = all[k], open = chain(target);
    for (const d of open) d.open = true;
    const names = new Set(open.map(d => d.name).filter(Boolean));
    for (const d of all) {
      if (shown(target)) break;
      const more = chain(d).filter(x => !open.includes(x));
      if (!more.length || target.contains(d) || more.some(x => x.name && names.has(x.name))) continue;
      for (const x of more) x.open = true;
      if (shown(target)) {
        open.push(...more);
        for (const x of more) if (x.name) names.add(x.name);
      } else {
        for (const x of more) x.open = false;
      }
    }
  }
  requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(done, 0)));
})`

// TestTheHTMLTabRendersExactlyLikeThePreview is the proof behind the
// HTML tab's layout. codeview.Format puts a child on a line of its own
// only where whitespace cannot change what renders; this renders every
// sample twice, as written and as the tab lays it out, and requires
// every element and every run of text to land in the same place within
// half a pixel, and the visible text to be the same. In English and in
// Arabic, because a space that shows is a space in either direction, and
// at a desktop frame's width and a phone's, because the flex rules a
// container is listed for must hold at both. Each document is read as it
// loads, with every disclosure closed, and with each one open in turn:
// a menu's content and what its opening reveals have no boxes until
// then, so a reading on load alone would pass whitespace that changes
// an open menu.
//
// The Copy button copies the formatted text, so this is what makes
// pasting it safe. A sample that differs is the formatter's rule for
// that container being wrong: fix the rule in internal/codeview, never
// exempt the sample.
func TestTheHTMLTabRendersExactlyLikeThePreview(t *testing.T) {
	docs := map[string]string{}
	type job struct {
		locale, name, raw, fmt string
	}
	var jobs []job
	for _, locale := range []string{"en", "ar"} {
		for i, p := range formatPairs(t, locale) {
			if p.raw == p.fmt {
				continue
			}
			base := fmt.Sprintf("/fmt/%s/%d", locale, i)
			docs[base+"-raw.html"] = previewDoc(mountPath, RootTheme(), locale, p.name, deaden(mountPath, p.raw))
			docs[base+"-fmt.html"] = previewDoc(mountPath, RootTheme(), locale, p.name, deaden(mountPath, p.fmt))
			jobs = append(jobs, job{locale, p.name, base + "-raw.html", base + "-fmt.html"})
		}
	}
	// The control: the short-forms format with a line break put between
	// "(" and its <abbr>, mid-sentence, where a space shows. A probe that
	// cannot tell that copy from the preview is measuring nothing.
	var control job
	for _, p := range formatPairs(t, "en") {
		if p.name != "format abbr" {
			continue
		}
		broken := strings.Replace(p.raw, "(<abbr", "(\n<abbr", 1)
		if broken == p.raw {
			t.Fatal("the control's sentence is not in the short-forms sample")
		}
		docs["/fmt/control-raw.html"] = previewDoc(mountPath, RootTheme(), "en", p.name, p.raw)
		docs["/fmt/control.html"] = previewDoc(mountPath, RootTheme(), "en", p.name, broken)
		control = job{"en", "control", "/fmt/control-raw.html", "/fmt/control.html"}
	}
	if control.name == "" {
		t.Fatal("no short-forms sample to build the control from")
	}
	// The second control is the same space put where a reading taken on
	// load cannot see it: in content that is display: none until a
	// disclosure opens, the way a phone's topbar keeps its nav in a
	// [rst-shell-tail] its closed menu hides. (A closed <details>'s own
	// content still has boxes in Chromium, so it would not do.) Only a
	// drive that opens the menu catches the space.
	hidden := `<style>details:not([open]) + div { display: none; }</style><details><summary>More</summary></details><div><p>Read the <abbr title="frequently asked questions">FAQ</abbr>first.</p></div>`
	docs["/fmt/hidden-raw.html"] = previewDoc(mountPath, RootTheme(), "en", "hidden control", hidden)
	// The third is that space inside the topbar's account menu at a
	// phone's width, where the account menu is inside [rst-shell-tail]
	// and the tail is shown only while its SIBLING, the shell's Menu, is
	// open. Opening the account menu and the disclosures it sits in is
	// not enough to see it. The words are in a <span> because at that
	// width the menu's links are flex, and between a link's own flex
	// items a space would not show either way.
	for _, p := range formatPairs(t, "en") {
		if p.name != "idiom shell-topbar" {
			continue
		}
		marked := strings.Replace(p.raw, `<a href="/settings">Settings</a>`, `<a href="/settings"><span>Set <b>up</b>tings</span></a>`, 1)
		if marked == p.raw {
			t.Fatal("the account menu's Settings link is not in the topbar sample")
		}
		// Chromium lays out a closed <details>'s content, boxes and all,
		// where other engines give it none; this rule gives the control
		// the other engines' behaviour, so only the account menu being
		// open, with the tail shown, puts the words on screen.
		marked = `<style>details:not([open]) > :not(summary) { display: none; }</style>` + marked
		docs["/fmt/account-raw.html"] = previewDoc(mountPath, RootTheme(), "en", p.name, marked)
		docs["/fmt/account.html"] = previewDoc(mountPath, RootTheme(), "en", p.name, strings.Replace(marked, "</b>tings", "</b>\ntings", 1))
	}
	if docs["/fmt/account.html"] == "" {
		t.Fatal("no topbar sample to build the account-menu control from")
	}
	docs["/fmt/hidden.html"] = previewDoc(mountPath, RootTheme(), "en", "hidden control", strings.Replace(hidden, "</abbr>first", "</abbr>\nfirst", 1))
	if len(jobs) < 100 {
		t.Fatalf("only %d samples change when formatted; the sample walk or the formatter has stopped", len(jobs))
	}
	tree := treeHandler(t)
	rig := harness.New(t, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if doc, ok := docs[r.URL.Path]; ok {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(doc))
				return
			}
			tree.ServeHTTP(w, r)
		})
	})
	ctx, cancel := context.WithTimeout(rig.Context(), 15*time.Minute)
	defer cancel()

	// read is one document in each state a reader can put it in: as it
	// loads, with every disclosure open, with every one closed, and with
	// each one open in turn along with what it takes to show it.
	read := func(url string) []layoutReading {
		probe := func() layoutReading {
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(layoutProbe, &raw)); err != nil {
				t.Fatalf("%s: %v", url, err)
			}
			var out layoutReading
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		var n int
		if err := chromedp.Run(ctx, chromedp.Navigate(rig.Origin+url),
			chromedp.Evaluate(`new Promise(r => document.fonts.ready.then(() => requestAnimationFrame(() => requestAnimationFrame(r))))`, nil, awaitPromise),
			chromedp.Evaluate(`document.querySelectorAll("details").length`, &n)); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		out := []layoutReading{probe()}
		for k := -2; k < n; k++ {
			if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(%s)(%d)`, disclosureState, k), nil, awaitPromise)); err != nil {
				t.Fatalf("%s: %v", url, err)
			}
			out = append(out, probe())
		}
		return out
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(900, 900)); err != nil {
		t.Fatal(err)
	}
	if want, got := read(control.raw), read(control.fmt); sameReadings(want, got) == "" {
		t.Fatal("a space inside a sentence moved no box; this drive cannot see a formatter that changes rendering")
	}
	if want, got := read("/fmt/hidden-raw.html"), read("/fmt/hidden.html"); sameReadings(want, got) == "" {
		t.Fatal("a space inside a closed disclosure moved no box once opened; this drive cannot see a formatter that changes what a menu shows")
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 900)); err != nil {
		t.Fatal(err)
	}
	if want, got := read("/fmt/account-raw.html"), read("/fmt/account.html"); sameReadings(want, got) == "" {
		t.Fatal("a space inside the topbar's account menu at 390px moved no box; this drive never sees that menu open with the shell menu that shows it")
	}
	var compared, states int
	for _, width := range []int64{900, 390} {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			where := fmt.Sprintf("%s at %dpx: %s", j.locale, width, j.name)
			want, got := read(j.raw), read(j.fmt)
			compared++
			states += len(want)
			if diff := sameReadings(want, got); diff != "" {
				t.Errorf("%s: %s", where, diff)
			}
		}
	}
	t.Logf("compared %d samples in %d states, each rendered as written and as formatted", compared, states)
}

// sameReadings compares a document's readings state by state, and says
// how the first difference shows; "" when there is none.
func sameReadings(want, got []layoutReading) string {
	if len(want) != len(got) {
		return fmt.Sprintf("%d disclosure states in the preview, %d in the formatted copy", len(want), len(got))
	}
	for s := range want {
		state := "as loaded"
		switch {
		case s == 1:
			state = "with every disclosure open"
		case s == 2:
			state = "with every disclosure closed"
		case s > 2:
			state = fmt.Sprintf("with disclosure %d open, and what shows it", s-3)
		}
		if got[s].Text != want[s].Text {
			return fmt.Sprintf("%s, the visible text differs\npreview: %q\n    tab: %q", state, want[s].Text, got[s].Text)
		}
		if len(got[s].Boxes) != len(want[s].Boxes) {
			return fmt.Sprintf("%s, %d boxes in the preview, %d in the formatted copy", state, len(want[s].Boxes), len(got[s].Boxes))
		}
		for k := range want[s].Boxes {
			if !sameBoxes(want[s].Boxes[k], got[s].Boxes[k]) {
				return fmt.Sprintf("%s, a box moved\npreview: %s\n    tab: %s", state, want[s].Boxes[k], got[s].Boxes[k])
			}
		}
	}
	return ""
}

// sameBoxes compares two probe lines: the same element or text, and
// every rectangle within half a pixel.
func sameBoxes(a, b string) bool {
	if a == b {
		return true
	}
	ia, ib := strings.LastIndex(a, " ["), strings.LastIndex(b, " [")
	if ia < 0 || ib < 0 || a[:ia] != b[:ib] {
		return false
	}
	var ra, rb [][]float64
	if json.Unmarshal([]byte(a[ia+1:]), &ra) != nil || json.Unmarshal([]byte(b[ib+1:]), &rb) != nil || len(ra) != len(rb) {
		return false
	}
	for i := range ra {
		for k := range ra[i] {
			if math.Abs(ra[i][k]-rb[i][k]) > 0.5 {
				return false
			}
		}
	}
	return true
}
