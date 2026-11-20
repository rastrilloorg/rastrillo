// conditional_node — drives webauthn.mjs's authenticate with
// conditional mediation in plain Node, with PublicKeyCredential and
// navigator.credentials replaced. webauthn/conditional_test.go is the
// only caller; it reads back what each scenario asked the browser for
// and how it ended. Never embedded.
const calls = [];
let conditionalAvailable = true;
let answer = () => ({
  rawId: new Uint8Array([1]),
  response: { clientDataJSON: new Uint8Array([2]), authenticatorData: new Uint8Array([3]), signature: new Uint8Array([4]) },
  getClientExtensionResults: () => ({}),
});
globalThis.PublicKeyCredential = { isConditionalMediationAvailable: async () => conditionalAvailable };
Object.defineProperty(globalThis, "navigator", {
  value: {
    credentials: {
      get: async (options) => {
        calls.push({ rpId: options.publicKey.rpId, mediation: options.mediation ?? null, signal: "signal" in options });
        return answer();
      },
    },
  },
  configurable: true,
});

const { authenticate } = await import("./webauthn.mjs");
const signal = new AbortController().signal;
const out = {};
async function run(name, args, setup) {
  calls.length = 0;
  if (setup) setup();
  try {
    await authenticate({ challenge: "AAAA", rpId: "app.test", ...args });
    out[name] = { ok: true, calls: [...calls] };
  } catch (err) {
    out[name] = { ok: false, error: err.name === "Error" ? err.message : err.name, calls: [...calls] };
  }
  conditionalAvailable = true;
}

await run("modal, as before", {});
await run("conditional", { mediation: "conditional", signal });
await run("conditional where the browser has none", { mediation: "conditional", signal }, () => { conditionalAvailable = false; });
await run("conditional with a legacy relying party", { mediation: "conditional", signal, legacyRpId: "old.test" });
answer = () => { throw new DOMException("aborted", "AbortError"); };
await run("conditional, aborted", { mediation: "conditional", signal });
process.stdout.write(JSON.stringify(out));
