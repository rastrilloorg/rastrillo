// register_node — drives register.mjs's enrol in plain Node, with the
// network and the authenticator replaced. passkey/registerjs_test.go is
// the only caller: it pipes {origin} on stdin and reads back one walk
// with the real postJSON over a stubbed fetch, then each stubbed
// scenario's outcome. Beside register.mjs so `go test` finds it by
// path; never embedded. The walks run the module's own postOrReauth
// over a stubbed fetch.
import { enrol, postOrReauth } from "./register.mjs";

let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const { origin } = JSON.parse(raw);
const loc = { href: origin + "/signin/add/passkey", origin };
const page = {
  begin: "/passkey/register/begin",
  finish: "/passkey/register/finish?return_to=%2Fdocs",
  rpId: "app.test", rpName: "Harbour", userName: "ada@example.com", loc,
};
const created = {
  credentialId: "id", clientDataJSON: "cd", attestationObject: "ao",
  prf: new Uint8Array([1]),
};

const out = { requests: [], registered: null, finishKeys: null };
globalThis.fetch = async (url, init) => {
  out.requests.push(init.method + " " + url);
  if (url === page.begin) {
    return new Response(JSON.stringify({ challenge: "Y2g" }), { status: 200 });
  }
  out.finishKeys = Object.keys(JSON.parse(init.body)).sort();
  return new Response(JSON.stringify({ ok: true, to: "/signin/add/passkey?done=1" }), { status: 200 });
};
out.walk = await enrol({
  ...page, post: postOrReauth,
  register: async (asked) => {
    out.registered = {
      challenge: asked.challenge, rpId: asked.rpId, rpName: asked.rpName,
      userName: asked.userName, userIdLength: asked.userId.length,
    };
    return created;
  },
});

// The session went stale after the page loaded: begin answers 401
// naming the sign-in page, and the page goes there.
const reauth = "/signin?reauth=1&return_to=%2Fsignin%2Fadd%2Fpasskey&force=1";
globalThis.fetch = async () => new Response(JSON.stringify({ reauth }), { status: 401 });
out.stale = await enrol({
  ...page, post: postOrReauth,
  register: async () => { throw new Error("register must not run"); },
});
globalThis.fetch = async () => new Response(JSON.stringify({ reauth: "//evil.example/x" }), { status: 401 });
out.staleOffSite = await enrol({ ...page, post: postOrReauth, register: async () => created });
globalThis.fetch = async () => new Response("{}", { status: 401 });
out.staleNoAddress = await enrol({ ...page, post: postOrReauth, register: async () => created });

const answer = (body) => async () => body;
const fail = (err) => async () => { throw err; };
const scenarios = {
  "finish says ok false": { begin: answer({ challenge: "c" }), register: answer(created), finish: answer({ ok: false, to: "/x" }) },
  "finish not 2xx": { begin: answer({ challenge: "c" }), register: answer(created), finish: fail(new Error("status 403")) },
  "begin without a challenge": { begin: answer({}), register: answer(created), finish: answer({ ok: true, to: "/x" }) },
  "the person cancelled": { begin: answer({ challenge: "c" }), register: fail(new Error("NotAllowedError")), finish: answer({ ok: true, to: "/x" }) },
  "finish names another site": { begin: answer({ challenge: "c" }), register: answer(created), finish: answer({ ok: true, to: "//evil.example/x" }) },
};
out.outcomes = {};
for (const [name, sc] of Object.entries(scenarios)) {
  const post = (url) => (url === page.begin ? sc.begin() : sc.finish());
  out.outcomes[name] = await enrol({ ...page, post, register: sc.register });
}

process.stdout.write(JSON.stringify(out));
