/* gallery.js — the design-system gallery's own script, and the only one
   in the tree that is not part of the framework. rastrillo.js, select.js
   and datetime.js are shipped to apps; this file is furniture for the
   page that shows them off, so it lives beside the renderer rather than
   in ui/ and no scaffold ever writes it.

   Same rules as its three neighbours all the same: first-party,
   dependency-free, no network, and inert-safe — the page it enhances is
   a complete document with scripts off. What it adds, a section each:
   - the colour scheme: data-theme on <html>, remembered as
     rst-ds-scheme, and painted into every preview frame;
   - the rail filter;
   - keeping your place across a theme or language switch;
   - copy buttons.

   Why it is a blocking <script> in <head> rather than a deferred one at
   the foot, which is how the other three load: both of the things it
   does have to happen before the first paint. Applying a remembered
   Dark after the body has parsed is a visible flash of the system
   scheme on every load, and revealing the toggle after the body has
   parsed is the control popping into a bar the reader is already
   looking at. It is small, same-origin and does no work beyond setting
   two attributes before DOMContentLoaded, so blocking on it costs one
   parse.

   The toggle is display: none until this file sets data-rst-js on
   <html>, which is the whole of the scriptless story: with scripts off
   the control is never shown, the page keeps color-scheme: light dark
   from the theme, and the reader's OS decides — which is exactly what
   the System position of the toggle means anyway. A visible control
   that cannot do anything would be the misleading version. */
(function () {
  "use strict";

  var root = document.documentElement;
  var KEY = "rst-ds-scheme";
  var SCHEMES = ["system", "light", "dark"];

  // Storage is wrapped both ways: a browser in private mode, or one
  // configured to refuse site data, throws on read AND on write rather
  // than returning null. A page whose colour toggle throws is a page
  // with a broken toggle, so both sides degrade to "this visit only".
  //
  // A list's first value is its default, which is stored as no key at
  // all.
  function load(key, values) {
    try {
      var v = localStorage.getItem(key);
      return values.indexOf(v) > 0 ? v : values[0];
    } catch (e) {
      return values[0];
    }
  }

  function save(key, value, values) {
    try {
      if (value === values[0]) localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch (e) {
      /* the choice still applies to this page; it just will not survive */
    }
  }

  function stored() {
    return load(KEY, SCHEMES);
  }

  // System removes the attribute rather than setting a third value:
  // the themes declare every colour once as light-dark() under
  // color-scheme: light dark, and their two toggle rules are
  // :root[data-theme="light"] and :root[data-theme="dark"]. No
  // attribute is the OS deciding, which is what System is.
  function apply(scheme, el) {
    el = el || root;
    if (scheme === "light" || scheme === "dark") el.setAttribute("data-theme", scheme);
    else el.removeAttribute("data-theme");
  }

  // Every example on this page is a document of its own inside an
  // iframe, and an iframe does not inherit the reader's choice: a
  // colour scheme is not propagated into an embedded document that
  // declares one, and every preview links a theme that does. The
  // frames are same-origin, so the fix is the attribute apply() has
  // just written, written again on each of them. Frames are lazy and
  // there are a hundred of them, hence the load handler as well.
  function frames(scheme) {
    var f = document.querySelectorAll(".ds-view__frame");
    for (var i = 0; i < f.length; i++) paint(f[i], scheme);
  }

  // Painting never shows a frame; shown() does, once its document is
  // parsed and styled. Painting showed about:blank and half-parsed
  // documents.
  function paint(frame, scheme) {
    try {
      apply(scheme, frame.contentDocument.documentElement);
    } catch (e) {
      /* not loaded yet, or not readable; its load handler will */
    }
  }

  function readable(frame) {
    try {
      return frame.contentDocument;
    } catch (e) {
      return null;
    }
  }

  // Every scheme button: every page has two sets, the bar's and the
  // phone's display settings menu's, and both stay in step.
  function pressed(scheme) {
    var buttons = document.querySelectorAll("[data-ds-scheme]");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute("aria-pressed", buttons[i].dataset.dsScheme === scheme ? "true" : "false");
    }
  }

  // A page back from the back/forward cache is reactivated, not re-run,
  // so none of this happens again. On a phone Back goes through
  // history, so without this a page left in Light comes back Light
  // after Dark was chosen on the page after it, its own toggle still
  // pressed on Light. Theme and language are addresses, not stored
  // choices, and need nothing.
  addEventListener("pageshow", function (event) {
    if (!event.persisted) return;
    apply(stored());
    pressed(stored());
    frames(stored());
  });

  // Phase one, at parse time: the remembered scheme and the marker the
  // stylesheet reveals the toggle with.
  root.setAttribute("data-rst-js", "on");
  apply(stored());

  // This file also runs in the shell demos, which the shells page
  // frames whole, and the page around a same-origin frame grants its
  // autofocus: the stage shell's sign-in field took the reader's focus
  // as its lazy frame loaded, and the gallery scrolled down to it.
  // A browser grants autofocus early in a rendering update, to the
  // first field that asked and can take focus, and runs animation
  // frame callbacks after (HTML's "update the rendering"). So in a
  // frame each field that asks is inert from the parser's insertion (a
  // microtask, before any update) to the first animation frame after
  // load, and its request is refused. Removing the attribute does not
  // work (the request is made on insertion), nor does hiding or
  // inerting the frame (Chromium focuses inside it anyway; measured).
  // Opened in a tab of its own, the demo keeps its autofocus.
  var host = null;
  try {
    host = window.frameElement;
  } catch (e) {
    /* framed by another origin, whose frames are not ours */
  }
  if (host && host.classList.contains("ds-view__frame")) {
    var asked = [];
    var hold = function (el) {
      if (el.hasAttribute("autofocus") && !el.inert) {
        el.inert = true;
        asked.push(el);
      }
    };
    var inserted = new MutationObserver(function (records) {
      for (var r = 0; r < records.length; r++) {
        var added = records[r].addedNodes;
        for (var n = 0; n < added.length; n++) {
          if (added[n].nodeType !== 1) continue;
          hold(added[n]);
          var inner = added[n].querySelectorAll("[autofocus]");
          for (var i = 0; i < inner.length; i++) hold(inner[i]);
        }
      }
    });
    inserted.observe(root, { childList: true, subtree: true });
    addEventListener("load", function () {
      inserted.disconnect();
      requestAnimationFrame(function () {
        for (var i = 0; i < asked.length; i++) asked[i].inert = false;
      });
    });
  }

  // Phase two, once the body exists: wire the buttons up. This file is
  // in <head> and not deferred, so readyState is always "loading" here
  // — the branch is for a copy of it moved to the foot of the page,
  // which is a reasonable thing to do and should not silently stop
  // working.
  function ready(fn) {
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", fn);
    else fn();
  }

  // Shown even if it could not be painted: a frame in the wrong scheme
  // beats one that never appears.
  function shown(frame) {
    paint(frame, stored());
    frame.setAttribute("data-ds-painted", "");
  }

  // DOMContentLoaded does not wait for stylesheets, and a frame shown
  // then shows its document without them. Each sheet not yet arrived
  // is waited for, loaded or failed; one that failed before this
  // looked never says so, and the frame's load shows it instead.
  function styled(frame, doc) {
    var links = doc.querySelectorAll('link[rel~="stylesheet"]'), left = 1;
    function one() {
      if (--left === 0 && readable(frame) === doc) shown(frame);
    }
    for (var i = 0; i < links.length; i++) {
      if (links[i].sheet) continue;
      left++;
      links[i].addEventListener("load", one);
      links[i].addEventListener("error", one);
    }
    one();
  }

  // Every document a frame loads goes through arm(), the first and each
  // one a sample's form or link loads in it after, and in this order:
  // the hook that hides the frame again as the document goes, then the
  // reveal. Shown first, a document that navigated before its own load
  // (the old hook waited for load) left the mark in place, and the
  // next one painted in the OS's scheme, unhidden. Not cleared when
  // the whole gallery page enters the back/forward cache (persisted),
  // which unloads nothing and restores as it was.
  //
  // Parsed and styled is soon enough to show it: waiting for load kept
  // a parsed preview an empty box while an image was slow, or for as
  // long as the network took to give up on one that hung.
  function arm(frame, armed) {
    var doc;
    try {
      doc = frame.contentDocument;
    } catch (e) {
      return false;
    }
    if (!doc || doc === armed.doc || doc.URL === "about:blank") return false;
    armed.doc = doc;
    doc.defaultView.addEventListener("pagehide", function (event) {
      if (event.persisted) return;
      frame.removeAttribute("data-ds-painted");
      seek(frame, armed);
    });
    if (doc.readyState === "loading") {
      doc.addEventListener("DOMContentLoaded", function () {
        if (readable(frame) === doc) styled(frame, doc);
      });
    } else {
      styled(frame, doc);
    }
    return true;
  }

  // The parent hears nothing when a navigation inside a frame commits
  // its next document, so after pagehide it looks until the frame holds
  // one, and arms it. The look ends there, or at the frame's load,
  // which arms whatever is there; a document from another origin cannot
  // be read, and load shows the frame unpainted.
  //
  // It also ends when the frame leaves the page, and after SEEK_MS
  // whatever happens. Without those, a frame removed mid-look, or sent
  // to another origin whose load hangs, kept a 16ms timer running for
  // the life of the page, holding the frame and its old document, and
  // the second kept the frame hidden for good. Giving up shows the
  // frame unpainted, as load would have.
  var SEEK_MS = 5000;

  function seek(frame, armed) {
    clearTimeout(armed.timer);
    var end = Date.now() + SEEK_MS;
    (function look() {
      armed.timer = 0;
      if (!frame.isConnected || arm(frame, armed)) return;
      if (Date.now() >= end) shown(frame);
      else armed.timer = setTimeout(look, 16);
    })();
  }

  // A frame's first document reuses the window of the about:blank it
  // replaces (same origin), so a listener on that window hears the
  // preview's DOMContentLoaded and arms it while it is still parsing;
  // the frames are lazy, so nothing looks for them before then. load
  // stays the fallback, for an engine that does not reuse the window.
  function watch(frame) {
    var armed = { doc: null, timer: 0 };
    try {
      frame.contentWindow.addEventListener("DOMContentLoaded", function () {
        arm(frame, armed);
      });
    } catch (e) {
      /* no window yet, or not readable; load will arm it */
    }
    frame.addEventListener("load", function () {
      clearTimeout(armed.timer);
      arm(frame, armed);
      shown(frame);
    });
    // A document already parsed before this ran (a frame served from
    // cache) has had its DOMContentLoaded.
    arm(frame, armed);
  }

  ready(function () {
    var previews = document.querySelectorAll(".ds-view__frame");
    for (var i = 0; i < previews.length; i++) watch(previews[i]);
    frames(stored());

    var buttons = document.querySelectorAll("[data-ds-scheme]");
    if (!buttons.length) return;

    pressed(stored());

    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function (event) {
        var scheme = event.currentTarget.dataset.dsScheme;
        if (SCHEMES.indexOf(scheme) < 0) return;
        apply(scheme);
        save(KEY, scheme, SCHEMES);
        pressed(scheme);
        frames(scheme);
      });
    }
  });

  // ── The nav filter ──────────────────────────────────────────────────
  //
  // The sidebar is a complete list of every anchor on the page before
  // this runs and stays one if it never does: the box is display:none
  // until data-rst-js is set, the same deal the toggle above has, so
  // scripts off gets the nav and no dead control.
  //
  // It says nothing of its own: the one sentence it can put on screen,
  // "nothing matches", is rendered by the page in the page's language
  // and starts out hidden, and this file only takes the attribute off
  // it. Same deal as select.js reading its strings out of the catalog.
  ready(function () {
    var input = document.querySelector("[data-ds-filter]");
    var nav = input && document.getElementById(input.getAttribute("aria-controls"));
    if (!input || !nav) return;
    var empty = document.querySelector("[data-ds-filter-empty]");
    var sections = nav.querySelectorAll("details");

    // Folded both sides: "seccion" finds "Sección", and the rail is
    // headings in twelve languages. NFD splits a letter from its
    // accents; the range is the combining marks, and dropping them is
    // the whole of it.
    function fold(s) {
      return s.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    }

    var links = nav.querySelectorAll("a");
    // An entry's synonyms (data-ds-terms) match beside its name, and
    // the plain page links filter like every entry, so No matches and
    // a visible Overview link never show together.
    for (var i = 0; i < links.length; i++) links[i].dsText = fold(links[i].textContent + " " + (links[i].getAttribute("data-ds-terms") || ""));
    var pages = nav.querySelectorAll(":scope > a");
    // A section's own name is searchable too: "shells" has to land
    // somewhere, and the reader typing it means the whole section.
    for (var i = 0; i < sections.length; i++) {
      var head = sections[i].querySelector("summary");
      sections[i].dsText = head ? fold(head.textContent) : "";
    }

    // What the reader had open before they typed. A query opens
    // whatever it found something in; clearing it hands the reader
    // their own arrangement back.
    var chosen = null;

    function run(query) {
      var q = fold(query.trim());
      if (q && chosen === null) {
        chosen = [];
        for (var i = 0; i < sections.length; i++) chosen.push(sections[i].open);
      }
      var found = false;
      for (var s = 0; s < sections.length; s++) {
        var section = sections[s];
        var kids = section.children;
        var shown = 0;
        // The section's own name matching stands for everything under
        // it: a reader who types "form" means the Form page, not the
        // one entry in it whose name happens to contain the word.
        var whole = !q || section.dsText.indexOf(q) >= 0;
        for (var k = 0; k < kids.length; k++) {
          var el = kids[k];
          if (el.tagName !== "A") continue;
          var hit = whole || el.dsText.indexOf(q) >= 0;
          el.hidden = !hit;
          if (hit) shown++;
        }
        section.hidden = shown === 0;
        if (shown) found = true;
        section.open = q ? shown > 0 : chosen ? chosen[s] : section.open;
      }
      for (var p = 0; p < pages.length; p++) {
        pages[p].hidden = q && pages[p].dsText.indexOf(q) < 0;
        if (q && !pages[p].hidden) found = true;
      }
      if (!q) chosen = null;
      if (empty) empty.hidden = !q || found;
    }

    input.addEventListener("input", function () {
      run(input.value);
    });

    // Escape clears a query that is there and is left alone when there
    // is not, so the key still belongs to whatever is around the box.
    // Focus never moves: nothing here touches it.
    input.addEventListener("keydown", function (event) {
      if (event.key !== "Escape" || input.value === "") return;
      event.preventDefault();
      input.value = "";
      run("");
    });

    // A back-navigation restores the box with a value already in it:
    // filter to what it says, not to what the page was rendered saying.
    if (input.value) run(input.value);
  });

  // ── Keeping your place ──────────────────────────────────────────────
  //
  // A theme or language link, in the pinned bar or in the phone's display
  // settings menu, carries the section being read, worked out at the
  // click; the address bar is never rewritten.
  //  1. A fragment this document put in place, while its target has not
  //     moved (2px): the case geometry cannot answer, the last partial of
  //     a page that clamps, or a glyph in a row of icons sharing a top.
  //  2. Else the anchor at the reading line (the scroll padding plus a
  //     pixel, where a fragment lands): the greatest top at or above it,
  //     first in document order.
  //  3. Above every anchor, nothing.
  // The rule-1 record is written only when this document scrolls to a
  // target: at load on a fresh, untouched navigation (redoing the parser's
  // scroll, which late layout may have moved), and a frame
  // after a plain, uncancelled click on a fragment link here, the only
  // signal when that fragment is already in the address. History drops
  // it, because Back restores where the reader had scrolled.
  var record = null, touched = false;

  function anchor(id) {
    var el = id && document.getElementById(id);
    return el && el.hasAttribute("data-ds-anchor") ? el : null;
  }

  function fragment(hash) {
    try {
      return decodeURIComponent(hash.slice(1));
    } catch (e) {
      return hash.slice(1);
    }
  }

  function keep(el) {
    record = { id: el.id, top: el.getBoundingClientRect().top };
  }

  ["wheel", "touchstart", "pointerdown", "keydown"].forEach(function (type) {
    addEventListener(type, function () { touched = true; }, { capture: true, passive: true });
  });

  addEventListener("load", function () {
    var nav = performance.getEntriesByType("navigation")[0], el = anchor(fragment(location.hash));
    if (!el || touched || !nav || nav.type !== "navigate") return;
    el.scrollIntoView({ block: "start" });
    requestAnimationFrame(function () { keep(el); });
  });

  document.addEventListener("click", function (event) {
    var a = event.target.closest && event.target.closest("a[href]"), el = a && anchor(fragment(a.hash));
    if (!el || event.button || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || a.target || a.hasAttribute("download") ||
        a.origin + a.pathname + a.search !== location.origin + location.pathname + location.search) return;
    requestAnimationFrame(function () {
      if (!event.defaultPrevented) keep(el);
    });
  });

  addEventListener("popstate", function () { record = null; });
  if (window.navigation) navigation.addEventListener("navigate", function (event) {
    if (event.navigationType === "traverse") record = null;
  });

  function place() {
    var el = record && fragment(location.hash) === record.id && anchor(record.id);
    if (el && Math.abs(el.getBoundingClientRect().top - record.top) <= 2) return el.id;
    var line = (parseFloat(getComputedStyle(root).scrollPaddingBlockStart) || 0) + 1;
    var all = document.querySelectorAll("[data-ds-anchor]"), best = "", top = -Infinity;
    for (var i = 0; i < all.length; i++) {
      var r = all[i].getBoundingClientRect();
      if ((r.width || r.height) && r.top <= line && r.top > top) {
        best = all[i].id;
        top = r.top;
      }
    }
    return best;
  }

  // A link's address is restored by a timer scheduled before it is
  // changed, so nothing failing between can leave it set. Not a
  // microtask: that checkpoint comes before activation reads href. Again
  // on pageshow, for a page back from the cache whose timer never ran.
  ready(function () {
    var links = document.querySelectorAll(".ds-top__controls a[href], .ds-prefs a[href]");
    if (!links.length) return;
    for (var i = 0; i < links.length; i++) links[i].dsHref = links[i].getAttribute("href");
    function canonical() {
      for (var i = 0; i < links.length; i++) links[i].setAttribute("href", links[i].dsHref);
    }
    addEventListener("pageshow", canonical);
    document.addEventListener("click", function (event) {
      var a = event.target.closest && event.target.closest("a[href]"), id;
      if (!a || a.dsHref === undefined) return;
      setTimeout(canonical, 0);
      if ((id = place())) a.setAttribute("href", a.dsHref + "#" + encodeURIComponent(id));
    });
  });

  // ── Copy ────────────────────────────────────────────────────────────
  //
  // A button on every source block, drawn only where the clipboard API
  // exists. On a plain-HTTP origin (a tailnet address) it does not, and
  // a button that cannot work is worse than a <pre> a reader can
  // select. The words are the page's, read off the live region it
  // renders in its own language; a button's name is built from text
  // already on screen, so the server writes nothing per block.
  ready(function () {
    var region = document.querySelector("[data-ds-copy-status]");
    if (!region || !navigator.clipboard || !navigator.clipboard.writeText) return;
    var d = region.dataset, pres = document.querySelectorAll("pre.ds-src:not([data-ds-nocopy])");

    // The name after the label: the tab the block is under (HTML or
    // Template), the section (its anchored heading, or on a page of
    // prose the last heading before it), and the nearest state label
    // above. A state shows the same label in both tabs, so without the
    // tab two buttons in one example would share a name.
    function name(pre) {
      var sec = pre.closest("[data-ds-anchor]"), h = sec && sec.querySelector("h1, h2, h3, h4");
      var all = document.querySelectorAll("main :is(h1, h2, h3, h4)"), sample = pre.closest(".ds-sample");
      if (!h) for (var i = 0; i < all.length && all[i].compareDocumentPosition(pre) & 4; i++) h = all[i];
      for (var s = pre.previousElementSibling; s && !s.matches(".ds-state"); s = s.previousElementSibling);
      s = s || sample && sample.querySelector(":scope > .ds-state");
      var panel = pre.closest(".ds-view__code"), tab = panel && panel.parentNode.querySelector(
        panel.classList.contains("ds-view__code--t") ? ".ds-view__tab--t" : ".ds-view__tab--h");
      return [tab, h, s].filter(Boolean).map(function (e) {
        return e.textContent.trim();
      }).join(d.join);
    }

    // Emptied and written a beat later, so Copied twice in a row is
    // still announced.
    function say(text) {
      region.textContent = "";
      setTimeout(function () { region.textContent = text; }, 50);
    }

    for (var i = 0; i < pres.length; i++) (function (pre) {
      var code = pre.querySelector("code") || pre, b = document.createElement("button");
      var label = document.createTextNode(d.copy), suffix = document.createElement("span"), timer;
      b.type = "button";
      b.className = "ds-copy";
      suffix.className = "rst-sr-only";
      suffix.textContent = " " + name(pre);
      b.append(label, suffix);
      pre.before(b);
      // Copied is said only once the write resolves. A refusal (a
      // permission, lost focus, a policy) or a throw is announced as a
      // failure and the block selected, so Ctrl or Cmd+C works at once.
      b.addEventListener("click", function () {
        new Promise(function (ok) { ok(navigator.clipboard.writeText(code.textContent)); }).then(function () {
          say(d.copied);
          label.data = d.copied;
          clearTimeout(timer);
          timer = setTimeout(function () { label.data = d.copy; }, 2000);
        }, function () {
          say(d.failed);
          getSelection().selectAllChildren(code);
        });
      });
    })(pres[i]);
  });
})();
