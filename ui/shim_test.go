package ui

import (
	"bytes"
	"strings"
	"testing"
)

// The shim has no browser harness — JS behavior is verified by hand
// and by the notes example's no-JS end-to-end path. What a Go test can
// hold honest is the contract the docs promise: the vocabulary the
// file answers to, its inert-by-default IIFE shape, and the absence of
// anything a CSP would reject.
func TestShimContract(t *testing.T) {
	js := string(ShimJS())
	for _, want := range []string{
		"data-poll", "data-poll-every", "data-poll-push", "data-busy", "data-busy-label",
		"EventSource",
		"Rastrillo-Fragment", "Rastrillo-Location",
		// Behavior a Go test can still hold cheaply: the terminal
		// statuses that end a poll, the local-path guard on the
		// header-driven navigation, and the bfcache restore that
		// re-enables a busy form.
		"403", "404", "localPath", "pageshow",
		// Light dismiss: the menu classes it answers to — the nested
		// rst-menu-group among them, so a submenu is never left open
		// behind its closing parent — the containment test that keeps the
		// menu being used open, the Escape key, and the focus hand-back to
		// the summary. Delegated on the document, so the count of
		// addEventListener calls stays at two however many menus a page
		// renders.
		"rst-dropdown", "rst-menu-group", "rst-row-menu",
		"closeMenus", "contains", "Escape", "summary.focus()",
		// The busy rule: the spinner it builds, the submitter it reads
		// (only the clicked button goes busy), and the cancelled-submit
		// hand-back.
		"rst-spin rst-btn__spin", "e.submitter", "defaultPrevented",
		// The local-path guard must reject control characters —
		// browsers strip tab/CR/LF before parsing, so "/\t/evil"
		// resolves scheme-relative — mirroring sessions.SafeReturn.
		"\\u0000-\\u001f\\u007f",
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
	// Light dismiss is delegated, not bound per element: a menu that
	// arrives inside a polled fragment has to be covered the moment it
	// lands, and re-scanning must never double-bind. A querySelectorAll
	// over the menus inside scan() would be exactly that bug, so pin the
	// listeners to the document and pin scan() to the two data-attribute
	// vocabularies it has always answered.
	if !strings.Contains(js, `document.addEventListener("click", dismissMenus, true)`) ||
		!strings.Contains(js, `document.addEventListener("keydown", dismissMenus, true)`) {
		t.Error("light dismiss is not two delegated document listeners")
	}
	// Three mentions and no more: the definition and the two listeners.
	// A fourth would mean someone started binding it per element again.
	if n := strings.Count(js, "dismissMenus"); n != 3 {
		t.Errorf("dismissMenus appears %d times, want 3 (one definition, two document listeners)", n)
	}
	// Shell chrome and the toggle-block stay out of it: neither is a
	// menu, and dismissing them on an outside click would fight the user.
	for _, bad := range []string{"rst-shell__chrome", "rst-tblock"} {
		if strings.Contains(js, bad) {
			t.Errorf("shim reaches for %q; light dismiss covers menus only", bad)
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
	js := string(ShimJS())
	if !strings.Contains(js, `document.addEventListener("submit", busySubmit, true)`) {
		t.Error("the busy rule is not one delegated capture-phase submit listener on the document")
	}
	// Two mentions and no more: the definition and the one listener. A
	// third would mean someone started binding it per form again.
	if n := strings.Count(js, "busySubmit"); n != 2 {
		t.Errorf("busySubmit appears %d times, want 2 (one definition, one document listener)", n)
	}
	// The opt-in scan is gone. Any querySelectorAll over a data-busy
	// selector is the old shape coming back.
	for _, bad := range []string{`"form[data-busy]"`, `"[data-busy]"`, "busyForm"} {
		if strings.Contains(js, bad) {
			t.Errorf("shim still scans for %s; the busy rule is on by default, not opted into", bad)
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
		`if (/^dialog$/i.test(btn && btn.getAttribute("formmethod") ||`,
		`      form.getAttribute("method"))) return;`,
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

// Raised twice, each time by one new default-on section, and each time
// with the arithmetic written down so it stays a decision rather than a
// drift.
//
//	8KB → 12KB, by menu light dismiss. 7,542 → 10,423 bytes: 2,881
//	added, 715 of them code (one selector constant, two small
//	functions, two addEventListener calls) and 2,166 comment.
//
//	12KB → 16KB, by the busy rule. 11,689 → 16,177 bytes: 4,488 added,
//	of which 620 are CODE — busyOff, busySubmit, one addEventListener,
//	and one selector constant deleted — and 3,868 are COMMENT: the
//	header's second honesty note (two sections are now on by default,
//	not one), the rewritten data-busy vocabulary entry, the block that
//	writes down the two traps in longhand, and the note on why
//	every attribute is read through getAttribute. Those last two are
//	there because they are the bugs the next person will reintroduce:
//	setting disabled a tick too early, and reaching for form.target.
//
// Not raised a third time, and this is the round that nearly forced it.
// Two busy-rule defects — a <form method="dialog"> the guard wedged
// shut, and a form="id" submit button living outside its form that the
// reset could not reach — cost 98 bytes of CODE and would have cost
// another 300 in the comment voice the rest of this file is written in.
// 16,177 → 16,349: 172 added, of which 98 are code and 74 net comment,
// paid for by two sentences that were already written somewhere else
// (the data-busy opt-out, which the vocabulary block at the top states,
// and the delegated-listener rationale, which light dismiss states in
// full — and which pointed the reader "above" at a section that is
// below it). 35 bytes are left under the cap. That is the warning, in
// arithmetic: the next behaviour this file gains does not fit, and the
// reasoning for these two lives in TestBusyRuleIsTheDefault above and
// in browser_test.go's legs 2d and 4c because it did not fit here.
//
// The round after that closed the third shape of the same defect —
// <button formmethod="dialog"> in an ordinary form — for +29 bytes, by
// MERGING rather than adding: one effective method (the submitter's
// formmethod, else the form's) replaces the bail that read the form's
// method alone. 16,349 → 16,378: 47 bytes of code, and 18 GIVEN BACK by
// the comment, because two checks explained separately are now one
// explanation, and the submitter paragraph moved up to sit beside the
// var it describes. 6 bytes are left under the cap, and leg 2e in
// browser_test.go drives the shape.
//
// Splitting was the alternative and was rejected on the arithmetic:
// 618 bytes of behaviour does not earn a third scaffolded file, a third
// <script> tag on every page a shell renders, and a third thing for the
// app owner to find. select.js is split out because it is a whole
// widget; this is twenty-six lines that belong beside the form
// vocabulary they extend.
//
// The cap is still the point, and what it protects is the CODE: an app
// owner owns this file from the moment it is scaffolded and has to be
// able to read the whole thing in one sitting. The code in it grew from
// 5,082 to 5,702 bytes across those 4,488 — the file got more talkative,
// not more complicated. Past 16KB, split something out instead.
//
// select.js keeps the 12KB cap it reached separately. The two files
// sharing a number was a coincidence of history, not a rule: this one
// now carries three sections, and that one carries one widget.
func TestShimIsSmall(t *testing.T) {
	if n := len(ShimJS()); n > 16*1024 {
		t.Fatalf("shim is %d bytes; the point is that an app owner can read it in one sitting — trim it", n)
	}
	if bytes.Contains(ShimJS(), []byte("\t")) {
		t.Error("shim uses two-space indentation, not tabs")
	}
}

// select.js holds to the same contract as the shim, and to the same
// reason for existing: small enough that the app owner who now owns it
// can read the whole thing.
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
	// it is what the form submits.
	for _, bad := range []string{".remove()", "removeChild", "outerHTML ="} {
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
	for _, want := range []string{"OPTGROUP", `"group"`, "rst-select__group"} {
		if !strings.Contains(js, want) {
			t.Errorf("select.js does not mention %q; a grouped select would be flattened", want)
		}
	}
	// Raised from 8KB to 12KB, once, by optgroups and the markup
	// opt-out. Flattening native.options was a line shorter and silently
	// threw away the headings an author wrote to make a long list
	// readable; rendering the groups costs a nested list, an ARIA group
	// per optgroup, and a filter that hides a heading when its rows all
	// go. That is the trade, and it is a decision, not a drift.
	//
	// The cap is still the point: this file exists apart from the shim
	// so the app owner who now owns it can read the whole thing in one
	// sitting. Past 12KB, split something out instead.
	if n := len(SelectJS()); n > 12*1024 {
		t.Fatalf("select.js is %d bytes; it is split out of the shim precisely to stay readable — trim it", n)
	}
	if bytes.Contains(SelectJS(), []byte("\t")) {
		t.Error("select.js uses two-space indentation, not tabs")
	}
}

// No scaffolded script may reach off-origin: all three are vendored,
// first-party and dependency-free.
func TestScriptsAreSelfContained(t *testing.T) {
	for name, js := range map[string]string{
		"rastrillo.js": string(ShimJS()),
		"select.js":    string(SelectJS()),
		"datetime.js":  string(DatetimeJS()),
	} {
		for _, bad := range []string{"http://", "https://", "import ", "require(", "//cdn"} {
			if strings.Contains(js, bad) {
				t.Errorf("%s reaches outside the page (%q)", name, bad)
			}
		}
	}
}
