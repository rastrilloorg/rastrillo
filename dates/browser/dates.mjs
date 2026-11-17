// dates.mjs: dates in the reader's locale, in the browser, exactly as
// rastrillo's dates package writes them on the server. Hand it a
// locale's data, dates.Locale.Client() put in the page as JSON, before
// anything is drawn. formatWith is the same interpreter dates/format.go
// is, held to it by dates/testdata/date_labels.json, so a label drawn
// here reads exactly as the server's. Intl is never asked: its
// patterns are the browser's ICU, which need not be the CLDR rastrillo
// ships (hack/cldrdates read that once, at build time).
//
// Moved from Mapa (editor/dates.mjs, level-up build note 85) with its
// semantics kept: what Mapa's gates check is what this writes.

let current = null

// useDates sets the locale's data: dates.Locale.Client()'s JSON.
export function useDates (data) {
  current = data ?? null
}

export function datesInUse () {
  return current
}

// digitsOf writes a number in the pattern's digits, when its locale
// writes others than ASCII's.
function digitsOf (text, digits) {
  return digits ? text.replace(/[0-9]/g, digit => digits[digit]) : text
}

const pad = (value, wanted) => wanted ? String(value).padStart(2, '0') : String(value)

function hourOf (hour, cycle) {
  switch (cycle) {
    case 'h12': return hour % 12 === 0 ? 12 : hour % 12
    case 'h11': return hour % 12
    case 'h24': return hour === 0 ? 24 : hour
  }
  return hour
}

// numeric is one numeric field of a pattern, in its digits.
function numeric (pattern, part, fields) {
  switch (part.t) {
    case 'monthNumber': return digitsOf(pad(fields.month, part.pad), pattern.digits)
    case 'day': return digitsOf(pad(fields.day, part.pad), pattern.digits)
    case 'year': return digitsOf(String(fields.year), pattern.digits)
    case 'hour': return digitsOf(pad(hourOf(fields.hour, part.cycle), part.pad), pattern.digits)
    case 'minute': return digitsOf(pad(fields.minute, true), pattern.digits)
  }
  return ''
}

// formatWith draws one pattern for fields: year, month (1 to 12), day,
// weekday (0 is Sunday), hour and minute.
export function formatWith (pattern, fields) {
  let out = ''
  for (const part of pattern.parts) {
    switch (part.t) {
      case 'lit': out += part.v; break
      case 'weekday': out += pattern.weekdays[fields.weekday]; break
      case 'month': out += pattern.months[fields.month - 1]; break
      case 'period': out += pattern.periods[fields.hour < 12 ? 0 : 1]; break
      default: out += numeric(pattern, part, fields)
    }
  }
  return out
}

// fieldsOf is an ISO date's fields.
export function fieldsOf (iso) {
  const [year, month, day] = iso.split('-').map(Number)
  const date = new Date(0)
  date.setUTCFullYear(year, month - 1, day)
  return { year, month, day, weekday: date.getUTCDay(), hour: 0, minute: 0 }
}

// isoOf is a kind written in ISO 8601, as Go's dates.Locale.Format
// writes it when it draws no pattern: the date, the month, the year,
// the time or both, by kind.
function isoOf (kind, fields) {
  const year = String(fields.year).padStart(4, '0')
  const month = pad(fields.month, true)
  const date = `${year}-${month}-${pad(fields.day, true)}`
  const time = `${pad(fields.hour, true)}:${pad(fields.minute, true)}`
  switch (kind) {
    case 'monthYear': return `${year}-${month}`
    case 'month': return month
    case 'year': return year
    case 'time': return time
    case 'stamp': return `${date} ${time}`
  }
  return date
}

// format is one kind of date (dates.Kinds) for an ISO date, in data's
// locale; ISO 8601 for that kind when there is no data, or the year is
// outside 1 to 9999, exactly as Go's dates.Locale.Format.
export function format (kind, iso, data = current) {
  const pattern = data?.patterns?.[kind]
  const fields = fieldsOf(iso)
  const drawn = fields.year >= 1 && fields.year <= 9999
  return pattern && drawn ? formatWith(pattern, fields) : isoOf(kind, fields)
}

// dayLabel is dates.Locale.Day: "Thursday 12 November" in en-GB, with
// the year when it is not thisYear.
export function dayLabel (iso, thisYear = new Date().getFullYear(), data = current) {
  return format(Number(iso.slice(0, 4)) === thisYear ? 'day' : 'dayYear', iso, data)
}

// monthTitle is a month calendar's title: "November 2026".
export function monthTitle (iso, data = current) {
  return format('monthYear', `${iso.slice(0, 7)}-01`, data)
}

// firstDay is the locale's first day of the week, 0 for Sunday; Monday
// when there is no data.
export function firstDay (data = current) {
  return data?.firstDay ?? 1
}

// weekHeads are the week's columns from its first day: the day's name
// and its narrow letter.
export function weekHeads (data = current) {
  const english = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  const names = data?.weekdays ?? english
  const letters = data?.narrow ?? english.map(name => name[0])
  return Array.from({ length: 7 }, (_, n) => {
    const day = (firstDay(data) + n) % 7
    return { name: names[day], letter: letters[day] }
  })
}

// order is how a numeric date is read: "dmy", "mdy" or "ymd"; "ymd"
// with no data, which writes ISO dates, as Go's nil Locale does. Mapa's
// copy said "dmy" here, a case its pages never reach.
export function order (data = current) {
  return data?.order ?? 'ymd'
}
