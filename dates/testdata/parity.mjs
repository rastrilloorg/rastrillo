// parity.mjs: dates.mjs writes every case in date_labels.json exactly
// as Go's dates package does, from the same records. The Go test
// TestDatesMjsWritesWhatGoWrites feeds it {records, cases} on stdin.
import { readFileSync } from 'node:fs'
import { formatWith } from '../browser/dates.mjs'

const { records, cases } = JSON.parse(readFileSync(0, 'utf8'))
const wrong = []
for (const { locale, kind, at, want } of cases) {
  const [date, time] = at.split('T')
  const [year, month, day] = date.split('-').map(Number)
  const [hour, minute] = time.split(':').map(Number)
  // setUTCFullYear, not Date.UTC, which reads years 0 to 99 as 19xx.
  const midnight = new Date(0)
  midnight.setUTCFullYear(year, month - 1, day)
  const weekday = midnight.getUTCDay()
  const got = formatWith(records[locale].patterns[kind],
    { year, month, day, weekday, hour, minute })
  if (got !== want) wrong.push(`${locale} ${kind} ${at}: ${JSON.stringify(got)}, Go ${JSON.stringify(want)}`)
}
if (wrong.length || cases.length === 0) {
  console.error(wrong.join('\n') || 'no cases')
  process.exit(1)
}
console.log(`parity: ${cases.length} cases, every one as Go writes it`)
