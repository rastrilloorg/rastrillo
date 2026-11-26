// pow-hint.js — draws a pow form's status when pow.js fails to load.
//
// The status ([data-pow-status]) is a live region kept out of sight by
// rst-sr-only, so nothing appears or disappears on a form that works
// (rastrillo discussion #437). A form whose pow.js never arrives keeps
// its submit disabled for good, and then its words ("if this form
// doesn't respond, reload the page") must be seen: moving the page is
// the lesser harm. Form.Script renders this file before pow.js, and
// both are deferred, so this runs first and hears the module's error
// event, which fires for a failed fetch, a refused one and a missing
// import. A timeout would also draw it on a slow network.
(function () {
  "use strict";
  document.addEventListener("error", function (event) {
    var script = event.target;
    if (!script || script.tagName !== "SCRIPT" ||
        !script.hasAttribute("data-pow-script")) return;
    var all = document.querySelectorAll("[data-pow-status]");
    for (var i = 0; i < all.length; i++) {
      all[i].classList.remove("rst-sr-only");
    }
  }, true);
})();
