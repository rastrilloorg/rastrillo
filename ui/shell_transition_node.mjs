import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("./shell.js", import.meta.url), "utf8");
const listeners = {};
const document = {
  querySelector: () => null,
  addEventListener() {},
};
const window = {
  onpagereveal: null,
  onpageswap: null,
  addEventListener: (event, listener) => { listeners[event] = listener; },
};
vm.runInNewContext(source, {
  window, document, URL, DOMException,
  location: new URL("https://app.test/"),
});

const unhandled = [];
process.on("unhandledRejection", reason => { unhandled.push(reason); });
const turn = () => new Promise(resolve => setImmediate(resolve));
for (const event of ["pagereveal", "pageswap"]) {
  for (const promise of ["ready", "finished", "updateCallbackDone"]) {
    const skips = ["AbortError", "InvalidStateError", "TimeoutError"];
    const reasons = skips.map(name => new DOMException("No transition", name));
    reasons.push(new Error("application failure"),
      new TypeError("broken callback"), new DOMException("denied", "SecurityError"),
      Object.assign(new Error("application abort"), { name: "AbortError" }), null);
    for (const reason of reasons) {
      unhandled.length = 0;
      const transition = {
        types: new Set(),
        ready: Promise.resolve(),
        finished: Promise.resolve(),
        updateCallbackDone: Promise.resolve(),
      };
      transition[promise] = Promise.reject(reason);
      listeners[event]({ viewTransition: transition });
      await turn();
      await turn();
      const skipped = reason instanceof DOMException && skips.includes(reason.name);
      const expected = skipped ? [] : [reason];
      assert.deepEqual(unhandled, expected, `${event}.${promise}: ${reason}`);
    }
  }
}

// A global rejection filter would hide this unrelated application failure.
unhandled.length = 0;
const unrelated = new DOMException("application abort", "AbortError");
Promise.reject(unrelated);
await turn();
await turn();
assert.deepEqual(unhandled, [unrelated]);
