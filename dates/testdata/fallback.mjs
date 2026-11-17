// fallback.mjs: with the page's data for a locale that has no dates
// (Go's nil Locale, null as JSON), dates.mjs falls back as Go does, and
// a year outside 1 to 9999 is written as ISO in both. The Go test
// TestNoDatesFallBackAlikeInGoAndTheBrowser feeds it what Go wrote.
import { readFileSync } from 'node:fs'
import { firstDay, format, order, useDates, weekHeads } from '../browser/dates.mjs'

const { client, want } = JSON.parse(readFileSync(0, 'utf8'))
useDates(client)
const kinds = {}
for (const kind of Object.keys(want.kinds)) {
  kinds[kind] = [format(kind, '2026-11-20'), format(kind, '0000-01-01'),
    format(kind, '10000-03-04', want.britishRecord)]
}
const got = {
  kinds,
  order: order(),
  firstDay: firstDay(),
  heads: weekHeads().map(head => `${head.name} ${head.letter}`),
  day: format('day', '2026-11-20'),
  far: format('day', '0000-01-01'),
  british: format('day', '0000-01-01', want.britishRecord)
}
const wrong = Object.keys(got).filter(key => JSON.stringify(got[key]) !== JSON.stringify(want[key]))
if (wrong.length) {
  console.error(wrong.map(key => `${key}: ${JSON.stringify(got[key])}, Go ${JSON.stringify(want[key])}`).join('\n'))
  process.exit(1)
}
console.log('fallback: dates.mjs falls back as Go does')
