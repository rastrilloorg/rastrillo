package ui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var markupTag = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)([^>]*?)(/?)>`)

var voidElement = map[string]bool{"area": true, "br": true, "col": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "wbr": true}

var (
	classValue = regexp.MustCompile(`class="([^"]*)"`)
	attrNamed  = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`(?:^|\s)` + name + `(?:="([^"]*)")?(?:\s|$)`)
	}
	lrowAttr   = attrNamed("rst-lrow")
	personAttr = attrNamed("rst-person")
)

// hasClass reports whether attrs carries class token c, exactly.
func hasClass(attrs, c string) bool {
	if m := classValue.FindStringSubmatch(attrs); m != nil {
		for _, tok := range strings.Fields(m[1]) {
			if tok == c {
				return true
			}
		}
	}
	return false
}

// isDataRow is a list-grid row in either spelling, not its head row.
func isDataRow(attrs string) bool {
	a := " " + attrs + " "
	if m := lrowAttr.FindStringSubmatch(a); m != nil {
		return !strings.Contains(" "+m[1]+" ", " head ")
	}
	return hasClass(attrs, "rst-lrow") && !hasClass(attrs, "rst-lrow--head")
}

// isIdentityLink is a.rst-nm or a person link, in either spelling.
func isIdentityLink(attrs string) bool {
	return hasClass(attrs, "rst-nm") || hasClass(attrs, "rst-person") || personAttr.MatchString(" "+attrs+" ")
}

// identityLinksPerRow returns, for every [rst-lrow] data row in markup,
// how many of its DIRECT children are identity links (a.rst-nm or
// a[rst-person]). A small depth-tracking reader is enough: the samples
// are hand-written, well-formed markup.
func identityLinksPerRow(markup string) []int {
	var out []int
	type frame struct {
		row  bool
		slot int
	}
	var stack []frame
	for _, m := range markupTag.FindAllStringSubmatch(markup, -1) {
		closing, name, attrs, self := m[1] == "/", strings.ToLower(m[2]), m[3], m[4] == "/"
		if closing {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		parentRow := len(stack) > 0 && stack[len(stack)-1].row
		if parentRow && name == "a" && isIdentityLink(attrs) {
			out[stack[len(stack)-1].slot]++
		}
		if self || voidElement[name] {
			continue
		}
		f := frame{}
		if isDataRow(attrs) {
			f = frame{row: true, slot: len(out)}
			out = append(out, 0)
		}
		stack = append(stack, f)
	}
	return out
}

func TestEveryListGridRowHasOneIdentityLink(t *testing.T) {
	rows := 0
	for name, sample := range Styleguide() {
		for i, n := range identityLinksPerRow(sample) {
			rows++
			if n > 1 {
				t.Errorf("styleguide %q row %d has %d identity links; one per row, or the later overlay hides the earlier link", name, i, n)
			}
		}
	}
	if rows == 0 {
		t.Fatal("found no list-grid data rows in the samples; the reader is broken")
	}
	// The reader itself, both ways.
	two := `<div rst-lrow><a class="rst-nm" href="/a">A</a><a rst-person href="/p"><span rst-person-name>P</span></a></div>`
	if got := identityLinksPerRow(two); len(got) != 1 || got[0] != 2 {
		t.Errorf("a row with two identity links reads as %v, want [2]", got)
	}
	nested := `<div rst-lrow><span><a class="rst-nm" href="/a">A</a></span><a class="rst-nm" href="/b">B</a></div>`
	if got := identityLinksPerRow(nested); len(got) != 1 || got[0] != 1 {
		t.Errorf("a nested link counted as a direct child: %v, want [1]", got)
	}
	// markup-spelling: old-spelling begin — the reader must count the
	// class spelling too, which apps still write, so these cases use it.
	for _, c := range []struct {
		markup string
		want   []int
	}{
		{`<div class="rst-lrow"><a class="rst-nm" href="/a">A</a><a class="rst-person" href="/p">P</a></div>`, []int{2}},
		{`<div rst-lrow=""><a class="rst-nm" href="/a">A</a><a rst-person href="/p">P</a></div>`, []int{2}},
		{`<div rst-lrow="head"><a class="rst-nm" href="/a">A</a></div><div class="rst-lrow rst-lrow--head"><a class="rst-nm" href="/a">A</a></div>`, nil},
		{`<div rst-lrow><a class="rst-nm-extra" href="/a">A</a></div>`, []int{0}},
		// markup-spelling: old-spelling end
	} {
		if got := identityLinksPerRow(c.markup); !reflect.DeepEqual(got, c.want) {
			t.Errorf("identityLinksPerRow(%s) = %v, want %v", c.markup, got, c.want)
		}
	}
}
