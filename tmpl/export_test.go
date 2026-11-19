package tmpl

import "testing"

// What the external tests (package tmpl_test) need to see inside a set.

// NewViaHelper stands in for a framework package (ui, live, view) that
// creates a set on an app's behalf: its frame is in package tmpl, so the
// owner is whoever called it.
func NewViaHelper(name string) *Template { return New(name) }

func OwnerOf(template *Template) string { return template.owner() }

// SetOwner gives a set a made-up owner, so a test can call
// tmplrt.RegisterOwner for it without making every other set in this
// test package look stale. Call it before the set is first used.
func SetOwner(template *Template, owner string) { template.set.owner = owner }

func FingerprintOf(
	template *Template,
	name string,
) string {
	return template.fingerprints()[name]
}

var (
	PackageOf   = packageOf
	IsFramework = func(pkg string) bool { return frameworkPackages[pkg] }
)

const StaleMessage = staleMessage

var TypeExpr = typeExpr

// ResetCapture forgets the sets recorded so far, so a capture test sees
// only its own.
func ResetCapture() { captured.reset() }

// OnCaptureAdd runs callback each time the recorder adds a set, after the set
// is
// visible to WriteCapture.
func OnCaptureAdd(t testing.TB, callback func()) {
	testHookCaptured = callback
	t.Cleanup(func() { testHookCaptured = nil })
}

// SetTrace turns tracing on or off for one test, as TraceEnv does for a
// process, and returns what restores it.
func SetTrace(on bool) (restore func()) {
	saved := tracing
	tracing = on
	return func() { tracing = saved }
}

const TraceMessage = traceMessage
