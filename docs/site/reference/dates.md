# 🤖 dates

`amadan.net/rastrillo/rastrillo/dates`

Writes dates and times in a reader's language from CLDR 48's patterns,
which each language pack carries beside its number and money data. The
browser draws the same patterns with `dates.mjs`, byte for byte.
`{{date $L .At "kind"}}` calls this package for you.

## For

```go
func For(tag string) *Locale
func (in *Locale) Code() string
```

`For` is the dates of the locale `tag` resolves to, as numbers and
money resolve it: `de-AT` is de-AT's, `en-US` is `en`'s. A tag whose
pack is not linked resolves to your app's default locale, as numbers
do, and gets its dates. It is nil when there is no default to fall to,
and for the eleven locales the formatter cannot draw: those whose
calendar is not the Gregorian (`fa`, `th`, `ps`), whose numeric date
runs year, day, month (`ky`), whose day periods are more than AM and PM
(`zh-Hant`, so also `zh-TW`, which resolves to it), whose
minutes ICU writes unpadded (`yo`), and `ht`, which ICU has no data
for. `hack/cldrdates/coverage.tsv` lists each one with its reason. A nil `Locale` writes ISO 8601 dates and
starts its weeks on Monday, so a reader sees an exact date, never a
wrong one.

## Format

```go
var Kinds = []string{"day", "dayYear", "short", "date", "dateYear",
	"monthYear", "month", "year", "time", "stamp"}

func (in *Locale) Format(kind string, at time.Time) string
func (in *Locale) Day(iso string, thisYear int) string
func (in *Locale) Long(iso string) string
func (in *Locale) Short(iso string) string
func (in *Locale) Time(at time.Time) string
func (in *Locale) Stamp(at time.Time) string
func (in *Locale) DayTitle(iso string) string
func (in *Locale) YearPart(iso string) string
```

```go
const (
	FirstYear = 1
	LastYear  = 9999
)
```

`Format` writes `at` in its own zone, so convert it with `In` first. It
draws years `FirstYear` to `LastYear`; outside them it writes ISO 8601,
since ICU would write year 0 as year 1 of an era the patterns do not
name. For
Friday 20 November 2026 at 21:09:

| Kind | `en-GB` | `en-US` | `de` |
|---|---|---|---|
| `day` | `Friday 20 November` | `Friday, November 20` | `Freitag, 20. November` |
| `dayYear` | `Friday, 20 November 2026` | `Friday, November 20, 2026` | `Freitag, 20. November 2026` |
| `short` | `Fri 20 Nov` | `Fri, Nov 20` | `Fr., 20. Nov.` |
| `dateYear` | `20 November 2026` | `November 20, 2026` | `20. November 2026` |
| `monthYear` | `November 2026` | `November 2026` | `November 2026` |
| `time` | `21:09` | `9:09 PM` | `21:09` |
| `stamp` | `20 November 2026 at 21:09` | `November 20, 2026 at 9:09 PM` | `20. November 2026 um 21:09` |

`date`, `month` and `year` are the date, the month's name and the year
alone. `Day`, `Long` and `Short` take an ISO date (`2026-11-20`): `Day`
adds the year only when it is not `thisYear`. `Time` and `Stamp` are
`Format`'s `time` and `stamp`. `DayTitle` and `YearPart` cut the full
date before its year, as the locale writes it, so a page can set the
year apart.

## Weeks and dates typed in numbers

```go
type Head struct{ Name, Letter string }

func (in *Locale) FirstDay() time.Weekday
func (in *Locale) WeekStart(day time.Time) time.Time
func (in *Locale) Heads() []Head
func (in *Locale) Letter(weekday time.Weekday) string
func (in *Locale) Order() string
func (in *Locale) Separator() string
```

`FirstDay` is the day the locale's weeks start on: Monday in `en-GB`,
Sunday in `en-US`, Saturday in `ar-EG`. `WeekStart` is the first day of
the week holding `day`, and `Heads` are a calendar's columns from that
day: each weekday's name and its narrow letter. `Order` and `Separator`
(the mark as the locale writes it, which is `年` in `zh-Hans-SG`)
say how the locale writes a date in numbers, which is how to read one
typed: `dmy` and `/` in `en-GB`, `mdy` and `/` in `en-US`, `dmy` and `.`
in `de`.

## In the browser

```go
type Client struct{ /* code, firstDay, order, weekdays, narrow, patterns */ }

func (in *Locale) Client() *Client
func Assets() fs.FS
```

`Assets` is `dates.mjs`, served from the module so it cannot drift from
the Go it matches. Mount it behind an Assets registry:

```go
dateAssets := rastrillo.NewAssets(dates.Assets())
mux.Handle("GET /dates/", http.StripPrefix("/dates/", dateAssets.Handler()))
```

Put `dates.For(locale).Client()` in the page as JSON, then import the
module and hand it the data:

```js
import { useDates, dayLabel, weekHeads } from '/dates/dates.mjs'
useDates(JSON.parse(document.getElementById('dates').textContent))
dayLabel('2026-11-20') // "Friday 20 November" in en-GB
```

It exports `useDates`, `datesInUse`, `formatWith`, `fieldsOf`, `format`,
`dayLabel`, `monthTitle`, `firstDay`, `weekHeads` and `order`. With no
data (a nil `Client()`, `null` in the page) it falls back as Go's nil
`Locale` does: each kind in ISO 8601 (`2026-11-20` for a date,
`2026-11` for `monthYear`, `11` for `month`, `2026` for `year`), weeks
from Monday, and `order()` `ymd`. A year outside 1 to 9999 is written
the same ISO way in both, and `DayTitle` then holds the whole of it. `dates/testdata/date_labels.json` holds the
cases Go and `dates.mjs` must both write.

## Where the patterns come from

`make locale-dates` reads them out of the ICU that Node links, which
must carry the packs' CLDR, and checks every pattern against it for
every day of 2024 to 2026. It writes each pack's `dates_gen.go` and
`hack/cldrdates/coverage.tsv`. A test regenerates them and fails on any
difference when its Node carries the same CLDR.
