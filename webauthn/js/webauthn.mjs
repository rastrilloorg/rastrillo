// rastrillo/webauthn's browser half: navigator.credentials wrappers that
// produce exactly what the Go package verifies. Generalized from kass's
// proven lib/passkey.js — the acknowledged JS dependency, shipped as one
// reusable, dependency-free ES module instead of a per-app hand-copy.
//
// register() and authenticate() return plain objects of base64url
// strings, ready to POST as JSON to actions that call
// Config.Register / Config.Verify on the matching decoded bytes.
//
// PRF is opt-in: pass prfSalt (a stable string — its value must never
// change for the life of the deployment, or synced passkeys derive
// different keys) and the results carry the PRF output as `prf`
// (Uint8Array — a client-side secret; derive keys from it, never send
// it to the server). Authenticators without PRF throw NoPRF when a salt
// was asked for, saying so plainly rather than quietly falling back to
// something weaker.

const te = new TextEncoder();

export function toB64url(bytes) {
  let s = "";
  const arr = new Uint8Array(bytes);
  for (let i = 0; i < arr.length; i++) s += String.fromCharCode(arr[i]);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function fromB64url(s) {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  return arr;
}

export function available() {
  return typeof PublicKeyCredential !== "undefined" && !!navigator.credentials;
}

export class NoPRF extends Error {
  constructor() {
    super("this authenticator does not support the PRF extension");
    this.name = "NoPRF";
  }
}

function prfExtension(prfSalt) {
  return prfSalt ? { prf: { eval: { first: te.encode(prfSalt) } } } : {};
}

// register creates a passkey. challenge and userId are base64url strings
// (the server mints the challenge — 32 random bytes — and must keep it
// for the verify step).
//
// Some browsers refuse to return PRF output during creation, so where it
// is missing (and a salt was asked for) an assertion runs immediately to
// fetch it. Two prompts on those platforms — worse, but honest, and
// better than storing a credential that turns out to open nothing
// (kass's rule, kept).
export async function register({ challenge, rpId, rpName, userId, userName, prfSalt }) {
  const credential = await navigator.credentials.create({
    publicKey: {
      rp: { id: rpId, name: rpName || rpId },
      user: { id: fromB64url(userId), name: userName, displayName: userName },
      challenge: fromB64url(challenge),
      pubKeyCredParams: [{ type: "public-key", alg: -7 }], // ES256, the only alg the server verifies
      authenticatorSelection: { residentKey: "required", userVerification: "required" },
      extensions: prfExtension(prfSalt),
    },
  });
  if (!credential) throw new Error("no passkey was created");

  let prf;
  if (prfSalt) {
    prf = credential.getClientExtensionResults()?.prf?.results?.first;
    if (!prf) prf = await prfByAssertion(credential.rawId, prfSalt);
    prf = new Uint8Array(prf);
  }

  return {
    credentialId: toB64url(credential.rawId),
    clientDataJSON: toB64url(credential.response.clientDataJSON),
    attestationObject: toB64url(credential.response.attestationObject),
    prf,
  };
}

// authenticate runs a discoverable-credential assertion — one tap,
// nothing typed; the server learns who it was once the signature checks
// out.
//
// legacyRpId is set only while an instance changes hostname: a passkey
// made under the old name is invisible to a request for the new one (the
// platform offers nothing, without prompting), so asking again under the
// old name is what keeps existing members signing in across the move —
// the mirror of the Go Config.LegacyRPID, which accepts it for
// assertions and never for registration.
export async function authenticate({ challenge, rpId, legacyRpId, prfSalt }) {
  let assertion = await getAssertion(rpId, challenge, prfSalt);
  if (!assertion && legacyRpId && legacyRpId !== rpId) {
    assertion = await getAssertion(legacyRpId, challenge, prfSalt);
  }
  if (!assertion) throw new Error("no passkey was offered");

  let prf;
  if (prfSalt) {
    prf = assertion.getClientExtensionResults()?.prf?.results?.first;
    if (!prf) throw new NoPRF();
    prf = new Uint8Array(prf);
  }

  return {
    credentialId: toB64url(assertion.rawId),
    clientDataJSON: toB64url(assertion.response.clientDataJSON),
    authenticatorData: toB64url(assertion.response.authenticatorData),
    signature: toB64url(assertion.response.signature),
    prf,
  };
}

async function getAssertion(rpId, challenge, prfSalt) {
  try {
    return await navigator.credentials.get({
      publicKey: {
        rpId,
        challenge: fromB64url(challenge),
        allowCredentials: [],
        userVerification: "required",
        extensions: prfExtension(prfSalt),
      },
    });
  } catch (err) {
    // NotAllowedError covers both "nothing here" and "the person
    // dismissed the prompt" — deliberately indistinguishable so a site
    // cannot probe for credentials. Returning null lets the caller fall
    // through to the legacy relying party.
    if (err?.name === "NotAllowedError") return null;
    throw err;
  }
}

async function prfByAssertion(rawId, prfSalt) {
  const assertion = await navigator.credentials.get({
    publicKey: {
      challenge: crypto.getRandomValues(new Uint8Array(32)),
      allowCredentials: [{ type: "public-key", id: rawId }],
      userVerification: "required",
      extensions: prfExtension(prfSalt),
    },
  });
  const prf = assertion?.getClientExtensionResults()?.prf?.results?.first;
  if (!prf) throw new NoPRF();
  return prf;
}
