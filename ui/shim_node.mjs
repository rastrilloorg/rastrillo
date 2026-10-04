// shim_node — loads rastrillo.js into a stub document in plain Node and
// counts how polling starts: timers scheduled and EventSources opened
// for one data-poll element, on a page someone is viewing, on a
// prerendered page nobody is viewing yet, and on that page once it is
// activated.
//
// ui/shim_test.go is the only caller. It reads back one JSON object of
// counts.
//
// This file lives beside rastrillo.js so `go test` finds it by name; it
// is never embedded, and nothing in ui.go reaches for *.mjs.
import { readFileSync } from "node:fs";
import vm from "node:vm";

const src = readFileSync(new URL("./rastrillo.js", import.meta.url), "utf8");

function load(prerendering, push) {
  const n = { timers: 0, sources: 0 };
  const on = {};
  const attrs = { "data-poll": "/status" };
  if (push) attrs["data-poll-push"] = "/status/events";
  const el = { getAttribute: (k) => (k in attrs ? attrs[k] : null) };
  const document = {
    readyState: "complete",
    prerendering,
    querySelectorAll: (sel) => (sel === "[data-poll]" ? [el] : []),
    addEventListener: (type, fn) => { (on[type] = on[type] || []).push(fn); },
  };
  function EventSource() { n.sources++; this.addEventListener = () => {}; }
  const window = { EventSource };
  vm.runInNewContext(src, {
    window, document, EventSource,
    setTimeout: () => { n.timers++; },
    fetch: () => new Promise(() => {}),
  });
  const before = { ...n };
  document.prerendering = false;
  for (const fn of on.prerenderingchange || []) fn({});
  return { before, after: { ...n } };
}

const out = {};
for (const push of [false, true]) {
  const kind = push ? "push" : "timer";
  out[kind] = { viewed: load(false, push).before, prerendered: load(true, push) };
}
process.stdout.write(JSON.stringify(out));
