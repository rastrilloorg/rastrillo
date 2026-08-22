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

func TestShimIsSmall(t *testing.T) {
	if n := len(ShimJS()); n > 8*1024 {
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
	if n := len(SelectJS()); n > 8*1024 {
		t.Fatalf("select.js is %d bytes; it is split out of the shim precisely to stay readable — trim it", n)
	}
	if bytes.Contains(SelectJS(), []byte("\t")) {
		t.Error("select.js uses two-space indentation, not tabs")
	}
}

// Neither file may reach off-origin: both are vendored, first-party and
// dependency-free.
func TestScriptsAreSelfContained(t *testing.T) {
	for name, js := range map[string]string{"rastrillo.js": string(ShimJS()), "select.js": string(SelectJS())} {
		for _, bad := range []string{"http://", "https://", "import ", "require(", "//cdn"} {
			if strings.Contains(js, bad) {
				t.Errorf("%s reaches outside the page (%q)", name, bad)
			}
		}
	}
}
