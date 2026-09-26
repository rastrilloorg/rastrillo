package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// A country picker is where select.js's ranking was learned (Tito Go's
// phone field), so it is what these tests search: each row written the
// way a phone field writes it — "Name (+code)", the ISO code and the
// calling code as data-rst-terms, data-rst-first on the country that owns
// a shared code. The list is curated, not complete: every country an
// assertion names is here, and so is every country that competes with it
// for the query (Guinea-Bissau for "guin", Oman for "man", the four +44s).
type countryRow struct{ iso, en, de, code string }

// primary marks the country that owns a calling code others share.
var primary = map[string]bool{"GB": true, "US": true, "RU": true, "RE": true}

var countries = []countryRow{
	{"DZ", "Algeria", "Algerien", "213"},
	{"AS", "American Samoa", "Amerikanisch-Samoa", "1684"},
	{"AT", "Austria", "Österreich", "43"},
	{"KH", "Cambodia", "Kambodscha", "855"},
	{"CM", "Cameroon", "Kamerun", "237"},
	{"CA", "Canada", "Kanada", "1"},
	{"CI", "Côte d’Ivoire", "Côte d’Ivoire", "225"},
	{"DK", "Denmark", "Dänemark", "45"},
	{"GQ", "Equatorial Guinea", "Äquatorialguinea", "240"},
	{"GE", "Georgia", "Georgien", "995"},
	{"DE", "Germany", "Deutschland", "49"},
	{"GG", "Guernsey", "Guernsey", "44"},
	{"GN", "Guinea", "Guinea", "224"},
	{"GW", "Guinea-Bissau", "Guinea-Bissau", "245"},
	{"IN", "India", "Indien", "91"},
	{"ID", "Indonesia", "Indonesien", "62"},
	{"IR", "Iran", "Iran", "98"},
	{"IE", "Ireland", "Irland", "353"},
	{"IM", "Isle of Man", "Isle of Man", "44"},
	{"IL", "Israel", "Israel", "972"},
	{"JM", "Jamaica", "Jamaika", "1876"},
	{"JP", "Japan", "Japan", "81"},
	{"JE", "Jersey", "Jersey", "44"},
	{"JO", "Jordan", "Jordanien", "962"},
	{"KZ", "Kazakhstan", "Kasachstan", "7"},
	{"YT", "Mayotte", "Mayotte", "262"},
	{"NE", "Niger", "Niger", "227"},
	{"NG", "Nigeria", "Nigeria", "234"},
	{"OM", "Oman", "Oman", "968"},
	{"PG", "Papua New Guinea", "Papua-Neuguinea", "675"},
	{"RE", "Réunion", "Réunion", "262"},
	{"RU", "Russia", "Russland", "7"},
	{"SS", "South Sudan", "Südsudan", "211"},
	{"SD", "Sudan", "Sudan", "249"},
	{"AE", "United Arab Emirates", "Vereinigte Arabische Emirate", "971"},
	{"GB", "United Kingdom", "Vereinigtes Königreich", "44"},
	{"US", "United States", "Vereinigte Staaten", "1"},
}

type rankOption struct {
	Value string   `json:"value"`
	Name  string   `json:"name"`
	Text  string   `json:"text"`
	Terms []string `json:"terms"`
	First bool     `json:"first"`
}

func countryOptions(lang string) []rankOption {
	var out []rankOption
	for _, c := range countries {
		name := c.en
		if lang == "de" {
			name = c.de
		}
		text := name + " (+" + c.code + ")"
		out = append(out, rankOption{Value: c.iso, Name: text, Text: text, Terms: []string{c.iso, "+" + c.code}, First: primary[c.iso]})
	}
	return out
}

func runRank(t *testing.T, lang string, queries []string) (map[string][]string, map[string]string) {
	t.Helper()
	in, err := json.Marshal(map[string]any{"options": countryOptions(lang), "queries": queries})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Order   map[string][]string `json:"order"`
		Settled map[string]string   `json:"settled"`
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"select_node.mjs"}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	return got.Order, got.Settled
}

// Leaving the box after typing (Tab, or a tap on the next field) commits
// the highlighted row only when the search can mean one: a single match,
// an exact hit on three characters, or a calling code its country owns.
// Two letters never do: "ge" is Georgia's code and the start of Germany,
// and a wrong calling code under a phone number is worse than an unpicked
// box the form then asks about.
func TestLeavingTheBoxCommitsOnlyWhatASearchCanMean(t *testing.T) {
	t.Parallel()
	_, settled := runRank(t, "en", []string{"irel", "germ", "india", "ger", "ire", "+44", "44", "+1", "1", "+7", "+971", "ge", "ir", "ind", "zzzz"})
	for q, want := range map[string]string{
		"irel": "IE", "germ": "DE", "india": "IN", "ger": "DE", "ire": "IE",
		"+44": "GB", "44": "GB", "+1": "US", "1": "US", "+7": "RU", "+971": "AE",
		"ge": "", "ir": "", "ind": "", "zzzz": "",
	} {
		if got := settled[q]; got != want {
			t.Errorf("leaving after %q commits %q, want %q", q, got, want)
		}
	}
}

// Two rows that are both an exact hit settle nothing: the brackets are
// glosses, so "Springfield" names "Springfield (IL)" and
// "Springfield (MA)" equally, and leaving the box must not pick one.
func TestLeavingTheBoxNeverPicksBetweenTwoExactHits(t *testing.T) {
	t.Parallel()
	opts := []rankOption{
		{Value: "IL", Name: "Springfield (IL)", Text: "Springfield (IL)"},
		{Value: "MA", Name: "Springfield (MA)", Text: "Springfield (MA)"},
		{Value: "SH", Name: "Shelbyville", Text: "Shelbyville"},
		{Value: "PA", Name: "Portland (OR)", Text: "Portland (OR)", First: true},
		{Value: "PM", Name: "Portland (ME)", Text: "Portland (ME)"},
	}
	in, err := json.Marshal(map[string]any{"options": opts, "queries": []string{"springfield", "shelbyville", "portland"}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Settled map[string]string `json:"settled"`
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"select_node.mjs"}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	if got.Settled["springfield"] != "" {
		t.Errorf("leaving after \"springfield\" picked %q between two exact hits, want nothing", got.Settled["springfield"])
	}
	// Exactly one of the exact ties marked first is the single best row,
	// the rule a shared calling code settles by.
	if got.Settled["portland"] != "PA" {
		t.Errorf("leaving after \"portland\" picked %q, want PA: one exact tie is marked first", got.Settled["portland"])
	}
	if got.Settled["shelbyville"] != "SH" {
		t.Errorf("leaving after \"shelbyville\" picked %q, want SH: a lone exact hit still settles", got.Settled["shelbyville"])
	}
}

// Someone types a country's name in their own language, its ISO code, or
// the calling code with or without its "+", and the country they meant is
// the first row — the one Enter takes. Substring matching alone got most
// of these wrong: "ae" is inside "Israel", and "+1" is inside a dozen
// codes, alphabetically led by American Samoa.
func TestSearchRanksTheRightRowFirst(t *testing.T) {
	t.Parallel()
	en, _ := runRank(t, "en", []string{"c", "j", "44", "262", "+971", "971", "ae", "de", "gb", "united k", "man", "sudan", "suda", "guin", "+44", "+1", "cote", "cote d'ivoire", "ireland", "zzzz"})
	for q, want := range map[string]string{
		"c":             "KH", // one letter reads alphabetically
		"j":             "JM", // owning a calling code only breaks a tie on the code
		"44":            "GB", // a code typed whole without its "+" is still exact
		"262":           "RE",
		"+971":          "AE", // the calling code, as read off a number
		"971":           "AE", // ...and without the plus
		"ae":            "AE", // an ISO code beats a name that merely contains the letters
		"de":            "DE", // ...and one that starts with them (Denmark)
		"gb":            "GB",
		"united k":      "GB", // the start of a name
		"man":           "IM", // the start of a WORD beats the middle of one (Oman, Germany)
		"sudan":         "SD", // the start of the name beats a later word (South Sudan)
		"suda":          "SD",
		"guin":          "GN", // Guinea before Equatorial Guinea and Papua New Guinea
		"+44":           "GB", // a shared code opens on the country that owns it
		"+1":            "US",
		"cote":          "CI", // accents fold away: Côte d’Ivoire
		"cote d'ivoire": "CI", // ...and a typed apostrophe is its curly one
		"ireland":       "IE",
	} {
		if len(en[q]) == 0 || en[q][0] != want {
			head := en[q]
			if len(head) > 4 {
				head = head[:4]
			}
			t.Errorf("en %q: first rows %v, want %s first", q, head, want)
		}
	}
	if len(en["zzzz"]) != 0 {
		t.Errorf("a query nothing matches found %v", en["zzzz"])
	}
	// The name someone reads is in their language, so that is what they type.
	de, _ := runRank(t, "de", []string{"osterreich", "vereinigte"})
	if len(de["osterreich"]) == 0 || de["osterreich"][0] != "AT" {
		t.Errorf("de \"osterreich\": got %v, want AT first", de["osterreich"])
	}
	if len(de["vereinigte"]) != 3 {
		t.Errorf("de \"vereinigte\": got %v, want the three Vereinigte", de["vereinigte"])
	}
}

// The blank of a REQUIRED select ("Choose one") is a prompt, not an
// answer: the box shows nothing for it, so its placeholder is what is
// read, and the list never offers it — choosing it would only leave the
// question unanswered. A marked prompt ("Country") drops out once
// something is typed. An optional select's blank ("No answer") is a real
// answer everywhere, and a disabled blank never is.
func TestRequiredBlankIsAPrompt(t *testing.T) {
	t.Parallel()
	type blank struct {
		Name      string `json:"-"`
		Value     string `json:"value"`
		Marked    bool   `json:"marked"`
		Required  bool   `json:"required"`
		Searching bool   `json:"searching"`
		Disabled  bool   `json:"disabled"`
		Prompt    bool   `json:"-"`
		Offered   bool   `json:"-"`
	}
	cases := []blank{
		{Name: "required blank, idle", Required: true, Prompt: true, Offered: false},
		{Name: "required blank, searching", Required: true, Searching: true, Prompt: true, Offered: false},
		{Name: "marked prompt, idle", Marked: true, Prompt: true, Offered: true},
		{Name: "marked prompt, searching", Marked: true, Searching: true, Prompt: true, Offered: false},
		{Name: "marked required prompt, idle", Marked: true, Required: true, Prompt: true, Offered: true},
		{Name: "marked required prompt, searching", Marked: true, Required: true, Searching: true, Prompt: true, Offered: false},
		{Name: "optional blank, idle", Prompt: false, Offered: true},
		{Name: "optional blank, searching", Searching: true, Prompt: false, Offered: true},
		// A page that renders "Choose one" disabled means it, and a select
		// that then stops being required must not bring it back as a pick.
		{Name: "disabled blank on a select no longer required", Disabled: true, Prompt: true, Offered: false},
		{Name: "a real answer on a required select", Value: "AE", Required: true, Searching: true, Prompt: false, Offered: true},
	}
	in, err := json.Marshal(map[string]any{"blanks": cases})
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Prompt  bool `json:"prompt"`
		Offered bool `json:"offered"`
	}
	if err := json.Unmarshal(nodetest.Run(t, nodetest.Cmd{Args: []string{"select_node.mjs"}, Stdin: in}), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d answers for %d cases", len(got), len(cases))
	}
	for i, c := range cases {
		if got[i].Prompt != c.Prompt {
			t.Errorf("%s: prompt = %v, want %v (the box would show it as the pick)", c.Name, got[i].Prompt, c.Prompt)
		}
		if got[i].Offered != c.Offered {
			t.Errorf("%s: offered = %v, want %v", c.Name, got[i].Offered, c.Offered)
		}
	}
}

// The rules above are only true of a screen if the combobox calls them.
// A source check, because the wiring is one line each; the browser drives
// in browser_test.go hold the behaviour.
func TestSelectUsesItsPromptRules(t *testing.T) {
	t.Parallel()
	src := string(SelectJS())
	for _, want := range []string{
		"prompt: isPrompt(o.value, d.rstPrompt !== undefined, native.required, o.disabled),",
		"offered(native.options[o.index].value, o.marked, native.required, searching, o.disabled)",
		// Up from nothing highlighted goes to the last row.
		"setActive((active ? v[Math.max(v.indexOf(active) - 1, 0)] : v[v.length - 1]) || null);",
		// An unanswered question opens with nothing highlighted, so a bare
		// Enter cannot answer it with the first row...
		"if (unanswered()) return null;",
		// ...opening is where an idle list lands, from openList...
		"markSelected();\n      setActive(opening());",
		// ...and from a cleared search.
		"if (!searching && !steered && (requeried || unanswered())) setActive(opening());",
		// A fallback highlight is never a prompt or a page's extra row.
		"visible().find((o) => !o.run && !o.prompt)",
		// A highlight that is no longer on the list is replaced.
		"!shown.includes(active)",
		// With the list open, Enter never submits the form.
		"if (open) {\n            e.preventDefault();\n            tookEnter = true;\n            if (active) choose(active);",
		// A select that stops being required re-derives its prompts and
		// re-marks the selection.
		"if (!o.run) o.prompt = isPrompt(native.options[o.index].value, o.marked, native.required, native.options[o.index].disabled);\n      }\n      markSelected();",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("select.js no longer calls %q", want)
		}
	}
}
