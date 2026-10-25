/* light.js: the light. First-party, dependency-free, and deletable:
   remove this file and its script tag and the page is the still depth
   design, because every property here has a default the stylesheet
   already carries.

   What it does: lights the page from where the sun is at this hour
   (from the left in the morning, overhead at noon, from the right in
   the evening); puts the pointer's position on the button under it, so
   the button catches a sheen there; puts a press's position on the
   button, for a mouse and for a touch, so the press darkens most where
   it landed; and lets a card breathe: its rim brightens near the
   pointer over about a second and fades when the pointer leaves. The
   whole page's light can follow the pointer instead: set FOLLOW below
   to true. It is off because every write of it restyles the page.

   What it writes: --rst-light-x and --rst-light-y on the root, from -1
   to 1; --rst-mx, --rst-my, --rst-px and --rst-py on a button, in the
   button's own pixels; --rst-card-x, --rst-card-y and --rst-card-lit
   on a card. Nothing else. It never writes on a switch, so a drag
   rastrillo.js owns (data-rst-drag) never meets it.

   What it never does: capture the pointer, read a sensor (no ambient
   light, no tilt: both need a permission or a flag), or run under
   prefers-reduced-motion or forced colours, where it adds no listener
   and clears what it wrote if the setting turns on mid-session. One
   animation frame loop, running only while something is still easing;
   the hour is read again every five minutes; the loop pauses while the
   page is hidden.

   For a test: dispatch a CustomEvent "rst:light" on the document with
   detail {x, y, hold}; hold keeps the light there against pointer
   moves. */
(function () {
  "use strict";
  var root = document.documentElement;
  // Without matchMedia, or with one that cannot report a change (Safari
  // before 14), the reduced-motion and forced-colours checks cannot be
  // made or kept, so the page keeps the still design rather than a
  // light that ignores them.
  if (!window.matchMedia || !window.matchMedia("all").addEventListener) return;
  var reduce = matchMedia("(prefers-reduced-motion: reduce)");
  var forced = matchMedia("(forced-colors: active)");

  // FOLLOW is C9's fallback: false drops the pointer-following root
  // light for the hour-only light and keeps the per-element sheen. It
  // is false because every root write restyles and repaints the whole
  // document, about 12ms over 500 rows, and even at one write per
  // WRITE_MS that put the CI runner at 4.15 to 4.20 ms a frame against
  // a 4ms budget; hour-only measured 2.07 ms. The eased, rate-limited
  // path below stays, so turning it back on is this one line
  // (TestTheLightStaysWithinItsFrameBudget measures it).
  var FOLLOW = false;
  var WRITE_MS = 200;
  var BTN = "[rst-btn],.rst-btn";
  var CARD = "[rst-box],.rst-box,[rst-card],.rst-card,[rst-stats],.rst-stats,[rst-list],.rst-list";
  // Time constants in ms: the light settles in about a third of a
  // second, a card's spot follows at a walk, its brightness over about
  // a second. Elapsed time, not a per-frame step, so 120 Hz is no faster.
  var LIGHT_T = 125, CARD_T = 320, LIT_T = 400;

  var on = false, tx = 0, ty = -1, lx = 0, ly = -1, wx = 0, wy = -1;
  var raf = 0, last = 0, pending = null, pointer = false, hold = false, force = false, wrote = 0;
  var cards = new WeakMap(), live = new Set(), timers = new Set(), clock = 0;

  function sun() {
    var d = new Date(), h = d.getHours() + d.getMinutes() / 60;
    var t = Math.max(-1, Math.min(1, (h - 12) / 6));
    return [t * 0.8, -1 + Math.abs(t) * 0.35];
  }
  function target() {
    if (hold || (pointer && FOLLOW)) return;
    var s = sun();
    tx = s[0];
    ty = s[1];
  }
  // Every path that wants a frame comes through here, so a timer or a
  // move that fires after stop() or while the page is hidden schedules
  // nothing.
  function go() {
    if (!on || document.hidden) return;
    if (!raf) raf = requestAnimationFrame(step);
  }
  function ease(v, to, dt, T) {
    return v + (to - v) * (1 - Math.exp(-dt / T));
  }
  function light(el, x, y) {
    var o = cards.get(el);
    if (!o) { o = { x: x, y: y, l: 0 }; cards.set(el, o); }
    o.tx = x; o.ty = y; o.tl = 1;
    live.add(el);
  }
  function dim(el) {
    var o = cards.get(el);
    if (o) { o.tl = 0; live.add(el); }
  }

  // One loop. Every read (the pending move's rectangles) happens before
  // any write, and it reschedules itself only while something is still
  // easing.
  function step(now) {
    raf = 0;
    var dt = last ? Math.min(100, now - last) : 16;
    last = now;
    var moving = false;
    if (pending) {
      var e = pending;
      pending = null;
      var b = e.target.closest && e.target.closest(BTN);
      var c = e.target.closest && e.target.closest(CARD);
      var br = b && b.getBoundingClientRect();
      var cr = c && c.getBoundingClientRect();
      if (b) {
        b.style.setProperty("--rst-mx", (e.clientX - br.left).toFixed(0) + "px");
        b.style.setProperty("--rst-my", (e.clientY - br.top).toFixed(0) + "px");
      }
      live.forEach(function (k) { if (k !== c) dim(k); });
      if (c) light(c, e.clientX - cr.left, e.clientY - cr.top);
    }
    if (!hold) {
      lx = ease(lx, tx, dt, LIGHT_T);
      ly = ease(ly, ty, dt, LIGHT_T);
    }
    if (force || ((Math.abs(lx - wx) > 0.01 || Math.abs(ly - wy) > 0.01) && now - wrote >= WRITE_MS)) {
      force = false;
      wx = lx; wy = ly; wrote = now;
      root.style.setProperty("--rst-light-x", lx.toFixed(3));
      root.style.setProperty("--rst-light-y", ly.toFixed(3));
    }
    if (Math.abs(tx - lx) > 0.002 || Math.abs(ty - ly) > 0.002) moving = true;
    live.forEach(function (el) {
      if (!el.isConnected) { live.delete(el); return; }
      var o = cards.get(el);
      o.x = ease(o.x, o.tx, dt, CARD_T);
      o.y = ease(o.y, o.ty, dt, CARD_T);
      o.l = ease(o.l, o.tl, dt, LIT_T);
      var done = Math.abs(o.tx - o.x) < 0.5 && Math.abs(o.ty - o.y) < 0.5 && Math.abs(o.tl - o.l) < 0.004;
      if (done) { o.x = o.tx; o.y = o.ty; o.l = o.tl; }
      // A lit card that has settled is written once, not on every frame
      // the root light moves: a card is a large paint.
      if (!done || !o.still) {
        el.style.setProperty("--rst-card-x", o.x.toFixed(1) + "px");
        el.style.setProperty("--rst-card-y", o.y.toFixed(1) + "px");
        el.style.setProperty("--rst-card-lit", o.l.toFixed(3));
      }
      o.still = done;
      if (!done) moving = true; else if (o.tl === 0) live.delete(el);
    });
    if (moving) go(); else last = 0;
  }

  function onMove(e) {
    if (e.pointerType !== "mouse") return;
    if (FOLLOW && !hold) {
      pointer = true;
      // The light sits at the pointer, a little above the page.
      tx = Math.max(-1, Math.min(1, (e.clientX / innerWidth - 0.5) * 2));
      ty = Math.max(-1, Math.min(1, (e.clientY / innerHeight - 0.5) * 2)) * 0.6 - 0.4;
    }
    pending = e;
    go();
  }
  // The press darkens most where it landed, for mouse and touch alike;
  // a touch leaves pointer mode, and a tapped card catches the light
  // briefly. Both rectangles are read before either element is written.
  function onDown(e) {
    var b = e.target.closest && e.target.closest(BTN);
    var c = e.pointerType !== "mouse" && e.target.closest && e.target.closest(CARD);
    var br = b && b.getBoundingClientRect();
    var cr = c && c.getBoundingClientRect();
    if (b) {
      b.style.setProperty("--rst-px", (e.clientX - br.left).toFixed(0) + "px");
      b.style.setProperty("--rst-py", (e.clientY - br.top).toFixed(0) + "px");
    }
    if (e.pointerType === "mouse") return;
    leavePointer();
    if (c) {
      light(c, e.clientX - cr.left, e.clientY - cr.top);
      var id = setTimeout(function () { timers.delete(id); dim(c); go(); }, 1400);
      timers.add(id);
      go();
    }
  }
  function leavePointer() {
    pointer = false;
    target();
    go();
  }
  // The queued move goes first: a move over a card in the same task as
  // the leave would be read by the next frame and light the card the
  // pointer has just left, and it would settle lit with no pointer on
  // the page (Astra A6).
  function onLeave() {
    pending = null;
    live.forEach(dim);
    leavePointer();
  }
  function onVisibility() {
    if (document.hidden) {
      if (raf) cancelAnimationFrame(raf);
      raf = 0; last = 0;
    } else {
      go();
    }
  }
  // The test hook (C14): sets the light at once and, with hold, keeps
  // it there against pointer moves until the next event without hold.
  function onHook(e) {
    var d = e.detail || {};
    hold = !!d.hold;
    if (typeof d.x === "number") { tx = lx = d.x; }
    if (typeof d.y === "number") { ty = ly = d.y; }
    force = true;
    go();
  }

  function start() {
    if (on) return;
    on = true;
    addEventListener("pointermove", onMove, { passive: true });
    addEventListener("pointerdown", onDown, { passive: true });
    addEventListener("keydown", leavePointer, { passive: true });
    document.addEventListener("pointerleave", onLeave);
    document.addEventListener("visibilitychange", onVisibility);
    document.addEventListener("rst:light", onHook);
    clock = setInterval(function () { target(); go(); }, 5 * 60 * 1000);
    // Set, not eased, at start: easing in from straight above would be
    // a root write a frame for a third of a second while the page
    // loads, toward a position nobody saw it leave.
    target();
    lx = tx; ly = ty; force = true;
    go();
  }
  // Reduced motion is total, and forced colours drop every background
  // and shadow this file moves: under either, no listener, no frame, no
  // timer, and every property this file wrote goes back to its default.
  function stop() {
    if (!on) return;
    on = false;
    removeEventListener("pointermove", onMove);
    removeEventListener("pointerdown", onDown);
    removeEventListener("keydown", leavePointer);
    document.removeEventListener("pointerleave", onLeave);
    document.removeEventListener("visibilitychange", onVisibility);
    document.removeEventListener("rst:light", onHook);
    clearInterval(clock);
    timers.forEach(clearTimeout);
    timers.clear();
    if (raf) cancelAnimationFrame(raf);
    raf = 0; last = 0; pending = null; pointer = false; hold = false; force = false; wrote = 0;
    lx = wx = tx = 0; ly = wy = ty = -1;
    live.clear();
    // The cards' state goes with their properties: a card that had
    // settled lit kept l = 1 and still = true here, so after a
    // preference toggle the pointer returning to it found nothing to
    // ease and wrote nothing, and the card stayed dark (Astra A7).
    cards = new WeakMap();
    root.style.removeProperty("--rst-light-x");
    root.style.removeProperty("--rst-light-y");
    // Every element this file ever wrote on carries one of its
    // properties in its style attribute; no list of them is kept, so a
    // button or card replaced by a fragment is not retained here.
    var written = document.querySelectorAll('[style*="--rst-"]');
    for (var i = 0; i < written.length; i++) {
      ["--rst-mx", "--rst-my", "--rst-px", "--rst-py", "--rst-card-x", "--rst-card-y", "--rst-card-lit"].forEach(function (p) {
        written[i].style.removeProperty(p);
      });
    }
  }
  function settle() {
    if (reduce.matches || forced.matches) stop(); else start();
  }
  reduce.addEventListener("change", settle);
  forced.addEventListener("change", settle);
  settle();
})();
