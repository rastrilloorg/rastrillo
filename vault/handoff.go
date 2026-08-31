package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// The handoff moves one credential between an instance and a home
// through the person's browser: secrets ride URL fragments (which
// never reach a server) on the way out, and come back sealed to an
// ephemeral key minted for exactly one return. The instance and the
// home never speak — Eleven's model, kept deliberately.

// restoreContext is the crypto.Seal context for the restore return
// leg. Pinned: changing it strands every home already sealing under it.
const restoreContext = "rastrillo/vault/restore/v1"

// RestoreRequest is what a client carries TO the home's restore page
// (as a URL fragment): where to return, a one-time nonce, and the
// ephemeral public key the answer must be sealed to.
type RestoreRequest struct {
	V      int    `json:"v"`
	Ret    string `json:"ret"`
	Nonce  string `json:"nonce"`
	EphPub []byte `json:"eph_pub"`
}

// restoreReturn is the sealed answer's plaintext shape.
type restoreReturn struct {
	Token string `json:"token"`
	Nonce string `json:"nonce"`
}

// NewRestoreRequest mints the ephemeral keypair and nonce for one
// restore round trip. The keypair lives exactly as long as the round
// trip and is never stored server-side.
func NewRestoreRequest(ret string) (RestoreRequest, *crypto.Keypair, error) {
	kp, err := crypto.Generate()
	if err != nil {
		return RestoreRequest{}, nil, err
	}
	nonce, err := newNonce()
	if err != nil {
		return RestoreRequest{}, nil, err
	}
	return RestoreRequest{V: 1, Ret: ret, Nonce: nonce, EphPub: kp.BoxPub()}, kp, nil
}

// OpenRestoreReturn opens the home's sealed answer with the round
// trip's ephemeral keypair and verifies the nonce — a stale or
// replayed return fails closed.
func OpenRestoreReturn(kp *crypto.Keypair, nonce string, sealed []byte) (token string, err error) {
	plain, err := crypto.Open(kp, restoreContext, sealed)
	if err != nil {
		return "", err
	}
	var rr restoreReturn
	if err := json.Unmarshal(plain, &rr); err != nil {
		return "", err
	}
	if rr.Nonce != nonce || rr.Token == "" {
		return "", errNonce
	}
	return rr.Token, nil
}

// EnrolAnswer is the enrol handler's JSON response: the home URL the
// browser navigates to (payload already in the fragment) plus the
// payload's own fields for clients that assemble their own URL.
type EnrolAnswer struct {
	EnrolURL string     `json:"enrol_url"`
	Nonce    string     `json:"nonce"`
	Entry    EnrolEntry `json:"entry"`
}

// EnrolEntry is what the home stores in the person's servers blob:
// this instance's origin and the fresh Method:"vault" session token
// that will re-admit them here.
type EnrolEntry struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Handoff is the instance's two handlers for the vault handoff. Mount
// Enrol behind sessions.Require and Restore on the signed-out router;
// both under the app-wide csrf middleware like every other POST.
type Handoff struct {
	// Sessions is the app's session store — Enrol mints through it,
	// Restore adopts through it.
	Sessions *sessions.Sessions

	// Home is the home service's origin, scheme included. An app with
	// no home never constructs a Handoff.
	Home string

	// Origin is this app's own external origin — what the home's
	// entry for this instance points back at.
	Origin string

	// SigninPath is where a failed restore lands. Default "/signin".
	SigninPath string
}

// Enrol answers the payload a signed-in browser carries to the home's
// enrol page: a fresh Method:"vault" session token for this instance
// — never the browser's own cookie — plus the origin the entry names.
// POST only; mount behind sessions.Require.
func (h Handoff) Enrol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := sessions.Current(r)
	if !ok {
		http.Error(w, "sign in first", http.StatusForbidden)
		return
	}
	token, err := h.Sessions.Mint(sessions.Session{Subject: sess.Subject, Method: "vault"})
	if err != nil {
		http.Error(w, "could not mint", http.StatusInternalServerError)
		return
	}
	nonce, err := newNonce()
	if err != nil {
		http.Error(w, "could not mint", http.StatusInternalServerError)
		return
	}
	answer := EnrolAnswer{Nonce: nonce, Entry: EnrolEntry{URL: h.Origin, Token: token}}
	payload, err := json.Marshal(struct {
		V     int        `json:"v"`
		Nonce string     `json:"nonce"`
		Ret   string     `json:"ret"`
		Entry EnrolEntry `json:"entry"`
	}{1, nonce, h.Origin, answer.Entry})
	if err != nil {
		http.Error(w, "could not mint", http.StatusInternalServerError)
		return
	}
	answer.EnrolURL = h.Home + "/enrol#" + base64.RawURLEncoding.EncodeToString(payload)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)
}

// Restore adopts the token the browser unwrapped from the home's
// return fragment: a live row gains this browser's cookie and the
// request redirects to its same-site return_to; a dead token lands on
// the signin page with no cookie — restore failure is a signin
// prompt, never an error page. POST only, signed-out.
func (h Handoff) Restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	signin := h.SigninPath
	if signin == "" {
		signin = "/signin"
	}
	token := r.PostFormValue("token")
	if token == "" {
		http.Redirect(w, r, signin, http.StatusSeeOther)
		return
	}
	if _, ok := h.Sessions.Adopt(w, r, token); !ok {
		http.Redirect(w, r, signin, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, sessions.SafeReturn(r, "/"), http.StatusSeeOther)
}

// errNonce is OpenRestoreReturn's refusal — wrapped once so tests and
// callers need no string match.
var errNonce = errNonceType{}

type errNonceType struct{}

func (errNonceType) Error() string {
	return "rastrillo/vault: restore return failed its nonce or carried no token"
}

// newNonce mints a 16-byte base64url one-time value.
func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
