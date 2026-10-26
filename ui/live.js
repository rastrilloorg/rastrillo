/* live.js — live regions. First-party, dependency-free, and inert unless
   the page has the rastrillo-live meta and a data-live region. With
   scripts off a live page is an ordinary page, current when loaded.
   App-owned once scaffolded: edit it like any other static file.

   It answers: which parts of this page are out of date, and how do they
   become current without disturbing the person using them? The stream
   says which topics moved, never what changed; the page fetches its own
   regions again, from its own URL, as its own viewer.

   Vocabulary:
     <meta name="rastrillo-live" content="PATH" data-paused="TEXT">
                         the stream, and the note shown when it stops
     data-live="T@V …"   on a region with an id: its topics, each at the
                         version it was rendered from
     data-live-boot="B"  the server process that rendered it
     data-live-stale     set here on a region left alone while being
                         edited; it is fetched when the editing ends
     id="notice"         the role="status" notice: its contents change
                         in place, the element is never replaced

   One EventSource per page, GET PATH?boot=B&t=T@V&t=T@V, each topic at
   the lowest version a region holds. On a change event, each region
   showing an older version is fetched with Rastrillo-Fragment: <id>,
   and replaced by the answer's element (listed in Rastrillo-Regions).
   A 204 with Rastrillo-Location navigates there, if it is local.

   The rules (the edit checks are for stream fetches; a form's own answer
   swaps the form it was sent from):
   - This file owns reconnecting: an error closes the EventSource, so
     the browser's own retry never runs beside this one, and it reopens
     after 1s, doubling to a minute.
   - One fetch in flight per region, one change queued behind it, and
     an answer applies only if it is from the fetch the region started
     last.
   - Just before a swap, the region must be on the page and not being
     edited (holding the focused field, or a field changed from its
     default). A navigation waits while anything on the page is.
   - Focus moves to the focused element's replacement, else to the
     region itself (tabindex="-1"); never to <body>.
   - A failed fetch is retried after 1s, 2s, 4s; then, as when the
     stream has been down 10s, the notice shows the paused note. It
     clears once the stream is open and no region's fetches are failing.
   - Hidden a minute, the stream closes until shown. pagehide closes it
     and abandons fetches; a back/forward restore reopens it from the
     regions' versions. A prerendered page starts once shown. */
(function () {
  "use strict";

  var NOTICE = "notice";
  var SKIP = /^(button|submit|reset|image|hidden)$/;
  var meta = null;
  var src = null; // the EventSource
  var up = false; // src is open
  var following = ""; // the topics src follows
  var wait = 1000; // the next reconnect's delay
  var reconnect = 0, down = 0, hidden = 0; // timers
  var ctl = null; // aborts this run's fetches; null while stopped
  var seq = 0; // fetches started on this page
  var paused = false;
  // Per region id. Region names are the server's, but "constructor" is
  // a valid one, so nothing here may inherit from Object.
  var regions = Object.create(null);

  // rastrillo.js's guard, as sessions.SafeReturn: anything laxer is an
  // open redirect. Browsers strip tab/CR/LF before parsing, so
  // "/\t/evil.example" would be scheme-relative.
  function localPath(to) {
    return to.charAt(0) === "/" && to.charAt(1) !== "/" &&
      to.indexOf("\\") === -1 && !/[\u0000-\u001f\u007f]/.test(to);
  }

  function state(name) {
    return regions[name] || (regions[name] = {
      last: 0, busy: false, queued: false, stale: false, tries: 0, retry: 0, failed: false
    });
  }

  function token(s) {
    var at = s.lastIndexOf("@");
    return at > 0 ? { topic: s.slice(0, at), v: Number(s.slice(at + 1)), raw: s } : null;
  }

  function tokens(el) {
    return (el.getAttribute("data-live") || "").split(" ").map(token).filter(Boolean);
  }

  // The notice is never fetched as a live region: a fragment request
  // naming it takes the flash, and a background refresh would eat a
  // message nobody saw.
  function live() {
    return Array.prototype.filter.call(document.querySelectorAll("[data-live]"), function (el) {
      return el.id && el.id !== NOTICE;
    });
  }

  // Each topic once, at the lowest version a region holds: that region
  // is still owed a fetch. Regions from two boots (one swapped in after
  // a restart) send no boot, which the server reads as "all changed",
  // rather than one boot's versions as the other's.
  function subscription() {
    var low = Object.create(null), order = [], boot = null;
    live().forEach(function (el) {
      var b = el.getAttribute("data-live-boot") || "";
      boot = boot === null || boot === b ? b : "";
      tokens(el).forEach(function (t) {
        if (!low[t.topic]) order.push(t.topic);
        if (!low[t.topic] || t.v < low[t.topic].v) low[t.topic] = t;
      });
    });
    return {
      boot: boot || "",
      topics: order.join(" "),
      query: order.map(function (k) { return "&t=" + encodeURIComponent(low[k].raw); }).join("")
    };
  }

  function open() {
    clearTimeout(reconnect);
    reconnect = 0;
    var sub = subscription();
    if (!sub.topics) return;
    var path = meta.getAttribute("content") || "";
    var es = new EventSource(path + (path.indexOf("?") < 0 ? "?" : "&") +
      "boot=" + encodeURIComponent(sub.boot) + sub.query);
    src = es;
    following = sub.topics;
    es.addEventListener("open", function () {
      if (src !== es) return;
      up = true;
      wait = 1000;
      clearTimeout(down);
      down = 0;
      unpause();
    });
    es.addEventListener("change", changed);
    // EventSource hides the status, so a refused topic, a full server
    // and a dropped connection all look alike: back off and retry.
    es.addEventListener("error", function () {
      if (src !== es) return;
      close();
      reconnect = setTimeout(open, wait);
      wait = Math.min(wait * 2, 60000);
      if (!down) down = setTimeout(pause, 10000);
    });
  }

  function close() {
    if (src) { src.close(); src = null; }
    up = false;
  }

  function changed(e) {
    var t = token(e.data || "");
    if (!t) return;
    var boot = (e.lastEventId || "").split(" ")[0];
    live().forEach(function (el) {
      var mine = tokens(el).filter(function (x) { return x.topic === t.topic; })[0];
      // Already showing this version (the page's own form fetch landed
      // first) costs nothing. Another boot's number says nothing about
      // this region.
      if (!mine || (el.getAttribute("data-live-boot") === boot && mine.v >= t.v)) return;
      refresh(el.id);
    });
  }

  function refresh(name) {
    var st = state(name), el = document.getElementById(name), run = ctl;
    if (!run || !el) return;
    if (st.busy) { st.queued = true; return; }
    if (editing(el)) { stale(el); return; }
    clearTimeout(st.retry);
    st.busy = true;
    send(location.pathname + location.search, [name], {}, true).then(function () {
      if (run !== ctl) return; // abandoned on pagehide
      st.busy = st.failed = false;
      st.tries = 0;
      unpause();
      if (st.queued) { st.queued = false; refresh(name); }
    }, function () {
      if (run !== ctl) return;
      // The retry fetches the latest, so a queued change rides on it.
      st.busy = st.queued = false;
      // Retries are ordinary requests; they must not run forever.
      if (++st.tries > 3) { st.failed = true; pause(); return; }
      st.retry = setTimeout(function () { refresh(name); }, 1000 << (st.tries - 1));
    });
  }

  // send fetches regions and applies the answer: the one way anything
  // reaches the page, so a form section beside this one gets ordering,
  // focus and the notice rule by calling it with its own URL and init
  // (headers a plain object or Headers; init is not changed). Every
  // region named counts this fetch as the last it started, so whatever
  // it had in flight is superseded. stream turns on the edit checks: a
  // form's answer must swap its own form, which holds unsent input and
  // often the focus.
  function send(url, names, init, stream) {
    var id = ++seq, run = ctl, opts = {}, k;
    if (!run) return Promise.reject(new Error("live.js is stopped"));
    names.forEach(function (n) { state(n).last = id; });
    for (k in init) opts[k] = init[k];
    opts.headers = new Headers(opts.headers || {});
    opts.headers.set("Rastrillo-Fragment", names.join(" "));
    opts.signal = run.signal;
    // Past the HTTP cache: its lock makes a GET wait for any other GET
    // of the same URL still out, so no fetch could overtake a slow one.
    opts.cache = "no-store";
    return fetch(url, opts).then(function (res) {
      var to = res.headers.get("Rastrillo-Location");
      if (res.status === 204 && to) {
        // Leaving discards whatever is typed anywhere on the page, and a
        // background change can wait until it is sent.
        if (stream && editing(document.body)) {
          names.forEach(function (n) {
            var el = document.getElementById(n);
            if (el) { state(n).leave = true; stale(el); }
          });
        } else if (localPath(to)) location.assign(to);
        return res;
      }
      // Only an answer that names its regions is one. A signed-out
      // redirect ends on a whole sign-in page, whose notice would
      // otherwise be swapped into this one.
      var held = res.headers.get("Rastrillo-Regions");
      if (held === null) throw new Error("not a region answer: " + res.status);
      held = held.split(" ");
      return res.text().then(function (html) {
        if (run === ctl) apply(html, names.filter(function (n) { return held.indexOf(n) >= 0; }), id, stream);
        return res;
      });
    });
  }

  function apply(html, names, id, stream) {
    var tpl = document.createElement("template");
    tpl.innerHTML = html;
    Array.prototype.slice.call(tpl.content.children).forEach(function (next) {
      var name = next.id, old = document.getElementById(name);
      if (names.indexOf(name) < 0 || state(name).last !== id || !old) return;
      if (name === NOTICE) {
        // In place: a role="status" element that is replaced is a new
        // live region, and screen readers announce nothing in it.
        if (old.innerHTML !== next.innerHTML) old.innerHTML = next.innerHTML;
        paused = false;
        return;
      }
      if (stream && editing(old)) { stale(old); return; }
      swap(old, next);
    });
    // A swap can change what the page follows (a keyed topic for a new
    // row); a stream left on the old set would never hear of it.
    if (src && subscription().topics !== following) { close(); open(); }
  }

  function swap(old, next) {
    var focused = document.activeElement, inside = !!focused && old.contains(focused);
    // The notice inside a region: the new one is from a render that did
    // not take the flash, so it is empty. Keep the element, and what it
    // is saying.
    var notice = document.getElementById(NOTICE), slot = notice && old.contains(notice) && find(next, NOTICE);
    if (slot) slot.replaceWith(notice);
    old.replaceWith(next);
    state(next.id).stale = false;
    if (!inside) return;
    var to = focused !== old && focused.id ? find(next, focused.id) : null;
    if (to) to.focus({ preventScroll: true });
    if (!to || document.activeElement !== to) {
      next.setAttribute("tabindex", "-1");
      next.focus({ preventScroll: true });
    }
  }

  // Ids are arbitrary strings; comparing them avoids building a
  // selector that would need escaping.
  function find(root, id) {
    if (root.id === id) return root;
    var all = root.querySelectorAll("[id]");
    for (var i = 0; i < all.length; i++) if (all[i].id === id) return all[i];
    return null;
  }

  function editing(el) {
    var a = document.activeElement;
    if (a && a !== el && el.contains(a) && (a.isContentEditable ||
      /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName) && !SKIP.test(a.type))) return true;
    return Array.prototype.some.call(el.querySelectorAll("input,textarea,select"), unsent);
  }

  function unsent(c) {
    var o = c.options, picked = false, i;
    if (SKIP.test(c.type)) return false;
    if (c.type === "file") return c.files.length > 0;
    if (c.type === "checkbox" || c.type === "radio") return c.checked !== c.defaultChecked;
    if (!o) return c.value !== c.defaultValue;
    for (i = 0; i < o.length; i++) if (o[i].defaultSelected) picked = true;
    // With no option marked selected the browser shows the first (none,
    // in a list box). That is the default, not an edit.
    if (!picked) return c.selectedIndex > (c.multiple || c.size > 1 ? -1 : 0);
    for (i = 0; i < o.length; i++) if (o[i].selected !== o[i].defaultSelected) return true;
    return false;
  }

  function stale(el) {
    state(el.id).stale = true;
    el.setAttribute("data-live-stale", "");
  }

  // Read once focus has landed: during focusout it is still on its way.
  // A region held back from navigating waits on the whole page.
  function edited() {
    for (var name in regions) {
      var st = regions[name], el = st.stale && document.getElementById(name);
      if (el && !editing(st.leave ? document.body : el)) { st.stale = st.leave = false; refresh(name); }
    }
  }

  function pause() {
    var n = document.getElementById(NOTICE), text = meta.getAttribute("data-paused");
    if (!n || !text) return;
    n.textContent = text;
    paused = true;
  }

  // The note is true until the stream is open and no region's fetches
  // are failing; either alone can still mean updates are not arriving.
  // Only our own words are cleared: a flash that arrived since stays.
  function unpause() {
    var n = document.getElementById(NOTICE);
    if (!paused || !up) return;
    for (var name in regions) if (regions[name].failed) return;
    if (n && n.textContent === meta.getAttribute("data-paused")) n.textContent = "";
    paused = false;
  }

  // The stream and its timers, for a hidden tab. Fetches already out may
  // still land.
  function quiet() {
    close();
    clearTimeout(reconnect);
    clearTimeout(down);
    reconnect = down = 0;
  }

  function hide() {
    if (!hidden) hidden = setTimeout(function () { hidden = 0; quiet(); }, 60000);
  }

  // pagehide: everything, at once. An answer to a fetch from before the
  // page was cached must not land after the ones fetched on its return,
  // and an open stream or fetch keeps a page out of the cache.
  function stop() {
    quiet();
    clearTimeout(hidden);
    hidden = 0;
    if (ctl) ctl.abort();
    ctl = null;
    for (var name in regions) {
      clearTimeout(regions[name].retry);
      regions[name].busy = regions[name].queued = false;
    }
  }

  function run() {
    ctl = new AbortController();
    wait = 1000;
    open();
    if (document.visibilityState === "hidden") hide();
  }

  function start() {
    meta = document.querySelector('meta[name="rastrillo-live"]');
    if (!meta || !window.EventSource || !window.AbortController || !document.querySelector("[data-live]")) return;
    document.addEventListener("focusout", function () { setTimeout(edited, 0); });
    document.addEventListener("visibilitychange", function () {
      if (!ctl) return;
      if (document.visibilityState === "hidden") { hide(); return; }
      clearTimeout(hidden);
      hidden = 0;
      if (!src && !reconnect) open();
      for (var name in regions) {
        if (regions[name].failed) { regions[name].failed = false; regions[name].tries = 0; refresh(name); }
      }
    });
    addEventListener("pagehide", stop);
    addEventListener("pageshow", function (e) { if (e.persisted && !ctl) run(); });
    run();
  }

  // A prerendered page may never be shown: holding a stream open for a
  // page someone only hovered a link to costs the server a connection.
  function begin() {
    if (document.prerendering) document.addEventListener("prerenderingchange", start, { once: true });
    else start();
  }

  // The seam, for Node and for test pages that define module (as
  // select.js exports its pure half). A browser page has no module.
  if (typeof module !== "undefined" && module && module.exports) module.exports.send = send;

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", begin);
  else begin();
})();
