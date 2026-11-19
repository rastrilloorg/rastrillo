// conditional_node — drives signin.mjs's autofill sign-in (conditional)
// in plain Node, with the network and the authenticator replaced.
// passkey/conditionaljs_test.go is the only caller; it reads back each
// scenario's outcome, how many times it began, and what authenticate
// was asked. This file sits beside signin.mjs so `go test` finds it by
// path; it is never embedded.
import { conditional } from "./signin.mjs";

const assertion = { credentialId: "id", clientDataJSON: "cd", authenticatorData: "ad", signature: "sig" };
const ok = { ok: true, to: "/home" };
const refused = () => { throw new Error("status 400"); };

const scenarios = {
  "signed in at once": { finishes: [() => ok] },
  "refused once, then signed in": { finishes: [refused, () => ok] },
  "refused twice": { finishes: [refused, refused, () => ok] },
  "finish says ok false twice": { finishes: [() => ({ ok: false }), () => ({ ok: false })] },
  "begin fails": { begin: () => { throw new Error("status 500"); }, finishes: [() => ok] },
  "the request was aborted": { auth: () => { throw new DOMException("aborted", "AbortError"); }, finishes: [() => ok] },
  "refused after the form was sent": { abortAfterFinish: true, finishes: [refused, () => ok] },
  "off-site destination": { finishes: [() => ({ ok: true, to: "//evil.example" })] },
};

const loc = { href: "https://app.test/signin", origin: "https://app.test" };
const out = {};
for (const [name, sc] of Object.entries(scenarios)) {
  let begins = 0, finished = 0;
  const asked = [];
  const ctl = new AbortController();
  const post = async (url) => {
    if (url === "/begin") {
      begins++;
      if (sc.begin) return sc.begin();
      return { challenge: "c" + begins };
    }
    const answer = sc.finishes[finished++];
    if (sc.abortAfterFinish) ctl.abort();
    return answer();
  };
  const authenticate = async (args) => {
    asked.push({ challenge: args.challenge, mediation: args.mediation, signal: args.signal === ctl.signal });
    if (sc.auth) return sc.auth();
    return assertion;
  };
  const result = await conditional({ post, authenticate, begin: "/begin", finish: "/finish", rpId: "app.test", loc, signal: ctl.signal });
  out[name] = { result, begins, asked };
}
process.stdout.write(JSON.stringify(out));
