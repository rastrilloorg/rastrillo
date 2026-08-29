/* gallery.js — the design-system gallery's own script, and the only one
   in the tree that is not part of the framework. rastrillo.js, select.js
   and datetime.js are shipped to apps; this file is furniture for the
   page that shows them off, so it lives beside the renderer rather than
   in ui/ and no scaffold ever writes it.

   Same rules as its three neighbours all the same: first-party,
   dependency-free, no network, and inert-safe — the page it enhances is
   a complete document with scripts off. What it adds:

     the colour-scheme toggle   System / Light / Dark, written to
                                data-theme on <html> and remembered in
                                localStorage under rst-ds-scheme
     the sidebar filter         type to hide the nav entries that do
                                not match, and the sections left empty
     the preview frames         the chosen scheme again, on each of the
                                iframes the examples are drawn in

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
  function stored() {
    try {
      var v = localStorage.getItem(KEY);
      return SCHEMES.indexOf(v) > 0 ? v : "system";
    } catch (e) {
      return "system";
    }
  }

  function remember(scheme) {
    try {
      if (scheme === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, scheme);
    } catch (e) {
      /* the choice still applies to this page; it just will not survive */
    }
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

    function pressed(scheme) {
      for (var i = 0; i < buttons.length; i++) {
        buttons[i].setAttribute("aria-pressed", buttons[i].dataset.dsScheme === scheme ? "true" : "false");
      }
    }

    pressed(stored());

    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function (event) {
        var scheme = event.currentTarget.dataset.dsScheme;
        if (SCHEMES.indexOf(scheme) < 0) return;
        apply(scheme);
        remember(scheme);
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
    for (var i = 0; i < links.length; i++) links[i].dsText = fold(links[i].textContent);
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
        var shown = 0, group = null, inGroup = 0;
        // The section's own name matching stands for everything under
        // it, the same way a family heading's does for its run.
        var whole = !q || section.dsText.indexOf(q) >= 0;
        for (var k = 0; k < kids.length; k++) {
          var el = kids[k];
          if (el.tagName !== "A") continue;
          // A family heading is a link like any other, but it stands
          // for the run under it: matching the family shows the whole
          // family, and matching nothing in it takes the heading with
          // it rather than leaving a label over a gap.
          if (el.classList.contains("ds-nav__group")) {
            if (group) group.hidden = inGroup === 0;
            group = el;
            group.dsWhole = whole || el.dsText.indexOf(q) >= 0;
            inGroup = group.dsWhole ? 1 : 0;
            shown += inGroup;
            continue;
          }
          var hit = whole || (group && group.dsWhole) || el.dsText.indexOf(q) >= 0;
          el.hidden = !hit;
          if (hit) {
            shown++;
            inGroup++;
          }
        }
        if (group) group.hidden = inGroup === 0;
        section.hidden = shown === 0;
        if (shown) found = true;
        section.open = q ? shown > 0 : chosen ? chosen[s] : section.open;
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
})();
