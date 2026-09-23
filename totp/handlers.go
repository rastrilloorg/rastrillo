package totp

import (
	"database/sql"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside the app's own. One table: a sealed secret per subject,
// confirmed or not, and the last step it verified at.
var Schema = migrate.MustFromFS(migrationFS, "totp")

// ErrEnrolled is Begin's refusal for a subject whose authenticator is
// already confirmed: Disable first (from a page behind RequireFresh),
// then enrol afresh. Silently replacing a live secret would let a
// stale session swap the factor out from under its owner.
var ErrEnrolled = errors.New("rastrillo/totp: an authenticator is already set up")

// Config configures New. DB, Sessions, Key and Issuer are required.
type Config struct {
	// DB stores sealed secrets.
	DB *sql.DB

	// Sessions is the shared session core; StepUp calls its SignIn.
	Sessions *sessions.Sessions

	// Gate is the shared second-factor seam SignIn redeems. Nil
	// leaves enrolment and step-up working and makes SignIn refuse.
	Gate *secondfactor.Gate

	// Key seals secrets at rest: 32 bytes, e.g. crypto.Derive(instance
	// key, "rastrillo/totp"). Losing it loses every enrolment — the
	// same blast radius as the instance key it should derive from.
	Key []byte

	// Issuer names the app in the authenticator's list — the site's
	// name. It is what a person sees beside the code for years, so
	// make it the name they know the app by.
	Issuer string

	// StepUpFailedPath is where a wrong step-up code lands, with
	// return_to carried along. Default "/signin?reauth=1&totp=failed".
	StepUpFailedPath string

	Logger *slog.Logger
}

// Handlers is the wired factor: an app builds one at boot (New),
// registers it with the Gate and mounts its methods. One per process:
// the step-up limiter's state lives on the value.
type Handlers struct {
	cfg   Config
	limit *limiter
}

// New validates cfg and returns a ready *Handlers.
func New(cfg Config) (*Handlers, error) {
	switch {
	case cfg.DB == nil:
		return nil, errors.New("rastrillo/totp: Config.DB is required")
	case cfg.Sessions == nil:
		return nil, errors.New("rastrillo/totp: Config.Sessions is required")
	case len(cfg.Key) != 32:
		return nil, errors.New("rastrillo/totp: Config.Key must be 32 bytes (crypto.Derive makes one)")
	case cfg.Issuer == "":
		return nil, errors.New("rastrillo/totp: Config.Issuer is required")
	}
	if cfg.StepUpFailedPath == "" {
		cfg.StepUpFailedPath = "/signin?reauth=1&totp=failed"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Handlers{cfg: cfg, limit: newLimiter()}, nil
}

// Enrolment is what a set-up page shows: the key for typing in by
// hand, the otpauth URI, and the same URI as a QR code (inline SVG,
// mark it template.HTML). The secret is confirmed — and the factor
// live — only once Confirm has seen a code from it.
type Enrolment struct {
	Key string
	URI string
	QR  string
}

// Enrolled reports whether subject has a confirmed authenticator —
// the secondfactor.Factor contract, and a settings page's cue.
func (h *Handlers) Enrolled(subject string) (bool, error) {
	var n int
	err := h.cfg.DB.QueryRow(
		`SELECT COUNT(*) FROM totp_secrets WHERE subject = ? AND confirmed_at <> ''`, subject).Scan(&n)
	return n > 0, err
}

// Begin mints a secret for subject and returns what to show. An
// unconfirmed secret from an abandoned attempt is replaced; a
// confirmed one is ErrEnrolled. Call it from a page behind
// sessions.RequireFresh: it is the moment a factor gets attached.
func (h *Handlers) Begin(subject, account string) (Enrolment, error) {
	enrolled, err := h.Enrolled(subject)
	if err != nil {
		return Enrolment{}, err
	}
	if enrolled {
		return Enrolment{}, ErrEnrolled
	}
	secret, err := newSecret()
	if err != nil {
		return Enrolment{}, err
	}
	sealed, err := crypto.SealSym(h.cfg.Key, secret)
	if err != nil {
		return Enrolment{}, err
	}
	if _, err := h.cfg.DB.Exec(
		`INSERT OR REPLACE INTO totp_secrets (subject, secret, confirmed_at, last_counter, created_at) VALUES (?, ?, '', 0, ?)`,
		subject, sealed, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return Enrolment{}, err
	}
	return h.enrolment(secret, account)
}

// Pending returns the enrolment Begin started for subject and nobody
// has confirmed yet — so a set-up page can be a GET that shows the
// same QR again after a refresh, with Begin behind a POST. ok is
// false when nothing is pending (or the factor is already live).
func (h *Handlers) Pending(subject, account string) (Enrolment, bool, error) {
	var sealed []byte
	err := h.cfg.DB.QueryRow(
		`SELECT secret FROM totp_secrets WHERE subject = ? AND confirmed_at = ''`, subject).Scan(&sealed)
	if err == sql.ErrNoRows {
		return Enrolment{}, false, nil
	}
	if err != nil {
		return Enrolment{}, false, err
	}
	secret, err := crypto.OpenSym(h.cfg.Key, sealed)
	if err != nil {
		return Enrolment{}, false, err
	}
	en, err := h.enrolment(secret, account)
	return en, err == nil, err
}

// enrolment is what Begin and Pending both show for one secret.
func (h *Handlers) enrolment(secret []byte, account string) (Enrolment, error) {
	key := b32.EncodeToString(secret)
	uri := keyURI(h.cfg.Issuer, account, key)
	svg, err := qrSVG(uri, "QR code for your authenticator app")
	if err != nil {
		return Enrolment{}, err
	}
	return Enrolment{Key: key, URI: uri, QR: svg}, nil
}

// Confirm checks one code against subject's UNCONFIRMED secret and,
// if it verifies, makes the factor live. false means the code did
// not verify (or nothing is pending confirmation, or the subject has
// used up its budget); the page says "try again" either way.
func (h *Handlers) Confirm(subject, submitted string) (bool, error) {
	if h.limit.blocked(subject, time.Now()) {
		return false, nil
	}
	step, err := h.verify(subject, submitted, false)
	if err != nil {
		return false, err
	}
	if step < 0 {
		h.limit.fail(subject, time.Now())
		return false, nil
	}
	h.limit.clear(subject)
	_, err = h.cfg.DB.Exec(
		`UPDATE totp_secrets SET confirmed_at = ?, last_counter = ? WHERE subject = ?`,
		time.Now().UTC().Format(time.RFC3339), step, subject)
	return err == nil, err
}

// Disable removes subject's authenticator, confirmed or not. From a
// page behind sessions.RequireFresh, like every change to how an
// account is entered.
func (h *Handlers) Disable(subject string) error {
	_, err := h.cfg.DB.Exec(`DELETE FROM totp_secrets WHERE subject = ?`, subject)
	return err
}

// verify checks submitted against subject's secret — the confirmed
// one when confirmed is set, the pending one otherwise — and on a hit
// records the step so it cannot be replayed. Returns the step, or -1.
func (h *Handlers) verify(subject, submitted string, confirmed bool) (int64, error) {
	var sealed []byte
	var last int64
	var confirmedAt string
	err := h.cfg.DB.QueryRow(
		`SELECT secret, last_counter, confirmed_at FROM totp_secrets WHERE subject = ?`, subject).
		Scan(&sealed, &last, &confirmedAt)
	if err == sql.ErrNoRows || (err == nil && (confirmedAt != "") != confirmed) {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	secret, err := crypto.OpenSym(h.cfg.Key, sealed)
	if err != nil {
		return -1, err
	}
	step := match(secret, submitted, time.Now(), last)
	if step < 0 || !confirmed {
		return step, nil
	}
	// Spend the step: a raced second submission of the same code
	// finds last_counter already past it and misses.
	res, err := h.cfg.DB.Exec(
		`UPDATE totp_secrets SET last_counter = ? WHERE subject = ? AND last_counter < ?`, step, subject, step)
	if err != nil {
		return -1, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return -1, nil
	}
	return step, nil
}

// SignIn is POST /totp/signin: verify the form's "code" against the
// pending half-session's subject and complete it through the Gate —
// the real session is the first factor's method plus "+totp". A miss
// is a Strike: the half-session survives until its fifth, then the
// person starts again from the first factor. Misses redirect to the
// Gate's confirm page with ?totp=failed, or ?totp=exhausted once the
// budget is spent; which digits were wrong is never said.
func (h *Handlers) SignIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.Gate == nil {
		http.Error(w, "signed out", http.StatusForbidden)
		return
	}
	p, ok := h.cfg.Gate.Pending(r)
	if !ok {
		http.Error(w, "signed out", http.StatusForbidden)
		return
	}
	step, err := h.verify(p.Subject, r.PostFormValue("code"), true)
	if err != nil {
		h.fail(w, "verify", err)
		return
	}
	if step < 0 {
		h.cfg.Logger.Warn("rastrillo/totp: sign-in code refused")
		to := h.cfg.Gate.ConfirmPath() + "?totp=failed"
		if err := h.cfg.Gate.Strike(w, p); errors.Is(err, secondfactor.ErrExhausted) {
			to = h.cfg.Gate.ConfirmPath() + "?totp=exhausted"
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	if err := h.cfg.Gate.Complete(w, r, p, Method); err != nil {
		if errors.Is(err, secondfactor.ErrConsumed) {
			http.Redirect(w, r, h.cfg.Gate.ConfirmPath()+"?totp=failed", http.StatusSeeOther)
			return
		}
		h.fail(w, "mint session", err)
		return
	}
	http.Redirect(w, r, p.ReturnTo, http.StatusSeeOther)
}

// StepUp is POST /totp/stepup: verify the form's "code" for the
// caller — whose session may be stale; that is the point — and rotate
// the session fresh, Method "totp", AuthTime now, which is what
// satisfies sessions.RequireFresh again. Success redirects to the
// form's same-site return_to (else "/"); a miss to StepUpFailedPath
// with return_to carried along, after the per-subject budget.
func (h *Handlers) StepUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := sessions.Current(r)
	if !ok {
		sess, ok = h.cfg.Sessions.From(r)
	}
	if !ok {
		http.Error(w, "signed out", http.StatusForbidden)
		return
	}
	failed := h.cfg.StepUpFailedPath
	if to := sessions.SafeReturn(r, ""); to != "" {
		failed += "&return_to=" + url.QueryEscape(to)
	}
	now := time.Now()
	if h.limit.blocked(sess.Subject, now) {
		http.Redirect(w, r, failed, http.StatusSeeOther)
		return
	}
	step, err := h.verify(sess.Subject, r.PostFormValue("code"), true)
	if err != nil {
		h.fail(w, "verify", err)
		return
	}
	if step < 0 {
		h.limit.fail(sess.Subject, now)
		h.cfg.Logger.Warn("rastrillo/totp: step-up code refused")
		http.Redirect(w, r, failed, http.StatusSeeOther)
		return
	}
	h.limit.clear(sess.Subject)
	if err := h.cfg.Sessions.SignIn(w, r, sessions.Session{Subject: sess.Subject, Method: Method, AuthTime: now}); err != nil {
		h.fail(w, "rotate session", err)
		return
	}
	http.Redirect(w, r, sessions.SafeReturn(r, "/"), http.StatusSeeOther)
}

func (h *Handlers) fail(w http.ResponseWriter, what string, err error) {
	h.cfg.Logger.Error("rastrillo/totp: "+what, "err", err)
	http.Error(w, "something went wrong", http.StatusInternalServerError)
}

// ── the step-up and enrolment budget ────────────────────────────────

const (
	limitFailures = 10
	limitWindow   = 15 * time.Minute
)

// limiter is a per-subject sliding window of failures, in memory:
// the password plugin's shape. It guards the two paths that verify
// against a session or a settings page rather than a half-session,
// where secondfactor's Strike does the same job.
type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{failures: map[string][]time.Time{}} }

func (l *limiter) prune(key string, now time.Time) []time.Time {
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if now.Sub(t) < limitWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

func (l *limiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, now)) >= limitFailures
}

func (l *limiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key, now), now)
}

func (l *limiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
