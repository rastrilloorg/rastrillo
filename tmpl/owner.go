package tmpl

import (
	"runtime"
	"strings"
)

// frameworkPackages build sets on an app's behalf. A set they create
// belongs to the app that called them, because that is the package
// rastrillo generate writes the compiled code into. The paths are exact:
// a package under one of them, such as tmpl/internal/corpus, owns the
// sets it creates.
var frameworkPackages = map[string]bool{
	"amadan.net/rastrillo/rastrillo/tmpl": true,
	"amadan.net/rastrillo/rastrillo/ui":   true,
	"amadan.net/rastrillo/rastrillo/live": true,
	"amadan.net/rastrillo/rastrillo/view": true,
}

// callerPackage is the import path of the first function on the stack
// outside the framework packages and the runtime, or "" if there is
// none, as for a set a framework package builds in its own init. A main
// package is "main", which is how its functions are named.
func callerPackage() string {
	var pcs [64]uintptr
	count := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:count])
	for {
		frame, more := frames.Next()
		pkg := packageOf(frame.Function)
		if pkg != "" && pkg != "runtime" && !frameworkPackages[pkg] {
			return pkg
		}
		if !more {
			return ""
		}
	}
}

// packageOf is the import path in a function name as the runtime spells
// it: "example.com/app.(*T).Method.func1" is in "example.com/app".
func packageOf(function string) string {
	// Type arguments can contain slashes and dots of their own.
	if index := strings.IndexByte(function, '['); index >= 0 {
		function = function[:index]
	}
	slash := strings.LastIndexByte(function, '/')
	dot := strings.IndexByte(function[slash+1:], '.')
	if dot < 0 {
		return ""
	}
	// The runtime escapes dots in the last path element, so that the
	// first dot after the last slash ends the path.
	return strings.ReplaceAll(function[:slash+1+dot], "%2e", ".")
}
