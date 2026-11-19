/* rastrillo.js — the fragment shim. First-party, dependency-free, and
   inert: the polling section answers only to an opt-in data attribute,
   and everything it enhances also works with scripts disabled (a
   status page's <noscript> meta refresh). This file is app-owned from
   the moment it is scaffolded — edit it like any other static file.

   Menu dismiss, the switch's drag, the touch listener and the colour
   scheme menu's in-place apply are Fiesta's fiesta.js, which every
   page loads beside this file. An older rastrillo.js held them too;
   running that one beside fiesta.js is not supported.

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

   Every poll carries the request header Rastrillo-Fragment: 1, which
   marks the request as a shim poll so a handler can tell it apart from
   direct navigation. Reserved: the framework's own handlers do not
   read it today.

   A polled response may answer 204 with a Rastrillo-Location header
   instead of a fragment; the shim navigates there, but only to a local
   path. 403 and 404 end the poll for good — the job was swept, or was
   never yours. Any other fetch error backs off (doubling to a 30s cap)
   and keeps trying — a network blip must not strand a status page.

   The scheme cookie. fiesta.js applies a choice from the colour-scheme
   menu in place, so its form never reaches the server; this file
   writes the cookie that form would have set, from the rst-scheme
   event fiesta.js dispatches, so the next render paints the same
   scheme first. */
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

  // The cookie is the server's own (name, path, age, SameSite; see
  // scheme.go), or a reload paints the scheme the reader just left.
  // System is no cookie, as on the server.
  document.addEventListener("rst-scheme", function (e) {
    var s = e.detail && e.detail.scheme, on = s === "light" || s === "dark";
    document.cookie = "rastrillo_scheme=" + (on ? s + "; Max-Age=31536000" : "; Max-Age=0") + "; Path=/; SameSite=Lax" + (location.protocol === "https:" ? "; Secure" : "");
  });

  function scan() {
    document.querySelectorAll("[data-poll]").forEach(poll);
  }

  // A prerendered page runs this before anyone is looking at it, and
  // may never be shown: polling there would fetch on a timer, or hold an
  // EventSource open, for a page the reader only hovered a link to. It
  // starts when the page is activated instead.
  function start() {
    if (document.prerendering) document.addEventListener("prerenderingchange", scan, { once: true });
    else scan();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
