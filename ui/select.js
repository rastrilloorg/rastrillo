/* select.js — the searchable-select enhancement. A sibling of
   rastrillo.js, following the same rules: first-party, dependency-free,
   and inert by default. Only a <select> that opts in with
   data-rst-select gets behaviour, and what it enhances works with
   scripts disabled, because the thing it enhances is a real <select>
   that never leaves the page.

   Two halves in one file. The PURE half — folding and ranking a query,
   what leaving the box commits, which blanks are prompts — has no DOM
   and no state, and is exported to Node behind a guard a browser never
   takes, so ui/select_node.mjs can pin the ranking without a browser
   (datetime.js's arrangement). Everything that touches a page stays
   behind the `document` guard. App-owned from the moment it is
   scaffolded. Converged with Tito Go's searchselect, whose country
   picker is where most of the rules below were learned.

   Vocabulary, on the <select>:
     data-rst-select                 mirror a filterable ARIA 1.2 combobox
                                     onto it
     data-rst-select="false"         never; stay a native select
     data-rst-select-filter          the search box's placeholder
     data-rst-select-results         live-region text, {n} substituted
     data-rst-select-result-one      live-region text for exactly one
     data-rst-select-no-matches      the row shown when nothing matches
   On an <option>, all optional:
     data-rst-name / data-rst-desc   a name and a quieter description, when
                                     the option's text is "Name (desc)"
     data-rst-short                  what the CLOSED box shows once this is
                                     picked ("+44"), for a narrow box
     data-rst-lead                   a decorative glyph before the row and
                                     the box (a flag); never the only thing
                                     saying which option this is
     data-rst-terms                  more words the search matches (an ISO
                                     code, "+971") that the row does not print
     data-rst-first                  leads its ties in an exact search
     data-rst-prompt                 a blank that asks rather than answers
                                     ("Country")
   An <hr> between two options is drawn as a divider, hidden while
   searching. An <optgroup> becomes a labelled group; a search ranks
   across groups and hides their headings, and clearing it puts every
   row back in its own group.

   ui/partials/field-select.html emits the select's strings from the
   framework's base catalog (rastrillo.ui.select_*), on attributes,
   because this markup is built in the browser where the catalog is out
   of reach. */
(function () {
  "use strict";

  // fold is what a query and an option are compared as: lower case, with the
  // accents taken off and a curly apostrophe read as a straight one, so "cote"
  // and "cote d'ivoire" find "Côte d’Ivoire" and "osterreich" finds
  // "Österreich" — someone on a keyboard without the letter still gets there.
  const fold = (s) =>
    String(s || "").normalize("NFD").replace(/\p{M}/gu, "").replace(/[\u2018\u2019]/g, "'").toLowerCase().trim();

  // rank says how well one option answers a query, lower being better, or -1
  // for no match at all. An option is {name, text, terms}: name is the words
  // the row leads with, text the whole string (name and desc), terms the extra
  // words it answers to. 0 is an exact hit (typing "gb" or "+44" or the whole
  // name); 1 is the start of the name; 2 the start of one of its later words,
  // or of a term; 3 is anywhere in the text. The start of the name outranks the
  // start of a later word, so "sudan" is Sudan before South Sudan and "guinea"
  // is Guinea before Equatorial Guinea. A term is matched from its start with
  // or without a leading "+", so "971" finds +971 as surely as "+971" does.
  // folded is an option's words read once, as rank compares them: an option
  // may carry them precomputed (select.js does, so a keystroke over 240
  // countries folds only the query), or they are read here. Everything rank
  // derives from an option's own words is read here too — the name without
  // its trailing bracket, the words it splits into, the terms without their
  // "+" — so a keystroke does no string work on an option at all.
  const plain = (t) => t.replace(/^\+/, "");
  const folded = (o) => {
    if (o.folded) return o.folded;
    const name = fold(o.name);
    const terms = (o.terms || []).map(fold).filter(Boolean);
    return {
      name,
      text: fold(o.text),
      terms,
      // A trailing bracket is a gloss on the name, not part of it: somebody who
      // types "india" whole has named "India (+91)" exactly.
      bareName: name.replace(/\s*\([^()]*\)$/, ""),
      words: name.split(/[\s\-’'(),.]+/).filter(Boolean),
      plainTerms: terms.map(plain),
    };
  };

  // ranked is rank for a query already folded, which is how order asks: once
  // per keystroke, not once per option.
  const ranked = (o, q) => {
    if (!q) return 0;
    const bare = q.replace(/^\+/, "");
    const f = folded(o);
    const name = f.name;
    const terms = f.terms;
    const plains = f.plainTerms;
    // A term typed whole, with or without its "+": "44" is +44 exactly.
    if (name === q || f.bareName === q || terms.includes(q) || (bare && plains.includes(bare))) return 0;
    if (name.startsWith(q)) return 1;
    if (f.words.some((w) => w.startsWith(q))) return 2;
    if (bare && plains.some((t) => t.startsWith(bare))) return 2;
    if (f.text.includes(q)) return 3;
    return -1;
  };

  const rank = (o, query) => ranked(o, fold(query));

  // order returns the options a query matches, best first. Among EXACT matches
  // (rank 0 — a calling code typed whole) an option marked `first` (data-rst-first) leads — the country that owns
  // a shared calling code, so "+1" opens on the United States rather than on
  // American Samoa — and after that ties keep the options' own order, which is
  // the page's own (or the alphabet's).
  const order = (opts, query) => {
    const q = fold(query);
    return opts
      .map((o, i) => ({ o, i, r: ranked(o, q) }))
      .filter((x) => x.r >= 0)
      .sort((a, b) => a.r - b.r || (a.r === 0 ? (b.o.first ? 1 : 0) - (a.o.first ? 1 : 0) : 0) || a.i - b.i)
      .map((x) => x.o);
  };

  // settle is the match that LEAVING the box after a search commits, or null:
  // the one row a search can only mean. hits are order()'s answer.
  //   - a single match settles;
  //   - a calling code typed whole whose best row owns it settles, with or
  //     without its "+" ("+44" and "44" are the United Kingdom, "1" the US);
  //   - on three or more characters, the best row settles when it is the ONLY
  //     row that good: an exact hit ("germany"), or a name that starts with
  //     the query when nothing else does ("ger" is Germany; Algeria and Niger
  //     only contain it). "ind" settles nothing: India and Indonesia tie, and
  //     neither does an exact hit two rows share.
  // Two letters alone never settle: "ge" is Georgia's code and the start of
  // Germany, and a wrong calling code under a phone number is worse than an
  // unpicked box the form then asks about.
  const settle = (hits, query) => {
    if (hits.length === 1) return hits[0];
    if (!hits.length) return null;
    const q = fold(query);
    const bare = q.replace(/^\+/, "");
    const r0 = rank(hits[0], query);
    // An exact hit settles only as the single best row: no other exact
    // hit, or exactly one of them marked first (the country that owns a
    // shared calling code). "Springfield (IL)" and "(MA)" settle nothing.
    const exact = () => {
      const ties = hits.filter((h) => rank(h, query) === 0);
      return ties.length === 1 || ties.filter((h) => h.first).length === 1 ? hits[0] : null;
    };
    if (r0 === 0 && /^\d+$/.test(bare)) return hits[0].first ? exact() : null;
    if (bare.length < 3 || /^\d+$/.test(bare)) return null;
    if (r0 === 0) return exact();
    return r0 < rank(hits[1], query) ? hits[0] : null;
  };

  // isPrompt says whether an option is a PROMPT rather than an answer: one the
  // page marked data-rst-prompt (a phone number's "Country"), or the blank of a
  // REQUIRED select ("Choose one" over the country question). A prompt is never
  // shown as the pick — the box stays empty so its placeholder ("Search
  // countries") says what is wanted — and a required select never offers its
  // blank as a row: choosing it would only leave the question unanswered. An optional select's blank
  // ("No answer") is a real answer and is neither.
  // A DISABLED blank is never an answer either, required or not: a page that
  // renders "Choose one" disabled means it, and a select that stops being
  // required (a sibling script unticking a box) must not bring it back as a
  // pick.
  const isPrompt = (value, marked, required, disabled) =>
    marked || (value === "" && (required || !!disabled));

  // offered says whether a blank option is listed. A MARKED prompt (a phone
  // number's "Country") is listed while nothing is typed and drops out once
  // something is — required or not, exactly as before. An unmarked blank on a
  // required select is never listed. Any other blank ("No answer") always is.
  const offered = (value, marked, required, searching, disabled) =>
    value !== "" || (marked ? !searching : !(required || !!disabled));

  // The pure half, for ui/select_node.mjs. A browser has no `module` and
  // never takes this branch.
  if (typeof module !== "undefined" && module && module.exports) {
    module.exports = { fold: fold, rank: rank, order: order, settle: settle, isPrompt: isPrompt, offered: offered };
  }
  if (typeof document === "undefined") return;

  const ACTS = new Set(["ArrowDown", "ArrowUp", "Home", "End", "Enter", "Tab"]);

  const el = (tag, attr) => {
    const e = document.createElement(tag);
    if (attr) e.setAttribute(attr, "");
    return e;
  };

  // A <select> with too many options to scan by eye becomes a filterable
  // combobox — but the <select> itself never leaves the DOM. It is
  // visually hidden and mirrored, so form submission, required
  // validation, reset and autofill all keep working on the element they
  // always did. The select is the single source of truth; if this never
  // runs, the user gets an ordinary native select.
  function combo(native) {
    if (native.dataset.rstEnhanced) return; // idempotent: safe to re-scan
    native.dataset.rstEnhanced = "true";

    const id = native.id || "rst-select-" + Math.random().toString(36).slice(2);
    const listId = id + "-listbox";
    const say = (key, fallback) => native.getAttribute("data-rst-select-" + key) || fallback;
    const manyFmt = say("results", "{n} results");
    const oneFmt = say("result-one", "1 result");

    // The option model mirrors the live select, index for index, so a row
    // and select.selectedIndex always agree. Groups are kept: each option
    // knows the list it belongs in, and a search that lifts it out puts
    // it back there.
    const groups = [];
    const opts = Array.from(native.options).map((o, i) => {
      const d = o.dataset;
      const g = o.parentElement.tagName === "OPTGROUP" ? o.parentElement : null;
      if (g && groups[groups.length - 1] !== g) groups.push(g);
      return {
        text: o.text,
        name: d.rstName || o.text,
        desc: d.rstDesc || "",
        short: d.rstShort || "",
        lead: d.rstLead || "",
        terms: (d.rstTerms || "").split(/\s+/).filter(Boolean),
        marked: d.rstPrompt !== undefined,
        // Whether it is a prompt can change: a sibling script may stop the
        // select being required, so the observer below re-derives it.
        prompt: isPrompt(o.value, d.rstPrompt !== undefined, native.required, o.disabled),
        first: d.rstFirst !== undefined,
        sepBefore: !!(o.previousElementSibling && o.previousElementSibling.tagName === "HR"),
        disabled: o.disabled,
        blank: o.value === "",
        index: i,
        group: g,
        // The native option itself, read live: an app that removes or
        // reorders options after enhancement moves every index, and a
        // cached one would submit a different option from the row chosen.
        el: o,
        li: null,
      };
    });
    for (const o of opts) o.folded = folded(o);
    const compact = opts.some((o) => o.short);

    const wrap = el("div", "rst-combo");
    if (compact) wrap.setAttribute("rst-combo-compact", "");
    // The pick's glyph, drawn before the box. Decorative: the description
    // below is what says which option it is.
    const lead = el("span", "rst-combo-lead");
    lead.setAttribute("aria-hidden", "true");
    // The whole name of a compact pick, for a screen reader only: a box
    // showing "+44" is described as "United Kingdom +44".
    const said = el("span");
    said.id = id + "-said";
    said.hidden = true;

    const input = el("input", "rst-input");
    input.type = "text";
    input.id = id + "-combo";
    input.autocomplete = "off";
    input.spellcheck = false;
    // A compact box is too narrow for a search sentence, so it says what
    // it is for in the one word it is labelled with.
    input.placeholder = compact && native.getAttribute("aria-label")
      ? native.getAttribute("aria-label")
      : say("filter", "Type to filter");
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-controls", listId);
    input.setAttribute("aria-autocomplete", "list");
    const described = native.getAttribute("aria-describedby");
    if (described) input.setAttribute("aria-describedby", described);
    if (compact) input.setAttribute("aria-describedby", described ? described + " " + said.id : said.id);

    const list = el("ul", "rst-combo-list");
    list.id = listId;
    list.hidden = true;
    list.setAttribute("role", "listbox");

    const status = el("p");
    status.className = "rst-sr-only";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");

    // The label named the select; it now names the control the user
    // actually types into, and the list too.
    const label = native.id && document.querySelector('label[for="' + CSS.escape(native.id) + '"]');
    if (label) {
      if (!label.id) label.id = id + "-label";
      label.htmlFor = input.id;
      list.setAttribute("aria-labelledby", label.id);
    } else if (native.getAttribute("aria-label")) {
      input.setAttribute("aria-label", native.getAttribute("aria-label"));
      list.setAttribute("aria-label", native.getAttribute("aria-label"));
    }

    // Each group is a labelled box in the list. The group's name lives on
    // aria-label, so the heading inside is furniture — hidden from the
    // accessibility tree and out of the keyboard order. <li> cannot hold
    // <li>, so its rows nest in a list of their own with role=none.
    const boxes = new Map();
    for (const g of groups) {
      const box = el("li");
      box.setAttribute("role", "group");
      box.setAttribute("aria-label", g.label);
      const rows = el("ul", "rst-combo-rows");
      rows.setAttribute("role", "none");
      const head = el("li", "rst-select-group");
      head.setAttribute("aria-hidden", "true");
      head.textContent = g.label;
      rows.appendChild(head);
      box.appendChild(rows);
      boxes.set(g, { box, rows });
    }
    const home = (o) => (o.group ? boxes.get(o.group).rows : list);

    for (const o of opts) {
      const li = el("li", "rst-combo-option");
      li.id = listId + "-" + o.index;
      li.setAttribute("role", "option");
      li.setAttribute("aria-selected", "false");
      if (o.disabled) li.setAttribute("aria-disabled", "true");
      if (o.lead) {
        const glyph = el("span", "rst-combo-lead");
        glyph.setAttribute("aria-hidden", "true");
        glyph.textContent = o.lead;
        li.append(glyph);
      }
      if (o.desc) {
        // Two spans rather than the option's one string: the brackets are
        // for a reader without JavaScript, and here a quieter colour does
        // that job with no punctuation for a locale to argue with.
        const name = el("span", "rst-combo-name");
        name.textContent = o.name;
        const desc = el("span", "rst-combo-desc");
        desc.textContent = o.desc;
        li.append(name, desc);
      } else {
        li.append(o.text);
      }
      o.li = li;
      if (o.group && !list.contains(boxes.get(o.group).box)) list.appendChild(boxes.get(o.group).box);
      if (o.sepBefore) {
        o.sep = el("li", "rst-combo-sep");
        o.sep.setAttribute("aria-hidden", "true");
        home(o).appendChild(o.sep);
      }
      home(o).appendChild(li);
    }
    const empty = el("li", "rst-combo-empty");
    empty.hidden = true;
    empty.textContent = say("no-matches", "No matches");
    list.appendChild(empty);

    native.parentNode.insertBefore(wrap, native);
    wrap.append(lead, input, said, list, status);
    // Drop [rst-input] too: its width:100% would otherwise leave the hidden
    // select a full-width box held out of sight by clip-path alone.
    native.removeAttribute("rst-input");
    native.classList.add("rst-sr-only");
    native.setAttribute("tabindex", "-1");
    native.setAttribute("aria-hidden", "true");

    let open = false;
    // The highlighted row itself, not an index: an extra row is in `opts`
    // but has no index in the select.
    let active = null;
    let extras = 0;

    // The selection as the select itself reports it: an option removed after
    // enhancement keeps `selected` set, so it is never asked directly.
    const selectedEl = () => native.options[native.selectedIndex] || null;
    const picked = () => opts.find((o) => o.el && o.el === selectedEl()) || null;
    // A prompt is not an answer, so the closed box shows nothing for it and
    // its placeholder says what is wanted.
    const currentText = () => {
      const o = picked();
      return o && !o.prompt ? o.short || o.name : "";
    };
    // Whether the box shows the pick or words somebody typed: an open
    // list refreshed from outside (a required flip, new extras) searches
    // for the typed words, never for the pick's own text.
    let showingPick = true;
    const showText = () => {
      input.value = currentText();
      showingPick = true;
    };
    const query = () => (showingPick ? "" : input.value);
    const showPick = () => {
      const o = picked();
      lead.textContent = (o && o.lead) || "";
      lead.hidden = !lead.textContent;
      wrap.toggleAttribute("rst-combo-has-lead", !lead.hidden);
      said.textContent = o && o.short ? [o.name, o.desc].filter(Boolean).join(" ") : "";
    };
    showText();
    showPick();

    // The row that LEAVING the box after typing takes (Tab, or a tap
    // elsewhere), or null: settle() says which searches can only mean one
    // row. A row somebody ARROWED to is their choice, as on a native select.
    let steered = false;
    const settledPick = () => {
      if (!open || !active || active.run) return null;
      if (steered) return active.el === selectedEl() ? null : active;
      if (input.value === currentText()) return null;
      const pick = settle(lastHits, input.value);
      return pick && pick === (lastHits.length === 1 ? pick : active) ? pick : null;
    };

    // The rows in the order they are on screen: best match first while
    // searching, the select's own order otherwise; the page's extras last.
    let shown = opts;
    const visible = () => shown.filter((o) => !o.li.hidden && !o.disabled);
    // What a list falls back to highlighting: one of the select's own
    // answers — never a row the page added (it owns Enter), and never a
    // prompt (a bare Enter would "answer" with the question).
    const firstReal = () => visible().find((o) => !o.run && !o.prompt) || null;
    const unanswered = () => {
      const sel = picked();
      // Nothing selected at all (selectedIndex -1) is unanswered too.
      return !sel || sel.prompt;
    };
    // The row an idle list highlights: the pick on an answered box, nothing
    // on an unanswered one — highlighting the first row would let a bare
    // Enter answer the question with whatever happens to sort first. A
    // cleared search lands where opening does, so an idle list reads one
    // way however it got there.
    const opening = () => {
      if (unanswered()) return null;
      const sel = picked();
      return sel && !sel.li.hidden && !sel.disabled ? sel : firstReal();
    };
    // The rows the arrows, Home and End step through: what is on screen,
    // less a prompt, which is still there to tap.
    const steps = () => visible().filter((o) => !o.prompt);

    const setActive = (o, atTop) => {
      // Only the row losing the highlight and the row gaining it are touched.
      if (active && active !== o) active.li.classList.remove("is-active");
      if (o) o.li.classList.add("is-active");
      active = o || null;
      if (o) {
        input.setAttribute("aria-activedescendant", o.li.id);
        if (atTop && visible()[0] === o) list.scrollTop = 0;
        else o.li.scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };

    // A prompt is never selected: a listbox announcing a selected option
    // says somebody answered the question.
    const markSelected = () => {
      for (const o of opts) {
        const v = o.el && o.el === selectedEl() && !o.prompt ? "true" : "false";
        if (o.li.getAttribute("aria-selected") !== v) o.li.setAttribute("aria-selected", v);
      }
    };

    // A burst of typing is searched twice a frame at most, however many
    // input events it fires: a phone keyboard can fire several inside one
    // frame (predictive text, autocorrect) and only the last is ever seen.
    // The FIRST input of a frame is searched at once, so a single keystroke
    // reads exactly as it always did; any more only mark the search stale,
    // and it runs once before the frame is drawn. Anything that ACTS on the
    // list (Enter, the arrows, Tab, leaving) catches it up first (flush),
    // so typing then pressing Enter takes the row for the words typed.
    let pending = false;
    let frame = 0;
    let lastQuery = null;
    let lastHits = [];
    // Rows a search has lifted to the top of the list, out of their own
    // place. Only these ever go back when the search is cleared.
    const lifted = new Set();
    // Where a row stands with no search: before its successor in the same
    // list, or last in its group, or before the next group or the empty row.
    const anchorAfter = (o) => {
      const next = opts[o.index + 1];
      if (next && !next.run && next.group === o.group) return next.sep || next.li;
      if (o.group) return null;
      if (next && !next.run) return next.group ? boxes.get(next.group).box : next.sep || next.li;
      return empty;
    };
    // A hidden state is written only when it changes: writing the same
    // value still invalidates the element's style.
    const show = (e, on) => {
      if (e.hidden === on) e.hidden = !on;
    };
    const filter = (query) => {
      pending = false;
      const own = opts.filter((o) => !o.run);
      const searching = fold(query) !== "";
      // A blank that is a prompt is never an answer: a required select never
      // lists its blank, and a marked prompt drops out once something is
      // typed. An optional select's "No answer" is a real answer.
      const answerable = (o) =>
        !o.blank || offered(o.el.value, o.marked, native.required, searching, o.disabled);
      const hits = order(own.filter(answerable), query);
      lastHits = hits.filter((o) => !o.disabled);
      const hit = new Set(hits);
      const extra = opts.filter((o) => o.run);
      shown = searching ? [...hits, ...own.filter((o) => !hit.has(o)), ...extra] : opts;
      const tail = extra.length ? extra[0].li : null;
      if (empty.nextSibling !== tail || empty.parentNode !== list) list.insertBefore(empty, tail);
      // What a keystroke costs is what it MOVES. A search lays out only its
      // hits, best first, at the top of the list; a row it hides stays where
      // it is, because a hidden row's place is never seen. A hit already
      // next in line is not moved, so narrowing a search moves almost
      // nothing. Clearing puts back only the rows a search lifted.
      if (searching) {
        const hitRow = new Set(hits.map((o) => o.li));
        let ref = list.firstChild;
        for (const o of hits) {
          // Never past the empty row (before the extras): a grouped hit is
          // not a list child, and would land after the extras.
          while (ref && ref !== empty && ref !== o.li && !hitRow.has(ref)) ref = ref.nextSibling;
          if (ref === o.li) {
            ref = ref.nextSibling;
          } else {
            list.insertBefore(o.li, ref);
            lifted.add(o);
          }
        }
      } else if (lifted.size) {
        // Highest index first, so each row's successor is home before it.
        for (const o of [...lifted].sort((a, b) => b.index - a.index)) home(o).insertBefore(o.li, anchorAfter(o));
        lifted.clear();
      }
      for (const o of own) {
        show(o.li, hit.has(o));
        if (o.sep) show(o.sep, !searching);
      }
      for (const { box } of boxes.values()) show(box, !searching);
      for (const o of extra) show(o.li, true);
      const found = hits.filter((o) => !o.disabled).length;
      show(empty, !found);
      const text = found === 1 ? oneFmt : manyFmt.replace("{n}", String(found));
      if (status.textContent !== text) status.textContent = text;
      // A NEW query re-ranks the list, so its best match is the one Enter
      // takes. The same query run again (the page handing over fresh
      // extras) keeps a row somebody arrowed to, if it is still there.
      const requeried = query !== lastQuery;
      if (requeried) steered = false;
      lastQuery = query;
      if (!searching && !steered && (requeried || unanswered())) setActive(opening());
      else if ((searching && requeried) || !active || !shown.includes(active) || active.li.hidden || active.disabled) {
        setActive(firstReal(), true);
      }
    };
    const flush = () => {
      if (pending && open) filter(input.value);
    };
    const onFrame = () => {
      frame = 0;
      flush();
    };
    const searchTyping = () => {
      if (frame) {
        pending = true;
        return;
      }
      frame = requestAnimationFrame(onFrame);
      filter(input.value);
    };

    const openList = (showAll) => {
      if (open) return;
      open = true;
      list.hidden = false;
      input.setAttribute("aria-expanded", "true");
      if (showAll) filter("");
      markSelected();
      setActive(opening());
    };

    const closeList = (revert) => {
      if (!open) return;
      open = false;
      list.hidden = true;
      input.setAttribute("aria-expanded", "false");
      input.removeAttribute("aria-activedescendant");
      // Typing not yet searched dies with the list, and so does the frame
      // that would have searched it — or the next keystroke, reopening the
      // list, would count as the second of that frame and show the old
      // search's rows against the new words.
      pending = false;
      cancelAnimationFrame(frame);
      frame = 0;
      // A search closed is a search over: the next one, even with the same
      // words, starts unsteered, or leaving would commit a row somebody
      // arrowed to in a search they cancelled.
      steered = false;
      lastQuery = null;
      if (active) active.li.classList.remove("is-active");
      if (revert) {
        showText();
        showPick();
      }
    };

    const choose = (o) => {
      if (!o || o.disabled) return;
      // An extra row does something instead of BEING something: there is
      // nothing to select, and the page decides what happens to the text.
      if (o.run) {
        o.run();
        return;
      }
      // A row whose option the page has since removed selects nothing.
      if (!native.contains(o.el)) {
        closeList(true);
        return;
      }
      o.el.selected = true;
      // Mirror onto the real control, so a listener the app attached to the
      // select still fires.
      native.dispatchEvent(new Event("change", { bubbles: true }));
      showText();
      showPick();
      markSelected();
      closeList(false);
    };

    input.addEventListener("focus", () => {
      // Select the pick so the first keystroke replaces it; otherwise typing
      // into "Option 1" searches for "Option 1O".
      input.select();
      openList(true);
    });
    input.addEventListener("click", () => openList(true));
    input.addEventListener("input", () => {
      showingPick = false;
      // Once somebody types, the box holds their words, not the pick.
      lead.hidden = true;
      wrap.removeAttribute("rst-combo-has-lead");
      openList(false);
      searchTyping();
    });

    // Whether this keystroke's keydown took Enter for the list: see the
    // keypress guard below.
    let tookEnter = false;
    input.addEventListener("keydown", (e) => {
      tookEnter = false;
      if (ACTS.has(e.key)) flush();
      switch (e.key) {
        case "ArrowDown": {
          e.preventDefault();
          if (!open) return openList(true);
          const v = steps();
          steered = true;
          setActive(v[Math.min(v.indexOf(active) + 1, v.length - 1)] || v[0] || null);
          break;
        }
        case "ArrowUp": {
          e.preventDefault();
          if (!open) return openList(true);
          const v = steps();
          steered = true;
          // From nothing highlighted, Up goes to the LAST row, as a native
          // select and the APG combobox pattern do.
          setActive((active ? v[Math.max(v.indexOf(active) - 1, 0)] : v[v.length - 1]) || null);
          break;
        }
        case "Home":
        case "End":
          if (open) {
            e.preventDefault();
            steered = true;
            const v = steps();
            setActive((e.key === "Home" ? v[0] : v[v.length - 1]) || null);
          }
          break;
        case "Enter":
          // With the list open, Enter belongs to the list: it picks the
          // highlighted row, and with none it does nothing. It never submits
          // the form from under an open list, which would post whatever the
          // select held before the person chose.
          if (open) {
            e.preventDefault();
            tookEnter = true;
            if (active) choose(active);
          }
          break;
        case "Escape":
          if (open) {
            // This Escape was for the list; something further up (a dialog)
            // must not take it as its own and close with the choice half made.
            e.preventDefault();
            e.stopPropagation();
            closeList(true);
          }
          break;
        case "Tab":
          // Somebody who typed "irel", sees Ireland highlighted and tabs on
          // means Ireland, as a native select commits its type-ahead on Tab.
          // With nothing typed, Tab only leaves. Escape reverts.
          if (settledPick()) choose(settledPick());
          else closeList(true);
          break;
      }
    });

    // A synthesised key can arrive as a keydown and a separate character
    // event, and the second carries the \r implicit form submission listens
    // for with no memory of the first one's preventDefault. Measured, not
    // guessed (issue #86): without this a browser drive submitted the form
    // on every Enter and passed only by outrunning the navigation. Keyed on
    // what the keydown did rather than on `open`, because taking a row has
    // closed the list by the time the character arrives.
    input.addEventListener("keypress", (e) => {
      if (e.key === "Enter" && tookEnter) e.preventDefault();
      tookEnter = false;
    });

    // mousedown, not click, so focus never leaves the input mid-pick.
    list.addEventListener("mousedown", (e) => {
      const li = e.target.closest("[rst-combo-option]");
      if (!li) return;
      e.preventDefault();
      choose(opts.find((o) => o.li === li));
    });

    // Leaving the box after typing takes the settled row, as Tab does: on a
    // phone there is no Tab. With nothing typed, leaving only leaves.
    wrap.addEventListener("focusout", (e) => {
      if (wrap.contains(e.relatedTarget)) return;
      flush();
      if (settledPick()) choose(settledPick());
      else closeList(true);
    });

    // Rows the page adds after the select's own, replacing whatever it added
    // last: suggestions for what to do with the typed text. Each is
    // {label, meta, run}; an empty list takes them all away.
    wrap.rstExtras = (rows) => {
      for (let i = opts.length - 1; i >= 0; i--) {
        if (!opts[i].run) break;
        opts[i].li.remove();
        opts.splice(i, 1);
      }
      for (const row of rows || []) {
        const li = el("li", "rst-combo-option");
        li.id = listId + "-extra-" + ++extras;
        li.setAttribute("role", "option");
        li.setAttribute("aria-selected", "false");
        const name = el("span", "rst-combo-name");
        name.textContent = row.label;
        li.append(name);
        if (row.meta) {
          const meta = el("span", "rst-combo-desc");
          meta.textContent = row.meta;
          li.append(meta);
        }
        list.appendChild(li);
        // index -1, so markSelected can never call one selected.
        opts.push({ text: row.label, name: row.label, desc: row.meta || "", short: "", lead: "",
          terms: [], first: false, disabled: false, index: -1, group: null, el: null, li, run: row.run });
      }
      shown = opts;
      if (open) filter(query());
    };

    // A write from outside (reset, autofill, a sibling script) dispatches
    // "change", and the display follows. choose()'s own lands as a no-op.
    native.addEventListener("change", () => {
      showText();
      showPick();
      markSelected();
      input.setCustomValidity("");
      settleValidity();
    });

    // A REQUIRED select still validates — it is still what the form posts —
    // but it is out of sight and aria-hidden, so the browser's "please pick
    // one" would focus a control nobody can see. The focus it gives the
    // select is handed to the box, which BORROWS the select's message for
    // the moment it is shown and gives it straight back: left on the box, it
    // would keep the form refusing after the select stopped being required.
    const settleValidity = () => {
      if (!native.validity.valid && native.willValidate) return;
      // Valid now, or no longer validated at all (a sibling script
      // dropped required, or disabled it): the borrowed message goes too,
      // or it would keep the form refusing a select that no longer objects.
      input.removeAttribute("aria-invalid");
      input.setCustomValidity("");
    };
    native.addEventListener("invalid", () => input.setAttribute("aria-invalid", "true"));
    // "true", not an empty value: an ARIA boolean is a string.
    const settleRequired = () => {
      if (native.required) input.setAttribute("aria-required", "true");
      else input.removeAttribute("aria-required");
    };
    settleRequired();
    // A sibling script may stop the select being required (or start it)
    // without touching it otherwise. That changes which blanks are prompts,
    // and so what the box shows and which row is selected.
    new MutationObserver(() => {
      settleRequired();
      settleValidity();
      for (const o of opts) {
        if (!o.run) o.prompt = isPrompt(o.el.value, o.marked, native.required, o.el.disabled);
      }
      markSelected();
      // An open list re-reads its rows: a blank that just became a prompt
      // must leave it, and must not stay the highlight.
      if (open) {
        if (showingPick) showText();
        filter(query());
      } else {
        showText();
        showPick();
      }
    }).observe(native, { attributes: true, attributeFilter: ["required", "disabled"] });
    native.addEventListener("focus", () => {
      input.focus();
      if (!native.validity.valid) {
        input.setCustomValidity(native.validationMessage);
        input.reportValidity();
      }
    });
    input.addEventListener("input", () => input.setCustomValidity(""));
    input.addEventListener("blur", () => {
      input.setCustomValidity("");
      settleValidity();
    });
  }

  // Idempotent, so re-scanning is safe. A select arriving later inside a
  // polled fragment stays native — correct, not enhanced.
  function scan() {
    document.querySelectorAll("select[data-rst-select]").forEach((s) => {
      // data-rst-select="false" is the markup-side opt-out; to CSS the
      // attribute is simply present, so it is checked here.
      if (s.dataset.rstSelect !== "false" && !s.multiple) combo(s);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
})();
