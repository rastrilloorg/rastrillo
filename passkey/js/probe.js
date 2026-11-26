// probe.js — decides before the first paint whether this browser can
// use a passkey. ui's signin-head partial loads it as a classic script
// in <head>, which holds back the first paint, so the card is first
// drawn with the passkey buttons as they will stay. Shown later by a
// module, a door moved the email form's Continue under a person's tap
// (rastrillo discussion #433).
//
// Where PublicKeyCredential and navigator.credentials exist (webauthn's
// available()), it marks <html data-rst-passkeys>, then watches the card
// being parsed and shows each sign-in door (data-rst-passkey, shipped
// hidden) and enables each add button (data-rst-passkey-register,
// shipped disabled) as it arrives. Mutation callbacks run before the
// browser renders, so no frame shows them otherwise. With no script, or
// no WebAuthn, they stay as shipped.
//
// Until a button's module claims it (rstPasskeyClaimed), a person's
// press is kept on the button object (rstPasskeyPress) for the module
// to run. Only a trusted event counts, and only a property a script set
// can hold it: an attribute in the markup or a script's click() would
// start a ceremony nobody asked for. Sending a form or leaving the page
// drops a kept press. If a module script marked data-rst-passkey-script
// fails to load, a press says what the module would have said, since
// that module will never answer it.
//
// It also puts away a door's note on Escape or its close button, for
// the module as for itself, and marks <html data-rst-passkey-notes> so
// the module does not do it twice.
(function () {
  "use strict";
  if (!window.PublicKeyCredential || !navigator.credentials) return;
  document.documentElement.setAttribute("data-rst-passkeys", "");
  var buttons = "[data-rst-passkey], [data-rst-passkey-register]";
  var broken = false;
  var ready = function () {
    var doors = document.querySelectorAll("[data-rst-passkey][hidden]");
    for (var i = 0; i < doors.length; i++) doors[i].hidden = false;
    var adds = document.querySelectorAll(
      "[data-rst-passkey-register][disabled]");
    for (var j = 0; j < adds.length; j++) adds[j].disabled = false;
  };
  var each = function (act) {
    var all = document.querySelectorAll(buttons);
    for (var i = 0; i < all.length; i++) act(all[i]);
  };
  var drop = function () {
    each(function (button) { button.rstPasskeyPress = false; });
  };
  // say is signin.mjs's say(): the words, and the note's close button
  // shown only with them.
  var say = function (msg, words) {
    msg.textContent = words;
    var note = msg.closest("[rst-signin-passkey-note]");
    var close = note && note.querySelector("[rst-signin-passkey-close]");
    if (close) close.hidden = !words;
  };
  // unsay is signin.mjs's unsay(): every note cleared, and focus that was
  // in one back on its door's button.
  var unsay = function () {
    var notes = document.querySelectorAll("[rst-signin-passkey-note]");
    for (var i = 0; i < notes.length; i++) {
      var msg = notes[i].querySelector("[rst-signin-passkey-msg]");
      if (!msg || !msg.textContent) continue;
      var inside = notes[i].contains(document.activeElement);
      say(msg, "");
      var door = notes[i].parentElement.querySelector("[data-rst-passkey]");
      if (inside && door) door.focus();
    }
  };
  document.documentElement.setAttribute("data-rst-passkey-notes", "");
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") unsay();
  });
  document.addEventListener("click", function (event) {
    if (event.target.closest &&
        event.target.closest("[rst-signin-passkey-close]")) unsay();
  });
  // fail is signin.mjs's unavailable(), and register.mjs's failed(): the
  // words, and on the one-way door the email way.
  var fail = function (button) {
    var msg = document.getElementById(
      button.getAttribute("aria-describedby") || "");
    if (msg) {
      say(msg, button.getAttribute("data-rst-passkey-unavailable") ||
        button.getAttribute("data-rst-passkey-failed") || "");
    }
    var to = button.getAttribute("data-rst-passkey-fallback") || "";
    if (to.charAt(0) === "/" && to.charAt(1) !== "/" &&
        !/[\\\u0000-\u001f\u007f]/.test(to)) location.replace(to);
  };
  var watch = new MutationObserver(ready);
  watch.observe(document.documentElement, { childList: true, subtree: true });
  document.addEventListener("DOMContentLoaded", function () {
    watch.disconnect();
    ready();
  });
  document.addEventListener("click", function (event) {
    var button = event.isTrusted && event.target.closest &&
      event.target.closest(buttons);
    if (!button || button.rstPasskeyClaimed) return;
    if (broken) fail(button);
    else button.rstPasskeyPress = true;
  }, true);
  document.addEventListener("error", function (event) {
    var script = event.target;
    if (!script || script.tagName !== "SCRIPT" ||
        !script.hasAttribute("data-rst-passkey-script")) return;
    broken = true;
    each(function (button) {
      if (button.rstPasskeyPress) {
        button.rstPasskeyPress = false;
        fail(button);
      }
    });
  }, true);
  document.addEventListener("submit", drop, true);
  document.addEventListener("submit", unsay, true);
  window.addEventListener("pagehide", drop);
})();
