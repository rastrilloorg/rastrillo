package ui

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// fixturesComplete flipped true the day every shipped locale got a
// fixture file. TestEveryShippedLocaleHasADatetimeFixture is a gate
// from here on: a new locale in locales/ with no fixture beside it
// fails the build, because a catalog nobody has typed a date into is a
// language this parser has not been shown to read.
const fixturesComplete = true

// vocabStdin is what the browser reads off data-rst-date-words, built
// the same way {{dateWords}} builds it: the framework's own catalog for
// each locale, the seventeen vocabulary keys, and nothing else. The
// fixtures deliberately do not carry their own copy — words under test
// have to be the words that ship, or a translator's improvement would
// go green against a stale duplicate.
//
// Every shipped catalog goes over, not just the fixture's own: a case
// may name its own language with a "lang" key, which is what
// testdata/datetime/regressions.json is made of.
func vocabStdin(t *testing.T, locale string) []byte {
	t.Helper()
	catalogs := make(map[string]map[string]string)
	for code, catalog := range rastrillo.BaseCatalogs() {
		words := make(map[string]string, len(dateWordNames))
		for _, name := range dateWordNames {
			value := catalog["rastrillo.ui.date_"+name]
			if value == "" {
				t.Fatalf("%s: base catalog has no rastrillo.ui.date_%s", code, name)
			}
			words[name] = value
		}
		catalogs[code] = words
	}
	if _, ok := catalogs[locale]; !ok {
		t.Fatalf("no base catalog for %q", locale)
	}
	encoded, err := json.Marshal(map[string]any{"locale": locale, "catalogs": catalogs})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// localeOf reads a fixture's default language off its filename. A file
// not named after a shipped locale is a cross-locale one — every case
// in it names its own language — and English is its default.
func localeOf(fixture string) string {
	name := strings.TrimSuffix(filepath.Base(fixture), ".json")
	for _, code := range rastrillo.BaseLocales() {
		if code == name {
			return code
		}
	}
	return "en"
}

// The parser is the risk in datetime.js, and it is the half no Go test
// can reach on its own: it reads month and weekday names out of Intl,
// folds the digits a locale writes, and answers in wall-clock dates.
// datetime_node.mjs runs it under plain Node against one fixture per
// locale.
//
// Node is not a build dependency of this framework, so its absence
// skips — LOUDLY, with the command that would have run, because a
// silent skip on a CI image without node would retire this suite
// without anybody deciding to.
func TestDatetimeParserFixtures(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH, so datetime.js's parser is unverified here: " +
			"install Node and rerun `go test ./ui/` to exercise ui/datetime_node.mjs")
	}

	fixtures, err := filepath.Glob(filepath.Join("testdata", "datetime", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures in testdata/datetime")
	}
	if _, err := os.Stat(filepath.Join("testdata", "datetime", "en.json")); err != nil {
		t.Fatalf("en.json is the reference fixture and must exist: %v", err)
	}

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".json")
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(node, "datetime_node.mjs", fixture)
			cmd.Stdin = bytes.NewReader(vocabStdin(t, localeOf(fixture)))
			// The fixtures are wall-clock: a runner in a zone with a
			// summer-time jump inside a fixture's span would move an
			// expectation for reasons that have nothing to do with the
			// parser. UTC has no such jump.
			cmd.Env = append(os.Environ(), "TZ=UTC")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s\n%v", out, err)
			}
			t.Log(strings.TrimSpace(string(out)))
		})
	}
}

// A field has to be able to read what it just wrote. Every fixture is a
// phrase somebody chose to type, so no fixture could catch the opposite
// failure: the combobox formatting a value with Intl and then refusing
// its own formatting back. It was refusing it in five of the twelve
// shipped languages — ja and zh-Hans write 年 and a bracketed weekday,
// pt writes "de" linkers, ru writes a "г." suffix, yue writes a
// dayPeriod (下晝) no catalog spells — which made an in-place edit
// impossible in each of them and looked like nothing at all from here.
//
// So this drives datetime_node.mjs's --round-trip mode: format three
// instants per kind with the display's own options, parse each back,
// and compare. All twelve have to survive the trip. The first cut of
// this gate allowed eleven, which meant a single-locale regression
// exited 0 and left its only trace in a t.Log — a passing test nobody
// reads. The harness now exits non-zero the moment one locale fails and
// names the languages on the FAIL line, which is what lands in the
// t.Fatalf below.
func TestDatetimeReadsItsOwnDisplayBack(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH, so datetime.js's read-back is unverified here: " +
			"install Node and rerun `go test ./ui/` to exercise ui/datetime_node.mjs --round-trip")
	}
	cmd := exec.Command(node, "datetime_node.mjs", "--round-trip")
	cmd.Stdin = bytes.NewReader(vocabStdin(t, "en"))
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n%v", out, err)
	}
	t.Log(strings.TrimSpace(string(out)))
}

// The calendar overlay draws six weeks of days, and the arithmetic
// behind that is the half of it that can be quietly wrong: a grid that
// drops a day in a zone that moved its clocks that morning, a week that
// starts on the wrong day, column headings that do not describe their
// own columns. None of that shows up in a screenshot — a calendar
// missing 1 March looks exactly like a calendar — and none of it needs
// a browser to catch, because ui/calendar.js keeps that arithmetic in
// front of its own document guard for precisely this reason.
//
// --calendar walks every shipped locale over twenty-five months (both
// of 2026 and 2027, plus a leap February) and holds each grid to its
// invariants: forty-two cells, every one at local midnight, each one
// day after the last, starting on that locale's own first day of the
// week, with every day of the month present exactly once and the 1st in
// the first row. Four undisputed week-start facts are pinned there too
// — the derivation keeps no table, so the test carries the anchor.
//
// The DST case is not hypothetical: building the grid by adding
// 86,400,000 milliseconds instead of counting whole days fails here in
// every locale, on the two months a clock changes, and passes
// everywhere else. TZ is deliberately NOT pinned to UTC, unlike the
// fixture runs — a zone with no clock changes is the one zone that
// cannot catch it, and the invariants hold in every zone by
// construction.
func TestCalendarGridHoldsInEveryLocale(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH, so calendar.js's grid is unverified here: " +
			"install Node and rerun `go test ./ui/` to exercise ui/datetime_node.mjs --calendar")
	}
	cmd := exec.Command(node, "datetime_node.mjs", "--calendar")
	cmd.Stdin = bytes.NewReader(vocabStdin(t, "en"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n%v", out, err)
	}
	t.Log(strings.TrimSpace(string(out)))
}

// calendar.js holds to the same contract as the other three scripts:
// its own vocabulary, a native-input-free widget built in the browser,
// a document guard so Node can load its arithmetic, and no English
// month or weekday name anywhere in it.
func TestCalendarContract(t *testing.T) {
	js := string(CalendarJS())
	for _, want := range []string{
		// The seam datetime.js looks up at enhance time. If this name
		// moves, the button silently falls back to the browser's own
		// picker — which is the failure this whole branch removed.
		"rastrilloCalendar",
		// The roles a calendar has to carry to be usable without sight:
		// a named dialog, a grid, real column headers, the committed
		// day marked as selected and today marked as today.
		`"dialog"`, `"grid"`, `"columnheader"`, `"gridcell"`,
		"aria-selected", "aria-current", "aria-disabled",
		// The keys it has to answer to.
		"ArrowLeft", "ArrowRight", "PageUp", "PageDown", "Home", "End", "Escape",
		// The three strings it puts on screen all arrive translated.
		"data-rst-day", "tabIndex",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("calendar.js does not mention %q", want)
		}
	}
	if !strings.Contains(js, `typeof document !== "undefined"`) {
		t.Error("calendar.js must keep its panel behind a document guard, or the Node harness cannot load it")
	}
	if !strings.Contains(js, "module.exports") {
		t.Error("calendar.js must export its arithmetic for ui/datetime_node.mjs --calendar")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("calendar.js must stay CSP-clean")
	}
	if bytes.Contains(CalendarJS(), []byte("\t")) {
		t.Error("calendar.js uses two-space indentation, not tabs")
	}
	// Its own budget, set where datetime.js's was first set and for the
	// same reason: an app owner has to be able to read the whole file in
	// one sitting, and the next few hundred bytes should be a decision
	// rather than a drift. It lands at about 15KB, roughly half of it
	// the prose explaining the other half.
	if n := len(CalendarJS()); n > 24*1024 {
		t.Fatalf("calendar.js is %d bytes; split something out before it stops being readable", n)
	}
}

// The same rule datetime.js lives under, for the same reason: a month
// or weekday name written down here would work in English and fail
// silently in every other language the framework ships. calendar.js
// asks Intl, in the page's own lang, for all of it — the names, the
// digits, and the day a week starts on.
func TestCalendarDerivesItsNames(t *testing.T) {
	body := string(CalendarJS())
	body = body[strings.Index(body, "(function ("):]
	for _, name := range []string{
		"january", "february", "december", "\"jan\"", "\"dec\"",
		"monday", "friday", "sunday", "\"mon\"", "\"fri\"",
	} {
		if strings.Contains(body, name) {
			t.Errorf("calendar.js spells %s itself; month and weekday names come from Intl", name)
		}
	}
}

// Every shipped locale gets its own fixture, because the vocabulary is
// the part of this parser that cannot be derived: a missing fixture is
// a language nobody has checked can type a date.
func TestEveryShippedLocaleHasADatetimeFixture(t *testing.T) {
	if !fixturesComplete {
		t.Skip("fixtures land locale by locale; en is the only one asserted today " +
			"(flip fixturesComplete in ui/datetime_test.go when the rest land)")
	}
	for _, locale := range rastrillo.BaseLocales() {
		if _, err := os.Stat(filepath.Join("testdata", "datetime", locale+".json")); err != nil {
			t.Errorf("no parser fixture for shipped locale %q: %v", locale, err)
		}
	}
}

// datetime.js holds to the same contract as the shim and select.js: the
// vocabulary the docs promise, the ARIA convention, an idempotent scan,
// and a native input that survives enhancement because it is what the
// form submits.
func TestDatetimeContract(t *testing.T) {
	js := string(DatetimeJS())
	for _, want := range []string{
		"data-rst-date", "data-rst-time", "data-rst-date-words",
		"data-rst-date-set", "data-rst-date-hint", "data-rst-date-pick",
		"data-rst-date-results", "data-rst-date-result-one",
		"data-rst-date-quick-today", "data-rst-date-quick-tomorrow",
		"data-rst-date-quick-next-week", "data-rst-date-quick-plus-1h",
		"data-rst-date-quick-plus-2h", "data-rst-date-quick-end-of-day",
		"data-rst-date-quick-next-day", "data-rst-range",
		// The calendar overlay's own strings, and a time field's own
		// button label — the overlay builds its markup in the browser,
		// where the catalog is out of reach, so every word it shows has
		// to arrive on an attribute.
		"data-rst-date-pick-time", "data-rst-date-calendar",
		"data-rst-date-prev-month", "data-rst-date-next-month",
		// The seam to ui/calendar.js. If this name moves and the other
		// file is not moved with it, nothing breaks loudly: the button
		// quietly falls back to the browser's own picker, which is the
		// unusable control this whole overlay replaced.
		"rastrilloCalendar",
		// The completion layer, which is what lets "5j" read as a date
		// at all — and parse, which stays strict underneath it.
		"parseAll", "function parse(",
		// The convention, pinned: an ARIA combobox that mirrors rather
		// than replaces, announced to assistive tech.
		"combobox", "aria-activedescendant", "aria-expanded", "aria-live",
		"rst-sr-only",
		// The month and weekday names are Intl's, never a table.
		"formatToParts",
		// showPicker survives as the fallback for a page that links
		// this file without calendar.js, and nowhere else.
		"showPicker",
		// {example} is substituted here, in the locale's own format —
		// the partials emit the hint template verbatim.
		"{example}", "{n}",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("datetime.js does not mention %q", want)
		}
	}
	for _, bad := range []string{".remove()", "removeChild", "outerHTML ="} {
		if strings.Contains(js, bad) {
			t.Errorf("datetime.js destroys DOM (%q); the native input must survive", bad)
		}
	}
	if !strings.Contains(js, "rstEnhanced") {
		t.Error("datetime.js is not idempotent; re-scanning would double-enhance")
	}
	if !strings.Contains(js, `typeof document !== "undefined"`) {
		t.Error("datetime.js must keep its page half behind a document guard, or the Node harness cannot load it")
	}
	if !strings.Contains(js, "module.exports") {
		t.Error("datetime.js must export its parser for ui/datetime_node.mjs")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("datetime.js must stay CSP-clean")
	}
	if bytes.Contains(DatetimeJS(), []byte("\t")) {
		t.Error("datetime.js uses two-space indentation, not tabs")
	}
	// Bigger than the shim's and select.js's 8KB by design: this one
	// carries a parser as well as a combobox, and it lands within a few
	// lines of the implementation it was ported from. The cap is here
	// so the next few hundred bytes are a decision rather than a drift
	// — a file an app owner has to page through is a file they stop
	// owning.
	//
	// Raised twice, and the arithmetic is written down both times so
	// the next raise has to argue with real numbers.
	//
	// 48KB → 54KB, by the twelve-locale fixtures: writing a real date
	// in each of the eleven other languages bought four capabilities
	// this parser did not have. It folds the digits an Arabic or Hindi
	// keyboard makes (plain "ar" resolves to latn in ICU, so the
	// locale's default numbering system was not enough); it holds the
	// month name Intl only gives up when a day is in the format, which
	// is the form a Russian actually types; it reads a date written
	// with counter words rather than month names (12月25日); and it
	// tells a counted 月 from Monday. The honest split of that 6.6KB:
	// about 2.7KB of code and about 3.9KB of the prose explaining it.
	// An earlier wording here said "six kilobytes for eleven
	// languages", which read as six kilobytes of parser and was not.
	//
	// 54KB → 60KB, by the read-back wave: the parser now derives, from
	// the same Intl options the field displays with, the literals that
	// display threads between the numbers and the dayPeriod names it
	// picks — so it can read its own writing in all twelve languages
	// instead of five, and "25 de marzo" parses because "de" is a
	// literal Intl already told us about. Split again: about 2.7KB of
	// code, about 3.2KB of comment. More than half of both raises is
	// prose, which is the trade this file makes on purpose — the cap is
	// on what an owner has to page through, not on what runs.
	//
	// 60KB → 76KB, by the calendar overlay, and this raise came with a
	// SPLIT in front of it. The button used to call native.showPicker(),
	// which is one line and close to useless in a form: the panel
	// belongs to the browser, cannot be styled to match the page, does
	// not exist at all for a time input on some engines, and — measured,
	// not guessed — opens WITHOUT moving focus, so the guard on the
	// change listener here swallowed the pick and the field looked like
	// it had ignored the selection. Replacing it took a real widget, so
	// the widget went to ui/calendar.js: 22KB that would otherwise have
	// landed on this number, in a file that needs none of the parser.
	//
	// What stayed is +15.1KB, an honest 7.3KB of code and 7.8KB of
	// prose, and none of it draws a calendar. The popup became three
	// states rather than one — the suggestions, the grid, and a time
	// field's half hours — so there is still only one thing to close and
	// one thing the arrow keys drive; the words drive the grid live
	// while it is open; min/max became bounds a range's end can tighten;
	// the panel flips above the field where there is no room below it;
	// and parseAll arrived, which offers a reading where the exact
	// grammar had none — "5j" is not a date, and 5 January, 5 June and
	// 5 July are three.
	//
	// The cap is on what an owner has to page through. Two files at 74KB
	// and 22KB is that; one file at 96KB is not, which is the whole
	// reason the split came first and the raise second.
	if n := len(DatetimeJS()); n > 76*1024 {
		t.Fatalf("datetime.js is %d bytes; split something out before it stops being readable", n)
	}
}

// The half a machine CAN derive is derived. Month and weekday names
// never appear in this file: they come out of Intl in the page's own
// language, so a field enhanced in Hindi reads Hindi month names with
// nothing added here. A spelling written down would work in English and
// silently fail everywhere else — the exact half-failure the split
// exists to prevent. (The vocabulary keys themselves — "tomorrow",
// "next", "ago" — do appear: they are the attribute's field names, and
// their VALUES are what arrives translated.)
func TestDatetimeDerivesItsCalendarNames(t *testing.T) {
	body := string(DatetimeJS())
	body = body[strings.Index(body, "(function () {"):]
	for _, name := range []string{
		"january", "february", "december", "\"jan\"", "\"dec\"",
		"monday", "friday", "sunday", "\"mon\"", "\"fri\"",
	} {
		if strings.Contains(body, name) {
			t.Errorf("datetime.js spells %s itself; month and weekday names come from Intl", name)
		}
	}
}
