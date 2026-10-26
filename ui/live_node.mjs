// live_node — loads live.js into a stub document in plain Node and
// reports how the stream starts, reconnects and gives up: EventSources
// opened (with their URLs and whether they were closed), fetches
// started, and the timers the script asked for. EventSource is
// controllable (the harness fires open, error and change), fetch
// rejects, and timers run only when the harness says so, so a backoff
// is read as the delays requested rather than waited out.
//
// ui/live_test.go is the only caller. Each scenario is named by the
// first argument and prints one JSON object.
//
// This file lives beside live.js so `go test` finds it by name; it is
// never embedded, and nothing in ui.go reaches for *.mjs.
import { readFileSync } from "node:fs";
import vm from "node:vm";

const src = readFileSync(new URL("./live.js", import.meta.url), "utf8");

function element(attrs, extra) {
  return Object.assign({
    id: attrs.id || "",
    textContent: "",
    getAttribute: (k) => (k in attrs ? attrs[k] : null),
    hasAttribute: (k) => k in attrs,
    setAttribute: (k, v) => { attrs[k] = String(v); },
    removeAttribute: (k) => { delete attrs[k]; },
    contains: () => false,
    querySelectorAll: () => [],
  }, extra);
}

// page builds one stub page. regions are attribute maps; meta is the
// rastrillo-live meta's attributes, or null for a page without one.
function page({ regions = [], meta = { content: "/live", "data-paused": "Paused" }, prerendering = false } = {}) {
  const n = { sources: [], fetches: [], timers: [] };
  const on = {};
  const won = {};
  const els = regions.map((a) => element(a));
  const notice = element({ id: "notice", role: "status" });
  const metaEl = meta && element(meta);
  const document = {
    readyState: "complete",
    prerendering,
    visibilityState: "visible",
    activeElement: null,
    querySelector: (sel) => {
      if (sel.includes("rastrillo-live")) return metaEl;
      if (sel === "[data-live]") return els[0] || null;
      return null;
    },
    querySelectorAll: (sel) => (sel === "[data-live]" ? els : []),
    getElementById: (id) => (id === "notice" ? notice : els.find((e) => e.id === id) || null),
    addEventListener: (type, fn) => { (on[type] = on[type] || []).push(fn); },
    // Answers here carry no elements, so a parsed template is empty.
    createElement: () => ({ innerHTML: "", content: { children: [] } }),
  };
  class EventSource {
    constructor(url) {
      this.url = url;
      this.closed = false;
      this.on = {};
      n.sources.push(this);
    }
    addEventListener(type, fn) { (this.on[type] = this.on[type] || []).push(fn); }
    close() { this.closed = true; }
    fire(type, ev = {}) { for (const fn of this.on[type] || []) fn.call(this, ev); }
  }
  const module = { exports: {} };
  const win = {
    document,
    EventSource,
    AbortController,
    Headers,
    module,
    location: { pathname: "/", search: "", assign: () => {} },
    addEventListener: (type, fn) => { (won[type] = won[type] || []).push(fn); },
    // Rejects, as a network failure does, until the scenario sets
    // n.answer; then it answers 200 with Rastrillo-Regions and no body.
    fetch: (url, init) => {
      n.fetches.push({ url, init, headers: Object.fromEntries(init.headers) });
      if (!n.answer) return Promise.reject(new TypeError("Failed to fetch"));
      const h = { "rastrillo-regions": n.answer };
      return Promise.resolve({ status: 200, headers: { get: (k) => h[k.toLowerCase()] ?? null }, text: () => Promise.resolve("") });
    },
    setTimeout: (fn, ms) => { n.timers.push({ fn, ms, done: false }); return n.timers.length; },
    clearTimeout: (id) => { if (n.timers[id - 1]) n.timers[id - 1].done = true; },
  };
  win.window = win;
  vm.runInNewContext(src, win);
  const settle = () => new Promise((r) => setImmediate(r));
  return {
    n, notice, document, send: module.exports.send,
    latest: () => n.sources[n.sources.length - 1],
    fireDoc: (type) => { for (const fn of on[type] || []) fn({}); },
    // pending lists the delays of the timers not yet run or cleared.
    pending: () => n.timers.filter((t) => !t.done).map((t) => t.ms),
    // run runs every pending timer with this delay, once.
    run: async (ms) => {
      for (const t of n.timers.filter((t) => !t.done && t.ms === ms)) { t.done = true; t.fn(); }
      await settle();
    },
    settle,
  };
}

const region = (id, live, boot = "b00t") => ({ id, "data-live": live, "data-live-boot": boot });

const scenarios = {
  // No meta, or no region: nothing opens and nothing is fetched.
  inert() {
    const noMeta = page({ regions: [region("list", "a@1")], meta: null });
    const noRegion = page({ regions: [] });
    return {
      noMeta: { sources: noMeta.n.sources.length, fetches: noMeta.n.fetches.length },
      noRegion: { sources: noRegion.n.sources.length, fetches: noRegion.n.fetches.length },
    };
  },
  // Three regions over two topics: one stream, each topic once, at the
  // lowest version a region holds.
  many() {
    const p = page({ regions: [region("one", "a@4"), region("two", "b/x_1@9"), region("three", "a@2 b/x_1@11")] });
    return { sources: p.n.sources.length, url: p.latest() && p.latest().url };
  },
  lowest() {
    const p = page({ regions: [region("one", "a@3"), region("two", "a@5")] });
    return { url: p.latest() && p.latest().url };
  },
  // An error closes the stream at once; the next one waits for the
  // backoff timer, which doubles to a minute and resets once open.
  async reconnect() {
    const p = page({ regions: [region("list", "a@1")] });
    const first = p.latest();
    first.fire("error");
    const out = { closed: first.closed, sourcesAfterError: p.n.sources.length, pendingAfterError: p.pending() };
    await p.run(1000);
    out.sourcesAfterTimer = p.n.sources.length;
    const delays = [1000];
    for (let i = 0; i < 8; i++) {
      p.latest().fire("error");
      const backoff = p.pending().filter((ms) => ms !== 10000);
      delays.push(backoff[0]);
      await p.run(backoff[0]);
    }
    out.delays = delays;
    p.latest().fire("open");
    p.latest().fire("error");
    out.afterOpen = p.pending().filter((ms) => ms !== 10000);
    return out;
  },
  // A change whose fetch keeps failing is retried after 1s, 2s and 4s,
  // then the page shows the paused note. The stream reopening does not
  // clear it, since that region is still behind; its next fetch landing
  // does.
  async retry() {
    const p = page({ regions: [region("list", "a@1")] });
    p.latest().fire("open");
    p.latest().fire("change", { data: "a@2", lastEventId: "b00t a@2" });
    await p.settle();
    const out = { fetches: [p.n.fetches.length], headers: p.n.fetches[0] && p.n.fetches[0].headers, delays: [] };
    for (let i = 0; i < 3; i++) {
      const d = p.pending();
      out.delays.push(d[0]);
      await p.run(d[0]);
      out.fetches.push(p.n.fetches.length);
    }
    out.pendingAfter = p.pending();
    out.notice = p.notice.textContent;
    p.latest().fire("error");
    await p.run(1000);
    p.latest().fire("open");
    out.noticeAfterOpen = p.notice.textContent;
    p.n.answer = "list";
    p.latest().fire("change", { data: "a@3", lastEventId: "b00t a@3" });
    await p.settle();
    out.noticeAfterSuccess = p.notice.textContent;
    return out;
  },
  // The seam a forms section calls: it copies the caller's init rather
  // than writing into it, takes Headers as well as a plain object, and
  // answers with a promise even when the page is stopped.
  async seam() {
    const p = page({ regions: [region("list", "a@1")] });
    p.n.answer = "list";
    const init = { method: "POST", headers: new Headers({ "Rastrillo-Page": "/notes" }) };
    await p.send("/notes", ["list", "notice"], init, false);
    const sent = p.n.fetches[0];
    const plain = { headers: { "Rastrillo-Page": "/notes" } };
    await p.send("/notes", ["list"], plain, false);
    const inert = page({ regions: [region("list", "a@1")], meta: null });
    let stopped;
    try {
      stopped = await inert.send("/notes", ["list"], {}, false).then(() => "resolved", () => "rejected");
    } catch (e) {
      stopped = "threw " + e;
    }
    return {
      method: sent.init.method,
      cache: sent.init.cache,
      headers: sent.headers,
      plainHeaders: p.n.fetches[1].headers,
      callerHeaders: Object.fromEntries(init.headers),
      callerKeys: Object.keys(init).concat(Object.keys(plain), Object.keys(plain.headers)),
      stopped,
    };
  },
  // A change for a version the region already shows fetches nothing.
  async current() {
    const p = page({ regions: [region("list", "a@5")] });
    p.latest().fire("change", { data: "a@5", lastEventId: "b00t a@5" });
    await p.settle();
    const same = p.n.fetches.length;
    p.latest().fire("change", { data: "a@5", lastEventId: "n3wb00t a@5" });
    await p.settle();
    return { sameBoot: same, newBoot: p.n.fetches.length };
  },
  prerender() {
    const p = page({ regions: [region("list", "a@1")], prerendering: true });
    const before = p.n.sources.length;
    p.document.prerendering = false;
    p.fireDoc("prerenderingchange");
    return { before, after: p.n.sources.length };
  },
};

process.stdout.write(JSON.stringify(await scenarios[process.argv[2]]()));
