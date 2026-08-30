package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// axe-core is vendored under ui/testdata/axe for the accessibility gate
// in internal/designsystem/a11y_test.go. It is test data, and these two
// tests are the whole of what "test data" means here: it is what the
// README says it is, and it is not in anything this package ships.
//
// Both run without a build tag, on every `go test ./...`, because the
// day somebody adds a //go:embed that sweeps testdata in is a day this
// has to fail loudly and not only when a browser is available.

const axeDir = "testdata/axe"

// axeReadme parses the pin out of the README beside the file: the
// version line and the sha256 line, in the indented block near the top.
// The README is the documentation and the manifest at once, so there is
// no second place for the two to disagree.
var (
	axeVersionLine = regexp.MustCompile(`(?m)^\s+version\s+(\S+)\s*$`)
	axeSHALine     = regexp.MustCompile(`(?m)^\s+sha256\s+([0-9a-f]{64})\s*$`)
)

func TestVendoredAxeIsThePinnedVersion(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(axeDir, "README.md"))
	if err != nil {
		t.Fatalf("the vendored axe has no README: %v", err)
	}
	vm := axeVersionLine.FindSubmatch(readme)
	sm := axeSHALine.FindSubmatch(readme)
	if vm == nil || sm == nil {
		t.Fatalf("%s/README.md does not name a version and a sha256 in its pin block", axeDir)
	}
	body, err := os.ReadFile(filepath.Join(axeDir, "axe.min.js"))
	if err != nil {
		t.Fatalf("reading the vendored axe: %v", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != string(sm[1]) {
		t.Errorf("axe.min.js is sha256 %s; the README pins %s", got, sm[1])
	}
	// Deque stamps the version into the banner comment, so the file
	// says its own version and the README cannot drift from it quietly.
	want := "axe v" + string(vm[1])
	if head := string(body[:min(len(body), 200)]); !strings.Contains(head, want) {
		t.Errorf("the README pins %s, but the file's banner does not say %q", vm[1], want)
	}
}

// TestVendoredAxeIsNotAShippedAsset is the containment gate. A scanner
// that ended up inside the thing it scans would be half a megabyte of
// third-party JavaScript on every page of every app built on this
// framework — and it would find the page it is on accessible, which is
// the joke that writes itself.
func TestVendoredAxeIsNotAShippedAsset(t *testing.T) {
	// The banner is present in every build of axe-core and appears in
	// no hand-written asset, so it is the marker to hunt for.
	const marker = "Deque Systems"
	shipped := map[string][]byte{
		"ui.TokensCSS()":  TokensCSS(),
		"ui.ShimJS()":     ShimJS(),
		"ui.SelectJS()":   SelectJS(),
		"ui.DatetimeJS()": DatetimeJS(),
	}
	for _, name := range ThemeNames() {
		css, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ui.ThemeCSS(%q) reports no such theme", name)
		}
		shipped["ui.ThemeCSS("+name+")"] = css
	}
	for _, name := range LayoutNames() {
		src, ok := Layout(name)
		if !ok {
			t.Fatalf("ui.Layout(%q) reports no such layout", name)
		}
		shipped["ui.Layout("+name+")"] = src
	}
	for what, body := range shipped {
		if strings.Contains(string(body), marker) {
			t.Errorf("%s carries the vendored axe-core: it is test data and must not ship", what)
		}
	}

	// And nothing embedded reaches testdata at all. Templates() is the
	// one embedded filesystem this package exposes; walking it costs
	// nothing and catches a //go:embed pattern that grew too wide.
	err := fs.WalkDir(Templates(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(p, "testdata") {
			t.Errorf("ui.Templates() embeds %s — testdata must stay out of the shipped tree", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking ui.Templates(): %v", err)
	}
}
