// Package auth is the framework's turnkey sign-in and the family
// default: a magic-link email that works for every address,
// auto-upgrading to the keymail ceremony when the address resolves to
// a claimed keymail inbox. It wraps github.com/keymaildev/signin
// (which owns the ceremony) the way seapointish's reviewed integration
// wires it — the deliberate holes signin leaves (link storage, mailer,
// cookies, sessions, CSRF, admission) filled once, here, instead of
// once per app.
//
// The shape: an app builds one *Auth at boot (New), merges auth.Schema
// into its migrate.Set — migrate.Merge(sessions.Schema, auth.Schema),
// since auth's backfill migration reads the sessions table — and
// mounts four handlers —
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
// The shipped sign-in screen (ui's signin partial) is opt-in:
// Config.SigninScreen. With it on, SigninState reads what the page
// shows, PrepareSigninResponse writes the headers and cookie deletions
// that go with it, Forget is "Use a different email", and the page
// renders the rest. With it off nothing about Begin or Callback changes.
//
// The decision tree: every submitted address is classified; a claimed
// keymail inbox gets the keymail-OAuth ceremony (the upgrade), and
// every other address — and every classification failure, which fails
// open by design — gets a signed link by email. Nobody is ever locked
// out by the upgrade path existing.
//
// Hardening beyond the default is a seam, not a different plugin.
// Step-up: RequireFreshSession demands a recently-verified credential
// for sensitive routes (keymail deployments advertising reauth get a
// real prompt=login ceremony), and the passkey package wires a
// WebAuthn second factor onto that seam — enroll while signed in,
// then satisfy step-up with an assertion instead of a full
// re-sign-in. Sign-in-time 2FA: Config.SecondFactor, called where a
// verified first factor would mint the session — wire
// passkey.Handlers.Gate there and an enrolled account must assert
// before any session exists (the pending half-session; see the
// passkey package doc). Accounts with nothing enrolled sign in
// exactly as before either way.
package auth

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/carlos"
	"amadan.net/rastrillo/rastrillo/clientip"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
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
	// DB is the app's database. migrate.Merge(sessions.Schema,
	// auth.Schema) must be applied (via migrate.Apply) before any
	// handler runs.
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

	// Proof is sign-in's front door: Begin runs it before the rate
	// limiter, classification and any mail, so an anonymous visitor
	// cannot make the server probe a domain of their choosing, send a
	// link, or spend another address's budget without solving. Build one
	// pow.Guard and share it with password; scope keeps their tokens
	// apart. Required unless ProofOff: an app that upgraded without
	// wiring it must fail at boot, not refuse every visitor in
	// production.
	Proof *pow.Guard
	// ProofOff runs sign-in without the front door.
	ProofOff bool

	// Mailer delivers magic links. Nil falls back to mail.Logged with a
	// warning — the emailed link is a live credential, so the fallback
	// is dev-only and says so on every send.
	Mailer mail.Sender

	// Authorize is the admission gate: given a verified address, may it
	// have a session? Nil admits every verified address. Membership
	// models (tables, roles, admin bootstrap) are app policy layered on
	// this hook.
	Authorize func(address string) bool

	// SubjectFor decides what a minted session is keyed by. Nil — the
	// default — keys it by the verified address, which is what every
	// app wanting readable session rows should keep.
	//
	// It exists for the app that must not store a readable address at
	// rest: return an opaque person ref (an HMAC of the address under
	// a pepper the app holds, a row id from its own directory) and the
	// address stops reaching the sessions table, and with it every
	// table keyed off the subject — passkey credentials, challenges,
	// pending enrollments and recovery codes all inherit whatever this
	// returns. A gate that greps the raw database file is the point:
	// SQLite does not zero deleted pages and a replicated WAL ships
	// frames continuously, so "write it and clear it later" reaches
	// the bucket regardless. The address must never be written.
	//
	// Two consequences worth knowing before setting it. Identity
	// (auth.From, sessions.Current) carries this value in its Address
	// field, so an app that remaps has no readable address at hand
	// from the session alone — that is the trade being made. And
	// Authorize is unaffected: admission answers a question about an
	// address, and it still receives the verified address itself.
	//
	// It does not make the plugin server-blind on its own. The link
	// store (auth_links) still holds the address at rest between
	// sending a magic link and its click, because the link must
	// survive a restart; sealing that store is separate work.
	//
	// An error refuses the sign-in — no session is minted and no
	// address is written as a fallback.
	SubjectFor func(address string) (string, error)

	// SecondFactor is the sign-in-time 2FA seam: called at the exact
	// point a verified first factor would mint the session, with the
	// session that WOULD be minted. done=true means the hook took over
	// the response (stored a pending half-session and redirected —
	// passkey.Handlers.Gate is the shipped implementation); done=false
	// means no second factor applies and sign-in proceeds unchanged.
	// Nil is exactly today's behavior. The hook keeps this plugin
	// ignorant of any particular factor — anything with this signature
	// can gate.
	SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)

	// Refused answers a first factor that verified but that Authorize
	// would not admit. Nil keeps the plugin's own answer, a bare-text
	// 403 "This address is verified but not admitted here." — which is
	// no page an app would show anybody. An app sets this to render
	// its own sign-in page with an error and a way to try a different
	// address (a redirect to SigninPath with a query of its own is
	// enough). It is called with the verified identity and must write
	// the whole response; no session is minted, and the attempt is
	// already over. Only someone who has just proved they hold the
	// address reaches it, so saying "not admitted" here is no oracle.
	Refused func(w http.ResponseWriter, r *http.Request, id Identity)

	// TrustedProxyHops is how many proxies you run in front of the app,
	// which decides the address the per-IP sign-in budget counts:
	// clientip.From reads that many elements from the right of
	// X-Forwarded-For. Nil — the default — means 1 when the app runs on
	// CARLOS (carlos.Running), whose edge adds exactly one element (the
	// visitor), and 0 anywhere else: ignore the header and count the
	// connection's peer, which behind a proxy of your own means every
	// visitor shares the proxy's budget — limits bite sooner, never
	// later. A value you set always wins, 0 included: a pointer, because
	// an explicit 0 must be told apart from unset. Never set it higher
	// than the proxies really there: one too many trusts an element the
	// client wrote, and a forged address per request is an unlimited
	// budget.
	TrustedProxyHops *int

	// SigninPath is the app's sign-in page, the target of outcome
	// redirects. Default "/signin".
	SigninPath string

	// SignedInPath is where a fresh session lands. Default "/".
	SignedInPath string

	// Subject is the magic-link email subject. Default "Your sign-in
	// link".
	Subject string

	// Body writes the magic-link email around the link, so an app can
	// name itself and speak in its own voice. Nil sends DefaultBody. A
	// Body that states the lifetime should take it from LinkTTL.
	Body func(link string) string

	// SessionTTL is the minted session's lifetime. Default
	// DefaultSessionTTL.
	SessionTTL time.Duration

	// SigninScreen says this app renders the shipped sign-in screen (ui's
	// signin partial, fed by SigninState). It turns on everything the
	// screen needs from auth at once: the attempt cookie that lets the
	// screen prefill an address and say where a link went, the keymail
	// continuation that keeps form-action 'self' strict, and remembering
	// the way in. Default false, and false is exactly the behaviour
	// before the screen existed: no new cookie is written, read or
	// deleted, and Begin and Callback answer as they always did. One
	// switch rather than three because they are one feature — an app
	// remembering the way in without the screen would be writing a
	// cookie nothing reads.
	SigninScreen bool

	// BeginPath and ForgetPath are where the app mounted Begin (POST) and
	// Forget (POST), for the screen's form actions. auth mounts nothing
	// itself, and neither can be inferred from SigninPath: an app may
	// mount Begin at /auth/begin. Defaults "/signin" and
	// "/signin/forget".
	BeginPath  string
	ForgetPath string

	// Remember switches off one part of SigninScreen: a pointer to false
	// keeps the screen and stops remembering the way in (a shared
	// kiosk), and deletes what was remembered before. Nil or true leaves
	// it on. With SigninScreen off it has no effect — nothing is
	// remembered to begin with. A pointer because an explicit false must
	// be told apart from unset.
	Remember *bool

	// KeymailServers is the closed set of keymail servers (host or
	// host:port, compared ignoring case, one trailing dot and an
	// explicit :443) this app will classify against or exchange a code
	// with. An IPv6 host must be written in brackets ("[::1]" or
	// "[::1]:8443"); unbracketed, its colons collide with the port
	// separator and let one entry match more than the operator wrote.
	//
	// Nil (the default, unset) means any server an address's own
	// _keymail delegation names — which is keymail's protocol: the
	// domain's owner chooses its server, the same party that controls
	// its MX and could receive a magic link anyway, and a server cannot
	// vouch for anyone else's address because Callback compares the
	// address it returns with the one the flow started for. An address
	// whose server is not listed gets a magic link and its server is
	// never contacted.
	//
	// A non-nil empty slice ([]string{}) means none: every address
	// classifies as "not keymail" without a server ever being dialed —
	// no DNS delegation lookup, no HTTPS probe, to the address's own
	// domain or anywhere else. Every sign-in is plain magic-link. Set
	// this, not nil, for an app with no real keymail federation
	// partners: left unset, the classifier still looks up each domain's
	// _keymail delegation and probes the server it names, or the domain
	// itself when DNS says it has none. A server that accepts the
	// connection without ever answering costs the full classify
	// timeout, and the classifier remembers a "not keymail" answer for
	// only a minute, so in a quiet app that is nearly every sign-in
	// from that domain.
	//
	// Copied at New; changing it means a restart, which also empties
	// the classifier's caches. It applies whether or not SigninScreen
	// is on.
	KeymailServers []string

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
	// hops is Config.TrustedProxyHops resolved once, at New: the
	// environment is read at boot, not per request.
	hops int
	// jar holds the remembered way in and ends an attempt; passkey gets
	// the same one through RememberJar.
	jar *lastsignin.Jar
	// attemptKey seals the attempt cookie. Its own derivation so it
	// opens nothing else and nothing else opens it.
	attemptKey []byte
	// continueKey seals the continuation cookie; its own derivation for
	// the same reason as attemptKey.
	continueKey []byte
	// now is the clock the screen's cookies are sealed and judged by.
	// time.Now outside tests.
	now func() time.Time
	// servers is KeymailServers as a set, nil for "any server"; guard
	// enforces it on both clients, and the authorize-URL predicate
	// checks it again.
	servers map[string]bool
	guard   *hostGuard
	// exchangeHTTP is the token-exchange client: nil — the library's own
	// default — unless KeymailServers asked for a guard.
	exchangeHTTP *http.Client
}

// DefaultBody is the magic-link email when Config.Body is nil. It
// replaces keymaildev/signin's own default, which says the link "expires
// shortly": every sign-in page in front of it says how long, and the
// email has to agree with them.
func DefaultBody(link string) string {
	return "Open this link to sign in:\n\n" + link + "\n\nIt works once and expires in " +
		strconv.Itoa(int(LinkTTL/time.Minute)) + " minutes. If you didn’t ask for it, you can ignore this email."
}

// ErrEmptyInstanceKey means Config.InstanceKey was empty — see the
// field's comment for why this is fatal rather than defaulted.
var ErrEmptyInstanceKey = errors.New("rastrillo/auth: Config.InstanceKey must not be empty")

// ErrProofUnset: Config has neither Proof nor ProofOff.
var ErrProofUnset = errors.New("rastrillo/auth: Config.Proof is unset: build a pow.Guard (serve pow.Assets(), apply pow.Schema) and set Config.Proof, or set Config.ProofOff")

// ErrProofMode: the Guard binds its work to an input, and the sign-in
// forms carry the address in three different shapes (two hidden
// inputs and a field), none of them bound.
var ErrProofMode = errors.New("rastrillo/auth: Config.Proof is a bound pow.Guard; sign-in needs an unbound one")

// ProofScope is the scope Begin's challenges are issued and checked
// under. A Guard shared with other forms keeps their tokens apart by
// it.
const ProofScope = "rastrillo/auth/begin"

// New wires the one long-lived flow: explicit in-memory rate limiter,
// derived pending key, the keymail client bound to Origin, the link
// store over cfg.DB, and a stock classifier (signin v0.1.1 probes
// keymail's real /api/federation/lookup route, so the RoundTripper
// rewrite this package carried since v0.6.0 is gone).
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
	switch {
	case cfg.Proof == nil && !cfg.ProofOff:
		return nil, ErrProofUnset
	case cfg.Proof != nil && cfg.ProofOff:
		return nil, errors.New("rastrillo/auth: Config.Proof and Config.ProofOff are both set; choose one")
	case cfg.Proof != nil && cfg.Proof.Bound():
		return nil, ErrProofMode
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
	if cfg.Body == nil {
		cfg.Body = DefaultBody
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.BeginPath == "" {
		cfg.BeginPath = "/signin"
	}
	if cfg.ForgetPath == "" {
		cfg.ForgetPath = "/signin/forget"
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

	a := &Auth{
		cfg: cfg, sessions: sess, hops: trustedHops(cfg.TrustedProxyHops, carlos.Running()),
		now:         time.Now,
		attemptKey:  crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/auth/attempt/v1"),
		continueKey: crypto.Derive([]byte(cfg.InstanceKey), "rastrillo/auth/continue/v1"),
	}
	jar, err := lastsignin.New(lastsignin.Config{
		Origin: cfg.Origin, InstanceKey: cfg.InstanceKey,
		AttemptCookie: a.attemptCookie(), Mode: rememberMode(cfg),
		// Through a.now, not time.Now, so a test that moves auth's clock
		// moves the jar's too and the two never disagree about expiry.
		Now: func() time.Time { return a.now() },
	})
	if err != nil {
		return nil, err
	}
	a.jar = jar

	servers, err := keymailServers(cfg.KeymailServers)
	if err != nil {
		return nil, err
	}
	a.servers = servers
	classifier := &signin.Classifier{}
	if servers != nil {
		a.guard = &hostGuard{allow: servers}
		classifier.HTTP = &http.Client{Transport: a.guard, Timeout: classifyTimeout}
		a.exchangeHTTP = &http.Client{Transport: a.guard, Timeout: exchangeTimeout}
		if len(servers) == 0 {
			// An empty allow set refuses every server the guard could
			// ever be shown, so nothing a real _keymail TXT lookup
			// found would survive it. Short-circuit the lookup itself
			// instead: neverDelegates answers with a plain (non-"not
			// found") error, which classify.go's delegate() reads as
			// "names nothing" and skips the well-known probe outright
			// — neither a DNS query nor an HTTP request is ever built.
			// Relying on the guard alone would still be correct, but
			// would pay a real DNS round trip whose answer can never
			// matter.
			classifier.LookupTXT = neverDelegates
		}
	}

	a.flow = &signin.Flow{
		Classifier: classifier,
		Keymail: func(server string) *signin.Keymail {
			return &signin.Keymail{
				Base: "https://" + server, Origin: cfg.Origin,
				RedirectPath: callbackPath, HTTP: a.exchangeHTTP,
			}
		},
		Links:    &linkStore{db: cfg.DB},
		Mailer:   cfg.Mailer,
		Limiter:  signin.NewMemoryLimiter(15 * time.Minute),
		Key:      sha256.Sum256([]byte("rastrillo/auth/pending\x00" + cfg.InstanceKey)),
		Origin:   cfg.Origin,
		LinkPath: "/auth/verify",
		Subject:  cfg.Subject,
		Body:     cfg.Body,
		LinkTTL:  LinkTTL,
	}
	return a, nil
}

// rememberMode is the jar's mode, from the one switch: nothing at all
// without the screen, whatever Remember says; with it, Remember=false
// forgets and anything else remembers.
func rememberMode(cfg Config) lastsignin.Mode {
	switch {
	case !cfg.SigninScreen:
		return lastsignin.Off
	case cfg.Remember != nil && !*cfg.Remember:
		return lastsignin.Forgetting
	default:
		return lastsignin.On
	}
}

// RememberJar is the jar holding this browser's remembered way in, and
// the seam that ends a sign-in attempt. Give it to passkey.Config.Remember
// so a passkey sign-in clears the screen's typed address and is
// remembered like the other two ways in. With SigninScreen off the jar
// is inert, so wiring it is always safe.
func (a *Auth) RememberJar() *lastsignin.Jar { return a.jar }

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

// trustedHops resolves Config.TrustedProxyHops: an explicit value wins;
// unset is the CARLOS edge's one hop there, and none anywhere else.
func trustedHops(set *int, onCarlos bool) int {
	switch {
	case set != nil:
		return *set
	case onCarlos:
		return clientip.DefaultHops
	default:
		return 0
	}
}
