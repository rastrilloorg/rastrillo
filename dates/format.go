package dates

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// moment is the fields a pattern reads: month 1 to 12, weekday 0 for
// Sunday, the hour 0 to 23.
type moment struct {
	year, month, day, weekday, hour, minute int
}

func momentOf(at time.Time) moment {
	return moment{year: at.Year(), month: int(at.Month()), day: at.Day(),
		weekday: int(at.Weekday()), hour: at.Hour(), minute: at.Minute()}
}

func padded(value int, pad bool) string {
	if pad && value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

// hourOf is the hour as the pattern's cycle counts it.
func hourOf(hour int, cycle string) int {
	switch cycle {
	case "h12":
		if hour%12 == 0 {
			return 12
		}
		return hour % 12
	case "h11":
		return hour % 12
	case "h24":
		if hour == 0 {
			return 24
		}
	}
	return hour
}

// write draws the pattern for at: browser/dates.mjs's formatWith, line
// for line.
func (shape *pattern) write(at moment) string {
	var out strings.Builder
	for _, part := range shape.Parts {
		switch part.Type {
		case "lit":
			out.WriteString(part.Value)
		case "weekday":
			out.WriteString(shape.Weekdays[at.weekday])
		case "month":
			out.WriteString(shape.Months[at.month-1])
		case "period":
			out.WriteString(shape.Periods[min(at.hour/12, 1)])
		default:
			out.WriteString(shape.localDigits(shape.numeric(part, at)))
		}
	}
	return out.String()
}

// numeric is one of a pattern's numeric fields, in ASCII digits.
func (shape *pattern) numeric(part field, at moment) string {
	switch part.Type {
	case "monthNumber":
		return padded(at.month, part.Pad)
	case "day":
		return padded(at.day, part.Pad)
	case "year":
		return strconv.Itoa(at.year)
	case "hour":
		return padded(hourOf(at.hour, part.Cycle), part.Pad)
	case "minute":
		return padded(at.minute, true)
	}
	return ""
}

// localDigits writes ASCII digits in the pattern's own, if it has any.
func (shape *pattern) localDigits(text string) string {
	digits := []rune(shape.Digits)
	if len(digits) != 10 {
		return text
	}
	return strings.Map(func(char rune) rune {
		if char >= '0' && char <= '9' {
			return digits[char-'0']
		}
		return char
	}, text)
}

// isoLayouts are what a nil Locale writes for each kind: ISO 8601,
// which no reader misreads.
var isoLayouts = map[string]string{"day": time.DateOnly,
	"dayYear": time.DateOnly, "short": time.DateOnly,
	"date": time.DateOnly, "dateYear": time.DateOnly,
	"monthYear": "2006-01", "month": "01", "year": "2006",
	"time": "15:04", "stamp": "2006-01-02 15:04"}

// FirstYear and LastYear bound the years a pattern draws. ICU writes
// year 0 as year 1 (before Christ, an era the patterns do not name),
// and past 9999 the four-digit forms stop holding.
const (
	FirstYear = 1
	LastYear  = 9999
)

// Format is one kind of date (Kinds) for a moment, in the moment's own
// zone: convert it with In first. An unknown kind writes nothing. A
// year outside FirstYear to LastYear, and a nil Locale, write ISO 8601.
func (in *Locale) Format(kind string, at time.Time) string {
	if !in.draws(at) {
		return at.Format(isoLayouts[kind])
	}
	shape, ok := in.data.Patterns[kind]
	if !ok {
		return ""
	}
	return shape.write(momentOf(at))
}

// draws reports whether the locale's patterns write at: a Locale with
// dates, and a year from FirstYear to LastYear.
func (in *Locale) draws(at time.Time) bool {
	return in != nil && at.Year() >= FirstYear && at.Year() <= LastYear
}

// day reads an ISO date, or reports it is none.
func day(iso string) (time.Time, bool) {
	at, err := time.Parse(time.DateOnly, iso)
	return at, err == nil && len(iso) == len(time.DateOnly)
}

// iso formats an ISO date as kind; anything that is no date is shown
// as it is.
func (in *Locale) iso(kind, iso string) string {
	at, ok := day(iso)
	if !ok {
		return iso
	}
	return in.Format(kind, at)
}

// Day is an ISO date as a chip shows it: "Thursday 12 November" in
// en-GB, with the year when it is not thisYear. "" for text that is no
// date.
func (in *Locale) Day(iso string, thisYear int) string {
	at, ok := day(iso)
	if !ok {
		return ""
	}
	if at.Year() == thisYear {
		return in.Format("day", at)
	}
	return in.Format("dayYear", at)
}

// Long is an ISO date in full, year and all: "Thursday 12 November
// 2026" in en-GB.
func (in *Locale) Long(iso string) string { return in.iso("dayYear", iso) }

// Short is an ISO date on a pill: "Thu 12 Nov" in en-GB.
func (in *Locale) Short(iso string) string { return in.iso("short", iso) }

// Time is a time of day: "21:09", or "9:09 PM" in en-US.
func (in *Locale) Time(at time.Time) string { return in.Format("time", at) }

// Stamp is a moment in full, for a title: "12 November 2026 at 21:09".
func (in *Locale) Stamp(at time.Time) string { return in.Format("stamp", at) }

// DayTitle is the full date up to its year, which YearPart sets apart:
// together they are Long, written as the locale composes it ("martes,
// 17 de noviembre" and " de 2026"), so a page can set the year apart.
func (in *Locale) DayTitle(iso string) string {
	head, _ := in.titleParts(iso)
	return head
}

// YearPart is the rest of the full date from the separator before its
// year: " 2026", ", 2026" in en-US. "" for text that is no date.
func (in *Locale) YearPart(iso string) string {
	_, tail := in.titleParts(iso)
	return tail
}

// titleParts cuts dayYear before the literal ahead of its year. A
// pattern whose year comes first is not cut.
func (in *Locale) titleParts(iso string) (string, string) {
	at, ok := day(iso)
	if !ok {
		return iso, ""
	}
	if !in.draws(at) {
		// Long's ISO, whole: an ISO date has no year to set apart.
		return in.Format("dayYear", at), ""
	}
	shape := in.data.Patterns["dayYear"]
	cut := slices.IndexFunc(shape.Parts, func(part field) bool {
		return part.Type == "year"
	})
	if cut > 0 && shape.Parts[cut-1].Type == "lit" {
		cut--
	}
	if cut <= 0 {
		return shape.write(momentOf(at)), ""
	}
	head, tail := *shape, *shape
	head.Parts, tail.Parts = shape.Parts[:cut], shape.Parts[cut:]
	return head.write(momentOf(at)), tail.write(momentOf(at))
}
