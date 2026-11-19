//go:build !unix

package routelist

import "testing"

// Without process groups a cancelled build or listing can leave its
// children running and the scratch directory held open, so every step
// refuses before Go runs.
func TestListingRefusesWhereItCannotCleanUp(t *testing.T) {
	_, err := Find(t.Context(), fixture("app"), "")
	wantError(t, err, noCleanupHere)
	target := Target{ModuleDir: fixture("app"), ImportPath: appPackage,
		AppType: "App", Pointer: true}
	_, err = List(t.Context(), target)
	wantError(t, err, noCleanupHere)
	_, err = Unmounted(t.Context(), target, nil)
	wantError(t, err, noCleanupHere)
}
