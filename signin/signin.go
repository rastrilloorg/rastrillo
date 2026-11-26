// Package signin assembles Rastrillo's default sign-in on top of auth:
// the emailed link first, and a passkey, an authenticator app and a
// passphrase as further ways in. It builds passkey and totp from one
// Config, keeps the passphrases, limits the secret ways' misses in SQL
// and fills auth's SigninState with the doors that are on. Every way
// but the passkey ends in auth.Admit, so refusal, the remembered way,
// return_to and the session are the same as for a link.
//
// The name matches Keymail's library (github.com/keymaildev/signin),
// which only auth imports, so the two never meet in one file.
package signin

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/carlos"
	"amadan.net/rastrillo/rastrillo/clientip"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/totp"
)

// Ways is the set of ways in, beyond the emailed link, an app turns
// on. The link is always on and has no bit.
type Ways uint8

const (
	Passkey Ways = 1 << iota
	Authenticator
	Passphrase
)

// The paths signin's ways answer on, which Fill puts on the doors and
// Mount mounts. The passkey door is passkey's discover pair, the one
// that signs a person in on a passkey alone. Its script is served as
// signin.mjs, the name register.mjs imports it by.
const (
	CodePath           = "/signin/code"
	PassphrasePath     = "/signin/passphrase"
	PasskeyBeginPath   = "/passkey/discover/begin"
	PasskeyFinishPath  = "/passkey/discover/finish"
	WebauthnModulePath = "/signin/js/webauthn.mjs"
	PasskeyModulePath  = "/signin/js/signin.mjs"
)

// PasskeyProbePath is where Mount serves passkey.ProbeJS(). The card
// waits for it, so its name carries its bytes' hash and it is cached
// for good, where the modules are asked for again on every load.
var PasskeyProbePath = "/signin/js/probe." + hash(passkey.ProbeJS()) + ".js"

// Config configures New. Auth, DB and Render are always required;
// Origin, Key and Issuer when the passkey or authenticator way is on.
type Config struct {
	Auth *auth.Auth
	// Sessions is the store the app's sessions live in; nil means
	// Auth.Sessions(), which is where a link signs people in.
	Sessions *sessions.Sessions
	DB       *sql.DB
	Origin   string
	// Key seals authenticator secrets at rest: derive it from the
	// instance key (crypto.Derive), never leave it zero.
	Key [32]byte
	// Issuer is the name beside the code in the authenticator app.
	Issuer string
	Ways   Ways
	// Steps are the sign-up details the app collects after the email is
	// proved; Gate sends a person to the first one still needed.
	Steps []Step
	// Lookup finds the subject an address signs in as, and must never
	// create one. Required when auth's SubjectFor is set, because that
	// hook may create rows; ok false is an unknown address.
	Lookup func(ctx context.Context, address string) (
		subject string, ok bool, err error)
	// Authorize is asked before a passkey signs subject in. Without
	// Lookup subjects are addresses and auth's Authorize answers; with
	// it, and the passkey way on, the app must say who is let in.
	Authorize func(subject string) bool
	// Proof is the pow.Guard auth has, since the screen's forms carry
	// auth's challenge; nil only with ProofOff.
	Proof    *pow.Guard
	ProofOff bool
	Render   func(response http.ResponseWriter, request *http.Request,
		view View)
	// TrustedProxyHops is auth's: how many proxies stand in front of
	// the app, for the client address the limiter counts by.
	TrustedProxyHops *int
	Logger           *slog.Logger
	Now              func() time.Time
}

// Signins is the assembled sign-in. Build one per process with New.
type Signins struct {
	config      Config
	codes       *totp.Handlers
	passkeys    *passkey.Handlers
	passphrases passphrases
	limit       limiter
	decoy       string
	hops        int
	// checkCode and checkPassphrase are the credential checks, fields
	// so a test can see that a refused request never reached them.
	checkCode       secretCheck
	checkPassphrase secretCheck
	// checkDecoyCode is the work a code costs for a subject with no
	// authenticator; a field so a test can see the unknown path run it.
	checkDecoyCode func(submitted string)
}

// secretCheck is whether secret is subject's code or passphrase.
type secretCheck = func(ctx context.Context, subject, secret string) (
	bool, error)

// New checks config against the Auth it assembles and builds the ways
// that are on.
func New(config Config) (*Signins, error) {
	if err := config.refusal(); err != nil {
		return nil, err
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Sessions == nil {
		config.Sessions = config.Auth.Sessions()
	}
	decoy, err := decoyHash()
	if err != nil {
		return nil, err
	}
	signins := &Signins{
		config: config, decoy: decoy, limit: limiter{database: config.DB},
		passphrases: passphrases{database: config.DB, now: config.Now},
		hops:        trustedHops(config.TrustedProxyHops, carlos.Running()),
	}
	signins.checkPassphrase = signins.verifyPassphrase
	if err := signins.buildWays(); err != nil {
		return nil, err
	}
	return signins, nil
}

// refusal is the first thing in config New cannot build on.
func (config Config) refusal() error {
	switch {
	case config.Auth == nil:
		return errors.New("rastrillo/signin: Config.Auth is required: " +
			"pass the *auth.Auth the app signs people in with")
	case config.DB == nil:
		return errors.New("rastrillo/signin: Config.DB is required: " +
			"pass the database signin.Schema was applied to")
	case config.Render == nil:
		return errors.New("rastrillo/signin: Config.Render is required: " +
			"pass a function that draws a View in the app's layout")
	case config.Ways&Authenticator != 0 && config.Key == [32]byte{}:
		return errors.New("rastrillo/signin: Config.Key is all zeros: " +
			"derive one from the instance key with crypto.Derive")
	}
	if err := config.Auth.Assembles(); err != nil {
		return err
	}
	_, err := config.Auth.Subject(context.Background(), "")
	if errors.Is(err, auth.ErrNeedsLookup) && config.Lookup == nil {
		return errors.New("rastrillo/signin: auth's SubjectFor may " +
			"create subjects; set Config.Lookup to find one without it")
	}
	if config.Lookup != nil && config.Ways&Passkey != 0 &&
		config.Authorize == nil {
		return errors.New("rastrillo/signin: with Config.Lookup and the " +
			"passkey way on, set Config.Authorize to say which subjects " +
			"a passkey may sign in")
	}
	return config.proofRefusal()
}

// proofRefusal mirrors auth's and password's: an app that upgraded
// without wiring the proof must fail at boot, not run unguarded.
func (config Config) proofRefusal() error {
	switch {
	case config.Proof == nil && !config.ProofOff:
		return errors.New("rastrillo/signin: Config.Proof is unset: " +
			"pass auth's pow.Guard, or set Config.ProofOff as auth does")
	case config.Proof != nil && config.ProofOff:
		return errors.New("rastrillo/signin: Config.Proof and " +
			"Config.ProofOff are both set; choose one")
	case config.Proof != nil && config.Proof.Bound():
		return errors.New("rastrillo/signin: Config.Proof is a bound " +
			"pow.Guard; the sign-in forms need auth's unbound one")
	}
	return nil
}

// trustedHops is auth's rule for Config.TrustedProxyHops, so the
// limiter counts the client auth's own limiter does.
func trustedHops(set *int, onCarlos bool) int {
	switch {
	case set != nil:
		return *set
	case onCarlos:
		return clientip.DefaultHops
	}
	return 0
}

// Fill sets the doors of the ways that are on, leaving the others nil
// so the screen offers only what the app mounted.
func (signins *Signins) Fill(state *auth.SigninState) {
	ways := signins.config.Ways
	if ways&Passkey != 0 {
		state.Passkey = &auth.PasskeyDoor{
			BeginPath: PasskeyBeginPath, FinishPath: PasskeyFinishPath,
			ModuleURL: WebauthnModulePath, ScriptURL: PasskeyModulePath,
			ProbeURL: PasskeyProbePath,
		}
	}
	if ways&Authenticator != 0 {
		state.Authenticator = &auth.SecretDoor{Path: CodePath}
	}
	if ways&Passphrase != 0 {
		state.Passphrase = &auth.SecretDoor{Path: PassphrasePath}
	}
}

// Sweep forgets misses too old to count. Call it from the loop that
// sweeps auth.
func (signins *Signins) Sweep(now time.Time) error {
	return signins.limit.sweep(context.Background(), now)
}
