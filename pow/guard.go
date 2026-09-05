package pow

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Reason is why a submission was refused. The set is closed on purpose:
// a refusal you log has to be one of these, so the log stays countable
// and nothing that reaches it can carry anything a submitter typed.
type Reason string

const (
	ReasonHoneypot    Reason = "honeypot"
	ReasonSealInvalid Reason = "seal_invalid"
	ReasonTooFast     Reason = "too_fast"
	ReasonTooOld      Reason = "too_old"
	ReasonShort       Reason = "pow_short"
	ReasonNonceSpent  Reason = "nonce_spent"

	// ReasonBounds is a request body that would not parse as a form at
	// all — a truncated post, or one past http.MaxBytesReader's limit.
	ReasonBounds Reason = "bounds"

	// ReasonUnavailable is the nonce store failing. It is the one
	// refusal the submitter had nothing to do with, and it is still a
	// refusal: letting a submission through when replay protection is
	// unreachable turns a database outage into unlimited replay.
	ReasonUnavailable Reason = "unavailable"
)

// ErrNoNonceStore means Config.Nonces was nil. There is no default,
// because the default would have to be "no replay protection": one
// solved challenge stays good for its whole MaxAge window, and every
// replay costs the attacker nothing and your app another row, another
// message, another whatever the form spends. Passing MemoryNonces is a
// decision somebody typed.
var ErrNoNonceStore = errors.New("rastrillo/pow: Config.Nonces must not be nil")

// ErrEmptyInstanceKey means Config.InstanceKey was empty. Without it
// the seal is forgeable and every check below is theatre.
var ErrEmptyInstanceKey = errors.New("rastrillo/pow: Config.InstanceKey must not be empty")

// Config configures New. InstanceKey and Nonces are required;
// everything else has a default worth keeping until you have measured
// otherwise.
type Config struct {
	// InstanceKey seals challenges, derived as
	// sha256("rastrillo/pow/challenge\x00" + InstanceKey). It is the
	// same instance key auth takes, and the label is what stops a
	// token minted by one subsystem verifying in the other.
	InstanceKey string

	// Nonces remembers which challenges have been spent. SQLNonces for
	// anything that survives a restart; MemoryNonces for a single
	// process that can afford to forget.
	Nonces NonceStore

	// Difficulty defaults to DefaultDifficulty. Measure before you
	// change it, at p95 and p99 rather than the mean.
	Difficulty int

	// MinAge and MaxAge default to DefaultMinAge and DefaultMaxAge.
	MinAge, MaxAge time.Duration
}

// Guard is one form's front door.
type Guard struct {
	key            []byte
	nonces         NonceStore
	difficulty     int
	minAge, maxAge time.Duration
}

// New returns a Guard, or an error naming the field that was missing.
func New(cfg Config) (*Guard, error) {
	if cfg.InstanceKey == "" {
		return nil, ErrEmptyInstanceKey
	}
	if cfg.Nonces == nil {
		return nil, ErrNoNonceStore
	}
	key := sha256.Sum256([]byte("rastrillo/pow/challenge\x00" + cfg.InstanceKey))
	g := &Guard{
		key:        key[:],
		nonces:     cfg.Nonces,
		difficulty: cfg.Difficulty,
		minAge:     cfg.MinAge,
		maxAge:     cfg.MaxAge,
	}
	if g.difficulty <= 0 {
		g.difficulty = DefaultDifficulty
	}
	if g.minAge <= 0 {
		g.minAge = DefaultMinAge
	}
	if g.maxAge <= 0 {
		g.maxAge = DefaultMaxAge
	}
	return g, nil
}

// Issue mints a challenge for a form about to render. It writes
// nothing, so a crawler hitting the page a thousand times costs a
// thousand HMACs and no rows.
func (g *Guard) Issue(now time.Time) Challenge {
	return newChallenge(g.key, now, g.difficulty)
}

// Check runs the whole front door against a submitted form, and is the
// only thing a handler needs to call. It returns the reason a
// submission was refused, and whether it passed.
//
// binding is the value the proof of work was bound to — the address the
// form submitted, in the usual case. Pass exactly what the browser's
// [data-pow-binding] input held; the package normalises it identically
// on both sides.
//
// The order is not an implementation detail. The honeypot first,
// because it is free, needs no seal, and must not sit behind anything
// that writes. The seal before the clock, because until the signature
// holds, the timestamp is a number the submitter chose. The proof of
// work before the nonce is spent, so a submission that was never going
// to pass does not cost a row. And the nonce spent before the caller
// does anything at all, because a solved challenge is otherwise
// replayable for its whole MaxAge window.
//
// It reads the form, so it calls ParseForm. Bound the body with
// http.MaxBytesReader before you call it — a free-text field is
// otherwise an invitation to post a gigabyte.
//
// What to do with a refusal is the caller's, and mostly the answer is
// to render the same page every outcome renders. A form that says "you
// are already signed up" is a form anybody can ask about anybody.
func (g *Guard) Check(r *http.Request, binding string) (Reason, bool) {
	if err := r.ParseForm(); err != nil {
		return ReasonBounds, false
	}
	if strings.TrimSpace(r.PostFormValue(fieldHoneypot)) != "" {
		return ReasonHoneypot, false
	}
	c := Challenge{
		Nonce:      r.PostFormValue(fieldNonce),
		IssuedAt:   atoi64(r.PostFormValue(fieldIssuedAt)),
		Difficulty: atoi(r.PostFormValue(fieldDifficulty)),
		Seal:       r.PostFormValue(fieldSeal),
	}
	now := time.Now()
	if reason, ok := verifySeal(g.key, c, now, g.minAge, g.maxAge); !ok {
		return reason, false
	}
	// The difficulty is inside the seal, so this compares a value the
	// submitter could not edit against the one the guard is set to. A
	// challenge minted before the difficulty was lowered is still
	// honoured; one claiming less than it was minted with cannot exist.
	if c.Difficulty < g.difficulty {
		return ReasonSealInvalid, false
	}
	if !Verify(c.Nonce, binding, r.PostFormValue(fieldCounter), c.Difficulty) {
		return ReasonShort, false
	}
	fresh, err := g.nonces.Spend(r.Context(), c.Nonce, c.expiry(g.maxAge))
	if err != nil {
		// The store is the one step that can fail for a reason the
		// submitter had nothing to do with. Refusing is the only safe
		// answer: letting it through would make an unreachable
		// database into unlimited replay.
		return ReasonUnavailable, false
	}
	if !fresh {
		return ReasonNonceSpent, false
	}
	return "", true
}

// Trapped reports whether the honeypot was filled, for a handler that
// wants to answer before doing anything else and is not calling Check.
// Check does this itself, first.
func Trapped(r *http.Request) bool {
	return strings.TrimSpace(r.PostFormValue(fieldHoneypot)) != ""
}

// Sweep drops spent nonces that are past their expiry. Correctness
// never depends on it — an expired challenge is refused on age alone —
// its job is keeping the table from growing for the life of the
// instance. Call it from an existing tick; it needs no schedule of its
// own.
func (g *Guard) Sweep(now time.Time) error {
	return g.nonces.Sweep(now)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
