// The plain-number oracle: ICU, through Node's Intl.NumberFormat,
// against number.Format.
//
//   go run ./cmd/numcheck | node oracle/numbers.js
//
// Input rows: locale id, exact decimal, style, ours. A percent row
// gives ICU the value divided by 100, because number.Format writes the
// value as shown (50 is 50 %). Values go to Intl as strings, so no row
// passes through a float. It refuses a Node whose CLDR is not the
// table's, and a truncated grid.
"use strict";
const fs = require("fs");

const want = (fs.readFileSync(__dirname + "/../../../locales/core_gen.go", "utf8").match(/CLDRVersion = "([^"]+)"/) || [])[1] || "";
if (!want.startsWith(process.versions.cldr.split(".")[0] + ".")) {
  console.error(`numbers: Node has CLDR ${process.versions.cldr}, the table is cldr-json ${want}`);
  process.exit(1);
}

// exemptions turn ICU's string into ours where ICU's data and
// cldr-json's disagree, for one locale; any other difference fails.
const exemptions = {
  // As for money (cmd/check/exemptions.go): ICU groups fr-CH with
  // U+0027, cldr-json 48 with U+202F.
  "fr-CH": s => s.replaceAll("'", "\u202f"),
};

// hundredth moves an exact decimal's point two places left.
function hundredth(dec) {
  const negative = dec.startsWith("-");
  let [whole, frac = ""] = (negative ? dec.slice(1) : dec).split(".");
  whole = whole.padStart(3, "0");
  const out = whole.slice(0, -2).replace(/^0+(?=\d)/, "") + "." + whole.slice(-2) + frac;
  return (negative ? "-" : "") + out;
}

function options(style, dec) {
  if (["NaN", "+Inf", "-Inf"].includes(dec)) dec = "0";
  const frac = (dec.split(".")[1] || "").length;
  switch (style) {
    case "plain": return { useGrouping: false, maximumFractionDigits: 20 };
    case "places2": return { minimumFractionDigits: Math.max(2, frac), maximumFractionDigits: 20 };
    case "percent": return { style: "percent", minimumFractionDigits: frac, maximumFractionDigits: 20 };
  }
  return { maximumFractionDigits: 20 };
}

// unsupported are locales ICU has no data for (it answers in its
// default instead, which says nothing about ours); cmd/check pins the
// same list as icuUnsupported, and a locale ICU gains or loses fails.
const unsupported = new Set(["ht"]);

const lines = fs.readFileSync(0, "utf8").split("\n").filter(Boolean);
const last = lines.pop() || "";
if (!last.startsWith("#rows\t") || Number(last.split("\t")[1]) !== lines.length || lines.length === 0) {
  console.error("numbers: the grid is truncated or empty");
  process.exit(1);
}
const wrong = [];
let exempted = 0;
let skipped = 0;
for (const line of lines) {
  const [id, dec, style, ours] = line.split("\t");
  const supported = Intl.NumberFormat.supportedLocalesOf([id]).length > 0;
  if (supported === unsupported.has(id)) {
    wrong.push(`${id}\tICU ${supported ? "now has" : "has no"} data for it: update unsupported`);
    continue;
  }
  if (!supported) { skipped++; continue; }
  // NaN and the infinities come as Go prints them; a percent of either
  // is itself.
  const special = { NaN: NaN, "+Inf": Infinity, "-Inf": -Infinity };
  const value = dec in special ? special[dec] : style === "percent" ? hundredth(dec) : dec;
  const icu = new Intl.NumberFormat(id, options(style, dec)).format(value);
  if (icu === ours) continue;
  if (exemptions[id] && exemptions[id](icu) === ours) { exempted++; continue; }
  wrong.push(`${id}\t${dec}\t${style}\tours ${JSON.stringify(ours)}\tICU ${JSON.stringify(icu)}`);
}
console.log(`numbers: ${lines.length} rows, ${lines.length - wrong.length - exempted - skipped} equal ICU, ${exempted} exempted, ${skipped} without ICU data, ${wrong.length} differ`);
if (wrong.length) {
  console.log(wrong.slice(0, 60).join("\n"));
  process.exit(1);
}
