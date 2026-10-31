package corpus

import (
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// regenerate is how to fix a set with no compiled code.
const regenerate = "run: go run ./internal/tmplgen/cmd/tmplgen-corpus"

// TestMain clears the suite's shadow mode (hack/gotest.sh turns it on
// for every test). TestCorpusEquivalence renders each case both ways
// itself, and some cases' data changes as it renders, a channel drained
// or a receiver swapped, so a second render inside shadow mode would
// differ from the first. A test that wants shadow mode sets it itself.
func TestMain(tests *testing.M) {
	os.Unsetenv(tmpl.ShadowEnv)
	os.Exit(tests.Run())
}
