/* select.js — the searchable-select enhancement. A sibling of
   rastrillo.js, following exactly the same rules: first-party,
   dependency-free, and inert by default. Only a <select> that opts in
   with data-rst-select gets behavior, and what it enhances works with
   scripts disabled, because the thing it enhances is a real <select>
   that never leaves the page.

   It is a separate file rather than more of rastrillo.js so that both
   stay small enough for an app owner to read in one sitting — the shim
   holds itself to 8KB and a full ARIA combobox would have doubled it;
   this one holds itself to 12KB.
   Both are app-owned from the moment they are scaffolded.

   Vocabulary:
     data-rst-select                 on a <select>: mirror a filterable
                                     ARIA 1.2 combobox onto it
     data-rst-select="false"         never; stay a native select at any
                                     size
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

  // The list mirrors the select's own shape rather than flattening it:
  // an <optgroup> the author wrote to make a long list readable becomes
  // a group in the popup, and loose <option> children sit at the top
  // level as a group with no label. native.options would have been
  // shorter and would have silently thrown the headings away.
  var groups = [];
  Array.prototype.forEach.call(native.children, function (el) {
    if (el.tagName === "OPTGROUP") {
      groups.push({ label: el.label, options: Array.prototype.slice.call(el.querySelectorAll("option")) });
    } else if (el.tagName === "OPTION") {
      groups.push({ label: "", options: [el] });
    }
  });
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
  // Drop .rst-input too: both are single class selectors, so its
  // width:100% wins on source order and would leave the hidden select a
  // full-width box held out of sight by clip-path alone.
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

  // One pickable row. Its index is its position in `shown`, which is
  // the keyboard order — headings never enter it, so arrowing down
  // steps option to option straight across a group boundary.
  function row(o) {
    var li = document.createElement("li");
    li.id = listId + "-" + shown.length;
    li.className = "rst-combo__option";
    li.setAttribute("role", "option");
    li.setAttribute("aria-selected", o.value === native.value ? "true" : "false");
    li.textContent = o.text;
    li.addEventListener("mousedown", function (e) {
      e.preventDefault(); // keep focus; blur would undo the pick
      commit(o);
    });
    shown.push(o);
    return li;
  }

  function draw(filter) {
    var needle = filter.trim().toLowerCase();
    shown = [];
    list.innerHTML = "";
    groups.forEach(function (g) {
      var matched = g.options.filter(function (o) {
        return o.value !== "" && o.text.toLowerCase().indexOf(needle) !== -1;
      });
      // A group filtered down to nothing takes its heading with it: a
      // heading over no rows is a lie about what is there.
      if (!matched.length) return;
      var into = list;
      if (g.label) {
        // The group's name lives on aria-label, so the heading itself is
        // furniture — hidden from the accessibility tree, out of the
        // keyboard order, announced once as the group it names.
        var box = document.createElement("li");
        box.setAttribute("role", "group");
        box.setAttribute("aria-label", g.label);
        // <li> cannot hold <li>, so the rows nest in a list of their
        // own; role=none keeps it out of the accessibility tree, which
        // leaves the options owned by the group.
        into = document.createElement("ul");
        into.className = "rst-combo__rows";
        into.setAttribute("role", "none");
        var head = document.createElement("li");
        head.className = "rst-select__group";
        head.setAttribute("aria-hidden", "true");
        head.textContent = g.label;
        into.appendChild(head);
        box.appendChild(into);
        list.appendChild(box);
      }
      matched.forEach(function (o) { into.appendChild(row(o)); });
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
    // The rows, not list.children: a grouped list's children are the
    // groups, and the headings inside them are not stops.
    list.querySelectorAll('[role="option"]').forEach(function (li, i) {
      li.classList.toggle("is-active", i === active);
    });
    input.setAttribute("aria-activedescendant", listId + "-" + active);
  }

  input.addEventListener("input", function () { draw(input.value); });
  input.addEventListener("focus", function () {
    // Select the committed label so the first keystroke replaces it.
    // Otherwise typing into a box holding "Option 1" filters for
    // "Option 1Option 12" and silently matches nothing.
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
        // Enter acts on the filter, never on the form: submitting
        // mid-filter discards the edit and silently keeps the previous
        // selection. Take the highlight, or the only match, or close.
        e.preventDefault();
        if (active >= 0) {
          commit(shown[active]);
        } else if (shown.length === 1) {
          commit(shown[0]);
        } else {
          close();
        }
        break;
      case "Escape": e.preventDefault(); close(); input.value = chosenLabel(); break;
    }
  });

  // The same guard datetime.js carries, where the long version of this
  // note lives. Enter belongs to the combobox, never to the form: a
  // commit that also submits posts whatever the select held before the
  // user chose. On a key a person presses, the keydown handler's
  // preventDefault settles it — the engine derives the keypress from
  // the keydown it just cancelled. On a SYNTHESISED key it does not:
  // CDP, and every browser drive built on it, can deliver the keydown
  // and the character as two independent events, so the second arrives
  // with no memory of the first one's refusal, carrying the \r that
  // implicit form submission listens for. Measured, not guessed (issue
  // #86): without this line the select drive fired a submit on every
  // run and passed only by outrunning the navigation.
  input.addEventListener("keypress", function (e) {
    if (e.key === "Enter") e.preventDefault();
  });
}

// Idempotent, so re-scanning is safe if a future change re-scans after
// the shim swaps in a polled fragment. Today nothing does: a select
// arriving inside a replacement stays native — correct, not enhanced.
function scan() {
  document.querySelectorAll("select[data-rst-select]").forEach(function (el) {
    // data-rst-select="false" is the markup-side opt-out: a select that
    // says no stays native at any size. It has to be checked rather
    // than selected against, because to CSS the attribute is simply
    // present. field-select opts out the other way, by never emitting
    // the attribute at all; this is for markup written by hand.
    if (el.dataset.rstSelect !== "false") combo(el);
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", scan);
} else {
  scan();
}
})();
