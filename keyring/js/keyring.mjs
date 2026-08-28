// rastrillo/keyring's JS twin: the seed lifecycle over the crypto
// twin's primitives, byte-compatible with the Go package one directory
// up. It imports ./crypto.mjs rather than inlining it — apps serve
// crypto.JS() and keyring.JS() as siblings under one mount, and that
// import is the deployment contract written down. Zero other
// dependencies; runs in any browser and in Node ≥ 20.
//
// ring("kass") reproduces kass's strings and bytes exactly: WebCrypto's
// zero-length HKDF salt ≡ Go's nil salt (RFC 5869 — HMAC zero-pads
// either to the block size), so kass's stored wrapped seeds unwrap
// unchanged.
//
// One trade said out loud: contentKey and wrapKey return raw key bytes
// (Uint8Array), trading kass's non-extractable-CryptoKey hygiene for
// wrappability — a grant cannot wrap a key it cannot read.
//
// All byte parameters and results are Uint8Array. A member private key
// is pkcs8 bytes (Uint8Array) or a JWK object — the ECDH-only import
// the app already holds.

import { derive, sealSym, openSym, seal, open, newKey } from "./crypto.mjs";

// newSeed mints a fresh 32-byte seed — the one per-person root every
// content key derives from. It rides crypto.mjs's newKey, as the Go
// side rides crypto.NewKey: one primitive per side, no copies.
export function newSeed() {
  return newKey();
}

// ring namespaces every keyring operation for one app; every context
// string is derived from the namespace, so two apps can never collide.
export function ring(namespace) {
  const grantContext = namespace + "/grant/v1";
  const wrapContext = namespace + "/wrap/v1";
  return {
    // prfSalt is the WebAuthn PRF-extension eval input for this ring.
    prfSalt: () => namespace + "/prf/v1",

    // contentKey derives the ring's content key from the seed (raw
    // bytes — see the module comment for the extractability trade).
    contentKey: (seed) => derive(seed, namespace + "/content/v1"),

    // blobKey derives the sealing key for one named vault blob —
    // ns/blob/<name>/v1. Callers validate the name first (the vault
    // client's closed namespace); the ring derives what it is given.
    blobKey: (seed, name) => derive(seed, namespace + "/blob/" + name + "/v1"),

    // wrapKey derives the seed-wrapping key from a credential's PRF
    // output, consumed directly from the PRF extension result.
    wrapKey: (prf) => derive(prf, wrapContext),

    // wrapSeed seals the seed under the credential's PRF output:
    // iv(12) ‖ AES-256-GCM ct. The same seed under a new credential's
    // PRF output is device add and RPID move entire.
    wrapSeed: async (prf, seed) => sealSym(await derive(prf, wrapContext), seed),

    // unwrapSeed reverses wrapSeed; a wrong credential, wrong
    // namespace and tampered blob reject indistinguishably.
    unwrapSeed: async (prf, wrapped) => openSym(await derive(prf, wrapContext), wrapped),

    // grant wraps a content key to a member's 65-byte box public
    // point: ephPub(65) ‖ iv(12) ‖ ct. One key, one instance, never
    // the seed.
    grant: (memberBoxPub, contentKey) => seal(memberBoxPub, grantContext, contentKey),

    // openGrant opens a grant with the member's private key — pkcs8
    // bytes or a JWK, the ECDH-only import kass already holds.
    openGrant: async (memberBoxPriv, sealed) =>
      open({ boxPrivJwk: await asBoxJwk(memberBoxPriv) }, grantContext, sealed),
  };
}

// asBoxJwk normalises a member private key to the JWK shape the crypto
// twin's open consumes: pkcs8 bytes go through a WebCrypto
// import/export round trip (extractable only inside this function),
// and JWKs pass through untouched.
async function asBoxJwk(priv) {
  if (priv instanceof Uint8Array || priv instanceof ArrayBuffer) {
    const key = await crypto.subtle.importKey(
      "pkcs8", priv, { name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
    return crypto.subtle.exportKey("jwk", key);
  }
  return priv;
}
