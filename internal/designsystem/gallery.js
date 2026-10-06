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
   - the page-wide view, remembered as rst-ds-view;
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
  // The scheme and the page-wide view share them; a list's first
  // value is its default, which is stored as no key at all.
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

  function paint(frame, scheme) {
    try {
      apply(scheme, frame.contentDocument.documentElement);
    } catch (e) {
      /* not loaded yet, or not readable; its load handler will */
    }
  }

  // Every scheme button: the Overview has two sets, the bar's and the
  // phone index's, and both stay in step.
  function pressed(scheme) {
    var buttons = document.querySelectorAll("[data-ds-scheme]");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute("aria-pressed", buttons[i].dataset.dsScheme === scheme ? "true" : "false");
    }
  }

  // A page back from the back/forward cache is reactivated, not re-run,
  // so none of this happens again. On a phone the toggle is on the index
  // only and Back goes through history, so without this a page left in
  // Light comes back Light after Dark was chosen on the index, with no
  // control on it to fix that. Theme and language are addresses, not
  // stored choices, and need nothing.
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

  // Phase two, once the body exists: wire the buttons up. This file is
  // in <head> and not deferred, so readyState is always "loading" here
  // — the branch is for a copy of it moved to the foot of the page,
  // which is a reasonable thing to do and should not silently stop
  // working.
  function ready(fn) {
    if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", fn);
    else fn();
  }

  ready(function () {
    var previews = document.querySelectorAll(".ds-view__frame");
    for (var i = 0; i < previews.length; i++) {
      previews[i].addEventListener("load", function (event) {
        paint(event.currentTarget, stored());
      });
    }
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
  // A theme or language link in the pinned bar carries the section being
  // read, worked out at the click; the address bar is never rewritten.
  //  1. A fragment this document put in place, while its target has not
  //     moved (2px): the case geometry cannot answer, the last partial of
  //     a page that clamps, or a glyph in a row of icons sharing a top.
  //  2. Else the anchor at the reading line (the scroll padding plus a
  //     pixel, where a fragment lands): the greatest top at or above it,
  //     first in document order.
  //  3. Above every anchor, nothing.
  // The rule-1 record is written only when this document scrolls to a
  // target: at load on a fresh, untouched navigation (redoing the parser's
  // scroll, which the stored view's layout has since moved), and a frame
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
    var bar = document.querySelector(".ds-top");
    if (!bar) return;
    var links = bar.querySelectorAll(".ds-top__controls a[href]");
    for (var i = 0; i < links.length; i++) links[i].dsHref = links[i].getAttribute("href");
    function canonical() {
      for (var i = 0; i < links.length; i++) links[i].setAttribute("href", links[i].dsHref);
    }
    addEventListener("pageshow", canonical);
    bar.addEventListener("click", function (event) {
      var a = event.target.closest("a[href]"), id;
      if (!a || a.dsHref === undefined) return;
      setTimeout(canonical, 0);
      if ((id = place())) a.setAttribute("href", a.dsHref + "#" + encodeURIComponent(id));
    });
  });

  // ── One view for the whole page ─────────────────────────────────────
  //
  // Buttons, not radios, so pressing the pressed one re-applies it after
  // a reader changed one widget by hand. Pressed is read off the radios,
  // never off what a width shows: Auto when nothing is checked; a view
  // when every widget with that tab has it checked and every other has
  // nothing checked; else none. .checked fires no change event and needs
  // none, since the panels follow :has(:checked). Applied at
  // DOMContentLoaded, before load puts a fragment in place against it.
  var VIEWS = ["auto", "desktop", "mobile", "code"];
  ready(function () {
    var group = document.querySelector(".ds-viewall");
    if (!group) return;
    var widgets = document.querySelectorAll(".ds-view"), buttons = group.querySelectorAll("button");
    // .ds-view__tab--d, --m or --c. Auto has none, nor does a view a
    // widget lacks (a framed page has no Code): it is left on Auto.
    function radio(w, view) {
      var tab = w.querySelector(".ds-view__tab--" + view.charAt(0));
      return tab && tab.querySelector("input");
    }
    function choose(view) {
      for (var i = 0; i < widgets.length; i++) {
        var want = radio(widgets[i], view), inputs = widgets[i].querySelectorAll(".ds-view__tab input");
        for (var j = 0; j < inputs.length; j++) inputs[j].checked = inputs[j] === want;
      }
    }
    function show() {
      var on = "";
      for (var v = 0; v < VIEWS.length && !on; v++) {
        for (var i = 0, all = true; i < widgets.length && all; i++) {
          var r = v && radio(widgets[i], VIEWS[v]);
          all = r ? r.checked : !widgets[i].querySelector(".ds-view__tab input:checked");
        }
        if (all) on = VIEWS[v];
      }
      for (var k = 0; k < buttons.length; k++) buttons[k].setAttribute("aria-pressed", String(buttons[k].dataset.dsView === on));
    }
    choose(load("rst-ds-view", VIEWS));
    show();
    group.addEventListener("click", function (event) {
      var b = event.target.closest("button");
      if (!b) return;
      choose(b.dataset.dsView);
      save("rst-ds-view", b.dataset.dsView, VIEWS);
      show();
    });
    document.addEventListener("change", show);
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

    // The name after the label: the disclosure's summary when the block
    // is in one, the section (its anchored heading, or on a page of
    // prose the last heading before it), and the nearest state label
    // above. Labels are unique within a section, so names are unique
    // wherever the labels are.
    function name(pre) {
      var sec = pre.closest("[data-ds-anchor]"), h = sec && sec.querySelector("h1, h2, h3, h4");
      var all = document.querySelectorAll("main :is(h1, h2, h3, h4)"), sample = pre.closest(".ds-sample");
      if (!h) for (var i = 0; i < all.length && all[i].compareDocumentPosition(pre) & 4; i++) h = all[i];
      for (var s = pre.previousElementSibling; s && !s.matches(".ds-state"); s = s.previousElementSibling);
      s = s || sample && sample.querySelector(":scope > .ds-state");
      var html = pre.closest("details.ds-html");
      return [html && html.querySelector("summary"), h, s].filter(Boolean).map(function (e) {
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
