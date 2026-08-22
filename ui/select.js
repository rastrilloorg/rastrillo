/* select.js — the searchable-select enhancement. A sibling of
   rastrillo.js, following exactly the same rules: first-party,
   dependency-free, and inert by default. Only a <select> that opts in
   with data-rst-select gets behavior, and what it enhances works with
   scripts disabled, because the thing it enhances is a real <select>
   that never leaves the page.

   It is a separate file rather than more of rastrillo.js so that both
   stay small enough for an app owner to read in one sitting — the shim
   holds itself to 8KB and a full ARIA combobox would have doubled it.
   Both are app-owned from the moment they are scaffolded.

   Vocabulary:
     data-rst-select                 on a <select>: mirror a filterable
                                     ARIA 1.2 combobox onto it
     data-rst-select-filter          placeholder for the filter input
     data-rst-select-results         live-region text, {n} substituted
     data-rst-select-result-one      live-region text for exactly one

   ui/partials/field-select.html emits all four past ten options. The
   strings come from the framework's base catalog (rastrillo.ui.select_*)
   and ride out on attributes, because this markup is built in the
   browser where the catalog is out of reach. */
(function () {
  "use strict";

// A <select> with too many options to scan by eye becomes a filterable
// combobox — but the <select> itself never leaves the DOM. It is
// visually hidden and mirrored, so form submission, required
// validation, reset and browser autofill all keep working on the
// element they always did. Nothing here invents state: the select is
// the single source of truth, and if this function never runs the user
// gets a perfectly ordinary native select.
//
// Strings arrive on data attributes because this markup is built in
// the browser, after the catalog is out of reach. See
// rastrillo.ui.select_* in the framework's base catalog.
function combo(native) {
  if (native.dataset.rstEnhanced) return; // idempotent: safe to re-scan
  native.dataset.rstEnhanced = "true";

  var options = Array.prototype.slice.call(native.options);
  var id = native.id;
  var listId = id + "-listbox";
  var filterLabel = native.getAttribute("data-rst-select-filter") || "Type to filter";
  var manyFmt = native.getAttribute("data-rst-select-results") || "{n} results";
  var oneFmt = native.getAttribute("data-rst-select-result-one") || "1 result";

  var wrap = document.createElement("div");
  wrap.className = "rst-combo";

  var input = document.createElement("input");
  input.type = "text";
  input.className = "rst-input rst-combo__input";
  input.id = id + "-combo";
  input.autocomplete = "off";
  input.setAttribute("role", "combobox");
  input.setAttribute("aria-expanded", "false");
  input.setAttribute("aria-controls", listId);
  input.setAttribute("aria-autocomplete", "list");
  input.setAttribute("placeholder", filterLabel);

  var list = document.createElement("ul");
  list.className = "rst-combo__list";
  list.id = listId;
  list.setAttribute("role", "listbox");
  list.hidden = true;

  var status = document.createElement("p");
  status.className = "rst-sr-only";
  status.setAttribute("role", "status");
  status.setAttribute("aria-live", "polite");

  native.parentNode.insertBefore(wrap, native);
  wrap.appendChild(input);
  wrap.appendChild(list);
  wrap.appendChild(status);

  // The label named the select; it now names the control the user
  // actually types into. Without this the combobox has no accessible
  // name at all.
  var label = native.id && document.querySelector('label[for="' + native.id + '"]');
  if (label) label.setAttribute("for", input.id);
  // Drop .rst-input as well as adding .rst-sr-only: both are single
  // class selectors, so .rst-input's width:100% would win on source
  // order and leave the hidden select a full-width absolutely-positioned
  // box held out of sight by clip-path alone. A control nobody can see
  // does not need input styling.
  native.classList.remove("rst-input");
  native.classList.add("rst-sr-only");
  native.setAttribute("tabindex", "-1");
  native.setAttribute("aria-hidden", "true");

  var shown = [];
  var active = -1;

  function chosenLabel() {
    var o = native.options[native.selectedIndex];
    return o && o.value !== "" ? o.text : "";
  }
  input.value = chosenLabel();

  function close() {
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
    active = -1;
  }

  function commit(option) {
    native.value = option.value;
    input.value = option.text;
    // Mirror onto the real control, so a listener the app attached to
    // the select still fires.
    native.dispatchEvent(new Event("change", { bubbles: true }));
    close();
  }

  function draw(filter) {
    var needle = filter.trim().toLowerCase();
    shown = options.filter(function (o) {
      return o.value !== "" && o.text.toLowerCase().indexOf(needle) !== -1;
    });
    list.innerHTML = "";
    shown.forEach(function (o, i) {
      var li = document.createElement("li");
      li.id = listId + "-" + i;
      li.className = "rst-combo__option";
      li.setAttribute("role", "option");
      li.setAttribute("aria-selected", o.value === native.value ? "true" : "false");
      li.textContent = o.text;
      li.addEventListener("mousedown", function (e) {
        e.preventDefault(); // keep focus; blur would undo the pick
        commit(o);
      });
      list.appendChild(li);
    });
    list.hidden = shown.length === 0;
    input.setAttribute("aria-expanded", shown.length ? "true" : "false");
    status.textContent = shown.length === 1
      ? oneFmt
      : manyFmt.replace("{n}", String(shown.length));
    active = -1;
    input.removeAttribute("aria-activedescendant");
  }

  function move(delta) {
    if (list.hidden) draw(input.value);
    if (!shown.length) return;
    active = (active + delta + shown.length) % shown.length;
    Array.prototype.forEach.call(list.children, function (li, i) {
      li.classList.toggle("is-active", i === active);
    });
    input.setAttribute("aria-activedescendant", listId + "-" + active);
  }

  input.addEventListener("input", function () { draw(input.value); });
  input.addEventListener("focus", function () {
    // Select the committed label so the first keystroke replaces it
    // rather than appending to it. Without this, typing into a control
    // that already holds "Option 1" filters for "Option 1Option 12" and
    // silently matches nothing — the list looks empty for no stated
    // reason and Enter commits whatever was already there.
    input.select();
    draw("");
  });
  input.addEventListener("blur", function () {
    // A half-typed filter is not a value: restore what is committed.
    window.setTimeout(function () { input.value = chosenLabel(); close(); }, 0);
  });
  input.addEventListener("keydown", function (e) {
    switch (e.key) {
      case "ArrowDown": e.preventDefault(); move(1); break;
      case "ArrowUp": e.preventDefault(); move(-1); break;
      case "Home": if (!list.hidden) { e.preventDefault(); active = -1; move(1); } break;
      case "End": if (!list.hidden) { e.preventDefault(); active = shown.length - 1; move(0); } break;
      case "Enter":
        if (!list.hidden && active >= 0) { e.preventDefault(); commit(shown[active]); }
        break;
      case "Escape": e.preventDefault(); close(); input.value = chosenLabel(); break;
    }
  });
}

// combo is idempotent (it flags the select), so re-scanning is safe —
// which matters if a future change re-scans after the shim swaps a
// polled fragment in. Today nothing does, so a select that arrives
// inside a replaced fragment stays a plain native select: correct, just
// not enhanced.
function scan() {
  document.querySelectorAll("select[data-rst-select]").forEach(combo);
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", scan);
} else {
  scan();
}
})();
