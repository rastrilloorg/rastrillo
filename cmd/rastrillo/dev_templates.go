package main

import (
	"os"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// devGenerateArgs is how dev runs generate: without the template step.
// It builds and starts the app to capture its sets, which would double
// the time from save to restart, for code dev's build leaves out
// anyway (devBuildArgs).
func devGenerateArgs(dir string) []string {
	return []string{"-templates=false", dir}
}

// devBuildArgs is dev's go build. The tag leaves compiled templates
// out, so dev always renders the templates on disk through
// html/template, and a committed file that no longer compiles, after a
// field it reads was renamed, cannot break the loop. generate --check
// is where a stale file fails.
func devBuildArgs(bin, appPkg string) []string {
	return []string{"build", "-tags=rastrillo_dev", "-o", bin, appPkg}
}

// devChildEnv is the app's environment under dev: dev's own, with
// tmpl.DevEnv set. dev's build has no compiled code to go stale, so
// this only matters to an app whose compiled code got in some other
// way; tmpl then logs a stale set at debug level, where a save is
// expected to outrun generate, rather than warning on every restart.
func devChildEnv() []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, tmpl.DevEnv+"=")
	})
	return append(env, tmpl.DevEnv+"=1")
}
