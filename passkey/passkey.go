// Package passkey hardens an app's sessions with a WebAuthn second
// factor on the step-up seam: a signed-in user enrolls a passkey, and
// a valid-but-stale session (refused by sessions.RequireFresh) is made
// fresh again by an assertion ceremony instead of a full re-sign-in.
//
// A passkey serves three ways. It upgrades an EXISTING session's
// freshness (step-up); it completes a sign-in whose FIRST factor
// already verified (secondfactor's pending half-session); and, as a
// discoverable credential asserted with user verification, it signs a
// person in on its own — a phishing-resistant credential that has
// already proved possession and a PIN or biometric is both factors,
// and stacking an emailed link in front of it adds friction and no
// security. An assertion WITHOUT user verification is a weaker proof:
// the discover flow hands it to the Gate for a second factor where the
// account holds one, and the session it earns says so in its method.
//
// The shape: an app builds one *Handlers at boot (New), merges
// passkey.Schema into its migrate.Set, serves webauthn.JS() as a
// static asset for the browser half, and mounts the JSON endpoints —
//
//	POST /passkey/register/begin   -> {"challenge": ...}
//	POST /passkey/register/finish  <- register()'s result (webauthn.mjs), plus "label"
//	POST /passkey/stepup/begin     -> {"challenge": ...}
//	POST /passkey/stepup/finish    <- authenticate()'s result
//	POST /passkey/signin/begin     -> {"challenge": ...}   (Gate flow)
//	POST /passkey/signin/finish    <- authenticate()'s result
//	POST /passkey/discover/begin   -> {"challenge": ...}   (no session, no half-session)
//	POST /passkey/discover/finish  <- authenticate()'s result -> {"to": ...}
//
// — behind the app's csrf.Protect like every other mutating route.
// A successful step-up calls sessions.SignIn, which rotates the
// session with Method "passkey" and a fresh AuthTime — exactly what
// RequireFresh checks.
//
// # Sign-in-time 2FA
//
// The pending half-session between factors is the secondfactor
// package's (Config.Gate): an identity plugin's SecondFactor hook is
// secondfactor.Gate.Hold, which for an enrolled account trades the
// immediate sign-in for a half-session and a redirect to the app's
// confirm page. That page runs webauthn.mjs's authenticate() against
// /passkey/signin/{begin,finish}; a verified assertion completes the
// half-session through the Gate, which mints the real session as the
// ORIGINAL first-factor method plus "+passkey" ("magiclink+passkey",
// say) and AuthTime now. Recovery codes live there too, for the
// account whose only passkey is lost. *Handlers is a
// secondfactor.Factor: register it with Gate.Add.
//
// # The sign-in screen
//
// On auth's shipped sign-in screen the discover pair is the passkey
// door. Set Config.Remember to auth's RememberJar so a passkey sign-in
// ends the screen's attempt and is remembered like the other ways in.
package passkey

import (
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn"
)

// Method and MethodUnverified are the words a session carries for a
// passkey proof: the first when the authenticator verified the user (a
// PIN, a fingerprint, a face — phishing-resistant and two factors in
// one), the second when it only found somebody present. An app's tier
// policy reads them; the Gate suffixes them onto the first factor.
const (
	Method           = "passkey"
	MethodUnverified = "passkey-nouv"
)

// challengeTTL bounds a ceremony: begin to finish inside this window,
// or start again. Long enough for an authenticator prompt, short
// enough that an abandoned challenge is not a standing invitation.
const challengeTTL = 2 * time.Minute

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside the app's own. It replaces the exported Migrations
// []string: the ledger records what ran, so these statements are no
// longer re-executed on every boot. Credentials are public material (a
// public key verifies signatures and nothing else); challenges are
// single-use rows consumed by DELETE ... RETURNING. The half-session
// and recovery-code tables this set once created are secondfactor's
// now; its Schema adopts them (merge it after this one).
var Schema = migrate.MustFromFS(migrationFS, "passkey")

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

	// Gate is the shared second-factor seam the sign-in pair redeems
	// (SignInBegin/SignInFinish read its pending half-session and
	// complete it). Nil leaves step-up and enrolment working and makes
	// the sign-in pair refuse: without a Gate nothing can be pending.
	Gate *secondfactor.Gate

	// Authorize, when set, is asked before the discover flow signs a
	// subject in on a passkey alone — the app's roster check, the same
	// hook its other identity plugins take. Nil admits every subject
	// that holds a credential.
	Authorize func(subject string) bool

	// OtherFactor, when set, says whether subject holds a second factor
	// that is not a passkey — an authenticator app, say. The discover
	// flow asks it when an assertion arrives without user verification:
	// with another factor to prove, the sign-in is held at the Gate for
	// it; without one, the person is signed in at the weaker tier and it
	// is the app's job to nudge. Nil means no other factor exists.
	OtherFactor func(subject string) (bool, error)

	// SignedInPath is where a discover sign-in lands when the request
	// carries no same-site return_to. Default "/".
	SignedInPath string

	// Refused, when set, hears about every ceremony that failed
	// verification, with the reason the caller is never told — so an
	// app can write "a passkey was refused: origin does not match" to
	// the account's own security activity. It runs before the response
	// is written and must not write one itself.
	Refused func(r *http.Request, err error)

	// Remember is the sign-in screen's jar (auth's RememberJar). With it,
	// a verified discover assertion ends the screen's sign-in attempt —
	// so an address typed before the passkey was used stops prefilling
	// the form — and records "passkey" as this browser's way in, with no
	// address: discovery knows a subject, not an address, and keeping
	// the old address would label one person's passkey sign-in with
	// another's name. Nil does neither, and a previously typed or
	// remembered address stays on the screen after a passkey sign-in.
	// An auth with SigninScreen off hands out an inert jar, so wiring
	// this is always safe.
	Remember *lastsignin.Jar

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
	if cfg.SignedInPath == "" {
		cfg.SignedInPath = "/"
	}
	return &Handlers{
		cfg: cfg,
		wa:  webauthn.Config{RPID: u.Hostname(), Origin: cfg.Origin, LegacyRPID: cfg.LegacyRPID},
	}, nil
}

// ── the sign-in half: completing secondfactor's half-session ────────

// pending resolves the request's half-session through the Gate; with
// no Gate configured nothing can ever be pending.
func (h *Handlers) pending(r *http.Request) (secondfactor.Pending, bool) {
	if h.cfg.Gate == nil {
		return secondfactor.Pending{}, false
	}
	return h.cfg.Gate.Pending(r)
}

// SignInBegin is POST /passkey/signin/begin: mint an assertion
// challenge for the pending half-session's subject — the Gate flow's
// counterpart of StepUpBegin, keyed on the pending cookie instead of a
// session (there is no session yet; that is the point).
func (h *Handlers) SignInBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	p, ok := h.pending(r)
	if !ok {
		h.refuse(w)
		return
	}
	h.begin(w, p.Subject, "signin")
}

// SignInFinish is POST /passkey/signin/finish: verify the assertion
// against the pending subject's enrolled credentials, then complete
// the half-session through the Gate — single use, so a raced second
// finish loses — which mints the real session: the ORIGINAL
// first-factor method plus "+passkey", AuthTime now. The JSON answer
// carries "to" — the return_to the Gate stored — for the confirm
// page's JS to navigate.
func (h *Handlers) SignInFinish(w http.ResponseWriter, r *http.Request) {
	p, ok := h.pending(r)
	if !ok {
		h.refuse(w)
		return
	}
	_, a, ok := h.assert(w, r, p.Subject, "signin")
	if !ok {
		return
	}

	// Complete the half-session last, on success only — and refuse if
	// a raced (or replayed) finish already did.
	if err := h.cfg.Gate.Complete(w, r, p, methodFor(a)); err != nil {
		if errors.Is(err, secondfactor.ErrConsumed) {
			h.badRequest(w, r, err)
			return
		}
		h.fail(w, "mint session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": p.ReturnTo})
}

// Enrolled reports whether subject has at least one passkey — the
// app's cue to offer "confirm with your passkey" instead of a full
// re-sign-in on its step-up page, and the secondfactor.Factor contract
// the Gate consults.
func (h *Handlers) Enrolled(subject string) (bool, error) {
	var n int
	err := h.cfg.DB.QueryRow(
		`SELECT COUNT(*) FROM passkey_credentials WHERE subject = ?`, subject).Scan(&n)
	return n > 0, err
}

// Info is one credential as the inventory sees it: nothing secret (a
// public key verifies signatures and nothing else, and it is not even
// here), just what a person needs to tell their passkeys apart and a
// policy needs to rate them.
type Info struct {
	// ID is the credential id, hex — the handle Rename and Remove take.
	ID    string
	Label string
	// UserVerified says the authenticator verified the user at
	// registration; a credential that did not is rated a tier lower.
	UserVerified bool
	// BackupEligible and BackupState say whether this is a synced
	// passkey (eligible, and backed up) or one bound to a single device
	// — the one whose loss is the account's loss.
	BackupEligible bool
	BackupState    bool
	// AAGUID names the authenticator's make, hex, where attestation
	// carried one; all zeros where it did not.
	AAGUID     string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// ErrNotYours is Rename's and Remove's answer to an id that is not one
// of the subject's credentials — unknown and someone else's alike.
var ErrNotYours = errors.New("rastrillo/passkey: no such credential for this subject")

// List is every credential subject holds, oldest first.
func (h *Handlers) List(subject string) ([]Info, error) {
	rows, err := h.cfg.DB.Query(`SELECT id, label, user_verified, backup_eligible, backup_state, aaguid, created_at, last_used_at
		FROM passkey_credentials WHERE subject = ? ORDER BY created_at, rowid`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		var in Info
		var uv, be, bs int
		var created, used string
		if err := rows.Scan(&in.ID, &in.Label, &uv, &be, &bs, &in.AAGUID, &created, &used); err != nil {
			return nil, err
		}
		in.UserVerified, in.BackupEligible, in.BackupState = uv != 0, be != 0, bs != 0
		in.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if used != "" {
			in.LastUsedAt, _ = time.Parse(time.RFC3339, used)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// Rename gives one of subject's credentials a new label.
func (h *Handlers) Rename(subject, id, label string) error {
	res, err := h.cfg.DB.Exec(`UPDATE passkey_credentials SET label = ? WHERE id = ? AND subject = ?`, label, id, subject)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotYours
	}
	return nil
}

// Remove deletes one of subject's credentials; it no longer
// authenticates. Whether the account may lose it — its last factor, its
// only strong one — is the app's rule to hold before calling this.
func (h *Handlers) Remove(subject, id string) error {
	res, err := h.cfg.DB.Exec(`DELETE FROM passkey_credentials WHERE id = ? AND subject = ?`, id, subject)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotYours
	}
	return nil
}

// methodFor is the session method an assertion earns.
func methodFor(a webauthn.Assertion) string {
	if a.UserVerified {
		return Method
	}
	return MethodUnverified
}

// assert is the shared half of every assertion endpoint: decode the
// ceremony, spend its challenge (minted for subject and purpose),
// look the credential up under subject, verify, and record the counter
// and the moment. It answers the caller itself on any failure and
// reports ok=false; on success it returns the credential's subject
// (which is subject, unless subject was "" — the discover flow, where
// the credential says who) and what the assertion proved.
func (h *Handlers) assert(w http.ResponseWriter, r *http.Request, subject, purpose string) (string, webauthn.Assertion, bool) {
	var body struct {
		ID             string `json:"id"`
		ClientDataJSON string `json:"clientDataJSON"`
		AuthData       string `json:"authenticatorData"`
		Signature      string `json:"signature"`
	}
	clientDataJSON, fields, err := decodeCeremony(r, &body, &body.ClientDataJSON, &body.ID, &body.AuthData, &body.Signature)
	if err != nil {
		h.badRequest(w, r, err)
		return "", webauthn.Assertion{}, false
	}
	credID, authData, signature := fields[0], fields[1], fields[2]

	challenge, err := h.takeChallenge(clientDataJSON, subject, purpose)
	if err != nil {
		h.badRequest(w, r, err)
		return "", webauthn.Assertion{}, false
	}

	var pub []byte
	var count uint32
	var owner string
	q := `SELECT subject, public_key, sign_count FROM passkey_credentials WHERE id = ?`
	args := []any{hex.EncodeToString(credID)}
	if subject != "" {
		q += ` AND subject = ?`
		args = append(args, subject)
	}
	if err := h.cfg.DB.QueryRow(q, args...).Scan(&owner, &pub, &count); err != nil {
		h.badRequest(w, r, errors.New("unknown credential"))
		return "", webauthn.Assertion{}, false
	}

	a, err := h.wa.Assert(webauthn.Credential{ID: credID, PublicKey: pub, SignCount: count},
		challenge, clientDataJSON, authData, signature)
	if err != nil {
		h.badRequest(w, r, err)
		return "", webauthn.Assertion{}, false
	}
	if _, err := h.cfg.DB.Exec(
		`UPDATE passkey_credentials SET sign_count = ?, backup_state = ?, last_used_at = ? WHERE id = ?`,
		a.SignCount, boolInt(a.BackupState), time.Now().UTC().Format(time.RFC3339), hex.EncodeToString(credID)); err != nil {
		h.fail(w, "update credential", err)
		return "", webauthn.Assertion{}, false
	}
	return owner, a, true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── the discover flow: a passkey signs in on its own ─────────────────

// DiscoverBegin is POST /passkey/discover/begin: a challenge for a
// caller nobody knows yet. The browser is asked for any credential it
// holds for this relying party (allowCredentials empty), and the
// credential it answers with says who.
func (h *Handlers) DiscoverBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	h.begin(w, "", "discover")
}

// DiscoverFinish is POST /passkey/discover/finish: verify the assertion
// against whichever credential it names, and admit its owner. With user
// verification the session is minted outright, Method "passkey" — the
// authenticator has proved possession and the person, and nothing
// weaker need be asked. Without it, and with another factor to prove,
// the sign-in is held at the Gate as Method "passkey-nouv" for that
// factor to complete; with nothing else to prove, the person is signed
// in at that weaker method for the app to nudge. The JSON answer's "to"
// is where the page's JS should go next: the return_to, or the Gate's
// confirm page.
func (h *Handlers) DiscoverFinish(w http.ResponseWriter, r *http.Request) {
	subject, a, ok := h.assert(w, r, "", "discover")
	if !ok {
		return
	}
	// The first factor is proved, so the screen's attempt is over
	// whether or not the subject is admitted — the same rule auth's
	// admit follows, through the same seam.
	if h.cfg.Remember != nil {
		h.cfg.Remember.EndAttempt(w)
	}
	if h.cfg.Authorize != nil && !h.cfg.Authorize(subject) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not admitted"})
		return
	}
	// Before Hold or SignIn, so the held path and the completed path are
	// remembered alike; heldResponse passes Set-Cookie through.
	if h.cfg.Remember != nil {
		h.cfg.Remember.Remember(w, lastsignin.Record{Method: lastsignin.MethodPasskey})
	}
	sess := sessions.Session{Subject: subject, Method: methodFor(a), AuthTime: time.Now()}
	to := sessions.SafeReturn(r, h.cfg.SignedInPath)
	if !a.UserVerified && h.cfg.Gate != nil && h.cfg.OtherFactor != nil {
		other, err := h.cfg.OtherFactor(subject)
		if err != nil {
			h.fail(w, "other factor", err)
			return
		}
		if other {
			// Hold writes a redirect for a page flow; this is a fetch,
			// so catch it and hand the destination back as JSON.
			hold := &heldResponse{ResponseWriter: w}
			done, err := h.cfg.Gate.Hold(hold, r, sess)
			if err != nil {
				h.fail(w, "hold", err)
				return
			}
			if done {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": hold.Header().Get("Location"), "pending": true})
				return
			}
		}
	}
	if err := h.cfg.Sessions.SignIn(w, r, sess); err != nil {
		h.fail(w, "mint session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": to})
}

// heldResponse lets Gate.Hold set its cookie and name its redirect
// without the redirect reaching the wire: the cookie header passes
// through, the status and body are swallowed.
type heldResponse struct {
	http.ResponseWriter
}

func (h *heldResponse) WriteHeader(int)             {}
func (h *heldResponse) Write(b []byte) (int, error) { return len(b), nil }

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
		// Label is the name the person (or the page, from what it
		// knows of the device) gives the credential. Optional.
		Label string `json:"label"`
	}
	clientDataJSON, fields, err := decodeCeremony(r, &body, &body.ClientDataJSON, &body.AttestationObject)
	if err != nil {
		h.badRequest(w, r, err)
		return
	}
	challenge, err := h.takeChallenge(clientDataJSON, sess.Subject, "register")
	if err != nil {
		h.badRequest(w, r, err)
		return
	}
	cred, err := h.wa.Register(challenge, clientDataJSON, fields[0])
	if err != nil {
		h.badRequest(w, r, err)
		return
	}
	// The same credential id registering twice is the same authenticator
	// re-enrolling, which changes nothing: the row it has is the row it
	// keeps. A different person presenting it is refused — a credential
	// belongs to whoever registered it first.
	var owner string
	switch err := h.cfg.DB.QueryRow(`SELECT subject FROM passkey_credentials WHERE id = ?`, hex.EncodeToString(cred.ID)).Scan(&owner); {
	case err == nil && owner != sess.Subject:
		h.badRequest(w, r, errors.New("credential is already registered"))
		return
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": hex.EncodeToString(cred.ID), "existing": true})
		return
	case err != sql.ErrNoRows:
		h.fail(w, "lookup credential", err)
		return
	}
	label := body.Label
	if len(label) > 80 {
		label = label[:80]
	}
	_, err = h.cfg.DB.Exec(
		`INSERT INTO passkey_credentials (id, subject, public_key, sign_count, created_at, label, aaguid, user_verified, backup_eligible, backup_state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hex.EncodeToString(cred.ID), sess.Subject, cred.PublicKey, cred.SignCount,
		time.Now().UTC().Format(time.RFC3339), label, hex.EncodeToString(cred.AAGUID),
		boolInt(cred.UserVerified), boolInt(cred.BackupEligible), boolInt(cred.BackupState))
	if err != nil {
		h.fail(w, "store credential", err)
		return
	}
	// The ceremony just proved the new passkey, as surely as an
	// assertion would: the session is rotated fresh at the tier the
	// authenticator earned, so the change the person is about to make
	// next needs no second step-up with the thing in their hand.
	method := Method
	if !cred.UserVerified {
		method = MethodUnverified
	}
	if err := h.cfg.Sessions.SignIn(w, r, sessions.Session{Subject: sess.Subject, Method: method, AuthTime: time.Now()}); err != nil {
		h.fail(w, "rotate session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": hex.EncodeToString(cred.ID), "verified": cred.UserVerified})
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
	_, a, ok := h.assert(w, r, sess.Subject, "stepup")
	if !ok {
		return
	}

	if err := h.cfg.Sessions.SignIn(w, r, sessions.Session{
		Subject:  sess.Subject,
		Method:   methodFor(a),
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
// is logged for the operator and handed to Config.Refused, never
// enumerated to the caller.
func (h *Handlers) badRequest(w http.ResponseWriter, r *http.Request, err error) {
	h.cfg.Logger.Warn("rastrillo/passkey: ceremony refused", "err", err)
	if h.cfg.Refused != nil {
		h.cfg.Refused(r, err)
	}
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
// not at all (secondfactor.Sweep does the same for half-sessions).
func Sweep(db *sql.DB, now time.Time) error {
	_, err := db.Exec(`DELETE FROM passkey_challenges WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	return err
}
