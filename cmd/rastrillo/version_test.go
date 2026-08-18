package main

import "testing"

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
