package iconsets

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
)

// Every set answers every Lucide slug. This is what makes --icons a flag
// rather than a code migration: the shipped partials call the same four
// names whatever set is chosen.
func TestEverySetCoversEverySlug(t *testing.T) {
	for _, name := range Names() {
		for _, d := range []Delivery{Inline, CDN, JS} {
			got, err := Render(name, d)
			if err != nil {
				t.Fatalf("Render(%q, %q): %v", name, d, err)
			}
			for _, slug := range Slugs() {
				if !strings.Contains(string(got.Source), `"`+slug+`":`) {
					t.Errorf("set %q delivery %q is missing slug %q", name, d, slug)
				}
			}
		}
	}
}

// The invariant that survives every combination.
func TestEveryCombinationIsAriaHidden(t *testing.T) {
	for _, name := range Names() {
		for _, d := range []Delivery{Inline, CDN, JS} {
			got, _ := Render(name, d)
			n := strings.Count(string(got.Source), `aria-hidden="true"`)
			if n < len(Slugs()) {
				t.Errorf("set %q delivery %q: %d aria-hidden markers, want >= %d", name, d, n, len(Slugs()))
			}
		}
	}
}

// Inline delivery reaches nothing outside the page — the original
// icons.go promise, kept as the default's behaviour.
func TestInlineDeliveryIsSelfContained(t *testing.T) {
	for _, name := range Names() {
		got, _ := Render(name, Inline)
		for _, bad := range []string{"http://", "https://", "<script", "<link ", "url("} {
			if strings.Contains(string(got.Source), bad) {
				t.Errorf("inline %q reaches outside the page (%q)", name, bad)
			}
		}
		if got.Notice != "" {
			t.Errorf("inline %q emitted a tradeoff notice: %q", name, got.Notice)
		}
	}
}

// Non-default delivery states its cost exactly once, and pins an SRI.
func TestRemoteDeliveryNoticesAndPinsIntegrity(t *testing.T) {
	for _, name := range Names() {
		for _, d := range []Delivery{CDN, JS} {
			got, _ := Render(name, d)
			if got.Notice == "" {
				t.Errorf("%q/%q: no informed-consent notice", name, d)
			}
			if !strings.Contains(string(got.Source), "integrity=") {
				t.Errorf("%q/%q: no SRI pinned", name, d)
			}
			if !strings.Contains(string(got.Source), `crossorigin="anonymous"`) {
				t.Errorf("%q/%q: SRI without crossorigin is inert", name, d)
			}
		}
	}
}

// JS delivery's cost is the specific one that collides with the
// progressive-enhancement convention, so it must be named.
func TestJSNoticeNamesTheNoJSCost(t *testing.T) {
	got, _ := Render("lucide", JS)
	if !strings.Contains(strings.ToLower(got.Notice), "javascript") {
		t.Errorf("JS notice does not mention JavaScript: %q", got.Notice)
	}
}

// Font Awesome Free is CC BY 4.0: shipping it without attribution is a
// licence violation, so the renderer must hand the scaffold the file.
func TestFontAwesomeCarriesAttribution(t *testing.T) {
	for _, d := range []Delivery{Inline, CDN, JS} {
		got, _ := Render("font-awesome", d)
		if got.AttribName == "" || len(got.Attribution) == 0 {
			t.Fatalf("font-awesome/%q ships no attribution file", d)
		}
		if !strings.Contains(string(got.Attribution), "CC BY 4.0") {
			t.Errorf("font-awesome/%q attribution does not name the licence", d)
		}
	}
	got, _ := Render("lucide", Inline)
	if got.AttribName != "" {
		t.Errorf("lucide should need no attribution file, got %q", got.AttribName)
	}
}

func TestRenderedSourceCompilesAsAPackage(t *testing.T) {
	got, _ := Render("lucide", Inline)
	src := string(got.Source)
	for _, want := range []string{
		"package icons",
		"func Icon(slug string) template.HTML",
		"func Assets() template.HTML",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("rendered source is missing %q", want)
		}
	}
}

func TestUnknownSetAndDeliveryAreErrors(t *testing.T) {
	if _, err := Render("dingbats", Inline); err == nil {
		t.Error("unknown set did not error")
	}
	if _, err := Render("lucide", Delivery("carrier-pigeon")); err == nil {
		t.Error("unknown delivery did not error")
	}
}

// Rendering Go source that does not parse is the failure mode this whole
// package risks.
func TestRenderedSourceParses(t *testing.T) {
	for _, name := range Names() {
		for _, d := range []Delivery{Inline, CDN, JS} {
			got, err := Render(name, d)
			if err != nil {
				t.Fatalf("Render(%q,%q): %v", name, d, err)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), "icons.go", got.Source, parser.AllErrors); err != nil {
				t.Errorf("%s/%s does not parse: %v\n%s", name, d, err, got.Source)
			}
		}
	}
}

// The drift guard: rastrillo's own vocabulary and the scaffoldable sets
// must stay in step. An icon added to icons.go that no set answers is an
// icon that vanishes the moment someone passes --icons.
func TestSlugsMatchTheFrameworkVocabulary(t *testing.T) {
	want := rastrillo.IconSlugs()
	got := Slugs()
	if len(got) != len(want) {
		t.Fatalf("iconsets covers %d slugs, the framework answers %d:\ngot  %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slug %d: iconsets has %q, the framework has %q", i, got[i], want[i])
		}
	}
}
