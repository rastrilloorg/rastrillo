// signin_node — drives signin.mjs's ceremony and destination guard in
// plain Node, with the network and the authenticator replaced. Nothing
// here touches a page: signin.mjs keeps every DOM step behind its own
// `document` guard, which Node never passes.
//
// passkey/signinjs_test.go is the only caller. It pipes {origin,
// destinations} on stdin and reads back each scenario's outcome, the
// keys each scenario's finish request carried, where each destination
// would send the page (safeNext), and whether each destination is a
// local path on its own terms (localPath) — the spec requires that
// rule tested directly, not only through safeNext's origin check. This
// file sits beside signin.mjs so `go test` finds it by path; it is
// never embedded.
import { ceremony, safeNext, localPath } from "./signin.mjs";

const answer = (body) => async () => body;
const fail = (err) => async () => { throw err; };
const assertion = () => ({
  credentialId: "id", clientDataJSON: "cd", authenticatorData: "ad", signature: "sig",
  prf: new Uint8Array([1, 2, 3]),
});

const scenarios = {
  "signed in, local destination": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "/home" }) },
  "held for a second factor": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "/signin/confirm", pending: true }) },
  "signed in, destination off-site": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true, to: "//evil.example/x" }) },
  "signed in, no destination": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: true }) },
  "finish says ok false": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ ok: false, to: "/home" }) },
  "finish has no ok": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer({ to: "/home" }) },
  "finish answers null": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: answer(null) },
  "begin not 2xx": { begin: fail(new Error("status 500")), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "begin without a challenge": { begin: answer({}), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "finish not 2xx": { begin: answer({ challenge: "c" }), auth: answer(assertion()), finish: fail(new Error("status 400")) },
  "the network is down": { begin: fail(new TypeError("Failed to fetch")), auth: answer(assertion()), finish: answer({ ok: true, to: "/" }) },
  "dismissed, or no passkey": { begin: answer({ challenge: "c" }), auth: fail(new Error("no passkey was offered")), finish: answer({ ok: true, to: "/" }) },
  "the authenticator failed": { begin: answer({ challenge: "c" }), auth: fail(new Error("NotReadableError")), finish: answer({ ok: true, to: "/" }) },
};

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const { origin, destinations } = JSON.parse(raw);
const loc = { href: origin + "/signin", origin };
// destinations is absent from the outcomes-only scenario (Go passes
// nil): treat it as empty rather than crash on null.map.
const cases = destinations || [];
const out = {
  outcomes: {},
  finishKeys: {},
  destinations: cases.map((to) => safeNext(to, loc)),
  localPaths: cases.map((to) => localPath(to)),
};
for (const [name, sc] of Object.entries(scenarios)) {
  let sent = null;
  const post = (url, body) => {
    if (url === "/finish") {
      sent = body;
      return sc.finish();
    }
    return sc.begin();
  };
  out.outcomes[name] = await ceremony({ post, authenticate: sc.auth, begin: "/begin", finish: "/finish", rpId: "app.test", loc });
  out.finishKeys[name] = sent ? Object.keys(sent).sort() : null;
}
process.stdout.write(JSON.stringify(out));
