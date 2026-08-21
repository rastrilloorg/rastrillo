// Package sessions maintains signed-in sessions: SQLite-backed rows
// (so sign-out and admin revocation are real — a deleted row is dead
// even if the cookie lives on), __Host- cookies on https origins, and
// the request-context surface (Current, UserID) the rest of an app
// reads. It deliberately does not know how a session is EARNED —
// identity plugins (auth's keymail flow, password) verify a credential
// and call SignIn; that one call is the whole plugin contract.
package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Session is a signed-in identity as resolved from a session cookie.
// Subject is the app's own identifier for who is signed in (a user ID,
// typically, kept as a string so plugins never have to agree on a
// numeric type); Method names how the credential was verified (a
// plugin's own vocabulary — "password", "keymail", ...); AuthTime is
// when the underlying credential was verified (zero if the plugin
// doesn't track it — a magic link, say); At is when this session row
// was created.
type Session struct {
	Subject  string
	Method   string
	AuthTime time.Time
	At       time.Time
}

// Config configures New. DB and Origin are required; everything else
// has a serviceable default.
type Config struct {
	// DB is the app's database. Migrations must be applied (e.g. via
	// the app's own migration runner) before any method runs.
	DB *sql.DB

	// Origin is the app's external origin, scheme included —
	// "https://app.example.com". It decides the Secure/__Host- cookie
	// attributes and nothing else: sessions does not redirect off of it.
	Origin string

	// TTL is how long a minted session lives. Default 30 days.
	TTL time.Duration

	// SigninPath is where Require sends a signed-out GET/HEAD request.
	// Default "/signin".
	SigninPath string

	Logger *slog.Logger
}

// Sessions is the session store plus cookie policy for one origin.
// Build exactly one per process (New) and share it.
type Sessions struct {
	cfg Config
}

// Migrations is the package's schema — additive and idempotent, meant
// to be appended to an app's own migration list. Tokens never touch
// this table: only their SHA-256 hash is stored.
var Migrations = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  method     TEXT NOT NULL DEFAULT '',
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}

// New validates cfg and returns a ready *Sessions.
func New(cfg Config) (*Sessions, error) {
	if cfg.DB == nil {
		return nil, errors.New("rastrillo/sessions: Config.DB is required")
	}
	if cfg.Origin == "" || (!strings.HasPrefix(cfg.Origin, "https://") && !strings.HasPrefix(cfg.Origin, "http://")) {
		return nil, errors.New("rastrillo/sessions: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.TTL == 0 {
		cfg.TTL = 30 * 24 * time.Hour
	}
	if cfg.SigninPath == "" {
		cfg.SigninPath = "/signin"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Sessions{cfg: cfg}, nil
}

// secure reports whether the app's origin is https — which decides
// both the Secure cookie attribute and the __Host- name prefix (the
// prefix requires Secure, so a plain-http dev origin gets the
// unprefixed name).
func (s *Sessions) secure() bool { return strings.HasPrefix(s.cfg.Origin, "https://") }

// CookieName is the session cookie's name for this Sessions' origin:
// "__Host-rastrillo_session" on https, plain "rastrillo_session" on
// http (the __Host- prefix is meaningless, and rejected by some
// clients, without Secure).
func (s *Sessions) CookieName() string {
	if s.secure() {
		return "__Host-rastrillo_session"
	}
	return "rastrillo_session"
}

func (s *Sessions) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: s.CookieName(), Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secure(),
	})
}

func (s *Sessions) clearCookie(w http.ResponseWriter) {
	s.setCookie(w, "", -1)
}

// NewToken mints a session token and its storage hash. Only the hash
// is stored; only the token rides the cookie.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is the storage hash of a session token: SHA-256, hex.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// create stores a fresh session row.
func (s *Sessions) create(hash string, sess Session, now time.Time) error {
	authTime := ""
	if !sess.AuthTime.IsZero() {
		authTime = sess.AuthTime.UTC().Format(time.RFC3339)
	}
	_, err := s.cfg.DB.Exec(`INSERT INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		hash, sess.Subject, sess.Method, authTime,
		now.UTC().Format(time.RFC3339), now.Add(s.cfg.TTL).UTC().Format(time.RFC3339))
	return err
}

// lookup resolves a token hash to its Session, expiry-checked.
func (s *Sessions) lookup(hash string, now time.Time) (Session, bool, error) {
	var sess Session
	var method, authTime, created, expires string
	err := s.cfg.DB.QueryRow(`SELECT subject, method, auth_time, created_at, expires_at
		FROM sessions WHERE token_hash = ?`, hash).
		Scan(&sess.Subject, &method, &authTime, &created, &expires)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || now.After(exp) {
		return Session{}, false, nil
	}
	sess.Method = method
	if at, err := time.Parse(time.RFC3339, created); err == nil {
		sess.At = at
	}
	if authTime != "" {
		if at, err := time.Parse(time.RFC3339, authTime); err == nil {
			sess.AuthTime = at
		}
	}
	return sess, true, nil
}

// revoke deletes one session row — real revocation, not just a cleared
// cookie.
func (s *Sessions) revoke(hash string) error {
	_, err := s.cfg.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

// SignIn mints a fresh session for sess and sets the cookie. If the
// request already carries a valid session cookie, that row is revoked
// first — SignIn doubles as re-authentication (a privilege change, a
// password reset survivor kicking out the old token), and rotation
// means the old token can never be replayed after a fresh SignIn.
func (s *Sessions) SignIn(w http.ResponseWriter, r *http.Request, sess Session) error {
	if c, err := r.Cookie(s.CookieName()); err == nil {
		if err := s.revoke(HashToken(c.Value)); err != nil {
			return err
		}
	}
	token, hash, err := NewToken()
	if err != nil {
		return err
	}
	if err := s.create(hash, sess, time.Now()); err != nil {
		return err
	}
	s.setCookie(w, token, int(s.cfg.TTL.Seconds()))
	return nil
}

// SignOut revokes the presented session's row (real revocation — the
// cookie alone dying would leave the token valid) and clears the
// cookie. A revoke failure is logged, not returned: sign-out from the
// browser's point of view must succeed even if the row couldn't be
// deleted right now (the expiry check in From still keeps it usable
// only until it lapses).
func (s *Sessions) SignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.CookieName()); err == nil {
		if err := s.revoke(HashToken(c.Value)); err != nil {
			s.cfg.Logger.Error("rastrillo/sessions: sign out", "err", err)
		}
	}
	s.clearCookie(w)
}

// From resolves the request's session cookie to its Session — the
// direct lookup, for handlers outside Require that want to know who
// (a public page with a signed-in header, say).
func (s *Sessions) From(r *http.Request) (Session, bool) {
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return Session{}, false
	}
	sess, ok, err := s.lookup(HashToken(c.Value), time.Now())
	if err != nil {
		s.cfg.Logger.Error("rastrillo/sessions: session lookup", "err", err)
		return Session{}, false
	}
	return sess, ok
}

type ctxKey struct{}

// Middleware resolves the request's session once and stashes it in the
// context for Current/UserID, then always calls next — unlike Require,
// a signed-out request is not blocked (for routes that behave
// differently when signed in without requiring it).
func (s *Sessions) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sess, ok := s.From(r); ok {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess))
		}
		next.ServeHTTP(w, r)
	})
}

// Require guards a handler: no valid session sends a page request
// (GET/HEAD) to SigninPath with a same-site return_to, and answers
// anything else 403; a valid session rides the request context for
// Current/UserID.
func (s *Sessions) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.From(r)
		if !ok {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				to := s.cfg.SigninPath + "?return_to=" + url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, to, http.StatusSeeOther)
			} else {
				http.Error(w, "signed out", http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// Current returns the Session Middleware or Require resolved for this
// request.
func Current(r *http.Request) (Session, bool) {
	sess, ok := r.Context().Value(ctxKey{}).(Session)
	return sess, ok
}

// UserID is Current's Subject parsed as a numeric ID, for apps whose
// subject is a database row ID. ok is false when there is no current
// session or its Subject isn't an integer (a plugin using non-numeric
// subjects, say).
func UserID(r *http.Request) (int64, bool) {
	sess, ok := Current(r)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(sess.Subject, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// Sweep deletes expired session rows. Correctness never depends on it —
// From and lookup check expiry themselves — its job is keeping
// abandoned sessions from accumulating for the life of the instance.
// Call it from boot, a sidecar pass, or not at all.
func (s *Sessions) Sweep(now time.Time) error {
	_, err := s.cfg.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	return err
}

// SafeReturn returns r.FormValue("return_to") when it is a same-site
// absolute path — starts with exactly one "/", no scheme, no
// backslash — and fallback otherwise. Anything laxer is an open
// redirect on a sign-in endpoint.
func SafeReturn(r *http.Request, fallback string) string {
	to := r.FormValue("return_to")
	if to == "" || !strings.HasPrefix(to, "/") ||
		strings.HasPrefix(to, "//") || strings.ContainsAny(to, "\\") {
		return fallback
	}
	return to
}
