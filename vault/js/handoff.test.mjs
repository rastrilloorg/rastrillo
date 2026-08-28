// The vault twin's tests: the restore handoff round trip in JS, plus
// a fixture sealed by the GO side (written by js_test.go into
// ../testdata/handoff.json) proving the two implementations open each
// other. Run via `go test ./vault/` (the shim materialises crypto.mjs
// beside this file), or by hand after copying crypto/js/crypto.mjs in.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { restoreRequest, openRestoreReturn, encodeFragment, decodeFragment } from "./vault.mjs";
import { seal, importKeypair, unb64 } from "./crypto.mjs";

const RESTORE_CONTEXT = "rastrillo/vault/restore/v1";

test("restore round trip: JS seals, JS opens, nonce enforced", async () => {
  const { req, keypair } = await restoreRequest("https://app.test/vault/restore");
  assert.equal(req.v, 1);
  assert.equal(req.ret, "https://app.test/vault/restore");
  assert.ok(req.nonce.length > 0);

  const sealed = await seal(unb64(req.eph_pub), RESTORE_CONTEXT,
    new TextEncoder().encode(JSON.stringify({ token: "tok-1", nonce: req.nonce })));

  assert.equal(await openRestoreReturn(keypair, req.nonce, sealed), "tok-1");
  await assert.rejects(() => openRestoreReturn(keypair, "wrong", sealed));
});

test("fragment encode/decode round trips and rejects garbage", () => {
  const obj = { v: 1, nonce: "n", ret: "https://app.test" };
  assert.deepEqual(decodeFragment(encodeFragment(obj)), obj);
  assert.throws(() => decodeFragment("!!not-base64url!!"));
});

test("a Go-sealed restore return opens here", async () => {
  const fx = JSON.parse(await readFile(
    fileURLToPath(new URL("../testdata/handoff.json", import.meta.url)), "utf8"));
  const kp = importKeypair(fx.keypair, unb64(fx.sign_pub), unb64(fx.box_pub));
  const token = await openRestoreReturn(kp, fx.nonce, unb64(fx.sealed));
  assert.equal(token, fx.token);
});
