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
  function apply(scheme) {
    if (scheme === "light" || scheme === "dark") root.setAttribute("data-theme", scheme);
    else root.removeAttribute("data-theme");
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
    var buttons = document.querySelectorAll("[data-ds-scheme]");
    if (!buttons.length) return;

    function paint(scheme) {
      for (var i = 0; i < buttons.length; i++) {
        buttons[i].setAttribute("aria-pressed", buttons[i].dataset.dsScheme === scheme ? "true" : "false");
      }
    }

    paint(stored());

    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function (event) {
        var scheme = event.currentTarget.dataset.dsScheme;
        if (SCHEMES.indexOf(scheme) < 0) return;
        apply(scheme);
        remember(scheme);
        paint(scheme);
      });
    }
  });

  // ── Seam: the nav filter ────────────────────────────────────────────
  // Task 5 adds the gallery's section nav and the type-to-filter box
  // over it. It belongs here, below this line, on the same terms as the
  // toggle above: it enhances a nav that is a complete list of links
  // with scripts off, and it adds no strings of its own — every word it
  // needs rides out on a data attribute the renderer fills from
  // prose.go, the way select.js takes its strings from the catalog.
})();
