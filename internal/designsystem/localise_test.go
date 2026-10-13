package designsystem

import (
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// English is the source, so translating a sample into English must
// change nothing: not a byte of markup, not a value in the data. A
// walker that dropped an attribute or re-spaced a line would pass every
// other gate in English and show up only as a difference between
// languages nobody compares.
func TestLocalisingIntoEnglishChangesNothing(t *testing.T) {
	var markups []string
	for _, s := range ui.Styleguide() {
		markups = append(markups, s)
	}
	for _, d := range formatDocs() {
		markups = append(markups, d.Markup)
	}
	for _, d := range screenDocs() {
		markups = append(markups, d.Markup)
	}
	markups = append(markups, string(accountMarkup))
	for _, m := range markups {
		if got := localMarkup("en", m); got != m {
			t.Errorf("localMarkup changed English markup\n got %q\nwant %q", got, m)
		}
	}
	for _, fam := range families() {
		for _, doc := range fam.Partials {
			for _, s := range doc.States {
				if got := localData("en", s.Data); !reflect.DeepEqual(got, plainData(s.Data)) {
					t.Errorf("%s (%s): localData changed English data\n got %#v\nwant %#v", doc.Name, s.State, got, s.Data)
				}
			}
		}
	}
}

// plainData is data with every verb turned back into the string it
// marks, which is what English localData is expected to return.
func plainData(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = plainData(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = plainData(e)
		}
		return out
	case verb:
		return string(v)
	}
	return v
}

// What the walkers translate and what they leave, on the cases that
// decide it: a name, an address, a code, a route and a field's name stay;
// a sentence around a name is translated with the name kept; a run of
// words beside inline elements is one sentence; and a sentence beside a
// control is not, because the control is not part of the sentence.
func TestTheWalkersTranslateWordsAndKeepStandIns(t *testing.T) {
	for s, keep := range map[string]bool{
		"Grace Hopper": true, "grace@example.com": true, "AB3PX": true, "/orders/1": true,
		"notify_on": true, "€48,210": true, "D02 XY45": true, "4.1k": true, "#nav": true,
		"Order AB3PX": false, "Actions for Grace Hopper": false, "Save": false, "1h 12m": false,
	} {
		if got := keepsAsWritten(s); got != keep {
			t.Errorf("keepsAsWritten(%q) = %v, want %v", s, got, keep)
		}
	}
	var asked []string
	stop := setProseTrace(func(en string) { asked = append(asked, en) })
	localMarkup("ja", `<p rst-count-line>Displaying <strong>1–20</strong> of <strong>412</strong></p>`+
		`<button><span rst-spin aria-hidden="true"></span>Publish</button>`+
		`<a rst-shell-brand href="/">Notes</a><span rst-person-name>Ada Lovelace</span>`+
		`<small>AB3PX · <bdi>grace@example.com</bdi></small>`+
		`<a href="/x" aria-label="Help: orders" data-tip="About orders"><svg><path d="M0 0"/></svg></a>`+
		`<select><optgroup label="Europe"><option value="dublin">Dublin</option></optgroup></select>`)
	localData("ja", map[string]any{"Name": "notify_on", "Label": "Email me about replies", "Href": "/a",
		"Hidden": [][2]string{{"state", "archived"}}, "Query": "spam", "Tone": "positive", "Items": []any{
			map[string]any{"Label": verb("Archive")},
		}})
	stop()
	want := []string{
		"Displaying <strong>1–20</strong> of <strong>412</strong>", "Publish",
		"Help: orders", "About orders", "Europe", "Dublin",
		"Email me about replies", "spam", "Archive" + verbSense,
	}
	sort.Strings(asked)
	sort.Strings(want)
	if !slices.Equal(asked, want) {
		t.Errorf("the walkers looked up\n%q\nwant\n%q", asked, want)
	}
}

// standInRun is what a translation must carry over from its key: the
// names and brands, every email address, and every code that mixes
// letters and digits.
var standInRun = regexp.MustCompile(`\S+@\S+\.\w+|#\d+|\b[A-Z]+[0-9][A-Z0-9]*\b`)

func TestSampleTranslationsKeepTheirStandIns(t *testing.T) {
	for key, row := range sampleProse {
		var kept []string
		for _, s := range sampleStandIns {
			if strings.Contains(key, s) {
				kept = append(kept, s)
			}
		}
		kept = append(kept, standInRun.FindAllString(stripTags(key), -1)...)
		for _, locale := range nonEnglish() {
			for _, s := range kept {
				if !strings.Contains(row[locale], s) {
					t.Errorf("the %s translation of %q lost %q: %q", locale, key, s, row[locale])
				}
			}
		}
	}
}

// tagSkeleton is a run of markup's elements in a comparable form: each
// tag with its spoken attribute values blanked, sorted, because a
// language may put the two numbers of a count line the other way round
// but may not drop one, add one or change an href.
func tagSkeleton(s string) []string {
	var out []string
	for _, t := range tokenise(s) {
		if t.tag == "" {
			continue
		}
		raw := spokenAttr.ReplaceAllStringFunc(t.raw, func(m string) string {
			sub := spokenAttr.FindStringSubmatch(m)
			if spokenAttrs[sub[1]] {
				return sub[1] + `=""`
			}
			return m
		})
		out = append(out, raw)
	}
	sort.Strings(out)
	return out
}

// A key with markup in it is a sentence with elements inside, and its
// translation is inserted into the sample as markup. So every
// translation carries exactly the elements its key does, attributes and
// all bar the spoken ones; and a key with no markup gets a translation
// with none, and with nothing that could close the attribute it may be
// written into.
func TestSampleMarkupSurvivesTranslation(t *testing.T) {
	for key, row := range sampleProse {
		want := tagSkeleton(key)
		for _, locale := range nonEnglish() {
			tr := row[locale]
			if got := tagSkeleton(tr); !slices.Equal(got, want) {
				t.Errorf("the %s translation of %q has elements %q, want %q", locale, key, got, want)
			}
			if len(want) == 0 && strings.ContainsAny(tr, `<>"&`) {
				t.Errorf("the %s translation of %q carries markup characters: %q", locale, key, tr)
			}
		}
	}
}

// englishSampleLabels are sample words that must not be on a page in
// another language. Short ones are matched as an element's whole text
// or a template string, which is how a label is written; a bare "Save"
// would also match "Saved" in prose.
var englishSampleLabels = []string{
	"Save", "Cancel", "Edit", "Publish", "Settings",
	"Email me about replies", "Save post", "Back to posts", "Revenue this month",
	"Search posts", "Publish on", "Doors open", "Release notes, August",
	"Changes save immediately.", "Continue with Google", "Invoice #4471 never arrived",
	"Everything on a screen sits inside boxes.", "Moorings and berths, booked in a minute.",
}

// The samples speak the page's language. Every document a translated
// directory holds, the component pages, the preview frames, the demos,
// is swept for a fixed list of the samples' English: a button label, a
// field label, a row title, a brand's pitch. The prose gates catch a
// missing translation; this catches a sample that never asked for one,
// a string a walker decided was a stand-in when it was a word.
func TestNoEnglishSampleLabelReachesATranslatedPage(t *testing.T) {
	files := render(t)
	var swept int
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") || localeOfPath(name) == "en" || !strings.HasPrefix(name, RootTheme()+"/") {
			continue
		}
		swept++
		page := string(body)
		for _, label := range englishSampleLabels {
			var hit bool
			if len(label) < 12 {
				hit = strings.Contains(page, ">"+label+"<") || strings.Contains(page, `&#34;`+label+`&#34;`) || strings.Contains(page, `"`+label+`"`)
			} else {
				hit = strings.Contains(page, label)
			}
			if hit {
				t.Errorf("%s carries the English sample label %q", name, label)
			}
		}
	}
	if want := len(rastrillo.BaseLocales()) - 1; swept < want*len(pageKinds()) {
		t.Errorf("swept %d translated documents, fewer than one per page kind per locale", swept)
	}
	// The labels have to be on the English pages for their absence
	// elsewhere to mean anything.
	var en strings.Builder
	for name, body := range files {
		if strings.HasPrefix(name, RootTheme()+"/en/") {
			en.Write(body)
		}
	}
	for _, label := range englishSampleLabels {
		if !strings.Contains(en.String(), label) {
			t.Errorf("%q is on no English page; the sweep for it checks nothing", label)
		}
	}
}
