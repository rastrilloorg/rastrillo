/* An incoming transition can reject before pagereveal when the destination
   does not opt in. Load before styles, and observe each transition once. */
(function () {
  "use strict";
  var observed = new WeakSet();
  function observe(transition) {
    if (!transition || observed.has(transition)) return;
    observed.add(transition);
    ["ready", "finished", "updateCallbackDone"].forEach(function (key) {
      if (transition[key]) transition[key].catch(function (err) {
        if (!(err instanceof DOMException) ||
            ["AbortError", "InvalidStateError", "TimeoutError"].indexOf(err.name) < 0) throw err;
      });
    });
  }
  observe(document.activeViewTransition);
  window.addEventListener("pagereveal", function (event) {
    observe(event.viewTransition);
  });
})();
