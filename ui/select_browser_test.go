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
		mo.observe(list, { childList: true, subtree: true, attributes: true, attributeFilter: ['hidden'], attributeOldValue: true });

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
						if (n.hidden) out.hiddenMoved.push(word.slice(0, i) + ': ' + (n.id || n.tagName));
					}
				} else {
					if (!writes.has(r.target)) writes.set(r.target, []);
					writes.get(r.target).push(r.oldValue !== null);
				}
			}
			for (const [el, olds] of writes) {
				olds.forEach((was, j) => {
					const now = j + 1 < olds.length ? olds[j + 1] : el.hidden;
					if (was === now) out.noopWrites.push(word.slice(0, i) + ': ' + (el.id || el.tagName));
				});
			}
		}
		mo.disconnect();
		out.shown = list.querySelectorAll('[rst-combo-option]:not([hidden])').length;

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
		out.promptIdle = !blank.hidden;
		for (const q of ['a', 'i', 'united k']) {
			type(q);
			await frame();
			if (q === 'a') out.sepSearching = !sep.hidden;
		}
		type('');
		await frame();
		out.order = seq();
		out.restored = out.order.join() === s0.join();
		out.sepAfter = !sep.hidden;
		type('choose');
		await frame();
		out.promptTyped = !blank.hidden;
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
		const top = list.querySelector('[rst-combo-option]:not([hidden])');
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
		out.promptRow = !prompt.hidden;
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
		const input = document.querySelector('#size-combo');
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
		out.openFlipListed = !blank.hidden;
		out.openFlipActive = input.getAttribute('aria-activedescendant') || '';
		// Idle (nothing typed), the refresh is not a search for the pick's
		// own text: every real choice stays, and the box drops "None".
		out.openFlipShown = document.querySelectorAll('#size-listbox [rst-combo-option]:not([hidden])').length;
		out.openFlipText = input.value;
		key('Escape');
		select.required = false;
		await frame();
		// And extras handed over while the box shows the pick hide nothing.
		input.focus();
		input.click();
		await frame();
		input.closest('[rst-combo]').rstExtras([{ label: 'Something else', run: () => {} }]);
		out.idleExtrasShown = document.querySelectorAll('#size-listbox [rst-combo-option]:not([hidden])').length;
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
		input.focus();
		input.click();
		await frame();
		const medium = [...document.querySelectorAll('#size-listbox [rst-combo-option]')].find((li) => li.textContent === 'Medium');
		medium.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
		out.afterRemovalValue = select.value;
		select.insertBefore(small, select.querySelector('option[value="m"]'));
		select.value = '';
		select.dispatchEvent(new Event('change', { bubbles: true }));

		// The SELECTED option removed: the next pick is what the box shows.
		select.value = 's';
		select.dispatchEvent(new Event('change', { bubbles: true }));
		const small2 = select.querySelector('option[value="s"]');
		small2.remove();
		input.focus();
		input.click();
		await frame();
		const large = [...document.querySelectorAll('#size-listbox [rst-combo-option]')].find((li) => li.textContent === 'Large');
		large.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
		out.removedSelectedShown = input.value;
		out.removedSelectedValue = select.value;
		select.insertBefore(small2, select.querySelector('option[value="m"]'));

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
		out.screenOrder = [...dList.querySelectorAll('[rst-combo-option]:not([hidden])')].map((li) => li.textContent).join('|');
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
