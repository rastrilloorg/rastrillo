/* rastrillo.js — the fragment shim. First-party, dependency-free, and
   inert by default: only elements that opt in with a data attribute
   get behavior, and everything it enhances also works with scripts
   disabled (a status page's <noscript> meta refresh). This file is
   app-owned from the moment it is scaffolded — edit it like any other
   static file.

   Vocabulary:
     data-poll="URL"       fetch URL for an HTML fragment, replace this
                           element with it, repeat while the new
                           fragment still carries data-poll
     data-poll-every="2"   seconds between polls (default 2)
     data-busy             on a <form>: on the way out, disable submit
                           buttons and set aria-busy="true"
     data-busy-label="…"   optional button text while busy

   A polled response may answer 204 with a Rastrillo-Location header
   instead of a fragment; the shim navigates there. Fetch errors back
   off (doubling to a 30s cap) and keep trying — a network blip must
   not strand a status page. */
(function () {
  "use strict";

  function poll(el) {
    var base = (parseFloat(el.getAttribute("data-poll-every")) || 2) * 1000;
    var wait = base;
    function tick() {
      fetch(el.getAttribute("data-poll"), { headers: { "Rastrillo-Fragment": "1" } })
        .then(function (res) {
          if (!res.ok && res.status !== 204) throw new Error("status " + res.status);
          var to = res.headers.get("Rastrillo-Location");
          if (to) { window.location.assign(to); return null; }
          return res.text();
        })
        .then(function (html) {
          if (html === null) return; // navigating
          wait = base; // a healthy response resets the backoff
          var tpl = document.createElement("template");
          tpl.innerHTML = html;
          var next = tpl.content.firstElementChild;
          if (!next) return; // fragment with no element: stop politely
          el.replaceWith(next);
          el = next;
          if (el.hasAttribute("data-poll")) schedule();
        })
        .catch(function () {
          wait = Math.min(wait * 2, 30000);
          schedule();
        });
    }
    function schedule() { setTimeout(tick, wait); }
    schedule();
  }

  function busy(form) {
    form.addEventListener("submit", function () {
      // Deferred a tick: disabling a submit button during the submit
      // event would drop its name/value from the submitted form data.
      setTimeout(function () {
        form.setAttribute("aria-busy", "true");
        var buttons = form.querySelectorAll(
          'button[type="submit"], button:not([type]), input[type="submit"]'
        );
        buttons.forEach(function (b) {
          b.disabled = true;
          var label = form.getAttribute("data-busy-label");
          if (label) {
            if (b.tagName === "INPUT") { b.value = label; } else { b.textContent = label; }
          }
        });
      }, 0);
    });
  }

  function scan() {
    document.querySelectorAll("[data-poll]").forEach(poll);
    document.querySelectorAll("form[data-busy]").forEach(busy);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
