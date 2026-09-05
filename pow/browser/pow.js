// Wires a form to the solver.
//
// The submit button is rendered disabled, with an explanation inside
// <noscript>, and this module enables it once it has loaded. That
// ordering is the only one that fails safe: JavaScript cannot enable a
// control that lives inside <noscript>, and a module that throws on an
// old browser leaves the honest disabled state rather than a form that
// looks live and silently does nothing.
//
// A form behind this genuinely requires JavaScript — the proof of work
// is mandatory. A visitor who cannot run it deserves to learn that
// immediately, and from you.
//
// The markup contract, which pow.Challenge.Fields and
// pow.Challenge.FormAttrs render for you:
//
//	[data-pow-form]     the form, carrying nonce, difficulty, worker URL
//	[data-pow-binding]  the input the work is bound to (the address)
//	[data-pow-counter]  the hidden input the solution is written into
//	[data-pow-submit]   the submit button, rendered disabled
//	[data-pow-status]   optional, hidden: gets the attempt count
//
// This module does no hashing itself — the worker imports sha256.js.

const forms = document.querySelectorAll("[data-pow-form]");

for (const form of forms) {
  const submit = form.querySelector("[data-pow-submit]");
  const binding = form.querySelector("[data-pow-binding]");
  const counter = form.querySelector("[data-pow-counter]");
  const status = form.querySelector("[data-pow-status]");
  const workerURL = form.dataset.powWorker;
  const difficulty = parseInt(form.dataset.powDifficulty, 10);
  const nonce = form.dataset.powNonce;

  // A form missing a piece is a wiring bug, and enabling its submit
  // would post something the server is certain to refuse. Leave it
  // disabled and say so where a developer will see it.
  if (!submit || !binding || !counter || !workerURL || !nonce) {
    console.error("pow.js: form is missing a required element or attribute", form);
    continue;
  }

  const idleLabel = submit.dataset.label || submit.textContent;
  const workingLabel = submit.dataset.workingLabel || idleLabel;

  // Everything above resolved, so the form can be used.
  submit.disabled = false;

  let solved = false;

  form.addEventListener("submit", (event) => {
    if (solved) return; // second pass: the counter is in, let it through.

    event.preventDefault();
    if (!binding.value.trim()) {
      binding.reportValidity();
      return;
    }

    submit.disabled = true;
    submit.textContent = workingLabel;
    if (status) status.hidden = false;

    // The worker is addressed by a fingerprinted URL the template
    // supplies, because static assets are content-hashed and this
    // module cannot guess the name. Module type, so it can import
    // powcore.js the same way.
    const worker = new Worker(workerURL, { type: "module" });

    worker.onmessage = (e) => {
      if (!e.data.done) {
        if (status) status.textContent = String(e.data.tried);
        return;
      }
      worker.terminate();
      counter.value = e.data.counter;
      solved = true;
      form.requestSubmit ? form.requestSubmit() : form.submit();
    };

    worker.onerror = () => {
      worker.terminate();
      // Nothing clever to do: without a solution the server will refuse
      // the submission anyway, so say so here rather than posting
      // something we know will be rejected.
      submit.disabled = false;
      submit.textContent = idleLabel;
      if (status) status.textContent = "";
      form.dispatchEvent(new CustomEvent("pow:failed"));
    };

    worker.postMessage({
      nonce,
      binding: binding.value,
      difficulty,
    });
  });
}
