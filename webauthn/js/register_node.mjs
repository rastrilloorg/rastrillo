// register_node — drives webauthn.mjs's register in plain Node, with
// navigator.credentials replaced. webauthn/register_test.go is the only
// caller; it reads back the relying party each ceremony asked for and
// how the run ended. Never embedded.
const calls = [];
let prfAtCreation = true;
const prfBytes = new Uint8Array(32).fill(7);
const extensions = (prf) => () => (prf ? { prf: { results: { first: prf } } } : {});
Object.defineProperty(globalThis, "navigator", {
  value: {
    credentials: {
      create: async (options) => {
        calls.push({ ceremony: "create", rpId: options.publicKey.rp.id ?? null });
        return {
          rawId: new Uint8Array([1, 2, 3]),
          response: { clientDataJSON: new Uint8Array([4]), attestationObject: new Uint8Array([5]) },
          getClientExtensionResults: extensions(prfAtCreation && prfBytes),
        };
      },
      get: async (options) => {
        const allowed = options.publicKey.allowCredentials.map((c) => Array.from(c.id));
        calls.push({ ceremony: "get", rpId: options.publicKey.rpId ?? null, allowed });
        return { getClientExtensionResults: extensions(prfBytes) };
      },
    },
  },
  configurable: true,
});

const { register } = await import("./webauthn.mjs");
const out = {};
async function run(name, args) {
  calls.length = 0;
  try {
    const cred = await register({
      challenge: "AAAA", rpName: "Box", userId: "AQID", userName: "kay@example.org", prfSalt: "salt-v1", ...args,
    });
    out[name] = { ok: true, prfBytes: cred.prf.length, calls: [...calls] };
  } catch (err) {
    out[name] = { ok: false, error: err.name === "Error" ? err.message : err.name, calls: [...calls] };
  }
  prfAtCreation = true;
}

// The relying party is a parent of the page's host, as an app that
// serves each account on its own subdomain registers.
await run("PRF at creation", { rpId: "box.example.test" });
prfAtCreation = false;
await run("PRF by a second prompt", { rpId: "box.example.test" });
prfAtCreation = false;
await run("PRF by a second prompt, no relying party given", {});
process.stdout.write(JSON.stringify(out));
