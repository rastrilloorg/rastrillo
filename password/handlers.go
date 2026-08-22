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

	"github.com/carlosframework/rastrillo/sessions"
)

// wrongCredentials is the ONE message Signin ever shows for a failed
// attempt — an unknown email and a wrong password read identically,
// so the response is not an enumeration oracle.
const wrongCredentials = "Wrong email or password."

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
	// answer 404. Any error Create returns is treated as a duplicate
	// email (the only realistic failure mode for a unique-email store)
	// and re-renders accordingly.
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
	return &Handlers{cfg: cfg, limit: newLimiter()}, nil
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
	if err != nil {
		// Create's only realistic failure against a unique-email store
		// is a duplicate; any error is reported that way to the visitor
		// rather than leaking storage details, but it's still logged —
		// a DB outage should not vanish silently just because it looks
		// like a duplicate-email case to the caller.
		h.cfg.Logger.Error("rastrillo/password: create", "err", err)
		h.rerenderSignup(w, r, "That email is already registered.", email)
		return
	}

	h.signInAndRedirect(w, r, id)
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
