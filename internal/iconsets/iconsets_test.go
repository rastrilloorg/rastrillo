package iconsets

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
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

// codeOnly drops comment lines, so a licence URL in an attribution
// comment is not mistaken for markup that reaches off-origin. What
// matters is the icon markup a browser is handed, not what the file says
// about itself.
func codeOnly(src []byte) string {
	var b strings.Builder
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// Inline delivery reaches nothing outside the page — the original
// icons.go promise, kept as the default's behaviour.
func TestInlineDeliveryIsSelfContained(t *testing.T) {
	for _, name := range Names() {
		got, _ := Render(name, Inline)
		for _, bad := range []string{"http://", "https://", "<script", "<link ", "url("} {
			if strings.Contains(codeOnly(got.Source), bad) {
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

// LucideName answers every slug the framework does, and it answers with
// a name lucide.dev actually publishes — the class the vendored webfont
// binds to, which is the same string in the same file as the glyph data.
//
// This is the provenance the design system's Icons page prints beside
// each slug, so an unanswered slug there would be a blank line on a
// published page. A thirteenth slug lands here first: this fails, and
// TestSlugsMatchTheFrameworkVocabulary fails beside it, before anything
// renders.
//
// The count of RENAMED slugs is asserted rather than described: this is
// the arithmetic the prose in icons.go, iconsets.go and
// docs/site/icons.md makes a claim about, and
// TestTheWrittenSlugCountsMatchTheSet below holds those three sentences
// to whatever this counts.
func TestLucideNameAnswersEverySlug(t *testing.T) {
	renamed := map[string]string{}
	for _, slug := range rastrillo.IconSlugs() {
		name := LucideName(slug)
		if name == "" {
			t.Errorf("LucideName(%q) is empty: the vendored Lucide set has no glyph for a slug the framework answers", slug)
			continue
		}
		if strings.ContainsAny(name, ` "<>`) {
			t.Errorf("LucideName(%q) is %q, which is markup rather than a name — the element's class shape has changed", slug, name)
		}
		if name != slug {
			renamed[slug] = name
		}
	}
	if got := renamed["kebab"]; got != "ellipsis-vertical" {
		t.Errorf("LucideName(\"kebab\") is %q, want \"ellipsis-vertical\" — icons.go and docs/site/icons.md both name that one specifically", got)
	}
	if len(renamed) != 5 {
		t.Errorf("%d of the %d slugs differ from their Lucide name, want 5 — and %q, %q and %q all say %s of the %s in prose, which TestTheWrittenSlugCountsMatchTheSet holds them to: %v",
			len(renamed), len(rastrillo.IconSlugs()),
			"icons.go", "iconsets.go", "docs/site/icons.md",
			numberWord(len(renamed)), numberWord(len(rastrillo.IconSlugs())), renamed)
	}
}

// ── The counts written in prose ──────────────────────────────────────

// numberWords is the English number words this project's prose spells
// out. Go past twenty and prose stops spelling numbers anyway, so a
// count that walks off the end is a failure rather than a gap: the gate
// below says so instead of silently checking nothing.
var numberWords = []string{
	"zero", "one", "two", "three", "four", "five", "six", "seven",
	"eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen",
	"fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty",
}

func numberWord(n int) string {
	if n < 0 || n >= len(numberWords) {
		return fmt.Sprint(n)
	}
	return numberWords[n]
}

// countPhrase matches "<number word> of the <number word>" and
// countedNoun matches "<number word> slugs" — the two shapes the three
// documents use to state how big this set is.
var (
	anyNumber   = `(` + strings.Join(numberWords, "|") + `)`
	countPhrase = regexp.MustCompile(`\b` + anyNumber + ` of the ` + anyNumber + `\b`)
	countedNoun = regexp.MustCompile(`\b` + anyNumber + ` slugs\b`)
)

// slugProse names the three places outside Go code where the size of
// this set is written out in words, and the fenced list in the docs.
//
// They are prose, so nothing derives them, so they are exactly the
// stale-number failure this project has corrected five times in three
// days. The 13th slug is the scenario: if it is NOT renamed, every
// other gate here passes and all three sentences go on saying "of the
// twelve" about a thirteen-slug set.
var slugProse = []string{"../../icons.go", "iconsets.go", "../../docs/site/icons.md"}

// normalise makes a Go comment and a Markdown paragraph the same thing
// to match against: comment markers gone, line wrapping gone, case
// gone. Without it "five of\n// the twelve" is invisible to any
// substring the gate could write.
func normalise(src string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(src, "//", " ")), " "))
}

// TestTheWrittenSlugCountsMatchTheSet holds every spelled-out count in
// those documents to the set itself.
//
// It reads the shapes rather than a list of expected sentences, so it
// covers a fourth sentence somebody writes tomorrow without being told
// about it, and it fails whether the prose is too small or too large.
func TestTheWrittenSlugCountsMatchTheSet(t *testing.T) {
	slugs := rastrillo.IconSlugs()
	renamed := 0
	for _, slug := range slugs {
		if name := LucideName(slug); name != "" && name != slug {
			renamed++
		}
	}
	total, part := numberWord(len(slugs)), numberWord(renamed)
	if total == fmt.Sprint(len(slugs)) {
		t.Fatalf("the set has %d slugs, past the number words this gate knows; extend numberWords and reword the three documents", len(slugs))
	}
	checked := 0
	for _, path := range slugProse {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			continue
		}
		text := normalise(string(src))
		for _, m := range countPhrase.FindAllStringSubmatch(text, -1) {
			checked++
			if m[1] != part || m[2] != total {
				t.Errorf("%s says %q; the set is %s renamed of %s. Reword it — nothing derives this sentence.", path, m[0], part, total)
			}
		}
		for _, m := range countedNoun.FindAllStringSubmatch(text, -1) {
			checked++
			if m[1] != total {
				t.Errorf("%s says %q; the set has %s slugs. Reword it — nothing derives this sentence.", path, m[0], total)
			}
		}
	}
	// Asserted rather than assumed. These sentences are the whole
	// subject of this gate; if normalise or either pattern stops
	// matching, the loop above walks three files and finds nothing to
	// disagree with.
	if checked < len(slugProse) {
		t.Errorf("found %d written counts across %d documents, want at least one each — either a sentence was deleted or the patterns have stopped matching",
			checked, len(slugProse))
	}
	t.Logf("%d written counts held to %s slugs, %s of them renamed", checked, total, part)
}

// The fenced list in docs/site/icons.md is the same hazard in list form:
// twelve slug names typed out under a sentence that says
// rastrillo.IconSlugs() is the list. A thirteenth slug leaves the page
// naming twelve and claiming to name all of them.
func TestTheDocumentedSlugListIsTheSet(t *testing.T) {
	const path = "../../docs/site/icons.md"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	_, after, ok := strings.Cut(string(src), "```text\n")
	if !ok {
		t.Fatalf("%s has no ```text block; this gate is reading nothing", path)
	}
	block, _, ok := strings.Cut(after, "```")
	if !ok {
		t.Fatalf("%s: the ```text block never closes", path)
	}
	got := strings.Fields(block)
	sort.Strings(got)
	want := rastrillo.IconSlugs()
	if len(got) != len(want) {
		t.Fatalf("%s lists %d slugs, the framework answers %d:\ngot  %v\nwant %v", path, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: slug %d is %q, the framework answers %q", path, i, got[i], want[i])
		}
	}
}

// Font Awesome's licence asks that the attribution comments in its
// distributed files not be removed. Transcribing path data into Go drops
// them, so the generated source carries one back, next to the data.
func TestFontAwesomeCreditTravelsWithTheSource(t *testing.T) {
	for _, d := range []Delivery{Inline, CDN, JS} {
		got, err := Render("font-awesome", d)
		if err != nil {
			t.Fatal(err)
		}
		src := string(got.Source)
		for _, want := range []string{
			"Font Awesome Free 7.3.1 by @fontawesome",
			"https://fontawesome.com/license/free",
			"CC BY 4.0",
		} {
			if !strings.Contains(src, want) {
				t.Errorf("font-awesome/%s generated source lost %q", d, want)
			}
		}
	}
	// Lucide is ISC and asks for nothing extra in the source.
	got, _ := Render("lucide", Inline)
	if strings.Contains(string(got.Source), "fontawesome") {
		t.Error("lucide source mentions font-awesome")
	}
}
