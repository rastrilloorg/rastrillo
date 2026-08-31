package main

import "runtime/debug"

// rastrilloFallbackVersion is what rastrilloVersion reports when this
// binary's own build info carries no tagged version to pin — e.g. a
// plain `go build`/`go test` in a local checkout, which runtime/debug
// reports as "(devel)". Bump this on every release tag; better still,
// find a way to derive it so this constant needs no maintenance at all.
const rastrilloFallbackVersion = "v0.23.0"

// rastrilloVersion reports the version of github.com/carlosframework/rastrillo
// that built this CLI binary, so `rastrillo new` can pin the scaffolded
// go.mod to the framework version the CLI actually ships with, rather
// than a hardcoded constant that goes stale every release. That drift
// was the bug this replaces: a hardcoded "v0.1.0" predates
// rastrillo.Run entirely, so a fresh scaffold against a v0.5.0-or-later
// CLI failed `go build` with "undefined: rastrillo.Run".
//
// `go install github.com/carlosframework/rastrillo/cmd/rastrillo@vX.Y.Z`
// embeds that tag in the resulting binary's build info. Since
// cmd/rastrillo lives in the same module as the framework it
// scaffolds (github.com/carlosframework/rastrillo), that tag *is* the
// module's own version — the exact one to require.
func rastrilloVersion() string {
	v, _ := rastrilloVersionTagged()
	return v
}

// rastrilloVersionTagged reports the same version and whether it came
// from a real install tag rather than the fallback constant.
//
// `rastrillo doctor` needs the second half. It compares its own version
// against the app's, and a binary someone built from a checkout reports
// the fallback — a version it has no evidence for. Saying so is the
// difference between "you are on different versions" and "I am guessing
// at mine": the first is a finding, the second is a caveat, and a tool
// that reports drift must not confuse them.
func rastrilloVersionTagged() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return rastrilloFallbackVersion, false
	}
	v := versionFromBuildInfo(info.Main.Version)
	return v, v == info.Main.Version
}

// versionFromBuildInfo isolates the fallback decision from
// debug.ReadBuildInfo so it's unit-testable without a real @vX.Y.Z
// install: "(devel)" and "" (no main module, e.g. GOPATH-mode builds)
// both mean "no tagged version available."
func versionFromBuildInfo(mainVersion string) string {
	if mainVersion == "" || mainVersion == "(devel)" {
		return rastrilloFallbackVersion
	}
	return mainVersion
}
