package dates

import "time"

// english are the weekdays a nil Locale names, Sunday first.
var english = []string{"Sunday", "Monday", "Tuesday", "Wednesday",
	"Thursday", "Friday", "Saturday"}

// FirstDay is the day the locale's weeks start on; Monday, ISO 8601's,
// for a nil Locale.
func (in *Locale) FirstDay() time.Weekday {
	if in == nil {
		return time.Monday
	}
	return time.Weekday(in.data.FirstDay)
}

// WeekStart is the first day of the week holding day.
func (in *Locale) WeekStart(day time.Time) time.Time {
	back := (int(day.Weekday()) - int(in.FirstDay()) + 7) % 7
	return day.AddDate(0, 0, -back)
}

// Head is a calendar's column: the day's name, and its letter.
type Head struct{ Name, Letter string }

// Heads are the week's columns from its first day.
func (in *Locale) Heads() []Head {
	out := make([]Head, 7)
	for column := range out {
		weekday := (int(in.FirstDay()) + column) % 7
		out[column] = Head{Name: in.weekday(weekday),
			Letter: in.Letter(time.Weekday(weekday))}
	}
	return out
}

func (in *Locale) weekday(weekday int) string {
	if in == nil {
		return english[weekday]
	}
	return in.data.Weekdays[weekday]
}

// Letter is a weekday's narrow name: "M" for Monday in English.
func (in *Locale) Letter(weekday time.Weekday) string {
	if in == nil {
		return english[weekday][:1]
	}
	return in.data.Narrow[weekday]
}

// Order is how the locale writes a date in numbers, which is how a
// typed one is read: "dmy", "mdy" or "ymd"; "ymd" for a nil Locale.
func (in *Locale) Order() string {
	if in == nil {
		return "ymd"
	}
	return in.data.Order
}

// Separator is the mark between a date's numbers: "/", or "." in
// German; "-" for a nil Locale.
func (in *Locale) Separator() string {
	if in == nil {
		return "-"
	}
	return in.data.Separator
}
