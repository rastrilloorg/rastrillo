/* anchor.js — keeps a dropdown's menu on the screen in a browser
   without CSS anchor positioning. First-party, dependency-free, and
   app-owned from the moment it is scaffolded.

   A dropdown hugs its summary, and its menu opens toward the leading
   side. Where the browser has anchor positioning, tokens.css flips the
   menu when there is no room there, and this file does nothing at all:
   it tests for the feature first and returns. Where it does not, the
   menu hangs from the <details>' own box, and nothing in a stylesheet
   knows where that box is on the screen, so a dropdown at the leading
   edge would open its menu off it. Here the menu is measured as it
   opens, and when it would leave the screen and fits better from the
   summary's other edge, the <details> is marked .is-flip-inline, which
   tokens.css opens from that edge, and measured again while it is open
   if the window is resized or turned. Only inline: a menu with no room
   below still opens below.

   Delete this file and its <script> tag once every browser your app
   supports has anchor positioning; nothing else changes. Without it, a
   browser that lacks the feature keeps each dropdown as wide as its
   container, so the menu opens on the screen but at the container's
   far edge rather than under its summary. */
(function () {
  "use strict";

  if (window.CSS && CSS.supports && CSS.supports("position-area: block-end") && CSS.supports("position-try-fallbacks: flip-block")) return;

  // Told to tokens.css: a dropdown may hug its summary now, because
  // something here will flip its menu. Without this file it keeps its
  // container's width, which keeps the menu on the screen unflipped.
  document.documentElement.setAttribute("data-rst-flip-menus", "");

  // Absolute is the stylesheet's path for a browser without anchor
  // positioning; a menu nested in another menu is static, and is left
  // alone. toggle does not bubble; capture still sees it.
  var DROPDOWN = "[rst-dropdown],.rst-dropdown";
  var PANEL = ":scope > [rst-dropdown-menu], :scope > .rst-dropdown__menu";
  var open = [];
  function overflow(r, w) {
    return Math.max(0, -r.left) + Math.max(0, r.right - w);
  }
  function place(d) {
    var p = d.querySelector(PANEL), w, before;
    if (!p || getComputedStyle(p).position !== "absolute") return;
    d.classList.remove("is-flip-inline");
    w = document.documentElement.clientWidth;
    before = overflow(p.getBoundingClientRect(), w);
    if (!before) return;
    d.classList.add("is-flip-inline");
    if (overflow(p.getBoundingClientRect(), w) >= before) d.classList.remove("is-flip-inline");
  }
  // A window resized or turned with a menu open can move its summary,
  // so every open menu is placed again. The listener is there only
  // while one is open.
  function replace() {
    open = open.filter(function (d) { return d.open && d.isConnected; });
    open.forEach(place);
    if (!open.length) removeEventListener("resize", replace);
  }
  document.addEventListener("toggle", function (e) {
    var d = e.target;
    if (!d.matches || !d.matches(DROPDOWN)) return;
    if (!d.open) return replace();
    place(d);
    if (open.indexOf(d) < 0) open.push(d);
    addEventListener("resize", replace, { passive: true });
  }, true);
})();
