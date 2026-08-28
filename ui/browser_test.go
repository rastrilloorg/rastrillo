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
