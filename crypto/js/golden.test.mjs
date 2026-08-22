// The JS twin's half of the compatibility contract: replay the same
// pinned fixture (../testdata/golden.json) the Go package replays, plus
// cross-shape round trips. Run via `node --test` (Node ≥ 20); the Go
// test suite runs it automatically when node is on PATH (js_test.go).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import {
  importKeypair, generate, seal, open, sign, verify,
  derive, sealSym, openSym, unb64, b64,
  deriveInvite, newInviteSecret, wrapKey, unwrapKey, newKey,
} from "./crypto.mjs";

const golden = JSON.parse(
  await readFile(fileURLToPath(new URL("../testdata/golden.json", import.meta.url)), "utf8"));

const kp = importKeypair(
  golden.envelope.keypair,
  unb64(golden.envelope.sign_pub),
  unb64(golden.envelope.box_pub));

test("open replays the pinned sealed blob", async () => {
  const got = await open(kp, golden.envelope.seal.context, unb64(golden.envelope.seal.sealed));
  assert.equal(b64(got), golden.envelope.seal.plaintext);
});

test("verify replays the pinned signature", async () => {
  const ok = await verify(
    unb64(golden.envelope.sign_pub),
    golden.envelope.sign.context,
    unb64(golden.envelope.sign.message),
    unb64(golden.envelope.sign.signature));
  assert.equal(ok, true);
});

test("verify rejects the pinned signature under the wrong context", async () => {
  const ok = await verify(
    unb64(golden.envelope.sign_pub),
    "not-the-context",
    unb64(golden.envelope.sign.message),
    unb64(golden.envelope.sign.signature));
  assert.equal(ok, false);
});

test("derive reproduces the pinned subkeys", async () => {
  const key = unb64(golden.repokey.key);
  for (const [ctx, want] of [
    ["amadan-pack-v1", golden.repokey.derive.pack],
    ["amadan-manifest-v1", golden.repokey.derive.manifest],
    ["amadan-browse-v1", golden.repokey.derive.browse],
    ["amadan-ci-v1", golden.repokey.derive.ci],
  ]) {
    assert.equal(b64(await derive(key, ctx)), want, `derive(${ctx})`);
  }
});

test("openSym replays the pinned sealed blob", async () => {
  const got = await openSym(unb64(golden.repokey.key), unb64(golden.repokey.sealsym.sealed));
  assert.equal(b64(got), golden.repokey.sealsym.plaintext);
});

test("seal here, open with the golden private key", async () => {
  const plain = new TextEncoder().encode("js twin round trip");
  const sealed = await seal(kp.boxPub, "twin-v1", plain);
  const got = await open(kp, "twin-v1", sealed);
  assert.deepEqual(got, plain);
});

test("sign here verifies here, and fails on tamper", async () => {
  const fresh = await generate();
  const msg = new TextEncoder().encode("attest");
  const sig = await sign(fresh, "attest-v1", msg);
  assert.equal(sig.length, 64);
  assert.equal(await verify(fresh.signPub, "attest-v1", msg, sig), true);
  msg[0] ^= 1;
  assert.equal(await verify(fresh.signPub, "attest-v1", msg, sig), false);
});

test("sealSym/openSym round trip with a fresh key", async () => {
  const key = crypto.getRandomValues(new Uint8Array(32));
  const plain = new TextEncoder().encode("symmetric");
  const got = await openSym(key, await sealSym(key, plain));
  assert.deepEqual(got, plain);
});

const invites = JSON.parse(
  await readFile(fileURLToPath(new URL("../testdata/invites.json", import.meta.url)), "utf8"));

// unb64url: the vectors carry unpadded base64url; unb64 wants standard.
function unb64url(s) {
  let t = s.replace(/-/g, "+").replace(/_/g, "/");
  while (t.length % 4 !== 0) t += "=";
  return unb64(t);
}

test("deriveInvite replays eleven's pinned vectors", async () => {
  for (const vec of invites.invites) {
    const inv = await deriveInvite(invites.context, vec.t);
    assert.equal(inv.id, vec.id);
    assert.equal(inv.claimSecret, vec.claimSecret);
    assert.equal(inv.claimHash, vec.claimHash);
    // The pinned wrapped blob opens under the derived wrapKey to the
    // pinned group key (wrap uses a random IV, so only unwrap replays).
    const key = await unwrapKey(inv.wrapKey, vec.wrappedGroupKey);
    assert.deepEqual(key, unb64url(vec.groupKey));
  }
});

test("wrapKey/unwrapKey round trip, foreign context refused", async () => {
  const secret = newInviteSecret();
  const inv = await deriveInvite("app-invite", secret);
  const key = newKey();
  const wrapped = await wrapKey(inv.wrapKey, key);
  assert.deepEqual(await unwrapKey(inv.wrapKey, wrapped), key);

  const other = await deriveInvite("other-app", secret);
  await assert.rejects(() => unwrapKey(other.wrapKey, wrapped));
});
