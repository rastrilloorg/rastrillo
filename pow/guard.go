package pow

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Reason is why a submission was refused. The set is closed so a log
// of refusals stays countable and nothing a submitter typed reaches it.
type Reason string

const (
	ReasonHoneypot    Reason = "honeypot"
	ReasonSealInvalid Reason = "seal_invalid"
	ReasonTooFast     Reason = "too_fast"
	ReasonTooOld      Reason = "too_old"
	ReasonShort       Reason = "pow_short"
	ReasonSpent       Reason = "spent"
	// ReasonBounds: a body that would not parse as a form at all.
	ReasonBounds Reason = "bounds"
	// ReasonUnavailable: the nonce store failed. Still a refusal:
	// letting a submission through when replay protection is unreachable
	// turns a database outage into unlimited replay.
	ReasonUnavailable Reason = "unavailable"
	// ReasonMissing: no challenge was posted at all. The form was never
	// wired, or predates this release; an operator needs to tell that
	// apart from an attack, so it is not seal_invalid.
	ReasonMissing Reason = "missing"
	// ReasonAttempts: this token has been admitted Config.Attempts times.
	ReasonAttempts Reason = "attempts"
	// ReasonBusy: Config.Tracked tokens are already being counted.
	ReasonBusy Reason = "busy"
)

const (
	// NoProof is Config.Difficulty for a token-only form: a sealed,
	// single-use challenge and the honeypot, no proof of work, and no
	// JavaScript needed to submit it.
	NoProof         = -1
	DefaultAttempts = 20
	DefaultTracked  = 100_000
)

var (
	// ErrNoNonceStore means Config.Nonces was nil. There is no default,
	// because the default would have to be "no replay protection": one
	// solved challenge stays good for its whole MaxAge window, and every
	// replay costs the attacker nothing and your app another row, another
	// message, another whatever the form spends. Passing MemoryNonces is a
	// decision somebody typed.
	ErrNoNonceStore     = errors.New("rastrillo/pow: Config.Nonces must not be nil")
	ErrEmptyInstanceKey = errors.New("rastrillo/pow: Config.InstanceKey must not be empty")
	// ErrNoAssets: a proof-of-work form the browser cannot complete must
	// not boot. A URL being set does not prove it is served; the status
	// line covers the visitor's side of that.
	ErrNoAssets = errors.New("rastrillo/pow: Config.ScriptURL and Config.WorkerURL are required unless Difficulty is NoProof")
	// ErrBindNeedsProof: NoProof has no work to bind, and a Bind that is
	// silently ignored is a setting somebody believes is protecting them.
	ErrBindNeedsProof = errors.New("rastrillo/pow: Config.Bind needs proof of work; it means nothing with NoProof")
	// ErrSpent: the token was spent by an identical request, or expired
	// before the spend. Refuse; never 500. Commit does not look further
	// to tell the two apart (see Execer).
	ErrSpent = errors.New("rastrillo/pow: challenge spent or expired")
	// ErrNotAdmitted: Commit of a refused admission. A handler that went
	// on past a refusal would otherwise create its row with nothing
	// consumed.
	ErrNotAdmitted = errors.New("rastrillo/pow: commit of a refused admission")
)

// Config configures New. InstanceKey and Nonces are required, and the
// asset URLs unless Difficulty is NoProof.
type Config struct {
	// InstanceKey seals challenges, derived under its own label so a
	// token minted by another subsystem never verifies here.
	InstanceKey string
	Nonces      NonceStore
	// Difficulty defaults to DefaultDifficulty; NoProof for token-only.
	Difficulty int
	// Bind ties the work to the [data-pow-binding] input. Off by
	// default: with single-use tokens one solve already buys one
	// submission, and binding means solving cannot start before submit.
	Bind bool
	// MinAge and MaxAge default to DefaultMinAge and DefaultMaxAge.
	// MaxAge is read once, at issue, and sealed into the token.
	MinAge, MaxAge time.Duration
	// Attempts and Tracked bound Admit's per-token counting; defaults
	// DefaultAttempts and DefaultTracked. Check never counts.
	Attempts, Tracked int
	// ScriptURL and WorkerURL are the fingerprinted URLs of pow.js and
	// pow-worker.js as the app serves pow.Assets().
	ScriptURL, WorkerURL string
}

// Guard is a front door. One Guard serves many forms: the scope passed
// to Form and Want keeps their tokens apart.
type Guard struct {
	key                  []byte
	nonces               NonceStore
	difficulty           int // 0: no proof
	bind                 bool
	minAge, maxAge       time.Duration
	scriptURL, workerURL string
	attempts             *attempts
	now                  func() time.Time
}

// New returns a Guard, or an error naming what is missing. It checks
// the store is ready, so apply pow.Schema before calling it.
func New(cfg Config) (*Guard, error) {
	if cfg.InstanceKey == "" {
		return nil, ErrEmptyInstanceKey
	}
	if cfg.Nonces == nil {
		return nil, ErrNoNonceStore
	}
	key := sha256.Sum256([]byte("rastrillo/pow/challenge\x00" + cfg.InstanceKey))
	g := &Guard{
		key: key[:], nonces: cfg.Nonces, bind: cfg.Bind,
		minAge: cfg.MinAge, maxAge: cfg.MaxAge,
		scriptURL: cfg.ScriptURL, workerURL: cfg.WorkerURL,
		now: time.Now,
	}
	switch {
	case cfg.Difficulty == NoProof:
		g.difficulty = 0
	case cfg.Difficulty <= 0:
		g.difficulty = DefaultDifficulty
	default:
		g.difficulty = cfg.Difficulty
	}
	if g.difficulty > 0 && (cfg.ScriptURL == "" || cfg.WorkerURL == "") {
		return nil, ErrNoAssets
	}
	if cfg.Bind && g.difficulty == 0 {
		return nil, ErrBindNeedsProof
	}
	if g.minAge <= 0 {
		g.minAge = DefaultMinAge
	}
	if g.maxAge <= 0 {
		g.maxAge = DefaultMaxAge
	}
	limit, max := cfg.Attempts, cfg.Tracked
	if limit <= 0 {
		limit = DefaultAttempts
	}
	if max <= 0 {
		max = DefaultTracked
	}
	g.attempts = newAttempts(limit, max)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cfg.Nonces.Ready(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

// Bound reports Config.Bind, for a package that renders forms it cannot
// bind (auth's sign-in) and must refuse a bound Guard at boot.
func (g *Guard) Bound() bool { return g.bind }

// Want is what the server expects of a submission: the scope it issued
// the form under, and, for a bound Guard, the submitted value.
type Want struct {
	Scope   string
	Binding string
}

// Admission is Admit's verdict, and the handle that spends it.
type Admission struct {
	OK     bool
	Reason Reason   // the first failure
	Also   []Reason // every other cheap failure, for the log
	g      *Guard
	c      Challenge
	sealed bool
}

func (a *Admission) fail(r Reason) {
	a.OK = false
	if a.Reason == "" {
		a.Reason = r
		return
	}
	a.Also = append(a.Also, r)
}

// Recovered reports that the posted token was a recovery token. Only
// after the seal verified: an unverified flag is a number the submitter
// chose. A handler that re-renders uses it to keep recovery sticky, or
// a trapless attempt that fails on a wrong password comes back with the
// trap and is refused again.
func (a Admission) Recovered() bool { return a.sealed && a.c.Flags&flagTrapOmitted != 0 }

// Parent is a token Verify authenticated, for a request made on a
// parent form's behalf.
type Parent struct {
	Scope      string
	Nonce      string // canonical: key allowances by it
	Difficulty int
	Issued     time.Time
}

// Admit decides and writes nothing; Commit spends, inside the caller's
// transaction. It reads the form, so bound the body with
// http.MaxBytesReader first.
//
// Admit reads the store through the store's own connection, not the
// caller's, so call it before beginning the transaction: only
// Commit(ctx, tx) belongs inside. On a writer pool of one connection
// (the usual SQLite setup) an Admit inside the transaction waits for
// the connection the transaction holds, and the handler deadlocks.
//
// Order: honeypot, then the seal before anything it vouches for, then
// every cheap check (each failure recorded, so the log shows what a
// refusal was also wrong about), and only then, if all passed, the one
// database read and the attempt count.
func (g *Guard) Admit(r *http.Request, w Want) Admission { return g.admit(r, w, true) }

func (g *Guard) admit(r *http.Request, w Want, count bool) Admission {
	a := Admission{g: g}
	if err := r.ParseForm(); err != nil {
		a.fail(ReasonBounds)
		return a
	}
	c, reason := readChallenge(r)
	if reason != "" {
		a.fail(reason)
		return a
	}
	// The flag is unverified here; if it lies, the seal check below
	// refuses the request anyway.
	if c.Flags&flagTrapOmitted == 0 && Trapped(r) {
		a.fail(ReasonHoneypot)
	}
	if !sealOK(g.key, c) || c.Scope != w.Scope {
		a.fail(ReasonSealInvalid)
		return a
	}
	a.c, a.sealed = c, true
	now := g.now()
	if now.Sub(c.Issued) < g.minAge {
		a.fail(ReasonTooFast)
	}
	// Sealed expiry, and never longer than this Guard's MaxAge: a deploy
	// that raised MaxAge must not revive tokens already spent and swept.
	if now.After(c.Expires) || c.Expires.Sub(c.Issued) > g.maxAge {
		a.fail(ReasonTooOld)
	}
	if c.Difficulty < g.difficulty || (c.Flags&flagBound != 0) != g.bind {
		a.fail(ReasonSealInvalid)
	}
	if c.Difficulty > 0 {
		binding := ""
		if g.bind {
			binding = w.Binding
		}
		// A bound Guard with no binding is a caller that forgot
		// Want.Binding. Checking the work against "" would accept a
		// client that solved unbound: refuse instead.
		if g.bind && strings.TrimSpace(binding) == "" ||
			!Verify(c.Nonce, binding, r.PostFormValue(fieldCounter), c.Difficulty) {
			a.fail(ReasonShort)
		}
	}
	if a.Reason != "" {
		return a
	}
	spent, err := g.nonces.Spent(r.Context(), c.Nonce)
	switch {
	case err != nil:
		a.fail(ReasonUnavailable)
		return a
	case spent:
		a.fail(ReasonSpent)
		return a
	}
	if count {
		if reason := g.attempts.take(c.Nonce, c.Expires, now); reason != "" {
			a.fail(reason)
			return a
		}
	}
	a.OK = true
	return a
}

// Commit spends the admitted token on ex: the caller's transaction, so
// a validation failure never reaches it and a rollback undoes it. nil
// spends on the store's own handle.
func (a Admission) Commit(ctx context.Context, ex Execer) error {
	if !a.OK || a.g == nil {
		return ErrNotAdmitted
	}
	fresh, err := a.g.nonces.Spend(ctx, ex, a.c.Nonce, a.c.Expires)
	if err != nil {
		return err
	}
	if !fresh {
		return ErrSpent
	}
	return nil
}

// Check is Admit and Commit at once, on the store's own handle, without
// the attempt count: it spends immediately, so there is nothing
// uncommitted to count, and a map filled through Admit by another form
// cannot make it answer busy. For a handler that redirects after every
// POST or has no transaction of its own, where the spend must land
// before mail or a probe does.
//
// It reads and spends through the store's own connection, so never
// call it inside an open transaction: on a writer pool of one
// connection it waits for the connection that transaction holds, and
// the handler deadlocks.
func (g *Guard) Check(r *http.Request, w Want) Admission {
	a := g.admit(r, w, false)
	if !a.OK {
		return a
	}
	fresh, err := g.nonces.Spend(r.Context(), nil, a.c.Nonce, a.c.Expires)
	switch {
	case err != nil:
		a.fail(ReasonUnavailable)
	case !fresh:
		a.fail(ReasonSpent)
	}
	return a
}

// Verify authenticates a parent token for a request made on its form's
// behalf (an email check, an upload): seal, expiry, proof at the
// sealed difficulty, not spent. It neither counts nor spends, and
// returns the authenticated scope for the caller to authorise. Any
// Guard sharing the instance key verifies any of the app's unbound
// tokens. A bound token is refused: its proof cannot be checked without
// the value it was bound to, which such a request does not carry.
//
// Verify reads the store through the store's own connection, so call it
// before beginning a transaction, never inside one: on a writer pool of
// one connection it waits for the connection that transaction holds,
// and the handler deadlocks.
func (g *Guard) Verify(r *http.Request) (Parent, Reason, bool) {
	if err := r.ParseForm(); err != nil {
		return Parent{}, ReasonBounds, false
	}
	c, reason := readChallenge(r)
	if reason != "" {
		return Parent{}, reason, false
	}
	if !sealOK(g.key, c) || c.Flags&flagBound != 0 {
		return Parent{}, ReasonSealInvalid, false
	}
	now := g.now()
	if now.After(c.Expires) || c.Expires.Sub(c.Issued) > g.maxAge {
		return Parent{}, ReasonTooOld, false
	}
	if c.Difficulty > 0 && !Verify(c.Nonce, "", r.PostFormValue(fieldCounter), c.Difficulty) {
		return Parent{}, ReasonShort, false
	}
	spent, err := g.nonces.Spent(r.Context(), c.Nonce)
	if err != nil {
		return Parent{}, ReasonUnavailable, false
	}
	if spent {
		return Parent{}, ReasonSpent, false
	}
	return Parent{Scope: c.Scope, Nonce: c.Nonce, Difficulty: c.Difficulty, Issued: c.Issued}, "", true
}

// Trapped reports whether the honeypot was filled.
func Trapped(r *http.Request) bool {
	return strings.TrimSpace(r.PostFormValue(fieldHoneypot)) != ""
}

// Sweep drops spent rows past their sealed expiry and the margin, at
// most a batch per call. Call it from a tick the app already has.
func (g *Guard) Sweep(now time.Time) error { return g.nonces.Sweep(now) }
