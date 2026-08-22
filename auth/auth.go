// Package auth is the framework's turnkey sign-in: keymail-first with a
// magic-link email fallback, wrapped around github.com/keymaildev/signin
// (which owns the ceremony) the way seapointish's reviewed integration
// wires it — the deliberate holes signin leaves (link storage, mailer,
// cookies, sessions, CSRF, admission) filled once, here, instead of once
// per app.
//
// The shape: an app builds one *Auth at boot (New), appends
// auth.Migrations to its Options.Migrations, and mounts four handlers —
//
//	POST /signin         → a.Begin
//	GET  /auth/callback  → a.Callback   (the keymail OAuth return)
//	GET  /auth/verify    → a.Verify     (the magic-link landing)
//	POST /signout        → a.Signout
//
// — then guards routes with a.RequireSession and reads the signed-in
// identity with auth.From(r). The signin *page* stays the app's: Begin
// and the completion handlers report outcomes by redirecting to
// Config.SigninPath with a query the page renders (?sent=1 — link
// emailed; ?err=rate|address|expired|keymail; ?force=1 — offer the
// plain-email escape hatch after a failed keymail approval).
//
// In this flow the magic link IS the email fallback: keymail-OAuth is
// the primary path for addresses that resolve to a claimed keymail
// inbox, and every other address — and every classification failure,
// which fails open by design — gets a signed link by email instead.
package auth

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/keymaildev/signin"

	"github.com/carlosframework/rastrillo/mail"
	"github.com/carlosframework/rastrillo/sessions"
)

// Identity is the verified sign-in identity — signin's, aliased so an
// app using this package needs no second import for the type.
type Identity = signin.Identity

// DefaultSessionTTL is how long a minted session lives — seapointish's
// 30 days.
const DefaultSessionTTL = 30 * 24 * time.Hour

// pendingTTL caps the pending cookie: signin's own pending blob expires
// in 10 minutes, so a longer cookie only widens the replay surface.
const pendingTTL = 10 * time.Minute

// Config configures New. Origin and InstanceKey are required; everything
// else has a serviceable default.
type Config struct {
	// DB is the app's database. auth.Migrations must be in the app's
	// Options.Migrations (or otherwise applied) before any handler runs.
	DB *sql.DB

	// Origin is the app's external origin, scheme included —
	// "https://app.example.com". It is the OAuth client_id (keymail
	// validates redirects against it), the base of emailed links, and
	// what decides Secure/__Host- cookies.
	Origin string

	// InstanceKey seals the pending blob (HMAC), derived as
	// sha256("rastrillo/auth/pending\x00" + InstanceKey). Empty is
	// refused: an empty input hashes to one fixed, publicly computable
	// value — identical across every such deployment, known to anyone
	// reading this source — which would let an attacker forge a pending
	// blob naming their own keymail server, silently defeating signin's
	// own all-zero-key guard. (seapointish's reasoning, kept verbatim.)
	InstanceKey string

	// Mailer delivers magic links. Nil falls back to mail.Logged with a
	// warning — the emailed link is a live credential, so the fallback
	// is dev-only and says so on every send.
	Mailer mail.Sender

	// Authorize is the admission gate: given a verified address, may it
	// have a session? Nil admits every verified address. Membership
	// models (tables, roles, admin bootstrap) are app policy layered on
	// this hook.
	Authorize func(address string) bool

	// SigninPath is the app's sign-in page, the target of outcome
	// redirects. Default "/signin".
	SigninPath string

	// SignedInPath is where a fresh session lands. Default "/".
	SignedInPath string

	// Subject is the magic-link email subject. Default "Your sign-in
	// link".
	Subject string

	// SessionTTL is the minted session's lifetime. Default
	// DefaultSessionTTL.
	SessionTTL time.Duration

	Logger *slog.Logger
}

// Auth is the wired flow plus session storage. Build exactly one per
// process (New) and share it: the underlying signin.Flow carries its
// rate-limiter state on the value, so a Flow per request would get
// fresh empty budgets every time — no rate limiting at all, silently.
type Auth struct {
	cfg      Config
	flow     *signin.Flow
	sessions *sessions.Sessions
}

// ErrEmptyInstanceKey means Config.InstanceKey was empty — see the
// field's comment for why this is fatal rather than defaulted.
var ErrEmptyInstanceKey = errors.New("rastrillo/auth: Config.InstanceKey must not be empty")

// New wires the one long-lived flow: explicit in-memory rate limiter,
// derived pending key, the keymail client bound to Origin, the link
// store over cfg.DB, and the classifier carrying the lookup-path
// rewrite (see classifier.go).
func New(cfg Config) (*Auth, error) {
	if cfg.Origin == "" || (!strings.HasPrefix(cfg.Origin, "https://") && !strings.HasPrefix(cfg.Origin, "http://")) {
		return nil, errors.New("rastrillo/auth: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.InstanceKey == "" {
		return nil, ErrEmptyInstanceKey
	}
	if cfg.DB == nil {
		return nil, errors.New("rastrillo/auth: Config.DB is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Mailer == nil {
		cfg.Logger.Warn("rastrillo/auth: no Mailer configured — magic links will be logged, not sent")
		cfg.Mailer = mail.Logged(cfg.Logger)
	}
	if cfg.SigninPath == "" {
		cfg.SigninPath = "/signin"
	}
	if cfg.SignedInPath == "" {
		cfg.SignedInPath = "/"
	}
	if cfg.Subject == "" {
		cfg.Subject = "Your sign-in link"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}

	sess, err := sessions.New(sessions.Config{
		DB:         cfg.DB,
		Origin:     cfg.Origin,
		TTL:        cfg.SessionTTL,
		SigninPath: cfg.SigninPath,
		Logger:     cfg.Logger,
	})
	if err != nil {
		return nil, err
	}

	a := &Auth{cfg: cfg, sessions: sess}
	a.flow = &signin.Flow{
		Classifier: newClassifier(),
		Keymail: func(server string) *signin.Keymail {
			return &signin.Keymail{Base: "https://" + server, Origin: cfg.Origin}
		},
		Links:    &linkStore{db: cfg.DB},
		Mailer:   cfg.Mailer,
		Limiter:  signin.NewMemoryLimiter(15 * time.Minute),
		Key:      sha256.Sum256([]byte("rastrillo/auth/pending\x00" + cfg.InstanceKey)),
		Origin:   cfg.Origin,
		LinkPath: "/auth/verify",
		Subject:  cfg.Subject,
	}
	return a, nil
}

// secure reports whether the app's origin is https — which decides both
// the Secure cookie attribute and the __Host- name prefix (the prefix
// requires Secure, so a plain-http dev origin gets the unprefixed name;
// the vitogo TODO, resolved by deciding on the origin). Only the pending
// cookie uses this now; the session cookie's equivalent decision lives
// in sessions.Sessions.secure — the two must keep agreeing, since both
// derive from the same Config.Origin.
func (a *Auth) secure() bool { return strings.HasPrefix(a.cfg.Origin, "https://") }

func (a *Auth) cookieName(base string) string {
	if a.secure() {
		return "__Host-" + base
	}
	return base
}

// SessionCookie is the session cookie's name for this Auth's origin —
// delegated to the sessions core, which owns the cookie now.
func (a *Auth) SessionCookie() string { return a.sessions.CookieName() }

func (a *Auth) pendingCookie() string { return a.cookieName("rastrillo_pending") }

func (a *Auth) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: a.secure(),
	})
}

func (a *Auth) clearCookie(w http.ResponseWriter, name string) {
	a.setCookie(w, name, "", -1)
}

// NewToken mints a session token and its storage hash — a thin alias
// over the sessions core, kept so existing callers of auth.NewToken
// need no import change.
func NewToken() (token, hash string, err error) { return sessions.NewToken() }

// HashToken is the storage hash of a session token: SHA-256, hex —
// a thin alias over the sessions core.
func HashToken(token string) string { return sessions.HashToken(token) }
