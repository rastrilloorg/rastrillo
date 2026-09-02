// datetime_node — drives datetime.js's parser over one fixture file, in
// plain Node, with no browser anywhere.
//
// The parser is the risk in datetime.js and the half a Go test cannot
// reach: it is a pure function of (text, vocabulary, locale tables,
// now), and this harness is what lets it be developed and regressed at
// the speed of `go test` instead of a browser drive. Everything that
// touches a page stays behind datetime.js's `document` guard, which
// Node never takes — so loading the file here runs the parser and
// nothing else.
//
// Usage, which ui/datetime_test.go is the only caller of:
//
//	echo '{"locale":"en","catalogs":{"en":{…},…}}' |
//	  node datetime_node.mjs testdata/datetime/en.json
//
// Two modes take the place of a fixture path. --round-trip runs the
// read-back loop: every catalog on stdin, formatted with the combobox's
// own display options and parsed straight back. --calendar holds
// ui/calendar.js's grid arithmetic to its invariants in every shipped
// locale. See roundTrip and calendar below.
//
// The vocabulary comes in on stdin rather than out of the fixture,
// because it is not test data: it is the framework's own base catalog
// for that locale, and a fixture carrying its own copy would go stale
// the first time a translator improved a word. The Go test reads it
// from rastrillo.BaseCatalogs() and pipes it in — every shipped
// catalog, not just the fixture's own — so the words under test are
// the words that ship.
//
// A fixture named after a locale (en.json) is read in that locale. A
// case may also name its own with a "lang" key, which is what
// regressions.json is: one file of cross-locale regressions, each case
// carrying the language it is about, so a bug found in Japanese has
// somewhere to live before the Japanese fixture exists.
//
// This file lives beside datetime.js rather than inside a testdata
// directory so `go test` can find it by name, and it is never embedded:
// nothing in ui.go reaches for *.mjs.
import { readFileSync } from "node:fs";
import parser from "./datetime.js";

const fixturePath = process.argv[2];
if (!fixturePath) {
  process.stderr.write("usage: datetime_node.mjs <fixture.json | --round-trip | --calendar>\n");
  process.exit(2);
}

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const { locale, catalogs } = JSON.parse(Buffer.concat(chunks).toString("utf8"));

// ── the read-back loop ───────────────────────────────────────────────
//
// A field that cannot read its own writing is a field nobody can edit
// in place: focus it, change the year, and the reading it wrote a
// second ago comes back a refusal. That failed silently in five of the
// twelve shipped languages — ja and zh wrote 年 and a bracketed
// weekday, pt wrote "de" linkers, ru wrote a "г." suffix, and yue wrote
// a dayPeriod no catalog spells — and no fixture could have caught it,
// because every fixture is a phrase somebody chose to type.
//
// So this loop types nothing. It formats an instant with the SAME
// options the combobox displays with, hands the result straight back to
// the parser, and asks for the instant again. Run as
// `datetime_node.mjs --round-trip`, over every catalog on stdin.
const roundTrip = () => {
  const samples = [
    new Date(2026, 11, 25, 15, 30),
    new Date(2026, 2, 3, 9, 5),
    new Date(2027, 7, 28, 0, 0),
  ];
  const now = new Date(2026, 7, 28, 10, 30);
  const day = (d) => [d.getFullYear(), d.getMonth(), d.getDate()].join("-");
  const clock = (d) => [d.getHours(), d.getMinutes()].join(":");
  const lines = [], broken = [];
  for (const lang of Object.keys(catalogs)) {
    const tbl = parser.tables(lang);
    const misses = [];
    let ok = 0, total = 0;
    for (const kind of ["date", "datetime", "time"]) {
      let fmt;
      try {
        fmt = new Intl.DateTimeFormat(lang, parser.displayOptions(kind));
      } catch {
        continue;
      }
      for (const want of samples) {
        total++;
        const shown = fmt.format(want);
        let read = null;
        try {
          read = parser.parse(shown, catalogs[lang], tbl, now, {});
        } catch (e) {
          read = null;
        }
        // A date display carries no clock and a time display carries no
        // day, so each kind is asked back only for what it wrote.
        const same = read &&
          (kind === "time" || day(read.date) === day(want)) &&
          (kind === "date" || clock(read.date) === clock(want));
        if (same) ok++;
        else misses.push(`      ${kind} ${JSON.stringify(shown)} -> ${read ? read.date : "null"}`);
      }
    }
    lines.push(`  ${lang}: ${ok}/${total}`);
    if (misses.length) {
      lines.push(...misses);
      broken.push(lang);
    }
  }
  // Every locale, not all-but-one. An earlier version of this gate
  // allowed eleven of twelve so a CLDR bump could not block the build,
  // and that is exactly the hole a reviewer walked through: one locale
  // could stop reading its own writing, the harness would still exit 0,
  // and the only trace was a name in a t.Log nobody reads on a passing
  // test. A language that cannot re-read its own date is broken for the
  // people who speak it, whoever caused it, so it fails — and the
  // failure line names the languages rather than making anyone go and
  // count the columns.
  const langs = Object.keys(catalogs).length;
  const good = langs - broken.length;
  process.stdout.write(
    `own-display read-back: ${good} of ${langs} locales round-trip fully\n` +
      `${lines.join("\n")}\n` +
      (broken.length
        ? `FAIL: ${broken.length} locale(s) cannot read their own display back: ${broken.join(", ")}\n`
        : ""),
  );
  process.exit(broken.length ? 1 : 0);
};

if (fixturePath === "--round-trip") roundTrip();

// ── the calendar's arithmetic ────────────────────────────────────────
//
// ui/calendar.js draws the month grid a date field opens. Everything in
// it that touches a page is behind a `document` guard Node never takes,
// and the three functions in front of that guard are the ones that can
// be quietly WRONG: a grid that drops a day in a zone that moved its
// clocks that morning, a week that starts on the wrong day, column
// headings that do not describe their own columns.
//
// None of that needs a browser to catch, and none of it shows up in a
// screenshot either — a calendar missing 1 March looks exactly like a
// calendar. So this mode asserts the invariants directly, over every
// locale the framework ships and over a span of months wide enough to
// include the awkward ones: a leap February, both hemispheres' clock
// changes, and a month beginning on each of the seven weekdays.
//
// Run as `datetime_node.mjs --calendar`, with the same catalogs on
// stdin as every other mode — used here only for the list of locales.
const calendar = async () => {
  const cal = (await import("./calendar.js")).default;
  const two = (n) => String(n).padStart(2, "0");
  const day = (d) => `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
  const langs = Object.keys(catalogs);
  const broken = new Set();
  const failures = [];
  const anchors = [];

  // Every month of 2026 and 2027, plus February 2028 for the leap day.
  // Wide enough that a month beginning on each of the seven weekdays
  // appears several times over, and that both hemispheres' clock
  // changes are covered whatever zone the runner sits in.
  const months = [];
  for (let y = 2026; y <= 2027; y++) for (let m = 0; m < 12; m++) months.push([y, m]);
  months.push([2028, 1]);

  // Four facts about the world, pinned in the TEST rather than in the
  // file under it. calendar.js derives the first day of the week from
  // Intl and keeps no table, which is right — but a derivation that
  // came back consistently wrong would still satisfy every invariant
  // below, because they all check the grid against the same number.
  // These four are the anchor: they are not in dispute, they are
  // stable in CLDR, and they belong here, where a table is evidence
  // rather than a thing to maintain.
  for (const [tag, want, who] of [
    ["en-US", 0, "the United States starts its weeks on Sunday"],
    ["en-GB", 1, "the United Kingdom starts its weeks on Monday"],
    ["ar-EG", 6, "Egypt starts its weeks on Saturday"],
    ["ja-JP", 0, "Japan starts its weeks on Sunday"],
  ]) {
    const got = cal.firstDayOfWeek(tag);
    if (got !== want) anchors.push(`      ${tag}: firstDayOfWeek = ${got}, want ${want} — ${who}`);
  }

  for (const lang of langs) {
    const say = (msg) => {
      broken.add(lang);
      if (failures.length < 40) failures.push(`      ${lang}: ${msg}`);
    };
    const first = cal.firstDayOfWeek(lang);
    const heads = cal.weekdayNames(lang, first);

    if (!Number.isInteger(first) || first < 0 || first > 6) {
      say(`firstDayOfWeek = ${first}, which is not a getDay number`);
    }
    if (heads.length !== 7) say(`weekdayNames gave ${heads.length} columns, want 7`);
    else {
      // The headings must describe the columns they sit over. This is
      // the failure that would look right and read wrong: a grid
      // starting on Monday under a heading row starting on Sunday is
      // off by one the whole way across, and every date in it is
      // announced on the wrong weekday.
      for (let i = 0; i < 7; i++) {
        if (heads[i].day !== (first + i) % 7) {
          say(`column ${i} heads day ${heads[i].day}, want ${(first + i) % 7}`);
        }
        if (!heads[i].short || !heads[i].long) say(`column ${i} has an empty name`);
      }
      if (new Set(heads.map((h) => h.long)).size !== 7) {
        say(`the seven spoken weekday names are not seven distinct names`);
      }
    }

    for (const [y, m] of months) {
      const grid = cal.monthGrid(y, m, first);
      const where = `${y}-${two(m + 1)}`;

      // Always 42 cells, so the panel keeps one height all year.
      if (grid.length !== 42) {
        say(`${where}: ${grid.length} cells, want 42`);
        continue;
      }

      // Every cell at local midnight, and each exactly one day after
      // the one before it. Building the grid by adding 86,400,000
      // milliseconds instead of counting whole days breaks precisely
      // here, in the two months a clock changes, and nowhere else.
      let stop = false;
      for (let i = 0; i < 42 && !stop; i++) {
        const d = grid[i];
        if (d.getHours() || d.getMinutes() || d.getSeconds()) {
          say(`${where}: cell ${i} is ${d}, not local midnight`);
          stop = true;
        } else if (i > 0) {
          const p = grid[i - 1];
          const want = new Date(p.getFullYear(), p.getMonth(), p.getDate() + 1);
          if (day(d) !== day(want)) {
            say(`${where}: cell ${i} is ${day(d)}, want ${day(want)} — a day was dropped or repeated`);
            stop = true;
          }
        }
      }
      if (stop) continue;

      if (grid[0].getDay() !== first) {
        say(`${where}: the grid starts on day ${grid[0].getDay()}, want ${first}`);
      }

      // Every day of the month present, exactly once, with the 1st in
      // the first week. Six rows is enough for every month there is —
      // 31 days beginning on the last day of the week needs 37 cells —
      // but only when the leading run is the right length.
      const inMonth = grid.filter((d) => d.getMonth() === m && d.getFullYear() === y);
      const last = new Date(y, m + 1, 0).getDate();
      if (inMonth.length !== last) {
        say(`${where}: ${inMonth.length} of the month's ${last} days are on the grid`);
      }
      const at = grid.findIndex((d) => d.getDate() === 1 && d.getMonth() === m);
      if (at < 0 || at > 6) say(`${where}: the 1st sits at cell ${at}, not in the first week`);
    }
  }

  process.stdout.write(
    `calendar grid: ${langs.length - broken.size} of ${langs.length} locales draw ` +
      `${months.length} months each with the right days in the right columns\n` +
      (anchors.length
        ? `FAIL: the first day of the week is derived wrongly in ${anchors.length} of 4 anchor locales:\n` +
          `${anchors.join("\n")}\n`
        : "") +
      (broken.size
        ? `FAIL: ${broken.size} locale(s) draw a month wrong: ${[...broken].join(", ")}\n` +
          `${failures.join("\n")}\n`
        : ""),
  );
  process.exit(broken.size || anchors.length ? 1 : 0);
};

if (fixturePath === "--calendar") await calendar();


const cases = JSON.parse(readFileSync(fixturePath, "utf8"));

// One set of locale tables per language in the file, built once.
const tablesFor = new Map();
const langOf = (c) => c.lang || locale;
const setupFor = (lang) => {
  if (!tablesFor.has(lang)) {
    if (!catalogs[lang]) throw new Error(`no catalog for lang ${JSON.stringify(lang)}`);
    tablesFor.set(lang, parser.tables(lang));
  }
  return { tables: tablesFor.get(lang), vocab: catalogs[lang] };
};

// Wall-clock strings, both ways, in the local zone the Go test pins —
// deliberately not Date.parse, which reads a bare "2026-08-28T10:30" as
// UTC and would move every expectation by the runner's offset.
const at = (s) => {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}))?$/.exec(String(s));
  if (!m) throw new Error(`fixture: ${JSON.stringify(s)} is not a wall-clock date`);
  return new Date(+m[1], +m[2] - 1, +m[3], m[4] ? +m[4] : 0, m[5] ? +m[5] : 0);
};
const pad = (n) => String(n).padStart(2, "0");
const wire = (d) =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;

const failures = [];

for (const [i, c] of cases.entries()) {
  const id = c.id || `#${i}`;
  // A missing "expected" is a typo, not an expectation of null. Read as
  // null it would pass against any refusal, which is exactly the answer
  // a broken parser gives — a whole case quietly asserting nothing.
  if (!Object.prototype.hasOwnProperty.call(c, "expected")) {
    throw new Error(`case ${id} in ${fixturePath} has no "expected" key`);
  }
  const lang = langOf(c);
  let got, err = null, override = null;
  try {
    const setup = setupFor(lang);
    // parseAll, not parse, because parseAll is what the FIELD reads
    // with: the exact grammar first, and where that refuses, the
    // readings a half-typed word could still be finished into. Its
    // first answer is the one Enter takes, so that is what "expected"
    // means here, and a case may name the rest with "alternatives".
    //
    // Every reading in the list is the strict grammar's own — a
    // completion only ever hands it a longer sentence to read — so a
    // fixture asserting null still asserts exactly what it used to.
    const reads = parser.parseAll(c.input, setup.vocab, setup.tables, at(c.now), {
      prev: c.prev ? at(c.prev) : null,
      base: c.base ? at(c.base) : null,
    });
    got = reads.length ? wire(reads[0].date) : null;
    if (Object.prototype.hasOwnProperty.call(c, "alternatives")) {
      const rest = reads.slice(1).map((r) => wire(r.date));
      if (JSON.stringify(rest) !== JSON.stringify(c.alternatives)) {
        got = `${got} + alternatives ${JSON.stringify(rest)}`;
        override = `${c.expected} + alternatives ${JSON.stringify(c.alternatives)}`;
      }
    }
  } catch (e) {
    err = e;
    got = `threw: ${e && e.message}`;
  }
  const want = override === null ? c.expected : override;
  if (got !== want) {
    failures.push(
      `  ${id} [${lang}] input=${JSON.stringify(c.input)} now=${c.now}` +
        `${c.prev ? ` prev=${c.prev}` : ""}${c.base ? ` base=${c.base}` : ""}\n` +
        `      got  ${JSON.stringify(got)}\n      want ${JSON.stringify(want)}` +
        (err ? `\n      ${err.stack}` : ""),
    );
  }
}

if (failures.length) {
  process.stdout.write(
    `${failures.length} of ${cases.length} cases failed in ${fixturePath}:\n${failures.join("\n")}\n`,
  );
  process.exit(1);
}
process.stdout.write(`${cases.length} cases passed in ${fixturePath}\n`);
