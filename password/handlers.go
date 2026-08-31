package password

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// wrongCredentials is the ONE message Signin ever shows for a failed
// attempt — an unknown email and a wrong password read identically,
// so the response is not an enumeration oracle.
const wrongCredentials = "Wrong email or password."

// ErrRefused marks a Create failure as a policy refusal rather than a
// storage failure. Signup renders a wrapped refusal's message to the
// visitor at 403 instead of the duplicate-email copy at 422.
//
// A membership layer is the motivating caller: telling an uninvited
// visitor that their address is already registered is simply false.
// The 403 buys that honesty at a price — a refused address now looks
// different from a registered one, where the duplicate message made
// the two indistinguishable. That is an accepted trade, not an avoided
// one; Signup's doc comment states the whole posture. Use Refuse to
// build one.
var ErrRefused = errors.New("rastrillo/password: signup refused")

// refusedGeneric is what a refusal renders when it carries no message
// of its own — a bare ErrRefused, or Refuse(""). The sentinel's own
// text names the package and would read as an internal leak on a
// public page, and a blank error would render a 403 with nothing on
// it, so neither is ever shown.
const refusedGeneric = "Sign-up is not open to that address."

// Refuse wraps a visitor-facing message as a signup refusal. The
// message is rendered verbatim, so write it as visitor copy — sentence
// case, ending in a full stop.
//
// Write one message that fits every refused address, and never
// interpolate the submitted address into it. Nothing here enforces
// that: a message that varies by address turns the 403 from a single
// outcome into a finer oracle than the status alone. An empty message
// renders refusedGeneric rather than a blank page.
func Refuse(msg string) error { return &refusal{msg: msg} }

type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }
func (r *refusal) Unwrap() error { return ErrRefused }

// PageData is what a Render* callback renders: the current attempt's
// error (empty on first load), the submitted email (so a re-rendered
// form doesn't make the visitor retype it), and the return_to to
// carry through the form's hidden field.
type PageData struct {
	Error    string
	Email    string
	ReturnTo string
}

// Config configures New. Sessions, Lookup, and RenderSignin are
// required; everything else has a serviceable default or disables a
// feature when left zero.
type Config struct {
	// Sessions is the shared session core. Signin/Signup call
	// Sessions.SignIn on success; Signout calls Sessions.SignOut.
	Sessions *sessions.Sessions

	// Lookup resolves an email (already lowercased and trimmed) to the
	// user's id and stored password hash. sql.ErrNoRows means unknown
	// email — Signin treats it identically to a wrong password.
	Lookup func(ctx context.Context, email string) (id int64, hash string, err error)

	// Create stores a new user's email and password hash, returning the
	// new id. Nil disables signup entirely: SignupPage and Signup both
	// answer 404. An error wrapping ErrRefused is a policy refusal and
	// renders its own message at 403; any other error is treated as a
	// duplicate email (the only realistic failure mode for a
	// unique-email store) and re-renders accordingly.
	Create func(ctx context.Context, email, hash string) (int64, error)

	// SignedInPath is where a fresh session lands absent a same-site
	// return_to. Default "/".
	SignedInPath string

	// SecondFactor is the sign-in-time 2FA seam: called at the exact
	// point a verified password would mint the session, with the
	// session that WOULD be minted. done=true means the hook took over
	// the response (stored a pending half-session and redirected —
	// passkey.Handlers.Gate is the shipped implementation); done=false
	// means no second factor applies and sign-in proceeds unchanged.
	// Nil is exactly today's behavior.
	SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)

	// RenderSignin renders the sign-in page/form, called both for a
	// plain page load (SigninPage) and to re-render after a failed
	// attempt (Signin), with an appropriate status already written.
	RenderSignin func(w http.ResponseWriter, r *http.Request, d PageData)

	// RenderSignup renders the signup page/form the same way
	// RenderSignin does for sign-in. Required only when Create is set.
	RenderSignup func(w http.ResponseWriter, r *http.Request, d PageData)

	Logger *slog.Logger
}

// Handlers is the wired plugin: an app builds one at boot (New) and
// mounts its methods. Build exactly one per process and share it — the
// sign-in rate limiter's state lives on the value, so a Handlers per
// request would get fresh empty budgets every time (the same reasoning
// as auth.Auth's one-Flow rule).
type Handlers struct {
	cfg   Config
	limit *limiter
	// refusals is a second budget, for Create's policy refusals only.
	// It is deliberately not the one Signin consults — see limit.go.
	refusals *limiter
}

// New validates cfg and returns a ready *Handlers.
func New(cfg Config) (*Handlers, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("rastrillo/password: Config.Sessions is required")
	}
	if cfg.Lookup == nil {
		return nil, errors.New("rastrillo/password: Config.Lookup is required")
	}
	if cfg.RenderSignin == nil {
		return nil, errors.New("rastrillo/password: Config.RenderSignin is required")
	}
	if cfg.Create != nil && cfg.RenderSignup == nil {
		return nil, errors.New("rastrillo/password: Config.RenderSignup is required when Config.Create is set")
	}
	if cfg.SignedInPath == "" {
		cfg.SignedInPath = "/"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Handlers{cfg: cfg, limit: newLimiter(), refusals: newLimiter()}, nil
}

// SigninPage renders the sign-in form: GET, no state change.
func (h *Handlers) SigninPage(w http.ResponseWriter, r *http.Request) {
	h.cfg.RenderSignin(w, r, PageData{ReturnTo: r.URL.Query().Get("return_to")})
}

// SignupPage renders the signup form, or 404 when signup is disabled
// (Config.Create nil).
func (h *Handlers) SignupPage(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Create == nil {
		http.NotFound(w, r)
		return
	}
	h.cfg.RenderSignup(w, r, PageData{ReturnTo: r.URL.Query().Get("return_to")})
}

// Signin is POST /signin: verify the submitted credential and mint a
// session on success. Unknown email and wrong password are made to
// look — and cost — the same: both fall through to the identical
// wrongCredentials message, and an unknown email still runs Verify
// (against the package decoyHash) before failing, so the wall-clock
// cost doesn't leak which case happened.
//
// Failed attempts are rate-limited per email (see limit.go for the
// policy and its trade-offs); a blocked attempt re-renders at 429
// without touching Lookup or PBKDF2.
//
// POST only: a GET-mounted Signin would put the plaintext password
// into the URL, browser history, referrers, and access logs.
func (h *Handlers) Signin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := normalizeEmail(r.FormValue("email"))
	submitted := r.FormValue("password")

	// The limit gate runs before Lookup and Verify: a blocked attempt
	// costs no PBKDF2 work (that CPU amplification is half of what the
	// limiter is for) and learns nothing about the account, because the
	// gate sees only the attempt count.
	if h.limit.blocked(email, time.Now()) {
		w.WriteHeader(http.StatusTooManyRequests)
		h.cfg.RenderSignin(w, r, PageData{
			Error:    tooManyAttempts,
			Email:    email,
			ReturnTo: r.FormValue("return_to"),
		})
		return
	}

	id, hash, err := h.cfg.Lookup(r.Context(), email)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Unknown email: still run Verify, against the decoy hash, so
		// this branch costs the same wall-clock as a found-but-wrong
		// password below — no timing oracle for account enumeration.
		Verify(decoyHash, submitted)
		h.limit.fail(email, time.Now())
		h.rerenderSignin(w, r, email)
		return
	case err != nil:
		h.cfg.Logger.Error("rastrillo/password: lookup", "err", err)
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}

	if !Verify(hash, submitted) {
		h.limit.fail(email, time.Now())
		h.rerenderSignin(w, r, email)
		return
	}

	h.limit.clear(email)
	h.signInAndRedirect(w, r, id)
}

// rerenderSignin writes the one shared failure status and message —
// the 422 is written before RenderSignin runs, so whatever RenderSignin
// writes lands under that status.
func (h *Handlers) rerenderSignin(w http.ResponseWriter, r *http.Request, email string) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	h.cfg.RenderSignin(w, r, PageData{
		Error:    wrongCredentials,
		Email:    email,
		ReturnTo: r.FormValue("return_to"),
	})
}

// Signup is POST /signup: validate, hash, store, and sign in — or 404
// if Config.Create is nil (signup disabled).
//
// Signup shares Signin's per-email limiter for the failures both doors
// can see, so probing an address through either spends the same budget
// and blocks both. Policy refusals meter against a second budget of
// their own, which Signin never consults (see limit.go).
//
// What remains open by design: the duplicate-email answer below is an
// honest account-existence oracle, because a password form has no
// out-of-band channel to defer the answer to. A refusal widens that
// rather than narrowing it — a 403 and a 422 are distinguishable, so
// an app that refuses uninvited addresses tells a prober which
// addresses it already knows, where the duplicate copy would have made
// the two cases look the same. The trade is taken with open eyes: the
// alternative is telling a stranger a false thing about their own
// address, and a true distinguishable answer is worth more than a
// false identical one. The enumeration-free alternative for the whole
// flow is the keymail plugin, whose email loop answers every address
// identically; and a sweep across many addresses — which per-email
// limiting cannot see — stays the deployment concern limit.go
// documents.
//
// POST only: same plaintext-password-in-URL reasoning as Signin.
func (h *Handlers) Signup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.Create == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := normalizeEmail(r.FormValue("email"))
	submitted := r.FormValue("password")

	// The gate runs before validation and Hash, mirroring Signin: a
	// blocked attempt costs no PBKDF2 work and hears only the volume
	// message. Both budgets are consulted here — the refusal budget is
	// spent only by this handler, so nothing else would ever enforce
	// it.
	now := time.Now()
	if h.limit.blocked(email, now) || h.refusals.blocked(email, now) {
		w.WriteHeader(http.StatusTooManyRequests)
		h.cfg.RenderSignup(w, r, PageData{
			Error:    tooManyAttempts,
			Email:    email,
			ReturnTo: r.FormValue("return_to"),
		})
		return
	}

	if email == "" || !strings.Contains(email, "@") {
		h.rerenderSignup(w, r, "Enter a valid email address.", email)
		return
	}
	if len(submitted) < 8 {
		h.rerenderSignup(w, r, "Password must be at least 8 characters.", email)
		return
	}

	hash, err := Hash(submitted)
	if err != nil {
		h.cfg.Logger.Error("rastrillo/password: hash", "err", err)
		http.Error(w, "sign-up failed", http.StatusInternalServerError)
		return
	}

	id, err := h.cfg.Create(r.Context(), email, hash)
	if errors.Is(err, ErrRefused) {
		// A refusal is policy, not storage: the visitor hears the
		// caller's own reason at 403. It costs a unit of the refusal
		// budget — Hash ran above, so an unmetered refusal path is a
		// PBKDF2 burn vector — but not a unit of the shared budget
		// Signin reads, or a stranger who knows an invitee's address
		// could hold it at 429 on both doors indefinitely.
		//
		// The 403 is distinguishable from the 422 below, so it does
		// carry an existence bit the duplicate copy hid; Signup's doc
		// comment states why that trade is taken. errors.Is is the
		// predicate but not the copy: the message comes off the
		// *refusal itself, so a caller who wrapped the sentinel with
		// their own context cannot put that context on a public page.
		h.refusals.fail(email, time.Now())
		w.WriteHeader(http.StatusForbidden)
		h.cfg.RenderSignup(w, r, PageData{
			Error:    refusalMessage(err),
			Email:    email,
			ReturnTo: r.FormValue("return_to"),
		})
		return
	}
	if err != nil {
		// Create's only realistic failure against a unique-email store
		// is a duplicate; any error is reported that way to the visitor
		// rather than leaking storage details, but it's still logged —
		// a DB outage should not vanish silently just because it looks
		// like a duplicate-email case to the caller.
		h.cfg.Logger.Error("rastrillo/password: create", "err", err)
		// The honest answer is also the oracle (see the doc comment),
		// so it costs a limiter unit: ten confirmations of one address
		// inside the window and both doors block.
		h.limit.fail(email, time.Now())
		h.rerenderSignup(w, r, "That email is already registered.", email)
		return
	}

	h.limit.clear(email)
	h.refusals.clear(email)
	h.signInAndRedirect(w, r, id)
}

// refusalMessage is the visitor copy for a refusal. errors.As, not
// err.Error(): Error() on a wrapped refusal returns the wrapper's
// message ("invitation lookup: rastrillo/password: signup refused"),
// which is an internals leak on a public 403. A bare sentinel and an
// empty Refuse("") both fall back to refusedGeneric.
func refusalMessage(err error) string {
	var ref *refusal
	if errors.As(err, &ref) && ref.msg != "" {
		return ref.msg
	}
	return refusedGeneric
}

func (h *Handlers) rerenderSignup(w http.ResponseWriter, r *http.Request, msg, email string) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	h.cfg.RenderSignup(w, r, PageData{
		Error:    msg,
		Email:    email,
		ReturnTo: r.FormValue("return_to"),
	})
}

// signInAndRedirect offers the would-be session to the SecondFactor
// hook (which may trade it for a pending half-session and take over
// the response), and otherwise mints it and sends the 303 both Signin
// and Signup finish with.
func (h *Handlers) signInAndRedirect(w http.ResponseWriter, r *http.Request, id int64) {
	sess := sessions.Session{
		Subject:  strconv.FormatInt(id, 10),
		Method:   "password",
		AuthTime: time.Now(),
	}
	if h.cfg.SecondFactor != nil {
		done, err := h.cfg.SecondFactor(w, r, sess)
		if err != nil {
			h.cfg.Logger.Error("rastrillo/password: second factor", "err", err)
			http.Error(w, "sign-in failed", http.StatusInternalServerError)
			return
		}
		if done {
			return
		}
	}
	if err := h.cfg.Sessions.SignIn(w, r, sess); err != nil {
		h.cfg.Logger.Error("rastrillo/password: sign in", "err", err)
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, sessions.SafeReturn(r, h.cfg.SignedInPath), http.StatusSeeOther)
}

// Signout is POST /signout: revoke the session and clear the cookie.
// CSRF is not this package's job — csrf.Protect is mounted app-wide,
// but it only gates POST/PUT/PATCH/DELETE, so a GET-mounted Signout
// would escape it entirely; POST only, enforced here rather than by
// mount-pattern convention.
func (h *Handlers) Signout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.cfg.Sessions.SignOut(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// normalizeEmail is the canonical form Lookup and Create are always
// called with: lowercased, whitespace-trimmed, so "User@Example.com "
// and "user@example.com" resolve to the same row.
func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
