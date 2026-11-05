package ui

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/fiesta"
	"amadan.net/rastrillo/rastrillo"
)

// Fiesta's Node fixtures prove datetime.js reads a date typed in each
// language with the words in Fiesta's own tables, so a Rastrillo page
// is covered by them only while its catalogs carry those same words:
// {{dateWords}} sends the request's catalog on data-rst-date-words,
// and the attribute wins over the table. A catalog word Fiesta's table
// lacks is a spelling nobody has shown the parser can read.
func TestTheCatalogsDateWordsAreFiestas(t *testing.T) {
	for code, catalog := range rastrillo.BaseCatalogs() {
		table, ok := fiesta.Strings(code)
		if !ok {
			t.Errorf("%s: Fiesta %s has no strings for it",
				code, fiesta.Version)
			continue
		}
		var parsed struct {
			Words map[string]string `json:"date-words"`
		}
		if err := json.Unmarshal(table, &parsed); err != nil {
			t.Fatalf("%s: Fiesta's table: %v", code, err)
		}
		for _, name := range dateWordNames {
			ours := catalog["rastrillo.ui.date_"+name]
			if theirs := parsed.Words[name]; ours != theirs {
				t.Errorf("%s: rastrillo.ui.date_%s is %q, Fiesta's %s is %q",
					code, name, ours, name, theirs)
			}
		}
	}
}

// calendar.js holds to the same contract as the other three scripts:
// its own vocabulary, a native-input-free widget built in the browser,
// and no English month or weekday name anywhere in it.
func TestCalendarContract(t *testing.T) {
	js := string(CalendarJS())
	for _, want := range []string{
		// The seam datetime.js looks up at enhance time. If this name
		// moves, the button silently falls back to the browser's own
		// picker — which is the failure this whole branch removed.
		"fiestaCalendar",
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
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("calendar.js must stay CSP-clean")
	}
	if bytes.Contains(CalendarJS(), []byte("\t")) {
		t.Error("calendar.js uses two-space indentation, not tabs")
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
		// The seam to calendar.js. If this name moves and the other
		// file is not moved with it, nothing breaks loudly: the button
		// quietly falls back to the browser's own picker, which is the
		// unusable control this whole overlay replaced.
		"fiestaCalendar",
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
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") {
		t.Error("datetime.js must stay CSP-clean")
	}
	if bytes.Contains(DatetimeJS(), []byte("\t")) {
		t.Error("datetime.js uses two-space indentation, not tabs")
	}
}

// The half a machine CAN derive is derived. Month and weekday names
// never appear in this file: they come out of Intl in the page's own
// language, so a field enhanced in Hindi reads Hindi month names with
// nothing added here. A spelling written down would work in English and
// silently fail everywhere else — the exact half-failure the split
// exists to prevent. (The relative words, "tomorrow", "next", "ago",
// do appear: they key the attribute's JSON, and in English they are
// the fallback when neither the attribute nor a loaded table has them.)
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

// The pinned datetime.js reads its words from the field's attribute,
// then a loaded Fiesta table, then English it carries itself, so the
// pages describing it must not say it holds no English vocabulary, or
// that only its labels fall back to English.
func TestTheDocsDescribeDatetimesEnglishWords(t *testing.T) {
	if !strings.Contains(string(DatetimeJS()), "var ENGLISH_WORDS") {
		t.Fatal("datetime.js no longer carries English words; " +
			"say where its words come from in the files below")
	}
	for _, file := range []string{
		"../docs/site/templates.md", "../docs/site/reference/ui.md", "ui.go",
	} {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		words := slices.DeleteFunc(strings.Fields(string(text)),
			func(word string) bool { return word == "//" })
		prose := strings.Join(words, " ")
		for _, claim := range []string{
			"no English vocabulary", "only the labels fall back",
			"Only the on-screen labels fall back",
		} {
			if strings.Contains(prose, claim) {
				t.Errorf("%s says %q", file, claim)
			}
		}
		if !strings.Contains(prose, "the English built into the script") {
			t.Errorf("%s does not say the words fall back to the "+
				"English built into the script", file)
		}
	}
}
