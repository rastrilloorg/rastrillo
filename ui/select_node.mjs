// select_node — drives select.js's pure half in plain Node: how a query
// ranks a select's options, what leaving the box after a search commits,
// and which blanks are prompts. Everything that touches a page stays
// behind select.js's `document` guard, which Node never takes.
//
// ui/select_test.go is the only caller. It pipes {options, queries} on
// stdin and reads back each query's values, best first, and the value
// leaving the box would settle on; or {blanks}, and reads back what the
// box shows and lists for each blank option.
//
// This file lives beside select.js so `go test` finds it by name; it is
// never embedded, and nothing in ui.go reaches for *.mjs.
import sel from "./select.js";

const { order, settle, isPrompt, offered } = sel;

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const { options, queries, blanks } = JSON.parse(raw);
if (blanks) {
  process.stdout.write(JSON.stringify(blanks.map((b) => ({
    prompt: isPrompt(b.value, b.marked, b.required, b.disabled),
    offered: offered(b.value, b.marked, b.required, b.searching, b.disabled),
  }))));
} else {
  const out = {};
  const settled = {};
  for (const q of queries) {
    const hits = order(options, q);
    out[q] = hits.map((o) => o.value);
    const s = settle(hits, q);
    settled[q] = s ? s.value : "";
  }
  process.stdout.write(JSON.stringify({ order: out, settled }));
}
