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
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/internal/returnto"
	"amadan.net/rastrillo/rastrillo/migrate"
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

// Config configures New. Origin is required, and DB is too unless
// Signed is set; everything else has a serviceable default.
type Config struct {
	// DB is the app's database. Migrations must be applied (e.g. via
	// the app's own migration runner) before any method runs.
	DB *sql.DB

	// Origin is the app's external origin, scheme included —
	// "https://app.example.com". In row mode it decides the
	// Secure/__Host- cookie attributes and nothing else: sessions does
	// not redirect off of it. Signed mode also redirects to Origin plus
	// RefreshPath or SigninPath, absolute so a tenant's base path cannot
	// be put in front of them.
	Origin string

	// TTL is how long a minted session lives from the moment it is
	// minted, however much it is used: the absolute lifetime. Default
	// 30 days. An idle lifetime is the app's to hold — it knows when a
	// browser was last seen; this table's shape is frozen by adoption.
	TTL time.Duration

	// SigninPath is where Require sends a signed-out GET/HEAD request.
	// Default "/signin".
	SigninPath string

	Logger *slog.Logger

	// Signed, when set, is signed mode: no rows, DB nil, and a
	// short-lived signed cookie a hub mints and its tenants verify.
	Signed *Signed

	// Now is the clock. Default time.Now. It exists so a caller with
	// its own clock (auth, in tests) moves the cookie's with it.
	Now func() time.Time
}

// Sessions is the session store plus cookie policy for one origin.
// Build exactly one per process (New) and share it.
type Sessions struct {
	cfg Config

	// keys is the signed-mode verification set: Signed.Keys plus Key's
	// own public half, copied in New.
	keys map[string][]byte
}

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside the app's own. It replaces the exported Migrations
// []string: the ledger records what ran, so these statements are no
// longer re-executed on every boot. Tokens never touch this table:
// only their SHA-256 hash is stored.
var Schema = migrate.MustFromFS(migrationFS, "sessions")

// ErrPathRouted answers SignIn and RevokeOthers on the row-backed store
// (Signed nil) under a base path or on its direct host. Every tenant on one origin
// would write the same root-scoped __Host- cookie, so each sign-in
// would sign the reader out of every other tenant and the hub (spec
// §6.3: tenants verify, never mint). A path-routed tenant gets its
// sessions from the hub instead, through Signed (Plan 2).
var ErrPathRouted = errors.New("rastrillo/sessions: no row-backed sessions under a base path; sign in at the hub")

// rowsOff reports whether the row-backed store stands aside for r. The
// cookie it would read there may be the hub's own, or a sibling
// tenant's, so reading it would be a lookup of someone else's token and
// clearing it would sign the reader out of the product. Signed mode is
// not affected: there a tenant verifies the hub's cookie on the product
// path, which is the point of it.
func (s *Sessions) rowsOff(r *http.Request) bool {
	return s.cfg.Signed == nil && basepath.PathRouted(r)
}

// refusePathRouted logs a refused mint. It names the public path and
// not the query, which on a sign-in route can carry a credential.
func (s *Sessions) refusePathRouted(what string, r *http.Request) {
	s.cfg.Logger.Error("rastrillo/sessions: "+what+" refused under a base path; sessions come from the hub", "path", basepath.Path(r, r.URL.Path))
}

// New validates cfg and returns a ready *Sessions.
func New(cfg Config) (*Sessions, error) {
	var keys map[string][]byte
	if cfg.Signed != nil {
		var err error
		if cfg.Signed, keys, err = signedConfig(cfg); err != nil {
			return nil, err
		}
	} else if cfg.DB == nil {
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
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Sessions{cfg: cfg, keys: keys}, nil
}

func (s *Sessions) now() time.Time { return s.cfg.Now() }

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
	if s.cfg.Signed != nil {
		if s.secure() {
			return "__Host-" + s.cfg.Signed.Name
		}
		return s.cfg.Signed.Name
	}
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
	return s.lookupContext(context.Background(), hash, now)
}

func (s *Sessions) lookupContext(ctx context.Context, hash string, now time.Time) (Session, bool, error) {
	var sess Session
	var method, authTime, created, expires string
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT subject, method, auth_time, created_at, expires_at
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
// request already carries a session cookie, the row for its hash is
// revoked first — SignIn doubles as re-authentication (a privilege change, a
// password reset survivor kicking out the old token), and rotation
// means the old token can never be replayed after a fresh SignIn.
func (s *Sessions) SignIn(w http.ResponseWriter, r *http.Request, sess Session) error {
	if s.cfg.Signed != nil {
		return s.signedSignIn(w, r, sess)
	}
	if s.rowsOff(r) {
		s.refusePathRouted("sign in", r)
		return ErrPathRouted
	}
	if c, err := r.Cookie(s.CookieName()); err == nil {
		if err := s.revoke(HashToken(c.Value)); err != nil {
			return err
		}
	}
	token, hash, err := NewToken()
	if err != nil {
		return err
	}
	if err := s.create(hash, sess, s.now()); err != nil {
		return err
	}
	s.setCookie(w, token, int(s.cfg.TTL.Seconds()))
	return nil
}

// Mint creates a session row and returns its token without touching
// any cookie — for a credential that lives somewhere other than this
// browser (the vault's copy of an instance session, say). Same TTL,
// same table, same Sweep as every other row; only the delivery
// differs. The token is the credential: hand it only to something the
// subject controls.
func (s *Sessions) Mint(sess Session) (string, error) {
	if s.cfg.Signed != nil {
		return "", ErrSigned
	}
	token, hash, err := NewToken()
	if err != nil {
		return "", err
	}
	if err := s.create(hash, sess, s.now()); err != nil {
		return "", err
	}
	return token, nil
}

// Adopt verifies token against the store and, if its row is alive,
// sets the session cookie to it and returns the session. It mints
// nothing and revokes nothing: the row simply gains a cookie pointing
// at it (Mint's counterpart — the vault restore path). A dead or
// unknown token refuses without touching any cookie, so a failed
// restore falls through to the app's ordinary sign-in.
func (s *Sessions) Adopt(w http.ResponseWriter, r *http.Request, token string) (Session, bool) {
	if s.cfg.Signed != nil {
		return Session{}, false
	}
	if s.rowsOff(r) {
		s.refusePathRouted("adopt", r)
		return Session{}, false
	}
	sess, ok, err := s.lookup(HashToken(token), s.now())
	if err != nil {
		s.cfg.Logger.Error("rastrillo/sessions: adopt lookup", "err", err)
		return Session{}, false
	}
	if !ok {
		return Session{}, false
	}
	s.setCookie(w, token, int(s.cfg.TTL.Seconds()))
	return sess, true
}

// SignOut revokes the presented session's row (real revocation — the
// cookie alone dying would leave the token valid) and clears the
// cookie. A revoke failure is logged, not returned: sign-out from the
// browser's point of view must succeed even if the row couldn't be
// deleted right now (the expiry check in From still keeps it usable
// only until it lapses).
func (s *Sessions) SignOut(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Signed != nil {
		// No row to revoke: a copy of this cookie stays refreshable
		// until the subject's epoch moves or the session life ends.
		s.clearCookie(w)
		return
	}
	if s.rowsOff(r) {
		// Nothing here was minted by this store, so there is nothing of
		// its own to revoke or clear (see rowsOff).
		return
	}
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
	sess, ok, _ := s.resolve(r)
	return sess, ok
}

// FromContext bounds session lookup by the caller's operation deadline. A
// canceled lookup refuses access without deleting or refreshing the session.
// From retains its existing behavior for callers without an operation bound.
func (s *Sessions) FromContext(ctx context.Context, r *http.Request) (Session, bool) {
	sess, ok, _ := s.resolveContext(ctx, r)
	return sess, ok
}

// resolve is From plus one more bit: stale means the request presented
// a cookie that definitively resolved to no session (missing row or
// expired) — as opposed to no cookie at all, or a lookup error, which
// is logged and reported not-stale so a transient DB failure never
// gets a live cookie cleared.
func (s *Sessions) resolve(r *http.Request) (sess Session, ok, stale bool) {
	return s.resolveContext(context.Background(), r)
}

func (s *Sessions) resolveContext(ctx context.Context, r *http.Request) (sess Session, ok, stale bool) {
	if s.cfg.Signed != nil {
		// Never stale: a cookie this app cannot verify may be one the
		// hub signed under a key this app does not hold yet, and
		// clearing it would sign the person out of every app on the
		// origin.
		sess, state := s.signedCookie(r)
		return sess, state == cookieValid, false
	}
	if s.rowsOff(r) {
		// Not stale either, so Middleware and Require never clear it:
		// see rowsOff.
		return Session{}, false, false
	}
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return Session{}, false, false
	}
	sess, ok, err = s.lookupContext(ctx, HashToken(c.Value), s.now())
	if err != nil {
		if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
			s.cfg.Logger.Error("rastrillo/sessions: session lookup", "err", err)
		}
		return Session{}, false, false
	}
	return sess, ok, !ok
}

type ctxKey struct{}

// Middleware resolves the request's session once and stashes it in the
// context for Current/UserID, then always calls next — unlike Require,
// a signed-out request is not blocked (for routes that behave
// differently when signed in without requiring it). A cookie that
// definitively resolves to nothing (revoked or expired row) is cleared
// on the way through, so a dead cookie stops riding every request.
func (s *Sessions) Middleware(next http.Handler) http.Handler {
	if s.cfg.Signed != nil {
		return s.signedMiddleware(next)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok, stale := s.resolve(r)
		if stale {
			s.clearCookie(w)
		}
		if ok {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess))
		}
		next.ServeHTTP(w, r)
	})
}

// Require guards a handler: no valid session sends a page request
// (GET/HEAD) to SigninPath with a same-site return_to, and answers
// anything else 403; a valid session rides the request context for
// Current/UserID. A session Middleware already stashed is trusted
// as-is — stacking Require inside Middleware costs one lookup, not
// two — and, like Middleware, a definitively-stale cookie is cleared.
func (s *Sessions) Require(next http.Handler) http.Handler {
	if s.cfg.Signed != nil {
		return s.signedGate(next, nil)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.admitting(w, r)
		if !ok {
			s.refuse(w, r, false)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// RequireFresh is Require plus step-up: the session must exist AND its
// credential must have been verified within maxAge. The freshness
// reference is AuthTime when the plugin recorded one (keymail's
// auth_time), else the session row's own creation time — a session is
// only ever minted at credential verification, so At is an honest
// lower bound, and plugins that never set AuthTime (the magic link)
// still converge instead of redirect-looping.
//
// A stale-but-valid GET/HEAD redirects to SigninPath with return_to
// plus reauth=1, so the sign-in page can say "confirm it's you" —
// re-verifying calls SignIn, which rotates the session with a fresh
// AuthTime/At and satisfies the gate. Anything else answers 403.
func (s *Sessions) RequireFresh(maxAge time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s.cfg.Signed != nil {
			return s.signedGate(next, func(sess Session) bool { return Fresh(sess, maxAge, s.now()) })
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := s.admitting(w, r)
			if !ok {
				s.refuse(w, r, false)
				return
			}
			if !Fresh(sess, maxAge, s.now()) {
				s.refuse(w, r, true)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
		})
	}
}

// Fresh reports whether sess's credential was verified within maxAge
// of now — AuthTime when set, else At (see RequireFresh). Exposed for
// handlers that step up conditionally rather than per-route.
func Fresh(sess Session, maxAge time.Duration, now time.Time) bool {
	verified := sess.AuthTime
	if verified.IsZero() {
		verified = sess.At
	}
	return !verified.IsZero() && now.Sub(verified) <= maxAge
}

// admitting is the shared front half of Require and RequireFresh:
// trust Middleware's stash, else resolve (clearing a definitively
// stale cookie on the way).
func (s *Sessions) admitting(w http.ResponseWriter, r *http.Request) (Session, bool) {
	sess, ok := Current(r)
	if !ok {
		var stale bool
		sess, ok, stale = s.resolve(r)
		if stale {
			s.clearCookie(w)
		}
	}
	return sess, ok
}

// refuse sends a page request (GET/HEAD) to SigninPath — with
// reauth=1 when a valid-but-stale session needs step-up rather than a
// plain sign-in — and answers anything else 403. On a direct host it
// sends the page to its product URL instead.
func (s *Sessions) refuse(w http.ResponseWriter, r *http.Request, reauth bool) {
	page := r.Method == http.MethodGet || r.Method == http.MethodHead
	if page && basepath.OnDirectHost(r) {
		// The direct host carries no session (spec §7.3), so the reader
		// is sent to the product path, where the hub's session lives.
		// Neither SigninPath nor the hub's sign-in would do: the first
		// is this tenant's own sign-in, which mints nothing here
		// (ErrPathRouted), and the second's return_to would be
		// requestPath, which has no slug here and would land the reader
		// on the hub's root. So this runs before the signed branch.
		if basepath.PlatformCall(r) {
			// The wake check's client follows redirects, and an https
			// product URL on a plain socket fails the wake.
			http.Error(w, "signed out", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, basepath.ProductURL(r), http.StatusSeeOther)
		return
	}
	if page {
		to := s.cfg.SigninPath + "?return_to=" + url.QueryEscape(r.URL.RequestURI())
		if s.cfg.Signed != nil {
			// Absolute, so a base-path rewrite cannot move the hub's
			// sign-in under a tenant's slug, and from requestPath, so
			// return_to keeps the slug http.StripPrefix removed.
			to = s.cfg.Origin + s.cfg.SigninPath + "?return_to=" + url.QueryEscape(requestPath(r))
		}
		if reauth {
			to += "&reauth=1"
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	if reauth {
		http.Error(w, "re-authentication required", http.StatusForbidden)
		return
	}
	http.Error(w, "signed out", http.StatusForbidden)
}

// Current returns the Session Middleware or Require resolved for this
// request.
func Current(r *http.Request) (Session, bool) {
	sess, ok := r.Context().Value(ctxKey{}).(Session)
	return sess, ok
}

// WithSession returns a request whose context carries sess for
// Current/UserID — the stash half of Middleware/Require, exported for
// middleware OUTSIDE this package that resolves a session itself
// (auth.RequireSession, say) and must keep sessions.Current in
// agreement: code downstream of any session-guarding middleware —
// generated scoped actions in particular — reads identity through
// sessions.Current alone, whichever plugin did the guarding.
func WithSession(r *http.Request, sess Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess))
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
	if s.cfg.Signed != nil {
		return nil
	}
	_, err := s.cfg.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	return err
}

// SafeReturn returns r.FormValue("return_to") when it is a same-site
// absolute path — starts with exactly one "/", no scheme, no
// backslash, no control characters — and fallback otherwise. Anything
// laxer is an open redirect on a sign-in endpoint. Control characters
// matter because browsers strip tab/CR/LF from a URL before parsing
// it: "/\t/evil.example" would pass a bare "//" check and still
// navigate scheme-relative off-site.
func SafeReturn(r *http.Request, fallback string) string {
	return SafeReturnTo(r.FormValue("return_to"), fallback)
}

// SafeReturnTo is SafeReturn's own check, on a bare string rather than
// a request's return_to field — for a caller that has one from
// somewhere else a form can't reach, a cookie that outlived the
// request that set it, say.
//
// The check itself lives in internal/returnto, so the root package's
// switchers can run the same one without importing this package (which
// imports root for rastrillo.Actor).
func SafeReturnTo(to, fallback string) string {
	return returnto.Safe(to, fallback)
}
