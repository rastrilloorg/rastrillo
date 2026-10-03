// Wires protected forms to the solver.
//
// The submit is rendered disabled, with a <noscript> line and a status
// line beside it, and this module enables it once the form is wired.
// That order is the only one that fails safe: JavaScript cannot enable
// a control inside <noscript>, and a module that is blocked, missing or
// throws leaves the honest disabled state and the status line, rather
// than a form that looks live and does nothing.
//
// Markup, which pow.Form renders:
//
//   form[data-pow-form]  nonce, difficulty, worker URL, min age, [bound]
//   [data-pow-counter]   the hidden input the solution is written into
//   [data-pow-submit]    submit controls, rendered disabled; found through
//                        form.elements, so form= controls outside count
//   [data-pow-status]    visible until ready; shown again on failure
//   [data-pow-binding]   bound forms only: the input the work is tied to
//
// The module does no hashing itself; the worker imports sha256.js.

import { asciiLower } from "./powcore.js";

// Every wired form's state. The page's own forms, so holding them is
// free; disconnected ones are dropped on the next init.
const states = new Map();

// init wires every protected form under root. Idempotent per form. It
// runs once when the module first evaluates; a page that replaces its
// document (document.open/write) must call it again, because a module
// URL that has already run is never evaluated a second time.
export function init(root = document) {
  // document.open erases every listener on the document and the window,
  // so a page that replaced its document has lost these too. Adding the
  // same function twice is a no-op, so this is safe on every call.
  addEventListener("pagehide", onPageHide);
  addEventListener("pageshow", onPageShow);
  for (const [form, st] of states) {
    if (!form.isConnected) {
      endHold(st);
      stopWorker(st, new Error("pow: the form was removed"));
      states.delete(form);
    }
  }
  for (const form of root.querySelectorAll("form[data-pow-form]")) setup(form);
}

// whenSolved resolves with the pow_* fields a request on this form's
// behalf must carry for Guard.Verify. It never stays pending: it
// rejects on worker failure, when the page is left, and after timeout.
// A form that is not a proof form resolves at once with its fields; a
// bound form rejects, because a bound proof cannot be verified without
// the value it is bound to.
export function whenSolved(form, { timeout = 10000 } = {}) {
  if (!form.hasAttribute("data-pow-form")) return Promise.resolve({ fields: fieldsFrom(form, null) });
  const st = states.get(form);
  if (!st) return Promise.reject(new Error("pow: this form is not initialised"));
  if (st.bound) return Promise.reject(new Error("pow: a bound proof cannot be verified on its own"));
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("pow: timed out")), timeout);
    solve(st, "").then(
      () => { clearTimeout(timer); resolve({ fields: fieldsOf(st) }); },
      (err) => { clearTimeout(timer); reject(err); },
    );
  });
}

function setup(form) {
  if (states.has(form)) return;
  const d = form.dataset;
  const bound = form.hasAttribute("data-pow-bound");
  const st = {
    form,
    bound,
    nonce: d.powNonce,
    difficulty: parseInt(d.powDifficulty, 10),
    workerURL: d.powWorker,
    readyAt: performance.now() + (parseInt(d.powMinAge || "0", 10) || 0),
    counter: form.querySelector("[data-pow-counter]"),
    status: form.querySelector("[data-pow-status]"),
    binding: bound ? form.querySelector("[data-pow-binding]") : null,
    submits: Array.from(form.elements).filter((el) => el.matches("[data-pow-submit]")),
    worker: null,
    solving: null,
    solution: null,
    held: null,
    releasing: false,
  };
  // A form missing a piece is a wiring bug. Enabling its submit would
  // post something the server is certain to refuse; leave it disabled,
  // with the status line showing, and say so where a developer looks.
  if (!st.nonce || !st.workerURL || !st.counter || !(st.difficulty > 0) ||
      st.submits.length === 0 || (bound && !st.binding)) {
    console.error("pow.js: form is missing a required element or attribute", form);
    return;
  }
  states.set(form, st);
  form.addEventListener("submit", (e) => onSubmit(st, e));
  for (const b of st.submits) b.disabled = false;
  if (st.status) st.status.hidden = true;
  form.setAttribute("data-pow-ready", "");
  // Unbound work can start now, while the visitor reads and types, so
  // most never wait at all.
  if (!bound) solve(st, "").catch(() => {});
}

// A solution belongs to one binding value, normalised exactly as
// powcore does. Autofill or a correction while the worker runs changes
// the value; releasing a proof for the old one would be refused as
// pow_short with nothing the visitor can read.
function keyOf(value) {
  return asciiLower(value.trim());
}

function solve(st, value) {
  const key = keyOf(value);
  if (st.solution && st.solution.key === key) return Promise.resolve(st.solution.counter);
  if (st.solving && st.solving.key === key) return st.solving.promise;
  stopWorker(st, new Error("pow: superseded"));
  let settle;
  const promise = new Promise((resolve, reject) => { settle = { resolve, reject }; });
  st.solving = { key, promise, settle };
  try {
    const worker = new Worker(st.workerURL, { type: "module" });
    st.worker = worker;
    worker.onmessage = (e) => {
      if (!e.data.done) return;
      const s = st.solving;
      stopWorker(st, null);
      st.solution = { key, counter: e.data.counter };
      s.settle.resolve(e.data.counter);
      st.form.dispatchEvent(new CustomEvent("pow:solved", { detail: { fields: fieldsOf(st) } }));
    };
    worker.onerror = (e) => {
      e.preventDefault();
      failed(st, new Error("pow: the worker failed"));
    };
    worker.postMessage({ nonce: st.nonce, binding: value, difficulty: st.difficulty });
  } catch (err) {
    // The constructor itself can throw (a CSP worker-src, a bad URL).
    failed(st, err);
  }
  return promise;
}

function stopWorker(st, reason) {
  if (st.worker) {
    st.worker.terminate();
    st.worker = null;
  }
  const s = st.solving;
  st.solving = null;
  if (s && reason) s.settle.reject(reason);
}

function failed(st, err) {
  stopWorker(st, err);
  endHold(st);
  if (st.status) st.status.hidden = false;
  st.form.dispatchEvent(new CustomEvent("pow:failed", { detail: { error: err } }));
}

// Every submit of a protected form goes out through release, even one
// with nothing left to wait for (then the hold lasts one tick). One
// path means one owner: busy.js steps aside for a pow form until pow
// releases it, so the two never hold the same submit and never fight
// over the button. A submit cannot be re-sent from inside its own
// submit event, which is why even the zero wait is asynchronous.
function onSubmit(st, e) {
  if (st.releasing) return; // our own requestSubmit
  e.preventDefault();
  if (st.held) return; // a second click while held changes nothing
  const value = st.bound ? st.binding.value : "";
  if (st.bound && !value.trim()) {
    st.binding.reportValidity();
    return;
  }
  hold(st, e.submitter || null);
}

// hold keeps a submit until the solve and the minimum age are both done.
// A beforeunload listener exists only while holding: pagehide fires
// only once a navigation commits, and until then a solve finishing
// would submit over the navigation the visitor chose. Only while
// holding, so ordinary pages stay eligible for the back-forward cache.
// The feedback uses busy.js's vocabulary (aria-busy on the button, and
// data-busy-label on the button or the form) so an app styles one busy
// state, whichever script is holding.
function hold(st, submitter) {
  const onLeave = () => endHold(st);
  const label = submitter && (submitter.getAttribute("data-busy-label") || st.form.getAttribute("data-busy-label"));
  st.held = { submitter, onLeave, idle: label && submitter.tagName === "BUTTON" ? submitter.textContent : null };
  addEventListener("beforeunload", onLeave);
  if (submitter) {
    submitter.setAttribute("aria-busy", "true");
    if (st.held.idle !== null) submitter.textContent = label;
    // The same spinner busy.js draws, so a held pow button and a held
    // busy.js button look alike; aria-busy alone only changes the cursor.
    if (submitter.tagName === "BUTTON") {
      const spin = document.createElement("span");
      spin.setAttribute("rst-spin", "");
      spin.setAttribute("aria-hidden", "true");
      submitter.insertBefore(spin, submitter.firstChild);
      st.held.spin = spin;
    }
  }
  wait(st);
}

function wait(st) {
  const value = st.bound ? st.binding.value : "";
  const delay = Math.max(0, st.readyAt - performance.now());
  Promise.all([solve(st, value), new Promise((r) => setTimeout(r, delay))])
    .then(() => release(st), () => {});
}

function release(st) {
  const h = st.held;
  if (!h) return; // left, or failed, while waiting
  const value = st.bound ? st.binding.value : "";
  if (!st.solution || st.solution.key !== keyOf(value)) {
    wait(st); // the binding changed while the worker ran: solve again
    return;
  }
  endHold(st);
  // The submitter may have left the form during the hold (removed, or
  // its form= changed). Sending without it would drop its name and
  // value and its formaction/formmethod, and post somewhere the visitor
  // did not choose. Hand the form back instead, as busy.js does.
  if (h.submitter && (!h.submitter.isConnected || h.submitter.form !== st.form)) return;
  st.counter.value = st.solution.counter;
  const sub = h.submitter || undefined;
  const wasDisabled = sub ? sub.disabled : false;
  if (sub) sub.disabled = false;
  // busy.js reads this during the submit event requestSubmit fires
  // synchronously: it is the one submit of a pow form busy.js arms (the
  // spinner and the double-submit guard), and it never holds it.
  st.form.setAttribute("data-pow-released", "");
  st.releasing = true;
  try {
    st.form.requestSubmit(sub);
  } finally {
    st.releasing = false;
    st.form.removeAttribute("data-pow-released");
    if (sub) sub.disabled = wasDisabled;
  }
}

function endHold(st) {
  const h = st.held;
  if (!h) return;
  st.held = null;
  removeEventListener("beforeunload", h.onLeave);
  if (h.submitter) {
    h.submitter.removeAttribute("aria-busy");
    if (h.spin) h.spin.remove();
    if (h.idle !== null) h.submitter.textContent = h.idle;
  }
}

function fieldsFrom(form, counter) {
  const out = {};
  for (const el of form.querySelectorAll('input[type="hidden"][name^="pow_"]')) out[el.name] = el.value;
  if (counter !== null) out.pow_counter = counter;
  return out;
}

function fieldsOf(st) {
  return fieldsFrom(st.form, st.solution ? st.solution.counter : null);
}

// Leaving the page ends every hold and stops every worker; a timer or a
// solve surviving into the back-forward cache would otherwise submit
// on the visitor's return.
function onPageHide() {
  for (const st of states.values()) {
    endHold(st);
    stopWorker(st, new Error("pow: the page was left"));
  }
}

// The cache restores the DOM as it was left. Hand every form back and
// restart unfinished unbound work.
function onPageShow(e) {
  if (!e.persisted) return;
  for (const st of states.values()) {
    for (const b of st.submits) b.disabled = false;
    if (!st.bound && !st.solution) solve(st, "").catch(() => {});
  }
}

init(document);
