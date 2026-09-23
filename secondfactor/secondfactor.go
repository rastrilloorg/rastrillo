// Package secondfactor is the seam between a verified first factor and
// the session it earns: the pending half-session that names who must
// still prove a second thing, the attempt budget on guessable proofs,
// and the recovery codes that are every factor's escape hatch. It owns
// no factor of its own — passkey and totp each verify their proof and
// redeem the half-session here — so an app with two factors enrolled
// has ONE confirm page offering both, and an app adding a third factor
// adds it here rather than teaching each identity plugin about it.
//
// The trust boundary is the one passkey drew and this package keeps: a
// half-session opens nothing by itself. It is minted only by Hold,
// which an identity plugin calls at the exact point a verified first
// factor would mint the session (Config.SecondFactor on auth, password
// and any app plugin in their shape), and it is redeemed only by
// Complete, which a factor calls after ITS proof verified. Nobody is
// signed in from nothing.
//
// The shape: an app builds one *Gate at boot (New), merges
// secondfactor.Schema into its migrate.Set AFTER passkey.Schema (see
// Schema), registers each factor it mounts (Add), and hands g.Hold to
// its identity plugins —
//
//	au, _ := auth.New(auth.Config{..., SecondFactor: g.Hold})
//	pk, _ := passkey.New(passkey.Config{..., Gate: g})
//	tp, _ := totp.New(totp.Config{..., Gate: g})
//	g.Add(pk, tp)
//
// — and mounts one plain form POST, the recovery redemption:
//
//	POST /signin/recovery  <- form field "code"   (g.SignInRecovery)
//
// behind the app's csrf.Protect like every other mutating route. The
// confirm page itself is the app's (Config.ConfirmPath): it asks
// g.Pending(r) who is waiting and which factors they hold, and offers
// each factor's own completion endpoint.
package secondfactor

import (
	"database/sql"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// pendingTTL bounds the gap between factors: first-factor success to
// finished proof inside this window, or sign in again from the top.
// Long enough to find the authenticator, short enough that an
// abandoned half-session is not a standing invitation.
const pendingTTL = 5 * time.Minute

// maxAttempts is the guess budget one half-session carries for the
// factors that are guessable — a six-digit code, not a signature. Past
// it the half-session is deleted and the person starts again from the
// first factor, which is the rate limit: a million-code space at five
// guesses per verified first factor is 5×10⁻⁶ per sign-in, and every
// retry costs a fresh magic link or password round.
const maxAttempts = 5

// ErrConsumed is Complete's refusal when the half-session was already
// redeemed (a raced second finish, a replayed one) or has expired.
var ErrConsumed = errors.New("rastrillo/secondfactor: pending sign-in already completed or expired")

// ErrExhausted is Strike's report that the half-session has used its
// last attempt and is gone.
var ErrExhausted = errors.New("rastrillo/secondfactor: too many attempts; sign in again")

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside the app's own. Merge it AFTER passkey.Schema: its second
// migration adopts the half-session and recovery-code tables the
// passkey package owned before this package existed, copying every
// recovery code across and dropping the old tables — and on a fresh
// database that order lets it also tidy away the two tables passkey's
// frozen first migration still creates.
var Schema = migrate.MustFromFS(migrationFS, "secondfactor").Add(migrate.Migration{
	ID: "0002_adopt_passkey",
	Fn: adoptPasskeyTables,
})

// adoptPasskeyTables carries recovery codes over from the passkey
// package's table, where they lived until this package took the seam
// over, and drops the two tables passkey no longer reads. A database
// that never had them (or a fresh one where passkey's schema comes
// later in the merge) is left alone. SQL cannot express "if this
// table exists", which is why this one is Go.
func adoptPasskeyTables(tx *gorm.DB) error {
	var n int
	if err := tx.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'passkey_recovery_codes'`).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		if err := tx.Exec(`INSERT OR IGNORE INTO secondfactor_recovery_codes (code_hash, subject, created_at)
			SELECT code_hash, subject, created_at FROM passkey_recovery_codes`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DROP TABLE passkey_recovery_codes`).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`DROP TABLE IF EXISTS passkey_pending`).Error
}

// Factor is what a second factor looks like to the Gate: it can say
// whether a subject holds one. Verification is the factor's own
// business — it calls Complete when its proof checks out.
type Factor interface {
	Enrolled(subject string) (bool, error)
}

// Config configures New. Sessions, DB and Origin are required.
type Config struct {
	// Sessions is the shared session core. Complete calls its SignIn.
	Sessions *sessions.Sessions

	// DB stores half-sessions and recovery codes.
	DB *sql.DB

	// Origin is the app's external origin, scheme included —
	// "https://app.example.com". It decides the pending cookie's
	// Secure/__Host- attributes, the way sessions' own does.
	Origin string

	// ConfirmPath is where Hold sends a first-factor-verified caller
	// who still has a factor to prove: the app's "confirm it's you"
	// page. Default "/signin/confirm".
	ConfirmPath string

	Logger *slog.Logger
}

// Gate is the wired seam: an app builds one at boot (New), registers
// its factors (Add) and hands Hold to its identity plugins.
type Gate struct {
	cfg     Config
	factors []Factor
}

// New validates cfg and returns a ready *Gate.
func New(cfg Config) (*Gate, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("rastrillo/secondfactor: Config.Sessions is required")
	}
	if cfg.DB == nil {
		return nil, errors.New("rastrillo/secondfactor: Config.DB is required")
	}
	if !strings.HasPrefix(cfg.Origin, "https://") && !strings.HasPrefix(cfg.Origin, "http://") {
		return nil, errors.New("rastrillo/secondfactor: Config.Origin must be an absolute origin like https://app.example.com")
	}
	if cfg.ConfirmPath == "" {
		cfg.ConfirmPath = "/signin/confirm"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Gate{cfg: cfg}, nil
}

// Add registers factors. Hold consults every one; an account enrolled
// in none passes the gate untouched.
func (g *Gate) Add(factors ...Factor) { g.factors = append(g.factors, factors...) }

// ConfirmPath is where Hold redirects — exposed so a factor's own
// failure redirect can land on the same page.
func (g *Gate) ConfirmPath() string { return g.cfg.ConfirmPath }

// Enrolled reports whether subject holds at least one factor — the
// app's cue for whether a step-up can be a proof rather than a full
// re-sign-in, and for what a confirm page should offer.
func (g *Gate) Enrolled(subject string) (bool, error) {
	for _, f := range g.factors {
		ok, err := f.Enrolled(subject)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (g *Gate) secure() bool { return strings.HasPrefix(g.cfg.Origin, "https://") }

// cookieName mirrors sessions' own cookie policy: __Host- on https
// (the prefix requires Secure), plain on a http dev origin.
func (g *Gate) cookieName() string {
	if g.secure() {
		return "__Host-rastrillo_secondfactor"
	}
	return "rastrillo_secondfactor"
}

func (g *Gate) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: g.cookieName(), Value: value, Path: "/",
		MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   g.secure(),
	})
}

// Hold is the identity plugins' SecondFactor hook: called where the
// plugin would mint the session, with the session that WOULD be
// minted. No factor enrolled → (false, nil), and the plugin signs in
// exactly as before. Enrolled → store a half-session (only its hash
// lands in the database, only the token rides the cookie — sessions'
// own token discipline), remember a same-site return_to, redirect to
// ConfirmPath, (true, nil).
//
// The would-be session's Method is what the minted session will
// carry, suffixed with the factor that completed it: a plugin passing
// "magiclink" yields "magiclink+passkey" or "magiclink+totp". An app
// may call Hold itself with a method of its own — "device", say, for
// a remembered browser — and the same rule applies: nothing is minted
// unless a factor completes it.
func (g *Gate) Hold(w http.ResponseWriter, r *http.Request, sess sessions.Session) (bool, error) {
	enrolled, err := g.Enrolled(sess.Subject)
	if err != nil {
		return false, err
	}
	if !enrolled {
		return false, nil
	}
	token, hash, err := sessions.NewToken()
	if err != nil {
		return false, err
	}
	_, err = g.cfg.DB.Exec(
		`INSERT INTO secondfactor_pending (token_hash, subject, method, return_to, expires_at) VALUES (?, ?, ?, ?, ?)`,
		hash, sess.Subject, sess.Method, sessions.SafeReturn(r, "/"),
		time.Now().Add(pendingTTL).UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	g.setCookie(w, token, int(pendingTTL.Seconds()))
	http.Redirect(w, r, g.cfg.ConfirmPath, http.StatusSeeOther)
	return true, nil
}

// Pending is one live half-session: who is waiting, how their first
// factor verified, and where they were going.
type Pending struct {
	Subject  string
	Method   string
	ReturnTo string
	hash     string
}

// Pending resolves the request's cookie to its live half-session —
// expiry-checked, NOT consumed (a failed proof must not burn the whole
// between-factors window; consumption is Complete's job, on success
// only). The confirm page and every factor's sign-in endpoint start
// here; a miss means there is nobody to confirm.
func (g *Gate) Pending(r *http.Request) (Pending, bool) {
	c, err := r.Cookie(g.cookieName())
	if err != nil {
		return Pending{}, false
	}
	p := Pending{hash: sessions.HashToken(c.Value)}
	var expires string
	err = g.cfg.DB.QueryRow(
		`SELECT subject, method, return_to, expires_at FROM secondfactor_pending WHERE token_hash = ?`,
		p.hash).Scan(&p.Subject, &p.Method, &p.ReturnTo, &expires)
	if err != nil {
		return Pending{}, false
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().After(exp) {
		return Pending{}, false
	}
	return p, true
}

// Complete is the door every factor fits: consume the half-session
// (DELETE ... RETURNING — single use, so a raced second finish loses
// with ErrConsumed), clear the cookie, and mint the real session as
// the first factor's method plus "+" plus factor, AuthTime now —
// which is exactly what sessions.RequireFresh checks. The caller has
// verified the proof; this is the part it must not get wrong.
func (g *Gate) Complete(w http.ResponseWriter, r *http.Request, p Pending, factor string) error {
	var consumed string
	if err := g.cfg.DB.QueryRow(
		`DELETE FROM secondfactor_pending WHERE token_hash = ? RETURNING subject`,
		p.hash).Scan(&consumed); err != nil {
		return ErrConsumed
	}
	g.setCookie(w, "", -1)
	return g.cfg.Sessions.SignIn(w, r, sessions.Session{
		Subject:  p.Subject,
		Method:   p.Method + "+" + factor,
		AuthTime: time.Now(),
	})
}

// Strike records a failed guess against the half-session. A factor
// whose proof can be guessed (a code) calls it on every miss; one
// whose proof cannot (a signature over a fresh challenge) never
// needs to. The budget is maxAttempts per half-session: the last
// strike deletes the row, clears the cookie and returns ErrExhausted,
// and the person signs in again from the first factor. Any other
// miss returns nil and leaves the half-session alive.
func (g *Gate) Strike(w http.ResponseWriter, p Pending) error {
	var attempts int
	err := g.cfg.DB.QueryRow(
		`UPDATE secondfactor_pending SET attempts = attempts + 1 WHERE token_hash = ? RETURNING attempts`,
		p.hash).Scan(&attempts)
	if err != nil {
		return ErrConsumed
	}
	if attempts < maxAttempts {
		return nil
	}
	if _, err := g.cfg.DB.Exec(`DELETE FROM secondfactor_pending WHERE token_hash = ?`, p.hash); err != nil {
		g.cfg.Logger.Error("rastrillo/secondfactor: delete exhausted half-session", "err", err)
	}
	g.setCookie(w, "", -1)
	return ErrExhausted
}

// Sweep deletes expired half-session rows. Correctness never depends
// on it — Pending checks expiry itself — it just keeps abandoned
// sign-ins from accumulating. Call it from boot, a sidecar pass, or
// not at all.
func Sweep(db *sql.DB, now time.Time) error {
	_, err := db.Exec(`DELETE FROM secondfactor_pending WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	return err
}
