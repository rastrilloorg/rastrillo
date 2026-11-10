package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// The shim has no browser harness for polling: JS behavior is verified
// by the notes example's no-JS end-to-end path, and for how polling
// starts by shim_node.mjs in Node. What a Go test can hold honest is
// the contract the docs promise: the vocabulary the file answers to,
// its inert-by-default IIFE shape, and the absence of anything a CSP
// would reject.
func TestShimContract(t *testing.T) {
	js := string(ShimJS())
	for _, want := range []string{
		"data-poll", "data-poll-every", "data-poll-push",
		"EventSource",
		"Rastrillo-Fragment", "Rastrillo-Location",
		// Behavior a Go test can still hold cheaply: the terminal
		// statuses that end a poll, the local-path guard on the
		// header-driven navigation.
		"403", "404", "localPath",
		// The local-path guard must reject control characters —
		// browsers strip tab/CR/LF before parsing, so "/\t/evil"
		// resolves scheme-relative — mirroring sessions.SafeReturn.
		"\\u0000-\\u001f\\u007f",
		// fiesta.js applies a scheme choice in place and posts nothing,
		// so the cookie /_theme would have set is written here, from
		// Fiesta's rst-scheme event, with the attributes scheme.go
		// gives it: the same name and age, or a reload paints the
		// scheme the reader just left.
		`addEventListener("rst-scheme"`,
		`rastrillo_scheme=`, "Max-Age=31536000", "Max-Age=0",
		"Path=/; SameSite=Lax", "; Secure",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("shim does not mention %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.SplitN(js, "\n(function", 2)[0]), "/*") {
		t.Error("shim should open with its contract comment")
	}
	if !strings.Contains(js, "(function () {") || !strings.Contains(js, "})();") {
		t.Error("shim should be a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("shim must stay CSP-clean")
	}
}

// Fiesta's fiesta.js answers both the menus and the scheme switch, so
// rastrillo.js answering them too would run every menu's dismissal and
// every switch's drag twice; Fiesta's header calls the pair
// unsupported. What is left is polling and the scheme cookie.
func TestRastrilloJSHoldsOnlyTheShim(t *testing.T) {
	js := string(ShimJS())
	for _, gone := range []struct{ code, duty string }{
		{"dismissMenus", "menu dismiss"},
		{`addEventListener("keydown"`, "menu dismiss"},
		{`addEventListener("pointerdown"`, "switch drag"},
		{`addEventListener("touchstart"`, "touch press"},
		{`addEventListener("submit"`, "the scheme menu's apply"},
		{"data-theme", "the scheme menu's apply"},
		{"prefers-color-scheme", "the OS-scheme listener"},
	} {
		if strings.Contains(js, gone.code) {
			t.Errorf("rastrillo.js still has %s (%s), which fiesta.js "+
				"does", gone.duty, gone.code)
		}
	}
	if !strings.Contains(js, "function poll(") {
		t.Error("rastrillo.js no longer polls")
	}
}

// fiesta.js holds what rastrillo.js held before it was cut to the shim:
// light dismiss, the switch's drag, the touch listener and the scheme
// menu's apply. These are the parts of that contract a Rastrillo page
// relies on.
func TestFiestaJSKeepsTheOldShimsContract(t *testing.T) {
	js := string(fiestaScript("fiesta.js"))
	for _, want := range []string{
		// Light dismiss: the menu classes it answers to — the nested
		// rst-menu-group among them, so a submenu is never left open
		// behind its closing parent — the containment test that keeps
		// the menu being used open, the Escape key, and the focus
		// hand-back to the summary.
		"rst-dropdown", "rst-menu-group", "rst-row-menu",
		"closeMenus", "contains", "Escape", "summary.focus()",
		// Both spellings, for an app that has not run `rastrillo
		// markup` yet.
		`MENUS.replace(`,
		// The topbar's and console's narrow Menu, in both spellings
		// written out, scoped to a topbar or a console that names its
		// view, with its tail counted as inside it only while its
		// summary is rendered.
		`[rst-shell-topbar] [rst-shell-menu][open],[rst-shell-console~=page] [rst-shell-menu][open],[rst-shell-console~=index] [rst-shell-menu][open]`,
		`.rst-shell-topbar .rst-shell__menu[open],.rst-shell-console--page .rst-shell__menu[open],.rst-shell-console--index .rst-shell__menu[open]`,
		`TAIL = "[rst-shell-tail],.rst-shell__tail"`, "menuAround", "getClientRects().length",
		// The switch's drag, on a switch and on a toggle block's head.
		`TRACK = "[rst-switch-track],.rst-switch__track"`,
		"[rst-tblock-head],.rst-tblock__head",
		// The colour-scheme switch: the menu in both spellings, the
		// attribute the themes read, and the event rastrillo.js writes
		// the cookie from.
		`"[rst-scheme],.rst-scheme"`, "data-theme",
		`new CustomEvent("rst-scheme", { bubbles: true, ` +
			`detail: { scheme: s } })`,
		// The iOS touch listener: :active lands on a tap only when a
		// touchstart listener exists.
		`addEventListener("touchstart", function () {}, { passive: true })`,
		// The OS flipping under a page on System lands at once.
		"osScheme.onchange",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("fiesta.js does not mention %q", want)
		}
	}
	// Light dismiss is delegated, not bound per element: a menu that
	// arrives inside a polled fragment has to be covered the moment it
	// lands. Three mentions: the definition and the two listeners.
	if !strings.Contains(js, `document.addEventListener("click", dismissMenus, true)`) ||
		!strings.Contains(js, `document.addEventListener("keydown", dismissMenus, true)`) {
		t.Error("light dismiss is not two delegated document listeners")
	}
	if mentions := strings.Count(js, "dismissMenus"); mentions != 3 {
		t.Errorf("dismissMenus appears %d times, want 3 (one "+
			"definition, two document listeners)", mentions)
	}
	// The old sidebar drawer and the toggle-block stay out of it:
	// neither is a menu.
	reach, err := lightDismissReach(js)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range reach {
		t.Errorf("light dismiss reaches for %q; it covers menus only", bad)
	}
}

// lightDismissReach returns which of the names light dismiss must never
// touch appear in its code, which runs from the menu list, MENUS, with
// its extensions, through menuAround and closeMenus to the two document
// listeners; an error when that code is not where it expects.
func lightDismissReach(js string) ([]string, error) {
	from, to := strings.Index(js, "var MENUS"), strings.Index(js, `document.addEventListener("keydown", dismissMenus, true)`)
	if from < 0 || to < from {
		return nil, fmt.Errorf("the light-dismiss code is not where this check expects it")
	}
	var out []string
	for _, bad := range []string{"rst-shell-chrome", "rst-tblock"} {
		if strings.Contains(js[from:to], bad) {
			out = append(out, bad)
		}
	}
	return out, nil
}

// TestTheLightDismissGateReadsTheMenuList (switch review 5): the gate
// has to read the menu list, not only the functions after it. A drawer
// or a toggle block spliced into MENUS makes closeMenus(null) close it,
// and the functions' own text would never say so. The shipped fiesta.js
// is the control.
func TestTheLightDismissGateReadsTheMenuList(t *testing.T) {
	js := string(fiestaScript("fiesta.js"))
	if reach, err := lightDismissReach(js); err != nil || len(reach) != 0 {
		t.Fatalf("the shipped fiesta.js: reach %v, err %v; want none",
			reach, err)
	}
	for _, m := range []struct{ name, bad string }{{"the drawer spliced into MENUS", "rst-shell-chrome"}, {"the toggle block spliced into MENUS", "rst-tblock"}} {
		mutant := strings.Replace(js, `var MENUS = "`, `var MENUS = "[`+m.bad+`][open],`, 1)
		if mutant == js {
			t.Fatal("MENUS is not where this test expects it")
		}
		reach, err := lightDismissReach(mutant)
		if err != nil {
			t.Fatal(err)
		}
		if len(reach) != 1 || reach[0] != m.bad {
			t.Errorf("%s: the gate reports %v; want %q, which closeMenus(null) would now close", m.name, reach, m.bad)
		}
	}
}

// The busy rule is a DEFAULT, and this is the assertion that says so in
// Go rather than in a browser: one delegated submit listener on the
// document, no scan over forms carrying an attribute, and the only
// thing data-busy can now say is "false".
//
// The bug this catches is a quiet regression, not a crash: someone
// restores the old `document.querySelectorAll("form[data-busy]")` scan,
// every drive that plants data-busy on its form keeps passing, and every
// app that never wrote the attribute silently loses the rule.
func TestBusyRuleIsTheDefault(t *testing.T) {
	js := string(BusyJS())
	if !strings.Contains(js, `document.addEventListener("submit", busySubmit, true)`) {
		t.Error("the busy rule is not one delegated capture-phase submit listener on the document")
	}
	// Three mentions and no more: the definition, the one listener, and
	// the window listener's late call for a swapping form live.js left
	// to the browser (armed there because only then is it known whether
	// live.js took it). A fourth would mean someone started binding it
	// per form again.
	if n := strings.Count(js, "busySubmit"); n != 3 {
		t.Errorf("busySubmit appears %d times, want 3 (one definition, one document listener, one late call for a data-live-form)", n)
	}
	if !strings.Contains(js, "busySubmit(e, true)") {
		t.Error("busy.js no longer arms a swapping form live.js left to the browser")
	}
	// The opt-in scan is gone. Any querySelectorAll over a data-busy
	// selector is the old shape coming back.
	for _, bad := range []string{`"form[data-busy]"`, `"[data-busy]"`, "busyForm"} {
		if strings.Contains(js, bad) {
			t.Errorf("busy.js scans for %s; the busy rule is on by default, not opted into", bad)
		}
	}
	// data-busy survives only as an opt-out, on the form and on the
	// button. Both readings have to be there: a rule you can only turn
	// off for a whole form is not the rule the docs describe.
	if n := strings.Count(js, `getAttribute("data-busy") === "false"`); n != 2 {
		t.Errorf(`data-busy=="false" is tested %d times, want 2 (the form's opt-out and the button's)`, n)
	}
	// A form is [LegacyOverrideBuiltIns], so a control named "target"
	// shadows form.target with the input element — truthy, not "_self",
	// and the property form would silently switch the rule off for that
	// form. Every attribute the handler reads goes through getAttribute,
	// and the guard is the form's own aria-busy rather than an expando a
	// control could shadow (or, under strict mode, throw on).
	// Comments stripped first, or this gate would fire on the very
	// sentence in the shim that explains why the property read is wrong.
	code := js[strings.Index(js, "(function () {"):]
	var b strings.Builder
	for _, line := range strings.Split(code, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	code = b.String()
	for _, bad := range []string{"form.target", "form.method", ".formMethod", "form.rstBusy", ".rstBusy ="} {
		if strings.Contains(code, bad) {
			t.Errorf("the shim reads %s as a property; a control of that name shadows it — switching the rule off, or, for an assignment under strict mode, throwing", bad)
		}
	}
	if !strings.Contains(js, `form.getAttribute("target")`) {
		t.Error("the shim does not read the form's target through getAttribute")
	}
	if !strings.Contains(js, `if (form.getAttribute("aria-busy") === "true") { e.preventDefault(); return; }`) {
		t.Error("the double-submit guard is not the form's own aria-busy attribute")
	}
	// A submit that goes NOWHERE must not be guarded, and there are
	// three shapes of it. target="_blank" leaves this page where it is;
	// so does <form method="dialog">, the standard close button inside a
	// <dialog>; and so does <button formmethod="dialog"> in an ordinary
	// form, the same close reached through the submitter's own
	// attribute. The dialog shapes are the worse: nothing ever navigates
	// or reloads afterwards, so a guard armed there is never cleared —
	// the close button ends up disabled and the dialog cannot be closed
	// through that form again, ever.
	//
	// One effective method, computed once, with the submitter's
	// formmethod beating the form's method the way HTML says it does.
	// Both read through getAttribute — never form.method or
	// btn.formMethod — for the [LegacyOverrideBuiltIns] reason above,
	// and matched case-insensitively because method is an enumerated
	// attribute.
	for _, want := range []string{
		`var method = btn && btn.getAttribute("formmethod") || ` +
			`form.getAttribute("method");`,
		`if (/^dialog$/i.test(method)) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf(`the busy rule does not bail out of an effective method of "dialog" (%s missing); a dialog's close form — or a button's formmethod="dialog" — arms a guard nothing will ever clear`, want)
		}
	}
	// The submitter has to be in hand BEFORE the guard runs, or the
	// effective method cannot see it and the bail reads the form's
	// method alone.
	if strings.Index(js, "    var btn = e.submitter;") > strings.Index(js, `    if (/^dialog$/i.test(`) {
		t.Error("the submitter is read after the dialog bail; the bail cannot see formmethod")
	}
	// The reset has to reach a submit button that belongs to the form
	// through form="id" while living somewhere else in the document — a
	// sticky header's Save. busySubmit busies it (it is the submit
	// event's submitter, wherever it sits), so a reset that swept the
	// form's own subtree left it disabled and spinning after every
	// bfcache restore, for good. Sweep the document and test ownership.
	if strings.Contains(js, `form.querySelectorAll('[aria-busy="true"]')`) {
		t.Error(`busyOff sweeps the form's descendants; a submit button associated through form="id" is not one, and never comes back`)
	}
	if !strings.Contains(js, `if (b.form !== form && !form.contains(b)) return;`) {
		t.Error("busyOff does not test ownership, so it would clear busy state belonging to another form")
	}
	// The payload trap, pinned where a reader will trip over it: the
	// two mutations that can change what the server receives — disabled
	// and an <input type="submit">'s value — happen inside the deferred
	// callback, never in the synchronous part of the handler.
	sync, deferred, ok := strings.Cut(js, "    setTimeout(function () {")
	if !ok {
		t.Fatal("the busy handler no longer defers anything; disabled must not be set during the submit event")
	}
	sync = sync[strings.Index(sync, "function busySubmit"):]
	for _, bad := range []string{"btn.disabled", "btn.value ="} {
		if strings.Contains(sync, bad) {
			t.Errorf("%s runs synchronously in the submit handler; it drops the button's name/value from the payload", bad)
		}
		if !strings.Contains(deferred, bad) {
			t.Errorf("%s is not in the deferred callback; the busy state never hardens", bad)
		}
	}
}

// 8 KiB, the cap before light dismiss, the busy rule, the switch and
// the scheme menu moved in; they live in Fiesta's busy.js and fiesta.js
// now, and the shim is back to polling and the scheme cookie.
func TestShimIsSmall(t *testing.T) {
	if n := len(ShimJS()); n > 8*1024 {
		t.Fatalf("shim is %d bytes; the point is that an app owner can read it in one sitting — trim it", n)
	}
	if bytes.Contains(ShimJS(), []byte("\t")) {
		t.Error("shim uses two-space indentation, not tabs")
	}
}

// select.js holds to the same contract as the shim: the attributes the
// field partials write, an ARIA combobox, and a native select that
// survives enhancement.
func TestSelectContract(t *testing.T) {
	js := string(SelectJS())
	for _, want := range []string{
		"data-rst-select", "data-rst-select-filter",
		"data-rst-select-results", "data-rst-select-result-one",
		// The convention, pinned: an ARIA combobox that mirrors rather
		// than replaces, announced to assistive tech.
		"combobox", "aria-activedescendant", "aria-expanded", "aria-live",
		"rst-sr-only",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("select.js does not mention %q", want)
		}
	}
	// The whole point: the native control survives enhancement, because
	// it is what the form submits. The one thing select.js does remove is
	// a row it drew itself (a page's extra suggestions, replaced), so the
	// fence is on the native select and the markup around it.
	for _, bad := range []string{"native.remove()", "removeChild(native", "outerHTML =", "innerHTML ="} {
		if strings.Contains(js, bad) {
			t.Errorf("select.js destroys DOM (%q); the native select must survive", bad)
		}
	}
	// Inert by default, and re-scannable.
	if !strings.Contains(js, "rstEnhanced") {
		t.Error("select.js is not idempotent; re-scanning would double-enhance")
	}
	// The markup-side opt-out, and the groups.
	if !strings.Contains(js, `!== "false"`) {
		t.Error(`select.js does not honour data-rst-select="false"; a hand-written select cannot opt out`)
	}
	for _, want := range []string{"OPTGROUP", `"group"`, "rst-select-group"} {
		if !strings.Contains(js, want) {
			t.Errorf("select.js does not mention %q; a grouped select would be flattened", want)
		}
	}
	if bytes.Contains(SelectJS(), []byte("\t")) {
		t.Error("select.js uses two-space indentation, not tabs")
	}
}

// No scaffolded script may reach off-origin: every one is vendored,
// first-party and dependency-free.
func TestScriptsAreSelfContained(t *testing.T) {
	assets, _ := VendoredAssets("day")
	for name, js := range assets {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		for _, bad := range []string{"http://", "https://", "import ", "require(", "//cdn"} {
			if strings.Contains(string(js), bad) {
				t.Errorf("%s reaches outside the page (%q)", name, bad)
			}
		}
	}
}

// anchor.js is the one script an app is told to delete in time, so its
// contract is that it is inert where it is not needed: the feature test
// returns before any listener is added, and the class it writes is the
// one tokens.css opens a menu from.
func TestAnchorContract(t *testing.T) {
	js := string(AnchorJS())
	test, listen := strings.Index(js, `CSS.supports("position-area: block-end")`), strings.Index(js, "addEventListener")
	if test < 0 || listen < 0 || test > listen {
		t.Error("anchor.js must test for anchor positioning, and return, before it listens for anything")
	}
	if !strings.Contains(js, "is-flip-inline") || !strings.Contains(string(TokensCSS()), "[rst-dropdown].is-flip-inline > [rst-dropdown-menu]") {
		t.Error("anchor.js and tokens.css no longer agree on .is-flip-inline")
	}
	if strings.Contains(js, "\t") {
		t.Error("anchor.js uses two-space indentation, not tabs")
	}
}

// busy.js was split out of the shim when the hold was added (the shim
// sat 20 bytes under its cap). Its contract, held the same way: the
// vocabulary, the spinner and submitter, the cancelled-submit hand-back,
// the hold (its duration, the window listener that runs last, the
// re-submit that keeps the submitter), and Back in both directions.
func TestBusyContract(t *testing.T) {
	js := string(BusyJS())
	for _, want := range []string{
		"data-busy", "data-busy-label",
		`spin.setAttribute("rst-spin", "")`, "e.submitter", "defaultPrevented",
		"HOLD_MS = 650", `window.addEventListener("submit"`, "requestSubmit(btn || undefined)",
		"held.has(e)", "form === releasing", "checkValidity()",
		"pagehide", "clearTimeout", "pageshow", "e.persisted",
		// Review fixes: an opted-out button is never left disabled, and a
		// submitter that left the form during the hold hands the form back.
		"btn.disabled = wasDisabled", "btn.form !== form", `getAttribute("formtarget")`,
		// Only a submit with a spinner showing is held.
		`btn.getAttribute("aria-busy") !== "true") return;`,
		// The handover: a swapping form is live.js's when live.js sends it.
		`form.hasAttribute("data-live-form") && !late`,
		// ...and the late arm leaves one live.js claimed (e.liveForm),
		// in either script order.
		`e.defaultPrevented || e.liveForm) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("busy.js does not mention %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(js), "/*") || !strings.Contains(js, "(function () {") || !strings.HasSuffix(strings.TrimSpace(js), "})();") {
		t.Error("busy.js should be its contract comment and a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") || strings.Contains(js, "\t") {
		t.Error("busy.js must stay CSP-clean and use two-space indentation")
	}
}

// shell.js holds to the contract every scaffolded script does: the
// three behaviours it exists for, named; both spellings of what it
// reads; the storage guard; the prerender and pageswap facts its
// ordering depends on.
func TestShellContract(t *testing.T) {
	js := string(ShellJS())
	for _, want := range []string{
		// 1. Direction.
		"pagereveal", "viewTransition.types.add", `"back"`, `"forward"`, "navigationType", `"traverse"`,
		// 2. History reuse, only when proven. The Navigation API is read
		// through a local (nav), so the call is spelled nav.entries().
		"history.back()", "nav.entries()", "sameDocument", "e.button !== 0", "defaultPrevented",
		// 3. Focus return, record first, never while prerendering.
		"rst-shell-return", "pageswap", "pagehide", "document.prerendering", "decodeURIComponent", "sessionStorage",
		// Both spellings.
		"[rst-shell-back]", ".rst-shell__back", `[rst-shell-sidebar~="index"]`, ".rst-shell-sidebar--index",
		`[rst-shell-console~="page"]`, ".rst-shell-console--page",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("shell.js does not mention %q", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(js), "/*") || !strings.Contains(js, "(function () {") || !strings.HasSuffix(strings.TrimSpace(js), "})();") {
		t.Error("shell.js should be its contract comment and a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") || strings.Contains(js, "\t") {
		t.Error("shell.js must stay CSP-clean and use two-space indentation")
	}
}

// A prerendered page runs its scripts before anyone is looking at it,
// and may never be looked at: a data-poll there would fetch every two
// seconds (or hold an EventSource open) for a page the reader only
// hovered a link to. Polling starts when the page is shown: at once on
// a page being viewed, at activation on a prerendered one.
func TestPollingWaitsForAPrerenderedPageToBeShown(t *testing.T) {
	type count struct{ Timers, Sources int }
	var got map[string]struct {
		Viewed      count
		Prerendered struct{ Before, After count }
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"shim_node.mjs"}}), &got); err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[string]count{"timer": {Timers: 1}, "push": {Sources: 1}} {
		g := got[kind]
		if g.Viewed != want {
			t.Errorf("%s polling on a page being viewed started %+v, want %+v", kind, g.Viewed, want)
		}
		if g.Prerendered.Before != (count{}) {
			t.Errorf("%s polling on a prerendered page nobody is viewing started %+v, want nothing", kind, g.Prerendered.Before)
		}
		if g.Prerendered.After != want {
			t.Errorf("%s polling on a prerendered page, once activated, started %+v, want %+v", kind, g.Prerendered.After, want)
		}
	}
}
