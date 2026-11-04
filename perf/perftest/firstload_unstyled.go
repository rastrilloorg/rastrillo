package perftest

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

const (
	unstyledMsg = "%s: shows %s, but its slice of tokens.css lacks a " +
		"rule for it that the whole file has, so it can render " +
		"unstyled. A page's slice holds the hooks its templates name, " +
		"not markup built in Go or by a script: ask for this hook in " +
		"the page's template with {{needs \"%s\"}}"
	unstyledReadMsg = "%s: its tokens.css cannot be read to check " +
		"the hooks it shows against its slice: %v"
)

// reportUnstyled fails a sliced page for each hook it shows that is
// named by a rule the whole tokens.css would give it and its slice
// does not (spec 3a): the slice comes from the page's templates, so
// markup they do not hold renders unstyled, where the whole file would
// have styled it. Asking for every hook the failure names puts the
// rule in the slice. A hook no rule styles is the app's own, and fine.
func reportUnstyled(
	t testing.TB, screen string, page []byte, sheet pageStylesheet,
) {
	t.Helper()
	shown := shownHooks(page)
	if !sheet.sliced() || len(shown) == 0 {
		return
	}
	unkept, err := vendored.Unkept(sheet.whole, sheet.slice, shown)
	if err != nil {
		t.Errorf(unstyledReadMsg, screen, err)
		return
	}
	for _, hook := range unkept {
		t.Errorf(unstyledMsg, screen, hook, hook)
	}
}

// shownHooks is the hooks a page's markup carries: its attribute names
// and class names, read by the reader the slicer's hooks come from, and
// the value each hook attribute carries, so a variant the page does not
// show is not counted as unstyled, and the hooks it wears as classes,
// so a class selector for a hook it carries only as an attribute is
// not either. An option's data-rst-class counts as classes: select.js
// copies them onto the row it draws.
func shownHooks(page []byte) map[string]bool {
	var names []byte
	shown := map[string]bool{}
	for _, tag := range pageTags(page) {
		for name, value := range tag.attributes {
			names = append(names, name+" "...)
			if name == "class" || name == "data-rst-class" {
				names = append(names, value+" "...)
				for _, class := range vendored.HooksIn([]byte(value)) {
					shown[vendored.ShownClass(class)] = true
				}
			}
			if vendored.IsHook(name) {
				shown[vendored.ShownValue(name, value)] = true
			}
		}
	}
	for _, hook := range vendored.HooksIn(names) {
		shown[hook] = true
	}
	if len(shown) > 0 {
		shown[vendored.ListedClasses] = true
	}
	return shown
}
