package ui

import (
	"html/template"
	"io"
	"strings"
	"testing"
	"time"
)

// {{date}} writes a moment in the localizer's locale, from the CLDR
// patterns its pack carries, in the moment's own zone.
func TestDateWritesTheLocalesPattern(t *testing.T) {
	german := NewLocalizer("de", "de", func(key string, _ ...any) string {
		return key
	})
	at := time.Date(2026, 11, 20, 21, 9, 0, 0, time.UTC)
	var absent *time.Time
	data := map[string]any{"L": german, "At": at, "Ptr": &at,
		"None": absent}
	for src, want := range map[string]string{
		`{{date $L .At "dayYear"}}`: "Freitag, 20. November 2026",
		`{{date $L .Ptr "time"}}`:   "21:09",
		`{{date $L .None "time"}}`:  "",
		`{{date $L .At "short"}}`:   "Fr., 20. Nov.",
		`{{date .At "day"}}`:        "Friday, November 20",
	} {
		got := execTmpl(t, Funcs(WithLocale("en")),
			`{{$L := localizer $}}`+src, data)
		if got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
}

func TestDateRefusesWhatItCannotWrite(t *testing.T) {
	for src, want := range map[string]string{
		`{{date .At}}`:        "date: 1 arguments, want 2",
		`{{date .At "long"}}`: `date: "long" is not a kind of date`,
		`{{date .At 5}}`:      "date: argument 2 is int, want string",
		`{{date "2026-11-20" "day"}}`: "date: argument 1 is string, " +
			"want time.Time",
	} {
		tmpl := template.Must(template.New("x").Funcs(Funcs()).Parse(src))
		err := tmpl.Execute(io.Discard, map[string]any{"At": time.Now()})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to contain %q", src, err, want)
		}
	}
}
