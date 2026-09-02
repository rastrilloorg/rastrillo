package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repoRootForVersion returns this repo's root, computed from this
// file's own location rather than the working directory, so the two
// release-hygiene tests below read the same CHANGELOG.md and the same
// tag store however `go test` was invoked.
func repoRootForVersion(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// newestChangelogVersion reads the first "## vX.Y.Z" heading in
// CHANGELOG.md, which the file's own preamble declares is newest-first.
func newestChangelogVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootForVersion(t), "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			if strings.HasPrefix(v, "v") {
				return strings.TrimSpace(v)
			}
		}
	}
	t.Fatal("CHANGELOG.md: no `## vX.Y.Z` heading found")
	return ""
}

// TestFallbackVersionMatchesTheChangelog catches a half-done release
// prep: the two places a release has to be written down disagreeing.
// It is the cheap half of the guard — no git, no network — and it
// fails on the prep commit itself rather than a release later.
func TestFallbackVersionMatchesTheChangelog(t *testing.T) {
	if got, want := rastrilloFallbackVersion, newestChangelogVersion(t); got != want {
		t.Errorf("rastrilloFallbackVersion = %q, newest CHANGELOG.md heading = %q\n"+
			"a release is recorded in both places; bump them together", got, want)
	}
}

// TestFallbackVersionIsNotBehindTheNewestTag catches the other half,
// and the one that actually bit: a tag cut with no prep commit behind
// it. Being *ahead* of the newest tag is the normal state during prep,
// so only "behind" is a failure — and behind means every app scaffolded
// by a dev-built CLI pins a version that does not exist, so its first
// `go mod tidy` dies with "unknown revision".
//
// Skips rather than fails without a tag store: a shallow clone or a
// source tarball has no tags, and a gate that cannot run is not
// evidence of a problem.
func TestFallbackVersionIsNotBehindTheNewestTag(t *testing.T) {
	cmd := exec.Command("git", "tag", "--list", "v*")
	cmd.Dir = repoRootForVersion(t)
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("no tag store to check against: %v", err)
	}

	newest := ""
	for _, tag := range strings.Fields(string(out)) {
		if semverLess(newest, tag) {
			newest = tag
		}
	}
	if newest == "" {
		t.Skip("no v* tags in this checkout")
	}

	if semverLess(rastrilloFallbackVersion, newest) {
		t.Errorf("rastrilloFallbackVersion = %q but %q is tagged\n"+
			"a release was cut without a prep commit; every scaffold from a\n"+
			"dev build now pins a version that may not exist", rastrilloFallbackVersion, newest)
	}
}

// semverLess orders the "vMAJOR.MINOR.PATCH" tags this repo cuts.
// Hand-rolled rather than importing golang.org/x/mod/semver, the same
// call modpath.go makes about golang.org/x/mod/modfile: one fixed
// shape, a dozen lines, not worth a dependency in the gate. Anything
// it cannot parse sorts lowest, so an unparseable tag can never be
// mistaken for the newest one and turn this test red on a stray ref.
func semverLess(a, b string) bool {
	av, aok := semverFields(a)
	bv, bok := semverFields(b)
	if !aok || !bok {
		return !aok && bok
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}

func semverFields(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// versionFromBuildInfo isolates the fallback decision from
// debug.ReadBuildInfo (which this test doesn't control — go test
// binaries always report "(devel)", see TestRastrilloVersionFallsBackUnderGoTest)
// so the "devel vs. tagged" branches are each exercised directly.
func TestVersionFromBuildInfo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.5.0", "v0.5.0"},
		{"v1.2.3", "v1.2.3"},
		{"(devel)", rastrilloFallbackVersion},
		{"", rastrilloFallbackVersion},
	}
	for _, c := range cases {
		if got := versionFromBuildInfo(c.in); got != c.want {
			t.Errorf("versionFromBuildInfo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A `go test` binary is a local-checkout build: runtime/debug reports
// its main module version as "(devel)" (no @vX.Y.Z install to embed),
// so rastrilloVersion must fall back rather than pin "(devel)" into a
// scaffolded go.mod.
func TestRastrilloVersionFallsBackUnderGoTest(t *testing.T) {
	if got := rastrilloVersion(); got != rastrilloFallbackVersion {
		t.Errorf("rastrilloVersion() under go test = %q, want fallback %q", got, rastrilloFallbackVersion)
	}
}
