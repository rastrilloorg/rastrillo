package oidc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// ConnectionKey is the app's own name for which tenant's registration a
// Connection is — opaque to this package. "" for a single-tenant app.
// Read once at Begin (Config.ConnectionKey) and sealed into the ceremony,
// so Return resolves the SAME tenant with nothing taken from the
// provider's own callback request — design D3/D4, review finding 4.
type ConnectionKey string

// Config configures New. Sessions, InstanceKey, Connections and Admit are
// required; everything else has a serviceable default or is nil to mean
// "off".
type Config struct {
	// Sessions is the shared session core this app already built.
	// Begin/Return mint into it exactly as password and auth do
	// (Sessions.SignIn) — this package contains no branch on whether it
	// was built in ordinary (DB) mode or Signed (hub) mode; see Admit's
	// doc comment and SecondFactor's for the two places that distinction
	// still shows through.
	Sessions *sessions.Sessions

	// InstanceKey seals the ceremony cookie (crypto.Derive, crypto.SealSym).
	// Must be at least 32 bytes: auth.Config.InstanceKey refuses an EMPTY
	// key because an empty input hashes to one fixed, publicly-computable
	// value; this package adds a length floor on top of that, since
	// crypto.Derive has none of its own and a short key is a short key
	// whether or not it happens to be the empty string.
	InstanceKey string

	// Connections resolves a provider kind and tenant key to this app's
	// registration with it. ok=false means this app has no such
	// connection and Begin/Return answer 404/the generic failure page
	// rather than starting or continuing a ceremony nothing can finish.
	// An error is a failure to answer the question at all (a config
	// store that is down) and is answered 500, never folded into ok=false.
	Connections func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error)

	// ConnectionKey picks which tenant Begin is starting a ceremony for.
	// Nil means every ceremony uses "" — the single-tenant answer. Called
	// exactly once, at Begin; Return never calls it again, reading the
	// key sealed into the ceremony instead, because a hub's tenants are
	// routed by PATH on one shared origin (sessions' Signed mode) and the
	// provider's own callback carries nothing that would let Return
	// re-derive it the same way.
	ConnectionKey func(r *http.Request) ConnectionKey

	// Admit maps a verified, already-domain-checked identity (Admitted's
	// core refusals have already run) to this app's own subject, or
	// refuses it — ips's D88.8: "the directory says who somebody is;
	// this instance still says whether they work here." Required: New
	// refuses a nil Admit.
	//
	// The returned subject becomes sessions.Session.Subject, which
	// decides more than this one session row:
	//
	//   - In Signed (hub) mode a subject must satisfy an opaque-identifier
	//     shape — never an address — or sessions.SignIn fails. An app
	//     running hub mode cannot return a raw email here.
	//   - secondfactor.Gate decides whether someone has a passkey or TOTP
	//     enrolled by looking up THIS subject. An app with password or
	//     keymail sign-in already wired MUST return the SAME subject
	//     those use for the same person — a different spelling here is
	//     an enrolled second factor silently skipped, on the one door
	//     that bypasses a password.
	//
	// An error means something failed (a membership lookup that could
	// not run) and answers 500. A refusal — wrap or return ErrNotAdmitted
	// — answers through Refused: one sentence, safe to show because the
	// caller has just proved the address is theirs to their own
	// organisation's directory (ips's D88.8 reasoning).
	Admit func(ctx context.Context, conn Connection, id Identity) (subject string, err error)

	// SecondFactor is the sign-in-time 2FA seam, called at the exact
	// point a verified-and-admitted identity would mint a session, with
	// the session that WOULD be minted — the same contract auth.Config
	// and password.Config already use (wire
	// passkeyHandlers.Gate.Hold). Nil means no second factor.
	//
	// This package cannot detect, from here, whether Config.Sessions was
	// built in Signed (hub) mode — sessions exposes no accessor for it —
	// so it cannot refuse the combination at New the way this design
	// would prefer to. What is true today: secondfactor's own "+factor"
	// Method suffix does not fit Signed mode's key-id alphabet, so
	// running SecondFactor against a Signed-mode Sessions fails CLOSED
	// at sign-in time (Sessions.SignIn returns an error, logged, no
	// session minted) rather than silently shipping a broken
	// combination — but it fails late, not at boot. Raised as a question
	// for sessions/secondfactor in the design's Open questions; do not
	// wire both together until that lands.
	SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)

	// Refused answers an identity that verified but was not admitted —
	// Admitted's own core refusals (no address, denied-verified, wrong
	// organisation), Admit's refusal, or a step-up that the provider did
	// not actually satisfy freshly (D6). Nil gets a bare-text 403 naming
	// nothing beyond "not admitted here". Only someone who has just
	// proved they hold the address reaches this, so naming the reason is
	// no oracle for anybody else — see the design's error-path table for
	// every OTHER failure, none of which reaches here.
	Refused func(w http.ResponseWriter, r *http.Request, id Identity, reason error)

	// SignedInPath is where a fresh session lands absent a same-site
	// return_to carried through the ceremony. Default "/".
	SignedInPath string

	Logger *slog.Logger

	// Now is the clock. Nil: time.Now. Test seam.
	Now func() time.Time

	// HTTP is passed through to this package's Engine. Nil: the
	// Engine's own default (15s timeout, redirects disabled).
	HTTP *http.Client
}

// ErrStepUpNotFresh means Begin was asked for a fresh re-authentication
// (RequireFresh's ?reauth=1 convention) and the provider's own auth_time
// does not show one happened within the last minute — design D6, review
// finding 8. prompt=login&max_age=0 ASKS a provider to force this; it
// does not guarantee one, since some providers answer an already-
// interactive session with a stale auth_time regardless. Answered
// through Refused, not minted as a session that would only loop at the
// next RequireFresh check.
var ErrStepUpNotFresh = errors.New("oidc: the provider did not confirm a fresh sign-in")

// freshWindow is how recent identity.AuthTime must be to satisfy a
// step-up request. One minute: generous enough for the round trip to the
// provider and back, narrow enough that an old auth_time clearly was not
// produced by the prompt=login this request just sent.
const freshWindow = time.Minute

// New validates cfg and returns a ready *Handlers.
func New(cfg Config) (*Handlers, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("oidc: Config.Sessions is required")
	}
	if len(cfg.InstanceKey) < 32 {
		return nil, errors.New("oidc: Config.InstanceKey must be at least 32 bytes")
	}
	if cfg.Connections == nil {
		return nil, errors.New("oidc: Config.Connections is required")
	}
	if cfg.Admit == nil {
		return nil, errors.New("oidc: Config.Admit is required")
	}
	if cfg.SignedInPath == "" {
		cfg.SignedInPath = "/"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Handlers{
		cfg:    cfg,
		engine: &Engine{HTTP: cfg.HTTP, Now: cfg.Now},
	}, nil
}

// Handlers is the wired plugin: an app builds one at boot (New) and
// mounts Begin and Return itself, exactly as it would password's or
// auth's handlers. Build exactly one per process and share it — the
// Engine's discovery/key caches live on the value.
type Handlers struct {
	cfg    Config
	engine *Engine
}

func (h *Handlers) now() time.Time { return h.cfg.Now() }

// secureCookies reports whether this app's cookies should carry the
// __Host- prefix and Secure, read off the SAME decision
// Config.Sessions already made for the session cookie — asking a second
// config field (an Origin of this package's own) would be one more thing
// that can disagree with it, which is the trap this avoids.
func (h *Handlers) secureCookies() bool {
	return strings.HasPrefix(h.cfg.Sessions.CookieName(), "__Host-")
}

// ceremonyCookieName is per PROVIDER KIND (design D4, review finding 9):
// two simultaneous ceremonies for DIFFERENT providers, two browser tabs,
// must not overwrite each other. Two ceremonies for the SAME provider at
// once still collide, which is accepted — the second Begin simply wins,
// and the first tab's Return then meets a ceremony sealed with its own
// state/nonce no longer present, which fails CheckState and shows the
// generic failure page, a safe (if inconvenient) outcome.
func (h *Handlers) ceremonyCookieName(kind Kind) string {
	base := "rastrillo_oidc_ceremony_" + string(kind)
	if h.secureCookies() {
		return "__Host-" + base
	}
	return base
}

func (h *Handlers) setCeremonyCookie(w http.ResponseWriter, kind Kind, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: h.ceremonyCookieName(kind), Value: value, Path: "/",
		MaxAge: int(ceremonyTTL / time.Second), HttpOnly: true,
		// Lax, not Strict: the return leg is a top-level GET navigation
		// from the PROVIDER's own origin, which Strict would not send
		// the cookie on — every sign-in would fail as an expired
		// ceremony (ips's D88.5).
		SameSite: http.SameSiteLaxMode, Secure: h.secureCookies(),
	})
}

func (h *Handlers) clearCeremonyCookie(w http.ResponseWriter, kind Kind) {
	http.SetCookie(w, &http.Cookie{
		Name: h.ceremonyCookieName(kind), Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: h.secureCookies(),
	})
}

// Begin is GET /signin/oidc/{provider} (the app mounts the path; the
// {provider} URL parameter name is this package's own, read via
// chi.URLParam).
func (h *Handlers) Begin(w http.ResponseWriter, r *http.Request) {
	kind := Kind(chi.URLParam(r, "provider"))
	p, ok := ProviderFor(kind)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var key ConnectionKey
	if h.cfg.ConnectionKey != nil {
		key = h.cfg.ConnectionKey(r)
	}
	conn, ok, err := h.cfg.Connections(r.Context(), kind, key)
	if err != nil {
		h.serverError(w, "connections lookup", err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if p.NeedsHostedDomain && len(conn.AllowedDomains) == 0 {
		// A misconfigured connection, not a caller's fault, but
		// something must not silently start a ceremony that Admitted
		// will refuse for every account that could ever complete it.
		h.serverError(w, "connection", fmt.Errorf("connection for %s has no AllowedDomains, required by this provider", kind))
		return
	}

	meta, err := h.engine.Discover(r.Context(), conn.Issuer)
	if err != nil {
		h.cfg.Logger.Error("oidc: discovery failed", "provider", kind, "err", err)
		h.ceremonyFailedPage(w, r)
		return
	}

	start, err := NewStart()
	if err != nil {
		h.serverError(w, "mint ceremony", err)
		return
	}

	reauth := r.URL.Query().Get("reauth") == "1"
	prompt := "select_account"
	if reauth {
		prompt = "login"
	}
	authURL := AuthCodeURL(meta, conn, start, prompt)
	if reauth {
		// max_age=0 ASKS the provider to force interactive
		// re-authentication; Return still checks the resulting
		// auth_time itself (D6) because not every provider honours
		// this.
		authURL += "&max_age=0"
	}

	now := h.now()
	cer := ceremony{
		Start:    start,
		Key:      key,
		Reauth:   reauth,
		ReturnTo: sessions.SafeReturn(r, ""),
		Expires:  now.Add(ceremonyTTL),
	}
	sealed, err := sealCeremony(h.cfg.InstanceKey, kind, cer)
	if err != nil {
		h.serverError(w, "seal ceremony", err)
		return
	}
	h.setCeremonyCookie(w, kind, sealed)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// Return is GET /signin/oidc/{provider}/return.
func (h *Handlers) Return(w http.ResponseWriter, r *http.Request) {
	kind := Kind(chi.URLParam(r, "provider"))
	p, ok := ProviderFor(kind)
	if !ok {
		http.NotFound(w, r)
		return
	}

	cookie, cookieErr := r.Cookie(h.ceremonyCookieName(kind))
	// Cleared immediately, on every outcome from here on: a ceremony
	// cookie is single-use, and nothing below this line re-reads it.
	h.clearCeremonyCookie(w, kind)
	if cookieErr != nil {
		h.ceremonyFailedPage(w, r)
		return
	}
	cer, err := openCeremony(h.cfg.InstanceKey, kind, cookie.Value, h.now())
	if err != nil {
		h.ceremonyFailedPage(w, r)
		return
	}

	if provErr := r.URL.Query().Get("error"); provErr != "" {
		h.cfg.Logger.Warn("oidc: provider returned an error", "provider", kind,
			"error", provErr, "description", r.URL.Query().Get("error_description"))
		h.ceremonyFailedPage(w, r)
		return
	}
	if err := cer.CheckState(r.URL.Query().Get("state")); err != nil {
		h.ceremonyFailedPage(w, r)
		return
	}

	// Resolved AGAIN, with the tenant key sealed into the ceremony at
	// Begin — nothing here is taken from the provider's callback, which
	// carries no tenant information at all (design D3/D4, finding 4).
	conn, ok, err := h.cfg.Connections(r.Context(), kind, cer.Key)
	if err != nil {
		h.serverError(w, "connections lookup", err)
		return
	}
	if !ok {
		// The connection existed at Begin and was removed before Return
		// — treated the same as any other ceremony failure, not
		// disclosed as "this tenant no longer exists".
		h.ceremonyFailedPage(w, r)
		return
	}

	meta, err := h.engine.Discover(r.Context(), conn.Issuer)
	if err != nil {
		h.cfg.Logger.Error("oidc: discovery failed", "provider", kind, "err", err)
		h.ceremonyFailedPage(w, r)
		return
	}

	code := r.URL.Query().Get("code")
	identity, err := h.engine.Exchange(r.Context(), meta, conn, code, cer.Start)
	if err != nil {
		h.cfg.Logger.Error("oidc: token exchange or verification failed", "provider", kind, "err", err)
		h.ceremonyFailedPage(w, r)
		return
	}

	if cer.Reauth {
		if identity.AuthTime.IsZero() || h.now().Sub(identity.AuthTime) > freshWindow {
			h.refuse(w, r, identity, ErrStepUpNotFresh)
			return
		}
	}

	if err := Admitted(p, conn, identity); err != nil {
		h.refuse(w, r, identity, err)
		return
	}

	subject, err := h.cfg.Admit(r.Context(), conn, identity)
	if err != nil {
		if errors.Is(err, ErrNotAdmitted) {
			h.refuse(w, r, identity, err)
			return
		}
		h.serverError(w, "admit", err)
		return
	}

	// AuthTime is clamped to this server's own clock (never taken on
	// faith from the provider) for two reasons: Signed (hub) mode
	// refuses a session whose AuthTime is after its own issue time
	// (review finding 2), and in ordinary mode a provider clock running
	// even slightly ahead should not be allowed to assert a sign-in that
	// — from this server's own point of view — has not happened yet.
	authTime := identity.AuthTime
	now := h.now()
	if !authTime.IsZero() && authTime.After(now) {
		authTime = now
	}

	sess := sessions.Session{
		Subject: subject,
		// Hyphen, not colon: Signed mode's Method alphabet is
		// [a-z0-9_-]{1,32} (review finding 2). "oidc-microsoft" /
		// "oidc-google".
		Method:   "oidc-" + string(kind),
		AuthTime: authTime,
	}

	// Admit runs BEFORE SecondFactor, always (design D5, review finding
	// 6): calling SecondFactor first would let an identity Admit would
	// have refused reach a pending half-session that secondfactor.Gate
	// later promotes to a real one with no further admission check. By
	// the time execution reaches here, Admitted and Admit have both
	// already run and succeeded.
	if h.cfg.SecondFactor != nil {
		done, err := h.cfg.SecondFactor(w, r, sess)
		if err != nil {
			h.serverError(w, "second factor", err)
			return
		}
		if done {
			return
		}
	}

	if err := h.cfg.Sessions.SignIn(w, r, sess); err != nil {
		h.serverError(w, "sign in", err)
		return
	}

	target := cer.ReturnTo
	if target == "" {
		target = h.cfg.SignedInPath
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// refuse answers an identity that verified but was not admitted — the
// one case ips's D88.8 says is safe to be specific with, because only
// someone who has just proved the address is theirs reaches it.
func (h *Handlers) refuse(w http.ResponseWriter, r *http.Request, id Identity, reason error) {
	h.cfg.Logger.Info("oidc: sign-in refused", "subject", id.Subject, "reason", reason)
	if h.cfg.Refused != nil {
		h.cfg.Refused(w, r, id, reason)
		return
	}
	http.Error(w, "This address is verified but not admitted here.", http.StatusForbidden)
}

// ceremonyFailedPage is every row of the error-path table whose caller is
// anonymous or whose identity is not yet established: a generic page,
// nothing provider-specific disclosed, the detail already logged by the
// caller above. "That sign-in took too long, or could not be completed —
// try again."
func (h *Handlers) ceremonyFailedPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "That sign-in took too long, or could not be completed. Try again.", http.StatusBadRequest)
}

func (h *Handlers) serverError(w http.ResponseWriter, stage string, err error) {
	h.cfg.Logger.Error("oidc: "+stage, "err", err)
	http.Error(w, "sign-in failed", http.StatusInternalServerError)
}
