// gen.mjs: make locale-dates. Writes locales/<pack>/dates_gen.go, the
// CLDR date patterns and names the dates package formats with, for
// every locale the packs carry, and hack/cldrdates/coverage.tsv, which
// says for each locale whether it has dates and, if not, why.
//
// Moved from Mapa's editor/gen-dates.mjs (level-up build note 85) with
// its kinds and its method kept. The data is read out of the ICU this
// Node links, which must carry the packs' CLDR: a CLDR skeleton, given
// to Intl.DateTimeFormat, comes back as its pattern in formatToParts,
// and the names each pattern uses are read through that same pattern,
// so a language that declines a month after a day gets the declined
// form there and only there. Nothing reads Intl at run time: Go (the
// dates package) and the browser (dates/browser/dates.mjs) both
// interpret these patterns, held to each other by
// dates/testdata/date_labels.json.
//
// Before writing, every pattern is drawn by dates.mjs's interpreter
// for every day of 2024 to 2026, 1 January of year 1 and 31 December
// 9999, and every minute pair that matters,
// and compared with Intl. A locale with any difference, or one the
// interpreter cannot draw (another calendar by default, a day period
// beyond AM and PM, a field it does not know), gets no dates: its
// reader sees ISO dates, never a wrong one, and coverage.tsv says why.

import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { formatWith } from '../../dates/browser/dates.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const core = readFileSync(join(root, 'locales', 'core_gen.go'), 'utf8')

const cldr = core.match(/CLDRVersion = "([^"]+)"/)[1]
if (!cldr.startsWith(process.versions.cldr.split('.')[0] + '.')) {
  console.error(`cldrdates: this Node links CLDR ${process.versions.cldr}; the packs are cldr-json ${cldr}. Use a Node whose ICU carries it.`)
  process.exit(1)
}

if (Intl.DateTimeFormat.supportedLocalesOf(['de', 'ja', 'ar', 'hi']).length < 4) {
  console.error('cldrdates: this Node has no full ICU (small-icu?), so most locales would read as having none. Use a Node built with full ICU.')
  process.exit(2)
}

// packs maps each pack to its locale ids, from core_gen.go's index.
function packs () {
  const index = core.slice(core.indexOf('var index = map[string]string{'))
  const body = index.slice(0, index.indexOf('\n}'))
  const out = new Map()
  for (const [, id, pack] of body.matchAll(/"([^"]+)":\s*"([^"]+)",/g)) {
    if (!out.has(pack)) out.set(pack, [])
    out.get(pack).push(id)
  }
  return out
}

// kinds are the dates the package writes, as Intl options (CLDR
// skeletons), Mapa's ten.
const kinds = {
  day: { weekday: 'long', day: 'numeric', month: 'long' },
  dayYear: { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' },
  short: { weekday: 'short', day: 'numeric', month: 'short' },
  date: { day: 'numeric', month: 'long' },
  dateYear: { day: 'numeric', month: 'long', year: 'numeric' },
  monthYear: { month: 'long', year: 'numeric' },
  month: { month: 'long' },
  year: { year: 'numeric' },
  // CLDR's own short time and long date: as skeletons, hour: 'numeric'
  // would come back unpadded (9:07) where CLDR writes 09:07.
  time: { timeStyle: 'short' },
  stamp: { dateStyle: 'long', timeStyle: 'short' }
}

const at = (year, month, day, hour = 0, minute = 0) =>
  new Date(Date.UTC(year, month - 1, day, hour, minute))

// boundary is a midnight in any year: Date.UTC reads 0 to 99 as 19xx.
function boundary (year, month, day) {
  const date = new Date(0)
  date.setUTCFullYear(year, month - 1, day)
  return date
}

function formatter (locale, options) {
  return new Intl.DateTimeFormat(locale, { timeZone: 'UTC', ...options })
}

const digitRun = /^\p{Nd}+$/u

// fieldOf is one formatToParts part as a pattern field. A sample whose
// day, month and hour are single digits shows whether each is padded.
function fieldOf (part, hourCycle) {
  const wide = [...part.value].length === 2
  switch (part.type) {
    case 'literal': return { t: 'lit', v: part.value }
    case 'weekday': return { t: 'weekday' }
    case 'year': return { t: 'year' }
    case 'dayPeriod': return { t: 'period' }
    case 'minute': return { t: 'minute' }
    case 'day': return { t: 'day', pad: wide }
    case 'month':
      return digitRun.test(part.value) ? { t: 'monthNumber', pad: wide } : { t: 'month' }
    case 'hour': return { t: 'hour', pad: wide, cycle: hourCycle }
  }
  throw new Error(`a ${part.type} field, which dates.mjs does not draw`)
}

function namesOf (format, type, dates) {
  return dates.map(date => format.formatToParts(date).find(part => part.type === type).value)
}

// patternOf is one kind's pattern for a locale, with the names it uses,
// and its digits when they are not ASCII's.
function patternOf (locale, options, digits) {
  const format = formatter(locale, options)
  const cycle = format.resolvedOptions().hourCycle ?? ''
  const parts = format.formatToParts(at(2025, 1, 5, 9, 7)).map(part => fieldOf(part, cycle))
  const pattern = { parts }
  const types = new Set(parts.map(part => part.t))
  // 4 to 10 January 2026 run Sunday to Saturday.
  if (types.has('weekday')) pattern.weekdays = namesOf(format, 'weekday', [4, 5, 6, 7, 8, 9, 10].map(day => at(2026, 1, day)))
  if (types.has('month')) pattern.months = namesOf(format, 'month', [...Array(12)].map((_, month) => at(2026, month + 1, 15)))
  if (types.has('period')) pattern.periods = namesOf(format, 'dayPeriod', [at(2026, 1, 1, 9), at(2026, 1, 1, 21)])
  if (digits) pattern.digits = digits
  return pattern
}

// check draws every pattern for 2024 to 2026 and compares it with Intl.
function check (locale, kind, pattern) {
  const format = formatter(locale, kinds[kind])
  const times = kind === 'time' || kind === 'stamp'
    ? [[0, 0], [0, 5], [9, 7], [11, 59], [12, 0], [13, 30], [21, 9], [23, 59]]
    : [[0, 0]]
  // Every day of 2024 to 2026, and the first and last the formatter
  // draws (dates.FirstYear and LastYear).
  const days = [boundary(1, 1, 1), boundary(9999, 12, 31)]
  for (let day = at(2024, 1, 1); day < at(2027, 1, 1); day = new Date(day.getTime() + 864e5)) days.push(day)
  for (const day of days) {
    for (const [hour, minute] of times) {
      const moment = new Date(day.getTime() + (hour * 60 + minute) * 6e4)
      const fields = {
        year: moment.getUTCFullYear(), month: moment.getUTCMonth() + 1, day: moment.getUTCDate(),
        weekday: moment.getUTCDay(), hour, minute
      }
      const ours = formatWith(pattern, fields)
      // formatToParts, not format: V8's format swaps CLDR's U+202F
      // before AM and PM for a plain space, for old pages' sake, and
      // the patterns are CLDR's.
      const theirs = format.formatToParts(moment).map(part => part.value).join('')
      if (ours !== theirs) throw new Error(`${kind} on ${moment.toISOString().slice(0, 16)}: ${JSON.stringify(ours)} where ICU writes ${JSON.stringify(theirs)}`)
    }
  }
}

function standalone (locale, width) {
  const format = formatter(locale, { weekday: width })
  return [4, 5, 6, 7, 8, 9, 10].map(day => format.format(at(2026, 1, day)))
}

function numericParts (locale) {
  return formatter(locale, { day: 'numeric', month: 'numeric', year: 'numeric' })
    .formatToParts(at(2025, 1, 5))
}

// orderOf is the numeric date's order, "dmy", "mdy" or "ymd", which is
// how a typed 12/11 is read.
function orderOf (locale) {
  return numericParts(locale).filter(part => part.type !== 'literal').map(part => part.type[0]).join('')
}

// separatorOf is the mark between that date's numbers: "/", or "." in
// German. Bidi marks around it (Arabic writes RLM before "/") are
// layout, not something a person types, so they are left out.
function separatorOf (locale) {
  return numericParts(locale).find(part => part.type === 'literal').value
    .replace(/\p{Cf}/gu, '').trim()
}

// digitsOf is the locale's ten digits, or undefined for ASCII's.
function digitsOf (locale) {
  const format = new Intl.NumberFormat(locale, { useGrouping: false })
  if (format.resolvedOptions().numberingSystem === 'latn') return undefined
  return [...Array(10)].map((_, digit) => format.format(digit)).join('')
}

function localeData (locale) {
  if (Intl.DateTimeFormat.supportedLocalesOf([locale]).length === 0) throw new Error('ICU has no data for it')
  const calendar = formatter(locale, {}).resolvedOptions().calendar
  if (calendar !== 'gregory') throw new Error(`its calendar is ${calendar}, and dates.mjs draws the Gregorian`)
  const info = new Intl.Locale(locale).getWeekInfo()
  const digits = digitsOf(locale)
  const patterns = {}
  for (const [kind, options] of Object.entries(kinds)) {
    patterns[kind] = patternOf(locale, options, digits)
    check(locale, kind, patterns[kind])
  }
  const order = orderOf(locale)
  if (!['dmy', 'mdy', 'ymd'].includes(order)) throw new Error(`its numeric date runs ${order}`)
  return {
    // firstDay is Go's time.Weekday: 0 is Sunday. Intl counts 1 to 7
    // from Monday.
    firstDay: info.firstDay % 7,
    order,
    separator: separatorOf(locale),
    weekdays: standalone(locale, 'long'),
    narrow: standalone(locale, 'narrow'),
    patterns
  }
}

// goString is a Go string literal for text, with every invisible
// character escaped so the generated file can be read.
function goString (text) {
  return JSON.stringify(text).replace(/[\p{Cf}\p{Zs}\p{Zl}\p{Zp}]/gu,
    char => char === ' ' ? char : '\\u' + char.codePointAt(0).toString(16).padStart(4, '0'))
}

// goFile is one pack's dates_gen.go, as gofmt writes it, so the gate's
// gofmt check passes on what this writes.
export function goFile (pack, records) {
  return execFileSync('gofmt', { input: goSource(pack, records), encoding: 'utf8' })
}

// goSource is one pack's dates_gen.go: each locale's record as JSON, a
// record written once however many locales share it.
function goSource (pack, records) {
  const shared = new Map()
  const lines = []
  for (const [id, record] of records) {
    const json = JSON.stringify(record)
    if (!shared.has(json)) shared.set(json, `record${shared.size}`)
    lines.push(`\t${JSON.stringify(id)}: ${shared.get(json)},`)
  }
  const constants = [...shared].map(([json, name]) => `\t${name} = ${goString(json)}`)
  return `// Code generated by hack/cldrdates; DO NOT EDIT.

// Source: CLDR ${process.versions.cldr} through ICU ${process.versions.icu} (Node's Intl),
// each pattern checked against it for every day of 2024 to 2026. Data
// from the Unicode CLDR, © Unicode, Inc., under the Unicode License v3
// (locales/LICENSE-UNICODE).

package ${pack}

import "amadan.net/rastrillo/rastrillo/locales"

func init() { locales.RegisterDates(${JSON.stringify(pack)}, dates) }

// dates is each locale's date patterns and names, as the JSON the
// dates package reads. A locale with none is not listed.
var dates = map[string]string{
${lines.join('\n')}
}

const (
${constants.join('\n')}
)
`
}

// generate is every pack's records and the coverage lines.
export function generate () {
  const byPack = new Map()
  const coverage = ['# locale\tdates\twhy not']
  for (const [pack, ids] of [...packs()].sort(([a], [b]) => a < b ? -1 : 1)) {
    const records = []
    for (const id of ids.sort()) {
      try {
        records.push([id, localeData(id)])
        coverage.push(`${id}\tyes\t`)
      } catch (error) {
        coverage.push(`${id}\tno\t${error.message}`)
      }
    }
    if (records.length) byPack.set(pack, records)
  }
  return { byPack, coverage: coverage.join('\n') + '\n' }
}

// check compares what generate writes with the committed files, and
// names every one that differs, so a hand edit or a stale file fails.
function checkCommitted ({ byPack, coverage }) {
  const stale = []
  const read = path => { try { return readFileSync(path, 'utf8') } catch { return '' } }
  for (const [pack, records] of byPack) {
    const path = join(root, 'locales', pack, 'dates_gen.go')
    if (read(path) !== goFile(pack, records)) stale.push(path)
  }
  const committed = read(join(here, 'coverage.tsv'))
  if (committed !== coverage) {
    const before = new Set(committed.split('\n'))
    const changed = coverage.split('\n').filter(line => !before.has(line))
    stale.push(`hack/cldrdates/coverage.tsv, which now says:\n  ${changed.slice(0, 5).join('\n  ')}`)
  }
  if (stale.length) {
    console.error(`cldrdates: not what ICU gives; run make locale-dates:\n${stale.join('\n')}`)
    process.exit(1)
  }
  console.log(`cldrdates: ${byPack.size} packs' dates are what ICU ${process.versions.icu} gives`)
}

// probe says whether this Node has what generate needs, and names
// what it lacks. It asks about the environment only, never about a
// pattern: a Node that has everything and draws a date differently is
// a failure for --check to report, not a reason to skip it.
function probe () {
  if (Intl.DateTimeFormat.supportedLocalesOf(['de', 'ja', 'ar', 'hi']).length < 4) {
    console.log('no full ICU (small-icu?)')
  } else if (typeof Intl.Locale.prototype.getWeekInfo !== 'function') {
    console.log('no Intl.Locale.prototype.getWeekInfo')
  } else {
    console.log('ok')
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url) && process.argv[2] === '--probe') {
  probe()
} else if (process.argv[1] === fileURLToPath(import.meta.url) && process.argv[2] === '--check') {
  checkCommitted(generate())
} else if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { byPack, coverage } = generate()
  for (const [pack, records] of byPack) {
    writeFileSync(join(root, 'locales', pack, 'dates_gen.go'), goFile(pack, records))
  }
  writeFileSync(join(here, 'coverage.tsv'), coverage)
  const covered = coverage.split('\n').filter(line => line.includes('\tyes\t')).length
  console.log(`cldrdates: ${covered} locales in ${byPack.size} packs (CLDR ${process.versions.cldr}, ICU ${process.versions.icu}); hack/cldrdates/coverage.tsv lists the rest`)
}
