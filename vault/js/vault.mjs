// The vault twin: the browser's half of the restore handoff, over the
// same WebCrypto module the Go side's crypto package twins. Serve it
// beside crypto.mjs — the sibling import is the deployment contract
// (keyring's pattern; see the Go package doc in rastrillo/vault).
//
// The enrol direction needs no crypto here: the instance's enrol
// handler answers a complete enrol_url whose fragment already carries
// the payload — the browser only navigates. This module exists for
// the restore direction, where an ephemeral keypair must be minted on
// this side and the home's answer opened with it.
import { generate, open, b64, unb64 } from "./crypto.mjs";

// restoreContext pins the domain-separation string, byte-identical to
// the Go side's restoreContext. Changing it strands every home.
const restoreContext = "rastrillo/vault/restore/v1";

// restoreRequest mints the ephemeral keypair and nonce for one
// restore round trip and returns the fragment payload to carry to the
// home's restore page. Keep `keypair` in sessionStorage-scoped state
// for the return leg; it lives exactly as long as the round trip.
export async function restoreRequest(ret) {
  const keypair = await generate();
  const nonce = b64url(crypto.getRandomValues(new Uint8Array(16)));
  return {
    req: { v: 1, ret, nonce, eph_pub: b64(keypair.boxPub) },
    keypair,
  };
}

// openRestoreReturn opens the home's sealed answer and verifies the
// nonce; a stale, replayed, or tampered return rejects.
export async function openRestoreReturn(keypair, nonce, sealed) {
  const plain = await open(keypair, restoreContext, sealed);
  const rr = JSON.parse(new TextDecoder().decode(plain));
  if (rr.nonce !== nonce || !rr.token) {
    throw new Error("vault: restore return failed its nonce or carried no token");
  }
  return rr.token;
}

// encodeFragment / decodeFragment move a payload through a URL
// fragment: base64url over UTF-8 JSON, no padding — fragments never
// reach a server, which is the whole reason they carry the secrets.
export function encodeFragment(obj) {
  return b64url(new TextEncoder().encode(JSON.stringify(obj)));
}

export function decodeFragment(s) {
  if (!/^[A-Za-z0-9_-]+$/.test(s)) {
    throw new Error("vault: fragment is not base64url");
  }
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  const raw = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const bytes = Uint8Array.from(raw, (c) => c.charCodeAt(0));
  return JSON.parse(new TextDecoder().decode(bytes));
}

// b64url is unpadded base64url over bytes.
function b64url(bytes) {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
