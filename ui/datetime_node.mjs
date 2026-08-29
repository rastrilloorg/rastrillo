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
// With --round-trip in place of a fixture it runs the read-back loop
// instead: every catalog on stdin, formatted with the combobox's own
// display options and parsed straight back. See roundTrip below.
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
  process.stderr.write("usage: datetime_node.mjs <fixture.json | --round-trip>\n");
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
  // Eleven of twelve, not twelve of twelve: a locale whose display Intl
  // changes under us should be reported by name rather than block the
  // build on a CLDR bump. The names are in the output either way.
  const want = 11;
  const good = Object.keys(catalogs).length - broken.length;
  process.stdout.write(
    `own-display read-back: ${good} of ${Object.keys(catalogs).length} locales round-trip fully\n` +
      `${lines.join("\n")}\n` +
      (broken.length ? `  not round-tripping: ${broken.join(", ")}\n` : ""),
  );
  process.exit(good >= want ? 0 : 1);
};

if (fixturePath === "--round-trip") roundTrip();

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
  let got, err = null;
  try {
    const setup = setupFor(lang);
    const read = parser.parse(c.input, setup.vocab, setup.tables, at(c.now), {
      prev: c.prev ? at(c.prev) : null,
      base: c.base ? at(c.base) : null,
    });
    got = read ? wire(read.date) : null;
  } catch (e) {
    err = e;
    got = `threw: ${e && e.message}`;
  }
  const want = c.expected;
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
