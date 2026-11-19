package corpus

import (
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/tmplgen"
)

// TestCorpusGeneratedIsFresh regenerates the corpus file in check mode:
// a case edited without regenerating would otherwise run on
// html/template and TestCorpusEquivalence would say only that it is not
// compiled. It also wants every template compiled: generate reports
// none it leaves on html/template.
func TestCorpusGeneratedIsFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the capture command")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tmplgen.Run(root, tmplgen.Options{
		Mains: []string{"./tmpl/internal/corpus/cmd/corpuscapture"},
		Check: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range res.CaptureFailures {
		t.Errorf("capture failed: %s: %s", failure.Owner, failure.Reason)
	}
	if len(res.Stale) > 0 {
		t.Errorf(
			"stale: %v; run: go run ./internal/tmplgen/cmd/tmplgen-corpus",
			res.Stale,
		)
	}
	for _, note := range res.Unsupported {
		t.Errorf(
			"not compiled: set %s, template %q: %s",
			note.Set,
			note.Template,
			note.Reason,
		)
	}
}
