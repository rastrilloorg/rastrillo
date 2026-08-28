// The JS twin's half of the keyring compatibility contract: replay
// ../testdata/golden.json — the same pinned vectors the Go test
// replays — through WebCrypto, plus round trips for the randomised
// directions. keyring.mjs imports ./crypto.mjs, which is not checked
// in beside it: the Go shim (js_test.go) materialises the serve-time
// sibling layout in a temp dir and runs this file there. To run by
// hand, copy crypto/js/crypto.mjs into keyring/js/ first, then
// `node --test js/golden.test.mjs` from keyring/ (Node ≥ 20).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { ring, newSeed } from "./keyring.mjs";
import { unb64, b64, generate } from "./crypto.mjs";

const golden = JSON.parse(
  await readFile(fileURLToPath(new URL("../testdata/golden.json", import.meta.url)), "utf8"));

const r = ring(golden.namespace);

test("kass strings verbatim", () => {
  assert.equal(golden.strings.prf_salt, "kass/prf/v1");
  assert.equal(golden.strings.content_context, "kass/content/v1");
  assert.equal(golden.strings.wrap_context, "kass/wrap/v1");
  assert.equal(golden.strings.grant_context, "kass/grant/v1");
  assert.equal(r.prfSalt(), golden.strings.prf_salt);
});

test("contentKey reproduces the pinned key", async () => {
  const got = await r.contentKey(unb64(golden.seed));
  assert.equal(b64(got), golden.content_key);
});

test("wrapKey reproduces the pinned key", async () => {
  const got = await r.wrapKey(unb64(golden.prf));
  assert.equal(b64(got), golden.wrap_key);
});

test("unwrapSeed replays the pinned wrapped blob", async () => {
  const got = await r.unwrapSeed(unb64(golden.prf), unb64(golden.wrapped_seed));
  assert.equal(b64(got), golden.seed);
});

test("unwrapSeed refuses a wrong PRF output", async () => {
  const wrong = crypto.getRandomValues(new Uint8Array(32));
  await assert.rejects(() => r.unwrapSeed(wrong, unb64(golden.wrapped_seed)));
});

test("openGrant replays the pinned grant from the pkcs8 import", async () => {
  const got = await r.openGrant(unb64(golden.member.box_priv_pkcs8), unb64(golden.grant));
  assert.equal(b64(got), golden.content_key);
});

test("wrapSeed here unwraps here — device add is the same seed, new wrap", async () => {
  const seed = newSeed();
  const prfA = crypto.getRandomValues(new Uint8Array(32));
  const prfB = crypto.getRandomValues(new Uint8Array(32));
  const wrapA = await r.wrapSeed(prfA, seed);
  const wrapB = await r.wrapSeed(prfB, seed);
  assert.notDeepEqual(wrapA, wrapB);
  assert.deepEqual(await r.unwrapSeed(prfA, wrapA), seed);
  assert.deepEqual(await r.unwrapSeed(prfB, wrapB), seed);
});

test("grant here opens here with a JWK member key", async () => {
  const member = await generate();
  const key = crypto.getRandomValues(new Uint8Array(32));
  const sealed = await r.grant(member.boxPub, key);
  assert.deepEqual(await r.openGrant(member.boxPrivJwk, sealed), key);
});

test("a grant is namespaced", async () => {
  const member = await generate();
  const key = crypto.getRandomValues(new Uint8Array(32));
  const sealed = await r.grant(member.boxPub, key);
  const other = ring("app");
  await assert.rejects(() => other.openGrant(member.boxPrivJwk, sealed));
});

test("blobKey matches the Go derivation, pinned by value", async () => {
  // Not in golden.json (that file is hash-pinned, wire-format-forever);
  // pinned instead against the Go side's deterministic output for
  // seed = 0x07 x32, ring "kass", name "servers" — see
  // keyring/blobkey_test.go.
  const seed = new Uint8Array(32).fill(7);
  const got = await r.blobKey(seed, "servers");
  assert.equal(
    Buffer.from(got).toString("hex"),
    "f3b239c47fc8cec5c20588083f93c73640b6b75ce8a1473576d93ebe172d2d43");
});

test("blobKey isolates names", async () => {
  const seed = new Uint8Array(32).fill(7);
  const a = Buffer.from(await r.blobKey(seed, "servers")).toString("hex");
  const b = Buffer.from(await r.blobKey(seed, "drafts")).toString("hex");
  assert.notEqual(a, b);
});
