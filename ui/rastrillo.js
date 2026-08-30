/* rastrillo.js — the fragment shim. First-party, dependency-free, and
   almost entirely inert: the polling section answers only to an opt-in
   data attribute, and everything it enhances also works with scripts
   disabled (a status page's <noscript> meta refresh). This file is
   app-owned from the moment it is scaffolded — edit it like any other
   static file.

   Two sections are on by default instead, because neither has a
   per-instance decision to make. Light dismiss for the menu idioms keys
   off the rst-dropdown / rst-row-menu classes: a menu that closes on an
   outside click on one screen and not the next is worse than either rule
   applied everywhere. The busy rule keys off nothing at all — every
   submit button in every form that goes somewhere is covered, because
   a button that changes something should say so while it works, and a
   form that double-submits on a second click is a bug on every screen.
   Delete either section to opt the whole app out of it.

   Vocabulary:
     data-poll="URL"       fetch URL for an HTML fragment, replace this
                           element with it, and keep going only if the
                           replacement element itself carries data-poll
     data-poll-every="2"   seconds between polls (default 2)
     data-poll-push="URL"  optional, beside data-poll: open an
                           EventSource to URL and re-fetch the fragment
                           when the server says so, instead of on a
                           timer. Any EventSource error downgrades this
                           element to timer polling for good (once,
                           no flapping); a browser without EventSource
                           never leaves the timer path
     data-busy="false"     on a <form> or on one submit button: opt OUT
                           of the busy rule below. The rule is the
                           default, so any other value — data-busy on
                           its own included — changes nothing
     data-busy-label="…"   on the form or on the button: replacement
                           button text while it works

   select.js is a sibling file following exactly these rules, kept
   separate so this one stays small enough to read in one sitting. It
   answers data-rst-select on a <select>, which field-select emits past
   ten options.

   Every poll carries the request header Rastrillo-Fragment: 1, which
   marks the request as a shim poll so a handler can tell it apart from
   direct navigation. Reserved: the framework's own handlers do not
   read it today.

   Menus (no attribute — see above): an open <details class="rst-dropdown">,
   <details class="rst-row-menu"> or nested <details class="rst-menu-group">
   closes on a click outside it and on Escape. Which of them is open at
   once is still the native <details name> group, with no script involved
   at all.

   A polled response may answer 204 with a Rastrillo-Location header
   instead of a fragment; the shim navigates there, but only to a local
   path. 403 and 404 end the poll for good — the job was swept, or was
   never yours. Any other fetch error backs off (doubling to a 30s cap)
   and keeps trying — a network blip must not strand a status page. */
(function () {
  "use strict";

  // The same rule sessions.SafeReturn enforces server-side: a same-site
  // absolute path — starts with exactly one "/", no scheme, no
  // backslash, no control characters (browsers strip tab/CR/LF before
  // parsing, so "/\t/evil.example" would resolve scheme-relative).
  // Anything laxer is an open redirect, here driven by a response
  // header instead of a form field.
  function localPath(to) {
    return to.charAt(0) === "/" && to.charAt(1) !== "/" &&
      to.indexOf("\\") === -1 && !/[\u0000-\u001f\u007f]/.test(to);
  }

  function poll(el) {
    var base = (parseFloat(el.getAttribute("data-poll-every")) || 2) * 1000;
    var wait = base;
    var src = null; // open EventSource while pushing; null on the timer path
    var busy = false; // a fetch is in flight
    var queued = false; // an update landed mid-fetch; tick again after it
    function stop() {
      if (src) { src.close(); src = null; }
    }
    // One downgrade, never back: push hands this element to the timer.
    function fallback() {
      stop();
      schedule();
    }
    function tick() {
      if (busy) { queued = true; return; }
      busy = true;
      fetch(el.getAttribute("data-poll"), { headers: { "Rastrillo-Fragment": "1" } })
        .then(function (res) {
          // Terminal, not an error to retry: retrying a 404 forever is
          // noise, and following a signed-out redirect would swap a
          // sign-in page into the fragment's slot.
          if (res.status === 403 || res.status === 404) return null;
          if (!res.ok && res.status !== 204) throw new Error("status " + res.status);
          var to = res.headers.get("Rastrillo-Location");
          if (to) {
            if (localPath(to)) window.location.assign(to);
            return null; // navigating, or refusing to — either way, done
          }
          return res.text();
        })
        .then(function (html) {
          busy = false;
          if (html === null) { stop(); return; } // stopped
          wait = base; // a healthy response resets the backoff
          var tpl = document.createElement("template");
          tpl.innerHTML = html;
          var next = tpl.content.firstElementChild;
          if (!next) { stop(); return; } // fragment with no element: stop politely
          el.replaceWith(next);
          el = next;
          if (!el.hasAttribute("data-poll")) { stop(); return; }
          if (!src) { schedule(); return; }
          // Pushing: a fragment that dropped data-poll-push falls back
          // to the timer; otherwise catch up if an update was queued.
          if (!el.hasAttribute("data-poll-push")) { fallback(); return; }
          if (queued) { queued = false; tick(); }
        })
        .catch(function () {
          busy = false;
          wait = Math.min(wait * 2, 30000);
          if (!src) schedule(); // pushing: the next event retries instead
        });
    }
    function schedule() { setTimeout(tick, wait); }
    var push = el.getAttribute("data-poll-push");
    if (push && window.EventSource) {
      src = new EventSource(push);
      src.addEventListener("update", function () { tick(); });
      src.addEventListener("done", function () { stop(); tick(); });
      src.addEventListener("gone", stop);
      src.onerror = fallback;
      return;
    }
    schedule();
  }

  // ── The busy rule ────────────────────────────────────────────────
  //
  // A button that CHANGES something says so while it works; a button
  // that only reveals something — a disclosure, a dropdown, a tab —
  // does not. So every submit button in every form is covered by
  // default, with no attribute to remember: the submitted button gets
  // aria-busy, a spinner and — once the submission is under way —
  // disabled, and its form gets aria-busy and a guard against a second
  // submit. The data-busy vocabulary above opts out of either half.
  //
  // The guard is the substance, the spinner is the manners, and neither
  // is a promise: with scripts off the form submits exactly as it
  // always did, twice if the visitor clicks twice. Idempotency stays
  // the server's job. One delegated capture-phase listener, for the
  // reasons light dismiss gives below: fragments, and never double-bind.
  //
  // Two traps this shape exists to avoid:
  //
  //   - A form the browser REFUSED must not sit there looking busy.
  //     Constraint validation fails before the submit event is fired at
  //     all, so an invalid form never reaches this code; a cancelled
  //     one is handed back in the tick below. (formnovalidate skips
  //     validation and really does submit, which should look like it.)
  //   - Nothing that could change the payload happens while the payload
  //     is being read. The entry list is built before submit fires, but
  //     engines have differed, so aria-busy, the spinner and a
  //     <button>'s TEXT (never submitted — a button submits its value
  //     attribute) go on synchronously, while disabled and an
  //     <input type="submit">'s value (which IS what it submits) wait.
  //
  // Every busy element in the DOCUMENT, then the ownership test: a
  // form="id" submit button is owned by a form it does not live in.
  function busyOff(form) {
    form.removeAttribute("aria-busy");
    document.querySelectorAll('[aria-busy="true"]').forEach(function (b) {
      if (b.form !== form && !form.contains(b)) return;
      b.disabled = false;
      b.removeAttribute("aria-busy");
      var spin = b.querySelector(".rst-btn__spin");
      if (spin) spin.remove();
      var idle = b.getAttribute("data-idle-label");
      if (idle === null) return;
      if (b.tagName === "INPUT") { b.value = idle; } else { b.textContent = idle; }
      b.removeAttribute("data-idle-label");
    });
  }

  function busySubmit(e) {
    var form = e.target;
    if (!form || form.tagName !== "FORM") return;
    if (form.getAttribute("data-busy") === "false") return;
    // The button the browser submitted with: the one clicked, or — for
    // Enter in a field — the default one it implicitly clicked. Only
    // that one goes busy; every other submit button in the form keeps
    // its name and its value. An engine with no SubmitEvent.submitter
    // leaves the buttons alone and keeps the guard, which is the half
    // that matters.
    var btn = e.submitter;
    // A form that opens its result elsewhere, or a submit that closes a
    // dialog and stays put, has nothing to be busy about and nothing to
    // clear. The submitter's formmethod beats the form's.
    //
    // getAttribute, never the IDL properties. A form is
    // [LegacyOverrideBuiltIns]: a control named "target" or "method" (a
    // target date, a method column — ordinary field names) shadows the IDL
    // attribute with the input itself, so the property form quietly
    // switches the rule off and hands back the double submit. The busy flag
    // below is the form's own aria-busy for the same reason: an expando is
    // shadowable, and under strict mode assigning to a shadowed one throws.
    var to = form.getAttribute("target");
    if (to && to !== "_self") return;
    if (/^dialog$/i.test(btn && btn.getAttribute("formmethod") ||
      form.getAttribute("method"))) return;
    if (form.getAttribute("aria-busy") === "true") { e.preventDefault(); return; }
    form.setAttribute("aria-busy", "true");
    if (!btn || btn.getAttribute("data-busy") === "false") btn = null;
    var label = btn && (btn.getAttribute("data-busy-label") ||
      form.getAttribute("data-busy-label"));
    if (btn) {
      btn.setAttribute("aria-busy", "true");
      if (btn.tagName === "BUTTON") {
        if (label) {
          btn.setAttribute("data-idle-label", btn.textContent);
          btn.textContent = label;
        }
        // A child element, not a pseudo-element: the shim has to be
        // able to take it away again. An <input type="submit"> has no
        // children, which is the one shape this cannot draw a spinner
        // on. tokens.css stops it rotating under reduced motion.
        var spin = document.createElement("span");
        spin.className = "rst-spin rst-btn__spin";
        spin.setAttribute("aria-hidden", "true");
        btn.insertBefore(spin, btn.firstChild);
      }
    }
    setTimeout(function () {
      // Someone downstream cancelled the submit — an app handler doing
      // the work itself, most likely. Nothing is on its way anywhere,
      // so hand the form back, guard included, and let whoever took the
      // job own the feedback too.
      if (e.defaultPrevented) { busyOff(form); return; }
      if (!btn) return;
      if (label && btn.tagName === "INPUT") {
        btn.setAttribute("data-idle-label", btn.value);
        btn.value = label;
      }
      btn.disabled = true;
    }, 0);
  }

  document.addEventListener("submit", busySubmit, true);

  // Light dismiss — the one behaviour the native disclosure genuinely
  // cannot do, which is the shim's whole admission rule. A <details>
  // menu closes on a second click of its own summary and on nothing
  // else: not on a click elsewhere on the page, not on Escape. Both are
  // what every menu anywhere else does, and neither is expressible in
  // HTML or CSS.
  //
  // Two delegated listeners on the document, never one per element, so a
  // dropdown that arrives inside a polled fragment is covered the moment
  // it lands and re-scanning can never double-bind. Capture phase, so a
  // page whose own handler stops propagation cannot leave a menu stuck
  // open.
  //
  // The scriptless baseline is untouched: with this file removed, the
  // menus still toggle and the native <details name> group still keeps
  // one open at a time.
  //
  // The nested rst-menu-group is in MENUS even though it is never in the
  // <details name> group with its parent — the two mechanisms answer
  // different questions. The name group decides which menus may be open
  // at once, and a submenu must be exempt from it or opening one would
  // close the menu around it. Dismissal is not exclusivity: a submenu
  // left open behind its closing parent is still open the next time the
  // parent opens, which is a menu remembering a state the user has no
  // way to see. Listing it here also gives the natural behaviour of
  // clicking elsewhere INSIDE the parent closing the submenu — the
  // contains(except) test below already draws that line for free.
  //
  // Shell chrome and the toggle-block are deliberately absent from
  // MENUS. A sidebar's disclosure strip and a settings switch are not
  // menus; closing them because a click landed elsewhere would fight the
  // user rather than help.
  var MENUS = "details.rst-dropdown[open], details.rst-menu-group[open], " +
    "details.rst-row-menu[open]";

  // except is the clicked node: the menu containing it stays open, which
  // is what keeps a click on a menu item — or on the summary of a menu
  // being opened right now — from closing the thing being used.
  function closeMenus(except) {
    document.querySelectorAll(MENUS).forEach(function (d) {
      if (!except || !d.contains(except)) d.open = false;
    });
  }

  function dismissMenus(e) {
    if (e.type === "click") { closeMenus(e.target); return; }
    if (e.key !== "Escape") return;
    // Focus is about to be inside a subtree that is no longer rendered,
    // which strands a keyboard user at the top of the document. Hand it
    // back to the summary that opened the menu.
    var el = document.activeElement;
    var host = el && el.closest ? el.closest(MENUS) : null;
    // Climb to the OUTERMOST open menu around the focus. closest() finds
    // the innermost, which for focus inside a submenu is the submenu —
    // and its summary is inside the parent that is about to close too, so
    // focusing it would hand focus to something no longer rendered, which
    // is no hand-back at all.
    while (host && host.parentElement && host.parentElement.closest(MENUS)) {
      host = host.parentElement.closest(MENUS);
    }
    closeMenus(null);
    if (host) {
      var summary = host.querySelector("summary");
      if (summary) summary.focus();
    }
  }

  document.addEventListener("click", dismissMenus, true);
  document.addEventListener("keydown", dismissMenus, true);

  function scan() {
    document.querySelectorAll("[data-poll]").forEach(poll);
  }

  // The back/forward cache restores a page's DOM exactly as it was left
  // — busy buttons still disabled, still wearing the busy label and the
  // spinner — so a visitor who navigates back finds a dead form. Hand
  // every busy form back.
  window.addEventListener("pageshow", function (e) {
    if (e.persisted) document.querySelectorAll("form[aria-busy]").forEach(busyOff);
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
