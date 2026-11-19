package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/mail"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// BlindLink keeps the pending credential separate from the delivery address.
// Store only its token hash, opaque lookup key, purpose and expiry.
type BlindLink struct {
	Hash      string
	LookupKey string
	Purpose   string
	Expires   time.Time
}

// BlindLinkStore must consume each hash at most once across concurrent
// processes and restarts. Take consumes the record atomically before auth
// checks its hash, purpose, key and expiry. A returned record stays spent
// when validation or subject resolution fails.
//
// Put must reject duplicate hashes. Spend removes links for a lookup key;
// Sweep removes expired links. Implementations must be safe for concurrent
// use and persist only BlindLink fields.
type BlindLinkStore interface {
	Put(context.Context, BlindLink) error
	Take(context.Context, string) (BlindLink, bool, error)
	Spend(context.Context, string) (int, error)
	Sweep(context.Context, time.Time) error
}

// BlindLinks keeps addresses out of link and session storage. LookupKey
// receives a lowercase, trimmed address before storage. Return the app’s
// HMAC email index, encoded as hex or base64url, using a pepper the app holds.
// ResolveSubject receives that key after consumption. Enforce admission
// there and return an opaque account or person ref. Return sessions.ErrNoUser
// for a 403 or Config.Refused; Refused receives the verified lookup key in
// Identity.Address. Other errors fail the sign-in without a session.
//
// Both results must be nonempty, unchanged by trimming, and contain no @.
// Do not return encoded or encrypted addresses: these checks cannot prove
// that a value is opaque. SecondFactor receives the resolved subject.
//
// Use a Mailer that delivers without retaining or logging the recipient.
// Leave Authorize, SubjectFor, SigninScreen, Records, Blind, Pepper and
// Signed unset, and KeymailServers empty. Keymail and cookies that remember
// an address are disabled. Keep addresses out of your hooks, middleware
// and logger attributes too. Legacy Config.Blind still stores decryptable
// link addresses and does not provide this guarantee.
type BlindLinks struct {
	Store          BlindLinkStore
	LookupKey      func(context.Context, string) (string, error)
	ResolveSubject func(context.Context, string) (string, error)
}

// ErrBlindConfiguration names the settings to change without including
// their values. Match it with errors.Is when New refuses BlindLinks.
var ErrBlindConfiguration = errors.New(
	"rastrillo/auth: invalid BlindLinks configuration; " +
		"set Store, LookupKey, ResolveSubject and a delivering Mailer; " +
		"leave Authorize, SubjectFor, SigninScreen, Records, Blind, " +
		"Pepper and Signed unset, and KeymailServers empty",
)

// ErrBlindOperation hides the cause because a mailer, store or hook may
// include an address in its error. Match it with errors.Is; it wraps no cause.
var ErrBlindOperation = errors.New(
	"rastrillo/auth: blind sign-in operation failed",
)

func blindConfig(cfg Config) error {
	blind := cfg.BlindLinks
	if blind == nil {
		return nil
	}
	_, logs := cfg.Mailer.(*signin.LogMailer)
	switch {
	case logs:
		return ErrBlindConfiguration
	case blind.Store == nil || blind.LookupKey == nil ||
		blind.ResolveSubject == nil:
		return ErrBlindConfiguration
	case cfg.Records != nil || cfg.Blind || cfg.Pepper != nil ||
		cfg.Signed != nil:
		return ErrBlindConfiguration
	case cfg.Authorize != nil || cfg.SubjectFor != nil || cfg.SigninScreen:
		return ErrBlindConfiguration
	case len(cfg.KeymailServers) != 0:
		return ErrBlindConfiguration
	case cfg.Mailer == nil || !mail.IsConfigured(cfg.Mailer):
		return ErrBlindConfiguration
	}
	return nil
}

type blindLinkAdapter struct {
	config BlindLinks
	now    func() time.Time
}

func opaque(value string) bool {
	return value != "" && strings.TrimSpace(value) == value &&
		!strings.Contains(value, "@")
}

func (blind *BlindLinks) lookup(ctx context.Context, address string) (
	string, error,
) {
	key, err := blind.LookupKey(ctx,
		strings.ToLower(strings.TrimSpace(address)))
	if err != nil || !opaque(key) {
		return "", ErrBlindOperation
	}
	return key, nil
}

func (store *blindLinkAdapter) PutLink(ctx context.Context,
	hash, address, purpose string, expires time.Time,
) error {
	key, err := store.config.lookup(ctx, address)
	if err != nil {
		return err
	}
	return blindError(store.config.Store.Put(ctx, BlindLink{
		Hash: hash, LookupKey: key, Purpose: purpose, Expires: expires,
	}))
}

func (store *blindLinkAdapter) TakeLink(ctx context.Context,
	hash, purpose string,
) (string, bool, error) {
	// Consume first so a refused credential cannot be tried again.
	link, ok, err := store.config.Store.Take(ctx, hash)
	if err != nil {
		return "", false, ErrBlindOperation
	}
	if !ok || link.Hash != hash || link.Purpose != purpose ||
		!link.Expires.After(store.now()) || !opaque(link.LookupKey) {
		return "", false, nil
	}
	return link.LookupKey, true, nil
}

func blindError(err error) error {
	if err != nil {
		return ErrBlindOperation
	}
	return nil
}

func (auth *Auth) wireBlindLinks(links *BlindLinks) {
	auth.flow.Links = &blindLinkAdapter{
		config: *links, now: func() time.Time { return auth.now() },
	}
	// Keymail caches addresses and carries them in pending cookies.
	auth.flow.Classifier = nil
	auth.flow.Keymail = nil
	auth.flow.Now = func() time.Time { return auth.now() }
	auth.flow.Limiter = &blindLimiter{
		next: auth.flow.Limiter,
		pepper: crypto.Derive([]byte(auth.cfg.InstanceKey),
			"rastrillo/auth/blind-limiter/v1"),
	}
}

type blindLimiter struct {
	next   signin.Limiter
	pepper []byte
}

func (limiter *blindLimiter) Allow(ctx context.Context, key string,
	now time.Time, budget int,
) (bool, error) {
	// Rate budgets must retain no address after the request ends.
	return limiter.next.Allow(ctx, emailHash(limiter.pepper, key), now, budget)
}

func (auth *Auth) blindSubject(
	ctx context.Context, key string,
) (string, error) {
	subject, err := auth.cfg.BlindLinks.ResolveSubject(ctx, key)
	// Admission is a normal refusal; discard wrapped recipient text.
	if errors.Is(err, sessions.ErrNoUser) {
		return "", sessions.ErrNoUser
	}
	if err != nil || !opaque(subject) {
		return "", ErrBlindOperation
	}
	return subject, nil
}

func (auth *Auth) spendBlindLinks(ctx context.Context, address string) (
	int, error,
) {
	key, err := auth.cfg.BlindLinks.lookup(ctx, address)
	if err != nil {
		return 0, err
	}
	count, err := auth.cfg.BlindLinks.Store.Spend(ctx, key)
	return count, blindError(err)
}
