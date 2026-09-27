//go:build browser

package ui

// Browser drives for select.js's converged behaviour, ported from Tito
// Go's searchselect fences (internal/instance/ui_test.go). Each drive runs
// its steps inside one page script and reads the result back as JSON,
// because what is being fenced is timing inside a frame and the exact
// DOM writes a keystroke makes — things a step-per-round-trip drive
// would blur.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// afterFrame resolves once the next frame has been drawn and the task
// after it has run: where a keystroke's deferred search has landed.
const afterFrame = `new Promise((r) => requestAnimationFrame(() => setTimeout(() => r(0), 0)))`

func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// zones is a time-zone list long enough (over fifty rows) that a
// keystroke moving every row would show, with "Europe/Lo…" and "Dubl"
// among them.
var zones = []string{
	"Africa/Abidjan", "Africa/Accra", "Africa/Algiers", "Africa/Cairo", "Africa/Casablanca",
	"Africa/Johannesburg", "Africa/Lagos", "Africa/Nairobi", "Africa/Tunis", "America/Anchorage",
	"America/Bogota", "America/Buenos_Aires", "America/Caracas", "America/Chicago", "America/Denver",
	"America/Halifax", "America/Havana", "America/Lima", "America/Los_Angeles", "America/Mexico_City",
	"America/New_York", "America/Phoenix", "America/Santiago", "America/Sao_Paulo", "America/Toronto",
	"America/Vancouver", "Asia/Bangkok", "Asia/Dhaka", "Asia/Dubai", "Asia/Hong_Kong",
	"Asia/Jakarta", "Asia/Jerusalem", "Asia/Karachi", "Asia/Kathmandu", "Asia/Kolkata",
	"Asia/Manila", "Asia/Seoul", "Asia/Shanghai", "Asia/Singapore", "Asia/Tehran",
	"Asia/Tokyo", "Atlantic/Azores", "Atlantic/Reykjavik", "Australia/Adelaide", "Australia/Brisbane",
	"Australia/Perth", "Australia/Sydney", "Europe/Amsterdam", "Europe/Athens", "Europe/Berlin",
	"Europe/Brussels", "Europe/Bucharest", "Europe/Dublin", "Europe/Helsinki", "Europe/Istanbul",
	"Europe/Lisbon", "Europe/London", "Europe/Madrid", "Europe/Moscow", "Europe/Oslo",
	"Europe/Paris", "Europe/Prague", "Europe/Rome", "Europe/Stockholm", "Europe/Vienna",
	"Europe/Warsaw", "Europe/Zurich", "Pacific/Auckland", "Pacific/Fiji", "Pacific/Honolulu",
}

// flag is a country's regional-indicator pair, the glyph data-rst-lead
// carries.
func flag(iso string) string {
	var b strings.Builder
	for _, r := range iso {
		b.WriteRune(0x1F1E6 + (r - 'A'))
	}
	return b.String()
}

// convergedPage serves select.js, the stylesheets and one form holding
// the three selects the drives below open.
func convergedPage(t *testing.T) http.Handler {
	t.Helper()
	var body strings.Builder
	body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<title>select</title><link rel="stylesheet" href="/tokens.css">` +
		`<link rel="stylesheet" href="/theme.css">` +
		`<script defer src="/select.js"></script></head><body>` +
		`<form method="post" action="/submit">`)

	// A time zone, no blank: the keeps-pace drive.
	body.WriteString(`<label rst-field-label for="tz">Time zone</label>` +
		`<select rst-input id="tz" name="tz" data-rst-select>`)
	for _, z := range zones {
		fmt.Fprintf(&body, `<option value="%s">%s</option>`, z, z)
	}
	body.WriteString(`</select>`)

	// A REQUIRED country question: pinned countries above a divider, then
	// the rest, under a disabled "Choose one".
	pinned := []string{"AE", "SA", "GB", "IN"}
	byISO := map[string]countryRow{"SA": {"SA", "Saudi Arabia", "Saudi-Arabien", "966"}}
	for _, c := range countries {
		byISO[c.iso] = c
	}
	option := func(c countryRow) {
		fmt.Fprintf(&body, `<option value="%s" data-rst-lead="%s" data-rst-terms="%s +%s">%s</option>`,
			c.iso, flag(c.iso), c.iso, c.code, html.EscapeString(c.en))
	}
	body.WriteString(`<label rst-field-label for="country">Country</label>` +
		`<select rst-input id="country" name="country" required data-rst-select>` +
		`<option value="" disabled selected>Choose one</option>`)
	for _, iso := range pinned {
		option(byISO[iso])
	}
	body.WriteString(`<hr>`)
	for _, c := range countries {
		if c.iso != "GB" && c.iso != "AE" && c.iso != "IN" {
			option(c)
		}
	}
	body.WriteString(`</select>`)

	// A phone number's country: a compact box, a MARKED prompt.
	body.WriteString(`<select rst-input id="cc" name="cc" aria-label="Country" required data-rst-select data-rst-select-filter="Search">` +
		`<option data-rst-prompt value="" selected>Country</option>`)
	for _, c := range countries {
		first := ""
		if primary[c.iso] {
			first = " data-rst-first"
		}
		fmt.Fprintf(&body, `<option value="%s" data-rst-short="+%s" data-rst-lead="%s" data-rst-terms="%s +%s"%s>%s (+%s)</option>`,
			c.iso, c.code, flag(c.iso), c.iso, c.code, first, html.EscapeString(c.en), c.code)
	}
	body.WriteString(`</select><input type="tel" name="phone">`)

	// An optional select whose blank is a real answer — until something
	// makes the select required.
	body.WriteString(`<label rst-field-label for="size">Size</label>` +
		`<select rst-input id="size" name="size" data-rst-select>` +
		`<option value="" selected>None</option><option value="s">Small</option><option value="m">Medium</option><option value="l">Large</option>` +
		`</select>`)

	// A grouped select, for the extra rows a page adds under a search.
	body.WriteString(`<label rst-field-label for="dept">Department</label>` +
		`<select rst-input id="dept" name="dept" data-rst-select>` +
		`<optgroup label="Sales"><option value="acc">Accounts</option><option value="ret">Retail</option></optgroup>` +
		`<optgroup label="Support"><option value="asi">Assistance</option><option value="bil">Billing</option></optgroup>` +
		`</select>`)

	// A select the page replaces outright, and one whose options it
	// rebuilds, each in a host of its own.
	body.WriteString(`<label rst-field-label for="colour">Colour</label><div id="colour-host">` +
		`<select rst-input id="colour" name="colour" data-rst-select>` +
		`<option value="red">Red</option><option value="green">Green</option><option value="blue">Blue</option>` +
		`</select></div>`)
	body.WriteString(`<label rst-field-label for="flavour">Flavour</label><div id="flavour-host">` +
		`<select rst-input id="flavour" name="flavour" data-rst-select>` +
		`<option value="apple">Apple</option><option value="banana">Banana</option><option value="cherry">Cherry</option>` +
		`</select></div>`)

	body.WriteString(`<button type="submit" id="go">Save</button></form></body></html>`)
	page := body.String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /select.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(SelectJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, _ := ThemeCSS(ThemeNames()[0])
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	return mux
}

// drive opens the page and runs one page script, decoding its result.
func drive(t *testing.T, script string, out any) {
	t.Helper()
	mux := convergedPage(t)
	rig := harness.New(t, func(string) http.Handler { return mux })
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx,
		// A page in a background tab gets no focus events from focus(),
		// so a handler that reacts to focus could not be observed at all.
		emulation.SetFocusEmulationEnabled(true),
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`#tz-combo`, chromedp.ByQuery),
		chromedp.Evaluate(`(async () => JSON.stringify(await (async () => {`+script+`})()))()`, &raw, awaitPromise),
	); err != nil {
		t.Fatalf("drive failed: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
}

// A burst of typing is searched once at the time and once in the frame,
// not once per input event; Enter typed in the same task acts on the
// words typed; a keystroke moves only the rows it shows and writes a
// row's hidden state only when it changes; and clearing puts every row
// back in its own place.
func TestSelectKeepsPaceWithTyping(t *testing.T) {
	t.Parallel()
	var got struct {
		SyncSearches  int      `json:"syncSearches"`
		FrameSearches int      `json:"frameSearches"`
		Picked        string   `json:"picked"`
		Rows          int      `json:"rows"`
		Moves         int      `json:"moves"`
		HiddenMoved   []string `json:"hiddenMoved"`
		NoopWrites    []string `json:"noopWrites"`
		OutOfPlace    []string `json:"outOfPlace"`
		Shown         int      `json:"shown"`
	}
	drive(t, `
		const input = document.querySelector('#tz-combo');
		const select = document.querySelector('#tz');
		const list = document.querySelector('#tz-listbox');
		const frame = () => `+afterFrame+`;
		const type = (v) => { input.value = v; input.dispatchEvent(new Event('input', { bubbles: true })); };
		const out = { hiddenMoved: [], noopWrites: [], outOfPlace: [], moves: 0 };
		input.focus();
		input.click();
		await frame();
		const seen = [];
		const mo = new MutationObserver((m) => seen.push(...m));
		mo.observe(list, { childList: true, subtree: true, attributes: true, attributeFilter: ['style'], attributeOldValue: true });

		let searched = 0;
		const searches = new MutationObserver((m) => { searched += m.length; });
		searches.observe(input, { attributes: true, attributeFilter: ['aria-activedescendant'] });
		for (const v of ['D', 'Du', 'Dub', 'Dubl']) type(v);
		out.syncSearches = searches.takeRecords().length;
		await frame();
		out.frameSearches = searched + searches.takeRecords().length;
		searches.disconnect();
		mo.takeRecords();

		type('Dubl');
		type('Lond');
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		out.picked = select.value;
		await frame();
		mo.takeRecords();
		seen.length = 0;

		input.click();
		await frame();
		mo.takeRecords();
		seen.length = 0;
		const word = 'Europe/Lo';
		for (let i = 1; i <= word.length; i++) {
			type(word.slice(0, i));
			await frame();
			const recs = [...seen, ...mo.takeRecords()];
			seen.length = 0;
			const writes = new Map();
			for (const r of recs) {
				if (r.type === 'childList') {
					for (const n of r.addedNodes) {
						out.moves++;
						if (n.style.display === 'none') out.hiddenMoved.push(word.slice(0, i) + ': ' + (n.id || n.tagName));
					}
				} else {
					if (!writes.has(r.target)) writes.set(r.target, []);
					writes.get(r.target).push((r.oldValue || '').includes('none'));
				}
			}
			for (const [el, olds] of writes) {
				olds.forEach((was, j) => {
					const now = j + 1 < olds.length ? olds[j + 1] : el.style.display === 'none';
					if (was === now) out.noopWrites.push(word.slice(0, i) + ': ' + (el.id || el.tagName));
				});
			}
		}
		mo.disconnect();
		out.shown = list.querySelectorAll('[rst-combo-option]:not([style*="none"])').length;

		type('');
		await frame();
		const rows = [...list.querySelectorAll('[rst-combo-option]')];
		out.rows = rows.length;
		rows.forEach((li, i) => {
			if (li.id !== 'tz-listbox-' + i) out.outOfPlace.push(i + ': ' + li.id);
		});
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
		return out;
	`, &got)
	if got.SyncSearches != 1 || got.FrameSearches != 1 {
		t.Errorf("four input events in one task were searched %d times at once and %d more in the frame, want 1 and 1", got.SyncSearches, got.FrameSearches)
	}
	if got.Picked != "Europe/London" {
		t.Errorf("typing \"Dubl\" then \"Lond\" and pressing Enter in the same task picked %q, want Europe/London: Enter acted on a list that had not caught up with the words", got.Picked)
	}
	if len(got.HiddenMoved) > 0 {
		t.Errorf("a keystroke moved %d rows it hides (first: %v): only the rows a search shows should move", len(got.HiddenMoved), got.HiddenMoved[0])
	}
	if len(got.NoopWrites) > 0 {
		t.Errorf("%d hidden-state writes set a row to what it already was (first: %v)", len(got.NoopWrites), got.NoopWrites[0])
	}
	// The fence above is only a fence if the observer saw rows move at all.
	if got.Shown == 0 || got.Moves == 0 {
		t.Errorf("typing \"Europe/Lo\" showed %d rows and moved %d: the observer saw no search happen", got.Shown, got.Moves)
	}
	if got.Rows < 50 || len(got.OutOfPlace) > 0 {
		t.Errorf("after clearing the search, %d of %d rows are out of their own place (first: %v)", len(got.OutOfPlace), got.Rows, got.OutOfPlace)
	}
}

// A required country question with pinned countries above a divider:
// clearing a search puts a lifted pinned country back ABOVE the divider
// and the divider shows again; the required blank is never a row, even
// for words that name it; words typed and cleared on the unanswered
// question leave nothing highlighted, so Enter cannot answer it; and a
// pick closes the list without leaving the next search waiting on a frame.
func TestSelectKeepsItsDividerAndPrompt(t *testing.T) {
	t.Parallel()
	var got struct {
		Sep           int      `json:"sep"`
		Restored      bool     `json:"restored"`
		Order         []string `json:"order"`
		SepSearching  bool     `json:"sepSearching"`
		SepAfter      bool     `json:"sepAfter"`
		PromptIdle    bool     `json:"promptIdle"`
		PromptTyped   bool     `json:"promptTyped"`
		ClearedActive string   `json:"clearedActive"`
		ClearedEnter  string   `json:"clearedEnter"`
		ClearedOpen   bool     `json:"clearedOpen"`
		Picked        string   `json:"picked"`
		ActiveAfter   string   `json:"activeAfter"`
		TopAfter      string   `json:"topAfter"`
	}
	drive(t, `
		const input = document.querySelector('#country-combo');
		const list = document.querySelector('#country-listbox');
		const select = document.querySelector('#country');
		const frame = () => `+afterFrame+`;
		const type = (v) => { input.value = v; input.dispatchEvent(new Event('input', { bubbles: true })); };
		const seq = () => [...list.children].map((c) => c.id || c.tagName + (c.hasAttribute('rst-combo-sep') ? ':sep' : ''));
		const blank = document.querySelector('#country-listbox-0');
		const sep = list.querySelector('[rst-combo-sep]');
		const out = { sep: list.querySelectorAll('[rst-combo-sep]').length };
		input.focus();
		input.click();
		await frame();
		const s0 = seq();
		out.promptIdle = blank.style.display !== 'none';
		for (const q of ['a', 'i', 'united k']) {
			type(q);
			await frame();
			if (q === 'a') out.sepSearching = sep.style.display !== 'none';
		}
		type('');
		await frame();
		out.order = seq();
		out.restored = out.order.join() === s0.join();
		out.sepAfter = sep.style.display !== 'none';
		type('choose');
		await frame();
		out.promptTyped = blank.style.display !== 'none';
		type('ger');
		await frame();
		type('');
		await frame();
		out.clearedActive = input.getAttribute('aria-activedescendant') || '';
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		out.clearedEnter = select.value;
		out.clearedOpen = input.getAttribute('aria-expanded') === 'true';

		type('irel');
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		out.picked = select.value;
		type('germ');
		const act = input.getAttribute('aria-activedescendant');
		out.activeAfter = act ? select.options[Number(act.split('-listbox-')[1])].value : '';
		const top = list.querySelector('[rst-combo-option]:not([style*="none"])');
		out.topAfter = top ? select.options[Number(top.id.split('-listbox-')[1])].value : '';
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
		return out;
	`, &got)
	if got.Sep != 1 {
		t.Fatalf("the country list drew %d dividers, want 1: the fence below needs one", got.Sep)
	}
	if !got.Restored {
		head := got.Order
		if len(head) > 8 {
			head = head[:8]
		}
		t.Errorf("after searching \"a\", \"i\" and \"united k\" and clearing, the list is not in its own order: it starts %v", head)
	}
	if got.SepSearching || !got.SepAfter {
		t.Errorf("the divider showed %v while searching and %v after, want false then true", got.SepSearching, got.SepAfter)
	}
	if got.PromptIdle || got.PromptTyped {
		t.Errorf("the required blank was a row with nothing typed (%v) or for \"choose\" (%v): it is a prompt, never an answer", got.PromptIdle, got.PromptTyped)
	}
	if got.ClearedActive != "" || got.ClearedEnter != "" || !got.ClearedOpen {
		t.Errorf("typing \"ger\" and clearing it on the unanswered question left %q highlighted, and Enter picked %q (list open: %v): a cleared search must highlight nothing, as opening does", got.ClearedActive, got.ClearedEnter, got.ClearedOpen)
	}
	if got.Picked != "IE" {
		t.Errorf("\"irel\" then Enter picked %q, want IE", got.Picked)
	}
	if got.ActiveAfter != "DE" || got.TopAfter != "DE" {
		t.Errorf("typing \"germ\" in the frame after a pick highlighted %q with %q on top, want DE and DE: the search waited on the frame the earlier typing armed", got.ActiveAfter, got.TopAfter)
	}
}

// A phone number's country: a MARKED prompt, "Country", which is a row
// while nothing is typed. Unanswered, it highlights nothing — on opening,
// and when a search is cleared — and the box shows only its placeholder,
// so Enter picks nothing and the list stays open. The prompt is never a
// stop for the arrows, Home or End, and never aria-selected. Answered,
// the box reopens on its pick and a cleared search comes back to it.
func TestSelectPromptClearsToItsPlaceholder(t *testing.T) {
	t.Parallel()
	var got struct {
		PromptRow             bool   `json:"promptRow"`
		OpenActive            string `json:"openActive"`
		ClearedActive         string `json:"clearedActive"`
		ClearedValue          string `json:"clearedValue"`
		Placeholder           string `json:"placeholder"`
		ClearedEnter          string `json:"clearedEnter"`
		ClearedOpen           bool   `json:"clearedOpen"`
		Picked                string `json:"picked"`
		Shown                 string `json:"shown"`
		PromptSelected        string `json:"promptSelected"`
		DownFromNothing       string `json:"downFromNothing"`
		UpFromFirst           string `json:"upFromFirst"`
		End                   string `json:"end"`
		Home                  string `json:"home"`
		GBRow                 string `json:"gbRow"`
		GBSelected            string `json:"gbSelected"`
		ReopenActive          string `json:"reopenActive"`
		SearchActive          string `json:"searchActive"`
		AnsweredClearedActive string `json:"answeredClearedActive"`
	}
	drive(t, `
		const input = document.querySelector('#cc-combo');
		const select = document.querySelector('#cc');
		const frame = () => `+afterFrame+`;
		const type = (v) => { input.value = v; input.dispatchEvent(new Event('input', { bubbles: true })); };
		const key = (k) => input.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
		const act = () => input.getAttribute('aria-activedescendant') || '';
		const out = {};
		input.focus();
		input.click();
		await frame();
		const prompt = document.querySelector('#cc-listbox-0');
		out.promptRow = prompt.style.display !== 'none';
		out.openActive = act();
		out.promptSelected = prompt.getAttribute('aria-selected');
		key('ArrowDown');
		out.downFromNothing = act();
		key('ArrowUp');
		out.upFromFirst = act();
		key('End');
		out.end = act();
		key('Home');
		out.home = act();
		type('44');
		await frame();
		type('');
		await frame();
		out.clearedActive = act();
		out.clearedValue = input.value;
		out.placeholder = input.placeholder;
		key('Enter');
		out.clearedEnter = select.value;
		out.clearedOpen = input.getAttribute('aria-expanded') === 'true';
		type('+44');
		key('Enter');
		out.picked = select.value;
		out.shown = input.value;
		out.gbRow = 'cc-listbox-' + select.selectedIndex;
		out.gbSelected = document.querySelector('#' + out.gbRow).getAttribute('aria-selected');
		input.blur();
		input.focus();
		input.click();
		await frame();
		out.reopenActive = act();
		type('ger');
		await frame();
		out.searchActive = act();
		type('');
		await frame();
		out.answeredClearedActive = act();
		key('Escape');
		return out;
	`, &got)
	if !got.PromptRow {
		t.Error("the \"Country\" prompt is not a row with nothing typed")
	}
	if got.OpenActive != "" {
		t.Errorf("the unanswered box opened with %q highlighted, want nothing", got.OpenActive)
	}
	if got.ClearedActive != "" || got.ClearedEnter != "" || !got.ClearedOpen {
		t.Errorf("typing \"44\" and clearing it left %q highlighted, and Enter picked %q (list open: %v): a cleared search highlights nothing", got.ClearedActive, got.ClearedEnter, got.ClearedOpen)
	}
	if got.ClearedValue != "" || got.Placeholder != "Country" {
		t.Errorf("the cleared box reads %q under placeholder %q, want empty under \"Country\"", got.ClearedValue, got.Placeholder)
	}
	if got.Picked != "GB" || got.Shown != "+44" {
		t.Errorf("\"+44\" then Enter picked %q showing %q, want GB showing +44", got.Picked, got.Shown)
	}
	// The prompt is never a stop for the keys, and never "selected".
	const first = "cc-listbox-1"
	last := fmt.Sprintf("cc-listbox-%d", len(countries))
	if got.DownFromNothing != first {
		t.Errorf("ArrowDown from nothing highlighted %q, want the first country %s: the prompt is not a stop", got.DownFromNothing, first)
	}
	if got.UpFromFirst != first || got.Home != first {
		t.Errorf("ArrowUp from the first country went to %q and Home to %q, want both %s: the keys never land on the prompt", got.UpFromFirst, got.Home, first)
	}
	if got.End != last {
		t.Errorf("End highlighted %q, want the last country %s", got.End, last)
	}
	if got.PromptSelected != "false" {
		t.Errorf("the prompt reads aria-selected=%q on an unanswered box, want false: a prompt is never selected", got.PromptSelected)
	}
	if got.GBSelected != "true" {
		t.Errorf("after picking GB its row reads aria-selected=%q, want true", got.GBSelected)
	}
	// Opening lands where opening() says: the pick, on an answered box.
	if got.ReopenActive != got.GBRow {
		t.Errorf("the answered box reopened on %q, want its pick %s", got.ReopenActive, got.GBRow)
	}
	if got.SearchActive == "" || got.SearchActive == got.GBRow || got.AnsweredClearedActive != got.GBRow {
		t.Errorf("on the answered box, \"ger\" highlighted %q and clearing it %q, want another row and then the pick %s", got.SearchActive, got.AnsweredClearedActive, got.GBRow)
	}
}

// A select that becomes required (or stops) re-derives its prompts: an
// optional select's "None" is a real answer — shown, listed, selected —
// and once the select is required the same blank is a prompt, so the box
// empties and no row is selected. And a highlighted row a page takes
// away (its extra suggestions replaced) is never left as the highlight.
func TestSelectFollowsRequiredAndExtras(t *testing.T) {
	t.Parallel()
	var got struct {
		OptionalShown    string `json:"optionalShown"`
		OptionalSelected string `json:"optionalSelected"`
		RequiredShown    string `json:"requiredShown"`
		RequiredSelected string `json:"requiredSelected"`
		BackShown        string `json:"backShown"`
		ExtraActive      string `json:"extraActive"`
		AfterRemove      string `json:"afterRemove"`
		AfterRemoveInDOM bool   `json:"afterRemoveInDOM"`
		BorrowedCleared  bool   `json:"borrowedCleared"`
		AfterRemoval     string `json:"afterRemovalValue"`
		AfterCancel      string `json:"afterCancelValue"`
		Requery          string `json:"requeryValue"`
		RemovedShown     string `json:"removedSelectedShown"`
		RemovedValue     string `json:"removedSelectedValue"`
		NoneActive       string `json:"noneActive"`
		NoneIndex        int    `json:"noneIndex"`
		BorrowedDisabled bool   `json:"borrowedClearedDisabled"`
		AriaRequired     string `json:"ariaRequired"`
		OpenFlipListed   bool   `json:"openFlipListed"`
		OpenFlipActive   string `json:"openFlipActive"`
		OpenFlipShown    int    `json:"openFlipShown"`
		OpenFlipText     string `json:"openFlipText"`
		IdleExtrasShown  int    `json:"idleExtrasShown"`
		ScreenOrder      string `json:"screenOrder"`
		KeyOrder         string `json:"keyOrder"`
	}
	drive(t, `
		let input = document.querySelector('#size-combo');
		// Changing the options rebuilds the box, so the drive re-reads it.
		const again = async () => { await frame(); input = document.querySelector('#size-combo'); };
		const select = document.querySelector('#size');
		const blank = document.querySelector('#size-listbox-0');
		const frame = () => `+afterFrame+`;
		const key = (k) => input.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
		const out = {};
		out.optionalShown = input.value;
		// Read with the list open, which is when rows are marked.
		input.focus();
		input.click();
		await frame();
		out.optionalSelected = blank.getAttribute('aria-selected');
		key('Escape');
		select.required = true;
		await frame();
		out.requiredShown = input.value;
		out.requiredSelected = blank.getAttribute('aria-selected');
		out.ariaRequired = input.getAttribute('aria-required');
		select.required = false;
		await frame();
		// And with the list OPEN when it flips: the blank leaves the list
		// and is not left highlighted.
		input.focus();
		input.click();
		await frame();
		select.required = true;
		await frame();
		out.openFlipListed = blank.style.display !== 'none';
		out.openFlipActive = input.getAttribute('aria-activedescendant') || '';
		// Idle (nothing typed), the refresh is not a search for the pick's
		// own text: every real choice stays, and the box drops "None".
		out.openFlipShown = document.querySelectorAll('#size-listbox [rst-combo-option]:not([style*="none"])').length;
		out.openFlipText = input.value;
		key('Escape');
		select.required = false;
		await frame();
		// And extras handed over while the box shows the pick hide nothing.
		input.focus();
		input.click();
		await frame();
		input.closest('[rst-combo]').rstExtras([{ label: 'Something else', run: () => {} }]);
		out.idleExtrasShown = document.querySelectorAll('#size-listbox [rst-combo-option]:not([style*="none"])').length;
		input.closest('[rst-combo]').rstExtras([]);
		key('Escape');
		select.required = false;
		await frame();
		out.backShown = input.value;

		// Extras: arrow onto one, then have the page take them all away.
		const wrap = input.closest('[rst-combo]');
		input.focus();
		input.click();
		await frame();
		wrap.rstExtras([{ label: 'Something else', run: () => {} }]);
		key('End');
		out.extraActive = input.getAttribute('aria-activedescendant') || '';
		wrap.rstExtras([]);
		const id = input.getAttribute('aria-activedescendant') || '';
		out.afterRemove = id;
		out.afterRemoveInDOM = id === '' || !!document.getElementById(id);
		key('Escape');

		// An option the app removes after enhancement moves every index:
		// the row chosen is still the option submitted.
		const small = select.querySelector('option[value="s"]');
		small.remove();
		await again();
		input.focus();
		input.click();
		await frame();
		const medium = [...document.querySelectorAll('#size-listbox [rst-combo-option]')].find((li) => li.textContent === 'Medium');
		medium.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
		out.afterRemovalValue = select.value;
		select.insertBefore(small, select.querySelector('option[value="m"]'));
		await again();
		select.value = '';
		select.dispatchEvent(new Event('change', { bubbles: true }));

		// The SELECTED option removed: the next pick is what the box shows.
		select.value = 's';
		select.dispatchEvent(new Event('change', { bubbles: true }));
		const small2 = select.querySelector('option[value="s"]');
		small2.remove();
		await again();
		input.focus();
		input.click();
		await frame();
		const large = [...document.querySelectorAll('#size-listbox [rst-combo-option]')].find((li) => li.textContent === 'Large');
		large.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
		out.removedSelectedShown = input.value;
		out.removedSelectedValue = select.value;
		select.insertBefore(small2, select.querySelector('option[value="m"]'));
		await again();

		// Nothing selected at all is unanswered: nothing is highlighted, and
		// a bare Enter answers nothing.
		select.selectedIndex = -1;
		input.focus();
		input.click();
		await frame();
		out.noneActive = input.getAttribute('aria-activedescendant') || '';
		key('Enter');
		out.noneIndex = select.selectedIndex;
		key('Escape');
		select.value = '';
		select.dispatchEvent(new Event('change', { bubbles: true }));

		// A cancelled search takes its steering with it: type, arrow,
		// Escape, type the same again, leave — nothing is committed.
		input.focus();
		input.click();
		await frame();
		const typeSize = (v) => { input.value = v; input.dispatchEvent(new Event('input', { bubbles: true })); };
		typeSize('m');
		await frame();
		key('ArrowDown');
		key('Escape');
		typeSize('m');
		await frame();
		key('Tab');
		out.afterCancelValue = select.value;
		key('Escape');

		// And the same words after a cancelled search are a NEW search: it
		// re-ranks, so Enter takes its best match, not the pick the old one
		// left highlighted. Large is picked; "a" ranks Small first.
		select.value = 'l';
		select.dispatchEvent(new Event('change', { bubbles: true }));
		input.focus();
		input.click();
		await frame();
		typeSize('a');
		await frame();
		key('Escape');
		typeSize('a');
		await frame();
		key('Enter');
		out.requeryValue = select.value;
		select.value = '';
		select.dispatchEvent(new Event('change', { bubbles: true }));

		// A borrowed "please pick one" goes the moment the select stops
		// objecting, even while the box keeps focus.
		select.required = true;
		select.value = '';
		select.dispatchEvent(new Event('change', { bubbles: true }));
		select.required = true;
		input.setCustomValidity(select.validationMessage || 'Please pick one');
		select.required = false;
		await frame();
		out.borrowedCleared = input.validity.valid;
		// And when it is disabled instead: a disabled select is never
		// validated, whatever its validity says.
		select.required = true;
		input.setCustomValidity(select.validationMessage || 'Please pick one');
		select.disabled = true;
		await frame();
		out.borrowedClearedDisabled = input.validity.valid;
		select.disabled = false;
		select.required = false;
		await frame();

		// Grouped hits under a search sit above the page's extra rows, in
		// the order the keys walk them.
		const dInput = document.querySelector('#dept-combo');
		const dWrap = dInput.closest('[rst-combo]');
		const dList = document.querySelector('#dept-listbox');
		dInput.focus();
		dInput.click();
		await frame();
		dWrap.rstExtras([{ label: 'Other', run: () => {} }]);
		dInput.value = 'a';
		dInput.dispatchEvent(new Event('input', { bubbles: true }));
		await frame();
		out.screenOrder = [...dList.querySelectorAll('[rst-combo-option]:not([style*="none"])')].map((li) => li.textContent).join('|');
		const walked = [];
		const dkey = (k) => dInput.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
		dkey('Home');
		for (let i = 0; i < 6; i++) {
			const a = dInput.getAttribute('aria-activedescendant');
			const t = a ? document.getElementById(a).textContent : '';
			if (!walked.includes(t)) walked.push(t);
			dkey('ArrowDown');
		}
		out.keyOrder = walked.join('|');
		dkey('Escape');
		return out;
	`, &got)
	if got.OptionalShown != "None" || got.OptionalSelected != "true" {
		t.Errorf("an optional select's blank shows %q and reads aria-selected=%q, want \"None\" and true: it is a real answer", got.OptionalShown, got.OptionalSelected)
	}
	if got.RequiredShown != "" || got.RequiredSelected != "false" {
		t.Errorf("made required, the blank shows %q and reads aria-selected=%q, want empty and false: the observer did not re-derive the prompt", got.RequiredShown, got.RequiredSelected)
	}
	if !got.BorrowedDisabled {
		t.Error("the box kept the select's borrowed message after the select was disabled: a disabled field would still block the form")
	}
	if got.AriaRequired != "true" {
		t.Errorf("the box of a required select reads aria-required=%q, want \"true\": an ARIA boolean is a string", got.AriaRequired)
	}
	if got.OpenFlipListed || got.OpenFlipActive == "size-listbox-0" {
		t.Errorf("made required with the list open, the blank is still listed (%v) or highlighted (%q): the open list did not re-read its prompts", got.OpenFlipListed, got.OpenFlipActive)
	}
	if got.OpenFlipShown != 3 || got.OpenFlipText != "" {
		t.Errorf("made required with the list open and nothing typed, %d rows show and the box reads %q, want the 3 real choices and an empty box: the refresh searched for the old pick's text", got.OpenFlipShown, got.OpenFlipText)
	}
	if got.IdleExtrasShown != 5 {
		t.Errorf("extras handed to an idle open list left %d rows showing, want 5 (the 4 options and the extra)", got.IdleExtrasShown)
	}
	if got.BackShown != "None" {
		t.Errorf("made optional again, the box shows %q, want \"None\"", got.BackShown)
	}
	if !strings.Contains(got.ExtraActive, "-extra-") {
		t.Fatalf("End did not reach the extra row (highlighted %q): the fence below needs it", got.ExtraActive)
	}
	if !got.AfterRemoveInDOM {
		t.Errorf("after the page took its extra rows away the highlight still names %q, a row that is gone", got.AfterRemove)
	}
	if got.AfterRemoval != "m" {
		t.Errorf("after the app removed an option, choosing the Medium row submitted %q, want m: a cached index picked another option", got.AfterRemoval)
	}
	if got.RemovedShown != "Large" || got.RemovedValue != "l" {
		t.Errorf("after the selected option was removed, choosing Large shows %q and submits %q, want Large and l: a detached option still read as selected", got.RemovedShown, got.RemovedValue)
	}
	if got.NoneActive != "" || got.NoneIndex != -1 {
		t.Errorf("with nothing selected the list opened on %q and Enter left selectedIndex %d, want nothing highlighted and -1: no selection is unanswered", got.NoneActive, got.NoneIndex)
	}
	if got.Requery != "s" {
		t.Errorf("\"a\" typed again after a cancelled \"a\" and Enter picked %q, want s (Small, its best match): the cancelled search's query survived, so it did not re-rank", got.Requery)
	}
	if got.AfterCancel != "" {
		t.Errorf("a search typed, arrowed, cancelled and typed again committed %q on leaving, want nothing: the cancelled search's steering survived", got.AfterCancel)
	}
	if !got.BorrowedCleared {
		t.Error("the box kept the select's borrowed message after the select stopped being required: the form would still refuse to submit")
	}
	if got.ScreenOrder == "" || got.ScreenOrder != got.KeyOrder || !strings.HasSuffix(got.ScreenOrder, "|Other") {
		t.Errorf("a grouped search shows %q but the keys walk %q: the matches must sit above the page's extra row, in the keys' order", got.ScreenOrder, got.KeyOrder)
	}
}

// A page may replace a whole <select> (a question flow swapping its
// answer control). The box that spoke for the old one steps aside and the
// new one is enhanced afresh: before, Tito Go's box kept driving the
// detached select, showing "Green" while the form posted "Blue".
func TestSelectStepsAsideWhenReplaced(t *testing.T) {
	t.Parallel()
	var got struct {
		Boxes        int    `json:"boxes"`
		Shown        string `json:"shown"`
		LabelFor     string `json:"labelFor"`
		PostedNow    string `json:"postedNow"`
		Picked       string `json:"picked"`
		PostedPick   string `json:"postedPick"`
		SameBoxes    int    `json:"sameTaskBoxes"`
		SameShown    string `json:"sameTaskShown"`
		SameErrors   int    `json:"sameTaskErrors"`
		BothLabelled bool   `json:"bothLabelled"`
		CloneBoxes   int    `json:"cloneBoxes"`
	}
	drive(t, `
		const frame = () => `+afterFrame+`;
		const form = document.querySelector('form');
		const posted = () => new FormData(form).get('colour');
		// Pick Green through the box, then the page swaps the select for
		// one that holds Blue.
		let input = document.querySelector('#colour-combo');
		input.focus();
		input.value = 'green';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		input.blur();
		const fresh = document.createElement('select');
		fresh.id = 'colour';
		fresh.name = 'colour';
		fresh.setAttribute('rst-input', '');
		fresh.setAttribute('data-rst-select', '');
		fresh.innerHTML = '<option value="red">Red</option><option value="green">Green</option><option value="blue" selected>Blue</option>';
		document.querySelector('#colour').replaceWith(fresh);
		await frame();
		const out = {};
		out.boxes = document.querySelectorAll('#colour-host [rst-combo]').length;
		input = document.querySelector('#colour-combo');
		out.shown = input ? input.value : '';
		out.labelFor = document.querySelector('label[for="colour-combo"]') ? 'colour-combo' : '';
		out.postedNow = posted();
		input.focus();
		input.value = 'red';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		out.picked = input.value;
		out.postedPick = posted();
		// Two selects replaced in one task: each new box is named by its label.
		const swapIn = (id, opts) => {
			const n = document.createElement('select');
			n.id = id;
			n.name = id;
			n.setAttribute('data-rst-select', '');
			n.innerHTML = opts;
			document.querySelector('#' + id).replaceWith(n);
		};
		swapIn('colour', '<option value="red">Red</option><option value="blue">Blue</option>');
		swapIn('flavour', '<option value="fig">Fig</option><option value="lime">Lime</option>');
		await frame();
		out.bothLabelled = !!document.querySelector('label[for="colour-combo"]') && !!document.querySelector('label[for="flavour-combo"]');
		// Changed and replaced in the same task: a handover, and no error.
		let errors = 0;
		window.addEventListener('error', () => errors++);
		const again = document.createElement('select');
		again.id = 'colour';
		again.name = 'colour';
		again.setAttribute('data-rst-select', '');
		again.innerHTML = '<option value="teal" selected>Teal</option><option value="navy">Navy</option>';
		const current = document.querySelector('#colour');
		current.innerHTML = '';
		current.replaceWith(again);
		await frame();
		out.sameTaskBoxes = document.querySelectorAll('#colour-host [rst-combo]').length;
		out.sameTaskShown = (document.querySelector('#colour-combo') || {}).value || '';
		// Replaced with a clone of itself, which copies the enhancement
		// marker: the clone is still enhanced.
		const orig = document.querySelector('#colour');
		orig.replaceWith(orig.cloneNode(true));
		await frame();
		out.cloneBoxes = document.querySelectorAll('#colour-host [rst-combo]').length;
		// Options changed and the whole host cleared in one task: nothing
		// is left to enhance, and nothing throws.
		const last = document.querySelector('#colour');
		last.append(new Option('Grey', 'grey'));
		document.querySelector('#colour-host').innerHTML = '';
		await frame();
		out.sameTaskErrors = errors;
		return out;
	`, &got)
	if got.Boxes != 1 {
		t.Errorf("after the select was replaced its host holds %d boxes, want 1: the old box did not step aside, or the new select was not enhanced", got.Boxes)
	}
	if got.Shown != "Blue" || got.PostedNow != "blue" {
		t.Errorf("the box shows %q while the form posts %q, want Blue and blue: the box speaks for a select no longer in the form", got.Shown, got.PostedNow)
	}
	if got.LabelFor != "colour-combo" {
		t.Error("the label does not name the new box")
	}
	if got.CloneBoxes != 1 {
		t.Errorf("a select replaced with its own clone left %d boxes, want 1: the clone's copied marker was trusted", got.CloneBoxes)
	}
	if !got.BothLabelled {
		t.Error("two selects replaced in one task: a new box is left without its label, which an old box handed back to the hidden select")
	}
	if got.SameBoxes != 1 || got.SameShown != "Teal" || got.SameErrors != 0 {
		t.Errorf("options changed and select replaced in one task: %d boxes showing %q, %d errors; want 1 box showing Teal and none", got.SameBoxes, got.SameShown, got.SameErrors)
	}
	if got.Picked != "Red" || got.PostedPick != "red" {
		t.Errorf("picking Red on the new box shows %q and posts %q, want Red and red", got.Picked, got.PostedPick)
	}
}

// A page may rebuild a select's options while somebody is searching it.
// The box is rebuilt from what the select now holds — every row an option
// it really has — and the person keeps their focus and their words.
func TestSelectRebuildsWhenItsOptionsChange(t *testing.T) {
	t.Parallel()
	var got struct {
		Boxes         int    `json:"boxes"`
		Focused       bool   `json:"focused"`
		Typed         string `json:"typed"`
		Rows          string `json:"rows"`
		Picked        string `json:"picked"`
		BoxesAfterTwo int    `json:"boxesAfterTwo"`
		StillHidden   bool   `json:"stillHidden"`
		LiveListeners int    `json:"liveListeners"`
		Recorded      int    `json:"recorded"`
		ExtraKept     bool   `json:"extraKept"`
		RebuildInputs int    `json:"rebuildInputs"`
		StayedClosed  bool   `json:"stayedClosed"`
		KeptFocus     bool   `json:"keptFocus"`
		Caret         int    `json:"caret"`
		StillOn       string `json:"stillOn"`
	}
	drive(t, `
		const frame = () => `+afterFrame+`;
		const select = document.querySelector('#flavour');
		// Record every listener a box puts on the select from here on, so a
		// retired box's can be counted.
		const listeners = [];
		const add = select.addEventListener;
		select.addEventListener = function (type, fn, opt) {
			listeners.push({ type, signal: opt && opt.signal });
			return add.call(this, type, fn, opt);
		};
		let input = document.querySelector('#flavour-combo');
		input.focus();
		input.value = 'bl';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		await frame();
		// The caret mid-word, as when somebody goes back to correct a letter.
		input.setSelectionRange(1, 1);
		input.closest('[rst-combo]').rstExtras([{ label: 'Other', run: () => {} }]);
		// A rebuild restores the search quietly: an input event would let a
		// page that refreshes options on input loop forever.
		let inputs = 0;
		document.addEventListener('input', () => inputs++, true);
		select.innerHTML = '<option value="blueberry">Blueberry</option><option value="blackberry">Blackberry</option><option value="kiwi">Kiwi</option>';
		await frame();
		const out = {};
		out.boxes = document.querySelectorAll('#flavour-host [rst-combo]').length;
		input = document.querySelector('#flavour-combo');
		out.focused = document.activeElement === input;
		out.typed = input.value;
		out.caret = input.selectionStart;
		out.rows = [...document.querySelectorAll('#flavour-listbox [rst-combo-option]:not([style*="none"])')].map((li) => li.textContent).join('|');
		out.extraKept = [...document.querySelectorAll('#flavour-listbox [rst-combo-option]')].some((li) => li.textContent === 'Other');
		out.rebuildInputs = inputs;
		input.closest('[rst-combo]').rstExtras([]);
		// Arrow to the second row, then the page changes the options again:
		// the row somebody is on is still the one Enter takes.
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
		select.append(new Option('Bilberry', 'bilberry'));
		await frame();
		input = document.querySelector('#flavour-combo');
		const on = input.getAttribute('aria-activedescendant');
		out.stillOn = on ? document.getElementById(on).textContent : '';
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
		out.picked = select.value;
		// A second change: only the current box may answer it. An old box
		// still listening would step aside again and hand the select back
		// half-restored, visible and unenhanced.
		// Enter closed the list and focus stayed: a rebuild now must not
		// reopen it (and take the next Enter from the form).
		select.append(new Option('Lime', 'lime'));
		await frame();
		out.stayedClosed = document.querySelector('#flavour-combo').getAttribute('aria-expanded') === 'false';
		out.keptFocus = document.activeElement === document.querySelector('#flavour-combo');
		out.boxesAfterTwo = document.querySelectorAll('#flavour-host [rst-combo]').length;
		out.stillHidden = select.classList.contains('rst-sr-only') && select.dataset.rstEnhanced === 'true';
		out.liveListeners = listeners.filter((l) => !(l.signal && l.signal.aborted)).length;
		out.recorded = listeners.length;
		return out;
	`, &got)
	if got.Boxes != 1 {
		t.Errorf("after the options were rebuilt the host holds %d boxes, want 1", got.Boxes)
	}
	if got.Recorded < 9 || got.LiveListeners != 3 {
		t.Errorf("after three rebuilds %d of the %d listeners boxes put on the select are live, want only the current box's 3: a retired box still answers the select's events", got.LiveListeners, got.Recorded)
	}
	if got.Caret != 1 {
		t.Errorf("after the rebuild the caret is at %d, want 1: it jumped to the end of the words being corrected", got.Caret)
	}
	if got.StillOn != "Blackberry" {
		t.Errorf("after arrowing to Blackberry and a rebuild, %q is highlighted, want Blackberry: Enter would take another row", got.StillOn)
	}
	if got.BoxesAfterTwo != 1 || !got.StillHidden {
		t.Errorf("after a second change the host holds %d boxes and the select is enhanced-and-hidden %v, want 1 and true: an earlier box was still listening", got.BoxesAfterTwo, got.StillHidden)
	}
	if !got.Focused || got.Typed != "bl" {
		t.Errorf("after the rebuild the box has focus %v and reads %q, want true and \"bl\": the person lost their place", got.Focused, got.Typed)
	}
	if got.RebuildInputs != 0 {
		t.Errorf("rebuilding fired %d input events, want 0: a page refreshing options on input would loop", got.RebuildInputs)
	}
	if !got.StayedClosed || !got.KeptFocus {
		t.Errorf("a rebuild after a pick left the list closed=%v and focus kept=%v, want both: a closed list stays closed", got.StayedClosed, got.KeptFocus)
	}
	if !got.ExtraKept {
		t.Error("the page's extra row was lost when the options were rebuilt")
	}
	if got.Rows != "Blueberry|Blackberry|Other" {
		t.Errorf("after the rebuild \"bl\" shows %q, want Blueberry|Blackberry|Other: the rows are not the options the select now holds (and the page's extra)", got.Rows)
	}
	if got.Picked != "blackberry" {
		t.Errorf("Enter after the rebuilds picked %q, want blackberry", got.Picked)
	}
}

// Near the bottom of the viewport the list opens UPWARD and fits above
// the box; near the top it opens below. It is re-placed when the viewport
// changes while open (a phone's keyboard shrinking the visual viewport,
// or a resize), and the keyboard order is the same either way.
func TestSelectOpensUpWhenThereIsNoRoomBelow(t *testing.T) {
	t.Parallel()
	var got struct {
		BottomUp         bool    `json:"bottomUp"`
		BottomListTop    float64 `json:"bottomListTop"`
		BottomListEnd    float64 `json:"bottomListEnd"`
		BottomBoxTop     float64 `json:"bottomBoxTop"`
		TopUp            bool    `json:"topUp"`
		TopListTop       float64 `json:"topListTop"`
		TopBoxEnd        float64 `json:"topBoxEnd"`
		MovedUp          bool    `json:"movedUp"`
		ScrolledDown     bool    `json:"scrolledDown"`
		PanelUp          bool    `json:"panelUp"`
		NarrowUp         bool    `json:"narrowUp"`
		BroadUp          bool    `json:"broadUp"`
		DroppedOnRemoval int     `json:"droppedOnRemoval"`
		FirstDownUp      string  `json:"firstDownUp"`
		FirstDownBelow   string  `json:"firstDownBelow"`
	}
	drive(t, `
		const frame = () => `+afterFrame+`;
		const input = document.querySelector('#tz-combo');
		const wrap = input.closest('[rst-combo]');
		const list = document.querySelector('#tz-listbox');
		const key = (k) => input.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
		const pin = (edge) => {
			wrap.style.position = 'fixed';
			wrap.style.left = '10px';
			wrap.style.width = '300px';
			wrap.style.top = edge === 'top' ? '10px' : '';
			wrap.style.bottom = edge === 'bottom' ? '10px' : '';
		};
		const out = {};
		pin('bottom');
		input.focus();
		input.click();
		await frame();
		await frame();
		out.bottomUp = wrap.hasAttribute('rst-combo-up');
		out.bottomListTop = list.getBoundingClientRect().top;
		out.bottomListEnd = list.getBoundingClientRect().bottom;
		out.bottomBoxTop = input.getBoundingClientRect().top;
		key('ArrowDown');
		out.firstDownUp = input.getAttribute('aria-activedescendant') || '';
		key('Escape');
		input.blur();
		pin('top');
		input.focus();
		input.click();
		await frame();
		await frame();
		out.topUp = wrap.hasAttribute('rst-combo-up');
		out.topListTop = list.getBoundingClientRect().top;
		out.topBoxEnd = input.getBoundingClientRect().bottom;
		key('ArrowDown');
		out.firstDownBelow = input.getAttribute('aria-activedescendant') || '';
		// Moved to the top while open: a scroll in any panel re-places it.
		key('Escape');
		pin('bottom');
		input.blur();
		input.focus();
		input.click();
		await frame();
		await frame();
		pin('top');
		document.querySelector('form').dispatchEvent(new Event('scroll'));
		await frame();
		await frame();
		out.scrolledDown = !wrap.hasAttribute('rst-combo-up');
		// Moved to the bottom while open: a resize re-places it.
		pin('bottom');
		window.dispatchEvent(new Event('resize'));
		await frame();
		await frame();
		out.movedUp = wrap.hasAttribute('rst-combo-up');
		key('Escape');
		// Opened low with one match, then cleared: the list's own height
		// does not fit below either way, so it opens up either way.
		input.blur();
		wrap.style.cssText = 'position:fixed;left:10px;width:300px;top:' + (window.innerHeight - 130) + 'px';
		input.focus();
		input.value = 'Europe/Lond';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		await frame();
		await frame();
		out.narrowUp = wrap.hasAttribute('rst-combo-up');
		input.value = '';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		await frame();
		await frame();
		out.broadUp = wrap.hasAttribute('rst-combo-up');
		key('Escape');
		wrap.removeAttribute('style');
		// Near the top of a scrolling panel that sits low on the screen:
		// the room above the box is outside the panel, so it opens down.
		const panel = document.createElement('div');
		panel.style.cssText = 'position:fixed;left:10px;width:320px;height:200px;overflow-y:auto;top:' + (window.innerHeight - 220) + 'px';
		wrap.parentNode.insertBefore(panel, wrap);
		panel.append(wrap, document.querySelector('#tz'));
		input.focus();
		input.click();
		await frame();
		await frame();
		out.panelUp = wrap.hasAttribute('rst-combo-up');
		key('Escape');
		// And a box removed with its fragment while open lets go of the
		// page-wide listeners at the next event.
		let dropped = 0;
		const rm = window.removeEventListener;
		window.removeEventListener = function (type, ...a) { if (type === 'resize') dropped++; return rm.call(this, type, ...a); };
		// Opened without focus (a synthetic click gives none), so removal
		// fires no blur here, as on a browser that never does.
		input.blur();
		input.click();
		await frame();
		panel.remove();
		window.dispatchEvent(new Event('resize'));
		window.removeEventListener = rm;
		out.droppedOnRemoval = dropped;
		return out;
	`, &got)
	if !got.BottomUp || got.BottomListTop < 0 || got.BottomListEnd > got.BottomBoxTop+1 {
		t.Errorf("at the bottom of the viewport the list opened up=%v spanning %.0f..%.0f with the box at %.0f, want upward, above the box and inside the viewport", got.BottomUp, got.BottomListTop, got.BottomListEnd, got.BottomBoxTop)
	}
	if got.TopUp || got.TopListTop < got.TopBoxEnd-1 {
		t.Errorf("at the top of the viewport the list opened up=%v starting at %.0f with the box ending at %.0f, want downward, below the box", got.TopUp, got.TopListTop, got.TopBoxEnd)
	}
	// Placement is decided by the room against the list's own height, not
	// by how many rows a search shows, so typing never re-places it.
	if !got.NarrowUp || !got.BroadUp {
		t.Errorf("low on the screen, one match opened up=%v and clearing the search left up=%v, want up both times: the room, not the search, decides", got.NarrowUp, got.BroadUp)
	}
	if got.PanelUp {
		t.Error("near the top of a low scrolling panel the list opened upward, into the part of the panel that is clipped")
	}
	if got.DroppedOnRemoval == 0 {
		t.Error("a box removed with its fragment while open kept its page-wide resize listener")
	}
	if !got.ScrolledDown {
		t.Error("moved to the top while open and a panel scrolled, the list still opened upward: a scrolling ancestor must re-place it")
	}
	if !got.MovedUp {
		t.Error("moved to the bottom while open and resized, the list was not re-placed upward")
	}
	if got.FirstDownUp == "" || got.FirstDownUp != got.FirstDownBelow {
		t.Errorf("ArrowDown highlighted %q opening up and %q opening down, want the same row: only placement flips", got.FirstDownUp, got.FirstDownBelow)
	}
}

// Typing never rebuilds a box (its own writes must not look like the page
// changing the options), and reads layout at most once a frame: on a long
// form with several boxes, forced reflow per keystroke was what made
// typing slow on a phone.
func TestSelectTypingIsCheap(t *testing.T) {
	t.Parallel()
	var got struct {
		Rebuilds     int      `json:"rebuilds"`
		HiddenWrites int      `json:"hiddenWrites"`
		Outside      []string `json:"outside"`
		PerFrame     []int    `json:"perFrame"`
		Searched     bool     `json:"searched"`
	}
	drive(t, `
		const frame = () => `+afterFrame+`;
		const out = { rebuilds: 0, perFrame: [] };
		const hosts = new MutationObserver((m) => {
			for (const r of m) for (const n of r.addedNodes) if (n.nodeType === 1 && n.hasAttribute('rst-combo')) out.rebuilds++;
		});
		hosts.observe(document.body, { childList: true, subtree: true });
		// Every attribute written anywhere while typing: a page whose
		// stylesheet has [hidden] inside a :has() pays for each one.
		const tzWrap = document.querySelector('#tz-combo').closest('[rst-combo]');
		out.hiddenWrites = 0;
		out.outside = [];
		let counting = false;
		const attrs = new MutationObserver((m) => {
			if (!counting) return;
			for (const r of m) {
				if (r.attributeName === 'hidden') out.hiddenWrites++;
				if (!tzWrap.contains(r.target)) out.outside.push((r.target.id || r.target.tagName) + '@' + (r.attributeName || r.type));
			}
		});
		attrs.observe(document.documentElement, { attributes: true, childList: true, characterData: true, subtree: true });
		let reads = 0;
		const count = (proto, name) => {
			const was = proto[name];
			proto[name] = function (...a) { reads++; return was.apply(this, a); };
		};
		count(Element.prototype, 'getBoundingClientRect');
		count(Element.prototype, 'scrollIntoView');
		const st = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop');
		Object.defineProperty(Element.prototype, 'scrollTop', { configurable: true, get() { return st.get.call(this); }, set(v) { reads++; st.set.call(this, v); } });
		const input = document.querySelector('#tz-combo');
		input.focus();
		input.click();
		await frame();
		const type = (v) => { input.value = v; input.dispatchEvent(new Event('input', { bubbles: true })); };
		counting = true;
		for (const [a, b] of [['E', 'Eu'], ['Eur', 'Euro'], ['Europ', 'Europe'], ['Europe/', 'Europe/L']]) {
			reads = 0;
			// Two keystrokes inside one frame, as a phone keyboard sends
			// them: each a new query, each searched (one at once, one in the
			// frame).
			type(a);
			type(b);
			await frame();
			out.perFrame.push(reads);
		}
		attrs.takeRecords();
		counting = false;
		attrs.disconnect();
		out.searched = document.querySelectorAll('#tz-listbox [rst-combo-option]:not([style*="none"])').length < 70;
		input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
		hosts.disconnect();
		Object.defineProperty(Element.prototype, 'scrollTop', st);
		return out;
	`, &got)
	if got.HiddenWrites != 0 {
		t.Errorf("typing wrote the hidden attribute %d times, want 0: a page with [hidden] in a :has() pays for every write", got.HiddenWrites)
	}
	if len(got.Outside) != 0 {
		t.Errorf("typing mutated %d things outside the combobox (first: %s), want none", len(got.Outside), got.Outside[0])
	}
	if got.Rebuilds != 0 {
		t.Errorf("typing rebuilt a box %d times, want 0: the box's own writes were taken for the page changing its options", got.Rebuilds)
	}
	if !got.Searched {
		t.Fatal("typing did not narrow the list: the layout fence below would prove nothing")
	}
	for i, n := range got.PerFrame {
		if n > 1 {
			t.Errorf("frame %d of typing read layout %d times, want at most 1", i, n)
		}
	}
}

// A rebuild in the middle of something: typing not yet searched, an Enter
// whose character has not arrived, a highlighted extra row, a highlighted
// row the page just disabled, a box whose pick is selected for replacing.
// Each must come through the rebuild meaning what it meant before it.
func TestSelectRebuildKeepsWhatIsInFlight(t *testing.T) {
	t.Parallel()
	var got struct {
		FlushKept     string `json:"flushKept"`
		EnterGuarded  bool   `json:"enterGuarded"`
		ExtraOn       string `json:"extraOn"`
		DisabledOn    string `json:"disabledOn"`
		PickSelection string `json:"pickSelection"`
	}
	drive(t, `
		const frame = () => `+afterFrame+`;
		const select = document.querySelector('#flavour');
		const box = () => document.querySelector('#flavour-combo');
		const type = (v) => { const i = box(); i.value = v; i.dispatchEvent(new Event('input', { bubbles: true })); };
		const key = (k) => { const e = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }); box().dispatchEvent(e); return e; };
		const on = () => { const a = box().getAttribute('aria-activedescendant'); return a ? document.getElementById(a).textContent : ''; };
		const out = {};
		select.innerHTML = '<option value="blueberry">Blueberry</option><option value="blackberry">Blackberry</option><option value="kiwi">Kiwi</option>';
		await frame();

		// "ber", an arrow to its second row, then "berry" in the same frame —
		// not yet searched — and an options change in the same task. "berry" ties every row, so leaving settles nothing;
		// the old search's arrowed row must not ride through.
		box().focus();
		box().click();
		await frame();
		const before = select.value;
		type('ber');
		key('ArrowDown');
		type('berry');
		select.append(new Option('Bilberry', 'bilberry'));
		await frame();
		key('Tab');
		out.flushKept = select.value === before ? '' : select.value;
		key('Escape');

		// Enter picks a row; the page's change handler then rebuilds the
		// options; the Enter's character arrives on the new box and must not
		// submit the form.
		select.addEventListener('change', () => select.append(new Option('Fig', 'fig')), { once: true });
		box().focus();
		box().click();
		await frame();
		type('kiwi');
		await frame();
		key('Enter');
		await Promise.resolve();
		const kp = new KeyboardEvent('keypress', { key: 'Enter', bubbles: true, cancelable: true });
		box().dispatchEvent(kp);
		out.enterGuarded = kp.defaultPrevented;
		box().dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }));

		// The highlight on one of the page's extra rows survives a rebuild.
		box().focus();
		box().click();
		await frame();
		box().closest('[rst-combo]').rstExtras([{ label: 'Other', run: () => {} }]);
		key('End');
		select.append(new Option('Lime', 'lime'));
		await frame();
		out.extraOn = on();
		box().closest('[rst-combo]').rstExtras([]);
		key('Escape');

		// A highlighted row the page disables is not restored as the highlight.
		box().focus();
		box().click();
		await frame();
		type('kiwi');
		await frame();
		select.querySelector('option[value="kiwi"]').disabled = true;
		await frame();
		out.disabledOn = on();
		key('Escape');
		select.querySelector('option[value="kiwi"]').disabled = false;
		await frame();

		// Focusing an answered box selects its pick, so the first keystroke
		// replaces it; a rebuild keeps that.
		box().blur();
		box().focus();
		const len = box().value.length;
		select.append(new Option('Plum', 'plum'));
		await frame();
		out.pickSelection = box().selectionStart + '..' + box().selectionEnd + '/' + len;
		return out;
	`, &got)
	if got.FlushKept != "" {
		t.Errorf("leaving after \"berry\" committed %q: the rebuild restored the arrowed row of a search the typing had already replaced", got.FlushKept)
	}
	if !got.EnterGuarded {
		t.Error("an Enter that picked a row, whose change handler rebuilt the box, went on to submit the form")
	}
	if got.ExtraOn != "Other" {
		t.Errorf("a highlighted extra row came through a rebuild as %q, want Other: Enter would do something else", got.ExtraOn)
	}
	if got.DisabledOn == "Kiwi" {
		t.Error("a row the page disabled was restored as the highlight")
	}
	if !strings.HasPrefix(got.PickSelection, "0..") || strings.Split(strings.TrimPrefix(got.PickSelection, "0.."), "/")[0] != strings.Split(got.PickSelection, "/")[1] {
		t.Errorf("a focused answered box reads selection %s after a rebuild, want its whole pick selected", got.PickSelection)
	}
}
