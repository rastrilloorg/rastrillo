// signin.mjs — the sign-in screen's passkey door. ui's signin partial
// loads it with <script type="module"> only when it renders a passkey
// door, so no other page of an app ever requests it. The app serves it
// from passkey.JS() at the URL it gives auth.PasskeyDoor.ScriptURL.
//
// The door ships hidden. It is revealed only where a ceremony can run —
// PublicKeyCredential exists, the app's webauthn module loads, and the
// module says credentials are available — because a passkey button that
// cannot work is a dead end on the one screen that must never have one.
// The email form beside it works with or without any of this.
//
// Busy is drawn as rastrillo.js's busy rule draws it (aria-busy and an
// rst-spin child, which tokens.css keys the spinner on) plus
// aria-disabled, and never with disabled: a disabled button drops
// focus, and a failure is announced to someone whose focus has to still
// be on the button to try again.

// localPath is sessions.SafeReturn's rule: exactly one leading "/", no
// backslash, no control character — browsers strip tab/CR/LF before
// parsing, so "/\t/evil.example" would otherwise resolve off-site.
export function localPath(to) {
  return typeof to === "string" && to.charAt(0) === "/" && to.charAt(1) !== "/" &&
    to.indexOf("\\") === -1 && !/[\u0000-\u001f\u007f]/.test(to);
}

// safeNext is where a successful sign-in may send the page: the
// server's "to" when it is a local path AND parses to this origin, "/"
// otherwise. The server never sends an absolute URL, so accepting even
// a same-origin one would only widen what a bad answer could do; the
// parsed-origin check is the second line, for whatever a future edit to
// localPath lets through.
export function safeNext(to, loc) {
  if (!localPath(to)) return "/";
  try {
    return new URL(to, loc.href).origin === loc.origin ? to : "/";
  } catch (e) {
    return "/";
  }
}

// postJSON is a same-origin POST, so csrf.Protect sees
// Sec-Fetch-Site: same-origin. A non-2xx answer is an error, never a
// body to act on.
export async function postJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  if (!res.ok) throw new Error("status " + res.status);
  return res.json();
}

// ceremony runs one passkey sign-in with its effects passed in, so Node
// can drive every branch. It never throws: it resolves to {navigate} —
// only when finish answered ok:true — or to {message}, "cancelled" or
// "failed". A failed answer must never navigate as if it had worked.
export async function ceremony({ post, authenticate, begin, finish, rpId, legacyRpId, loc }) {
  let a;
  try {
    const b = await post(begin, {});
    if (!b || typeof b.challenge !== "string") return { message: "failed" };
    a = await authenticate({ challenge: b.challenge, rpId, legacyRpId });
  } catch (err) {
    // "no passkey was offered" is webauthn.mjs's one message for a
    // dismissed prompt and for no passkey at all — the same on purpose,
    // so a site cannot probe for credentials — and it gets the gentler
    // words.
    return { message: err && err.message === "no passkey was offered" ? "cancelled" : "failed" };
  }
  try {
    // authenticate() names the credential credentialId; the server reads
    // it as id. prf is never sent: it is a client-side secret.
    const done = await post(finish, {
      id: a.credentialId,
      clientDataJSON: a.clientDataJSON,
      authenticatorData: a.authenticatorData,
      signature: a.signature,
    });
    if (!done || done.ok !== true) return { message: "failed" };
    return { navigate: safeNext(done.to, loc) };
  } catch (err) {
    return { message: "failed" };
  }
}

function busy(btn, on) {
  let spin = btn.querySelector("[rst-spin]");
  if (!on) {
    btn.removeAttribute("aria-busy");
    btn.removeAttribute("aria-disabled");
    if (spin) spin.remove();
    return;
  }
  btn.setAttribute("aria-busy", "true");
  btn.setAttribute("aria-disabled", "true");
  if (!spin) {
    spin = document.createElement("span");
    spin.setAttribute("rst-spin", "");
    spin.setAttribute("aria-hidden", "true");
    btn.insertBefore(spin, btn.firstChild);
  }
}

function door(btn) {
  if (!window.PublicKeyCredential) return;
  const msg = document.getElementById(btn.getAttribute("aria-describedby") || "");
  let mod = null;
  import(btn.getAttribute("data-rst-passkey-module")).then((m) => {
    if (m && m.available && m.available()) {
      mod = m;
      btn.hidden = false; // revealing never moves focus
    }
  }, () => {});
  btn.addEventListener("click", async () => {
    // A second click while one ceremony runs would start another, and
    // the first one's answer would land on a page that has moved on.
    if (!mod || btn.getAttribute("aria-busy") === "true") return;
    busy(btn, true);
    if (msg) msg.textContent = "";
    const out = await ceremony({
      post: postJSON,
      authenticate: mod.authenticate,
      begin: btn.getAttribute("data-rst-passkey-begin"),
      finish: btn.getAttribute("data-rst-passkey-finish"),
      rpId: location.hostname,
      legacyRpId: btn.getAttribute("data-rst-passkey-legacy-rpid") || undefined,
      loc: location,
    });
    if (out.navigate) {
      location.assign(out.navigate);
      return;
    }
    busy(btn, false);
    if (msg) msg.textContent = btn.getAttribute("data-rst-passkey-" + out.message) || "";
  });
}

// A module runs after the document is parsed, so the door is already
// there. Node has no document and stops here.
if (typeof document !== "undefined") {
  document.querySelectorAll("[data-rst-passkey]").forEach(door);
  // A sign-in navigates away mid-busy; the back-forward cache restores
  // the page exactly as it was left, spinner and all.
  window.addEventListener("pageshow", (e) => {
    if (e.persisted) document.querySelectorAll("[data-rst-passkey][aria-busy]").forEach((b) => busy(b, false));
  });
}
