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
		"data-poll", "data-poll-every", "data-busy", "data-busy-label",
		"Rastrillo-Fragment", "Rastrillo-Location",
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
