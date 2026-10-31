package corpus

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	texttemplate "text/template"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"amadan.net/rastrillo/rastrillo/tmpl/internal/tmplhook"
)

// TestCorpusEquivalence renders every case's entry through its compiled
// code and through html/template, and wants the same bytes, or an error
// from both.
func TestCorpusEquivalence(t *testing.T) {
	for _, testCase := range Cases() {
		t.Run(testCase.Name, func(t *testing.T) {
			set, err := testCase.Build()
			if err != nil {
				t.Fatal(err)
			}
			if !set.IsCompiled(testCase.Entry) {
				t.Fatalf(
					"%s has no compiled code; "+regenerate,
					testCase.Entry,
				)
			}
			for index, data := range testCase.Data {
				std, err := testCase.std()
				if err != nil {
					t.Fatal(err)
				}
				var want, got bytes.Buffer
				wantErr := std.ExecuteTemplate(
					&want,
					testCase.Entry,
					fresh(data),
				)
				gotErr := set.ExecuteTemplate(&got, testCase.Entry, fresh(data))
				label := fmt.Sprintf("data %d (%T)", index, data)
				switch {
				case wantErr != nil && gotErr == nil:
					t.Errorf(
						"%s: html/template fails (%v), compiled code wrote\n%s",
						label,
						wantErr,
						got.Bytes(),
					)
				case wantErr == nil && gotErr != nil:
					t.Errorf(
						"%s: compiled code fails (%v), html/template wrote\n%s",
						label,
						gotErr,
						want.Bytes(),
					)
				case gotErr != nil:
					if !errors.As(gotErr, new(texttemplate.ExecError)) {
						t.Errorf(
							"%s: compiled error %v is not a "+
								"text/template.ExecError",
							label,
							gotErr,
						)
					}
					has := testCase.errHas
					if has != "" && (!strings.Contains(gotErr.Error(), has) ||
						!strings.Contains(wantErr.Error(), has)) {
						t.Errorf(
							"%s: errors %q and %q, want both to contain %q",
							label,
							gotErr,
							wantErr,
							testCase.errHas,
						)
					}
				case testCase.errHas != "":
					t.Errorf(
						"%s: both succeeded; the case expects them "+
							"to fail with %q",
						label,
						testCase.errHas,
					)
				case !bytes.Equal(got.Bytes(), want.Bytes()):
					t.Errorf(
						"%s: compiled code wrote\n%s\nhtml/template wrote\n%s",
						label,
						got.Bytes(),
						want.Bytes(),
					)
				}
			}
		})
	}
}

func fresh(data any) any {
	if fresh, ok := data.(Fresh); ok {
		return fresh()
	}
	return data
}

// TestUIPagesEquivalence renders every ui case in shadow mode, where tmpl
// runs each template both ways and fails when the bytes or the failing
// differ. ui.Pages does not hand out its sets, so tmplhook says whether
// each render ran compiled code: one that did not would pass shadow
// mode by running html/template alone.
func TestUIPagesEquivalence(t *testing.T) {
	t.Setenv(tmpl.ShadowEnv, "1")
	var ran []string // the entries rendered on html/template
	defer tmplhook.Listen(func(entry string, compiled bool) {
		if !compiled {
			ran = append(ran, entry)
		}
	})()

	for _, testCase := range PagesCases() {
		t.Run(testCase.Name, func(t *testing.T) {
			pages, err := testCase.Build()
			if err != nil {
				t.Fatal(err)
			}
			rendered := 0
			for index, run := range testCase.Runs(pages) {
				ran = nil
				var buffer bytes.Buffer
				err := pages.Execute(&buffer, run.Page, run.Data)
				if err != nil &&
					strings.Contains(err.Error(), "tmpl: shadow:") {
					t.Errorf(
						"run %d (%s, %T): %v",
						index,
						run.Page,
						run.Data,
						err,
					)
				}
				// ui.Pages checks for a localizer before it executes
				// anything, so a run it refuses runs no template.
				if len(ran) > 0 {
					t.Errorf(
						"run %d (%s, %T): %v ran on html/template",
						index,
						run.Page,
						run.Data,
						ran,
					)
				}
				if err == nil {
					rendered++
				}
			}
			if rendered == 0 {
				t.Error("no page rendered without an error")
			}
		})
	}
}

// TestUIPagesCopyIsCurrent: testdata/uipages is a copy of ui's fixtures,
// which the corpus cannot embed from another directory.
func TestUIPagesCopyIsCurrent(t *testing.T) {
	original := os.DirFS("../../../ui/testdata/pages")
	copied := uiApp()
	seen := map[string]bool{}
	err := fs.WalkDir(
		original,
		".",
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			seen[path] = true
			want, err := fs.ReadFile(original, path)
			if err != nil {
				return err
			}
			got, err := fs.ReadFile(copied, path)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf(
					"testdata/uipages/%s differs from "+
						"ui/testdata/pages/%s; copy it again",
					path,
					path,
				)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WalkDir(
		copied,
		".",
		func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && !seen[path] {
				t.Errorf(
					"testdata/uipages/%s is not in ui/testdata/pages; "+
						"delete it",
					path,
				)
			}
			return err
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
