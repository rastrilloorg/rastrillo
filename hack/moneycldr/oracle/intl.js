// The independent oracle: ICU, through Node's Intl.NumberFormat.
//
//   node oracle/intl.js < grid.tsv > intl.tsv
//
// Input rows: category, tag, code, exponent, minor, exact decimal.
// Output appends the locale ICU resolved (empty when ICU has no data for
// the tag's language, so the row says nothing about our output) and
// ICU's string, then a "#complete<TAB>n" line, so cmd/check can tell a
// finished run from a truncated one.
// It refuses to run against a CLDR version other than the table's,
// because then a difference would measure the data drift, not the code.
"use strict";
const fs = require("fs");
const readline = require("readline");

const want = (fs.readFileSync(__dirname + "/../../../locales/core_gen.go", "utf8").match(/CLDRVersion = "([^"]+)"/) || [])[1] || "";
if (!want.startsWith(process.versions.cldr.split(".")[0] + ".")) {
  console.error(`oracle: Node has CLDR ${process.versions.cldr}, the table is cldr-json ${want}`);
  process.exit(1);
}

const formatters = new Map();
const resolvedFor = new Map();
function formatter(tag, code, exp) {
  const key = tag + "\t" + code;
  let f = formatters.get(key);
  if (!f) {
    f = new Intl.NumberFormat(tag, {
      style: "currency", currency: code,
      minimumFractionDigits: exp, maximumFractionDigits: exp,
    });
    formatters.set(key, f);
  }
  return f;
}
function icuLocale(tag) {
  if (!resolvedFor.has(tag)) {
    let r = "";
    try {
      const norm = tag.replace(/_/g, "-");
      if (Intl.NumberFormat.supportedLocalesOf([norm]).length > 0) {
        r = new Intl.NumberFormat(norm).resolvedOptions().locale;
      }
    } catch (e) {
      // Never an empty answer: cmd/check treats "" as "ICU has no data",
      // which is only allowed for pinned tags.
      r = "!" + String(e.message).replace(/[\t\n]/g, " ");
    }
    resolvedFor.set(tag, r);
  }
  return resolvedFor.get(tag);
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
const out = [];
let count = 0;
rl.on("line", (line) => {
  count++;
  const [, tag, code, exp, , dec] = line.split("\t");
  const loc = icuLocale(tag);
  let s = "";
  if (loc && !loc.startsWith("!")) {
    try {
      s = formatter(tag.replace(/_/g, "-"), code, Number(exp)).format(dec);
    } catch (e) {
      out.push(`${line}\t!${String(e.message).replace(/[\t\n]/g, " ")}\t`);
      return;
    }
  }
  out.push(`${line}\t${loc}\t${s}`);
  if (out.length >= 10000) process.stdout.write(out.splice(0).join("\n") + "\n");
});
rl.on("close", () => {
  if (out.length) process.stdout.write(out.join("\n") + "\n");
  process.stdout.write(`#complete\t${count}\n`);
});
