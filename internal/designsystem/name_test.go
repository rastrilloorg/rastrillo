package designsystem

import (
	"html/template"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// galleryName is what the gallery calls itself: Paul's name, in
// sentence case, and the prose key its translations hang off.
const galleryName = "Rastrillo components"

// designSystemWords is "design system" as each locale's prose wrote it
// when the gallery called itself one. Irish is cut to the letters every
// mutation of córas keeps (gcóras, chóras).
var designSystemWords = map[string][]string{
	"en":      {"design system"},
	"ga":      {"óras dearaidh"},
	"zh-Hans": {"设计系统"},
	"es":      {"sistema de diseño"},
	"hi":      {"डिज़ाइन सिस्टम"},
	"pt-PT":   {"sistema de design"},
	"bn":      {"ডিজাইন সিস্টেম"},
	"ru":      {"дизайн-систем"},
	"ja":      {"デザインシステム"},
	"yue":     {"設計系統"},
	"vi":      {"hệ thống thiết kế"},
	"ar":      {"نظام تصميم", "نظام التصميم"},
}

var topBrand = regexp.MustCompile(
	`<a class="ds-top__brand" href="[^"]*">([^<]*)</a>`)

// The gallery is Rastrillo components, the name Paul gave it once
// Fiesta took over the look and the design system that documents it.
// Every document but a standalone preview says so in its title, the
// gallery pages in their top bar too and the Overview in its h1, and the
// demo, modal and shell pages lead back to it by name. A preview is a
// sample on its own and carries neither name nor chrome. "Design
// system" is still said, in any locale, only about Fiesta's; the prose
// gates already hold every rendered key translated and every
// translated key rendered.
func TestTheGalleryCallsItselfRastrilloComponents(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			dir := theme + "/" + locale + "/"
			name := proseIn(locale, galleryName)
			for _, pk := range pageKinds() {
				where := dir + pk.File
				page := string(files[where])
				title := docTitle(proseIn(locale, pk.Title), name, theme)
				hasTitle(t, where, page, title)
				brand := topBrand.FindAllStringSubmatch(page, -1)
				if len(brand) != 1 || brand[0][1] != escape(name) {
					t.Errorf("%s: top bar brand %v, want %q",
						where, brand, name)
				}
				h1 := `<h1 rst-shell-title><span></span><span>` +
					escape(name) + `</span></h1>`
				if pk.Kind == "overview" && !strings.Contains(page, h1) {
					t.Errorf("%s: the h1 is not %q", where, name)
				}
			}
			back := ">" + escape(proseIn(locale, "Back to "+galleryName)) +
				"</a>"
			for where, doc := range files {
				page := string(doc)
				if !strings.HasPrefix(where, dir) ||
					!strings.HasSuffix(where, ".html") {
					continue
				}
				file := strings.TrimPrefix(where, dir)
				switch {
				case isPreviewDoc(page):
					if strings.Contains(titleOf(page), name) ||
						strings.Contains(page, "ds-top__brand") {
						t.Errorf("%s: a standalone preview carries "+
							"the gallery's name or chrome", where)
					}
				case file == "modal.html" ||
					strings.HasPrefix(file, "shells/"):
					endsWithName(t, where, page, name)
					if !strings.Contains(page, back) {
						t.Errorf("%s: no link back saying %s",
							where, back)
					}
				case strings.HasPrefix(file, "demo"):
					endsWithName(t, where, page, name)
				}
				saysDesignSystemOnlyOfFiesta(t, where, page, locale)
			}
		}
	}
}

func escape(text string) string { return template.HTMLEscapeString(text) }

func titleOf(page string) string {
	if match := titleTag.FindStringSubmatch(page); match != nil {
		return match[1]
	}
	return ""
}

func hasTitle(t *testing.T, where, page, want string) {
	t.Helper()
	if got := titleOf(page); got != escape(want) {
		t.Errorf("%s: <title> %q, want %q", where, got, want)
	}
}

func endsWithName(t *testing.T, where, page, name string) {
	t.Helper()
	if got := titleOf(page); !strings.HasSuffix(got, titleSep+escape(name)) {
		t.Errorf("%s: <title> %q does not end with %q", where, got, name)
	}
}

// saysDesignSystemOnlyOfFiesta reads the page as runs of text and
// tags, split at every angle bracket, so a run that says "design
// system" has to name Fiesta itself rather than sit near a link that
// does.
func saysDesignSystemOnlyOfFiesta(t *testing.T, where, page, locale string) {
	t.Helper()
	runs := strings.FieldsFunc(page, func(char rune) bool {
		return char == '<' || char == '>'
	})
	words := designSystemWords[locale]
	if locale != "en" {
		words = append(words, designSystemWords["en"]...)
	}
	for _, run := range runs {
		lower := strings.ToLower(run)
		for _, word := range words {
			if strings.Contains(lower, word) &&
				!strings.Contains(run, "Fiesta") {
				t.Errorf("%s: %q calls something other than "+
					"Fiesta a design system", where, run)
			}
		}
	}
}
