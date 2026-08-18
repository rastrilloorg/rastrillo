// rastrillo/crypto's JS twin: the family envelope in WebCrypto, byte-
// compatible with the Go package one directory up (and so with keymail's
// crypto.go, amadan's internal/envelope, seapointish's internal/seal).
// Zero dependencies; runs in any browser and in Node ≥ 20 (which carries
// WebCrypto, atob/btoa and TextEncoder as globals).
//
// Formats (see the Go package doc):
//   seal:    ephPub(65, uncompressed P-256 point) ‖ iv(12) ‖ AES-256-GCM ct
//   sign:    ECDSA P-256 over SHA-256(context ‖ 0x00 ‖ msg), raw r‖s (64)
//   sealSym: iv(12) ‖ AES-256-GCM ct
//   derive:  HKDF-SHA256, salt empty (≡ Go's nil salt: HMAC zero-pads
//            either to the same block), info=context, 32 bytes
//
// All byte parameters and results are Uint8Array. A keypair is
// {signPrivJwk, boxPrivJwk, signPub, boxPub} — JWKs for the private
// halves because WebCrypto cannot derive an EC public point from a bare
// scalar, raw 65-byte points for the public halves.

const te = new TextEncoder();

export function b64(bytes) {
  let s = "";
  const arr = new Uint8Array(bytes);
  for (let i = 0; i < arr.length; i++) s += String.fromCharCode(arr[i]);
  return btoa(s);
}

export function unb64(s) {
  const bin = atob(s);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  return arr;
}

function b64url(bytes) {
  return b64(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function unhex(s) {
  const arr = new Uint8Array(s.length / 2);
  for (let i = 0; i < arr.length; i++) arr[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return arr;
}

function concat(...parts) {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let off = 0;
  for (const p of parts) { out.set(p, off); off += p.length; }
  return out;
}

// generate creates a fresh dual keypair (ECDSA sign + ECDH box).
export async function generate() {
  const sign = await crypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" }, true, ["sign", "verify"]);
  const box = await crypto.subtle.generateKey(
    { name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
  return {
    signPrivJwk: await crypto.subtle.exportKey("jwk", sign.privateKey),
    boxPrivJwk: await crypto.subtle.exportKey("jwk", box.privateKey),
    signPub: new Uint8Array(await crypto.subtle.exportKey("raw", sign.publicKey)),
    boxPub: new Uint8Array(await crypto.subtle.exportKey("raw", box.publicKey)),
  };
}

// importKeypair rebuilds a keypair from the Go package's MarshalKeypair
// JSON (hex-encoded private scalars) plus the two 65-byte public points.
// The points are required because WebCrypto cannot compute a public key
// from a private scalar; Go callers get them from SignPub()/BoxPub().
export function importKeypair(marshaled, signPub, boxPub) {
  const kj = typeof marshaled === "string" ? JSON.parse(marshaled) : marshaled;
  const jwk = (d, pub, ops) => ({
    kty: "EC", crv: "P-256",
    x: b64url(pub.slice(1, 33)),
    y: b64url(pub.slice(33, 65)),
    d: b64url(unhex(d)),
    ext: true, key_ops: ops,
  });
  return {
    signPrivJwk: jwk(kj.sign_priv_d, signPub, ["sign"]),
    boxPrivJwk: jwk(kj.box_priv_d, boxPub, ["deriveBits"]),
    signPub: new Uint8Array(signPub),
    boxPub: new Uint8Array(boxPub),
  };
}

async function aesKeyFrom(shared, context, ops) {
  const hkdfKey = await crypto.subtle.importKey("raw", shared, "HKDF", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(0), info: te.encode(context) },
    hkdfKey, { name: "AES-GCM", length: 256 }, false, ops);
}

// seal encrypts plaintext for a recipient's 65-byte box public point,
// domain-separated by context.
export async function seal(recipientBoxPub, context, plaintext) {
  const recipient = await crypto.subtle.importKey("raw", recipientBoxPub,
    { name: "ECDH", namedCurve: "P-256" }, false, []);
  const eph = await crypto.subtle.generateKey(
    { name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
  const shared = await crypto.subtle.deriveBits(
    { name: "ECDH", public: recipient }, eph.privateKey, 256);
  const aes = await aesKeyFrom(shared, context, ["encrypt"]);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, aes, plaintext));
  const ephPub = new Uint8Array(await crypto.subtle.exportKey("raw", eph.publicKey));
  return concat(ephPub, iv, ct);
}

// open decrypts a seal()-format blob with the keypair's box private key.
export async function open(keypair, context, sealed) {
  if (sealed.length < 65 + 12) throw new Error("rastrillo/crypto: sealed input too short");
  const ephPub = await crypto.subtle.importKey("raw", sealed.slice(0, 65),
    { name: "ECDH", namedCurve: "P-256" }, false, []);
  const priv = await crypto.subtle.importKey("jwk", keypair.boxPrivJwk,
    { name: "ECDH", namedCurve: "P-256" }, false, ["deriveBits"]);
  const shared = await crypto.subtle.deriveBits({ name: "ECDH", public: ephPub }, priv, 256);
  const aes = await aesKeyFrom(shared, context, ["decrypt"]);
  const plain = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: sealed.slice(65, 77) }, aes, sealed.slice(77));
  return new Uint8Array(plain);
}

// sign produces the raw 64-byte r‖s signature over
// SHA-256(context ‖ 0x00 ‖ msg). WebCrypto hashes the data itself, so the
// signed payload is context ‖ 0x00 ‖ msg — the same digest Go computes.
export async function sign(keypair, context, msg) {
  const key = await crypto.subtle.importKey("jwk", keypair.signPrivJwk,
    { name: "ECDSA", namedCurve: "P-256" }, false, ["sign"]);
  const data = concat(te.encode(context), new Uint8Array([0]), msg);
  const sig = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, key, data);
  return new Uint8Array(sig);
}

// verify reports whether sig is valid over msg under a 65-byte public
// signing point and context.
export async function verify(signPub, context, msg, sig) {
  try {
    const key = await crypto.subtle.importKey("raw", signPub,
      { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]);
    const data = concat(te.encode(context), new Uint8Array([0]), msg);
    return await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, key, sig, data);
  } catch {
    return false;
  }
}

// derive expands key into a 32-byte subkey (HKDF-SHA256, info=context).
export async function derive(key, context) {
  const hkdfKey = await crypto.subtle.importKey("raw", key, "HKDF", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits(
    { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(0), info: te.encode(context) },
    hkdfKey, 256);
  return new Uint8Array(bits);
}

// newKey generates a fresh 32-byte symmetric key.
export function newKey() {
  return crypto.getRandomValues(new Uint8Array(32));
}

// sealSym encrypts plaintext under a 32-byte key: iv(12) ‖ ct.
export async function sealSym(key, plaintext) {
  const aes = await crypto.subtle.importKey("raw", key, "AES-GCM", false, ["encrypt"]);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, aes, plaintext));
  return concat(iv, ct);
}

// openSym decrypts a sealSym()-format blob under key.
export async function openSym(key, sealed) {
  if (sealed.length < 12) throw new Error("rastrillo/crypto: sealed input too short");
  const aes = await crypto.subtle.importKey("raw", key, "AES-GCM", false, ["decrypt"]);
  const plain = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: sealed.slice(0, 12) }, aes, sealed.slice(12));
  return new Uint8Array(plain);
}
