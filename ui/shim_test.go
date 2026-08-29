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
		// Light dismiss: the menu classes it answers to, the containment
		// test that keeps the menu being used open, the Escape key, and
		// the focus hand-back to the summary. Delegated on the document,
		// so the count of addEventListener calls stays at two however
		// many menus a page renders.
		"rst-dropdown", "rst-row-menu", "closeMenus", "contains", "Escape", "summary.focus()",
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

// Raised from 8KB to 12KB, once, by the menu light-dismiss section —
// the same move select.js made below, and to the same number, so the two
// sibling files now share one cap.
//
// The arithmetic, so this is a decision and not a drift. The file went
// from 7,542 to 10,423 bytes: 2,881 added, of which 715 are code (one
// selector constant, two small functions, two addEventListener calls)
// and 2,166 are comment — the header's honesty note that this one
// section is class-driven rather than data-attribute opt-in, the
// vocabulary entry, and the block's own reasoning. There was 650 bytes
// of headroom under 8KB, so even the code alone would not have fitted.
//
// The cap is still the point: an app owner owns this file from the
// moment it is scaffolded and has to be able to read the whole thing in
// one sitting. Past 12KB, split something out instead — that is what
// select.js already is.
func TestShimIsSmall(t *testing.T) {
	if n := len(ShimJS()); n > 12*1024 {
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
