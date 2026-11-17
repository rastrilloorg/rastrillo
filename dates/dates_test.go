package dates

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/locales"
	_ "amadan.net/rastrillo/rastrillo/locales/modern"
	"amadan.net/rastrillo/rastrillo/nodetest"
)

var update = flag.Bool("update", false, "rewrite testdata/date_labels.json")

const labelsPath = "testdata/date_labels.json"

// labelCase is one moment drawn as one kind in one locale: what Go
// writes, and what browser/dates.mjs must write too.
type labelCase struct {
	Locale string `json:"locale"`
	Kind   string `json:"kind"`
	At     string `json:"at"`
	Want   string `json:"want"`
}

// labelLocales are Mapa's six, then locales that exercise what Mapa's
// do not: other digits, other scripts, another week, a 12-hour clock
// with its own day periods.
var labelLocales = []string{"en-IE", "en-GB", "en-US", "de", "fr", "es",
	"ar-EG", "hi", "ja", "zh-Hans", "ru", "ga", "pt-PT", "bn"}

var labelMoments = []string{"2024-02-29T00:00", "2025-01-01T09:07",
	"2026-11-17T12:00", "2026-11-22T21:09", "2026-12-31T23:59",
	"2027-06-06T00:30", "0001-01-01T00:00", "9999-12-31T23:59"}

func labelCases(t *testing.T) []labelCase {
	t.Helper()
	var out []labelCase
	for _, code := range labelLocales {
		if For(code) == nil {
			t.Fatalf("%s has no dates linked", code)
		}
		for _, kind := range Kinds {
			for _, text := range labelMoments {
				at, err := time.Parse("2006-01-02T15:04", text)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, labelCase{Locale: code, Kind: kind,
					At: text, Want: For(code).Format(kind, at)})
			}
		}
	}
	return out
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", " ")
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// TestDateLabelsAreCurrent is the Go half of the browser's parity: the
// file TestDatesMjsWritesWhatGoWrites holds dates.mjs to. go test
// ./dates -run TestDateLabelsAreCurrent -update rewrites it.
func TestDateLabelsAreCurrent(t *testing.T) {
	encoded := encode(t, labelCases(t))
	if *update {
		if err := os.WriteFile(labelsPath, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(labelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, encoded) {
		t.Fatalf("%s is stale: rerun with -update and commit it",
			labelsPath)
	}
}

// The browser writes every committed case exactly as Go does, from the
// same records.
func TestDatesMjsWritesWhatGoWrites(t *testing.T) {
	committed, err := os.ReadFile(labelsPath)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string]record{}
	for _, code := range labelLocales {
		records[code] = For(code).data
	}
	input := encode(t, map[string]any{"records": records,
		"cases": json.RawMessage(committed)})
	out := nodetest.Run(t, nodetest.Cmd{
		Args:  []string{"testdata/parity.mjs"},
		Stdin: input,
	})
	if !strings.HasPrefix(string(out), "parity: ") {
		t.Fatalf("parity.mjs said %q", out)
	}
}

func TestLocalesWriteTheirOwnFormats(t *testing.T) {
	at := time.Date(2026, 11, 17, 21, 9, 0, 0, time.UTC)
	cases := []struct{ code, kind, want string }{
		{"en-GB", "day", "Tuesday 17 November"},
		{"en-IE", "dayYear", "Tuesday 17 November 2026"},
		{"en-US", "day", "Tuesday, November 17"},
		{"en-US", "time", "9:09\u202fPM"},
		{"en-IE", "time", "21:09"},
		{"de", "day", "Dienstag, 17. November"},
		{"de-AT", "dateYear", "17. November 2026"},
		{"de", "short", "Di., 17. Nov."},
		{"fr", "monthYear", "novembre 2026"},
		{"fr", "stamp", "17 novembre 2026 à 21:09"},
		{"es", "dateYear", "17 de noviembre de 2026"},
		{"ar-EG", "dateYear", "١٧ نوفمبر ٢٠٢٦"},
	}
	for _, each := range cases {
		if got := For(each.code).Format(each.kind, at); got != each.want {
			t.Errorf("%s %s = %q, want %q", each.code, each.kind, got,
				each.want)
		}
	}
}

func TestWeeksStartWhereTheLocaleStartsThem(t *testing.T) {
	friday := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	for code, want := range map[string]string{"en-IE": "2026-11-16",
		"en-US": "2026-11-15", "de": "2026-11-16", "fr": "2026-11-16",
		"ar-EG": "2026-11-14"} {
		got := For(code).WeekStart(friday).Format(time.DateOnly)
		if got != want {
			t.Errorf("%s: the week holding %s starts %s, want %s", code,
				friday.Format(time.DateOnly), got, want)
		}
	}
	for code, want := range map[string]string{"en-IE": "MTWTFSS",
		"en-US": "SMTWTFS", "de": "MDMDFSS", "fr": "LMMJVSD"} {
		var letters strings.Builder
		for _, head := range For(code).Heads() {
			letters.WriteString(head.Letter)
		}
		if letters.String() != want {
			t.Errorf("%s heads %q, want %q", code, letters.String(), want)
		}
	}
}

func TestTypedDatesHaveTheLocalesOrder(t *testing.T) {
	for code, want := range map[string]string{"en-IE": "dmy /",
		"en-US": "mdy /", "de": "dmy .", "ja": "ymd /", "hu": "ymd ."} {
		got := For(code).Order() + " " + For(code).Separator()
		if got != want {
			t.Errorf("%s writes a date in numbers as %q, want %q", code,
				got, want)
		}
	}
}

func TestDayCarriesTheYearOnlyWhenItIsNotThisOne(t *testing.T) {
	english := For("en-GB")
	if got := english.Day("2026-01-01", 2026); got != "Thursday 1 January" {
		t.Errorf("this year: %q", got)
	}
	if got := english.Day("2025-01-01", 2026); got !=
		"Wednesday, 1 January 2025" {
		t.Errorf("another year: %q", got)
	}
	if got := english.Day("2026-02-30", 2026); got != "" {
		t.Errorf("no date: %q", got)
	}
	spanish := For("es")
	if head, tail := spanish.DayTitle("2026-11-17"),
		spanish.YearPart("2026-11-17"); head != "martes, 17 de noviembre" ||
		tail != " de 2026" {
		t.Errorf("es title %q and %q", head, tail)
	}
}

// A locale with no dates linked, or none drawn, writes ISO dates and
// never fails: a reader sees an exact date, never a wrong one.
func TestALocaleWithNoDatesWritesISO(t *testing.T) {
	at := time.Date(2026, 11, 17, 21, 9, 0, 0, time.UTC)
	for _, code := range []string{"th", "fa", "xx-Nope"} {
		restore := locales.SetDefaultForTest("")
		dates := For(code)
		restore()
		if dates != nil {
			t.Errorf("%s has dates %q", code, dates.Code())
			continue
		}
		if got := dates.Format("stamp", at); got != "2026-11-17 21:09" {
			t.Errorf("%s stamp %q", code, got)
		}
		if got := dates.Long("2026-11-17"); got != "2026-11-17" {
			t.Errorf("%s long %q", code, got)
		}
		if dates.FirstDay() != time.Monday || dates.Order() != "ymd" ||
			dates.Heads()[0].Name != "Monday" || dates.Client() != nil {
			t.Errorf("%s: the week or client of a nil Locale", code)
		}
	}
}

// Every locale the packs carry either has dates whose every kind is
// drawn, or is listed in coverage.tsv with the reason it has none.
func TestEveryLinkedLocaleHasDatesOrSaysWhyNot(t *testing.T) {
	coverage, err := os.ReadFile("../hack/cldrdates/coverage.tsv")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, line := range strings.Split(string(coverage), "\n") {
		if fields := strings.Split(line, "\t"); len(fields) == 3 &&
			!strings.HasPrefix(line, "#") {
			listed[fields[0]] = fields[1]
		}
	}
	at := time.Date(2026, 11, 17, 21, 9, 0, 0, time.UTC)
	for _, id := range locales.LinkedIDs() {
		_, linked := locales.Dates(id)
		want := map[bool]string{true: "yes", false: "no"}[linked]
		if listed[id] != want {
			t.Errorf("%s: coverage.tsv says %q, the pack %q", id,
				listed[id], want)
		}
		if !linked {
			continue
		}
		for _, kind := range Kinds {
			if For(id).Format(kind, at) == "" {
				t.Errorf("%s draws no %s", id, kind)
			}
		}
	}
}

// The browser half is served from the module, so it cannot drift from
// the Go it is tested against.
func TestAssetsServeDatesMjs(t *testing.T) {
	body, err := fs.ReadFile(Assets(), "dates.mjs")
	if err != nil || !bytes.Contains(body,
		[]byte("export function formatWith")) {
		t.Fatalf("Assets has no dates.mjs: %v", err)
	}
}

// The packs' dates are CLDR's, not a hand edit: with the ICU they were
// read from, regenerating them gives the committed bytes. A Node
// carrying another ICU, or only English (small-icu), cannot say, and
// skips naming what it skipped.
func TestDatesAreWhatICUGives(t *testing.T) {
	header, err := os.ReadFile("../locales/en/dates_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(string(header), "through ICU ")
	want, _, _ := strings.Cut(after, " ")
	have := strings.TrimSpace(string(nodetest.Run(t, nodetest.Cmd{
		Args: []string{"-p", "process.versions.icu + (Intl.DateTimeFormat" +
			".supportedLocalesOf(['de', 'ja', 'ar']).length === 3 ? '' : " +
			"' small')"}})))
	if have != want {
		t.Skipf("this Node links ICU %q and the packs' dates came from "+
			"ICU %s, so their source is unverified here", have, want)
	}
	probe := strings.TrimSpace(string(nodetest.Run(t, nodetest.Cmd{
		Args: []string{"../hack/cldrdates/gen.mjs", "--probe"}})))
	if probe != "ok" {
		t.Skipf("this Node has %s, so the packs' dates are unverified "+
			"here", probe)
	}
	nodetest.Run(t, nodetest.Cmd{
		Args:    []string{"../hack/cldrdates/gen.mjs", "--check"},
		Timeout: 5 * time.Minute,
	})
}

// Years 1 to 9999 are drawn, as ICU draws them (year 1 began on a
// Monday, proleptically); outside them ISO 8601 is written, where ICU
// would write year 0 as year 1 of another era.
func TestYearsOutsideOneTo9999AreISO(t *testing.T) {
	british := For("en-GB")
	for _, each := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), "Monday, 1 January 1"},
		{time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
			"Friday, 31 December 9999"},
		{time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), "0000-01-01"},
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "10000-01-01"},
	} {
		if got := british.Format("dayYear", each.at); got != each.want {
			t.Errorf("year %d: %q, want %q", each.at.Year(), got, each.want)
		}
		iso := each.at.Format(time.DateOnly)
		whole := british.DayTitle(iso) + british.YearPart(iso)
		if long := british.Long(iso); whole != long {
			t.Errorf("year %d: DayTitle and YearPart make %q, Long %q",
				each.at.Year(), whole, long)
		}
	}
}

// A locale with no dates is nil in Go and null in the page, and both
// halves fall back alike: ISO dates, Monday first, numbers read ymd.
func TestNoDatesFallBackAlikeInGoAndTheBrowser(t *testing.T) {
	thai := For("th")
	client, err := json.Marshal(thai.Client())
	if err != nil {
		t.Fatal(err)
	}
	var heads []string
	for _, head := range thai.Heads() {
		heads = append(heads, head.Name+" "+head.Letter)
	}
	at := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	british := For("en-GB")
	kinds := map[string][3]string{}
	for _, kind := range Kinds {
		kinds[kind] = [3]string{thai.Format(kind, at),
			thai.Format(kind, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)),
			british.Format(kind, time.Date(10000, 3, 4, 0, 0, 0, 0,
				time.UTC))}
	}
	want := map[string]any{"order": thai.Order(), "kinds": kinds,
		"firstDay": int(thai.FirstDay()), "heads": heads,
		"day": thai.Format("day", at),
		"far": thai.Format("day", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)),
		"british": For("en-GB").Format("day", time.Date(0, 1, 1, 0, 0, 0,
			0, time.UTC)),
		"britishRecord": For("en-GB").Client()}
	input := encode(t, map[string]any{"client": json.RawMessage(client),
		"want": want})
	nodetest.Run(t, nodetest.Cmd{Args: []string{"testdata/fallback.mjs"},
		Stdin: input})
}
