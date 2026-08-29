/* rastrillo.js — the fragment shim. First-party, dependency-free, and
   almost entirely inert: everything apart from the menu section answers
   only to an opt-in data attribute, and everything it enhances also works
   with scripts disabled (a status page's <noscript> meta refresh). This
   file is app-owned from the moment it is scaffolded — edit it like any
   other static file.

   The one exception is light dismiss for the menu idioms, which keys off
   the rst-dropdown / rst-row-menu classes rather than a data attribute:
   there is no per-instance decision to make, and a menu that closes on
   an outside click on one screen and not the next is worse than either
   rule applied everywhere. Delete the section to opt the whole app out.

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
     data-busy             on a <form>: on the way out, disable submit
                           buttons and set aria-busy="true"
     data-busy-label="…"   optional button text while busy

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

  var SUBMITS = 'button[type="submit"], button:not([type]), input[type="submit"]';

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

  function busyForm(form) {
    form.addEventListener("submit", function () {
      // Deferred a tick: disabling a submit button during the submit
      // event would drop its name/value from the submitted form data.
      setTimeout(function () {
        form.setAttribute("aria-busy", "true");
        var buttons = form.querySelectorAll(SUBMITS);
        buttons.forEach(function (b) {
          b.disabled = true;
          var label = form.getAttribute("data-busy-label");
          if (label) {
            if (b.tagName === "INPUT") {
              b.setAttribute("data-idle-label", b.value);
              b.value = label;
            } else {
              b.setAttribute("data-idle-label", b.textContent);
              b.textContent = label;
            }
          }
        });
      }, 0);
    });
  }

  // The back/forward cache restores a page's DOM exactly as it was left
  // — busy buttons still disabled, still wearing the busy label — so a
  // visitor who navigates back finds a dead form. Re-enable the buttons,
  // put their idle labels back, and clear the busy flag.
  function unbusy() {
    document.querySelectorAll("form[data-busy]").forEach(function (form) {
      form.removeAttribute("aria-busy");
      form.querySelectorAll(SUBMITS).forEach(function (b) {
        b.disabled = false;
        var idle = b.getAttribute("data-idle-label");
        if (idle !== null) {
          if (b.tagName === "INPUT") { b.value = idle; } else { b.textContent = idle; }
          b.removeAttribute("data-idle-label");
        }
      });
    });
  }

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
    document.querySelectorAll("form[data-busy]").forEach(busyForm);
  }

  window.addEventListener("pageshow", function (e) {
    if (e.persisted) unbusy();
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
