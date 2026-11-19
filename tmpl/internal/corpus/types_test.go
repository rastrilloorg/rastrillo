package corpus

import (
	"bytes"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl/internal/tmplhook"
)

// TestCorpusTypesHold: every fact generate wrote of the corpus's types
// holds of them, so each typed variant is in use. A fact the generator
// wrote otherwise than reflect finds it would send typed data down the
// code that reads by reflection, which renders the same bytes, and the
// equivalence tests would not notice.
func TestCorpusTypesHold(t *testing.T) {
	var changed []error
	defer tmplhook.ListenTypes(
		func(err error) { changed = append(changed, err) },
	)()
	for _, testCase := range Cases() {
		set, err := testCase.Build()
		if err != nil {
			t.Fatal(err)
		}
		// Preparing a set binds the code of every template in it.
		set.IsCompiled(testCase.Entry)
	}
	for _, testCase := range PagesCases() {
		pages, err := testCase.Build()
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range testCase.Runs(pages) {
			pages.Execute(new(bytes.Buffer), run.Page, run.Data)
		}
	}
	for _, err := range changed {
		t.Error(err)
	}
}
