//go:build browser

// The browser tests for the two enhancements that need one —
// field-select's combobox and the date fields' — the residue a Go test
// cannot reach, because it needs a real JS engine and real focus and
// keyboard handling.
//
// Build-tagged rather than env-gated so a plain `go test ./...` never
// silently half-runs it, and so chromedp stays out of the ordinary
// build graph. Run it with:
//
//	go test -tags browser ./ui/
//
// It rides the harness package's rig: Chromium discovery
// (RASTRILLO_CHROME, PATH, the Playwright cache — a skip is not a
// pass, RASTRILLO_BROWSER_OPTIONAL makes it deliberate), the
// loud-failure watchers, and the screen gate's junk scan all live
// there now, shared with every browser drive in the family.
//
// KNOWN LIMITATION, stated rather than discovered: this test is
// timing-sensitive under machine load. On an idle box it passes in
// ~0.4s and 20 consecutive runs are green. On a box at load ~9-14 it
// fails roughly 1 run in 4, always the same way — a keystroke arrives
// while focus has drifted, Enter reaches the document instead of the
// combobox, the form submits, the execution context dies, and the next
// step hangs until the deadline. The failure names the step it got to,
// so it is legible rather than mysterious.
//
// CI runs this in its browser job on an otherwise idle runner; the
// load-flake cost falls on whoever runs it deliberately on a busy
// machine. Rerun before believing a failure, and read the reported
// step: a real regression fails at a specific assertion, load flake
// fails at a deadline after "read-mirrored-value" or later. Fixing it
// properly likely means driving the widget through synthesised events
// inside one page evaluation, which trades away the fidelity of real
// CDP input — not obviously the right trade, so it has not been made.
//
// One test per enhancement, deliberately: a browser drive is
// expensive, so each drives a whole journey — render, enhance, type,
// keyboard-commit, mirror, submit — and asserts the server received the
// value. Everything cheaper lives in field_select_test.go,
// datetime_test.go and shim_test.go.
package ui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/carlosframework/rastrillo/harness"
)

// page builds the handler serving one form carrying an enhanced
// field-select, the real select.js, tokens.css and theme, and records
// what a submit delivers.
func page(t *testing.T, optionCount int) (http.Handler, chan string) {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	got := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /select.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(SelectJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	// Both halves of the scaffolded stylesheet, the way a real app links
	// them: structure from tokens.css, colour and type from the theme.
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		select {
		case got <- r.PostFormValue("author"):
		default:
		}
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>ok</title></head><body><p id="done">received</p></body></html>`)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>select</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css">` +
			`<script defer src="/select.js"></script></head><body>` +
			`<form method="post" action="/submit">`)
		if err := tmpl.ExecuteTemplate(&body, "field-select", selectData(optionCount)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body.WriteString(`<button type="submit" id="go">Save</button></form></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})
	return mux, got
}

// TestEnhancedSelectDrivesTheWholeJourney is the one browser test.
//
// Bug classes it exists to catch — each renders perfectly and says
// nothing wrong:
//
//   - the combobox never mirrors back, so the form submits the old value
//     while the screen shows the new one;
//   - the native select is removed rather than hidden, so the form
//     submits nothing at all;
//   - the label is left pointing at the hidden select, leaving the
//     control the user types into with no accessible name;
//   - a JS error takes the enhancement down and the page still looks fine.
func TestEnhancedSelectDrivesTheWholeJourney(t *testing.T) {
	mux, submitted := page(t, 40)
	rig := harness.New(t, func(string) http.Handler { return mux })

	// A healthy run takes well under a second on an idle machine, but
	// this is wall-clock against a real browser: on a loaded box it is
	// slower by orders of magnitude, and a budget tuned to the idle case
	// fails for no reason. 60s was that mistake, tuned on a quiet dev
	// machine: the browser CI job's first-ever run hit the documented
	// load flake at exactly the deadline. The budget exists so a hang
	// fails as itself, not to race a busy runner's clock — 180s still
	// fails far faster than Go's default test timeout, so a genuine
	// regression surfaces as a deadline rather than a hung suite.
	ctx, cancelTimeout := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancelTimeout()

	var (
		comboCount, nativeCount, optionsShown int
		labelFor, nativeValue                 string
		filterText                            string
		nativeHidden                          bool
	)

	// A bare "context deadline exceeded" tells whoever hits this in CI
	// nothing at all. Record the last step that completed, and report it
	// with the state gathered so far.
	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"), at("navigated"),
		chromedp.WaitVisible(`input[role="combobox"]`, chromedp.ByQuery),
		at("combobox-visible"),
		// The enhancement happened; the native control survived it.
		chromedp.Evaluate(`document.querySelectorAll('input[role="combobox"]').length`, &comboCount),
		chromedp.Evaluate(`document.querySelectorAll('select[name="author"]').length`, &nativeCount),
		// Hidden, not removed. sr-only leaves a ~1px box; anything wider
		// means the select is still taking real layout space.
		chromedp.Evaluate(`(document.querySelector('select[name="author"]')?.getBoundingClientRect().width ?? 999) < 4`, &nativeHidden),
		// The label must name the control the user actually types into.
		chromedp.Evaluate(`document.querySelector('label')?.getAttribute('for') ?? ''`, &labelFor),

		// Every probe above is null-safe on purpose: a missing node should
		// reach the assertions as an empty value, so the failure names the
		// broken invariant instead of surfacing a chromedp node error.
		//
		// Filter, then pick with the keyboard only: a mouse-only
		// combobox is a broken one.
		chromedp.Click(`input[role="combobox"]`, chromedp.ByQuery), at("clicked-combobox"),
		chromedp.SendKeys(`input[role="combobox"]`, "Option 12", chromedp.ByQuery), at("typed-filter"),
		chromedp.Evaluate(`document.querySelectorAll('[role="option"]').length`, &optionsShown),
		chromedp.Evaluate(`document.querySelector('input[role="combobox"]')?.value ?? ''`, &filterText),
		// Synchronise on observable state rather than assuming a keystroke
		// landed. Under load the arrow key can arrive before the filtered
		// list is drawn, or while focus has drifted; then Enter reaches
		// the document instead of the combobox, the form submits, the
		// execution context dies and the next step hangs on a page that
		// no longer exists. Waiting for the highlight turns that into a
		// fast, legible failure at the exact step that did not happen.
		chromedp.WaitVisible(`[role="option"]`, chromedp.ByQuery), at("list-drawn"),
		// SendKeys, not KeyEvent: KeyEvent trusts ambient focus, and on
		// a CI runner focus drifted between the highlight and Enter —
		// Enter reached the form, the form submitted an empty value,
		// and the drive died on a page with no widget left to poll.
		// SendKeys focuses its target before delivering the same real
		// CDP key events, so the key lands where the user's would.
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.ArrowDown), chromedp.ByQuery), at("arrow-down"),
		chromedp.WaitVisible(`[role="option"].is-active`, chromedp.ByQuery), at("option-highlighted"),
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.Enter), chromedp.ByQuery), at("enter"),

		// The mirror: what the form will actually submit. Polled, not
		// sampled: three CI runs in a row read "" here and then burned
		// the whole drive budget waiting for a #done the empty submit
		// could never produce. Bounded at 10s so a commit that truly
		// never happens fails fast, at this step, with the widget's
		// state in the error instead of a bare deadline.
		chromedp.ActionFunc(func(ctx context.Context) error {
			deadline := time.Now().Add(10 * time.Second)
			for {
				var v string
				if err := chromedp.Evaluate(`document.querySelector('select[name="author"]')?.value ?? ''`, &v).Do(ctx); err != nil {
					return err
				}
				if v != "" {
					nativeValue = v
					return nil
				}
				if time.Now().After(deadline) {
					var snap string
					_ = chromedp.Evaluate(`JSON.stringify({
						activeEl: (document.activeElement && (document.activeElement.tagName + "/" + (document.activeElement.getAttribute("role") || ""))) || "none",
						hasFocus: document.hasFocus(),
						options: document.querySelectorAll('[role="option"]').length,
						highlighted: document.querySelectorAll('[role="option"].is-active').length,
						listHidden: (function(l){ return l ? l.hidden : "no-list" })(document.querySelector('[role="listbox"]')),
						inputValue: (function(i){ return i ? i.value : "no-input" })(document.querySelector('input[role="combobox"]')),
					})`, &snap).Do(ctx)
					return fmt.Errorf("mirror never committed within 10s of Enter; widget state: %s", snap)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
		}), at("read-mirrored-value"),

		// Submit rather than Click: what this test asserts is that the
		// form posts the mirrored value, and chromedp.Click waits for the
		// button to be actionable — a wait that intermittently never
		// resolved here even with the value already correctly mirrored.
		// Submitting the form exercises the thing under test without
		// depending on hit-testing an overlay-adjacent button.
		chromedp.Submit(`#go`, chromedp.ByQuery), at("submitted-form"),
		chromedp.WaitVisible(`#done`, chromedp.ByQuery), at("server-responded"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v\n  filterText=%q optionsShown=%d nativeValue=%q labelFor=%q",
			reached, err, filterText, optionsShown, nativeValue, labelFor)
	}

	if comboCount != 1 {
		t.Errorf("expected exactly one combobox, found %d", comboCount)
	}
	if nativeCount != 1 {
		t.Errorf("the native select must survive enhancement, found %d", nativeCount)
	}
	if !nativeHidden {
		t.Error("the native select is still visible; it should be hidden, not removed")
	}
	if !strings.HasSuffix(labelFor, "-combo") {
		t.Errorf("label still points at the hidden select (for=%q): the combobox has no accessible name", labelFor)
	}
	// Focus must select the committed label so typing replaces it. When
	// it does not, the filter text becomes "Option 1Option 12", nothing
	// matches, and Enter commits the select's pre-existing default — a
	// green test that proves nothing. Assert the text we actually typed.
	if filterText != "Option 12" {
		t.Errorf("filter box holds %q, want %q: focus is not replacing the committed label", filterText, "Option 12")
	}
	if optionsShown != 1 {
		t.Errorf("filtering for a unique label showed %d options, want 1", optionsShown)
	}
	// "Option 12" is the 12th option, so value "12". Crucially NOT "1",
	// which is what the select holds by default — an assertion that
	// merely checked non-empty would pass on the bug this test exists for.
	if nativeValue != "12" {
		t.Errorf("native select holds %q, want %q — the combobox did not mirror the keyboard pick back", nativeValue, "12")
	}

	select {
	case v := <-submitted:
		if v != nativeValue {
			t.Errorf("server received author=%q, the select held %q", v, nativeValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the form never reached the server")
	}

	// The console check and the junk scan are the rig's screen gate
	// now — which also reads input values and aria-labels, and knows
	// the junk set in full: the "null" this file's in-place scan was
	// missing arrived with the move.
	rig.Screen("body", "after the journey")
}

// datePage serves one form carrying an enhanced field-date, the real
// datetime.js, tokens.css and theme, and records what a submit
// delivers.
func datePage(t *testing.T) (http.Handler, chan string) {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	got := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /datetime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(DatetimeJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		select {
		case got <- r.PostFormValue("due"):
		default:
		}
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>ok</title></head><body><p id="done">received</p></body></html>`)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>date</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css">` +
			`<script defer src="/datetime.js"></script></head><body>` +
			`<form method="post" action="/submit">`)
		if err := tmpl.ExecuteTemplate(&body, "field-date", map[string]any{
			"Name": "due", "Label": "Due",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body.WriteString(`<button type="submit" id="go">Save</button></form></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})
	return mux, got
}

// TestEnhancedDateDrivesTheWholeJourney is the date field's one browser
// test, on the same terms as the select drive above: a real engine, real
// focus, real keys.
//
// Bug classes it exists to catch — each renders perfectly and says
// nothing wrong:
//
//   - the parser reads "tomorrow" but nothing is written back, so the
//     form submits an empty date while the screen shows one;
//   - the native input is removed rather than hidden, so the form
//     submits nothing at all;
//   - the label is left pointing at the hidden native, leaving the
//     control the user types into with no accessible name;
//   - a JS error takes the enhancement down and the page still looks
//     fine (the rig's screen gate catches this one).
//
// It shares the select drive's KNOWN LIMITATION: real CDP input under
// machine load can deliver a keystroke while focus has drifted. Read
// the reported step before believing a failure.
func TestEnhancedDateDrivesTheWholeJourney(t *testing.T) {
	mux, submitted := datePage(t)
	rig := harness.New(t, func(string) http.Handler { return mux })

	ctx, cancelTimeout := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancelTimeout()

	// The browser and this test share one clock, so "tomorrow" is the
	// same day for both — except across the one midnight a run could
	// straddle. Both readings are accepted rather than pinning the
	// clock, which a real browser will not let a Go test do anyway.
	before := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	var (
		comboCount, nativeCount int
		labelFor, nativeValue   string
		typed                   string
		nativeHidden            bool
	)

	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"), at("navigated"),
		chromedp.WaitVisible(`input[role="combobox"]`, chromedp.ByQuery), at("combobox-visible"),
		chromedp.Evaluate(`document.querySelectorAll('input[role="combobox"]').length`, &comboCount),
		chromedp.Evaluate(`document.querySelectorAll('input[name="due"]').length`, &nativeCount),
		// Hidden, not removed — and "hidden" measured the way the
		// utility actually hides it. A replaced element keeps an
		// intrinsic minimum size whatever inline-size says (the date
		// input measures 6x5 in Chromium, not the select's ~1x1), so
		// what proves it invisible is the clip and the absolute
		// position, not a width threshold. A native left in flow fails
		// all three.
		chromedp.Evaluate(`(function () {
			var el = document.querySelector('input[name="due"]');
			if (!el) return false;
			var s = getComputedStyle(el);
			return s.position === "absolute" && s.clipPath !== "none" &&
				el.getBoundingClientRect().width < 24;
		})()`, &nativeHidden),
		chromedp.Evaluate(`document.querySelector('label')?.getAttribute('for') ?? ''`, &labelFor),

		chromedp.Click(`input[role="combobox"]`, chromedp.ByQuery), at("clicked-combobox"),
		chromedp.SendKeys(`input[role="combobox"]`, "tomorrow", chromedp.ByQuery), at("typed-phrase"),
		chromedp.Evaluate(`document.querySelector('input[role="combobox"]')?.value ?? ''`, &typed),
		// Synchronise on the reading being armed rather than assuming
		// the keystrokes landed: the highlighted row IS the parse, and
		// waiting for it turns a drifted focus into a fast, legible
		// failure at this step instead of a deadline three steps later.
		chromedp.WaitVisible(`[role="option"].is-active`, chromedp.ByQuery), at("reading-armed"),
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.Enter), chromedp.ByQuery), at("enter"),

		chromedp.ActionFunc(func(ctx context.Context) error {
			deadline := time.Now().Add(10 * time.Second)
			for {
				var v string
				if err := chromedp.Evaluate(`document.querySelector('input[name="due"]')?.value ?? ''`, &v).Do(ctx); err != nil {
					return err
				}
				if v != "" {
					nativeValue = v
					return nil
				}
				if time.Now().After(deadline) {
					var snap string
					_ = chromedp.Evaluate(`JSON.stringify({
						activeEl: (document.activeElement && (document.activeElement.tagName + "/" + (document.activeElement.getAttribute("role") || ""))) || "none",
						options: document.querySelectorAll('[role="option"]').length,
						highlighted: document.querySelectorAll('[role="option"].is-active').length,
						inputValue: (function(i){ return i ? i.value : "no-input" })(document.querySelector('input[role="combobox"]')),
					})`, &snap).Do(ctx)
					return fmt.Errorf("the carrier never took a value within 10s of Enter; widget state: %s", snap)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
		}), at("read-carrier-value"),

		chromedp.Submit(`#go`, chromedp.ByQuery), at("submitted-form"),
		chromedp.WaitVisible(`#done`, chromedp.ByQuery), at("server-responded"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v\n  typed=%q nativeValue=%q labelFor=%q",
			reached, err, typed, nativeValue, labelFor)
	}

	after := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	if comboCount != 1 {
		t.Errorf("expected exactly one combobox, found %d", comboCount)
	}
	if nativeCount != 1 {
		t.Errorf("the native date input must survive enhancement, found %d", nativeCount)
	}
	if !nativeHidden {
		t.Error("the native date input is still visible; it should be clipped out of sight, not removed")
	}
	if !strings.HasSuffix(labelFor, "-combo") {
		t.Errorf("label still points at the hidden native (for=%q): the combobox has no accessible name", labelFor)
	}
	if typed != "tomorrow" {
		t.Errorf("the combobox holds %q, want %q: the keystrokes did not land where they were aimed", typed, "tomorrow")
	}
	// The whole point: a phrase typed in English became the wire value
	// the server parses. Crucially NOT today's date, which is what an
	// unparsed reading committing the first quick pick would leave.
	if nativeValue != before && nativeValue != after {
		t.Errorf("the carrier holds %q, want tomorrow (%q or %q)", nativeValue, before, after)
	}

	select {
	case v := <-submitted:
		if v != nativeValue {
			t.Errorf("server received due=%q, the carrier held %q", v, nativeValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the form never reached the server")
	}

	rig.Screen("body", "after the date journey")
}

// groupPage serves one form carrying two hand-written selects: a
// grouped one that should enhance, and a large one that says no.
//
// Hand-written rather than rendered through field-select, because
// field-select's Options are flat — the partial has no optgroup to
// emit. Grouped selects are exactly the case an app writes by hand, so
// that is what the drive drives.
func groupPage(t *testing.T) (http.Handler, chan string) {
	t.Helper()

	got := make(chan string, 1)
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
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		select {
		case got <- r.PostFormValue("city"):
		default:
		}
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>ok</title></head><body><p id="done">received</p></body></html>`)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
			`<title>groups</title><link rel="stylesheet" href="/tokens.css">` +
			`<link rel="stylesheet" href="/theme.css">` +
			`<script defer src="/select.js"></script></head><body>` +
			`<form method="post" action="/submit">` +
			`<label class="rst-field__label" for="city">City</label>` +
			`<select class="rst-input" id="city" name="city" data-rst-select` +
			` data-rst-select-filter="Type to filter"` +
			` data-rst-select-results="{n} results"` +
			` data-rst-select-result-one="1 result">` +
			`<option value="">Choose a city</option>` +
			`<optgroup label="Ireland">` +
			`<option value="dub">Dublin</option>` +
			`<option value="cor">Cork</option>` +
			`<option value="gal">Galway</option>` +
			`</optgroup>` +
			`<optgroup label="Spain">` +
			`<option value="mad">Madrid</option>` +
			`<option value="bcn">Barcelona</option>` +
			`</optgroup>` +
			`</select>` +
			// The markup-side opt-out, on a select far past the size that
			// would otherwise enhance: it must stay a plain native select.
			`<label class="rst-field__label" for="team">Team</label>` +
			`<select class="rst-input" id="team" name="team" data-rst-select="false">`)
		for i := 1; i <= 40; i++ {
			fmt.Fprintf(&body, `<option value="%d">Team %d</option>`, i, i)
		}
		body.WriteString(`</select>` +
			`<button type="submit" id="go">Save</button></form></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})
	return mux, got
}

// TestGroupedSelectRendersItsGroups drives the two things a Go test
// cannot see: that a grouped select keeps its groups on the way into
// the combobox, and that a select saying data-rst-select="false" is
// left alone.
//
// Bug classes it exists to catch — each renders perfectly and says
// nothing wrong:
//
//   - the mirror flattens native.options, so the headings the author
//     wrote to make a long list readable silently vanish;
//   - a group whose options all filter out keeps its heading, leaving a
//     heading over nothing;
//   - the headings join the keyboard order, so arrowing down lands on a
//     heading and Enter commits the wrong option — or nothing;
//   - the opt-out is read as a truthy attribute (it is present, after
//     all) and the select enhances anyway.
//
// It shares the select drive's KNOWN LIMITATION above: real CDP input
// under machine load can deliver a keystroke while focus has drifted.
// Read the reported step before believing a failure.
func TestGroupedSelectRendersItsGroups(t *testing.T) {
	mux, submitted := groupPage(t)
	rig := harness.New(t, func(string) http.Handler { return mux })

	ctx, cancelTimeout := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancelTimeout()

	var (
		comboCount, optOutEnhanced            int
		optOutVisible                         bool
		groupLabels, headings                 string
		headingsAreOptions                    int
		openGroups, openOptions               int
		narrowedGroups, narrowedOptions       int
		narrowedHeadings                      string
		activeText, nativeValue, carrierShape string
	)

	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}
	// Poll rather than sample: the list is redrawn on every keystroke,
	// and under load a probe fired straight after SendKeys reads the
	// list as it was. Waiting on the observable state turns a drifted
	// keystroke into a fast failure at the step that did not happen.
	until := func(name, js string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			deadline := time.Now().Add(10 * time.Second)
			for {
				var ok bool
				if err := chromedp.Evaluate(js, &ok).Do(ctx); err != nil {
					return err
				}
				if ok {
					return nil
				}
				if time.Now().After(deadline) {
					var snap string
					_ = chromedp.Evaluate(`JSON.stringify({
						options: document.querySelectorAll('[role="option"]').length,
						groups: document.querySelectorAll('[role="group"]').length,
						headings: Array.from(document.querySelectorAll('.rst-select__group')).map(function (h) { return h.textContent }),
						inputValue: (function (i) { return i ? i.value : "no-input" })(document.querySelector('input[role="combobox"]')),
					})`, &snap).Do(ctx)
					return fmt.Errorf("%s never became true within 10s; widget state: %s", name, snap)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
		})
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"), at("navigated"),
		chromedp.WaitVisible(`input[role="combobox"]`, chromedp.ByQuery), at("combobox-visible"),

		// Exactly one enhancement on a page holding two selects, both of
		// which carry the attribute.
		chromedp.Evaluate(`document.querySelectorAll('input[role="combobox"]').length`, &comboCount),
		chromedp.Evaluate(`document.querySelectorAll('select[name="team"][data-rst-enhanced]').length`, &optOutEnhanced),
		// Untouched means untouched: still a real, visible native select.
		chromedp.Evaluate(`(document.querySelector('select[name="team"]')?.getBoundingClientRect().width ?? 0) > 40`, &optOutVisible),
		// The grouped native survives as the carrier, groups and all.
		chromedp.Evaluate(`(function () {
			var s = document.querySelector('select[name="city"]');
			if (!s) return "no-select";
			return s.querySelectorAll('optgroup').length + "/" + s.options.length;
		})()`, &carrierShape),

		// Open it: focus draws the unfiltered list.
		chromedp.Click(`input[role="combobox"]`, chromedp.ByQuery), at("clicked-combobox"),
		until("the grouped list is drawn", `document.querySelectorAll('[role="option"]').length === 5`),
		at("list-drawn"),
		chromedp.Evaluate(`document.querySelectorAll('[role="group"]').length`, &openGroups),
		chromedp.Evaluate(`document.querySelectorAll('[role="option"]').length`, &openOptions),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[role="group"]')).map(function (g) { return g.getAttribute('aria-label') }).join(',')`, &groupLabels),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.rst-select__group')).map(function (h) { return h.textContent }).join(',')`, &headings),
		// A heading is furniture, never a pick.
		chromedp.Evaluate(`document.querySelectorAll('.rst-select__group[role="option"]').length`, &headingsAreOptions),

		// Filter to "a": Galway in one group, Madrid and Barcelona in the
		// other. Both groups survive, so the keyboard has a boundary to
		// cross.
		chromedp.SendKeys(`input[role="combobox"]`, "a", chromedp.ByQuery), at("typed-a"),
		until("the list narrows to three", `document.querySelectorAll('[role="option"]').length === 3`),
		at("narrowed-to-three"),
		// Two arrows: the second lands on the first option of the SECOND
		// group. If the headings were in the keyboard order it would land
		// on the "Spain" heading instead.
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.ArrowDown), chromedp.ByQuery), at("arrow-down-1"),
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.ArrowDown), chromedp.ByQuery), at("arrow-down-2"),
		until("a row is highlighted", `document.querySelectorAll('[role="option"].is-active').length === 1`),
		at("row-highlighted"),
		chromedp.Evaluate(`document.querySelector('[role="option"].is-active')?.textContent ?? ''`, &activeText),

		// Extend the filter to "ad": only Madrid matches, so the Ireland
		// group empties — and must take its heading with it.
		chromedp.SendKeys(`input[role="combobox"]`, "d", chromedp.ByQuery), at("typed-d"),
		until("the list narrows to one", `document.querySelectorAll('[role="option"]').length === 1`),
		at("narrowed-to-one"),
		chromedp.Evaluate(`document.querySelectorAll('[role="group"]').length`, &narrowedGroups),
		chromedp.Evaluate(`document.querySelectorAll('[role="option"]').length`, &narrowedOptions),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.rst-select__group')).map(function (h) { return h.textContent }).join(',')`, &narrowedHeadings),

		// The sole match commits on Enter, and mirrors onto the carrier.
		chromedp.SendKeys(`input[role="combobox"]`, string(kb.Enter), chromedp.ByQuery), at("enter"),
		until("the mirror commits", `(document.querySelector('select[name="city"]')?.value ?? '') !== ''`),
		at("read-mirrored-value"),
		chromedp.Evaluate(`document.querySelector('select[name="city"]')?.value ?? ''`, &nativeValue),

		chromedp.Submit(`#go`, chromedp.ByQuery), at("submitted-form"),
		chromedp.WaitVisible(`#done`, chromedp.ByQuery), at("server-responded"),
	); err != nil {
		t.Fatalf("drive failed after %q: %v\n  groupLabels=%q headings=%q activeText=%q nativeValue=%q",
			reached, err, groupLabels, headings, activeText, nativeValue)
	}

	if comboCount != 1 {
		t.Errorf("expected exactly one combobox, found %d", comboCount)
	}
	if optOutEnhanced != 0 {
		t.Errorf(`data-rst-select="false" enhanced anyway (%d enhanced selects): the opt-out is being read as a present attribute`, optOutEnhanced)
	}
	if !optOutVisible {
		t.Error(`the opted-out select is no longer a visible native select; "false" must leave it entirely alone`)
	}
	if carrierShape != "2/6" {
		t.Errorf("the native carrier reads %q, want %q: enhancement must not touch the select", carrierShape, "2/6")
	}
	if openGroups != 2 {
		t.Errorf("the open list holds %d ARIA groups, want 2: the optgroups were flattened away", openGroups)
	}
	if openOptions != 5 {
		t.Errorf("the open list holds %d options, want 5", openOptions)
	}
	if groupLabels != "Ireland,Spain" {
		t.Errorf("group aria-labels are %q, want %q: the optgroup labels did not carry over", groupLabels, "Ireland,Spain")
	}
	if headings != "Ireland,Spain" {
		t.Errorf("visible group headings are %q, want %q", headings, "Ireland,Spain")
	}
	if headingsAreOptions != 0 {
		t.Errorf("%d group headings carry role=option; a heading is furniture, not a pick", headingsAreOptions)
	}
	// Two arrows from nothing highlighted lands on the second match,
	// which lives in the second group. A heading in the keyboard order
	// would leave "Galway" here (or nothing highlighted at all).
	if activeText != "Madrid" {
		t.Errorf("two ArrowDowns highlighted %q, want %q: the keyboard order is not skipping the group headings", activeText, "Madrid")
	}
	if narrowedGroups != 1 || narrowedOptions != 1 {
		t.Errorf("filtering to one match left %d groups and %d options, want 1 and 1", narrowedGroups, narrowedOptions)
	}
	if narrowedHeadings != "Spain" {
		t.Errorf("after filtering the headings read %q, want %q: an emptied group kept its heading", narrowedHeadings, "Spain")
	}
	if nativeValue != "mad" {
		t.Errorf("the carrier holds %q, want %q — the pick inside a group did not mirror back", nativeValue, "mad")
	}

	select {
	case v := <-submitted:
		if v != nativeValue {
			t.Errorf("server received city=%q, the select held %q", v, nativeValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the form never reached the server")
	}

	rig.Screen("body", "after the grouped journey")
}

// ── The menu idioms: exclusivity, light dismiss, and the dialog panel ──

// menuPage serves one page carrying every menu shape at once, plus the
// two <details> that must stay OUT of the exclusivity group, plus the
// real shim. Everything on it is the library's own markup: the shell
// account dropdown from the topbar layout's sample, a row menu from the
// list grid, a nested rst-menu-group inside the dropdown, the sidebar's
// chrome strip and a toggle-block.
//
// Hand-assembled rather than rendered from Styleguide() because the two
// shell samples are whole page frames that cannot both be on one page,
// and because the drive needs stable ids to click.
func menuPage(t *testing.T) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /rastrillo.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(ShimJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
			`<title>menus</title><link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme.css">`+
			`<script defer src="/rastrillo.js"></script></head><body>`+
			// The header dropdown, in the shared group.
			`<header class="rst-shell__bar">`+
			`<details class="rst-dropdown rst-shell__account" name="`+MenuGroupDefault+`" id="account">`+
			`<summary id="account-summary">Account</summary>`+
			`<div class="rst-dropdown__menu"><a id="account-item" href="#settings">Settings</a></div>`+
			`</details></header>`+
			// A list-bar filter dropdown with a nested submenu whose group
			// is deliberately different. The search form is here for the
			// same reason it is in a real list bar: it takes the strip's
			// first 20rem, which puts the filter far enough along that its
			// menu — right-aligned to the summary, 176px wide — opens
			// inside the viewport instead of off the left edge, where
			// nothing can click it.
			`<div class="rst-lbar">`+
			`<search><form class="rst-search" method="get">`+
			`<input type="search" name="q" aria-label="Search"></form></search>`+
			`<details class="rst-dropdown" name="`+MenuGroupDefault+`" id="filter">`+
			`<summary id="filter-summary">Filter</summary>`+
			`<div class="rst-dropdown__menu">`+
			`<a id="filter-item" href="#paid">Paid</a>`+
			`<details class="rst-menu-group" name="rst-menus-price" id="submenu">`+
			`<summary id="submenu-summary">Price</summary>`+
			`<div><a id="submenu-item" href="#free">Free</a></div></details>`+
			`</div></details></div>`+
			// A row menu, in the shared group.
			`<div class="rst-card" style="--rst-cols: 1fr 32px">`+
			`<div class="rst-lrow"><a class="rst-nm" href="#row">Grace Hopper</a>`+
			`<details class="rst-row-menu" name="`+MenuGroupDefault+`" id="rowmenu">`+
			`<summary id="rowmenu-summary" aria-label="Actions for Grace Hopper">…</summary>`+
			`<div class="rst-row-menu__panel"><a href="#view">View</a></div>`+
			`</details></div></div>`+
			// Outside the group, both of them: the sidebar's chrome strip
			// and a toggle-block. Neither is a menu.
			`<details class="rst-shell__chrome" id="chrome" open><summary id="chrome-summary">Menu</summary></details>`+
			`<div class="rst-tblock"><label class="rst-tblock__head">`+
			`<input type="checkbox" id="tblock-input" name="notify" checked>`+
			`<span class="rst-switch__track" aria-hidden="true"></span>`+
			`<span><span class="rst-tblock__title">Email notifications</span></span></label>`+
			`<div class="rst-tblock__body">Body.</div></div>`+
			// Somewhere unambiguous to click that is not inside any menu.
			`<main class="rst-page" id="main"><p id="elsewhere">Nothing here.</p></main>`+
			`</body></html>`)
	})
	return mux
}

// openState reads which of the page's <details> are open, as one string,
// so a failure names the whole state rather than one boolean.
const openStateJS = `["account","filter","submenu","rowmenu","chrome"].` +
	`filter(function (id) { return document.getElementById(id).open }).join(",")`

// TestMenuExclusivityAndDropdownDismissDrive is the browser drive for
// the two halves of the menu contract that no Go test can reach: the
// native <details name> group, which needs a real engine implementing
// it, and the shim's light dismiss, which needs real clicks and real
// focus.
//
// Bug classes it exists to catch — every one of them renders perfectly
// and reports nothing wrong:
//
//   - a menu emitted with no name attribute at all, so two menus sit
//     open on top of each other;
//   - the nested rst-menu-group sharing its parent's group, so opening a
//     submenu closes the menu around it and the submenu vanishes with
//     it — the exact trap the partial docs warn about;
//   - light dismiss closing the menu the user is clicking inside, so
//     every menu item click misses;
//   - light dismiss bound per element, so a menu that arrived after load
//     never dismisses;
//   - shell chrome or the toggle-block swept into the group or the
//     dismiss, so the narrow-screen nav rail closes when a filter opens.
func TestMenuExclusivityAndDropdownDismissDrive(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return menuPage(t) })

	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	// The same last-step breadcrumb the select drive keeps: a bare
	// deadline names nothing.
	reached := "start"
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { reached = name; return nil })
	}
	var (
		afterAccount, afterRow, afterSubmenu string
		afterOutside, afterReopen            string
		afterInside, afterEsc                string
		focusAfterEsc                        string
		focusInSubmenu                       string
		afterSubEsc, focusAfterSubEsc        string
		tblockOpenAfter                      bool
	)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"), at("navigated"),
		chromedp.WaitVisible(`#account-summary`, chromedp.ByQuery), at("page-visible"),

		// 1. Open the header dropdown.
		chromedp.Click(`#account-summary`, chromedp.ByQuery), at("opened-account"),
		chromedp.Evaluate(openStateJS, &afterAccount),

		// 2. Open a row menu. The header dropdown must close, natively:
		//    same name, one group, no script involved.
		chromedp.Click(`#rowmenu-summary`, chromedp.ByQuery), at("opened-rowmenu"),
		chromedp.Evaluate(openStateJS, &afterRow),

		// 3. Open the filter dropdown, then the submenu inside it. The
		//    parent MUST still be open: <details name> exclusivity is
		//    document-wide, so a shared name here would have closed it.
		chromedp.Click(`#filter-summary`, chromedp.ByQuery), at("opened-filter"),
		chromedp.Click(`#submenu-summary`, chromedp.ByQuery), at("opened-submenu"),
		chromedp.Evaluate(openStateJS, &afterSubmenu),

		// 4. Click somewhere that is not a menu. Every menu closes — the
		//    nested submenu included, which the <details name> group
		//    cannot do for it and must not: dismissal is not exclusivity.
		//    The chrome strip is left alone.
		chromedp.Click(`#elsewhere`, chromedp.ByQuery), at("clicked-outside"),
		chromedp.Evaluate(openStateJS, &afterOutside),

		// 4b. Reopen the parent. The submenu must NOT come back open: a
		//     menu that remembers a state the user cannot see is the bug
		//     that made submenus join the dismiss selector in the first
		//     place.
		chromedp.Click(`#filter-summary`, chromedp.ByQuery), at("reopened-filter"),
		chromedp.Evaluate(openStateJS, &afterReopen),
		chromedp.Click(`#elsewhere`, chromedp.ByQuery), at("cleared-again"),

		// 5. Reopen, then click an item INSIDE the open menu. It must
		//    survive: a light dismiss that fires on its own menu's items
		//    makes every one of them unclickable.
		chromedp.Click(`#account-summary`, chromedp.ByQuery), at("reopened-account"),
		chromedp.Click(`#account-item`, chromedp.ByQuery), at("clicked-inside"),
		chromedp.Evaluate(openStateJS, &afterInside),

		// 6. Escape closes it and hands focus back to the summary, rather
		//    than stranding the keyboard user at the top of the document.
		chromedp.KeyEvent(kb.Escape), at("pressed-escape"),
		chromedp.Evaluate(openStateJS, &afterEsc),
		chromedp.Evaluate(`document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : "none"`, &focusAfterEsc),

		// 7. Escape with focus INSIDE an open submenu. Everything closes,
		//    and focus must land on the PARENT dropdown's summary — not
		//    the submenu's, which by then sits inside a closed parent and
		//    is not rendered, so focusing it would drop focus to <body>
		//    and strand the keyboard user at the top of the document.
		//    This is the leg that exercises the outermost-menu climb;
		//    without it that code is only ever proven by reading it.
		chromedp.Click(`#filter-summary`, chromedp.ByQuery), at("reopened-filter-for-escape"),
		chromedp.Click(`#submenu-summary`, chromedp.ByQuery), at("reopened-submenu"),
		// Focus rather than click: what this leg is about is where focus
		// goes, so it has to start from a focus this test put there on
		// purpose rather than one a click happened to leave behind.
		chromedp.Focus(`#submenu-item`, chromedp.ByQuery), at("focused-in-submenu"),
		chromedp.Evaluate(`document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : "none"`, &focusInSubmenu),
		chromedp.KeyEvent(kb.Escape), at("escape-from-submenu"),
		chromedp.Evaluate(openStateJS, &afterSubEsc),
		chromedp.Evaluate(`document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : "none"`, &focusAfterSubEsc),

		// The toggle-block is not a <details> at all, so nothing above can
		// have touched it — but it is the other thing the brief holds
		// outside the group, so read it rather than assume it.
		chromedp.Evaluate(`document.getElementById("tblock-input").checked`, &tblockOpenAfter),
	); err != nil {
		t.Fatalf("drive failed after %q: %v", reached, err)
	}

	if afterAccount != "account,chrome" {
		t.Errorf("after opening the account menu, open = %q, want %q", afterAccount, "account,chrome")
	}
	if afterRow != "rowmenu,chrome" {
		t.Errorf("after opening a row menu, open = %q, want %q — the shared <details name> group did not close the header dropdown", afterRow, "rowmenu,chrome")
	}
	if afterSubmenu != "filter,submenu,chrome" {
		t.Errorf("after opening the submenu, open = %q, want %q — a nested rst-menu-group sharing its parent's group closes the parent", afterSubmenu, "filter,submenu,chrome")
	}
	if afterOutside != "chrome" {
		t.Errorf("after a click outside, open = %q, want %q — light dismiss must close every menu, submenus included, and leave shell chrome alone", afterOutside, "chrome")
	}
	if afterReopen != "filter,chrome" {
		t.Errorf("reopening the parent gave open = %q, want %q — the submenu was left open behind its closing parent", afterReopen, "filter,chrome")
	}
	if afterInside != "account,chrome" {
		t.Errorf("after clicking an item inside the open menu, open = %q, want %q — light dismiss closed the menu being used", afterInside, "account,chrome")
	}
	if afterEsc != "chrome" {
		t.Errorf("after Escape, open = %q, want %q", afterEsc, "chrome")
	}
	if focusAfterEsc != "account-summary" {
		t.Errorf("focus after Escape is %q, want the summary that opened the menu", focusAfterEsc)
	}
	// The premise first: if focus never reached the submenu, the two
	// assertions below would pass for the wrong reason.
	if focusInSubmenu != "submenu-item" {
		t.Fatalf("focus before Escape is %q, want %q — this leg proves nothing unless focus starts inside the submenu", focusInSubmenu, "submenu-item")
	}
	if afterSubEsc != "chrome" {
		t.Errorf("after Escape from inside the submenu, open = %q, want %q", afterSubEsc, "chrome")
	}
	if focusAfterSubEsc != "filter-summary" {
		t.Errorf("focus after Escape from inside the submenu is %q, want %q — the hand-back must climb to the OUTERMOST open menu; the submenu's own summary is inside the parent that just closed, so focusing it drops focus to <body>", focusAfterSubEsc, "filter-summary")
	}
	if !tblockOpenAfter {
		t.Error("the toggle-block's switch was flipped by the menu handling; it is not a menu")
	}

	rig.Screen("body", "after the menu journey")
}

// modalPage serves the modal sample as a real page, exactly as
// Styleguide()["modal"] writes it, so the drive reads the shipped markup
// rather than a copy of it.
func modalPage(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
			`<title>modal</title><link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme.css"></head><body>`+
			Styleguide()["modal"]+`</body></html>`)
	})
	return mux
}

// TestModalDialogPanelDrive proves the <dialog open> panel is a rendered
// dialog and not a modal one, and that the scoped resets in tokens.css
// actually undo the UA dialog block.
//
// Bug classes it exists to catch — all of which look fine in the markup:
//
//   - the UA dialog block left in place, so the panel is absolutely
//     positioned with 1em of padding and sits wherever the viewport puts
//     it instead of centred in the overlay;
//   - the panel reaching the top layer (a showModal() creeping in), which
//     would put it above the scrim's stacking context, paint a ::backdrop
//     nobody styled, and trap Escape;
//   - the dialog rendering with Canvas colours instead of the theme's,
//     which reads as "the panel ignores the theme".
func TestModalDialogPanelDrive(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return modalPage(t) })

	ctx, cancel := context.WithTimeout(rig.Context(), 180*time.Second)
	defer cancel()

	var (
		tag, position, padding string
		isOpen, inTopLayer     bool
		centred, insideOverlay bool
		backdropInert          bool
		labelledBy, labelText  string
	)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.Origin+"/"),
		chromedp.WaitVisible(`.rst-modal-panel`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector(".rst-modal-panel").tagName`, &tag),
		chromedp.Evaluate(`document.querySelector(".rst-modal-panel").open === true`, &isOpen),
		// matches(":modal") is true only for a dialog in the top layer,
		// which is what showModal() does and a rendered-open one never
		// does. This is the assertion that the zero-JS promise holds.
		chromedp.Evaluate(`document.querySelector(".rst-modal-panel").matches(":modal")`, &inTopLayer),
		chromedp.Evaluate(`getComputedStyle(document.querySelector(".rst-modal-panel")).position`, &position),
		chromedp.Evaluate(`getComputedStyle(document.querySelector(".rst-modal-panel")).paddingTop`, &padding),
		chromedp.Evaluate(`document.querySelector(".rst-modal-overlay").contains(document.querySelector(".rst-modal-panel"))`, &insideOverlay),
		// Centred inside the overlay's flex box, within a pixel: the UA
		// block's absolute positioning and auto margins would put it
		// somewhere else entirely.
		chromedp.Evaluate(`(function () {
			var o = document.querySelector(".rst-modal-overlay").getBoundingClientRect();
			var p = document.querySelector(".rst-modal-panel").getBoundingClientRect();
			return Math.abs((o.left + o.right) / 2 - (p.left + p.right) / 2) < 2;
		})()`, &centred),
		chromedp.Evaluate(`document.querySelector(".rst-backdrop").hasAttribute("inert")`, &backdropInert),
		// The dialog role has to be named. Resolve aria-labelledby the way
		// an AT would — follow the id to the element and read its text —
		// rather than trusting the attribute is pointing at anything.
		chromedp.Evaluate(`document.querySelector(".rst-modal-panel").getAttribute("aria-labelledby") ?? ""`, &labelledBy),
		chromedp.Evaluate(`(function () {
			var id = document.querySelector(".rst-modal-panel").getAttribute("aria-labelledby");
			var el = id ? document.getElementById(id) : null;
			return el ? el.textContent.trim() : "";
		})()`, &labelText),
	); err != nil {
		t.Fatalf("modal drive failed: %v", err)
	}

	if tag != "DIALOG" {
		t.Errorf("the modal panel is a <%s>, want <dialog>", strings.ToLower(tag))
	}
	if !isOpen {
		t.Error("the dialog is not open; a modal route renders it already open, since nothing here can open it")
	}
	if inTopLayer {
		t.Error("the panel is in the top layer: something called showModal(), and the idiom is zero-JS")
	}
	if position != "static" {
		t.Errorf("the panel's position is %q, want %q — the UA dialog block was not reset", position, "static")
	}
	if padding != "0px" {
		t.Errorf("the panel's padding-top is %q, want %q — the UA dialog block's 1em survived", padding, "0px")
	}
	if !insideOverlay {
		t.Error("the panel is no longer inside the overlay div, which is the scrim")
	}
	if !centred {
		t.Error("the panel is not centred in the overlay; the UA dialog block is still positioning it")
	}
	if !backdropInert {
		t.Error("the page behind the panel lost its inert attribute")
	}
	if labelledBy == "" {
		t.Error("the dialog has no aria-labelledby: a dialog role with no name is announced as \"dialog\" and nothing else")
	}
	// Resolved through the id, not merely present: an aria-labelledby
	// pointing at an id that is not in the document names nothing at all,
	// and looks entirely correct in the markup.
	if labelText == "" {
		t.Errorf("aria-labelledby=%q resolves to no text; the dialog's accessible name is empty", labelledBy)
	}

	rig.Screen(".rst-modal-panel", "the open modal")
}

// fieldRowPage serves one page holding every layout this batch's
// geometry bugs lived in, in whichever writing mode the query string
// asks for: a date range whose end carries an error (the row that
// misaligned and dropped its message across its neighbour), a
// grow/short pair (the row whose grown input ran under the short
// field's label), a pair where BOTH halves carry an error, and a row
// mixing a labelled field with an unlabelled one (the reserved label
// line). Everything goes through the real partials, the real stylesheet
// and the real enhancement, because every one of the bugs was in the
// interaction between them.
//
// dir rides the query string rather than a second handler: the two
// passes must differ in exactly one attribute, or an RTL failure is not
// evidence about direction.
func fieldRowPage(t *testing.T) http.Handler {
	t.Helper()
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /datetime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(DatetimeJS())
	})
	mux.HandleFunc("GET /tokens.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write(TokensCSS())
	})
	mux.HandleFunc("GET /theme.css", func(w http.ResponseWriter, r *http.Request) {
		css, ok := ThemeCSS(ThemeNames()[0])
		if !ok {
			http.Error(w, "no theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Write(css)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		dir := "ltr"
		if r.URL.Query().Get("dir") == "rtl" {
			dir = "rtl"
		}
		var body strings.Builder
		fmt.Fprintf(&body, `<!doctype html><html lang="en" dir=%q><head><meta charset="utf-8">`+
			`<title>rows</title><link rel="stylesheet" href="/tokens.css">`+
			`<link rel="stylesheet" href="/theme.css">`+
			`<script defer src="/datetime.js"></script></head><body class="rst-page">`+
			`<form class="rst-form" method="post" action="/submit">`, dir)
		if err := tmpl.ExecuteTemplate(&body, "field-daterange", map[string]any{
			"Legend": "Booking", "Kind": "date",
			"Start": map[string]any{"Name": "dr_from", "Label": "From", "Value": "2026-09-04"},
			"End": map[string]any{"Name": "dr_to", "Label": "To", "Value": "2026-09-01",
				"Error": "The end comes before the start, so this booking cannot be made."},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body.WriteString(`<div class="rst-field-row" id="addr">` +
			`<div class="rst-field rst-grow"><label class="rst-field__label" for="city">City</label>` +
			`<input class="rst-input" type="text" id="city" name="city" value="Dublin"></div>` +
			`<div class="rst-field"><label class="rst-field__label" for="zip">ZIP</label>` +
			`<input class="rst-input rst-input--short" type="text" id="zip" name="zip" value="D02 XY45"></div>` +
			`</div>`)
		// Both halves in error, one message far longer than the other:
		// the case where a message, left to contribute to its field's
		// max-content width, stretched its own column and squeezed its
		// neighbour.
		body.WriteString(`<div class="rst-field-row" id="both">` +
			`<div class="rst-field"><label class="rst-field__label" for="b_one">Opens</label>` +
			`<input class="rst-input" type="text" id="b_one" name="b_one" aria-invalid="true" aria-describedby="b_one-error">` +
			`<small class="rst-field__error" id="b_one-error">This start is in the past and the booking window closed on Tuesday, so pick another one.</small></div>` +
			`<div class="rst-field"><label class="rst-field__label" for="b_two">Closes</label>` +
			`<input class="rst-input" type="text" id="b_two" name="b_two" aria-invalid="true" aria-describedby="b_two-error">` +
			`<small class="rst-field__error" id="b_two-error">Too late.</small></div>` +
			`</div>`)
		// A labelled field beside one with no label at all: the reserved
		// label line is the only thing keeping the two controls level.
		body.WriteString(`<div class="rst-field-row" id="nolabel">` +
			`<div class="rst-field"><label class="rst-field__label" for="n_one">Reference</label>` +
			`<input class="rst-input" type="text" id="n_one" name="n_one"></div>` +
			`<div class="rst-field"><input class="rst-input" type="text" id="n_two" name="n_two" aria-label="Check digit"></div>` +
			`</div>`)
		body.WriteString(`<button type="submit" id="go">Save</button></form></body></html>`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, body.String())
	})
	return mux
}

// geometry is one element's border box, as the browser measured it.
type geometry struct {
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Width  float64 `json:"width"`
}

// rowGeometry is everything the assertions below need, read in one
// evaluation so every rect comes from the same layout.
type rowGeometry struct {
	FromControl geometry `json:"fromControl"`
	ToControl   geometry `json:"toControl"`
	FromField   geometry `json:"fromField"`
	ToField     geometry `json:"toField"`
	Error       geometry `json:"error"`
	FromPick    geometry `json:"fromPick"`
	City        geometry `json:"city"`
	CityField   geometry `json:"cityField"`
	Zip         geometry `json:"zip"`
	ZipField    geometry `json:"zipField"`
	BothOne     geometry `json:"bothOne"`
	BothTwo     geometry `json:"bothTwo"`
	BothOneMsg  geometry `json:"bothOneMsg"`
	BothTwoMsg  geometry `json:"bothTwoMsg"`
	BothOneFld  geometry `json:"bothOneFld"`
	BothTwoFld  geometry `json:"bothTwoFld"`
	Labelled    geometry `json:"labelled"`
	Unlabelled  geometry `json:"unlabelled"`
	Dir         string   `json:"dir"`
	RootFont    float64  `json:"rootFont"`
}

const rowGeometryJS = `(function () {
  function box(el) {
    var r = el.getBoundingClientRect();
    return {top: r.top, left: r.left, right: r.right, bottom: r.bottom, width: r.width};
  }
  function byId(id) { return box(document.getElementById(id)); }
  // The control a person sees: the enhancement's combobox where it ran,
  // the native input where it did not.
  function control(name) {
    var native = document.getElementsByName(name)[0];
    var wrap = native.closest(".rst-dtp");
    return wrap ? wrap.querySelector(".rst-dtp__input") : native;
  }
  function field(name) { return document.getElementsByName(name)[0].closest(".rst-field"); }
  var from = control("dr_from"), to = control("dr_to");
  return {
    fromControl: box(from), toControl: box(to),
    fromField: box(field("dr_from")), toField: box(field("dr_to")),
    error: byId("dr_to-error"),
    fromPick: box(from.closest(".rst-dtp").querySelector(".rst-dtp__pick")),
    city: byId("city"), cityField: box(field("city")),
    zip: byId("zip"), zipField: box(field("zip")),
    bothOne: byId("b_one"), bothTwo: byId("b_two"),
    bothOneMsg: byId("b_one-error"), bothTwoMsg: byId("b_two-error"),
    bothOneFld: box(field("b_one")), bothTwoFld: box(field("b_two")),
    labelled: byId("n_one"), unlabelled: byId("n_two"),
    dir: document.documentElement.getAttribute("dir") || "ltr",
    rootFont: parseFloat(getComputedStyle(document.documentElement).fontSize)
  };
})()`

// One device pixel of slack: sub-pixel layout is legitimate, a
// misaligned row is not.
const geometrySlack = 1.0

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// overlaps reports whether two border boxes intersect by more than the
// slack — the literal shape of the bug from the screenshot, a message
// printed across the control beside it.
func overlaps(a, b geometry) bool {
	return a.Left < b.Right-geometrySlack && a.Right > b.Left+geometrySlack &&
		a.Top < b.Bottom-geometrySlack && a.Bottom > b.Top+geometrySlack
}

// within reports whether the inner box stays inside the outer one on the
// inline axis.
func within(inner, outer geometry) bool {
	return inner.Left >= outer.Left-geometrySlack && inner.Right <= outer.Right+geometrySlack
}

// assertRowGeometry is every invariant .rst-field-row now owes, checked
// against one layout. It is called once per writing mode, because the
// picker button's placement and the row's own inline axis are the two
// things a logical-property mistake gets wrong in exactly one of them.
func assertRowGeometry(t *testing.T, g rowGeometry) {
	t.Helper()
	rtl := g.Dir == "rtl"

	// 1. The row aligns by the control, whatever else the fields carry.
	for _, pair := range []struct {
		what string
		a, b geometry
	}{
		{"the date range (error on the end)", g.FromControl, g.ToControl},
		{"the pair with an error on both halves", g.BothOne, g.BothTwo},
		{"a labelled field beside an unlabelled one", g.Labelled, g.Unlabelled},
	} {
		if d := absf(pair.a.Top - pair.b.Top); d > geometrySlack {
			t.Errorf("%s: the controls do not share a top edge, %.1f apart (%.1f and %.1f) — the row is aligning by something other than the control",
				pair.what, d, pair.a.Top, pair.b.Top)
		}
	}

	// 2. A message stays in its own column and off its neighbour.
	for _, m := range []struct {
		what                  string
		msg, neighbour, field geometry
	}{
		{"the To field's error", g.Error, g.FromControl, g.ToField},
		{"the long error on the first half", g.BothOneMsg, g.BothTwo, g.BothOneFld},
		{"the short error on the second half", g.BothTwoMsg, g.BothOne, g.BothTwoFld},
	} {
		if overlaps(m.msg, m.neighbour) {
			t.Errorf("%s overlaps the control beside it: message %+v, control %+v", m.what, m.msg, m.neighbour)
		}
		if !within(m.msg, m.field) {
			t.Errorf("%s escapes its own column: message [%.1f, %.1f], field [%.1f, %.1f]",
				m.what, m.msg.Left, m.msg.Right, m.field.Left, m.field.Right)
		}
	}
	// A long message must not have bought its column extra width at its
	// neighbour's expense — that is what contain:inline-size is for.
	if d := absf(g.BothOneFld.Width - g.BothTwoFld.Width); d > geometrySlack {
		t.Errorf("a long error stretched its own column: fields are %.1f and %.1f wide, %.1f apart — the message is being counted in the field's max-content width",
			g.BothOneFld.Width, g.BothTwoFld.Width, d)
	}

	// 3. No control outgrows the field that holds it — the box-sizing
	// bug, stated as the invariant it broke.
	for _, c := range []struct {
		name           string
		control, field geometry
	}{
		{"From", g.FromControl, g.FromField},
		{"To", g.ToControl, g.ToField},
		{"City", g.City, g.CityField},
		{"ZIP", g.Zip, g.ZipField},
	} {
		if !within(c.control, c.field) {
			t.Errorf("the %s control overflows its field: control [%.1f, %.1f], field [%.1f, %.1f] — width:100%% is being measured as a content box",
				c.name, c.control.Left, c.control.Right, c.field.Left, c.field.Right)
		}
	}
	if overlaps(g.City, g.ZipField) {
		t.Errorf("the grown City control runs into the ZIP field: City [%.1f, %.1f], ZIP field [%.1f, %.1f]",
			g.City.Left, g.City.Right, g.ZipField.Left, g.ZipField.Right)
	}

	// 4. The picker sits at the control's INLINE end, inside it, centred
	// on it. Which physical edge that is flips with the writing mode,
	// which is the whole point of running this twice.
	inset := 0.35 * g.RootFont
	gotEdge, wantEdge, edge := g.FromPick.Right, g.FromControl.Right-inset, "right"
	if rtl {
		gotEdge, wantEdge, edge = g.FromPick.Left, g.FromControl.Left+inset, "left"
	}
	if absf(gotEdge-wantEdge) > 2 {
		t.Errorf("dir=%s: the picker button is not at the control's inline end: button %s edge %.1f, wanted about %.1f (control spans [%.1f, %.1f])",
			g.Dir, edge, gotEdge, wantEdge, g.FromControl.Left, g.FromControl.Right)
	}
	if !within(g.FromPick, g.FromControl) || g.FromPick.Top < g.FromControl.Top || g.FromPick.Bottom > g.FromControl.Bottom {
		t.Errorf("dir=%s: the picker button is not inside the control it belongs to: button %+v, control %+v", g.Dir, g.FromPick, g.FromControl)
	}
	if top, bottom := g.FromPick.Top-g.FromControl.Top, g.FromControl.Bottom-g.FromPick.Bottom; absf(top-bottom) > geometrySlack {
		t.Errorf("dir=%s: the picker button is not centred on the control: %.1f above, %.1f below", g.Dir, top, bottom)
	}

	// 5. Short is compact, not crushed: 8rem is what tokens.css declares,
	// and the point of declaring it is that a row cannot take it away.
	if want := 8 * g.RootFont; g.Zip.Width < want-geometrySlack {
		t.Errorf("the short field lost its width in the row: %.1f, wanted at least %.1f", g.Zip.Width, want)
	}
	if g.City.Width <= g.Zip.Width {
		t.Errorf("rst-grow did not grow: City %.1f, ZIP %.1f", g.City.Width, g.Zip.Width)
	}
}

// TestFieldRowGeometryHoldsUnderAnError pins the layout contract of
// .rst-field-row with a real engine doing the layout, because none of
// these bugs is visible in the markup — every one of them is a used
// value the browser computed.
//
// The bug classes it exists to catch, all four shipped at once and all
// four looked fine in the template:
//
//   - the row bottom-aligned its fields, so a field carrying an error
//     lifted its control clear of its neighbour's and dropped the error
//     line across the field beside it;
//   - .rst-input sized width:100% as a content box, so every input
//     overflowed its own column by its padding and borders and ran
//     under the field next to it;
//   - the picker button, anchored to a wrapper the input had outgrown,
//     landed short of the control's visible end — inside the text, not
//     at the edge;
//   - the short field shrank to whatever was left over.
//
// And the three mechanisms the fix introduced, which are load-bearing
// and would otherwise be proven by nothing: the reserved label line
// under a field with no label, contain:inline-size keeping a long
// message out of its column's width, and the picker's placement in a
// right-to-left page — the one thing a logical-property mistake gets
// wrong in exactly one writing mode.
//
// Cheap by browser-drive standards: two navigations, one evaluation
// each, no keyboard input, so it does not share the keystroke
// flakiness the two journey drives above document.
func TestFieldRowGeometryHoldsUnderAnError(t *testing.T) {
	rig := harness.New(t, func(string) http.Handler { return fieldRowPage(t) })

	ctx, cancelTimeout := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancelTimeout()

	for _, dir := range []string{"ltr", "rtl"} {
		t.Run(dir, func(t *testing.T) {
			var g rowGeometry
			if err := chromedp.Run(ctx,
				chromedp.Navigate(rig.Origin+"/?dir="+dir),
				chromedp.WaitVisible(".rst-dtp__pick", chromedp.ByQuery),
				chromedp.Evaluate(rowGeometryJS, &g),
			); err != nil {
				t.Fatalf("dir=%s: reading the row's geometry: %v", dir, err)
			}
			if g.Dir != dir {
				t.Fatalf("asked for dir=%s, the document reports %q — the two passes are not differing in what they claim to", dir, g.Dir)
			}
			assertRowGeometry(t, g)
		})
	}

	rig.Screen("body", "the field rows")
}
