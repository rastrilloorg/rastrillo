// signin.mjs — the sign-in screen's passkey door. ui's signin partial
// loads it with <script type="module"> only when it renders a passkey
// door, so no other page of an app ever requests it. The app serves it
// from passkey.JS() at the URL it gives auth.PasskeyDoor.ScriptURL.
//
// The door ships hidden, and probe.js, which runs before the first
// paint, shows it where WebAuthn exists. This module does not show it:
// a door shown late moved the email form's Continue under a person's
// tap (rastrillo discussion #433). The one exception is a page whose
// app has not added signin-head since upgrading, where the probe never
// ran: there the module shows the door late, as it always did, rather
// than lose it, and says on the console what to add. A press is kept until the
// app's webauthn module has loaded, and where that module fails or says
// credentials are unavailable, the press says passkeys do not work here
// (data-rst-passkey-unavailable).
//
// On the one-way screen the door is the page's only way in, so the
// unavailable words also open the email way (data-rst-passkey-fallback),
// and a browser with no WebAuthn, whose door stays hidden, goes there at
// once. The same screen's email field may offer passkeys in its
// autofill (data-rst-passkey-conditional), which fails silently: the
// field works without it.
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
// body to act on. signal, when given, aborts the request in flight.
export async function postJSON(url, body, signal) {
  const res = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
    signal,
  });
  if (!res.ok) throw new Error("status " + res.status);
  return res.json();
}

// ceremony runs one passkey sign-in with its effects passed in, so Node
// can drive every branch. It never throws: it resolves to {navigate} —
// only when finish answered ok:true — or to {message}, "cancelled" or
// "failed". A failed answer must never navigate as if it had worked.
//
// signal, when given, ends the attempt: it is handed to each request
// and the prompt, and once it is aborted the ceremony resolves to {}
// before anything more is sent, because the person has moved on.
export async function ceremony({ post, authenticate, begin, finish, rpId, legacyRpId, loc, signal }) {
  const ended = () => !!(signal && signal.aborted);
  let a;
  try {
    if (ended()) return {};
    const b = await post(begin, {}, signal);
    if (ended()) return {};
    if (!b || typeof b.challenge !== "string") return { message: "failed" };
    a = await authenticate({ challenge: b.challenge, rpId, legacyRpId, signal });
  } catch (err) {
    if (ended()) return {};
    // "no passkey was offered" is webauthn.mjs's one message for a
    // dismissed prompt and for no passkey at all — the same on purpose,
    // so a site cannot probe for credentials — and it gets the gentler
    // words.
    return { message: err && err.message === "no passkey was offered" ? "cancelled" : "failed" };
  }
  if (ended()) return {};
  try {
    // authenticate() names the credential credentialId; the server reads
    // it as id. prf is never sent: it is a client-side secret.
    const done = await post(finish, {
      id: a.credentialId,
      clientDataJSON: a.clientDataJSON,
      authenticatorData: a.authenticatorData,
      signature: a.signature,
    }, signal);
    if (ended()) return {};
    if (!done || done.ok !== true) return { message: "failed" };
    return { navigate: safeNext(done.to, loc) };
  } catch (err) {
    return { message: "failed" };
  }
}

// busy draws a button as busy, or not; the add page's register.mjs
// draws its button the same way.
export function busy(btn, on) {
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

// conditional runs one autofill sign-in with its effects passed in, so
// Node can drive every branch. It never throws and never has a message:
// it resolves to {navigate} after an ok:true finish, and to {} otherwise.
// The autofill request can outlive the challenge begin minted (five
// minutes, or another tab's begin replacing it), so a refused finish
// begins again and retries once, with a new challenge and a new pick.
//
// Sending the form cancels it (signal), and a cancelled attempt sends
// nothing more: it checks before each request and after the
// authenticator answers, and hands the signal to each request, because
// a finish that lands after the form was sent would set a session or
// end the attempt while the person signs in with what they typed.
export async function conditional({ post, authenticate, begin, finish, rpId, loc, signal }) {
  const stopped = () => !!(signal && signal.aborted);
  for (let tries = 0; tries < 2; tries++) {
    let a;
    try {
      if (stopped()) return {};
      const b = await post(begin, {}, signal);
      if (stopped() || !b || typeof b.challenge !== "string") return {};
      a = await authenticate({ challenge: b.challenge, rpId, mediation: "conditional", signal });
    } catch (err) {
      return {};
    }
    if (stopped()) return {};
    try {
      const done = await post(finish, {
        id: a.credentialId,
        clientDataJSON: a.clientDataJSON,
        authenticatorData: a.authenticatorData,
        signature: a.signature,
      }, signal);
      if (stopped()) return {};
      if (done && done.ok === true) return { navigate: safeNext(done.to, loc) };
    } catch (err) {
      // A refused finish answers non-2xx: try once more below.
    }
  }
  return {};
}

// unavailable says passkeys do not work in this browser, and on the
// one-way door moves to the email way.
function unavailable(btn, msg) {
  if (msg) msg.textContent = btn.getAttribute("data-rst-passkey-unavailable") || "";
  fallBack(btn);
}

function fallBack(btn) {
  const to = btn.getAttribute("data-rst-passkey-fallback");
  if (to && localPath(to)) location.replace(to);
}

function door(btn) {
  const msg = document.getElementById(btn.getAttribute("aria-describedby") || "");
  if (!window.PublicKeyCredential) {
    fallBack(btn);
    return;
  }
  const loaded = import(btn.getAttribute("data-rst-passkey-module")).then(
    (m) => (m && m.available && m.available() ? m : null),
    () => null,
  );
  if (!probed()) {
    loaded.then((mod) => {
      if (mod) btn.hidden = false;
      else unavailable(btn, msg);
    });
  }
  const press = async () => {
    // A second click while one ceremony runs would start another, and
    // the first one's answer would land on a page that has moved on.
    if (btn.getAttribute("aria-busy") === "true") return;
    const signal = startAttempt(btn);
    busy(btn, true);
    if (msg) msg.textContent = "";
    const mod = await loaded;
    if (signal.aborted) {
      idle(btn, signal);
      return;
    }
    if (!mod) {
      idle(btn, signal);
      unavailable(btn, msg);
      return;
    }
    const out = await ceremony({
      post: postJSON,
      authenticate: mod.authenticate,
      begin: btn.getAttribute("data-rst-passkey-begin"),
      finish: btn.getAttribute("data-rst-passkey-finish"),
      rpId: location.hostname,
      legacyRpId: btn.getAttribute("data-rst-passkey-legacy-rpid") || undefined,
      loc: location,
      signal,
    });
    if (signal.aborted) {
      idle(btn, signal);
      return;
    }
    if (out.navigate) {
      location.assign(out.navigate);
      return;
    }
    idle(btn, signal);
    if (msg) msg.textContent = btn.getAttribute("data-rst-passkey-" + out.message) || "";
  };
  claim(btn, press);
}

// probed is whether probe.js ran: it marks <html> where WebAuthn
// exists, and a module only asks once it has found WebAuthn too.
export function probed() {
  return document.documentElement.hasAttribute("data-rst-passkeys");
}

// claim takes over a button from probe.js: from here this module hears
// its clicks, and a press probe.js kept runs now. As in the probe, only
// a person's click counts: a script's click() must not start a
// ceremony. Without the probe the button shows late, which only the
// console can say.
export function claim(btn, press) {
  if (!probed()) {
    console.error("rastrillo: passkey buttons need passkey.ProbeJS() in <head>; see ui's signin-head");
  }
  btn.addEventListener("click", (event) => {
    if (event.isTrusted) press();
  });
  btn.rstPasskeyClaimed = true;
  if (btn.rstPasskeyPress) {
    btn.rstPasskeyPress = false;
    press();
  }
}

// attempt is the page's one passkey attempt in flight: a press on any
// passkey button starts it, ending the last, and sending a form or
// leaving the page ends it, so a press made before the person chose
// another way, or before they went back, never runs once its module
// arrives.
let attempt = null;

// owners maps each button to the attempt that last made it busy. A
// press cancelled by leaving the page can resume after the person came
// back and pressed again; its end must not mark the button idle while
// the newer press runs, or a third press would start it over.
const owners = new WeakMap();

export function startAttempt(btn) {
  endAttempt();
  attempt = new AbortController();
  owners.set(btn, attempt.signal);
  return attempt.signal;
}

// idle draws btn as not busy, if signal's attempt still owns it.
export function idle(btn, signal) {
  if (owners.get(btn) === signal) busy(btn, false);
}

function endAttempt() {
  if (attempt) attempt.abort();
  attempt = null;
}

// autofill offers passkeys in the email field's autofill. It is aborted
// the moment the form is sent, so a typed address never races a passkey
// picked a moment earlier. Every failure is silent.
function autofill(form) {
  if (!window.PublicKeyCredential || typeof AbortController === "undefined") return;
  const ctl = new AbortController();
  form.addEventListener("submit", () => ctl.abort(), { once: true });
  import(form.getAttribute("data-rst-passkey-module")).then(async (m) => {
    if (!m || !m.conditionalAvailable || !(await m.conditionalAvailable())) return;
    const out = await conditional({
      post: postJSON,
      authenticate: m.authenticate,
      begin: form.getAttribute("data-rst-passkey-begin"),
      finish: form.getAttribute("data-rst-passkey-finish"),
      rpId: location.hostname,
      loc: location,
      signal: ctl.signal,
    });
    if (out.navigate && !ctl.signal.aborted) location.assign(out.navigate);
  }, () => {});
}

// A module runs after the document is parsed, so the door is already
// there. Node has no document and stops here.
if (typeof document !== "undefined") {
  document.querySelectorAll("[data-rst-passkey]").forEach(door);
  document.querySelectorAll("form[data-rst-passkey-conditional]").forEach(autofill);
  document.addEventListener("submit", endAttempt, true);
  window.addEventListener("pagehide", endAttempt);
  // A sign-in navigates away mid-busy; the back-forward cache restores
  // the page exactly as it was left, spinner and all.
  window.addEventListener("pageshow", (e) => {
    if (e.persisted) document.querySelectorAll("[data-rst-passkey][aria-busy]").forEach((b) => busy(b, false));
  });
}
