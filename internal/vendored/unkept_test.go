package vendored_test

import (
	"errors"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

func TestUnkeptNamesTheHooksOfRulesTheSliceLacks(t *testing.T) {
	whole := `[rst-btn]{a:b}.rst-card{c:d}` +
		`.rst-field-row>.rst-grow{e:f}` +
		`.rst-btn:not(.rst-danger){g:h}.rst-danger{i:j}` +
		`@media (x){.rst-card{k:l}}`
	button := `[rst-btn]{a:b}.rst-btn:not(.rst-danger){g:h}`
	cases := []struct {
		name, slice string
		shown       []string
		want        []string
	}{
		{"all kept", button + `.rst-card{c:d}@media (x){.rst-card{k:l}}`,
			[]string{"rst-btn", "rst-card"}, nil},
		{"own rule missing", button,
			[]string{"rst-btn", "rst-card"}, []string{"rst-card"}},
		{"partner not shown", button,
			[]string{"rst-btn", "rst-grow"}, nil},
		{"partner shown, rule missing", button,
			[]string{"rst-btn", "rst-field-row", "rst-grow"},
			[]string{"rst-field-row", "rst-grow"}},
		{"only under :not in the slice", button,
			[]string{"rst-btn", "rst-danger"}, []string{"rst-danger"}},
		{"kept outside its group", button + `.rst-card{c:d}`,
			[]string{"rst-btn", "rst-card"}, []string{"rst-card"}},
	}
	for _, test := range cases {
		hooks := map[string]bool{}
		for _, hook := range test.shown {
			hooks[hook] = true
		}
		got, err := vendored.Unkept(
			[]byte(whole), []byte(test.slice), hooks)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("%s: Unkept = %q, want %q", test.name, got, test.want)
		}
	}
}

// A selector a page cannot match is no loss: a slice that drops a
// class the page never wears keeps its rule for the page, and one that
// drops a class it wears does not.
func TestUnkeptComparesSelectorBySelector(t *testing.T) {
	whole := `.rst-btn,[rst-btn]{a:b}`
	cases := []struct {
		name, slice string
		shown       []string
		want        []string
	}{
		{"an unworn class dropped", `[rst-btn]{a:b}`,
			[]string{"rst-btn", vendored.ListedClasses}, nil},
		{"a worn class dropped", `[rst-btn]{a:b}`,
			[]string{"rst-btn", vendored.ListedClasses,
				vendored.ShownClass("rst-btn")}, []string{"rst-btn"}},
		{"classes not listed", `[rst-btn]{a:b}`,
			[]string{"rst-btn"}, []string{"rst-btn"}},
		{"every selector kept", whole,
			[]string{"rst-btn", vendored.ListedClasses,
				vendored.ShownClass("rst-btn")}, nil},
		{"the rule's block differs", `[rst-btn]{a:c}`,
			[]string{"rst-btn", vendored.ListedClasses},
			[]string{"rst-btn"}},
	}
	for _, test := range cases {
		hooks := map[string]bool{}
		for _, hook := range test.shown {
			hooks[hook] = true
		}
		got, err := vendored.Unkept(
			[]byte(whole), []byte(test.slice), hooks)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("%s: Unkept = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestUnkeptRefusesWhatItCannotRead(t *testing.T) {
	_, err := vendored.Unkept(
		[]byte(`[rst-a]{a:b`), []byte(`[rst-a]{a:b}`), nil)
	if !errors.Is(err, vendored.ErrUnreadable) {
		t.Errorf("got %v, want ErrUnreadable", err)
	}
}
