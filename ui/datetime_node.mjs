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
//	echo '{"locale":"en","vocab":{…}}' | node datetime_node.mjs testdata/datetime/en.json
//
// The vocabulary comes in on stdin rather than out of the fixture,
// because it is not test data: it is the framework's own base catalog
// for that locale, and a fixture carrying its own copy would go stale
// the first time a translator improved a word. The Go test reads it
// from rastrillo.BaseCatalogs() and pipes it in, so the words under
// test are the words that ship.
//
// This file lives beside datetime.js rather than inside a testdata
// directory so `go test` can find it by name, and it is never embedded:
// nothing in ui.go reaches for *.mjs.
import { readFileSync } from "node:fs";
import parser from "./datetime.js";

const fixturePath = process.argv[2];
if (!fixturePath) {
  process.stderr.write("usage: datetime_node.mjs <fixture.json>\n");
  process.exit(2);
}

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const { locale, vocab } = JSON.parse(Buffer.concat(chunks).toString("utf8"));
const cases = JSON.parse(readFileSync(fixturePath, "utf8"));

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

const tables = parser.tables(locale);
const failures = [];

for (const [i, c] of cases.entries()) {
  const id = c.id || `#${i}`;
  let got, err = null;
  try {
    const read = parser.parse(c.input, vocab, tables, at(c.now), {
      prev: c.prev ? at(c.prev) : null,
      base: c.base ? at(c.base) : null,
    });
    got = read ? wire(read.date) : null;
  } catch (e) {
    err = e;
    got = `threw: ${e && e.message}`;
  }
  const want = c.expected === undefined ? null : c.expected;
  if (got !== want) {
    failures.push(
      `  ${id} input=${JSON.stringify(c.input)} now=${c.now}` +
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
