//go:build browser

// The harness's own end-to-end proof, and the browser-side proof of
// this package: real WebAuthn ceremonies against Chromium's virtual
// authenticator, driven through js/webauthn.mjs exactly as an app
// serves it, verified by Config.Register and Config.Verify exactly as
// an app calls them — against a minimal in-repo fixture app (a driver
// page, the embedded module, JSON endpoints, one in-memory
// credential). PRF (hmac-secret) is deterministic per credential+salt,
// which is what makes its output assertable at all.
package webauthn_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// fixture is the app under drive: one enrolled credential, one pending
// challenge, the real Config doing the verifying. RPID is "localhost"
// because the rig's origin is http://localhost:PORT — the trustworthy
// origin an IP address can never be.
type fixture struct {
	mu        sync.Mutex
	cfg       webauthn.Config
	challenge []byte
	cred      webauthn.Credential
	enrolled  bool
}

const fixturePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>webauthn fixture</title><script type="module" src="/driver.mjs"></script></head><body><p id="page">webauthn fixture</p></body></html>`

// fixtureDriver is the page's half of the drive. It appends #ready
// only after wiring window.driver, so a Screen("#ready", ...) wait
// guarantees the module executed before the test evaluates driver.*
// calls. PRF bytes come back hex-encoded — a value chromedp carries as
// a plain string.
const fixtureDriver = `import { register, authenticate } from "/webauthn.mjs";

const salt = "fixture-prf-salt-v1";
const toHex = (bytes) => Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, "0")).join("");

async function json(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) throw new Error(url + " answered " + res.status);
  return res.json();
}

window.driver = {
  // register runs the library's creation ceremony and hands the result
  // to the server's Config.Register.
  async register() {
    const begin = await json("/register/begin");
    const cred = await register({
      challenge: begin.challenge,
      rpId: "localhost",
      rpName: "fixture",
      userId: begin.userId,
      userName: "frida@example.com",
      prfSalt: salt,
    });
    await json("/register/finish", {
      credentialId: cred.credentialId,
      clientDataJSON: cred.clientDataJSON,
      attestationObject: cred.attestationObject,
    });
    return toHex(cred.prf);
  },
  // authenticate runs the library's discoverable assertion and hands
  // the result to the server's Config.Verify.
  async authenticate() {
    const begin = await json("/signin/begin");
    const asrt = await authenticate({ challenge: begin.challenge, rpId: "localhost", prfSalt: salt });
    await json("/signin/finish", {
      credentialId: asrt.credentialId,
      clientDataJSON: asrt.clientDataJSON,
      authenticatorData: asrt.authenticatorData,
      signature: asrt.signature,
    });
    return toHex(asrt.prf);
  },
  // probeCreatePRF asks a raw create() for PRF and reports what came
  // back AT CREATION — "" when the extension result is absent. The
  // probe's credential is deliberately non-resident (residentKey
  // "discouraged"), so it never shows up in a later discoverable
  // assertion and cannot make authenticate() ambiguous.
  async probeCreatePRF() {
    const cred = await navigator.credentials.create({
      publicKey: {
        rp: { id: "localhost", name: "fixture" },
        user: { id: crypto.getRandomValues(new Uint8Array(16)), name: "probe@example.com", displayName: "probe" },
        challenge: crypto.getRandomValues(new Uint8Array(32)),
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        authenticatorSelection: { residentKey: "discouraged", userVerification: "required" },
        extensions: { prf: { eval: { first: new TextEncoder().encode(salt) } } },
      },
    });
    const first = cred?.getClientExtensionResults()?.prf?.results?.first;
    return first ? toHex(first) : "";
  },
};

const ready = document.createElement("p");
ready.id = "ready";
ready.textContent = "driver up";
document.body.append(ready);
`

func (f *fixture) handler(origin string) http.Handler {
	f.cfg = webauthn.Config{RPID: "localhost", Origin: origin}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, fixturePage)
	})
	mux.HandleFunc("GET /webauthn.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(webauthn.JS())
	})
	mux.HandleFunc("GET /driver.mjs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, fixtureDriver)
	})
	mux.HandleFunc("POST /register/begin", f.registerBegin)
	mux.HandleFunc("POST /register/finish", f.registerFinish)
	mux.HandleFunc("POST /signin/begin", f.signinBegin)
	mux.HandleFunc("POST /signin/finish", f.signinFinish)
	return mux
}

// mintChallenge stores a fresh ceremony challenge, exactly as an app
// would keep it across the round trip. A nil return means the error
// was already written.
func (f *fixture) mintChallenge(w http.ResponseWriter) []byte {
	challenge, err := webauthn.NewChallenge()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	f.mu.Lock()
	f.challenge = challenge
	f.mu.Unlock()
	return challenge
}

func (f *fixture) registerBegin(w http.ResponseWriter, r *http.Request) {
	challenge := f.mintChallenge(w)
	if challenge == nil {
		return
	}
	userID := make([]byte, 16)
	rand.Read(userID)
	writeJSON(w, map[string]string{
		"challenge": base64.RawURLEncoding.EncodeToString(challenge),
		"userId":    base64.RawURLEncoding.EncodeToString(userID),
	})
}

func (f *fixture) registerFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CredentialID      string `json:"credentialId"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientData, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	attestation, err2 := base64.RawURLEncoding.DecodeString(body.AttestationObject)
	if err1 != nil || err2 != nil {
		http.Error(w, "bad base64url", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	challenge := f.challenge
	f.mu.Unlock()
	cred, err := f.cfg.Register(challenge, clientData, attestation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.cred, f.enrolled = cred, true
	f.mu.Unlock()
	writeJSON(w, map[string]string{"status": "enrolled"})
}

func (f *fixture) signinBegin(w http.ResponseWriter, r *http.Request) {
	if challenge := f.mintChallenge(w); challenge != nil {
		writeJSON(w, map[string]string{"challenge": base64.RawURLEncoding.EncodeToString(challenge)})
	}
}

func (f *fixture) signinFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CredentialID      string `json:"credentialId"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	cred, enrolled, challenge := f.cred, f.enrolled, f.challenge
	f.mu.Unlock()
	if !enrolled {
		http.Error(w, "nobody is enrolled", http.StatusBadRequest)
		return
	}
	credID, err0 := base64.RawURLEncoding.DecodeString(body.CredentialID)
	clientData, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	authData, err2 := base64.RawURLEncoding.DecodeString(body.AuthenticatorData)
	signature, err3 := base64.RawURLEncoding.DecodeString(body.Signature)
	if err0 != nil || err1 != nil || err2 != nil || err3 != nil {
		http.Error(w, "bad base64url", http.StatusBadRequest)
		return
	}
	if !bytes.Equal(credID, cred.ID) {
		http.Error(w, "unknown credential", http.StatusBadRequest)
		return
	}
	count, err := f.cfg.Verify(cred, challenge, clientData, authData, signature)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.cred.SignCount = count
	f.mu.Unlock()
	writeJSON(w, map[string]string{"status": "verified"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// evalString evaluates one driver call in the page, awaiting its
// promise — an unhandled rejection fails the test through Run, with
// the screen in the report.
func evalString(r *harness.Rig, expr string) string {
	var out string
	r.Run(chromedp.Evaluate(expr, &out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	return out
}

// TestBrowserCeremoniesRoundTripWithPRF is the whole life of a
// passkey-wrapped secret, for real: enrolment through register() with
// a PRF salt, the server verifying with Config.Register; a
// discoverable sign-in through authenticate(), verified with
// Config.Verify — and the PRF bytes identical across both, because
// hmac-secret is deterministic per credential+salt. That equality is
// what lets an E2EE app open on a second sign-in what it sealed at
// enrolment.
func TestBrowserCeremoniesRoundTripWithPRF(t *testing.T) {
	f := &fixture{}
	rig := harness.New(t, f.handler)
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#ready", "fixture booted")

	regPRF := evalString(rig, "driver.register()")
	if len(regPRF) != 64 {
		t.Fatalf("registration PRF is %q, want 32 bytes of hex", regPRF)
	}
	authPRF := evalString(rig, "driver.authenticate()")
	if authPRF != regPRF {
		t.Fatalf("assertion PRF %q != registration PRF %q — PRF must be deterministic per credential+salt", authPRF, regPRF)
	}
	rig.Screen("#ready", "after both ceremonies")
}

// TestRegisterFallsBackToPRFByAssertion covers the branch that shipped
// from kass untested: prfSalt requested, creation returns no PRF,
// register() fetches it with an immediate assertion
// (webauthn.mjs's prfByAssertion) — one test upstream, for every
// consumer.
func TestRegisterFallsBackToPRFByAssertion(t *testing.T) {
	// The baseline first, unshimmed: create() DOES return PRF here.
	// Without it the shim below proves nothing — an authenticator that
	// never answered PRF at creation would take the fallback path
	// whether or not the shim works.
	base := harness.New(t, (&fixture{}).handler)
	base.Run(chromedp.Navigate(base.Origin + "/"))
	base.Screen("#ready", "baseline fixture booted")
	if got := evalString(base, "driver.probeCreatePRF()"); len(got) != 64 {
		t.Fatalf("unshimmed create returned PRF %q, want 32 bytes of hex — the virtual authenticator is not answering the extension at creation", got)
	}

	// Now the shimmed rig: creation PRF is withheld by an own-property
	// override on the returned credential, so register() must take the
	// assertion fallback — credentials.get is untouched and serves real
	// PRF there.
	f := &fixture{}
	rig := harness.New(t, f.handler, harness.WithoutPRFAtCreation())
	rig.Run(chromedp.Navigate(rig.Origin + "/"))
	rig.Screen("#ready", "shimmed fixture booted")
	if got := evalString(rig, "driver.probeCreatePRF()"); got != "" {
		t.Fatalf("shimmed create still returned PRF %q — the shim is not holding, so register() would never fall back", got)
	}
	regPRF := evalString(rig, "driver.register()")
	if len(regPRF) != 64 {
		t.Fatalf("fallback registration PRF is %q, want 32 bytes of hex", regPRF)
	}
	// PRF (hmac-secret) is deterministic per credential+salt, so the
	// bytes the fallback fetched must equal a straight assertion's.
	authPRF := evalString(rig, "driver.authenticate()")
	if authPRF != regPRF {
		t.Fatalf("straight assertion PRF %q != fallback PRF %q — the fallback fetched something other than the credential's real PRF", authPRF, regPRF)
	}
	rig.Screen("#ready", "after the fallback ceremonies")
}
