// reveal.mjs — the passphrase add page's "Show passphrase" switch. ui's
// signin-add partial loads it with <script type="module"> only on that
// page; the app serves it from signin.RevealJS().
//
// The switch ships hidden because nothing but this module can work it.
// Wired, it shows the row, starts off (a page restored from history may
// keep the box ticked), shows the field's text while it is on, and hides
// the text again as the form is sent, so the browser and any password
// manager see a password field going out.

// wire connects one switch (box) to its field and shows its row. The
// elements are passed in, so Node can drive it with stand-ins.
export function wire(box, field, row) {
  const show = (shown) => {
    field.type = shown ? "text" : "password";
  };
  box.checked = false;
  show(false);
  box.addEventListener("change", () => show(box.checked));
  if (field.form) field.form.addEventListener("submit", () => show(false));
  row.hidden = false;
}

// A module runs after the document is parsed, so the switch is already
// there. Node has no document and stops here.
if (typeof document !== "undefined") {
  document.querySelectorAll("[data-rst-reveal]").forEach((box) => {
    const field = document.getElementById(box.getAttribute("aria-controls") || "");
    const row = box.closest("[data-rst-reveal-row]");
    if (field && row) wire(box, field, row);
  });
}
