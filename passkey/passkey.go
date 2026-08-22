// Package passkey hardens an app's sessions with a WebAuthn second
// factor on the step-up seam: a signed-in user enrolls a passkey, and
// a valid-but-stale session (refused by sessions.RequireFresh) is made
// fresh again by an assertion ceremony instead of a full re-sign-in.
//
// The trust boundary is deliberate: a passkey here upgrades an
// EXISTING session's freshness — it never signs anybody in from
// nothing. Every endpoint demands a valid session first (stale is
// fine; absent is not), so a stolen credential id alone opens no door,
// and the primary factor (magic link, keymail, password) stays the way
// an account is entered. Requiring a passkey at first sign-in — a true
// second factor gate — needs a pending half-session between factors
// and is deliberately not built yet.
//
// The shape: an app builds one *Handlers at boot (New), appends
// passkey.Migrations to its migration list, serves webauthn.JS() as a
// static asset for the browser half, and mounts four JSON endpoints —
//
//	POST /passkey/register/begin   -> {"challenge": ...}
//	POST /passkey/register/finish  <- register()'s result (webauthn.mjs)
//	POST /passkey/stepup/begin     -> {"challenge": ...}
//	POST /passkey/stepup/finish    <- authenticate()'s result
//
// — behind the app's csrf.Protect like every other mutating route.
// A successful step-up calls sessions.SignIn, which rotates the
// session with Method "passkey" and a fresh AuthTime — exactly what
// RequireFresh checks.
package passkey

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/carlosframework/rastrillo/sessions"
	"github.com/carlosframework/rastrillo/webauthn"
)

// challengeTTL bounds a ceremony: begin to finish inside this window,
// or start again. Long enough for an authenticator prompt, short
// enough that an abandoned challenge is not a standing invitation.
const challengeTTL = 2 * time.Minute

// Migrations is the package's schema — additive and idempotent, meant
// to be appended to an app's migration list. Credentials are public
// material (a public key verifies signatures and nothing else);
// challenges are single-use rows consumed by DELETE ... RETURNING.
var Migrations = []string{
	`CREATE TABLE IF NOT EXISTS passkey_credentials (
	  id         TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  public_key BLOB NOT NULL,
	  sign_count INTEGER NOT NULL DEFAULT 0,
	  created_at TEXT NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS passkey_credentials_subject
	  ON passkey_credentials (subject);`,
	`CREATE TABLE IF NOT EXISTS passkey_challenges (
	  challenge  TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  purpose    TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}

// Config configures New. Sessions, DB and Origin are required.
type Config struct {
	// Sessions is the shared session core. Every endpoint resolves the
	// caller through it, and a successful step-up calls its SignIn.
	Sessions *sessions.Sessions

	// DB stores credentials and in-flight challenges.
	DB *sql.DB

	// Origin is the app's external origin, scheme included —
	// "https://app.example.com". The WebAuthn RPID is its hostname.
	Origin string

	// LegacyRPID is passed through to webauthn.Config: a relying party
	// this instance used to be, accepted for assertions (never
	// registration) so a hostname move doesn't strand enrolled keys.
	LegacyRPID string

	Logger *slog.Logger
}

// Handlers is the wired plugin: an app builds one at boot (New) and
// mounts its methods.
type Handlers struct {
	cfg Config
	wa  webauthn.Config
}

// New validates cfg and returns a ready *Handlers.
func New(cfg Config) (*Handlers, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("rastrillo/passkey: Config.Sessions is required")
	}
	if cfg.DB == nil {
		return nil, errors.New("rastrillo/passkey: Config.DB is required")
	}
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("rastrillo/passkey: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Handlers{
		cfg: cfg,
		wa:  webauthn.Config{RPID: u.Hostname(), Origin: cfg.Origin, LegacyRPID: cfg.LegacyRPID},
	}, nil
}

// Enrolled reports whether subject has at least one passkey — the
// app's cue to offer "confirm with your passkey" instead of a full
// re-sign-in on its step-up page.
func (h *Handlers) Enrolled(subject string) (bool, error) {
	var n int
	err := h.cfg.DB.QueryRow(
		`SELECT COUNT(*) FROM passkey_credentials WHERE subject = ?`, subject).Scan(&n)
	return n > 0, err
}

// current resolves the calling session — valid is enough, fresh is
// not required (a stale session is exactly who step-up serves).
func (h *Handlers) current(r *http.Request) (sessions.Session, bool) {
	if sess, ok := sessions.Current(r); ok {
		return sess, true
	}
	return h.cfg.Sessions.From(r)
}

// begin mints, stores and returns a ceremony challenge for subject.
func (h *Handlers) begin(w http.ResponseWriter, subject, purpose string) {
	challenge, err := webauthn.NewChallenge()
	if err != nil {
		h.fail(w, "challenge", err)
		return
	}
	_, err = h.cfg.DB.Exec(
		`INSERT INTO passkey_challenges (challenge, subject, purpose, expires_at) VALUES (?, ?, ?, ?)`,
		hex.EncodeToString(challenge), subject, purpose,
		time.Now().Add(challengeTTL).UTC().Format(time.RFC3339))
	if err != nil {
		h.fail(w, "store challenge", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"challenge": base64.RawURLEncoding.EncodeToString(challenge),
	})
}

// takeChallenge consumes the posted ceremony's challenge row — single
// use by construction (DELETE ... RETURNING) — and checks it was
// minted for this subject, this purpose, and hasn't expired. The
// challenge itself arrives inside clientDataJSON, where the
// authenticator's signature covers it.
func (h *Handlers) takeChallenge(clientDataJSON []byte, subject, purpose string) ([]byte, error) {
	var cd struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(clientDataJSON, &cd); err != nil {
		return nil, errors.New("unreadable clientDataJSON")
	}
	challenge, err := base64.RawURLEncoding.DecodeString(cd.Challenge)
	if err != nil || len(challenge) == 0 {
		return nil, errors.New("unreadable challenge")
	}
	var gotSubject, gotPurpose, expires string
	err = h.cfg.DB.QueryRow(
		`DELETE FROM passkey_challenges WHERE challenge = ? RETURNING subject, purpose, expires_at`,
		hex.EncodeToString(challenge)).Scan(&gotSubject, &gotPurpose, &expires)
	if err != nil {
		return nil, errors.New("unknown or already-used challenge")
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().After(exp) {
		return nil, errors.New("challenge expired")
	}
	if gotSubject != subject || gotPurpose != purpose {
		return nil, errors.New("challenge was minted for someone else")
	}
	return challenge, nil
}

// RegisterBegin is POST /passkey/register/begin: mint an enrollment
// challenge for the signed-in caller.
func (h *Handlers) RegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	sess, ok := h.current(r)
	if !ok {
		h.refuse(w)
		return
	}
	h.begin(w, sess.Subject, "register")
}

// RegisterFinish is POST /passkey/register/finish: verify the creation
// ceremony (webauthn.mjs register()'s JSON, base64url fields) and
// store the credential for the signed-in caller.
func (h *Handlers) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.current(r)
	if !ok {
		h.refuse(w)
		return
	}
	var body struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	}
	clientDataJSON, fields, err := decodeCeremony(r, &body, &body.ClientDataJSON, &body.AttestationObject)
	if err != nil {
		h.badRequest(w, err)
		return
	}
	challenge, err := h.takeChallenge(clientDataJSON, sess.Subject, "register")
	if err != nil {
		h.badRequest(w, err)
		return
	}
	cred, err := h.wa.Register(challenge, clientDataJSON, fields[0])
	if err != nil {
		h.badRequest(w, err)
		return
	}
	_, err = h.cfg.DB.Exec(
		`INSERT OR REPLACE INTO passkey_credentials (id, subject, public_key, sign_count, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		hex.EncodeToString(cred.ID), sess.Subject, cred.PublicKey, cred.SignCount,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		h.fail(w, "store credential", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// StepUpBegin is POST /passkey/stepup/begin: mint an assertion
// challenge for the caller — whose session may be stale; that is the
// point — provided they have a passkey to assert with.
func (h *Handlers) StepUpBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	sess, ok := h.current(r)
	if !ok {
		h.refuse(w)
		return
	}
	enrolled, err := h.Enrolled(sess.Subject)
	if err != nil {
		h.fail(w, "lookup credentials", err)
		return
	}
	if !enrolled {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no passkey enrolled"})
		return
	}
	h.begin(w, sess.Subject, "stepup")
}

// StepUpFinish is POST /passkey/stepup/finish: verify the assertion
// (webauthn.mjs authenticate()'s JSON) against one of the caller's
// enrolled credentials and rotate the session fresh — Method
// "passkey", AuthTime now — which is what satisfies
// sessions.RequireFresh again.
func (h *Handlers) StepUpFinish(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.current(r)
	if !ok {
		h.refuse(w)
		return
	}
	var body struct {
		ID             string `json:"id"`
		ClientDataJSON string `json:"clientDataJSON"`
		AuthData       string `json:"authenticatorData"`
		Signature      string `json:"signature"`
	}
	clientDataJSON, fields, err := decodeCeremony(r, &body, &body.ClientDataJSON, &body.ID, &body.AuthData, &body.Signature)
	if err != nil {
		h.badRequest(w, err)
		return
	}
	credID, authData, signature := fields[0], fields[1], fields[2]

	challenge, err := h.takeChallenge(clientDataJSON, sess.Subject, "stepup")
	if err != nil {
		h.badRequest(w, err)
		return
	}

	var pub []byte
	var count uint32
	err = h.cfg.DB.QueryRow(
		`SELECT public_key, sign_count FROM passkey_credentials WHERE id = ? AND subject = ?`,
		hex.EncodeToString(credID), sess.Subject).Scan(&pub, &count)
	if err != nil {
		h.badRequest(w, errors.New("unknown credential"))
		return
	}

	next, err := h.wa.Verify(webauthn.Credential{ID: credID, PublicKey: pub, SignCount: count},
		challenge, clientDataJSON, authData, signature)
	if err != nil {
		h.badRequest(w, err)
		return
	}
	if _, err := h.cfg.DB.Exec(
		`UPDATE passkey_credentials SET sign_count = ? WHERE id = ?`,
		next, hex.EncodeToString(credID)); err != nil {
		h.fail(w, "update sign count", err)
		return
	}

	if err := h.cfg.Sessions.SignIn(w, r, sessions.Session{
		Subject:  sess.Subject,
		Method:   "passkey",
		AuthTime: time.Now(),
	}); err != nil {
		h.fail(w, "rotate session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// decodeCeremony reads the POSTed JSON body into dst, then base64url-
// decodes the named string fields (first is always clientDataJSON,
// returned separately; the rest come back in order). POST-only: every
// ceremony endpoint mutates server state (a challenge row at least).
func decodeCeremony(r *http.Request, dst any, first *string, rest ...*string) ([]byte, [][]byte, error) {
	if r.Method != http.MethodPost {
		return nil, nil, errors.New("method not allowed")
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(dst); err != nil {
		return nil, nil, errors.New("unreadable request body")
	}
	clientDataJSON, err := base64.RawURLEncoding.DecodeString(*first)
	if err != nil || len(clientDataJSON) == 0 {
		return nil, nil, errors.New("unreadable clientDataJSON")
	}
	out := make([][]byte, 0, len(rest))
	for _, f := range rest {
		b, err := base64.RawURLEncoding.DecodeString(*f)
		if err != nil || len(b) == 0 {
			return nil, nil, errors.New("unreadable ceremony field")
		}
		out = append(out, b)
	}
	return clientDataJSON, out, nil
}

// refuse answers a caller with no valid session at all. 403 JSON, not
// a redirect: these are fetch() endpoints, not page navigations.
func (h *Handlers) refuse(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "signed out"})
}

// badRequest answers a ceremony that failed verification. One generic
// message: which check failed (challenge, origin, signature, counter)
// is logged for the operator, never enumerated to the caller.
func (h *Handlers) badRequest(w http.ResponseWriter, err error) {
	h.cfg.Logger.Warn("rastrillo/passkey: ceremony refused", "err", err)
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ceremony failed"})
}

func (h *Handlers) fail(w http.ResponseWriter, what string, err error) {
	h.cfg.Logger.Error("rastrillo/passkey: "+what, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "something went wrong"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Sweep deletes expired challenge rows. Correctness never depends on
// it — takeChallenge checks expiry itself — it just keeps abandoned
// ceremonies from accumulating. Call it from boot, a sidecar pass, or
// not at all.
func Sweep(db *sql.DB, now time.Time) error {
	_, err := db.Exec(`DELETE FROM passkey_challenges WHERE expires_at < ?`,
		now.UTC().Format(time.RFC3339))
	return err
}
