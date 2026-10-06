package designsystem

import (
	"errors"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/codeview"
)

// Every generated call renders the partial's own output, byte for byte,
// in all twelve locales: the call is parsed into a fresh copy of that
// locale's partial tree, run against its dot, and compared with what
// the preview was rendered from. Twelve because a default resolved
// through T differs by language while the call does not, and a call
// that only matched in English would be a call that leaves something
// out.
func TestEveryGeneratedCallRendersThePartialsOwnOutput(t *testing.T) {
	var checked int
	for _, locale := range rastrillo.BaseLocales() {
		samples := sampleTree(t, locale)
		calls, err := partialTree(locale)
		if err != nil {
			t.Fatal(err)
		}
		type job struct {
			name, what string
			c          call
			want       string
		}
		var jobs []job
		for _, fam := range families() {
			for _, doc := range fam.Partials {
				for i, s := range doc.States {
					if s.Raw != "" {
						continue
					}
					want, err := renderSample(samples, doc.Name, i, s, locale)
					if err != nil {
						t.Fatalf("%s/%s (%s): %v", locale, doc.Name, s.State, err)
					}
					c, err := callFor(doc.Name, s, locale)
					if err != nil {
						t.Errorf("%s: %v", locale, err)
						continue
					}
					name := fmt.Sprintf("ds-call-%d", len(jobs))
					if _, err := calls.Parse(`{{define "` + name + `"}}` + c.Source + `{{end}}`); err != nil {
						t.Errorf("%s/%s (%s): the call does not parse: %v\n%s", locale, doc.Name, s.State, err, c.Source)
						continue
					}
					jobs = append(jobs, job{name, doc.Name + " (" + s.State + ")", c, string(want)})
				}
			}
		}
		for _, j := range jobs {
			var b strings.Builder
			if err := calls.ExecuteTemplate(&b, j.name, j.c.Dot); err != nil {
				t.Errorf("%s/%s: the call does not run: %v\n%s", locale, j.what, err, j.c.Source)
				continue
			}
			if b.String() != j.want {
				t.Errorf("%s/%s: the call renders different markup from the preview\ncall: %s\n got: %.300s\nwant: %.300s", locale, j.what, j.c.Source, b.String(), j.want)
			}
			checked++
		}
	}
	if checked < 12*50 {
		t.Errorf("only %d calls checked across twelve locales; the sample set has far more", checked)
	}
}

func TestCallForNamesThePartialTheStateAndTheType(t *testing.T) {
	for name, c := range map[string]struct {
		data any
		typ  string
	}{
		"struct":        {map[string]any{"V": struct{}{}}, "struct {}"},
		"pointer":       {map[string]any{"V": new(string)}, "*string"},
		"nil":           {map[string]any{"V": nil}, "nil"},
		"template.HTML": {map[string]any{"V": template.HTML("x")}, "template.HTML"},
	} {
		_, err := callFor("notice", sample{State: "Probe " + name, Data: c.data}, "en")
		if !errors.Is(err, codeview.ErrUnwritable) {
			t.Errorf("%s: err = %v, want ErrUnwritable", name, err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, "notice") || !strings.Contains(msg, "Probe "+name) || !strings.Contains(msg, "V is "+c.typ+":") && !strings.Contains(msg, "V is a "+c.typ+":") {
			t.Errorf("%s: %q does not name the partial, the state and the type %s", name, msg, c.typ)
		}
	}
}

// The Keys block order is read off the partials themselves, so a key
// added to a partial's documentation moves its calls on the next render.
func TestThePartialKeyOrderIsReadOffTheKeysBlocks(t *testing.T) {
	keys := partialKeys()
	for partial, want := range map[string][]string{
		"meter":        {"Percent", "Text"},
		"confirm-form": {"Action", "Label", "Danger", "Hidden", "CancelHref", "CancelLabel"},
		"locale-menu":  {"Items", "Return", "Action", "MenuGroup"},
	} {
		if got := keys[partial]; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v, want %v", partial, got, want)
		}
	}
	if got := keys["row-menu"]; len(got) != 3 {
		t.Errorf("row-menu: keys %v, want Name, Items, MenuGroup (the nested item keys are not the partial's)", got)
	}
}
