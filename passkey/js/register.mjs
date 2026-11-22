// register.mjs — the passkey add page's button. ui's signin-add partial
// loads it with <script type="module"> only on that page. The app
// serves it from passkey.RegisterJS(), with passkey.JS() beside it as
// signin.mjs, whose safeNext and busy it imports.
//
// The button ships disabled, keeping its place, and is enabled only
// where a ceremony can run: PublicKeyCredential exists and the app's
// webauthn module (data-rst-passkey-module) loads and says so. Where
// one cannot, the message line says the passkey did not work, so the
// page is never a button that does nothing.
//
// The session can go stale between the page loading and the button
// being pressed. Then begin or finish answers 401 naming the sign-in
// page ("reauth"), which returns here, and the page goes there.
//
// The passkey's user handle is random, made here per passkey: it is
// stored on the device and must not carry the address, which the
// device shows as the passkey's name instead (data-rst-passkey-user).
import { safeNext, busy } from "./signin.mjs";

// postOrReauth is signin.mjs's postJSON, plus one answer of its own: a
// 401 naming a sign-in page throws an error carrying it as reauth. Any
// other non-2xx answer is an error, never a body to act on.
export async function postOrReauth(url, body) {
  const res = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  if (res.status === 401) {
    const answer = await res.json().catch(() => null);
    if (answer && typeof answer.reauth === "string") {
      const err = new Error("reauth");
      err.reauth = answer.reauth;
      throw err;
    }
  }
  if (!res.ok) throw new Error("status " + res.status);
  return res.json();
}

// userId is a fresh random user handle, base64url without padding.
function userId() {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// enrol runs one passkey creation with its effects passed in, so Node
// can drive every branch. It never throws: it resolves to {navigate}
// when finish answered ok:true or a request named a sign-in page to
// go to first, else to {message: "failed"}. A failure must never
// navigate as if it had worked.
export async function enrol({ post, register, begin, finish, rpId, rpName, userName, loc }) {
  try {
    const b = await post(begin, {});
    if (!b || typeof b.challenge !== "string") return { message: "failed" };
    const created = await register({
      challenge: b.challenge, rpId, rpName, userId: userId(), userName,
    });
    // The prf output, if any, is a client-side secret: never sent.
    const done = await post(finish, {
      clientDataJSON: created.clientDataJSON,
      attestationObject: created.attestationObject,
    });
    if (!done || done.ok !== true) return { message: "failed" };
    return { navigate: safeNext(done.to, loc) };
  } catch (err) {
    if (err && typeof err.reauth === "string") {
      return { navigate: safeNext(err.reauth, loc) };
    }
    return { message: "failed" };
  }
}

function button(btn) {
  const msg = document.getElementById(btn.getAttribute("aria-describedby") || "");
  const failed = () => {
    if (msg) msg.textContent = btn.getAttribute("data-rst-passkey-failed") || "";
  };
  if (!window.PublicKeyCredential) {
    failed();
    return;
  }
  let mod = null;
  import(btn.getAttribute("data-rst-passkey-module")).then((m) => {
    if (m && m.available && m.available()) {
      mod = m;
      btn.disabled = false;
    } else {
      failed();
    }
  }, failed);
  btn.addEventListener("click", async () => {
    if (!mod || btn.getAttribute("aria-busy") === "true") return;
    busy(btn, true);
    if (msg) msg.textContent = "";
    const out = await enrol({
      post: postOrReauth,
      register: mod.register,
      begin: btn.getAttribute("data-rst-passkey-begin"),
      finish: btn.getAttribute("data-rst-passkey-finish"),
      rpId: location.hostname,
      rpName: btn.getAttribute("data-rst-passkey-rp-name") || location.hostname,
      userName: btn.getAttribute("data-rst-passkey-user") || "",
      loc: location,
    });
    if (out.navigate) {
      location.assign(out.navigate);
      return;
    }
    busy(btn, false);
    failed();
  });
}

// A module runs after the document is parsed, so the button is
// already there. Node has no document and stops here.
if (typeof document !== "undefined") {
  document.querySelectorAll("[data-rst-passkey-register]").forEach(button);
  window.addEventListener("pageshow", (e) => {
    if (e.persisted) document.querySelectorAll("[data-rst-passkey-register][aria-busy]").forEach((b) => busy(b, false));
  });
}
