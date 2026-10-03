// shell_node — runs shell.js against a stub page in plain Node, one
// fresh script per page and one sessionStorage for the tab, so a record
// one page writes is what the next page reads, as in a browser. Only
// the direction is driven: what the back control's click leaves behind
// and which way the next page's transition is typed.
//
// ui/shell_test.go is the only caller. It reads back one JSON object:
// each scenario's name and the transition type the last page got.
//
// This file lives beside shell.js so `go test` finds it by name; it is
// never embedded, and nothing in ui.go reaches for *.mjs.
import { readFileSync } from "node:fs";
import vm from "node:vm";

const src = readFileSync(new URL("./shell.js", import.meta.url), "utf8");

// A page: its URL, its view ("page" or "index"), and the href of its
// back control, if it has one. No Navigation API, so the click never
// takes the history.back() path and always leaves its record behind.
function open(storage, url, view, backHref) {
  const on = { window: {}, document: {} };
  const location = new URL(url);
  const root = { matches: (sel) => sel.includes(`~="${view}"`) };
  const back = backHref && {
    href: new URL(backHref, url).href,
    closest: (sel) => (sel.includes("[rst-shell-back]") ? back : null),
  };
  const document = {
    prerendering: false,
    querySelector: (sel) => (sel.includes("[rst-shell-sidebar]") ? root : null),
    querySelectorAll: () => [],
    getElementById: () => null,
    addEventListener: (type, fn) => { on.document[type] = fn; },
  };
  const window = {
    sessionStorage: storage,
    onpagereveal: null,
    onpageswap: null,
    addEventListener: (type, fn) => { on.window[type] = fn; },
  };
  vm.runInNewContext(src, { window, document, location, URL, matchMedia: () => ({ matches: true }) });
  return {
    tapBack() {
      on.document.click({ target: back, button: 0, defaultPrevented: false, preventDefault() {} });
    },
    leave() { on.window.pageswap({}); },
    reveal() {
      const types = new Set();
      on.window.pagereveal({ viewTransition: { types } });
      return [...types].join(",");
    },
  };
}

function tab() {
  const m = new Map();
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => { m.set(k, String(v)); },
    removeItem: (k) => { m.delete(k); },
  };
}

const out = {};

// Back is tapped and the page really goes back to its up page.
{
  const s = tab();
  const p = open(s, "https://app.test/invoices", "page", "/#nav-invoices");
  p.tapBack();
  p.leave();
  out.taken = open(s, "https://app.test/#nav-invoices", "index").reveal();
}

// Back is tapped, the reader cancels the leave-page prompt, then follows
// a link to another content page: that is a forward navigation.
{
  const s = tab();
  const p = open(s, "https://app.test/invoices", "page", "/");
  p.tapBack();
  p.leave();
  out.cancelled = open(s, "https://app.test/orders", "page", "/").reveal();
}

process.stdout.write(JSON.stringify(out));
